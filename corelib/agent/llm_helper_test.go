package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestDoSimpleLLMRequest_OpenAISSEFallback(t *testing.T) {
	sseBody := strings.Join([]string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}",
		"data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}",
		"data: [DONE]",
		"",
	}, "\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model"}
	resp, err := DoSimpleLLMRequest(cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}, srv.Client(), 2*time.Second)
	if err != nil {
		t.Fatalf("DoSimpleLLMRequest returned error: %v", err)
	}
	if got := resp.Content; got != "Hello world" {
		t.Fatalf("content = %q, want %q", got, "Hello world")
	}
}

func TestDoSimpleLLMRequest_NormalizesCodeGenAutoModel(t *testing.T) {
	var gotModel string
	client := &http.Client{Transport: agentRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("Decode: %v", err)
		}
		gotModel, _ = body["model"].(string)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)),
			Request:    r,
		}, nil
	})}

	cfg := corelib.MaclawLLMConfig{URL: "https://codegen.qianxin-inc.cn/api/v1", Model: "auto"}
	resp, err := DoSimpleLLMRequest(cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}, client, 2*time.Second)
	if err != nil {
		t.Fatalf("DoSimpleLLMRequest returned error: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("content = %q, want ok", resp.Content)
	}
	if gotModel != corelib.CodeGenDefaultModelID {
		t.Fatalf("model = %q, want %q", gotModel, corelib.CodeGenDefaultModelID)
	}
}

type agentRoundTripFunc func(*http.Request) (*http.Response, error)

func (f agentRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestDoSimpleLLMRequest_OpenAIReasoningFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"","reasoning_content":"hidden answer"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model"}
	resp, err := DoSimpleLLMRequest(cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}, srv.Client(), 2*time.Second)
	if err != nil {
		t.Fatalf("DoSimpleLLMRequest returned error: %v", err)
	}
	if got := resp.Content; got != "hidden answer" {
		t.Fatalf("content = %q, want %q", got, "hidden answer")
	}
}

func TestDoSimpleLLMRequest_UsesResponsesWireAPI(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("Decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resp_test","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"responses ok"}]}]}`))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{URL: srv.URL + "/v1", Model: "test-model", WireAPI: "responses"}
	resp, err := DoSimpleLLMRequest(cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}, srv.Client(), 2*time.Second)
	if err != nil {
		t.Fatalf("DoSimpleLLMRequest returned error: %v", err)
	}
	if resp.Content != "responses ok" {
		t.Fatalf("content = %q, want responses ok", resp.Content)
	}
	if gotPath != "/v1/responses" {
		t.Fatalf("path = %q, want /v1/responses", gotPath)
	}
	if _, ok := gotBody["input"]; !ok {
		t.Fatalf("request body missing Responses input: %#v", gotBody)
	}
	if _, ok := gotBody["messages"]; ok {
		t.Fatalf("request body leaked chat messages: %#v", gotBody)
	}
}

