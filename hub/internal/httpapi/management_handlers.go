package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/feishu"
	"github.com/RapidAI/CodeClaw/hub/internal/invitation"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type ManualBindRequest struct {
	Email string `json:"email"`
}

type LookupUserRequest struct {
	Email  string `json:"email"`
	Mobile string `json:"mobile"`
}

type DeleteBoundUserRequest struct {
	Email string `json:"email"`
}

type BlockEmailRequest struct {
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

type CenterConfigRequest struct {
	BaseURL               string   `json:"base_url"`
	PublicBaseURL         string   `json:"public_base_url"`
	Visibility            string   `json:"visibility"`
	EnrollmentMode        string   `json:"enrollment_mode"`
	CorporateEmailDomain  *string  `json:"corporate_email_domain,omitempty"`
	CorporateEmailDomains []string `json:"corporate_email_domains,omitempty"`
	AcceptPublicSignup    *bool    `json:"accept_public_signup,omitempty"`
}

type FailureLogView struct {
	ID        string         `json:"id"`
	Category  string         `json:"category"`
	EventCode string         `json:"event_code"`
	Message   string         `json:"message"`
	EntityID  string         `json:"entity_id"`
	Email     string         `json:"email"`
	ClientIP  string         `json:"client_ip"`
	Details   map[string]any `json:"details"`
	CreatedAt string         `json:"created_at"`
}

func ListFailureLogsHandler(repo store.FailureEventLogRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if repo == nil {
			writeError(w, http.StatusNotImplemented, "FAILURE_LOGS_UNAVAILABLE", "Failure logs are unavailable")
			return
		}
		limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
		offset, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("offset")))
		items, total, err := repo.List(r.Context(), store.FailureEventLogFilter{
			Keyword:  strings.TrimSpace(r.URL.Query().Get("keyword")),
			Category: strings.TrimSpace(r.URL.Query().Get("category")),
			Offset:   offset,
			Limit:    limit,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILURE_LOGS_FAILED", err.Error())
			return
		}
		logs := make([]FailureLogView, 0, len(items))
		for _, item := range items {
			if item == nil {
				continue
			}
			details := map[string]any{}
			if strings.TrimSpace(item.DetailsJSON) != "" {
				_ = json.Unmarshal([]byte(item.DetailsJSON), &details)
			}
			logs = append(logs, FailureLogView{
				ID:        item.ID,
				Category:  item.Category,
				EventCode: item.EventCode,
				Message:   item.Message,
				EntityID:  item.EntityID,
				Email:     item.Email,
				ClientIP:  item.ClientIP,
				Details:   details,
				CreatedAt: item.CreatedAt.Format(time.RFC3339),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"logs": logs, "total": total, "offset": offset, "limit": limit})
	}
}

type BoundUserView struct {
	ID               string                    `json:"id"`
	Email            string                    `json:"email"`
	SN               string                    `json:"sn"`
	Status           string                    `json:"status"`
	EnrollmentStatus string                    `json:"enrollment_status"`
	SmartRoute       bool                      `json:"smart_route"`
	HasServiceAccess bool                      `json:"has_service_access,omitempty"`
	ServiceStatus    *llmservice.ServiceStatus `json:"service_status,omitempty"`
}

func ManualBindHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req ManualBindRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.Email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email is required")
			return
		}

		user, err := identity.ManualBindForTenant(r.Context(), RequestTenantID(r), req.Email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MANUAL_BIND_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true,
			"user": map[string]any{
				"id":    user.ID,
				"email": user.Email,
				"sn":    user.SN,
			},
		})
	}
}

func DeleteBoundUserHandler(identity *auth.IdentityService, deviceSvc *device.Service, invitationSvc *invitation.Service, feishuNotifier *feishu.Notifier, imCleaners []IMBindingCleaner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email := strings.TrimSpace(r.URL.Query().Get("email"))
		if email == "" && r.Body != nil {
			var req DeleteBoundUserRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
				email = strings.TrimSpace(req.Email)
			}
		}
		if email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email is required")
			return
		}
		if identity == nil || identity.UsersRepo() == nil {
			writeError(w, http.StatusInternalServerError, "USER_DELETE_UNAVAILABLE", "User repository is unavailable")
			return
		}

		user, err := identity.UsersRepo().GetByTenantEmail(r.Context(), RequestTenantID(r), email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LOOKUP_USER_FAILED", err.Error())
			return
		}
		if user == nil {
			writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "User not found")
			return
		}

		var deletedMachines int64
		if deviceSvc != nil {
			deletedMachines, err = deviceSvc.ForceDeleteMachinesByUser(r.Context(), user.ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "DELETE_USER_MACHINES_FAILED", err.Error())
				return
			}
		}
		var deletedCodes int64
		if invitationSvc != nil {
			deletedCodes, err = invitationSvc.DeleteCodeByTenantEmail(r.Context(), user.TenantID, user.Email)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "DELETE_USER_INVITES_FAILED", err.Error())
				return
			}
		}
		if feishuNotifier != nil {
			feishuNotifier.RemoveOpenIDForTenant(user.TenantID, user.Email)
		}
		removeIMBindingsForTenant(imCleaners, user.TenantID, user.Email)
		if err := identity.UsersRepo().DeleteByTenantEmail(r.Context(), user.TenantID, user.Email); err != nil {
			writeError(w, http.StatusInternalServerError, "DELETE_USER_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "email": user.Email, "deleted_machines": deletedMachines, "deleted_invitation_codes": deletedCodes})
	}
}
func ListBlockedEmailsHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := identity.ListBlockedEmails(auth.WithTenant(r.Context(), RequestTenantID(r)))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_BLOCKED_EMAILS_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"blocked_emails": items})
	}
}

func AddBlockedEmailHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req BlockEmailRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.Email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email is required")
			return
		}

		if err := identity.AddBlockedEmail(auth.WithTenant(r.Context(), RequestTenantID(r)), req.Email, req.Reason); err != nil {
			writeError(w, http.StatusInternalServerError, "ADD_BLOCKED_EMAIL_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func RemoveBlockedEmailHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email := r.PathValue("email")
		if email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email is required")
			return
		}

		if err := identity.RemoveBlockedEmail(auth.WithTenant(r.Context(), RequestTenantID(r)), email); err != nil {
			writeError(w, http.StatusInternalServerError, "REMOVE_BLOCKED_EMAIL_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func LookupUserHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email := r.URL.Query().Get("email")
		mobile := r.URL.Query().Get("mobile")
		if email == "" && mobile == "" {
			var req LookupUserRequest
			if r.Method != http.MethodGet {
				if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
					email = req.Email
					mobile = req.Mobile
				}
			}
		}

		var (
			user *store.User
			err  error
		)

		switch {
		case mobile != "":
			user, err = identity.LookupUserByMobile(auth.WithTenant(r.Context(), RequestTenantID(r)), mobile)
		case email != "":
			user, err = identity.LookupUserByEmail(auth.WithTenant(r.Context(), RequestTenantID(r)), email)
		default:
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email or mobile is required")
			return
		}

		if err != nil {
			if err == auth.ErrInvalidEmail {
				writeError(w, http.StatusBadRequest, "INVALID_EMAIL", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "LOOKUP_USER_FAILED", err.Error())
			return
		}
		if user == nil {
			writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "User not found")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"user": map[string]any{
				"id":        user.ID,
				"tenant_id": user.TenantID,
				"email":     user.Email,
				"sn":        user.SN,
				"status":    user.Status,
			},
		})
	}
}

func ListUsersHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, securitySvc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			items []*store.User
			err   error
		)
		if IsGlobalAdmin(r.Context()) && strings.TrimSpace(r.URL.Query().Get("tenant_id")) == "" {
			items, err = identity.ListUsers(r.Context())
		} else {
			items, err = identity.ListUsersForTenant(r.Context(), RequestTenantID(r))
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_USERS_FAILED", err.Error())
			return
		}
		out := make([]BoundUserView, 0, len(items))
		seenEmails := make(map[string]struct{}, len(items))
		for _, user := range items {
			if user == nil {
				continue
			}
			emailKey := strings.TrimSpace(strings.ToLower(user.Email))
			if emailKey == "" {
				continue
			}
			if _, exists := seenEmails[emailKey]; exists {
				continue
			}
			seenEmails[emailKey] = struct{}{}
			var serviceStatus *llmservice.ServiceStatus
			if system != nil {
				serviceStatus, _ = llmservice.ResolveServiceStatus(r.Context(), system, securitySvc, user.Email, externalLLMBaseURL(r))
			}
			out = append(out, BoundUserView{
				ID:               user.ID,
				Email:            user.Email,
				SN:               user.SN,
				Status:           user.Status,
				EnrollmentStatus: user.EnrollmentStatus,
				SmartRoute:       user.SmartRoute,
				HasServiceAccess: serviceStatus != nil && serviceStatus.Active,
				ServiceStatus:    serviceStatus,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"users": out})
	}
}

func GetCenterStatusHandler(centerSvc *center.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status, err := centerSvc.RefreshStatus(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CENTER_STATUS_FAILED", err.Error())
			return
		}
		filterCenterStatusForTenantAdmin(r, status)
		writeJSON(w, http.StatusOK, status)
	}
}

