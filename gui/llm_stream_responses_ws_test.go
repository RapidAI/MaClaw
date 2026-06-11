package main

import (
	"encoding/json"
	"testing"

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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := responsesWSEndpoint(tt.url); got != tt.want {
				t.Fatalf("endpoint = %q, want %q", got, tt.want)
			}
		})
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
	values := params["properties"].(map[string]interface{})["values"].(map[string]interface{})
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
