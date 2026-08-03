package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

// TestBugCondition_SSE_SingleChunk verifies that DoOpenAIRequest can handle
// a single-chunk SSE response. On UNFIXED code this test FAILS because
// json.Unmarshal chokes on the "data: " prefix — confirming the bug exists.
//
// **Validates: Requirements 1.1, 1.3**
func TestBugCondition_SSE_SingleChunk(t *testing.T) {
	sseBody := "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\ndata: [DONE]\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseBody))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{
		URL:   srv.URL,
		Model: "test-model",
	}
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}

	resp, err := DoOpenAIRequest(context.Background(), cfg, messages, nil, srv.Client())
	if err != nil {
		t.Fatalf("DoOpenAIRequest returned error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice in response")
	}
	if got := resp.Choices[0].Message.Content; got != "Hello" {
		t.Errorf("content = %q, want %q", got, "Hello")
	}
}

func TestBuildAnthropicMessagesRequestDataUsesSharedEndpointAndOptions(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{URL: "https://anthropic.test/api/v1", Model: "claude-test"}
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": "be brief"},
		map[string]interface{}{"role": "user", "content": "hello"},
	}
	tools := []map[string]interface{}{{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "lookup",
			"description": "look up value",
			"parameters": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"q": map[string]interface{}{"type": "string"}},
			},
		},
	}}

	endpoint, body, err := BuildAnthropicMessagesRequestData(cfg, messages, AnthropicMessagesRequestOptions{Stream: true, Tools: tools})
	if err != nil {
		t.Fatalf("BuildAnthropicMessagesRequestData: %v", err)
	}
	if endpoint != "https://anthropic.test/api/v1/messages" {
		t.Fatalf("endpoint = %q", endpoint)
	}
	var req struct {
		Model     string                   `json:"model"`
		System    string                   `json:"system"`
		Stream    bool                     `json:"stream"`
		MaxTokens int                      `json:"max_tokens"`
		Messages  []map[string]interface{} `json:"messages"`
		Tools     []map[string]interface{} `json:"tools"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if req.Model != "claude-test" || req.System != "be brief" || !req.Stream || req.MaxTokens != cfg.EffectiveMaxOutputTokens() {
		t.Fatalf("request body scalar fields = %+v", req)
	}
	if len(req.Messages) != 1 || req.Messages[0]["role"] != "user" {
		t.Fatalf("messages = %#v", req.Messages)
	}
	if len(req.Tools) != 1 || req.Tools[0]["name"] != "lookup" {
		t.Fatalf("tools = %#v", req.Tools)
	}
}

// TestBugCondition_SSE_MultiChunk verifies that DoOpenAIRequest can handle
// a multi-chunk SSE response with incremental content deltas. On UNFIXED code
// this test FAILS — confirming the bug exists.
//
// **Validates: Requirements 1.1, 1.3**
func TestBuildAnthropicMessagesRequestDataDefaultsToolUseMaxTokens(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{URL: "https://anthropic.test", Model: "claude-test"}
	tools := []map[string]interface{}{{
		"name":        "write_file",
		"description": "write file",
		"parameters":  map[string]interface{}{"type": "object"},
	}}
	_, body, err := BuildAnthropicMessagesRequestData(cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": "write a file"},
	}, AnthropicMessagesRequestOptions{Tools: tools})
	if err != nil {
		t.Fatalf("BuildAnthropicMessagesRequestData: %v", err)
	}
	var req struct {
		MaxTokens int `json:"max_tokens"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if req.MaxTokens != cfg.EffectiveMaxOutputTokens() {
		t.Fatalf("max_tokens = %d, want %d", req.MaxTokens, cfg.EffectiveMaxOutputTokens())
	}
}

func TestBuildOpenAIChatRequestDataDefaultsToolUseMaxTokens(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/coding/paas/v4", Model: "glm-5.1", AgentType: "opencode"}
	tools := []map[string]interface{}{{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "write_file",
			"description": "write file",
			"parameters":  map[string]interface{}{"type": "object"},
		},
	}}
	_, body, err := BuildOpenAIChatRequestData(cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": "write a file"},
	}, OpenAIChatRequestOptions{Tools: tools})
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData: %v", err)
	}
	var req struct {
		MaxTokens int `json:"max_tokens"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if req.MaxTokens != cfg.EffectiveMaxOutputTokens() {
		t.Fatalf("max_tokens = %d, want %d", req.MaxTokens, cfg.EffectiveMaxOutputTokens())
	}
}

func TestBuildOpenAIChatRequestDataHonorsExplicitToolUseMaxTokens(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-chat"}
	tools := []map[string]interface{}{{
		"type": "function",
		"function": map[string]interface{}{
			"name":       "lookup",
			"parameters": map[string]interface{}{"type": "object"},
		},
	}}
	_, body, err := BuildOpenAIChatRequestData(cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": "lookup"},
	}, OpenAIChatRequestOptions{
		Tools:       tools,
		PassThrough: map[string]interface{}{"max_tokens": 512},
	})
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData: %v", err)
	}
	var req struct {
		MaxTokens int `json:"max_tokens"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if req.MaxTokens != 512 {
		t.Fatalf("max_tokens = %d, want 512", req.MaxTokens)
	}
}

func TestParseNonStreamOpenAIResponseBodyDetectsTruncatedToolCall(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_bad","type":"function","function":{"name":"write_file","arguments":"{\"path\":\"a.txt\",\"content\":\"unterminated"}}]},"finish_reason":"length"}]}`)
	resp, err := ParseNonStreamOpenAIResponseBody(body)
	if err != nil {
		t.Fatalf("ParseNonStreamOpenAIResponseBody: %v", err)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices = %d, want 1", len(resp.Choices))
	}
	choice := resp.Choices[0]
	if len(choice.TruncatedToolNames) != 1 || choice.TruncatedToolNames[0] != "write_file" {
		t.Fatalf("TruncatedToolNames = %#v, want write_file", choice.TruncatedToolNames)
	}
	if len(choice.Message.ToolCalls) != 0 {
		t.Fatalf("truncated tool calls should be removed: %#v", choice.Message.ToolCalls)
	}
	if choice.FinishReason != "length" {
		t.Fatalf("finish_reason = %q, want length", choice.FinishReason)
	}
}

func TestParseSSEToResponseDetectsTruncatedToolCallAndPreservesLength(t *testing.T) {
	body := []byte(strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"I'll write the script now:"}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_bad","type":"function","function":{"name":"write_file","arguments":"{\"path\":\"a.txt\",\"content\":\"unterminated"}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"length"}]}`,
		`data: [DONE]`,
		``,
	}, "\n\n"))
	resp, err := ParseSSEToResponse(body)
	if err != nil {
		t.Fatalf("ParseSSEToResponse: %v", err)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices = %d, want 1", len(resp.Choices))
	}
	choice := resp.Choices[0]
	if len(choice.TruncatedToolNames) != 1 || choice.TruncatedToolNames[0] != "write_file" {
		t.Fatalf("TruncatedToolNames = %#v, want write_file", choice.TruncatedToolNames)
	}
	if len(choice.Message.ToolCalls) != 0 {
		t.Fatalf("truncated tool calls should be removed: %#v", choice.Message.ToolCalls)
	}
	if choice.FinishReason != "length" {
		t.Fatalf("finish_reason = %q, want length", choice.FinishReason)
	}
	if choice.Message.Content != "I'll write the script now:" {
		t.Fatalf("content = %q, want preamble preserved", choice.Message.Content)
	}
}

func TestBuildAnthropicMessagesRequestDataDefaultsTextMaxTokens(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{URL: "https://anthropic.test", Model: "claude-test"}
	_, body, err := BuildAnthropicMessagesRequestData(cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": "ping"},
	}, AnthropicMessagesRequestOptions{})
	if err != nil {
		t.Fatalf("BuildAnthropicMessagesRequestData: %v", err)
	}
	var req struct {
		MaxTokens int `json:"max_tokens"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if req.MaxTokens != cfg.EffectiveMaxOutputTokens() {
		t.Fatalf("max_tokens = %d, want %d", req.MaxTokens, cfg.EffectiveMaxOutputTokens())
	}
}

func TestBuildAnthropicMessagesRequestDataHonorsMaxTokensOption(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{URL: "https://anthropic.test", Model: "claude-test"}
	_, body, err := BuildAnthropicMessagesRequestData(cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": "ping"},
	}, AnthropicMessagesRequestOptions{MaxTokens: 8})
	if err != nil {
		t.Fatalf("BuildAnthropicMessagesRequestData: %v", err)
	}
	var req struct {
		MaxTokens int `json:"max_tokens"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if req.MaxTokens != 8 {
		t.Fatalf("max_tokens = %d, want 8", req.MaxTokens)
	}
}

func TestDoAnthropicRequestUsesOfficialSDKShape(t *testing.T) {
	var gotPath, gotUserAgent string
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotUserAgent = r.Header.Get("User-Agent")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_test","type":"message","role":"assistant","model":"glm-5.1","content":[{"type":"text","text":"sdk ok"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":3,"output_tokens":2}}`))
	}))
	defer srv.Close()

	resp, err := DoAnthropicRequest(context.Background(), corelib.MaclawLLMConfig{
		URL:       srv.URL,
		Key:       "test-key",
		Model:     "glm-5.1",
		Protocol:  "anthropic",
		AgentType: "claude code 2.0",
	}, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, nil, srv.Client())
	if err != nil {
		t.Fatalf("DoAnthropicRequest: %v", err)
	}
	if gotPath != "/v1/messages" {
		t.Fatalf("path = %q, want /v1/messages", gotPath)
	}
	if gotUserAgent != "claude code 2.0" {
		t.Fatalf("User-Agent = %q", gotUserAgent)
	}
	if gotBody["model"] != "glm-5.1" {
		t.Fatalf("model = %#v", gotBody["model"])
	}
	if resp == nil || len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "sdk ok" {
		t.Fatalf("response = %#v", resp)
	}
	if resp.Usage.PromptTokens != 3 || resp.Usage.CompletionTokens != 2 || resp.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %#v", resp.Usage)
	}
}

func TestAnthropicSDKMessagePreservesToolSchemaBody(t *testing.T) {
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		captured = string(data)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_test","type":"message","role":"assistant","model":"glm-5.1","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`))
	}))
	defer srv.Close()

	tools := []map[string]interface{}{{
		"type": "function",
		"function": map[string]interface{}{
			"name": "lookup",
			"parameters": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"q": map[string]interface{}{"type": "string"}},
				"required":   []interface{}{"q"},
			},
		},
	}}
	_, body, err := BuildAnthropicMessagesRequestData(
		corelib.MaclawLLMConfig{URL: srv.URL, Model: "glm-5.1"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		AnthropicMessagesRequestOptions{Tools: tools},
	)
	if err != nil {
		t.Fatalf("BuildAnthropicMessagesRequestData: %v", err)
	}
	if _, _, _, err := anthropicSDKMessage(context.Background(), corelib.MaclawLLMConfig{
		URL:      srv.URL,
		Key:      "test-key",
		Model:    "glm-5.1",
		Protocol: "anthropic",
	}, body, srv.Client()); err != nil {
		t.Fatalf("anthropicSDKMessage: %v", err)
	}
	if strings.Contains(captured, `"properties":{"q":{"type":"string"},"properties":{},"type":"object"}`) {
		t.Fatalf("SDK request changed Anthropic tool schema: %s", captured)
	}
	if !strings.Contains(captured, `"input_schema":{"properties":{"q":{"type":"string"}},"required":["q"],"type":"object"}`) {
		t.Fatalf("SDK request did not preserve Anthropic tool schema: %s", captured)
	}
}

func TestAnthropicSDKMessageStreamPreservesToolSchemaBody(t *testing.T) {
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		captured = string(data)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`event: message_start` + "\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","model":"glm-5.1","content":[],"stop_reason":null,"usage":{"input_tokens":3,"output_tokens":0}}}` + "\n\n"))
		_, _ = w.Write([]byte(`event: content_block_start` + "\n"))
		_, _ = w.Write([]byte(`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n"))
		_, _ = w.Write([]byte(`event: content_block_delta` + "\n"))
		_, _ = w.Write([]byte(`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}` + "\n\n"))
		_, _ = w.Write([]byte(`event: message_delta` + "\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}` + "\n\n"))
		_, _ = w.Write([]byte(`event: message_stop` + "\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_stop"}` + "\n\n"))
	}))
	defer srv.Close()

	tools := []map[string]interface{}{{
		"type": "function",
		"function": map[string]interface{}{
			"name": "lookup",
			"parameters": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"q": map[string]interface{}{"type": "string"}},
				"required":   []interface{}{"q"},
			},
		},
	}}
	_, body, err := BuildAnthropicMessagesRequestData(
		corelib.MaclawLLMConfig{URL: srv.URL, Model: "glm-5.1"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		AnthropicMessagesRequestOptions{Stream: true, Tools: tools},
	)
	if err != nil {
		t.Fatalf("BuildAnthropicMessagesRequestData: %v", err)
	}
	if _, _, _, err := anthropicSDKMessageStream(context.Background(), corelib.MaclawLLMConfig{
		URL:      srv.URL,
		Key:      "test-key",
		Model:    "glm-5.1",
		Protocol: "anthropic",
	}, body, srv.Client(), nil); err != nil {
		t.Fatalf("anthropicSDKMessageStream: %v", err)
	}
	if strings.Contains(captured, `"properties":{"q":{"type":"string"},"properties":{},"type":"object"}`) {
		t.Fatalf("SDK stream request changed Anthropic tool schema: %s", captured)
	}
	if !strings.Contains(captured, `"input_schema":{"properties":{"q":{"type":"string"}},"required":["q"],"type":"object"}`) {
		t.Fatalf("SDK stream request did not preserve Anthropic tool schema: %s", captured)
	}
}

func TestAnthropicSDKMessagePreservesHTTP400RawBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"bad tool schema"}}`))
	}))
	defer srv.Close()

	_, status, body, err := anthropicSDKMessage(context.Background(), corelib.MaclawLLMConfig{
		URL:      srv.URL,
		Key:      "test-key",
		Model:    "glm-5.1",
		Protocol: "anthropic",
	}, []byte(`{"model":"glm-5.1","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`), srv.Client())
	if err == nil {
		t.Fatal("expected error")
	}
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	if !strings.Contains(string(body), "bad tool schema") {
		t.Fatalf("body = %s", body)
	}
}

func TestListAnthropicModelsWithSDKUsesOfficialSDKShape(t *testing.T) {
	var gotPath, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotUA = r.Header.Get("User-Agent")
		if r.Header.Get("x-api-key") != "test-key" {
			t.Fatalf("x-api-key = %q, want test-key", r.Header.Get("x-api-key"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"glm-5.1","display_name":"GLM 5.1"}]}`))
	}))
	defer srv.Close()

	models, err := ListAnthropicModelsWithSDK(context.Background(), corelib.MaclawLLMConfig{
		URL:       srv.URL,
		Key:       "test-key",
		Protocol:  "anthropic",
		AgentType: "claude code 2.0",
	}, srv.Client())
	if err != nil {
		t.Fatalf("ListAnthropicModelsWithSDK() error = %v", err)
	}
	if gotPath != "/v1/models" {
		t.Fatalf("path = %q, want /v1/models", gotPath)
	}
	if gotUA != "claude code 2.0" {
		t.Fatalf("User-Agent = %q, want claude code 2.0", gotUA)
	}
	if len(models) != 1 || models[0].ID != "glm-5.1" || models[0].DisplayName != "GLM 5.1" {
		t.Fatalf("models = %+v, want glm-5.1/GLM 5.1", models)
	}
}

func TestAnthropicSDKMessageStreamPreservesHTTP400RawBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"bad stream payload"}}`))
	}))
	defer srv.Close()

	_, status, body, err := anthropicSDKMessageStream(context.Background(), corelib.MaclawLLMConfig{
		URL:      srv.URL,
		Key:      "test-key",
		Model:    "glm-5.1",
		Protocol: "anthropic",
	}, []byte(`{"model":"glm-5.1","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`), srv.Client(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	if !strings.Contains(string(body), "bad stream payload") {
		t.Fatalf("body = %s", body)
	}
}

func TestConvertToAnthropicMessagesHandlesTypedOpenAICompatValues(t *testing.T) {
	type contentBlock struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	}
	type message struct {
		Role      string                   `json:"role"`
		Content   []contentBlock           `json:"content,omitempty"`
		ToolCalls []map[string]interface{} `json:"tool_calls,omitempty"`
	}
	converted := ConvertToAnthropicMessages([]interface{}{
		message{Role: "system", Content: []contentBlock{{Type: "text", Text: "system one"}}},
		message{Role: "developer", Content: []contentBlock{{Type: "text", Text: "developer one"}}},
		message{Role: "system", Content: []contentBlock{{Type: "input_text", Text: "system two"}}},
		message{
			Role:    "assistant",
			Content: []contentBlock{{Type: "output_text", Text: "thinking"}},
			ToolCalls: []map[string]interface{}{{
				"id":   "call_1",
				"type": "function",
				"function": map[string]interface{}{
					"name":      "search",
					"arguments": `{"q":"go"}`,
				},
			}},
		},
	})

	if converted.SystemText != "system one\ndeveloper one\nsystem two" {
		t.Fatalf("system text = %q", converted.SystemText)
	}
	if len(converted.Messages) != 1 {
		t.Fatalf("messages len = %d, want 1", len(converted.Messages))
	}
	assistant := converted.Messages[0].(map[string]interface{})
	blocks := assistant["content"].([]interface{})
	if len(blocks) != 2 {
		t.Fatalf("assistant blocks len = %d, want 2: %#v", len(blocks), blocks)
	}
	toolUse := blocks[1].(map[string]interface{})
	if toolUse["type"] != "tool_use" || toolUse["name"] != "search" {
		t.Fatalf("tool_use block = %#v", toolUse)
	}
}

func TestConvertToAnthropicMessagesStringifiesToolResultObjectContent(t *testing.T) {
	converted := ConvertToAnthropicMessages([]interface{}{
		map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": map[string]interface{}{"ok": true}},
	})

	if len(converted.Messages) != 1 {
		t.Fatalf("messages len = %d, want 1", len(converted.Messages))
	}
	msg := converted.Messages[0].(map[string]interface{})
	blocks := msg["content"].([]interface{})
	if len(blocks) != 1 {
		t.Fatalf("blocks len = %d, want 1", len(blocks))
	}
	block := blocks[0].(map[string]interface{})
	if got := block["content"]; got != `{"ok":true}` {
		t.Fatalf("tool result content = %#v, want JSON string", got)
	}
}

func TestBugCondition_SSE_MultiChunk(t *testing.T) {
	sseBody := strings.Join([]string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}",
		"data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}",
		"data: {\"choices\":[{\"delta\":{\"content\":\"!\"}}]}",
		"data: [DONE]",
		"",
	}, "\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseBody))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{
		URL:   srv.URL,
		Model: "test-model",
	}
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}

	resp, err := DoOpenAIRequest(context.Background(), cfg, messages, nil, srv.Client())
	if err != nil {
		t.Fatalf("DoOpenAIRequest returned error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice in response")
	}
	if got := resp.Choices[0].Message.Content; got != "Hello world!" {
		t.Errorf("content = %q, want %q", got, "Hello world!")
	}
}

// TestBugCondition_RequestBody_MissingStreamFalse verifies that DoOpenAIRequest
// sends "stream": false in the request body. On UNFIXED code this test FAILS
// because the field is absent — confirming the bug exists.
//
// **Validates: Requirements 1.2**
func TestBugCondition_RequestBody_MissingStreamFalse(t *testing.T) {
	var capturedBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		// Return valid JSON so the function doesn't error on the response side.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{
		URL:   srv.URL,
		Model: "test-model",
	}
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}

	_, err := DoOpenAIRequest(context.Background(), cfg, messages, nil, srv.Client())
	if err != nil {
		t.Fatalf("DoOpenAIRequest returned error: %v", err)
	}

	var reqMap map[string]interface{}
	if err := json.Unmarshal(capturedBody, &reqMap); err != nil {
		t.Fatalf("failed to parse captured request body: %v", err)
	}

	streamVal, ok := reqMap["stream"]
	if !ok {
		t.Fatal("request body does not contain \"stream\" key — expected \"stream\": false")
	}
	streamBool, isBool := streamVal.(bool)
	if !isBool || streamBool != false {
		t.Errorf("stream = %v, want false", streamVal)
	}
}

