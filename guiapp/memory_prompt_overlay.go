package guiapp

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func (h *IMMessageHandler) appendCurrentMemoryFacts(b *strings.Builder, userID string) {
	if h == nil || h.currentMemoryStore() == nil || b == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		userID = desktopUserID
	}
	strictOwner := isIsolatedAssistantSessionUserID(userID)
	facts := h.currentMemoryStore().UserFactSummaryForPrompt(memory.UserFactSummaryPromptOptions{
		Header:      "用户信息: ",
		MaxRunes:    400,
		OwnerID:     userID,
		StrictOwner: strictOwner,
	})
	b.WriteString(facts)
	preferences := h.currentMemoryStore().UserPreferenceSummaryForPrompt(memory.UserPreferenceSummaryPromptOptions{
		Header:      "用户偏好与指令: ",
		MaxRunes:    600,
		OwnerID:     userID,
		StrictOwner: strictOwner,
	})
	b.WriteString(preferences)
}
