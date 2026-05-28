package main

import "strings"

// hasActiveInterruptableLoop returns true if there is a currently running
// agent loop (any session) that has NOT been cancelled. Used by CancelCurrentSession
// and InjectSupplementary which operate on the "current" (most recent) loop.
func (h *IMMessageHandler) hasActiveInterruptableLoop() bool {
	if h == nil {
		return false
	}
	h.globalLoopMu.RLock()
	ctx := h.currentLoopCtx
	h.globalLoopMu.RUnlock()
	return ctx != nil && !ctx.IsCancelled()
}

func (h *IMMessageHandler) shouldTryInlineInterrupt(msg IMUserMessage) bool {
	if h.interruptHandler == nil {
		return false
	}
	if strings.TrimSpace(msg.UserID) == "" {
		return false
	}
	if h.hasCancelledTaskBoundary(msg.UserID) {
		return false
	}
	// Only interrupt the active loop if it belongs to the same user.
	// Different users (or different project tabs) must not merge into
	// each other's loops.
	return h.hasActiveLoopForUser(msg.UserID)
}
