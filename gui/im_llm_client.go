package main

// LLM HTTP client: OpenAI-compatible and Anthropic Messages API request/response handling.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// doLLMRequest sends a chat completion request to the configured LLM.
// Supports both OpenAI-compatible and Anthropic Messages API protocols.
// The httpClient parameter selects which connection pool to use (chat vs background).
func (h *IMMessageHandler) doLLMRequest(cfg corelib.MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}, httpClient *http.Client) (*llm.Response, error) {
	if cfg.IsResponsesAPI() {
		return h.doResponsesAPILLMRequest(cfg, messages, tools, httpClient)
	}
	if cfg.Protocol == "anthropic" {
		return h.doAnthropicLLMRequest(cfg, messages, tools, httpClient)
	}
	return h.doOpenAILLMRequest(cfg, messages, tools, httpClient)
}

func (h *IMMessageHandler) doOpenAILLMRequest(cfg corelib.MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}, httpClient *http.Client) (*llm.Response, error) {
	ctx := context.Background()
	resp, err := llm.DoOpenAIRequest(ctx, cfg, messages, tools, httpClient)
	if err != nil {
		// Re-wrap with dumpLLMContext for HTTP 500 context dump support.
		// DoOpenAIRequest returns "HTTP %d: ..." errors; extract status if 500.
		if strings.Contains(err.Error(), "HTTP 500") {
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
	endpoint := corelib.AnthropicMessagesEndpoint(cfg.URL)

	converted := convertToAnthropicMessages(messages)

	reqBody := map[string]interface{}{
		"model":      cfg.Model,
		"messages":   converted.Messages,
		"max_tokens": 4096,
	}
	if converted.SystemText != "" {
		reqBody["system"] = converted.SystemText
	}

	// Convert OpenAI-style tools to Anthropic tool format
	if len(tools) > 0 {
		if at := convertToAnthropicTools(tools); len(at) > 0 {
			reqBody["tools"] = at
		}
	}

	data, _ := json.Marshal(reqBody)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", cfg.UserAgent())
	req.Header.Set("anthropic-version", "2023-06-01")
	corelib.SetAnthropicAuthHeaders(req, cfg.Key)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return llm.ParseNonStreamAnthropicResponse(resp)
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
