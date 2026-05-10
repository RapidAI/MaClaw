package misc

import "strings"

type taskOrchestratorStatus string

const (
	taskOrchestratorStatusUnknown   taskOrchestratorStatus = ""
	taskOrchestratorStatusPlanning  taskOrchestratorStatus = "planning"
	taskOrchestratorStatusPending   taskOrchestratorStatus = "pending"
	taskOrchestratorStatusRunning   taskOrchestratorStatus = "running"
	taskOrchestratorStatusCompleted taskOrchestratorStatus = "completed"
	taskOrchestratorStatusFailed    taskOrchestratorStatus = "failed"
	taskOrchestratorStatusCancelled taskOrchestratorStatus = "cancelled"
)

func normalizeTaskOrchestratorStatus(status string) taskOrchestratorStatus {
	switch taskOrchestratorStatus(strings.TrimSpace(status)) {
	case taskOrchestratorStatusPlanning:
		return taskOrchestratorStatusPlanning
	case taskOrchestratorStatusPending:
		return taskOrchestratorStatusPending
	case taskOrchestratorStatusRunning:
		return taskOrchestratorStatusRunning
	case taskOrchestratorStatusCompleted:
		return taskOrchestratorStatusCompleted
	case taskOrchestratorStatusFailed:
		return taskOrchestratorStatusFailed
	case taskOrchestratorStatusCancelled:
		return taskOrchestratorStatusCancelled
	default:
		return taskOrchestratorStatusUnknown
	}
}

func (s taskOrchestratorStatus) String() string {
	return string(s)
}

func (s taskOrchestratorStatus) IsCompleted() bool {
	return s == taskOrchestratorStatusCompleted
}

func (s taskOrchestratorStatus) IsPending() bool {
	return s == taskOrchestratorStatusPending
}

func (s taskOrchestratorStatus) IsRunning() bool {
	return s == taskOrchestratorStatusRunning
}

func (s taskOrchestratorStatus) IsActive() bool {
	return s.IsPending() || s.IsRunning()
}

func (s taskOrchestratorStatus) IsResumablePlan() bool {
	return s == taskOrchestratorStatusFailed || s == taskOrchestratorStatusRunning
}
