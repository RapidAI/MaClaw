package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestBuildResponsesWSHeadersDefaultsCodeGenClientName(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{Key: "sk-test", AgentType: "openclaw"}
	headers := buildResponsesWSHeaders(cfg, "wss://codegen.qianxin-inc.cn/api/v1/responses")
	if got := headers.Get(corelib.CodeGenClientNameHeader); got != corelib.CodeGenClientName {
		t.Fatalf("%s = %q, want %q", corelib.CodeGenClientNameHeader, got, corelib.CodeGenClientName)
	}
	if got := headers.Get("User-Agent"); got != "openclaw" {
		t.Fatalf("User-Agent = %q, want openclaw", got)
	}
}

func TestBuildResponsesWSHeadersPreservesCustomCodeGenClientName(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{Key: "sk-test", AgentType: "custom-agent"}
	headers := buildResponsesWSHeaders(cfg, "wss://api.codegen.qianxin-inc.cn/api/v1/responses")
	if got := headers.Get(corelib.CodeGenClientNameHeader); got != "custom-agent" {
		t.Fatalf("%s = %q, want custom-agent", corelib.CodeGenClientNameHeader, got)
	}
}

func TestBuildResponsesWSHeadersSkipsNonCodeGenURL(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{Key: "sk-test", AgentType: "custom-agent"}
	headers := buildResponsesWSHeaders(cfg, "wss://api.example.com/v1/responses")
	if got := headers.Get(corelib.CodeGenClientNameHeader); got != "" {
		t.Fatalf("non-CodeGen %s = %q, want empty", corelib.CodeGenClientNameHeader, got)
	}
}

func TestResponsesWSEndpointNormalizesBaseURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"bare https", "https://api.example.com", "wss://api.example.com/v1/responses"},
		{"v1 https", "https://api.example.com/v1", "wss://api.example.com/v1/responses"},
		{"full wss", "wss://api.example.com/v1/responses", "wss://api.example.com/v1/responses"},
		{"qwen compatible", "https://dashscope.aliyuncs.com/compatible-mode/v1", "wss://dashscope.aliyuncs.com/compatible-mode/v1/responses"},
		{"glm v4", "https://open.bigmodel.cn/api/paas/v4", "wss://open.bigmodel.cn/api/paas/v4/responses"},
		{"codex subscription base", "https://chatgpt.com/backend-api/codex", "wss://chatgpt.com/backend-api/codex/responses"},
		{"codex subscription full", "wss://chatgpt.com/backend-api/codex/responses", "wss://chatgpt.com/backend-api/codex/responses"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := responsesWSEndpoint(tt.url); got != tt.want {
				t.Fatalf("endpoint = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResponsesWSStreamConvertsBareJSONToolCallsAndSuppressesTokens(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("path = %q, want /v1/responses", r.URL.Path)
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil {
			t.Fatalf("read response.create: %v", err)
		}
		frames := []string{
			`{"type":"response.output_text.delta","delta":"{\"tool_calls\":[{\"function\":{\"name\":\"bash\","}`,
			`{"type":"response.output_text.delta","delta":"\"arguments\":\"{\\\"command\\\":\\\"dir\\\"}\"}}]}"}`,
			`{"type":"response.completed","response":{"status":"completed"}}`,
		}
		for _, frame := range frames {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
				t.Fatalf("write frame: %v", err)
			}
		}
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{
		URL:      srv.URL,
		Key:      "test-key",
		Model:    "test-model",
		Protocol: "openai",
		WireAPI:  "responses_ws",
	}
	var streamed strings.Builder
	resp, err := (&IMMessageHandler{}).doResponsesWSLLMRequestStream(
		context.Background(),
		cfg,
		[]interface{}{map[string]interface{}{"role": "user", "content": "run dir"}},
		nil,
		srv.Client(),
		func(delta string) { streamed.WriteString(delta) },
		nil,
	)
	if err != nil {
		t.Fatalf("doResponsesWSLLMRequestStream returned error: %v", err)
	}
	if got := streamed.String(); got != "" {
		t.Fatalf("stream leaked bare JSON tool call: %q", got)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices len = %d, want 1", len(resp.Choices))
	}
	choice := resp.Choices[0]
	if choice.FinishReason != "tool_calls" {
		t.Fatalf("finish_reason = %q, want tool_calls", choice.FinishReason)
	}
	if choice.Message.Content != "" {
		t.Fatalf("content = %q, want empty after tool conversion", choice.Message.Content)
	}
	if len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("tool_calls len = %d, want 1", len(choice.Message.ToolCalls))
	}
	call := choice.Message.ToolCalls[0]
	if call.Function.Name != "bash" {
		t.Fatalf("tool name = %q, want bash", call.Function.Name)
	}
	if call.Function.Arguments != `{"command":"dir"}` {
		t.Fatalf("tool arguments = %s, want command dir", call.Function.Arguments)
	}
}

