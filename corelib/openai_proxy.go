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
	"os"
	"path/filepath"
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

	return !hasUserProvidedOpenAIEnv(extraEnv)
}

// NeedsOpenAIProxyAuto is like NeedsOpenAIProxy but also auto-detects
// OpenAI env var usage from skill step commands and script files when
// RequiredEnv is not explicitly declared. This handles skills downloaded
// from ClawHub or other sources that use OPENAI_API_KEY in their scripts
// but don't declare requires_env in their metadata.
//
// Detection order:
//  1. RequiredEnv explicitly declares OPENAI_API_KEY 閳?use proxy
//  2. Step commands reference OPENAI_API_KEY/OPENAI_BASE_URL 閳?use proxy
//  3. Script files (.py, .js, .ts, .sh) in skillDir reference them 閳?use proxy
//
// In all cases, if the user has already provided the env vars via extraEnv
// or the process environment, the proxy is not started.
func NeedsOpenAIProxyAuto(requiredEnv []string, extraEnv map[string]string, steps []NLSkillStep, skillDir string) bool {
	// Fast path: user already provided credentials via extraEnv
	if hasUserProvidedOpenAIEnv(extraEnv) {
		return false
	}

	// Check if skill explicitly declares OPENAI_API_KEY requirement.
	// Checked before the process-level env so that a stale
	// "sk-maclaw-local-proxy" sentinel in os env doesn't prevent
	// proxy startup for skills that genuinely need it.
	explicitlyRequired := false
	for _, env := range requiredEnv {
		if env == "OPENAI_API_KEY" {
			explicitlyRequired = true
			break
		}
	}

	// Check process-level env: if OPENAI_API_KEY is set globally and
	// extraEnv doesn't explicitly override it (e.g. with empty string),
	// the skill can use the existing key directly 閳?no proxy needed.
	// However, ignore the maclaw-internal proxy sentinel ("sk-maclaw-local-proxy")
	// because it points to a proxy that may no longer be running.
	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		if _, explicitlyCleared := extraEnv["OPENAI_API_KEY"]; !explicitlyCleared {
			if v == "sk-maclaw-local-proxy" {
				// Stale leftover from a previous proxy session 閳?ignore it
				// and fall through so a fresh proxy is started.
				log.Printf("[openai-proxy] ignoring stale process env OPENAI_API_KEY=sk-maclaw-local-proxy")
			} else {
				// Real user-provided key in process env 閳?use it directly.
				return false
			}
		}
	}

	if explicitlyRequired {
		return true
	}

	// Layer 2: scan step commands for env var references
	for _, step := range steps {
		if step.Action != "bash" {
			continue
		}
		cmd, _ := step.Params["command"].(string)
		if cmd != "" && containsOpenAIEnvRef(cmd) {
			log.Printf("[openai-proxy] auto-detected OPENAI env usage in step command")
			return true
		}
	}

	// Layer 3: scan script files in skillDir
	if skillDir != "" && scanSkillDirForOpenAIEnv(skillDir) {
		log.Printf("[openai-proxy] auto-detected OPENAI env usage in skill scripts at %s", skillDir)
		return true
	}

	return false
}

// hasUserProvidedOpenAIEnv checks if the user has provided OPENAI_API_KEY
// or OPENAI_BASE_URL via extraEnv with non-empty values.
func hasUserProvidedOpenAIEnv(extraEnv map[string]string) bool {
	if v, ok := extraEnv["OPENAI_API_KEY"]; ok && v != "" {
		return true
	}
	if v, ok := extraEnv["OPENAI_BASE_URL"]; ok && v != "" {
		return true
	}
	return false
}

// openaiEnvPatterns are the env var names we look for in script content.
var openaiEnvPatterns = []string{
	"OPENAI_API_KEY",
	"OPENAI_BASE_URL",
}

