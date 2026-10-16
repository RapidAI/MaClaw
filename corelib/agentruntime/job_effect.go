package agentruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// JobEffectState is the protected domain-effect lifecycle associated with a
// durable Job. It is deliberately separate from JobStatus: a worker can die
// while the external effect has already committed, and only domain evidence
// may close that uncertainty.
type JobEffectState string

const (
	JobEffectPrepared  JobEffectState = "prepared"
	JobEffectCommitted JobEffectState = "committed"
	JobEffectFailed    JobEffectState = "failed"
	JobEffectUnknown   JobEffectState = "unknown"
)

var (
	ErrJobEffectUnavailable = errors.New("job effect recorder is unavailable")
	ErrJobEffectNotFound    = errors.New("job effect record not found")
	ErrJobEffectConflict    = errors.New("job effect record conflicts with durable state")
	ErrInvalidJobEffect     = errors.New("invalid job effect record")
	// ErrJobEffectOutcomeUncertain means external I/O may have happened after
	// the protected effect record could no longer be advanced. Job hosts must
	// project this as unknown/reconcile_required, never as a retryable failure.
	ErrJobEffectOutcomeUncertain = errors.New("job effect outcome is uncertain")
)

// JobEffect is protected reconciliation data. It must never be embedded in a
// transport Job or returned by a generic Job API: ResourceID and Payload can
// contain provider identifiers or other domain-private correlation material.
// A repository owns one record for each (JobID, Kind) pair.
type JobEffect struct {
	Version       uint64
	JobID         string
	TenantID      string
	UserID        string
	JobKind       string
	Kind          string
	ResourceID    string
	State         JobEffectState
	ReceiptDigest string
	ReasonCode    string
	Payload       json.RawMessage
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// JobEffectRepository is the multi-writer protected repository used by GUI,
// srv and future hosts. Prepare is append-once/idempotent for a Job effect;
// Update uses Version CAS and may bind a previously empty ResourceID or settle
// the effect, but it must not change its Job/principal/kind identity.
type JobEffectRepository interface {
	ListByJob(context.Context, string) ([]JobEffect, error)
	Get(context.Context, string, string) (JobEffect, error)
	Prepare(context.Context, JobEffect) (canonical JobEffect, created bool, err error)
	Update(context.Context, uint64, JobEffect) (JobEffect, error)
	DeleteByJobs(context.Context, []string) error
	Probe(context.Context) error
	Close() error
}

type JobEffectPreparation struct {
	Kind    string
	Payload json.RawMessage
}

type JobEffectBinding struct {
	Kind       string
	ResourceID string
}

type JobEffectSettlement struct {
	Kind          string
	State         JobEffectState
	ReceiptDigest string
	ReasonCode    string
	Payload       json.RawMessage
}

// JobEffectRecorder is injected into a worker context by its host. Domain code
// can persist effect identity before provider I/O, bind a returned resource,
// and settle trusted evidence without importing GUI or HTTP packages.
type JobEffectRecorder interface {
	PrepareJobEffect(context.Context, JobEffectPreparation) (JobEffect, error)
	BindJobEffect(context.Context, JobEffectBinding) (JobEffect, error)
	SettleJobEffect(context.Context, JobEffectSettlement) (JobEffect, error)
}

type jobEffectRecorderContextKey struct{}

func WithJobEffectRecorder(ctx context.Context, recorder JobEffectRecorder) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if recorder == nil {
		return ctx
	}
	return context.WithValue(ctx, jobEffectRecorderContextKey{}, recorder)
}

func JobEffectRecorderAvailable(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	_, ok := ctx.Value(jobEffectRecorderContextKey{}).(JobEffectRecorder)
	return ok
}

func PrepareJobEffect(ctx context.Context, kind string, payload any) (JobEffect, error) {
	recorder, err := jobEffectRecorderFromContext(ctx)
	if err != nil {
		return JobEffect{}, err
	}
	raw, err := marshalJobEffectPayload(payload)
	if err != nil {
		return JobEffect{}, err
	}
	return recorder.PrepareJobEffect(ctx, JobEffectPreparation{Kind: kind, Payload: raw})
}

