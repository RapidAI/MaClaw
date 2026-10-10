package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
)

// botAuthenticator is the machine identity a bot request needs. A viewer
// token resolves to the same tenant and user, so the bot user surface accepts
// either credential. Only the machine path has ever proven a device, which is
// why the check below still rejects an empty owner for both.
type botAuthenticator interface {
	AuthenticateMachine(ctx context.Context, machineID, rawToken string) (*auth.MachinePrincipal, error)
	AuthenticateViewer(ctx context.Context, rawToken string) (*auth.ViewerPrincipal, error)
}

func botMachine(w http.ResponseWriter, r *http.Request, identity botAuthenticator) (*auth.MachinePrincipal, bool) {
	principal, ok := botViewerPrincipal(w, r, identity)
	if !ok {
		return nil, false
	}
	if strings.TrimSpace(principal.UserID) == "" || strings.TrimSpace(principal.TenantID) == "" {
		writeError(w, http.StatusForbidden, "BOT_DISABLED", botmgmt.DisabledMessage)
		return nil, false
	}
	return principal, true
}

// botViewerPrincipal accepts a machine credential first and falls back to a
// viewer token. The mobile and web clients only ever hold a viewer token, so
// without this fallback they cannot reach their own bots. A viewer token names
// one user, so it stands in for a machine of that same user.
func botViewerPrincipal(w http.ResponseWriter, r *http.Request, identity botAuthenticator) (*auth.MachinePrincipal, bool) {
	if identity == nil {
		writeError(w, http.StatusUnauthorized, "MACHINE_UNAUTHORIZED", "machine authorization required")
		return nil, false
	}
	machineID := strings.TrimSpace(r.Header.Get("X-Machine-ID"))
	token := extractBearerToken(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "MACHINE_UNAUTHORIZED", "machine authorization required")
		return nil, false
	}
	if machineID != "" {
		principal, err := identity.AuthenticateMachine(r.Context(), machineID, token)
		if err == nil && principal != nil {
			return principal, true
		}
		// A stale machine ID on a valid viewer token must not lock the owner
		// out of their own bots, so fall through to the viewer check.
	}
	viewer, err := identity.AuthenticateViewer(r.Context(), token)
	if err != nil || viewer == nil {
		writeError(w, http.StatusUnauthorized, "MACHINE_UNAUTHORIZED", "machine authorization required")
		return nil, false
	}
	return &auth.MachinePrincipal{
		TenantID:  viewer.TenantID,
		UserID:    viewer.UserID,
		MachineID: strings.TrimSpace(r.Header.Get("X-Machine-ID")),
	}, true
}

// GetBotAccessHandler GET /api/v1/bots/access
func GetBotAccessHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok {
			return
		}
		enabled := false
		if svc != nil {
			on, err := svc.Enabled(r.Context(), principal.TenantID, principal.UserID)
			if err == nil {
				enabled = on
			}
		}
		out := map[string]any{"enabled": enabled}
		if !enabled {
			out["message"] = botmgmt.DisabledMessage
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// ListBotsHandler GET /api/v1/bots
func ListBotsHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		bots, err := svc.BotsForUser(r.Context(), principal.TenantID, principal.UserID)
		if err != nil {
			writeBotUserError(w, r, principal, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": bots})
	}
}

// PostBotUserHandler POST /api/v1/bots
func PostBotUserHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		var in struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "invalid bot settings")
			return
		}
		bot, err := svc.CreateBotForUser(r.Context(), principal.TenantID, principal.UserID, in.Name, in.Description)
		if err != nil {
			writeBotUserError(w, r, principal, err)
			return
		}
		writeJSON(w, http.StatusCreated, bot)
	}
}

// PatchBotUserHandler PATCH /api/v1/bots/{id}
func PatchBotUserHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		var in struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "invalid bot settings")
			return
		}
		bot, err := svc.UpdateBotForUser(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"), in.Name, in.Description)
		if err != nil {
			writeBotUserError(w, r, principal, err)
			return
		}
		writeJSON(w, http.StatusOK, bot)
	}
}

