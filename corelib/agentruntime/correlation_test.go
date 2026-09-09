package agentruntime

import (
	"context"
	"testing"
)

func TestTraceIDValidationAndNormalization(t *testing.T) {
	trace := "4bf92f3577b34da6a3ce929d0e0e4736"
	ctx := WithTraceID(context.Background(), "  "+trace+"  ")
	if got := TraceID(ctx); got != trace {
		t.Fatalf("TraceID=%q, want %q", got, trace)
	}
	for _, invalid := range []string{"", "00000000000000000000000000000000", "not-hex", trace[:31], trace + "00"} {
		if got := TraceID(WithTraceID(context.Background(), invalid)); got != "" {
			t.Fatalf("invalid trace id %q accepted as %q", invalid, got)
		}
	}
}

func TestSpanIDValidationAndNormalization(t *testing.T) {
	span := "00f067aa0ba902b7"
	ctx := WithSpanID(context.Background(), "  "+span+"  ")
	if got := SpanID(ctx); got != span {
		t.Fatalf("SpanID=%q, want %q", got, span)
	}
	for _, invalid := range []string{"", "0000000000000000", "not-hex", span[:15], span + "00"} {
		if got := SpanID(WithSpanID(context.Background(), invalid)); got != "" {
			t.Fatalf("invalid span id %q accepted as %q", invalid, got)
		}
	}
}

func TestRunSpanContextAndChildDerivation(t *testing.T) {
	ctx := WithTraceID(context.Background(), "4bf92f3577b34da6a3ce929d0e0e4736")
	parent := "00f067aa0ba902b7"
	child := DeriveChildSpanID(parent, "run-123")
	if len(child) != 16 || child == parent || child == "0000000000000000" {
		t.Fatalf("invalid child span id %q", child)
	}
	if child != DeriveChildSpanID(parent, "run-123") {
		t.Fatal("child span derivation is not deterministic")
	}
	ctx = WithRunSpanID(ctx, child)
	if got := RunSpanID(ctx); got != child {
		t.Fatalf("RunSpanID=%q, want %q", got, child)
	}
	if got := RunSpanID(WithRunSpanID(context.Background(), "0000000000000000")); got != "" {
		t.Fatalf("zero run span accepted: %q", got)
	}
}

func TestTraceParentHeader(t *testing.T) {
	ctx := context.Background()
	if TraceParentHeader(ctx) != "" {
		t.Fatal("empty context must not emit traceparent")
	}
	ctx = WithTraceID(ctx, "4bf92f3577b34da6a3ce929d0e0e4736")
	if TraceParentHeader(ctx) != "" {
		t.Fatal("trace without span must not emit traceparent")
	}
	ctx = WithSpanID(ctx, "00f067aa0ba902b7")
	got := TraceParentHeader(ctx)
	want := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
