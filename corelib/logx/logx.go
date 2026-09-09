// Package logx provides the shared structured logging boundary for MaClaw
// hosts. Production hosts should prefer this over ad-hoc log.Printf so that
// correlation IDs and credential/path redaction are applied uniformly; the
// standard library log package remains the documented fallback.
package logx

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// Options configures a structured logger.
type Options struct {
	// Format is "json" or "text" (default "text").
	Format string
	// Level is the minimum level (default Info).
	Level slog.Leveler
}

// New builds a structured logger writing to w. Credential/URL/path redaction
// is always on: hosts must not be able to opt out of the safety boundary.
func New(w io.Writer, opts Options) *slog.Logger {
	if w == nil {
		w = os.Stderr
	}
	level := opts.Level
	if level == nil {
		level = slog.LevelInfo
	}
	handlerOpts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if strings.EqualFold(strings.TrimSpace(opts.Format), "json") {
		handler = slog.NewJSONHandler(w, handlerOpts)
	} else {
		handler = slog.NewTextHandler(w, handlerOpts)
	}
	return slog.New(NewRedactHandler(handler))
}

// NewFromEnv builds a logger from environment configuration:
// MACLAW_LOG_FORMAT=json|text (default text), MACLAW_LOG_LEVEL=debug|info|warn|error.
func NewFromEnv(w io.Writer) *slog.Logger {
	opts := Options{Format: os.Getenv("MACLAW_LOG_FORMAT")}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_LOG_LEVEL"))) {
	case "debug":
		opts.Level = slog.LevelDebug
	case "warn":
		opts.Level = slog.LevelWarn
	case "error":
		opts.Level = slog.LevelError
	}
	return New(w, opts)
}

// WithCorrelation returns a logger carrying the correlation IDs already
// threaded through ctx by the shared runtime (request id, W3C trace/span).
func WithCorrelation(ctx context.Context, l *slog.Logger) *slog.Logger {
	if l == nil {
		l = slog.Default()
	}
	if ctx == nil {
		return l
	}
	if id := agentruntime.CorrelationID(ctx); id != "" {
		l = l.With(slog.String("request_id", id))
	}
	if id := agentruntime.TraceID(ctx); id != "" {
		l = l.With(slog.String("trace_id", id))
	}
	if id := agentruntime.SpanID(ctx); id != "" {
		l = l.With(slog.String("span_id", id))
	}
	if id := agentruntime.RunSpanID(ctx); id != "" {
		l = l.With(slog.String("run_span_id", id))
	}
	return l
}
