package browser

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"
	"testing"
)

func TestPrepareOCRImageBase64_NoResizeWhenSmall(t *testing.T) {
	b64 := solidPNGBase64(t, 800, 600, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	out, sx, sy, err := prepareOCRImageBase64(b64, 2560)
	if err != nil {
		t.Fatal(err)
	}
	if out != b64 {
		t.Fatal("expected original payload when under max edge")
	}
	if sx != 1 || sy != 1 {
		t.Fatalf("scale=%v,%v want 1,1", sx, sy)
	}
}

func TestPrepareOCRImageBase64_DownscalesLarge(t *testing.T) {
	// 4000x2000 → longest edge 2560 → 2560x1280
	b64 := solidPNGBase64(t, 4000, 2000, color.RGBA{R: 200, G: 100, B: 50, A: 255})
	out, sx, sy, err := prepareOCRImageBase64(b64, 2560)
	if err != nil {
		t.Fatal(err)
	}
	if out == b64 {
		t.Fatal("expected re-encoded smaller image")
	}
	if sx <= 1 || sy <= 1 {
		t.Fatalf("scale should be >1, got %v,%v", sx, sy)
	}
	raw, err := base64.StdEncoding.DecodeString(out)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 2560 || cfg.Height != 1280 {
		t.Fatalf("resized size=%dx%d want 2560x1280", cfg.Width, cfg.Height)
	}
	// Round-trip: OCR box on resized image maps back near original.
	scaled := scaleOCRResults([]OCRResult{{
		Text: "A", Confidence: 0.9, BBox: [4]int{100, 50, 40, 20},
	}}, sx, sy)
	if scaled[0].BBox[0] < 150 || scaled[0].BBox[0] > 160 {
		t.Fatalf("mapped x=%d want ~156", scaled[0].BBox[0])
	}
	if scaled[0].BBox[1] < 75 || scaled[0].BBox[1] > 85 {
		t.Fatalf("mapped y=%d want ~78", scaled[0].BBox[1])
	}
}

func TestPrepareOCRImageBase64_PortraitDownscale(t *testing.T) {
	// 1080x4000 → longest edge 2560 → 691x2560
	b64 := solidPNGBase64(t, 1080, 4000, color.RGBA{R: 1, A: 255})
	out, sx, sy, err := prepareOCRImageBase64(b64, 2560)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(out)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Height != 2560 {
		t.Fatalf("height=%d want 2560", cfg.Height)
	}
	if cfg.Width != 1080*2560/4000 {
		t.Fatalf("width=%d want %d", cfg.Width, 1080*2560/4000)
	}
	if sx <= 1 || sy <= 1 {
		t.Fatalf("scale=%v,%v", sx, sy)
	}
}

func TestPrepareOCRImageBase64_DataURI(t *testing.T) {
	b64 := solidPNGBase64(t, 64, 48, color.RGBA{B: 255, A: 255})
	uri := "data:image/png;base64," + b64
	out, sx, sy, err := prepareOCRImageBase64(uri, 2560)
	if err != nil {
		t.Fatal(err)
	}
	// Fast path must strip data-URI so the Python sidecar can b64decode.
	if strings.HasPrefix(out, "data:") || strings.Contains(out, ",") {
		t.Fatalf("sidecar payload must be pure base64, got prefix %q", out[:min(40, len(out))])
	}
	if out != b64 {
		t.Fatalf("expected stripped pure base64")
	}
	if sx != 1 || sy != 1 {
		t.Fatalf("scale=%v,%v", sx, sy)
	}
}

func TestPureStdBase64_StripsDataURI(t *testing.T) {
	b64 := solidPNGBase64(t, 16, 16, color.RGBA{G: 1, A: 255})
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	got := pureStdBase64("data:image/png;base64,"+b64, raw)
	if got != b64 {
		t.Fatalf("got %q want pure base64", got[:min(32, len(got))])
	}
}

func TestPrepareOCRImageBase64_InvalidBase64(t *testing.T) {
	_, _, _, err := prepareOCRImageBase64("not-base64!!!", 2560)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestScaleOCRResults_Identity(t *testing.T) {
	in := []OCRResult{{Text: "x", BBox: [4]int{1, 2, 3, 4}}}
	out := scaleOCRResults(in, 1, 1)
	if out[0].BBox != in[0].BBox {
		t.Fatalf("%v", out[0].BBox)
	}
}

func TestScaleOCRResults_MinSize(t *testing.T) {
	// Tiny boxes must not collapse to zero width/height after upscale mapping.
	out := scaleOCRResults([]OCRResult{{
		Text: "i", BBox: [4]int{10, 10, 1, 1},
	}}, 2.5, 2.5)
	if out[0].BBox[2] < 1 || out[0].BBox[3] < 1 {
		t.Fatalf("bbox=%v", out[0].BBox)
	}
	if out[0].BBox[0] != 25 || out[0].BBox[1] != 25 {
		t.Fatalf("xy=%v", out[0].BBox)
	}
}

func TestScaleOCRResults_Empty(t *testing.T) {
	if got := scaleOCRResults(nil, 2, 2); got != nil {
		t.Fatalf("%v", got)
	}
}

func TestDecodeImageBytes_StripsDataURI(t *testing.T) {
	b64 := solidPNGBase64(t, 8, 8, color.RGBA{G: 255, A: 255})
	raw, err := decodeImageBytes("data:image/png;base64," + b64)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 8 || string(raw[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Fatalf("not a png header: %v", raw[:min(8, len(raw))])
	}
}

func solidPNGBase64(t *testing.T, w, h int, c color.RGBA) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: c}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}
