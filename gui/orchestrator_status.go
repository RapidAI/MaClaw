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

func orchestratorTaskStatusIsResumable(status string) bool {
	switch normalizeOrchestratorTaskStatus(status) {
	case orchestratorTaskStatusFailed, orchestratorTaskStatusRunning:
		return true
	default:
		return false
	}
}

func orchestratorTaskStatusIsActive(status string) bool {
	switch normalizeOrchestratorTaskStatus(status) {
	case orchestratorTaskStatusPending, orchestratorTaskStatusRunning:
		return true
	default:
		return false
	}
}
