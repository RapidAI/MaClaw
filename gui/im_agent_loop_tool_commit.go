package main

import (
	"crypto/sha256"
	"fmt"
	"log"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

type agentLoopToolCommitOptions struct {
	UserID                     string
	ToolCall                   llm.ToolCall
	TruncatedResult            string
	Execution                  toolExecutionResult
	Conversation               []interface{}
	History                    []agent.ConversationEntry
	Phase                      *agentLoopPhase
	DriftDetector              *DriftDetector
	ConsecutiveWriteFileErrors *int
	InFlightLifecycle          *imInFlightLifecycle
	RecordToolResult           func(string, interface{})
	RecordSystemMessages       func(int, []interface{})
}

type agentLoopToolCommitResult struct {
	Conversation []interface{}
	History      []agent.ConversationEntry
	Response     *IMAgentResponse
}

func (h *IMMessageHandler) commitAgentLoopToolResult(opts agentLoopToolCommitOptions) agentLoopToolCommitResult {
	tc := opts.ToolCall
	conversation := opts.Conversation
	history := opts.History

	if opts.RecordToolResult != nil {
		opts.RecordToolResult(tc.ID, opts.TruncatedResult)
	}
	conversation = append(conversation, map[string]interface{}{
		"role":         "tool",
		"tool_call_id": tc.ID,
		"content":      opts.TruncatedResult,
	})
	history = append(history, agent.ConversationEntry{
		Role:        "tool",
		Content:     opts.TruncatedResult,
		ToolCallID:  tc.ID,
		ToolOutcome: opts.Execution.Outcome.String(),
	})

	if opts.InFlightLifecycle != nil {
		opts.InFlightLifecycle.SetOnce()
	}

	conversation = handleOversizedFailedToolArguments(conversation, tc, opts.Execution)
	conversation = updateWriteFileRecoveryHint(conversation, opts.Execution, opts.ConsecutiveWriteFileErrors)

	conversation, resp := h.observeToolCommitDrift(opts.UserID, tc, opts.TruncatedResult, history, conversation, opts.Phase, opts.DriftDetector, opts.RecordSystemMessages)
	if resp != nil {
		return agentLoopToolCommitResult{Conversation: conversation, History: history, Response: resp}
	}

	return agentLoopToolCommitResult{Conversation: conversation, History: history}
}

func (h *IMMessageHandler) pinConditionalToolAfterSuccess(toolName string, execResult toolExecutionResult) {
	if h == nil || h.toolRouter == nil || execResult.FailureKind != toolFailureNone || !tool.ShouldPinConditionalTool(toolName) {
		return
	}
	h.toolRouter.ActivateSessionTool(toolName)
	log.Printf("[ToolPin] session-pinned conditional tool %q", toolName)
}

func (h *IMMessageHandler) recordAgentLoopToolTrace(ctx *LoopContext, tc llm.ToolCall, traceResult string, rawResult string, execResult toolExecutionResult) {
	if h == nil || h.traceService == nil || ctx == nil || ctx.RunID == "" {
		return
	}
	h.appendTraceEvent(ctx, "tool.executed", "info", tc.Function.Name, truncateTraceText(traceResult, 220), "", tc.Function.Name)
	h.appendTraceEvidence(ctx, "ai_tool", traceCategoryForToolExecution(execResult), tc.Function.Name, truncateTraceText(traceResult, 400), "", tc.Function.Name)
	if execResult.ToolKind != agentToolKindCreateSession || h.manager == nil {
		return
	}
	if linkedRunID := h.linkTraceToLatestAISession(ctx, rawResult); linkedRunID != "" {
		h.appendTraceEvent(ctx, "session.linked", "info", "Linked remote session", linkedRunID, "", "")
	}
}

func handleOversizedFailedToolArguments(conversation []interface{}, tc llm.ToolCall, execResult toolExecutionResult) []interface{} {
	if !execResult.IsFailure() || len(tc.Function.Arguments) <= 2000 {
		return conversation
	}
	truncateToolCallArgsInConversation(conversation, tc.ID, tc.Function.Arguments)
	log.Printf("[agent-loop] truncated oversized args (%d chars) for failed tool call %s/%s",
		len(tc.Function.Arguments), tc.Function.Name, tc.ID)
	return conversation
}

func updateWriteFileRecoveryHint(conversation []interface{}, execResult toolExecutionResult, consecutiveErrors *int) []interface{} {
	if consecutiveErrors == nil {
		return conversation
	}
	if execResult.IsWriteFileRecoverableFailure() {
		(*consecutiveErrors)++
		if *consecutiveErrors >= 2 {
			jsonHint := fmt.Sprintf("[system hint] Consecutive write_file calls failed %d times. Split large content across smaller write_file calls or use another currently available file-generation path.", *consecutiveErrors)
			conversation = append(conversation, map[string]string{
				"role":    "system",
				"content": jsonHint,
			})
			log.Printf("[agent-loop] injected write_file failure hint after %d consecutive failures", *consecutiveErrors)
		}
		return conversation
	}

	*consecutiveErrors = 0
	return conversation
}

func (h *IMMessageHandler) observeToolCommitDrift(
	userID string,
	tc llm.ToolCall,
	truncated string,
	history []agent.ConversationEntry,
	conversation []interface{},
	phase *agentLoopPhase,
	detector *DriftDetector,
	recordSystemMessages func(int, []interface{}),
) ([]interface{}, *IMAgentResponse) {
	if detector == nil {
		return conversation, nil
	}

	argsHash := fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Function.Arguments)))
	resultHash := fmt.Sprintf("%x", sha256.Sum256([]byte(truncated)))
	detector.Record(ToolCallRecord{
		ToolName:   tc.Function.Name,
		ArgsHash:   argsHash,
		Timestamp:  time.Now(),
		ResultHint: truncateRunesForDrift(truncated, 200),
		ResultHash: resultHash,
	})

	driftResult := detector.DetectDrift()
	if !driftResult.Drifted {
		return conversation, nil
	}

	log.Printf("[Harness] drift detected pattern=%s needHuman=%v replanCount=%d tool=%s",
		driftResult.Pattern, driftResult.NeedHumanHelp, detector.ReplanCount(), driftResult.DriftedTool)
	conversation = append(conversation, map[string]string{
		"role":    "system",
		"content": driftResult.ReplanPrompt,
	})
	if recordSystemMessages != nil {
		recordSystemMessages(len(conversation)-1, conversation)
	}
	detector.ResetWindow()

	if !driftResult.NeedHumanHelp {
		enterRecoverPhase(phase, agentRecoverDriftDetected, buildDriftRecoverPrompt(driftResult))
		return conversation, nil
	}

	h.sessionDriftReplanCount.Store(userID, detector.ReplanCount())
	h.sessionDriftTool.Store(userID, driftResult.DriftedTool)

	resp := &IMAgentResponse{
		Text: fmt.Sprintf("Agent repeatedly called %s without success and stopped trying. Please check the task requirements or provide new guidance.", driftResult.DriftedTool),
	}
	h.saveConversationHistoryTimed(userID, history, resp)
	return conversation, resp
}
