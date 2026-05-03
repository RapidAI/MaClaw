package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	coreconfig "github.com/RapidAI/CodeClaw/corelib/config"
)

func TestLoadConfigConcurrentFirstRun(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}

	const workers = 12
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if _, err := app.LoadConfig(); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("LoadConfig() error = %v", err)
		}
	}

	configPath := filepath.Join(tmpHome, ".maclaw", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Read config.json error = %v", err)
	}
	var cfg corelib.AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("Unmarshal config.json error = %v", err)
	}
	if cfg.ActiveTool != "claude" {
		t.Fatalf("ActiveTool = %q, want %q", cfg.ActiveTool, "claude")
	}
	if !cfg.CheckUpdateOnStartup {
		t.Fatal("CheckUpdateOnStartup = false, want true for first-run default config")
	}

	matches, err := filepath.Glob(configPath + ".tmp*")
	if err != nil {
		t.Fatalf("Glob temp files error = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temp files remain: %v", matches)
	}
}

func TestUpdateToastTranslationUsesReadableText(t *testing.T) {
	app := &App{}

	app.SetLanguage("en")
	if got, want := app.tr("New version %s is available. Open About to download the update.", "V9.9.9"), "New version V9.9.9 is available. Open About to download the update."; got != want {
		t.Fatalf("en update toast = %q, want %q", got, want)
	}

	app.SetLanguage("zh-Hans")
	if got, want := app.tr("New version %s is available. Open About to download the update.", "V9.9.9"), "发现新版本 V9.9.9，可前往关于页面下载更新。"; got != want {
		t.Fatalf("zh-Hans update toast = %q, want %q", got, want)
	}

	app.SetLanguage("zh-Hant")
	if got, want := app.tr("New version %s is available. Open About to download the update.", "V9.9.9"), "發現新版本 V9.9.9，可前往關於頁面下載更新。"; got != want {
		t.Fatalf("zh-Hant update toast = %q, want %q", got, want)
	}
}

func TestLoadConfigConcurrentLegacyMigration(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	legacyConfig := []byte(`{"active_tool":"claude","current_project":"legacy-project"}`)
	legacyPath := filepath.Join(tmpHome, ".aicoder_config.json")
	if err := os.WriteFile(legacyPath, legacyConfig, 0644); err != nil {
		t.Fatalf("Write legacy config error = %v", err)
	}

	app := &App{testHomeDir: tmpHome}

	const workers = 10
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if _, err := app.LoadConfig(); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("LoadConfig() error = %v", err)
		}
	}

	configPath := filepath.Join(tmpHome, ".maclaw", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Read migrated config error = %v", err)
	}
	if string(data) != string(legacyConfig) {
		t.Fatalf("migrated config = %q, want %q", string(data), string(legacyConfig))
	}
	var migrated map[string]any
	if err := json.Unmarshal(data, &migrated); err != nil {
		t.Fatalf("Unmarshal migrated config error = %v", err)
	}

	matches, err := filepath.Glob(configPath + ".tmp*")
	if err != nil {
		t.Fatalf("Glob temp files error = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temp files remain: %v", matches)
	}
}

