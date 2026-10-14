package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
)

// PutBotDesktopModeAdminHandler PUT /api/admin/bots/{id}/desktop-mode
// {"desktop_mode":"isolated"|"shared"}. Isolated gives the bot its own
// virtual display in its owner's desktop container; shared (the default) is
// the owner's desktop.
func PutBotDesktopModeAdminHandler(svc *botmgmt.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			return
		}
		var in struct {
			DesktopMode string `json:"desktop_mode"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "invalid bot settings")
			return
		}
		bot, err := svc.SetBotDesktopMode(r.Context(), botTenantID(r), r.PathValue("id"), in.DesktopMode)
		if err != nil {
			writeBotError(w, err)
			return
		}
		mode := bot.DesktopMode
		if mode == "" {
			mode = "shared"
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": bot.ID, "desktop_mode": mode})
	}
}

// PostBotDesktopHandBackHandler POST /api/v1/bots/{id}/desktop/handback
// The person finished on an isolated bot's display (login, 2FA, captcha):
// the bot may drive it again, and a continuation message resumes the task.
// Body (optional): {"message":"...","phase":"execute"}; "resume":false only
// hands the display back.
func PostBotDesktopHandBackHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		in := struct {
			Message string `json:"message"`
			Phase   string `json:"phase"`
			Resume  *bool  `json:"resume"`
		}{}
		if r.Body != nil {
			raw, err := io.ReadAll(io.LimitReader(r.Body, botMessageBodyLimit))
			if err != nil || (len(strings.TrimSpace(string(raw))) > 0 && json.Unmarshal(raw, &in) != nil) {
				writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "invalid bot settings")
				return
			}
		}
		botID := r.PathValue("id")
		if err := svc.HandBackDesktop(r.Context(), principal.TenantID, principal.UserID, botID); err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		if in.Resume != nil && !*in.Resume {
			writeJSON(w, http.StatusOK, map[string]any{"handed_back": true})
			return
		}
		message := strings.TrimSpace(in.Message)
		if message == "" {
			message = "我已经在桌面上完成了需要我做的操作（登录/验证），桌面交还给你，请继续刚才的任务。"
		}
		admission, err := svc.AdmitDesktopMessage(r.Context(), principal.TenantID, principal.UserID, botID, message, in.Phase, "")
		if err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		if admission.Accepted && !admission.Settled {
			writeJSON(w, http.StatusAccepted, map[string]any{
				"handed_back":       true,
				"accepted":          true,
				"run_id":            admission.RunID,
				"status":            "running",
				"client_message_id": admission.ClientMessageID,
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"handed_back": true, "reply": admission.Reply})
	}
}
