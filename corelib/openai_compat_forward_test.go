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
		{"paas v4", "https://open.bigmodel.cn/api/paas/v4", "https://open.bigmodel.cn/api/paas/v4/chat/completions"},
		{"full chat", "https://api.example.com/v1/chat/completions", "https://api.example.com/v1/chat/completions"},
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

func TestForwardOpenAICompatRequestNormalizesGLMCodingPlanEndpointAndMessages(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.URL.String(); got != "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions" {
			t.Fatalf("upstream URL = %q", got)
		}
		if got := req.Header.Get("User-Agent"); got != "Kilo Code" {
			t.Fatalf("User-Agent = %q, want Kilo Code", got)
		}
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		messages := body["messages"].([]any)
		firstContent := messages[0].(map[string]any)["content"]
		if firstContent != "look" {
			t.Fatalf("first content = %#v, want text-only look", firstContent)
		}
		secondContent := messages[1].(map[string]any)["content"]
		if secondContent != "[No user content provided]" {
			t.Fatalf("second content = %#v, want GLM placeholder", secondContent)
		}
		resp := `{"id":"chatcmpl-test","object":"chat.completion","model":"glm-5.1","choices":[]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(resp)),
			Request:    req,
		}, nil
	})}

	_, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-5.1", AgentType: "Kilo Code"}, map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": "look"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,xx"}},
			}},
			map[string]any{"role": "user", "content": ""},
		},
	}, client, "")
	if err != nil {
		t.Fatalf("ForwardOpenAICompatRequest() error = %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusOK)
	}
}

func TestOpenAICompatSDKBaseURLNormalizesProviderEndpoints(t *testing.T) {
	tests := []struct {
		name string
		cfg  MaclawLLMConfig
		want string
	}{
		{
			name: "bare deepseek host",
			cfg:  MaclawLLMConfig{URL: "https://api.deepseek.com", AgentType: "opencode"},
			want: "https://api.deepseek.com/v1",
		},
		{
			name: "deepseek chat endpoint",
			cfg:  MaclawLLMConfig{URL: "https://api.deepseek.com/v1/chat/completions", AgentType: "opencode"},
			want: "https://api.deepseek.com/v1",
		},
		{
			name: "glm coding plan rewrite",
			cfg:  MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/paas/v4", AgentType: "Kilo Code"},
			want: "https://open.bigmodel.cn/api/coding/paas/v4",
		},
		{
			name: "glm coding chat endpoint",
			cfg:  MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions", AgentType: "Kilo Code"},
			want: "https://open.bigmodel.cn/api/coding/paas/v4",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := openAICompatSDKBaseURL(tt.cfg); got != tt.want {
				t.Fatalf("openAICompatSDKBaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestForwardOpenAICompatRequestWithSDKReturnsStructuredErrorBody(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("User-Agent"); got != "opencode" {
			t.Fatalf("User-Agent = %q, want opencode", got)
		}
		if got := req.Header.Get(CodeGenClientNameHeader); got != "" {
			t.Fatalf("non-CodeGen %s = %q, want empty", CodeGenClientNameHeader, got)
		}
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"error":{"message":"bad request from upstream","type":"invalid_request_error"}}`)),
			Request:    req,
		}, nil
	})}

	body, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: "https://api.deepseek.com/v1/chat/completions", Model: "deepseek-v4-flash", AgentType: "opencode"}, map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}, client, "")
	if err == nil {
		t.Fatalf("ForwardOpenAICompatRequest() error = nil, want SDK API error")
	}
	if statusCode != http.StatusBadRequest {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusBadRequest)
	}
	if !strings.Contains(string(body), "bad request from upstream") {
		t.Fatalf("error body missing upstream message: %s", body)
	}
}

func TestForwardOpenAICompatRequestWithSDKSendsCodeGenHeaderOnlyForCodeGen(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get(CodeGenClientNameHeader); got != CodeGenClientName {
			t.Fatalf("%s = %q, want %q", CodeGenClientNameHeader, got, CodeGenClientName)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"id":"chatcmpl-test","object":"chat.completion","model":"qax-codegen/Auto","choices":[]}`)),
			Request:    req,
		}, nil
	})}

	_, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: "https://codegen.qianxin-inc.cn/api/v1", Model: "auto", AgentType: "openclaw"}, map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}, client, "")
	if err != nil {
		t.Fatalf("ForwardOpenAICompatRequest() error = %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusOK)
	}
}

func TestForwardOpenAICompatRequestDropsOrphanedToolChoice(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		if _, ok := body["tool_choice"]; ok {
			t.Fatalf("orphaned tool_choice leaked upstream: %#v", body)
		}
		resp := `{"id":"chatcmpl-test","object":"chat.completion","model":"glm-5.1","choices":[]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(resp)),
			Request:    req,
		}, nil
	})}

	_, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/coding/paas/v4", Model: "glm-5.1", AgentType: "Kilo Code"}, map[string]any{
		"messages":    []any{map[string]any{"role": "user", "content": "hi"}},
		"tool_choice": "required",
	}, client, "")
	if err != nil {
		t.Fatalf("ForwardOpenAICompatRequest() error = %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusOK)
	}
}

