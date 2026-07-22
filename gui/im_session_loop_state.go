package main

import (
	"strings"
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
	endedAt  time.Time    // most recent loop end, retained for session lifecycle diagnostics
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
	return ctx != nil && !ctx.IsCancelled() && ctx.AcceptingReplans()
}

// tryInjectActiveReplan resolves the owning loop and accepts the injection
// while its session publication lock is held. Cleanup/replacement therefore
// cannot swap loopCtx between lookup and TryRequestReplan.
func (h *IMMessageHandler) tryInjectActiveReplan(userID string, enqueue func()) bool {
	if h == nil || userID == "" {
		return false
	}
	v, ok := h.sessionLoops.Load(userID)
	if !ok {
		return false
	}
	state := v.(*sessionLoopState)
	state.stateMu.RLock()
	defer state.stateMu.RUnlock()
	ctx := state.loopCtx
	if ctx == nil || ctx.IsCancelled() {
		return false
	}
	_, accepted := ctx.TryRequestReplan(enqueue)
	return accepted
}

// tryInjectGuideReference accepts only against a published active consumer.
// Owning the session serialization mutex is not enough: confirmation gates,
// direct-execution routes, and other pre-loop branches can still return without
// ever starting an LLM consumer.
func (h *IMMessageHandler) tryInjectGuideReference(userID, injection, expectedRequestID string) (accepted bool, preLoop bool) {
	if h == nil || userID == "" {
		return false, false
	}
	v, ok := h.sessionLoops.Load(userID)
	if !ok {
		return false, false
	}
	state := v.(*sessionLoopState)
	state.stateMu.RLock()
	defer state.stateMu.RUnlock()
	if ctx := state.loopCtx; ctx != nil {
		if ctx.IsCancelled() {
			return false, false
		}
		// Bind a delayed GUI steer to the turn that was visibly busy when the
		// user pressed Enter. Without this check, an RPC delayed across a turn
		// boundary could silently attach the instruction to the replacement turn
		// in the same session. Empty remains compatible with legacy callers.
		if expected := strings.TrimSpace(expectedRequestID); expected != "" &&
			strings.TrimSpace(ctx.Runtime.RequestID) != expected {
			return false, false
		}
		_, accepted = ctx.TryRequestReplan(func() { h.accumulateInjection(userID, injection) })
		return accepted, false
	}
	return false, false
}

func (h *IMMessageHandler) canAcceptGuideReferenceForUser(userID string) bool {
	if v, ok := h.sessionLoops.Load(userID); ok {
		state := v.(*sessionLoopState)
		state.stateMu.RLock()
		ctx := state.loopCtx
		state.stateMu.RUnlock()
		if ctx != nil {
			// A cancelled loop can still own the session mutex while unwinding,
			// but it will never run another model iteration. Do not acknowledge a
			// steer that has no consumer.
			return !ctx.IsCancelled() && ctx.AcceptingReplans()
		}
		return false
	}
	// A recently ended loop has no guaranteed consumer. Returning true here
	// used to make the GUI remove the queue item and show an accepted receipt,
	// even though the instruction could remain stranded until it expired.
	// Reject it so the GUI keeps the item and sends it as the next normal turn.
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
