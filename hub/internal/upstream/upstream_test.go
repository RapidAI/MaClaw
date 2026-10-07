package upstream

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestNewStatusErrorKeepsBaseStatusAndDetail(t *testing.T) {
	base := errors.New("upstream request failed")
	err := NewStatusError(base, http.StatusUnauthorized, []byte(`{"error":"unauthorized"}`))
	if !errors.Is(err, base) {
		t.Fatalf("errors.Is lost the base sentinel")
	}
	if err.Status != http.StatusUnauthorized || err.Detail != "unauthorized" {
		t.Fatalf("status=%d detail=%q", err.Status, err.Detail)
	}
	if msg := err.Error(); !strings.Contains(msg, "401") || !strings.Contains(msg, "unauthorized") {
		t.Fatalf("Error() = %q", msg)
	}
}

func TestDetailFallsBackToRawBody(t *testing.T) {
	if got := Detail([]byte("  ")); got != "" {
		t.Fatalf("empty body detail = %q", got)
	}
	if got := Detail([]byte("boom\n happened")); got != "boom happened" {
		t.Fatalf("plain body detail = %q", got)
	}
	long := Detail([]byte(`{"error":"` + strings.Repeat("啊", 500) + `"}`))
	if len([]rune(long)) != 203 || !strings.HasSuffix(long, "...") {
		t.Fatalf("long detail len=%d suffix=%q", len([]rune(long)), long[len(long)-3:])
	}
}

func TestMessageUsesStatusHintsAndKeepsTransportReason(t *testing.T) {
	base := "upstream rejected the request"
	msg := Message(NewStatusError(errors.New("x"), http.StatusForbidden, nil), base)
	if !strings.Contains(msg, "HTTP 403") || !strings.Contains(msg, "permission") {
		t.Fatalf("403 message = %q", msg)
	}
	msg = Message(errors.New("dial tcp 10.0.0.1:443: connect: connection refused"), base)
	if !strings.Contains(msg, "connection refused") {
		t.Fatalf("transport message = %q", msg)
	}
	if msg := Message(nil, base); msg != base {
		t.Fatalf("nil message = %q", msg)
	}
}
