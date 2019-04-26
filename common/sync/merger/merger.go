/*
 * Copyright (c) 2019. Abstrium SAS <team (at) pydio.com>
 * This file is part of Pydio Cells.
 *
 * Pydio Cells is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * Pydio Cells is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with Pydio Cells.  If not, see <http://www.gnu.org/licenses/>.
 *
 * The latest code can be found at <https://pydio.com>.
 */

package merger

import (
	"context"

	"github.com/pydio/cells/common/proto/tree"
	"github.com/pydio/cells/common/sync/model"
)

type ConflictType int

const (
	ConflictFolderUUID ConflictType = iota
	ConflictFileContent
	ConflictNodeType
)

// Conflict represent a conflict between two nodes at the same path
type Conflict struct {
	Type      ConflictType
	NodeLeft  *tree.Node
	NodeRight *tree.Node
}

type OperationType int

const (
	OpCreateFile OperationType = iota
	OpUpdateFile
	OpCreateFolder
	OpMoveFolder
	OpMoveFile
	OpDelete
	OpRefreshUuid
)

type Operation struct {
	Key       string
	Type      OperationType
	Node      *tree.Node
	EventInfo model.EventInfo
	Batch     Batch
}

// NewDiff creates a new Diff implementation
func NewDiff(ctx context.Context, left model.PathSyncSource, right model.PathSyncSource) Diff {
	return newTreeDiff(ctx, left, right)
}

// NewBatch creates a new Batch implementation
func NewBatch(source model.PathSyncSource, target model.PathSyncTarget) Batch {
	return newSimpleBatch(source, target)
}

// Batch represents a set of operations to be processed
type Batch interface {
	model.Stater
	StatusProvider

	// Source get or set the source of this batch
	Source(newSource ...model.PathSyncSource) model.PathSyncSource
	// Target get or set the target of this batch
	Target(newTarget ...model.PathSyncTarget) model.PathSyncTarget

	// Enqueue stacks a Operation - By default, it is registered with the event.Key, but an optional key can be passed.
	// TODO : check this key param is really necessary
	Enqueue(event *Operation, key ...string)
	// EventsByTypes retrieves all events of a given type
	EventsByType(types []OperationType, sorted ...bool) (events []*Operation)
	// Filter tries to detect unnecessary changes locally
	Filter(ctx context.Context)
	// FilterToTarget tries to compare changes to target and remove unnecessary ones
	FilterToTarget(ctx context.Context)

	// HasTransfers tels if the source and target will exchange actual data.
	HasTransfers() bool
	// Size returns the total number of operations
	Size() int
	// ProgressTotal returns the total number of bytes to be processed, to be used for progress.
	// Basically, file transfers operations returns the file size, but other operations return a 1 byte size.
	ProgressTotal() int64

	// SetSessionProvider registers a target as supporting the SessionProvider interface
	SetSessionProvider(providerContext context.Context, provider model.SessionProvider)
	// StartSessionProvider calls StartSession on the underlying provider if it is set
	StartSessionProvider(rootNode *tree.Node) (*tree.IndexationSession, error)
	// FlushSessionProvider calls FlushSession on the underlying provider if it is set
	FlushSessionProvider(sessionUuid string) error
	// FinishSessionProvider calls FinishSession on the underlying provider if it is set
	FinishSessionProvider(sessionUuid string) error
}

// Diff represents basic differences between two sources
// It can be then transformed to Batch, depending on the sync being
// unidirectional (transform to Creates and Deletes) or bidirectional (transform only to Creates)
type Diff interface {
	model.Stater
	StatusProvider

	// Compute performs the actual Diff operation
	Compute() error
	// ToUnidirectionalBatch transforms current diff into a set of batch operations
	ToUnidirectionalBatch(direction model.DirectionType) (batch Batch, err error)
	// ToBidirectionalBatch transforms current diff into a set of 2 batches of operations
	ToBidirectionalBatch(leftTarget model.PathSyncTarget, rightTarget model.PathSyncTarget) (batch *BidirectionalBatch, err error)
	// conflicts list discovered conflicts
	Conflicts() []*Conflict
}

// ProcessStatus informs about the status of an operation
type ProcessStatus struct {
	IsError      bool
	StatusString string
	Progress     float32
}

// StatusProvider can register channels to send status/done events during processing
type StatusProvider interface {
	// SetupChannels register channels for listening to status and done infos
	SetupChannels(status chan ProcessStatus, done chan interface{})
	// Status notify of a new ProcessStatus
	Status(s ProcessStatus)
	// Done notify the batch is processed, operations is the number of processed operations
	Done(info interface{})
}

func ConflictsByType(cc []*Conflict, conflictType ConflictType) (conflicts []*Conflict) {
	for _, c := range cc {
		if c.Type == conflictType {
			conflicts = append(conflicts, c)
		}
	}
	return
}

func MostRecentNode(n1, n2 *tree.Node) *tree.Node {
	if n1.MTime > n2.MTime {
		return n1
	} else {
		return n2
	}
}

func (e *Operation) Source() model.PathSyncSource {
	return e.Batch.Source()
}

func (e *Operation) Target() model.PathSyncTarget {
	return e.Batch.Target()
}

func (e *Operation) NodeFromSource(ctx context.Context) (node *tree.Node, err error) {
	if e.EventInfo.ScanEvent && e.EventInfo.ScanSourceNode != nil {
		node = e.EventInfo.ScanSourceNode
	} else {
		node, err = e.Source().LoadNode(e.EventInfo.CreateContext(ctx), e.EventInfo.Path)
	}
	if err == nil {
		e.Node = node
	}
	return
}

func (e *Operation) NodeInTarget(ctx context.Context) (node *tree.Node, found bool) {
	if e.Node != nil {
		// If deleteEvent has node, it is already loaded from a snapshot, no need to reload from target
		return e.Node, true
	} else {
		node, err := e.Target().LoadNode(e.EventInfo.CreateContext(ctx), e.EventInfo.Path)
		if err != nil {
			return nil, false
		} else {
			return node, true
		}
	}
}