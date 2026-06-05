package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
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
	}, srv.Client(), 5*time.Second)
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
