package llm

import (
	"bufio"
	"bytes"
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

func TestDoResponsesAPIRequestStreamIncompleteSnapshotIsLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.incomplete\ndata: {\"response\":{\"status\":\"incomplete\",\"incomplete_details\":{\"reason\":\"max_output_tokens\"},\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"print('hi'\"}]}]}}\n\n")
	}))
	defer srv.Close()

	response, err := DoResponsesAPIRequestStream(context.Background(), corelib.MaclawLLMConfig{URL: srv.URL, Model: "test", WireAPI: "responses"}, nil, nil, srv.Client(), nil, nil)
	if err != nil {
		t.Fatalf("DoResponsesAPIRequestStream: %v", err)
	}
	if got := response.Choices[0].FinishReason; got != "length" {
		t.Fatalf("finish reason = %q, want length", got)
	}
	if got := response.Choices[0].Message.Content; got != "print('hi'" {
		t.Fatalf("content = %q", got)
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

// A provider may report output_index only in the stream and let response.output
// interleave reasoning and message items. The final snapshot used to be keyed by
// slice position, so its function_call merged onto the wrong item and the tool
// call lost the name its deltas had. A nameless call is dropped by the agent
// loop, and two of those in a row end the turn as "consecutive empty responses".
func TestResponsesCompletedToolCallsUseProviderOutputIndex(t *testing.T) {
	payload := `{"type":"response.completed","response":{"output":[
		{"type":"reasoning","summary":[{"type":"summary_text","text":"plan"}]},
		{"type":"message","content":[{"type":"output_text","text":"hi"}]},
		{"type":"function_call","output_index":2,"call_id":"call_a","name":"read_file","arguments":"{\"path\":\"a.go\"}"}
	]}}`
	items := responsesCompletedStreamToolCalls(payload)
	got := items[2]
	if got == nil {
		t.Fatalf("function_call was not keyed by its output_index: %v", items)
	}
	if got.name != "read_file" || got.callID != "call_a" || !strings.Contains(got.args.String(), "a.go") {
		t.Fatalf("snapshot item = %+v args=%q", got, got.args.String())
	}
	if _, wrong := items[1]; wrong {
		t.Fatalf("snapshot landed on the message position")
	}
}

// function_call_arguments.* can arrive without output_item.added. The delta path
// then creates an item that has arguments and no name; the final snapshot has
// the name, and the backfill is what stops it reaching the loop as a nameless
// tool call.
func TestResponsesStreamBackfillsToolNameMissingFromDeltas(t *testing.T) {
	items := map[int]*responsesStreamItem{}
	deltas := &responsesStreamItem{itemType: "function_call", callID: "call_a"}
	deltas.args.WriteString(`{"path":"a.go"}`)
	items[0] = deltas

	snapshot := map[int]*responsesStreamItem{
		0: {itemType: "function_call", callID: "call_a", name: "read_file"},
	}
	backfillResponsesStreamToolNames(items, snapshot)
	if got := items[0]; got.name != "read_file" {
		t.Fatalf("name was not backfilled: %+v", got)
	}
	if got := items[0].args.String(); got != `{"path":"a.go"}` {
		t.Fatalf("backfill changed the arguments: %q", got)
	}
}

func TestResponsesStreamBackfillMatchesOnCallIDAcrossIndices(t *testing.T) {
	items := map[int]*responsesStreamItem{}
	items[0] = &responsesStreamItem{itemType: "function_call", callID: "call_b"}
	items[1] = &responsesStreamItem{itemType: "function_call", callID: "call_a"}

	// The snapshot orders the calls the other way round.
	snapshot := map[int]*responsesStreamItem{
		0: {itemType: "function_call", callID: "call_a", name: "write_file"},
		1: {itemType: "function_call", callID: "call_b", name: "read_file"},
	}
	backfillResponsesStreamToolNames(items, snapshot)
	if items[0].name != "read_file" || items[1].name != "write_file" {
		t.Fatalf("call ids were not used: %q %q", items[0].name, items[1].name)
	}
}

func TestResponsesStreamBackfillKeepsExistingNameAndIgnoresEmptySnapshot(t *testing.T) {
	items := map[int]*responsesStreamItem{
		0: {itemType: "function_call", callID: "call_a", name: "read_file"},
		1: {itemType: "function_call", callID: "call_b"},
	}
	snapshot := map[int]*responsesStreamItem{
		0: {itemType: "function_call", callID: "call_a", name: "write_file"},
		1: {itemType: "function_call", callID: "call_b"},
	}
	backfillResponsesStreamToolNames(items, snapshot)
	if items[0].name != "read_file" {
		t.Fatalf("an existing name was replaced: %q", items[0].name)
	}
	if items[1].name != "" {
		t.Fatalf("a nameless snapshot invented a name: %q", items[1].name)
	}
}

func TestResponsesStreamBackfillDoesNotBorrowNeighbourWhenCallIDMisses(t *testing.T) {
	items := map[int]*responsesStreamItem{
		0: {itemType: "function_call", callID: "call_missing"},
		1: {itemType: "function_call"},
	}
	snapshot := map[int]*responsesStreamItem{
		0: {itemType: "function_call", callID: "call_other", name: "write_file"},
		1: {itemType: "function_call", callID: "call_b", name: "read_file"},
	}
	backfillResponsesStreamToolNames(items, snapshot)
	if items[0].name != "" {
		t.Fatalf("unmatched call id borrowed the neighbour name: %q", items[0].name)
	}
	if items[1].name != "" {
		t.Fatalf("blank call id borrowed a named neighbour: %q", items[1].name)
	}
}

func TestResponsesStreamArgumentDeltaWithoutIndexDoesNotClobberNeighbour(t *testing.T) {
	items := map[int]*responsesStreamItem{
		0: {itemType: "function_call", itemID: "fc_named", callID: "call_named", name: "read_file"},
	}
	item := responsesStreamItemForEvent(items, 0, false, "fc_other")
	item.args.WriteString(`{"path":"b.go"}`)
	if items[0].name != "read_file" || items[0].args.Len() != 0 {
		t.Fatalf("missing output_index wrote onto the neighbour: %#v args=%q", items[0], items[0].args.String())
	}
	if item.itemID != "fc_other" || item.args.String() != `{"path":"b.go"}` {
		t.Fatalf("unindexed delta was not parked on its own item: %#v", item)
	}
}

func TestResponsesStreamCompletedWithoutIndexKeepsSingleCallName(t *testing.T) {
	items := map[int]*responsesStreamItem{
		3: {itemType: "function_call", itemID: "fc_1"},
	}
	items[3].args.WriteString(`{"path":"a.go"}`)
	snapshot := map[int]*responsesStreamItem{
		-1: {itemType: "function_call", itemID: "fc_1", callID: "call_1", name: "read_file"},
	}
	slot := responsesStreamSnapshotSlot(items, -1, snapshot[-1], 1)
	items[slot] = mergeResponsesStreamToolItem(items[slot], snapshot[-1])
	if items[3].name != "read_file" || items[3].callID != "call_1" {
		t.Fatalf("single call was not named from the id-matched snapshot: %#v", items[3])
	}
	if _, collided := items[-1]; collided {
		t.Fatal("snapshot occupied a second slot")
	}
}

// response.completed carries every output item of the turn in ONE event, each
// with its full argument string. With several large calls the line passes the
// old 256 KiB scan limit, and the turn failed with "bufio.Scanner: token too
// long" before any of it reached the caller. The streaming round under review
// (BR-20261123-600B21AFE72D) failed exactly that way.
func TestDoResponsesAPIRequestStreamAcceptsLargeCompletedSnapshot(t *testing.T) {
	// Three calls, each well under MaxToolArgumentsBytes, together far past the
	// old per-event limit.
	blob := strings.Repeat("x", 120*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		args := `{"path":"a.go","pad":"` + blob + `"}`
		completed := map[string]interface{}{
			"response": map[string]interface{}{
				"id": "resp-big",
				"output": []interface{}{
					map[string]interface{}{"type": "reasoning", "summary": []interface{}{map[string]interface{}{"type": "summary_text", "text": "plan"}}},
					map[string]interface{}{"type": "function_call", "output_index": 1, "call_id": "call_a", "name": "read_file", "arguments": args},
					map[string]interface{}{"type": "function_call", "output_index": 2, "call_id": "call_b", "name": "write_file", "arguments": args},
					map[string]interface{}{"type": "function_call", "output_index": 3, "call_id": "call_c", "name": "edit_file", "arguments": args},
				},
			},
		}
		payload, err := json.Marshal(completed)
		if err != nil {
			t.Fatalf("marshal completed: %v", err)
		}
		_, _ = w.Write([]byte("event: response.completed\ndata: " + string(payload) + "\n\n"))
	}))
	defer srv.Close()

	response, err := DoResponsesAPIRequestStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Model: "test", WireAPI: "responses"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "test"}},
		nil,
		srv.Client(),
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("DoResponsesAPIRequestStream: %v", err)
	}
	if response == nil || len(response.Choices) != 1 {
		t.Fatalf("unexpected response: %#v", response)
	}
	calls := response.Choices[0].Message.ToolCalls
	if len(calls) != 3 {
		t.Fatalf("tool calls = %d, want 3", len(calls))
	}
	if calls[0].Function.Name != "read_file" || calls[1].Function.Name != "write_file" || calls[2].Function.Name != "edit_file" {
		t.Fatalf("names = %q %q %q", calls[0].Function.Name, calls[1].Function.Name, calls[2].Function.Name)
	}
	if !strings.Contains(calls[0].Function.Arguments, blob[:64]) {
		t.Fatalf("arguments were not carried through the larger event")
	}
}

