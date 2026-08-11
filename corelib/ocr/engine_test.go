package ocr

import (
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testModelDir = "../../.tmp/ocr-models"

func testEngine(t testing.TB) *Engine {
	t.Helper()
	det := filepath.Join(testModelDir, "ppocrv6_small_det.onnx")
	rec := filepath.Join(testModelDir, "ppocrv6_small_rec.onnx")
	if _, err := os.Stat(det); err != nil {
		t.Skip("det model not available:", det)
	}
	if _, err := os.Stat(rec); err != nil {
		t.Skip("rec model not available:", rec)
	}
	e, err := NewEngine(det, rec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	return e
}

func loadTestImage(t testing.TB, name string) image.Image {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Skip("test image not available:", name)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// assertLines checks that every expected line appears as one recognized
// result (exact match on clean rendered text).
func assertLines(t *testing.T, results []Result, expected []string) {
	t.Helper()
	var got []string
	for _, r := range results {
		got = append(got, r.Text)
	}
	for _, want := range expected {
		found := false
		for _, g := range got {
			if g == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected line %q not found in results %q", want, got)
		}
	}
}

func TestEngineEnglish(t *testing.T) {
	e := testEngine(t)
	results, err := e.Recognize(loadTestImage(t, "en.png"))
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, results, []string{"Hello World 123", "The quick brown fox jumps"})
}

func TestEngineChinese(t *testing.T) {
	e := testEngine(t)
	results, err := e.Recognize(loadTestImage(t, "zh.png"))
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, results, []string{"人工智能改变世界", "光学字符识别引擎测试"})
}

func TestEngineMixed(t *testing.T) {
	e := testEngine(t)
	results, err := e.Recognize(loadTestImage(t, "mixed.png"))
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, results, []string{
		"Order ID: A20240807",
		"Total 1,234.56 CNY",
		"Version v2.5.1 build 88",
	})
}

func TestManagerLifecycle(t *testing.T) {
	det := filepath.Join(testModelDir, "ppocrv6_small_det.onnx")
	rec := filepath.Join(testModelDir, "ppocrv6_small_rec.onnx")
	if _, err := os.Stat(det); err != nil {
		t.Skip("models not available")
	}
	m := NewManager(det, rec)
	defer m.Shutdown()
	if m.Loaded() {
		t.Fatal("must not be loaded before first use")
	}
	if _, err := m.Recognize(loadTestImage(t, "en.png")); err != nil {
		t.Fatal(err)
	}
	if !m.Loaded() {
		t.Fatal("must be loaded after Recognize")
	}
	m.Unload()
	if m.Loaded() {
		t.Fatal("must be unloaded after Unload")
	}
}

// TestEngineRecognizeAfterClose ensures Recognize on a closed (or never
// loaded) engine returns an error instead of dereferencing nil graphs —
// this guards the manager unload path racing an in-flight Recognize.
func TestEngineRecognizeAfterClose(t *testing.T) {
	e := &Engine{}
	if _, err := e.Recognize(image.NewRGBA(image.Rect(0, 0, 8, 8))); err == nil {
		t.Fatal("expected error for closed engine")
	}
	e.Close() // must not panic on an already-closed engine
	if _, err := e.Recognize(image.NewRGBA(image.Rect(0, 0, 8, 8))); err == nil {
		t.Fatal("expected error after Close")
	}
}

// TestEngineResultJSONShape guards the Result field contract consumed by
// callers migrating off the RapidOCR sidecar.
func TestEngineResultFields(t *testing.T) {
	e := testEngine(t)
	results, err := e.Recognize(loadTestImage(t, "en.png"))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("no results")
	}
	for _, r := range results {
		if strings.TrimSpace(r.Text) == "" {
			t.Errorf("empty text in %+v", r)
		}
		if r.Confidence <= 0 || r.Confidence > 1 {
			t.Errorf("confidence %v out of range", r.Confidence)
		}
		if r.BBox[2] <= 0 || r.BBox[3] <= 0 {
			t.Errorf("degenerate bbox %v", r.BBox)
		}
	}
}
