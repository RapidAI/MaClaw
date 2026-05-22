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
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const platformProviderRegistryKey = "ve_platform_provider_registry"
const platformRequestNonceRegistryKey = "ve_platform_request_nonce_registry"
const platformRequestReplayWindow = 10 * time.Minute

type platformProviderRegistry struct {
	Providers []platformProviderEntry `json:"providers"`
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
	SourceType         string `json:"source_type"`
	AccountType        string `json:"account_type"`
	ReviewStatus       string `json:"review_status"`
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
			out = append(out, map[string]any{"hub_tenant_id": t.ID, "id": t.ID, "code": t.Slug, "slug": t.Slug, "name": t.Name, "status": t.Status, "primary_domain": t.PrimaryDomain, "virtual_mail_domain": virtualDomain, "ve_enabled": strings.EqualFold(strings.TrimSpace(t.Status), "active") && virtualDomain != "", "updated_at": t.UpdatedAt.Format(time.RFC3339)})
		}
		writeJSON(w, http.StatusOK, map[string]any{"tenants": out})
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
		if platformID != "" && strings.TrimSpace(employee.PlatformID) != platformID {
			continue
		}
		if strings.TrimSpace(employee.PlatformEmployeeID) == platformEmployeeID || strings.TrimSpace(employee.ID) == platformEmployeeID || strings.TrimSpace(employee.MachineID) == platformEmployeeID {
			return employee, true
		}
	}
	return digitalEmployeeEntry{}, false
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

func PlatformSourceUsersSyncHandler(system store.SystemSettingsRepository, users store.UserRepository, tenants store.TenantRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, body, ok := authenticatePlatformRequest(w, r, system)
		if !ok {
			return
		}
		var req struct {
			TenantID    string `json:"tenant_id"`
			HubTenantID string `json:"hub_tenant_id"`
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
		out, err := platformSourceUsersForTenant(r.Context(), system, users, tenantID)
		if err != nil {
			if errors.Is(err, errPlatformUserRepositoryUnavailable) {
				writeError(w, http.StatusServiceUnavailable, "USER_REPOSITORY_UNAVAILABLE", "user repository is unavailable")
				return
			}
			writeError(w, http.StatusInternalServerError, "SOURCE_USERS_LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": out, "users": out})
	}
}

var errPlatformUserRepositoryUnavailable = errors.New("user repository is unavailable")

