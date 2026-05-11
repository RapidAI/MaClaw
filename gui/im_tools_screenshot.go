package main

import "time"

func (h *IMMessageHandler) toolScreenshot(args map[string]interface{}) string {
	if msg := screenshotLocalImagePathGuardMessage(h.lastUserText); msg != "" {
		return msg
	}

	if msg := screenshotCooldownMessage(h.lastScreenshotAt, time.Now()); msg != "" {
		return msg
	}

	if displayRaw, ok := args["display"]; ok {
		displayIndex, err := parseScreenshotDisplayIndex(displayRaw)
		if err != nil {
			return err.Error()
		}
		return h.captureScreenshotForDisplay(displayIndex)
	}

	sessionID, _ := args["session_id"].(string)

	if h.manager != nil {
		selection := selectScreenshotSession(sessionID, h.manager.List())
		switch selection.Kind {
		case screenshotSessionSelectionSelected:
			sessionID = selection.SessionID
		case screenshotSessionSelectionMultiple:
			return selection.Message
		case screenshotSessionSelectionNone:
			return h.captureDirectScreenshotResult()
		}
	}

	if sessionID == "" {
		return "缺少 session_id 参数，且无法自动选择会话"
	}
	if h.manager == nil {
		return "会话管理器未初始化"
	}

	return h.captureSessionScreenshotResult(sessionID)
}
