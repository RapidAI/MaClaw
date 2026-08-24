package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/accessibility"
	"github.com/RapidAI/CodeClaw/corelib/computeruse"
)

func TestCropPNGBase64(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(2, 3, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	src := base64.StdEncoding.EncodeToString(buf.Bytes())
	out, err := cropPNGBase64(src, 2, 3, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(out)
	if err != nil {
		t.Fatal(err)
	}
	got, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	b := got.Bounds()
	if b.Dx() != 2 || b.Dy() != 2 {
		t.Fatalf("size=%dx%d", b.Dx(), b.Dy())
	}
	r, _, _, a := got.At(b.Min.X, b.Min.Y).RGBA()
	if a == 0 || r>>8 < 200 {
		t.Fatalf("cropped pixel not red: r=%d a=%d", r, a)
	}
}

func TestExpandWindowCrop(t *testing.T) {
	b := expandWindowCrop(accessibility.WindowBounds{X: 10, Y: 20, Width: 100, Height: 80, Title: "App"}, 8)
	if b.X != 2 || b.Y != 12 || b.Width != 116 || b.Height != 96 {
		t.Fatalf("got %+v", b)
	}
	if b.Title != "App" {
		t.Fatalf("title=%q", b.Title)
	}
}

func TestCropCaptureToWindowMapsOrigin(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 200, 100))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	pngB64 := base64.StdEncoding.EncodeToString(buf.Bytes())
	meta := computeruse.ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, OriginX: 1920, OriginY: 0}
	cap, ok := cropCaptureToWindow(pngB64, meta, accessibility.WindowBounds{X: 1960, Y: 10, Width: 80, Height: 70, Title: "Win"})
	if !ok {
		t.Fatal("crop failed")
	}
	if cap.Meta.CropTitle != "Win" || cap.Meta.OriginX != 1960 || cap.Meta.OriginY != 10 {
		t.Fatalf("meta=%+v", cap.Meta)
	}
	if cap.Width != 80 || cap.Height != 70 {
		t.Fatalf("size %dx%d", cap.Width, cap.Height)
	}
}