func TestBuildOpenAIChatRequestData_MergesSystemAndAddsExtrasForMiniMax(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{URL: "https://api.minimaxi.com/v1", Model: "MiniMax-M2.7"}
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": "system prompt"},
		map[string]interface{}{"role": "user", "content": "hi"},
		map[string]interface{}{
			"role":    "assistant",
			"content": "",
			"tool_calls": []interface{}{map[string]interface{}{
				"id":   "call_1",
				"type": "function",
				"function": map[string]interface{}{
					"name":      "search",
					"arguments": "{",
				},
			}},
		},
	}

	endpoint, body, err := BuildOpenAIChatRequestData(cfg, messages, OpenAIChatRequestOptions{
		Stream: true,
		Tools: []map[string]interface{}{{
			"type":     "function",
			"function": map[string]interface{}{"name": "search"},
		}},
		ExtraBody: map[string]interface{}{
			"temperature": 0.7,
			"stream_options": map[string]interface{}{
				"include_usage": true,
			},
			"stream": false,
		},
	})
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	if got, want := endpoint, "https://api.minimaxi.com/v1/chat/completions"; got != want {
		t.Fatalf("endpoint = %q, want %q", got, want)
	}

	var req struct {
		Model         string                   `json:"model"`
		Stream        bool                     `json:"stream"`
		Messages      []map[string]interface{} `json:"messages"`
		Tools         []map[string]interface{} `json:"tools"`
		Temperature   float64                  `json:"temperature"`
		StreamOptions map[string]interface{}   `json:"stream_options"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	if req.Model != "MiniMax-M2.7" {
		t.Fatalf("model = %q, want MiniMax-M2.7", req.Model)
	}
	if !req.Stream {
		t.Fatalf("stream = %v, want true", req.Stream)
	}
	if len(req.Tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(req.Tools))
	}
	if req.Temperature != 0.7 {
		t.Fatalf("temperature = %v, want 0.7", req.Temperature)
	}
	if req.StreamOptions["include_usage"] != true {
		t.Fatalf("stream_options.include_usage = %#v, want true", req.StreamOptions["include_usage"])
	}
	if len(req.Messages) != 2 {
		t.Fatalf("messages len = %d, want 2 after system merge", len(req.Messages))
	}
	if got := req.Messages[0]["content"]; got != "system prompt\n\nhi" {
		t.Fatalf("merged content = %#v, want %q", got, "system prompt\n\nhi")
	}
	assistantCalls, _ := req.Messages[1]["tool_calls"].([]interface{})
	assistantCall, _ := assistantCalls[0].(map[string]interface{})
	fn, _ := assistantCall["function"].(map[string]interface{})
	if got := fn["arguments"]; got != "{}" {
		t.Fatalf("arguments = %#v, want %q", got, "{}")
	}
}

func TestParseNonStreamOpenAIResponseBody_NormalizesContentPartsAndPreservesToolCalls(t *testing.T) {
	body := []byte(`{
		"choices": [{
			"message": {
				"role": "assistant",
				"content": [
					{"type": "text", "content": "hello"},
					{"type": "output_text", "text": "world"}
				],
				"reasoning_content": "reason",
				"tool_calls": [{
					"id": "call_1",
					"type": "function",
					"function": {"name": "search", "arguments": "{\"q\":\"go\"}"}
				}]
			},
			"finish_reason": "tool_calls"
		}]
	}`)

	resp, err := ParseNonStreamOpenAIResponseBody(body)
	if err != nil {
		t.Fatalf("ParseNonStreamOpenAIResponseBody returned error: %v", err)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices len = %d, want 1", len(resp.Choices))
	}
	msg := resp.Choices[0].Message
	if got := msg.Content; got != "hello\nworld" {
		t.Fatalf("content = %q, want %q", got, "hello\nworld")
	}
	if got := msg.ReasoningContent; got != "reason" {
		t.Fatalf("reasoning_content = %q, want %q", got, "reason")
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("tool_calls len = %d, want 1", len(msg.ToolCalls))
	}
	if got := msg.ToolCalls[0].Function.Name; got != "search" {
		t.Fatalf("tool name = %q, want %q", got, "search")
	}
	if got := msg.ToolCalls[0].Function.Arguments; got != `{"q":"go"}` {
		t.Fatalf("tool arguments = %q, want %q", got, `{"q":"go"}`)
	}
	if _, ok := msg.RawContent.([]interface{}); !ok {
		t.Fatalf("raw content type = %T, want []interface{}", msg.RawContent)
	}
}

func TestParseNonStreamOpenAIResponseBody_ConvertsLegacyFunctionCall(t *testing.T) {
	body := []byte(`{
		"choices": [{
			"message": {
				"role": "assistant",
				"content": "",
				"function_call": {
					"name": "bash",
					"arguments": "{\"command\":\"dir\"}"
				}
			},
			"finish_reason": "function_call"
		}]
	}`)

	resp, err := ParseNonStreamOpenAIResponseBody(body)
	if err != nil {
		t.Fatalf("ParseNonStreamOpenAIResponseBody returned error: %v", err)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices len = %d, want 1", len(resp.Choices))
	}
	if got := resp.Choices[0].FinishReason; got != "tool_calls" {
		t.Fatalf("finish_reason = %q, want tool_calls", got)
	}
	msg := resp.Choices[0].Message
	if got := msg.Content; got != "" {
		t.Fatalf("content = %q, want empty", got)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("tool_calls len = %d, want 1", len(msg.ToolCalls))
	}
	if got := msg.ToolCalls[0].Type; got != "function" {
		t.Fatalf("tool type = %q, want function", got)
	}
	if got := msg.ToolCalls[0].Function.Name; got != "bash" {
		t.Fatalf("tool name = %q, want bash", got)
	}
	if got := msg.ToolCalls[0].Function.Arguments; got != `{"command":"dir"}` {
		t.Fatalf("tool arguments = %q, want %q", got, `{"command":"dir"}`)
	}
}

func TestProjectOpenAIWireResponse_NormalizesTypedContentParts(t *testing.T) {
	type contentPart struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	}
	wire := openAIWireResponse{
		Choices: []openAIWireChoice{{
			Message: openAIWireMessage{
				Role: "assistant",
				Content: []contentPart{
					{Type: "output_text", Text: "hello"},
					{Type: "image_url"},
					{Type: "text", Text: "world"},
				},
			},
			FinishReason: "stop",
		}},
	}

	resp := projectOpenAIWireResponse(wire)
	if len(resp.Choices) != 1 {
		t.Fatalf("choices len = %d, want 1", len(resp.Choices))
	}
	msg := resp.Choices[0].Message
	if got := msg.Content; got != "hello\nworld" {
		t.Fatalf("content = %q, want %q", got, "hello\nworld")
	}
	if _, ok := msg.RawContent.([]contentPart); !ok {
		t.Fatalf("raw content type = %T, want []contentPart", msg.RawContent)
	}
}

func TestProjectOpenAIWireResponse_NormalizesMapSliceContentParts(t *testing.T) {
	wire := openAIWireResponse{
		Choices: []openAIWireChoice{{
			Message: openAIWireMessage{
				Role: "assistant",
				Content: []map[string]interface{}{
					{"type": "text", "content": "alpha"},
					{"type": "input_text", "text": "beta"},
				},
			},
			FinishReason: "stop",
		}},
	}

	resp := projectOpenAIWireResponse(wire)
	if got := resp.Choices[0].Message.Content; got != "alpha\nbeta" {
		t.Fatalf("content = %q, want %q", got, "alpha\nbeta")
	}
	if _, ok := resp.Choices[0].Message.RawContent.([]map[string]interface{}); !ok {
		t.Fatalf("raw content type = %T, want []map[string]interface{}", resp.Choices[0].Message.RawContent)
	}
}

func TestNewOpenAIChatRequest_SetsHeaders(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{
		URL:       "https://example.com/v1",
		Model:     "test-model",
		Key:       "secret-key",
		AgentType: "Kilo Code",
	}
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}

	req, _, endpoint, err := NewOpenAIChatRequest(context.Background(), cfg, messages, OpenAIChatRequestOptions{Stream: false})
	if err != nil {
		t.Fatalf("NewOpenAIChatRequest returned error: %v", err)
	}
	if got, want := endpoint, "https://example.com/v1/chat/completions"; got != want {
		t.Fatalf("endpoint = %q, want %q", got, want)
	}
	if got, want := req.Method, http.MethodPost; got != want {
		t.Fatalf("method = %q, want %q", got, want)
	}
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer secret-key" {
		t.Fatalf("Authorization = %q, want bearer header", got)
	}
	if got := req.Header.Get("User-Agent"); got != "Kilo Code" {
		t.Fatalf("User-Agent = %q, want Kilo Code", got)
	}
}

func TestNewOpenAIChatRequest_SetsXAIOAuthHeader(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{
		URL:          "https://api.x.ai/v1",
		Key:          "xai-oauth-token",
		Model:        "grok-4.5",
		ProviderName: "xAI-Grok",
		AuthType:     "oauth",
	}
	req, _, _, err := NewOpenAIChatRequest(context.Background(), cfg,
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		OpenAIChatRequestOptions{Stream: false})
	if err != nil {
		t.Fatalf("NewOpenAIChatRequest returned error: %v", err)
	}
	if got := req.Header.Get("X-XAI-Token-Auth"); got != "xai-grok-cli" {
		t.Fatalf("X-XAI-Token-Auth = %q, want xai-grok-cli", got)
	}

	cfg.AuthType = ""
	req, _, _, err = NewOpenAIChatRequest(context.Background(), cfg,
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		OpenAIChatRequestOptions{Stream: false})
	if err != nil {
		t.Fatalf("NewOpenAIChatRequest returned error: %v", err)
	}
	if got := req.Header.Get("X-XAI-Token-Auth"); got != "" {
		t.Fatalf("API-key-style xAI request sent OAuth marker %q", got)
	}
}

func TestOpenAIChatCompletionsEndpointNormalizesBaseURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"bare", "https://api.example.com", "https://api.example.com/v1/chat/completions"},
		{"v1", "https://api.example.com/v1", "https://api.example.com/v1/chat/completions"},
		{"paas v4", "https://open.bigmodel.cn/api/paas/v4", "https://open.bigmodel.cn/api/paas/v4/chat/completions"},
		{"full", "https://api.example.com/v1/chat/completions", "https://api.example.com/v1/chat/completions"},
		{"qwen compatible", "https://dashscope.aliyuncs.com/compatible-mode/v1", "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildOpenAIChatCompletionsEndpoint(tt.url); got != tt.want {
				t.Fatalf("endpoint = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildOpenAIModelsEndpointCandidates(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		protocol string
		want     []string
	}{
		{
			name: "bare",
			url:  "https://api.example.com",
			want: []string{
				"https://api.example.com/models",
				"https://api.example.com/v1/models",
			},
		},
		{
			name: "v1",
			url:  "https://api.example.com/v1",
			want: []string{"https://api.example.com/v1/models"},
		},
		{
			name: "models endpoint",
			url:  "https://api.example.com/v1/models",
			want: []string{"https://api.example.com/v1/models"},
		},
		{
			name: "chat completions endpoint",
			url:  "https://api.example.com/v1/chat/completions",
			want: []string{"https://api.example.com/v1/models"},
		},
		{
			name: "glm coding chat completions endpoint",
			url:  "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions",
			want: []string{"https://open.bigmodel.cn/api/coding/paas/v4/models"},
		},
		{
			name:     "anthropic prefers v1 then legacy",
			url:      "https://api.example.com/anthropic",
			protocol: "anthropic",
			want: []string{
				"https://api.example.com/anthropic/v1/models",
				"https://api.example.com/anthropic/models",
			},
		},
		{
			name: "glm coding plan normalized",
			url:  "https://open.bigmodel.cn/api/coding/paas/v4",
			want: []string{"https://open.bigmodel.cn/api/coding/paas/v4/models"},
		},
		{
			name: "volcengine tokenplan openai v3",
			url:  "https://ark.cn-beijing.volces.com/api/plan/v3",
			want: []string{"https://ark.cn-beijing.volces.com/api/plan/v3/models"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildOpenAIModelsEndpointCandidates(tt.url, tt.protocol); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("BuildOpenAIModelsEndpointCandidates() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestBuildOpenAIChatRequestData_NormalizesGLMCodingPlanEndpoint(t *testing.T) {
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}
	endpoint, _, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-5.1", AgentType: "Kilo Code"},
		messages,
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	if want := "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions"; endpoint != want {
		t.Fatalf("endpoint = %q, want %q", endpoint, want)
	}

	endpoint, _, err = BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-5.1", AgentType: "openclaw"},
		messages,
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	if want := "https://open.bigmodel.cn/api/paas/v4/chat/completions"; endpoint != want {
		t.Fatalf("non-coding endpoint = %q, want %q", endpoint, want)
	}
}

func TestResponsesEndpointNormalizesBaseURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"bare", "https://api.example.com", "https://api.example.com/v1/responses"},
		{"v1", "https://api.example.com/v1", "https://api.example.com/v1/responses"},
		{"full", "https://api.example.com/v1/responses", "https://api.example.com/v1/responses"},
		{"qwen compatible", "https://dashscope.aliyuncs.com/compatible-mode/v1", "https://dashscope.aliyuncs.com/compatible-mode/v1/responses"},
		{"volcengine tokenplan openai v3", "https://ark.cn-beijing.volces.com/api/plan/v3", "https://ark.cn-beijing.volces.com/api/plan/v3/responses"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildResponsesEndpoint(tt.url); got != tt.want {
				t.Fatalf("endpoint = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMiniMax_RequestBody_SanitizesInvalidReplayedToolArguments(t *testing.T) {
	var capturedBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{
		URL:   srv.URL + "/proxy/minimaxi.com",
		Model: "MiniMax-M2.7",
	}
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": "system prompt"},
		map[string]interface{}{"role": "user", "content": "hi"},
		map[string]interface{}{
			"role":    "assistant",
			"content": "",
			"tool_calls": []ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "search", Arguments: "{"},
			}},
		},
	}

	_, err := DoOpenAIRequest(context.Background(), cfg, messages, nil, srv.Client())
	if err != nil {
		t.Fatalf("DoOpenAIRequest returned error: %v", err)
	}

	var req struct {
		Messages []map[string]interface{} `json:"messages"`
	}
	if err := json.Unmarshal(capturedBody, &req); err != nil {
		t.Fatalf("failed to parse captured request body: %v", err)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("expected 2 messages after system merge, got %d", len(req.Messages))
	}
	if got := req.Messages[0]["content"]; got != "system prompt\n\nhi" {
		t.Fatalf("merged user content = %#v, want %q", got, "system prompt\n\nhi")
	}
	assistantCalls, ok := req.Messages[1]["tool_calls"].([]interface{})
	if !ok || len(assistantCalls) != 1 {
		t.Fatalf("assistant tool_calls = %#v, want 1 entry", req.Messages[1]["tool_calls"])
	}
	assistantCall, _ := assistantCalls[0].(map[string]interface{})
	fn, _ := assistantCall["function"].(map[string]interface{})
	if got := fn["arguments"]; got != "{}" {
		t.Fatalf("arguments = %#v, want %q", got, "{}")
	}
}

func TestMiniMax_RequestBody_PreservesValidReplayedToolArguments(t *testing.T) {
	var capturedBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{URL: srv.URL + "/minimaxi.com", Model: "MiniMax-M2.7"}
	messages := []interface{}{
		map[string]interface{}{
			"role":    "assistant",
			"content": "",
			"tool_calls": []interface{}{map[string]interface{}{
				"id":   "call_1",
				"type": "function",
				"function": map[string]interface{}{
					"name":      "search",
					"arguments": `{"q":"golang"}`,
				},
			}},
		},
	}

	_, err := DoOpenAIRequest(context.Background(), cfg, messages, nil, srv.Client())
	if err != nil {
		t.Fatalf("DoOpenAIRequest returned error: %v", err)
	}

	var req struct {
		Messages []map[string]interface{} `json:"messages"`
	}
	if err := json.Unmarshal(capturedBody, &req); err != nil {
		t.Fatalf("failed to parse captured request body: %v", err)
	}
	assistantCalls, _ := req.Messages[0]["tool_calls"].([]interface{})
	assistantCall, _ := assistantCalls[0].(map[string]interface{})
	fn, _ := assistantCall["function"].(map[string]interface{})
	if got := fn["arguments"]; got != `{"q":"golang"}` {
		t.Fatalf("arguments = %#v, want valid JSON preserved", got)
	}
}

func TestOpenAI_RequestBody_SanitizesInvalidReplayedToolArguments(t *testing.T) {
	var capturedBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model"}
	messages := []interface{}{
		map[string]interface{}{
			"role":    "assistant",
			"content": "",
			"tool_calls": []interface{}{map[string]interface{}{
				"id":   "call_1",
				"type": "function",
				"function": map[string]interface{}{
					"name":      "search",
					"arguments": "{",
				},
			}},
		},
	}

	_, err := DoOpenAIRequest(context.Background(), cfg, messages, nil, srv.Client())
	if err != nil {
		t.Fatalf("DoOpenAIRequest returned error: %v", err)
	}

	var req struct {
		Messages []map[string]interface{} `json:"messages"`
	}
	if err := json.Unmarshal(capturedBody, &req); err != nil {
		t.Fatalf("failed to parse captured request body: %v", err)
	}
	assistantCalls, _ := req.Messages[0]["tool_calls"].([]interface{})
	assistantCall, _ := assistantCalls[0].(map[string]interface{})
	fn, _ := assistantCall["function"].(map[string]interface{})
	if got := fn["arguments"]; got != "{}" {
		t.Fatalf("arguments = %#v, want sanitized empty object", got)
	}
}

func TestOpenAI_RequestBody_StringifiesReplayedToolArgumentObjects(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "gpt-test"},
		[]interface{}{
			map[string]interface{}{
				"role":    "assistant",
				"content": "",
				"tool_calls": []interface{}{map[string]interface{}{
					"id":   "call_1",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "search",
						"arguments": map[string]interface{}{"q": "golang", "limit": float64(3)},
					},
				}},
			},
			map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": "ok"},
			map[string]interface{}{
				"role":    "assistant",
				"content": "",
				"function_call": map[string]interface{}{
					"name":      "legacy_search",
					"arguments": map[string]interface{}{"q": "legacy"},
				},
			},
		},
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req struct {
		Messages []map[string]interface{} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	assistantCalls, _ := req.Messages[0]["tool_calls"].([]interface{})
	assistantCall, _ := assistantCalls[0].(map[string]interface{})
	fn, _ := assistantCall["function"].(map[string]interface{})
	if got := fn["arguments"]; got != `{"limit":3,"q":"golang"}` {
		t.Fatalf("tool arguments = %#v, want object encoded as JSON string", got)
	}
	functionCall := req.Messages[2]["function_call"].(map[string]interface{})
	if got := functionCall["arguments"]; got != `{"q":"legacy"}` {
		t.Fatalf("function_call arguments = %#v, want object encoded as JSON string", got)
	}
}

func TestOpenAI_RequestBody_NormalizesMissingToolCallIDsAndTypes(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "gpt-test"},
		[]interface{}{
			map[string]interface{}{
				"role":    "assistant",
				"content": "",
				"tool_calls": []interface{}{map[string]interface{}{
					"function": map[string]interface{}{
						"name":      "search",
						"arguments": map[string]interface{}{"q": "golang"},
					},
				}},
			},
			map[string]interface{}{"role": "tool", "tool_call_id": "", "content": "ok"},
		},
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req struct {
		Messages []map[string]interface{} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("messages len = %d, want 2: %s", len(req.Messages), string(body))
	}
	assistantCalls, ok := req.Messages[0]["tool_calls"].([]interface{})
	if !ok || len(assistantCalls) != 1 {
		t.Fatalf("assistant tool_calls = %#v", req.Messages[0]["tool_calls"])
	}
	assistantCall, _ := assistantCalls[0].(map[string]interface{})
	callID, _ := assistantCall["id"].(string)
	if callID == "" {
		t.Fatalf("assistant tool call id empty: %#v", assistantCall)
	}
	if got := assistantCall["type"]; got != "function" {
		t.Fatalf("assistant tool call type = %#v, want function", got)
	}
	if got := req.Messages[1]["tool_call_id"]; got != callID {
		t.Fatalf("tool_call_id = %#v, want generated id %q", got, callID)
	}
}

func TestNormalizeOpenAIChatToolCallLinkageFillsEmptyToolResultID(t *testing.T) {
	messages := normalizeOpenAIChatToolCallLinkage([]interface{}{
		map[string]interface{}{
			"role": "assistant",
			"tool_calls": []interface{}{map[string]interface{}{
				"function": map[string]interface{}{"name": "search", "arguments": "{}"},
			}},
		},
		map[string]interface{}{"role": "tool", "tool_call_id": "", "content": "ok"},
	})
	if len(messages) != 2 {
		t.Fatalf("messages len = %d, want 2", len(messages))
	}
	assistantMsg := messages[0].(map[string]interface{})
	assistantCalls := assistantMsg["tool_calls"].([]map[string]interface{})
	callID := assistantCalls[0]["id"].(string)
	if callID == "" {
		t.Fatalf("generated call id empty: %#v", assistantCalls[0])
	}
	toolMsg := messages[1].(map[string]interface{})
	if got := toolMsg["tool_call_id"]; got != callID {
		t.Fatalf("tool_call_id = %#v, want %q", got, callID)
	}
}

func TestSanitizeOrphanedToolCallsKeepsNormalizedMapToolCalls(t *testing.T) {
	messages := normalizeOpenAIChatToolCallLinkage([]interface{}{
		map[string]interface{}{
			"role": "assistant",
			"tool_calls": []interface{}{map[string]interface{}{
				"function": map[string]interface{}{"name": "search", "arguments": "{}"},
			}},
		},
		map[string]interface{}{"role": "tool", "tool_call_id": "", "content": "ok"},
	})
	messages = sanitizeOpenAIChatMessagesForSDKCompatibility(messages, false)
	if ids := extractToolCallIDs(messages[0]); len(ids) != 1 {
		t.Fatalf("extractToolCallIDs = %#v from %#v", ids, messages[0])
	}
	ids := extractToolCallIDs(messages[0])
	toolMsg := messages[1].(map[string]interface{})
	if got := toolMsg["tool_call_id"]; got != ids[0] {
		t.Fatalf("pre-orphan tool_call_id = %#v, want %q; messages=%#v", got, ids[0], messages)
	}
	got := sanitizeOrphanedToolCalls(messages, false)
	if len(got) != 2 {
		t.Fatalf("sanitized len = %d, want 2: %#v", len(got), got)
	}
}

func TestOpenAI_RequestBody_SanitizesTypedMapToolArguments(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-27b"},
		[]interface{}{
			map[string]interface{}{
				"role": "assistant",
				"tool_calls": []map[string]interface{}{{
					"id":   "call_1",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "search",
						"arguments": "{",
					},
				}},
			},
			map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": "ok"},
		},
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req struct {
		Messages []map[string]interface{} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	assistantCalls, _ := req.Messages[0]["tool_calls"].([]interface{})
	assistantCall, _ := assistantCalls[0].(map[string]interface{})
	fn, _ := assistantCall["function"].(map[string]interface{})
	if got := fn["arguments"]; got != "{}" {
		t.Fatalf("arguments = %#v, want sanitized empty object", got)
	}
}

func TestOpenAI_RequestBody_SanitizesTypedStructToolArguments(t *testing.T) {
	type toolFunction struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	type toolCall struct {
		ID       string       `json:"id"`
		Type     string       `json:"type"`
		Function toolFunction `json:"function"`
	}
	type message struct {
		Role      string     `json:"role"`
		Content   string     `json:"content"`
		ToolCalls []toolCall `json:"tool_calls,omitempty"`
	}

	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-27b"},
		[]interface{}{
			message{
				Role:    "assistant",
				Content: "",
				ToolCalls: []toolCall{{
					ID:       "call_1",
					Type:     "function",
					Function: toolFunction{Name: "search", Arguments: "{"},
				}},
			},
			map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": "ok"},
		},
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req struct {
		Messages []map[string]interface{} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	assistantCalls, _ := req.Messages[0]["tool_calls"].([]interface{})
	assistantCall, _ := assistantCalls[0].(map[string]interface{})
	fn, _ := assistantCall["function"].(map[string]interface{})
	if got := fn["arguments"]; got != "{}" {
		t.Fatalf("arguments = %#v, want sanitized empty object", got)
	}
}

func TestOpenAI_RequestBody_SanitizesInvalidLegacyFunctionCallArguments(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://example.test/v1", Model: "test-model"},
		[]interface{}{map[string]interface{}{
			"role":    "assistant",
			"content": "",
			"function_call": map[string]interface{}{
				"name":      "search",
				"arguments": "{",
			},
		}},
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req struct {
		Messages []map[string]interface{} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	fn := req.Messages[0]["function_call"].(map[string]interface{})
	if got := fn["arguments"]; got != "{}" {
		t.Fatalf("legacy function_call arguments = %#v, want sanitized empty object", got)
	}
}

func TestSanitizeInvalidToolCallArgumentsHandlesTypedMessages(t *testing.T) {
	type toolFunction struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	type toolCall struct {
		ID       string       `json:"id"`
		Type     string       `json:"type"`
		Function toolFunction `json:"function"`
	}
	type message struct {
		Role      string     `json:"role"`
		Content   string     `json:"content"`
		ToolCalls []toolCall `json:"tool_calls,omitempty"`
	}

	got := sanitizeInvalidToolCallArguments([]interface{}{message{
		Role:    "assistant",
		Content: "",
		ToolCalls: []toolCall{{
			ID:       "call_1",
			Type:     "function",
			Function: toolFunction{Name: "search", Arguments: "{"},
		}},
	}})

	msg := got[0].(map[string]interface{})
	calls := msg["tool_calls"].([]interface{})
	call := calls[0].(map[string]interface{})
	fn := call["function"].(map[string]interface{})
	if got := fn["arguments"]; got != "{}" {
		t.Fatalf("arguments = %#v, want sanitized empty object", got)
	}
}

func TestOpenAI_RequestBody_DoesNotInventMissingToolFunction(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://example.test/v1", Model: "test-model"},
		[]interface{}{map[string]interface{}{
			"role": "assistant",
			"tool_calls": []interface{}{map[string]interface{}{
				"id":   "call_1",
				"type": "function",
			}},
		}},
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req struct {
		Messages []map[string]interface{} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	if _, ok := req.Messages[0]["tool_calls"]; ok {
		t.Fatalf("malformed tool call should be dropped, not repaired: %#v", req.Messages[0])
	}
}

func TestOpenAI_RequestBody_StripsTypedTrailingOrphanedToolCalls(t *testing.T) {
	type toolFunction struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	type toolCall struct {
		ID       string       `json:"id"`
		Type     string       `json:"type"`
		Function toolFunction `json:"function"`
	}
	type message struct {
		Role      string     `json:"role"`
		Content   string     `json:"content"`
		ToolCalls []toolCall `json:"tool_calls,omitempty"`
	}

	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-27b"},
		[]interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
			message{
				Role:    "assistant",
				Content: "",
				ToolCalls: []toolCall{{
					ID:       "call_1",
					Type:     "function",
					Function: toolFunction{Name: "search", Arguments: `{"q":"x"}`},
				}},
			},
		},
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req struct {
		Messages []map[string]interface{} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	if len(req.Messages) != 2 || req.Messages[1]["role"] != "assistant" || req.Messages[1]["content"] != "" {
		t.Fatalf("orphaned assistant message should have explicit empty content: %#v", req.Messages)
	}
}

func TestCopyMapWithoutHandlesTypedMessages(t *testing.T) {
	type message struct {
		Role      string `json:"role"`
		Content   string `json:"content"`
		ToolCalls []any  `json:"tool_calls,omitempty"`
	}

	got := copyMapWithout(message{
		Role:      "assistant",
		Content:   "done",
		ToolCalls: []any{map[string]any{"id": "call_1"}},
	}, "tool_calls")
	msg := got.(map[string]interface{})
	if msg["role"] != "assistant" || msg["content"] != "done" {
		t.Fatalf("typed message fields not preserved: %#v", msg)
	}
	if _, ok := msg["tool_calls"]; ok {
		t.Fatalf("tool_calls leaked after copyMapWithout: %#v", msg)
	}
}

func TestOpenAI_RequestBody_StripsEmptyToolCalls(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://example.test/v1", Model: "test-model"},
		[]interface{}{
			map[string]interface{}{"role": "assistant", "content": "done", "tool_calls": []interface{}{}},
			map[string]interface{}{"role": "assistant", "content": "done again", "tool_calls": []ToolCall{}},
		},
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req struct {
		Messages []map[string]interface{} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	for i, message := range req.Messages {
		if _, ok := message["tool_calls"]; ok {
			t.Fatalf("message %d leaked empty tool_calls: %#v", i, message)
		}
	}
}

func TestOpenAI_RequestBody_AddsEmptyContentToAssistantWithoutContentOrToolCalls(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"},
		[]interface{}{
			map[string]interface{}{"role": "user", "content": "continue"},
			// This mirrors a malformed historical tool turn after its invalid
			// tool_calls were discarded by the compatibility sanitizer.
			map[string]interface{}{"role": "assistant", "tool_calls": []interface{}{}},
			map[string]interface{}{"role": "user", "content": "finish the task"},
		},
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req struct {
		Messages []map[string]interface{} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	if len(req.Messages) != 3 {
		t.Fatalf("messages = %#v, want the assistant turn retained with empty content", req.Messages)
	}
	assistant := req.Messages[1]
	if assistant["role"] != "assistant" || assistant["content"] != "" {
		t.Fatalf("assistant message = %#v, want explicit empty content", assistant)
	}
	if _, hasToolCalls := assistant["tool_calls"]; hasToolCalls {
		t.Fatalf("empty tool_calls leaked: %#v", assistant)
	}
}

func TestOpenAI_RequestBody_PreservesExplicitNullAssistantContent(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"},
		[]interface{}{
			map[string]interface{}{"role": "user", "content": "continue"},
			map[string]interface{}{"role": "assistant", "content": nil},
		},
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req struct {
		Messages []map[string]interface{} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("messages = %#v, want explicit-null assistant retained", req.Messages)
	}
	if content, ok := req.Messages[1]["content"]; !ok || content != "" {
		t.Fatalf("assistant content = %#v, want sanitized empty string", content)
	}
}

func TestOpenAI_RequestBody_PreservesExplicitEmptyAssistantContent(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"},
		[]interface{}{
			map[string]interface{}{"role": "user", "content": "continue"},
			map[string]interface{}{"role": "assistant", "content": ""},
		},
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req struct {
		Messages []map[string]interface{} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	if len(req.Messages) != 2 || req.Messages[1]["content"] != "" {
		t.Fatalf("explicit empty assistant content was not preserved: %#v", req.Messages)
	}
}

func TestOpenAI_RequestBody_AddsContentWhenOnlyLegacyFunctionCallIsPresent(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"},
		[]interface{}{
			map[string]interface{}{"role": "user", "content": "continue"},
			map[string]interface{}{
				"role": "assistant",
				"function_call": map[string]interface{}{
					"name":      "legacy_search",
					"arguments": `{}`,
				},
			},
		},
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req struct {
		Messages []map[string]interface{} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	assistant := req.Messages[1]
	if assistant["content"] != "" {
		t.Fatalf("assistant content = %#v, want explicit empty string", assistant["content"])
	}
	if _, hasLegacyCall := assistant["function_call"]; !hasLegacyCall {
		t.Fatalf("legacy function call should still be preserved: %#v", assistant)
	}
}

func TestOpenAI_RequestBody_StripsTrailingOrphanedToolCalls(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-27b"},
		[]interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
			map[string]interface{}{
				"role":    "assistant",
				"content": "",
				"tool_calls": []interface{}{map[string]interface{}{
					"id":   "call_1",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "search",
						"arguments": `{"q":"x"}`,
					},
				}},
			},
		},
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req struct {
		Messages []map[string]interface{} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	if len(req.Messages) != 2 || req.Messages[1]["role"] != "assistant" || req.Messages[1]["content"] != "" {
		t.Fatalf("orphaned assistant message should have explicit empty content: %#v", req.Messages)
	}
}

func TestOpenAI_RequestBody_DropsOrphanedToolMessagesAfterPartialToolResults(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://example.test/v1", Model: "test-model"},
		[]interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
			map[string]interface{}{
				"role":    "assistant",
				"content": "",
				"tool_calls": []interface{}{
					map[string]interface{}{"id": "call_1", "type": "function", "function": map[string]interface{}{"name": "a", "arguments": `{}`}},
					map[string]interface{}{"id": "call_2", "type": "function", "function": map[string]interface{}{"name": "b", "arguments": `{}`}},
				},
			},
			map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": "partial"},
			map[string]interface{}{"role": "user", "content": "next"},
		},
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req struct {
		Messages []map[string]interface{} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	for i, message := range req.Messages {
		if _, ok := message["tool_calls"]; ok {
			t.Fatalf("message %d leaked orphaned tool_calls: %#v", i, message)
		}
		if role, _ := message["role"].(string); role == "tool" {
			t.Fatalf("message %d leaked orphaned tool result: %#v", i, message)
		}
	}
}

func TestOpenAI_RequestBody_NormalizesCodeGenAutoModel(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://codegen.qianxin-inc.cn/api/v1", Model: "auto"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	if got := req["model"]; got != corelib.CodeGenDefaultModelID {
		t.Fatalf("CodeGen model = %#v, want %q", got, corelib.CodeGenDefaultModelID)
	}

	_, body, err = BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://hub.example.test/api/llm/v1", Model: "auto"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	if got := req["model"]; got != "auto" {
		t.Fatalf("non-CodeGen model = %#v, want auto", got)
	}
}

func TestOpenAI_RequestBody_StripsCodeGenUnsupportedStreamOptions(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://codegen.qianxin-inc.cn/api/v1", Model: "auto"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		OpenAIChatRequestOptions{
			Stream: true,
			ExtraBody: map[string]interface{}{
				"metadata":           map[string]interface{}{"trace": "x"},
				"store":              true,
				"stream_options":     map[string]interface{}{"include_usage": true},
				"audio":              map[string]interface{}{"voice": "alloy"},
				"web_search_options": map[string]interface{}{"search_context_size": "low"},
			},
			PassThrough: map[string]interface{}{
				"parallel_tool_calls": true,
				"logprobs":            true,
				"top_logprobs":        2,
				"service_tier":        "auto",
				"reasoning_effort":    "low",
				"modalities":          []interface{}{"text"},
				"prediction":          map[string]interface{}{"type": "content", "content": "OK"},
			},
			ToolChoice: "auto",
			ResponseFormat: map[string]interface{}{
				"type": "json_schema",
				"json_schema": map[string]interface{}{
					"name":   "codegen",
					"schema": map[string]interface{}{"type": "object"},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	for _, key := range []string{"stream_options", "parallel_tool_calls", "store", "metadata", "response_format", "tool_choice", "function_call", "logprobs", "top_logprobs", "service_tier", "reasoning_effort", "modalities", "prediction", "audio", "web_search_options"} {
		if _, ok := req[key]; ok {
			t.Fatalf("CodeGen request leaked %s: %#v", key, req)
		}
	}
	if got := req["stream"]; got != true {
		t.Fatalf("stream = %#v, want true", got)
	}
}

func TestOpenAI_RequestBody_SanitizesQwenOpenAICompatProvider(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-27b", ProviderName: "Qwen 27B"},
		[]interface{}{
			map[string]interface{}{"role": "system", "content": "base system"},
			map[string]interface{}{"role": "user", "content": "hi", "timestamp": "2026-06-11T17:40:44+08:00", "internal_trace": "drop-me"},
			map[string]interface{}{"role": "system", "content": "[Skill preference] prefer skill"},
			map[string]interface{}{"role": "assistant", "content": "previous", "reasoning_content": "hidden", "created_at": "drop-me"},
			map[string]interface{}{"role": "assistant", "content": nil},
			map[string]interface{}{"role": "assistant", "content": "", "tool_calls": []interface{}{map[string]interface{}{"id": "call_1", "type": "function", "extra": "drop-me", "function": map[string]interface{}{"name": "bash", "arguments": "{}", "extra": "drop-me"}}}},
			map[string]interface{}{"role": "tool_result", "tool_use_id": "call_1", "content": map[string]interface{}{"ok": true}},
		},
		OpenAIChatRequestOptions{
			Stream: true,
			Tools: []map[string]interface{}{{
				"type": "function",
				"function": map[string]interface{}{
					"name":   "strict_tool",
					"strict": true,
					"parameters": map[string]interface{}{
						"type":                 "object",
						"additionalProperties": false,
						"properties": map[string]interface{}{
							"ids": map[string]interface{}{"type": "array"},
						},
					},
				},
			}},
			ExtraBody: map[string]interface{}{
				"metadata":           map[string]interface{}{"trace": "x"},
				"store":              true,
				"stream_options":     map[string]interface{}{"include_usage": true},
				"audio":              map[string]interface{}{"voice": "alloy"},
				"web_search_options": map[string]interface{}{"search_context_size": "low"},
			},
			PassThrough: map[string]interface{}{
				"parallel_tool_calls": true,
				"logprobs":            true,
				"top_logprobs":        2,
				"service_tier":        "auto",
				"reasoning_effort":    "low",
				"modalities":          []interface{}{"text"},
				"prediction":          map[string]interface{}{"type": "content", "content": "OK"},
			},
			ToolChoice: "auto",
			ResponseFormat: map[string]interface{}{
				"type": "json_schema",
				"json_schema": map[string]interface{}{
					"name":   "intent",
					"schema": map[string]interface{}{"type": "object"},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	for _, key := range []string{"stream_options", "parallel_tool_calls", "store", "metadata", "response_format", "tool_choice", "function_call", "logprobs", "top_logprobs", "service_tier", "reasoning_effort", "modalities", "prediction", "audio", "web_search_options"} {
		if _, ok := req[key]; ok {
			t.Fatalf("Qwen request leaked %s: %#v", key, req)
		}
	}
	messages := req["messages"].([]interface{})
	if len(messages) != 6 {
		t.Fatalf("messages len = %d, want 6: %#v", len(messages), messages)
	}
	first := messages[0].(map[string]interface{})
	if first["role"] != "system" || !strings.Contains(first["content"].(string), "base system") || !strings.Contains(first["content"].(string), "[Skill preference] prefer skill") {
		t.Fatalf("system messages were not merged into leading system message: %#v", first)
	}
	for i, message := range messages[1:] {
		msg := message.(map[string]interface{})
		if role := msg["role"]; role == "system" {
			t.Fatalf("non-leading system message leaked at index %d: %#v", i+1, message)
		}
		if _, ok := msg["reasoning_content"]; ok {
			t.Fatalf("Qwen request leaked reasoning_content in message: %#v", message)
		}
		if content, ok := msg["content"]; ok && content == nil {
			t.Fatalf("Qwen request leaked null content in message: %#v", message)
		}
		for _, key := range []string{"timestamp", "internal_trace", "created_at"} {
			if _, ok := msg[key]; ok {
				t.Fatalf("Qwen request leaked non-SDK message field %s: %#v", key, message)
			}
		}
	}
	assistantToolCallMsg := messages[4].(map[string]interface{})
	toolCalls := assistantToolCallMsg["tool_calls"].([]interface{})
	toolCall := toolCalls[0].(map[string]interface{})
	if _, ok := toolCall["extra"]; ok {
		t.Fatalf("Qwen tool_call leaked non-SDK field: %#v", toolCall)
	}
	if got := toolCall["type"]; got != "function" {
		t.Fatalf("tool_call type = %#v, want function", got)
	}
	toolCallFn := toolCall["function"].(map[string]interface{})
	if _, ok := toolCallFn["extra"]; ok {
		t.Fatalf("Qwen tool_call function leaked non-SDK field: %#v", toolCallFn)
	}
	toolResult := messages[5].(map[string]interface{})
	if got := toolResult["role"]; got != "tool" {
		t.Fatalf("legacy tool_result role = %#v, want tool", got)
	}
	if got := toolResult["tool_call_id"]; got != "call_1" {
		t.Fatalf("tool_call_id = %#v, want call_1", got)
	}
	if got := toolResult["content"]; got != `{"ok":true}` {
		t.Fatalf("tool result content = %#v, want JSON string", got)
	}
	fn := req["tools"].([]interface{})[0].(map[string]interface{})["function"].(map[string]interface{})
	if _, ok := fn["strict"]; ok {
		t.Fatalf("Qwen tool leaked strict: %#v", fn)
	}
	params := fn["parameters"].(map[string]interface{})
	if _, ok := params["additionalProperties"]; ok {
		t.Fatalf("Qwen tool schema leaked additionalProperties: %#v", params)
	}
	items := params["properties"].(map[string]interface{})["ids"].(map[string]interface{})["items"].(map[string]interface{})
	if got := items["type"]; got != "string" {
		t.Fatalf("array items type = %#v, want string", got)
	}
}

func TestOpenAI_RequestBody_RelocatesOversizedQwenSystemPrompt(t *testing.T) {
	largeSystem := strings.Repeat("runtime instruction and context\n", 700)
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://codegen.qianxin-inc.cn/api/v1", Model: "qax-codegen/Auto", ProviderName: "CodeGen"},
		[]interface{}{
			map[string]interface{}{"role": "system", "content": largeSystem},
			map[string]interface{}{"role": "user", "content": "write the document"},
			map[string]interface{}{"role": "system", "content": "[Skill preference] prefer reusable skill"},
		},
		OpenAIChatRequestOptions{Stream: true},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	messages := req["messages"].([]interface{})
	if len(messages) != 2 {
		t.Fatalf("messages len = %d, want 2: %#v", len(messages), messages)
	}
	system := messages[0].(map[string]interface{})
	if system["role"] != "system" {
		t.Fatalf("first role = %#v, want system", system["role"])
	}
	if got := len(system["content"].(string)); got >= conservativeOpenAICompatSystemPromptLimit {
		t.Fatalf("system content len = %d, want below conservative limit", got)
	}
	user := messages[1].(map[string]interface{})
	userContent := user["content"].(string)
	for _, want := range []string{"[Runtime context]", "[Skill preference] prefer reusable skill", "write the document"} {
		if !strings.Contains(userContent, want) {
			t.Fatalf("relocated user content missing %q: %.200q", want, userContent)
		}
	}
	if strings.Contains(userContent, "runtime instruction and context\nruntime instruction and context") {
		t.Fatalf("relocated user content leaked oversized runtime prompt: %.200q", userContent)
	}
	if len(userContent) > conservativeOpenAICompatRuntimeContextLimit+1200 {
		t.Fatalf("relocated user content too large: len=%d", len(userContent))
	}
}

func TestOpenAI_RequestBody_RelocatesOversizedQwenSystemPromptKeepsTaskContext(t *testing.T) {
	largeSystem := strings.Repeat("runtime instruction and context\n", 700) + `
## 当前任务

用户需求：BUG修复验证专项用例.xlsx 测试用例 根据 中再集团-U1SP5测试报告.docx 生成一份 星网U8BUG的回归验证测试报告。

[用户选择的本地文件路径]
E:\BUG修复验证专项用例.xlsx
E:\中再集团-U1SP5测试报告.docx

项目路径：e:\test4

## 阶段指令

请基于前序阶段的产出物和用户需求，生成本阶段的完整文档内容（Markdown 格式）。

## 重要约束
- 只生成一份文档，输出完毕后立即停止。

## 无关大段
` + strings.Repeat("noise\n", 900)
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://codegen.qianxin-inc.cn/api/v1", Model: "qax-codegen/Auto", ProviderName: "CodeGen"},
		[]interface{}{
			map[string]interface{}{"role": "system", "content": largeSystem},
			map[string]interface{}{"role": "user", "content": "请现在生成「测试策略」阶段的完整文档内容。"},
			map[string]interface{}{"role": "system", "content": "[Skill preference] prefer reusable skill"},
		},
		OpenAIChatRequestOptions{Stream: true},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	messages := req["messages"].([]interface{})
	if len(messages) != 2 {
		t.Fatalf("messages len = %d, want 2: %#v", len(messages), messages)
	}
	userContent := messages[1].(map[string]interface{})["content"].(string)
	for _, want := range []string{"星网U8BUG", `e:\test4`, "测试策略", "[Skill preference] prefer reusable skill"} {
		if !strings.Contains(userContent, want) {
			t.Fatalf("relocated user content missing %q: %.400q", want, userContent)
		}
	}
	for _, unexpected := range []string{"runtime instruction and context\nruntime instruction and context", "noise\nnoise"} {
		if strings.Contains(userContent, unexpected) {
			t.Fatalf("relocated user content leaked %q: %.400q", unexpected, userContent)
		}
	}
	if len(userContent) > conservativeOpenAICompatRuntimeContextLimit+1600 {
		t.Fatalf("relocated user content too large: len=%d", len(userContent))
	}
}

func TestCompactOpenAICompatMessagesForToollessRetryKeepsLatestUserAndTaskContext(t *testing.T) {
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": strings.Repeat("runtime context\n", 900) + `
## 当前任务
用户需求：生成星网U8BUG回归验证测试报告。

[用户选择的本地文件路径]
E:\BUG修复验证专项用例.xlsx
E:\中再集团-U1SP5测试报告.docx

项目路径：e:\test4

## 阶段指令
生成测试策略阶段完整文档。

## 无关大段
` + strings.Repeat("noise\n", 900)},
		map[string]interface{}{"role": "user", "content": "older request"},
		map[string]interface{}{"role": "system", "content": "[Skill preference] prefer reusable skill"},
		map[string]interface{}{"role": "user", "content": "latest request"},
	}
	compact := CompactOpenAICompatMessagesForToollessRetry(
		corelib.MaclawLLMConfig{URL: "https://codegen.qianxin-inc.cn/api/v1", Model: "qax-codegen/Auto", ProviderName: "CodeGen"},
		messages,
	)
	if len(compact) != 2 {
		t.Fatalf("compact len = %d, want 2: %#v", len(compact), compact)
	}
	system := compact[0].(map[string]interface{})
	if system["role"] != "system" || system["content"] != conservativeOpenAICompatCompactSystemPrompt {
		t.Fatalf("compact system = %#v", system)
	}
	user := compact[1].(map[string]interface{})
	userContent := user["content"].(string)
	if !strings.Contains(userContent, "latest request") {
		t.Fatalf("compact user missing latest request: %q", userContent)
	}
	for _, want := range []string{"[Relevant runtime context]", "星网U8BUG", `e:\test4`, "[Skill preference] prefer reusable skill"} {
		if !strings.Contains(userContent, want) {
			t.Fatalf("compact user missing %q: %q", want, userContent)
		}
	}
	for _, unexpected := range []string{"older request", "runtime context\nruntime context", "noise\nnoise"} {
		if strings.Contains(userContent, unexpected) {
			t.Fatalf("compact user leaked %q: %q", unexpected, userContent)
		}
	}
	if len(userContent) > conservativeOpenAICompatRuntimeContextLimit+1200 {
		t.Fatalf("compact user too large: len=%d", len(userContent))
	}
}

func TestCompactOpenAICompatMessagesForToollessRetryCompactsTwoMessagePrompt(t *testing.T) {
	compact := CompactOpenAICompatMessagesForToollessRetry(
		corelib.MaclawLLMConfig{URL: "https://codegen.qianxin-inc.cn/api/v1", Model: "qax-codegen/Auto", ProviderName: "CodeGen"},
		[]interface{}{
			map[string]interface{}{"role": "system", "content": strings.Repeat("runtime context\n", 900)},
			map[string]interface{}{"role": "user", "content": "latest request"},
		},
	)
	if len(compact) != 2 {
		t.Fatalf("compact len = %d, want 2: %#v", len(compact), compact)
	}
	user := compact[1].(map[string]interface{})
	if got := user["content"].(string); !strings.Contains(got, "latest request") || strings.Contains(got, "runtime context\nruntime context") {
		t.Fatalf("compact user content = %q", got)
	}
}

func TestCompactOpenAICompatMessagesForToollessRetryHandlesTypedMessages(t *testing.T) {
	type typedMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	compact := CompactOpenAICompatMessagesForToollessRetry(
		corelib.MaclawLLMConfig{URL: "https://codegen.qianxin-inc.cn/api/v1", Model: "qax-codegen/Auto", ProviderName: "CodeGen"},
		[]interface{}{
			typedMessage{Role: "system", Content: strings.Repeat("runtime context\n", 900)},
			typedMessage{Role: "user", Content: "latest typed request"},
		},
	)
	if len(compact) != 2 {
		t.Fatalf("compact len = %d, want 2: %#v", len(compact), compact)
	}
	user := compact[1].(map[string]interface{})
	if got := user["content"].(string); !strings.Contains(got, "latest typed request") || strings.Contains(got, "runtime context\nruntime context") {
		t.Fatalf("compact typed user content = %q", got)
	}
}

func TestCompactOpenAICompatMessagesForToollessRetryReturnsNilForStandardProvider(t *testing.T) {
	compact := CompactOpenAICompatMessagesForToollessRetry(
		corelib.MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "gpt-test"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
	)
	if compact != nil {
		t.Fatalf("compact = %#v, want nil", compact)
	}
}

func TestOpenAI_RequestBody_StripsReasoningContentForStandardSDKCompatibility(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "gpt-test"},
		[]interface{}{map[string]interface{}{"role": "assistant", "content": "previous", "reasoning_content": "keep"}},
		OpenAIChatRequestOptions{Stream: false},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	message := req["messages"].([]interface{})[0].(map[string]interface{})
	if _, ok := message["reasoning_content"]; ok {
		t.Fatalf("standard OpenAI SDK-compatible request leaked reasoning_content: %#v", message)
	}
}

func TestOpenAI_RequestBody_PreservesReasoningContentForDeepSeekThinking(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-chat"},
		[]interface{}{map[string]interface{}{"role": "assistant", "content": "previous", "reasoning_content": "keep"}},
		OpenAIChatRequestOptions{Stream: false},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	message := req["messages"].([]interface{})[0].(map[string]interface{})
	if got := message["reasoning_content"]; got != "keep" {
		t.Fatalf("reasoning_content = %#v, want keep", got)
	}
}

func TestOpenAI_RequestBody_KeepsMaclawOfficialHubInitialTools(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://hub.mypapers.top/api/llm/v1", Model: "auto", ProviderName: "MaClaw官方"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		OpenAIChatRequestOptions{
			Stream: true,
			Tools: []map[string]interface{}{{
				"type": "function",
				"function": map[string]interface{}{
					"name": "manage_skill",
					"parameters": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"args": map[string]interface{}{"type": "object"},
							"ids":  map[string]interface{}{"type": "array"},
						},
					},
				},
			}},
			ExtraBody: map[string]interface{}{
				"stream_options": map[string]interface{}{"include_usage": true},
			},
		},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	if _, ok := req["stream_options"]; ok {
		t.Fatalf("official hub request leaked stream_options: %#v", req)
	}
	if _, ok := req["tools"]; !ok {
		t.Fatalf("official hub initial request should keep OpenAI tools and rely on 400 fallback if needed: %#v", req)
	}
}

func TestOpenAI_RequestBody_KeepsMaclawOfficialHubToolsAfterToolInteraction(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://hub.mypapers.top/api/llm/v1", Model: "auto", ProviderName: "MaClaw官方"},
		[]interface{}{
			map[string]interface{}{"role": "assistant", "content": "", "tool_calls": []interface{}{
				map[string]interface{}{"id": "call_1", "type": "function", "function": map[string]interface{}{"name": "lookup", "arguments": "{}"}},
			}},
			map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": "ok"},
		},
		OpenAIChatRequestOptions{
			Stream: true,
			Tools: []map[string]interface{}{{
				"type": "function",
				"function": map[string]interface{}{
					"name":       "lookup",
					"parameters": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
				},
			}},
		},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	if _, ok := req["tools"]; !ok {
		t.Fatalf("official hub request with existing tool interaction should keep tools: %#v", req)
	}
}

func TestDoOpenAIRequest_RetriesQwenWithoutToolsOnBadRequest(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if attempts == 1 {
			if _, ok := body["tools"]; !ok {
				t.Fatalf("first attempt missing tools: %#v", body)
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"tools unsupported"}}`))
			return
		}
		if _, ok := body["tools"]; ok {
			t.Fatalf("retry leaked tools: %#v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "chatcmpl-retry",
			"choices": []map[string]interface{}{{
				"message": map[string]interface{}{"role": "assistant", "content": "ok"},
			}},
		})
	}))
	defer srv.Close()

	tools := []map[string]interface{}{{
		"type": "function",
		"function": map[string]interface{}{
			"name":       "read_file",
			"parameters": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
	}}
	resp, err := DoOpenAIRequest(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Model: "qwen-27b", Protocol: "openai"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		tools,
		srv.Client(),
	)
	if err != nil {
		t.Fatalf("DoOpenAIRequest returned error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if got := resp.Choices[0].Message.Content; got != "ok" {
		t.Fatalf("content = %q, want ok", got)
	}
}

func TestDoOpenAIRequest_DoesNotRetryWithoutToolsAfterToolHistory(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request"}}`))
	}))
	defer srv.Close()

	tools := []map[string]interface{}{{
		"type":     "function",
		"function": map[string]interface{}{"name": "read_file", "parameters": map[string]interface{}{"type": "object"}},
	}}
	_, err := DoOpenAIRequest(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Model: "qwen-27b", Protocol: "openai"},
		[]interface{}{
			map[string]interface{}{"role": "assistant", "tool_calls": []interface{}{map[string]interface{}{"id": "call_1"}}},
			map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": "result"},
		},
		tools,
		srv.Client(),
	)
	if err == nil {
		t.Fatal("expected HTTP 400 error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestShouldRetryOpenAIWithoutToolsIgnoresEmptyToolCalls(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{URL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-27b"}
	messages := []interface{}{
		map[string]interface{}{"role": "assistant", "content": "done", "tool_calls": []interface{}{}},
		map[string]interface{}{"role": "assistant", "content": "done", "function_call": map[string]interface{}{}},
	}
	tools := []map[string]interface{}{{"type": "function", "function": map[string]interface{}{"name": "read_file"}}}
	if !ShouldRetryOpenAIWithoutTools(cfg, http.StatusBadRequest, messages, tools) {
		t.Fatal("expected retry when only empty historical tool fields are present")
	}
}