func TestLoadConfigCachesInMemoryUntilSave(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() seed error = %v", err)
	}

	cfg.RemoteEmail = "first@example.com"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	cached, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() cached error = %v", err)
	}
	if cached.RemoteEmail != "first@example.com" {
		t.Fatalf("cached RemoteEmail = %q, want %q", cached.RemoteEmail, "first@example.com")
	}

	configPath := filepath.Join(tmpHome, ".maclaw", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Read config.json error = %v", err)
	}
	var diskCfg corelib.AppConfig
	if err := json.Unmarshal(data, &diskCfg); err != nil {
		t.Fatalf("Unmarshal config.json error = %v", err)
	}
	if diskCfg.RemoteEmail != "first@example.com" {
		t.Fatalf("disk RemoteEmail = %q, want %q", diskCfg.RemoteEmail, "first@example.com")
	}

	diskCfg.RemoteEmail = "external@example.com"
	updatedData, err := json.Marshal(diskCfg)
	if err != nil {
		t.Fatalf("Marshal external config error = %v", err)
	}
	if err := os.WriteFile(configPath, updatedData, 0644); err != nil {
		t.Fatalf("Write external config error = %v", err)
	}

	stale, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() stale error = %v", err)
	}
	if stale.RemoteEmail != "first@example.com" {
		t.Fatalf("stale RemoteEmail = %q, want cached %q before invalidation", stale.RemoteEmail, "first@example.com")
	}

	app.configMu.Lock()
	app.invalidateConfigCacheLocked()
	app.configMu.Unlock()

	fresh, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() fresh error = %v", err)
	}
	if fresh.RemoteEmail != "external@example.com" {
		t.Fatalf("fresh RemoteEmail = %q, want %q", fresh.RemoteEmail, "external@example.com")
	}
}

func TestLoadConfigRefreshesLogDetailGateAfterCacheInvalidation(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() seed error = %v", err)
	}
	cfg.LogDetailEnabled = true
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() enable log detail error = %v", err)
	}
	if !corelib.IsLogDetailEnabled() {
		t.Fatal("expected runtime log detail gate to be enabled after SaveConfig")
	}

	cfg.LogDetailEnabled = false
	configPath := filepath.Join(tmpHome, ".maclaw", "config.json")
	updatedData, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal config error = %v", err)
	}
	if err := os.WriteFile(configPath, updatedData, 0644); err != nil {
		t.Fatalf("Write config.json error = %v", err)
	}

	app.configMu.Lock()
	app.invalidateConfigCacheLocked()
	app.configMu.Unlock()
	corelib.SetLogDetailEnabled(true)

	fresh, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() fresh error = %v", err)
	}
	if fresh.LogDetailEnabled {
		t.Fatal("expected fresh config to load log detail as disabled")
	}
	if corelib.IsLogDetailEnabled() {
		t.Fatal("expected runtime log detail gate to be disabled after non-cached LoadConfig")
	}
}

func TestSaveConfigPersistsLogDetailEnabled(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.LogDetailEnabled = true
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	reloaded, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() reload error = %v", err)
	}
	if !reloaded.LogDetailEnabled {
		t.Fatal("expected LogDetailEnabled to persist as true")
	}
}

func TestSaveConfigSanitizesPetSettings(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.PetEnabled = true
	cfg.PetSkin = "unknown-skin"
	cfg.PetSize = 999
	cfg.PetInteractionMode = "noisy"
	cfg.PetConversationMode = "free-chat"
	cfg.PetReadbackMode = "everything"
	cfg.PetVoiceReadback = true
	motionSound := false
	cfg.PetMotionSound = &motionSound
	cfg.PetContinuousTimeout = 1

	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	reloaded, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() reload error = %v", err)
	}
	if reloaded.PetSkin != "clawmate" {
		t.Fatalf("PetSkin = %q, want clawmate", reloaded.PetSkin)
	}
	if reloaded.PetSize != 120 {
		t.Fatalf("PetSize = %d, want 120", reloaded.PetSize)
	}
	if reloaded.PetInteractionMode != "balanced" {
		t.Fatalf("PetInteractionMode = %q, want balanced", reloaded.PetInteractionMode)
	}
	if reloaded.PetConversationMode != "text-first" {
		t.Fatalf("PetConversationMode = %q, want text-first", reloaded.PetConversationMode)
	}
	if reloaded.PetReadbackMode != "summary" {
		t.Fatalf("PetReadbackMode = %q, want summary", reloaded.PetReadbackMode)
	}
	if !reloaded.PetVoiceReadback {
		t.Fatal("PetVoiceReadback = false, want true for summary readback")
	}
	if petMotionSoundEnabled(reloaded) {
		t.Fatal("PetMotionSound should remain disabled")
	}
	if reloaded.PetContinuousTimeout != 5 {
		t.Fatalf("PetContinuousTimeout = %d, want 5", reloaded.PetContinuousTimeout)
	}
}

