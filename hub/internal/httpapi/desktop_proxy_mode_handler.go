package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/RapidAI/CodeClaw/hub/internal/desktoppool"
)

// writeProxyModeError maps desktop proxy mode errors; the rest go through
// writeDesktopError.
func writeProxyModeError(w http.ResponseWriter, err error) {
	if errors.Is(err, desktoppool.ErrProxyModeLocked) {
		writeError(w, http.StatusForbidden, "DESKTOP_PROXY_MODE_LOCKED", err.Error())
		return
	}
	writeDesktopError(w, err)
}

// GetUserDesktopProxyModeHandler GET /api/v1/bots/desktop-proxy-mode
//
// The calling user's desktop egress mode (proxy/direct/smart), the tenant
// default and whether the tenant locked it.
func GetUserDesktopProxyModeHandler(pool *desktoppool.Pool, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok {
			return
		}
		if pool == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "desktop service store is unavailable")
			return
		}
		view, err := pool.ProxyMode(r.Context(), principal.TenantID, principal.UserID)
		if err != nil {
			writeProxyModeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

// PutUserDesktopProxyModeHandler PUT /api/v1/bots/desktop-proxy-mode
// {"mode":"proxy"|"direct"|"smart"|""}. "" follows the tenant default. The
// new mode applies without rebuilding the desktop: desktopd drops the
// desktop's open tunnels and new connections take the new route.
func PutUserDesktopProxyModeHandler(pool *desktoppool.Pool, identity botAuthenticator) http.HandlerFunc {
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
			Mode string `json:"mode"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_SERVICE", "invalid proxy mode")
			return
		}
		view, err := pool.SetUserProxyMode(r.Context(), principal.TenantID, principal.UserID, in.Mode)
		if err != nil {
			writeProxyModeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

// GetDesktopProxyModeAdminHandler GET /api/admin/desktop-services/proxy-mode
func GetDesktopProxyModeAdminHandler(pool *desktoppool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if pool == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "desktop service store is unavailable")
			return
		}
		policy, err := pool.TenantProxyModePolicy(r.Context(), desktopTenantID(r))
		if err != nil {
			writeProxyModeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, policy)
	}
}

// PutDesktopProxyModeAdminHandler PUT /api/admin/desktop-services/proxy-mode
// {"default":"smart","locked":false}
func PutDesktopProxyModeAdminHandler(pool *desktoppool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if pool == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "desktop service store is unavailable")
			return
		}
		var in desktoppool.ProxyModePolicy
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_SERVICE", "invalid proxy mode policy")
			return
		}
		policy, err := pool.SetTenantProxyModePolicy(r.Context(), desktopTenantID(r), in)
		if err != nil {
			writeProxyModeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, policy)
	}
}

// PostDesktopProxyModeHandler POST /api/v1/desktop-services/proxy-mode is the
// MaClawSrv (desktop API token) read of one user's mode. It is read-only on
// purpose: a bot must not switch the proxy on its own (prompt injection could
// otherwise turn egress policy off); users switch it in their settings.
func PostDesktopProxyModeHandler(pool *desktoppool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if pool == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "desktop service store is unavailable")
			return
		}
		if !authorizeDesktopAPI(w, r) {
			return
		}
		var in struct {
			TenantID string `json:"tenant_id"`
			UserID   string `json:"user_id"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_SERVICE", "invalid desktop service settings")
			return
		}
		view, err := pool.ProxyMode(r.Context(), in.TenantID, in.UserID)
		if err != nil {
			writeProxyModeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}
