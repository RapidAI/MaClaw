package main

import (
	"sync"
	"time"
)

// sessionLoopState holds per-userID loop state. Each desktop tab (local or
// project) and each IM user gets their own instance, enabling concurrent
// agent loop execution without data races on shared fields.
type sessionLoopState struct {
	mu       sync.Mutex   // serializes agent loop execution for THIS session
	stateMu  sync.RWMutex // protects loop metadata while mu may be held by a loop
	loopCtx  *LoopContext // active loop context (nil when idle)
	userText string       // last user message text for this session
	endedAt  time.Time    // most recent loop end, used for short guide-fire races
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
		state := v.(*sessionLoopState)
		state.stateMu.RLock()
		defer state.stateMu.RUnlock()
		return state.loopCtx
	}
	return nil
}

// hasActiveLoopForUser returns true if the given userID has a running,
// non-cancelled agent loop.
func (h *IMMessageHandler) hasActiveLoopForUser(userID string) bool {
	ctx := h.getSessionLoopCtx(userID)
	return ctx != nil && !ctx.IsCancelled()
}

func (h *IMMessageHandler) canAcceptGuideReferenceForUser(userID string) bool {
	if h.hasActiveLoopForUser(userID) {
		return true
	}
	if v, ok := h.sessionLoops.Load(userID); ok {
		state := v.(*sessionLoopState)
		if !state.mu.TryLock() {
			return true
		}
		state.mu.Unlock()
		state.stateMu.RLock()
		endedAt := state.endedAt
		state.stateMu.RUnlock()
		return !endedAt.IsZero() && time.Since(endedAt) <= 2*time.Minute
	}
	return false
}

func (h *IMMessageHandler) setSessionLoopCtx(userID string, ctx *LoopContext) {
	if h == nil {
		return
	}
	state := h.getSessionLoop(userID)
	state.stateMu.Lock()
	defer state.stateMu.Unlock()
	state.loopCtx = ctx
}