func TestShouldRetryOpenAIWithoutToolsIgnoresTypedEmptyFunctionCall(t *testing.T) {
	type functionCall struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	}
	cfg := corelib.MaclawLLMConfig{URL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-27b"}
	messages := []interface{}{
		map[string]interface{}{"role": "assistant", "content": "done", "function_call": functionCall{}},
	}
	tools := []map[string]interface{}{{"type": "function", "function": map[string]interface{}{"name": "read_file"}}}
	if !ShouldRetryOpenAIWithoutTools(cfg, http.StatusBadRequest, messages, tools) {
		t.Fatal("expected retry when typed function_call is empty")
	}

	messages[0] = map[string]interface{}{"role": "assistant", "content": "", "function_call": functionCall{Name: "read_file", Arguments: "{}"}}
	if ShouldRetryOpenAIWithoutTools(cfg, http.StatusBadRequest, messages, tools) {
		t.Fatal("did not expect retry when typed function_call is populated")
	}
}

func TestShouldRetryOpenAIWithCompactAllowsToollessBadRequest(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{URL: "https://codegen.qianxin-inc.cn/api/v1", Model: "qax-codegen/Auto"}
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": strings.Repeat("runtime context\n", 900)},
		map[string]interface{}{"role": "user", "content": "latest request"},
	}
	if !ShouldRetryOpenAIWithCompact(cfg, http.StatusBadRequest, messages) {
		t.Fatal("expected compact retry for conservative OpenAI-compatible 400 without tools")
	}
	if ShouldRetryOpenAIWithCompact(corelib.MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "gpt-test"}, http.StatusBadRequest, messages) {
		t.Fatal("standard OpenAI provider should not use compact compatibility retry")
	}
}

