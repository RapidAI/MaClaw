package tool

import (
	"strings"
	"testing"
)

// TestScheduleStatusContractCanonicalizesTheAdvertisedVocabulary pins the
// alias shape of this set. Unlike the browser, desktop, and repo-mutate
// actions, the status vocabulary answers two different questions: what the
// schema advertises (canonical names only) and what the runtime accepts
// (canonical names plus the aliases a model naturally reaches for). A host
// that advertised an alias would teach two spellings for one fact; a host
// that stopped accepting one would strand the other spelling's turn. Each
// direction below pins one of those two failures.
func TestScheduleStatusContractCanonicalizesTheAdvertisedVocabulary(t *testing.T) {
	options := ScheduleStatusOptions()
	if len(options) == 0 {
		t.Fatal("schedule status must expose at least one option")
	}
	if len(options) != 2 {
		t.Fatalf("the reviewed canonical set is paused and active, got %v", options)
	}
	for _, option := range options {
		canonical, ok := ScheduleStatusCanonical(option)
		if !ok || canonical != option {
			t.Fatalf("canonical option %q maps to (%q, %v)", option, canonical, ok)
		}
	}
	aliases := map[string]string{"pause": "paused", "resume": "active", "PAUSED": "paused", " active ": "active"}
	for sent, want := range aliases {
		canonical, ok := ScheduleStatusCanonical(sent)
		if !ok || canonical != want {
			t.Fatalf("alias %q maps to (%q, %v), want %q", sent, canonical, ok, want)
		}
	}
	// Omitting status must read the same as sending nothing: empty stays
	// accepted-as-empty, and padding that to spaces is not a third value.
	if canonical, ok := ScheduleStatusCanonical(""); !ok || canonical != "" {
		t.Fatalf("empty status = (%q, %v), want accepted-as-empty", canonical, ok)
	}
	for _, invented := range []string{"disabled", "enabled", "off", "running"} {
		if _, ok := ScheduleStatusCanonical(invented); ok {
			t.Fatalf("invented status %q must not be accepted", invented)
		}
	}
}

// The hint a rejection carries must list the canonical names, which is the
// same vocabulary the enum renders — never an alias, so the model learns the
// spelling the store will spell back.
func TestScheduleStatusContractHintListsCanonicalOnly(t *testing.T) {
	hint := ScheduleStatusHint()
	for _, option := range ScheduleStatusOptions() {
		if !strings.Contains(hint, option) {
			t.Fatalf("hint %q does not mention canonical status %q", hint, option)
		}
	}
	for alias := range map[string]string{"pause": "", "resume": ""} {
		if strings.Contains(" "+hint+" ", " "+alias+" ") {
			t.Fatalf("hint %q advertises alias %q; aliases are accepted but not advertised", hint, alias)
		}
	}
	if hint == "" || !strings.HasPrefix(hint, "allowed: ") {
		t.Fatalf("hint %q lost its allowed prefix", hint)
	}
}

// TestKnowledgeAdminStatusContractCanonicalizesTheAdvertisedSet pins the
// knowledge-admin vocabulary: canonical names advertised, imperative aliases
// accepted-but-unadvertised, dropped/deleted mapped by the record's own
// spelling, and empty staying accepted-as-unset.
func TestKnowledgeAdminStatusContractCanonicalizesTheAdvertisedSet(t *testing.T) {
	options := KnowledgeAdminStatusOptions()
	if len(options) != 3 {
		t.Fatalf("disabled, enabled, deleted are the reviewed set, got %v", options)
	}
	for _, option := range options {
		canonical, ok := KnowledgeAdminStatusCanonical(option)
		if !ok || canonical != option {
			t.Fatalf("canonical option %q maps to (%q, %v)", option, canonical, ok)
		}
	}
	aliases := map[string]string{"disable": "disabled", "enable": "enabled", "active": "enabled", "delete": "deleted"}
	for sent, want := range aliases {
		canonical, ok := KnowledgeAdminStatusCanonical(sent)
		if !ok || canonical != want {
			t.Fatalf("alias %q maps to (%q, %v), want %q", sent, canonical, ok, want)
		}
	}
	if canonical, ok := KnowledgeAdminStatusCanonical(""); !ok || canonical != "" {
		t.Fatalf("empty status = (%q, %v), want accepted-as-empty", canonical, ok)
	}
	for _, invented := range []string{"archived", "paused", "purged"} {
		if _, ok := KnowledgeAdminStatusCanonical(invented); ok {
			t.Fatalf("invented status %q must not be accepted", invented)
		}
	}
}

// The hint carries the canonical names only, so a model that learns from its
// first failure learns the spelling the record spells back.
func TestKnowledgeAdminStatusContractHintListsCanonicalOnly(t *testing.T) {
	hint := KnowledgeAdminStatusHint()
	for _, option := range KnowledgeAdminStatusOptions() {
		if !strings.Contains(hint, option) {
			t.Fatalf("hint %q does not mention canonical status %q", hint, option)
		}
	}
	for alias := range map[string]string{"disable": "", "enable": "", "delete": ""} {
		if strings.Contains(" "+hint+" ", " "+alias+" ") {
			t.Fatalf("hint %q advertises alias %q; aliases are accepted but not advertised", hint, alias)
		}
	}
}
