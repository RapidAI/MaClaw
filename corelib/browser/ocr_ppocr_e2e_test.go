package browser

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/ocr"
)

// providerStressModels returns the local PP-OCRv6 small models or skips.
func providerStressModels(t *testing.T) (det, rec string) {
	t.Helper()
	dir := filepath.Join("..", "..", ".tmp", "ocr-models")
	det = filepath.Join(dir, "ppocrv6_small_det.onnx")
	rec = filepath.Join(dir, "ppocrv6_small_rec.onnx")
	if _, ok := ocr.ModelFileStatus(det); !ok {
		t.Skip("det model not present:", det)
	}
	if _, ok := ocr.ModelFileStatus(rec); !ok {
		t.Skip("rec model not present:", rec)
	}
	return det, rec
}

func loadStressPNGBase64(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "ocr", "testdata", "stress", name))
	if err != nil {
		t.Fatalf("stress image %s: %v (regenerate with corelib/ocr/testdata/stress/gen_stress.py)", name, err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// TestNativeOCRProviderProductionSizeScreenshot pushes a 2560x1440 text
// screenshot — the exact production input size after PrepareImageBase64's
// 2560 long-edge cap — through the provider. The det model then sees a
// 960x540 resize; text quality must still be sane.
func TestNativeOCRProviderProductionSizeScreenshot(t *testing.T) {
	det, rec := providerStressModels(t)
	p := NewNativeOCRProvider(det, rec, nil)
	defer p.Close()

	results, err := p.Recognize(loadStressPNGBase64(t, "screenshot_2560x1440.png"))
	if err != nil {
		t.Fatal(err)
	}
	// The screenshot has 38 text lines; allow detection slack.
	if len(results) < 30 || len(results) > 60 {
		t.Fatalf("results=%d outside [30,60]", len(results))
	}
	want := "[00] service-ok status=ok latency=9ms req=8048"
	found := false
	for _, r := range results {
		if r.Text == want {
			found = true
		}
		if r.Confidence < 0 || r.Confidence > 1 {
			t.Errorf("confidence %v out of range for %q", r.Confidence, r.Text)
		}
		// No downscale expected at 2560 long edge, so boxes are in original
		// coordinates and must lie inside the 2560x1440 frame.
		if r.BBox[0] < 0 || r.BBox[1] < 0 || r.BBox[0]+r.BBox[2] > 2560 || r.BBox[1]+r.BBox[3] > 1440 {
			t.Errorf("bbox %v escapes 2560x1440 frame", r.BBox)
		}
	}
	if !found {
		texts := make([]string, 0, len(results))
		for _, r := range results {
			texts = append(texts, r.Text)
		}
		t.Errorf("expected exact line %q not found; first texts: %q", want, texts[:min(4, len(texts))])
	}
	t.Logf("results=%d", len(results))
}

// TestNativeOCRProviderConcurrentDeterminism hammers one shared provider
// from 8 goroutines x 50 calls (run with -race). Identical inputs must
// produce identical texts — the PrepareImageBase64 path, the lazy manager
// and the engine must not leak buffer reuse or map order into results.
func TestNativeOCRProviderConcurrentDeterminism(t *testing.T) {
	det, rec := providerStressModels(t)
	p := NewNativeOCRProvider(det, rec, nil)
	defer p.Close()

	payloads := []string{
		loadStressPNGBase64(t, "single_word.png"),
		loadStressPNGBase64(t, "wide_strip_3000x80.png"),
		loadStressPNGBase64(t, "tiny_16x16.png"),
	}
	sign := func(res []OCRResult, err error) string {
		if err != nil {
			return "ERR: " + err.Error()
		}
		var b strings.Builder
		for _, r := range res {
			fmt.Fprintf(&b, "%q|%.6f|%v|", r.Text, r.Confidence, r.BBox)
		}
		return b.String()
	}

	const goroutines = 8
	const calls = 50
	const unset = "\x00unset" // a legit signature can be "" (0 results)
	base := make([]string, len(payloads))
	for i := range base {
		base[i] = unset
	}
	var baseMu sync.Mutex
	errs := make(chan error, goroutines*calls)
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < calls; i++ {
				idx := (g + i) % len(payloads)
				got := sign(mustRecognize(p, payloads[idx]))
				baseMu.Lock()
				if base[idx] == unset {
					base[idx] = got
				} else if base[idx] != got {
					errs <- fmt.Errorf("payload %d: nondeterministic result\ngot:  %.200s\nwant: %.200s",
						idx, got, base[idx])
				}
				baseMu.Unlock()
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func mustRecognize(p *NativeOCRProvider, b64 string) ([]OCRResult, error) {
	res, err := p.Recognize(b64)
	return res, err
}
