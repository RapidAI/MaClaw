package main

import (
	"context"
	"net/http"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/progress"
	"github.com/RapidAI/CodeClaw/corelib/tool"
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
	GateConfig                    codingToolGateConfig
	SkipCodingGate                bool
	OrchestratorActive            func() bool
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
	SteeringDetector              *SteeringWorkflowDetector
	OnToken                       llm.TokenCallback
	OnProgress                    tool.ProgressCallback
	OnNewRound                    NewRoundCallback
	StreamDoneCallback            StreamDoneCallback
	ReportActivity                func(int, int, string)
	RecordToolCall                func(string, string, string)
	RecordToolResult              func(string, interface{})
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
	GateConfig                    codingToolGateConfig
	SkipCodingGate                bool
	OrchestratorActive            func() bool
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
	SteeringDetector              *SteeringWorkflowDetector
	OnToken                       llm.TokenCallback
	OnProgress                    tool.ProgressCallback
	OnNewRound                    NewRoundCallback
	StreamDoneCallback            StreamDoneCallback
	ReportActivity                func(int, int, string)
	RecordToolCall                func(string, string, string)
	RecordToolResult              func(string, interface{})
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
			GateConfig:                    opts.GateConfig,
			SkipCodingGate:                opts.SkipCodingGate,
			OrchestratorActive:            opts.OrchestratorActive,
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
			SteeringDetector:              opts.SteeringDetector,
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
		HTTPClient:              opts.HTTPClient,
		Conversation:            *opts.Conversation,
		Tools:                   *opts.Tools,
		ToolsTokenBudget:        *opts.ToolsTokenBudget,
		BaseTools:               opts.BaseTools,
		GateConfig:              opts.GateConfig,
		SkipCodingGate:          opts.SkipCodingGate,
		OrchestratorActive:      opts.OrchestratorActive,
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

	llmDispatch := h.dispatchAgentLoopLLMRound(agentLoopLLMDispatchOptions{
		Context:             opts.Context,
		RequestContext:      opts.RequestContext,
		Config:              opts.Config,
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
	resp := llmDispatch.Response
	*opts.Conversation = llmDispatch.Conversation
	opts.Telemetry.ApplyLLMDispatch(llmDispatch)
	if llmDispatch.Cancelled {
		return agentLoopIterationDispatchResult{Break: true}
	}
	if llmDispatch.Exit != nil {
		return agentLoopIterationDispatchResult{Response: llmDispatch.Exit}
	}

	postTurn := h.handleAgentLoopPostLLMTurn(agentLoopPostLLMTurnOptions{
		Context:                       opts.Context,
		Response:                      resp,
		UserID:                        opts.UserID,
		Iteration:                     opts.Iteration,
		Platform:                      opts.Platform,
		EffectiveMax:                  *opts.EffectiveMax,
		GateConfig:                    opts.GateConfig,
		SkipCodingGate:                opts.SkipCodingGate,
		OrchestratorActive:            opts.OrchestratorActive,
		Conversation:                  *opts.Conversation,
		History:                       *opts.History,
		Recorder:                      opts.Recorder,
		Phase:                         opts.Phase,
		SteeringDetector:              opts.SteeringDetector,
		StreamDone:                    opts.Telemetry.StreamDone(),
		ReportActivity:                opts.ReportActivity,
		RecordSystemMessages:          opts.RecordSystemMessages,
		AttachLLMTelemetry:            opts.AttachLLMTelemetry,
		AttachPendingVisibleArtifacts: opts.AttachPendingVisibleArtifacts,
	})
	choice := postTurn.Choice
	msgContent := postTurn.MessageContent
	*opts.Conversation = postTurn.Conversation
	*opts.History = postTurn.History
	opts.Telemetry.ApplyPostLLMTurn(postTurn)
	if postTurn.Response != nil {
		return agentLoopIterationDispatchResult{Response: postTurn.Response}
	}
	if postTurn.ContinueLoop {
		return agentLoopIterationDispatchResult{Continue: true}
	}
	if len(choice.Message.ToolCalls) == 0 {
		noToolPath := h.handleAgentLoopNoToolPath(agentLoopNoToolPathOptions{
			Context:                  opts.Context,
			UserID:                   opts.UserID,
			UserText:                 opts.UserText,
			Iteration:                opts.Iteration,
			Platform:                 opts.Platform,
			GateConfig:               opts.GateConfig,
			MessageContent:           msgContent,
			Choice:                   choice,
			Phase:                    opts.Phase,
			Conversation:             *opts.Conversation,
			Tools:                    *opts.Tools,
			BaseTools:                opts.BaseTools,
			History:                  *opts.History,
			LengthContinuationBuffer: &opts.RunState.LengthContinuationBuffer,
			TotalToolCallsInLoop:     opts.RunState.TotalToolCallsInLoop,
			SteeringDetector:         opts.SteeringDetector,
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
			return agentLoopIterationDispatchResult{Response: noToolPath.Response}
		}
		if noToolPath.ContinueLoop {
			return agentLoopIterationDispatchResult{Continue: true}
		}
	}

	toolPath := h.handleAgentLoopToolPath(agentLoopToolPathOptions{
		Context:                    opts.Context,
		UserID:                     opts.UserID,
		UserText:                   opts.UserText,
		Iteration:                  opts.Iteration,
		Platform:                   opts.Platform,
		GateConfig:                 opts.GateConfig,
		MessageContent:             msgContent,
		LengthContinuationText:     opts.RunState.LengthContinuationBuffer.String(),
		Choice:                     choice,
		Phase:                      opts.Phase,
		Conversation:               *opts.Conversation,
		History:                    *opts.History,
		VisibleArtifacts:           opts.VisibleArtifacts,
		SteeringDetector:           opts.SteeringDetector,
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
	*opts.Conversation = toolPath.Conversation
	*opts.History = toolPath.History
	opts.RunState.ApplyToolPath(toolPath)
	opts.Telemetry.ApplyToolPath(toolPath)
	if toolPath.Response != nil {
		return agentLoopIterationDispatchResult{Response: toolPath.Response}
	}
	return agentLoopIterationDispatchResult{}
}
