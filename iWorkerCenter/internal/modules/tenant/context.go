package tenant

import "context"

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
