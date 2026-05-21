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

func RequireAdmin(admins *auth.AdminService, next http.HandlerFunc, tenantRepos ...store.TenantRepository) http.HandlerFunc {
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

		if !tenantAdminScopeActive(r.Context(), admin, tenantRepos...) {
			writeError(w, http.StatusUnauthorized, "ADMIN_UNAUTHORIZED", "Tenant admin scope is inactive")
			return
		}

		ctx := context.WithValue(r.Context(), adminUserContextKey, admin)
		next(w, r.WithContext(ctx))
	}
}

func tenantAdminScopeActive(ctx context.Context, admin *store.AdminUser, tenantRepos ...store.TenantRepository) bool {
	if admin == nil || !adminHasTenantScope(admin) {
		return true
	}
	if len(tenantRepos) == 0 || tenantRepos[0] == nil {
		return true
	}
	tenantID := strings.TrimSpace(admin.TenantID)
	if tenantID == "" {
		return false
	}
	tenant, err := tenantRepos[0].GetByID(ctx, tenantID)
	if err != nil || tenant == nil || tenant.DeletedAt != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(tenant.Status), "active")
}

func RequireGlobalAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if AdminFromContext(r.Context()) == nil || !IsGlobalAdmin(r.Context()) || strings.TrimSpace(r.URL.Query().Get("tenant_id")) != "" {
			writeError(w, http.StatusForbidden, "GLOBAL_ADMIN_REQUIRED", "Global admin authorization required")
			return
		}
		next(w, r)
	}
}

func RequireTenantAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin := AdminFromContext(r.Context())
		if admin == nil || !adminHasTenantScope(admin) {
			writeError(w, http.StatusForbidden, "TENANT_ADMIN_REQUIRED", "Tenant admin authorization required")
			return
		}
		if queryTenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id")); queryTenantID != "" && queryTenantID != AdminTenantID(r.Context()) {
			writeError(w, http.StatusForbidden, "TENANT_FORBIDDEN", "Tenant access denied")
			return
		}
		next(w, r)
	}
}

func AdminFromContext(ctx context.Context) *store.AdminUser {
	admin, _ := ctx.Value(adminUserContextKey).(*store.AdminUser)
	return admin
}

func adminHasTenantScope(admin *store.AdminUser) bool {
	return admin != nil && strings.EqualFold(strings.TrimSpace(admin.Scope), "tenant")
}

func AdminTenantID(ctx context.Context) string {
	admin := AdminFromContext(ctx)
	if adminHasTenantScope(admin) && strings.TrimSpace(admin.TenantID) != "" {
		return strings.TrimSpace(admin.TenantID)
	}
	return store.DefaultTenantID
}

func IsGlobalAdmin(ctx context.Context) bool {
	admin := AdminFromContext(ctx)
	return admin != nil && !adminHasTenantScope(admin)
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
	if adminHasTenantScope(admin) {
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
