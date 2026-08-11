package codingruntime

import (
	"context"
	"strings"
	"sync"
)

// ChildExecutionRegistry keeps live, process-local cancellation handles for
// durably admitted read-only children. It deliberately does not replace the
// Store: task state, recovery, and stale-callback isolation remain durable
// Ledger responsibilities. The registry exists only to interrupt a child that
// is presently blocked in a model or host-tool call after its parent is
// explicitly cancelled.
//
// Hosts should keep one registry for the lifetime of their process and call
// Begin immediately before dispatching an admitted child. The returned release
// function must be deferred by the dispatch goroutine. A child must not inherit
// its parent request context: admission closes that parent Attempt normally as
// waiting_child, whereas CancelParent represents explicit cancellation of the
// durable parent subtree.
type ChildExecutionRegistry struct {
	mu       sync.Mutex
	byParent map[string]map[string]*childExecution
}

type childExecution struct {
	cancel context.CancelFunc
}

// Begin creates an independent execution-lifetime context for one admitted
// child and registers it under its parent task. Empty IDs still return a usable
// context, but are intentionally not registered because they cannot be safely
// targeted by a later durable cancellation.
func (r *ChildExecutionRegistry) Begin(parentTaskID, childTaskID string) (context.Context, func()) {
	parentTaskID, childTaskID = strings.TrimSpace(parentTaskID), strings.TrimSpace(childTaskID)
	ctx, cancel := context.WithCancel(context.Background())
	if r == nil || parentTaskID == "" || childTaskID == "" {
		return ctx, cancel
	}

	entry := &childExecution{cancel: cancel}
	var replaced context.CancelFunc
	r.mu.Lock()
	if r.byParent == nil {
		r.byParent = make(map[string]map[string]*childExecution)
	}
	children := r.byParent[parentTaskID]
	if children == nil {
		children = make(map[string]*childExecution)
		r.byParent[parentTaskID] = children
	}
	if prior := children[childTaskID]; prior != nil {
		replaced = prior.cancel
	}
	children[childTaskID] = entry
	r.mu.Unlock()
	// A task ID can only have one live execution. This also prevents a faulty
	// host retry from leaving an unreachable context running.
	if replaced != nil {
		replaced()
	}

	return ctx, func() {
		cancel()
		r.mu.Lock()
		if children := r.byParent[parentTaskID]; children != nil && children[childTaskID] == entry {
			delete(children, childTaskID)
			if len(children) == 0 {
				delete(r.byParent, parentTaskID)
			}
		}
		r.mu.Unlock()
	}
}

// CancelParent cancels every currently live child registered for parentTaskID.
// It intentionally leaves their entries in place until their dispatch
// goroutines return and call release, preserving a simple ownership rule and
// avoiding races with concurrent completion.
func (r *ChildExecutionRegistry) CancelParent(parentTaskID string) {
	if r == nil || strings.TrimSpace(parentTaskID) == "" {
		return
	}
	r.mu.Lock()
	children := r.byParent[strings.TrimSpace(parentTaskID)]
	cancels := make([]context.CancelFunc, 0, len(children))
	for _, entry := range children {
		if entry != nil && entry.cancel != nil {
			cancels = append(cancels, entry.cancel)
		}
	}
	r.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}
