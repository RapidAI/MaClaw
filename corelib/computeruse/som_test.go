package computeruse

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

func TestBuildMarks_AssociatesOCRAndDropsSyntheticNames(t *testing.T) {
	els := []taskengine.UIElement{
		{Type: "interactable", Name: "element_0", BBox: [4]int{10, 10, 40, 20}, Confidence: 0.9, Source: "yolo", Interactable: true},
		{Type: "interactable", Name: "element_1", BBox: [4]int{100, 100, 50, 20}, Confidence: 0.8, Source: "yolo", Interactable: true},
	}
	ocr := []taskengine.OCRResult{
		{Text: "文件", Confidence: 0.95, BBox: [4]int{12, 12, 30, 16}},
		{Text: "编辑", Confidence: 0.9, BBox: [4]int{105, 102, 30, 16}},
	}
	marks := BuildMarks(els, ocr)
	if len(marks) != 2 {
		t.Fatalf("len=%d", len(marks))
	}
	if marks[0].Ref != "e0" || marks[1].Ref != "e1" {
		t.Fatalf("refs=%q %q", marks[0].Ref, marks[1].Ref)
	}
	if marks[0].Name != "文件" {
		t.Fatalf("e0 name=%q want 文件", marks[0].Name)
	}
	if marks[1].Name != "编辑" {
		t.Fatalf("e1 name=%q want 编辑", marks[1].Name)
	}
	if marks[0].CenterX != 30 || marks[0].CenterY != 20 {
		t.Fatalf("e0 center=%d,%d", marks[0].CenterX, marks[0].CenterY)
	}
}

func TestAssociateOCRLabelPrefersIoUOverNearestCenter(t *testing.T) {
	bbox := [4]int{0, 0, 80, 40}
	ocr := []taskengine.OCRResult{
		{Text: "near", BBox: [4]int{36, 16, 8, 8}, Confidence: 0.9},
		{Text: "label", BBox: [4]int{5, 5, 70, 30}, Confidence: 0.8},
	}
	got := associateOCRLabel(bbox, ocr)
	if got != "label" {
		t.Fatalf("got %q want label", got)
	}
}

func TestAssociateOCRLabelSkipsZeroSizeBoxes(t *testing.T) {
	bbox := [4]int{0, 0, 80, 40}
	ocr := []taskengine.OCRResult{
		{Text: "ghost", BBox: [4]int{10, 10, 0, 0}, Confidence: 0.99},
		{Text: "ok", BBox: [4]int{8, 8, 20, 12}, Confidence: 0.5},
	}
	got := associateOCRLabel(bbox, ocr)
	if got != "ok" {
		t.Fatalf("got %q want ok", got)
	}
}

func TestRenderTextObserve_NoBase64(t *testing.T) {
	res := &ObserveResult{
		Mode:    ObserveTextPrimary,
		Meta:    ScreenMeta{Width: 800, Height: 600, ScaleFactor: 1},
		Windows: []string{"Notepad"},
		Elements: []MarkedElement{
			{Ref: "e0", Type: "interactable", Name: "File", CenterX: 10, CenterY: 10, BBox: [4]int{0, 0, 20, 20}, Confidence: 0.9, Source: "yolo"},
		},
		OCRExcerpt:    "File Edit",
		ScreenshotB64: strings.Repeat("A", 5000),
	}
	text := RenderTextObserve(res, 80)
	if strings.Contains(text, res.ScreenshotB64) {
		t.Fatal("screenshot base64 must not appear in text observe")
	}
	if !strings.Contains(text, "e0") || !strings.Contains(text, "File") {
		t.Fatalf("missing element text: %s", text)
	}
	if !strings.Contains(text, "text-only") {
		t.Fatalf("missing text-only hint: %s", text)
	}
	if !strings.Contains(text, "screen_index=0") {
		t.Fatalf("missing primary-monitor hint: %s", text)
	}
}

func TestRenderVisionObserve_NoElementsOrBase64(t *testing.T) {
	res := &ObserveResult{
		Mode:          ObserveVisionAssist,
		Meta:          ScreenMeta{Width: 1920, Height: 1080, ScaleFactor: 1, VisionWidth: 1568, VisionHeight: 882},
		Windows:       []string{"Chrome"},
		ScreenshotB64: "iVBORw0KGgo",
	}
	text := RenderVisionObserve(res)
	if strings.Contains(text, res.ScreenshotB64) {
		t.Fatal("screenshot bytes must not appear in vision observe text")
	}
	if !strings.Contains(text, "perception=llm_vision") || !strings.Contains(text, "image=1568x882") {
		t.Fatalf("missing vision hints: %s", text)
	}
	if strings.Contains(text, "elements (") {
		t.Fatal("vision observe must not use the text-primary elements dump")
	}
}

func TestResolveRef(t *testing.T) {
	els := []MarkedElement{{Ref: "e2", CenterX: 5, CenterY: 6}}
	m, err := ResolveRef(els, "e2")
	if err != nil || m.CenterX != 5 {
		t.Fatalf("e2: %v %#v", err, m)
	}
	m, err = ResolveRef(els, "2")
	if err != nil || m.CenterY != 6 {
		t.Fatalf("numeric: %v %#v", err, m)
	}
	if _, err := ResolveRef(els, "e9"); err == nil {
		t.Fatal("expected stale_ref")
	}
}

