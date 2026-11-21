package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// TestDesktopVerifyExpectParsesWindowAndElement covers the two expectation
// shapes a model writes, and that a wrong shape is refused with a message
// that names the right one.
func TestDesktopVerifyExpectParsesWindowAndElement(t *testing.T) {
	win, err := desktopParseExpect(map[string]any{"window": map[string]any{"exists": true}})
	if err != nil {
		t.Fatalf("window exists: %v", err)
	}
	if win.Window == nil || win.Window.Exists == nil || !*win.Window.Exists {
		t.Fatalf("window exists parsed as %+v", win)
	}

	el, err := desktopParseExpect(map[string]any{"element": map[string]any{
		"selector": map[string]any{"label_contains": "保存"},
		"enabled":  true,
	}})
	if err != nil {
		t.Fatalf("element enabled: %v", err)
	}
	if el.Element == nil || el.Element.Enabled == nil || !*el.Element.Enabled {
		t.Fatalf("element parsed as %+v", el)
	}
	if el.Element.Selector.Label != "保存" {
		t.Fatalf("selector label = %q", el.Element.Selector.Label)
	}

	if _, err := desktopParseExpect(map[string]any{"element": map[string]any{
		"selector": map[string]any{"label_contains": "x"}, "exists": false,
	}}); err == nil || !strings.Contains(err.Error(), "window") {
		t.Fatalf("element absence must point at the window check, got %v", err)
	}
	if _, err := desktopParseExpect(map[string]any{"element": map[string]any{}}); err == nil {
		t.Fatal("an element check without a selector must be refused")
	}
}

// TestDesktopSecondsArg pins the duration reading: numbers are seconds,
// strings are durations, and both stay within the bound.
func TestDesktopSecondsArg(t *testing.T) {
	cases := []struct {
		in   any
		want time.Duration
	}{
		{float64(5), 5 * time.Second},
		{"3s", 3 * time.Second},
		{"500ms", 500 * time.Millisecond},
		{float64(1000), 120 * time.Second}, // clamped
		{"nonsense", 20 * time.Second},     // default
	}
	for _, c := range cases {
		got := desktopSecondsArg(map[string]any{"timeout": c.in}, "timeout", 20*time.Second, 120*time.Second)
		if got != c.want {
			t.Errorf("timeout %v = %s, want %s", c.in, got, c.want)
		}
	}
}

// TestDesktopWaitNeedsATarget keeps a bare wait from blocking on nothing.
func TestDesktopWaitNeedsATarget(t *testing.T) {
	_, err := operateDesktopWaitUntil(context.Background(), agentruntime.Scope{RunID: "test"}, map[string]any{})
	if err == nil {
		t.Fatal("wait_until without a title or app must be refused")
	}
}

// TestDesktopCuaActionsAreListed guards the two action names against a list
// edit that drops them: the model only sees the names in that list.
func TestDesktopCuaActionsAreListed(t *testing.T) {
	for _, want := range []string{"verify_state", "wait_until"} {
		found := false
		for _, action := range desktopExecuteActions {
			if action == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("action %q is not in desktopExecuteActions", want)
		}
	}
}
