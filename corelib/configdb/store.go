package configdb

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	DatabaseFileName = "config.db"
	JSONFileName     = "config.json"
)

func DatabasePath(dataDir string) string {
	return filepath.Join(dataDir, DatabaseFileName)
}

func JSONPath(dataDir string) string {
	return filepath.Join(dataDir, JSONFileName)
}

func LoadEntries(path string) (map[string]json.RawMessage, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	db, err := open(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT key, value_json FROM config_entries`)
	if err != nil {
		return nil, fmt.Errorf("read config entries: %w", err)
	}
	defer rows.Close()
	entries := make(map[string]json.RawMessage)
	for rows.Next() {
		var key string
		var value []byte
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("scan config entry: %w", err)
		}
		entries[key] = append(json.RawMessage(nil), value...)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate config entries: %w", err)
	}
	return entries, nil
}

func SaveEntries(path string, entries map[string]json.RawMessage) error {
	return saveEntries(path, entries, nil, false)
}

func ReplaceEntries(path string, entries map[string]json.RawMessage) error {
	return saveEntries(path, entries, nil, true)
}

// ReplaceManagedEntries upserts entries and deletes only managed keys that are
// absent from the new set. Keys outside managed, such as permission rules read
// without the config manager, stay in place.
func ReplaceManagedEntries(path string, entries map[string]json.RawMessage, managed []string) error {
	return saveEntries(path, entries, managed, false)
}

func saveEntries(path string, entries map[string]json.RawMessage, dropMissing []string, replace bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir config dir: %w", err)
	}
	type change struct {
		key, value, hash string
	}
	pending := make([]change, 0, len(entries))
	for key, value := range entries {
		if !json.Valid(value) {
			return fmt.Errorf("invalid JSON for config key %q", key)
		}
		text := string(value)
		pending = append(pending, change{key, text, configValueHash(text)})
	}
	db, err := open(path)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("config connection: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return fmt.Errorf("begin config transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, `ROLLBACK`)
		}
	}()

	existing := make(map[string]string)
	rows, err := conn.QueryContext(ctx, `SELECT key, value_hash FROM config_entries`)
	if err != nil {
		return fmt.Errorf("read config entries: %w", err)
	}
	for rows.Next() {
		var key, hash string
		if err := rows.Scan(&key, &hash); err != nil {
			rows.Close()
			return fmt.Errorf("scan config entry: %w", err)
		}
		existing[key] = hash
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate config entries: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close config entries: %w", err)
	}

	keep := make(map[string]struct{}, len(pending))
	for _, item := range pending {
		keep[item.key] = struct{}{}
	}
	var deleteKeys []string
	if replace {
		for key := range existing {
			if _, ok := keep[key]; !ok {
				deleteKeys = append(deleteKeys, key)
			}
		}
	} else if len(dropMissing) > 0 {
		managed := make(map[string]struct{}, len(dropMissing))
		for _, key := range dropMissing {
			managed[key] = struct{}{}
		}
		for key := range existing {
			if _, ok := managed[key]; !ok {
				continue
			}
			if _, ok := keep[key]; ok {
				continue
			}
			deleteKeys = append(deleteKeys, key)
		}
	}
	var upserts []change
	for _, item := range pending {
		if hash, ok := existing[item.key]; ok && hash == item.hash {
			continue
		}
		upserts = append(upserts, item)
	}
	if len(deleteKeys) == 0 && len(upserts) == 0 {
		if _, err := conn.ExecContext(ctx, `ROLLBACK`); err != nil {
			return fmt.Errorf("rollback unchanged config: %w", err)
		}
		committed = true
		return nil
	}

	if len(deleteKeys) > 0 {
		del, err := conn.PrepareContext(ctx, `DELETE FROM config_entries WHERE key=?`)
		if err != nil {
			return fmt.Errorf("prepare config delete: %w", err)
		}
		for _, key := range deleteKeys {
			if _, err := del.ExecContext(ctx, key); err != nil {
				del.Close()
				return fmt.Errorf("delete config key %q: %w", key, err)
			}
		}
		if err := del.Close(); err != nil {
			return fmt.Errorf("close config delete: %w", err)
		}
	}
	if len(upserts) > 0 {
		stmt, err := conn.PrepareContext(ctx, `INSERT INTO config_entries(key, value_json, updated_at, value_hash) VALUES(?, ?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json, updated_at=excluded.updated_at, value_hash=excluded.value_hash`)
		if err != nil {
			return fmt.Errorf("prepare config upsert: %w", err)
		}
		now := timeNowUnixNano()
		for _, item := range upserts {
			if _, err := stmt.ExecContext(ctx, item.key, item.value, now, item.hash); err != nil {
				stmt.Close()
				return fmt.Errorf("upsert config key %q: %w", item.key, err)
			}
		}
		if err := stmt.Close(); err != nil {
			return fmt.Errorf("close config upsert: %w", err)
		}
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return fmt.Errorf("commit config transaction: %w", err)
	}
	committed = true
	return nil
}

func LoadJSONDocument(jsonPath string) ([]byte, bool, error) {
	entries, dbErr := LoadEntries(filepath.Join(filepath.Dir(jsonPath), DatabaseFileName))
	if dbErr == nil && len(entries) > 0 {
		data, err := json.Marshal(entries)
		return data, true, err
	}
	if fileExists(jsonPath) {
		data, err := os.ReadFile(jsonPath)
		if err == nil && len(bytes.TrimSpace(data)) == 0 {
			return nil, false, nil
		}
		return data, true, err
	}
	if dbErr != nil && !errors.Is(dbErr, os.ErrNotExist) {
		return nil, false, dbErr
	}
	return nil, false, nil
}

// ReadValue decodes one top-level config field. A populated config.db is
// queried by primary key; the legacy JSON file is parsed only when the
// database is missing or still empty. Read errors report as a missing value.
func ReadValue(dataDir, key string, out any) bool {
	raw, ok, err := ReadRaw(dataDir, key)
	return err == nil && ok && json.Unmarshal(raw, out) == nil
}

// ReadRaw returns one stored JSON value. ok is false when the key is absent.
// A populated database is not parsed as a whole document.
func ReadRaw(dataDir, key string) (json.RawMessage, bool, error) {
	return lookupConfigValue(DatabasePath(dataDir), JSONPath(dataDir), key)
}

func lookupConfigValue(dbPath, jsonPath, key string) (json.RawMessage, bool, error) {
	info, statErr := os.Stat(dbPath)
	if statErr == nil && !info.IsDir() {
		db, err := open(dbPath)
		if err != nil {
			return lookupJSONValue(jsonPath, key, err)
		}
		defer db.Close()
		var value []byte
		err = db.QueryRow(`SELECT value_json FROM config_entries WHERE key=?`, key).Scan(&value)
		if err == nil {
			return append(json.RawMessage(nil), value...), true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, false, err
		}
		var present int
		err = db.QueryRow(`SELECT 1 FROM config_entries LIMIT 1`).Scan(&present)
		if err == nil {
			return nil, false, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, false, err
		}
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, false, statErr
	}
	return lookupJSONValue(jsonPath, key, nil)
}

func lookupJSONValue(jsonPath, key string, dbErr error) (json.RawMessage, bool, error) {
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, dbErr
		}
		return nil, false, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, false, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, false, err
	}
	value, ok := fields[key]
	return value, ok, nil
}

const configUserVersion = 2

var configSchemaMu sync.Mutex

func open(path string) (*sql.DB, error) {
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=synchronous(FULL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open config db: %w", err)
	}
	db.SetMaxOpenConns(1)
	if configSchemaCurrent(db) {
		return db, nil
	}
	configSchemaMu.Lock()
	defer configSchemaMu.Unlock()
	if configSchemaCurrent(db) {
		return db, nil
	}
	if err := initializeConfigSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, configUserVersion)); err != nil {
		db.Close()
		return nil, fmt.Errorf("mark config schema version: %w", err)
	}
	return db, nil
}

func configSchemaCurrent(db *sql.DB) bool {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return false
	}
	return version >= configUserVersion
}

func initializeConfigSchema(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS config_entries (
		key TEXT PRIMARY KEY,
		value_hash TEXT NOT NULL DEFAULT '',
		updated_at INTEGER NOT NULL,
		value_json TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("initialize config db: %w", err)
	}
	if _, err := db.Exec(`ALTER TABLE config_entries ADD COLUMN value_hash TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return fmt.Errorf("upgrade config db: %w", err)
	}
	return ensureConfigHashBeforePayload(db)
}

// ensureConfigHashBeforePayload moves value_hash ahead of value_json.
// SQLite stores a row as one record and puts the tail on overflow pages, so a
// hash that follows a large JSON value is only reachable by walking those pages.
func ensureConfigHashBeforePayload(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(config_entries)`)
	if err != nil {
		return fmt.Errorf("inspect config schema: %w", err)
	}
	hashCID, jsonCID := -1, -1
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return fmt.Errorf("scan config schema: %w", err)
		}
		switch name {
		case "value_hash":
			hashCID = cid
		case "value_json":
			jsonCID = cid
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate config schema: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close config schema: %w", err)
	}
	if hashCID >= 0 && jsonCID >= 0 && hashCID < jsonCID {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin config schema rebuild: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TABLE config_entries_v2 (
		key TEXT PRIMARY KEY,
		value_hash TEXT NOT NULL DEFAULT '',
		updated_at INTEGER NOT NULL,
		value_json TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create config schema v2: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO config_entries_v2(key, value_hash, updated_at, value_json)
		SELECT key, COALESCE(value_hash, ''), updated_at, value_json FROM config_entries`); err != nil {
		return fmt.Errorf("copy config entries: %w", err)
	}
	if _, err := tx.Exec(`DROP TABLE config_entries`); err != nil {
		return fmt.Errorf("drop old config entries: %w", err)
	}
	if _, err := tx.Exec(`ALTER TABLE config_entries_v2 RENAME TO config_entries`); err != nil {
		return fmt.Errorf("rename config entries: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit config schema rebuild: %w", err)
	}
	return nil
}

func configValueHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

var timeNowUnixNano = func() int64 {
	return time.Now().UnixNano()
}
