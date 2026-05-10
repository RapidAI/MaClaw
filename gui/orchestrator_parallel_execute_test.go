package main

import (
	"strings"
	"testing"
)

func TestOrchestratorRejectsLocalBuiltinToolsBeforeRemoteLaunch(t *testing.T) {
	o := &Orchestrator{}

	got := o.executeOneTask(TaskRequest{
		Tool:        "write_file",
		Description: "write a local file",
		ProjectPath: t.TempDir(),
	})

	if normalizeOrchestratorSessionStatus(got.Status) != orchestratorSessionStatusFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
	if !strings.Contains(got.Error, "local built-in tool") || !strings.Contains(got.Error, "call it directly") {
		t.Fatalf("unexpected error: %q", got.Error)
	}
}

func TestParallelExecuteUnsupportedToolErrorForUnknownTool(t *testing.T) {
	got := parallelExecuteUnsupportedToolError("not-a-real-tool")
	if strings.Contains(got, "local built-in tool") {
		t.Fatalf("unknown tool should not be reported as local built-in: %q", got)
	}
	if !strings.Contains(got, "not a remote coding tool") {
		t.Fatalf("unexpected error: %q", got)
	}
}
