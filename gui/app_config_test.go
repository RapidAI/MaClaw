package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
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

	matches, err := filepath.Glob(configPath + ".tmp*")
	if err != nil {
		t.Fatalf("Glob temp files error = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temp files remain: %v", matches)
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
