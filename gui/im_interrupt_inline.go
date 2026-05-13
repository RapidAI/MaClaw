package main

import "strings"

// hasActiveInterruptableLoop returns true if there is a currently running
// agent loop that has NOT been cancelled. This is the single source of truth
// for "should we attempt to interrupt/merge into the running loop?"
//
// All interrupt entry points (desktop panel serialization boundary, IM inline
// interrupt, hub client interrupt) MUST use this method instead of checking
// currentLoopCtx != nil directly. The distinction matters because after
// CancelCurrentSession() is called, currentLoopCtx is still non-nil until
// the loop's defer runs — but the loop is dying and must not accept merges.
func (h *IMMessageHandler) hasActiveInterruptableLoop() bool {
	if h == nil || h.currentLoopCtx == nil {
		return false
	}
	return !h.currentLoopCtx.IsCancelled()
}

func (h *IMMessageHandler) shouldTryInlineInterrupt(msg IMUserMessage) bool {
	if h.interruptHandler == nil || !h.hasActiveInterruptableLoop() {
		return false
	}
	if strings.TrimSpace(msg.UserID) == "" {
		return false
	}
	return true
}
