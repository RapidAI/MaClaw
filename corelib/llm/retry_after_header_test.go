package llm

import (
	"net/http"
	"testing"
	"time"
)

func TestRetryAfterHeaderBody(t *testing.T) {
	now := time.Date(2026, 10, 23, 1, 0, 0, 0, time.UTC)
	r := &http.Response{Header: http.Header{"Retry-After": {"45"}}}
	if w := HubRetryWait(retryAfterHeaderBody(r, now), now); w != 45*time.Second {
		t.Fatalf("seconds form: %v", w)
	}
	r.Header.Set("Retry-After", now.Add(90*time.Second).Format(http.TimeFormat))
	if w := HubRetryWait(retryAfterHeaderBody(r, now), now); w != 90*time.Second {
		t.Fatalf("date form: %v", w)
	}
	if b := retryAfterHeaderBody(&http.Response{Header: http.Header{}}, now); b != nil {
		t.Fatal("no header should give nil")
	}
}