func TestDoOpenAIRequest_CompactsQwenWithoutToolsOnBadRequest(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		messages := body["messages"].([]interface{})
		switch attempts {
		case 1:
			if len(messages) != 2 {
				t.Fatalf("first attempt messages len = %d, want original 2", len(messages))
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"context too large"}}`))
		case 2:
			if len(messages) != 2 {
				t.Fatalf("compact retry messages len = %d, want 2", len(messages))
			}
			user := messages[1].(map[string]interface{})
			content := user["content"].(string)
			if !strings.Contains(content, "latest request") || strings.Contains(content, "runtime context\nruntime context") {
				t.Fatalf("compact retry user content = %.200q", content)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
		default:
			t.Fatalf("unexpected attempt %d", attempts)
		}
	}))
	defer srv.Close()

	resp, err := DoOpenAIRequest(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Model: "qwen-27b", Protocol: "openai"},
		[]interface{}{
			map[string]interface{}{"role": "system", "content": strings.Repeat("runtime context\n", 900)},
			map[string]interface{}{"role": "user", "content": "latest request"},
		},
		nil,
		srv.Client(),
	)
	if err != nil {
		t.Fatalf("DoOpenAIRequest returned error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if got := resp.Choices[0].Message.Content; got != "ok" {
		t.Fatalf("content = %q, want ok", got)
	}
}

func TestDoOpenAIRequestStream_RetriesQwenWithoutToolsOnBadRequest(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if attempts == 1 {
			if _, ok := body["tools"]; !ok {
				t.Fatalf("first attempt missing tools: %#v", body)
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"tools unsupported"}}`))
			return
		}
		if _, ok := body["tools"]; ok {
			t.Fatalf("retry leaked tools: %#v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	resp, err := DoOpenAIRequestStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Model: "qwen-27b", Protocol: "openai"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		[]map[string]interface{}{{
			"type":     "function",
			"function": map[string]interface{}{"name": "read_file", "parameters": map[string]interface{}{"type": "object"}},
		}},
		srv.Client(),
		nil,
	)
	if err != nil {
		t.Fatalf("DoOpenAIRequestStream returned error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if got := resp.Choices[0].Message.Content; got != "ok" {
		t.Fatalf("content = %q, want ok", got)
	}
}

