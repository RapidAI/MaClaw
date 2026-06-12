package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/progress"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

type agentLoopToolBranchStartOptions struct {
	Context    *LoopContext
	Iteration  int
	Choice     llm.Choice
	Phase      *agentLoopPhase
	TrialState *trialReflectState
	UserID     string
}

func (h *IMMessageHandler) startAgentLoopToolBranch(opts agentLoopToolBranchStartOptions) int {
	if opts.Phase != nil {
		opts.Phase.Stage = agentStageExecute
		opts.Phase.ConsecutiveNoTool = 0
		resetAgentLoopTruncationRecoveryAfterToolCalls(opts.Phase, opts.Choice)
	}
	logAgentLoopPartialTruncation(opts.Choice)
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
	MessageContent             string
	LengthContinuationText     string
	Choice                     llm.Choice
	Phase                      *agentLoopPhase
	Conversation               []interface{}
	History                    []agent.ConversationEntry
	VisibleArtifacts           *pendingVisibleArtifacts
	DriftDetector              *DriftDetector
	TrialState                 *trialReflectState
	CodingIterCount            int
	TotalToolCallsInLoop       int
	ConsecutiveWriteFileErrors *int
	InFlightLifecycle          *imInFlightLifecycle
	OnProgress                 tool.ProgressCallback
	OnToken                    llm.TokenCallback
	SendToolProgress           func(string)
	MilestoneTracker           *progress.AgentProgressTracker
	RecordToolCall             func(string, string, string)
	RecordToolResult           func(string, interface{})
	RecordSystemMessages       func(int, []interface{})
	AdaptiveRetry              *AdaptiveRetry
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
	requestID, loopID := "", ""
	if opts.Context != nil {
		requestID = opts.Context.Runtime.RequestID
		loopID = opts.Context.ID
	}
	logSlow := func(stage string, startedAt time.Time) {
		if elapsed := time.Since(startedAt); elapsed >= 500*time.Millisecond {
			log.Printf("[agent-loop-tool-path] slow stage=%s owner=%q request_id=%q loop=%q iteration=%d elapsed=%s tool_calls=%d",
				stage, opts.UserID, requestID, loopID, opts.Iteration, elapsed.Round(time.Millisecond), len(opts.Choice.Message.ToolCalls))
		}
	}
	stageStartedAt := time.Now()
	totalToolCalls := opts.TotalToolCallsInLoop + h.startAgentLoopToolBranch(agentLoopToolBranchStartOptions{
		Context:    opts.Context,
		Iteration:  opts.Iteration,
		Choice:     opts.Choice,
		Phase:      opts.Phase,
		TrialState: opts.TrialState,
		UserID:     opts.UserID,
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
		GateActive:                 false,
		MessageContent:             opts.MessageContent,
		ToolCalls:                  opts.Choice.Message.ToolCalls,
		Phase:                      opts.Phase,
		Conversation:               opts.Conversation,
		History:                    opts.History,
		VisibleArtifacts:           opts.VisibleArtifacts,
		DriftDetector:              opts.DriftDetector,
		ConsecutiveWriteFileErrors: opts.ConsecutiveWriteFileErrors,
		InFlightLifecycle:          opts.InFlightLifecycle,
		OnProgress:                 opts.OnProgress,
		OnToken:                    opts.OnToken,
		SendToolProgress:           opts.SendToolProgress,
		MilestoneTracker:           opts.MilestoneTracker,
		RecordToolCall:             opts.RecordToolCall,
		RecordToolResult:           opts.RecordToolResult,
		RecordSystemMessages:       opts.RecordSystemMessages,
		AdaptiveRetry:              opts.AdaptiveRetry,
		Debug:                      opts.Debug,
		StreamDone:                 opts.StreamDone,
	})
	logSlow("execute_tool_calls", stageStartedAt)
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

	stageStartedAt = time.Now()
	postToolResult := h.handleAgentLoopPostToolBranch(agentLoopPostToolBranchOptions{
		Context:                    opts.Context,
		UserID:                     opts.UserID,
		UserText:                   opts.UserText,
		Iteration:                  opts.Iteration,
		Platform:                   opts.Platform,
		MessageContent:             opts.MessageContent,
		AssistantHadVisibleContent: assistantMessageHasVisibleContent(opts.Choice.Message.Content),
		LengthContinuationText:     opts.LengthContinuationText,
		ToolCalls:                  opts.Choice.Message.ToolCalls,
		ToolResults:                toolCallResult.ToolResults,
		ToolOutcomes:               toolCallResult.ToolOutcomes,
		ToolExecResults:            toolCallResult.ToolExecResults,
		Conversation:               result.Conversation,
		History:                    result.History,
		Phase:                      opts.Phase,
		TrialState:                 opts.TrialState,
		CodingIterCount:            opts.CodingIterCount,
		TotalToolCallsInLoop:       totalToolCalls,
		PendingArtifacts:           toolCallResult.PendingArtifacts,
		VisibleArtifacts:           opts.VisibleArtifacts,
		StreamDone:                 opts.StreamDone,
		LastCompressionSummary:     opts.LastCompressionSummary,
		RecordSystemMessages:       opts.RecordSystemMessages,
		AttachLLMTelemetry:         opts.AttachLLMTelemetry,
		AttachVisibleArtifacts:     opts.AttachVisibleArtifacts,
	})
	logSlow("post_tool_branch", stageStartedAt)
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
	DriftDetector              *DriftDetector
	ConsecutiveWriteFileErrors *int
	InFlightLifecycle          *imInFlightLifecycle
	OnProgress                 tool.ProgressCallback
	OnToken                    llm.TokenCallback
	SendToolProgress           func(string)
	MilestoneTracker           *progress.AgentProgressTracker
	RecordToolCall             func(string, string, string)
	RecordToolResult           func(string, interface{})
	RecordSystemMessages       func(int, []interface{})
	AdaptiveRetry              *AdaptiveRetry
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
	requestID, loopID := "", ""
	if opts.Context != nil {
		requestID = opts.Context.Runtime.RequestID
		loopID = opts.Context.ID
	}
	logSlow := func(stage string, startedAt time.Time, tc llm.ToolCall) {
		if elapsed := time.Since(startedAt); elapsed >= 500*time.Millisecond {
			log.Printf("[agent-loop-tool-path] slow stage=%s owner=%q request_id=%q loop=%q iteration=%d tool=%q elapsed=%s",
				stage, opts.UserID, requestID, loopID, opts.Iteration, strings.TrimSpace(tc.Function.Name), elapsed.Round(time.Millisecond))
		}
	}
	result := agentLoopToolCallsResult{
		Conversation:    opts.Conversation,
		History:         opts.History,
		ToolResults:     make([]string, 0, len(opts.ToolCalls)),
		ToolOutcomes:    make([]toolOutcome, 0, len(opts.ToolCalls)),
		ToolExecResults: make([]toolExecutionResult, 0, len(opts.ToolCalls)),
	}
	for tcIdx, tc := range opts.ToolCalls {
		if opts.Context != nil && opts.Context.IsCancelled() {
			opts.Context.SetLoopState(LoopStateStopped)
			result.Response = h.cancelledExitResponse(opts.UserID, result.History, opts.UserText)
			result.Cancelled = true
			return result
		}

		toolExecStartedAt := time.Now()
		execResult := h.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
			Context:          opts.Context,
			UserID:           opts.UserID,
			UserText:         opts.UserText,
			SkipWorkflowGate: h.shouldSkipWorkflowToolExecutionGate(opts.UserID, opts.Context),
			ToolCall:         tc,
			Iteration:        opts.Iteration,
			Phase:            derefAgentLoopPhase(opts.Phase),
			Debug:            opts.Debug,
			OnProgress:       opts.OnProgress,
			OnToken:          opts.OnToken,
			SendToolProgress: opts.SendToolProgress,
			MilestoneTracker: opts.MilestoneTracker,
			RecordToolCall:   opts.RecordToolCall,
			AdaptiveRetry:    opts.AdaptiveRetry,
		})
		logSlow("tool_exec", toolExecStartedAt, tc)
		rawResult := execResult.Text

		stageStartedAt := time.Now()
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
		logSlow("post_exec_pre_observation", stageStartedAt, tc)

		stageStartedAt = time.Now()
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
		h.recordAgentLoopToolUsage(opts.Context, opts.UserText, tc, execResult.Outcome, agentLoopToolUsageFollowUp(tcIdx, opts.ToolCalls, execResult.Outcome))
		logSlow("record_usage", stageStartedAt, tc)

		stageStartedAt = time.Now()
		h.recordAgentLoopToolTrace(opts.Context, tc, traceResult, rawResult, execResult)
		logSlow("trace_and_steering", stageStartedAt, tc)

		stageStartedAt = time.Now()
		truncated := truncateToolResultForTool(tc.Function.Name, toolContent)
		// OpenHuman-inspired: check tool result for prompt injection attempts.
		// Only check external-source tools (web_fetch, web_search, read_file, bash)
		// to avoid wasting CPU on internal tools that return safe content.
		if isExternalSourceTool(tc.Function.Name) {
			if injectionWarning := h.checkToolResultInjection(tc.Function.Name, truncated); injectionWarning != "" {
				truncated = injectionWarning + truncated
			}
		}
		logSlow("truncate_and_injection_scan", stageStartedAt, tc)

		stageStartedAt = time.Now()
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
			ParallelGroupIndex:         tcIdx,
			ParallelGroupTotal:         len(opts.ToolCalls),
		})
		logSlow("commit_tool_result", stageStartedAt, tc)
		result.Conversation = commitResult.Conversation
		result.History = commitResult.History
		if commitResult.Response != nil {
			result.Response = commitResult.Response
			return result
		}
	}
	return result
}

func agentLoopToolUsageFollowUp(index int, toolCalls []llm.ToolCall, outcome toolOutcome) toolUsageFollowUp {
	if outcome != toolOutcomeFailed || index < 0 || index >= len(toolCalls) {
		return toolUsageFollowUpContinue
	}
	name := strings.TrimSpace(toolCalls[index].Function.Name)
	for i := index + 1; i < len(toolCalls); i++ {
		if strings.TrimSpace(toolCalls[i].Function.Name) == name {
			return toolUsageFollowUpRetry
		}
	}
	return toolUsageFollowUpContinue
}

func derefAgentLoopPhase(phase *agentLoopPhase) agentLoopPhase {
	if phase == nil {
		return agentLoopPhase{}
	}
	return *phase
}
