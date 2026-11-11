package desktopd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// A bot's display number used to stay with its registry entry after the bot
// was deleted or re-created under a new id; with 30 numbers the pool ran out
// and no new bot could open (papers 2026-11-11: 31 entries, 3 screens
// running). Hub now releases a deleted bot's number at once and prunes the
// entries of bots that no longer exist. Reclaim on exhaustion stays in the
// supervisor as the fallback.

// ReleaseBot gives one bot's display number back. A bot that is busy (a run
// or a person on it) is refused with ErrDesktopBusy.
func (s *Service) ReleaseBot(ctx context.Context, tenantID, userID string) (map[string]any, error) {
	spec, err := normalize(Spec{TenantID: tenantID, UserID: userID})
	if err != nil {
		return nil, err
	}
	if spec.BotID == "" {
		return nil, fmt.Errorf("%w: bot_id is required", ErrInvalid)
	}
	if !s.dockerRuntime() {
		return map[string]any{"released": false}, nil
	}
	key, err := specKey(spec)
	if err != nil {
		return nil, err
	}
	out, err := s.docker(ctx, "exec", containerName(spec.TenantID, spec.UserID), "python3", "/desktop_supervisor.py", "release-bot", key)
	result := map[string]any{}
	if line := statusLine(out); line != "" {
		_ = json.Unmarshal([]byte(line), &result)
	}
	if busy, _ := result["busy"].(bool); busy {
		return result, fmt.Errorf("%w: this bot's desktop is in use", ErrDesktopBusy)
	}
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()+out), "no such container") || strings.Contains(strings.ToLower(err.Error()+out), "is not running") {
			return map[string]any{"released": false}, nil
		}
		return nil, fmt.Errorf("%w: release bot display: %v", ErrDocker, err)
	}
	return result, nil
}

// PruneBots drops the display entries of this owner's bots that are not in
// keepBotIDs. Running or busy screens and the owner's own are never touched.
func (s *Service) PruneBots(ctx context.Context, tenantID, userID string, keepBotIDs []string) (map[string]any, error) {
	spec, err := normalize(Spec{TenantID: tenantID, UserID: userID})
	if err != nil {
		return nil, err
	}
	if spec.BotID != "" {
		return nil, fmt.Errorf("%w: prune takes the owner, not a bot", ErrInvalid)
	}
	if !s.dockerRuntime() {
		return map[string]any{"released": []any{}}, nil
	}
	keys := make([]string, 0, len(keepBotIDs))
	for _, id := range keepBotIDs {
		key, err := desktop.BotKey(spec.TenantID, spec.UserID, id)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	arg := "-"
	if len(keys) > 0 {
		arg = strings.Join(keys, ",")
	}
	out, err := s.docker(ctx, "exec", containerName(spec.TenantID, spec.UserID), "python3", "/desktop_supervisor.py", "prune-bots", arg)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()+out), "no such container") || strings.Contains(strings.ToLower(err.Error()+out), "is not running") {
			return map[string]any{"released": []any{}}, nil
		}
		return nil, fmt.Errorf("%w: prune bot displays: %v", ErrDocker, err)
	}
	result := map[string]any{}
	if line := statusLine(out); line != "" {
		_ = json.Unmarshal([]byte(line), &result)
	}
	return result, nil
}

func registerBotRelease(mux *http.ServeMux, svc *Service, authOK func(*http.Request) bool) {
	mux.HandleFunc("POST /v1/desktops/bot-release", func(w http.ResponseWriter, r *http.Request) {
		if !authOK(r) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		var in struct {
			TenantID string `json:"tenant_id"`
			UserID   string `json:"user_id"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		out, err := svc.ReleaseBot(r.Context(), in.TenantID, in.UserID)
		if err != nil {
			writeServiceErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	mux.HandleFunc("POST /v1/desktops/bot-prune", func(w http.ResponseWriter, r *http.Request) {
		if !authOK(r) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		var in struct {
			TenantID string   `json:"tenant_id"`
			UserID   string   `json:"user_id"`
			Keep     []string `json:"keep_bots"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		out, err := svc.PruneBots(r.Context(), in.TenantID, in.UserID, in.Keep)
		if err != nil {
			writeServiceErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
}
