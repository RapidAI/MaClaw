package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/logx"
)

func TestWithRequestIDAcceptsSafeCallerCorrelation(t *testing.T) {
	var seen string
	handler := withRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = requestIDFromContext(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set(requestIDHeader, "client-req.1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if seen != "client-req.1" || w.Header().Get(requestIDHeader) != seen {
		t.Fatalf("request id was not propagated: seen=%q response=%q", seen, w.Header().Get(requestIDHeader))
	}
}

func TestWithRequestIDPropagatesTraceParentWithoutLeakingRawPrincipal(t *testing.T) {
	const trace = "4bf92f3577b34da6a3ce929d0e0e4736"
	var seen, seenSpan string
	handler := withRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = agentruntime.TraceID(r.Context())
		seenSpan = agentruntime.SpanID(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("traceparent", "00-"+trace+"-00f067aa0ba902b7-01")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if seen != trace || seenSpan != "00f067aa0ba902b7" {
		t.Fatalf("trace context was not propagated: trace=%q span=%q", seen, seenSpan)
	}
}

func TestTraceIDFromTraceParentRejectsMalformedValues(t *testing.T) {
	for _, value := range []string{"", "00-00000000000000000000000000000000-00f067aa0ba902b7-01", "01-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", "00-short-00f067aa0ba902b7-01"} {
		if got := traceIDFromTraceParent(value); got != "" {
			t.Fatalf("malformed traceparent %q parsed as %q", value, got)
		}
	}
}

func TestWithRequestIDReplacesUnsafeOrOversizedValue(t *testing.T) {
	for _, value := range []string{"bad\nheader", strings.Repeat("x", 129), ""} {
		handler := withRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !validRequestID(requestIDFromContext(r.Context())) {
				t.Errorf("generated request id is unsafe: %q", requestIDFromContext(r.Context()))
			}
		}))
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		if value != "" {
			req.Header.Set(requestIDHeader, value)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if id := w.Header().Get(requestIDHeader); !validRequestID(id) {
			t.Fatalf("response request id is unsafe for input %q: %q", value, id)
		}
	}
}

func TestWithRequestIDLogsCompletionWithCorrelation(t *testing.T) {
	var buf bytes.Buffer
	prev := srvLogger
	srvLogger = logx.New(&buf, logx.Options{Format: "json", Level: slog.LevelDebug})
	t.Cleanup(func() { srvLogger = prev })

	handler := withRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/test?api_key=sk-should-not-appear", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	out := buf.String()
	if !strings.Contains(out, "http request completed") {
		t.Fatalf("completion log missing: %s", out)
	}
	if !strings.Contains(out, `"request_id"`) || !strings.Contains(out, `"status":502`) {
		t.Fatalf("correlation/status missing: %s", out)
	}
	if strings.Contains(out, "sk-should-not-appear") {
		t.Fatalf("credential leaked into request log: %s", out)
	}
}
