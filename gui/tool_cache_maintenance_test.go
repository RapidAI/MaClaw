package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestCleanToolCacheNowRemovesCacheChildrenOnly(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	cacheDir := app.toolCacheDir()
	if err := os.MkdirAll(filepath.Join(cacheDir, "_cacache", "content-v2"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "_cacache", "content-v2", "blob"), []byte("cached package"), 0o644); err != nil {
		t.Fatal(err)
	}
	dataFile := filepath.Join(app.GetDataDir(), "keep.db")
	if err := os.MkdirAll(filepath.Dir(dataFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataFile, []byte("persistent"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := app.CleanToolCacheNow()
	if err != nil {
		t.Fatalf("CleanToolCacheNow() error = %v", err)
	}
	if result.FreedBytes == 0 {
		t.Fatalf("FreedBytes = 0, want cache bytes freed; result=%+v", result)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "_cacache")); !os.IsNotExist(err) {
		t.Fatalf("_cacache stat error = %v, want not exist", err)
	}
	if _, err := os.Stat(dataFile); err != nil {
		t.Fatalf("persistent data file removed or inaccessible: %v", err)
	}
}

func TestCleanToolCacheNowReportsMissingCacheDir(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}

	result, err := app.CleanToolCacheNow()
	if err != nil {
		t.Fatalf("CleanToolCacheNow() error = %v", err)
	}
	if !result.Skipped || result.Reason != "empty" {
		t.Fatalf("result = %+v, want empty skip", result)
	}
	if result.Exists {
		t.Fatal("Exists = true, want false for missing cache dir")
	}
	if result.BeforeBytes != 0 || result.AfterBytes != 0 || result.FreedBytes != 0 {
		t.Fatalf("result sizes = before:%d after:%d freed:%d, want all zero", result.BeforeBytes, result.AfterBytes, result.FreedBytes)
	}
}

func TestCleanToolCacheSkipsBelowThreshold(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	cacheDir := app.toolCacheDir()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cacheFile := filepath.Join(cacheDir, "small-cache-entry")
	if err := os.WriteFile(cacheFile, []byte("small"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.ToolCacheMaintenance = corelib.ToolCacheMaintenanceConfig{
			Enabled:          true,
			MaxBytes:         1024,
			MinIntervalHours: 24,
			CleanOnStartup:   true,
			CleanOnExit:      true,
		}
	}); err != nil {
		t.Fatalf("PatchConfig() error = %v", err)
	}

	result, err := app.cleanToolCache(context.Background(), "test", false)
	if err != nil {
		t.Fatalf("cleanToolCache() error = %v", err)
	}
	if !result.Skipped || result.Reason != "below_threshold" {
		t.Fatalf("result = %+v, want below_threshold skip", result)
	}
	if _, err := os.Stat(cacheFile); err != nil {
		t.Fatalf("cache file should remain: %v", err)
	}
}

func TestGetToolCacheStatusMarksApproximateAboveThreshold(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	cacheDir := app.toolCacheDir()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "large"), []byte("cached package"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.ToolCacheMaintenance = corelib.ToolCacheMaintenanceConfig{
			Enabled:          true,
			MaxBytes:         1,
			MinIntervalHours: 24,
			CleanOnStartup:   true,
			CleanOnExit:      true,
		}
	}); err != nil {
		t.Fatalf("PatchConfig() error = %v", err)
	}

	status, err := app.GetToolCacheStatus()
	if err != nil {
		t.Fatalf("GetToolCacheStatus() error = %v", err)
	}
	if !status.Exists {
		t.Fatal("Exists = false, want true")
	}
	if status.SizeBytes < 1 {
		t.Fatalf("SizeBytes = %d, want at least threshold", status.SizeBytes)
	}
	if !status.SizeApproximate {
		t.Fatal("SizeApproximate = false, want true after threshold short-circuit")
	}
}

func TestDirSizeAtLeastReportsTimeoutBeforeCountingFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cached"), []byte("cached package"), 0o644); err != nil {
		t.Fatal(err)
	}

	size, exists, exceeded, timedOut, err := dirSizeAtLeast(dir, 1024, -time.Nanosecond)
	if err != nil {
		t.Fatalf("dirSizeAtLeast() error = %v", err)
	}
	if !exists {
		t.Fatal("exists = false, want true")
	}
	if exceeded {
		t.Fatal("exceeded = true, want false before counting files")
	}
	if !timedOut {
		t.Fatal("timedOut = false, want true")
	}
	if size != 0 {
		t.Fatalf("size = %d, want 0 before counting files", size)
	}
}

