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

// Server is the OpenAI-compatible HTTP proxy server backed by 当贝 AI.
type Server struct {
	addr     string
	auth     *AuthStore
	client   *DangbeiClient
	mu       sync.Mutex // serialize completion requests (one at a time)
	listener net.Listener
	srv      *http.Server
}

// NewServer creates a new proxy server.
// addr is the listen address (e.g. ":10099").
// configDir is the directory for persisting auth data.
func NewServer(addr, configDir string) *Server {
	auth := NewAuthStore(configDir)
	auth.Load() // best-effort load persisted cookie
	return &Server{
		addr:   addr,
		auth:   auth,
		client: NewDangbeiClient(auth),
	}
}

// Auth returns the underlying AuthStore for external login flows.
func (s *Server) Auth() *AuthStore { return s.auth }

// Client returns the underlying DangbeiClient.
func (s *Server) Client() *DangbeiClient { return s.client }

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
	log.Printf("[freeproxy] listening on %s (当贝 AI backend)", s.listener.Addr())

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
	models := AvailableModels()
	var data []map[string]interface{}
	// Add the generic "free-proxy" meta model
	data = append(data, map[string]interface{}{
		"id": "free-proxy", "object": "model", "owned_by": "dangbei",
	})
	for _, m := range models {
		data = append(data, map[string]interface{}{
			"id": m.ID, "object": "model", "owned_by": "dangbei",
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"object": "list", "data": data})
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !s.auth.HasAuth() {
		writeError(w, http.StatusUnauthorized, "未登录当贝 AI，请先在 MaClaw 设置中完成登录")
		return
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	// Combine messages into a single prompt
	var prompt strings.Builder
	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			prompt.WriteString("[System] " + m.Content + "\n\n")
		case "user":
			prompt.WriteString(m.Content + "\n")
		case "assistant":
			prompt.WriteString("[Previous assistant response] " + m.Content + "\n\n")
		}
	}

	// Resolve model class — "free-proxy" or empty defaults to deepseek_r1
	modelClass := req.Model
	if modelClass == "" || modelClass == "free-proxy" {
		modelClass = "deepseek_r1"
	}

	ctx := r.Context()

	// Create a session for this request (not under lock — network I/O)
	sessionID, err := s.client.CreateSession(ctx)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "create session: "+err.Error())
		return
	}
	defer func() {
		go s.client.DeleteSession(context.Background(), sessionID)
	}()

	// Serialize completions — one at a time to avoid 当贝 rate limits
	s.mu.Lock()
	defer s.mu.Unlock()

	cr := CompletionRequest{
		SessionID:  sessionID,
		Prompt:     prompt.String(),
		ModelClass: modelClass,
	}

	if req.Stream {
		s.handleStream(ctx, w, cr, modelClass)
	} else {
		s.handleNonStream(ctx, w, cr, modelClass)
	}
}

func (s *Server) handleNonStream(ctx context.Context, w http.ResponseWriter, cr CompletionRequest, model string) {
	fullText, _, err := s.client.StreamCompletion(ctx, cr, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "completion error: "+err.Error())
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
			PromptTokens:     len(cr.Prompt) / 4,
			CompletionTokens: len(fullText) / 4,
			TotalTokens:      (len(cr.Prompt) + len(fullText)) / 4,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleStream(ctx context.Context, w http.ResponseWriter, cr CompletionRequest, model string) {
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

	_, _, err := s.client.StreamCompletion(ctx, cr, func(token string) {
		sendChunk(token, false)
	})
	if err != nil {
		log.Printf("[freeproxy] stream error: %v", err)
	}

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
