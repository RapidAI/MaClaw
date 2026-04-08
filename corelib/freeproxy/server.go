package freeproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"regexp"
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

	modelMu      sync.RWMutex // protects defaultModel
	defaultModel string       // user-selected model
}

// NewServer creates a new proxy server.
func NewServer(addr, configDir string) *Server {
	auth := NewAuthStore(configDir)
	auth.Load()
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

// SetDefaultModel sets the default model used when the request model is
// "free-proxy" or empty. Thread-safe.
func (s *Server) SetDefaultModel(model string) {
	s.modelMu.Lock()
	s.defaultModel = model
	s.modelMu.Unlock()
}

// getDefaultModel returns the user-selected default model, falling back to deepseek_r1.
func (s *Server) getDefaultModel() string {
	s.modelMu.RLock()
	m := s.defaultModel
	s.modelMu.RUnlock()
	if m == "" {
		return "deepseek_r1"
	}
	return m
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
	Model          string                   `json:"model"`
	Messages       []map[string]interface{} `json:"messages"`
	Stream         bool                     `json:"stream"`
	Tools          []interface{}            `json:"tools,omitempty"`
	ToolChoice     interface{}              `json:"tool_choice,omitempty"`
	ResponseFormat interface{}              `json:"response_format,omitempty"`
	Temperature    *float64                 `json:"temperature,omitempty"`
	TopP           *float64                 `json:"top_p,omitempty"`
	MaxTokens      *int                     `json:"max_tokens,omitempty"`
}

type chatMessage struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
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
	Delta        interface{} `json:"delta,omitempty"`
	FinishReason *string     `json:"finish_reason"`
}

