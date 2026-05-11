package main

import (
	"fmt"
	"log"
	"time"
)

func (h *IMMessageHandler) captureScreenshotForDisplay(displayIndex int) string {
	if h.manager == nil {
		return "会话管理器未初始化"
	}
	captureStart := time.Now()
	base64Data, err := h.manager.CaptureScreenshotDirectForDisplay(displayIndex)
	log.Printf("[screenshot] CaptureScreenshotDirectForDisplay(%d) took %v, data_len=%d, err=%v",
		displayIndex, time.Since(captureStart), len(base64Data), err)
	if err != nil {
		return fmt.Sprintf("截取显示器 %d 失败: %s", displayIndex, err.Error())
	}
	h.lastScreenshotAt = time.Now()
	return formatScreenshotBase64Result(base64Data)
}

func (h *IMMessageHandler) captureDirectScreenshotResult() string {
	captureStart := time.Now()
	base64Data, err := h.manager.CaptureScreenshotDirect()
	log.Printf("[screenshot] CaptureScreenshotDirect took %v, data_len=%d, err=%v", time.Since(captureStart), len(base64Data), err)
	if err != nil {
		return fmt.Sprintf("截图失败: %s", err.Error())
	}
	h.lastScreenshotAt = time.Now()
	return formatScreenshotBase64Result(base64Data)
}

func (h *IMMessageHandler) captureSessionScreenshotResult(sessionID string) string {
	if h.shouldReturnScreenshotBase64ForCurrentPlatform() {
		captureStart := time.Now()
		base64Data, err := h.manager.CaptureScreenshotToBase64(sessionID)
		log.Printf("[screenshot] CaptureScreenshotToBase64 took %v, data_len=%d, err=%v", time.Since(captureStart), len(base64Data), err)
		if err != nil {
			return fmt.Sprintf("截图失败: %s", err.Error())
		}
		h.lastScreenshotAt = time.Now()
		return formatScreenshotBase64Result(base64Data)
	}

	if err := h.manager.CaptureScreenshot(sessionID); err != nil {
		return fmt.Sprintf("截图失败: %s", err.Error())
	}
	h.lastScreenshotAt = time.Now()
	return "[screenshot_sent]"
}

func (h *IMMessageHandler) shouldReturnScreenshotBase64ForCurrentPlatform() bool {
	platform := ""
	if h != nil && h.currentLoopCtx != nil {
		platform = h.currentLoopCtx.Platform
	}
	return !normalizeIMMessagePlatformKind(platform).IsDesktopPlaybackTarget()
}
