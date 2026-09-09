package agentruntime

import (
	"context"
	"testing"
	"time"
)

func TestJobLeaseExpiredAndRecover(t *testing.T) {
	if JobIsActive(JobStatusSucceeded) || !JobIsActive(JobStatusRunning) {
		t.Fatal("active helper mismatch")
	}
	now := time.Now().UTC()
	deadline := JobLeaseDeadline(now, DefaultJobLeaseTTL)
	if deadline == nil || deadline.Sub(now) != DefaultJobLeaseTTL {
		t.Fatalf("deadline=%v", deadline)
	}
	terminal := PrepareJobLeaseForPersist(Job{Status: JobStatusSucceeded, LeaseExpiresAt: deadline}, "owner", now, DefaultJobLeaseTTL)
	if terminal.LeaseExpiresAt != nil {
		t.Fatal("terminal job must drop lease expiry")
	}
	owned := PrepareJobLeaseForPersist(Job{Status: JobStatusRunning, LeaseOwnerID: "owner"}, "owner", now, DefaultJobLeaseTTL)
	if owned.LeaseExpiresAt == nil || !JobShouldRenewLease(owned, "owner") || JobShouldRenewLease(owned, "other") {
		t.Fatalf("owned lease=%#v", owned)
	}
	running := Job{ID: "job-1", Status: JobStatusRunning}
	if !JobLeaseExpired(running, now) {
		t.Fatal("active job without lease must be expired")
	}
	future := now.Add(time.Minute)
	leased := Job{ID: "job-1", Status: JobStatusRunning, LeaseOwnerID: "owner", LeaseExpiresAt: &future}
	if JobLeaseExpired(leased, now) {
		t.Fatal("live lease must not be expired")
	}
	past := now.Add(-time.Second)
	expired := Job{ID: "job-1", Status: JobStatusRunning, LeaseOwnerID: "owner", LeaseExpiresAt: &past, RecoveryPolicy: JobRecoveryPolicyReconcile}
	if !JobLeaseExpired(expired, now) {
		t.Fatal("past lease must be expired")
	}
	recovered := RecoverExpiredJobLease(expired, now)
	if recovered.Status != JobStatusUnknown || recovered.ErrorCode != JobErrorCodeReconcileRequired || recovered.LeaseExpiresAt != nil {
		t.Fatalf("reconcile recovery=%#v", recovered)
	}
	failed := RecoverExpiredJobLease(Job{ID: "job-2", Status: JobStatusRunning, RecoveryPolicy: JobRecoveryPolicyFail, LeaseOwnerID: "owner", LeaseExpiresAt: &past}, now)
	if failed.Status != JobStatusFailed || failed.ErrorCode != JobErrorCodeServiceRestarted || failed.CompletedAt == nil {
		t.Fatalf("fail recovery=%#v", failed)
	}
	scanned := RecoverExpiredJobs([]Job{leased, expired}, now, func(job Job) bool { return job.ID == "skip" })
	if len(scanned) != 1 || scanned[0].ID != "job-1" || scanned[0].Status != JobStatusUnknown {
		t.Fatalf("scan=%#v", scanned)
	}
	if ShouldRenewLocalJobLease(owned, "owner", false) || !ShouldRenewLocalJobLease(owned, "owner", true) {
		t.Fatal("local renewal requires a live worker")
	}
	if DefaultJobLeaseTick != 10*time.Second {
		t.Fatalf("tick=%s", DefaultJobLeaseTick)
	}
}

func TestRecoverExpiredJobsInRepositoryPersistsUnknown(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryJobRepository()
	stale := Job{ID: "run-1", Kind: "skill.run", Status: JobStatusRunning, TenantID: "gui", UserID: "local", RecoveryPolicy: JobRecoveryPolicyReconcile}
	if _, created, err := repo.Admit(ctx, stale); err != nil || !created {
		t.Fatalf("admit=%v created=%t", err, created)
	}
	live := Job{ID: "run-2", Kind: "skill.run", Status: JobStatusRunning, TenantID: "gui", UserID: "local", LeaseOwnerID: "owner"}
	future := time.Now().UTC().Add(time.Minute)
	live.LeaseExpiresAt = &future
	if _, _, err := repo.Admit(ctx, live); err != nil {
		t.Fatal(err)
	}
	if err := RecoverExpiredJobsInRepository(ctx, repo, time.Now().UTC(), func(job Job) bool { return job.ID == "run-2" }); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, "run-1")
	if err != nil || got.Status != JobStatusUnknown || got.ErrorCode != JobErrorCodeReconcileRequired {
		t.Fatalf("recovered=%#v err=%v", got, err)
	}
	kept, err := repo.Get(ctx, "run-2")
	if err != nil || kept.Status != JobStatusRunning {
		t.Fatalf("skipped=%#v err=%v", kept, err)
	}
}
