package skill

import (
	"context"
	"log"
	"os"
	"path/filepath"
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
	Promoter      *NudgePromoter
	Optimizer     *SkillOptimizer
	Gate          *RepairGate
	Scheduler     *MaintenanceScheduler
	Versioner     *Versioner
	UsageTracker  *tool.UsageTracker
	SkillLoader   func() []corelib.NLSkillEntry
	SkillSaver    func([]corelib.NLSkillEntry) error
	UploadTrigger func(skillName string, result *SkillExecutionResultCompat) // enqueues upload via SkillLifecycleManager (subject to skill_auto_upload_enabled)
	LLM           LLMRepairer
	EventEmitter  func(event string, data map[string]string) // notifies frontend of evolution actions

	// Config
	PostExecDelay        time.Duration // delay after skill exec before running pipeline (default 5s)
	EnablePromoter       bool
	EnableOptimizer      bool
	EnableRepair         bool          // schedule self-repair for failed skills (default true)
	RepairCooldown       time.Duration // min interval between LLM repair attempts per skill (default 1h)
	PromoteCheckInterval int           // check nudge promotion every N notifications (default 10)

	// RepairHook is the platform-specific repair applier (LLM + gate + security +
	// persist). When set, processRequest delegates failed-skill repair here so
	// GUI can enforce security scans. When nil, a core-only repair path runs
	// (ApplyRepair + SkillSaver) suitable for headless/tests.
	RepairHook func(entry *corelib.NLSkillEntry, runArgs map[string]string)

	// Internal
	// pendingBySkill coalesces notifications by skill name: rapid re-runs of the
	// same skill keep only the latest request instead of dropping when busy.
	pendingBySkill map[string]evolutionRequest
	pendingMu      sync.Mutex
	pendingWake    chan struct{} // buffer 1 — wake the loop without blocking Notify
	stopCh         chan struct{}
	once           sync.Once    // protects stopCh close
	startOnce      sync.Once    // protects Start from being called twice
	requestCount   atomic.Int64 // counts processed requests for throttling

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

// SkillExecutionResultCompat is a bridge type to avoid importing gui package.
type SkillExecutionResultCompat struct {
	Success       bool
	OutputQuality string
	TokensUsed    int
}

type evolutionRequest struct {
	SkillName  string
	Entry      *corelib.NLSkillEntry
	ExecResult *SkillExecutionResultCompat
	RunArgs    map[string]string
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
	return &EvolutionPipeline{
		PostExecDelay:        5 * time.Second,
		EnablePromoter:       true,
		EnableOptimizer:      true,
		EnableRepair:         true,
		RepairCooldown:       DefaultRepairCooldown,
		PromoteCheckInterval: 10,
		pendingBySkill:       make(map[string]evolutionRequest),
		pendingWake:          make(chan struct{}, 1),
		stopCh:               make(chan struct{}),
		optimizeAttempts:     make(map[string]time.Time),
		repairAttempts:       make(map[string]time.Time),
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
		SkillName:  skillName,
		Entry:      entryCopy,
		ExecResult: resultCopy,
		RunArgs:    argsCopy,
	}

	p.pendingMu.Lock()
	if _, exists := p.pendingBySkill[skillName]; exists {
		n := p.CoalescedNotifications.Add(1)
		log.Printf("[evolution-pipeline] coalesced pending notification skill=%s coalesced_total=%d", skillName, n)
	}
	if p.pendingBySkill == nil {
		p.pendingBySkill = make(map[string]evolutionRequest)
	}
	p.pendingBySkill[skillName] = req
	p.pendingMu.Unlock()

	// Non-blocking wake (buffer 1): if the loop is already scheduled, skip.
	select {
	case p.pendingWake <- struct{}{}:
	default:
	}
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
			batch := p.takeAllPending()
			for _, req := range batch {
				// Re-check stop between skills so shutdown stays responsive.
				select {
				case <-p.stopCh:
					return
				default:
				}
				p.processRequest(req)
			}
		}
	}
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
	PendingSkills          int           `json:"pending_skills"`
	CoalescedNotifications uint64        `json:"coalesced_notifications"`
	DroppedNotifications   uint64        `json:"dropped_notifications"`
	ProcessedRequests      int           `json:"processed_requests"`
	EnableRepair           bool          `json:"enable_repair"`
	EnableOptimizer        bool          `json:"enable_optimizer"`
	EnablePromoter         bool          `json:"enable_promoter"`
	RepairCooldown         time.Duration `json:"repair_cooldown"`
	HasRepairHook          bool          `json:"has_repair_hook"`
	HasOptimizer           bool          `json:"has_optimizer"`
	HasPromoter            bool          `json:"has_promoter"`
}