// Argument deltas for two calls can arrive interleaved (A1, B1, A2, B2).
// output_index is what keeps them apart; a missing index used to append the
// next fragment onto whichever call sat at 0.
func TestDoResponsesAPIRequestStreamKeepsInterleavedArgumentDeltas(t *testing.T) {
	body := responsesStreamEvents(
		responsesStreamEvent("response.output_item.added", map[string]interface{}{
			"output_index": 0,
			"item":         map[string]interface{}{"type": "function_call", "id": "fc_a", "call_id": "call_a", "name": "read_file"},
		}),
		responsesStreamEvent("response.output_item.added", map[string]interface{}{
			"output_index": 1,
			"item":         map[string]interface{}{"type": "function_call", "id": "fc_b", "call_id": "call_b", "name": "write_file"},
		}),
		responsesStreamEvent("response.function_call_arguments.delta", map[string]interface{}{
			"output_index": 0, "item_id": "fc_a", "delta": `{"path":"a.go"`,
		}),
		responsesStreamEvent("response.function_call_arguments.delta", map[string]interface{}{
			"output_index": 1, "item_id": "fc_b", "delta": `{"path":"b.go"`,
		}),
		responsesStreamEvent("response.function_call_arguments.delta", map[string]interface{}{
			"output_index": 0, "item_id": "fc_a", "delta": `,"line":1}`,
		}),
		responsesStreamEvent("response.function_call_arguments.delta", map[string]interface{}{
			"output_index": 1, "item_id": "fc_b", "delta": `,"content":"x"}`,
		}),
		responsesStreamEvent("response.completed", map[string]interface{}{
			"response": map[string]interface{}{"output": []interface{}{
				map[string]interface{}{"type": "function_call", "id": "fc_a", "call_id": "call_a", "name": "read_file", "arguments": `{"path":"a.go","line":1}`},
				map[string]interface{}{"type": "function_call", "id": "fc_b", "call_id": "call_b", "name": "write_file", "arguments": `{"path":"b.go","content":"x"}`},
			}},
		}),
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	response, err := DoResponsesAPIRequestStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Model: "test", WireAPI: "responses"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "test"}},
		nil,
		srv.Client(),
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("DoResponsesAPIRequestStream: %v", err)
	}
	if response == nil || len(response.Choices) != 1 {
		t.Fatalf("unexpected response: %#v", response)
	}
	calls := response.Choices[0].Message.ToolCalls
	if len(calls) != 2 {
		t.Fatalf("tool calls = %#v, want 2", calls)
	}
	if calls[0].ID != "call_a" || calls[0].Function.Name != "read_file" || calls[0].Function.Arguments != `{"path":"a.go","line":1}` {
		t.Fatalf("call A = %+v", calls[0])
	}
	if calls[1].ID != "call_b" || calls[1].Function.Name != "write_file" || calls[1].Function.Arguments != `{"path":"b.go","content":"x"}` {
		t.Fatalf("call B = %+v", calls[1])
	}
}

