package agentruntime

import (
	"context"
	"testing"
	"time"
)

func TestMemoryJobEffectRepositoryPrepareOnceAndSettle(t *testing.T) {
	repo := NewMemoryJobEffectRepository()
	ctx := context.Background()
	now := time.Now().UTC()
	first, created, err := repo.Prepare(ctx, JobEffect{
		JobID: "run-1", Kind: "skill.run", JobKind: "skill.run",
		TenantID: "gui", UserID: "local", ResourceID: "run-1",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || !created || first.State != JobEffectPrepared || first.Version != 1 {
		t.Fatalf("prepare=%#v created=%t err=%v", first, created, err)
	}
	replay, created, err := repo.Prepare(ctx, first)
	if err != nil || created || replay.Version != 1 {
		t.Fatalf("prepare must be idempotent created=%t err=%v %#v", created, err, replay)
	}
	settled := first
	settled.State = JobEffectCommitted
	settled.ReceiptDigest = JobEffectReceiptDigest("run-1:succeeded")
	updated, err := repo.Update(ctx, first.Version, settled)
	if err != nil || updated.Version != 2 || updated.State != JobEffectCommitted {
		t.Fatalf("settle=%#v err=%v", updated, err)
	}
	if _, err := repo.Update(ctx, first.Version, settled); err == nil {
		t.Fatal("stale settle must conflict")
	}
	listed, err := repo.ListByJob(ctx, "run-1")
	if err != nil || len(listed) != 1 || listed[0].State != JobEffectCommitted {
		t.Fatalf("list=%#v err=%v", listed, err)
	}
}
