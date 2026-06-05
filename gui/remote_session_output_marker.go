package main

import "strings"

type remoteSessionOutputMarkerKind int

const (
	remoteSessionOutputMarkerNone remoteSessionOutputMarkerKind = iota
	remoteSessionOutputMarkerReadingPromptStdin
)

func classifyRemoteSessionOutputMarker(text string) remoteSessionOutputMarkerKind {
	trimmed := strings.TrimSpace(text)
	switch {
	case strings.Contains(text, "Reading prompt from stdin"):
		return remoteSessionOutputMarkerReadingPromptStdin
	default:
		_ = trimmed
		return remoteSessionOutputMarkerNone
	}
}
