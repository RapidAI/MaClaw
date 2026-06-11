package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

type llmStreamRoundTripFunc func(*http.Request) (*http.Response, error)

func (f llmStreamRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestOpenAIStreamCodeGenRequestStripsUnsupportedStreamOptions(t *testing.T) {
	client := &http.Client{Transport: llmStreamRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if got := body["model"]; got != corelib.CodeGenDefaultModelID {
			t.Fatalf("model = %#v, want %q", got, corelib.CodeGenDefaultModelID)
		}
		if got := body["stream"]; got != true {
			t.Fatalf("stream = %#v, want true", got)
		}
		if _, ok := body["stream_options"]; ok {
			t.Fatalf("stream_options leaked into CodeGen GUI stream request: %#v", body)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n")),
			Request:    req,
		}, nil
	})}

	h := &IMMessageHandler{}
	resp, err := h.doOpenAILLMRequestStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: "https://codegen.qianxin-inc.cn/api/v1", Model: "auto", Protocol: "openai"},
		[]interface{}{map[string]string{"role": "user", "content": "test"}},
		nil,
		client,
		func(string) {},
		&llmStreamMetrics{},
	)
	if err != nil {
		t.Fatalf("doOpenAILLMRequestStream error: %v", err)
	}
	if resp == nil || len(resp.Choices) == 0 || resp.Choices[0].Message.Content != "ok" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestOpenAIStreamQwenRetriesWithoutToolsOnBadRequest(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if attempts == 1 {
			if _, ok := body["tools"]; !ok {
				t.Fatalf("first attempt missing tools: %#v", body)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, `{"error":{"message":"tools unsupported"}}`)
			return
		}
		if _, ok := body["tools"]; ok {
			t.Fatalf("retry leaked tools: %#v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	h := &IMMessageHandler{}
	resp, err := h.doOpenAILLMRequestStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: server.URL, Model: "qwen-27b", Protocol: "openai"},
		[]interface{}{map[string]string{"role": "user", "content": "test"}},
		[]map[string]interface{}{{
			"type":     "function",
			"function": map[string]interface{}{"name": "read_file", "parameters": map[string]interface{}{"type": "object"}},
		}},
		server.Client(),
		func(string) {},
		&llmStreamMetrics{},
	)
	if err != nil {
		t.Fatalf("doOpenAILLMRequestStream error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if resp == nil || len(resp.Choices) == 0 || resp.Choices[0].Message.Content != "ok" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestOpenAIStreamReasoningDoesNotEmitRolePrefixContent(t *testing.T) {
	const sensitive = "SECRET_BROWSER_REASONING_PAYLOAD"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, `data: {"choices":[{"delta":{"reasoning_content":"thinking kept\n"},"finish_reason":null}]}`+"\n\n")
		_, _ = fmt.Fprint(w, `data: {"choices":[{"delta":{"reasoning_content":"Browser: `+sensitive+`"},"finish_reason":null}]}`+"\n\n")
		_, _ = fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	h := &IMMessageHandler{}
	var streamed strings.Builder
	resp, err := h.doOpenAILLMRequestStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: server.URL, Model: "test-model", Protocol: "openai"},
		[]interface{}{map[string]string{"role": "user", "content": "test"}},
		nil,
		server.Client(),
		func(delta string) { streamed.WriteString(delta) },
		&llmStreamMetrics{},
	)
	if err != nil {
		t.Fatalf("doOpenAILLMRequestStream error: %v", err)
	}
	if resp == nil || len(resp.Choices) == 0 {
		t.Fatal("expected response choice")
	}
	if got := streamed.String(); strings.Contains(got, sensitive) || strings.Contains(got, "Browser:") {
		t.Fatalf("reasoning role prefix leaked to streamed tokens: %q", got)
	}
	msg := resp.Choices[0].Message
	if msg.ReasoningContent != "thinking kept" {
		t.Fatalf("ReasoningContent = %q, want sanitized reasoning", msg.ReasoningContent)
	}
	if strings.Contains(msg.Content, sensitive) || strings.Contains(msg.Content, "Browser:") {
		t.Fatalf("reasoning fallback leaked into content: %q", msg.Content)
	}
}

func TestOpenAIStreamNonSSEParseErrorDoesNotLogBody(t *testing.T) {
	const sensitive = "SECRET_NON_SSE_BODY_PAYLOAD"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, "Browser: "+sensitive)
	}))
	defer server.Close()

	var logs bytes.Buffer
	oldWriter := log.Writer()
	oldFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
	})

	h := &IMMessageHandler{}
	_, err := h.doOpenAILLMRequestStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: server.URL, Model: "test-model", Protocol: "openai"},
		[]interface{}{map[string]string{"role": "user", "content": "test"}},
		nil,
		server.Client(),
		func(string) {},
		&llmStreamMetrics{},
	)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if strings.Contains(logs.String(), sensitive) || strings.Contains(logs.String(), "Browser:") {
		t.Fatalf("non-SSE parse error logged response body: %q", logs.String())
	}
}

func TestOpenAIStreamHTTPErrorDoesNotLogOrReturnBody(t *testing.T) {
	const sensitive = "SECRET_HTTP_ERROR_BODY_PAYLOAD"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "Browser: "+sensitive)
	}))
	defer server.Close()

	var logs bytes.Buffer
	oldWriter := log.Writer()
	oldFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
	})

	h := &IMMessageHandler{}
	_, err := h.doOpenAILLMRequestStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: server.URL, Model: "test-model", Protocol: "openai"},
		[]interface{}{map[string]string{"role": "user", "content": "test"}},
		nil,
		server.Client(),
		func(string) {},
		&llmStreamMetrics{},
	)
	if err == nil {
		t.Fatal("expected HTTP error")
	}
	combined := logs.String() + "\n" + err.Error()
	if strings.Contains(combined, sensitive) || strings.Contains(combined, "Browser:") {
		t.Fatalf("HTTP error leaked response body: %q", combined)
	}
}
