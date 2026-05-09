package main

import "strings"

func looksLikePendingUserReplyPromptCandidate(assistantText string) bool {
	text := strings.TrimSpace(strings.ToLower(assistantText))
	if text == "" {
		return false
	}
	doneHints := []string{"task completed", "completed", "done", "let me know if you need anything else"}
	for _, hint := range doneHints {
		if strings.Contains(text, hint) {
			return false
		}
	}
	if strings.Contains(text, "?") || strings.Contains(text, "？") {
		return true
	}
	pendingHints := []string{"please confirm", "confirm before", "which ", "what ", "choose", "approve", "provide", "select"}
	for _, hint := range pendingHints {
		if strings.Contains(text, hint) {
			return true
		}
	}
	return false
}
