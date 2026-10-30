package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/fileutil"

	_ "modernc.org/sqlite"
)

func multibotDBPath(jsonPath string) string {
	if strings.EqualFold(filepath.Ext(jsonPath), ".json") {
		return strings.TrimSuffix(jsonPath, filepath.Ext(jsonPath)) + ".db"
	}
	return jsonPath + ".db"
}

func readMultibotState() multibotState {
	st := multibotState{Runs: map[string]multibotRunCount{}, Seen: map[string]string{}}
	jsonPath := multibotStatePath()
	dbPath := multibotDBPath(jsonPath)
	if !multibotFileExists(dbPath) && !multibotFileExists(jsonPath) {
		return st
	}
	db, err := openMultibotDB(dbPath)
	if err != nil {
		return st
	}
	defer db.Close()
	_ = importMultibotJSON(db, jsonPath)
	loaded, err := readMultibotRows(db)
	if err != nil {
		return st
	}
	return loaded
}

func writeMultibotState(st multibotState) error {
	jsonPath := multibotStatePath()
	db, err := openMultibotDB(multibotDBPath(jsonPath))
	if err != nil {
		return err
	}
	defer db.Close()
	seal := multibotLegacySeal(db, jsonPath)
	if err := writeMultibotRows(db, st, seal); err != nil {
		return err
	}
	if seal {
		_ = os.Remove(jsonPath)
		_ = os.Remove(jsonPath + ".tmp")
	}
	return nil
}

// multibotLegacySeal reports whether this save may record the import and
// remove the pre-migration file. A readable file that has not been copied
// stays, so the next open can still import it.
func multibotLegacySeal(db *sql.DB, jsonPath string) bool {
	var marker string
	err := db.QueryRow(`SELECT value FROM multibot_meta WHERE key = 'legacy_import'`).Scan(&marker)
	if err == nil && marker == "1" {
		return true
	}
	return !multibotLegacyPending(jsonPath)
}

func multibotLegacyPending(path string) bool {
	return fileutil.LegacyImportPending(path, func(data []byte) bool {
		_, err := unmarshalMultibotState(data)
		return err == nil
	})
}

func unmarshalMultibotState(data []byte) (multibotState, error) {
	var st multibotState
	err := json.Unmarshal(data, &st)
	return st, err
}

func openMultibotDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
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
CREATE TABLE IF NOT EXISTS multibot_runs (
  run_key TEXT PRIMARY KEY,
  n INTEGER NOT NULL,
  at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS multibot_seen (
  seen_key TEXT PRIMARY KEY,
  at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS multibot_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func importMultibotJSON(db *sql.DB, jsonPath string) error {
	var marker string
	err := db.QueryRow(`SELECT value FROM multibot_meta WHERE key = 'legacy_import'`).Scan(&marker)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if marker == "1" {
		return nil
	}
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		if os.IsNotExist(err) {
			_, err = db.Exec(`INSERT INTO multibot_meta(key, value) VALUES('legacy_import','1') ON CONFLICT(key) DO UPDATE SET value = excluded.value`)
			return err
		}
		return nil
	}
	st, err := unmarshalMultibotState(data)
	if err != nil {
		return nil
	}
	if err := writeMultibotRows(db, st, true); err != nil {
		return err
	}
	_ = os.Remove(jsonPath)
	_ = os.Remove(jsonPath + ".tmp")
	return nil
}

func writeMultibotRows(db *sql.DB, st multibotState, mark bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM multibot_runs`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`DELETE FROM multibot_seen`); err != nil {
		_ = tx.Rollback()
		return err
	}
	runStmt, err := tx.Prepare(`INSERT INTO multibot_runs(run_key, n, at) VALUES(?,?,?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer runStmt.Close()
	for key, run := range st.Runs {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if _, err := runStmt.Exec(key, run.N, run.At); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	seenStmt, err := tx.Prepare(`INSERT INTO multibot_seen(seen_key, at) VALUES(?,?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer seenStmt.Close()
	for key, at := range st.Seen {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if _, err := seenStmt.Exec(key, at); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if mark {
		if _, err := tx.Exec(`INSERT INTO multibot_meta(key, value) VALUES('legacy_import','1') ON CONFLICT(key) DO UPDATE SET value = excluded.value`); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func readMultibotRows(db *sql.DB) (multibotState, error) {
	st := multibotState{Runs: map[string]multibotRunCount{}, Seen: map[string]string{}}
	runs, err := db.Query(`SELECT run_key, n, at FROM multibot_runs`)
	if err != nil {
		return st, err
	}
	for runs.Next() {
		var key, at string
		var n int
		if err := runs.Scan(&key, &n, &at); err != nil {
			_ = runs.Close()
			return st, err
		}
		st.Runs[key] = multibotRunCount{N: n, At: at}
	}
	if err := runs.Err(); err != nil {
		_ = runs.Close()
		return st, err
	}
	_ = runs.Close()
	seen, err := db.Query(`SELECT seen_key, at FROM multibot_seen`)
	if err != nil {
		return st, err
	}
	defer seen.Close()
	for seen.Next() {
		var key, at string
		if err := seen.Scan(&key, &at); err != nil {
			return st, err
		}
		st.Seen[key] = at
	}
	return st, seen.Err()
}

func multibotFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
