package llm

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

type requestTraceContextKey struct{}

// RequestTrace identifies the logical caller behind an LLM HTTP request.
// It is diagnostic-only; no provider-facing request body depends on it.
type RequestTrace struct {
	Caller    string
	OwnerID   string
	RequestID string
	LoopID    string
	Iteration int
}

func WithRequestTrace(ctx context.Context, trace RequestTrace) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	trace.Caller = strings.TrimSpace(trace.Caller)
	trace.OwnerID = strings.TrimSpace(trace.OwnerID)
	trace.RequestID = strings.TrimSpace(trace.RequestID)
	trace.LoopID = strings.TrimSpace(trace.LoopID)
	return context.WithValue(ctx, requestTraceContextKey{}, trace)
}

func RequestTraceFromContext(ctx context.Context) (RequestTrace, bool) {
	if ctx == nil {
		return RequestTrace{}, false
	}
	trace, ok := ctx.Value(requestTraceContextKey{}).(RequestTrace)
	return trace, ok
}

func WithRequestTraceIfMissing(ctx context.Context, fallbackCaller string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if trace, ok := RequestTraceFromContext(ctx); ok && strings.TrimSpace(trace.Caller) != "" {
		return ctx
	}
	caller := strings.TrimSpace(fallbackCaller)
	if caller == "" {
		caller = inferRequestTraceCaller(2)
	}
	return WithRequestTrace(ctx, RequestTrace{Caller: caller})
}

func inferRequestTraceCaller(skip int) string {
	pc, _, _, ok := runtime.Caller(skip)
	if !ok {
		return "unknown"
	}
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return "unknown"
	}
	name := strings.TrimSpace(fn.Name())
	if name == "" {
		return "unknown"
	}
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	name = filepath.ToSlash(name)
	return strings.TrimPrefix(name, "main.")
}

func RequestTraceLogFields(ctx context.Context) string {
	trace, ok := RequestTraceFromContext(ctx)
	if !ok {
		trace.Caller = "unknown"
	}
	if trace.Caller == "" {
		trace.Caller = "unknown"
	}
	return fmt.Sprintf("caller=%q owner=%q request_id=%q loop=%q iteration=%d", trace.Caller, trace.OwnerID, trace.RequestID, trace.LoopID, trace.Iteration)
}
