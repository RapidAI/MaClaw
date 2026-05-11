package main

import "strings"

type agentViewStepStatusKind string

const (
	agentViewStepStatusPending agentViewStepStatusKind = "pending"
	agentViewStepStatusRunning agentViewStepStatusKind = "running"
	agentViewStepStatusDone    agentViewStepStatusKind = "done"
	agentViewStepStatusError   agentViewStepStatusKind = "error"
	agentViewStepStatusSuccess agentViewStepStatusKind = "success"
	agentViewStepStatusFailed  agentViewStepStatusKind = "failed"
	agentViewStepStatusTimeout agentViewStepStatusKind = "timeout"
	agentViewStepStatusFailure agentViewStepStatusKind = "failure"
	agentViewStepStatusDoneAlt agentViewStepStatusKind = "completed"
)

func normalizeAgentViewStepStatus(status string) agentViewStepStatusKind {
	return normalizeAgentViewSkillStepStatus(normalizeSkillStepStatus(status), status)
}

func normalizeAgentViewSkillStepStatus(status skillStepStatus, fallback string) agentViewStepStatusKind {
	switch status.Normalized() {
	case skillStepStatusSuccess:
		return agentViewStepStatusDone
	case skillStepStatusRunning:
		return agentViewStepStatusRunning
	case skillStepStatusFailed, skillStepStatusTimeout:
		return agentViewStepStatusError
	}
	switch agentViewStepStatusKind(strings.ToLower(strings.TrimSpace(fallback))) {
	case agentViewStepStatusDone, agentViewStepStatusDoneAlt:
		return agentViewStepStatusDone
	case agentViewStepStatusError, agentViewStepStatusFailure:
		return agentViewStepStatusError
	default:
		return agentViewStepStatusPending
	}
}
