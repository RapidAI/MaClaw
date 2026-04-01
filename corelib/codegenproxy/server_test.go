package codegenproxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// startTestServer starts a proxy on :0 and waits for it to be ready.
func startTestServer(t *testing.T) (*Server, context.CancelFunc) {
	t.Helper()
	srv := NewServer(":0")
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start(ctx) }()

	// Wait for listener to bind
	deadline := time.After(2 * time.Second)
	for {
		if srv.Addr() != nil {
			break
		}
		select {
		case err := <-errCh:
			cancel()
			t.Fatalf("server exited early: %v", err)
		case <-deadline:
			cancel()
			t.Fatal("server did not start in time")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	return srv, cancel
}

func TestServerHealthEndpoint(t *testing.T) {
	srv, cancel := startTestServer(t)
	defer cancel()

	resp, err := http.Get("http://" + srv.Addr().String() + "/health")
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("health status = %d, want 200", resp.StatusCode)
	}
}

func TestConvertAnthropicToOpenAI_BasicText(t *testing.T) {
	req := anthropicRequest{
		Model:     "claude-3",
		System:    "You are helpful.",
		MaxTokens: 2048,
		Messages: []anthropicMessage{
			{Role: "user", Content: "Hello"},
		},
	}

	result := convertAnthropicToOpenAI(req)

	if result.Model != "claude-3" {
		t.Fatalf("model = %q, want %q", result.Model, "claude-3")
	}
	if result.MaxTokens != 2048 {
		t.Fatalf("max_tokens = %d, want 2048", result.MaxTokens)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("messages count = %d, want 2", len(result.Messages))
	}
	if result.Messages[0].Role != "system" {
		t.Fatalf("first message role = %q, want system", result.Messages[0].Role)
	}
	if result.Messages[1].Role != "user" {
		t.Fatalf("second message role = %q, want user", result.Messages[1].Role)
	}
}

func TestConvertAnthropicToOpenAI_ToolUse(t *testing.T) {
	req := anthropicRequest{
		Model: "claude-3",
		Messages: []anthropicMessage{
			{Role: "user", Content: "What's the weather?"},
			{Role: "assistant", Content: []interface{}{
				map[string]interface{}{"type": "text", "text": "Let me check."},
				map[string]interface{}{
					"type": "tool_use", "id": "call_123",
					"name": "get_weather", "input": map[string]interface{}{"city": "Seattle"},
				},
			}},
			{Role: "user", Content: []interface{}{
				map[string]interface{}{
					"type": "tool_result", "tool_use_id": "call_123",
					"content": "Sunny, 72°F",
				},
			}},
		},
		Tools: []anthropicTool{
			{Name: "get_weather", Description: "Get weather", InputSchema: map[string]interface{}{"type": "object"}},
		},
	}

	result := convertAnthropicToOpenAI(req)

	if len(result.Messages) != 3 {
		t.Fatalf("messages count = %d, want 3", len(result.Messages))
	}
	if len(result.Messages[1].ToolCalls) != 1 {
		t.Fatalf("tool_calls count = %d, want 1", len(result.Messages[1].ToolCalls))
	}
	if result.Messages[1].ToolCalls[0].Function.Name != "get_weather" {
		t.Fatalf("tool name = %q, want get_weather", result.Messages[1].ToolCalls[0].Function.Name)
	}
	if result.Messages[2].Role != "tool" {
		t.Fatalf("tool result role = %q, want tool", result.Messages[2].Role)
	}
	if result.Messages[2].ToolCallID != "call_123" {
		t.Fatalf("tool_call_id = %q, want call_123", result.Messages[2].ToolCallID)
	}
	if len(result.Tools) != 1 || result.Tools[0].Function.Name != "get_weather" {
		t.Fatalf("tools = %+v", result.Tools)
	}
}

