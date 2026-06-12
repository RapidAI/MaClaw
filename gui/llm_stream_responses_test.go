package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestResponsesAPIStreamConvertsPlainToolCallAndSuppressesTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("path = %q, want /v1/responses", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "event: response.output_text.delta\n")
		_, _ = fmt.Fprint(w, `data: {"delta":"checking\nTOOL"}`+"\n")
		_, _ = fmt.Fprint(w, "event: response.output_text.delta\n")
		_, _ = fmt.Fprint(w, `data: {"delta":"_CALL\n{\"function\":\"ssh_execute_command\",\"args\":{\"host\":\"example.com\",\"username\":\"root\",\"password\":\"secret-value\",\"command\":\"df -h\"}}"}`+"\n")
		_, _ = fmt.Fprint(w, "event: response.completed\n")
		_, _ = fmt.Fprint(w, `data: {"response":{"status":"completed"}}`+"\n")
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{
		URL:      srv.URL,
		Key:      "test-key",
		Model:    "test-model",
		Protocol: "openai",
		WireAPI:  "responses",
	}
	var streamed strings.Builder
	resp, err := (&IMMessageHandler{}).doResponsesAPILLMRequestStream(
		context.Background(),
		cfg,
		[]interface{}{map[string]interface{}{"role": "user", "content": "check disk"}},
		nil,
		srv.Client(),
		func(delta string) { streamed.WriteString(delta) },
		nil,
	)
	if err != nil {
		t.Fatalf("doResponsesAPILLMRequestStream returned error: %v", err)
	}
	if got := streamed.String(); strings.Contains(got, "TOOL_CALL") || strings.Contains(got, "secret-value") {
		t.Fatalf("stream leaked plain tool call: %q", got)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices len = %d, want 1", len(resp.Choices))
	}
	choice := resp.Choices[0]
	if choice.FinishReason != "tool_calls" {
		t.Fatalf("finish_reason = %q, want tool_calls", choice.FinishReason)
	}
	if choice.Message.Content != "" {
		t.Fatalf("content = %q, want empty after tool conversion", choice.Message.Content)
	}
	if len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("tool_calls len = %d, want 1", len(choice.Message.ToolCalls))
	}
	call := choice.Message.ToolCalls[0]
	if call.Function.Name != "ssh" {
		t.Fatalf("tool name = %q, want ssh", call.Function.Name)
	}
	for _, want := range []string{`"action":"connect"`, `"host":"example.com"`, `"user":"root"`, `"initial_command":"df -h"`} {
		if !strings.Contains(call.Function.Arguments, want) {
			t.Fatalf("tool arguments = %s, want to contain %s", call.Function.Arguments, want)
		}
	}
}

func TestResponsesAPIStreamConvertsBareJSONToolCallsAndSuppressesTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("path = %q, want /v1/responses", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "event: response.output_text.delta\n")
		_, _ = fmt.Fprint(w, `data: {"delta":"{\"tool_calls\":[{\"function\":{\"name\":\"bash\","}`+"\n")
		_, _ = fmt.Fprint(w, "event: response.output_text.delta\n")
		_, _ = fmt.Fprint(w, `data: {"delta":"\"arguments\":\"{\\\"command\\\":\\\"dir\\\"}\"}}]}"}`+"\n")
		_, _ = fmt.Fprint(w, "event: response.completed\n")
		_, _ = fmt.Fprint(w, `data: {"response":{"status":"completed"}}`+"\n")
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{
		URL:      srv.URL,
		Key:      "test-key",
		Model:    "test-model",
		Protocol: "openai",
		WireAPI:  "responses",
	}
	var streamed strings.Builder
	resp, err := (&IMMessageHandler{}).doResponsesAPILLMRequestStream(
		context.Background(),
		cfg,
		[]interface{}{map[string]interface{}{"role": "user", "content": "run dir"}},
		nil,
		srv.Client(),
		func(delta string) { streamed.WriteString(delta) },
		nil,
	)
	if err != nil {
		t.Fatalf("doResponsesAPILLMRequestStream returned error: %v", err)
	}
	if got := streamed.String(); got != "" {
		t.Fatalf("stream leaked bare JSON tool call: %q", got)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices len = %d, want 1", len(resp.Choices))
	}
	choice := resp.Choices[0]
	if choice.FinishReason != "tool_calls" {
		t.Fatalf("finish_reason = %q, want tool_calls", choice.FinishReason)
	}
	if choice.Message.Content != "" {
		t.Fatalf("content = %q, want empty after tool conversion", choice.Message.Content)
	}
	if len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("tool_calls len = %d, want 1", len(choice.Message.ToolCalls))
	}
	call := choice.Message.ToolCalls[0]
	if call.Function.Name != "bash" {
		t.Fatalf("tool name = %q, want bash", call.Function.Name)
	}
	if call.Function.Arguments != `{"command":"dir"}` {
		t.Fatalf("tool arguments = %s, want command dir", call.Function.Arguments)
	}
}
