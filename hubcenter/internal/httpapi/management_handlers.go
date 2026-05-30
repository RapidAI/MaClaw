package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	corelib "github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/hubs"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

type BlockEmailRequest struct {
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

type BlockIPRequest struct {
	IP     string `json:"ip"`
	Reason string `json:"reason"`
}

type ToggleHubRequest struct {
	HubID  string `json:"hub_id,omitempty"`
	Reason string `json:"reason"`
}

type UpdateHubVisibilityRequest struct {
	HubID      string `json:"hub_id,omitempty"`
	Visibility string `json:"visibility"`
}

type HubIDRequest struct {
	HubID string `json:"hub_id,omitempty"`
}

type MigrateHubUserRequest struct {
	Mode           string `json:"mode"`
	Email          string `json:"email"`
	TenantID       string `json:"tenant_id,omitempty"`
	SourceTenantID string `json:"source_tenant_id,omitempty"`
	TargetTenantID string `json:"target_tenant_id,omitempty"`
	Domain         string `json:"domain"`
	FromHubID      string `json:"from_hub_id"`
	ToHubID        string `json:"to_hub_id"`
}

type adminHubView struct {
	*store.HubInstance
	GuestDomains                  []string                                         `json:"guest_domains,omitempty"`
	Tenants                       []hubs.HubUserDashboardItem                      `json:"tenants,omitempty"`
	DigitalEmployeeAuthorizations map[string]*corelib.DigitalEmployeeAuthorization `json:"digital_employee_authorizations,omitempty"`
	RegistrationPolicy            hubs.HubRegistrationPolicyConfig                 `json:"registration_policy"`
}

const adminDefaultTenantID = "tenant_default"

func ListHubsHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := service.ListHubs(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_HUBS_FAILED", err.Error())
			return
		}
		dashboard, err := service.ListUserDashboard(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_HUBS_FAILED", err.Error())
			return
		}
		dashboardByHub := map[string]hubs.HubUserDashboardItem{}
		tenantsByHub := map[string][]hubs.HubUserDashboardItem{}
		policyByHub, err := service.HubRegistrationPolicies(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_HUBS_FAILED", err.Error())
			return
		}
		for _, item := range dashboard {
			if strings.TrimSpace(item.TenantID) == "" {
				dashboardByHub[item.HubID] = item
				continue
			}
			tenantsByHub[item.HubID] = append(tenantsByHub[item.HubID], item)
		}
		views := make([]adminHubView, 0, len(items))
		for _, item := range items {
			if item == nil {
				continue
			}
			policy := policyByHub[item.ID]
			auths, err := service.HubDigitalEmployeeAuthorizations(r.Context(), item.ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "LIST_HUBS_FAILED", err.Error())
				return
			}
			tenantAuths := map[string]*corelib.DigitalEmployeeAuthorization{}
			for tenantID, auth := range auths {
				tenantID = adminExternalTenantID(tenantID)
				if auth == nil {
					continue
				}
				tenantAuths[tenantID] = auth
			}
			tenantItems := []hubs.HubUserDashboardItem{adminDefaultTenantItem(item, dashboardByHub[item.ID])}
			tenantItems = append(tenantItems, tenantsByHub[item.ID]...)
			seenTenants := map[string]struct{}{}
			for _, tenant := range tenantItems {
				seenTenants[strings.TrimSpace(tenant.TenantID)] = struct{}{}
			}
			for tenantID, tenantPolicy := range policy.Tenants {
				tenantID = adminExternalTenantID(tenantID)
				if _, ok := seenTenants[tenantID]; ok {
					continue
				}
				tenantItems = append(tenantItems, hubs.HubUserDashboardItem{HubID: item.ID, TenantID: tenantID, TenantName: tenantPolicy.TenantName, HubName: item.Name, BaseURL: item.BaseURL, Status: item.Status, IsDisabled: item.IsDisabled, AcceptPublicSignup: item.AcceptPublicSignup, SignupMode: item.EnrollmentMode, LastSeenAt: item.LastSeenAt})
				seenTenants[tenantID] = struct{}{}
			}
			for tenantID := range tenantAuths {
				tenantID = strings.TrimSpace(tenantID)
				if _, ok := seenTenants[tenantID]; ok {
					continue
				}
				tenantItems = append(tenantItems, hubs.HubUserDashboardItem{HubID: item.ID, TenantID: tenantID, HubName: item.Name, BaseURL: item.BaseURL, Status: item.Status, IsDisabled: item.IsDisabled, AcceptPublicSignup: item.AcceptPublicSignup, SignupMode: item.EnrollmentMode, LastSeenAt: item.LastSeenAt})
			}
			views = append(views, adminHubView{HubInstance: item, GuestDomains: dashboardByHub[item.ID].GuestDomains, Tenants: tenantItems, DigitalEmployeeAuthorizations: tenantAuths, RegistrationPolicy: adminExternalRegistrationPolicy(policy)})
		}
		writeJSON(w, http.StatusOK, map[string]any{"hubs": views})
	}
}

