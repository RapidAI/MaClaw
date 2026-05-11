package main

import (
	"fmt"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/progress"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

type agentLoopToolBranchStartOptions struct {
	Context          *LoopContext
	Iteration        int
	Choice           llm.Choice
	Phase            *agentLoopPhase
	TrialState       *trialReflectState
	UserID           string
	SteeringDetector *SteeringWorkflowDetector
	GateConfig       codingToolGateConfig
}

func (h *IMMessageHandler) startAgentLoopToolBranch(opts agentLoopToolBranchStartOptions) int {
	if opts.Phase != nil {
		opts.Phase.Stage = agentStageExecute
		opts.Phase.ConsecutiveNoTool = 0
	}
	logAgentLoopPartialTruncation(opts.Choice)
	h.emitAgentLoopSteeringSuggestMaximize(opts.UserID, opts.SteeringDetector, opts.GateConfig)
	if opts.TrialState != nil && opts.TrialState.enabled && h.traceService != nil && opts.Context != nil && opts.Context.RunID != "" {
		h.appendTraceEvent(opts.Context, "trial.started", "info", "Trial iteration started", fmt.Sprintf("iteration=%d tool_calls=%d", opts.Iteration+1, len(opts.Choice.Message.ToolCalls)), "", "")
	}
	return len(opts.Choice.Message.ToolCalls)
}

type agentLoopToolPathOptions struct {
	Context                    *LoopContext
	UserID                     string
	UserText                   string
	Iteration                  int
	Platform                   string
	GateConfig                 codingToolGateConfig
	MessageContent             string
	LengthContinuationText     string
	Choice                     llm.Choice
	Phase                      *agentLoopPhase
	Conversation               []interface{}
	History                    []agent.ConversationEntry
	VisibleArtifacts           *pendingVisibleArtifacts
	SteeringDetector           *SteeringWorkflowDetector
	DriftDetector              *DriftDetector
	TrialState                 *trialReflectState
	CodingIterCount            int
	TotalToolCallsInLoop       int
	ConsecutiveWriteFileErrors *int
	InFlightLifecycle          *imInFlightLifecycle
	OnProgress                 tool.ProgressCallback
	SendToolProgress           func(string)
	MilestoneTracker           *progress.AgentProgressTracker
	RecordToolCall             func(string, string, string)
	RecordToolResult           func(string, interface{})
	RecordSystemMessages       func(int, []interface{})
	Debug                      bool
	StreamDone                 bool
	LastCompressionSummary     *string
	AttachLLMTelemetry         func(*IMAgentResponse)
	AttachVisibleArtifacts     func(*IMAgentResponse)
}

type agentLoopToolPathResult struct {
	Conversation             []interface{}
	History                  []agent.ConversationEntry
	MessageContent           string
	CodingIterCount          int
	TotalToolCallsInLoop     int
	VoiceData                string
	VoiceFileName            string
	VoiceMimeType            string
	ToolExecElapsed          time.Duration
	PostStreamReturnPrepTime bool
	Response                 *IMAgentResponse
}

func (h *IMMessageHandler) handleAgentLoopToolPath(opts agentLoopToolPathOptions) agentLoopToolPathResult {
	totalToolCalls := opts.TotalToolCallsInLoop + h.startAgentLoopToolBranch(agentLoopToolBranchStartOptions{
		Context:          opts.Context,
		Iteration:        opts.Iteration,
		Choice:           opts.Choice,
		Phase:            opts.Phase,
		TrialState:       opts.TrialState,
		UserID:           opts.UserID,
		SteeringDetector: opts.SteeringDetector,
		GateConfig:       opts.GateConfig,
	})
	result := agentLoopToolPathResult{
		Conversation:         opts.Conversation,
		History:              opts.History,
		MessageContent:       opts.MessageContent,
		CodingIterCount:      opts.CodingIterCount,
		TotalToolCallsInLoop: totalToolCalls,
	}

	toolCallResult := h.executeAgentLoopToolCalls(agentLoopToolCallsOptions{
		Context:                    opts.Context,
		UserID:                     opts.UserID,
		UserText:                   opts.UserText,
		Iteration:                  opts.Iteration,
		Platform:                   opts.Platform,
		GateActive:                 opts.GateConfig.active,
		MessageContent:             opts.MessageContent,
		ToolCalls:                  opts.Choice.Message.ToolCalls,
		Phase:                      opts.Phase,
		Conversation:               opts.Conversation,
		History:                    opts.History,
		VisibleArtifacts:           opts.VisibleArtifacts,
		SteeringDetector:           opts.SteeringDetector,
		DriftDetector:              opts.DriftDetector,
		ConsecutiveWriteFileErrors: opts.ConsecutiveWriteFileErrors,
		InFlightLifecycle:          opts.InFlightLifecycle,
		OnProgress:                 opts.OnProgress,
		SendToolProgress:           opts.SendToolProgress,
		MilestoneTracker:           opts.MilestoneTracker,
		RecordToolCall:             opts.RecordToolCall,
		RecordToolResult:           opts.RecordToolResult,
		RecordSystemMessages:       opts.RecordSystemMessages,
		Debug:                      opts.Debug,
		StreamDone:                 opts.StreamDone,
	})
	result.Conversation = toolCallResult.Conversation
	result.History = toolCallResult.History
	result.ToolExecElapsed = toolCallResult.ToolExecElapsed
	result.VoiceData = toolCallResult.PendingArtifacts.VoiceData
	result.VoiceFileName = toolCallResult.PendingArtifacts.VoiceFileName
	result.VoiceMimeType = toolCallResult.PendingArtifacts.VoiceMimeType
	if toolCallResult.Response != nil {
		result.Response = toolCallResult.Response
		return result
	}

	postToolResult := h.handleAgentLoopPostToolBranch(agentLoopPostToolBranchOptions{
		Context:                opts.Context,
		UserID:                 opts.UserID,
		UserText:               opts.UserText,
		Iteration:              opts.Iteration,
		Platform:               opts.Platform,
		GateConfig:             opts.GateConfig,
		MessageContent:         opts.MessageContent,
		LengthContinuationText: opts.LengthContinuationText,
		ToolCalls:              opts.Choice.Message.ToolCalls,
		ToolResults:            toolCallResult.ToolResults,
		ToolOutcomes:           toolCallResult.ToolOutcomes,
		ToolExecResults:        toolCallResult.ToolExecResults,
		Conversation:           result.Conversation,
		History:                result.History,
		Phase:                  opts.Phase,
		TrialState:             opts.TrialState,
		CodingIterCount:        opts.CodingIterCount,
		TotalToolCallsInLoop:   totalToolCalls,
		PendingArtifacts:       toolCallResult.PendingArtifacts,
		VisibleArtifacts:       opts.VisibleArtifacts,
		SteeringDetector:       opts.SteeringDetector,
		StreamDone:             opts.StreamDone,
		LastCompressionSummary: opts.LastCompressionSummary,
		RecordSystemMessages:   opts.RecordSystemMessages,
		AttachLLMTelemetry:     opts.AttachLLMTelemetry,
		AttachVisibleArtifacts: opts.AttachVisibleArtifacts,
	})
	result.Conversation = postToolResult.Conversation
	result.History = postToolResult.History
	result.MessageContent = postToolResult.MessageContent
	result.CodingIterCount = postToolResult.CodingIterCount
	result.PostStreamReturnPrepTime = postToolResult.PostStreamReturnPrepTime
	result.Response = postToolResult.Response
	return result
}

type agentLoopToolCallsOptions struct {
	Context                    *LoopContext
	UserID                     string
	UserText                   string
	Iteration                  int
	Platform                   string
	GateActive                 bool
	MessageContent             string
	ToolCalls                  []llm.ToolCall
	Phase                      *agentLoopPhase
	Conversation               []interface{}
	History                    []agent.ConversationEntry
	VisibleArtifacts           *pendingVisibleArtifacts
	SteeringDetector           *SteeringWorkflowDetector
	DriftDetector              *DriftDetector
	ConsecutiveWriteFileErrors *int
	InFlightLifecycle          *imInFlightLifecycle
	OnProgress                 tool.ProgressCallback
	SendToolProgress           func(string)
	MilestoneTracker           *progress.AgentProgressTracker
	RecordToolCall             func(string, string, string)
	RecordToolResult           func(string, interface{})
	RecordSystemMessages       func(int, []interface{})
	Debug                      bool
	StreamDone                 bool
}

type agentLoopToolCallsResult struct {
	Conversation     []interface{}
	History          []agent.ConversationEntry
	ToolResults      []string
	ToolOutcomes     []toolOutcome
	ToolExecResults  []toolExecutionResult
	PendingArtifacts agentLoopPendingToolArtifacts
	ToolExecElapsed  time.Duration
	Response         *IMAgentResponse
	Cancelled        bool
}

func (h *IMMessageHandler) executeAgentLoopToolCalls(opts agentLoopToolCallsOptions) agentLoopToolCallsResult {
	result := agentLoopToolCallsResult{
		Conversation:    opts.Conversation,
		History:         opts.History,
		ToolResults:     make([]string, 0, len(opts.ToolCalls)),
		ToolOutcomes:    make([]toolOutcome, 0, len(opts.ToolCalls)),
		ToolExecResults: make([]toolExecutionResult, 0, len(opts.ToolCalls)),
	}
	for _, tc := range opts.ToolCalls {
		if opts.Context.IsCancelled() {
			opts.Context.SetLoopState(LoopStateStopped)
			result.Response = h.cancelledExitResponse(opts.UserID, result.History, opts.UserText)
			result.Cancelled = true
			return result
		}

		toolExecStartedAt := time.Now()
		execResult := h.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
			UserID:           opts.UserID,
			SkipWorkflowGate: opts.Context != nil && opts.Context.SkipNeedsConfirmGate,
			ToolCall:         tc,
			Iteration:        opts.Iteration,
			Phase:            derefAgentLoopPhase(opts.Phase),
			Debug:            opts.Debug,
			OnProgress:       opts.OnProgress,
			SendToolProgress: opts.SendToolProgress,
			MilestoneTracker: opts.MilestoneTracker,
			RecordToolCall:   opts.RecordToolCall,
		})
		rawResult := execResult.Text
		if opts.VisibleArtifacts != nil && opts.VisibleArtifacts.QRCodeURL == "" {
			opts.VisibleArtifacts.QRCodeURL = extractWeixinQRCodeURLFromToolResult(rawResult)
		}

		askUserResult := h.handleAgentLoopAskUserToolResult(opts.UserID, opts.Platform, opts.MessageContent, rawResult, opts.GateActive, tc.ID, result.Conversation, result.History, result.ToolResults, opts.RecordToolResult)
		rawResult = askUserResult.Result
		result.Conversation = askUserResult.Conversation
		result.History = askUserResult.History
		result.ToolResults = askUserResult.ToolResults
		if askUserResult.Response != nil {
			result.Response = askUserResult.Response
			return result
		}

		if IsSubAgentContext(rawResult) {
			rawResult = ExtractSubAgentContext(rawResult)
		}
		h.pinConditionalToolAfterSuccess(tc.Function.Name, execResult)

		payloadObservation := parseToolPayloadResult(rawResult)
		traceResult := payloadObservation.TraceResult
		toolContent := payloadObservation.ToolContent
		if opts.StreamDone {
			result.ToolExecElapsed += time.Since(toolExecStartedAt)
		}
		result.ToolResults = append(result.ToolResults, traceResult)
		result.ToolOutcomes = append(result.ToolOutcomes, execResult.Outcome)
		result.ToolExecResults = append(result.ToolExecResults, execResult)
		result.PendingArtifacts.ApplyObservation(payloadObservation)

		h.recordAgentLoopToolTrace(opts.Context, tc, traceResult, rawResult, execResult)
		h.emitAgentLoopSteeringDocUpdate(opts.UserID, opts.SteeringDetector, tc.Function.Name, tc.Function.Arguments)

		truncated := truncateToolResultForTool(tc.Function.Name, toolContent)
		commitResult := h.commitAgentLoopToolResult(agentLoopToolCommitOptions{
			UserID:                     opts.UserID,
			ToolCall:                   tc,
			TruncatedResult:            truncated,
			Execution:                  execResult,
			Conversation:               result.Conversation,
			History:                    result.History,
			Phase:                      opts.Phase,
			DriftDetector:              opts.DriftDetector,
			ConsecutiveWriteFileErrors: opts.ConsecutiveWriteFileErrors,
			InFlightLifecycle:          opts.InFlightLifecycle,
			RecordToolResult:           opts.RecordToolResult,
			RecordSystemMessages:       opts.RecordSystemMessages,
		})
		result.Conversation = commitResult.Conversation
		result.History = commitResult.History
		if commitResult.Response != nil {
			result.Response = commitResult.Response
			return result
		}
	}
	return result
}

func derefAgentLoopPhase(phase *agentLoopPhase) agentLoopPhase {
	if phase == nil {
		return agentLoopPhase{}
	}
	return *phase
}
