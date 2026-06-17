package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

type agentLoopLLMResponseGuardResult struct {
	Response     *llm.Response
	Err          error
	Conversation []interface{}
	Usage        llmUsageSnapshot
	Exit         *IMAgentResponse
}

func (h *IMMessageHandler) guardAgentLoopLLMResponse(
	ctx *LoopContext,
	reqCtx context.Context,
	cfg corelib.MaclawLLMConfig,
	conversation []interface{},
	tools []map[string]interface{},
	httpClient *http.Client,
	onToken llm.TokenCallback,
	streamDoneCallback func(),
	resp *llm.Response,
	err error,
	userID string,
	history []agent.ConversationEntry,
	userText string,
	inFlightLifecycle *imInFlightLifecycle,
) agentLoopLLMResponseGuardResult {
	result := agentLoopLLMResponseGuardResult{
		Response:     resp,
		Err:          err,
		Conversation: conversation,
	}
	if result.Err != nil {
		if ctx.IsCancelled() {
			ctx.SetLoopState(LoopStateStopped)
			result.Exit = h.cancelledExitResponse(userID, history, userText)
			return result
		}

		contextRetry := h.retryLLMRequestAfterContextWindowExceeded(reqCtx, cfg, conversation, tools, httpClient, onToken, streamDoneCallback, err)
		result.Response = contextRetry.Response
		result.Err = contextRetry.Err
		result.Conversation = contextRetry.Conversation
		result.Usage = contextRetry.Usage
		if result.Err != nil {
			inFlightLifecycle.PreserveOnFinish()
			// Try to produce a human-readable error message instead of raw
			// technical details (JSON bodies, full URLs, etc.).
			errMsg := result.Err.Error()
			if friendly, ok := classifyOpenAICompatibleHTTPError(result.Err, cfg.ProviderName); ok && friendly != "" {
				errMsg = friendly
			}
			result.Exit = h.llmErrorExitResponse(userID, history, fmt.Sprintf("LLM request failed: %s [url=%s model=%s protocol=%s]", errMsg, cfg.URL, cfg.Model, cfg.Protocol))
			return result
		}
	}

	if result.Response == nil || len(result.Response.Choices) == 0 {
		log.Printf("[agent-loop] LLM returned 0 choices: url=%s model=%s protocol=%s", cfg.URL, cfg.Model, cfg.Protocol)
		inFlightLifecycle.PreserveOnFinish()
		result.Exit = h.llmErrorExitResponse(userID, history, "LLM returned no choices")
	}
	return result
}
