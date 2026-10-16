package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// ApprovalContext is injected by a trusted host after an explicit user
// approval. The model-facing database arguments intentionally cannot carry the
// token: accepting a token from tool JSON would turn an untrusted model output
// into an authorization decision.
type ApprovalContext struct {
	Token             string    `json:"-"`
	ID                string    `json:"-"`
	SQLFingerprint    string    `json:"-"`
	ParamsFingerprint string    `json:"-"`
	ProfileID         string    `json:"-"`
	SchemaVersion     int       `json:"-"`
	ExpiresAt         time.Time `json:"-"`
	OperationID       string    `json:"-"`
	Attempt           int       `json:"-"`
	ParentActionID    string    `json:"-"`
}

type approvalContextKey struct{}

// WithApprovalContext attaches a host-owned approval to a database operation.
// The context is request-scoped and must not be persisted or returned to the
// model. ID is an audit-safe identifier; when omitted, a short digest of the
// token is derived locally.
func WithApprovalContext(ctx context.Context, approval ApprovalContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	approval.Token = strings.TrimSpace(approval.Token)
	approval.ID = strings.TrimSpace(approval.ID)
	approval.SQLFingerprint = strings.TrimSpace(approval.SQLFingerprint)
	approval.ParamsFingerprint = strings.TrimSpace(approval.ParamsFingerprint)
	approval.ProfileID = strings.TrimSpace(approval.ProfileID)
	approval.OperationID = strings.TrimSpace(approval.OperationID)
	approval.ParentActionID = strings.TrimSpace(approval.ParentActionID)
	if approval.Attempt < 0 {
		approval.Attempt = 0
	}
	if approval.Token == "" {
		return ctx
	}
	if approval.ID == "" {
		digest := sha256.Sum256([]byte(approval.Token))
		approval.ID = "db-appr-" + hex.EncodeToString(digest[:8])
	}
	return context.WithValue(ctx, approvalContextKey{}, approval)
}

// WithApprovalToken is a convenience for hosts that only have the opaque
// token. It derives a non-secret audit ID and never exposes the token to tool
// arguments or result projections.
func WithApprovalToken(ctx context.Context, token string) context.Context {
	return WithApprovalContext(ctx, ApprovalContext{Token: token})
}

// ContextHasApproval reports whether a trusted host already injected a token.
func ContextHasApproval(ctx context.Context) bool {
	return approvalFromContext(ctx).Token != ""
}

func approvalFromContext(ctx context.Context) ApprovalContext {
	if ctx == nil {
		return ApprovalContext{}
	}
	approval, _ := ctx.Value(approvalContextKey{}).(ApprovalContext)
	approval.Token = strings.TrimSpace(approval.Token)
	approval.ID = strings.TrimSpace(approval.ID)
	approval.SQLFingerprint = strings.TrimSpace(approval.SQLFingerprint)
	approval.ParamsFingerprint = strings.TrimSpace(approval.ParamsFingerprint)
	approval.ProfileID = strings.TrimSpace(approval.ProfileID)
	approval.OperationID = strings.TrimSpace(approval.OperationID)
	approval.ParentActionID = strings.TrimSpace(approval.ParentActionID)
	if approval.Attempt < 0 {
		approval.Attempt = 0
	}
	return approval
}

// MutationNeedsHostApproval reports whether args are a committing write that
// HandleTool will reject without a host-injected approval context. Dry-run
// (the default) and malformed mutations do not qualify: the model cannot
// supply a token, so hosts must prompt and inject one themselves.
func MutationNeedsHostApproval(ctx context.Context, args map[string]interface{}) bool {
	if args == nil || boolArg(args, "dry_run", true) || ContextHasApproval(ctx) {
		return false
	}
	_, _, err := MutationFingerprints(args)
	return err == nil
}
