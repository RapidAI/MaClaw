package main

// Shared agent-loop strangler (phase 3):
//
// When enabled, eligible chat AND background turns run through
// corelib/agent.RunLoop instead of the full IM iteration machinery.
//
// Modes (env MACLAW_SHARED_AGENT_LOOP):
//   - 1/true/on  → use shared path when eligible
//   - 0/false/off → force legacy
//   - shadow     → always legacy, but log when shared would have been used
//
// Also: AppConfig.SharedAgentLoopEnabled (defaults true on new installs).
// Canary: MACLAW_SHARED_AGENT_LOOP_PERCENT=0..100 (sticky by userID, default 100).
// Workflow pilot (non-doc): MACLAW_SHARED_AGENT_LOOP_WORKFLOW=1
//
// Still excluded by default: workflow agent loops and all workflow doc phases.

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/doctor"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/llm/moa"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// sharedAgentLoopMode is the effective strangler mode.
type sharedAgentLoopMode int

const (
	sharedAgentLoopOff sharedAgentLoopMode = iota
	sharedAgentLoopOn
	sharedAgentLoopShadow // log-only; never divert
)

// shouldUseSharedAgentLoop reports whether this turn should EXECUTE on the shared RunLoop.
// Shadow mode returns false (legacy executes) but logs eligibility separately.
func (h *IMMessageHandler) shouldUseSharedAgentLoop(ctx *LoopContext, userID string, attachments []MessageAttachment) bool {
	mode := resolveSharedAgentLoopMode(h)
	eligible, reason := h.sharedAgentLoopEligibility(ctx, attachments)
	switch mode {
	case sharedAgentLoopOff:
		return false
	case sharedAgentLoopShadow:
		// Log only eligible turns to avoid drowning logs on workflow traffic.
		if eligible {
			inCanary := sharedLoopCanaryAllowsFor(h, userID)
			recordSharedLoopSkip("shadow", reason)
			log.Printf("[agent-loop] shadow eligible kind=%s reason=%q attachments=%d canary=%v percent=%d (legacy path kept)",
				ctxKindLabel(ctx), reason, len(attachments), inCanary, sharedLoopPercentFor(h))
		}
		return false
	case sharedAgentLoopOn:
		if !eligible {
			recordSharedLoopSkip("ineligible", reason)
			return false
		}
		if !sharedLoopCanaryAllowsFor(h, userID) {
			recordSharedLoopSkip("canary", "canary")
			log.Printf("[agent-loop] shared canary skip owner=%q percent=%d kind=%s",
				userID, sharedLoopPercentFor(h), ctxKindLabel(ctx))
			return false
		}
		return true
	default:
		return false
	}
}

// sharedAgentLoopEligibility returns whether the turn shape is allowed on shared RunLoop.
func (h *IMMessageHandler) sharedAgentLoopEligibility(ctx *LoopContext, attachments []MessageAttachment) (ok bool, reason string) {
	if h == nil {
		return false, "nil handler"
	}
	if ctx == nil {
		return false, "nil context"
	}
	// Phase 3: chat + background.
	if ctx.Kind != LoopKindChat && ctx.Kind != LoopKindBackground {
		return false, "unsupported loop kind"
	}
	// Doc-capture / AG-UI document phases always stay on the legacy loop.
	if ctx.WorkflowDocPhase {
		return false, "workflow doc phase"
	}
	// Full workflow agent loops: legacy by default; opt-in via config/env pilot.
	if ctx.WorkflowAgentLoop {
		if !sharedLoopWorkflowPilotEnabledFor(h) {
			return false, "workflow phase"
		}
		return true, "workflow-pilot"
	}
	// Light attachments are supported; reject oversized batches.
	if !sharedLoopAttachmentsAllowed(attachments) {
		return false, "attachment batch too large"
	}
	if ctx.Kind == LoopKindBackground {
		return true, "background"
	}
	return true, "chat"
}

func ctxKindLabel(ctx *LoopContext) string {
	if ctx == nil {
		return "nil"
	}
	switch ctx.Kind {
	case LoopKindChat:
		return "chat"
	case LoopKindBackground:
		return "background"
	default:
		return fmt.Sprintf("kind=%d", int(ctx.Kind))
	}
}

