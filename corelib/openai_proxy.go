package corelib

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

// OpenAIProxyConfig holds the upstream LLM configuration for the proxy.
type OpenAIProxyConfig struct {
	URL      string // upstream base URL (e.g. "https://open.bigmodel.cn/api/anthropic")
	Key      string // upstream API key
	Model    string // model name to use
	Protocol string // "" or "openai" or "anthropic"
	WireAPI  string // "" or "chat" or "responses" or "responses-ws"
}

// OpenAIProxy is a local HTTP proxy that provides an OpenAI-compatible
// /v1/chat/completions endpoint, forwarding requests to the configured
// upstream LLM provider with protocol conversion as needed.
type OpenAIProxy struct {
	config   OpenAIProxyConfig
	server   *http.Server
	listener net.Listener
	port     int
	client   *http.Client
}

// NewOpenAIProxy creates a new proxy instance with the given config.
func NewOpenAIProxy(cfg OpenAIProxyConfig) *OpenAIProxy {
	return &OpenAIProxy{
		config: cfg,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// Start binds to a random port on 127.0.0.1 and begins serving.
// Returns the allocated port number or an error.
func (p *OpenAIProxy) Start() (int, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", p.handleChatCompletions)

	var err error
	p.listener, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("openai proxy: listen: %w", err)
	}

	p.port = p.listener.Addr().(*net.TCPAddr).Port
	p.server = &http.Server{Handler: mux}

	log.Printf("[openai-proxy] listening on 127.0.0.1:%d (upstream: %s, protocol: %s, model: %s)",
		p.port, p.config.URL, p.config.Protocol, p.config.Model)

	go func() {
		if err := p.server.Serve(p.listener); err != nil && err != http.ErrServerClosed {
			log.Printf("[openai-proxy] serve error: %v", err)
		}
	}()

	return p.port, nil
}

// Stop gracefully shuts down the proxy server with a 5-second deadline.
func (p *OpenAIProxy) Stop() error {
	if p.server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	log.Printf("[openai-proxy] shutting down on port %d", p.port)
	return p.server.Shutdown(ctx)
}

// Port returns the port the proxy is listening on.
func (p *OpenAIProxy) Port() int {
	return p.port
}

// NeedsOpenAIProxy determines if a skill needs the local OpenAI proxy.
// Returns true if required_env contains OPENAI_API_KEY and the user
// has not provided OPENAI_API_KEY or OPENAI_BASE_URL via extra_env.
func NeedsOpenAIProxy(requiredEnv []string, extraEnv map[string]string) bool {
	// Check if requiredEnv contains OPENAI_API_KEY (case-sensitive)
	found := false
	for _, env := range requiredEnv {
		if env == "OPENAI_API_KEY" {
			found = true
			break
		}
	}
	if !found {
		return false
	}

	// Check if user has provided OPENAI_API_KEY or OPENAI_BASE_URL via extraEnv
	if v, ok := extraEnv["OPENAI_API_KEY"]; ok && v != "" {
		return false
	}
	if v, ok := extraEnv["OPENAI_BASE_URL"]; ok && v != "" {
		return false
	}

	return true
}

// routeProtocol determines which upstream protocol to use based on config.
// Returns "anthropic", "responses", or "openai" (default).
func (p *OpenAIProxy) routeProtocol() string {
	if strings.EqualFold(p.config.Protocol, "anthropic") {
		return "anthropic"
	}
	w := strings.ToLower(strings.TrimSpace(p.config.WireAPI))
	if w == "responses" || w == "responses-ws" {
		return "responses"
	}
	return "openai"
}

// handleChatCompletions is the main HTTP handler for the proxy.
// It validates the request path and method, parses the JSON body,
// and routes to the appropriate forward function based on protocol.
func (p *OpenAIProxy) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// 1. Validate path
	if r.URL.Path != "/v1/chat/completions" {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "Not Found",
				"type":    "invalid_request_error",
			},
		})
		return
	}

	// 2. Validate method
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "Method Not Allowed",
				"type":    "invalid_request_error",
			},
		})
		return
	}

	// 3. Read and parse body (limit to 10MB to prevent OOM from malicious input)
	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": fmt.Sprintf("invalid JSON: %s", err.Error()),
				"type":    "invalid_request_error",
			},
		})
		return
	}

	var body map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": fmt.Sprintf("invalid JSON: %s", err.Error()),
				"type":    "invalid_request_error",
			},
		})
		return
	}

	// 4. Route based on protocol
	protocol := p.routeProtocol()
	originalModel, _ := body["model"].(string) // capture before forward mutates body
	var respBody []byte
	var statusCode int
	var forwardErr error

	switch protocol {
	case "anthropic":
		respBody, statusCode, forwardErr = p.forwardAnthropic(body)
	case "responses":
		respBody, statusCode, forwardErr = p.forwardResponses(body)
	default:
		respBody, statusCode, forwardErr = p.forwardOpenAI(body)
	}

	// 5. Handle forward error
	if forwardErr != nil {
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": fmt.Sprintf("upstream provider unreachable: %s", forwardErr.Error()),
				"type":    "server_error",
			},
		})
		return
	}

	// 6. Write response
	w.WriteHeader(statusCode)
	w.Write(respBody)

	// 7. Log at debug level
	log.Printf("[openai-proxy] protocol=%s url=%s model=%s status=%d", protocol, p.config.URL, originalModel, statusCode)
}