func TestForwardOpenAICompatRequestDropsToolChoiceWithLegacyFunctions(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		if _, ok := body["tool_choice"]; ok {
			t.Fatalf("tool_choice should not be sent with legacy functions: %#v", body)
		}
		if got := body["function_call"]; got != "auto" {
			t.Fatalf("function_call = %#v, want auto", got)
		}
		resp := `{"id":"chatcmpl-test","object":"chat.completion","model":"glm-5.1","choices":[]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(resp)),
			Request:    req,
		}, nil
	})}

	_, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/coding/paas/v4", Model: "glm-5.1", AgentType: "Kilo Code"}, map[string]any{
		"messages":      []any{map[string]any{"role": "user", "content": "hi"}},
		"functions":     []any{map[string]any{"name": "noop", "parameters": map[string]any{"type": "object"}}},
		"function_call": "auto",
		"tool_choice":   "required",
	}, client, "")
	if err != nil {
		t.Fatalf("ForwardOpenAICompatRequest() error = %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusOK)
	}
}

func TestForwardOpenAICompatRequestSanitizesSDKStructuredFields(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		messages := body["messages"].([]any)
		user := messages[0].(map[string]any)
		if _, ok := user["timestamp"]; ok {
			t.Fatalf("message extra field leaked upstream: %#v", user)
		}
		assistant := messages[1].(map[string]any)
		toolCalls := assistant["tool_calls"].([]any)
		toolCall := toolCalls[0].(map[string]any)
		if _, ok := toolCall["extra"]; ok {
			t.Fatalf("tool_call extra leaked upstream: %#v", toolCall)
		}
		fn := toolCall["function"].(map[string]any)
		if _, ok := fn["extra"]; ok {
			t.Fatalf("tool_call function extra leaked upstream: %#v", fn)
		}
		if got := fn["arguments"]; got != "{}" {
			t.Fatalf("invalid tool arguments = %#v, want sanitized empty object", got)
		}
		tool := body["tools"].([]any)[0].(map[string]any)
		if _, ok := tool["extra"]; ok {
			t.Fatalf("tool extra leaked upstream: %#v", tool)
		}
		toolFn := tool["function"].(map[string]any)
		if _, ok := toolFn["extra"]; ok {
			t.Fatalf("tool function extra leaked upstream: %#v", toolFn)
		}
		toolChoice := body["tool_choice"].(map[string]any)
		if _, ok := toolChoice["extra"]; ok {
			t.Fatalf("tool_choice extra leaked upstream: %#v", toolChoice)
		}
		responseFormat := body["response_format"].(map[string]any)
		jsonSchema := responseFormat["json_schema"].(map[string]any)
		if _, ok := jsonSchema["extra"]; ok {
			t.Fatalf("response_format extra leaked upstream: %#v", responseFormat)
		}
		resp := `{"id":"chatcmpl-test","object":"chat.completion","model":"gpt-test","choices":[]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(resp)),
			Request:    req,
		}, nil
	})}

	_, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: "https://api.example.com/v1", Model: "gpt-test"}, map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "hi", "timestamp": "drop-me"},
			map[string]any{
				"role":    "assistant",
				"content": "",
				"tool_calls": []any{map[string]any{
					"id":    "call_1",
					"type":  "function",
					"extra": "drop-me",
					"function": map[string]any{
						"name":      "noop",
						"arguments": "{",
						"extra":     "drop-me",
					},
				}},
			},
			map[string]any{"role": "tool", "tool_call_id": "call_1", "content": map[string]any{"ok": true}, "extra": "drop-me"},
		},
		"tools": []any{map[string]any{
			"type":  "function",
			"extra": "drop-me",
			"function": map[string]any{
				"name":       "noop",
				"extra":      "drop-me",
				"parameters": map[string]any{"type": "object", "properties": map[string]any{}},
			},
		}},
		"tool_choice": map[string]any{
			"type":     "function",
			"extra":    "drop-me",
			"function": map[string]any{"name": "noop", "extra": "drop-me"},
		},
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "answer",
				"schema": map[string]any{"type": "object"},
				"extra":  "drop-me",
			},
		},
	}, client, "")
	if err != nil {
		t.Fatalf("ForwardOpenAICompatRequest() error = %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusOK)
	}
}

func TestForwardOpenAICompatRequestPreservesObjectFunctionArguments(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		messages := body["messages"].([]any)
		assistant := messages[1].(map[string]any)
		toolCall := assistant["tool_calls"].([]any)[0].(map[string]any)
		fn := toolCall["function"].(map[string]any)
		var args map[string]any
		if err := json.Unmarshal([]byte(fn["arguments"].(string)), &args); err != nil {
			t.Fatalf("decode tool arguments: %v", err)
		}
		if got := args["path"]; got != "main.go" {
			t.Fatalf("tool argument path = %#v, want main.go", got)
		}
		functionCall := assistant["function_call"].(map[string]any)
		var legacyArgs map[string]any
		if err := json.Unmarshal([]byte(functionCall["arguments"].(string)), &legacyArgs); err != nil {
			t.Fatalf("decode legacy function arguments: %v", err)
		}
		if got := legacyArgs["cmd"]; got != "ls" {
			t.Fatalf("legacy function argument cmd = %#v, want ls", got)
		}
		resp := `{"id":"chatcmpl-test","object":"chat.completion","model":"gpt-test","choices":[]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(resp)),
			Request:    req,
		}, nil
	})}

	_, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: "https://api.example.com/v1", Model: "gpt-test"}, map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
			map[string]any{
				"role":    "assistant",
				"content": "",
				"tool_calls": []any{map[string]any{
					"id":   "call_1",
					"type": "function",
					"function": map[string]any{
						"name":      "read_file",
						"arguments": map[string]any{"path": "main.go"},
					},
				}},
				"function_call": map[string]any{
					"name":      "shell",
					"arguments": map[string]any{"cmd": "ls"},
				},
			},
			map[string]any{"role": "tool", "tool_call_id": "call_1", "content": "ok"},
		},
	}, client, "")
	if err != nil {
		t.Fatalf("ForwardOpenAICompatRequest() error = %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusOK)
	}
}

func TestForwardOpenAICompatRequestNormalizesMissingToolCallLinkage(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		messages := body["messages"].([]any)
		assistant := messages[0].(map[string]any)
		toolCall := assistant["tool_calls"].([]any)[0].(map[string]any)
		callID, _ := toolCall["id"].(string)
		if callID == "" {
			t.Fatalf("generated call id empty: %#v", toolCall)
		}
		tool := messages[1].(map[string]any)
		if got := tool["tool_call_id"]; got != callID {
			t.Fatalf("tool_call_id = %#v, want generated id %q", got, callID)
		}
		resp := `{"id":"chatcmpl-test","object":"chat.completion","model":"gpt-test","choices":[]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(resp)),
			Request:    req,
		}, nil
	})}

	_, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: "https://api.example.com/v1", Model: "gpt-test"}, map[string]any{
		"messages": []any{
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{map[string]any{
					"function": map[string]any{"name": "read_file", "arguments": map[string]any{"path": "main.go"}},
				}},
			},
			map[string]any{"role": "tool", "tool_call_id": "", "content": "ok"},
		},
	}, client, "")
	if err != nil {
		t.Fatalf("ForwardOpenAICompatRequest() error = %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusOK)
	}
}

