package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// cachedVersion holds cached version information for a single tool.
type cachedVersion struct {
	Version   string    `json:"version"`
	CheckedAt time.Time `json:"checked_at"`
	Installed bool      `json:"installed"`
	Path      string    `json:"path"`
}

// ToolVersionCache caches tool version information to avoid
// synchronous external process execution during Send_Hello.
// It uses sync.Map for concurrent-safe access and persists
// data to the active data directory.
type ToolVersionCache struct {
	versions sync.Map // map[string]*cachedVersion
}

// NewToolVersionCache creates a new ToolVersionCache and loads
// any previously persisted cache from disk.
func NewToolVersionCache() *ToolVersionCache {
	c := &ToolVersionCache{}
	c.loadFromDisk()
	return c
}

// GetCached returns cached version info for the named tool, or nil if not cached.
func (c *ToolVersionCache) GetCached(name string) *cachedVersion {
	v, ok := c.versions.Load(name)
	if !ok {
		return nil
	}
	cv, _ := v.(*cachedVersion)
	return cv
}

// GetInstallStatus checks binary existence only via file stat / LookPath.
// It NEVER executes the binary to obtain version output.
//
// Prefer the app private tools dir over PATH so version probes target the same
// binary the remote session launcher uses (privateToolsDirForApp), not a
// system-wide Claude install that happens to be on PATH.
func (c *ToolVersionCache) GetInstallStatus(name string) (installed bool, path string) {
	meta, ok := lookupRemoteToolMetadata(name)
	binaryName := name
	if ok && meta.BinaryName != "" {
		binaryName = meta.BinaryName
	}

	// 1) Private tools directory (app-managed install).
	toolsDir := filepath.Join(corelib.MaclawDataDir(), "tools")
	candidates := []string{binaryName}
	if binaryName != name {
		candidates = append(candidates, name)
	}
	for _, candidate := range candidates {
		fullPath := filepath.Join(toolsDir, candidate)
		if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
			return true, fullPath
		}
		exePath := fullPath + ".exe"
		if info, err := os.Stat(exePath); err == nil && !info.IsDir() {
			return true, exePath
		}
	}

	// 2) PATH fallback (optional system install).
	if p, err := exec.LookPath(binaryName); err == nil {
		return true, p
	}

	return false, ""
}

// GetCachedToolNames returns the list of tool names that have cached entries.
func (c *ToolVersionCache) GetCachedToolNames() []string {
	var names []string
	c.versions.Range(func(key, _ any) bool {
		if name, ok := key.(string); ok {
			names = append(names, name)
		}
		return true
	})
	return names
}

// RefreshAllAsync starts parallel version checks for the given tools in background
// goroutines. Each tool's version is checked concurrently with a combined timeout.
// After all checks complete (or timeout), results are persisted to disk.
func (c *ToolVersionCache) RefreshAllAsync(tools []string, timeout time.Duration) {
	go c.refreshAll(tools, timeout)
}

// refreshAll performs the actual parallel version refresh.
func (c *ToolVersionCache) refreshAll(tools []string, timeout time.Duration) {
	if len(tools) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var wg sync.WaitGroup
	for _, tool := range tools {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			c.refreshOne(ctx, name)
		}(tool)
	}

	// Wait for all goroutines or context timeout.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All completed normally.
	case <-ctx.Done():
		// Timeout reached; wait briefly for in-flight goroutines to store
		// partial results before persisting.
		log.Printf("[ToolVersionCache] refresh timed out after %v, waiting for in-flight goroutines", timeout)
		// Give goroutines a short grace period to finish storing their results.
		graceDone := make(chan struct{})
		go func() {
			wg.Wait()
			close(graceDone)
		}()
		select {
		case <-graceDone:
		case <-time.After(2 * time.Second):
		}
	}

	c.saveToDisk()
}

// toolVersionCacheFreshTTL skips re-executing tool binaries when the same path
// was probed recently (including failed/empty version results). Hub reconnects
// call RefreshAllAsync on every machine.hello; without this gate each reconnect
// can spawn coding CLIs.
const toolVersionCacheFreshTTL = 1 * time.Hour

