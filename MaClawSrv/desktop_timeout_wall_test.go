package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

func TestModalListAsksTheDesktopContainer(t *testing.T) {
	prev := desktopRemoteCtl
	t.Cleanup(func() { desktopRemoteCtl = prev })
	var ops []string
	desktopRemoteCtl = func(_ context.Context, _, _, display string, request map[string]any) (map[string]any, error) {
		if display != ":20" {
			t.Fatalf("display %q", display)
		}
		op, _ := request["op"].(string)
		ops = append(ops, op)
		if op == "windows" {
			return map[string]any{"ok": true, "windows": []any{
				map[string]any{"id": "0x41", "type": "_NET_WM_WINDOW_TYPE_DIALOG", "modal": true, "role": "alert"},
			}}, nil
		}
		if op == "window" {
			if request["do"] != "close" || request["id"] != "0x41" {
				t.Fatalf("close request %#v", request)
			}
			return map[string]any{"ok": true, "window": map[string]any{"id": "0x41", "closed": true}}, nil
		}
		t.Fatalf("op %s", op)
		return nil, nil
	}
	scope := agentruntime.Scope{TenantID: "t", UserID: "u", RunID: "run-modal"}
	desktopRoundWakes.Store(scope.RunID, &desktopRoundWake{woken: true, have: true, endpoint: desktopEndpoint{Display: ":20"}})
	t.Cleanup(func() { desktopRoundWakes.Delete(scope.RunID) })
	note, blocked := desktopBlockingDialog(scope)
	if !blocked || !strings.Contains(note, "dismissed") || !strings.Contains(note, "0x41") {
		t.Fatalf("dialog = %q blocked=%v", note, blocked)
	}
	if len(ops) != 2 || ops[0] != "windows" || ops[1] != "window" {
		t.Fatalf("ops %v", ops)
	}
}

func TestTimeoutUsesObservedFlagsNotTheURL(t *testing.T) {
	prevTargets := desktopTimeoutTargets
	prevFlags := desktopTimeoutFlags
	prevWindows := desktopListWMWindows
	t.Cleanup(func() {
		desktopTimeoutTargets = prevTargets
		desktopTimeoutFlags = prevFlags
		desktopListWMWindows = prevWindows
	})
	desktopListWMWindows = func(context.Context, agentruntime.Scope) []desktopWMWindow { return nil }
	desktopTimeoutTargets = func(agentruntime.Scope) []browser.TargetInfo {
		return []browser.TargetInfo{{ID: "tab", Type: "page", URL: "https://example.com/home"}}
	}
	desktopTimeoutFlags = func(agentruntime.Scope) (browser.BrowserPageFlags, bool) {
		return browser.BrowserPageFlags{}, true
	}
	if _, _, ok := desktopTimeoutHandoff(agentruntime.Scope{}, "type"); ok {
		t.Fatal("a url alone is not a login wall")
	}
	desktopTimeoutFlags = func(agentruntime.Scope) (browser.BrowserPageFlags, bool) {
		return browser.BrowserPageFlags{LoginWall: true}, true
	}
	text, ask, ok := desktopTimeoutHandoff(agentruntime.Scope{TenantID: "t", UserID: "u"}, "type")
	if !ok || !ask || !strings.Contains(text, "page flags: login_wall") || !strings.Contains(text, "https://example.com/home") {
		t.Fatalf("ask=%v ok=%v text=%q", ask, ok, text)
	}
}

func TestModalWindowIsReportedFromItsType(t *testing.T) {
	prevWindows := desktopListWMWindows
	prevDismiss := desktopDismissWMWindow
	t.Cleanup(func() {
		desktopListWMWindows = prevWindows
		desktopDismissWMWindow = prevDismiss
	})
	desktopListWMWindows = func(context.Context, agentruntime.Scope) []desktopWMWindow {
		return []desktopWMWindow{
			{ID: "0x1", Type: "_NET_WM_WINDOW_TYPE_NORMAL", Role: "browser"},
			{ID: "0x2", Type: "_NET_WM_WINDOW_TYPE_DIALOG", Role: "pop-up", Modal: true, Transient: true},
		}
	}
	dismissed := false
	desktopDismissWMWindow = func(_ context.Context, w desktopWMWindow) error {
		if w.ID != "0x2" {
			t.Fatalf("dismissed %s", w.ID)
		}
		dismissed = true
		return nil
	}
	text, ask, ok := desktopTimeoutHandoff(agentruntime.Scope{}, "wait")
	if !ok || ask || !dismissed || !strings.Contains(text, "It was dismissed") || strings.Contains(text, "0x1") {
		t.Fatalf("ask=%v ok=%v dismissed=%v text=%q", ask, ok, dismissed, text)
	}
	desktopDismissWMWindow = func(context.Context, desktopWMWindow) error { return errors.New("left open") }
	text, _, ok = desktopTimeoutHandoff(agentruntime.Scope{}, "click")
	if !ok || !strings.Contains(text, "still open") || strings.Contains(text, "It was dismissed") {
		t.Fatalf("text=%q", text)
	}
	if desktopWindowModal(desktopWMWindow{Type: "_NET_WM_WINDOW_TYPE_NORMAL", Role: "browser"}) {
		t.Fatal("a normal browser window is not a modal")
	}
	parsed := parseXPropWindow("0x9", "_NET_WM_WINDOW_TYPE(ATOM) = _NET_WM_WINDOW_TYPE_DIALOG\n_NET_WM_STATE(ATOM) = _NET_WM_STATE_MODAL\nWM_TRANSIENT_FOR(WINDOW): window id # 0x1\nWM_WINDOW_ROLE(STRING) = \"pop-up\"\nWM_NAME(STRING) = \"Save password?\"\n")
	if !desktopWindowModal(parsed) || parsed.Role != "pop-up" || parsed.ID != "0x9" {
		t.Fatalf("parsed %+v", parsed)
	}
}