// forwardOpenAI forwards the request directly to an OpenAI-compatible upstream.
// It replaces the model field, forces stream: false, and forwards the response as-is.
func (p *OpenAIProxy) forwardOpenAI(body map[string]interface{}) ([]byte, int, error) {
	// Work on a shallow copy to avoid mutating the caller's map
	fwd := make(map[string]interface{}, len(body))
	for k, v := range body {
		fwd[k] = v
	}

	// Replace model with config value
	fwd["model"] = p.config.Model

	// Force stream to false
	fwd["stream"] = false

	// Marshal body back to JSON
	jsonBody, err := json.Marshal(fwd)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal request body: %w", err)
	}

	// Construct upstream URL
	upstreamURL := strings.TrimRight(p.config.URL, "/") + "/v1/chat/completions"

	// Create HTTP request
	req, err := http.NewRequest("POST", upstreamURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.config.Key)

	// Execute request
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("read response body: %w", err)
	}

	// Forward response body and status code as-is
	return respBody, resp.StatusCode, nil
}

// openaiToAnthropic converts an OpenAI Chat Completions request body
// to an Anthropic Messages API request body.
func openaiToAnthropic(body map[string]interface{}, model string) map[string]interface{} {
	anthropicReq := map[string]interface{}{
		"model":  model,
		"stream": false,
	}

	// Extract max_tokens from body, default to 4096
	if mt, ok := body["max_tokens"]; ok && mt != nil {
		anthropicReq["max_tokens"] = mt
	} else {
		anthropicReq["max_tokens"] = 4096
	}

	// Extract messages array
	messages, _ := body["messages"].([]interface{})
	if messages == nil {
		anthropicReq["messages"] = []interface{}{}
		return anthropicReq
	}

	// Separate system messages from non-system messages
	var systemParts []string
	var nonSystemMessages []interface{}

	for _, m := range messages {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)

		if role == "system" {
			if content != "" {
				systemParts = append(systemParts, content)
			}
		} else {
			nonSystemMessages = append(nonSystemMessages, map[string]interface{}{
				"role":    role,
				"content": content,
			})
		}
	}

	// Set system field if there are system messages
	if len(systemParts) > 0 {
		anthropicReq["system"] = strings.Join(systemParts, "\n")
	}

	// Set messages (non-system)
	if nonSystemMessages == nil {
		nonSystemMessages = []interface{}{}
	}
	anthropicReq["messages"] = nonSystemMessages

	return anthropicReq
}

