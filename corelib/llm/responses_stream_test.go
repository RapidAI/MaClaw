package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

type responsesStreamReadErrorBody struct {
	data []byte
	err  error
	done bool
}

func (r *responsesStreamReadErrorBody) Read(p []byte) (int, error) {
	if r.done {
		return 0, r.err
	}
	r.done = true
	return copy(p, r.data), nil
}

func (r *responsesStreamReadErrorBody) Close() error { return nil }

type responsesStreamRoundTripper func(*http.Request) (*http.Response, error)

func (f responsesStreamRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDoResponsesAPIRequestStreamEmitsReasoningBeforeText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if got := request["stream"]; got != true {
			t.Fatalf("stream = %#v, want true", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.reasoning_summary_text.delta\n" +
			"data: {\"delta\":\"Inspect inputs. \"}\n\n" +
			"event: response.reasoning_summary_text.delta\n" +
			"data: {\"delta\":\"Plan answer.\"}\n\n" +
			"event: response.output_text.delta\n" +
			"data: {\"delta\":\"Done.\"}\n\n" +
			"event: response.completed\n" +
			"data: {\"response\":{\"id\":\"resp-stream-1\",\"output\":[{\"type\":\"reasoning\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"Inspect inputs. Plan answer.\"}]}]}}\n\n"))
	}))
	defer srv.Close()

	var events []string
	response, err := DoResponsesAPIRequestStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Model: "test", WireAPI: "responses"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "test"}},
		nil,
		srv.Client(),
		func(delta string) { events = append(events, "text:"+delta) },
		func(delta string) { events = append(events, "reasoning:"+delta) },
	)
	if err != nil {
		t.Fatalf("DoResponsesAPIRequestStream: %v", err)
	}
	if response.ResponseID != "resp-stream-1" {
		t.Fatalf("response ID = %q, want provider response.completed ID", response.ResponseID)
	}
	if got, want := strings.Join(events, "|"), "reasoning:Inspect inputs. |reasoning:Plan answer.|text:Done."; got != want {
		t.Fatalf("events = %q, want %q", got, want)
	}
	if response == nil || len(response.Choices) != 1 {
		t.Fatalf("unexpected response: %#v", response)
	}
	message := response.Choices[0].Message
	if message.ReasoningContent != "Inspect inputs. Plan answer." {
		t.Fatalf("reasoning = %q", message.ReasoningContent)
	}
	if message.Content != "Done." {
		t.Fatalf("content = %q", message.Content)
	}
}

func TestDoResponsesAPIRequestStreamReturnsToolsFromCompletedItems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.output_item.added\n" +
			"data: {\"output_index\":0,\"item\":{\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"read_file\"}}\n\n" +
			"event: response.function_call_arguments.delta\n" +
			"data: {\"output_index\":0,\"delta\":\"{\\\"path\\\":\\\"a.txt\\\"}\"}\n\n" +
			"event: response.completed\n" +
			"data: {\"response\":{}}\n\n"))
	}))
	defer srv.Close()

	response, err := DoResponsesAPIRequestStream(context.Background(), corelib.MaclawLLMConfig{URL: srv.URL, Model: "test", WireAPI: "responses"}, nil, nil, srv.Client(), nil, nil)
	if err != nil {
		t.Fatalf("DoResponsesAPIRequestStream: %v", err)
	}
	if response.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("finish reason = %q", response.Choices[0].FinishReason)
	}
	calls := response.Choices[0].Message.ToolCalls
	if len(calls) != 1 || calls[0].ID != "call_1" || calls[0].Function.Name != "read_file" || calls[0].Function.Arguments != `{"path":"a.txt"}` {
		t.Fatalf("tool calls = %#v", calls)
	}
}

func TestDoResponsesAPIRequestStreamRejectsConflictingResponseIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.created\n" +
			"data: {\"response\":{\"id\":\"resp-first\"}}\n\n" +
			"event: response.completed\n" +
			"data: {\"response\":{\"id\":\"resp-second\",\"output\":[]}}\n\n"))
	}))
	defer srv.Close()

	_, err := DoResponsesAPIRequestStream(context.Background(), corelib.MaclawLLMConfig{URL: srv.URL, Model: "test", WireAPI: "responses"}, nil, nil, srv.Client(), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "response ID changed") {
		t.Fatalf("error = %v, want conflicting provider response ID failure", err)
	}
}

