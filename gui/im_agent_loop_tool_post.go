package main

import (
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

type agentLoopPostToolBranchOptions struct {
	Context                *LoopContext
	UserID                 string
	UserText               string
	Iteration              int
	Platform               string
	GateConfig             codingToolGateConfig
	MessageContent         string
	LengthContinuationText string
	ToolCalls              []llm.ToolCall
	ToolResults            []string
	ToolOutcomes           []toolOutcome
	ToolExecResults        []toolExecutionResult
	Conversation           []interface{}
	History                []agent.ConversationEntry
	Phase                  *agentLoopPhase
	TrialState             *trialReflectState
	CodingIterCount        int
	TotalToolCallsInLoop   int
	PendingArtifacts       agentLoopPendingToolArtifacts
	VisibleArtifacts       *pendingVisibleArtifacts
	SteeringDetector       *SteeringWorkflowDetector
	StreamDone             bool
	LastCompressionSummary *string
	RecordSystemMessages   func(int, []interface{})
	AttachLLMTelemetry     func(*IMAgentResponse)
	AttachVisibleArtifacts func(*IMAgentResponse)
}

type agentLoopPostToolBranchResult struct {
	Conversation             []interface{}
	History                  []agent.ConversationEntry
	MessageContent           string
	CodingIterCount          int
	Response                 *IMAgentResponse
	PostStreamReturnPrepTime bool
}

func (h *IMMessageHandler) handleAgentLoopPostToolBranch(opts agentLoopPostToolBranchOptions) agentLoopPostToolBranchResult {
	result := agentLoopPostToolBranchResult{
		Conversation:    opts.Conversation,
		History:         opts.History,
		MessageContent:  opts.MessageContent,
		CodingIterCount: opts.CodingIterCount,
	}

	processSkillPreferenceToolExecutions(opts.Phase, opts.ToolCalls, opts.ToolExecResults)
	h.observeAgentLoopTrialIteration(opts.Context, opts.TrialState, opts.Phase, opts.UserText, opts.ToolCalls, opts.ToolResults, opts.ToolOutcomes)
	codingBudget := h.enforceAgentLoopCodingBudget(opts.UserID, opts.Iteration, result.CodingIterCount, opts.ToolCalls, result.Conversation, result.History, opts.Phase, opts.PendingArtifacts.VoiceData, opts.PendingArtifacts.VoiceFileName, opts.PendingArtifacts.VoiceMimeType, opts.RecordSystemMessages, opts.AttachLLMTelemetry, opts.AttachVisibleArtifacts)
	result.CodingIterCount = codingBudget.Count
	result.Conversation = codingBudget.Conversation
	if codingBudget.Response != nil {
		result.Response = codingBudget.Response
		return result
	}

	screenshotIsOnlyAction := len(opts.ToolCalls) <= 1 && opts.TotalToolCallsInLoop <= 1
	screenshotResult := h.handleAgentLoopScreenshotArtifact(opts.UserID, opts.Iteration, opts.Platform, opts.PendingArtifacts.ImageKey, opts.PendingArtifacts.ScreenshotSent, screenshotIsOnlyAction, opts.TotalToolCallsInLoop, len(opts.ToolCalls), result.History, opts.VisibleArtifacts, opts.StreamDone, opts.AttachLLMTelemetry)
	if screenshotResult.PostStreamReturnPrepTime {
		result.PostStreamReturnPrepTime = true
	}
	if screenshotResult.Response != nil {
		result.Response = screenshotResult.Response
		return result
	}

	fileArtifactResult := h.handleAgentLoopFileArtifacts(opts.UserID, opts.Platform, opts.PendingArtifacts.Files, opts.PendingArtifacts.VoiceData, opts.PendingArtifacts.VoiceFileName, opts.PendingArtifacts.VoiceMimeType, result.History, opts.StreamDone, opts.AttachLLMTelemetry)
	if fileArtifactResult.PostStreamReturnPrepTime {
		result.PostStreamReturnPrepTime = true
	}
	if fileArtifactResult.Response != nil {
		result.Response = fileArtifactResult.Response
		return result
	}

	toolBranchGate := h.applyAgentLoopToolBranchNeedsConfirmGate(opts.Context, opts.UserID, opts.Iteration, opts.Platform, opts.GateConfig, result.MessageContent, opts.LengthContinuationText, opts.Phase, opts.SteeringDetector, result.History, opts.PendingArtifacts.VoiceData, opts.PendingArtifacts.VoiceFileName, opts.PendingArtifacts.VoiceMimeType, opts.AttachLLMTelemetry, opts.AttachVisibleArtifacts)
	result.MessageContent = toolBranchGate.MsgContent
	if toolBranchGate.Response != nil {
		result.Response = toolBranchGate.Response
		return result
	}

	result.History = h.applyPendingContextCompression(opts.UserID, result.History, opts.LastCompressionSummary)
	return result
}
