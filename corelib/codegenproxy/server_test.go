package codegenproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
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

func TestConvertOpenAIToAnthropic_LegacyFunctionCall(t *testing.T) {
	resp := openaiChatResponse{
		Choices: []openaiChoice{{
			Message: openaiMessage{
				Role: "assistant",
				FunctionCall: &openaiLegacyFunctionCall{
					Name:      "search",
					Arguments: `{"query":"test"}`,
				},
			},
			FinishReason: "function_call",
		}},
	}

	result := convertOpenAIToAnthropic(resp, "Qwen-Flash")

	if result.StopReason != "tool_use" {
		t.Fatalf("stop_reason = %q, want tool_use", result.StopReason)
	}
	if len(result.Content) != 1 || result.Content[0].Type != "tool_use" {
		t.Fatalf("content = %+v", result.Content)
	}
	if result.Content[0].Name != "search" || result.Content[0].Input["query"] != "test" {
		t.Fatalf("tool_use = %+v, want search query test", result.Content[0])
	}
}

func TestConvertOpenAIToAnthropic_SkipsInvalidToolArguments(t *testing.T) {
	resp := openaiChatResponse{
		Choices: []openaiChoice{{
			Message: openaiMessage{
				Role: "assistant",
				ToolCalls: []openaiToolCall{
					{
						ID: "call_array", Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{Name: "array_args", Arguments: `[]`},
					},
					{
						ID: "call_invalid", Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{Name: "invalid_args", Arguments: `{`},
					},
				},
			},
			FinishReason: "tool_calls",
		}},
	}

	result := convertOpenAIToAnthropic(resp, "Qwen-Flash")

	if result.StopReason != "end_turn" {
		t.Fatalf("stop_reason = %q, want end_turn", result.StopReason)
	}
	if len(result.Content) != 1 || result.Content[0].Type != "text" || result.Content[0].Text != "" {
		t.Fatalf("content = %+v, want empty text fallback", result.Content)
	}
}

