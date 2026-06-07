package httpapi

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	corea2a "github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const platformProviderRegistryKey = "ve_platform_provider_registry"
const macLawSrvRuntimeRegistryKey = "maclawsrv_runtime_registry"
const platformRequestNonceRegistryKey = "ve_platform_request_nonce_registry"
const platformRequestReplayWindow = 10 * time.Minute
const platformSignedBodyMaxBytes = int64(veAvatarDataURLMaxSize + 512*1024)
const platformA2ADeliveryTimeout = time.Duration(corelib.DefaultAgentTimeoutSec) * time.Second
const maclawSrvRuntimePlatformID = "maclawsrv"

type platformProviderRegistry struct {
	Providers []platformProviderEntry `json:"providers"`
}

type macLawSrvRuntimeRegistry struct {
	Runtimes []macLawSrvRuntimeEntry `json:"runtimes"`
}

type macLawSrvRuntimeEntry struct {
	RuntimeID   string   `json:"runtime_id"`
	BaseURL     string   `json:"base_url"`
	AdminSecret string   `json:"admin_secret,omitempty"`
	TenantIDs   []string `json:"tenant_ids,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
	UpdatedAt   string   `json:"updated_at,omitempty"`
}

type platformRequestNonceRegistry struct {
	Nonces map[string]string `json:"nonces"`
}

type platformProviderEntry struct {
	PlatformID            string                 `json:"platform_id"`
	PlatformName          string                 `json:"platform_name"`
	CallbackBaseURL       string                 `json:"callback_base_url"`
	PublicKeyPEM          string                 `json:"public_key"`
	PublicKeyFingerprint  string                 `json:"public_key_fingerprint"`
	VirtualMailDomain     string                 `json:"virtual_mail_domain"`
	CallbackSecret        string                 `json:"callback_secret,omitempty"`
	RegistrationStatus    string                 `json:"registration_status"`
	RegistrationRequestID string                 `json:"registration_request_id"`
	RequestedFeatures     []string               `json:"requested_features,omitempty"`
	TenantDomains         []platformTenantDomain `json:"tenant_domains,omitempty"`
	CreatedAt             string                 `json:"created_at,omitempty"`
	UpdatedAt             string                 `json:"updated_at,omitempty"`
}

type platformTenantDomain struct {
	TenantID          string `json:"tenant_id"`
	TenantCode        string `json:"tenant_code"`
	TenantName        string `json:"tenant_name"`
	HubTenantID       string `json:"hub_tenant_id,omitempty"`
	HubTenantCode     string `json:"hub_tenant_code,omitempty"`
	VirtualMailDomain string `json:"virtual_mail_domain"`
	Status            string `json:"status"`
}

type platformEmployeeRequest struct {
	EmployeeID         string `json:"employee_id"`
	PlatformEmployeeID string `json:"platform_employee_id"`
	TenantID           string `json:"tenant_id"`
	HubTenantID        string `json:"hub_tenant_id"`
	TenantName         string `json:"tenant_name"`
	Name               string `json:"name"`
	Handle             string `json:"handle"`
	VirtualEmail       string `json:"virtual_email"`
	SkillDescription   string `json:"skill_description"`
	SkillTags          string `json:"skill_tags"`
	AvatarDataURL      string `json:"avatar_data_url"`
	SourceType         string `json:"source_type"`
	AccountType        string `json:"account_type"`
	ReviewStatus       string `json:"review_status"`
	DefaultLLM         string `json:"default_llm"`
	LLMServiceGroupID  string `json:"llm_service_group_id"`
	RuntimeProviderID  string `json:"runtime_provider_id"`
	RuntimeBaseURL     string `json:"runtime_base_url"`
	RuntimeAPIKey      string `json:"runtime_api_key"`
}

func PlatformProviderRegisterHandler(system store.SystemSettingsRepository, tenants ...store.TenantRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, providerID, timestamp, nonce, err := readSignedPlatformBody(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "PLATFORM_SIGNATURE_REQUIRED", err.Error())
			return
		}
		var req struct {
			PlatformID            string                 `json:"platform_id"`
			PlatformName          string                 `json:"platform_name"`
			CallbackBaseURL       string                 `json:"callback_base_url"`
			PublicKey             string                 `json:"public_key"`
			PublicKeyFingerprint  string                 `json:"public_key_fingerprint"`
			VirtualMailDomain     string                 `json:"virtual_mail_domain"`
			CallbackSecret        string                 `json:"callback_secret"`
			RequestedFeatures     []string               `json:"requested_features"`
			RegistrationRequestID string                 `json:"registration_request_id"`
			TenantDomains         []platformTenantDomain `json:"tenant_domains"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.PlatformID == "" {
			req.PlatformID = providerID
		}
		if strings.TrimSpace(req.PlatformID) == "" || strings.TrimSpace(req.PublicKey) == "" {
			writeError(w, http.StatusBadRequest, "INVALID_PROVIDER", "platform_id and public_key are required")
			return
		}
		if err := verifyPlatformSignature(platformSignaturePayload(r.Method, r.URL.RequestURI(), timestamp, nonce, body), r.Header.Get("X-VE-Signature"), req.PublicKey); err != nil {
			writeError(w, http.StatusUnauthorized, "PLATFORM_SIGNATURE_INVALID", err.Error())
			return
		}
		stored, err := recordPlatformRequestNonce(r.Context(), system, req.PlatformID, nonce, time.Now().UTC())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "PLATFORM_NONCE_STORE_FAILED", err.Error())
			return
		}
		if !stored {
			writeError(w, http.StatusConflict, "PLATFORM_REPLAY_DETECTED", "platform request nonce has already been used")
			return
		}
		now := time.Now().UTC().Format(time.RFC3339)
		reg := loadPlatformProviderRegistry(r.Context(), system)
		entry := platformProviderEntry{PlatformID: strings.TrimSpace(req.PlatformID), PlatformName: strings.TrimSpace(req.PlatformName), CallbackBaseURL: strings.TrimRight(strings.TrimSpace(req.CallbackBaseURL), "/"), PublicKeyPEM: strings.TrimSpace(req.PublicKey), PublicKeyFingerprint: strings.TrimSpace(req.PublicKeyFingerprint), VirtualMailDomain: strings.ToLower(strings.TrimSpace(req.VirtualMailDomain)), CallbackSecret: strings.TrimSpace(req.CallbackSecret), RegistrationStatus: "active", RegistrationRequestID: strings.TrimSpace(req.RegistrationRequestID), RequestedFeatures: req.RequestedFeatures, TenantDomains: sanitizePlatformTenantDomains(r.Context(), firstTenantRepo(tenants...), req.TenantDomains), CreatedAt: now, UpdatedAt: now}
		if i := reg.find(entry.PlatformID); i >= 0 {
			entry.CreatedAt = reg.Providers[i].CreatedAt
			reg.Providers[i] = entry
		} else {
			reg.Providers = append(reg.Providers, entry)
		}
		if err := savePlatformProviderRegistry(r.Context(), system, reg); err != nil {
			writeError(w, http.StatusInternalServerError, "PROVIDER_SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "registration_status": "active", "platform_id": entry.PlatformID})
	}
}

func PlatformProviderDiagnoseHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entry, _, ok := authenticatePlatformRequest(w, r, system)
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "platform_id": entry.PlatformID, "registration_status": entry.RegistrationStatus, "checked_at": time.Now().UTC().Format(time.RFC3339)})
	}
}

func PlatformTenantDomainsHandler(system store.SystemSettingsRepository, tenants ...store.TenantRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entry, body, ok := authenticatePlatformRequest(w, r, system)
		if !ok {
			return
		}
		var req struct {
			TenantDomains []platformTenantDomain `json:"tenant_domains"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		reg := loadPlatformProviderRegistry(r.Context(), system)
		count := 0
		if i := reg.find(entry.PlatformID); i >= 0 {
			reg.Providers[i].TenantDomains = sanitizePlatformTenantDomains(r.Context(), firstTenantRepo(tenants...), req.TenantDomains)
			reg.Providers[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			count = len(reg.Providers[i].TenantDomains)
			if err := savePlatformProviderRegistry(r.Context(), system, reg); err != nil {
				writeError(w, http.StatusInternalServerError, "TENANT_DOMAINS_SAVE_FAILED", err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "tenant_domain_count": count})
	}
}

func PlatformTenantsListHandler(system store.SystemSettingsRepository, tenants store.TenantRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entry, _, ok := authenticatePlatformRequest(w, r, system)
		if !ok {
			return
		}
		if tenants == nil {
			writeError(w, http.StatusServiceUnavailable, "TENANT_REPOSITORY_UNAVAILABLE", "tenant repository is unavailable")
			return
		}
		items, err := tenants.List(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "TENANTS_LIST_FAILED", err.Error())
			return
		}
		out := make([]map[string]any, 0, len(items))
		for _, t := range items {
			if !isPlatformTenantVisible(t) {
				continue
			}
			virtualDomain := platformVirtualMailDomainForTenant(entry, t)
			out = append(out, map[string]any{"hub_tenant_id": t.ID, "id": t.ID, "code": t.Slug, "slug": t.Slug, "name": t.Name, "status": t.Status, "primary_domain": t.PrimaryDomain, "domains": tenantEmailDomains(t), "virtual_mail_domain": virtualDomain, "ve_enabled": strings.EqualFold(strings.TrimSpace(t.Status), "active") && virtualDomain != "", "updated_at": t.UpdatedAt.Format(time.RFC3339)})
		}
		writeJSON(w, http.StatusOK, map[string]any{"tenants": out})
	}
}

func PlatformLLMOptionsHandler(system store.SystemSettingsRepository, tenants store.TenantRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, body, ok := authenticatePlatformRequest(w, r, system)
		if !ok {
			return
		}
		var req struct {
			TenantID    string `json:"tenant_id"`
			HubTenantID string `json:"hub_tenant_id"`
		}
		if len(bytes.TrimSpace(body)) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
				return
			}
		}
		tenantID := strings.TrimSpace(req.HubTenantID)
		if tenantID == "" {
			tenantID = strings.TrimSpace(req.TenantID)
		}
		if tenantID == "" {
			writeError(w, http.StatusBadRequest, "TENANT_REQUIRED", "hub_tenant_id is required")
			return
		}
		if !platformTenantIDAllowed(r.Context(), tenants, tenantID) {
			writeError(w, http.StatusNotFound, "TENANT_NOT_FOUND", "Hub tenant not found")
			return
		}
		reg, err := llmservice.LoadRegistry(r.Context(), scopedSystemSettingsForTenant(tenantID, system))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_LOAD_FAILED", err.Error())
			return
		}
		groups := make([]map[string]any, 0, len(reg.ModelServiceGroups))
		for _, group := range reg.ModelServiceGroups {
			id := strings.TrimSpace(group.ID)
			if id == "" || strings.EqualFold(id, llmservice.DefaultModelServiceGroupID) {
				continue
			}
			groups = append(groups, map[string]any{"id": id, "name": strings.TrimSpace(group.Name), "description": strings.TrimSpace(group.Description), "access_policy": strings.TrimSpace(group.AccessPolicy)})
		}
		defaultGroup := ""
		for _, id := range reg.DefaultNewUserServiceGroups {
			if strings.TrimSpace(id) != "" && !strings.EqualFold(strings.TrimSpace(id), llmservice.DefaultModelServiceGroupID) {
				defaultGroup = strings.TrimSpace(id)
				break
			}
		}
		if defaultGroup == "" && len(groups) > 0 {
			defaultGroup, _ = groups[0]["id"].(string)
		}
		baseURL := strings.TrimRight(externalLLMBaseURL(r), "/")
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":                       true,
			"hub_tenant_id":            tenantID,
			"endpoints":                []map[string]any{{"id": "hub-openai-v1", "label": "Hub OpenAI v1", "url": baseURL}},
			"service_groups":           groups,
			"model_service_groups":     groups,
			"default_endpoint":         baseURL,
			"default_service_group_id": defaultGroup,
		})
	}
}

func PlatformTenantAdminsListHandler(system store.SystemSettingsRepository, tenants store.TenantRepository, admins *auth.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _, ok := authenticatePlatformRequest(w, r, system)
		if !ok {
			return
		}
		if tenants == nil || admins == nil {
			writeError(w, http.StatusServiceUnavailable, "TENANT_ADMIN_REPOSITORY_UNAVAILABLE", "tenant admin repository is unavailable")
			return
		}
		items, err := tenants.List(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "TENANTS_LIST_FAILED", err.Error())
			return
		}
		out := []map[string]any{}
		tenantIDs := []string{}
		tenantOut := []map[string]any{}
		for _, t := range items {
			if t == nil || strings.EqualFold(strings.TrimSpace(t.ID), store.DefaultTenantID) || !isPlatformTenantActive(t) {
				continue
			}
			tenantIDs = append(tenantIDs, t.ID)
			tenantOut = append(tenantOut, map[string]any{"hub_tenant_id": t.ID, "id": t.ID, "tenant_id": t.ID, "slug": t.Slug, "name": t.Name, "status": t.Status})
			tenantAdmins, err := admins.ListTenantAdmins(r.Context(), t.ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "TENANT_ADMINS_LIST_FAILED", err.Error())
				return
			}
			for _, admin := range tenantAdmins {
				if admin == nil || !strings.EqualFold(strings.TrimSpace(admin.Status), "active") {
					continue
				}
				out = append(out, map[string]any{"hub_admin_id": admin.ID, "id": admin.ID, "hub_tenant_id": t.ID, "tenant_id": t.ID, "tenant_code": t.Slug, "tenant_name": t.Name, "username": admin.Username, "email": admin.Email, "display_name": admin.DisplayName, "role": admin.Role, "status": admin.Status, "updated_at": admin.UpdatedAt.Format(time.RFC3339)})
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"tenant_ids": tenantIDs, "tenants": tenantOut, "admins": out})
	}
}

func PlatformTenantAdminAuthenticateHandler(system store.SystemSettingsRepository, tenants store.TenantRepository, admins *auth.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, body, ok := authenticatePlatformRequest(w, r, system)
		if !ok {
			return
		}
		if tenants == nil || admins == nil {
			writeError(w, http.StatusServiceUnavailable, "TENANT_ADMIN_AUTH_UNAVAILABLE", "tenant admin authentication is unavailable")
			return
		}
		var req struct {
			HubTenantID string `json:"hub_tenant_id"`
			TenantID    string `json:"tenant_id"`
			Username    string `json:"username"`
			Password    string `json:"password"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		tenantID := strings.TrimSpace(req.HubTenantID)
		if tenantID == "" {
			tenantID = strings.TrimSpace(req.TenantID)
		}
		if tenantID == "" || strings.TrimSpace(req.Username) == "" || req.Password == "" {
			writeError(w, http.StatusBadRequest, "INVALID_TENANT_ADMIN_AUTH", "hub_tenant_id, username, and password are required")
			return
		}
		if t, err := tenants.GetByID(r.Context(), tenantID); err != nil {
			writeError(w, http.StatusInternalServerError, "TENANT_LOAD_FAILED", err.Error())
			return
		} else if !isPlatformTenantActive(t) {
			writeError(w, http.StatusNotFound, "TENANT_NOT_FOUND", "Hub tenant not found")
			return
		}
		admin, err := admins.VerifyScopedCredentials(r.Context(), req.Username, req.Password, tenantID)
		if err != nil || admin == nil {
			writeError(w, http.StatusUnauthorized, "TENANT_ADMIN_AUTH_FAILED", "invalid username or password")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "admin": map[string]any{"hub_admin_id": admin.ID, "id": admin.ID, "hub_tenant_id": tenantID, "tenant_id": tenantID, "username": admin.Username, "email": admin.Email, "display_name": admin.DisplayName, "role": admin.Role, "status": admin.Status}})
	}
}

func platformVirtualMailDomainForTenant(provider platformProviderEntry, tenant *store.Tenant) string {
	if tenant == nil {
		return ""
	}
	tenantID := strings.TrimSpace(tenant.ID)
	for _, domain := range provider.TenantDomains {
		if strings.TrimSpace(domain.VirtualMailDomain) == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(domain.HubTenantID), tenantID) || strings.EqualFold(strings.TrimSpace(domain.TenantID), tenantID) {
			return strings.ToLower(strings.Trim(strings.TrimSpace(domain.VirtualMailDomain), "."))
		}
	}
	base := strings.ToLower(strings.Trim(strings.TrimSpace(provider.VirtualMailDomain), "."))
	if base == "" {
		return ""
	}
	prefix := strings.ToLower(strings.Trim(strings.TrimSpace(tenant.Slug), "."))
	if prefix == "" {
		prefix = strings.ToLower(strings.Trim(strings.TrimSpace(tenantID), "."))
	}
	prefix = strings.NewReplacer("_", "-", " ", "-", ".", "-").Replace(prefix)
	if prefix == "" {
		return base
	}
	return prefix + "." + base
}

