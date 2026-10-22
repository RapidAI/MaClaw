package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
)

// PostBotRunPauseHandler POST /api/v1/bots/{id}/runs/{runID}/pause
// Holds the run at its next step boundary. Body (optional):
// {"reason":"...","timeout_sec":n}. 501 PAUSE_UNSUPPORTED means the
// MaClawSrv behind this bot is older than pause.
func PostBotRunPauseHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		var in struct {
			Reason     string `json:"reason"`
			TimeoutSec int64  `json:"timeout_sec"`
		}
		if r.Body != nil {
			raw, _ := io.ReadAll(io.LimitReader(r.Body, 64<<10))
			if len(strings.TrimSpace(string(raw))) > 0 && json.Unmarshal(raw, &in) != nil {
				writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid pause body")
				return
			}
		}
		st, err := svc.PauseDesktopRun(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"), r.PathValue("runID"), in.Reason, in.TimeoutSec)
		if err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		writeJSON(w, http.StatusOK, st)
	}
}

// PostBotRunResumeHandler POST /api/v1/bots/{id}/runs/{runID}/resume
func PostBotRunResumeHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		st, err := svc.ResumeDesktopRun(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"), r.PathValue("runID"))
		if err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		writeJSON(w, http.StatusOK, st)
	}
}
