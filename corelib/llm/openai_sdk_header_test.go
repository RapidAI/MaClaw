package llm

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestOpenAISDKStreamForwardsReasoningAliases(t *testing.T) {
	for _, field := range []string{"reasoning", "thinking"} {
		t.Run(field, func(t *testing.T) {
			var reasoning string
			client := &http.Client{Transport: openAISDKHeaderRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if !strings.Contains(req.URL.Path, "/chat/completions") {
					t.Fatalf("request path = %q", req.URL.Path)
				}
				body := `data: {"choices":[{"delta":{"` + field + `":"Plan first."}}]}` + "\n" +
					`data: {"choices":[{"delta":{"content":"Done."},"finish_reason":"stop"}]}` + "\n" +
					"data: [DONE]\n"
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       io.NopCloser(strings.NewReader(body)),
					Request:    req,
				}, nil
			})}

			response, err := DoOpenAIRequestStreamWithReasoning(
				context.Background(),
				corelib.MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "agnes", Protocol: "openai"},
				[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
				nil,
				client,
				nil,
				func(delta string) { reasoning += delta },
			)
			if err != nil {
				t.Fatalf("DoOpenAIRequestStreamWithReasoning: %v", err)
			}
			if got, want := response.Choices[0].Message.ReasoningContent, "Plan first."; got != want {
				t.Fatalf("response reasoning = %q, want %q", got, want)
			}
			if got, want := reasoning, "Plan first."; got != want {
				t.Fatalf("reasoning callback = %q, want %q", got, want)
			}
		})
	}
}

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

func TestOpenAISDKSendsXAIOAuthHeader(t *testing.T) {
	client := &http.Client{Transport: openAISDKHeaderRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("X-XAI-Token-Auth"); got != "xai-grok-cli" {
			t.Fatalf("X-XAI-Token-Auth = %q, want xai-grok-cli", got)
		}
		return openAISDKHeaderResponse(req), nil
	})}

	resp, err := DoOpenAIRequest(context.Background(), corelib.MaclawLLMConfig{
		URL: "https://api.x.ai/v1", Key: "oauth-token", Model: "grok-4.5",
		Protocol: "openai", ProviderName: "xAI-Grok", AuthType: "oauth",
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
