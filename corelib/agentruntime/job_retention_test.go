package agentruntime

import (
	"context"
	"testing"
	"time"
)

func TestSelectJobsForRetentionPruneKeepsUnknownAndActive(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	old := now.Add(-2 * DefaultJobRetention)
	recent := now.Add(-time.Hour)
	aged := Job{ID: "aged", Status: JobStatusSucceeded, CompletedAt: &old}
	fresh := Job{ID: "fresh", Status: JobStatusSucceeded, CompletedAt: &recent}
	unknown := Job{ID: "unknown", Status: JobStatusUnknown, CompletedAt: &old}
	active := Job{ID: "active", Status: JobStatusRunning}
	got := SelectJobsForRetentionPrune([]Job{aged, fresh, unknown, active}, now, 0, 0)
	if len(got) != 1 || got[0].ID != "aged" {
		t.Fatalf("age prune=%#v", got)
	}

	t0 := now.Add(-time.Hour)
	t1 := now.Add(-time.Minute)
	t2 := now.Add(-time.Second)
	overflow := SelectJobsForRetentionPrune([]Job{
		{ID: "a", Status: JobStatusSucceeded, CompletedAt: &t0},
		{ID: "b", Status: JobStatusFailed, CompletedAt: &t1},
		{ID: "c", Status: JobStatusCanceled, CompletedAt: &t2},
		unknown,
		active,
	}, now, 24*time.Hour, 3)
	ids := map[string]bool{}
	for _, job := range overflow {
		ids[job.ID] = true
	}
	if !ids["a"] || !ids["b"] || ids["c"] || ids["unknown"] || ids["active"] || len(overflow) != 2 {
		t.Fatalf("cap prune=%#v", overflow)
	}
}

func TestPruneRetainedJobsInRepositoryDeletesAgedTerminal(t *testing.T) {
	ctx := context.Background()
	jobs := NewMemoryJobRepository()
	effects := NewMemoryJobEffectRepository()
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	old := now.Add(-2 * DefaultJobRetention)
	aged := Job{ID: "aged", Kind: "skill.run", Status: JobStatusSucceeded, TenantID: "gui", UserID: "local", CompletedAt: &old}
	if _, created, err := jobs.Admit(ctx, aged); err != nil || !created {
		t.Fatalf("admit aged=%v created=%t", err, created)
	}
	freshCompleted := now.Add(-time.Hour)
	fresh := Job{ID: "fresh", Kind: "skill.run", Status: JobStatusSucceeded, TenantID: "gui", UserID: "local", CompletedAt: &freshCompleted}
	if _, _, err := jobs.Admit(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	if _, _, err := effects.Prepare(ctx, JobEffect{
		JobID: "aged", Kind: "skill.run", JobKind: "skill.run", TenantID: "gui", UserID: "local",
		ResourceID: "aged", CreatedAt: old, UpdatedAt: old,
	}); err != nil {
		t.Fatal(err)
	}
	if err := PruneRetainedJobsInRepository(ctx, jobs, effects, now, 0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := jobs.Get(ctx, "aged"); err != ErrJobRepositoryNotFound {
		t.Fatalf("aged job must be deleted: err=%v", err)
	}
	if _, err := jobs.Get(ctx, "fresh"); err != nil {
		t.Fatalf("fresh job must remain: %v", err)
	}
	if leftover, err := effects.ListByJob(ctx, "aged"); err != nil || len(leftover) != 0 {
		t.Fatalf("aged effects must be cleaned: leftover=%#v err=%v", leftover, err)
	}
}
