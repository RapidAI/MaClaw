package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// --- Test 1: GetCached returns nil for uncached tools ---

func TestGetCached_ReturnsNilWhenNoCacheExists(t *testing.T) {
	c := &ToolVersionCache{}
	result := c.GetCached("nonexistent-tool")
	if result != nil {
		t.Errorf("expected nil for uncached tool, got %+v", result)
	}
}

// --- Test 2: GetCached returns entry after Store ---

func TestGetCached_ReturnsEntryAfterStore(t *testing.T) {
	c := &ToolVersionCache{}

	entry := &cachedVersion{
		Version:   "1.2.3",
		CheckedAt: time.Now(),
		Installed: true,
		Path:      "/usr/local/bin/mytool",
	}
	c.versions.Store("mytool", entry)

	result := c.GetCached("mytool")
	if result == nil {
		t.Fatal("expected non-nil result after Store")
	}
	if result.Version != "1.2.3" {
		t.Errorf("expected version 1.2.3, got %s", result.Version)
	}
	if !result.Installed {
		t.Error("expected Installed=true")
	}
	if result.Path != "/usr/local/bin/mytool" {
		t.Errorf("expected path /usr/local/bin/mytool, got %s", result.Path)
	}
}

// --- Test 3: RefreshAllAsync completes within timeout ---

func TestRefreshAllAsync_CompletesWithinTimeout(t *testing.T) {
	c := &ToolVersionCache{}

	// Use tools that likely don't exist to ensure quick LookPath failure.
	tools := []string{"__nonexistent_tool_abc__", "__nonexistent_tool_xyz__"}

	// RefreshAllAsync spawns a goroutine. We call refreshAll directly to test synchronously.
	done := make(chan struct{})
	go func() {
		c.refreshAll(tools, 10*time.Second)
		close(done)
	}()

	select {
	case <-done:
		// Completed within timeout — good.
	case <-time.After(12 * time.Second):
		t.Fatal("refreshAll did not complete within 12 seconds (timeout is 10s)")
	}

	// Verify entries were stored (as not installed).
	for _, tool := range tools {
		entry := c.GetCached(tool)
		if entry == nil {
			t.Errorf("expected cached entry for %s after refresh", tool)
			continue
		}
		if entry.Installed {
			t.Errorf("expected %s to be not installed", tool)
		}
	}
}

// --- Test 4: JSON persistence round-trip (save then load) ---

func TestJSONPersistence_RoundTrip(t *testing.T) {
	// Create a temp directory for the cache file.
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "tool_version_cache.json")

	// Prepare test data.
	now := time.Now().Truncate(time.Second) // Truncate for JSON round-trip precision.
	testData := persistedCache{
		Tools: map[string]*cachedVersion{
			"claude": {
				Version:   "2.1.29",
				CheckedAt: now,
				Installed: true,
				Path:      "/usr/local/bin/claude",
			},
			"codex": {
				Version:   "0.1.5",
				CheckedAt: now,
				Installed: true,
				Path:      "/usr/local/bin/codex",
			},
			"missing-tool": {
				Version:   "",
				CheckedAt: now,
				Installed: false,
				Path:      "",
			},
		},
	}

	// Write the cache file.
	data, err := json.MarshalIndent(testData, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal test data: %v", err)
	}
	if err := os.WriteFile(cacheFile, data, 0o644); err != nil {
		t.Fatalf("failed to write cache file: %v", err)
	}

	// Load into a new ToolVersionCache by manually calling loadFromDisk logic.
	c := &ToolVersionCache{}
	fileData, err := os.ReadFile(cacheFile)
	if err != nil {
		t.Fatalf("failed to read cache file: %v", err)
	}
	var pc persistedCache
	if err := json.Unmarshal(fileData, &pc); err != nil {
		t.Fatalf("failed to unmarshal cache: %v", err)
	}
	for name, entry := range pc.Tools {
		if entry != nil {
			c.versions.Store(name, entry)
		}
	}

	// Verify loaded data.
	claude := c.GetCached("claude")
	if claude == nil {
		t.Fatal("expected claude entry after load")
	}
	if claude.Version != "2.1.29" {
		t.Errorf("expected claude version 2.1.29, got %s", claude.Version)
	}
	if !claude.Installed {
		t.Error("expected claude Installed=true")
	}
	if claude.Path != "/usr/local/bin/claude" {
		t.Errorf("expected claude path /usr/local/bin/claude, got %s", claude.Path)
	}

	codex := c.GetCached("codex")
	if codex == nil {
		t.Fatal("expected codex entry after load")
	}
	if codex.Version != "0.1.5" {
		t.Errorf("expected codex version 0.1.5, got %s", codex.Version)
	}

	missing := c.GetCached("missing-tool")
	if missing == nil {
		t.Fatal("expected missing-tool entry after load")
	}
	if missing.Installed {
		t.Error("expected missing-tool Installed=false")
	}
	if missing.Version != "" {
		t.Errorf("expected empty version for missing-tool, got %s", missing.Version)
	}

	// Now test saveToDisk by storing data and writing.
	c2 := &ToolVersionCache{}
	c2.versions.Store("newtool", &cachedVersion{
		Version:   "3.0.0",
		CheckedAt: now,
		Installed: true,
		Path:      "/opt/bin/newtool",
	})

	// Manually build persisted cache and write (simulating saveToDisk).
	pc2 := persistedCache{Tools: make(map[string]*cachedVersion)}
	c2.versions.Range(func(key, value any) bool {
		name, _ := key.(string)
		entry, _ := value.(*cachedVersion)
		if name != "" && entry != nil {
			pc2.Tools[name] = entry
		}
		return true
	})
	data2, err := json.MarshalIndent(pc2, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal for save: %v", err)
	}
	saveFile := filepath.Join(tmpDir, "save_test.json")
	if err := os.WriteFile(saveFile, data2, 0o644); err != nil {
		t.Fatalf("failed to write save file: %v", err)
	}

	// Re-read and verify.
	savedData, err := os.ReadFile(saveFile)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}
	var pc3 persistedCache
	if err := json.Unmarshal(savedData, &pc3); err != nil {
		t.Fatalf("failed to unmarshal saved data: %v", err)
	}
	if pc3.Tools["newtool"] == nil {
		t.Fatal("expected newtool in saved data")
	}
	if pc3.Tools["newtool"].Version != "3.0.0" {
		t.Errorf("expected newtool version 3.0.0, got %s", pc3.Tools["newtool"].Version)
	}
}

