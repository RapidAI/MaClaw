package agentruntime

import (
	"errors"
	"testing"
	"time"
)

func TestValidateJobEnvelopeLeasePairing(t *testing.T) {
	now := time.Now().UTC()
	job := Job{ID: "job-1", Kind: "test", Status: JobStatusRunning, TenantID: "t", UserID: "u", CreatedAt: now}
	if err := ValidateJobEnvelope(job); err != nil {
		t.Fatalf("unleased running job: %v", err)
	}
	job.LeaseOwnerID = "executor"
	if err := ValidateJobEnvelope(job); err == nil {
		t.Fatal("owned running job without expiry must fail")
	}
	job.LeaseExpiresAt = &now
	if err := ValidateJobEnvelope(job); err != nil {
		t.Fatalf("owned running job: %v", err)
	}
	job.Status = JobStatusSucceeded
	if err := ValidateJobEnvelope(job); err == nil {
		t.Fatal("terminal job must not retain a lease")
	}
}

func TestCloneJobCopiesPointerFields(t *testing.T) {
	expires := time.Now().UTC()
	checkpoint := JobCheckpoint{Phase: "step"}
	job := Job{ID: "job-1", Result: []byte(`{"ok":true}`), Checkpoint: &checkpoint, LeaseExpiresAt: &expires}
	cloned := CloneJob(job)
	job.Result[2] = 'x'
	checkpoint.Phase = "mutated"
	expires = expires.Add(time.Hour)
	if string(cloned.Result) != `{"ok":true}` || cloned.Checkpoint.Phase != "step" || !cloned.LeaseExpiresAt.Before(expires) {
		t.Fatalf("clone shared backing storage: %#v", cloned)
	}
}

func TestValidateJobImmutableFields(t *testing.T) {
	now := time.Now().UTC()
	current := Job{ID: "job-1", TenantID: "t", UserID: "u", Kind: "k", CreatedAt: now, LeaseOwnerID: "owner"}
	next := current
	next.Status = JobStatusRunning
	if err := ValidateJobImmutableFields(current, next); err != nil {
		t.Fatal(err)
	}
	next.LeaseOwnerID = "other"
	if err := ValidateJobImmutableFields(current, next); err == nil {
		t.Fatal("lease owner mutation must fail")
	}
	if !errors.Is(ValidateJobRetryState(JobRetryPolicy{}, 0, nil), nil) {
		t.Fatal("empty retry policy is valid")
	}
}
