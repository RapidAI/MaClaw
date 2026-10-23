package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestStreamingRequestContextBodyHasNoWallClock(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	deadline, ok := parent.Deadline()
	if !ok {
		t.Fatal("parent missing deadline")
	}
	parent = WithOwnedRouteDeadline(parent, deadline)

	ctx, stop, markHeaders, _ := streamingRequestContext(parent)
	defer stop()
	if !markHeaders() {
		t.Fatal("headers did not own the body")
	}
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		t.Fatal("live SSE body still has a wall-clock deadline")
	}
	select {
	case <-ctx.Done():
		t.Fatalf("stream context ended without a stop: %v", ctx.Err())
	case <-time.After(50 * time.Millisecond):
	}
}

func TestNormalizeStreamSetupErrorKeepsConnectionFailure(t *testing.T) {
	var deadlineHit atomic.Bool
	deadlineHit.Store(true)
	err := normalizeStreamSetupError(&deadlineHit, context.DeadlineExceeded)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline err = %v", err)
	}
	if got := normalizeStreamSetupError(&deadlineHit, context.Canceled); !errors.Is(got, context.DeadlineExceeded) {
		t.Fatalf("canceled during route timeout = %v", got)
	}
	conn := errors.New("connection reset")
	if got := normalizeStreamSetupError(&deadlineHit, conn); got != conn {
		t.Fatalf("connection err = %v, want the original failure", got)
	}
	deadlineHit.Store(false)
	if got := normalizeStreamSetupError(&deadlineHit, context.Canceled); got != context.Canceled {
		t.Fatalf("cancel without route timer = %v", got)
	}
}

func TestStreamingRequestContextMarkHeadersLosesToExpiredRouteDeadline(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	parent = WithOwnedRouteDeadline(parent, time.Now().Add(-time.Second))

	ctx, stop, markHeaders, flag := streamingRequestContext(parent)
	defer stop()
	deadline := time.Now().Add(time.Second)
	for !flag.Load() {
		if time.Now().After(deadline) {
			t.Fatal("route timer did not claim the request")
		}
		time.Sleep(2 * time.Millisecond)
	}
	if markHeaders() {
		t.Fatal("markHeaders owned a request the route deadline already claimed")
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("claimed route deadline did not cancel the stream context")
	}
}

func TestStreamingRequestContextKeepsBodyAlivePastOwnedRouteDeadline(t *testing.T) {
	parent, cancelParent := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancelParent()
	deadline, ok := parent.Deadline()
	if !ok {
		t.Fatal("parent missing deadline")
	}
	parent = WithOwnedRouteDeadline(parent, deadline)

	ctx, stop, markHeaders, _ := streamingRequestContext(parent)
	defer stop()
	markHeaders()

	select {
	case <-ctx.Done():
		t.Fatalf("stream context ended early: %v", ctx.Err())
	case <-time.After(120 * time.Millisecond):
	}
	if parent.Err() == nil {
		t.Fatal("parent deadline did not fire")
	}
	if ctx.Err() != nil {
		t.Fatalf("stream context err = %v, want nil after headers", ctx.Err())
	}
}

func TestStreamingRequestContextCancelsBeforeHeadersOnRouteDeadline(t *testing.T) {
	parent, cancelParent := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancelParent()
	deadline, _ := parent.Deadline()
	parent = WithOwnedRouteDeadline(parent, deadline)

	ctx, stop, _, _ := streamingRequestContext(parent)
	defer stop()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("stream context stayed open until headers timed out")
	}
}

func TestStreamingRequestContextStopSurvivesRouteDeadline(t *testing.T) {
	cancelParent, stopTurn := context.WithCancel(context.Background())
	defer stopTurn()
	route, cancelRoute := context.WithTimeout(cancelParent, 40*time.Millisecond)
	defer cancelRoute()
	deadline, ok := route.Deadline()
	if !ok {
		t.Fatal("route missing deadline")
	}
	route = BindRouteStreamDeadline(route, deadline, cancelParent)

	ctx, stop, markHeaders, _ := streamingRequestContext(route)
	defer stop()
	markHeaders()

	select {
	case <-ctx.Done():
		t.Fatalf("route deadline aborted the body: %v", ctx.Err())
	case <-time.After(120 * time.Millisecond):
	}
	stopTurn()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("stop after the route deadline did not abort the stream")
	}
}

