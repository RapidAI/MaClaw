package agentruntime

import (
	"context"
	"sort"
	"strings"
	"time"
)

const (
	// DefaultJobRetention is how long a terminal Job stays visible for hosts
	// that prune by age. Unknown Jobs are never age-pruned.
	DefaultJobRetention = 24 * time.Hour
	// DefaultJobMaxCount is the housekeeping cap after age pruning. Unknown and
	// in-flight Jobs count toward the cap but are never selected for deletion.
	DefaultJobMaxCount = 2000
)

// JobIsRetentionEligible reports that a Job may be removed by time or count
// housekeeping. Unknown remains durable reconciliation evidence.
func JobIsRetentionEligible(job Job) bool {
	if job.Status == JobStatusUnknown {
		return false
	}
	return job.CompletedAt != nil
}

// SelectJobsForRetentionPrune returns Jobs that age-based or count-based
// housekeeping should delete. Hosts persist the deletion; this helper never
// writes. Unknown Jobs are never selected.
func SelectJobsForRetentionPrune(jobs []Job, now time.Time, retention time.Duration, maxCount int) []Job {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	if retention <= 0 {
		retention = DefaultJobRetention
	}
	if maxCount <= 0 {
		maxCount = DefaultJobMaxCount
	}
	prune := make([]Job, 0)
	remaining := make([]Job, 0, len(jobs))
	for _, job := range jobs {
		if JobIsRetentionEligible(job) && now.Sub(*job.CompletedAt) > retention {
			prune = append(prune, job)
			continue
		}
		remaining = append(remaining, job)
	}
	if len(remaining) <= maxCount {
		return prune
	}
	terminal := make([]Job, 0)
	for _, job := range remaining {
		if JobIsRetentionEligible(job) {
			terminal = append(terminal, job)
		}
	}
	sort.Slice(terminal, func(i, j int) bool {
		return terminal[i].CompletedAt.Before(*terminal[j].CompletedAt)
	})
	overflow := len(remaining) - maxCount
	if overflow > len(terminal) {
		overflow = len(terminal)
	}
	return append(prune, terminal[:overflow]...)
}

// PruneRetainedJobsInRepository deletes age/count-expired Jobs from a
// repository and then drops their protected effects. Job deletion is the
// canonical commit; effect cleanup is best-effort so a second SQLite file
// cannot resurrect a removed Job. Unknown Jobs are never selected.
func PruneRetainedJobsInRepository(ctx context.Context, jobs JobRepository, effects JobEffectRepository, now time.Time, retention time.Duration, maxCount int) error {
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
	selected := SelectJobsForRetentionPrune(items, now, retention, maxCount)
	if len(selected) == 0 {
		return nil
	}
	versions := make([]JobVersion, 0, len(selected))
	ids := make([]string, 0, len(selected))
	for _, job := range selected {
		if job.Version == 0 || strings.TrimSpace(job.ID) == "" {
			continue
		}
		versions = append(versions, JobVersion{ID: job.ID, Version: job.Version})
		ids = append(ids, job.ID)
	}
	if len(versions) == 0 {
		return nil
	}
	if err := jobs.Delete(ctx, versions); err != nil {
		return err
	}
	if effects != nil {
		_ = effects.DeleteByJobs(ctx, ids)
	}
	return nil
}
