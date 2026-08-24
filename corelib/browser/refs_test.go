package browser

import (
	"strings"
	"testing"
)

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

func TestSelectorCandidatesForTextAmbiguousExact(t *testing.T) {
	s := &BrowserAgentSession{
		lastSnapshotID: "snap-1",
		snapshots: map[string]*BrowserSnapshot{
			"snap-1": {
				Refs: []BrowserElementRef{
					{Ref: "@e1", Name: "发布", Selector: "button.a"},
					{Ref: "@e2", Name: "发布", Selector: "button.b"},
				},
			},
		},
	}
	_, _, err := s.selectorCandidatesForText("", "发布")
	if err == nil {
		t.Fatal("expected ambiguous error")
	}
	amb, ok := err.(*AmbiguousElementError)
	if !ok {
		t.Fatalf("err type %T: %v", err, err)
	}
	if len(amb.Refs) != 2 {
		t.Fatalf("refs=%d", len(amb.Refs))
	}
}

func TestRejectDisabledRef(t *testing.T) {
	if err := rejectDisabledRef(nil); err != nil {
		t.Fatal(err)
	}
	if err := rejectDisabledRef(&BrowserElementRef{Ref: "@e1", Disabled: false}); err != nil {
		t.Fatal(err)
	}
	err := rejectDisabledRef(&BrowserElementRef{Ref: "@e3", Name: "提交", Disabled: true})
	if err == nil {
		t.Fatal("expected disabled error")
	}
	if !strings.Contains(err.Error(), "@e3") {
		t.Fatalf("err=%v", err)
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

func TestLocatorCandidatesAllowsRawSelectorWithoutSession(t *testing.T) {
	s := &BrowserAgentSession{}
	cands, ref, err := s.locatorCandidates("", "", "#buy")
	if err != nil {
		t.Fatal(err)
	}
	if ref != nil || len(cands) != 1 || cands[0] != "#buy" {
		t.Fatalf("cands=%v ref=%#v", cands, ref)
	}
}

func TestParseSelectorCount(t *testing.T) {
	if parseSelectorCount(`{"n":3}`) != 3 {
		t.Fatal("expected 3")
	}
	if parseSelectorCount(`not-json`) != -1 {
		t.Fatal("invalid json should be -1")
	}
}

func TestSelectOptionJSMatchesLabel(t *testing.T) {
	js := selectOptionJS("select#country", "China")
	for _, marker := range []string{"applySelect", "option not found", "textContent"} {
		if !strings.Contains(js, marker) {
			t.Fatalf("select JS missing %q", marker)
		}
	}
	if strings.Contains(js, `element not found:`) {
		t.Fatal("select JS leaked selector into error string")
	}
}

func TestWaitSelectorJSWalksFramesAndShadow(t *testing.T) {
	js := waitSelectorJS(`button.buy:nth-of-type(1)`)
	for _, marker := range []string{"findDeep", "findInFrames", "shadowRoot", "iframe"} {
		if !strings.Contains(js, marker) {
			t.Fatalf("wait JS missing %q", marker)
		}
	}
	if !strings.Contains(js, `"button.buy:nth-of-type(1)"`) {
		t.Fatalf("selector not JSON-encoded: %s", js)
	}
	if strings.Contains(js, `element not found`) {
		t.Fatal("wait JS should not embed not-found errors with selectors")
	}
}

func TestGetTextAndCountJSWalkFrames(t *testing.T) {
	if !strings.Contains(getTextJS("#price"), "findInFrames") && !strings.Contains(getTextJS("#price"), "findScoped") {
		t.Fatal("getText JS must walk same-origin iframes")
	}
	if !strings.Contains(selectOptionJS("select#country", "China"), "findScoped") && !strings.Contains(selectOptionJS("select#country", "China"), "findInFrames") {
		t.Fatal("select JS must walk same-origin iframes")
	}
	js := countSelectorJS("#buy")
	if !strings.Contains(js, "countDeepFrames") {
		t.Fatal("count JS must walk same-origin iframes")
	}
	if !strings.Contains(countInDocJS("#buy"), `{n: queryAllDeep(document`) {
		t.Fatal("main-document count must use queryAllDeep, not a frame walk")
	}
	if !strings.Contains(insertTextViaJS("hi"), "HTMLInputElement.prototype") {
		t.Fatal("JS text insert must use the native value setter")
	}
	scoped := countScopedJS("#pay", frameScope{Name: "checkout", Path: []int{1}})
	if !strings.Contains(scoped, "countScoped") || !strings.Contains(scoped, `"checkout"`) || !strings.Contains(scoped, "[1]") {
		t.Fatalf("scoped count JS = %s", scoped)
	}
	if !strings.Contains(pierceFindJS, "function countScoped") {
		t.Fatal("pierce JS missing countScoped")
	}
	if !strings.Contains(pierceFindJS, "function pageTextFrom") {
		t.Fatal("pierce JS missing pageTextFrom")
	}
}

func TestIsFrameGoneErr(t *testing.T) {
	if isFrameGoneErr(nil) || !isFrameGoneErr(errFrameGone()) {
		t.Fatal("frame-gone helper mismatch")
	}
}

func TestExtractObjectID(t *testing.T) {
	if got := extractObjectID([]byte(`{"result":{"objectId":"1.2"}}`)); got != "1.2" {
		t.Fatalf("objectId=%q", got)
	}
	if got := extractObjectID([]byte(`{"result":{"value":null}}`)); got != "" {
		t.Fatalf("empty objectId=%q", got)
	}
}

func TestPierceFindJSScopesToNamedFrame(t *testing.T) {
	for _, marker := range []string{"findScoped", "findScopedLocated", "findFrameChain", "matchFrame", "docAtPath", "queryIframes"} {
		if !strings.Contains(pierceFindJS, marker) {
			t.Fatalf("pierce JS missing %q", marker)
		}
	}
	if !strings.Contains(pierceFindJS, "if (!scoped || !scoped.doc) return null") {
		t.Fatal("scoped lookup must fail closed when the target iframe is missing")
	}
	got := scopedFindCall("#submit", frameScope{Name: "checkout", URL: "https://pay.example.com/card", Path: []int{1, 0}})
	if !strings.Contains(got, `"checkout"`) || !strings.Contains(got, "https://pay.example.com/card") || !strings.Contains(got, "[1,0]") {
		t.Fatalf("scoped find call = %s", got)
	}
	waitJS := waitSelectorJSIn("#buy", frameScope{Name: "pay", URL: "https://pay.example.com"})
	if !strings.Contains(waitJS, "findScoped") || !strings.Contains(waitJS, `"pay"`) {
		t.Fatal("wait JS must pass frame name into findScoped")
	}
}

func TestFrameChildPathNestedIndexes(t *testing.T) {
	frames := []BrowserFrameSnapshot{
		{FrameID: "main", URL: "https://example.com"},
		{FrameID: "ads", ParentFrameID: "main"},
		{FrameID: "pay", ParentFrameID: "main"},
		{FrameID: "card", ParentFrameID: "pay"},
	}
	if got := frameChildPath(frames, "main"); got != nil {
		t.Fatalf("main path = %v, want nil", got)
	}
	if got := frameChildPath(frames, "ads"); len(got) != 1 || got[0] != 0 {
		t.Fatalf("ads path = %v, want [0]", got)
	}
	if got := frameChildPath(frames, "pay"); len(got) != 1 || got[0] != 1 {
		t.Fatalf("pay path = %v, want [1]", got)
	}
	if got := frameChildPath(frames, "card"); len(got) != 2 || got[0] != 1 || got[1] != 0 {
		t.Fatalf("card path = %v, want [1 0]", got)
	}
	if got := frameChildPath(frames, "gone"); got != nil {
		t.Fatalf("missing frame path = %v, want nil", got)
	}
}

func TestExtractPageTextWalksShadowAndIframes(t *testing.T) {
	js := extractPageTextExpr()
	for _, marker := range []string{"pageTextFrom", "queryIframes", "shadowRoot"} {
		if !strings.Contains(js, marker) {
			t.Fatalf("extract page text JS missing %q", marker)
		}
	}
	if strings.Contains(js, "document.body.innerText || document.body.textContent") && !strings.Contains(js, "pageTextFrom") {
		t.Fatal("extract must not use body-only innerText")
	}
}

func TestPageHTMLJSWalksShadowAndIframes(t *testing.T) {
	js := pageHTMLJS()
	for _, marker := range []string{"queryIframes", "shadowRoot", "outerHTML", "contentDocument"} {
		if !strings.Contains(js, marker) {
			t.Fatalf("page HTML JS missing %q", marker)
		}
	}
}
