package guiapp

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestOrchestratorPublishesRuntimeJob(t *testing.T) {
	orch := NewTaskOrchestrator2(nil, nil, nil)
	plan, err := orch.CreatePlan("demo", []PlanSubTask{{Description: "one", Tool: "bash"}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := orch.runtimeJobs.Get(context.Background(), plan.ID)
	if err != nil || got.Kind != "orchestrator.plan" || got.Status != agentruntime.JobStatusPending {
		t.Fatalf("runtime job=%#v err=%v", got, err)
	}
	if err := orch.Cancel(plan.ID); err != nil {
		t.Fatal(err)
	}
	got, err = orch.runtimeJobs.Get(context.Background(), plan.ID)
	if err != nil || got.Status != agentruntime.JobStatusCanceled {
		t.Fatalf("canceled runtime job=%#v err=%v", got, err)
	}
}

func TestOrchestratorReconcilesCommittedPlanEffect(t *testing.T) {
	orch := NewTaskOrchestrator2(nil, nil, nil)
	plan, err := orch.CreatePlan("demo", []PlanSubTask{{Description: "one", Tool: "bash"}})
	if err != nil {
		t.Fatal(err)
	}
	orch.mu.Lock()
	plan.Status = orchestratorTaskStatusCompleted
	orch.publishRuntimeJobLocked(plan)
	orch.mu.Unlock()
	job, err := orch.runtimeJobs.Get(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	effects, err := orch.runtimeEffects.ListByJob(context.Background(), plan.ID)
	if err != nil || len(effects) != 1 || effects[0].State != agentruntime.JobEffectCommitted {
		t.Fatalf("effects=%#v err=%v", effects, err)
	}
	reconciled, err := orch.ReconcileJob(context.Background(), job, effects)
	if err != nil || !reconciled.Resolved || reconciled.Status != agentruntime.JobStatusSucceeded {
		t.Fatalf("reconcile=%#v err=%v", reconciled, err)
	}
}

func TestOrchestratorRuntimeStatusValue(t *testing.T) {
	cases := map[orchestratorTaskStatus]agentruntime.JobStatus{
		orchestratorTaskStatusPlanning:       agentruntime.JobStatusPending,
		orchestratorTaskStatusPending:        agentruntime.JobStatusPending,
		orchestratorTaskStatusRunning:        agentruntime.JobStatusRunning,
		orchestratorTaskStatusCompleted:      agentruntime.JobStatusSucceeded,
		orchestratorTaskStatusFailed:         agentruntime.JobStatusFailed,
		orchestratorTaskStatusCancelled:      agentruntime.JobStatusCanceled,
		orchestratorTaskStatusPartialFailure: agentruntime.JobStatusUnknown,
	}
	for status, want := range cases {
		if got := status.RuntimeStatusValue(); got != want {
			t.Errorf("RuntimeStatusValue(%q)=%q, want %q", status, got, want)
		}
	}
}
