package guiapp

import "strings"

// hasActiveInterruptableLoop returns true if there is a currently running
// agent loop (any session) that has NOT been cancelled. Used by CancelCurrentSession
// and InjectSupplementary which operate on the "current" (most recent) loop.
func (h *IMMessageHandler) hasActiveInterruptableLoop() bool {
	if h == nil {
		return false
	}
	ctx, _, _ := h.legacyLoopSnapshot()
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
	// Structured confirmation action commands (__confirm_execution__ /
	// __cancel_execution__) have deterministic routing in preflight (the
	// credential card resolver runs before the session lock) and must never
	// enter the relevance scheduler — a queued or misclassified card reply
	// would deadlock against the fenced tool call it is meant to resolve.
	// This bypass is deliberately limited to those two prefixes: task-switch
	// commands (__workflow_choice__ / __resume_unfinished__ /
	// __dismiss_unfinished__ / __start_new_task__) keep the inline interrupt
	// path, where queueing behind the active loop is the pre-existing
	// (intended) behavior.
	if isConfirmationActionCommandText(msg.Text) {
		return false
	}
	// Only interrupt the active loop if it belongs to the same user.
	// Different users (or different project tabs) must not merge into
	// each other's loops.
	return h.hasActiveLoopForUser(msg.UserID)
}
