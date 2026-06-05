package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestDoSimpleOpenAIRequest_ContentResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello world"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model"}
	resp, err := doSimpleOpenAIRequest(context.Background(), cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}, srv.Client(), 2*time.Second)
	if err != nil {
		t.Fatalf("doSimpleOpenAIRequest returned error: %v", err)
	}
	if got := resp.Content; got != "hello world" {
		t.Fatalf("content = %q, want %q", got, "hello world")
	}
}

func TestDoSimpleOpenAIRequest_ReasoningFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"","reasoning_content":"hidden answer"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model"}
	resp, err := doSimpleOpenAIRequest(context.Background(), cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}, srv.Client(), 2*time.Second)
	if err != nil {
		t.Fatalf("doSimpleOpenAIRequest returned error: %v", err)
	}
	if got := resp.Content; got != "hidden answer" {
		t.Fatalf("content = %q, want %q", got, "hidden answer")
	}
}

func TestDoSimpleOpenAIRequest_SSEFallback(t *testing.T) {
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
	resp, err := doSimpleOpenAIRequest(context.Background(), cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}, srv.Client(), 2*time.Second)
	if err != nil {
		t.Fatalf("doSimpleOpenAIRequest returned error: %v", err)
	}
	if got := resp.Content; got != "Hello world" {
		t.Fatalf("content = %q, want %q", got, "Hello world")
	}
}

func TestDoSimpleOpenAIRequest_ParseErrorPassthrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{"))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model"}
	_, err := doSimpleOpenAIRequest(context.Background(), cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}, srv.Client(), 2*time.Second)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "parse response") {
		t.Fatalf("expected parse response error, got %v", err)
	}
	if strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("expected parse error passthrough, got %v", err)
	}
}

func TestDumpLLMContextDoesNotPersistRequestBody(t *testing.T) {
	tempDir := t.TempDir()
	requestBody := []byte(`{"messages":[{"content":"Browser: SECRET_REQUEST_BODY"}]}`)
	err := dumpLLMContext(http.StatusInternalServerError, "llm request failed", requestBody, tempDir)
	if err == nil {
		t.Fatal("expected error")
	}
	errText := err.Error()
	if strings.Contains(errText, "SECRET_REQUEST_BODY") || strings.Contains(errText, "llm_context_") {
		t.Fatalf("error leaked sensitive data or dump path: %q", errText)
	}
	entries, readErr := os.ReadDir(tempDir)
	if readErr != nil {
		t.Fatalf("ReadDir: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no dump files, got %d", len(entries))
	}
}
