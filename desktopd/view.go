package desktopd

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// errScreenNotUp is a nudge of a display that is not running.
var errScreenNotUp = errors.New("screen is not up")

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
	// Ask whether the display is up before reading its address. A docker
	// failure here is not "stopped": an empty page would make Hub reopen a
	// desktop that is still up.
	live, err := s.screenLive(ctx, spec)
	if err != nil {
		return Session{}, err
	}
	if !live {
		return Session{}, nil
	}
	status, err := s.Screen(ctx, spec)
	if err != nil {
		return Session{}, err
	}
	if strings.TrimSpace(status.Token) == "" || !desktop.ValidDisplay(status.Display) {
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

// screenLive reports whether this screen's display is up. A stopped
// container or an explicit running:false is not live and not an error.
// A docker failure while the container is running is an error: the screen
// may still be up, and an empty answer would look like it had stopped.
func (s *Service) screenLive(ctx context.Context, spec Spec) (bool, error) {
	key, err := specKey(spec)
	if err != nil {
		return false, err
	}
	name := containerName(spec.TenantID, spec.UserID)
	if name == "" {
		return false, nil
	}
	out, err := s.docker(ctx, "inspect", "--format", "{{.State.Running}}", name)
	if err != nil {
		if missingObject(err) {
			return false, nil
		}
		return false, err
	}
	if strings.TrimSpace(out) != "true" {
		return false, nil
	}
	out, err = s.docker(ctx, "exec", name, "python3", "/desktop_supervisor.py", "display", key)
	if err != nil {
		// The supervisor's own "never opened" is an answer: this screen is
		// not up. Any other exec failure may be a blink while it is up.
		if strings.Contains(err.Error(), "desktop was never opened") {
			return false, nil
		}
		return false, err
	}
	return displayStatusLive(out), nil
}

// NudgeSession turns a running screen's picture back on. Xvfb blanks after
// ten minutes, and a display that is still running then stays black while
// its address looks live. This does not start, stop, or refresh activity.
func (s *Service) NudgeSession(ctx context.Context, spec Spec) error {
	spec, err := normalize(spec)
	if err != nil {
		return err
	}
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	live, err := s.screenLive(ctx, spec)
	if err != nil {
		return err
	}
	if !live {
		// Not up is not a successful unblank. Callers record a success as
		// "just touched", and that would hold off restarting a screen that
		// is actually stopped.
		return errScreenNotUp
	}
	status, err := s.Screen(ctx, spec)
	if err != nil {
		return err
	}
	display := strings.TrimSpace(status.Display)
	if !desktop.ValidDisplay(display) {
		return errScreenNotUp
	}
	name := containerName(spec.TenantID, spec.UserID)
	if name == "" {
		return errScreenNotUp
	}
	// xset may be missing in an old image. The screen is still up either way.
	// Each call is bounded so a stuck X command cannot hold the watch poll.
	for _, argv := range [][]string{
		{"xset", "s", "reset"},
		{"xset", "-dpms"},
		{"xset", "dpms", "force", "on"},
	} {
		_, _ = s.execOnDisplay(ctx, name, display, argv...)
	}
	// A bot display has no wallpaper. A minimized Chromium is a black screen
	// with the process still running, so the address looks live and 唤醒 is
	// never offered. Map it only when nothing visible is already up, so a
	// captcha the person is dragging is left alone.
	if spec.BotID != "" {
		s.mapHiddenBrowser(ctx, name, display)
	}
	return nil
}

func (s *Service) execOnDisplay(ctx context.Context, name, display string, argv ...string) (string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	args := append([]string{"exec", "-e", "DISPLAY=" + display, name}, argv...)
	return s.docker(cmdCtx, args...)
}

func (s *Service) mapHiddenBrowser(ctx context.Context, name, display string) {
	out, err := s.execOnDisplay(ctx, name, display, "xdotool", "search", "--class", "chromium")
	if err != nil {
		return
	}
	hidden := ""
	hiddenWidth := 0
	for _, wid := range strings.Fields(out) {
		if !decimalID(wid) {
			continue
		}
		info, err := s.execOnDisplay(ctx, name, display, "xwininfo", "-id", wid)
		if err != nil {
			continue
		}
		state, width := windowMapState(info)
		if width < 200 {
			continue
		}
		if state == "IsViewable" {
			return
		}
		if state != "" && width > hiddenWidth {
			hidden, hiddenWidth = wid, width
		}
	}
	if hidden == "" {
		return
	}
	_, _ = s.execOnDisplay(ctx, name, display, "xdotool", "windowmap", hidden)
	_, _ = s.execOnDisplay(ctx, name, display, "xdotool", "windowactivate", hidden)
}

func decimalID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func windowMapState(info string) (string, int) {
	state, width := "", 0
	for _, line := range strings.Split(info, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Map State:") {
			state = strings.TrimSpace(strings.TrimPrefix(line, "Map State:"))
		} else if strings.HasPrefix(line, "Width:") {
			for _, field := range strings.Fields(strings.TrimPrefix(line, "Width:")) {
				n := 0
				ok := true
				for _, r := range field {
					if r < '0' || r > '9' {
						ok = false
						break
					}
					n = n*10 + int(r-'0')
				}
				if ok {
					width = n
				}
				break
			}
		}
	}
	return state, width
}

func displayStatusLive(out string) bool {
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
	return displayStatusLive(out)
}
