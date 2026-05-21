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
	cacheRead, cacheWrite := deriveCacheTokens(resp)
	h.accumulateLLMTokenUsageWithCache(providerName, input, output, cacheRead, cacheWrite)
	// OpenHuman-inspired: record cost for budget tracking
	if input > 0 || output > 0 {
		model := h.getMaclawLLMConfig().Model
		h.recordLLMCost(model, input, output)
	}
	return llmUsageSnapshot{Input: input, Output: output}
}

func deriveCacheTokens(resp *llm.Response) (int, int) {
	if resp == nil || resp.Usage == nil {
		return 0, 0
	}
	return resp.Usage.CachedInputTokens, resp.Usage.CacheWriteTokens
}
