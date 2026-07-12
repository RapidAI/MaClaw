package memory

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// PipelineResult holds the combined outcome of a full maintenance cycle.
type PipelineResult struct {
	Compress      *CompressResult               `json:"compress,omitempty"`
	Promote       *PromoteResult                `json:"promote,omitempty"`
	Reflect       *ReflectResult                `json:"reflect,omitempty"`
	Experience    *ExperienceDistillResult      `json:"experience,omitempty"`
	Consolidation []ConsolidationResult         `json:"consolidation,omitempty"`
	Profile       *ConsolidationResult          `json:"profile,omitempty"`
	Profiles      []ConsolidationResult         `json:"profiles,omitempty"`
	Dormant       int                           `json:"dormant_marked"`
	Candidates    *CandidateConsolidationResult `json:"candidates,omitempty"`
	LLMCallsUsed  int                           `json:"llm_calls_used,omitempty"`
	LLMCallsLeft  int                           `json:"llm_calls_left,omitempty"`
	LLMBudgetHit  bool                          `json:"llm_budget_hit,omitempty"`
	Duration      string                        `json:"duration"`
}

const defaultPipelineLLMCallBudget = 3

// PromoteResult holds the outcome of an episodic-to-semantic promotion run.
type PromoteResult struct {
	Promoted int    `json:"promoted"`
	Error    string `json:"error,omitempty"`
}

// ReflectResult holds the outcome of a reflection run.
type ReflectResult struct {
	InsightsGenerated int    `json:"insights_generated"`
	Error             string `json:"error,omitempty"`
}

// Pipeline orchestrates the background memory maintenance cycle:
//
//	decay strengths -> compress -> synthesize -> consolidate -> profile -> themes
//
// It runs every 6 hours when started.
type Pipeline struct {
	store        *Store
	compressor   *Compressor
	synthesizer  *Synthesizer
	experience   *ExperienceDistiller
	consolidator *Consolidator
	profiler     *ProfileConsolidator
	emitter      corelib.EventEmitter

	mu           sync.Mutex
	runMu        sync.Mutex
	running      bool
	ctx          context.Context
	cancelFn     context.CancelFunc
	triggerTimer *time.Timer
	lastRun      time.Time
	lastResult   *PipelineResult
	startDelay   time.Duration
}

// NewPipeline creates a Pipeline. Any component can be nil (skipped).
// The second and third positional parameters are deprecated (formerly Promoter
// and Reflector) - use SetSynthesizer to wire the combined module.
func NewPipeline(store *Store, compressor *Compressor, _ interface{}, _ interface{}, emitter corelib.EventEmitter) *Pipeline {
	return &Pipeline{
		store:      store,
		compressor: compressor,
		experience: NewExperienceDistiller(),
		emitter:    emitter,
	}
}

// SetSynthesizer wires the combined Promoter+Reflector module.
func (p *Pipeline) SetSynthesizer(s *Synthesizer) {
	p.mu.Lock()
	p.synthesizer = s
	p.mu.Unlock()
}

