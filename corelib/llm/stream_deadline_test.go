package llm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

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
