package tool

import (
	"strings"
	"testing"
)

// TestComputerControlContractRefusesInventedActions is the desktop analogue of
// the browser contract test. No invented desktop action name has reached
// production yet, so unlike the browser case there is no incident behind it --
// the enum is being closed before the guess rather than after.
func TestComputerControlContractRefusesInventedActions(t *testing.T) {
	actions := ComputerControlActions()
	if len(actions) == 0 {
		t.Fatal("computer control must expose at least one action")
	}
	seen := make(map[string]bool)
	for _, action := range actions {
		if seen[action] {
			t.Fatalf("action %q appears twice in the closed set", action)
		}
		seen[action] = true
		if !ComputerControlActionAllowed(action) {
			t.Fatalf("action %q is in the set but the allow check refuses it", action)
		}
	}
	for _, guessed := range []string{"open", "screenshot", "press", "OBSERVE", " observe ", ""} {
		if ComputerControlActionAllowed(guessed) {
			t.Fatalf("invented action %q must not be allowed", guessed)
		}
	}
}

// The hint a rejection carries must list the real names, so a model that
// ignored the enum learns the vocabulary from its first failure.
func TestComputerControlContractHintListsTheEnforcedSet(t *testing.T) {
	hint := ComputerControlActionHint()
	for _, action := range ComputerControlActions() {
		if !strings.Contains(hint, action) {
			t.Fatalf("hint %q does not mention allowed action %q", hint, action)
		}
		if !ComputerControlActionAllowed(action) {
			t.Fatalf("hint advertises %q but the boundary refuses it", action)
		}
	}
	if hint == "" || !strings.HasPrefix(hint, "allowed: ") {
		t.Fatalf("hint %q lost its allowed prefix", hint)
	}
}
