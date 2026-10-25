package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/desktoppool"
)

// desktopViewForgetter drops a cached noVNC page after the container restarts.
type desktopViewForgetter interface {
	ForgetDesktopView(tenantID, userID string)
}

func writeComputerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, desktoppool.ErrComputerResetDisabled):
		writeError(w, http.StatusForbidden, "DESKTOP_RESET_DISABLED", err.Error())
	case errors.Is(err, desktoppool.ErrComputerBusy):
		writeError(w, http.StatusConflict, "DESKTOP_BUSY", err.Error())
	case errors.Is(err, desktoppool.ErrComputerMissing):
		writeError(w, http.StatusNotFound, "DESKTOP_NOT_CREATED", err.Error())
	default:
		writeDesktopError(w, err)
	}
}

// auditComputer is the audit trail for user-triggered restart/reset: one
// structured line per attempt (who, which desktop, forced, outcome).
func auditComputer(action, tenantID, userID string, force bool, out desktoppool.ComputerResult, err error) {
	result := "ok"
	if err != nil {
		result = "error: " + err.Error()
	}
	log.Printf("[audit] desktop.%s tenant=%s user=%s force=%v container=%s backup=%s result=%q",
		action, tenantID, userID, force, out.Container, out.Backup, result)
}

// GetUserDesktopComputerHandler GET /api/v1/bots/desktop-computer
// What the caller may do with their own desktop ("电脑" tab).
func GetUserDesktopComputerHandler(pool *desktoppool.Pool, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok {
			return
		}
		if pool == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "desktop service store is unavailable")
			return
		}
		policy, err := pool.TenantComputerPolicy(r.Context(), principal.TenantID)
		if err != nil {
			writeComputerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"restart_allowed": true, "reset_allowed": !policy.ResetDisabled})
	}
}

// PostUserDesktopComputerHandler POST /api/v1/bots/desktop-computer/{restart|reset}
// {"force":false}. Always the caller's own desktop: tenant and user come from
// the authenticated principal, never from the body.
func PostUserDesktopComputerHandler(pool *desktoppool.Pool, identity botAuthenticator, views desktopViewForgetter, action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok {
			return
		}
		if pool == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "desktop service store is unavailable")
			return
		}
		var in struct {
			Force bool `json:"force"`
		}
		if r.Body != nil {
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil && !strings.Contains(err.Error(), "EOF") {
				writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_SERVICE", "invalid request")
				return
			}
		}
		var (
			out desktoppool.ComputerResult
			err error
		)
		if action == "reset" {
			out, err = pool.ResetDesktop(r.Context(), principal.TenantID, principal.UserID, in.Force)
		} else {
			out, err = pool.RestartDesktop(r.Context(), principal.TenantID, principal.UserID, in.Force)
		}
		auditComputer(action, principal.TenantID, principal.UserID, in.Force, out, err)
		if err != nil {
			writeComputerError(w, err)
			return
		}
		if views != nil {
			views.ForgetDesktopView(principal.TenantID, principal.UserID)
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// GetDesktopComputerPolicyAdminHandler GET /api/admin/desktop-services/computer-policy
func GetDesktopComputerPolicyAdminHandler(pool *desktoppool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if pool == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "desktop service store is unavailable")
			return
		}
		policy, err := pool.TenantComputerPolicy(r.Context(), desktopTenantID(r))
		if err != nil {
			writeComputerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, policy)
	}
}

// PutDesktopComputerPolicyAdminHandler PUT /api/admin/desktop-services/computer-policy
// {"reset_disabled":true}
func PutDesktopComputerPolicyAdminHandler(pool *desktoppool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if pool == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "desktop service store is unavailable")
			return
		}
		var in desktoppool.ComputerPolicy
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_SERVICE", "invalid computer policy")
			return
		}
		tenantID := desktopTenantID(r)
		policy, err := pool.SetTenantComputerPolicy(r.Context(), tenantID, in)
		if err != nil {
			writeComputerError(w, err)
			return
		}
		log.Printf("[audit] desktop.computer-policy tenant=%s reset_disabled=%v", tenantID, policy.ResetDisabled)
		writeJSON(w, http.StatusOK, policy)
	}
}