// RunOnce executes one full maintenance cycle synchronously.
func (p *Pipeline) RunOnce(ctx context.Context) *PipelineResult {
	if p == nil {
		return &PipelineResult{}
	}
	p.runMu.Lock()
	defer p.runMu.Unlock()
	start := time.Now()
	result := &PipelineResult{}
	if ctx == nil {
		ctx = context.Background()
	}
	// Pipeline can be constructed before the store is ready (tests / early boot).
	// Never touch p.store without a nil check — a nil store previously panic'd the
	// background QoS runner and flaked the whole gui test package.
	if p.store == nil {
		result.Duration = fmt.Sprintf("%.1fs", time.Since(start).Seconds())
		log.Printf("[pipeline] skip RunOnce: store is nil")
		return result
	}
	budget := NewLLMCallBudget(defaultPipelineLLMCallBudget)
	ctx = WithLLMCallBudget(ctx, budget)
	defer func() {
		used, left, exhausted := budget.Snapshot()
		result.LLMCallsUsed = used
		result.LLMCallsLeft = left
		result.LLMBudgetHit = exhausted || left <= 0
		if result.LLMBudgetHit {
			log.Printf("[pipeline] LLM budget exhausted used=%d left=%d", used, left)
		}
		log.Printf("[pipeline] done duration=%s llm_calls_used=%d llm_calls_left=%d llm_budget_hit=%v", result.Duration, used, left, result.LLMBudgetHit)
	}()

	// Step 0: Decay strengths and mark dormant entries.
	p.store.mu.RLock()
	dormantUpdates := dormantDecayUpdates(p.store.entries, time.Now())
	p.store.mu.RUnlock()
	if len(dormantUpdates) > 0 {
		if err := p.store.updateMetadataEntriesByID(dormantUpdates); err != nil {
			log.Printf("[pipeline] persist dormant decay: %v", err)
		} else {
			result.Dormant = len(dormantUpdates)
		}
	}

	// Step 1: Process pending semantic dedup pairs (embedding recall -> LLM judgment).
	if ctx.Err() == nil {
		merged := p.store.ProcessPendingDedup(ctx)
		if merged > 0 {
			log.Printf("[pipeline] semantic dedup merged %d entries", merged)
		}
	}

	// Step 1a: Revisit quarantined memory candidates before lossy maintenance.
	if ctx.Err() == nil {
		candidates := p.store.ConsolidateMemoryCandidates(ctx)
		if candidates.Scanned > 0 {
			result.Candidates = &candidates
			if candidates.Promoted > 0 || candidates.Merged > 0 || candidates.Rejected > 0 {
				log.Printf("[pipeline] memory candidates: scanned=%d promoted=%d merged=%d rejected=%d kept=%d",
					candidates.Scanned, candidates.Promoted, candidates.Merged, candidates.Rejected, candidates.Kept)
			}
		}
	}

	// Step 1b: Classify the experience mix before lossy maintenance steps.
	// This is non-mutating in Phase 1 and gives later LLM-backed distillation a
	// safe gating surface for large trace batches.
	if p.experience != nil && ctx.Err() == nil {
		p.store.mu.RLock()
		entries := make([]Entry, len(p.store.entries))
		copy(entries, p.store.entries)
		p.store.mu.RUnlock()
		experience := p.experience.Analyze(entries)
		result.Experience = &experience
	}
	var protectedSamples []ProtectedExperienceCandidate
	if result.Experience != nil {
		protectedSamples = result.Experience.ProtectedSamples
	}
	if p.compressor != nil {
		p.compressor.SetExperienceProtectionSamples(protectedSamples)
	}
	if p.synthesizer != nil {
		p.synthesizer.SetExperienceProtectionSamples(protectedSamples)
	}

	// Step 2: Compress (dedup + LLM compress).
	if p.compressor != nil && ctx.Err() == nil {
		cr, err := p.compressor.Compress(ctx)
		if err == nil {
			result.Compress = cr
		} else if errors.Is(err, ErrLLMCallBudgetExhausted) {
			log.Printf("[pipeline] compress stopped: %v", err)
		}
	}

	// Step 3: Synthesize (combined promotion + reflection in a single LLM call).
	if p.synthesizer != nil && ctx.Err() == nil && !llmCallBudgetDepleted(ctx) {
		sr, err := p.synthesizer.Synthesize(ctx)
		if err != nil {
			log.Printf("[pipeline] synthesize error: %v", err)
		} else if sr != nil {
			result.Promote = &PromoteResult{Promoted: sr.Promoted, Error: sr.Error}
			result.Reflect = &ReflectResult{InsightsGenerated: sr.InsightsGenerated, Error: sr.Error}
			if sr.Promoted > 0 || sr.InsightsGenerated > 0 {
				log.Printf("[pipeline] synthesize: promoted=%d insights=%d", sr.Promoted, sr.InsightsGenerated)
			}
		}
	}

	// Step 4: Stratified consolidation (L2-L5).
	// In multi-tenant mode, run consolidation per-user to maintain isolation.
	if p.consolidator != nil && ctx.Err() == nil && !llmCallBudgetDepleted(ctx) {
		// Get all unique owner IDs (users with memories).
		ownerIDs := p.store.UniqueOwnerIDs()

		if len(ownerIDs) == 0 {
			// Single-user mode or no user-specific memories: run with empty ownerID.
			cr := p.consolidator.RunScheduledConsolidation(ctx, time.Now(), "")
			if len(cr) > 0 {
				result.Consolidation = cr
			}
		} else {
			// Multi-tenant mode: run consolidation for each user separately.
			var allResults []ConsolidationResult
			for _, ownerID := range ownerIDs {
				if ctx.Err() != nil {
					break
				}
				cr := p.consolidator.RunScheduledConsolidation(ctx, time.Now(), ownerID)
				allResults = append(allResults, cr...)
			}
			if len(allResults) > 0 {
				result.Consolidation = allResults
			}
		}
	}

	// Step 5: Profile consolidation (L5 persona). In multi-tenant mode, profile
	// synthesis is also owner-scoped; otherwise one user's persona can absorb
	// another user's summaries and reflections.
	if p.profiler != nil && ctx.Err() == nil && !llmCallBudgetDepleted(ctx) {
		ownerIDs := p.store.UniqueOwnerIDs()
		if len(ownerIDs) == 0 {
			ownerIDs = []string{""}
		}
		for _, ownerID := range ownerIDs {
			if ctx.Err() != nil {
				break
			}
			pr, err := p.profiler.ConsolidateForOwner(ctx, ownerID)
			if err == nil && pr != nil && pr.NodesCreated > 0 {
				if result.Profile == nil {
					result.Profile = pr
				}
				result.Profiles = append(result.Profiles, *pr)
			}
		}
	}

	// Step 6: Rebuild the embedding-aware theme layer from current entries.
	if p.store.themeManager != nil && ctx.Err() == nil {
		p.store.mu.RLock()
		entries := make([]Entry, len(p.store.entries))
		copy(entries, p.store.entries)
		p.store.mu.RUnlock()

		themes := p.store.themeManager.RebuildContext(ctx, entries, nil)
		if len(themes) > 0 {
			log.Printf("[pipeline] theme layer: %d themes discovered", len(themes))
		}
	}

	result.Duration = fmt.Sprintf("%.1fs", time.Since(start).Seconds())

	p.mu.Lock()
	p.lastRun = time.Now()
	p.lastResult = result
	p.mu.Unlock()

	if p.emitter != nil {
		p.emitter.Emit("memory:pipeline_completed", result)
	}

	return result
}

