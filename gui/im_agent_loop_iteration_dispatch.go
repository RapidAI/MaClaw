package main

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/progress"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

type agentLoopIterationDispatchOptions struct {
	Context                       *LoopContext
	RequestContext                context.Context
	UserID                        string
	UserText                      string
	Iteration                     int
	Platform                      string
	Config                        corelib.MaclawLLMConfig
	HTTPClient                    *http.Client
	BaseTools                     []map[string]interface{}
	Tools                         *[]map[string]interface{}
	ToolsTokenBudget              *int
	Conversation                  *[]interface{}
	History                       *[]agent.ConversationEntry
	EffectiveMax                  *int
	MinIterations                 int
	ConfigMax                     int
	ChatFinalizeGrace             int
	Phase                         *agentLoopPhase
	GoalAnchor                    *GoalAnchor
	ProgressTracker               *HarnessProgressTracker
	TrialState                    *trialReflectState
	DriftDetector                 *DriftDetector
	MilestoneTracker              *progress.AgentProgressTracker
	Telemetry                     *agentLoopTelemetry
	RuntimeState                  agentLoopRuntimeState
	RunState                      *agentLoopRunState
	AdaptiveRetry                 *AdaptiveRetry
	FirstRequestMetrics           *llmFirstRequestMetrics
	InFlightLifecycle             *imInFlightLifecycle
	Recorder                      *TrajectoryRecorder
	VisibleArtifacts              *pendingVisibleArtifacts
	OnToken                       llm.TokenCallback
	OnProgress                    tool.ProgressCallback
	OnNewRound                    NewRoundCallback
	StreamDoneCallback            StreamDoneCallback
	ReportActivity                func(int, int, string)
	RecordToolCall                func(string, string, string)
	RecordToolResult              func(string, interface{}, string, string)
	RecordSystemMessages          func(int, []interface{})
	AttachLLMTelemetry            func(*IMAgentResponse)
	AttachPendingVisibleArtifacts func(*IMAgentResponse)
}

type agentLoopIterationDispatchResult struct {
	Response *IMAgentResponse
	Break    bool
	Continue bool
}

type agentLoopMainIterationsOptions struct {
	Context                       *LoopContext
	RequestContext                context.Context
	UserID                        string
	UserText                      string
	Platform                      string
	Config                        corelib.MaclawLLMConfig
	HTTPClient                    *http.Client
	BaseTools                     []map[string]interface{}
	Tools                         []map[string]interface{}
	ToolsTokenBudget              int
	Conversation                  []interface{}
	History                       []agent.ConversationEntry
	EffectiveMax                  int
	MinIterations                 int
	ConfigMax                     int
	ChatFinalizeGrace             int
	Phase                         *agentLoopPhase
	GoalAnchor                    *GoalAnchor
	ProgressTracker               *HarnessProgressTracker
	TrialState                    *trialReflectState
	DriftDetector                 *DriftDetector
	MilestoneTracker              *progress.AgentProgressTracker
	Telemetry                     *agentLoopTelemetry
	RuntimeState                  agentLoopRuntimeState
	RunState                      *agentLoopRunState
	AdaptiveRetry                 *AdaptiveRetry
	FirstRequestMetrics           *llmFirstRequestMetrics
	InFlightLifecycle             *imInFlightLifecycle
	Recorder                      *TrajectoryRecorder
	VisibleArtifacts              *pendingVisibleArtifacts
	OnToken                       llm.TokenCallback
	OnProgress                    tool.ProgressCallback
	OnNewRound                    NewRoundCallback
	StreamDoneCallback            StreamDoneCallback
	ReportActivity                func(int, int, string)
	RecordToolCall                func(string, string, string)
	RecordToolResult              func(string, interface{}, string, string)
	RecordSystemMessages          func(int, []interface{})
	AttachLLMTelemetry            func(*IMAgentResponse)
	AttachPendingVisibleArtifacts func(*IMAgentResponse)
}

