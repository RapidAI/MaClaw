package main

import (
	"log"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

type llmUsageSnapshot struct {
	Input  int
	Output int
}

func (h *IMMessageHandler) recordLLMUsageSnapshot(label string, resp *llm.Response, conversation []interface{}) llmUsageSnapshot {
	if resp == nil {
		return llmUsageSnapshot{}
	}
	input, output := deriveLLMTokenUsage(resp, conversation)
	providerName := h.getMaclawLLMProviders().Current
	log.Printf("[LLM] usage %s provider=%q input=%d output=%d usage_nil=%t choices=%d", label, providerName, input, output, resp.Usage == nil, len(resp.Choices))
	if len(resp.Choices) > 0 {
		log.Printf("[LLM] finish_reason=%q content_len=%d tool_calls=%d", resp.Choices[0].FinishReason, len(resp.Choices[0].Message.Content), len(resp.Choices[0].Message.ToolCalls))
	}
	h.accumulateLLMTokenUsage(providerName, input, output)
	return llmUsageSnapshot{Input: input, Output: output}
}
