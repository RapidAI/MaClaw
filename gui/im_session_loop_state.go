package main

import "sync"

// sessionLoopState holds per-userID loop state. Each desktop tab (local or
// project) and each IM user gets their own instance, enabling concurrent
// agent loop execution without data races on shared fields.
type sessionLoopState struct {
	mu       sync.Mutex   // serializes agent loop execution for THIS session
	loopCtx  *LoopContext // active loop context (nil when idle)
	userText string       // last user message text for this session
}

// getSessionLoop returns the sessionLoopState for the given userID, creating
// one if it doesn't exist. Safe for concurrent use.
func (h *IMMessageHandler) getSessionLoop(userID string) *sessionLoopState {
	if v, ok := h.sessionLoops.Load(userID); ok {
		return v.(*sessionLoopState)
	}
	state := &sessionLoopState{}
	actual, _ := h.sessionLoops.LoadOrStore(userID, state)
	return actual.(*sessionLoopState)
}

// getSessionLoopCtx returns the active LoopContext for the given userID,
// or nil if no loop is running for that session.
func (h *IMMessageHandler) getSessionLoopCtx(userID string) *LoopContext {
	if v, ok := h.sessionLoops.Load(userID); ok {
		return v.(*sessionLoopState).loopCtx
	}
	return nil
}

// hasActiveLoopForUser returns true if the given userID has a running,
// non-cancelled agent loop.
func (h *IMMessageHandler) hasActiveLoopForUser(userID string) bool {
	ctx := h.getSessionLoopCtx(userID)
	return ctx != nil && !ctx.IsCancelled()
}

func (h *IMMessageHandler) setSessionLoopCtx(userID string, ctx *LoopContext) {
	if h == nil {
		return
	}
	state := h.getSessionLoop(userID)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.loopCtx = ctx
}
