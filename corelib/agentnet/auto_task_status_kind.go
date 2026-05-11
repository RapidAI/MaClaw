package agentnet

import "strings"

type autoRunStatus string

const (
	autoRunStatusUnknown    autoRunStatus = ""
	autoRunStatusClaiming   autoRunStatus = "claiming"
	autoRunStatusExecuting  autoRunStatus = "executing"
	autoRunStatusSubmitting autoRunStatus = "submitting"
	autoRunStatusDone       autoRunStatus = "done"
	autoRunStatusFailed     autoRunStatus = "failed"
)

func normalizeAutoRunStatus(status string) autoRunStatus {
	switch autoRunStatus(strings.ToLower(strings.TrimSpace(status))) {
	case autoRunStatusClaiming:
		return autoRunStatusClaiming
	case autoRunStatusExecuting:
		return autoRunStatusExecuting
	case autoRunStatusSubmitting:
		return autoRunStatusSubmitting
	case autoRunStatusDone:
		return autoRunStatusDone
	case autoRunStatusFailed:
		return autoRunStatusFailed
	default:
		return autoRunStatusUnknown
	}
}

func (s autoRunStatus) String() string {
	return string(s)
}

func (s autoRunStatus) IsActive() bool {
	switch s {
	case autoRunStatusClaiming, autoRunStatusExecuting, autoRunStatusSubmitting:
		return true
	default:
		return false
	}
}

type autoPollStatus string

const (
	autoPollStatusUnknown         autoPollStatus = ""
	autoPollStatusBusy            autoPollStatus = "busy"
	autoPollStatusChecking        autoPollStatus = "checking"
	autoPollStatusOffline         autoPollStatus = "offline"
	autoPollStatusNoTasks         autoPollStatus = "no_tasks"
	autoPollStatusNoMatchingTasks autoPollStatus = "no_matching_tasks"
	autoPollStatusPicked          autoPollStatus = "picked"
	autoPollStatusError           autoPollStatus = "error"
)

func (s autoPollStatus) String() string {
	return string(s)
}

type taskAvailabilityStatus string

const (
	taskAvailabilityUnknown taskAvailabilityStatus = ""
	taskAvailabilityOpen    taskAvailabilityStatus = "open"
	taskAvailabilityCreated taskAvailabilityStatus = "created"
	taskAvailabilitySettled taskAvailabilityStatus = "settled"
)

func normalizeTaskAvailabilityStatus(status TaskStateStatus) taskAvailabilityStatus {
	switch NormalizeTaskStateStatus(status) {
	case TaskStateStatusOpen:
		return taskAvailabilityOpen
	case TaskStateStatusCreated:
		return taskAvailabilityCreated
	case TaskStateStatusSettled:
		return taskAvailabilitySettled
	default:
		return taskAvailabilityUnknown
	}
}

func (s taskAvailabilityStatus) CanPick() bool {
	switch s {
	case taskAvailabilityOpen, taskAvailabilityCreated, taskAvailabilitySettled, taskAvailabilityUnknown:
		return true
	default:
		return false
	}
}
