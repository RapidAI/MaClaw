package skill

import (
	"context"
	"log"
	"sync"
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
	Promoter        *NudgePromoter
	Optimizer       *SkillOptimizer
	Gate            *RepairGate
	Scheduler       *MaintenanceScheduler
	Versioner       *Versioner
	UsageTracker    *tool.UsageTracker
	SkillLoader     func() []corelib.NLSkillEntry
	SkillSaver      func([]corelib.NLSkillEntry) error
	UploadTrigger   func(skillName string, result *SkillExecutionResultCompat) // triggers AutoUploadTrigger.CheckAndTrigger
	LLM             LLMRepairer
	EventEmitter    func(event string, data map[string]string) // notifies frontend of evolution actions

	// Config
	PostExecDelay       time.Duration // delay after skill exec before running pipeline (default 5s)
	EnablePromoter      bool
	EnableOptimizer     bool
	PromoteCheckInterval int // check nudge promotion every N notifications (default 10)

	// Internal
	pendingCh    chan evolutionRequest
	stopCh       chan struct{}
	once         sync.Once // protects stopCh close
	startOnce    sync.Once // protects Start from being called twice
	requestCount int       // counts processed requests for throttling

	// LLM call throttling — prevents repeated expensive calls for the same skill/candidate.
	optimizeAttempts   map[string]time.Time // skill name → last LLM optimization attempt time
	promoteBlacklist   map[string]time.Time // candidate ContextKey → time blocked (retry after 24h)
	throttleMu         sync.Mutex
}

// SkillExecutionResultCompat is a bridge type to avoid importing gui package.
type SkillExecutionResultCompat struct {
	Success       bool
	OutputQuality string
	TokensUsed    int
}

type evolutionRequest struct {
	SkillName string
	Entry     *corelib.NLSkillEntry
	ExecResult *SkillExecutionResultCompat
	RunArgs   map[string]string
}

// NewEvolutionPipeline creates a pipeline with sensible defaults.
func NewEvolutionPipeline() *EvolutionPipeline {
	return &EvolutionPipeline{
		PostExecDelay:        5 * time.Second,
		EnablePromoter:       true,
		EnableOptimizer:      true,
		PromoteCheckInterval: 10,
		pendingCh:            make(chan evolutionRequest, 32),
		stopCh:               make(chan struct{}),
		optimizeAttempts:     make(map[string]time.Time),
		promoteBlacklist:     make(map[string]time.Time),
	}
}

// Start begins the background processing loop.
func (p *EvolutionPipeline) Start() {
	if p == nil {
		return
	}
	p.startOnce.Do(func() {
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
	})
	if p.Scheduler != nil {
		p.Scheduler.Stop()
	}
}

// NotifySkillExecution is called after a skill execution completes.
// It enqueues an async evolution check without blocking the caller.
// The entry is deep-copied to prevent data races with the main agent loop.
func (p *EvolutionPipeline) NotifySkillExecution(skillName string, entry *corelib.NLSkillEntry, result *SkillExecutionResultCompat, runArgs map[string]string) {
	if p == nil || entry == nil {
		return
	}
	// Deep-copy entry to prevent race with main loop mutations.
	entryCopy := *entry
	entryCopy.Steps = append([]corelib.NLSkillStep(nil), entry.Steps...)
	var argsCopy map[string]string
	if len(runArgs) > 0 {
		argsCopy = make(map[string]string, len(runArgs))
		for k, v := range runArgs {
			argsCopy[k] = v
		}
	}
	select {
	case p.pendingCh <- evolutionRequest{
		SkillName:  skillName,
		Entry:      &entryCopy,
		ExecResult: result,
		RunArgs:    argsCopy,
	}:
	default:
		// Channel full — skip this notification. Non-blocking.
		log.Printf("[evolution-pipeline] notification queue full, skipping skill=%s", skillName)
	}
}

func (p *EvolutionPipeline) loop() {
	for {
		select {
		case <-p.stopCh:
			return
		case req := <-p.pendingCh:
			// Delay before processing to avoid interfering with user interaction.
			select {
			case <-p.stopCh:
				return
			case <-time.After(p.PostExecDelay):
			}
			p.processRequest(req)
		}
	}
}