func TestDoResponsesAPIRequestStreamFallsBackToCompletedOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.completed\n" +
			"data: {\"response\":{\"output\":[" +
			"{\"type\":\"reasoning\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"Checked inputs.\"}]}," +
			"{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"Final answer.\"}]}]}}\n\n"))
	}))
	defer srv.Close()

	var events []string
	response, err := DoResponsesAPIRequestStream(context.Background(), corelib.MaclawLLMConfig{URL: srv.URL, Model: "test", WireAPI: "responses"}, nil, nil, srv.Client(),
		func(delta string) { events = append(events, "text:"+delta) },
		func(delta string) { events = append(events, "reasoning:"+delta) },
	)
	if err != nil {
		t.Fatalf("DoResponsesAPIRequestStream: %v", err)
	}
	if got, want := strings.Join(events, "|"), "reasoning:Checked inputs.|text:Final answer."; got != want {
		t.Fatalf("events = %q, want %q", got, want)
	}
	if got, want := response.Choices[0].Message.Content, "Final answer."; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestDoResponsesAPIRequestStreamFallsBackToCompletedToolCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.completed\n" +
			"data: {\"response\":{\"output\":[{\"type\":\"function_call\",\"call_id\":\"call_done\",\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\\\"done.txt\\\"}\"}]}}\n\n"))
	}))
	defer srv.Close()

	response, err := DoResponsesAPIRequestStream(context.Background(), corelib.MaclawLLMConfig{URL: srv.URL, Model: "test", WireAPI: "responses"}, nil, nil, srv.Client(), nil, nil)
	if err != nil {
		t.Fatalf("DoResponsesAPIRequestStream: %v", err)
	}
	choice := response.Choices[0]
	if choice.FinishReason != "tool_calls" || len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("unexpected choice: %#v", choice)
	}
	call := choice.Message.ToolCalls[0]
	if call.ID != "call_done" || call.Function.Name != "read_file" || call.Function.Arguments != `{"path":"done.txt"}` {
		t.Fatalf("tool call = %#v", call)
	}
}

func TestDoResponsesAPIRequestStreamCompletedToolDoesNotDiscardDeltas(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.output_item.added\n" +
			"data: {\"output_index\":2,\"item\":{\"type\":\"function_call\",\"call_id\":\"call_keep\",\"name\":\"read_file\"}}\n\n" +
			"event: response.function_call_arguments.delta\n" +
			"data: {\"output_index\":2,\"delta\":\"{\\\"path\\\":\\\"full.txt\\\"}\"}\n\n" +
			"event: response.completed\n" +
			"data: {\"response\":{\"output\":[{\"type\":\"message\",\"content\":[]},{\"type\":\"reasoning\",\"summary\":[]},{\"type\":\"function_call\",\"call_id\":\"call_keep\",\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\"}]}}\n\n"))
	}))
	defer srv.Close()

	response, err := DoResponsesAPIRequestStream(context.Background(), corelib.MaclawLLMConfig{URL: srv.URL, Model: "test", WireAPI: "responses"}, nil, nil, srv.Client(), nil, nil)
	if err != nil {
		t.Fatalf("DoResponsesAPIRequestStream: %v", err)
	}
	calls := response.Choices[0].Message.ToolCalls
	if len(calls) != 1 || calls[0].Function.Arguments != `{"path":"full.txt"}` {
		t.Fatalf("tool calls = %#v", calls)
	}
}

func TestDoResponsesAPIRequestStreamParsesSeparatorlessEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.output_text.delta\n" +
			"data: {\"delta\":\"One\"}\n" +
			"event: response.output_text.delta\n" +
			"data: {\"delta\":\" two\"}\n" +
			"event: response.completed\n" +
			"data: {\"response\":{}}\n"))
	}))
	defer srv.Close()

	response, err := DoResponsesAPIRequestStream(context.Background(), corelib.MaclawLLMConfig{URL: srv.URL, Model: "test", WireAPI: "responses"}, nil, nil, srv.Client(), nil, nil)
	if err != nil {
		t.Fatalf("DoResponsesAPIRequestStream: %v", err)
	}
	if got, want := response.Choices[0].Message.Content, "One two"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestDoResponsesAPIRequestStreamReturnsPartialResponseOnReadFailure(t *testing.T) {
	readErr := errors.New("connection reset")
	client := &http.Client{Transport: responsesStreamRoundTripper(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       &responsesStreamReadErrorBody{data: []byte("event: response.output_text.delta\ndata: {\"delta\":\"partial\"}\n\n"), err: readErr},
			Request:    req,
		}, nil
	})}

	response, err := DoResponsesAPIRequestStream(context.Background(), corelib.MaclawLLMConfig{URL: "https://example.test", Model: "test", WireAPI: "responses"}, nil, nil, client, nil, nil)
	if !errors.Is(err, readErr) {
		t.Fatalf("error = %v, want wrapped %v", err, readErr)
	}
	if response == nil || len(response.Choices) != 1 || response.Choices[0].Message.Content != "partial" {
		t.Fatalf("partial response = %#v", response)
	}
}

