package main

import (
	"strings"
	"time"
)

func (h *IMMessageHandler) toolScreenshot(args map[string]interface{}) string {
	ownerID, hasRuntimeOwner := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	if hasRuntimeOwner && ownerID == "" {
		return "screenshot failed: runtime owner is missing; isolated runtime will not fall back to desktop loop"
	}
	platform := consumeRuntimePlatformFromToolArgs(args)
	if platform == "" {
		platform = h.runtimePlatformForOwnerOrCurrent(ownerID, hasRuntimeOwner)
	}
	taskText := h.runtimeTaskTextForOwner(ownerID)
	if !hasRuntimeOwner {
		taskText, _ = h.currentRuntimeTaskTextOrLegacy()
	}
	if msg := screenshotLocalImagePathGuardMessage(taskText); msg != "" {
		return msg
	}

	cooldownKey, scopedCooldown := h.screenshotCooldownScope(ownerID, hasRuntimeOwner)
	now := time.Now()
	if msg := screenshotCooldownMessage(h.lastScreenshotAtForScope(cooldownKey, scopedCooldown), now); msg != "" {
		return msg
	}
	recordResult := func(result string) string {
		if screenshotCaptureSucceeded(result) {
			h.recordScreenshotAtForScope(cooldownKey, scopedCooldown, now)
		}
		return result
	}

	if displayRaw, ok := args["display"]; ok {
		displayIndex, err := parseScreenshotDisplayIndex(displayRaw)
		if err != nil {
			return err.Error()
		}
		return recordResult(h.captureScreenshotForDisplay(displayIndex))
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
			return recordResult(h.captureDirectScreenshotResult())
		}
	}

	if sessionID == "" {
		return "缺少 session_id 参数，且无法自动选择会话"
	}
	if h.manager == nil {
		return "会话管理器未初始化"
	}

	return recordResult(h.captureSessionScreenshotResultForPlatform(sessionID, platform))
}

func (h *IMMessageHandler) screenshotCooldownScope(ownerID string, hasRuntimeOwner bool) (string, bool) {
	ownerID = strings.TrimSpace(ownerID)
	if hasRuntimeOwner {
		return "owner:" + ownerID, true
	}
	if h == nil {
		return "", false
	}
	return "", false
}

func (h *IMMessageHandler) lastScreenshotAtForScope(key string, scoped bool) time.Time {
	if h == nil || !scoped || strings.TrimSpace(key) == "" {
		if h == nil {
			return time.Time{}
		}
		return h.lastScreenshotAt
	}
	if v, ok := h.screenshotCooldowns.Load(key); ok {
		if ts, ok := v.(time.Time); ok {
			return ts
		}
	}
	return time.Time{}
}

func (h *IMMessageHandler) recordScreenshotAtForScope(key string, scoped bool, ts time.Time) {
	if h == nil {
		return
	}
	if scoped && strings.TrimSpace(key) != "" {
		h.screenshotCooldowns.Store(key, ts)
		return
	}
	h.lastScreenshotAt = ts
}

func screenshotCaptureSucceeded(result string) bool {
	trimmed := strings.TrimSpace(result)
	return strings.HasPrefix(trimmed, "[image_base64|") || strings.HasPrefix(trimmed, "[screenshot_sent]")
}