func adminExternalTenantID(tenantID string) string {
	if strings.TrimSpace(tenantID) == "" {
		return adminDefaultTenantID
	}
	return strings.TrimSpace(tenantID)
}

func adminDefaultTenantItem(hub *store.HubInstance, item hubs.HubUserDashboardItem) hubs.HubUserDashboardItem {
	if item.HubID == "" && hub != nil {
		item = hubs.HubUserDashboardItem{HubID: hub.ID, HubName: hub.Name, BaseURL: hub.BaseURL, Status: hub.Status, IsDisabled: hub.IsDisabled, CorporateEmailDomain: hub.CorporateEmailDomain, CorporateEmailDomains: adminHubMailDomains(hub), AcceptPublicSignup: hub.AcceptPublicSignup, SignupMode: hub.EnrollmentMode, LastSeenAt: hub.LastSeenAt}
	}
	if hub != nil {
		domains := adminMergeMailDomains(item.CorporateEmailDomains, adminHubMailDomains(hub))
		if len(domains) > 0 {
			item.CorporateEmailDomains = domains
			if strings.TrimSpace(item.CorporateEmailDomain) == "" {
				item.CorporateEmailDomain = domains[0]
			}
		}
	}
	item.TenantID = adminDefaultTenantID
	return item
}

func adminMergeMailDomains(groups ...[]string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, group := range groups {
		for _, value := range group {
			value = strings.TrimSpace(strings.ToLower(value))
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func adminHubMailDomains(hub *store.HubInstance) []string {
	if hub == nil || strings.TrimSpace(hub.CorporateEmailDomain) == "" {
		return nil
	}
	return adminMergeMailDomains([]string{hub.CorporateEmailDomain})
}

func adminExternalRegistrationPolicy(policy hubs.HubRegistrationPolicyConfig) hubs.HubRegistrationPolicyConfig {
	out := policy
	out.Tenants = map[string]store.HubTenantRegistrationPolicy{}
	for tenantID, tenantPolicy := range policy.Tenants {
		externalID := adminExternalTenantID(tenantID)
		tenantPolicy.TenantID = externalID
		out.Tenants[externalID] = tenantPolicy
	}
	return out
}

func ListEnterpriseMailDomainsHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := service.ListEnterpriseMailDomains(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_ENTERPRISE_MAIL_DOMAINS_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": adminExternalEnterpriseMailDomainItems(items)})
	}
}

func adminExternalEnterpriseMailDomainItems(items []hubs.EnterpriseMailDomainItem) []hubs.EnterpriseMailDomainItem {
	out := make([]hubs.EnterpriseMailDomainItem, 0, len(items))
	for _, item := range items {
		item.TenantID = adminExternalTenantID(item.TenantID)
		out = append(out, item)
	}
	return out
}

func UpdateHubRegistrationPolicyHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req hubs.UpdateHubRegistrationPolicyRequest
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		hubID := strings.TrimSpace(r.PathValue("id"))
		if hubID == "" {
			hubID = strings.TrimSpace(req.HubID)
		}
		if hubID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_HUB_ID", "Hub id is required")
			return
		}
		cfg, err := service.UpdateHubRegistrationPolicy(r.Context(), hubID, req)
		if err != nil {
			if errors.Is(err, hubs.ErrHubNotFound) {
				writeError(w, http.StatusNotFound, "HUB_NOT_FOUND", "Hub not found")
				return
			}
			if errors.Is(err, hubs.ErrInvalidRegistrationPolicy) {
				writeError(w, http.StatusBadRequest, "INVALID_REGISTRATION_POLICY", "registration policy conflicts with public fallback rules")
				return
			}
			writeError(w, http.StatusInternalServerError, "UPDATE_REGISTRATION_POLICY_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "registration_policy": adminExternalRegistrationPolicy(cfg)})
	}
}

func ListUserDashboardHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := service.ListUserDashboard(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_USER_DASHBOARD_FAILED", err.Error())
			return
		}
		report, err := service.UserRegistrationReport(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "USER_REGISTRATION_REPORT_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": adminExternalUserDashboardItems(items), "registration_report": adminExternalUserRegistrationReport(report)})
	}
}

