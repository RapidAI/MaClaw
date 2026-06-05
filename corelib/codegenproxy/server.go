// Package codegenproxy implements a local HTTP proxy that accepts
// Anthropic Messages API requests and converts them to OpenAI chat
// completions format before forwarding to the upstream CodeGen service.
// This allows Claude Code (which speaks Anthropic protocol) to work
// with CodeGen (which only speaks OpenAI protocol).
package codegenproxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func setCodeGenUpstreamHeaders(req *http.Request, clientName string) {
	if req != nil {
		req.Header.Set(corelib.CodeGenClientNameHeader, corelib.NormalizeCodeGenClientName(clientName))
	}
}

// Server is the local Anthropic→OpenAI protocol conversion proxy.
type Server struct {
	addr     string
	listener net.Listener
	srv      *http.Server
	client   *http.Client // reused for upstream requests

	mu          sync.RWMutex
	upstreamURL string // CodeGen OpenAI-compatible base URL
	apiKey      string // CodeGen access token
	clientName  string // X-Codegen-Client-Name value for upstream CodeGen requests
	clientKey   string // optional local proxy API key for OpenAI/Anthropic clients
}

// NewServer creates a new codegen proxy server.
func NewServer(addr string) *Server {
	return &Server{
		addr: addr,
		client: &http.Client{
			Timeout: 10 * time.Minute,
			Transport: &http.Transport{
				MaxIdleConns:       10,
				IdleConnTimeout:    90 * time.Second,
				DisableCompression: true, // SSE must not be compressed
			},
		},
	}
}

// SetUpstream configures the upstream CodeGen endpoint and API key.
func (s *Server) SetUpstream(baseURL, apiKey string) {
	s.SetUpstreamWithClientName(baseURL, apiKey, corelib.CodeGenClientName)
}

// SetUpstreamWithClientName configures the upstream CodeGen endpoint, API key,
// and CodeGen client identity header.
func (s *Server) SetUpstreamWithClientName(baseURL, apiKey, clientName string) {
	s.mu.Lock()
	s.upstreamURL = strings.TrimRight(baseURL, "/")
	s.apiKey = apiKey
	s.clientName = strings.TrimSpace(clientName)
	s.mu.Unlock()
}

// SetClientAPIKey configures the optional API key that local OpenAI and
// Anthropic clients must send to the proxy. When this key is set, incoming
// credentials are validated locally and are not forwarded to CodeGen; the
// SSO access token configured by SetUpstream is used for upstream requests.
func (s *Server) SetClientAPIKey(apiKey string) {
	s.mu.Lock()
	s.clientKey = strings.TrimSpace(apiKey)
	s.mu.Unlock()
}

func (s *Server) getConfig() (string, string, string, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.upstreamURL, s.apiKey, s.clientName, s.clientKey
}

// resolveAPIKey determines the API key to use for the upstream request.
// Priority: incoming request header > server-configured key.
// Claude Code sets ANTHROPIC_AUTH_TOKEN which the SDK sends as
// x-api-key and/or Authorization: Bearer in the request headers.
func resolveAPIKey(r *http.Request, fallback string) string {
	if k := r.Header.Get("x-api-key"); k != "" {
		return k
	}
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return fallback
}

func requestAPIKey(r *http.Request) string {
	if k := r.Header.Get("x-api-key"); k != "" {
		return k
	}
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

func (s *Server) authorizeClient(r *http.Request, configuredClientKey string) bool {
	if strings.TrimSpace(configuredClientKey) == "" {
		return true
	}
	return requestAPIKey(r) == configuredClientKey
}

// Start starts the HTTP server. It blocks until the server is stopped.
func (s *Server) Start(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.addr, err)
	}
	return s.Serve(ctx, listener)
}