var _ io.ReadCloser = (*responsesStreamReadErrorBody)(nil)

func TestDoResponsesAPIRequestStreamReturnsPartialResponseOnFailedEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.reasoning_summary_text.delta\n" +
			"data: {\"delta\":\"Thinking about inputs.\"}\n\n" +
			"event: response.output_text.delta\n" +
			"data: {\"delta\":\"partial answer\"}\n\n" +
			"event: response.failed\n" +
			"data: {\"response\":{\"id\":\"resp-failed\",\"error\":{\"message\":\"provider overloaded\",\"code\":\"overloaded\"}}}\n\n"))
	}))
	defer srv.Close()

	response, err := DoResponsesAPIRequestStream(context.Background(), corelib.MaclawLLMConfig{URL: srv.URL, Model: "test", WireAPI: "responses"}, nil, nil, srv.Client(), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "provider overloaded") {
		t.Fatalf("error = %v, want provider failure", err)
	}
	if response == nil || len(response.Choices) != 1 {
		t.Fatalf("partial response = %#v", response)
	}
	message := response.Choices[0].Message
	if message.ReasoningContent != "Thinking about inputs." {
		t.Fatalf("reasoning = %q, want accumulated reasoning preserved", message.ReasoningContent)
	}
	if message.Content != "partial answer" {
		t.Fatalf("content = %q, want accumulated text preserved", message.Content)
	}
	if response.ResponseID != "resp-failed" {
		t.Fatalf("response ID = %q, want provider-issued ID preserved", response.ResponseID)
	}
}

func TestDoResponsesAPIRequestStreamReturnsPartialResponseOnOversizedToolArguments(t *testing.T) {
	oversized := strings.Repeat("x", MaxToolArgumentsBytes+1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.reasoning_summary_text.delta\n" +
			"data: {\"delta\":\"Preparing write.\"}\n\n" +
			"event: response.output_item.added\n" +
			"data: {\"output_index\":0,\"item\":{\"type\":\"function_call\",\"call_id\":\"call_big\",\"name\":\"write_file\"}}\n\n" +
			"event: response.function_call_arguments.delta\n" +
			"data: {\"output_index\":0,\"delta\":\"" + oversized + "\"}\n\n"))
	}))
	defer srv.Close()

	response, err := DoResponsesAPIRequestStream(context.Background(), corelib.MaclawLLMConfig{URL: srv.URL, Model: "test", WireAPI: "responses"}, nil, nil, srv.Client(), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "tool arguments too large") {
		t.Fatalf("error = %v, want oversized tool arguments failure", err)
	}
	if response == nil || len(response.Choices) != 1 {
		t.Fatalf("partial response = %#v", response)
	}
	if got := response.Choices[0].Message.ReasoningContent; got != "Preparing write." {
		t.Fatalf("reasoning = %q, want accumulated reasoning preserved", got)
	}
}

func TestDoResponsesAPIRequestStreamHandlesReasoningSummaryPartAdded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.reasoning_summary_part.added\n" +
			"data: {\"part\":{\"type\":\"summary_text\",\"text\":\"Whole summary at once.\"}}\n\n" +
			"event: response.reasoning_summary_text.done\n" +
			"data: {\"text\":\"Whole summary at once.\"}\n\n" +
			"event: response.completed\n" +
			"data: {\"response\":{}}\n\n"))
	}))
	defer srv.Close()

	var reasoning []string
	response, err := DoResponsesAPIRequestStream(context.Background(), corelib.MaclawLLMConfig{URL: srv.URL, Model: "test", WireAPI: "responses"}, nil, nil, srv.Client(), nil,
		func(delta string) { reasoning = append(reasoning, delta) })
	if err != nil {
		t.Fatalf("DoResponsesAPIRequestStream: %v", err)
	}
	// The done event repeats the same summary; the dedup in the append helper
	// must prevent it from being emitted twice.
	if got, want := strings.Join(reasoning, "|"), "Whole summary at once."; got != want {
		t.Fatalf("reasoning events = %q, want %q", got, want)
	}
	if got := response.Choices[0].Message.ReasoningContent; got != "Whole summary at once." {
		t.Fatalf("reasoning = %q", got)
	}
}