type streamToolCallDelta struct {
	Index    int          `json:"index"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function ToolFunction `json:"function"`
}

type streamChatDelta struct {
	Role      string                `json:"role,omitempty"`
	Content   interface{}           `json:"content,omitempty"`
	ToolCalls []streamToolCallDelta `json:"tool_calls,omitempty"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func normalizeChatContentForPrompt(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []interface{}:
		parts := make([]string, 0, len(x))
		for _, item := range x {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			typ, _ := m["type"].(string)
			if typ == "text" || typ == "input_text" || typ == "output_text" {
				if text, _ := m["text"].(string); text != "" {
					parts = append(parts, text)
					continue
				}
				if text, _ := m["content"].(string); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	}
}

func formatAssistantToolCallsForPrompt(toolCalls []ToolCall) string {
	if len(toolCalls) == 0 {
		return ""
	}
	parts := make([]string, 0, len(toolCalls))
	for i, tc := range toolCalls {
		id := strings.TrimSpace(tc.ID)
		if id == "" {
			id = fmt.Sprintf("call_%d", i+1)
		}
		name := strings.TrimSpace(tc.Function.Name)
		if name == "" {
			name = "unknown"
		}
		args := strings.TrimSpace(tc.Function.Arguments)
		if args == "" {
			args = "{}"
		}
		parts = append(parts, fmt.Sprintf("Tool call:\n- tool_call_id: %s\n- function.name: %s\n- function.arguments: %s", id, name, args))
	}
	return strings.Join(parts, "\n\n")
}

func formatToolResultForPrompt(msg map[string]interface{}) string {
	toolCallID, _ := msg["tool_call_id"].(string)
	if strings.TrimSpace(toolCallID) == "" {
		toolCallID = "unknown"
	}
	content := normalizeChatContentForPrompt(msg["content"])
	if strings.TrimSpace(content) == "" {
		content = "null"
	}
	return fmt.Sprintf("Tool result:\n- tool_call_id: %s\n- content: %s", toolCallID, content)
}

func toStreamToolCallDeltas(toolCalls []ToolCall) []streamToolCallDelta {
	if len(toolCalls) == 0 {
		return nil
	}
	deltas := make([]streamToolCallDelta, 0, len(toolCalls))
	for i, tc := range toolCalls {
		deltas = append(deltas, streamToolCallDelta{
			Index:    i,
			ID:       tc.ID,
			Type:     tc.Type,
			Function: tc.Function,
		})
	}
	return deltas
}

func normalizeMessagesForPrompt(messages []map[string]interface{}) []chatMessage {
	out := make([]chatMessage, 0, len(messages))
	for _, m := range messages {
		role, _ := m["role"].(string)
		role = strings.ToLower(strings.TrimSpace(role))
		cm := chatMessage{Role: role, Content: normalizeChatContentForPrompt(m["content"])}
		if rawCalls, ok := m["tool_calls"].([]interface{}); ok {
			for _, item := range rawCalls {
				b, _ := json.Marshal(item)
				var tc ToolCall
				if json.Unmarshal(b, &tc) == nil {
					cm.ToolCalls = append(cm.ToolCalls, tc)
				}
			}
		}
		if id, _ := m["tool_call_id"].(string); id != "" {
			cm.ToolCallID = id
		}
		out = append(out, cm)
	}
	return out
}

func extractToolCalls(content string) []ToolCall {
	if HasToolCalls(content) {
		if toolCalls := ParseToolCalls(content); len(toolCalls) > 0 {
			return toolCalls
		}
	}
	if HasXMLToolCalls(content) {
		if toolCalls := ParseXMLToolCalls(content); len(toolCalls) > 0 {
			return toolCalls
		}
	}
	return nil
}

func removeToolCallBlocks(content string) string {
	cleaned := content
	if HasToolCalls(cleaned) {
		cleaned = RemoveToolCallBlocks(cleaned)
	}
	if HasXMLToolCalls(cleaned) {
		cleaned = RemoveXMLToolCallBlocks(cleaned)
	}
	cleaned = reCompactBlank.ReplaceAllString(cleaned, "\n\n")
	return strings.TrimSpace(cleaned)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	models := AvailableModels()
	var data []map[string]interface{}
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
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
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
	normalizedMessages := normalizeMessagesForPrompt(req.Messages)

	// Combine messages into a single prompt
	var prompt strings.Builder

	// Inject tool system prompt before other messages so the model sees it first
	hasTools := len(req.Tools) > 0
	if hasTools {
		toolPrompt := GenerateToolSystemPrompt(req.Tools)
		if toolPrompt != "" {
			prompt.WriteString("[System] " + toolPrompt + "\n\n")
		}
	}

	for _, m := range normalizedMessages {
		switch m.Role {
		case "system":
			compact := compactSystemPrompt(normalizeChatContentForPrompt(m.Content))
			if compact != "" {
				if len(compact) != len(normalizeChatContentForPrompt(m.Content)) {
					log.Printf("[freeproxy] system prompt compacted: %d -> %d chars", len(normalizeChatContentForPrompt(m.Content)), len(compact))
				}
				prompt.WriteString("[System] " + compact + "\n\n")
			}
		case "user":
			prompt.WriteString(normalizeChatContentForPrompt(m.Content) + "\n")
		case "assistant":
			combined := normalizeChatContentForPrompt(m.Content)
			if toolText := formatAssistantToolCallsForPrompt(m.ToolCalls); toolText != "" {
				if combined != "" {
					combined += "\n\n"
				}
				combined += toolText
			}
			if combined != "" {
				prompt.WriteString("[Previous assistant response] " + combined + "\n\n")
			}
		case "tool", "function":
			toolMsg := map[string]interface{}{
				"tool_call_id": m.ToolCallID,
				"content":      m.Content,
			}
			prompt.WriteString(formatToolResultForPrompt(toolMsg) + "\n\n")
		}
	}

	modelClass := req.Model
	if modelClass == "" || modelClass == "free-proxy" {
		modelClass = s.getDefaultModel()
	}

	ctx := r.Context()

	// Create a conversation for this request
	conversationID, err := s.client.CreateSession(ctx)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "create conversation: "+err.Error())
		return
	}
	defer func() {
		go func() {
			dctx, dcancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer dcancel()
			s.client.DeleteSession(dctx, conversationID)
		}()
	}()

	// Serialize completions to avoid rate limits
	s.mu.Lock()
	defer s.mu.Unlock()

	if ctx.Err() != nil {
		writeError(w, http.StatusServiceUnavailable, "request cancelled while waiting in queue")
		return
	}

	cr := CompletionRequest{
		ConversationID: conversationID,
		Prompt:         prompt.String(),
		ModelClass:     modelClass,
	}

	if req.Stream {
		s.handleStream(ctx, w, cr, modelClass, hasTools)
	} else {
		s.handleNonStream(ctx, w, cr, modelClass, hasTools)
	}
}

