package corelib

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ForwardOpenAICompatRequest forwards an OpenAI chat/completions style request
// to the configured upstream and returns an OpenAI-compatible JSON response.
// It centralizes protocol conversion in corelib so business layers only handle
// provider selection and persistence.
func ForwardOpenAICompatRequest(ctx context.Context, cfg MaclawLLMConfig, body map[string]interface{}, client *http.Client, responseModel string) ([]byte, int, error) {
	if client == nil {
		client = http.DefaultClient
	}

	// Shallow-copy body and remove streaming-only fields that are meaningless
	// when all forward paths force stream:false.
	clean := make(map[string]interface{}, len(body))
	for k, v := range body {
		clean[k] = v
	}
	delete(clean, "stream_options")
	delete(clean, "provider")
	delete(clean, "model_provider")

	wireAPI := strings.ToLower(strings.TrimSpace(cfg.WireAPI))
	switch {
	case wireAPI == "responses" || wireAPI == "responses-ws":
		// Responses API has its own message format — sanitize tool messages
		// before protocol conversion since the converter doesn't handle them.
		if msgs, ok := clean["messages"]; ok {
			clean["messages"] = sanitizeToolMessages(msgs)
		}
		return forwardResponsesCompat(ctx, cfg, clean, client, responseModel)
	case strings.EqualFold(strings.TrimSpace(cfg.Protocol), "anthropic"):
		// Anthropic Messages API doesn't support OpenAI tool message format —
		// sanitize before protocol conversion.
		if msgs, ok := clean["messages"]; ok {
			clean["messages"] = sanitizeToolMessages(msgs)
		}
		return forwardAnthropicCompatRequest(ctx, cfg, clean, client, responseModel)
	default:
		// OpenAI-compatible path: pass through as-is. Most OpenAI-compatible
		// providers (GLM, GPT, Kimi, etc.) support tool calling natively.
		// If a provider rejects role:"tool" (e.g. some DeepSeek endpoints),
		// the Hub's failover mechanism will try the next provider.
		return forwardOpenAICompatRequest(ctx, cfg, clean, client, responseModel)
	}
}

func forwardOpenAICompatRequest(ctx context.Context, cfg MaclawLLMConfig, body map[string]interface{}, client *http.Client, responseModel string) ([]byte, int, error) {
	fwd := make(map[string]interface{}, len(body))
	for k, v := range body {
		fwd[k] = v
	}
	fwd["model"] = cfg.Model
	fwd["stream"] = false

	jsonBody, err := json.Marshal(fwd)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal request body: %w", err)
	}
	upstreamURL := openAIChatCompletionsEndpoint(cfg.URL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", cfg.UserAgent())
	if cfg.Key != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Key)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("read response body: %w", err)
	}
	if strings.TrimSpace(responseModel) != "" {
		respBody = OverrideOpenAIResponseModel(respBody, responseModel)
	}
	return respBody, resp.StatusCode, nil
}

func forwardAnthropicCompatRequest(ctx context.Context, cfg MaclawLLMConfig, body map[string]interface{}, client *http.Client, responseModel string) ([]byte, int, error) {
	anthropicReq := openaiToAnthropic(body, cfg.Model)
	jsonBody, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal anthropic request: %w", err)
	}
	upstreamURL := AnthropicMessagesEndpoint(cfg.URL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", cfg.UserAgent())
	SetAnthropicAuthHeaders(req, cfg.Key)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode >= 400 {
		errBodyStr := string(respBody)
		if len(errBodyStr) > 1024 {
			errBodyStr = errBodyStr[:1024] + "...(truncated)"
		}
		errResp := map[string]interface{}{
			"error": map[string]interface{}{
				"message": fmt.Sprintf("upstream error (HTTP %d): %s", resp.StatusCode, errBodyStr),
				"type":    "server_error",
			},
		}
		data, _ := json.Marshal(errResp)
		return data, resp.StatusCode, nil
	}
	var respMap map[string]interface{}
	if err := json.Unmarshal(respBody, &respMap); err != nil {
		return nil, 0, fmt.Errorf("parse anthropic response: %w", err)
	}
	model := strings.TrimSpace(responseModel)
	if model == "" {
		model = cfg.Model
	}
	openaiResp := anthropicToOpenAI(respMap, model)
	data, err := json.Marshal(openaiResp)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal openai response: %w", err)
	}
	return data, http.StatusOK, nil
}

