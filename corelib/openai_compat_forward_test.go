package corelib

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

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

func TestForwardOpenAICompatRequestNormalizesCodeGenAutoModel(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		if got := body["model"]; got != CodeGenDefaultModelID {
			t.Fatalf("upstream model = %#v, want %q", got, CodeGenDefaultModelID)
		}
		resp := `{"id":"chatcmpl-test","object":"chat.completion","model":"qax-codegen/Auto","choices":[]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(resp)),
			Request:    req,
		}, nil
	})}

	_, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: "https://codegen.qianxin-inc.cn/api/v1", Model: "auto"}, map[string]any{
		"messages": []any{},
	}, client, "")
	if err != nil {
		t.Fatalf("ForwardOpenAICompatRequest() error = %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusOK)
	}
}

func TestForwardOpenAICompatRequestSanitizesCodeGenTools(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		tools := body["tools"].([]any)
		fn := tools[0].(map[string]any)["function"].(map[string]any)
		if _, ok := fn["strict"]; ok {
			t.Fatalf("strict leaked into CodeGen tool: %#v", fn)
		}
		params := fn["parameters"].(map[string]any)
		if _, ok := params["additionalProperties"]; ok {
			t.Fatalf("additionalProperties=false leaked into CodeGen schema: %#v", params)
		}
		props := params["properties"].(map[string]any)
		values := props["values"].(map[string]any)
		if got := values["items"].(map[string]any)["type"]; got != "string" {
			t.Fatalf("array items type = %#v, want string", got)
		}
		functions := body["functions"].([]any)
		legacyFn := functions[0].(map[string]any)
		if _, ok := legacyFn["strict"]; ok {
			t.Fatalf("legacy strict leaked into CodeGen function: %#v", legacyFn)
		}
		legacyParams := legacyFn["parameters"].(map[string]any)
		legacyIDs := legacyParams["properties"].(map[string]any)["ids"].(map[string]any)
		if got := legacyIDs["items"].(map[string]any)["type"]; got != "string" {
			t.Fatalf("legacy array items type = %#v, want string", got)
		}
		resp := `{"id":"chatcmpl-test","object":"chat.completion","model":"qax-codegen/Auto","choices":[]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(resp)),
			Request:    req,
		}, nil
	})}

	_, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: "https://codegen.qianxin-inc.cn/api/v1", Model: "auto"}, map[string]any{
		"messages": []any{},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":   "strict_tool",
				"strict": true,
				"parameters": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"values": map[string]any{"type": "array"},
					},
				},
			},
		}},
		"functions": []any{map[string]any{
			"name":   "legacy_function",
			"strict": true,
			"parameters": map[string]any{
				"properties": map[string]any{
					"ids": map[string]any{"type": "array"},
				},
			},
		}},
	}, client, "")
	if err != nil {
		t.Fatalf("ForwardOpenAICompatRequest() error = %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusOK)
	}
}

func TestForwardOpenAICompatRequestPreservesNonCodeGenToolSchema(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		tools := body["tools"].([]any)
		fn := tools[0].(map[string]any)["function"].(map[string]any)
		if got := fn["strict"]; got != true {
			t.Fatalf("strict = %#v, want true", got)
		}
		params := fn["parameters"].(map[string]any)
		if got := params["additionalProperties"]; got != false {
			t.Fatalf("additionalProperties = %#v, want false", got)
		}
		props := params["properties"].(map[string]any)
		values := props["values"].(map[string]any)
		if _, ok := values["items"]; ok {
			t.Fatalf("non-CodeGen array schema should not be patched: %#v", values)
		}
		if got := values["default"].([]any)[0]; got != "x" {
			t.Fatalf("default = %#v, want x", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "chatcmpl-test", "object": "chat.completion", "model": "strict-model", "choices": []any{}})
	}))
	defer server.Close()

	_, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: server.URL, Model: "strict-model"}, map[string]any{
		"messages": []any{},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":   "strict_tool",
				"strict": true,
				"parameters": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"values": map[string]any{"type": "array", "default": []any{"x"}},
					},
				},
			},
		}},
	}, server.Client(), "")
	if err != nil {
		t.Fatalf("ForwardOpenAICompatRequest() error = %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusOK)
	}
}