func TestDoResponsesAPIRequestStreamAcceptsThinkingSummaryPart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.completed\n" +
			"data: {\"response\":{\"output\":[{\"type\":\"reasoning\",\"summary\":[{\"type\":\"thinking\",\"text\":\"Thought it through.\"}]}]}}\n\n"))
	}))
	defer srv.Close()

	response, err := DoResponsesAPIRequestStream(context.Background(), corelib.MaclawLLMConfig{URL: srv.URL, Model: "test", WireAPI: "responses"}, nil, nil, srv.Client(), nil, nil)
	if err != nil {
		t.Fatalf("DoResponsesAPIRequestStream: %v", err)
	}
	if got := response.Choices[0].Message.ReasoningContent; got != "Thought it through." {
		t.Fatalf("reasoning = %q, want thinking part captured", got)
	}
}

func TestResponsesStreamOutlivesRouteDeadlineAfterHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"delta\":\"hello \"}\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(350 * time.Millisecond):
		}
		_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"delta\":\"there\"}\n\n")
		_, _ = io.WriteString(w, "event: response.completed\ndata: {\"response\":{\"id\":\"resp-long\",\"status\":\"completed\"}}\n\n")
	}))
	defer server.Close()

	parent, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	deadline, _ := parent.Deadline()
	parent = WithOwnedRouteDeadline(parent, deadline)

	resp, err := DoResponsesAPIRequestStream(
		parent,
		corelib.MaclawLLMConfig{URL: server.URL, Model: "test", WireAPI: "responses"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		nil,
		server.Client(),
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("stream error = %v, want the body to finish after the route deadline", err)
	}
	if resp == nil || len(resp.Choices) == 0 {
		t.Fatal("missing response")
	}
	if got := resp.Choices[0].Message.Content; got != "hello there" {
		t.Fatalf("content = %q, want hello there", got)
	}
}

func TestResponsesStreamRouteDeadlineStillBoundsHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer func() {
		server.CloseClientConnections()
		server.Close()
	}()

	parent, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	deadline, _ := parent.Deadline()
	parent = WithOwnedRouteDeadline(parent, deadline)

	started := time.Now()
	_, err := DoResponsesAPIRequestStream(
		parent,
		corelib.MaclawLLMConfig{URL: server.URL, Model: "test", WireAPI: "responses"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		nil,
		server.Client(),
		nil,
		nil,
	)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("elapsed = %s, want the route deadline rather than an open body", elapsed)
	}
}

func TestResponsesStreamStopAbortsAfterHeaders(t *testing.T) {
	cancelParent, stopTurn := context.WithCancel(context.Background())
	defer stopTurn()
	route, cancelRoute := context.WithTimeout(cancelParent, 2*time.Second)
	defer cancelRoute()
	deadline, _ := route.Deadline()
	route = BindRouteStreamDeadline(route, deadline, cancelParent)

	headersSent := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"delta\":\"hello \"}\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		close(headersSent)
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer func() {
		server.CloseClientConnections()
		server.Close()
	}()

	go func() {
		select {
		case <-headersSent:
		case <-time.After(time.Second):
			return
		}
		stopTurn()
	}()

	started := time.Now()
	_, err := DoResponsesAPIRequestStream(
		route,
		corelib.MaclawLLMConfig{URL: server.URL, Model: "test", WireAPI: "responses"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		nil,
		server.Client(),
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("stop after headers returned a finished stream")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("elapsed = %s, want the stop to abort the body", elapsed)
	}
}

func TestResponsesStreamWrapperCancelAbortsAfterHeaders(t *testing.T) {
	cancelParent, stopTurn := context.WithCancel(context.Background())
	defer stopTurn()
	route, cancelRoute := context.WithTimeout(cancelParent, 2*time.Second)
	defer cancelRoute()
	deadline, _ := route.Deadline()
	route = BindRouteStreamDeadline(route, deadline, cancelParent)
	wrapper, cancelWrapper := context.WithCancel(route)
	defer cancelWrapper()

	headersSent := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"delta\":\"hello \"}\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		close(headersSent)
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer func() {
		server.CloseClientConnections()
		server.Close()
	}()

	go func() {
		select {
		case <-headersSent:
		case <-time.After(time.Second):
			return
		}
		cancelWrapper()
	}()

	started := time.Now()
	_, err := DoResponsesAPIRequestStream(
		wrapper,
		corelib.MaclawLLMConfig{URL: server.URL, Model: "test", WireAPI: "responses"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		nil,
		server.Client(),
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("wrapper cancel after headers returned a finished stream")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("elapsed = %s, want the wrapper cancel to abort the body", elapsed)
	}
	if cancelParent.Err() != nil {
		t.Fatal("wrapper cancel also stopped the turn")
	}
}