func PlatformMigrationSubmitHandler(system store.SystemSettingsRepository, tenants store.TenantRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provider, body, ok := authenticatePlatformRequest(w, r, system)
		if !ok {
			return
		}
		var req struct {
			MigrationID         string `json:"migration_id"`
			TenantID            string `json:"tenant_id"`
			HubTenantID         string `json:"hub_tenant_id"`
			Title               string `json:"title"`
			TargetEmployeeID    string `json:"target_employee_id"`
			TargetHubEmployeeID string `json:"target_hub_employee_id"`
			TargetHubAccountID  string `json:"target_hub_account_id"`
			HubRequestID        string `json:"hub_request_id"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		migrationID := strings.TrimSpace(req.MigrationID)
		if migrationID == "" {
			writeError(w, http.StatusBadRequest, "MIGRATION_ID_REQUIRED", "migration_id is required")
			return
		}
		tenantID := strings.TrimSpace(req.HubTenantID)
		if tenantID == "" {
			tenantID = strings.TrimSpace(req.TenantID)
		}
		if tenantID == "" {
			writeError(w, http.StatusBadRequest, "TENANT_REQUIRED", "hub_tenant_id is required")
			return
		}
		if tenants != nil {
			if t, err := tenants.GetByID(r.Context(), tenantID); err != nil {
				writeError(w, http.StatusInternalServerError, "TENANT_LOAD_FAILED", err.Error())
				return
			} else if !isPlatformTenantActive(t) {
				writeError(w, http.StatusNotFound, "TENANT_NOT_FOUND", "Hub tenant not found")
				return
			}
		}
		platformEmployeeID := strings.TrimSpace(req.TargetEmployeeID)
		callbackHubEmployeeID := strings.TrimSpace(req.TargetHubEmployeeID)
		callbackHubAccountID := strings.TrimSpace(req.TargetHubAccountID)
		if platformEmployeeID != "" {
			entry, found := platformEmployeeInTenant(r.Context(), system, tenantID, provider.PlatformID, platformEmployeeID)
			if !found {
				writeError(w, http.StatusNotFound, "EMPLOYEE_NOT_FOUND", "target platform employee was not found in Hub registry")
				return
			}
			if callbackHubEmployeeID != "" && callbackHubEmployeeID != firstNonEmpty(entry.ID, entry.MachineID) {
				writeError(w, http.StatusForbidden, "EMPLOYEE_IDENTITY_MISMATCH", "target_hub_employee_id does not match the registered platform employee")
				return
			}
			if callbackHubAccountID != "" && strings.TrimSpace(entry.OwnerUserID) != "" && callbackHubAccountID != strings.TrimSpace(entry.OwnerUserID) {
				writeError(w, http.StatusForbidden, "EMPLOYEE_IDENTITY_MISMATCH", "target_hub_account_id does not match the registered platform employee")
				return
			}
			callbackHubEmployeeID = firstNonEmpty(callbackHubEmployeeID, entry.ID, entry.MachineID)
			callbackHubAccountID = firstNonEmpty(callbackHubAccountID, entry.OwnerUserID)
		}
		exportJobID := "hub_export_" + randomHexID(10)
		callback := map[string]any{
			"migration_id":      migrationID,
			"status":            "approved",
			"hub_export_job_id": exportJobID,
			"hub_tenant_id":     tenantID,
			"hub_employee_id":   callbackHubEmployeeID,
			"hub_account_id":    callbackHubAccountID,
			"message":           "migration request approved by Hub platform interface",
		}
		go postPlatformMigrationCallbacks(provider, callback)
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "migration_id": migrationID, "status": "approved", "hub_export_job_id": exportJobID,
			"hub_tenant_id": tenantID, "hub_request_id": strings.TrimSpace(req.HubRequestID)})
	}
}

func postPlatformMigrationCallbacks(provider platformProviderEntry, callback map[string]any) {
	postPlatformCallback(provider, "/api/hub/callback/migration", callback)
	completed := copyStringAnyMap(callback)
	completed["status"] = "completed"
	completed["message"] = "migration export completed by Hub platform interface"
	postPlatformCallback(provider, "/api/hub/callback/migration", completed)
}

func PlatformMigrationCancelHandler(system store.SystemSettingsRepository, tenants store.TenantRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provider, body, ok := authenticatePlatformRequest(w, r, system)
		if !ok {
			return
		}
		var req struct {
			MigrationID         string `json:"migration_id"`
			TenantID            string `json:"tenant_id"`
			HubTenantID         string `json:"hub_tenant_id"`
			TargetEmployeeID    string `json:"target_employee_id"`
			TargetHubEmployeeID string `json:"target_hub_employee_id"`
			TargetHubAccountID  string `json:"target_hub_account_id"`
			HubExportJobID      string `json:"hub_export_job_id"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		migrationID := firstNonEmpty(strings.TrimSpace(req.MigrationID), strings.TrimSpace(r.PathValue("id")))
		if migrationID == "" {
			writeError(w, http.StatusBadRequest, "MIGRATION_ID_REQUIRED", "migration_id is required")
			return
		}
		tenantID := firstNonEmpty(strings.TrimSpace(req.HubTenantID), strings.TrimSpace(req.TenantID))
		if tenantID == "" {
			writeError(w, http.StatusBadRequest, "TENANT_REQUIRED", "hub_tenant_id is required")
			return
		}
		if !platformTenantIDAllowed(r.Context(), tenants, tenantID) {
			writeError(w, http.StatusNotFound, "TENANT_NOT_FOUND", "Hub tenant not found")
			return
		}
		callbackHubEmployeeID := strings.TrimSpace(req.TargetHubEmployeeID)
		callbackHubAccountID := strings.TrimSpace(req.TargetHubAccountID)
		if platformEmployeeID := strings.TrimSpace(req.TargetEmployeeID); platformEmployeeID != "" {
			entry, found := platformEmployeeInTenant(r.Context(), system, tenantID, provider.PlatformID, platformEmployeeID)
			if !found {
				writeError(w, http.StatusNotFound, "EMPLOYEE_NOT_FOUND", "target platform employee was not found in Hub registry")
				return
			}
			if callbackHubEmployeeID != "" && callbackHubEmployeeID != firstNonEmpty(entry.ID, entry.MachineID) {
				writeError(w, http.StatusForbidden, "EMPLOYEE_IDENTITY_MISMATCH", "target_hub_employee_id does not match the registered platform employee")
				return
			}
			if callbackHubAccountID != "" && strings.TrimSpace(entry.OwnerUserID) != "" && callbackHubAccountID != strings.TrimSpace(entry.OwnerUserID) {
				writeError(w, http.StatusForbidden, "EMPLOYEE_IDENTITY_MISMATCH", "target_hub_account_id does not match the registered platform employee")
				return
			}
			callbackHubEmployeeID = firstNonEmpty(callbackHubEmployeeID, entry.ID, entry.MachineID)
			callbackHubAccountID = firstNonEmpty(callbackHubAccountID, entry.OwnerUserID)
		}
		callback := map[string]any{"migration_id": migrationID, "status": "canceled", "hub_export_job_id": strings.TrimSpace(req.HubExportJobID), "hub_tenant_id": tenantID, "hub_employee_id": callbackHubEmployeeID, "hub_account_id": callbackHubAccountID, "message": "migration canceled by Platform"}
		go postPlatformCallback(provider, "/api/hub/callback/migration", callback)
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "migration_id": migrationID, "status": "canceled", "hub_tenant_id": tenantID})
	}
}

func copyStringAnyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func PlatformKnowledgeImportHandler(system store.SystemSettingsRepository, tenants store.TenantRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provider, body, ok := authenticatePlatformRequest(w, r, system)
		if !ok {
			return
		}
		var req struct {
			ImportID           string `json:"import_id"`
			EmployeeID         string `json:"employee_id"`
			PlatformEmployeeID string `json:"platform_employee_id"`
			HubTenantID        string `json:"hub_tenant_id"`
			TenantID           string `json:"tenant_id"`
			Title              string `json:"title"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		importID := strings.TrimSpace(req.ImportID)
		if importID == "" {
			writeError(w, http.StatusBadRequest, "IMPORT_ID_REQUIRED", "import_id is required")
			return
		}
		tenantID := strings.TrimSpace(req.HubTenantID)
		if tenantID == "" {
			tenantID = strings.TrimSpace(req.TenantID)
		}
		if tenantID == "" {
			writeError(w, http.StatusBadRequest, "TENANT_REQUIRED", "hub_tenant_id is required")
			return
		}
		if tenants != nil {
			if t, err := tenants.GetByID(r.Context(), tenantID); err != nil {
				writeError(w, http.StatusInternalServerError, "TENANT_LOAD_FAILED", err.Error())
				return
			} else if !isPlatformTenantActive(t) {
				writeError(w, http.StatusNotFound, "TENANT_NOT_FOUND", "Hub tenant not found")
				return
			}
		}
		platformEmployeeID := strings.TrimSpace(req.PlatformEmployeeID)
		if platformEmployeeID == "" {
			platformEmployeeID = strings.TrimSpace(req.EmployeeID)
		}
		if platformEmployeeID == "" {
			writeError(w, http.StatusBadRequest, "EMPLOYEE_ID_REQUIRED", "employee_id is required")
			return
		}
		employeeEntry, found := platformEmployeeInTenant(r.Context(), system, tenantID, provider.PlatformID, platformEmployeeID)
		if !found {
			writeError(w, http.StatusNotFound, "EMPLOYEE_NOT_FOUND", "platform employee was not found in Hub registry")
			return
		}
		jobID := "hub_kimp_" + randomHexID(10)
		callback := map[string]any{
			"import_id":         importID,
			"status":            "accepted",
			"hub_import_job_id": jobID,
			"hub_tenant_id":     tenantID,
			"hub_employee_id":   firstNonEmpty(employeeEntry.ID, employeeEntry.MachineID),
			"hub_account_id":    employeeEntry.OwnerUserID,
			"message":           "knowledge import accepted by Hub platform interface",
		}
		go postPlatformKnowledgeCallbacks(provider, callback)
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "import_id": importID, "employee_id": platformEmployeeID, "hub_tenant_id": tenantID, "status": "accepted", "hub_import_job_id": jobID})
	}
}

func postPlatformKnowledgeCallbacks(provider platformProviderEntry, callback map[string]any) {
	postPlatformCallback(provider, "/api/hub/callback/knowledge-import", callback)
	completed := copyStringAnyMap(callback)
	completed["status"] = "completed"
	completed["message"] = "knowledge import completed by Hub platform interface"
	postPlatformCallback(provider, "/api/hub/callback/knowledge-import", completed)
}

func PlatformKnowledgeImportCancelHandler(system store.SystemSettingsRepository, tenants store.TenantRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provider, body, ok := authenticatePlatformRequest(w, r, system)
		if !ok {
			return
		}
		var req struct {
			ImportID           string `json:"import_id"`
			EmployeeID         string `json:"employee_id"`
			PlatformEmployeeID string `json:"platform_employee_id"`
			HubTenantID        string `json:"hub_tenant_id"`
			TenantID           string `json:"tenant_id"`
			HubEmployeeID      string `json:"hub_employee_id"`
			HubAccountID       string `json:"hub_account_id"`
			HubImportJobID     string `json:"hub_import_job_id"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		importID := firstNonEmpty(strings.TrimSpace(req.ImportID), strings.TrimSpace(r.PathValue("id")))
		if importID == "" {
			writeError(w, http.StatusBadRequest, "IMPORT_ID_REQUIRED", "import_id is required")
			return
		}
		tenantID := firstNonEmpty(strings.TrimSpace(req.HubTenantID), strings.TrimSpace(req.TenantID))
		if tenantID == "" {
			writeError(w, http.StatusBadRequest, "TENANT_REQUIRED", "hub_tenant_id is required")
			return
		}
		if !platformTenantIDAllowed(r.Context(), tenants, tenantID) {
			writeError(w, http.StatusNotFound, "TENANT_NOT_FOUND", "Hub tenant not found")
			return
		}
		platformEmployeeID := firstNonEmpty(strings.TrimSpace(req.PlatformEmployeeID), strings.TrimSpace(req.EmployeeID))
		if platformEmployeeID == "" {
			writeError(w, http.StatusBadRequest, "EMPLOYEE_ID_REQUIRED", "employee_id is required")
			return
		}
		employeeEntry, found := platformEmployeeInTenant(r.Context(), system, tenantID, provider.PlatformID, platformEmployeeID)
		if !found {
			writeError(w, http.StatusNotFound, "EMPLOYEE_NOT_FOUND", "platform employee was not found in Hub registry")
			return
		}
		if strings.TrimSpace(req.HubEmployeeID) != "" && strings.TrimSpace(req.HubEmployeeID) != firstNonEmpty(employeeEntry.ID, employeeEntry.MachineID) {
			writeError(w, http.StatusForbidden, "EMPLOYEE_IDENTITY_MISMATCH", "hub_employee_id does not match the registered platform employee")
			return
		}
		if strings.TrimSpace(req.HubAccountID) != "" && strings.TrimSpace(employeeEntry.OwnerUserID) != "" && strings.TrimSpace(req.HubAccountID) != strings.TrimSpace(employeeEntry.OwnerUserID) {
			writeError(w, http.StatusForbidden, "EMPLOYEE_IDENTITY_MISMATCH", "hub_account_id does not match the registered platform employee")
			return
		}
		callback := map[string]any{"import_id": importID, "status": "canceled", "hub_import_job_id": strings.TrimSpace(req.HubImportJobID), "hub_tenant_id": tenantID, "hub_employee_id": firstNonEmpty(employeeEntry.ID, employeeEntry.MachineID), "hub_account_id": employeeEntry.OwnerUserID, "message": "knowledge import canceled by Platform"}
		go postPlatformCallback(provider, "/api/hub/callback/knowledge-import", callback)
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "import_id": importID, "status": "canceled", "hub_tenant_id": tenantID})
	}
}