func TestStreamingRequestContextHostDeadlineAbortsAfterHeaders(t *testing.T) {
	host, stopHost := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer stopHost()
	route, cancelRoute := context.WithTimeout(host, 40*time.Millisecond)
	defer cancelRoute()
	deadline, ok := route.Deadline()
	if !ok {
		t.Fatal("route missing deadline")
	}
	route = BindRouteStreamDeadline(route, deadline, host)

	ctx, stop, markHeaders, _ := streamingRequestContext(route)
	defer stop()
	markHeaders()

	select {
	case <-ctx.Done():
		t.Fatalf("route deadline aborted the body: %v", ctx.Err())
	case <-time.After(90 * time.Millisecond):
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("host deadline did not abort the stream after headers")
	}
}

func TestStreamingRequestContextWrapperCancelAbortsAfterHeaders(t *testing.T) {
	cancelParent, stopTurn := context.WithCancel(context.Background())
	defer stopTurn()
	route, cancelRoute := context.WithTimeout(cancelParent, 2*time.Second)
	defer cancelRoute()
	deadline, ok := route.Deadline()
	if !ok {
		t.Fatal("route missing deadline")
	}
	route = BindRouteStreamDeadline(route, deadline, cancelParent)
	wrapper, cancelWrapper := context.WithCancel(route)

	ctx, stop, markHeaders, _ := streamingRequestContext(wrapper)
	defer stop()
	if !markHeaders() {
		t.Fatal("headers did not own the body")
	}
	cancelWrapper()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("cancel of the request wrapper did not abort the stream")
	}
	if cancelParent.Err() != nil {
		t.Fatal("wrapper cancel also stopped the turn")
	}
}

func TestStreamingRequestContextWrapperDeadlineDoesNotAbortAfterHeaders(t *testing.T) {
	cancelParent, stopTurn := context.WithCancel(context.Background())
	defer stopTurn()
	route, cancelRoute := context.WithTimeout(cancelParent, 40*time.Millisecond)
	defer cancelRoute()
	deadline, ok := route.Deadline()
	if !ok {
		t.Fatal("route missing deadline")
	}
	route = BindRouteStreamDeadline(route, deadline, cancelParent)
	wrapper, cancelWrapper := context.WithCancel(route)
	defer cancelWrapper()

	ctx, stop, markHeaders, _ := streamingRequestContext(wrapper)
	defer stop()
	if !markHeaders() {
		t.Fatal("headers did not own the body")
	}

	select {
	case <-ctx.Done():
		t.Fatalf("route deadline aborted the wrapped body: %v", ctx.Err())
	case <-time.After(120 * time.Millisecond):
	}
	if route.Err() == nil {
		t.Fatal("route deadline did not fire")
	}
}

func TestStreamingRequestContextWrapperCancelAbortsBeforeHeaders(t *testing.T) {
	cancelParent, stopTurn := context.WithCancel(context.Background())
	defer stopTurn()
	route, cancelRoute := context.WithTimeout(cancelParent, 2*time.Second)
	defer cancelRoute()
	deadline, _ := route.Deadline()
	route = BindRouteStreamDeadline(route, deadline, cancelParent)
	wrapper, cancelWrapper := context.WithCancel(route)

	ctx, stop, _, _ := streamingRequestContext(wrapper)
	defer stop()
	cancelWrapper()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("cancel of the request wrapper left the stream waiting for headers")
	}
}

func TestStreamingRequestContextHonorsCallerCancelAfterHeaders(t *testing.T) {
	parent, cancelParent := context.WithTimeout(context.Background(), time.Second)
	deadline, _ := parent.Deadline()
	parent = WithOwnedRouteDeadline(parent, deadline)

	ctx, stop, markHeaders, _ := streamingRequestContext(parent)
	defer stop()
	markHeaders()
	cancelParent()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("caller cancel did not abort the stream context")
	}
}

func TestStreamingRequestContextLeavesHostDeadlineAttached(t *testing.T) {
	parent, cancelParent := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancelParent()
	ctx, stop, markHeaders, _ := streamingRequestContext(parent)
	defer stop()
	markHeaders()
	if ctx != parent {
		t.Fatal("context without an owned route deadline was replaced")
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("host deadline did not cancel the original context")
	}
}