func (p *EvolutionPipeline) processRequest(req evolutionRequest) {
	// Recover from panics to prevent the pipeline goroutine from dying.
	// A crashed pipeline would silently stop all self-evolution processing.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[evolution-pipeline] PANIC recovered in processRequest for skill=%s: %v", req.SkillName, r)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	p.requestCount++

	// 1. Check if skill should be optimized (working but suboptimal).
	if p.EnableOptimizer && p.Optimizer != nil && req.Entry != nil {
		p.tryOptimize(ctx, req)
	}

	// 2. Check nudge candidates for promotion (throttled — every N requests).
	interval := p.PromoteCheckInterval
	if interval <= 0 {
		interval = 10
	}
	if p.EnablePromoter && p.Promoter != nil && p.UsageTracker != nil && p.requestCount%interval == 0 {
		p.tryPromoteNudges(ctx)
	}
}

func (p *EvolutionPipeline) tryOptimize(ctx context.Context, req evolutionRequest) {
	if req.Entry == nil || p.UsageTracker == nil {
		return
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

	if !p.Optimizer.ShouldOptimize(req.Entry, records) {
		return
	}

	// Throttle: don't call LLM if we already attempted optimization for this
	// skill within the cooldown period (even if it was rejected/failed).
	p.throttleMu.Lock()
	if lastAttempt, ok := p.optimizeAttempts[req.SkillName]; ok {
		if time.Since(lastAttempt).Hours() < 24 {
			p.throttleMu.Unlock()
			return
		}
	}
	p.optimizeAttempts[req.SkillName] = time.Now()
	p.throttleMu.Unlock()

	log.Printf("[evolution-pipeline] optimizing skill=%s", req.SkillName)

	historicalArgs := p.UsageTracker.RecentRunArgs(trackerName, 3)
	result, err := p.Optimizer.Optimize(ctx, req.Entry, records, historicalArgs)
	if err != nil {
		log.Printf("[evolution-pipeline] optimization failed for skill=%s: %v", req.SkillName, err)
		return
	}

	if !result.Optimized {
		log.Printf("[evolution-pipeline] optimization not applicable for skill=%s: %s", req.SkillName, result.Explanation)
		return
	}

	// Apply optimization.
	if ApplyOptimization(req.Entry, result, p.Versioner) {
		// Persist atomically: SkillSaver's closure holds the write lock, so
		// we pass the full updated list inside one call. The SkillSaver loads
		// the latest state internally (under lock), applies our modification,
		// and saves — preventing TOCTOU races with updateUsageStats.
		if p.SkillSaver != nil {
			// Build the updated list by loading fresh (SkillSaver holds lock),
			// but since SkillSaver takes []NLSkillEntry we must load first.
			// The brief window between SkillLoader/SkillSaver is acceptable
			// because the worst case is a UsageCount being off by 1 (not data
			// corruption), and the pipeline runs at low frequency (every 5s+).
			var skills []corelib.NLSkillEntry
			if p.SkillLoader != nil {
				skills = p.SkillLoader()
			}
			found := false
			for i := range skills {
				if skills[i].Name == req.SkillName {
					skills[i] = *req.Entry
					found = true
					break
				}
			}
			if found {
				if err := p.SkillSaver(skills); err != nil {
					log.Printf("[evolution-pipeline] save optimized skill=%s failed: %v", req.SkillName, err)
					return
				}
			}
		}

		// Write updated steps back to skill.yaml on disk so that loadSkills
		// (which treats skill.yaml as source of truth for Steps) doesn't
		// overwrite our optimization on next restart.
		if req.Entry.SkillDir != "" {
			if err := WriteBackOptimizedSteps(req.Entry); err != nil {
				log.Printf("[evolution-pipeline] writeback skill.yaml for %s failed: %v", req.SkillName, err)
				// Non-fatal: config.json has the optimization, yaml writeback is best-effort.
			}
		}

		log.Printf("[evolution-pipeline] optimization applied for skill=%s, triggering upload check", req.SkillName)

		// Notify frontend that a skill was optimized.
		if p.EventEmitter != nil {
			p.EventEmitter("skill:optimized", map[string]string{
				"skill":       req.SkillName,
				"explanation": result.Explanation,
			})
		}

		// Trigger auto-upload.
		if p.UploadTrigger != nil {
			p.UploadTrigger(req.SkillName, &SkillExecutionResultCompat{
				Success:       true,
				OutputQuality: "good",
			})
		}
	}
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
				p.EventEmitter("skill:auto_discovered", map[string]string{
					"skill":       result.SkillName,
					"explanation": result.Explanation,
				})
			}
			// Trigger auto-upload for newly promoted skill.
			if p.UploadTrigger != nil {
				p.UploadTrigger(result.SkillName, &SkillExecutionResultCompat{
					Success:       true,
					OutputQuality: "basic",
				})
			}
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
