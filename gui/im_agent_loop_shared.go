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
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"reflect"
	"runtime/debug"
	"strings"
	"sync/atomic"
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
	onNewRound NewRoundCallback,
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
	if strings.TrimSpace(loopID) == "" {
		loopID = fmt.Sprintf("shared-%d", time.Now().UnixNano())
		log.Printf("[InFlightTask] generated missing shared run id user=%q run=%q", userID, loopID)
	}
	kind := ctxKindLabel(ctx)
	var loopStats struct {
		tools, iters int
	}
	log.Printf("[agent-loop] shared start owner=%q request_id=%q loop=%q kind=%s platform=%q text_len=%d attachments=%d",
		userID, requestID, loopID, kind, platform, len([]rune(userText)), len(attachments))
	// Trajectory cleanup after recover so panic turns stamp error status before Flush.
	var (
		trajRecorder  *TrajectoryRecorder
		trajCleanup   func()
		trajTelemetry *agentLoopTelemetry
	)
	defer func() {
		if r := recover(); r != nil {
			result = &IMAgentResponse{Error: fmt.Sprintf("Shared agent loop panicked: %v", r)}
			log.Printf("[agent-loop] shared panic owner=%q request_id=%q loop=%q panic=%v\n%s", userID, requestID, loopID, r, debug.Stack())
		}
		status, _ := classifyIMAgentResponseOutcome(result)
		if result != nil {
			result.ToolCallsInTurn = loopStats.tools
			if result.RequestID == "" {
				result.RequestID = requestID
			}
			if result.ResponseSource == "" {
				result.ResponseSource = "shared_agent_loop"
			}
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
		// Stamp outcome only when RecordLoopResult did not already finalize it.
		// Re-applying from a sparse IM response would clobber cancel/paused status
		// and wipe LoopResult token usage (cancel uses Text, not Error).
		// Panic / unexpected early return also lands here — close orphans first.
		if trajRecorder != nil && !trajRecorder.HasOutcome() {
			if reason := unpairedCloseReasonFromIMResponse(result); reason != "" {
				trajRecorder.CloseUnpairedToolCalls(reason)
			}
			trajRecorder.SetOutcomeFromIMResponse(result, trajTelemetry, loopStats.iters, loopStats.tools)
		}
		if trajCleanup != nil {
			trajCleanup()
		}
	}()

	telemetry := newAgentLoopTelemetry()
	trajTelemetry = telemetry
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
	runtimeState := h.beginAgentLoopRuntimeState(ctx, userID, userText, onProgress, onStreamDone, telemetry)
	defer runtimeState.Cleanup()
	projectPath := h.effectiveWorkingDirForUser(userID)
	if projectPath == "" {
		projectPath = projectPathFromUserID(userID)
	}

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
	trajRecorder = startState.Recorder
	trajCleanup = startState.Cleanup

	cfg := startState.Config
	telemetry.Route = startState.RouteDecision

	// prepareAgentLoopStartState has already staged attachments and completed
	// local voice ASR. Reuse that exact payload: rebuilding it here used to run
	// WAV conversion, ASR and optional remote transcript correction a second
	// time before the first Agent request.
	progressOut := runtimeState.SendProgress
	if progressOut == nil {
		progressOut = onProgress
	}
	userContent := startState.UserContent

	cb := &sharedAgentLoopCallbacks{
		handler:           h,
		loopCtx:           ctx,
		userID:            userID,
		userText:          userText,
		checkpointHistory: append(append([]agent.ConversationEntry(nil), history...), agent.ConversationEntry{Role: "user", Content: userContent}),
		checkpointRunID:   loopID,
		checkpointProject: projectPath,
		hasLocalFileWork:  len(attachments) > 0 || hasCurrentLocalFileWork(userText),
		platform:          platform, // pin turn platform for tool rewrites (not only loopCtx)
		systemPrompt:      startState.SystemPrompt,
		tools:             startState.Tools,
		llmCfg:            cfg,
		route:             startState.RouteDecision,
		onProgress:        progressOut,
		onToken:           onToken,
		onNewRound:        onNewRound,
		maxIter:           startState.EffectiveMax,
		httpClient:        startState.HTTPClient,
	}
	if cb.maxIter <= 0 {
		cb.maxIter = startState.MaxIterations
	}

	// cb also implements ToolBatchCommitter. The durable checkpoint hook runs
	// only after all results in a tool batch have been paired in HistoryDelta.
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
		// Still stamp trajectory so cancel turns are not missing from session logs.
		if startState.Recorder != nil {
			startState.Recorder.SetKind("shared")
			startState.Recorder.RecordLoopResult(loopResult)
		}
		// A pre-tool checkpoint deliberately contains only the prior valid
		// history prefix. If cancellation lands after the assistant announced a
		// batch but before every result was paired, saving outHistory here would
		// overwrite that checkpoint with a provider-invalid partial group.
		// Leave the durable prefix + uncertain marker intact instead.
		if cb.hasPendingToolBatch {
			return h.interruptedSharedLoopExitResponse(userText)
		}
		return h.cancelledExitResponse(userID, outHistory, userText)
	}
	// ask_user intentionally pauses before the core loop can append its tool
	// result. Reuse the legacy pause finalizer to atomically persist the paired
	// history and pending-user state, but do not treat this interactive pause as
	// a durable side-effect checkpoint. An earlier successful checkpoint, if
	// any, intentionally remains available for crash recovery.
	if loopResult.AskUser != nil {
		askUserOutcome := h.handleAgentLoopAskUserToolResult(
			userID,
			platform,
			userText,
			agent.AskUserResultMarker(loopResult.AskUser),
			false,
			loopResult.PauseToolCallID,
			nil,
			outHistory,
			nil,
			nil,
			false,
		)
		// Persist the paired interactive history and retire the temporary pre-tool
		// marker in one write. A split save/clear can leave an old marker on disk
		// and incorrectly show crash recovery after a normal interactive pause.
		if err := h.persistSharedInteractivePause(userID, loopID, askUserOutcome.History); err != nil {
			log.Printf("[InFlightTask] shared ask-user finalization flush failed user=%q run=%q err=%v", userID, loopID, err)
			// The paired question only becomes resumable state after its history and
			// marker transition reach disk together. Do not leave an in-memory
			// pending answer that can disappear on restart while the old pre-tool
			// checkpoint is still the durable truth.
			h.pendingAskUser.Delete(userID)
			return h.sharedInteractivePausePersistenceFailureResponse(
				userID, requestID, telemetry, onStreamDone, cb,
			)
		} else {
			cb.checkpointCommitted = false
			cb.hasPendingToolBatch = false
		}
		if askUserOutcome.Response != nil {
			// handleAgentLoopAskUserToolResult deferred this local state mutation
			// while the paired history waited for its atomic checkpoint transition.
			h.commitPendingAskUser(userID, &AskUserRequest{
				Question:  loopResult.AskUser.Question,
				Options:   append([]string(nil), loopResult.AskUser.Options...),
				Context:   loopResult.AskUser.Context,
				InputType: loopResult.AskUser.InputType,
			}, askUserOutcome.History)
		}
		if askUserOutcome.Response != nil {
			return h.finalizeSharedLoopAskUser(userID, loopResult, requestID, telemetry, onStreamDone, cb, startState.Recorder, askUserOutcome.Response)
		}
		if len(loopResult.HistoryDelta) > 0 {
			h.saveConversationHistoryTimed(userID, outHistory, &IMAgentResponse{})
		}
		return h.finalizeSharedLoopAskUser(userID, loopResult, requestID, telemetry, onStreamDone, cb, startState.Recorder, nil)
	}

	// Interactive record_audio pause: open desktop waveform card and stop the loop
	// (parity with legacy handleAgentLoopRecordAudioToolResult). Must run before the
	// generic history save so pending state binds to history that includes the tool result.
	// Trajectory is completed inside finalize so the opened-session tool result is included.
	if loopResult.RecordAudio != nil {
		return h.finalizeSharedLoopRecordAudio(
			userID, platform, userText, outHistory, loopResult, requestID, telemetry, onStreamDone, cb, startState.Recorder,
		)
	}

	// Complete trajectory for shared path: start recorded system/history/user;
	// replay HistoryDelta (assistant + tools) and outcome before Cleanup Flush.
	if startState.Recorder != nil {
		startState.Recorder.SetKind("shared")
		startState.Recorder.RecordLoopResult(loopResult)
	}

	// When the synchronous batch checkpoint failed, do not fall back to the
	// generic asynchronous history save: doing so could later flush a tool batch
	// which was explicitly rejected as unsafe to recover. Preserve only the last
	// successfully checkpointed context and stop the loop instead.
	if shouldSaveSharedLoopTerminalHistory(loopResult, cb) {
		h.saveConversationHistoryTimed(userID, outHistory, &IMAgentResponse{})
	}
	if cb.hasPendingToolBatch {
		return h.interruptedSharedLoopResultResponse(userText, loopResult, requestID, telemetry, onStreamDone, cb)
	}
	// A marker represents incomplete work only. Clear it after a normal shared
	// loop completion, and only when this exact run owns it. Errors/cancellation
	// deliberately retain the latest successful checkpoint for recovery.
	if cb.checkpointCommitted && loopResult.Error == "" && loopResult.AskUser == nil && loopResult.RecordAudio == nil {
		if err := h.memory.CompleteInFlightCheckpointForRun(userID, loopID); err != nil {
			log.Printf("[InFlightTask] shared normal cleanup flush failed user=%q run=%q err=%v", userID, loopID, err)
		}
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
		Reasoning:      sharedLoopDisplayReasoning(loopResult),
		Error:          loopResult.Error,
		HardExit:       loopResult.HardExit,
		RequestID:      requestID,
		SessionKey:     userID,
		ResponseSource: "shared_agent_loop",
	}
	// Attach files materialized during send_file/send_to_im so the desktop UI
	// can show the local path and diagnostics report file_materialize > 0.
	if len(cb.deliveredPaths) > 0 {
		resp.LocalFilePaths = append([]string(nil), cb.deliveredPaths...)
		resp.LocalFilePath = cb.deliveredPaths[0]
		resp.FileMaterializeNanos = cb.fileMaterializeNanos
		if cb.filesForwarded > 0 {
			resp.ResponseSource = imResponseSourceFileDelivery.String()
		}
	}
	if loopResult.AskUser != nil {
		resp.Text = loopResult.Text
		// Mark interactive pause for UI/telemetry (parity with record_audio + legacy ask_user).
		resp.ResponseSource = imResponseSourceAskUser.String()
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
	telemetry.InputBreakdown = cb.inputBreakdown
	telemetry.Attach(resp)
	if onStreamDone != nil {
		onStreamDone()
	}
	_ = loopID
	return resp
}