// sameToolBinaryPath compares install paths for cache skip decisions.
// Windows paths are compared case-insensitively after Clean.
func sameToolBinaryPath(a, b string) bool {
	a = filepath.Clean(strings.TrimSpace(a))
	b = filepath.Clean(strings.TrimSpace(b))
	if a == "" || b == "" {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// refreshOne checks the version of a single tool by executing it.
func (c *ToolVersionCache) refreshOne(ctx context.Context, name string) {
	installed, path := c.GetInstallStatus(name)

	// Always re-check install state (cheap path/LookPath). Only skip the
	// expensive binary exec when we already probed this exact path recently —
	// hub reconnects call this on every machine.hello.
	//
	// Skip even when Version is empty: a recent failed/unparseable --version
	// must not re-spawn the CLI on every reconnect (that was a thrash path for
	// Claude Code process storms).
	if installed && path != "" {
		if existing := c.GetCached(name); existing != nil &&
			existing.Installed &&
			sameToolBinaryPath(existing.Path, path) &&
			time.Since(existing.CheckedAt) < toolVersionCacheFreshTTL {
			return
		}
	}

	entry := &cachedVersion{
		CheckedAt: time.Now(),
		Installed: installed,
		Path:      path,
	}

	if installed && path != "" {
		// Safe probe only: --version. Never pass bare "version"/"-v" which
		// coding CLIs treat as a user prompt and start a full session.
		entry.Version = c.detectVersion(ctx, path)
	}

	c.versions.Store(name, entry)
}

// detectVersion probes a tool binary with --version only.
//
// IMPORTANT: never pass bare "version" or ambiguous "-v". Coding CLIs such as
// Claude Code treat non-flag args as a user prompt and start a full session —
// the source of batch "Claude Code" processes under MacLaw/TigerClaw after
// hub reconnects that re-ran RefreshAllAsync.
func (c *ToolVersionCache) detectVersion(ctx context.Context, path string) string {
	toolCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if ctx.Err() != nil {
		return ""
	}

	cmd := exec.CommandContext(toolCtx, path, "--version")
	cmd.Env = os.Environ()
	hideCommandWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	version := strings.TrimSpace(string(out))
	if idx := strings.IndexByte(version, '\n'); idx > 0 {
		version = strings.TrimSpace(version[:idx])
	}
	if len(version) == 0 || len(version) >= 200 {
		return ""
	}
	return version
}

// cacheFilePath returns the path to the JSON persistence file.
func (c *ToolVersionCache) cacheFilePath() string {
	return filepath.Join(corelib.MaclawDataDir(), "tool_version_cache.json")
}

// persistedCache is the JSON structure for disk persistence.
type persistedCache struct {
	Tools map[string]*cachedVersion `json:"tools"`
}

// loadFromDisk loads cached version data from the JSON file.
func (c *ToolVersionCache) loadFromDisk() {
	path := c.cacheFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		// File doesn't exist or can't be read — start with empty cache.
		return
	}

	var pc persistedCache
	if err := json.Unmarshal(data, &pc); err != nil {
		log.Printf("[ToolVersionCache] failed to parse cache file %s: %v", path, err)
		return
	}

	for name, entry := range pc.Tools {
		if entry != nil {
			c.versions.Store(name, entry)
		}
	}
}

// saveToDisk persists the current cache to the JSON file.
func (c *ToolVersionCache) saveToDisk() {
	pc := persistedCache{
		Tools: make(map[string]*cachedVersion),
	}

	c.versions.Range(func(key, value any) bool {
		name, _ := key.(string)
		entry, _ := value.(*cachedVersion)
		if name != "" && entry != nil {
			pc.Tools[name] = entry
		}
		return true
	})

	data, err := json.MarshalIndent(pc, "", "  ")
	if err != nil {
		log.Printf("[ToolVersionCache] failed to marshal cache: %v", err)
		return
	}

	path := c.cacheFilePath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[ToolVersionCache] failed to create cache directory %s: %v", dir, err)
		return
	}

	// Write to temp file then rename for atomicity.
	// On Windows, os.Rename fails if the destination exists, so remove it first.
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		log.Printf("[ToolVersionCache] failed to write cache file %s: %v", tmpPath, err)
		return
	}

	// Remove destination before rename (required on Windows).
	_ = os.Remove(path)
	if err := os.Rename(tmpPath, path); err != nil {
		log.Printf("[ToolVersionCache] failed to rename cache file: %v", err)
		// Clean up temp file on failure.
		_ = os.Remove(tmpPath)
	}
}
