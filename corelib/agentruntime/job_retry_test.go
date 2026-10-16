package agentruntime

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNormalizeJobRetryPolicyFailsClosed(t *testing.T) {
	want := JobRetryPolicy{MaxAttempts: 1}
	for _, policy := range []JobRetryPolicy{
		{},
		{MaxAttempts: 3},
		{MaxAttempts: 11, InitialBackoffMillis: 1, MaximumBackoffMillis: 1},
		{MaxAttempts: 2, InitialBackoffMillis: 100, MaximumBackoffMillis: 10},
	} {
		if got := NormalizeJobRetryPolicy(policy); got != want {
			t.Fatalf("NormalizeJobRetryPolicy(%#v) = %#v, want %#v", policy, got, want)
		}
	}
}

func TestJobRetryBackoffIsExponentialAndCapped(t *testing.T) {
	policy := JobRetryPolicy{MaxAttempts: 5, InitialBackoffMillis: 10, MaximumBackoffMillis: 25}
	wants := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 25 * time.Millisecond, 25 * time.Millisecond, 0}
	for i, want := range wants {
		if got := JobRetryBackoff(policy, uint32(i+1)); got != want {
			t.Fatalf("attempt %d backoff = %v, want %v", i+1, got, want)
		}
	}
}

func TestMarkJobErrorRetryableIsExplicitAndPreservesCause(t *testing.T) {
	cause := errors.New("temporary probe failure")
	marked := MarkJobErrorRetryable(cause)
	if !IsJobErrorRetryable(marked) || !errors.Is(marked, cause) {
		t.Fatalf("retry marker did not preserve cause: %v", marked)
	}
	if IsJobErrorRetryable(cause) {
		t.Fatal("plain error was retryable")
	}
	if got := MarkJobErrorRetryable(context.Canceled); got != context.Canceled || IsJobErrorRetryable(got) {
		t.Fatalf("context cancellation became retryable: %v", got)
	}
}

func TestJobAttemptContext(t *testing.T) {
	ctx := WithJobAttempt(context.Background(), 3)
	if got := JobAttemptFromContext(ctx); got != 3 {
		t.Fatalf("attempt = %d, want 3", got)
	}
	if got := JobAttemptFromContext(context.Background()); got != 0 {
		t.Fatalf("plain context attempt = %d, want 0", got)
	}
}

func TestValidateJobRetryStateRejectsImpossibleSchedule(t *testing.T) {
	policy := JobRetryPolicy{MaxAttempts: 3, InitialBackoffMillis: 10, MaximumBackoffMillis: 20}
	next := time.Now().UTC()
	for _, attempt := range []uint32{0, 3, 4} {
		if err := ValidateJobRetryState(policy, attempt, &next); !errors.Is(err, ErrInvalidJobRetryPolicy) {
			t.Fatalf("attempt %d error = %v", attempt, err)
		}
	}
}
