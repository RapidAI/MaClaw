package main

import (
	"strings"
	"testing"
)

// resetComputerUseOCRState clears the cached OCR sidecar/provider on the
// global computer-use runtime so warmup tests start from a clean slate.
func resetComputerUseOCRState(t *testing.T) {
	t.Helper()
	globalComputerUse.mu.Lock()
	prevSidecar, prevOCR := globalComputerUse.ocrSidecar, globalComputerUse.ocr
	globalComputerUse.ocrSidecar, globalComputerUse.ocr = nil, nil
	globalComputerUse.mu.Unlock()
	t.Cleanup(func() {
		globalComputerUse.mu.Lock()
		globalComputerUse.ocrSidecar, globalComputerUse.ocr = prevSidecar, prevOCR
		globalComputerUse.mu.Unlock()
	})
}

func TestWarmComputerUseOCRModelsAbsent(t *testing.T) {
	withOCRTestHome(t)
	withOCRTestModelsDir(t) // empty: models not installed
	isolateSharedOCRProvider(t)
	resetComputerUseOCRState(t)

	out := warmComputerUseOCR()
	if out["installed"] != false || out["skipped"] != true || out["warm_ok"] != false || out["ready"] != false {
		t.Fatalf("warmup with absent models = %#v", out)
	}
	if msg, _ := out["error"].(string); !strings.Contains(msg, "not installed") {
		t.Fatalf("warmup error = %q, want a not-installed note", msg)
	}
}

func TestWarmComputerUseOCRModelsPresent(t *testing.T) {
	withOCRTestHome(t)
	installRealOCRModels(t)
	isolateSharedOCRProvider(t)
	resetComputerUseOCRState(t)

	out := warmComputerUseOCR()
	if out["installed"] != true || out["warm_ok"] != true || out["ready"] != true {
		t.Fatalf("warmup with present models = %#v (error=%v)", out, out["error"])
	}

	// Second call hits the already-running fast path.
	out = warmComputerUseOCR()
	if out["warm_ok"] != true || out["note"] != "already running" {
		t.Fatalf("second warmup = %#v", out)
	}
}

func TestWarmComputerUseOCRResultShape(t *testing.T) {
	withOCRTestHome(t)
	withOCRTestModelsDir(t)
	isolateSharedOCRProvider(t)
	resetComputerUseOCRState(t)

	out := warmComputerUseOCR()
	for _, key := range []string{"installed", "ready", "warm_ok", "warm_ms", "error", "skipped"} {
		if _, ok := out[key]; !ok {
			t.Fatalf("warmup result missing key %q: %#v", key, out)
		}
	}
}
