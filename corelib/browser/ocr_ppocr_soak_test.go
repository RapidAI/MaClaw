package browser

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/ocr"
)

// TestNativeOCRProviderSoak is the long-run lifecycle soak: continuous
// Recognize from 4 goroutines against one NativeOCRProvider, a concurrent
// SetModelPaths hammer using the SAME paths (the no-op path that must keep
// the warmed manager), and a Warm every 30s — for 10 minutes, intended to be
// run under -race. Asserts zero errors and stable heap/goroutines: a genuine
// leak in the provider/manager/arena-pool lifecycle shows up as monotonic
// HeapInuse drift over 10 minutes of churn.
//
//	OCR_SOAK_MINUTES=10 go test -race -timeout 20m -run TestNativeOCRProviderSoak ./corelib/browser/
//
// (the -timeout must comfortably exceed the soak length; the go test default
// of 10m kills a 10-minute soak at the finish line)
//
// Skipped unless OCR_SOAK_MINUTES > 0 and the PP-OCRv6 models are present.
func TestNativeOCRProviderSoak(t *testing.T) {
	mins, _ := strconv.Atoi(os.Getenv("OCR_SOAK_MINUTES"))
	if mins <= 0 {
		t.Skip("set OCR_SOAK_MINUTES>0 to run the soak test")
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
	// Warm up before the baseline so pool high-water marks (arena pool,
	// scratch pools, dict caches) are counted in the "before" snapshot.
	if err := p.Warm(); err != nil {
		t.Fatalf("initial Warm: %v", err)
	}
	if _, err := p.Recognize(imgB64); err != nil {
		t.Fatalf("initial Recognize: %v", err)
	}

	snap := func() runtime.MemStats {
		runtime.GC()
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return m
	}
	before := snap()
	g0 := runtime.NumGoroutine()

	var recognizeCalls, recognizeErrs, warmErrs int64
	var wg sync.WaitGroup
	stop := make(chan struct{})

	// 4 continuous Recognize workers.
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
					t.Errorf("soak Recognize error: %v", err)
				}
				atomic.AddInt64(&recognizeCalls, 1)
			}
		}()
	}

	// Same-path SetModelPaths hammer: the no-op path must never shut the
	// warmed manager down or disturb in-flight Recognize calls.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			p.SetModelPaths(det, rec)
			time.Sleep(2 * time.Millisecond)
		}
	}()

	// Warm every 30s.
	wg.Add(1)
	go func() {
		defer wg.Done()
		tick := time.NewTicker(30 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
			}
			if err := p.Warm(); err != nil {
				atomic.AddInt64(&warmErrs, 1)
				t.Errorf("soak Warm error: %v", err)
			}
		}
	}()

	// Progress + memory trace once a minute.
	done := time.After(time.Duration(mins) * time.Minute)
	trace := time.NewTicker(time.Minute)
	defer trace.Stop()
loop:
	for {
		select {
		case <-done:
			break loop
		case <-trace.C:
			m := snap()
			t.Logf("soak: calls=%d HeapInuse=%dMB goroutines=%d",
				atomic.LoadInt64(&recognizeCalls), m.HeapInuse>>20, runtime.NumGoroutine())
		}
	}
	close(stop)
	wg.Wait()
	p.Close()

	after := snap()
	calls := atomic.LoadInt64(&recognizeCalls)
	t.Logf("soak done: %d Recognize calls, %d errors, %d Warm errors; HeapInuse %dMB -> %dMB; goroutines %d -> %d",
		calls, atomic.LoadInt64(&recognizeErrs), atomic.LoadInt64(&warmErrs),
		before.HeapInuse>>20, after.HeapInuse>>20, g0, runtime.NumGoroutine())
	if n := atomic.LoadInt64(&recognizeErrs); n > 0 {
		t.Fatalf("%d Recognize errors during soak (valid models, no path flips)", n)
	}
	if n := atomic.LoadInt64(&warmErrs); n > 0 {
		t.Fatalf("%d Warm errors during soak", n)
	}
	if calls == 0 {
		t.Fatal("no Recognize calls completed — workers stalled?")
	}
	const maxDrift = 64 << 20 // 64MB over the whole soak, generous vs working set
	if drift := int64(after.HeapInuse) - int64(before.HeapInuse); drift > maxDrift {
		t.Errorf("HeapInuse drift %d bytes exceeds %d (before=%d after=%d)",
			drift, maxDrift, before.HeapInuse, after.HeapInuse)
	}
	if d := runtime.NumGoroutine() - g0; d > 2 {
		t.Errorf("goroutine count grew by %d (possible timer/goroutine leak)", d)
	}
}
