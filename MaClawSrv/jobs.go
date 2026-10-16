package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/fileutil"
)

// asyncJobStatus remains a source-compatible host alias while the canonical
// vocabulary lives in the transport-neutral Runtime contract.
type asyncJobStatus = agentruntime.JobStatus

const (
	asyncJobStatusPending   = agentruntime.JobStatusPending
	asyncJobStatusRunning   = agentruntime.JobStatusRunning
	asyncJobStatusSucceeded = agentruntime.JobStatusSucceeded
	asyncJobStatusFailed    = agentruntime.JobStatusFailed
	asyncJobStatusCanceled  = agentruntime.JobStatusCanceled
	asyncJobStatusUnknown   = agentruntime.JobStatusUnknown
)

const (
	asyncJobRetention = agentruntime.DefaultJobRetention
	asyncJobMaxCount  = agentruntime.DefaultJobMaxCount
	asyncJobLeaseTTL  = agentruntime.DefaultJobLeaseTTL
	asyncJobLeaseTick = agentruntime.DefaultJobLeaseTick
)

// asyncJobView remains a source-compatible alias for the shared Runtime Job
// envelope used by the HTTP transport.
type asyncJobView = agentruntime.Job

type asyncJobRecord struct {
	asyncJobView
	cancel context.CancelFunc
}

type asyncJobSnapshot struct {
	Items []asyncJobView `json:"items"`
}

type asyncJobManager struct {
	mu                   sync.RWMutex
	jobs                 map[string]*asyncJobRecord
	dataRoot             string
	repositoryPath       string
	repository           agentruntime.JobRepository
	effectRepositoryPath string
	effectRepository     agentruntime.JobEffectRepository
	effectCleanupPath    string
	// effectCleanupPending contains Job ids whose primary row was deleted but
	// whose protected receipt cleanup failed. The map is hydrated from a small
	// durable marker so a restart cannot strand orphaned protected receipts.
	effectCleanupPending map[string]struct{}
	// store/filePath are a single-writer compatibility hook retained for
	// legacy adapter tests. Production composition leaves store nil and uses
	// the granular SQLite repository exclusively.
	store                agentruntime.JobStore
	filePath             string
	instanceID           string
	leaseStop            chan struct{}
	leaseWake            chan struct{}
	leaseWG              sync.WaitGroup
	leaseRunning         bool
	lastPersistErr       error
	lastEffectPersistErr error
	closed               bool
}

type asyncJobReporter struct {
	manager *asyncJobManager
	jobID   string
}

type asyncJobEffectRecorder struct {
	manager *asyncJobManager
	jobID   string
}

var _ agentruntime.JobReporter = asyncJobReporter{}
var _ agentruntime.JobEffectRecorder = asyncJobEffectRecorder{}

func (r asyncJobReporter) ReportJobUpdate(ctx context.Context, update agentruntime.JobUpdate) error {
	if r.manager == nil {
		return agentruntime.ErrJobReporterUnavailable
	}
	return r.manager.reportJobUpdate(ctx, r.jobID, update)
}

func (r asyncJobEffectRecorder) PrepareJobEffect(ctx context.Context, preparation agentruntime.JobEffectPreparation) (agentruntime.JobEffect, error) {
	if r.manager == nil {
		return agentruntime.JobEffect{}, agentruntime.ErrJobEffectUnavailable
	}
	return r.manager.prepareJobEffect(ctx, r.jobID, preparation)
}

func (r asyncJobEffectRecorder) BindJobEffect(ctx context.Context, binding agentruntime.JobEffectBinding) (agentruntime.JobEffect, error) {
	if r.manager == nil {
		return agentruntime.JobEffect{}, agentruntime.ErrJobEffectUnavailable
	}
	return r.manager.bindJobEffect(ctx, r.jobID, binding)
}

func (r asyncJobEffectRecorder) SettleJobEffect(ctx context.Context, settlement agentruntime.JobEffectSettlement) (agentruntime.JobEffect, error) {
	if r.manager == nil {
		return agentruntime.JobEffect{}, agentruntime.ErrJobEffectUnavailable
	}
	return r.manager.settleJobEffect(ctx, r.jobID, settlement)
}

func newAsyncJobManager(dataRoot string) *asyncJobManager {
	m := &asyncJobManager{
		jobs:                 map[string]*asyncJobRecord{},
		effectCleanupPending: map[string]struct{}{},
		dataRoot:             dataRoot,
		instanceID:           agentservice.NewID("job_executor"),
		leaseStop:            make(chan struct{}),
		leaseWake:            make(chan struct{}, 1),
	}
	root := filepath.Clean(filepath.Join(dataRoot, "state"))
	if stringsTrim(dataRoot) == "" {
		return m
	}
	m.repositoryPath = filepath.Join(root, "jobs.db")
	m.effectRepositoryPath = filepath.Join(root, "job_effects.db")
	m.effectCleanupPath = filepath.Join(root, "job_effect_cleanup.json")
	m.filePath = filepath.Join(root, "jobs.json")
	// Construction is read-only with respect to a missing data root. The
	// Service/readiness layer owns root provisioning; silently recreating a
	// disappeared mount here could make an empty repository look healthy.
	if _, err := os.Stat(dataRoot); err != nil {
		m.lastPersistErr = err
		return m
	}
	repository, err := newSQLiteAsyncJobRepository(m.repositoryPath, m.filePath)
	if err != nil {
		m.lastPersistErr = err
		return m
	}
	m.repository = repository
	effectRepository, effectErr := agentservice.NewSQLiteJobEffectRepository(m.effectRepositoryPath)
	if effectErr != nil {
		m.lastEffectPersistErr = effectErr
	} else {
		m.effectRepository = effectRepository
	}
	m.loadPendingEffectCleanup()
	m.loadFromRepository()
	return m
}

func (m *asyncJobManager) createUserJob(kind string, p agentservice.Principal, run func(context.Context) (any, error)) *asyncJobRecord {
	return m.createUserJobWithPolicies(kind, p, agentruntime.JobRecoveryPolicyReconcile, agentruntime.JobRetryPolicy{}, run)
}

func (m *asyncJobManager) createUserJobWithRecoveryPolicy(kind string, p agentservice.Principal, recoveryPolicy agentruntime.JobRecoveryPolicy, run func(context.Context) (any, error)) *asyncJobRecord {
	return m.createUserJobWithPolicies(kind, p, recoveryPolicy, agentruntime.JobRetryPolicy{}, run)
}

func (m *asyncJobManager) createUserJobWithPolicies(kind string, p agentservice.Principal, recoveryPolicy agentruntime.JobRecoveryPolicy, retryPolicy agentruntime.JobRetryPolicy, run func(context.Context) (any, error)) *asyncJobRecord {
	job, _ := m.createUserJobWithAdmission(kind, p, recoveryPolicy, retryPolicy, agentruntime.JobIdempotencyIdentity{}, run)
	return job
}