// Serve starts the HTTP server on a listener created by the caller. This lets
// desktop wrappers bind synchronously, report failures immediately, and then
// hand the already-bound socket to the proxy.
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	shutdownDone := make(chan struct{})
	defer close(shutdownDone)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", s.handleOpenAIChatCompletions)
	mux.HandleFunc("/v1/models", s.handleOpenAIModels)
	mux.HandleFunc("/models", s.handleOpenAIModels)
	mux.HandleFunc("/anthropic/v1/messages", s.handleMessages)
	mux.HandleFunc("/anthropic/v1/models", s.handleModels)
	mux.HandleFunc("/health", s.handleHealth)

	s.listener = listener

	s.srv = &http.Server{Handler: mux}
	log.Printf("[codegenproxy] listening on %s (Anthropic→OpenAI adapter)", s.listener.Addr())

	go func() {
		select {
		case <-ctx.Done():
			_ = s.srv.Shutdown(context.Background())
		case <-shutdownDone:
		}
	}()

	if err := s.srv.Serve(s.listener); err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() {
	if s.srv != nil {
		s.srv.Shutdown(context.Background())
	}
}

// Addr returns the listener address. Only valid after Start has bound.
func (s *Server) Addr() net.Addr {
	if s.listener != nil {
		return s.listener.Addr()
	}
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	upURL, _, _, _ := s.getConfig()
	status := "ok"
	if upURL == "" {
		status = "not_configured"
	}
	json.NewEncoder(w).Encode(map[string]string{"status": status})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	s.handleModelsForProtocol(w, r, "anthropic")
}

func (s *Server) handleOpenAIModels(w http.ResponseWriter, r *http.Request) {
	s.handleModelsForProtocol(w, r, "openai")
}

func (s *Server) handleModelsForProtocol(w http.ResponseWriter, r *http.Request, protocol string) {
	upURL, fallbackKey, clientName, clientKey := s.getConfig()
	if upURL == "" {
		writeError(w, http.StatusServiceUnavailable, "upstream not configured")
		return
	}
	if !s.authorizeClient(r, clientKey) {
		writeError(w, http.StatusUnauthorized, "invalid proxy api key")
		return
	}
	apiKey := fallbackKey
	if clientKey == "" {
		apiKey = resolveAPIKey(r, fallbackKey)
	}
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, upURL+"/models", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	setCodeGenUpstreamHeaders(req, clientName)
	resp, err := s.client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		writeError(w, http.StatusBadGateway, "read upstream models: "+err.Error())
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(body)
		return
	}
	normalized, err := normalizeModelsResponse(body, protocol)
	if err != nil {
		writeError(w, http.StatusBadGateway, "parse upstream models: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(normalized)
}

// handleOpenAIChatCompletions forwards OpenAI-compatible chat completion
// requests directly to CodeGen. The request body, including model name, is
// passed through unchanged so agent-side /model commands can select models
// without TigerProxy rewriting them.
func (s *Server) handleOpenAIChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	upURL, fallbackKey, clientName, clientKey := s.getConfig()
	if upURL == "" {
		writeError(w, http.StatusServiceUnavailable, "upstream not configured")
		return
	}
	if !s.authorizeClient(r, clientKey) {
		writeError(w, http.StatusUnauthorized, "invalid proxy api key")
		return
	}
	apiKey := fallbackKey
	if clientKey == "" {
		apiKey = resolveAPIKey(r, fallbackKey)
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	body = normalizeOpenAIModelInBody(body)

	upReq, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, upURL+"/chat/completions", bytes.NewReader(body))
	upReq.Header.Set("Content-Type", "application/json")
	upReq.Header.Set("Accept", r.Header.Get("Accept"))
	upReq.Header.Set("Authorization", "Bearer "+apiKey)
	setCodeGenUpstreamHeaders(upReq, clientName)

	upResp, err := s.client.Do(upReq)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream: "+err.Error())
		return
	}
	defer upResp.Body.Close()

	if ct := upResp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(upResp.StatusCode)
	_, _ = io.Copy(w, upResp.Body)
}

