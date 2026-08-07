package computeruse

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

func findTestObserve() *ObserveResult {
	return &ObserveResult{
		Elements: []MarkedElement{
			{Ref: "e0", Type: "edit", Name: "搜索", BBox: [4]int{10, 10, 100, 24}, CenterX: 60, CenterY: 22, Source: "accessibility", Interactable: true},
			{Ref: "e1", Type: "interactable", Name: "", BBox: [4]int{10, 60, 200, 40}, CenterX: 110, CenterY: 80, Source: "yolo", Interactable: true},
		},
		OCRLines: []taskengine.OCRResult{
			{Text: "张三 产品部", Confidence: 0.9, BBox: [4]int{20, 200, 120, 20}}, // no element covers this
			{Text: "搜索", Confidence: 0.95, BBox: [4]int{14, 12, 60, 18}},      // covered by e0
			{Text: "Zhang San (PM)", Confidence: 0.8, BBox: [4]int{20, 240, 140, 20}},
		},
	}
}

func TestFindMatches_ElementLabelAndOCRSynth(t *testing.T) {
	obs := findTestObserve()

	// Element label hit returns the existing ref.
	m := FindMatches(obs, "搜索", 10)
	if len(m) != 1 || m[0].Ref != "e0" {
		t.Fatalf("label match: %+v", m)
	}

	// OCR-only hit becomes a synthesized clickable element (no ref yet).
	m = FindMatches(obs, "张三", 10)
	if len(m) != 1 {
		t.Fatalf("ocr match count=%d %+v", len(m), m)
	}
	if m[0].Ref != "" || m[0].Source != "ocr" || m[0].CenterX != 80 || m[0].CenterY != 210 {
		t.Fatalf("synth element: %+v", m[0])
	}

	// OCR line covered by an element must not duplicate.
	for _, hit := range m {
		if hit.Name == "搜索" && hit.Source == "ocr" {
			t.Fatalf("covered OCR line duplicated: %+v", hit)
		}
	}

	// Case-insensitive ASCII.
	m = FindMatches(obs, "zhang san", 10)
	if len(m) != 1 || m[0].Source != "ocr" {
		t.Fatalf("ascii match: %+v", m)
	}

	// No match.
	if m := FindMatches(obs, "李四", 10); len(m) != 0 {
		t.Fatalf("want no match, got %+v", m)
	}
	// Empty query / nil observe.
	if m := FindMatches(obs, "  ", 10); m != nil {
		t.Fatalf("empty query: %+v", m)
	}
	if m := FindMatches(nil, "x", 10); m != nil {
		t.Fatalf("nil observe: %+v", m)
	}
}

func TestFindMatches_ElementValueMatch(t *testing.T) {
	obs := &ObserveResult{
		Elements: []MarkedElement{
			{Ref: "e0", Type: "edit", Name: "", Value: "张三丰", BBox: [4]int{10, 10, 100, 24}, CenterX: 60, CenterY: 22, Source: "accessibility", Interactable: true},
		},
	}
	m := FindMatches(obs, "张三", 10)
	if len(m) != 1 || m[0].Ref != "e0" {
		t.Fatalf("value match: %+v", m)
	}
}

func TestFindMatches_Limit(t *testing.T) {
	obs := &ObserveResult{}
	for i := 0; i < 5; i++ {
		obs.OCRLines = append(obs.OCRLines, taskengine.OCRResult{
			Text: "报 表", Confidence: 0.9, BBox: [4]int{0, i * 30, 50, 20},
		})
	}
	if m := FindMatches(obs, "报表", 2); len(m) != 2 {
		t.Fatalf("limit: got %d", len(m))
	}
}

func TestSessionCommitObserveKeepsOCRLinesAndAppendElements(t *testing.T) {
	sess := NewSession(DefaultConfig())
	ocr := []taskengine.OCRResult{{Text: "张三", Confidence: 0.9, BBox: [4]int{20, 200, 60, 20}}}
	res := sess.CommitObserve(ScreenMeta{Width: 800, Height: 600, ScaleFactor: 1}, nil, nil, ocr, "")
	if len(res.OCRLines) != 1 || res.OCRLines[0].Text != "张三" {
		t.Fatalf("OCRLines not retained: %+v", res.OCRLines)
	}

	refs := sess.AppendElements([]MarkedElement{{Type: "text", Name: "张三", CenterX: 50, CenterY: 210, Source: "ocr"}})
	if len(refs) != 1 || refs[0] != "e0" {
		t.Fatalf("refs=%v", refs)
	}
	// The appended element is clickable through the normal ref path.
	x, y, el, err := sess.ResolveClickRef("e0")
	if err != nil || x != 50 || y != 210 || el.Name != "张三" {
		t.Fatalf("resolve appended: %v %d,%d %+v", err, x, y, el)
	}

	// Refs are stale after an action — append must refuse.
	sess.RecordAction("click", "e0", true, "", true)
	if refs := sess.AppendElements([]MarkedElement{{Name: "x"}}); refs != nil {
		t.Fatalf("append on stale refs: %v", refs)
	}
}

func TestSessionAppendElements_MultipleAndStale(t *testing.T) {
	sess := NewSession(DefaultConfig())
	sess.CommitObserve(ScreenMeta{Width: 800, Height: 600, ScaleFactor: 1}, nil,
		[]taskengine.UIElement{{Type: "button", Name: "确定", BBox: [4]int{1, 1, 10, 10}, Source: "accessibility", Interactable: true}}, nil, "")
	refs := sess.AppendElements([]MarkedElement{
		{Type: "text", Name: "a", Source: "ocr"},
		{Type: "text", Name: "b", Source: "ocr"},
	})
	if len(refs) != 2 || refs[0] != "e1" || refs[1] != "e2" {
		t.Fatalf("refs=%v want [e1 e2] (continue after committed elements)", refs)
	}
	if _, _, _, err := sess.ResolveClickRef("e2"); err != nil {
		t.Fatalf("resolve e2: %v", err)
	}
	// Empty append is a no-op.
	if got := sess.AppendElements(nil); got != nil {
		t.Fatalf("empty append: %v", got)
	}
}

func TestPlaybookMentionsFindAndWindowHint(t *testing.T) {
	p := Playbook()
	for _, want := range []string{"computer_find", "window parameter", "search box", "computer_scroll"} {
		if !strings.Contains(p, want) {
			t.Fatalf("playbook missing %q", want)
		}
	}
}
