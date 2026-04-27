package progress

// InterruptHandler is the interface that IM gateways use to signal the
// running agent loop when a new message arrives during execution.
//
// This decouples the gateway (corelib/weixin, hub/im) from the agent loop
// implementation (gui/im_message_handler). The gateway doesn't need to know
// about LoopContext or IMMessageHandler — it just calls TryInterrupt with
// the new message text, and the handler decides what to do.
type InterruptHandler interface {
	// TryInterrupt is called by the IM gateway when a new message arrives
	// while the agent loop is running (lock contention detected).
	//
	// Returns:
	//   - result.Handled: true if the message was processed (cancel/merge/status)
	//   - result.Action: the scheduling action taken
	//   - result.Reply: optional text to send back to the user immediately
	TryInterrupt(userID string, messageText string) InterruptResult
}

// InterruptResult is the structured return from TryInterrupt.
// Using a struct instead of (bool, string) avoids workarounds like
// parsing reply text to determine the action type.
type InterruptResult struct {
	Handled bool           // true if the message was fully processed (don't queue it)
	Action  ScheduleAction // which action was taken
	Reply   string         // optional text to send back to the user immediately

	// Queued indicates the message was acknowledged but NOT consumed.
	// The gateway should send Reply as immediate feedback, then let the
	// message continue through the normal queuing path (wait for lock).
	// When Queued is true, Handled MUST be false.
	//
	// Used by Queue: the user gets instant "收到，完成后处理"
	// feedback, and the message is processed normally after the current
	// loop finishes — using the gateway's own response delivery mechanism.
	Queued bool

	// PendingConfirm indicates the scheduler is uncertain and wants user
	// confirmation before acting. The message is NOT consumed and NOT queued
	// — it is held in the CorrectionStore. The gateway must store the
	// Corrections and wait for the user to pick one. If the user doesn't
	// respond before the TTL expires, the message is re-dispatched as a
	// normal queued message (safe fallback).
	//
	// When PendingConfirm is true, both Handled and Queued MUST be false.
	PendingConfirm bool

	// Corrections provides one-click override options for the user when the
	// scheduler's automatic decision may not match their intent. Each option
	// represents an alternative action the user can trigger. The IM gateway
	// renders these as clickable buttons/links with a TTL — they expire
	// automatically when the current task ends or after the TTL elapses.
	Corrections []CorrectionOption
}

// CorrectionOption represents a one-click override that lets the user correct
// the scheduler's automatic decision. For example, if the scheduler chose
// Merge but the user actually wanted to replace the current task, a
// CorrectionOption with Action=ActionReplace and Label="改为打断" is offered.
type CorrectionOption struct {
	Label  string `json:"label"`  // button text shown to user
	Action string `json:"action"` // action to execute if clicked: "merge", "queue", "replace"
}

// NewCorrectionOption creates a CorrectionOption with the action serialized
// as a string for JSON compatibility.
func NewCorrectionOption(label string, action ScheduleAction) CorrectionOption {
	return CorrectionOption{Label: label, Action: action.String()}
}

// ActionFromString parses a ScheduleAction from its string representation.
// Returns ActionMerge and false if the string is not recognized.
func ActionFromString(s string) (ScheduleAction, bool) {
	switch s {
	case "merge":
		return ActionMerge, true
	case "queue":
		return ActionQueue, true
	case "replace":
		return ActionReplace, true
	case "status_query":
		return ActionStatusQuery, true
	default:
		return ActionMerge, false
	}
}

// DefaultCorrectionTTL is the default time-to-live for correction buttons.
const DefaultCorrectionTTL = 120
