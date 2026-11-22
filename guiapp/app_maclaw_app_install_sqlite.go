package guiapp

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

type maclawAppInstallDatabase struct {
	db       *sql.DB
	path     string
	imported bool
}

var maclawAppInstallDBMu sync.Mutex

func (a *App) maclawAppInstallDBPath() string {
	return filepath.Join(a.GetDataDir(), "app_install_records.db")
}

func (a *App) maclawAppInstallDBOpen() (*sql.DB, error) {
	maclawAppInstallDBMu.Lock()
	defer maclawAppInstallDBMu.Unlock()
	if a == nil {
		return nil, os.ErrInvalid
	}
	path := a.maclawAppInstallDBPath()
	if a.maclawAppInstallDB != nil && a.maclawAppInstallDB.path != path {
		_ = a.maclawAppInstallDB.db.Close()
		a.maclawAppInstallDB = nil
	}
	if a.maclawAppInstallDB == nil {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)")
		if err != nil {
			return nil, err
		}
		db.SetMaxOpenConns(1)
		if testing.Testing() {
			db.SetMaxIdleConns(0)
		} else {
			db.SetMaxIdleConns(1)
		}
		if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS app_install_records (
    position INTEGER PRIMARY KEY,
    app_id TEXT NOT NULL UNIQUE,
    signature TEXT NOT NULL,
    app_name TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT '',
    installed_at TEXT NOT NULL DEFAULT '',
    package_sha256 TEXT NOT NULL DEFAULT '',
    package_bytes INTEGER NOT NULL DEFAULT 0,
    has_missing_required INTEGER NOT NULL DEFAULT 0,
    has_blocking_dependency INTEGER NOT NULL DEFAULT 0,
    message TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_app_install_records_source ON app_install_records(source);
CREATE TABLE IF NOT EXISTS app_install_fields (
    app_id TEXT NOT NULL,
    field_path TEXT NOT NULL,
    value_type TEXT NOT NULL,
    value_text TEXT,
    value_number REAL,
    value_bool INTEGER,
    PRIMARY KEY(app_id, field_path),
    FOREIGN KEY(app_id) REFERENCES app_install_records(app_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_app_install_fields_path ON app_install_fields(field_path);
CREATE TABLE IF NOT EXISTS app_install_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`); err != nil {
			_ = db.Close()
			return nil, err
		}
		a.maclawAppInstallDB = &maclawAppInstallDatabase{db: db, path: path}
	}
	store := a.maclawAppInstallDB
	if !store.imported {
		if err := a.importLegacyMaclawAppInstallRegistry(store.db); err != nil {
			return nil, err
		}
		store.imported = true
	}
	return store.db, nil
}

func (a *App) closeMaclawAppInstallDB() {
	maclawAppInstallDBMu.Lock()
	defer maclawAppInstallDBMu.Unlock()
	a.closeMaclawAppInstallDBLocked()
}

// closeMaclawAppInstallDBLocked assumes the caller already holds
// maclawAppInstallDBMu.
func (a *App) closeMaclawAppInstallDBLocked() {
	if a == nil || a.maclawAppInstallDB == nil {
		return
	}
	_ = a.maclawAppInstallDB.db.Close()
	a.maclawAppInstallDB = nil
}

func (a *App) importLegacyMaclawAppInstallRegistry(db *sql.DB) error {
	var imported int
	if err := db.QueryRow(`SELECT COUNT(*) FROM app_install_meta WHERE key='legacy_json_imported'`).Scan(&imported); err != nil {
		return err
	}
	if imported > 0 {
		return nil
	}
	registry := maclawAppInstallRegistry{Schema: "maclaw.app.installs.v1", Installs: []maclawAppInstallRecord{}}
	data, err := os.ReadFile(a.maclawAppInstallRegistryPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &registry); err != nil {
			return fmt.Errorf("decode maclaw app install registry: %w", err)
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := writeMaclawAppInstallRegistryTx(tx, registry); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO app_install_meta(key, value) VALUES('legacy_json_imported', '1')`); err != nil {
		return err
	}
	return tx.Commit()
}

func maclawAppInstallRecordSignature(record maclawAppInstallRecord) (string, error) {
	raw, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}

var maclawAppInstallMainFields = map[string]bool{
	"app_id": true, "app_name": true, "kind": true, "source": true, "installed_at": true,
	"package_sha256": true, "package_bytes": true, "has_missing_required": true,
	"has_blocking_dependency": true, "message": true,
}

func maclawAppInstallRecordMap(record maclawAppInstallRecord) (map[string]interface{}, error) {
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	var value map[string]interface{}
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func saveMaclawAppInstallRegistry(db *sql.DB, registry maclawAppInstallRegistry) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := writeMaclawAppInstallRegistryTx(tx, registry); err != nil {
		return err
	}
	return tx.Commit()
}

func writeMaclawAppInstallRegistryTx(tx *sql.Tx, registry maclawAppInstallRegistry) error {
	type storedSignature struct {
		signature string
		position  int
	}
	existing := map[string]storedSignature{}
	rows, err := tx.Query(`SELECT app_id, signature, position FROM app_install_records`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, signature string
		var position int
		if err := rows.Scan(&id, &signature, &position); err != nil {
			_ = rows.Close()
			return err
		}
		existing[id] = storedSignature{signature: signature, position: position}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	reordered := false
	for position, record := range registry.Installs {
		if stored, ok := existing[record.AppID]; ok && stored.position != position {
			reordered = true
			break
		}
	}
	if reordered {
		if _, err := tx.Exec(`UPDATE app_install_records SET position=-position-1`); err != nil {
			return err
		}
	}
	kept := map[string]struct{}{}
	for position, record := range registry.Installs {
		if strings.TrimSpace(record.AppID) == "" {
			continue
		}
		signature, err := maclawAppInstallRecordSignature(record)
		if err != nil {
			return err
		}
		kept[record.AppID] = struct{}{}
		stored := existing[record.AppID]
		if stored.signature == signature {
			if reordered {
				if _, err := tx.Exec(`UPDATE app_install_records SET position=? WHERE app_id=?`, position, record.AppID); err != nil {
					return err
				}
				continue
			}
			if stored.position == position {
				continue
			}
		}
		if err := writeMaclawAppInstallRecord(tx, record, position, signature); err != nil {
			return err
		}
	}
	staleRows, err := tx.Query(`SELECT app_id FROM app_install_records`)
	if err != nil {
		return err
	}
	stale := []string{}
	for staleRows.Next() {
		var id string
		if err := staleRows.Scan(&id); err != nil {
			_ = staleRows.Close()
			return err
		}
		if _, ok := kept[id]; !ok {
			stale = append(stale, id)
		}
	}
	if err := staleRows.Err(); err != nil {
		_ = staleRows.Close()
		return err
	}
	_ = staleRows.Close()
	for _, id := range stale {
		if _, err := tx.Exec(`DELETE FROM app_install_records WHERE app_id=?`, id); err != nil {
			return err
		}
	}
	schema := registry.Schema
	if schema == "" {
		schema = "maclaw.app.installs.v1"
	}
	for key, value := range map[string]string{"schema": schema, "updated_at": registry.UpdatedAt} {
		if _, err := tx.Exec(`INSERT INTO app_install_meta(key, value) VALUES(?,?)
			ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value); err != nil {
			return err
		}
	}
	return nil
}

func writeMaclawAppInstallRecord(tx *sql.Tx, record maclawAppInstallRecord, position int, signature string) error {
	missing, blocking := 0, 0
	if record.HasMissingRequired {
		missing = 1
	}
	if record.HasBlockingDependency {
		blocking = 1
	}
	if _, err := tx.Exec(`INSERT INTO app_install_records(
		position, app_id, signature, app_name, kind, source, installed_at, package_sha256, package_bytes,
		has_missing_required, has_blocking_dependency, message) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(app_id) DO UPDATE SET position=excluded.position, signature=excluded.signature,
		app_name=excluded.app_name, kind=excluded.kind, source=excluded.source, installed_at=excluded.installed_at,
		package_sha256=excluded.package_sha256, package_bytes=excluded.package_bytes,
		has_missing_required=excluded.has_missing_required, has_blocking_dependency=excluded.has_blocking_dependency,
		message=excluded.message`, position, record.AppID, signature, record.AppName, record.Kind, record.Source,
		record.InstalledAt, record.PackageSHA, record.PackageSize, missing, blocking, record.Message); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM app_install_fields WHERE app_id=?`, record.AppID); err != nil {
		return err
	}
	value, err := maclawAppInstallRecordMap(record)
	if err != nil {
		return err
	}
	for key, fieldValue := range value {
		if maclawAppInstallMainFields[key] {
			continue
		}
		fields, err := flattenSQLiteValueFields(fieldValue)
		if err != nil {
			return err
		}
		for _, field := range fields {
			field.Path = sqliteMapFieldPath("", key) + field.Path
			if _, err := tx.Exec(`INSERT INTO app_install_fields(
				app_id, field_path, value_type, value_text, value_number, value_bool) VALUES(?,?,?,?,?,?)`,
				record.AppID, field.Path, field.Kind, nullableSQLiteFieldText(field), nullableSQLiteFieldNumber(field), nullableSQLiteFieldBool(field)); err != nil {
				return err
			}
		}
	}
	return nil
}

func loadMaclawAppInstallRegistry(db *sql.DB) (maclawAppInstallRegistry, error) {
	registry := maclawAppInstallRegistry{Schema: "maclaw.app.installs.v1", Installs: []maclawAppInstallRecord{}}
	rows, err := db.Query(`SELECT app_id, app_name, kind, source, installed_at, package_sha256, package_bytes,
		has_missing_required, has_blocking_dependency, message FROM app_install_records ORDER BY position`)
	if err != nil {
		return registry, err
	}
	valuesByID := map[string]map[string]interface{}{}
	positions := []string{}
	for rows.Next() {
		var id, name, kind, source, installedAt, packageSHA, message string
		var packageSize, missing, blocking int
		if err := rows.Scan(&id, &name, &kind, &source, &installedAt, &packageSHA, &packageSize, &missing, &blocking, &message); err != nil {
			_ = rows.Close()
			return registry, err
		}
		valuesByID[id] = map[string]interface{}{
			"app_id": id, "app_name": name, "kind": kind, "source": source, "installed_at": installedAt,
			"package_sha256": packageSHA, "package_bytes": packageSize,
			"has_missing_required": missing != 0, "has_blocking_dependency": blocking != 0, "message": message,
		}
		positions = append(positions, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return registry, err
	}
	_ = rows.Close()
	fieldRows, err := db.Query(`SELECT app_id, field_path, value_type, value_text, value_number, value_bool
		FROM app_install_fields ORDER BY app_id, field_path`)
	if err != nil {
		return registry, err
	}
	defer fieldRows.Close()
	fieldsByID := map[string][]sqliteValueField{}
	for fieldRows.Next() {
		var id string
		var field sqliteValueField
		var text sql.NullString
		var number sql.NullFloat64
		var boolean sql.NullBool
		if err := fieldRows.Scan(&id, &field.Path, &field.Kind, &text, &number, &boolean); err != nil {
			return registry, err
		}
		field.Text = text.String
		field.Number = number.Float64
		field.Bool = boolean.Valid && boolean.Bool
		fieldsByID[id] = append(fieldsByID[id], field)
	}
	if err := fieldRows.Err(); err != nil {
		return registry, err
	}
	for id, fields := range fieldsByID {
		values, err := unflattenSQLiteValueFields(fields)
		if err != nil {
			return registry, err
		}
		for key, value := range values {
			valuesByID[id][key] = value
		}
	}
	for _, id := range positions {
		raw, err := json.Marshal(valuesByID[id])
		if err != nil {
			return registry, err
		}
		var record maclawAppInstallRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			return registry, err
		}
		registry.Installs = append(registry.Installs, record)
	}
	if err := db.QueryRow(`SELECT COALESCE((SELECT value FROM app_install_meta WHERE key='schema'), 'maclaw.app.installs.v1'),
		COALESCE((SELECT value FROM app_install_meta WHERE key='updated_at'), '')`).Scan(&registry.Schema, &registry.UpdatedAt); err != nil {
		return registry, err
	}
	return registry, nil
}

func (a *App) readMaclawAppInstallRegistryDB() (maclawAppInstallRegistry, error) {
	db, err := a.maclawAppInstallDBOpen()
	if err != nil {
		return maclawAppInstallRegistry{Schema: "maclaw.app.installs.v1"}, err
	}
	return loadMaclawAppInstallRegistry(db)
}

func (a *App) writeMaclawAppInstallRegistryDB(registry maclawAppInstallRegistry) error {
	db, err := a.maclawAppInstallDBOpen()
	if err != nil {
		return err
	}
	return saveMaclawAppInstallRegistry(db, registry)
}