func TestSetCodeGenUpstreamHeadersNormalizeLegacyClientName(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://codegen.qianxin-inc.cn/api/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	setCodeGenUpstreamHeaders(req, "openclaw")
	if got := req.Header.Get("User-Agent"); got != corelib.CodeGenClientName {
		t.Fatalf("User-Agent = %q, want %q", got, corelib.CodeGenClientName)
	}
	if got := req.Header.Get(corelib.CodeGenClientNameHeader); got != corelib.CodeGenClientName {
		t.Fatalf("%s = %q, want %q", corelib.CodeGenClientNameHeader, got, corelib.CodeGenClientName)
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
		if got := r.Header.Get("User-Agent"); got != "custom-agent" {
			t.Errorf("upstream User-Agent = %q, want %q", got, "custom-agent")
		}
		if got := r.Header.Get(corelib.CodeGenClientNameHeader); got != "custom-agent" {
			t.Errorf("upstream %s = %q, want %q", corelib.CodeGenClientNameHeader, got, "custom-agent")
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
	srv.SetUpstreamWithClientName(upstream.URL, "fallback-key", "custom-agent")

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

func TestAnthropicProxyPreservesProviderPrefixedModel(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req openaiChatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("upstream received invalid JSON: %v", err)
			http.Error(w, "bad request", 400)
			return
		}
		if req.Model != "qax-codegen/Qwen-Flash" {
			t.Errorf("upstream model = %q, want qax-codegen/Qwen-Flash", req.Model)
		}
		json.NewEncoder(w).Encode(openaiChatResponse{
			ID: "chatcmpl-prefixed",
			Choices: []openaiChoice{{
				Message:      openaiMessage{Role: "assistant", Content: "ok"},
				FinishReason: "stop",
			}},
		})
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetUpstream(upstream.URL, "fallback-key")

	anthReq := `{
		"model": "qax-codegen/Qwen-Flash",
		"messages": [{"role": "user", "content": "Hello"}],
		"max_tokens": 1024,
		"stream": false
	}`

	req, _ := http.NewRequest(http.MethodPost,
		"http://"+srv.Addr().String()+"/anthropic/v1/messages",
		strings.NewReader(anthReq))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
}

func TestAnthropicProxyResolvesShortModelAlias(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"qax-codegen/Qwen-Flash","name":"Qwen-Flash","provider":"qax-codegen"}]}`))
			return
		case "/chat/completions":
			body, _ := io.ReadAll(r.Body)
			var req openaiChatRequest
			if err := json.Unmarshal(body, &req); err != nil {
				t.Errorf("upstream received invalid JSON: %v", err)
				http.Error(w, "bad request", 400)
				return
			}
			if req.Model != "qax-codegen/Qwen-Flash" {
				t.Errorf("upstream model = %q, want qax-codegen/Qwen-Flash", req.Model)
			}
			json.NewEncoder(w).Encode(openaiChatResponse{
				ID: "chatcmpl-alias",
				Choices: []openaiChoice{{
					Message:      openaiMessage{Role: "assistant", Content: "ok"},
					FinishReason: "stop",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetUpstream(upstream.URL, "fallback-key")

	anthReq := `{
		"model": "Qwen-Flash",
		"messages": [{"role": "user", "content": "Hello"}],
		"max_tokens": 1024,
		"stream": false
	}`

	req, _ := http.NewRequest(http.MethodPost,
		"http://"+srv.Addr().String()+"/anthropic/v1/messages",
		strings.NewReader(anthReq))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
}

func TestAnthropicProxyClampsQwenFlashMaxTokens(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"Qwen-Flash","name":"Qwen-Flash"}]}`))
			return
		case "/chat/completions":
			body, _ := io.ReadAll(r.Body)
			var req openaiChatRequest
			if err := json.Unmarshal(body, &req); err != nil {
				t.Errorf("upstream received invalid JSON: %v", err)
				http.Error(w, "bad request", 400)
				return
			}
			if req.Model != "Qwen-Flash" {
				t.Errorf("upstream model = %q, want Qwen-Flash", req.Model)
			}
			if req.MaxTokens != codeGenQwenFlashMaxTokens {
				t.Errorf("max_tokens = %d, want %d", req.MaxTokens, codeGenQwenFlashMaxTokens)
			}
			if len(req.Tools) != 1 || req.Tools[0].Function.Name != "search" {
				t.Errorf("tools = %+v, want one search tool", req.Tools)
			}
			if len(req.Functions) != 0 {
				t.Errorf("functions count = %d, want 0", len(req.Functions))
			}
			parameters, ok := req.Tools[0].Function.Parameters.(map[string]interface{})
			if !ok {
				t.Fatalf("parameters = %#v, want object", req.Tools[0].Function.Parameters)
			}
			if _, ok := parameters["$schema"]; ok {
				t.Fatalf("parameters should remove $schema: %#v", parameters)
			}
			if _, ok := parameters["additionalProperties"]; ok {
				t.Fatalf("parameters should remove additionalProperties=false: %#v", parameters)
			}
			json.NewEncoder(w).Encode(openaiChatResponse{
				ID: "chatcmpl-qwen",
				Choices: []openaiChoice{{
					Message:      openaiMessage{Role: "assistant", Content: "ok"},
					FinishReason: "stop",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetUpstream(upstream.URL, "fallback-key")

	anthReq := `{
		"model": "Qwen-Flash",
		"messages": [{"role": "user", "content": "Hello"}],
		"max_tokens": 32000,
		"stream": false,
		"tools": [{"name":"search","description":"` + strings.Repeat("long ", 200) + `","input_schema":{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"properties":{"query":{"type":"string","description":"` + strings.Repeat("desc ", 120) + `"}}}}]
	}`

	req, _ := http.NewRequest(http.MethodPost,
		"http://"+srv.Addr().String()+"/anthropic/v1/messages",
		strings.NewReader(anthReq))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
}

func TestAnthropicProxySanitizesQwenFlashClaudeCodeSystemPrompt(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"Qwen-Flash","name":"Qwen-Flash"}]}`))
			return
		case "/chat/completions":
			body, _ := io.ReadAll(r.Body)
			var req openaiChatRequest
			if err := json.Unmarshal(body, &req); err != nil {
				t.Errorf("upstream received invalid JSON: %v", err)
				http.Error(w, "bad request", 400)
				return
			}
			if len(req.Messages) == 0 || req.Messages[0].Role != "system" {
				t.Fatalf("first message = %+v, want system", req.Messages)
			}
			systemText, _ := req.Messages[0].Content.(string)
			for _, forbidden := range []string{"x-anthropic-billing-header", "Claude Code", "Anthropic"} {
				if strings.Contains(systemText, forbidden) {
					t.Fatalf("system prompt leaked %q: %q", forbidden, systemText)
				}
			}
			if !strings.Contains(systemText, "TigerClaw Code") {
				t.Fatalf("system prompt = %q, want TigerClaw Code", systemText)
			}
			json.NewEncoder(w).Encode(openaiChatResponse{
				ID: "chatcmpl-qwen-system",
				Choices: []openaiChoice{{
					Message:      openaiMessage{Role: "assistant", Content: "ok"},
					FinishReason: "stop",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetUpstream(upstream.URL, "fallback-key")

	anthReq := `{
		"model": "Qwen-Flash",
		"system": "x-anthropic-billing-header: cc_version=2.1.168.a47\nYou are Claude Code, Anthropic's official CLI for Claude.\nUse tools well.",
		"messages": [{"role": "user", "content": "Hello"}],
		"max_tokens": 32000,
		"stream": false
	}`

	req, _ := http.NewRequest(http.MethodPost,
		"http://"+srv.Addr().String()+"/anthropic/v1/messages",
		strings.NewReader(anthReq))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
}

func TestAnthropicProxyKeepsGLMClaudeCodeSystemPrompt(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req openaiChatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("upstream received invalid JSON: %v", err)
			http.Error(w, "bad request", 400)
			return
		}
		systemText, _ := req.Messages[0].Content.(string)
		if !strings.Contains(systemText, "You are Claude Code") {
			t.Fatalf("system prompt = %q, want original Claude Code prompt", systemText)
		}
		json.NewEncoder(w).Encode(openaiChatResponse{
			ID: "chatcmpl-glm-system",
			Choices: []openaiChoice{{
				Message:      openaiMessage{Role: "assistant", Content: "ok"},
				FinishReason: "stop",
			}},
		})
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetUpstream(upstream.URL, "fallback-key")

	anthReq := `{
		"model": "GLM-5.1",
		"system": "x-anthropic-billing-header: cc_version=2.1.168.a47\nYou are Claude Code, Anthropic's official CLI for Claude.",
		"messages": [{"role": "user", "content": "Hello"}],
		"max_tokens": 32000,
		"stream": false
	}`

	req, _ := http.NewRequest(http.MethodPost,
		"http://"+srv.Addr().String()+"/anthropic/v1/messages",
		strings.NewReader(anthReq))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
}

func TestAnthropicProxyMergesAdjacentQwenFlashUserMessages(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"Qwen-Flash","name":"Qwen-Flash"}]}`))
			return
		case "/chat/completions":
			body, _ := io.ReadAll(r.Body)
			var req openaiChatRequest
			if err := json.Unmarshal(body, &req); err != nil {
				t.Errorf("upstream received invalid JSON: %v", err)
				http.Error(w, "bad request", 400)
				return
			}
			if len(req.Messages) != 2 {
				t.Fatalf("messages = %+v, want system plus merged user", req.Messages)
			}
			if req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
				t.Fatalf("roles = %+v, want system,user", req.Messages)
			}
			content, _ := req.Messages[1].Content.(string)
			if !strings.Contains(content, "first") || !strings.Contains(content, "second") {
				t.Fatalf("merged user content = %q, want first and second", content)
			}
			json.NewEncoder(w).Encode(openaiChatResponse{
				ID: "chatcmpl-qwen-merge",
				Choices: []openaiChoice{{
					Message:      openaiMessage{Role: "assistant", Content: "ok"},
					FinishReason: "stop",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetUpstream(upstream.URL, "fallback-key")

	anthReq := `{
		"model": "Qwen-Flash",
		"system": "You are helpful.",
		"messages": [
			{"role": "user", "content": "first"},
			{"role": "user", "content": "second"}
		],
		"max_tokens": 1024,
		"stream": false
	}`

	req, _ := http.NewRequest(http.MethodPost,
		"http://"+srv.Addr().String()+"/anthropic/v1/messages",
		strings.NewReader(anthReq))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
}

func TestAnthropicProxyDropsQwenFlashMidConversationSystemMessages(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"Qwen-Flash","name":"Qwen-Flash"}]}`))
			return
		case "/chat/completions":
			body, _ := io.ReadAll(r.Body)
			var req openaiChatRequest
			if err := json.Unmarshal(body, &req); err != nil {
				t.Errorf("upstream received invalid JSON: %v", err)
				http.Error(w, "bad request", 400)
				return
			}
			if len(req.Messages) != 2 {
				t.Fatalf("messages = %+v, want system,user", req.Messages)
			}
			if req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
				t.Fatalf("roles = %+v, want system,user", req.Messages)
			}
			for _, msg := range req.Messages {
				text := logContentText(msg.Content)
				for _, forbidden := range []string{"Claude Code", "Anthropic", "Skill tool"} {
					if strings.Contains(text, forbidden) {
						t.Fatalf("message leaked %q: %+v", forbidden, req.Messages)
					}
				}
			}
			json.NewEncoder(w).Encode(openaiChatResponse{
				ID: "chatcmpl-qwen-mid-system",
				Choices: []openaiChoice{{
					Message:      openaiMessage{Role: "assistant", Content: "ok"},
					FinishReason: "stop",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetUpstream(upstream.URL, "fallback-key")

	anthReq := `{
		"model": "Qwen-Flash",
		"system": "x-anthropic-billing-header: cc_version=2.1.168.a47\nYou are Claude Code, Anthropic's official CLI for Claude.",
		"messages": [
			{"role": "user", "content": "hello"},
			{"role": "system", "content": "The following skills are available for use with the Skill tool: Claude API / Anthropic SDK."}
		],
		"max_tokens": 1024,
		"stream": false
	}`

	req, _ := http.NewRequest(http.MethodPost,
		"http://"+srv.Addr().String()+"/anthropic/v1/messages",
		strings.NewReader(anthReq))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
}

func TestAnthropicProxyKeepsQwenFlashToolsForRepeatedToolHistory(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"Qwen-Flash","name":"Qwen-Flash"}]}`))
			return
		case "/chat/completions":
			body, _ := io.ReadAll(r.Body)
			var req openaiChatRequest
			if err := json.Unmarshal(body, &req); err != nil {
				t.Errorf("upstream received invalid JSON: %v", err)
				http.Error(w, "bad request", 400)
				return
			}
			if len(req.Tools) != 1 {
				t.Fatalf("tools count = %d, want 1; proxy must not end repeated tool history", len(req.Tools))
			}
			if got := countOpenAIToolResultMessages(req.Messages); got != codeGenQwenFlashToolLoopAfterToolResults {
				t.Fatalf("tool result messages = %d, want preserved %d", got, codeGenQwenFlashToolLoopAfterToolResults)
			}
			json.NewEncoder(w).Encode(openaiChatResponse{
				ID: "chatcmpl-qwen-repeated-tools",
				Choices: []openaiChoice{{
					Message:      openaiMessage{Role: "assistant", Content: "final"},
					FinishReason: "stop",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetUpstream(upstream.URL, "fallback-key")

	messages := []string{`{"role": "user", "content": "Use tools then answer"}`}
	for i := 0; i < codeGenQwenFlashToolLoopAfterToolResults; i++ {
		messages = append(messages,
			`{"role": "assistant", "content": [{"type":"tool_use","id":"call_`+string(rune('a'+i))+`","name":"search","input":{"q":"x"}}]}`,
			`{"role": "user", "content": [{"type":"tool_result","tool_use_id":"call_`+string(rune('a'+i))+`","content":"result"}]}`,
		)
	}
	anthReq := `{
		"model": "Qwen-Flash",
		"system": "You are Claude Code, Anthropic's official CLI for Claude.",
		"messages": [` + strings.Join(messages, ",") + `],
		"max_tokens": 1024,
		"stream": false,
		"tools": [{"name":"search","input_schema":{"type":"object","properties":{"q":{"type":"string"}}}}]
	}`

	req, _ := http.NewRequest(http.MethodPost,
		"http://"+srv.Addr().String()+"/anthropic/v1/messages",
		strings.NewReader(anthReq))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
}

func TestQwenFlashDoesNotFlagDistinctToolProgressAsLoop(t *testing.T) {
	messages := []openaiMessage{{Role: "user", Content: "Use tools then answer"}}
	for i := 0; i < codeGenQwenFlashToolLoopAfterToolResults; i++ {
		var tc openaiToolCall
		tc.ID = fmt.Sprintf("call_%d", i)
		tc.Type = "function"
		tc.Function.Name = "Read"
		tc.Function.Arguments = fmt.Sprintf(`{"file_path":"file_%d.go"}`, i)
		messages = append(messages,
			openaiMessage{Role: "assistant", ToolCalls: []openaiToolCall{tc}},
			openaiMessage{Role: "tool", ToolCallID: tc.ID, Content: "result"},
		)
	}

	toolResults, repeatedToolCalls, loop := detectQwenFlashRepeatedToolLoop(messages)
	if toolResults != codeGenQwenFlashToolLoopAfterToolResults {
		t.Fatalf("toolResults = %d, want %d", toolResults, codeGenQwenFlashToolLoopAfterToolResults)
	}
	if repeatedToolCalls != 1 {
		t.Fatalf("repeatedToolCalls = %d, want 1", repeatedToolCalls)
	}
	if loop {
		t.Fatalf("loop = true, want false for distinct tool progress")
	}
}

func TestAnthropicProxyBuffersSplitStreamToolCalls(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"Qwen-Flash","name":"Qwen-Flash"}]}`))
			return
		case "/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"file_path\\\":\"}}]}}]}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_write\",\"function\":{\"name\":\"Write\",\"arguments\":\"\\\"main.cpp\\\",\\\"content\\\":\\\"hi\\\"}\"}}]}}]}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetUpstream(upstream.URL, "fallback-key")

	anthReq := `{
		"model": "Qwen-Flash",
		"messages": [{"role": "user", "content": "write file"}],
		"max_tokens": 1024,
		"stream": true,
		"tools": [{"name":"Write","input_schema":{"type":"object","properties":{"file_path":{"type":"string"},"content":{"type":"string"}}}}]
	}`

	req, _ := http.NewRequest(http.MethodPost,
		"http://"+srv.Addr().String()+"/anthropic/v1/messages",
		strings.NewReader(anthReq))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	if !strings.Contains(text, `"name":"Write"`) {
		t.Fatalf("stream missing buffered tool name: %s", text)
	}
	if !strings.Contains(text, `"partial_json":"{\"file_path\":\"main.cpp\",\"content\":\"hi\"}"`) {
		t.Fatalf("stream missing complete buffered args: %s", text)
	}
	if strings.Contains(text, `"name":""`) {
		t.Fatalf("stream emitted empty tool name: %s", text)
	}
}

func TestAnthropicProxyStreamsLargeToolCallArguments(t *testing.T) {
	largeContent := strings.Repeat("x", 9*1024*1024)
	args, err := json.Marshal(map[string]string{
		"file_path": "large.txt",
		"content":   largeContent,
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"Qwen-Flash","name":"Qwen-Flash"}]}`))
			return
		case "/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			chunk, _ := json.Marshal(map[string]interface{}{
				"choices": []interface{}{map[string]interface{}{
					"delta": map[string]interface{}{
						"tool_calls": []interface{}{map[string]interface{}{
							"index": 0,
							"id":    "call_large",
							"function": map[string]interface{}{
								"name":      "Write",
								"arguments": string(args),
							},
						}},
					},
				}},
			})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", chunk)
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetUpstream(upstream.URL, "fallback-key")

	anthReq := `{
		"model": "Qwen-Flash",
		"messages": [{"role": "user", "content": "write large file"}],
		"max_tokens": 1024,
		"stream": true,
		"tools": [{"name":"Write","input_schema":{"type":"object","properties":{"file_path":{"type":"string"},"content":{"type":"string"}}}}]
	}`

	req, _ := http.NewRequest(http.MethodPost,
		"http://"+srv.Addr().String()+"/anthropic/v1/messages",
		strings.NewReader(anthReq))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	if !strings.Contains(text, `"name":"Write"`) {
		t.Fatalf("stream missing large tool name")
	}
	if !strings.Contains(text, `"partial_json":`) || !strings.Contains(text, `large.txt`) {
		t.Fatalf("stream missing large tool arguments")
	}
	if !strings.Contains(text, `"stop_reason":"tool_use"`) {
		t.Fatalf("stream stop_reason missing tool_use: tail=%s", truncate(text, 512))
	}
}

