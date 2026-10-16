package agentruntime

import (
	"encoding/json"
	"errors"
	"strings"
)

// ValidateJobEnvelope checks the durable Job contract shared by every
// repository implementation. Lease owner/expiry pairing is fail-closed so a
// host cannot persist an active owned job without an expiry, or a terminal
// job that still holds an execution lease.
func ValidateJobEnvelope(job Job) error {
	if strings.TrimSpace(job.ID) == "" {
		return errors.New("job id is required")
	}
	if err := ValidateJobRetryState(job.RetryPolicy, job.Attempt, job.NextAttemptAt); err != nil {
		return err
	}
	if job.Checkpoint != nil {
		if err := ValidateJobCheckpoint(*job.Checkpoint); err != nil {
			return err
		}
	}
	if err := ValidateJobIdempotencyState(job.IdempotencyDigest, job.RequestDigest, job.IdempotentReplay); err != nil {
		return err
	}
	active := job.Status == JobStatusPending || job.Status == JobStatusRunning
	if job.LeaseExpiresAt != nil && strings.TrimSpace(job.LeaseOwnerID) == "" {
		return errors.New("job lease owner is required when lease expiry is set")
	}
	if active && strings.TrimSpace(job.LeaseOwnerID) != "" && job.LeaseExpiresAt == nil {
		return errors.New("active owned job requires a lease expiry")
	}
	if !active && job.LeaseExpiresAt != nil {
		return errors.New("terminal job cannot retain an execution lease")
	}
	if job.LeaseExpiresAt != nil && job.LeaseExpiresAt.IsZero() {
		return errors.New("job lease expiry cannot be zero")
	}
	if _, err := json.Marshal(job); err != nil {
		return err
	}
	return nil
}

// ValidateJobImmutableFields rejects identity mutations that would break
// idempotency lookup or steal another worker's lease owner.
func ValidateJobImmutableFields(current, next Job) error {
	if current.ID != next.ID || current.TenantID != next.TenantID || current.UserID != next.UserID || current.Kind != next.Kind ||
		current.IdempotencyDigest != next.IdempotencyDigest || current.RequestDigest != next.RequestDigest || current.LeaseOwnerID != next.LeaseOwnerID || !current.CreatedAt.Equal(next.CreatedAt) {
		return errors.New("job immutable identity fields cannot be changed")
	}
	return nil
}

// CloneJob returns a deep copy of a Job envelope, including pointer fields
// that callers must not share with the repository's canonical record.
func CloneJob(job Job) Job {
	copy := job
	if job.Result != nil {
		copy.Result = append(json.RawMessage(nil), job.Result...)
	}
	if job.Checkpoint != nil {
		checkpoint := *job.Checkpoint
		copy.Checkpoint = &checkpoint
	}
	if job.NextAttemptAt != nil {
		t := *job.NextAttemptAt
		copy.NextAttemptAt = &t
	}
	if job.StartedAt != nil {
		t := *job.StartedAt
		copy.StartedAt = &t
	}
	if job.CompletedAt != nil {
		t := *job.CompletedAt
		copy.CompletedAt = &t
	}
	if job.LeaseExpiresAt != nil {
		t := *job.LeaseExpiresAt
		copy.LeaseExpiresAt = &t
	}
	return copy
}
