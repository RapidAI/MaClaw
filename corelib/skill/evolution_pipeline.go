package skill

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// EvolutionPipeline is the async background worker that ties together all
// self-evolution components: NudgePromoter, SkillOptimizer, RepairGate,
// MaintenanceScheduler, and AutoUpload trigger.
//
// It runs completely independently of the main agent loop. The main agent
// only feeds data via RecordExperience (< 1ms). All verification, optimization,
// promotion, and upload happen asynchronously with a configurable delay after
// skill execution completes.
type EvolutionPipeline struct {
	// Components
	Promoter     *NudgePromoter
	Optimizer    *SkillOptimizer
	Gate         *RepairGate
	Scheduler    *MaintenanceScheduler
	Versioner    *Versioner
	UsageTracker *tool.UsageTracker
	SkillLoader  func() []corelib.NLSkillEntry
	SkillSaver   func([]corelib.NLSkillEntry) error
	// DefinitionWriter writes the authoritative YAML definition. Nil selects
	// the built-in atomic writer; injection keeps compensation tests isolated.
	DefinitionWriter func(*corelib.NLSkillEntry) error
	// IndexRefresher rebuilds derived routing/index state after a durable
	// definition change. Returning an error makes index publication part of
	// the same compensation boundary as config and YAML.
	IndexRefresher func() error
	// FinalAuditor is invoked inside the definition commit transaction, after
	// config/YAML/index publication but before the caller emits success. A
	// non-nil error triggers compensation; this keeps audit evidence atomic
	// with the mutation rather than best-effort after the fact.
	FinalAuditor func(event string, data map[string]string) error
	// ExternalRecovery restores caller-owned external pre-images (for example
	// dynamic capability contracts) before filesystem/config rollback during
	// cross-process compensation recovery. It is intentionally optional so
	// legacy records remain compatible; services that mutate external state must
	// provide it and persist the corresponding snapshot in the record.
	ExternalRecovery func(EvolutionCompensationRecord) error
	UploadTrigger    func(skillName string, result *SkillExecutionResultCompat) // enqueues upload via SkillLifecycleManager (subject to skill_auto_upload_enabled)
	LLM              LLMRepairer
	EventEmitter     func(event string, data map[string]string) // notifies frontend of evolution actions

	// Config
	PostExecDelay        time.Duration // delay after skill exec before running pipeline (default 5s)
	EnablePromoter       bool
	EnableOptimizer      bool
	EnableRepair         bool          // schedule self-repair for failed skills (default true)
	RepairCooldown       time.Duration // min interval between LLM repair attempts per skill (default 1h)
	PromoteCheckInterval int           // check nudge promotion every N notifications (default 10)
	MaxConcurrentWorkers int           // reserved worker limit; 1 preserves per-skill ordering
	WorkerTimeout        time.Duration // max duration for one evolution request
	// ConfigRevision identifies the configuration snapshot used by workers.
	ConfigRevision string

	// RepairHook is the platform-specific repair applier (LLM + gate + security +
	// persist). When set, processRequest delegates failed-skill repair here so
	// GUI can enforce security scans. When nil, a core-only repair path runs
	// (ApplyRepair + SkillSaver) suitable for headless/tests.
	RepairHook func(entry *corelib.NLSkillEntry, runArgs map[string]string)
	// RepairHookWithContext is the cancellation-aware variant. When set it is
	// preferred over RepairHook; the legacy field remains for compatibility.
	RepairHookWithContext func(context.Context, *corelib.NLSkillEntry, map[string]string)

	// Internal
	// pendingBySkill coalesces notifications by skill name: rapid re-runs of the
	// same skill keep only the latest request instead of dropping when busy.
	pendingBySkill         map[string]evolutionRequest
	pendingMu              sync.Mutex
	pendingWake            chan struct{}   // buffer 1 — wake the loop without blocking Notify
	activeSkills           map[string]bool // protects same-skill ordering across workers
	requestStatuses        map[string]EvolutionRequestStatus
	stopCh                 chan struct{}
	once                   sync.Once    // protects stopCh close
	startOnce              sync.Once    // protects Start from being called twice
	requestCount           atomic.Int64 // counts processed requests for throttling
	lastFailureMu          sync.RWMutex
	lastFailures           map[string]EvolutionFailureSummary
	cancelMu               sync.Mutex
	cancelBySkill          map[string]context.CancelFunc
	requestIDBySkill       map[string]string
	shutdownCtx            context.Context
	shutdownCancel         context.CancelFunc
	compensationRecoveryMu sync.Mutex // serializes startup/manual durable recovery
	cancelledCount         atomic.Uint64
	timedOutCount          atomic.Uint64

	// CoalescedNotifications counts NotifySkillExecution calls that replaced a
	// still-pending request for the same skill (not lost — superseded).
	CoalescedNotifications atomic.Uint64
	// DroppedNotifications is kept for backwards-compatible metrics; with the
	// coalesce map it stays 0 under normal operation (never hard-drops).
	DroppedNotifications atomic.Uint64

	// LLM call throttling — prevents repeated expensive calls for the same skill/candidate.
	optimizeAttempts map[string]time.Time // skill name → last LLM optimization attempt time
	repairAttempts   map[string]time.Time // skill name → last self-repair attempt time
	promoteBlacklist map[string]time.Time // candidate ContextKey → time blocked (retry after 24h)
	throttleMu       sync.Mutex
}

// DefaultEvolutionWorkerTimeout bounds one background evolution request.
// Keeping this as a named policy constant makes timeout behavior testable and
// prevents individual callers from silently choosing different deadlines.
const DefaultEvolutionWorkerTimeout = 3 * time.Minute

// SkillExecutionResultCompat is a bridge type to avoid importing gui package.
type SkillExecutionResultCompat struct {
	Success       bool
	OutputQuality string
	TokensUsed    int
	ErrorClass    string
	Error         string
}

type evolutionRequest struct {
	RequestID  string
	SkillName  string
	Entry      *corelib.NLSkillEntry
	ExecResult *SkillExecutionResultCompat
	RunArgs    map[string]string
	EnqueuedAt time.Time
}

// evolutionContextKey carries request-scoped correlation metadata into
// platform hooks. Keeping this in the core package lets GUI/TUI adapters use
// the same request_id without changing the legacy hook signature.
type evolutionContextKey string

const (
	evolutionRequestIDContextKey evolutionContextKey = "maclaw.evolution.request_id"
	evolutionAttemptContextKey   evolutionContextKey = "maclaw.evolution.attempt"
)

// WithEvolutionRequestMetadata annotates a worker context with the stable
// request correlation fields used by downstream commit/audit adapters.
func WithEvolutionRequestMetadata(ctx context.Context, requestID string, attempt int) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, evolutionRequestIDContextKey, strings.TrimSpace(requestID))
	return context.WithValue(ctx, evolutionAttemptContextKey, attempt)
}

// EvolutionRequestMetadata extracts request correlation fields from a worker
// context. Empty values mean the caller is outside an evolution worker.
func EvolutionRequestMetadata(ctx context.Context) (requestID string, attempt int) {
	if ctx == nil {
		return "", 0
	}
	if v, ok := ctx.Value(evolutionRequestIDContextKey).(string); ok {
		requestID = strings.TrimSpace(v)
	}
	if v, ok := ctx.Value(evolutionAttemptContextKey).(int); ok {
		attempt = v
	}
	return requestID, attempt
}

// EvolutionRequestStatus is a bounded request-level view for operators. It
// deliberately excludes arguments, prompts and model output.
type EvolutionRequestStatus struct {
	RequestID  string `json:"request_id"`
	Skill      string `json:"skill"`
	State      string `json:"state"` // pending | running
	EnqueuedAt string `json:"enqueued_at,omitempty"`
	StartedAt  string `json:"started_at,omitempty"`
}

var evolutionRequestSequence atomic.Uint64

// newEvolutionRequestID creates a process-unique correlation id for audit
// records. The sequence prevents collisions when notifications share a tick.
func newEvolutionRequestID() string {
	seq := evolutionRequestSequence.Add(1)
	return fmt.Sprintf("evo_%s_%d", time.Now().UTC().Format("20060102T150405.000000000Z"), seq)
}

// EvolutionFailureSummary is a bounded, redacted failure snapshot exposed to
// operators. Argument values are never stored; only a digest is retained.
type EvolutionFailureSummary struct {
	Skill          string `json:"skill"`
	FailureCount   uint64 `json:"failure_count"`
	LastError      string `json:"last_error,omitempty"`
	LastErrorClass string `json:"last_error_class,omitempty"`
	LastArgsDigest string `json:"last_args_digest,omitempty"`
	LastFailureAt  string `json:"last_failure_at,omitempty"`
}

// RepairDraftResult describes a reviewed repair-draft attempt.  A successful
// attempt only creates a draft; applying it remains an explicit user action.
type RepairDraftResult struct {
	Created        bool
	Draft          string
	SkipReason     string
	Explanation    string
	RequiresReview bool
}

// DefaultRepairCooldown is used when RepairCooldown is unset and when AppConfig
// SkillEvolutionRepairCooldownHours is 0.
const DefaultRepairCooldown = time.Hour

