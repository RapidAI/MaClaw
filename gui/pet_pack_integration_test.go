package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/gui/petpack"
)

func TestSanitizePetConfigUsesRegistryAllowlist(t *testing.T) {
	user := t.TempDir()
	corelib.SetMaclawBaseDir(user)
	// Ensure registry scans bundled
	reg := petpack.NewRegistry(user+"/pet-packs", petpack.BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	petpack.SetGlobalForTest(reg)

	cfg := corelib.AppConfig{PetSkin: "unknown-skin", PetSize: 200}
	sanitizePetConfig(&cfg)
	if cfg.PetSkin != "clawmate" {
		t.Fatalf("unknown → clawmate, got %q", cfg.PetSkin)
	}
	if cfg.PetSize != 120 {
		t.Fatalf("size clamp, got %d", cfg.PetSize)
	}

	// third-party installed
	allow := reg.Allowlist()
	allow["third-party-pet"] = true
	// inject into registry via rescan with dir
	// simpler: call SanitizeSkinID path by putting pack on disk
	// Already tested in petpack; here verify official retained
	cfg2 := corelib.AppConfig{PetSkin: "dev-claw", PetVariant: "", PetVariantMigrated: false}
	sanitizePetConfig(&cfg2)
	if cfg2.PetSkin != "dev-claw" {
		t.Fatalf("official wiped: %q", cfg2.PetSkin)
	}
	if cfg2.PetVariant != petpack.VariantClassic {
		t.Fatalf("K18 empty → classic, got %q", cfg2.PetVariant)
	}
	if !cfg2.PetFigurativeUpgradePromptPending {
		t.Fatal("expected upgrade prompt")
	}
}

func TestSanitizeNotReadyKeepsWellFormedID(t *testing.T) {
	// Force a not-ready registry: empty ready flag via new unscanned registry
	reg := petpack.NewRegistry(t.TempDir(), nil)
	// not scanned → Ready false
	petpack.SetGlobalForTest(reg)
	cfg := corelib.AppConfig{PetSkin: "my-custom-pet", PetVariantMigrated: true, PetVariant: "classic"}
	sanitizePetConfig(&cfg)
	if cfg.PetSkin != "my-custom-pet" {
		t.Fatalf("not-ready must keep well-formed id, got %q", cfg.PetSkin)
	}
}

func TestFloatingAppearanceIncludesVariantAndReducedMotion(t *testing.T) {
	base := corelib.AppConfig{PetEnabled: true, PetSkin: "clawmate", PetSize: 88, PetVariant: "classic"}
	next := base
	next.PetVariant = "default"
	if !floatingAppearanceChanged(base, next) {
		t.Fatal("variant change should trigger appearance refresh")
	}
	next2 := base
	next2.PetReducedMotion = true
	if !floatingAppearanceChanged(base, next2) {
		t.Fatal("reduced-motion should trigger appearance change detection")
	}
}

func TestSetPetRuntimeStateOnManager(t *testing.T) {
	// Use real manager + platform window (windows: layered; others: stub).
	m := NewFloatingAssistantManager(nil)
	if m == nil || m.window == nil {
		t.Fatal("manager/window nil")
	}
	m.SetPetRuntimeState("listening", 0)
	if got := m.CurrentPetRuntimeState(); got != "listening" {
		t.Fatalf("state = %q, want listening", got)
	}
	m.SetPetRuntimeState("thinking", 50)
	if got := m.CurrentPetRuntimeState(); got != "thinking" {
		t.Fatalf("state = %q, want thinking", got)
	}
	// Wait TTL → idle
	time.Sleep(80 * time.Millisecond)
	if got := m.CurrentPetRuntimeState(); got != "idle" {
		t.Fatalf("after ttl state = %q, want idle", got)
	}
	m.SetPetRuntimeState("speaking", 0)
	if got := m.CurrentPetRuntimeState(); got != "speaking" {
		t.Fatalf("speaking = %q", got)
	}
	m.SetPetRuntimeState("done", 0)
	if got := m.CurrentPetRuntimeState(); got != "done" {
		t.Fatalf("done = %q", got)
	}
}

func TestAppSetDesktopPetStateBridge(t *testing.T) {
	app := &App{}
	app.SetDesktopPetState("listening", 0)
	if got := app.GetDesktopPetState(); got != "listening" {
		t.Fatalf("bridge state = %q", got)
	}
}

func TestLoadConfigPersistsK18ClassicMigration(t *testing.T) {
	// Existing on-disk config with missing pet_variant must persist classic after LoadConfig.
	home := t.TempDir()
	app := &App{testHomeDir: home}
	// Write a legacy-style config without pet_variant / migrated flag.
	cfgDir := filepath.Join(home, ".maclaw")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"pet_enabled":true,"pet_skin":"clawmate","pet_size":88}`)
	cfgPath := filepath.Join(cfgDir, "config.json")
	if err := os.WriteFile(cfgPath, legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PetVariant != petpack.VariantClassic {
		t.Fatalf("loaded variant = %q, want classic", loaded.PetVariant)
	}
	if !loaded.PetVariantMigrated {
		t.Fatal("expected migrated flag")
	}
	// Disk must also persist classic (criterion 3).
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var disk corelib.AppConfig
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatal(err)
	}
	if disk.PetVariant != petpack.VariantClassic {
		t.Fatalf("disk pet_variant = %q, want classic", disk.PetVariant)
	}
	if !disk.PetVariantMigrated {
		t.Fatal("disk missing pet_variant_migrated")
	}
}

func TestGetPetPackPreviewDataURLOfficial(t *testing.T) {
	// Ensure registry has bundled assets.
	reg := petpack.NewRegistry(t.TempDir(), petpack.BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	petpack.SetGlobalForTest(reg)
	app := &App{}
	url := app.GetPetPackPreviewDataURL("clawmate")
	if url == "" || !strings.HasPrefix(url, "data:image/") {
		t.Fatalf("expected data URL, got %q", url)
	}
	if !strings.Contains(url, ";base64,") {
		t.Fatalf("expected base64 payload: %q", url[:min(40, len(url))])
	}
	// State frame for figurative preview
	speak := app.GetPetPackStateFrameDataURL("clawmate", "speaking", "default")
	if speak == "" || !strings.HasPrefix(speak, "data:image/") {
		t.Fatalf("expected speaking frame data URL, got %q", speak)
	}
	// classic has no raster state frames
	if got := app.GetPetPackStateFrameDataURL("clawmate", "idle", "classic"); got != "" {
		t.Fatalf("classic should return empty, got len=%d", len(got))
	}
	dir := app.GetPetPacksDir()
	if strings.TrimSpace(dir) == "" {
		t.Fatal("GetPetPacksDir empty")
	}
}

func TestValidatePetPackZipPath(t *testing.T) {
	if _, err := validatePetPackZipPath(""); err == nil {
		t.Fatal("empty path should fail")
	}
	if _, err := validatePetPackZipPath("C:\\nope\\file.txt"); err == nil {
		t.Fatal("non-zip extension should fail")
	}
	dir := t.TempDir()
	if _, err := validatePetPackZipPath(dir); err == nil {
		t.Fatal("directory should fail")
	}
	missing := filepath.Join(dir, "missing.zip")
	if _, err := validatePetPackZipPath(missing); err == nil {
		t.Fatal("missing zip should fail")
	}
	okPath := filepath.Join(dir, "pack.zip")
	if err := os.WriteFile(okPath, []byte("PK"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := validatePetPackZipPath(okPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(okPath) {
		t.Fatalf("got %q", got)
	}
}

func TestInstallPetPackZipRejectsBadPath(t *testing.T) {
	app := &App{}
	if _, err := app.InstallPetPackZip(""); err == nil {
		t.Fatal("empty path should error")
	}
	if _, err := app.InstallPetPackZip("not-a-zip.bin"); err == nil {
		t.Fatal("non-zip should error")
	}
}

func TestFirstRunDefaultsFigurative(t *testing.T) {
	d := corelib.AppConfigDefaults()
	corelib.ApplyNewInstallPetDefaults(&d)
	if d.PetVariant != "default" || !d.PetVariantMigrated {
		t.Fatalf("new install figurative: variant=%q migrated=%v", d.PetVariant, d.PetVariantMigrated)
	}
	// First-run save path uses ApplyNewInstallPetDefaults; sanitize must keep figurative.
	sanitizePetConfig(&d)
	if d.PetVariant != "default" {
		t.Fatalf("sanitize must keep figurative new install, got %q", d.PetVariant)
	}
}

func TestTryLoadPackFrameFigurative(t *testing.T) {
	// Do not use EnsureGlobal — other tests may poison the singleton.
	reg := petpack.NewRegistry(t.TempDir(), petpack.BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	petpack.SetGlobalForTest(reg)
	frame, resolved, err := reg.ResolveAndLoad("clawmate", "default", petpack.StateIdle, 64, petpack.NewFrameCache())
	if err != nil {
		t.Fatal(err)
	}
	if resolved == nil {
		t.Fatal("resolved nil")
	}
	if frame == nil {
		t.Fatalf("figurative pack frame missing; renderer=%s native=%v dir=%q", resolved.Renderer, resolved.Native, resolved.Manifest.Dir)
	}
	if petpack.ResolveVariantForRuntime("") != "classic" {
		t.Fatal("empty runtime variant")
	}
}
