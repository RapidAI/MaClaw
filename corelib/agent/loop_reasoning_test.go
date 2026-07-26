package agent

import (
	"strings"
	"testing"
)

func TestAppendLoopDisplayReasoningKeepsDistinctRounds(t *testing.T) {
	var got strings.Builder
	appendLoopDisplayReasoning(&got, "Inspect the request.")
	appendLoopDisplayReasoning(&got, "Use the tool result to prepare the answer.")
	appendLoopDisplayReasoning(&got, "Use the tool result to prepare the answer.")
	if want := "Inspect the request.\n\nUse the tool result to prepare the answer."; got.String() != want {
		t.Fatalf("reasoning = %q, want %q", got.String(), want)
	}
}

func TestAppendLoopDisplayReasoningReplacesPartialSummary(t *testing.T) {
	var got strings.Builder
	appendLoopDisplayReasoning(&got, "Inspect the request")
	appendLoopDisplayReasoning(&got, "Inspect the request and prepare the answer.")
	if want := "Inspect the request and prepare the answer."; got.String() != want {
		t.Fatalf("reasoning = %q, want %q", got.String(), want)
	}
}
