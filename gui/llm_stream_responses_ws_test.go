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
