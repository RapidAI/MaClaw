package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestDoLLMRequestStreamSuppressesPreambleForToolCallRound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"saved already\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-save\",\"type\":\"function\",\"function\":{\"name\":\"knowledge_save_text\",\"arguments\":\"{\\\"text\\\":\\\"alpha\\\"}\"}}]},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	var streamed string
	resp, err := (&IMMessageHandler{}).doLLMRequestStream(context.Background(), streamTestConfig(server.URL), []interface{}{
		map[string]interface{}{"role": "user", "content": "save alpha"},
	}, []map[string]interface{}{streamTestTool("knowledge_save_text")}, server.Client(), func(delta string) {
		streamed += delta
	}, &llmStreamMetrics{})
	if err != nil {
		t.Fatalf("doLLMRequestStream: %v", err)
	}
	if streamed != "" {
		t.Fatalf("tool-call preamble leaked to stream: %q", streamed)
	}
	if resp == nil || len(resp.Choices) != 1 || len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %#v", resp)
	}
}

func TestDoLLMRequestStreamFlushesFinalAnswerTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"final\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\" answer\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	var streamed string
	resp, err := (&IMMessageHandler{}).doLLMRequestStream(context.Background(), streamTestConfig(server.URL), []interface{}{
		map[string]interface{}{"role": "user", "content": "say final"},
	}, []map[string]interface{}{streamTestTool("knowledge_save_text")}, server.Client(), func(delta string) {
		streamed += delta
	}, &llmStreamMetrics{})
	if err != nil {
		t.Fatalf("doLLMRequestStream: %v", err)
	}
	if streamed != "final answer" {
		t.Fatalf("streamed = %q, want final answer", streamed)
	}
	if resp == nil || len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "final answer" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func streamTestConfig(url string) corelib.MaclawLLMConfig {
	return corelib.MaclawLLMConfig{URL: url, Model: "test-model", Protocol: "openai"}
}

func streamTestTool(name string) map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": "test tool",
			"parameters":  map[string]interface{}{"type": "object"},
		},
	}
}
