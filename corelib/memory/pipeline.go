package memory

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// PipelineResult holds the combined outcome of a full maintenance cycle.
type PipelineResult struct {
	Compress      *CompressResult          `json:"compress,omitempty"`
	Promote       *PromoteResult           `json:"promote,omitempty"`
	Reflect       *ReflectResult           `json:"reflect,omitempty"`
	Experience    *ExperienceDistillResult `json:"experience,omitempty"`
	Consolidation []ConsolidationResult    `json:"consolidation,omitempty"`
	Profile       *ConsolidationResult     `json:"profile,omitempty"`
	Profiles      []ConsolidationResult    `json:"profiles,omitempty"`
	Dormant       int                      `json:"dormant_marked"`
	Duration      string                   `json:"duration"`
}

// Pipeline orchestrates the background memory maintenance cycle:
//
//	decay strengths -> compress -> promote -> reflect
//
// It runs every 6 hours when started.
type Pipeline struct {
	store        *Store
	compressor   *Compressor
	promoter     *Promoter
	reflector    *Reflector
	experience   *ExperienceDistiller
	consolidator *Consolidator
	profiler     *ProfileConsolidator
	emitter      corelib.EventEmitter

	mu         sync.Mutex
	running    bool
	cancelFn   context.CancelFunc
	lastRun    time.Time
	lastResult *PipelineResult
}

// NewPipeline creates a Pipeline. Any component can be nil (skipped).
func NewPipeline(store *Store, compressor *Compressor, promoter *Promoter, reflector *Reflector, emitter corelib.EventEmitter) *Pipeline {
	return &Pipeline{
		store:      store,
		compressor: compressor,
		promoter:   promoter,
		reflector:  reflector,
		experience: NewExperienceDistiller(),
		emitter:    emitter,
	}
}

// RunOnce executes one full maintenance cycle synchronously.
func (p *Pipeline) RunOnce(ctx context.Context) *PipelineResult {
	start := time.Now()
	result := &PipelineResult{}

	// Step 0: Decay strengths and mark dormant entries.
	p.store.mu.Lock()
	result.Dormant = batchDecayAndMark(p.store.entries, time.Now())
	if result.Dormant > 0 {
		p.store.rebuildDerivedIndexesLocked(true)
		p.store.dirty = true
	}
	p.store.mu.Unlock()
	if result.Dormant > 0 {
		p.store.signalSave()
	}

	// Step 1: Process pending semantic dedup pairs (embedding recall -> LLM judgment).
	if ctx.Err() == nil {
		merged := p.store.ProcessPendingDedup(ctx)
		if merged > 0 {
			log.Printf("[pipeline] semantic dedup merged %d entries", merged)
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
	if p.promoter != nil {
		p.promoter.SetExperienceProtectionSamples(protectedSamples)
	}

	// Step 2: Compress (dedup + LLM compress).
	if p.compressor != nil && ctx.Err() == nil {
		cr, err := p.compressor.Compress(ctx)
		if err == nil {
			result.Compress = cr
		}
	}

	// Step 2: Promote (episodic -> semantic).
	if p.promoter != nil && ctx.Err() == nil {
		pr, err := p.promoter.Promote(ctx)
		if err == nil {
			result.Promote = pr
		}
	}

	// Step 3: Reflect (generate insights).
	if p.reflector != nil && ctx.Err() == nil {
		rr, err := p.reflector.Reflect(ctx)
		if err == nil {
			result.Reflect = rr
		}
	}

	// Step 4: Stratified consolidation (L2-L5).
	// In multi-tenant mode, run consolidation per-user to maintain isolation.
	if p.consolidator != nil && ctx.Err() == nil {
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
	if p.profiler != nil && ctx.Err() == nil {
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

	// Step 6: Topic clustering (inspired by Graphiti Community Subgraph).
	// Rebuild topic clusters from current entries. This is lightweight (no LLM)
	// and provides community-like summaries for global context.
	if p.store.topicClusterer != nil && ctx.Err() == nil {
		p.store.mu.RLock()
		entries := make([]Entry, len(p.store.entries))
		copy(entries, p.store.entries)
		p.store.mu.RUnlock()

		clusters := p.store.topicClusterer.Cluster(entries)

		// Generate summaries for clusters (uses LLM if available).
		if p.compressor != nil && p.compressor.llm != nil {
			clusters = p.store.topicClusterer.GenerateSummaries(clusters, entries, p.compressor.llm)
		}

		if len(clusters) > 0 {
			log.Printf("[pipeline] topic clustering: %d clusters discovered", len(clusters))
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
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running {
		return
	}
	p.running = true
	ctx, cancel := context.WithCancel(context.Background())
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
	p.cancelFn()
	p.running = false
}

func (p *Pipeline) loop(ctx context.Context) {
	// Run immediately on start.
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
	promoter := p.promoter
	reflector := p.reflector
	consolidator := p.consolidator
	profiler := p.profiler
	p.mu.Unlock()

	if compressor != nil {
		compressor.SetLLM(llm)
	}
	if promoter != nil {
		promoter.SetLLM(llm)
	}
	if reflector != nil {
		reflector.SetLLM(llm)
	}
	if consolidator != nil {
		consolidator.SetLLM(llm)
	}
	if profiler != nil {
		profiler.SetLLM(llm)
	}
}
