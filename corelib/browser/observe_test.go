package browser

import "testing"

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
