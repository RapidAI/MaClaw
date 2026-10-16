package agentruntime

// Stable event type identifiers shared by GUI, headless service and future
// transports. Keeping these in the Runtime package prevents string literals
// from drifting between adapters.
const (
	EventRunStarted     = "run.started"
	EventRunCompleted   = "run.completed"
	EventRunFailed      = "run.failed"
	EventRunCancelled   = "run.cancelled"
	EventTurnStarted    = "turn.started"
	EventProgress       = "progress"
	EventAssistantDelta = "assistant.delta"
	EventToolCall       = "tool.call"
	EventToolResult     = "tool.result"
	EventCheckpoint     = "checkpoint"
	EventAskUser        = "ask_user"
)
