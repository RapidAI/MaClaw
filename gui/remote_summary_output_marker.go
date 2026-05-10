package main

import "strings"

type remoteSummaryOutputMarkerKind string

const (
	remoteSummaryOutputMarkerUnknown remoteSummaryOutputMarkerKind = ""
	remoteSummaryOutputMarkerBusy    remoteSummaryOutputMarkerKind = "busy"
	remoteSummaryOutputMarkerWaiting remoteSummaryOutputMarkerKind = "waiting"
)

func classifyRemoteSummaryOutputMarker(joinedLower string) remoteSummaryOutputMarkerKind {
	if containsRemoteSummaryWaitingHint(joinedLower) {
		return remoteSummaryOutputMarkerWaiting
	}
	isReading := strings.Contains(joinedLower, "reading") && !strings.Contains(joinedLower, "reading prompt from stdin")
	if strings.Contains(joinedLower, "running") || isReading || strings.Contains(joinedLower, "editing") {
		return remoteSummaryOutputMarkerBusy
	}
	return remoteSummaryOutputMarkerUnknown
}

func containsRemoteSummaryWaitingHint(joinedLower string) bool {
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