func TestForwardOpenAICompatRequestDropsOrphanedToolHistory(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		messages := body["messages"].([]any)
		if len(messages) != 4 {
			t.Fatalf("messages len = %d, want 4 after dropping orphaned tool history: %#v", len(messages), messages)
		}
		firstAssistant := messages[1].(map[string]any)
		if _, ok := firstAssistant["tool_calls"]; ok {
			t.Fatalf("orphaned tool_calls leaked upstream: %#v", firstAssistant)
		}
		if got := messages[2].(map[string]any)["role"]; got != "assistant" {
			t.Fatalf("complete assistant role = %#v, want assistant", got)
		}
		if _, ok := messages[2].(map[string]any)["tool_calls"]; !ok {
			t.Fatalf("complete tool_calls were stripped: %#v", messages[2])
		}
		if got := messages[3].(map[string]any)["role"]; got != "tool" {
			t.Fatalf("complete tool role = %#v, want tool", got)
		}
		resp := `{"id":"chatcmpl-test","object":"chat.completion","model":"gpt-test","choices":[]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(resp)),
			Request:    req,
		}, nil
	})}

	_, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: "https://api.example.com/v1", Model: "gpt-test"}, map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{map[string]any{
					"id":       "call_orphan",
					"type":     "function",
					"function": map[string]any{"name": "noop", "arguments": "{}"},
				}},
			},
			map[string]any{"role": "tool", "tool_call_id": "call_wrong", "content": "wrong"},
			map[string]any{"role": "tool", "tool_call_id": "call_standalone", "content": "standalone"},
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{map[string]any{
					"id":       "call_ok",
					"type":     "function",
					"function": map[string]any{"name": "noop", "arguments": "{}"},
				}},
			},
			map[string]any{"role": "tool", "tool_call_id": "call_ok", "content": "ok"},
		},
	}, client, "")
	if err != nil {
		t.Fatalf("ForwardOpenAICompatRequest() error = %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusOK)
	}
}

func TestForwardOpenAICompatRequestNormalizesDeepSeekFlashOptions(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		if got := body["tool_choice"]; got != "auto" {
			t.Fatalf("tool_choice = %#v, want auto", got)
		}
		if got := body["n"]; got != float64(1) {
			t.Fatalf("n = %#v, want 1", got)
		}
		responseFormat := body["response_format"].(map[string]any)
		if got := responseFormat["type"]; got != "json_object" {
			t.Fatalf("response_format.type = %#v, want json_object", got)
		}
		messages := body["messages"].([]any)
		if got := messages[0].(map[string]any)["role"]; got != "system" {
			t.Fatalf("developer role = %#v, want system", got)
		}
		firstContent := messages[0].(map[string]any)["content"].(string)
		if !strings.Contains(strings.ToLower(firstContent), "json") {
			t.Fatalf("DeepSeek JSON instruction missing from first message: %#v", firstContent)
		}
		if got := messages[1].(map[string]any)["content"]; got != "text only" {
			t.Fatalf("deepseek content = %#v, want text only", got)
		}
		tools := body["tools"].([]any)
		fn := tools[0].(map[string]any)["function"].(map[string]any)
		if got := fn["strict"]; got != true {
			t.Fatalf("strict = %#v, want preserved true", got)
		}
		params := fn["parameters"].(map[string]any)
		if got := params["type"]; got != "object" {
			t.Fatalf("parameters.type = %#v, want object", got)
		}
		if _, ok := params["properties"]; !ok {
			t.Fatalf("parameters.properties missing: %#v", params)
		}
		if got := params["additionalProperties"]; got != false {
			t.Fatalf("additionalProperties = %#v, want preserved false", got)
		}
		resp := `{"id":"chatcmpl-test","object":"chat.completion","model":"deepseek-v4-flash","choices":[]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(resp)),
			Request:    req,
		}, nil
	})}

	_, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"}, map[string]any{
		"tool_choice":     "required",
		"response_format": map[string]any{"type": "json_schema"},
		"n":               2,
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":       "noop",
				"strict":     true,
				"parameters": map[string]any{"additionalProperties": false},
			},
		}},
		"messages": []any{
			map[string]any{"role": "developer", "content": "dev"},
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": "text only"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,xx"}},
			}},
		},
	}, client, "")
	if err != nil {
		t.Fatalf("ForwardOpenAICompatRequest() error = %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusOK)
	}
}

