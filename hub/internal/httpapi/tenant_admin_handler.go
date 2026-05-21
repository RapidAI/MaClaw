package httpapi

import (
	"encoding/json"
	"net/http"
	"regexp"
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

var tenantIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{2,63}$`)

type tenantAdminCreateRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
}

func AdminLoginTenantsHandler(tenants store.TenantRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := tenants.List(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "TENANTS_LIST_FAILED", err.Error())
			return
		}
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if item == nil || item.DeletedAt != nil || isReservedTenantID(item.ID) || !strings.EqualFold(strings.TrimSpace(item.Status), "active") {
				continue
			}
			out = append(out, tenantLoginOptionDTO(item))
		}
		writeJSON(w, http.StatusOK, map[string]any{"tenants": out})
	}
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
		tenantID := normalizeTenantIDInput(req.ID)
		if tenantID == "" {
			tenantID = "tenant_" + slug
		}
		if isReservedTenantID(tenantID) || !isValidTenantID(tenantID) {
			writeError(w, http.StatusBadRequest, "INVALID_TENANT_ID", "Tenant id must start with a letter and contain only lowercase letters, numbers, underscores, or hyphens")
			return
		}
		now := time.Now()
		tenant := &store.Tenant{ID: tenantID, Slug: slug, Name: name, Status: "active", PrimaryDomain: normalizeDomain(req.PrimaryDomain), SettingsJSON: "{}", CreatedByAdminID: actor.ID, CreatedAt: now, UpdatedAt: now}
		if err := tenants.Create(r.Context(), tenant); err != nil {
			writeError(w, http.StatusConflict, "TENANT_CREATE_FAILED", err.Error())
			return
		}
		admin, err := admins.CreateTenantAdmin(r.Context(), tenant.ID, req.InitialAdminUsername, req.InitialAdminPassword, req.InitialAdminEmail, req.InitialAdminName, "tenant_owner")
		if err != nil {
			_ = tenants.DeleteByID(r.Context(), tenant.ID)
			status, code := tenantAdminCreateError(err)
			writeError(w, status, code, err.Error())
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
		if tenant.DeletedAt != nil || !strings.EqualFold(strings.TrimSpace(tenant.Status), "active") {
			writeError(w, http.StatusBadRequest, "TENANT_INACTIVE", "Tenant must be active to create tenant admins")
			return
		}
		var req tenantAdminCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		requestedRole := strings.TrimSpace(req.Role)
		if requestedRole == "" {
			requestedRole = "tenant_admin"
		}
		if !isAssignableTenantAdminRole(requestedRole, IsGlobalAdmin(r.Context())) {
			if strings.EqualFold(requestedRole, "tenant_owner") {
				writeError(w, http.StatusForbidden, "TENANT_OWNER_FORBIDDEN", "Only global admins can create tenant owners")
				return
			}
			writeError(w, http.StatusBadRequest, "INVALID_TENANT_ADMIN_ROLE", "Invalid tenant admin role")
			return
		}
		admin, err := admins.CreateTenantAdmin(r.Context(), tenantID, req.Username, req.Password, req.Email, req.DisplayName, requestedRole)
		if err != nil {
			status, code := tenantAdminCreateError(err)
			writeError(w, status, code, err.Error())
			return
		}
		writeAdminAuditLog(r.Context(), audit, actor.ID, "tenant_admin.created", map[string]any{"tenant_id": tenantID, "admin_id": admin.ID, "role": admin.Role})
		writeJSON(w, http.StatusCreated, map[string]any{"admin": adminDTO(admin)})
	}
}

func tenantAdminCreateError(err error) (int, string) {
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(msg, "required") || strings.Contains(msg, "reserved") || strings.Contains(msg, "valid admin email") {
		return http.StatusBadRequest, "INVALID_TENANT_ADMIN"
	}
	if strings.Contains(msg, "unique constraint") || strings.Contains(msg, "constraint failed") {
		return http.StatusConflict, "TENANT_ADMIN_CONFLICT"
	}
	return http.StatusInternalServerError, "TENANT_ADMIN_CREATE_FAILED"
}

func isAssignableTenantAdminRole(role string, allowOwner bool) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "tenant_admin":
		return true
	case "tenant_owner":
		return allowOwner
	default:
		return false
	}
}

func tenantDTOs(items []*store.Tenant) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, tenantDTO(item))
	}
	return out
}

func isReservedTenantID(id string) bool {
	return strings.EqualFold(strings.TrimSpace(id), auth.ExplicitGlobalAdminTenantScope)
}
func normalizeTenantIDInput(id string) string { return strings.ToLower(strings.TrimSpace(id)) }
func isValidTenantID(id string) bool          { return tenantIDPattern.MatchString(strings.TrimSpace(id)) }

func tenantLoginOptionDTO(t *store.Tenant) map[string]any {
	if t == nil {
		return map[string]any{}
	}
	return map[string]any{"id": t.ID, "slug": t.Slug, "name": t.Name, "primary_domain": t.PrimaryDomain}
}

func tenantDTO(t *store.Tenant) map[string]any {
	if t == nil {
		return map[string]any{}
	}
	out := map[string]any{"id": t.ID, "slug": t.Slug, "name": t.Name, "status": t.Status, "primary_domain": t.PrimaryDomain, "settings_json": t.SettingsJSON, "created_at": t.CreatedAt.Format(time.RFC3339), "updated_at": t.UpdatedAt.Format(time.RFC3339)}
	if t.DeletedAt != nil {
		out["deleted_at"] = t.DeletedAt.Format(time.RFC3339)
	}
	return out
}

func adminDTO(admin *store.AdminUser) map[string]any {
	if admin == nil {
		return map[string]any{}
	}
	return map[string]any{"id": admin.ID, "username": admin.Username, "email": admin.Email, "scope": adminScopeDTO(admin.Scope), "role": adminRoleDTO(admin.Scope, admin.Role), "tenant_id": strings.TrimSpace(admin.TenantID), "display_name": admin.DisplayName, "status": admin.Status}
}

func adminScopeDTO(scope string) string {
	if strings.EqualFold(strings.TrimSpace(scope), "tenant") {
		return "tenant"
	}
	return "global"
}

func adminRoleDTO(scope string, role string) string {
	role = strings.TrimSpace(role)
	switch strings.ToLower(role) {
	case "tenant_owner":
		return "tenant_owner"
	case "tenant_admin":
		return "tenant_admin"
	case "global_owner":
		return "global_owner"
	case "global_admin":
		return "global_admin"
	}
	if role != "" {
		return role
	}
	if adminScopeDTO(scope) == "tenant" {
		return "tenant_owner"
	}
	return "global_owner"
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
