package main

import "strings"

// completionSignals are phrases that indicate a task has been completed.
// Do not include "" — strings.Contains(line, "") is always true.
var completionSignals = []string{
	"i've completed",
	"已完成",
	"all done",
	"successfully",
	"changes applied",
}

// incompletionSignals are phrases that indicate a task is still in progress.
var incompletionSignals = []string{
	"i'll continue",
	"接下来我会",
	"next, i'll",
	"let me continue",
	"i need to",
	"还需要",
}

type sessionCompletionSignalKind int

const (
	sessionCompletionSignalUnknown sessionCompletionSignalKind = iota
	sessionCompletionSignalCompleted
	sessionCompletionSignalIncomplete
)

func classifySessionCompletionSignal(line string) sessionCompletionSignalKind {
	lower := strings.ToLower(line)
	for _, sig := range completionSignals {
		if strings.Contains(lower, sig) {
			return sessionCompletionSignalCompleted
		}
	}
	for _, sig := range incompletionSignals {
		if strings.Contains(lower, sig) {
			return sessionCompletionSignalIncomplete
		}
	}
	return sessionCompletionSignalUnknown
}