// containsOpenAIEnvRef checks if text contains references to OpenAI env vars.
func containsOpenAIEnvRef(text string) bool {
	for _, pat := range openaiEnvPatterns {
		if strings.Contains(text, pat) {
			return true
		}
	}
	return false
}

// scriptExtensions are file extensions that may contain env var references.
var scriptExtensions = map[string]bool{
	".py": true, ".js": true, ".ts": true, ".sh": true,
	".rb": true, ".pl": true, ".php": true, ".go": true,
}

// scanSkillDirForOpenAIEnv scans script files in skillDir for OPENAI_API_KEY
// or OPENAI_BASE_URL references. Scans the top-level directory and one level
// of common subdirectories (scripts/, src/, lib/). Safety limits: max 30 files,
// max 64KB each.
func scanSkillDirForOpenAIEnv(skillDir string) bool {
	// Directories to scan: top-level + common script subdirectories
	dirsToScan := []string{skillDir}
	for _, sub := range []string{"scripts", "src", "lib"} {
		subDir := filepath.Join(skillDir, sub)
		if info, err := os.Stat(subDir); err == nil && info.IsDir() {
			dirsToScan = append(dirsToScan, subDir)
		}
	}

	scanned := 0
	for _, dir := range dirsToScan {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			if !scriptExtensions[ext] {
				continue
			}
			scanned++
			if scanned > 30 {
				return false // safety limit
			}

			info, err := entry.Info()
			if err != nil || info.Size() > 64*1024 {
				continue // skip large files
			}

			data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				continue
			}
			if containsOpenAIEnvRef(string(data)) {
				return true
			}
		}
	}
	return false
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
	upstreamURL := openAIChatCompletionsEndpoint(p.config.URL)

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

	messages, _ := body["messages"].([]interface{})
	converted := convertOpenAIToAnthropicMessages(messages)
	if converted.SystemText != "" {
		anthropicReq["system"] = converted.SystemText
	}
	if converted.Messages == nil {
		converted.Messages = []interface{}{}
	}
	anthropicReq["messages"] = converted.Messages
	if tools, ok := body["tools"].([]interface{}); ok && len(tools) > 0 {
		anthropicReq["tools"] = convertAnthropicToolsAny(tools)
	} else if tools, ok := body["tools"].([]map[string]interface{}); ok && len(tools) > 0 {
		anthropicReq["tools"] = convertAnthropicTools(tools)
	}

	return anthropicReq
}

func convertAnthropicToolsAny(tools []interface{}) []map[string]interface{} {
	typed := make([]map[string]interface{}, 0, len(tools))
	for _, item := range tools {
		if m, ok := item.(map[string]interface{}); ok {
			typed = append(typed, m)
		}
	}
	return convertAnthropicTools(typed)
}

type anthropicConvertedMessages struct {
	SystemText string
	Messages   []interface{}
}

func convertOpenAIToAnthropicMessages(messages []interface{}) anthropicConvertedMessages {
	var result anthropicConvertedMessages
	for _, m := range messages {
		mm := mapFromAny(m)
		if mm == nil {
			continue
		}
		role, _ := mm["role"].(string)
		switch role {
		case "system":
			if content, _ := mm["content"].(string); content != "" {
				if result.SystemText != "" {
					result.SystemText += "\n"
				}
				result.SystemText += content
			}
		case "assistant":
			var blocks []interface{}
			if text, _ := mm["content"].(string); text != "" {
				blocks = append(blocks, map[string]interface{}{"type": "text", "text": text})
			}
			for _, tc := range extractOpenAIToolCalls(mm) {
				var input interface{}
				_ = json.Unmarshal([]byte(tc.Arguments), &input)
				if input == nil {
					input = map[string]interface{}{}
				}
				blocks = append(blocks, map[string]interface{}{"type": "tool_use", "id": tc.ID, "name": tc.Name, "input": input})
			}
			if len(blocks) > 0 {
				result.Messages = append(result.Messages, map[string]interface{}{"role": "assistant", "content": blocks})
			}
		case "tool":
			callID, _ := mm["tool_call_id"].(string)
			content, _ := mm["content"].(string)
			block := map[string]interface{}{"type": "tool_result", "tool_use_id": callID, "content": content}
			if callID != "" {
				block["id"] = "toolrslt_" + callID
			}
			if len(result.Messages) > 0 {
				if last, ok := result.Messages[len(result.Messages)-1].(map[string]interface{}); ok {
					if role, _ := last["role"].(string); role == "user" {
						if blocks, ok := last["content"].([]interface{}); ok && len(blocks) > 0 {
							if first, ok := blocks[0].(map[string]interface{}); ok && first["type"] == "tool_result" {
								last["content"] = append(blocks, block)
								continue
							}
						}
					}
				}
			}
			result.Messages = append(result.Messages, map[string]interface{}{"role": "user", "content": []interface{}{block}})
		default:
			result.Messages = append(result.Messages, map[string]interface{}{"role": role, "content": mm["content"]})
		}
	}
	return result
}

