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

	attempt := func() (done bool) {
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
			return true
		}
		return !classifyLLMRetryError(retryErr).ContextWindowExceeded()
	}

	// First do a real compaction pass: trimConversation keeps tool-call groups
	// intact and injects an LLM handoff summary of the dropped history, so an
	// overflow during a long-running task does not silently delete the task
	// context message-by-message (the previous behavior below remains as the
	// last-resort fallback). The summarizer uses the same cfg/httpClient as the
	// failed request. Compaction is judged by tokens, not message count:
	// content-only truncation (truncateAssistantContent) keeps the length.
	beforeTokens := estimateConversationTokens(conversation)
	compacted := trimConversation(conversation, cfg.EffectiveContextTokens(), estimateToolsTokens(tools), guardedCompactionSummarizer(cfg, httpClient))
	if estimateConversationTokens(compacted) < beforeTokens {
		conversation = compacted
		afterTokens := estimateConversationTokens(conversation)
		log.Printf("[agent-loop] context window exceeded, compacted conversation %d->%d tokens (%d msgs) before retry",
			beforeTokens, afterTokens, len(conversation))
		if attempt() {
			return result
		}
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
		if attempt() {
			return result
		}
	}
	result.Conversation = conversation
	return result
}
