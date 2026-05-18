package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type tenantCreateRequest struct {
	ID                   string `json:"id"`
	Slug                 string `json:"slug"`
	Name                 string `json:"name"`
	PrimaryDomain        string `json:"primary_domain"`
	InitialAdminUsername string `json:"initial_admin_username"`
	InitialAdminPassword string `json:"initial_admin_password"`
	InitialAdminEmail    string `json:"initial_admin_email"`
	InitialAdminName     string `json:"initial_admin_name"`
}

type tenantAdminCreateRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
}

func AdminTenantsListHandler(tenants store.TenantRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !IsGlobalAdmin(r.Context()) {
			writeError(w, http.StatusForbidden, "GLOBAL_ADMIN_REQUIRED", "Global admin authorization required")
			return
		}
		items, err := tenants.List(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "TENANTS_LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tenants": tenantDTOs(items)})
	}
}

func AdminTenantCreateHandler(tenants store.TenantRepository, admins *auth.AdminService, audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := AdminFromContext(r.Context())
		if actor == nil || !IsGlobalAdmin(r.Context()) {
			writeError(w, http.StatusForbidden, "GLOBAL_ADMIN_REQUIRED", "Global admin authorization required")
			return
		}
		var req tenantCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		slug := normalizeTenantSlug(req.Slug)
		name := strings.TrimSpace(req.Name)
		if slug == "" || name == "" || strings.TrimSpace(req.InitialAdminUsername) == "" || strings.TrimSpace(req.InitialAdminPassword) == "" || strings.TrimSpace(req.InitialAdminEmail) == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Tenant slug, name, and initial admin credentials are required")
			return
		}
		tenantID := strings.TrimSpace(req.ID)
		if tenantID == "" {
			tenantID = "tenant_" + slug
		}
		now := time.Now()
		tenant := &store.Tenant{
			ID:               tenantID,
			Slug:             slug,
			Name:             name,
			Status:           "active",
			PrimaryDomain:    normalizeDomain(req.PrimaryDomain),
			SettingsJSON:     "{}",
			CreatedByAdminID: actor.ID,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if err := tenants.Create(r.Context(), tenant); err != nil {
			writeError(w, http.StatusConflict, "TENANT_CREATE_FAILED", err.Error())
			return
		}
		admin, err := admins.CreateTenantAdmin(r.Context(), tenant.ID, req.InitialAdminUsername, req.InitialAdminPassword, req.InitialAdminEmail, req.InitialAdminName, "tenant_owner")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "TENANT_ADMIN_CREATE_FAILED", err.Error())
			return
		}
		writeAdminAuditLog(r.Context(), audit, actor.ID, "tenant.created", map[string]any{"tenant_id": tenant.ID, "tenant_slug": tenant.Slug, "initial_admin_id": admin.ID})
		writeJSON(w, http.StatusCreated, map[string]any{"tenant": tenantDTO(tenant), "admin": adminDTO(admin)})
	}
}

func AdminTenantDetailHandler(tenants store.TenantRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := strings.TrimSpace(r.PathValue("tenantId"))
		if tenantID == "" {
			writeError(w, http.StatusBadRequest, "TENANT_REQUIRED", "Tenant id is required")
			return
		}
		if !IsGlobalAdmin(r.Context()) && AdminTenantID(r.Context()) != tenantID {
			writeError(w, http.StatusForbidden, "TENANT_FORBIDDEN", "Tenant access denied")
			return
		}
		tenant, err := tenants.GetByID(r.Context(), tenantID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "TENANT_LOAD_FAILED", err.Error())
			return
		}
		if tenant == nil {
			writeError(w, http.StatusNotFound, "TENANT_NOT_FOUND", "Tenant not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tenant": tenantDTO(tenant)})
	}
}

func AdminTenantAdminCreateHandler(tenants store.TenantRepository, admins *auth.AdminService, audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := AdminFromContext(r.Context())
		tenantID := strings.TrimSpace(r.PathValue("tenantId"))
		if tenantID == "" {
			writeError(w, http.StatusBadRequest, "TENANT_REQUIRED", "Tenant id is required")
			return
		}
		if actor == nil || (!IsGlobalAdmin(r.Context()) && AdminTenantID(r.Context()) != tenantID) {
			writeError(w, http.StatusForbidden, "TENANT_FORBIDDEN", "Tenant access denied")
			return
		}
		tenant, err := tenants.GetByID(r.Context(), tenantID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "TENANT_LOAD_FAILED", err.Error())
			return
		}
		if tenant == nil {
			writeError(w, http.StatusNotFound, "TENANT_NOT_FOUND", "Tenant not found")
			return
		}
		var req tenantAdminCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		admin, err := admins.CreateTenantAdmin(r.Context(), tenantID, req.Username, req.Password, req.Email, req.DisplayName, req.Role)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "TENANT_ADMIN_CREATE_FAILED", err.Error())
			return
		}
		writeAdminAuditLog(r.Context(), audit, actor.ID, "tenant_admin.created", map[string]any{"tenant_id": tenantID, "admin_id": admin.ID, "role": admin.Role})
		writeJSON(w, http.StatusCreated, map[string]any{"admin": adminDTO(admin)})
	}
}

func tenantDTOs(items []*store.Tenant) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, tenantDTO(item))
	}
	return out
}

func tenantDTO(t *store.Tenant) map[string]any {
	if t == nil {
		return map[string]any{}
	}
	out := map[string]any{
		"id":             t.ID,
		"slug":           t.Slug,
		"name":           t.Name,
		"status":         t.Status,
		"primary_domain": t.PrimaryDomain,
		"settings_json":  t.SettingsJSON,
		"created_at":     t.CreatedAt.Format(time.RFC3339),
		"updated_at":     t.UpdatedAt.Format(time.RFC3339),
	}
	if t.DeletedAt != nil {
		out["deleted_at"] = t.DeletedAt.Format(time.RFC3339)
	}
	return out
}

func adminDTO(admin *store.AdminUser) map[string]any {
	if admin == nil {
		return map[string]any{}
	}
	return map[string]any{
		"id":           admin.ID,
		"username":     admin.Username,
		"email":        admin.Email,
		"scope":        admin.Scope,
		"role":         admin.Role,
		"tenant_id":    admin.TenantID,
		"display_name": admin.DisplayName,
		"status":       admin.Status,
	}
}

func normalizeTenantSlug(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	var b strings.Builder
	lastDash := false
	for _, r := range v {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func normalizeDomain(v string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(v)), ".")
}
