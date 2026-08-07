package browser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNativeOCRProviderWarmNotInstalled(t *testing.T) {
	dir := t.TempDir()
	// Point the provider at nonexistent model files.
	p := NewNativeOCRProvider(
		filepath.Join(dir, "ppocrv6_small_det.onnx"),
		filepath.Join(dir, "ppocrv6_small_rec.onnx"),
		nil,
	)
	if p.Installed() {
		t.Fatal("expected not installed")
	}
	if p.IsAvailable() {
		t.Fatal("expected not available")
	}
	if p.Ready() {
		t.Fatal("expected not ready")
	}
	err := p.Warm()
	if err == nil {
		t.Fatal("expected warm error when not installed")
	}
	if p.Ready() {
		t.Fatal("should stay not ready")
	}
	// Recognize must fail (models missing), not panic.
	if _, err := p.Recognize(""); err == nil {
		t.Fatal("expected recognize error when not installed")
	}
}

func TestNativeOCRProviderNil(t *testing.T) {
	var p *NativeOCRProvider
	if err := p.Warm(); err == nil {
		t.Fatal("expected error")
	}
	if _, err := p.Recognize(""); err == nil {
		t.Fatal("expected error")
	}
	if p.Installed() || p.IsAvailable() || p.Ready() {
		t.Fatal("nil should report false")
	}
	// Close on nil must not panic.
	p.Close()
}

func TestNativeOCRProviderRecognizeBadImage(t *testing.T) {
	dir := t.TempDir()
	p := NewNativeOCRProvider(
		filepath.Join(dir, "det.onnx"),
		filepath.Join(dir, "rec.onnx"),
		nil,
	)
	if _, err := p.Recognize("not-base64!!!"); err == nil {
		t.Fatal("expected prepare error for invalid base64")
	}
}

func TestNativeOCRProviderSetModelPaths(t *testing.T) {
	dir := t.TempDir()
	missingDet := filepath.Join(dir, "missing_det.onnx")
	missingRec := filepath.Join(dir, "missing_rec.onnx")

	// Valid-looking ONNX files for the "new tier": field 1 varint header.
	presentDet := filepath.Join(dir, "ppocrv6_medium_det.onnx")
	presentRec := filepath.Join(dir, "ppocrv6_medium_rec.onnx")
	for _, path := range []string{presentDet, presentRec} {
		if err := os.WriteFile(path, []byte{0x08, 0x07, 0x01, 0x02}, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	p := NewNativeOCRProvider(missingDet, missingRec, nil)
	if p.IsAvailable() {
		t.Fatal("expected not available with missing files")
	}

	// Switching to present paths (tier change) must make the provider usable.
	p.SetModelPaths(presentDet, presentRec)
	if !p.IsAvailable() {
		t.Fatal("expected available after SetModelPaths to present files")
	}

	// Same paths again: no-op, still available.
	p.SetModelPaths(presentDet, presentRec)
	if !p.IsAvailable() {
		t.Fatal("expected available after no-op SetModelPaths")
	}

	// Switching back to missing paths must reflect immediately.
	p.SetModelPaths(missingDet, missingRec)
	if p.IsAvailable() {
		t.Fatal("expected not available after SetModelPaths to missing files")
	}

	// Nil receiver must not panic.
	var nilP *NativeOCRProvider
	nilP.SetModelPaths(presentDet, presentRec)
}
