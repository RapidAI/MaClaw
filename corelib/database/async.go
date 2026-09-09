package database

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	maxAsyncJobs = 4
	asyncJobTTL  = 10 * time.Minute
)

type asyncJob struct {
	mu             sync.Mutex
	ID             string
	Status         string
	OwnerID        string
	SessionID      string
	ConnectionID   string
	ProfileID      string
	OperationID    string
	Attempt        int
	ParentActionID string
	Fingerprint    string
	ResultHandle   string
	Error          string
	RowCount       int
	Warnings       []string
	ElapsedMS      int64
	Created        time.Time
	Expires        time.Time
	cancel         context.CancelFunc
}

func (j *asyncJob) snapshot() AsyncJobStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	return AsyncJobStatus{
		ContractVersion: ContractVersion,
		JobID:           j.ID,
		Status:          j.Status,
		ProfileID:       j.ProfileID,
		ConnectionID:    j.ConnectionID,
		ResultHandle:    j.ResultHandle,
		RowCount:        j.RowCount,
		Warnings:        append([]string(nil), j.Warnings...),
		Error:           j.Error,
		ElapsedMS:       j.ElapsedMS,
	}
}

func (m *Manager) StartAsyncQuery(ctx context.Context, adapter Adapter, req QueryRequest, ownerID, sessionID, connectionID, profileID string) (AsyncJobStatus, error) {
	if m == nil {
		return AsyncJobStatus{}, fmt.Errorf("database unavailable")
	}
	if adapter == nil {
		return AsyncJobStatus{}, fmt.Errorf("database connection not found")
	}
	if strings.TrimSpace(req.SQL) == "" {
		return AsyncJobStatus{}, fmt.Errorf("syntax: sql is required")
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 30
	}
	if timeout > 600 {
		return AsyncJobStatus{}, fmt.Errorf("quota_exceeded: timeout_seconds must be between 1 and 600")
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return AsyncJobStatus{}, ErrManagerClosed
	}
	if m.jobs == nil {
		m.jobs = make(map[string]*asyncJob)
	}
	m.expireAsyncJobsLocked()
	running := 0
	for _, job := range m.jobs {
		if job.OwnerID == ownerID && (job.Status == "queued" || job.Status == "running") {
			running++
		}
	}
	if running >= maxAsyncJobs {
		m.mu.Unlock()
		return AsyncJobStatus{}, fmt.Errorf("quota_exceeded: too many async database queries")
	}
	jobID := "db-job-" + randomID()
	jobCtx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	job := &asyncJob{
		ID:             jobID,
		Status:         "queued",
		OwnerID:        ownerID,
		SessionID:      sessionID,
		ConnectionID:   connectionID,
		ProfileID:      profileID,
		OperationID:    requestScopeFromContext(ctx).OperationID,
		Attempt:        requestScopeFromContext(ctx).Attempt,
		ParentActionID: requestScopeFromContext(ctx).ParentActionID,
		Fingerprint:    sqlFingerprint(req.SQL),
		Created:        time.Now(),
		Expires:        time.Now().Add(asyncJobTTL),
		cancel:         cancel,
	}
	m.jobs[jobID] = job
	m.mu.Unlock()

	go m.runAsyncQuery(jobCtx, job, adapter, req, ownerID, sessionID, connectionID)
	_ = ctx
	return job.snapshot(), nil
}

func (m *Manager) runAsyncQuery(ctx context.Context, job *asyncJob, adapter Adapter, req QueryRequest, ownerID, sessionID, connectionID string) {
	started := time.Now()
	job.mu.Lock()
	if job.Status == "cancelled" {
		job.mu.Unlock()
		return
	}
	job.Status = "running"
	job.mu.Unlock()

	release, err := m.acquireProfile(ctx, job.ProfileID)
	if err != nil {
		m.recordQueryOutcomeMeta(job.OperationID, job.Attempt, job.ParentActionID, job.Fingerprint, err)
		m.finishAsyncJob(job, started, "", 0, nil, err)
		return
	}
	defer release()

	result, err := adapter.Query(ctx, req)
	if err != nil {
		m.recordQueryOutcomeMeta(job.OperationID, job.Attempt, job.ParentActionID, job.Fingerprint, err)
		m.finishAsyncJob(job, started, "", 0, nil, err)
		return
	}
	job.mu.Lock()
	cancelled := job.Status == "cancelled"
	job.mu.Unlock()
	if cancelled {
		m.finishAsyncJob(job, started, "", 0, nil, context.Canceled)
		return
	}
	result.ProfileID = job.ProfileID
	result.ConnectionID = connectionID
	handle := ""
	if len(result.allRows) > 0 {
		handle = m.storeResultAtWithConnection(result, ownerID, sessionID, connectionID, 0)
	}
	if handle == "" && result.NextCursor != "" {
		handle = result.NextCursor
	}
	if handle == "" && result.ResultHandle != "" {
		handle = result.ResultHandle
	}
	if handle == "" && len(result.Rows) > 0 {
		// Persist even a complete page so the caller can page through the
		// encrypted handle after the Agent loop has moved on.
		cloned := result
		cloned.allRows = result.Rows
		if len(result.allRows) > 0 {
			cloned.allRows = result.allRows
		}
		handle = m.storeResultAtWithConnection(cloned, ownerID, sessionID, connectionID, 0)
	}
	m.metrics.queryCount.Add(1)
	m.recordQueryOutcomeMeta(job.OperationID, job.Attempt, job.ParentActionID, job.Fingerprint, nil)
	if result.Truncated {
		m.metrics.truncations.Add(1)
	}
	m.finishAsyncJob(job, started, handle, result.RowCount, result.Warnings, nil)
}