func UpdateCenterConfigHandler(centerSvc *center.Service, identity *auth.IdentityService, onPublicBaseURLChanged ...func(string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireGlobalAdminForHubCenter(w, r) {
			return
		}
		var req CenterConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.BaseURL == "" && req.PublicBaseURL == "" && req.Visibility == "" && req.EnrollmentMode == "" && req.CorporateEmailDomain == nil && len(req.CorporateEmailDomains) == 0 && req.AcceptPublicSignup == nil {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Base URL, public base URL, visibility, enrollment mode, corporate email domains, or public signup setting is required")
			return
		}
		var (
			status *center.RegistrationState
			err    error
		)
		if req.BaseURL != "" {
			status, err = centerSvc.SetBaseURL(r.Context(), req.BaseURL)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "CENTER_CONFIG_FAILED", err.Error())
				return
			}
		}
		if req.PublicBaseURL != "" {
			status, err = centerSvc.SetPublicBaseURL(r.Context(), req.PublicBaseURL)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "CENTER_CONFIG_FAILED", err.Error())
				return
			}
			// Notify IM plugins so temp-file download URLs use the new domain.
			for _, fn := range onPublicBaseURLChanged {
				fn(status.PublicBaseURL)
			}
		}
		if req.Visibility != "" {
			status, err = centerSvc.SetVisibility(r.Context(), req.Visibility)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "CENTER_CONFIG_FAILED", err.Error())
				return
			}
		}
		if req.EnrollmentMode != "" {
			status, err = centerSvc.SetEnrollmentMode(r.Context(), req.EnrollmentMode)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "CENTER_CONFIG_FAILED", err.Error())
				return
			}
			if err := identity.SetEnrollmentMode(r.Context(), req.EnrollmentMode); err != nil {
				writeError(w, http.StatusInternalServerError, "CENTER_CONFIG_FAILED", err.Error())
				return
			}
		}
		if req.CorporateEmailDomain != nil {
			status, err = centerSvc.SetCorporateEmailDomain(r.Context(), *req.CorporateEmailDomain)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "CENTER_CONFIG_FAILED", err.Error())
				return
			}
		}
		if len(req.CorporateEmailDomains) > 0 {
			status, err = centerSvc.SetCorporateEmailDomains(r.Context(), req.CorporateEmailDomains)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "CENTER_CONFIG_FAILED", err.Error())
				return
			}
		}
		if req.AcceptPublicSignup != nil {
			status, err = centerSvc.SetAcceptPublicSignup(r.Context(), *req.AcceptPublicSignup)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "CENTER_CONFIG_FAILED", err.Error())
				return
			}
		}
		status, err = centerSvc.RefreshStatus(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CENTER_CONFIG_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

func RegisterCenterHandler(centerSvc *center.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireGlobalAdminForHubCenter(w, r) {
			return
		}
		admin := AdminFromContext(r.Context())
		if admin == nil {
			writeError(w, http.StatusUnauthorized, "ADMIN_UNAUTHORIZED", "Admin authorization required")
			return
		}

		status, err := centerSvc.Register(r.Context(), admin.Email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CENTER_REGISTER_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

func filterCenterStatusForTenantAdmin(r *http.Request, status *center.RegistrationState) {
	if r == nil || status == nil || AdminFromContext(r.Context()) == nil || IsGlobalAdmin(r.Context()) {
		return
	}
	tenantID := AdminTenantID(r.Context())
	if status.DigitalEmployeeAuthorizations != nil {
		if authz := status.DigitalEmployeeAuthorizations[tenantID]; authz != nil {
			status.DigitalEmployeeAuthorization = authz
		}
	}
	status.DigitalEmployeeAuthorizations = nil
}

func requireGlobalAdminForHubCenter(w http.ResponseWriter, r *http.Request) bool {
	if r == nil || AdminFromContext(r.Context()) == nil || IsGlobalAdmin(r.Context()) {
		return true
	}
	writeError(w, http.StatusForbidden, "GLOBAL_ADMIN_REQUIRED", "Hub Center registration is managed by the Hub global administrator")
	return false
}

// --- Smart Route permission handlers ---

const smartRouteAllKey = "smart_route_all"

// UpdateUserSmartRouteHandler toggles the smart_route flag for a single user.
func UpdateUserSmartRouteHandler(users store.UserRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			UserID  string `json:"user_id"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.UserID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "user_id is required")
			return
		}
		if err := users.UpdateSmartRoute(r.Context(), req.UserID, req.Enabled); err != nil {
			writeError(w, http.StatusInternalServerError, "UPDATE_SMART_ROUTE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// GetSmartRouteAllHandler returns the global smart_route_all toggle.
func GetSmartRouteAllHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, _ := system.Get(r.Context(), smartRouteAllKey)
		enabled := raw == "true"
		writeJSON(w, http.StatusOK, map[string]any{"enabled": enabled})
	}
}

// UpdateSmartRouteAllHandler sets the global smart_route_all toggle.
func UpdateSmartRouteAllHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		val := "false"
		if req.Enabled {
			val = "true"
		}
		if err := system.Set(r.Context(), smartRouteAllKey, val); err != nil {
			writeError(w, http.StatusInternalServerError, "UPDATE_SMART_ROUTE_ALL_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "enabled": req.Enabled})
	}
}
