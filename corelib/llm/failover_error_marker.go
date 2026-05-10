package llm

import "strings"

type failoverErrorMarker int

const (
	failoverErrorMarkerNetwork failoverErrorMarker = iota
	failoverErrorMarkerRateLimit
	failoverErrorMarkerAuth
	failoverErrorMarkerServer
	failoverErrorMarkerTimeout
)

func classifyFailoverErrorMarker(err error) failoverErrorMarker {
	if err == nil {
		return failoverErrorMarkerNetwork
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "429") || strings.Contains(s, "rate limit") || strings.Contains(s, "Too Many Requests"):
		return failoverErrorMarkerRateLimit
	case strings.Contains(s, "401") || strings.Contains(s, "403") || strings.Contains(s, "Unauthorized") || strings.Contains(s, "Forbidden"):
		return failoverErrorMarkerAuth
	case strings.Contains(s, "500") || strings.Contains(s, "502") || strings.Contains(s, "503") || strings.Contains(s, "504"):
		return failoverErrorMarkerServer
	case strings.Contains(s, "timeout") || strings.Contains(s, "deadline exceeded"):
		return failoverErrorMarkerTimeout
	default:
		return failoverErrorMarkerNetwork
	}
}

func (m failoverErrorMarker) Reason() FailoverReason {
	switch m {
	case failoverErrorMarkerRateLimit:
		return FailoverRateLimit
	case failoverErrorMarkerAuth:
		return FailoverAuthError
	case failoverErrorMarkerServer:
		return FailoverServerError
	case failoverErrorMarkerTimeout:
		return FailoverTimeout
	default:
		return FailoverNetwork
	}
}
