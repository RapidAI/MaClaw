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

func TestGuardSubmitClickRejectsRecentDuplicate(t *testing.T) {
	s := &BrowserAgentSession{}
	key := "https://www.zhihu.com/pin|发布"
	if err := s.guardSubmitClick(key); err != nil {
		t.Fatalf("first guardSubmitClick error = %v", err)
	}
	s.rememberSubmitClick(key)
	if err := s.guardSubmitClick(key); err == nil {
		t.Fatalf("second guardSubmitClick error = nil, want duplicate rejection")
	}
}

func TestGuardSubmitClickDoesNotRecordFailedAttempt(t *testing.T) {
	s := &BrowserAgentSession{}
	key := "https://www.zhihu.com/pin|publish"
	if err := s.guardSubmitClick(key); err != nil {
		t.Fatalf("first guardSubmitClick error = %v", err)
	}
	if err := s.guardSubmitClick(key); err != nil {
		t.Fatalf("second guardSubmitClick without remember error = %v", err)
	}
}

func TestGuardSubmitClickIgnoresNonSubmitKeys(t *testing.T) {
	s := &BrowserAgentSession{}
	if err := s.guardSubmitClick(""); err != nil {
		t.Fatalf("guardSubmitClick empty key error = %v", err)
	}
}

func TestSubmitClickMarkerDetectsChineseLabels(t *testing.T) {
	if !containsSubmitClickMarker("\u53d1\u5e03", "") {
		t.Fatal("expected Chinese publish label to be submit marker")
	}
	if !containsSubmitClickMarker("", "button[aria-label='\u786e\u8ba4\u53d1\u5e03']") {
		t.Fatal("expected Chinese confirm publish selector to be submit marker")
	}
}