func adminExternalUserDashboardItems(items []hubs.HubUserDashboardItem) []hubs.HubUserDashboardItem {
	out := make([]hubs.HubUserDashboardItem, 0, len(items))
	for _, item := range items {
		item.TenantID = adminExternalTenantID(item.TenantID)
		out = append(out, item)
	}
	return out
}

func adminExternalUserRegistrationReport(report hubs.UserRegistrationReport) hubs.UserRegistrationReport {
	for i := range report.Hubs {
		report.Hubs[i].TenantID = adminExternalTenantID(report.Hubs[i].TenantID)
	}
	return report
}

type UpdateDigitalEmployeeAuthorizationRequest struct {
	HubID     string `json:"hub_id,omitempty"`
	TenantID  string `json:"tenant_id,omitempty"`
	Quota     int    `json:"quota"`
	Years     int    `json:"years"`
	Enabled   *bool  `json:"enabled,omitempty"`
	StartDate string `json:"start_date,omitempty"` // optional ISO date YYYY-MM-DD
}

func UpdateDigitalEmployeeAuthorizationHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req UpdateDigitalEmployeeAuthorizationRequest
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		hubID := adminHubIDFromRequest(r, req.HubID)
		if hubID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_HUB_ID", "Hub id is required")
			return
		}
		tenantID := strings.TrimSpace(req.TenantID)
		if tenantID == "" {
			writeError(w, http.StatusBadRequest, "TENANT_ID_REQUIRED", "tenant_id is required for digital employee authorization")
			return
		}
		if req.Quota < 0 {
			writeError(w, http.StatusBadRequest, "INVALID_QUOTA", "Quota must be >= 0")
			return
		}
		if req.Years < 0 {
			writeError(w, http.StatusBadRequest, "INVALID_YEARS", "Years must be >= 0")
			return
		}
		if (req.Enabled == nil || *req.Enabled) && req.Years < 1 {
			writeError(w, http.StatusBadRequest, "INVALID_YEARS", "Years must be >= 1 when enabling digital employee authorization")
			return
		}
		if req.StartDate != "" {
			if _, parseErr := time.Parse("2006-01-02", req.StartDate); parseErr != nil {
				writeError(w, http.StatusBadRequest, "INVALID_START_DATE", "start_date must be in YYYY-MM-DD format")
				return
			}
		}
		auth, err := service.UpdateDigitalEmployeeAuthorization(r.Context(), hubID, hubs.DigitalEmployeeAuthorizationUpdate{TenantID: tenantID, Quota: req.Quota, Years: req.Years, Enabled: req.Enabled, StartDate: req.StartDate})
		if err != nil {
			if errors.Is(err, hubs.ErrDigitalEmployeeTenantRequired) {
				writeError(w, http.StatusBadRequest, "TENANT_ID_REQUIRED", "tenant_id is required for digital employee authorization")
				return
			}
			if errors.Is(err, hubs.ErrDigitalEmployeeQuotaDecrease) {
				writeError(w, http.StatusBadRequest, "DIGITAL_EMPLOYEE_QUOTA_DECREASE", "Digital employee authorization count can only increase")
				return
			}
			if errors.Is(err, hubs.ErrDigitalEmployeeQuotaRequired) {
				writeError(w, http.StatusBadRequest, "DIGITAL_EMPLOYEE_QUOTA_REQUIRED", "Digital employee authorization count must be greater than zero when enabling authorization")
				return
			}
			if errors.Is(err, hubs.ErrDigitalEmployeeYearsRequired) {
				writeError(w, http.StatusBadRequest, "INVALID_YEARS", "Years must be >= 1 when enabling digital employee authorization")
				return
			}
			if errors.Is(err, hubs.ErrDigitalEmployeeAuthorizationStoreUnavailable) {
				writeError(w, http.StatusServiceUnavailable, "DIGITAL_EMPLOYEE_AUTHORIZATION_STORE_UNAVAILABLE", "Digital employee authorization store is unavailable")
				return
			}
			if errors.Is(err, hubs.ErrHubNotFound) {
				writeError(w, http.StatusNotFound, "HUB_NOT_FOUND", "Hub not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "UPDATE_DIGITAL_EMPLOYEE_AUTHORIZATION_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "tenant_id": adminExternalTenantID(tenantID), "digital_employee_authorization": auth})
	}
}
func UpdateHubVisibilityHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req UpdateHubVisibilityRequest
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		hubID := adminHubIDFromRequest(r, req.HubID)
		if hubID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_HUB_ID", "Hub id is required")
			return
		}
		if strings.TrimSpace(req.Visibility) == "" {
			writeError(w, http.StatusBadRequest, "INVALID_VISIBILITY", "Visibility is required")
			return
		}

		if err := service.UpdateVisibility(r.Context(), hubID, req.Visibility); err != nil {
			writeError(w, http.StatusInternalServerError, "UPDATE_HUB_VISIBILITY_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "visibility": strings.TrimSpace(req.Visibility)})
	}
}

func DisableHubHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req ToggleHubRequest
		if err := decodeOptionalLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		hubID := adminHubIDFromRequest(r, req.HubID)
		if hubID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_HUB_ID", "Hub id is required")
			return
		}
		if err := service.DisableHub(r.Context(), hubID, req.Reason); err != nil {
			writeError(w, http.StatusInternalServerError, "DISABLE_HUB_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func EnableHubHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req HubIDRequest
		if err := decodeOptionalLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		hubID := adminHubIDFromRequest(r, req.HubID)
		if hubID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_HUB_ID", "Hub id is required")
			return
		}
		if err := service.EnableHub(r.Context(), hubID); err != nil {
			writeError(w, http.StatusInternalServerError, "ENABLE_HUB_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func ConfirmHubHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req HubIDRequest
		if err := decodeOptionalLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		hubID := adminHubIDFromRequest(r, req.HubID)
		if hubID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_HUB_ID", "Hub id is required")
			return
		}
		if err := service.ConfirmHubRegistrationByAdmin(r.Context(), hubID); err != nil {
			writeError(w, http.StatusInternalServerError, "CONFIRM_HUB_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "online"})
	}
}

func DeleteHubHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req HubIDRequest
		if err := decodeOptionalLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		hubID := adminHubIDFromRequest(r, req.HubID)
		if hubID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_HUB_ID", "Hub id is required")
			return
		}
		if err := service.DeleteHub(r.Context(), hubID); err != nil {
			writeError(w, http.StatusInternalServerError, "DELETE_HUB_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "unregistered"})
	}
}

func adminHubIDFromRequest(r *http.Request, bodyHubID string) string {
	if r != nil {
		if hubID := strings.TrimSpace(r.PathValue("id")); hubID != "" {
			return hubID
		}
	}
	return strings.TrimSpace(bodyHubID)
}

func RefreshHubUserInventoryHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := service.RefreshUserInventory(r.Context())
		if err != nil {
			writeError(w, http.StatusBadRequest, "REFRESH_USER_INVENTORY_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
	}
}
func MigrateHubUserHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req MigrateHubUserRequest
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		mode := strings.ToLower(strings.TrimSpace(req.Mode))
		if mode == "" {
			if strings.TrimSpace(req.Email) != "" {
				mode = "email"
			} else if strings.TrimSpace(req.Domain) != "" {
				mode = "domain"
			}
		}

		var (
			result *hubs.MigrationResult
			err    error
		)
		switch mode {
		case "email", "user":
			result, err = service.MigrateUser(r.Context(), hubs.MigrateUserRequest{Email: req.Email, TenantID: strings.TrimSpace(req.TenantID), SourceTenantID: strings.TrimSpace(req.SourceTenantID), TargetTenantID: strings.TrimSpace(req.TargetTenantID), FromHubID: req.FromHubID, ToHubID: req.ToHubID})
		case "domain":
			result, err = service.MigrateDomain(r.Context(), hubs.MigrateDomainRequest{Domain: req.Domain, TenantID: strings.TrimSpace(req.TenantID), SourceTenantID: strings.TrimSpace(req.SourceTenantID), TargetTenantID: strings.TrimSpace(req.TargetTenantID), FromHubID: req.FromHubID, ToHubID: req.ToHubID})
		default:
			writeError(w, http.StatusBadRequest, "INVALID_MIGRATION_MODE", "Migration mode must be email or domain")
			return
		}
		if err != nil {
			if errors.Is(err, hubs.ErrHubNotFound) {
				writeError(w, http.StatusNotFound, "TARGET_HUB_NOT_FOUND", err.Error())
				return
			}
			if errors.Is(err, hubs.ErrHubDisabled) {
				writeError(w, http.StatusLocked, "TARGET_HUB_DISABLED", "Target hub has been disabled")
				return
			}
			writeError(w, http.StatusBadRequest, "MIGRATE_USER_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "migration": adminExternalMigrationResult(result)})
	}
}

func adminExternalMigrationResult(result *hubs.MigrationResult) *hubs.MigrationResult {
	if result == nil {
		return nil
	}
	out := *result
	out.SourceTenantID = adminExternalTenantID(out.SourceTenantID)
	out.TargetTenantID = adminExternalTenantID(out.TargetTenantID)
	return &out
}

func ListBlockedEmailsHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := service.ListBlockedEmails(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_BLOCKED_EMAILS_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"blocked_emails": items})
	}
}

func AddBlockedEmailHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req BlockEmailRequest
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.Email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email is required")
			return
		}
		if err := service.AddBlockedEmail(r.Context(), req.Email, req.Reason); err != nil {
			writeError(w, http.StatusInternalServerError, "ADD_BLOCKED_EMAIL_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func RemoveBlockedEmailHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email := r.PathValue("email")
		if email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email is required")
			return
		}
		if err := service.RemoveBlockedEmail(r.Context(), email); err != nil {
			writeError(w, http.StatusInternalServerError, "REMOVE_BLOCKED_EMAIL_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func ListBlockedIPsHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := service.ListBlockedIPs(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_BLOCKED_IPS_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"blocked_ips": items})
	}
}

func AddBlockedIPHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req BlockIPRequest
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.IP == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "IP is required")
			return
		}
		if err := service.AddBlockedIP(r.Context(), req.IP, req.Reason); err != nil {
			writeError(w, http.StatusInternalServerError, "ADD_BLOCKED_IP_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func RemoveBlockedIPHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := r.PathValue("ip")
		if ip == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "IP is required")
			return
		}
		if err := service.RemoveBlockedIP(r.Context(), ip); err != nil {
			writeError(w, http.StatusInternalServerError, "REMOVE_BLOCKED_IP_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}