// openaiToResponses converts an OpenAI Chat Completions request body
// to a Responses API request body.
func openaiToResponses(body map[string]interface{}, model string) map[string]interface{} {
	responsesReq := map[string]interface{}{
		"model":  model,
		"stream": false,
	}
	messages, _ := body["messages"].([]interface{})
	if messages == nil {
		responsesReq["input"] = []interface{}{}
	} else {
		responsesReq["input"] = messages
	}
	return responsesReq
}

// responsesToOpenAI converts a Responses API response body
// to an OpenAI Chat Completions response body.
func responsesToOpenAI(resp map[string]interface{}, model string) map[string]interface{} {
	var contentBuilder strings.Builder
	if outputArr, ok := resp["output"].([]interface{}); ok {
		for _, item := range outputArr {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			itemType, _ := itemMap["type"].(string)
			if itemType != "message" {
				continue
			}
			contentArr, ok := itemMap["content"].([]interface{})
			if !ok {
				continue
			}
			for _, block := range contentArr {
				blockMap, ok := block.(map[string]interface{})
				if !ok {
					continue
				}
				blockType, _ := blockMap["type"].(string)
				if blockType == "output_text" {
					text, _ := blockMap["text"].(string)
					contentBuilder.WriteString(text)
				}
			}
		}
	}
	contentText := contentBuilder.String()
	var promptTokens, completionTokens float64
	if usage, ok := resp["usage"].(map[string]interface{}); ok {
		promptTokens, _ = usage["input_tokens"].(float64)
		completionTokens, _ = usage["output_tokens"].(float64)
	}
	totalTokens := promptTokens + completionTokens
	id, _ := resp["id"].(string)
	if id == "" {
		id = "chatcmpl-proxy"
	}
	return map[string]interface{}{
		"id":     id,
		"object": "chat.completion",
		"model":  model,
		"choices": []interface{}{
			map[string]interface{}{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": contentText,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      totalTokens,
		},
	}
}

func forwardResponsesCompat(ctx context.Context, cfg MaclawLLMConfig, body map[string]interface{}, client *http.Client, responseModel string) ([]byte, int, error) {
	responsesReq := openaiToResponses(body, cfg.Model)
	jsonBody, err := json.Marshal(responsesReq)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal responses request: %w", err)
	}
	upstreamURL := openAIResponsesEndpoint(cfg.URL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", cfg.UserAgent())
	if cfg.Key != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Key)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode >= 400 {
		errBodyStr := string(respBody)
		if len(errBodyStr) > 1024 {
			errBodyStr = errBodyStr[:1024] + "...(truncated)"
		}
		errResp := map[string]interface{}{
			"error": map[string]interface{}{
				"message": fmt.Sprintf("upstream error (HTTP %d): %s", resp.StatusCode, errBodyStr),
				"type":    "server_error",
			},
		}
		data, _ := json.Marshal(errResp)
		return data, resp.StatusCode, nil
	}
	var respMap map[string]interface{}
	if err := json.Unmarshal(respBody, &respMap); err != nil {
		return nil, 0, fmt.Errorf("parse responses api response: %w", err)
	}
	model := strings.TrimSpace(responseModel)
	if model == "" {
		model = cfg.Model
	}
	openaiResp := responsesToOpenAI(respMap, model)
	data, err := json.Marshal(openaiResp)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal openai response: %w", err)
	}
	return data, http.StatusOK, nil
}

// openAIChatCompletionsEndpoint constructs the chat/completions URL from a base URL.
// If the URL already ends with "/v1", it appends "/chat/completions" directly;
// otherwise it appends "/v1/chat/completions".
// This avoids double "/v1" when the base URL is e.g. "https://api.deepseek.com/v1".
func openAIChatCompletionsEndpoint(baseURL string) string {
	return appendV1Path(baseURL, "/chat/completions")
}

// openAIResponsesEndpoint constructs the responses API URL from a base URL.
// If the URL already ends with "/v1", it appends "/responses" directly;
// otherwise it appends "/v1/responses".
func openAIResponsesEndpoint(baseURL string) string {
	return appendV1Path(baseURL, "/responses")
}

