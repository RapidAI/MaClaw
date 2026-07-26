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

func TestResponsesAPIStreamForwardsReasoningSummaryToThinkingChannel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "event: response.reasoning_summary_text.delta\n")
		_, _ = fmt.Fprint(w, `data: {"delta":"First inspect the request. "}`+"\n")
		_, _ = fmt.Fprint(w, "event: response.reasoning_summary_text.delta\n")
		_, _ = fmt.Fprint(w, `data: {"delta":"Then return the answer."}`+"\n")
		_, _ = fmt.Fprint(w, "event: response.output_text.delta\n")
		_, _ = fmt.Fprint(w, `data: {"delta":"Done."}`+"\n")
		_, _ = fmt.Fprint(w, "event: response.completed\n")
		_, _ = fmt.Fprint(w, `data: {"response":{"status":"completed"}}`+"\n")
	}))
	defer srv.Close()

	var streamed strings.Builder
	resp, err := (&IMMessageHandler{}).doResponsesAPILLMRequestStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Key: "test-key", Model: "test-model", Protocol: "openai", WireAPI: "responses"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "test"}},
		nil,
		srv.Client(),
		func(delta string) { streamed.WriteString(delta) },
		nil,
	)
	if err != nil {
		t.Fatalf("doResponsesAPILLMRequestStream returned error: %v", err)
	}
	if got, want := resp.Choices[0].Message.ReasoningContent, "First inspect the request. Then return the answer."; got != want {
		t.Fatalf("reasoning_content = %q, want %q", got, want)
	}
	if got := resp.Choices[0].Message.Content; got != "Done." {
		t.Fatalf("content = %q, want Done.", got)
	}
	if got := streamed.String(); !strings.Contains(got, "\x01First inspect the request. Then return the answer.") {
		t.Fatalf("reasoning summary was not sent to thinking channel: %q", got)
	}
}

func TestResponsesAPIStreamUsesFinalReasoningItemWhenDeltasAreAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "event: response.output_item.done\n")
		_, _ = fmt.Fprint(w, `data: {"item":{"type":"reasoning","summary":[{"type":"summary_text","text":"Use the final summary."}]}}`+"\n")
		_, _ = fmt.Fprint(w, "event: response.output_text.delta\n")
		_, _ = fmt.Fprint(w, `data: {"delta":"Done."}`+"\n")
		_, _ = fmt.Fprint(w, "event: response.completed\n")
		_, _ = fmt.Fprint(w, `data: {"response":{"status":"completed"}}`+"\n")
	}))
	defer srv.Close()

	var streamed strings.Builder
	resp, err := (&IMMessageHandler{}).doResponsesAPILLMRequestStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Key: "test-key", Model: "test-model", Protocol: "openai", WireAPI: "responses"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "test"}},
		nil,
		srv.Client(),
		func(delta string) { streamed.WriteString(delta) },
		nil,
	)
	if err != nil {
		t.Fatalf("doResponsesAPILLMRequestStream returned error: %v", err)
	}
	if got, want := resp.Choices[0].Message.ReasoningContent, "Use the final summary."; got != want {
		t.Fatalf("reasoning_content = %q, want %q", got, want)
	}
	if got := streamed.String(); !strings.Contains(got, "\x01Use the final summary.") {
		t.Fatalf("final reasoning summary was not sent to thinking channel: %q", got)
	}
}

func TestResponsesAPIStreamHandlesMultilineAndUntypedReasoningEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// The first event intentionally spans data lines and uses CRLF. The
		// second has no event field and relies on the payload type fallback.
		_, _ = fmt.Fprint(w, "event: response.reasoning_summary_text.delta\r\ndata: {\"delta\":\"First \",\r\ndata: \"provider\":\"test\"}\r\n\r\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.reasoning.delta\",\"delta\":\"Then answer.\"}\n\n")
		_, _ = fmt.Fprint(w, "event: response.output_text.delta\ndata: {\"delta\":\"Done.\"}\n\n")
		_, _ = fmt.Fprint(w, "event: response.completed\ndata: {\"response\":{\"status\":\"completed\"}}\n\n")
	}))
	defer srv.Close()

	var streamed strings.Builder
	resp, err := (&IMMessageHandler{}).doResponsesAPILLMRequestStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Key: "test-key", Model: "test-model", Protocol: "openai", WireAPI: "responses"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "test"}},
		nil,
		srv.Client(),
		func(delta string) { streamed.WriteString(delta) },
		nil,
	)
	if err != nil {
		t.Fatalf("doResponsesAPILLMRequestStream returned error: %v", err)
	}
	if got, want := resp.Choices[0].Message.ReasoningContent, "First Then answer."; got != want {
		t.Fatalf("reasoning_content = %q, want %q", got, want)
	}
	if got := streamed.String(); !strings.Contains(got, "\x01First Then answer.") {
		t.Fatalf("reasoning was not sent to thinking channel: %q", got)
	}
}