func TestForwardOpenAICompatRequestNormalizesTypedDeepSeekFlashResponseFormat(t *testing.T) {
	type jsonSchemaFormat struct {
		Type string `json:"type"`
		Name string `json:"name,omitempty"`
	}

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		responseFormat := body["response_format"].(map[string]any)
		if got := responseFormat["type"]; got != "json_object" {
			t.Fatalf("response_format.type = %#v, want json_object", got)
		}
		messages := body["messages"].([]any)
		firstContent := messages[0].(map[string]any)["content"].(string)
		if !strings.Contains(strings.ToLower(firstContent), "json") {
			t.Fatalf("DeepSeek JSON instruction missing from first message: %#v", firstContent)
		}
		resp := `{"id":"chatcmpl-test","object":"chat.completion","model":"deepseek-v4-flash","choices":[]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(resp)),
			Request:    req,
		}, nil
	})}

	_, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"}, map[string]any{
		"response_format": jsonSchemaFormat{Type: "json_schema", Name: "result"},
		"messages":        []any{map[string]any{"role": "user", "content": "Return ok true."}},
	}, client, "")
	if err != nil {
		t.Fatalf("ForwardOpenAICompatRequest() error = %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusOK)
	}
}

func TestForwardOpenAICompatRequestNormalizesTypedDeepSeekFlashToolChoice(t *testing.T) {
	type toolChoiceFunction struct {
		Name string `json:"name"`
	}
	type toolChoice struct {
		Type     string             `json:"type"`
		Function toolChoiceFunction `json:"function"`
	}

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		if got := body["tool_choice"]; got != "auto" {
			t.Fatalf("tool_choice = %#v, want auto", got)
		}
		resp := `{"id":"chatcmpl-test","object":"chat.completion","model":"deepseek-v4-flash","choices":[]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(resp)),
			Request:    req,
		}, nil
	})}

	_, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"}, map[string]any{
		"tool_choice": toolChoice{Type: "function", Function: toolChoiceFunction{Name: "noop"}},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":       "noop",
				"parameters": map[string]any{"type": "object"},
			},
		}},
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}, client, "")
	if err != nil {
		t.Fatalf("ForwardOpenAICompatRequest() error = %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusOK)
	}
}

func TestForwardOpenAICompatRequestNormalizesTypedMessages(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		messages := body["messages"].([]any)
		if got := messages[0].(map[string]any)["role"]; got != "system" {
			t.Fatalf("first role = %#v, want injected system", got)
		}
		if got := messages[1].(map[string]any)["role"]; got != "user" {
			t.Fatalf("second role = %#v, want user", got)
		}
		if got := messages[1].(map[string]any)["content"]; got != "Return ok true." {
			t.Fatalf("typed content blocks = %#v, want text-only content", got)
		}
		resp := `{"id":"chatcmpl-test","object":"chat.completion","model":"deepseek-v4-flash","choices":[]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(resp)),
			Request:    req,
		}, nil
	})}

	_, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"}, map[string]any{
		"response_format": map[string]any{"type": "json_object"},
		"messages": []map[string]interface{}{
			{"role": "user", "content": []map[string]interface{}{
				{"type": "text", "text": "Return ok true."},
				{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64,xx"}},
			}},
		},
	}, client, "")
	if err != nil {
		t.Fatalf("ForwardOpenAICompatRequest() error = %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusOK)
	}
}

func TestForwardOpenAICompatRequestNormalizesStringMapMessages(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		messages := body["messages"].([]any)
		first := messages[0].(map[string]any)
		if got := first["role"]; got != "system" {
			t.Fatalf("developer role = %#v, want system", got)
		}
		content := first["content"].(string)
		if !strings.Contains(content, "dev") || !strings.Contains(strings.ToLower(content), "json") {
			t.Fatalf("system content should merge developer and JSON instruction, got %#v", content)
		}
		resp := `{"id":"chatcmpl-test","object":"chat.completion","model":"deepseek-v4-flash","choices":[]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(resp)),
			Request:    req,
		}, nil
	})}

	_, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"}, map[string]any{
		"response_format": map[string]any{"type": "json_object"},
		"messages": []map[string]string{
			{"role": "developer", "content": "dev"},
			{"role": "user", "content": "Return ok true."},
		},
	}, client, "")
	if err != nil {
		t.Fatalf("ForwardOpenAICompatRequest() error = %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusOK)
	}
}

