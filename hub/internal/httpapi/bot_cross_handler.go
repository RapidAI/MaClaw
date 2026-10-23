package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
)

// PostBotPrimaryHandler POST /api/v1/bots/{id}/primary
func PostBotPrimaryHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		if err := svc.SetPrimaryBot(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id")); err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		items, err := svc.ListBotsForUser(r.Context(), principal.TenantID, principal.UserID)
		if err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// PostBotCrossHandler POST /api/v1/bots/{id}/cross
func PostBotCrossHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		var in botmgmt.BotCrossNote
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "invalid bot settings")
			return
		}
		note, err := svc.PostBotCrossTalk(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"), in)
		if err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		writeJSON(w, http.StatusOK, note)
	}
}

// GetBotCrossNotesHandler GET /api/v1/bots/cross-notes
func GetBotCrossNotesHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		notes, err := svc.ListBotCrossNotes(r.Context(), principal.TenantID, principal.UserID)
		if err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": notes})
	}
}

// PostBotCrossStartHandler POST /api/v1/bots/{id}/cross-notes/{noteID}/start
func PostBotCrossStartHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		note, claimed, err := svc.ClaimBotCrossStart(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"), r.PathValue("noteID"))
		if err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":         note.ID,
			"owner":      note.Owner,
			"from":       note.From,
			"target":     note.Target,
			"kind":       note.Kind,
			"text":       note.Text,
			"order":      note.Order,
			"created_at": note.CreatedAt,
			"started_at": note.StartedAt,
			"run_id":     note.RunID,
			"reply_to":   note.ReplyTo,
			"claimed":    claimed,
		})
	}
}

// PostBotCrossRunHandler POST /api/v1/bots/{id}/cross-notes/{noteID}/run
func PostBotCrossRunHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		var in struct {
			RunID string `json:"run_id"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "invalid bot settings")
			return
		}
		note, err := svc.NoteBotCrossRun(r.Context(), principal.TenantID, principal.UserID, strings.TrimSpace(r.PathValue("id")), strings.TrimSpace(r.PathValue("noteID")), in.RunID)
		if err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		writeJSON(w, http.StatusOK, note)
	}
}

// BotMailboxHandler GET|POST /api/v1/bots/{id}/inbox — the bot's durable Hub
// mailbox ({id} may be "main"). POST {"ack":[ids],"all":false} marks ids
// taken and returns what is still unacked; GET only reads.
func BotMailboxHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		var in struct {
			Ack []string `json:"ack"`
			All bool     `json:"all"`
		}
		if r.Method == http.MethodPost {
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
				writeError(w, http.StatusBadRequest, "INVALID_INPUT", "invalid inbox request")
				return
			}
		} else {
			in.All = r.URL.Query().Get("all") == "1"
		}
		items, err := svc.BotMailbox(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"), in.Ack, in.All)
		if err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}
