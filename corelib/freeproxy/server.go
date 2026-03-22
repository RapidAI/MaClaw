package freeproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Server is the OpenAI-compatible HTTP proxy server.
type Server struct {
	addr     string
	cdp      *CDPClient
	mu       sync.Mutex // serialize requests per adapter (one tab at a time)
	listener net.Listener
	srv      *http.Server
}

// NewServer creates a new proxy server.
// addr is the listen address (e.g. ":10099").
// chromeDebugURL is the Chrome DevTools debug URL (e.g. "http://localhost:9222").
func NewServer(addr, chromeDebugURL string) *Server {
	return &Server{
		addr: addr,
		cdp:  NewCDPClient(chromeDebugURL),
	}
}

// Start starts the HTTP server. It blocks until the server is stopped.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/health", s.handleHealth)

	var err error
	s.listener, err = net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.addr, err)
	}

	s.srv = &http.Server{Handler: mux}
	log.Printf("[freeproxy] listening on %s", s.listener.Addr())

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
	s.cdp.Close()
}

// ── OpenAI-compatible request/response types ──

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   chatUsage    `json:"usage"`
}

type chatChoice struct {
	Index        int         `json:"index"`
	Message      chatMessage `json:"message,omitempty"`
	Delta        chatMessage `json:"delta,omitempty"`
	FinishReason *string     `json:"finish_reason"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	var models []map[string]interface{}
	for name := range Registry {
		models = append(models, map[string]interface{}{
			"id":       name,
			"object":   "model",
			"owned_by": "freeproxy",
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"object": "list",
		"data":   models,
	})
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	// Combine all messages into a single prompt
	var prompt strings.Builder
	for _, m := range req.Messages {
		if m.Role == "system" {
			prompt.WriteString("[System] " + m.Content + "\n\n")
		} else if m.Role == "user" {
			prompt.WriteString(m.Content + "\n")
		} else if m.Role == "assistant" {
			prompt.WriteString("[Previous assistant response] " + m.Content + "\n\n")
		}
	}

	adapter := ResolveAdapter(req.Model)
	ctx := r.Context()

	// Serialize access — one request at a time per browser tab
	s.mu.Lock()
	defer s.mu.Unlock()

	// Find or connect to the target tab
	tab, err := s.cdp.FindTabByDomain(ctx, adapter.Domain())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if err := s.cdp.ConnectTab(ctx, tab); err != nil {
		writeError(w, http.StatusServiceUnavailable, "connect to tab: "+err.Error())
		return
	}

	model := req.Model
	if model == "" {
		model = adapter.Name()
	}

	if req.Stream {
		s.handleStream(ctx, w, adapter, tab.ID, prompt.String(), model)
	} else {
		s.handleNonStream(ctx, w, adapter, tab.ID, prompt.String(), model)
	}
}

func (s *Server) handleNonStream(ctx context.Context, w http.ResponseWriter, adapter Adapter, tabID, prompt, model string) {
	fullText, err := adapter.SendMessage(ctx, s.cdp, tabID, prompt, func(string) {})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "adapter error: "+err.Error())
		return
	}

	stop := "stop"
	resp := chatResponse{
		ID:      fmt.Sprintf("fp-%d", time.Now().UnixMilli()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []chatChoice{{
			Index:        0,
			Message:      chatMessage{Role: "assistant", Content: fullText},
			FinishReason: &stop,
		}},
		Usage: chatUsage{
			PromptTokens:     len(prompt) / 4,
			CompletionTokens: len(fullText) / 4,
			TotalTokens:      (len(prompt) + len(fullText)) / 4,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleStream(ctx context.Context, w http.ResponseWriter, adapter Adapter, tabID, prompt, model string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	id := fmt.Sprintf("fp-%d", time.Now().UnixMilli())

	sendChunk := func(content string, finish bool) {
		chunk := chatResponse{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: time.Now().Unix(),
			Model:   model,
			Choices: []chatChoice{{Index: 0, Delta: chatMessage{Role: "assistant", Content: content}}},
		}
		if finish {
			stop := "stop"
			chunk.Choices[0].FinishReason = &stop
			chunk.Choices[0].Delta = chatMessage{}
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	_, err := adapter.SendMessage(ctx, s.cdp, tabID, prompt, func(token string) {
		sendChunk(token, false)
	})
	if err != nil {
		// Send error as final chunk
		sendChunk("", false)
	}

	// Send finish chunk
	sendChunk("", true)
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"message": msg,
			"type":    "freeproxy_error",
			"code":    code,
		},
	})
}
