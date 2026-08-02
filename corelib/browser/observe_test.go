package browser

import (
	"strings"
	"testing"
)

func TestBrowserElementRefSupportsStableFields(t *testing.T) {
	ref := BrowserElementRef{
		Ref:                "@e1",
		Selector:           "button:nth-of-type(1)",
		SelectorCandidates: []string{"button:nth-of-type(1)", "[role=button]:nth-of-type(1)"},
		StableKey:          "button||submit|button|Submit|1",
	}
	if len(ref.SelectorCandidates) != 2 {
		t.Fatalf("SelectorCandidates len = %d", len(ref.SelectorCandidates))
	}
	if ref.StableKey == "" {
		t.Fatal("StableKey should not be empty")
	}
}

func TestBrowserElementRefSupportsMultipleSelectorCandidates(t *testing.T) {
	ref := BrowserElementRef{SelectorCandidates: []string{"#submit", "button[name=submit]", "button:nth-of-type(1)"}}
	if len(ref.SelectorCandidates) != 3 {
		t.Fatalf("SelectorCandidates len = %d", len(ref.SelectorCandidates))
	}
}

// TestObserveScriptGeneratesMultipleCandidates guards the injected observe
// script contract: it must build an ordered multi-candidate list (not a
// single positional selector) so actions.go's candidate retry has fallbacks.
func TestObserveScriptGeneratesMultipleCandidates(t *testing.T) {
	for _, marker := range []string{
		"selectorCandidatesFor",
		"pushUnique",
		"pushRaw",
		"data-testid",
		"aria-label",
		"nth-of-type",
		"selector_candidates: candidates",
	} {
		if !strings.Contains(browserObserveScript, marker) {
			t.Fatalf("observe script missing marker %q", marker)
		}
	}
	if strings.Contains(browserObserveScript, "selector_candidates: [selector]") {
		t.Fatal("observe script still emits a single selector candidate")
	}
}
