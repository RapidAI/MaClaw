package main

import "strings"

type traceSourceKind string

const (
	traceSourceKindUnknown             traceSourceKind = ""
	traceSourceKindAdaptiveRetry       traceSourceKind = "adaptive_retry"
	traceSourceKindAITool              traceSourceKind = "ai_tool"
	traceSourceKindRemoteEvent         traceSourceKind = "remote_event"
	traceSourceKindRemoteOutput        traceSourceKind = "remote_output"
	traceSourceKindTrialReflect        traceSourceKind = "trial_reflect"
	traceSourceKindTrialReflectSummary traceSourceKind = "trial_reflect_summary"
)

func (kind traceSourceKind) String() string {
	return string(kind)
}

func normalizeTraceSourceKind(sourceKind string) traceSourceKind {
	switch traceSourceKind(strings.ToLower(strings.TrimSpace(sourceKind))) {
	case traceSourceKindAdaptiveRetry:
		return traceSourceKindAdaptiveRetry
	case traceSourceKindAITool:
		return traceSourceKindAITool
	case traceSourceKindRemoteEvent:
		return traceSourceKindRemoteEvent
	case traceSourceKindRemoteOutput:
		return traceSourceKindRemoteOutput
	case traceSourceKindTrialReflect:
		return traceSourceKindTrialReflect
	case traceSourceKindTrialReflectSummary:
		return traceSourceKindTrialReflectSummary
	default:
		return traceSourceKindUnknown
	}
}