func BindJobEffectResource(ctx context.Context, kind, resourceID string) (JobEffect, error) {
	if strings.TrimSpace(resourceID) == "" {
		return JobEffect{}, fmt.Errorf("%w: resource id is required", ErrInvalidJobEffect)
	}
	recorder, err := jobEffectRecorderFromContext(ctx)
	if err != nil {
		return JobEffect{}, err
	}
	return recorder.BindJobEffect(ctx, JobEffectBinding{Kind: kind, ResourceID: resourceID})
}

func SettleJobEffect(ctx context.Context, kind string, state JobEffectState, receipt, reasonCode string, payload any) (JobEffect, error) {
	if state != JobEffectCommitted && state != JobEffectFailed && state != JobEffectUnknown {
		return JobEffect{}, fmt.Errorf("%w: settlement state is invalid", ErrInvalidJobEffect)
	}
	recorder, err := jobEffectRecorderFromContext(ctx)
	if err != nil {
		return JobEffect{}, err
	}
	raw, err := marshalJobEffectPayload(payload)
	if err != nil {
		return JobEffect{}, err
	}
	digest := ""
	if strings.TrimSpace(receipt) != "" {
		digest = JobEffectReceiptDigest(receipt)
	}
	return recorder.SettleJobEffect(ctx, JobEffectSettlement{Kind: kind, State: state, ReceiptDigest: digest, ReasonCode: reasonCode, Payload: raw})
}

func jobEffectRecorderFromContext(ctx context.Context) (JobEffectRecorder, error) {
	if ctx == nil {
		return nil, ErrJobEffectUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	recorder, _ := ctx.Value(jobEffectRecorderContextKey{}).(JobEffectRecorder)
	if recorder == nil {
		return nil, ErrJobEffectUnavailable
	}
	return recorder, nil
}

func marshalJobEffectPayload(payload any) (json.RawMessage, error) {
	if payload == nil {
		return nil, nil
	}
	if raw, ok := payload.(json.RawMessage); ok {
		if !json.Valid(raw) {
			return nil, fmt.Errorf("%w: payload is not valid JSON", ErrInvalidJobEffect)
		}
		return append(json.RawMessage(nil), raw...), nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: encode payload: %v", ErrInvalidJobEffect, err)
	}
	return raw, nil
}

func JobEffectReceiptDigest(receipt string) string {
	sum := sha256.Sum256([]byte(receipt))
	return hex.EncodeToString(sum[:])
}

func ValidateJobEffect(effect JobEffect) error {
	if effect.Version == 0 || strings.TrimSpace(effect.JobID) == "" || strings.TrimSpace(effect.TenantID) == "" || strings.TrimSpace(effect.UserID) == "" || strings.TrimSpace(effect.JobKind) == "" {
		return fmt.Errorf("%w: version and job scope are required", ErrInvalidJobEffect)
	}
	if err := validateJobEffectToken(effect.Kind, "kind"); err != nil {
		return err
	}
	if effect.ResourceID != strings.TrimSpace(effect.ResourceID) || len(effect.ResourceID) > 512 || containsJobEffectControl(effect.ResourceID) {
		return fmt.Errorf("%w: resource id is invalid", ErrInvalidJobEffect)
	}
	switch effect.State {
	case JobEffectPrepared, JobEffectCommitted, JobEffectFailed, JobEffectUnknown:
	default:
		return fmt.Errorf("%w: state is invalid", ErrInvalidJobEffect)
	}
	if effect.State == JobEffectCommitted && !validSHA256Hex(effect.ReceiptDigest) {
		return fmt.Errorf("%w: committed effect requires a receipt digest", ErrInvalidJobEffect)
	}
	if effect.ReceiptDigest != "" && !validSHA256Hex(effect.ReceiptDigest) {
		return fmt.Errorf("%w: receipt digest is invalid", ErrInvalidJobEffect)
	}
	if effect.ReasonCode != "" {
		if err := validateJobEffectToken(effect.ReasonCode, "reason code"); err != nil {
			return err
		}
	}
	if len(effect.Payload) > 256*1024 || (len(effect.Payload) > 0 && !json.Valid(effect.Payload)) {
		return fmt.Errorf("%w: payload is invalid or too large", ErrInvalidJobEffect)
	}
	if effect.CreatedAt.IsZero() || effect.UpdatedAt.IsZero() || effect.UpdatedAt.Before(effect.CreatedAt) {
		return fmt.Errorf("%w: timestamps are invalid", ErrInvalidJobEffect)
	}
	return nil
}

func validateJobEffectToken(value, name string) error {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 {
		return fmt.Errorf("%w: %s must contain 1-64 safe characters", ErrInvalidJobEffect, name)
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-' {
			continue
		}
		return fmt.Errorf("%w: %s must contain 1-64 safe characters", ErrInvalidJobEffect, name)
	}
	return nil
}

