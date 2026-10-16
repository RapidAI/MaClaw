package httpapi

import (
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// withInboundTraceParent imports the W3C trace context emitted by MaClawSrv.
// Invalid or incomplete headers are ignored so an untrusted caller cannot
// inject arbitrary correlation values into downstream logs/audit records.
func withInboundTraceParent(next http.Handler) http.Handler {
	if next == nil {
		return http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID, spanID, ok := parseTraceParent(r.Header.Get("traceparent"))
		if ok {
			ctx := agentruntime.WithTraceID(r.Context(), traceID)
			ctx = agentruntime.WithSpanID(ctx, spanID)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

func parseTraceParent(value string) (traceID, spanID string, ok bool) {
	parts := strings.Fields(strings.TrimSpace(value))
	if len(parts) != 1 {
		return "", "", false
	}
	fields := strings.Split(parts[0], "-")
	if len(fields) != 4 || len(fields[0]) != 2 || len(fields[1]) != 32 || len(fields[2]) != 16 || len(fields[3]) != 2 {
		return "", "", false
	}
	if strings.EqualFold(fields[0], "ff") || strings.EqualFold(fields[1], strings.Repeat("0", 32)) || strings.EqualFold(fields[2], strings.Repeat("0", 16)) {
		return "", "", false
	}
	for _, field := range fields {
		if _, err := hex.DecodeString(field); err != nil {
			return "", "", false
		}
	}
	return strings.ToLower(fields[1]), strings.ToLower(fields[2]), true
}
