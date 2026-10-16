package agentruntime

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type settlingJobReconciler struct{}

func (settlingJobReconciler) ReconcileJob(_ context.Context, job Job, effects []JobEffect) (JobReconcileResult, error) {
	if job.Kind != "skill.run" {
		return JobReconcileResult{}, nil
	}
	for _, effect := range effects {
		if effect.State == JobEffectCommitted && len(effect.Payload) > 0 {
			return JobReconcileResult{Resolved: true, Status: JobStatusSucceeded, Result: effect.Payload}, nil
		}
	}
	return JobReconcileResult{}, nil
}

func TestReconcileOpenJobsSettlesCommittedEffect(t *testing.T) {
	ctx := context.Background()
	jobs := NewMemoryJobRepository()
	effects := NewMemoryJobEffectRepository()
	running := Job{ID: "run-1", Kind: "skill.run", Status: JobStatusRunning, TenantID: "gui", UserID: "local"}
	if _, created, err := jobs.Admit(ctx, running); err != nil || !created {
		t.Fatalf("admit=%v created=%t", err, created)
	}
	done := Job{ID: "run-2", Kind: "skill.run", Status: JobStatusSucceeded, TenantID: "gui", UserID: "local"}
	if _, _, err := jobs.Admit(ctx, done); err != nil {
		t.Fatal(err)
	}
	other := Job{ID: "plan-1", Kind: "orchestrator.plan", Status: JobStatusRunning, TenantID: "gui", UserID: "local"}
	if _, _, err := jobs.Admit(ctx, other); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	payload := json.RawMessage(`{"ok":true}`)
	prepared, _, err := effects.Prepare(ctx, JobEffect{
		JobID: "run-1", Kind: "skill.run", JobKind: "skill.run", TenantID: "gui", UserID: "local",
		ResourceID: "run-1", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	prepared.State = JobEffectCommitted
	prepared.ReceiptDigest = JobEffectReceiptDigest("run-1:committed")
	prepared.Payload = payload
	prepared.UpdatedAt = now
	if _, err := effects.Update(ctx, prepared.Version, prepared); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileOpenJobs(ctx, jobs, effects, "skill.run", settlingJobReconciler{}); err != nil {
		t.Fatal(err)
	}
	got, err := jobs.Get(ctx, "run-1")
	if err != nil || got.Status != JobStatusSucceeded || string(got.Result) != `{"ok":true}` || got.CompletedAt == nil {
		t.Fatalf("settled=%#v err=%v", got, err)
	}
	still, err := jobs.Get(ctx, "plan-1")
	if err != nil || still.Status != JobStatusRunning {
		t.Fatalf("other kind must be left alone: %#v err=%v", still, err)
	}
	if !JobStatusIsTerminal(JobStatusSucceeded) || JobStatusIsTerminal(JobStatusRunning) {
		t.Fatal("terminal helper mismatch")
	}
}

func TestRecoverAndReconcileOpenJobsExpiresThenSettles(t *testing.T) {
	ctx := context.Background()
	jobs := NewMemoryJobRepository()
	effects := NewMemoryJobEffectRepository()
	agedAt := time.Now().UTC().Add(-2 * DefaultJobRetention)
	aged := Job{ID: "run-aged", Kind: "skill.run", Status: JobStatusSucceeded, TenantID: "gui", UserID: "local", CompletedAt: &agedAt}
	if _, _, err := jobs.Admit(ctx, aged); err != nil {
		t.Fatal(err)
	}
	orphan := Job{ID: "run-orphan", Kind: "skill.run", Status: JobStatusRunning, TenantID: "gui", UserID: "local", RecoveryPolicy: JobRecoveryPolicyReconcile}
	if _, _, err := jobs.Admit(ctx, orphan); err != nil {
		t.Fatal(err)
	}
	settled := Job{ID: "run-1", Kind: "skill.run", Status: JobStatusRunning, TenantID: "gui", UserID: "local", RecoveryPolicy: JobRecoveryPolicyReconcile}
	if _, _, err := jobs.Admit(ctx, settled); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	payload := json.RawMessage(`{"ok":true}`)
	prepared, _, err := effects.Prepare(ctx, JobEffect{
		JobID: "run-1", Kind: "skill.run", JobKind: "skill.run", TenantID: "gui", UserID: "local",
		ResourceID: "run-1", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	prepared.State = JobEffectCommitted
	prepared.ReceiptDigest = JobEffectReceiptDigest("run-1:committed")
	prepared.Payload = payload
	prepared.UpdatedAt = now
	if _, err := effects.Update(ctx, prepared.Version, prepared); err != nil {
		t.Fatal(err)
	}
	if err := RecoverAndReconcileOpenJobs(ctx, jobs, effects, "skill.run", settlingJobReconciler{}, nil); err != nil {
		t.Fatal(err)
	}
	got, err := jobs.Get(ctx, "run-1")
	if err != nil || got.Status != JobStatusSucceeded || string(got.Result) != `{"ok":true}` {
		t.Fatalf("settled=%#v err=%v", got, err)
	}
	unknown, err := jobs.Get(ctx, "run-orphan")
	if err != nil || unknown.Status != JobStatusUnknown || unknown.ErrorCode != JobErrorCodeReconcileRequired {
		t.Fatalf("orphan=%#v err=%v", unknown, err)
	}
	if _, err := jobs.Get(ctx, "run-aged"); err != ErrJobRepositoryNotFound {
		t.Fatalf("aged terminal job must be pruned: err=%v", err)
	}
}

func TestReconcileFromProtectedEffectsAndResourceIDs(t *testing.T) {
	committed := ReconcileFromProtectedEffects([]JobEffect{{
		Kind: "skill.run", State: JobEffectCommitted, Payload: json.RawMessage(`{"ok":true}`),
	}}, "skill.run", "skill run failed", nil)
	if !committed.Resolved || committed.Status != JobStatusSucceeded || string(committed.Result) != `{"ok":true}` {
		t.Fatalf("committed=%#v", committed)
	}
	redacted := ReconcileFromProtectedEffects([]JobEffect{{
		Kind: "mcp.create", State: JobEffectCommitted, Payload: json.RawMessage(`{"secret":"x"}`),
	}}, "mcp.create", "MCP effect failed", func(json.RawMessage) json.RawMessage { return nil })
	if redacted.Resolved {
		t.Fatal("empty sanitize must leave the job unresolved")
	}
	failed := ReconcileFromProtectedEffects([]JobEffect{{
		Kind: "skill.run", State: JobEffectFailed, ReasonCode: "boom",
	}}, "skill.run", "skill run failed", nil)
	if !failed.Resolved || failed.Status != JobStatusFailed || failed.ErrorCode != "boom" {
		t.Fatalf("failed=%#v", failed)
	}
	if got := JobEffectOperation(JobEffect{Payload: json.RawMessage(`{"operation":"Upload"}`)}); got != "upload" {
		t.Fatalf("operation=%q", got)
	}
	if got := ParseJobEffectResourceIDs(`["a"," b "]`); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("ids=%#v", got)
	}
}