// RepairCooldownFromHours converts a config hour count to a duration.
// hours <= 0 returns DefaultRepairCooldown.
func RepairCooldownFromHours(hours int) time.Duration {
	if hours <= 0 {
		return DefaultRepairCooldown
	}
	if hours > 24*30 {
		hours = 24 * 30 // cap at 30 days
	}
	return time.Duration(hours) * time.Hour
}

// NewEvolutionPipeline creates a pipeline with sensible defaults.
func NewEvolutionPipeline() *EvolutionPipeline {
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	return &EvolutionPipeline{
		PostExecDelay:        5 * time.Second,
		EnablePromoter:       true,
		EnableOptimizer:      true,
		EnableRepair:         true,
		RepairCooldown:       DefaultRepairCooldown,
		PromoteCheckInterval: 10,
		MaxConcurrentWorkers: 2,
		WorkerTimeout:        DefaultEvolutionWorkerTimeout,
		pendingBySkill:       make(map[string]evolutionRequest),
		pendingWake:          make(chan struct{}, 1),
		activeSkills:         make(map[string]bool),
		requestStatuses:      make(map[string]EvolutionRequestStatus),
		stopCh:               make(chan struct{}),
		optimizeAttempts:     make(map[string]time.Time),
		repairAttempts:       make(map[string]time.Time),
		promoteBlacklist:     make(map[string]time.Time),
		lastFailures:         make(map[string]EvolutionFailureSummary),
		cancelBySkill:        make(map[string]context.CancelFunc),
		requestIDBySkill:     make(map[string]string),
		shutdownCtx:          shutdownCtx,
		shutdownCancel:       shutdownCancel,
	}
}

// Start begins the background processing loop.
func (p *EvolutionPipeline) Start() {
	if p == nil {
		return
	}
	p.startOnce.Do(func() {
		if recovered, pending, err := p.RecoverPendingCompensations(); err != nil {
			log.Printf("[evolution-pipeline] pending compensation recovery failed: %v", err)
		} else if recovered > 0 || pending > 0 {
			log.Printf("[evolution-pipeline] pending compensation recovery recovered=%d pending=%d", recovered, pending)
		}
		go p.loop()
		// Start the maintenance scheduler if configured.
		if p.Scheduler != nil {
			p.Scheduler.Start()
		}
	})
}

// Stop terminates the background loop.
func (p *EvolutionPipeline) Stop() {
	if p == nil {
		return
	}
	p.once.Do(func() {
		close(p.stopCh)
		if p.shutdownCancel != nil {
			p.shutdownCancel()
		}
		p.markPendingShutdown()
	})
	if p.Scheduler != nil {
		p.Scheduler.Stop()
	}
}

// markPendingShutdown gives queued requests an explicit durable terminal
// state. Previously Stop could discard the coalescing map without emitting any
// evidence, making a graceful shutdown indistinguishable from data loss.
func (p *EvolutionPipeline) markPendingShutdown() {
	if p == nil {
		return
	}
	p.pendingMu.Lock()
	if p.requestStatuses == nil {
		p.requestStatuses = make(map[string]EvolutionRequestStatus)
	}
	pending := make([]evolutionRequest, 0, len(p.pendingBySkill))
	for name, req := range p.pendingBySkill {
		if req.SkillName == "" {
			req.SkillName = name
		}
		pending = append(pending, req)
		delete(p.pendingBySkill, name)
		delete(p.requestStatuses, req.RequestID)
	}
	p.pendingMu.Unlock()
	for _, req := range pending {
		p.emitRequestEvent(EventSkillEvolutionCancelled, req, map[string]string{
			"reason": "shutdown", "termination": "shutdown", "failure_reason": "context_canceled",
			"decision": "cancelled",
		})
	}
}

// NotifySkillExecution is called after a skill execution completes.
// It enqueues an async evolution check without blocking the caller.
// The entry is deep-copied to prevent data races with the main agent loop.
//
// Multiple notifications for the same skill coalesce: only the latest request
// is kept until the worker drains the map, so rapid re-runs never hard-drop work.
func (p *EvolutionPipeline) NotifySkillExecution(skillName string, entry *corelib.NLSkillEntry, result *SkillExecutionResultCompat, runArgs map[string]string) {
	if p == nil || entry == nil {
		return
	}
	skillName = strings.TrimSpace(skillName)
	if skillName == "" {
		skillName = strings.TrimSpace(entry.Name)
	}
	if skillName == "" {
		return
	}
	// Deep-copy entry (including Params maps) to prevent race with main loop mutations.
	entryCopy := CloneNLSkillEntry(entry)
	if entryCopy == nil {
		return
	}
	var argsCopy map[string]string
	if len(runArgs) > 0 {
		argsCopy = make(map[string]string, len(runArgs))
		for k, v := range runArgs {
			argsCopy[k] = v
		}
	}
	// Copy result so caller can reuse the pointer.
	var resultCopy *SkillExecutionResultCompat
	if result != nil {
		rc := *result
		resultCopy = &rc
	}
	req := evolutionRequest{
		RequestID:  newEvolutionRequestID(),
		SkillName:  skillName,
		Entry:      entryCopy,
		ExecResult: resultCopy,
		RunArgs:    argsCopy,
		EnqueuedAt: time.Now().UTC(),
	}

	p.pendingMu.Lock()
	if _, exists := p.pendingBySkill[skillName]; exists {
		if previous, ok := p.pendingBySkill[skillName]; ok {
			delete(p.requestStatuses, previous.RequestID)
		}
		n := p.CoalescedNotifications.Add(1)
		log.Printf("[evolution-pipeline] coalesced pending notification skill=%s coalesced_total=%d", skillName, n)
	}
	if p.pendingBySkill == nil {
		p.pendingBySkill = make(map[string]evolutionRequest)
	}
	p.pendingBySkill[skillName] = req
	if p.requestStatuses == nil {
		p.requestStatuses = make(map[string]EvolutionRequestStatus)
	}
	p.requestStatuses[req.RequestID] = EvolutionRequestStatus{RequestID: req.RequestID, Skill: skillName, State: "pending", EnqueuedAt: req.EnqueuedAt.Format(time.RFC3339)}
	p.pendingMu.Unlock()

	// Non-blocking wake (buffer 1): if the loop is already scheduled, skip.
	select {
	case p.pendingWake <- struct{}{}:
	default:
	}
}

// emitRequestEvent enriches lifecycle events with stable correlation and
// configuration metadata. Callers may add a reason/decision in extra; the
// helper never overwrites an explicitly supplied value.
func (p *EvolutionPipeline) emitRequestEvent(event string, req evolutionRequest, extra map[string]string) {
	if p == nil || p.EventEmitter == nil {
		return
	}
	data := make(map[string]string, len(extra)+5)
	for k, v := range extra {
		data[k] = v
	}
	if strings.TrimSpace(data["request_id"]) == "" && req.RequestID != "" {
		data["request_id"] = req.RequestID
	}
	if strings.TrimSpace(data["skill"]) == "" {
		data["skill"] = req.SkillName
	}
	if strings.TrimSpace(data["attempt"]) == "" {
		data["attempt"] = "1"
	}
	if strings.TrimSpace(data["config_revision"]) == "" {
		data["config_revision"] = p.configRevision()
	}
	if strings.TrimSpace(data["schema_version"]) == "" {
		data["schema_version"] = "2"
	}
	p.EventEmitter(event, data)
}

// configRevision returns a deterministic, non-sensitive snapshot of the
// worker policy. It changes when an operator changes timeout, concurrency or
// evolution switches, allowing audit consumers to reconstruct which policy
// governed a request without persisting the full configuration.
func (p *EvolutionPipeline) configRevision() string {
	if p == nil {
		return ""
	}
	if configured := strings.TrimSpace(p.ConfigRevision); configured != "" {
		return configured
	}
	material := fmt.Sprintf("repair=%t|optimizer=%t|promoter=%t|workers=%d|timeout=%s|cooldown=%s",
		p.EnableRepair, p.EnableOptimizer, p.EnablePromoter, p.MaxConcurrentWorkers,
		p.WorkerTimeout, p.RepairCooldown)
	sum := sha256.Sum256([]byte(material))
	return "sha256:" + hex.EncodeToString(sum[:8])
}

func (p *EvolutionPipeline) requestIDForSkill(skillName string) string {
	if p == nil {
		return ""
	}
	p.cancelMu.Lock()
	defer p.cancelMu.Unlock()
	return p.requestIDBySkill[strings.TrimSpace(skillName)]
}

func (p *EvolutionPipeline) loop() {
	for {
		select {
		case <-p.stopCh:
			return
		case <-p.pendingWake:
			// Delay once per wake batch to avoid interfering with user interaction.
			// Additional notifies during the delay coalesce into pendingBySkill.
			select {
			case <-p.stopCh:
				return
			case <-time.After(p.PostExecDelay):
			}
			batch := p.takeDispatchablePending()
			if len(batch) == 0 {
				// A pending request may be waiting behind an active request for
				// the same skill. The active worker will wake us on completion.
				continue
			}
			var wg sync.WaitGroup
			for _, req := range batch {
				// Re-check stop between skills so shutdown stays responsive.
				select {
				case <-p.stopCh:
					return
				default:
				}
				req := req
				wg.Add(1)
				go func() {
					defer wg.Done()
					defer p.finishActiveSkill(req.SkillName)
					p.processRequest(req)
				}()
			}
			wg.Wait()
		}
	}
}

