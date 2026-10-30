package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

// fileKVStore implements corelib/skill.KVStore. Rows live in a sibling SQLite
// database. The JSON file is read once, and is written only when SQLite cannot
// be opened.
type fileKVStore struct {
	path string
	db   *sql.DB
	mu   sync.Mutex
	data map[string]string
}

const fileKVSchema = `
CREATE TABLE IF NOT EXISTS kv (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS kv_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);
`

func newFileKVStore(path string) *fileKVStore {
	s := &fileKVStore{path: path, data: make(map[string]string)}
	s.loadFromDisk()
	return s
}

func fileKVDBPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	ext := filepath.Ext(path)
	if strings.EqualFold(ext, ".json") {
		return strings.TrimSuffix(path, ext) + ".db"
	}
	return path + ".db"
}

func (s *fileKVStore) openDB() error {
	dbPath := fileKVDBPath(s.path)
	if dbPath == "" {
		return os.ErrInvalid
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	if testing.Testing() {
		db.SetMaxIdleConns(0)
	} else {
		db.SetMaxIdleConns(1)
	}
	if _, err := db.Exec(fileKVSchema); err != nil {
		_ = db.Close()
		return err
	}
	s.db = db
	return nil
}

func (s *fileKVStore) Get(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data[key], nil
}

func (s *fileKVStore) Set(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		if err := s.writeKeyLocked(key, value); err != nil {
			return err
		}
		if value == "" {
			delete(s.data, key)
		} else {
			s.data[key] = value
		}
		return nil
	}
	prev, had := s.data[key]
	if value == "" {
		delete(s.data, key)
	} else {
		s.data[key] = value
	}
	if err := s.saveJSONLocked(); err != nil {
		if had {
			s.data[key] = prev
		} else {
			delete(s.data, key)
		}
		return err
	}
	return nil
}

func (s *fileKVStore) Keys(prefix string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.data))
	for key := range s.data {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func (s *fileKVStore) loadFromDisk() {
	if err := s.openDB(); err != nil {
		s.loadJSONLocked()
		return
	}
	if s.legacyImported() {
		s.loadRowsLocked()
		return
	}
	parsed, found := s.loadJSONLocked()
	if !found {
		// No legacy file. The SQLite rows are the store; replacing them with
		// the empty map would delete every key.
		s.loadRowsLocked()
		_ = s.markLegacyImportedLocked()
		return
	}
	if !parsed {
		s.loadRowsLocked()
		return
	}
	if err := s.replaceAllLocked(); err != nil {
		return
	}
	// The keys are in SQLite. Leaving the file would replace them if the
	// import flag were lost.
	_ = os.Remove(s.path)
}

func (s *fileKVStore) legacyImported() bool {
	var value string
	err := s.db.QueryRow(`SELECT value FROM kv_meta WHERE key='legacy_import'`).Scan(&value)
	return err == nil && value == "1"
}

// loadJSONLocked reports parsed, found. A missing file is found=false. A read
// or parse failure is found with parsed=false, and the import stays unmarked.
func (s *fileKVStore) loadJSONLocked() (bool, bool) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return false, !os.IsNotExist(err)
	}
	parsed := map[string]string{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return false, true
	}
	if parsed == nil {
		// JSON null unmarshals as a nil map. It is not an empty object, and
		// replacing the table with it would delete every key.
		return false, true
	}
	s.data = parsed
	return true, true
}

func (s *fileKVStore) markLegacyImportedLocked() error {
	if s == nil || s.db == nil {
		return os.ErrInvalid
	}
	_, err := s.db.Exec(`INSERT INTO kv_meta(key, value) VALUES('legacy_import','1') ON CONFLICT(key) DO UPDATE SET value=excluded.value`)
	return err
}

func (s *fileKVStore) loadRowsLocked() {
	rows, err := s.db.Query(`SELECT key, value FROM kv`)
	if err != nil {
		return
	}
	defer rows.Close()
	loaded := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return
		}
		loaded[key] = value
	}
	if err := rows.Err(); err != nil {
		return
	}
	s.data = loaded
}

func (s *fileKVStore) replaceAllLocked() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM kv`); err != nil {
		return err
	}
	for key, value := range s.data {
		if _, err := tx.Exec(`INSERT INTO kv(key, value) VALUES(?,?)`, key, value); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO kv_meta(key, value) VALUES('legacy_import','1') ON CONFLICT(key) DO UPDATE SET value=excluded.value`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *fileKVStore) writeKeyLocked(key, value string) error {
	if value == "" {
		_, err := s.db.Exec(`DELETE FROM kv WHERE key=?`, key)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO kv(key, value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (s *fileKVStore) saveJSONLocked() error {
	blob, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.path, blob, 0o644)
}