func TestBuildResponsesWSFrameNormalizesCodeGenAutoModelAndSanitizesTools(t *testing.T) {
	data, err := buildResponsesWSFrame(
		corelib.MaclawLLMConfig{URL: "https://codegen.qianxin-inc.cn/api/v1", Model: "auto"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		[]map[string]interface{}{{
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
	)
	if err != nil {
		t.Fatalf("buildResponsesWSFrame: %v", err)
	}
	var frame map[string]interface{}
	if err := json.Unmarshal(data, &frame); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got := frame["model"]; got != corelib.CodeGenDefaultModelID {
		t.Fatalf("model = %#v, want %q", got, corelib.CodeGenDefaultModelID)
	}
	tool := frame["tools"].([]interface{})[0].(map[string]interface{})
	if _, ok := tool["strict"]; ok {
		t.Fatalf("strict leaked into Responses WS tool: %#v", tool)
	}
	params := tool["parameters"].(map[string]interface{})
	if _, ok := params["additionalProperties"]; ok {
		t.Fatalf("additionalProperties=false leaked: %#v", params)
	}
	properties := params["properties"].(map[string]interface{})
	for _, bad := range []string{"type", "properties"} {
		if _, ok := properties[bad]; ok {
			t.Fatalf("Responses WS properties container was treated as schema and leaked %q: %#v", bad, properties)
		}
	}
	values := properties["values"].(map[string]interface{})
	if got := values["items"].(map[string]interface{})["type"]; got != "string" {
		t.Fatalf("array items type = %#v, want string", got)
	}
}

func TestBuildResponsesWSFrameSanitizesQwenOpenAICompatProvider(t *testing.T) {
	data, err := buildResponsesWSFrame(
		corelib.MaclawLLMConfig{URL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-27b", ProviderName: "Qwen"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		[]map[string]interface{}{{
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
	)
	if err != nil {
		t.Fatalf("buildResponsesWSFrame: %v", err)
	}
	var frame map[string]interface{}
	if err := json.Unmarshal(data, &frame); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if _, ok := frame["store"]; ok {
		t.Fatalf("store leaked into Qwen Responses WS frame: %#v", frame)
	}
	tool := frame["tools"].([]interface{})[0].(map[string]interface{})
	if _, ok := tool["strict"]; ok {
		t.Fatalf("strict leaked into Qwen Responses WS tool: %#v", tool)
	}
	params := tool["parameters"].(map[string]interface{})
	if _, ok := params["additionalProperties"]; ok {
		t.Fatalf("additionalProperties=false leaked: %#v", params)
	}
	properties := params["properties"].(map[string]interface{})
	for _, bad := range []string{"type", "properties"} {
		if _, ok := properties[bad]; ok {
			t.Fatalf("Qwen Responses WS properties container was treated as schema and leaked %q: %#v", bad, properties)
		}
	}
	values := properties["values"].(map[string]interface{})
	if got := values["items"].(map[string]interface{})["type"]; got != "string" {
		t.Fatalf("array items type = %#v, want string", got)
	}
}

func TestBuildResponsesWSFrameHonorsGlobalThinkingMode(t *testing.T) {
	tests := []struct {
		name       string
		cfg        corelib.MaclawLLMConfig
		wantKey    string
		wantValue  interface{}
		absentKeys []string
	}{
		{
			name:      "DeepSeek compatible disabled",
			cfg:       corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-reasoner", ThinkingMode: "disabled"},
			wantKey:   "thinking",
			wantValue: map[string]interface{}{"type": "disabled"},
			absentKeys: []string{
				"reasoning", "reasoning_effort", "enable_thinking",
			},
		},
		{
			name:      "OpenAI enabled",
			cfg:       corelib.MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "gpt-5", ThinkingMode: "enabled", ReasoningEffort: "high"},
			wantKey:   "reasoning",
			wantValue: map[string]interface{}{"effort": "high", "summary": "auto"},
			absentKeys: []string{
				"thinking", "reasoning_effort", "enable_thinking",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := buildResponsesWSFrame(tt.cfg, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, nil)
			if err != nil {
				t.Fatalf("buildResponsesWSFrame: %v", err)
			}
			var frame map[string]interface{}
			if err := json.Unmarshal(data, &frame); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			if !reflect.DeepEqual(frame[tt.wantKey], tt.wantValue) {
				t.Fatalf("%s = %#v, want %#v", tt.wantKey, frame[tt.wantKey], tt.wantValue)
			}
			for _, key := range tt.absentKeys {
				if _, exists := frame[key]; exists {
					t.Fatalf("unexpected %s in frame: %#v", key, frame)
				}
			}
		})
	}
}

func TestResponsesWSStreamForwardsReasoningSummaryToThinkingChannel(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil {
			t.Fatalf("read response.create: %v", err)
		}
		for _, frame := range []string{
			`{"type":"response.reasoning_summary_text.delta","delta":"Check inputs. "}`,
			`{"type":"response.reasoning_summary_text.delta","delta":"Then answer."}`,
			`{"type":"response.output_text.delta","delta":"Done."}`,
			`{"type":"response.completed","response":{"status":"completed"}}`,
		} {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
				t.Fatalf("write frame: %v", err)
			}
		}
	}))
	defer srv.Close()

	var streamed strings.Builder
	resp, err := (&IMMessageHandler{}).doResponsesWSLLMRequestStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Key: "test-key", Model: "test-model", Protocol: "openai", WireAPI: "responses_ws"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "test"}},
		nil,
		srv.Client(),
		func(delta string) { streamed.WriteString(delta) },
		nil,
	)
	if err != nil {
		t.Fatalf("doResponsesWSLLMRequestStream returned error: %v", err)
	}
	if got, want := resp.Choices[0].Message.ReasoningContent, "Check inputs. Then answer."; got != want {
		t.Fatalf("reasoning_content = %q, want %q", got, want)
	}
	if got := resp.Choices[0].Message.Content; got != "Done." {
		t.Fatalf("content = %q, want Done.", got)
	}
	if got := streamed.String(); !strings.Contains(got, "\x01Check inputs. Then answer.") {
		t.Fatalf("reasoning summary was not sent to thinking channel: %q", got)
	}
}

