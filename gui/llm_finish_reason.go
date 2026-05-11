package main

import "strings"

type llmFinishReason string

const (
	llmFinishReasonUnknown   llmFinishReason = ""
	llmFinishReasonStop      llmFinishReason = "stop"
	llmFinishReasonLength    llmFinishReason = "length"
	llmFinishReasonToolCalls llmFinishReason = "tool_calls"
)

func normalizeLLMFinishReason(value string) llmFinishReason {
	switch llmFinishReason(strings.ToLower(strings.TrimSpace(value))) {
	case llmFinishReasonStop:
		return llmFinishReasonStop
	case llmFinishReasonLength:
		return llmFinishReasonLength
	case llmFinishReasonToolCalls:
		return llmFinishReasonToolCalls
	default:
		return llmFinishReasonUnknown
	}
}

func (reason llmFinishReason) String() string {
	return string(reason)
}
