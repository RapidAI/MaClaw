package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/progress"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

type agentLoopBonusRoundOptions struct {
	Context                *LoopContext
	UserID                 string
	Config                 corelib.MaclawLLMConfig
	RequestContext         context.Context
	Conversation           []interface{}
	History                []agent.ConversationEntry
	Tools                  []map[string]interface{}
	HTTPClient             *http.Client
	EffectiveTokenLimit    int
	ToolsTokenBudget       int
	OnToken                llm.TokenCallback
	OnProgress             tool.ProgressCallback
	OnNewRound             NewRoundCallback
	StreamDoneCallback     func()
	FirstRequestMetrics    *llmFirstRequestMetrics
	StreamDone             bool
	Phase                  *agentLoopPhase
	MilestoneTracker       *progress.AgentProgressTracker
	Recorder               *TrajectoryRecorder
	InFlightLifecycle      *imInFlightLifecycle
	RecordToolCall         func(id, name, args string)
	RecordToolResult       func(id string, content interface{})
	AttachLLMTelemetry     func(*IMAgentResponse)
	AttachVisibleArtifacts func(*IMAgentResponse)
	SendProgress           func(string)
	Debug                  bool
}

type agentLoopBonusRoundResult struct {
	Response         *IMAgentResponse
	Conversation     []interface{}
	History          []agent.ConversationEntry
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	UsageElapsed     time.Duration
	UsageDone        bool
	ToolExecElapsed  time.Duration
}

func (h *IMMessageHandler) runActiveSessionBonusRound(opts agentLoopBonusRoundOptions) agentLoopBonusRoundResult {
	result := agentLoopBonusRoundResult{Conversation: opts.Conversation, History: opts.History}
	if opts.SendProgress != nil {
		opts.SendProgress("Reasoning rounds are exhausted, but coding sessions are still active. Checking status...")
	}

	conversation := opts.Conversation
	conversation = trimConversation(conversation, opts.EffectiveTokenLimit, opts.ToolsTokenBudget, nil)
	if opts.OnNewRound != nil {
		opts.OnNewRound()
	}
	bonusMetrics := &llmStreamMetrics{}
	bonusResp, err := h.doLLMRequestStream(opts.RequestContext, opts.Config, conversation, opts.Tools, opts.HTTPClient, opts.OnToken, bonusMetrics)
	opts.FirstRequestMetrics.AddStreamMetrics(bonusMetrics)
	if err == nil && opts.StreamDoneCallback != nil {
		opts.StreamDoneCallback()
	}
	if bonusResp != nil {
		usageStartedAt := time.Now()
		usage := h.recordLLMUsageSnapshot("bonus_round", bonusResp, conversation)
		result.InputTokens = usage.Input
		result.OutputTokens = usage.Output
		result.CacheReadTokens = usage.CacheRead
		result.CacheWriteTokens = usage.CacheWrite
		if opts.StreamDone {
			result.UsageElapsed = time.Since(usageStartedAt)
			result.UsageDone = true
		}
	}
	if err == nil && bonusResp != nil && len(bonusResp.Choices) > 0 {
		conversation, result.History, result.ToolExecElapsed = h.applyBonusRoundChoice(conversation, result.History, bonusResp.Choices[0], opts)
	}

	result.Conversation = conversation
	h.saveConversationHistoryTimed(opts.UserID, result.History, &IMAgentResponse{})
	resp := &IMAgentResponse{Text: "Coding session is still running. Reply 'continue' to keep watching, or send another message to continue normally.", Deferred: true}
	opts.AttachLLMTelemetry(resp)
	opts.AttachVisibleArtifacts(resp)
	result.Response = resp
	return result
}

func (h *IMMessageHandler) applyBonusRoundChoice(conversation []interface{}, history []agent.ConversationEntry, choice llm.Choice, opts agentLoopBonusRoundOptions) ([]interface{}, []agent.ConversationEntry, time.Duration) {
	content := choice.Message.Content
	reasoning := choice.Message.ReasoningContent
	if content == "" && reasoning != "" {
		content = reasoning
	}
	assistantMsg := map[string]interface{}{"role": "assistant", "content": content}
	if reasoning != "" {
		assistantMsg["reasoning_content"] = reasoning
	} else {
		assistantMsg["reasoning_content"] = ""
	}
	if len(choice.Message.ToolCalls) > 0 {
		assistantMsg["tool_calls"] = choice.Message.ToolCalls
	}
	conversation = append(conversation, assistantMsg)
	if opts.Recorder != nil {
		opts.Recorder.Record("assistant", content, choice.Message.ToolCalls, "", reasoning)
	}
	history = append(history, agent.ConversationEntry{
		Role: "assistant", Content: content, ReasoningContent: reasoning, ToolCalls: choice.Message.ToolCalls,
	})

	var toolExecElapsed time.Duration
	for _, tc := range choice.Message.ToolCalls {
		if opts.Context != nil && opts.Context.IsCancelled() {
			break
		}
		tc.Function.Arguments = normalizeAgentLoopToolArgumentsJSON(tc.Function.Arguments)
		workflowAllowed, workflowReject := h.workflowAllowsBonusRoundToolCall(opts.UserID, opts.Context, tc)
		lang := h.imUILang()
		toolOnProgress := filteredToolProgressCallback(lang, tc.Function.Name, opts.OnProgress, opts.Debug)
		toolExecStartedAt := time.Now()
		if workflowAllowed {
			opts.MilestoneTracker.RecordToolCall(tc.Function.Name, tc.Function.Arguments, false)
			if opts.Debug && opts.SendProgress != nil {
				opts.SendProgress(userFacingToolProgressText(lang, tc.Function.Name))
			}
			opts.RecordToolCall(tc.ID, tc.Function.Name, tc.Function.Arguments)
		}
		execResult := workflowReject
		if workflowAllowed {
			execResult = h.executeBonusRoundTool(tc, toolOnProgress, opts.Phase, opts.HTTPClient, opts.UserID, opts.Context)
		}
		toolResult := execResult.Text
		if workflowAllowed {
			opts.MilestoneTracker.RecordToolCall(tc.Function.Name, tc.Function.Arguments, true)
		}
		if IsAskUserResult(toolResult) {
			toolResult = "ask_user is unavailable in background tasks; choose the next action directly."
		}
		if agent.IsRecordAudioResult(toolResult) {
			toolResult = "record_audio is unavailable in background tasks; choose the next action directly."
		}
		if IsSubAgentContext(toolResult) {
			toolResult = ExtractSubAgentContext(toolResult)
		}
		if h.toolRouter != nil && tool.ShouldPinConditionalTool(tc.Function.Name) && execResult.Outcome == toolOutcomeSucceeded && execResult.FailureKind == toolFailureNone {
			h.toolRouter.ActivateSessionTool(tc.Function.Name)
			log.Printf("[ToolPin] session-pinned conditional tool %q", tc.Function.Name)
		}
		if opts.StreamDone {
			toolExecElapsed += time.Since(toolExecStartedAt)
		}
		truncated := truncateToolResultForToolWithSession(tc.Function.Name, opts.UserID, toolResult)
		opts.RecordToolResult(tc.ID, truncated)
		conversation = append(conversation, map[string]interface{}{"role": "tool", "tool_call_id": tc.ID, "content": truncated})
		history = append(history, agent.ConversationEntry{Role: "tool", Content: truncated, ToolCallID: tc.ID, ToolOutcome: execResult.Outcome.String()})
		opts.InFlightLifecycle.SetOnce()
	}
	return conversation, history, toolExecElapsed
}

