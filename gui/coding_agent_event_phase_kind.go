package main

import "strings"

type codingAgentEventPhaseKind string

const (
	codingAgentEventPhaseUnknown   codingAgentEventPhaseKind = ""
	codingAgentEventPhaseStarting  codingAgentEventPhaseKind = "starting"
	codingAgentEventPhaseRunning   codingAgentEventPhaseKind = "running"
	codingAgentEventPhaseCompleted codingAgentEventPhaseKind = "completed"
	codingAgentEventPhaseFailed    codingAgentEventPhaseKind = "failed"
	codingAgentEventPhaseRetrying  codingAgentEventPhaseKind = "retrying"
	codingAgentEventPhaseSkipped   codingAgentEventPhaseKind = "skipped"
	codingAgentEventPhaseResult    codingAgentEventPhaseKind = "result"
)

func normalizeCodingAgentEventPhaseKind(phase string) codingAgentEventPhaseKind {
	switch codingAgentEventPhaseKind(strings.ToLower(strings.TrimSpace(phase))) {
	case codingAgentEventPhaseStarting:
		return codingAgentEventPhaseStarting
	case codingAgentEventPhaseRunning:
		return codingAgentEventPhaseRunning
	case codingAgentEventPhaseCompleted:
		return codingAgentEventPhaseCompleted
	case codingAgentEventPhaseFailed:
		return codingAgentEventPhaseFailed
	case codingAgentEventPhaseRetrying:
		return codingAgentEventPhaseRetrying
	case codingAgentEventPhaseSkipped:
		return codingAgentEventPhaseSkipped
	case codingAgentEventPhaseResult:
		return codingAgentEventPhaseResult
	default:
		return codingAgentEventPhaseUnknown
	}
}

func (k codingAgentEventPhaseKind) String() string {
	return string(k)
}