func TestDoOpenAIRequestStream_CompactsQwenWithoutToolsOnBadRequest(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		messages := body["messages"].([]interface{})
		switch attempts {
		case 1:
			if len(messages) != 2 {
				t.Fatalf("first attempt messages len = %d, want original 2", len(messages))
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"context too large"}}`))
		case 2:
			if len(messages) != 2 {
				t.Fatalf("compact retry messages len = %d, want 2", len(messages))
			}
			user := messages[1].(map[string]interface{})
			content := user["content"].(string)
			if !strings.Contains(content, "latest request") || strings.Contains(content, "runtime context\nruntime context") {
				t.Fatalf("compact retry user content = %.200q", content)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n"))
		default:
			t.Fatalf("unexpected attempt %d", attempts)
		}
	}))
	defer srv.Close()

	resp, err := DoOpenAIRequestStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Model: "qwen-27b", Protocol: "openai"},
		[]interface{}{
			map[string]interface{}{"role": "system", "content": strings.Repeat("runtime context\n", 900)},
			map[string]interface{}{"role": "user", "content": "latest request"},
		},
		nil,
		srv.Client(),
		nil,
	)
	if err != nil {
		t.Fatalf("DoOpenAIRequestStream returned error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if got := resp.Choices[0].Message.Content; got != "ok" {
		t.Fatalf("content = %q, want ok", got)
	}
}

func TestDoOpenAIRequestStream_CompactsQwenAfterToollessBadRequest(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		messages := body["messages"].([]interface{})
		switch attempts {
		case 1:
			if _, ok := body["tools"]; !ok {
				t.Fatalf("first attempt missing tools: %#v", body)
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"tools unsupported"}}`))
		case 2:
			if _, ok := body["tools"]; ok {
				t.Fatalf("toolless retry leaked tools: %#v", body)
			}
			if len(messages) != 3 {
				t.Fatalf("toolless retry messages len = %d, want original 3", len(messages))
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"context too large"}}`))
		case 3:
			if _, ok := body["tools"]; ok {
				t.Fatalf("compact retry leaked tools: %#v", body)
			}
			if len(messages) != 2 {
				t.Fatalf("compact retry messages len = %d, want 2", len(messages))
			}
			user := messages[1].(map[string]interface{})
			content := user["content"].(string)
			if !strings.Contains(content, "latest request") || strings.Contains(content, "older request") || strings.Contains(content, "runtime context\nruntime context") {
				t.Fatalf("compact retry user content = %.200q", content)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n"))
		default:
			t.Fatalf("unexpected attempt %d", attempts)
		}
	}))
	defer srv.Close()

	resp, err := DoOpenAIRequestStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Model: "qwen-27b", Protocol: "openai"},
		[]interface{}{
			map[string]interface{}{"role": "system", "content": strings.Repeat("runtime context\n", 900)},
			map[string]interface{}{"role": "user", "content": "older request"},
			map[string]interface{}{"role": "user", "content": "latest request"},
		},
		[]map[string]interface{}{{
			"type":     "function",
			"function": map[string]interface{}{"name": "read_file", "parameters": map[string]interface{}{"type": "object"}},
		}},
		srv.Client(),
		nil,
	)
	if err != nil {
		t.Fatalf("DoOpenAIRequestStream returned error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if got := resp.Choices[0].Message.Content; got != "ok" {
		t.Fatalf("content = %q, want ok", got)
	}
}

func TestDoOpenAIRequestStream_JSONFallbackFromSDKResponse(t *testing.T) {
	var capturedBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %q, want /v1/chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("User-Agent"); got != "opencode" {
			t.Fatalf("User-Agent = %q, want opencode", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"plain json","reasoning_content":"plain thought"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`))
	}))
	defer srv.Close()

	var streamed string
	var streamedReasoning string
	resp, err := DoOpenAIRequestStreamWithReasoning(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Model: "deepseek-v4-flash", Protocol: "openai", AgentType: "opencode"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		nil,
		srv.Client(),
		func(token string) { streamed += token },
		func(token string) { streamedReasoning += token },
	)
	if err != nil {
		t.Fatalf("DoOpenAIRequestStream returned error: %v", err)
	}
	if got := capturedBody["stream"]; got != true {
		t.Fatalf("stream = %#v, want true", got)
	}
	if got := resp.Choices[0].Message.Content; got != "plain json" {
		t.Fatalf("content = %q, want plain json", got)
	}
	if got := resp.Choices[0].Message.ReasoningContent; got != "plain thought" {
		t.Fatalf("reasoning_content = %q, want plain thought", got)
	}
	if streamed != "plain json" {
		t.Fatalf("streamed = %q, want plain json", streamed)
	}
	if streamedReasoning != "plain thought" {
		t.Fatalf("streamedReasoning = %q, want plain thought", streamedReasoning)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %#v, want total 5", resp.Usage)
	}
}

func TestDoOpenAIRequestStream_SDKPreservesToolCallsAndReasoning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"choices":[{"delta":{"reasoning_content":"think "}}]}`,
			`data: {"choices":[{"delta":{"content":"done"}}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":""}}]}}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\""}}]}}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"x\"}"}}]},"finish_reason":"tool_calls"}]}`,
			`data: {"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":6,"total_tokens":10}}`,
			`data: [DONE]`,
			"",
		}, "\n")))
	}))
	defer srv.Close()

	resp, err := DoOpenAIRequestStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Model: "glm-5.1", Protocol: "openai", AgentType: "Kilo Code"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		nil,
		srv.Client(),
		nil,
	)
	if err != nil {
		t.Fatalf("DoOpenAIRequestStream returned error: %v", err)
	}
	choice := resp.Choices[0]
	if got := choice.Message.ReasoningContent; got != "think " {
		t.Fatalf("reasoning_content = %q, want think ", got)
	}
	if got := choice.FinishReason; got != "tool_calls" {
		t.Fatalf("finish_reason = %q, want tool_calls", got)
	}
	if len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("tool_calls len = %d, want 1", len(choice.Message.ToolCalls))
	}
	tc := choice.Message.ToolCalls[0]
	if tc.ID != "call_1" || tc.Function.Name != "lookup" || tc.Function.Arguments != `{"q":"x"}` {
		t.Fatalf("tool_call = %#v", tc)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 10 {
		t.Fatalf("usage = %#v, want total 10", resp.Usage)
	}
}

func TestDoOpenAIRequestStreamWithReasoningStreamsReasoningDeltas(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"choices":[{"delta":{"reasoning_content":"Inspect "}}]}`,
			`data: {"choices":[{"delta":{"reasoning_content":"the request."}}]}`,
			`data: {"choices":[{"delta":{"content":"Final answer."},"finish_reason":"stop"}]}`,
			`data: [DONE]`,
			"",
		}, "\n")))
	}))
	defer srv.Close()

	var textDeltas, reasoningDeltas []string
	resp, err := DoOpenAIRequestStreamWithReasoning(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Model: "deepseek-v4-flash", Protocol: "openai"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		nil,
		srv.Client(),
		func(delta string) { textDeltas = append(textDeltas, delta) },
		func(delta string) { reasoningDeltas = append(reasoningDeltas, delta) },
	)
	if err != nil {
		t.Fatalf("DoOpenAIRequestStreamWithReasoning returned error: %v", err)
	}
	if got, want := strings.Join(reasoningDeltas, ""), "Inspect the request."; got != want {
		t.Fatalf("reasoning deltas = %q, want %q", got, want)
	}
	if got, want := len(reasoningDeltas), 2; got != want {
		t.Fatalf("reasoning delta count = %d, want %d", got, want)
	}
	if got, want := strings.Join(textDeltas, ""), "Final answer."; got != want {
		t.Fatalf("text deltas = %q, want %q", got, want)
	}
	if got, want := resp.Choices[0].Message.ReasoningContent, "Inspect the request."; got != want {
		t.Fatalf("reasoning_content = %q, want %q", got, want)
	}
}

func TestDoOpenAIRequestStream_SDKDetectsTruncatedToolCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"choices":[{"delta":{"content":"I'll write the script now:"}}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_bad","type":"function","function":{"name":"write_file","arguments":"{\"path\":\"a.txt\",\"content\":\"unterminated"}}]}}]}`,
			`data: {"choices":[{"delta":{},"finish_reason":"length"}]}`,
			`data: [DONE]`,
			"",
		}, "\n")))
	}))
	defer srv.Close()

	resp, err := DoOpenAIRequestStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Model: "glm-5.1", Protocol: "openai", AgentType: "Kilo Code"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "write a script"}},
		nil,
		srv.Client(),
		nil,
	)
	if err != nil {
		t.Fatalf("DoOpenAIRequestStream returned error: %v", err)
	}
	choice := resp.Choices[0]
	if len(choice.TruncatedToolNames) != 1 || choice.TruncatedToolNames[0] != "write_file" {
		t.Fatalf("TruncatedToolNames = %#v, want write_file", choice.TruncatedToolNames)
	}
	if len(choice.Message.ToolCalls) != 0 {
		t.Fatalf("truncated tool calls should be removed: %#v", choice.Message.ToolCalls)
	}
	if choice.FinishReason != "length" {
		t.Fatalf("finish_reason = %q, want length", choice.FinishReason)
	}
	if choice.Message.Content != "I'll write the script now:" {
		t.Fatalf("content = %q, want preamble preserved", choice.Message.Content)
	}
}

func TestDoOpenAIRequestStream_SDKConvertsLegacyFunctionCallDeltas(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"choices":[{"delta":{"function_call":{"name":"bash","arguments":""}}}]}`,
			`data: {"choices":[{"delta":{"function_call":{"arguments":"{\"command\""}}}]}`,
			`data: {"choices":[{"delta":{"function_call":{"arguments":":\"dir\"}"}},"finish_reason":"function_call"}]}`,
			`data: [DONE]`,
			"",
		}, "\n")))
	}))
	defer srv.Close()

	var streamed strings.Builder
	resp, err := DoOpenAIRequestStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Model: "legacy-openai-compatible", Protocol: "openai", AgentType: "opencode"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "use bash"}},
		nil,
		srv.Client(),
		func(token string) { streamed.WriteString(token) },
	)
	if err != nil {
		t.Fatalf("DoOpenAIRequestStream returned error: %v", err)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices len = %d, want 1", len(resp.Choices))
	}
	choice := resp.Choices[0]
	if got := choice.FinishReason; got != "tool_calls" {
		t.Fatalf("finish_reason = %q, want tool_calls", got)
	}
	if got := choice.Message.Content; got != "" {
		t.Fatalf("content = %q, want empty", got)
	}
	if got := streamed.String(); got != "" {
		t.Fatalf("streamed = %q, want empty", got)
	}
	if len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("tool_calls len = %d, want 1", len(choice.Message.ToolCalls))
	}
	tc := choice.Message.ToolCalls[0]
	if tc.Type != "function" || tc.Function.Name != "bash" || tc.Function.Arguments != `{"command":"dir"}` {
		t.Fatalf("tool_call = %#v", tc)
	}
}

