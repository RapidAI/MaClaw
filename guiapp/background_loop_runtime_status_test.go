package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestLoopStateRuntimeStatusValue(t *testing.T) {
	cases := map[LoopState]agentruntime.JobStatus{
		LoopStateUnknown:   agentruntime.JobStatusUnknown,
		LoopStateRunning:   agentruntime.JobStatusRunning,
		LoopStatePaused:    agentruntime.JobStatusUnknown,
		LoopStateStopped:   agentruntime.JobStatusCanceled,
		LoopStateCompleted: agentruntime.JobStatusSucceeded,
		LoopStateFailed:    agentruntime.JobStatusFailed,
		LoopStateTimeout:   agentruntime.JobStatusFailed,
		LoopState("bogus"): agentruntime.JobStatusUnknown,
	}
	for state, want := range cases {
		if got := state.RuntimeStatusValue(); got != want {
			t.Errorf("RuntimeStatusValue(%q)=%q, want %q", state, got, want)
		}
	}
}

func TestBackgroundLoopListViewsProjectsRuntimeStatus(t *testing.T) {
	mgr := NewBackgroundLoopManager(nil)
	ctx := mgr.Spawn(SlotKindCoding, "user-1", "demo", 1, nil)
	if ctx == nil {
		t.Fatal("Spawn returned nil")
	}
	views := mgr.ListViews()
	if len(views) != 1 {
		t.Fatalf("ListViews() returned %d views, want 1", len(views))
	}
	if views[0].RuntimeStatus != agentruntime.JobStatusRunning {
		t.Fatalf("RuntimeStatus=%q, want %q", views[0].RuntimeStatus, agentruntime.JobStatusRunning)
	}
	mgr.Complete(ctx.ID)
}
