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
	// ParallelGroupIndex is the 0-based index of the current tool_call
	// within the parallel group. Used to defer drift detection responses
	// until the entire group has been executed.
	ParallelGroupIndex int
	// ParallelGroupTotal is the total number of tool_calls in the current
	// parallel group. When > 1, drift detection that would interrupt the
	// loop (NeedHumanHelp=true) is deferred until the last tool_call.
	ParallelGroupTotal int
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
	recordToolResultID := tc.ID

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
	if opts.Execution.FailureKind == toolFailurePolicyRejected && isRolePrefixRiskToolName(tc.Function.Name) {
		redactedID := redactedRolePrefixRiskToolCallID(tc.ID)
		conversation = redactRolePrefixRiskToolCallInConversation(conversation, tc.ID, redactedID)
		history = redactRolePrefixRiskToolCallInHistory(history, tc.ID, redactedID)
		recordToolResultID = redactedID
	}
	if opts.RecordToolResult != nil {
		opts.RecordToolResult(recordToolResultID, opts.TruncatedResult)
	}

	if opts.InFlightLifecycle != nil {
		opts.InFlightLifecycle.SetOnce()
	}

	conversation = handleOversizedFailedToolArguments(conversation, tc, opts.Execution)
	conversation = updateWriteFileRecoveryHint(conversation, opts.Execution, opts.ConsecutiveWriteFileErrors)

	// Drift detection: defer ALL drift responses when in the middle of a
	// parallel tool_calls group. Both interrupting (NeedHumanHelp=true) and
	// non-interrupting (recover prompt injection) responses are unsafe mid-group:
	//
	// 1. Interrupting: remaining tool_calls won't have tool_results → orphaned
	// 2. Non-interrupting: system message between tool_results causes
	//    sanitizeOrphanedToolCalls to break scanning early → false orphan detection
	//
	// Mechanism: always record the tool call into the drift detector window
	// (for accurate frequency/pattern tracking), but only allow drift responses
	// on the LAST tool_call of the group. Deferred drift will be re-detected
	// when the last tool_call is committed.
	isLastInGroup := opts.ParallelGroupTotal <= 1 || opts.ParallelGroupIndex >= opts.ParallelGroupTotal-1
	conversation, resp := h.observeToolCommitDrift(opts.UserID, tc, opts.TruncatedResult, history, conversation, opts.Phase, opts.DriftDetector, opts.RecordSystemMessages, isLastInGroup)
	if resp != nil {
		return agentLoopToolCommitResult{Conversation: conversation, History: history, Response: resp}
	}

	return agentLoopToolCommitResult{Conversation: conversation, History: history}
}

func redactedRolePrefixRiskToolCallID(toolCallID string) string {
	if toolCallID == "" {
		return "blocked_tool_call"
	}
	sum := sha256.Sum256([]byte(toolCallID))
	return fmt.Sprintf("blocked_tool_call_%x", sum[:4])
}

func redactRolePrefixRiskToolCallInConversation(conversation []interface{}, toolCallID, redactedID string) []interface{} {
	if toolCallID == "" {
		return conversation
	}
	for i := len(conversation) - 1; i >= 0; i-- {
		msg, ok := conversation[i].(map[string]interface{})
		if !ok {
			continue
		}
		switch msg["role"] {
		case "assistant":
			if redactToolCallInValue(msg["tool_calls"], toolCallID, redactedID) {
				return conversation
			}
		case "tool":
			if msg["tool_call_id"] == toolCallID {
				msg["tool_call_id"] = redactedID
			}
		}
	}
	return conversation
}

func redactRolePrefixRiskToolCallInHistory(history []agent.ConversationEntry, toolCallID, redactedID string) []agent.ConversationEntry {
	if toolCallID == "" {
		return history
	}
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "tool" && history[i].ToolCallID == toolCallID {
			history[i].ToolCallID = redactedID
			continue
		}
		if history[i].Role != "assistant" {
			continue
		}
		if calls, ok := history[i].ToolCalls.([]llm.ToolCall); ok {
			changed := false
			for j := range calls {
				if calls[j].ID == toolCallID && isRolePrefixRiskToolName(calls[j].Function.Name) {
					calls[j].ID = redactedID
					calls[j].Function.Name = "blocked_tool"
					changed = true
				}
			}
			if changed {
				history[i].ToolCalls = calls
				return history
			}
		}
		if redactToolCallInValue(history[i].ToolCalls, toolCallID, redactedID) {
			return history
		}
	}
	return history
}

func redactToolCallInValue(value interface{}, toolCallID, redactedID string) bool {
	switch calls := value.(type) {
	case []llm.ToolCall:
		for i := range calls {
			if calls[i].ID == toolCallID && isRolePrefixRiskToolName(calls[i].Function.Name) {
				calls[i].ID = redactedID
				calls[i].Function.Name = "blocked_tool"
				return true
			}
		}
	case []interface{}:
		for _, raw := range calls {
			m, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			if redactToolCallMap(m, toolCallID, redactedID) {
				return true
			}
		}
	case []map[string]interface{}:
		for _, m := range calls {
			if redactToolCallMap(m, toolCallID, redactedID) {
				return true
			}
		}
	}
	return false
}

func redactToolCallMap(m map[string]interface{}, toolCallID, redactedID string) bool {
	if m == nil || m["id"] != toolCallID {
		return false
	}
	fn, ok := m["function"].(map[string]interface{})
	if !ok {
		return false
	}
	name, _ := fn["name"].(string)
	if !isRolePrefixRiskToolName(name) {
		return false
	}
	m["id"] = redactedID
	fn["name"] = "blocked_tool"
	return true
}

func (h *IMMessageHandler) pinConditionalToolAfterSuccess(toolName string, execResult toolExecutionResult) {
	if h == nil || h.toolRouter == nil || execResult.Outcome != toolOutcomeSucceeded || execResult.FailureKind != toolFailureNone || !tool.ShouldPinConditionalTool(toolName) {
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
	h.appendTraceEvidence(ctx, traceSourceKindAITool.String(), traceCategoryForToolExecution(execResult).String(), tc.Function.Name, truncateTraceText(traceResult, 400), "", tc.Function.Name)
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
	isLastInGroup bool,
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

	// If drift requires human help (interrupting response) but we're in the
	// middle of a parallel tool_calls group, defer the interruption.
	// Interrupting mid-group leaves orphaned tool_calls in the conversation
	// (assistant message has N tool_calls but only M < N tool_results follow),
	// which causes API proxies to reject the next request with HTTP 400.
	//
	// Non-interrupting drift (recover prompt injection) is ALSO unsafe mid-group
	// because sanitizeOrphanedToolCalls scans tool_results by breaking on any
	// non-tool message. A system message injected between tool_results causes
	// the sanitizer to see an incomplete group and strip the tool_calls.
	//
	// Both cases are deferred: undo the replanCount increment and let the
	// remaining tool_calls execute. The drift will be re-detected on the last
	// tool_call of the group (or on the next iteration).
	if !isLastInGroup {
		log.Printf("[Harness] drift detected pattern=%s needHuman=%v but deferring: parallel group not complete (tool=%s)",
			driftResult.Pattern, driftResult.NeedHumanHelp, driftResult.DriftedTool)
		detector.UndoLastReplan()
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