func PlatformSyncJobRunHandler(system store.SystemSettingsRepository, tenants store.TenantRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provider, body, ok := authenticatePlatformRequest(w, r, system)
		if !ok {
			return
		}
		var req struct {
			JobID              string `json:"job_id"`
			EmployeeID         string `json:"employee_id"`
			PlatformEmployeeID string `json:"platform_employee_id"`
			HubTenantID        string `json:"hub_tenant_id"`
			TenantID           string `json:"tenant_id"`
			HubEmployeeID      string `json:"hub_employee_id"`
			HubAccountID       string `json:"hub_account_id"`
			Mode               string `json:"mode"`
			SyncCursor         string `json:"sync_cursor"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		jobID := strings.TrimSpace(req.JobID)
		if jobID == "" {
			jobID = strings.TrimSpace(r.PathValue("id"))
		}
		if jobID == "" {
			writeError(w, http.StatusBadRequest, "SYNC_JOB_ID_REQUIRED", "job_id is required")
			return
		}
		tenantID := strings.TrimSpace(req.HubTenantID)
		if tenantID == "" {
			tenantID = strings.TrimSpace(req.TenantID)
		}
		if tenantID == "" {
			writeError(w, http.StatusBadRequest, "TENANT_REQUIRED", "hub_tenant_id is required")
			return
		}
		if !platformTenantIDAllowed(r.Context(), tenants, tenantID) {
			writeError(w, http.StatusNotFound, "TENANT_NOT_FOUND", "Hub tenant not found")
			return
		}
		platformEmployeeID := strings.TrimSpace(req.PlatformEmployeeID)
		if platformEmployeeID == "" {
			platformEmployeeID = strings.TrimSpace(req.EmployeeID)
		}
		if platformEmployeeID == "" {
			writeError(w, http.StatusBadRequest, "EMPLOYEE_ID_REQUIRED", "employee_id is required")
			return
		}
		employeeEntry, found := platformEmployeeInTenant(r.Context(), system, tenantID, provider.PlatformID, platformEmployeeID)
		if !found {
			writeError(w, http.StatusNotFound, "EMPLOYEE_NOT_FOUND", "platform employee was not found in Hub registry")
			return
		}
		if strings.TrimSpace(req.HubEmployeeID) != "" && strings.TrimSpace(req.HubEmployeeID) != firstNonEmpty(employeeEntry.ID, employeeEntry.MachineID) {
			writeError(w, http.StatusForbidden, "EMPLOYEE_IDENTITY_MISMATCH", "hub_employee_id does not match the registered platform employee")
			return
		}
		if strings.TrimSpace(req.HubAccountID) != "" && strings.TrimSpace(employeeEntry.OwnerUserID) != "" && strings.TrimSpace(req.HubAccountID) != strings.TrimSpace(employeeEntry.OwnerUserID) {
			writeError(w, http.StatusForbidden, "EMPLOYEE_IDENTITY_MISMATCH", "hub_account_id does not match the registered platform employee")
			return
		}
		hubSyncJobID := "hub_sync_" + randomHexID(10)
		callback := map[string]any{
			"job_id":          jobID,
			"status":          "accepted",
			"hub_sync_job_id": hubSyncJobID,
			"hub_tenant_id":   tenantID,
			"hub_employee_id": firstNonEmpty(employeeEntry.ID, employeeEntry.MachineID),
			"hub_account_id":  employeeEntry.OwnerUserID,
			"message":         "sync job accepted by Hub platform interface",
		}
		go postPlatformSyncCallbacks(provider, callback)
		writeJSON(w, http.StatusAccepted, map[string]any{
			"ok":              true,
			"job_id":          jobID,
			"status":          "accepted",
			"hub_sync_job_id": hubSyncJobID,
			"hub_tenant_id":   tenantID,
			"hub_employee_id": firstNonEmpty(employeeEntry.ID, employeeEntry.MachineID),
			"hub_account_id":  employeeEntry.OwnerUserID,
		})
	}
}

func postPlatformSyncCallbacks(provider platformProviderEntry, callback map[string]any) {
	postPlatformCallback(provider, "/api/hub/callback/sync", callback)
	completed := copyStringAnyMap(callback)
	completed["status"] = "completed"
	completed["message"] = "sync job completed by Hub platform interface"
	postPlatformCallback(provider, "/api/hub/callback/sync", completed)
}

func platformEmployeeExistsInTenant(ctx context.Context, system store.SystemSettingsRepository, tenantID, platformID, platformEmployeeID string) bool {
	_, ok := platformEmployeeInTenant(ctx, system, tenantID, platformID, platformEmployeeID)
	return ok
}

func platformEmployeeInTenant(ctx context.Context, system store.SystemSettingsRepository, tenantID, platformID, platformEmployeeID string) (digitalEmployeeEntry, bool) {
	platformID = strings.TrimSpace(platformID)
	platformEmployeeID = strings.TrimSpace(platformEmployeeID)
	if tenantID == "" || platformEmployeeID == "" {
		return digitalEmployeeEntry{}, false
	}
	registry := loadVERegistry(ctx, scopedSystemSettingsForTenant(tenantID, system))
	for _, employee := range registry.Employees {
		if platformID != "" && !strings.EqualFold(strings.TrimSpace(employee.PlatformID), platformID) {
			continue
		}
		if platformEmployeeIDMatchesEntry(employee, platformEmployeeID) {
			return employee, true
		}
	}
	return digitalEmployeeEntry{}, false
}

func platformEmployeeIDMatchesEntry(entry digitalEmployeeEntry, platformEmployeeID string) bool {
	platformEmployeeID = strings.TrimSpace(platformEmployeeID)
	if platformEmployeeID == "" {
		return false
	}
	entryPlatformEmployeeID := strings.TrimSpace(entry.PlatformEmployeeID)
	if strings.EqualFold(entryPlatformEmployeeID, platformEmployeeID) {
		return true
	}
	if entryPlatformEmployeeID != "" {
		return false
	}
	if normalizeVEEmployeeType(entry.EmployeeType) == veEmployeeTypePhysical {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(entry.ID), platformEmployeeID) || strings.EqualFold(strings.TrimSpace(entry.MachineID), platformEmployeeID)
}

func postPlatformCallback(provider platformProviderEntry, path string, payload map[string]any) {
	if strings.TrimSpace(provider.CallbackBaseURL) == "" {
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	endpoint := strings.TrimRight(provider.CallbackBaseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(provider.CallbackSecret) != "" {
		req.Header.Set("X-VE-Callback-Secret", provider.CallbackSecret)
	}
	setPlatformCallbackReplayHeaders(req, "cb")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
}

func setPlatformCallbackReplayHeaders(req *http.Request, prefix string) {
	if req == nil {
		return
	}
	req.Header.Set("X-VE-Callback-Timestamp", time.Now().UTC().Format(time.RFC3339Nano))
	req.Header.Set("X-VE-Callback-Nonce", prefix+"_"+randomHexID(16))
}

type platformMachineLister interface {
	ListByTenant(ctx context.Context, tenantID string) ([]*store.Machine, error)
}

func firstPlatformMachineLister(machineRepos ...platformMachineLister) platformMachineLister {
	if len(machineRepos) == 0 {
		return nil
	}
	return machineRepos[0]
}

type platformSourceUserSyncStats struct {
	ExcludedDesktopEnrolled   int `json:"excluded_desktop_enrolled"`
	ExcludedPlatformEmployees int `json:"excluded_platform_employees"`
}

type platformSourceUserSyncOptions struct {
	IncludeDesktopEnrolled   bool
	IncludePlatformEmployees bool
}

func PlatformSourceUsersSyncHandler(system store.SystemSettingsRepository, users store.UserRepository, tenants store.TenantRepository, machineRepos ...platformMachineLister) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, body, ok := authenticatePlatformRequest(w, r, system)
		if !ok {
			return
		}
		var req struct {
			TenantID                  string `json:"tenant_id"`
			HubTenantID               string `json:"hub_tenant_id"`
			IncludeDesktopEnrolled    bool   `json:"include_desktop_enrolled"`
			IncludeDesktopEnrolledAlt bool   `json:"includeDesktopEnrolled"`
			IncludePlatformEmployees  bool   `json:"include_platform_employees"`
			IncludePlatformEmployees2 bool   `json:"includePlatformEmployees"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		tenantID := strings.TrimSpace(req.HubTenantID)
		if tenantID == "" {
			tenantID = strings.TrimSpace(req.TenantID)
		}
		if tenantID == "" {
			writeError(w, http.StatusBadRequest, "TENANT_REQUIRED", "hub_tenant_id is required")
			return
		}
		if !platformTenantIDAllowed(r.Context(), tenants, tenantID) {
			writeError(w, http.StatusNotFound, "TENANT_NOT_FOUND", "Hub tenant not found")
			return
		}
		var machines platformMachineLister
		if len(machineRepos) > 0 {
			machines = machineRepos[0]
		}
		options := platformSourceUserSyncOptions{IncludeDesktopEnrolled: req.IncludeDesktopEnrolled || req.IncludeDesktopEnrolledAlt, IncludePlatformEmployees: req.IncludePlatformEmployees || req.IncludePlatformEmployees2}
		out, stats, err := platformSourceUsersForTenantWithStatsAndOptions(r.Context(), system, users, tenantID, options, machines)
		if err != nil {
			if errors.Is(err, errPlatformUserRepositoryUnavailable) {
				writeError(w, http.StatusServiceUnavailable, "USER_REPOSITORY_UNAVAILABLE", "user repository is unavailable")
				return
			}
			writeError(w, http.StatusInternalServerError, "SOURCE_USERS_LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": out, "users": out, "sync_summary": stats})
	}
}

var errPlatformUserRepositoryUnavailable = errors.New("user repository is unavailable")

func platformSourceUsersForTenant(ctx context.Context, system store.SystemSettingsRepository, users store.UserRepository, tenantID string, machineRepos ...platformMachineLister) ([]map[string]any, error) {
	out, _, err := platformSourceUsersForTenantWithStats(ctx, system, users, tenantID, machineRepos...)
	return out, err
}

func platformSourceUsersForTenantWithStats(ctx context.Context, system store.SystemSettingsRepository, users store.UserRepository, tenantID string, machineRepos ...platformMachineLister) ([]map[string]any, platformSourceUserSyncStats, error) {
	return platformSourceUsersForTenantWithStatsAndOptions(ctx, system, users, tenantID, platformSourceUserSyncOptions{}, firstPlatformMachineLister(machineRepos...))
}

func platformSourceUsersForTenantWithStatsAndOptions(ctx context.Context, system store.SystemSettingsRepository, users store.UserRepository, tenantID string, options platformSourceUserSyncOptions, machines platformMachineLister) ([]map[string]any, platformSourceUserSyncStats, error) {
	stats := platformSourceUserSyncStats{}
	if users == nil {
		return nil, stats, errPlatformUserRepositoryUnavailable
	}
	items, err := users.ListByTenant(ctx, tenantID)
	if err != nil {
		return nil, stats, err
	}
	excludedIDs, excludedEmails := platformEmployeeAccountExclusions(ctx, system, tenantID)
	platformEmployeeUserIDs := make(map[string]struct{}, len(excludedIDs))
	for id := range excludedIDs {
		platformEmployeeUserIDs[id] = struct{}{}
	}
	platformEmployeeEmails := make(map[string]struct{}, len(excludedEmails))
	for email := range excludedEmails {
		platformEmployeeEmails[email] = struct{}{}
	}
	if options.IncludePlatformEmployees {
		excludedIDs = map[string]struct{}{}
		excludedEmails = map[string]struct{}{}
	}
	desktopUserIDs := map[string]struct{}{}
	if machines != nil {
		machineUserIDs, err := platformMachineAccountExclusions(ctx, machines, tenantID)
		if err != nil {
			return nil, stats, err
		}
		for id := range machineUserIDs {
			desktopUserIDs[id] = struct{}{}
			if !options.IncludeDesktopEnrolled {
				excludedIDs[id] = struct{}{}
			}
		}
	}
	out := make([]map[string]any, 0, len(items))
	for _, user := range items {
		if user == nil {
			continue
		}
		if !platformHubUserActive(user) {
			continue
		}
		userID := strings.TrimSpace(user.ID)
		userEmail := strings.ToLower(strings.TrimSpace(user.Email))
		if _, ok := excludedIDs[userID]; ok {
			if _, platformEmployee := platformEmployeeUserIDs[userID]; platformEmployee {
				stats.ExcludedPlatformEmployees++
			} else if _, desktop := desktopUserIDs[userID]; desktop {
				stats.ExcludedDesktopEnrolled++
			} else {
				stats.ExcludedPlatformEmployees++
			}
			continue
		}
		if _, ok := excludedEmails[userEmail]; ok {
			stats.ExcludedPlatformEmployees++
			continue
		}
		_, platformEmployeeID := platformEmployeeUserIDs[userID]
		_, platformEmployeeEmail := platformEmployeeEmails[userEmail]
		isPlatformEmployee := platformEmployeeID || platformEmployeeEmail
		accountType := "physical_employee"
		provider := "hub"
		if isPlatformEmployee {
			accountType = "virtual_employee"
			provider = "virtualemployee-platform"
		}
		out = append(out, map[string]any{
			"id":                  user.ID,
			"tenant_id":           tenantID,
			"external_id":         user.ID,
			"email":               user.Email,
			"display_name":        user.Email,
			"department":          "",
			"title":               "",
			"status":              user.Status,
			"account_type":        accountType,
			"provider":            provider,
			"is_virtual_employee": isPlatformEmployee,
			"updated_at":          user.UpdatedAt.Format(time.RFC3339),
		})
	}
	return out, stats, nil
}

func platformHubUserActive(user *store.User) bool {
	if user == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(user.Status)) {
	case "active", "approved", "statusapproved", "status_approved", "enabled", "normal":
		return true
	default:
		return false
	}
}

func platformMachineAccountExclusions(ctx context.Context, machines platformMachineLister, tenantID string) (map[string]struct{}, error) {
	ids := map[string]struct{}{}
	if machines == nil {
		return ids, nil
	}
	items, err := machines.ListByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	for _, machine := range items {
		if machine == nil {
			continue
		}
		if id := strings.TrimSpace(machine.UserID); id != "" {
			ids[id] = struct{}{}
		}
	}
	return ids, nil
}

func platformEmployeeAccountExclusions(ctx context.Context, system store.SystemSettingsRepository, tenantID string) (map[string]struct{}, map[string]struct{}) {
	ids := map[string]struct{}{}
	emails := map[string]struct{}{}
	registry := loadVERegistry(ctx, scopedSystemSettingsForTenant(tenantID, system))
	for _, employee := range registry.Employees {
		if strings.TrimSpace(employee.PlatformID) == "" && strings.TrimSpace(employee.PlatformEmployeeID) == "" {
			continue
		}
		if id := strings.TrimSpace(employee.OwnerUserID); id != "" {
			ids[id] = struct{}{}
		}
		if email := strings.ToLower(strings.TrimSpace(employee.OwnerEmail)); email != "" {
			emails[email] = struct{}{}
		}
	}
	return ids, emails
}

func PlatformSourceUserViewerTokenHandler(system store.SystemSettingsRepository, tenants store.TenantRepository, users store.UserRepository, identities ...*auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, body, ok := authenticatePlatformRequest(w, r, system)
		if !ok {
			return
		}
		if users == nil {
			writeError(w, http.StatusServiceUnavailable, "USER_REPOSITORY_UNAVAILABLE", "user repository is unavailable")
			return
		}
		if len(identities) == 0 || identities[0] == nil {
			writeError(w, http.StatusServiceUnavailable, "IDENTITY_SERVICE_UNAVAILABLE", "identity service is unavailable")
			return
		}
		var req struct {
			SourceUserID string `json:"source_user_id"`
			ExternalID   string `json:"external_id"`
			Email        string `json:"email"`
			HubTenantID  string `json:"hub_tenant_id"`
			TenantID     string `json:"tenant_id"`
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
				return
			}
		}
		tenantID := firstNonEmpty(strings.TrimSpace(req.HubTenantID), strings.TrimSpace(req.TenantID))
		if tenantID == "" {
			writeError(w, http.StatusBadRequest, "TENANT_ID_REQUIRED", "hub_tenant_id is required")
			return
		}
		if !platformTenantIDAllowed(r.Context(), tenants, tenantID) {
			writeError(w, http.StatusNotFound, "TENANT_NOT_FOUND", "Hub tenant not found")
			return
		}
		var user *store.User
		var err error
		seenUserIDs := map[string]struct{}{}
		for _, userID := range []string{strings.TrimSpace(req.SourceUserID), strings.TrimSpace(r.PathValue("id")), strings.TrimSpace(req.ExternalID)} {
			if userID == "" {
				continue
			}
			if _, seen := seenUserIDs[userID]; seen {
				continue
			}
			seenUserIDs[userID] = struct{}{}
			candidate, lookupErr := users.GetByID(r.Context(), userID)
			if lookupErr != nil {
				err = lookupErr
				break
			}
			if candidate == nil || strings.TrimSpace(candidate.TenantID) != tenantID {
				continue
			}
			if strings.TrimSpace(req.Email) != "" && !strings.EqualFold(strings.TrimSpace(candidate.Email), strings.TrimSpace(req.Email)) {
				continue
			}
			user = candidate
			break
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "USER_LOOKUP_FAILED", err.Error())
			return
		}
		if user == nil && strings.TrimSpace(req.Email) != "" {
			user, err = users.GetByTenantEmail(r.Context(), tenantID, strings.TrimSpace(req.Email))
			if err != nil {
				writeError(w, http.StatusInternalServerError, "USER_LOOKUP_FAILED", err.Error())
				return
			}
		}
		if user == nil || strings.TrimSpace(user.TenantID) != tenantID || !platformHubUserActive(user) {
			writeError(w, http.StatusNotFound, "SOURCE_USER_NOT_FOUND", "source user was not found in Hub tenant")
			return
		}
		if excluded, err := platformUserExcludedFromSourceUsers(r.Context(), system, identities[0].MachinesRepo(), tenantID, user); err != nil {
			writeError(w, http.StatusInternalServerError, "SOURCE_USER_EXCLUSION_LOOKUP_FAILED", err.Error())
			return
		} else if excluded {
			writeError(w, http.StatusNotFound, "SOURCE_USER_NOT_FOUND", "source user was not found in Hub tenant")
			return
		}
		viewerToken, err := identities[0].IssueViewerTokenForUser(r.Context(), user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "VIEWER_TOKEN_ISSUE_FAILED", err.Error())
			return
		}
		if strings.TrimSpace(viewerToken) == "" {
			writeError(w, http.StatusInternalServerError, "VIEWER_TOKEN_EMPTY", "Hub did not issue a viewer token")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "source_user_id": user.ID, "hub_account_id": user.ID, "hub_tenant_id": tenantID, "viewer_token": viewerToken, "hub_llm_viewer_token": viewerToken, "access_token": viewerToken})
	}
}

func platformUserExcludedFromSourceUsers(ctx context.Context, system store.SystemSettingsRepository, machines platformMachineLister, tenantID string, user *store.User) (bool, error) {
	if user == nil {
		return true, nil
	}
	userID := strings.TrimSpace(user.ID)
	userEmail := strings.ToLower(strings.TrimSpace(user.Email))
	excludedIDs, excludedEmails := platformEmployeeAccountExclusions(ctx, system, tenantID)
	if _, ok := excludedIDs[userID]; ok {
		return true, nil
	}
	if _, ok := excludedEmails[userEmail]; ok {
		return true, nil
	}
	return platformUserHasTenantMachine(ctx, machines, tenantID, userID)
}

func platformUserHasTenantMachine(ctx context.Context, machines platformMachineLister, tenantID, userID string) (bool, error) {
	userID = strings.TrimSpace(userID)
	if machines == nil || strings.TrimSpace(tenantID) == "" || userID == "" {
		return false, nil
	}
	excludedIDs, err := platformMachineAccountExclusions(ctx, machines, tenantID)
	if err != nil {
		return false, err
	}
	_, ok := excludedIDs[userID]
	return ok, nil
}

