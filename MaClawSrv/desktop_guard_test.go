package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestDesktopRepeatCheckWarnsThenRefuses(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: "u", RunID: "repeat-run"}
	args := map[string]any{"action": "clipboard_get"}
	for i := 1; i <= 2; i++ {
		if w, err := desktopRepeatCheck(scope, "clipboard_get", args); w != "" || err != nil {
			t.Fatalf("call %d: warn=%q err=%v", i, w, err)
		}
	}
	if w, err := desktopRepeatCheck(scope, "clipboard_get", args); err != nil || !strings.Contains(w, "3 times") {
		t.Fatalf("third call should warn: %q %v", w, err)
	}
	if _, err := desktopRepeatCheck(scope, "clipboard_get", args); err == nil || !strings.Contains(err.Error(), "4 times") {
		t.Fatalf("fourth call should be refused: %v", err)
	}
	// A different call resets the streak.
	if w, err := desktopRepeatCheck(scope, "screenshot", map[string]any{"action": "screenshot"}); w != "" || err != nil {
		t.Fatalf("changed call: %q %v", w, err)
	}
	if w, err := desktopRepeatCheck(scope, "clipboard_get", args); w != "" || err != nil {
		t.Fatalf("streak not reset: %q %v", w, err)
	}
	// Exempt actions never trip it.
	for i := 0; i < 6; i++ {
		if _, err := desktopRepeatCheck(scope, "request_takeover", map[string]any{"action": "request_takeover"}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDesktopPinShotSize(t *testing.T) {
	t.Setenv(desktopShotWidthEnv, "")
	got := desktopPinShotSize(desktop.ShotOptions{MaxWidth: 1100, MaxHeight: 688})
	if got.MaxWidth != 0 || got.MaxHeight != 0 {
		t.Fatalf("default must be native size, got %+v", got)
	}
	t.Setenv(desktopShotWidthEnv, "1280")
	if got := desktopPinShotSize(desktop.ShotOptions{MaxWidth: 1100}); got.MaxWidth != 1280 || got.MaxHeight != 0 {
		t.Fatalf("pinned width not applied: %+v", got)
	}
	zoom := desktop.ShotOptions{MaxWidth: 500, Region: desktop.ShotRegion{X: 1, Y: 1, Width: 100, Height: 100}}
	if got := desktopPinShotSize(zoom); got.MaxWidth != 500 {
		t.Fatalf("region zoom must keep its own size: %+v", got)
	}
}

func TestDesktopBudgetLogFields(t *testing.T) {
	s := desktopBudgetLogFields(agentruntime.Scope{TenantID: "t", UserID: "u", RunID: "log-run"})
	for _, k := range []string{"step=", "actions=", "image_tokens=", "batch_max="} {
		if !strings.Contains(s, k) {
			t.Fatalf("missing %s in %q", k, s)
		}
	}
	if desktopModelLabel(agentruntime.TurnRequest{}) != "unknown" {
		t.Fatal("no config should log unknown")
	}
}
