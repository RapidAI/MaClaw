package guiapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/guiapp/petpack"
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

	cfg := corelib.AppConfig{PetSkin: "unknown-skin", PetSize: 400}
	sanitizePetConfig(&cfg)
	if cfg.PetSkin != petpack.DefaultPackID {
		t.Fatalf("unknown → ragdoll, got %q", cfg.PetSkin)
	}
	if cfg.PetSize != maxPetSize {
		t.Fatalf("size clamp, got %d", cfg.PetSize)
	}

	// A saved crab id is rewritten. Ragdoll is the only official skin.
	cfg2 := corelib.AppConfig{PetSkin: "clawmate", PetVariant: "", PetVariantMigrated: false}
	sanitizePetConfig(&cfg2)
	if cfg2.PetSkin != petpack.DefaultPackID {
		t.Fatalf("retired skin = %q, want ragdoll", cfg2.PetSkin)
	}
	if cfg2.PetVariant != petpack.VariantDefault {
		t.Fatalf("legacy variant = %q, want default", cfg2.PetVariant)
	}
	if cfg2.PetFigurativeUpgradePromptPending {
		t.Fatal("retired upgrade prompt should be cleared")
	}
}

func TestSanitizePetSizeBumpsShippedDefaults(t *testing.T) {
	prev := petpack.EnsureGlobal()
	t.Cleanup(func() { petpack.SetGlobalForTest(prev) })
	petpack.SetGlobalForTest(petpack.NewRegistry(t.TempDir(), nil))

	for _, size := range []int{0, legacyDefaultPetSize, previousDefaultPetSize} {
		cfg := corelib.AppConfig{PetSkin: "ragdoll", PetVariant: "default", PetVariantMigrated: true, PetSize: size}
		sanitizePetConfig(&cfg)
		if cfg.PetSize != defaultPetSize {
			t.Fatalf("size %d became %d, want %d", size, cfg.PetSize, defaultPetSize)
		}
	}
	for _, size := range []int{96, 120, 200, maxPetSize} {
		cfg := corelib.AppConfig{PetSkin: "ragdoll", PetVariant: "default", PetVariantMigrated: true, PetSize: size}
		sanitizePetConfig(&cfg)
		if cfg.PetSize != size {
			t.Fatalf("chosen size %d became %d", size, cfg.PetSize)
		}
	}
	// Window sizing leaves a caller-supplied size alone. Position tests pass 88.
	if got := floatingWindowSize(corelib.AppConfig{PetSize: legacyDefaultPetSize}); got != legacyDefaultPetSize+16 {
		t.Fatalf("unsanitized window = %d, want %d", got, legacyDefaultPetSize+16)
	}
	if got := floatingWindowSize(corelib.AppConfig{PetSize: previousDefaultPetSize}); got != previousDefaultPetSize+16 {
		t.Fatalf("unsanitized previous window = %d, want %d", got, previousDefaultPetSize+16)
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

func TestFloatingAppearanceIncludesReducedMotion(t *testing.T) {
	base := corelib.AppConfig{PetEnabled: true, PetSkin: "clawmate", PetSize: 88}
	next := base
	next.PetReducedMotion = true
	// Reduced motion is a motion-level change (applied in place), not an
	// appearance rebuild.
	if floatingAppearanceChanged(base, next) {
		t.Fatal("reduced-motion should not trigger an appearance rebuild")
	}
	if !floatingMotionChanged(base, next) {
		t.Fatal("reduced-motion should trigger motion change detection")
	}
}

func TestSetPetRuntimeStateOnManager(t *testing.T) {
	m := NewFloatingAssistantManager(nil)
	if m == nil || m.window == nil {
		t.Fatal("manager/window nil")
	}
	m.SetPetRuntimeState("listening", 0)
	if got := m.CurrentPetRuntimeState(); got != "listening" {
		t.Fatalf("state = %q, want listening", got)
	}
	// Companion listening outranks a later thinking or done push.
	m.SetPetRuntimeState("thinking", 0)
	if got := m.CurrentPetRuntimeState(); got != "listening" {
		t.Fatalf("state = %q, want listening while the companion holds", got)
	}
	m.SetPetRuntimeState("done", 0)
	if got := m.CurrentPetRuntimeState(); got != "listening" {
		t.Fatalf("state = %q, want listening while the companion holds", got)
	}
	// Idle releases the companion. With no stored duty the pet wanders.
	m.SetPetRuntimeState("idle", 0)
	if got := m.CurrentPetRuntimeState(); got != "idle" {
		t.Fatalf("after release state = %q, want idle", got)
	}
	m.SetPetRuntimeState("speaking", 0)
	if got := m.CurrentPetRuntimeState(); got != "speaking" {
		t.Fatalf("speaking = %q", got)
	}
	m.SetPetRuntimeState("idle", 0)
	m.SetPetRuntimeState("thinking", 0)
	if got := m.CurrentPetRuntimeState(); got != "thinking" {
		t.Fatalf("thinking = %q", got)
	}
	m.SetPetRuntimeState("done", 0)
	if got := m.CurrentPetRuntimeState(); got != "done" {
		t.Fatalf("done = %q", got)
	}
}

// dutyRestoreWindow records the duty push, including a cue the noop window drops.
type dutyRestoreWindow struct {
	mockFloatingWindow
	ttl          int
	event        string
	cue          string
	applies      int
	lines        []string
	coverBase    string
	coverBaseTTL int
	coverLine    string
}

func (w *dutyRestoreWindow) ApplyPetDuty(state string, ttlMs int, event, cue string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.applies++
	w.runtimeState = state
	w.ttl = ttlMs
	w.event = event
	w.cue = cue
}

func (w *dutyRestoreWindow) SetPetPresenceLine(line string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lines = append(w.lines, line)
}

func (w *dutyRestoreWindow) SetPetRuntimeState(state string, ttlMs int) {
	w.ApplyPetDuty(state, ttlMs, "", "")
}

func (w *dutyRestoreWindow) SetPetTimedCover(state string, ttlMs int, base string, baseTTLMs int, line string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.runtimeState = state
	w.ttl = ttlMs
	w.coverBase = base
	w.coverBaseTTL = baseTTLMs
	w.coverLine = line
}

func (w *dutyRestoreWindow) snapshot() (state string, ttl int, event, cue string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.runtimeState, w.ttl, w.event, w.cue
}

func TestDutyRestoreKeepsOnlyTheLivePose(t *testing.T) {
	m := NewFloatingAssistantManager(nil)
	win := &dutyRestoreWindow{}
	m.window = win

	m.applyDuty(petDutyDecision{
		Pose: string(petpack.StateDone), TTLMS: 1600,
		Event: "task_done", Cue: "celebrate",
	})
	m.mu.Lock()
	m.lastDuty.since = time.Now().Add(-2 * time.Second)
	m.mu.Unlock()
	m.SetPetRuntimeState("listening", 0)
	m.SetPetRuntimeState("idle", 0)
	pose, ttl, event, cue := win.snapshot()
	if pose != string(petpack.StateIdle) || ttl != 0 || event != "" || cue != "" {
		t.Fatalf("expired restore = %s ttl=%d event=%q cue=%q", pose, ttl, event, cue)
	}

	m.applyDuty(petDutyDecision{
		Pose: string(petpack.StateDone), TTLMS: 1600,
		Event: "task_done", Cue: "celebrate",
	})
	m.SetPetRuntimeState("idle", 0)
	pose, ttl, event, cue = win.snapshot()
	if pose != string(petpack.StateDone) || ttl < 1 || ttl > 1600 || event != "" || cue != "" {
		t.Fatalf("live restore = %s ttl=%d event=%q cue=%q", pose, ttl, event, cue)
	}

	m.applyDuty(petDutyDecision{Pose: string(petpack.StateThinking)})
	m.SetPetRuntimeState("speaking", 0)
	m.SetPetRuntimeState("idle", 0)
	pose, ttl, event, cue = win.snapshot()
	if pose != string(petpack.StateThinking) || ttl != 0 || event != "" || cue != "" {
		t.Fatalf("thinking restore = %s ttl=%d event=%q cue=%q", pose, ttl, event, cue)
	}
}

func TestTimedPetStateDoesNotHoldCompanion(t *testing.T) {
	m := NewFloatingAssistantManager(nil)
	win := &dutyRestoreWindow{}
	m.window = win

	m.applyDuty(petDutyDecision{Pose: string(petpack.StateThinking), Line: "正在做"})
	m.SetPetRuntimeState("speaking", 3200)
	pose, ttl, _, _ := win.snapshot()
	if pose != string(petpack.StateSpeaking) || ttl != 3200 {
		t.Fatalf("timed speaking = %s ttl=%d", pose, ttl)
	}
	m.applyDuty(petDutyDecision{Pose: string(petpack.StateDone), TTLMS: 1600, Line: "做好了"})
	pose, ttl, _, _ = win.snapshot()
	if pose != string(petpack.StateDone) || ttl != 1600 {
		t.Fatalf("timed speaking swallowed the task pose=%s ttl=%d", pose, ttl)
	}

	m.SetPetRuntimeState("listening", 0)
	m.SetPetRuntimeState("alert", 1500)
	pose, ttl, _, _ = win.snapshot()
	if pose != string(petpack.StateAlert) || ttl != 1500 {
		t.Fatalf("companion hold dropped the timed alert pose=%s ttl=%d", pose, ttl)
	}
	m.SetPetRuntimeState("thinking", 0)
	pose, ttl, _, _ = win.snapshot()
	if pose != string(petpack.StateAlert) || ttl != 1500 {
		t.Fatalf("timed alert released the companion pose=%s ttl=%d", pose, ttl)
	}
}

func TestTimedThinkingReturnsToDuty(t *testing.T) {
	m := NewFloatingAssistantManager(nil)
	win := &dutyRestoreWindow{}
	m.window = win
	m.applyDuty(petDutyDecision{Pose: string(petpack.StateThinking), Line: "正在做"})
	m.SetPetRuntimeState("listening", 0)
	m.SetPetRuntimeState("thinking", 15000)
	pose, ttl, _, _ := win.snapshot()
	win.mu.Lock()
	base, baseTTL, line := win.coverBase, win.coverBaseTTL, win.coverLine
	win.mu.Unlock()
	if pose != string(petpack.StateThinking) || ttl != 15000 || base != string(petpack.StateThinking) || baseTTL != 0 || line != "正在做" {
		t.Fatalf("transcription cover pose=%s ttl=%d base=%s baseTTL=%d line=%q", pose, ttl, base, baseTTL, line)
	}
	m.applyDuty(petDutyDecision{Pose: string(petpack.StateDone), TTLMS: 1600, Line: "做好了"})
	pose, ttl, _, _ = win.snapshot()
	if pose != string(petpack.StateDone) || ttl != 1600 {
		t.Fatalf("transcription kept the companion hold pose=%s ttl=%d", pose, ttl)
	}

	alone := NewFloatingAssistantManager(nil)
	aloneWin := &dutyRestoreWindow{}
	alone.window = aloneWin
	alone.SetPetRuntimeState("listening", 0)
	alone.SetPetRuntimeState("thinking", 15000)
	aloneWin.mu.Lock()
	base = aloneWin.coverBase
	line = aloneWin.coverLine
	aloneWin.mu.Unlock()
	if base != "" || line != "" {
		t.Fatalf("transcription with no task returns to %q %q", base, line)
	}
}

func TestPresenceLineDoesNotReplayThePose(t *testing.T) {
	m := NewFloatingAssistantManager(nil)
	win := &dutyRestoreWindow{}
	m.window = win
	m.applyDuty(petDutyDecision{
		Pose: string(petpack.StateThinking), Event: "task_started", Cue: "focus", Line: "正在做",
	})
	m.applyDuty(petDutyDecision{Unchanged: true, LineChanged: true, Line: "在改定价页"})
	win.mu.Lock()
	applies := win.applies
	lines := append([]string(nil), win.lines...)
	win.mu.Unlock()
	if applies != 1 || len(lines) != 2 || lines[0] != "正在做" || lines[1] != "在改定价页" {
		t.Fatalf("applies=%d lines=%q", applies, lines)
	}

	m.mu.Lock()
	m.companionPose = true
	m.mu.Unlock()
	m.applyDuty(petDutyDecision{
		Pose: string(petpack.StateListening), Event: "task_needs_you", Cue: "notice", Line: "要你看一下",
	})
	win.mu.Lock()
	applies = win.applies
	last := ""
	if len(win.lines) > 0 {
		last = win.lines[len(win.lines)-1]
	}
	win.mu.Unlock()
	if applies != 1 || last != "要你看一下" {
		t.Fatalf("voice hold applies=%d last=%q", applies, last)
	}
}

func TestAppSetDesktopPetStateBridge(t *testing.T) {
	app := &App{}
	// K11: the bridge never creates the pet window just to apply state.
	app.SetDesktopPetState("listening", 0)
	if fa := app.existingFloatingAssistant(); fa != nil {
		t.Fatal("bridge without window must not create the floating assistant")
	}
	app.floatingAssistant = NewFloatingAssistantManager(nil)
	app.SetDesktopPetState("listening", 0)
	if got := app.floatingAssistant.CurrentPetRuntimeState(); got != "listening" {
		t.Fatalf("bridge state = %q", got)
	}
}

func TestLoadConfigNormalizesRetiredPetQualityStyle(t *testing.T) {
	// Existing config with no pet_variant is normalized to the pack default.
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
	if loaded.PetVariant != petpack.VariantDefault {
		t.Fatalf("loaded variant = %q, want default", loaded.PetVariant)
	}
	if !loaded.PetVariantMigrated {
		t.Fatal("expected migrated flag")
	}
	// Disk must persist the normalized default.
	disk := readPersistedConfigForTest(t, home)
	if disk.PetVariant != petpack.VariantDefault {
		t.Fatalf("disk pet_variant = %q, want default", disk.PetVariant)
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
	url := app.GetPetPackPreviewDataURL(petpack.DefaultPackID)
	if url == "" || !strings.HasPrefix(url, "data:image/") {
		t.Fatalf("expected data URL, got %q", url)
	}
	if !strings.Contains(url, ";base64,") {
		t.Fatalf("expected base64 payload: %q", url[:min(40, len(url))])
	}
	// State frame for figurative preview
	speak := app.GetPetPackStateFrameDataURL(petpack.DefaultPackID, "speaking", "default")
	if speak == "" || !strings.HasPrefix(speak, "data:image/") {
		t.Fatalf("expected speaking frame data URL, got %q", speak)
	}
	// Legacy classic value is normalized to the pack frame.
	if got := app.GetPetPackStateFrameDataURL(petpack.DefaultPackID, "idle", "classic"); got == "" {
		t.Fatal("legacy classic should return the default pack frame")
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
	frame, resolved, err := reg.ResolveAndLoad(petpack.DefaultPackID, "default", petpack.StateIdle, 64, petpack.NewFrameCache())
	if err != nil {
		t.Fatal(err)
	}
	if resolved == nil {
		t.Fatal("resolved nil")
	}
	if frame == nil {
		t.Fatalf("figurative pack frame missing; renderer=%s native=%v dir=%q", resolved.Renderer, resolved.Native, resolved.Manifest.Dir)
	}
	if legacyFrame := tryLoadPackFrame(petpack.DefaultPackID, "classic", "idle", 64, petpack.NewFrameCache()); legacyFrame == nil {
		t.Fatal("legacy classic value should still render the selected pack frame")
	}
	if petpack.ResolveVariantForRuntime("") != "default" {
		t.Fatal("empty runtime variant")
	}
}
