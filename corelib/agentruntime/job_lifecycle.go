package agentruntime

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	jobPersistenceFailedMessage          = "async job persistence failed"
	jobCompletionPersistUncertainMessage = "job persistence failed after a side effect; reconciliation required"
	jobEffectUncertainMessage            = "job effect outcome is uncertain; reconciliation required"
	jobCanceledMessage                   = "job canceled"
)

var errHostJobFailed = errors.New("job failed")

// StampJobRunning starts the first attempt if none has begun, otherwise keeps
// the Job running. Host DTO mirrors call this on every in-progress publish.
func StampJobRunning(job Job, now time.Time) Job {
	if job.Attempt == 0 {
		if job.Status == "" || job.Status == JobStatusUnknown || job.Status == JobStatusPending {
			job.Status = JobStatusPending
		}
		if next, ok := BeginJobAttempt(job, now); ok {
			return next
		}
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	job.Status = JobStatusRunning
	if job.StartedAt == nil {
		started := now
		job.StartedAt = &started
	}
	return job
}

// StampHostJobStatus projects a host-observed Runtime status onto a Job.
// Running goes through StampJobRunning; succeeded/failed/canceled go through
// ApplyJobWorkerOutcome. Unknown is left as a non-terminal observation.
func StampHostJobStatus(job Job, status JobStatus, now time.Time, outcome JobWorkerOutcome) Job {
	switch status {
	case JobStatusPending:
		if job.Status == "" || job.Status == JobStatusUnknown {
			job.Status = JobStatusPending
		}
		return job
	case JobStatusRunning:
		return StampJobRunning(job, now)
	case JobStatusSucceeded:
		return ApplyJobWorkerOutcome(job, now, JobWorkerOutcome{Result: outcome.Result})
	case JobStatusFailed:
		code := strings.TrimSpace(outcome.ErrorCode)
		if code == "" {
			code = "job_failed"
		}
		text := strings.TrimSpace(outcome.ErrorText)
		if text == "" {
			text = "job failed"
		}
		return ApplyJobWorkerOutcome(job, now, JobWorkerOutcome{Err: errHostJobFailed, ErrorCode: code, ErrorText: text})
	case JobStatusCanceled:
		return StampJobCanceled(job, now, outcome.ErrorText)
	default:
		job.Status = status
		return job
	}
}

// BeginJobAttempt advances an active Job onto its next worker attempt.
// False means the Job cannot start (not active, or retry budget exhausted).
func BeginJobAttempt(job Job, now time.Time) (Job, bool) {
	if !JobIsActive(job.Status) {
		return job, false
	}
	policy := NormalizeJobRetryPolicy(job.RetryPolicy)
	if job.Attempt >= policy.MaxAttempts {
		return job, false
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	job.RetryPolicy = policy
	job.Attempt++
	job.Status = JobStatusRunning
	job.NextAttemptAt = nil
	if job.StartedAt == nil {
		started := now
		job.StartedAt = &started
	}
	return job, true
}

// ScheduleJobRetry parks an active Job until the next authorized attempt.
func ScheduleJobRetry(job Job, delay time.Duration, errorCode, errorText string, now time.Time) Job {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	next := now.Add(delay)
	job.Status = JobStatusPending
	job.NextAttemptAt = &next
	job.LastAttemptErrorCode = errorCode
	job.LastAttemptError = errorText
	return job
}

// JobWorkerOutcome is the host-translated result of one worker attempt.
// Uncertain and Canceled take precedence over Err; a nil Err with Result is success.
type JobWorkerOutcome struct {
	Result    json.RawMessage
	Err       error
	Canceled  bool
	Uncertain bool
	ErrorCode string
	ErrorText string
}

// StampJobCanceled is the host cancel envelope. Empty errorText uses the
// shared "job canceled" text; shutdown paths may pass a more specific reason.
func StampJobCanceled(job Job, now time.Time, errorText string) Job {
	return ApplyJobWorkerOutcome(job, now, JobWorkerOutcome{Canceled: true, ErrorText: errorText})
}

// ApplyJobWorkerOutcome stamps a terminal or unknown outcome onto a Job.
// Hosts persist afterwards; this helper never writes storage.
func ApplyJobWorkerOutcome(job Job, now time.Time, outcome JobWorkerOutcome) Job {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	completed := now
	job.CompletedAt = &completed
	job.NextAttemptAt = nil
	switch {
	case outcome.Uncertain:
		job.Status = JobStatusUnknown
		job.ErrorCode = JobErrorCodeReconcileRequired
		job.Error = jobEffectUncertainMessage
		job.CompletedAt = nil
	case outcome.Canceled:
		job.Status = JobStatusCanceled
		job.ErrorCode = JobErrorCodeCanceled
		text := strings.TrimSpace(outcome.ErrorText)
		if text == "" {
			text = jobCanceledMessage
		}
		job.Error = text
		job.Result = nil
	case outcome.Err != nil:
		job.Status = JobStatusFailed
		job.ErrorCode = outcome.ErrorCode
		job.Error = outcome.ErrorText
		job.Result = nil
	default:
		job.Status = JobStatusSucceeded
		job.ErrorCode = ""
		job.Error = ""
		job.Result = append(json.RawMessage(nil), outcome.Result...)
	}
	return job
}

// MarkJobPersistenceFailed converts an otherwise active Job into an explicit
// terminal failure after durable admission/update failed. Hosts must not
// persist again from this envelope.
func MarkJobPersistenceFailed(job Job, now time.Time) Job {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	job.Status = JobStatusFailed
	job.ErrorCode = JobErrorCodePersistenceFailed
	job.Error = jobPersistenceFailedMessage
	job.Result = nil
	job.NextAttemptAt = nil
	job.LeaseExpiresAt = nil
	if job.CompletedAt == nil {
		completed := now
		job.CompletedAt = &completed
	}
	return job
}

// MarkJobCompletionPersistFailed distinguishes a completion write failure
// after a protected side effect (unknown + reconcile) from an ordinary
// persist failure (failed).
func MarkJobCompletionPersistFailed(job Job, now time.Time, hasProtectedEffects bool) Job {
	if !hasProtectedEffects {
		return MarkJobPersistenceFailed(job, now)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	job.Status = JobStatusUnknown
	job.ErrorCode = JobErrorCodeReconcileRequired
	job.Error = jobCompletionPersistUncertainMessage
	job.Result = nil
	job.NextAttemptAt = nil
	job.LeaseExpiresAt = nil
	if job.CompletedAt == nil {
		completed := now
		job.CompletedAt = &completed
	}
	return job
}
