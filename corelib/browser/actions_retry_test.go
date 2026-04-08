package browser

import "testing"

func TestSelectorCandidatesForActionUsesRefCandidates(t *testing.T) {
	s := &BrowserAgentSession{
		lastSnapshotID: "snap-1",
		snapshots: map[string]*BrowserSnapshot{
			"snap-1": {Refs: []BrowserElementRef{{Ref: "@e1", Selector: "#submit", SelectorCandidates: []string{"button[name=submit]"}}}},
		},
	}
	candidates, ref, err := s.selectorCandidatesForAction("", "@e1", "")
	if err != nil {
		t.Fatalf("selectorCandidatesForAction error = %v", err)
	}
	if ref == nil || ref.Ref != "@e1" {
		t.Fatalf("resolved ref = %#v", ref)
	}
	if len(candidates) != 2 {
		t.Fatalf("candidates len = %d", len(candidates))
	}
}
