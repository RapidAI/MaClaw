package main

func (h *IMMessageHandler) imAuditFinalizer(msg IMUserMessage, trimmed string, result **IMAgentResponse) func() {
	platform := normalizeIMMessagePlatformKind(msg.Platform)
	if h == nil || h.app == nil || platform.IsDesktopPlaybackTarget() || (trimmed == "" && !msg.SkipUserAudit) {
		return nil
	}
	return func() {
		store := h.app.getIMAuditStore()
		if store == nil {
			return
		}
		if !msg.SkipUserAudit {
			store.Write(IMAuditMessage{
				UserID: msg.UserID, Platform: msg.Platform, Role: "user", Content: msg.Text,
			})
		}
		if result == nil || *result == nil {
			return
		}
		content := (*result).Text
		if content == "" {
			content = (*result).Error
		}
		if content == "" {
			return
		}
		store.Write(IMAuditMessage{
			UserID:   msg.UserID,
			Platform: msg.Platform,
			Role:     "assistant",
			Content:  content,
		})
	}
}
