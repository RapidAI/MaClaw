package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestToolVersionCacheRefreshClearsStaleInstalledWhenMissing(t *testing.T) {
	c := &ToolVersionCache{}
	name := "__maclaw_no_such_coding_cli__"
	c.versions.Store(name, &cachedVersion{
		Version:   "9.9.9",
		CheckedAt: time.Now(),
		Installed: true,
		Path:      `C:\does\not\exist\claude.exe`,
	})
	// Install probe is cheap and must refresh state even when version TTL is fresh.
	c.refreshOne(context.Background(), name)
	entry := c.GetCached(name)
	if entry == nil {
		t.Fatal("expected cache entry after refresh")
	}
	if entry.Installed {
		t.Fatalf("expected uninstalled after path miss, got %#v", entry)
	}
	if entry.Version != "" {
		t.Fatalf("expected empty version when uninstalled, got %q", entry.Version)
	}
}

func TestToolVersionCacheRefreshSkipsRecentProbeEvenWhenVersionEmpty(t *testing.T) {
	// Put a real file on PATH via a temp dir so GetInstallStatus finds it.
	// detectVersion would try to exec it; we must skip when CheckedAt is fresh.
	tmp := t.TempDir()
	binName := "maclaw_probe_tool"
	binPath := filepath.Join(tmp, binName)
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	// Not a real executable: if skip fails, detectVersion returns "" quickly
	// after exec error — but we assert CheckedAt is NOT refreshed (skip path).
	if err := os.WriteFile(binPath, []byte("not-a-real-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	c := &ToolVersionCache{}
	seededAt := time.Now().Add(-30 * time.Second)
	c.versions.Store(binName, &cachedVersion{
		Version:   "", // empty version still counts as a recent probe
		CheckedAt: seededAt,
		Installed: true,
		Path:      binPath,
	})

	c.refreshOne(context.Background(), binName)
	entry := c.GetCached(binName)
	if entry == nil {
		t.Fatal("expected cache entry")
	}
	// Skip must preserve the seed CheckedAt (not rewrite with time.Now()).
	if !entry.CheckedAt.Equal(seededAt) {
		t.Fatalf("expected skip to keep CheckedAt=%v, got %v (re-probed?)", seededAt, entry.CheckedAt)
	}
	if entry.Path != binPath {
		t.Fatalf("path = %q, want %q", entry.Path, binPath)
	}
}

func TestToolVersionCacheRefreshReprobesAfterTTL(t *testing.T) {
	tmp := t.TempDir()
	binName := "maclaw_probe_tool_ttl"
	binPath := filepath.Join(tmp, binName)
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	if err := os.WriteFile(binPath, []byte("not-a-real-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	c := &ToolVersionCache{}
	oldAt := time.Now().Add(-2 * toolVersionCacheFreshTTL)
	c.versions.Store(binName, &cachedVersion{
		Version:   "",
		CheckedAt: oldAt,
		Installed: true,
		Path:      binPath,
	})

	c.refreshOne(context.Background(), binName)
	entry := c.GetCached(binName)
	if entry == nil {
		t.Fatal("expected cache entry")
	}
	if !entry.CheckedAt.After(oldAt) {
		t.Fatalf("expected re-probe after TTL, CheckedAt still %v", entry.CheckedAt)
	}
}

func TestIsExternalCodingCLITool(t *testing.T) {
	if !isExternalCodingCLITool("claude") {
		t.Fatal("claude should be external coding CLI")
	}
	if !isExternalCodingCLITool("Claude") {
		t.Fatal("Claude should be external coding CLI")
	}
	if !isExternalCodingCLITool("") {
		t.Fatal("empty tool must be treated as external coding CLI (normalizes to claude)")
	}
	if isExternalCodingCLITool("browser") {
		t.Fatal("browser is not an external coding CLI process tool")
	}
	if isExternalCodingCLITool("ai-assistant") {
		t.Fatal("ai-assistant background sessions are not external CLIs")
	}
}

func TestSameToolBinaryPath(t *testing.T) {
	if !sameToolBinaryPath(`/tools/claude`, `/tools/claude`) {
		t.Fatal("identical paths should match")
	}
	if sameToolBinaryPath(``, `/tools/claude`) {
		t.Fatal("empty path should not match")
	}
	if runtime.GOOS == "windows" {
		if !sameToolBinaryPath(`C:\Tools\Claude.exe`, `c:\tools\claude.exe`) {
			t.Fatal("windows paths should match case-insensitively")
		}
	}
}
