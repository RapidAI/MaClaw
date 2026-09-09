package database

import (
	"context"
	"strings"
)

// RequestScope identifies the authenticated owner/session for a database
// request. It contains no credentials and is safe to inject into a context by
// GUI, srv or TUI composition roots. Tool arguments remain model-controlled;
// HandleTool uses this scope only as a trusted fallback for connection
// binding/audit metadata.
type RequestScope struct {
	OwnerID        string
	SessionID      string
	OperationID    string
	Attempt        int
	ParentActionID string
}

type requestScopeKey struct{}

// WithRequestScope attaches host-authenticated ownership metadata to a single
// tool invocation. The returned context is immutable; values are trimmed and
// copied so callers cannot mutate an in-flight scope through shared strings.
func WithRequestScope(ctx context.Context, scope RequestScope) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	scope.OwnerID = strings.TrimSpace(scope.OwnerID)
	scope.SessionID = strings.TrimSpace(scope.SessionID)
	scope.OperationID = strings.TrimSpace(scope.OperationID)
	scope.ParentActionID = strings.TrimSpace(scope.ParentActionID)
	if scope.Attempt < 0 {
		scope.Attempt = 0
	}
	if scope.OwnerID == "" && scope.SessionID == "" && scope.OperationID == "" && scope.ParentActionID == "" && scope.Attempt == 0 {
		return ctx
	}
	return context.WithValue(ctx, requestScopeKey{}, scope)
}

func requestScopeFromContext(ctx context.Context) RequestScope {
	if ctx == nil {
		return RequestScope{}
	}
	scope, _ := ctx.Value(requestScopeKey{}).(RequestScope)
	scope.OwnerID = strings.TrimSpace(scope.OwnerID)
	scope.SessionID = strings.TrimSpace(scope.SessionID)
	scope.OperationID = strings.TrimSpace(scope.OperationID)
	scope.ParentActionID = strings.TrimSpace(scope.ParentActionID)
	if scope.Attempt < 0 {
		scope.Attempt = 0
	}
	return scope
}

// RequestScopeFrom returns the host-injected owner/session metadata, if any.
func RequestScopeFrom(ctx context.Context) RequestScope {
	return requestScopeFromContext(ctx)
}
