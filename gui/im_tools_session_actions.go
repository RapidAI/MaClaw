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
	return disabledExternalCodingSessionToolText("send_and_observe")
}

// toolControlSession rejects legacy external coding-session control.
func (h *IMMessageHandler) toolControlSession(args map[string]interface{}) string {
	return disabledExternalCodingSessionToolText("control_session")
}
