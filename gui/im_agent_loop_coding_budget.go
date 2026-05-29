package main

import (
	"fmt"
	"log"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

const (
	codingIterBudgetSoft = 50
)

type agentLoopCodingBudgetResult struct {
	Conversation []interface{}
	Count        int
	Response     *IMAgentResponse
}

func (h *IMMessageHandler) enforceAgentLoopCodingBudget(
	ctx *LoopContext,
	userID string,
	iteration int,
	currentCount int,
	toolCalls []llm.ToolCall,
	conversation []interface{},
	history []agent.ConversationEntry,
	phase *agentLoopPhase,
	voiceData string,
	voiceFileName string,
	voiceMimeType string,
	recordSystemMessages func(int, []interface{}),
	attachLLMTelemetry func(*IMAgentResponse),
	attachPendingVisibleArtifacts func(*IMAgentResponse),
) agentLoopCodingBudgetResult {
	result := agentLoopCodingBudgetResult{Conversation: conversation, Count: nextCodingIterationCount(currentCount, toolCalls)}
	hardLimit := codingIterBudgetHardLimit(ctx)
	if result.Count >= hardLimit {
		log.Printf("[coding-budget] hard limit reached: %d consecutive coding iterations, force-returning (iter=%d max=%d)", result.Count, iteration, hardLimit)
		if phase != nil {
			phase.Stage = agentStageFinalize
		}
		finalResp := &IMAgentResponse{Text: fmt.Sprintf("Coding execution reached the %d-iteration limit. Completed work has been saved. Send 'continue' to keep going.", hardLimit)}
		if voiceData != "" {
			finalResp.VoiceData = voiceData
			finalResp.VoiceFileName = voiceFileName
			finalResp.VoiceMimeType = voiceMimeType
		}
		attachLLMTelemetry(finalResp)
		attachPendingVisibleArtifacts(finalResp)
		h.saveConversationHistoryTimed(userID, history, finalResp)
		result.Response = finalResp
		return result
	}
	if result.Count == codingIterBudgetSoft {
		log.Printf("[coding-budget] soft limit reached: %d consecutive coding iterations, injecting progress reminder (iter=%d)", result.Count, iteration)
		systemMessagesStart := len(conversation)
		conversation = append(conversation, map[string]string{
			"role":    "system",
			"content": fmt.Sprintf("[system hint] You have executed %d consecutive coding iterations. Finish the current file, report progress, and wait for user confirmation before continuing.", result.Count),
		})
		recordSystemMessages(systemMessagesStart, conversation)
		result.Conversation = conversation
	}
	return result
}

func codingIterBudgetHardLimit(ctx *LoopContext) int {
	if ctx != nil {
		limit := ctx.MaxIterations()
		if limit > 0 {
			return config.EffectiveMaxIterations(limit)
		}
	}
	return config.MaxAgentIterationsCap
}

func nextCodingIterationCount(current int, toolCalls []llm.ToolCall) int {
	codingToolsThisIter := 0
	for _, tc := range toolCalls {
		if classifyAgentToolKind(tc.Function.Name).IsCodingIterationTool() {
			codingToolsThisIter++
		}
	}
	if len(toolCalls) > 0 && codingToolsThisIter*100/len(toolCalls) >= 80 {
		return current + 1
	}
	if len(toolCalls) == 0 {
		return current
	}
	return 0
}
