package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestCallLightweightLLMNormalizesCodeGenAutoModel(t *testing.T) {
	var gotModel string
	handler := &IMMessageHandler{
		client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("Decode: %v", err)
			}
			gotModel, _ = body["model"].(string)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"simple"}}]}`)),
				Request:    r,
			}, nil
		})},
	}

	got := handler.callLightweightLLM(
		corelib.MaclawLLMConfig{URL: "https://codegen.qianxin-inc.cn/api/v1", Model: "auto"},
		"classify",
		"hi",
		2,
	)

	if strings.TrimSpace(got) != "simple" {
		t.Fatalf("response = %q, want simple", got)
	}
	if gotModel != corelib.CodeGenDefaultModelID {
		t.Fatalf("model = %q, want %q", gotModel, corelib.CodeGenDefaultModelID)
	}
}

func TestCallLightweightLLMSanitizesQwenOpenAICompatRequest(t *testing.T) {
	handler := &IMMessageHandler{
		client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("Decode: %v", err)
			}
			for _, key := range []string{"stream_options", "parallel_tool_calls", "store", "metadata", "response_format", "tool_choice", "function_call", "logprobs", "top_logprobs"} {
				if _, ok := body[key]; ok {
					t.Fatalf("Qwen lightweight request leaked %s: %#v", key, body)
				}
			}
			if got := body["stream"]; got != false {
				t.Fatalf("stream = %#v, want false", got)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"complex"}}]}`)),
				Request:    r,
			}, nil
		})},
	}

	got := handler.callLightweightLLM(
		corelib.MaclawLLMConfig{URL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-27b", ProviderName: "Qwen"},
		"classify",
		"hi",
		2,
	)

	if strings.TrimSpace(got) != "complex" {
		t.Fatalf("response = %q, want complex", got)
	}
}

func TestCallLightweightLLMUsesResponsesWireAPI(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	handler := &IMMessageHandler{
		client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			gotPath = r.URL.Path
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Errorf("Decode: %v", err)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"id":"resp_test","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"none"}]}]}`)),
				Request:    r,
			}, nil
		})},
	}

	got := handler.callLightweightLLM(
		corelib.MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-5.1", WireAPI: "responses"},
		"classify",
		"hi",
		2,
	)

	if strings.TrimSpace(got) != "none" {
		t.Fatalf("response = %q, want none", got)
	}
	if gotPath != "/api/paas/v4/responses" {
		t.Fatalf("path = %q, want /api/paas/v4/responses", gotPath)
	}
	if _, ok := gotBody["input"]; !ok {
		t.Fatalf("request body missing input: %#v", gotBody)
	}
	if _, ok := gotBody["messages"]; ok {
		t.Fatalf("request body leaked messages: %#v", gotBody)
	}
}
func TestCallLightweightLLMOnceDoesNotRetryFailedRoutingCall(t *testing.T) {
	calls := 0
	handler := &IMMessageHandler{
		client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return nil, errors.New("routing backend unavailable")
		})},
	}

	got := handler.callLightweightLLMOnce(
		corelib.MaclawLLMConfig{URL: "https://example.test/v1", Model: "small"},
		"classify",
		"hi",
		1,
	)

	if got != "" {
		t.Fatalf("response = %q, want empty after failed single attempt", got)
	}
	if calls != 1 {
		t.Fatalf("routing calls = %d, want 1", calls)
	}
}
