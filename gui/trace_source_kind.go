package main

import "strings"

type traceSourceKind string

const (
	traceSourceKindUnknown             traceSourceKind = ""
	traceSourceKindAdaptiveRetry       traceSourceKind = "adaptive_retry"
	traceSourceKindTrialReflectSummary traceSourceKind = "trial_reflect_summary"
)

func normalizeTraceSourceKind(sourceKind string) traceSourceKind {
	switch traceSourceKind(strings.ToLower(strings.TrimSpace(sourceKind))) {
	case traceSourceKindAdaptiveRetry:
		return traceSourceKindAdaptiveRetry
	case traceSourceKindTrialReflectSummary:
		return traceSourceKindTrialReflectSummary
	default:
		return traceSourceKindUnknown
	}
}