func TestResponsesAPIStreamDoesNotDuplicateFinalReasoningSummary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "event: response.reasoning_summary_text.delta\ndata: {\"delta\":\"Streamed summary.\"}\n\n")
		_, _ = fmt.Fprint(w, "event: response.output_item.done\ndata: {\"item\":{\"type\":\"reasoning_summary\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"Streamed summary."+"\"}]}}\n\n")
		_, _ = fmt.Fprint(w, "event: response.completed\ndata: {\"response\":{\"status\":\"completed\"}}\n\n")
	}))
	defer srv.Close()

	resp, err := (&IMMessageHandler{}).doResponsesAPILLMRequestStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Key: "test-key", Model: "test-model", Protocol: "openai", WireAPI: "responses"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "test"}},
		nil,
		srv.Client(),
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("doResponsesAPILLMRequestStream returned error: %v", err)
	}
	if got, want := resp.Choices[0].Message.ReasoningContent, "Streamed summary."; got != want {
		t.Fatalf("reasoning_content = %q, want one copy of %q", got, want)
	}
}

func TestResponsesAPIStreamCompletesPartialReasoningSummaryFromFinalItem(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "event: response.reasoning_summary_text.delta\ndata: {\"delta\":\"First \"}\n\n")
		_, _ = fmt.Fprint(w, "event: response.output_item.done\ndata: {\"item\":{\"type\":\"reasoning_summary\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"First answer."+"\"}]}}\n\n")
		_, _ = fmt.Fprint(w, "event: response.completed\ndata: {\"response\":{\"status\":\"completed\"}}\n\n")
	}))
	defer srv.Close()

	var streamed strings.Builder
	resp, err := (&IMMessageHandler{}).doResponsesAPILLMRequestStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Key: "test-key", Model: "test-model", Protocol: "openai", WireAPI: "responses"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "test"}},
		nil,
		srv.Client(),
		func(delta string) { streamed.WriteString(delta) },
		nil,
	)
	if err != nil {
		t.Fatalf("doResponsesAPILLMRequestStream returned error: %v", err)
	}
	if got, want := resp.Choices[0].Message.ReasoningContent, "First answer."; got != want {
		t.Fatalf("reasoning_content = %q, want %q", got, want)
	}
	if got := streamed.String(); !strings.Contains(got, "\x01First answer.") {
		t.Fatalf("completed reasoning summary was not streamed: %q", got)
	}
}

func TestResponsesAPIStreamUsesCompletedResponseReasoningSummaryFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "event: response.completed\ndata: {\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"reasoning\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"Completed summary."+"\"}]}]}}\n\n")
	}))
	defer srv.Close()

	var streamed strings.Builder
	resp, err := (&IMMessageHandler{}).doResponsesAPILLMRequestStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Key: "test-key", Model: "test-model", Protocol: "openai", WireAPI: "responses"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "test"}},
		nil,
		srv.Client(),
		func(delta string) { streamed.WriteString(delta) },
		nil,
	)
	if err != nil {
		t.Fatalf("doResponsesAPILLMRequestStream returned error: %v", err)
	}
	if got, want := resp.Choices[0].Message.ReasoningContent, "Completed summary."; got != want {
		t.Fatalf("reasoning_content = %q, want %q", got, want)
	}
	if got := streamed.String(); !strings.Contains(got, "\x01Completed summary.") {
		t.Fatalf("completed response summary was not streamed: %q", got)
	}
}
