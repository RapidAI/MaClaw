package tool

import (
	"strings"
	"testing"
)

// TestRepoMutateContractPinsTheActionTable pins the three answers the contract
// gives about each action: that it is allowed, and what it pairs with. Commit
// stages and records work, so it must carry a message; push publishes an
// already-recorded history, so it must not be forced to repeat one.
func TestRepoMutateContractPinsTheActionTable(t *testing.T) {
	actions := RepoMutateActions()
	if len(actions) == 0 {
		t.Fatal("repo mutate must expose at least one action")
	}
	seen := make(map[string]bool)
	for _, action := range actions {
		if seen[action] {
			t.Fatalf("action %q appears twice in the closed set", action)
		}
		seen[action] = true
		if !RepoMutateActionAllowed(action) {
			t.Fatalf("action %q is in the set but the allow check refuses it", action)
		}
	}
	if !seen["commit"] || !seen["push"] {
		t.Fatalf("the reviewed set is commit and push, got %v", actions)
	}
	if !RepoMutateNeedsMessage("commit") {
		t.Fatal("commit without a message would record unnamed work")
	}
	if RepoMutateNeedsMessage("push") {
		t.Fatal("push publishes recorded history; forcing a message would change what push means")
	}
	for _, guessed := range []string{"save", "revert", "COMMIT", " commit ", ""} {
		if RepoMutateActionAllowed(guessed) {
			t.Fatalf("invented action %q must not be allowed", guessed)
		}
	}
}

// The hint a rejection carries must list the real names, so a model that
// ignored the enum learns the vocabulary from its first failure.
func TestRepoMutateContractHintListsTheEnforcedSet(t *testing.T) {
	hint := RepoMutateActionHint()
	for _, action := range RepoMutateActions() {
		if !strings.Contains(hint, action) {
			t.Fatalf("hint %q does not mention allowed action %q", hint, action)
		}
		if !RepoMutateActionAllowed(action) {
			t.Fatalf("hint advertises %q but the boundary refuses it", action)
		}
	}
	if hint == "" || !strings.HasPrefix(hint, "allowed: ") {
		t.Fatalf("hint %q lost its allowed prefix", hint)
	}
}
