package guiapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestSkillRunRuntimeStatusValue(t *testing.T) {
	for status, want := range map[skillRunLifecycleStatus]agentruntime.JobStatus{
		skillRunStatusRunning:   agentruntime.JobStatusRunning,
		skillRunStatusSuccess:   agentruntime.JobStatusSucceeded,
		skillRunStatusFailed:    agentruntime.JobStatusFailed,
		skillRunStatusCancelled: agentruntime.JobStatusCanceled,
		skillRunStatusUnknown:   agentruntime.JobStatusUnknown,
	} {
		got := (&SkillRunStatus{Status: status}).RuntimeStatusValue()
		if got != want {
			t.Errorf("RuntimeStatusValue(%q)=%q, want %q", status, got, want)
		}
	}
}

func TestSkillRunRuntimeStatusValueNil(t *testing.T) {
	var status *SkillRunStatus
	if got := status.RuntimeStatusValue(); got != agentruntime.JobStatusUnknown {
		t.Fatalf("nil RuntimeStatusValue=%q, want unknown", got)
	}
}

func TestSkillRunnerPublishesRuntimeJob(t *testing.T) {
	runner := NewSkillRunner(nil)
	runner.publishRuntimeJob(SkillRunStatus{
		RunID:     "run-1",
		Skill:     "demo",
		OwnerID:   "user-a",
		Status:    skillRunStatusRunning,
		StartedAt: "2026-09-03T01:02:03Z",
	})
	got, err := runner.runtimeJobs.Get(context.Background(), "run-1")
	if err != nil || got.Kind != "skill.run" || got.Status != agentruntime.JobStatusRunning || got.UserID != "user-a" {
		t.Fatalf("runtime job=%#v err=%v", got, err)
	}
	runner.publishRuntimeJob(SkillRunStatus{RunID: "run-1", Skill: "demo", Status: skillRunStatusSuccess, StartedAt: "2026-09-03T01:02:03Z", EndedAt: "2026-09-03T01:03:03Z"})
	got, err = runner.runtimeJobs.Get(context.Background(), "run-1")
	if err != nil || got.Status != agentruntime.JobStatusSucceeded || got.CompletedAt == nil {
		t.Fatalf("updated runtime job=%#v err=%v", got, err)
	}
	effects, err := runner.runtimeEffects.ListByJob(context.Background(), "run-1")
	if err != nil || len(effects) != 1 || effects[0].State != agentruntime.JobEffectCommitted {
		t.Fatalf("runtime effect=%#v err=%v", effects, err)
	}
	reconciled, err := runner.ReconcileJob(context.Background(), got, effects)
	if err != nil || !reconciled.Resolved || reconciled.Status != agentruntime.JobStatusSucceeded {
		t.Fatalf("reconcile=%#v err=%v", reconciled, err)
	}
}

func TestSkillRunnerDurableStoresSurviveReopenAndReconcile(t *testing.T) {
	dir := t.TempDir()
	first := NewSkillRunner(nil)
	if err := first.UseDurableRuntimeStores(dir); err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	ended := time.Now().UTC().Format(time.RFC3339)
	first.publishRuntimeJob(SkillRunStatus{RunID: "run-1", Skill: "demo", Status: skillRunStatusRunning, OwnerID: "user-a", StartedAt: started})
	first.publishRuntimeJob(SkillRunStatus{RunID: "run-1", Skill: "demo", Status: skillRunStatusSuccess, OwnerID: "user-a", StartedAt: started, EndedAt: ended})
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second := NewSkillRunner(nil)
	if err := second.UseDurableRuntimeStores(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	got, err := second.runtimeJobs.Get(context.Background(), "run-1")
	if err != nil || got.Status != agentruntime.JobStatusSucceeded {
		t.Fatalf("reopened job=%#v err=%v", got, err)
	}
	effects, err := second.runtimeEffects.ListByJob(context.Background(), "run-1")
	if err != nil || len(effects) != 1 || effects[0].State != agentruntime.JobEffectCommitted {
		t.Fatalf("reopened effects=%#v err=%v", effects, err)
	}
	if err := agentruntime.UpsertJob(context.Background(), second.runtimeJobs, agentruntime.Job{ID: "run-2", Kind: "skill.run", Status: agentruntime.JobStatusRunning, TenantID: "gui", UserID: "local"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, _, err := second.runtimeEffects.Prepare(context.Background(), agentruntime.JobEffect{
		JobID: "run-2", Kind: "skill.run", JobKind: "skill.run", TenantID: "gui", UserID: "local", ResourceID: "run-2", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	prepared, err := second.runtimeEffects.Get(context.Background(), "run-2", "skill.run")
	if err != nil {
		t.Fatal(err)
	}
	prepared.State = agentruntime.JobEffectFailed
	prepared.ReasonCode = "skill_run_failed"
	if _, err := second.runtimeEffects.Update(context.Background(), prepared.Version, prepared); err != nil {
		t.Fatal(err)
	}
	if err := second.ReconcileRuntimeJobs(context.Background()); err != nil {
		t.Fatal(err)
	}
	failed, err := second.runtimeJobs.Get(context.Background(), "run-2")
	if err != nil || failed.Status != agentruntime.JobStatusFailed {
		t.Fatalf("reconciled job=%#v err=%v", failed, err)
	}
	old := time.Now().UTC().Add(-2 * agentruntime.DefaultJobRetention)
	if err := agentruntime.UpsertJob(context.Background(), second.runtimeJobs, agentruntime.Job{
		ID: "run-old", Kind: "skill.run", Status: agentruntime.JobStatusSucceeded, TenantID: "gui", UserID: "local", CompletedAt: &old,
	}); err != nil {
		t.Fatal(err)
	}
	if err := second.ReconcileRuntimeJobs(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := second.runtimeJobs.Get(context.Background(), "run-old"); !errors.Is(err, agentruntime.ErrJobRepositoryNotFound) {
		t.Fatalf("aged terminal job must be pruned: err=%v", err)
	}
}
