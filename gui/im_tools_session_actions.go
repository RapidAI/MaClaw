package main

func (h *IMMessageHandler) toolInterruptSession(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	return runSessionControlAction(h.manager, sessionID, sessionControlActionInterrupt)
}

func (h *IMMessageHandler) toolKillSession(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	return runSessionControlAction(h.manager, sessionID, sessionControlActionKill)
}

// toolSendAndObserve combines send_input + get_session_output into a single
// tool call. It sends text to a session, waits briefly for output to
// accumulate, then returns the session output.
//
// When the TaskExecutionOrchestrator is active, the text is automatically
// enriched with per-task context to keep each session focused on its assigned
// task slice.
func (h *IMMessageHandler) toolSendAndObserve(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	text, _ := args["text"].(string)
	timeoutSeconds, _ := args["timeout_seconds"].(float64)
	text = h.enrichSendAndObserveTextForTask(sessionID, text)

	return SendAndObserveSession(h.manager, sessionID, text, SessionObserveOptions{
		TimeoutSeconds: timeoutSeconds,
		Lines:          40,
	}, func(renderArgs map[string]interface{}) string {
		return h.toolGetSessionOutput(renderArgs)
	})
}

// toolControlSession merges interrupt_session and kill_session into one tool.
func (h *IMMessageHandler) toolControlSession(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	actionText, _ := args["action"].(string)
	action := normalizeSessionControlAction(actionText)
	return runSessionControlAction(h.manager, sessionID, action)
}
