package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

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
