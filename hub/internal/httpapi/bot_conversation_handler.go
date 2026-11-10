package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
)

// BotConversationHandler GET|POST /api/v1/bots/conversation
//
// GET ?after=<seq>&bot_id=&limit=&wait=<seconds> lists this person's synced
// conversation entries changed after the cursor; wait holds the request
// (at most 25s) until something changes. POST {client_id, entries:[...]}
// upserts this client's entries. Every GUI of the same person reads the same
// journal, so a message, acknowledgement, reply, card answer or run state
// written by one is shown by the others.
func BotConversationHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		if r.Method == http.MethodPost {
			var in struct {
				ClientID string                      `json:"client_id"`
				Entries  []botmgmt.ConversationWrite `json:"entries"`
			}
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
				writeError(w, http.StatusBadRequest, "INVALID_INPUT", "invalid conversation write")
				return
			}
			head, err := svc.WriteConversation(r.Context(), principal.TenantID, principal.UserID, in.ClientID, in.Entries)
			if err != nil {
				writeBotUserError(w, r, principal, err, "")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"head": head})
			return
		}
		q := r.URL.Query()
		after, _ := strconv.ParseInt(strings.TrimSpace(q.Get("after")), 10, 64)
		limit, _ := strconv.Atoi(strings.TrimSpace(q.Get("limit")))
		waitSec, _ := strconv.Atoi(strings.TrimSpace(q.Get("wait")))
		page, err := svc.ReadConversation(r.Context(), principal.TenantID, principal.UserID, q.Get("bot_id"), after, limit, time.Duration(waitSec)*time.Second)
		if err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		writeJSON(w, http.StatusOK, page)
	}
}
