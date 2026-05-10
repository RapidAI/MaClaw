package main

import (
	"log"
	"strings"
)

const maxLengthContinuations = 3

type agentLoopTextContinuationResult struct {
	Conversation []interface{}
	ContinueLoop bool
}

func handleAgentLoopTextContinuation(
	iteration int,
	finishReason string,
	msgContent string,
	phase *agentLoopPhase,
	lengthContinuationBuf *strings.Builder,
	conversation []interface{},
	recordSystemMessages func(int, []interface{}),
) agentLoopTextContinuationResult {
	result := agentLoopTextContinuationResult{Conversation: conversation}
	isTruncated, truncationReason := shouldContinueTextOutput(finishReason, msgContent)
	if isTruncated {
		log.Printf("[agent-loop] text continuation requested: reason=%s runeLen=%d finish_reason=%q",
			truncationReason, len([]rune(strings.TrimRight(msgContent, " \t\r\n"))), finishReason)
	}
	if !isTruncated || phase == nil || phase.LengthContinuations >= maxLengthContinuations {
		return result
	}

	phase.LengthContinuations++
	phase.ConsecutiveNoTool = 0
	lengthContinuationBuf.WriteString(msgContent)
	log.Printf("[agent-loop] text continuation on text-only response (reason=%s, continuation %d/%d, iter=%d, textLen=%d, accumulated=%d), injecting continuation prompt",
		truncationReason, phase.LengthContinuations, maxLengthContinuations, iteration, len(msgContent), lengthContinuationBuf.Len())
	systemMessagesStart := len(conversation)
	conversation = append(conversation, map[string]string{
		"role":    "system",
		"content": "[system hint] Your output was truncated. Continue from the cutoff point without repeating previous content.",
	})
	recordSystemMessages(systemMessagesStart, conversation)
	result.Conversation = conversation
	result.ContinueLoop = true
	return result
}
