package ocr

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"
	"testing"
)

// Migrated from corelib/browser/ocr_prepare_test.go (renamed to the exported
// helpers in imageutil.go).

func TestPrepareImageBase64_NoResizeWhenSmall(t *testing.T) {
	b64 := solidPNGBase64(t, 800, 600, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	out, sx, sy, err := PrepareImageBase64(b64, 2560)
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

func TestPrepareImageBase64_DownscalesLarge(t *testing.T) {
	// 4000x2000 → longest edge 2560 → 2560x1280
	b64 := solidPNGBase64(t, 4000, 2000, color.RGBA{R: 200, G: 100, B: 50, A: 255})
	out, sx, sy, err := PrepareImageBase64(b64, 2560)
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
	scaled := ScaleResults([]Result{{
		Text: "A", Confidence: 0.9, BBox: [4]int{100, 50, 40, 20},
	}}, sx, sy)
	if scaled[0].BBox[0] < 150 || scaled[0].BBox[0] > 160 {
		t.Fatalf("mapped x=%d want ~156", scaled[0].BBox[0])
	}
	if scaled[0].BBox[1] < 75 || scaled[0].BBox[1] > 85 {
		t.Fatalf("mapped y=%d want ~78", scaled[0].BBox[1])
	}
}

func TestPrepareImageBase64_PortraitDownscale(t *testing.T) {
	// 1080x4000 → longest edge 2560 → 691x2560
	b64 := solidPNGBase64(t, 1080, 4000, color.RGBA{R: 1, A: 255})
	out, sx, sy, err := PrepareImageBase64(b64, 2560)
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

func TestPrepareImageBase64_DataURI(t *testing.T) {
	b64 := solidPNGBase64(t, 64, 48, color.RGBA{B: 255, A: 255})
	uri := "data:image/png;base64," + b64
	out, sx, sy, err := PrepareImageBase64(uri, 2560)
	if err != nil {
		t.Fatal(err)
	}
	// Fast path must strip data-URI so downstream consumers can b64decode.
	if strings.HasPrefix(out, "data:") || strings.Contains(out, ",") {
		t.Fatalf("payload must be pure base64, got prefix %q", out[:min(40, len(out))])
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

func TestPrepareImageBase64_InvalidBase64(t *testing.T) {
	_, _, _, err := PrepareImageBase64("not-base64!!!", 2560)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestScaleResults_Identity(t *testing.T) {
	in := []Result{{Text: "x", BBox: [4]int{1, 2, 3, 4}}}
	out := ScaleResults(in, 1, 1)
	if out[0].BBox != in[0].BBox {
		t.Fatalf("%v", out[0].BBox)
	}
}

func TestScaleResults_MinSize(t *testing.T) {
	// Tiny boxes must not collapse to zero width/height after upscale mapping.
	out := ScaleResults([]Result{{
		Text: "i", BBox: [4]int{10, 10, 1, 1},
	}}, 2.5, 2.5)
	if out[0].BBox[2] < 1 || out[0].BBox[3] < 1 {
		t.Fatalf("bbox=%v", out[0].BBox)
	}
	if out[0].BBox[0] != 25 || out[0].BBox[1] != 25 {
		t.Fatalf("xy=%v", out[0].BBox)
	}
}

func TestScaleResults_Empty(t *testing.T) {
	if got := ScaleResults(nil, 2, 2); got != nil {
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

func TestPrepareImageBase64_RejectsHugeDimensions(t *testing.T) {
	// 8000x8000 = 64 MP, above the 50 MP guard; the payload itself is tiny.
	b64 := base64.StdEncoding.EncodeToString(fakePNGWithDims(8000, 8000))
	if _, _, _, err := PrepareImageBase64(b64, 2560); err == nil ||
		!strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected too-large error, got %v", err)
	}
	// 6000x6000 = 36 MP (8K-class screenshot) must still pass the guard.
	small := base64.StdEncoding.EncodeToString(fakePNGWithDims(6000, 6000))
	if _, _, _, err := PrepareImageBase64(small, 2560); err != nil &&
		strings.Contains(err.Error(), "too large") {
		t.Fatalf("36MP image wrongly rejected: %v", err)
	}
}

// fakePNGWithDims builds a PNG whose IHDR declares w×h but carries no valid
// image data — enough for DecodeConfig, not for a full Decode.
func fakePNGWithDims(w, h int) []byte {
	var ihdr bytes.Buffer
	ihdr.WriteString("IHDR")
	var dims [8]byte
	binary.BigEndian.PutUint32(dims[0:4], uint32(w))
	binary.BigEndian.PutUint32(dims[4:8], uint32(h))
	ihdr.Write(dims[:])
	ihdr.Write([]byte{8, 2, 0, 0, 0}) // 8-bit RGB, deflate, no filter/interlace

	data := ihdr.Bytes()
	var out bytes.Buffer
	out.WriteString("\x89PNG\r\n\x1a\n")
	binary.Write(&out, binary.BigEndian, uint32(len(data)-4)) // length excludes "IHDR"
	out.Write(data)
	binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(data))
	return out.Bytes()
}