func (h *IMMessageHandler) workflowAllowsBonusRoundToolCall(userID string, ctx *LoopContext, tc llm.ToolCall) (bool, toolExecutionResult) {
	policyUserID := h.workflowPolicyOwnerID(userID, ctx)
	if !h.isWorkflowToolAllowedForOwner(policyUserID, tc.Function.Name) {
		result := workflowPolicyToolRejectedText(tc.Function.Name)
		return false, toolExecutionResult{Text: result, ToolName: tc.Function.Name, ToolKind: classifyAgentToolKind(tc.Function.Name), Outcome: toolOutcomeFailed, FailureKind: toolFailurePolicyRejected}
	}
	if allowed, reason := h.isWorkflowToolCallAllowedForOwner(policyUserID, tc.Function.Name, tc.Function.Arguments); !allowed {
		result := fmt.Sprintf("[system rejected] %s", reason)
		return false, toolExecutionResult{Text: result, ToolName: tc.Function.Name, ToolKind: classifyAgentToolKind(tc.Function.Name), Outcome: toolOutcomeFailed, FailureKind: toolFailurePolicyRejected}
	}
	return true, toolExecutionResult{}
}

func (h *IMMessageHandler) executeBonusRoundTool(tc llm.ToolCall, onProgress tool.ProgressCallback, phase *agentLoopPhase, httpClient *http.Client, userID string, ctx *LoopContext) toolExecutionResult {
	policyUserID := h.workflowPolicyOwnerID(userID, ctx)
	if allowed, result := h.workflowAllowsBonusRoundToolCall(userID, ctx, tc); !allowed {
		return result
	}
	if phase != nil && phase.TruncationBlockedTools[tc.Function.Name] {
		result := fmt.Sprintf("[system rejected] %s is temporarily blocked because its arguments were repeatedly truncated. Use another currently available tool path.", tc.Function.Name)
		return toolExecutionResult{Text: result, ToolName: tc.Function.Name, ToolKind: classifyAgentToolKind(tc.Function.Name), Outcome: toolOutcomeFailed, FailureKind: toolFailureTruncationBlocked}
	}
	if tool.IsCodingSessionTool(tc.Function.Name) {
		result := fmt.Sprintf("[system rejected] %s is disabled for the agent. Coding tasks must run through the internal CodingSubAgent, not external coding sessions.", tc.Function.Name)
		return toolExecutionResult{Text: result, ToolName: tc.Function.Name, ToolKind: classifyAgentToolKind(tc.Function.Name), Outcome: toolOutcomeFailed, FailureKind: toolFailurePolicyRejected}
	}
	if tc.Function.Name == "delegate_task" {
		loopCtx := ctx
		if loopCtx == nil && httpClient != nil {
			loopCtx = &LoopContext{HTTPClient: httpClient}
		}
		if result, handled := h.executeCodingWorkflowDelegateTask(agentLoopToolExecutionOptions{
			Context:    loopCtx,
			UserID:     userID,
			ToolCall:   tc,
			OnProgress: onProgress,
		}); handled {
			return result
		}
	}
	// In bonus round (still agent loop context), intercept missing/invalid parameter
	// errors before executeToolDetailed to prevent AgentView panel from popping up.
	if errResult := h.preCheckToolArgsForAgentLoop(tc.Function.Name, tc.Function.Arguments, -1); errResult != nil {
		return *errResult
	}
	execCtx := context.Background()
	if ctx != nil {
		var cancel context.CancelFunc
		execCtx, cancel = ctx.Context()
		defer cancel()
	}
	return h.executeToolDetailedWithRuntimeContext(execCtx, policyUserID, loopContextHasExplicitRuntimeOwner(ctx), runtimePlatformFromLoopContext(ctx), tc.Function.Name, tc.Function.Arguments, "", onProgress)
}