// sharedLoopDisplayReasoning returns all provider-supplied display-safe
// summaries from a shared loop. Multi-step tool work has multiple assistant
// rounds, so using only the final HistoryDelta entry made the visible record
// look incomplete.
func sharedLoopDisplayReasoning(result agent.LoopResult) string {
	if reasoning := strings.TrimSpace(result.Reasoning); reasoning != "" {
		return reasoning
	}
	for i := len(result.HistoryDelta) - 1; i >= 0; i-- {
		entry := result.HistoryDelta[i]
		if entry.Role != "assistant" {
			continue
		}
		if reasoning := strings.TrimSpace(entry.ReasoningContent); reasoning != "" {
			return reasoning
		}
	}
	return ""
}

// sharedAgentLoopCallbacks adapts IMMessageHandler to agent.LoopCallbacks.
type sharedAgentLoopCallbacks struct {
	handler  *IMMessageHandler
	loopCtx  *LoopContext
	userID   string
	userText string
	// checkpointHistory contains only provider-valid history: a valid prefix plus
	// full, durable tool batches. It is never updated from OnToolExecuted
	// because that callback runs before history commit and would create
	// marker-only recovery states. In particular, a pre-tool checkpoint does
	// not append an unpaired assistant tool-call declaration: strict providers
	// reject that shape on the next request. The pending tool is recorded in
	// checkpoint metadata as diagnostic recovery evidence instead.
	checkpointHistory   []agent.ConversationEntry
	checkpointRunID     string
	checkpointProject   string
	checkpointCommitted bool
	// hasPendingToolBatch is true from a successful pre-tool checkpoint until
	// its complete assistant/tool-result batch is durably committed (or an
	// interactive pause atomically pairs its result). Terminal paths must not
	// asynchronously save HistoryDelta while this is true: it can contain an
	// assistant tool-call declaration with missing results.
	hasPendingToolBatch bool
	// hasLocalFileWork survives the light-to-full upgrade, which occurs after
	// attachment staging and otherwise only has the raw user text to inspect.
	hasLocalFileWork bool
	platform         string // turn platform from runAgentLoopShared (desktop/weixin/…)
	systemPrompt     string
	tools            []map[string]interface{}
	llmCfg           corelib.MaclawLLMConfig
	route            modelRouteDecision
	onProgress       tool.ProgressCallback
	onToken          llm.TokenCallback
	onNewRound       NewRoundCallback
	maxIter          int
	httpClient       *http.Client
	escalated        bool
	toolCalls        int
	// moaPreset is set for the duration of one agent loop after /moa or auto arming.
	moaPreset *moa.ResolvedPreset
	moaAuto   bool
	// File delivery from send_file / send_to_im materialize (shared path only).
	deliveredPaths       []string
	fileMaterializeNanos int64
	filesForwarded       int
	inputBreakdown       agent.LoopInputBreakdown
	// firstRequestBudgetApplied prevents a latency-oriented history cap from
	// constraining later tool rounds, which may legitimately need more context.
	firstRequestBudgetApplied bool
	// Revision last incorporated by TransformConversation. Keeping this at the
	// conversation boundary (rather than the later HTTP-start boundary) closes
	// the race where steering arrives between transform and request creation.
	llmReplanRevision atomic.Int64
}

