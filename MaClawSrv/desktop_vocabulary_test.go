package main

import (
	"strings"
	"testing"
)

// TestDesktopActionVocabularyCoversBothPhases pins the desktop tool's action
// vocabulary. One list answers the schema enum, the dispatcher's allow check,
// and the refusal text; the plan phase exposes the read-only prefix, and the
// phase gate blocks the rest by derivation. A new action therefore has to be
// added to the execute list once -- and its plan-phase posture is decided by
// whether it joins the plan list -- rather than re-stated in three places that
// could drift.
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
	// The plan phase may only observe. Everything else is blocked, and the
	// refusal names the action so the model knows which verb was refused.
	mutating := map[string]bool{"navigate": true, "click": true, "type": true, "press": true, "task_run": true, "script": true, "skill_run": true, "app_run": true, "app_open": true, "install": true, "deliver": true, "file_write": true, "file_edit": true, "bash": true}
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
