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
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	llmcompat "github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	openai "github.com/openai/openai-go"
	openaioption "github.com/openai/openai-go/option"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"
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

func TestConvertAnthropicToOpenAI_ObjectToolResultContent(t *testing.T) {
	req := anthropicRequest{
		Model: "claude-3",
		Messages: []anthropicMessage{
			{Role: "user", Content: []interface{}{
				map[string]interface{}{
					"type":        "tool_result",
					"tool_use_id": "call_json",
					"content":     map[string]interface{}{"ok": true},
				},
			}},
		},
	}

	result := convertAnthropicToOpenAI(req)
	if len(result.Messages) != 1 {
		t.Fatalf("messages count = %d, want 1", len(result.Messages))
	}
	if result.Messages[0].Role != "tool" || result.Messages[0].ToolCallID != "call_json" {
		t.Fatalf("tool message = %+v", result.Messages[0])
	}
	if got := result.Messages[0].Content; got != `{"ok":true}` {
		t.Fatalf("tool result content = %q, want JSON object", got)
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

func TestConvertOpenAIToAnthropic_ContentToolCall(t *testing.T) {
	resp := openaiChatResponse{
		ID: "chatcmpl-content-tool",
		Choices: []openaiChoice{{
			Message: openaiMessage{
				Role: "assistant",
				Content: `<turn: tool_call>
<invoke name="read_file">
<parameter name="path" string="true">README.md</parameter>
</invoke>
</turn>`,
			},
			FinishReason: "stop",
		}},
	}

	result := convertOpenAIToAnthropic(resp, "Qwen-Flash")

	if result.StopReason != "tool_use" {
		t.Fatalf("stop_reason = %q, want tool_use", result.StopReason)
	}
	if len(result.Content) != 1 || result.Content[0].Type != "tool_use" {
		t.Fatalf("content = %+v, want one tool_use", result.Content)
	}
	if result.Content[0].Name != "read_file" {
		t.Fatalf("tool name = %q, want read_file", result.Content[0].Name)
	}
	if result.Content[0].Text != "" {
		t.Fatalf("raw content leaked as text: %q", result.Content[0].Text)
	}
	if result.Content[0].Input["path"] != "README.md" {
		t.Fatalf("tool input = %+v, want README path", result.Content[0].Input)
	}
}

func TestConvertOpenAIToAnthropic_BareJSONFunctionStringContentToolCall(t *testing.T) {
	resp := openaiChatResponse{
		ID: "chatcmpl-content-tool-json-function",
		Choices: []openaiChoice{{
			Message: openaiMessage{
				Role:    "assistant",
				Content: `{"function":"read_file","args":{"path":"README.md"}}`,
			},
			FinishReason: "stop",
		}},
	}

	result := convertOpenAIToAnthropic(resp, "Qwen-Flash")

	if result.StopReason != "tool_use" {
		t.Fatalf("stop_reason = %q, want tool_use", result.StopReason)
	}
	if len(result.Content) != 1 || result.Content[0].Type != "tool_use" {
		t.Fatalf("content = %+v, want one tool_use", result.Content)
	}
	if result.Content[0].Name != "read_file" || result.Content[0].Input["path"] != "README.md" {
		t.Fatalf("tool_use = %+v, want read_file README", result.Content[0])
	}
}

func TestMayContainContentToolCall(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "plain text", content: "Here is the summary, no tools needed.", want: false},
		{name: "xml", content: `<tool_call>{"function":{"name":"read_file","arguments":{"path":"README.md"}}}</tool_call>`, want: true},
		{name: "codex invoke", content: `<turn: tool_call><invoke name="read_file"></invoke></turn>`, want: true},
		{name: "plain marker", content: `TOOL_CALL {"name":"read_file","arguments":{"path":"README.md"}}`, want: true},
		{name: "bare json", content: `{"name":"read_file","arguments":{"path":"README.md"}}`, want: true},
		{name: "bare function string", content: `{"function":"read_file","args":{"path":"README.md"}}`, want: true},
		{name: "bare tool alias", content: `{"tool_name":"read_file","input":{"path":"README.md"}}`, want: true},
		{name: "legacy function call", content: `{"function_call":{"name":"read_file","arguments":{"path":"README.md"}}}`, want: true},
		{name: "fenced json", content: "```json\n{\"name\":\"read_file\",\"arguments\":{\"path\":\"README.md\"}}\n```", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mayContainContentToolCall(tc.content); got != tc.want {
				t.Fatalf("mayContainContentToolCall() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConvertOpenAIToAnthropic_MalformedContentToolCallDoesNotLeakRawText(t *testing.T) {
	resp := openaiChatResponse{
		ID: "chatcmpl-malformed-content-tool",
		Choices: []openaiChoice{{
			Message: openaiMessage{
				Role:    "assistant",
				Content: `<tool_call>{"function":{"name":"read_file","arguments":{bad-json}}</tool_call>`,
			},
			FinishReason: "stop",
		}},
	}

	result := convertOpenAIToAnthropic(resp, "Qwen-Flash")

	if result.StopReason != "end_turn" {
		t.Fatalf("stop_reason = %q, want end_turn", result.StopReason)
	}
	if len(result.Content) != 1 || result.Content[0].Type != "text" {
		t.Fatalf("content = %+v, want one text block", result.Content)
	}
	if result.Content[0].Text != llmcompat.MalformedContentToolCallErrorMsg {
		t.Fatalf("text = %q, want malformed tool-call message", result.Content[0].Text)
	}
	if strings.Contains(result.Content[0].Text, "<tool_call>") || strings.Contains(result.Content[0].Text, "bad-json") {
		t.Fatalf("raw malformed tool call leaked: %q", result.Content[0].Text)
	}
}

func TestConvertOpenAIToAnthropic_InvalidContentToolArgumentsDoNotLeakRawText(t *testing.T) {
	resp := openaiChatResponse{
		ID: "chatcmpl-invalid-content-tool-args",
		Choices: []openaiChoice{{
			Message: openaiMessage{
				Role:    "assistant",
				Content: `<tool_call>{"function":{"name":"read_file","arguments":[]}}</tool_call>`,
			},
			FinishReason: "stop",
		}},
	}

	result := convertOpenAIToAnthropic(resp, "Qwen-Flash")

	if result.StopReason != "end_turn" {
		t.Fatalf("stop_reason = %q, want end_turn", result.StopReason)
	}
	if len(result.Content) != 1 || result.Content[0].Type != "text" {
		t.Fatalf("content = %+v, want one text block", result.Content)
	}
	if result.Content[0].Text != llmcompat.MalformedContentToolCallErrorMsg {
		t.Fatalf("text = %q, want malformed tool-call message", result.Content[0].Text)
	}
	if strings.Contains(result.Content[0].Text, "<tool_call>") || strings.Contains(result.Content[0].Text, "arguments") {
		t.Fatalf("raw invalid tool call leaked: %q", result.Content[0].Text)
	}
}

func TestConvertOpenAIToAnthropic_MixedMalformedContentToolCallDoesNotExecutePartial(t *testing.T) {
	resp := openaiChatResponse{
		ID: "chatcmpl-mixed-content-tool",
		Choices: []openaiChoice{{
			Message: openaiMessage{
				Role: "assistant",
				Content: `<tool_call>{"function":{"name":"read_file","arguments":{"path":"README.md"}}}</tool_call>
<tool_call>{"function":{"name":"write_file","arguments":{bad-json}}</tool_call>`,
			},
			FinishReason: "stop",
		}},
	}

	result := convertOpenAIToAnthropic(resp, "Qwen-Flash")

	if result.StopReason != "end_turn" {
		t.Fatalf("stop_reason = %q, want end_turn", result.StopReason)
	}
	if len(result.Content) != 1 || result.Content[0].Type != "text" {
		t.Fatalf("content = %+v, want one text block", result.Content)
	}
	if result.Content[0].Text != llmcompat.MalformedContentToolCallErrorMsg {
		t.Fatalf("text = %q, want malformed tool-call message", result.Content[0].Text)
	}
}

func TestConvertOpenAIToAnthropic_StandardToolCallWinsOverMalformedContentToolCall(t *testing.T) {
	resp := openaiChatResponse{
		ID: "chatcmpl-standard-tool-wins",
		Choices: []openaiChoice{{
			Message: openaiMessage{
				Role:    "assistant",
				Content: `<tool_call>{"function":{"name":"write_file","arguments":{bad-json}}</tool_call>`,
				ToolCalls: []openaiToolCall{{
					ID:   "call_read",
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      "read_file",
						Arguments: `{"path":"README.md"}`,
					},
				}},
			},
			FinishReason: "tool_calls",
		}},
	}

	result := convertOpenAIToAnthropic(resp, "Qwen-Flash")

	if result.StopReason != "tool_use" {
		t.Fatalf("stop_reason = %q, want tool_use", result.StopReason)
	}
	if len(result.Content) != 1 || result.Content[0].Type != "tool_use" {
		t.Fatalf("content = %+v, want one tool_use", result.Content)
	}
	if result.Content[0].ID != "call_read" || result.Content[0].Name != "read_file" {
		t.Fatalf("tool_use = %+v, want call_read/read_file", result.Content[0])
	}
	if result.Content[0].Input["path"] != "README.md" {
		t.Fatalf("tool input = %+v, want README path", result.Content[0].Input)
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

func TestConvertOpenAIToAnthropic_UnwrapsEncodedToolArguments(t *testing.T) {
	resp := openaiChatResponse{
		Choices: []openaiChoice{{
			Message: openaiMessage{
				Role: "assistant",
				ToolCalls: []openaiToolCall{{
					ID: "call_encoded", Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "Write", Arguments: `"{\"file_path\":\"main.cpp\",\"content\":\"hi\"}"`},
				}},
			},
			FinishReason: "tool_calls",
		}},
	}

	result := convertOpenAIToAnthropic(resp, "Qwen-Flash")

	if result.StopReason != "tool_use" {
		t.Fatalf("stop_reason = %q, want tool_use", result.StopReason)
	}
	if len(result.Content) != 1 || result.Content[0].Type != "tool_use" {
		t.Fatalf("content = %+v", result.Content)
	}
	if result.Content[0].Input["file_path"] != "main.cpp" || result.Content[0].Input["content"] != "hi" {
		t.Fatalf("tool input = %+v, want unwrapped object", result.Content[0].Input)
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

func TestAnthropicSDKClientCanCallCodeGenProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer fallback-key" {
			t.Errorf("upstream Authorization = %q, want Bearer fallback-key", got)
		}
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
		if len(req.Messages) != 1 || req.Messages[0].Role != "user" {
			t.Errorf("messages = %+v, want one user message", req.Messages)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-anthropic-sdk","choices":[{"message":{"role":"assistant","content":"anthropic sdk ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetClientAPIKey("local-key")
	srv.SetUpstream(upstream.URL, "fallback-key")

	client := anthropic.NewClient(
		anthropicoption.WithBaseURL("http://"+srv.Addr().String()+"/anthropic"),
		anthropicoption.WithAPIKey("local-key"),
	)
	msg, err := client.Messages.New(context.Background(), anthropic.MessageNewParams{
		Model:     "qax-codegen/Auto",
		MaxTokens: 128,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("hi")),
		},
	})
	if err != nil {
		t.Fatalf("anthropic SDK request failed: %v", err)
	}
	if msg.ID != "chatcmpl-anthropic-sdk" {
		t.Fatalf("message id = %q, want chatcmpl-anthropic-sdk", msg.ID)
	}
	if len(msg.Content) != 1 || msg.Content[0].Text != "anthropic sdk ok" {
		t.Fatalf("message content = %+v, want anthropic sdk ok", msg.Content)
	}
	if msg.Usage.InputTokens != 3 || msg.Usage.OutputTokens != 4 {
		t.Fatalf("usage = %+v, want input 3 output 4", msg.Usage)
	}
}

func TestAnthropicSDKClientCanStreamFromCodeGenProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req openaiChatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("upstream received invalid JSON: %v", err)
			http.Error(w, "bad request", 400)
			return
		}
		if !req.Stream {
			t.Errorf("stream = false, want true")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\" sdk\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetClientAPIKey("local-key")
	srv.SetUpstream(upstream.URL, "fallback-key")

	client := anthropic.NewClient(
		anthropicoption.WithBaseURL("http://"+srv.Addr().String()+"/anthropic"),
		anthropicoption.WithAPIKey("local-key"),
	)
	stream := client.Messages.NewStreaming(context.Background(), anthropic.MessageNewParams{
		Model:     "qax-codegen/Auto",
		MaxTokens: 128,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("hi")),
		},
	})

	var msg anthropic.Message
	for stream.Next() {
		if err := msg.Accumulate(stream.Current()); err != nil {
			t.Fatalf("accumulate stream event: %v", err)
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("anthropic SDK stream failed: %v", err)
	}
	if msg.StopReason != "end_turn" {
		t.Fatalf("stop_reason = %q, want end_turn", msg.StopReason)
	}
	if len(msg.Content) != 1 || msg.Content[0].Text != "hello sdk" {
		t.Fatalf("content = %+v, want hello sdk", msg.Content)
	}
}

func TestAnthropicSDKClientCanStreamToolUseFromCodeGenProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req openaiChatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("upstream received invalid JSON: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if !req.Stream {
			t.Errorf("stream = false, want true")
		}
		if len(req.Tools) != 1 || req.Tools[0].Function.Name != "read_file" {
			t.Fatalf("upstream tools = %+v, want read_file", req.Tools)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_read\",\"type\":\"function\",\"function\":{\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\"}}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"README.md\\\"}\"}}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetClientAPIKey("local-key")
	srv.SetUpstream(upstream.URL, "fallback-key")

	client := anthropic.NewClient(
		anthropicoption.WithBaseURL("http://"+srv.Addr().String()+"/anthropic"),
		anthropicoption.WithAPIKey("local-key"),
	)
	stream := client.Messages.NewStreaming(context.Background(), anthropic.MessageNewParams{
		Model:     "qax-codegen/Auto",
		MaxTokens: 128,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("read README")),
		},
		Tools: []anthropic.ToolUnionParam{
			anthropic.ToolUnionParamOfTool(anthropic.ToolInputSchemaParam{
				Properties: map[string]interface{}{
					"path": map[string]interface{}{"type": "string"},
				},
			}, "read_file"),
		},
	})

	var msg anthropic.Message
	for stream.Next() {
		if err := msg.Accumulate(stream.Current()); err != nil {
			t.Fatalf("accumulate stream event: %v", err)
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("anthropic SDK stream failed: %v", err)
	}
	if msg.StopReason != "tool_use" {
		t.Fatalf("stop_reason = %q, want tool_use; message=%s", msg.StopReason, msg.RawJSON())
	}
	if len(msg.Content) != 1 || msg.Content[0].Type != "tool_use" {
		t.Fatalf("content = %+v, want one tool_use", msg.Content)
	}
	toolUse := msg.Content[0].AsToolUse()
	if toolUse.ID != "call_read" || toolUse.Name != "read_file" {
		t.Fatalf("tool_use id/name = %q/%q, want call_read/read_file", toolUse.ID, toolUse.Name)
	}
	if string(toolUse.Input) != `{"path":"README.md"}` {
		t.Fatalf("tool input = %s, want README path", toolUse.Input)
	}
}