func (c *sharedAgentLoopCallbacks) OnLoopInputBreakdown(b agent.LoopInputBreakdown) {
	if c != nil {
		c.inputBreakdown = b
	}
}

// effectivePlatform prefers the pinned turn platform, then loop context.
func (c *sharedAgentLoopCallbacks) effectivePlatform() string {
	if c == nil {
		return ""
	}
	if p := strings.TrimSpace(c.platform); p != "" {
		return p
	}
	if c.loopCtx != nil {
		return runtimePlatformFromLoopContext(c.loopCtx)
	}
	return ""
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
		routingText := computerUseRoutingTextForLocalFileWork(c.userText, c.hasLocalFileWork)
		toolSet := c.handler.prepareAgentLoopTools(c.userID, routingText, c.loopCtx, phase)
		c.tools = toolSet.Tools
		// Rebuild full policy surface (profile now full on loopCtx).
		c.systemPrompt = c.handler.buildSystemPromptWithMemory(agent.CompactQueryForEmbedding(routingText), false, c.loopCtx)
	}
	log.Printf("[shared-loop] light→full prompt upgrade reason=%s tools=%d", reason, len(c.tools))
	// A full execution profile can still use a light prompt profile as an
	// adaptive optimization. The upgrade contract is about execution layer and
	// tool re-authorization, so report success once the light layer is gone.
	return c.loopCtx != nil && !c.loopCtx.Runtime.Execution.IsLight()
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
	if localFileWorkBlocksComputerUseExecution(c.loopCtx, c.userText, name) {
		return "[system rejected] Computer Use is unavailable while handling the current local attachment. Use the local file/document tools instead."
	}
	// Defense-in-depth: never execute oversized payloads even if a caller bypasses RunLoop.
	if argSize := len(argsJSON); argSize > guiMaxToolArgumentsBytes {
		toolName := strings.TrimSpace(name)
		if toolName == "" {
			toolName = "unknown"
		}
		return fmt.Sprintf("tool arguments too large for %s: %d bytes exceeds limit %d", toolName, argSize, guiMaxToolArgumentsBytes)
	}
	requestID := ""
	if c.loopCtx != nil {
		requestID = c.loopCtx.Runtime.RequestID
	}
	// Keep permission wait and the actual handler under one operation. A steer
	// arriving anywhere in that window cancels context-aware tools and prevents
	// a newly-created post-permission context from accidentally reviving the
	// stale call.
	execCtx := context.Background()
	if c.loopCtx != nil {
		var end context.CancelFunc
		execCtx, end, _ = c.loopCtx.BeginReplannableOperation(context.Background())
		if end != nil {
			defer end()
		}
	}
	// Close the check/start race in core RunLoop: steering may have landed after
	// its last batch check but before this operation was installed. In that case
	// no cancel function existed for TryRequestReplan to call, so reject the stale
	// tool explicitly before permission UI, progress, or handler side effects.
	if c.LLMReplanRequested() {
		return sharedToolInterruptedText(execCtx)
	}
	toolCallID := newACPToolCallID(name)
	if isACPProgrammingRequestID(requestID) {
		if allowed, reason := globalACPPermission.check(execCtx, requestID, name, argsJSON); !allowed {
			msg := "[system rejected] " + reason
			if strings.TrimSpace(reason) == "" {
				msg = "[system rejected] tool not permitted"
			}
			emitACPToolEventForRequest(requestID, ACPToolEvent{
				Phase: "end", ToolCallID: toolCallID, Name: name, ArgsJSON: argsJSON,
				Result: msg, OK: false, Kind: acpToolKind(name), Title: acpToolTitle(name, argsJSON),
				Paths: acpPathsFromToolArgs(name, argsJSON),
			})
			return msg
		}
		emitACPToolEventForRequest(requestID, ACPToolEvent{
			Phase:      "start",
			ToolCallID: toolCallID,
			Name:       name,
			ArgsJSON:   argsJSON,
			Kind:       acpToolKind(name),
			Paths:      acpPathsFromToolArgs(name, argsJSON),
			Title:      acpToolTitle(name, argsJSON),
		})
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
	platform := c.effectivePlatform()
	if c.loopCtx != nil && c.loopCtx.LansengerGroupPermissions != nil {
		if !c.loopCtx.LansengerGroupPermissions.allowsTool(name) {
			return "[system rejected] 群聊权限未授权该工具访问本地资源或知识库"
		}
	}
	policyUserID := c.handler.workflowPolicyOwnerID(c.userID, c.loopCtx)
	exec := c.handler.executeToolDetailedWithRuntimeContext(
		execCtx,
		policyUserID,
		loopContextHasExplicitRuntimeOwner(c.loopCtx),
		platform,
		name,
		argsJSON,
		c.userText,
		toolProgress,
	)
	// A non-context-aware handler cannot be forcibly stopped once entered, but a
	// steer that arrived before it returned must still suppress downstream side
	// effects such as file materialization/forwarding and interactive UI opening.
	if c.LLMReplanRequested() || execCtx.Err() != nil {
		exec.Text = sharedToolInterruptedText(execCtx)
		exec.Outcome = toolOutcomeFailed
		exec.FailureKind = toolFailureHandlerReported
	}
	// Materialize [file_base64|…|im] before truncating: shared RunLoop has no
	// post-tool artifact branch, so this is the only place desktop→WeChat runs.
	// Pass originating platform so WeChat/Feishu channel turns report channel
	// delivery success (LocalFilePaths) instead of false "sender unconfigured".
	mat := c.handler.materializeToolFilePayloadForPlatform(exec.Text, platform)
	outText := ""
	ok := true
	if mat.Handled {
		c.deliveredPaths = appendUniqueStrings(c.deliveredPaths, mat.LocalPaths...)
		c.fileMaterializeNanos += mat.MaterializeNanos
		if mat.Forwarded {
			c.filesForwarded++
		}
		// Status is short; skip spill of the original multi-MB base64 blob.
		outText = mat.Text
	} else if agent.IsRecordAudioResult(mat.Text) {
		// Keep interactive markers intact: never run size/semantic compression on them
		// (core early-stops only when the prefix survives into RunLoop).
		// Background has no desktop waveform host — match bonus-round policy.
		if c.loopCtx != nil && c.loopCtx.Kind == LoopKindBackground {
			outText = "record_audio is unavailable in background tasks; choose the next action directly."
			ok = false
		} else if rewritten := c.handler.rewriteRecordAudioMarkerForSharedLoop(c.userID, platform, mat.Text); rewritten != "" {
			outText = rewritten
			ok = false
		} else {
			outText = mat.Text
		}
	} else if agent.IsAskUserResult(mat.Text) {
		outText = mat.Text
	} else {
		// Keep raw output through UI/ACP reporting, hooks and drift detection.
		// RunLoop invokes ProjectToolResult exactly once before model commit.
		outText = mat.Text
	}
	trimOut := strings.TrimSpace(outText)
	if strings.HasPrefix(trimOut, "[system rejected]") ||
		strings.HasPrefix(strings.ToLower(trimOut), "error:") {
		ok = false
	}
	if isACPProgrammingRequestID(requestID) {
		paths := acpPathsFromToolArgs(name, argsJSON)
		if mat.Handled && len(mat.LocalPaths) > 0 {
			paths = append(paths, mat.LocalPaths...)
		}
		emitACPToolEventForRequest(requestID, ACPToolEvent{
			Phase:      "end",
			ToolCallID: toolCallID,
			Name:       name,
			ArgsJSON:   argsJSON,
			Result:     outText,
			OK:         ok,
			Kind:       acpToolKind(name),
			Paths:      paths,
			Title:      acpToolTitle(name, argsJSON),
		})
	}
	return outText
}