func (s *Server) handleNonStream(ctx context.Context, w http.ResponseWriter, cr CompletionRequest, model string, hasTools bool) {
	fullText, _, err := s.client.StreamCompletion(ctx, cr, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "completion error: "+err.Error())
		return
	}

	stop := "stop"
	choice := chatChoice{Index: 0, FinishReason: &stop}

	// Check for tool calls in the response
	if hasTools {
		toolCalls := extractToolCalls(fullText)
		if len(toolCalls) > 0 {
			cleanText := removeToolCallBlocks(fullText)
			toolStop := "tool_calls"
			choice.FinishReason = &toolStop
			choice.Message = chatMessage{Role: "assistant", Content: cleanText, ToolCalls: toolCalls}
		} else {
			choice.Message = chatMessage{Role: "assistant", Content: fullText}
		}
	} else {
		choice.Message = chatMessage{Role: "assistant", Content: fullText}
	}

	resp := chatResponse{
		ID:      fmt.Sprintf("fp-%d", time.Now().UnixMilli()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []chatChoice{choice},
		Usage: chatUsage{
			PromptTokens:     len(cr.Prompt) / 4,
			CompletionTokens: len(fullText) / 4,
			TotalTokens:      (len(cr.Prompt) + len(fullText)) / 4,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleStream(ctx context.Context, w http.ResponseWriter, cr CompletionRequest, model string, hasTools bool) {
	// When tools are present, we must buffer the full response to detect
	// tool_call blocks before sending anything to the client.
	if hasTools {
		s.handleStreamWithTools(ctx, w, cr, model)
		return
	}

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
			Choices: []chatChoice{{Index: 0, Delta: streamChatDelta{Role: "assistant", Content: content}}},
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
		sendChunk(fmt.Sprintf("\n[stream error: %v]", err), false)
	}

	sendChunk("", true)
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// handleStreamWithTools buffers the full completion, checks for tool calls,
// then emits the result as SSE. This is necessary because tool_call blocks
// can only be detected after the full response is available.
func (s *Server) handleStreamWithTools(ctx context.Context, w http.ResponseWriter, cr CompletionRequest, model string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	fullText, _, err := s.client.StreamCompletion(ctx, cr, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "completion error: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	id := fmt.Sprintf("fp-%d", time.Now().UnixMilli())

	if toolCalls := extractToolCalls(fullText); len(toolCalls) > 0 {
		cleanText := removeToolCallBlocks(fullText)
		// Emit text content if any
		if cleanText != "" {
			chunk := chatResponse{
				ID: id, Object: "chat.completion.chunk", Created: time.Now().Unix(), Model: model,
				Choices: []chatChoice{{Index: 0, Delta: streamChatDelta{Role: "assistant", Content: cleanText}}},
			}
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
		// Emit tool calls chunk
		toolStop := "tool_calls"
		chunk := chatResponse{
			ID: id, Object: "chat.completion.chunk", Created: time.Now().Unix(), Model: model,
			Choices: []chatChoice{{Index: 0, Delta: streamChatDelta{ToolCalls: toStreamToolCallDeltas(toolCalls)}, FinishReason: &toolStop}},
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	// No tool calls — emit as normal text stream
	stop := "stop"
	chunk := chatResponse{
		ID: id, Object: "chat.completion.chunk", Created: time.Now().Unix(), Model: model,
		Choices: []chatChoice{{Index: 0, Delta: streamChatDelta{Role: "assistant", Content: fullText}}},
	}
	data, _ := json.Marshal(chunk)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()

	finishChunk := chatResponse{
		ID: id, Object: "chat.completion.chunk", Created: time.Now().Unix(), Model: model,
		Choices: []chatChoice{{Index: 0, Delta: chatMessage{}, FinishReason: &stop}},
	}
	data, _ = json.Marshal(finishChunk)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// Pre-compiled regexps for compactSystemPrompt to avoid re-compilation per call.
var (
	reCompactTools    = regexp.MustCompile(`(?s)需要执行操作时，使用以下格式调用工具：.*?(\n\n|\z)`)
	reCompactToolList = regexp.MustCompile(`(?s)可用工具：\n(?:- .+\n(?:  .+\n)*)+`)
	reCompactRules    = regexp.MustCompile(`(?s)规则：\n(?:\d+\..+\n)+`)
	reCompactBlank    = regexp.MustCompile(`\n{3,}`)
)

// compactSystemPrompt strips verbose tool definitions, workflow rules, and
// other boilerplate from system prompts to keep the question field short
// enough for the 当贝 API. It preserves the core identity line and any
// short instructions.
func compactSystemPrompt(s string) string {
	s = reCompactTools.ReplaceAllString(s, "")
	s = reCompactToolList.ReplaceAllString(s, "")

	// Remove workflow section: find start marker, then cut until next "## " or end
	if idx := strings.Index(s, "## ⚠️ 编程任务工作流"); idx >= 0 {
		end := len(s)
		// Find next "## " that is NOT part of the workflow subsections
		rest := s[idx+len("## ⚠️ 编程任务工作流"):]
		for _, marker := range []string{"\n## 当前设备", "\n## 用户记忆"} {
			if p := strings.Index(rest, marker); p >= 0 {
				candidate := idx + len("## ⚠️ 编程任务工作流") + p
				if candidate < end {
					end = candidate
				}
			}
		}
		s = s[:idx] + s[end:]
	}

	// Remove other verbose sections by string search (no lookahead needed)
	removeSections := []string{
		"执行验证原则", "会话失败止损原则", "工具使用要点",
		"安全防火墙", "高级能力", "对话管理", "动态工具",
		"记忆管理指引", "上线昵称报告",
	}
	for _, sec := range removeSections {
		marker := "## "
		// Find the section header containing this text
		idx := strings.Index(s, sec)
		if idx < 0 {
			continue
		}
		// Walk back to find the "## " prefix
		start := strings.LastIndex(s[:idx], marker)
		if start < 0 {
			start = idx
		}
		// Find the next "## " after this section
		rest := s[idx+len(sec):]
		end := len(s)
		if p := strings.Index(rest, "\n## "); p >= 0 {
			end = idx + len(sec) + p
		}
		s = s[:start] + s[end:]
	}

	s = reCompactRules.ReplaceAllString(s, "")
	s = reCompactBlank.ReplaceAllString(s, "\n\n")
	s = strings.TrimSpace(s)

	// Hard cap at 4000 runes
	runes := []rune(s)
	if len(runes) > 4000 {
		s = string(runes[:4000]) + "\n...(truncated)"
	}

	return s
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

