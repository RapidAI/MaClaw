package corelib

import (
	"strings"
	"testing"
)

func TestOpenAIChatCompletionsEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{"bare domain", "https://api.deepseek.com", "https://api.deepseek.com/v1/chat/completions"},
		{"with /v1", "https://api.deepseek.com/v1", "https://api.deepseek.com/v1/chat/completions"},
		{"with /v1/", "https://api.deepseek.com/v1/", "https://api.deepseek.com/v1/chat/completions"},
		{"trailing slash", "https://api.example.com/", "https://api.example.com/v1/chat/completions"},
		{"custom path", "https://host/api/proxy", "https://host/api/proxy/v1/chat/completions"},
		{"custom path with /v1", "https://host/api/v1", "https://host/api/v1/chat/completions"},
		{"hub llm url", "https://hub.mypapers.top/api/llm/v1", "https://hub.mypapers.top/api/llm/v1/chat/completions"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := openAIChatCompletionsEndpoint(tt.baseURL)
			if got != tt.want {
				t.Errorf("openAIChatCompletionsEndpoint(%q) = %q, want %q", tt.baseURL, got, tt.want)
			}
		})
	}
}

func TestOpenAIResponsesEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{"bare domain", "https://api.openai.com", "https://api.openai.com/v1/responses"},
		{"with /v1", "https://api.openai.com/v1", "https://api.openai.com/v1/responses"},
		{"with /v1/", "https://api.openai.com/v1/", "https://api.openai.com/v1/responses"},
		{"trailing slash", "https://api.example.com/", "https://api.example.com/v1/responses"},
		{"custom path with /v1", "https://host/api/v1", "https://host/api/v1/responses"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := openAIResponsesEndpoint(tt.baseURL)
			if got != tt.want {
				t.Errorf("openAIResponsesEndpoint(%q) = %q, want %q", tt.baseURL, got, tt.want)
			}
		})
	}
}

func TestAnthropicMessagesEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{"bare domain", "https://api.anthropic.com", "https://api.anthropic.com/v1/messages"},
		{"with /v1", "https://host/api/v1", "https://host/api/v1/messages"},
		{"with /v1/", "https://host/api/v1/", "https://host/api/v1/messages"},
		{"anthropic path", "https://open.bigmodel.cn/api/anthropic", "https://open.bigmodel.cn/api/anthropic/v1/messages"},
		{"trailing slash", "https://api.example.com/", "https://api.example.com/v1/messages"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AnthropicMessagesEndpoint(tt.baseURL)
			if got != tt.want {
				t.Errorf("AnthropicMessagesEndpoint(%q) = %q, want %q", tt.baseURL, got, tt.want)
			}
		})
	}
}

func TestAppendV1Path(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		subPath string
		want    string
	}{
		{"no /v1, chat", "https://api.example.com", "/chat/completions", "https://api.example.com/v1/chat/completions"},
		{"has /v1, chat", "https://api.example.com/v1", "/chat/completions", "https://api.example.com/v1/chat/completions"},
		{"has /v1/, chat", "https://api.example.com/v1/", "/chat/completions", "https://api.example.com/v1/chat/completions"},
		{"no /v1, messages", "https://api.anthropic.com", "/messages", "https://api.anthropic.com/v1/messages"},
		{"has /v1, messages", "https://host/api/v1", "/messages", "https://host/api/v1/messages"},
		{"no /v1, responses", "https://api.openai.com", "/responses", "https://api.openai.com/v1/responses"},
		{"has /v1, responses", "https://api.openai.com/v1", "/responses", "https://api.openai.com/v1/responses"},
		{"nested /v1 in path", "https://hub.mypapers.top/api/llm/v1", "/chat/completions", "https://hub.mypapers.top/api/llm/v1/chat/completions"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appendV1Path(tt.baseURL, tt.subPath)
			if got != tt.want {
				t.Errorf("appendV1Path(%q, %q) = %q, want %q", tt.baseURL, tt.subPath, got, tt.want)
			}
		})
	}
}

