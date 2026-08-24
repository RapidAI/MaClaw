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
