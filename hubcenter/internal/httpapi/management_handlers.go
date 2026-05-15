package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/hubs"
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
	Reason string `json:"reason"`
}

type UpdateHubVisibilityRequest struct {
	Visibility string `json:"visibility"`
}

type MigrateHubUserRequest struct {
	Mode      string `json:"mode"`
	Email     string `json:"email"`
	Domain    string `json:"domain"`
	FromHubID string `json:"from_hub_id"`
	ToHubID   string `json:"to_hub_id"`
}

func ListHubsHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := service.ListHubs(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_HUBS_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"hubs": items})
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
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "registration_report": report})
	}
}

type UpdateDigitalEmployeeAuthorizationRequest struct {
	Quota     int    `json:"quota"`
	Years     int    `json:"years"`
	Enabled   *bool  `json:"enabled,omitempty"`
	StartDate string `json:"start_date,omitempty"` // optional ISO date YYYY-MM-DD
}

func UpdateDigitalEmployeeAuthorizationHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hubID := r.PathValue("id")
		if hubID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_HUB_ID", "Hub id is required")
			return
		}
		var req UpdateDigitalEmployeeAuthorizationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
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
		if req.StartDate != "" {
			if _, parseErr := time.Parse("2006-01-02", req.StartDate); parseErr != nil {
				writeError(w, http.StatusBadRequest, "INVALID_START_DATE", "start_date must be in YYYY-MM-DD format")
				return
			}
		}
		auth, err := service.UpdateDigitalEmployeeAuthorization(r.Context(), hubID, hubs.DigitalEmployeeAuthorizationUpdate{Quota: req.Quota, Years: req.Years, Enabled: req.Enabled, StartDate: req.StartDate})
		if err != nil {
			if errors.Is(err, hubs.ErrDigitalEmployeeQuotaDecrease) {
				writeError(w, http.StatusBadRequest, "DIGITAL_EMPLOYEE_QUOTA_DECREASE", "Digital employee authorization count can only increase")
				return
			}
			if errors.Is(err, hubs.ErrHubNotFound) {
				writeError(w, http.StatusNotFound, "HUB_NOT_FOUND", "Hub not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "UPDATE_DIGITAL_EMPLOYEE_AUTHORIZATION_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "digital_employee_authorization": auth})
	}
}
func UpdateHubVisibilityHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hubID := r.PathValue("id")
		if hubID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_HUB_ID", "Hub id is required")
			return
		}

		var req UpdateHubVisibilityRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
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
		hubID := r.PathValue("id")
		var req ToggleHubRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
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
		hubID := r.PathValue("id")
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
		hubID := r.PathValue("id")
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
		hubID := r.PathValue("id")
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
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
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
			result, err = service.MigrateUser(r.Context(), hubs.MigrateUserRequest{Email: req.Email, FromHubID: req.FromHubID, ToHubID: req.ToHubID})
		case "domain":
			result, err = service.MigrateDomain(r.Context(), hubs.MigrateDomainRequest{Domain: req.Domain, FromHubID: req.FromHubID, ToHubID: req.ToHubID})
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
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "migration": result})
	}
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
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
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
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
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
