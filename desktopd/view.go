package desktopd

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// PeekSession returns the noVNC (and CDP) page for a screen that is already
// running. It never starts, restores, or refreshes last_activity. An empty
// Session means the screen is not up; that is not an error.
func (s *Service) PeekSession(ctx context.Context, spec Spec) (Session, error) {
	spec, err := normalize(spec)
	if err != nil {
		return Session{}, err
	}
	if ctx != nil && ctx.Err() != nil {
		return Session{}, ctx.Err()
	}
	base, err := s.publicBase()
	if err != nil {
		return Session{}, err
	}
	status, err := s.Screen(ctx, spec)
	if err != nil || strings.TrimSpace(status.Token) == "" || !desktop.ValidDisplay(status.Display) {
		return Session{}, nil
	}
	if !s.displayRunning(ctx, spec) {
		return Session{}, nil
	}
	// One tunnel per container, named by the owner. The gate token in the
	// URL userinfo selects this bot's screen, not the owner's / Bot 1 screen.
	ownerKey, err := desktop.UserKey(spec.TenantID, spec.UserID)
	if err != nil {
		return Session{}, err
	}
	session := Session{Display: status.Display, Container: containerName(spec.TenantID, spec.UserID)}
	if cdp, err := tunnelEndpoint(base, ownerKey, "cdp", status.Token, "", ""); err == nil {
		session.CDP = cdp
	}
	novnc, err := tunnelEndpoint(base, ownerKey, "vnc", status.Token, "vnc.html", "autoconnect=1&resize=scale")
	if err != nil {
		return Session{}, nil
	}
	session.Novnc = novnc
	return session, nil
}

// displayRunning is the supervisor's running bit for this screen. A missing
// or unreadable answer is treated as up when Screen already returned a
// token, so an older supervisor still yields a view. running:false stays
// empty: a slept screen must not look live.
func (s *Service) displayRunning(ctx context.Context, spec Spec) bool {
	key, err := specKey(spec)
	if err != nil {
		return false
	}
	name := containerName(spec.TenantID, spec.UserID)
	if name == "" {
		return false
	}
	out, err := s.docker(ctx, "exec", name, "python3", "/desktop_supervisor.py", "display", key)
	if err != nil {
		return false
	}
	line := statusLine(out)
	if line == "" {
		return true
	}
	var status struct {
		Running *bool `json:"running"`
	}
	if json.Unmarshal([]byte(line), &status) != nil || status.Running == nil {
		return true
	}
	return *status.Running
}
