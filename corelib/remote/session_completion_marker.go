package remote

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
	if !strings.HasPrefix(lower, geminiACPTurnCompletePrefix) {
		return sessionCompletionMarkerUnknown
	}
	rest := strings.TrimSpace(lower[len(geminiACPTurnCompletePrefix):])
	switch {
	case strings.Contains(rest, "success") || strings.Contains(rest, "done") || strings.Contains(rest, "completed"):
		return sessionCompletionMarkerCompleted
	case strings.Contains(rest, "cancelled") || strings.Contains(rest, "canceled"):
		return sessionCompletionMarkerIncomplete
	default:
		return sessionCompletionMarkerUnknown
	}
}
