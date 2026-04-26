package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestAsyncJobManagerCancelRunningJob(t *testing.T) {
	mgr := newAsyncJobManager(t.TempDir())
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
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for canceled status")
}

func TestAsyncJobManagerListUserJobs(t *testing.T) {
	mgr := newAsyncJobManager(t.TempDir())
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
	principal := agentservice.Principal{TenantID: "tenant_1", UserID: "user_1"}
	job := mgr.createUserJob("job.persist", principal, func(ctx context.Context) (any, error) {
		return map[string]string{"status": "persisted"}, nil
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, ok := mgr.getUserJob(job.ID, principal)
		if ok && current.Status == asyncJobStatusSucceeded {
			loaded := newAsyncJobManager(root)
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