func containsJobEffectControl(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// JobReconcileResult is a domain handler's evidence-backed projection for an
// unknown Job. Resolved=false leaves the Job unknown. A resolved handler may
// only settle succeeded or failed; it never returns an executable state and
// therefore cannot authorize worker replay.
type JobReconcileResult struct {
	Resolved  bool
	Status    JobStatus
	Result    json.RawMessage
	ErrorCode string
	Error     string
}

type JobReconciler interface {
	ReconcileJob(context.Context, Job, []JobEffect) (JobReconcileResult, error)
}

func ValidateJobReconcileResult(result JobReconcileResult) error {
	if !result.Resolved {
		if result.Status != "" || len(result.Result) != 0 || result.ErrorCode != "" || result.Error != "" {
			return fmt.Errorf("%w: unresolved reconciliation must not carry an outcome", ErrInvalidJobEffect)
		}
		return nil
	}
	if result.Status != JobStatusSucceeded && result.Status != JobStatusFailed {
		return fmt.Errorf("%w: reconciliation may only settle succeeded or failed", ErrInvalidJobEffect)
	}
	if len(result.Result) > 0 && !json.Valid(result.Result) {
		return fmt.Errorf("%w: reconciliation result is not valid JSON", ErrInvalidJobEffect)
	}
	if result.Status == JobStatusSucceeded && (result.ErrorCode != "" || result.Error != "") {
		return fmt.Errorf("%w: successful reconciliation cannot carry an error", ErrInvalidJobEffect)
	}
	if result.Status == JobStatusFailed && strings.TrimSpace(result.ErrorCode) == "" {
		return fmt.Errorf("%w: failed reconciliation requires an error code", ErrInvalidJobEffect)
	}
	return nil
}

// ReconcileFromProtectedEffects settles a Job from committed or failed
// protected effects. Domain reconcilers call this first so they cannot drift
// on receipt-backed success/failure; a sanitize hook lets srv redact payloads
// while GUI keeps the stored JSON.
func ReconcileFromProtectedEffects(effects []JobEffect, effectKind, failedError string, sanitize func(json.RawMessage) json.RawMessage) JobReconcileResult {
	effectKind = strings.TrimSpace(effectKind)
	if failedError == "" {
		failedError = "effect failed"
	}
	for _, effect := range effects {
		if strings.TrimSpace(effect.Kind) != effectKind {
			continue
		}
		if effect.State == JobEffectCommitted && len(effect.Payload) > 0 && json.Valid(effect.Payload) {
			payload := append(json.RawMessage(nil), effect.Payload...)
			if sanitize != nil {
				payload = sanitize(payload)
				if len(payload) == 0 {
					continue
				}
			}
			return JobReconcileResult{Resolved: true, Status: JobStatusSucceeded, Result: payload}
		}
		if effect.State == JobEffectFailed {
			code := strings.TrimSpace(effect.ReasonCode)
			if code == "" {
				code = "effect_failed"
			}
			return JobReconcileResult{Resolved: true, Status: JobStatusFailed, ErrorCode: code, Error: failedError}
		}
	}
	return JobReconcileResult{}
}

// JobEffectOperation reads the optional "operation" field from an effect payload.
func JobEffectOperation(effect JobEffect) string {
	var payload struct {
		Operation string `json:"operation"`
	}
	if json.Unmarshal(effect.Payload, &payload) == nil {
		return strings.ToLower(strings.TrimSpace(payload.Operation))
	}
	return ""
}

// ParseJobEffectResourceIDs splits a resource id that is either a single
// token or a JSON string array.
func ParseJobEffectResourceIDs(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var ids []string
	if strings.HasPrefix(value, "[") && json.Unmarshal([]byte(value), &ids) == nil {
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			if id = strings.TrimSpace(id); id != "" {
				out = append(out, id)
			}
		}
		return out
	}
	return []string{value}
}

