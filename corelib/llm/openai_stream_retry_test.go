package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestDoOpenAIRequestStreamDoesNotCompactActiveToolRequestAfterCompat400(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/chat/completions" {
			t.Fatalf("path = %q, want /api/v1/chat/completions", r.URL.Path)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"compat reject"}}`))
	}))
	defer server.Close()
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	client := server.Client()
	client.Transport = rewriteHostRoundTripper{base: client.Transport, target: serverURL}

	cfg := corelib.MaclawLLMConfig{
		URL:          "http://codegen.qianxin-inc.cn/api/v1",
		Model:        "qax-codegen/Auto",
		ProviderName: "CodeGen",
		Protocol:     "openai",
	}
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": strings.Repeat("runtime context\n", 900) + "\n## 当前任务\n生成测试报告\n"},
		map[string]interface{}{"role": "user", "content": "生成测试策略阶段文档"},
	}
	tools := []map[string]interface{}{{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "write_file",
			"description": "write a file",
			"parameters":  map[string]interface{}{"type": "object"},
		},
	}}

	var streamed strings.Builder
	_, err = DoOpenAIRequestStream(context.Background(), cfg, messages, tools, client, func(token string) {
		streamed.WriteString(token)
	})
	if err == nil {
		t.Fatal("expected compatibility HTTP 400")
	}
	if streamed.Len() != 0 {
		t.Fatalf("unexpected streamed content after rejected tool request: %q", streamed.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 1 {
		t.Fatalf("request count = %d, want 1", len(bodies))
	}
	if _, ok := bodies[0]["tools"]; !ok {
		t.Fatalf("first request should include tools: %#v", bodies[0])
	}
}

func TestDoOpenAIRequestStreamDoesNotHideCompatRetryWhenRequestOwnerDisablesIt(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"compat reject"}}`))
	}))
	defer server.Close()
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	client := server.Client()
	client.Transport = rewriteHostRoundTripper{base: client.Transport, target: serverURL}
	cfg := corelib.MaclawLLMConfig{
		URL: "http://codegen.qianxin-inc.cn/api/v1", Model: "qax-codegen/Auto", ProviderName: "CodeGen", Protocol: "openai",
	}
	tools := []map[string]interface{}{{"type": "function", "function": map[string]interface{}{"name": "read_file", "parameters": map[string]interface{}{"type": "object"}}}}
	_, err = DoOpenAIRequestStream(WithTransparentRequestRetriesDisabled(context.Background()), cfg, []interface{}{map[string]interface{}{"role": "user", "content": "inspect"}}, tools, client, nil)
	if err == nil {
		t.Fatal("expected original compat failure")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts=%d, request owner must suppress hidden compatibility retries", got)
	}
}