// appendV1Path appends a sub-path under /v1 to a base URL.
// If the base URL already ends with "/v1", the sub-path is appended directly;
// otherwise "/v1" is prepended to the sub-path.
// This is the shared logic behind openAIChatCompletionsEndpoint,
// openAIResponsesEndpoint, and AnthropicMessagesEndpoint.
func appendV1Path(baseURL, subPath string) string {
	trimmed := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(trimmed, "/v1") {
		return trimmed + subPath
	}
	return trimmed + "/v1" + subPath
}

// sanitizeToolMessages converts tool-calling messages to plain user/assistant
// format for upstream providers that don't support the OpenAI tool calling
// protocol. Specifically:
//   - role:"tool" messages → role:"user" with content "[Tool Result] ..."
//   - role:"assistant" messages with tool_calls → keep as assistant, replace
//     content with a text summary of the tool calls, remove tool_calls field
//   - Consecutive converted tool results are merged into a single user message
//     to avoid violating user/assistant alternation requirements.
//   - All other messages pass through unchanged.
func sanitizeToolMessages(raw interface{}) interface{} {
	msgs, ok := raw.([]interface{})
	if !ok {
		return raw
	}
	out := make([]interface{}, 0, len(msgs))
	for _, m := range msgs {
		msg, ok := m.(map[string]interface{})
		if !ok {
			out = append(out, m)
			continue
		}
		role, _ := msg["role"].(string)
		switch role {
		case "tool":
			// Convert tool result to user message
			content, _ := msg["content"].(string)
			name, _ := msg["name"].(string)
			label := "[Tool Result"
			if name != "" {
				label += ": " + name
			}
			label += "] "
			newContent := label + content

			// Merge with previous user message if the last output is also
			// a user message (from a prior tool conversion), to avoid
			// consecutive user messages that some APIs reject.
			if len(out) > 0 {
				prev, ok := out[len(out)-1].(map[string]interface{})
				if ok {
					prevRole, _ := prev["role"].(string)
					if prevRole == "user" {
						prevContent, _ := prev["content"].(string)
						prev["content"] = prevContent + "\n" + newContent
						continue
					}
				}
			}
			out = append(out, map[string]interface{}{
				"role":    "user",
				"content": newContent,
			})
		case "assistant":
			toolCalls, hasToolCalls := msg["tool_calls"]
			if !hasToolCalls || toolCalls == nil {
				out = append(out, m)
				continue
			}
			// Convert assistant tool_calls to plain text
			content, _ := msg["content"].(string)
			callSummary := summarizeToolCalls(toolCalls)
			if content != "" && callSummary != "" {
				content = content + "\n" + callSummary
			} else if callSummary != "" {
				content = callSummary
			}
			cleaned := make(map[string]interface{}, len(msg))
			for k, v := range msg {
				if k == "tool_calls" {
					continue
				}
				cleaned[k] = v
			}
			cleaned["content"] = content
			out = append(out, cleaned)
		default:
			out = append(out, m)
		}
	}
	return out
}

// summarizeToolCalls converts a tool_calls array into a human-readable text
// summary for providers that don't support tool calling.
func summarizeToolCalls(raw interface{}) string {
	calls, ok := raw.([]interface{})
	if !ok || len(calls) == 0 {
		return ""
	}
	var b strings.Builder
	for i, c := range calls {
		call, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		fn, _ := call["function"].(map[string]interface{})
		if fn == nil {
			continue
		}
		name, _ := fn["name"].(string)
		args, _ := fn["arguments"].(string)
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("[Tool Call: ")
		b.WriteString(name)
		b.WriteString("]")
		if args != "" && args != "{}" {
			// Truncate very long arguments to avoid bloating context
			if len(args) > 200 {
				args = args[:200] + "..."
			}
			b.WriteString(" ")
			b.WriteString(args)
		}
	}
	return b.String()
}

// OverrideOpenAIResponseModel rewrites the top-level model field in an
// OpenAI-compatible JSON response when present.
func OverrideOpenAIResponseModel(body []byte, model string) []byte {
	if strings.TrimSpace(model) == "" {
		return body
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	payload["model"] = model
	data, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return data
}