func convertAnthropicTools(tools []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		fn, _ := t["function"].(map[string]interface{})
		if fn == nil {
			continue
		}
		tool := map[string]interface{}{"name": fn["name"]}
		if desc, ok := fn["description"]; ok {
			tool["description"] = desc
		}
		if params, ok := fn["parameters"]; ok {
			tool["input_schema"] = params
		}
		out = append(out, tool)
	}
	return out
}

type openAIToolCallParts struct {
	ID        string
	Name      string
	Arguments string
}

func extractOpenAIToolCalls(mm map[string]interface{}) []openAIToolCallParts {
	raw := mm["tool_calls"]
	items, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]openAIToolCallParts, 0, len(items))
	for _, item := range items {
		call, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		fn, _ := call["function"].(map[string]interface{})
		out = append(out, openAIToolCallParts{
			ID:        stringFromMap(call, "id"),
			Name:      stringFromMap(fn, "name"),
			Arguments: stringFromMap(fn, "arguments"),
		})
	}
	return out
}

func mapFromAny(v interface{}) map[string]interface{} {
	switch m := v.(type) {
	case map[string]interface{}:
		return m
	case map[string]string:
		out := make(map[string]interface{}, len(m))
		for k, val := range m {
			out[k] = val
		}
		return out
	default:
		return nil
	}
}

func stringFromMap(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

// anthropicToOpenAI converts an Anthropic Messages API response body
// to an OpenAI Chat Completions response body.
func anthropicToOpenAI(resp map[string]interface{}, model string) map[string]interface{} {
	// Extract and concatenate text content blocks
	var contentBuilder strings.Builder
	var toolCalls []interface{}
	if contentArr, ok := resp["content"].([]interface{}); ok {
		for _, block := range contentArr {
			blockMap, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			blockType, _ := blockMap["type"].(string)
			switch blockType {
			case "text":
				text, _ := blockMap["text"].(string)
				contentBuilder.WriteString(text)
			case "tool_use":
				id, _ := blockMap["id"].(string)
				name, _ := blockMap["name"].(string)
				input := blockMap["input"]
				argsBytes, _ := json.Marshal(input)
				if string(argsBytes) == "null" || len(argsBytes) == 0 {
					argsBytes = []byte("{}")
				}
				if id != "" && name != "" {
					toolCalls = append(toolCalls, map[string]interface{}{
						"id":   id,
						"type": "function",
						"function": map[string]interface{}{
							"name":      name,
							"arguments": string(argsBytes),
						},
					})
				}
			}
		}
	}
	contentText := contentBuilder.String()
	message := map[string]interface{}{
		"role":    "assistant",
		"content": contentText,
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}

	// Map stop_reason to finish_reason
	stopReason, _ := resp["stop_reason"].(string)
	var finishReason string
	switch stopReason {
	case "tool_use":
		finishReason = "tool_calls"
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
				"index":         0,
				"message":       message,
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
	upstreamURL := openAIResponsesEndpoint(p.config.URL)

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
