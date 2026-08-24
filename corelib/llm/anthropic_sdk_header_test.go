package llm

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

type anthropicSDKHeaderRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn anthropicSDKHeaderRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestAnthropicSDKDoesNotSendCodeGenHeaderForGLM(t *testing.T) {
	client := &http.Client{Transport: anthropicSDKHeaderRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get(corelib.CodeGenClientNameHeader); got != "" {
			t.Fatalf("non-CodeGen %s = %q, want empty", corelib.CodeGenClientNameHeader, got)
		}
		return anthropicSDKHeaderErrorResponse(req), nil
	})}

	_, err := DoAnthropicRequest(context.Background(), corelib.MaclawLLMConfig{
		URL:       "https://open.bigmodel.cn/api/anthropic",
		Model:     "glm-5.1",
		Protocol:  "anthropic",
		AgentType: "claude code 2.0",
	}, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, nil, client)
	if err == nil {
		t.Fatal("DoAnthropicRequest() error = nil, want upstream error")
	}
}

func TestAnthropicSDKSendsCodeGenHeaderForCodeGen(t *testing.T) {
	client := &http.Client{Transport: anthropicSDKHeaderRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get(corelib.CodeGenClientNameHeader); got != corelib.CodeGenClientName {
			t.Fatalf("%s = %q, want %q", corelib.CodeGenClientNameHeader, got, corelib.CodeGenClientName)
		}
		return anthropicSDKHeaderErrorResponse(req), nil
	})}

	_, err := DoAnthropicRequest(context.Background(), corelib.MaclawLLMConfig{
		URL:       "https://codegen.qianxin-inc.cn/api/anthropic",
		Model:     "claude-test",
		Protocol:  "anthropic",
		AgentType: "openclaw",
	}, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, nil, client)
	if err == nil {
		t.Fatal("DoAnthropicRequest() error = nil, want upstream error")
	}
}

func TestAnthropicSDKSendsHubWorkloadHints(t *testing.T) {
	client := &http.Client{Transport: anthropicSDKHeaderRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("X-MaClaw-Task-Type"); got != "fast" {
			t.Fatalf("task type = %q", got)
		}
		if got := req.Header.Get("X-MaClaw-Workflow-Type"); got != "coding" {
			t.Fatalf("workflow type = %q", got)
		}
		if got := req.Header.Get("X-MaClaw-Phase-Kind"); got != "execution" {
			t.Fatalf("phase kind = %q", got)
		}
		if got := req.Header.Get("X-MaClaw-Workload-Class"); got != "" {
			t.Fatalf("desktop must not invent P0 class, got %q", got)
		}
		return anthropicSDKHeaderErrorResponse(req), nil
	})}

	cfg := corelib.MaclawLLMConfig{
		URL:      "https://hub.example.com/api/llm/v1",
		Model:    "auto",
		Protocol: "anthropic",
	}.WithHubWorkloadHints("fast", "coding", "execution")
	_, err := DoAnthropicRequest(context.Background(), cfg,
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, nil, client)
	if err == nil {
		t.Fatal("DoAnthropicRequest() error = nil, want upstream error")
	}
}

func TestAnthropicSDKDoesNotSendHintsToThirdParty(t *testing.T) {
	client := &http.Client{Transport: anthropicSDKHeaderRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("X-MaClaw-Task-Type"); got != "" {
			t.Fatalf("third-party sent task hint %q", got)
		}
		return anthropicSDKHeaderErrorResponse(req), nil
	})}

	cfg := corelib.MaclawLLMConfig{
		URL:          "https://api.anthropic.com",
		Model:        "claude-sonnet-4",
		Protocol:     "anthropic",
		TaskTypeHint: "fast",
	}
	_, err := DoAnthropicRequest(context.Background(), cfg,
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, nil, client)
	if err == nil {
		t.Fatal("DoAnthropicRequest() error = nil, want upstream error")
	}
}

func TestAnthropicSDKOptionsOmitsRequestTimeoutWhenContextHasDeadline(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{URL: "https://api.anthropic.com", TimeoutSec: 600}
	withDeadline, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	open := anthropicSDKOptions(context.Background(), cfg, http.DefaultClient)
	bound := anthropicSDKOptions(withDeadline, cfg, http.DefaultClient)
	if len(bound) >= len(open) {
		t.Fatalf("deadline context should omit SDK request timeout, open=%d bound=%d", len(open), len(bound))
	}
}

func anthropicSDKHeaderErrorResponse(req *http.Request) *http.Response {
	return &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(`{"type":"error","error":{"type":"invalid_request_error","message":"bad request"}}`)),
		Request:    req,
	}
}