func TestForwardOpenAICompatRequestTextualizesStringMapContentBlocks(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		messages := body["messages"].([]any)
		user := messages[0].(map[string]any)
		if got := user["content"]; got != "Say OK." {
			t.Fatalf("string-map content blocks = %#v, want Say OK.", got)
		}
		resp := `{"id":"chatcmpl-test","object":"chat.completion","model":"deepseek-v4-flash","choices":[]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(resp)),
			Request:    req,
		}, nil
	})}

	_, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"}, map[string]any{
		"messages": []map[string]interface{}{
			{"role": "user", "content": []map[string]string{
				{"type": "text", "text": "Say OK."},
			}},
		},
	}, client, "")
	if err != nil {
		t.Fatalf("ForwardOpenAICompatRequest() error = %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusOK)
	}
}

func TestForwardOpenAICompatRequestNormalizesStringMapFunctions(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		functions := body["functions"].([]any)
		fn := functions[0].(map[string]any)
		params := fn["parameters"].(map[string]any)
		if got := params["type"]; got != "object" {
			t.Fatalf("parameters.type = %#v, want object", got)
		}
		if _, ok := params["properties"]; !ok {
			t.Fatalf("parameters.properties missing: %#v", params)
		}
		resp := `{"id":"chatcmpl-test","object":"chat.completion","model":"deepseek-v4-flash","choices":[]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(resp)),
			Request:    req,
		}, nil
	})}

	_, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"}, map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
		"functions": []map[string]string{
			{"name": "noop", "description": "noop"},
		},
		"function_call": "auto",
	}, client, "")
	if err != nil {
		t.Fatalf("ForwardOpenAICompatRequest() error = %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusOK)
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
		for _, bad := range []string{"type", "properties"} {
			if _, ok := props[bad]; ok {
				t.Fatalf("properties container was treated as schema and leaked %q: %#v", bad, props)
			}
		}
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
		legacyProps := legacyParams["properties"].(map[string]any)
		for _, bad := range []string{"type", "properties"} {
			if _, ok := legacyProps[bad]; ok {
				t.Fatalf("legacy properties container was treated as schema and leaked %q: %#v", bad, legacyProps)
			}
		}
		legacyIDs := legacyProps["ids"].(map[string]any)
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
		for _, key := range []string{"stream_options", "parallel_tool_calls", "store", "metadata", "response_format", "tool_choice", "function_call", "logprobs", "top_logprobs"} {
			if _, ok := body[key]; ok {
				t.Fatalf("%s leaked into CodeGen stream request: %#v", key, body)
			}
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(bytes.NewBufferString("data: [DONE]\n\n")),
			Request:    req,
		}, nil
	})}

	resp, err := ForwardOpenAICompatStreamRequest(context.Background(), MaclawLLMConfig{URL: "https://codegen.qianxin-inc.cn/api/v1", Model: "auto"}, map[string]any{
		"messages":            []any{},
		"metadata":            map[string]any{"trace": "x"},
		"parallel_tool_calls": true,
		"tool_choice":         "auto",
		"function_call":       "auto",
		"logprobs":            true,
		"top_logprobs":        2,
		"response_format":     map[string]any{"type": "json_schema"},
		"store":               true,
		"stream_options":      map[string]any{"include_usage": true},
	}, client)
	if err != nil {
		t.Fatalf("ForwardOpenAICompatStreamRequest() error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestForwardOpenAICompatStreamRequestPreservesExplicitStreamUsageOnly(t *testing.T) {
	type streamOptions struct {
		IncludeUsage bool   `json:"include_usage"`
		Extra        string `json:"extra,omitempty"`
	}

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		streamOptions, _ := body["stream_options"].(map[string]any)
		if streamOptions == nil {
			t.Fatalf("stream_options missing from non-CodeGen stream request: %#v", body)
		}
		if got := streamOptions["include_usage"]; got != false {
			t.Fatalf("stream_options.include_usage = %#v, want false", got)
		}
		if _, ok := streamOptions["extra"]; ok {
			t.Fatalf("stream_options.extra leaked upstream: %#v", streamOptions)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(bytes.NewBufferString("data: [DONE]\n\n")),
			Request:    req,
		}, nil
	})}

	resp, err := ForwardOpenAICompatStreamRequest(context.Background(), MaclawLLMConfig{URL: "https://api.example.com/v1", Model: "gpt-test"}, map[string]any{
		"messages":       []any{},
		"stream_options": streamOptions{IncludeUsage: false, Extra: "drop-me"},
	}, client)
	if err != nil {
		t.Fatalf("ForwardOpenAICompatStreamRequest() error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestForwardOpenAICompatStreamRequestDoesNotInventStreamOptions(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		if _, ok := body["stream_options"]; ok {
			t.Fatalf("stream_options should not be invented: %#v", body)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(bytes.NewBufferString("data: [DONE]\n\n")),
			Request:    req,
		}, nil
	})}

	resp, err := ForwardOpenAICompatStreamRequest(context.Background(), MaclawLLMConfig{URL: "https://api.example.com/v1", Model: "gpt-test"}, map[string]any{
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

func TestForwardOpenAICompatStreamRequestNormalizesDeepSeekFlashOptions(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		if got := body["stream"]; got != true {
			t.Fatalf("stream = %#v, want true", got)
		}
		if got := body["n"]; got != float64(1) {
			t.Fatalf("n = %#v, want 1", got)
		}
		if _, ok := body["tool_choice"]; ok {
			t.Fatalf("orphaned tool_choice leaked upstream: %#v", body)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(bytes.NewBufferString("data: [DONE]\n\n")),
			Request:    req,
		}, nil
	})}

	resp, err := ForwardOpenAICompatStreamRequest(context.Background(), MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"}, map[string]any{
		"messages":    []any{map[string]any{"role": "user", "content": "hi"}},
		"n":           2,
		"tool_choice": "required",
	}, client)
	if err != nil {
		t.Fatalf("ForwardOpenAICompatStreamRequest() error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestForwardOpenAICompatStreamRequestSanitizesQwenProvider(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		for _, key := range []string{"stream_options", "parallel_tool_calls", "store", "metadata", "response_format", "tool_choice", "function_call", "logprobs", "top_logprobs"} {
			if _, ok := body[key]; ok {
				t.Fatalf("%s leaked into Qwen stream request: %#v", key, body)
			}
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(bytes.NewBufferString("data: [DONE]\n\n")),
			Request:    req,
		}, nil
	})}

	resp, err := ForwardOpenAICompatStreamRequest(context.Background(), MaclawLLMConfig{URL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-27b"}, map[string]any{
		"messages":            []any{},
		"metadata":            map[string]any{"trace": "x"},
		"parallel_tool_calls": true,
		"tool_choice":         "auto",
		"function_call":       "auto",
		"logprobs":            true,
		"top_logprobs":        2,
		"response_format":     map[string]any{"type": "json_schema"},
		"store":               true,
		"stream_options":      map[string]any{"include_usage": true},
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

func TestForwardOpenAICompatResponsesRequestConvertsTypedMessages(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		input := body["input"].([]any)
		if len(input) != 1 {
			t.Fatalf("input len = %d, want 1: %#v", len(input), input)
		}
		message := input[0].(map[string]any)
		if got := message["role"]; got != "user" {
			t.Fatalf("input role = %#v, want user", got)
		}
		resp := `{"id":"resp-test","object":"response","model":"test-model","output":[]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(resp)),
			Request:    req,
		}, nil
	})}

	_, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: "https://api.example.com/v1", Model: "test-model", WireAPI: "responses"}, map[string]any{
		"messages": []map[string]interface{}{
			{"role": "user", "content": "hi"},
		},
	}, client, "")
	if err != nil {
		t.Fatalf("ForwardOpenAICompatRequest() error = %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusOK)
	}
}

func TestForwardOpenAICompatResponsesRequestPreservesToolStrict(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		tools := body["tools"].([]any)
		tool := tools[0].(map[string]any)
		if got := tool["strict"]; got != true {
			t.Fatalf("Responses tool strict = %#v, want true", got)
		}
		resp := `{"id":"resp-test","object":"response","model":"test-model","output":[]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(resp)),
			Request:    req,
		}, nil
	})}

	_, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: "https://api.example.com/v1", Model: "test-model", WireAPI: "responses"}, map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":       "strict_tool",
				"strict":     true,
				"parameters": map[string]any{"type": "object", "properties": map[string]any{}},
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

