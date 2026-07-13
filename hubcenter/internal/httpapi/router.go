package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/auth"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/entry"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/ha"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/hubs"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/mail"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/notification"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skill"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

type EntryResolveRequest struct {
	Email          string `json:"email"`
	PhoneNumber    string `json:"phone_number,omitempty"`
	Domain         string `json:"domain,omitempty"`
	TenantID       string `json:"tenant_id,omitempty"`
	InvitationCode string `json:"invitation_code,omitempty"`
}

// LLMRouteHook is called during router setup to register LLM service routes.
// Set by the application layer after constructing LLM dependencies.
var llmRouteHook func(mux *http.ServeMux, adminService *auth.AdminService, hubService *hubs.Service)
var llmAuthorizationSyncMu sync.RWMutex
var llmAuthorizationSyncChecker *llmservice.AuthorizationChecker

const heartbeatAuthorizationKeyLLMCompute = "llm_compute"

// SetLLMRouteHook sets the hook for registering LLM routes.
func SetLLMRouteHook(hook func(mux *http.ServeMux, adminService *auth.AdminService, hubService *hubs.Service)) {
	llmRouteHook = hook
}

// SetLLMAuthorizationSyncChecker sets the checker used to publish LLM
// authorization state through the generic Hub heartbeat authorization payload.
func SetLLMAuthorizationSyncChecker(checker *llmservice.AuthorizationChecker) {
	llmAuthorizationSyncMu.Lock()
	defer llmAuthorizationSyncMu.Unlock()
	llmAuthorizationSyncChecker = checker
}

func currentLLMAuthorizationSyncChecker() *llmservice.AuthorizationChecker {
	llmAuthorizationSyncMu.RLock()
	defer llmAuthorizationSyncMu.RUnlock()
	return llmAuthorizationSyncChecker
}

type AdminRouteQueryRequest struct {
	Query       string `json:"query"`
	QueryType   string `json:"query_type,omitempty"`
	PhoneNumber string `json:"phone_number,omitempty"`
}

type HubHeartbeatRequest struct {
	HubSecret              string         `json:"hub_secret"`
	InvitationCodeRequired *bool          `json:"invitation_code_required,omitempty"`
	BaseURL                string         `json:"base_url,omitempty"`
	Host                   string         `json:"host,omitempty"`
	Port                   int            `json:"port,omitempty"`
	Visibility             string         `json:"visibility,omitempty"`
	EnrollmentMode         string         `json:"enrollment_mode,omitempty"`
	CorporateEmailDomain   string         `json:"corporate_email_domain,omitempty"`
	CorporateEmailDomains  []string       `json:"corporate_email_domains,omitempty"`
	AcceptPublicSignup     *bool          `json:"accept_public_signup,omitempty"`
	Capabilities           map[string]any `json:"capabilities,omitempty"`
}