func TestForwardOpenAICompatStreamRequestNormalizesCodeGenAutoModel(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		if got := body["model"]; got != CodeGenDefaultModelID {
			t.Fatalf("upstream model = %#v, want %q", got, CodeGenDefaultModelID)
		}
		if got := body["stream"]; got != true {
			t.Fatalf("stream = %#v, want true", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(bytes.NewBufferString("data: [DONE]\n\n")),
			Request:    req,
		}, nil
	})}

	resp, err := ForwardOpenAICompatStreamRequest(context.Background(), MaclawLLMConfig{URL: "https://codegen.qianxin-inc.cn/api/v1", Model: "auto"}, map[string]any{
		"messages": []any{},
	}, client)
	if err != nil {
		t.Fatalf("ForwardOpenAICompatStreamRequest() error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestForwardOpenAICompatResponsesRequestNormalizesCodeGenAutoModel(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		if got := body["model"]; got != CodeGenDefaultModelID {
			t.Fatalf("upstream model = %#v, want %q", got, CodeGenDefaultModelID)
		}
		tools := body["tools"].([]any)
		flatTool := tools[0].(map[string]any)
		if _, ok := flatTool["strict"]; ok {
			t.Fatalf("Responses strict leaked into CodeGen tool: %#v", flatTool)
		}
		params := flatTool["parameters"].(map[string]any)
		if _, ok := params["additionalProperties"]; ok {
			t.Fatalf("Responses additionalProperties=false leaked: %#v", params)
		}
		values := params["properties"].(map[string]any)["values"].(map[string]any)
		if got := values["items"].(map[string]any)["type"]; got != "string" {
			t.Fatalf("Responses array items type = %#v, want string", got)
		}
		resp := `{"id":"resp-test","object":"response","model":"qax-codegen/Auto","output":[]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(resp)),
			Request:    req,
		}, nil
	})}

	_, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: "https://codegen.qianxin-inc.cn/api/v1", Model: "auto", WireAPI: "responses"}, map[string]any{
		"messages": []any{},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":   "strict_tool",
				"strict": true,
				"parameters": map[string]any{
					"additionalProperties": false,
					"properties": map[string]any{
						"values": map[string]any{"type": "array"},
					},
				},
			},
		}},
	}, client, "")
	if err != nil {
		t.Fatalf("ForwardOpenAICompatRequest() error = %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusOK)
	}
}

func TestForwardOpenAICompatRequestStripsClientProviderHints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		if _, ok := body["provider"]; ok {
			t.Fatalf("provider hint leaked upstream: %+v", body)
		}
		if _, ok := body["model_provider"]; ok {
			t.Fatalf("model_provider hint leaked upstream: %+v", body)
		}
		if body["model"] != "upstream-model" {
			t.Fatalf("model = %v, want upstream-model", body["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "chatcmpl-test", "object": "chat.completion", "model": "upstream-model", "choices": []any{}})
	}))
	defer server.Close()

	_, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: server.URL, Model: "upstream-model"}, map[string]any{
		"model":          "auto",
		"provider":       "openai",
		"model_provider": "openai",
		"messages":       []any{},
	}, server.Client(), "auto")
	if err != nil {
		t.Fatalf("ForwardOpenAICompatRequest() error = %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusOK)
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

func TestForwardOpenAICompatRequestAnthropicPreservesToolProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		messages := body["messages"].([]any)
		assistant := messages[1].(map[string]any)
		blocks := assistant["content"].([]any)
		toolUse := blocks[0].(map[string]any)
		if toolUse["type"] != "tool_use" || toolUse["name"] != "ssh" || toolUse["id"] != "call_1" {
			t.Fatalf("unexpected anthropic tool_use block: %#v", toolUse)
		}
		toolResultMsg := messages[2].(map[string]any)
		resultBlocks := toolResultMsg["content"].([]any)
		toolResult := resultBlocks[0].(map[string]any)
		if toolResult["type"] != "tool_result" || toolResult["tool_use_id"] != "call_1" {
			t.Fatalf("unexpected anthropic tool_result block: %#v", toolResult)
		}
		if tools, ok := body["tools"].([]any); !ok || len(tools) != 1 {
			t.Fatalf("tools not converted: %#v", body["tools"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_1",
			"stop_reason": "tool_use",
			"content": []any{map[string]any{
				"type":  "tool_use",
				"id":    "call_2",
				"name":  "ssh",
				"input": map[string]any{"action": "check_task"},
			}},
		})
	}))
	defer server.Close()

	body := toolRoundTripBody()
	respBody, status, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: server.URL, Model: "claude", Protocol: "anthropic"}, body, server.Client(), "auto")
	if err != nil || status != http.StatusOK {
		t.Fatalf("ForwardOpenAICompatRequest status=%d err=%v body=%s", status, err, respBody)
	}
	var resp map[string]any
	if err := json.Unmarshal(respBody, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	choice := resp["choices"].([]any)[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Fatalf("finish_reason = %v", choice["finish_reason"])
	}
	message := choice["message"].(map[string]any)
	if strings.Contains(message["content"].(string), "[Tool Call:") {
		t.Fatalf("response leaked textual tool call: %#v", message)
	}
	if calls := message["tool_calls"].([]any); len(calls) != 1 {
		t.Fatalf("tool_calls = %#v", calls)
	}
}

func TestForwardOpenAICompatRequestResponsesPreservesToolProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		input := body["input"].([]any)
		if input[1].(map[string]any)["type"] != "function_call" {
			t.Fatalf("assistant tool call was not converted: %#v", input[1])
		}
		if input[2].(map[string]any)["type"] != "function_call_output" {
			t.Fatalf("tool result was not converted: %#v", input[2])
		}
		if tools, ok := body["tools"].([]any); !ok || len(tools) != 1 {
			t.Fatalf("tools not converted: %#v", body["tools"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "resp_1",
			"output": []any{map[string]any{
				"type":      "function_call",
				"call_id":   "call_2",
				"name":      "ssh",
				"arguments": `{"action":"check_task"}`,
			}},
		})
	}))
	defer server.Close()

	body := toolRoundTripBody()
	respBody, status, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: server.URL, Model: "gpt", WireAPI: "responses"}, body, server.Client(), "auto")
	if err != nil || status != http.StatusOK {
		t.Fatalf("ForwardOpenAICompatRequest status=%d err=%v body=%s", status, err, respBody)
	}
	var resp map[string]any
	if err := json.Unmarshal(respBody, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	choice := resp["choices"].([]any)[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Fatalf("finish_reason = %v", choice["finish_reason"])
	}
	message := choice["message"].(map[string]any)
	if strings.Contains(message["content"].(string), "[Tool Call:") {
		t.Fatalf("response leaked textual tool call: %#v", message)
	}
	if calls := message["tool_calls"].([]any); len(calls) != 1 {
		t.Fatalf("tool_calls = %#v", calls)
	}
}

func toolRoundTripBody() map[string]any {
	return map[string]any{
		"model": "auto",
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "ssh",
				"description": "SSH tool",
				"parameters":  map[string]any{"type": "object"},
			},
		}},
		"messages": []any{
			map[string]any{"role": "user", "content": "check task"},
			map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{
				"id":   "call_1",
				"type": "function",
				"function": map[string]any{
					"name":      "ssh",
					"arguments": `{"action":"check_task"}`,
				},
			}}},
			map[string]any{"role": "tool", "tool_call_id": "call_1", "content": "still running"},
		},
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
