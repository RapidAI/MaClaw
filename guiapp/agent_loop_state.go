package guiapp

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

type LoopState string

const (
	LoopStateUnknown   LoopState = ""
	LoopStateRunning   LoopState = "running"
	LoopStatePaused    LoopState = "paused"
	LoopStateStopped   LoopState = "stopped"
	LoopStateCompleted LoopState = "completed"
	LoopStateFailed    LoopState = "failed"
	LoopStateTimeout   LoopState = "timeout"
)

func normalizeLoopState(value string) LoopState {
	switch LoopState(strings.ToLower(strings.TrimSpace(value))) {
	case LoopStateRunning:
		return LoopStateRunning
	case LoopStatePaused:
		return LoopStatePaused
	case LoopStateStopped:
		return LoopStateStopped
	case LoopStateCompleted:
		return LoopStateCompleted
	case LoopStateFailed:
		return LoopStateFailed
	case LoopStateTimeout:
		return LoopStateTimeout
	default:
		return LoopStateUnknown
	}
}

func (s LoopState) String() string {
	return string(s)
}

// RuntimeStatusValue projects the GUI loop vocabulary onto the shared
// transport-neutral Runtime job status vocabulary.
func (s LoopState) RuntimeStatusValue() agentruntime.JobStatus {
	switch normalizeLoopState(s.String()) {
	case LoopStateRunning:
		return agentruntime.JobStatusRunning
	case LoopStateCompleted:
		return agentruntime.JobStatusSucceeded
	case LoopStateStopped:
		return agentruntime.JobStatusCanceled
	case LoopStateFailed, LoopStateTimeout:
		return agentruntime.JobStatusFailed
	case LoopStatePaused:
		return agentruntime.JobStatusUnknown
	default:
		return agentruntime.JobStatusUnknown
	}
}

func (s LoopState) IsPaused() bool {
	return s == LoopStatePaused
}
