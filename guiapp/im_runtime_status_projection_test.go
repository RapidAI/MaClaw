package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestRuntimeStatusInfoProjectsSharedJobStatus(t *testing.T) {
	if got := runtimeTaskStatusKilled.RuntimeStatusValue(); got != agentruntime.JobStatusCanceled {
		t.Fatalf("killed task runtime status=%q, want canceled", got)
	}
	if got := runtimeTaskStatusCompleted.RuntimeStatusValue(); got != agentruntime.JobStatusSucceeded {
		t.Fatalf("completed task runtime status=%q, want succeeded", got)
	}
	if got := runtimeSessionStatusError.RuntimeStatusValue(); got != agentruntime.JobStatusFailed {
		t.Fatalf("error session runtime status=%q, want failed", got)
	}
	if got := runtimeSessionStatusExited.RuntimeStatusValue(); got != agentruntime.JobStatusUnknown {
		t.Fatalf("exited session runtime status=%q, want unknown", got)
	}
}