func sharedToolInterruptedText(ctx context.Context) string {
	reason := context.Canceled.Error()
	if ctx != nil && ctx.Err() != nil {
		reason = ctx.Err().Error()
	}
	return "tool execution interrupted: " + reason
}

// rewriteRecordAudioMarkerForSharedLoop returns a non-empty rejection string when
// the host must not open the interactive recording UI (IM channels, concurrent
// session). Empty string means the marker is valid for core early-stop.
func (h *IMMessageHandler) rewriteRecordAudioMarkerForSharedLoop(userID, platform, marker string) string {
	if h == nil || !agent.IsRecordAudioResult(marker) {
		return ""
	}
	req, ok := agent.ParseRecordAudioResult(marker)
	if !ok {
		return ""
	}
	if !normalizeIMMessagePlatformKind(platform).IsDesktop() {
		log.Printf("[record-audio] shared: rejected on non-desktop platform user=%s platform=%s title=%q", userID, platform, req.Title)
		return recordAudioDesktopOnlyRejection()
	}
	if rawPending, loaded := h.pendingRecordAudio.Load(userID); loaded {
		var history []agent.ConversationEntry
		if h.memory != nil {
			history = h.memory.Load(userID)
		}
		if pending, fresh := pendingRecordAudioForCurrentHistory(rawPending, history); fresh && pending != nil {
			log.Printf("[record-audio] shared: rejected concurrent session user=%s active_title=%q new_title=%q", userID, pending.Title, req.Title)
			return recordAudioConcurrentRejection(pending.Title)
		}
	}
	return ""
}

