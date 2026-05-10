package main

import "strings"

type mcpHealthStatus string

const (
	mcpHealthStatusUnknown     mcpHealthStatus = "unknown"
	mcpHealthStatusHealthy     mcpHealthStatus = "healthy"
	mcpHealthStatusSlow        mcpHealthStatus = "slow"
	mcpHealthStatusUnavailable mcpHealthStatus = "unavailable"
)

func normalizeMCPHealthStatus(status string) mcpHealthStatus {
	switch mcpHealthStatus(strings.ToLower(strings.TrimSpace(status))) {
	case mcpHealthStatusHealthy:
		return mcpHealthStatusHealthy
	case mcpHealthStatusSlow:
		return mcpHealthStatusSlow
	case mcpHealthStatusUnavailable:
		return mcpHealthStatusUnavailable
	case mcpHealthStatusUnknown:
		return mcpHealthStatusUnknown
	default:
		return mcpHealthStatusUnknown
	}
}
