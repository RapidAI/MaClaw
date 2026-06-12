package llm

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

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

func anthropicSDKHeaderErrorResponse(req *http.Request) *http.Response {
	return &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(`{"type":"error","error":{"type":"invalid_request_error","message":"bad request"}}`)),
		Request:    req,
	}
}
