package guiapp

import "strings"

type mcpHealthStatus string

const (
	mcpHealthStatusUnknown     mcpHealthStatus = "unknown"
	mcpHealthStatusHealthy     mcpHealthStatus = "healthy"
	mcpHealthStatusSlow        mcpHealthStatus = "slow"
	mcpHealthStatusDegraded    mcpHealthStatus = "degraded"
	mcpHealthStatusUnavailable mcpHealthStatus = "unavailable"
)

func normalizeMCPHealthStatus(status mcpHealthStatus) mcpHealthStatus {
	switch mcpHealthStatus(strings.ToLower(strings.TrimSpace(status.String()))) {
	case mcpHealthStatusHealthy:
		return mcpHealthStatusHealthy
	case mcpHealthStatusSlow:
		return mcpHealthStatusSlow
	case mcpHealthStatusDegraded:
		return mcpHealthStatusDegraded
	case mcpHealthStatusUnavailable:
		return mcpHealthStatusUnavailable
	case mcpHealthStatusUnknown:
		return mcpHealthStatusUnknown
	default:
		return mcpHealthStatusUnknown
	}
}

// mcpHealthObservationSucceeded is the single definition of a tools/list
// probe that completed. slow is that success above the latency threshold.
// degraded is a later failed probe that has not yet reached unavailable;
// its cached tool list is not a current observation and must not be planned.
func mcpHealthObservationSucceeded(status mcpHealthStatus) bool {
	switch normalizeMCPHealthStatus(status) {
	case mcpHealthStatusHealthy, mcpHealthStatusSlow:
		return true
	default:
		return false
	}
}

func (status mcpHealthStatus) String() string {
	return string(status)
}
