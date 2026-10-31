//go:build windows

package remote

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"testing"
)

func TestFinishScreenshotKeepsUsableGDI(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i] = 220
		img.Pix[i+1] = 40
		img.Pix[i+2] = 40
		img.Pix[i+3] = 255
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	b64 := base64.StdEncoding.EncodeToString(buf.Bytes())
	got, err := finishScreenshot(0, 0, 32, 32, b64, nil)
	if err != nil || got != b64 {
		t.Fatalf("usable capture must be kept, err=%v same=%v", err, got == b64)
	}
}

func TestPrintWindowsPastOldCapStayPaintable(t *testing.T) {
	if printWindowEnumMax < 1024 {
		t.Fatalf("enum cap %d stops before occlusion can see the rest of the desktop", printWindowEnumMax)
	}
	hits := make([]printWindowInfo, 300)
	for i := range hits {
		hits[i] = printWindowInfo{x: 0, y: 0, w: 100, h: 100, occludes: true}
	}
	hits = append(hits, printWindowInfo{x: 500, y: 0, w: 80, h: 80, occludes: true})
	kept := windowsForPrint(hits, 0, 0, 800, 200)
	if len(kept) != 2 || kept[1].x != 500 {
		t.Fatalf("window after the old 256 cap must remain, kept %#v", kept)
	}
}

func TestBlankTopWindowDoesNotCoverTheOneBehind(t *testing.T) {
	const side = 64
	blank := make([]byte, side*side*4)
	red := make([]byte, side*side*4)
	blue := make([]byte, side*side*4)
	for i := 0; i < len(red); i += 4 {
		red[i+2] = 255
		blue[i] = 255
	}
	rasters := map[uintptr][]byte{1: blank, 2: red, 3: blue}
	draw := func(win printWindowInfo) ([]byte, bool) {
		src, ok := rasters[win.hwnd]
		return src, ok
	}
	dst := image.NewRGBA(image.Rect(0, 0, side, side))
	windows := []printWindowInfo{
		{hwnd: 1, x: 0, y: 0, w: side, h: side, occludes: true},
		{hwnd: 2, x: 0, y: 0, w: side, h: side, occludes: true},
	}
	if n := paintOrderedWindows(dst, windows, 0, 0, draw); n != 1 || dst.Pix[0] != 255 {
		t.Fatalf("blank top hid the window behind it: n=%d pix=%v", n, dst.Pix[:4])
	}

	dst = image.NewRGBA(image.Rect(0, 0, side, side))
	windows = []printWindowInfo{
		{hwnd: 2, x: 0, y: 0, w: side, h: side, occludes: true},
		{hwnd: 3, x: 0, y: 0, w: side, h: side, occludes: true},
	}
	if n := paintOrderedWindows(dst, windows, 0, 0, draw); n != 1 || dst.Pix[0] != 255 || dst.Pix[2] != 0 {
		t.Fatalf("top window must keep its pixels: n=%d pix=%v", n, dst.Pix[:4])
	}
}

func TestFilterVisiblePrintWindowsKeepsUncovered(t *testing.T) {
	hits := []printWindowInfo{
		{x: 0, y: 0, w: 200, h: 200, occludes: true},
		{x: 10, y: 10, w: 40, h: 40, occludes: true},
		{x: 400, y: 0, w: 100, h: 100, occludes: true},
		{x: -80, y: 0, w: 40, h: 40, occludes: true},
	}
	kept := filterVisiblePrintWindows(hits, 0, 0, 640, 480)
	if len(kept) != 2 || kept[0].w != 200 || kept[1].x != 400 {
		t.Fatalf("kept %#v", kept)
	}

	layered := []printWindowInfo{
		{x: 0, y: 0, w: 200, h: 200, occludes: false},
		{x: 10, y: 10, w: 40, h: 40, occludes: true},
	}
	kept = filterVisiblePrintWindows(layered, 0, 0, 200, 200)
	if len(kept) != 2 {
		t.Fatalf("layered overlay must not hide the window under it: %#v", kept)
	}
}

func TestBgraHasInkAndBlitClip(t *testing.T) {
	src := make([]byte, 8*8*4)
	if bgraHasInk(src, 8, 8) {
		t.Fatal("black buffer must not count as ink")
	}
	src[2] = 255
	// One bright pixel is below the sampled average threshold on a black field.
	if bgraHasInk(src, 8, 8) {
		t.Fatal("one pixel must not lift the average over the blank threshold")
	}
	for i := 0; i < len(src); i += 4 {
		src[i] = 10
		src[i+1] = 20
		src[i+2] = 200
	}
	if !bgraHasInk(src, 8, 8) {
		t.Fatal("bright buffer must count as ink")
	}
	dst := image.NewRGBA(image.Rect(0, 0, 4, 4))
	blitBGRA(dst, src, 8, 8, -2, -2)
	// Source pixel (2,2) lands on dest (0,0): BGRA 10,20,200 → RGBA 200,20,10.
	if dst.Pix[0] != 200 || dst.Pix[1] != 20 || dst.Pix[2] != 10 || dst.Pix[3] != 255 {
		t.Fatalf("clipped blit pix=%v", dst.Pix[:4])
	}
}