func TestResponsesWSStreamUsesFinalReasoningItemWhenDeltasAreAbsent(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil {
			t.Fatalf("read response.create: %v", err)
		}
		for _, frame := range []string{
			`{"type":"response.output_item.done","item":{"type":"reasoning","summary":[{"type":"summary_text","text":"Use the final summary."}]}}`,
			`{"type":"response.output_text.delta","delta":"Done."}`,
			`{"type":"response.completed","response":{"status":"completed"}}`,
		} {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
				t.Fatalf("write frame: %v", err)
			}
		}
	}))
	defer srv.Close()

	var streamed strings.Builder
	resp, err := (&IMMessageHandler{}).doResponsesWSLLMRequestStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Key: "test-key", Model: "test-model", Protocol: "openai", WireAPI: "responses_ws"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "test"}},
		nil,
		srv.Client(),
		func(delta string) { streamed.WriteString(delta) },
		nil,
	)
	if err != nil {
		t.Fatalf("doResponsesWSLLMRequestStream returned error: %v", err)
	}
	if got, want := resp.Choices[0].Message.ReasoningContent, "Use the final summary."; got != want {
		t.Fatalf("reasoning_content = %q, want %q", got, want)
	}
	if got := streamed.String(); !strings.Contains(got, "\x01Use the final summary.") {
		t.Fatalf("final reasoning summary was not sent to thinking channel: %q", got)
	}
}