func TestSaveConfigDefaultsPetSizeForClarity(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.PetEnabled = true
	cfg.PetSize = 0

	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	reloaded, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() reload error = %v", err)
	}
	if reloaded.PetSize != defaultPetSize {
		t.Fatalf("PetSize = %d, want %d", reloaded.PetSize, defaultPetSize)
	}
}

func TestFloatingAppearanceChangedIncludesPetRuntimeSettings(t *testing.T) {
	base := corelib.AppConfig{
		ShowAssistantEntry:   true,
		PetEnabled:           true,
		PetSkin:              "clawmate",
		PetSize:              defaultPetSize,
		PetInteractionMode:   "balanced",
		PetContinuousTimeout: 30,
	}

	motionOff := false
	withMotionOff := base
	withMotionOff.PetMotionEnabled = &motionOff
	if !floatingAppearanceChanged(base, withMotionOff) {
		t.Fatal("expected motion toggle to refresh floating window")
	}

	soundOff := false
	withSoundOff := base
	withSoundOff.PetMotionSound = &soundOff
	if !floatingAppearanceChanged(base, withSoundOff) {
		t.Fatal("expected motion sound toggle to refresh floating window")
	}

	withQuiet := base
	withQuiet.PetQuietMode = true
	if !floatingAppearanceChanged(base, withQuiet) {
		t.Fatal("expected quiet mode toggle to refresh floating window")
	}

	withMode := base
	withMode.PetInteractionMode = "active"
	if !floatingAppearanceChanged(base, withMode) {
		t.Fatal("expected interaction mode change to refresh floating window")
	}
}

func TestSaveConfigConcurrentWritesValidJSON(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() seed error = %v", err)
	}

	const workers = 12
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			localCfg := cfg
			localCfg.RemoteEmail = "worker-" + string(rune('a'+i)) + "@example.com"
			if err := app.SaveConfig(localCfg); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("SaveConfig() error = %v", err)
		}
	}

	configPath := filepath.Join(tmpHome, ".maclaw", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Read config.json error = %v", err)
	}
	var diskCfg corelib.AppConfig
	if err := json.Unmarshal(data, &diskCfg); err != nil {
		t.Fatalf("Unmarshal config.json error = %v", err)
	}
	if diskCfg.RemoteEmail == "" {
		t.Fatal("expected RemoteEmail to be persisted")
	}

	matches, err := filepath.Glob(configPath + ".tmp*")
	if err != nil {
		t.Fatalf("Glob temp files error = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temp files remain: %v", matches)
	}
}

func TestConfigManagerRemoteEnabledDoesNotChangeDefaultLaunchMode(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.DefaultLaunchMode = "local"
	cfg.RemoteEnabled = false
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	mgr := NewConfigManager(app)
	if _, err := mgr.UpdateConfig("remote", "remote_enabled", "true"); err != nil {
		t.Fatalf("UpdateConfig(remote_enabled) error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() after update error = %v", err)
	}
	if !saved.RemoteEnabled {
		t.Fatal("RemoteEnabled = false, want true")
	}
	if saved.DefaultLaunchMode != "local" {
		t.Fatalf("DefaultLaunchMode = %q, want local", saved.DefaultLaunchMode)
	}
}

func TestConfigManagerDefaultLaunchModeDoesNotChangeRemoteEnabled(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEnabled = true
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if err := app.SetDefaultLaunchMode("remote"); err != nil {
		t.Fatalf("SetDefaultLaunchMode(remote) error = %v", err)
	}

	mgr := NewConfigManager(app)
	if _, err := mgr.UpdateConfig("remote", "default_launch_mode", "local"); err != nil {
		t.Fatalf("UpdateConfig(default_launch_mode) error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() after update error = %v", err)
	}
	if saved.DefaultLaunchMode != "local" {
		t.Fatalf("DefaultLaunchMode = %q, want local", saved.DefaultLaunchMode)
	}
	if !saved.RemoteEnabled {
		t.Fatal("RemoteEnabled = false, want true")
	}
}

