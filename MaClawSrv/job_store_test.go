package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestFileAsyncJobStoreReplaceAndLoad(t *testing.T) {
	store := &fileAsyncJobStore{path: filepath.Join(t.TempDir(), "state", "jobs.json")}
	created := time.Now().UTC()
	want := []agentruntime.Job{{ID: "job_1", Kind: "demo", Status: agentruntime.JobStatusSucceeded, RecoveryPolicy: agentruntime.JobRecoveryPolicyReconcile, TenantID: "tenant", UserID: "user", CreatedAt: created}}
	if err := store.Replace(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != want[0].ID || got[0].Status != want[0].Status || got[0].RecoveryPolicy != want[0].RecoveryPolicy || !got[0].CreatedAt.Equal(created) {
		t.Fatalf("unexpected job store roundtrip: %#v", got)
	}
}

func TestFileAsyncJobStoreCanceledReplaceKeepsCommittedSnapshot(t *testing.T) {
	store := &fileAsyncJobStore{path: filepath.Join(t.TempDir(), "state", "jobs.json")}
	first := []agentruntime.Job{{ID: "job_first", Status: agentruntime.JobStatusSucceeded, CreatedAt: time.Now().UTC()}}
	if err := store.Replace(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Replace(ctx, []agentruntime.Job{{ID: "job_second", Status: agentruntime.JobStatusSucceeded, CreatedAt: time.Now().UTC()}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled replace error = %v", err)
	}
	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "job_first" {
		t.Fatalf("canceled replace changed committed snapshot: %#v", got)
	}
}

func TestFileAsyncJobStoreMissingSnapshotDistinguishesFirstStartFromBrokenParent(t *testing.T) {
	root := t.TempDir()
	if items, err := (&fileAsyncJobStore{path: filepath.Join(root, "missing", "state", "jobs.json")}).Load(context.Background()); err != nil || len(items) != 0 {
		t.Fatalf("missing first-start snapshot = %#v err=%v", items, err)
	}
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (&fileAsyncJobStore{path: filepath.Join(blocker, "state", "jobs.json")}).Load(context.Background()); err == nil {
		t.Fatal("file in the parent chain was mistaken for an empty store")
	}
}

func TestFileAsyncJobStoreRejectsInvalidCheckpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "jobs.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"items":[{"id":"job_invalid","checkpoint":{"sequence":1,"phase":"prepare","updated_at":"0001-01-01T00:00:00Z"}}]}`)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := (&fileAsyncJobStore{path: path}).Load(context.Background())
	if !errors.Is(err, agentruntime.ErrInvalidJobUpdate) {
		t.Fatalf("invalid checkpoint error = %v", err)
	}
}

func TestFileAsyncJobStoreRejectsInvalidRetryState(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state", "jobs.json")
	store := &fileAsyncJobStore{path: path}
	next := time.Now().UTC()
	invalid := []agentruntime.Job{{
		ID:          "job_invalid_replace",
		RetryPolicy: agentruntime.JobRetryPolicy{MaxAttempts: 3, InitialBackoffMillis: 10, MaximumBackoffMillis: 20},
		Attempt:     3, NextAttemptAt: &next,
	}}
	if err := store.Replace(context.Background(), invalid); !errors.Is(err, agentruntime.ErrInvalidJobRetryPolicy) {
		t.Fatalf("invalid replacement retry state error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("invalid replacement created a durable snapshot: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"items":[{"id":"job_invalid","retry_policy":{"max_attempts":3,"initial_backoff_ms":10,"maximum_backoff_ms":20},"attempt":3,"next_attempt_at":"2026-09-01T10:00:00Z"}]}`)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := (&fileAsyncJobStore{path: path}).Load(context.Background())
	if !errors.Is(err, agentruntime.ErrInvalidJobRetryPolicy) {
		t.Fatalf("invalid retry state error = %v", err)
	}
}

func TestFileAsyncJobStoreRejectsInvalidOrDuplicateIdempotencyState(t *testing.T) {
	identity, err := agentruntime.NewJobIdempotencyIdentity("tenant", "user", "demo", "key", map[string]bool{"same": true})
	if err != nil {
		t.Fatal(err)
	}
	for name, items := range map[string][]agentruntime.Job{
		"response replay marker": {{ID: "job_replay", IdempotencyDigest: identity.Digest, RequestDigest: identity.RequestDigest, IdempotentReplay: true}},
		"duplicate digest": {
			{ID: "job_one", IdempotencyDigest: identity.Digest, RequestDigest: identity.RequestDigest},
			{ID: "job_two", IdempotencyDigest: identity.Digest, RequestDigest: identity.RequestDigest},
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state", "jobs.json")
			err := (&fileAsyncJobStore{path: path}).Replace(context.Background(), items)
			if !errors.Is(err, agentruntime.ErrInvalidJobIdempotency) {
				t.Fatalf("invalid idempotency replacement error = %v", err)
			}
			if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
				t.Fatalf("invalid idempotency snapshot was written: %v", statErr)
			}
		})
	}
}
