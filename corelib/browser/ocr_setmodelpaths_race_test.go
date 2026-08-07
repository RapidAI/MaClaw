package browser

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/ocr"
)

// TestNativeOCRProviderSetModelPathsRace hammers SetModelPaths concurrently
// with Recognize/Warm, alternating between two VALID model path sets so every
// flip shuts the loaded manager down mid-Recognize. The modelmanager
// deferred-unload machinery (unloadPending + active count) must let in-flight
// Recognize calls finish on the old engine: with valid models on both sides,
// ANY Recognize error is a spurious, user-visible failure.
//
// Flip count defaults to 4 (each flip forces a full model reload); extend via
//
//	OCR_STRESS_FLIPS=20 go test -race -run TestNativeOCRProviderSetModelPathsRace ./corelib/browser/
//
// Skipped when the PP-OCRv6 models are not present.
func TestNativeOCRProviderSetModelPathsRace(t *testing.T) {
	dir := filepath.Join("..", "..", ".tmp", "ocr-models")
	detA := filepath.Join(dir, "ppocrv6_small_det.onnx")
	recA := filepath.Join(dir, "ppocrv6_small_rec.onnx")
	if _, ok := ocr.ModelFileStatus(detA); !ok {
		t.Skip("det model not present:", detA)
	}
	if _, ok := ocr.ModelFileStatus(recA); !ok {
		t.Skip("rec model not present:", recA)
	}
	// A second, distinct path set pointing at the same model content, so
	// SetModelPaths sees a real change and shuts the manager down.
	linkDir := t.TempDir()
	detB := filepath.Join(linkDir, "det_b.onnx")
	recB := filepath.Join(linkDir, "rec_b.onnx")
	for _, pair := range [][2]string{{detA, detB}, {recA, recB}} {
		if err := os.Link(pair[0], pair[1]); err != nil {
			data, rerr := os.ReadFile(pair[0])
			if rerr != nil {
				t.Fatal(rerr)
			}
			if werr := os.WriteFile(pair[1], data, 0o644); werr != nil {
				t.Fatal(werr)
			}
		}
	}

	flips := 4
	if v, err := strconv.Atoi(os.Getenv("OCR_STRESS_FLIPS")); err == nil && v > 0 {
		flips = v
	}
	imgB64 := base64.StdEncoding.EncodeToString(tinyPNGForTest(t))

	p := NewNativeOCRProvider(detA, recA, nil)
	var recognizeErrs, warmErrs int64
	var wg sync.WaitGroup
	stop := make(chan struct{})

	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := p.Recognize(imgB64); err != nil {
					atomic.AddInt64(&recognizeErrs, 1)
					t.Errorf("spurious Recognize error (valid models on both path sets): %v", err)
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if err := p.Warm(); err != nil {
				atomic.AddInt64(&warmErrs, 1)
				t.Errorf("spurious Warm error (valid models on both path sets): %v", err)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	for i := 0; i < flips; i++ {
		time.Sleep(30 * time.Millisecond)
		if i%2 == 0 {
			p.SetModelPaths(detB, recB)
		} else {
			p.SetModelPaths(detA, recA)
		}
	}
	close(stop)
	wg.Wait()
	p.Close()

	t.Logf("done: %d flips, %d Recognize errors, %d Warm errors", flips,
		atomic.LoadInt64(&recognizeErrs), atomic.LoadInt64(&warmErrs))
}

// TestNativeOCRProviderSetModelPathsRaceMissingModels runs the same hammer
// with MISSING model files so it executes on every machine (no model
// downloads). Engines never load here, so this exercises the manager-swap
// bookkeeping only; every Recognize must fail with an ordinary load error,
// never with a closed-engine/use-after-free style error or a race.
func TestNativeOCRProviderSetModelPathsRaceMissingModels(t *testing.T) {
	dir := t.TempDir()
	detA := filepath.Join(dir, "missing_det_a.onnx")
	recA := filepath.Join(dir, "missing_rec_a.onnx")
	detB := filepath.Join(dir, "missing_det_b.onnx")
	recB := filepath.Join(dir, "missing_rec_b.onnx")
	imgB64 := base64.StdEncoding.EncodeToString(tinyPNGForTest(t))

	p := NewNativeOCRProvider(detA, recA, nil)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	var unexpected int64

	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_, err := p.Recognize(imgB64)
				if err == nil {
					atomic.AddInt64(&unexpected, 1)
					t.Error("Recognize succeeded with missing model files")
					continue
				}
				// Load failures are expected; anything mentioning a closed
				// engine means a mid-Recognize shutdown leaked through.
				if strings.Contains(err.Error(), "closed") {
					atomic.AddInt64(&unexpected, 1)
					t.Errorf("Recognize hit a closed engine: %v", err)
				}
			}
		}()
	}

	for i := 0; i < 200; i++ {
		if i%2 == 0 {
			p.SetModelPaths(detB, recB)
		} else {
			p.SetModelPaths(detA, recA)
		}
	}
	close(stop)
	wg.Wait()
	p.Close()
	if n := atomic.LoadInt64(&unexpected); n > 0 {
		t.Fatalf("%d unexpected Recognize outcomes", n)
	}
}
