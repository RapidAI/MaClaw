package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func newSQLiteAsyncJobRepositoryForTest(t testing.TB, path string) *agentservice.SQLiteJobRepository {
	t.Helper()
	repository, err := newSQLiteAsyncJobRepository(path, "")
	if err != nil {
		t.Fatalf("open SQLite async job repository: %v", err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil {
			t.Errorf("close SQLite async job repository: %v", err)
		}
	})
	return repository
}

func testRepositoryJob(id string, identity agentruntime.JobIdempotencyIdentity) agentruntime.Job {
	return agentruntime.Job{
		ID:                id,
		Kind:              "test.repository",
		Status:            agentruntime.JobStatusPending,
		RecoveryPolicy:    agentruntime.JobRecoveryPolicyFail,
		RetryPolicy:       agentruntime.NormalizeJobRetryPolicy(agentruntime.JobRetryPolicy{}),
		IdempotencyDigest: identity.Digest,
		RequestDigest:     identity.RequestDigest,
		TenantID:          "tenant",
		UserID:            "user",
		CreatedAt:         time.Now().UTC(),
	}
}

func TestSQLiteAsyncJobRepositoryAdmissionIsUniqueAcrossHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "jobs.db")
	first := newSQLiteAsyncJobRepositoryForTest(t, path)
	second := newSQLiteAsyncJobRepositoryForTest(t, path)
	identity, err := agentruntime.NewJobIdempotencyIdentity("tenant", "user", "test.repository", "client-key", map[string]string{"value": "same"})
	if err != nil {
		t.Fatal(err)
	}

	const callers = 32
	start := make(chan struct{})
	results := make([]agentruntime.Job, callers)
	created := make([]bool, callers)
	errs := make([]error, callers)
	var wait sync.WaitGroup
	for i := 0; i < callers; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			repository := agentruntime.JobRepository(first)
			if index%2 == 1 {
				repository = second
			}
			results[index], created[index], errs[index] = repository.Admit(context.Background(), testRepositoryJob(agentservice.NewID("job"), identity))
		}(i)
	}
	close(start)
	wait.Wait()

	canonicalID := ""
	createdCount := 0
	for i := range results {
		if errs[i] != nil {
			t.Fatalf("admission %d: %v", i, errs[i])
		}
		if canonicalID == "" {
			canonicalID = results[i].ID
		}
		if results[i].ID != canonicalID || results[i].Version != 1 || results[i].IdempotentReplay {
			t.Fatalf("non-canonical admission %d: %#v", i, results[i])
		}
		if created[i] {
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Fatalf("created admissions = %d, want 1", createdCount)
	}
	items, err := second.List(context.Background())
	if err != nil || len(items) != 1 || items[0].ID != canonicalID {
		t.Fatalf("durable canonical jobs = %#v err=%v", items, err)
	}

	conflicting, err := agentruntime.NewJobIdempotencyIdentity("tenant", "user", "test.repository", "client-key", map[string]string{"value": "different"})
	if err != nil {
		t.Fatal(err)
	}
	if _, created, err := first.Admit(context.Background(), testRepositoryJob(agentservice.NewID("job"), conflicting)); created || !errors.Is(err, agentruntime.ErrJobIdempotencyConflict) {
		t.Fatalf("conflicting admission created=%v err=%v", created, err)
	}
}

func TestSQLiteAsyncJobRepositoryInitializesConcurrently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "jobs.db")
	const callers = 8
	start := make(chan struct{})
	errs := make([]error, callers)
	var wait sync.WaitGroup
	for i := 0; i < callers; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			repository, err := newSQLiteAsyncJobRepository(path, "")
			errs[index] = err
			if repository != nil {
				if closeErr := repository.Close(); err == nil {
					errs[index] = closeErr
				}
			}
		}(i)
	}
	close(start)
	wait.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("constructor %d: %v", i, err)
		}
	}
	repository := newSQLiteAsyncJobRepositoryForTest(t, path)
	if err := repository.Probe(context.Background()); err != nil {
		t.Fatalf("probe concurrently initialized repository: %v", err)
	}
}