func TestDoSimpleLLMRequestContextWithOptionsSendsStrictIntentContract(t *testing.T) {
	for _, tc := range []struct {
		name    string
		wireAPI string
	}{
		{name: "chat"},
		{name: "responses", wireAPI: "responses"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotBody map[string]interface{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
					t.Fatalf("decode request: %v", err)
				}
				w.Header().Set("Content-Type", "application/json")
				if tc.wireAPI == "responses" {
					_, _ = w.Write([]byte(`{"id":"resp_test","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"{\"top\":[{\"skill\":\"live_data\",\"score\":0.9,\"workflow_type\":\"\"}]}"}]}]}`))
					return
				}
				_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"top\":[{\"skill\":\"live_data\",\"score\":0.9,\"workflow_type\":\"\"}]"},"finish_reason":"stop"}]}`))
			}))
			defer srv.Close()

			cfg := corelib.MaclawLLMConfig{URL: srv.URL + "/v1", Model: "test-model", WireAPI: tc.wireAPI}
			response, err := DoSimpleLLMRequestContextWithOptions(context.Background(), cfg, []interface{}{map[string]interface{}{"role": "user", "content": "classify"}}, srv.Client(), 2*time.Second, SimpleLLMRequestOptions{
				ResponseFormat:         intent.TreeResponseFormat(),
				PreserveResponseFormat: true,
			})
			if err != nil || response.Content == "" {
				t.Fatalf("response=%#v err=%v", response, err)
			}
			if tc.wireAPI == "responses" {
				text, _ := gotBody["text"].(map[string]interface{})
				format, _ := text["format"].(map[string]interface{})
				if format["type"] != "json_schema" || format["strict"] != true {
					t.Fatalf("Responses text.format = %#v, want strict JSON schema", format)
				}
				return
			}
			format, _ := gotBody["response_format"].(map[string]interface{})
			if format["type"] != "json_schema" {
				t.Fatalf("Chat response_format = %#v, want JSON schema", format)
			}
		})
	}
}

func TestDoSimpleLLMRequestContextWithOptionsRejectsUnsupportedStructuredProtocol(t *testing.T) {
	called := false
	client := &http.Client{Transport: agentRoundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("request must not be sent")
	})}
	_, err := DoSimpleLLMRequestContextWithOptions(context.Background(), corelib.MaclawLLMConfig{URL: "https://example.invalid", Model: "claude-test", Protocol: "anthropic"}, nil, client, time.Second, SimpleLLMRequestOptions{
		ResponseFormat:         intent.TreeResponseFormat(),
		PreserveResponseFormat: true,
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported for Anthropic") {
		t.Fatalf("error = %v, want explicit structured-output capability failure", err)
	}
	if called {
		t.Fatal("Anthropic request was sent after structured-output capability failure")
	}
}

func TestDoSimpleLLMRequest_RetriesUntilSuccess(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := attempts.Add(1)
		if current < 3 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(fmt.Sprintf("temporary failure %d", current)))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"recovered"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model"}
	resp, err := DoSimpleLLMRequest(cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}, srv.Client(), 2*time.Second)
	if err != nil {
		t.Fatalf("DoSimpleLLMRequest returned error: %v", err)
	}
	if resp.Content != "recovered" {
		t.Fatalf("content = %q, want %q", resp.Content, "recovered")
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

func TestDoSimpleLLMRequest_HTTPErrorIncludesStatus(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("gateway failed"))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model"}
	_, err := DoSimpleLLMRequest(cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}, srv.Client(), 10*time.Second)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, err) {
		// keep staticcheck happy; actual assertion is string-based below
	}
	if !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("error = %q, want HTTP 502 included", err.Error())
	}
	if !strings.Contains(err.Error(), "after 5 attempts") {
		t.Fatalf("error = %q, want retry count included", err.Error())
	}
	if got := attempts.Load(); got != 5 {
		t.Fatalf("attempts = %d, want 5", got)
	}
}

func TestDoSimpleLLMRequest_HTTPErrorDoesNotExposeBodyOrPrompt(t *testing.T) {
	const responseSecret = "Browser: SECRET_RESPONSE_BODY"
	const promptSecret = "SECRET_REQUEST_PROMPT"
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(responseSecret))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model"}
	_, err := DoSimpleLLMRequest(cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": promptSecret},
	}, srv.Client(), 5*time.Second)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	errText := err.Error()
	if strings.Contains(errText, responseSecret) || strings.Contains(errText, promptSecret) || strings.Contains(errText, "llm_context_") {
		t.Fatalf("error leaked sensitive data or dump path: %q", errText)
	}
	if !strings.Contains(errText, "request body not dumped") {
		t.Fatalf("error = %q, want request body not dumped marker", errText)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}

func TestDoSimpleLLMRequest_DoesNotRetryClientErrors(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("invalid api key"))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model"}
	_, err := DoSimpleLLMRequest(cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}, srv.Client(), 2*time.Second)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if strings.Contains(err.Error(), "after 5 attempts") {
		t.Fatalf("error = %q, should not include retry count for non-retryable error", err.Error())
	}
	if !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("error = %q, want HTTP 401 included", err.Error())
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}

func TestDoSimpleLLMRequest_StopsWaitingWhenTimeoutExpires(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("gateway failed"))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model"}
	start := time.Now()
	_, err := DoSimpleLLMRequest(cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}, srv.Client(), 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("error = %q, want context deadline exceeded", err.Error())
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
	if elapsed := time.Since(start); elapsed > 180*time.Millisecond {
		t.Fatalf("elapsed = %s, want backoff wait to stop promptly", elapsed)
	}
}

func TestApplyRefreshedLLMAuth(t *testing.T) {
	dst := corelib.MaclawLLMConfig{Key: "old", AuthType: "oauth", Model: "grok-4.6"}
	got := applyRefreshedLLMAuth(dst, corelib.MaclawLLMConfig{Key: "new", AuthType: "oauth"})
	if got.Key != "new" || got.Model != "grok-4.6" {
		t.Fatalf("got %+v", got)
	}
	got = applyRefreshedLLMAuth(dst, corelib.MaclawLLMConfig{})
	if got.Key != "old" {
		t.Fatalf("empty refresh must keep key, got %q", got.Key)
	}
}

func TestShouldRetrySimpleLLMError_OAuthTokenValidation(t *testing.T) {
	oauthErr := &llm.HTTPStatusError{
		StatusCode: http.StatusForbidden,
		Body:       []byte(`{"error":{"message":"The OAuth2 access token could not be validated."}}`),
	}
	if !shouldRetrySimpleLLMError(oauthErr) {
		t.Fatal("OAuth token-validation 403 should be retried")
	}
	if shouldRetrySimpleLLMError(&llm.HTTPStatusError{
		StatusCode: http.StatusForbidden,
		Body:       []byte(`{"code":"LLM_MODEL_FORBIDDEN","message":"no active model service entitlement"}`),
	}) {
		t.Fatal("generic model-forbidden 403 should not be retried")
	}
	if shouldRetrySimpleLLMError(errors.New("OpenAI 认证失败 (HTTP 401)")) {
		t.Fatal("generic 401 should not be retried")
	}
}

func TestDoSimpleLLMRequest_RetriesOAuthTokenValidation403(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := attempts.Add(1)
		if current < 3 {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"message":"The OAuth2 access token could not be validated."}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"recovered"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model"}
	resp, err := DoSimpleLLMRequest(cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}, srv.Client(), 10*time.Second)
	if err != nil {
		t.Fatalf("DoSimpleLLMRequest returned error: %v", err)
	}
	if resp.Content != "recovered" {
		t.Fatalf("content = %q, want recovered", resp.Content)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}