func (m *asyncJobManager) createUserJobIdempotent(kind string, p agentservice.Principal, recoveryPolicy agentruntime.JobRecoveryPolicy, retryPolicy agentruntime.JobRetryPolicy, identity agentruntime.JobIdempotencyIdentity, run func(context.Context) (any, error)) (*asyncJobRecord, error) {
	if identity.Digest == "" {
		return nil, agentruntime.ErrInvalidJobIdempotency
	}
	return m.createUserJobWithAdmission(kind, p, recoveryPolicy, retryPolicy, identity, run)
}

func (m *asyncJobManager) createUserJobWithAdmission(kind string, p agentservice.Principal, recoveryPolicy agentruntime.JobRecoveryPolicy, retryPolicy agentruntime.JobRetryPolicy, identity agentruntime.JobIdempotencyIdentity, run func(context.Context) (any, error)) (*asyncJobRecord, error) {
	if err := agentruntime.ValidateJobIdempotencyState(identity.Digest, identity.RequestDigest, false); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	baseCtx, cancel := context.WithCancel(context.Background())
	job := &asyncJobRecord{asyncJobView: asyncJobView{
		ID:                agentservice.NewID("job"),
		LeaseOwnerID:      m.instanceID,
		LeaseExpiresAt:    asyncJobLeaseDeadline(now),
		Kind:              kind,
		Status:            asyncJobStatusPending,
		RecoveryPolicy:    agentruntime.NormalizeJobRecoveryPolicy(recoveryPolicy),
		RetryPolicy:       agentruntime.NormalizeJobRetryPolicy(retryPolicy),
		IdempotencyDigest: identity.Digest,
		RequestDigest:     identity.RequestDigest,
		TenantID:          p.TenantID,
		UserID:            p.UserID,
		CreatedAt:         now,
	}, cancel: cancel}
	ctx := agentruntime.WithJobReporter(baseCtx, asyncJobReporter{manager: m, jobID: job.ID})
	ctx = agentruntime.WithJobEffectRecorder(ctx, asyncJobEffectRecorder{manager: m, jobID: job.ID})
	ctx = agentruntime.WithJobIdempotencyIdentity(ctx, identity)
	m.mu.Lock()
	if m.closed {
		job.asyncJobView = agentruntime.ApplyJobWorkerOutcome(job.asyncJobView, now, agentruntime.JobWorkerOutcome{
			Err:       agentservice.ErrServiceClosed,
			ErrorCode: agentservice.ErrorCode(agentservice.ErrServiceClosed),
			ErrorText: "service is closed",
		})
		job.asyncJobView = agentruntime.PrepareJobLeaseForPersist(job.asyncJobView, m.instanceID, now, asyncJobLeaseTTL)
		out := m.snapshot(job)
		m.mu.Unlock()
		cancel()
		return out, nil
	}
	m.pruneLocked(now)
	if m.store != nil {
		if identity.Digest != "" {
			for _, existing := range m.jobs {
				if existing.IdempotencyDigest != identity.Digest {
					continue
				}
				if existing.TenantID != p.TenantID || existing.UserID != p.UserID || existing.Kind != kind || existing.RequestDigest != identity.RequestDigest {
					m.mu.Unlock()
					cancel()
					return nil, agentruntime.ErrJobIdempotencyConflict
				}
				out := m.snapshot(existing)
				out.IdempotentReplay = true
				m.mu.Unlock()
				cancel()
				return out, nil
			}
		}
		job.Version = 1
		m.jobs[job.ID] = job
		if err := m.persistLegacySnapshotLocked(); err != nil {
			job.Version = 0
			job.IdempotencyDigest = ""
			job.RequestDigest = ""
			m.markPersistenceFailureLocked(job, err, now)
			out := m.snapshot(job)
			m.mu.Unlock()
			cancel()
			return out, nil
		}
		out := m.snapshot(job)
		jobID := job.ID
		m.mu.Unlock()
		go m.execute(ctx, jobID, run)
		return out, nil
	}
	if m.repository == nil {
		err := errors.New("async job repository is unavailable")
		m.lastPersistErr = err
		job.IdempotencyDigest = ""
		job.RequestDigest = ""
		m.markPersistenceFailureLocked(job, err, now)
		m.jobs[job.ID] = job
		out := m.snapshot(job)
		m.mu.Unlock()
		cancel()
		return out, nil
	}
	canonical, created, err := m.repository.Admit(context.Background(), job.asyncJobView)
	if errors.Is(err, agentruntime.ErrJobIdempotencyConflict) {
		m.mu.Unlock()
		cancel()
		return nil, err
	}
	if err != nil {
		// No worker was started and no durable admission exists, so retaining
		// the idempotency identity would turn a safe retry into a replay of an
		// in-memory failure. Keep the failed diagnostic Job but release the key.
		job.IdempotencyDigest = ""
		job.RequestDigest = ""
		m.markPersistenceFailureLocked(job, err, now)
		m.lastPersistErr = err
		m.jobs[job.ID] = job
		out := m.snapshot(job)
		m.mu.Unlock()
		cancel()
		return out, nil
	}
	m.lastPersistErr = nil
	if !created {
		existing := m.mergeRepositoryJobLocked(canonical)
		out := m.snapshot(existing)
		out.IdempotentReplay = true
		m.mu.Unlock()
		cancel()
		return out, nil
	}
	job.asyncJobView = canonical
	m.jobs[job.ID] = job
	m.ensureLeaseCoordinatorLocked()
	out := m.snapshot(job)
	jobID := job.ID
	m.mu.Unlock()
	go m.execute(ctx, jobID, run)
	return out, nil
}

func (m *asyncJobManager) updateProgress(jobID string, progress float64, text string) {
	if m == nil || strings.TrimSpace(jobID) == "" {
		return
	}
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	_ = m.reportJobUpdate(context.Background(), jobID, agentruntime.JobUpdate{Progress: &progress, ProgressText: text})
}

func (m *asyncJobManager) reportJobUpdate(ctx context.Context, jobID string, update agentruntime.JobUpdate) error {
	if m == nil || strings.TrimSpace(jobID) == "" {
		return agentruntime.ErrJobReporterUnavailable
	}
	if err := agentruntime.ValidateJobUpdate(update); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	job := m.jobs[jobID]
	if m.closed || job == nil || (job.Status != asyncJobStatusPending && job.Status != asyncJobStatusRunning) {
		return agentruntime.ErrJobNotActive
	}
	if update.Checkpoint != nil && job.Checkpoint != nil && update.Checkpoint.Sequence <= job.Checkpoint.Sequence {
		return agentruntime.ErrStaleJobCheckpoint
	}
	oldProgress, oldProgressText := job.Progress, job.ProgressText
	oldCheckpoint := cloneJobCheckpoint(job.Checkpoint)
	if update.Progress != nil {
		job.Progress = *update.Progress
		job.ProgressText = strings.TrimSpace(update.ProgressText)
	}
	if update.Checkpoint != nil {
		checkpoint := *update.Checkpoint
		checkpoint.Phase = strings.TrimSpace(checkpoint.Phase)
		checkpoint.UpdatedAt = time.Now().UTC()
		job.Checkpoint = &checkpoint
	}
	if err := m.persistJobLocked(job); err != nil {
		if errors.Is(err, agentruntime.ErrJobRepositoryVersionConflict) {
			return err
		}
		job.Progress = oldProgress
		job.ProgressText = oldProgressText
		job.Checkpoint = oldCheckpoint
		return err
	}
	return nil
}

