package agentruntime

import (
	"context"
	"errors"
	"testing"
)

func TestMemoryJobRepositoryAdmitCASAndClone(t *testing.T) {
	repo := NewMemoryJobRepository()
	ctx := context.Background()
	job := Job{ID: "job-1", Kind: "demo", Status: JobStatusPending, TenantID: "tenant", UserID: "user"}
	canonical, created, err := repo.Admit(ctx, job)
	if err != nil || !created || canonical.Version != 1 {
		t.Fatalf("admit = %#v created=%t err=%v", canonical, created, err)
	}
	canonical.Status = JobStatusRunning
	updated, err := repo.Update(ctx, 1, canonical)
	if err != nil || updated.Version != 2 || updated.Status != JobStatusRunning {
		t.Fatalf("update = %#v err=%v", updated, err)
	}
	if _, err := repo.Update(ctx, 1, updated); !errors.Is(err, ErrJobRepositoryVersionConflict) {
		t.Fatalf("stale update err=%v, want version conflict", err)
	}
	got, err := repo.Get(ctx, "job-1")
	if err != nil || got.Status != JobStatusRunning {
		t.Fatalf("get = %#v err=%v", got, err)
	}
}

func TestUpsertJobAdmitsThenUpdates(t *testing.T) {
	repo := NewMemoryJobRepository()
	ctx := context.Background()
	if err := UpsertJob(ctx, repo, Job{ID: "job-1", Kind: "skill.run", Status: JobStatusRunning}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, "job-1")
	if err != nil || got.Status != JobStatusRunning || got.Version != 1 {
		t.Fatalf("admitted=%#v err=%v", got, err)
	}
	if err := UpsertJob(ctx, repo, Job{ID: "job-1", Kind: "skill.run", Status: JobStatusSucceeded}); err != nil {
		t.Fatal(err)
	}
	got, err = repo.Get(ctx, "job-1")
	if err != nil || got.Status != JobStatusSucceeded || got.Version != 2 {
		t.Fatalf("updated=%#v err=%v", got, err)
	}
	if err := UpsertJob(ctx, nil, Job{ID: "job-1"}); err != nil {
		t.Fatalf("nil repo must be a no-op: %v", err)
	}
	if err := UpsertJob(ctx, repo, Job{}); err == nil {
		t.Fatal("empty id must fail")
	}
}

func TestIsJobRepositoryConcurrencyError(t *testing.T) {
	if !IsJobRepositoryConcurrencyError(ErrJobRepositoryVersionConflict) || !IsJobRepositoryConcurrencyError(ErrJobRepositoryNotFound) {
		t.Fatal("CAS conflict and vanished records are concurrency errors")
	}
	if IsJobRepositoryConcurrencyError(ErrJobRepositoryClosed) || IsJobRepositoryConcurrencyError(nil) {
		t.Fatal("closed repository and nil must not look like a lost race")
	}
}

func TestMemoryJobRepositoryIdempotencyAndClose(t *testing.T) {
	repo := NewMemoryJobRepository()
	ctx := context.Background()
	first, created, err := repo.Admit(ctx, Job{ID: "job-a", Kind: "demo", IdempotencyDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RequestDigest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"})
	if err != nil || !created {
		t.Fatalf("first admit err=%v created=%t", err, created)
	}
	replay, created, err := repo.Admit(ctx, Job{ID: "job-b", Kind: "demo", IdempotencyDigest: first.IdempotencyDigest, RequestDigest: first.RequestDigest})
	if err != nil || created || replay.ID != first.ID {
		t.Fatalf("replay=%#v created=%t err=%v", replay, created, err)
	}
	_, _, err = repo.Admit(ctx, Job{ID: "job-c", Kind: "demo", IdempotencyDigest: first.IdempotencyDigest, RequestDigest: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"})
	if !errors.Is(err, ErrJobIdempotencyConflict) {
		t.Fatalf("conflict err=%v", err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.List(ctx); !errors.Is(err, ErrJobRepositoryClosed) {
		t.Fatalf("closed list err=%v", err)
	}
}
