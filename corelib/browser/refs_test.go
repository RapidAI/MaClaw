package browser

import "testing"

func TestSelectorCandidatesForRefDeduplicates(t *testing.T) {
	s := &BrowserAgentSession{
		lastSnapshotID: "snap-1",
		snapshots: map[string]*BrowserSnapshot{
			"snap-1": {
				Refs: []BrowserElementRef{{
					Ref:                "@e1",
					Selector:           "#submit",
					SelectorCandidates: []string{"#submit", "button[name=submit]", "button[name=submit]"},
				}},
			},
		},
	}
	candidates, ref, err := s.selectorCandidatesForRef("", "@e1")
	if err != nil {
		t.Fatalf("selectorCandidatesForRef error = %v", err)
	}
	if ref == nil || ref.Ref != "@e1" {
		t.Fatalf("resolved ref = %#v", ref)
	}
	if len(candidates) != 2 {
		t.Fatalf("candidates len = %d, want 2", len(candidates))
	}
	if candidates[0] != "#submit" || candidates[1] != "button[name=submit]" {
		t.Fatalf("candidates = %#v", candidates)
	}
}

func TestSelectorCandidatesForTextUsesLatestSnapshot(t *testing.T) {
	s := &BrowserAgentSession{
		lastSnapshotID: "snap-1",
		snapshots: map[string]*BrowserSnapshot{
			"snap-1": {
				Refs: []BrowserElementRef{{
					Ref:      "@e1",
					Name:     "Publish",
					Selector: "button.share",
				}},
			},
		},
	}
	candidates, ref, err := s.selectorCandidatesForText("", "Publish")
	if err != nil {
		t.Fatalf("selectorCandidatesForText error = %v", err)
	}
	if ref == nil || ref.Ref != "@e1" {
		t.Fatalf("resolved ref = %#v", ref)
	}
	if len(candidates) != 1 || candidates[0] != "button.share" {
		t.Fatalf("candidates = %#v", candidates)
	}
}

func TestSelectorCandidatesFromResolvedRefErrorIsReadable(t *testing.T) {
	_, _, err := selectorCandidatesFromResolvedRef(&BrowserElementRef{Ref: "@e9"})
	if err == nil {
		t.Fatal("expected missing selector error")
	}
	if got := err.Error(); got != "ref @e9 has no selector candidates; run observe again" {
		t.Fatalf("error = %q", got)
	}
}
