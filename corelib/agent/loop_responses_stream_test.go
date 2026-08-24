package agent

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

type agentResponsesReadErrorBody struct {
	data []byte
	err  error
	done bool
}

func (r *agentResponsesReadErrorBody) Read(p []byte) (int, error) {
	if r.done {
		return 0, r.err
	}
	r.done = true
	return copy(p, r.data), nil
}

func (r *agentResponsesReadErrorBody) Close() error { return nil }

type agentResponsesRoundTripper func(*http.Request) (*http.Response, error)

func (f agentResponsesRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDoLLMRequestWithToolsStreamForwardsResponsesReasoning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.reasoning_summary_text.delta\n" +
			"data: {\"delta\":\"Check context.\"}\n\n" +
			"event: response.output_text.delta\n" +
			"data: {\"delta\":\"Answer.\"}\n\n" +
			"event: response.completed\n" +
			"data: {\"response\":{}}\n\n"))
	}))
	defer srv.Close()

	var tokens []string
	response, err := doLLMRequestWithToolsStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Model: "test", WireAPI: "responses"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "test"}},
		nil,
		srv.Client(),
		func(token string) { tokens = append(tokens, token) },
	)
	if err != nil {
		t.Fatalf("doLLMRequestWithToolsStream: %v", err)
	}
	if got, want := strings.Join(tokens, "|"), "\x01Check context.|Answer."; got != want {
		t.Fatalf("tokens = %q, want %q", got, want)
	}
	if response == nil || len(response.Choices) != 1 {
		t.Fatalf("unexpected response: %#v", response)
	}
	if got, want := response.Choices[0].Message.ReasoningContent, "Check context."; got != want {
		t.Fatalf("reasoning = %q, want %q", got, want)
	}
}

func TestDoLLMRequestWithToolsStreamDoesNotFallbackAfterPartialResponsesStream(t *testing.T) {
	readErr := errors.New("connection reset")
	attempts := 0
	client := &http.Client{Transport: agentResponsesRoundTripper(func(req *http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       &agentResponsesReadErrorBody{data: []byte("event: response.output_text.delta\ndata: {\"delta\":\"partial\"}\n\n"), err: readErr},
			Request:    req,
		}, nil
	})}

	var tokens []string
	response, err := doLLMRequestWithToolsStream(context.Background(), corelib.MaclawLLMConfig{URL: "https://example.test", Model: "test", WireAPI: "responses"}, nil, nil, client, func(token string) { tokens = append(tokens, token) })
	if !errors.Is(err, readErr) {
		t.Fatalf("error = %v, want wrapped %v", err, readErr)
	}
	if attempts != 1 {
		t.Fatalf("requests = %d, want 1 (no non-stream fallback after partial output)", attempts)
	}
	if response == nil || response.Choices[0].Message.Content != "partial" {
		t.Fatalf("partial response = %#v", response)
	}
	if got, want := strings.Join(tokens, ""), "partial"; got != want {
		t.Fatalf("tokens = %q, want %q", got, want)
	}
}

func TestDoLLMRequestWithToolsStreamDoesNotFallbackAfterPartialChatReasoning(t *testing.T) {
	readErr := errors.New("connection reset")
	attempts := 0
	client := &http.Client{Transport: agentResponsesRoundTripper(func(req *http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       &agentResponsesReadErrorBody{data: []byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"partial thought\"}}]}\n\n"), err: readErr},
			Request:    req,
		}, nil
	})}

	var tokens []string
	response, err := doLLMRequestWithToolsStream(context.Background(), corelib.MaclawLLMConfig{URL: "https://example.test", Model: "deepseek-v4-flash"}, nil, nil, client, func(token string) { tokens = append(tokens, token) })
	if !errors.Is(err, readErr) {
		t.Fatalf("error = %v, want wrapped %v", err, readErr)
	}
	if attempts != 1 {
		t.Fatalf("requests = %d, want 1 (no non-stream fallback after partial reasoning)", attempts)
	}
	if response == nil || response.Choices[0].Message.ReasoningContent != "partial thought" {
		t.Fatalf("partial response = %#v", response)
	}
	if got, want := strings.Join(tokens, ""), "\x01partial thought"; got != want {
		t.Fatalf("tokens = %q, want %q", got, want)
	}
}

func TestDoLLMRequestWithToolsStreamForwardsChatReasoningWhileStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"Inspect request.\"}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{\"content\":\"Final answer.\"},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: [DONE]\n\n"))
	}))
	defer srv.Close()

	var tokens []string
	response, err := doLLMRequestWithToolsStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Model: "deepseek-v4-flash"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "test"}},
		nil,
		srv.Client(),
		func(token string) { tokens = append(tokens, token) },
	)
	if err != nil {
		t.Fatalf("doLLMRequestWithToolsStream: %v", err)
	}
	if got, want := strings.Join(tokens, "|"), "\x01Inspect request.|Final answer."; got != want {
		t.Fatalf("tokens = %q, want %q", got, want)
	}
	if response == nil || response.Choices[0].Message.ReasoningContent != "Inspect request." {
		t.Fatalf("response = %#v", response)
	}
}

