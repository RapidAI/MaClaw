package guiapp

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/fileutil"

	_ "modernc.org/sqlite"
)

func toolVersionDBPath(jsonPath string) string {
	if strings.EqualFold(filepath.Ext(jsonPath), ".json") {
		return strings.TrimSuffix(jsonPath, filepath.Ext(jsonPath)) + ".db"
	}
	return jsonPath + ".db"
}

func (c *ToolVersionCache) loadFromPath(jsonPath string) {
	if c == nil || strings.TrimSpace(jsonPath) == "" {
		return
	}
	dbPath := toolVersionDBPath(jsonPath)
	if !fileExists(dbPath) && !fileExists(jsonPath) {
		return
	}
	db, err := openToolVersionDB(dbPath)
	if err != nil {
		log.Printf("[ToolVersionCache] open %s: %v", dbPath, err)
		return
	}
	defer db.Close()
	if err := importToolVersionJSON(db, jsonPath); err != nil {
		log.Printf("[ToolVersionCache] import %s: %v", jsonPath, err)
	}
	entries, err := readToolVersions(db)
	if err != nil {
		log.Printf("[ToolVersionCache] read %s: %v", dbPath, err)
		return
	}
	for name, entry := range entries {
		if entry != nil {
			c.versions.Store(name, entry)
		}
	}
}

func (c *ToolVersionCache) saveToPath(jsonPath string) {
	if c == nil || strings.TrimSpace(jsonPath) == "" {
		return
	}
	tools := map[string]*cachedVersion{}
	c.versions.Range(func(key, value any) bool {
		name, _ := key.(string)
		entry, _ := value.(*cachedVersion)
		if name != "" && entry != nil {
			copied := *entry
			tools[name] = &copied
		}
		return true
	})
	db, err := openToolVersionDB(toolVersionDBPath(jsonPath))
	if err != nil {
		log.Printf("[ToolVersionCache] open cache: %v", err)
		return
	}
	defer db.Close()
	seal := toolVersionLegacySeal(db, jsonPath)
	if err := writeToolVersions(db, tools, seal); err != nil {
		log.Printf("[ToolVersionCache] save cache: %v", err)
		return
	}
	if seal {
		_ = os.Remove(jsonPath)
		_ = os.Remove(jsonPath + ".tmp")
	}
}

// toolVersionLegacySeal reports whether this save may record the import and
// remove the pre-migration file. A readable file that has not been copied
// stays, so the next open can still import it.
func toolVersionLegacySeal(db *sql.DB, jsonPath string) bool {
	var marker string
	err := db.QueryRow(`SELECT value FROM tool_version_meta WHERE key = 'legacy_import'`).Scan(&marker)
	if err == nil && marker == "1" {
		return true
	}
	return !toolVersionLegacyPending(jsonPath)
}

func toolVersionLegacyPending(path string) bool {
	return fileutil.LegacyImportPending(path, func(data []byte) bool {
		_, err := unmarshalToolVersionCache(data)
		return err == nil
	})
}

func unmarshalToolVersionCache(data []byte) (persistedCache, error) {
	var pc persistedCache
	err := json.Unmarshal(data, &pc)
	return pc, err
}

func openToolVersionDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if testing.Testing() {
		db.SetMaxIdleConns(0)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS tool_versions (
  name TEXT PRIMARY KEY,
  version TEXT NOT NULL DEFAULT '',
  checked_at TEXT NOT NULL DEFAULT '',
  installed INTEGER NOT NULL DEFAULT 0,
  path TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS tool_version_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func importToolVersionJSON(db *sql.DB, jsonPath string) error {
	var marker string
	err := db.QueryRow(`SELECT value FROM tool_version_meta WHERE key = 'legacy_import'`).Scan(&marker)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if marker == "1" {
		return nil
	}
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		if os.IsNotExist(err) {
			_, err = db.Exec(`INSERT INTO tool_version_meta(key, value) VALUES('legacy_import','1') ON CONFLICT(key) DO UPDATE SET value = excluded.value`)
			return err
		}
		return nil
	}
	pc, err := unmarshalToolVersionCache(data)
	if err != nil {
		log.Printf("[ToolVersionCache] failed to parse cache file %s: %v", jsonPath, err)
		return nil
	}
	if err := writeToolVersions(db, pc.Tools, true); err != nil {
		return err
	}
	_ = os.Remove(jsonPath)
	_ = os.Remove(jsonPath + ".tmp")
	return nil
}

func writeToolVersions(db *sql.DB, tools map[string]*cachedVersion, mark bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM tool_versions`); err != nil {
		_ = tx.Rollback()
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO tool_versions(name, version, checked_at, installed, path) VALUES(?,?,?,?,?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for name, entry := range tools {
		if strings.TrimSpace(name) == "" || entry == nil {
			continue
		}
		installed := 0
		if entry.Installed {
			installed = 1
		}
		checked := ""
		if !entry.CheckedAt.IsZero() {
			checked = entry.CheckedAt.UTC().Format(time.RFC3339Nano)
		}
		if _, err := stmt.Exec(name, entry.Version, checked, installed, entry.Path); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if mark {
		if _, err := tx.Exec(`INSERT INTO tool_version_meta(key, value) VALUES('legacy_import','1') ON CONFLICT(key) DO UPDATE SET value = excluded.value`); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func readToolVersions(db *sql.DB) (map[string]*cachedVersion, error) {
	rows, err := db.Query(`SELECT name, version, checked_at, installed, path FROM tool_versions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*cachedVersion{}
	for rows.Next() {
		var name, version, checked, path string
		var installed int
		if err := rows.Scan(&name, &version, &checked, &installed, &path); err != nil {
			return nil, err
		}
		entry := &cachedVersion{Version: version, Installed: installed != 0, Path: path}
		if checked != "" {
			if parsed, err := time.Parse(time.RFC3339Nano, checked); err == nil {
				entry.CheckedAt = parsed
			} else if parsed, err := time.Parse(time.RFC3339, checked); err == nil {
				entry.CheckedAt = parsed
			}
		}
		out[name] = entry
	}
	return out, rows.Err()
}
