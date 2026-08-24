package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

// tenantListCenterStatusProvider is the minimal interface for querying center
// registration state (digital employee authorizations) in the tenant list handler.
type tenantListCenterStatusProvider interface {
	Status(ctx context.Context) (*center.RegistrationState, error)
}

// tenantListComputeStatusProvider is the minimal interface for querying compute
// module authorization status per tenant.
type tenantListComputeStatusProvider interface {
	GetAuthorizationStatus(ctx context.Context, tenantID string) *llmservice.TenantAuthorizationStatus
}

// tenantAdminRouteSyncer records a tenant administrator's identity in
// HubCenter. It is intentionally small so tenant administration remains
// available if HubCenter is temporarily unreachable; startup reconciliation
// retries any route that could not be synced immediately.
type tenantAdminRouteSyncer interface {
	SyncTenantAdminRoute(ctx context.Context, email, tenantID string, previousEmailOpt ...string) error
}

type tenantCreateRequest struct {
	ID                    string   `json:"id"`
	Slug                  string   `json:"slug"`
	Name                  string   `json:"name"`
	PrimaryDomain         string   `json:"primary_domain"`
	Domains               []string `json:"domains"`
	AllowUserRegistration *bool    `json:"allow_user_registration"`
	RestrictEmailDomains  *bool    `json:"restrict_email_domains"`
	LogoURL               *string  `json:"logo_url"`
	InitialAdminUsername  string   `json:"initial_admin_username"`
	InitialAdminPassword  string   `json:"initial_admin_password"`
	InitialAdminEmail     string   `json:"initial_admin_email"`
	InitialAdminName      string   `json:"initial_admin_name"`
}

type tenantDomainsUpdateRequest struct {
	Name                  string   `json:"name"`
	PrimaryDomain         string   `json:"primary_domain"`
	Domains               []string `json:"domains"`
	AllowUserRegistration *bool    `json:"allow_user_registration"`
	RestrictEmailDomains  *bool    `json:"restrict_email_domains"`
	LogoURL               *string  `json:"logo_url"`
}

var tenantIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{2,63}$`)
var tenantDomainPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)

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

type tenantMergeRequest struct {
	TargetTenantID string `json:"target_tenant_id"`
	DryRun         bool   `json:"dry_run"`
	DeleteSource   *bool  `json:"delete_source"`
}

type tenantStatusUpdater interface {
	UpdateStatus(ctx context.Context, id string, status string) error
}

type tenantDomainsUpdater interface {
	UpdateDomains(ctx context.Context, id string, primaryDomain string, settingsJSON string) error
}

type tenantSettingsUpdater interface {
	UpdateSettings(ctx context.Context, id string, name string, primaryDomain string, settingsJSON string) error
}

type tenantSoftDeleter interface {
	SoftDeleteByID(ctx context.Context, id string) error
}

func AdminLoginTenantsHandler(tenants store.TenantRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defaultTenant, _ := tenants.EnsureDefault(r.Context())
		items, err := tenants.List(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "TENANTS_LIST_FAILED", err.Error())
			return
		}
		out := make([]map[string]any, 0, len(items))
		seenDefault := false
		for _, item := range items {
			if item != nil && strings.EqualFold(strings.TrimSpace(item.ID), store.DefaultTenantID) {
				seenDefault = true
			}
			if !tenantLoginOptionVisible(item) {
				continue
			}
			out = append(out, tenantLoginOptionDTO(item))
		}
		if !seenDefault && tenantLoginOptionVisible(defaultTenant) {
			out = append(out, tenantLoginOptionDTO(defaultTenant))
		}
		writeJSON(w, http.StatusOK, map[string]any{"tenants": out})
	}
}

func tenantLoginOptionVisible(item *store.Tenant) bool {
	return item != nil && item.DeletedAt == nil && !strings.EqualFold(strings.TrimSpace(item.ID), auth.ExplicitGlobalAdminTenantScope) && strings.EqualFold(strings.TrimSpace(item.Status), "active")
}

func AdminTenantsListHandler(tenants store.TenantRepository) http.HandlerFunc {
	return AdminTenantsListWithAuthHandler(tenants, nil, nil, nil)
}

// AdminTenantsListWithAuthHandler returns the tenant list enriched with per-tenant
// digital employee authorization and compute module authorization info.
func AdminTenantsListWithAuthHandler(tenants store.TenantRepository, centerSvc tenantListCenterStatusProvider, accessCtrl tenantListComputeStatusProvider, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !IsGlobalAdmin(r.Context()) {
			writeError(w, http.StatusForbidden, "GLOBAL_ADMIN_REQUIRED", "Global admin authorization required")
			return
		}
		defaultTenant, _ := tenants.EnsureDefault(r.Context())
		items, err := tenants.List(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "TENANTS_LIST_FAILED", err.Error())
			return
		}
		out := make([]*store.Tenant, 0, len(items))
		seenDefault := false
		for _, item := range items {
			if item != nil && strings.EqualFold(strings.TrimSpace(item.ID), store.DefaultTenantID) {
				seenDefault = true
			}
			if item == nil || strings.EqualFold(strings.TrimSpace(item.ID), auth.ExplicitGlobalAdminTenantScope) {
				continue
			}
			out = append(out, item)
		}
		if !seenDefault && defaultTenant != nil && !strings.EqualFold(strings.TrimSpace(defaultTenant.ID), auth.ExplicitGlobalAdminTenantScope) {
			out = append(out, defaultTenant)
		}

		dtos := tenantDTOs(out)

		// Enrich each tenant DTO with authorization info.
		// Primary source: local SQLite table (always available).
		// Secondary source: center heartbeat cache (for real-time Active/Reason state).
		authzLoaded := enrichTenantsWithAuthorization(r.Context(), dtos, out, db, centerSvc, accessCtrl)

		writeJSON(w, http.StatusOK, map[string]any{"tenants": dtos, "authorization_loaded": authzLoaded})
	}
}

func AdminTenantCreateHandler(tenants store.TenantRepository, admins *auth.AdminService, audit store.AdminAuditRepository, routeSyncers ...tenantAdminRouteSyncer) http.HandlerFunc {
	return adminTenantCreateHandler(nil, tenants, admins, audit, routeSyncers...)
}

func AdminTenantCreateWithPlatformCallbackHandler(system store.SystemSettingsRepository, tenants store.TenantRepository, admins *auth.AdminService, audit store.AdminAuditRepository, routeSyncers ...tenantAdminRouteSyncer) http.HandlerFunc {
	return adminTenantCreateHandler(system, tenants, admins, audit, routeSyncers...)
}

func adminTenantCreateHandler(system store.SystemSettingsRepository, tenants store.TenantRepository, admins *auth.AdminService, audit store.AdminAuditRepository, routeSyncers ...tenantAdminRouteSyncer) http.HandlerFunc {
	routeSyncer := firstTenantAdminRouteSyncer(routeSyncers)
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
		if adminRequested && admins == nil {
			writeError(w, http.StatusServiceUnavailable, "TENANT_ADMIN_UNSUPPORTED", "Tenant admin creation is not supported")
			return
		}
		tenantID := normalizeTenantIDInput(req.ID)
		if tenantID == "" {
			tenantID = "tenant_" + slug
		}
		if isReservedTenantID(tenantID) || tenantID == store.DefaultTenantID || !isValidTenantID(tenantID) {
			writeError(w, http.StatusBadRequest, "INVALID_TENANT_ID", "Tenant id must start with a letter and contain only lowercase letters, numbers, underscores, or hyphens")
			return
		}
		domains, invalidDomain := normalizeTenantDomainsForInput(append([]string{req.PrimaryDomain}, req.Domains...))
		if invalidDomain != "" {
			writeError(w, http.StatusBadRequest, "INVALID_TENANT_DOMAIN", "Tenant email domain is invalid: "+invalidDomain)
			return
		}
		if req.RestrictEmailDomains != nil && *req.RestrictEmailDomains && len(domains) == 0 {
			writeError(w, http.StatusBadRequest, "EMAIL_DOMAIN_RESTRICTION_REQUIRES_DOMAIN", "At least one email domain is required when domain-restricted registration is enabled")
			return
		}
		if conflictDomain, conflictTenantID, err := conflictingTenantDomain(r.Context(), tenants, tenantID, domains); err != nil {
			writeError(w, http.StatusInternalServerError, "TENANT_DOMAIN_CHECK_FAILED", err.Error())
			return
		} else if conflictDomain != "" {
			writeError(w, http.StatusConflict, "TENANT_DOMAIN_CONFLICT", "Tenant email domain "+conflictDomain+" is already used by "+conflictTenantID)
			return
		}
		primaryDomain := ""
		if len(domains) > 0 {
			primaryDomain = domains[0]
		}
		if req.LogoURL != nil && !isValidTenantLogoURL(*req.LogoURL) {
			writeError(w, http.StatusBadRequest, "INVALID_TENANT_LOGO_URL", "Tenant logo URL must be an HTTPS URL no longer than 2048 characters")
			return
		}
		settingsJSON := tenantSettingsJSONWithDomainsAndRegistrationAndLogo("{}", domains, req.AllowUserRegistration, req.RestrictEmailDomains, req.LogoURL)
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
			syncTenantAdminRoute(r.Context(), routeSyncer, tenant.ID, admin.Email)
		}
		auditPayload := map[string]any{"tenant_id": tenant.ID, "tenant_slug": tenant.Slug, "restrict_email_domains": tenantRestrictsEmailDomains(tenant)}
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

func firstTenantAdminRouteSyncer(syncers []tenantAdminRouteSyncer) tenantAdminRouteSyncer {
	if len(syncers) > 0 {
		return syncers[0]
	}
	return nil
}

func syncTenantAdminRoute(ctx context.Context, syncer tenantAdminRouteSyncer, tenantID, email string, previousEmailOpt ...string) {
	if syncer == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(email) == "" {
		return
	}
	previousEmail := ""
	if len(previousEmailOpt) > 0 {
		previousEmail = strings.TrimSpace(strings.ToLower(previousEmailOpt[0]))
	}
	if err := syncer.SyncTenantAdminRoute(ctx, email, tenantID, previousEmailOpt...); err != nil {
		// A tenant admin has already been committed locally. Do not fail or roll
		// it back because HubCenter may be transiently unavailable; the startup
		// reconciler will retry until the route is present.
		log.Printf("[tenant-admin] HubCenter administrator identity sync failed for tenant=%s email=%s previous_email=%s: %v", tenantID, strings.TrimSpace(strings.ToLower(email)), previousEmail, err)
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
	return adminTenantDomainsUpdateHandler(nil, tenants, audit)
}

func AdminTenantDomainsUpdateWithPlatformCallbackHandler(system store.SystemSettingsRepository, tenants store.TenantRepository, audit store.AdminAuditRepository) http.HandlerFunc {
	return adminTenantDomainsUpdateHandler(system, tenants, audit)
}

func adminTenantDomainsUpdateHandler(system store.SystemSettingsRepository, tenants store.TenantRepository, audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := AdminFromContext(r.Context())
		domainUpdater, hasDomainUpdater := tenants.(tenantDomainsUpdater)
		settingsUpdater, hasSettingsUpdater := tenants.(tenantSettingsUpdater)
		if !hasDomainUpdater && !hasSettingsUpdater {
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
		name := strings.TrimSpace(req.Name)
		if name == "" {
			name = tenant.Name
		}
		domains, invalidDomain := normalizeTenantDomainsForInput(append([]string{req.PrimaryDomain}, req.Domains...))
		if invalidDomain != "" {
			writeError(w, http.StatusBadRequest, "INVALID_TENANT_DOMAIN", "Tenant email domain is invalid: "+invalidDomain)
			return
		}
		restrictEmailDomains := tenantRestrictsEmailDomains(tenant)
		if req.RestrictEmailDomains != nil {
			restrictEmailDomains = *req.RestrictEmailDomains
		}
		if restrictEmailDomains && len(domains) == 0 {
			writeError(w, http.StatusBadRequest, "EMAIL_DOMAIN_RESTRICTION_REQUIRES_DOMAIN", "At least one email domain is required when domain-restricted registration is enabled")
			return
		}
		if conflictDomain, conflictTenantID, err := conflictingTenantDomain(r.Context(), tenants, tenantID, domains); err != nil {
			writeError(w, http.StatusInternalServerError, "TENANT_DOMAIN_CHECK_FAILED", err.Error())
			return
		} else if conflictDomain != "" {
			writeError(w, http.StatusConflict, "TENANT_DOMAIN_CONFLICT", "Tenant email domain "+conflictDomain+" is already used by "+conflictTenantID)
			return
		}
		primaryDomain := ""
		if len(domains) > 0 {
			primaryDomain = domains[0]
		}
		if req.LogoURL != nil && !isValidTenantLogoURL(*req.LogoURL) {
			writeError(w, http.StatusBadRequest, "INVALID_TENANT_LOGO_URL", "Tenant logo URL must be an HTTPS URL no longer than 2048 characters")
			return
		}
		settingsJSON := tenantSettingsJSONWithDomainsAndRegistrationAndLogo(tenant.SettingsJSON, domains, req.AllowUserRegistration, req.RestrictEmailDomains, req.LogoURL)
		var updateErr error
		if hasSettingsUpdater {
			updateErr = settingsUpdater.UpdateSettings(r.Context(), tenantID, name, primaryDomain, settingsJSON)
		} else if name != tenant.Name {
			writeError(w, http.StatusServiceUnavailable, "TENANT_SETTINGS_UNSUPPORTED", "Tenant setting updates are not supported")
			return
		} else {
			updateErr = domainUpdater.UpdateDomains(r.Context(), tenantID, primaryDomain, settingsJSON)
		}
		if updateErr != nil {
			writeError(w, http.StatusInternalServerError, "TENANT_DOMAINS_UPDATE_FAILED", updateErr.Error())
			return
		}
		updated, _ := tenants.GetByID(r.Context(), tenantID)
		writeAdminAuditLog(r.Context(), audit, actor.ID, "tenant.settings_updated", map[string]any{"tenant_id": tenantID, "name": name, "domains": domains, "allow_user_registration": tenantAllowsUserRegistration(updated), "restrict_email_domains": tenantRestrictsEmailDomains(updated), "has_logo_url": tenantLogoURL(updated) != ""})
		postPlatformTenantCallbacks(r.Context(), system, updated, "")
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

type tenantDeleteRequest struct {
	Password string `json:"password"`
}

// tenantHardDeleter is the interface for physically removing a tenant record.
type tenantHardDeleter interface {
	DeleteByID(ctx context.Context, id string) error
}

func AdminTenantDeleteHandler(tenants store.TenantRepository, runtimeStoppers ...TenantIMRuntimeStopper) http.HandlerFunc {
	return adminTenantDeleteHandler(nil, nil, nil, nil, nil, tenants, runtimeStoppers...)
}

func AdminTenantDeleteWithPlatformCallbackHandler(system store.SystemSettingsRepository, audit store.AdminAuditRepository, admins *auth.AdminService, db *sql.DB, centerSvc BoundUserRouteDeleter, tenants store.TenantRepository, runtimeStoppers ...TenantIMRuntimeStopper) http.HandlerFunc {
	return adminTenantDeleteHandler(system, audit, admins, db, centerSvc, tenants, runtimeStoppers...)
}

func adminTenantDeleteHandler(system store.SystemSettingsRepository, audit store.AdminAuditRepository, admins *auth.AdminService, db *sql.DB, routeDeleter BoundUserRouteDeleter, tenants store.TenantRepository, runtimeStoppers ...TenantIMRuntimeStopper) http.HandlerFunc {
	runtimeStopper := firstTenantIMRuntimeStopper(runtimeStoppers)
	return func(w http.ResponseWriter, r *http.Request) {
		actor := AdminFromContext(r.Context())
		if actor == nil || !IsGlobalAdmin(r.Context()) {
			writeError(w, http.StatusForbidden, "GLOBAL_ADMIN_REQUIRED", "Global admin authorization required")
			return
		}
		tenantID := strings.TrimSpace(r.PathValue("tenantId"))
		if tenantID == "" || isReservedTenantID(tenantID) || tenantID == store.DefaultTenantID {
			writeError(w, http.StatusBadRequest, "INVALID_TENANT", "Tenant id is invalid")
			return
		}

		// Require admin password confirmation for destructive delete.
		var req tenantDeleteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && admins != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must contain password field")
			return
		}

		// Verify the requesting admin's password when admin service is available.
		if admins != nil {
			if strings.TrimSpace(req.Password) == "" {
				writeError(w, http.StatusBadRequest, "PASSWORD_REQUIRED", "Admin login password is required to confirm tenant deletion")
				return
			}
			if _, err := admins.VerifyScopedCredentials(r.Context(), actor.Username, req.Password, auth.ExplicitGlobalAdminTenantScope); err != nil {
				writeError(w, http.StatusUnauthorized, "PASSWORD_INCORRECT", "Admin password verification failed")
				return
			}
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

		// Stop IM runtimes before data purge.
		if runtimeStopper != nil {
			runtimeStopper.StopTenantIMs(r.Context(), tenantID)
		}

		// Delete HubCenter routes for all tenant identities (best-effort, errors
		// logged but not blocking). Tenant administrators do not necessarily have
		// a users row, so include them before their admin records are purged.
		if db != nil && routeDeleter != nil {
			purgeTenantUserRoutes(r.Context(), db, tenantID, routeDeleter)
		}

		// Purge all tenant-scoped data from all tables, then hard-delete the tenant record.
		if db != nil {
			if err := purgeTenantData(r.Context(), db, tenantID); err != nil {
				writeError(w, http.StatusInternalServerError, "TENANT_PURGE_FAILED", "Failed to purge tenant data: "+err.Error())
				return
			}
		}

		// Hard-delete the tenant record itself.
		hardDeleter, ok := tenants.(tenantHardDeleter)
		if !ok {
			// Fallback to soft-delete if hard-delete interface not available.
			if softDeleter, ok2 := tenants.(tenantSoftDeleter); ok2 {
				if err := softDeleter.SoftDeleteByID(r.Context(), tenantID); err != nil {
					writeError(w, http.StatusInternalServerError, "TENANT_DELETE_FAILED", err.Error())
					return
				}
			}
		} else {
			if err := hardDeleter.DeleteByID(r.Context(), tenantID); err != nil {
				writeError(w, http.StatusInternalServerError, "TENANT_DELETE_FAILED", err.Error())
				return
			}
		}

		writeAdminAuditLog(r.Context(), audit, actor.ID, "tenant.hard_deleted", map[string]any{"tenant_id": tenantID, "tenant_slug": tenant.Slug, "purge": true})
		postPlatformTenantCallbacks(r.Context(), system, tenant, "deleted")
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "tenant_id": tenantID, "purged": true})
	}
}

// purgeTenantData deletes all rows belonging to the tenant from all tenant-scoped tables,
// including system_settings entries scoped to the tenant.
func purgeTenantData(ctx context.Context, db *sql.DB, tenantID string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Defer FK checks so table deletion order doesn't matter.
	if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
		return err
	}

	tables, err := sqlite.TenantScopedTables(ctx, tx)
	if err != nil {
		return err
	}

	for _, table := range tables {
		quoted := sqlite.TenantMergeQuoteIdent(table)
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+quoted+` WHERE tenant_id = ?`, tenantID); err != nil {
			return errors.New("purge table " + table + ": " + err.Error())
		}
	}

	// Also purge tenant-scoped system settings within the same transaction.
	prefix := "tenant:" + tenantID + ":"
	if _, err := tx.ExecContext(ctx, `DELETE FROM system_settings WHERE key LIKE ?`, prefix+"%"); err != nil {
		return errors.New("purge system_settings: " + err.Error())
	}

	return tx.Commit()
}

// purgeTenantUserRoutes deletes HubCenter routing entries for all users of the tenant.
// This is best-effort: failures are logged but do not block the deletion.
// Uses bounded concurrency to avoid timeout on tenants with many users.
func purgeTenantUserRoutes(ctx context.Context, db *sql.DB, tenantID string, routeDeleter BoundUserRouteDeleter) {
	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT lower(email) FROM (
			SELECT email FROM users WHERE tenant_id = ?
			UNION
			SELECT email FROM admin_users WHERE tenant_id = ? AND scope = 'tenant'
		) WHERE trim(email) != ''`, tenantID, tenantID)
	if err != nil {
		log.Printf("[tenant-purge] list tenant users for route cleanup: %v", err)
		return
	}
	defer rows.Close()

	var emails []string
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			continue
		}
		if email != "" {
			emails = append(emails, email)
		}
	}
	if rows.Err() != nil {
		log.Printf("[tenant-purge] scan tenant user emails: %v", rows.Err())
	}
	if len(emails) == 0 {
		return
	}

	log.Printf("[tenant-purge] deleting hub center routes for %d users in tenant %s", len(emails), tenantID)

	// Bounded concurrency: up to 5 parallel route deletions.
	const maxConcurrency = 5
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

	for _, email := range emails {
		sem <- struct{}{}
		wg.Add(1)
		go func(email string) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := routeDeleter.DeleteUserRoute(ctx, email, tenantID); err != nil {
				log.Printf("[tenant-purge] delete hub center route for %s@%s: %v", email, tenantID, err)
			}
		}(email)
	}
	wg.Wait()
}

