package main

import (
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func (h *IMMessageHandler) tryDirectExecutionProfile(msg IMUserMessage, loopCtx *LoopContext, history []agent.ConversationEntry) (*IMAgentResponse, bool) {
	if loopCtx == nil || !loopCtx.Runtime.Execution.IsDirect() {
		return nil, false
	}
	startedAt := time.Now()
	requestID := strings.TrimSpace(loopCtx.Runtime.RequestID)
	profile := loopCtx.Runtime.Execution
	toolName := directExecutionToolName(profile)
	if toolName == "" {
		log.Printf("[exec-direct] skip request_id=%q user=%q task=%s reason=no_tool", requestID, msg.UserID, profile.TaskType)
		return nil, false
	}
	if h == nil || h.registry == nil {
		log.Printf("[exec-direct] skip request_id=%q user=%q task=%s tool=%s reason=no_registry", requestID, msg.UserID, profile.TaskType, toolName)
		return nil, false
	}
	if _, ok := h.registry.Get(toolName); !ok {
		log.Printf("[exec-direct] skip request_id=%q user=%q task=%s tool=%s reason=tool_missing", requestID, msg.UserID, profile.TaskType, toolName)
		return nil, false
	}
	contract := h.executionContractForRegisteredToolName(toolName)
	if !contract.Explicit || !contract.SupportsDirect || !contract.Deterministic {
		log.Printf("[exec-direct] skip request_id=%q user=%q task=%s tool=%s reason=contract_not_direct", requestID, msg.UserID, profile.TaskType, toolName)
		return nil, false
	}
	result := h.executeToolDetailedWithRuntimeState(msg.UserID, strings.TrimSpace(msg.UserID) != "", msg.Platform, toolName, `{}`, msg.Text, nil)
	if result.Outcome != toolOutcomeSucceeded || strings.TrimSpace(result.Text) == "" {
		log.Printf("[exec-direct] fallback request_id=%q user=%q task=%s tool=%s outcome=%s failure=%s elapsed=%s",
			requestID, msg.UserID, profile.TaskType, toolName, result.Outcome.String(), string(result.FailureKind), time.Since(startedAt).Round(time.Millisecond))
		return nil, false
	}
	text := directExecutionFinalText(msg, profile, strings.TrimSpace(result.Text))
	resp := &IMAgentResponse{
		Text:           text,
		RequestID:      requestID,
		SessionKey:     loopCtx.Runtime.Conversation.SessionKey,
		ResponseSource: "direct_execution",
	}
	updated := append([]agent.ConversationEntry(nil), history...)
	updated = append(updated,
		agent.ConversationEntry{Role: "user", Content: msg.Text},
		agent.ConversationEntry{Role: "assistant", Content: text},
	)
	if h.memory != nil {
		h.saveConversationHistoryTimed(msg.UserID, updated, resp)
	}
	log.Printf("[exec-direct] done request_id=%q user=%q task=%s tool=%s elapsed=%s text_len=%d",
		requestID, msg.UserID, profile.TaskType, toolName, time.Since(startedAt).Round(time.Millisecond), len([]rune(text)))
	imPerfLog("direct_execution", startedAt, requestID, msg.UserID, "task", profile.TaskType, "tool", toolName, "text_len", len([]rune(text)))
	return resp, true
}

func directExecutionToolName(profile ExecutionProfile) string {
	return strings.TrimSpace(profile.DirectToolName)
}

func directExecutionFinalText(msg IMUserMessage, profile ExecutionProfile, toolResult string) string {
	switch directExecutionToolName(profile) {
	case "current_datetime":
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(msg.Lang)), "en") {
			return "Current date/time: " + toolResult
		}
		return "\u5f53\u524d\u65e5\u671f\u65f6\u95f4\uff1a" + toolResult
	default:
		return toolResult
	}
}