func TestDoOpenAIRequestStream_SDKDefaultsMissingToolCallType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"lookup","arguments":""}}]}}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":\"x\"}"}}]},"finish_reason":"tool_calls"}]}`,
			`data: [DONE]`,
			"",
		}, "\n")))
	}))
	defer srv.Close()

	resp, err := DoOpenAIRequestStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Model: "missing-type-compatible", Protocol: "openai", AgentType: "opencode"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "use lookup"}},
		nil,
		srv.Client(),
		nil,
	)
	if err != nil {
		t.Fatalf("DoOpenAIRequestStream returned error: %v", err)
	}
	if len(resp.Choices) != 1 || len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("response = %#v", resp)
	}
	if got := resp.Choices[0].Message.ToolCalls[0].Type; got != "function" {
		t.Fatalf("tool_call type = %q, want function", got)
	}
}

func TestOpenAISDKBaseURLNormalizesProviderEndpoints(t *testing.T) {
	tests := []struct {
		name string
		cfg  corelib.MaclawLLMConfig
		want string
	}{
		{
			name: "bare host appends v1",
			cfg:  corelib.MaclawLLMConfig{URL: "https://api.deepseek.com", AgentType: "opencode"},
			want: "https://api.deepseek.com/v1",
		},
		{
			name: "v1 unchanged",
			cfg:  corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1", AgentType: "opencode"},
			want: "https://api.deepseek.com/v1",
		},
		{
			name: "full chat endpoint trims",
			cfg:  corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1/chat/completions", AgentType: "opencode"},
			want: "https://api.deepseek.com/v1",
		},
		{
			name: "glm coding plan rewrite",
			cfg:  corelib.MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/paas/v4", AgentType: "Kilo Code"},
			want: "https://open.bigmodel.cn/api/coding/paas/v4",
		},
		{
			name: "glm coding full chat trims",
			cfg:  corelib.MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions", AgentType: "Kilo Code"},
			want: "https://open.bigmodel.cn/api/coding/paas/v4",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := openAISDKBaseURL(tt.cfg); got != tt.want {
				t.Fatalf("openAISDKBaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSummarizeOpenAIChatRequestBodyOmitsPromptContent(t *testing.T) {
	body := []byte(`{"model":"gpt-test","stream":true,"messages":[{"role":"user","content":"SECRET_PROMPT"}],"tools":[{"type":"function"}],"stream_options":{"include_usage":true},"tool_choice":"auto","response_format":{"type":"json_object"}}`)
	got := SummarizeOpenAIChatRequestBody(body)
	for _, want := range []string{
		`model="gpt-test"`,
		"stream=true",
		"messages=1",
		"tools=1",
		"stream_options=true",
		"tool_choice=true",
		"response_format=true",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "SECRET_PROMPT") || strings.Contains(got, "content") {
		t.Fatalf("summary leaked prompt content: %q", got)
	}
}

func TestProviderRequestBodies_NormalizeCodeGenAutoModel(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{URL: "https://codegen.qianxin-inc.cn/api/v1", Model: "auto"}
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}

	_, body, err := BuildResponsesAPIRequestData(cfg, messages, ResponsesAPIRequestOptions{})
	if err != nil {
		t.Fatalf("BuildResponsesAPIRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse responses request body: %v", err)
	}
	if got := req["model"]; got != corelib.CodeGenDefaultModelID {
		t.Fatalf("Responses CodeGen model = %#v, want %q", got, corelib.CodeGenDefaultModelID)
	}

	_, body, err = BuildResponsesAPIRequestData(cfg, messages, ResponsesAPIRequestOptions{
		Tools: []map[string]interface{}{{
			"type": "function",
			"function": map[string]interface{}{
				"name":   "strict_tool",
				"strict": true,
				"parameters": map[string]interface{}{
					"additionalProperties": false,
					"properties": map[string]interface{}{
						"values": map[string]interface{}{"type": "array"},
					},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("BuildResponsesAPIRequestData returned error: %v", err)
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse responses request body: %v", err)
	}
	tools := req["tools"].([]interface{})
	flatTool := tools[0].(map[string]interface{})
	if _, ok := flatTool["strict"]; ok {
		t.Fatalf("Responses CodeGen strict leaked: %#v", flatTool)
	}
	params := flatTool["parameters"].(map[string]interface{})
	if _, ok := params["additionalProperties"]; ok {
		t.Fatalf("Responses CodeGen additionalProperties leaked: %#v", params)
	}
	properties := params["properties"].(map[string]interface{})
	for _, bad := range []string{"type", "properties"} {
		if _, ok := properties[bad]; ok {
			t.Fatalf("Responses CodeGen properties container was treated as schema and leaked %q: %#v", bad, properties)
		}
	}
	values := properties["values"].(map[string]interface{})
	if got := values["items"].(map[string]interface{})["type"]; got != "string" {
		t.Fatalf("Responses CodeGen array items type = %#v, want string", got)
	}

	req = BuildAnthropicMessagesRequestBody(cfg, messages, AnthropicMessagesRequestOptions{})
	if got := req["model"]; got != corelib.CodeGenDefaultModelID {
		t.Fatalf("Anthropic CodeGen model = %#v, want %q", got, corelib.CodeGenDefaultModelID)
	}
}

func TestResponsesAPIRequestData_NormalizesMissingToolCallLinkage(t *testing.T) {
	_, body, err := BuildResponsesAPIRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "gpt-test"},
		[]interface{}{
			map[string]interface{}{
				"role":    "assistant",
				"content": "",
				"tool_calls": []map[string]interface{}{{
					"function": map[string]interface{}{
						"name":      "search",
						"arguments": map[string]interface{}{"q": "golang"},
					},
				}},
			},
			map[string]interface{}{"role": "tool", "tool_call_id": "", "content": "ok"},
		},
		ResponsesAPIRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildResponsesAPIRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse responses request body: %v", err)
	}
	input := req["input"].([]interface{})
	if len(input) != 2 {
		t.Fatalf("input len = %d, want 2: %#v", len(input), input)
	}
	call := input[0].(map[string]interface{})
	output := input[1].(map[string]interface{})
	callID, _ := call["call_id"].(string)
	if call["type"] != "function_call" || callID == "" {
		t.Fatalf("function_call not normalized: %#v", call)
	}
	if output["type"] != "function_call_output" || output["call_id"] != callID {
		t.Fatalf("function_call_output not linked to %q: %#v", callID, output)
	}
	if call["arguments"] != `{"q":"golang"}` {
		t.Fatalf("arguments = %#v, want JSON string", call["arguments"])
	}
}

func TestResponsesAPIRequestData_PreservesImageInput(t *testing.T) {
	_, body, err := BuildResponsesAPIRequestData(corelib.MaclawLLMConfig{
		URL: "https://api.example.com/v1", Model: "vision-model",
	}, []interface{}{map[string]interface{}{
		"role": "user",
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": "describe the image"},
			map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64,abc", "detail": "high"}},
		},
	}}, ResponsesAPIRequestOptions{})
	if err != nil {
		t.Fatalf("BuildResponsesAPIRequestData returned error: %v", err)
	}

	var request map[string]interface{}
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	input := request["input"].([]interface{})
	content := input[0].(map[string]interface{})["content"].([]interface{})
	image := content[1].(map[string]interface{})
	if image["type"] != "input_image" || image["image_url"] != "data:image/png;base64,abc" || image["detail"] != "high" {
		t.Fatalf("image content = %#v, want preserved input_image", image)
	}
}

func TestResponsesAPIRequestData_PreservesResponsesFormatMultimodalInput(t *testing.T) {
	_, body, err := BuildResponsesAPIRequestData(corelib.MaclawLLMConfig{
		URL: "https://api.example.com/v1", Model: "vision-model",
	}, []interface{}{map[string]interface{}{
		"role": "user",
		"content": []interface{}{
			map[string]interface{}{"type": "input_text", "content": "describe these inputs"},
			map[string]interface{}{"type": "input_image", "image_url": "data:image/png;base64,abc", "detail": "high"},
			map[string]interface{}{"type": "input_file", "file_id": "file_abc"},
			map[string]interface{}{"type": "input_audio", "input_audio": map[string]interface{}{"data": "AAAA", "format": "wav"}},
		},
	}}, ResponsesAPIRequestOptions{})
	if err != nil {
		t.Fatalf("BuildResponsesAPIRequestData returned error: %v", err)
	}

	var request map[string]interface{}
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	content := request["input"].([]interface{})[0].(map[string]interface{})["content"].([]interface{})
	if len(content) != 4 {
		t.Fatalf("len(content) = %d, want 4: %#v", len(content), content)
	}
	if part := content[1].(map[string]interface{}); part["type"] != "input_image" || part["image_url"] != "data:image/png;base64,abc" || part["detail"] != "high" {
		t.Fatalf("image input = %#v", part)
	}
	if part := content[2].(map[string]interface{}); part["type"] != "input_file" || part["file_id"] != "file_abc" {
		t.Fatalf("file input = %#v", part)
	}
	if part := content[3].(map[string]interface{}); part["type"] != "input_audio" {
		t.Fatalf("audio input = %#v", part)
	}
}

func TestAnthropicMessagesRequestBody_NormalizesMissingToolCallLinkage(t *testing.T) {
	req := BuildAnthropicMessagesRequestBody(
		corelib.MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/anthropic", Model: "glm-5.1"},
		[]interface{}{
			map[string]interface{}{
				"role": "assistant",
				"tool_calls": []map[string]interface{}{{
					"function": map[string]interface{}{
						"name":      "search",
						"arguments": map[string]interface{}{"q": "golang"},
					},
				}},
			},
			map[string]interface{}{"role": "tool", "tool_call_id": "", "content": map[string]interface{}{"ok": true}},
		},
		AnthropicMessagesRequestOptions{MaxTokens: 8},
	)
	messages := req["messages"].([]interface{})
	if len(messages) != 2 {
		t.Fatalf("messages len = %d, want 2: %#v", len(messages), messages)
	}
	assistant := messages[0].(map[string]interface{})
	assistantBlocks := assistant["content"].([]interface{})
	toolUse := assistantBlocks[0].(map[string]interface{})
	toolUseID, _ := toolUse["id"].(string)
	if toolUse["type"] != "tool_use" || toolUseID == "" || toolUse["name"] != "search" {
		t.Fatalf("tool_use not normalized: %#v", toolUse)
	}
	user := messages[1].(map[string]interface{})
	userBlocks := user["content"].([]interface{})
	toolResult := userBlocks[0].(map[string]interface{})
	if toolResult["type"] != "tool_result" || toolResult["tool_use_id"] != toolUseID {
		t.Fatalf("tool_result not linked to %q: %#v", toolUseID, toolResult)
	}
	if toolResult["content"] != `{"ok":true}` {
		t.Fatalf("tool_result content = %#v, want JSON string", toolResult["content"])
	}
}

func TestResponsesAPIRequestData_DropsOrphanedToolHistory(t *testing.T) {
	_, body, err := BuildResponsesAPIRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "gpt-test"},
		[]interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
			map[string]interface{}{
				"role":    "assistant",
				"content": "",
				"tool_calls": []interface{}{
					map[string]interface{}{"id": "call_1", "type": "function", "function": map[string]interface{}{"name": "a", "arguments": `{}`}},
					map[string]interface{}{"id": "call_2", "type": "function", "function": map[string]interface{}{"name": "b", "arguments": `{}`}},
				},
			},
			map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": "partial"},
			map[string]interface{}{"role": "user", "content": "next"},
		},
		ResponsesAPIRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildResponsesAPIRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse responses request body: %v", err)
	}
	for i, item := range req["input"].([]interface{}) {
		m := item.(map[string]interface{})
		if typ, _ := m["type"].(string); typ == "function_call" || typ == "function_call_output" {
			t.Fatalf("input item %d leaked orphaned tool history: %#v", i, m)
		}
	}
}

func TestAnthropicMessagesRequestBody_DropsOrphanedToolHistory(t *testing.T) {
	req := BuildAnthropicMessagesRequestBody(
		corelib.MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/anthropic", Model: "glm-5.1"},
		[]interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
			map[string]interface{}{
				"role":    "assistant",
				"content": "",
				"tool_calls": []interface{}{
					map[string]interface{}{"id": "call_1", "type": "function", "function": map[string]interface{}{"name": "a", "arguments": `{}`}},
					map[string]interface{}{"id": "call_2", "type": "function", "function": map[string]interface{}{"name": "b", "arguments": `{}`}},
				},
			},
			map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": "partial"},
			map[string]interface{}{"role": "user", "content": "next"},
		},
		AnthropicMessagesRequestOptions{MaxTokens: 8},
	)
	for i, item := range req["messages"].([]interface{}) {
		msg := item.(map[string]interface{})
		blocks, _ := msg["content"].([]interface{})
		for j, block := range blocks {
			m, _ := block.(map[string]interface{})
			if typ, _ := m["type"].(string); typ == "tool_use" || typ == "tool_result" {
				t.Fatalf("message %d block %d leaked orphaned tool history: %#v", i, j, m)
			}
		}
	}
}

func TestResponsesAPIRequestData_SanitizesQwenOpenAICompatProvider(t *testing.T) {
	_, body, err := BuildResponsesAPIRequestData(
		corelib.MaclawLLMConfig{URL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-27b", ProviderName: "Qwen"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		ResponsesAPIRequestOptions{
			Tools: []map[string]interface{}{{
				"type": "function",
				"function": map[string]interface{}{
					"name":   "strict_tool",
					"strict": true,
					"parameters": map[string]interface{}{
						"additionalProperties": false,
						"properties": map[string]interface{}{
							"values": map[string]interface{}{"type": "array"},
						},
					},
				},
			}},
			ExtraBody: map[string]interface{}{
				"metadata":            map[string]interface{}{"trace": "x"},
				"parallel_tool_calls": true,
				"tool_choice":         "auto",
				"function_call":       "auto",
				"logprobs":            true,
				"top_logprobs":        2,
				"response_format":     map[string]interface{}{"type": "json_schema"},
				"store":               false,
				"stream_options":      map[string]interface{}{"include_usage": true},
				"service_tier":        "auto",
				"reasoning_effort":    "low",
				"modalities":          []interface{}{"text"},
				"prediction":          map[string]interface{}{"type": "content", "content": "OK"},
				"audio":               map[string]interface{}{"voice": "alloy"},
				"web_search_options":  map[string]interface{}{"search_context_size": "low"},
			},
		},
	)
	if err != nil {
		t.Fatalf("BuildResponsesAPIRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse responses request body: %v", err)
	}
	for _, key := range []string{"stream_options", "parallel_tool_calls", "store", "metadata", "response_format", "tool_choice", "function_call", "logprobs", "top_logprobs", "service_tier", "reasoning_effort", "modalities", "prediction", "audio", "web_search_options"} {
		if _, ok := req[key]; ok {
			t.Fatalf("Qwen Responses request leaked %s: %#v", key, req)
		}
	}
	tool := req["tools"].([]interface{})[0].(map[string]interface{})
	if _, ok := tool["strict"]; ok {
		t.Fatalf("Qwen Responses strict leaked: %#v", tool)
	}
	params := tool["parameters"].(map[string]interface{})
	if _, ok := params["additionalProperties"]; ok {
		t.Fatalf("Qwen Responses additionalProperties leaked: %#v", params)
	}
	properties := params["properties"].(map[string]interface{})
	for _, bad := range []string{"type", "properties"} {
		if _, ok := properties[bad]; ok {
			t.Fatalf("Qwen Responses properties container was treated as schema and leaked %q: %#v", bad, properties)
		}
	}
	values := properties["values"].(map[string]interface{})
	if got := values["items"].(map[string]interface{})["type"]; got != "string" {
		t.Fatalf("array items type = %#v, want string", got)
	}
}

func TestResponsesAPIRequestData_PreservesStrictToolSchemaForNonCodeGenProvider(t *testing.T) {
	_, body, err := BuildResponsesAPIRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "gpt-test"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		ResponsesAPIRequestOptions{
			Tools: []map[string]interface{}{{
				"type":  "function",
				"extra": "drop-me",
				"function": map[string]interface{}{
					"name":   "strict_tool",
					"extra":  "drop-me",
					"strict": true,
					"parameters": map[string]interface{}{
						"type":                 "object",
						"additionalProperties": false,
						"properties": map[string]interface{}{
							"value": map[string]interface{}{"type": "string"},
						},
					},
				},
			}},
		},
	)
	if err != nil {
		t.Fatalf("BuildResponsesAPIRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse responses request body: %v", err)
	}
	tool := req["tools"].([]interface{})[0].(map[string]interface{})
	if got := tool["strict"]; got != true {
		t.Fatalf("Responses strict = %#v, want true", got)
	}
	if _, ok := tool["extra"]; ok {
		t.Fatalf("Responses tool leaked non-SDK field: %#v", tool)
	}
	params := tool["parameters"].(map[string]interface{})
	if got := params["additionalProperties"]; got != false {
		t.Fatalf("additionalProperties = %#v, want false", got)
	}
}

func TestResponsesAPIRequestData_DropsStreamOptionsForNonCodeGenProvider(t *testing.T) {
	_, body, err := BuildResponsesAPIRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "gpt-test"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		ResponsesAPIRequestOptions{
			Stream: true,
			ExtraBody: map[string]interface{}{
				"stream_options":      map[string]interface{}{"include_usage": true},
				"function_call":       "auto",
				"parallel_tool_calls": true,
				"tool_choice":         "auto",
				"logprobs":            true,
				"top_logprobs":        2,
				"response_format":     map[string]interface{}{"type": "json_object"},
				"service_tier":        "auto",
				"reasoning_effort":    "low",
				"modalities":          []interface{}{"text"},
				"prediction":          map[string]interface{}{"type": "content", "content": "OK"},
				"audio":               map[string]interface{}{"voice": "alloy"},
				"web_search_options":  map[string]interface{}{"search_context_size": "low"},
				"metadata":            map[string]interface{}{"trace": "keep"},
				"store":               true,
				"reasoning":           map[string]interface{}{"effort": "low"},
			},
		},
	)
	if err != nil {
		t.Fatalf("BuildResponsesAPIRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse responses request body: %v", err)
	}
	for _, key := range []string{"stream_options", "function_call", "parallel_tool_calls", "tool_choice", "logprobs", "top_logprobs", "response_format", "service_tier", "reasoning_effort", "modalities", "prediction", "audio", "web_search_options"} {
		if _, ok := req[key]; ok {
			t.Fatalf("Responses request leaked %s: %#v", key, req)
		}
	}
	text := req["text"].(map[string]interface{})
	format := text["format"].(map[string]interface{})
	if got := format["type"]; got != "json_object" {
		t.Fatalf("Responses text.format.type = %#v, want json_object", got)
	}
	if _, ok := req["metadata"]; !ok {
		t.Fatalf("Responses request should preserve metadata: %#v", req)
	}
	if got := req["store"]; got != true {
		t.Fatalf("Responses request should preserve store=true, got %#v", got)
	}
	if _, ok := req["reasoning"]; !ok {
		t.Fatalf("Responses request should preserve reasoning: %#v", req)
	}
}

func TestResponsesAPIRequestData_MapsJSONSchemaResponseFormatToTextFormat(t *testing.T) {
	_, body, err := BuildResponsesAPIRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "gpt-test"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		ResponsesAPIRequestOptions{
			ExtraBody: map[string]interface{}{
				"response_format": map[string]interface{}{
					"type": "json_schema",
					"json_schema": map[string]interface{}{
						"name":   "answer",
						"schema": map[string]interface{}{"type": "object"},
						"strict": true,
					},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("BuildResponsesAPIRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse responses request body: %v", err)
	}
	if _, ok := req["response_format"]; ok {
		t.Fatalf("Responses request leaked response_format: %#v", req)
	}
	text := req["text"].(map[string]interface{})
	format := text["format"].(map[string]interface{})
	if format["type"] != "json_schema" || format["name"] != "answer" || format["strict"] != true {
		t.Fatalf("Responses text.format = %#v", format)
	}
	if _, ok := format["json_schema"]; ok {
		t.Fatalf("Responses text.format leaked nested json_schema: %#v", format)
	}
}

func TestResponsesAPIRequestData_MapsChatTokenLimitForNonCodeGenProvider(t *testing.T) {
	_, body, err := BuildResponsesAPIRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "gpt-test"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		ResponsesAPIRequestOptions{
			ExtraBody: map[string]interface{}{
				"max_tokens":            111,
				"max_completion_tokens": 222,
			},
		},
	)
	if err != nil {
		t.Fatalf("BuildResponsesAPIRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse responses request body: %v", err)
	}
	if got := req["max_output_tokens"]; got != float64(222) {
		t.Fatalf("max_output_tokens = %#v, want 222", got)
	}
	if _, ok := req["max_tokens"]; ok {
		t.Fatalf("Responses request leaked max_tokens: %#v", req)
	}
	if _, ok := req["max_completion_tokens"]; ok {
		t.Fatalf("Responses request leaked max_completion_tokens: %#v", req)
	}
}

func TestResponsesAPIRequestData_DropsQwenOrphanedToolHistory(t *testing.T) {
	_, body, err := BuildResponsesAPIRequestData(
		corelib.MaclawLLMConfig{URL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-27b", ProviderName: "Qwen"},
		[]interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
			map[string]interface{}{
				"role":    "assistant",
				"content": "",
				"tool_calls": []interface{}{
					map[string]interface{}{"id": "call_1", "type": "function", "function": map[string]interface{}{"name": "a", "arguments": "{"}},
					map[string]interface{}{"id": "call_2", "type": "function", "function": map[string]interface{}{"name": "b", "arguments": `{}`}},
				},
			},
			map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": "partial"},
			map[string]interface{}{"role": "user", "content": "next"},
		},
		ResponsesAPIRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildResponsesAPIRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse responses request body: %v", err)
	}
	for i, item := range req["input"].([]interface{}) {
		m, _ := item.(map[string]interface{})
		if typ, _ := m["type"].(string); typ == "function_call" || typ == "function_call_output" {
			t.Fatalf("input item %d leaked orphaned tool history: %#v", i, m)
		}
	}
}

func TestResponsesAPIRequestData_SanitizesInvalidToolCallArgumentsForNonCodeGenProvider(t *testing.T) {
	_, body, err := BuildResponsesAPIRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "gpt-test"},
		[]interface{}{
			map[string]interface{}{
				"role":    "assistant",
				"content": "",
				"tool_calls": []interface{}{
					map[string]interface{}{"id": "call_1", "type": "function", "function": map[string]interface{}{"name": "search", "arguments": "{"}},
				},
			},
			map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": "ok"},
		},
		ResponsesAPIRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildResponsesAPIRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse responses request body: %v", err)
	}
	input := req["input"].([]interface{})
	call := input[0].(map[string]interface{})
	if call["type"] != "function_call" {
		t.Fatalf("first input type = %#v, want function_call", call["type"])
	}
	if got := call["arguments"]; got != "{}" {
		t.Fatalf("function_call arguments = %#v, want {}", got)
	}
}

func TestResponsesAPIRequestData_StringifiesToolArgumentObjects(t *testing.T) {
	_, body, err := BuildResponsesAPIRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "gpt-test"},
		[]interface{}{
			map[string]interface{}{
				"role":    "assistant",
				"content": "",
				"tool_calls": []interface{}{
					map[string]interface{}{
						"id":   "call_1",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "search",
							"arguments": map[string]interface{}{"q": "golang", "limit": float64(3)},
						},
					},
				},
			},
			map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": "ok"},
		},
		ResponsesAPIRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildResponsesAPIRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse responses request body: %v", err)
	}
	input := req["input"].([]interface{})
	call := input[0].(map[string]interface{})
	if call["type"] != "function_call" {
		t.Fatalf("first input type = %#v, want function_call", call["type"])
	}
	if got := call["arguments"]; got != `{"limit":3,"q":"golang"}` {
		t.Fatalf("function_call arguments = %#v, want object encoded as JSON string", got)
	}
}

func TestOpenAI_RequestBody_AddsMissingArrayItemsInToolSchema(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://codegen.qianxin-inc.cn/api/v1", Model: "test-model"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		OpenAIChatRequestOptions{
			Tools: []map[string]interface{}{{
				"type": "function",
				"function": map[string]interface{}{
					"name":        "manage_skill",
					"description": "Manage skills",
					"parameters": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"action":           map[string]interface{}{"type": "string"},
							"approved_actions": map[string]interface{}{"type": "array"},
						},
						"required": []string{"action"},
					},
				},
			}},
		},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	tools, _ := req["tools"].([]interface{})
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(tools))
	}
	toolDef, _ := tools[0].(map[string]interface{})
	fn, _ := toolDef["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	approved, _ := props["approved_actions"].(map[string]interface{})
	items, _ := approved["items"].(map[string]interface{})
	if got := items["type"]; got != "string" {
		t.Fatalf("approved_actions.items.type = %#v, want string", got)
	}
}

func TestOpenAI_RequestBody_StripsProviderIncompatibleToolSchemaFields(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://codegen.qianxin-inc.cn/api/v1", Model: "test-model"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		OpenAIChatRequestOptions{
			Tools: []map[string]interface{}{{
				"type": "function",
				"x_execution_contract": map[string]interface{}{
					"risk": "local-only",
				},
				"function": map[string]interface{}{
					"name":        "strict_tool",
					"description": "Strict local schema",
					"strict":      true,
					"extra":       "drop me",
					"parameters": map[string]interface{}{
						"additionalProperties": false,
						"properties": map[string]interface{}{
							"mode": map[string]interface{}{
								"oneOf": []interface{}{
									map[string]interface{}{"type": "string"},
								},
								"nullable": true,
								"type":     "string",
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
			}},
		},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	toolDef := req["tools"].([]interface{})[0].(map[string]interface{})
	if _, ok := toolDef["x_execution_contract"]; ok {
		t.Fatalf("x_execution_contract leaked into OpenAI tool definition: %#v", toolDef)
	}
	fn := toolDef["function"].(map[string]interface{})
	if _, ok := fn["strict"]; ok {
		t.Fatalf("strict should be stripped for CodeGen tool definition: %#v", fn)
	}
	if _, ok := fn["extra"]; ok {
		t.Fatalf("function extra field leaked into OpenAI tool definition: %#v", fn)
	}
	params := fn["parameters"].(map[string]interface{})
	if got := params["type"]; got != "object" {
		t.Fatalf("parameters.type = %#v, want object", got)
	}
	if _, ok := params["additionalProperties"]; ok {
		t.Fatalf("additionalProperties=false should be stripped: %#v", params)
	}
	props := params["properties"].(map[string]interface{})
	mode := props["mode"].(map[string]interface{})
	for _, bad := range []string{"oneOf", "nullable"} {
		if _, ok := mode[bad]; ok {
			t.Fatalf("unsupported schema key %q should be stripped: %#v", bad, mode)
		}
	}
	metadata := props["metadata"].(map[string]interface{})
	if _, ok := metadata["additionalProperties"]; ok {
		t.Fatalf("additionalProperties schema should be stripped: %#v", metadata)
	}
	args := props["args"].(map[string]interface{})
	if got := args["properties"]; got == nil {
		t.Fatalf("object schema without properties should be completed: %#v", args)
	}
}

func TestOpenAI_RequestBody_PreservesStrictToolSchemaForNonCodeGenProvider(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "test-model"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		OpenAIChatRequestOptions{
			Tools: []map[string]interface{}{{
				"type":  "function",
				"extra": "drop-me",
				"function": map[string]interface{}{
					"name":   "strict_tool",
					"extra":  "drop-me",
					"strict": true,
					"parameters": map[string]interface{}{
						"type":                 "object",
						"additionalProperties": false,
						"properties": map[string]interface{}{
							"mode": map[string]interface{}{
								"type":    "string",
								"default": "fast",
							},
						},
					},
				},
			}},
		},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	toolDef := req["tools"].([]interface{})[0].(map[string]interface{})
	if _, ok := toolDef["extra"]; ok {
		t.Fatalf("tool extra field leaked into OpenAI tool definition: %#v", toolDef)
	}
	fn := toolDef["function"].(map[string]interface{})
	if got := fn["strict"]; got != true {
		t.Fatalf("strict = %#v, want true", got)
	}
	if _, ok := fn["extra"]; ok {
		t.Fatalf("function extra field leaked into OpenAI tool definition: %#v", fn)
	}
	params := fn["parameters"].(map[string]interface{})
	if got := params["additionalProperties"]; got != false {
		t.Fatalf("additionalProperties = %#v, want false", got)
	}
	mode := params["properties"].(map[string]interface{})["mode"].(map[string]interface{})
	if got := mode["default"]; got != "fast" {
		t.Fatalf("default = %#v, want fast", got)
	}
}

func TestOpenAI_RequestBody_CompletesToolParameterSchemaShape(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		OpenAIChatRequestOptions{
			Tools: []map[string]interface{}{{
				"type": "function",
				"function": map[string]interface{}{
					"name": "search",
					"parameters": map[string]interface{}{
						"required": "filters",
						"properties": map[string]interface{}{
							"filters": map[string]interface{}{
								"required": []interface{}{"q", "q", 123, ""},
								"properties": map[string]interface{}{
									"q": map[string]interface{}{"type": "string"},
								},
							},
							"ids": map[string]interface{}{"type": "array"},
						},
					},
				},
			}},
		},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	params := req["tools"].([]interface{})[0].(map[string]interface{})["function"].(map[string]interface{})["parameters"].(map[string]interface{})
	if got := params["type"]; got != "object" {
		t.Fatalf("parameters.type = %#v, want object", got)
	}
	required := params["required"].([]interface{})
	if len(required) != 1 || required[0] != "filters" {
		t.Fatalf("parameters.required = %#v, want [filters]", required)
	}
	props := params["properties"].(map[string]interface{})
	filters := props["filters"].(map[string]interface{})
	if got := filters["type"]; got != "object" {
		t.Fatalf("nested object type = %#v, want object", got)
	}
	if got := filters["properties"]; got == nil {
		t.Fatalf("nested object properties missing: %#v", filters)
	}
	filterRequired := filters["required"].([]interface{})
	if len(filterRequired) != 1 || filterRequired[0] != "q" {
		t.Fatalf("nested required = %#v, want [q]", filterRequired)
	}
	ids := props["ids"].(map[string]interface{})
	if got := ids["items"].(map[string]interface{})["type"]; got != "string" {
		t.Fatalf("array items type = %#v, want string", got)
	}
}

func TestOpenAI_RequestBody_CompletesTypedToolParameterSchemaShape(t *testing.T) {
	type arraySchema struct {
		Type string `json:"type,omitempty"`
	}
	type objectSchema struct {
		Type       string                 `json:"type,omitempty"`
		Properties map[string]interface{} `json:"properties,omitempty"`
	}

	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		OpenAIChatRequestOptions{
			Tools: []map[string]interface{}{{
				"type": "function",
				"function": map[string]interface{}{
					"name": "search",
					"parameters": objectSchema{
						Properties: map[string]interface{}{
							"filters": objectSchema{},
							"ids":     arraySchema{Type: "array"},
						},
					},
				},
			}},
		},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	params := req["tools"].([]interface{})[0].(map[string]interface{})["function"].(map[string]interface{})["parameters"].(map[string]interface{})
	if got := params["type"]; got != "object" {
		t.Fatalf("typed parameters.type = %#v, want object", got)
	}
	props := params["properties"].(map[string]interface{})
	filters := props["filters"].(map[string]interface{})
	if got := filters["type"]; got != "object" {
		t.Fatalf("typed nested object type = %#v, want object", got)
	}
	if got := filters["properties"]; got == nil {
		t.Fatalf("typed nested object properties missing: %#v", filters)
	}
	ids := props["ids"].(map[string]interface{})
	if got := ids["items"].(map[string]interface{})["type"]; got != "string" {
		t.Fatalf("typed array items type = %#v, want string", got)
	}
}

func TestParseSSEToResponse_RejectsOversizedToolArguments(t *testing.T) {
	oversized := strings.Repeat("a", maxToolArgumentsBytes+1)
	body := []byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"generate_pdf\",\"arguments\":\"" + oversized + "\"}}]}}]}\n")

	_, err := ParseSSEToResponse(body)
	if err == nil || !strings.Contains(err.Error(), "tool arguments too large") {
		t.Fatalf("expected oversized tool arguments error, got %v", err)
	}
}

func TestBuildOpenAIChatRequestData_PassesThroughCommonChatOptions(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{URL: "https://example.com/v1", Model: "gpt-test"}
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}

	_, body, err := BuildOpenAIChatRequestData(cfg, messages, OpenAIChatRequestOptions{
		Stream: false,
		Tools: []map[string]interface{}{{
			"type": "function",
			"function": map[string]interface{}{
				"name":       "search",
				"parameters": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
			},
		}},
		ToolChoice: map[string]interface{}{
			"type":     "function",
			"function": map[string]interface{}{"name": "search"},
		},
		ResponseFormat: map[string]interface{}{
			"type": "json_schema",
			"json_schema": map[string]interface{}{
				"name":   "answer",
				"schema": map[string]interface{}{"type": "object"},
			},
		},
		PassThrough: map[string]interface{}{
			"temperature":           0.2,
			"top_p":                 0.9,
			"max_tokens":            123,
			"max_completion_tokens": 321,
			"presence_penalty":      0.1,
			"frequency_penalty":     0.3,
			"stop":                  []interface{}{"END"},
			"parallel_tool_calls":   true,
			"user":                  "u-1",
			"seed":                  float64(7),
			"n":                     float64(2),
			"service_tier":          "auto",
			"reasoning_effort":      "low",
			"modalities":            []interface{}{"text"},
			"prediction":            map[string]interface{}{"type": "content", "content": "OK"},
			"audio":                 map[string]interface{}{"voice": "alloy", "format": "mp3"},
			"web_search_options":    map[string]interface{}{"search_context_size": "low"},
		},
	})
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	for _, key := range []string{"temperature", "top_p", "max_tokens", "max_completion_tokens", "presence_penalty", "frequency_penalty", "stop", "parallel_tool_calls", "user", "seed", "n", "service_tier", "reasoning_effort", "modalities", "prediction", "audio", "web_search_options", "tool_choice", "response_format"} {
		if _, ok := req[key]; !ok {
			t.Fatalf("expected key %q in request body", key)
		}
	}
}

func TestBuildOpenAIChatRequestData_NormalizesTypedToolChoice(t *testing.T) {
	type toolChoiceFunction struct {
		Name string `json:"name"`
	}
	type toolChoice struct {
		Type     string             `json:"type"`
		Function toolChoiceFunction `json:"function"`
	}

	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://example.com/v1", Model: "gpt-test"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		OpenAIChatRequestOptions{
			Tools: []map[string]interface{}{{
				"type": "function",
				"function": map[string]interface{}{
					"name":       "search",
					"parameters": map[string]interface{}{"type": "object"},
				},
			}},
			ToolChoice: toolChoice{Type: "function", Function: toolChoiceFunction{Name: "search"}},
		},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	choice := req["tool_choice"].(map[string]interface{})
	if choice["type"] != "function" {
		t.Fatalf("tool_choice.type = %#v, want function", choice["type"])
	}
	fn := choice["function"].(map[string]interface{})
	if fn["name"] != "search" {
		t.Fatalf("tool_choice.function.name = %#v, want search", fn["name"])
	}
}

func TestBuildOpenAIChatRequestData_DeepSeekFlashDowngradesTypedToolChoice(t *testing.T) {
	type toolChoiceFunction struct {
		Name string `json:"name"`
	}
	type toolChoice struct {
		Type     string             `json:"type"`
		Function toolChoiceFunction `json:"function"`
	}

	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		OpenAIChatRequestOptions{
			Tools: []map[string]interface{}{{
				"type": "function",
				"function": map[string]interface{}{
					"name":       "search",
					"parameters": map[string]interface{}{"type": "object"},
				},
			}},
			ToolChoice: toolChoice{Type: "function", Function: toolChoiceFunction{Name: "search"}},
		},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	if got := req["tool_choice"]; got != "auto" {
		t.Fatalf("DeepSeek flash tool_choice = %#v, want auto", got)
	}
}

func TestBuildOpenAIChatRequestData_NormalizesSDKTopLevelOptions(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{URL: "https://example.com/v1", Model: "gpt-test"}
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}
	tools := []map[string]interface{}{{
		"type": "function",
		"function": map[string]interface{}{
			"name":       "search",
			"parameters": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
	}}

	_, body, err := BuildOpenAIChatRequestData(cfg, messages, OpenAIChatRequestOptions{
		Stream: false,
		Tools:  tools,
		ToolChoice: map[string]interface{}{
			"type":     "function",
			"function": map[string]interface{}{"name": "search", "extra": "drop-me"},
			"extra":    "drop-me",
		},
		ExtraBody: map[string]interface{}{
			"stream_options": map[string]interface{}{"include_usage": true},
		},
	})
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	if _, ok := req["stream_options"]; ok {
		t.Fatalf("non-stream request leaked stream_options: %#v", req)
	}
	toolChoice := req["tool_choice"].(map[string]interface{})
	if _, ok := toolChoice["extra"]; ok {
		t.Fatalf("tool_choice leaked non-SDK field: %#v", toolChoice)
	}
	fn := toolChoice["function"].(map[string]interface{})
	if got := fn["name"]; got != "search" {
		t.Fatalf("tool_choice function name = %#v, want search", got)
	}
	if _, ok := fn["extra"]; ok {
		t.Fatalf("tool_choice function leaked non-SDK field: %#v", fn)
	}

	_, body, err = BuildOpenAIChatRequestData(cfg, messages, OpenAIChatRequestOptions{
		Stream:     true,
		ToolChoice: "auto",
		ExtraBody: map[string]interface{}{
			"stream_options": map[string]interface{}{"include_usage": true, "extra": "drop-me"},
		},
	})
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	req = map[string]interface{}{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	if _, ok := req["tool_choice"]; ok {
		t.Fatalf("request without tools leaked tool_choice: %#v", req)
	}
	streamOptions := req["stream_options"].(map[string]interface{})
	if got := streamOptions["include_usage"]; got != true {
		t.Fatalf("stream_options.include_usage = %#v, want true", got)
	}
	if _, ok := streamOptions["extra"]; ok {
		t.Fatalf("stream_options leaked non-SDK field: %#v", streamOptions)
	}
}

func TestBuildOpenAIChatRequestData_DowngradesDeepSeekFlashForcedToolChoice(t *testing.T) {
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}
	tools := []map[string]interface{}{{
		"type": "function",
		"function": map[string]interface{}{
			"name":       "search",
			"parameters": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
	}}

	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"},
		messages,
		OpenAIChatRequestOptions{
			Tools:      tools,
			ToolChoice: map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "search"}},
		},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	if got := req["tool_choice"]; got != "auto" {
		t.Fatalf("DeepSeek flash tool_choice = %#v, want auto", got)
	}

	_, body, err = BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"},
		messages,
		OpenAIChatRequestOptions{Tools: tools, ToolChoice: "required"},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	req = map[string]interface{}{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	if got := req["tool_choice"]; got != "auto" {
		t.Fatalf("DeepSeek flash required tool_choice = %#v, want auto", got)
	}
}

func TestBuildOpenAIChatRequestData_DowngradesDeepSeekFlashN(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		OpenAIChatRequestOptions{PassThrough: map[string]interface{}{"n": 2}},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	if got := req["n"]; got != float64(1) {
		t.Fatalf("DeepSeek flash n = %#v, want 1", got)
	}

	_, body, err = BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "gpt-test"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		OpenAIChatRequestOptions{PassThrough: map[string]interface{}{"n": 2}},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	req = map[string]interface{}{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	if got := req["n"]; got != float64(2) {
		t.Fatalf("OpenAI n = %#v, want 2", got)
	}
}

func TestBuildOpenAIChatRequestData_DowngradesDeepSeekFlashJSONSchemaResponseFormat(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		OpenAIChatRequestOptions{
			ResponseFormat: map[string]interface{}{
				"type": "json_schema",
				"json_schema": map[string]interface{}{
					"name":   "answer",
					"schema": map[string]interface{}{"type": "object"},
					"strict": true,
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	responseFormat := req["response_format"].(map[string]interface{})
	if got := responseFormat["type"]; got != "json_object" {
		t.Fatalf("DeepSeek flash response_format.type = %#v, want json_object", got)
	}
	if _, ok := responseFormat["json_schema"]; ok {
		t.Fatalf("DeepSeek flash response_format leaked json_schema: %#v", responseFormat)
	}
}

func TestBuildOpenAIChatRequestData_DowngradesIncompleteDeepSeekFlashJSONSchemaResponseFormat(t *testing.T) {
	type responseFormat struct {
		Type       string         `json:"type"`
		JSONSchema map[string]any `json:"json_schema,omitempty"`
	}

	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "Return ok true."}},
		OpenAIChatRequestOptions{
			ResponseFormat: responseFormat{
				Type:       "json_schema",
				JSONSchema: map[string]any{"schema": map[string]any{"type": "object"}},
			},
		},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	responseFormatBody := req["response_format"].(map[string]interface{})
	if got := responseFormatBody["type"]; got != "json_object" {
		t.Fatalf("DeepSeek flash response_format.type = %#v, want json_object", got)
	}
	messages := req["messages"].([]interface{})
	first := messages[0].(map[string]interface{})
	if got := first["content"].(string); !strings.Contains(strings.ToLower(got), "json") {
		t.Fatalf("JSON response instruction missing: %#v", got)
	}
}

func TestBuildOpenAIChatRequestData_AddsDeepSeekFlashJSONResponseInstruction(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "Return ok true."}},
		OpenAIChatRequestOptions{ResponseFormat: map[string]interface{}{"type": "json_object"}},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	messages := req["messages"].([]interface{})
	first := messages[0].(map[string]interface{})
	if got := first["role"]; got != "system" {
		t.Fatalf("first role = %#v, want system", got)
	}
	if got := first["content"].(string); !strings.Contains(strings.ToLower(got), "json") {
		t.Fatalf("JSON response instruction missing: %#v", got)
	}

	_, body, err = BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "Return JSON object with ok true."}},
		OpenAIChatRequestOptions{ResponseFormat: map[string]interface{}{"type": "json_object"}},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	req = map[string]interface{}{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	messages = req["messages"].([]interface{})
	if len(messages) != 1 {
		t.Fatalf("messages len = %d, want 1 when user already mentions JSON: %#v", len(messages), messages)
	}
}

func TestBuildOpenAIChatRequestData_ConvertsDeepSeekFlashDeveloperMessages(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"},
		[]interface{}{
			map[string]interface{}{"role": "developer", "content": "Answer OK only."},
			map[string]interface{}{"role": "user", "content": "hi"},
		},
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	messages := req["messages"].([]interface{})
	first := messages[0].(map[string]interface{})
	if got := first["role"]; got != "system" {
		t.Fatalf("DeepSeek flash developer role = %#v, want system", got)
	}

	_, body, err = BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "gpt-test"},
		[]interface{}{
			map[string]interface{}{"role": "developer", "content": "Answer OK only."},
			map[string]interface{}{"role": "user", "content": "hi"},
		},
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	req = map[string]interface{}{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	messages = req["messages"].([]interface{})
	first = messages[0].(map[string]interface{})
	if got := first["role"]; got != "developer" {
		t.Fatalf("OpenAI developer role = %#v, want developer", got)
	}
}

func TestBuildOpenAIChatRequestData_NormalizesNullUserContent(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"},
		[]interface{}{map[string]interface{}{"role": "user", "content": nil}},
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	message := req["messages"].([]interface{})[0].(map[string]interface{})
	if got := message["content"]; got != "" {
		t.Fatalf("null user content = %#v, want empty string", got)
	}
}

func TestBuildOpenAIChatRequestData_EnsuresNonEmptyMessages(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"},
		nil,
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	messages := req["messages"].([]interface{})
	if len(messages) != 1 {
		t.Fatalf("messages len = %d, want 1: %#v", len(messages), messages)
	}
	message := messages[0].(map[string]interface{})
	if got := message["role"]; got != "user" {
		t.Fatalf("fallback message role = %#v, want user", got)
	}
	if got := message["content"]; got != "" {
		t.Fatalf("fallback message content = %#v, want empty string", got)
	}
}

func TestEnsureDeepSeekFlashJSONResponseInstructionHandlesTypedMessageSlice(t *testing.T) {
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	reqBody := map[string]interface{}{
		"response_format": map[string]interface{}{"type": "json_object"},
		"messages": []message{
			{Role: "user", Content: "Return ok true."},
		},
	}

	ensureDeepSeekFlashJSONResponseInstruction(reqBody)

	messages := reqBody["messages"].([]interface{})
	first := messages[0].(map[string]interface{})
	if first["role"] != "system" {
		t.Fatalf("first role = %#v, want system", first["role"])
	}
	if got := first["content"].(string); !strings.Contains(strings.ToLower(got), "json") {
		t.Fatalf("JSON instruction missing: %#v", got)
	}
}

func TestBuildOpenAIChatRequestData_TextualizesDeepSeekFlashContentBlocks(t *testing.T) {
	messages := []interface{}{map[string]interface{}{
		"role": "user",
		"content": []interface{}{
			map[string]interface{}{"type": "input_text", "text": "Say "},
			map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64,iVBORw0KGgo="}},
			map[string]interface{}{"type": "file", "file": map[string]interface{}{"file_id": "file-test"}},
			map[string]interface{}{"type": "input_audio", "input_audio": map[string]interface{}{"data": "AAAA", "format": "wav"}},
			map[string]interface{}{"type": "output_text", "content": "OK only."},
		},
	}}

	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"},
		messages,
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	message := req["messages"].([]interface{})[0].(map[string]interface{})
	if got := message["content"]; got != "Say OK only." {
		t.Fatalf("DeepSeek flash content = %#v, want text-only content", got)
	}

	_, body, err = BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "gpt-test"},
		messages,
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	req = map[string]interface{}{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	message = req["messages"].([]interface{})[0].(map[string]interface{})
	blocks := message["content"].([]interface{})
	if len(blocks) != 5 {
		t.Fatalf("OpenAI content blocks len = %d, want 5: %#v", len(blocks), blocks)
	}
	if got := blocks[1].(map[string]interface{})["type"]; got != "image_url" {
		t.Fatalf("OpenAI second content block type = %#v, want image_url", got)
	}
}

func TestBuildOpenAIChatRequestData_TextualizesTypedContentBlocks(t *testing.T) {
	type contentBlock struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	}
	type message struct {
		Role    string         `json:"role"`
		Content []contentBlock `json:"content"`
	}
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"},
		[]interface{}{message{Role: "user", Content: []contentBlock{
			{Type: "input_text", Text: "typed "},
			{Type: "image_url"},
			{Type: "output_text", Text: "text"},
		}}},
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	msg := req["messages"].([]interface{})[0].(map[string]interface{})
	if got := msg["content"]; got != "typed text" {
		t.Fatalf("typed content blocks = %#v, want text-only content", got)
	}
}

func TestBuildOpenAIChatRequestData_UsesPlaceholderForDeepSeekFlashMediaOnlyUserContent(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"},
		[]interface{}{map[string]interface{}{
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64,iVBORw0KGgo="}},
			},
		}},
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	message := req["messages"].([]interface{})[0].(map[string]interface{})
	if got := message["content"]; got != openAICompatUnsupportedNonTextContentPlaceholder {
		t.Fatalf("media-only user content = %#v, want placeholder", got)
	}
}

func TestBuildOpenAIChatRequestData_TextualizesGLMCodingPlanContentBlocks(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-5.1", AgentType: "Kilo Code"},
		[]interface{}{map[string]interface{}{
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "Say "},
				map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64,iVBORw0KGgo="}},
				map[string]interface{}{"type": "text", "text": "OK only."},
			},
		}},
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	message := req["messages"].([]interface{})[0].(map[string]interface{})
	if got := message["content"]; got != "Say OK only." {
		t.Fatalf("GLM coding content = %#v, want text-only content", got)
	}
}

func TestBuildOpenAIChatRequestData_DropsGLMCodingPlanOrphanedToolHistory(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-5.1", AgentType: "Kilo Code"},
		[]interface{}{
			map[string]interface{}{"role": "tool", "tool_call_id": "call_00_test", "content": "ok"},
		},
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	messages := req["messages"].([]interface{})
	if len(messages) != 1 {
		t.Fatalf("messages len = %d, want fallback user message: %#v", len(messages), messages)
	}
	message := messages[0].(map[string]interface{})
	if got := message["role"]; got != "user" {
		t.Fatalf("fallback role = %#v, want user", got)
	}
	if got := message["content"]; got != glmCodingPlanEmptyUserContentPlaceholder {
		t.Fatalf("fallback content = %#v, want GLM placeholder", got)
	}
}

func TestBuildOpenAIChatRequestData_NormalizesGLMCodingPlanEmptyUserContent(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-5.1", AgentType: "Kilo Code"},
		[]interface{}{map[string]interface{}{"role": "user", "content": nil}},
		OpenAIChatRequestOptions{},
	)
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	message := req["messages"].([]interface{})[0].(map[string]interface{})
	if got := message["content"]; got != glmCodingPlanEmptyUserContentPlaceholder {
		t.Fatalf("GLM null user content = %#v, want placeholder", got)
	}
}

func TestParseNonStreamOpenAIResponseBody_ContentParts(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":[{"type":"text","text":"hello"},{"type":"image_url","image_url":{"url":"https://example.com/x.png"}},{"type":"text","text":"world"}]},"finish_reason":"stop"}]}`)

	resp, err := ParseNonStreamOpenAIResponseBody(body)
	if err != nil {
		t.Fatalf("ParseNonStreamOpenAIResponseBody returned error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}
	msg := resp.Choices[0].Message
	if got := msg.Content; got != "hello\nworld" {
		t.Fatalf("content = %q, want %q", got, "hello\nworld")
	}
	parts, ok := msg.RawContent.([]interface{})
	if !ok || len(parts) != 3 {
		t.Fatalf("raw content = %#v, want original 3-part content array", msg.RawContent)
	}
}

// TestPreservation_StandardJSONResponse verifies that DoOpenAIRequest correctly
// parses a standard JSON response with content and finish_reason.
//
// **Validates: Requirements 3.1**
func TestPreservation_StandardJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{
		URL:   srv.URL,
		Model: "test-model",
	}
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}

	resp, err := DoOpenAIRequest(context.Background(), cfg, messages, nil, srv.Client())
	if err != nil {
		t.Fatalf("DoOpenAIRequest returned error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}
	if got := resp.Choices[0].Message.Content; got != "Hello" {
		t.Errorf("content = %q, want %q", got, "Hello")
	}
	if got := resp.Choices[0].FinishReason; got != "stop" {
		t.Errorf("finish_reason = %q, want %q", got, "stop")
	}
}

// TestPreservation_HTTPErrorResponse500 verifies that DoOpenAIRequest returns
// an error containing "HTTP 500" when the server responds with status 500.
//
// **Validates: Requirements 3.2**
func TestPreservation_HTTPErrorResponse500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{
		URL:   srv.URL,
		Model: "test-model",
	}
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}

	_, err := DoOpenAIRequest(context.Background(), cfg, messages, nil, srv.Client())
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "HTTP 500")
	}
}