func TestOpenAIStreamOutlivesRouteDeadlineAfterHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello \"}}]}\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(350 * time.Millisecond):
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"there\"},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	parent, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	deadline, _ := parent.Deadline()
	parent = WithOwnedRouteDeadline(parent, deadline)

	resp, err := DoOpenAIRequestStream(
		parent,
		corelib.MaclawLLMConfig{URL: server.URL, Model: "glm-5.3-flash", Protocol: "openai"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		nil,
		server.Client(),
		nil,
	)
	if err != nil {
		t.Fatalf("stream error = %v, want the body to finish after the route deadline", err)
	}
	if got := resp.Choices[0].Message.Content; got != "hello there" {
		t.Fatalf("content = %q, want hello there", got)
	}
}

func TestAnthropicStreamOutlivesRouteDeadlineAfterHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeAnthropicStreamPrefix(w, "hello ")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(350 * time.Millisecond):
		}
		writeAnthropicStreamSuffix(w, "there")
	}))
	defer server.Close()

	parent, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	deadline, _ := parent.Deadline()
	parent = WithOwnedRouteDeadline(parent, deadline)

	resp, _, _, err := anthropicSDKMessageStream(
		parent,
		corelib.MaclawLLMConfig{URL: server.URL, Key: "test-key", Model: "glm-5.3-flash", Protocol: "anthropic"},
		[]byte(`{"model":"glm-5.3-flash","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`),
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

func TestAnthropicStreamRouteDeadlineStillBoundsHeaders(t *testing.T) {
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
	_, _, _, err := anthropicSDKMessageStream(
		parent,
		corelib.MaclawLLMConfig{URL: server.URL, Key: "test-key", Model: "glm-5.3-flash", Protocol: "anthropic"},
		[]byte(`{"model":"glm-5.3-flash","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`),
		server.Client(),
		nil,
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("elapsed = %s, want the route deadline rather than the continuation cap", elapsed)
	}
}

func TestAnthropicStreamStopAbortsAfterHeaders(t *testing.T) {
	cancelParent, stopTurn := context.WithCancel(context.Background())
	defer stopTurn()
	route, cancelRoute := context.WithTimeout(cancelParent, 2*time.Second)
	defer cancelRoute()
	deadline, _ := route.Deadline()
	route = BindRouteStreamDeadline(route, deadline, cancelParent)

	headersSent := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeAnthropicStreamPrefix(w, "hello ")
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
	_, _, _, err := anthropicSDKMessageStream(
		route,
		corelib.MaclawLLMConfig{URL: server.URL, Key: "test-key", Model: "glm-5.3-flash", Protocol: "anthropic"},
		[]byte(`{"model":"glm-5.3-flash","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`),
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

func writeAnthropicStreamPrefix(w io.Writer, text string) {
	_, _ = io.WriteString(w, "event: message_start\n")
	_, _ = io.WriteString(w, `data: {"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","model":"glm-5.3-flash","content":[],"stop_reason":null,"usage":{"input_tokens":3,"output_tokens":0}}}`+"\n\n")
	_, _ = io.WriteString(w, "event: content_block_start\n")
	_, _ = io.WriteString(w, `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`+"\n\n")
	_, _ = io.WriteString(w, "event: content_block_delta\n")
	_, _ = fmt.Fprintf(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%q}}\n\n", text)
}

func writeAnthropicStreamSuffix(w io.Writer, text string) {
	_, _ = io.WriteString(w, "event: content_block_delta\n")
	_, _ = fmt.Fprintf(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%q}}\n\n", text)
	_, _ = io.WriteString(w, "event: content_block_stop\n")
	_, _ = io.WriteString(w, `data: {"type":"content_block_stop","index":0}`+"\n\n")
	_, _ = io.WriteString(w, "event: message_delta\n")
	_, _ = io.WriteString(w, `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`+"\n\n")
	_, _ = io.WriteString(w, "event: message_stop\n")
	_, _ = io.WriteString(w, `data: {"type":"message_stop"}`+"\n\n")
}

func TestOpenAIStreamRouteDeadlineStillBoundsHeaders(t *testing.T) {
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
	_, err := DoOpenAIRequestStream(
		parent,
		corelib.MaclawLLMConfig{URL: server.URL, Model: "glm-5.3-flash", Protocol: "openai"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		nil,
		server.Client(),
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("elapsed = %s, want the route deadline rather than the continuation cap", elapsed)
	}
}
