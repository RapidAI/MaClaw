package main

import "strings"

type remoteSessionOutputMarkerKind int

const (
	remoteSessionOutputMarkerNone remoteSessionOutputMarkerKind = iota
	remoteSessionOutputMarkerReadingPromptStdin
	remoteSessionOutputMarkerGeminiPromptError
	remoteSessionOutputMarkerGeminiSessionError
)

func classifyRemoteSessionOutputMarker(text string) remoteSessionOutputMarkerKind {
	trimmed := strings.TrimSpace(text)
	switch {
	case strings.Contains(text, "Reading prompt from stdin"):
		return remoteSessionOutputMarkerReadingPromptStdin
	case strings.HasPrefix(trimmed, "[gemini-acp] prompt error:"):
		return remoteSessionOutputMarkerGeminiPromptError
	case strings.HasPrefix(trimmed, "[gemini-acp] session error:"):
		return remoteSessionOutputMarkerGeminiSessionError
	default:
		return remoteSessionOutputMarkerNone
	}
}
