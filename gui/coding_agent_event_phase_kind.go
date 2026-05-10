package main

import "strings"

type codingAgentEventPhaseKind string

const (
	codingAgentEventPhaseUnknown codingAgentEventPhaseKind = ""
	codingAgentEventPhaseRunning codingAgentEventPhaseKind = "running"
	codingAgentEventPhaseResult  codingAgentEventPhaseKind = "result"
)

func normalizeCodingAgentEventPhaseKind(phase string) codingAgentEventPhaseKind {
	switch codingAgentEventPhaseKind(strings.ToLower(strings.TrimSpace(phase))) {
	case codingAgentEventPhaseRunning:
		return codingAgentEventPhaseRunning
	case codingAgentEventPhaseResult:
		return codingAgentEventPhaseResult
	default:
		return codingAgentEventPhaseUnknown
	}
}

func (k codingAgentEventPhaseKind) String() string {
	return string(k)
}