// JobEffectMatchesJob reports that a protected effect belongs to this Job's
// identity. Reconciliation must not settle a Job from another tenant/kind.
func JobEffectMatchesJob(effect JobEffect, job Job) bool {
	return effect.JobID == job.ID && effect.TenantID == job.TenantID && effect.UserID == job.UserID && effect.JobKind == job.Kind
}

// ApplyJobReconcileResult copies a resolved domain projection onto a Job
// envelope. Hosts that also clear leases or redact payloads do that after.
func ApplyJobReconcileResult(job Job, result JobReconcileResult) Job {
	job.Status = result.Status
	job.ErrorCode = result.ErrorCode
	job.Error = result.Error
	job.Result = append(json.RawMessage(nil), result.Result...)
	return job
}

// ApplyResolvedJobReconcile settles a resolved Job: copies the domain
// projection, stamps CompletedAt, and drops retry/lease metadata. GUI scan
// and srv unknown-job CAS both finish this way before persist.
func ApplyResolvedJobReconcile(job Job, result JobReconcileResult, now time.Time) Job {
	job = ApplyJobReconcileResult(job, result)
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	completed := now
	job.CompletedAt = &completed
	job.NextAttemptAt = nil
	job.LeaseExpiresAt = nil
	return job
}

// RecoverAndReconcileOpenJobs is the start-of-process crash-recovery entry:
// expire abandoned leases, settle remaining non-terminal Jobs from protected
// effects, then prune Jobs past the shared retention window. It never replays
// a worker. Pass skip to hold back Jobs this process still owns; GUI
// start-of-process uses nil.
func RecoverAndReconcileOpenJobs(ctx context.Context, jobs JobRepository, effects JobEffectRepository, kind string, reconciler JobReconciler, skip func(Job) bool) error {
	if err := RecoverExpiredJobsInRepository(ctx, jobs, time.Now().UTC(), skip); err != nil {
		return err
	}
	if err := ReconcileOpenJobs(ctx, jobs, effects, kind, reconciler); err != nil {
		return err
	}
	return PruneRetainedJobsInRepository(ctx, jobs, effects, time.Now().UTC(), DefaultJobRetention, DefaultJobMaxCount)
}

// ReconcileOpenJobs walks a repository, asks a read-only reconciler to settle
// non-terminal jobs of one kind, and upserts resolved outcomes. GUI skill
// runner and task orchestrator both start this way; it never replays a worker.
func ReconcileOpenJobs(ctx context.Context, jobs JobRepository, effects JobEffectRepository, kind string, reconciler JobReconciler) error {
	if jobs == nil || reconciler == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	items, err := jobs.List(ctx)
	if err != nil {
		return err
	}
	kind = strings.TrimSpace(kind)
	for _, job := range items {
		if kind != "" && strings.TrimSpace(job.Kind) != kind {
			continue
		}
		if JobStatusIsTerminal(job.Status) {
			continue
		}
		var listed []JobEffect
		if effects != nil {
			listed, err = effects.ListByJob(ctx, job.ID)
			if err != nil {
				return err
			}
			mismatch := false
			for _, effect := range listed {
				if !JobEffectMatchesJob(effect, job) {
					mismatch = true
					break
				}
			}
			if mismatch {
				continue
			}
		}
		result, err := reconciler.ReconcileJob(ctx, job, listed)
		if err != nil || !result.Resolved {
			continue
		}
		if err := ValidateJobReconcileResult(result); err != nil {
			continue
		}
		if err := UpsertJob(ctx, jobs, ApplyResolvedJobReconcile(job, result, time.Now().UTC())); err != nil {
			return err
		}
	}
	return nil
}