// finalizeSharedLoopRecordAudio opens the desktop recording session after the
// shared RunLoop early-stopped on a record_audio marker (ResponseSource=record_audio).
func (h *IMMessageHandler) finalizeSharedLoopRecordAudio(
	userID, platform, userText string,
	outHistory []agent.ConversationEntry,
	loopResult agent.LoopResult,
	requestID string,
	telemetry *agentLoopTelemetry,
	onStreamDone StreamDoneCallback,
	cb *sharedAgentLoopCallbacks,
	recorder *TrajectoryRecorder,
) *IMAgentResponse {
	req := loopResult.RecordAudio
	if req == nil {
		if recorder != nil {
			recorder.SetKind("shared")
			recorder.RecordLoopResult(loopResult)
		}
		return &IMAgentResponse{
			Text:           loopResult.Text,
			RequestID:      requestID,
			SessionKey:     userID,
			ResponseSource: "shared_agent_loop",
		}
	}
	raw := agent.FormatRecordAudioMarker(req)
	// Prefer the id of the tool that triggered the pause (multi-tool batch safe).
	tcID := strings.TrimSpace(loopResult.PauseToolCallID)
	if tcID == "" {
		// Fallback: name-match record_audio in the assistant batch (not merely last id).
		tcID = toolCallIDFromHistoryDeltaByName(loopResult.HistoryDelta, "record_audio")
	}
	if tcID == "" {
		tcID = "record_audio_shared"
	}
	out := h.handleAgentLoopRecordAudioToolResult(
		userID, platform, userText, raw, false, tcID,
		nil, outHistory, nil, nil, false,
	)
	pairedHistory := out.History
	if out.Response == nil && tcID != "" && strings.TrimSpace(out.Result) != "" {
		pairedHistory = append(append([]agent.ConversationEntry(nil), outHistory...), agent.ConversationEntry{
			Role:        "tool",
			Content:     out.Result,
			ToolCallID:  tcID,
			ToolName:    "record_audio",
			ToolOutcome: toolOutcomeUncertain.String(),
		})
	}
	// Like ask_user, record_audio replaces autonomous execution with a paired
	// interactive state. Persist it and retire the temporary marker together.
	var pausePersistErr error
	if cb != nil {
		if err := h.persistSharedInteractivePause(userID, cb.checkpointRunID, pairedHistory); err != nil {
			log.Printf("[InFlightTask] shared record-audio finalization flush failed user=%q run=%q err=%v", userID, cb.checkpointRunID, err)
			// Mirror ask_user: pending recording UI is only valid after the paired
			// transcript has been committed. Otherwise a restart loses the UI state
			// but retains the earlier recovery marker.
			h.pendingRecordAudio.Delete(userID)
			pausePersistErr = err
		} else {
			cb.checkpointCommitted = false
			cb.hasPendingToolBatch = false
		}
	}
	if pausePersistErr == nil && out.Response != nil {
		// The handler intentionally deferred this local state mutation while the
		// paired history was waiting for its atomic checkpoint transition.
		h.commitPendingRecordAudio(userID, req, pairedHistory)
	}

	// Trajectory: HistoryDelta lacks the tool result (core early-stops before append);
	// record delta then the friendly/rejection tool result so sessions stay paired.
	if recorder != nil {
		recorder.SetKind("shared")
		recorder.RecordLoopResult(loopResult)
		if out.Response != nil && pausePersistErr == nil {
			// Interactive pause succeeded — pair the opened-session tool result.
			recorder.RecordEarlyStopToolResult(tcID, "record_audio", out.Result)
		} else {
			// Host rejected after the loop already paused on the marker (IM channel,
			// concurrent session race). Keep pairing but stamp error, not paused.
			toolContent := strings.TrimSpace(out.Result)
			if toolContent == "" {
				toolContent = "record_audio rejected by host"
			}
			recorder.RecordEntry(TrajectoryEntry{
				Role:        "tool_result",
				Content:     toolContent,
				ToolCallID:  tcID,
				ToolName:    "record_audio",
				ToolOutcome: "failed",
			})
			recorder.CloseUnpairedToolCalls("loop_paused")
			recorder.SetOutcome("error", toolContent, loopResult.Iterations, loopResult.ToolCalls, -1, -1)
		}
	}
	if pausePersistErr != nil {
		return h.sharedInteractivePausePersistenceFailureResponse(
			userID, requestID, telemetry, onStreamDone, cb,
		)
	}

	if out.Response != nil {
		resp := out.Response
		resp.RequestID = requestID
		resp.SessionKey = userID
		// Preserve usage/route when the pause happened after tool rounds.
		if loopResult.Usage.InputTokens > 0 || loopResult.Usage.OutputTokens > 0 {
			resp.InputTokens = loopResult.Usage.InputTokens
			resp.OutputTokens = loopResult.Usage.OutputTokens
			resp.TotalTokens = loopResult.Usage.TotalTokens()
			resp.CacheReadTokens = loopResult.Usage.CachedTokens
			resp.CacheWriteTokens = loopResult.Usage.CacheWriteTokens
			resp.EstCostRMB = loopResult.Usage.EstCostRMB
			if telemetry != nil {
				telemetry.LastLLMInputTokens = loopResult.Usage.InputTokens
				telemetry.LastLLMOutputTokens = loopResult.Usage.OutputTokens
				telemetry.LastLLMCacheReadTokens = loopResult.Usage.CachedTokens
				telemetry.LastLLMCacheWriteTokens = loopResult.Usage.CacheWriteTokens
			}
		}
		if cb != nil {
			liveRoute := cb.currentRouteDecision()
			if liveRoute.Task != "" || liveRoute.Model != "" {
				if telemetry != nil {
					telemetry.Route = liveRoute
				}
			}
		}
		if telemetry != nil {
			telemetry.Attach(resp)
		}
		if onStreamDone != nil {
			onStreamDone()
		}
		log.Printf("[record-audio] shared: session UI opened user=%s platform=%s title=%q tc=%s source=record_audio",
			userID, platform, req.Title, tcID)
		return resp
	}
	// Unexpected rejection after ExecuteTool precheck (race on concurrent session).
	text := recordAudioUserFacingRejectText(out.Result, loopResult.Text)
	// Keep tool-call/result pairing when we have a rewritten rejection.
	if cb == nil && tcID != "" && strings.TrimSpace(out.Result) != "" {
		h.saveConversationHistoryTimed(userID, pairedHistory, nil)
	} else if cb == nil && len(loopResult.HistoryDelta) > 0 {
		h.saveConversationHistoryTimed(userID, outHistory, nil)
	}
	resp := &IMAgentResponse{
		Text:           text,
		RequestID:      requestID,
		SessionKey:     userID,
		ResponseSource: "shared_agent_loop",
	}
	if telemetry != nil {
		telemetry.Attach(resp)
	}
	if onStreamDone != nil {
		onStreamDone()
	}
	return resp
}

// shouldSaveSharedLoopTerminalHistory rejects the generic terminal save when
// the shared core stopped mid-batch. In that state HistoryDelta includes an
// assistant tool-call declaration without every matching tool result; the
// pre-tool checkpoint is the only durable provider-valid representation.
func shouldSaveSharedLoopTerminalHistory(loopResult agent.LoopResult, cb *sharedAgentLoopCallbacks) bool {
	if len(loopResult.HistoryDelta) == 0 || loopResult.Error == "recovery_checkpoint_failed" {
		return false
	}
	return cb == nil || !cb.hasPendingToolBatch
}

