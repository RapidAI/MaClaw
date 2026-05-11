package main

import "strings"

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

func (s LoopState) IsPaused() bool {
	return s == LoopStatePaused
}
