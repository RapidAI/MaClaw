package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// PipelineResult holds the combined outcome of a full maintenance cycle.
type PipelineResult struct {
	Compress      *CompressResult       `json:"compress,omitempty"`
	Promote       *PromoteResult        `json:"promote,omitempty"`
	Reflect       *ReflectResult        `json:"reflect,omitempty"`
	Consolidation []ConsolidationResult `json:"consolidation,omitempty"`
	Profile       *ConsolidationResult  `json:"profile,omitempty"`
	Dormant       int                   `json:"dormant_marked"`
	Duration      string                `json:"duration"`
}

// Pipeline orchestrates the background memory maintenance cycle:
//
//	decay strengths → compress → promote → reflect
//
// It runs every 6 hours when started.
type Pipeline struct {
	store        *Store
	compressor   *Compressor
	promoter     *Promoter
	reflector    *Reflector
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
		p.store.dirty = true
	}
	p.store.mu.Unlock()
	if result.Dormant > 0 {
		p.store.signalSave()
	}

	// Step 1: Compress (dedup + LLM compress).
	if p.compressor != nil && ctx.Err() == nil {
		cr, err := p.compressor.Compress(ctx)
		if err == nil {
			result.Compress = cr
		}
	}

	// Step 2: Promote (episodic → semantic).
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
	if p.consolidator != nil && ctx.Err() == nil {
		cr := p.consolidator.RunScheduledConsolidation(ctx, time.Now())
		if len(cr) > 0 {
			result.Consolidation = cr
		}
	}

	// Step 5: Profile consolidation (L5 persona).
	if p.profiler != nil && ctx.Err() == nil {
		pr, err := p.profiler.Consolidate(ctx)
		if err == nil && pr != nil && pr.NodesCreated > 0 {
			result.Profile = pr
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