func PlatformEmployeeRegisterHandler(system store.SystemSettingsRepository, tenants store.TenantRepository, users store.UserRepository, identities ...*auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entry, body, ok := authenticatePlatformRequest(w, r, system)
		if !ok {
			return
		}
		var req platformEmployeeRequest
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		tenantID := strings.TrimSpace(req.HubTenantID)
		if tenantID == "" {
			tenantID = strings.TrimSpace(req.TenantID)
		}
		email := strings.ToLower(strings.TrimSpace(req.VirtualEmail))
		name := strings.TrimSpace(req.Name)
		platformEmployeeID := strings.TrimSpace(req.PlatformEmployeeID)
		if platformEmployeeID == "" {
			platformEmployeeID = strings.TrimSpace(req.EmployeeID)
		}
		if tenantID == "" || email == "" || name == "" {
			writeError(w, http.StatusBadRequest, "INVALID_EMPLOYEE", "hub_tenant_id, virtual_email and name are required")
			return
		}
		if platformEmployeeID == "" {
			writeError(w, http.StatusBadRequest, "EMPLOYEE_ID_REQUIRED", "employee_id is required")
			return
		}
		avatarDataURL := strings.TrimSpace(req.AvatarDataURL)
		if avatarDataURL != "" {
			if len(avatarDataURL) > veAvatarDataURLMaxSize {
				writeError(w, http.StatusBadRequest, "INVALID_EMPLOYEE", "avatar image is too large")
				return
			}
			valid, tooLarge := validateVEAvatarDataURL(avatarDataURL)
			if tooLarge {
				writeError(w, http.StatusBadRequest, "INVALID_EMPLOYEE", "avatar image is too large")
				return
			}
			if !valid {
				writeError(w, http.StatusBadRequest, "INVALID_EMPLOYEE", "avatar image must be a data URL")
				return
			}
		}
		if users == nil {
			writeError(w, http.StatusServiceUnavailable, "USER_REPOSITORY_UNAVAILABLE", "user repository is unavailable")
			return
		}
		if tenants != nil {
			if t, err := tenants.GetByID(r.Context(), tenantID); err != nil {
				writeError(w, http.StatusInternalServerError, "TENANT_LOAD_FAILED", err.Error())
				return
			} else if !isPlatformTenantActive(t) {
				writeError(w, http.StatusNotFound, "TENANT_NOT_FOUND", "Hub tenant not found")
				return
			}
		}
		runtimeProviderID := strings.TrimSpace(req.RuntimeProviderID)
		if runtimeProviderID != "" && !strings.EqualFold(runtimeProviderID, maclawSrvRuntimePlatformID) {
			writeError(w, http.StatusBadRequest, "UNSUPPORTED_RUNTIME_PROVIDER", "platform digital employees must run in MaClawSrv runtime")
			return
		}
		runtimeProviderID = maclawSrvRuntimePlatformID
		runtimeBaseURL := strings.TrimSpace(req.RuntimeBaseURL)
		if runtimeBaseURL == "" {
			if _, ok := loadMacLawSrvRuntimeRegistry(r.Context(), system).findForTenant(tenantID); !ok {
				writeError(w, http.StatusBadRequest, "MACLAWSRV_RUNTIME_REQUIRED", "runtime_base_url is required because platform digital employees run in MaClawSrv runtime")
				return
			}
		}
		tenantSystem := scopedSystemSettingsForTenant(tenantID, system)
		if err := validatePlatformEmployeeLLMServiceGroup(r.Context(), tenantSystem, firstNonEmpty(req.LLMServiceGroupID, req.DefaultLLM)); err != nil {
			writeError(w, http.StatusBadRequest, "LLM_SERVICE_GROUP_ENTITLEMENT_FAILED", err.Error())
			return
		}
		userID := ""
		createdUser := false
		existing, err := users.GetByTenantEmail(r.Context(), tenantID, email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "USER_LOOKUP_FAILED", err.Error())
			return
		}
		if existing != nil {
			userID = existing.ID
		} else {
			now := time.Now().UTC()
			userID = "veusr_" + randomHexID(12)
			if err := users.Create(r.Context(), &store.User{ID: userID, TenantID: tenantID, Email: email, SN: "VE-" + strings.ToUpper(randomHexID(4)), Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}); err != nil {
				writeError(w, http.StatusConflict, "USER_CREATE_FAILED", err.Error())
				return
			}
			createdUser = true
		}
		reg := loadVERegistry(r.Context(), tenantSystem)
		previousReg := digitalEmployeeRegistry{Employees: append([]digitalEmployeeEntry(nil), reg.Employees...)}
		machineIDSource := platformEmployeeMachineIDSource(reg, req.EmployeeID, platformEmployeeID)
		machineID := "ve_" + strings.Trim(machineIDSource, "_")
		veID := "ve_" + strings.TrimPrefix(machineID, "ve_")
		veEntry := digitalEmployeeEntry{ID: veID, MachineID: machineID, EmployeeType: veEmployeeTypeVirtual, PlatformID: entry.PlatformID, PlatformEmployeeID: platformEmployeeID, RuntimeProviderID: runtimeProviderID, OwnerUserID: userID, OwnerEmail: email, Name: name, SkillDescription: strings.TrimSpace(req.SkillDescription), AvatarDataURL: avatarDataURL, AccessPolicy: "public", Status: veStatusActive, OnlineStatus: veOnlineStatusOffline, RegisteredAt: time.Now().UTC().Format(time.RFC3339), UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
		registryIndex := -1
		if i := findPlatformEmployeeRegistrationIndex(reg, veID, machineID, entry.PlatformID, platformEmployeeID); i >= 0 {
			veEntry = mergePlatformEmployeeRegistration(reg.Employees[i], veEntry)
			reg.Employees[i] = veEntry
			registryIndex = i
		} else {
			reg.Employees = append(reg.Employees, veEntry)
			registryIndex = len(reg.Employees) - 1
		}
		normalizeVERegistryResidentFlags(&reg)
		if registryIndex >= 0 && registryIndex < len(reg.Employees) {
			veEntry = reg.Employees[registryIndex]
		}
		if err := saveVERegistry(r.Context(), tenantSystem, reg); err != nil {
			if createdUser {
				if delErr := users.DeleteByTenantEmail(r.Context(), tenantID, email); delErr != nil {
					log.Printf("[platform-employee-register] rollback created user failed tenant=%s email=%s err=%v", tenantID, email, delErr)
				}
			}
			writeError(w, http.StatusInternalServerError, "VE_REGISTRY_SAVE_FAILED", err.Error())
			return
		}
		rollbackRegistration := func() {
			if err := saveVERegistry(r.Context(), tenantSystem, previousReg); err != nil {
				log.Printf("[platform-employee-register] rollback registry failed tenant=%s employee=%s err=%v", tenantID, platformEmployeeID, err)
			}
			if createdUser {
				if err := users.DeleteByTenantEmail(r.Context(), tenantID, email); err != nil {
					log.Printf("[platform-employee-register] rollback created user failed tenant=%s email=%s err=%v", tenantID, email, err)
				}
			}
		}
		var previousRuntimeReg macLawSrvRuntimeRegistry
		runtimeChanged := false
		if runtimeBaseURL != "" {
			previousRuntimeReg = loadMacLawSrvRuntimeRegistry(r.Context(), system)
			if err := upsertMacLawSrvRuntimeRegistry(r.Context(), system, tenantID, runtimeBaseURL, req.RuntimeAPIKey); err != nil {
				rollbackRegistration()
				writeError(w, http.StatusInternalServerError, "MACLAWSRV_RUNTIME_REGISTRY_SAVE_FAILED", err.Error())
				return
			}
			runtimeChanged = true
		}
		if err := ensurePlatformEmployeeLLMEntitlement(r.Context(), tenantSystem, email, firstNonEmpty(req.LLMServiceGroupID, req.DefaultLLM)); err != nil {
			if runtimeChanged {
				if restoreErr := saveMacLawSrvRuntimeRegistry(r.Context(), system, previousRuntimeReg); restoreErr != nil {
					log.Printf("[platform-employee-register] rollback runtime registry failed tenant=%s employee=%s err=%v", tenantID, platformEmployeeID, restoreErr)
				}
			}
			rollbackRegistration()
			writeError(w, http.StatusBadRequest, "LLM_SERVICE_GROUP_ENTITLEMENT_FAILED", err.Error())
			return
		}
		hubAccountID := firstNonEmpty(veEntry.OwnerUserID, userID)
		resp := map[string]any{"ok": true, "employee": veEntry, "hub_employee_id": veEntry.ID, "hub_account_id": hubAccountID, "hub_tenant_id": tenantID, "platform_id": entry.PlatformID}
		if len(identities) > 0 && identities[0] != nil {
			if viewerToken, err := identities[0].IssueViewerTokenForUser(r.Context(), hubAccountID); err == nil && strings.TrimSpace(viewerToken) != "" {
				resp["viewer_token"] = viewerToken
				resp["hub_llm_viewer_token"] = viewerToken
				resp["access_token"] = viewerToken
			}
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func mergePlatformEmployeeRegistration(previous, next digitalEmployeeEntry) digitalEmployeeEntry {
	next.RegisteredAt = firstNonEmpty(previous.RegisteredAt, next.RegisteredAt)
	next.Status = firstNonEmpty(previous.Status, next.Status)
	next.OnlineStatus = firstNonEmpty(previous.OnlineStatus, next.OnlineStatus)
	if next.Status == veStatusDisabled {
		next.DisabledAt = previous.DisabledAt
	}
	if next.Status == veStatusRejected {
		next.RejectReason = previous.RejectReason
		next.RejectedAt = previous.RejectedAt
	}
	if strings.TrimSpace(next.AvatarDataURL) == "" {
		next.AvatarDataURL = previous.AvatarDataURL
	}
	next.AccessPolicy = firstNonEmpty(previous.AccessPolicy, next.AccessPolicy)
	next.Whitelist = normalizeVEStringList(previous.Whitelist)
	next.Blacklist = normalizeVEStringList(previous.Blacklist)
	next.VisibleGroupIDs = normalizeVEStringList(previous.VisibleGroupIDs)
	next.Resident = previous.Resident
	return next
}

func platformEmployeeMachineIDSource(reg digitalEmployeeRegistry, employeeID, platformEmployeeID string) string {
	source := strings.TrimSpace(employeeID)
	if source == "" {
		return strings.TrimSpace(platformEmployeeID)
	}
	machineID := "ve_" + strings.Trim(source, "_")
	veID := "ve_" + strings.TrimPrefix(machineID, "ve_")
	idx := reg.findByIDOrMachineID(veID)
	if idx < 0 {
		idx = reg.findByIDOrMachineID(machineID)
	}
	if idx >= 0 && normalizeVEEmployeeType(reg.Employees[idx].EmployeeType) == veEmployeeTypePhysical && strings.TrimSpace(platformEmployeeID) != "" && !strings.EqualFold(strings.TrimSpace(platformEmployeeID), source) {
		return strings.TrimSpace(platformEmployeeID)
	}
	return source
}

func findPlatformEmployeeRegistrationIndex(reg digitalEmployeeRegistry, veID, machineID, platformID, platformEmployeeID string) int {
	veID = strings.TrimSpace(veID)
	machineID = strings.TrimSpace(machineID)
	platformID = strings.TrimSpace(platformID)
	platformEmployeeID = strings.TrimSpace(platformEmployeeID)
	for i, entry := range reg.Employees {
		if veID != "" && strings.EqualFold(strings.TrimSpace(entry.ID), veID) {
			if platformRegistrationCanUpdateEntry(entry) {
				return i
			}
			continue
		}
		if machineID != "" && strings.EqualFold(strings.TrimSpace(entry.MachineID), machineID) {
			if platformRegistrationCanUpdateEntry(entry) {
				return i
			}
			continue
		}
		if platformEmployeeID == "" || !strings.EqualFold(strings.TrimSpace(entry.PlatformEmployeeID), platformEmployeeID) {
			continue
		}
		if normalizeVEEmployeeType(entry.EmployeeType) == veEmployeeTypePhysical {
			continue
		}
		entryPlatformID := strings.TrimSpace(entry.PlatformID)
		if platformID == "" || entryPlatformID == "" || strings.EqualFold(entryPlatformID, platformID) {
			return i
		}
	}
	return -1
}

func platformRegistrationCanUpdateEntry(entry digitalEmployeeEntry) bool {
	if normalizeVEEmployeeType(entry.EmployeeType) == veEmployeeTypePhysical {
		return false
	}
	return strings.TrimSpace(entry.PlatformEmployeeID) != ""
}

func ensurePlatformEmployeeLLMEntitlement(ctx context.Context, tenantSystem llmservice.SystemSettingsRepository, email, serviceGroupID string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	serviceGroupID = strings.TrimSpace(serviceGroupID)
	if email == "" || serviceGroupID == "" || strings.EqualFold(serviceGroupID, llmservice.DefaultModelServiceGroupID) {
		return nil
	}
	reg, err := llmservice.LoadRegistry(ctx, tenantSystem)
	if err != nil {
		return err
	}
	group, err := validatePlatformEmployeeLLMServiceGroupInRegistry(reg, serviceGroupID)
	if err != nil {
		return err
	}
	upsertPlatformEmployeeUserBinding(reg, email, serviceGroupID)
	if group.AccessPolicy == llmservice.AccessPolicyGrantRequired && !platformEmployeeHasActiveGrant(reg, email, serviceGroupID, time.Now().UTC()) {
		now := time.Now().UTC()
		reg.Grants = append(reg.Grants, llmservice.Grant{
			ID:             llmservice.NewID("grant"),
			Email:          email,
			ServiceGroupID: serviceGroupID,
			Source:         "ve_platform_employee",
			StartsAt:       now,
			ExpiresAt:      now.AddDate(10, 0, 0),
			CreatedAt:      now,
		})
	}
	return llmservice.SaveRegistry(ctx, tenantSystem, reg)
}

func validatePlatformEmployeeLLMServiceGroup(ctx context.Context, tenantSystem llmservice.SystemSettingsRepository, serviceGroupID string) error {
	serviceGroupID = strings.TrimSpace(serviceGroupID)
	if serviceGroupID == "" || strings.EqualFold(serviceGroupID, llmservice.DefaultModelServiceGroupID) {
		return nil
	}
	reg, err := llmservice.LoadRegistry(ctx, tenantSystem)
	if err != nil {
		return err
	}
	_, err = validatePlatformEmployeeLLMServiceGroupInRegistry(reg, serviceGroupID)
	return err
}

func validatePlatformEmployeeLLMServiceGroupInRegistry(reg *llmservice.Registry, serviceGroupID string) (*llmservice.ModelServiceGroup, error) {
	serviceGroupID = strings.TrimSpace(serviceGroupID)
	if serviceGroupID == "" || strings.EqualFold(serviceGroupID, llmservice.DefaultModelServiceGroupID) {
		return nil, nil
	}
	if reg == nil {
		return nil, fmt.Errorf("llm service group %q not found", serviceGroupID)
	}
	group := reg.FindModelServiceGroup(serviceGroupID)
	if group == nil {
		return nil, fmt.Errorf("llm service group %q not found", serviceGroupID)
	}
	return group, nil
}

func upsertPlatformEmployeeUserBinding(reg *llmservice.Registry, email, serviceGroupID string) {
	for i := range reg.UserBindings {
		if !strings.EqualFold(strings.TrimSpace(reg.UserBindings[i].Email), email) {
			continue
		}
		for _, id := range reg.UserBindings[i].ServiceGroupIDs {
			if strings.EqualFold(strings.TrimSpace(id), serviceGroupID) {
				return
			}
		}
		reg.UserBindings[i].ServiceGroupIDs = append(reg.UserBindings[i].ServiceGroupIDs, serviceGroupID)
		return
	}
	reg.UserBindings = append(reg.UserBindings, llmservice.UserBinding{Email: email, ServiceGroupIDs: []string{serviceGroupID}})
}

func platformEmployeeHasActiveGrant(reg *llmservice.Registry, email, serviceGroupID string, now time.Time) bool {
	for _, grant := range reg.Grants {
		if !strings.EqualFold(strings.TrimSpace(grant.Email), email) || !strings.EqualFold(strings.TrimSpace(grant.ServiceGroupID), serviceGroupID) {
			continue
		}
		if !now.Before(grant.StartsAt) && now.Before(grant.ExpiresAt) {
			return true
		}
	}
	return false
}

func PlatformEmployeeViewerTokenHandler(system store.SystemSettingsRepository, tenants store.TenantRepository, users store.UserRepository, identities ...*auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entry, body, ok := authenticatePlatformRequest(w, r, system)
		if !ok {
			return
		}
		if users == nil {
			writeError(w, http.StatusServiceUnavailable, "USER_REPOSITORY_UNAVAILABLE", "user repository is unavailable")
			return
		}
		if len(identities) == 0 || identities[0] == nil {
			writeError(w, http.StatusServiceUnavailable, "IDENTITY_SERVICE_UNAVAILABLE", "identity service is unavailable")
			return
		}
		var req struct {
			EmployeeID         string `json:"employee_id"`
			PlatformEmployeeID string `json:"platform_employee_id"`
			HubTenantID        string `json:"hub_tenant_id"`
			TenantID           string `json:"tenant_id"`
			HubEmployeeID      string `json:"hub_employee_id"`
			HubAccountID       string `json:"hub_account_id"`
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
				return
			}
		}
		platformEmployeeID := firstNonEmpty(strings.TrimSpace(req.PlatformEmployeeID), strings.TrimSpace(req.EmployeeID), strings.TrimSpace(r.PathValue("id")), strings.TrimSpace(req.HubEmployeeID))
		if platformEmployeeID == "" {
			writeError(w, http.StatusBadRequest, "EMPLOYEE_ID_REQUIRED", "employee_id is required")
			return
		}
		tenantID := firstNonEmpty(strings.TrimSpace(req.HubTenantID), strings.TrimSpace(req.TenantID))
		if tenantID == "" {
			writeError(w, http.StatusBadRequest, "TENANT_ID_REQUIRED", "hub_tenant_id is required")
			return
		}
		if !platformTenantIDAllowed(r.Context(), tenants, tenantID) {
			writeError(w, http.StatusNotFound, "TENANT_NOT_FOUND", "Hub tenant not found")
			return
		}
		employeeEntry, found := platformEmployeeInTenant(r.Context(), system, tenantID, entry.PlatformID, platformEmployeeID)
		if !found {
			writeError(w, http.StatusNotFound, "EMPLOYEE_NOT_FOUND", "platform employee was not found in Hub registry")
			return
		}
		if strings.TrimSpace(req.HubEmployeeID) != "" && strings.TrimSpace(req.HubEmployeeID) != firstNonEmpty(employeeEntry.ID, employeeEntry.MachineID) {
			writeError(w, http.StatusForbidden, "EMPLOYEE_IDENTITY_MISMATCH", "hub_employee_id does not match the registered platform employee")
			return
		}
		if strings.TrimSpace(req.HubAccountID) != "" && strings.TrimSpace(employeeEntry.OwnerUserID) != "" && strings.TrimSpace(req.HubAccountID) != strings.TrimSpace(employeeEntry.OwnerUserID) {
			writeError(w, http.StatusForbidden, "EMPLOYEE_IDENTITY_MISMATCH", "hub_account_id does not match the registered platform employee")
			return
		}
		if strings.TrimSpace(employeeEntry.OwnerUserID) == "" {
			writeError(w, http.StatusConflict, "EMPLOYEE_ACCOUNT_NOT_BOUND", "platform employee is not bound to a Hub account")
			return
		}
		user, err := users.GetByID(r.Context(), strings.TrimSpace(employeeEntry.OwnerUserID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "USER_LOOKUP_FAILED", err.Error())
			return
		}
		if user == nil || strings.TrimSpace(user.TenantID) != tenantID || !platformHubUserActive(user) {
			writeError(w, http.StatusNotFound, "EMPLOYEE_ACCOUNT_NOT_FOUND", "platform employee Hub account was not found")
			return
		}
		viewerToken, err := identities[0].IssueViewerTokenForUser(r.Context(), user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "VIEWER_TOKEN_ISSUE_FAILED", err.Error())
			return
		}
		if strings.TrimSpace(viewerToken) == "" {
			writeError(w, http.StatusInternalServerError, "VIEWER_TOKEN_EMPTY", "Hub did not issue a viewer token")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "employee_id": platformEmployeeID, "hub_employee_id": firstNonEmpty(employeeEntry.ID, employeeEntry.MachineID), "hub_account_id": user.ID, "hub_tenant_id": tenantID, "viewer_token": viewerToken, "hub_llm_viewer_token": viewerToken, "access_token": viewerToken})
	}
}