func TestAnthropicProxyParsesMultilineSSEDataEvent(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"Qwen-Flash","name":"Qwen-Flash"}]}`))
			return
		case "/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[\n"))
			_, _ = w.Write([]byte("data: {\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}\n"))
			_, _ = w.Write([]byte("data: ]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetUpstream(upstream.URL, "fallback-key")

	anthReq := `{
		"model": "Qwen-Flash",
		"messages": [{"role": "user", "content": "say hello"}],
		"max_tokens": 1024,
		"stream": true
	}`

	req, _ := http.NewRequest(http.MethodPost,
		"http://"+srv.Addr().String()+"/anthropic/v1/messages",
		strings.NewReader(anthReq))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	if !strings.Contains(text, `"text":"hello"`) {
		t.Fatalf("stream missing multiline content: %s", text)
	}
	if !strings.Contains(text, `"stop_reason":"end_turn"`) {
		t.Fatalf("stream stop_reason missing end_turn: %s", text)
	}
}

func TestShortSHA256DoesNotExposePayload(t *testing.T) {
	payload := `{"secret":"do-not-log-me","choices":[]}`
	got := shortSHA256(payload)
	if len(got) != 16 {
		t.Fatalf("short hash length = %d, want 16", len(got))
	}
	if strings.Contains(got, "do-not-log-me") || strings.Contains(got, "secret") {
		t.Fatalf("hash leaked payload: %s", got)
	}
}

func TestReadOpenAIStreamEventsReturnsReaderError(t *testing.T) {
	reader := io.MultiReader(
		strings.NewReader("data: {\"choices\":[]}\n\n"),
		errReader{err: io.ErrUnexpectedEOF},
	)
	seen := 0
	err := readOpenAIStreamEvents(reader, func(payload string) bool {
		seen++
		return true
	})
	if err == nil {
		t.Fatalf("err = nil, want reader error")
	}
	if seen != 1 {
		t.Fatalf("seen events = %d, want 1", seen)
	}
}

func TestAnthropicProxyStreamReadErrorEmitsErrorEvent(t *testing.T) {
	srv := NewServer(":0")
	body := io.NopCloser(io.MultiReader(
		strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"),
		errReader{err: io.ErrUnexpectedEOF},
	))
	upResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       body,
	}
	rec := httptest.NewRecorder()

	srv.handleStreamResponse(rec, upResp, "Qwen-Flash", "test-stream-error")

	text := rec.Body.String()
	if !strings.Contains(text, "event: error") {
		t.Fatalf("stream missing error event: %s", text)
	}
	if strings.Contains(text, "event: message_stop") {
		t.Fatalf("stream emitted message_stop after read error: %s", text)
	}
}

func TestAnthropicProxyStreamReadErrorDoesNotEmitPartialToolUse(t *testing.T) {
	srv := NewServer(":0")
	body := io.NopCloser(io.MultiReader(
		strings.NewReader(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_partial","function":{"name":"Write","arguments":"{\"file_path\":\"x.txt\",\"content\":\"unterminated"}}]}}]}`+"\n\n"),
		errReader{err: io.ErrUnexpectedEOF},
	))
	upResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       body,
	}
	rec := httptest.NewRecorder()

	srv.handleStreamResponse(rec, upResp, "Qwen-Flash", "test-partial-tool-error")

	text := rec.Body.String()
	if !strings.Contains(text, "event: error") {
		t.Fatalf("stream missing error event: %s", text)
	}
	if strings.Contains(text, `"type":"tool_use"`) {
		t.Fatalf("stream emitted partial tool_use after read error: %s", text)
	}
}