func (m *asyncJobManager) prepareJobEffect(ctx context.Context, jobID string, preparation agentruntime.JobEffectPreparation) (agentruntime.JobEffect, error) {
	if m == nil || strings.TrimSpace(jobID) == "" {
		return agentruntime.JobEffect{}, agentruntime.ErrJobEffectUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return agentruntime.JobEffect{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	job, repository, err := m.effectJobLocked(jobID)
	if err != nil {
		return agentruntime.JobEffect{}, err
	}
	now := time.Now().UTC()
	effect := agentruntime.JobEffect{
		JobID: job.ID, TenantID: job.TenantID, UserID: job.UserID,
		JobKind: job.Kind, Kind: strings.TrimSpace(preparation.Kind),
		State: agentruntime.JobEffectPrepared, Payload: append(json.RawMessage(nil), preparation.Payload...),
		CreatedAt: now,
	}
	canonical, _, err := repository.Prepare(ctx, effect)
	if err != nil {
		m.lastEffectPersistErr = err
		return agentruntime.JobEffect{}, err
	}
	m.lastEffectPersistErr = nil
	return canonical, nil
}

func (m *asyncJobManager) bindJobEffect(ctx context.Context, jobID string, binding agentruntime.JobEffectBinding) (agentruntime.JobEffect, error) {
	if m == nil || strings.TrimSpace(jobID) == "" {
		return agentruntime.JobEffect{}, agentruntime.ErrJobEffectUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return agentruntime.JobEffect{}, err
	}
	if strings.TrimSpace(binding.ResourceID) == "" {
		return agentruntime.JobEffect{}, agentruntime.ErrInvalidJobEffect
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	job, repository, err := m.effectJobLocked(jobID)
	if err != nil {
		return agentruntime.JobEffect{}, err
	}
	current, err := repository.Get(ctx, job.ID, strings.TrimSpace(binding.Kind))
	if err != nil {
		m.lastEffectPersistErr = err
		return agentruntime.JobEffect{}, err
	}
	if !jobEffectMatchesJob(current, job.asyncJobView) {
		return agentruntime.JobEffect{}, agentruntime.ErrJobEffectConflict
	}
	resourceID := strings.TrimSpace(binding.ResourceID)
	if current.ResourceID == resourceID && resourceID != "" {
		m.lastEffectPersistErr = nil
		return current, nil
	}
	current.ResourceID = resourceID
	updated, err := repository.Update(ctx, current.Version, current)
	if err != nil {
		m.lastEffectPersistErr = err
		return agentruntime.JobEffect{}, err
	}
	m.lastEffectPersistErr = nil
	return updated, nil
}

func (m *asyncJobManager) settleJobEffect(ctx context.Context, jobID string, settlement agentruntime.JobEffectSettlement) (agentruntime.JobEffect, error) {
	if m == nil || strings.TrimSpace(jobID) == "" {
		return agentruntime.JobEffect{}, agentruntime.ErrJobEffectUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return agentruntime.JobEffect{}, err
	}
	if settlement.State != agentruntime.JobEffectCommitted && settlement.State != agentruntime.JobEffectFailed && settlement.State != agentruntime.JobEffectUnknown {
		return agentruntime.JobEffect{}, agentruntime.ErrInvalidJobEffect
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	job, repository, err := m.effectJobLocked(jobID)
	if err != nil {
		return agentruntime.JobEffect{}, err
	}
	current, err := repository.Get(ctx, job.ID, strings.TrimSpace(settlement.Kind))
	if err != nil {
		m.lastEffectPersistErr = err
		return agentruntime.JobEffect{}, err
	}
	if !jobEffectMatchesJob(current, job.asyncJobView) {
		return agentruntime.JobEffect{}, agentruntime.ErrJobEffectConflict
	}
	if current.State == agentruntime.JobEffectCommitted || current.State == agentruntime.JobEffectFailed {
		if current.State == settlement.State && current.ReceiptDigest == settlement.ReceiptDigest && current.ReasonCode == strings.TrimSpace(settlement.ReasonCode) && (len(settlement.Payload) == 0 || string(current.Payload) == string(settlement.Payload)) {
			m.lastEffectPersistErr = nil
			return current, nil
		}
		return agentruntime.JobEffect{}, agentruntime.ErrJobEffectConflict
	}
	current.State = settlement.State
	current.ReceiptDigest = strings.TrimSpace(settlement.ReceiptDigest)
	current.ReasonCode = strings.TrimSpace(settlement.ReasonCode)
	if len(settlement.Payload) > 0 {
		current.Payload = append(json.RawMessage(nil), settlement.Payload...)
	}
	updated, err := repository.Update(ctx, current.Version, current)
	if err != nil {
		m.lastEffectPersistErr = err
		return agentruntime.JobEffect{}, err
	}
	m.lastEffectPersistErr = nil
	return updated, nil
}

func (m *asyncJobManager) effectJobLocked(jobID string) (*asyncJobRecord, agentruntime.JobEffectRepository, error) {
	if m.closed {
		return nil, nil, agentservice.ErrServiceClosed
	}
	job := m.jobs[jobID]
	if job == nil || !agentruntime.JobIsActive(job.asyncJobView.Status) || job.cancel == nil || job.LeaseOwnerID != m.instanceID {
		return nil, nil, agentruntime.ErrJobNotActive
	}
	if m.effectRepository == nil {
		err := errors.New("async job effect repository is unavailable")
		m.lastEffectPersistErr = err
		return nil, nil, errors.Join(agentruntime.ErrJobEffectUnavailable, err)
	}
	return job, m.effectRepository, nil
}

func jobEffectMatchesJob(effect agentruntime.JobEffect, job agentruntime.Job) bool {
	return agentruntime.JobEffectMatchesJob(effect, job)
}

func (m *asyncJobManager) execute(ctx context.Context, jobID string, run func(context.Context) (any, error)) {
	for {
		if err := ctx.Err(); err != nil {
			m.completeJob(ctx, jobID, nil, err)
			return
		}
		attempt, ok := m.beginJobAttempt(jobID)
		if !ok {
			return
		}
		result, err := run(agentruntime.WithJobAttempt(ctx, attempt))
		if err != nil && ctx.Err() == nil && agentruntime.IsJobErrorRetryable(err) {
			delay, scheduled, stop := m.prepareJobRetry(jobID, err)
			if stop {
				return
			}
			if scheduled {
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					m.completeJob(ctx, jobID, nil, ctx.Err())
					return
				case <-timer.C:
					continue
				}
			}
		}
		m.completeJob(ctx, jobID, result, err)
		return
	}
}

func (m *asyncJobManager) beginJobAttempt(jobID string) (uint32, bool) {
	started := time.Now().UTC()
	m.mu.Lock()
	job := m.jobs[jobID]
	if job == nil || m.closed || job.Status == asyncJobStatusCanceled || (job.Status != asyncJobStatusPending && job.Status != asyncJobStatusRunning) {
		m.mu.Unlock()
		return 0, false
	}
	next, ok := agentruntime.BeginJobAttempt(job.asyncJobView, started)
	if !ok {
		m.mu.Unlock()
		return 0, false
	}
	job.asyncJobView = next
	if err := m.persistJobLocked(job); err != nil {
		if isJobRepositoryConcurrencyError(err) {
			cancelFn := job.cancel
			job.cancel = nil
			m.mu.Unlock()
			if cancelFn != nil {
				cancelFn()
			}
			return 0, false
		}
		m.markPersistenceFailureLocked(job, err, started)
		cancelFn := job.cancel
		job.cancel = nil
		m.mu.Unlock()
		if cancelFn != nil {
			cancelFn()
		}
		return 0, false
	}
	attempt := job.Attempt
	m.mu.Unlock()
	return attempt, true
}

func (m *asyncJobManager) prepareJobRetry(jobID string, attemptErr error) (time.Duration, bool, bool) {
	m.mu.Lock()
	job := m.jobs[jobID]
	if job == nil || m.closed || job.Status == asyncJobStatusCanceled {
		m.mu.Unlock()
		return 0, false, true
	}
	delay := agentruntime.JobRetryBackoff(job.RetryPolicy, job.Attempt)
	if delay <= 0 {
		m.mu.Unlock()
		return 0, false, false
	}
	now := time.Now().UTC()
	job.asyncJobView = agentruntime.ScheduleJobRetry(job.asyncJobView, delay, agentservice.ErrorCode(attemptErr), redactAsyncJobText(m.dataRoot, attemptErr.Error()), now)
	if err := m.persistJobLocked(job); err != nil {
		if isJobRepositoryConcurrencyError(err) {
			cancelFn := job.cancel
			job.cancel = nil
			m.mu.Unlock()
			if cancelFn != nil {
				cancelFn()
			}
			return 0, false, true
		}
		m.markPersistenceFailureLocked(job, err, now)
		cancelFn := job.cancel
		job.cancel = nil
		m.mu.Unlock()
		if cancelFn != nil {
			cancelFn()
		}
		return 0, false, true
	}
	m.mu.Unlock()
	return delay, true, false
}

func (m *asyncJobManager) completeJob(ctx context.Context, jobID string, result any, err error) {
	completed := time.Now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	defer m.signalLeaseCoordinatorLocked()
	job := m.jobs[jobID]
	if job == nil {
		return
	}
	if m.closed || !agentruntime.JobIsActive(job.Status) {
		return
	}
	job.cancel = nil
	outcome := agentruntime.JobWorkerOutcome{}
	switch {
	case err != nil && errors.Is(err, agentruntime.ErrJobEffectOutcomeUncertain):
		outcome.Uncertain = true
	case err != nil && ctx.Err() == context.Canceled:
		outcome.Canceled = true
	case err != nil:
		outcome.Err = err
		outcome.ErrorCode = agentservice.ErrorCode(err)
		outcome.ErrorText = redactAsyncJobText(m.dataRoot, err.Error())
	default:
		payload, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			outcome.Err = marshalErr
			outcome.ErrorCode = agentruntime.JobErrorCodeResultNotSerializable
			outcome.ErrorText = redactAsyncJobText(m.dataRoot, marshalErr.Error())
		} else {
			outcome.Result = redactAsyncJobRawMessage(m.dataRoot, payload)
		}
	}
	job.asyncJobView = agentruntime.ApplyJobWorkerOutcome(job.asyncJobView, completed, outcome)
	if persistErr := m.persistJobLocked(job); persistErr != nil && !isJobRepositoryConcurrencyError(persistErr) {
		m.markCompletionPersistenceFailureLocked(job, completed)
	}
}

func (m *asyncJobManager) getUserJob(jobID string, p agentservice.Principal) (*asyncJobRecord, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshFromRepositoryLocked()
	m.pruneLocked(time.Now().UTC())
	job, ok := m.jobs[jobID]
	if !ok || job.TenantID != p.TenantID || job.UserID != p.UserID {
		return nil, false
	}
	return m.snapshot(job), true
}

// reconcileUserJob asks a domain handler to inspect protected effect evidence
// for an unknown Job, then commits only an evidence-backed terminal CAS. It
// never receives or invokes the original worker closure, so reconciliation
// cannot become an accidental replay path.
func (m *asyncJobManager) reconcileUserJob(ctx context.Context, jobID string, p agentservice.Principal, reconciler agentruntime.JobReconciler) (*asyncJobRecord, bool, error) {
	job, ok := m.getUserJob(jobID, p)
	if !ok || job.Status != asyncJobStatusUnknown || reconciler == nil {
		return job, ok, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.RLock()
	effectRepository := m.effectRepository
	m.mu.RUnlock()
	if effectRepository == nil {
		return job, true, agentruntime.ErrJobEffectUnavailable
	}
	effects, err := effectRepository.ListByJob(ctx, job.ID)
	if err != nil {
		m.mu.Lock()
		m.lastEffectPersistErr = err
		m.mu.Unlock()
		return job, true, err
	}
	m.mu.Lock()
	m.lastEffectPersistErr = nil
	m.mu.Unlock()
	for _, effect := range effects {
		if !jobEffectMatchesJob(effect, job.asyncJobView) {
			return job, true, agentruntime.ErrJobEffectConflict
		}
	}
	result, err := reconciler.ReconcileJob(ctx, job.asyncJobView, effects)
	if err != nil {
		return job, true, err
	}
	if err := agentruntime.ValidateJobReconcileResult(result); err != nil {
		return job, true, err
	}
	if !result.Resolved {
		return job, true, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshFromRepositoryLocked()
	current := m.jobs[job.ID]
	if current == nil || current.TenantID != p.TenantID || current.UserID != p.UserID {
		return nil, false, nil
	}
	if current.Status != asyncJobStatusUnknown || current.Version != job.Version {
		return m.snapshot(current), true, nil
	}
	old := cloneAsyncJobEnvelope(current.asyncJobView)
	current.asyncJobView = agentruntime.ApplyResolvedJobReconcile(current.asyncJobView, result, time.Now().UTC())
	current.Result = redactAsyncJobRawMessage(m.dataRoot, current.Result)
	current.Error = redactAsyncJobText(m.dataRoot, current.Error)
	if err := m.persistJobLocked(current); err != nil {
		if !isJobRepositoryConcurrencyError(err) {
			current.asyncJobView = old
		}
		return m.snapshot(current), true, err
	}
	return m.snapshot(current), true, nil
}

func (m *asyncJobManager) listUserJobs(p agentservice.Principal, kind string, status asyncJobStatus) []asyncJobRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshFromRepositoryLocked()
	m.pruneLocked(time.Now().UTC())
	items := make([]asyncJobRecord, 0, len(m.jobs))
	kind = stringsTrim(kind)
	for _, job := range m.jobs {
		if job.TenantID != p.TenantID || job.UserID != p.UserID {
			continue
		}
		if kind != "" && job.Kind != kind {
			continue
		}
		if status != "" && job.Status != status {
			continue
		}
		items = append(items, *m.snapshot(job))
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items
}

func (m *asyncJobManager) cancelUserJob(jobID string, p agentservice.Principal) (*asyncJobRecord, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshFromRepositoryLocked()
	m.pruneLocked(time.Now().UTC())
	job, ok := m.jobs[jobID]
	if !ok || job.TenantID != p.TenantID || job.UserID != p.UserID {
		return nil, false
	}
	if job.Status == asyncJobStatusPending || job.Status == asyncJobStatusRunning {
		old := cloneAsyncJobEnvelope(job.asyncJobView)
		job.asyncJobView = agentruntime.StampJobCanceled(job.asyncJobView, time.Now().UTC(), "")
		if err := m.persistJobLocked(job); err != nil {
			if !isJobRepositoryConcurrencyError(err) {
				job.asyncJobView = old
			}
			return m.snapshot(job), true
		}
		if job.cancel != nil {
			job.cancel()
		}
		job.cancel = nil
		m.signalLeaseCoordinatorLocked()
	}
	return m.snapshot(job), true
}

func (m *asyncJobManager) deleteUserJob(jobID string, p agentservice.Principal) (*asyncJobRecord, bool, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshFromRepositoryLocked()
	m.pruneLocked(time.Now().UTC())
	job, ok := m.jobs[jobID]
	if !ok || job.TenantID != p.TenantID || job.UserID != p.UserID {
		return nil, false, false
	}
	if job.Status == asyncJobStatusPending || job.Status == asyncJobStatusRunning {
		return m.snapshot(job), true, false
	}
	out := m.snapshot(job)
	delete(m.jobs, jobID)
	if err := m.deleteJobsLocked([]agentruntime.JobVersion{{ID: job.ID, Version: job.Version}}); err != nil {
		// Deletion is also durable state. Restore the in-memory record when the
		// replacement cannot be committed, otherwise a subsequent read would
		// claim success while the record reappears after restart.
		m.jobs[jobID] = job
		return m.snapshot(job), true, false
	}
	return out, true, true
}

func (m *asyncJobManager) deleteUserJobs(p agentservice.Principal, kind string, status asyncJobStatus, before *time.Time) []asyncJobRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshFromRepositoryLocked()
	m.pruneLocked(time.Now().UTC())
	kind = stringsTrim(kind)
	deleted := make([]asyncJobRecord, 0)
	removed := make(map[string]*asyncJobRecord)
	for id, job := range m.jobs {
		if job.TenantID != p.TenantID || job.UserID != p.UserID {
			continue
		}
		if job.Status == asyncJobStatusPending || job.Status == asyncJobStatusRunning {
			continue
		}
		if kind != "" && job.Kind != kind {
			continue
		}
		if status != "" && job.Status != status {
			continue
		}
		if before != nil && !job.CreatedAt.Before(*before) {
			continue
		}
		deleted = append(deleted, *m.snapshot(job))
		removed[id] = job
		delete(m.jobs, id)
	}
	if len(deleted) > 0 {
		sort.Slice(deleted, func(i, j int) bool {
			return deleted[i].CreatedAt.Before(deleted[j].CreatedAt)
		})
		expected := make([]agentruntime.JobVersion, 0, len(removed))
		for _, record := range removed {
			expected = append(expected, agentruntime.JobVersion{ID: record.ID, Version: record.Version})
		}
		if err := m.deleteJobsLocked(expected); err != nil {
			for id, record := range removed {
				m.jobs[id] = record
			}
			return nil
		}
	}
	return deleted
}

func (m *asyncJobManager) pruneLocked(now time.Time) {
	m.retryPendingEffectCleanupLocked()
	if len(m.jobs) == 0 {
		return
	}
	changed := false
	removed := make(map[string]*asyncJobRecord)
	// Unknown Jobs stay as reconciliation evidence; the shared selector never
	// returns them for age or count housekeeping.
	items := make([]agentruntime.Job, 0, len(m.jobs))
	for _, job := range m.jobs {
		items = append(items, job.asyncJobView)
	}
	for _, job := range agentruntime.SelectJobsForRetentionPrune(items, now, asyncJobRetention, asyncJobMaxCount) {
		record := m.jobs[job.ID]
		if record == nil {
			continue
		}
		removed[job.ID] = record
		delete(m.jobs, job.ID)
		changed = true
	}
	if changed {
		expected := make([]agentruntime.JobVersion, 0, len(removed))
		for _, job := range removed {
			if job.Version > 0 {
				expected = append(expected, agentruntime.JobVersion{ID: job.ID, Version: job.Version})
			}
		}
		if err := m.deleteJobsLocked(expected); err != nil {
			// Pruning is a durable deletion just like an explicit DELETE API.
			// Restore records when the replacement cannot be committed so an
			// in-memory read never disagrees with the next process restart.
			for id, job := range removed {
				m.jobs[id] = job
			}
		}
	}
}

func (m *asyncJobManager) snapshotCounts() map[asyncJobStatus]int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshFromRepositoryLocked()
	m.pruneLocked(time.Now().UTC())
	counts := map[asyncJobStatus]int{}
	for _, job := range m.jobs {
		counts[job.Status]++
	}
	return counts
}
func (m *asyncJobManager) loadFromRepository() {
	if m.repository == nil {
		return
	}
	items, err := m.repository.List(context.Background())
	if err != nil {
		m.lastPersistErr = err
		return
	}
	if len(items) == 0 {
		return
	}
	now := time.Now().UTC()
	for _, item := range items {
		record := &asyncJobRecord{asyncJobView: item}
		record.Status = agentruntime.NormalizeJobStatus(record.Status)
		record.RecoveryPolicy = agentruntime.NormalizeJobRecoveryPolicy(record.RecoveryPolicy)
		record.RetryPolicy = agentruntime.NormalizeJobRetryPolicy(record.RetryPolicy)
		changed := false
		if agentruntime.JobIsActive(record.asyncJobView.Status) && agentruntime.JobLeaseExpired(record.asyncJobView, now) {
			m.applyExpiredJobRecoveryLocked(record, now)
			changed = true
		} else if record.Status == asyncJobStatusUnknown && record.ErrorCode == "" {
			record.ErrorCode = agentruntime.JobErrorCodeReconcileRequired
			if strings.TrimSpace(record.Error) == "" {
				record.Error = "job outcome is uncertain; reconciliation required"
			}
			changed = true
		}
		m.jobs[record.ID] = record
		if changed {
			if err := m.persistJobLocked(record); err != nil && !isJobRepositoryConcurrencyError(err) {
				m.lastPersistErr = err
			}
		}
	}
	m.pruneLocked(now)
}

// markPersistenceFailureLocked converts an otherwise successful/active job
// into a terminal, explicit failure. The method intentionally does not call
// persist again: the underlying error is already known and recursive
// retries would obscure the original admission failure.
func (m *asyncJobManager) markPersistenceFailureLocked(job *asyncJobRecord, _ error, at time.Time) {
	if job == nil {
		return
	}
	job.asyncJobView = agentruntime.MarkJobPersistenceFailed(job.asyncJobView, at)
}

// markCompletionPersistenceFailureLocked distinguishes an ordinary Job
// envelope write failure from a domain worker that already recorded a
// protected side effect. In the latter case reporting failed would invite a
// caller to retry an operation that may have committed; keep it unknown until
// the domain reconciler can inspect the effect evidence.
func (m *asyncJobManager) markCompletionPersistenceFailureLocked(job *asyncJobRecord, at time.Time) {
	if job == nil {
		return
	}
	hasEffects := false
	if m != nil && m.effectRepository != nil {
		if effects, err := m.effectRepository.ListByJob(context.Background(), job.ID); err == nil && len(effects) > 0 {
			hasEffects = true
		}
	}
	job.asyncJobView = agentruntime.MarkJobCompletionPersistFailed(job.asyncJobView, at, hasEffects)
}

func (m *asyncJobManager) persistJobLocked(job *asyncJobRecord) error {
	if job == nil {
		return nil
	}
	previousLease := cloneAsyncJobTime(job.LeaseExpiresAt)
	m.prepareJobLeaseForPersistLocked(job, time.Now().UTC())
	if m.store != nil {
		previousVersion := job.Version
		if job.Version == 0 {
			job.Version = 1
		} else {
			job.Version++
		}
		if err := m.persistLegacySnapshotLocked(); err != nil {
			job.Version = previousVersion
			job.LeaseExpiresAt = previousLease
			return err
		}
		return nil
	}
	if m.repository == nil {
		job.LeaseExpiresAt = previousLease
		err := errors.New("async job repository is unavailable")
		m.lastPersistErr = err
		return errors.Join(agentservice.ErrJobPersistence, err)
	}
	updated, err := m.repository.Update(context.Background(), job.Version, m.snapshot(job).asyncJobView)
	if err == nil {
		job.asyncJobView = updated
		m.lastPersistErr = nil
		return nil
	}
	if isJobRepositoryConcurrencyError(err) {
		canonical, getErr := m.repository.Get(context.Background(), job.ID)
		if getErr == nil {
			cancelFn := job.cancel
			job.asyncJobView = canonical
			if job.Status != asyncJobStatusPending && job.Status != asyncJobStatusRunning && cancelFn != nil {
				cancelFn()
				job.cancel = nil
			}
			m.lastPersistErr = nil
			return err
		}
		if errors.Is(err, agentruntime.ErrJobRepositoryNotFound) || errors.Is(getErr, agentruntime.ErrJobRepositoryNotFound) {
			// Update preparation may have renewed/cleared the in-memory lease.
			// A missing canonical row did not commit that mutation, so preserve the
			// caller's prior view until the next repository refresh removes it.
			job.LeaseExpiresAt = previousLease
			m.lastPersistErr = nil
			return agentruntime.ErrJobRepositoryNotFound
		}
		err = getErr
	}
	job.LeaseExpiresAt = previousLease
	m.lastPersistErr = err
	return errors.Join(agentservice.ErrJobPersistence, err)
}

func asyncJobLeaseDeadline(now time.Time) *time.Time {
	return agentruntime.JobLeaseDeadline(now, asyncJobLeaseTTL)
}

func cloneAsyncJobTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func (m *asyncJobManager) prepareJobLeaseForPersistLocked(job *asyncJobRecord, now time.Time) {
	if job == nil {
		return
	}
	job.asyncJobView = agentruntime.PrepareJobLeaseForPersist(job.asyncJobView, m.instanceID, now, asyncJobLeaseTTL)
}

func (m *asyncJobManager) applyExpiredJobRecoveryLocked(job *asyncJobRecord, now time.Time) {
	if job == nil {
		return
	}
	job.asyncJobView = agentruntime.RecoverExpiredJobLease(job.asyncJobView, now)
}

func (m *asyncJobManager) deleteJobsLocked(expected []agentruntime.JobVersion) error {
	if m.store != nil {
		if err := m.persistLegacySnapshotLocked(); err != nil {
			return err
		}
		m.cleanupDeletedJobEffectsLocked(expected)
		return nil
	}
	if len(expected) == 0 {
		return nil
	}
	if m.repository == nil {
		err := errors.New("async job repository is unavailable")
		m.lastPersistErr = err
		return errors.Join(agentservice.ErrJobPersistence, err)
	}
	if err := m.repository.Delete(context.Background(), expected); err != nil {
		if isJobRepositoryConcurrencyError(err) {
			m.lastPersistErr = nil
			return err
		}
		m.lastPersistErr = err
		return errors.Join(agentservice.ErrJobPersistence, err)
	}
	m.lastPersistErr = nil
	m.cleanupDeletedJobEffectsLocked(expected)
	return nil
}

// cleanupDeletedJobEffectsLocked removes protected receipts only after the
// canonical Job deletion has committed. Job and effect repositories currently
// use independent SQLite files, so a cross-database transaction is not
// possible; cleanup failures are retained in lastEffectPersistErr and retried
// by the next deletion/housekeeping pass rather than making the caller believe
// that the Job itself was not deleted.
func (m *asyncJobManager) cleanupDeletedJobEffectsLocked(expected []agentruntime.JobVersion) {
	if m == nil || m.effectRepository == nil || len(expected) == 0 {
		return
	}
	ids := make([]string, 0, len(expected))
	seen := make(map[string]struct{}, len(expected))
	for _, item := range expected {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return
	}
	if err := m.effectRepository.DeleteByJobs(context.Background(), ids); err != nil {
		m.lastEffectPersistErr = err
		if m.effectCleanupPending == nil {
			m.effectCleanupPending = make(map[string]struct{})
		}
		for _, id := range ids {
			m.effectCleanupPending[id] = struct{}{}
		}
		_ = m.persistPendingEffectCleanupLocked()
		return
	}
	m.lastEffectPersistErr = nil
	for _, id := range ids {
		delete(m.effectCleanupPending, id)
	}
	_ = m.persistPendingEffectCleanupLocked()
}

func (m *asyncJobManager) retryPendingEffectCleanupLocked() {
	if m == nil || m.effectRepository == nil || len(m.effectCleanupPending) == 0 {
		return
	}
	ids := make([]string, 0, len(m.effectCleanupPending))
	for id := range m.effectCleanupPending {
		ids = append(ids, id)
	}
	if err := m.effectRepository.DeleteByJobs(context.Background(), ids); err != nil {
		m.lastEffectPersistErr = err
		return
	}
	m.lastEffectPersistErr = nil
	for _, id := range ids {
		delete(m.effectCleanupPending, id)
	}
	_ = m.persistPendingEffectCleanupLocked()
}

// loadPendingEffectCleanup restores orphan cleanup intents across process
// restarts. The effect repository is durable and independent from the Job
// repository; without this tiny marker a shutdown between Job deletion and
// receipt cleanup would leave protected rows forever.
func (m *asyncJobManager) loadPendingEffectCleanup() {
	if m == nil || strings.TrimSpace(m.effectCleanupPath) == "" {
		return
	}
	payload, err := os.ReadFile(m.effectCleanupPath)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		m.lastEffectPersistErr = err
		return
	}
	var ids []string
	if err := json.Unmarshal(payload, &ids); err != nil {
		m.lastEffectPersistErr = err
		return
	}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" && len(id) <= 256 {
			m.effectCleanupPending[id] = struct{}{}
		}
	}
}