func TestQueueToolCacheCleanupOnExitLaunchesBackgroundCleanup(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	cacheDir := app.toolCacheDir()
	if err := os.MkdirAll(filepath.Join(cacheDir, "_cacache"), 0o755); err != nil {
		t.Fatal(err)
	}
	cacheFile := filepath.Join(cacheDir, "_cacache", "blob")
	if err := os.WriteFile(cacheFile, []byte("cached package"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.ToolCacheMaintenance = corelib.ToolCacheMaintenanceConfig{
			Enabled:          true,
			MaxBytes:         1,
			MinIntervalHours: 24,
			CleanOnStartup:   true,
			CleanOnExit:      true,
		}
	}); err != nil {
		t.Fatalf("PatchConfig() error = %v", err)
	}

	var launched []string
	previousLauncher := toolCacheBackgroundCleanupLauncher
	toolCacheBackgroundCleanupLauncher = func(paths []string) error {
		launched = append(launched, paths...)
		return nil
	}
	t.Cleanup(func() { toolCacheBackgroundCleanupLauncher = previousLauncher })

	result, err := app.queueToolCacheCleanupOnExit()
	if err != nil {
		t.Fatalf("queueToolCacheCleanupOnExit() error = %v", err)
	}
	if result.Skipped {
		t.Fatalf("queueToolCacheCleanupOnExit() skipped: %+v", result)
	}
	if len(launched) != 1 || filepath.Base(launched[0]) != "_cacache" {
		t.Fatalf("launched paths = %v, want _cacache child", launched)
	}
	if _, err := os.Stat(cacheFile); err != nil {
		t.Fatalf("shutdown queue should not synchronously remove cache file: %v", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.ToolCacheMaintenance.LastCleanupAt != "" {
		t.Fatalf("LastCleanupAt = %q, want empty until cleanup actually completes", cfg.ToolCacheMaintenance.LastCleanupAt)
	}
}

func TestQueueToolCacheCleanupOnExitSkipsBelowThreshold(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	cacheDir := app.toolCacheDir()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "tiny"), []byte("tiny"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.ToolCacheMaintenance = corelib.ToolCacheMaintenanceConfig{
			Enabled:          true,
			MaxBytes:         1024,
			MinIntervalHours: 24,
			CleanOnStartup:   true,
			CleanOnExit:      true,
		}
	}); err != nil {
		t.Fatalf("PatchConfig() error = %v", err)
	}

	launched := false
	previousLauncher := toolCacheBackgroundCleanupLauncher
	toolCacheBackgroundCleanupLauncher = func(paths []string) error {
		launched = true
		return nil
	}
	t.Cleanup(func() { toolCacheBackgroundCleanupLauncher = previousLauncher })

	result, err := app.queueToolCacheCleanupOnExit()
	if err != nil {
		t.Fatalf("queueToolCacheCleanupOnExit() error = %v", err)
	}
	if !result.Skipped || result.Reason != "below_threshold" {
		t.Fatalf("result = %+v, want below_threshold skip", result)
	}
	if launched {
		t.Fatal("background cleanup launched below threshold")
	}
}

func TestPatchToolCacheMaintenancePreservesOmittedBooleans(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.ToolCacheMaintenance = corelib.ToolCacheMaintenanceConfig{
			Enabled:          true,
			MaxBytes:         256,
			MinIntervalHours: 12,
			CleanOnStartup:   false,
			CleanOnExit:      false,
		}
	}); err != nil {
		t.Fatalf("PatchConfig() error = %v", err)
	}

	cfg, err := app.PatchConfigFields(map[string]interface{}{
		"tool_cache_maintenance": map[string]interface{}{
			"max_bytes": float64(1024),
		},
	})
	if err != nil {
		t.Fatalf("PatchConfigFields() error = %v", err)
	}
	maintenance := cfg.ToolCacheMaintenance
	if maintenance.MaxBytes != 1024 {
		t.Fatalf("MaxBytes = %d, want 1024", maintenance.MaxBytes)
	}
	if !maintenance.Enabled {
		t.Fatal("Enabled changed to false after partial patch")
	}
	if maintenance.CleanOnStartup {
		t.Fatal("CleanOnStartup changed to true after partial patch")
	}
	if maintenance.CleanOnExit {
		t.Fatal("CleanOnExit changed to true after partial patch")
	}
	if maintenance.MinIntervalHours != 12 {
		t.Fatalf("MinIntervalHours = %d, want 12", maintenance.MinIntervalHours)
	}
}

func TestEnsureToolCachePathRejectsOutsideDataDir(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	outside := filepath.Join(t.TempDir(), "cache")
	if err := ensureToolCachePath(outside, dataDir); err == nil {
		t.Fatal("ensureToolCachePath accepted cache path outside data dir")
	}
}
