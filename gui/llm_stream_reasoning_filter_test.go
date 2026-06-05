package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

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
