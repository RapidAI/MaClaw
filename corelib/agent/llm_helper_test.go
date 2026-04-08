package agent

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestDoSimpleLLMRequest_HTTPErrorIncludesStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("gateway failed"))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model"}
	_, err := DoSimpleLLMRequest(cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}, srv.Client(), 2*time.Second)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, err) {
		// keep staticcheck happy; actual assertion is string-based below
	}
	if !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("error = %q, want HTTP 502 included", err.Error())
	}
}