// Start begins the background maintenance loop (every 6 hours).
func (p *Pipeline) Start() {
	p.start(0)
}

// StartDelayed begins the background loop after an initial delay. The periodic
// cadence is unchanged after the first cycle.
func (p *Pipeline) StartDelayed(initialDelay time.Duration) {
	p.start(initialDelay)
}

func (p *Pipeline) start(initialDelay time.Duration) {
	if initialDelay < 0 {
		initialDelay = 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running {
		return
	}
	p.running = true
	p.startDelay = initialDelay
	ctx, cancel := context.WithCancel(context.Background())
	p.ctx = ctx
	p.cancelFn = cancel
	go p.loop(ctx)
}

// Stop halts the background loop.
func (p *Pipeline) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.running {
		return
	}
	if p.triggerTimer != nil {
		p.triggerTimer.Stop()
		p.triggerTimer = nil
	}
	p.cancelFn()
	p.running = false
	p.ctx = nil
}

// TriggerSoon schedules one maintenance cycle after an idle debounce window.
// Repeated calls reset the timer, so bursts of memory writes collapse into a
// single background pass.
func (p *Pipeline) TriggerSoon(delay time.Duration) {
	if p == nil {
		return
	}
	if delay < 0 {
		delay = 0
	}
	p.mu.Lock()
	if p.triggerTimer != nil {
		p.triggerTimer.Stop()
	}
	p.triggerTimer = time.AfterFunc(delay, func() {
		p.mu.Lock()
		ctx := p.ctx
		p.triggerTimer = nil
		p.mu.Unlock()
		if ctx == nil {
			ctx = context.Background()
		}
		p.RunOnce(ctx)
	})
	p.mu.Unlock()
}

func (p *Pipeline) loop(ctx context.Context) {
	initialDelay := p.initialDelay()
	if initialDelay > 0 {
		timer := time.NewTimer(initialDelay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-timer.C:
		}
	}
	// Run immediately after the optional startup delay.
	p.RunOnce(ctx)

	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.RunOnce(ctx)
		}
	}
}

func (p *Pipeline) initialDelay() time.Duration {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.startDelay
}

func llmCallBudgetDepleted(ctx context.Context) bool {
	budget, ok := LLMCallBudgetFromContext(ctx)
	return ok && budget.Depleted()
}

// Status returns the last run info.
func (p *Pipeline) Status() (running bool, lastRun time.Time, lastResult *PipelineResult) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running, p.lastRun, p.lastResult
}

// SetConsolidator wires optional TiMem consolidation components into the pipeline.
func (p *Pipeline) SetConsolidator(consolidator *Consolidator, profiler *ProfileConsolidator) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.consolidator = consolidator
	p.profiler = profiler
}

// SetLLM rewires every LLM-backed evolution component owned by the pipeline.
// The pipeline is created during memory-store initialization, while user LLM
// configuration may arrive later or change at runtime.
func (p *Pipeline) SetLLM(llm LLMChatCaller) {
	p.mu.Lock()
	compressor := p.compressor
	synthesizer := p.synthesizer
	consolidator := p.consolidator
	profiler := p.profiler
	p.mu.Unlock()

	if compressor != nil {
		compressor.SetLLM(llm)
	}
	if synthesizer != nil {
		synthesizer.SetLLM(llm)
	}
	if consolidator != nil {
		consolidator.SetLLM(llm)
	}
	if profiler != nil {
		profiler.SetLLM(llm)
	}
}