type HubUserLinkSyncRequest struct {
	HubSecret  string `json:"hub_secret"`
	TenantID   string `json:"tenant_id,omitempty"`
	Email      string `json:"email"`
	IsDefault  bool   `json:"is_default"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

func RegisterHubHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req hubs.RegisterHubRequest
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		resp, err := service.RegisterHubFromIP(r.Context(), req, clientIPFromRequest(r))
		if err != nil {
			if errors.Is(err, hubs.ErrHubDisabled) {
				writeError(w, http.StatusLocked, "HUB_DISABLED", "Hub has been disabled by Hub Center")
				return
			}
			if errors.Is(err, hubs.ErrEmailBlocked) {
				writeError(w, http.StatusForbidden, "EMAIL_BLOCKED", err.Error())
				return
			}
			if errors.Is(err, hubs.ErrIPBlocked) {
				writeError(w, http.StatusForbidden, "IP_BLOCKED", err.Error())
				return
			}
			if err.Error() == "mail delivery is not configured" {
				writeError(w, http.StatusBadRequest, "MAIL_NOT_CONFIGURED", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "REGISTER_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func HubHeartbeatHandler(service *hubs.Service, haSvcs ...*ha.Service) http.HandlerFunc {
	var haSvc *ha.Service
	if len(haSvcs) > 0 {
		haSvc = haSvcs[0]
	}
	return func(w http.ResponseWriter, r *http.Request) {
		hubID := r.PathValue("id")
		if hubID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_HUB_ID", "Hub id is required")
			return
		}

		var req HubHeartbeatRequest
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}

		var update *hubs.HeartbeatHubUpdate
		if heartbeatHasRegistrationUpdate(req) {
			update = &hubs.HeartbeatHubUpdate{
				BaseURL:               req.BaseURL,
				Host:                  req.Host,
				Port:                  req.Port,
				Visibility:            req.Visibility,
				EnrollmentMode:        req.EnrollmentMode,
				CorporateEmailDomain:  req.CorporateEmailDomain,
				CorporateEmailDomains: req.CorporateEmailDomains,
				AcceptPublicSignup:    req.AcceptPublicSignup,
				Capabilities:          req.Capabilities,
			}
		}
		if err := service.HeartbeatHubWithSecret(r.Context(), hubID, req.HubSecret, req.InvitationCodeRequired, update); err != nil {
			if errors.Is(err, hubs.ErrHubNotReadyOnNode) {
				writeClientAwareError(w, http.StatusConflict, "HUB_NOT_READY_ON_NODE", "Hub metadata is not available on this node yet.", true, true)
				return
			}
			if errors.Is(err, hubs.ErrHubUnauthorized) {
				if haSvc != nil {
					writeClientAwareError(w, http.StatusUnauthorized, "HUB_UNREGISTERED", "Hub is not registered on this node.", true, true)
					return
				}
				writeError(w, http.StatusUnauthorized, "HUB_UNREGISTERED", "Hub is not registered")
				return
			}
			if errors.Is(err, hubs.ErrHubDisabled) {
				writeError(w, http.StatusLocked, "HUB_DISABLED", "Hub has been disabled by Hub Center")
				return
			}
			if errors.Is(err, hubs.ErrHubPendingConfirmation) {
				writeError(w, http.StatusConflict, "HUB_PENDING_CONFIRMATION", "Hub registration is waiting for email confirmation")
				return
			}
			writeError(w, http.StatusInternalServerError, "HEARTBEAT_FAILED", err.Error())
			return
		}
		auths := map[string]*corelib.DigitalEmployeeAuthorization{}
		if loaded, authErr := service.HubDigitalEmployeeAuthorizations(r.Context(), hubID); authErr == nil && loaded != nil {
			auths = loaded
		}
		resp := map[string]any{"ok": true, "status": "online"}
		authPayloads := map[string]any{}
		if auth := auths[""]; auth != nil {
			resp["digital_employee_authorization"] = auth
		}
		allowExternalProviders := service.HubAllowExternalProviders(r.Context(), hubID)
		if len(auths) > 0 {
			tenantAuths := map[string]*corelib.DigitalEmployeeAuthorization{}
			for tenantID, auth := range auths {
				if strings.TrimSpace(tenantID) != "" && auth != nil {
					tenantAuths[tenantID] = auth
				}
			}
			if len(tenantAuths) > 0 {
				resp["digital_employee_authorizations"] = tenantAuths
			}
			authPayloads["digital_employee"] = map[string]any{
				"default": auths[""],
				"tenants": tenantAuths,
			}
		}
		if checker := currentLLMAuthorizationSyncChecker(); checker != nil {
			llmComputeTenants := buildHeartbeatLLMComputeAuthorizationPayload(r.Context(), checker, hubID)
			allowExternalProviders = heartbeatLLMComputeAllowsExternal(llmComputeTenants)
			authPayloads[heartbeatAuthorizationKeyLLMCompute] = map[string]any{
				"tenants": llmComputeTenants,
			}
		}
		// Backward-compatible top-level field for older Hub/UI code. Newer
		// clients also read authorizations.llm_compute per tenant.
		resp["allow_external_providers"] = allowExternalProviders
		if len(authPayloads) > 0 {
			resp["authorizations"] = authPayloads
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func heartbeatLLMComputeAllowsExternal(tenants map[string]*llmservice.TenantAuthorizationStatus) bool {
	for _, status := range tenants {
		if status != nil && status.AllowExternalProviders {
			return true
		}
	}
	return false
}

func buildHeartbeatLLMComputeAuthorizationPayload(ctx context.Context, checker *llmservice.AuthorizationChecker, hubID string) map[string]*llmservice.TenantAuthorizationStatus {
	if checker == nil || strings.TrimSpace(hubID) == "" {
		return map[string]*llmservice.TenantAuthorizationStatus{}
	}
	all, err := checker.ListByHub(ctx, hubID)
	if err != nil {
		return map[string]*llmservice.TenantAuthorizationStatus{}
	}
	tenantIDs := map[string]struct{}{}
	for _, auth := range all {
		if auth == nil {
			continue
		}
		for _, tenantID := range heartbeatLLMComputeTenantKeys(auth.TenantID) {
			tenantIDs[tenantID] = struct{}{}
		}
		if auth.TenantID == "" || auth.TenantID == "default" || auth.TenantID == "tenant_default" {
			tenantIDs["tenant_default"] = struct{}{}
		}
	}
	if len(tenantIDs) == 0 {
		return map[string]*llmservice.TenantAuthorizationStatus{}
	}
	out := map[string]*llmservice.TenantAuthorizationStatus{}
	for tenantID := range tenantIDs {
		responseTenantID := adminExternalTenantID(tenantID)
		status, err := llmservice.BuildTenantAuthorizationStatus(ctx, checker, hubID, responseTenantID)
		if err != nil || status == nil {
			continue
		}
		if !status.AllowExternalProviders && len(status.Authorizations) == 0 {
			continue
		}
		out[responseTenantID] = status
	}
	return out
}

func heartbeatLLMComputeTenantKeys(tenantID string) []string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" || tenantID == "default" || tenantID == "tenant_default" {
		return []string{"tenant_default"}
	}
	out := []string{tenantID}
	if strings.HasPrefix(tenantID, "tenant_") {
		out = append(out, strings.TrimPrefix(tenantID, "tenant_"))
	} else {
		out = append(out, "tenant_"+tenantID)
	}
	return out
}

func heartbeatHasRegistrationUpdate(req HubHeartbeatRequest) bool {
	return strings.TrimSpace(req.BaseURL) != "" ||
		strings.TrimSpace(req.Host) != "" ||
		req.Port != 0 ||
		strings.TrimSpace(req.Visibility) != "" ||
		strings.TrimSpace(req.EnrollmentMode) != "" ||
		strings.TrimSpace(req.CorporateEmailDomain) != "" ||
		len(req.CorporateEmailDomains) > 0 ||
		req.AcceptPublicSignup != nil ||
		req.Capabilities != nil
}

func HubUserLinkSyncHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hubID := r.PathValue("id")
		if hubID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_HUB_ID", "Hub id is required")
			return
		}
		var req HubUserLinkSyncRequest
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		if err := service.SyncHubUserLink(r.Context(), hubID, req.HubSecret, req.Email, req.IsDefault, req.ReplaceAll, req.TenantID); err != nil {
			if errors.Is(err, hubs.ErrHubUnauthorized) {
				writeError(w, http.StatusUnauthorized, "HUB_UNREGISTERED", "Hub is not registered")
				return
			}
			if errors.Is(err, hubs.ErrHubPendingConfirmation) {
				writeError(w, http.StatusConflict, "HUB_PENDING_CONFIRMATION", "Hub registration is waiting for email confirmation")
				return
			}
			if errors.Is(err, hubs.ErrHubDisabled) {
				writeError(w, http.StatusLocked, "HUB_DISABLED", "Hub has been disabled by Hub Center")
				return
			}
			writeError(w, http.StatusInternalServerError, "HUB_USER_LINK_SYNC_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func HubUserLinkDeleteHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hubID := r.PathValue("id")
		if hubID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_HUB_ID", "Hub id is required")
			return
		}
		var req HubUserLinkSyncRequest
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		if err := service.DeleteHubUserLink(r.Context(), hubID, req.HubSecret, req.Email, req.TenantID); err != nil {
			if errors.Is(err, hubs.ErrHubUnauthorized) {
				writeError(w, http.StatusUnauthorized, "HUB_UNREGISTERED", "Hub is not registered")
				return
			}
			if errors.Is(err, hubs.ErrHubPendingConfirmation) {
				writeError(w, http.StatusConflict, "HUB_PENDING_CONFIRMATION", "Hub registration is waiting for email confirmation")
				return
			}
			if errors.Is(err, hubs.ErrHubDisabled) {
				writeError(w, http.StatusForbidden, "HUB_DISABLED", "Hub is disabled")
				return
			}
			writeError(w, http.StatusBadRequest, "HUB_USER_LINK_DELETE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

type HubInvitationCodeSyncRequest struct {
	HubSecret   string   `json:"hub_secret"`
	Codes       []string `json:"codes"`
	TenantID    string   `json:"tenant_id"`
	UsedByEmail string   `json:"used_by_email,omitempty"`
}

func HubInvitationCodeSyncHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hubID := r.PathValue("id")
		if hubID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_HUB_ID", "Hub id is required")
			return
		}
		var req HubInvitationCodeSyncRequest
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		if err := service.SyncInvitationCodes(r.Context(), hubID, req.HubSecret, req.Codes, req.TenantID); err != nil {
			if errors.Is(err, hubs.ErrHubUnauthorized) {
				writeError(w, http.StatusUnauthorized, "HUB_UNREGISTERED", "Hub is not registered")
				return
			}
			writeError(w, http.StatusInternalServerError, "INVITATION_CODE_SYNC_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "synced": len(req.Codes)})
	}
}

func HubInvitationCodeDeleteHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hubID := r.PathValue("id")
		if hubID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_HUB_ID", "Hub id is required")
			return
		}
		var req HubInvitationCodeSyncRequest
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		if err := service.DeleteInvitationCodes(r.Context(), hubID, req.HubSecret, req.Codes, req.UsedByEmail); err != nil {
			if errors.Is(err, hubs.ErrHubUnauthorized) {
				writeError(w, http.StatusUnauthorized, "HUB_UNREGISTERED", "Hub is not registered")
				return
			}
			writeError(w, http.StatusInternalServerError, "INVITATION_CODE_DELETE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func ConfirmHubRegistrationHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if err := service.ConfirmRegistration(r.Context(), token); err != nil {
			status := http.StatusBadRequest
			message := "Hub registration confirmation is invalid or expired."
			if !errors.Is(err, hubs.ErrInvalidConfirmationToken) {
				status = http.StatusInternalServerError
				message = fmt.Sprintf("Hub registration confirmation failed: %v", err)
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(status)
			_, _ = w.Write([]byte("<!doctype html><html><head><meta charset=\"utf-8\"><title>Hub Registration</title></head><body style=\"font-family:Segoe UI,sans-serif;padding:32px;background:#f5f9ff;color:#18314f\"><div style=\"max-width:640px;margin:0 auto;background:#fff;border:1px solid rgba(24,49,79,.08);border-radius:18px;padding:28px;box-shadow:0 12px 30px rgba(24,49,79,.08)\"><h1 style=\"margin:0 0 12px\">Hub registration confirmation failed</h1><p style=\"line-height:1.7\">" + message + "</p></div></body></html>"))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><html><head><meta charset=\"utf-8\"><title>Hub Registration Confirmed</title></head><body style=\"font-family:Segoe UI,sans-serif;padding:32px;background:#f5f9ff;color:#18314f\"><div style=\"max-width:640px;margin:0 auto;background:#fff;border:1px solid rgba(24,49,79,.08);border-radius:18px;padding:28px;box-shadow:0 12px 30px rgba(24,49,79,.08)\"><h1 style=\"margin:0 0 12px\">Hub registration confirmed</h1><p style=\"line-height:1.7\">The Hub is now activated in Hub Center. You can return to the admin console and refresh the status.</p></div></body></html>"))
	}
}

func EntryResolveHandler(service *entry.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req EntryResolveRequest
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		if strings.TrimSpace(req.PhoneNumber) != "" && strings.TrimSpace(req.Email) == "" && normalizeEntryResolvePhoneIdentity(req.PhoneNumber) == "" {
			writeError(w, http.StatusBadRequest, "INVALID_PHONE_NUMBER", "Invalid phone number")
			return
		}
		identity := entryResolveIdentity(req)
		resp, err := service.ResolveByEmailFromIP(r.Context(), identity, clientIPFromRequest(r), req.InvitationCode)
		if err != nil {
			if errors.Is(err, entry.ErrIPBlocked) {
				writeError(w, http.StatusForbidden, "IP_BLOCKED", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "ENTRY_RESOLVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func entryResolveIdentity(req EntryResolveRequest) string {
	if phone := normalizeEntryResolvePhoneIdentity(req.PhoneNumber); phone != "" {
		return phone
	}
	if phone := normalizeEntryResolvePhoneIdentity(req.Email); phone != "" {
		return phone
	}
	return strings.TrimSpace(req.Email)
}

func normalizeEntryResolvePhoneIdentity(phoneNumber string) string {
	phoneNumber = strings.TrimSpace(phoneNumber)
	phoneNumber = strings.TrimPrefix(strings.ToLower(phoneNumber), "phone:")
	if phoneNumber == "" || strings.Contains(phoneNumber, "@") {
		return ""
	}
	var b strings.Builder
	for _, r := range phoneNumber {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' || r == '-' || r == '.' || r == '(' || r == ')' || r == ' ' || r == '\t':
			continue
		default:
			return ""
		}
	}
	if b.Len() < 6 {
		return ""
	}
	return "phone:" + b.String()
}

func EntryResolveDomainHandler(service *entry.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req EntryResolveRequest
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		domain := strings.TrimSpace(req.Domain)
		if domain == "" && strings.Contains(req.Email, "@") {
			_, domain, _ = strings.Cut(strings.TrimSpace(req.Email), "@")
		}
		resp, err := service.ResolveByDomain(r.Context(), domain)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ENTRY_DOMAIN_RESOLVE_FAILED", err.Error())
			return
		}
		filterResolveResultByTenant(resp, req.TenantID)
		writeJSON(w, http.StatusOK, resp)
	}
}

func filterResolveResultByTenant(resp *entry.ResolveResult, tenantID string) {
	if resp == nil || len(resp.Hubs) == 0 {
		return
	}
	tenantID = normalizeHubSyncTenantID(tenantID)
	exact := make([]entry.HubAccessView, 0, len(resp.Hubs))
	global := make([]entry.HubAccessView, 0, len(resp.Hubs))
	for _, item := range resp.Hubs {
		itemTenantID := normalizeHubSyncTenantID(item.TenantID)
		if itemTenantID != "" && itemTenantID == tenantID {
			exact = append(exact, item)
			continue
		}
		if itemTenantID == "" {
			global = append(global, item)
		}
	}
	filtered := global
	if len(exact) > 0 {
		filtered = exact
	}
	if len(filtered) == 0 {
		resp.Mode = "none"
		resp.DefaultHubID = ""
		resp.DefaultPWA = ""
		resp.Hubs = nil
		resp.Message = "No domain route found"
		return
	}
	resp.Hubs = filtered
	resp.DefaultHubID = filtered[0].HubID
	resp.DefaultPWA = filtered[0].PWAURL
	resp.Message = ""
	if len(filtered) == 1 {
		resp.Mode = "single"
	} else {
		resp.Mode = "multiple"
	}
}

func normalizeHubSyncTenantID(tenantID string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "tenant_default" {
		return ""
	}
	return tenantID
}

func AdminRouteQueryHandler(service *entry.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req AdminRouteQueryRequest
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		query := normalizeAdminRouteQuery(req.Query)
		queryType := strings.TrimSpace(strings.ToLower(req.QueryType))
		if strings.TrimSpace(req.PhoneNumber) != "" {
			queryType = "phone"
			query = strings.TrimSpace(req.PhoneNumber)
		}
		var (
			resp *entry.ResolveResult
			err  error
		)
		switch queryType {
		case "domain":
			resp, err = service.ResolveAdminByDomain(r.Context(), query)
		case "phone":
			phone := normalizeEntryResolvePhoneIdentity(query)
			if phone == "" {
				writeError(w, http.StatusBadRequest, "INVALID_PHONE_NUMBER", "Invalid phone number")
				return
			}
			query = phone
			resp, err = service.ResolveAdminByEmail(r.Context(), query)
		default:
			query = normalizeAdminRouteIdentity(query)
			resp, err = service.ResolveAdminByEmail(r.Context(), query)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ADMIN_ROUTE_QUERY_FAILED", err.Error())
			return
		}
		externalizeAdminResolveResult(resp)
		writeJSON(w, http.StatusOK, resp)
	}
}

func normalizeAdminRouteQuery(query string) string {
	return strings.TrimSpace(strings.ToLower(query))
}

func normalizeAdminRouteIdentity(query string) string {
	return normalizeAdminRoutePhoneQuery(normalizeAdminRouteQuery(query))
}

func normalizeAdminRoutePhoneQuery(query string) string {
	if !isAdminRoutePhoneQueryCandidate(query) {
		return query
	}
	if phone := normalizeEntryResolvePhoneIdentity(query); phone != "" {
		return phone
	}
	return query
}

func isAdminRoutePhoneQueryCandidate(query string) bool {
	query = strings.TrimSpace(strings.ToLower(query))
	if strings.HasPrefix(query, "phone:") {
		return true
	}
	if strings.ContainsAny(query, "@.") {
		return false
	}
	hasDigit := false
	for _, r := range query {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case r == '+' || r == '-' || r == ' ' || r == '\t' || r == '(' || r == ')':
		default:
			return false
		}
	}
	return hasDigit
}

func AdminInvitationCodeQueryHandler(service *entry.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Code string `json:"code"`
		}
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		code := strings.TrimSpace(strings.ToUpper(req.Code))
		if code == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Invitation code is required")
			return
		}
		result, err := service.LookupInvitationCodeRoute(r.Context(), code)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INVITATION_CODE_QUERY_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func sameURLOrigin(a, b string) bool {
	left, err := url.Parse(strings.TrimSpace(a))
	if err != nil || left.Scheme == "" || left.Host == "" {
		return false
	}
	right, err := url.Parse(strings.TrimSpace(b))
	if err != nil || right.Scheme == "" || right.Host == "" {
		return false
	}
	return strings.EqualFold(left.Scheme, right.Scheme) &&
		strings.EqualFold(left.Hostname(), right.Hostname()) &&
		effectiveURLPort(left) == effectiveURLPort(right)
}

func isHTTPSURLOrigin(raw string) bool {
	uri, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && strings.EqualFold(uri.Scheme, "https") && uri.Host != ""
}

func effectiveURLPort(uri *url.URL) string {
	if port := uri.Port(); port != "" {
		return port
	}
	switch strings.ToLower(uri.Scheme) {
	case "http", "ws":
		return "80"
	case "https", "wss":
		return "443"
	default:
		return ""
	}
}

func externalizeAdminResolveResult(resp *entry.ResolveResult) {
	if resp == nil {
		return
	}
	for i := range resp.Hubs {
		resp.Hubs[i].TenantID = adminExternalTenantID(resp.Hubs[i].TenantID)
	}
}

func NewRouter(adminService *auth.AdminService, hubService *hubs.Service, entryService *entry.Service, mailer *mail.Service, skillStore *skill.SkillStore, failureLogs store.FailureEventLogRepository, gossipRepo store.GossipRepository, gossipCache *GossipCache, smHandlers *SkillMarketHandlers, systemSettings store.SystemSettingsRepository, newsRepo store.NewsRepository, haConfigSvc *ha.ConfigService, optionalSvcs ...any) http.Handler {
	var haSvc *ha.Service
	var userUsageRepo store.HubUserUsageRepository
	var notifService *notification.Service
	for _, svc := range optionalSvcs {
		switch v := svc.(type) {
		case *ha.Service:
			if haSvc == nil {
				haSvc = v
			}
		case store.HubUserUsageRepository:
			userUsageRepo = v
		case *notification.Service:
			notifService = v
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", HealthHandler("MaClaw-hubcenter"))
	mux.HandleFunc("GET /api/admin/status", AdminStatusHandler(adminService))
	mux.HandleFunc("POST /api/admin/setup", SetupAdminHandler(adminService))
	mux.HandleFunc("POST /api/admin/login", AdminLoginHandler(adminService))
	mux.HandleFunc("POST /api/admin/password", RequireAdmin(adminService, AdminChangePasswordHandler(adminService)))
	mux.HandleFunc("POST /api/admin/profile", RequireAdmin(adminService, AdminUpdateProfileHandler(adminService)))
	mux.HandleFunc("GET /api/admin/failure-logs", RequireAdmin(adminService, ListFailureLogsHandler(failureLogs)))
	mux.HandleFunc("GET /api/admin/routing/diagnostics", RequireAdmin(adminService, AdminRoutingDiagnosticsHandler(entryService)))
	mux.HandleFunc("POST /api/admin/routing/query", RequireAdmin(adminService, AdminRouteQueryHandler(entryService)))
	mux.HandleFunc("POST /api/admin/routing/invitation-code-query", RequireAdmin(adminService, AdminInvitationCodeQueryHandler(entryService)))
	mux.HandleFunc("POST /api/admin/routing/delete-email-route", RequireAdmin(adminService, AdminDeleteEmailRouteHandler(hubService)))
	mux.HandleFunc("POST /api/admin/routing/restore-email-route", RequireAdmin(adminService, AdminRestoreEmailRouteHandler(hubService)))
	mux.HandleFunc("POST /api/admin/routing/verify-email-route", RequireAdmin(adminService, AdminVerifyEmailRouteHandler(hubService)))
	mux.HandleFunc("GET /api/admin/server/config", RequireAdmin(adminService, GetAdminServerConfigHandler(hubService)))
	mux.HandleFunc("POST /api/admin/server/config", RequireAdmin(adminService, UpdateAdminServerConfigHandler(hubService)))
	mux.HandleFunc("GET /api/admin/ha/status", RequireAdmin(adminService, AdminHAStatusHandler(haSvc)))
	mux.HandleFunc("GET /api/admin/ha/config", RequireAdmin(adminService, GetHAConfigHandler(haConfigSvc, haSvc)))
	mux.HandleFunc("POST /api/admin/ha/config", RequireAdmin(adminService, UpdateHAConfigHandler(haConfigSvc, haSvc)))
	mux.HandleFunc("POST /api/admin/ha/skillhub/broadcast", RequireAdmin(adminService, AdminHABroadcastSkillHubHandler(haSvc)))
	mux.HandleFunc("POST /api/admin/ha/skillmarket/broadcast", RequireAdmin(adminService, AdminHABroadcastSkillMarketHandler(haSvc)))
	mux.HandleFunc("GET /api/admin/ha/public-key", RequireAdmin(adminService, HAKeyMaterialHandler(haConfigSvc, haSvc)))
	mux.HandleFunc("GET /api/admin/ha/public-keys", RequireAdmin(adminService, HACollectedPublicKeysHandler(haConfigSvc, haSvc, haSvc)))
	mux.HandleFunc("GET /api/admin/mail/config", RequireAdmin(adminService, GetMailConfigHandler(mailer)))
	mux.HandleFunc("POST /api/admin/mail/config", RequireAdmin(adminService, UpdateMailConfigHandler(mailer)))
	mux.HandleFunc("GET /api/admin/hubs", RequireAdmin(adminService, ListHubsHandler(hubService)))
	mux.HandleFunc("GET /api/admin/routing/enterprise-mail-domains", RequireAdmin(adminService, ListEnterpriseMailDomainsHandler(hubService)))
	mux.HandleFunc("GET /api/admin/hubs/runtime", RequireAdmin(adminService, ListHubRuntimeStatusesHandler(hubService)))
	mux.HandleFunc("GET /api/admin/users/dashboard", RequireAdmin(adminService, ListUserDashboardHandler(hubService)))
	mux.HandleFunc("GET /api/admin/user-rankings", RequireAdmin(adminService, CenterUserRankingsHandler(userUsageRepo)))
	mux.HandleFunc("POST /api/admin/hubs/name", RequireAdmin(adminService, UpdateHubNameHandler(hubService)))
	mux.HandleFunc("POST /api/admin/hubs/{id}/name", RequireAdmin(adminService, UpdateHubNameHandler(hubService)))
	mux.HandleFunc("POST /api/admin/hubs/visibility", RequireAdmin(adminService, UpdateHubVisibilityHandler(hubService)))
	mux.HandleFunc("POST /api/admin/hubs/{id}/visibility", RequireAdmin(adminService, UpdateHubVisibilityHandler(hubService)))
	mux.HandleFunc("POST /api/admin/hubs/registration-policy", RequireAdmin(adminService, UpdateHubRegistrationPolicyHandler(hubService)))
	mux.HandleFunc("POST /api/admin/hubs/{id}/registration-policy", RequireAdmin(adminService, UpdateHubRegistrationPolicyHandler(hubService)))
	mux.HandleFunc("POST /api/admin/hubs/digital-employee-authorization", RequireAdmin(adminService, UpdateDigitalEmployeeAuthorizationHandler(hubService)))
	mux.HandleFunc("POST /api/admin/hubs/{id}/digital-employee-authorization", RequireAdmin(adminService, UpdateDigitalEmployeeAuthorizationHandler(hubService)))
	mux.HandleFunc("POST /api/admin/hubs/disable", RequireAdmin(adminService, DisableHubHandler(hubService)))
	mux.HandleFunc("POST /api/admin/hubs/{id}/disable", RequireAdmin(adminService, DisableHubHandler(hubService)))
	mux.HandleFunc("POST /api/admin/hubs/enable", RequireAdmin(adminService, EnableHubHandler(hubService)))
	mux.HandleFunc("POST /api/admin/hubs/{id}/enable", RequireAdmin(adminService, EnableHubHandler(hubService)))
	mux.HandleFunc("POST /api/admin/hubs/confirm", RequireAdmin(adminService, ConfirmHubHandler(hubService)))
	mux.HandleFunc("POST /api/admin/hubs/{id}/confirm", RequireAdmin(adminService, ConfirmHubHandler(hubService)))
	mux.HandleFunc("DELETE /api/admin/hubs", RequireAdmin(adminService, DeleteHubHandler(hubService)))
	mux.HandleFunc("DELETE /api/admin/hubs/{id}", RequireAdmin(adminService, DeleteHubHandler(hubService)))
	mux.HandleFunc("POST /api/admin/users/refresh-inventory", RequireAdmin(adminService, RefreshHubUserInventoryHandler(hubService)))
	mux.HandleFunc("POST /api/admin/users/migrate", RequireAdmin(adminService, MigrateHubUserHandler(hubService)))
	mux.HandleFunc("GET /api/admin/blocked-emails", RequireAdmin(adminService, ListBlockedEmailsHandler(hubService)))
	mux.HandleFunc("POST /api/admin/blocked-emails", RequireAdmin(adminService, AddBlockedEmailHandler(hubService)))
	mux.HandleFunc("DELETE /api/admin/blocked-emails/{email}", RequireAdmin(adminService, RemoveBlockedEmailHandler(hubService)))
	mux.HandleFunc("GET /api/admin/blocked-ips", RequireAdmin(adminService, ListBlockedIPsHandler(hubService)))
	mux.HandleFunc("POST /api/admin/blocked-ips", RequireAdmin(adminService, AddBlockedIPHandler(hubService)))
	mux.HandleFunc("DELETE /api/admin/blocked-ips/{ip}", RequireAdmin(adminService, RemoveBlockedIPHandler(hubService)))
	mux.HandleFunc("POST /api/admin/mail/test", RequireAdmin(adminService, AdminSendTestMailHandler(mailer)))
	mux.HandleFunc("POST /api/hubs/register", RegisterHubHandler(hubService))
	mux.HandleFunc("POST /api/hubs/{id}/heartbeat", HubHeartbeatHandler(hubService, haSvc))
	mux.HandleFunc("POST /api/hubs/{id}/user-links/sync", HubUserLinkSyncHandler(hubService))
	mux.HandleFunc("POST /api/hubs/{id}/user-usage/sync", HubUserUsageSyncHandler(hubService, userUsageRepo))
	mux.HandleFunc("DELETE /api/hubs/{id}/user-links/sync", HubUserLinkDeleteHandler(hubService))
	mux.HandleFunc("POST /api/hubs/{id}/invitation-codes/sync", HubInvitationCodeSyncHandler(hubService))
	mux.HandleFunc("DELETE /api/hubs/{id}/invitation-codes/sync", HubInvitationCodeDeleteHandler(hubService))
	mux.HandleFunc("GET /hub-registration/confirm", ConfirmHubRegistrationHandler(hubService))
	mux.HandleFunc("POST /api/entry/resolve", EntryResolveHandler(entryService))
	mux.HandleFunc("POST /api/entry/resolve-domain", EntryResolveDomainHandler(entryService))
	mux.HandleFunc("GET /api/client/quality", ClientQualityHandler(haSvc))
	mux.HandleFunc("GET /api/client/endpoints", ClientEndpointsHandler(haSvc))
	mux.HandleFunc("GET /api/client/hubcenters", ClientHubCentersHandler(haConfigSvc))
	if haSvc != nil {
		mux.HandleFunc("GET /api/internal/ha/ops", HAOpsPullHandler(haSvc))
		mux.HandleFunc("POST /api/internal/ha/ops/apply", HAOpsApplyHandler(haSvc))
		mux.HandleFunc("GET /api/internal/ha/public-key", HAInternalKeyMaterialHandler(haConfigSvc, haSvc, haSvc))
	}
	// Skill Catalog API
	var searchRemover skillSearchRemover
	if smHandlers != nil {
		searchRemover = smHandlers.SearchService()
	}
	skillHandlers := NewSkillHandlers(skillStore, searchRemover)
	mux.HandleFunc("GET /api/v1/skills/search", skillHandlers.SearchSkills)
	mux.HandleFunc("GET /api/v1/skills/{id}", skillHandlers.GetSkill)
	mux.HandleFunc("GET /api/v1/skills/{id}/download", skillHandlers.DownloadSkill)
	mux.HandleFunc("GET /api/v1/skills/by-skill-id/{skill_id}/download", skillHandlers.DownloadBySkillID)
	mux.HandleFunc("GET /api/v1/skills/popular", skillHandlers.PopularSkills)
	mux.HandleFunc("POST /api/v1/skills", skillHandlers.PublishSkill)
	mux.HandleFunc("POST /api/v1/skills/{id}/rate", skillHandlers.RateSkill)
	// SkillHub admin management
	mux.HandleFunc("GET /api/admin/skillhub/list", RequireAdmin(adminService, skillHandlers.AdminListSkills))
	mux.HandleFunc("GET /api/admin/capability-market/external-search", RequireAdmin(adminService, AdminCapabilityMarketExternalSearchHandler()))
	mux.HandleFunc("POST /api/admin/skillhub/visibility", RequireAdmin(adminService, skillHandlers.AdminSetVisibility))
	mux.HandleFunc("POST /api/admin/skillhub/trust-level", RequireAdmin(adminService, skillHandlers.AdminSetTrustLevel))
	mux.HandleFunc("DELETE /api/admin/skillhub/{id}", RequireAdmin(adminService, skillHandlers.AdminDeleteSkill))
	mux.HandleFunc("POST /api/admin/skillhub/import-url", RequireAdmin(adminService, skillHandlers.AdminImportFromURL))
	// Gossip - anonymous gossip board
	gossipWriteRL := newGossipRateLimiter(10, 10*time.Minute) // 10 writes per 10 min per key
	mux.HandleFunc("POST /api/gossip/publish", gossipRateLimitMiddleware(gossipWriteRL, GossipPublishHandler(gossipRepo, gossipCache, systemSettings)))
	mux.HandleFunc("GET /api/gossip/browse", GossipBrowseHandler(gossipRepo))
	mux.HandleFunc("POST /api/gossip/comment", gossipRateLimitMiddleware(gossipWriteRL, GossipCommentHandler(gossipRepo, gossipCache)))
	mux.HandleFunc("POST /api/gossip/rate", gossipRateLimitMiddleware(gossipWriteRL, GossipRateHandler(gossipRepo, gossipCache)))
	mux.HandleFunc("GET /api/gossip/comments", GossipCommentsListHandler(gossipRepo))
	mux.HandleFunc("GET /api/gossip/snapshot", GossipSnapshotHandler(gossipCache))
	mux.HandleFunc("OPTIONS /api/gossip/snapshot", GossipSnapshotHandler(gossipCache))
	// Gossip admin management
	mux.HandleFunc("GET /api/admin/gossip", RequireAdmin(adminService, AdminListGossipHandler(gossipRepo)))
	mux.HandleFunc("DELETE /api/admin/gossip", RequireAdmin(adminService, AdminDeleteGossipHandler(gossipRepo, gossipCache)))
	mux.HandleFunc("DELETE /api/admin/gossip/flagged", RequireAdmin(adminService, AdminDeleteFlaggedGossipHandler(gossipRepo, gossipCache)))
	mux.HandleFunc("POST /api/admin/gossip/lock", RequireAdmin(adminService, AdminLockGossipHandler(gossipRepo, gossipCache)))
	mux.HandleFunc("GET /api/admin/gossip/comments", RequireAdmin(adminService, AdminListGossipCommentsHandler(gossipRepo)))
	mux.HandleFunc("DELETE /api/admin/gossip/comments", RequireAdmin(adminService, AdminDeleteGossipCommentHandler(gossipRepo, gossipCache)))
	// Gossip moderation (LLM)
	mux.HandleFunc("POST /api/admin/gossip/flag", RequireAdmin(adminService, AdminFlagGossipHandler(gossipRepo, gossipCache)))
	mux.HandleFunc("GET /api/admin/moderation/config", RequireAdmin(adminService, GetModerationConfigHandler(systemSettings)))
	mux.HandleFunc("POST /api/admin/moderation/config", RequireAdmin(adminService, UpdateModerationConfigHandler(systemSettings)))
	mux.HandleFunc("POST /api/admin/moderation/test", RequireAdmin(adminService, TestModerationHandler(systemSettings)))
	registerSharedStaticAssets(mux, "./web")
	registerAdminStaticRoutes(mux, "./web/admin", "/admin")
	registerStaticRoutes(mux, "./web/skillhub", "/skillhub")
	registerStaticRoutes(mux, "./web/skillmarket", "/skillmarket")
	registerStaticRoutes(mux, "./web/skillmarket", "/marketplace")
	registerStaticRoutes(mux, "./web/skillmarket", "/capabilitymarket")
	mux.HandleFunc("GET /api/capability-market/customer-account", CapabilityMarketCustomerAccountHandler(systemSettings))
	mux.HandleFunc("GET /api/capability-market/billing/licenses", CapabilityMarketBillingLicensesHandler(systemSettings, smHandlers))
	mux.HandleFunc("GET /api/capability-market/mcp", CapabilityMarketMCPListHandler(systemSettings))
	mux.HandleFunc("GET /api/capability-market/mcp/{id}", CapabilityMarketMCPDetailHandler(systemSettings))
	mux.HandleFunc("POST /api/capability-market/mcp/{id}/purchase", CapabilityMarketMCPPurchaseHandler(systemSettings))
	mux.HandleFunc("POST /api/admin/capability-market/mcp", RequireAdmin(adminService, AdminCapabilityMarketMCPUpsertHandler(systemSettings)))
	mux.HandleFunc("PUT /api/admin/capability-market/mcp/{id}", RequireAdmin(adminService, AdminCapabilityMarketMCPUpsertHandler(systemSettings)))
	mux.HandleFunc("DELETE /api/admin/capability-market/mcp/{id}", RequireAdmin(adminService, AdminCapabilityMarketMCPDeleteHandler(systemSettings)))
	mux.HandleFunc("GET /api/admin/capabilities/external-search", RequireAdmin(adminService, AdminCapabilityMarketExternalSearchHandler()))
	mux.HandleFunc("POST /api/admin/capability-market/mcp/validate", RequireAdmin(adminService, AdminMCPValidateHandler()))
	mux.HandleFunc("POST /api/admin/capability-market/import", RequireAdmin(adminService, AdminCapabilityMarketImportHandler(systemSettings, skillStore)))
	registerStaticRoutes(mux, "./web/gossip", "/gossip")
	// News - public API for latest announcements
	mux.HandleFunc("GET /api/news", NewsLatestHandler(newsRepo))
	mux.HandleFunc("OPTIONS /api/news", NewsLatestHandler(newsRepo))
	// News - admin management
	mux.HandleFunc("GET /api/admin/news", RequireAdmin(adminService, AdminListNewsHandler(newsRepo)))
	mux.HandleFunc("POST /api/admin/news", RequireAdmin(adminService, AdminCreateNewsHandler(newsRepo, haSvc)))
	mux.HandleFunc("PUT /api/admin/news", RequireAdmin(adminService, AdminUpdateNewsHandler(newsRepo, haSvc)))
	mux.HandleFunc("DELETE /api/admin/news", RequireAdmin(adminService, AdminDeleteNewsHandler(newsRepo, haSvc)))
	// Notification - admin management (HubCenter cross-Hub notifications)
	if notifService != nil {
		notifHandlers := NewNotificationHandlers(notifService)
		mux.HandleFunc("POST /api/v1/admin/notifications", RequireAdmin(adminService, notifHandlers.CreateNotification))
		mux.HandleFunc("GET /api/v1/admin/notifications", RequireAdmin(adminService, notifHandlers.ListNotifications))
		mux.HandleFunc("GET /api/v1/admin/notifications/{id}", RequireAdmin(adminService, notifHandlers.GetNotification))
		mux.HandleFunc("POST /api/v1/admin/notifications/{id}/revoke", RequireAdmin(adminService, notifHandlers.RevokeNotification))
		mux.HandleFunc("DELETE /api/v1/admin/notifications/{id}", RequireAdmin(adminService, notifHandlers.DeleteNotification))
	}
	// SkillMarket API
	if smHandlers != nil {
		// Auth rate limiters
		authLoginRL := newGossipRateLimiter(10, 5*time.Minute)    // 10 login attempts per 5 min per IP
		authRegisterRL := newGossipRateLimiter(5, 10*time.Minute) // 5 registrations per 10 min per IP
		authLookupRL := newGossipRateLimiter(5, 10*time.Minute)   // 5 lookup emails per 10 min per IP
		// Auth endpoints (no session required)
		mux.HandleFunc("POST /api/v1/auth/register", gossipRateLimitMiddleware(authRegisterRL, smHandlers.Register))
		mux.HandleFunc("GET /api/v1/auth/activate", smHandlers.Activate)
		mux.HandleFunc("POST /api/v1/auth/login", gossipRateLimitMiddleware(authLoginRL, smHandlers.Login))
		mux.HandleFunc("POST /api/v1/auth/logout", smHandlers.Logout)
		mux.HandleFunc("POST /api/v1/auth/machine-login", gossipRateLimitMiddleware(authLoginRL, smHandlers.MachineLogin))
		mux.HandleFunc("POST /api/v1/auth/lookup", gossipRateLimitMiddleware(authLookupRL, smHandlers.SendLookupVerification))
		mux.HandleFunc("GET /api/v1/auth/verify-identity", smHandlers.VerifyIdentity)
		mux.HandleFunc("GET /api/v1/auth/session", smHandlers.ValidateSession)
		mux.HandleFunc("GET /api/v1/auth/me", smHandlers.CurrentUser)
		mux.HandleFunc("POST /api/v1/auth/change-password", smHandlers.ChangePassword)
		mux.HandleFunc("POST /api/v1/auth/resend-activation", gossipRateLimitMiddleware(authLookupRL, smHandlers.ResendActivation))
		mux.HandleFunc("POST /api/v1/auth/forgot-password", gossipRateLimitMiddleware(authLookupRL, smHandlers.SendPasswordReset))
		mux.HandleFunc("POST /api/v1/auth/reset-password", smHandlers.ResetPassword)
		// Existing endpoints
		mux.HandleFunc("POST /api/v1/skills/submit", smHandlers.SubmitSkill)
		mux.HandleFunc("GET /api/v1/skill-submissions/{id}", smHandlers.GetSubmissionStatus)
		mux.HandleFunc("POST /api/v1/account/ensure", smHandlers.EnsureAccount)
		mux.HandleFunc("GET /api/v1/account/{email}", smHandlers.GetAccount)
		mux.HandleFunc("POST /api/v1/account/verify", smHandlers.VerifyAccount)
		mux.HandleFunc("GET /api/v1/credits/balance", smHandlers.GetCreditsBalance)
		mux.HandleFunc("GET /api/v1/credits/transactions", smHandlers.GetCreditsTransactions)
		mux.HandleFunc("POST /api/v1/credits/topup", smHandlers.TopUpCredits)
		mux.HandleFunc("POST /api/v1/credits/withdraw", smHandlers.WithdrawCredits)
		mux.HandleFunc("GET /api/v1/crypto/pubkey", smHandlers.GetPublicKey)
		mux.HandleFunc("GET /api/v1/skillmarket/{id}/download", smHandlers.DownloadSkillMarket)
		mux.HandleFunc("GET /api/capability-market/capabilities/{id}/download", smHandlers.DownloadSkillMarket)
		// Rating & Trial API
		mux.HandleFunc("GET /api/v1/skillmarket/search", smHandlers.SearchSkillMarket)
		mux.HandleFunc("GET /api/capability-market/search", smHandlers.SearchSkillMarket)
		mux.HandleFunc("GET /api/v1/skillmarket/my-skills", smHandlers.ListMySkills)
		mux.HandleFunc("GET /api/v1/skillmarket/top", smHandlers.GetLeaderboard)
		mux.HandleFunc("POST /api/v1/skillmarket/{id}/rate", smHandlers.RateSkill)
		mux.HandleFunc("GET /api/v1/skillmarket/{id}/ratings", smHandlers.GetRatingStats)
		// Admin review & config
		mux.HandleFunc("GET /api/v1/admin/skillmarket/review", RequireAdmin(adminService, smHandlers.AdminReviewQueue))
		mux.HandleFunc("POST /api/v1/admin/skillmarket/{id}/approve", RequireAdmin(adminService, smHandlers.AdminApproveSkill))
		mux.HandleFunc("POST /api/v1/admin/skillmarket/{id}/reject", RequireAdmin(adminService, smHandlers.AdminRejectSkill))
		mux.HandleFunc("GET /api/v1/admin/config/trial", RequireAdmin(adminService, smHandlers.GetTrialConfig))
		mux.HandleFunc("PUT /api/v1/admin/config/trial", RequireAdmin(adminService, smHandlers.UpdateTrialConfig))
		mux.HandleFunc("GET /api/v1/admin/config/upload-auth", RequireAdmin(adminService, smHandlers.GetUploadAuthConfig))
		mux.HandleFunc("PUT /api/v1/admin/config/upload-auth", RequireAdmin(adminService, smHandlers.UpdateUploadAuthConfig))
		// API Key management
		mux.HandleFunc("POST /api/v1/skillmarket/{id}/apikeys/upload", smHandlers.UploadAPIKeys)
		mux.HandleFunc("GET /api/v1/skillmarket/{id}/apikeys/status", smHandlers.GetAPIKeyStatus)
		mux.HandleFunc("POST /api/v1/skillmarket/{id}/withdraw", smHandlers.WithdrawSkill)
		mux.HandleFunc("GET /api/v1/account/{email}/tier", smHandlers.GetAccountTier)
		// Admin refund & purchases
		mux.HandleFunc("POST /api/v1/admin/refund", RequireAdmin(adminService, smHandlers.AdminRefund))
		mux.HandleFunc("GET /api/v1/admin/purchases", RequireAdmin(adminService, smHandlers.AdminListPurchases))
		// Skill ID ownership management
		mux.HandleFunc("GET /api/v1/admin/skill-ownership/{skill_id}", RequireAdmin(adminService, smHandlers.GetSkillIDOwnership))
		mux.HandleFunc("POST /api/v1/admin/skill-ownership/transfer", RequireAdmin(adminService, smHandlers.TransferSkillIDOwnership))
		mux.HandleFunc("GET /api/v1/account/skill-ids", smHandlers.ListMySkillIDs)
	}

	// LLM Service routes (providers, proxy, card store) — registered via external call.
	// Caller (app init) should call RegisterLLMRoutes(mux, ...) before this point
	// if the LLM service module is initialized. We expose the mux via a hook here
	// so the application layer can register LLM routes after constructing dependencies.
	if llmRouteHook != nil {
		llmRouteHook(mux, adminService, hubService)
	}

	return adminOpaqueHubIDCompat(mux, adminService, hubService)
}

func adminOpaqueHubIDCompat(next http.Handler, adminService *auth.AdminService, hubService *hubs.Service) http.Handler {
	routes := []struct {
		method  string
		suffix  string
		handler http.HandlerFunc
	}{
		{http.MethodPost, "/name", RequireAdmin(adminService, UpdateHubNameHandler(hubService))},
		{http.MethodPost, "/registration-policy", RequireAdmin(adminService, UpdateHubRegistrationPolicyHandler(hubService))},
		{http.MethodPost, "/visibility", RequireAdmin(adminService, UpdateHubVisibilityHandler(hubService))},
		{http.MethodPost, "/digital-employee-authorization", RequireAdmin(adminService, UpdateDigitalEmployeeAuthorizationHandler(hubService))},
		{http.MethodPost, "/disable", RequireAdmin(adminService, DisableHubHandler(hubService))},
		{http.MethodPost, "/enable", RequireAdmin(adminService, EnableHubHandler(hubService))},
		{http.MethodPost, "/confirm", RequireAdmin(adminService, ConfirmHubHandler(hubService))},
	}
	deleteHandler := RequireAdmin(adminService, DeleteHubHandler(hubService))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "/api/admin/hubs/"
		escapedPath := r.URL.EscapedPath()
		if r.Method == http.MethodDelete && escapedPath == strings.TrimSuffix(prefix, "/") {
			deleteHandler.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(escapedPath, prefix) {
			afterPrefix := strings.TrimPrefix(escapedPath, prefix)
			for _, route := range routes {
				if r.Method == route.method && afterPrefix == strings.TrimPrefix(route.suffix, "/") {
					route.handler.ServeHTTP(w, r)
					return
				}
				if r.Method != route.method || !strings.HasSuffix(afterPrefix, route.suffix) {
					continue
				}
				escapedID := strings.TrimSuffix(afterPrefix, route.suffix)
				if escapedID == "" {
					continue
				}
				if id, err := url.PathUnescape(escapedID); err == nil && strings.TrimSpace(id) != "" {
					r.SetPathValue("id", id)
					route.handler.ServeHTTP(w, r)
					return
				}
			}
			if r.Method == http.MethodDelete {
				escapedID := afterPrefix
				if escapedID != "" {
					if id, err := url.PathUnescape(escapedID); err == nil && strings.TrimSpace(id) != "" {
						r.SetPathValue("id", id)
						deleteHandler.ServeHTTP(w, r)
						return
					}
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
