package guiapp

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"testing"

	_ "modernc.org/sqlite"
)

func openSkillScanDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if testing.Testing() {
		db.SetMaxIdleConns(0)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS scan_cache (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  skill_name TEXT NOT NULL DEFAULT '',
  hash TEXT NOT NULL DEFAULT '',
  scanner_version TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT '',
  level TEXT NOT NULL DEFAULT '',
  summary TEXT NOT NULL DEFAULT '',
  scanned_by TEXT NOT NULL DEFAULT '',
  scanned_at TEXT NOT NULL DEFAULT '',
  signature TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS scan_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);`); err != nil {
		_ = db.Close()
		return nil, err
	}
	restrictSkillScanFiles(path)
	return db, nil
}

func readSkillScanCacheDB(dbPath, jsonPath string) (skillScanCacheRecord, error) {
	db, err := openSkillScanDB(dbPath)
	if err != nil {
		return skillScanCacheRecord{}, err
	}
	defer db.Close()
	defer restrictSkillScanFiles(dbPath)
	imported, rec, err := importSkillScanJSON(db, jsonPath)
	if imported {
		return rec, nil
	}
	stored, readErr := readSkillScanRow(db)
	if err != nil && errors.Is(readErr, os.ErrNotExist) {
		return skillScanCacheRecord{}, err
	}
	if readErr != nil {
		return skillScanCacheRecord{}, readErr
	}
	return stored, nil
}

func importSkillScanJSON(db *sql.DB, jsonPath string) (bool, skillScanCacheRecord, error) {
	var marker string
	err := db.QueryRow(`SELECT value FROM scan_meta WHERE key = 'legacy_import'`).Scan(&marker)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, skillScanCacheRecord{}, err
	}
	if marker == "1" {
		return false, skillScanCacheRecord{}, nil
	}
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		if os.IsNotExist(err) {
			if markErr := markSkillScanImported(db); markErr != nil {
				return false, skillScanCacheRecord{}, markErr
			}
			return false, skillScanCacheRecord{}, nil
		}
		return false, skillScanCacheRecord{}, nil
	}
	var rec skillScanCacheRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return false, skillScanCacheRecord{}, err
	}
	if err := writeSkillScanRow(db, rec); err != nil {
		return false, skillScanCacheRecord{}, err
	}
	_ = os.Remove(jsonPath)
	return true, rec, nil
}

func writeSkillScanCacheDB(dbPath string, rec skillScanCacheRecord) error {
	db, err := openSkillScanDB(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	defer restrictSkillScanFiles(dbPath)
	return writeSkillScanRow(db, rec)
}

// restrictSkillScanFiles matches the old JSON cache, which was written as 0600.
// SQLite also creates the WAL and shared-memory files beside the database.
func restrictSkillScanFiles(path string) {
	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Lstat(name)
		if err != nil || info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		_ = os.Chmod(name, 0o600)
	}
}

func writeSkillScanRow(db *sql.DB, rec skillScanCacheRecord) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO scan_cache(id, skill_name, hash, scanner_version, status, level, summary, scanned_by, scanned_at, signature)
		VALUES(1, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			skill_name = excluded.skill_name,
			hash = excluded.hash,
			scanner_version = excluded.scanner_version,
			status = excluded.status,
			level = excluded.level,
			summary = excluded.summary,
			scanned_by = excluded.scanned_by,
			scanned_at = excluded.scanned_at,
			signature = excluded.signature`,
		rec.SkillName, rec.Hash, rec.ScannerVersion, string(rec.Status), rec.Level, rec.Summary, rec.ScannedBy, rec.ScannedAt, rec.Signature); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`INSERT INTO scan_meta(key, value) VALUES('legacy_import','1') ON CONFLICT(key) DO UPDATE SET value = excluded.value`); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func markSkillScanImported(db *sql.DB) error {
	_, err := db.Exec(`INSERT INTO scan_meta(key, value) VALUES('legacy_import','1') ON CONFLICT(key) DO UPDATE SET value = excluded.value`)
	return err
}

func readSkillScanRow(db *sql.DB) (skillScanCacheRecord, error) {
	var rec skillScanCacheRecord
	var status string
	err := db.QueryRow(`SELECT skill_name, hash, scanner_version, status, level, summary, scanned_by, scanned_at, signature FROM scan_cache WHERE id = 1`).Scan(
		&rec.SkillName, &rec.Hash, &rec.ScannerVersion, &status, &rec.Level, &rec.Summary, &rec.ScannedBy, &rec.ScannedAt, &rec.Signature)
	if errors.Is(err, sql.ErrNoRows) {
		return skillScanCacheRecord{}, os.ErrNotExist
	}
	if err != nil {
		return skillScanCacheRecord{}, err
	}
	rec.Status = skillScanCacheStatus(status)
	return rec, nil
}
