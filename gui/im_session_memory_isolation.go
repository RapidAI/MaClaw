package main

import "strings"

// isIsolatedAssistantSessionUserID identifies assistant conversations whose
// working context must never be mixed with another active conversation. Their
// only permitted cross-session input is a completed, archived experience
// selected explicitly by the memory layer.
func isIsolatedAssistantSessionUserID(userID string) bool {
	userID = strings.TrimSpace(userID)
	return isLansengerGroupConversationUserID(userID) ||
		isProjectTabUserID(userID) ||
		isACPAssistantSessionUserID(userID) ||
		expertIDFromUserID(userID) != ""
}
