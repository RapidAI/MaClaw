package main

import (
	"context"
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

func loadPersistedAsyncJobsForTest(t testing.TB, mgr *asyncJobManager) []agentruntime.Job {
	t.Helper()
	mgr.mu.RLock()
	repository := mgr.repository
	mgr.mu.RUnlock()
	if repository == nil {
		t.Fatal("async job repository is unavailable")
	}
	items, err := repository.List(context.Background())
	if err != nil {
		t.Fatalf("load persisted jobs: %v", err)
	}
	return items
}

func TestAsyncJobManagerCancelRunningJob(t *testing.T) {
	mgr := newAsyncJobManager(t.TempDir())
	t.Cleanup(mgr.close)
	principal := agentservice.Principal{TenantID: "tenant_1", UserID: "user_1"}
	started := make(chan struct{}, 1)
	job := mgr.createUserJob("demo.cancel", principal, func(ctx context.Context) (any, error) {
		started <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for job start")
	}
	canceled, ok := mgr.cancelUserJob(job.ID, principal)
	if !ok {
		t.Fatalf("cancelUserJob should find job")
	}
	if canceled.ID != job.ID {
		t.Fatalf("unexpected canceled job: %#v", canceled)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, ok := mgr.getUserJob(job.ID, principal)
		if !ok {
			t.Fatalf("job disappeared before terminal state")
		}
		if current.Status == asyncJobStatusCanceled {
			if current.Error != "job canceled" {
				t.Fatalf("unexpected canceled error: %#v", current)
			}
			if current.ErrorCode != "job_canceled" {
				t.Fatalf("unexpected canceled error code: %#v", current)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for canceled status")
}

func TestAsyncJobManagerListUserJobs(t *testing.T) {
	mgr := newAsyncJobManager(t.TempDir())
	t.Cleanup(mgr.close)
	p1 := agentservice.Principal{TenantID: "tenant_1", UserID: "user_1"}
	p2 := agentservice.Principal{TenantID: "tenant_1", UserID: "user_2"}
	mgr.createUserJob("job.one", p1, func(ctx context.Context) (any, error) {
		return map[string]string{"status": "one"}, nil
	})
	time.Sleep(10 * time.Millisecond)
	mgr.createUserJob("job.two", p1, func(ctx context.Context) (any, error) {
		return map[string]string{"status": "two"}, nil
	})
	otherJob := mgr.createUserJob("job.other", p2, func(ctx context.Context) (any, error) {
		return map[string]string{"status": "other"}, nil
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		items := mgr.listUserJobs(p1, "", "")
		other, ok := mgr.getUserJob(otherJob.ID, p2)
		if len(items) == 2 && items[0].UserID == p1.UserID && items[1].UserID == p1.UserID {
			if !items[0].CreatedAt.Before(items[1].CreatedAt) && !items[0].CreatedAt.Equal(items[1].CreatedAt) {
				t.Fatalf("jobs not sorted by created_at: %#v", items)
			}
			if ok && other.Status == asyncJobStatusSucceeded && items[0].Status == asyncJobStatusSucceeded && items[1].Status == asyncJobStatusSucceeded {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for user job list")
}

func TestAsyncJobManagerPersistsCompletedJobs(t *testing.T) {
	root := t.TempDir()
	mgr := newAsyncJobManager(root)
	t.Cleanup(mgr.close)
	principal := agentservice.Principal{TenantID: "tenant_1", UserID: "user_1"}
	job := mgr.createUserJob("job.persist", principal, func(ctx context.Context) (any, error) {
		return map[string]string{"status": "persisted"}, nil
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, ok := mgr.getUserJob(job.ID, principal)
		if ok && current.Status == asyncJobStatusSucceeded {
			loaded := newAsyncJobManager(root)
			defer loaded.close()
			restored, ok := loaded.getUserJob(job.ID, principal)
			if !ok {
				t.Fatalf("expected persisted job to reload")
			}
			if restored.Status != asyncJobStatusSucceeded {
				t.Fatalf("unexpected restored status: %#v", restored)
			}
			var result map[string]string
			if err := json.Unmarshal(restored.Result, &result); err != nil {
				t.Fatalf("unmarshal restored result: %v", err)
			}
			if result["status"] != "persisted" {
				t.Fatalf("unexpected restored result: %#v", result)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for persisted job")
}

func TestAsyncJobManagerListUserJobsWithFilters(t *testing.T) {
	mgr := newAsyncJobManager(t.TempDir())
	t.Cleanup(mgr.close)
	p := agentservice.Principal{TenantID: "tenant_1", UserID: "user_1"}
	mgr.createUserJob("skill.import", p, func(ctx context.Context) (any, error) {
		return map[string]string{"status": "ok"}, nil
	})
	time.Sleep(10 * time.Millisecond)
	mgr.createUserJob("mcp.start", p, func(ctx context.Context) (any, error) {
		return map[string]string{"status": "ok"}, nil
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		kindItems := mgr.listUserJobs(p, "skill.import", "")
		statusItems := mgr.listUserJobs(p, "", asyncJobStatusSucceeded)
		if len(kindItems) == 1 && kindItems[0].Kind == "skill.import" && len(statusItems) == 2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for filtered job list")
}

func TestAsyncJobManagerDeleteCompletedJob(t *testing.T) {
	mgr := newAsyncJobManager(t.TempDir())
	t.Cleanup(mgr.close)
	p := agentservice.Principal{TenantID: "tenant_1", UserID: "user_1"}
	job := mgr.createUserJob("job.delete", p, func(ctx context.Context) (any, error) {
		return map[string]string{"status": "done"}, nil
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, ok := mgr.getUserJob(job.ID, p)
		if ok && current.Status == asyncJobStatusSucceeded {
			deletedJob, found, deleted := mgr.deleteUserJob(job.ID, p)
			if !found || !deleted {
				t.Fatalf("expected completed job to be deleted")
			}
			if deletedJob.ID != job.ID {
				t.Fatalf("unexpected deleted job: %#v", deletedJob)
			}
			if _, ok := mgr.getUserJob(job.ID, p); ok {
				t.Fatalf("expected deleted job to disappear")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for completed job")
}

func TestAsyncJobManagerDeleteActiveJobRejected(t *testing.T) {
	mgr := newAsyncJobManager(t.TempDir())
	t.Cleanup(mgr.close)
	p := agentservice.Principal{TenantID: "tenant_1", UserID: "user_1"}
	started := make(chan struct{}, 1)
	job := mgr.createUserJob("job.active", p, func(ctx context.Context) (any, error) {
		started <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for active job")
	}
	_, found, deleted := mgr.deleteUserJob(job.ID, p)
	if !found {
		t.Fatalf("expected active job to be found")
	}
	if deleted {
		t.Fatalf("active job should not be deleted")
	}
	_, _ = mgr.cancelUserJob(job.ID, p)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, ok := mgr.getUserJob(job.ID, p)
		if ok && current.Status == asyncJobStatusCanceled {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for canceled job")
}

func TestAsyncJobManagerDeleteJobsWithFilters(t *testing.T) {
	mgr := newAsyncJobManager(t.TempDir())
	t.Cleanup(mgr.close)
	p := agentservice.Principal{TenantID: "tenant_1", UserID: "user_1"}
	other := agentservice.Principal{TenantID: "tenant_1", UserID: "user_2"}
	oldJob := mgr.createUserJob("skill.import", p, func(ctx context.Context) (any, error) {
		return map[string]string{"status": "old"}, nil
	})
	time.Sleep(10 * time.Millisecond)
	keepJob := mgr.createUserJob("mcp.start", p, func(ctx context.Context) (any, error) {
		return map[string]string{"status": "keep"}, nil
	})
	otherJob := mgr.createUserJob("skill.import", other, func(ctx context.Context) (any, error) {
		return map[string]string{"status": "other"}, nil
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		oldCurrent, oldOK := mgr.getUserJob(oldJob.ID, p)
		keepCurrent, keepOK := mgr.getUserJob(keepJob.ID, p)
		otherCurrent, otherOK := mgr.getUserJob(otherJob.ID, other)
		if oldOK && keepOK && otherOK && oldCurrent.Status == asyncJobStatusSucceeded && keepCurrent.Status == asyncJobStatusSucceeded && otherCurrent.Status == asyncJobStatusSucceeded {
			before := keepCurrent.CreatedAt
			deleted := mgr.deleteUserJobs(p, "skill.import", asyncJobStatusSucceeded, &before)
			if len(deleted) != 1 || deleted[0].ID != oldJob.ID {
				t.Fatalf("unexpected deleted jobs: %#v", deleted)
			}
			if _, ok := mgr.getUserJob(oldJob.ID, p); ok {
				t.Fatalf("expected old job to be deleted")
			}
			if _, ok := mgr.getUserJob(keepJob.ID, p); !ok {
				t.Fatalf("expected keep job to remain")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for completed jobs")
}

func TestAsyncJobManagerPersistenceFailureFailsClosed(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	mgr := newAsyncJobManager(blocker)
	t.Cleanup(mgr.close)
	p := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	executed := make(chan struct{}, 1)
	job := mgr.createUserJob("job.persistence", p, func(context.Context) (any, error) {
		executed <- struct{}{}
		return nil, nil
	})
	select {
	case <-executed:
		t.Fatal("job must not execute when admission cannot be persisted")
	default:
	}
	if job.Status != asyncJobStatusFailed {
		t.Fatalf("expected fail-closed status, got %#v", job)
	}
	if job.ErrorCode != "job_persistence_failed" {
		t.Fatalf("unexpected persistence error code: %#v", job)
	}
	if mgr.persistenceHealthy() {
		t.Fatal("manager should report unhealthy persistence")
	}
}

func TestAsyncJobManagerMarksInFlightJobsAfterRestart(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	created := time.Now().UTC().Add(-time.Minute)
	nextAttempt := created.Add(10 * time.Second)
	payload, err := json.Marshal(asyncJobSnapshot{Items: []asyncJobView{{
		ID: "job_restart", Kind: "demo", Status: asyncJobStatusRunning,
		TenantID: "tenant", UserID: "user", CreatedAt: created,
		RetryPolicy: agentruntime.JobRetryPolicy{MaxAttempts: 3, InitialBackoffMillis: 10, MaximumBackoffMillis: 20},
		Attempt:     1, NextAttemptAt: &nextAttempt,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "jobs.json"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	mgr := newAsyncJobManager(root)
	t.Cleanup(mgr.close)
	job, ok := mgr.getUserJob("job_restart", agentservice.Principal{TenantID: "tenant", UserID: "user"})
	if !ok {
		t.Fatal("expected restarted job to be discoverable")
	}
	if job.Status != asyncJobStatusUnknown || job.ErrorCode != agentruntime.JobErrorCodeReconcileRequired {
		t.Fatalf("expected reconcile-required unknown state, got %#v", job)
	}
	if job.RecoveryPolicy != agentruntime.JobRecoveryPolicyReconcile {
		t.Fatalf("legacy job did not default to fail-closed recovery: %#v", job)
	}
	if job.Attempt != 1 || job.NextAttemptAt != nil {
		t.Fatalf("restart replayed or retained an executable retry schedule: %#v", job)
	}
}

func TestAsyncJobManagerCanFailKnownSideEffectFreeJobAfterRestart(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	created := time.Now().UTC().Add(-time.Minute)
	payload, err := json.Marshal(asyncJobSnapshot{Items: []asyncJobView{{
		ID: "job_restart_fail", Kind: "health_check", Status: asyncJobStatusRunning,
		RecoveryPolicy: agentruntime.JobRecoveryPolicyFail,
		TenantID:       "tenant", UserID: "user", CreatedAt: created,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "jobs.json"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	mgr := newAsyncJobManager(root)
	t.Cleanup(mgr.close)
	job, ok := mgr.getUserJob("job_restart_fail", agentservice.Principal{TenantID: "tenant", UserID: "user"})
	if !ok {
		t.Fatal("expected restarted job to be discoverable")
	}
	if job.Status != asyncJobStatusFailed || job.ErrorCode != agentruntime.JobErrorCodeServiceRestarted || job.CompletedAt == nil {
		t.Fatalf("expected explicit fail recovery policy, got %#v", job)
	}
}

func TestAsyncJobManagerDoesNotAutoPruneUnknownJobs(t *testing.T) {
	mgr := newAsyncJobManager(t.TempDir())
	t.Cleanup(mgr.close)
	created := time.Now().UTC().Add(-2 * asyncJobRetention)
	completed := created
	mgr.mu.Lock()
	mgr.jobs["job_reconcile"] = &asyncJobRecord{asyncJobView: asyncJobView{
		ID: "job_reconcile", Kind: "demo", Status: asyncJobStatusUnknown,
		RecoveryPolicy: agentruntime.JobRecoveryPolicyReconcile,
		ErrorCode:      agentruntime.JobErrorCodeReconcileRequired,
		TenantID:       "tenant", UserID: "user", CreatedAt: created, CompletedAt: &completed,
	}}
	mgr.pruneLocked(time.Now().UTC())
	_, present := mgr.jobs["job_reconcile"]
	mgr.mu.Unlock()
	if !present {
		t.Fatal("unknown reconciliation evidence must not be pruned automatically")
	}
}

func TestAsyncJobManagerPruneRollsBackOnPersistenceFailure(t *testing.T) {
	root := t.TempDir()
	mgr := newAsyncJobManager(root)
	t.Cleanup(mgr.close)
	blocker := filepath.Join(root, "persist-blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	created := time.Now().UTC().Add(-2 * asyncJobRetention)
	mgr.mu.Lock()
	mgr.jobs["job_expired"] = &asyncJobRecord{asyncJobView: asyncJobView{
		ID: "job_expired", Kind: "demo", Status: asyncJobStatusSucceeded,
		TenantID: "tenant", UserID: "user", CreatedAt: created,
		CompletedAt: &created,
	}}
	mgr.filePath = filepath.Join(blocker, "state", "jobs.json")
	mgr.store = &fileAsyncJobStore{path: mgr.filePath}
	mgr.pruneLocked(time.Now().UTC())
	_, stillPresent := mgr.jobs["job_expired"]
	mgr.mu.Unlock()
	if !stillPresent {
		t.Fatal("prune must restore records when durable deletion fails")
	}
	if mgr.persistenceHealthy() {
		t.Fatal("failed prune should expose unhealthy persistence")
	}
}

func TestAsyncJobManagerDeleteRollsBackOnPersistenceFailure(t *testing.T) {
	root := t.TempDir()
	mgr := newAsyncJobManager(root)
	t.Cleanup(mgr.close)
	p := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	job := mgr.createUserJob("job.delete-persist", p, func(context.Context) (any, error) {
		return map[string]string{"status": "done"}, nil
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, ok := mgr.getUserJob(job.ID, p)
		if ok && current.Status == asyncJobStatusSucceeded {
			blocker := filepath.Join(root, "delete-blocker")
			if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			mgr.mu.Lock()
			mgr.filePath = filepath.Join(blocker, "state", "jobs.json")
			mgr.store = &fileAsyncJobStore{path: mgr.filePath}
			mgr.mu.Unlock()
			_, found, deleted := mgr.deleteUserJob(job.ID, p)
			if !found || deleted {
				t.Fatalf("delete should fail closed: found=%v deleted=%v", found, deleted)
			}
			if _, stillPresent := mgr.getUserJob(job.ID, p); !stillPresent {
				t.Fatal("failed durable delete must keep the record visible")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for completed job")
}

func TestAsyncJobManagerDeleteRemovesProtectedEffects(t *testing.T) {
	mgr := newAsyncJobManager(t.TempDir())
	t.Cleanup(mgr.close)
	p := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	job := mgr.createUserJob("mcp.create", p, func(ctx context.Context) (any, error) {
		if _, err := agentruntime.PrepareJobEffect(ctx, "mcp.create", map[string]string{"operation": "create"}); err != nil {
			return nil, err
		}
		if _, err := agentruntime.BindJobEffectResource(ctx, "mcp.create", "server-1"); err != nil {
			return nil, err
		}
		if _, err := agentruntime.SettleJobEffect(ctx, "mcp.create", agentruntime.JobEffectCommitted, "server-1:created", "", map[string]string{"status": "ready"}); err != nil {
			return nil, err
		}
		return map[string]string{"status": "ready"}, nil
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, ok := mgr.getUserJob(job.ID, p)
		if ok && current.Status == asyncJobStatusSucceeded {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if current, ok := mgr.getUserJob(job.ID, p); !ok || current.Status != asyncJobStatusSucceeded {
		t.Fatalf("job did not complete before deletion: %#v ok=%v", current, ok)
	}
	effects, err := mgr.effectRepository.ListByJob(context.Background(), job.ID)
	if err != nil || len(effects) != 1 {
		t.Fatalf("expected protected effect before deletion, effects=%#v err=%v", effects, err)
	}
	if _, found, deleted := mgr.deleteUserJob(job.ID, p); !found || !deleted {
		t.Fatalf("deleteUserJob found=%v deleted=%v", found, deleted)
	}
	remaining, err := mgr.effectRepository.ListByJob(context.Background(), job.ID)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("expected protected effects to be removed, remaining=%#v err=%v", remaining, err)
	}
}

type flakyDeleteEffectRepository struct {
	agentruntime.JobEffectRepository
	failures atomic.Int32
}

func (r *flakyDeleteEffectRepository) DeleteByJobs(ctx context.Context, jobIDs []string) error {
	if r.failures.Load() > 0 {
		r.failures.Add(-1)
		return errors.New("injected effect cleanup failure")
	}
	return r.JobEffectRepository.DeleteByJobs(ctx, jobIDs)
}

func TestAsyncJobManagerRetriesProtectedEffectCleanupAfterFailure(t *testing.T) {
	mgr := newAsyncJobManager(t.TempDir())
	t.Cleanup(mgr.close)
	p := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	job := mgr.createUserJob("mcp.create", p, func(ctx context.Context) (any, error) {
		if _, err := agentruntime.PrepareJobEffect(ctx, "mcp.create", map[string]string{"operation": "create"}); err != nil {
			return nil, err
		}
		if _, err := agentruntime.BindJobEffectResource(ctx, "mcp.create", "server-cleanup"); err != nil {
			return nil, err
		}
		if _, err := agentruntime.SettleJobEffect(ctx, "mcp.create", agentruntime.JobEffectCommitted, "server-cleanup:created", "", map[string]string{"status": "ready"}); err != nil {
			return nil, err
		}
		return map[string]string{"status": "ready"}, nil
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if current, ok := mgr.getUserJob(job.ID, p); ok && current.Status == asyncJobStatusSucceeded {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if current, ok := mgr.getUserJob(job.ID, p); !ok || current.Status != asyncJobStatusSucceeded {
		t.Fatalf("job did not complete before cleanup test: %#v ok=%v", current, ok)
	}
	real := mgr.effectRepository
	flaky := &flakyDeleteEffectRepository{JobEffectRepository: real}
	flaky.failures.Store(2)
	mgr.mu.Lock()
	mgr.effectRepository = flaky
	mgr.mu.Unlock()
	if _, found, deleted := mgr.deleteUserJob(job.ID, p); !found || !deleted {
		t.Fatalf("job deletion should succeed despite effect cleanup failure: found=%v deleted=%v", found, deleted)
	}
	remaining, err := real.ListByJob(context.Background(), job.ID)
	if err != nil || len(remaining) != 1 {
		t.Fatalf("expected protected effect to remain for retry, effects=%#v err=%v", remaining, err)
	}
	mgr.mu.RLock()
	_, pending := mgr.effectCleanupPending[job.ID]
	mgr.mu.RUnlock()
	if !pending {
		t.Fatal("failed cleanup was not queued for retry")
	}
	// Any normal read/housekeeping pass retries the durable cleanup without
	// resurrecting the deleted canonical Job. The second injected failure keeps
	// the intent pending so the restart path below exercises the durable marker.
	_ = mgr.listUserJobs(p, "", "")
	remaining, err = real.ListByJob(context.Background(), job.ID)
	if err != nil || len(remaining) != 1 {
		t.Fatalf("unexpected effect state after injected retry failure, effects=%#v err=%v", remaining, err)
	}
	mgr.mu.RLock()
	_, pending = mgr.effectCleanupPending[job.ID]
	mgr.mu.RUnlock()
	if !pending {
		t.Fatal("failed retry unexpectedly cleared cleanup pending")
	}
	// The retry intent is durable: a process restart must still clean orphaned
	// protected rows even though the canonical Job is already gone.
	mgr.close()
	restarted := newAsyncJobManager(mgr.dataRoot)
	t.Cleanup(restarted.close)
	_ = restarted.listUserJobs(p, "", "")
	remaining, err = restarted.effectRepository.ListByJob(context.Background(), job.ID)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("restart did not reconcile persisted cleanup intent, effects=%#v err=%v", remaining, err)
	}
}

func TestAsyncJobManagerCorruptStoreFailsReadiness(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "jobs.json"), []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	mgr := newAsyncJobManager(root)
	t.Cleanup(mgr.close)
	if mgr.persistenceHealthy() {
		t.Fatal("corrupt job store must be reported unhealthy")
	}
	if _, ok := mgr.getAnyJob("unknown"); ok {
		t.Fatal("corrupt store must not invent jobs")
	}
}

func TestAsyncJobManagerFirstLoadDoesNotRecreateMissingDataRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	mgr := newAsyncJobManager(root)
	t.Cleanup(mgr.close)
	if mgr.persistenceHealthy() {
		t.Fatal("a missing data root must fail closed")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("job manager construction recreated missing data root: %v", err)
	}
}

func TestAsyncJobManagerProvidesDurableSharedReporter(t *testing.T) {
	root := t.TempDir()
	mgr := newAsyncJobManager(root)
	t.Cleanup(mgr.close)
	principal := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	reported := make(chan error, 1)
	release := make(chan struct{})
	job := mgr.createUserJob("job.reporter", principal, func(ctx context.Context) (any, error) {
		progress := 0.4
		err := agentruntime.ReportJobUpdate(ctx, agentruntime.JobUpdate{
			Progress:     &progress,
			ProgressText: "transferring",
			Checkpoint:   &agentruntime.JobCheckpoint{Sequence: 1, Phase: "transfer"},
		})
		reported <- err
		<-release
		return map[string]string{"status": "done"}, nil
	})
	defer close(release)
	if err := <-reported; err != nil {
		t.Fatalf("ReportJobUpdate: %v", err)
	}

	current, ok := mgr.getUserJob(job.ID, principal)
	if !ok || current.Progress != 0.4 || current.ProgressText != "transferring" || current.Checkpoint == nil {
		t.Fatalf("unexpected reported job snapshot: %#v", current)
	}
	if current.Checkpoint.Sequence != 1 || current.Checkpoint.Phase != "transfer" || current.Checkpoint.UpdatedAt.IsZero() {
		t.Fatalf("unexpected checkpoint: %#v", current.Checkpoint)
	}

	persisted := loadPersistedAsyncJobsForTest(t, mgr)
	if len(persisted) != 1 || persisted[0].ID != job.ID || persisted[0].Progress != 0.4 || persisted[0].Checkpoint == nil || persisted[0].Checkpoint.Phase != "transfer" {
		t.Fatalf("unexpected persisted job: %#v", persisted)
	}

	current.Checkpoint.Phase = "polluted"
	again, ok := mgr.getUserJob(job.ID, principal)
	if !ok || again.Checkpoint == nil || again.Checkpoint.Phase != "transfer" {
		t.Fatalf("caller mutated manager checkpoint: %#v", again)
	}
}

func TestAsyncJobManagerRejectsStaleCheckpointWithoutPartialProgress(t *testing.T) {
	mgr := newAsyncJobManager(t.TempDir())
	t.Cleanup(mgr.close)
	principal := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	reported := make(chan [2]error, 1)
	release := make(chan struct{})
	job := mgr.createUserJob("job.stale-checkpoint", principal, func(ctx context.Context) (any, error) {
		firstProgress := 0.5
		firstErr := agentruntime.ReportJobUpdate(ctx, agentruntime.JobUpdate{
			Progress: &firstProgress,
			Checkpoint: &agentruntime.JobCheckpoint{
				Sequence: 2,
				Phase:    "transfer",
			},
		})
		staleProgress := 0.9
		staleErr := agentruntime.ReportJobUpdate(ctx, agentruntime.JobUpdate{
			Progress: &staleProgress,
			Checkpoint: &agentruntime.JobCheckpoint{
				Sequence: 1,
				Phase:    "verify",
			},
		})
		reported <- [2]error{firstErr, staleErr}
		<-release
		return nil, nil
	})
	defer close(release)
	errs := <-reported
	if errs[0] != nil {
		t.Fatalf("initial checkpoint update: %v", errs[0])
	}
	if !errors.Is(errs[1], agentruntime.ErrStaleJobCheckpoint) {
		t.Fatalf("stale checkpoint error = %v", errs[1])
	}
	current, ok := mgr.getUserJob(job.ID, principal)
	if !ok || current.Progress != 0.5 || current.Checkpoint == nil || current.Checkpoint.Sequence != 2 || current.Checkpoint.Phase != "transfer" {
		t.Fatalf("stale update partially changed job: %#v", current)
	}
}

func TestAsyncJobManagerRollsBackReporterUpdateOnPersistenceFailure(t *testing.T) {
	root := t.TempDir()
	mgr := newAsyncJobManager(root)
	t.Cleanup(mgr.close)
	principal := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	firstDone := make(chan error, 1)
	continueSecond := make(chan struct{})
	secondDone := make(chan error, 1)
	release := make(chan struct{})
	job := mgr.createUserJob("job.reporter-rollback", principal, func(ctx context.Context) (any, error) {
		firstProgress := 0.25
		firstDone <- agentruntime.ReportJobUpdate(ctx, agentruntime.JobUpdate{
			Progress:     &firstProgress,
			ProgressText: "prepared",
			Checkpoint:   &agentruntime.JobCheckpoint{Sequence: 1, Phase: "prepare"},
		})
		<-continueSecond
		secondProgress := 0.75
		secondDone <- agentruntime.ReportJobUpdate(ctx, agentruntime.JobUpdate{
			Progress:     &secondProgress,
			ProgressText: "verified",
			Checkpoint:   &agentruntime.JobCheckpoint{Sequence: 2, Phase: "verify"},
		})
		<-release
		return nil, nil
	})
	defer close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("initial reporter update: %v", err)
	}
	blocker := filepath.Join(root, "reporter-blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	mgr.mu.Lock()
	mgr.filePath = filepath.Join(blocker, "state", "jobs.json")
	mgr.store = &fileAsyncJobStore{path: mgr.filePath}
	mgr.mu.Unlock()
	close(continueSecond)
	if err := <-secondDone; !errors.Is(err, agentservice.ErrJobPersistence) {
		t.Fatalf("persistence failure = %v", err)
	}
	current, ok := mgr.getUserJob(job.ID, principal)
	if !ok || current.Progress != 0.25 || current.ProgressText != "prepared" || current.Checkpoint == nil || current.Checkpoint.Sequence != 1 || current.Checkpoint.Phase != "prepare" {
		t.Fatalf("failed durable update was not rolled back: %#v", current)
	}
}

func TestAsyncJobManagerRetriesOnlyExplicitRetryableErrors(t *testing.T) {
	root := t.TempDir()
	mgr := newAsyncJobManager(root)
	t.Cleanup(mgr.close)
	principal := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	policy := agentruntime.JobRetryPolicy{MaxAttempts: 3, InitialBackoffMillis: 1, MaximumBackoffMillis: 2}
	var calls atomic.Uint32
	job := mgr.createUserJobWithPolicies("job.retryable", principal, agentruntime.JobRecoveryPolicyFail, policy, func(ctx context.Context) (any, error) {
		call := calls.Add(1)
		if got := agentruntime.JobAttemptFromContext(ctx); got != call {
			return nil, errors.New("job attempt context mismatch")
		}
		if call < 3 {
			return nil, agentruntime.MarkJobErrorRetryable(errors.New("temporary dependency failure"))
		}
		return map[string]uint32{"attempt": call}, nil
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, ok := mgr.getUserJob(job.ID, principal)
		if ok && current.Status == asyncJobStatusSucceeded {
			if current.Attempt != 3 || calls.Load() != 3 || current.NextAttemptAt != nil {
				t.Fatalf("unexpected retry state: %#v calls=%d", current, calls.Load())
			}
			if current.RetryPolicy != policy || current.LastAttemptErrorCode == "" || current.LastAttemptError == "" {
				t.Fatalf("retry audit fields were not retained: %#v", current)
			}
			persisted := loadPersistedAsyncJobsForTest(t, mgr)
			if len(persisted) != 1 || persisted[0].Attempt != 3 || persisted[0].Status != agentruntime.JobStatusSucceeded {
				t.Fatalf("unexpected persisted retry result: %#v", persisted)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for retried job: calls=%d", calls.Load())
}

func TestAsyncJobManagerDoesNotRetryUnmarkedErrors(t *testing.T) {
	mgr := newAsyncJobManager(t.TempDir())
	t.Cleanup(mgr.close)
	principal := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	policy := agentruntime.JobRetryPolicy{MaxAttempts: 3, InitialBackoffMillis: 1, MaximumBackoffMillis: 2}
	var calls atomic.Uint32
	job := mgr.createUserJobWithPolicies("job.permanent", principal, agentruntime.JobRecoveryPolicyFail, policy, func(context.Context) (any, error) {
		calls.Add(1)
		return nil, errors.New("permanent failure")
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, ok := mgr.getUserJob(job.ID, principal)
		if ok && current.Status == asyncJobStatusFailed {
			if current.Attempt != 1 || calls.Load() != 1 || current.NextAttemptAt != nil {
				t.Fatalf("plain error was retried: %#v calls=%d", current, calls.Load())
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for permanent failure")
}

func TestAsyncJobManagerCancellationInterruptsRetryBackoff(t *testing.T) {
	mgr := newAsyncJobManager(t.TempDir())
	t.Cleanup(mgr.close)
	principal := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	policy := agentruntime.JobRetryPolicy{MaxAttempts: 3, InitialBackoffMillis: 5000, MaximumBackoffMillis: 5000}
	var calls atomic.Uint32
	job := mgr.createUserJobWithPolicies("job.cancel-backoff", principal, agentruntime.JobRecoveryPolicyFail, policy, func(context.Context) (any, error) {
		calls.Add(1)
		return nil, agentruntime.MarkJobErrorRetryable(errors.New("temporary failure"))
	})

	cancelRequested := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, ok := mgr.getUserJob(job.ID, principal)
		if ok && current.Status == asyncJobStatusPending && current.NextAttemptAt != nil {
			if _, found := mgr.cancelUserJob(job.ID, principal); !found {
				t.Fatal("retrying job disappeared before cancellation")
			}
			cancelRequested = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !cancelRequested {
		t.Fatal("job never entered retry backoff")
	}

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, ok := mgr.getUserJob(job.ID, principal)
		if ok && current.Status == asyncJobStatusCanceled {
			if current.Attempt != 1 || calls.Load() != 1 || current.NextAttemptAt != nil {
				t.Fatalf("canceled backoff executed again: %#v calls=%d", current, calls.Load())
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for retry backoff cancellation")
}

func TestAsyncJobManagerFailsClosedWhenRetryScheduleCannotPersist(t *testing.T) {
	root := t.TempDir()
	mgr := newAsyncJobManager(root)
	t.Cleanup(mgr.close)
	principal := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	policy := agentruntime.JobRetryPolicy{MaxAttempts: 3, InitialBackoffMillis: 1, MaximumBackoffMillis: 2}
	ready := make(chan struct{})
	release := make(chan struct{})
	job := mgr.createUserJobWithPolicies("job.retry-persist", principal, agentruntime.JobRecoveryPolicyFail, policy, func(context.Context) (any, error) {
		close(ready)
		<-release
		return nil, agentruntime.MarkJobErrorRetryable(errors.New("temporary failure"))
	})
	<-ready
	blocker := filepath.Join(root, "retry-blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	mgr.mu.Lock()
	mgr.filePath = filepath.Join(blocker, "state", "jobs.json")
	mgr.store = &fileAsyncJobStore{path: mgr.filePath}
	mgr.mu.Unlock()
	close(release)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, ok := mgr.getUserJob(job.ID, principal)
		if ok && current.Status == asyncJobStatusFailed {
			if current.ErrorCode != agentruntime.JobErrorCodePersistenceFailed || current.Attempt != 1 || current.NextAttemptAt != nil {
				t.Fatalf("retry persistence failure was not fail-closed: %#v", current)
			}
			if mgr.persistenceHealthy() {
				t.Fatal("retry scheduling persistence failure did not affect readiness")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for retry persistence failure")
}

func TestAsyncJobManagerIdempotentAdmissionIsAtomic(t *testing.T) {
	root := t.TempDir()
	mgr := newAsyncJobManager(root)
	t.Cleanup(mgr.close)
	principal := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	identity, err := agentruntime.NewJobIdempotencyIdentity(principal.TenantID, principal.UserID, "job.idempotent", "client-key", map[string]string{"value": "same"})
	if err != nil {
		t.Fatal(err)
	}
	const callers = 32
	jobs := make([]*asyncJobRecord, callers)
	errs := make([]error, callers)
	start := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Uint32
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			jobs[index], errs[index] = mgr.createUserJobIdempotent("job.idempotent", principal, agentruntime.JobRecoveryPolicyFail, agentruntime.JobRetryPolicy{}, identity, func(ctx context.Context) (any, error) {
				calls.Add(1)
				got, ok := agentruntime.JobIdempotencyIdentityFromContext(ctx)
				if !ok || got != identity {
					return nil, errors.New("worker idempotency context mismatch")
				}
				<-release
				return map[string]bool{"ok": true}, nil
			})
		}(i)
	}
	close(start)
	wg.Wait()

	var canonicalID string
	replays := 0
	for i := range jobs {
		if errs[i] != nil || jobs[i] == nil {
			t.Fatalf("admission %d failed: job=%#v err=%v", i, jobs[i], errs[i])
		}
		if canonicalID == "" {
			canonicalID = jobs[i].ID
		}
		if jobs[i].ID != canonicalID {
			t.Fatalf("idempotent admission created multiple jobs: %q and %q", canonicalID, jobs[i].ID)
		}
		if jobs[i].IdempotentReplay {
			replays++
		}
	}
	if replays != callers-1 || calls.Load() > 1 {
		t.Fatalf("replays=%d calls=%d, want %d/1", replays, calls.Load(), callers-1)
	}
	close(release)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, ok := mgr.getUserJob(canonicalID, principal)
		if ok && current.Status == asyncJobStatusSucceeded {
			if current.IdempotentReplay || current.IdempotencyDigest != identity.Digest || current.RequestDigest != identity.RequestDigest {
				t.Fatalf("unexpected canonical idempotency state: %#v", current)
			}
			payload, err := json.Marshal(loadPersistedAsyncJobsForTest(t, mgr))
			if err != nil {
				t.Fatalf("read persisted idempotent job: %v", err)
			}
			if strings.Contains(string(payload), "client-key") {
				t.Fatalf("raw idempotency key was persisted: %s", payload)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for canonical idempotent job")
}

func TestAsyncJobManagerRejectsIdempotencyKeyReuseWithDifferentRequest(t *testing.T) {
	mgr := newAsyncJobManager(t.TempDir())
	t.Cleanup(mgr.close)
	principal := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	firstIdentity, _ := agentruntime.NewJobIdempotencyIdentity(principal.TenantID, principal.UserID, "job.conflict", "same-key", map[string]int{"value": 1})
	conflictIdentity, _ := agentruntime.NewJobIdempotencyIdentity(principal.TenantID, principal.UserID, "job.conflict", "same-key", map[string]int{"value": 2})
	release := make(chan struct{})
	first, err := mgr.createUserJobIdempotent("job.conflict", principal, agentruntime.JobRecoveryPolicyFail, agentruntime.JobRetryPolicy{}, firstIdentity, func(context.Context) (any, error) {
		<-release
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var conflictCalls atomic.Uint32
	conflict, err := mgr.createUserJobIdempotent("job.conflict", principal, agentruntime.JobRecoveryPolicyFail, agentruntime.JobRetryPolicy{}, conflictIdentity, func(context.Context) (any, error) {
		conflictCalls.Add(1)
		return nil, nil
	})
	if !errors.Is(err, agentruntime.ErrJobIdempotencyConflict) || conflict != nil || conflictCalls.Load() != 0 {
		t.Fatalf("conflicting admission = %#v err=%v calls=%d", conflict, err, conflictCalls.Load())
	}
	if current, ok := mgr.getUserJob(first.ID, principal); !ok || current.RequestDigest != firstIdentity.RequestDigest {
		t.Fatalf("conflict changed canonical job: %#v", current)
	}
	close(release)
}

func TestAsyncJobManagerIdempotentAdmissionSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	principal := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	identity, _ := agentruntime.NewJobIdempotencyIdentity(principal.TenantID, principal.UserID, "job.restart-idempotency", "restart-key", map[string]bool{"same": true})
	var calls atomic.Uint32
	mgr := newAsyncJobManager(root)
	first, err := mgr.createUserJobIdempotent("job.restart-idempotency", principal, agentruntime.JobRecoveryPolicyFail, agentruntime.JobRetryPolicy{}, identity, func(context.Context) (any, error) {
		calls.Add(1)
		return map[string]bool{"ok": true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	completed := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, ok := mgr.getUserJob(first.ID, principal)
		if ok && current.Status == asyncJobStatusSucceeded {
			completed = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !completed {
		t.Fatal("timed out waiting for original idempotent job")
	}
	mgr.close()

	restarted := newAsyncJobManager(root)
	t.Cleanup(restarted.close)
	replay, err := restarted.createUserJobIdempotent("job.restart-idempotency", principal, agentruntime.JobRecoveryPolicyFail, agentruntime.JobRetryPolicy{}, identity, func(context.Context) (any, error) {
		calls.Add(1)
		return nil, nil
	})
	if err != nil || replay == nil || replay.ID != first.ID || !replay.IdempotentReplay || calls.Load() != 1 {
		t.Fatalf("restart replay = %#v err=%v calls=%d", replay, err, calls.Load())
	}
}

func TestAsyncJobManagerReleasesIdempotencyKeyWhenAdmissionIsNotDurable(t *testing.T) {
	root := t.TempDir()
	mgr := newAsyncJobManager(root)
	t.Cleanup(mgr.close)
	principal := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	identity, _ := agentruntime.NewJobIdempotencyIdentity(principal.TenantID, principal.UserID, "job.admission-retry", "admission-retry-key", map[string]bool{"same": true})
	blocker := filepath.Join(root, "admission-blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	mgr.mu.Lock()
	mgr.filePath = filepath.Join(blocker, "state", "jobs.json")
	mgr.store = &fileAsyncJobStore{path: mgr.filePath}
	mgr.mu.Unlock()
	var calls atomic.Uint32
	failed, err := mgr.createUserJobIdempotent("job.admission-retry", principal, agentruntime.JobRecoveryPolicyFail, agentruntime.JobRetryPolicy{}, identity, func(context.Context) (any, error) {
		calls.Add(1)
		return nil, nil
	})
	if err != nil || failed.Status != asyncJobStatusFailed || failed.ErrorCode != agentruntime.JobErrorCodePersistenceFailed || failed.IdempotencyDigest != "" || calls.Load() != 0 {
		t.Fatalf("failed admission retained execution identity: %#v err=%v calls=%d", failed, err, calls.Load())
	}

	mgr.mu.Lock()
	mgr.filePath = filepath.Join(root, "state", "jobs.json")
	mgr.store = &fileAsyncJobStore{path: mgr.filePath}
	mgr.mu.Unlock()
	retried, err := mgr.createUserJobIdempotent("job.admission-retry", principal, agentruntime.JobRecoveryPolicyFail, agentruntime.JobRetryPolicy{}, identity, func(context.Context) (any, error) {
		calls.Add(1)
		return map[string]bool{"ok": true}, nil
	})
	if err != nil || retried == nil || retried.ID == failed.ID || retried.IdempotentReplay {
		t.Fatalf("safe admission retry was not accepted: failed=%#v retried=%#v err=%v", failed, retried, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, ok := mgr.getUserJob(retried.ID, principal)
		if ok && current.Status == asyncJobStatusSucceeded {
			if calls.Load() != 1 {
				t.Fatalf("safe admission retry calls=%d", calls.Load())
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for safe admission retry")
}

func TestAsyncJobManagerCloseCancelsWorkersAndStopsAdmission(t *testing.T) {
	mgr := newAsyncJobManager(t.TempDir())
	p := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	job := mgr.createUserJob("job.shutdown", p, func(context.Context) (any, error) {
		started <- struct{}{}
		<-release // deliberately ignore cancellation to test no resurrection
		return map[string]string{"status": "late-success"}, nil
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for shutdown job")
	}
	mgr.close()
	current, ok := mgr.getUserJob(job.ID, p)
	if !ok || current.Status != asyncJobStatusCanceled || current.ErrorCode != agentruntime.JobErrorCodeCanceled {
		t.Fatalf("shutdown did not cancel active job: %#v", current)
	}
	mgr.updateProgress(job.ID, 1, "late progress")
	current, _ = mgr.getUserJob(job.ID, p)
	if current.Progress != 0 || current.ProgressText != "" {
		t.Fatalf("closed manager accepted late progress: %#v", current)
	}
	close(release)
	time.Sleep(50 * time.Millisecond)
	current, ok = mgr.getUserJob(job.ID, p)
	if !ok || current.Status != asyncJobStatusCanceled {
		t.Fatalf("late worker result resurrected canceled job: %#v", current)
	}
	executed := make(chan struct{}, 1)
	rejected := mgr.createUserJob("job.after-close", p, func(context.Context) (any, error) {
		executed <- struct{}{}
		return nil, nil
	})
	if rejected.Status != asyncJobStatusFailed || rejected.ErrorCode != "service_closed" {
		t.Fatalf("closed manager accepted admission: %#v", rejected)
	}
	select {
	case <-executed:
		t.Fatal("closed manager started a worker")
	default:
	}
}
