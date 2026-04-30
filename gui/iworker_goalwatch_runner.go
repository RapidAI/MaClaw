package main

import (
	"context"
	"sync"
	"time"
)

const (
	defaultIWorkerGoalWatchLimit   = 20
	defaultIWorkerGoalWatchTimeout = 30 * time.Second
	defaultIWorkerGoalWatchNote    = "iworker_watcher_tick"
)

type iworkerGoalWatchRecoverer interface {
	RecoverEligibleGoalWatchPushesContext(ctx context.Context, limit int, note string) IWorkerCenterRecoverySummary
}

type IWorkerGoalWatchRunner struct {
	recoverer iworkerGoalWatchRecoverer
	limit     int
	timeout   time.Duration
	note      string
	now       func() time.Time

	mu        sync.Mutex
	running   bool
	startedAt time.Time
	last      IWorkerGoalWatchRunStatus
}

type IWorkerGoalWatchRunnerConfig struct {
	Recoverer iworkerGoalWatchRecoverer
	Limit     int
	Timeout   time.Duration
	Note      string
	Now       func() time.Time
}

type IWorkerGoalWatchRunStatus struct {
	Running           bool                         `json:"running"`
	StartedAt         time.Time                    `json:"started_at,omitempty"`
	FinishedAt        time.Time                    `json:"finished_at,omitempty"`
	LastRunAgeSeconds int64                        `json:"last_run_age_seconds,omitempty"`
	SkippedReason     string                       `json:"skipped_reason,omitempty"`
	Summary           IWorkerCenterRecoverySummary `json:"summary"`
}

func NewIWorkerGoalWatchRunner(cfg IWorkerGoalWatchRunnerConfig) *IWorkerGoalWatchRunner {
	limit := cfg.Limit
	if limit <= 0 {
		limit = defaultIWorkerGoalWatchLimit
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultIWorkerGoalWatchTimeout
	}
	note := cfg.Note
	if note == "" {
		note = defaultIWorkerGoalWatchNote
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &IWorkerGoalWatchRunner{recoverer: cfg.Recoverer, limit: limit, timeout: timeout, note: note, now: now}
}

func (r *IWorkerGoalWatchRunner) RunOnce(ctx context.Context) IWorkerGoalWatchRunStatus {
	if r == nil || r.recoverer == nil {
		return IWorkerGoalWatchRunStatus{SkippedReason: "goalwatch_recoverer_not_configured"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	started := r.now().UTC()
	if !r.tryStart(started) {
		status := r.Status()
		status.SkippedReason = "previous_goalwatch_run_still_active"
		return status
	}
	defer r.finishIfRunning()

	runCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	summary := r.recoverer.RecoverEligibleGoalWatchPushesContext(runCtx, r.limit, r.note)
	finished := r.now().UTC()
	if err := runCtx.Err(); err != nil && len(summary.Errors) == 0 {
		summary.Errors = append(summary.Errors, err.Error())
	}
	status := IWorkerGoalWatchRunStatus{StartedAt: started, FinishedAt: finished, Summary: summary}
	r.mu.Lock()
	r.last = status
	r.running = false
	r.startedAt = time.Time{}
	r.mu.Unlock()
	return status
}

func (r *IWorkerGoalWatchRunner) Status() IWorkerGoalWatchRunStatus {
	if r == nil {
		return IWorkerGoalWatchRunStatus{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	status := r.last
	status.Running = r.running
	status.StartedAt = r.startedAt
	if !status.FinishedAt.IsZero() {
		age := r.now().UTC().Sub(status.FinishedAt)
		if age > 0 {
			status.LastRunAgeSeconds = int64(age.Seconds())
		}
	}
	return status
}

func (r *IWorkerGoalWatchRunner) tryStart(now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return false
	}
	r.running = true
	r.startedAt = now
	return true
}

func (r *IWorkerGoalWatchRunner) finishIfRunning() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		r.running = false
		r.startedAt = time.Time{}
	}
}