func TestAnthropicSDKClientCanStreamContentToolUseFromCodeGenProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req openaiChatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("upstream received invalid JSON: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if !req.Stream {
			t.Errorf("stream = false, want true")
		}
		if len(req.Tools) != 1 || req.Tools[0].Function.Name != "read_file" {
			t.Fatalf("upstream tools = %+v, want read_file", req.Tools)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"<turn: tool_call>\\n<invoke name=\\\"read_file\\\">\\n\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"<parameter name=\\\"path\\\" string=\\\"true\\\">README.md</parameter>\\n\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"</invoke>\\n</turn>\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetClientAPIKey("local-key")
	srv.SetUpstream(upstream.URL, "fallback-key")

	client := anthropic.NewClient(
		anthropicoption.WithBaseURL("http://"+srv.Addr().String()+"/anthropic"),
		anthropicoption.WithAPIKey("local-key"),
	)
	stream := client.Messages.NewStreaming(context.Background(), anthropic.MessageNewParams{
		Model:     "qax-codegen/Auto",
		MaxTokens: 128,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("read README")),
		},
		Tools: []anthropic.ToolUnionParam{
			anthropic.ToolUnionParamOfTool(anthropic.ToolInputSchemaParam{
				Properties: map[string]interface{}{
					"path": map[string]interface{}{"type": "string"},
				},
			}, "read_file"),
		},
	})

	var msg anthropic.Message
	for stream.Next() {
		if err := msg.Accumulate(stream.Current()); err != nil {
			t.Fatalf("accumulate stream event: %v", err)
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("anthropic SDK stream failed: %v", err)
	}
	if msg.StopReason != "tool_use" {
		t.Fatalf("stop_reason = %q, want tool_use; message=%s", msg.StopReason, msg.RawJSON())
	}
	if len(msg.Content) != 1 || msg.Content[0].Type != "tool_use" {
		t.Fatalf("content = %+v, want one tool_use", msg.Content)
	}
	toolUse := msg.Content[0].AsToolUse()
	if toolUse.Name != "read_file" {
		t.Fatalf("tool name = %q, want read_file", toolUse.Name)
	}
	if string(toolUse.Input) != `{"path":"README.md"}` {
		t.Fatalf("tool input = %s, want README path", toolUse.Input)
	}
}

func TestAnthropicProxyMalformedStreamContentToolCallEmitsError(t *testing.T) {
	srv := NewServer(":0")
	body := io.NopCloser(strings.NewReader(
		"data: {\"choices\":[{\"delta\":{\"content\":\"<tool_call>{\\\"function\\\":{\\\"name\\\":\\\"read_file\\\",\\\"arguments\\\":{bad-json}}</tool_call>\"}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: [DONE]\n\n",
	))
	upResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       body,
	}
	rec := httptest.NewRecorder()
	toolSchemas := map[string]toolSchemaSummary{
		"read_file": {},
	}

	srv.handleStreamResponse(rec, upResp, "Qwen-Flash", "test-malformed-content-tool", toolSchemas)

	text := rec.Body.String()
	if !strings.Contains(text, "event: error") {
		t.Fatalf("stream missing error event: %s", text)
	}
	if strings.Contains(text, "bad-json") || strings.Contains(text, "<tool_call>") {
		t.Fatalf("stream leaked malformed tool call: %s", text)
	}
	if strings.Contains(text, "event: message_stop") {
		t.Fatalf("stream emitted message_stop after malformed content tool call: %s", text)
	}
}

func TestAnthropicProxyInvalidStreamContentToolArgumentsEmitError(t *testing.T) {
	srv := NewServer(":0")
	body := io.NopCloser(strings.NewReader(
		"data: {\"choices\":[{\"delta\":{\"content\":\"<tool_call>{\\\"function\\\":{\\\"name\\\":\\\"read_file\\\",\\\"arguments\\\":[]}}</tool_call>\"}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: [DONE]\n\n",
	))
	upResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       body,
	}
	rec := httptest.NewRecorder()
	toolSchemas := map[string]toolSchemaSummary{
		"read_file": {},
	}

	srv.handleStreamResponse(rec, upResp, "Qwen-Flash", "test-invalid-content-tool-args", toolSchemas)

	text := rec.Body.String()
	if !strings.Contains(text, "event: error") {
		t.Fatalf("stream missing error event: %s", text)
	}
	if strings.Contains(text, `"type":"tool_use"`) || strings.Contains(text, "<tool_call>") {
		t.Fatalf("stream emitted or leaked invalid content tool call: %s", text)
	}
}

func TestAnthropicProxyMixedMalformedStreamContentToolCallDoesNotExecutePartial(t *testing.T) {
	srv := NewServer(":0")
	body := io.NopCloser(strings.NewReader(
		"data: {\"choices\":[{\"delta\":{\"content\":\"<tool_call>{\\\"function\\\":{\\\"name\\\":\\\"read_file\\\",\\\"arguments\\\":{\\\"path\\\":\\\"README.md\\\"}}}</tool_call>\\n\"}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{\"content\":\"<tool_call>{\\\"function\\\":{\\\"name\\\":\\\"write_file\\\",\\\"arguments\\\":{bad-json}}</tool_call>\"}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: [DONE]\n\n",
	))
	upResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       body,
	}
	rec := httptest.NewRecorder()
	toolSchemas := map[string]toolSchemaSummary{
		"read_file":  {},
		"write_file": {},
	}

	srv.handleStreamResponse(rec, upResp, "Qwen-Flash", "test-mixed-malformed-content-tool", toolSchemas)

	text := rec.Body.String()
	if !strings.Contains(text, "event: error") {
		t.Fatalf("stream missing error event: %s", text)
	}
	if strings.Contains(text, `"type":"tool_use"`) || strings.Contains(text, "README.md") {
		t.Fatalf("stream executed or leaked partial valid tool call: %s", text)
	}
}

