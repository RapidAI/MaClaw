package main

import "strings"

type sessionOutputMarkerKind int

const (
	sessionOutputMarkerNone sessionOutputMarkerKind = iota
	sessionOutputMarkerAPIRetry
	sessionOutputMarkerAPIError
)

func (k sessionOutputMarkerKind) IsAPIRetry() bool {
	return k == sessionOutputMarkerAPIRetry
}

func (k sessionOutputMarkerKind) IsTransientAPIIssue() bool {
	return k == sessionOutputMarkerAPIRetry || k == sessionOutputMarkerAPIError
}

func classifySessionOutputMarker(line string) sessionOutputMarkerKind {
	switch {
	case strings.Contains(line, "API retry") || strings.Contains(line, "api_retry"):
		return sessionOutputMarkerAPIRetry
	case strings.Contains(line, "❌"):
		return sessionOutputMarkerAPIError
	default:
		return sessionOutputMarkerNone
	}
}

func recentSessionOutputHasMarker(lines []string, maxLines int, accepts func(sessionOutputMarkerKind) bool) bool {
	if maxLines <= 0 || accepts == nil {
		return false
	}
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-maxLines; i-- {
		if accepts(classifySessionOutputMarker(lines[i])) {
			return true
		}
	}
	return false
}