func TestSQLiteAsyncJobRepositoryUpdateUsesVersionCAS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "jobs.db")
	first := newSQLiteAsyncJobRepositoryForTest(t, path)
	second := newSQLiteAsyncJobRepositoryForTest(t, path)
	canonical, created, err := first.Admit(context.Background(), testRepositoryJob("job_cas", agentruntime.JobIdempotencyIdentity{}))
	if err != nil || !created {
		t.Fatalf("admit: created=%v err=%v", created, err)
	}
	winner := canonical
	winner.Status = agentruntime.JobStatusRunning
	winner, err = first.Update(context.Background(), canonical.Version, winner)
	if err != nil || winner.Version != 2 || winner.Status != agentruntime.JobStatusRunning {
		t.Fatalf("winning update = %#v err=%v", winner, err)
	}
	stale := canonical
	stale.Status = agentruntime.JobStatusFailed
	if _, err := second.Update(context.Background(), canonical.Version, stale); !errors.Is(err, agentruntime.ErrJobRepositoryVersionConflict) {
		t.Fatalf("stale update error = %v", err)
	}
	persisted, err := second.Get(context.Background(), canonical.ID)
	if err != nil || persisted.Version != 2 || persisted.Status != agentruntime.JobStatusRunning {
		t.Fatalf("persisted winner = %#v err=%v", persisted, err)
	}
}

func TestSQLiteAsyncJobRepositoryBulkDeleteIsAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "jobs.db")
	repository := newSQLiteAsyncJobRepositoryForTest(t, path)
	first, _, err := repository.Admit(context.Background(), testRepositoryJob("job_delete_one", agentruntime.JobIdempotencyIdentity{}))
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := repository.Admit(context.Background(), testRepositoryJob("job_delete_two", agentruntime.JobIdempotencyIdentity{}))
	if err != nil {
		t.Fatal(err)
	}
	updated := second
	updated.Status = agentruntime.JobStatusRunning
	if _, err := repository.Update(context.Background(), second.Version, updated); err != nil {
		t.Fatal(err)
	}
	err = repository.Delete(context.Background(), []agentruntime.JobVersion{
		{ID: first.ID, Version: first.Version},
		{ID: second.ID, Version: second.Version},
	})
	if !errors.Is(err, agentruntime.ErrJobRepositoryVersionConflict) {
		t.Fatalf("stale bulk delete error = %v", err)
	}
	items, err := repository.List(context.Background())
	if err != nil || len(items) != 2 {
		t.Fatalf("partial bulk delete committed: %#v err=%v", items, err)
	}
}