func (m *asyncJobManager) persistPendingEffectCleanupLocked() error {
	if m == nil || strings.TrimSpace(m.effectCleanupPath) == "" {
		return nil
	}
	ids := make([]string, 0, len(m.effectCleanupPending))
	for id := range m.effectCleanupPending {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		if err := os.Remove(m.effectCleanupPath); err != nil && !os.IsNotExist(err) {
			m.lastEffectPersistErr = err
			return err
		}
		return nil
	}
	payload, err := json.Marshal(ids)
	if err != nil {
		m.lastEffectPersistErr = err
		return err
	}
	if err := fileutil.AtomicWriteFile(m.effectCleanupPath, payload, 0o600); err != nil {
		m.lastEffectPersistErr = err
		return err
	}
	return nil
}

func (m *asyncJobManager) persistLegacySnapshotLocked() error {
	if m.store == nil {
		return nil
	}
	items := make([]agentruntime.Job, 0, len(m.jobs))
	for _, record := range m.jobs {
		items = append(items, m.snapshot(record).asyncJobView)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	if err := m.store.Replace(context.Background(), items); err != nil {
		m.lastPersistErr = err
		return errors.Join(agentservice.ErrJobPersistence, err)
	}
	m.lastPersistErr = nil
	return nil
}

func isJobRepositoryConcurrencyError(err error) bool {
	return agentruntime.IsJobRepositoryConcurrencyError(err)
}

func (m *asyncJobManager) mergeRepositoryJobLocked(item agentruntime.Job) *asyncJobRecord {
	item.Status = agentruntime.NormalizeJobStatus(item.Status)
	item.RecoveryPolicy = agentruntime.NormalizeJobRecoveryPolicy(item.RecoveryPolicy)
	item.RetryPolicy = agentruntime.NormalizeJobRetryPolicy(item.RetryPolicy)
	if existing := m.jobs[item.ID]; existing != nil {
		cancelFn := existing.cancel
		existing.asyncJobView = cloneAsyncJobEnvelope(item)
		if existing.Status != asyncJobStatusPending && existing.Status != asyncJobStatusRunning && cancelFn != nil {
			cancelFn()
			existing.cancel = nil
		}
		return existing
	}
	record := &asyncJobRecord{asyncJobView: cloneAsyncJobEnvelope(item)}
	m.jobs[item.ID] = record
	return record
}

func (m *asyncJobManager) refreshFromRepositoryLocked() {
	if m.closed {
		return
	}
	var (
		items []agentruntime.Job
		err   error
	)
	if m.store != nil {
		items, err = m.store.Load(context.Background())
		for i := range items {
			if items[i].Version == 0 {
				items[i].Version = 1
			}
		}
	} else if m.repository != nil {
		items, err = m.repository.List(context.Background())
	} else {
		return
	}
	if err != nil {
		m.lastPersistErr = err
		return
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		seen[item.ID] = struct{}{}
		m.mergeRepositoryJobLocked(item)
	}
	for id, record := range m.jobs {
		if record.Version == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		if record.cancel != nil {
			record.cancel()
		}
		delete(m.jobs, id)
	}
	if m.store == nil {
		m.reconcileExpiredJobsLocked(time.Now().UTC())
	}
}

// reconcileExpiredJobsLocked is the observation-driven recovery path. Any
// API read/metrics refresh can settle work whose owner disappeared, while the
// active-owner coordinator below renews leases without requiring request
// traffic.
func (m *asyncJobManager) reconcileExpiredJobsLocked(now time.Time) {
	items := make([]agentruntime.Job, 0, len(m.jobs))
	for _, job := range m.jobs {
		items = append(items, job.asyncJobView)
	}
	instanceID := m.instanceID
	recovered := agentruntime.RecoverExpiredJobs(items, now, func(job agentruntime.Job) bool {
		record := m.jobs[job.ID]
		return record != nil && record.cancel != nil && record.LeaseOwnerID == instanceID
	})
	for _, next := range recovered {
		job := m.jobs[next.ID]
		if job == nil {
			continue
		}
		old := cloneAsyncJobEnvelope(job.asyncJobView)
		job.asyncJobView = next
		if err := m.persistJobLocked(job); err != nil && !isJobRepositoryConcurrencyError(err) {
			job.asyncJobView = old
		}
	}
}

func (m *asyncJobManager) ensureLeaseCoordinatorLocked() {
	if m == nil || m.closed || m.store != nil || m.repository == nil || m.leaseRunning || !m.hasLocalActiveJobsLocked() {
		return
	}
	m.leaseRunning = true
	m.leaseWG.Add(1)
	go m.runLeaseCoordinator()
}

func (m *asyncJobManager) signalLeaseCoordinatorLocked() {
	if m == nil || !m.leaseRunning {
		return
	}
	select {
	case m.leaseWake <- struct{}{}:
	default:
	}
}

func (m *asyncJobManager) hasLocalActiveJobsLocked() bool {
	for _, job := range m.jobs {
		if agentruntime.ShouldRenewLocalJobLease(job.asyncJobView, m.instanceID, job.cancel != nil) {
			return true
		}
	}
	return false
}

func (m *asyncJobManager) runLeaseCoordinator() {
	defer m.leaseWG.Done()
	ticker := time.NewTicker(asyncJobLeaseTick)
	defer ticker.Stop()
	for {
		select {
		case <-m.leaseStop:
			return
		case <-m.leaseWake:
		case <-ticker.C:
		}
		if !m.renewLocalJobLeases() {
			return
		}
	}
}

func (m *asyncJobManager) renewLocalJobLeases() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.repository == nil || m.store != nil {
		m.leaseRunning = false
		return false
	}
	m.refreshFromRepositoryLocked()
	now := time.Now().UTC()
	for _, job := range m.jobs {
		if !agentruntime.ShouldRenewLocalJobLease(job.asyncJobView, m.instanceID, job.cancel != nil) {
			continue
		}
		if err := m.persistJobLocked(job); err != nil {
			if !isJobRepositoryConcurrencyError(err) {
				cancelFn := job.cancel
				job.cancel = nil
				m.markPersistenceFailureLocked(job, err, now)
				if cancelFn != nil {
					cancelFn()
				}
			}
		}
	}
	active := m.hasLocalActiveJobsLocked()
	if !active {
		m.leaseRunning = false
	}
	return active
}

