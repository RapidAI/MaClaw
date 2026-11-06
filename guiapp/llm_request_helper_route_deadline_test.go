package guiapp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestCraftGenerationOutlivesTheOwnedRouteDeadline(t *testing.T) {
	resetDetachedSimpleLLMReadsForTest(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if _, err := io.WriteString(w, "event: response.output_text.delta\ndata: {\"delta\":\"scr\"}\n\n"); err != nil {
			return
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(400 * time.Millisecond):
		}
		_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"delta\":\"ipt\"}\n\n"+
			"event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n")
	}))
	defer srv.Close()

	parent, stop := context.WithCancel(context.Background())
	defer stop()
	route, cancel := context.WithTimeout(parent, 200*time.Millisecond)
	defer cancel()
	deadline, _ := route.Deadline()
	route = llm.BindRouteStreamDeadline(route, deadline, parent)

	resp, err := doSimpleLLMRequestWithOptions(route, routeDeadlineTestConfig(srv.URL), routeDeadlineTestMessages(), srv.Client(), 2*time.Second, craftScriptGenerationOptions())
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil || resp.Content != "script" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestCraftGenerationStopAfterTheRouteDeadlineAbortsTheBody(t *testing.T) {
	resetDetachedSimpleLLMReadsForTest(t)
	headersSent := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if _, err := io.WriteString(w, "event: response.output_text.delta\ndata: {\"delta\":\"scr\"}\n\n"); err != nil {
			return
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		close(headersSent)
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
			_, _ = io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n")
		}
	}))
	defer srv.Close()

	parent, stopTurn := context.WithCancel(context.Background())
	route, cancel := context.WithTimeout(parent, 250*time.Millisecond)
	defer cancel()
	deadline, _ := route.Deadline()
	route = llm.BindRouteStreamDeadline(route, deadline, parent)
	go func() {
		select {
		case <-headersSent:
		case <-time.After(2 * time.Second):
			return
		}
		time.Sleep(400 * time.Millisecond)
		stopTurn()
	}()

	started := time.Now()
	_, err := doSimpleLLMRequestWithOptions(route, routeDeadlineTestConfig(srv.URL), routeDeadlineTestMessages(), srv.Client(), 5*time.Second, craftScriptGenerationOptions())
	elapsed := time.Since(started)
	if err == nil {
		t.Fatal("user stop returned a script")
	}
	if elapsed < 350*time.Millisecond {
		t.Fatalf("body ended after %s, before the stop that follows the route deadline", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("body stayed open for %s after the stop", elapsed)
	}
}

func TestCraftGenerationRouteDeadlineStillFailsBeforeHeaders(t *testing.T) {
	resetDetachedSimpleLLMReadsForTest(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n")
		}
	}))
	defer srv.Close()

	parent, stop := context.WithCancel(context.Background())
	defer stop()
	route, cancel := context.WithTimeout(parent, 80*time.Millisecond)
	defer cancel()
	deadline, _ := route.Deadline()
	route = llm.BindRouteStreamDeadline(route, deadline, parent)

	started := time.Now()
	_, err := doSimpleLLMRequestWithOptions(route, routeDeadlineTestConfig(srv.URL), routeDeadlineTestMessages(), srv.Client(), 2*time.Second, craftScriptGenerationOptions())
	if err == nil {
		t.Fatal("pre-header stall returned a script")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("pre-header stall lasted %s", elapsed)
	}
}

func TestCraftGenerationShortHostDeadlineStillAbortsTheBody(t *testing.T) {
	resetDetachedSimpleLLMReadsForTest(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"delta\":\"scr\"}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
			_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"delta\":\"ipt\"}\n\n"+
				"event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n")
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := doSimpleLLMRequestWithOptions(ctx, routeDeadlineTestConfig(srv.URL), routeDeadlineTestMessages(), srv.Client(), 2*time.Second, craftScriptGenerationOptions())
	if err == nil {
		t.Fatal("short host deadline returned a script")
	}
	if strings.Contains(err.Error(), "scheduling budget") {
		t.Fatalf("short host deadline was reported as the scheduling budget: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("short host deadline lasted %s", elapsed)
	}
}

func TestAwaitResponseWithoutRouteOptStillStopsAtTheCallerDeadline(t *testing.T) {
	resetDetachedSimpleLLMReadsForTest(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"delta\":\"scr\"}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
			_, _ = io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n")
		}
	}))
	defer srv.Close()

	parent, stop := context.WithCancel(context.Background())
	defer stop()
	route, cancel := context.WithTimeout(parent, 80*time.Millisecond)
	defer cancel()
	deadline, _ := route.Deadline()
	route = llm.BindRouteStreamDeadline(route, deadline, parent)

	started := time.Now()
	_, err := doSimpleLLMRequestWithOptions(route, routeDeadlineTestConfig(srv.URL), routeDeadlineTestMessages(), srv.Client(), 2*time.Second, simpleLLMRequestOptions{AwaitResponse: true})
	if err == nil {
		t.Fatal("a caller that did not opt in received the body past its deadline")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("caller deadline lasted %s", elapsed)
	}
}

func routeDeadlineTestConfig(url string) corelib.MaclawLLMConfig {
	return corelib.MaclawLLMConfig{URL: url, Model: "grok-4.7", Protocol: "openai", WireAPI: "responses"}
}

func routeDeadlineTestMessages() []interface{} {
	return []interface{}{map[string]interface{}{"role": "user", "content": "write the craft script"}}
}