// takeDispatchablePending reserves up to the configured worker count. A skill
// already being processed is left in the pending map, which guarantees same-
// skill serialization while allowing unrelated skills to run concurrently.
func (p *EvolutionPipeline) takeDispatchablePending() []evolutionRequest {
	if p == nil {
		return nil
	}
	workers := p.MaxConcurrentWorkers
	if workers <= 0 {
		workers = 1
	}
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	if p.activeSkills == nil {
		p.activeSkills = make(map[string]bool)
	}
	if len(p.pendingBySkill) == 0 {
		return nil
	}
	out := make([]evolutionRequest, 0, workers)
	for name, req := range p.pendingBySkill {
		if len(out) >= workers || p.activeSkills[name] {
			continue
		}
		delete(p.pendingBySkill, name)
		p.activeSkills[name] = true
		if p.requestStatuses == nil {
			p.requestStatuses = make(map[string]EvolutionRequestStatus)
		}
		status := p.requestStatuses[req.RequestID]
		status.RequestID = req.RequestID
		status.Skill = req.SkillName
		status.State = "running"
		status.StartedAt = time.Now().UTC().Format(time.RFC3339)
		p.requestStatuses[req.RequestID] = status
		out = append(out, req)
	}
	return out
}

func (p *EvolutionPipeline) finishActiveSkill(name string) {
	if p == nil {
		return
	}
	p.pendingMu.Lock()
	delete(p.activeSkills, name)
	for requestID, status := range p.requestStatuses {
		if status.Skill == name && status.State == "running" {
			delete(p.requestStatuses, requestID)
		}
	}
	p.pendingMu.Unlock()
	select {
	case p.pendingWake <- struct{}{}:
	default:
	}
}

// CancelSkill requests cancellation of the pending or active evolution job
// for one skill. It never cancels the user's skill execution itself. The
// operation is idempotent and returns true when something was removed or
// signalled.
func (p *EvolutionPipeline) CancelSkill(name string) bool {
	if p == nil {
		return false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	found := false
	var cancelledReq evolutionRequest
	p.pendingMu.Lock()
	if req, ok := p.pendingBySkill[name]; ok {
		cancelledReq = req
		delete(p.pendingBySkill, name)
		delete(p.requestStatuses, req.RequestID)
		found = true
	}
	p.pendingMu.Unlock()
	p.cancelMu.Lock()
	if cancel, ok := p.cancelBySkill[name]; ok {
		// Remove the cancel handle before invoking it. This makes repeated
		// CancelSkill calls idempotent even while the worker is still unwinding.
		delete(p.cancelBySkill, name)
		cancel()
		found = true
	}
	if cancelledReq.RequestID == "" {
		cancelledReq = evolutionRequest{RequestID: p.requestIDBySkill[name], SkillName: name}
	}
	p.cancelMu.Unlock()
	if found {
		p.cancelledCount.Add(1)
		if cancelledReq.RequestID == "" {
			cancelledReq = evolutionRequest{RequestID: p.requestIDForSkill(name), SkillName: name}
		}
		p.emitRequestEvent(EventSkillEvolutionCancelled, cancelledReq, map[string]string{
			"reason": "operator_requested", "termination": "operator_cancelled", "failure_reason": "context_canceled",
		})
		select {
		case p.pendingWake <- struct{}{}:
		default:
		}
	}
	return found
}

// takeAllPending drains the coalesce map into a stable slice (name-sorted for
// deterministic test/debug order is not required; map iteration order is fine).
func (p *EvolutionPipeline) takeAllPending() []evolutionRequest {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	if len(p.pendingBySkill) == 0 {
		return nil
	}
	out := make([]evolutionRequest, 0, len(p.pendingBySkill))
	for name, req := range p.pendingBySkill {
		out = append(out, req)
		delete(p.pendingBySkill, name)
	}
	return out
}

// PendingSkillCount returns how many distinct skills currently await processing.
// Intended for tests and diagnostics.
func (p *EvolutionPipeline) PendingSkillCount() int {
	if p == nil {
		return 0
	}
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	return len(p.pendingBySkill)
}

// EvolutionStatus is a diagnostic snapshot of the pipeline.
type EvolutionStatus struct {
	PendingSkills            int                       `json:"pending_skills"`
	CoalescedNotifications   uint64                    `json:"coalesced_notifications"`
	DroppedNotifications     uint64                    `json:"dropped_notifications"`
	ProcessedRequests        int                       `json:"processed_requests"`
	EnableRepair             bool                      `json:"enable_repair"`
	EnableOptimizer          bool                      `json:"enable_optimizer"`
	EnablePromoter           bool                      `json:"enable_promoter"`
	RepairCooldown           time.Duration             `json:"repair_cooldown"`
	HasRepairHook            bool                      `json:"has_repair_hook"`
	HasOptimizer             bool                      `json:"has_optimizer"`
	HasPromoter              bool                      `json:"has_promoter"`
	MaxConcurrentWorkers     int                       `json:"max_concurrent_workers"`
	OldestPendingAt          string                    `json:"oldest_pending_at,omitempty"`
	QueueWaitSeconds         int                       `json:"queue_wait_seconds"`
	FailureSummaries         []EvolutionFailureSummary `json:"failure_summaries,omitempty"`
	ActiveSkills             int                       `json:"active_skills"`
	CancelledRequests        uint64                    `json:"cancelled_requests"`
	TimedOutRequests         uint64                    `json:"timed_out_requests"`
	PendingCompensations     int                       `json:"pending_compensations"`
	CompensationQueueHealthy bool                      `json:"compensation_queue_healthy"`
	CompensationQueueError   string                    `json:"compensation_queue_error,omitempty"`
	Requests                 []EvolutionRequestStatus  `json:"requests,omitempty"`
}

// Status returns a diagnostic snapshot (safe for concurrent readers).
func (p *EvolutionPipeline) Status() EvolutionStatus {
	if p == nil {
		return EvolutionStatus{}
	}
	status := EvolutionStatus{
		PendingSkills:          p.PendingSkillCount(),
		CoalescedNotifications: p.CoalescedNotifications.Load(),
		DroppedNotifications:   p.DroppedNotifications.Load(),
		ProcessedRequests:      int(p.requestCount.Load()),
		EnableRepair:           p.EnableRepair,
		EnableOptimizer:        p.EnableOptimizer,
		EnablePromoter:         p.EnablePromoter,
		RepairCooldown:         p.RepairCooldown,
		HasRepairHook:          p.RepairHook != nil || p.RepairHookWithContext != nil,
		HasOptimizer:           p.Optimizer != nil,
		HasPromoter:            p.Promoter != nil,
		MaxConcurrentWorkers: func() int {
			if p.MaxConcurrentWorkers > 0 {
				return p.MaxConcurrentWorkers
			}
			return 1
		}(),
		CancelledRequests: p.cancelledCount.Load(),
		TimedOutRequests:  p.timedOutCount.Load(),
	}
	if pending, _, err := p.pendingCompensationSnapshot(); err == nil {
		status.PendingCompensations = pending
		status.CompensationQueueHealthy = true
	} else {
		// The admission check is fail-closed when the queue cannot be read;
		// surface that same uncertainty to operators instead of displaying a
		// misleading zero pending count.
		status.CompensationQueueHealthy = false
		status.CompensationQueueError = err.Error()
	}
	p.pendingMu.Lock()
	status.ActiveSkills = len(p.activeSkills)
	var oldest time.Time
	for _, req := range p.pendingBySkill {
		if !req.EnqueuedAt.IsZero() && (oldest.IsZero() || req.EnqueuedAt.Before(oldest)) {
			oldest = req.EnqueuedAt
		}
	}
	p.pendingMu.Unlock()
	if !oldest.IsZero() {
		status.OldestPendingAt = oldest.Format(time.RFC3339)
		status.QueueWaitSeconds = maxInt(0, int(time.Since(oldest).Seconds()))
	}
	p.lastFailureMu.RLock()
	status.FailureSummaries = make([]EvolutionFailureSummary, 0, len(p.lastFailures))
	for _, summary := range p.lastFailures {
		status.FailureSummaries = append(status.FailureSummaries, summary)
	}
	p.lastFailureMu.RUnlock()
	p.pendingMu.Lock()
	status.Requests = make([]EvolutionRequestStatus, 0, len(p.requestStatuses))
	for _, request := range p.requestStatuses {
		status.Requests = append(status.Requests, request)
	}
	p.pendingMu.Unlock()
	sort.Slice(status.Requests, func(i, j int) bool {
		if status.Requests[i].State != status.Requests[j].State {
			return status.Requests[i].State < status.Requests[j].State
		}
		return status.Requests[i].EnqueuedAt < status.Requests[j].EnqueuedAt
	})
	if len(status.Requests) > 64 {
		status.Requests = status.Requests[:64]
	}
	return status
}

func (p *EvolutionPipeline) pendingCompensationSnapshot() (int, int, error) {
	records, err := readEvolutionCompensations()
	if err != nil {
		return 0, 0, err
	}
	needsReview := 0
	for _, record := range records {
		if strings.TrimSpace(record.Status) == "needs_review" {
			needsReview++
		}
	}
	return len(records), needsReview, nil
}

func runArgsDigest(args map[string]string) string {
	if len(args) == 0 {
		return ""
	}
	b, _ := json.Marshal(args)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (p *EvolutionPipeline) recordFailure(req evolutionRequest) {
	if p == nil {
		return
	}
	message := "execution failed"
	class := ""
	if req.ExecResult != nil {
		message = strings.TrimSpace(req.ExecResult.Error)
		class = strings.TrimSpace(req.ExecResult.ErrorClass)
	}
	if message == "" {
		message = "execution failed"
	}
	if class == "" {
		class = ExtractErrorClass(message)
	}
	if len(message) > 512 {
		message = message[:512] + "…"
	}
	p.lastFailureMu.Lock()
	if p.lastFailures == nil {
		p.lastFailures = make(map[string]EvolutionFailureSummary)
	}
	summary := p.lastFailures[req.SkillName]
	summary.Skill = req.SkillName
	summary.FailureCount++
	summary.LastError = message
	summary.LastErrorClass = class
	summary.LastArgsDigest = runArgsDigest(req.RunArgs)
	summary.LastFailureAt = time.Now().UTC().Format(time.RFC3339)
	p.lastFailures[req.SkillName] = summary
	p.lastFailureMu.Unlock()
}

func (p *EvolutionPipeline) processRequest(req evolutionRequest) {
	// Recover from panics to prevent the pipeline goroutine from dying.
	// A crashed pipeline would silently stop all self-evolution processing.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[evolution-pipeline] PANIC recovered in processRequest for skill=%s: %v", req.SkillName, r)
		}
	}()

	parentCtx := p.shutdownCtx
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	workerTimeout := p.WorkerTimeout
	if workerTimeout <= 0 {
		workerTimeout = DefaultEvolutionWorkerTimeout
	}
	ctx, cancel := context.WithTimeout(parentCtx, workerTimeout)
	defer cancel()
	ctx = WithEvolutionRequestMetadata(ctx, req.RequestID, 1)
	p.cancelMu.Lock()
	if p.cancelBySkill == nil {
		p.cancelBySkill = make(map[string]context.CancelFunc)
	}
	p.cancelBySkill[req.SkillName] = cancel
	if p.requestIDBySkill == nil {
		p.requestIDBySkill = make(map[string]string)
	}
	p.requestIDBySkill[req.SkillName] = req.RequestID
	p.cancelMu.Unlock()
	defer func() {
		p.cancelMu.Lock()
		delete(p.cancelBySkill, req.SkillName)
		delete(p.requestIDBySkill, req.SkillName)
		p.cancelMu.Unlock()
		if err := ctx.Err(); err != nil {
			if err == context.DeadlineExceeded {
				p.timedOutCount.Add(1)
				p.emitRequestEvent(EventSkillEvolutionTimedOut, req, map[string]string{
					"reason": "worker_deadline", "termination": "worker_timeout", "failure_reason": "deadline_exceeded",
				})
			} else if err == context.Canceled && p.shutdownCtx != nil && p.shutdownCtx.Err() != nil {
				p.emitRequestEvent(EventSkillEvolutionCancelled, req, map[string]string{
					"reason": "shutdown", "termination": "shutdown", "failure_reason": "context_canceled",
				})
			}
		}
	}()

	p.requestCount.Add(1)

	// 0. Surface failed executions for frontend/audit.
	failed := req.ExecResult != nil && !req.ExecResult.Success
	if failed {
		p.recordFailure(req)
	}
	if failed {
		p.emitRequestEvent(EventSkillExecutionFailed, req, map[string]string{
			"reason": "execution_failed", "failure_reason": strings.TrimSpace(req.ExecResult.ErrorClass),
		})
	}

	// 1. Self-repair for failed skills (unified schedule + throttle).
	// Platform security/persist is handled by RepairHook when configured.
	if failed && p.EnableRepair && req.Entry != nil {
		p.tryRepair(ctx, req)
	}
	if ctx.Err() != nil {
		return
	}

	// 2. Check if skill should be optimized (working but suboptimal).
	// tryOptimize consults ShouldOptimize + 24h throttle; safe on failures too.
	if p.EnableOptimizer && p.Optimizer != nil && req.Entry != nil {
		p.tryOptimize(ctx, req)
	}
	if ctx.Err() != nil {
		return
	}

	// 3. Check nudge candidates for promotion (throttled — every N requests).
	interval := p.PromoteCheckInterval
	if interval <= 0 {
		interval = 10
	}
	if p.EnablePromoter && p.Promoter != nil && p.UsageTracker != nil && p.requestCount.Load()%int64(interval) == 0 {
		p.tryPromoteNudges(ctx)
	}
}