func (m *asyncJobManager) persistenceError() error {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.lastPersistErr == nil && m.lastEffectPersistErr == nil {
		return nil
	}
	return errors.Join(agentservice.ErrJobPersistence, m.lastPersistErr, m.lastEffectPersistErr)
}

func (m *asyncJobManager) persistenceHealthy() bool {
	if m == nil {
		return true
	}
	m.mu.RLock()
	repository := m.repository
	effectRepository := m.effectRepository
	closed := m.closed
	m.mu.RUnlock()
	if closed {
		return true
	}
	if m.persistenceError() != nil || repository == nil || effectRepository == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return repository.Probe(ctx) == nil && effectRepository.Probe(ctx) == nil
}

// close stops admission and requests cancellation of every active job. The
// manager is owned by the HTTP composition root, so shutdown must not leave
// workers running against a service whose stores are already closing.
func (m *asyncJobManager) close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	leaseStop := m.leaseStop
	m.mu.Unlock()
	if leaseStop != nil {
		close(leaseStop)
	}
	m.leaseWG.Wait()

	m.mu.Lock()
	now := time.Now().UTC()
	for _, job := range m.jobs {
		if job.Status != asyncJobStatusPending && job.Status != asyncJobStatusRunning {
			continue
		}
		// A refreshed/replayed record may be executing in another process and
		// therefore has no local cancellation function. Closing this host must
		// never publish a cancellation for work it does not own.
		if job.cancel == nil {
			continue
		}
		job.cancel()
		job.cancel = nil
		job.asyncJobView = agentruntime.StampJobCanceled(job.asyncJobView, now, "job canceled during service shutdown")
		_ = m.persistJobLocked(job)
	}
	repository := m.repository
	effectRepository := m.effectRepository
	m.mu.Unlock()
	if repository != nil {
		_ = repository.Close()
	}
	if effectRepository != nil {
		_ = effectRepository.Close()
	}
}

