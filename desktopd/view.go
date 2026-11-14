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
	// DesktopUnavailable is an inspect or display read that failed. It is
	// not a stopped container and not a stopped display.
	DesktopUnavailable = "unavailable"
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
	running, err := s.displayStatus(ctx, spec)
	if err != nil || !running {
		return Session{}, nil
	}
	return s.sessionForRunningScreen(ctx, spec)
}

// sessionForRunningScreen builds the page for a screen the caller already
// knows is running. It does not ask the supervisor again.
func (s *Service) sessionForRunningScreen(ctx context.Context, spec Spec) (Session, error) {
	base, err := s.publicBase()
	if err != nil {
		return Session{}, err
	}
	status, err := s.Screen(ctx, spec)
	if err != nil || strings.TrimSpace(status.Token) == "" || !desktop.ValidDisplay(status.Display) {
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
	if err != nil {
		return Session{State: DesktopUnavailable}, nil
	}
	if !up {
		return Session{State: DesktopAsleep}, nil
	}
	// One display read. PeekSession would ask again.
	running, err := s.displayStatus(ctx, spec)
	if err != nil {
		return Session{State: DesktopUnavailable}, nil
	}
	if !running {
		return Session{State: DesktopDisplayStopped}, nil
	}
	session, err := s.sessionForRunningScreen(ctx, spec)
	if err != nil {
		return Session{}, err
	}
	session.State = classifyDesktopView(true, true, session.Novnc)
	if session.State != DesktopRunning {
		return Session{State: session.State}, nil
	}
	return session, nil
}

// displayRunning is the supervisor's running bit. A read error is not up.
// displayStatus treats a missing running bit as up so an older supervisor
// still yields a view, and running:false as stopped.
func (s *Service) displayRunning(ctx context.Context, spec Spec) bool {
	running, err := s.displayStatus(ctx, spec)
	return err == nil && running
}

// displayStatus is the supervisor's running bit. A docker exec failure is
// an error, not "stopped". A missing running bit is up, so an older
// supervisor still yields a view. running:false is display_stopped.
func (s *Service) displayStatus(ctx context.Context, spec Spec) (bool, error) {
	key, err := specKey(spec)
	if err != nil {
		return false, err
	}
	name := containerName(spec.TenantID, spec.UserID)
	if name == "" {
		return false, ErrInvalid
	}
	out, err := s.docker(ctx, "exec", name, "python3", "/desktop_supervisor.py", "display", key)
	if err != nil {
		return false, err
	}
	line := statusLine(out)
	if line == "" {
		return true, nil
	}
	var status struct {
		Running *bool `json:"running"`
	}
	if json.Unmarshal([]byte(line), &status) != nil || status.Running == nil {
		return true, nil
	}
	return *status.Running, nil
}