func TestAnthropicProxyInvalidToolArgumentsEmitError(t *testing.T) {
	srv := NewServer(":0")
	body := io.NopCloser(strings.NewReader(
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_bad\",\"function\":{\"name\":\"Write\",\"arguments\":\"{\\\"file_path\\\": \"}}]}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
			"data: [DONE]\n\n",
	))
	upResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       body,
	}
	rec := httptest.NewRecorder()

	srv.handleStreamResponse(rec, upResp, "Qwen-Flash", "test-invalid-tool-args")

	text := rec.Body.String()
	if !strings.Contains(text, "event: error") {
		t.Fatalf("stream missing error event: %s", text)
	}
	if strings.Contains(text, `"type":"tool_use"`) {
		t.Fatalf("stream emitted tool_use with invalid args: %s", text)
	}
	if strings.Contains(text, `"partial_json":"{}"`) {
		t.Fatalf("stream downgraded invalid args to empty object: %s", text)
	}
}

func TestAnthropicProxyNonObjectToolArgumentsEmitError(t *testing.T) {
	srv := NewServer(":0")
	body := io.NopCloser(strings.NewReader(
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_array\",\"function\":{\"name\":\"Write\",\"arguments\":\"[]\"}}]}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
			"data: [DONE]\n\n",
	))
	upResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       body,
	}
	rec := httptest.NewRecorder()

	srv.handleStreamResponse(rec, upResp, "Qwen-Flash", "test-non-object-tool-args")

	text := rec.Body.String()
	if !strings.Contains(text, "event: error") {
		t.Fatalf("stream missing error event: %s", text)
	}
	if strings.Contains(text, `"type":"tool_use"`) {
		t.Fatalf("stream emitted tool_use with non-object args: %s", text)
	}
}

