package agentservice

import "time"

type MCPHealthStatus string

const (
	MCPHealthUnknown     MCPHealthStatus = "unknown"
	MCPHealthStopped     MCPHealthStatus = "stopped"
	MCPHealthRunning     MCPHealthStatus = "running"
	MCPHealthDisabled    MCPHealthStatus = "disabled"
	MCPHealthHealthy     MCPHealthStatus = "healthy"
	MCPHealthSlow        MCPHealthStatus = "slow"
	MCPHealthUnavailable MCPHealthStatus = "unavailable"
)

func (s MCPHealthStatus) String() string {
	if s == "" {
		return string(MCPHealthUnknown)
	}
	return string(s)
}

func normalizeMCPHealthStatus(s MCPHealthStatus) MCPHealthStatus {
	if s == "" {
		return MCPHealthUnknown
	}
	return s
}

const remoteMCPSlowThreshold = 5 * time.Second

func remoteMCPHealthStatus(elapsed time.Duration) MCPHealthStatus {
	if elapsed > remoteMCPSlowThreshold {
		return MCPHealthSlow
	}
	return MCPHealthHealthy
}