func TestDoLLMRequestWithToolsStreamForwardsAnthropicThinking(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`event: message_start` + "\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","model":"glm-5.3","content":[],"stop_reason":null,"usage":{"input_tokens":3,"output_tokens":0}}}` + "\n\n"))
		_, _ = w.Write([]byte(`event: content_block_start` + "\n"))
		_, _ = w.Write([]byte(`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}` + "\n\n"))
		_, _ = w.Write([]byte(`event: content_block_delta` + "\n"))
		_, _ = w.Write([]byte(`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Check context."}}` + "\n\n"))
		_, _ = w.Write([]byte(`event: content_block_start` + "\n"))
		_, _ = w.Write([]byte(`data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}` + "\n\n"))
		_, _ = w.Write([]byte(`event: content_block_delta` + "\n"))
		_, _ = w.Write([]byte(`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Answer."}}` + "\n\n"))
		_, _ = w.Write([]byte(`event: message_delta` + "\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}` + "\n\n"))
		_, _ = w.Write([]byte(`event: message_stop` + "\n"))
		_, _ = w.Write([]byte(`data: {"type":"message_stop"}` + "\n\n"))
	}))
	defer srv.Close()

	var tokens []string
	response, err := doLLMRequestWithToolsStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Model: "glm-5.3", Protocol: "anthropic", Key: "test-key"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "test"}},
		nil,
		srv.Client(),
		func(token string) { tokens = append(tokens, token) },
	)
	if err != nil {
		t.Fatalf("doLLMRequestWithToolsStream: %v", err)
	}
	if got, want := strings.Join(tokens, "|"), "\x01Check context.|Answer."; got != want {
		t.Fatalf("tokens = %q, want %q", got, want)
	}
	if response == nil || response.Choices[0].Message.ReasoningContent != "Check context." {
		t.Fatalf("response = %#v", response)
	}
}

func TestDoLLMRequestWithToolsStreamDoesNotFallbackAfterPartialAnthropicThinking(t *testing.T) {
	readErr := errors.New("connection reset")
	attempts := 0
	payload := "event: message_start\n" +
		`data: {"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","model":"glm-5.3","content":[],"stop_reason":null,"usage":{"input_tokens":3,"output_tokens":0}}}` + "\n\n" +
		"event: content_block_start\n" +
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}` + "\n\n" +
		"event: content_block_delta\n" +
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"partial thought"}}` + "\n\n"
	client := &http.Client{Transport: agentResponsesRoundTripper(func(req *http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       &agentResponsesReadErrorBody{data: []byte(payload), err: readErr},
			Request:    req,
		}, nil
	})}

	var tokens []string
	response, err := doLLMRequestWithToolsStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: "https://example.test", Model: "glm-5.3", Protocol: "anthropic", Key: "test-key"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "test"}},
		nil,
		client,
		func(token string) { tokens = append(tokens, token) },
	)
	if !errors.Is(err, readErr) {
		t.Fatalf("error = %v, want wrapped %v", err, readErr)
	}
	if attempts != 1 {
		t.Fatalf("requests = %d, want 1 (no non-stream fallback after partial thinking)", attempts)
	}
	if response == nil || response.Choices[0].Message.ReasoningContent != "partial thought" {
		t.Fatalf("partial response = %#v", response)
	}
	if got, want := strings.Join(tokens, ""), "\x01partial thought"; got != want {
		t.Fatalf("tokens = %q, want %q", got, want)
	}
}

func TestDoLLMRequestWithToolsStreamFlushesShortPartialChatText(t *testing.T) {
	readErr := errors.New("connection reset")
	client := &http.Client{Transport: agentResponsesRoundTripper(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       &agentResponsesReadErrorBody{data: []byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"), err: readErr},
			Request:    req,
		}, nil
	})}

	var tokens []string
	response, err := doLLMRequestWithToolsStream(context.Background(), corelib.MaclawLLMConfig{URL: "https://example.test", Model: "deepseek-v4-flash"}, nil, nil, client, func(token string) { tokens = append(tokens, token) })
	if !errors.Is(err, readErr) {
		t.Fatalf("error = %v, want wrapped %v", err, readErr)
	}
	if response == nil || response.Choices[0].Message.Content != "partial" {
		t.Fatalf("partial response = %#v", response)
	}
	if got, want := strings.Join(tokens, ""), "partial"; got != want {
		t.Fatalf("tokens = %q, want %q", got, want)
	}
}

var _ io.ReadCloser = (*agentResponsesReadErrorBody)(nil)
