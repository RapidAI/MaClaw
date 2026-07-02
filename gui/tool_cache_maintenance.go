package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

type ToolCacheStatus struct {
	Path             string `json:"path"`
	Exists           bool   `json:"exists"`
	SizeBytes        int64  `json:"size_bytes"`
	SizeApproximate  bool   `json:"size_approximate,omitempty"`
	AutoEnabled      bool   `json:"auto_enabled"`
	MaxBytes         int64  `json:"max_bytes"`
	MinIntervalHours int    `json:"min_interval_hours"`
	CleanOnStartup   bool   `json:"clean_on_startup"`
	CleanOnExit      bool   `json:"clean_on_exit"`
	LastCleanupAt    string `json:"last_cleanup_at,omitempty"`
}

type ToolCacheCleanupResult struct {
	Path        string `json:"path"`
	Exists      bool   `json:"exists"`
	BeforeBytes int64  `json:"before_bytes"`
	AfterBytes  int64  `json:"after_bytes"`
	FreedBytes  int64  `json:"freed_bytes"`
	Skipped     bool   `json:"skipped"`
	Reason      string `json:"reason,omitempty"`
}

var toolCacheCleanupMu sync.Mutex
var toolCacheBackgroundCleanupLauncher = launchBackgroundCacheCleanup

var errToolCacheSizeBudget = errors.New("tool cache size budget exhausted")

func (a *App) toolCacheDir() string {
	return filepath.Join(a.GetDataDir(), "cache")
}

func (a *App) GetToolCacheStatus() (ToolCacheStatus, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return ToolCacheStatus{}, err
	}
	maintenance := cfg.ToolCacheMaintenance.WithDefaults()
	path := a.toolCacheDir()
	size, exists, exceeded, timedOut, err := dirSizeAtLeast(path, maintenance.MaxBytes, 500*time.Millisecond)
	if err != nil {
		return ToolCacheStatus{}, err
	}
	return ToolCacheStatus{
		Path:             path,
		Exists:           exists,
		SizeBytes:        size,
		SizeApproximate:  exceeded || timedOut,
		AutoEnabled:      maintenance.Enabled,
		MaxBytes:         maintenance.MaxBytes,
		MinIntervalHours: maintenance.MinIntervalHours,
		CleanOnStartup:   maintenance.CleanOnStartup,
		CleanOnExit:      maintenance.CleanOnExit,
		LastCleanupAt:    maintenance.LastCleanupAt,
	}, nil
}

func (a *App) CleanToolCacheNow() (ToolCacheCleanupResult, error) {
	result, err := a.cleanToolCache(context.Background(), "manual", true)
	if err != nil {
		return result, err
	}
	return result, nil
}

func (a *App) maybeCleanToolCacheOnStartup(cfg corelib.AppConfig) {
	if a.isToolCacheMaintenanceSuppressedForTest() {
		return
	}
	maintenance := cfg.ToolCacheMaintenance.WithDefaults()
	if !maintenance.Enabled || !maintenance.CleanOnStartup {
		return
	}
	go func() {
		timer := time.NewTimer(45 * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-a.ctx.Done():
			return
		}
		ctx, cancel := context.WithTimeout(a.ctx, 45*time.Second)
		defer cancel()
		result, err := a.cleanToolCache(ctx, "startup", false)
		if err != nil {
			log.Printf("[tool-cache] startup cleanup failed: %v", err)
			return
		}
		if result.Skipped {
			log.Printf("[tool-cache] startup cleanup skipped: %s", result.Reason)
		} else {
			log.Printf("[tool-cache] startup cleanup freed=%d before=%d after=%d", result.FreedBytes, result.BeforeBytes, result.AfterBytes)
		}
	}()
}

func (a *App) maybeCleanToolCacheOnExit() {
	if a.isToolCacheMaintenanceSuppressedForTest() {
		return
	}
	result, err := a.queueToolCacheCleanupOnExit()
	if err != nil {
		log.Printf("[tool-cache] shutdown cleanup failed: %v", err)
		return
	}
	if result.Skipped {
		log.Printf("[tool-cache] shutdown cleanup skipped: %s", result.Reason)
		return
	}
	log.Printf("[tool-cache] shutdown cleanup queued before=%d path=%q", result.BeforeBytes, result.Path)
}