func TestAnthropicProxyStandardStreamToolCallWinsOverMalformedContentToolCall(t *testing.T) {
	srv := NewServer(":0")
	body := io.NopCloser(strings.NewReader(
		"data: {\"choices\":[{\"delta\":{\"content\":\"<tool_call>{\\\"function\\\":{\\\"name\\\":\\\"read_file\\\",\\\"arguments\\\":{bad-json}}</tool_call>\"}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_read\",\"type\":\"function\",\"function\":{\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\\\"README.md\\\"}\"}}]}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
			"data: [DONE]\n\n",
	))
	upResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       body,
	}
	rec := httptest.NewRecorder()
	toolSchemas := map[string]toolSchemaSummary{
		"read_file": {},
	}

	srv.handleStreamResponse(rec, upResp, "Qwen-Flash", "test-standard-tool-wins", toolSchemas)

	text := rec.Body.String()
	if strings.Contains(text, "event: error") {
		t.Fatalf("stream emitted error despite valid standard tool call: %s", text)
	}
	if !strings.Contains(text, `"type":"tool_use"`) || !strings.Contains(text, `"id":"call_read"`) {
		t.Fatalf("stream missing standard tool_use: %s", text)
	}
	if strings.Contains(text, "bad-json") || strings.Contains(text, "<tool_call>") {
		t.Fatalf("stream leaked malformed content tool call: %s", text)
	}
}

func TestAnthropicCountTokensEndpointEstimatesInputTokens(t *testing.T) {
	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetClientAPIKey("local-key")

	req, _ := http.NewRequest(http.MethodPost,
		"http://"+srv.Addr().String()+"/anthropic/v1/messages/count_tokens",
		strings.NewReader(`{
			"model":"qax-codegen/Auto",
			"system":"You are helpful.",
			"messages":[{"role":"user","content":[{"type":"text","text":"hello world"}]}],
			"tools":[{"name":"search","description":"search docs","input_schema":{"type":"object","properties":{"query":{"type":"string"}}}}]
		}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "local-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var out struct {
		InputTokens int `json:"input_tokens"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode count_tokens response: %v", err)
	}
	if out.InputTokens <= 0 {
		t.Fatalf("input_tokens = %d, want positive estimate", out.InputTokens)
	}
}

func TestAnthropicSDKClientCanCallCountTokens(t *testing.T) {
	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetClientAPIKey("local-key")

	client := anthropic.NewClient(
		anthropicoption.WithBaseURL("http://"+srv.Addr().String()+"/anthropic"),
		anthropicoption.WithAPIKey("local-key"),
	)
	count, err := client.Messages.CountTokens(context.Background(), anthropic.MessageCountTokensParams{
		Model: "qax-codegen/Auto",
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("hello count tokens")),
		},
		System: anthropic.MessageCountTokensParamsSystemUnion{
			OfString: anthropic.String("You are helpful."),
		},
	})
	if err != nil {
		t.Fatalf("anthropic SDK CountTokens failed: %v", err)
	}
	if count.InputTokens <= 0 {
		t.Fatalf("input_tokens = %d, want positive estimate", count.InputTokens)
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

func TestValidateBufferedToolUseNormalizesEmptyArguments(t *testing.T) {
	acc := &streamToolCallAccum{Index: 1, Name: "Noop"}
	if err := validateBufferedToolUse(acc); err != nil {
		t.Fatalf("validateBufferedToolUse: %v", err)
	}
	if acc.Arguments != "{}" {
		t.Fatalf("Arguments = %q, want {}", acc.Arguments)
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

func TestClassifyToolArguments(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: "", want: "empty"},
		{name: "object", raw: `{"file_path":"main.cpp"}`, want: "object"},
		{name: "encoded_object", raw: `"{\"file_path\":\"main.cpp\"}"`, want: "encoded_object"},
		{name: "array", raw: `[]`, want: "array"},
		{name: "string", raw: `"main.cpp"`, want: "string"},
		{name: "null", raw: `null`, want: "null"},
		{name: "invalid", raw: `{`, want: "invalid_json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyToolArguments(tt.raw); got != tt.want {
				t.Fatalf("classifyToolArguments(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestCodeGenOpenAICompatibilitySanitizesToolsForAnyModel(t *testing.T) {
	payload := map[string]interface{}{
		"model":               "qax-codegen/Auto",
		"metadata":            map[string]interface{}{"trace": "x"},
		"parallel_tool_calls": true,
		"tool_choice":         "auto",
		"function_call":       "auto",
		"logprobs":            true,
		"top_logprobs":        2,
		"response_format":     map[string]interface{}{"type": "json_schema"},
		"store":               true,
		"stream_options":      map[string]interface{}{"include_usage": true},
		"tools": []interface{}{
			map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":   "strict_tool",
					"strict": true,
					"parameters": map[string]interface{}{
						"additionalProperties": false,
						"properties": map[string]interface{}{
							"values": map[string]interface{}{
								"type": "array",
								"oneOf": []interface{}{
									map[string]interface{}{"type": "string"},
								},
							},
							"metadata": map[string]interface{}{
								"type": "object",
								"additionalProperties": map[string]interface{}{
									"type": "string",
								},
							},
							"args": map[string]interface{}{
								"type": "object",
							},
						},
					},
				},
			},
		},
		"functions": []interface{}{
			map[string]interface{}{
				"name":   "legacy_tool",
				"strict": true,
				"parameters": map[string]interface{}{
					"properties": map[string]interface{}{
						"ids": map[string]interface{}{"type": "array"},
					},
				},
			},
		},
	}

	notes := applyCodeGenOpenAIMapCompatibility(payload, "qax-codegen/Auto")

	if !containsPrefix(notes, "codegen_sanitize_tools:") {
		t.Fatalf("notes missing tools sanitize entry: %#v", notes)
	}
	if !containsPrefix(notes, "codegen_sanitize_functions:") {
		t.Fatalf("notes missing functions sanitize entry: %#v", notes)
	}
	for _, key := range []string{"parallel_tool_calls", "store", "metadata", "response_format", "tool_choice", "function_call", "logprobs", "top_logprobs"} {
		if !containsPrefix(notes, "codegen_drop_"+key) {
			t.Fatalf("notes missing %s drop entry: %#v", key, notes)
		}
		if _, ok := payload[key]; ok {
			t.Fatalf("%s leaked into CodeGen request: %#v", key, payload)
		}
	}
	// stream_options is preserved (not dropped) — verify it's still present.
	if _, ok := payload["stream_options"]; !ok {
		t.Fatalf("stream_options should be preserved, but was removed")
	}
	tool := payload["tools"].([]interface{})[0].(map[string]interface{})
	fn := tool["function"].(map[string]interface{})
	if _, ok := fn["strict"]; ok {
		t.Fatalf("strict leaked into tool: %#v", fn)
	}
	params := fn["parameters"].(map[string]interface{})
	if _, ok := params["additionalProperties"]; ok {
		t.Fatalf("additionalProperties=false leaked: %#v", params)
	}
	properties := params["properties"].(map[string]interface{})
	for _, bad := range []string{"type", "properties"} {
		if _, ok := properties[bad]; ok {
			t.Fatalf("properties container was treated as schema and leaked %q: %#v", bad, properties)
		}
	}
	values := properties["values"].(map[string]interface{})
	if _, ok := values["oneOf"]; ok {
		t.Fatalf("oneOf leaked: %#v", values)
	}
	if got := values["items"].(map[string]interface{})["type"]; got != "string" {
		t.Fatalf("array items type = %#v, want string", got)
	}
	metadata := properties["metadata"].(map[string]interface{})
	if _, ok := metadata["additionalProperties"]; ok {
		t.Fatalf("additionalProperties schema leaked: %#v", metadata)
	}
	args := properties["args"].(map[string]interface{})
	if got := args["properties"]; got == nil {
		t.Fatalf("object schema without properties should be completed: %#v", args)
	}
	legacy := payload["functions"].([]interface{})[0].(map[string]interface{})
	if _, ok := legacy["strict"]; ok {
		t.Fatalf("legacy strict leaked: %#v", legacy)
	}
	legacyParams := legacy["parameters"].(map[string]interface{})
	legacyProperties := legacyParams["properties"].(map[string]interface{})
	for _, bad := range []string{"type", "properties"} {
		if _, ok := legacyProperties[bad]; ok {
			t.Fatalf("legacy properties container was treated as schema and leaked %q: %#v", bad, legacyProperties)
		}
	}
	ids := legacyProperties["ids"].(map[string]interface{})
	if got := ids["items"].(map[string]interface{})["type"]; got != "string" {
		t.Fatalf("legacy array items type = %#v, want string", got)
	}
}

func TestCodeGenOpenAICompatibilityNormalizesToolCallLinkage(t *testing.T) {
	payload := map[string]interface{}{
		"model": "qax-codegen/Auto",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
			map[string]interface{}{
				"role": "assistant",
				"tool_calls": []interface{}{map[string]interface{}{
					"function": map[string]interface{}{
						"name":      "read_file",
						"arguments": map[string]interface{}{"path": "main.go"},
					},
				}},
			},
			map[string]interface{}{"role": "tool", "tool_call_id": "", "content": map[string]interface{}{"ok": true}},
		},
	}

	notes := applyCodeGenOpenAIMapCompatibility(payload, "qax-codegen/Auto")
	if !containsPrefix(notes, "codegen_sanitize_messages:") {
		t.Fatalf("notes missing message sanitize entry: %#v", notes)
	}
	messages := payload["messages"].([]interface{})
	assistant := messages[1].(map[string]interface{})
	toolCalls := assistant["tool_calls"].([]interface{})
	call := toolCalls[0].(map[string]interface{})
	callID, _ := call["id"].(string)
	if !strings.HasPrefix(callID, "call_") {
		t.Fatalf("generated call id = %#v", call["id"])
	}
	if call["type"] != "function" {
		t.Fatalf("tool call type = %#v", call["type"])
	}
	fn := call["function"].(map[string]interface{})
	if fn["arguments"] != `{"path":"main.go"}` {
		t.Fatalf("arguments = %#v", fn["arguments"])
	}
	tool := messages[2].(map[string]interface{})
	if tool["tool_call_id"] != callID {
		t.Fatalf("tool_call_id = %#v, want %q", tool["tool_call_id"], callID)
	}
	if tool["content"] != `{"ok":true}` {
		t.Fatalf("tool content = %#v", tool["content"])
	}
}

func TestCodeGenOpenAICompatibilityAddsContentToEmptyAssistantMessage(t *testing.T) {
	payload := map[string]interface{}{
		"model": "deepseek-v4-flash",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "continue"},
			map[string]interface{}{"role": "assistant", "tool_calls": []interface{}{}},
		},
	}

	notes := applyCodeGenOpenAIMapCompatibility(payload, "deepseek-v4-flash")
	if !containsPrefix(notes, "codegen_sanitize_messages:") {
		t.Fatalf("notes missing message sanitize entry: %#v", notes)
	}
	messages := payload["messages"].([]interface{})
	assistant := messages[1].(map[string]interface{})
	if assistant["role"] != "assistant" || assistant["content"] != "" {
		t.Fatalf("assistant = %#v, want explicit empty content", assistant)
	}
	if _, hasToolCalls := assistant["tool_calls"]; hasToolCalls {
		t.Fatalf("empty tool_calls leaked: %#v", assistant)
	}
}

