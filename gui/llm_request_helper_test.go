package main

import (
	"context"
	"encoding/json"
	"io"
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

func TestDoSimpleOpenAIRequest_NormalizesCodeGenAutoModel(t *testing.T) {
	var gotModel string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
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
	resp, err := doSimpleOpenAIRequest(context.Background(), cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}, client, 2*time.Second)
	if err != nil {
		t.Fatalf("doSimpleOpenAIRequest returned error: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("content = %q, want ok", resp.Content)
	}
	if gotModel != corelib.CodeGenDefaultModelID {
		t.Fatalf("model = %q, want %q", gotModel, corelib.CodeGenDefaultModelID)
	}
}

func TestDoSimpleAnthropicRequest_NormalizesCodeGenAutoModel(t *testing.T) {
	var gotModel string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("Decode: %v", err)
		}
		gotModel, _ = body["model"].(string)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"content":[{"type":"text","text":"ok"}]}`)),
			Request:    r,
		}, nil
	})}

	cfg := corelib.MaclawLLMConfig{URL: "https://codegen.qianxin-inc.cn/api/v1", Model: "auto", Protocol: "anthropic"}
	resp, err := doSimpleAnthropicRequest(context.Background(), cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}, client, 2*time.Second)
	if err != nil {
		t.Fatalf("doSimpleAnthropicRequest returned error: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("content = %q, want ok", resp.Content)
	}
	if gotModel != corelib.CodeGenDefaultModelID {
		t.Fatalf("model = %q, want %q", gotModel, corelib.CodeGenDefaultModelID)
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

func TestDoSimpleLLMRequestUsesResponsesWireAPI(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"resp_test",
			"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]
		}`))
	}))
	defer server.Close()

	resp, err := doSimpleLLMRequest(context.Background(), corelib.MaclawLLMConfig{
		URL:      server.URL,
		Key:      "test-key",
		Model:    "glm-5.1",
		Protocol: "openai",
		WireAPI:  "responses",
	}, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, server.Client(), 5*time.Second)
	if err != nil {
		t.Fatalf("doSimpleLLMRequest: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("content = %q, want ok", resp.Content)
	}
	if gotPath != "/v1/responses" {
		t.Fatalf("path = %q, want /v1/responses", gotPath)
	}
	if _, ok := gotBody["messages"]; ok {
		t.Fatalf("chat messages leaked into Responses request: %#v", gotBody)
	}
	if _, ok := gotBody["input"]; !ok {
		t.Fatalf("responses input missing: %#v", gotBody)
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