// DeleteBotUserHandler DELETE /api/v1/bots/{id}
func DeleteBotUserHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		if err := svc.DeleteBotForUser(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id")); err != nil {
			writeBotUserError(w, r, principal, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

// PostBotMessageHandler POST /api/v1/bots/{id}/messages
func PostBotMessageHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		var in struct {
			Content string `json:"content"`
			Phase   string `json:"phase"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "invalid bot settings")
			return
		}
		if wantsAsyncBotReply(r) {
			admission, err := svc.AdmitDesktopMessage(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"), in.Content, in.Phase)
			if err != nil {
				writeBotUserError(w, r, principal, err)
				return
			}
			if admission.Accepted && !admission.Settled {
				writeJSON(w, http.StatusAccepted, map[string]any{
					"accepted": true,
					"run_id":   admission.RunID,
					"status":   "running",
				})
				return
			}
			writeJSON(w, http.StatusOK, admission.Reply)
			return
		}
		reply, err := svc.PostMessagePhase(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"), in.Content, in.Phase)
		if err != nil {
			writeBotUserError(w, r, principal, err)
			return
		}
		writeJSON(w, http.StatusOK, reply)
	}
}

// wantsAsyncBotReply is the desktop client's request to accept the command
// before the run finishes. The reply is read from the run afterwards.
func wantsAsyncBotReply(r *http.Request) bool {
	if r == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("async")), "true") {
		return true
	}
	for _, value := range r.Header.Values("Prefer") {
		for _, part := range strings.Split(value, ",") {
			token := strings.TrimSpace(part)
			if i := strings.IndexByte(token, ';'); i >= 0 {
				token = strings.TrimSpace(token[:i])
			}
			if strings.EqualFold(token, "respond-async") {
				return true
			}
		}
	}
	return false
}

// GetBotRunHandler GET /api/v1/bots/{id}/runs/{runID}
// A running command answers 202. The finished reply is the same object as a
// synchronous message. One poll does not cancel the run.
func GetBotRunHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		reply, done, err := svc.DesktopRunResult(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"), r.PathValue("runID"))
		if err != nil {
			writeBotUserError(w, r, principal, err)
			return
		}
		if !done {
			writeJSON(w, http.StatusAccepted, map[string]any{
				"accepted": true,
				"status":   "running",
				"run_id":   r.PathValue("runID"),
			})
			return
		}
		writeJSON(w, http.StatusOK, reply)
	}
}

// GetBotDesktopHandler GET /api/v1/bots/{id}/desktop
// Returns the Hub noVNC path while this user's desktop is running.
func GetBotDesktopHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		botID := r.PathValue("id")
		novnc, userControl, err := svc.DesktopWatch(r.Context(), principal.TenantID, principal.UserID, botID)
		if err != nil {
			writeBotUserError(w, r, principal, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"novnc_url":        novnc,
			"user_control":     userControl,
			"attention_reason": svc.DesktopAttention(principal.TenantID, principal.UserID, botID),
		})
	}
}

// PostBotDesktopWatchHandler POST /api/v1/bots/{id}/desktop
// Opens or holds this user's desktop while the owner watches it or takes
// over from the Bot page. The hold expires shortly after the last poll, so
// a closed page does not pin the desktop forever.
func PostBotDesktopWatchHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		botID := r.PathValue("id")
		release, epoch, ok := botWatchRelease(w, r)
		if !ok {
			return
		}
		ctx := botmgmt.WithDesktopWatchEpoch(r.Context(), epoch)
		if release {
			if err := svc.ReleaseUserDesktopView(ctx, principal.TenantID, principal.UserID, botID); err != nil {
				writeBotUserError(w, r, principal, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"released": true})
			return
		}
		novnc, userControl, err := svc.HoldDesktopView(ctx, principal.TenantID, principal.UserID, botID)
		if err != nil {
			writeBotUserError(w, r, principal, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"novnc_url":        novnc,
			"user_control":     userControl,
			"attention_reason": svc.DesktopAttention(principal.TenantID, principal.UserID, botID),
		})
	}
}

// botWatchRelease reads an optional {"release":true,"epoch":n}. An empty body
// is a hold. EOF is not a bad request: the viewer poll posts no body.
// Epoch identifies one open of the page. Zero means an older client.
func botWatchRelease(w http.ResponseWriter, r *http.Request) (bool, int64, bool) {
	if r.Body == nil {
		return false, 0, true
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "invalid bot settings")
		return false, 0, false
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return false, 0, true
	}
	var in struct {
		Release bool  `json:"release"`
		Epoch   int64 `json:"epoch"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "invalid bot settings")
		return false, 0, false
	}
	if in.Epoch < 0 {
		in.Epoch = 0
	}
	return in.Release, in.Epoch, true
}

// GetDesktopHandoffHandler proxies noVNC through Hub. The token in the path
// is the credential; the iframe cannot send the machine bearer.
func GetDesktopHandoffHandler(svc *botmgmt.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("X-Frame-Options", "ALLOWALL")
		w.Header().Set("Content-Security-Policy", "frame-ancestors *")
		svc.ProxyDesktopHandoff(w, r, r.PathValue("token"), r.PathValue("rest"))
	}
}