type agentLoopMainIterationsResult struct {
	Response         *IMAgentResponse
	Conversation     []interface{}
	History          []agent.ConversationEntry
	Tools            []map[string]interface{}
	ToolsTokenBudget int
	EffectiveMax     int
}

func (h *IMMessageHandler) runAgentLoopMainIterations(opts agentLoopMainIterationsOptions) agentLoopMainIterationsResult {
	result := agentLoopMainIterationsResult{
		Conversation:     opts.Conversation,
		History:          opts.History,
		Tools:            opts.Tools,
		ToolsTokenBudget: opts.ToolsTokenBudget,
		EffectiveMax:     opts.EffectiveMax,
	}
	for iteration := 0; ; iteration++ {
		opts.Context.SetIteration(iteration)
		if opts.Recorder != nil {
			opts.Recorder.SetCurrentIteration(iteration)
		}
		iterationResult := h.runAgentLoopIteration(agentLoopIterationDispatchOptions{
			Context:                       opts.Context,
			RequestContext:                opts.RequestContext,
			UserID:                        opts.UserID,
			UserText:                      opts.UserText,
			Iteration:                     iteration,
			Platform:                      opts.Platform,
			Config:                        opts.Config,
			HTTPClient:                    opts.HTTPClient,
			BaseTools:                     opts.BaseTools,
			Tools:                         &result.Tools,
			ToolsTokenBudget:              &result.ToolsTokenBudget,
			Conversation:                  &result.Conversation,
			History:                       &result.History,
			EffectiveMax:                  &result.EffectiveMax,
			MinIterations:                 opts.MinIterations,
			ConfigMax:                     opts.ConfigMax,
			ChatFinalizeGrace:             opts.ChatFinalizeGrace,
			Phase:                         opts.Phase,
			GoalAnchor:                    opts.GoalAnchor,
			ProgressTracker:               opts.ProgressTracker,
			TrialState:                    opts.TrialState,
			DriftDetector:                 opts.DriftDetector,
			MilestoneTracker:              opts.MilestoneTracker,
			Telemetry:                     opts.Telemetry,
			RuntimeState:                  opts.RuntimeState,
			RunState:                      opts.RunState,
			AdaptiveRetry:                 opts.AdaptiveRetry,
			FirstRequestMetrics:           opts.FirstRequestMetrics,
			InFlightLifecycle:             opts.InFlightLifecycle,
			Recorder:                      opts.Recorder,
			VisibleArtifacts:              opts.VisibleArtifacts,
			OnToken:                       opts.OnToken,
			OnProgress:                    opts.OnProgress,
			OnNewRound:                    opts.OnNewRound,
			StreamDoneCallback:            opts.StreamDoneCallback,
			ReportActivity:                opts.ReportActivity,
			RecordToolCall:                opts.RecordToolCall,
			RecordToolResult:              opts.RecordToolResult,
			RecordSystemMessages:          opts.RecordSystemMessages,
			AttachLLMTelemetry:            opts.AttachLLMTelemetry,
			AttachPendingVisibleArtifacts: opts.AttachPendingVisibleArtifacts,
		})
		if iterationResult.Response != nil {
			result.Response = iterationResult.Response
			return result
		}
		if iterationResult.Break {
			return result
		}
		if iterationResult.Continue {
			continue
		}
	}
}