// interruptedSharedLoopResultResponse keeps the UI outcome aligned with the
// durable state for non-cancel terminal exits (argument validation, policy
// failures, hard-stop guards, and max rounds) that happen after a pre-tool
// checkpoint but before a complete batch commit. Returning the raw loop error
// would suggest ordinary retry even though recovery must begin from the saved
// provider-valid prefix and explicit review marker.
func (h *IMMessageHandler) interruptedSharedLoopResultResponse(
	userText string,
	loopResult agent.LoopResult,
	requestID string,
	telemetry *agentLoopTelemetry,
	onStreamDone StreamDoneCallback,
	cb *sharedAgentLoopCallbacks,
) *IMAgentResponse {
	resp := h.interruptedSharedLoopExitResponse(userText)
	resp.RequestID = requestID
	if cb != nil {
		resp.SessionKey = cb.userID
	}
	resp.ResponseSource = "shared_agent_loop"
	resp.HardExit = true
	if loopResult.Usage.InputTokens > 0 || loopResult.Usage.OutputTokens > 0 {
		resp.InputTokens = loopResult.Usage.InputTokens
		resp.OutputTokens = loopResult.Usage.OutputTokens
		resp.TotalTokens = loopResult.Usage.TotalTokens()
		resp.CacheReadTokens = loopResult.Usage.CachedTokens
		resp.CacheWriteTokens = loopResult.Usage.CacheWriteTokens
		resp.EstCostRMB = loopResult.Usage.EstCostRMB
	}
	if telemetry != nil {
		if cb != nil {
			if route := cb.currentRouteDecision(); route.Task != "" || route.Model != "" {
				telemetry.Route = route
			}
		}
		telemetry.Attach(resp)
	}
	if onStreamDone != nil {
		onStreamDone()
	}
	return resp
}

// sharedInteractivePausePersistenceFailureResponse fails closed when the
// paired interactive history cannot be committed with the run-owned recovery
// marker. Showing a card here would invite a reply that the process cannot
// safely associate with durable history after a restart.
func (h *IMMessageHandler) sharedInteractivePausePersistenceFailureResponse(
	userID, requestID string,
	telemetry *agentLoopTelemetry,
	onStreamDone StreamDoneCallback,
	cb *sharedAgentLoopCallbacks,
) *IMAgentResponse {
	resp := &IMAgentResponse{
		Text:           "无法安全保存本次交互进度，已停止打开交互卡片。请重试；若应用已退出，请重启后从恢复任务继续。",
		Error:          "recovery_checkpoint_failed",
		RequestID:      requestID,
		SessionKey:     userID,
		ResponseSource: "shared_agent_loop",
	}
	if cb != nil && telemetry != nil {
		if route := cb.currentRouteDecision(); route.Task != "" || route.Model != "" {
			telemetry.Route = route
		}
	}
	if telemetry != nil {
		telemetry.Attach(resp)
	}
	if onStreamDone != nil {
		onStreamDone()
	}
	return resp
}

// finalizeSharedLoopAskUser records the shared loop's interactive question as
// a terminal UI response. The core loop deliberately returns before appending
// a synthetic tool result, so this helper must not turn the pause into a
// recoverable in-flight task or ask the model to replay it.
func (h *IMMessageHandler) finalizeSharedLoopAskUser(
	userID string,
	loopResult agent.LoopResult,
	requestID string,
	telemetry *agentLoopTelemetry,
	onStreamDone StreamDoneCallback,
	cb *sharedAgentLoopCallbacks,
	recorder *TrajectoryRecorder,
	baseResponse *IMAgentResponse,
) *IMAgentResponse {
	if recorder != nil {
		recorder.SetKind("shared")
		recorder.RecordLoopResult(loopResult)
		tcID := strings.TrimSpace(loopResult.PauseToolCallID)
		if tcID == "" {
			tcID = toolCallIDFromHistoryDeltaByName(loopResult.HistoryDelta, "ask_user")
		}
		content := strings.TrimSpace(loopResult.Text)
		if content == "" {
			content = agent.FormatAskUserForDisplay(loopResult.AskUser)
		}
		recorder.RecordEarlyStopToolResult(tcID, "ask_user", content)
	}
	resp := baseResponse
	if resp == nil {
		resp = &IMAgentResponse{Text: loopResult.Text, ResponseSource: imResponseSourceAskUser.String()}
	}
	if strings.TrimSpace(resp.Text) == "" {
		resp.Text = loopResult.Text
	}
	resp.Reasoning = sharedLoopDisplayReasoning(loopResult)
	resp.RequestID = requestID
	resp.SessionKey = userID
	resp.ResponseSource = imResponseSourceAskUser.String()
	if loopResult.Usage.InputTokens > 0 || loopResult.Usage.OutputTokens > 0 {
		resp.InputTokens = loopResult.Usage.InputTokens
		resp.OutputTokens = loopResult.Usage.OutputTokens
		resp.TotalTokens = loopResult.Usage.TotalTokens()
		resp.CacheReadTokens = loopResult.Usage.CachedTokens
		resp.CacheWriteTokens = loopResult.Usage.CacheWriteTokens
		resp.EstCostRMB = loopResult.Usage.EstCostRMB
		if telemetry != nil {
			telemetry.LastLLMInputTokens = loopResult.Usage.InputTokens
			telemetry.LastLLMOutputTokens = loopResult.Usage.OutputTokens
			telemetry.LastLLMCacheReadTokens = loopResult.Usage.CachedTokens
			telemetry.LastLLMCacheWriteTokens = loopResult.Usage.CacheWriteTokens
		}
	}
	if cb != nil && telemetry != nil {
		if route := cb.currentRouteDecision(); route.Task != "" || route.Model != "" {
			telemetry.Route = route
		}
	}
	if telemetry != nil {
		telemetry.Attach(resp)
	}
	if onStreamDone != nil {
		onStreamDone()
	}
	return resp
}

