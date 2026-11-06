package guiapp

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

// errBrowserExtensionLegacyPending means the pre-migration JSON is still a
// document this store can import. A hello or install must not publish rows
// that the later import would replace.
var errBrowserExtensionLegacyPending = errors.New("browser extension legacy state is still unread")

func browserExtensionStatePaths(dir string) (jsonPath, dbPath string) {
	return filepath.Join(dir, "browser-extension-state.json"), filepath.Join(dir, "browser-extension-state.db")
}

func loadBrowserExtensionState(dir string) (browserExtensionStateFile, error) {
	jsonPath, dbPath := browserExtensionStatePaths(dir)
	if !fileExists(dbPath) && !fileExists(jsonPath) {
		return browserExtensionStateFile{}, nil
	}
	db, err := openBrowserExtensionDB(dbPath)
	if err != nil {
		return browserExtensionStateFile{}, err
	}
	defer db.Close()
	if err := importBrowserExtensionJSON(db, jsonPath); err != nil {
		return browserExtensionStateFile{}, err
	}
	state, err := readBrowserExtensionRows(db)
	if err != nil {
		return browserExtensionStateFile{}, err
	}
	imported, err := browserExtensionLegacyImported(db)
	if err != nil {
		return browserExtensionStateFile{}, err
	}
	// A file left behind after a successful import is stale. It must not block
	// hello, and it must not be imported over the rows already stored.
	if !imported && browserExtensionLegacyPending(jsonPath) {
		return state, errBrowserExtensionLegacyPending
	}
	return state, nil
}

// updateBrowserExtensionState reads, edits, and writes under the state lock.
// A failed read is not an empty store: the edit is not written.
func updateBrowserExtensionState(dir string, fn func(*browserExtensionStateFile) error) error {
	if strings.TrimSpace(dir) == "" {
		return os.ErrInvalid
	}
	return withBrowserExtensionStateLock(dir, func() error {
		state, err := loadBrowserExtensionState(dir)
		if err != nil {
			return err
		}
		if err := fn(&state); err != nil {
			return err
		}
		return saveBrowserExtensionState(dir, state, false)
	})
}

