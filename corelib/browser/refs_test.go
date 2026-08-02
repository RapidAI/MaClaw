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

func textMatchSession() *BrowserAgentSession {
	return &BrowserAgentSession{
		lastSnapshotID: "snap-1",
		snapshots: map[string]*BrowserSnapshot{
			"snap-1": {
				Refs: []BrowserElementRef{
					{Ref: "@e1", Name: "提交", Selector: "button.submit"},
					{Ref: "@e2", Name: "发布新文章", Selector: "button.publish"},
				},
			},
		},
	}
}

func TestSelectorCandidatesForTextPrefersExactMatch(t *testing.T) {
	s := textMatchSession()
	_, ref, err := s.selectorCandidatesForText("", "发布新文章")
	if err != nil {
		t.Fatalf("selectorCandidatesForText error = %v", err)
	}
	if ref == nil || ref.Ref != "@e2" {
		t.Fatalf("resolved ref = %#v, want @e2", ref)
	}
}

func TestSelectorCandidatesForTextReverseContainsNeedsLongName(t *testing.T) {
	s := textMatchSession()
	// The reverse rank requires RuneCount(name) >= 4. "提交" (2 runes)
	// appears verbatim in the query but must NOT reverse-match.
	s.snapshots["snap-1"].Refs[0].Name = "提交"
	if _, _, err := s.selectorCandidatesForText("", "请点击提交按钮"); err == nil {
		t.Fatal("expected no match: reverse-contains must reject short names")
	}
}

func TestSelectorCandidatesForTextReverseContainsMatchesLongName(t *testing.T) {
	s := textMatchSession()
	_, ref, err := s.selectorCandidatesForText("", "请点击发布新文章按钮")
	if err != nil {
		t.Fatalf("selectorCandidatesForText error = %v", err)
	}
	if ref == nil || ref.Ref != "@e2" {
		t.Fatalf("resolved ref = %#v, want @e2", ref)
	}
}

func TestSelectorCandidatesForTextPrefixBeatsContains(t *testing.T) {
	s := &BrowserAgentSession{
		lastSnapshotID: "snap-1",
		snapshots: map[string]*BrowserSnapshot{
			"snap-1": {
				Refs: []BrowserElementRef{
					{Ref: "@e1", Name: "立即保存", Selector: "button.a"},
					{Ref: "@e2", Name: "保存草稿", Selector: "button.b"},
				},
			},
		},
	}
	_, ref, err := s.selectorCandidatesForText("", "保存")
	if err != nil {
		t.Fatalf("selectorCandidatesForText error = %v", err)
	}
	if ref == nil || ref.Ref != "@e2" {
		t.Fatalf("resolved ref = %#v, want @e2 (prefix beats contains)", ref)
	}
}

func TestSelectorCandidatesForTextContainsBeatsReverse(t *testing.T) {
	s := &BrowserAgentSession{
		lastSnapshotID: "snap-1",
		snapshots: map[string]*BrowserSnapshot{
			"snap-1": {
				Refs: []BrowserElementRef{
					{Ref: "@e1", Name: "保存按钮", Selector: "button.a"},
					{Ref: "@e2", Name: "继续", Text: "操作请点击保存按钮继续完成", Selector: "button.b"},
				},
			},
		},
	}
	// @e1 only matches via reverse-contains (query contains its name);
	// @e2 matches via contains (its text contains the query) and must win.
	_, ref, err := s.selectorCandidatesForText("", "请点击保存按钮继续")
	if err != nil {
		t.Fatalf("selectorCandidatesForText error = %v", err)
	}
	if ref == nil || ref.Ref != "@e2" {
		t.Fatalf("resolved ref = %#v, want @e2 (contains beats reverse)", ref)
	}
}
