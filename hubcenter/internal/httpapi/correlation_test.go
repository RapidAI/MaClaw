package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestParseTraceParent(t *testing.T) {
	trace, span, ok := parseTraceParent("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	if !ok || trace != "4bf92f3577b34da6a3ce929d0e0e4736" || span != "00f067aa0ba902b7" {
		t.Fatalf("parsed traceparent = %q/%q/%v", trace, span, ok)
	}
	for _, value := range []string{"", "00-00000000000000000000000000000000-00f067aa0ba902b7-01", "00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01", "not-a-traceparent"} {
		if _, _, ok := parseTraceParent(value); ok {
			t.Fatalf("invalid traceparent accepted: %q", value)
		}
	}
}

func TestWithInboundTraceParentInjectsContext(t *testing.T) {
	var gotTrace, gotSpan string
	h := withInboundTraceParent(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotTrace = agentruntime.TraceID(r.Context())
		gotSpan = agentruntime.SpanID(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if gotTrace == "" || gotSpan == "" {
		t.Fatalf("trace context missing: trace=%q span=%q", gotTrace, gotSpan)
	}
}
