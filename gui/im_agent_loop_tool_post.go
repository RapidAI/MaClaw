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
		// File delivery short-circuits the loop; keep any assistant text from
		// this turn so the user still sees the explanation plus forward status.
		if msg := strings.TrimSpace(stripThinkingTags(opts.MessageContent)); msg != "" {
			if existing := strings.TrimSpace(fileArtifactResult.Response.Text); existing != "" {
				fileArtifactResult.Response.Text = msg + "\n\n" + existing
			} else {
				fileArtifactResult.Response.Text = msg
			}
		}
		result.Response = fileArtifactResult.Response
		return result
	}

	stageStartedAt = time.Now()
	toolBranchGate := h.applyAgentLoopToolBranchNeedsConfirmGate(opts.Context, opts.UserID, opts.Iteration, opts.Platform, result.MessageContent, opts.LengthContinuationText, opts.Phase, result.History, opts.PendingArtifacts.VoiceData, opts.PendingArtifacts.VoiceFileName, opts.PendingArtifacts.VoiceMimeType, opts.AttachLLMTelemetry, opts.AttachVisibleArtifacts)
	logSlow("needs_confirm_gate", stageStartedAt)
	result.MessageContent = toolBranchGate.MsgContent
	if toolBranchGate.Response != nil {
		result.Response = toolBranchGate.Response
		return result
	}

	stageStartedAt = time.Now()
	result.History = h.applyPendingContextCompression(opts.UserID, result.History, opts.LastCompressionSummary)
	logSlow("pending_compression", stageStartedAt)
	// After tool execution (including compress_context), the loop always continues.
	// The only valid termination signal is the LLM producing a pure-text response
	// with no tool calls — handled by the no-tool branch in the caller.
	return result
}

func assistantMessageHasVisibleContent(content string) bool {
	return strings.TrimSpace(stripThinkingTags(content)) != ""
}
