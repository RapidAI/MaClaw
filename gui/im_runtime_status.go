package main

import "strings"

type runtimeTaskSource string

const (
	runtimeTaskSourceLocal runtimeTaskSource = "local"
	runtimeTaskSourceSSH   runtimeTaskSource = "ssh"
)

type runtimeSessionSource string

const (
	runtimeSessionSourceCoding runtimeSessionSource = "coding"
	runtimeSessionSourceSSH    runtimeSessionSource = "ssh"
)

type runtimeTaskStatus string

const (
	runtimeTaskStatusPending   runtimeTaskStatus = "pending"
	runtimeTaskStatusRunning   runtimeTaskStatus = "running"
	runtimeTaskStatusCompleted runtimeTaskStatus = "completed"
	runtimeTaskStatusFailed    runtimeTaskStatus = "failed"
	runtimeTaskStatusKilled    runtimeTaskStatus = "killed"
	runtimeTaskStatusUnknown   runtimeTaskStatus = "unknown"
)

func normalizeRuntimeTaskStatus(status interface{}) runtimeTaskStatus {
	switch runtimeTaskStatus(strings.TrimSpace(statusString(status))) {
	case runtimeTaskStatusPending:
		return runtimeTaskStatusPending
	case runtimeTaskStatusRunning:
		return runtimeTaskStatusRunning
	case runtimeTaskStatusCompleted:
		return runtimeTaskStatusCompleted
	case runtimeTaskStatusFailed:
		return runtimeTaskStatusFailed
	case runtimeTaskStatusKilled:
		return runtimeTaskStatusKilled
	default:
		return runtimeTaskStatusUnknown
	}
}

func (s runtimeTaskStatus) IsActive() bool {
	return s == runtimeTaskStatusRunning || s == runtimeTaskStatusPending
}

func (s runtimeTaskStatus) HasExitCode() bool {
	return s == runtimeTaskStatusCompleted || s == runtimeTaskStatusFailed
}

func (s runtimeTaskStatus) Icon() string {
	switch s {
	case runtimeTaskStatusRunning, runtimeTaskStatusPending:
		return "🔄"
	case runtimeTaskStatusCompleted:
		return "✅"
	case runtimeTaskStatusFailed:
		return "❌"
	case runtimeTaskStatusKilled:
		return "⏹️"
	default:
		return "❓"
	}
}
