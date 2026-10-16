package agentservice

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestSQLiteJobEffectRepositoryPersistsBindingAndSettlement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "effects", "job_effects.db")
	first, err := NewSQLiteJobEffectRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	second, err := NewSQLiteJobEffectRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	now := time.Now().UTC()
	candidate := agentruntime.JobEffect{JobID: "job-1", TenantID: "tenant", UserID: "user", JobKind: "migration.export", Kind: "migration.export", Payload: []byte(`{"phase":"create"}`), CreatedAt: now}
	prepared, created, err := first.Prepare(context.Background(), candidate)
	if err != nil || !created || prepared.Version != 1 || prepared.State != agentruntime.JobEffectPrepared {
		t.Fatalf("prepare = %#v created=%v err=%v", prepared, created, err)
	}
	replayCandidate := candidate
	replayCandidate.CreatedAt = now.Add(time.Second)
	replay, created, err := second.Prepare(context.Background(), replayCandidate)
	if err != nil || created || replay.Version != prepared.Version {
		t.Fatalf("replay = %#v created=%v err=%v", replay, created, err)
	}
	bound := prepared
	bound.ResourceID = "export-1"
	bound, err = second.Update(context.Background(), prepared.Version, bound)
	if err != nil || bound.Version != 2 || bound.ResourceID != "export-1" {
		t.Fatalf("bind = %#v err=%v", bound, err)
	}
	stale := prepared
	stale.ResourceID = "export-2"
	if _, err := first.Update(context.Background(), prepared.Version, stale); !errors.Is(err, agentruntime.ErrJobEffectConflict) {
		t.Fatalf("stale bind error = %v", err)
	}
	settled := bound
	settled.State = agentruntime.JobEffectCommitted
	settled.ReceiptDigest = agentruntime.JobEffectReceiptDigest("ready:export-1")
	settled.ReasonCode = "remote_ready"
	settled.Payload = []byte(`{"status":"ready"}`)
	settled, err = first.Update(context.Background(), bound.Version, settled)
	if err != nil || settled.State != agentruntime.JobEffectCommitted || settled.Version != 3 {
		t.Fatalf("settle = %#v err=%v", settled, err)
	}
	reopened, err := second.Get(context.Background(), "job-1", "migration.export")
	if err != nil || reopened.ResourceID != "export-1" || reopened.ReceiptDigest != settled.ReceiptDigest {
		t.Fatalf("reopened = %#v err=%v", reopened, err)
	}
	if _, err := second.Update(context.Background(), reopened.Version, agentruntime.JobEffect{JobID: reopened.JobID, TenantID: reopened.TenantID, UserID: reopened.UserID, JobKind: reopened.JobKind, Kind: reopened.Kind, ResourceID: reopened.ResourceID, State: agentruntime.JobEffectUnknown, CreatedAt: reopened.CreatedAt}); !errors.Is(err, agentruntime.ErrJobEffectConflict) {
		t.Fatalf("terminal effect reopened: %v", err)
	}
	mutatedReceipt := reopened
	mutatedReceipt.ReceiptDigest = agentruntime.JobEffectReceiptDigest("different receipt")
	if _, err := second.Update(context.Background(), reopened.Version, mutatedReceipt); !errors.Is(err, agentruntime.ErrJobEffectConflict) {
		t.Fatalf("terminal effect receipt mutated: %v", err)
	}
}

func TestSQLiteJobEffectRepositoryDeleteByJobs(t *testing.T) {
	repository, err := NewSQLiteJobEffectRepository(filepath.Join(t.TempDir(), "job_effects.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	for _, jobID := range []string{"job-1", "job-2"} {
		_, _, err := repository.Prepare(context.Background(), agentruntime.JobEffect{JobID: jobID, TenantID: "tenant", UserID: "user", JobKind: "test", Kind: "test.effect", CreatedAt: time.Now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.DeleteByJobs(context.Background(), []string{"job-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Get(context.Background(), "job-1", "test.effect"); !errors.Is(err, agentruntime.ErrJobEffectNotFound) {
		t.Fatalf("deleted effect error = %v", err)
	}
	if _, err := repository.Get(context.Background(), "job-2", "test.effect"); err != nil {
		t.Fatalf("sibling effect removed: %v", err)
	}
}
