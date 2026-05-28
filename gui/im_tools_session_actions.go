package main

func (h *IMMessageHandler) toolInterruptSession(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	return runSessionControlAction(h.manager, sessionID, sessionControlActionInterrupt)
}

func (h *IMMessageHandler) toolKillSession(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	return runSessionControlAction(h.manager, sessionID, sessionControlActionKill)
}

// toolSendAndObserve rejects legacy external coding-session follow-up.
func (h *IMMessageHandler) toolSendAndObserve(args map[string]interface{}) string {
	return "[system rejected] send_and_observe is disabled for external coding sessions. Coding tasks must run through the internal CodingSubAgent."
}

// toolControlSession merges interrupt_session and kill_session into one tool.
func (h *IMMessageHandler) toolControlSession(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	actionText, _ := args["action"].(string)
	action := normalizeSessionControlAction(actionText)
	return runSessionControlAction(h.manager, sessionID, action)
}
