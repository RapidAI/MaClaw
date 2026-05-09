package main

// shouldApplyWorkflowFilter decides whether workflow phase tool filtering must
// stay active for the current loop. Awaiting-review is an engine-level barrier:
// it overrides per-message skip signals so NeedsConfirm phases cannot continue
// into unrestricted tool execution.
func shouldApplyWorkflowFilter(skipNeedsConfirmGate, awaitingReview bool) bool {
	return !skipNeedsConfirmGate || awaitingReview
}

// shouldBypassNeedsConfirmGate decides whether a per-message continuation may
// bypass NeedsConfirm capture. The engine review barrier always wins; a saved
// NeedsConfirm output must be explicitly confirmed, modified, or cancelled.
func shouldBypassNeedsConfirmGate(skipNeedsConfirmGate, codingGateActive, awaitingReview bool) bool {
	return skipNeedsConfirmGate && !codingGateActive && !awaitingReview
}
