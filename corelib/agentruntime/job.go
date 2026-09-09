package agentruntime

import (
	"encoding/json"
	"strings"
	"time"
)

// JobStatus is the transport-neutral lifecycle vocabulary for work that
// outlives the request which created it. Hosts may persist richer job records,
// but they should not invent a second set of status strings for the same
// Runtime operation.
type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusSucceeded JobStatus = "succeeded"
	JobStatusFailed    JobStatus = "failed"
	JobStatusCanceled  JobStatus = "canceled"
	JobStatusUnknown   JobStatus = "unknown"
)

// JobRecoveryPolicy defines how a host must project an in-flight durable job
// after its worker process disappears. Reconcile is the fail-closed default:
// the operation may already have produced an external side effect, so the host
// must surface an unknown outcome rather than claim failure or replay it.
type JobRecoveryPolicy string

const (
	JobRecoveryPolicyReconcile JobRecoveryPolicy = "reconcile"
	JobRecoveryPolicyFail      JobRecoveryPolicy = "fail"
)

// Stable job error identifiers shared by every host. These are deliberately
// separate from JobStatus: a failed job may carry a domain error while a
// canceled/restarted job needs a machine-readable lifecycle reason.
const (
	JobErrorCodeCanceled              = "job_canceled"
	JobErrorCodeServiceRestarted      = "service_restarted"
	JobErrorCodeReconcileRequired     = "reconcile_required"
	JobErrorCodeResultNotSerializable = "result_not_serializable"
	JobErrorCodePersistenceFailed     = "job_persistence_failed"
)

// NormalizeJobStatus prevents persisted/remote values from creating a second
// implicit status vocabulary. Unknown values are retained as JobStatusUnknown
// so a host can surface them for reconciliation instead of treating them as a
// successful or retryable state.
func NormalizeJobStatus(status JobStatus) JobStatus {
	switch status {
	case JobStatusPending, JobStatusRunning, JobStatusSucceeded, JobStatusFailed, JobStatusCanceled, JobStatusUnknown:
		return status
	default:
		return JobStatusUnknown
	}
}

// JobStatusIsTerminal reports statuses that must not be reconciled or replayed.
func JobStatusIsTerminal(status JobStatus) bool {
	switch NormalizeJobStatus(status) {
	case JobStatusSucceeded, JobStatusFailed, JobStatusCanceled:
		return true
	default:
		return false
	}
}

// ProjectLegacyJobStatus maps host-specific aliases onto the shared
// JobStatus vocabulary. Unknown or partial states fail closed to unknown.
func ProjectLegacyJobStatus(status string) JobStatus {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "queued", "pending", "planning", "needs_ocr", "pending_review", "review_pending":
		return JobStatusPending
	case "running", "indexing", "in_progress", "in-progress", "processing", "uploading", "importing", "downloading", "verifying", "under_review", "reviewing":
		return JobStatusRunning
	case "success", "succeeded", "completed", "complete", "approved", "passed":
		return JobStatusSucceeded
	case "failed", "failure", "error", "timeout", "verify_failed", "rejected", "declined":
		return JobStatusFailed
	case "cancelled", "canceled", "cancel", "killed", "withdrawn", "skipped":
		return JobStatusCanceled
	case "unknown":
		return JobStatusUnknown
	default:
		return JobStatusUnknown
	}
}

// NormalizeJobRecoveryPolicy keeps legacy jobs and future/invalid policy
// values fail-closed. An empty policy is legacy data and therefore cannot be
// assumed safe to replay or mark failed.
func NormalizeJobRecoveryPolicy(policy JobRecoveryPolicy) JobRecoveryPolicy {
	switch policy {
	case JobRecoveryPolicyFail:
		return JobRecoveryPolicyFail
	case JobRecoveryPolicyReconcile, "":
		return JobRecoveryPolicyReconcile
	default:
		return JobRecoveryPolicyReconcile
	}
}

// Job is the shared durable envelope for work that outlives its creating
// request. Hosts may add private bookkeeping (for example a cancel function),
// but the persisted/API projection should retain this shape and vocabulary.
type Job struct {
	ID string `json:"id"`
	// Version is the repository concurrency token. It starts at one after
	// durable admission and advances on every committed mutation. Hosts must
	// treat it as opaque and use JobRepository CAS operations rather than
	// assigning it themselves.
	Version uint64 `json:"version,omitempty"`
	// LeaseOwnerID and LeaseExpiresAt are repository/runtime coordination
	// metadata, not transport fields. A non-expired lease means another live
	// host may still be executing the Job; restart recovery must wait for lease
	// expiry. The owner remains stable while the expiry is cleared on terminal
	// transitions.
	LeaseOwnerID         string            `json:"-"`
	LeaseExpiresAt       *time.Time        `json:"-"`
	Kind                 string            `json:"kind"`
	Status               JobStatus         `json:"status"`
	RecoveryPolicy       JobRecoveryPolicy `json:"recovery_policy"`
	RetryPolicy          JobRetryPolicy    `json:"retry_policy"`
	Attempt              uint32            `json:"attempt,omitempty"`
	NextAttemptAt        *time.Time        `json:"next_attempt_at,omitempty"`
	LastAttemptErrorCode string            `json:"last_attempt_error_code,omitempty"`
	LastAttemptError     string            `json:"last_attempt_error,omitempty"`
	IdempotencyDigest    string            `json:"idempotency_digest,omitempty"`
	RequestDigest        string            `json:"request_digest,omitempty"`
	IdempotentReplay     bool              `json:"idempotent_replay,omitempty"`
	TenantID             string            `json:"tenant_id"`
	UserID               string            `json:"user_id"`
	Progress             float64           `json:"progress,omitempty"`
	ProgressText         string            `json:"progress_text,omitempty"`
	Checkpoint           *JobCheckpoint    `json:"checkpoint,omitempty"`
	Result               json.RawMessage   `json:"result,omitempty"`
	ErrorCode            string            `json:"error_code,omitempty"`
	Error                string            `json:"error,omitempty"`
	CreatedAt            time.Time         `json:"created_at"`
	StartedAt            *time.Time        `json:"started_at,omitempty"`
	CompletedAt          *time.Time        `json:"completed_at,omitempty"`
}
