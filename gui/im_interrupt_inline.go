package main

import "strings"

func (h *IMMessageHandler) shouldTryInlineInterrupt(msg IMUserMessage) bool {
	if h == nil || h.interruptHandler == nil || h.currentLoopCtx == nil {
		return false
	}
	if strings.TrimSpace(msg.UserID) == "" {
		return false
	}
	if h.currentLoopCtx.IsCancelled() {
		return false
	}
	return true
}