func TestForwardOpenAICompatResponsesRequestConvertsTypedContentBlocksAndTools(t *testing.T) {
	type contentBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type message struct {
		Role    string         `json:"role"`
		Content []contentBlock `json:"content"`
	}
	type functionDef struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description"`
		Parameters  map[string]interface{} `json:"parameters"`
	}
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		input := body["input"].([]any)
		content := input[0].(map[string]any)["content"].([]any)
		if got := content[0].(map[string]any)["text"]; got != "hello\nworld" {
			t.Fatalf("converted content text = %#v, want joined text blocks", got)
		}
		tools := body["tools"].([]any)
		tool := tools[0].(map[string]any)
		if got := tool["name"]; got != "typed_tool" {
			t.Fatalf("converted typed tool name = %#v, want typed_tool", got)
		}
		resp := `{"id":"resp-test","object":"response","model":"test-model","output":[]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(resp)),
			Request:    req,
		}, nil
	})}

	_, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: "https://api.example.com/v1", Model: "test-model", WireAPI: "responses"}, map[string]any{
		"messages": []message{{Role: "user", Content: []contentBlock{{Type: "text", Text: "hello"}, {Type: "image_url"}, {Type: "text", Text: "world"}}}},
		"tools": []map[string]interface{}{{
			"type": "function",
			"function": functionDef{
				Name:        "typed_tool",
				Description: "typed",
				Parameters:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
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

func TestForwardOpenAICompatAnthropicRequestConvertsTypedToolCalls(t *testing.T) {
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
		ToolCalls []toolCall `json:"tool_calls"`
	}
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		messages := body["messages"].([]any)
		assistant := messages[0].(map[string]any)
		content := assistant["content"].([]any)
		if got := content[0].(map[string]any)["text"]; got != "Checking." {
			t.Fatalf("assistant text block = %#v, want Checking.", got)
		}
		toolUse := content[1].(map[string]any)
		if toolUse["type"] != "tool_use" || toolUse["name"] != "read_file" || toolUse["id"] != "call_typed" {
			t.Fatalf("typed tool_calls were not converted to Anthropic tool_use: %#v", assistant)
		}
		resp := `{"id":"msg-test","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(resp)),
			Request:    req,
		}, nil
	})}

	_, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: "https://api.example.com/v1", Model: "claude-test", Protocol: "anthropic"}, map[string]any{
		"messages": []message{{
			Role:    "assistant",
			Content: "Checking.",
			ToolCalls: []toolCall{{
				ID:       "call_typed",
				Type:     "function",
				Function: toolFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`},
			}},
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

func TestForwardOpenAICompatRequestSurfacesStructuredUpstreamError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"error":{"message":"bad response_format"}}`)),
			Request:    req,
		}, nil
	})}

	body, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: "https://api.example.com/v1", Model: "test-model", Protocol: "anthropic"}, map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}, client, "")
	if err != nil {
		t.Fatalf("ForwardOpenAICompatRequest() error = %v", err)
	}
	if statusCode != http.StatusBadRequest {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusBadRequest)
	}
	if !strings.Contains(string(body), "bad response_format") {
		t.Fatalf("structured upstream message missing: %s", body)
	}
	if strings.Contains(string(body), "body_len") {
		t.Fatalf("structured upstream message should not fall back to body_len: %s", body)
	}
}

func TestForwardOpenAICompatRequestDoesNotEchoUnstructuredUpstreamError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(bytes.NewBufferString(`SECRET_RAW_BODY`)),
			Request:    req,
		}, nil
	})}

	body, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: "https://api.example.com/v1", Model: "test-model", WireAPI: "responses"}, map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}, client, "")
	if err != nil {
		t.Fatalf("ForwardOpenAICompatRequest() error = %v", err)
	}
	if statusCode != http.StatusBadRequest {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusBadRequest)
	}
	if strings.Contains(string(body), "SECRET_RAW_BODY") {
		t.Fatalf("unstructured upstream body leaked: %s", body)
	}
	if !strings.Contains(string(body), "body_len") {
		t.Fatalf("unstructured upstream error should include body_len: %s", body)
	}
}