func TestSQLiteAsyncJobRepositoryImportsLegacySnapshotOnce(t *testing.T) {
	root := t.TempDir()
	legacyPath := filepath.Join(root, "state", "jobs.json")
	legacy := asyncJobSnapshot{Items: []asyncJobView{{
		ID: "job_legacy", Kind: "legacy", Status: asyncJobStatusSucceeded,
		RetryPolicy: agentruntime.NormalizeJobRetryPolicy(agentruntime.JobRetryPolicy{}),
		TenantID:    "tenant", UserID: "user", CreatedAt: time.Now().UTC(),
	}}}
	payload, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "state", "jobs.db")
	repository, err := newSQLiteAsyncJobRepository(path, legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	items, err := repository.List(context.Background())
	if err != nil || len(items) != 1 || items[0].ID != "job_legacy" || items[0].Version != 1 {
		t.Fatalf("imported jobs = %#v err=%v", items, err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("legacy backup was removed: %v", err)
	}
	reopened := newSQLiteAsyncJobRepositoryForTest(t, path)
	items, err = reopened.List(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("legacy snapshot imported more than once: %#v err=%v", items, err)
	}
}

func TestSQLiteAsyncJobRepositoryMigratesVersionOneLeaseColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "jobs.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE async_job_repository_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE async_jobs (
  job_id TEXT PRIMARY KEY,
  version INTEGER NOT NULL CHECK(version > 0),
  tenant_id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  status TEXT NOT NULL,
  idempotency_digest TEXT,
  request_digest TEXT NOT NULL DEFAULT '',
  payload_json BLOB NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
INSERT INTO async_job_repository_meta(key,value) VALUES('schema_version','1'),('legacy_jobs_json_imported','done');`)
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	repository := newSQLiteAsyncJobRepositoryForTest(t, path)
	version, err := repository.MetaValue(context.Background(), "schema_version")
	if err != nil || version != asyncJobRepositorySchemaVersion {
		t.Fatalf("schema version = %q err=%v", version, err)
	}
	job := testRepositoryJob("job_leased", agentruntime.JobIdempotencyIdentity{})
	job.LeaseOwnerID = "executor_one"
	job.LeaseExpiresAt = asyncJobLeaseDeadline(time.Now().UTC())
	if _, created, err := repository.Admit(context.Background(), job); err != nil || !created {
		t.Fatalf("admit leased job after migration: created=%v err=%v", created, err)
	}
}

func TestAsyncJobManagerAdmissionIsUniqueAcrossRepositoryHandles(t *testing.T) {
	root := t.TempDir()
	first := newAsyncJobManager(root)
	second := newAsyncJobManager(root)
	t.Cleanup(first.close)
	t.Cleanup(second.close)
	principal := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	identity, err := agentruntime.NewJobIdempotencyIdentity(principal.TenantID, principal.UserID, "job.cross-process", "shared-key", map[string]bool{"same": true})
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	var calls atomic.Uint32
	run := func(context.Context) (any, error) {
		calls.Add(1)
		<-release
		return map[string]bool{"ok": true}, nil
	}
	canonical, err := first.createUserJobIdempotent("job.cross-process", principal, agentruntime.JobRecoveryPolicyFail, agentruntime.JobRetryPolicy{}, identity, run)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := second.createUserJobIdempotent("job.cross-process", principal, agentruntime.JobRecoveryPolicyFail, agentruntime.JobRetryPolicy{}, identity, run)
	if err != nil || replay.ID != canonical.ID || !replay.IdempotentReplay {
		t.Fatalf("cross-handle replay = %#v err=%v canonical=%#v", replay, err, canonical)
	}
	if calls.Load() > 1 {
		t.Fatalf("workers started = %d, want at most 1", calls.Load())
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, ok := second.getUserJob(canonical.ID, principal)
		if ok && current.Status == asyncJobStatusSucceeded {
			if calls.Load() != 1 {
				t.Fatalf("workers started = %d, want 1", calls.Load())
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("second manager did not observe canonical completion")
}

func TestAsyncJobManagerDoesNotRecoverLiveForeignLease(t *testing.T) {
	root := t.TempDir()
	first := newAsyncJobManager(root)
	t.Cleanup(first.close)
	principal := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	job := first.createUserJobWithRecoveryPolicy("job.foreign-live", principal, agentruntime.JobRecoveryPolicyReconcile, func(context.Context) (any, error) {
		started <- struct{}{}
		<-release
		return map[string]bool{"ok": true}, nil
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for leased worker")
	}
	second := newAsyncJobManager(root)
	t.Cleanup(second.close)
	observed, ok := second.getUserJob(job.ID, principal)
	if !ok || !agentruntime.JobIsActive(observed.Status) || observed.LeaseOwnerID != first.instanceID || observed.LeaseExpiresAt == nil {
		t.Fatalf("live foreign lease was recovered: %#v", observed)
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		observed, ok = second.getUserJob(job.ID, principal)
		if ok && observed.Status == asyncJobStatusSucceeded {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("foreign manager did not observe leased worker completion")
}

func TestAsyncJobManagerRecoversExpiredLeaseAndRejectsLateWorkerResult(t *testing.T) {
	root := t.TempDir()
	first := newAsyncJobManager(root)
	second := newAsyncJobManager(root)
	t.Cleanup(first.close)
	t.Cleanup(second.close)
	principal := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	job := first.createUserJobWithRecoveryPolicy("job.expired-lease", principal, agentruntime.JobRecoveryPolicyReconcile, func(context.Context) (any, error) {
		started <- struct{}{}
		<-release
		return map[string]bool{"late": true}, nil
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for leased worker")
	}
	canonical, err := second.repository.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().UTC().Add(-time.Second)
	canonical.LeaseExpiresAt = &past
	if _, err := second.repository.Update(context.Background(), canonical.Version, canonical); err != nil {
		t.Fatalf("expire foreign lease: %v", err)
	}
	recovered, ok := second.getUserJob(job.ID, principal)
	if !ok || recovered.Status != asyncJobStatusUnknown || recovered.ErrorCode != agentruntime.JobErrorCodeReconcileRequired || recovered.LeaseExpiresAt != nil {
		t.Fatalf("expired lease was not recovered fail-closed: %#v", recovered)
	}
	close(release)
	time.Sleep(50 * time.Millisecond)
	persisted, err := second.repository.Get(context.Background(), job.ID)
	if err != nil || persisted.Status != asyncJobStatusUnknown || persisted.LeaseExpiresAt != nil {
		t.Fatalf("late worker resurrected recovered job: %#v err=%v", persisted, err)
	}
}

func TestAsyncJobManagerRenewsLocalWorkerLease(t *testing.T) {
	root := t.TempDir()
	manager := newAsyncJobManager(root)
	t.Cleanup(manager.close)
	principal := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	job := manager.createUserJob("job.heartbeat", principal, func(context.Context) (any, error) {
		started <- struct{}{}
		<-release
		return nil, nil
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for heartbeat worker")
	}
	before, err := manager.repository.Get(context.Background(), job.ID)
	if err != nil || before.LeaseExpiresAt == nil {
		t.Fatalf("initial lease = %#v err=%v", before, err)
	}
	if active := manager.renewLocalJobLeases(); !active {
		t.Fatal("lease coordinator lost an active local job")
	}
	after, err := manager.repository.Get(context.Background(), job.ID)
	if err != nil || after.Version <= before.Version || after.LeaseExpiresAt == nil || after.LeaseExpiresAt.Before(*before.LeaseExpiresAt) {
		t.Fatalf("renewed lease = %#v, before=%#v err=%v", after, before, err)
	}
	close(release)
}

func TestAsyncJobManagerRestoresPreparedLeaseWhenCanonicalJobDisappears(t *testing.T) {
	root := t.TempDir()
	manager := newAsyncJobManager(root)
	t.Cleanup(manager.close)
	principal := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	job := manager.createUserJob("job.deleted-during-update", principal, func(context.Context) (any, error) {
		started <- struct{}{}
		<-release
		return nil, nil
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for worker")
	}
	canonical, err := manager.repository.Get(context.Background(), job.ID)
	if err != nil || canonical.LeaseExpiresAt == nil {
		t.Fatalf("load canonical job: %#v err=%v", canonical, err)
	}
	manager.mu.Lock()
	before := cloneAsyncJobTime(manager.jobs[job.ID].LeaseExpiresAt)
	manager.mu.Unlock()
	if err := manager.repository.Delete(context.Background(), []agentruntime.JobVersion{{ID: canonical.ID, Version: canonical.Version}}); err != nil {
		t.Fatalf("delete canonical job: %v", err)
	}
	progress := 0.5
	err = manager.reportJobUpdate(context.Background(), job.ID, agentruntime.JobUpdate{Progress: &progress})
	if !errors.Is(err, agentruntime.ErrJobRepositoryNotFound) {
		t.Fatalf("report after canonical delete error = %v", err)
	}
	manager.mu.Lock()
	after := cloneAsyncJobTime(manager.jobs[job.ID].LeaseExpiresAt)
	manager.mu.Unlock()
	if before == nil || after == nil || !before.Equal(*after) {
		t.Fatalf("uncommitted lease mutation was retained: before=%v after=%v", before, after)
	}
	manager.mu.Lock()
	retainedProgress := manager.jobs[job.ID].Progress
	manager.mu.Unlock()
	if retainedProgress != 0 {
		t.Fatalf("uncommitted progress mutation was retained: %v", retainedProgress)
	}
	close(release)
}

type committedEffectJobReconciler struct{}

func (committedEffectJobReconciler) ReconcileJob(_ context.Context, job agentruntime.Job, effects []agentruntime.JobEffect) (agentruntime.JobReconcileResult, error) {
	if len(effects) != 1 || effects[0].JobID != job.ID || effects[0].State != agentruntime.JobEffectCommitted || effects[0].ResourceID != "remote-1" {
		return agentruntime.JobReconcileResult{}, nil
	}
	return agentruntime.JobReconcileResult{Resolved: true, Status: agentruntime.JobStatusSucceeded, Result: json.RawMessage(`{"resource_id":"remote-1","reconciled":true}`)}, nil
}

func TestAsyncJobManagerReconcilesUnknownFromProtectedCommittedEffect(t *testing.T) {
	root := t.TempDir()
	first := newAsyncJobManager(root)
	second := newAsyncJobManager(root)
	t.Cleanup(first.close)
	t.Cleanup(second.close)
	principal := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	job := first.createUserJobWithRecoveryPolicy("job.effect-reconcile", principal, agentruntime.JobRecoveryPolicyReconcile, func(ctx context.Context) (any, error) {
		if _, err := agentruntime.PrepareJobEffect(ctx, "remote.effect", map[string]string{"operation": "create"}); err != nil {
			return nil, err
		}
		if _, err := agentruntime.BindJobEffectResource(ctx, "remote.effect", "remote-1"); err != nil {
			return nil, err
		}
		if _, err := agentruntime.SettleJobEffect(ctx, "remote.effect", agentruntime.JobEffectCommitted, "remote-1:committed", "remote_committed", map[string]string{"status": "ready"}); err != nil {
			return nil, err
		}
		started <- struct{}{}
		<-release
		return map[string]bool{"late": true}, nil
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for committed effect")
	}
	canonical, err := second.repository.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().UTC().Add(-time.Second)
	canonical.LeaseExpiresAt = &past
	if _, err := second.repository.Update(context.Background(), canonical.Version, canonical); err != nil {
		t.Fatalf("expire worker lease: %v", err)
	}
	unknown, ok := second.getUserJob(job.ID, principal)
	if !ok || unknown.Status != asyncJobStatusUnknown {
		t.Fatalf("job did not enter unknown before domain reconciliation: %#v", unknown)
	}
	reconciled, ok, err := second.reconcileUserJob(context.Background(), job.ID, principal, committedEffectJobReconciler{})
	if err != nil || !ok || reconciled.Status != asyncJobStatusSucceeded || !strings.Contains(string(reconciled.Result), `"reconciled":true`) {
		t.Fatalf("reconciled job = %#v ok=%v err=%v", reconciled, ok, err)
	}
	close(release)
	time.Sleep(50 * time.Millisecond)
	persisted, err := second.repository.Get(context.Background(), job.ID)
	if err != nil || persisted.Status != asyncJobStatusSucceeded || !strings.Contains(string(persisted.Result), `"reconciled":true`) {
		t.Fatalf("late worker replaced reconciled result: %#v err=%v", persisted, err)
	}
}
