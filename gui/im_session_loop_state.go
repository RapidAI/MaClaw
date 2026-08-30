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
	mu          sync.Mutex   // serializes agent loop execution for THIS session
	stateMu     sync.RWMutex // protects loop metadata while mu may be held by a loop
	loopCtx     *LoopContext // active loop context (nil when idle)
	userText    string       // last user message text for this session
	pendingText string       // text of the request currently holding mu, before loopCtx is published
	pendingGen  uint64       // monotonic generation for that pending request; never reset on unlock
	endedAt     time.Time    // most recent loop end, retained for session lifecycle diagnostics
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
	if h.hasCancelledTaskBoundary(userID) {
		return false
	}
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

func (h *IMMessageHandler) pendingForegroundText(userID string) string {
	text, _ := h.pendingForegroundState(userID)
	return text
}

func (h *IMMessageHandler) pendingForegroundGen(userID string) uint64 {
	_, gen := h.pendingForegroundState(userID)
	return gen
}

func (h *IMMessageHandler) pendingForegroundState(userID string) (string, uint64) {
	if h == nil || strings.TrimSpace(userID) == "" {
		return "", 0
	}
	v, ok := h.sessionLoops.Load(userID)
	if !ok {
		return "", 0
	}
	state := v.(*sessionLoopState)
	state.stateMu.RLock()
	defer state.stateMu.RUnlock()
	return state.pendingText, state.pendingGen
}

func (h *IMMessageHandler) setPendingForegroundText(userID, text string) {
	if h == nil || strings.TrimSpace(userID) == "" {
		return
	}
	state := h.getSessionLoop(userID)
	state.stateMu.Lock()
	state.pendingText = text
	if text != "" {
		state.pendingGen++
	}
	state.stateMu.Unlock()
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

// inFlightTurn is a request that holds state.mu and has a LoopContext, but has
// not yet published that context as the active session loop. Cancel and the
// next-message merge fence must observe it during the UIC / pre-loop window.
type inFlightTurn struct {
	ctx      *LoopContext
	userText string
}

func (h *IMMessageHandler) beginInFlightTurn(userID, userText string, ctx *LoopContext) {
	if h == nil || strings.TrimSpace(userID) == "" || ctx == nil {
		return
	}
	h.inFlightTurns.Store(userID, &inFlightTurn{ctx: ctx, userText: userText})
}

func (h *IMMessageHandler) endInFlightTurn(userID string, ctx *LoopContext) {
	if h == nil || strings.TrimSpace(userID) == "" || ctx == nil {
		return
	}
	current, ok := h.inFlightTurns.Load(userID)
	if !ok {
		return
	}
	turn, _ := current.(*inFlightTurn)
	if turn == nil || turn.ctx != ctx {
		return
	}
	h.inFlightTurns.CompareAndDelete(userID, current)
}

func (h *IMMessageHandler) inFlightTurnForUser(userID string) *inFlightTurn {
	if h == nil || strings.TrimSpace(userID) == "" {
		return nil
	}
	current, ok := h.inFlightTurns.Load(userID)
	if !ok {
		return nil
	}
	turn, _ := current.(*inFlightTurn)
	if turn == nil || turn.ctx == nil {
		return nil
	}
	return turn
}

func (h *IMMessageHandler) cancelAndClearInFlightTurn(userID string) {
	if h == nil || strings.TrimSpace(userID) == "" {
		return
	}
	current, ok := h.inFlightTurns.Load(userID)
	if !ok {
		return
	}
	if turn, _ := current.(*inFlightTurn); turn != nil && turn.ctx != nil {
		turn.ctx.Cancel()
	}
	// CompareAndDelete so a replacement turn stored after this load is kept.
	h.inFlightTurns.CompareAndDelete(userID, current)
}
