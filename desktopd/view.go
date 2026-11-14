package desktopd

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// Typed picture states. running is the only one with a live frame.
// asleep: the container is stopped (the reaper docker-stopped it, or it
// was never started). display_stopped: the container is up and the
// supervisor idle-stopped the bot display. Both are 200 from /view;
// opening either without a wake reason is 409 DESKTOP_ASLEEP.
const (
	DesktopRunning        = "running"
	DesktopAsleep         = "asleep"
	DesktopDisplayStopped = "display_stopped"
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

// classifyDesktopView is the picture state for one look. A stopped
// container is asleep. A container whose display is not running, or whose
// screen has no page, is display_stopped. Only both-up with a page is running.
func classifyDesktopView(containerUp, displayUp bool, novnc string) string {
	if !containerUp {
		return DesktopAsleep
	}
	if !displayUp || strings.TrimSpace(novnc) == "" {
		return DesktopDisplayStopped
	}
	return DesktopRunning
}

// ViewState is PeekSession plus the typed state a viewer needs so it can
// drop a remembered page. It never starts, restores, or refreshes
// last_activity. docker inspect distinguishes a stopped container (asleep)
// from a container whose display the supervisor stopped (display_stopped).
func (s *Service) ViewState(ctx context.Context, spec Spec) (Session, error) {
	spec, err := normalize(spec)
	if err != nil {
		return Session{}, err
	}
	if ctx != nil && ctx.Err() != nil {
		return Session{}, ctx.Err()
	}
	up, err := s.containerRunning(ctx, containerName(spec.TenantID, spec.UserID))
	if err != nil || !up {
		return Session{State: DesktopAsleep}, nil
	}
	if !s.displayRunning(ctx, spec) {
		return Session{State: DesktopDisplayStopped}, nil
	}
	session, err := s.PeekSession(ctx, spec)
	if err != nil {
		return Session{}, err
	}
	session.State = classifyDesktopView(true, true, session.Novnc)
	if session.State != DesktopRunning {
		return Session{State: session.State}, nil
	}
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