// sharedLoopAttachmentsAllowed gates attachment shape for the shared path.
// Images/files/voice are OK; reject empty data spam or huge batches.
func sharedLoopAttachmentsAllowed(attachments []MessageAttachment) bool {
	if len(attachments) == 0 {
		return true
	}
	if len(attachments) > 8 {
		return false
	}
	var total int64
	for i := range attachments {
		att := &attachments[i]
		if att.Size > 0 {
			total += att.Size
		} else if att.Data != "" {
			// Rough base64 size estimate.
			total += int64(len(att.Data) * 3 / 4)
		}
		// Empty attachment with no data and no name is suspicious — skip gate (allow).
	}
	// Soft cap ~25MB raw across the batch.
	const maxBatch = 25 * 1024 * 1024
	return total <= maxBatch
}

func sharedAgentLoopEnabled(h *IMMessageHandler) bool {
	return resolveSharedAgentLoopMode(h) == sharedAgentLoopOn
}

func resolveSharedAgentLoopMode(h *IMMessageHandler) sharedAgentLoopMode {
	// Package tests intentionally stay on the legacy IM loop: shared-loop unit
	// tests exercise resolveSharedAgentLoopModeLive / eligibility helpers, while
	// RunAgentLoop* assertions still target legacy recover/trial/trace behavior.
	// Parallel t.Setenv races must not divert those suites onto the shared path.
	if testing.Testing() {
		return sharedAgentLoopOff
	}
	return resolveSharedAgentLoopModeLive(h)
}

// resolveSharedAgentLoopModeLive is the production resolver (env > config > defaults).
// Unit tests call it directly so they are not affected by the testing.Testing() gate.
func resolveSharedAgentLoopModeLive(h *IMMessageHandler) sharedAgentLoopMode {
	if v := strings.TrimSpace(os.Getenv("MACLAW_SHARED_AGENT_LOOP")); v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			return sharedAgentLoopOn
		case "shadow", "observe", "dry-run", "dryrun":
			return sharedAgentLoopShadow
		case "0", "false", "no", "off":
			return sharedAgentLoopOff
		}
	}
	// Secondary shadow flag (can combine with config enabled for dual control).
	if shadowEnvTrue() {
		return sharedAgentLoopShadow
	}
	if h != nil && h.app != nil {
		if cfg, err := h.app.LoadConfig(); err == nil && cfg.SharedAgentLoopEnabled {
			return sharedAgentLoopOn
		}
	}
	// No env, no loaded app config: use product default (new-install posture).
	if h == nil || h.app == nil {
		return sharedAgentLoopModeFromDefaultConfig()
	}
	return sharedAgentLoopOff
}

func sharedAgentLoopModeFromDefaultConfig() sharedAgentLoopMode {
	if corelib.AppConfigDefaults().SharedAgentLoopEnabled {
		return sharedAgentLoopOn
	}
	return sharedAgentLoopOff
}

// sharedLoopPercent returns 0..100 canary percentage (env > config > 100).
func sharedLoopPercent() int {
	return sharedLoopPercentFor(nil)
}

func sharedLoopPercentFor(h *IMMessageHandler) int {
	cfg := corelib.AppConfig{}
	if h != nil && h.app != nil {
		if loaded, err := h.app.LoadConfig(); err == nil {
			cfg = loaded
		}
	}
	n, _ := doctor.ResolveSharedLoopPercent(cfg)
	return n
}

// sharedLoopCanaryAllows is sticky by userID (FNV-1a bucket). Empty userID always allows.
// Delegates to doctor.SharedLoopCanaryAllows so CLI preview and runtime stay aligned.
func sharedLoopCanaryAllows(userID string) bool {
	return doctor.SharedLoopCanaryAllows(userID, sharedLoopPercent())
}

func sharedLoopCanaryAllowsFor(h *IMMessageHandler, userID string) bool {
	return doctor.SharedLoopCanaryAllows(userID, sharedLoopPercentFor(h))
}