// TestPreservation_ToolCallJSONResponse verifies that DoOpenAIRequest correctly
// parses a JSON response containing tool_calls.
//
// **Validates: Requirements 3.3**
func TestPreservation_ToolCallJSONResponse(t *testing.T) {
	toolCallResp := `{
		"choices":[{
			"message":{
				"role":"assistant",
				"content":"",
				"tool_calls":[{
					"id":"call_123",
					"type":"function",
					"function":{
						"name":"get_weather",
						"arguments":"{\"location\":\"Beijing\"}"
					}
				}]
			},
			"finish_reason":"tool_calls"
		}]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(toolCallResp))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{
		URL:   srv.URL,
		Model: "test-model",
	}
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "What is the weather?"},
	}

	resp, err := DoOpenAIRequest(context.Background(), cfg, messages, nil, srv.Client())
	if err != nil {
		t.Fatalf("DoOpenAIRequest returned error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}
	choice := resp.Choices[0]
	if got := choice.FinishReason; got != "tool_calls" {
		t.Errorf("finish_reason = %q, want %q", got, "tool_calls")
	}
	if len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(choice.Message.ToolCalls))
	}
	tc := choice.Message.ToolCalls[0]
	if tc.ID != "call_123" {
		t.Errorf("tool call ID = %q, want %q", tc.ID, "call_123")
	}
	if tc.Type != "function" {
		t.Errorf("tool call type = %q, want %q", tc.Type, "function")
	}
	if tc.Function.Name != "get_weather" {
		t.Errorf("tool call function name = %q, want %q", tc.Function.Name, "get_weather")
	}
	if tc.Function.Arguments != `{"location":"Beijing"}` {
		t.Errorf("tool call arguments = %q, want %q", tc.Function.Arguments, `{"location":"Beijing"}`)
	}
}

// TestPreservation_HTTPErrorResponse400 verifies that DoOpenAIRequest returns
// an error containing "HTTP 400" when the server responds with status 400.
//
// **Validates: Requirements 3.2**
func TestPreservation_HTTPErrorResponse400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"bad request"}}`))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{
		URL:   srv.URL,
		Model: "test-model",
	}
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}

	_, err := DoOpenAIRequest(context.Background(), cfg, messages, nil, srv.Client())
	if err == nil {
		t.Fatal("expected error for HTTP 400, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 400") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "HTTP 400")
	}
	if strings.Contains(err.Error(), "bad request") {
		t.Fatalf("error leaked response body: %q", err.Error())
	}
	var httpErr *HTTPStatusError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error type = %T, want HTTPStatusError", err)
	}
	if httpErr.StatusCode != http.StatusBadRequest || !strings.Contains(string(httpErr.Body), "bad request") {
		t.Fatalf("HTTPStatusError = status %d body %q", httpErr.StatusCode, string(httpErr.Body))
	}
}