func AdminTenantMergeHandler(db *sql.DB, tenants store.TenantRepository, audit store.AdminAuditRepository, runtimeStoppers ...TenantIMRuntimeStopper) http.HandlerFunc {
	runtimeStopper := firstTenantIMRuntimeStopper(runtimeStoppers)
	return func(w http.ResponseWriter, r *http.Request) {
		actor := AdminFromContext(r.Context())
		if actor == nil || !IsGlobalAdmin(r.Context()) {
			writeError(w, http.StatusForbidden, "GLOBAL_ADMIN_REQUIRED", "Global admin authorization required")
			return
		}
		if db == nil {
			writeError(w, http.StatusServiceUnavailable, "TENANT_MERGE_UNSUPPORTED", "Tenant merge is not supported")
			return
		}
		sourceTenantID := strings.TrimSpace(r.PathValue("tenantId"))
		if sourceTenantID == "" || isReservedTenantID(sourceTenantID) {
			writeError(w, http.StatusBadRequest, "INVALID_TENANT", "Source tenant id is invalid")
			return
		}
		var req tenantMergeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		targetTenantID := strings.TrimSpace(req.TargetTenantID)
		if targetTenantID == "" || isReservedTenantID(targetTenantID) || sourceTenantID == targetTenantID {
			writeError(w, http.StatusBadRequest, "INVALID_TARGET_TENANT", "Target tenant id is invalid")
			return
		}
		if source, err := tenants.GetByID(r.Context(), sourceTenantID); err != nil {
			writeError(w, http.StatusInternalServerError, "TENANT_LOOKUP_FAILED", err.Error())
			return
		} else if source == nil || source.DeletedAt != nil {
			writeError(w, http.StatusNotFound, "SOURCE_TENANT_NOT_FOUND", "Source tenant not found")
			return
		}
		if target, err := tenants.GetByID(r.Context(), targetTenantID); err != nil {
			writeError(w, http.StatusInternalServerError, "TENANT_LOOKUP_FAILED", err.Error())
			return
		} else if target == nil || target.DeletedAt != nil {
			writeError(w, http.StatusNotFound, "TARGET_TENANT_NOT_FOUND", "Target tenant not found")
			return
		} else if !strings.EqualFold(strings.TrimSpace(target.Status), "active") {
			writeError(w, http.StatusBadRequest, "TARGET_TENANT_INACTIVE", "Target tenant must be active")
			return
		}
		deleteSource := true
		if req.DeleteSource != nil {
			deleteSource = *req.DeleteSource
		}
		result, err := sqlite.MergeTenants(r.Context(), db, sqlite.TenantMergeOptions{FromTenant: sourceTenantID, ToTenant: targetTenantID, DryRun: req.DryRun, DeleteSource: deleteSource})
		if err != nil {
			if errors.Is(err, sqlite.ErrTenantMergeConflict) {
				writeError(w, http.StatusConflict, "TENANT_MERGE_CONFLICT", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "TENANT_MERGE_FAILED", err.Error())
			return
		}
		if !req.DryRun {
			if deleteSource && runtimeStopper != nil && sourceTenantID != store.DefaultTenantID {
				runtimeStopper.StopTenantIMs(r.Context(), sourceTenantID)
			}
			writeAdminAuditLog(r.Context(), audit, actor.ID, "tenant.merged", map[string]any{"source_tenant_id": sourceTenantID, "target_tenant_id": targetTenantID, "delete_source": deleteSource})
		}
		writeJSON(w, http.StatusOK, map[string]any{"result": result})
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

func AdminTenantAdminCreateHandler(tenants store.TenantRepository, admins *auth.AdminService, audit store.AdminAuditRepository, routeSyncers ...tenantAdminRouteSyncer) http.HandlerFunc {
	routeSyncer := firstTenantAdminRouteSyncer(routeSyncers)
	return func(w http.ResponseWriter, r *http.Request) {
		actor := AdminFromContext(r.Context())
		tenantID := strings.TrimSpace(r.PathValue("tenantId"))
		if tenantID == "" {
			writeError(w, http.StatusBadRequest, "TENANT_REQUIRED", "Tenant id is required")
			return
		}
		if isReservedTenantID(tenantID) {
			writeError(w, http.StatusBadRequest, "INVALID_TENANT", "Tenant id is invalid")
			return
		}
		if strings.EqualFold(tenantID, store.DefaultTenantID) {
			_, _ = tenants.EnsureDefault(r.Context())
		}
		if actor == nil || (!IsGlobalAdmin(r.Context()) && AdminTenantID(r.Context()) != tenantID) {
			writeError(w, http.StatusForbidden, "TENANT_FORBIDDEN", "Tenant access denied")
			return
		}
		if admins == nil {
			writeError(w, http.StatusServiceUnavailable, "TENANT_ADMIN_UNSUPPORTED", "Tenant admin creation is not supported")
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
		syncTenantAdminRoute(r.Context(), routeSyncer, tenantID, admin.Email)
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
	return strings.EqualFold(trimmed, auth.ExplicitGlobalAdminTenantScope)
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
			if domain == "" || !tenantDomainPattern.MatchString(domain) {
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

func normalizeTenantDomainsForInput(values []string) ([]string, string) {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, raw := range values {
		for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' ' }) {
			domain := normalizeDomain(part)
			if domain == "" {
				continue
			}
			if !tenantDomainPattern.MatchString(domain) {
				return nil, domain
			}
			if _, ok := seen[domain]; ok {
				continue
			}
			seen[domain] = struct{}{}
			out = append(out, domain)
		}
	}
	return out, ""
}

func conflictingTenantDomain(ctx context.Context, tenants store.TenantRepository, currentTenantID string, domains []string) (string, string, error) {
	if tenants == nil || len(domains) == 0 {
		return "", "", nil
	}
	wanted := map[string]struct{}{}
	for _, domain := range domains {
		if domain != "" {
			wanted[domain] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return "", "", nil
	}
	items, err := tenants.List(ctx)
	if err != nil {
		return "", "", err
	}
	currentTenantID = strings.TrimSpace(currentTenantID)
	for _, tenant := range items {
		if tenant == nil || tenant.DeletedAt != nil || strings.EqualFold(strings.TrimSpace(tenant.ID), currentTenantID) {
			continue
		}
		for _, domain := range tenantEmailDomains(tenant) {
			if _, ok := wanted[domain]; ok {
				return domain, tenant.ID, nil
			}
		}
	}
	return "", "", nil
}

func tenantEmailDomains(t *store.Tenant) []string {
	if t == nil {
		return nil
	}
	settings := tenantSettingsMap(t.SettingsJSON)
	var values []string
	for _, key := range []string{"email_domains", "domains"} {
		if raw, ok := settings[key].([]any); ok {
			for _, item := range raw {
				if s, ok := item.(string); ok {
					values = append(values, s)
				}
			}
		}
	}
	return normalizeTenantDomains(append([]string{t.PrimaryDomain}, values...))
}

func tenantSettingsJSONWithDomains(settingsJSON string, domains []string) string {
	return tenantSettingsJSONWithDomainsAndRegistration(settingsJSON, domains, nil, nil)
}

func tenantSettingsJSONWithDomainsAndRegistration(settingsJSON string, domains []string, allowUserRegistration *bool, restrictEmailDomains *bool) string {
	return tenantSettingsJSONWithDomainsAndRegistrationAndLogo(settingsJSON, domains, allowUserRegistration, restrictEmailDomains, nil)
}

func tenantSettingsJSONWithDomainsAndRegistrationAndLogo(settingsJSON string, domains []string, allowUserRegistration *bool, restrictEmailDomains *bool, logoURL *string) string {
	settings := tenantSettingsMap(settingsJSON)
	settings["email_domains"] = domains
	delete(settings, "domains")
	if allowUserRegistration != nil {
		settings["allow_user_registration"] = *allowUserRegistration
		delete(settings, "registration_enabled")
	}
	if restrictEmailDomains != nil {
		settings["restrict_email_domains"] = *restrictEmailDomains
	}
	if logoURL != nil {
		if value := strings.TrimSpace(*logoURL); value != "" {
			settings["logo_url"] = value
		} else {
			delete(settings, "logo_url")
		}
	}
	data, err := json.Marshal(settings)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func isValidTenantLogoURL(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	if len(value) > 2048 || strings.ContainsAny(value, "\r\n\t") {
		return false
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil
}

func tenantLogoURL(t *store.Tenant) string {
	if t == nil {
		return ""
	}
	value, _ := tenantSettingsMap(t.SettingsJSON)["logo_url"].(string)
	if !isValidTenantLogoURL(value) {
		return ""
	}
	return strings.TrimSpace(value)
}

func tenantRestrictsEmailDomains(t *store.Tenant) bool {
	if t == nil {
		return false
	}
	value, _ := tenantSettingsMap(t.SettingsJSON)["restrict_email_domains"].(bool)
	return value
}

func tenantAllowsUserRegistration(t *store.Tenant) bool {
	if t == nil {
		return true
	}
	settings := tenantSettingsMap(t.SettingsJSON)
	for _, key := range []string{"allow_user_registration", "registration_enabled"} {
		if value, ok := settings[key].(bool); ok {
			return value
		}
	}
	return true
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
	return map[string]any{"id": t.ID, "slug": t.Slug, "name": t.Name, "primary_domain": t.PrimaryDomain, "domains": tenantEmailDomains(t), "allow_user_registration": tenantAllowsUserRegistration(t), "restrict_email_domains": tenantRestrictsEmailDomains(t)}
}

func tenantDTO(t *store.Tenant) map[string]any {
	if t == nil {
		return map[string]any{}
	}
	out := map[string]any{"id": t.ID, "slug": t.Slug, "name": t.Name, "status": t.Status, "primary_domain": t.PrimaryDomain, "domains": tenantEmailDomains(t), "allow_user_registration": tenantAllowsUserRegistration(t), "restrict_email_domains": tenantRestrictsEmailDomains(t), "logo_url": tenantLogoURL(t), "settings_json": t.SettingsJSON, "created_at": t.CreatedAt.Format(time.RFC3339), "updated_at": t.UpdatedAt.Format(time.RFC3339)}
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

// ---------------------------------------------------------------------------
// Tenant list authorization enrichment
// ---------------------------------------------------------------------------

// tenantDigitalEmployeeAuthRow holds a row from tenant_digital_employee_authorizations.
type tenantDigitalEmployeeAuthRow struct {
	TenantID   string
	Enabled    bool
	Quota      int
	Used       int
	ValidUntil string
	Status     string
}

// loadTenantDigitalEmployeeAuthorizations reads authorization data for all
// tenants from the local SQLite table (the authoritative source).
func loadTenantDigitalEmployeeAuthorizations(ctx context.Context, db *sql.DB) map[string]*tenantDigitalEmployeeAuthRow {
	if db == nil {
		return nil
	}
	rows, err := db.QueryContext(ctx, `SELECT tenant_id, enabled, quota, used, valid_until, status FROM tenant_digital_employee_authorizations`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	result := make(map[string]*tenantDigitalEmployeeAuthRow)
	for rows.Next() {
		var row tenantDigitalEmployeeAuthRow
		var enabledInt int
		var validUntil sql.NullString
		if err := rows.Scan(&row.TenantID, &enabledInt, &row.Quota, &row.Used, &validUntil, &row.Status); err != nil {
			continue
		}
		row.Enabled = enabledInt != 0
		if validUntil.Valid {
			row.ValidUntil = validUntil.String
		}
		result[row.TenantID] = &row
	}
	return result
}

// enrichTenantsWithAuthorization adds digital_employee_authorization and
// compute_authorization fields to each tenant DTO.
// Primary data source: local SQLite table (always available, even without HubCenter).
// Secondary: center heartbeat cache for compute module status.
// Returns true if at least the local DB was successfully queried.
func enrichTenantsWithAuthorization(ctx context.Context, dtos []map[string]any, tenants []*store.Tenant, db *sql.DB, centerSvc tenantListCenterStatusProvider, accessCtrl tenantListComputeStatusProvider) bool {
	if len(dtos) == 0 {
		return false
	}

	// Primary source: local DB for digital employee authorization
	localAuthz := loadTenantDigitalEmployeeAuthorizations(ctx, db)

	// Secondary source: center status for real-time Active/Reason normalization
	var centerStatus *center.RegistrationState
	if centerSvc != nil {
		centerStatus, _ = centerSvc.Status(ctx)
	}

	// Lazy resolve compute access control at request time
	if accessCtrl == nil {
		if ac := GetMaClawAccessControl(); ac != nil {
			accessCtrl = ac
		}
	}

	for i, dto := range dtos {
		if i >= len(tenants) || tenants[i] == nil {
			continue
		}
		tenantID := strings.TrimSpace(tenants[i].ID)
		if tenantID == "" {
			continue
		}

		// Digital employee authorization: prefer center heartbeat (has real-time
		// Active/Reason/ExpiresAt), fall back to local DB row.
		var deAuthDTO map[string]any
		if centerStatus != nil {
			if deAuth := centerStatusAuthorizationForTenant(centerStatus, tenantID); deAuth != nil {
				deAuthDTO = map[string]any{
					"enabled":    deAuth.Enabled,
					"quota":      deAuth.Quota,
					"active":     deAuth.Active,
					"expires_at": deAuth.ExpiresAt,
					"reason":     deAuth.Reason,
				}
			}
		}
		if deAuthDTO == nil && localAuthz != nil {
			if row := localAuthz[tenantID]; row != nil {
				active := row.Enabled && row.Quota > 0 && row.Status == "active"
				reason := ""
				if !row.Enabled {
					reason = "disabled"
				} else if row.Quota <= 0 {
					reason = "quota_zero"
				} else if row.Status != "active" {
					reason = row.Status
				}
				deAuthDTO = map[string]any{
					"enabled":    row.Enabled,
					"quota":      row.Quota,
					"used":       row.Used,
					"active":     active,
					"expires_at": row.ValidUntil,
					"reason":     reason,
				}
			}
		}
		if deAuthDTO != nil {
			dto["digital_employee_authorization"] = deAuthDTO
		}

		// Compute module authorization (LLM compute credits)
		if accessCtrl != nil {
			computeAuth := accessCtrl.GetAuthorizationStatus(ctx, tenantID)
			if computeAuth != nil && (len(computeAuth.Authorizations) > 0 || computeAuth.AllowExternalProviders) {
				dto["compute_authorization"] = tenantComputeAuthorizationSummary(computeAuth)
			}
		}
	}
	return localAuthz != nil || centerStatus != nil
}

// tenantComputeAuthorizationSummary builds a summary of compute module
// authorization for display in the tenant card.
func tenantComputeAuthorizationSummary(auth *llmservice.TenantAuthorizationStatus) map[string]any {
	if auth == nil {
		return nil
	}
	var totalCredits, usedCredits, remainingCredits float64
	var creditAuthorizationCount int
	var latestExpiry string
	now := time.Now().UTC()
	for _, a := range auth.Authorizations {
		if strings.TrimSpace(a.ServiceGroupID) == "__external_compute_permission__" {
			continue
		}
		if !tenantComputeAuthorizationSummaryCardActive(a, now) {
			continue
		}
		totalCredits += a.CreditsTotal
		usedCredits += a.CreditsUsed
		remainingCredits += a.CreditsRemaining
		creditAuthorizationCount++
		if a.ExpiresAt > latestExpiry {
			latestExpiry = a.ExpiresAt
		}
	}
	return map[string]any{
		"active":              auth.AllowExternalProviders,
		"total_credits":       totalCredits,
		"used_credits":        usedCredits,
		"remaining_credits":   remainingCredits,
		"authorization_count": creditAuthorizationCount,
		"expires_at":          latestExpiry,
		"allow_external":      auth.AllowExternalProviders,
	}
}

func tenantComputeAuthorizationSummaryCardActive(auth llmservice.AuthorizationSummary, now time.Time) bool {
	if auth.Active {
		return true
	}
	status := strings.ToLower(strings.TrimSpace(auth.Status))
	if status == "expired" || status == "exhausted" || status == "inactive" || status == "invalid" {
		return false
	}
	if strings.TrimSpace(auth.ExpiresAt) != "" {
		if expiresAt, err := time.Parse(time.RFC3339, strings.TrimSpace(auth.ExpiresAt)); err == nil && !expiresAt.After(now) {
			return false
		}
	}
	return auth.CreditsRemaining > 0
}
