package main

import "fmt"

func (h *IMMessageHandler) toolSendInput(args map[string]interface{}) string {
	return "[system rejected] send_input is disabled for external coding sessions. Coding tasks must run through the internal CodingSubAgent."
}

func (h *IMMessageHandler) toolGetSessionOutput(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return "缺少 session_id 参数"
	}
	if h.manager == nil {
		return "会话管理器未初始化"
	}
	session, ok := h.manager.Get(sessionID)
	if !ok {
		return fmt.Sprintf("会话 %s 不存在", sessionID)
	}

	maxLines := sessionOutputLineLimit(args)
	waitForSessionStartupOutput(session)

	snapshot := snapshotSessionOutput(session)
	hintFacts := collectSessionOutputHintFacts(session, snapshot.Status, snapshot.RawLines)
	return renderSessionOutput(sessionID, maxLines, snapshot, hintFacts)
}

func (h *IMMessageHandler) toolGetSessionEvents(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return "缺少 session_id 参数"
	}
	if h.manager == nil {
		return "会话管理器未初始化"
	}
	session, ok := h.manager.Get(sessionID)
	if !ok {
		return fmt.Sprintf("会话 %s 不存在", sessionID)
	}
	return renderSessionEvents(sessionID, snapshotSessionEvents(session))
}