func shadowEnvTrue() bool {
	v := strings.TrimSpace(os.Getenv("MACLAW_SHARED_AGENT_LOOP_SHADOW"))
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// sharedLoopWorkflowPilotEnabled allows non-doc WorkflowAgentLoop on shared path
// (env MACLAW_SHARED_AGENT_LOOP_WORKFLOW > config shared_agent_loop_workflow).
func sharedLoopWorkflowPilotEnabled() bool {
	return sharedLoopWorkflowPilotEnabledFor(nil)
}

func sharedLoopWorkflowPilotEnabledFor(h *IMMessageHandler) bool {
	cfg := corelib.AppConfig{}
	if h != nil && h.app != nil {
		if loaded, err := h.app.LoadConfig(); err == nil {
			cfg = loaded
		}
	}
	on, _ := doctor.ResolveSharedLoopWorkflowPilot(cfg)
	return on
}

// sharedAgentLoopModeName is for doctor / debug.
func sharedAgentLoopModeName(h *IMMessageHandler) string {
	switch resolveSharedAgentLoopMode(h) {
	case sharedAgentLoopOn:
		return "on"
	case sharedAgentLoopShadow:
		return "shadow"
	default:
		return "off"
	}
}

// runAgentLoopShared executes an eligible chat turn via corelib/agent.RunLoop.
func (h *IMMessageHandler) runAgentLoopShared(
	ctx *LoopContext,
	userID, systemPrompt string,
	history []agent.ConversationEntry,
	userText string,
	attachments []MessageAttachment,
	onProgress tool.ProgressCallback,
	onToken llm.TokenCallback,
	onStreamDone StreamDoneCallback,
	minIterations int,
	platform string,
) (result *IMAgentResponse) {
	startedAt := time.Now()
	requestID, loopID := "", ""
	if ctx != nil {
		requestID = ctx.Runtime.RequestID
		loopID = ctx.ID
	}
	kind := ctxKindLabel(ctx)
	var loopStats struct {
		tools, iters int
	}
	log.Printf("[agent-loop] shared start owner=%q request_id=%q loop=%q kind=%s platform=%q text_len=%d attachments=%d",
		userID, requestID, loopID, kind, platform, len([]rune(userText)), len(attachments))
	defer func() {
		status := "success"
		if result != nil && result.Error != "" {
			status = "error"
		}
		if result != nil && strings.HasPrefix(strings.ToLower(result.Text), "task cancelled") {
			status = "cancelled"
		}
		if result != nil && result.RequestID == "" {
			result.RequestID = requestID
		}
		if result != nil && result.ResponseSource == "" {
			result.ResponseSource = "shared_agent_loop"
		}
		inTok, outTok := 0, 0
		routeTask, routeSrc, routeModel := "", "", ""
		if result != nil {
			inTok, outTok = result.InputTokens, result.OutputTokens
			routeTask, routeSrc, routeModel = result.RouteTask, result.RouteSource, result.RouteModel
		}
		// Greppable metrics line for shadow/cost analysis.
		log.Printf("[agent-loop] shared end owner=%q request_id=%q loop=%q kind=%s status=%s elapsed=%s tools=%d iters=%d tokens_in=%d tokens_out=%d route=%s/%s model=%s path=shared",
			userID, requestID, loopID, kind, status, time.Since(startedAt).Round(time.Millisecond),
			loopStats.tools, loopStats.iters, inTok, outTok, routeTask, routeSrc, routeModel)
		// Process-local counters for GetSharedAgentLoopStatus / doctor.
		cancelled := status == "cancelled"
		errored := status == "error"
		recordSharedAgentLoopTurn(status == "success", cancelled, errored)
		if r := recover(); r != nil {
			result = &IMAgentResponse{Error: fmt.Sprintf("Shared agent loop panicked: %v", r)}
			log.Printf("[agent-loop] shared panic owner=%q request_id=%q loop=%q panic=%v", userID, requestID, loopID, r)
		}
	}()

	telemetry := newAgentLoopTelemetry()
	if ctx != nil {
		if pp := strings.TrimSpace(ctx.Runtime.Execution.PromptProfile); pp != "" {
			telemetry.PromptProfile = pp
		} else if ctx.Runtime.Execution.IsLight() {
			telemetry.PromptProfile = "light"
		}
		telemetry.PromptFullTokens = ctx.Runtime.PromptFullTokens
		telemetry.PromptLightTokens = ctx.Runtime.PromptLightTokens
		telemetry.PromptABSample = ctx.Runtime.PromptABSample
		telemetry.PromptSoftFull = ctx.Runtime.PromptSoftFull
	}
	defer h.beginAgentLoopRuntime(ctx, userID, userText, platform)()
	runtimeState := h.beginAgentLoopRuntimeState(ctx, userID, userText, onProgress, onStreamDone, telemetry)
	defer runtimeState.Cleanup()

	startState := h.prepareAgentLoopStartState(agentLoopStartOptions{
		Context:          ctx,
		UserID:           userID,
		UserText:         userText,
		SystemPrompt:     systemPrompt,
		Platform:         platform,
		Attachments:      attachments,
		History:          history,
		MinIterations:    minIterations,
		PriorReplanCount: runtimeState.PriorReplanCount,
		AdaptiveRetry:    runtimeState.AdaptiveRetry,
		MilestoneTracker: runtimeState.MilestoneTracker,
		Telemetry:        telemetry,
		SendProgress:     runtimeState.SendProgress,
	})
	defer startState.Cleanup()

	cfg := startState.Config
	telemetry.Route = startState.RouteDecision

	// Multimodal user payload (text / image blocks / file path annotations).
	// Use runtime SendProgress so intermediate status styling matches the legacy loop.
	progressOut := runtimeState.SendProgress
	if progressOut == nil {
		progressOut = onProgress
	}
	userContent := buildUserContent(userText, attachments, cfg.Protocol, cfg.SupportsVision, h.app, progressOut)

	cb := &sharedAgentLoopCallbacks{
		handler:      h,
		loopCtx:      ctx,
		userID:       userID,
		userText:     userText,
		systemPrompt: startState.SystemPrompt,
		tools:        startState.Tools,
		llmCfg:       cfg,
		route:        startState.RouteDecision,
		onProgress:   progressOut,
		onToken:      onToken,
		maxIter:      startState.EffectiveMax,
		httpClient:   startState.HTTPClient,
	}
	if cb.maxIter <= 0 {
		cb.maxIter = startState.MaxIterations
	}

	// Pass cb as LoopHooks so OnToolExecuted can escalate models mid-loop.
	loopResult := agent.RunLoopWithUserContent(cb, userText, userContent, history, startState.HTTPClient, cb)
	loopStats.tools = loopResult.ToolCalls
	loopStats.iters = loopResult.Iterations
	if h.app != nil {
		accumulateLoopResultUsage(h.app, cb.llmCfg, loopResult)
	}

	// Merge history delta for multi-turn continuity (includes multimodal user content).
	outHistory := history
	if len(loopResult.HistoryDelta) > 0 {
		outHistory = append(append([]agent.ConversationEntry(nil), history...), loopResult.HistoryDelta...)
	}

	// Cancel path: align with legacy cancelledExitResponse.
	if loopResult.Error == "cancelled" || loopResult.Error == "cancelled during LLM retry" ||
		(ctx != nil && ctx.IsCancelled() && strings.TrimSpace(loopResult.Text) == "") {
		return h.cancelledExitResponse(userID, outHistory, userText)
	}

	if len(loopResult.HistoryDelta) > 0 {
		h.saveConversationHistoryTimed(userID, outHistory, &IMAgentResponse{})
	}

	// Budget mid-loop / entry stop (EarlyStopper) — keep user-facing text.
	if loopResult.Error == "daily_llm_budget_exceeded" {
		text := strings.TrimSpace(loopResult.Text)
		if text == "" {
			if blocked, msg := h.checkDailyBudgetGate(); blocked {
				text = msg
			} else {
				text = "今日 LLM 预算已用尽。"
			}
		}
		return &IMAgentResponse{
			Text:           text,
			Error:          "daily_llm_budget_exceeded",
			HardExit:       true,
			RequestID:      requestID,
			SessionKey:     userID,
			ResponseSource: "budget_gate",
		}
	}

	resp := &IMAgentResponse{
		Text:           loopResult.Text,
		Error:          loopResult.Error,
		HardExit:       loopResult.HardExit,
		RequestID:      requestID,
		SessionKey:     userID,
		ResponseSource: "shared_agent_loop",
	}
	if loopResult.AskUser != nil {
		resp.Text = loopResult.Text
		// Ask-user is returned as text for now; AG-UI paths stay on legacy loop.
	}
	if loopResult.Usage.InputTokens > 0 || loopResult.Usage.OutputTokens > 0 {
		resp.InputTokens = loopResult.Usage.InputTokens
		resp.OutputTokens = loopResult.Usage.OutputTokens
		resp.TotalTokens = loopResult.Usage.TotalTokens()
		resp.CacheReadTokens = loopResult.Usage.CachedTokens
		resp.CacheWriteTokens = loopResult.Usage.CacheWriteTokens
		resp.EstCostRMB = loopResult.Usage.EstCostRMB
		telemetry.LastLLMInputTokens = loopResult.Usage.InputTokens
		telemetry.LastLLMOutputTokens = loopResult.Usage.OutputTokens
		telemetry.LastLLMCacheReadTokens = loopResult.Usage.CachedTokens
		telemetry.LastLLMCacheWriteTokens = loopResult.Usage.CacheWriteTokens
	}
	// Prefer live route on callbacks (may have escalated after tools).
	liveRoute := cb.currentRouteDecision()
	if liveRoute.Task != "" || liveRoute.Model != "" {
		telemetry.Route = liveRoute
	} else if loopResult.Route.TaskType != "" || loopResult.Route.Model != "" {
		telemetry.Route = modelRouteDecision{
			Task:             loopResult.Route.TaskType,
			Source:           loopResult.Route.Source,
			Model:            loopResult.Route.Model,
			Provider:         loopResult.Route.Provider,
			Reason:           loopResult.Route.Reason,
			Escalated:        loopResult.Route.Source == "escalate",
			CostTier:         loopResult.Route.CostTier,
			CostRouteMode:    loopResult.Route.CostRouteMode,
			CostRouteApplied: loopResult.Route.CostRouteApplied,
			ThinkingPolicy:   loopResult.Route.ThinkingPolicy,
		}
	} else {
		telemetry.Route = startState.RouteDecision
	}
	if h.app != nil {
		h.app.recordLastModelRoute(telemetry.Route)
	}
	// Surface mid-loop light→full recovery on Turn chip + response JSON.
	if loopResult.LightUpgraded {
		telemetry.PromptUpgraded = true
		telemetry.PromptProfile = string(agent.PromptProfileFull)
	} else if cb != nil {
		// Reflect final profile (may have been upgraded via soft path before loop).
		if pp := cb.CurrentPromptProfile(); pp != "" {
			telemetry.PromptProfile = string(pp)
		}
	}
	telemetry.Attach(resp)
	if onStreamDone != nil {
		onStreamDone()
	}
	_ = loopID
	return resp
}

// sharedAgentLoopCallbacks adapts IMMessageHandler to agent.LoopCallbacks.
type sharedAgentLoopCallbacks struct {
	handler      *IMMessageHandler
	loopCtx      *LoopContext
	userID       string
	userText     string
	systemPrompt string
	tools        []map[string]interface{}
	llmCfg       corelib.MaclawLLMConfig
	route        modelRouteDecision
	onProgress   tool.ProgressCallback
	onToken      llm.TokenCallback
	maxIter      int
	httpClient   *http.Client
	escalated    bool
	toolCalls    int
	// moaPreset is set for the duration of one agent loop after /moa or auto arming.
	moaPreset *moa.ResolvedPreset
	moaAuto   bool
}

func (c *sharedAgentLoopCallbacks) GetLLMConfig() corelib.MaclawLLMConfig {
	return c.llmCfg
}

// AllowMoAFanOut implements agent.MoABudgetGate when the app tracks daily LLM budget.
func (c *sharedAgentLoopCallbacks) AllowMoAFanOut(nRefs int) (ok bool, reason string) {
	if c == nil || c.handler == nil || c.handler.app == nil {
		return true, ""
	}
	ct := c.handler.app.ohModules.costTracker
	if ct == nil || ct.BudgetLimit() <= 0 {
		return true, ""
	}
	need := moa.EstimateWaveMinUSD(nRefs)
	if ct.CanAfford(need) {
		return true, ""
	}
	lang := c.handler.imCommandResponseLang("")
	if normalizeAppLanguageKind(lang).IsChinese() {
		return false, fmt.Sprintf("今日预算不足，已跳过其他模型会诊（约需 $%.4f；%s）", need, ct.DailySummary())
	}
	return false, fmt.Sprintf("moa advisors skipped (daily budget low; need ~$%.4f, %s)", need, ct.DailySummary())
}

// PrepareMoA implements agent.MoAHost for one-shot / sticky / allow_auto council turns.
func (c *sharedAgentLoopCallbacks) PrepareMoA(iteration int, toolsSeen bool, fanoutsRan int) (active bool, preset moa.ResolvedPreset, progress string) {
	if c == nil || c.handler == nil {
		return false, moa.ResolvedPreset{}, ""
	}
	// K9: kill switch must disable even if sticky was armed earlier in-session.
	if !moa.EnvAllows() {
		return false, moa.ResolvedPreset{}, ""
	}
	// First prepare of this loop: resolve arming source once and pin on callbacks.
	if c.moaPreset == nil {
		if c.handler.moaSessions != nil {
			if sess := c.handler.moaSessions.peek(c.userID); sess != nil && (sess.OneShot || sess.Sticky) {
				p := sess.Resolved
				c.moaPreset = &p
				if sess.OneShot && !sess.Sticky {
					c.handler.moaSessions.clearOneShot(c.userID)
				}
			}
		}
		// allow_auto: hard turns only (K13), when not already armed.
		if c.moaPreset == nil {
			if auto, ok := c.handler.tryPrepareMoAAuto(c.userText, c.route); ok {
				c.moaPreset = &auto
				c.moaAuto = true
			}
		}
	}
	if c.moaPreset == nil || !c.moaPreset.Enabled {
		return false, moa.ResolvedPreset{}, ""
	}
	// K16: MoA implies full prompt profile (no light).
	if c.CurrentPromptProfile().IsLight() {
		c.UpgradeLightPromptToFull("moa council")
	}
	n := len(c.moaPreset.References)
	progress = fmt.Sprintf("consulting %d models…", n)
	lang := ""
	if c.handler != nil {
		lang = c.handler.imCommandResponseLang("")
	}
	if normalizeAppLanguageKind(lang).IsChinese() {
		progress = fmt.Sprintf("正在征询 %d 个其他模型…", n)
	}
	if c.moaAuto && iteration == 0 && fanoutsRan == 0 {
		if normalizeAppLanguageKind(lang).IsChinese() {
			progress = "难题自动多模型会诊：" + progress
		} else {
			progress = "auto multi-model: " + progress
		}
	}
	_ = toolsSeen
	return true, *c.moaPreset, progress
}

// CurrentPromptProfile implements agent.PromptProfileProvider for light-tool deny.
func (c *sharedAgentLoopCallbacks) CurrentPromptProfile() agent.PromptProfile {
	if c == nil || c.loopCtx == nil {
		return agent.PromptProfileFull
	}
	pp := strings.TrimSpace(c.loopCtx.Runtime.Execution.PromptProfile)
	if pp == "" && c.loopCtx.Runtime.Execution.IsLight() {
		return agent.PromptProfileLight
	}
	return agent.NormalizePromptProfile(pp)
}

// UpgradeLightPromptToFull implements agent.LightProfileUpgrader: expands tools
// and rebuilds the system prompt so the denied tool can re-authorize.
func (c *sharedAgentLoopCallbacks) UpgradeLightPromptToFull(reason string) bool {
	if c == nil || !c.CurrentPromptProfile().IsLight() {
		return false
	}
	if c.loopCtx != nil {
		c.loopCtx.Runtime.Execution = fullExecutionProfile("light tool deny → full: " + reason)
	}
	if c.handler != nil {
		phase := c.handler.initialAgentLoopPhase(c.userText, c.loopCtx)
		toolSet := c.handler.prepareAgentLoopTools(c.userID, c.userText, c.loopCtx, phase)
		c.tools = toolSet.Tools
		// Rebuild full policy surface (profile now full on loopCtx).
		c.systemPrompt = c.handler.buildSystemPromptWithMemory(c.userText, false, c.loopCtx)
	}
	log.Printf("[shared-loop] light→full prompt upgrade reason=%s tools=%d", reason, len(c.tools))
	return !c.CurrentPromptProfile().IsLight()
}

func (c *sharedAgentLoopCallbacks) RouteTurn(userText string) (corelib.MaclawLLMConfig, agent.RouteDecision, bool) {
	if c == nil {
		return corelib.MaclawLLMConfig{}, agent.RouteDecision{}, false
	}
	// Routing already applied in prepareAgentLoopStartState; expose it.
	return c.llmCfg, agent.RouteDecision{
		TaskType:         c.route.Task,
		Model:            c.route.Model,
		Provider:         c.route.Provider,
		Source:           c.route.Source,
		Reason:           c.route.Reason + " (shared loop)",
		Applied:          true,
		CostTier:         c.route.CostTier,
		CostRouteMode:    c.route.CostRouteMode,
		CostRouteApplied: c.route.CostRouteApplied,
		ThinkingPolicy:   c.route.ThinkingPolicy,
	}, true
}

func (c *sharedAgentLoopCallbacks) GetMaxIterations() int {
	return config.EffectiveMaxIterations(c.maxIter)
}

func (c *sharedAgentLoopCallbacks) BuildSystemPrompt(userText string, isFirstTurn bool) string {
	_ = userText
	_ = isFirstTurn
	return c.systemPrompt
}

func (c *sharedAgentLoopCallbacks) BuildTools(userText string) []map[string]interface{} {
	_ = userText
	return c.tools
}

func (c *sharedAgentLoopCallbacks) ExecuteTool(name, argsJSON string) string {
	if c == nil || c.handler == nil {
		return "handler unavailable"
	}
	// Defense-in-depth: never execute oversized payloads even if a caller bypasses RunLoop.
	if argSize := len(argsJSON); argSize > guiMaxToolArgumentsBytes {
		toolName := strings.TrimSpace(name)
		if toolName == "" {
			toolName = "unknown"
		}
		return fmt.Sprintf("tool arguments too large for %s: %d bytes exceeds limit %d", toolName, argSize, guiMaxToolArgumentsBytes)
	}
	// IM channels need concrete tool details (path/command/pattern), not bare tool names.
	// Emit here where argsJSON is available; OnToolCall only receives the name.
	// Language follows GUI interface language (App.CurrentLanguage).
	lang := c.handler.imUILang()
	if c.onProgress != nil {
		c.onProgress(userFacingToolProgressTextWithArgs(lang, name, argsJSON))
	}
	// Filter internal tool chatter the same way as the legacy agent-loop path so
	// unstyled bash/skill logs never leak into WeChat/QQ as normal chat bubbles.
	toolProgress := filteredToolProgressCallback(lang, name, c.onProgress, false)
	exec := c.handler.executeToolDetailedWithUserText(name, argsJSON, c.userText, toolProgress)
	return truncateToolResultForToolWithSession(name, c.userID, exec.Text)
}

func (c *sharedAgentLoopCallbacks) ExecuteToolStructured(name, argsJSON string) agent.ToolExecutionResult {
	text := c.ExecuteTool(name, argsJSON)
	outcome := agent.ToolExecutionOutcomeOK
	if strings.HasPrefix(strings.TrimSpace(text), "[错误]") ||
		strings.Contains(text, "工具执行异常") ||
		strings.Contains(text, "未知工具") {
		outcome = agent.ToolExecutionOutcomeError
	}
	if strings.Contains(text, "命令超时") {
		outcome = agent.ToolExecutionOutcomeTimeout
	}
	return agent.ToolExecutionResult{Result: text, Outcome: outcome}
}

func (c *sharedAgentLoopCallbacks) OnToken(delta string) {
	if c.onToken != nil {
		c.onToken(delta)
	}
}

func (c *sharedAgentLoopCallbacks) OnProgress(text string) {
	if c.onProgress != nil {
		c.onProgress(text)
	}
}

func (c *sharedAgentLoopCallbacks) OnToolCall(name string) {
	// Intentionally no-op: bare tool names (e.g. "bash", "read_file") are not
	// useful on IM. Concrete progress with args is emitted from ExecuteTool.
	_ = name
}

func (c *sharedAgentLoopCallbacks) OnToolResult(name string) {
	_ = name
}

// OnToolExecuted implements agent.LoopHooks for mid-loop model escalation.
func (c *sharedAgentLoopCallbacks) OnToolExecuted(name, argsJSON, result string, success bool) {
	_ = name
	_ = argsJSON
	_ = result
	_ = success
	if c == nil {
		return
	}
	c.toolCalls++
	c.maybeEscalateAfterTools()
}

func (c *sharedAgentLoopCallbacks) OnEmptyResponse(iteration int) bool {
	_ = iteration
	return false
}

func (c *sharedAgentLoopCallbacks) TransformConversation(conversation []interface{}) []interface{} {
	return nil
}

func (c *sharedAgentLoopCallbacks) maybeEscalateAfterTools() {
	if c == nil || c.escalated || c.handler == nil || c.toolCalls == 0 {
		return
	}
	// Only escalate light initial routes.
	light := c.route.Task == string(llm.TaskFast) ||
		c.route.Task == string(llm.TaskSummary) ||
		c.route.Task == string(llm.TaskIntent) ||
		c.route.Source == "aux"
	if !light {
		return
	}
	before := c.llmCfg.Model
	upgraded := c.handler.routeLLMConfig(llm.TaskReasoning)
	if upgraded.Model == "" || (upgraded.Model == before && upgraded.URL == c.llmCfg.URL) {
		return
	}
	c.llmCfg = upgraded
	c.route.Task = string(llm.TaskReasoning)
	c.route.Source = "escalate"
	c.route.Model = upgraded.Model
	c.route.Provider = upgraded.ProviderName
	c.route.Reason = "tools requested after light turn (shared loop)"
	c.route.Escalated = true
	c.escalated = true
	if c.handler.app != nil {
		c.handler.app.recordLastModelRoute(c.route)
	}
	log.Printf("[model-route] shared escalate %s→%s", before, upgraded.Model)
}

func (c *sharedAgentLoopCallbacks) currentRouteDecision() modelRouteDecision {
	if c == nil {
		return modelRouteDecision{}
	}
	return c.route
}

func (c *sharedAgentLoopCallbacks) ShouldStop() bool {
	if c == nil || c.loopCtx == nil {
		return false
	}
	return c.loopCtx.IsCancelled()
}

// EarlyStop implements agent.EarlyStopper — daily LLM budget mid-loop hard-stop.
func (c *sharedAgentLoopCallbacks) EarlyStop() (stop bool, errCode, userText string) {
	if c == nil || c.handler == nil {
		return false, "", ""
	}
	blocked, msg := c.handler.checkDailyBudgetGate()
	if !blocked {
		return false, "", ""
	}
	return true, "daily_llm_budget_exceeded", msg
}

// OnLLMUsage implements agent.LLMUsageRecorder so CostTracker updates each round
// (shared path previously only accumulated tokens at loop end).
func (c *sharedAgentLoopCallbacks) OnLLMUsage(model string, inputTokens, outputTokens int) {
	if c == nil || c.handler == nil {
		return
	}
	if model == "" {
		model = c.llmCfg.Model
	}
	c.handler.recordLLMCost(model, inputTokens, outputTokens)
}

func (c *sharedAgentLoopCallbacks) LLMRequestContext(iteration int) (context.Context, func(error), error) {
	if c != nil && c.loopCtx != nil {
		ctx, cancel := c.loopCtx.Context()
		trace := llm.RequestTrace{
			Caller:    "shared_agent_loop",
			OwnerID:   c.userID,
			RequestID: c.loopCtx.Runtime.RequestID,
			LoopID:    c.loopCtx.ID,
			Iteration: iteration,
		}
		ctx = llm.WithRequestTrace(ctx, trace)
		return ctx, func(error) { cancel() }, nil
	}
	return nil, nil, fmt.Errorf("no loop context")
}