// tryRepair schedules/executes self-repair for a failed skill execution.
func (p *EvolutionPipeline) tryRepair(ctx context.Context, req evolutionRequest) {
	if req.Entry == nil {
		return
	}
	ok, reason := ExplainRepairGate(req.Entry)
	fileBackedOnly := false
	if !ok {
		if reason != "file_backed" {
			log.Printf("[evolution-pipeline] repair skipped skill=%s reason=%s", req.SkillName, reason)
			return
		}
		fileBackedOnly = true
	}
	// Prefer freshest entry from SkillLoader (usage stats may have advanced).
	entry := req.Entry
	if p.SkillLoader != nil {
		for _, s := range p.SkillLoader() {
			if s.Name == req.SkillName {
				cp := CloneNLSkillEntry(&s)
				if cp != nil {
					entry = cp
				}
				break
			}
		}
		ok, reason = ExplainRepairGate(entry)
		if !ok {
			if reason != "file_backed" {
				log.Printf("[evolution-pipeline] repair skipped skill=%s reason=%s", req.SkillName, reason)
				return
			}
			fileBackedOnly = true
		} else {
			fileBackedOnly = false
		}
	}

	// 节流检查在这里，但冷却时间戳只在真正发起修复动作前才写（见下方各分支），
	// 避免被 ctx 取消/LLM 未配置的尝试白白消耗冷却；LLM 已调用但失败/gate 拒绝
	// 的尝试消耗了真实 LLM 调用，必须写时间戳防止每次失败都白调一次。
	// RepairHook 分支在调 hook 前写时间戳：hook 内部可能因 canStartRepairSkill
	// 复检而 bail（未发 LLM 调用），但即便如此也要节流，否则该技能的每次失败
	// 都会重复触发 hook。
	cooldown := p.RepairCooldown
	if cooldown <= 0 {
		cooldown = DefaultRepairCooldown
	}
	p.throttleMu.Lock()
	if last, ok := p.repairAttempts[req.SkillName]; ok && time.Since(last) < cooldown {
		p.throttleMu.Unlock()
		log.Printf("[evolution-pipeline] repair throttled skill=%s remaining=%s", req.SkillName, (cooldown - time.Since(last)).Round(time.Second))
		return
	}
	p.throttleMu.Unlock()

	if ctx.Err() != nil {
		return
	}

	// file-backed 技能不后台改盘，走人审 patch draft 流（P0-4）。
	if fileBackedOnly {
		p.tryFileBackedRepairDraft(ctx, req, entry, false)
		return
	}

	log.Printf("[evolution-pipeline] scheduling self-repair skill=%s usage=%d success=%d",
		req.SkillName, entry.UsageCount, entry.SuccessCount)

	if p.RepairHookWithContext != nil || p.RepairHook != nil {
		if entry.RepairAttemptCount >= SelfRepairMaxAttempts {
			return
		}
		p.markRepairAttempt(req.SkillName)
		if p.RepairHookWithContext != nil {
			p.RepairHookWithContext(ctx, entry, req.RunArgs)
		} else {
			p.RepairHook(entry, req.RunArgs)
		}
		return
	}

	// Core-only fallback (no platform security scan): useful for tests / headless.
	if p.LLM == nil || !p.LLM.IsConfigured() {
		log.Printf("[evolution-pipeline] repair skipped skill=%s: LLM not configured", req.SkillName)
		return
	}
	repairCtx := NewRepairContext(entry, req.RunArgs)
	result, err := AttemptRepairWithGoContext(ctx, p.LLM, entry, repairCtx)
	if err != nil {
		// LLM 调用已真实发生（失败也算成本），消耗冷却防止每次失败都重调。
		p.markRepairAttempt(req.SkillName)
		p.recordRepairFailure(req, entry, ExtractErrorClass(entry.LastError), err.Error())
		log.Printf("[evolution-pipeline] repair LLM failed skill=%s: %v", req.SkillName, err)
		return
	}
	if result != nil && result.Repaired && len(result.NewSteps) > 0 && p.Gate != nil {
		nlSteps := make([]corelib.NLSkillStep, len(result.NewSteps))
		for i, s := range result.NewSteps {
			nlSteps[i] = corelib.NLSkillStep{Action: s.Action, Params: s.Params, OnError: s.OnError}
		}
		var historicalArgs []map[string]string
		if p.UsageTracker != nil {
			historicalArgs = p.UsageTracker.RecentRunArgs("skill:"+req.SkillName, 3)
		}
		{
			gateResult, gateErr := p.Gate.Verify(ctx, entry, nlSteps, historicalArgs)
			if gateErr != nil {
				// gate 运行在 LLM 调用之后，错误也意味着本次 LLM 调用已白花。
				p.markRepairAttempt(req.SkillName)
				p.recordRepairFailure(req, entry, "gate_error", gateErr.Error())
				log.Printf("[evolution-pipeline] repair gate error skill=%s: %v", req.SkillName, gateErr)
				return
			}
			if !gateResult.IsRealPass() {
				p.markRepairAttempt(req.SkillName)
				reason := "nil"
				if gateResult != nil {
					reason = gateResult.Reason
					if gateResult.Status == "passed" && gateResult.EvidenceMode != "real" {
						reason = "passed gate lacks real evidence"
					}
				}
				p.recordRepairFailure(req, entry, "gate_rejected", reason)
				log.Printf("[evolution-pipeline] repair gate rejected skill=%s: %s", req.SkillName, reason)
				return
			}
		}
	}
	// LLM 修复成功且 gate 通过、真正落 ApplyRepair 之前才写冷却时间戳。
	p.markRepairAttempt(req.SkillName)
	originalEntry := CloneNLSkillEntry(entry)
	if !ApplyRepair(entry, result) {
		if result != nil && !result.ShouldDisable {
			p.recordRepairFailure(req, entry, ExtractErrorClass(entry.LastError), result.Explanation)
		}
		if result != nil && result.ShouldDisable && p.SkillSaver != nil && p.SkillLoader != nil {
			skills := p.SkillLoader()
			for i := range skills {
				if skills[i].Name == req.SkillName {
					mergeEvolvedEntry(&skills[i], entry)
					_ = p.SkillSaver(skills)
					break
				}
			}
		}
		return
	}
	if p.SkillSaver == nil || p.SkillLoader == nil {
		if originalEntry != nil {
			*entry = *originalEntry
		}
		log.Printf("[evolution-pipeline] repair not persisted skill=%s: persistence not configured", req.SkillName)
		return
	}
	commitAudit := map[string]string{
		"skill": req.SkillName, "action": "repair", "decision": "committed",
		"request_id": req.RequestID, "attempt": "1", "config_revision": p.configRevision(),
		"schema_version": "2", "evidence_mode": "real",
	}
	commit := p.persistDefinitionChangeWithAudit(ctx, req.SkillName, entry, "skill:repair_commit", commitAudit)
	if commit.State != "committed" || commit.CleanupStatus != "clear" {
		if originalEntry != nil {
			*entry = *originalEntry
		}
		log.Printf("[evolution-pipeline] repair commit %s skill=%s reason=%s", commit.State, req.SkillName, commit.FailureReason)
		p.emitRequestEvent(EventSkillEvolutionRolledBack, req, map[string]string{
			"decision": commit.State, "reason": "repair_persistence_failed", "failure_reason": commit.FailureReason,
		})
		return
	}
	explanation := ""
	if result != nil {
		explanation = result.Explanation
	}
	p.emitRequestEvent(EventSkillRepaired, req, map[string]string{
		"explanation": explanation, "decision": "applied", "reason": "repair_gate_passed",
	})
	log.Printf("[evolution-pipeline] repair applied skill=%s", req.SkillName)
}