func responsesStreamEvent(eventType string, payload map[string]interface{}) []byte {
	raw, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return []byte("event: " + eventType + "\ndata: " + string(raw) + "\n\n")
}

func responsesStreamEvents(parts ...[]byte) []byte {
	return bytes.Join(parts, nil)
}

// A later event can still exceed the scan limit (a snapshot of more calls than
// 4 MiB holds). That must not erase the argument deltas already accepted, or
// the turn looks like a consecutive empty response.
func TestDoResponsesAPIRequestStreamKeepsDeltasWhenLaterEventExceedsScanLimit(t *testing.T) {
	body := responsesStreamEvents(
		responsesStreamEvent("response.output_item.added", map[string]interface{}{
			"output_index": 0,
			"item":         map[string]interface{}{"type": "function_call", "id": "fc_a", "call_id": "call_a", "name": "read_file"},
		}),
		responsesStreamEvent("response.function_call_arguments.delta", map[string]interface{}{
			"output_index": 0, "item_id": "fc_a", "delta": `{"path":"a.go"}`,
		}),
	)
	body = append(body, []byte("event: response.completed\ndata: {\"pad\":\"")...)
	body = append(body, bytes.Repeat([]byte("y"), responsesStreamMaxEventBytes)...)
	body = append(body, []byte("\"}\n\n")...)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	response, err := DoResponsesAPIRequestStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Model: "test", WireAPI: "responses"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "test"}},
		nil,
		srv.Client(),
		nil,
		nil,
	)
	got := ""
	if response != nil && len(response.Choices) > 0 {
		got = response.Choices[0].Message.Content
	}
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error=%v body=%d content=%q", err, len(body), got)
	}
	if response == nil || len(response.Choices) != 1 {
		t.Fatalf("partial response = %#v", response)
	}
	calls := response.Choices[0].Message.ToolCalls
	if len(calls) != 1 || calls[0].Function.Name != "read_file" || calls[0].Function.Arguments != `{"path":"a.go"}` {
		t.Fatalf("kept arguments = %#v", calls)
	}
}

