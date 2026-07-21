package petpack

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestRegistryBundledScanAndFrameResolve(t *testing.T) {
	user := t.TempDir()
	reg := NewRegistry(user, BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	if !reg.Ready() {
		t.Fatal("registry should be ready")
	}
	allow := reg.Allowlist()
	for _, id := range OfficialPackIDs {
		if !allow[id] {
			t.Fatalf("missing official id %s in allowlist", id)
		}
	}
	// figurative default yields a pack frame for clawmate
	frame, resolved, err := reg.ResolveAndLoad("clawmate", VariantDefault, StateIdle, 88, NewFrameCache())
	if err != nil {
		t.Fatal(err)
	}
	if resolved == nil {
		t.Fatal("resolved nil")
	}
	if frame == nil {
		t.Fatal("expected non-nil native frame for figurative clawmate idle")
	}
	if frame.Bounds().Dx() != 88 || frame.Bounds().Dy() != 88 {
		t.Fatalf("frame size %v", frame.Bounds())
	}
	// classic may still load native if present; procedural path is caller's fallback when they choose classic + ignore frames
	if !reg.HasNativeFrame("clawmate", VariantDefault, StateSpeaking) {
		t.Fatal("expected speaking native frame")
	}
}

func TestSanitizeSkinAllowlistAndNotReady(t *testing.T) {
	// not ready: well-formed third-party kept
	if got := SanitizeSkinID("my-custom-pet", false, nil); got != "my-custom-pet" {
		t.Fatalf("not-ready should keep valid id, got %q", got)
	}
	if got := SanitizeSkinID("!!!bad", false, nil); got != DefaultPackID {
		t.Fatalf("invalid id should fall back, got %q", got)
	}
	// ready: only allowlist
	allow := map[string]bool{"clawmate": true, "third-party-pet": true}
	if got := SanitizeSkinID("third-party-pet", true, allow); got != "third-party-pet" {
		t.Fatalf("installed third-party wiped: %q", got)
	}
	if got := SanitizeSkinID("unknown-skin", true, allow); got != DefaultPackID {
		t.Fatalf("unknown should → clawmate, got %q", got)
	}
}

func TestThirdPartyInstalledSurvivesRescan(t *testing.T) {
	user := t.TempDir()
	// install minimal pack
	packDir := filepath.Join(user, "third-party-pet")
	if err := os.MkdirAll(filepath.Join(packDir, "native"), 0o755); err != nil {
		t.Fatal(err)
	}
	// copy a real png from bundled clawmate if available via write small png from scan
	// write minimal valid 1x1 png
	png1x1 := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xde, 0x00, 0x00, 0x00,
		0x0c, 0x49, 0x44, 0x41, 0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x00, 0x03, 0x00, 0x01, 0x00, 0x05, 0xfe, 0xd4, 0xef, 0x00, 0x00,
		0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
	if err := os.WriteFile(filepath.Join(packDir, "native", "idle.png"), png1x1, 0o644); err != nil {
		t.Fatal(err)
	}
	man := []byte(`
schema_version: 1
id: third-party-pet
name: Third
version: 0.1.0
renderer: native-raster
assets:
  native:
    idle: native/idle.png
motion:
  pitch: 1.0
`)
	if err := os.WriteFile(filepath.Join(packDir, "pet-pack.yaml"), man, 0o644); err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(user, BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	allow := reg.Allowlist()
	if !allow["third-party-pet"] {
		t.Fatal("third-party not in allowlist")
	}
	if got := SanitizeSkinID("third-party-pet", true, allow); got != "third-party-pet" {
		t.Fatalf("got %q", got)
	}
	// second scan still keeps
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	if got := SanitizeSkinID("third-party-pet", true, reg.Allowlist()); got != "third-party-pet" {
		t.Fatalf("after rescan got %q", got)
	}
}

func TestMigratePetVariantK18(t *testing.T) {
	// existing install
	cfg := &corelib.AppConfig{PetSkin: "clawmate", PetVariantMigrated: false, PetVariant: ""}
	if !MigratePetVariant(cfg) {
		t.Fatal("legacy empty migration should report mutated")
	}
	if cfg.PetVariant != VariantClassic {
		t.Fatalf("existing empty → classic, got %q", cfg.PetVariant)
	}
	if !cfg.PetFigurativeUpgradePromptPending {
		t.Fatal("expected upgrade prompt pending")
	}
	if !cfg.PetVariantMigrated {
		t.Fatal("expected migrated")
	}
	// resolve empty always classic
	if ResolveVariantForRuntime("") != VariantClassic {
		t.Fatal("resolve empty")
	}
	// new-install style already migrated default (AppConfigDefaults)
	cfg2 := &corelib.AppConfig{PetVariant: VariantDefault, PetVariantMigrated: true}
	if MigratePetVariant(cfg2) {
		t.Fatal("stable new-install figurative should not force rewrite")
	}
	if cfg2.PetVariant != VariantDefault {
		t.Fatalf("new install should keep default, got %q", cfg2.PetVariant)
	}
}

func TestListStableOrderOfficialFirst(t *testing.T) {
	user := t.TempDir()
	// Third-party pack so list has mixed ids.
	packDir := filepath.Join(user, "zzz-custom")
	if err := os.MkdirAll(filepath.Join(packDir, "native"), 0o755); err != nil {
		t.Fatal(err)
	}
	png1x1 := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xde, 0x00, 0x00, 0x00,
		0x0c, 0x49, 0x44, 0x41, 0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x00, 0x03, 0x00, 0x01, 0x00, 0x05, 0xfe, 0xd4, 0xef, 0x00, 0x00,
		0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
	_ = os.WriteFile(filepath.Join(packDir, "native", "idle.png"), png1x1, 0o644)
	_ = os.WriteFile(filepath.Join(packDir, "pet-pack.yaml"), []byte(`
schema_version: 1
id: zzz-custom
name: Z
version: 0.1.0
renderer: native-raster
assets:
  native:
    idle: native/idle.png
`), 0o644)

	reg := NewRegistry(user, BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	list := reg.List()
	if len(list) < 5 {
		t.Fatalf("expected official+custom, got %d", len(list))
	}
	// First four should be official catalog order when present.
	for i, id := range OfficialPackIDs {
		if list[i].ID != id {
			t.Fatalf("list[%d]=%q, want official %q", i, list[i].ID, id)
		}
	}
	// Custom at end (alpha among non-official).
	if list[len(list)-1].ID != "zzz-custom" {
		t.Fatalf("last id = %q, want zzz-custom", list[len(list)-1].ID)
	}
}

func TestAppConfigDefaultsFigurativeNewInstall(t *testing.T) {
	// AppConfigDefaults must NOT seed figurative (would poison UnmarshalJSON of legacy configs).
	d := corelib.AppConfigDefaults()
	if d.PetVariantMigrated || d.PetVariant == VariantDefault {
		t.Fatalf("AppConfigDefaults must leave pet variant zero for legacy UnmarshalJSON; got variant=%q migrated=%v", d.PetVariant, d.PetVariantMigrated)
	}
	// Brand-new install helper is the figurative path.
	corelib.ApplyNewInstallPetDefaults(&d)
	if d.PetVariant != VariantDefault || !d.PetVariantMigrated {
		t.Fatalf("ApplyNewInstallPetDefaults figurative: variant=%q migrated=%v", d.PetVariant, d.PetVariantMigrated)
	}
	if MigratePetVariant(&d) {
		t.Fatal("figurative new install should not mutate under MigratePetVariant")
	}
	if d.PetVariant != VariantDefault {
		t.Fatalf("after migrate got %q", d.PetVariant)
	}
}