// handleMessages receives Anthropic Messages API requests, converts to
// OpenAI chat completions, forwards to upstream CodeGen, converts back.
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	upURL, fallbackKey, clientName, clientKey := s.getConfig()
	if upURL == "" {
		writeError(w, http.StatusServiceUnavailable, "upstream not configured")
		return
	}
	if !s.authorizeClient(r, clientKey) {
		writeError(w, http.StatusUnauthorized, "invalid proxy api key")
		return
	}

	// Claude Code sends token via x-api-key / Authorization: Bearer.
	// Fall back to the server-configured key (from SSO) if absent.
	apiKey := fallbackKey
	if clientKey == "" {
		apiKey = resolveAPIKey(r, fallbackKey)
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}

	var anthReq anthropicRequest
	if err := json.Unmarshal(body, &anthReq); err != nil {
		writeError(w, http.StatusBadRequest, "parse body: "+err.Error())
		return
	}
	anthReq.Model = normalizeModelIdentifier(anthReq.Model)

	// Convert Anthropic → OpenAI
	openaiReq := convertAnthropicToOpenAI(anthReq)

	reqData, _ := json.Marshal(openaiReq)

	// Forward to upstream CodeGen using standard OpenAI Bearer auth.
	upEndpoint := upURL + "/chat/completions"
	upReq, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, upEndpoint, bytes.NewReader(reqData))
	upReq.Header.Set("Content-Type", "application/json")
	upReq.Header.Set("Authorization", "Bearer "+apiKey)
	setCodeGenUpstreamHeaders(upReq, clientName)

	upResp, err := s.client.Do(upReq)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream: "+err.Error())
		return
	}
	defer upResp.Body.Close()

	if anthReq.Stream {
		s.handleStreamResponse(w, upResp, anthReq.Model)
	} else {
		s.handleNonStreamResponse(w, upResp, anthReq.Model)
	}
}

// handleNonStreamResponse converts an OpenAI non-streaming response to Anthropic format.
func (s *Server) handleNonStreamResponse(w http.ResponseWriter, upResp *http.Response, model string) {
	respBody, _ := io.ReadAll(io.LimitReader(upResp.Body, 10*1024*1024))

	if upResp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(upResp.StatusCode)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"type": "error",
			"error": map[string]interface{}{
				"type":    "api_error",
				"message": truncate(string(respBody), 1024),
			},
		})
		return
	}

	var openaiResp openaiChatResponse
	if err := json.Unmarshal(respBody, &openaiResp); err != nil {
		writeError(w, http.StatusBadGateway, "parse upstream: "+err.Error())
		return
	}

	anthResp := convertOpenAIToAnthropic(openaiResp, model)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(anthResp)
}

// handleStreamResponse converts an OpenAI SSE stream to Anthropic SSE stream.
func (s *Server) handleStreamResponse(w http.ResponseWriter, upResp *http.Response, model string) {
	if upResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(upResp.Body, 256*1024))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(upResp.StatusCode)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"type": "error",
			"error": map[string]interface{}{
				"type":    "api_error",
				"message": truncate(string(respBody), 1024),
			},
		})
		return
	}

	// Some gateways return non-SSE content-type even for stream=true.
	// If the body is clearly not SSE, fall back to non-stream parsing.
	ct := upResp.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		s.handleNonStreamResponse(w, upResp, model)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// Anthropic SSE: message_start → content_block_start/delta/stop → message_delta → message_stop
	msgID := fmt.Sprintf("msg_proxy_%d", time.Now().UnixNano())
	writeSSE(w, "message_start", map[string]interface{}{
		"type": "message_start",
		"message": map[string]interface{}{
			"id": msgID, "type": "message", "role": "assistant",
			"model": model, "content": []interface{}{},
			"usage": map[string]int{"input_tokens": 0, "output_tokens": 0},
		},
	})
	flusher.Flush()

	scanner := bufio.NewScanner(upResp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	blockIdx := 0
	textStarted := false
	curToolIdx := -1
	var stopReason string

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}

		var chunk openaiStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		finish := chunk.Choices[0].FinishReason

		// ── text content ──
		if delta.Content != "" {
			if !textStarted {
				writeSSE(w, "content_block_start", map[string]interface{}{
					"type": "content_block_start", "index": blockIdx,
					"content_block": map[string]interface{}{"type": "text", "text": ""},
				})
				flusher.Flush()
				textStarted = true
			}
			writeSSE(w, "content_block_delta", map[string]interface{}{
				"type": "content_block_delta", "index": blockIdx,
				"delta": map[string]interface{}{"type": "text_delta", "text": delta.Content},
			})
			flusher.Flush()
		}

		// ── tool calls ──
		for _, tc := range delta.ToolCalls {
			if tc.Index != curToolIdx {
				// close previous text block
				if textStarted {
					writeSSE(w, "content_block_stop", map[string]interface{}{
						"type": "content_block_stop", "index": blockIdx,
					})
					blockIdx++
					textStarted = false
					flusher.Flush()
				}
				// close previous tool block
				if curToolIdx >= 0 {
					writeSSE(w, "content_block_stop", map[string]interface{}{
						"type": "content_block_stop", "index": blockIdx,
					})
					blockIdx++
					flusher.Flush()
				}
				curToolIdx = tc.Index
				writeSSE(w, "content_block_start", map[string]interface{}{
					"type": "content_block_start", "index": blockIdx,
					"content_block": map[string]interface{}{
						"type": "tool_use", "id": tc.ID,
						"name": tc.Function.Name, "input": map[string]interface{}{},
					},
				})
				flusher.Flush()
			}
			if tc.Function.Arguments != "" {
				writeSSE(w, "content_block_delta", map[string]interface{}{
					"type": "content_block_delta", "index": blockIdx,
					"delta": map[string]interface{}{
						"type": "input_json_delta", "partial_json": tc.Function.Arguments,
					},
				})
				flusher.Flush()
			}
		}

		if finish != "" {
			stopReason = finish
		}
	}

	// close any open block
	if textStarted || curToolIdx >= 0 {
		writeSSE(w, "content_block_stop", map[string]interface{}{
			"type": "content_block_stop", "index": blockIdx,
		})
		flusher.Flush()
	}

	anthStop := "end_turn"
	switch stopReason {
	case "tool_calls":
		anthStop = "tool_use"
	case "length":
		anthStop = "max_tokens"
	}

	writeSSE(w, "message_delta", map[string]interface{}{
		"type":  "message_delta",
		"delta": map[string]interface{}{"stop_reason": anthStop, "stop_sequence": nil},
		"usage": map[string]int{"output_tokens": 0},
	})
	flusher.Flush()

	writeSSE(w, "message_stop", map[string]interface{}{"type": "message_stop"})
	flusher.Flush()
}

