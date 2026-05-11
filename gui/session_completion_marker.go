package main

import "strings"

const geminiACPTurnCompletePrefix = "[gemini-acp] turn complete:"

type sessionCompletionMarkerKind string

const (
	sessionCompletionMarkerUnknown    sessionCompletionMarkerKind = ""
	sessionCompletionMarkerCompleted  sessionCompletionMarkerKind = "completed"
	sessionCompletionMarkerIncomplete sessionCompletionMarkerKind = "incomplete"
)

func classifyGeminiACPTurnCompleteMarker(line string) sessionCompletionMarkerKind {
	lower := strings.ToLower(line)
	if !isGeminiACPTurnCompleteLine(lower) {
		return sessionCompletionMarkerUnknown
	}
	restLower := strings.TrimSpace(lower[len(geminiACPTurnCompletePrefix):])
	for _, token := range sessionCompletionMarkerTokens(restLower) {
		switch token {
		case "success", "succeeded", "successful", "done", "complete", "completed":
			return sessionCompletionMarkerCompleted
		case "cancelled", "canceled", "cancel":
			return sessionCompletionMarkerIncomplete
		}
	}
	return sessionCompletionMarkerUnknown
}

func isGeminiACPTurnCompleteLine(line string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), geminiACPTurnCompletePrefix)
}

func sessionCompletionMarkerTokens(value string) []string {
	return strings.FieldsFunc(strings.ToLower(strings.TrimSpace(value)), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
}
