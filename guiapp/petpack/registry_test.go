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

func TestPackInfoPreservesManifestDescription(t *testing.T) {
	info := packInfoFrom(&PetPackManifest{
		ID:              "creator-pet",
		Name:            "Creator pet",
		Description:     "The original package description.",
		DescriptionI18n: map[string]string{"en": "Localized display description."},
	})
	if info.DescriptionText != "The original package description." {
		t.Fatalf("DescriptionText = %q, want manifest description", info.DescriptionText)
	}
	if got := info.Description["en"]; got != "Localized display description." {
		t.Fatalf("Description i18n = %q, want localized display description", got)
	}
}

func TestClawMateBundledV3PerformanceRigIsReady(t *testing.T) {
	reg := NewRegistry(t.TempDir(), BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	manifest, ok := reg.Get(DefaultPackID)
	if !ok || manifest == nil {
		t.Fatal("missing bundled clawmate")
	}
	if manifest.SchemaVersion != 3 || manifest.Renderer != RendererCharacter || !manifest.Capabilities.PetPerformanceV3 {
		t.Fatalf("ClawMate must be a v3 performance pack, got schema=%d renderer=%q caps=%+v", manifest.SchemaVersion, manifest.Renderer, manifest.Capabilities)
	}
	if manifest.Status != StatusOK {
		t.Fatalf("ClawMate v3 pack invalid: %s", manifest.Error)
	}
	resolved, err := reg.Resolve(DefaultPackID, VariantDefault)
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := NewCharacterRenderer(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if frame := renderer.RenderState(StateThinking, 450, 88); frame == nil || frame.Bounds().Dx() != 88 {
		t.Fatal("expected rendered ClawMate performance frame")
	}
	if !renderer.TriggerEvent("task_done", 5000) {
		t.Fatal("expected ClawMate task_done reaction")
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

func TestRetiredBundledSkinIsNotTreatedAsOfficial(t *testing.T) {
	if IsOfficialPackID("mini-claw") {
		t.Fatal("retired bundled skin must not retain official fallback privileges")
	}
	if got := SanitizeSkinID("mini-claw", true, OfficialAllowlist()); got != DefaultPackID {
		t.Fatalf("retired skin = %q, want %q", got, DefaultPackID)
	}
}

func TestRegistryRejectsInvalidStaticIdleFallback(t *testing.T) {
	user := t.TempDir()
	packDir := filepath.Join(user, "broken-idle")
	if err := os.MkdirAll(filepath.Join(packDir, "native"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packDir, "native", "idle.png"), []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := []byte("schema_version: 1\nid: broken-idle\nname: Broken\nrenderer: native-raster\nassets:\n  native:\n    idle: native/idle.png\n")
	if err := os.WriteFile(filepath.Join(packDir, "pet-pack.yaml"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(user, nil)
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	pack, ok := reg.Get("broken-idle")
	if !ok || pack == nil || pack.Status != StatusInvalid {
		t.Fatalf("pack status = %+v, want invalid", pack)
	}
}

func TestRegistryRejectsInvalidNonDefaultSkeletonVariant(t *testing.T) {
	user := t.TempDir()
	packDir := filepath.Join(user, "mixed-rig")
	if err := os.MkdirAll(filepath.Join(packDir, "native"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packDir, "native", "idle.png"), minimalPNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := []byte("schema_version: 2\nid: mixed-rig\nrenderer: native-raster\nassets:\n  native:\n    idle: native/idle.png\nvariants:\n  - id: experimental\n    renderer: native-skeleton\n    assets:\n      rig:\n        definition: rig/missing.json\n        textures: [rig/missing.png]\n")
	if err := os.WriteFile(filepath.Join(packDir, "pet-pack.yaml"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(user, nil)
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	pack, ok := reg.Get("mixed-rig")
	if !ok || pack == nil || pack.Status != StatusInvalid {
		t.Fatalf("pack status = %+v, want invalid", pack)
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

func TestMigratePetVariantNormalizesRetiredQualityStyle(t *testing.T) {
	// Legacy installs are normalized to the selected pack's default presentation.
	cfg := &corelib.AppConfig{PetSkin: "clawmate", PetVariantMigrated: false, PetVariant: ""}
	if !MigratePetVariant(cfg) {
		t.Fatal("legacy empty migration should report mutated")
	}
	if cfg.PetVariant != VariantDefault {
		t.Fatalf("legacy variant = %q, want default", cfg.PetVariant)
	}
	if cfg.PetFigurativeUpgradePromptPending {
		t.Fatal("retired upgrade prompt should be cleared")
	}
	if !cfg.PetVariantMigrated {
		t.Fatal("expected migrated")
	}
	if ResolveVariantForRuntime("") != VariantDefault {
		t.Fatal("resolve empty")
	}
	// new-install style already migrated default (AppConfigDefaults)
	cfg2 := &corelib.AppConfig{PetVariant: VariantDefault, PetVariantMigrated: true}
	if MigratePetVariant(cfg2) {
		t.Fatal("stable default should not force rewrite")
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
	if len(list) < 2 {
		t.Fatalf("expected official+custom, got %d", len(list))
	}
	// The single maintained official pack stays first.
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
