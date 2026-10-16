package task

import (
	"strings"
	"testing"
)

// TestStatusVocabularyCanonicalizesTheModelFacingSet pins the task vocabulary
// the same way the goal one is pinned: the enum only advertises names the
// record stores, the parser answers the same set it renders, and an empty
// status stays accepted-as-unset. No aliases exist here by pinned decision --
// the store never spelled them back, so teaching some would be inventing a
// vocabulary nobody verified.
func TestStatusVocabularyCanonicalizesTheModelFacingSet(t *testing.T) {
	options := StatusOptions()
	if len(options) != 5 {
		t.Fatalf("pending, in_progress, completed, failed, blocked are the reviewed set, got %v", options)
	}
	for _, option := range options {
		switch Status(option) {
		case StatusPending, StatusInProgress, StatusCompleted, StatusFailed, StatusBlocked:
		default:
			t.Fatalf("advertised status %q is not a lifecycle name the record stores", option)
		}
		canonical, ok := StatusCanonical(option)
		if !ok || canonical != Status(option) {
			t.Fatalf("canonical option %q maps to (%q, %v)", option, canonical, ok)
		}
	}
	if canonical, ok := StatusCanonical(""); !ok || canonical != "" {
		t.Fatalf("empty status = (%q, %v), want accepted-as-empty", canonical, ok)
	}
	// Case and padding fold away the same way every other vocabulary in the
	// hosts normalizes: they are spellings of the same five names, not new
	// ones.
	aliases := map[string]Status{"PENDING": StatusPending, " in_progress ": StatusInProgress, "COMPLETED": StatusCompleted}
	for sent, want := range aliases {
		canonical, ok := StatusCanonical(sent)
		if !ok || canonical != want {
			t.Fatalf("alias %q maps to (%q, %v), want %q", sent, canonical, ok, want)
		}
	}
	for _, refused := range []string{"active", "open", "done", "in-progress"} {
		if _, ok := StatusCanonical(refused); ok {
			t.Fatalf("invented status %q must not be accepted", refused)
		}
	}
	if hint := StatusHint(); !strings.Contains(hint, "pending") || !strings.Contains(hint, "in_progress") {
		t.Fatalf("hint %q does not name the canonical set", hint)
	}
}