func (a *App) queueToolCacheCleanupOnExit() (ToolCacheCleanupResult, error) {
	toolCacheCleanupMu.Lock()
	defer toolCacheCleanupMu.Unlock()

	cfg, err := a.LoadConfig()
	if err != nil {
		return ToolCacheCleanupResult{}, err
	}
	maintenance := cfg.ToolCacheMaintenance.WithDefaults()
	cacheDir := a.toolCacheDir()
	result := ToolCacheCleanupResult{Path: cacheDir}
	if !maintenance.Enabled || !maintenance.CleanOnExit {
		result.Skipped = true
		result.Reason = "disabled"
		return result, nil
	}
	before, exists, exceeded, timedOut, err := dirSizeAtLeast(cacheDir, maintenance.MaxBytes, 250*time.Millisecond)
	if err != nil {
		return result, err
	}
	result.Exists = exists
	result.BeforeBytes = before
	if !exists || (before == 0 && !timedOut) {
		result.Skipped = true
		result.Reason = "empty"
		return result, nil
	}
	if !exceeded && !timedOut {
		result.Skipped = true
		result.Reason = "below_threshold"
		return result, nil
	}
	if !cleanupIntervalElapsed(maintenance.LastCleanupAt, maintenance.MinIntervalHours) {
		result.Skipped = true
		result.Reason = "interval_not_elapsed"
		return result, nil
	}
	if err := ensureToolCachePath(cacheDir, a.GetDataDir()); err != nil {
		return result, err
	}
	entries, err := cacheChildren(cacheDir)
	if err != nil {
		return result, err
	}
	if len(entries) == 0 {
		result.Skipped = true
		result.Reason = "empty"
		return result, nil
	}
	if err := toolCacheBackgroundCleanupLauncher(entries); err != nil {
		return result, err
	}
	return result, nil
}

func (a *App) isToolCacheMaintenanceSuppressedForTest() bool {
	if strings.TrimSpace(a.testHomeDir) != "" {
		return false
	}
	exe := strings.ToLower(filepath.Base(os.Args[0]))
	return strings.HasSuffix(exe, ".test") || strings.HasSuffix(exe, ".test.exe")
}

func (a *App) cleanToolCache(ctx context.Context, reason string, force bool) (ToolCacheCleanupResult, error) {
	toolCacheCleanupMu.Lock()
	defer toolCacheCleanupMu.Unlock()

	cfg, err := a.LoadConfig()
	if err != nil {
		return ToolCacheCleanupResult{}, err
	}
	maintenance := cfg.ToolCacheMaintenance.WithDefaults()
	cacheDir := a.toolCacheDir()
	result := ToolCacheCleanupResult{Path: cacheDir}
	var before int64
	var exists bool
	if force {
		var err error
		before, exists, err = dirSize(cacheDir)
		if err != nil {
			return result, err
		}
		result.Exists = exists
		result.BeforeBytes = before
		if !exists || before == 0 {
			result.Skipped = true
			result.Reason = "empty"
			return result, nil
		}
	} else {
		var exceeded, timedOut bool
		var err error
		before, exists, exceeded, timedOut, err = dirSizeAtLeast(cacheDir, maintenance.MaxBytes, 2*time.Second)
		if err != nil {
			return result, err
		}
		result.Exists = exists
		result.BeforeBytes = before
		if !exists || (before == 0 && !timedOut) {
			result.Skipped = true
			result.Reason = "empty"
			return result, nil
		}
		if !exceeded && !timedOut {
			result.Skipped = true
			result.Reason = "below_threshold"
			return result, nil
		}
	}
	if !force {
		if !cleanupIntervalElapsed(maintenance.LastCleanupAt, maintenance.MinIntervalHours) {
			result.Skipped = true
			result.Reason = "interval_not_elapsed"
			return result, nil
		}
	}
	if err := ensureToolCachePath(cacheDir, a.GetDataDir()); err != nil {
		return result, err
	}
	if err := removeCacheChildren(ctx, cacheDir); err != nil {
		return result, err
	}
	after, afterExists, err := dirSize(cacheDir)
	if err != nil {
		return result, err
	}
	result.Exists = afterExists
	result.AfterBytes = after
	result.FreedBytes = before - after
	if result.FreedBytes < 0 {
		result.FreedBytes = 0
	}
	now := time.Now().Format(time.RFC3339)
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.ToolCacheMaintenance = cfg.ToolCacheMaintenance.WithDefaults()
		cfg.ToolCacheMaintenance.LastCleanupAt = now
	}); err != nil {
		log.Printf("[tool-cache] cleanup timestamp save failed after %s cleanup: %v", reason, err)
	}
	return result, nil
}

