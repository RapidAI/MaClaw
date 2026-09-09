package agentruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const maxJobIdempotencyKeyBytes = 256

var (
	ErrInvalidJobIdempotency  = errors.New("invalid job idempotency identity or state")
	ErrJobIdempotencyConflict = errors.New("job idempotency key conflicts with a different request")
)

// JobIdempotencyIdentity contains only fixed-size SHA-256 digests. The raw client
// key and request payload must never enter a durable Job envelope or worker
// context because either may contain credentials or private parameters.
type JobIdempotencyIdentity struct {
	Digest        string
	RequestDigest string
}

// NewJobIdempotencyIdentity scopes a client key to one tenant/user/job kind
// and hashes the request payload. JSON object keys are encoded deterministically
// by encoding/json, so semantically identical Go request values are stable.
func NewJobIdempotencyIdentity(tenantID, userID, kind, key string, request any) (JobIdempotencyIdentity, error) {
	tenantID = strings.TrimSpace(tenantID)
	userID = strings.TrimSpace(userID)
	kind = strings.TrimSpace(kind)
	if tenantID == "" || userID == "" || kind == "" {
		return JobIdempotencyIdentity{}, fmt.Errorf("%w: tenant, user, and kind are required", ErrInvalidJobIdempotency)
	}
	if err := validateRawJobIdempotencyKey(key); err != nil {
		return JobIdempotencyIdentity{}, err
	}
	requestPayload, err := json.Marshal(request)
	if err != nil {
		return JobIdempotencyIdentity{}, fmt.Errorf("%w: request is not serializable: %v", ErrInvalidJobIdempotency, err)
	}
	scopePayload, err := json.Marshal([]string{"maclaw.job-idempotency/v1", tenantID, userID, kind, key})
	if err != nil {
		return JobIdempotencyIdentity{}, fmt.Errorf("%w: encode scope: %v", ErrInvalidJobIdempotency, err)
	}
	return JobIdempotencyIdentity{
		Digest:        sha256Hex(scopePayload),
		RequestDigest: sha256Hex(requestPayload),
	}, nil
}

func validateRawJobIdempotencyKey(key string) error {
	if strings.TrimSpace(key) != key || key == "" || len(key) > maxJobIdempotencyKeyBytes {
		return fmt.Errorf("%w: key must contain 1-%d bytes without surrounding whitespace", ErrInvalidJobIdempotency, maxJobIdempotencyKeyBytes)
	}
	for _, r := range key {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: key contains control characters", ErrInvalidJobIdempotency)
		}
	}
	return nil
}

func sha256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// ValidateJobIdempotencyState validates fields read from or written to a Job
// repository. The response-only replay bit must never be persisted.
func ValidateJobIdempotencyState(digest, requestDigest string, idempotentReplay bool) error {
	if digest == "" && requestDigest == "" {
		if idempotentReplay {
			return fmt.Errorf("%w: replay marker is response-only", ErrInvalidJobIdempotency)
		}
		return nil
	}
	if !validSHA256Hex(digest) || !validSHA256Hex(requestDigest) {
		return fmt.Errorf("%w: digest and request_digest must be lowercase SHA-256 values", ErrInvalidJobIdempotency)
	}
	if idempotentReplay {
		return fmt.Errorf("%w: replay marker is response-only", ErrInvalidJobIdempotency)
	}
	return nil
}

func validSHA256Hex(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

type jobIdempotencyContextKey struct{}

// WithJobIdempotencyIdentity exposes only safe digests to domain workers so
// they can bind an operation ledger without importing an HTTP host.
func WithJobIdempotencyIdentity(ctx context.Context, identity JobIdempotencyIdentity) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if ValidateJobIdempotencyState(identity.Digest, identity.RequestDigest, false) != nil || identity.Digest == "" {
		return ctx
	}
	return context.WithValue(ctx, jobIdempotencyContextKey{}, identity)
}

// JobIdempotencyIdentityFromContext returns safe digests for the current job.
func JobIdempotencyIdentityFromContext(ctx context.Context) (JobIdempotencyIdentity, bool) {
	if ctx == nil {
		return JobIdempotencyIdentity{}, false
	}
	identity, ok := ctx.Value(jobIdempotencyContextKey{}).(JobIdempotencyIdentity)
	return identity, ok
}
