package computeruse

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

func TestApplyDisplayGeometryAndMapping(t *testing.T) {
	var meta ScreenMeta
	ApplyDisplayGeometry(&meta, 1920, 0, 1920, 1080, 1920, 1080)
	if meta.OriginX != 1920 || meta.OriginY != 0 || meta.ScaleFactor != 1 {
		t.Fatalf("meta=%+v", meta)
	}
	sx, sy := MapCaptureToScreen(meta, 10, 20)
	if sx != 1930 || sy != 20 {
		t.Fatalf("capture→screen %d,%d", sx, sy)
	}
	cx, cy := MapScreenToCapture(meta, 1930, 20)
	if cx != 10 || cy != 20 {
		t.Fatalf("screen→capture %d,%d", cx, cy)
	}
}

func TestApplyDisplayGeometryRetinaScale(t *testing.T) {
	var meta ScreenMeta
	ApplyDisplayGeometry(&meta, 0, 0, 1440, 900, 2880, 1800)
	if meta.ScaleFactor != 2 {
		t.Fatalf("scale=%v want 2", meta.ScaleFactor)
	}
	sx, sy := MapCaptureToScreen(meta, 200, 100)
	if sx != 100 || sy != 50 {
		t.Fatalf("retina map %d,%d", sx, sy)
	}
	w, h := ScaleSize(meta, 40, 20)
	if w != 80 || h != 40 {
		t.Fatalf("scale size %d,%d", w, h)
	}
}

func TestSession_ResolveClickRefAppliesOrigin(t *testing.T) {
	s := NewSession(DefaultConfig())
	els := []taskengine.UIElement{
		{Name: "OK", BBox: [4]int{0, 0, 10, 10}, Source: "yolo", Interactable: true, Confidence: 0.9},
	}
	s.CommitObserve(ScreenMeta{Width: 100, Height: 100, ScaleFactor: 1, OriginX: 1920, OriginY: 10}, nil, els, nil, "")
	x, y, _, err := s.ResolveClickRef("e0")
	if err != nil {
		t.Fatal(err)
	}
	if x != 1925 || y != 15 {
		t.Fatalf("got %d,%d want 1925,15", x, y)
	}
}
