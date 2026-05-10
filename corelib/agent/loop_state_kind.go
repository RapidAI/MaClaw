package agent

import "strings"

type LoopStateKind string

const (
	LoopStateUnknown   LoopStateKind = ""
	LoopStateRunning   LoopStateKind = "running"
	LoopStatePaused    LoopStateKind = "paused"
	LoopStateCompleted LoopStateKind = "completed"
	LoopStateFailed    LoopStateKind = "failed"
	LoopStateStopped   LoopStateKind = "stopped"
)

func normalizeLoopStateKind(value string) LoopStateKind {
	switch LoopStateKind(strings.ToLower(strings.TrimSpace(value))) {
	case LoopStateRunning:
		return LoopStateRunning
	case LoopStatePaused:
		return LoopStatePaused
	case LoopStateCompleted:
		return LoopStateCompleted
	case LoopStateFailed:
		return LoopStateFailed
	case LoopStateStopped:
		return LoopStateStopped
	default:
		return LoopStateUnknown
	}
}

func (k LoopStateKind) String() string {
	return string(k)
}

func (k LoopStateKind) IsPaused() bool {
	return k == LoopStatePaused
}
