package agentruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// CorrelationID is the transport-neutral request/run correlation identifier.
// Hosts (HTTP, GUI, TUI, IM) may attach one to a context before entering the
// shared Service/Runtime.  The value is intentionally bounded and log-safe so
// it can be copied into Run metadata and event payloads without becoming an
// injection channel.
type correlationIDContextKey struct{}
type traceIDContextKey struct{}
type spanIDContextKey struct{}
type runSpanIDContextKey struct{}

const maxCorrelationIDBytes = 128

// WithCorrelationID attaches a bounded, printable correlation id. Invalid or
// empty values are ignored so callers cannot accidentally persist unsafe data.
func WithCorrelationID(ctx context.Context, value string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	value = strings.TrimSpace(value)
	if !validCorrelationID(value) {
		return ctx
	}
	return context.WithValue(ctx, correlationIDContextKey{}, value)
}

// CorrelationID returns the safe correlation id attached to ctx, if any.
func CorrelationID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(correlationIDContextKey{}).(string)
	value = strings.TrimSpace(value)
	if !validCorrelationID(value) {
		return ""
	}
	return value
}

func validCorrelationID(value string) bool {
	if value == "" || len(value) > maxCorrelationIDBytes {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' || r == ':' {
			continue
		}
		return false
	}
	return true
}

// WithTraceID attaches a W3C-compatible trace identifier to ctx. The helper
// accepts the 32-hex-digit trace-id portion of a traceparent header and keeps
// it separate from request_id so callers can join logs without conflating
// transport retries and distributed traces.
func WithTraceID(ctx context.Context, value string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	value = strings.TrimSpace(value)
	if !validTraceID(value) {
		return ctx
	}
	return context.WithValue(ctx, traceIDContextKey{}, strings.ToLower(value))
}

// TraceID returns the validated trace identifier attached to ctx, if any.
func TraceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(traceIDContextKey{}).(string)
	value = strings.TrimSpace(value)
	if !validTraceID(value) {
		return ""
	}
	return strings.ToLower(value)
}

// WithSpanID attaches the parent span identifier carried by a W3C
// traceparent header. Keeping it separate from TraceID lets downstream
// adapters create a child span without overwriting the distributed trace.
func WithSpanID(ctx context.Context, value string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	value = strings.TrimSpace(value)
	if !validSpanID(value) {
		return ctx
	}
	return context.WithValue(ctx, spanIDContextKey{}, strings.ToLower(value))
}

// SpanID returns the validated parent span id attached to ctx, if any.
func SpanID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(spanIDContextKey{}).(string)
	value = strings.TrimSpace(value)
	if !validSpanID(value) {
		return ""
	}
	return strings.ToLower(value)
}

// WithRunSpanID attaches the child span created for an Agent run. It is kept
// separate from SpanID so the inbound HTTP/IM parent remains available for
// compatibility while downstream events and audit records can link to the
// actual run span.
func WithRunSpanID(ctx context.Context, value string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	value = strings.TrimSpace(value)
	if !validSpanID(value) {
		return ctx
	}
	return context.WithValue(ctx, runSpanIDContextKey{}, strings.ToLower(value))
}

// RunSpanID returns the child span associated with the current Agent run, if
// one has been attached by the Service boundary.
func RunSpanID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(runSpanIDContextKey{}).(string)
	value = strings.TrimSpace(value)
	if !validSpanID(value) {
		return ""
	}
	return strings.ToLower(value)
}

// DeriveChildSpanID returns a deterministic, W3C-shaped child span id for a
// stable run identity. A deterministic derivation avoids introducing a random
// source into replay tests while still giving every run a span distinct from
// its inbound parent. The parent value participates in the digest so retries
// with the same run id under a different request context cannot collide.
func DeriveChildSpanID(parentSpanID, stableRunID string) string {
	seed := strings.TrimSpace(parentSpanID) + "\x00" + strings.TrimSpace(stableRunID)
	digest := sha256.Sum256([]byte(seed))
	child := hex.EncodeToString(digest[:8])
	if child == strings.Repeat("0", 16) {
		// SHA-256 all-zero output is fantastically unlikely, but W3C reserves
		// the zero span id. Keep the helper total and contract-safe regardless
		// of a future test hash implementation.
		child = "0000000000000001"
	}
	return child
}

func validTraceID(value string) bool {
	if len(value) != 32 || value == strings.Repeat("0", 32) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 16
}

func validSpanID(value string) bool {
	if len(value) != 16 || value == strings.Repeat("0", 16) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 8
}

// TraceParentHeader renders the W3C traceparent header value for outbound
// cross-service calls. It returns "" unless the context carries both a valid
// trace id and span id, so downstream services never receive a malformed or
// half-linked header.
func TraceParentHeader(ctx context.Context) string {
	traceID := TraceID(ctx)
	spanID := SpanID(ctx)
	if !validTraceID(traceID) || !validSpanID(spanID) {
		return ""
	}
	return "00-" + traceID + "-" + spanID + "-01"
}
