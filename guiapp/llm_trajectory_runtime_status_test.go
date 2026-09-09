package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestTrajectoryRuntimeStatusProjection(t *testing.T) {
	cases := map[string]agentruntime.JobStatus{
		"success":   agentruntime.JobStatusSucceeded,
		"error":     agentruntime.JobStatusFailed,
		"hard_exit": agentruntime.JobStatusFailed,
		"cancelled": agentruntime.JobStatusCanceled,
		"paused":    agentruntime.JobStatusPending,
		"unknown":   agentruntime.JobStatusUnknown,
	}
	for status, want := range cases {
		if got := trajectoryRuntimeStatus(status); got != want {
			t.Errorf("trajectoryRuntimeStatus(%q)=%q, want %q", status, got, want)
		}
	}
}