func (m *asyncJobManager) snapshot(job *asyncJobRecord) *asyncJobRecord {
	if job == nil {
		return nil
	}
	copy := *job
	copy.cancel = nil
	copy.Error = redactAsyncJobText(m.dataRoot, copy.Error)
	copy.LastAttemptError = redactAsyncJobText(m.dataRoot, copy.LastAttemptError)
	copy.Checkpoint = cloneJobCheckpoint(job.Checkpoint)
	copy.LeaseExpiresAt = cloneAsyncJobTime(job.LeaseExpiresAt)
	if job.Result != nil {
		copy.Result = redactAsyncJobRawMessage(m.dataRoot, append(json.RawMessage(nil), job.Result...))
	}
	return &copy
}

func cloneJobCheckpoint(checkpoint *agentruntime.JobCheckpoint) *agentruntime.JobCheckpoint {
	if checkpoint == nil {
		return nil
	}
	copy := *checkpoint
	return &copy
}

func redactAsyncJobText(dataRoot, text string) string {
	return redactSupportBundleText(dataRoot, stringsTrim(text))
}

func redactAsyncJobRawMessage(dataRoot string, payload json.RawMessage) json.RawMessage {
	if len(payload) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		redacted := []byte(redactSupportBundleText(dataRoot, string(payload)))
		if !json.Valid(redacted) {
			return json.RawMessage(`{"redacted":true}`)
		}
		return json.RawMessage(redacted)
	}
	redacted, err := json.Marshal(redactAsyncJobValue(dataRoot, "", value))
	if err != nil || !json.Valid(redacted) {
		return json.RawMessage(`{"redacted":true}`)
	}
	return json.RawMessage(redacted)
}

