package tool

import (
	"strings"
	"testing"
)

// TestBrowserControlContractNeedsURLOnlyForNavigating pins the pairing the
// enum cannot express: url is declared optional in the schema, so which
// actions actually require one lives in the contract and nowhere else. A host
// that forgets to ask would send an empty url to the driver.
func TestBrowserControlContractNeedsURLOnlyForNavigating(t *testing.T) {
	actions := BrowserControlActions()
	if len(actions) == 0 {
		t.Fatal("browser control must expose at least one action")
	}
	seen := make(map[string]bool)
	for _, action := range actions {
		if seen[action] {
			t.Fatalf("action %q appears twice in the closed set", action)
		}
		seen[action] = true
		if !BrowserControlActionAllowed(action) {
			t.Fatalf("action %q is in the set but the allow check refuses it", action)
		}
	}
	for _, needsURL := range []string{"navigate"} {
		if !seen[needsURL] {
			t.Fatalf("url-bearing action %q is missing from the set %v", needsURL, actions)
		}
		if !BrowserControlNeedsURL(needsURL) {
			t.Fatalf("%s must require a url", needsURL)
		}
	}
	for _, action := range actions {
		if action == "navigate" {
			continue
		}
		if BrowserControlNeedsURL(action) {
			t.Fatalf("%s must not require a url", action)
		}
	}
}

// A rejected name is the point of the set: "open" is what a model guesses when
// the schema shows an unlabelled string, and it must stay refused however the
// model spells or pads it.
func TestBrowserControlContractRefusesInventedActions(t *testing.T) {
	for _, guessed := range []string{"open", "goto", "visit", "NAVIGATE", " navigate ", ""} {
		if BrowserControlActionAllowed(guessed) {
			t.Fatalf("invented action %q must not be allowed", guessed)
		}
	}
}

// The hint a rejection carries must list the real names, because a model that
// ignored the enum learns the vocabulary from its first failure. A hint naming
// an action the boundary refuses would teach the model to fail a second time.
func TestBrowserControlContractHintListsTheEnforcedSet(t *testing.T) {
	hint := BrowserControlActionHint()
	for _, action := range BrowserControlActions() {
		if !strings.Contains(hint, action) {
			t.Fatalf("hint %q does not mention allowed action %q", hint, action)
		}
		if !BrowserControlActionAllowed(action) {
			t.Fatalf("hint advertises %q but the boundary refuses it", action)
		}
	}
	// The legacy surface phrased the same failure as "unknown browser action:
	// open (supported: navigate, ...)"; the hint must stay at least as useful
	// by naming every name the turn could have used.
	if hint == "" || !strings.HasPrefix(hint, "allowed: ") {
		t.Fatalf("hint %q lost its allowed prefix", hint)
	}
}

// TestBrowserControlContractLeavesAnEffectMatchesTheURLBearingAction pins the
// third answer the contract gives about an action. It decides whether a lost
// reply must be reported as an unobserved outcome rather than a failure, so a
// host that got it wrong would tell the user a navigation might not have
// happened when it may already have, or claim uncertainty about a read.
func TestBrowserControlContractLeavesAnEffectMatchesTheURLBearingAction(t *testing.T) {
	for _, action := range BrowserControlActions() {
		if BrowserControlLeavesAnEffect(action) != BrowserControlNeedsURL(action) {
			t.Fatalf("%s: leavesAnEffect and needsURL disagree", action)
		}
	}
	if !BrowserControlLeavesAnEffect("navigate") {
		t.Fatal("navigating can move server state, so a lost reply must stay unobserved")
	}
	if BrowserControlLeavesAnEffect("snapshot") {
		t.Fatal("observing leaves nothing behind, so a lost reply is a plain failure")
	}
	// An unknown action must not silently read as effect-free: the caller only
	// reaches this after the allow check, but a wrong answer there would turn a
	// real navigation into a definite failure.
	if BrowserControlLeavesAnEffect("open") {
		t.Fatal("an action outside the set must not be treated as effect-bearing")
	}
}

// TestWriteModeOptionsMatchTheCanonicalizer pins the file-write mode set
// against the boundary that enforces it. The vocabulary lives with the
// WriteMode constants; the enum rendering and the empty-is-overwrite reading
// both answer to NormalizeWriteModeKind, so they must never disagree.
func TestWriteModeOptionsMatchTheCanonicalizer(t *testing.T) {
	options := WriteModeOptions()
	if len(options) != 2 {
		t.Fatalf("overwrite and append are the reviewed modes, got %v", options)
	}
	for _, option := range options {
		if _, err := NormalizeWriteModeKind(option); err != nil {
			t.Fatalf("advertised mode %q is refused by the canonicalizer: %v", option, err)
		}
	}
	// The schema declares mode optional, so the empty reading (overwrite)
	// stays in the boundary and out of the advertised set.
	beats, err := NormalizeWriteModeKind("")
	if err != nil || beats != WriteModeOverwrite {
		t.Fatalf("empty mode = (%q, %v), want overwrite-as-default", beats, err)
	}
	for _, invented := range []string{"overwrite_append", "replace", "write"} {
		if _, err := NormalizeWriteModeKind(invented); err == nil {
			t.Fatalf("invented mode %q must not be accepted", invented)
		}
	}
	if hint := WriteModeHint(); !strings.Contains(hint, "overwrite") || !strings.Contains(hint, "append") {
		t.Fatalf("hint %q does not name the canonical set", hint)
	}
}
