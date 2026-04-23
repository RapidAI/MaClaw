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
	Handled bool           // true if the message was processed
	Action  ScheduleAction // which action was taken
	Reply   string         // optional text to send back to the user
}