func redactAsyncJobValue(dataRoot, key string, value any) any {
	switch v := value.(type) {
	case string:
		if supportBundleSensitiveKey(key) {
			return "[redacted]"
		}
		if asyncJobPathLikeKey(key) || supportBundleLooksAbsolutePath(v) {
			return redactSupportBundleValue(dataRoot, v)
		}
		if asyncJobEndpointLikeKey(key) || strings.Contains(v, "://") {
			return redactEndpointForAPI(dataRoot, v)
		}
		return redactSupportBundleText(dataRoot, v)
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = redactAsyncJobValue(dataRoot, key, v[i])
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for childKey, childValue := range v {
			out[childKey] = redactAsyncJobValue(dataRoot, childKey, childValue)
		}
		return out
	default:
		return value
	}
}

func asyncJobPathLikeKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return false
	}
	switch key {
	case "path", "root_path", "file_path", "current_file", "relative_path", "data_dir", "runtime_dir", "workspace", "workspace_dir", "skill_dir", "project_path":
		return true
	}
	return strings.HasSuffix(key, "_path") || strings.HasSuffix(key, "_dir")
}

func asyncJobEndpointLikeKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return false
	}
	switch key {
	case "url", "uri", "endpoint", "endpoint_url", "base_url", "raw_url", "repo_url", "canonical_uri":
		return true
	}
	return strings.HasSuffix(key, "_url") || strings.HasSuffix(key, "_uri") || strings.HasSuffix(key, "_endpoint")
}

