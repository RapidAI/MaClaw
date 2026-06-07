package main

import (
	"fmt"
	"log"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func (h *IMMessageHandler) runAgentLoop(ctx *LoopContext, userID, systemPrompt string, history []agent.ConversationEntry, userText string, attachments []MessageAttachment, onProgress tool.ProgressCallback, onToken llm.TokenCallback, onNewRound NewRoundCallback, onStreamDone StreamDoneCallback, minIterations int, platform string) (result *IMAgentResponse) {
	startedAt := time.Now()
	requestID := ""
	loopID := ""
	if ctx != nil {
		requestID = ctx.Runtime.RequestID
		loopID = ctx.ID
	}
	log.Printf("[agent-loop] start owner=%q request_id=%q loop=%q platform=%q project=%q text_len=%d", userID, requestID, loopID, platform, projectPathFromUserID(userID), len([]rune(userText)))
	defer func() {
		status := "success"
		if result != nil && result.Error != "" {
			status = "error"
		}
		if result != nil && result.RequestID == "" {
			result.RequestID = requestID
		}
		log.Printf("[agent-loop] end owner=%q request_id=%q loop=%q status=%s elapsed=%s", userID, requestID, loopID, status, time.Since(startedAt).Round(time.Millisecond))
		imPerfLog("agent_loop", startedAt, requestID, userID, "status", status, "loop", loopID, "platform", platform, "text_len", len([]rune(userText)), "prompt_len", len(systemPrompt), "history_len", len(history))
		if r := recover(); r != nil {
			result = &IMAgentResponse{Error: fmt.Sprintf("Agent loop panicked: %v", r)}
			log.Printf("[agent-loop] panic owner=%q request_id=%q loop=%q panic=%v elapsed=%s", userID, requestID, loopID, r, time.Since(startedAt).Round(time.Millisecond))
		}
	}()

	telemetry := newAgentLoopTelemetry()
	attachLLMTelemetry := telemetry.Attach
	firstRequestMetrics := telemetry.FirstRequestMetrics
	defer h.beginAgentLoopRuntime(ctx, userID, userText, platform)()

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
	})
	defer startState.Cleanup()
	cfg := startState.Config
	trialState := startState.TrialState
	maxIter := startState.MaxIterations
	phase := startState.Phase
	baseTools := startState.BaseTools
	tools := startState.Tools
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
	gateConfig := startState.GateConfig
	skipCodingGate := startState.SkipCodingGate
	orchestratorActive := startState.OrchestratorActive
	steeringDetector := startState.SteeringDetector

	runState := newAgentLoopRunState(cfg)

	inFlightLifecycle := h.newInFlightLifecycle(userID, userText)
	inFlightLifecycle.loopID = loopID
	defer inFlightLifecycle.Cleanup()
	defer h.persistCompressionSummaryOnExit(userID, &runState.LastCompressionSummary)

	mainIterations := h.runAgentLoopMainIterations(agentLoopMainIterationsOptions{
		Context:                       ctx,
		RequestContext:                loopCtx,
		UserID:                        userID,
		UserText:                      userText,
		Platform:                      platform,
		Config:                        cfg,
		HTTPClient:                    httpClient,
		BaseTools:                     baseTools,
		Tools:                         tools,
		ToolsTokenBudget:              toolsTokenBudget,
		Conversation:                  conversation,
		History:                       history,
		EffectiveMax:                  effectiveMax,
		MinIterations:                 minIterations,
		ConfigMax:                     maxIter,
		ChatFinalizeGrace:             chatFinalizeGrace,
		GateConfig:                    gateConfig,
		SkipCodingGate:                skipCodingGate,
		OrchestratorActive:            orchestratorActive,
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
		SteeringDetector:              steeringDetector,
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
		Context:                ctx,
		RequestContext:         loopCtx,
		UserID:                 userID,
		UserText:               userText,
		Config:                 cfg,
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