func TestCodeGenOpenAIMapCompatibilityHandlesTypedSlices(t *testing.T) {
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type functionDef struct {
		Name       string                 `json:"name"`
		Strict     bool                   `json:"strict"`
		Parameters map[string]interface{} `json:"parameters"`
	}
	payload := map[string]interface{}{
		"messages": []msg{
			{Role: "system", Content: "You are Claude Code using anthropic-version."},
			{Role: "system", Content: "second system"},
			{Role: "user", Content: "older"},
			{Role: "user", Content: "newer"},
		},
		"tools": []map[string]interface{}{{
			"type": "function",
			"function": functionDef{
				Name:   "typed_tool",
				Strict: true,
				Parameters: map[string]interface{}{
					"additionalProperties": false,
					"properties": map[string]interface{}{
						"ids": map[string]interface{}{"type": "array", "nullable": true},
					},
				},
			},
		}},
	}

	notes := applyCodeGenOpenAIMapCompatibility(payload, "qax-codegen/Qwen-Flash")

	if !containsPrefix(notes, "qwen_flash_sanitize_system:") {
		t.Fatalf("notes missing typed system sanitize entry: %#v", notes)
	}
	if !containsPrefix(notes, "qwen_flash_merge_messages:") {
		t.Fatalf("notes missing typed message merge entry: %#v", notes)
	}
	messages := payload["messages"].([]interface{})
	if got := summarizeMessageRoles(messages); got != "system>user" {
		t.Fatalf("message roles = %q, want system>user", got)
	}
	user := messages[1].(map[string]interface{})
	if !strings.Contains(user["content"].(string), "older") || !strings.Contains(user["content"].(string), "newer") {
		t.Fatalf("typed adjacent user messages were not merged: %#v", user)
	}
	tool := payload["tools"].([]map[string]interface{})[0]
	fn := tool["function"].(map[string]interface{})
	params := fn["parameters"].(map[string]interface{})
	if _, ok := fn["strict"]; ok {
		t.Fatalf("typed strict leaked: %#v", fn)
	}
	properties := params["properties"].(map[string]interface{})
	for _, bad := range []string{"type", "properties"} {
		if _, ok := properties[bad]; ok {
			t.Fatalf("typed properties container was treated as schema and leaked %q: %#v", bad, properties)
		}
	}
	ids := properties["ids"].(map[string]interface{})
	if _, ok := ids["nullable"]; ok {
		t.Fatalf("typed nullable leaked: %#v", ids)
	}
	if got := ids["items"].(map[string]interface{})["type"]; got != "string" {
		t.Fatalf("typed array items type = %#v, want string", got)
	}
}

