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
	wireAPI := strings.ToLower(strings.TrimSpace(cfg.WireAPI))
	switch {
	case wireAPI == "responses" || wireAPI == "responses-ws":
		return forwardResponsesCompat(ctx, cfg, body, client, responseModel)
	case strings.EqualFold(strings.TrimSpace(cfg.Protocol), "anthropic"):
		return forwardAnthropicCompatRequest(ctx, cfg, body, client, responseModel)
	default:
		return forwardOpenAICompatRequest(ctx, cfg, body, client, responseModel)
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
	upstreamURL := strings.TrimRight(cfg.URL, "/") + "/v1/chat/completions"
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
	upstreamURL := strings.TrimRight(cfg.URL, "/") + "/v1/responses"
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
