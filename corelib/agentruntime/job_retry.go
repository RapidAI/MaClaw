package agentruntime

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	maxJobRetryAttempts  = uint32(10)
	maxJobRetryBackoffMS = int64((24 * time.Hour) / time.Millisecond)
)

var ErrInvalidJobRetryPolicy = errors.New("invalid job retry policy or state")

// JobRetryPolicy controls retries within the lifetime of the current worker
// process. It never authorizes replay after a process restart; that decision
// remains governed by JobRecoveryPolicy and domain reconciliation.
// MaxAttempts counts the initial attempt, so 1 means retry is disabled.
type JobRetryPolicy struct {
	MaxAttempts          uint32 `json:"max_attempts"`
	InitialBackoffMillis int64  `json:"initial_backoff_ms,omitempty"`
	MaximumBackoffMillis int64  `json:"maximum_backoff_ms,omitempty"`
}

// NormalizeJobRetryPolicy fails closed for legacy, invalid, or future policy
// values. A host must never turn malformed persisted data into permission to
// execute a worker more than once.
func NormalizeJobRetryPolicy(policy JobRetryPolicy) JobRetryPolicy {
	if policy == (JobRetryPolicy{}) {
		return JobRetryPolicy{MaxAttempts: 1}
	}
	if err := ValidateJobRetryPolicy(policy); err != nil {
		return JobRetryPolicy{MaxAttempts: 1}
	}
	return policy
}

// ValidateJobRetryPolicy validates a newly configured or persisted policy.
// The all-zero value is accepted for compatibility with legacy Job records
// and is normalized to a single attempt before execution.
func ValidateJobRetryPolicy(policy JobRetryPolicy) error {
	if policy == (JobRetryPolicy{}) {
		return nil
	}
	if policy.MaxAttempts == 0 || policy.MaxAttempts > maxJobRetryAttempts {
		return fmt.Errorf("%w: max_attempts must be between 1 and %d", ErrInvalidJobRetryPolicy, maxJobRetryAttempts)
	}
	if policy.MaxAttempts == 1 {
		if policy.InitialBackoffMillis != 0 || policy.MaximumBackoffMillis != 0 {
			return fmt.Errorf("%w: a single-attempt policy cannot define backoff", ErrInvalidJobRetryPolicy)
		}
		return nil
	}
	if policy.InitialBackoffMillis <= 0 || policy.InitialBackoffMillis > maxJobRetryBackoffMS {
		return fmt.Errorf("%w: initial_backoff_ms must be between 1 and %d", ErrInvalidJobRetryPolicy, maxJobRetryBackoffMS)
	}
	if policy.MaximumBackoffMillis < policy.InitialBackoffMillis || policy.MaximumBackoffMillis > maxJobRetryBackoffMS {
		return fmt.Errorf("%w: maximum_backoff_ms must be at least initial_backoff_ms and no more than %d", ErrInvalidJobRetryPolicy, maxJobRetryBackoffMS)
	}
	return nil
}

// ValidateJobRetryState validates the retry fields of a durable Job envelope.
func ValidateJobRetryState(policy JobRetryPolicy, attempt uint32, nextAttemptAt *time.Time) error {
	if err := ValidateJobRetryPolicy(policy); err != nil {
		return err
	}
	normalized := NormalizeJobRetryPolicy(policy)
	if attempt > normalized.MaxAttempts {
		return fmt.Errorf("%w: attempt exceeds retry policy", ErrInvalidJobRetryPolicy)
	}
	if nextAttemptAt == nil {
		return nil
	}
	if nextAttemptAt.IsZero() || attempt == 0 || attempt >= normalized.MaxAttempts {
		return fmt.Errorf("%w: invalid next_attempt_at retry state", ErrInvalidJobRetryPolicy)
	}
	return nil
}

// JobRetryBackoff returns the delay after a failed one-based attempt. A zero
// duration means no further attempt is authorized.
func JobRetryBackoff(policy JobRetryPolicy, failedAttempt uint32) time.Duration {
	policy = NormalizeJobRetryPolicy(policy)
	if failedAttempt == 0 || failedAttempt >= policy.MaxAttempts {
		return 0
	}
	delay := time.Duration(policy.InitialBackoffMillis) * time.Millisecond
	maximum := time.Duration(policy.MaximumBackoffMillis) * time.Millisecond
	for attempt := uint32(1); attempt < failedAttempt && delay < maximum; attempt++ {
		if delay > maximum/2 {
			delay = maximum
			break
		}
		delay *= 2
	}
	if delay > maximum {
		return maximum
	}
	return delay
}

type retryableJobError struct {
	cause error
}

func (e retryableJobError) Error() string { return e.cause.Error() }
func (e retryableJobError) Unwrap() error { return e.cause }
func (e retryableJobError) jobRetryable() {}

// MarkJobErrorRetryable is the explicit domain opt-in for automatic retry.
// Plain errors are never retried, even when the Job has more attempts.
func MarkJobErrorRetryable(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || IsJobErrorRetryable(err) {
		return err
	}
	return retryableJobError{cause: err}
}

// IsJobErrorRetryable reports whether the domain explicitly opted this error
// into the shared Job retry policy.
func IsJobErrorRetryable(err error) bool {
	var marker interface{ jobRetryable() }
	return errors.As(err, &marker)
}

type jobAttemptContextKey struct{}

// WithJobAttempt records the current one-based attempt for a worker without
// exposing host-private scheduler state.
func WithJobAttempt(ctx context.Context, attempt uint32) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if attempt == 0 {
		return ctx
	}
	return context.WithValue(ctx, jobAttemptContextKey{}, attempt)
}

// JobAttemptFromContext returns the current one-based attempt. Zero means the
// caller is not running under a shared Job scheduler.
func JobAttemptFromContext(ctx context.Context) uint32 {
	if ctx == nil {
		return 0
	}
	attempt, _ := ctx.Value(jobAttemptContextKey{}).(uint32)
	return attempt
}
