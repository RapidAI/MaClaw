package desktop

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.White)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestShotOptionsNormalize(t *testing.T) {
	o, err := ShotOptions{Format: "JPG"}.Normalize()
	if err != nil || o.Format != "jpeg" || o.Quality != ShotDefaultQuality || o.MIME() != "image/jpeg" {
		t.Fatalf("jpeg defaults: %+v %v", o, err)
	}
	if o, _ := (ShotOptions{Format: "png", Quality: 50}).Normalize(); o.Quality != 0 {
		t.Fatalf("png keeps no quality: %+v", o)
	}
	for _, bad := range []ShotOptions{
		{Format: "gif"}, {Format: "webp", Quality: 101}, {MaxWidth: -1}, {MaxWidth: 8}, {MaxHeight: ShotMaxEdge + 1},
		{Region: ShotRegion{X: -1, Width: 10, Height: 10}}, {Region: ShotRegion{X: 1, Y: 1}},
	} {
		if _, err := bad.Normalize(); err == nil {
			t.Fatalf("expected error for %+v", bad)
		}
	}
	if !(ShotOptions{}).Plain() || (ShotOptions{Marks: true}).Plain() || (ShotOptions{MaxWidth: 1024}).Plain() {
		t.Fatal("Plain is wrong")
	}
}

func TestShotScreenMapping(t *testing.T) {
	s := Shot{ScreenWidth: 1280, ScreenHeight: 800, ImageWidth: 1024, ImageHeight: 640, Region: ShotRegion{0, 0, 1280, 800}, Scale: 0.8}
	if x, y := s.ToScreen(512, 320); x != 640 || y != 400 {
		t.Fatalf("ToScreen = %d,%d", x, y)
	}
	if x, y := s.FromScreen(640, 400); x != 512 || y != 320 {
		t.Fatalf("FromScreen = %d,%d", x, y)
	}
	if x, y := s.ToScreen(5000, -3); x != 1279 || y != 0 {
		t.Fatalf("clamp = %d,%d", x, y)
	}
	zoom := Shot{ScreenWidth: 1280, ScreenHeight: 800, ImageWidth: 400, ImageHeight: 200, Region: ShotRegion{100, 50, 200, 100}, Scale: 2}
	if x, y := zoom.ToScreen(200, 100); x != 200 || y != 100 {
		t.Fatalf("zoom ToScreen = %d,%d", x, y)
	}
}

func TestShotScriptAndDecode(t *testing.T) {
	plain, err := ShotScript("", ShotOptions{})
	legacy, _ := ScreenshotScriptFor("")
	if err != nil || plain != legacy {
		t.Fatal("plain options must use the legacy script")
	}
	script, err := ShotScript("", ShotOptions{MaxWidth: 1024, Format: "webp", Cursor: true, Marks: true, Region: ShotRegion{10, 20, 300, 200}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`-resize "1024x>"`, `"webp:$o"`, "-quality 80", "getmouselocation", "maclaw-a11y-", `-crop "${rw}x${rh}+${rx}+${ry}"`, shotMetaPrefix} {
		if !strings.Contains(script, want) {
			t.Fatalf("script lacks %q", want)
		}
	}
	data := testPNG(t, 1024, 640)
	out := shotMetaPrefix + " 1280 800 0 0 1280 800 1024 640 33 44\n" + base64.StdEncoding.EncodeToString(data)
	shot, err := DecodeShot(out, ShotOptions{MaxWidth: 1024})
	if err != nil || shot.Scale != 0.8 || shot.ScreenWidth != 1280 || !shot.HasCursor || shot.CursorX != 33 {
		t.Fatalf("decode: %+v %v", shot, err)
	}
	if _, err := DecodeShot(out, ShotOptions{Format: "jpeg"}); err == nil {
		t.Fatal("mime mismatch must fail")
	}
	noCursor := shotMetaPrefix + " 1280 800 0 0 1280 800 1024 640 - -\n" + base64.StdEncoding.EncodeToString(data)
	if shot, err := DecodeShot(noCursor, ShotOptions{MaxWidth: 1024}); err != nil || shot.HasCursor {
		t.Fatalf("no cursor: %+v %v", shot, err)
	}
	// Legacy output: a bare base64 PNG at screen size.
	full := testPNG(t, 1280, 800)
	shot, err = DecodeShot(base64.StdEncoding.EncodeToString(full), ShotOptions{})
	if err != nil || shot.Scale != 1 || shot.ScreenWidth != 1280 || shot.Region.Width != 1280 {
		t.Fatalf("legacy: %+v %v", shot, err)
	}
	headers := ShotHeaderValues(Shot{ScreenWidth: 1280, ScreenHeight: 800, ImageWidth: 1024, ImageHeight: 640, Region: ShotRegion{0, 0, 1280, 800}, Scale: 0.8, CursorX: 5, CursorY: 6, HasCursor: true})
	back, err := ParseShotHeaders(func(k string) string { return headers[k] }, data)
	if err != nil || back.Scale != 0.8 || back.CursorY != 6 || back.ImageWidth != 1024 {
		t.Fatalf("headers: %+v %v", back, err)
	}
	old, err := ParseShotHeaders(func(string) string { return "" }, full)
	if err != nil || old.ScreenWidth != 1280 || old.Scale != 1 {
		t.Fatalf("old service: %+v %v", old, err)
	}
}