func PlatformEmployeeStatusHandler(system store.SystemSettingsRepository, tenants store.TenantRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entry, body, ok := authenticatePlatformRequest(w, r, system)
		if !ok {
			return
		}
		var req struct {
			EmployeeID         string `json:"employee_id"`
			PlatformEmployeeID string `json:"platform_employee_id"`
			VirtualEmail       string `json:"virtual_email"`
			ServiceStatus      string `json:"service_status"`
			HubTenantID        string `json:"hub_tenant_id"`
			HubEmployeeID      string `json:"hub_employee_id"`
			HubAccountID       string `json:"hub_account_id"`
			PlatformID         string `json:"platform_id"`
			DeleteVERegistry   bool   `json:"delete_ve_registry"`
			VERegistryStatus   string `json:"ve_registry_status"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		platformEmployeeID := strings.TrimSpace(req.PlatformEmployeeID)
		if platformEmployeeID == "" {
			platformEmployeeID = strings.TrimSpace(req.EmployeeID)
		}
		if platformEmployeeID == "" {
			platformEmployeeID = strings.TrimSpace(req.HubEmployeeID)
		}
		if platformEmployeeID == "" {
			writeError(w, http.StatusBadRequest, "EMPLOYEE_ID_REQUIRED", "employee_id is required")
			return
		}
		status := normalizePlatformEmployeeStatus(req.ServiceStatus)
		deleteRegistry := platformEmployeeStatusDeletesVERegistry(req.DeleteVERegistry, req.VERegistryStatus)
		tenantID := strings.TrimSpace(req.HubTenantID)
		var updated bool
		var err error
		if tenantID != "" {
			if !platformTenantIDAllowed(r.Context(), tenants, tenantID) {
				writeError(w, http.StatusNotFound, "TENANT_NOT_FOUND", "Hub tenant not found")
				return
			}
			employeeEntry, found := platformEmployeeInTenant(r.Context(), system, tenantID, entry.PlatformID, platformEmployeeID)
			if deleteRegistry {
				if matchedEntry, matched := platformEmployeeInTenantForDelete(r.Context(), system, tenantID, entry.PlatformID, platformEmployeeID, req.HubEmployeeID, req.HubAccountID); matched {
					employeeEntry = matchedEntry
					found = true
				}
			}
			if !found {
				writeError(w, http.StatusNotFound, "EMPLOYEE_NOT_FOUND", "platform employee was not found in Hub registry")
				return
			}
			if strings.TrimSpace(req.HubEmployeeID) != "" && strings.TrimSpace(req.HubEmployeeID) != firstNonEmpty(employeeEntry.ID, employeeEntry.MachineID) {
				writeError(w, http.StatusForbidden, "EMPLOYEE_IDENTITY_MISMATCH", "hub_employee_id does not match the registered platform employee")
				return
			}
			if strings.TrimSpace(req.HubAccountID) != "" && strings.TrimSpace(employeeEntry.OwnerUserID) != "" && strings.TrimSpace(req.HubAccountID) != strings.TrimSpace(employeeEntry.OwnerUserID) {
				writeError(w, http.StatusForbidden, "EMPLOYEE_IDENTITY_MISMATCH", "hub_account_id does not match the registered platform employee")
				return
			}
			if deleteRegistry {
				updated, err = deletePlatformEmployeeInTenant(r.Context(), system, tenantID, entry.PlatformID, platformEmployeeID, req.HubEmployeeID, req.HubAccountID)
			} else {
				updated, err = updatePlatformEmployeeStatusInTenant(r.Context(), system, tenantID, entry.PlatformID, platformEmployeeID, status)
			}
		} else {
			if deleteRegistry {
				tenantID, updated, err = deletePlatformEmployee(r.Context(), system, tenants, entry.PlatformID, platformEmployeeID, req.HubEmployeeID, req.HubAccountID)
			} else {
				tenantID, updated, err = updatePlatformEmployeeStatus(r.Context(), system, tenants, entry.PlatformID, platformEmployeeID, status)
			}
		}
		if err != nil {
			var runtimeActivationErr platformEmployeeRuntimeActivationError
			if errors.As(err, &runtimeActivationErr) {
				writeError(w, http.StatusConflict, runtimeActivationErr.code, runtimeActivationErr.message)
				return
			}
			writeError(w, http.StatusInternalServerError, "EMPLOYEE_STATUS_UPDATE_FAILED", err.Error())
			return
		}
		if !updated {
			writeError(w, http.StatusNotFound, "EMPLOYEE_NOT_FOUND", "platform employee was not found in Hub registry")
			return
		}
		if deleteRegistry {
			status = "deleted"
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "employee_id": platformEmployeeID, "hub_tenant_id": tenantID, "service_status": req.ServiceStatus, "hub_status": status})
	}
}

func platformEmployeeStatusDeletesVERegistry(deleteVERegistry bool, veRegistryStatus string) bool {
	return deleteVERegistry || strings.EqualFold(strings.TrimSpace(veRegistryStatus), "deleted") || strings.EqualFold(strings.TrimSpace(veRegistryStatus), "removed")
}

func PlatformEmployeeDeleteHandler(system store.SystemSettingsRepository, tenants store.TenantRepository, users store.UserRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entry, body, ok := authenticatePlatformRequest(w, r, system)
		if !ok {
			return
		}
		var req struct {
			EmployeeID         string `json:"employee_id"`
			PlatformEmployeeID string `json:"platform_employee_id"`
			VirtualEmail       string `json:"virtual_email"`
			HubTenantID        string `json:"hub_tenant_id"`
			HubEmployeeID      string `json:"hub_employee_id"`
			HubAccountID       string `json:"hub_account_id"`
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
				return
			}
		}
		platformEmployeeID := firstNonEmpty(strings.TrimSpace(req.PlatformEmployeeID), strings.TrimSpace(req.EmployeeID), strings.TrimSpace(r.PathValue("id")), strings.TrimSpace(req.HubEmployeeID))
		if platformEmployeeID == "" {
			writeError(w, http.StatusBadRequest, "EMPLOYEE_ID_REQUIRED", "employee_id is required")
			return
		}
		tenantID := strings.TrimSpace(req.HubTenantID)
		var employeeEntry digitalEmployeeEntry
		var found bool
		if tenantID != "" {
			if !platformTenantIDAllowed(r.Context(), tenants, tenantID) {
				writeError(w, http.StatusNotFound, "TENANT_NOT_FOUND", "Hub tenant not found")
				return
			}
			employeeEntry, found = platformEmployeeInTenant(r.Context(), system, tenantID, entry.PlatformID, platformEmployeeID)
			if matchedEntry, matched := platformEmployeeInTenantForDelete(r.Context(), system, tenantID, entry.PlatformID, platformEmployeeID, req.HubEmployeeID, req.HubAccountID); matched {
				employeeEntry = matchedEntry
				found = true
			}
			if !found {
				writeError(w, http.StatusNotFound, "EMPLOYEE_NOT_FOUND", "platform employee was not found in Hub registry")
				return
			}
		} else {
			var lookupErr error
			tenantID, employeeEntry, found, lookupErr = platformEmployeeForProvider(r.Context(), system, tenants, entry.PlatformID, platformEmployeeID, req.HubEmployeeID, req.HubAccountID)
			if lookupErr != nil {
				writeError(w, http.StatusInternalServerError, "EMPLOYEE_LOOKUP_FAILED", lookupErr.Error())
				return
			}
			if !found {
				writeError(w, http.StatusNotFound, "EMPLOYEE_NOT_FOUND", "platform employee was not found in Hub registry")
				return
			}
		}
		if strings.TrimSpace(req.HubEmployeeID) != "" && strings.TrimSpace(req.HubEmployeeID) != firstNonEmpty(employeeEntry.ID, employeeEntry.MachineID) {
			writeError(w, http.StatusForbidden, "EMPLOYEE_IDENTITY_MISMATCH", "hub_employee_id does not match the registered platform employee")
			return
		}
		if strings.TrimSpace(req.HubAccountID) != "" && strings.TrimSpace(employeeEntry.OwnerUserID) != "" && strings.TrimSpace(req.HubAccountID) != strings.TrimSpace(employeeEntry.OwnerUserID) {
			writeError(w, http.StatusForbidden, "EMPLOYEE_IDENTITY_MISMATCH", "hub_account_id does not match the registered platform employee")
			return
		}
		if users == nil {
			writeError(w, http.StatusServiceUnavailable, "USER_REPOSITORY_UNAVAILABLE", "user repository is unavailable")
			return
		}
		email, err := platformEmployeeDeleteEmail(r.Context(), users, tenantID, employeeEntry, req.VirtualEmail)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "USER_LOOKUP_FAILED", err.Error())
			return
		}
		if email == "" && strings.TrimSpace(req.VirtualEmail) != "" {
			writeError(w, http.StatusForbidden, "EMPLOYEE_IDENTITY_MISMATCH", "virtual_email does not match the registered platform employee")
			return
		}
		userDeleted := false
		if email != "" {
			if err := users.DeleteByTenantEmail(r.Context(), tenantID, email); err != nil {
				writeError(w, http.StatusInternalServerError, "USER_DELETE_FAILED", err.Error())
				return
			}
			userDeleted = true
		}
		removed, err := deletePlatformEmployeeInTenant(r.Context(), system, tenantID, entry.PlatformID, platformEmployeeID, req.HubEmployeeID, req.HubAccountID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "EMPLOYEE_DELETE_FAILED", err.Error())
			return
		}
		if !removed {
			writeError(w, http.StatusNotFound, "EMPLOYEE_NOT_FOUND", "platform employee was not found in Hub registry")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "employee_id": platformEmployeeID, "hub_tenant_id": tenantID, "hub_employee_id": firstNonEmpty(employeeEntry.ID, employeeEntry.MachineID), "hub_account_id": employeeEntry.OwnerUserID, "user_deleted": userDeleted, "ve_registry_deleted": true})
	}
}

func platformEmployeeDeleteEmail(ctx context.Context, users store.UserRepository, tenantID string, employeeEntry digitalEmployeeEntry, requestEmail string) (string, error) {
	registryEmail := strings.ToLower(strings.TrimSpace(employeeEntry.OwnerEmail))
	requestEmail = strings.ToLower(strings.TrimSpace(requestEmail))
	if registryEmail != "" {
		if requestEmail != "" && requestEmail != registryEmail {
			return "", nil
		}
		return registryEmail, nil
	}
	ownerUserID := strings.TrimSpace(employeeEntry.OwnerUserID)
	if ownerUserID == "" {
		if requestEmail == "" {
			return "", nil
		}
		user, err := users.GetByTenantEmail(ctx, tenantID, requestEmail)
		if err != nil {
			return "", err
		}
		if user == nil || strings.TrimSpace(user.TenantID) != strings.TrimSpace(tenantID) || !strings.EqualFold(strings.TrimSpace(user.Email), requestEmail) {
			return "", nil
		}
		return requestEmail, nil
	}
	user, err := users.GetByID(ctx, ownerUserID)
	if err != nil {
		return "", err
	}
	if user == nil || strings.TrimSpace(user.TenantID) != strings.TrimSpace(tenantID) {
		return "", nil
	}
	lookupEmail := strings.ToLower(strings.TrimSpace(user.Email))
	if requestEmail != "" && lookupEmail != "" && requestEmail != lookupEmail {
		return "", nil
	}
	return lookupEmail, nil
}

func firstTenantRepo(tenants ...store.TenantRepository) store.TenantRepository {
	if len(tenants) == 0 {
		return nil
	}
	return tenants[0]
}

func sanitizePlatformTenantDomains(ctx context.Context, tenants store.TenantRepository, domains []platformTenantDomain) []platformTenantDomain {
	if tenants == nil {
		return domains
	}
	out := make([]platformTenantDomain, 0, len(domains))
	for _, domain := range domains {
		hubTenantID := strings.TrimSpace(domain.HubTenantID)
		if hubTenantID == "" {
			hubTenantID = strings.TrimSpace(domain.TenantID)
		}
		if hubTenantID == "" || platformTenantIDAllowed(ctx, tenants, hubTenantID) {
			out = append(out, domain)
		}
	}
	return out
}

func isPlatformTenantActive(t *store.Tenant) bool {
	return isPlatformTenantVisible(t) && strings.EqualFold(strings.TrimSpace(t.Status), "active")
}

func isPlatformTenantVisible(t *store.Tenant) bool {
	return t != nil && t.DeletedAt == nil && !isReservedTenantID(t.ID) && strings.TrimSpace(t.ID) != ""
}

func platformTenantIDAllowed(ctx context.Context, tenants store.TenantRepository, tenantID string) bool {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return false
	}
	if tenants == nil {
		return true
	}
	tenant, err := tenants.GetByID(ctx, tenantID)
	return err == nil && isPlatformTenantActive(tenant)
}

func normalizePlatformEmployeeStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "suspended", "disabled", "inactive", "stopped", "deleted", "removed":
		return veStatusDisabled
	case "published", "active", "ready", "enabled":
		return veStatusActive
	case "rejected":
		return veStatusRejected
	case "pending":
		return veStatusPending
	default:
		return veStatusActive
	}
}

type platformEmployeeRuntimeActivationError struct {
	code    string
	message string
}

func (e platformEmployeeRuntimeActivationError) Error() string {
	return e.message
}

func platformTenantIDsForProviderUpdate(ctx context.Context, system store.SystemSettingsRepository, tenants store.TenantRepository) (map[string]struct{}, error) {
	tenantIDs := map[string]struct{}{}
	if tenants != nil {
		items, err := tenants.List(ctx)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if isPlatformTenantActive(item) {
				tenantIDs[strings.TrimSpace(item.ID)] = struct{}{}
			}
		}
	}
	providers := loadPlatformProviderRegistry(ctx, system)
	for _, provider := range providers.Providers {
		for _, domain := range provider.TenantDomains {
			for _, id := range []string{domain.HubTenantID, domain.TenantID} {
				if strings.TrimSpace(id) != "" {
					trimmedID := strings.TrimSpace(id)
					if platformTenantIDAllowed(ctx, tenants, trimmedID) {
						tenantIDs[trimmedID] = struct{}{}
					}
				}
			}
		}
	}
	return tenantIDs, nil
}

func updatePlatformEmployeeStatus(ctx context.Context, system store.SystemSettingsRepository, tenants store.TenantRepository, platformID, platformEmployeeID, status string) (string, bool, error) {
	platformID = strings.TrimSpace(platformID)
	platformEmployeeID = strings.TrimSpace(platformEmployeeID)
	if platformEmployeeID == "" {
		return "", false, nil
	}
	tenantIDs, err := platformTenantIDsForProviderUpdate(ctx, system, tenants)
	if err != nil {
		return "", false, err
	}
	for tenantID := range tenantIDs {
		tenantSystem := scopedSystemSettingsForTenant(tenantID, system)
		registry := loadVERegistry(ctx, tenantSystem)
		changed := false
		for i := range registry.Employees {
			emp := &registry.Employees[i]
			if platformID != "" && !strings.EqualFold(strings.TrimSpace(emp.PlatformID), platformID) {
				continue
			}
			if !platformEmployeeIDMatchesEntry(*emp, platformEmployeeID) {
				continue
			}
			if status == veStatusActive {
				checked, ready, code, message := verifyMacLawSrvRuntimeReadyForActivation(ctx, system, tenantID, *emp)
				if !ready {
					registry.Employees[i] = checked
					if err := saveVERegistry(ctx, tenantSystem, registry); err != nil {
						return tenantID, false, err
					}
					return tenantID, false, platformEmployeeRuntimeActivationError{code: code, message: message}
				}
				*emp = checked
			}
			emp.Status = status
			if status == veStatusDisabled {
				now := time.Now().UTC().Format(time.RFC3339)
				emp.DisabledAt = now
				emp.OnlineStatus = veOnlineStatusOffline
			} else if status == veStatusActive {
				emp.DisabledAt = ""
				if isMacLawSrvRuntimeEmployee(*emp) {
					if emp.OnlineStatus != veOnlineStatusOnline {
						emp.OnlineStatus = veOnlineStatusOffline
					}
				} else {
					emp.OnlineStatus = veOnlineStatusOnline
				}
			}
			emp.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			changed = true
			break
		}
		if changed {
			if err := saveVERegistry(ctx, tenantSystem, registry); err != nil {
				return tenantID, false, err
			}
			return tenantID, true, nil
		}
	}
	return "", false, nil
}

func deletePlatformEmployee(ctx context.Context, system store.SystemSettingsRepository, tenants store.TenantRepository, platformID, platformEmployeeID, hubEmployeeID, hubAccountID string) (string, bool, error) {
	platformEmployeeID = strings.TrimSpace(platformEmployeeID)
	if platformEmployeeID == "" {
		return "", false, nil
	}
	tenantIDs, err := platformTenantIDsForProviderUpdate(ctx, system, tenants)
	if err != nil {
		return "", false, err
	}
	for tenantID := range tenantIDs {
		if _, found := platformEmployeeInTenantForDelete(ctx, system, tenantID, platformID, platformEmployeeID, hubEmployeeID, hubAccountID); !found {
			continue
		}
		removed, err := deletePlatformEmployeeInTenant(ctx, system, tenantID, platformID, platformEmployeeID, hubEmployeeID, hubAccountID)
		if err != nil {
			return tenantID, false, err
		}
		if removed {
			return tenantID, true, nil
		}
	}
	return "", false, nil
}

func platformEmployeeForProvider(ctx context.Context, system store.SystemSettingsRepository, tenants store.TenantRepository, platformID, platformEmployeeID, hubEmployeeID, hubAccountID string) (string, digitalEmployeeEntry, bool, error) {
	platformEmployeeID = strings.TrimSpace(platformEmployeeID)
	if platformEmployeeID == "" {
		return "", digitalEmployeeEntry{}, false, nil
	}
	tenantIDs, err := platformTenantIDsForProviderUpdate(ctx, system, tenants)
	if err != nil {
		return "", digitalEmployeeEntry{}, false, err
	}
	for tenantID := range tenantIDs {
		employeeEntry, found := platformEmployeeInTenantForDelete(ctx, system, tenantID, platformID, platformEmployeeID, hubEmployeeID, hubAccountID)
		if found {
			return tenantID, employeeEntry, true, nil
		}
	}
	return "", digitalEmployeeEntry{}, false, nil
}

func platformEmployeeInTenantForDelete(ctx context.Context, system store.SystemSettingsRepository, tenantID, platformID, platformEmployeeID, hubEmployeeID, hubAccountID string) (digitalEmployeeEntry, bool) {
	platformID = strings.TrimSpace(platformID)
	platformEmployeeID = strings.TrimSpace(platformEmployeeID)
	if tenantID == "" || platformEmployeeID == "" {
		return digitalEmployeeEntry{}, false
	}
	registry := loadVERegistry(ctx, scopedSystemSettingsForTenant(tenantID, system))
	var best digitalEmployeeEntry
	bestScore := -1
	for _, employee := range registry.Employees {
		if platformID != "" && !strings.EqualFold(strings.TrimSpace(employee.PlatformID), platformID) {
			continue
		}
		if !platformEmployeeIDMatchesEntry(employee, platformEmployeeID) {
			continue
		}
		score := platformEmployeeDeleteLookupScore(employee, hubEmployeeID, hubAccountID)
		if score > bestScore {
			best = employee
			bestScore = score
		}
	}
	return best, bestScore >= 0
}

func platformEmployeeDeleteLookupMatches(entry digitalEmployeeEntry, hubEmployeeID, hubAccountID string) bool {
	return platformEmployeeDeleteLookupScore(entry, hubEmployeeID, hubAccountID) >= 0
}

func platformEmployeeDeleteLookupScore(entry digitalEmployeeEntry, hubEmployeeID, hubAccountID string) int {
	hubEmployeeID = strings.TrimSpace(hubEmployeeID)
	hubAccountID = strings.TrimSpace(hubAccountID)
	entryHubEmployeeID := firstNonEmpty(entry.ID, entry.MachineID)
	entryHubAccountID := strings.TrimSpace(entry.OwnerUserID)
	score := 0
	if hubEmployeeID != "" && hubEmployeeID != entryHubEmployeeID {
		return -1
	} else if hubEmployeeID != "" {
		score += 2
	}
	if hubAccountID != "" && entryHubAccountID != "" && hubAccountID != entryHubAccountID {
		return -1
	} else if hubAccountID != "" && entryHubAccountID != "" {
		score++
	}
	return score
}

func updatePlatformEmployeeStatusInTenant(ctx context.Context, system store.SystemSettingsRepository, tenantID, platformID, platformEmployeeID, status string) (bool, error) {
	tenantID = strings.TrimSpace(tenantID)
	platformID = strings.TrimSpace(platformID)
	platformEmployeeID = strings.TrimSpace(platformEmployeeID)
	if tenantID == "" || platformEmployeeID == "" {
		return false, nil
	}
	tenantSystem := scopedSystemSettingsForTenant(tenantID, system)
	registry := loadVERegistry(ctx, tenantSystem)
	for i := range registry.Employees {
		emp := &registry.Employees[i]
		if platformID != "" && !strings.EqualFold(strings.TrimSpace(emp.PlatformID), platformID) {
			continue
		}
		if !platformEmployeeIDMatchesEntry(*emp, platformEmployeeID) {
			continue
		}
		if status == veStatusActive {
			checked, ready, code, message := verifyMacLawSrvRuntimeReadyForActivation(ctx, system, tenantID, *emp)
			if !ready {
				registry.Employees[i] = checked
				if err := saveVERegistry(ctx, tenantSystem, registry); err != nil {
					return false, err
				}
				return false, platformEmployeeRuntimeActivationError{code: code, message: message}
			}
			*emp = checked
		}
		emp.Status = status
		if status == veStatusDisabled {
			now := time.Now().UTC().Format(time.RFC3339)
			emp.DisabledAt = now
			emp.OnlineStatus = veOnlineStatusOffline
		} else if status == veStatusActive {
			emp.DisabledAt = ""
			if isMacLawSrvRuntimeEmployee(*emp) {
				if emp.OnlineStatus != veOnlineStatusOnline {
					emp.OnlineStatus = veOnlineStatusOffline
				}
			} else {
				emp.OnlineStatus = veOnlineStatusOnline
			}
		}
		emp.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := saveVERegistry(ctx, tenantSystem, registry); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func deletePlatformEmployeeInTenant(ctx context.Context, system store.SystemSettingsRepository, tenantID, platformID, platformEmployeeID, hubEmployeeID, hubAccountID string) (bool, error) {
	tenantID = strings.TrimSpace(tenantID)
	platformID = strings.TrimSpace(platformID)
	platformEmployeeID = strings.TrimSpace(platformEmployeeID)
	if tenantID == "" || platformEmployeeID == "" {
		return false, nil
	}
	tenantSystem := scopedSystemSettingsForTenant(tenantID, system)
	registry := loadVERegistry(ctx, tenantSystem)
	bestIndex := -1
	bestScore := -1
	for i := range registry.Employees {
		emp := registry.Employees[i]
		if platformID != "" && !strings.EqualFold(strings.TrimSpace(emp.PlatformID), platformID) {
			continue
		}
		if !platformEmployeeIDMatchesEntry(emp, platformEmployeeID) {
			continue
		}
		score := platformEmployeeDeleteLookupScore(emp, hubEmployeeID, hubAccountID)
		if score > bestScore {
			bestIndex = i
			bestScore = score
		}
	}
	if bestIndex < 0 {
		return false, nil
	}
	registry.Employees = append(registry.Employees[:bestIndex], registry.Employees[bestIndex+1:]...)
	normalizeVERegistryResidentFlags(&registry)
	if err := saveVERegistry(ctx, tenantSystem, registry); err != nil {
		return false, err
	}
	return true, nil
}

func authenticatePlatformRequest(w http.ResponseWriter, r *http.Request, system store.SystemSettingsRepository) (platformProviderEntry, []byte, bool) {
	body, platformID, timestamp, nonce, err := readSignedPlatformBody(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "PLATFORM_SIGNATURE_REQUIRED", err.Error())
		return platformProviderEntry{}, nil, false
	}
	reg := loadPlatformProviderRegistry(r.Context(), system)
	idx := reg.find(platformID)
	if idx < 0 {
		writeError(w, http.StatusUnauthorized, "PLATFORM_NOT_REGISTERED", "platform provider is not registered")
		return platformProviderEntry{}, nil, false
	}
	entry := reg.Providers[idx]
	if entry.RegistrationStatus != "active" {
		writeError(w, http.StatusForbidden, "PLATFORM_INACTIVE", "platform provider is not active")
		return platformProviderEntry{}, nil, false
	}
	if err := verifyPlatformSignature(platformSignaturePayload(r.Method, r.URL.RequestURI(), timestamp, nonce, body), r.Header.Get("X-VE-Signature"), entry.PublicKeyPEM); err != nil {
		writeError(w, http.StatusUnauthorized, "PLATFORM_SIGNATURE_INVALID", err.Error())
		return platformProviderEntry{}, nil, false
	}
	stored, err := recordPlatformRequestNonce(r.Context(), system, platformID, nonce, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "PLATFORM_NONCE_STORE_FAILED", err.Error())
		return platformProviderEntry{}, nil, false
	}
	if !stored {
		writeError(w, http.StatusConflict, "PLATFORM_REPLAY_DETECTED", "platform request nonce has already been used")
		return platformProviderEntry{}, nil, false
	}
	return entry, body, true
}

func readSignedPlatformBody(r *http.Request) ([]byte, string, string, string, error) {
	platformID := strings.TrimSpace(r.Header.Get("X-VE-Platform-ID"))
	if platformID == "" {
		return nil, "", "", "", errors.New("X-VE-Platform-ID is required")
	}
	if strings.TrimSpace(r.Header.Get("X-VE-Signature")) == "" {
		return nil, "", "", "", errors.New("X-VE-Signature is required")
	}
	timestamp := strings.TrimSpace(r.Header.Get("X-VE-Timestamp"))
	nonce := strings.TrimSpace(r.Header.Get("X-VE-Nonce"))
	if timestamp == "" || nonce == "" {
		return nil, "", "", "", errors.New("X-VE-Timestamp and X-VE-Nonce are required")
	}
	requestTime, err := parsePlatformRequestTimestamp(timestamp)
	if err != nil {
		return nil, "", "", "", errors.New("X-VE-Timestamp must be RFC3339 or Unix seconds")
	}
	now := time.Now().UTC()
	if requestTime.Before(now.Add(-platformRequestReplayWindow)) || requestTime.After(now.Add(platformRequestReplayWindow)) {
		return nil, "", "", "", errors.New("X-VE-Timestamp is outside the allowed replay window")
	}
	if len(nonce) > 128 {
		return nil, "", "", "", errors.New("X-VE-Nonce is too long")
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, platformSignedBodyMaxBytes+1))
	if err != nil {
		return nil, "", "", "", err
	}
	if int64(len(body)) > platformSignedBodyMaxBytes {
		return nil, "", "", "", errors.New("request body is too large")
	}
	return body, platformID, timestamp, nonce, nil
}

func platformSignaturePayload(method, path, timestamp, nonce string, body []byte) []byte {
	bodyDigest := sha256.Sum256(body)
	canonical := strings.Join([]string{strings.ToUpper(strings.TrimSpace(method)), strings.TrimSpace(path), strings.TrimSpace(timestamp), strings.TrimSpace(nonce), hex.EncodeToString(bodyDigest[:])}, "\n")
	return []byte(canonical)
}

func verifyPlatformSignature(body []byte, encodedSig, publicPEM string) error {
	block, _ := pem.Decode([]byte(strings.TrimSpace(publicPEM)))
	if block == nil {
		return errors.New("public key is invalid")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return err
	}
	pub, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return errors.New("public key is not RSA")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encodedSig))
	if err != nil {
		return err
	}
	digest := sha256.Sum256(body)
	return rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sig)
}

func parsePlatformRequestTimestamp(value string) (time.Time, error) {
	if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return ts.UTC(), nil
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(seconds, 0).UTC(), nil
}

func loadPlatformRequestNonceRegistry(ctx context.Context, system store.SystemSettingsRepository) platformRequestNonceRegistry {
	if system == nil {
		return platformRequestNonceRegistry{Nonces: map[string]string{}}
	}
	raw, err := system.Get(ctx, platformRequestNonceRegistryKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return platformRequestNonceRegistry{Nonces: map[string]string{}}
	}
	var reg platformRequestNonceRegistry
	if err := json.Unmarshal([]byte(raw), &reg); err != nil || reg.Nonces == nil {
		return platformRequestNonceRegistry{Nonces: map[string]string{}}
	}
	return reg
}

func recordPlatformRequestNonce(ctx context.Context, system store.SystemSettingsRepository, platformID, nonce string, now time.Time) (bool, error) {
	if system == nil {
		return true, nil
	}
	reg := loadPlatformRequestNonceRegistry(ctx, system)
	cutoff := now.Add(-platformRequestReplayWindow)
	for key, value := range reg.Nonces {
		ts, err := time.Parse(time.RFC3339Nano, value)
		if err != nil || ts.Before(cutoff) {
			delete(reg.Nonces, key)
		}
	}
	key := strings.TrimSpace(platformID) + "\x1f" + strings.TrimSpace(nonce)
	if _, exists := reg.Nonces[key]; exists {
		return false, nil
	}
	reg.Nonces[key] = now.UTC().Format(time.RFC3339Nano)
	data, err := json.Marshal(reg)
	if err != nil {
		return false, err
	}
	return true, system.Set(ctx, platformRequestNonceRegistryKey, string(data))
}

func loadPlatformProviderRegistry(ctx context.Context, system store.SystemSettingsRepository) platformProviderRegistry {
	if system == nil {
		return platformProviderRegistry{Providers: []platformProviderEntry{}}
	}
	raw, err := system.Get(ctx, platformProviderRegistryKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return platformProviderRegistry{Providers: []platformProviderEntry{}}
	}
	var reg platformProviderRegistry
	if err := json.Unmarshal([]byte(raw), &reg); err != nil {
		return platformProviderRegistry{Providers: []platformProviderEntry{}}
	}
	if reg.Providers == nil {
		reg.Providers = []platformProviderEntry{}
	}
	return reg
}

func savePlatformProviderRegistry(ctx context.Context, system store.SystemSettingsRepository, reg platformProviderRegistry) error {
	if system == nil {
		return nil
	}
	sort.SliceStable(reg.Providers, func(i, j int) bool { return reg.Providers[i].PlatformID < reg.Providers[j].PlatformID })
	data, err := json.Marshal(reg)
	if err != nil {
		return err
	}
	return system.Set(ctx, platformProviderRegistryKey, string(data))
}

func loadMacLawSrvRuntimeRegistry(ctx context.Context, system store.SystemSettingsRepository) macLawSrvRuntimeRegistry {
	if system == nil {
		return macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{}}
	}
	raw, err := system.Get(ctx, macLawSrvRuntimeRegistryKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{}}
	}
	var reg macLawSrvRuntimeRegistry
	if err := json.Unmarshal([]byte(raw), &reg); err != nil {
		return macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{}}
	}
	if reg.Runtimes == nil {
		reg.Runtimes = []macLawSrvRuntimeEntry{}
	}
	return reg
}

func saveMacLawSrvRuntimeRegistry(ctx context.Context, system store.SystemSettingsRepository, reg macLawSrvRuntimeRegistry) error {
	if system == nil {
		return nil
	}
	sort.SliceStable(reg.Runtimes, func(i, j int) bool { return reg.Runtimes[i].RuntimeID < reg.Runtimes[j].RuntimeID })
	data, err := json.Marshal(reg)
	if err != nil {
		return err
	}
	return system.Set(ctx, macLawSrvRuntimeRegistryKey, string(data))
}

func (r macLawSrvRuntimeRegistry) findForTenant(tenantID string) (macLawSrvRuntimeEntry, bool) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return macLawSrvRuntimeEntry{}, false
	}
	var fallback macLawSrvRuntimeEntry
	var hasFallback bool
	for _, runtime := range r.Runtimes {
		if strings.TrimSpace(runtime.BaseURL) == "" {
			continue
		}
		hasTenantScope := false
		for _, id := range runtime.TenantIDs {
			if strings.TrimSpace(id) == "" {
				continue
			}
			hasTenantScope = true
			if strings.EqualFold(strings.TrimSpace(id), tenantID) {
				return runtime, true
			}
		}
		if !hasTenantScope && (strings.TrimSpace(runtime.RuntimeID) == "" || strings.EqualFold(strings.TrimSpace(runtime.RuntimeID), maclawSrvRuntimePlatformID)) {
			fallback = runtime
			hasFallback = true
		}
	}
	return fallback, hasFallback
}

func upsertMacLawSrvRuntimeRegistry(ctx context.Context, system store.SystemSettingsRepository, tenantID, baseURL, adminSecret string) error {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	reg := loadMacLawSrvRuntimeRegistry(ctx, system)
	idx := -1
	for i, runtime := range reg.Runtimes {
		if strings.EqualFold(strings.TrimRight(strings.TrimSpace(runtime.BaseURL), "/"), baseURL) {
			idx = i
			break
		}
	}
	entry := macLawSrvRuntimeEntry{RuntimeID: macLawSrvRuntimeIDForBaseURL(reg, baseURL), BaseURL: baseURL, AdminSecret: strings.TrimSpace(adminSecret), TenantIDs: []string{}, CreatedAt: now, UpdatedAt: now}
	if idx >= 0 {
		entry = reg.Runtimes[idx]
		if strings.TrimSpace(entry.RuntimeID) == "" {
			entry.RuntimeID = maclawSrvRuntimePlatformID
		}
		entry.BaseURL = baseURL
		if strings.TrimSpace(adminSecret) != "" {
			entry.AdminSecret = strings.TrimSpace(adminSecret)
		}
		entry.UpdatedAt = now
		if strings.TrimSpace(entry.CreatedAt) == "" {
			entry.CreatedAt = now
		}
	}
	if strings.TrimSpace(tenantID) != "" {
		seen := false
		for _, id := range entry.TenantIDs {
			if strings.EqualFold(strings.TrimSpace(id), tenantID) {
				seen = true
				break
			}
		}
		if !seen {
			entry.TenantIDs = append(entry.TenantIDs, strings.TrimSpace(tenantID))
		}
	}
	if idx >= 0 {
		reg.Runtimes[idx] = entry
	} else {
		reg.Runtimes = append(reg.Runtimes, entry)
	}
	return saveMacLawSrvRuntimeRegistry(ctx, system, reg)
}

func macLawSrvRuntimeIDForBaseURL(reg macLawSrvRuntimeRegistry, baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return maclawSrvRuntimePlatformID
	}
	for _, runtime := range reg.Runtimes {
		if strings.EqualFold(strings.TrimSpace(runtime.RuntimeID), maclawSrvRuntimePlatformID) && strings.TrimSpace(runtime.BaseURL) == "" {
			return maclawSrvRuntimePlatformID
		}
	}
	for _, runtime := range reg.Runtimes {
		if strings.EqualFold(strings.TrimSpace(runtime.RuntimeID), maclawSrvRuntimePlatformID) {
			sum := sha256.Sum256([]byte(strings.ToLower(baseURL)))
			return maclawSrvRuntimePlatformID + "_" + hex.EncodeToString(sum[:4])
		}
	}
	return maclawSrvRuntimePlatformID
}

func (r platformProviderRegistry) find(platformID string) int {
	platformID = strings.TrimSpace(platformID)
	for i, p := range r.Providers {
		if strings.EqualFold(p.PlatformID, platformID) {
			return i
		}
	}
	return -1
}

func randomHexID(n int) string {
	if n <= 0 {
		n = 8
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

type platformAwareMachineSender struct {
	fallback veMachineEventSender
	system   store.SystemSettingsRepository
	tenants  store.TenantRepository
}

type employeeDeliveryTarget struct {
	entry    digitalEmployeeEntry
	provider platformProviderEntry
	runtime  macLawSrvRuntimeEntry
	tenantID string
	kind     string
}

func (s platformAwareMachineSender) SendToMachine(machineID string, msg any) error {
	deliveryTarget, ok := s.findPlatformEmployee(context.Background(), machineID, "", false)
	if ok {
		log.Printf("[ve-platform-delivery] suppress websocket event for platform employee machine=%s employee=%s tenant=%s kind=%s msg_type=%v", machineID, deliveryTarget.entry.ID, deliveryTarget.tenantID, deliveryTarget.kind, platformA2AMessageType(msg))
		return nil
	}
	if s.fallback != nil {
		return s.fallback.SendToMachine(machineID, msg)
	}
	return nil
}

func (s platformAwareMachineSender) DiscussionInviteAutoAccepts(tenantID, targetID string) (bool, error) {
	targetID = strings.TrimSpace(targetID)
	deliveryTarget, ok := s.findPlatformEmployee(context.Background(), targetID, tenantID, true)
	if !ok {
		return false, nil
	}
	switch deliveryTarget.kind {
	case maclawSrvRuntimePlatformID:
		if strings.TrimSpace(deliveryTarget.runtime.BaseURL) == "" {
			return false, fmt.Errorf("MaClawSrv runtime is not configured for platform employee %s", targetID)
		}
		log.Printf("[ve-platform-delivery] auto-accept trusted discussion invite target=%s tenant=%s employee=%s", targetID, deliveryTarget.tenantID, deliveryTarget.entry.ID)
		return true, nil
	case "platform_inactive":
		return false, fmt.Errorf("platform employee %s is not active", targetID)
	case "platform_not_ready":
		return false, fmt.Errorf("platform employee %s runtime is not online", targetID)
	default:
		return false, fmt.Errorf("MaClawSrv runtime is not configured for platform employee %s", targetID)
	}
}

func (s platformAwareMachineSender) SendDiscussionMessage(session *corea2a.Session, msg corea2a.GroupDiscussionMessage, target corea2a.Participant) (bool, *corea2a.GroupDiscussionMessage, error) {
	targetID := strings.TrimSpace(target.ID)
	tenantID := ""
	if session != nil {
		tenantID = session.TenantID
	}
	executable := shouldExecutePlatformDiscussionMessage(msg.Kind)
	deliveryTarget, ok := s.findPlatformEmployee(context.Background(), targetID, tenantID, executable)
	if !ok {
		return false, nil, nil
	}
	if s.isPlatformDiscussionOrigin(msg.FromID, tenantID) {
		log.Printf("[ve-platform-delivery] suppress platform-to-platform discussion execution session=%s from=%s target=%s tenant=%s employee=%s msg_id=%s msg_kind=%s", groupDiscussionSessionID(session), msg.FromID, targetID, deliveryTarget.tenantID, deliveryTarget.entry.ID, msg.ID, msg.Kind)
		return true, nil, nil
	}
	if !executable {
		log.Printf("[ve-platform-delivery] suppress non-executable discussion message session=%s from=%s target=%s tenant=%s employee=%s msg_id=%s msg_kind=%s", groupDiscussionSessionID(session), msg.FromID, targetID, deliveryTarget.tenantID, deliveryTarget.entry.ID, msg.ID, msg.Kind)
		return true, nil, nil
	}
	log.Printf("[ve-platform-delivery] route discussion session=%s from=%s target=%s tenant=%s employee=%s kind=%s msg_id=%s msg_kind=%s", groupDiscussionSessionID(session), msg.FromID, targetID, deliveryTarget.tenantID, deliveryTarget.entry.ID, deliveryTarget.kind, msg.ID, msg.Kind)
	if deliveryTarget.kind == maclawSrvRuntimePlatformID {
		reply, err := s.postMacLawSrvDiscussionMessage(context.Background(), deliveryTarget.entry, deliveryTarget.runtime, deliveryTarget.tenantID, macLawSrvDiscussionPayload(session, msg, targetID, target.RoleCode))
		if err != nil {
			log.Printf("[ve-platform-delivery] runtime delivery failed session=%s target=%s tenant=%s employee=%s platform_employee=%s: %v", groupDiscussionSessionID(session), targetID, deliveryTarget.tenantID, deliveryTarget.entry.ID, platformLogID(deliveryTarget.entry.PlatformEmployeeID), err)
			return true, nil, err
		}
		if strings.TrimSpace(reply) == "" {
			return true, nil, errors.New("MaClawSrv runtime response did not include assistant content")
		}
		return true, &corea2a.GroupDiscussionMessage{ID: macLawSrvReplyMessageID(msg, targetID), FromID: targetID, Kind: corea2a.MessageAnswer, Content: strings.TrimSpace(reply), CreatedAt: time.Now().UTC()}, nil
	}
	if deliveryTarget.kind == "platform_inactive" {
		log.Printf("[ve-platform-delivery] reject inactive platform employee session=%s target=%s tenant=%s employee=%s status=%s", groupDiscussionSessionID(session), targetID, deliveryTarget.tenantID, deliveryTarget.entry.ID, deliveryTarget.entry.Status)
		return true, nil, fmt.Errorf("platform employee %s is not active", targetID)
	}
	if deliveryTarget.kind == "platform_not_ready" {
		log.Printf("[ve-platform-delivery] reject not-ready platform employee session=%s target=%s tenant=%s employee=%s status=%s online_status=%s", groupDiscussionSessionID(session), targetID, deliveryTarget.tenantID, deliveryTarget.entry.ID, deliveryTarget.entry.Status, deliveryTarget.entry.OnlineStatus)
		return true, nil, fmt.Errorf("platform employee %s runtime is not online", targetID)
	}
	log.Printf("[ve-platform-delivery] runtime missing session=%s target=%s tenant=%s employee=%s platform_employee=%s runtime_provider=%s", groupDiscussionSessionID(session), targetID, deliveryTarget.tenantID, deliveryTarget.entry.ID, platformLogID(deliveryTarget.entry.PlatformEmployeeID), deliveryTarget.entry.RuntimeProviderID)
	return true, nil, fmt.Errorf("MaClawSrv runtime is not configured for platform employee %s", targetID)
}

func (s platformAwareMachineSender) SendDiscussionMessageAsync(session *corea2a.Session, msg corea2a.GroupDiscussionMessage, target corea2a.Participant, onReply func(corea2a.GroupDiscussionMessage) error) (bool, error) {
	targetID := strings.TrimSpace(target.ID)
	tenantID := ""
	if session != nil {
		tenantID = session.TenantID
	}
	executable := shouldExecutePlatformDiscussionMessage(msg.Kind)
	deliveryTarget, ok := s.findPlatformEmployee(context.Background(), targetID, tenantID, executable)
	if !ok {
		return false, nil
	}
	if s.isPlatformDiscussionOrigin(msg.FromID, tenantID) {
		log.Printf("[ve-platform-delivery] suppress async platform-to-platform discussion execution session=%s from=%s target=%s tenant=%s employee=%s msg_id=%s msg_kind=%s", groupDiscussionSessionID(session), msg.FromID, targetID, deliveryTarget.tenantID, deliveryTarget.entry.ID, msg.ID, msg.Kind)
		return true, nil
	}
	if !executable {
		log.Printf("[ve-platform-delivery] suppress async non-executable discussion message session=%s from=%s target=%s tenant=%s employee=%s msg_id=%s msg_kind=%s", groupDiscussionSessionID(session), msg.FromID, targetID, deliveryTarget.tenantID, deliveryTarget.entry.ID, msg.ID, msg.Kind)
		return true, nil
	}
	log.Printf("[ve-platform-delivery] queue discussion session=%s from=%s target=%s tenant=%s employee=%s kind=%s msg_id=%s msg_kind=%s", groupDiscussionSessionID(session), msg.FromID, targetID, deliveryTarget.tenantID, deliveryTarget.entry.ID, deliveryTarget.kind, msg.ID, msg.Kind)
	if deliveryTarget.kind == maclawSrvRuntimePlatformID {
		sessionCopy := cloneSession(session)
		msgCopy := msg
		targetCopy := target
		go func() {
			started := time.Now()
			reply, err := s.postMacLawSrvDiscussionMessage(context.Background(), deliveryTarget.entry, deliveryTarget.runtime, deliveryTarget.tenantID, macLawSrvDiscussionPayload(sessionCopy, msgCopy, targetID, targetCopy.RoleCode))
			if err != nil {
				log.Printf("[ve-platform-delivery] async runtime delivery failed session=%s target=%s tenant=%s employee=%s platform_employee=%s duration=%s: %v", groupDiscussionSessionID(sessionCopy), targetID, deliveryTarget.tenantID, deliveryTarget.entry.ID, platformLogID(deliveryTarget.entry.PlatformEmployeeID), time.Since(started), err)
				return
			}
			reply = strings.TrimSpace(reply)
			if reply == "" {
				log.Printf("[ve-platform-delivery] async runtime empty reply session=%s target=%s tenant=%s employee=%s platform_employee=%s duration=%s", groupDiscussionSessionID(sessionCopy), targetID, deliveryTarget.tenantID, deliveryTarget.entry.ID, platformLogID(deliveryTarget.entry.PlatformEmployeeID), time.Since(started))
				return
			}
			replyMsg := corea2a.GroupDiscussionMessage{ID: macLawSrvReplyMessageID(msgCopy, targetID), FromID: targetID, Kind: corea2a.MessageAnswer, Content: reply, CreatedAt: time.Now().UTC()}
			if onReply != nil {
				if err := onReply(replyMsg); err != nil {
					log.Printf("[ve-platform-delivery] async runtime reply persist failed session=%s target=%s tenant=%s employee=%s platform_employee=%s duration=%s: %v", groupDiscussionSessionID(sessionCopy), targetID, deliveryTarget.tenantID, deliveryTarget.entry.ID, platformLogID(deliveryTarget.entry.PlatformEmployeeID), time.Since(started), err)
					return
				}
			}
			log.Printf("[ve-platform-delivery] async runtime reply persisted session=%s target=%s tenant=%s employee=%s platform_employee=%s duration=%s content_chars=%d", groupDiscussionSessionID(sessionCopy), targetID, deliveryTarget.tenantID, deliveryTarget.entry.ID, platformLogID(deliveryTarget.entry.PlatformEmployeeID), time.Since(started), len([]rune(reply)))
		}()
		return true, nil
	}
	if deliveryTarget.kind == "platform_inactive" {
		log.Printf("[ve-platform-delivery] reject inactive platform employee session=%s target=%s tenant=%s employee=%s status=%s", groupDiscussionSessionID(session), targetID, deliveryTarget.tenantID, deliveryTarget.entry.ID, deliveryTarget.entry.Status)
		return true, fmt.Errorf("platform employee %s is not active", targetID)
	}
	if deliveryTarget.kind == "platform_not_ready" {
		log.Printf("[ve-platform-delivery] reject not-ready platform employee session=%s target=%s tenant=%s employee=%s status=%s online_status=%s", groupDiscussionSessionID(session), targetID, deliveryTarget.tenantID, deliveryTarget.entry.ID, deliveryTarget.entry.Status, deliveryTarget.entry.OnlineStatus)
		return true, fmt.Errorf("platform employee %s runtime is not online", targetID)
	}
	log.Printf("[ve-platform-delivery] runtime missing session=%s target=%s tenant=%s employee=%s platform_employee=%s runtime_provider=%s", groupDiscussionSessionID(session), targetID, deliveryTarget.tenantID, deliveryTarget.entry.ID, platformLogID(deliveryTarget.entry.PlatformEmployeeID), deliveryTarget.entry.RuntimeProviderID)
	return true, fmt.Errorf("MaClawSrv runtime is not configured for platform employee %s", targetID)
}

func shouldExecutePlatformDiscussionMessage(kind corea2a.MessageKind) bool {
	switch kind {
	case "", corea2a.MessageStatement, corea2a.MessageQuestion:
		return true
	default:
		return false
	}
}

func (s platformAwareMachineSender) isPlatformDiscussionOrigin(fromID, tenantID string) bool {
	fromID = strings.TrimSpace(fromID)
	if fromID == "" {
		return false
	}
	_, ok := s.findPlatformEmployee(context.Background(), fromID, tenantID, false)
	return ok
}

func groupDiscussionSessionID(session *corea2a.Session) string {
	if session == nil {
		return ""
	}
	return strings.TrimSpace(session.ID)
}

func macLawSrvDiscussionEnvelope(session *corea2a.Session, msg corea2a.GroupDiscussionMessage, targetID string) corea2a.GroupEnvelope {
	fromID := strings.TrimSpace(msg.FromID)
	envelope := corea2a.NewGroupEnvelope(newGroupDiscussionID("a2aenv"), corea2a.GroupMessageDiscussionMessage, fromID, time.Now().UTC())
	if session != nil {
		envelope.SessionID = session.ID
	}
	envelope.ToIDs = []string{strings.TrimSpace(targetID)}
	envelope.Message = &msg
	return envelope
}

func macLawSrvDiscussionPayload(session *corea2a.Session, msg corea2a.GroupDiscussionMessage, targetID, targetRole string) map[string]any {
	inner := map[string]any{
		"envelope":    macLawSrvDiscussionEnvelope(session, msg, targetID),
		"target_role": strings.TrimSpace(targetRole),
	}
	if context := macLawSrvDiscussionContext(session, msg, targetID); context != "" {
		inner["discussion_context"] = context
		inner["content"] = context
	}
	return map[string]any{"type": "ve:discussion_message", "ts": time.Now().Unix(), "payload": inner}
}

func macLawSrvDiscussionContext(session *corea2a.Session, msg corea2a.GroupDiscussionMessage, targetID string) string {
	current := strings.TrimSpace(msg.Content)
	if session == nil {
		return current
	}
	recent := macLawSrvDiscussionRecentContext(macLawSrvDiscussionRecentMessages(session), msg)
	var b strings.Builder
	b.WriteString("你正在参与一个多人 Hub 群聊。请基于共享群聊上下文回答，不要把当前消息当成孤立的 1v1 私聊。@ 和 to_ids 只决定本轮谁回复，不决定可见范围。")
	if topic := strings.TrimSpace(session.Topic); topic != "" {
		b.WriteString("\n主题: " + topic)
	}
	if goal := strings.TrimSpace(session.Goal); goal != "" {
		b.WriteString("\n目标: " + goal)
	}
	if strings.TrimSpace(targetID) != "" {
		b.WriteString("\n当前被点名参与者: " + strings.TrimSpace(targetID))
	}
	if participants := macLawSrvDiscussionParticipantContext(session); participants != "" {
		b.WriteString("\n\nParticipants:\n")
		b.WriteString(participants)
	}
	if summary := strings.TrimSpace(session.ContextSummary); summary != "" {
		b.WriteString("\n\nShared compressed memory:\n")
		b.WriteString(summary)
	}
	if len(recent) > 0 {
		b.WriteString("\n\n最近群聊上下文:\n")
		b.WriteString(strings.Join(recent, "\n"))
	}
	if current != "" {
		b.WriteString("\n\n当前消息来自 " + strings.TrimSpace(msg.FromID) + ": " + current)
	}
	return b.String()
}

func macLawSrvDiscussionRecentMessages(session *corea2a.Session) []corea2a.Message {
	if session == nil {
		return nil
	}
	if strings.TrimSpace(session.ContextSummary) == "" {
		return session.Messages
	}
	return macLawSrvDiscussionMessagesAfterSummary(session.Messages, session.SummaryUpToID)
}

func macLawSrvDiscussionMessagesAfterSummary(messages []corea2a.Message, summaryUpToID string) []corea2a.Message {
	summaryUpToID = strings.TrimSpace(summaryUpToID)
	if summaryUpToID == "" {
		return messages
	}
	for i, msg := range messages {
		if strings.EqualFold(strings.TrimSpace(msg.ID), summaryUpToID) {
			return messages[i+1:]
		}
	}
	return messages
}

func macLawSrvDiscussionRecentContext(messages []corea2a.Message, current corea2a.GroupDiscussionMessage) []string {
	recent := make([]string, 0, len(messages))
	currentID := strings.TrimSpace(current.ID)
	currentIndex := macLawSrvDiscussionCurrentMessageIndex(messages, current)
	var streamFrom string
	var streamContent strings.Builder
	flushStream := func() {
		content := strings.TrimSpace(streamContent.String())
		if content != "" {
			fromID := strings.TrimSpace(streamFrom)
			if fromID == "" {
				fromID = "unknown"
			}
			recent = append(recent, fmt.Sprintf("[%s] %s", fromID, truncateMacLawSrvDiscussionContext(content, 1200)))
		}
		streamFrom = ""
		streamContent.Reset()
	}
	for i, item := range messages {
		if i == currentIndex {
			continue
		}
		content := strings.TrimSpace(item.Content)
		if currentID != "" && strings.EqualFold(strings.TrimSpace(item.ID), currentID) {
			continue
		}
		switch item.Kind {
		case corea2a.MessageStreamEnd, corea2a.MessageHandoff:
			flushStream()
			continue
		case corea2a.MessageStreamChunk:
			chunk := item.Content
			if chunk == "" {
				continue
			}
			fromID := strings.TrimSpace(item.FromID)
			if streamFrom != "" && !groupDiscussionParticipantIdentityMatches(streamFrom, fromID) {
				flushStream()
			}
			if streamFrom == "" {
				streamFrom = fromID
			}
			streamContent.WriteString(chunk)
			continue
		default:
			flushStream()
		}
		if content == "" || strings.HasPrefix(strings.ToLower(content), "invitation ") {
			continue
		}
		fromID := strings.TrimSpace(item.FromID)
		if fromID == "" {
			fromID = "unknown"
		}
		recent = append(recent, fmt.Sprintf("[%s] %s", fromID, truncateMacLawSrvDiscussionContext(content, 1200)))
	}
	flushStream()
	if len(recent) > 12 {
		recent = recent[len(recent)-12:]
	}
	return recent
}

func macLawSrvDiscussionCurrentMessageIndex(messages []corea2a.Message, current corea2a.GroupDiscussionMessage) int {
	if strings.TrimSpace(current.ID) != "" {
		return -1
	}
	currentContent := strings.TrimSpace(current.Content)
	if currentContent == "" {
		return -1
	}
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if strings.TrimSpace(msg.Content) != currentContent {
			continue
		}
		if current.Kind != "" && msg.Kind != current.Kind {
			continue
		}
		if strings.TrimSpace(current.FromID) != "" && !groupDiscussionParticipantIdentityMatches(msg.FromID, current.FromID) {
			continue
		}
		return i
	}
	return -1
}

func truncateMacLawSrvDiscussionContext(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if maxRunes <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "..."
}

func macLawSrvDiscussionParticipantContext(session *corea2a.Session) string {
	if session == nil || len(session.Participants) == 0 {
		return ""
	}
	items := make([]string, 0, len(session.Participants))
	seen := map[string]struct{}{}
	for _, participant := range session.Participants {
		id := strings.TrimSpace(participant.ID)
		if id == "" {
			continue
		}
		key := groupDiscussionCanonicalParticipantIdentityKey(id)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		role := strings.TrimSpace(participant.RoleCode)
		if role == "" {
			role = "participant"
		}
		items = append(items, fmt.Sprintf("- %s (%s)", id, role))
	}
	return strings.Join(items, "\n")
}

func isMacLawSrvRuntimeProvider(provider platformProviderEntry, entry digitalEmployeeEntry) bool {
	return strings.EqualFold(strings.TrimSpace(provider.PlatformID), maclawSrvRuntimePlatformID) || strings.EqualFold(strings.TrimSpace(entry.PlatformID), maclawSrvRuntimePlatformID) || strings.EqualFold(strings.TrimSpace(entry.RuntimeProviderID), maclawSrvRuntimePlatformID)
}

func isMacLawSrvRuntimeDeliveryEmployee(entry digitalEmployeeEntry) bool {
	return isPlatformRuntimeEmployeeEntry(entry) && isMacLawSrvRuntimeProvider(platformProviderEntry{}, entry)
}

func (s platformAwareMachineSender) findPlatformEmployee(ctx context.Context, machineID, tenantHint string, checkRuntimePresence bool) (employeeDeliveryTarget, bool) {
	machineID = strings.TrimSpace(machineID)
	if machineID == "" || s.system == nil {
		return employeeDeliveryTarget{}, false
	}
	providers := loadPlatformProviderRegistry(ctx, s.system)
	runtimes := loadMacLawSrvRuntimeRegistry(ctx, s.system)
	tenantIDs := map[string]struct{}{}
	if tenantHint = strings.TrimSpace(tenantHint); tenantHint != "" {
		if platformTenantIDAllowed(ctx, s.tenants, tenantHint) {
			tenantIDs[tenantHint] = struct{}{}
		}
	} else {
		if s.tenants != nil {
			if items, err := s.tenants.List(ctx); err == nil {
				for _, item := range items {
					if isPlatformTenantActive(item) {
						tenantIDs[item.ID] = struct{}{}
					}
				}
			}
		}
		for _, provider := range providers.Providers {
			for _, domain := range provider.TenantDomains {
				for _, id := range []string{domain.HubTenantID, domain.TenantID} {
					if strings.TrimSpace(id) != "" {
						trimmedID := strings.TrimSpace(id)
						if platformTenantIDAllowed(ctx, s.tenants, trimmedID) {
							tenantIDs[trimmedID] = struct{}{}
						}
					}
				}
			}
		}
	}
	for tenantID := range tenantIDs {
		registry := loadVERegistry(ctx, scopedSystemSettingsForTenant(tenantID, s.system))
		idx := registry.findByIDOrMachineID(machineID)
		if idx < 0 {
			idx = registry.findByPlatformEmployeeID(machineID)
		}
		if idx < 0 {
			continue
		}
		entry := registry.Employees[idx]
		if !isPlatformRuntimeEmployeeEntry(entry) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(entry.Status), veStatusActive) {
			return employeeDeliveryTarget{entry: entry, tenantID: tenantID, kind: "platform_inactive"}, true
		}
		if runtimeProviderID := strings.TrimSpace(entry.RuntimeProviderID); runtimeProviderID != "" && !strings.EqualFold(runtimeProviderID, maclawSrvRuntimePlatformID) {
			continue
		}
		isMacLawSrvDeliveryEmployee := isMacLawSrvRuntimeDeliveryEmployee(entry)
		if checkRuntimePresence && isMacLawSrvDeliveryEmployee {
			presence := loadMacLawSrvRuntimePresence(ctx, s.system, tenantID)
			if presence.Loaded {
				entry = applyVEDiscoverablePresence(ctx, entry, nil, presence)
				if !strings.EqualFold(strings.TrimSpace(entry.Status), veStatusActive) {
					return employeeDeliveryTarget{entry: entry, tenantID: tenantID, kind: "platform_inactive"}, true
				}
				if !strings.EqualFold(strings.TrimSpace(entry.OnlineStatus), veOnlineStatusOnline) {
					return employeeDeliveryTarget{entry: entry, tenantID: tenantID, kind: "platform_not_ready"}, true
				}
			}
		}
		if isMacLawSrvDeliveryEmployee {
			if runtime, ok := runtimes.findForTenant(tenantID); ok && strings.TrimSpace(entry.PlatformEmployeeID) != "" {
				return employeeDeliveryTarget{entry: entry, runtime: runtime, tenantID: tenantID, kind: maclawSrvRuntimePlatformID}, true
			}
		}
		if runtime, ok := runtimes.findForTenant(tenantID); ok && strings.TrimSpace(entry.PlatformID) != "" && strings.TrimSpace(entry.PlatformEmployeeID) != "" {
			return employeeDeliveryTarget{entry: entry, runtime: runtime, tenantID: tenantID, kind: maclawSrvRuntimePlatformID}, true
		}
		if isMacLawSrvDeliveryEmployee && strings.TrimSpace(entry.PlatformEmployeeID) != "" {
			return employeeDeliveryTarget{entry: entry, tenantID: tenantID, kind: maclawSrvRuntimePlatformID}, true
		}
		if strings.TrimSpace(entry.PlatformID) != "" || strings.TrimSpace(entry.PlatformEmployeeID) != "" || strings.TrimSpace(entry.RuntimeProviderID) != "" {
			return employeeDeliveryTarget{entry: entry, tenantID: tenantID, kind: "platform_missing_runtime"}, true
		}
	}
	return employeeDeliveryTarget{}, false
}

func isPlatformRuntimeEmployeeEntry(entry digitalEmployeeEntry) bool {
	if typ := normalizeVEEmployeeType(entry.EmployeeType); typ == veEmployeeTypePhysical {
		return false
	}
	return strings.TrimSpace(entry.PlatformEmployeeID) != ""
}

func (s platformAwareMachineSender) postMacLawSrvDiscussionMessage(ctx context.Context, entry digitalEmployeeEntry, runtime macLawSrvRuntimeEntry, tenantID string, msg any) (string, error) {
	payload := platformA2APayload(msg)
	if strings.TrimSpace(entry.PlatformEmployeeID) == "" {
		return "", errors.New("MaClawSrv runtime employee id is empty")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(runtime.BaseURL), "/")
	if baseURL == "" {
		return "", errors.New("MaClawSrv runtime base URL is empty")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	endpoint := baseURL + "/api/runtime/virtual-employees/" + url.PathEscape(entry.PlatformEmployeeID) + "/discussion-messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(runtime.AdminSecret) != "" {
		req.Header.Set("X-MaClaw-Admin-Secret", strings.TrimSpace(runtime.AdminSecret))
	}
	setPlatformEmployeeHubHeaders(req, entry, tenantID)
	client := &http.Client{Timeout: platformA2ADeliveryTimeout}
	started := time.Now()
	log.Printf("[ve-platform-delivery] post runtime start tenant=%s employee=%s platform_employee=%s endpoint=%s request_id=%v hub_discussion=%v hub_message=%v timeout=%s", tenantID, entry.ID, platformLogID(entry.PlatformEmployeeID), runtimeDiscussionEndpointLogValue(endpoint), payload["request_id"], payload["hub_discussion_id"], payload["hub_message_id"], platformA2ADeliveryTimeout)
	resp, err := client.Do(req)
	if err != nil {
		detail := sanitizeRuntimeDeliveryErrorText(err.Error(), entry.PlatformEmployeeID)
		log.Printf("[ve-platform-delivery] post runtime transport failed tenant=%s employee=%s platform_employee=%s duration=%s err=%s", tenantID, entry.ID, platformLogID(entry.PlatformEmployeeID), time.Since(started), detail)
		return "", fmt.Errorf("MaClawSrv runtime delivery failed: %s", detail)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	log.Printf("[ve-platform-delivery] post runtime done tenant=%s employee=%s platform_employee=%s status=%d duration=%s bytes=%d", tenantID, entry.ID, platformLogID(entry.PlatformEmployeeID), resp.StatusCode, time.Since(started), len(respBody))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := strings.TrimSpace(string(respBody))
		if detail != "" {
			return "", fmt.Errorf("MaClawSrv runtime returned status %d: %s", resp.StatusCode, sanitizeRuntimeDeliveryErrorText(detail, entry.PlatformEmployeeID))
		}
		return "", fmt.Errorf("MaClawSrv runtime returned status %d", resp.StatusCode)
	}
	reply := macLawSrvRuntimeReplyContent(respBody)
	if reply == "" {
		return "", errors.New("MaClawSrv runtime response did not include assistant content")
	}
	return reply, nil
}

func runtimeDiscussionEndpointLogValue(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "/api/runtime/virtual-employees/*/discussion-messages"
	}
	if parsed.Host == "" {
		return "/api/runtime/virtual-employees/*/discussion-messages"
	}
	return parsed.Host + "/api/runtime/virtual-employees/*/discussion-messages"
}

func platformLogID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:8])
}

func sanitizeRuntimeDeliveryErrorText(text, platformEmployeeID string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "transport error"
	}
	if platformEmployeeID = strings.TrimSpace(platformEmployeeID); platformEmployeeID != "" {
		text = strings.ReplaceAll(text, platformEmployeeID, "*")
	}
	return truncateRemoteResponseDetail(text)
}

func setPlatformEmployeeHubHeaders(req *http.Request, entry digitalEmployeeEntry, tenantID string) {
	if req == nil {
		return
	}
	if strings.TrimSpace(tenantID) != "" {
		req.Header.Set("X-VE-Hub-Tenant-ID", strings.TrimSpace(tenantID))
	}
	if strings.TrimSpace(entry.ID) != "" {
		req.Header.Set("X-VE-Hub-Employee-ID", strings.TrimSpace(entry.ID))
	}
	if strings.TrimSpace(entry.OwnerUserID) != "" {
		req.Header.Set("X-VE-Hub-Account-ID", strings.TrimSpace(entry.OwnerUserID))
	}
}

func macLawSrvRuntimeReplyContent(respBody []byte) string {
	if len(bytes.TrimSpace(respBody)) == 0 || !json.Valid(respBody) {
		return strings.TrimSpace(string(respBody))
	}
	var root map[string]any
	if err := json.Unmarshal(respBody, &root); err != nil {
		return ""
	}
	if msg, _ := root["message"].(map[string]any); msg != nil {
		if content, ok := msg["content"]; ok {
			return strings.TrimSpace(fmt.Sprint(content))
		}
	}
	if content, ok := root["content"]; ok {
		return strings.TrimSpace(fmt.Sprint(content))
	}
	return ""
}

func macLawSrvReplyMessageID(msg corea2a.GroupDiscussionMessage, targetID string) string {
	key := strings.TrimSpace(msg.ID)
	if key == "" {
		key = strings.TrimSpace(msg.SessionID) + "\x1f" + strings.TrimSpace(msg.FromID) + "\x1f" + strings.TrimSpace(msg.Content)
	}
	key += "\x1f" + strings.TrimSpace(targetID)
	sum := sha256.Sum256([]byte(key))
	return "a2amsg_maclawsrv_" + hex.EncodeToString(sum[:8])
}

func truncateRemoteResponseDetail(detail string) string {
	detail = strings.TrimSpace(detail)
	if len(detail) > 500 {
		return detail[:500] + "..."
	}
	return detail
}

func platformA2APayload(msg any) map[string]any {
	payload := map[string]any{"payload": msg}
	if outer, ok := msg.(map[string]any); ok {
		if typ := strings.TrimSpace(fmt.Sprintf("%v", outer["type"])); typ != "" {
			payload["event_type"] = typ
		}
		if inner, ok := outer["payload"].(map[string]any); ok {
			payload["payload"] = inner
			if content := envelopeStringField(inner, "content", "Content"); content != "" {
				payload["content"] = content
			}
			if env, ok := inner["envelope"]; ok {
				if typ := envelopeStringField(env, "Type", "type"); typ != "" {
					payload["protocol_event_type"] = typ
					if _, ok := payload["event_type"]; !ok {
						payload["event_type"] = typ
					}
				}
				if requestID := envelopeStringField(env, "ID", "id"); requestID != "" {
					payload["request_id"] = requestID
				}
				if sessionID := envelopeStringField(env, "SessionID", "session_id"); sessionID != "" {
					payload["hub_discussion_id"] = sessionID
				}
				if message := envelopeField(env, "Message", "message"); message != nil {
					if messageID := envelopeStringField(message, "ID", "id"); messageID != "" {
						payload["hub_message_id"] = messageID
					}
					if content := envelopeStringField(message, "Content", "content"); content != "" && payloadStringEmpty(payload, "content") {
						payload["content"] = content
					}
				}
			}
		}
	}
	return payload
}

func payloadStringEmpty(payload map[string]any, key string) bool {
	value, ok := payload[key]
	if !ok || value == nil {
		return true
	}
	return strings.TrimSpace(fmt.Sprint(value)) == ""
}

func platformA2AMessageType(msg any) any {
	if outer, ok := msg.(map[string]any); ok {
		if typ := outer["type"]; typ != nil {
			return typ
		}
		if inner, ok := outer["payload"].(map[string]any); ok {
			if typ := inner["type"]; typ != nil {
				return typ
			}
			if env := inner["envelope"]; env != nil {
				return envelopeField(env, "Type", "type")
			}
		}
	}
	return ""
}

func envelopeStringField(v any, names ...string) string {
	value := envelopeField(v, names...)
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func envelopeField(v any, names ...string) any {
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	for _, name := range names {
		if value, ok := m[name]; ok {
			return value
		}
		if value, ok := m[strings.ToLower(name)]; ok {
			return value
		}
	}
	return nil
}