func dirSize(path string) (int64, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	if !info.IsDir() {
		return 0, false, fmt.Errorf("%s is not a directory", path)
	}
	var total int64
	err = filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total, true, err
}

func dirSizeAtLeast(path string, threshold int64, maxDuration time.Duration) (size int64, exists bool, exceeded bool, timedOut bool, err error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, false, false, nil
		}
		return 0, false, false, false, err
	}
	if !info.IsDir() {
		return 0, false, false, false, fmt.Errorf("%s is not a directory", path)
	}
	useDeadline := maxDuration != 0
	deadline := time.Now().Add(maxDuration)
	err = filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if useDeadline && time.Now().After(deadline) {
			timedOut = true
			return errToolCacheSizeBudget
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		size += info.Size()
		if threshold > 0 && size >= threshold {
			exceeded = true
			return errToolCacheSizeBudget
		}
		return nil
	})
	if errors.Is(err, errToolCacheSizeBudget) {
		err = nil
	}
	return size, true, exceeded, timedOut, err
}

func cleanupIntervalElapsed(last string, minHours int) bool {
	if strings.TrimSpace(last) == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339, last)
	if err != nil {
		return true
	}
	return time.Since(t) >= time.Duration(minHours)*time.Hour
}

func ensureToolCachePath(cacheDir, dataDir string) error {
	cleanCache, err := filepath.Abs(filepath.Clean(cacheDir))
	if err != nil {
		return err
	}
	cleanData, err := filepath.Abs(filepath.Clean(dataDir))
	if err != nil {
		return err
	}
	if filepath.Base(cleanCache) != "cache" {
		return fmt.Errorf("refusing to clean non-cache directory: %s", cleanCache)
	}
	rel, err := filepath.Rel(cleanData, cleanCache)
	if err != nil {
		return err
	}
	if rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return fmt.Errorf("refusing to clean cache outside data directory: %s", cleanCache)
	}
	return nil
}

func removeCacheChildren(ctx context.Context, cacheDir string) error {
	entries, err := cacheChildren(cacheDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := os.RemoveAll(entry); err != nil {
			return err
		}
	}
	return os.MkdirAll(cacheDir, 0o755)
}

func cacheChildren(cacheDir string) ([]string, error) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, filepath.Join(cacheDir, entry.Name()))
	}
	return paths, nil
}

func launchBackgroundCacheCleanup(paths []string) error {
	for _, path := range paths {
		cleanPath, err := filepath.Abs(filepath.Clean(path))
		if err != nil {
			return err
		}
		var cmd *exec.Cmd
		if goruntime.GOOS == "windows" {
			cmd = exec.Command(
				"powershell.exe",
				"-NoProfile",
				"-NonInteractive",
				"-WindowStyle",
				"Hidden",
				"-Command",
				"Remove-Item -LiteralPath $args[0] -Recurse -Force -ErrorAction SilentlyContinue",
				cleanPath,
			)
		} else {
			cmd = exec.Command("rm", "-rf", "--", cleanPath)
		}
		if err := cmd.Start(); err != nil {
			return err
		}
		_ = cmd.Process.Release()
	}
	return nil
}
