package main

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"sync/atomic"
)

// codingChildExecutionContext is the deliberately narrow execution envelope
// for a nested CodingSubAgent. It is not a semantic identity, provider
// binding, grant, or transport correlation. In particular, it does not copy a
// parent LoopContext, its ingress token, request trace, attachments, workflow
// state, or UI session state into a child.
//
// Synchronous children may receive a one-way parent-cancellation bridge.
// Detached read-only children must not: their lifetime is owned by the runtime
// Attempt passed to ExecuteReadOnlyChild.
type codingChildExecutionContext struct {
	loopCtx *LoopContext
	release func()
}

var codingChildDiagnosticFallbackSeq atomic.Uint64

func newCodingChildExecutionContext(parent *LoopContext, httpClient *http.Client, detached bool) codingChildExecutionContext {
	child := NewLoopContext("coding-child-"+newCodingChildDiagnosticID(), 0, httpClient)
	release := func() { child.Done() }
	if detached || parent == nil {
		return codingChildExecutionContext{loopCtx: child, release: release}
	}

	// Context creates a cancellation-only bridge. No parent LoopContext fields
	// are copied to the child, and releasing the child stops the bridge before a
	// later parent cancellation can affect any reused object.
	parentCtx, parentCancel := parent.Context()
	child.BindParentContext(parentCtx)
	return codingChildExecutionContext{
		loopCtx: child,
		release: func() {
			parentCancel()
			child.Done()
		},
	}
}

func newCodingChildDiagnosticID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err == nil {
		return hex.EncodeToString(buf)
	}
	// This value is diagnostic-only. The fallback is intentionally not derived
	// from a task, path, loop ID, request ID, or any model-controlled value.
	return "fallback-" + strconv.FormatUint(codingChildDiagnosticFallbackSeq.Add(1), 10)
}

func (s *CodingSubAgent) releaseNestedLoopContext() {
	if s != nil && s.nestedLoopRelease != nil {
		s.nestedLoopRelease()
		s.nestedLoopRelease = nil
	}
}

func (r *RemoteCodingSubAgent) releaseNestedLoopContext() {
	if r != nil && r.nestedLoopRelease != nil {
		r.nestedLoopRelease()
		r.nestedLoopRelease = nil
	}
}
