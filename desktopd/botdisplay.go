package desktopd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

// specKey is the supervisor key of the desktop this spec names: the owner's
// user key, or the bot key for an isolated bot.
func specKey(spec Spec) (string, error) {
	if spec.BotID != "" {
		return desktop.BotKey(spec.TenantID, spec.UserID, spec.BotID)
	}
	return desktop.UserKey(spec.TenantID, spec.UserID)
}

// specDisplay is the X display a request acts on. For the owner's desktop it
// is the display the caller sent (MaClawSrv got it from its session). For a
// bot it is always derived here from that bot's key, so one bot can never
// reach another bot's or the owner's display by naming it.
func (s *Service) specDisplay(ctx context.Context, spec Spec, requested string) (string, error) {
	if spec.BotID == "" {
		return requested, nil
	}
	key, err := specKey(spec)
	if err != nil {
		return "", err
	}
	out, err := s.docker(ctx, "exec", containerName(spec.TenantID, spec.UserID), "python3", "/desktop_supervisor.py", "display", key)
	if err != nil {
		return "", fmt.Errorf("%w: this bot's desktop is not open yet", ErrInvalid)
	}
	var status struct {
		Display string `json:"display"`
	}
	if line := statusLine(out); line == "" || json.Unmarshal([]byte(line), &status) != nil || !desktop.ValidDisplay(status.Display) || status.Display == desktop.DefaultDisplay {
		return "", fmt.Errorf("%w: desktop supervisor returned no bot display", ErrDocker)
	}
	s.noteComputerScreen(spec, status.Display)
	return status.Display, nil
}

// stopOwnerOnlyWhileBotsBusy stops only the owner's own display when an
// isolated bot of this owner is working or has a person on it. docker stop
// would end that bot's task. It reports whether it handled the stop.
// botOwners are owners whose isolated bots opened a display since desktopd
// started. Only their stop asks the supervisor about busy bots; everyone
// else's stop is exactly as before.
func ownerID(spec Spec) string { return spec.TenantID + "\x00" + spec.UserID }

func (s *Service) noteBotOwner(spec Spec) { s.botOwners.Store(ownerID(spec), true) }

func (s *Service) stopOwnerOnlyWhileBotsBusy(ctx context.Context, name string, spec Spec) bool {
	if _, ok := s.botOwners.Load(ownerID(spec)); !ok {
		return false
	}
	out, err := s.docker(ctx, "exec", name, "python3", "/desktop_supervisor.py", "bots-busy")
	if err != nil {
		return false // stopped container or a supervisor without bots
	}
	var status struct {
		Busy []string `json:"busy"`
	}
	if line := statusLine(out); line == "" || json.Unmarshal([]byte(line), &status) != nil || len(status.Busy) == 0 {
		return false
	}
	key, err := desktop.UserKey(spec.TenantID, spec.UserID)
	if err != nil {
		return false
	}
	writeCtx, cancel := loginWriteContext(ctx)
	defer cancel()
	_, _ = s.docker(writeCtx, "exec", name, "python3", "/desktop_supervisor.py", "stop", key)
	return true
}

// withBotID lets every /v1/desktops/* request name an isolated bot with a
// bot_id field (JSON body) or query parameter. It is folded into user_id as
// the bot's desktop user, which normalize splits again. No bot_id is the
// owner's own desktop, exactly as before.
func withBotID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/desktops") {
			next.ServeHTTP(w, r)
			return
		}
		if q := r.URL.Query(); strings.TrimSpace(q.Get("bot_id")) != "" {
			user, err := botDesktopUser(q.Get("user_id"), q.Get("bot_id"))
			if err != nil {
				writeErr(w, http.StatusBadRequest, desktopbot.MissingIdentityNextStep("bot"))
				return
			}
			q.Set("user_id", user)
			q.Del("bot_id")
			r.URL.RawQuery = q.Encode()
		}
		if r.Method == http.MethodPost && r.Body != nil {
			raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBotRewriteBody))
			if err != nil {
				writeErr(w, http.StatusBadRequest, "invalid desktop request")
				return
			}
			raw, err = rewriteBotBody(raw)
			if err != nil {
				writeErr(w, http.StatusBadRequest, desktopbot.MissingIdentityNextStep("bot"))
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(raw))
			r.ContentLength = int64(len(raw))
		}
		next.ServeHTTP(w, r)
	})
}

// maxBotRewriteBody is above the largest /v1/desktops body (file writes).
const maxBotRewriteBody = 64 << 20

func rewriteBotBody(raw []byte) ([]byte, error) {
	if !bytes.Contains(raw, []byte(`"bot_id"`)) {
		return raw, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return raw, nil // the handler reports the bad body
	}
	var botID, userID string
	if v, ok := fields["bot_id"]; ok {
		if json.Unmarshal(v, &botID) != nil {
			return nil, fmt.Errorf("%w: %s", ErrInvalid, desktopbot.MissingIdentityNextStep("bot"))
		}
	}
	delete(fields, "bot_id")
	if strings.TrimSpace(botID) == "" {
		return json.Marshal(fields)
	}
	if v, ok := fields["user_id"]; ok {
		_ = json.Unmarshal(v, &userID)
	}
	user, err := botDesktopUser(userID, botID)
	if err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(user)
	fields["user_id"] = encoded
	return json.Marshal(fields)
}

func botDesktopUser(userID, botID string) (string, error) {
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return userID, nil
	}
	if !desktop.ValidBotID(botID) {
		return "", fmt.Errorf("%w: %s", ErrInvalid, desktopbot.MissingIdentityNextStep("bot"))
	}
	return desktop.BotDesktopUser(userID, botID), nil
}