// A complete event can sit in the same full scanner buffer as the oversized
// tail. Scanner asks the split function for a token before it reports
// ErrTooLong, and a zero advance drops that event. The kept call must
// survive; only the tail that cannot fit is the failure.
func TestDoResponsesAPIRequestStreamKeepsCompletedEventBeforeOversizedTail(t *testing.T) {
	const maxEvent = 64 * 1024
	saved := responsesStreamMaxEventBytes
	responsesStreamMaxEventBytes = maxEvent
	t.Cleanup(func() { responsesStreamMaxEventBytes = saved })

	prefix := []byte("event: response.output_item.added\ndata: {\"output_index\":0,\"item\":{\"type\":\"function_call\",\"id\":\"fc_a\",\"call_id\":\"call_a\",\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\\\"a.go\\\"}\"}}\n\nevent: response.function_call_arguments.delta\ndata: {\"pad\":\"")
	pad := maxEvent - len(prefix) - len("\"}\n\n")
	if pad < 1 {
		t.Fatalf("kept frame does not fill the scan buffer")
	}
	kept := append(append(append([]byte(nil), prefix...), bytes.Repeat([]byte("q"), pad)...), []byte("\"}\n\n")...)
	if len(kept) != maxEvent {
		t.Fatalf("kept frame len=%d, want %d", len(kept), maxEvent)
	}
	body := append(append([]byte(nil), kept...), []byte("event: response.output_text.delta\ndata: {\"delta\":\"")...)
	body = append(body, bytes.Repeat([]byte("y"), maxEvent)...)

	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Split(scanResponsesAPIStreamEvent)
	scanner.Buffer(make([]byte, 0, 64*1024), maxEvent)
	if !scanner.Scan() || !bytes.Contains(scanner.Bytes(), []byte("response.output_item.added")) {
		t.Fatalf("scanner dropped the kept event: %v", scanner.Err())
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	response, err := DoResponsesAPIRequestStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Model: "test", WireAPI: "responses"},
		[]any{map[string]any{"role": "user", "content": "test"}},
		nil,
		srv.Client(),
		nil,
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error=%v body=%d", err, len(body))
	}
	if response == nil || len(response.Choices) != 1 {
		t.Fatalf("partial response = %#v", response)
	}
	calls := response.Choices[0].Message.ToolCalls
	if len(calls) != 1 || calls[0].Function.Name != "read_file" || calls[0].Function.Arguments != `{"path":"a.go"}` {
		t.Fatalf("kept call was dropped with the oversized tail: %#v", calls)
	}
}

