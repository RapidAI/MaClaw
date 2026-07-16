package computeruse

import (
	"strings"
	"testing"

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

func TestRenderTextObserve_NoBase64(t *testing.T) {
	res := &ObserveResult{
		Mode: ObserveTextPrimary,
		Meta: ScreenMeta{Width: 800, Height: 600, ScaleFactor: 1},
		Windows: []string{"Notepad"},
		Elements: []MarkedElement{
			{Ref: "e0", Type: "interactable", Name: "File", CenterX: 10, CenterY: 10, BBox: [4]int{0, 0, 20, 20}, Confidence: 0.9, Source: "yolo"},
		},
		OCRExcerpt: "File Edit",
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