func (h *IMMessageHandler) runAgentLoopIteration(opts agentLoopIterationDispatchOptions) agentLoopIterationDispatchResult {
	iterationStartedAt := time.Now()
	requestID, loopID := "", ""
	if opts.Context != nil {
		requestID = opts.Context.Runtime.RequestID
		loopID = opts.Context.ID
	}
	logSlowPhase := func(phase string, startedAt time.Time) {
		if elapsed := time.Since(startedAt); elapsed >= 500*time.Millisecond {
			log.Printf("[agent-loop-iter] slow phase=%s owner=%q request_id=%q loop=%q iteration=%d elapsed=%s", phase, opts.UserID, requestID, loopID, opts.Iteration, elapsed.Round(time.Millisecond))
		}
	}
	phaseStartedAt := time.Now()
	roundPrep := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:                 opts.Context,
		UserID:                  opts.UserID,
		UserText:                opts.UserText,
		Iteration:               opts.Iteration,
		EffectiveMax:            *opts.EffectiveMax,
		MinIterations:           opts.MinIterations,
		ConfigMax:               opts.ConfigMax,
		ChatFinalizeGrace:       opts.ChatFinalizeGrace,
		Config:                  opts.Config,
		Conversation:            *opts.Conversation,
		Tools:                   *opts.Tools,
		ToolsTokenBudget:        *opts.ToolsTokenBudget,
		BaseTools:               opts.BaseTools,
		DirectModeToolsFiltered: opts.RunState.DirectModeToolsFiltered,
		EffectiveTokenLimit:     opts.RunState.EffectiveTokenLimit,
		Phase:                   opts.Phase,
		GoalAnchor:              opts.GoalAnchor,
		ProgressTracker:         opts.ProgressTracker,
		TrialState:              opts.TrialState,
		DriftDetector:           opts.DriftDetector,
		MilestoneTracker:        opts.MilestoneTracker,
		LastInputTokens:         opts.Telemetry.LastLLMInputTokens,
		LastOutputTokens:        opts.Telemetry.LastLLMOutputTokens,
		SendProgress:            opts.RuntimeState.SendProgress,
		IsDebug:                 opts.RuntimeState.IsDebug,
		RecordSystemMessages:    opts.RecordSystemMessages,
	})
	logSlowPhase("round_prep", phaseStartedAt)
	*opts.EffectiveMax = roundPrep.EffectiveMax
	*opts.Conversation = roundPrep.Conversation
	*opts.Tools = roundPrep.Tools
	*opts.ToolsTokenBudget = roundPrep.ToolsTokenBudget
	opts.RunState.ApplyRoundPrep(roundPrep)
	if roundPrep.Response != nil {
		return agentLoopIterationDispatchResult{Response: roundPrep.Response}
	}
	if roundPrep.Stop {
		return agentLoopIterationDispatchResult{Break: true}
	}
	if !opts.Telemetry.FirstLLMRequestMarked {
		opts.Telemetry.PreLLMIterationPrepElapsed += roundPrep.PrepElapsed
	}
	llmRequestContext := opts.RequestContext
	llmReplanRevision := int64(0)
	var endLLMOperation context.CancelFunc
	if opts.Context != nil {
		llmRequestContext, endLLMOperation, llmReplanRevision = opts.Context.BeginReplannableOperation(opts.RequestContext)
		defer func() {
			if endLLMOperation != nil {
				endLLMOperation()
			}
		}()
	}

	// Prefer run-state ActiveConfig so mid-loop escalations take effect.
	llmCfg := opts.Config
	if opts.RunState != nil && strings.TrimSpace(opts.RunState.ActiveConfig.Model) != "" {
		llmCfg = opts.RunState.ActiveConfig
	}
	requestBreakdown := agent.EstimateLoopInputBreakdown(*opts.Conversation, *opts.Tools)
	agent.RecordLoopInputBreakdown(requestBreakdown)
	opts.Telemetry.InputBreakdown = requestBreakdown

	phaseStartedAt = time.Now()
	llmDispatch := h.dispatchAgentLoopLLMRound(agentLoopLLMDispatchOptions{
		Context:             opts.Context,
		RequestContext:      llmRequestContext,
		Config:              llmCfg,
		Conversation:        *opts.Conversation,
		Tools:               *opts.Tools,
		HTTPClient:          opts.HTTPClient,
		OnToken:             opts.OnToken,
		OnProgress:          opts.OnProgress,
		StreamDoneCallback:  opts.StreamDoneCallback,
		AdaptiveRetry:       opts.AdaptiveRetry,
		FirstRequestMetrics: opts.FirstRequestMetrics,
		FirstRequestMarked:  opts.Telemetry.FirstLLMRequestMarked,
		FirstRequestStarted: opts.Telemetry.FirstLLMRequestStartedAt,
		FirstResponseAt:     opts.Telemetry.FirstLLMResponseAt,
		StreamDone:          opts.Telemetry.StreamDone(),
		UserID:              opts.UserID,
		History:             *opts.History,
		UserText:            opts.UserText,
		Iteration:           opts.Iteration,
		OnNewRound:          opts.OnNewRound,
		InFlightLifecycle:   opts.InFlightLifecycle,
	})
	if endLLMOperation != nil {
		endLLMOperation()
		endLLMOperation = nil
	}
	llmElapsed := time.Since(phaseStartedAt)
	logSlowPhase("llm_round", phaseStartedAt)
	resp := llmDispatch.Response
	*opts.Conversation = llmDispatch.Conversation
	opts.Telemetry.ApplyLLMDispatch(llmDispatch)
	if opts.Context != nil && opts.Context.ReplanRequestedSince(llmReplanRevision) {
		return agentLoopIterationDispatchResult{Continue: true}
	}
	if llmDispatch.Cancelled {
		return agentLoopIterationDispatchResult{Break: true}
	}
	if llmDispatch.Exit != nil {
		return agentLoopIterationDispatchResult{Response: llmDispatch.Exit}
	}

	phaseStartedAt = time.Now()
	postTurn := h.handleAgentLoopPostLLMTurn(agentLoopPostLLMTurnOptions{
		Context:                       opts.Context,
		Response:                      resp,
		UserID:                        opts.UserID,
		Iteration:                     opts.Iteration,
		Platform:                      opts.Platform,
		EffectiveMax:                  *opts.EffectiveMax,
		Conversation:                  *opts.Conversation,
		History:                       *opts.History,
		Recorder:                      opts.Recorder,
		Phase:                         opts.Phase,
		StreamDone:                    opts.Telemetry.StreamDone(),
		ReportActivity:                opts.ReportActivity,
		RecordSystemMessages:          opts.RecordSystemMessages,
		AttachLLMTelemetry:            opts.AttachLLMTelemetry,
		AttachPendingVisibleArtifacts: opts.AttachPendingVisibleArtifacts,
	})
	logSlowPhase("post_llm", phaseStartedAt)
	choice := postTurn.Choice
	msgContent := postTurn.MessageContent
	msgReasoning := postTurn.MessageReasoning
	*opts.Conversation = postTurn.Conversation
	*opts.History = postTurn.History
	opts.Telemetry.ApplyPostLLMTurn(postTurn)

	// V2 workflow doc buffer: accumulate non-empty final text output
	// so captureWorkflowDocAfterAgentLoop can use the complete accumulated text
	// instead of just resp.Text (which only contains the last iteration's output).
	// Tool-call rounds often contain process narration ("I'll inspect/write..."),
	// so exclude them from the phase document buffer.
	// Strip thinking tags before accumulating — they are not part of the document.
	if opts.Context != nil && opts.Context.WorkflowAgentLoop && msgContent != "" && len(choice.Message.ToolCalls) == 0 {
		cleaned := stripThinkingTags(msgContent)
		if opts.Context.WorkflowDocPhase {
			cleaned = v2.SanitizePhaseOutput(opts.Context.WorkflowPhaseID, msgContent)
			if !workflowDocTextLooksComplete(opts.Context.WorkflowPhaseID, cleaned) {
				cleaned = ""
			}
		}
		if cleaned != "" {
			if opts.Context.WorkflowDocBuffer.Len() > 0 {
				opts.Context.WorkflowDocBuffer.WriteString("\n\n")
			}
			opts.Context.WorkflowDocBuffer.WriteString(cleaned)
		}
	}

	// V2 workflow doc phase: force finalize when LLM produces substantial text
	// output without tool calls. This prevents the agent loop from running
	// indefinitely in NeedsConfirm phases that also use tools (e.g. patent
	// disclosure parsing: read_file to parse → then output analysis report).
	//
	// Two convergence conditions:
	// 1. Current iteration has >= 200 rune text output + no tool calls
	//    (LLM produced a complete document in one shot)
	// 2. WorkflowDocBuffer has accumulated >= 500 rune across iterations +
	//    current iteration has no tool calls (LLM finished using tools and
	//    is now outputting summary/conclusion text)
	if opts.Context != nil && opts.Context.WorkflowDocPhase && len(choice.Message.ToolCalls) == 0 {
		cleanedDoc := v2.SanitizePhaseOutput(opts.Context.WorkflowPhaseID, msgContent)
		trimmed := strings.TrimSpace(cleanedDoc)
		bufLen := len([]rune(opts.Context.WorkflowDocBuffer.String()))
		currentLen := len([]rune(trimmed))

		shouldFinalize := false
		if currentLen >= 200 && workflowDocTextLooksComplete(opts.Context.WorkflowPhaseID, trimmed) {
			// Condition 1: substantial output in current iteration
			shouldFinalize = true
		} else if bufLen >= 500 && opts.Iteration > 0 && workflowDocTextLooksComplete(opts.Context.WorkflowPhaseID, opts.Context.WorkflowDocBuffer.String()) {
			// Condition 2: enough accumulated across iterations, LLM is wrapping up
			shouldFinalize = true
		}

		if shouldFinalize {
			if opts.Phase != nil {
				opts.Phase.Stage = agentStageFinalize
			}
			// Use the full buffer if it has more content than current iteration alone
			finalText := cleanedDoc
			if bufLen > currentLen {
				finalText = strings.TrimSpace(opts.Context.WorkflowDocBuffer.String())
			}
			resp := &IMAgentResponse{Text: finalText, Reasoning: msgReasoning}
			if opts.AttachLLMTelemetry != nil {
				opts.AttachLLMTelemetry(resp)
			}
			if opts.AttachPendingVisibleArtifacts != nil {
				opts.AttachPendingVisibleArtifacts(resp)
			}
			h.saveConversationHistoryTimed(opts.UserID, *opts.History, resp)
			return agentLoopIterationDispatchResult{Response: resp}
		}
	}

	if postTurn.Response != nil {
		if postTurn.Response.Reasoning == "" {
			postTurn.Response.Reasoning = msgReasoning
		}
		return agentLoopIterationDispatchResult{Response: postTurn.Response}
	}
	if postTurn.ContinueLoop {
		return agentLoopIterationDispatchResult{Continue: true}
	}
	if opts.Context != nil && opts.Context.ReplanRequestedSince(llmReplanRevision) {
		*opts.Conversation = stripTrailingBrokenConversationToolGroup(*opts.Conversation)
		*opts.History = stripTrailingBrokenToolGroup(*opts.History)
		return agentLoopIterationDispatchResult{Continue: true}
	}
	if len(choice.Message.ToolCalls) == 0 {
		noToolPath := h.handleAgentLoopNoToolPath(agentLoopNoToolPathOptions{
			Context:                  opts.Context,
			UserID:                   opts.UserID,
			UserText:                 opts.UserText,
			Iteration:                opts.Iteration,
			Platform:                 opts.Platform,
			MessageContent:           msgContent,
			Choice:                   choice,
			Phase:                    opts.Phase,
			Conversation:             *opts.Conversation,
			Tools:                    *opts.Tools,
			BaseTools:                opts.BaseTools,
			History:                  *opts.History,
			LengthContinuationBuffer: &opts.RunState.LengthContinuationBuffer,
			TotalToolCallsInLoop:     opts.RunState.TotalToolCallsInLoop,
			StreamDone:               opts.Telemetry.StreamDone(),
			VoiceData:                opts.RunState.VoiceData,
			VoiceFileName:            opts.RunState.VoiceFileName,
			VoiceMimeType:            opts.RunState.VoiceMimeType,
			RecordSystemMessages:     opts.RecordSystemMessages,
			AttachLLMTelemetry:       opts.AttachLLMTelemetry,
			AttachVisibleArtifacts:   opts.AttachPendingVisibleArtifacts,
		})
		*opts.Conversation = noToolPath.Conversation
		*opts.Tools = noToolPath.Tools
		msgContent = noToolPath.MessageContent
		opts.Telemetry.ApplyNoToolPath(noToolPath)
		if noToolPath.Response != nil {
			if noToolPath.Response.Reasoning == "" {
				noToolPath.Response.Reasoning = msgReasoning
			}
			return agentLoopIterationDispatchResult{Response: noToolPath.Response}
		}
		if noToolPath.ContinueLoop {
			return agentLoopIterationDispatchResult{Continue: true}
		}
	}

	phaseStartedAt = time.Now()
	toolPath := h.handleAgentLoopToolPath(agentLoopToolPathOptions{
		Context:                    opts.Context,
		UserID:                     opts.UserID,
		UserText:                   opts.UserText,
		Iteration:                  opts.Iteration,
		Platform:                   opts.Platform,
		MessageContent:             msgContent,
		LengthContinuationText:     opts.RunState.LengthContinuationBuffer.String(),
		Choice:                     choice,
		Phase:                      opts.Phase,
		Tools:                      *opts.Tools,
		BaseTools:                  opts.BaseTools,
		Conversation:               *opts.Conversation,
		History:                    *opts.History,
		VisibleArtifacts:           opts.VisibleArtifacts,
		DriftDetector:              opts.DriftDetector,
		TrialState:                 opts.TrialState,
		CodingIterCount:            opts.RunState.CodingIterCount,
		TotalToolCallsInLoop:       opts.RunState.TotalToolCallsInLoop,
		ConsecutiveWriteFileErrors: &opts.RunState.ConsecutiveWriteFileErrors,
		InFlightLifecycle:          opts.InFlightLifecycle,
		OnProgress:                 opts.OnProgress,
		OnToken:                    opts.OnToken,
		SendToolProgress:           opts.RuntimeState.SendToolProgress,
		MilestoneTracker:           opts.MilestoneTracker,
		Recorder:                   opts.Recorder,
		RecordToolCall:             opts.RecordToolCall,
		RecordToolResult:           opts.RecordToolResult,
		RecordSystemMessages:       opts.RecordSystemMessages,
		AdaptiveRetry:              opts.AdaptiveRetry,
		Debug:                      opts.RuntimeState.IsDebug(),
		StreamDone:                 opts.Telemetry.StreamDone(),
		LastCompressionSummary:     &opts.RunState.LastCompressionSummary,
		AttachLLMTelemetry:         opts.AttachLLMTelemetry,
		AttachVisibleArtifacts:     opts.AttachPendingVisibleArtifacts,
	})
	logSlowPhase("tool_path", phaseStartedAt)
	*opts.Conversation = toolPath.Conversation
	*opts.History = toolPath.History
	*opts.Tools = toolPath.Tools
	opts.RunState.ApplyToolPath(toolPath)
	opts.Telemetry.ApplyToolPath(toolPath)
	// Tools appeared after a light model turn — escalate next rounds to reasoning.
	if len(choice.Message.ToolCalls) > 0 {
		h.escalateRunStateToReasoning(opts.RunState, "tools requested after light turn")
	}
	if toolPath.Response != nil {
		return agentLoopIterationDispatchResult{Response: toolPath.Response}
	}
	if toolPath.Continue {
		return agentLoopIterationDispatchResult{Continue: true}
	}
	if elapsed := time.Since(iterationStartedAt); elapsed >= time.Second {
		log.Printf("[agent-loop-iter] done owner=%q request_id=%q loop=%q iteration=%d elapsed=%s llm=%s tool=%s", opts.UserID, requestID, loopID, opts.Iteration, elapsed.Round(time.Millisecond), llmElapsed.Round(time.Millisecond), toolPath.ToolExecElapsed.Round(time.Millisecond))
	}
	return agentLoopIterationDispatchResult{}
}
