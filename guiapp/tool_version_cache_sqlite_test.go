package guiapp

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestToolVersionCacheImportsLegacyJSON(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "tool_version_cache.json")
	now := time.Date(2026, 5, 1, 1, 2, 3, 0, time.UTC)
	raw, err := json.Marshal(persistedCache{Tools: map[string]*cachedVersion{
		"claude": {Version: "1.2.3", CheckedAt: now, Installed: true, Path: "/bin/claude"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jsonPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	cache := &ToolVersionCache{}
	cache.loadFromPath(jsonPath)
	got := cache.GetCached("claude")
	if got == nil || got.Version != "1.2.3" || !got.Installed || !got.CheckedAt.Equal(now) {
		t.Fatalf("imported = %+v", got)
	}
	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Fatalf("legacy json should be removed, stat err=%v", err)
	}
	reloaded := &ToolVersionCache{}
	reloaded.loadFromPath(jsonPath)
	if reloaded.GetCached("claude") == nil || reloaded.GetCached("claude").Version != "1.2.3" {
		t.Fatal("reload missed the sqlite row")
	}

	db, err := sql.Open("sqlite", toolVersionDBPath(jsonPath))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM tool_version_meta WHERE key = 'legacy_import'`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jsonPath, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	kept := &ToolVersionCache{}
	kept.loadFromPath(jsonPath)
	if kept.GetCached("claude") == nil || kept.GetCached("claude").Path != "/bin/claude" {
		t.Fatal("corrupt json replaced sqlite rows")
	}
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatal(err)
	}
}

func TestToolVersionSaveLeavesUnreadLegacyJSON(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "tool_version_cache.json")
	now := time.Date(2026, 5, 1, 1, 2, 3, 0, time.UTC)
	raw, err := json.Marshal(persistedCache{Tools: map[string]*cachedVersion{
		"claude": {Version: "1.2.3", CheckedAt: now, Installed: true, Path: "/bin/claude"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jsonPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	cache := &ToolVersionCache{}
	cache.versions.Store("other", &cachedVersion{Version: "9", Installed: true})
	cache.saveToPath(jsonPath)
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatalf("save removed unread legacy json: %v", err)
	}
	loaded := &ToolVersionCache{}
	loaded.loadFromPath(jsonPath)
	if got := loaded.GetCached("claude"); got == nil || got.Version != "1.2.3" {
		t.Fatalf("import = %+v", got)
	}
	if loaded.GetCached("other") != nil {
		t.Fatal("unread save outranked the legacy file")
	}
}
