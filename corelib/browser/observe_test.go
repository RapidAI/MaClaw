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
		"queryAllDeep",
		"shadowRoot",
		"__maclawMut",
		"child.defaultView.__maclawMut",
		"input_type",
		"queryIframes",
		"ownerDocument",
		"collectFrames",
		"el.checked",
		"inputType !== 'password'",
		"function pageText()",
		"anySelector",
		"anyIframeSrc",
		"captcha_widget",
		"拖动滑块",
	} {
		if !strings.Contains(browserObserveScript, marker) {
			t.Fatalf("observe script missing marker %q", marker)
		}
	}
	if strings.Contains(browserObserveScript, "selector_candidates: [selector]") {
		t.Fatal("observe script still emits a single selector candidate")
	}
	if !strings.Contains(browserObserveScript, "if (isUnique(legacy, doc)) pushRaw(legacy)") {
		t.Fatal("observe script must not push non-unique legacy nth-of-type selectors")
	}
	if strings.Contains(browserObserveScript, "return String(document.body ? (document.body.innerText") {
		t.Fatal("observe pageText must walk shadow roots and same-origin iframes, not only document.body")
	}
}

func TestRemapRefFrameIDsByURL(t *testing.T) {
	refs := []BrowserElementRef{{Ref: "@e1", FrameID: "iframe-0"}}
	remapRefFrameIDs(refs, []BrowserFrameSnapshot{
		{FrameID: "CDP-IFRAME", URL: "https://child.example.com/app", Name: "other"},
	}, []BrowserFrameSnapshot{
		{FrameID: "iframe-0", URL: "https://child.example.com/app"},
	})
	if refs[0].FrameID != "CDP-IFRAME" {
		t.Fatalf("frame_id=%q", refs[0].FrameID)
	}
}

func TestRemapRefFrameIDsByName(t *testing.T) {
	refs := []BrowserElementRef{{Ref: "@e1", FrameID: "checkout"}}
	remapRefFrameIDs(refs, []BrowserFrameSnapshot{
		{FrameID: "CDP-IFRAME", URL: "https://pay.example.com", Name: "checkout"},
	}, nil)
	if refs[0].FrameID != "CDP-IFRAME" {
		t.Fatalf("frame_id=%q", refs[0].FrameID)
	}
}

func TestMergeAXRefsCopiesBackendNodeID(t *testing.T) {
	existing := []BrowserElementRef{{
		Ref:  "@e1",
		Role: "button",
		Name: "Buy",
	}}
	merged := mergeAXRefs(existing, []BrowserElementRef{{
		Role:          "button",
		Name:          "Buy",
		BackendNodeID: 99,
	}})
	if len(merged) != 1 {
		t.Fatalf("len=%d", len(merged))
	}
	if merged[0].BackendNodeID != 99 {
		t.Fatalf("backend=%d", merged[0].BackendNodeID)
	}
}

func TestRemapRefFrameIDsBySiblingIndex(t *testing.T) {
	refs := []BrowserElementRef{
		{Ref: "@e1", FrameID: "iframe-0"},
		{Ref: "@e2", FrameID: "iframe-1"},
		{Ref: "@e3", FrameID: "iframe-1-0"},
	}
	remapRefFrameIDs(refs, []BrowserFrameSnapshot{
		{FrameID: "MAIN", URL: "https://example.com"},
		{FrameID: "ADS", ParentFrameID: "MAIN"},
		{FrameID: "PAY", ParentFrameID: "MAIN"},
		{FrameID: "CARD", ParentFrameID: "PAY"},
	}, []BrowserFrameSnapshot{
		{FrameID: "main", URL: "https://example.com"},
		{FrameID: "iframe-0", ParentFrameID: "main"},
		{FrameID: "iframe-1", ParentFrameID: "main"},
		{FrameID: "iframe-1-0", ParentFrameID: "iframe-1"},
	})
	if refs[0].FrameID != "ADS" || refs[1].FrameID != "PAY" || refs[2].FrameID != "CARD" {
		t.Fatalf("refs=%#v", refs)
	}
}

func TestFilterRefsByQueryNoMatchReturnsEmpty(t *testing.T) {
	refs := []BrowserElementRef{
		{Name: "Submit", Role: "button", Tag: "button"},
		{Name: "Cancel", Role: "button", Tag: "button"},
	}
	if got := filterRefsByQuery(refs, ""); len(got) != 2 {
		t.Fatalf("empty query should keep all refs, got %d", len(got))
	}
	if got := filterRefsByQuery(refs, "submit"); len(got) != 1 || got[0].Name != "Submit" {
		t.Fatalf("matching query = %#v", got)
	}
	if got := filterRefsByQuery(refs, "does-not-exist"); len(got) != 0 {
		t.Fatalf("no match should be empty, got %#v", got)
	}
}

func TestObservableAttachedFrameSkipsPages(t *testing.T) {
	iframe := attachedFrame{Type: "iframe", SessionID: "sid", TargetID: "tid"}
	if !observableAttachedFrame(iframe, "main-tab") {
		t.Fatal("iframe should be observed")
	}
	if observableAttachedFrame(attachedFrame{Type: "page", SessionID: "sid", TargetID: "popup"}, "main-tab") {
		t.Fatal("popup page must not be mixed into observe refs")
	}
	if observableAttachedFrame(attachedFrame{Type: "iframe", SessionID: "sid", TargetID: "main-tab"}, "main-tab") {
		t.Fatal("active tab must not be re-observed as an attached frame")
	}
	if observableAttachedFrame(attachedFrame{Type: "iframe", TargetID: "tid"}, "main-tab") {
		t.Fatal("iframe without session id is not observable")
	}
}

func TestFrameSessionIDDoesNotMatchURLSubstring(t *testing.T) {
	s := &Session{attached: map[string]attachedFrame{
		"tid": {TargetID: "tid", SessionID: "sid", URL: "https://example.com/app/checkout"},
	}}
	if got := s.frameSessionID("app"); got != "" {
		t.Fatalf("substring match leaked session %q", got)
	}
	if got := s.frameSessionID("tid"); got != "sid" {
		t.Fatalf("target id lookup = %q", got)
	}
}