// anthropicToOpenAI converts an Anthropic Messages API response body
// to an OpenAI Chat Completions response body.
func anthropicToOpenAI(resp map[string]interface{}, model string) map[string]interface{} {
	// Extract and concatenate text content blocks
	var contentBuilder strings.Builder
	if contentArr, ok := resp["content"].([]interface{}); ok {
		for _, block := range contentArr {
			blockMap, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			blockType, _ := blockMap["type"].(string)
			if blockType == "text" {
				text, _ := blockMap["text"].(string)
				contentBuilder.WriteString(text)
			}
		}
	}
	contentText := contentBuilder.String()

	// Map stop_reason to finish_reason
	stopReason, _ := resp["stop_reason"].(string)
	var finishReason string
	switch stopReason {
	case "end_turn":
		finishReason = "stop"
	case "max_tokens":
		finishReason = "length"
	default:
		finishReason = "stop"
	}

	// Map usage fields
	var promptTokens, completionTokens float64
	if usage, ok := resp["usage"].(map[string]interface{}); ok {
		promptTokens, _ = usage["input_tokens"].(float64)
		completionTokens, _ = usage["output_tokens"].(float64)
	}
	totalTokens := promptTokens + completionTokens

	// Extract ID or use default
	id, _ := resp["id"].(string)
	if id == "" {
		id = "chatcmpl-proxy"
	}

	// Build OpenAI response
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
				"finish_reason": finishReason,
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      totalTokens,
		},
	}
}

// forwardAnthropic converts OpenAI request to Anthropic format, sends it,
// and converts the response back to OpenAI format.
func (p *OpenAIProxy) forwardAnthropic(body map[string]interface{}) ([]byte, int, error) {
	// 1. Convert request to Anthropic format
	anthropicReq := openaiToAnthropic(body, p.config.Model)

	// 2. Marshal to JSON
	jsonBody, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal anthropic request: %w", err)
	}

	// 3. Construct URL using AnthropicMessagesEndpoint
	upstreamURL := AnthropicMessagesEndpoint(p.config.URL)

	// 4. Create POST request
	req, err := http.NewRequest("POST", upstreamURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}

	// 5. Set headers
	req.Header.Set("Content-Type", "application/json")
	SetAnthropicAuthHeaders(req, p.config.Key)
	req.Header.Set("anthropic-version", "2023-06-01")

	// 6. Execute request
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	// 7. Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("read response body: %w", err)
	}

	// 8. On upstream 4xx/5xx: wrap error in OpenAI format
	if resp.StatusCode >= 400 {
		// Truncate error body to prevent oversized error messages
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

	// 9. Parse Anthropic response and convert to OpenAI format
	var respMap map[string]interface{}
	if err := json.Unmarshal(respBody, &respMap); err != nil {
		return nil, 0, fmt.Errorf("parse anthropic response: %w", err)
	}

	openaiResp := anthropicToOpenAI(respMap, p.config.Model)
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

	// Extract messages array and map to input field
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
	// Extract and concatenate text content from output items
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
			// Extract content array from message item
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

	// Map usage fields
	var promptTokens, completionTokens float64
	if usage, ok := resp["usage"].(map[string]interface{}); ok {
		promptTokens, _ = usage["input_tokens"].(float64)
		completionTokens, _ = usage["output_tokens"].(float64)
	}
	totalTokens := promptTokens + completionTokens

	// Extract ID or use default
	id, _ := resp["id"].(string)
	if id == "" {
		id = "chatcmpl-proxy"
	}

	// Build OpenAI response
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

// forwardResponses converts OpenAI request to Responses API format, sends it,
// and converts the response back to OpenAI format.
func (p *OpenAIProxy) forwardResponses(body map[string]interface{}) ([]byte, int, error) {
	// 1. Convert request to Responses API format
	responsesReq := openaiToResponses(body, p.config.Model)

	// 2. Marshal to JSON
	jsonBody, err := json.Marshal(responsesReq)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal responses request: %w", err)
	}

	// 3. Construct URL
	upstreamURL := strings.TrimRight(p.config.URL, "/") + "/v1/responses"

	// 4. Create POST request
	req, err := http.NewRequest("POST", upstreamURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}

	// 5. Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.config.Key)

	// 6. Execute request
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	// 7. Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("read response body: %w", err)
	}

	// 8. On upstream 4xx/5xx: wrap error in OpenAI format
	if resp.StatusCode >= 400 {
		// Truncate error body to prevent oversized error messages
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

	// 9. Parse Responses API response and convert to OpenAI format
	var respMap map[string]interface{}
	if err := json.Unmarshal(respBody, &respMap); err != nil {
		return nil, 0, fmt.Errorf("parse responses api response: %w", err)
	}

	openaiResp := responsesToOpenAI(respMap, p.config.Model)
	data, err := json.Marshal(openaiResp)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal openai response: %w", err)
	}

	return data, http.StatusOK, nil
}
