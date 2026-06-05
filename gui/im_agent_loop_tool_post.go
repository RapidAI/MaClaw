package main

import (
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

type agentLoopPostToolBranchOptions struct {
	Context                    *LoopContext
	UserID                     string
	UserText                   string
	Iteration                  int
	Platform                   string
	GateConfig                 codingToolGateConfig
	MessageContent             string
	AssistantHadVisibleContent bool
	LengthContinuationText     string
	ToolCalls                  []llm.ToolCall
	ToolResults                []string
	ToolOutcomes               []toolOutcome
	ToolExecResults            []toolExecutionResult
	Conversation               []interface{}
	History                    []agent.ConversationEntry
	Phase                      *agentLoopPhase
	TrialState                 *trialReflectState
	CodingIterCount            int
	TotalToolCallsInLoop       int
	PendingArtifacts           agentLoopPendingToolArtifacts
	VisibleArtifacts           *pendingVisibleArtifacts
	SteeringDetector           *SteeringWorkflowDetector
	StreamDone                 bool
	LastCompressionSummary     *string
	RecordSystemMessages       func(int, []interface{})
	AttachLLMTelemetry         func(*IMAgentResponse)
	AttachVisibleArtifacts     func(*IMAgentResponse)
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
	requestID, loopID := "", ""
	if opts.Context != nil {
		requestID = opts.Context.Runtime.RequestID
		loopID = opts.Context.ID
	}
	logSlow := func(stage string, startedAt time.Time) {
		if elapsed := time.Since(startedAt); elapsed >= 500*time.Millisecond {
			log.Printf("[agent-loop-tool-post] slow stage=%s owner=%q request_id=%q loop=%q iteration=%d elapsed=%s tool_calls=%d",
				stage, opts.UserID, requestID, loopID, opts.Iteration, elapsed.Round(time.Millisecond), len(opts.ToolCalls))
		}
	}
	result := agentLoopPostToolBranchResult{
		Conversation:    opts.Conversation,
		History:         opts.History,
		MessageContent:  opts.MessageContent,
		CodingIterCount: opts.CodingIterCount,
	}

	stageStartedAt := time.Now()
	processSkillPreferenceToolExecutions(opts.Phase, opts.ToolCalls, opts.ToolExecResults)
	h.observeAgentLoopTrialIteration(opts.Context, opts.TrialState, opts.Phase, opts.UserText, opts.ToolCalls, opts.ToolResults, opts.ToolOutcomes)
	logSlow("preferences_and_trial", stageStartedAt)

	stageStartedAt = time.Now()
	codingBudget := h.enforceAgentLoopCodingBudget(opts.Context, opts.UserID, opts.Iteration, result.CodingIterCount, opts.ToolCalls, result.Conversation, result.History, opts.Phase, opts.PendingArtifacts.VoiceData, opts.PendingArtifacts.VoiceFileName, opts.PendingArtifacts.VoiceMimeType, opts.RecordSystemMessages, opts.AttachLLMTelemetry, opts.AttachVisibleArtifacts)
	logSlow("coding_budget", stageStartedAt)
	result.CodingIterCount = codingBudget.Count
	result.Conversation = codingBudget.Conversation
	if codingBudget.Response != nil {
		result.Response = codingBudget.Response
		return result
	}

	screenshotIsOnlyAction := len(opts.ToolCalls) <= 1 && opts.TotalToolCallsInLoop <= 1
	stageStartedAt = time.Now()
	screenshotResult := h.handleAgentLoopScreenshotArtifact(opts.UserID, opts.Iteration, opts.Platform, opts.PendingArtifacts.ImageKey, opts.PendingArtifacts.ScreenshotSent, screenshotIsOnlyAction, opts.TotalToolCallsInLoop, len(opts.ToolCalls), result.History, opts.VisibleArtifacts, opts.StreamDone, opts.AttachLLMTelemetry)
	logSlow("screenshot_artifact", stageStartedAt)
	if screenshotResult.PostStreamReturnPrepTime {
		result.PostStreamReturnPrepTime = true
	}
	if screenshotResult.Response != nil {
		result.Response = screenshotResult.Response
		return result
	}

	stageStartedAt = time.Now()
	fileArtifactResult := h.handleAgentLoopFileArtifacts(opts.UserID, opts.Platform, opts.PendingArtifacts.Files, opts.PendingArtifacts.VoiceData, opts.PendingArtifacts.VoiceFileName, opts.PendingArtifacts.VoiceMimeType, result.History, opts.StreamDone, opts.AttachLLMTelemetry)
	logSlow("file_artifact", stageStartedAt)
	if fileArtifactResult.PostStreamReturnPrepTime {
		result.PostStreamReturnPrepTime = true
	}
	if fileArtifactResult.Response != nil {
		result.Response = fileArtifactResult.Response
		return result
	}

	stageStartedAt = time.Now()
	toolBranchGate := h.applyAgentLoopToolBranchNeedsConfirmGate(opts.Context, opts.UserID, opts.Iteration, opts.Platform, opts.GateConfig, result.MessageContent, opts.LengthContinuationText, opts.Phase, opts.SteeringDetector, result.History, opts.PendingArtifacts.VoiceData, opts.PendingArtifacts.VoiceFileName, opts.PendingArtifacts.VoiceMimeType, opts.AttachLLMTelemetry, opts.AttachVisibleArtifacts)
	logSlow("needs_confirm_gate", stageStartedAt)
	result.MessageContent = toolBranchGate.MsgContent
	if toolBranchGate.Response != nil {
		result.Response = toolBranchGate.Response
		return result
	}

	stageStartedAt = time.Now()
	result.History = h.applyPendingContextCompression(opts.UserID, result.History, opts.LastCompressionSummary)
	logSlow("pending_compression", stageStartedAt)
	// Some tools only maintain loop state after the answer is already complete.
	// Do not make those tools force another LLM round and hide the visible answer.
	if shouldFinalizeAssistantContentAfterResponseNeutralTools(opts.AssistantHadVisibleContent, result.MessageContent, opts.ToolCalls, opts.ToolOutcomes) {
		if opts.Phase != nil {
			opts.Phase.Stage = agentStageFinalize
		}
		finalText := stripThinkingTags(opts.LengthContinuationText + result.MessageContent)
		if opts.Context != nil {
			finalText = appendPendingBackgroundTaskFinalHint(finalText, h.pendingBackgroundTaskHint(opts.Context.StartedAt))
		}
		finalResp := &IMAgentResponse{Text: finalText}
		if opts.StreamDone {
			result.PostStreamReturnPrepTime = true
		}
		if opts.AttachLLMTelemetry != nil {
			opts.AttachLLMTelemetry(finalResp)
		}
		if opts.AttachVisibleArtifacts != nil {
			opts.AttachVisibleArtifacts(finalResp)
		}
		h.saveConversationHistoryTimed(opts.UserID, result.History, finalResp)
		result.Response = finalResp
	}
	return result
}

func shouldFinalizeAssistantContentAfterResponseNeutralTools(hasVisibleContent bool, content string, toolCalls []llm.ToolCall, toolOutcomes []toolOutcome) bool {
	if !hasVisibleContent || !assistantMessageHasVisibleContent(content) || len(toolCalls) == 0 || len(toolCalls) != len(toolOutcomes) {
		return false
	}
	for i, tc := range toolCalls {
		if toolOutcomes[i] != toolOutcomeSucceeded || !isResponseNeutralPostTurnTool(tc.Function.Name) {
			return false
		}
	}
	return true
}

func assistantMessageHasVisibleContent(content string) bool {
	return strings.TrimSpace(stripThinkingTags(content)) != ""
}

func isResponseNeutralPostTurnTool(name string) bool {
	switch strings.TrimSpace(name) {
	case "compress_context":
		return true
	default:
		return false
	}
}
