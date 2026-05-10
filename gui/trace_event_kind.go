package main

import "strings"

type traceEventKind string

const (
	traceEventKindUnknown       traceEventKind = ""
	traceEventKindTrialObserved traceEventKind = "trial.observed"
)

func normalizeTraceEventKind(kind string) traceEventKind {
	switch traceEventKind(strings.TrimSpace(kind)) {
	case traceEventKindTrialObserved:
		return traceEventKindTrialObserved
	default:
		return traceEventKindUnknown
	}
}

func (kind traceEventKind) String() string {
	return string(kind)
}
