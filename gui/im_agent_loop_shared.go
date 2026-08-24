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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/doctor"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/llm/moa"
	"github.com/RapidAI/CodeClaw/corelib/swarm"
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
	if startState.HostReject != nil {
		if startState.Cleanup != nil {
			startState.Cleanup()
		}
		return startState.HostReject
	}
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
		taskAnchor:        startState.TaskAnchor,
		tools:             startState.Tools,
		semanticSurface:   startState.SemanticSurface,
		legacySurface:     newLegacyToolSurfaceWithClientTools(startState.Tools, startState.ClientToolNames),
		llmCfg:            cfg,
		route:             startState.RouteDecision,
		phase:             startState.Phase,
		onProgress:        progressOut,
		onToken:           onToken,
		onNewRound:        onNewRound,
		maxIter:           startState.EffectiveMax,
		httpClient:        startState.HTTPClient,
	}
	if cb.maxIter <= 0 {
		cb.maxIter = startState.MaxIterations
	}
	// Cancellation must revoke the durable surface before an in-flight provider
	// response can reach the callback. The hook is scoped to this loop and is
	// removed after RunLoop returns, so a completed turn cannot cancel a later
	// surface on a reused LoopContext.
	removeSemanticCancelHook := func() {}
	if ctx != nil && cb.semanticSurface != nil {
		removeSemanticCancelHook = ctx.RegisterCancelHook(cb.cancelManagedSemanticSurface)
	}
	defer removeSemanticCancelHook()
	// The replacement fence belongs to the inbound turn rather than the loop
	// lifetime. A normal completed turn must unregister it so a later fresh
	// request does not attempt to cancel an already-settled historical surface.
	// On replacement/cancellation the owner drains it before this defer runs.
	defer func() {
		if cb.semanticSurface != nil && cb.semanticSurface.removeTurnFence != nil {
			cb.semanticSurface.removeTurnFence()
			cb.semanticSurface.removeTurnFence = nil
		}
	}()
	// Seed from the context so a leftover pre-loop revision is not treated as
	// mid-task steer (which would clear Live/Open on a resumed workspace).
	if ctx != nil {
		cb.llmReplanRevision.Store(ctx.ReplanRevision())
	}

	// cb also implements ToolBatchCommitter. The durable checkpoint hook runs
	// only after all results in a tool batch have been paired in HistoryDelta.
	// History kept for persistence still has leftover invoke_* names. The
	// model-facing copy only rewrites leftover previous_turn_tool onto
	// web_search when that grant is live. A sole PDF/write grant must not
	// inherit last turn's search placeholder.
	loopHistory := history
	if cb.semanticSurface != nil {
		loopHistory = rewriteExpiredSemanticGrantNames(history, cb.semanticSurface.liveGrantNames())
	}
	loopResult := agent.RunLoopWithUserContent(cb, userText, userContent, loopHistory, startState.HTTPClient, cb)
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
		cb.skipHostAutoFileDelivery = true
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
			cb.semanticDurabilityBlocked = true
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
			// The interactive assistant/tool pair is now durable and the pre-tool
			// recovery marker has been retired in the same write. Only at this
			// boundary may a held dependant semantic capability be issued.
			cb.releaseSemanticDependantIssue()
		}
		if askUserOutcome.Response != nil {
			// handleAgentLoopAskUserToolResult deferred this local state mutation
			// while the paired history waited for its atomic checkpoint transition.
			h.commitPendingAskUser(userID, &AskUserRequest{
				Question:  loopResult.AskUser.Question,
				Options:   append([]string(nil), loopResult.AskUser.Options...),
				Context:   loopResult.AskUser.Context,
				InputType: loopResult.AskUser.InputType,
			}, askUserOutcome.History, loopResult.WorkingState)
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
		cb.skipHostAutoFileDelivery = true
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
		text := strings.TrimSpace(sharedLoopUserFacingText(loopResult.Text))
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
		Text:           sharedLoopUserFacingText(loopResult.Text),
		Reasoning:      sharedLoopDisplayReasoning(loopResult),
		Error:          loopResult.Error,
		HardExit:       loopResult.HardExit,
		RequestID:      requestID,
		SessionKey:     userID,
		ResponseSource: "shared_agent_loop",
	}
	attachSharedLoopArtifacts(resp, cb)
	if loopResult.AskUser != nil {
		resp.Text = sharedLoopUserFacingText(loopResult.Text)
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

// attachSharedLoopArtifacts projects artifacts recorded while executing the
// shared agent loop into the final gateway response. Gateway implementations
// use ImageKey to upload an image reply (including Lansenger), while local file
// paths are delivered as regular attachment messages.
func attachSharedLoopArtifacts(resp *IMAgentResponse, cb *sharedAgentLoopCallbacks) {
	if resp == nil || cb == nil {
		return
	}
	cb.releaseSemanticDependantIssue()
	// Do not let terminal artifact projection turn an in-memory, uncommitted
	// batch into externally visible work.  In particular, the host-owned PDF
	// and current-channel delivery fallbacks below can materialize a successor
	// grant directly from an assistant report, bypassing the delayed-release
	// helper.  A pending pre-tool checkpoint is the durable source of truth, so
	// this entire projection must wait for the batch commit (or the interactive
	// paired-state commit) that clears the marker.
	if cb.hasPendingToolBatch || cb.semanticDurabilityBlocked {
		return
	}
	// Document generate is host-owned for the same reason as file delivery:
	// after search unlocks generate_pdf, flash models often write "请稍候"
	// and stop instead of calling the newly listed grant. A repeat weather+PDF
	// turn can skip search entirely and reuse history; host still issues
	// generate from the assistant report so the PDF is not lost behind an
	// unused lookup edge.
	cb.flushHostOwnedDocumentGenerate(resp)
	cb.flushHostOwnedLiveDataVisual(resp)
	// Current-channel file delivery is host-owned and has an empty schema. The
	// model often writes "PDF delivered" after generate_pdf and never calls the
	// follow-up grant, which left desktop chat with text and no attachment.
	cb.flushHostOwnedCurrentChannelFileDelivery(resp)
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
	if cb.semanticDeliveryImageKey != "" {
		resp.ImageKey = cb.semanticDeliveryImageKey
		resp.SemanticDelivery = cb.semanticDelivery
		resp.ResponseSource = imResponseSourceScreenshot.String()
	} else if cb.semanticDeliveryFileData != "" {
		resp.FileData = cb.semanticDeliveryFileData
		resp.FileName = cb.semanticDeliveryFileName
		resp.FileMimeType = cb.semanticDeliveryFileMIME
		resp.SemanticDelivery = cb.semanticDelivery
		resp.ResponseSource = imResponseSourceFileDelivery.String()
		materializeSemanticDeliveryFileForLocalChat(resp, cb)
	} else if cb.semanticDeliveryVoiceData != "" {
		resp.VoiceData = cb.semanticDeliveryVoiceData
		resp.VoiceFileName = cb.semanticDeliveryVoiceName
		resp.VoiceMimeType = cb.semanticDeliveryVoiceMIME
		resp.SemanticDelivery = cb.semanticDelivery
	} else if cb.screenshotImageKey != "" {
		resp.ImageKey = cb.screenshotImageKey
		resp.ResponseSource = imResponseSourceScreenshot.String()
	}
	finalizeHostOwnedFileResponse(resp, cb)
}

// materializeSemanticDeliveryFileForLocalChat writes a desktop/TUI PDF (or
// other file) to the handler-owned artifact directory. The desktop panel only
// renders LocalFilePath; FileData is for IM SendMedia and must stay off the
// Wails event after a successful local save. Weixin/Lansenger keep FileData
// and must not also get a path here, or the gateway would send the file twice.
func materializeSemanticDeliveryFileForLocalChat(resp *IMAgentResponse, cb *sharedAgentLoopCallbacks) {
	if resp == nil || cb == nil || cb.handler == nil || strings.TrimSpace(resp.FileData) == "" {
		return
	}
	switch normalizeIMMessagePlatformKind(cb.effectivePlatform()) {
	case imMessagePlatformDesktop, imMessagePlatformTUI:
	default:
		return
	}
	started := time.Now()
	path, err := cb.handler.saveFileDataToLocal(resp.FileName, resp.FileData)
	if err != nil {
		log.Printf("[semantic-delivery] local chat file materialize failed: %v", err)
		return
	}
	attachLocalPreview(resp, path, "")
	resp.FileData = ""
	if resp.FileMaterializeNanos == 0 {
		resp.FileMaterializeNanos = time.Since(started).Nanoseconds()
	}
}

func (c *sharedAgentLoopCallbacks) flushHostOwnedDocumentGenerate(resp *IMAgentResponse) {
	if c == nil || c.skipHostAutoFileDelivery || c.semanticSurface == nil {
		return
	}
	if hostOwnedPDFBlockedByResponse(resp) {
		return
	}
	if err := c.issueHostOwnedGenerateFromAvailableEvidence(resp); err != nil {
		log.Printf("[semantic] host generate issue from available evidence failed: %v", err)
	}
	name, grant := soleLiveSemanticGrantByAdapter(c.semanticSurface, "generate_pdf")
	if name == "" {
		return
	}
	selection, ok := semanticSelectionByID(c.semanticSurface.plan, grant.SelectionID)
	if !ok || selection.FitProof.MatchedCapability != "document.generate.file" {
		return
	}
	assistant := ""
	if resp != nil {
		assistant = resp.Text
	}
	title := hostOwnedPDFReportTitle(c.userText)
	content := hostOwnedPDFReportContent(assistant, c.semanticLookupEvidence, title)
	if strings.TrimSpace(content) == "" {
		return
	}
	payload, err := json.Marshal(map[string]string{"content": content, "title": title})
	if err != nil {
		log.Printf("[semantic] host auto generate_pdf marshal failed: %v", err)
		return
	}
	got := c.ExecuteToolCall(name, string(payload), "host-auto-generate-pdf").Result
	if !hostOwnedGeneratePDFSucceeded(got) {
		log.Printf("[semantic] host auto generate_pdf failed: %s", got)
		return
	}
	if resp != nil {
		if cleaned := stripDeferredPDFPromise(resp.Text); cleaned != "" {
			resp.Text = cleaned
		}
	}
}

// flushHostOwnedLiveDataVisual closes the same model-stop gap as PDF
// generation: after search, a small model may describe the requested image
// and stop instead of invoking the newly unlocked renderer/delivery grants.
func (c *sharedAgentLoopCallbacks) flushHostOwnedLiveDataVisual(resp *IMAgentResponse) {
	if c == nil || c.skipHostAutoFileDelivery || c.semanticSurface == nil || strings.TrimSpace(c.semanticDeliveryImageKey) != "" {
		return
	}
	if resp != nil && keepVisibleErrorAfterHostFileAttach(resp.Error) {
		return
	}
	if strings.TrimSpace(trustedHostLookupEvidence(c.semanticLookupEvidence)) == "" {
		return
	}
	name, grant := soleLiveSemanticGrantByAdapter(c.semanticSurface, semanticTrustedLiveDataVisualAdapter)
	if name == "" || grant.Token == "" {
		return
	}
	if got := c.ExecuteToolCall(name, `{}`, "host-auto-render-live-data").Result; strings.Contains(got, "[system rejected]") {
		log.Printf("[semantic] host auto live-data render failed: %s", got)
		return
	}
	deliverName, deliverGrant := soleLiveSemanticGrantByAdapter(c.semanticSurface, "semantic_deliver_current_image")
	if deliverName == "" || !currentChannelImageDeliveryReady(c.semanticSurface, deliverGrant) {
		return
	}
	if got := c.ExecuteToolCall(deliverName, `{}`, "host-auto-deliver-live-data-image").Result; strings.Contains(got, "[system rejected]") {
		log.Printf("[semantic] host auto live-data image deliver failed: %s", got)
	}
}

// finalizeHostOwnedFileResponse is the last attach step: a delivered file is a
// successful-enough turn. Desktop resolveSendResult treats any Error as a failed
// round and drops LocalFilePath, so a later LLM timeout after search would hide
// the PDF. Promise-only assistant text is replaced once the file is actually
// attached, so chat does not keep "请稍候".
func finalizeHostOwnedFileResponse(resp *IMAgentResponse, cb *sharedAgentLoopCallbacks) {
	if resp == nil {
		return
	}
	if strings.TrimSpace(resp.LocalFilePath) == "" && strings.TrimSpace(resp.FileData) == "" {
		return
	}
	if hostResponseHasPDF(resp) {
		title := "报告"
		if cb != nil {
			if got := hostOwnedPDFReportTitle(cb.userText); got != "" {
				title = got
			}
		}
		cleaned := stripDeferredPDFPromise(resp.Text)
		switch {
		case cleaned != "":
			resp.Text = cleaned
		case strings.TrimSpace(resp.Text) != "":
			resp.Text = "已生成「" + title + "」PDF。"
		}
	}
	if !shouldClearStaleErrorAfterHostFileAttach(resp.Error) {
		return
	}
	log.Printf("[semantic] host file attach recovered turn; clearing stale error %q", resp.Error)
	resp.Error = ""
}

func hostResponseHasPDF(resp *IMAgentResponse) bool {
	if resp == nil {
		return false
	}
	mime := strings.ToLower(strings.TrimSpace(resp.FileMimeType))
	if mime == "application/pdf" || strings.HasPrefix(mime, "application/pdf;") {
		return true
	}
	name := strings.ToLower(strings.TrimSpace(resp.FileName))
	path := strings.ToLower(strings.TrimSpace(resp.LocalFilePath))
	return strings.HasSuffix(name, ".pdf") || strings.HasSuffix(path, ".pdf")
}

func hostOwnedPDFBlockedByResponse(resp *IMAgentResponse) bool {
	if resp == nil {
		return false
	}
	return keepVisibleErrorAfterHostFileAttach(resp.Error)
}

func shouldClearStaleErrorAfterHostFileAttach(err string) bool {
	return strings.TrimSpace(err) != "" && !keepVisibleErrorAfterHostFileAttach(err)
}

func keepVisibleErrorAfterHostFileAttach(err string) bool {
	trimmed := strings.TrimSpace(err)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	if lower == "cancelled" || strings.HasPrefix(lower, "cancelled ") {
		return true
	}
	for _, keep := range []string{
		"semantic_capability_unmet",
		"[system rejected]",
		"daily_llm_budget",
		"recovery_checkpoint_failed",
		"panicked",
	} {
		if strings.Contains(lower, keep) {
			return true
		}
	}
	return false
}

func hostOwnedGeneratePDFSucceeded(result string) bool {
	trimmed := strings.TrimSpace(result)
	return strings.HasPrefix(trimmed, "PDF artifact published") &&
		!strings.Contains(trimmed, "[system rejected]") &&
		!strings.Contains(trimmed, "[system unknown]")
}

func (c *sharedAgentLoopCallbacks) issueHostOwnedGenerateFromAvailableEvidence(resp *IMAgentResponse) error {
	if c == nil || c.semanticSurface == nil {
		return nil
	}
	if name, _ := soleLiveSemanticGrantByAdapter(c.semanticSurface, "generate_pdf"); name != "" {
		return nil
	}
	if semanticRetiredGeneratePDF(c.semanticSurface) {
		return nil
	}
	assistant := ""
	if resp != nil {
		assistant = resp.Text
	}
	title := hostOwnedPDFReportTitle(c.userText)
	if strings.TrimSpace(hostOwnedPDFReportContent(assistant, c.semanticLookupEvidence, title)) == "" {
		return nil
	}
	if err := completeHostSatisfiedLookupForGenerate(c.semanticSurface); err != nil {
		return err
	}
	if _, err := refreshSemanticCallSurface(c.semanticSurface); err != nil {
		return err
	}
	if err := c.syncSemanticToolSurface(); err != nil {
		log.Printf("[semantic] host generate evidence sync failed: %v", err)
	}
	return nil
}

func completeHostSatisfiedLookupForGenerate(surface *semanticCallSurface) error {
	if surface == nil {
		return nil
	}
	for _, selection := range surface.plan.Selections {
		if !hostOwnedGenerateSelection(selection) || surface.completed[selection.ID] {
			continue
		}
		for _, requirement := range selection.Requires {
			if surface.completed[requirement] {
				continue
			}
			if strings.HasPrefix(requirement, "confirmation:") {
				return fmt.Errorf("generate still requires confirmation")
			}
			lookup, ok := semanticSelectionByID(surface.plan, requirement)
			if !ok || !semanticLookupSelection(lookup) {
				return fmt.Errorf("generate blocked by %s", requirement)
			}
			if err := completeSemanticCallSurfaceSelection(surface, requirement); err != nil {
				return err
			}
		}
	}
	return nil
}

func semanticLookupSelection(selection tool.PlannedSelection) bool {
	capability := strings.TrimSpace(string(selection.FitProof.MatchedCapability))
	if strings.HasPrefix(capability, "information.search.") || capability == "information.current_time" {
		return true
	}
	switch selection.AdapterName {
	case "web_search", semanticTrustedWebSearchAdapter:
		return true
	default:
		return false
	}
}

var hostOwnedPDFTitleSuffixRE = regexp.MustCompile(`(?i)[,，、;；\s]*(?:请(?:帮我)?|帮我)?(?:并|and)?\s*(?:生成|generate)\s*(?:一份|a)?\s*pdf(?:\s*(?:报告|report))?[.。!！]*$`)
var semanticReportDateRE = regexp.MustCompile(`^(?:\d{4}[-/.]\d{1,2}[-/.]\d{1,2}|\d{4}年\d{1,2}月\d{1,2}日)$`)

func hostOwnedPDFReportTitle(userText string) string {
	text := strings.TrimSpace(semanticUserIntentText(userText))
	for {
		next := strings.TrimSpace(hostOwnedPDFTitleSuffixRE.ReplaceAllString(text, ""))
		next = strings.Trim(next, "，。,;； ")
		if next == text {
			break
		}
		text = next
	}
	if text == "" {
		return "报告"
	}
	runes := []rune(text)
	if len(runes) > 40 {
		return string(runes[:40])
	}
	return text
}

func hostLookupEvidenceUntrusted(evidence string) bool {
	evidence = strings.TrimSpace(evidence)
	if evidence == "" {
		return true
	}
	if strings.Contains(evidence, "[system rejected]") ||
		strings.Contains(evidence, "[file_base64|") ||
		strings.Contains(evidence, "[system unknown]") {
		return true
	}
	lower := strings.ToLower(evidence)
	return strings.Contains(lower, "<tool_call") ||
		strings.Contains(lower, "<turn: tool_call") ||
		strings.Contains(lower, "|dsml|") ||
		strings.Contains(evidence, "｜DSML｜")
}

func trustedHostLookupEvidence(evidence string) string {
	evidence = strings.TrimSpace(evidence)
	if hostLookupEvidenceUntrusted(evidence) {
		return ""
	}
	return evidence
}

func hostOwnedPDFReportContent(assistantText, searchEvidence, title string) string {
	if evidence := trustedHostLookupEvidence(searchEvidence); evidence != "" {
		heading := strings.TrimSpace(title)
		if heading == "" {
			heading = "报告"
		}
		body := "# " + heading + "\n\n" + evidence
		if swarm.ValidatePDFContent(body) == nil {
			return body
		}
	}
	cleaned := strings.TrimSpace(llm.StripXMLToolCalls(stripDeferredPDFPromise(assistantText)))
	if substantialHostPDFReportText(cleaned) && swarm.ValidatePDFContent(cleaned) == nil {
		return cleaned
	}
	return ""
}

func stripDeferredPDFPromise(text string) string {
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		remainder, drop := stripDeferredPDFPromiseLine(line)
		if drop {
			if remainder == "" {
				continue
			}
			kept = append(kept, remainder)
			continue
		}
		kept = append(kept, raw)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func stripDeferredPDFPromiseLine(line string) (string, bool) {
	if line == "" {
		return "", false
	}
	if remainder, drop := stripFailedPDFAuthorizationExcuseLine(line); drop {
		if remainder == "" {
			return "", true
		}
		cleaned, _ := stripDeferredPDFPromiseLine(remainder)
		return cleaned, true
	}
	if waitOnlyDeferredLine(line) {
		return "", true
	}
	if idx := deferredPDFLeadInIndex(line); idx >= 0 {
		return strings.TrimSpace(strings.Trim(line[:idx], "，。,;； ")), true
	}
	if deferredPDFPromiseLine(line) {
		if idx := deferredWaitMarkerIndex(line); idx > 0 {
			prefix := strings.TrimSpace(strings.Trim(line[:idx], "，。,;； "))
			if prefix != "" && !deferredPDFPromiseLine(prefix) && !waitOnlyDeferredLine(prefix) {
				return prefix, true
			}
		}
		return "", true
	}
	return line, false
}

func deferredPDFLeadInIndex(line string) int {
	lower := strings.ToLower(line)
	if !strings.Contains(lower, "pdf") {
		return -1
	}
	best := -1
	for _, lead := range []string{"接下来我将", "接下来我会"} {
		idx := strings.Index(line, lead)
		if idx >= 0 && strings.Contains(lower[idx:], "pdf") && (best < 0 || idx < best) {
			best = idx
		}
	}
	for _, lead := range []string{"i will", "i'll", "i am going to", "let me generate"} {
		idx := strings.Index(lower, lead)
		if idx >= 0 && strings.Contains(lower[idx:], "pdf") && (best < 0 || idx < best) {
			best = idx
		}
	}
	return best
}

func deferredWaitMarkerIndex(line string) int {
	lower := strings.ToLower(line)
	best := -1
	for _, marker := range []string{"请稍候", "请稍后", "请稍等"} {
		if idx := strings.Index(line, marker); idx >= 0 && (best < 0 || idx < best) {
			best = idx
		}
	}
	if idx := strings.Index(lower, "please wait"); idx >= 0 && (best < 0 || idx < best) {
		best = idx
	}
	return best
}

func deferredPDFPromiseLine(line string) bool {
	if line == "" {
		return false
	}
	lower := strings.ToLower(line)
	mentionsPDF := strings.Contains(lower, "pdf")
	mentionsGenerateReport := strings.Contains(line, "生成") && strings.Contains(line, "报告")
	hasWait := strings.Contains(line, "请稍候") || strings.Contains(line, "请稍后") || strings.Contains(lower, "please wait")
	if hasWait && (mentionsPDF || mentionsGenerateReport) {
		return true
	}
	if strings.Contains(line, "接下来我将") && mentionsPDF {
		return true
	}
	return waitOnlyDeferredLine(line)
}

func stripFailedPDFAuthorizationExcuseLine(line string) (string, bool) {
	if !failedPDFAuthorizationExcuseLine(line) {
		return line, false
	}
	var kept []string
	for _, sentence := range splitPDFExcuseSentences(line) {
		if !failedPDFAuthorizationExcuseLine(sentence) {
			kept = append(kept, sentence)
		}
	}
	return strings.TrimSpace(strings.Join(kept, "")), true
}

func splitPDFExcuseSentences(line string) []string {
	var out []string
	var cur strings.Builder
	for _, r := range line {
		cur.WriteRune(r)
		if r == '。' || r == '！' || r == '？' {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func failedPDFAuthorizationExcuseLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(trimmed, "无法解析的工具调用") || strings.Contains(trimmed, "已拦截原始工具 XML") {
		return true
	}
	if strings.Contains(trimmed, "PDF生成失败") || strings.Contains(lower, "pdf generation failed") {
		return true
	}
	if strings.Contains(trimmed, "无法生成PDF") || strings.Contains(trimmed, "无法成功生成PDF") {
		return true
	}
	mentionsPDF := strings.Contains(lower, "generate_pdf") || strings.Contains(lower, "pdf")
	if mentionsPDF && (strings.Contains(trimmed, "无法直接生成") || strings.Contains(trimmed, "暂无法直接生成")) {
		return true
	}
	if mentionsPDF && (strings.Contains(trimmed, "工具列表中没有") ||
		strings.Contains(trimmed, "没有 PDF 生成工具") ||
		strings.Contains(trimmed, "没有PDF生成工具") ||
		strings.Contains(trimmed, "请重新发起生成") ||
		strings.Contains(trimmed, "授权工具出现")) {
		return true
	}
	if strings.Contains(trimmed, "如需PDF报告") &&
		(strings.Contains(trimmed, "授权") || strings.Contains(trimmed, "下一轮") ||
			strings.Contains(trimmed, "整理为") || strings.Contains(trimmed, "文本格式") ||
			strings.Contains(trimmed, "其他方式") || strings.Contains(lower, "re-authorize")) {
		return true
	}
	if !mentionsPDF {
		return false
	}
	return strings.Contains(trimmed, "未授权") ||
		strings.Contains(trimmed, "重新授权") ||
		strings.Contains(trimmed, "工具调用失败") ||
		strings.Contains(trimmed, "参数格式无效") ||
		strings.Contains(lower, "not authorized") ||
		strings.Contains(lower, "not allowed by the current execution policy")
}

func waitOnlyDeferredLine(line string) bool {
	trimmed := strings.Trim(strings.TrimSpace(line), "。.~～…!！")
	switch strings.ToLower(trimmed) {
	case "请稍候", "请稍后", "请稍等", "稍候", "稍后", "稍等",
		"please wait", "wait a moment", "one moment":
		return true
	default:
		return false
	}
}

func substantialHostPDFReportText(text string) bool {
	return len([]rune(strings.TrimSpace(text))) >= 80
}

func (c *sharedAgentLoopCallbacks) recordSemanticLookupEvidence(selection tool.PlannedSelection, result string) {
	if c == nil {
		return
	}
	result = strings.TrimSpace(result)
	if result == "" || selection.FitProof.MatchedCapability != "information.search.web" {
		return
	}
	if hostLookupEvidenceUntrusted(result) {
		return
	}
	c.semanticLookupEvidence = result
}

func (c *sharedAgentLoopCallbacks) flushHostOwnedCurrentChannelFileDelivery(resp *IMAgentResponse) {
	if c == nil || c.skipHostAutoFileDelivery || c.semanticSurface == nil || strings.TrimSpace(c.semanticDeliveryFileData) != "" {
		return
	}
	if resp != nil && keepVisibleErrorAfterHostFileAttach(resp.Error) {
		return
	}
	name, grant := soleLiveSemanticGrantByAdapter(c.semanticSurface, "semantic_deliver_current_file")
	if name == "" || !currentChannelFileDeliveryReady(c.semanticSurface, grant) {
		return
	}
	target := trustedLoopDeliveryTarget(c.loopCtx)
	if target == nil {
		return
	}
	selection, ok := semanticSelectionByID(c.semanticSurface.plan, grant.SelectionID)
	if !ok || !strings.EqualFold(strings.TrimSpace(selection.Provider.ProviderID), strings.TrimSpace(target.ChannelScope)) {
		return
	}
	got := c.ExecuteToolCall(name, `{}`, "host-auto-current-file").Result
	if strings.Contains(got, "[system rejected]") {
		log.Printf("[semantic] host auto current-channel file deliver failed: %s", got)
	}
}

func currentChannelFileDeliveryReady(surface *semanticCallSurface, grant tool.InvocationGrant) bool {
	if surface == nil || grant.SelectionID == "" {
		return false
	}
	selection, ok := semanticSelectionByID(surface.plan, grant.SelectionID)
	if !ok || !semanticCurrentChannelArtifactDelivery(selection) || len(selection.ArtifactDependencies) != 1 {
		return false
	}
	dependency := selection.ArtifactDependencies[0]
	kind := strings.TrimSpace(dependency.Artifact.Kind)
	if kind == "" {
		kind = strings.TrimSpace(dependency.Contract.Kind)
	}
	if !strings.EqualFold(kind, "document") {
		return false
	}
	if strings.TrimSpace(dependency.ArtifactID) != "" {
		return true
	}
	return currentChannelProducerArtifactPublished(surface, dependency.ProducerSelection, dependency.Contract)
}

func currentChannelImageDeliveryReady(surface *semanticCallSurface, grant tool.InvocationGrant) bool {
	if surface == nil || grant.SelectionID == "" {
		return false
	}
	selection, ok := semanticSelectionByID(surface.plan, grant.SelectionID)
	if !ok || !semanticCurrentChannelArtifactDelivery(selection) || len(selection.ArtifactDependencies) != 1 {
		return false
	}
	dependency := selection.ArtifactDependencies[0]
	kind := strings.TrimSpace(dependency.Artifact.Kind)
	if kind == "" {
		kind = strings.TrimSpace(dependency.Contract.Kind)
	}
	if !strings.EqualFold(kind, "image") {
		return false
	}
	if strings.TrimSpace(dependency.ArtifactID) != "" {
		return true
	}
	return currentChannelProducerArtifactPublished(surface, dependency.ProducerSelection, dependency.Contract)
}

func currentChannelProducerArtifactPublished(surface *semanticCallSurface, producerSelection string, contract tool.ArtifactContract) bool {
	producerSelection = strings.TrimSpace(producerSelection)
	if producerSelection == "" || surface == nil || surface.artifacts == nil || surface.artifacts.routes == nil {
		return false
	}
	refs, err := surface.artifacts.routes.ArtifactRefs(surface.scope)
	if err != nil {
		return false
	}
	for _, candidate := range refs {
		if candidate.ProducerSelection != producerSelection {
			continue
		}
		if artifactContractMatches(tool.ArtifactContract{Kind: candidate.Kind, MIMEType: candidate.MIMEType, Required: true}, contract) {
			return true
		}
	}
	return false
}

func soleLiveSemanticGrantByAdapter(surface *semanticCallSurface, adapter string) (string, tool.InvocationGrant) {
	if surface == nil {
		return "", tool.InvocationGrant{}
	}
	adapter = strings.TrimSpace(adapter)
	var name string
	var grant tool.InvocationGrant
	for grantName, item := range surface.grants {
		if item.AdapterName != adapter {
			continue
		}
		if name != "" {
			return "", tool.InvocationGrant{}
		}
		name = grantName
		grant = item
	}
	return name, grant
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
	// semanticDurabilityBlocked closes semantic successor publication after a
	// checkpoint write has failed. Unlike hasPendingToolBatch, this also covers
	// a failed pre-tool checkpoint, where no durable marker was established and
	// the core loop must stop before executing the batch. Terminal projection
	// must not interpret callback-local state from either failure mode as a
	// source of new grants or host-owned effects.
	semanticDurabilityBlocked bool
	// hasLocalFileWork survives the light-to-full upgrade, which occurs after
	// attachment staging and otherwise only has the raw user text to inspect.
	hasLocalFileWork bool
	platform         string // turn platform from runAgentLoopShared (desktop/weixin/…)
	systemPrompt     string
	// taskAnchor is the immutable identity/source snapshot captured before
	// history compaction. Prompt rebuilds must reuse it rather than reload the
	// mutable per-user anchor map, which may already point at a later turn.
	taskAnchor      *taskIdentityAnchor
	tools           []map[string]interface{}
	semanticSurface *semanticCallSurface
	// legacySurface is refreshed only when a complete replacement list is sent
	// to the model. It is never rebuilt from the registry, history, pins or
	// BaseTools, so a stale legacy response cannot execute a hidden function.
	legacySurface legacyToolSurface
	// skipHostAutoFileDelivery is set for ask_user / record_audio pauses so a
	// published PDF is not sent while the turn is still waiting on the user.
	skipHostAutoFileDelivery bool
	// semanticLookupEvidence is the last successful web-search body in this
	// turn. Host-owned generate_pdf uses it when the model stops after search.
	semanticLookupEvidence string
	llmCfg                 corelib.MaclawLLMConfig
	route                  modelRouteDecision
	// phase is the inbound-turn routing posture. Legacy compatibility surfaces
	// are rebuilt at every actual model-request boundary using this same
	// host-owned posture; a predecessor's rendered definitions are never a
	// successor input.
	phase      agentLoopPhase
	onProgress tool.ProgressCallback
	onToken    llm.TokenCallback
	onNewRound NewRoundCallback
	maxIter    int
	httpClient *http.Client
	escalated  bool
	// surfaceRefreshPending is set when the tool-driven route escalation changes
	// the model-visible prompt/tool policy and consumed by the core loop before
	// its following request.
	surfaceRefreshPending bool
	toolCalls             int
	// moaPreset is set for the duration of one agent loop after /moa or auto arming.
	moaPreset *moa.ResolvedPreset
	moaAuto   bool
	// File delivery from send_file / send_to_im materialize (shared path only).
	deliveredPaths       []string
	fileMaterializeNanos int64
	filesForwarded       int
	// screenshotImageKey holds the latest screenshot produced by the shared
	// loop. Unlike the legacy loop, the shared loop has no post-tool artifact
	// branch, so it must explicitly carry the image into the final IM response.
	screenshotImageKey string
	// semanticDeliveryImageKey is set only by the planned delivery selection
	// after a scoped ArtifactRef has been consumed by the artifact broker.
	semanticDeliveryImageKey string
	// semanticDeliveryFile* mirrors the image projection but carries a
	// broker-consumed document artifact. It is never model-supplied.
	semanticDeliveryFileData  string
	semanticDeliveryFileName  string
	semanticDeliveryFileMIME  string
	semanticDeliveryVoiceData string
	semanticDeliveryVoiceName string
	semanticDeliveryVoiceMIME string
	// semanticPreparedDelivery is populated only by the adapter from a trusted
	// artifact binding. Coordinated execution commits it through
	// PrepareDeliveryAndComplete so a host-call result can never be durable
	// without its prepared delivery (or vice versa).
	semanticPreparedDelivery *tool.DeliveryRecord
	// semanticDelivery is the trusted durable record identity paired with the
	// ImageKey projection. A gateway must record accepted/unknown after its
	// actual transport attempt; the prepared ToolPlan selection cannot do so.
	semanticDelivery *semanticDeliveryProjection
	// semanticCapturedImageKey is a short-lived handoff from the legacy handler
	// projection to its already-admitted semantic capture adapter. It is not
	// exposed to the model or attached to the gateway response.
	semanticCapturedImageKey string
	// semanticDeferFileMaterialize keeps a generate selection's [file_base64]
	// payload intact so the capability adapter can publish an ArtifactRef.
	// Delivery, not generate, is the only path that may attach file bytes to
	// the conversation response.
	semanticDeferFileMaterialize bool
	inputBreakdown               agent.LoopInputBreakdown
	// firstRequestBudgetApplied prevents a latency-oriented history cap from
	// constraining later tool rounds, which may legitimately need more context.
	firstRequestBudgetApplied bool
	// semanticHoldDependantIssue keeps generate_pdf from being issued in the
	// same assistant batch that just finished search. Same-family repeats are
	// still issued. Flash models call generate_pdf in parallel with web_search;
	// unlocking mid-batch lets the invented {query} payload reach Admission
	// and burn the one-shot grant.
	semanticHoldDependantIssue bool
	// semanticNeedDependantIssue is set when a selection completed during a
	// held batch and dependants still need IssueReady. Kept true across a
	// failed refresh so host flush can retry.
	semanticNeedDependantIssue bool
	// Revision last incorporated by TransformConversation. Keeping this at the
	// conversation boundary (rather than the later HTTP-start boundary) closes
	// the race where steering arrives between transform and request creation.
	llmReplanRevision atomic.Int64
}

func sharedLoopUserFacingText(text string) string {
	return agent.StripWorkingStateFromVisible(text)
}

func (c *sharedAgentLoopCallbacks) OnLoopInputBreakdown(b agent.LoopInputBreakdown) {
	if c != nil {
		c.inputBreakdown = b
	}
}

func (c *sharedAgentLoopCallbacks) LoadWorkingState() *agent.WorkingState {
	if c == nil || c.loopCtx == nil {
		return nil
	}
	return agent.CloneWorkingState(c.loopCtx.ResumeWorkingState)
}

func (c *sharedAgentLoopCallbacks) SaveWorkingState(_ *agent.WorkingState) {
	// Carrier is pendingAskUser / pendingRecordAudio plus LoopContext.ResumeWorkingState.
}

// ActiveWorkingStateGoal projects only a this-turn-active horizon or
// goal-continuation objective. Leftover store/session goals stay hidden.
func (c *sharedAgentLoopCallbacks) ActiveWorkingStateGoal() string {
	if c == nil || c.handler == nil {
		return ""
	}
	if c.loopCtx != nil && strings.TrimSpace(c.loopCtx.HorizonRole) != "" {
		if g := c.handler.activeHorizonWorkingStateGoal(c.userID); g != "" {
			return g
		}
	}
	if c.effectivePlatform() == "goal-continuation" {
		return c.handler.activeContinuationWorkingStateGoal(c.userID)
	}
	return ""
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
	if c.loopCtx.Runtime.Execution.PromptIsLight() {
		return agent.PromptProfileLight
	}
	return agent.NormalizePromptProfile(c.loopCtx.Runtime.Execution.PromptProfile)
}

// IsToolAllowedForPromptProfile resolves live semantic grants to their
// immutable planned selections. The model-visible name is a transport lookup
// only: light-profile admission is made from the capability plan's effect and
// confirmation contract, so a name's spelling cannot create a policy hole.
func (c *sharedAgentLoopCallbacks) IsToolAllowedForPromptProfile(name string, profile agent.PromptProfile) bool {
	if c != nil && c.semanticSurface != nil {
		grant, ok := c.semanticSurface.grants[c.semanticSurface.resolveFunctionName(name)]
		if !ok {
			// A semantic surface is closed: retired and invented names cannot
			// fall through to a legacy light-safe tool with a similar name,
			// including after a light→full prompt-budget upgrade.
			return false
		}
		if !profile.IsLight() {
			return true
		}
		selection, ok := semanticSelectionByID(c.semanticSurface.plan, grant.SelectionID)
		return ok && tool.IsLightPromptSafeSelection(selection)
	}
	if !profile.IsLight() {
		return true
	}
	return agent.IsLightTurnToolAllowed(name)
}

func (c *sharedAgentLoopCallbacks) semanticLightLookup() bool {
	return c != nil && c.semanticSurface != nil && c.CurrentPromptProfile().IsLight()
}

// IsToolAllowed keeps a governed semantic turn on its signed grants even after
// the execution profile is no longer light. Without this, light→full retry
// authorizes web_fetch/write_file by name, then ExecuteTool still rejects them.
// On a light profile, a grant is not enough: a mutating selection would pass
// this authorizer and then hit the core light deny ("set PROFILE=full"), which
// is the text that made the model ask the user to re-authorize tools.
func (c *sharedAgentLoopCallbacks) IsToolAllowed(name string) bool {
	if c == nil || c.semanticSurface == nil {
		return true
	}
	name = c.semanticSurface.resolveFunctionName(name)
	if _, ok := c.semanticSurface.grants[name]; !ok {
		return false
	}
	if c.loopCtx != nil && c.loopCtx.Runtime.Execution.PromptIsLight() {
		return c.IsToolAllowedForPromptProfile(name, agent.PromptProfileLight)
	}
	return true
}

// IsToolCallAllowed performs only current-surface intake checks. Historical
// invoke_* names are rejected by IsToolAllowed; no payload shape can translate
// one into a grant from this request.
func (c *sharedAgentLoopCallbacks) IsToolCallAllowed(name, argsJSON string) (bool, string) {
	if c == nil || c.semanticSurface == nil {
		return true, ""
	}
	resolved := c.semanticSurface.resolveFunctionName(name)
	if _, ok := c.semanticSurface.grants[resolved]; !ok {
		reason := strings.TrimSpace(c.ToolDenialMessage(name))
		return false, strings.TrimPrefix(reason, "Error: ")
	}
	if reason := c.invalidHostOwnedGenerateArgsReason(resolved, argsJSON); reason != "" {
		return false, reason
	}
	return true, ""
}

// invalidHostOwnedGenerateArgsReason is Intake, not Admission. A parallel
// generate_pdf {query} must not reach Coordinator.Reject and burn the grant
// the host tail still needs after search.
func (c *sharedAgentLoopCallbacks) invalidHostOwnedGenerateArgsReason(name, argsJSON string) string {
	if c == nil || c.semanticSurface == nil {
		return ""
	}
	grant, ok := c.semanticSurface.grants[name]
	if !ok {
		return ""
	}
	selection, ok := semanticSelectionByID(c.semanticSurface.plan, grant.SelectionID)
	if !ok {
		return ""
	}
	if grant.AdapterName != "generate_pdf" && !hostOwnedGenerateSelection(selection) {
		return ""
	}
	if semanticGeneratePDFArgsTooThin(argsJSON) {
		return hostOwnedGeneratePDFInvalidArgsReason
	}
	if _, err := c.semanticCanonicalArguments(selection, argsJSON); err == nil {
		return ""
	}
	return hostOwnedGeneratePDFInvalidArgsReason
}

const hostOwnedGeneratePDFInvalidArgsReason = "generate_pdf arguments are invalid. Call generate_pdf once with Markdown content and optional title only; do not pass query/path/output and do not ask the user to re-authorize tools."

func semanticUnissuedGeneratePDFDenial(surface *semanticCallSurface, name string) string {
	if surface == nil || strings.TrimSpace(name) != "generate_pdf" {
		return ""
	}
	if _, grant := soleLiveSemanticGrantByAdapter(surface, "generate_pdf"); grant.Token != "" {
		return ""
	}
	if semanticRetiredGeneratePDF(surface) {
		return "Error: generate_pdf was already used this turn. Answer from the published artifact; do not retry generate_pdf and do not ask the user to re-authorize tools."
	}
	for _, selection := range surface.plan.Selections {
		if selection.FitProof.MatchedCapability != "document.generate.file" {
			continue
		}
		return "Error: generate_pdf is not listed yet. Continue from lookup evidence; do not retry generate_pdf and do not ask the user to re-authorize tools."
	}
	return ""
}

// ToolDenialMessage implements agent.ToolDenialPresenter. Only a light
// governed lookup should tell the model to stop and answer from evidence;
// coding/workflow policy denials keep the generic execution-policy text.
func (c *sharedAgentLoopCallbacks) ToolDenialMessage(name string) string {
	if msg := semanticUnissuedGeneratePDFDenial(c.semanticSurface, name); msg != "" {
		return msg
	}
	if !c.semanticLightLookup() {
		return ""
	}
	n := strings.TrimSpace(name)
	if n == "" {
		n = "(unknown)"
	}
	if live := c.semanticSurface.soleLiveLookupGrantName(); live != "" && n != live && isSemanticInvocationGrantName(n) {
		return fmt.Sprintf(
			"Error: tool %q is not the current lookup grant. Call %q from this turn's tool list with the new query. Do not reuse earlier invoke_* names and do not ask the user to re-authorize tools.",
			n, live,
		)
	}
	return fmt.Sprintf(
		"Error: tool %q is not available in this turn. If a lookup already returned evidence, answer from that evidence. Do not ask the user to re-authorize tools or switch modes.",
		n,
	)
}

// ManagedSemanticTurn reports whether this shared loop is on a signed
// catalog plan. Phase C forbids light→full BuildTools rebuild on that surface.
func (c *sharedAgentLoopCallbacks) ManagedSemanticTurn() bool {
	return c != nil && c.semanticSurface != nil
}

// UpgradeLightPromptToFull implements agent.LightProfileUpgrader. A governed
// semantic family must not upgrade: the catalog plan is immutable, and a fake
// full profile makes the model retry unauthorized tools until grant expiry.
// A leftover miss must not upgrade either: full would widen the leftover
// surface after the miss already paid a light bound.
func (c *sharedAgentLoopCallbacks) UpgradeLightPromptToFull(reason string) bool {
	if c == nil || !c.CurrentPromptProfile().IsLight() {
		return false
	}
	if c.semanticSurface != nil {
		return false
	}
	// A leftover miss is already a bounded surface. Light→full would re-merge
	// CoreToolNames under a full profile and undo the leftover width cut.
	if loopContextHasRoutingMissFallback(c.loopCtx) {
		return false
	}
	if c.loopCtx != nil {
		c.loopCtx.Runtime.Execution = fullExecutionProfile("light tool deny → full: " + reason)
	}
	// Attachment/file work is an explicit non-CU posture for this turn. The
	// former eager tool render happened to clear the sticky Computer Use state
	// through prepareAgentLoopTools; retain that independent routing-state
	// transition without using a definition render as its trigger.
	if c.hasLocalFileWork && !hasExplicitComputerUseRequest(c.userText) {
		clearComputerUseSessionActive()
	}
	if c.handler != nil {
		// Refresh policy posture now, but do not render definitions here. This
		// method runs while handling a response for an already-sent request;
		// replacing c.tools or legacySurface would construct an unowned surface
		// without the successor request's epoch, manifest, receipt, or handoff.
		// BuildToolsForModelRequest derives the new phase and complete replacement
		// only after RunLoop opens the next real request boundary.
		c.systemPrompt = c.systemPromptWithTaskAnchor(c.handler.buildSystemPromptWithMemory(agent.CompactQueryForEmbedding(semanticUserIntentText(c.userText)), false, c.loopCtx))
	}
	log.Printf("[shared-loop] light→full prompt upgrade reason=%s", reason)
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

// BuildToolsForModelRequest makes the shared legacy path obey the same
// request-bound replacement rule as every other RunLoop callback. startState
// computes the inbound turn's routing decision, but the resulting definitions
// are only a preparation snapshot for prompt/trajectory diagnostics. The
// executable surface is rebuilt here, immediately after RunLoop has created
// the request epoch and immediately before the final wire receipt is made.
//
// A managed semantic surface remains closed over its durable grants: this
// method reads only its current visible closure and never refreshes, issues, or
// revives a grant. A legacy surface is rerouted from the current host registry
// under the fixed inbound posture, then atomically replaces the callback's
// execution snapshot with the exact definitions that will be serialized.
func (c *sharedAgentLoopCallbacks) BuildToolsForModelRequest(userText string, iteration int) []map[string]interface{} {
	_ = userText
	_ = iteration
	if c == nil {
		return nil
	}
	if c.semanticSurface != nil {
		definitions, err := visibleSemanticCallSurfaceDefinitions(c.semanticSurface)
		if err != nil {
			log.Printf("[semantic-routing] request-bound surface render failed: %v", err)
			return nil
		}
		c.tools = definitions
		return definitions
	}
	if c.handler == nil {
		return c.tools
	}
	// The inbound phase is policy posture, not a tool-surface receipt. Recompute
	// it at the request boundary so a preceding light→full policy upgrade is
	// reflected in this replacement without manufacturing an intermediate
	// callback-local tool inventory.
	c.phase = c.handler.initialAgentLoopPhase(c.userText, c.loopCtx)
	routingText := computerUseRoutingTextForLocalFileWork(c.userText, c.hasLocalFileWork)
	toolSet := c.handler.prepareAgentLoopTools(c.userID, routingText, c.loopCtx, c.phase)
	c.tools = toolSet.Tools
	c.legacySurface = c.legacySurface.replaceDefinitions(c.tools, toolSet.ClientToolNames)
	return c.tools
}

func (c *sharedAgentLoopCallbacks) ExecuteTool(name, argsJSON string) string {
	if c == nil || c.handler == nil {
		return "handler unavailable"
	}
	if c.semanticSurface != nil {
		return c.executeSemanticTool(name, argsJSON)
	}
	if c.legacySurface.HasSnapshot() && !c.legacySurface.Allows(name) {
		return legacyToolSurfaceDeniedText(name)
	}
	if c.legacySurface.HasSnapshot() && !c.legacySurface.AllowsLiveProvision(name) {
		return legacyAdapterCatalogDeniedText(name)
	}
	if c.legacySurface.HasSnapshot() {
		if err := c.legacySurface.AllowsArguments(name, argsJSON); err != nil {
			return legacyToolArgumentDeniedText(name, err)
		}
	}
	if isLegacyModelMCPGateway(name) {
		return legacyModelMCPGatewayDeniedText()
	}
	if isLegacyModelManageSkillGateway(name, argsJSON) {
		return legacyModelManageSkillGatewayDeniedText()
	}
	if c.legacySurface.IsClientTool(name) {
		clientTool, ok := clientToolForLoop(c.loopCtx, name)
		if !ok {
			return "[system rejected] client tool binding is unavailable"
		}
		return c.handler.dispatchClientToolCall(c.loopCtx, clientTool, "", argsJSON).Text
	}
	return c.executeToolWithoutSemanticSurface(name, argsJSON)
}

// ExecuteToolCall receives the model/provider tool-call identifier from the
// core loop. Semantic calls require it: synthesizing identity from a function
// name would merge unrelated calls and could replay the wrong effect.
func (c *sharedAgentLoopCallbacks) ExecuteToolCall(name, argsJSON, callID string) agent.ToolExecutionResult {
	return c.executeToolCallWithExecutionContext(name, argsJSON, callID, agent.ToolCallExecutionContext{})
}

func (c *sharedAgentLoopCallbacks) executeToolCallWithExecutionContext(name, argsJSON, callID string, execution agent.ToolCallExecutionContext) agent.ToolExecutionResult {
	if c != nil {
		setComputerUseTurnVision(c.llmCfg.SupportsVision)
		setComputerUseOwner(computerUseOwnerFromLoop(c.loopCtx, c.userID))
	}
	var result agent.ToolExecutionResult
	if c != nil && c.semanticSurface != nil {
		result = semanticAgentToolExecutionResult(c.executeSemanticToolCallWithEpoch(name, argsJSON, callID, execution.SurfaceEpoch))
	} else {
		if c != nil && c.legacySurface.HasSnapshot() && !c.legacySurface.Allows(name) {
			result = agent.ToolExecutionResult{Result: legacyToolSurfaceDeniedText(name), Outcome: agent.ToolExecutionOutcomeError}
		} else if c != nil && c.legacySurface.HasSnapshot() && !c.legacySurface.AllowsLiveProvision(name) {
			result = agent.ToolExecutionResult{Result: legacyAdapterCatalogDeniedText(name), Outcome: agent.ToolExecutionOutcomeError}
		} else if c != nil && c.legacySurface.HasSnapshot() {
			if err := c.legacySurface.AllowsArguments(name, argsJSON); err != nil {
				result = agent.ToolExecutionResult{Result: legacyToolArgumentDeniedText(name, err), Outcome: agent.ToolExecutionOutcomeError}
			} else if isLegacyModelMCPGateway(name) {
				result = agent.ToolExecutionResult{Result: legacyModelMCPGatewayDeniedText(), Outcome: agent.ToolExecutionOutcomeError}
			} else if isLegacyModelManageSkillGateway(name, argsJSON) {
				result = agent.ToolExecutionResult{Result: legacyModelManageSkillGatewayDeniedText(), Outcome: agent.ToolExecutionOutcomeError}
			} else if c.legacySurface.IsClientTool(name) {
				clientTool, ok := clientToolForLoop(c.loopCtx, name)
				if !ok {
					result = agent.ToolExecutionResult{Result: "[system rejected] client tool binding is unavailable", Outcome: agent.ToolExecutionOutcomeError}
				} else {
					dispatched := c.handler.dispatchClientToolCall(c.loopCtx, clientTool, callID, argsJSON)
					outcome := agent.ToolExecutionOutcomeOK
					if dispatched.Outcome != toolOutcomeSucceeded {
						outcome = agent.ToolExecutionOutcomeError
					}
					result = agent.ToolExecutionResult{Result: dispatched.Text, Outcome: outcome}
				}
			} else {
				result = c.ExecuteToolStructured(name, argsJSON)
			}
		} else if isLegacyModelMCPGateway(name) {
			result = agent.ToolExecutionResult{Result: legacyModelMCPGatewayDeniedText(), Outcome: agent.ToolExecutionOutcomeError}
		} else if isLegacyModelManageSkillGateway(name, argsJSON) {
			result = agent.ToolExecutionResult{Result: legacyModelManageSkillGatewayDeniedText(), Outcome: agent.ToolExecutionOutcomeError}
		} else {
			result = c.ExecuteToolStructured(name, argsJSON)
		}
	}
	return attachPendingComputerUseModelImage(result)
}

// BeginToolSurfaceEpoch is invoked by the shared core loop immediately before
// it sends one model request. The returned value never enters tool schemas or
// model arguments; it is retained by the loop and paired with tool calls from
// that response only.
func (c *sharedAgentLoopCallbacks) BeginToolSurfaceEpoch(iteration int) string {
	_ = iteration
	if c == nil {
		return ""
	}
	if c.semanticSurface != nil {
		return c.semanticSurface.beginEpoch()
	}
	return c.legacySurface.beginEpoch()
}

// cancelManagedSemanticSurface is the single lifecycle fence for an IM
// managed surface. It never edits the transient definition list as a
// substitute for durable revocation: coordinator cancellation retires model
// request surfaces, materializations and outstanding grants atomically.
func (c *sharedAgentLoopCallbacks) cancelManagedSemanticSurface() {
	if c == nil || c.semanticSurface == nil {
		return
	}
	cancelSemanticCallSurface(c.semanticSurface)
}

// cancelSemanticCallSurface is shared by terminal loop cancellation and fresh
// ingress replacement. Both must close the same durable authority boundary;
// merely clearing the LoopContext's ephemeral identity leaves issued grants
// usable to a late provider response.
func cancelSemanticCallSurface(surface *semanticCallSurface) {
	if surface == nil {
		return
	}
	surface.invalidateEpoch()
	if surface.coordinator == nil {
		return
	}
	if err := surface.coordinator.CancelRouteSurface(surface.scope, time.Now().UTC()); err != nil {
		log.Printf("[semantic-routing] cancel managed surface scope=%+v err=%v", surface.scope, err)
	}
}

// ExecuteToolCallWithContext is the model-response dispatch boundary. A
// response whose epoch was superseded by a new request or a child plan cannot
// resolve its function name against the current surface, even when both
// revisions expose the same stable adapter name (for example web_search).
func (c *sharedAgentLoopCallbacks) ExecuteToolCallWithContext(name, argsJSON, callID string, execution agent.ToolCallExecutionContext) agent.ToolExecutionResult {
	if c != nil {
		if c.semanticSurface != nil && !c.semanticSurface.epochIsCurrent(execution.SurfaceEpoch) {
			return semanticAgentToolExecutionResult("[system rejected] stale_surface")
		}
		if c.semanticSurface == nil && c.legacySurface.HasSnapshot() && !c.legacySurface.epochIsCurrent(execution.SurfaceEpoch) {
			return agent.ToolExecutionResult{Result: "[system rejected] stale_surface", Outcome: agent.ToolExecutionOutcomeError}
		}
	}
	return c.executeToolCallWithExecutionContext(name, argsJSON, callID, execution)
}

func semanticAgentToolExecutionResult(text string) agent.ToolExecutionResult {
	result := agent.ToolExecutionResult{Result: text, Outcome: agent.ToolExecutionOutcomeOK}
	// An indeterminate outcome is not a successful one. The outcome enum has
	// no unknown member, so the uncertainty stays in the text the model reads
	// while the loop is told the call did not succeed.
	if semanticSelectionFailed(text) || semanticSelectionOutcomeUnknown(text) {
		result.Outcome = agent.ToolExecutionOutcomeError
	}
	return result
}

// executeToolWithoutSemanticSurface is the legacy execution projection used
// only after a bound semantic adapter has admitted the selection, or by turns
// that have not migrated to a declared capability family yet.
func (c *sharedAgentLoopCallbacks) executeToolWithoutSemanticSurface(name, argsJSON string) string {
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
	exec := c.handler.executeToolDetailedWithRuntimeContextAndContextTokens(
		withTrustedAuditPrincipal(execCtx, c.semanticPrincipalID()),
		policyUserID,
		loopContextHasExplicitRuntimeOwner(c.loopCtx),
		platform,
		c.llmCfg.EffectiveContextTokens(),
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
	// A screenshot is an outbound artifact, not model context. Preserve it for
	// the final gateway response and replace the base64 body with the same small
	// observation used by the legacy loop. Without this, Lansenger receives only
	// the model's text claiming the screenshot was sent.
	screenshot := parseToolPayloadResultForPlatformLang(exec.Text, platform, c.handler.imUILangOrZh())
	if screenshot.ImageKey != "" {
		// A semantic capture selection must publish an ArtifactRef before any
		// channel delivery can happen. Return the opaque transport payload to the
		// bound adapter, which immediately consumes it into the broker; do not
		// populate the final response here.
		if c.semanticSurface != nil {
			c.semanticCapturedImageKey = screenshot.ImageKey
			return c.finishSharedToolExecution(requestID, toolCallID, name, argsJSON, screenshot.ToolContent, true, nil)
		}
		c.screenshotImageKey = screenshot.ImageKey
		return c.finishSharedToolExecution(requestID, toolCallID, name, argsJSON, screenshot.ToolContent, true, nil)
	}

	if c.semanticDeferFileMaterialize {
		return c.finishSharedToolExecution(requestID, toolCallID, name, argsJSON, exec.Text, true, nil)
	}
	// Materialize [file_base64|…|im] before truncating: shared RunLoop has no
	// post-tool artifact branch, so this is the only place desktop→WeChat runs.
	// Pass originating platform so WeChat/Feishu channel turns report channel
	// delivery success (LocalFilePaths) instead of false "sender unconfigured".
	mat := c.handler.materializeToolFilePayloadForPlatform(exec.Text, platform)
	outText := ""
	ok := !exec.IsFailure()
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
	return c.finishSharedToolExecution(requestID, toolCallID, name, argsJSON, outText, ok, mat.LocalPaths)
}

// executeSemanticTool resolves the opaque function name only through this
// turn's signed grant. In particular, it does not accept provider, skill, MCP
// server, implementation, or selection identity from model arguments.
func (c *sharedAgentLoopCallbacks) executeSemanticTool(functionName, argsJSON string) string {
	if c == nil || c.handler == nil || c.semanticSurface == nil || c.semanticSurface.issuer == nil || c.semanticSurface.executor == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	functionName = c.bindSemanticFunctionName(functionName, argsJSON)
	grant, ok := c.semanticSurface.grants[functionName]
	if !ok {
		grant, ok = c.semanticSurface.retiredGrants[functionName]
	}
	if !ok {
		return semanticGrantRejectMessage("selection_not_authorized")
	}
	// The executor admits the signed grant, loads durable predecessor facts,
	// acquires a conditional selection run record, then invokes the immutable
	// adapter. No callback-local completion map may manufacture DAG progress.
	execResult, selection, err := c.semanticSurface.executor.Execute(grant, c.semanticSurface.scope, c.semanticSurface.plan, c.semanticSurface.completed, func(selection tool.PlannedSelection) tool.SelectionExecutionResult {
		return c.executeBoundSemanticSelection(selection, argsJSON)
	})
	if err != nil {
		return semanticGrantRejectMessage(err.Error())
	}
	result := execResult.Result
	// An awaiting-receipt selection has consumed its one-time grant and must
	// immediately disappear from the model surface.  It has not completed its
	// effect, however, so it must not be passed to advanceSemanticCallSurface:
	// that function records DAG completion and could expose dependants before a
	// trusted transport receipt settles the operation.
	if execResult.AwaitingReceipt {
		if err := c.retireSemanticToolSurface(functionName, selection.ID); err != nil {
			return "[system rejected] semantic_plan_retire_failed"
		}
		return result
	}
	if !execResult.Succeeded || semanticSelectionFailed(result) {
		delete(c.semanticSurface.pendingArtifacts, selection.ID)
		return c.retireRejectedSemanticTool(functionName, selection.ID, result)
	}
	if err := c.registerSemanticArtifacts(selection.ID); err != nil {
		return "[system rejected] artifact_route_state_record_failed"
	}
	note, err := c.advanceSemanticToolSurface(selection.ID)
	if err != nil {
		return semanticAdvanceAfterSuccess(result, err)
	}
	return result + note
}

// executeSemanticToolCall persists the model call identity before grant
// consumption. The journal is deliberately in front of PlanExecutor: retrying
// the same host call returns its terminal projection rather than consuming a
// fresh grant or invoking the adapter twice.
func (c *sharedAgentLoopCallbacks) executeSemanticToolCall(functionName, argsJSON, callID string) string {
	return c.executeSemanticToolCallWithEpoch(functionName, argsJSON, callID, "")
}

func (c *sharedAgentLoopCallbacks) executeSemanticToolCallWithEpoch(functionName, argsJSON, callID, surfaceEpoch string) string {
	if c == nil || c.handler == nil || c.semanticSurface == nil || c.semanticSurface.issuer == nil || c.semanticSurface.executor == nil || c.semanticSurface.hostCalls == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	functionName = c.bindSemanticFunctionName(functionName, argsJSON)
	grant, ok := c.semanticSurface.grants[functionName]
	if !ok {
		grant, ok = c.semanticSurface.retiredGrants[functionName]
	}
	if !ok {
		return semanticGrantRejectMessage("selection_not_authorized")
	}
	if c.semanticSurface.coordinator != nil {
		return c.executeCoordinatedSemanticToolCall(functionName, argsJSON, callID, surfaceEpoch, grant)
	}
	selection, ok := semanticSelectionByID(c.semanticSurface.plan, grant.SelectionID)
	if !ok {
		return semanticGrantRejectMessage("invocation_grant_selection_not_found")
	}
	canonicalArgs, err := c.semanticCanonicalArguments(selection, argsJSON)
	requestDigest := "invalid:" + tool.SchemaDigest([]byte(argsJSON))
	if err == nil {
		requestDigest = canonicalArgs.Digest
	}
	identity := tool.HostCallIdentity{Protocol: "agent-loop/v1", ConnectionID: c.semanticHostConnectionID(), CallID: strings.TrimSpace(callID), SurfaceEpoch: strings.TrimSpace(surfaceEpoch)}
	record, action, journalErr := c.semanticSurface.hostCalls.Acquire(identity, tool.InvocationGrantFingerprint(grant), requestDigest, time.Now().UTC())
	if journalErr != nil {
		return semanticGrantRejectMessage(journalErr.Error())
	}
	action = tool.ResolveHostCallAcquireAction(action, record, requestDigest)
	switch action {
	case tool.HostCallAcquireReplay:
		return record.Result
	case tool.HostCallAcquireConflict:
		return "[system rejected] host_call_conflict"
	case tool.HostCallAcquireInProgress:
		return "[system rejected] host_call_in_progress"
	case tool.HostCallAcquireUnknown:
		// The journal recorded that this call may have reached its adapter.
		// Presenting a definite failure would invite a retry of an effect that
		// might already hold, so the uncertainty stays visible to the model.
		return "[system unknown] host_call_unknown"
	case tool.HostCallAcquireAdmit:
		// Continue below.
	default:
		return "[system rejected] host_call_unavailable"
	}
	if _, journalErr := c.semanticSurface.hostCalls.MarkAdmitted(identity, tool.InvocationGrantFingerprint(grant), requestDigest, time.Now().UTC()); journalErr != nil {
		return "[system rejected] " + journalErr.Error()
	}
	// Parameter rejection must still consume this one-time grant. The legacy
	// admission sequence owns that invariant; the journal merely remembers the
	// resulting terminal projection. Invalid JSON has no canonical request, so
	// it receives a separate fail-closed raw digest and cannot be confused with
	// a valid canonical request for the same host call ID.
	result := ""
	if err != nil {
		// Keep the host-call protocol aligned with the coordinated path.  Direct
		// unit/compatibility execution may retain detailed local diagnostics, but
		// a model-facing host call gets one stable terminal category and the
		// journal replays exactly that category for the same call ID.
		result = semanticModelParameterRejection(c.executeSemanticTool(functionName, argsJSON))
	} else {
		result = c.executeSemanticToolWithCanonical(functionName, grant, canonicalArgs)
	}
	if _, journalErr := c.semanticSurface.hostCalls.Complete(identity, tool.InvocationGrantFingerprint(grant), requestDigest, result, time.Now().UTC()); journalErr != nil {
		// The adapter may have run, but a host cannot safely report a result that
		// it failed to durably correlate with the original model call.
		return "[system rejected] " + journalErr.Error()
	}
	return result
}

// executeCoordinatedSemanticToolCall is the App-hosted execution path. It
// makes host-call state and one-time grant consumption one atomic commit for
// both valid and invalid requests. The adapter runs only after valid admission
// and completion is persisted with the route projection in a second
// transaction.
func (c *sharedAgentLoopCallbacks) executeCoordinatedSemanticToolCall(functionName, argsJSON, callID, surfaceEpoch string, grant tool.InvocationGrant) string {
	if c == nil || c.semanticSurface == nil || c.semanticSurface.coordinator == nil || c.semanticSurface.issuer == nil || c.semanticSurface.executor == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	selection, ok := semanticSelectionByID(c.semanticSurface.plan, grant.SelectionID)
	if !ok {
		return semanticGrantRejectMessage("invocation_grant_selection_not_found")
	}
	identity := tool.HostCallIdentity{Protocol: "agent-loop/v1", ConnectionID: c.semanticHostConnectionID(), CallID: strings.TrimSpace(callID), SurfaceEpoch: strings.TrimSpace(surfaceEpoch)}
	canonical, err := c.semanticCanonicalArguments(selection, argsJSON)
	if err != nil {
		// Keep the externally stable terminal category while retaining a
		// machine-readable subreason only in the durable execution record.  The
		// model must not be taught which hidden validation boundary it reached,
		// and every canonicalization failure consumes the same one-shot grant.
		result := "[system rejected] parameter_schema_invalid"
		reasonCode := semanticParameterRejectionReason(err)
		admission := tool.SemanticExecutionAdmission{Identity: identity, Grant: grant, RequestDigest: "invalid:" + tool.SchemaDigest([]byte(argsJSON)), Scope: c.semanticSurface.scope, Selection: selection, Now: time.Now().UTC()}
		if _, validationErr := c.semanticSurface.issuer.Validate(grant, c.semanticSurface.scope, c.semanticSurface.plan, c.semanticSurface.completed); validationErr != nil {
			return semanticGrantRejectMessage(validationErr.Error())
		}
		record, action, rejectErr := c.semanticSurface.coordinator.Reject(admission, result, reasonCode)
		if rejectErr != nil {
			return semanticGrantRejectMessage(rejectErr.Error())
		}
		action = tool.ResolveHostCallAcquireAction(action, record, admission.RequestDigest)
		switch action {
		case tool.HostCallAcquireReplay:
			return record.Result
		case tool.HostCallAcquireConflict:
			return "[system rejected] host_call_conflict"
		case tool.HostCallAcquireInProgress:
			return "[system rejected] host_call_in_progress"
		case tool.HostCallAcquireUnknown:
			return "[system unknown] host_call_unknown"
		}
		return c.retireRejectedSemanticTool(functionName, selection.ID, result)
	}
	if _, err := c.semanticSurface.issuer.Validate(grant, c.semanticSurface.scope, c.semanticSurface.plan, c.semanticSurface.completed); err != nil {
		return semanticGrantRejectMessage(err.Error())
	}
	// A prepared delivery intent is scoped to exactly one adapter invocation.
	// Never let a prior selection's transient projection influence a later
	// receipt-bound external effect in the same callback instance.
	c.semanticPreparedDelivery = nil
	admission := tool.SemanticExecutionAdmission{Identity: identity, Grant: grant, RequestDigest: canonical.Digest, Scope: c.semanticSurface.scope, Selection: selection, Now: time.Now().UTC()}
	record, action, err := c.semanticSurface.coordinator.Admit(admission)
	if err != nil {
		return semanticGrantRejectMessage(err.Error())
	}
	action = tool.ResolveHostCallAcquireAction(action, record, admission.RequestDigest)
	switch action {
	case tool.HostCallAcquireReplay:
		return record.Result
	case tool.HostCallAcquireConflict:
		return "[system rejected] host_call_conflict"
	case tool.HostCallAcquireInProgress:
		return "[system rejected] host_call_in_progress"
	case tool.HostCallAcquireUnknown:
		// A recorded may-have-happened call must not be replayed to the model
		// as a definite failure; that is what invites a duplicate mutation.
		return "[system unknown] host_call_unknown"
	case tool.HostCallAcquireAdmit:
		// The durable transaction is complete; it is now safe to perform I/O.
	default:
		return "[system rejected] host_call_unavailable"
	}
	result, selected, err := c.semanticSurface.executor.ExecuteAdmitted(selection, c.semanticSurface.scope, c.semanticSurface.plan, func(selection tool.PlannedSelection) tool.SelectionExecutionResult {
		ctx, cancel := c.semanticDynamicExecutionContext()
		defer cancel()
		return c.executeBoundSemanticSelectionCanonicalWithContext(agentservice.WithDynamicSemanticAdmission(ctx, admission), selection, canonical, true)
	})
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	if c.usesUnifiedDynamicEffectCoordinator(selected) && (result.AwaitingReceipt || result.Unknown || result.Succeeded || result.ReasonCode == "dynamic_binding_stale" || strings.HasSuffix(result.ReasonCode, "_binding_stale") || strings.HasSuffix(result.ReasonCode, "_bound_execution_unavailable")) {
		// The dynamic external-effect coordinator committed the host call and
		// plan execution atomically with the operation state. Completing it
		// again through the generic path would corrupt the one-way state
		// transition or turn an awaiting receipt into a false success.
		delete(c.semanticSurface.pendingArtifacts, selected.ID)
		if c.replanAfterSemanticLifecycleFailure(result.ReasonCode) {
			return result.Result
		}
		if err := c.retireSemanticToolSurface(functionName, selected.ID); err != nil {
			return "[system rejected] semantic_plan_retire_failed"
		}
		return result.Result
	}
	state := tool.PlanExecutionSucceeded
	if result.Unknown {
		state = tool.PlanExecutionUnknown
		result.Succeeded = false
		if strings.TrimSpace(result.ReasonCode) == "" {
			result.ReasonCode = "selection_execution_unknown"
		}
	} else if result.AwaitingReceipt {
		state = tool.PlanExecutionAwaitingReceipt
		result.Succeeded = false
		if strings.TrimSpace(result.ReasonCode) == "" {
			result.ReasonCode = "selection_awaiting_receipt"
		}
	} else if !result.Succeeded {
		state = tool.PlanExecutionFailed
		if strings.TrimSpace(result.ReasonCode) == "" {
			result.ReasonCode = "selection_execution_failed"
		}
	}
	artifacts := []tool.ArtifactRef(nil)
	payloads := []tool.ArtifactPayload(nil)
	if state == tool.PlanExecutionSucceeded {
		payloads = append(payloads, c.semanticSurface.pendingArtifacts[selected.ID]...)
		for _, payload := range payloads {
			artifacts = append(artifacts, payload.Ref)
		}
	}
	if state == tool.PlanExecutionAwaitingReceipt && c.semanticPreparedDelivery != nil {
		record, _, err := c.semanticSurface.coordinator.PrepareDeliveryAndComplete(admission, *c.semanticPreparedDelivery, result.Result, result.ReasonCode, time.Now().UTC())
		if err != nil {
			return "[system rejected] " + err.Error()
		}
		if c.semanticDelivery != nil {
			c.semanticDelivery.Scope = record.Scope
			c.semanticDelivery.SelectionID = record.SelectionID
			c.semanticDelivery.ChannelScope = record.ChannelScope
			c.semanticDelivery.DestinationID = record.DestinationID
		}
	} else if len(payloads) > 0 {
		if _, err := c.semanticSurface.coordinator.CompleteWithArtifactPayloads(admission, state, result.Result, result.ReasonCode, payloads, time.Now().UTC()); err != nil {
			return "[system rejected] " + err.Error()
		}
	} else if _, err := c.semanticSurface.coordinator.CompleteWithArtifacts(admission, state, result.Result, result.ReasonCode, artifacts, time.Now().UTC()); err != nil {
		return "[system rejected] " + err.Error()
	}
	if result.AwaitingReceipt {
		if err := c.retireSemanticToolSurface(functionName, selected.ID); err != nil {
			return "[system rejected] semantic_plan_retire_failed"
		}
		return result.Result
	}
	if !result.Succeeded || semanticSelectionFailed(result.Result) {
		delete(c.semanticSurface.pendingArtifacts, selected.ID)
		// Read-only dynamic bindings use the generic completion transaction,
		// unlike receipt-bound dynamic effects above. Once that terminal failure
		// is durable, a confirmed binding invalidation may publish one constrained
		// child revision. Do not apply this to builtin/channel failures: only a
		// selected dynamic binding can be refreshed without changing the need.
		if semanticSelectionIsDynamic(selected) && semanticReplanFailureEligible(result.ReasonCode) && c.replanAfterSemanticLifecycleFailure(result.ReasonCode) {
			return result.Result
		}
		return c.retireRejectedSemanticTool(functionName, selected.ID, result.Result)
	}
	// CompleteWithArtifacts made the immutable route projection visible in the
	// same transaction as selection success. The callback-local list is now
	// merely transient bookkeeping and must not write a second store.
	delete(c.semanticSurface.pendingArtifacts, selected.ID)
	note, err := c.advanceSemanticToolSurface(selected.ID)
	if err != nil {
		return semanticAdvanceAfterSuccess(result.Result, err)
	}
	return result.Result + note
}

// replanAfterSemanticLifecycleFailure advances only a binding-invalidated
// dynamic selection to one child revision. The child is built exclusively
// from trusted route input captured before the original plan was materialized;
// old grants, host calls and model arguments never enter the new surface.
//
// Unknown and awaiting-receipt outcomes are intentionally ineligible: their
// recovery path is receipt reconciliation, never a second dispatch.
func (c *sharedAgentLoopCallbacks) replanAfterSemanticLifecycleFailure(reasonCode string) bool {
	if c == nil || c.handler == nil || c.semanticSurface == nil || !semanticReplanFailureEligible(reasonCode) {
		return false
	}
	ctx, cancel := c.semanticDynamicExecutionContext()
	defer cancel()
	child, definitions, err := c.handler.replanSemanticCallSurfaceWithContext(ctx, c.semanticSurface, reasonCode)
	if err != nil {
		return false
	}
	c.semanticSurface = child
	c.tools = definitions
	return true
}

// semanticParameterRejectionReason preserves the validator's stable code for
// operators and replan policy without widening the model-visible error
// surface.  The public terminal result is deliberately one category:
// parameter_schema_invalid.  A model therefore cannot use field-specific
// errors as an oracle, but the host can still distinguish schema drift from a
// malformed payload during trusted diagnostics.
func semanticParameterRejectionReason(err error) string {
	if err == nil {
		return "parameter_schema_invalid"
	}
	code := strings.TrimSpace(err.Error())
	if !strings.HasPrefix(code, "parameter_") {
		return "parameter_schema_invalid"
	}
	return code
}

// semanticModelParameterRejection narrows validator details only at the
// model-host protocol boundary.  It deliberately leaves direct local adapter
// execution unchanged for operator diagnostics and tests; those calls do not
// carry a host call ID and are never replayed as model tool calls.
func semanticModelParameterRejection(result string) string {
	if strings.HasPrefix(strings.TrimSpace(result), "[system rejected] parameter_") {
		return "[system rejected] parameter_schema_invalid"
	}
	return result
}

func (c *sharedAgentLoopCallbacks) semanticHostConnectionID() string {
	if c != nil && c.semanticSurface != nil {
		return strings.TrimSpace(c.semanticSurface.hostConnectionID)
	}
	return ""
}

func (c *sharedAgentLoopCallbacks) bindSemanticFunctionName(functionName, argsJSON string) string {
	if c == nil || c.semanticSurface == nil {
		return strings.TrimSpace(functionName)
	}
	resolved := c.semanticSurface.resolveFunctionName(functionName)
	_ = argsJSON
	if resolved == strings.TrimSpace(functionName) {
		return resolved
	}
	return resolved
}

func semanticSelectionByID(plan tool.ToolPlan, selectionID string) (tool.PlannedSelection, bool) {
	for _, selection := range plan.Selections {
		if selection.ID == selectionID {
			return selection, true
		}
	}
	return tool.PlannedSelection{}, false
}

func (c *sharedAgentLoopCallbacks) executeSemanticToolWithCanonical(functionName string, grant tool.InvocationGrant, canonicalArgs tool.CanonicalRequest) string {
	execResult, selection, err := c.semanticSurface.executor.Execute(grant, c.semanticSurface.scope, c.semanticSurface.plan, c.semanticSurface.completed, func(selection tool.PlannedSelection) tool.SelectionExecutionResult {
		return c.executeBoundSemanticSelectionCanonical(selection, canonicalArgs)
	})
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	result := execResult.Result
	// See executeSemanticTool.  The canonical/journalled path has identical
	// receipt semantics: retire the current opaque function without projecting
	// a successful plan dependency.
	if execResult.AwaitingReceipt {
		if err := c.retireSemanticToolSurface(functionName, selection.ID); err != nil {
			return "[system rejected] semantic_plan_retire_failed"
		}
		return result
	}
	if !execResult.Succeeded || semanticSelectionFailed(result) {
		delete(c.semanticSurface.pendingArtifacts, selection.ID)
		return c.retireRejectedSemanticTool(functionName, selection.ID, result)
	}
	if err := c.registerSemanticArtifacts(selection.ID); err != nil {
		return "[system rejected] artifact_route_state_record_failed"
	}
	note, err := c.advanceSemanticToolSurface(selection.ID)
	if err != nil {
		return semanticAdvanceAfterSuccess(result, err)
	}
	return result + note
}

func removeToolDefinitionByName(definitions []map[string]interface{}, functionName string) []map[string]interface{} {
	functionName = strings.TrimSpace(functionName)
	filtered := definitions[:0]
	for _, definition := range definitions {
		if extractToolName(definition) != functionName {
			filtered = append(filtered, definition)
		}
	}
	return filtered
}

// retireRejectedSemanticTool closes a terminal model invocation without
// projecting plan success. It is deliberately shared by schema rejection and
// adapter failure paths: one opaque grant represents one execution attempt,
// not a parameter-probing or provider-retry handle. The retired lookup remains
// solely for durable same-host-call replay.
func (c *sharedAgentLoopCallbacks) retireRejectedSemanticTool(functionName, selectionID, result string) string {
	if c == nil || c.semanticSurface == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	if err := c.retireSemanticToolSurface(functionName, selectionID); err != nil {
		return "[system rejected] semantic_plan_retire_failed"
	}
	return result
}

// retireSemanticToolSurface removes one consumed adapter from the transient
// host list, durably retires its materialization, then replaces the entire
// model-visible list from the remaining active grants. The last step prevents
// branch-local append/remove code from accidentally retaining a stale opaque
// function or omitting another still-ready selection.
func (c *sharedAgentLoopCallbacks) retireSemanticToolSurface(functionName, selectionID string) error {
	if c == nil || c.semanticSurface == nil {
		return fmt.Errorf("semantic tool surface is unavailable")
	}
	// Hide immediately even if the durable write fails. The grant has already
	// been consumed, so re-exposure would create a stale retry authority.
	c.tools = removeToolDefinitionByName(c.tools, functionName)
	if _, err := retireSemanticCallSurfaceSelection(c.semanticSurface, selectionID); err != nil {
		return err
	}
	return c.syncSemanticToolSurface()
}

// advanceSemanticToolSurface commits trusted selection completion and derives
// the next complete exposure closure. It intentionally does not union a
// branch-local set of newly rendered tools with the old callback list.
// It returns the note, if any, that the caller must append to the result of
// the call it just completed. Returning it rather than exposing a separate
// lookup keeps a future execution path from silently omitting it.
func (c *sharedAgentLoopCallbacks) advanceSemanticToolSurface(selectionID string) (string, error) {
	if c == nil || c.semanticSurface == nil {
		return "", fmt.Errorf("semantic tool surface is unavailable")
	}
	var err error
	if c.semanticHoldDependantIssue {
		// Hold only the host-owned generate unlock. Same-family repeats
		// (second web_search / next edit) must still issue in this batch.
		err = completeSemanticCallSurfaceSelection(c.semanticSurface, selectionID)
		if err == nil {
			_, err = refreshSemanticCallSurfaceSkipping(c.semanticSurface, hostOwnedGenerateSelection)
			if semanticHasUnissuedReadyGenerate(c.semanticSurface) {
				c.semanticNeedDependantIssue = true
			}
		}
	} else {
		_, err = advanceSemanticCallSurface(c.semanticSurface, selectionID)
	}
	if err != nil {
		log.Printf("[semantic] plan advance failed selection=%q err=%v", selectionID, err)
		return "", err
	}
	if err := c.syncSemanticToolSurface(); err != nil {
		log.Printf("[semantic] plan surface sync failed selection=%q err=%v", selectionID, err)
		return "", err
	}
	return semanticSpentBudgetNote(c.semanticSurface, selectionID), nil
}

func hostOwnedGenerateSelection(selection tool.PlannedSelection) bool {
	return selection.AdapterName == "generate_pdf" || selection.FitProof.MatchedCapability == "document.generate.file"
}

func semanticHasUnissuedReadyGenerate(surface *semanticCallSurface) bool {
	if surface == nil {
		return false
	}
	for _, selection := range surface.plan.ReadySelections(surface.completed) {
		if surface.completed[selection.ID] || surface.materialized[selection.ID] {
			continue
		}
		if hostOwnedGenerateSelection(selection) {
			return true
		}
	}
	return false
}

func (c *sharedAgentLoopCallbacks) releaseSemanticDependantIssue() {
	if c == nil {
		return
	}
	// A complete tool batch may already have advanced the in-memory semantic
	// plan before its paired history reaches durable storage.  Final-response
	// projection is deliberately allowed to retry a previously failed release,
	// but it must never turn an outstanding pre-tool checkpoint into a new
	// authority boundary.  In particular, attachment flushing can run on
	// terminal/error paths, so keep the dependant held until the batch is
	// committed (or an interactive pause has atomically paired it).
	if c.hasPendingToolBatch || c.semanticDurabilityBlocked {
		return
	}
	c.semanticHoldDependantIssue = false
	if c.semanticSurface == nil || !c.semanticNeedDependantIssue {
		return
	}
	if _, err := refreshSemanticCallSurface(c.semanticSurface); err != nil {
		log.Printf("[semantic] delayed dependant issue failed: %v", err)
		return
	}
	if semanticHasUnissuedReadyGenerate(c.semanticSurface) {
		log.Printf("[semantic] delayed generate still unissued after refresh")
		return
	}
	c.semanticNeedDependantIssue = false
	if err := c.syncSemanticToolSurface(); err != nil {
		log.Printf("[semantic] delayed dependant sync failed: %v", err)
	}
}

func semanticRetiredGeneratePDF(surface *semanticCallSurface) bool {
	if surface == nil {
		return false
	}
	if _, ok := surface.retiredGrants["generate_pdf"]; ok {
		return true
	}
	for _, grant := range surface.retiredGrants {
		if grant.AdapterName == "generate_pdf" {
			return true
		}
	}
	return false
}

// semanticAdvanceAfterSuccess keeps a trusted adapter result when the next
// exposure closure cannot be derived. Replacing success with a generic reject
// made the model treat a published PDF as unauthorized and retry the spent
// grant. The host still logs the real advance error for operators.
func semanticAdvanceAfterSuccess(result string, err error) string {
	if err != nil {
		log.Printf("[semantic] keep successful result after plan advance failure err=%v", err)
	}
	trimmed := strings.TrimSpace(result)
	if trimmed == "" {
		trimmed = "The previous step succeeded."
	}
	return trimmed + "\n\n[system] Next-step tools could not be exposed after this succeeded. Continue from the published artifact; do not retry this grant."
}

func (c *sharedAgentLoopCallbacks) syncSemanticToolSurface() error {
	if c == nil || c.semanticSurface == nil {
		return fmt.Errorf("semantic tool surface is unavailable")
	}
	definitions, err := visibleSemanticCallSurfaceDefinitions(c.semanticSurface)
	if err != nil {
		return err
	}
	c.tools = definitions
	return nil
}

func semanticSelectionFailed(result string) bool {
	trimmed := strings.TrimSpace(result)
	return strings.HasPrefix(trimmed, "[system rejected]") || strings.HasPrefix(strings.ToLower(trimmed), "error:")
}

// semanticSelectionOutcomeUnknown recognises the marker a trusted host adapter
// emits when its effect may or may not have landed: an SSH session that timed
// out mid-command, a browser or desktop host that vanished, a delegate whose
// child receipt never arrived, a push whose remote could not be read back.
//
// This is deliberately not a failure. A failure says the effect did not happen
// and may be retried; an unknown says nobody can tell, so the plan must record
// PlanExecutionUnknown and consume the grant rather than invite a retry that
// could commit the same effect twice.
func semanticSelectionOutcomeUnknown(result string) bool {
	return strings.HasPrefix(strings.TrimSpace(result), "[system unknown]")
}

// semanticGrantRejectMessage keeps the machine-readable grant code so tests and
// telemetry still match, but tells the model to answer instead of asking the
// user to re-authorize tools that this turn cannot run.
func semanticGrantRejectMessage(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		code = "selection_not_authorized"
	}
	msg := "[system rejected] " + code
	switch {
	case strings.Contains(code, "selection_not_authorized"),
		strings.Contains(code, "invocation_grant_replayed"),
		strings.Contains(code, "invocation_grant_expired"),
		strings.Contains(code, "invocation_grant_selection_not_found"):
		return msg + ". If a lookup already returned evidence, answer from that evidence. Do not ask the user to re-authorize tools."
	default:
		return msg
	}
}

// semanticSelectionRequiresReceipt is derived from the immutable planned
// effect class, not a provider/tool name. An adapter that cannot supply the
// appropriate trusted receipt boundary is not executable for such a selection.
func semanticSelectionRequiresReceipt(selection tool.PlannedSelection) bool {
	for _, effect := range selection.Effects {
		if effect == tool.EffectExternalEffect || effect == tool.EffectSensitive || effect == tool.EffectLocalMutation {
			return true
		}
	}
	return false
}

// semanticBuiltinLocalMutationSelection identifies a host-owned builtin
// provider whose declared effects are local mutations with no external effect.
// The mutation is performed by the same host process that observes the
// outcome, so the legacy handler result classified by semanticSelectionFailed
// is the authoritative local completion receipt: a rejected/error projection
// is a failure, anything else is a committed local mutation. This boundary is
// deliberately unavailable to external effects, whose transport outcome the
// host cannot authoritatively observe from a synchronous text return.
func semanticBuiltinLocalMutationSelection(selection tool.PlannedSelection) bool {
	if !strings.EqualFold(strings.TrimSpace(selection.Provider.Kind), "builtin") {
		return false
	}
	local := false
	for _, effect := range selection.Effects {
		switch effect {
		case tool.EffectSensitive, tool.EffectLocalMutation:
			local = true
		case tool.EffectExternalEffect:
			return false
		}
	}
	return local
}

// semanticHostObservedExternalSelection identifies a host-owned trusted adapter
// whose external effect this same process observes before it returns: the SSH,
// browser, or desktop session it waits on, or the git ref it reads back after a
// commit or push. The handler result is therefore the observation receipt, in
// the same sense that a builtin local mutation's result is one.
//
// This is deliberately an allow-list of adapters rather than a test on the
// effect class. "External effect" alone says nothing about whether anyone
// watched the outcome, so a selection that merely declares it must keep failing
// closed; only these four carry a host that did the watching.
//
// Unlike a channel send, none of these hand work to a transport whose outcome
// arrives later, so they must not enter the delivery coordinator. Schedule
// dispatch and message.send.im stay on that path.
func semanticHostObservedExternalSelection(selection tool.PlannedSelection) bool {
	if !strings.EqualFold(strings.TrimSpace(selection.Provider.Kind), "builtin") {
		return false
	}
	external := false
	for _, effect := range selection.Effects {
		if effect == tool.EffectExternalEffect {
			external = true
		}
	}
	if !external {
		return false
	}
	switch strings.TrimSpace(selection.AdapterName) {
	case semanticTrustedSSHAdapter, semanticTrustedBrowserAdapter,
		semanticTrustedComputerUseAdapter, semanticTrustedRepoMutateAdapter:
		return true
	default:
		return false
	}
}

// semanticSelectionAwaitsReceipt identifies a local, host-owned receipt-aware
// adapter that has prepared an operation but whose trusted transport outcome
// is still pending. It keeps the generic PlanExecutor state machine
// independent of IM text, while ensuring that a prepared external hand-off can
// never be mistaken for a successful selection merely because the adapter
// returned a friendly status string.
func semanticSelectionAwaitsReceipt(selection tool.PlannedSelection) bool {
	return semanticSelectionRequiresReceipt(selection) && (semanticCurrentChannelArtifactDelivery(selection) || semanticScheduleChannelDispatch(selection))
}

// executeBoundSemanticSelection is the one host projection from an immutable
// adapter outcome to PlanExecutor state. Dynamic providers already return a
// structured, receipt-aware result from their common catalog: reducing that to
// text here would incorrectly mark awaiting/unknown effects as selection
// success. Builtin/channel adapters retain the legacy text projection until
// their own execution families move to the same typed boundary.
func (c *sharedAgentLoopCallbacks) executeBoundSemanticSelection(selection tool.PlannedSelection, argsJSON string) tool.SelectionExecutionResult {
	if c == nil || c.semanticSurface == nil {
		return tool.SelectionExecutionResult{Result: "[system rejected] semantic tool surface is unavailable", ReasonCode: "semantic_surface_unavailable"}
	}
	canonicalArgs, err := c.semanticCanonicalArguments(selection, argsJSON)
	if err != nil {
		return tool.SelectionExecutionResult{Result: "[system rejected] " + err.Error(), ReasonCode: err.Error()}
	}
	return c.executeBoundSemanticSelectionCanonical(selection, canonicalArgs)
}

func (c *sharedAgentLoopCallbacks) executeBoundSemanticSelectionCanonical(selection tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) tool.SelectionExecutionResult {
	return c.executeBoundSemanticSelectionCanonicalWithContext(context.Background(), selection, canonicalArgs, false)
}

// executeBoundSemanticSelectionCanonicalWithContext carries the already
// admitted host call only across the trusted dynamic execution boundary. It
// is never derived from a model argument or forwarded to a provider.
func (c *sharedAgentLoopCallbacks) executeBoundSemanticSelectionCanonicalWithContext(ctx context.Context, selection tool.PlannedSelection, canonicalArgs tool.CanonicalRequest, coordinated bool) tool.SelectionExecutionResult {
	if c == nil || c.semanticSurface == nil {
		return tool.SelectionExecutionResult{Result: "[system rejected] semantic tool surface is unavailable", ReasonCode: "semantic_surface_unavailable"}
	}
	if result, handled := c.executeSemanticDynamicProviderWithContext(ctx, selection, string(canonicalArgs.CanonicalJSON)); handled {
		return result
	}
	// An external/sensitive selection may never pass through the legacy text
	// dispatcher unless its provider family exposes a trusted receipt boundary.
	// Otherwise a friendly local return could be mistaken for delivery, write,
	// or publication success. Dynamic providers have already taken their typed
	// coordinator path above; current-channel delivery is the host-owned
	// receipt-aware channel adapter below; a builtin sensitive-only selection is
	// a host-local mutation whose host-owned handler outcome is the
	// authoritative local completion receipt; a host-observed external adapter
	// watched its own effect land before returning. Every other such selection
	// fails closed before its legacy handler can produce an untracked side
	// effect.
	if semanticSelectionRequiresReceipt(selection) && !semanticCurrentChannelArtifactDelivery(selection) && !semanticScheduleChannelDispatch(selection) && !semanticBuiltinLocalMutationSelection(selection) && !semanticHostObservedExternalSelection(selection) {
		return tool.SelectionExecutionResult{Result: "[system rejected] external_effect_receipt_boundary_missing", ReasonCode: "external_effect_receipt_boundary_missing"}
	}
	// A legacy multiplexer reached through a managed grant is bounded by the
	// capability, not by the breadth of its own schema. This refuses before the
	// adapter runs, so an out-of-bound action never becomes an effect.
	if reason, refused := semanticManagedInvocationRefusal(selection, canonicalArgs); refused {
		return tool.SelectionExecutionResult{Result: semanticManagedMISRefusalText(selection.AdapterName, reason), Succeeded: false, ReasonCode: reason}
	}
	result := c.executeBoundSemanticAdapterCanonical(selection, canonicalArgs, coordinated)
	if semanticSelectionFailed(result) {
		return tool.SelectionExecutionResult{Result: result, Succeeded: false, ReasonCode: "selection_execution_failed"}
	}
	// Checked before the awaiting-receipt branch on purpose: an adapter that
	// reports an indeterminate outcome must not have that answer upgraded into
	// "a receipt is still coming", which would leave the plan waiting for
	// evidence no one is going to produce.
	if semanticSelectionOutcomeUnknown(result) {
		return tool.SelectionExecutionResult{Result: result, Succeeded: false, Unknown: true, ReasonCode: "selection_execution_unknown"}
	}
	if semanticSelectionAwaitsReceipt(selection) {
		return tool.SelectionExecutionResult{Result: result, AwaitingReceipt: true, ReasonCode: "selection_awaiting_receipt"}
	}
	c.recordSemanticLookupEvidence(selection, result)
	return tool.SelectionExecutionResult{Result: result, Succeeded: true}
}

func (c *sharedAgentLoopCallbacks) usesUnifiedDynamicEffectCoordinator(selection tool.PlannedSelection) bool {
	if c == nil || c.handler == nil || !semanticSelectionIsDynamic(selection) || !semanticDynamicSelectionRequiresReceipt(selection) {
		return false
	}
	kind := strings.ToLower(strings.TrimSpace(selection.Provider.Kind))
	if kind != "mcp" && kind != "skill" {
		return false
	}
	coordinator, err := c.handler.semanticDynamicEffectCoordinator()
	if err != nil {
		return false
	}
	unified, ok := coordinator.(agentservice.UnifiedSemanticEffectCoordinator)
	return ok && unified.UsesSemanticExecutionCoordinator()
}

func semanticSelectionIsDynamic(selection tool.PlannedSelection) bool {
	return tool.SelectionIsDynamic(selection)
}

func (c *sharedAgentLoopCallbacks) executeBoundSemanticAdapter(selection tool.PlannedSelection, argsJSON string) string {
	if c == nil || c.semanticSurface == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	canonicalArgs, err := c.semanticCanonicalArguments(selection, argsJSON)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	return c.executeBoundSemanticAdapterCanonical(selection, canonicalArgs, false)
}

func (c *sharedAgentLoopCallbacks) executeBoundSemanticAdapterCanonical(selection tool.PlannedSelection, canonicalArgs tool.CanonicalRequest, coordinated bool) string {
	if c == nil || c.semanticSurface == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	// Dynamic providers are never sent through the legacy registry dispatcher:
	// that dispatcher resolves mutable names and would turn a semantic selection
	// back into a free gateway. The bound catalog revalidates the immutable
	// Skill/MCP identity immediately before transport.
	// Dynamic MCP/Skill execution is dispatched by
	// executeBoundSemanticSelectionCanonical so its structured unknown/receipt
	// state reaches PlanExecutor intact. Never re-enter the legacy text path.
	if kind := strings.ToLower(strings.TrimSpace(selection.Provider.Kind)); kind == "mcp" || kind == "skill" {
		return "[system rejected] dynamic_selection_dispatch_boundary_missing"
	}
	if semanticScheduleChannelDispatch(selection) {
		target := trustedLoopDeliveryTarget(c.loopCtx)
		if target == nil {
			return "[system rejected] trusted_delivery_target_missing"
		}
		if !strings.EqualFold(strings.TrimSpace(selection.Provider.ProviderID), strings.TrimSpace(target.ChannelScope)) {
			return "[system rejected] trusted_delivery_channel_mismatch"
		}
		record, err := c.semanticSurface.artifacts.prepareScheduleDispatchIntent(selection, target.ChannelScope, target.DestinationID)
		if err != nil {
			return "[system rejected] " + err.Error()
		}
		if c.handler != nil {
			if taskID := c.handler.takeAdministeredTaskID(); taskID != "" {
				principal := c.userID
				if c.semanticSurface != nil {
					principal = c.semanticSurface.scope.PrincipalID
				}
				if bindErr := c.handler.bindScheduleDispatch(taskID, target.ChannelScope, target.DestinationID, principal); bindErr != nil {
					return "[system rejected] " + bindErr.Error()
				}
			}
		}
		c.semanticDelivery = &semanticDeliveryProjection{Scope: record.Scope, SelectionID: record.SelectionID, Store: c.semanticSurface.artifacts.store, Executor: c.semanticSurface.executor, Coordinator: c.semanticSurface.coordinator, ChannelScope: record.ChannelScope, DestinationID: record.DestinationID}
		if c.semanticSurface.coordinator != nil {
			c.semanticPreparedDelivery = &tool.DeliveryRecord{Scope: c.semanticSurface.scope, SelectionID: selection.ID, ArtifactID: record.ArtifactID, ArtifactSourceScope: record.ArtifactSourceScope, ChannelScope: target.ChannelScope, DestinationID: target.DestinationID, State: tool.DeliveryPrepared}
		}
		return "Scheduled channel dispatch prepared (delivery record " + record.SelectionID + "). This is not a send."
	}
	if semanticMessageSendIM(selection) {
		target := trustedLoopDeliveryTarget(c.loopCtx)
		if target == nil {
			return "[system rejected] trusted_delivery_target_missing"
		}
		if !strings.EqualFold(strings.TrimSpace(selection.Provider.ProviderID), strings.TrimSpace(target.ChannelScope)) {
			return "[system rejected] trusted_delivery_channel_mismatch"
		}
		var args map[string]interface{}
		if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
			return "[system rejected] canonical_message_send_arguments_invalid"
		}
		text, err := semanticTrustedMessageSendArgsAllowed(args)
		if err != nil {
			return "[system rejected] " + err.Error()
		}
		record, err := c.semanticSurface.artifacts.prepareTrustedMessageSend(selection, target.ChannelScope, target.DestinationID, text)
		if err != nil {
			return "[system rejected] " + err.Error()
		}
		c.semanticDelivery = &semanticDeliveryProjection{Scope: record.Scope, SelectionID: record.SelectionID, Store: c.semanticSurface.artifacts.store, Executor: c.semanticSurface.executor, Coordinator: c.semanticSurface.coordinator, ChannelScope: record.ChannelScope, DestinationID: record.DestinationID}
		if c.semanticSurface.coordinator != nil {
			c.semanticPreparedDelivery = &tool.DeliveryRecord{Scope: c.semanticSurface.scope, SelectionID: selection.ID, ArtifactID: record.ArtifactID, ArtifactSourceScope: record.ArtifactSourceScope, ChannelScope: target.ChannelScope, DestinationID: target.DestinationID, State: tool.DeliveryPrepared}
		}
		return "IM message prepared for the trusted destination (delivery record " + record.SelectionID + "). This is not a send."
	}
	if semanticCurrentChannelArtifactDelivery(selection) || semanticSpecifiedTargetArtifactDelivery(selection) {
		target := trustedLoopDeliveryTarget(c.loopCtx)
		if target == nil {
			return "[system rejected] trusted_delivery_target_missing"
		}
		if !strings.EqualFold(strings.TrimSpace(selection.Provider.ProviderID), strings.TrimSpace(target.ChannelScope)) {
			return "[system rejected] trusted_delivery_channel_mismatch"
		}
		artifact, record, err := c.semanticSurface.artifacts.prepareCurrentChannelDelivery(selection, target.ChannelScope, target.DestinationID, firstRequiredArtifactContract(selection.Consumes))
		if err != nil {
			return "[system rejected] " + err.Error()
		}
		switch strings.ToLower(strings.TrimSpace(artifact.Ref.Kind)) {
		case "image":
			c.semanticDeliveryImageKey = artifact.Base64
		case "document":
			c.semanticDeliveryFileData = artifact.Base64
			c.semanticDeliveryFileMIME = artifact.Ref.MIMEType
			c.semanticDeliveryFileName = semanticArtifactFileName(artifact.Ref)
		case "audio":
			c.semanticDeliveryVoiceData = artifact.Base64
			c.semanticDeliveryVoiceMIME = artifact.Ref.MIMEType
			c.semanticDeliveryVoiceName = semanticArtifactFileName(artifact.Ref)
		default:
			return "[system rejected] current_channel_artifact_kind_unsupported"
		}
		// A logical delivery may already have been prepared by a prior revision
		// of the same root task. Keep the record's immutable scope rather than
		// pretending that the current selection owns the prior external effect.
		c.semanticDelivery = &semanticDeliveryProjection{Scope: record.Scope, SelectionID: record.SelectionID, Store: c.semanticSurface.artifacts.store, Executor: c.semanticSurface.executor, Coordinator: c.semanticSurface.coordinator, ChannelScope: record.ChannelScope, DestinationID: record.DestinationID, OnSettled: func(outcome tool.DeliveryState) {
			if c.handler == nil {
				return
			}
			switch outcome {
			case tool.DeliveryAccepted:
				c.handler.markSessionGovernedTaskStatus(c.userID, c.platform, sessionGovernedDestination(c.loopCtx), sessionGovernedSucceeded)
			case tool.DeliveryFailed:
				c.handler.markSessionGovernedTaskStatus(c.userID, c.platform, sessionGovernedDestination(c.loopCtx), sessionGovernedFailedExec)
			}
		}}
		if c.semanticSurface.coordinator != nil {
			// App hosts defer durable outbox creation until it can be committed
			// with the host-call terminal projection.
			c.semanticPreparedDelivery = &tool.DeliveryRecord{Scope: c.semanticSurface.scope, SelectionID: selection.ID, ArtifactID: artifact.Ref.ID, ArtifactSourceScope: artifact.Ref.Scope, ChannelScope: target.ChannelScope, DestinationID: target.DestinationID, State: tool.DeliveryPrepared}
		}
		return "Artifact prepared for delivery to the current channel (delivery record " + record.SelectionID + ")."
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == "semantic_read_trusted_document" {
		return c.executeTrustedDocumentRead(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedAudioTranscribeAdapter {
		return c.executeTrustedAudioTranscribe(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedAuditReadAdapter {
		return c.executeTrustedAuditRead(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedKnowledgeAdminAdapter {
		return c.executeTrustedKnowledgeAdmin(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedKnowledgeIngestAdapter {
		return c.executeTrustedKnowledgeIngest(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedKnowledgeReadAdapter {
		return c.executeTrustedKnowledgeRead(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedFileWriteAdapter {
		return c.executeTrustedFileWrite(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedFileReadAdapter {
		return c.executeTrustedFileRead(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedRepoInspectAdapter {
		return c.executeTrustedRepoInspect(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedWebFetchAdapter {
		return c.executeTrustedWebFetch(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedWebSearchAdapter {
		return c.executeTrustedWebSearch(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedLiveDataVisualAdapter {
		return c.publishRenderedLiveDataVisualArtifact(selection, c.executeTrustedLiveDataVisual(), coordinated)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedClockAdapter {
		return c.executeTrustedClock(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedConfigAdapter {
		return c.executeTrustedConfig(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedMemoryAdapter {
		return c.executeTrustedMemory(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedMemoryRecallAdapter {
		return c.executeTrustedMemoryRecall(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedTaskAdapter {
		return c.executeTrustedTask(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedGoalAdapter {
		return c.executeTrustedGoal(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedTemplateAdapter {
		return c.executeTrustedTemplate(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedSessionAdapter {
		return c.executeTrustedSession(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedScheduleAdapter {
		return c.executeTrustedSchedule(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedOfficeWriteAdapter {
		return c.executeTrustedOfficeWrite(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedAcquireRemoteAdapter {
		return c.executeTrustedAcquireRemote(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedShellAdapter {
		return c.executeTrustedShell(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedBuildVerifyAdapter {
		return c.executeTrustedBuildVerify(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedDelegateAdapter {
		return c.executeTrustedDelegate(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedSSHAdapter {
		return c.executeTrustedSSH(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedBrowserAdapter {
		return c.executeTrustedBrowser(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedComputerUseAdapter {
		return c.executeTrustedComputerUse(selection, canonicalArgs)
	}
	if selection.Provider.Kind == "builtin" && selection.AdapterName == semanticTrustedRepoMutateAdapter {
		return c.executeTrustedRepoMutate(selection, canonicalArgs)
	}
	c.semanticCapturedImageKey = ""
	if strings.EqualFold(strings.TrimSpace(string(selection.FitProof.MatchedCapability)), "document.generate.file") {
		c.semanticDeferFileMaterialize = true
		result := c.executeLegacyBoundAdapter(selection.AdapterName, string(canonicalArgs.CanonicalJSON))
		c.semanticDeferFileMaterialize = false
		return c.publishGeneratedDocumentArtifact(selection, result, coordinated)
	}
	if strings.EqualFold(strings.TrimSpace(string(selection.FitProof.MatchedCapability)), string(tool.CapabilityAudioRenderSpeech)) {
		result := c.executeLegacyBoundAdapter(selection.AdapterName, string(canonicalArgs.CanonicalJSON))
		return c.publishRenderedSpeechArtifact(selection, result, coordinated)
	}
	result := c.executeLegacyBoundAdapter(selection.AdapterName, string(canonicalArgs.CanonicalJSON))
	if strings.TrimSpace(selection.AdapterName) != "screenshot" {
		return result
	}
	imageKey := strings.TrimSpace(c.semanticCapturedImageKey)
	if imageKey == "" {
		return result
	}
	if coordinated && c.semanticSurface.coordinator != nil {
		payload, err := c.semanticSurface.artifacts.newPNGPayload(selection.ID, imageKey)
		if err != nil {
			return "[system rejected] artifact_publish_failed"
		}
		c.semanticSurface.pendingArtifacts[selection.ID] = append(c.semanticSurface.pendingArtifacts[selection.ID], payload)
	} else {
		ref, err := c.semanticSurface.artifacts.publishPNG(selection.ID, imageKey)
		if err != nil {
			return "[system rejected] artifact_publish_failed"
		}
		// Non-coordinated unit hosts preserve their historical publish-before-
		// projection sequence; only production owns the unified transaction.
		c.semanticSurface.pendingArtifacts[selection.ID] = append(c.semanticSurface.pendingArtifacts[selection.ID], tool.ArtifactPayload{Ref: ref})
	}
	// Capture merely publishes a broker artifact. It cannot place a payload on a
	// gateway response; only the dependent delivery selection may do that.
	c.screenshotImageKey = ""
	return result
}

func (c *sharedAgentLoopCallbacks) publishGeneratedDocumentArtifact(selection tool.PlannedSelection, result string, coordinated bool) string {
	if c == nil || c.semanticSurface == nil || c.semanticSurface.artifacts == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	obs := parseToolPayloadResult(result)
	if obs.File == nil || strings.TrimSpace(obs.File.data) == "" {
		return strings.TrimSpace(obs.ToolContent)
	}
	if coordinated && c.semanticSurface.coordinator != nil {
		payload, err := c.semanticSurface.artifacts.newPDFPayload(selection.ID, obs.File.data)
		if err != nil {
			return "[system rejected] artifact_publish_failed"
		}
		c.semanticSurface.pendingArtifacts[selection.ID] = append(c.semanticSurface.pendingArtifacts[selection.ID], payload)
	} else {
		ref, err := c.semanticSurface.artifacts.publishPDF(selection.ID, obs.File.data)
		if err != nil {
			return "[system rejected] artifact_publish_failed"
		}
		c.semanticSurface.pendingArtifacts[selection.ID] = append(c.semanticSurface.pendingArtifacts[selection.ID], tool.ArtifactPayload{Ref: ref})
	}
	return "PDF artifact published; deliver it through the current-channel file adapter."
}

func (c *sharedAgentLoopCallbacks) executeTrustedLiveDataVisual() string {
	if c == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	imageBase64, err := renderTrustedLiveDataVisual(c.userText, c.semanticLookupEvidence)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	return "[png_base64]" + imageBase64
}

func (c *sharedAgentLoopCallbacks) publishRenderedLiveDataVisualArtifact(selection tool.PlannedSelection, result string, coordinated bool) string {
	if c == nil || c.semanticSurface == nil || c.semanticSurface.artifacts == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	if strings.HasPrefix(strings.TrimSpace(result), "[system rejected]") {
		return strings.TrimSpace(result)
	}
	imageBase64 := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(result), "[png_base64]"))
	if imageBase64 == "" {
		return "[system rejected] trusted_live_data_visual_invalid_png"
	}
	if _, err := base64.StdEncoding.DecodeString(imageBase64); err != nil {
		return "[system rejected] trusted_live_data_visual_invalid_png"
	}
	if coordinated && c.semanticSurface.coordinator != nil {
		payload, err := c.semanticSurface.artifacts.newPNGPayload(selection.ID, imageBase64)
		if err != nil {
			return "[system rejected] artifact_publish_failed"
		}
		c.semanticSurface.pendingArtifacts[selection.ID] = append(c.semanticSurface.pendingArtifacts[selection.ID], payload)
	} else {
		ref, err := c.semanticSurface.artifacts.publishPNG(selection.ID, imageBase64)
		if err != nil {
			return "[system rejected] artifact_publish_failed"
		}
		c.semanticSurface.pendingArtifacts[selection.ID] = append(c.semanticSurface.pendingArtifacts[selection.ID], tool.ArtifactPayload{Ref: ref})
	}
	return "Live-data PNG artifact published; deliver it through the current-channel image adapter."
}

func (c *sharedAgentLoopCallbacks) publishRenderedSpeechArtifact(selection tool.PlannedSelection, result string, coordinated bool) string {
	if c == nil || c.semanticSurface == nil || c.semanticSurface.artifacts == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	wavBase64 := strings.TrimSpace(result)
	if strings.HasPrefix(wavBase64, toolPayloadSpeechArtifactPrefix) {
		wavBase64 = strings.TrimPrefix(wavBase64, toolPayloadSpeechArtifactPrefix)
	} else {
		return strings.TrimSpace(result)
	}
	if coordinated && c.semanticSurface.coordinator != nil {
		payload, err := c.semanticSurface.artifacts.newWAVPayload(selection.ID, wavBase64)
		if err != nil {
			return "[system rejected] artifact_publish_failed"
		}
		c.semanticSurface.pendingArtifacts[selection.ID] = append(c.semanticSurface.pendingArtifacts[selection.ID], payload)
	} else {
		ref, err := c.semanticSurface.artifacts.publishWAV(selection.ID, wavBase64)
		if err != nil {
			return "[system rejected] artifact_publish_failed"
		}
		c.semanticSurface.pendingArtifacts[selection.ID] = append(c.semanticSurface.pendingArtifacts[selection.ID], tool.ArtifactPayload{Ref: ref})
	}
	return "Speech artifact published; deliver it through the current-channel voice adapter. This is not a send."
}

// semanticCurrentChannelArtifactDelivery recognizes the capability contract,
// not an implementation adapter name. New receipt-aware current-channel
// providers can therefore participate without adding a name-triggered branch
// to the semantic execution path.
func semanticCurrentChannelArtifactDelivery(selection tool.PlannedSelection) bool {
	if !strings.EqualFold(strings.TrimSpace(selection.Provider.Kind), "channel") {
		return false
	}
	// PlannedSelection stores the chosen binding, not the full ProviderSpec;
	// its immutable fit proof is the contract-level authority at this execution
	// boundary.
	return strings.EqualFold(strings.TrimSpace(string(selection.FitProof.MatchedCapability)), "artifact.deliver.current_channel")
}

func semanticScheduleChannelDispatch(selection tool.PlannedSelection) bool {
	if !strings.EqualFold(strings.TrimSpace(selection.Provider.Kind), "channel") {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(string(selection.FitProof.MatchedCapability)), string(tool.CapabilityScheduleDispatchChannel))
}

func semanticArtifactFileName(ref tool.ArtifactRef) string {
	base := "attachment"
	switch strings.ToLower(strings.TrimSpace(ref.MIMEType)) {
	case "application/pdf":
		return base + ".pdf"
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return base + ".docx"
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return base + ".xlsx"
	case "application/vnd.openxmlformats-officedocument.presentationml.presentation":
		return base + ".pptx"
	case "text/plain":
		return base + ".txt"
	case "audio/wav", "audio/x-wav", "audio/wave":
		return base + ".wav"
	default:
		return base + ".bin"
	}
}

func (c *sharedAgentLoopCallbacks) groupPolicy() *lansengerGroupPermissionPolicy {
	if c == nil || c.loopCtx == nil {
		return nil
	}
	return c.loopCtx.LansengerGroupPermissions
}

func groupPermissionRejected() string {
	return "[system rejected] 群聊权限未授权该工具访问本地资源或知识库"
}

func (c *sharedAgentLoopCallbacks) rejectGroupWeb() string {
	if policy := c.groupPolicy(); policy != nil && !policy.AllowWebSearch {
		return groupPermissionRejected()
	}
	return ""
}

func (c *sharedAgentLoopCallbacks) rejectGroupFileRead() string {
	if policy := c.groupPolicy(); policy != nil && !policy.AllowAllDirectories && len(policy.AllowedDirectories) == 0 {
		return groupPermissionRejected()
	}
	return ""
}

func (c *sharedAgentLoopCallbacks) rejectGroupLocalAdmin() string {
	if c.groupPolicy() != nil {
		return groupPermissionRejected()
	}
	return ""
}

func (c *sharedAgentLoopCallbacks) rejectGroupKnowledgeRead() string {
	if policy := c.groupPolicy(); policy != nil && !policy.allowsKnowledge() {
		return groupPermissionRejected()
	}
	return ""
}

// executeTrustedDocumentRead is the semantic-only adapter for local document
// input. The model schema deliberately has no file_path/path/action field;
// all three are injected after an exact ArtifactRef dependency has been
// checked and consumed by the scoped broker.
func (c *sharedAgentLoopCallbacks) executeTrustedDocumentRead(selection tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if c == nil || c.semanticSurface == nil || c.semanticSurface.artifacts == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	contract := firstRequiredArtifactContract(selection.Consumes)
	payload, err := c.semanticSurface.artifacts.consumeTrustedInput(selection, contract)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	path, cleanup, err := materializeTrustedDocument(payload, semanticDocumentTempSuffixForSelection(selection))
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	defer cleanup()
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_document_arguments_invalid"
	}
	args["file_path"] = path
	// ToolReadDocument includes its resolved source path in the legacy result
	// envelope. That private temp path must not become model context, so the
	// semantic adapter returns a stable, path-free projection instead.
	result := agent.ToolReadDocumentWithContext(args, c.handler.getMaclawLLMConfig().EffectiveContextTokens())
	// A read that failed still comes back as a formatted body, so without
	// asking the envelope the turn records the failure notice as the document.
	if class, failed := agent.DocumentReadFailure(result); failed {
		return fmt.Sprintf("[system rejected] trusted_document_read_failed_%s", class)
	}
	return semanticDocumentReadResultProjection(result)
}

func semanticDocumentTempSuffixForSelection(selection tool.PlannedSelection) string {
	switch strings.ToLower(strings.TrimSpace(selection.FitProof.QualifierBindings["format"])) {
	case "pdf":
		return ".pdf"
	case "word":
		return ".docx"
	case "spreadsheet":
		return ".xlsx"
	case "presentation":
		return ".pptx"
	default:
		return ".txt"
	}
}

func (c *sharedAgentLoopCallbacks) executeTrustedAudioTranscribe(selection tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if reason := c.rejectGroupLocalAdmin(); reason != "" {
		return reason
	}
	if c == nil || c.semanticSurface == nil || c.semanticSurface.artifacts == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_audio_arguments_invalid"
	}
	if err := semanticTrustedAudioArgsAllowed(args); err != nil {
		return "[system rejected] " + err.Error()
	}
	contract := firstRequiredArtifactContract(selection.Consumes)
	payload, err := c.semanticSurface.artifacts.consumeTrustedInput(selection, contract)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	raw, err := decodeSemanticAttachmentBytes(payload.Base64)
	if err != nil || len(raw) == 0 {
		return "[system rejected] trusted_audio_payload_invalid"
	}
	text, err := c.handler.transcribeTrustedAudioBytes(payload.Ref.MIMEType, raw)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedAudioResultProjection(text)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	return out
}

func (c *sharedAgentLoopCallbacks) semanticPrincipalID() string {
	if c == nil {
		return ""
	}
	if c.semanticSurface != nil {
		if id := strings.TrimSpace(c.semanticSurface.scope.PrincipalID); id != "" {
			return id
		}
	}
	return strings.TrimSpace(c.userID)
}

func (c *sharedAgentLoopCallbacks) executeTrustedAuditRead(_ tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if reason := c.rejectGroupLocalAdmin(); reason != "" {
		return reason
	}
	if c == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_audit_arguments_invalid"
	}
	query, err := semanticTrustedAuditArgsAllowed(args)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	principalID := c.semanticPrincipalID()
	text, err := c.handler.readTrustedAudit(principalID, query)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedAuditResultProjection(text)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	return out
}

func (c *sharedAgentLoopCallbacks) executeTrustedKnowledgeAdmin(_ tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if reason := c.rejectGroupLocalAdmin(); reason != "" {
		return reason
	}
	if c == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_knowledge_admin_arguments_invalid"
	}
	id, status, refresh, hasRefresh, err := semanticTrustedKnowledgeAdminArgsAllowed(args)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	principalID := c.semanticPrincipalID()
	text, err := c.handler.administerTrustedKnowledge(principalID, id, status, refresh, hasRefresh)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedKnowledgeAdminResultProjection(text)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	return out
}

func (c *sharedAgentLoopCallbacks) executeTrustedKnowledgeIngest(_ tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if reason := c.rejectGroupLocalAdmin(); reason != "" {
		return reason
	}
	if c == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_knowledge_ingest_arguments_invalid"
	}
	text, url, path, err := semanticTrustedKnowledgeIngestArgsAllowed(args)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	result, err := c.handler.ingestTrustedKnowledge(c.semanticPrincipalID(), text, url, path)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedKnowledgeIngestResultProjection(result)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	return out
}

func (c *sharedAgentLoopCallbacks) executeTrustedKnowledgeRead(_ tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if reason := c.rejectGroupKnowledgeRead(); reason != "" {
		return reason
	}
	if c == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_knowledge_read_arguments_invalid"
	}
	query, err := semanticTrustedKnowledgeReadArgsAllowed(args)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	text, err := c.handler.readTrustedKnowledge(c.semanticPrincipalID(), query)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedKnowledgeReadResultProjection(text)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	return out
}

func (c *sharedAgentLoopCallbacks) executeTrustedFileWrite(_ tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if reason := c.rejectGroupLocalAdmin(); reason != "" {
		return reason
	}
	if c == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_file_write_arguments_invalid"
	}
	req, err := semanticTrustedFileWriteArgsAllowed(args)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	var result string
	if req.edit {
		result, err = c.handler.editTrustedFile(c.semanticPrincipalID(), req.path, req.oldString, req.newString)
	} else {
		result, err = c.handler.writeTrustedFile(c.semanticPrincipalID(), req.path, req.content, req.mode)
	}
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedFileWriteResultProjection(result)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	return out
}

func (c *sharedAgentLoopCallbacks) executeTrustedFileRead(_ tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if reason := c.rejectGroupFileRead(); reason != "" {
		return reason
	}
	if c == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_file_read_arguments_invalid"
	}
	path, query, filePattern, err := semanticTrustedFileReadArgsAllowed(args)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	result, err := c.handler.readTrustedFile(c.semanticPrincipalID(), path, query, filePattern)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedFileReadResultProjection(result)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	return out
}

func (c *sharedAgentLoopCallbacks) executeTrustedRepoInspect(_ tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if reason := c.rejectGroupLocalAdmin(); reason != "" {
		return reason
	}
	if c == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_repo_inspect_arguments_invalid"
	}
	if err := semanticTrustedRepoInspectArgsAllowed(args); err != nil {
		return "[system rejected] " + err.Error()
	}
	result, err := c.handler.inspectTrustedRepo(c.semanticPrincipalID())
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedRepoInspectResultProjection(result)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	return out
}

func (c *sharedAgentLoopCallbacks) executeTrustedWebFetch(_ tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if c == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	if reason := c.rejectGroupWeb(); reason != "" {
		return reason
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_web_fetch_arguments_invalid"
	}
	rawURL, err := semanticTrustedWebFetchArgsAllowed(args)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	publicNetworkOnly := c.loopCtx != nil && c.loopCtx.LansengerGroupPermissions != nil
	result, err := c.handler.fetchTrustedWeb(c.semanticPrincipalID(), rawURL, publicNetworkOnly)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedWebFetchResultProjection(result)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	return out
}

func (c *sharedAgentLoopCallbacks) executeTrustedWebSearch(_ tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if c == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	if reason := c.rejectGroupWeb(); reason != "" {
		return reason
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_web_search_arguments_invalid"
	}
	query, err := semanticTrustedWebSearchArgsAllowed(args)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	publicNetworkOnly := c.loopCtx != nil && c.loopCtx.LansengerGroupPermissions != nil
	result, err := c.handler.searchTrustedWeb(c.semanticPrincipalID(), query, publicNetworkOnly)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedWebSearchResultProjection(result)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	return out
}

func (c *sharedAgentLoopCallbacks) executeTrustedClock(_ tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if c == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_clock_arguments_invalid"
	}
	if err := semanticTrustedClockArgsAllowed(args); err != nil {
		return "[system rejected] " + err.Error()
	}
	result, err := c.handler.readTrustedClock(c.semanticPrincipalID())
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedClockResultProjection(result)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	return out
}

func (c *sharedAgentLoopCallbacks) executeTrustedConfig(_ tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if reason := c.rejectGroupLocalAdmin(); reason != "" {
		return reason
	}
	if c == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_config_arguments_invalid"
	}
	maxIterations, hasMax, thinkingMode, hasThinking, err := semanticTrustedConfigArgsAllowed(args)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	principalID := c.semanticPrincipalID()
	text, err := c.handler.administerTrustedConfig(principalID, maxIterations, hasMax, thinkingMode, hasThinking)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedConfigResultProjection(text)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	return out
}

func (c *sharedAgentLoopCallbacks) executeTrustedMemory(_ tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if reason := c.rejectGroupLocalAdmin(); reason != "" {
		return reason
	}
	if c == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_memory_arguments_invalid"
	}
	content, query, id, err := semanticTrustedMemoryArgsAllowed(args)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	text, err := c.handler.administerTrustedMemory(c.semanticPrincipalID(), content, query, id)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedMemoryResultProjection(text)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	return out
}

func (c *sharedAgentLoopCallbacks) executeTrustedMemoryRecall(_ tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if c == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_memory_recall_arguments_invalid"
	}
	if len(args) != 1 {
		return "[system rejected] trusted_memory_recall_arguments_rejected"
	}
	raw, ok := args["query"]
	if !ok {
		return "[system rejected] trusted_memory_recall_arguments_rejected"
	}
	query, ok := raw.(string)
	if !ok {
		return "[system rejected] trusted_memory_recall_arguments_rejected"
	}
	text, err := c.handler.recallTrustedMemory(c.semanticPrincipalID(), query)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedMemoryResultProjection(text)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	return out
}

func (c *sharedAgentLoopCallbacks) executeTrustedTask(_ tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if reason := c.rejectGroupLocalAdmin(); reason != "" {
		return reason
	}
	if c == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_task_arguments_invalid"
	}
	title, description, id, status, note, err := semanticTrustedTaskArgsAllowed(args)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	text, err := c.handler.administerTrustedTask(c.semanticPrincipalID(), title, description, id, status, note)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedTaskResultProjection(text)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	return out
}

func (c *sharedAgentLoopCallbacks) executeTrustedGoal(_ tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if reason := c.rejectGroupLocalAdmin(); reason != "" {
		return reason
	}
	if c == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_goal_arguments_invalid"
	}
	objective, status, note, err := semanticTrustedGoalArgsAllowed(args)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	text, err := c.handler.administerTrustedGoal(c.semanticPrincipalID(), objective, status, note)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedGoalResultProjection(text)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	return out
}

func (c *sharedAgentLoopCallbacks) executeTrustedTemplate(_ tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if reason := c.rejectGroupLocalAdmin(); reason != "" {
		return reason
	}
	if c == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_template_arguments_invalid"
	}
	name, codingTool, err := semanticTrustedTemplateArgsAllowed(args)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	text, err := c.handler.administerTrustedTemplate(c.semanticPrincipalID(), name, codingTool)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedTemplateResultProjection(text)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	return out
}

func (c *sharedAgentLoopCallbacks) executeTrustedSession(_ tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if reason := c.rejectGroupLocalAdmin(); reason != "" {
		return reason
	}
	if c == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_session_arguments_invalid"
	}
	id, err := semanticTrustedSessionArgsAllowed(args)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	text, err := c.handler.inspectTrustedSessions(c.semanticPrincipalID(), id)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedSessionResultProjection(text)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	return out
}

func (c *sharedAgentLoopCallbacks) executeTrustedSchedule(_ tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if reason := c.rejectGroupLocalAdmin(); reason != "" {
		return reason
	}
	if c == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_schedule_arguments_invalid"
	}
	parsed, err := semanticTrustedScheduleArgsAllowed(args)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	text, err := c.handler.administerTrustedSchedule(c.semanticPrincipalID(), parsed)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedScheduleResultProjection(text)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	return out
}

func (c *sharedAgentLoopCallbacks) executeTrustedOfficeWrite(_ tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if reason := c.rejectGroupLocalAdmin(); reason != "" {
		return reason
	}
	if c == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_office_write_arguments_invalid"
	}
	path, data, err := semanticTrustedOfficeWriteArgsAllowed(args)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	text, err := c.handler.writeTrustedOffice(c.semanticPrincipalID(), path, data)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedOfficeWriteResultProjection(text)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	return out
}

func (c *sharedAgentLoopCallbacks) executeTrustedAcquireRemote(_ tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if c == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	if reason := c.rejectGroupWeb(); reason != "" {
		return reason
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_artifact_acquire_arguments_invalid"
	}
	rawURL, err := semanticTrustedAcquireRemoteArgsAllowed(args)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	publicNetworkOnly := c.loopCtx != nil && c.loopCtx.LansengerGroupPermissions != nil
	text, err := c.handler.acquireTrustedRemote(c.semanticPrincipalID(), rawURL, publicNetworkOnly)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedAcquireRemoteResultProjection(text)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	return out
}

func (c *sharedAgentLoopCallbacks) executeTrustedShell(_ tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if reason := c.rejectGroupLocalAdmin(); reason != "" {
		return reason
	}
	if c == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_shell_arguments_invalid"
	}
	command, timeout, err := semanticTrustedShellArgsAllowed(args)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	text, err := c.handler.executeTrustedShell(c.semanticPrincipalID(), command, timeout)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedShellResultProjection(text)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	return out
}

func (c *sharedAgentLoopCallbacks) executeTrustedBuildVerify(_ tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if reason := c.rejectGroupLocalAdmin(); reason != "" {
		return reason
	}
	if c == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_build_verify_arguments_invalid"
	}
	task, target, err := semanticTrustedBuildVerifyArgsAllowed(args)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	text, err := c.handler.runTrustedBuildVerify(c.semanticPrincipalID(), task, target)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedBuildVerifyResultProjection(text)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	return out
}

func (c *sharedAgentLoopCallbacks) executeTrustedDelegate(_ tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if reason := c.rejectGroupLocalAdmin(); reason != "" {
		return reason
	}
	if c == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_delegate_arguments_invalid"
	}
	task, err := semanticTrustedDelegateArgsAllowed(args)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	text, err := c.handler.runTrustedDelegate(c.semanticPrincipalID(), task)
	if err != nil {
		if strings.Contains(err.Error(), "unavailable") {
			return "[system unknown] trusted_delegate_child_receipt_missing"
		}
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedDelegateResultProjection(text)
	if err != nil {
		return "[system unknown] " + err.Error()
	}
	return out
}

func (c *sharedAgentLoopCallbacks) executeTrustedSSH(_ tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if reason := c.rejectGroupLocalAdmin(); reason != "" {
		return reason
	}
	if c == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_ssh_arguments_invalid"
	}
	command, err := semanticTrustedSSHArgsAllowed(args)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	text, err := c.handler.executeTrustedSSH(c.semanticPrincipalID(), command)
	if err != nil {
		// The list stays deliberately wide: it still reports a session that
		// never carried the command as unknown, which is over-cautious rather
		// than unsafe. Dropping the unobserved name from it, however, would be
		// unsafe, since that is the one case where the command may have run.
		if strings.Contains(err.Error(), "unavailable") || strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "disconnect") || strings.Contains(err.Error(), "outcome_unobserved") {
			return "[system unknown] " + err.Error()
		}
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedSSHResultProjection(text)
	if err != nil {
		return "[system unknown] " + err.Error()
	}
	return out
}

func (c *sharedAgentLoopCallbacks) executeTrustedBrowser(_ tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if reason := c.rejectGroupLocalAdmin(); reason != "" {
		return reason
	}
	if c == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_browser_arguments_invalid"
	}
	action, url, err := semanticTrustedBrowserArgsAllowed(args)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	text, err := c.handler.controlTrustedBrowser(c.semanticPrincipalID(), action, url)
	if err != nil {
		// Only a dispatched request whose answer was lost may be reported as
		// unknown. A session that was never available or already dead did not
		// carry the request, and calling that unknown would claim an effect
		// might hold when none can.
		if strings.Contains(err.Error(), "unavailable") || strings.Contains(err.Error(), "outcome_unobserved") {
			return "[system unknown] " + err.Error()
		}
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedBrowserResultProjection(text)
	if err != nil {
		return "[system unknown] " + err.Error()
	}
	return out
}

func (c *sharedAgentLoopCallbacks) executeTrustedComputerUse(_ tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if reason := c.rejectGroupLocalAdmin(); reason != "" {
		return reason
	}
	if c == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_computer_use_arguments_invalid"
	}
	action, err := semanticTrustedComputerUseArgsAllowed(args)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	text, err := c.handler.controlTrustedDesktop(c.semanticPrincipalID(), action)
	if err != nil {
		if strings.Contains(err.Error(), "unavailable") {
			return "[system unknown] " + err.Error()
		}
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedComputerUseResultProjection(text)
	if err != nil {
		return "[system unknown] " + err.Error()
	}
	return out
}

func (c *sharedAgentLoopCallbacks) executeTrustedRepoMutate(_ tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) string {
	if reason := c.rejectGroupLocalAdmin(); reason != "" {
		return reason
	}
	if c == nil || c.handler == nil {
		return "[system rejected] semantic tool surface is unavailable"
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "[system rejected] canonical_repo_mutate_arguments_invalid"
	}
	action, message, err := semanticTrustedRepoMutateArgsAllowed(args)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	text, err := c.handler.mutateTrustedRepo(c.semanticPrincipalID(), action, message)
	if err != nil {
		if strings.Contains(err.Error(), "unknown") || strings.Contains(err.Error(), "receipt") {
			return "[system unknown] " + err.Error()
		}
		return "[system rejected] " + err.Error()
	}
	out, err := semanticTrustedRepoMutateResultProjection(text)
	if err != nil {
		return "[system rejected] " + err.Error()
	}
	return out
}

func semanticDocumentReadResultProjection(result string) string {
	lines := strings.Split(result, "\n")
	filtered := lines[:0]
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Native OfficeRead uses its legacy path-taking function in a resume
		// example. Neither that path nor the legacy function name is valid on
		// the semantic surface, so both must be removed from the projection.
		if strings.HasPrefix(trimmed, "# path:") || strings.HasPrefix(trimmed, "# continue:") {
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.Join(filtered, "\n")
}

func (c *sharedAgentLoopCallbacks) registerSemanticArtifacts(selectionID string) error {
	if c == nil || c.semanticSurface == nil || c.semanticSurface.artifacts == nil {
		return fmt.Errorf("semantic artifact surface unavailable")
	}
	for _, payload := range c.semanticSurface.pendingArtifacts[selectionID] {
		if err := c.semanticSurface.artifacts.registerPublished(payload.Ref); err != nil {
			return err
		}
	}
	delete(c.semanticSurface.pendingArtifacts, selectionID)
	return nil
}

// semanticCanonicalArguments is the migration boundary between opaque semantic
// adapters and legacy handlers. It closes the model input object before the
// legacy dispatcher can add trusted runtime-only metadata.
func (c *sharedAgentLoopCallbacks) semanticCanonicalArguments(selection tool.PlannedSelection, argsJSON string) (tool.CanonicalRequest, error) {
	if c == nil || c.semanticSurface == nil {
		return tool.CanonicalRequest{}, fmt.Errorf("semantic tool surface is unavailable")
	}
	schema, ok := c.semanticSurface.parameterSchemas[selection.AdapterName]
	if !ok {
		return tool.CanonicalRequest{}, fmt.Errorf("parameter_schema_missing")
	}
	if hostOwnedGenerateSelection(selection) {
		argsJSON = semanticGeneratePDFInvocationArgs(argsJSON)
	}
	return tool.CanonicalizeAuthorizedInvocationArguments(argsJSON, schema, selection.ParameterAuthorization)
}

// semanticGeneratePDFInvocationArgs keeps the closed schema fields when the
// model already supplied Markdown. content is the only required field; title
// and doc_type stay if they are strings. Decorative extras such as
// date/path/query must not fail-close a one-shot grant. A calendar date is
// folded into content; other typed or query-shaped values are dropped.
func semanticGeneratePDFInvocationArgs(argsJSON string) string {
	var parsed map[string]interface{}
	if json.Unmarshal([]byte(argsJSON), &parsed) != nil || parsed == nil {
		return argsJSON
	}
	content := semanticJSONString(parsed["content"])
	if content == "" {
		return argsJSON
	}
	// Title-only stubs must stay unwashed. Folding date into
	// "南京天气报告" would invent a two-line body and could pass a
	// later thickness check; path extras must also remain so schema
	// reject still fires if Intake is skipped.
	if semanticGeneratePDFLooksLikeTitleOnly(content) {
		return argsJSON
	}
	if date := semanticReportDateString(parsed["date"]); date != "" && !strings.Contains(content, date) {
		content = "日期：" + date + "\n\n" + content
	}
	out := map[string]string{"content": content}
	if title := semanticJSONString(parsed["title"]); title != "" {
		out["title"] = title
	}
	if docType := semanticJSONString(parsed["doc_type"]); docType != "" {
		out["doc_type"] = docType
	}
	body, err := json.Marshal(out)
	if err != nil {
		return argsJSON
	}
	return string(body)
}

func semanticJSONString(v interface{}) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func semanticReportDateString(v interface{}) string {
	s := semanticJSONString(v)
	if s == "" || len([]rune(s)) > 32 || !semanticReportDateRE.MatchString(s) {
		return ""
	}
	return s
}

func semanticGeneratePDFArgsTooThin(argsJSON string) bool {
	var parsed map[string]interface{}
	if json.Unmarshal([]byte(argsJSON), &parsed) != nil || parsed == nil {
		return false
	}
	content := semanticJSONString(parsed["content"])
	if semanticGeneratePDFLooksLikeTitleOnly(content) {
		return true
	}
	line, hasBody := semanticGeneratePDFTitleLine(content)
	title := semanticJSONString(parsed["title"])
	return !hasBody && title != "" && line != "" && strings.EqualFold(line, title) &&
		!semanticGeneratePDFHasReportBodySignal(line)
}

func semanticGeneratePDFTitleLine(content string) (string, bool) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", false
	}
	line := content
	hasBody := false
	if idx := strings.Index(content, "\n"); idx >= 0 {
		line = strings.TrimSpace(content[:idx])
		hasBody = strings.TrimSpace(content[idx+1:]) != ""
	}
	if strings.HasPrefix(line, "#") {
		line = strings.TrimSpace(strings.TrimLeft(line, "#"))
	}
	return line, hasBody
}

func semanticGeneratePDFLooksLikeTitleOnly(content string) bool {
	line, hasBody := semanticGeneratePDFTitleLine(content)
	if hasBody || line == "" || semanticGeneratePDFHasReportBodySignal(line) {
		return false
	}
	n := len([]rune(line))
	if n > 24 {
		return false
	}
	lower := strings.ToLower(line)
	return n <= 8 ||
		strings.HasSuffix(line, "报告") ||
		strings.HasSuffix(line, "纪要") ||
		strings.HasSuffix(line, "文档") ||
		strings.HasSuffix(lower, "report") ||
		strings.HasSuffix(lower, "pdf")
}

func semanticGeneratePDFHasReportBodySignal(content string) bool {
	return strings.ContainsAny(content, "。！？;；") ||
		strings.Contains(content, "℃") ||
		strings.Contains(content, "°") ||
		strings.ContainsAny(content, "0123456789")
}

func firstRequiredArtifactContract(contracts []tool.ArtifactContract) tool.ArtifactContract {
	for _, contract := range contracts {
		if contract.Required {
			return contract
		}
	}
	return tool.ArtifactContract{}
}

func (c *sharedAgentLoopCallbacks) executeLegacyBoundAdapter(name, argsJSON string) string {
	return c.executeToolWithoutSemanticSurface(name, argsJSON)
}

// finishSharedToolExecution emits the ACP end event for every post-execution
// return path. Keeping this centralized prevents artifact shortcuts (notably
// screenshots) from leaving ACP tool chips permanently in progress.
func (c *sharedAgentLoopCallbacks) finishSharedToolExecution(requestID, toolCallID, name, argsJSON, result string, ok bool, artifactPaths []string) string {
	trimmed := strings.TrimSpace(result)
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(trimmed, "[system rejected]") || strings.HasPrefix(lower, "error:") || strings.HasPrefix(lower, "[mcp error]") {
		ok = false
	}
	if !isACPProgrammingRequestID(requestID) {
		return result
	}
	paths := append([]string(nil), acpPathsFromToolArgs(name, argsJSON)...)
	paths = appendUniqueStrings(paths, artifactPaths...)
	emitACPToolEventForRequest(requestID, ACPToolEvent{
		Phase:      "end",
		ToolCallID: toolCallID,
		Name:       name,
		ArgsJSON:   argsJSON,
		Result:     result,
		OK:         ok,
		Kind:       acpToolKind(name),
		Paths:      paths,
		Title:      acpToolTitle(name, argsJSON),
	})
	return result
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
			Text:           sharedLoopUserFacingText(loopResult.Text),
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
			cb.semanticDurabilityBlocked = true
			log.Printf("[InFlightTask] shared record-audio finalization flush failed user=%q run=%q err=%v", userID, cb.checkpointRunID, err)
			// Mirror ask_user: pending recording UI is only valid after the paired
			// transcript has been committed. Otherwise a restart loses the UI state
			// but retains the earlier recovery marker.
			h.pendingRecordAudio.Delete(userID)
			pausePersistErr = err
		} else {
			cb.checkpointCommitted = false
			cb.hasPendingToolBatch = false
			// Match ask_user: release a held dependant only after the paired
			// interactive record state and recovery-marker transition are durable.
			cb.releaseSemanticDependantIssue()
		}
	}
	if pausePersistErr == nil && out.Response != nil {
		// The handler intentionally deferred this local state mutation while the
		// paired history was waiting for its atomic checkpoint transition.
		h.commitPendingRecordAudio(userID, req, pairedHistory, loopResult.WorkingState)
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
		content := strings.TrimSpace(sharedLoopUserFacingText(loopResult.Text))
		if content == "" {
			content = agent.FormatAskUserForDisplay(loopResult.AskUser)
		}
		recorder.RecordEarlyStopToolResult(tcID, "ask_user", content)
	}
	resp := baseResponse
	if resp == nil {
		resp = &IMAgentResponse{Text: sharedLoopUserFacingText(loopResult.Text), ResponseSource: imResponseSourceAskUser.String()}
	}
	if strings.TrimSpace(resp.Text) == "" {
		resp.Text = sharedLoopUserFacingText(loopResult.Text)
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
	if c != nil {
		setComputerUseTurnVision(c.llmCfg.SupportsVision)
		setComputerUseOwner(computerUseOwnerFromLoop(c.loopCtx, c.userID))
	}
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
	return attachPendingComputerUseModelImage(agent.ToolExecutionResult{Result: text, Outcome: outcome})
}

// ProjectToolResult implements agent.ToolResultProjector. It deliberately uses
// the same dual-view projector as the legacy GUI loop.
func (c *sharedAgentLoopCallbacks) ProjectToolResult(name string, result agent.ToolExecutionResult) string {
	sessionKey := ""
	contextTokens := 0
	if c != nil {
		sessionKey = c.userID
		contextTokens = c.llmCfg.EffectiveContextTokens()
		if c.handler != nil {
			sessionKey = c.handler.workflowPolicyOwnerID(c.userID, c.loopCtx)
		}
	}
	proj, err := agent.ProjectToolResultWithContext(name, sessionKey, result.Result, contextTokens)
	if err != nil && proj.Preview == "" {
		return truncateToolResultForToolWithSession(name, sessionKey, result.Result)
	}
	return proj.Preview
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
	if c != nil && len(delta) > 0 {
		c.semanticHoldDependantIssue = true
	}
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
		c.semanticDurabilityBlocked = true
		log.Printf("[InFlightTask] shared pre-tool checkpoint failed user=%q run=%q seq=%d err=%v", c.userID, c.checkpointRunID, meta.Sequence, err)
		return err
	}
	c.checkpointCommitted = true
	c.hasPendingToolBatch = true
	return nil
}

// OnToolBatchAbandoned records that execution stopped before this batch was
// durably committed. It deliberately does not release a held semantic
// dependant: cancellation, validation failure and hard stops retain the
// recoverable pre-tool checkpoint, while interactive pauses release only after
// their paired history and marker transition have committed in the host.
func (c *sharedAgentLoopCallbacks) OnToolBatchAbandoned(meta agent.ToolBatchMetadata) {
	_ = meta
}

// OnToolBatchCommitted implements agent.ToolBatchCommitter. RunLoop invokes it
// after the entire assistant tool-call batch and every paired tool result have
// been appended to HistoryDelta. A persistence error is returned to RunLoop so
// it stops before executing a following batch with further side effects.
func (c *sharedAgentLoopCallbacks) OnToolBatchCommitted(delta []agent.ConversationEntry, meta agent.ToolBatchMetadata) error {
	if c == nil || c.handler == nil || len(delta) == 0 {
		// No persisted complete batch exists on this path, so it cannot publish
		// authority derived from the batch. The optional callback remains a no-op
		// for compatibility, but semantic dependant issuance stays fail-closed.
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
		c.semanticDurabilityBlocked = true
		log.Printf("[InFlightTask] shared checkpoint failed user=%q run=%q seq=%d err=%v", c.userID, c.checkpointRunID, meta.Sequence, err)
		return err
	}
	c.checkpointCommitted = true
	c.hasPendingToolBatch = false
	// A dependant becomes model-visible only after the complete paired batch is
	// durably checkpointed. Releasing before persistRecoveryCheckpoint succeeds
	// would expose authority derived from a batch that recovery still treats as
	// externally uncertain.
	c.releaseSemanticDependantIssue()
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
	// A governed light lookup must stay on its grant-bound surface and light
	// fence. Promoting to the reasoning model after web_search caused the
	// full-agent prompt to be swapped in, and the model then asked the user
	// to re-authorize write_file/web_fetch that this turn cannot run.
	if c.semanticLightLookup() {
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
	if upgraded.Model == "" || (upgraded.Model == before &&
		upgraded.URL == c.llmCfg.URL &&
		upgraded.Key == c.llmCfg.Key &&
		upgraded.Protocol == c.llmCfg.Protocol &&
		upgraded.ProviderName == c.llmCfg.ProviderName &&
		upgraded.ContextLength == c.llmCfg.ContextLength) {
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
	c.surfaceRefreshPending = true
	if c.handler.app != nil {
		c.handler.app.recordLastModelRoute(c.route)
	}
	log.Printf("[model-route] shared escalate %s→%s", before, upgraded.Model)
}

// RefreshAfterToolExecution records that the next request must observe updated
// policy/grant state. It may update the non-executable system prompt, but never
// renders definitions itself: BuildToolsForModelRequest is the only legacy
// renderer and runs at a real request boundary.
func (c *sharedAgentLoopCallbacks) RefreshAfterToolExecution(name string) bool {
	_ = name
	if c == nil {
		return false
	}
	if c.semanticSurface != nil {
		pending := c.surfaceRefreshPending
		c.surfaceRefreshPending = false
		// A semantic family owns its current exposure closure. Escalation may
		// refresh the prompt; light lookup must keep its fence. Either way the
		// loop must re-read BuildTools so consumed grants disappear.
		if pending && c.handler != nil && !c.semanticLightLookup() {
			intentText := semanticUserIntentText(c.userText)
			c.systemPrompt = ensureSemanticGrantPromptFence(c.systemPromptWithTaskAnchor(c.handler.buildSystemPromptWithMemory(agent.CompactQueryForEmbedding(intentText), false, c.loopCtx)))
		}
		return true
	}
	if !c.surfaceRefreshPending || c.handler == nil {
		return false
	}
	c.surfaceRefreshPending = false
	c.systemPrompt = c.systemPromptWithTaskAnchor(c.handler.buildSystemPromptWithMemory(agent.CompactQueryForEmbedding(semanticUserIntentText(c.userText)), false, c.loopCtx))
	return true
}

func (c *sharedAgentLoopCallbacks) systemPromptWithTaskAnchor(systemPrompt string) string {
	if c == nil {
		return systemPrompt
	}
	return appendTaskIdentityAnchorPrompt(systemPrompt, c.taskAnchor)
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

func rewriteExpiredSemanticGrantNames(history []agent.ConversationEntry, live map[string]bool) []agent.ConversationEntry {
	if len(history) == 0 || !historyHasExpiredSemanticGrantName(history, live) {
		return history
	}
	out := make([]agent.ConversationEntry, len(history))
	copy(out, history)
	for i := range out {
		if rewritten := rewriteExpiredSemanticGrantName(out[i].ToolName, live); rewritten != out[i].ToolName {
			out[i].ToolName = rewritten
		}
		if out[i].ToolCalls != nil {
			out[i].ToolCalls = rewriteExpiredSemanticGrantToolCalls(out[i].ToolCalls, live)
		}
		out[i].Content = rewriteExpiredSemanticGrantContent(out[i].Content, live)
	}
	return out
}

func historyHasExpiredSemanticGrantName(history []agent.ConversationEntry, live map[string]bool) bool {
	for _, entry := range history {
		if expiredSemanticGrantName(entry.ToolName, live) {
			return true
		}
		if historyToolCallsHaveExpiredGrantName(entry.ToolCalls, live) {
			return true
		}
		asMap, ok := entry.Content.(map[string]interface{})
		if ok {
			if name, ok := asMap["name"].(string); ok && expiredSemanticGrantName(name, live) {
				return true
			}
		}
	}
	return false
}

func expiredSemanticGrantName(name string, live map[string]bool) bool {
	name = strings.TrimSpace(name)
	if name != previousTurnSemanticToolName {
		return false
	}
	return live != nil && live["web_search"]
}

func historyToolCallsHaveExpiredGrantName(calls interface{}, live map[string]bool) bool {
	switch typed := calls.(type) {
	case []llm.ToolCall:
		for _, call := range typed {
			if expiredSemanticGrantName(call.Function.Name, live) {
				return true
			}
		}
	case []map[string]interface{}:
		for _, call := range typed {
			if expiredSemanticGrantName(semanticGrantNameFromCallMap(call), live) {
				return true
			}
		}
	case []interface{}:
		for _, call := range typed {
			switch typedCall := call.(type) {
			case llm.ToolCall:
				if expiredSemanticGrantName(typedCall.Function.Name, live) {
					return true
				}
			case map[string]interface{}:
				if expiredSemanticGrantName(semanticGrantNameFromCallMap(typedCall), live) {
					return true
				}
			}
		}
	}
	return false
}

func semanticGrantNameFromCallMap(call map[string]interface{}) string {
	if call == nil {
		return ""
	}
	if name := semanticMapStringField(call["function"], "name"); name != "" {
		return name
	}
	if name, ok := call["name"].(string); ok {
		return name
	}
	return ""
}

func semanticMapStringField(value interface{}, key string) string {
	switch typed := value.(type) {
	case map[string]interface{}:
		name, _ := typed[key].(string)
		return name
	case map[string]string:
		return typed[key]
	default:
		return ""
	}
}

func rewriteExpiredSemanticGrantName(name string, live map[string]bool) string {
	name = strings.TrimSpace(name)
	if name != previousTurnSemanticToolName {
		return name
	}
	if live != nil && live["web_search"] {
		return "web_search"
	}
	return name
}

func rewriteExpiredSemanticGrantToolCalls(calls interface{}, live map[string]bool) interface{} {
	switch typed := calls.(type) {
	case []llm.ToolCall:
		out := append([]llm.ToolCall(nil), typed...)
		for i := range out {
			out[i].Function.Name = rewriteExpiredSemanticGrantName(out[i].Function.Name, live)
		}
		return out
	case []map[string]interface{}:
		out := make([]map[string]interface{}, len(typed))
		for i, call := range typed {
			out[i] = rewriteExpiredSemanticGrantCallMap(call, live)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i, call := range typed {
			switch typedCall := call.(type) {
			case map[string]interface{}:
				out[i] = rewriteExpiredSemanticGrantCallMap(typedCall, live)
			case llm.ToolCall:
				typedCall.Function.Name = rewriteExpiredSemanticGrantName(typedCall.Function.Name, live)
				out[i] = typedCall
			default:
				out[i] = call
			}
		}
		return out
	default:
		return calls
	}
}

func rewriteExpiredSemanticGrantCallMap(call map[string]interface{}, live map[string]bool) map[string]interface{} {
	if call == nil {
		return nil
	}
	out := make(map[string]interface{}, len(call)+1)
	for key, value := range call {
		out[key] = value
	}
	switch fn := out["function"].(type) {
	case map[string]interface{}:
		cloned := make(map[string]interface{}, len(fn)+1)
		for key, value := range fn {
			cloned[key] = value
		}
		if name, ok := cloned["name"].(string); ok {
			cloned["name"] = rewriteExpiredSemanticGrantName(name, live)
		}
		out["function"] = cloned
	case map[string]string:
		cloned := make(map[string]string, len(fn)+1)
		for key, value := range fn {
			cloned[key] = value
		}
		if name, ok := cloned["name"]; ok {
			cloned["name"] = rewriteExpiredSemanticGrantName(name, live)
		}
		out["function"] = cloned
	}
	if name, ok := out["name"].(string); ok {
		out["name"] = rewriteExpiredSemanticGrantName(name, live)
	}
	return out
}

func rewriteExpiredSemanticGrantContent(content interface{}, live map[string]bool) interface{} {
	asMap, ok := content.(map[string]interface{})
	if !ok || asMap == nil {
		return content
	}
	name, ok := asMap["name"].(string)
	if !ok {
		return content
	}
	rewritten := rewriteExpiredSemanticGrantName(name, live)
	if rewritten == name {
		return content
	}
	out := make(map[string]interface{}, len(asMap)+1)
	for key, value := range asMap {
		out[key] = value
	}
	out["name"] = rewritten
	return out
}
