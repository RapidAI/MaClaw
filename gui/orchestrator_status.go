package main

import "strings"

type orchestratorSessionStatus string

const (
	orchestratorSessionStatusUnknown orchestratorSessionStatus = ""
	orchestratorSessionStatusSuccess orchestratorSessionStatus = "success"
	orchestratorSessionStatusFailed  orchestratorSessionStatus = "failed"
)

func normalizeOrchestratorSessionStatus(status string) orchestratorSessionStatus {
	switch orchestratorSessionStatus(strings.TrimSpace(status)) {
	case orchestratorSessionStatusSuccess:
		return orchestratorSessionStatusSuccess
	case orchestratorSessionStatusFailed:
		return orchestratorSessionStatusFailed
	default:
		return orchestratorSessionStatusUnknown
	}
}

func (s orchestratorSessionStatus) String() string {
	return string(s)
}

func (s orchestratorSessionStatus) Normalized() orchestratorSessionStatus {
	switch s {
	case orchestratorSessionStatusSuccess, orchestratorSessionStatusFailed:
		return s
	default:
		return orchestratorSessionStatusUnknown
	}
}

func (s orchestratorSessionStatus) IsFailed() bool {
	return s.Normalized() == orchestratorSessionStatusFailed
}

type orchestratorTaskStatus string

const (
	orchestratorTaskStatusPlanning       orchestratorTaskStatus = "planning"
	orchestratorTaskStatusPending        orchestratorTaskStatus = "pending"
	orchestratorTaskStatusRunning        orchestratorTaskStatus = "running"
	orchestratorTaskStatusCompleted      orchestratorTaskStatus = "completed"
	orchestratorTaskStatusFailed         orchestratorTaskStatus = "failed"
	orchestratorTaskStatusCancelled      orchestratorTaskStatus = "cancelled"
	orchestratorTaskStatusPartialFailure orchestratorTaskStatus = "partial_failure"
)

func normalizeOrchestratorTaskStatus(status string) orchestratorTaskStatus {
	switch orchestratorTaskStatus(strings.TrimSpace(status)) {
	case orchestratorTaskStatusPlanning:
		return orchestratorTaskStatusPlanning
	case orchestratorTaskStatusPending:
		return orchestratorTaskStatusPending
	case orchestratorTaskStatusRunning:
		return orchestratorTaskStatusRunning
	case orchestratorTaskStatusCompleted:
		return orchestratorTaskStatusCompleted
	case orchestratorTaskStatusFailed:
		return orchestratorTaskStatusFailed
	case orchestratorTaskStatusCancelled:
		return orchestratorTaskStatusCancelled
	case orchestratorTaskStatusPartialFailure:
		return orchestratorTaskStatusPartialFailure
	default:
		return ""
	}
}

func (s orchestratorTaskStatus) String() string {
	return string(s)
}

func (s orchestratorTaskStatus) Normalized() orchestratorTaskStatus {
	switch s {
	case orchestratorTaskStatusPlanning, orchestratorTaskStatusPending, orchestratorTaskStatusRunning, orchestratorTaskStatusCompleted, orchestratorTaskStatusFailed, orchestratorTaskStatusCancelled, orchestratorTaskStatusPartialFailure:
		return s
	default:
		return ""
	}
}

func (s orchestratorTaskStatus) IsResumable() bool {
	switch s.Normalized() {
	case orchestratorTaskStatusFailed, orchestratorTaskStatusRunning:
		return true
	default:
		return false
	}
}

func (s orchestratorTaskStatus) IsActive() bool {
	switch s.Normalized() {
	case orchestratorTaskStatusPending, orchestratorTaskStatusRunning:
		return true
	default:
		return false
	}
}

func (s orchestratorTaskStatus) IsCompleted() bool {
	return s.Normalized() == orchestratorTaskStatusCompleted
}

func (s orchestratorTaskStatus) IsPending() bool {
	return s.Normalized() == orchestratorTaskStatusPending
}

func (s orchestratorTaskStatus) IsRunning() bool {
	return s.Normalized() == orchestratorTaskStatusRunning
}