func (m *Manager) finishAsyncJob(job *asyncJob, started time.Time, handle string, rowCount int, warnings []string, err error) {
	job.mu.Lock()
	job.ElapsedMS = time.Since(started).Milliseconds()
	job.ResultHandle = handle
	job.RowCount = rowCount
	job.Warnings = append([]string(nil), warnings...)
	if err != nil {
		if job.Status != "cancelled" {
			job.Status = "error"
			job.Error = err.Error()
		}
	} else if job.Status != "cancelled" {
		job.Status = "done"
	}
	cancel := job.cancel
	job.cancel = nil
	if !job.Expires.After(time.Now()) {
		job.Expires = time.Now().Add(time.Minute)
	}
	job.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (m *Manager) AsyncJobStatus(jobID, ownerID, sessionID string) (AsyncJobStatus, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return AsyncJobStatus{}, fmt.Errorf("syntax: job_id is required")
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return AsyncJobStatus{}, ErrManagerClosed
	}
	m.expireAsyncJobsLocked()
	job, ok := m.jobs[jobID]
	m.mu.Unlock()
	if !ok || job == nil {
		return AsyncJobStatus{}, fmt.Errorf("permission: async job not found")
	}
	if job.OwnerID != "" && (job.OwnerID != ownerID || job.SessionID != sessionID) {
		return AsyncJobStatus{}, fmt.Errorf("permission: async job belongs to another session")
	}
	return job.snapshot(), nil
}

func (m *Manager) cancelAsyncJobsForConnectionLocked(connectionID string) {
	cancels := make([]context.CancelFunc, 0)
	for _, job := range m.jobs {
		if job == nil || job.ConnectionID != connectionID {
			continue
		}
		job.mu.Lock()
		if job.Status == "queued" || job.Status == "running" {
			job.Status = "cancelled"
			job.Error = "cancelled"
			if job.cancel != nil {
				cancels = append(cancels, job.cancel)
				job.cancel = nil
			}
		}
		job.mu.Unlock()
	}
	for _, cancel := range cancels {
		cancel()
	}
}

func (m *Manager) takeJobCancelsLocked() []context.CancelFunc {
	cancels := make([]context.CancelFunc, 0, len(m.jobs))
	for _, job := range m.jobs {
		job.mu.Lock()
		if job.Status == "queued" || job.Status == "running" {
			job.Status = "cancelled"
			job.Error = "cancelled"
		}
		if job.cancel != nil {
			cancels = append(cancels, job.cancel)
			job.cancel = nil
		}
		job.mu.Unlock()
	}
	return cancels
}

func (m *Manager) expireAsyncJobsLocked() {
	now := time.Now()
	cancels := make([]context.CancelFunc, 0)
	for id, job := range m.jobs {
		if job == nil {
			delete(m.jobs, id)
			continue
		}
		job.mu.Lock()
		if !now.After(job.Expires) {
			job.mu.Unlock()
			continue
		}
		running := job.Status == "queued" || job.Status == "running"
		if job.cancel != nil {
			cancels = append(cancels, job.cancel)
			job.cancel = nil
		}
		if running {
			job.Status = "cancelled"
			job.Error = "cancelled"
			job.Expires = now.Add(time.Minute)
			job.mu.Unlock()
			continue
		}
		job.mu.Unlock()
		delete(m.jobs, id)
	}
	for _, cancel := range cancels {
		cancel()
	}
}