func TestForwardOpenAICompatAnthropicRequestReturnsStructuredErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("path = %q, want /v1/messages", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad anthropic request","type":"invalid_request_error"}}`))
	}))
	defer server.Close()

	body, statusCode, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: server.URL, Model: "claude", Protocol: "anthropic"}, map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}, server.Client(), "")
	if err != nil {
		t.Fatalf("ForwardOpenAICompatRequest() error = %v", err)
	}
	if statusCode != http.StatusBadRequest {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusBadRequest)
	}
	if !strings.Contains(string(body), "bad anthropic request") {
		t.Fatalf("structured anthropic upstream message missing: %s", body)
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

func TestAnthropicBaseURLNormalizesMessageEndpoints(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"bare domain", "https://api.anthropic.com", "https://api.anthropic.com"},
		{"v1", "https://host/api/v1", "https://host/api"},
		{"messages", "https://host/api/messages", "https://host/api"},
		{"v1 messages", "https://host/api/v1/messages", "https://host/api"},
		{"uppercase suffix", "https://host/api/V1/MESSAGES", "https://host/api"},
		{"trailing slash", "https://host/api/v1/messages/", "https://host/api"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AnthropicBaseURL(tt.raw); got != tt.want {
				t.Fatalf("AnthropicBaseURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestForwardAnthropicMessageWithSDKUsesOfficialSDKShape(t *testing.T) {
	var gotPath, gotUserAgent, gotClientName string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotUserAgent = r.Header.Get("User-Agent")
		gotClientName = r.Header.Get(CodeGenClientNameHeader)
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_sdk","type":"message","role":"assistant","model":"glm-5.1","content":[{"type":"text","text":"forward ok"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":4,"output_tokens":2}}`))
	}))
	defer server.Close()

	body, status, err := forwardAnthropicMessageWithSDK(context.Background(), MaclawLLMConfig{
		URL:       server.URL,
		Key:       "test-key",
		Model:     "glm-5.1",
		Protocol:  "anthropic",
		AgentType: "claude code 2.0",
	}, map[string]interface{}{
		"model":      "glm-5.1",
		"max_tokens": 8,
		"stream":     true,
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
		},
	}, server.Client())
	if err != nil || status != http.StatusOK {
		t.Fatalf("forwardAnthropicMessageWithSDK status=%d err=%v body=%s", status, err, body)
	}
	if gotPath != "/v1/messages" {
		t.Fatalf("path = %q, want /v1/messages", gotPath)
	}
	if gotUserAgent != "claude code 2.0" {
		t.Fatalf("User-Agent = %q, want claude code 2.0", gotUserAgent)
	}
	if gotClientName != "" {
		t.Fatalf("non-CodeGen %s = %q, want empty", CodeGenClientNameHeader, gotClientName)
	}
	if _, ok := gotBody["stream"]; ok {
		t.Fatalf("stream leaked into non-stream SDK request: %#v", gotBody)
	}
	if !strings.Contains(string(body), "forward ok") {
		t.Fatalf("body = %s", body)
	}
}

func TestForwardAnthropicMessageWithSDKPreservesHTTP400RawBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(CodeGenClientNameHeader); got != "" {
			t.Fatalf("non-CodeGen %s = %q, want empty", CodeGenClientNameHeader, got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"bad anthropic payload"}}`))
	}))
	defer server.Close()

	body, status, err := forwardAnthropicMessageWithSDK(context.Background(), MaclawLLMConfig{
		URL:      server.URL,
		Key:      "test-key",
		Model:    "glm-5.1",
		Protocol: "anthropic",
	}, map[string]interface{}{
		"model":      "glm-5.1",
		"max_tokens": 8,
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
		},
	}, server.Client())
	if err == nil {
		t.Fatal("expected error")
	}
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	if !strings.Contains(string(body), "bad anthropic payload") {
		t.Fatalf("body = %s", body)
	}
}

