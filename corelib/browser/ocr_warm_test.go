package browser

import (
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestRapidOCRSidecarWarmNotInstalled(t *testing.T) {
	dir := t.TempDir()
	// Point Maclaw base at temp so ocr dir is empty.
	prev := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(dir)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(prev) })

	sc := NewRapidOCRSidecar(nil)
	if sc.Installed() {
		t.Fatal("expected not installed")
	}
	if sc.Ready() {
		t.Fatal("expected not ready")
	}
	err := sc.Warm()
	if err == nil {
		t.Fatal("expected warm error when not installed")
	}
	if sc.Ready() {
		t.Fatal("should stay not ready")
	}
	// Sanity: ocr dir under temp
	_ = filepath.Join(dir, "ocr")
}

func TestRapidOCRSidecarWarmNil(t *testing.T) {
	var sc *RapidOCRSidecar
	if err := sc.Warm(); err == nil {
		t.Fatal("expected error")
	}
	if sc.Installed() || sc.Ready() {
		t.Fatal("nil should report false")
	}
}
