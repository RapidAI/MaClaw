package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/auth"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/entry"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/hubs"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/mail"
)

type EntryResolveRequest struct {
	Email string `json:"email"`
}

type HubHeartbeatRequest struct {
	HubSecret              string `json:"hub_secret"`
	InvitationCodeRequired *bool  `json:"invitation_code_required,omitempty"`
}

func RegisterHubHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req hubs.RegisterHubRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
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

func HubHeartbeatHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hubID := r.PathValue("id")
		if hubID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_HUB_ID", "Hub id is required")
			return
		}

		var req HubHeartbeatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}

		if err := service.HeartbeatHubWithSecret(r.Context(), hubID, req.HubSecret, req.InvitationCodeRequired); err != nil {
			if errors.Is(err, hubs.ErrHubUnauthorized) {
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
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "online"})
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
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		resp, err := service.ResolveByEmailFromIP(r.Context(), req.Email, clientIPFromRequest(r))
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

func NewRouter(adminService *auth.AdminService, hubService *hubs.Service, entryService *entry.Service, mailer *mail.Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", HealthHandler("codeclaw-hubcenter"))
	mux.HandleFunc("GET /api/admin/status", AdminStatusHandler(adminService))
	mux.HandleFunc("POST /api/admin/setup", SetupAdminHandler(adminService))
	mux.HandleFunc("POST /api/admin/login", AdminLoginHandler(adminService))
	mux.HandleFunc("POST /api/admin/password", RequireAdmin(adminService, AdminChangePasswordHandler(adminService)))
	mux.HandleFunc("POST /api/admin/profile", RequireAdmin(adminService, AdminUpdateProfileHandler(adminService)))
	mux.HandleFunc("GET /api/admin/server/config", RequireAdmin(adminService, GetAdminServerConfigHandler(hubService)))
	mux.HandleFunc("POST /api/admin/server/config", RequireAdmin(adminService, UpdateAdminServerConfigHandler(hubService)))
	mux.HandleFunc("GET /api/admin/mail/config", RequireAdmin(adminService, GetMailConfigHandler(mailer)))
	mux.HandleFunc("POST /api/admin/mail/config", RequireAdmin(adminService, UpdateMailConfigHandler(mailer)))
	mux.HandleFunc("GET /api/admin/hubs", RequireAdmin(adminService, ListHubsHandler(hubService)))
	mux.HandleFunc("POST /api/admin/hubs/{id}/visibility", RequireAdmin(adminService, UpdateHubVisibilityHandler(hubService)))
	mux.HandleFunc("POST /api/admin/hubs/{id}/disable", RequireAdmin(adminService, DisableHubHandler(hubService)))
	mux.HandleFunc("POST /api/admin/hubs/{id}/enable", RequireAdmin(adminService, EnableHubHandler(hubService)))
	mux.HandleFunc("POST /api/admin/hubs/{id}/confirm", RequireAdmin(adminService, ConfirmHubHandler(hubService)))
	mux.HandleFunc("DELETE /api/admin/hubs/{id}", RequireAdmin(adminService, DeleteHubHandler(hubService)))
	mux.HandleFunc("GET /api/admin/blocked-emails", RequireAdmin(adminService, ListBlockedEmailsHandler(hubService)))
	mux.HandleFunc("POST /api/admin/blocked-emails", RequireAdmin(adminService, AddBlockedEmailHandler(hubService)))
	mux.HandleFunc("DELETE /api/admin/blocked-emails/{email}", RequireAdmin(adminService, RemoveBlockedEmailHandler(hubService)))
	mux.HandleFunc("GET /api/admin/blocked-ips", RequireAdmin(adminService, ListBlockedIPsHandler(hubService)))
	mux.HandleFunc("POST /api/admin/blocked-ips", RequireAdmin(adminService, AddBlockedIPHandler(hubService)))
	mux.HandleFunc("DELETE /api/admin/blocked-ips/{ip}", RequireAdmin(adminService, RemoveBlockedIPHandler(hubService)))
	mux.HandleFunc("POST /api/admin/mail/test", RequireAdmin(adminService, AdminSendTestMailHandler(mailer)))
	mux.HandleFunc("POST /api/hubs/register", RegisterHubHandler(hubService))
	mux.HandleFunc("POST /api/hubs/{id}/heartbeat", HubHeartbeatHandler(hubService))
	mux.HandleFunc("GET /hub-registration/confirm", ConfirmHubRegistrationHandler(hubService))
	mux.HandleFunc("POST /api/entry/resolve", EntryResolveHandler(entryService))
	registerAdminStaticRoutes(mux, "./web/admin", "/admin")
	return mux
}
