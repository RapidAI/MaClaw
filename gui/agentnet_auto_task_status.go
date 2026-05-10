package main

import "strings"

type agentNetAutoRunStatus string

const (
	agentNetAutoRunStatusUnknown    agentNetAutoRunStatus = ""
	agentNetAutoRunStatusClaiming   agentNetAutoRunStatus = "claiming"
	agentNetAutoRunStatusExecuting  agentNetAutoRunStatus = "executing"
	agentNetAutoRunStatusSubmitting agentNetAutoRunStatus = "submitting"
	agentNetAutoRunStatusDone       agentNetAutoRunStatus = "done"
	agentNetAutoRunStatusFailed     agentNetAutoRunStatus = "failed"
)

func normalizeAgentNetAutoRunStatus(status string) agentNetAutoRunStatus {
	switch agentNetAutoRunStatus(strings.ToLower(strings.TrimSpace(status))) {
	case agentNetAutoRunStatusClaiming:
		return agentNetAutoRunStatusClaiming
	case agentNetAutoRunStatusExecuting:
		return agentNetAutoRunStatusExecuting
	case agentNetAutoRunStatusSubmitting:
		return agentNetAutoRunStatusSubmitting
	case agentNetAutoRunStatusDone:
		return agentNetAutoRunStatusDone
	case agentNetAutoRunStatusFailed:
		return agentNetAutoRunStatusFailed
	default:
		return agentNetAutoRunStatusUnknown
	}
}

func (s agentNetAutoRunStatus) IsActive() bool {
	switch s {
	case agentNetAutoRunStatusClaiming, agentNetAutoRunStatusExecuting, agentNetAutoRunStatusSubmitting:
		return true
	default:
		return false
	}
}

type agentNetAutoPollStatus string

const (
	agentNetAutoPollStatusUnknown         agentNetAutoPollStatus = ""
	agentNetAutoPollStatusBusy            agentNetAutoPollStatus = "busy"
	agentNetAutoPollStatusChecking        agentNetAutoPollStatus = "checking"
	agentNetAutoPollStatusOffline         agentNetAutoPollStatus = "offline"
	agentNetAutoPollStatusNoTasks         agentNetAutoPollStatus = "no_tasks"
	agentNetAutoPollStatusNoMatchingTasks agentNetAutoPollStatus = "no_matching_tasks"
	agentNetAutoPollStatusPicked          agentNetAutoPollStatus = "picked"
	agentNetAutoPollStatusError           agentNetAutoPollStatus = "error"
)

func normalizeAgentNetAutoPollStatus(status string) agentNetAutoPollStatus {
	switch agentNetAutoPollStatus(strings.ToLower(strings.TrimSpace(status))) {
	case agentNetAutoPollStatusBusy:
		return agentNetAutoPollStatusBusy
	case agentNetAutoPollStatusChecking:
		return agentNetAutoPollStatusChecking
	case agentNetAutoPollStatusOffline:
		return agentNetAutoPollStatusOffline
	case agentNetAutoPollStatusNoTasks:
		return agentNetAutoPollStatusNoTasks
	case agentNetAutoPollStatusNoMatchingTasks:
		return agentNetAutoPollStatusNoMatchingTasks
	case agentNetAutoPollStatusPicked:
		return agentNetAutoPollStatusPicked
	case agentNetAutoPollStatusError:
		return agentNetAutoPollStatusError
	default:
		return agentNetAutoPollStatusUnknown
	}
}

type agentNetTaskAvailabilityStatus string

const (
	agentNetTaskAvailabilityUnknown agentNetTaskAvailabilityStatus = ""
	agentNetTaskAvailabilityOpen    agentNetTaskAvailabilityStatus = "open"
	agentNetTaskAvailabilitySettled agentNetTaskAvailabilityStatus = "settled"
)

func normalizeAgentNetTaskAvailabilityStatus(status string) agentNetTaskAvailabilityStatus {
	switch agentNetTaskAvailabilityStatus(strings.ToLower(strings.TrimSpace(status))) {
	case agentNetTaskAvailabilityOpen:
		return agentNetTaskAvailabilityOpen
	case agentNetTaskAvailabilitySettled:
		return agentNetTaskAvailabilitySettled
	default:
		return agentNetTaskAvailabilityUnknown
	}
}

func (s agentNetTaskAvailabilityStatus) CanPick() bool {
	switch s {
	case agentNetTaskAvailabilityOpen, agentNetTaskAvailabilitySettled, agentNetTaskAvailabilityUnknown:
		return true
	default:
		return false
	}
}
