package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/logx"
)

// requestIDContextKey keeps transport correlation scoped to one request. The
// value is deliberately not exported as a generic string key so application
// code cannot accidentally overwrite it with untrusted metadata.
type requestIDContextKey struct{}

const requestIDHeader = "X-Request-ID"
const traceParentHeader = "traceparent"

func requestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(requestIDContextKey{}).(string)
	return strings.TrimSpace(value)
}

func validRequestID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
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

func traceIDFromTraceParent(value string) string {
	traceID, _ := traceParentIDs(value)
	return traceID
}

func spanIDFromTraceParent(value string) string {
	_, spanID := traceParentIDs(value)
	return spanID
}

func traceParentIDs(value string) (traceID, spanID string) {
	parts := strings.Split(strings.TrimSpace(value), "-")
	if len(parts) != 4 || strings.ToLower(parts[0]) != "00" || len(parts[1]) != 32 || len(parts[2]) != 16 || len(parts[3]) != 2 {
		return "", ""
	}
	if strings.EqualFold(parts[1], strings.Repeat("0", 32)) || strings.EqualFold(parts[2], strings.Repeat("0", 16)) {
		return "", ""
	}
	for _, part := range parts[1:] {
		for _, r := range part {
			if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
				continue
			}
			return "", ""
		}
	}
	return strings.ToLower(parts[1]), strings.ToLower(parts[2])
}

// withRequestID establishes one correlation id for every HTTP request and
// echoes it in the response. Caller-provided ids are accepted only when they
// are bounded and log-safe; malformed or oversized values are replaced with a
// server-generated opaque id so logs and SSE diagnostics cannot be injected.
// Request completion is logged through the shared structured logger with
// correlation IDs attached; 5xx responses are warnings, the rest debug.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if next == nil {
			return
		}
		requestID := strings.TrimSpace(r.Header.Get(requestIDHeader))
		if !validRequestID(requestID) {
			requestID = agentservice.NewID("req")
		}
		w.Header().Set(requestIDHeader, requestID)
		ctx := context.WithValue(r.Context(), requestIDContextKey{}, requestID)
		ctx = agentruntime.WithCorrelationID(ctx, requestID)
		if traceID := traceIDFromTraceParent(r.Header.Get(traceParentHeader)); traceID != "" {
			ctx = agentruntime.WithTraceID(ctx, traceID)
			if spanID := spanIDFromTraceParent(r.Header.Get(traceParentHeader)); spanID != "" {
				ctx = agentruntime.WithSpanID(ctx, spanID)
			}
		}
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r.WithContext(ctx))
		if recorder.status < 500 && !srvLog().Enabled(ctx, slog.LevelDebug) {
			return
		}
		entry := logx.WithCorrelation(ctx, srvLog()).With(
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", recorder.status),
			slog.Int64("elapsed_ms", time.Since(started).Milliseconds()),
		)
		if recorder.status >= 500 {
			entry.Warn("http request completed")
		} else {
			entry.Debug("http request completed")
		}
	})
}

// statusRecorder captures the response status for the completion log.
// Unwrap lets http.ResponseController reach the underlying Flusher/Hijacker
// so SSE and upgrade routes keep working through the wrapper. Note: the
// wrapper itself implements Flusher unconditionally, so handlers probing
// w.(http.Flusher) always succeed; on net/http this is always true anyway.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		// net/http ignores duplicate WriteHeader calls; mirror that so the
		// completion log records the status the client actually received.
		return
	}
	r.wroteHeader = true
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// ReadFrom preserves the zero-copy path (sendfile) for large responses such
// as migration exports, which io.Copy would otherwise buffer through 32KB
// chunks once the writer is wrapped.
func (r *statusRecorder) ReadFrom(rd io.Reader) (int64, error) {
	if rf, ok := r.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(rd)
	}
	return io.Copy(r.ResponseWriter, rd)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