func TestResponsesWSStreamUsesCompletedResponseReasoningSummaryFallback(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil {
			t.Fatalf("read response.create: %v", err)
		}
		frame := `{"type":"response.completed","response":{"status":"completed","output":[{"type":"reasoning","summary":[{"type":"summary_text","text":"Completed summary."}]}]}}`
		if err := conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
			t.Fatalf("write frame: %v", err)
		}
	}))
	defer srv.Close()

	var streamed strings.Builder
	resp, err := (&IMMessageHandler{}).doResponsesWSLLMRequestStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Key: "test-key", Model: "test-model", Protocol: "openai", WireAPI: "responses_ws"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "test"}},
		nil,
		srv.Client(),
		func(delta string) { streamed.WriteString(delta) },
		nil,
	)
	if err != nil {
		t.Fatalf("doResponsesWSLLMRequestStream returned error: %v", err)
	}
	if got, want := resp.Choices[0].Message.ReasoningContent, "Completed summary."; got != want {
		t.Fatalf("reasoning_content = %q, want %q", got, want)
	}
	if got := streamed.String(); !strings.Contains(got, "\x01Completed summary.") {
		t.Fatalf("completed response summary was not streamed: %q", got)
	}
}

func TestBuildResponsesWSFrameUsesOpenAIChatToolSanitizer(t *testing.T) {
	data, err := buildResponsesWSFrame(
		corelib.MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "gpt-test"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		[]map[string]interface{}{{
			"type":  "function",
			"extra": "drop-me",
			"function": map[string]interface{}{
				"name":  "strict_tool",
				"extra": "drop-me",
				"parameters": map[string]interface{}{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]interface{}{
						"values": map[string]interface{}{"type": "array"},
					},
				},
			},
		}},
	)
	if err != nil {
		t.Fatalf("buildResponsesWSFrame: %v", err)
	}
	var frame map[string]interface{}
	if err := json.Unmarshal(data, &frame); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	tool := frame["tools"].([]interface{})[0].(map[string]interface{})
	if _, ok := tool["extra"]; ok {
		t.Fatalf("tool extra leaked into Responses WS frame: %#v", tool)
	}
	if got := tool["name"]; got != "strict_tool" {
		t.Fatalf("tool name = %#v, want strict_tool", got)
	}
	params := tool["parameters"].(map[string]interface{})
	values := params["properties"].(map[string]interface{})["values"].(map[string]interface{})
	if got := values["items"].(map[string]interface{})["type"]; got != "string" {
		t.Fatalf("array items type = %#v, want string", got)
	}
}

func TestBuildResponsesWSFrameDropsQwenOrphanedToolHistory(t *testing.T) {
	data, err := buildResponsesWSFrame(
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
		nil,
	)
	if err != nil {
		t.Fatalf("buildResponsesWSFrame: %v", err)
	}
	var frame map[string]interface{}
	if err := json.Unmarshal(data, &frame); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	for i, item := range frame["input"].([]interface{}) {
		m, _ := item.(map[string]interface{})
		if typ, _ := m["type"].(string); typ == "function_call" || typ == "function_call_output" {
			t.Fatalf("input item %d leaked orphaned tool history: %#v", i, m)
		}
	}
}

func TestBuildResponsesWSFrameDropsOrphanedToolHistory(t *testing.T) {
	data, err := buildResponsesWSFrame(
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
		nil,
	)
	if err != nil {
		t.Fatalf("buildResponsesWSFrame: %v", err)
	}
	var frame map[string]interface{}
	if err := json.Unmarshal(data, &frame); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	for i, item := range frame["input"].([]interface{}) {
		m, _ := item.(map[string]interface{})
		if typ, _ := m["type"].(string); typ == "function_call" || typ == "function_call_output" {
			t.Fatalf("input item %d leaked orphaned tool history: %#v", i, m)
		}
	}
}

func TestBuildResponsesWSFrameStringifiesToolArgumentObjects(t *testing.T) {
	data, err := buildResponsesWSFrame(
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
		nil,
	)
	if err != nil {
		t.Fatalf("buildResponsesWSFrame: %v", err)
	}
	var frame map[string]interface{}
	if err := json.Unmarshal(data, &frame); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	input := frame["input"].([]interface{})
	call := input[0].(map[string]interface{})
	if call["type"] != "function_call" {
		t.Fatalf("first input type = %#v, want function_call", call["type"])
	}
	if got := call["arguments"]; got != `{"limit":3,"q":"golang"}` {
		t.Fatalf("function_call arguments = %#v, want object encoded as JSON string", got)
	}
}
