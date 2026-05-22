package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
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
	Domains              []string `json:"domains"`
	InitialAdminUsername string `json:"initial_admin_username"`
	InitialAdminPassword string `json:"initial_admin_password"`
	InitialAdminEmail    string `json:"initial_admin_email"`
	InitialAdminName     string `json:"initial_admin_name"`
}

type tenantDomainsUpdateRequest struct {
	PrimaryDomain string   `json:"primary_domain"`
	Domains       []string `json:"domains"`
}

var tenantIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{2,63}$`)

type tenantAdminCreateRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
}

type tenantStatusUpdateRequest struct {
	Status string `json:"status"`
}

type tenantStatusUpdater interface {
	UpdateStatus(ctx context.Context, id string, status string) error
}

type tenantDomainsUpdater interface {
	UpdateDomains(ctx context.Context, id string, primaryDomain string, settingsJSON string) error
}

type tenantSoftDeleter interface {
	SoftDeleteByID(ctx context.Context, id string) error
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
		out := make([]*store.Tenant, 0, len(items))
		for _, item := range items {
			if item == nil || isReservedTenantID(item.ID) {
				continue
			}
			out = append(out, item)
		}
		writeJSON(w, http.StatusOK, map[string]any{"tenants": tenantDTOs(out)})
	}
}

func AdminTenantCreateHandler(tenants store.TenantRepository, admins *auth.AdminService, audit store.AdminAuditRepository) http.HandlerFunc {
	return adminTenantCreateHandler(nil, tenants, admins, audit)
}

func AdminTenantCreateWithPlatformCallbackHandler(system store.SystemSettingsRepository, tenants store.TenantRepository, admins *auth.AdminService, audit store.AdminAuditRepository) http.HandlerFunc {
	return adminTenantCreateHandler(system, tenants, admins, audit)
}

func adminTenantCreateHandler(system store.SystemSettingsRepository, tenants store.TenantRepository, admins *auth.AdminService, audit store.AdminAuditRepository) http.HandlerFunc {
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
		if slug == "" {
			slug = normalizeTenantSlug(req.Name)
		}
		name := strings.TrimSpace(req.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Tenant name is required")
			return
		}
		if slug == "" {
			slug = "tenant-" + strconv.FormatInt(time.Now().UnixNano(), 36)
		}
		adminRequested := strings.TrimSpace(req.InitialAdminUsername) != "" || strings.TrimSpace(req.InitialAdminPassword) != "" || strings.TrimSpace(req.InitialAdminEmail) != "" || strings.TrimSpace(req.InitialAdminName) != ""
		if adminRequested && (strings.TrimSpace(req.InitialAdminUsername) == "" || strings.TrimSpace(req.InitialAdminPassword) == "" || strings.TrimSpace(req.InitialAdminEmail) == "") {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Initial admin username, password, and email are required when creating an initial admin")
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
		domains := normalizeTenantDomains(append([]string{req.PrimaryDomain}, req.Domains...))
		primaryDomain := ""
		if len(domains) > 0 {
			primaryDomain = domains[0]
		}
		settingsJSON := tenantSettingsJSONWithDomains("{}", domains)
		now := time.Now()
		tenant := &store.Tenant{ID: tenantID, Slug: slug, Name: name, Status: "active", PrimaryDomain: primaryDomain, SettingsJSON: settingsJSON, CreatedByAdminID: actor.ID, CreatedAt: now, UpdatedAt: now}
		if err := tenants.Create(r.Context(), tenant); err != nil {
			writeError(w, http.StatusConflict, "TENANT_CREATE_FAILED", err.Error())
			return
		}
		var admin *store.AdminUser
		if adminRequested {
			var err error
			admin, err = admins.CreateTenantAdmin(r.Context(), tenant.ID, req.InitialAdminUsername, req.InitialAdminPassword, req.InitialAdminEmail, req.InitialAdminName, "tenant_owner")
			if err != nil {
				_ = tenants.DeleteByID(r.Context(), tenant.ID)
				status, code := tenantAdminCreateError(err)
				writeError(w, status, code, err.Error())
				return
			}
		}
		auditPayload := map[string]any{"tenant_id": tenant.ID, "tenant_slug": tenant.Slug}
		if admin != nil {
			auditPayload["initial_admin_id"] = admin.ID
		}
		writeAdminAuditLog(r.Context(), audit, actor.ID, "tenant.created", auditPayload)
		postPlatformTenantCallbacks(r.Context(), system, tenant, "")
		resp := map[string]any{"tenant": tenantDTO(tenant)}
		if admin != nil {
			resp["admin"] = adminDTO(admin)
		}
		writeJSON(w, http.StatusCreated, resp)
	}
}

func AdminTenantDetailHandler(tenants store.TenantRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := strings.TrimSpace(r.PathValue("tenantId"))
		if tenantID == "" {
			writeError(w, http.StatusBadRequest, "TENANT_REQUIRED", "Tenant id is required")
			return
		}
		if isReservedTenantID(tenantID) {
			writeError(w, http.StatusBadRequest, "INVALID_TENANT", "Tenant id is invalid")
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

func AdminTenantDomainsUpdateHandler(tenants store.TenantRepository, audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := AdminFromContext(r.Context())
		updater, ok := tenants.(tenantDomainsUpdater)
		if !ok {
			writeError(w, http.StatusServiceUnavailable, "TENANT_DOMAINS_UNSUPPORTED", "Tenant domain updates are not supported")
			return
		}
		tenantID := strings.TrimSpace(r.PathValue("tenantId"))
		if tenantID == "" || isReservedTenantID(tenantID) {
			writeError(w, http.StatusBadRequest, "INVALID_TENANT", "Tenant id is invalid")
			return
		}
		if actor == nil || (!IsGlobalAdmin(r.Context()) && AdminTenantID(r.Context()) != tenantID) {
			writeError(w, http.StatusForbidden, "TENANT_FORBIDDEN", "Tenant access denied")
			return
		}
		var req tenantDomainsUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		tenant, err := tenants.GetByID(r.Context(), tenantID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "TENANT_LOAD_FAILED", err.Error())
			return
		}
		if tenant == nil || tenant.DeletedAt != nil {
			writeError(w, http.StatusNotFound, "TENANT_NOT_FOUND", "Tenant not found")
			return
		}
		domains := normalizeTenantDomains(append([]string{req.PrimaryDomain}, req.Domains...))
		primaryDomain := ""
		if len(domains) > 0 {
			primaryDomain = domains[0]
		}
		settingsJSON := tenantSettingsJSONWithDomains(tenant.SettingsJSON, domains)
		if err := updater.UpdateDomains(r.Context(), tenantID, primaryDomain, settingsJSON); err != nil {
			writeError(w, http.StatusInternalServerError, "TENANT_DOMAINS_UPDATE_FAILED", err.Error())
			return
		}
		updated, _ := tenants.GetByID(r.Context(), tenantID)
		writeAdminAuditLog(r.Context(), audit, actor.ID, "tenant.domains_updated", map[string]any{"tenant_id": tenantID, "domains": domains})
		writeJSON(w, http.StatusOK, map[string]any{"tenant": tenantDTO(updated)})
	}
}

func AdminTenantStatusUpdateHandler(tenants store.TenantRepository, runtimeStoppers ...TenantIMRuntimeStopper) http.HandlerFunc {
	return adminTenantStatusUpdateHandler(nil, nil, tenants, runtimeStoppers...)
}

func AdminTenantStatusUpdateWithPlatformCallbackHandler(system store.SystemSettingsRepository, audit store.AdminAuditRepository, tenants store.TenantRepository, runtimeStoppers ...TenantIMRuntimeStopper) http.HandlerFunc {
	return adminTenantStatusUpdateHandler(system, audit, tenants, runtimeStoppers...)
}

func adminTenantStatusUpdateHandler(system store.SystemSettingsRepository, audit store.AdminAuditRepository, tenants store.TenantRepository, runtimeStoppers ...TenantIMRuntimeStopper) http.HandlerFunc {
	runtimeStopper := firstTenantIMRuntimeStopper(runtimeStoppers)
	return func(w http.ResponseWriter, r *http.Request) {
		actor := AdminFromContext(r.Context())
		if actor == nil || !IsGlobalAdmin(r.Context()) {
			writeError(w, http.StatusForbidden, "GLOBAL_ADMIN_REQUIRED", "Global admin authorization required")
			return
		}
		updater, ok := tenants.(tenantStatusUpdater)
		if !ok {
			writeError(w, http.StatusServiceUnavailable, "TENANT_STATUS_UNSUPPORTED", "Tenant status updates are not supported")
			return
		}
		tenantID := strings.TrimSpace(r.PathValue("tenantId"))
		if tenantID == "" || isReservedTenantID(tenantID) || tenantID == store.DefaultTenantID {
			writeError(w, http.StatusBadRequest, "INVALID_TENANT", "Tenant id is invalid")
			return
		}
		var req tenantStatusUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		status := normalizeTenantStatus(req.Status)
		if status == "" {
			writeError(w, http.StatusBadRequest, "INVALID_TENANT_STATUS", "Tenant status must be active or inactive")
			return
		}
		tenant, err := tenants.GetByID(r.Context(), tenantID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "TENANT_LOAD_FAILED", err.Error())
			return
		}
		if tenant == nil || tenant.DeletedAt != nil {
			writeError(w, http.StatusNotFound, "TENANT_NOT_FOUND", "Tenant not found")
			return
		}
		if err := updater.UpdateStatus(r.Context(), tenantID, status); err != nil {
			writeError(w, http.StatusInternalServerError, "TENANT_STATUS_UPDATE_FAILED", err.Error())
			return
		}
		if status != "active" && runtimeStopper != nil {
			runtimeStopper.StopTenantIMs(r.Context(), tenantID)
		}
		updated, _ := tenants.GetByID(r.Context(), tenantID)
		writeAdminAuditLog(r.Context(), audit, actor.ID, "tenant.status_updated", map[string]any{"tenant_id": tenantID, "status": status})
		postPlatformTenantCallbacks(r.Context(), system, updated, "")
		writeJSON(w, http.StatusOK, map[string]any{"tenant": tenantDTO(updated)})
	}
}

func AdminTenantDeleteHandler(tenants store.TenantRepository, runtimeStoppers ...TenantIMRuntimeStopper) http.HandlerFunc {
	return adminTenantDeleteHandler(nil, nil, tenants, runtimeStoppers...)
}

func AdminTenantDeleteWithPlatformCallbackHandler(system store.SystemSettingsRepository, audit store.AdminAuditRepository, tenants store.TenantRepository, runtimeStoppers ...TenantIMRuntimeStopper) http.HandlerFunc {
	return adminTenantDeleteHandler(system, audit, tenants, runtimeStoppers...)
}

func adminTenantDeleteHandler(system store.SystemSettingsRepository, audit store.AdminAuditRepository, tenants store.TenantRepository, runtimeStoppers ...TenantIMRuntimeStopper) http.HandlerFunc {
	runtimeStopper := firstTenantIMRuntimeStopper(runtimeStoppers)
	return func(w http.ResponseWriter, r *http.Request) {
		actor := AdminFromContext(r.Context())
		if actor == nil || !IsGlobalAdmin(r.Context()) {
			writeError(w, http.StatusForbidden, "GLOBAL_ADMIN_REQUIRED", "Global admin authorization required")
			return
		}
		deleter, ok := tenants.(tenantSoftDeleter)
		if !ok {
			writeError(w, http.StatusServiceUnavailable, "TENANT_DELETE_UNSUPPORTED", "Tenant delete is not supported")
			return
		}
		tenantID := strings.TrimSpace(r.PathValue("tenantId"))
		if tenantID == "" || isReservedTenantID(tenantID) || tenantID == store.DefaultTenantID {
			writeError(w, http.StatusBadRequest, "INVALID_TENANT", "Tenant id is invalid")
			return
		}
		tenant, err := tenants.GetByID(r.Context(), tenantID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "TENANT_LOAD_FAILED", err.Error())
			return
		}
		if tenant == nil || tenant.DeletedAt != nil {
			writeError(w, http.StatusNotFound, "TENANT_NOT_FOUND", "Tenant not found")
			return
		}
		if err := deleter.SoftDeleteByID(r.Context(), tenantID); err != nil {
			writeError(w, http.StatusInternalServerError, "TENANT_DELETE_FAILED", err.Error())
			return
		}
		if runtimeStopper != nil {
			runtimeStopper.StopTenantIMs(r.Context(), tenantID)
		}
		updated, _ := tenants.GetByID(r.Context(), tenantID)
		writeAdminAuditLog(r.Context(), audit, actor.ID, "tenant.deleted", map[string]any{"tenant_id": tenantID, "tenant_slug": tenant.Slug})
		postPlatformTenantCallbacks(r.Context(), system, updated, "deleted")
		writeJSON(w, http.StatusOK, map[string]any{"tenant": tenantDTO(updated)})
	}
}

func postPlatformTenantCallbacks(ctx context.Context, system store.SystemSettingsRepository, tenant *store.Tenant, statusOverride string) {
	if system == nil || tenant == nil {
		return
	}
	reg := loadPlatformProviderRegistry(ctx, system)
	status := strings.TrimSpace(statusOverride)
	if status == "" {
		status = platformTenantCallbackStatus(tenant.Status)
	}
	for _, provider := range reg.Providers {
		if strings.TrimSpace(provider.CallbackBaseURL) == "" || strings.TrimSpace(provider.CallbackSecret) == "" || strings.TrimSpace(provider.RegistrationStatus) != "active" {
			continue
		}
		virtualDomain := platformVirtualMailDomainForTenant(provider, tenant)
		payload := map[string]any{
			"hub_tenant_id":       tenant.ID,
			"tenant_id":           tenant.ID,
			"hub_tenant_code":     tenant.Slug,
			"code":                tenant.Slug,
			"slug":                tenant.Slug,
			"name":                tenant.Name,
			"status":              status,
			"primary_domain":      tenant.PrimaryDomain,
			"domains":             tenantEmailDomains(tenant),
			"virtual_mail_domain": virtualDomain,
			"ve_enabled":          status == "active" && virtualDomain != "",
		}
		go postPlatformCallback(provider, "/api/hub/callback/tenant", payload)
	}
}

func platformTenantCallbackStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "active":
		return "active"
	case "inactive", "disabled", "suspended":
		return "disabled"
	default:
		return "disabled"
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
		if isReservedTenantID(tenantID) || tenantID == store.DefaultTenantID {
			writeError(w, http.StatusBadRequest, "INVALID_TENANT", "Tenant id is invalid")
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
	trimmed := strings.TrimSpace(id)
	return strings.EqualFold(trimmed, auth.ExplicitGlobalAdminTenantScope) || strings.EqualFold(trimmed, store.DefaultTenantID)
}
func normalizeTenantIDInput(id string) string { return strings.ToLower(strings.TrimSpace(id)) }
func isValidTenantID(id string) bool          { return tenantIDPattern.MatchString(strings.TrimSpace(id)) }

func normalizeTenantStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "active":
		return "active"
	case "inactive", "disabled":
		return "inactive"
	default:
		return ""
	}
}

func normalizeTenantDomains(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, raw := range values {
		for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' ' }) {
			domain := normalizeDomain(part)
			if domain == "" {
				continue
			}
			if _, ok := seen[domain]; ok {
				continue
			}
			seen[domain] = struct{}{}
			out = append(out, domain)
		}
	}
	return out
}

func tenantEmailDomains(t *store.Tenant) []string {
	if t == nil {
		return nil
	}
	settings := tenantSettingsMap(t.SettingsJSON)
	var values []string
	if raw, ok := settings["email_domains"].([]any); ok {
		for _, item := range raw {
			if s, ok := item.(string); ok {
				values = append(values, s)
			}
		}
	} else if raw, ok := settings["domains"].([]any); ok {
		for _, item := range raw {
			if s, ok := item.(string); ok {
				values = append(values, s)
			}
		}
	}
	return normalizeTenantDomains(append([]string{t.PrimaryDomain}, values...))
}

func tenantSettingsJSONWithDomains(settingsJSON string, domains []string) string {
	settings := tenantSettingsMap(settingsJSON)
	settings["email_domains"] = domains
	data, err := json.Marshal(settings)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func tenantSettingsMap(settingsJSON string) map[string]any {
	settings := map[string]any{}
	if strings.TrimSpace(settingsJSON) == "" {
		return settings
	}
	if err := json.Unmarshal([]byte(settingsJSON), &settings); err != nil {
		return map[string]any{}
	}
	return settings
}

func tenantLoginOptionDTO(t *store.Tenant) map[string]any {
	if t == nil {
		return map[string]any{}
	}
	return map[string]any{"id": t.ID, "slug": t.Slug, "name": t.Name, "primary_domain": t.PrimaryDomain, "domains": tenantEmailDomains(t)}
}

func tenantDTO(t *store.Tenant) map[string]any {
	if t == nil {
		return map[string]any{}
	}
	out := map[string]any{"id": t.ID, "slug": t.Slug, "name": t.Name, "status": t.Status, "primary_domain": t.PrimaryDomain, "domains": tenantEmailDomains(t), "settings_json": t.SettingsJSON, "created_at": t.CreatedAt.Format(time.RFC3339), "updated_at": t.UpdatedAt.Format(time.RFC3339)}
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
