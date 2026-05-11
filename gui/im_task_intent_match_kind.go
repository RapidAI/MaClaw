package main

import "strings"

type taskIntentMatchKind string

const (
	taskIntentMatchUnknown      taskIntentMatchKind = ""
	taskIntentMatchContinuation taskIntentMatchKind = "continuation"
)

func normalizeTaskIntentMatchKind(value string) taskIntentMatchKind {
	switch taskIntentMatchKind(strings.ToLower(strings.TrimSpace(value))) {
	case taskIntentMatchContinuation:
		return taskIntentMatchContinuation
	default:
		return taskIntentMatchUnknown
	}
}

func (kind taskIntentMatchKind) IsContinuation() bool {
	return kind == taskIntentMatchContinuation
}

func (result taskIntentResult) IsContinuationMatch() bool {
	return normalizeTaskIntentMatchKind(result.Matched).IsContinuation()
}
