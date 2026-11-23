package guiapp

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
		resp, retryErr := h.doLLMRequestStream(reqCtx, cfg, llmEndpointCategoryMainStream, conversation, tools, httpClient, onToken, retryMetrics)
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
	retryLimit := cfg.EffectiveContextTokens()
	for ctxTrimRetry := 0; ctxTrimRetry < maxContextTrimRetries; ctxTrimRetry++ {
		before := estimateConversationTokens(conversation)
		retryLimit = retryLimit * 3 / 4
		if retryLimit < 4000 {
			retryLimit = 4000
		}
		next := trimConversation(conversation, retryLimit, estimateToolsTokens(tools), nil)
		after := estimateConversationTokens(next)
		if after >= before {
			break
		}
		conversation = next
		log.Printf("[agent-loop] context window exceeded, group-trimmed conversation %d->%d tokens, retry %d/%d",
			before, after, ctxTrimRetry+1, maxContextTrimRetries)
		if attempt() {
			return result
		}
	}
	result.Conversation = conversation
	return result
}
