package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop/computeruse"
)

// The person watches the desktop. An app the bot starts has to come to the
// front, or Chromium (raised by every CDP attach) stays on top and the
// person sees nothing of the work. app_open and a bash command that starts
// a GUI program wait for the new window and activate it.

var (
	desktopRaiseWait = 12 * time.Second
	desktopRaisePoll = 500 * time.Millisecond
)

// desktopGUILaunch matches a bash command that starts a windowed program.
// Headless conversions (soffice --headless, --convert-to) open no window.
var desktopGUILaunch = regexp.MustCompile(`(?:^|[\s;&|(])(?:soffice|libreoffice|lowriter|localc|loimpress|evince|mousepad|gedit|eog|ristretto|thunar|xdg-open|gimp|xfce4-terminal|okular|atril|gnome-text-editor|gio\s+open)(?:\s|$|&)`)

func desktopBashLaunchesGUI(command string) bool {
	if strings.Contains(command, "--headless") || strings.Contains(command, "--convert-to") {
		return false
	}
	return desktopGUILaunch.MatchString(command)
}

type desktopWindowSeen struct {
	id, title, class string
	active           bool
}

func desktopWindowsNow(ctx context.Context, scope agentruntime.Scope, display string) ([]desktopWindowSeen, error) {
	out, err := computeruse.CtlOK(desktopCtlCall(ctx, scope, display, map[string]any{"op": "windows"}))
	if err != nil {
		return nil, err
	}
	raw, _ := out["windows"].([]any)
	seen := make([]desktopWindowSeen, 0, len(raw))
	for _, item := range raw {
		w, _ := item.(map[string]any)
		id, _ := w["id"].(string)
		if id == "" {
			continue
		}
		title, _ := w["title"].(string)
		class, _ := w["class"].(string)
		active, _ := w["active"].(bool)
		seen = append(seen, desktopWindowSeen{id: id, title: title, class: class, active: active})
	}
	return seen, nil
}

func desktopWindowIDs(windows []desktopWindowSeen) map[string]bool {
	ids := make(map[string]bool, len(windows))
	for _, w := range windows {
		ids[w.id] = true
	}
	return ids
}

func desktopIsBrowserClass(class string) bool {
	c := strings.ToLower(class)
	return strings.Contains(c, "chromium") || strings.Contains(c, "chrome")
}

// desktopRaiseNewWindow waits for a window that was not in before and
// activates the topmost one. hint (program name) picks an existing window
// of that program when the program reused its process without a new
// window. The returned sentence is for the model; empty means nothing to say.
func desktopRaiseNewWindow(ctx context.Context, scope agentruntime.Scope, display string, before map[string]bool, hint string) string {
	hint = strings.ToLower(strings.TrimSpace(hint))
	deadline := time.Now().Add(desktopRaiseWait)
	var last []desktopWindowSeen
	for {
		windows, err := desktopWindowsNow(ctx, scope, display)
		if err == nil {
			last = windows
			for i := len(windows) - 1; i >= 0; i-- {
				w := windows[i]
				if before[w.id] || desktopIsBrowserClass(w.class) && !strings.Contains(hint, "chrom") {
					continue
				}
				return desktopActivateWindow(ctx, scope, display, w)
			}
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			break
		}
		time.Sleep(desktopRaisePoll)
	}
	if hint != "" {
		for i := len(last) - 1; i >= 0; i-- {
			w := last[i]
			if strings.Contains(strings.ToLower(w.class), hint) || strings.Contains(strings.ToLower(w.title), hint) {
				return desktopActivateWindow(ctx, scope, display, w)
			}
		}
	}
	return fmt.Sprintf("No new window appeared within %ds; if the program should show a window, check window_list and activate it so the person can see it.", int(desktopRaiseWait/time.Second))
}

func desktopActivateWindow(ctx context.Context, scope agentruntime.Scope, display string, w desktopWindowSeen) string {
	if w.active {
		return fmt.Sprintf("Window %s %q is in front.", w.id, w.title)
	}
	if _, err := computeruse.CtlOK(desktopCtlCall(ctx, scope, display, map[string]any{"op": "window", "do": "activate", "id": w.id})); err != nil {
		return fmt.Sprintf("Window %s %q opened but could not be brought to front: %v", w.id, w.title, err)
	}
	return fmt.Sprintf("Window %s %q is now in front.", w.id, w.title)
}

// operateDesktopBashGUI runs a bash command that starts a windowed program
// and brings the program's new window to the front.
func operateDesktopBashGUI(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
	display := ""
	if userKey, err := desktopUserKey(scope); err == nil {
		if endpoint, err := ensureDesktopRound(ctx, scope, userKey, true); err == nil && validateDesktopEndpoint(endpoint) == nil {
			display = endpoint.Display
		}
	}
	var before []desktopWindowSeen
	var listErr error = fmt.Errorf("no display")
	if display != "" {
		before, listErr = desktopWindowsNow(ctx, scope, display)
	}
	text, err := operateDesktopContainer(ctx, scope, "bash", args)
	if err != nil || listErr != nil {
		return text, err
	}
	if note := desktopRaiseNewWindow(ctx, scope, display, desktopWindowIDs(before), ""); note != "" {
		text = strings.TrimSpace(text + "\n" + note)
	}
	return text, nil
}