func platformSourceUsersForTenant(ctx context.Context, system store.SystemSettingsRepository, users store.UserRepository, tenantID string) ([]map[string]any, error) {
	if users == nil {
		return nil, errPlatformUserRepositoryUnavailable
	}
	items, err := users.ListByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	excludedIDs, excludedEmails := platformEmployeeAccountExclusions(ctx, system, tenantID)
	out := make([]map[string]any, 0, len(items))
	for _, user := range items {
		if user == nil {
			continue
		}
		if _, ok := excludedIDs[strings.TrimSpace(user.ID)]; ok {
			continue
		}
		if _, ok := excludedEmails[strings.ToLower(strings.TrimSpace(user.Email))]; ok {
			continue
		}
		out = append(out, map[string]any{
			"id":           user.ID,
			"tenant_id":    tenantID,
			"external_id":  user.ID,
			"email":        user.Email,
			"display_name": user.Email,
			"department":   "",
			"title":        "",
			"status":       user.Status,
			"updated_at":   user.UpdatedAt.Format(time.RFC3339),
		})
	}
	return out, nil
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
func PlatformEmployeeRegisterHandler(system store.SystemSettingsRepository, tenants store.TenantRepository, users store.UserRepository) http.HandlerFunc {
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
		userID := ""
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
		}
		machineIDSource := strings.TrimSpace(req.EmployeeID)
		if machineIDSource == "" {
			machineIDSource = platformEmployeeID
		}
		machineID := "ve_" + strings.Trim(machineIDSource, "_")
		veID := "ve_" + strings.TrimPrefix(machineID, "ve_")
		tenantSystem := scopedSystemSettingsForTenant(tenantID, system)
		reg := loadVERegistry(r.Context(), tenantSystem)
		veEntry := digitalEmployeeEntry{ID: veID, MachineID: machineID, PlatformID: entry.PlatformID, PlatformEmployeeID: platformEmployeeID, OwnerUserID: userID, OwnerEmail: email, Name: name, SkillDescription: strings.TrimSpace(req.SkillDescription), AccessPolicy: "public", Status: veStatusActive, OnlineStatus: "platform", RegisteredAt: time.Now().UTC().Format(time.RFC3339), UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
		if i := reg.findByIDOrMachineID(veID); i >= 0 {
			veEntry.RegisteredAt = reg.Employees[i].RegisteredAt
			reg.Employees[i] = veEntry
		} else {
			reg.Employees = append(reg.Employees, veEntry)
		}
		if err := saveVERegistry(r.Context(), tenantSystem, reg); err != nil {
			writeError(w, http.StatusInternalServerError, "VE_REGISTRY_SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "employee": veEntry, "hub_employee_id": veEntry.ID, "hub_account_id": userID, "hub_tenant_id": tenantID, "platform_id": entry.PlatformID})
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
		tenantID := strings.TrimSpace(req.HubTenantID)
		var updated bool
		var err error
		if tenantID != "" {
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
			updated, err = updatePlatformEmployeeStatusInTenant(r.Context(), system, tenantID, entry.PlatformID, platformEmployeeID, status)
		} else {
			tenantID, updated, err = updatePlatformEmployeeStatus(r.Context(), system, tenants, entry.PlatformID, platformEmployeeID, status)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "EMPLOYEE_STATUS_UPDATE_FAILED", err.Error())
			return
		}
		if !updated {
			writeError(w, http.StatusNotFound, "EMPLOYEE_NOT_FOUND", "platform employee was not found in Hub registry")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "employee_id": platformEmployeeID, "hub_tenant_id": tenantID, "service_status": req.ServiceStatus, "hub_status": status})
	}
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
	case "suspended", "disabled", "inactive", "stopped":
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

func updatePlatformEmployeeStatus(ctx context.Context, system store.SystemSettingsRepository, tenants store.TenantRepository, platformID, platformEmployeeID, status string) (string, bool, error) {
	platformID = strings.TrimSpace(platformID)
	platformEmployeeID = strings.TrimSpace(platformEmployeeID)
	if platformEmployeeID == "" {
		return "", false, nil
	}
	tenantIDs := map[string]struct{}{}
	if tenants != nil {
		items, err := tenants.List(ctx)
		if err != nil {
			return "", false, err
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
	for tenantID := range tenantIDs {
		tenantSystem := scopedSystemSettingsForTenant(tenantID, system)
		registry := loadVERegistry(ctx, tenantSystem)
		changed := false
		for i := range registry.Employees {
			emp := &registry.Employees[i]
			if platformID != "" && strings.TrimSpace(emp.PlatformID) != platformID {
				continue
			}
			if strings.TrimSpace(emp.PlatformEmployeeID) != platformEmployeeID && strings.TrimSpace(emp.ID) != platformEmployeeID && strings.TrimSpace(emp.MachineID) != platformEmployeeID {
				continue
			}
			emp.Status = status
			if status == veStatusDisabled {
				now := time.Now().UTC().Format(time.RFC3339)
				emp.DisabledAt = now
				emp.OnlineStatus = veOnlineStatusOffline
			} else if status == veStatusActive {
				emp.DisabledAt = ""
				emp.OnlineStatus = "platform"
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
		if platformID != "" && strings.TrimSpace(emp.PlatformID) != platformID {
			continue
		}
		if strings.TrimSpace(emp.PlatformEmployeeID) != platformEmployeeID && strings.TrimSpace(emp.ID) != platformEmployeeID && strings.TrimSpace(emp.MachineID) != platformEmployeeID {
			continue
		}
		emp.Status = status
		if status == veStatusDisabled {
			now := time.Now().UTC().Format(time.RFC3339)
			emp.DisabledAt = now
			emp.OnlineStatus = veOnlineStatusOffline
		} else if status == veStatusActive {
			emp.DisabledAt = ""
			emp.OnlineStatus = "platform"
		}
		emp.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := saveVERegistry(ctx, tenantSystem, registry); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
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
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		return nil, "", "", "", err
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

func (s platformAwareMachineSender) SendToMachine(machineID string, msg any) error {
	entry, provider, tenantID, ok := s.findPlatformEmployee(context.Background(), machineID)
	if ok {
		if err := s.postPlatformA2A(context.Background(), entry, provider, tenantID, msg); err == nil {
			return nil
		} else if s.fallback == nil {
			return err
		}
	}
	if s.fallback != nil {
		return s.fallback.SendToMachine(machineID, msg)
	}
	return nil
}

func (s platformAwareMachineSender) findPlatformEmployee(ctx context.Context, machineID string) (digitalEmployeeEntry, platformProviderEntry, string, bool) {
	machineID = strings.TrimSpace(machineID)
	if machineID == "" || s.system == nil {
		return digitalEmployeeEntry{}, platformProviderEntry{}, "", false
	}
	providers := loadPlatformProviderRegistry(ctx, s.system)
	providerByID := map[string]platformProviderEntry{}
	for _, provider := range providers.Providers {
		providerByID[provider.PlatformID] = provider
	}
	tenantIDs := map[string]struct{}{}
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
	for tenantID := range tenantIDs {
		registry := loadVERegistry(ctx, scopedSystemSettingsForTenant(tenantID, s.system))
		idx := registry.findByMachineID(machineID)
		if idx < 0 {
			continue
		}
		entry := registry.Employees[idx]
		provider, ok := providerByID[entry.PlatformID]
		if ok && strings.TrimSpace(provider.CallbackBaseURL) != "" && strings.TrimSpace(entry.PlatformEmployeeID) != "" {
			return entry, provider, tenantID, true
		}
	}
	return digitalEmployeeEntry{}, platformProviderEntry{}, "", false
}

func (s platformAwareMachineSender) postPlatformA2A(ctx context.Context, entry digitalEmployeeEntry, provider platformProviderEntry, tenantID string, msg any) error {
	body, err := json.Marshal(platformA2APayload(msg))
	if err != nil {
		return err
	}
	endpoint := strings.TrimRight(provider.CallbackBaseURL, "/") + "/a2a/employees/" + url.PathEscape(entry.PlatformEmployeeID) + platformA2AEndpointSuffix(msg)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(provider.CallbackSecret) != "" {
		req.Header.Set("X-VE-Callback-Secret", provider.CallbackSecret)
	}
	setPlatformCallbackReplayHeaders(req, "a2a")
	if strings.TrimSpace(tenantID) != "" {
		req.Header.Set("X-VE-Hub-Tenant-ID", strings.TrimSpace(tenantID))
	}
	if strings.TrimSpace(entry.ID) != "" {
		req.Header.Set("X-VE-Hub-Employee-ID", strings.TrimSpace(entry.ID))
	}
	if strings.TrimSpace(entry.OwnerUserID) != "" {
		req.Header.Set("X-VE-Hub-Account-ID", strings.TrimSpace(entry.OwnerUserID))
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("platform A2A callback returned status %d", resp.StatusCode)
	}
	return nil
}

func platformA2AEndpointSuffix(msg any) string {
	eventType := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", platformA2AMessageType(msg))))
	switch eventType {
	case "ve:discussion_invite", "discussion_invite", "invitation":
		return "/invite"
	case "ve:discussion_cancel", "discussion_cancel", "cancel":
		return "/cancel"
	default:
		return "/messages"
	}
}

func platformA2APayload(msg any) map[string]any {
	payload := map[string]any{"payload": msg}
	if outer, ok := msg.(map[string]any); ok {
		if typ := strings.TrimSpace(fmt.Sprintf("%v", outer["type"])); typ != "" {
			payload["event_type"] = typ
		}
		if inner, ok := outer["payload"].(map[string]any); ok {
			payload["payload"] = inner
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
					if content := envelopeStringField(message, "Content", "content"); content != "" {
						payload["content"] = content
					}
				}
			}
		}
	}
	return payload
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
