package main

// LLM HTTP client: OpenAI-compatible and Anthropic Messages API request/response handling.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// doLLMRequest sends a chat completion request to the configured LLM.
// Supports both OpenAI-compatible and Anthropic Messages API protocols.
// The httpClient parameter selects which connection pool to use (chat vs background).
func (h *IMMessageHandler) doLLMRequest(cfg corelib.MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}, httpClient *http.Client) (*llm.Response, error) {
	return h.doLLMRequestWithContext(context.Background(), cfg, messages, tools, httpClient)
}

func (h *IMMessageHandler) doLLMRequestWithContext(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}, httpClient *http.Client) (*llm.Response, error) {
	ctx = llm.WithRequestTraceIfMissing(ctx, "im_llm")
	if cfg.IsResponsesAPI() {
		return h.doResponsesAPILLMRequestWithContext(ctx, cfg, messages, tools, httpClient)
	}
	if cfg.Protocol == "anthropic" {
		return h.doAnthropicLLMRequestWithContext(ctx, cfg, messages, tools, httpClient)
	}
	return h.doOpenAILLMRequestWithContext(ctx, cfg, messages, tools, httpClient)
}

func (h *IMMessageHandler) doOpenAILLMRequest(cfg corelib.MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}, httpClient *http.Client) (*llm.Response, error) {
	return h.doOpenAILLMRequestWithContext(context.Background(), cfg, messages, tools, httpClient)
}

func (h *IMMessageHandler) doOpenAILLMRequestWithContext(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}, httpClient *http.Client) (*llm.Response, error) {
	ctx = llm.WithRequestTraceIfMissing(ctx, "im_openai")
	lease, trace, acquireErr := acquireLLMSchedulerLease(ctx)
	if acquireErr != nil {
		return nil, acquireErr
	}
	defer lease.Release()
	scheduledCtx, scheduledCancel := context.WithCancel(ctx)
	lease.SetCancel(scheduledCancel)
	defer scheduledCancel()
	requestFn := llm.DoOpenAIRequest
	if h.app != nil {
		requestFn = h.app.cachedOpenAIRequest
	}
	resp, err := requestFn(scheduledCtx, cfg, messages, tools, httpClient)
	globalLLMScheduler.ObserveResult(trace, err)
	if err != nil {
		// Re-wrap with dumpLLMContext for HTTP 500 context dump support.
		// DoOpenAIRequest returns "HTTP %d: ..." errors; extract status if 500.
		if isLLMHTTPStatusError(err, http.StatusInternalServerError) {
			data, _ := json.Marshal(map[string]interface{}{
				"model": cfg.Model, "messages": messages, "tools": tools,
			})
			return nil, dumpLLMContext(500, err.Error(), data, h.getTempDir())
		}
		if friendlyMsg, ok := classifyOpenAICompatibleHTTPError(err, cfg.ProviderName); ok {
			return nil, fmt.Errorf("%s", friendlyMsg)
		}
		return nil, err
	}
	return resp, nil
}

// doAnthropicLLMRequest sends a request using the Anthropic Messages API protocol
// and converts the response to the internal llm.Response format for compatibility.
func (h *IMMessageHandler) doAnthropicLLMRequest(cfg corelib.MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}, httpClient *http.Client) (*llm.Response, error) {
	return h.doAnthropicLLMRequestWithContext(context.Background(), cfg, messages, tools, httpClient)
}

func (h *IMMessageHandler) doAnthropicLLMRequestWithContext(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}, httpClient *http.Client) (*llm.Response, error) {
	ctx = llm.WithRequestTraceIfMissing(ctx, "im_anthropic")
	lease, trace, acquireErr := acquireLLMSchedulerLease(ctx)
	if acquireErr != nil {
		return nil, acquireErr
	}
	defer lease.Release()
	scheduledCtx, scheduledCancel := context.WithCancel(ctx)
	lease.SetCancel(scheduledCancel)
	defer scheduledCancel()
	if h.app != nil {
		resp, err := h.app.cachedAnthropicRequest(scheduledCtx, cfg, messages, tools, httpClient)
		globalLLMScheduler.ObserveResult(trace, err)
		return resp, err
	}
	endpoint, data, err := llm.BuildAnthropicMessagesRequestData(cfg, messages, llm.AnthropicMessagesRequestOptions{Stream: false, Tools: tools})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(scheduledCtx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", cfg.UserAgent())
	req.Header.Set("anthropic-version", "2023-06-01")
	corelib.SetAnthropicAuthHeaders(req, cfg.Key)
	corelib.SetCodeGenClientNameHeaderIfNeededWithName(req, cfg.UserAgent())

	resp, err := httpClient.Do(req)
	globalLLMScheduler.ObserveResult(trace, err)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	parsed, err := llm.ParseNonStreamAnthropicResponse(resp)
	globalLLMScheduler.ObserveResult(trace, err)
	return parsed, err
}

// ---------------------------------------------------------------------------
// Shared Anthropic message/tool conversion helpers
// ---------------------------------------------------------------------------

// convertToAnthropicMessages converts OpenAI-style conversation messages
// into Anthropic Messages API format, separating the system prompt.
func convertToAnthropicMessages(messages []interface{}) llm.AnthropicConvertedMessages {
	return llm.ConvertToAnthropicMessages(messages)
}

// needsSystemMerge returns true for providers that do not support the "system"
// role in the messages array (e.g. MiniMax). For these providers we merge the
// system content into the first user message instead.
func needsSystemMerge(cfg corelib.MaclawLLMConfig) bool {
	return corelib.NeedsSystemMerge(cfg)
}

// mergeSystemIntoUser extracts system messages and prepends their content to
// the first user message. Returns a new slice; the original is not modified.
func mergeSystemIntoUser(messages []interface{}) []interface{} {
	return corelib.MergeSystemIntoUser(messages)
}

// convertToAnthropicTools converts OpenAI-style tool definitions to Anthropic format.
func convertToAnthropicTools(tools []map[string]interface{}) []map[string]interface{} {
	var anthropicTools []map[string]interface{}
	for _, t := range tools {
		fn, _ := t["function"].(map[string]interface{})
		if fn == nil {
			continue
		}
		at := map[string]interface{}{"name": fn["name"]}
		if desc, ok := fn["description"]; ok {
			at["description"] = desc
		}
		if params, ok := fn["parameters"]; ok {
			at["input_schema"] = params
		}
		anthropicTools = append(anthropicTools, at)
	}
	return anthropicTools
}
