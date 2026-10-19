package main

import (
	"strings"
	"testing"
)

// TestDesktopActionVocabularyCoversBothPhases pins the desktop tool's action
// vocabulary. Plan and execute advertise the execute list. The plan allow-list
// is the phase gate: a mutating action stays in the schema and is refused by
// desktopPlanActionAllowed. A new action is added to the execute list once.
func TestDesktopActionVocabularyCoversBothPhases(t *testing.T) {
	if len(desktopExecuteActions) == 0 {
		t.Fatal("desktop tool must expose at least one action")
	}
	seen := make(map[string]bool)
	for _, action := range desktopExecuteActions {
		if seen[action] {
			t.Fatalf("action %q appears twice in the closed set", action)
		}
		seen[action] = true
		if !desktopActionAllowed(action) {
			t.Fatalf("action %q is in the set but the dispatcher refuses it", action)
		}
	}
	for _, action := range desktopPlanActions {
		if !seen[action] {
			t.Fatalf("plan-phase action %q must be a member of the execute set", action)
		}
	}
	// The plan allow-list may only observe. Mutating actions stay out of that
	// list. The published schema still names them.
	mutating := map[string]bool{"navigate": true, "click": true, "type": true, "press": true, "task_run": true, "script": true, "skill_run": true, "app_run": true, "app_open": true, "install": true, "deliver": true, "file_write": true, "file_edit": true, "bash": true, "computer": true, "clipboard_set": true, "window": true, "request_takeover": true}
	for _, action := range desktopExecuteActions {
		if mutating[action] && desktopPlanActionAllowed(action) {
			t.Fatalf("plan phase must not expose mutating action %q", action)
		}
		if !mutating[action] && !desktopPlanActionAllowed(action) {
			t.Fatalf("read-only action %q is missing from the plan vocabulary", action)
		}
	}
	for _, invented := range []string{"open", "goto", "reboot", ""} {
		if desktopActionAllowed(invented) {
			t.Fatalf("invented action %q must not be allowed", invented)
		}
	}
	// The refusal must name the real vocabulary: this surface's rejection was
	// already self-healing before the enum existed, and it must stay so.
	refusal := "desktop action must be " + strings.Join(desktopExecuteActions, ", ")
	for _, action := range desktopExecuteActions {
		if !strings.Contains(refusal, action) {
			t.Fatalf("refusal %q does not mention action %q", refusal, action)
		}
	}
}