func saveBrowserExtensionState(dir string, state browserExtensionStateFile, forceSeal bool) error {
	if strings.TrimSpace(dir) == "" {
		return os.ErrInvalid
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	jsonPath, dbPath := browserExtensionStatePaths(dir)
	db, err := openBrowserExtensionDB(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	seal := forceSeal || browserExtensionLegacySeal(db, jsonPath)
	if !seal {
		return errBrowserExtensionLegacyPending
	}
	if err := writeBrowserExtensionRows(db, state, true); err != nil {
		return err
	}
	if err := removeBrowserExtensionLegacyFiles(jsonPath); err != nil && forceSeal {
		return err
	}
	chmodBrowserExtensionDB(dbPath)
	return nil
}

// clearBrowserExtensionState forgets chrome and edge rows. A readable legacy
// JSON file is removed in the same step so the next open cannot restore it.
func clearBrowserExtensionState(dir string) error {
	return saveBrowserExtensionState(dir, browserExtensionStateFile{}, true)
}

func browserExtensionLegacySeal(db *sql.DB, jsonPath string) bool {
	imported, err := browserExtensionLegacyImported(db)
	if err == nil && imported {
		return true
	}
	return !browserExtensionLegacyPending(jsonPath)
}

func browserExtensionLegacyImported(db *sql.DB) (bool, error) {
	var marker string
	err := db.QueryRow(`SELECT value FROM browser_extension_meta WHERE key = 'legacy_import'`).Scan(&marker)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return marker == "1", nil
}

func browserExtensionLegacyPending(path string) bool {
	return fileutil.LegacyImportPending(path, func(data []byte) bool {
		_, err := unmarshalBrowserExtensionState(data)
		return err == nil
	})
}

func unmarshalBrowserExtensionState(data []byte) (browserExtensionStateFile, error) {
	var state browserExtensionStateFile
	err := json.Unmarshal(data, &state)
	return state, err
}

func openBrowserExtensionDB(path string) (*sql.DB, error) {
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
CREATE TABLE IF NOT EXISTS browser_extension_browsers (
  browser TEXT PRIMARY KEY,
  version TEXT NOT NULL DEFAULT '',
  hello_at TEXT NOT NULL DEFAULT '',
  registered_at TEXT NOT NULL DEFAULT '',
  running_at_register INTEGER NOT NULL DEFAULT 0,
  install_mode TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS browser_extension_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureBrowserExtensionInstallMode(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	chmodBrowserExtensionDB(path)
	return db, nil
}

// ensureBrowserExtensionInstallMode adds install_mode to a database created
// before unpacked and store registrations were recorded.
func ensureBrowserExtensionInstallMode(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(browser_extension_browsers)`)
	if err != nil {
		return err
	}
	hasMode := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			_ = rows.Close()
			return err
		}
		if name == "install_mode" {
			hasMode = true
		}
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	if hasMode {
		return nil
	}
	_, err = db.Exec(`ALTER TABLE browser_extension_browsers ADD COLUMN install_mode TEXT NOT NULL DEFAULT ''`)
	return err
}

func importBrowserExtensionJSON(db *sql.DB, jsonPath string) error {
	var marker string
	err := db.QueryRow(`SELECT value FROM browser_extension_meta WHERE key = 'legacy_import'`).Scan(&marker)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if marker == "1" {
		return nil
	}
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		if os.IsNotExist(err) {
			return markBrowserExtensionImported(db)
		}
		return nil
	}
	state, err := unmarshalBrowserExtensionState(data)
	if err != nil {
		return nil
	}
	if err := writeBrowserExtensionRows(db, state, true); err != nil {
		return err
	}
	_ = removeBrowserExtensionLegacyFiles(jsonPath)
	return nil
}

func markBrowserExtensionImported(db *sql.DB) error {
	_, err := db.Exec(`INSERT INTO browser_extension_meta(key, value) VALUES('legacy_import','1') ON CONFLICT(key) DO UPDATE SET value = excluded.value`)
	return err
}

func writeBrowserExtensionRows(db *sql.DB, state browserExtensionStateFile, mark bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM browser_extension_browsers`); err != nil {
		_ = tx.Rollback()
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO browser_extension_browsers(browser, version, hello_at, registered_at, running_at_register, install_mode) VALUES(?,?,?,?,?,?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, browser := range []string{"chrome", "edge"} {
		rec := state.record(browser)
		if rec == nil {
			continue
		}
		running := 0
		if rec.BrowserRunningAtRegister {
			running = 1
		}
		if _, err := stmt.Exec(browser, rec.Version, rec.HelloAt, rec.RegisteredAt, running, rec.InstallMode); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if mark {
		if _, err := tx.Exec(`INSERT INTO browser_extension_meta(key, value) VALUES('legacy_import','1') ON CONFLICT(key) DO UPDATE SET value = excluded.value`); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func readBrowserExtensionRows(db *sql.DB) (browserExtensionStateFile, error) {
	rows, err := db.Query(`SELECT browser, version, hello_at, registered_at, running_at_register, install_mode FROM browser_extension_browsers`)
	if err != nil {
		return browserExtensionStateFile{}, err
	}
	defer rows.Close()
	var state browserExtensionStateFile
	for rows.Next() {
		var browser, version, helloAt, registeredAt, installMode string
		var running int
		if err := rows.Scan(&browser, &version, &helloAt, &registeredAt, &running, &installMode); err != nil {
			return browserExtensionStateFile{}, err
		}
		state.setRecord(browser, &browserExtensionBrowserRecord{
			Version:                  version,
			HelloAt:                  helloAt,
			RegisteredAt:             registeredAt,
			BrowserRunningAtRegister: running != 0,
			InstallMode:              installMode,
		})
	}
	if err := rows.Err(); err != nil {
		return browserExtensionStateFile{}, err
	}
	return state, nil
}

func removeBrowserExtensionLegacyFiles(jsonPath string) error {
	err := removeBrowserExtensionFile(jsonPath)
	_ = removeBrowserExtensionFile(jsonPath + ".tmp")
	matches, globErr := filepath.Glob(filepath.Join(filepath.Dir(jsonPath), "browser-extension-state.*.tmp"))
	if globErr == nil {
		for _, match := range matches {
			_ = removeBrowserExtensionFile(match)
		}
	}
	return err
}

// removeBrowserExtensionFile clears the read-only bit before delete. On
// Windows a read-only file otherwise returns access denied, and a failed
// delete used to leave hello stuck behind that file.
func removeBrowserExtensionFile(path string) error {
	_ = os.Chmod(path, 0o600)
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func chmodBrowserExtensionDB(path string) {
	_ = os.Chmod(path, 0o600)
	_ = os.Chmod(path+"-wal", 0o600)
	_ = os.Chmod(path+"-shm", 0o600)
}
