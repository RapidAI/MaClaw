package main

import (
	"context"
	"log"
	"net/http"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

type llmContextTrimRetryResult struct {
	Response     *llm.Response
	Err          error
	Conversation []interface{}
	Usage        llmUsageSnapshot
}

func (h *IMMessageHandler) retryLLMRequestAfterContextWindowExceeded(
	reqCtx context.Context,
	cfg corelib.MaclawLLMConfig,
	conversation []interface{},
	tools []map[string]interface{},
	httpClient *http.Client,
	onToken llm.TokenCallback,
	streamDoneCallback func(),
	err error,
) llmContextTrimRetryResult {
	result := llmContextTrimRetryResult{Err: err, Conversation: conversation}
	if !classifyLLMRetryError(err).ContextWindowExceeded() {
		return result
	}
	const maxContextTrimRetries = 5
	for ctxTrimRetry := 0; ctxTrimRetry < maxContextTrimRetries; ctxTrimRetry++ {
		removed := false
		for ci := 0; ci < len(conversation); ci++ {
			r := msgRole(conversation[ci])
			if r != "system" {
				conversation = append(conversation[:ci], conversation[ci+1:]...)
				removed = true
				log.Printf("[agent-loop] context window exceeded, removed entry at %d (role=%s), retry %d/%d",
					ci, r, ctxTrimRetry+1, maxContextTrimRetries)
				break
			}
		}
		if !removed {
			break
		}
		retryMetrics := &llmStreamMetrics{}
		resp, retryErr := h.doLLMRequestStream(reqCtx, cfg, conversation, tools, httpClient, onToken, retryMetrics)
		result.Response = resp
		result.Err = retryErr
		result.Conversation = conversation
		if retryErr == nil {
			if streamDoneCallback != nil {
				streamDoneCallback()
			}
			result.Usage = h.recordLLMUsageSnapshot("context_trim_retry", cfg, resp, conversation)
			return result
		}
		if !classifyLLMRetryError(retryErr).ContextWindowExceeded() {
			return result
		}
	}
	result.Conversation = conversation
	return result
}
