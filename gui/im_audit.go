package main

import "strings"

func (h *IMMessageHandler) imAuditFinalizer(msg IMUserMessage, trimmed string, result **IMAgentResponse) func() {
	platform := normalizeIMMessagePlatformKind(msg.Platform)
	// Third-party gateways intentionally stay out of imMessagePlatformKind: adding
	// them there would also change media/voice routing. They are nevertheless an
	// auditable IM family, including the bare "thirdparty" value relayed by Hub.
	isThirdParty := normalizeIMAuditPlatformKind(msg.Platform).IsThirdParty()
	if h == nil || h.app == nil || (platform.IsDesktopPlaybackTarget() && !isThirdParty) || (trimmed == "" && len(msg.Attachments) == 0 && !msg.SkipUserAudit) {
		return nil
	}
	return func() {
		store := h.app.getIMAuditStore()
		if store == nil {
			return
		}
		if !msg.SkipUserAudit {
			h.writeIMAuditMessage(store, IMAuditMessage{
				UserID: msg.UserID, Platform: msg.Platform, Role: "user", Content: imAuditUserContent(msg),
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
		h.writeIMAuditMessage(store, IMAuditMessage{
			UserID:   msg.UserID,
			Platform: msg.Platform,
			Role:     "assistant",
			Content:  content,
		})
	}
}

func imAuditUserContent(msg IMUserMessage) string {
	if strings.TrimSpace(msg.Text) != "" || len(msg.Attachments) == 0 {
		return msg.Text
	}
	labels := make([]string, 0, len(msg.Attachments))
	for _, attachment := range msg.Attachments {
		labels = append(labels, imAuditMediaPlaceholder(attachment.Type, attachment.FileName))
	}
	return strings.Join(labels, "\n")
}

func imAuditMediaPlaceholder(kind, fileName string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "attachment"
	}
	fileName = strings.TrimSpace(fileName)
	if fileName == "" {
		return "[" + kind + "]"
	}
	return "[" + kind + ": " + fileName + "]"
}

func (h *IMMessageHandler) writeIMAuditMessage(store *IMAuditStore, msg IMAuditMessage) {
	if store == nil {
		return
	}
	if normalizeIMAuditPlatformKind(msg.Platform).IsThirdParty() {
		// Device history is an operator-facing audit trail. A short bounded wait
		// is preferable to silently losing ESP32 turns when the normal queue bursts.
		store.WriteCritical(msg)
		return
	}
	store.Write(msg)
}