func TestDoOpenAIRequestDoesNotHideCompatRetryWhenRequestOwnerDisablesIt(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"compat reject"}}`))
	}))
	defer server.Close()
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	client := server.Client()
	client.Transport = rewriteHostRoundTripper{base: client.Transport, target: serverURL}
	cfg := corelib.MaclawLLMConfig{
		URL: "http://codegen.qianxin-inc.cn/api/v1", Model: "qax-codegen/Auto", ProviderName: "CodeGen", Protocol: "openai",
	}
	tools := []map[string]interface{}{{"type": "function", "function": map[string]interface{}{"name": "read_file", "parameters": map[string]interface{}{"type": "object"}}}}
	_, err = DoOpenAIRequest(WithTransparentRequestRetriesDisabled(context.Background()), cfg, []interface{}{map[string]interface{}{"role": "user", "content": "inspect"}}, tools, client)
	if err == nil {
		t.Fatal("expected original compat failure")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts=%d, request owner must suppress hidden compatibility retries", got)
	}
}

func TestRequestOwnerDisablesAutomaticPOSTRedirectSuccessorRequests(t *testing.T) {
	var sourceAttempts atomic.Int32
	var successorAttempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/chat/completions":
			sourceAttempts.Add(1)
			w.Header().Set("Location", "/redirected")
			w.WriteHeader(http.StatusTemporaryRedirect)
		case "/redirected":
			successorAttempts.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"redirected"},"finish_reason":"stop"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Protocol: "openai"}
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "inspect"}}

	// Ordinary callers retain the existing HTTP redirect behavior.
	resp, err := DoOpenAIRequest(context.Background(), cfg, messages, nil, server.Client())
	if err != nil || resp == nil || len(resp.Choices) == 0 || resp.Choices[0].Message.Content != "redirected" {
		t.Fatalf("ordinary redirect response=%#v err=%v", resp, err)
	}
	if got := successorAttempts.Load(); got != 1 {
		t.Fatalf("ordinary caller did not follow redirect: successor attempts=%d", got)
	}

	sourceAttempts.Store(0)
	successorAttempts.Store(0)
	_, err = DoOpenAIRequest(WithTransparentRequestRetriesDisabled(context.Background()), cfg, messages, nil, server.Client())
	if err == nil {
		t.Fatal("request owner should receive redirect response instead of a hidden successor request")
	}
	if got := sourceAttempts.Load(); got != 1 {
		t.Fatalf("source attempts=%d, want one", got)
	}
	if got := successorAttempts.Load(); got != 0 {
		t.Fatalf("redirect created hidden successor request count=%d", got)
	}
}

func TestRequestOwnerDisablesResponsesAPIStreamingRedirectSuccessorRequests(t *testing.T) {
	var sourceAttempts atomic.Int32
	var successorAttempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/responses":
			sourceAttempts.Add(1)
			w.Header().Set("Location", "/redirected")
			w.WriteHeader(http.StatusPermanentRedirect)
		case "/redirected":
			successorAttempts.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: response.completed\ndata: {\"response\":{\"id\":\"resp\",\"output\":[]}}\n\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := corelib.MaclawLLMConfig{URL: server.URL, Model: "test", WireAPI: "responses"}
	_, err := DoResponsesAPIRequestStream(WithTransparentRequestRetriesDisabled(context.Background()), cfg, []interface{}{map[string]interface{}{"role": "user", "content": "inspect"}}, nil, server.Client(), nil, nil)
	if err == nil {
		t.Fatal("request owner should receive redirect response instead of a hidden Responses successor")
	}
	if got := sourceAttempts.Load(); got != 1 {
		t.Fatalf("source attempts=%d, want one", got)
	}
	if got := successorAttempts.Load(); got != 0 {
		t.Fatalf("redirect created hidden Responses successor request count=%d", got)
	}
}

func TestOpenAISSERejectsConflictingProviderResponseIDs(t *testing.T) {
	body := strings.NewReader(strings.Join([]string{
		`data: {"id":"chatcmpl-a","choices":[{"delta":{"content":"first"}}]}`,
		`data: {"id":"chatcmpl-b","choices":[{"delta":{"content":"second"}}]}`,
		"",
	}, "\n"))
	if _, err := parseSSEStream(body, nil); err == nil || !strings.Contains(err.Error(), "response ID changed") {
		t.Fatalf("conflicting OpenAI stream IDs error=%v", err)
	}
}

func TestParseSSEToResponseRejectsConflictingProviderResponseIDs(t *testing.T) {
	body := []byte(strings.Join([]string{
		`data: {"id":"chatcmpl-a","choices":[{"delta":{"content":"first"}}]}`,
		`data: {"id":"chatcmpl-b","choices":[{"delta":{"content":"second"}}]}`,
		"",
	}, "\n"))
	if _, err := ParseSSEToResponse(body); err == nil || !strings.Contains(err.Error(), "response ID changed") {
		t.Fatalf("conflicting OpenAI compatibility stream IDs error=%v", err)
	}
}

func TestDoOpenAIRequestStreamPreservesStructuredHTTPErrorBody(t *testing.T) {
	body := `{"code":"LLM_MODEL_FORBIDDEN","message":"no active model service entitlement","type":"invalid_request_error"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	cfg := corelib.MaclawLLMConfig{URL: server.URL, Model: "auto", Protocol: "openai"}
	_, err := DoOpenAIRequestStream(context.Background(), cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": "hello"},
	}, nil, server.Client(), nil)
	if err == nil {
		t.Fatal("expected HTTP 403 error")
	}
	var httpErr *HTTPStatusError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error type = %T, want HTTPStatusError", err)
	}
	if httpErr.StatusCode != http.StatusForbidden || string(httpErr.Body) != body {
		t.Fatalf("structured HTTP error = status %d body %q", httpErr.StatusCode, string(httpErr.Body))
	}
}

type rewriteHostRoundTripper struct {
	base   http.RoundTripper
	target *url.URL
}

func (r rewriteHostRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = r.target.Scheme
	clone.URL.Host = r.target.Host
	clone.Host = r.target.Host
	base := r.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}
