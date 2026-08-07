package browser

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
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

// TestNativeOCRProviderStressLifecycle hammers Recognize + Warm + Close from
// many goroutines to shake lifecycle races between the provider, ocr.Manager
// and modelmanager (unload vs in-flight Acquire, manager swap vs in-flight
// Recognize). Run explicitly with the race detector:
//
//	OCR_STRESS_SECONDS=30 go test -race -run TestNativeOCRProviderStressLifecycle ./corelib/browser/
//
// Skipped unless OCR_STRESS_SECONDS > 0 and the PP-OCRv6 models are present.
func TestNativeOCRProviderStressLifecycle(t *testing.T) {
	secs, _ := strconv.Atoi(os.Getenv("OCR_STRESS_SECONDS"))
	if secs <= 0 {
		t.Skip("set OCR_STRESS_SECONDS>0 to run the stress test")
	}
	dir := filepath.Join("..", "..", ".tmp", "ocr-models")
	det := filepath.Join(dir, "ppocrv6_small_det.onnx")
	rec := filepath.Join(dir, "ppocrv6_small_rec.onnx")
	if _, ok := ocr.ModelFileStatus(det); !ok {
		t.Skip("det model not present:", det)
	}
	if _, ok := ocr.ModelFileStatus(rec); !ok {
		t.Skip("rec model not present:", rec)
	}
	imgB64 := base64.StdEncoding.EncodeToString(tinyPNGForTest(t))

	p := NewNativeOCRProvider(det, rec, nil)
	var recognizeErrs, closedErrs int64
	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Recognize workers.
	for g := 0; g < 6; g++ {
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
					if strings.Contains(err.Error(), "closed") {
						atomic.AddInt64(&closedErrs, 1)
					}
				}
			}
		}()
	}
	// Warm workers.
	for g := 0; g < 2; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = p.Warm()
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}
	// Close workers (force manager shutdown + reload under load).
	for g := 0; g < 2; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				p.Close()
				time.Sleep(20 * time.Millisecond)
			}
		}()
	}

	time.Sleep(time.Duration(secs) * time.Second)
	close(stop)
	wg.Wait()
	p.Close()

	if n := atomic.LoadInt64(&closedErrs); n > 0 {
		t.Fatalf("%d Recognize calls hit a closed engine (unload raced an in-flight Acquire)", n)
	}
	t.Logf("stress done: %d Recognize errors total (reload contention is allowed), 0 panics/races expected",
		atomic.LoadInt64(&recognizeErrs))
}

// tinyPNGForTest renders a small PNG with a few dark "text-like" bars so the
// det/rec pipeline runs end to end without being slow.
func tinyPNGForTest(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 96, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 96; x++ {
			img.Pix[(y*96+x)*4+0] = 255
			img.Pix[(y*96+x)*4+1] = 255
			img.Pix[(y*96+x)*4+2] = 255
			img.Pix[(y*96+x)*4+3] = 255
		}
	}
	for i := 0; i < 4; i++ {
		for y := 12; y < 36; y++ {
			for x := 8 + i*22; x < 8+i*22+14; x++ {
				img.Pix[(y*96+x)*4+0] = 0
				img.Pix[(y*96+x)*4+1] = 0
				img.Pix[(y*96+x)*4+2] = 0
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