// writeSSE writes a single Anthropic SSE event.
func writeSSE(w http.ResponseWriter, event string, data interface{}) {
	b, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(b))
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"type": "error",
		"error": map[string]interface{}{
			"type":    "api_error",
			"message": msg,
		},
	})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func normalizeOpenAIModelInBody(body []byte) []byte {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	if model, ok := payload["model"].(string); ok {
		payload["model"] = normalizeModelIdentifier(model)
		if out, err := json.Marshal(payload); err == nil {
			return out
		}
	}
	return body
}

func normalizeModelIdentifier(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return model
	}
	if i := strings.LastIndex(model, "/"); i >= 0 && i+1 < len(model) {
		return strings.TrimSpace(model[i+1:])
	}
	return model
}

func normalizeModelsResponse(body []byte, protocol string) ([]byte, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	entries, ok := modelEntries(raw)
	if !ok {
		return body, nil
	}
	if protocol == "anthropic" {
		return json.Marshal(map[string]interface{}{"data": normalizeAnthropicModels(entries)})
	}
	out := make(map[string]interface{}, len(raw)+1)
	for k, v := range raw {
		out[k] = v
	}
	out["object"] = "list"
	out["data"] = normalizeOpenAIModels(entries)
	delete(out, "models")
	return json.Marshal(out)
}

func modelEntries(raw map[string]interface{}) ([]interface{}, bool) {
	if data, ok := raw["data"].([]interface{}); ok {
		return data, true
	}
	if models, ok := raw["models"].([]interface{}); ok {
		return models, true
	}
	return nil, false
}

func normalizeOpenAIModels(entries []interface{}) []map[string]interface{} {
	models := make([]map[string]interface{}, 0, len(entries))
	for _, entry := range entries {
		m, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		id := normalizeModelIdentifier(modelField(m, "id", "name"))
		if id == "" {
			continue
		}
		models = append(models, map[string]interface{}{
			"id":       id,
			"object":   "model",
			"created":  0,
			"owned_by": modelField(m, "provider"),
		})
	}
	return models
}

func normalizeAnthropicModels(entries []interface{}) []map[string]interface{} {
	models := make([]map[string]interface{}, 0, len(entries))
	for _, entry := range entries {
		m, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		id := normalizeModelIdentifier(modelField(m, "id", "name"))
		if id == "" {
			continue
		}
		display := modelField(m, "display_name", "name")
		if display == "" {
			display = id
		}
		models = append(models, map[string]interface{}{
			"id":           id,
			"display_name": display,
			"type":         "model",
		})
	}
	return models
}

func modelField(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