// toolCallIDFromHistoryDeltaByName finds the last assistant tool_call whose
// function name matches (case-insensitive). Safer multi-tool fallback than
// "last id in batch" when PauseToolCallID is missing.
func toolCallIDFromHistoryDeltaByName(delta []agent.ConversationEntry, toolName string) string {
	want := strings.ToLower(strings.TrimSpace(toolName))
	if want == "" {
		return ""
	}
	for i := len(delta) - 1; i >= 0; i-- {
		if delta[i].Role != "assistant" || delta[i].ToolCalls == nil {
			continue
		}
		found := ""
		for _, tc := range extractNamedToolCallsForShared(delta[i].ToolCalls) {
			if strings.ToLower(tc.Name) == want && tc.ID != "" {
				found = tc.ID // prefer last match within the batch
			}
		}
		if found != "" {
			return found
		}
	}
	return ""
}

type namedToolCallID struct {
	ID   string
	Name string
}

// extractNamedToolCallsForShared extracts id+name pairs from assistant ToolCalls.
func extractNamedToolCallsForShared(toolCalls interface{}) []namedToolCallID {
	if arr, ok := toolCalls.([]interface{}); ok {
		out := make([]namedToolCallID, 0, len(arr))
		for _, item := range arr {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			id, _ := m["id"].(string)
			name := ""
			if fn, ok := m["function"].(map[string]interface{}); ok {
				name, _ = fn["name"].(string)
			}
			if id != "" || name != "" {
				out = append(out, namedToolCallID{ID: id, Name: name})
			}
		}
		return out
	}
	// Typed slice (e.g. []llm.ToolCall) — JSON round-trip.
	type fn struct {
		Name string `json:"name"`
	}
	type call struct {
		ID       string `json:"id"`
		Function fn     `json:"function"`
	}
	data, err := json.Marshal(toolCalls)
	if err != nil {
		return nil
	}
	var calls []call
	if json.Unmarshal(data, &calls) != nil {
		return nil
	}
	out := make([]namedToolCallID, 0, len(calls))
	for _, c := range calls {
		if c.ID != "" || c.Function.Name != "" {
			out = append(out, namedToolCallID{ID: c.ID, Name: c.Function.Name})
		}
	}
	return out
}

// appendUniqueStrings appends values not already present (order-preserving).
func appendUniqueStrings(dst []string, values ...string) []string {
	seen := make(map[string]bool, len(dst)+len(values))
	for _, s := range dst {
		seen[s] = true
	}
	for _, s := range values {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		dst = append(dst, s)
	}
	return dst
}

func (c *sharedAgentLoopCallbacks) ExecuteToolStructured(name, argsJSON string) agent.ToolExecutionResult {
	text := c.ExecuteTool(name, argsJSON)
	outcome := agent.ToolExecutionOutcomeOK
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "[错误]") ||
		strings.Contains(strings.ToLower(text), "tool execution interrupted:") ||
		strings.Contains(strings.ToLower(text), "context canceled") ||
		strings.Contains(text, "工具执行异常") ||
		strings.Contains(text, "未知工具") ||
		strings.Contains(text, "无法转发") ||
		strings.Contains(text, "Could not forward") ||
		strings.Contains(text, "Failed to save") ||
		(strings.Contains(text, "保存") && strings.Contains(text, "失败")) {
		outcome = agent.ToolExecutionOutcomeError
	}
	if strings.Contains(text, "命令超时") {
		outcome = agent.ToolExecutionOutcomeTimeout
	}
	return agent.ToolExecutionResult{Result: text, Outcome: outcome}
}

// ProjectToolResult implements agent.ToolResultProjector. It deliberately uses
// the same dual-view projector as the legacy GUI loop.
func (c *sharedAgentLoopCallbacks) ProjectToolResult(name string, result agent.ToolExecutionResult) string {
	sessionKey := ""
	if c != nil {
		sessionKey = c.userID
		if c.handler != nil {
			sessionKey = c.handler.workflowPolicyOwnerID(c.userID, c.loopCtx)
		}
	}
	return truncateToolResultForToolWithSession(name, sessionKey, result.Result)
}

func (c *sharedAgentLoopCallbacks) OnToken(delta string) {
	if c.onToken != nil {
		c.onToken(delta)
	}
}

