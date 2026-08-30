package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func (h *IMMessageHandler) runAgentLoop(ctx *LoopContext, userID, systemPrompt string, history []agent.ConversationEntry, userText string, attachments []MessageAttachment, onProgress tool.ProgressCallback, onToken llm.TokenCallback, onNewRound NewRoundCallback, onStreamDone StreamDoneCallback, minIterations int, platform string) (result *IMAgentResponse) {
	// Phase 6: daily LLM budget hard-stop (fleet-aware) before any model call.
	if blocked, msg := h.checkDailyBudgetGate(); blocked {
		reqID := ""
		if ctx != nil {
			reqID = ctx.Runtime.RequestID
		}
		return &IMAgentResponse{
			Text:           msg,
			Error:          "daily_llm_budget_exceeded",
			RequestID:      reqID,
			ResponseSource: "budget_gate",
		}
	}
	if ctx != nil && ctx.IsCancelled() {
		return h.cancelledExitResponse(userID, history, userText)
	}
	// Publish the runtime before choosing the shared/legacy implementation so a
	// busy-turn steer has one exact consumer regardless of which loop path wins.
	// Earlier pre-loop phases may still short-circuit and therefore must not
	// advertise steering acceptance.
	defer h.beginAgentLoopRuntime(ctx, userID, userText, platform)()

	// A capability-managed turn has a grant-bound executor only on the shared
	// loop.  It must not be controlled by the legacy/shared strangler flag:
	// selecting legacy here would discard SemanticSurface and re-open the
	// keyword/name router.  The shared eligibility gate remains relevant to
	// ungoverned capability families during their migration.
	semanticManaged := ctx != nil && ctx.Runtime.SemanticIntent != nil && imSemanticIntentIsManagedForLoop(ctx.WorkflowAgentLoop, *ctx.Runtime.SemanticIntent)
	// A withdrawn family must not claim the strangler bypass it is being
	// withdrawn from. The turn is refused either way by the planning gate, so
	// this only keeps the loop choice and its log honest.
	if semanticManaged {
		if _, withdrawn := semanticWithdrawnCapabilityLabel(h, userID, *ctx.Runtime.SemanticIntent); withdrawn {
			semanticManaged = false
		}
	}
	useSharedLoop := false
	if !semanticManaged {
		useSharedLoop = h.shouldUseSharedAgentLoop(ctx, userID, attachments)
	}
	if semanticManaged || useSharedLoop {
		if semanticManaged && !useSharedLoop {
			log.Printf("[agent-loop] semantic managed turn bypasses legacy strangler owner=%q request_id=%q loop=%q", userID, ctx.Runtime.RequestID, ctx.ID)
		}
		return h.runAgentLoopShared(ctx, userID, systemPrompt, history, userText, attachments, onProgress, onToken, onNewRound, onStreamDone, minIterations, platform)
	}

	recordLegacyAgentLoopTurn()
	startedAt := time.Now()
	requestID := ""
	loopID := ""
	if ctx != nil {
		requestID = ctx.Runtime.RequestID
		loopID = ctx.ID
	}
	log.Printf("[agent-loop] start owner=%q request_id=%q loop=%q platform=%q project=%q text_len=%d path=legacy", userID, requestID, loopID, platform, projectPathFromUserID(userID), len([]rune(userText)))
	// Captured by the exit defer after prepare/runState are created. Closures see
	// final values; outcome+Flush run AFTER recover so panic turns are not stamped success.
	var (
		trajRecorder  *TrajectoryRecorder
		trajCleanup   func()
		trajRunState  *agentLoopRunState
		trajTelemetry *agentLoopTelemetry
	)
	defer func() {
		if r := recover(); r != nil {
			result = &IMAgentResponse{Error: fmt.Sprintf("Agent loop panicked: %v", r)}
			log.Printf("[agent-loop] panic owner=%q request_id=%q loop=%q panic=%v elapsed=%s", userID, requestID, loopID, r, time.Since(startedAt).Round(time.Millisecond))
		}
		status, _ := classifyIMAgentResponseOutcome(result)
		if result != nil {
			if trajRunState != nil {
				result.ToolCallsInTurn = trajRunState.TotalToolCallsInLoop
			}
			if result.RequestID == "" {
				result.RequestID = requestID
			}
			if result.ResponseSource == "" {
				result.ResponseSource = "legacy_agent_loop"
			}
		}
		log.Printf("[agent-loop] end owner=%q request_id=%q loop=%q status=%s elapsed=%s path=legacy", userID, requestID, loopID, status, time.Since(startedAt).Round(time.Millisecond))
		imPerfLog("agent_loop", startedAt, requestID, userID, "status", status, "loop", loopID, "platform", platform, "text_len", len([]rune(userText)), "prompt_len", len(systemPrompt), "history_len", len(history), "path", "legacy")
		if trajRecorder != nil {
			// LoopContext.Iteration is 0-based (set at the start of each round).
			// Trajectory outcome uses a 1-based completed-round count, matching
			// corelib agent.LoopResult.Iterations (iteration+1).
			iters := 0
			if ctx != nil {
				iters = ctx.Iteration() + 1
			}
			tools := 0
			if trajRunState != nil {
				tools = trajRunState.TotalToolCallsInLoop
			}
			// Terminal safety net: cancel / panic / hard_exit / error leave no orphan tools.
			// Success and interactive pause already pair (or CloseUnpaired) in-loop.
			if reason := unpairedCloseReasonFromIMResponse(result); reason != "" {
				trajRecorder.CloseUnpairedToolCalls(reason)
			}
			trajRecorder.SetOutcomeFromIMResponse(result, trajTelemetry, iters, tools)
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
	attachLLMTelemetry := telemetry.Attach
	firstRequestMetrics := telemetry.FirstRequestMetrics
	runtimeState := h.beginAgentLoopRuntimeState(ctx, userID, userText, onProgress, onStreamDone, telemetry)
	defer runtimeState.Cleanup()
	loopCtx := runtimeState.RequestContext
	loopGoalAnchor := runtimeState.GoalAnchor
	loopDriftDetector := runtimeState.DriftDetector
	loopProgressTracker := runtimeState.ProgressTracker
	loopAdaptiveRetry := runtimeState.AdaptiveRetry
	priorReplanCount := runtimeState.PriorReplanCount
	sendProgress := runtimeState.SendProgress
	streamDoneCallback := runtimeState.StreamDoneCallback
	isDebug := runtimeState.IsDebug
	milestoneTracker := runtimeState.MilestoneTracker

	startState := h.prepareAgentLoopStartState(agentLoopStartOptions{
		Context:          ctx,
		UserID:           userID,
		UserText:         userText,
		SystemPrompt:     systemPrompt,
		Platform:         platform,
		Attachments:      attachments,
		History:          history,
		MinIterations:    minIterations,
		PriorReplanCount: priorReplanCount,
		AdaptiveRetry:    loopAdaptiveRetry,
		MilestoneTracker: milestoneTracker,
		Telemetry:        telemetry,
		SendProgress:     sendProgress,
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
	trialState := startState.TrialState
	maxIter := startState.MaxIterations
	phase := startState.Phase
	baseTools := startState.BaseTools
	tools := startState.Tools
	clientToolNames := startState.ClientToolNames
	toolsTokenBudget := startState.ToolsTokenBudget
	httpClient := startState.HTTPClient
	recorder := startState.Recorder
	loopAdaptiveRetry = startState.AdaptiveRetry
	visibleArtifacts := startState.VisibleArtifacts
	attachPendingVisibleArtifacts := startState.AttachPendingVisibleArtifacts
	recordSystemMessages := startState.RecordSystemMessages
	recordToolCall := startState.RecordToolCall
	recordToolResult := startState.RecordToolResult
	reportActivity := startState.ReportActivity
	conversation := startState.Conversation
	history = startState.History
	conversationStartedAt := startState.ConversationStartedAt
	effectiveMax := startState.EffectiveMax
	chatFinalizeGrace := startState.ChatFinalizeGrace

	runState := newAgentLoopRunState(cfg)
	trajRunState = runState
	runState.Telemetry = telemetry
	runState.applyRouteDecision(startState.RouteDecision, cfg)
	telemetry.Route = startState.RouteDecision

	inFlightLifecycle := h.newInFlightLifecycle(userID, userText)
	inFlightLifecycle.loopID = loopID
	defer inFlightLifecycle.Cleanup()
	defer h.persistCompressionSummaryOnExit(userID, &runState.LastCompressionSummary)

	mainIterations := h.runAgentLoopMainIterations(agentLoopMainIterationsOptions{
		Context:                       ctx,
		RequestContext:                loopCtx,
		UserID:                        userID,
		UserText:                      userText,
		TaskAnchor:                    startState.TaskAnchor,
		Platform:                      platform,
		Config:                        cfg,
		HTTPClient:                    httpClient,
		BaseTools:                     baseTools,
		Tools:                         tools,
		ClientToolNames:               clientToolNames,
		ToolsTokenBudget:              toolsTokenBudget,
		Conversation:                  conversation,
		History:                       history,
		EffectiveMax:                  effectiveMax,
		MinIterations:                 minIterations,
		ConfigMax:                     maxIter,
		ChatFinalizeGrace:             chatFinalizeGrace,
		Phase:                         &phase,
		GoalAnchor:                    loopGoalAnchor,
		ProgressTracker:               loopProgressTracker,
		TrialState:                    trialState,
		DriftDetector:                 loopDriftDetector,
		MilestoneTracker:              milestoneTracker,
		Telemetry:                     telemetry,
		RuntimeState:                  runtimeState,
		RunState:                      runState,
		AdaptiveRetry:                 loopAdaptiveRetry,
		FirstRequestMetrics:           firstRequestMetrics,
		InFlightLifecycle:             inFlightLifecycle,
		Recorder:                      recorder,
		VisibleArtifacts:              visibleArtifacts,
		OnToken:                       onToken,
		OnProgress:                    onProgress,
		OnNewRound:                    onNewRound,
		StreamDoneCallback:            streamDoneCallback,
		ReportActivity:                reportActivity,
		RecordToolCall:                recordToolCall,
		RecordToolResult:              recordToolResult,
		RecordSystemMessages:          recordSystemMessages,
		AttachLLMTelemetry:            attachLLMTelemetry,
		AttachPendingVisibleArtifacts: attachPendingVisibleArtifacts,
	})
	conversation = mainIterations.Conversation
	history = mainIterations.History
	tools = mainIterations.Tools
	toolsTokenBudget = mainIterations.ToolsTokenBudget
	effectiveMax = mainIterations.EffectiveMax
	if mainIterations.Response != nil {
		return mainIterations.Response
	}

	if ctx.IsCancelled() {
		return h.cancelledExitResponse(userID, history, userText)
	}

	return h.finishAgentLoopAndRecordTelemetry(agentLoopCompletionOptions{
		Context:        ctx,
		RequestContext: loopCtx,
		UserID:         userID,
		UserText:       userText,
		// Main iterations may have escalated from a light route. The bonus round
		// must inherit that active snapshot, including its context budget.
		Config:                 completionConfigFromRunState(runState, cfg),
		Conversation:           conversation,
		History:                history,
		Tools:                  tools,
		HTTPClient:             httpClient,
		EffectiveTokenLimit:    runState.EffectiveTokenLimit,
		ToolsTokenBudget:       toolsTokenBudget,
		OnToken:                onToken,
		OnProgress:             onProgress,
		OnNewRound:             onNewRound,
		StreamDoneCallback:     streamDoneCallback,
		FirstRequestMetrics:    firstRequestMetrics,
		StreamDone:             telemetry.StreamDone(),
		Phase:                  &phase,
		MilestoneTracker:       milestoneTracker,
		Recorder:               recorder,
		InFlightLifecycle:      inFlightLifecycle,
		RecordToolCall:         recordToolCall,
		RecordToolResult:       recordToolResult,
		AttachLLMTelemetry:     attachLLMTelemetry,
		AttachVisibleArtifacts: attachPendingVisibleArtifacts,
		SendProgress:           sendProgress,
		Debug:                  isDebug(),
		LastInputTokens:        telemetry.LastLLMInputTokens,
		LastOutputTokens:       telemetry.LastLLMOutputTokens,
		LastCacheReadTokens:    telemetry.LastLLMCacheReadTokens,
		LastCacheWriteTokens:   telemetry.LastLLMCacheWriteTokens,
		TotalToolCallsInLoop:   runState.TotalToolCallsInLoop,
		EffectiveMax:           effectiveMax,
		ConfigMax:              maxIter,
		LoopMaxOverride:        h.loopMaxOverride,
		ChatFinalizeGrace:      chatFinalizeGrace,
		ConversationStartedAt:  conversationStartedAt,
	}, telemetry)
}

func completionConfigFromRunState(run *agentLoopRunState, fallback corelib.MaclawLLMConfig) corelib.MaclawLLMConfig {
	if run == nil || strings.TrimSpace(run.ActiveConfig.Model) == "" {
		return fallback
	}
	return run.ActiveConfig
}
