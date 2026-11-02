package guiapp

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
	// The reasoning counter refills in this same loop. A hard stop here used
	// to end the turn and open another session, which repeated the task.
	if result.Count == codingIterBudgetSoft {
		log.Printf("[coding-budget] soft limit reached: %d consecutive coding iterations, injecting progress reminder (iter=%d)", result.Count, iteration)
		systemMessagesStart := len(conversation)
		conversation = append(conversation, map[string]string{
			"role":    "system",
			"content": fmt.Sprintf("[system hint] You have executed %d consecutive coding iterations. Keep working on the same task. Do not ask the user to send another message.", result.Count),
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