func TestBuildMarks_PreservesHandleAndPatterns(t *testing.T) {
	els := []taskengine.UIElement{
		{Type: "Button", Name: "OK", BBox: [4]int{0, 0, 10, 10}, Source: "accessibility", Handle: "OkBtn", Patterns: []string{"invoke"}, Interactable: true, Confidence: 1},
	}
	marks := BuildMarks(els, nil)
	if len(marks) != 1 || marks[0].Handle != "OkBtn" || len(marks[0].Patterns) != 1 {
		t.Fatalf("marks=%+v", marks)
	}
}

func TestBuildMarks_DedupesYoloCoveredByA11y(t *testing.T) {
	els := []taskengine.UIElement{
		// YOLO box fully covered by a same-scale a11y element → dropped.
		{Type: "interactable", Name: "element_0", BBox: [4]int{12, 12, 36, 20}, Confidence: 0.9, Source: "yolo", Interactable: true},
		{Type: "button", Name: "确定", BBox: [4]int{10, 10, 40, 24}, Confidence: 1, Source: "accessibility", Interactable: true},
		// YOLO box inside a large named container (area >> 4x) → kept.
		{Type: "interactable", Name: "element_1", BBox: [4]int{200, 200, 30, 20}, Confidence: 0.8, Source: "yolo", Interactable: true},
		{Type: "pane", Name: "主面板", BBox: [4]int{0, 0, 1000, 800}, Confidence: 1, Source: "accessibility"},
	}
	marks := BuildMarks(els, nil)
	if len(marks) != 3 {
		t.Fatalf("len=%d want 3: %+v", len(marks), marks)
	}
	if marks[0].Ref != "e0" || marks[0].Name != "确定" {
		t.Fatalf("e0=%+v want a11y 确定 first (gapless refs)", marks[0])
	}
	sources := map[string]int{}
	for _, m := range marks {
		sources[m.Source]++
	}
	if sources["yolo"] != 1 || sources["accessibility"] != 2 {
		t.Fatalf("sources=%v", sources)
	}
}

func TestBuildMarks_KeepsLabeledYoloOverNamelessA11y(t *testing.T) {
	els := []taskengine.UIElement{
		{Type: "interactable", Name: "保存按钮", BBox: [4]int{12, 12, 36, 20}, Confidence: 0.9, Source: "yolo", Interactable: true},
		{Type: "listitem", Name: "", BBox: [4]int{10, 10, 40, 24}, Confidence: 1, Source: "accessibility", Interactable: true},
	}
	marks := BuildMarks(els, nil)
	if len(marks) != 2 {
		t.Fatalf("len=%d want 2 (labeled yolo must survive): %+v", len(marks), marks)
	}
	found := false
	for _, m := range marks {
		if m.Source == "yolo" && m.Name == "保存按钮" {
			found = true
		}
	}
	if !found {
		t.Fatalf("labeled yolo mark lost: %+v", marks)
	}
}

func TestRenderTextObserve_LabeledFirst(t *testing.T) {
	var els []MarkedElement
	for i := 0; i < 5; i++ {
		els = append(els, MarkedElement{Ref: "e" + string(rune('0'+i)), Type: "interactable", BBox: [4]int{0, i * 10, 10, 8}, Source: "yolo"})
	}
	els = append(els, MarkedElement{Ref: "e5", Type: "listitem", Name: "张三", BBox: [4]int{0, 100, 100, 20}, Source: "accessibility"})
	res := &ObserveResult{Mode: ObserveTextPrimary, Meta: ScreenMeta{Width: 800, Height: 600, ScaleFactor: 1}, Elements: els}

	// Budget smaller than element count: the labeled element must survive.
	text := RenderTextObserve(res, 2)
	if !strings.Contains(text, `"张三"`) {
		t.Fatalf("labeled element cut by budget:\n%s", text)
	}
	if !strings.Contains(text, "labeled first") {
		t.Fatalf("missing truncation note:\n%s", text)
	}
}

func TestFormatOCRExcerpt_RuneSafeTruncation(t *testing.T) {
	ocr := []taskengine.OCRResult{{Text: strings.Repeat("中", 50), Confidence: 0.9}}
	out := FormatOCRExcerpt(ocr, 10)
	if !strings.HasPrefix(out, strings.Repeat("中", 10)) || !strings.HasSuffix(out, "…") {
		t.Fatalf("out=%q", out)
	}
	if !utf8.ValidString(out) {
		t.Fatal("truncated output is not valid UTF-8")
	}
}

func TestFormatOCRExcerpt_CJKGetsFullRuneBudget(t *testing.T) {
	// 1500 CJK runes ≈ 4500 bytes: a byte-based budget of 2000 would cut this
	// to ~667 runes; a rune budget must keep it whole.
	ocr := []taskengine.OCRResult{{Text: strings.Repeat("文", 1500), Confidence: 0.9}}
	out := FormatOCRExcerpt(ocr, 2000)
	if strings.HasSuffix(out, "…") || utf8.RuneCountInString(out) != 1500 {
		t.Fatalf("CJK text truncated early: runes=%d", utf8.RuneCountInString(out))
	}
	// Two lines joined by a space count the separator against the budget.
	ocr = []taskengine.OCRResult{
		{Text: strings.Repeat("a", 6), Confidence: 0.9},
		{Text: strings.Repeat("b", 6), Confidence: 0.9},
	}
	out = FormatOCRExcerpt(ocr, 10)
	if out != "aaaaaa bbb…" {
		t.Fatalf("out=%q", out)
	}
}
