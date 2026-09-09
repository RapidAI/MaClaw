package agentservice

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrUnauthorized       = errors.New("unauthorized")
	ErrForbidden          = errors.New("forbidden")
	ErrTenantNotFound     = errors.New("tenant not found")
	ErrUserNotFound       = errors.New("user not found")
	ErrUserConfigNotFound = errors.New("user config not found")
	ErrInstanceNotFound   = errors.New("instance not found")
	ErrSessionNotFound    = errors.New("session not found")
	ErrRunNotFound        = errors.New("run not found")
	ErrSnapshotNotFound   = errors.New("snapshot not found")
	ErrRecordNotFound     = errors.New("record not found")
	ErrRunNotRunning      = errors.New("run is not running")
	ErrInstanceBusy       = errors.New("instance has running runs")
	ErrUserBusy           = errors.New("user has running runs")
	ErrTenantBusy         = errors.New("tenant has running runs")
	ErrDeleteProtected    = errors.New("resource is delete-protected")
	ErrSessionBusy        = errors.New("session has running runs")
	ErrSessionArchived    = errors.New("session is archived")
	ErrCredentialNotFound = errors.New("credential not found")
	ErrQuotaExceeded      = errors.New("quota exceeded")
	ErrRateLimited        = errors.New("runtime rate limited")
	ErrInvalidConfig      = errors.New("invalid config")
	ErrAlreadyExists      = errors.New("resource already exists")
	ErrServiceClosed      = errors.New("service is closed")
	// ErrJobPersistence indicates that an asynchronous job could not be
	// durably recorded. Callers must not treat the operation as successfully
	// admitted because a restart may otherwise lose it.
	ErrJobPersistence = errors.New("async job persistence failed")
)

// RateLimitError carries a bounded retry hint while preserving a stable
// errors.Is/ ErrorCode identity for every transport. The duration is never
// derived from caller-controlled text and is safe to expose as Retry-After.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	if e == nil || e.RetryAfter <= 0 {
		return ErrRateLimited.Error()
	}
	return fmt.Sprintf("%s: retry after %s", ErrRateLimited, e.RetryAfter.Round(time.Millisecond))
}

func (e *RateLimitError) Unwrap() error { return ErrRateLimited }

// ErrorCode returns the stable, transport-neutral identifier for a Service
// error. Callers should branch on this value (or errors.Is in-process) rather
// than parsing localized human-readable text. Unknown errors intentionally
// collapse to request_error so implementation details are not exposed by
// public API contracts.
func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, context.Canceled):
		return "request_canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "request_deadline_exceeded"
	case errors.Is(err, ErrUnauthorized):
		return "unauthorized"
	case errors.Is(err, ErrForbidden):
		return "forbidden"
	case errors.Is(err, ErrTenantNotFound):
		return "tenant_not_found"
	case errors.Is(err, ErrUserNotFound):
		return "user_not_found"
	case errors.Is(err, ErrUserConfigNotFound):
		return "user_config_not_found"
	case errors.Is(err, ErrInstanceNotFound):
		return "instance_not_found"
	case errors.Is(err, ErrSessionNotFound):
		return "session_not_found"
	case errors.Is(err, ErrRunNotFound):
		return "run_not_found"
	case errors.Is(err, ErrSnapshotNotFound):
		return "snapshot_not_found"
	case errors.Is(err, ErrRecordNotFound):
		return "record_not_found"
	case errors.Is(err, ErrCredentialNotFound):
		return "credential_not_found"
	case errors.Is(err, ErrRunNotRunning):
		return "run_not_running"
	case errors.Is(err, ErrInstanceBusy):
		return "instance_busy"
	case errors.Is(err, ErrUserBusy):
		return "user_busy"
	case errors.Is(err, ErrTenantBusy):
		return "tenant_busy"
	case errors.Is(err, ErrDeleteProtected):
		return "delete_protected"
	case errors.Is(err, ErrSessionBusy):
		return "session_busy"
	case errors.Is(err, ErrSessionArchived):
		return "session_archived"
	case errors.Is(err, ErrQuotaExceeded):
		return "quota_exceeded"
	case errors.Is(err, ErrRateLimited):
		return "rate_limited"
	case errors.Is(err, ErrInvalidConfig):
		return "invalid_config"
	case errors.Is(err, ErrAlreadyExists):
		return "already_exists"
	case errors.Is(err, ErrServiceClosed):
		return "service_closed"
	case errors.Is(err, ErrJobPersistence):
		return "job_persistence_failed"
	default:
		return "request_error"
	}
}
