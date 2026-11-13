package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// desktopWMWindow is one top-level window as the window manager reports it.
// Kind comes from the window type and modal/transient state, never the title.
type desktopWMWindow struct {
	ID        string
	Type      string
	Role      string
	Modal     bool
	Transient bool
	// scope and display name the desktop container this window was listed
	// from. Closing it goes back through that container's supervisor.
	scope   agentruntime.Scope
	display string
}

// desktopTimeoutTargets lists browser targets through CDP Target.getTargets.
// Tests replace it. Production does not evaluate in the renderer.
var desktopTimeoutTargets = func(scope agentruntime.Scope) []browser.TargetInfo {
	session := desktopLiveSession(scope)
	if session == nil || strings.TrimSpace(session.Addr) == "" {
		return nil
	}
	targets, err := browser.DiscoverTargets(session.Addr)
	if err != nil {
		return nil
	}
	return targets
}

// desktopTimeoutFlags is the last page observation. It does not evaluate
// the page again; a hung renderer must not be asked to answer.
var desktopTimeoutFlags = func(scope agentruntime.Scope) (browser.BrowserPageFlags, bool) {
	session := desktopLiveSession(scope)
	if session == nil {
		return browser.BrowserPageFlags{}, false
	}
	return session.ObservedPageFlags()
}

// desktopListWMWindows enumerates windows. Tests replace it. Production
// reads the window manager; an empty list means none were seen.
var desktopListWMWindows = listDesktopWMWindowsLive

// desktopDismissWMWindow closes one window. The report says the dialog was
// dismissed only when this returns nil.
var desktopDismissWMWindow = dismissDesktopWMWindowLive

var errDesktopDialogLeftOpen = errors.New("dialog was not dismissed")

// desktopWindowModal is a browser dialog the window manager marks as a
// dialog that is modal or transient for another window.
//
// The list and the close both go through the desktop control service, which
// runs wmctrl and xprop inside the container on that bot's display. This
// process has no display of its own.
func listDesktopWMWindowsLive(ctx context.Context, scope agentruntime.Scope) []desktopWMWindow {
	display := ""
	if endpoint, ok := desktopRoundAwake(scope); ok {
		display = endpoint.Display
	}
	out, err := desktopCtlCall(ctx, scope, display, map[string]any{"op": "windows", "all": true})
	if err != nil || out == nil {
		return nil
	}
	raw, _ := out["windows"].([]any)
	windows := make([]desktopWMWindow, 0, len(raw))
	for _, item := range raw {
		row, _ := item.(map[string]any)
		if row == nil {
			continue
		}
		id, _ := row["id"].(string)
		if strings.TrimSpace(id) == "" {
			continue
		}
		kind, _ := row["type"].(string)
		role, _ := row["role"].(string)
		modal, _ := row["modal"].(bool)
		transient, _ := row["transient"].(bool)
		windows = append(windows, desktopWMWindow{
			ID: id, Type: kind, Role: role, Modal: modal, Transient: transient,
			scope: scope, display: display,
		})
	}
	return windows
}

func dismissDesktopWMWindowLive(ctx context.Context, w desktopWMWindow) error {
	if strings.TrimSpace(w.ID) == "" {
		return errDesktopDialogLeftOpen
	}
	out, err := desktopCtlCall(ctx, w.scope, w.display, map[string]any{"op": "window", "do": "close", "id": w.ID})
	if err != nil {
		return err
	}
	window, _ := out["window"].(map[string]any)
	if window == nil {
		return errDesktopDialogLeftOpen
	}
	closed, _ := window["closed"].(bool)
	if !closed {
		return errDesktopDialogLeftOpen
	}
	return nil
}

