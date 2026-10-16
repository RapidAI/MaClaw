package agentruntime

import (
	"context"
	"strings"
	"time"
)

const (
	// DefaultJobLeaseTTL is the production worker-lease window shared by hosts
	// that persist Jobs.
	DefaultJobLeaseTTL = 30 * time.Second
	// DefaultJobLeaseTick is the heartbeat interval. Hosts still own the
	// goroutine and persist path.
	DefaultJobLeaseTick = 10 * time.Second

	expiredJobLeaseFailMessage      = "worker lease expired before job completed"
	expiredJobLeaseReconcileMessage = "worker lease expired while job outcome was uncertain; reconciliation required"
)

// JobLeaseDeadline returns the expiry instant for a newly granted or renewed lease.
func JobLeaseDeadline(now time.Time, ttl time.Duration) *time.Time {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	if ttl <= 0 {
		ttl = DefaultJobLeaseTTL
	}
	deadline := now.Add(ttl)
	return &deadline
}

// PrepareJobLeaseForPersist stamps a persist-time lease. Terminal Jobs drop
// expiry; the current owner renews; a foreign owner is left unchanged.
func PrepareJobLeaseForPersist(job Job, ownerID string, now time.Time, ttl time.Duration) Job {
	if !JobIsActive(job.Status) {
		job.LeaseExpiresAt = nil
		return job
	}
	ownerID = strings.TrimSpace(ownerID)
	if ownerID != "" && strings.TrimSpace(job.LeaseOwnerID) == ownerID {
		job.LeaseExpiresAt = JobLeaseDeadline(now, ttl)
	}
	return job
}

// JobShouldRenewLease reports that this process still owns an active Job.
func JobShouldRenewLease(job Job, ownerID string) bool {
	ownerID = strings.TrimSpace(ownerID)
	return ownerID != "" && JobIsActive(job.Status) && strings.TrimSpace(job.LeaseOwnerID) == ownerID
}

// ShouldRenewLocalJobLease is the heartbeat selection predicate: this process
// still owns the Job and still holds a live worker (host cancel/handle).
func ShouldRenewLocalJobLease(job Job, ownerID string, hasLiveWorker bool) bool {
	return hasLiveWorker && JobShouldRenewLease(job, ownerID)
}

// JobIsActive reports statuses that may still have a live worker.
func JobIsActive(status JobStatus) bool {
	switch NormalizeJobStatus(status) {
	case JobStatusPending, JobStatusRunning:
		return true
	default:
		return false
	}
}

// JobLeaseExpired reports that an active Job has no live owner. An active Job
// without owner or expiry is treated as expired so recovery cannot wait forever.
func JobLeaseExpired(job Job, now time.Time) bool {
	if !JobIsActive(job.Status) {
		return false
	}
	if strings.TrimSpace(job.LeaseOwnerID) == "" || job.LeaseExpiresAt == nil {
		return true
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return !job.LeaseExpiresAt.After(now)
}

// RecoverExpiredJobLease applies RecoveryPolicy after a worker lease expires.
// Fail marks the Job failed; the default reconcile policy leaves it unknown
// so a domain handler can inspect protected effects. It never replays work.
func RecoverExpiredJobLease(job Job, now time.Time) Job {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	job.NextAttemptAt = nil
	job.LeaseExpiresAt = nil
	if NormalizeJobRecoveryPolicy(job.RecoveryPolicy) == JobRecoveryPolicyFail {
		job.Status = JobStatusFailed
		job.ErrorCode = JobErrorCodeServiceRestarted
		job.Error = expiredJobLeaseFailMessage
		completed := now
		job.CompletedAt = &completed
		return job
	}
	job.Status = JobStatusUnknown
	job.ErrorCode = JobErrorCodeReconcileRequired
	job.Error = expiredJobLeaseReconcileMessage
	job.CompletedAt = nil
	return job
}

// RecoverExpiredJobs returns recovered copies of every expired Job that skip
// does not hold back. Hosts persist the result; this helper never writes.
func RecoverExpiredJobs(items []Job, now time.Time, skip func(Job) bool) []Job {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	out := make([]Job, 0)
	for _, job := range items {
		if skip != nil && skip(job) {
			continue
		}
		if !JobLeaseExpired(job, now) {
			continue
		}
		out = append(out, RecoverExpiredJobLease(job, now))
	}
	return out
}

// RecoverExpiredJobsInRepository lists Jobs, applies RecoverExpiredJobs, and
// persists recovered envelopes through UpsertJob. Hosts that keep an in-memory
// worker table (srv cancel map) still persist themselves so skip can see it.
func RecoverExpiredJobsInRepository(ctx context.Context, jobs JobRepository, now time.Time, skip func(Job) bool) error {
	if jobs == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	items, err := jobs.List(ctx)
	if err != nil {
		return err
	}
	for _, job := range RecoverExpiredJobs(items, now, skip) {
		if err := UpsertJob(ctx, jobs, job); err != nil {
			return err
		}
	}
	return nil
}
