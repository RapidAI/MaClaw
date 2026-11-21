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

func TestRefreshGeometryFromImage(t *testing.T) {
	meta := ScreenMeta{
		Width: 200, Height: 100, ScaleFactor: 2,
		OriginX: 100, OriginY: 50,
		CropTitle: "蓝信", ScreenIndex: 1,
		VisionWidth: 80, VisionHeight: 40,
	}
	same := RefreshGeometryFromImage(meta, 200, 100)
	if same.VisionWidth != 80 || same.ScaleFactor != 2 || same.OriginX != 100 {
		t.Fatalf("matching size changed meta: %+v", same)
	}
	out := RefreshGeometryFromImage(meta, 100, 50)
	if out.Width != 100 || out.Height != 50 || out.OriginX != 100 || out.OriginY != 50 {
		t.Fatalf("geometry %+v", out)
	}
	if out.ScaleFactor != 1 || out.VisionWidth != 0 || out.CropTitle != "蓝信" || out.ScreenIndex != 1 {
		t.Fatalf("rebuilt %+v", out)
	}
	sx, sy := MapCaptureToScreen(out, 50, 25)
	if sx != 150 || sy != 75 {
		t.Fatalf("click maps to %d,%d", sx, sy)
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
