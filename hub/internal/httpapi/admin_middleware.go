package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type adminContextKey string
type requestTenantContextKey struct{}

const adminUserContextKey adminContextKey = "admin_user"

const DefaultTenantID = store.DefaultTenantID

func RequireAdmin(admins *auth.AdminService, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authz := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(strings.ToLower(authz), "bearer ") {
			writeError(w, http.StatusUnauthorized, "ADMIN_UNAUTHORIZED", "Admin authorization required")
			return
		}

		token := strings.TrimSpace(authz[len("Bearer "):])
		admin, err := admins.Authenticate(r.Context(), token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "ADMIN_UNAUTHORIZED", "Invalid admin token")
			return
		}

		ctx := context.WithValue(r.Context(), adminUserContextKey, admin)
		next(w, r.WithContext(ctx))
	}
}

func AdminFromContext(ctx context.Context) *store.AdminUser {
	admin, _ := ctx.Value(adminUserContextKey).(*store.AdminUser)
	return admin
}

func AdminTenantID(ctx context.Context) string {
	admin := AdminFromContext(ctx)
	if admin != nil && strings.TrimSpace(admin.Scope) == "tenant" && strings.TrimSpace(admin.TenantID) != "" {
		return strings.TrimSpace(admin.TenantID)
	}
	return store.DefaultTenantID
}

func IsGlobalAdmin(ctx context.Context) bool {
	admin := AdminFromContext(ctx)
	return admin != nil && strings.TrimSpace(admin.Scope) != "tenant"
}

func RequestTenantID(r *http.Request) string {
	if r == nil {
		return store.DefaultTenantID
	}
	if value := r.Context().Value(requestTenantContextKey{}); value != nil {
		if tenantID, ok := value.(string); ok && strings.TrimSpace(tenantID) != "" {
			return strings.TrimSpace(tenantID)
		}
	}
	admin := AdminFromContext(r.Context())
	if admin != nil && strings.TrimSpace(admin.Scope) == "tenant" {
		return AdminTenantID(r.Context())
	}
	if tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantID != "" {
		return tenantID
	}
	return store.DefaultTenantID
}

func WithRequestTenant(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, requestTenantContextKey{}, strings.TrimSpace(tenantID))
}
