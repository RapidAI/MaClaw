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
)

// Server is the local Anthropic→OpenAI protocol conversion proxy.
type Server struct {
	addr     string
	listener net.Listener
	srv      *http.Server
	client   *http.Client // reused for upstream requests

	mu          sync.RWMutex
	upstreamURL string // CodeGen OpenAI-compatible base URL
	apiKey      string // CodeGen access token
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
	s.mu.Lock()
	s.upstreamURL = strings.TrimRight(baseURL, "/")
	s.apiKey = apiKey
	s.mu.Unlock()
}

func (s *Server) getUpstream() (string, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.upstreamURL, s.apiKey
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

// Start starts the HTTP server. It blocks until the server is stopped.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/anthropic/v1/messages", s.handleMessages)
	mux.HandleFunc("/anthropic/v1/models", s.handleModels)
	mux.HandleFunc("/health", s.handleHealth)

	var err error
	s.listener, err = net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.addr, err)
	}

	s.srv = &http.Server{Handler: mux}
	log.Printf("[codegenproxy] listening on %s (Anthropic→OpenAI adapter)", s.listener.Addr())

	go func() {
		<-ctx.Done()
		s.srv.Shutdown(context.Background())
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
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	upURL, fallbackKey := s.getUpstream()
	if upURL == "" {
		writeError(w, http.StatusServiceUnavailable, "upstream not configured")
		return
	}
	apiKey := resolveAPIKey(r, fallbackKey)
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, upURL+"/models", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := s.client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// handleMessages receives Anthropic Messages API requests, converts to
// OpenAI chat completions, forwards to upstream CodeGen, converts back.
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	upURL, fallbackKey := s.getUpstream()
	if upURL == "" {
		writeError(w, http.StatusServiceUnavailable, "upstream not configured")
		return
	}

	// Claude Code sends token via x-api-key / Authorization: Bearer.
	// Fall back to the server-configured key (from SSO) if absent.
	apiKey := resolveAPIKey(r, fallbackKey)

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

	// Convert Anthropic → OpenAI
	openaiReq := convertAnthropicToOpenAI(anthReq)

	reqData, _ := json.Marshal(openaiReq)

	// Forward to upstream CodeGen using standard OpenAI Bearer auth.
	upEndpoint := upURL + "/chat/completions"
	upReq, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, upEndpoint, bytes.NewReader(reqData))
	upReq.Header.Set("Content-Type", "application/json")
	upReq.Header.Set("Authorization", "Bearer "+apiKey)

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