func TestSanitizeToolMessages(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		got := sanitizeToolMessages(nil)
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("no tool messages passes through", func(t *testing.T) {
		msgs := []interface{}{
			map[string]interface{}{"role": "user", "content": "hello"},
			map[string]interface{}{"role": "assistant", "content": "hi"},
		}
		result := sanitizeToolMessages(msgs)
		out, ok := result.([]interface{})
		if !ok {
			t.Fatal("expected []interface{}")
		}
		if len(out) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(out))
		}
		// Should be the same messages (pass-through)
		m0 := out[0].(map[string]interface{})
		if m0["role"] != "user" {
			t.Errorf("msg[0] role = %v, want user", m0["role"])
		}
	})

	t.Run("tool role converted to user", func(t *testing.T) {
		msgs := []interface{}{
			map[string]interface{}{"role": "tool", "content": "result data", "name": "web_search", "tool_call_id": "call_123"},
		}
		result := sanitizeToolMessages(msgs)
		out := result.([]interface{})
		if len(out) != 1 {
			t.Fatalf("expected 1 message, got %d", len(out))
		}
		m := out[0].(map[string]interface{})
		if m["role"] != "user" {
			t.Errorf("role = %v, want user", m["role"])
		}
		content := m["content"].(string)
		if content != "[Tool Result: web_search] result data" {
			t.Errorf("content = %q", content)
		}
		// Should not have tool_call_id
		if _, has := m["tool_call_id"]; has {
			t.Error("should not have tool_call_id")
		}
	})

	t.Run("assistant with tool_calls converted", func(t *testing.T) {
		msgs := []interface{}{
			map[string]interface{}{
				"role":    "assistant",
				"content": "",
				"tool_calls": []interface{}{
					map[string]interface{}{
						"id":   "call_1",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "read_file",
							"arguments": `{"path":"main.go"}`,
						},
					},
				},
			},
		}
		result := sanitizeToolMessages(msgs)
		out := result.([]interface{})
		m := out[0].(map[string]interface{})
		if m["role"] != "assistant" {
			t.Errorf("role = %v, want assistant", m["role"])
		}
		content := m["content"].(string)
		if content != `[Tool Call: read_file] {"path":"main.go"}` {
			t.Errorf("content = %q", content)
		}
		if _, has := m["tool_calls"]; has {
			t.Error("should not have tool_calls after sanitization")
		}
	})

	t.Run("full conversation with tool calling", func(t *testing.T) {
		msgs := []interface{}{
			map[string]interface{}{"role": "system", "content": "You are helpful."},
			map[string]interface{}{"role": "user", "content": "search for Go tutorials"},
			map[string]interface{}{
				"role": "assistant", "content": "",
				"tool_calls": []interface{}{
					map[string]interface{}{
						"id": "call_1", "type": "function",
						"function": map[string]interface{}{"name": "web_search", "arguments": `{"q":"Go tutorials"}`},
					},
				},
			},
			map[string]interface{}{"role": "tool", "content": "Found 10 results...", "name": "web_search", "tool_call_id": "call_1"},
			map[string]interface{}{"role": "assistant", "content": "Here are some Go tutorials..."},
		}
		result := sanitizeToolMessages(msgs)
		out := result.([]interface{})
		if len(out) != 5 {
			t.Fatalf("expected 5 messages, got %d", len(out))
		}
		// system unchanged
		if out[0].(map[string]interface{})["role"] != "system" {
			t.Error("msg[0] should be system")
		}
		// user unchanged
		if out[1].(map[string]interface{})["role"] != "user" {
			t.Error("msg[1] should be user")
		}
		// assistant tool_calls → plain assistant
		m2 := out[2].(map[string]interface{})
		if m2["role"] != "assistant" {
			t.Error("msg[2] should be assistant")
		}
		if _, has := m2["tool_calls"]; has {
			t.Error("msg[2] should not have tool_calls")
		}
		// tool → user
		m3 := out[3].(map[string]interface{})
		if m3["role"] != "user" {
			t.Errorf("msg[3] role = %v, want user", m3["role"])
		}
		// final assistant unchanged
		if out[4].(map[string]interface{})["role"] != "assistant" {
			t.Error("msg[4] should be assistant")
		}
	})

	t.Run("consecutive tool messages merged into single user", func(t *testing.T) {
		// assistant calls 2 tools → 2 tool results → should merge into 1 user message
		msgs := []interface{}{
			map[string]interface{}{"role": "user", "content": "do two things"},
			map[string]interface{}{
				"role": "assistant", "content": "",
				"tool_calls": []interface{}{
					map[string]interface{}{
						"id": "call_1", "type": "function",
						"function": map[string]interface{}{"name": "tool_a", "arguments": "{}"},
					},
					map[string]interface{}{
						"id": "call_2", "type": "function",
						"function": map[string]interface{}{"name": "tool_b", "arguments": "{}"},
					},
				},
			},
			map[string]interface{}{"role": "tool", "content": "result A", "name": "tool_a", "tool_call_id": "call_1"},
			map[string]interface{}{"role": "tool", "content": "result B", "name": "tool_b", "tool_call_id": "call_2"},
			map[string]interface{}{"role": "assistant", "content": "Done."},
		}
		result := sanitizeToolMessages(msgs)
		out := result.([]interface{})
		// user, assistant, merged_user(A+B), assistant = 4 messages
		if len(out) != 4 {
			t.Fatalf("expected 4 messages (consecutive tools merged), got %d", len(out))
		}
		// The merged user message should contain both tool results
		merged := out[2].(map[string]interface{})
		if merged["role"] != "user" {
			t.Errorf("merged msg role = %v, want user", merged["role"])
		}
		content := merged["content"].(string)
		if !strings.Contains(content, "tool_a") || !strings.Contains(content, "tool_b") {
			t.Errorf("merged content should contain both tool names, got %q", content)
		}
		if !strings.Contains(content, "result A") || !strings.Contains(content, "result B") {
			t.Errorf("merged content should contain both results, got %q", content)
		}
	})
}

func TestSummarizeToolCalls(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		if got := summarizeToolCalls(nil); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("single call", func(t *testing.T) {
		calls := []interface{}{
			map[string]interface{}{
				"function": map[string]interface{}{"name": "bash", "arguments": `{"cmd":"ls"}`},
			},
		}
		got := summarizeToolCalls(calls)
		if got != `[Tool Call: bash] {"cmd":"ls"}` {
			t.Errorf("got %q", got)
		}
	})

	t.Run("long arguments truncated", func(t *testing.T) {
		longArgs := `{"content":"` + string(make([]byte, 300)) + `"}`
		calls := []interface{}{
			map[string]interface{}{
				"function": map[string]interface{}{"name": "write_file", "arguments": longArgs},
			},
		}
		got := summarizeToolCalls(calls)
		if len(got) > 250 {
			t.Errorf("expected truncated output, got len=%d", len(got))
		}
	})
}