func TestAnthropicProxyNonStreamInvalidToolArgumentsReturnsError(t *testing.T) {
	srv := NewServer(":0")
	resp := openaiChatResponse{
		ID: "chatcmpl-invalid-tool",
		Choices: []openaiChoice{{
			Message: openaiMessage{
				Role: "assistant",
				ToolCalls: []openaiToolCall{{
					ID:   "call_bad",
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      "Write",
						Arguments: "[]",
					},
				}},
			},
			FinishReason: "tool_calls",
		}},
	}
	body, _ := json.Marshal(resp)
	upResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
	rec := httptest.NewRecorder()

	srv.handleNonStreamResponse(rec, upResp, "Qwen-Flash", "test-nonstream-invalid-tool")

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadGateway, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "tool_use") {
		t.Fatalf("non-stream emitted tool_use with invalid args: %s", rec.Body.String())
	}
}

func TestAnthropicProxyInvalidStreamChunkEmitsError(t *testing.T) {
	srv := NewServer(":0")
	body := io.NopCloser(strings.NewReader(
		"data: {not-json}\n\n" +
			"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: [DONE]\n\n",
	))
	upResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       body,
	}
	rec := httptest.NewRecorder()

	srv.handleStreamResponse(rec, upResp, "Qwen-Flash", "test-invalid-stream-chunk")

	text := rec.Body.String()
	if !strings.Contains(text, "event: error") {
		t.Fatalf("stream missing error event: %s", text)
	}
	if strings.Contains(text, "event: message_stop") {
		t.Fatalf("stream emitted message_stop after invalid chunk: %s", text)
	}
}

