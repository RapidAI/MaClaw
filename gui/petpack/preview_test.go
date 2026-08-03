package petpack

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPreviewBytesFromUserPack(t *testing.T) {
	user := t.TempDir()
	packDir := filepath.Join(user, "thumb-pet")
	if err := os.MkdirAll(filepath.Join(packDir, "native"), 0o755); err != nil {
		t.Fatal(err)
	}
	png := minimalPNG()
	if err := os.WriteFile(filepath.Join(packDir, "preview.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packDir, "native", "idle.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packDir, "pet-pack.yaml"), []byte(`
schema_version: 1
id: thumb-pet
name: Thumb
version: 1.0.0
renderer: native-raster
preview: preview.png
assets:
  preview: preview.png
  native:
    idle: native/idle.png
`), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := NewRegistry(user, BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	data, ctype, err := reg.LoadPreviewBytes("thumb-pet")
	if err != nil {
		t.Fatal(err)
	}
	if ctype != "image/png" {
		t.Fatalf("ctype = %q", ctype)
	}
	if len(data) < 20 {
		t.Fatalf("data too small: %d", len(data))
	}
	info, ok := reg.Get("thumb-pet")
	if !ok || info == nil {
		t.Fatal("missing pack")
	}
	list := reg.List()
	var found bool
	for _, p := range list {
		if p.ID == "thumb-pet" {
			found = true
			if !p.CanUninstall {
				t.Fatal("user pack should be uninstallable")
			}
			if !p.HasPreview {
				t.Fatal("expected has_preview")
			}
		}
		if p.ID == "clawmate" && p.CanUninstall {
			t.Fatal("bundled-only official should not be uninstallable")
		}
	}
	if !found {
		t.Fatal("thumb-pet not listed")
	}
}

func TestLoadPreviewBytesBundledOfficial(t *testing.T) {
	reg := NewRegistry(t.TempDir(), BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	data, ctype, err := reg.LoadPreviewBytes("clawmate")
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("empty preview")
	}
	if ctype != "image/png" && ctype != "image/webp" {
		t.Fatalf("unexpected ctype %q", ctype)
	}
}

func TestLoadStateFrameBytesFigurative(t *testing.T) {
	reg := NewRegistry(t.TempDir(), BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	// speaking frame for official clawmate (live character/skeleton when possible)
	data, ctype, err := reg.LoadStateFrameBytes("clawmate", "speaking", VariantDefault)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("empty speaking frame")
	}
	// Live rig path always encodes PNG; raster fallback may be webp.
	if ctype != "image/png" && ctype != "image/webp" {
		t.Fatalf("ctype %q", ctype)
	}
	// Live path should prefer PNG encode from rig for clawmate character packs.
	if ctype != "image/png" {
		t.Fatalf("expected live PNG preview for clawmate, got %q", ctype)
	}
	// The retired quality-style selector resolves classic to the pack default,
	// so legacy classic requests also serve raster frames.
	if _, _, err := reg.LoadStateFrameBytes("clawmate", "idle", VariantClassic); err != nil {
		t.Fatalf("legacy classic should resolve to pack default frames: %v", err)
	}
	// missing state falls back to idle (still non-empty for official)
	data2, _, err := reg.LoadStateFrameBytes("clawmate", "listening", VariantDefault)
	if err != nil || len(data2) == 0 {
		t.Fatalf("listening frame: err=%v len=%d", err, len(data2))
	}
	// done / alert official frames
	for _, st := range []string{"done", "alert", "thinking"} {
		d, _, err := reg.LoadStateFrameBytes("clawmate", st, VariantDefault)
		if err != nil || len(d) == 0 {
			t.Fatalf("state %q: err=%v len=%d", st, err, len(d))
		}
	}
}

func TestUninstallUserOnly(t *testing.T) {
	user := t.TempDir()
	reg := NewRegistry(user, BundledFS())
	_ = reg.Scan()
	if err := reg.Uninstall("clawmate"); err == nil {
		t.Fatal("bundled-only official uninstall should fail")
	}

	// install user pack then uninstall
	packDir := filepath.Join(user, "temp-pet")
	_ = os.MkdirAll(filepath.Join(packDir, "native"), 0o755)
	png := minimalPNG()
	_ = os.WriteFile(filepath.Join(packDir, "native", "idle.png"), png, 0o644)
	_ = os.WriteFile(filepath.Join(packDir, "pet-pack.yaml"), []byte(`
schema_version: 1
id: temp-pet
name: Temp
version: 1.0.0
renderer: native-raster
assets:
  native:
    idle: native/idle.png
`), 0o644)
	_ = reg.Scan()
	if err := reg.Uninstall("temp-pet"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(packDir); !os.IsNotExist(err) {
		t.Fatal("user pack dir should be gone")
	}
}