// OnLLMNewRound implements agent.LLMRoundNotifier. A live-steer replacement
// uses the same stream-generation boundary as an ordinary next tool round.
func (c *sharedAgentLoopCallbacks) OnLLMNewRound() {
	if c != nil && c.onNewRound != nil {
		c.onNewRound()
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

// OnToolBatchStarting implements agent.ToolBatchStarter. It durably records
// the last provider-valid history prefix before a tool begins. The supplied
// delta intentionally contains an unpaired assistant declaration, so it is
// evidence only and must not be persisted as conversation history. The core
// loop labels this checkpoint external_uncertain because a process crash may
// occur after the tool has changed state but before a result can be paired.
func (c *sharedAgentLoopCallbacks) OnToolBatchStarting(delta []agent.ConversationEntry, meta agent.ToolBatchMetadata) error {
	if c == nil || c.handler == nil || len(delta) == 0 {
		return nil
	}
	// Do not persist delta here. At this point it contains an assistant
	// tool_calls message without matching tool results and would make the
	// restored provider conversation invalid. LastToolName records the first
	// possible action without granting permission to replay it.
	entries := c.trimCheckpointHistory()
	err := c.handler.persistRecoveryCheckpoint(
		c.userID,
		c.userText,
		c.checkpointProject,
		c.checkpointRunID,
		entries,
		agent.InFlightCheckpoint{
			Sequence:        meta.Sequence,
			LastToolName:    meta.LastToolName,
			SideEffectState: meta.SideEffectState,
		},
	)
	if err != nil {
		log.Printf("[InFlightTask] shared pre-tool checkpoint failed user=%q run=%q seq=%d err=%v", c.userID, c.checkpointRunID, meta.Sequence, err)
		return err
	}
	c.checkpointCommitted = true
	c.hasPendingToolBatch = true
	return nil
}

// OnToolBatchAbandoned notes that an interactive tool paused the batch before
// sibling calls could execute. The marker remains until the host atomically
// writes the paired interactive history and clears it; clearing here would
// reopen a crash window between those two durable transitions.
func (c *sharedAgentLoopCallbacks) OnToolBatchAbandoned(meta agent.ToolBatchMetadata) {
	_ = meta
}

// OnToolBatchCommitted implements agent.ToolBatchCommitter. RunLoop invokes it
// after the entire assistant tool-call batch and every paired tool result have
// been appended to HistoryDelta. A persistence error is returned to RunLoop so
// it stops before executing a following batch with further side effects.
func (c *sharedAgentLoopCallbacks) OnToolBatchCommitted(delta []agent.ConversationEntry, meta agent.ToolBatchMetadata) error {
	if c == nil || c.handler == nil || len(delta) == 0 {
		return nil
	}
	c.checkpointHistory = append(c.checkpointHistory, delta...)
	entries := c.trimCheckpointHistory()
	err := c.handler.persistRecoveryCheckpoint(
		c.userID,
		c.userText,
		c.checkpointProject,
		c.checkpointRunID,
		entries,
		agent.InFlightCheckpoint{
			Sequence:        meta.Sequence,
			LastToolName:    meta.LastToolName,
			SideEffectState: meta.SideEffectState,
		},
	)
	if err != nil {
		log.Printf("[InFlightTask] shared checkpoint failed user=%q run=%q seq=%d err=%v", c.userID, c.checkpointRunID, meta.Sequence, err)
		return err
	}
	c.checkpointCommitted = true
	c.hasPendingToolBatch = false
	return nil
}

// trimCheckpointHistory bounds synchronous checkpoint files during long tool
// loops. TrimHistory operates on complete entry groups, so it never splits an
// assistant tool-call declaration from any of its results.
func (c *sharedAgentLoopCallbacks) trimCheckpointHistory() []agent.ConversationEntry {
	if c == nil {
		return nil
	}
	c.checkpointHistory = agent.TrimHistory(c.checkpointHistory)
	return c.checkpointHistory
}

func (c *sharedAgentLoopCallbacks) OnEmptyResponse(iteration int) bool {
	_ = iteration
	return false
}

func (c *sharedAgentLoopCallbacks) TransformConversation(conversation []interface{}) []interface{} {
	if c == nil || c.handler == nil {
		return nil
	}
	// Snapshot before draining. If steering lands at any point after this read
	// (including while the pending bag is being drained), its newer revision
	// remains visible to LLMReplanRequested and cannot be mistaken for content
	// already incorporated into this request.
	processedRevision := int64(0)
	if c.loopCtx != nil {
		processedRevision = c.loopCtx.ReplanRevision()
	}
	next, injected := c.handler.appendPendingSteerInjections(c.userID, conversation, 0)
	if c.loopCtx != nil {
		c.llmReplanRevision.Store(processedRevision)
	}
	if injected == "" {
		next = conversation
	}
	effectiveLimit := c.llmCfg.EffectiveContextTokens()
	if !c.firstRequestBudgetApplied {
		c.firstRequestBudgetApplied = true
		beforeTokens := estimateConversationTokens(next) + agent.EstimateToolsTokens(c.tools)
		budgetedLimit := firstAgentLoopRequestTokenLimit(effectiveLimit, next, c.tools)
		compacted := c.handler.compactAgentLoopConversation(c.loopCtx, c.userID, next, c.tools, budgetedLimit, agent.EstimateToolsTokens(c.tools), true)
		afterTokens := estimateConversationTokens(compacted) + agent.EstimateToolsTokens(c.tools)
		if afterTokens < beforeTokens || budgetedLimit < effectiveLimit {
			loopID := ""
			if c.loopCtx != nil {
				loopID = c.loopCtx.ID
			}
			log.Printf("[first-request-budget] loop=%q before~=%d after~=%d limit=%d normal_limit=%d tools=%d",
				loopID, beforeTokens, afterTokens, budgetedLimit, effectiveLimit, len(c.tools))
		}
		if injected == "" && sameConversationElements(compacted, conversation) {
			return nil
		}
		return compacted
	}
	compacted := c.handler.compactAgentLoopConversation(c.loopCtx, c.userID, next, c.tools, effectiveLimit, agent.EstimateToolsTokens(c.tools), false)
	if injected == "" && sameConversationElements(compacted, conversation) {
		return nil
	}
	return compacted
}

func sameConversationElements(a, b []interface{}) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !sameConversationElement(a[i], b[i]) {
			return false
		}
	}
	return true
}

func sameConversationElement(a, b interface{}) bool {
	switch av := a.(type) {
	case map[string]interface{}:
		bv, ok := b.(map[string]interface{})
		return ok && reflect.ValueOf(av).Pointer() == reflect.ValueOf(bv).Pointer()
	case map[string]string:
		bv, ok := b.(map[string]string)
		return ok && reflect.ValueOf(av).Pointer() == reflect.ValueOf(bv).Pointer()
	default:
		return reflect.DeepEqual(a, b)
	}
}

// LLMReplanRequested implements agent.LLMReplanAware. RequestReplan cancels
// only the current model operation; corelib then starts another iteration and
// TransformConversation injects the user's live steering.
func (c *sharedAgentLoopCallbacks) LLMReplanRequested() bool {
	return c != nil && c.loopCtx != nil && c.loopCtx.ReplanRequestedSince(c.llmReplanRevision.Load())
}

// TryFinalizeLLMResponse implements agent.LLMFinalizationGuard. The same lock
// used by InjectGuideReference decides whether the final answer or the user's
// interruption wins; accepted steering can no longer be stranded after commit.
func (c *sharedAgentLoopCallbacks) TryFinalizeLLMResponse() bool {
	return c == nil || c.loopCtx == nil || c.loopCtx.TrySealReplans(c.llmReplanRevision.Load())
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
		ctx, end, _ := c.loopCtx.BeginReplannableOperation(context.Background())
		trace := llm.RequestTrace{
			Caller:    "shared_agent_loop",
			OwnerID:   c.userID,
			RequestID: c.loopCtx.Runtime.RequestID,
			LoopID:    c.loopCtx.ID,
			Iteration: iteration,
		}
		ctx = llm.WithRequestTrace(ctx, trace)
		return ctx, func(error) { end() }, nil
	}
	return nil, nil, fmt.Errorf("no loop context")
}