type errReader struct {
	err error
}

func (r errReader) Read(_ []byte) (int, error) {
	return 0, r.err
}

func TestAnthropicProxyRetriesQwenFlashWithoutToolsOnBadRequest(t *testing.T) {
	chatAttempts := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"Qwen-Flash","name":"Qwen-Flash"}]}`))
			return
		case "/chat/completions":
			chatAttempts++
			body, _ := io.ReadAll(r.Body)
			var payload map[string]interface{}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Errorf("upstream received invalid JSON: %v", err)
				http.Error(w, "bad request", 400)
				return
			}
			if chatAttempts == 1 {
				if got := logArrayLen(payload["tools"]); got != 1 {
					t.Errorf("first attempt tools = %d, want 1", got)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"Bad Request","type":"upstream_error"}}`))
				return
			}
			if got := logArrayLen(payload["functions"]); got != 0 {
				t.Errorf("retry functions = %d, want 0", got)
			}
			if got := logArrayLen(payload["tools"]); got != 0 {
				t.Errorf("retry tools = %d, want 0", got)
			}
			json.NewEncoder(w).Encode(openaiChatResponse{
				ID: "chatcmpl-qwen-retry",
				Choices: []openaiChoice{{
					Message:      openaiMessage{Role: "assistant", Content: "ok"},
					FinishReason: "stop",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetUpstream(upstream.URL, "fallback-key")

	anthReq := `{
		"model": "Qwen-Flash",
		"messages": [{"role": "user", "content": "Hello"}],
		"max_tokens": 32000,
		"stream": false,
		"tools": [{"name":"search","input_schema":{"type":"object","properties":{"query":{"type":"string"}}}}]
	}`

	req, _ := http.NewRequest(http.MethodPost,
		"http://"+srv.Addr().String()+"/anthropic/v1/messages",
		strings.NewReader(anthReq))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	if chatAttempts != 2 {
		t.Fatalf("chat attempts = %d, want 2", chatAttempts)
	}
}

func TestOpenAIChatCompletionsProxySetsUpstreamUserAgent(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req openaiChatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("upstream received invalid JSON: %v", err)
			http.Error(w, "bad request", 400)
			return
		}
		if req.Model != "qax-codegen/Auto" {
			t.Errorf("upstream model = %q, want qax-codegen/Auto", req.Model)
		}
		if got := r.Header.Get("User-Agent"); got != corelib.CodeGenClientName {
			t.Errorf("upstream User-Agent = %q, want %q", got, corelib.CodeGenClientName)
		}
		if got := r.Header.Get(corelib.CodeGenClientNameHeader); got != corelib.CodeGenClientName {
			t.Errorf("upstream %s = %q, want %q", corelib.CodeGenClientNameHeader, got, corelib.CodeGenClientName)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-test","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetUpstream(upstream.URL, "fallback-key")

	req, _ := http.NewRequest(http.MethodPost,
		"http://"+srv.Addr().String()+"/v1/chat/completions",
		strings.NewReader(`{"model":"qax-codegen/Auto","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
}

func TestModelsProxySetsUpstreamUserAgent(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "custom-agent" {
			t.Errorf("upstream User-Agent = %q, want %q", got, "custom-agent")
		}
		if got := r.Header.Get(corelib.CodeGenClientNameHeader); got != "custom-agent" {
			t.Errorf("upstream %s = %q, want %q", corelib.CodeGenClientNameHeader, got, "custom-agent")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"qax-codegen/Auto","object":"model"}]}`))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetUpstreamWithClientName(upstream.URL, "fallback-key", "custom-agent")

	resp, err := http.Get("http://" + srv.Addr().String() + "/v1/models")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
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

func TestNormalizeModelsResponseOpenAIPreservesProviderPrefix(t *testing.T) {
	body := []byte(`{"data":[{"id":"qax-codegen/Qwen-Flash","name":"Qwen-Flash","provider":"qax-codegen"}]}`)
	got, err := normalizeModelsResponse(body, "openai")
	if err != nil {
		t.Fatalf("normalizeModelsResponse error: %v", err)
	}
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(got, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].ID != "qax-codegen/Qwen-Flash" {
		t.Fatalf("model id = %+v, want qax-codegen/Qwen-Flash", resp.Data)
	}
}

func TestNormalizeOpenAIModelInBodyPreservesProviderPrefix(t *testing.T) {
	got := normalizeOpenAIModelInBody([]byte(`{"model":"qax-codegen/Qwen-Flash","messages":[]}`))
	var payload map[string]interface{}
	if err := json.Unmarshal(got, &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if payload["model"] != "qax-codegen/Qwen-Flash" {
		t.Fatalf("model = %q, want qax-codegen/Qwen-Flash", payload["model"])
	}
}

func TestNormalizeModelsResponseAnthropicPreservesProviderPrefix(t *testing.T) {
	body := []byte(`{"models":[{"id":"qax-codegen/Qwen-Flash","name":"Qwen-Flash"}]}`)
	got, err := normalizeModelsResponse(body, "anthropic")
	if err != nil {
		t.Fatalf("normalizeModelsResponse error: %v", err)
	}
	var resp struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(got, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].ID != "qax-codegen/Qwen-Flash" || resp.Data[0].DisplayName != "Qwen-Flash" {
		t.Fatalf("model = %+v, want qax-codegen/Qwen-Flash", resp.Data)
	}
}

func TestTruncateForLogRedactsSensitiveFields(t *testing.T) {
	got := truncateForLog([]byte(`{
		"model": "qax-codegen/Qwen-Flash",
		"api_key": "sk-secret",
		"metadata": {"access_token": "tok-secret", "note": "keep-me"},
		"messages": [{"role": "user", "content": "hello"}]
	}`), 4096)

	for _, secret := range []string{"sk-secret", "tok-secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("log body leaked secret %q: %s", secret, got)
		}
	}
	for _, want := range []string{"qax-codegen/Qwen-Flash", "keep-me", "hello", "[REDACTED]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("log body missing %q: %s", want, got)
		}
	}
}
