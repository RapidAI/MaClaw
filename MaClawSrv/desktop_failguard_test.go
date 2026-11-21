package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop/computeruse"
)

func TestDesktopErrorClass(t *testing.T) {
	cases := []struct {
		out  string
		err  error
		want string
	}{
		{"typed\n\n" + desktopNotAppliedMarker + ": x", nil, "not_applied"},
		{"", errors.New("browser policy blocked cross-origin navigation from a to b"), "cross_origin_block"},
		{"", errors.New("ref e12 is stale; run probe again"), "element_not_found"},
		{"", errors.New("wait timed out"), "timeout"},
		{"ok", nil, ""},
		{"", errors.New("not_password_field"), ""},
		{"", fmt.Errorf(`step 1: each step needs "action" (one of click): %w`, computeruse.ErrArgShape), "bad_args"},
		{"", errors.New("the password must be set before checkout"), "failed"},
		{"", errors.New("the window manager returned 1"), "failed"},
	}
	for _, c := range cases {
		if got := desktopErrorClass(c.out, c.err); got != c.want {
			t.Errorf("%q %v: got %q want %q", c.out, c.err, got, c.want)
		}
	}
}

func TestDesktopFailClassGuardBlocksSecondFailure(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: "u-failguard", InstanceID: "i", RunID: "run-failguard"}
	a1 := map[string]any{"action": "script", "script": "el.innerText='a'"}
	a2 := map[string]any{"action": "script", "script": "el.innerText='b'"}
	notApplied := "done\n\n" + desktopNotAppliedMarker
	if err := desktopFailClassCheck(scope, "script", a1); err != nil {
		t.Fatal(err)
	}
	if w := desktopFailClassRecord(scope, "script", a1, notApplied, nil); w != "" {
		t.Fatalf("first failure warned: %q", w)
	}
	w := desktopFailClassRecord(scope, "script", a2, notApplied, nil)
	if !strings.Contains(w, "Method blocked") || !strings.Contains(w, "not_applied") {
		t.Fatalf("second failure must warn: %q", w)
	}
	if err := desktopFailClassCheck(scope, "script", map[string]any{"action": "script", "script": "other()"}); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("blocked intent must be refused: %v", err)
	}
	// Other methods and the way out stay open.
	if err := desktopFailClassCheck(scope, "navigate", map[string]any{"action": "navigate"}); err != nil {
		t.Fatal(err)
	}
	if err := desktopFailClassCheck(scope, "request_takeover", map[string]any{"action": "request_takeover"}); err != nil {
		t.Fatal(err)
	}
	// Different error classes do not add up.
	desktopFailClassRecord(scope, "navigate", nil, "", errors.New("cross-origin"))
	desktopFailClassRecord(scope, "navigate", nil, "", errors.New("timed out"))
	if err := desktopFailClassCheck(scope, "navigate", map[string]any{"action": "navigate"}); err != nil {
		t.Fatal(err)
	}
}

func TestAppRunArgShapeBlocksTheNextCall(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: "u-apprun", InstanceID: "i", RunID: "run-apprun-shape"}
	first := map[string]any{"steps": []any{map[string]any{"x": 1, "y": 2}}}
	second := map[string]any{"steps": []any{map[string]any{"x": 9, "y": 8, "text": "other"}}}
	shape := fmt.Errorf(`step 1: each step needs "action"; received {x:number,y:number}: %w`, computeruse.ErrArgShape)
	if err := desktopFailClassCheck(scope, "app_run", first); err != nil {
		t.Fatal(err)
	}
	if w := desktopFailClassRecord(scope, "app_run", first, "", shape); w != "" {
		t.Fatalf("first arg error must not block yet: %q", w)
	}
	w := desktopFailClassRecord(scope, "app_run", second, "", shape)
	if !strings.Contains(w, "bad_args") {
		t.Fatalf("second arg error = %q", w)
	}
	if err := desktopFailClassCheck(scope, "app_run", map[string]any{"steps": []any{map[string]any{"x": 3}}}); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("third app_run must be refused: %v", err)
	}
	if err := desktopFailClassCheck(scope, "screenshot", map[string]any{}); err != nil {
		t.Fatal(err)
	}
}
