package llm

import (
	"context"
	"strings"
	"testing"
)

func TestRequestTraceLogFieldsIncludesOwnerAndRequest(t *testing.T) {
	ctx := WithRequestTrace(context.Background(), RequestTrace{
		Caller:    "agent_loop",
		OwnerID:   "desktop-user:D:/tasks/a",
		RequestID: "req-1",
		LoopID:    "chat",
		Iteration: 2,
	})

	fields := RequestTraceLogFields(ctx)
	for _, want := range []string{`caller="agent_loop"`, `owner="desktop-user:D:/tasks/a"`, `request_id="req-1"`, `loop="chat"`, `iteration=2`} {
		if !strings.Contains(fields, want) {
			t.Fatalf("RequestTraceLogFields() = %q, missing %s", fields, want)
		}
	}
}

func TestWithRequestTraceIfMissingFillsFallbackCaller(t *testing.T) {
	ctx := WithRequestTraceIfMissing(context.Background(), "background_memory")
	fields := RequestTraceLogFields(ctx)
	if !strings.Contains(fields, `caller="background_memory"`) {
		t.Fatalf("RequestTraceLogFields() = %q, missing fallback caller", fields)
	}

	existing := WithRequestTrace(context.Background(), RequestTrace{Caller: "agent_loop", OwnerID: "owner-1"})
	existing = WithRequestTraceIfMissing(existing, "background_memory")
	fields = RequestTraceLogFields(existing)
	if !strings.Contains(fields, `caller="agent_loop"`) || !strings.Contains(fields, `owner="owner-1"`) {
		t.Fatalf("existing trace overwritten: %q", fields)
	}
}