func TestForwardAnthropicMessageWithSDKSendsCodeGenHeaderOnlyForCodeGen(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get(CodeGenClientNameHeader); got != CodeGenClientName {
			t.Fatalf("%s = %q, want %q", CodeGenClientNameHeader, got, CodeGenClientName)
		}
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"error":{"type":"invalid_request_error","message":"bad anthropic payload"}}`)),
			Request:    req,
		}, nil
	})}

	_, status, err := forwardAnthropicMessageWithSDK(context.Background(), MaclawLLMConfig{
		URL:       "https://codegen.qianxin-inc.cn/api/anthropic",
		Key:       "test-key",
		Model:     "claude-test",
		Protocol:  "anthropic",
		AgentType: "openclaw",
	}, map[string]interface{}{
		"model":      "claude-test",
		"max_tokens": 8,
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
		},
	}, client)
	if err == nil {
		t.Fatal("expected error")
	}
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
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

func TestForwardOpenAICompatRequestAnthropicMapsDeveloperAndToolOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		if body["system"] != "sys\ndev" {
			t.Fatalf("system = %#v, want merged system/developer", body["system"])
		}
		messages := body["messages"].([]any)
		toolResultMsg := messages[1].(map[string]any)
		blocks := toolResultMsg["content"].([]any)
		toolResult := blocks[0].(map[string]any)
		if toolResult["content"] != `{"ok":true}` {
			t.Fatalf("tool result content = %#v, want JSON string", toolResult["content"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_1",
			"stop_reason": "end_turn",
			"content": []any{map[string]any{
				"type": "text",
				"text": "ok",
			}},
		})
	}))
	defer server.Close()

	respBody, status, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: server.URL, Model: "claude", Protocol: "anthropic"}, map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": "sys"},
			map[string]any{"role": "developer", "content": "dev"},
			map[string]any{"role": "user", "content": "hi"},
			map[string]any{"role": "tool", "tool_call_id": "call_1", "content": map[string]any{"ok": true}},
		},
	}, server.Client(), "auto")
	if err != nil || status != http.StatusOK {
		t.Fatalf("ForwardOpenAICompatRequest status=%d err=%v body=%s", status, err, respBody)
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

func TestForwardOpenAICompatRequestResponsesDropsOrphanedToolHistory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		for i, item := range body["input"].([]any) {
			m, _ := item.(map[string]any)
			if typ, _ := m["type"].(string); typ == "function_call" || typ == "function_call_output" {
				t.Fatalf("input item %d leaked orphaned tool history: %#v", i, m)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "resp_1",
			"output": []any{map[string]any{
				"type": "message",
				"content": []any{map[string]any{
					"type": "output_text",
					"text": "ok",
				}},
			}},
		})
	}))
	defer server.Close()

	respBody, status, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: server.URL, Model: "gpt", WireAPI: "responses"}, map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{map[string]any{
					"id":       "call_1",
					"type":     "function",
					"function": map[string]any{"name": "read_file", "arguments": "{}"},
				}},
			},
			map[string]any{"role": "tool", "tool_call_id": "call_wrong", "content": "wrong"},
			map[string]any{"role": "user", "content": "next"},
		},
	}, server.Client(), "auto")
	if err != nil || status != http.StatusOK {
		t.Fatalf("ForwardOpenAICompatRequest status=%d err=%v body=%s", status, err, respBody)
	}
}

func TestForwardOpenAICompatRequestResponsesNormalizesMissingToolCallLinkage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		input := body["input"].([]any)
		call := input[0].(map[string]any)
		output := input[1].(map[string]any)
		callID, _ := call["call_id"].(string)
		if call["type"] != "function_call" || callID == "" {
			t.Fatalf("function_call not normalized: %#v", call)
		}
		if output["type"] != "function_call_output" || output["call_id"] != callID {
			t.Fatalf("function_call_output not linked to %q: %#v", callID, output)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "resp_1",
			"output": []any{map[string]any{
				"type": "message",
				"content": []any{map[string]any{
					"type": "output_text",
					"text": "ok",
				}},
			}},
		})
	}))
	defer server.Close()

	respBody, status, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: server.URL, Model: "gpt", WireAPI: "responses"}, map[string]any{
		"messages": []any{
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{map[string]any{
					"function": map[string]any{"name": "read_file", "arguments": map[string]any{"path": "main.go"}},
				}},
			},
			map[string]any{"role": "tool", "tool_call_id": "", "content": "ok"},
		},
	}, server.Client(), "auto")
	if err != nil || status != http.StatusOK {
		t.Fatalf("ForwardOpenAICompatRequest status=%d err=%v body=%s", status, err, respBody)
	}
}

func TestForwardOpenAICompatRequestResponsesMapsChatFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		if body["instructions"] != "dev note" {
			t.Fatalf("instructions = %#v, want developer note", body["instructions"])
		}
		if got := body["max_output_tokens"]; got != float64(321) {
			t.Fatalf("max_output_tokens = %#v, want 321", got)
		}
		if _, ok := body["max_tokens"]; ok {
			t.Fatalf("max_tokens leaked: %#v", body)
		}
		if _, ok := body["max_completion_tokens"]; ok {
			t.Fatalf("max_completion_tokens leaked: %#v", body)
		}
		text := body["text"].(map[string]any)
		format := text["format"].(map[string]any)
		if format["type"] != "json_schema" || format["name"] != "answer" {
			t.Fatalf("text.format = %#v", format)
		}
		if _, ok := format["json_schema"]; ok {
			t.Fatalf("text.format leaked nested json_schema: %#v", format)
		}
		if _, ok := body["metadata"]; !ok {
			t.Fatalf("metadata missing: %#v", body)
		}
		toolChoice := body["tool_choice"].(map[string]any)
		if toolChoice["type"] != "function" || toolChoice["name"] != "answer_tool" {
			t.Fatalf("tool_choice = %#v, want Responses function choice", toolChoice)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "resp_1", "output": []any{map[string]any{
			"type": "message",
			"content": []any{map[string]any{
				"type": "output_text",
				"text": "ok",
			}},
		}}})
	}))
	defer server.Close()

	respBody, status, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: server.URL, Model: "gpt", WireAPI: "responses"}, map[string]any{
		"messages": []any{
			map[string]any{"role": "developer", "content": "dev note"},
			map[string]any{"role": "user", "content": "hi"},
		},
		"max_tokens":            123,
		"max_completion_tokens": 321,
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "answer",
				"schema": map[string]any{"type": "object"},
			},
		},
		"tools": []any{map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "answer_tool", "parameters": map[string]any{"type": "object"}},
		}},
		"tool_choice": map[string]any{"type": "function", "function": map[string]any{"name": "answer_tool"}},
		"metadata":    map[string]any{"trace": "keep"},
	}, server.Client(), "auto")
	if err != nil || status != http.StatusOK {
		t.Fatalf("ForwardOpenAICompatRequest status=%d err=%v body=%s", status, err, respBody)
	}
}

func TestForwardOpenAICompatRequestResponsesStringifiesToolOutput(t *testing.T) {
	gotOutput := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		input := body["input"].([]any)
		output := input[0].(map[string]any)
		gotOutput, _ = output["output"].(string)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "resp_1", "output": []any{}})
	}))
	defer server.Close()

	_, status, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{URL: server.URL, Model: "gpt", WireAPI: "responses"}, map[string]any{
		"messages": []any{
			map[string]any{"role": "tool", "tool_call_id": "call_1", "content": map[string]any{"ok": true}},
		},
	}, server.Client(), "auto")
	if err != nil || status != http.StatusOK {
		t.Fatalf("ForwardOpenAICompatRequest status=%d err=%v", status, err)
	}
	if gotOutput != `{"ok":true}` {
		t.Fatalf("tool output = %#v, want JSON string", gotOutput)
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
		{"glm v4 chat", "https://open.bigmodel.cn/api/paas/v4", "/chat/completions", "https://open.bigmodel.cn/api/paas/v4/chat/completions"},
		{"glm coding v4 responses", "https://open.bigmodel.cn/api/coding/paas/v4", "/responses", "https://open.bigmodel.cn/api/coding/paas/v4/responses"},
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

	t.Run("object arguments", func(t *testing.T) {
		calls := []interface{}{
			map[string]interface{}{
				"function": map[string]interface{}{"name": "read_file", "arguments": map[string]interface{}{"path": "main.go"}},
			},
		}
		got := summarizeToolCalls(calls)
		if got != `[Tool Call: read_file] {"path":"main.go"}` {
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

func TestOpenAICompatForwardTextContentHandlesTypedSDKBlocks(t *testing.T) {
	type contentBlock struct {
		Type    string `json:"type"`
		Text    string `json:"text,omitempty"`
		Content string `json:"content,omitempty"`
	}
	got := openAICompatForwardTextContent([]contentBlock{
		{Type: "input_text", Text: "alpha"},
		{Type: "image_url"},
		{Type: "output_text", Content: "beta"},
	})
	if got != "alpha\nbeta" {
		t.Fatalf("text content = %q, want %q", got, "alpha\nbeta")
	}
}