func TestSetDefaultLaunchMode(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEnabled = true
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if err := app.SetDefaultLaunchMode("remote"); err != nil {
		t.Fatalf("SetDefaultLaunchMode(remote) error = %v", err)
	}

	if err := app.SetDefaultLaunchMode(" local "); err != nil {
		t.Fatalf("SetDefaultLaunchMode(local) error = %v", err)
	}
	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() after local error = %v", err)
	}
	if saved.DefaultLaunchMode != "local" {
		t.Fatalf("DefaultLaunchMode = %q, want local", saved.DefaultLaunchMode)
	}
	if !saved.RemoteEnabled {
		t.Fatal("RemoteEnabled = false after local mode, want true")
	}

	saved.RemoteEnabled = false
	if err := app.SaveConfig(saved); err != nil {
		t.Fatalf("SaveConfig(disable remote) error = %v", err)
	}
	if err := app.SetDefaultLaunchMode("REMOTE"); err != nil {
		t.Fatalf("SetDefaultLaunchMode(remote) error = %v", err)
	}
	saved, err = app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() after remote error = %v", err)
	}
	if saved.DefaultLaunchMode != "remote" {
		t.Fatalf("DefaultLaunchMode = %q, want remote", saved.DefaultLaunchMode)
	}
	if !saved.RemoteEnabled {
		t.Fatal("RemoteEnabled = false after remote mode, want true")
	}

	if err := app.SetDefaultLaunchMode("cloud"); err == nil {
		t.Fatal("SetDefaultLaunchMode(invalid) error = nil, want error")
	}
}

func TestSaveConfigPreservesDefaultLaunchModeFromStaleSnapshot(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEnabled = true
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if err := app.SetDefaultLaunchMode("local"); err != nil {
		t.Fatalf("SetDefaultLaunchMode(local) error = %v", err)
	}

	stale := cfg
	stale.DefaultLaunchMode = "remote"
	stale.RemoteEmail = "stale@example.com"
	if err := app.SaveConfig(stale); err != nil {
		t.Fatalf("SaveConfig(stale) error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() after stale save error = %v", err)
	}
	if saved.RemoteEmail != "stale@example.com" {
		t.Fatalf("RemoteEmail = %q, want stale@example.com", saved.RemoteEmail)
	}
	if saved.DefaultLaunchMode != "local" {
		t.Fatalf("DefaultLaunchMode = %q, want local", saved.DefaultLaunchMode)
	}
}

func TestCoreConfigManagerDefaultLaunchModeUsesFieldSetter(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEnabled = true
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if err := app.SetDefaultLaunchMode("local"); err != nil {
		t.Fatalf("SetDefaultLaunchMode(local) error = %v", err)
	}

	mgr := coreconfig.NewManager(app)
	if _, err := mgr.UpdateConfig("remote", "default_launch_mode", "remote"); err != nil {
		t.Fatalf("UpdateConfig(default_launch_mode) error = %v", err)
	}
	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() after update error = %v", err)
	}
	if saved.DefaultLaunchMode != "remote" {
		t.Fatalf("DefaultLaunchMode = %q, want remote", saved.DefaultLaunchMode)
	}

	if err := mgr.BatchUpdate([]coreconfig.ConfigChange{
		{Section: "remote", Key: "remote_enabled", Value: "true"},
		{Section: "remote", Key: "default_launch_mode", Value: "local"},
	}); err != nil {
		t.Fatalf("BatchUpdate(default_launch_mode) error = %v", err)
	}
	saved, err = app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() after batch update error = %v", err)
	}
	if saved.DefaultLaunchMode != "local" {
		t.Fatalf("DefaultLaunchMode = %q, want local", saved.DefaultLaunchMode)
	}
}
