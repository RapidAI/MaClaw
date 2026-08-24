package main

import (
	"context"
	"strings"
)

type trustedAuditPrincipalContextKey struct{}

func localSessionIDFromToolArgs(args map[string]interface{}) string {
	if args == nil {
		return "local"
	}
	for _, key := range []string{"session_id", "browser_session_id"} {
		if value, ok := args[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "local"
}

func withTrustedAuditPrincipal(ctx context.Context, principalID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return ctx
	}
	return context.WithValue(ctx, trustedAuditPrincipalContextKey{}, principalID)
}

func trustedAuditPrincipalFromContext(ctx context.Context, fallback string) string {
	if ctx != nil {
		if id, ok := ctx.Value(trustedAuditPrincipalContextKey{}).(string); ok {
			if id = strings.TrimSpace(id); id != "" {
				return id
			}
		}
	}
	return strings.TrimSpace(fallback)
}

func trustedAuditPrincipalFromSecurityContext(ctx *SecurityCallContext, fallback string) string {
	if ctx != nil {
		if id := strings.TrimSpace(ctx.UserID); id != "" {
			return id
		}
	}
	return strings.TrimSpace(fallback)
}