func stringsTrim(v string) string {
	for len(v) > 0 && (v[0] == ' ' || v[0] == '\t' || v[0] == '\n' || v[0] == '\r') {
		v = v[1:]
	}
	for len(v) > 0 {
		last := v[len(v)-1]
		if last != ' ' && last != '\t' && last != '\n' && last != '\r' {
			break
		}
		v = v[:len(v)-1]
	}
	return v
}

// adminJobListFilter scopes admin-wide async job listing.
type adminJobListFilter struct {
	Kind     string
	Status   asyncJobStatus
	TenantID string
	UserID   string
	Limit    int
}

func (m *asyncJobManager) listAllJobs(filter adminJobListFilter) []asyncJobRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshFromRepositoryLocked()
	m.pruneLocked(time.Now().UTC())
	kind := stringsTrim(filter.Kind)
	tenantID := stringsTrim(filter.TenantID)
	userID := stringsTrim(filter.UserID)
	items := make([]asyncJobRecord, 0, len(m.jobs))
	for _, job := range m.jobs {
		if kind != "" && job.Kind != kind {
			continue
		}
		if filter.Status != "" && job.Status != filter.Status {
			continue
		}
		if tenantID != "" && job.TenantID != tenantID {
			continue
		}
		if userID != "" && job.UserID != userID {
			continue
		}
		items = append(items, *m.snapshot(job))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	if filter.Limit > 0 && len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	return items
}

func (m *asyncJobManager) getAnyJob(jobID string) (*asyncJobRecord, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshFromRepositoryLocked()
	m.pruneLocked(time.Now().UTC())
	job, ok := m.jobs[jobID]
	if !ok {
		return nil, false
	}
	return m.snapshot(job), true
}

func (m *asyncJobManager) cancelAnyJob(jobID string) (*asyncJobRecord, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshFromRepositoryLocked()
	m.pruneLocked(time.Now().UTC())
	job, ok := m.jobs[jobID]
	if !ok {
		return nil, false
	}
	if job.Status == asyncJobStatusPending || job.Status == asyncJobStatusRunning {
		old := cloneAsyncJobEnvelope(job.asyncJobView)
		job.asyncJobView = agentruntime.StampJobCanceled(job.asyncJobView, time.Now().UTC(), "")
		if err := m.persistJobLocked(job); err != nil {
			if !isJobRepositoryConcurrencyError(err) {
				job.asyncJobView = old
			}
			return m.snapshot(job), true
		}
		if job.cancel != nil {
			job.cancel()
		}
		job.cancel = nil
		m.signalLeaseCoordinatorLocked()
	}
	return m.snapshot(job), true
}
