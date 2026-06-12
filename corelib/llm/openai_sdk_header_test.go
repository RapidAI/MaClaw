package llm

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestOpenAISDKDoesNotSendCodeGenHeaderForDeepSeek(t *testing.T) {
	client := &http.Client{Transport: openAISDKHeaderRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get(corelib.CodeGenClientNameHeader); got != "" {
			t.Fatalf("non-CodeGen %s = %q, want empty", corelib.CodeGenClientNameHeader, got)
		}
		return openAISDKHeaderResponse(req), nil
	})}

	resp, err := DoOpenAIRequest(context.Background(), corelib.MaclawLLMConfig{
		URL:       "https://api.deepseek.com/v1",
		Model:     "deepseek-v4-flash",
		Protocol:  "openai",
		AgentType: "opencode",
	}, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, nil, client)
	if err != nil {
		t.Fatalf("DoOpenAIRequest() error = %v", err)
	}
	if resp == nil || len(resp.Choices) != 1 {
		t.Fatalf("response = %#v", resp)
	}
}

func TestOpenAISDKSendsCodeGenHeaderForCodeGen(t *testing.T) {
	client := &http.Client{Transport: openAISDKHeaderRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get(corelib.CodeGenClientNameHeader); got != corelib.CodeGenClientName {
			t.Fatalf("%s = %q, want %q", corelib.CodeGenClientNameHeader, got, corelib.CodeGenClientName)
		}
		return openAISDKHeaderResponse(req), nil
	})}

	resp, err := DoOpenAIRequest(context.Background(), corelib.MaclawLLMConfig{
		URL:       "https://codegen.qianxin-inc.cn/api/v1",
		Model:     "auto",
		Protocol:  "openai",
		AgentType: "openclaw",
	}, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, nil, client)
	if err != nil {
		t.Fatalf("DoOpenAIRequest() error = %v", err)
	}
	if resp == nil || len(resp.Choices) != 1 {
		t.Fatalf("response = %#v", resp)
	}
}

type openAISDKHeaderRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn openAISDKHeaderRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func openAISDKHeaderResponse(req *http.Request) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(`{"id":"chatcmpl-test","object":"chat.completion","model":"test-model","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)),
		Request:    req,
	}
}