// parseXPropWindow reads window type, role, and modal/transient state from
// an xprop dump. The title is not consulted.
func parseXPropWindow(id, dump string) desktopWMWindow {
	w := desktopWMWindow{ID: strings.TrimSpace(id)}
	if strings.Contains(dump, "_NET_WM_WINDOW_TYPE_DIALOG") {
		w.Type = "_NET_WM_WINDOW_TYPE_DIALOG"
	} else if i := strings.Index(dump, "_NET_WM_WINDOW_TYPE"); i >= 0 {
		rest := dump[i:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[:nl]
		}
		w.Type = strings.TrimSpace(rest)
	}
	w.Modal = strings.Contains(dump, "_NET_WM_STATE_MODAL")
	w.Transient = strings.Contains(dump, "WM_TRANSIENT_FOR")
	const roleKey = `WM_WINDOW_ROLE(STRING) = `
	if i := strings.Index(dump, roleKey); i >= 0 {
		rest := strings.Trim(dump[i+len(roleKey):], " \t\"")
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[:nl]
		}
		w.Role = strings.Trim(rest, "\"")
	}
	return w
}

func desktopWindowModal(w desktopWMWindow) bool {
	if !strings.Contains(w.Type, "_NET_WM_WINDOW_TYPE_DIALOG") {
		return false
	}
	return w.Modal || w.Transient
}

// desktopBlockingDialog reports a native modal dialog. The sentence says
// it was dismissed only after the dismiss call succeeds.
func desktopBlockingDialog(scope agentruntime.Scope) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, w := range desktopListWMWindows(ctx, scope) {
		if !desktopWindowModal(w) {
			continue
		}
		note := "a browser dialog is in front of the page (modal window " + strings.TrimSpace(w.ID) + ")."
		if err := desktopDismissWMWindow(ctx, w); err != nil {
			return note + " It is still open. Stay on the web page; do not wait for it and do not open an external application.", true
		}
		return note + " It was dismissed. Stay on the web page; do not wait for it and do not open an external application.", true
	}
	return "", false
}

func desktopTimeoutPageURL(scope agentruntime.Scope) string {
	session := desktopLiveSession(scope)
	attached := ""
	if session != nil {
		attached = strings.TrimSpace(session.TargetID)
	}
	var fallback string
	for _, t := range desktopTimeoutTargets(scope) {
		if t.Type != "" && t.Type != "page" {
			continue
		}
		if strings.TrimSpace(t.URL) == "" {
			continue
		}
		if attached != "" && t.ID == attached {
			return t.URL
		}
		if fallback == "" {
			fallback = t.URL
		}
	}
	return fallback
}

func desktopFlagLine(flags browser.BrowserPageFlags) string {
	var names []string
	if flags.LoginQR {
		names = append(names, "login_qr")
	}
	if flags.LoginWall {
		names = append(names, "login_wall")
	}
	if flags.MFA {
		names = append(names, "mfa")
	}
	if flags.CaptchaWidget || flags.Captcha {
		names = append(names, "captcha_widget")
	}
	if flags.PaymentConfirm {
		names = append(names, "payment_confirm")
	}
	if flags.ConsentDialog {
		names = append(names, "consent_dialog")
	}
	if flags.BlockingNotice != "" {
		names = append(names, "blocking_notice")
	}
	return strings.Join(names, ",")
}

// desktopTimeoutHandoff is what a hard timeout should say instead of "the
// page is slow". Every timed-out tool is covered. ok is false when neither
// observed page flags nor a modal window was seen.
func desktopTimeoutHandoff(scope agentruntime.Scope, action string) (text string, ask bool, ok bool) {
	_ = action
	if text, blocked := desktopBlockingDialog(scope); blocked {
		return text, false, true
	}
	flags, have := desktopTimeoutFlags(scope)
	line := ""
	if have {
		line = desktopFlagLine(flags)
	}
	raw := strings.TrimSpace(desktopTimeoutPageURL(scope))
	if line == "" {
		return "", false, false
	}
	text = "page flags: " + line
	if raw != "" {
		text += "\nurl " + raw
	}
	return text, true, true
}