func TestConvertOpenAIToAnthropic_BasicText(t *testing.T) {
	resp := openaiChatResponse{
		ID: "chatcmpl-123",
		Choices: []openaiChoice{{
			Message:      openaiMessage{Role: "assistant", Content: "Hello!"},
			FinishReason: "stop",
		}},
		Usage: &openaiUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}

	result := convertOpenAIToAnthropic(resp, "claude-3")

	if result.Type != "message" {
		t.Fatalf("type = %q, want message", result.Type)
	}
	if result.StopReason != "end_turn" {
		t.Fatalf("stop_reason = %q, want end_turn", result.StopReason)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "Hello!" {
		t.Fatalf("content = %+v", result.Content)
	}
	if result.Usage.InputTokens != 10 || result.Usage.OutputTokens != 5 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

func TestConvertOpenAIToAnthropic_ToolCalls(t *testing.T) {
	resp := openaiChatResponse{
		Choices: []openaiChoice{{
			Message: openaiMessage{
				Role: "assistant",
				ToolCalls: []openaiToolCall{{
					ID: "call_456", Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "search", Arguments: `{"query":"test"}`},
				}},
			},
			FinishReason: "tool_calls",
		}},
	}

	result := convertOpenAIToAnthropic(resp, "claude-3")

	if result.StopReason != "tool_use" {
		t.Fatalf("stop_reason = %q, want tool_use", result.StopReason)
	}
	if len(result.Content) != 1 || result.Content[0].Type != "tool_use" {
		t.Fatalf("content = %+v", result.Content)
	}
	if result.Content[0].Name != "search" {
		t.Fatalf("tool name = %q, want search", result.Content[0].Name)
	}
}

func TestNonStreamProxyRoundTrip(t *testing.T) {
	// Mock upstream OpenAI server — verifies auth header format
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify upstream receives standard OpenAI Bearer auth
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			t.Errorf("upstream Authorization = %q, want %q", auth, "Bearer test-key")
		}

		body, _ := io.ReadAll(r.Body)
		var req openaiChatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("upstream received invalid JSON: %v", err)
			http.Error(w, "bad request", 400)
			return
		}
		if req.Messages[0].Role != "system" {
			t.Errorf("expected system message first, got %q", req.Messages[0].Role)
		}
		if req.MaxTokens != 1024 {
			t.Errorf("max_tokens = %d, want 1024", req.MaxTokens)
		}

		json.NewEncoder(w).Encode(openaiChatResponse{
			ID: "chatcmpl-test",
			Choices: []openaiChoice{{
				Message:      openaiMessage{Role: "assistant", Content: "Hi there!"},
				FinishReason: "stop",
			}},
		})
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetUpstream(upstream.URL, "fallback-key")

	// Send Anthropic-format request with x-api-key header (like Claude Code does)
	anthReq := `{
		"model": "claude-3",
		"system": "Be helpful",
		"messages": [{"role": "user", "content": "Hello"}],
		"max_tokens": 1024,
		"stream": false
	}`

	req, _ := http.NewRequest(http.MethodPost,
		"http://"+srv.Addr().String()+"/anthropic/v1/messages",
		strings.NewReader(anthReq))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "test-key") // Claude Code sends token here

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}

	var anthResp anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&anthResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if anthResp.Type != "message" {
		t.Fatalf("type = %q, want message", anthResp.Type)
	}
	if anthResp.StopReason != "end_turn" {
		t.Fatalf("stop_reason = %q, want end_turn", anthResp.StopReason)
	}
	if len(anthResp.Content) == 0 || anthResp.Content[0].Text != "Hi there!" {
		t.Fatalf("content = %+v", anthResp.Content)
	}
}

func TestResolveAPIKey_Priority(t *testing.T) {
	// x-api-key takes priority
	r, _ := http.NewRequest("POST", "/", nil)
	r.Header.Set("x-api-key", "from-xapi")
	r.Header.Set("Authorization", "Bearer from-bearer")
	if got := resolveAPIKey(r, "fallback"); got != "from-xapi" {
		t.Fatalf("got %q, want from-xapi", got)
	}

	// Authorization Bearer as fallback
	r2, _ := http.NewRequest("POST", "/", nil)
	r2.Header.Set("Authorization", "Bearer from-bearer")
	if got := resolveAPIKey(r2, "fallback"); got != "from-bearer" {
		t.Fatalf("got %q, want from-bearer", got)
	}

	// Server fallback when no headers
	r3, _ := http.NewRequest("POST", "/", nil)
	if got := resolveAPIKey(r3, "fallback"); got != "fallback" {
		t.Fatalf("got %q, want fallback", got)
	}
}
