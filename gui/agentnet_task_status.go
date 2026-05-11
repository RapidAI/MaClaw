package main

import "strings"

type agentNetTaskStatus string

const (
	agentNetTaskStatusUnknown   agentNetTaskStatus = ""
	agentNetTaskStatusOpen      agentNetTaskStatus = "open"
	agentNetTaskStatusCreated   agentNetTaskStatus = "created"
	agentNetTaskStatusAccepted  agentNetTaskStatus = "accepted"
	agentNetTaskStatusRejected  agentNetTaskStatus = "rejected"
	agentNetTaskStatusCancelled agentNetTaskStatus = "cancelled"
	agentNetTaskStatusSettled   agentNetTaskStatus = "settled"
)

func normalizeAgentNetTaskStatus(status agentNetTaskStatus) agentNetTaskStatus {
	switch agentNetTaskStatus(strings.ToLower(strings.TrimSpace(status.String()))) {
	case agentNetTaskStatusOpen:
		return agentNetTaskStatusOpen
	case agentNetTaskStatusCreated:
		return agentNetTaskStatusCreated
	case agentNetTaskStatusAccepted:
		return agentNetTaskStatusAccepted
	case agentNetTaskStatusRejected:
		return agentNetTaskStatusRejected
	case agentNetTaskStatusCancelled:
		return agentNetTaskStatusCancelled
	case agentNetTaskStatusSettled:
		return agentNetTaskStatusSettled
	default:
		return agentNetTaskStatusUnknown
	}
}

func (status agentNetTaskStatus) String() string {
	return string(status)
}