// recordRepairFailure applies the shared attempt-limit policy to the core
// fallback path and persists the governance metadata without clobbering live
// execution counters. Platform hooks may persist their own failures because
// they have additional security/GUI context; this helper is for the headless
// pipeline path only.
func (p *EvolutionPipeline) recordRepairFailure(req evolutionRequest, entry *corelib.NLSkillEntry, errorClass, explanation string) {
	if p == nil || entry == nil {
		return
	}
	RecordRepairAttemptFailure(entry, errorClass, explanation)
	if p.SkillLoader == nil || p.SkillSaver == nil {
		return
	}
	skills := p.SkillLoader()
	for i := range skills {
		if skills[i].Name != req.SkillName {
			continue
		}
		mergeEvolvedEntry(&skills[i], entry)
		if err := p.SkillSaver(skills); err != nil {
			log.Printf("[evolution-pipeline] save failed-attempt metadata skill=%s failed: %v", req.SkillName, err)
		}
		return
	}
}

// tryFileBackedRepairDraft 为 file-backed 技能生成人审 patch draft（P0-4
// 方案 A）：复用 LLM 修复 + gate 验证，通过后把 draft 写到
// <skill_dir>/.evolution-drafts/<utc时间戳>.json 并发 EventSkillRepairDraftReady。
// 本路径绝不修改 entry、不调 SkillSaver、不写回 skill.yaml —— 应用/拒绝由
// GUI 人审时完成。
func (p *EvolutionPipeline) tryFileBackedRepairDraft(ctx context.Context, req evolutionRequest, entry *corelib.NLSkillEntry, force bool) RepairDraftResult {
	// 除 file-backed 外其他门槛仍需全部通过（max_attempts / error class / 用量统计）。
	if ok, reason := explainRepairGate(entry, true); !ok && !(force && CanForceAttemptFileBackedRepairDraft(entry)) {
		log.Printf("[evolution-pipeline] repair draft skipped skill=%s reason=%s", req.SkillName, reason)
		return RepairDraftResult{SkipReason: reason}
	}

	// 已有未评审 draft 时不重复生成。
	draftsDir := filepath.Join(entry.SkillDir, RepairDraftsDirName)
	if HasPendingRepairDraft(draftsDir) {
		log.Printf("[evolution-pipeline] repair draft skipped skill=%s reason=draft_pending", req.SkillName)
		return RepairDraftResult{SkipReason: "draft_pending", RequiresReview: true}
	}

	// SKILL.md-only 技能没有机器可写的 steps 文件，apply 必然失败——在 LLM
	// 调用前跳过（不烧 LLM，也不写冷却时间戳：反正永远不会成功）。
	if !hasSkillYAMLFile(entry.SkillDir) {
		log.Printf("[evolution-pipeline] repair draft skipped skill=%s reason=no_skill_yaml", req.SkillName)
		return RepairDraftResult{SkipReason: "no_skill_yaml"}
	}

	// 含 poll/loop 步骤的技能：WriteBackOptimizedSteps 不回写 poll/loop，
	// apply 会静默剥离这些配置——跳过生成，不烧 LLM。
	if StepsHavePollLoop(entry.Steps) {
		log.Printf("[evolution-pipeline] repair draft skipped skill=%s reason=poll_loop_unsupported", req.SkillName)
		return RepairDraftResult{SkipReason: "poll_loop_unsupported"}
	}

	if p.LLM == nil || !p.LLM.IsConfigured() {
		log.Printf("[evolution-pipeline] repair draft skipped skill=%s: LLM not configured", req.SkillName)
		return RepairDraftResult{SkipReason: "llm_not_configured"}
	}
	if ctx.Err() != nil {
		return RepairDraftResult{SkipReason: "context_cancelled"}
	}

	repairCtx := NewRepairContext(entry, req.RunArgs)
	result, err := AttemptRepairWithGoContext(ctx, p.LLM, entry, repairCtx)
	if err != nil {
		// LLM 调用已真实发生（失败也算成本），消耗冷却防止每次失败都重调。
		p.markRepairAttempt(req.SkillName)
		if ctx.Err() == nil {
			p.recordRepairFailure(req, entry, ExtractErrorClass(entry.LastError), err.Error())
		}
		log.Printf("[evolution-pipeline] repair draft LLM failed skill=%s: %v", req.SkillName, err)
		return RepairDraftResult{SkipReason: "llm_error", Explanation: err.Error()}
	}
	if result != nil && result.ShouldDisable {
		// LLM 认为不可修复应禁用：生成"禁用建议" draft（NewSteps/OldSteps 为
		// 空，Explanation 说明禁用理由），由人审决定是否禁用。写盘+发事件流
		// 程与普通 draft 复用。
		draft := RepairDraft{
			Skill:       req.SkillName,
			Explanation: result.Explanation,
			LastError:   entry.LastError,
			CreatedAt:   time.Now().UTC().Format(time.RFC3339),
			Disable:     true,
		}
		name, err := p.writeRepairDraftAndNotify(req.SkillName, entry.SkillDir, draft)
		if err != nil {
			if ctx.Err() == nil {
				p.recordRepairFailure(req, entry, "draft_write_failed", err.Error())
			}
			return RepairDraftResult{SkipReason: "draft_write_failed", Explanation: err.Error()}
		}
		return RepairDraftResult{Created: true, Draft: name, Explanation: result.Explanation, RequiresReview: true}
	}
	if result == nil || !result.Repaired || len(result.NewSteps) == 0 {
		// LLM 已调用但认为不可修复/被 sanitize 拒绝：同样消耗冷却。
		// （AttemptRepairWithContext 内部不经 LLM 的 not-repairable 早退
		// 不可达——上方 gate 已用同一 IsRepairableError 过滤过。）
		explanation := ""
		if result != nil {
			explanation = result.Explanation
		}
		p.markRepairAttempt(req.SkillName)
		if ctx.Err() == nil {
			p.recordRepairFailure(req, entry, ExtractErrorClass(entry.LastError), explanation)
		}
		log.Printf("[evolution-pipeline] repair draft not applicable skill=%s: %s", req.SkillName, explanation)
		return RepairDraftResult{SkipReason: "not_repairable", Explanation: explanation}
	}

	nlSteps := convertRepairResultSteps(result.NewSteps)
	if p.Gate != nil {
		var historicalArgs []map[string]string
		if p.UsageTracker != nil {
			historicalArgs = p.UsageTracker.RecentRunArgs("skill:"+req.SkillName, 3)
		}
		{
			gateResult, gateErr := p.Gate.Verify(ctx, entry, nlSteps, historicalArgs)
			if gateErr != nil {
				p.markRepairAttempt(req.SkillName)
				if ctx.Err() == nil {
					p.recordRepairFailure(req, entry, "gate_error", gateErr.Error())
				}
				log.Printf("[evolution-pipeline] repair draft gate error skill=%s: %v", req.SkillName, gateErr)
				return RepairDraftResult{SkipReason: "gate_error", Explanation: gateErr.Error()}
			}
			if !gateResult.IsRealPass() {
				p.markRepairAttempt(req.SkillName)
				reason := "nil"
				if gateResult != nil {
					reason = gateResult.Reason
					if gateResult.Status == "passed" && gateResult.EvidenceMode != "real" {
						reason = "passed gate lacks real evidence"
					}
				}
				if ctx.Err() == nil {
					p.recordRepairFailure(req, entry, "gate_rejected", reason)
				}
				log.Printf("[evolution-pipeline] repair draft gate rejected skill=%s: %s", req.SkillName, reason)
				return RepairDraftResult{SkipReason: "gate_rejected", Explanation: reason}
			}
		}
	}

	draft := RepairDraft{
		Skill:       req.SkillName,
		OldSteps:    entry.Steps,
		NewSteps:    nlSteps,
		Explanation: result.Explanation,
		LastError:   entry.LastError,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	name, err := p.writeRepairDraftAndNotify(req.SkillName, entry.SkillDir, draft)
	if err != nil {
		return RepairDraftResult{SkipReason: "draft_write_failed", Explanation: err.Error()}
	}
	return RepairDraftResult{Created: true, Draft: name, Explanation: result.Explanation, RequiresReview: true}
}

// TriggerFileBackedRepairDraft is the synchronous, user-triggered entry point
// for file-backed skills.  It honors the repair cooldown but, when force is
// true, may bypass only the usage-rate threshold.  It never edits skill.yaml.
func (p *EvolutionPipeline) TriggerFileBackedRepairDraft(ctx context.Context, entry *corelib.NLSkillEntry, runArgs map[string]string, force bool) RepairDraftResult {
	if p == nil || entry == nil {
		return RepairDraftResult{SkipReason: "pipeline_or_skill_unavailable"}
	}
	if !IsFileBackedSkill(*entry) {
		return RepairDraftResult{SkipReason: "not_file_backed"}
	}
	cooldown := p.RepairCooldown
	if cooldown <= 0 {
		cooldown = DefaultRepairCooldown
	}
	p.throttleMu.Lock()
	last, attempted := p.repairAttempts[entry.Name]
	p.throttleMu.Unlock()
	if attempted && time.Since(last) < cooldown {
		return RepairDraftResult{SkipReason: "repair_throttled"}
	}
	return p.tryFileBackedRepairDraft(ctx, evolutionRequest{
		RequestID: newEvolutionRequestID(),
		SkillName: entry.Name,
		Entry:     entry,
		RunArgs:   runArgs,
	}, entry, force)
}

// writeRepairDraftAndNotify persists a repair draft and emits
// EventSkillRepairDraftReady. A successful write consumes the repair cooldown
// (shared with the auto-repair throttle); a failed write does not — nothing
// was produced, so the next failure may retry immediately.
func (p *EvolutionPipeline) writeRepairDraftAndNotify(skillName, skillDir string, draft RepairDraft) (string, error) {
	name, err := WriteRepairDraft(skillDir, draft)
	if err != nil {
		// 写盘失败不消耗冷却——draft 没产出，下次可立即重试。
		log.Printf("[evolution-pipeline] repair draft write failed skill=%s: %v", skillName, err)
		return "", err
	}
	// 与自动修复共用 repairAttempts 冷却节流：draft 落盘成功才写时间戳。
	p.markRepairAttempt(skillName)
	p.emitRequestEvent(EventSkillRepairDraftReady, evolutionRequest{RequestID: newEvolutionRequestID(), SkillName: skillName}, map[string]string{
		"draft": name, "decision": "draft", "reason": "review_required",
	})
	log.Printf("[evolution-pipeline] repair draft ready skill=%s draft=%s", skillName, name)
	return name, nil
}

// hasSkillYAMLFile reports whether skillDir contains a skill.yaml or
// skill.yml — the only durable steps store a repair draft can be applied to.
func hasSkillYAMLFile(skillDir string) bool {
	for _, name := range []string{"skill.yaml", "skill.yml"} {
		if st, err := os.Stat(filepath.Join(skillDir, name)); err == nil && !st.IsDir() {
			return true
		}
	}
	return false
}

// markRepairAttempt records the cooldown timestamp for a skill repair attempt.
// Call it once an LLM repair call has actually been spent (success, LLM error,
// gate error/rejection, or a draft written to disk) — throttled, cancelled or
// not-configured attempts must not consume the cooldown, and a failed draft
// write doesn't either (nothing was produced, retry may happen immediately).
func (p *EvolutionPipeline) markRepairAttempt(skillName string) {
	p.throttleMu.Lock()
	p.repairAttempts[skillName] = time.Now()
	p.throttleMu.Unlock()
}

// mergeEvolvedEntry copies only the fields that self-repair / optimization may
// modify from src into dst, preserving dst's live usage stats (UsageCount,
// SuccessCount, FailureCount, LastError, LastUsedAt) that may have advanced
// since src was loaded. Used when persisting evolved skills to avoid clobbering
// concurrent stats updates (TOCTOU).
//
// LastError is the exception: ApplyRepair rewrites it into a repair artifact
// marker ("auto-repaired: ..." / "auto-disabled: ...") instead of a live
// failure stat, so only those prefixed markers are copied — otherwise the
// marker would be lost and the persisted stale error would trigger repeated
// repairs of an already-fixed skill.
func mergeEvolvedEntry(dst *corelib.NLSkillEntry, src *corelib.NLSkillEntry) {
	if dst == nil || src == nil {
		return
	}
	dst.Steps = src.Steps
	dst.Description = src.Description
	dst.Status = src.Status
	dst.RepairAttemptCount = src.RepairAttemptCount
	dst.LastRepairAt = src.LastRepairAt
	dst.RepairHistory = src.RepairHistory
	dst.OptimizationCount = src.OptimizationCount
	dst.LastOptimizedAt = src.LastOptimizedAt
	if strings.HasPrefix(src.LastError, "auto-repaired:") || strings.HasPrefix(src.LastError, "auto-disabled:") {
		dst.LastError = src.LastError
	}
}

// persistDefinitionChange commits an evolved entry as one compensating
// transaction across the config overlay and the authoritative skill.yaml.
// SkillSaver is the config commit boundary; YAML is written only after the
// old bytes have been captured. If either side fails, both are restored before
// the caller is allowed to emit a success event or trigger an upload.
//
// The helper intentionally lives in the pipeline package so headless callers
// get the same safety semantics as the GUI wiring. Index refresh remains the
// responsibility of SkillSaver/its owner; a saver that reports success must
// have refreshed its index before returning.
type EvolutionCommitResult struct {
	State            string // committed | rolled_back | audit_pending
	FailureReason    string
	RollbackComplete bool
	CleanupStatus    string // clear | pending | needs_review
	RequestID        string
	BackupVersion    string
	ConfigRevision   string
}

func (p *EvolutionPipeline) persistDefinitionChange(ctx context.Context, skillName string, after *corelib.NLSkillEntry) EvolutionCommitResult {
	return p.persistDefinitionChangeWithAudit(ctx, skillName, after, "", nil)
}

// persistDefinitionChangeWithAudit is the pipeline adapter for the shared
// SkillCommitter. Keeping the adapter here preserves the existing pipeline
// call sites while making GUI/headless callers use the same transaction
// protocol and result fields.
func (p *EvolutionPipeline) persistDefinitionChangeWithAudit(ctx context.Context, skillName string, after *corelib.NLSkillEntry, event string, auditData map[string]string) EvolutionCommitResult {
	if p == nil {
		return EvolutionCommitResult{State: "rolled_back", FailureReason: "persistence_not_configured", CleanupStatus: "clear"}
	}
	return (&SkillCommitter{
		SkillLoader:      p.SkillLoader,
		SkillSaver:       p.SkillSaver,
		DefinitionWriter: p.DefinitionWriter,
		IndexRefresher:   p.IndexRefresher,
		FinalAuditor:     p.FinalAuditor,
		ConfigRevision:   p.configRevision(),
	}).Commit(ctx, skillName, after, event, auditData)
}

// persistDefinitionChangeLegacy is retained temporarily for source-level
// bisectability while all production call sites use SkillCommitter above.
// It can be removed once downstream adapters no longer reference it.
func (p *EvolutionPipeline) persistDefinitionChangeLegacy(ctx context.Context, skillName string, after *corelib.NLSkillEntry, event string, auditData map[string]string) EvolutionCommitResult {
	if p == nil || p.SkillLoader == nil || p.SkillSaver == nil || after == nil {
		return EvolutionCommitResult{State: "rolled_back", FailureReason: "persistence_not_configured"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return EvolutionCommitResult{State: "rolled_back", FailureReason: evolutionFailureReason(err)}
	}

	// Read the latest list immediately before the commit so live usage counters
	// are preserved, and retain a deep copy for config rollback.
	originalSkills := p.SkillLoader()
	rollbackSkills := make([]corelib.NLSkillEntry, len(originalSkills))
	for i := range originalSkills {
		if cp := CloneNLSkillEntry(&originalSkills[i]); cp != nil {
			rollbackSkills[i] = *cp
		}
	}
	updatedSkills := make([]corelib.NLSkillEntry, len(originalSkills))
	copy(updatedSkills, originalSkills)
	found := false
	for i := range updatedSkills {
		if updatedSkills[i].Name == skillName {
			mergeEvolvedEntry(&updatedSkills[i], after)
			found = true
			break
		}
	}
	if !found {
		return EvolutionCommitResult{State: "rolled_back", FailureReason: "skill_not_found"}
	}

	yamlPath, yamlBackup, yamlExists, err := evolutionYAMLBackup(after)
	if err != nil {
		return EvolutionCommitResult{State: "rolled_back", FailureReason: "yaml_backup_failed"}
	}
	configCommitAttempted := false
	rollback := func(cause error) EvolutionCommitResult {
		var rollbackErr error
		if yamlExists {
			if err := restoreEvolutionYAML(yamlPath, yamlBackup); err != nil {
				rollbackErr = fmt.Errorf("restore YAML: %w", err)
			}
		}
		if configCommitAttempted {
			if err := p.SkillSaver(rollbackSkills); err != nil {
				if rollbackErr != nil {
					rollbackErr = fmt.Errorf("%v; restore config: %w", rollbackErr, err)
				} else {
					rollbackErr = fmt.Errorf("restore config: %w", err)
				}
			}
		}
		if p.IndexRefresher != nil {
			if err := p.IndexRefresher(); err != nil {
				if rollbackErr != nil {
					rollbackErr = fmt.Errorf("%v; refresh index after rollback: %w", rollbackErr, err)
				} else {
					rollbackErr = fmt.Errorf("refresh index after rollback: %w", err)
				}
			}
		}
		if rollbackErr != nil {
			requestID, _ := EvolutionRequestMetadata(ctx)
			action := "evolution"
			if auditData != nil && strings.TrimSpace(auditData["action"]) != "" {
				action = auditData["action"]
			}
			record := newEvolutionCompensationRecord(requestID, skillName, action, yamlPath, yamlBackup, yamlExists, rollbackSkills, evolutionFailureReason(cause)+":rollback_incomplete")
			if err := appendEvolutionCompensation(record); err != nil {
				log.Printf("[evolution-pipeline] cannot persist compensation record skill=%s: %v", skillName, err)
			}
			return EvolutionCommitResult{State: "audit_pending", FailureReason: evolutionFailureReason(cause) + ":rollback_incomplete", RollbackComplete: false}
		}
		return EvolutionCommitResult{State: "rolled_back", FailureReason: evolutionFailureReason(cause), RollbackComplete: true}
	}

	if err := ctx.Err(); err != nil {
		return EvolutionCommitResult{State: "rolled_back", FailureReason: evolutionFailureReason(err), RollbackComplete: true}
	}
	configCommitAttempted = true
	if err := p.SkillSaver(updatedSkills); err != nil {
		// A saver may have partially written before reporting an error. Treat
		// the call as an attempted commit and run the same compensation path.
		return rollback(fmt.Errorf("save evolved skill: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return rollback(err)
	}
	if yamlExists {
		writer := p.DefinitionWriter
		if writer == nil {
			writer = WriteBackOptimizedSteps
		}
		if err := writer(after); err != nil {
			return rollback(fmt.Errorf("write evolved skill.yaml: %w", err))
		}
	}
	if p.IndexRefresher != nil {
		if err := p.IndexRefresher(); err != nil {
			return rollback(fmt.Errorf("refresh skill index: %w", err))
		}
	}
	if err := ctx.Err(); err != nil {
		return rollback(err)
	}
	if p.FinalAuditor != nil && strings.TrimSpace(event) != "" {
		if err := p.FinalAuditor(event, auditData); err != nil {
			return rollback(fmt.Errorf("final audit: %w", err))
		}
	}
	return EvolutionCommitResult{State: "committed", RollbackComplete: true}
}

func evolutionFailureReason(err error) string {
	if err == nil {
		return "unknown"
	}
	if errors.Is(err, context.Canceled) {
		return "context_canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	// Preserve the commit phase in the structured result.  Callers use this
	// value for deterministic UI/admission handling; collapsing every phase to
	// persistence_failed made a final-audit failure indistinguishable from an
	// index publication failure and prevented operators from choosing the right
	// recovery action.
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "final audit"):
		return "final_audit_failed"
	case strings.Contains(message, "refresh skill index") || strings.Contains(message, "refresh index"):
		return "index_refresh_failed"
	case strings.Contains(message, "write evolved skill.yaml"):
		return "yaml_write_failed"
	case strings.Contains(message, "save evolved skill"):
		return "config_write_failed"
	case strings.Contains(message, "external commit"):
		return "external_commit_failed"
	}
	return "persistence_failed"
}

func evolutionYAMLBackup(entry *corelib.NLSkillEntry) (string, []byte, bool, error) {
	if entry == nil || strings.TrimSpace(entry.SkillDir) == "" {
		return "", nil, false, nil
	}
	for _, name := range []string{"skill.yaml", "skill.yml"} {
		path := filepath.Join(entry.SkillDir, name)
		data, err := os.ReadFile(path)
		if err == nil {
			return path, data, true, nil
		}
		if !os.IsNotExist(err) {
			return "", nil, false, fmt.Errorf("read %s before evolution commit: %w", path, err)
		}
	}
	return "", nil, false, nil
}

func restoreEvolutionYAML(path string, data []byte) error {
	if strings.TrimSpace(path) == "" || len(data) == 0 {
		return fmt.Errorf("empty YAML rollback target")
	}
	tmp := path + ".evolution.rollback.tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := renameCompensationPath(tmp, path); err != nil {
		// Windows cannot rename a file over an existing destination. The
		// rollback pre-image remains in tmp until this replacement succeeds, and
		// the caller retains the same bytes in durable compensation if process
		// interruption occurs between remove and rename.
		if removeErr := removeCompensationPath(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("replace rollback target: %w (initial rename: %v)", removeErr, err)
		}
		if retryErr := renameCompensationPath(tmp, path); retryErr != nil {
			return fmt.Errorf("replace rollback target after remove: %w (initial rename: %v)", retryErr, err)
		}
	}
	return nil
}

// OptimizeResult describes the outcome of a one-shot TriggerOptimize call.
type OptimizeResult struct {
	Attempted   bool
	Optimized   bool
	Explanation string
	Skipped     bool
	SkipReason  string
}

// TriggerOptimize runs a one-shot optimization for entry (used by manage_skill
// trigger_optimize). force skips ShouldOptimize and the 24h attempt throttle;
// file-backed skills and missing LLM still hard-block.
func (p *EvolutionPipeline) TriggerOptimize(ctx context.Context, entry *corelib.NLSkillEntry, force bool) OptimizeResult {
	if p == nil || entry == nil {
		return OptimizeResult{SkipReason: "pipeline or entry is nil"}
	}
	if p.Optimizer == nil {
		return OptimizeResult{SkipReason: "optimizer not configured"}
	}
	if IsAgentGuidedWorkflowSkill(entry) {
		return OptimizeResult{SkipReason: "agent-guided workflows require interactive orchestration and cannot be optimized as one GUI skill step"}
	}
	if IsFileBackedSkill(*entry) {
		return OptimizeResult{SkipReason: "file-backed skills require a reviewed patch flow"}
	}
	if p.UsageTracker == nil {
		return OptimizeResult{SkipReason: "usage tracker not configured"}
	}
	req := evolutionRequest{RequestID: newEvolutionRequestID(), SkillName: entry.Name, Entry: entry}
	return p.runOptimize(ctx, req, force)
}

func (p *EvolutionPipeline) tryOptimize(ctx context.Context, req evolutionRequest) {
	_ = p.runOptimize(ctx, req, false)
}

func (p *EvolutionPipeline) runOptimize(ctx context.Context, req evolutionRequest, force bool) OptimizeResult {
	if req.Entry == nil || p.UsageTracker == nil || p.Optimizer == nil {
		return OptimizeResult{SkipReason: "optimizer prerequisites missing"}
	}
	if IsAgentGuidedWorkflowSkill(req.Entry) {
		return OptimizeResult{Skipped: true, SkipReason: "agent-guided workflows require interactive orchestration and cannot be optimized as one GUI skill step"}
	}

	// UsageTracker records skill executions with "skill:" prefix on ToolName.
	trackerName := "skill:" + req.SkillName

	// Get recent records for this skill.
	exports := p.UsageTracker.RecentSkillUsageRecords(trackerName, 14)
	records := make([]SkillUsageRecord, len(exports))
	for i, e := range exports {
		records[i] = SkillUsageRecord{
			Success:    e.Success,
			FollowUp:   e.FollowUp,
			ErrorClass: e.ErrorClass,
			Timestamp:  e.Timestamp,
		}
	}

	if !force && !p.Optimizer.ShouldOptimize(req.Entry, records) {
		reason := "skill does not meet automatic optimization thresholds"
		log.Printf("[evolution-pipeline] optimize skipped skill=%s reason=%s", req.SkillName, reason)
		return OptimizeResult{Skipped: true, SkipReason: reason}
	}
	if force && IsFileBackedSkill(*req.Entry) {
		reason := "file-backed skills require a reviewed patch flow"
		log.Printf("[evolution-pipeline] optimize skipped skill=%s reason=%s", req.SkillName, reason)
		return OptimizeResult{Skipped: true, SkipReason: reason}
	}

	// Throttle: don't call LLM if we already attempted optimization for this
	// skill within the cooldown period (even if it was rejected/failed).
	// Manual force bypasses the attempt throttle and does NOT write a
	// timestamp — 手动触发不应重置 24h 自动窗口。
	if !force {
		p.throttleMu.Lock()
		if lastAttempt, ok := p.optimizeAttempts[req.SkillName]; ok {
			if time.Since(lastAttempt).Hours() < 24 {
				p.throttleMu.Unlock()
				reason := "optimization throttled (24h cooldown); pass force=true to override"
				log.Printf("[evolution-pipeline] optimize skipped skill=%s reason=%s", req.SkillName, reason)
				return OptimizeResult{Skipped: true, SkipReason: reason}
			}
		}
		p.optimizeAttempts[req.SkillName] = time.Now()
		p.throttleMu.Unlock()
	}

	log.Printf("[evolution-pipeline] optimizing skill=%s force=%v", req.SkillName, force)

	historicalArgs := p.UsageTracker.RecentRunArgs(trackerName, 3)
	result, err := p.Optimizer.Optimize(ctx, req.Entry, records, historicalArgs)
	if err != nil {
		log.Printf("[evolution-pipeline] optimization failed for skill=%s: %v", req.SkillName, err)
		return OptimizeResult{Attempted: true, Explanation: err.Error()}
	}

	if !result.Optimized {
		log.Printf("[evolution-pipeline] optimization not applicable for skill=%s: %s", req.SkillName, result.Explanation)
		return OptimizeResult{Attempted: true, Optimized: false, Explanation: result.Explanation}
	}

	// Keep an in-memory snapshot as well: ApplyOptimization mutates req.Entry
	// before durable persistence, so a failed commit must not leave the worker
	// carrying an unapplied candidate after YAML/config rollback.
	originalEntry := CloneNLSkillEntry(req.Entry)
	// Apply optimization.
	if ApplyOptimization(req.Entry, result, p.Versioner) {
		// Persist atomically: SkillSaver's closure holds the write lock, so
		// we pass the full updated list inside one call. Only the fields the
		// optimization actually modified are merged into the freshly loaded
		// entry (mergeEvolvedEntry), preserving live usage stats.
		//
		// When the optimization cannot be persisted (no SkillSaver, or the
		// skill is missing from storage) nothing durable changed — log and
		// return WITHOUT emitting EventSkillOptimized or triggering upload,
		// so the frontend/audit never claim an optimization that didn't stick.
		if p.SkillSaver == nil || p.SkillLoader == nil {
			if originalEntry != nil {
				*req.Entry = *originalEntry
			}
			log.Printf("[evolution-pipeline] optimization not persisted skill=%s: persistence not configured", req.SkillName)
			return OptimizeResult{Attempted: true, Explanation: "optimization not persisted: persistence not configured"}
		}
		var skills []corelib.NLSkillEntry
		if p.SkillLoader != nil {
			skills = p.SkillLoader()
		}
		found := false
		for i := range skills {
			if skills[i].Name == req.SkillName {
				mergeEvolvedEntry(&skills[i], req.Entry)
				found = true
				break
			}
		}
		if !found {
			if originalEntry != nil {
				*req.Entry = *originalEntry
			}
			log.Printf("[evolution-pipeline] optimization not persisted skill=%s: not found in storage", req.SkillName)
			return OptimizeResult{Attempted: true, Explanation: "skill not found in storage, optimization not persisted"}
		}
		commitAudit := map[string]string{
			"skill": req.SkillName, "action": "optimize", "decision": "committed",
			"request_id": req.RequestID, "attempt": "1", "config_revision": p.configRevision(),
			"schema_version": "2", "evidence_mode": "real",
		}
		commit := p.persistDefinitionChangeWithAudit(ctx, req.SkillName, req.Entry, "skill:optimization_commit", commitAudit)
		if commit.State != "committed" || commit.CleanupStatus != "clear" {
			if originalEntry != nil {
				*req.Entry = *originalEntry
			}
			log.Printf("[evolution-pipeline] optimization commit %s skill=%s reason=%s", commit.State, req.SkillName, commit.FailureReason)
			p.emitRequestEvent(EventSkillEvolutionRolledBack, req, map[string]string{
				"decision": commit.State, "reason": "optimization_persistence_failed", "failure_reason": commit.FailureReason,
			})
			return OptimizeResult{Attempted: true, Explanation: "optimization not committed: " + commit.FailureReason}
		}

		log.Printf("[evolution-pipeline] optimization applied for skill=%s, triggering upload check", req.SkillName)

		// Notify frontend that a skill was optimized.
		p.emitRequestEvent(EventSkillOptimized, req, map[string]string{
			"explanation": result.Explanation, "decision": "applied", "reason": "optimization_gate_passed",
		})

		// Trigger auto-upload.
		if p.UploadTrigger != nil {
			p.UploadTrigger(req.SkillName, &SkillExecutionResultCompat{
				Success:       true,
				OutputQuality: "good",
			})
		}
		return OptimizeResult{Attempted: true, Optimized: true, Explanation: result.Explanation}
	}
	return OptimizeResult{Attempted: true, Optimized: false, Explanation: result.Explanation}
}

func (p *EvolutionPipeline) tryPromoteNudges(ctx context.Context) {
	candidates := p.UsageTracker.DistillSkillNudgeCandidates(30, 3)
	promotable := FilterPromotable(candidates, p.Promoter.Threshold)

	for _, candidate := range promotable {
		if ctx.Err() != nil {
			return
		}

		// Determine the skill name that TryPromote will use.
		checkName := candidate.SuggestedName
		if checkName == "" {
			checkName = generatePromotedSkillName(candidate)
		}

		// Check if skill with this name already exists.
		if p.skillExists(checkName) {
			continue
		}

		// Throttle: skip candidates that were recently rejected/blocked.
		p.throttleMu.Lock()
		if blockedAt, ok := p.promoteBlacklist[candidate.ContextKey]; ok {
			if time.Since(blockedAt).Hours() < 24 {
				p.throttleMu.Unlock()
				continue
			}
			delete(p.promoteBlacklist, candidate.ContextKey)
		}
		p.throttleMu.Unlock()

		result, err := p.Promoter.TryPromote(candidate)
		if err != nil {
			log.Printf("[evolution-pipeline] promotion failed for %s: %v", checkName, err)
			// Blacklist on error to prevent repeated LLM calls.
			p.throttleMu.Lock()
			p.promoteBlacklist[candidate.ContextKey] = time.Now()
			p.throttleMu.Unlock()
			continue
		}
		if result.Blocked {
			// Security scan blocked — blacklist to prevent repeated attempts.
			p.throttleMu.Lock()
			p.promoteBlacklist[candidate.ContextKey] = time.Now()
			p.throttleMu.Unlock()
			continue
		}
		if result.Promoted {
			log.Printf("[evolution-pipeline] promoted new skill: %s", result.SkillName)
			// Notify frontend about the new auto-discovered skill.
			if p.EventEmitter != nil {
				p.EventEmitter(EventSkillAutoDiscovered, map[string]string{
					"skill":       result.SkillName,
					"explanation": result.Explanation,
					"status":      "staged",
					"via":         "auto_discovery",
				})
			}
			// Newly discovered skills are staged; upload is intentionally deferred
			// until explicit approval and runtime proof are available.
		}
	}
}

func (p *EvolutionPipeline) skillExists(name string) bool {
	if p.SkillLoader == nil || name == "" {
		return false
	}
	for _, s := range p.SkillLoader() {
		if s.Name == name {
			return true
		}
	}
	return false
}