// Status returns a diagnostic snapshot (safe for concurrent readers).
func (p *EvolutionPipeline) Status() EvolutionStatus {
	if p == nil {
		return EvolutionStatus{}
	}
	return EvolutionStatus{
		PendingSkills:          p.PendingSkillCount(),
		CoalescedNotifications: p.CoalescedNotifications.Load(),
		DroppedNotifications:   p.DroppedNotifications.Load(),
		ProcessedRequests:      int(p.requestCount.Load()),
		EnableRepair:           p.EnableRepair,
		EnableOptimizer:        p.EnableOptimizer,
		EnablePromoter:         p.EnablePromoter,
		RepairCooldown:         p.RepairCooldown,
		HasRepairHook:          p.RepairHook != nil,
		HasOptimizer:           p.Optimizer != nil,
		HasPromoter:            p.Promoter != nil,
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

	p.requestCount.Add(1)

	// 0. Surface failed executions for frontend/audit.
	failed := req.ExecResult != nil && !req.ExecResult.Success
	if failed && p.EventEmitter != nil {
		p.EventEmitter(EventSkillExecutionFailed, map[string]string{
			"skill": req.SkillName,
		})
	}

	// 1. Self-repair for failed skills (unified schedule + throttle).
	// Platform security/persist is handled by RepairHook when configured.
	if failed && p.EnableRepair && req.Entry != nil {
		p.tryRepair(ctx, req)
	}

	// 2. Check if skill should be optimized (working but suboptimal).
	// tryOptimize consults ShouldOptimize + 24h throttle; safe on failures too.
	if p.EnableOptimizer && p.Optimizer != nil && req.Entry != nil {
		p.tryOptimize(ctx, req)
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
		p.tryFileBackedRepairDraft(ctx, req, entry)
		return
	}

	log.Printf("[evolution-pipeline] scheduling self-repair skill=%s usage=%d success=%d",
		req.SkillName, entry.UsageCount, entry.SuccessCount)

	if p.RepairHook != nil {
		p.markRepairAttempt(req.SkillName)
		p.RepairHook(entry, req.RunArgs)
		return
	}

	// Core-only fallback (no platform security scan): useful for tests / headless.
	if p.LLM == nil || !p.LLM.IsConfigured() {
		log.Printf("[evolution-pipeline] repair skipped skill=%s: LLM not configured", req.SkillName)
		return
	}
	repairCtx := NewRepairContext(entry, req.RunArgs)
	result, err := AttemptRepairWithContext(p.LLM, entry, repairCtx)
	if err != nil {
		// LLM 调用已真实发生（失败也算成本），消耗冷却防止每次失败都重调。
		p.markRepairAttempt(req.SkillName)
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
		if len(historicalArgs) > 0 {
			gateResult, gateErr := p.Gate.Verify(ctx, entry, nlSteps, historicalArgs)
			if gateErr != nil {
				// gate 运行在 LLM 调用之后，错误也意味着本次 LLM 调用已白花。
				p.markRepairAttempt(req.SkillName)
				log.Printf("[evolution-pipeline] repair gate error skill=%s: %v", req.SkillName, gateErr)
				return
			}
			if gateResult == nil || !gateResult.Passed {
				p.markRepairAttempt(req.SkillName)
				reason := "nil"
				if gateResult != nil {
					reason = gateResult.Reason
				}
				log.Printf("[evolution-pipeline] repair gate rejected skill=%s: %s", req.SkillName, reason)
				return
			}
		}
	}
	// LLM 修复成功且 gate 通过、真正落 ApplyRepair 之前才写冷却时间戳。
	p.markRepairAttempt(req.SkillName)
	if !ApplyRepair(entry, result) {
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
	if p.SkillSaver != nil && p.SkillLoader != nil {
		skills := p.SkillLoader()
		for i := range skills {
			if skills[i].Name == req.SkillName {
				mergeEvolvedEntry(&skills[i], entry)
				if err := p.SkillSaver(skills); err != nil {
					log.Printf("[evolution-pipeline] save repaired skill=%s failed: %v", req.SkillName, err)
					return
				}
				break
			}
		}
	}
	if entry.SkillDir != "" {
		if err := WriteBackOptimizedSteps(entry); err != nil {
			log.Printf("[evolution-pipeline] writeback repaired skill.yaml for %s failed: %v", req.SkillName, err)
		}
	}
	if p.EventEmitter != nil {
		explanation := ""
		if result != nil {
			explanation = result.Explanation
		}
		p.EventEmitter(EventSkillRepaired, map[string]string{
			"skill":       req.SkillName,
			"explanation": explanation,
		})
	}
	log.Printf("[evolution-pipeline] repair applied skill=%s", req.SkillName)
}

// tryFileBackedRepairDraft 为 file-backed 技能生成人审 patch draft（P0-4
// 方案 A）：复用 LLM 修复 + gate 验证，通过后把 draft 写到
// <skill_dir>/.evolution-drafts/<utc时间戳>.json 并发 EventSkillRepairDraftReady。
// 本路径绝不修改 entry、不调 SkillSaver、不写回 skill.yaml —— 应用/拒绝由
// GUI 人审时完成。
func (p *EvolutionPipeline) tryFileBackedRepairDraft(ctx context.Context, req evolutionRequest, entry *corelib.NLSkillEntry) {
	// 除 file-backed 外其他门槛仍需全部通过（max_attempts / error class / 用量统计）。
	if ok, reason := explainRepairGate(entry, true); !ok {
		log.Printf("[evolution-pipeline] repair draft skipped skill=%s reason=%s", req.SkillName, reason)
		return
	}

	// 已有未评审 draft 时不重复生成。
	draftsDir := filepath.Join(entry.SkillDir, RepairDraftsDirName)
	if HasPendingRepairDraft(draftsDir) {
		log.Printf("[evolution-pipeline] repair draft skipped skill=%s reason=draft_pending", req.SkillName)
		return
	}

	// SKILL.md-only 技能没有机器可写的 steps 文件，apply 必然失败——在 LLM
	// 调用前跳过（不烧 LLM，也不写冷却时间戳：反正永远不会成功）。
	if !hasSkillYAMLFile(entry.SkillDir) {
		log.Printf("[evolution-pipeline] repair draft skipped skill=%s reason=no_skill_yaml", req.SkillName)
		return
	}

	// 含 poll/loop 步骤的技能：WriteBackOptimizedSteps 不回写 poll/loop，
	// apply 会静默剥离这些配置——跳过生成，不烧 LLM。
	if StepsHavePollLoop(entry.Steps) {
		log.Printf("[evolution-pipeline] repair draft skipped skill=%s reason=poll_loop_unsupported", req.SkillName)
		return
	}

	if p.LLM == nil || !p.LLM.IsConfigured() {
		log.Printf("[evolution-pipeline] repair draft skipped skill=%s: LLM not configured", req.SkillName)
		return
	}
	if ctx.Err() != nil {
		return
	}

	repairCtx := NewRepairContext(entry, req.RunArgs)
	result, err := AttemptRepairWithContext(p.LLM, entry, repairCtx)
	if err != nil {
		// LLM 调用已真实发生（失败也算成本），消耗冷却防止每次失败都重调。
		p.markRepairAttempt(req.SkillName)
		log.Printf("[evolution-pipeline] repair draft LLM failed skill=%s: %v", req.SkillName, err)
		return
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
		p.writeRepairDraftAndNotify(req.SkillName, entry.SkillDir, draft)
		return
	}
	if result == nil || !result.Repaired || len(result.NewSteps) == 0 {
		// LLM 已调用但认为不可修复/被 sanitize 拒绝：同样消耗冷却。
		// （AttemptRepairWithContext 内部不经 LLM 的 not-repairable 早退
		// 不可达——上方 gate 已用同一 IsRepairableError 过滤过。）
		p.markRepairAttempt(req.SkillName)
		explanation := ""
		if result != nil {
			explanation = result.Explanation
		}
		log.Printf("[evolution-pipeline] repair draft not applicable skill=%s: %s", req.SkillName, explanation)
		return
	}

	nlSteps := convertRepairResultSteps(result.NewSteps)
	if p.Gate != nil {
		var historicalArgs []map[string]string
		if p.UsageTracker != nil {
			historicalArgs = p.UsageTracker.RecentRunArgs("skill:"+req.SkillName, 3)
		}
		if len(historicalArgs) > 0 {
			gateResult, gateErr := p.Gate.Verify(ctx, entry, nlSteps, historicalArgs)
			if gateErr != nil {
				p.markRepairAttempt(req.SkillName)
				log.Printf("[evolution-pipeline] repair draft gate error skill=%s: %v", req.SkillName, gateErr)
				return
			}
			if gateResult == nil || !gateResult.Passed {
				p.markRepairAttempt(req.SkillName)
				reason := "nil"
				if gateResult != nil {
					reason = gateResult.Reason
				}
				log.Printf("[evolution-pipeline] repair draft gate rejected skill=%s: %s", req.SkillName, reason)
				return
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
	p.writeRepairDraftAndNotify(req.SkillName, entry.SkillDir, draft)
}

// writeRepairDraftAndNotify persists a repair draft and emits
// EventSkillRepairDraftReady. A successful write consumes the repair cooldown
// (shared with the auto-repair throttle); a failed write does not — nothing
// was produced, so the next failure may retry immediately.
func (p *EvolutionPipeline) writeRepairDraftAndNotify(skillName, skillDir string, draft RepairDraft) {
	name, err := WriteRepairDraft(skillDir, draft)
	if err != nil {
		// 写盘失败不消耗冷却——draft 没产出，下次可立即重试。
		log.Printf("[evolution-pipeline] repair draft write failed skill=%s: %v", skillName, err)
		return
	}
	// 与自动修复共用 repairAttempts 冷却节流：draft 落盘成功才写时间戳。
	p.markRepairAttempt(skillName)
	if p.EventEmitter != nil {
		p.EventEmitter(EventSkillRepairDraftReady, map[string]string{
			"skill": skillName,
			"draft": name,
		})
	}
	log.Printf("[evolution-pipeline] repair draft ready skill=%s draft=%s", skillName, name)
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
	if IsFileBackedSkill(*entry) {
		return OptimizeResult{SkipReason: "file-backed skills require a reviewed patch flow"}
	}
	if p.UsageTracker == nil {
		return OptimizeResult{SkipReason: "usage tracker not configured"}
	}
	req := evolutionRequest{SkillName: entry.Name, Entry: entry}
	return p.runOptimize(ctx, req, force)
}

func (p *EvolutionPipeline) tryOptimize(ctx context.Context, req evolutionRequest) {
	_ = p.runOptimize(ctx, req, false)
}

func (p *EvolutionPipeline) runOptimize(ctx context.Context, req evolutionRequest, force bool) OptimizeResult {
	if req.Entry == nil || p.UsageTracker == nil || p.Optimizer == nil {
		return OptimizeResult{SkipReason: "optimizer prerequisites missing"}
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
		if p.SkillSaver == nil {
			log.Printf("[evolution-pipeline] optimization not persisted skill=%s: SkillSaver not configured", req.SkillName)
			return OptimizeResult{Attempted: true, Explanation: "optimization not persisted: SkillSaver not configured"}
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
			log.Printf("[evolution-pipeline] optimization not persisted skill=%s: not found in storage", req.SkillName)
			return OptimizeResult{Attempted: true, Explanation: "skill not found in storage, optimization not persisted"}
		}
		if err := p.SkillSaver(skills); err != nil {
			log.Printf("[evolution-pipeline] save optimized skill=%s failed: %v", req.SkillName, err)
			return OptimizeResult{Attempted: true, Explanation: "save failed: " + err.Error()}
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
			p.EventEmitter(EventSkillOptimized, map[string]string{
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
