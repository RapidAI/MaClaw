package ocr

import (
	"image"
	"image/color"
	"math"
	"testing"
)

func TestDictVocabSize(t *testing.T) {
	if got := VocabSize(); got != 18710 {
		t.Fatalf("VocabSize=%d want 18710", got)
	}
	d := Dict()
	if d[0] != "" {
		t.Fatalf("id 0 must be blank, got %q", d[0])
	}
	if d[1] != "!" {
		t.Fatalf("id 1 = %q want '!'", d[1])
	}
	if d[len(d)-1] != " " {
		t.Fatalf("last id must be space, got %q", d[len(d)-1])
	}
}

// TestDictTiny guards the per-tier dict selection: the PP-OCRv6 tiny rec
// model has a 6906-class CTC head and its own character dictionary, while
// small/medium share the 18710-class one.
func TestDictTiny(t *testing.T) {
	d := DictTiny()
	if len(d) != 6906 {
		t.Fatalf("len(DictTiny)=%d want 6906", len(d))
	}
	if d[0] != "" || d[1] != "!" || d[len(d)-1] != " " {
		t.Fatalf("tiny dict boundary ids wrong: %q %q %q", d[0], d[1], d[len(d)-1])
	}
	if got, err := DictForVocab(18710); err != nil || len(got) != 18710 {
		t.Fatalf("DictForVocab(18710) = %d, %v", len(got), err)
	}
	if got, err := DictForVocab(6906); err != nil || len(got) != 6906 {
		t.Fatalf("DictForVocab(6906) = %d, %v", len(got), err)
	}
	if _, err := DictForVocab(42); err == nil {
		t.Fatal("DictForVocab(42) must fail for an unknown vocab size")
	}
}

func TestCTCDecode(t *testing.T) {
	dict := []string{"", "a", "b", " "}
	// frames: a a blank b b b a  ->  "aba"
	probs := []float32{
		0.1, 0.8, 0.05, 0.05, // a
		0.1, 0.7, 0.1, 0.1, // a (repeat, collapsed)
		0.9, 0.02, 0.02, 0.06, // blank
		0.1, 0.1, 0.7, 0.1, // b
		0.1, 0.1, 0.6, 0.2, // b (repeat)
		0.1, 0.1, 0.6, 0.2, // b (repeat)
		0.2, 0.5, 0.2, 0.1, // a
	}
	text, conf := ctcGreedyDecode(probs, 7, 4, dict)
	if text != "aba" {
		t.Fatalf("text=%q want %q", text, "aba")
	}
	want := (0.8 + 0.7 + 0.5) / 3
	if math.Abs(conf-want) > 1e-6 {
		t.Fatalf("conf=%v want %v", conf, want)
	}
}

func TestCTCDecode_AllBlank(t *testing.T) {
	dict := []string{"", "a"}
	probs := []float32{0.9, 0.1, 0.8, 0.2}
	text, conf := ctcGreedyDecode(probs, 2, 2, dict)
	if text != "" || conf != 0 {
		t.Fatalf("text=%q conf=%v want empty/0", text, conf)
	}
}

func TestDetPreprocessShape(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 50))
	tn, rh, rw := detPreprocess(img)
	want := []int{1, 3, 64, 96} // round-to-32 of 50x100 (limit not hit)
	if len(tn.Shape) != 4 {
		t.Fatalf("shape=%v", tn.Shape)
	}
	for i := range want {
		if tn.Shape[i] != want[i] {
			t.Fatalf("shape=%v want %v", tn.Shape, want)
		}
	}
	if math.Abs(rh-64.0/50) > 1e-9 || math.Abs(rw-96.0/100) > 1e-9 {
		t.Fatalf("ratios=%v,%v", rh, rw)
	}
}

func TestDetPreprocessLargeImage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2000, 1000))
	tn, _, _ := detPreprocess(img)
	// max side 2000 > 960 -> ratio 0.48 -> 960x480, already multiples of 32.
	if tn.Shape[2] != 480 || tn.Shape[3] != 960 {
		t.Fatalf("shape=%v want [1 3 480 960]", tn.Shape)
	}
}

func TestRecPreprocessShape(t *testing.T) {
	crop := image.NewRGBA(image.Rect(0, 0, 200, 40))
	tn := recPreprocess(crop)
	// width = ceil(48 * 200/40) = 240
	if tn.Shape[2] != 48 || tn.Shape[3] != 240 {
		t.Fatalf("shape=%v want [1 3 48 240]", tn.Shape)
	}
}

func TestRecPreprocessWidthCap(t *testing.T) {
	crop := image.NewRGBA(image.Rect(0, 0, 5000, 40))
	tn := recPreprocess(crop)
	if tn.Shape[3] != recMaxWidth {
		t.Fatalf("width=%d want capped %d", tn.Shape[3], recMaxWidth)
	}
}

func TestCropAxisAlignedIdentity(t *testing.T) {
	// An axis-aligned box must reproduce the source region pixel-exactly.
	src := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for y := 0; y < 30; y++ {
		for x := 0; x < 40; x++ {
			src.Set(x, y, color.RGBA{uint8(x * 6), uint8(y * 8), 77, 255})
		}
	}
	box := [4][2]float32{{10, 10}, {30, 10}, {30, 20}, {10, 20}}
	crop := cropBox(src, box)
	if crop.Bounds().Dx() != 20 || crop.Bounds().Dy() != 10 {
		t.Fatalf("crop size=%v", crop.Bounds())
	}
	for y := 0; y < 10; y++ {
		for x := 0; x < 20; x++ {
			got := crop.RGBAAt(x, y)
			want := color.RGBA{uint8((x + 10) * 6), uint8((y + 10) * 8), 77, 255}
			if got != want {
				t.Fatalf("pixel (%d,%d)=%v want %v", x, y, got, want)
			}
		}
	}
}

func TestModelFileStatus(t *testing.T) {
	if _, ok := ModelFileStatus("does-not-exist.onnx"); ok {
		t.Fatal("missing file must report not-ok")
	}
	if _, ok := ModelFileStatus("../../.tmp/ocr-models/ppocrv6_small_det.onnx"); !ok {
		t.Skip("det model not present locally")
	}
}