// --- Test 5: GetInstallStatus finds tools on PATH without executing them ---

func TestGetInstallStatus_FindsToolOnPATH(t *testing.T) {
	c := &ToolVersionCache{}

	// "go" should be on PATH in any Go development environment.
	installed, path := c.GetInstallStatus("go")
	if !installed {
		t.Skip("'go' binary not found on PATH; skipping test")
	}
	if path == "" {
		t.Error("expected non-empty path when tool is installed")
	}

	// Verify a non-existent tool returns false.
	installed2, path2 := c.GetInstallStatus("__absolutely_nonexistent_binary_12345__")
	if installed2 {
		t.Error("expected non-existent tool to return installed=false")
	}
	if path2 != "" {
		t.Errorf("expected empty path for non-existent tool, got %s", path2)
	}
}

func TestGetInstallStatus_DoesNotExecuteBinary(t *testing.T) {
	// This test verifies the contract that GetInstallStatus only uses
	// exec.LookPath / file stat, never exec.Command.Output().
	// We test this by checking a tool that exists but would fail if executed
	// with no arguments (like "cmd" on Windows or "ls" on Unix).
	c := &ToolVersionCache{}

	// Use a common system binary that exists on PATH.
	var testBinary string
	if _, err := os.Stat("C:\\Windows\\System32\\cmd.exe"); err == nil {
		testBinary = "cmd"
	} else {
		testBinary = "ls"
	}

	// GetInstallStatus should return quickly (< 100ms) because it only
	// does LookPath, not execution.
	start := time.Now()
	installed, _ := c.GetInstallStatus(testBinary)
	elapsed := time.Since(start)

	if !installed {
		t.Skipf("'%s' not found on PATH; skipping", testBinary)
	}

	// LookPath should complete in < 100ms. If it took longer, it might
	// be executing the binary (which would be a bug).
	if elapsed > 100*time.Millisecond {
		t.Errorf("GetInstallStatus took %v, expected < 100ms (may be executing binary)", elapsed)
	}
}

// --- Test 6: GetCachedToolNames returns all cached tool names ---

func TestGetCachedToolNames_ReturnsAllCachedNames(t *testing.T) {
	c := &ToolVersionCache{}

	// Empty cache should return empty list.
	names := c.GetCachedToolNames()
	if len(names) != 0 {
		t.Errorf("expected empty list for empty cache, got %v", names)
	}

	// Store some entries.
	c.versions.Store("claude", &cachedVersion{Version: "2.1.29", Installed: true})
	c.versions.Store("codex", &cachedVersion{Version: "0.1.5", Installed: true})
	c.versions.Store("aider", &cachedVersion{Version: "0.50.0", Installed: false})

	names = c.GetCachedToolNames()
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d: %v", len(names), names)
	}

	// Check all expected names are present (order is not guaranteed with sync.Map).
	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}
	for _, expected := range []string{"claude", "codex", "aider"} {
		if !nameSet[expected] {
			t.Errorf("expected %s in cached tool names, got %v", expected, names)
		}
	}
}

// --- Additional: Concurrent access safety ---

func TestToolVersionCache_ConcurrentAccess(t *testing.T) {
	c := &ToolVersionCache{}

	// Concurrent writes and reads should not panic.
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(idx int) {
			defer wg.Done()
			name := "tool-" + string(rune('a'+idx%26))
			c.versions.Store(name, &cachedVersion{
				Version:   "1.0.0",
				CheckedAt: time.Now(),
				Installed: true,
				Path:      "/bin/" + name,
			})
		}(i)
		go func(idx int) {
			defer wg.Done()
			name := "tool-" + string(rune('a'+idx%26))
			_ = c.GetCached(name)
		}(i)
	}
	wg.Wait()

	// Also test GetCachedToolNames under concurrent access.
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func() {
			defer wg.Done()
			_ = c.GetCachedToolNames()
		}()
	}
	wg.Wait()
}

// --- RefreshAllAsync with empty tools list ---

func TestRefreshAllAsync_EmptyToolsList(t *testing.T) {
	c := &ToolVersionCache{}

	// Should not panic or hang with empty list.
	done := make(chan struct{})
	go func() {
		c.refreshAll(nil, 5*time.Second)
		close(done)
	}()

	select {
	case <-done:
		// Good — completed quickly.
	case <-time.After(2 * time.Second):
		t.Fatal("refreshAll with empty tools list should complete immediately")
	}
}
