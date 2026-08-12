package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type AdminLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Tenant   string `json:"tenant,omitempty"`
}

type AdminChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type AdminUpdateProfileRequest struct {
	Email string `json:"email"`
}

func AdminLoginHandler(admins *auth.AdminService, tenantRepos ...store.TenantRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req AdminLoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.Username == "" || req.Password == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Username and password are required")
			return
		}

		tenantScope := strings.TrimSpace(req.Tenant)
		if tenantScope == "" {
			tenantScope = auth.ExplicitGlobalAdminTenantScope
		}
		if !requestedTenantLoginAllowed(r.Context(), tenantScope, tenantRepos...) {
			writeError(w, http.StatusUnauthorized, "LOGIN_FAILED", "Invalid username or password")
			return
		}

		token, admin, err := admins.LoginScoped(r.Context(), req.Username, req.Password, tenantScope)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "LOGIN_FAILED", "Invalid username or password")
			return
		}
		if !adminTenantLoginAllowed(r.Context(), admin, tenantRepos...) {
			writeError(w, http.StatusUnauthorized, "LOGIN_FAILED", "Invalid username or password")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"access_token": token,
			"expires_in":   7200,
			"admin":        adminLoginDTO(r.Context(), admin, tenantRepos...),
		})
	}
}

func adminLoginDTO(ctx context.Context, admin *store.AdminUser, tenantRepos ...store.TenantRepository) map[string]any {
	out := adminDTO(admin)
	if admin == nil || !strings.EqualFold(strings.TrimSpace(admin.Scope), "tenant") || len(tenantRepos) == 0 || tenantRepos[0] == nil {
		return out
	}
	tenantID := strings.TrimSpace(admin.TenantID)
	if tenantID == "" || strings.EqualFold(tenantID, auth.ExplicitGlobalAdminTenantScope) {
		return out
	}
	tenant, err := tenantRepos[0].GetByID(ctx, tenantID)
	if err != nil || tenant == nil {
		return out
	}
	out["tenant_name"] = strings.TrimSpace(tenant.Name)
	out["tenant_slug"] = strings.TrimSpace(tenant.Slug)
	return out
}

func requestedTenantLoginAllowed(ctx context.Context, tenantID string, tenantRepos ...store.TenantRepository) bool {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" || strings.EqualFold(tenantID, auth.ExplicitGlobalAdminTenantScope) {
		return true
	}
	if strings.EqualFold(tenantID, auth.ExplicitGlobalAdminTenantScope) {
		return false
	}
	return tenantLoginScopeActive(ctx, tenantID, tenantRepos...)
}

func adminTenantLoginAllowed(ctx context.Context, admin *store.AdminUser, tenantRepos ...store.TenantRepository) bool {
	if admin == nil || !strings.EqualFold(strings.TrimSpace(admin.Scope), "tenant") {
		return true
	}
	if len(tenantRepos) == 0 || tenantRepos[0] == nil {
		return true
	}
	tenantID := strings.TrimSpace(admin.TenantID)
	if tenantID == "" || strings.EqualFold(tenantID, auth.ExplicitGlobalAdminTenantScope) {
		return false
	}
	return tenantLoginScopeActive(ctx, tenantID, tenantRepos...)
}

func tenantLoginScopeActive(ctx context.Context, tenantID string, tenantRepos ...store.TenantRepository) bool {
	if len(tenantRepos) == 0 || tenantRepos[0] == nil {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(tenantID), store.DefaultTenantID) {
		_, _ = tenantRepos[0].EnsureDefault(ctx)
	}
	tenant, err := tenantRepos[0].GetByID(ctx, strings.TrimSpace(tenantID))
	if err != nil || tenant == nil || tenant.DeletedAt != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(tenant.Status), "active")
}

func AdminChangePasswordHandler(admins *auth.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin := AdminFromContext(r.Context())
		if admin == nil {
			writeError(w, http.StatusUnauthorized, "ADMIN_UNAUTHORIZED", "Admin authorization required")
			return
		}

		var req AdminChangePasswordRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if strings.TrimSpace(req.CurrentPassword) == "" || strings.TrimSpace(req.NewPassword) == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Current password and new password are required")
			return
		}

		token, updatedAdmin, err := admins.ChangePasswordScoped(r.Context(), admin.Username, req.CurrentPassword, req.NewPassword, admin.Scope, admin.TenantID)
		if err != nil {
			if err == auth.ErrInvalidAdminPassword {
				writeError(w, http.StatusUnauthorized, "INVALID_PASSWORD", "Current password is incorrect")
				return
			}
			if isAdminValidationError(err) {
				writeError(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "CHANGE_PASSWORD_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":           true,
			"access_token": token,
			"admin":        adminDTO(updatedAdmin),
		})
	}
}

func AdminUpdateProfileHandler(admins *auth.AdminService, routeSyncers ...tenantAdminRouteSyncer) http.HandlerFunc {
	routeSyncer := firstTenantAdminRouteSyncer(routeSyncers)
	return func(w http.ResponseWriter, r *http.Request) {
		admin := AdminFromContext(r.Context())
		if admin == nil {
			writeError(w, http.StatusUnauthorized, "ADMIN_UNAUTHORIZED", "Admin authorization required")
			return
		}

		var req AdminUpdateProfileRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.Email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email is required")
			return
		}

		token, updatedAdmin, err := admins.UpdateEmailScoped(r.Context(), admin.Username, req.Email, admin.Scope, admin.TenantID)
		if err != nil {
			if isAdminValidationError(err) {
				writeError(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "UPDATE_PROFILE_FAILED", err.Error())
			return
		}
		if strings.EqualFold(strings.TrimSpace(updatedAdmin.Scope), "tenant") {
			syncTenantAdminRoute(r.Context(), routeSyncer, updatedAdmin.TenantID, updatedAdmin.Email)
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":           true,
			"access_token": token,
			"admin":        adminDTO(updatedAdmin),
		})
	}
}

func isAdminValidationError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "required") || strings.Contains(msg, "valid admin email")
}