func TestPrepareCodeGenCompactChatRetryBodyHandlesTypedMessages(t *testing.T) {
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	body, err := json.Marshal(map[string]interface{}{
		"messages": []msg{
			{Role: "system", Content: strings.Repeat("runtime context\n", 900)},
			{Role: "user", Content: "latest typed request"},
		},
		"tools": []map[string]string{{"type": "function"}},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	compact, ok := prepareCodeGenCompactChatRetryBody("https://codegen.qianxin-inc.cn/api/v1", "qax-codegen/Auto", body, http.StatusBadRequest)
	if !ok {
		t.Fatal("expected typed compact retry body")
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(compact, &payload); err != nil {
		t.Fatalf("decode compact body: %v", err)
	}
	messages := payload["messages"].([]interface{})
	if len(messages) != 2 {
		t.Fatalf("compact messages len = %d, want 2: %#v", len(messages), messages)
	}
	if got := payload["tools"]; got != nil {
		t.Fatalf("compact retry should omit tools, got %#v", got)
	}
	content := messages[1].(map[string]interface{})["content"].(string)
	if !strings.Contains(content, "latest typed request") || strings.Contains(content, "runtime context\nruntime context") {
		t.Fatalf("compact typed content = %.200q", content)
	}
}

func TestCodeGenOpenAIRequestCompatibilitySanitizesTypedToolSchemasForAnyModel(t *testing.T) {
	req := &openaiChatRequest{
		Model: "qax-codegen/Auto",
		Tools: []openaiTool{{
			Type: "function",
			Function: openaiFunction{
				Name: "strict_tool",
				Parameters: map[string]interface{}{
					"additionalProperties": false,
					"properties": map[string]interface{}{
						"values": map[string]interface{}{
							"type":     "array",
							"nullable": true,
						},
						"metadata": map[string]interface{}{
							"type": "object",
							"additionalProperties": map[string]interface{}{
								"type": "string",
							},
						},
					},
				},
			},
		}},
		Functions: []openaiFunction{{
			Name: "legacy_tool",
			Parameters: map[string]interface{}{
				"properties": map[string]interface{}{
					"ids": map[string]interface{}{"type": "array"},
				},
			},
		}},
	}

	notes := applyCodeGenOpenAIRequestCompatibility(req)

	if !containsPrefix(notes, "codegen_sanitize_tool_schemas:") {
		t.Fatalf("notes missing typed schema sanitize entry: %#v", notes)
	}
	params := req.Tools[0].Function.Parameters.(map[string]interface{})
	if _, ok := params["additionalProperties"]; ok {
		t.Fatalf("additionalProperties=false leaked: %#v", params)
	}
	properties := params["properties"].(map[string]interface{})
	for _, bad := range []string{"type", "properties"} {
		if _, ok := properties[bad]; ok {
			t.Fatalf("typed request properties container was treated as schema and leaked %q: %#v", bad, properties)
		}
	}
	values := properties["values"].(map[string]interface{})
	if _, ok := values["nullable"]; ok {
		t.Fatalf("nullable leaked: %#v", values)
	}
	if got := values["items"].(map[string]interface{})["type"]; got != "string" {
		t.Fatalf("array items type = %#v, want string", got)
	}
	metadata := properties["metadata"].(map[string]interface{})
	if _, ok := metadata["additionalProperties"]; ok {
		t.Fatalf("additionalProperties schema leaked: %#v", metadata)
	}
	legacyParams := req.Functions[0].Parameters.(map[string]interface{})
	legacyProperties := legacyParams["properties"].(map[string]interface{})
	for _, bad := range []string{"type", "properties"} {
		if _, ok := legacyProperties[bad]; ok {
			t.Fatalf("legacy typed request properties container was treated as schema and leaked %q: %#v", bad, legacyProperties)
		}
	}
	ids := legacyProperties["ids"].(map[string]interface{})
	if got := ids["items"].(map[string]interface{})["type"]; got != "string" {
		t.Fatalf("legacy array items type = %#v, want string", got)
	}
}

func containsPrefix(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func TestSummarizeStreamToolIncludesKeysAndSchemaMissing(t *testing.T) {
	schemas := map[string]toolSchemaSummary{
		"Write": {
			Required: map[string]struct{}{
				"file_path": {},
				"content":   {},
			},
			Props: map[string]struct{}{
				"file_path": {},
				"content":   {},
			},
		},
	}
	acc := &streamToolCallAccum{
		Index:     0,
		Name:      "Write",
		Arguments: `{"file_path":"main.cpp"}`,
	}

	got := summarizeStreamTool(acc, schemas)

	if !strings.Contains(got, "args_keys=file_path") {
		t.Fatalf("summary missing args keys: %s", got)
	}
	if !strings.Contains(got, "schema_missing=content") {
		t.Fatalf("summary missing schema gap: %s", got)
	}
}

func TestSummarizeStreamToolListIncludesContentToolCalls(t *testing.T) {
	schemas := map[string]toolSchemaSummary{
		"read_file": {
			Required: map[string]struct{}{"path": {}},
			Props:    map[string]struct{}{"path": {}},
		},
	}
	calls := []*streamToolCallAccum{{
		Index:     0,
		Name:      "read_file",
		Arguments: `{"path":"README.md"}`,
	}}

	got := summarizeStreamToolList(calls, schemas)

	if !strings.Contains(got, "read_file(") || !strings.Contains(got, "args_keys=path") || !strings.Contains(got, "schema=ok") {
		t.Fatalf("summary missing content tool details: %s", got)
	}
}

func TestContentToolCallsToStreamAccumsSkipsPlainText(t *testing.T) {
	calls, malformed := contentToolCallsToStreamAccums("Here is the summary, no tools needed.")

	if malformed {
		t.Fatalf("malformed = true, want false")
	}
	if len(calls) != 0 {
		t.Fatalf("calls = %+v, want none", calls)
	}
}

func TestContentToolCallsToStreamAccumsParsesContentToolCall(t *testing.T) {
	calls, malformed := contentToolCallsToStreamAccums(`<tool_call>{"function":{"name":"read_file","arguments":{"path":"README.md"}}}</tool_call>`)

	if malformed {
		t.Fatalf("malformed = true, want false")
	}
	if len(calls) != 1 {
		t.Fatalf("calls = %+v, want one", calls)
	}
	if calls[0].Name != "read_file" || calls[0].Arguments != `{"path":"README.md"}` {
		t.Fatalf("call = %+v, want read_file README", calls[0])
	}
}

func TestMaybeFlushStreamTextBufferFlushesSafeLongText(t *testing.T) {
	rec := httptest.NewRecorder()
	buf := strings.Builder{}
	buf.WriteString(strings.Repeat("a", codeGenContentToolScanFlushBytes+256))
	textStarted := false

	maybeFlushStreamTextBuffer(rec, rec, 0, &textStarted, &buf)

	body := rec.Body.String()
	if !textStarted {
		t.Fatalf("text block was not started")
	}
	if !strings.Contains(body, "content_block_delta") {
		t.Fatalf("missing text delta: %s", body)
	}
	if buf.Len() != codeGenContentToolScanRetainBytes {
		t.Fatalf("buffer len = %d, want retained %d", buf.Len(), codeGenContentToolScanRetainBytes)
	}
}

func TestMaybeFlushStreamTextBufferKeepsPotentialToolCall(t *testing.T) {
	rec := httptest.NewRecorder()
	buf := strings.Builder{}
	buf.WriteString(strings.Repeat("a", codeGenContentToolScanFlushBytes))
	buf.WriteString(`<tool_call>{"function":{"name":"read_file","arguments":{"path":"README.md"}}}</tool_call>`)
	textStarted := false

	maybeFlushStreamTextBuffer(rec, rec, 0, &textStarted, &buf)

	if textStarted {
		t.Fatalf("text block started despite possible tool call")
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("unexpected flushed body: %s", rec.Body.String())
	}
	if buf.Len() <= codeGenContentToolScanFlushBytes {
		t.Fatalf("buffer unexpectedly truncated")
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

	srv.handleStreamResponse(rec, upResp, "Qwen-Flash", "test-stream-error", nil)

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

	srv.handleStreamResponse(rec, upResp, "Qwen-Flash", "test-partial-tool-error", nil)

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

	srv.handleStreamResponse(rec, upResp, "Qwen-Flash", "test-invalid-tool-args", nil)

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

func TestAnthropicProxyUnwrapsEncodedStreamToolArguments(t *testing.T) {
	srv := NewServer(":0")
	encodedArgs := `"{\"file_path\":\"main.cpp\",\"content\":\"hi\"}"`
	chunk, err := json.Marshal(map[string]interface{}{
		"choices": []interface{}{
			map[string]interface{}{
				"delta": map[string]interface{}{
					"tool_calls": []interface{}{
						map[string]interface{}{
							"index": 0,
							"id":    "call_encoded",
							"function": map[string]interface{}{
								"name":      "Write",
								"arguments": encodedArgs,
							},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := io.NopCloser(strings.NewReader(
		"data: " + string(chunk) + "\n\n" +
			"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
			"data: [DONE]\n\n",
	))
	upResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       body,
	}
	rec := httptest.NewRecorder()

	srv.handleStreamResponse(rec, upResp, "Qwen-Flash", "test-encoded-tool-args", nil)

	text := rec.Body.String()
	if strings.Contains(text, "event: error") {
		t.Fatalf("stream emitted error for encoded args: %s", text)
	}
	if !strings.Contains(text, `"type":"tool_use"`) {
		t.Fatalf("stream missing tool_use: %s", text)
	}
	if !strings.Contains(text, `"partial_json":"{\"file_path\":\"main.cpp\",\"content\":\"hi\"}"`) {
		t.Fatalf("stream did not unwrap encoded args: %s", text)
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

	srv.handleStreamResponse(rec, upResp, "Qwen-Flash", "test-non-object-tool-args", nil)

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

	srv.handleNonStreamResponse(rec, upResp, "Qwen-Flash", "test-nonstream-invalid-tool", nil)

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

	srv.handleStreamResponse(rec, upResp, "Qwen-Flash", "test-invalid-stream-chunk", nil)

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

func TestOpenAIChatCompletionsProxyDefaultSDKNormalizesFullEndpoint(t *testing.T) {
	var upstreamPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPath = r.URL.Path
		if upstreamPath != "/v1/chat/completions" {
			t.Errorf("upstream path = %q, want /v1/chat/completions", upstreamPath)
		}
		if got := r.Header.Get("User-Agent"); got != corelib.CodeGenClientName {
			t.Errorf("User-Agent = %q, want %q", got, corelib.CodeGenClientName)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-test","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetUpstream(upstream.URL+"/v1/chat/completions", "fallback-key")

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
	if upstreamPath == "" {
		t.Fatalf("upstream was not called")
	}
}

func TestOpenAIChatCompletionsProxyStreamingUsesRawPassthrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "text/event-stream" {
			t.Errorf("Accept = %q, want text/event-stream", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetUpstream(upstream.URL, "fallback-key")

	req, _ := http.NewRequest(http.MethodPost,
		"http://"+srv.Addr().String()+"/v1/chat/completions",
		strings.NewReader(`{"model":"qax-codegen/Auto","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "data: [DONE]") {
		t.Fatalf("stream body not passed through: %s", body)
	}
}

func TestCodeGenProxyOpenAIEndpointBuildersNormalizeFullAndGLMURLs(t *testing.T) {
	if got, want := codeGenProxyChatCompletionsEndpoint("https://api.example.com/v1/chat/completions", "openclaw"), "https://api.example.com/v1/chat/completions"; got != want {
		t.Fatalf("chat endpoint = %q, want %q", got, want)
	}
	if got, want := codeGenProxyModelsEndpoint("https://api.example.com/v1/chat/completions", "openclaw", "openai"), "https://api.example.com/v1/models"; got != want {
		t.Fatalf("models endpoint = %q, want %q", got, want)
	}
	if got, want := codeGenProxyChatCompletionsEndpoint("https://open.bigmodel.cn/api/paas/v4", "Kilo Code"), "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions"; got != want {
		t.Fatalf("GLM chat endpoint = %q, want %q", got, want)
	}
	if got, want := codeGenProxyModelsEndpoint("https://open.bigmodel.cn/api/paas/v4", "Kilo Code", "openai"), "https://open.bigmodel.cn/api/coding/paas/v4/models"; got != want {
		t.Fatalf("GLM models endpoint = %q, want %q", got, want)
	}
}

func TestOpenAIChatCompletionsProxyCompactsAfterToollessHTTP400(t *testing.T) {
	var requests []map[string]interface{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]interface{}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("upstream received invalid JSON: %v", err)
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		requests = append(requests, payload)
		if len(requests) < 3 {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if logArrayLen(payload["tools"]) != 0 {
			t.Errorf("compact retry leaked tools: %#v", payload)
		}
		msgs, _ := payload["messages"].([]interface{})
		if len(msgs) != 2 {
			t.Errorf("compact messages = %d, want 2", len(msgs))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-test","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetUpstream(upstream.URL, "fallback-key")

	reqBody := `{
		"model":"qax-codegen/Auto",
		"stream":true,
		"messages":[
			{"role":"system","content":"large system"},
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"large tool result"},
			{"role":"user","content":"please answer latest"}
		],
		"tools":[{"type":"function","function":{"name":"read_file","parameters":{"type":"object"}}}]
	}`
	req, _ := http.NewRequest(http.MethodPost,
		"http://"+srv.Addr().String()+"/v1/chat/completions",
		strings.NewReader(reqBody))
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
	if len(requests) != 3 {
		t.Fatalf("upstream requests = %d, want 3", len(requests))
	}
	if logArrayLen(requests[0]["tools"]) == 0 {
		t.Fatalf("first request should include tools: %#v", requests[0])
	}
	if logArrayLen(requests[1]["tools"]) != 0 {
		t.Fatalf("toolless retry leaked tools: %#v", requests[1])
	}
}

func TestOpenAIChatCompletionsProxyCanUseOpenAISDKUpstreamClient(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("upstream path = %q, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fallback-key" {
			t.Errorf("upstream Authorization = %q, want Bearer fallback-key", got)
		}
		if got := r.Header.Get("User-Agent"); got != "custom-agent" {
			t.Errorf("upstream User-Agent = %q, want custom-agent", got)
		}
		if got := r.Header.Get(corelib.CodeGenClientNameHeader); got != "custom-agent" {
			t.Errorf("upstream %s = %q, want custom-agent", corelib.CodeGenClientNameHeader, got)
		}
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
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-sdk","choices":[{"message":{"role":"assistant","content":"sdk ok"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetOpenAIUpstreamClient(NewOpenAISDKUpstreamClient(nil))
	srv.SetUpstreamWithClientName(upstream.URL, "fallback-key", "custom-agent")

	req, _ := http.NewRequest(http.MethodPost,
		"http://"+srv.Addr().String()+"/v1/chat/completions",
		strings.NewReader(`{"model":"qax-codegen/Auto","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "sdk ok") {
		t.Fatalf("body = %s, want sdk ok", body)
	}
}

func TestOpenAISDKClientCanCallChatCompletionsFromCodeGenProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("upstream path = %q, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fallback-key" {
			t.Errorf("upstream Authorization = %q, want Bearer fallback-key", got)
		}
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
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-openai-sdk-client","choices":[{"message":{"role":"assistant","content":"openai sdk client ok"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetClientAPIKey("local-key")
	srv.SetUpstream(upstream.URL, "fallback-key")

	client := openai.NewClient(
		openaioption.WithBaseURL("http://"+srv.Addr().String()+"/v1"),
		openaioption.WithAPIKey("local-key"),
	)
	completion, err := client.Chat.Completions.New(context.Background(), openai.ChatCompletionNewParams{
		Model: "qax-codegen/Auto",
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage("hi"),
		},
	})
	if err != nil {
		t.Fatalf("openai SDK Chat.Completions.New failed: %v", err)
	}
	if completion.ID != "chatcmpl-openai-sdk-client" {
		t.Fatalf("completion id = %q, want chatcmpl-openai-sdk-client", completion.ID)
	}
	if len(completion.Choices) != 1 || completion.Choices[0].Message.Content != "openai sdk client ok" {
		t.Fatalf("completion choices = %+v, want openai sdk client ok", completion.Choices)
	}
}

func TestOpenAISDKClientCanCallResponsesFromCodeGenProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("upstream path = %q, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fallback-key" {
			t.Errorf("upstream Authorization = %q, want Bearer fallback-key", got)
		}
		body, _ := io.ReadAll(r.Body)
		var req openaiChatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("upstream received invalid JSON: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Model != "qax-codegen/Auto" {
			t.Errorf("upstream model = %q, want qax-codegen/Auto", req.Model)
		}
		if len(req.Messages) != 2 || req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
			t.Errorf("upstream messages = %+v, want system then user", req.Messages)
		}
		if req.MaxTokens != 32 {
			t.Errorf("max_tokens = %d, want 32", req.MaxTokens)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-openai-responses-client","choices":[{"message":{"role":"assistant","content":"responses sdk ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetClientAPIKey("local-key")
	srv.SetUpstream(upstream.URL, "fallback-key")

	client := openai.NewClient(
		openaioption.WithBaseURL("http://"+srv.Addr().String()+"/v1"),
		openaioption.WithAPIKey("local-key"),
	)
	resp, err := client.Responses.New(context.Background(), responses.ResponseNewParams{
		Model: shared.ResponsesModel("qax-codegen/Auto"),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String("hi"),
		},
		Instructions:    openai.String("You are helpful."),
		MaxOutputTokens: openai.Int(32),
	})
	if err != nil {
		t.Fatalf("openai SDK Responses.New failed: %v", err)
	}
	if resp.ID != "chatcmpl-openai-responses-client" {
		t.Fatalf("response id = %q, want chatcmpl-openai-responses-client", resp.ID)
	}
	if got := resp.OutputText(); got != "responses sdk ok" {
		t.Fatalf("OutputText = %q, want responses sdk ok", got)
	}
	if resp.Usage.InputTokens != 2 || resp.Usage.OutputTokens != 3 || resp.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %+v, want 2/3/5", resp.Usage)
	}
}

func TestOpenAIChatResponseToResponsesOmitsEmptyTextForLegacyFunctionCall(t *testing.T) {
	body := []byte(`{
		"id":"chatcmpl-legacy-function",
		"choices":[{
			"message":{"role":"assistant","function_call":{"name":"read_file","arguments":"{\"path\":\"main.go\"}"}},
			"finish_reason":"function_call"
		}]
	}`)
	respBody, err := convertOpenAIChatResponseToResponses(body, "qax-codegen/Auto")
	if err != nil {
		t.Fatalf("convert chat response: %v", err)
	}
	var resp struct {
		Output []struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"output"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		t.Fatalf("decode responses body: %v", err)
	}
	if len(resp.Output) != 1 || resp.Output[0].Type != "function_call" || resp.Output[0].Name != "read_file" {
		t.Fatalf("output = %+v, want one legacy function_call", resp.Output)
	}
}

func TestOpenAISDKClientCanStreamResponsesFromCodeGenProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("upstream path = %q, want /chat/completions", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var req openaiChatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("upstream received invalid JSON: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if !req.Stream {
			t.Errorf("stream = false, want true")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"responses\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\" stream\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetClientAPIKey("local-key")
	srv.SetUpstream(upstream.URL, "fallback-key")

	client := openai.NewClient(
		openaioption.WithBaseURL("http://"+srv.Addr().String()+"/v1"),
		openaioption.WithAPIKey("local-key"),
	)
	stream := client.Responses.NewStreaming(context.Background(), responses.ResponseNewParams{
		Model: shared.ResponsesModel("qax-codegen/Auto"),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String("hi"),
		},
	})
	var got strings.Builder
	var completed bool
	for stream.Next() {
		switch event := stream.Current().AsAny().(type) {
		case responses.ResponseTextDeltaEvent:
			got.WriteString(event.Delta)
		case responses.ResponseCompletedEvent:
			completed = true
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("openai SDK Responses.NewStreaming failed: %v", err)
	}
	if got.String() != "responses stream" {
		t.Fatalf("stream content = %q, want responses stream", got.String())
	}
	if !completed {
		t.Fatal("stream did not emit response.completed")
	}
}

func TestResponsesRequestConvertsFunctionCallOutputToChatToolMessage(t *testing.T) {
	body := []byte(`{
		"model":"qax-codegen/Auto",
		"input":[
			{"type":"function_call","call_id":"call_read","name":"read_file","arguments":"{\"path\":\"main.go\"}"},
			{"type":"function_call_output","call_id":"call_read","output":"package main"}
		]
	}`)
	chatBody, model, err := convertOpenAIResponsesRequestToChat(body)
	if err != nil {
		t.Fatalf("convert responses request: %v", err)
	}
	if model != "qax-codegen/Auto" {
		t.Fatalf("model = %q, want qax-codegen/Auto", model)
	}
	var req struct {
		Messages []openaiMessage `json:"messages"`
	}
	if err := json.Unmarshal(chatBody, &req); err != nil {
		t.Fatalf("decode chat body: %v", err)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("messages = %+v, want 2", req.Messages)
	}
	if req.Messages[0].Role != "assistant" || len(req.Messages[0].ToolCalls) != 1 || req.Messages[0].ToolCalls[0].ID != "call_read" {
		t.Fatalf("assistant tool call message = %+v", req.Messages[0])
	}
	if req.Messages[1].Role != "tool" || req.Messages[1].ToolCallID != "call_read" || req.Messages[1].Content != "package main" {
		t.Fatalf("tool result message = %+v", req.Messages[1])
	}
}

func TestResponsesRequestConvertsObjectFunctionCallData(t *testing.T) {
	body := []byte(`{
		"model":"qax-codegen/Auto",
		"input":[
			{"type":"function_call","call_id":"call_read","name":"read_file","arguments":{"path":"main.go"}},
			{"type":"function_call_output","call_id":"call_read","output":{"ok":true}}
		]
	}`)
	chatBody, _, err := convertOpenAIResponsesRequestToChat(body)
	if err != nil {
		t.Fatalf("convert responses request: %v", err)
	}
	var req struct {
		Messages []openaiMessage `json:"messages"`
	}
	if err := json.Unmarshal(chatBody, &req); err != nil {
		t.Fatalf("decode chat body: %v", err)
	}
	if got := req.Messages[0].ToolCalls[0].Function.Arguments; got != `{"path":"main.go"}` {
		t.Fatalf("tool call arguments = %q, want JSON object", got)
	}
	if got := req.Messages[1].Content; got != `{"ok":true}` {
		t.Fatalf("tool output = %q, want JSON object", got)
	}
}

func TestResponsesRequestConvertsTextFormatToChatResponseFormat(t *testing.T) {
	body := []byte(`{
		"model":"qax-codegen/Auto",
		"input":"answer with json",
		"text":{"format":{"type":"json_schema","name":"answer","schema":{"type":"object","properties":{"ok":{"type":"boolean"}}},"strict":true}}
	}`)
	chatBody, _, err := convertOpenAIResponsesRequestToChat(body)
	if err != nil {
		t.Fatalf("convert responses request: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(chatBody, &req); err != nil {
		t.Fatalf("decode chat body: %v", err)
	}
	responseFormat := req["response_format"].(map[string]interface{})
	if responseFormat["type"] != "json_schema" {
		t.Fatalf("response_format.type = %#v, want json_schema", responseFormat["type"])
	}
	jsonSchema := responseFormat["json_schema"].(map[string]interface{})
	if jsonSchema["name"] != "answer" || jsonSchema["strict"] != true {
		t.Fatalf("json_schema = %#v, want name answer strict true", jsonSchema)
	}
	if _, ok := jsonSchema["schema"].(map[string]interface{}); !ok {
		t.Fatalf("json_schema.schema missing: %#v", jsonSchema)
	}
}

func TestResponsesRequestConvertsJSONObjectTextFormat(t *testing.T) {
	body := []byte(`{
		"model":"qax-codegen/Auto",
		"input":"answer with json",
		"text":{"format":{"type":"json_object"}}
	}`)
	chatBody, _, err := convertOpenAIResponsesRequestToChat(body)
	if err != nil {
		t.Fatalf("convert responses request: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(chatBody, &req); err != nil {
		t.Fatalf("decode chat body: %v", err)
	}
	responseFormat := req["response_format"].(map[string]interface{})
	if responseFormat["type"] != "json_object" {
		t.Fatalf("response_format.type = %#v, want json_object", responseFormat["type"])
	}
}

func TestResponsesRequestConvertsToolChoiceToChatToolChoice(t *testing.T) {
	body := []byte(`{
		"model":"qax-codegen/Auto",
		"input":"use the selected tool",
		"tools":[{"type":"function","name":"answer_tool","parameters":{"type":"object","properties":{}}}],
		"tool_choice":{"type":"function","name":"answer_tool"}
	}`)
	chatBody, _, err := convertOpenAIResponsesRequestToChat(body)
	if err != nil {
		t.Fatalf("convert responses request: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(chatBody, &req); err != nil {
		t.Fatalf("decode chat body: %v", err)
	}
	toolChoice := req["tool_choice"].(map[string]interface{})
	fn := toolChoice["function"].(map[string]interface{})
	if toolChoice["type"] != "function" || fn["name"] != "answer_tool" {
		t.Fatalf("tool_choice = %#v, want function answer_tool", toolChoice)
	}
}

func TestResponsesRequestDefaultsMissingFunctionCallArguments(t *testing.T) {
	body := []byte(`{
		"model":"qax-codegen/Auto",
		"input":[{"type":"function_call","call_id":"call_ping","name":"ping"}]
	}`)
	chatBody, _, err := convertOpenAIResponsesRequestToChat(body)
	if err != nil {
		t.Fatalf("convert responses request: %v", err)
	}
	var req struct {
		Messages []openaiMessage `json:"messages"`
	}
	if err := json.Unmarshal(chatBody, &req); err != nil {
		t.Fatalf("decode chat body: %v", err)
	}
	if got := req.Messages[0].ToolCalls[0].Function.Arguments; got != "{}" {
		t.Fatalf("tool call arguments = %q, want {}", got)
	}
}

func TestResponsesRequestDropsEmptyFunctionCallOutput(t *testing.T) {
	body := []byte(`{
		"model":"qax-codegen/Auto",
		"input":[
			{"role":"user","content":"hi"},
			{"type":"function_call_output","call_id":"call_empty","output":null}
		]
	}`)
	chatBody, _, err := convertOpenAIResponsesRequestToChat(body)
	if err != nil {
		t.Fatalf("convert responses request: %v", err)
	}
	var req struct {
		Messages []openaiMessage `json:"messages"`
	}
	if err := json.Unmarshal(chatBody, &req); err != nil {
		t.Fatalf("decode chat body: %v", err)
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != "user" || req.Messages[0].Content != "hi" {
		t.Fatalf("messages = %+v, want only original user message", req.Messages)
	}
}

func TestResponsesRequestPreservesToolRoleCallID(t *testing.T) {
	body := []byte(`{
		"model":"qax-codegen/Auto",
		"input":[{"role":"tool","tool_call_id":"call_read","content":"package main"}]
	}`)
	chatBody, _, err := convertOpenAIResponsesRequestToChat(body)
	if err != nil {
		t.Fatalf("convert responses request: %v", err)
	}
	var req struct {
		Messages []openaiMessage `json:"messages"`
	}
	if err := json.Unmarshal(chatBody, &req); err != nil {
		t.Fatalf("decode chat body: %v", err)
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != "tool" || req.Messages[0].ToolCallID != "call_read" {
		t.Fatalf("messages = %+v, want tool message with call id", req.Messages)
	}
}

func TestResponsesRequestIgnoresImageOnlyContent(t *testing.T) {
	body := []byte(`{
		"model":"qax-codegen/Auto",
		"input":[{"role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,xx"}]}]
	}`)
	if _, _, err := convertOpenAIResponsesRequestToChat(body); err == nil || !strings.Contains(err.Error(), "input is required") {
		t.Fatalf("convert responses request error = %v, want input required", err)
	}
}

func TestResponsesRequestPreservesFunctionToolStrict(t *testing.T) {
	body := []byte(`{
		"model":"qax-codegen/Auto",
		"input":"hi",
		"tools":[{"type":"function","name":"read_file","description":"Read file","parameters":{"type":"object"},"strict":true}]
	}`)
	chatBody, _, err := convertOpenAIResponsesRequestToChat(body)
	if err != nil {
		t.Fatalf("convert responses request: %v", err)
	}
	var req struct {
		Tools []struct {
			Function struct {
				Name   string      `json:"name"`
				Strict interface{} `json:"strict"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(chatBody, &req); err != nil {
		t.Fatalf("decode chat body: %v", err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Function.Name != "read_file" || req.Tools[0].Function.Strict != true {
		t.Fatalf("tools = %+v, want strict read_file", req.Tools)
	}
}

func TestResponsesRequestDefaultsFunctionToolParameters(t *testing.T) {
	body := []byte(`{
		"model":"qax-codegen/Auto",
		"input":"hi",
		"tools":[{"type":"function","name":"ping"}]
	}`)
	chatBody, _, err := convertOpenAIResponsesRequestToChat(body)
	if err != nil {
		t.Fatalf("convert responses request: %v", err)
	}
	var req struct {
		Tools []struct {
			Function struct {
				Parameters map[string]interface{} `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(chatBody, &req); err != nil {
		t.Fatalf("decode chat body: %v", err)
	}
	params := req.Tools[0].Function.Parameters
	if params["type"] != "object" {
		t.Fatalf("parameters = %+v, want object schema", params)
	}
}

func TestOpenAIResponsesStreamConvertsToolCalls(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_read\",\"function\":{\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\"}}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"main.go\\\"}\"}}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetClientAPIKey("local-key")
	srv.SetUpstream(upstream.URL, "fallback-key")

	client := openai.NewClient(
		openaioption.WithBaseURL("http://"+srv.Addr().String()+"/v1"),
		openaioption.WithAPIKey("local-key"),
	)
	stream := client.Responses.NewStreaming(context.Background(), responses.ResponseNewParams{
		Model: shared.ResponsesModel("qax-codegen/Auto"),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String("hi"),
		},
	})
	var delta, doneArgs string
	var completed bool
	for stream.Next() {
		switch event := stream.Current().AsAny().(type) {
		case responses.ResponseFunctionCallArgumentsDeltaEvent:
			delta += event.Delta
		case responses.ResponseFunctionCallArgumentsDoneEvent:
			doneArgs = event.Arguments
		case responses.ResponseCompletedEvent:
			completed = true
			if len(event.Response.Output) != 1 || event.Response.Output[0].Type != "function_call" {
				t.Fatalf("completed output = %+v, want one function_call", event.Response.Output)
			}
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("openai SDK Responses.NewStreaming failed: %v", err)
	}
	if delta != `{"path":"main.go"}` || doneArgs != `{"path":"main.go"}` {
		t.Fatalf("tool args delta=%q done=%q, want JSON args", delta, doneArgs)
	}
	if !completed {
		t.Fatal("stream did not emit response.completed")
	}
}

func TestOpenAIResponsesStreamIndexesToolThenTextOutputs(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_read\",\"function\":{\"name\":\"read_file\",\"arguments\":\"{}\"}}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"after tool\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetClientAPIKey("local-key")
	srv.SetUpstream(upstream.URL, "fallback-key")

	req, _ := http.NewRequest(http.MethodPost,
		"http://"+srv.Addr().String()+"/v1/responses",
		strings.NewReader(`{"model":"qax-codegen/Auto","input":"hi","stream":true}`))
	req.Header.Set("Authorization", "Bearer local-key")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"sequence_number":2,"type":"response.output_item.added"`) ||
		!strings.Contains(string(body), `"output_index":0`) ||
		!strings.Contains(string(body), `"type":"response.output_text.delta"`) ||
		!strings.Contains(string(body), `"output_index":1`) {
		t.Fatalf("stream body missing expected output indexes: %s", body)
	}
}

func TestOpenAIResponsesStreamInvalidChunkEmitsError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {not-json}\n\n"))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetClientAPIKey("local-key")
	srv.SetUpstream(upstream.URL, "fallback-key")

	req, _ := http.NewRequest(http.MethodPost,
		"http://"+srv.Addr().String()+"/v1/responses",
		strings.NewReader(`{"model":"qax-codegen/Auto","input":"hi","stream":true}`))
	req.Header.Set("Authorization", "Bearer local-key")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "event: error") {
		t.Fatalf("stream body = %s, want error event", body)
	}
	if strings.Contains(string(body), "response.completed") {
		t.Fatalf("stream body = %s, should not complete after invalid chunk", body)
	}
}

func TestOpenAIResponsesStreamRetriesBadGatewayWithCompactHistory(t *testing.T) {
	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit := upstreamHits.Add(1)
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		if hit == 1 {
			if logArrayLen(payload["messages"]) < 3 || logArrayLen(payload["tools"]) == 0 {
				t.Fatalf("initial request was not full tool history: %#v", payload)
			}
			http.Error(w, `{"error":{"message":"Invalid assistant message: content or tool_calls must be set"}}`, http.StatusBadGateway)
			return
		}
		if logArrayLen(payload["tools"]) != 0 || logArrayLen(payload["functions"]) != 0 {
			t.Fatalf("compact retry leaked tools: %#v", payload)
		}
		if logArrayLen(payload["messages"]) >= 3 {
			t.Fatalf("compact retry did not reduce history: %#v", payload["messages"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"recovered\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetClientAPIKey("local-key")
	srv.SetUpstream(upstream.URL, "fallback-key")

	requestBody := `{"model":"vendor/deepseek-v4-flash","stream":true,"input":[
		{"role":"system","content":"runtime context"},
		{"role":"user","content":"first task"},
		{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"call_1","content":"result"},
		{"role":"user","content":"latest task"}
	],"tools":[{"type":"function","name":"read_file","parameters":{"type":"object"}}]}`
	req, _ := http.NewRequest(http.MethodPost, "http://"+srv.Addr().String()+"/v1/responses", strings.NewReader(requestBody))
	req.Header.Set("Authorization", "Bearer local-key")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "recovered") {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if upstreamHits.Load() != 2 {
		t.Fatalf("upstream hits=%d, want 2", upstreamHits.Load())
	}
}

func TestOpenAIResponsesToolCallPromptCacheMissThenHit(t *testing.T) {
	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-tool-cache","choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"README.md\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetClientAPIKey("local-key")
	srv.SetUpstream(upstream.URL, "fallback-key")
	srv.SetPromptCache(llmpool.NewCache(nil, llmpool.CacheConfig{MemoryMaxEntries: 32, MemoryMaxBytes: 1 << 20}))

	doReq := func(stream bool) (*http.Response, []byte) {
		t.Helper()
		body := `{"model":"qax-codegen/Auto","input":"ping tool cache","temperature":0,"tools":[{"type":"function","name":"read_file","parameters":{"type":"object"}}]}`
		if stream {
			body = `{"model":"qax-codegen/Auto","input":"ping tool cache stream","temperature":0,"stream":true,"tools":[{"type":"function","name":"read_file","parameters":{"type":"object"}}]}`
		}
		req, _ := http.NewRequest(http.MethodPost,
			"http://"+srv.Addr().String()+"/v1/responses",
			strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer local-key")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		payload, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp, payload
	}

	// Non-stream tool_call: miss then hit.
	resp1, body1 := doReq(false)
	if resp1.StatusCode != 200 {
		t.Fatalf("first status=%d body=%s", resp1.StatusCode, body1)
	}
	if resp1.Header.Get("X-Cache") != "MISS" {
		t.Fatalf("first X-Cache=%q, want MISS", resp1.Header.Get("X-Cache"))
	}
	resp2, body2 := doReq(false)
	if resp2.Header.Get("X-Cache") != "HIT" {
		t.Fatalf("second X-Cache=%q, want HIT body=%s", resp2.Header.Get("X-Cache"), body2)
	}
	if !bytes.Contains(body2, []byte("read_file")) {
		t.Fatalf("hit body missing tool: %s", body2)
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("upstream hits=%d, want 1", upstreamHits.Load())
	}
}

func TestOpenAIResponsesPromptCacheMissThenHit(t *testing.T) {
	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-resp-cache","choices":[{"message":{"role":"assistant","content":"cached via responses"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetClientAPIKey("local-key")
	srv.SetUpstream(upstream.URL, "fallback-key")
	srv.SetPromptCache(llmpool.NewCache(nil, llmpool.CacheConfig{MemoryMaxEntries: 32, MemoryMaxBytes: 1 << 20}))

	doReq := func() (*http.Response, []byte) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost,
			"http://"+srv.Addr().String()+"/v1/responses",
			strings.NewReader(`{"model":"qax-codegen/Auto","input":"ping cache","temperature":0}`))
		req.Header.Set("Authorization", "Bearer local-key")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp, body
	}

	resp1, body1 := doReq()
	if resp1.StatusCode != 200 {
		t.Fatalf("first status=%d body=%s", resp1.StatusCode, body1)
	}
	if resp1.Header.Get("X-Cache") != "MISS" {
		t.Fatalf("first X-Cache=%q, want MISS", resp1.Header.Get("X-Cache"))
	}
	if !bytes.Contains(body1, []byte("cached via responses")) {
		t.Fatalf("first body missing text: %s", body1)
	}

	resp2, body2 := doReq()
	if resp2.StatusCode != 200 {
		t.Fatalf("second status=%d body=%s", resp2.StatusCode, body2)
	}
	if resp2.Header.Get("X-Cache") != "HIT" {
		t.Fatalf("second X-Cache=%q, want HIT", resp2.Header.Get("X-Cache"))
	}
	if !bytes.Contains(body2, []byte("cached via responses")) {
		t.Fatalf("second body missing text: %s", body2)
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("upstream hits=%d, want 1 (second served from cache)", upstreamHits.Load())
	}
	hits, misses := srv.CacheHitMiss()
	if hits != 1 || misses != 1 {
		t.Fatalf("cache hits/misses=%d/%d, want 1/1", hits, misses)
	}
}

func TestOpenAIResponsesStreamPromptCacheStoreAndHit(t *testing.T) {
	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"stream \"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"cached\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2,\"total_tokens\":3}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetClientAPIKey("local-key")
	srv.SetUpstream(upstream.URL, "fallback-key")
	srv.SetPromptCache(llmpool.NewCache(nil, llmpool.CacheConfig{MemoryMaxEntries: 32, MemoryMaxBytes: 1 << 20}))

	doStream := func() (string, string) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost,
			"http://"+srv.Addr().String()+"/v1/responses",
			strings.NewReader(`{"model":"qax-codegen/Auto","input":"stream cache","temperature":0,"stream":true}`))
		req.Header.Set("Authorization", "Bearer local-key")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.Header.Get("X-Cache"), string(body)
	}

	x1, body1 := doStream()
	if x1 != "MISS" {
		t.Fatalf("first stream X-Cache=%q, want MISS", x1)
	}
	if !strings.Contains(body1, "response.completed") || !strings.Contains(body1, "stream cached") {
		t.Fatalf("first stream body unexpected: %s", body1)
	}

	// Allow stream store to finish (synchronous in handler, but be safe).
	x2, body2 := doStream()
	if x2 != "HIT" {
		t.Fatalf("second stream X-Cache=%q, want HIT (upstreamHits=%d body=%s)", x2, upstreamHits.Load(), body2)
	}
	if !strings.Contains(body2, "stream cached") {
		t.Fatalf("second stream missing text: %s", body2)
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("upstream hits=%d, want 1", upstreamHits.Load())
	}
}

func TestOpenAIResponsesAndChatCompletionsSharePromptCache(t *testing.T) {
	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-shared","choices":[{"message":{"role":"assistant","content":"shared cache body"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetClientAPIKey("local-key")
	srv.SetUpstream(upstream.URL, "fallback-key")
	srv.SetPromptCache(llmpool.NewCache(nil, llmpool.CacheConfig{MemoryMaxEntries: 32, MemoryMaxBytes: 1 << 20}))

	// Populate via Responses API.
	req1, _ := http.NewRequest(http.MethodPost,
		"http://"+srv.Addr().String()+"/v1/responses",
		strings.NewReader(`{"model":"qax-codegen/Auto","input":"share me","temperature":0}`))
	req1.Header.Set("Authorization", "Bearer local-key")
	req1.Header.Set("Content-Type", "application/json")
	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatalf("responses request: %v", err)
	}
	io.Copy(io.Discard, resp1.Body)
	resp1.Body.Close()
	if resp1.Header.Get("X-Cache") != "MISS" {
		t.Fatalf("responses X-Cache=%q, want MISS", resp1.Header.Get("X-Cache"))
	}

	// Hit via chat completions with equivalent deterministic body.
	// convertOpenAIResponsesRequestToChat turns string input into a user message.
	req2, _ := http.NewRequest(http.MethodPost,
		"http://"+srv.Addr().String()+"/v1/chat/completions",
		strings.NewReader(`{"model":"qax-codegen/Auto","temperature":0,"messages":[{"role":"user","content":"share me"}]}`))
	req2.Header.Set("Authorization", "Bearer local-key")
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("chat request: %v", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if resp2.Header.Get("X-Cache") != "HIT" {
		t.Fatalf("chat X-Cache=%q, want HIT (upstreamHits=%d body=%s)", resp2.Header.Get("X-Cache"), upstreamHits.Load(), body2)
	}
	if !bytes.Contains(body2, []byte("shared cache body")) {
		t.Fatalf("chat body = %s", body2)
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("upstream hits=%d, want 1", upstreamHits.Load())
	}
}

func TestOpenAISDKClientCanStreamChatCompletionsFromCodeGenProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req openaiChatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("upstream received invalid JSON: %v", err)
			http.Error(w, "bad request", 400)
			return
		}
		if !req.Stream {
			t.Errorf("stream = false, want true")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"openai\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\" stream\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetClientAPIKey("local-key")
	srv.SetUpstream(upstream.URL, "fallback-key")

	client := openai.NewClient(
		openaioption.WithBaseURL("http://"+srv.Addr().String()+"/v1"),
		openaioption.WithAPIKey("local-key"),
	)
	stream := client.Chat.Completions.NewStreaming(context.Background(), openai.ChatCompletionNewParams{
		Model: "qax-codegen/Auto",
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage("hi"),
		},
	})
	var got strings.Builder
	for stream.Next() {
		chunk := stream.Current()
		if len(chunk.Choices) > 0 {
			got.WriteString(chunk.Choices[0].Delta.Content)
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("openai SDK streaming failed: %v", err)
	}
	if got.String() != "openai stream" {
		t.Fatalf("stream content = %q, want openai stream", got.String())
	}
}

func TestOpenAISDKUpstreamClientPreservesAPIErrorStatusAndBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request from upstream","type":"invalid_request_error"}}`))
	}))
	defer upstream.Close()

	client := NewOpenAISDKUpstreamClient(nil)
	resp, err := client.DoChatCompletions(
		context.Background(),
		upstream.URL+"/chat/completions",
		"fallback-key",
		"custom-agent",
		[]byte(`{"model":"qax-codegen/Auto","messages":[{"role":"user","content":"hi"}]}`),
		"application/json",
		false,
	)
	if err != nil {
		t.Fatalf("DoChatCompletions returned transport error: %v", err)
	}
	if resp == nil {
		t.Fatal("response is nil")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "bad request from upstream") {
		t.Fatalf("body = %s, want upstream error body", body)
	}
}

func TestOpenAIUpstreamClientsRepairInvalidAssistantHistory(t *testing.T) {
	for _, tc := range []struct {
		name   string
		client OpenAIUpstreamClient
		stream bool
	}{
		{name: "raw", client: NewHTTPOpenAIUpstreamClient(nil)},
		{name: "sdk", client: NewOpenAISDKUpstreamClient(nil)},
		{name: "sdk stream fallback", client: NewOpenAISDKUpstreamClient(nil), stream: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("decode upstream request: %v", err)
				}
				messages := body["messages"].([]any)
				assistant := messages[1].(map[string]any)
				if got, ok := assistant["content"]; !ok || got != "" {
					t.Fatalf("assistant content = %#v, want explicit empty string: %#v", got, assistant)
				}
				if _, ok := assistant["tool_calls"]; ok {
					t.Fatalf("invalid tool_calls leaked upstream: %#v", assistant)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"chatcmpl-test","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
			}))
			defer upstream.Close()

			_, err := tc.client.DoChatCompletions(
				context.Background(), upstream.URL+"/chat/completions", "fallback-key", "test-client",
				[]byte(`{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"continue"},{"role":"assistant","content":null,"tool_calls":[{}]}]}`),
				"application/json", tc.stream,
			)
			if err != nil {
				t.Fatalf("DoChatCompletions() error = %v", err)
			}
		})
	}
}

func TestSummarizeAssistantMessageDiagnosticsDoesNotExposeContent(t *testing.T) {
	summary := summarizeAssistantMessageDiagnostics([]any{
		map[string]any{"role": "user", "content": "not counted"},
		map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{}}},
		map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"function": map[string]any{"name": "safe"}}}},
	})
	for _, want := range []string{
		"total=2",
		"missing_content=1",
		"null_content=1",
		"invalid_tool_calls=1",
		"repair_needed=1",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary = %q, want %q", summary, want)
		}
	}
	if strings.Contains(summary, "not counted") || strings.Contains(summary, "safe") {
		t.Fatalf("summary leaks input data: %q", summary)
	}
}

func TestAnthropicStreamProxyWithOpenAISDKClientFallsBackToRawHTTP(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req openaiChatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("upstream received invalid JSON: %v", err)
			http.Error(w, "bad request", 400)
			return
		}
		if !req.Stream {
			t.Errorf("stream = false, want true")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetOpenAIUpstreamClient(NewOpenAISDKUpstreamClient(nil))
	srv.SetUpstream(upstream.URL, "fallback-key")

	anthReq := `{
		"model": "qax-codegen/Auto",
		"messages": [{"role": "user", "content": "Hello"}],
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

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "content_block_delta") || !strings.Contains(string(body), "hello") {
		t.Fatalf("stream body = %s, want anthropic SSE hello", body)
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

func TestOpenAISDKClientCanListModelsFromCodeGenProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("upstream path = %q, want /models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fallback-key" {
			t.Errorf("upstream Authorization = %q, want Bearer fallback-key", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"qax-codegen/Auto","object":"model","provider":"qax-codegen"}]}`))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetClientAPIKey("local-key")
	srv.SetUpstream(upstream.URL, "fallback-key")

	client := openai.NewClient(
		openaioption.WithBaseURL("http://"+srv.Addr().String()+"/v1"),
		openaioption.WithAPIKey("local-key"),
	)
	page, err := client.Models.List(context.Background())
	if err != nil {
		t.Fatalf("openai SDK Models.List failed: %v", err)
	}
	if page == nil || len(page.Data) != 1 {
		t.Fatalf("models page = %+v, want one model", page)
	}
	if page.Data[0].ID != "qax-codegen/Auto" {
		t.Fatalf("model id = %q, want qax-codegen/Auto", page.Data[0].ID)
	}
}

func TestAnthropicSDKClientCanListModelsFromCodeGenProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("upstream path = %q, want /models", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"qax-codegen/Auto","name":"Auto","provider":"qax-codegen"}]}`))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetClientAPIKey("local-key")
	srv.SetUpstream(upstream.URL, "fallback-key")

	client := anthropic.NewClient(
		anthropicoption.WithBaseURL("http://"+srv.Addr().String()+"/anthropic"),
		anthropicoption.WithAPIKey("local-key"),
	)
	page, err := client.Models.List(context.Background(), anthropic.ModelListParams{})
	if err != nil {
		t.Fatalf("anthropic SDK Models.List failed: %v", err)
	}
	if page == nil || len(page.Data) != 1 {
		t.Fatalf("models page = %+v, want one model", page)
	}
	if page.Data[0].ID != "qax-codegen/Auto" || page.Data[0].DisplayName != "Auto" {
		t.Fatalf("model = %+v, want qax-codegen/Auto Auto", page.Data[0])
	}
}

func TestOpenAISDKClientCanGetModelFromCodeGenProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("upstream path = %q, want /models", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"qax-codegen/Auto","name":"Auto","provider":"qax-codegen"}]}`))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetClientAPIKey("local-key")
	srv.SetUpstream(upstream.URL, "fallback-key")

	client := openai.NewClient(
		openaioption.WithBaseURL("http://"+srv.Addr().String()+"/v1"),
		openaioption.WithAPIKey("local-key"),
	)
	model, err := client.Models.Get(context.Background(), "qax-codegen/Auto")
	if err != nil {
		t.Fatalf("openai SDK Models.Get failed: %v", err)
	}
	if model.ID != "qax-codegen/Auto" || model.Object != "model" {
		t.Fatalf("model = %+v, want qax-codegen/Auto model", model)
	}
}

func TestModelGetHandlesEscapedProviderPrefixedID(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"qax-codegen/Qwen-Flash","name":"Qwen-Flash","provider":"qax-codegen"}]}`))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetUpstream(upstream.URL, "fallback-key")

	resp, err := http.Get("http://" + srv.Addr().String() + "/v1/models/qax-codegen%2FQwen-Flash")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var model struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &model); err != nil {
		t.Fatalf("decode model: %v", err)
	}
	if model.ID != "qax-codegen/Qwen-Flash" {
		t.Fatalf("model id = %q, want qax-codegen/Qwen-Flash", model.ID)
	}
}

func TestModelGetReturnsNotFoundForUnknownModel(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"qax-codegen/Auto","name":"Auto","provider":"qax-codegen"}]}`))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetUpstream(upstream.URL, "fallback-key")

	resp, err := http.Get("http://" + srv.Addr().String() + "/v1/models/missing-model")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 404; body=%s", resp.StatusCode, body)
	}
}

func TestAnthropicSDKClientCanGetModelFromCodeGenProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("upstream path = %q, want /models", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"qax-codegen/Auto","name":"Auto","provider":"qax-codegen"}]}`))
	}))
	defer upstream.Close()

	srv, cancel := startTestServer(t)
	defer cancel()
	srv.SetClientAPIKey("local-key")
	srv.SetUpstream(upstream.URL, "fallback-key")

	client := anthropic.NewClient(
		anthropicoption.WithBaseURL("http://"+srv.Addr().String()+"/anthropic"),
		anthropicoption.WithAPIKey("local-key"),
	)
	model, err := client.Models.Get(context.Background(), "qax-codegen/Auto", anthropic.ModelGetParams{})
	if err != nil {
		t.Fatalf("anthropic SDK Models.Get failed: %v", err)
	}
	if model.ID != "qax-codegen/Auto" || model.DisplayName != "Auto" {
		t.Fatalf("model = %+v, want qax-codegen/Auto Auto", model)
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