// ---------------------------------------------------------------------------
// Task 4 — Unit tests for ParseSSEToResponse edge cases
// ---------------------------------------------------------------------------

// TestParseSSE_SingleChunkTextContent verifies that ParseSSEToResponse with a
// single data line returns a Response with the correct content.
//
// **Validates: Requirements 2.2**
func TestParseSSE_SingleChunkTextContent(t *testing.T) {
	body := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\ndata: [DONE]\n")

	resp, err := ParseSSEToResponse(body)
	if err != nil {
		t.Fatalf("ParseSSEToResponse returned error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}
	if got := resp.Choices[0].Message.Content; got != "Hello" {
		t.Errorf("content = %q, want %q", got, "Hello")
	}
	if got := resp.Choices[0].Message.Role; got != "assistant" {
		t.Errorf("role = %q, want %q", got, "assistant")
	}
}

// TestParseSSE_MultiChunkAccumulation verifies that ParseSSEToResponse with
// multiple data lines concatenates all content deltas.
//
// **Validates: Requirements 2.2**
func TestParseSSE_MultiChunkAccumulation(t *testing.T) {
	body := []byte(strings.Join([]string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"one\"}}]}",
		"data: {\"choices\":[{\"delta\":{\"content\":\" two\"}}]}",
		"data: {\"choices\":[{\"delta\":{\"content\":\" three\"}}]}",
		"data: [DONE]",
		"",
	}, "\n"))

	resp, err := ParseSSEToResponse(body)
	if err != nil {
		t.Fatalf("ParseSSEToResponse returned error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}
	if got := resp.Choices[0].Message.Content; got != "one two three" {
		t.Errorf("content = %q, want %q", got, "one two three")
	}
}

// TestParseSSE_ToolCallDeltas verifies that ParseSSEToResponse correctly
// assembles tool calls from incremental SSE deltas with index, id, name,
// and argument fragments.
//
// **Validates: Requirements 2.2**
func TestParseSSE_ToolCallDeltas(t *testing.T) {
	body := []byte(strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":""}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"loc"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ation\":"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"NYC\"}"}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		"data: [DONE]",
		"",
	}, "\n"))

	resp, err := ParseSSEToResponse(body)
	if err != nil {
		t.Fatalf("ParseSSEToResponse returned error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}
	msg := resp.Choices[0].Message
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "call_abc" {
		t.Errorf("tool call ID = %q, want %q", tc.ID, "call_abc")
	}
	if tc.Type != "function" {
		t.Errorf("tool call type = %q, want %q", tc.Type, "function")
	}
	if tc.Function.Name != "get_weather" {
		t.Errorf("tool call name = %q, want %q", tc.Function.Name, "get_weather")
	}
	wantArgs := `{"location":"NYC"}`
	if tc.Function.Arguments != wantArgs {
		t.Errorf("tool call arguments = %q, want %q", tc.Function.Arguments, wantArgs)
	}
	if got := resp.Choices[0].FinishReason; got != "tool_calls" {
		t.Errorf("finish_reason = %q, want %q", got, "tool_calls")
	}
}

func TestParseSSE_ToolCallDeltasDefaultMissingType(t *testing.T) {
	body := []byte(strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_abc","function":{"name":"get_weather","arguments":"{\"location\":\"NYC\"}"}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		"data: [DONE]",
		"",
	}, "\n"))

	resp, err := ParseSSEToResponse(body)
	if err != nil {
		t.Fatalf("ParseSSEToResponse returned error: %v", err)
	}
	if len(resp.Choices) != 1 || len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("response = %#v", resp)
	}
	if got := resp.Choices[0].Message.ToolCalls[0].Type; got != "function" {
		t.Fatalf("tool_call type = %q, want function", got)
	}
}

func TestParseSSE_LegacyFunctionCallDeltas(t *testing.T) {
	body := []byte(strings.Join([]string{
		`data: {"choices":[{"delta":{"function_call":{"name":"bash","arguments":""}}}]}`,
		`data: {"choices":[{"delta":{"function_call":{"arguments":"{\"command\""}}}]}`,
		`data: {"choices":[{"delta":{"function_call":{"arguments":":\"dir\"}"}},"finish_reason":"function_call"}]}`,
		"data: [DONE]",
		"",
	}, "\n"))

	resp, err := ParseSSEToResponse(body)
	if err != nil {
		t.Fatalf("ParseSSEToResponse returned error: %v", err)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices len = %d, want 1", len(resp.Choices))
	}
	if got := resp.Choices[0].FinishReason; got != "tool_calls" {
		t.Fatalf("finish_reason = %q, want tool_calls", got)
	}
	msg := resp.Choices[0].Message
	if got := msg.Content; got != "" {
		t.Fatalf("content = %q, want empty", got)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("tool_calls len = %d, want 1", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if got := tc.Type; got != "function" {
		t.Fatalf("tool type = %q, want function", got)
	}
	if got := tc.Function.Name; got != "bash" {
		t.Fatalf("tool name = %q, want bash", got)
	}
	if got := tc.Function.Arguments; got != `{"command":"dir"}` {
		t.Fatalf("tool arguments = %q, want %q", got, `{"command":"dir"}`)
	}
}

// TestParseSSE_ReasoningContent verifies that ParseSSEToResponse accumulates
// reasoning_content deltas from SSE chunks.
//
// **Validates: Requirements 2.2**
func TestParseSSE_ReasoningContent(t *testing.T) {
	body := []byte(strings.Join([]string{
		`data: {"choices":[{"delta":{"reasoning_content":"Let me "}}]}`,
		`data: {"choices":[{"delta":{"reasoning_content":"think..."}}]}`,
		`data: {"choices":[{"delta":{"content":"The answer is 42."}}]}`,
		"data: [DONE]",
		"",
	}, "\n"))

	var streamed strings.Builder
	resp, err := parseSSEStream(strings.NewReader(string(body)), func(delta string) {
		streamed.WriteString(delta)
	})
	if err != nil {
		t.Fatalf("ParseSSEToResponse returned error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}
	msg := resp.Choices[0].Message
	if got := msg.ReasoningContent; got != "Let me think..." {
		t.Errorf("reasoning_content = %q, want %q", got, "Let me think...")
	}
	if got := msg.Content; got != "The answer is 42." {
		t.Errorf("content = %q, want %q", got, "The answer is 42.")
	}
	if got := streamed.String(); got != "The answer is 42." {
		t.Errorf("streamed tokens = %q, want content only", got)
	}
}

// TestParseSSE_EmptyMalformedLines verifies that ParseSSEToResponse gracefully
// skips blank lines, lines without "data:" prefix, and malformed JSON, while
// still parsing valid chunks.
//
// **Validates: Requirements 2.2**
func TestParseSSE_EmptyMalformedLines(t *testing.T) {
	body := []byte(strings.Join([]string{
		"",
		"event: message",
		"data: {\"choices\":[{\"delta\":{\"content\":\"A\"}}]}",
		"",
		": this is a comment",
		"data: NOT-VALID-JSON",
		"data: {\"choices\":[{\"delta\":{\"content\":\"B\"}}]}",
		"random garbage line",
		"data: [DONE]",
		"",
	}, "\n"))

	resp, err := ParseSSEToResponse(body)
	if err != nil {
		t.Fatalf("ParseSSEToResponse returned error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}
	if got := resp.Choices[0].Message.Content; got != "AB" {
		t.Errorf("content = %q, want %q", got, "AB")
	}
}

// TestParseSSE_StripAllExtra verifies that ParseSSEToResponse applies
// StripAllExtra to the accumulated content. The current StripThinkTags regex
// matches <think>...</think> followed by a literal backslash (due to \\s* in
// the raw string pattern). We use that pattern to confirm StripAllExtra is
// invoked on the accumulated SSE content.
//
// **Validates: Requirements 2.2**
func TestParseSSE_StripAllExtra(t *testing.T) {
	// The regex in filters.go uses `\\s*` in a raw string, which matches a
	// literal backslash after </think>. We craft content that matches this
	// pattern to prove StripAllExtra is called on the accumulated result.
	body := []byte(strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"<think>internal reasoning</think>\\"}}]}`,
		`data: {"choices":[{"delta":{"content":"visible answer"}}]}`,
		"data: [DONE]",
		"",
	}, "\n"))

	resp, err := ParseSSEToResponse(body)
	if err != nil {
		t.Fatalf("ParseSSEToResponse returned error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}
	got := resp.Choices[0].Message.Content
	if strings.Contains(got, "<think>") {
		t.Errorf("content still contains <think> tags: %q", got)
	}
	if got != "visible answer" {
		t.Errorf("content = %q, want %q", got, "visible answer")
	}
}

// TestParseSSE_SSEDetectionViaBodyPrefix verifies that DoOpenAIRequest detects
// SSE format via body prefix (data:) even when Content-Type is application/json,
// and parses it correctly via the SSE path.
//
// **Validates: Requirements 2.2**
func TestParseSSE_SSEDetectionViaBodyPrefix(t *testing.T) {
	sseBody := strings.Join([]string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"detected\"}}]}",
		"data: [DONE]",
		"",
	}, "\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Intentionally set JSON content type, but body is SSE
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseBody))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{
		URL:   srv.URL,
		Model: "test-model",
	}
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}

	resp, err := DoOpenAIRequest(context.Background(), cfg, messages, nil, srv.Client())
	if err != nil {
		t.Fatalf("DoOpenAIRequest returned error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}
	if got := resp.Choices[0].Message.Content; got != "detected" {
		t.Errorf("content = %q, want %q", got, "detected")
	}
}

// TestParseSSE_JSONPathUnchanged verifies that DoOpenAIRequest takes the JSON
// path (not SSE) when the response body starts with '{'.
//
// **Validates: Requirements 2.2**
func TestParseSSE_JSONPathUnchanged(t *testing.T) {
	jsonBody := `{"choices":[{"message":{"role":"assistant","content":"json-path"},"finish_reason":"stop"}]}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(jsonBody))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{
		URL:   srv.URL,
		Model: "test-model",
	}
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}

	resp, err := DoOpenAIRequest(context.Background(), cfg, messages, nil, srv.Client())
	if err != nil {
		t.Fatalf("DoOpenAIRequest returned error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}
	if got := resp.Choices[0].Message.Content; got != "json-path" {
		t.Errorf("content = %q, want %q", got, "json-path")
	}
	if got := resp.Choices[0].FinishReason; got != "stop" {
		t.Errorf("finish_reason = %q, want %q", got, "stop")
	}
}

func TestBuildResponsesAPIRequestData_ThinkingModeOverride(t *testing.T) {
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}
	build := func(cfg corelib.MaclawLLMConfig) map[string]interface{} {
		t.Helper()
		_, body, err := BuildResponsesAPIRequestData(cfg, messages, ResponsesAPIRequestOptions{})
		if err != nil {
			t.Fatalf("BuildResponsesAPIRequestData returned error: %v", err)
		}
		var req map[string]interface{}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("failed to parse responses request body: %v", err)
		}
		return req
	}

	// OpenAI-compatible endpoints (e.g. Volcano Ark) take the chat-style thinking object.
	ark := corelib.MaclawLLMConfig{URL: "https://ark.cn-beijing.volces.com/api/plan/v3", Model: "glm-5.2", WireAPI: "responses", ThinkingMode: "enabled"}
	req := build(ark)
	if thinking, _ := req["thinking"].(map[string]interface{}); thinking["type"] != "enabled" {
		t.Fatalf("ark enabled thinking = %#v, want type=enabled", req["thinking"])
	}
	if _, ok := req["reasoning"]; ok {
		t.Fatalf("ark enabled leaked reasoning key: %#v", req["reasoning"])
	}

	ark.ThinkingMode = "disabled"
	req = build(ark)
	if thinking, _ := req["thinking"].(map[string]interface{}); thinking["type"] != "disabled" {
		t.Fatalf("ark disabled thinking = %#v, want type=disabled", req["thinking"])
	}

	// OpenAI/xAI Responses takes reasoning.effort. Enabled mode also requests
	// the provider-authorized display summary that the GUI renders as thinking.
	oai := corelib.MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "gpt-5", WireAPI: "responses", ThinkingMode: "enabled"}
	req = build(oai)
	if reasoning, _ := req["reasoning"].(map[string]interface{}); reasoning["effort"] != "medium" {
		t.Fatalf("openai enabled reasoning = %#v, want effort=medium", req["reasoning"])
	}
	if reasoning, _ := req["reasoning"].(map[string]interface{}); reasoning["summary"] != "auto" {
		t.Fatalf("openai enabled reasoning = %#v, want summary=auto", req["reasoning"])
	}
	oai.ReasoningEffort = "high"
	req = build(oai)
	if reasoning, _ := req["reasoning"].(map[string]interface{}); reasoning["effort"] != "high" {
		t.Fatalf("openai enabled reasoning = %#v, want effort=high", req["reasoning"])
	}
	oai.ThinkingMode = "disabled"
	oai.ReasoningEffort = ""
	req = build(oai)
	if reasoning, _ := req["reasoning"].(map[string]interface{}); reasoning["effort"] != "minimal" {
		t.Fatalf("openai disabled reasoning = %#v, want effort=minimal", req["reasoning"])
	}
	if reasoning, _ := req["reasoning"].(map[string]interface{}); reasoning["summary"] != nil {
		t.Fatalf("openai disabled reasoning must not request a summary: %#v", req["reasoning"])
	}

	// Auto (empty) leaves provider defaults untouched.
	auto := corelib.MaclawLLMConfig{URL: "https://ark.cn-beijing.volces.com/api/plan/v3", Model: "glm-5.2", WireAPI: "responses"}
	req = build(auto)
	if _, ok := req["thinking"]; ok {
		t.Fatalf("auto leaked thinking key: %#v", req["thinking"])
	}
	if _, ok := req["reasoning"]; ok {
		t.Fatalf("auto leaked reasoning key: %#v", req["reasoning"])
	}
}

func TestBuildOpenAIChatRequestBody_ThinkingModeUsesProviderNativeControl(t *testing.T) {
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}

	deepseek := buildOpenAIChatRequestBody(
		corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-reasoner", ThinkingMode: "disabled"},
		messages,
		OpenAIChatRequestOptions{},
	)
	if thinking, _ := deepseek["thinking"].(map[string]interface{}); thinking["type"] != "disabled" {
		t.Fatalf("DeepSeek thinking = %#v, want disabled", deepseek["thinking"])
	}
	if _, hasEffort := deepseek["reasoning_effort"]; hasEffort {
		t.Fatalf("DeepSeek must not receive OpenAI reasoning_effort: %#v", deepseek)
	}

	grok := buildOpenAIChatRequestBody(
		corelib.MaclawLLMConfig{URL: "https://api.x.ai/v1", Model: "grok-4.5", ThinkingMode: "enabled", ReasoningEffort: "high"},
		messages,
		OpenAIChatRequestOptions{},
	)
	if got := grok["reasoning_effort"]; got != "high" {
		t.Fatalf("Grok reasoning_effort = %#v, want high", got)
	}
	if _, hasThinking := grok["thinking"]; hasThinking {
		t.Fatalf("Grok must not receive incompatible thinking object: %#v", grok)
	}

	qwen := buildOpenAIChatRequestBody(
		corelib.MaclawLLMConfig{URL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen3", ThinkingMode: "disabled"},
		messages,
		OpenAIChatRequestOptions{},
	)
	if got := qwen["enable_thinking"]; got != false {
		t.Fatalf("Qwen enable_thinking = %#v, want false", got)
	}
}

func TestBuildAnthropicMessagesRequestBodyBoundsThinkingBudgetToOutputLimit(t *testing.T) {
	req := BuildAnthropicMessagesRequestBody(
		corelib.MaclawLLMConfig{URL: "https://api.anthropic.com", Model: "claude-sonnet", ThinkingMode: "enabled"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		AnthropicMessagesRequestOptions{MaxTokens: 8},
	)
	thinking, _ := req["thinking"].(map[string]interface{})
	if thinking["budget_tokens"] != 7 {
		t.Fatalf("thinking budget = %#v, want 7", thinking["budget_tokens"])
	}
	if got := req["max_tokens"]; got != 8 {
		t.Fatalf("max_tokens = %#v, want configured limit preserved", got)
	}
}
