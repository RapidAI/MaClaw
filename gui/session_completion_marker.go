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
	switch {
	case strings.Contains(restLower, "success") || strings.Contains(restLower, "done") || strings.Contains(restLower, "completed"):
		return sessionCompletionMarkerCompleted
	case strings.Contains(restLower, "cancelled") || strings.Contains(restLower, "canceled"):
		return sessionCompletionMarkerIncomplete
	default:
		return sessionCompletionMarkerUnknown
	}
}

func isGeminiACPTurnCompleteLine(line string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), geminiACPTurnCompletePrefix)
}
