package main

import "strings"

type imMessageLifecycle struct {
	Trimmed string
	Cleanup func()
}

func (h *IMMessageHandler) beginIMMessageLifecycle(msg IMUserMessage, result **IMAgentResponse) imMessageLifecycle {
	cleanupFns := make([]func(), 0, 3)
	trimmed := strings.TrimSpace(msg.Text)
	if strings.TrimSpace(msg.UserID) != "" {
		cleanupFns = append(cleanupFns, func() {
			h.suppressPendingUserReplyUpdate.Delete(msg.UserID)
		})
	}
	if finalizeAudit := h.imAuditFinalizer(msg, trimmed, result); finalizeAudit != nil {
		cleanupFns = append(cleanupFns, finalizeAudit)
	}
	if uic := h.getUnifiedClassifier(); uic != nil {
		cleanupFns = append(cleanupFns, uic.InvalidateCache)
	}
	return imMessageLifecycle{
		Trimmed: trimmed,
		Cleanup: func() {
			for i := len(cleanupFns) - 1; i >= 0; i-- {
				cleanupFns[i]()
			}
		},
	}
}
