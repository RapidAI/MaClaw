package tenant

import (
	"context"
	"net/http"
	"strings"
)

type tenantContextKey struct{}

// WithTenantID returns a new context carrying the tenant ID.
func WithTenantID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, id)
}

// TenantIDFromContext extracts the tenant ID from the context.
// Returns empty string if not set.
func TenantIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(tenantContextKey{}).(string)
	return v
}

// RequestTenantID resolves tenant identity from authenticated context, query, header, then default.
// iWorker clients send X-Tenant-ID so tenant routing remains stable through gateways;
// admin requests with a valid session use the session tenant over client-provided tenant hints.
func RequestTenantID(r *http.Request) string {
	if r == nil {
		return "default"
	}
	for _, value := range []string{TenantIDFromContext(r.Context()), r.URL.Query().Get("tenant_id"), r.Header.Get("X-Tenant-ID"), "default"} {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "default"
}
