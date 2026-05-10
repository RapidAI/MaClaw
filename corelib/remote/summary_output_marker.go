package remote

import "strings"

type summaryOutputMarkerKind int

const (
	summaryOutputMarkerUnknown summaryOutputMarkerKind = iota
	summaryOutputMarkerBusy
	summaryOutputMarkerWaiting
)

func classifySummaryOutputMarker(joinedLower string) summaryOutputMarkerKind {
	if containsSummaryWaitingHint(joinedLower) {
		return summaryOutputMarkerWaiting
	}
	isReading := strings.Contains(joinedLower, "reading") && !strings.Contains(joinedLower, "reading prompt from stdin")
	if strings.Contains(joinedLower, "running") || isReading || strings.Contains(joinedLower, "editing") {
		return summaryOutputMarkerBusy
	}
	return summaryOutputMarkerUnknown
}

func containsSummaryWaitingHint(joinedLower string) bool {
	waitingHints := []string{
		"what would you like",
		"what do you want",
		"how can i help",
		"what should i do",
		"waiting for",
		"your turn",
		"enter a command",
		"type a message",
		"send a message",
		"reading prompt from stdin",
	}
	for _, hint := range waitingHints {
		if strings.Contains(joinedLower, hint) {
			return true
		}
	}
	return false
}
