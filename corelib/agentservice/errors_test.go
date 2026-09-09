package agentservice

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestErrorCodeIsStableAcrossWrappedServiceErrors(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{err: ErrUnauthorized, want: "unauthorized"},
		{err: context.Canceled, want: "request_canceled"},
		{err: fmt.Errorf("timeout: %w", context.DeadlineExceeded), want: "request_deadline_exceeded"},
		{err: fmt.Errorf("lookup: %w", ErrRunNotFound), want: "run_not_found"},
		{err: fmt.Errorf("quota: %w", ErrQuotaExceeded), want: "quota_exceeded"},
		{err: &RateLimitError{RetryAfter: time.Second}, want: "rate_limited"},
		{err: ErrServiceClosed, want: "service_closed"},
		{err: fmt.Errorf("write job envelope: %w", ErrJobPersistence), want: "job_persistence_failed"},
		{err: errors.New("database exploded"), want: "request_error"},
	}
	for _, tc := range cases {
		if got := ErrorCode(tc.err); got != tc.want {
			t.Fatalf("ErrorCode(%v)=%q, want %q", tc.err, got, tc.want)
		}
	}
	if got := ErrorCode(nil); got != "" {
		t.Fatalf("ErrorCode(nil)=%q, want empty", got)
	}
}
