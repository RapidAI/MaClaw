package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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
	if req.Model != "claude-test" || req.System != "be brief" || !req.Stream || req.MaxTokens != 4096 {
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

func TestNewOpenAIChatRequest_SetsHeaders(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{
		URL:       "https://example.com/v1",
		Model:     "test-model",
		Key:       "secret-key",
		AgentType: "claude-code/2.0.0",
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
}

func TestOpenAIChatCompletionsEndpointNormalizesBaseURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"bare", "https://api.example.com", "https://api.example.com/v1/chat/completions"},
		{"v1", "https://api.example.com/v1", "https://api.example.com/v1/chat/completions"},
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

func TestOpenAI_RequestBody_SanitizesTypedMapToolArguments(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://example.test/v1", Model: "test-model"},
		[]interface{}{map[string]interface{}{
			"role": "assistant",
			"tool_calls": []map[string]interface{}{{
				"id":   "call_1",
				"type": "function",
				"function": map[string]interface{}{
					"name":      "search",
					"arguments": "{",
				},
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
	assistantCalls, _ := req.Messages[0]["tool_calls"].([]interface{})
	assistantCall, _ := assistantCalls[0].(map[string]interface{})
	fn, _ := assistantCall["function"].(map[string]interface{})
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
	assistantCalls, _ := req.Messages[0]["tool_calls"].([]interface{})
	assistantCall, _ := assistantCalls[0].(map[string]interface{})
	if _, ok := assistantCall["function"]; ok {
		t.Fatalf("function was invented for malformed tool call: %#v", assistantCall)
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
				"metadata":       map[string]interface{}{"trace": "x"},
				"store":          true,
				"stream_options": map[string]interface{}{"include_usage": true},
			},
			PassThrough: map[string]interface{}{
				"parallel_tool_calls": true,
				"logprobs":            true,
				"top_logprobs":        2,
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
	for _, key := range []string{"stream_options", "parallel_tool_calls", "store", "metadata", "response_format", "tool_choice", "function_call", "logprobs", "top_logprobs"} {
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
			map[string]interface{}{"role": "user", "content": "hi"},
			map[string]interface{}{"role": "assistant", "content": "previous", "reasoning_content": "hidden"},
			map[string]interface{}{"role": "assistant", "content": nil},
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
				"metadata":       map[string]interface{}{"trace": "x"},
				"store":          true,
				"stream_options": map[string]interface{}{"include_usage": true},
			},
			PassThrough: map[string]interface{}{
				"parallel_tool_calls": true,
				"logprobs":            true,
				"top_logprobs":        2,
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
	for _, key := range []string{"stream_options", "parallel_tool_calls", "store", "metadata", "response_format", "tool_choice", "function_call", "logprobs", "top_logprobs"} {
		if _, ok := req[key]; ok {
			t.Fatalf("Qwen request leaked %s: %#v", key, req)
		}
	}
	for _, message := range req["messages"].([]interface{}) {
		msg := message.(map[string]interface{})
		if _, ok := msg["reasoning_content"]; ok {
			t.Fatalf("Qwen request leaked reasoning_content in message: %#v", message)
		}
		if content, ok := msg["content"]; ok && content == nil {
			t.Fatalf("Qwen request leaked null content in message: %#v", message)
		}
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

func TestOpenAI_RequestBody_PreservesReasoningContentForNonConservativeProvider(t *testing.T) {
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
	if got := message["reasoning_content"]; got != "keep" {
		t.Fatalf("reasoning_content = %#v, want keep", got)
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
	values := params["properties"].(map[string]interface{})["values"].(map[string]interface{})
	if got := values["items"].(map[string]interface{})["type"]; got != "string" {
		t.Fatalf("Responses CodeGen array items type = %#v, want string", got)
	}

	req = BuildAnthropicMessagesRequestBody(cfg, messages, AnthropicMessagesRequestOptions{})
	if got := req["model"]; got != corelib.CodeGenDefaultModelID {
		t.Fatalf("Anthropic CodeGen model = %#v, want %q", got, corelib.CodeGenDefaultModelID)
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
	for _, key := range []string{"stream_options", "parallel_tool_calls", "store", "metadata", "response_format", "tool_choice", "function_call", "logprobs", "top_logprobs"} {
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
	values := params["properties"].(map[string]interface{})["values"].(map[string]interface{})
	if got := values["items"].(map[string]interface{})["type"]; got != "string" {
		t.Fatalf("array items type = %#v, want string", got)
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
}

func TestOpenAI_RequestBody_PreservesStrictToolSchemaForNonCodeGenProvider(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(
		corelib.MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "test-model"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		OpenAIChatRequestOptions{
			Tools: []map[string]interface{}{{
				"type": "function",
				"function": map[string]interface{}{
					"name":   "strict_tool",
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
	fn := toolDef["function"].(map[string]interface{})
	if got := fn["strict"]; got != true {
		t.Fatalf("strict = %#v, want true", got)
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
		ToolChoice: map[string]interface{}{
			"type":     "function",
			"function": map[string]interface{}{"name": "search"},
		},
		ResponseFormat: map[string]interface{}{
			"type": "json_schema",
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
		},
	})
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData returned error: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	for _, key := range []string{"temperature", "top_p", "max_tokens", "max_completion_tokens", "presence_penalty", "frequency_penalty", "stop", "parallel_tool_calls", "user", "seed", "n", "tool_choice", "response_format"} {
		if _, ok := req[key]; !ok {
			t.Fatalf("expected key %q in request body", key)
		}
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

	resp, err := ParseSSEToResponse(body)
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
