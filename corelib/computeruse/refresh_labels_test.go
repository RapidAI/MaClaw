package computeruse

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

func TestRefreshCaptureDropsStalePayLabels(t *testing.T) {
	s := NewSession(DefaultConfig())
	const crop = "收件箱 (1) - Google Chrome"
	s.SetWindowResolver(func(int, int) string { return crop })
	s.SetWindowIDResolver(func(int, int) uint64 { return 7 })
	s.CommitObserve(ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, CropTitle: crop}, nil, []taskengine.UIElement{
		{Name: "搜索", BBox: [4]int{0, 0, 40, 20}, Source: "yolo"},
	}, []taskengine.OCRResult{
		{Text: "立即支付", BBox: [4]int{10, 10, 40, 20}},
	}, "old")
	if !s.LabelsCurrent() || s.ElementNameAtScreen(10, 10) != "搜索" || s.OCRTextAtScreen(20, 20) != "立即支付" {
		t.Fatalf("before refresh name=%q ocr=%q current=%v", s.ElementNameAtScreen(10, 10), s.OCRTextAtScreen(20, 20), s.LabelsCurrent())
	}
	before := len(s.Audit())
	s.ApplyRefreshCapture(ScreenMeta{Width: 100, Height: 50, ScaleFactor: 1, CropTitle: crop}, "crop-png")
	if s.LabelsCurrent() || s.RefsValid() {
		t.Fatal("refresh kept labels or click refs")
	}
	if s.ElementNameAtScreen(10, 10) != "" || s.OCRTextAtScreen(20, 20) != "" {
		t.Fatal("stale boxes were hit-tested on the new image")
	}
	if labels := s.KeySurfaceLabels(crop); labels != nil {
		t.Fatalf("stale key labels %v", labels)
	}
	png, ok := s.StaleCropScreenshot()
	if !ok || png != "crop-png" {
		t.Fatalf("screenshot %q ok %v", png, ok)
	}
	if got, stale := s.ObserveDocumentStale("订单 - Google Chrome", 7); !stale || got != crop {
		t.Fatalf("navigation after refresh stale=%v crop=%q", stale, got)
	}
	if _, stale := s.ObserveDocumentStale("收件箱 (2) - Google Chrome", 7); stale {
		t.Fatal("an unread count became a new document")
	}
	if _, stale := s.ObserveDocumentStale("记事本", 8); stale {
		t.Fatal("another window was marked stale")
	}
	if s.AdoptOCRLabels(nil) {
		t.Fatal("empty OCR adopted labels")
	}
	if !s.AdoptOCRLabels([]taskengine.OCRResult{{Text: "立即支付", BBox: [4]int{10, 10, 40, 20}}}) {
		t.Fatal("adopt failed")
	}
	if !s.LabelsCurrent() || s.RefsValid() {
		t.Fatal("adopt published click refs or left labels stale")
	}
	if s.OCRTextAtScreen(20, 20) != "立即支付" {
		t.Fatalf("ocr after adopt %q", s.OCRTextAtScreen(20, 20))
	}
	if _, ok := s.StaleCropScreenshot(); ok {
		t.Fatal("current labels still offered the crop for another OCR")
	}
	if len(s.Audit()) != before {
		t.Fatalf("adopt recorded an action: %d -> %d", before, len(s.Audit()))
	}
	s.ApplyRefreshCapture(ScreenMeta{Width: 100, Height: 50, ScaleFactor: 1}, "monitor")
	if _, ok := s.StaleCropScreenshot(); ok || s.AdoptOCRLabels([]taskengine.OCRResult{{Text: "立即支付", BBox: [4]int{0, 0, 10, 10}}}) {
		t.Fatal("a monitor fallback became a label source")
	}
}

func TestAdoptOCRLabelsRejectsZeroBox(t *testing.T) {
	s := NewSession(DefaultConfig())
	s.ApplyRefreshCapture(ScreenMeta{Width: 80, Height: 40, ScaleFactor: 1, CropTitle: "蓝信"}, "crop")
	if s.AdoptOCRLabels([]taskengine.OCRResult{{Text: "立即支付", BBox: [4]int{0, 0, 0, 0}}}) {
		t.Fatal("a line with no box unlocked labels")
	}
	if s.LabelsCurrent() || s.RefsValid() {
		t.Fatal("zero-box OCR left labels or refs current")
	}
	if s.AdoptOCRLabels([]taskengine.OCRResult{{Text: "  ", BBox: [4]int{1, 1, 10, 10}}}) {
		t.Fatal("blank text unlocked labels")
	}
}

func TestOCROnlyObserveKeepsRememberedWindow(t *testing.T) {
	s := NewSession(DefaultConfig())
	const crop = "收件箱 (1) - Google Chrome"
	s.SetWindowResolver(func(int, int) string { return crop })
	s.SetWindowIDResolver(func(int, int) uint64 { return 7 })
	s.CommitObserve(ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, CropTitle: crop}, nil, []taskengine.UIElement{
		{Name: "搜索", BBox: [4]int{0, 0, 40, 20}},
	}, nil, "")
	s.SetWindowResolver(nil)
	s.SetWindowIDResolver(nil)
	s.CommitObserve(ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, CropTitle: crop}, nil, []taskengine.UIElement{
		{Name: "龙军", BBox: [4]int{10, 10, 40, 20}, Source: "ocr"},
	}, []taskengine.OCRResult{{Text: "龙军", BBox: [4]int{10, 10, 40, 20}}}, "ocr")
	if got, stale := s.ObserveDocumentStale("订单 - Google Chrome", 7); !stale || got != crop {
		t.Fatalf("ocr-only observe cleared the window: stale=%v crop=%q", stale, got)
	}

	s.SetWindowResolver(func(x, _ int) string {
		if x < 50 {
			return "记事本"
		}
		return "计算器"
	})
	s.SetWindowIDResolver(func(x, _ int) uint64 {
		if x < 50 {
			return 1
		}
		return 2
	})
	s.CommitObserve(ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, CropTitle: crop}, nil, []taskengine.UIElement{
		{Name: "甲", BBox: [4]int{0, 0, 20, 20}},
		{Name: "乙", BBox: [4]int{80, 0, 20, 20}},
	}, nil, "")
	if _, stale := s.ObserveDocumentStale("订单 - Google Chrome", 7); stale {
		t.Fatal("a mixed shot kept the previous window id")
	}
}
