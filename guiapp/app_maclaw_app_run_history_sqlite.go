package guiapp

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

type maclawAppRunHistoryDatabase struct {
	db       *sql.DB
	path     string
	imported bool
}

var maclawAppRunHistoryDBMu sync.Mutex

func (a *App) maclawAppRunHistoryDBPath() string {
	return filepath.Join(a.GetDataDir(), "app_run_history.db")
}

func (a *App) maclawAppRunHistoryDBOpen() (*sql.DB, error) {
	maclawAppRunHistoryDBMu.Lock()
	defer maclawAppRunHistoryDBMu.Unlock()
	if a == nil {
		return nil, os.ErrInvalid
	}
	path := a.maclawAppRunHistoryDBPath()
	if a.maclawAppRunHistoryDB != nil && a.maclawAppRunHistoryDB.path != path {
		_ = a.maclawAppRunHistoryDB.db.Close()
		a.maclawAppRunHistoryDB = nil
	}
	if a.maclawAppRunHistoryDB == nil {
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
CREATE TABLE IF NOT EXISTS app_run_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL COLLATE NOCASE UNIQUE,
    app_id TEXT NOT NULL,
    status TEXT NOT NULL,
    definition_hash TEXT NOT NULL DEFAULT '',
    test_protocol_fingerprint TEXT NOT NULL DEFAULT '',
    workspace_layout_fingerprint TEXT NOT NULL DEFAULT '',
    output_mode TEXT NOT NULL DEFAULT '',
    input_summary TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT '',
    artifact_id TEXT NOT NULL DEFAULT '',
    artifact_uri TEXT NOT NULL DEFAULT '',
    artifact_name TEXT NOT NULL DEFAULT '',
    artifact_path TEXT NOT NULL DEFAULT '',
    artifact_download_state TEXT NOT NULL DEFAULT '',
    skill_name TEXT NOT NULL DEFAULT '',
    governance_recorded INTEGER NOT NULL DEFAULT 0,
    governance_error TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT '',
    at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_app_run_history_app ON app_run_history(app_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_app_run_history_at ON app_run_history(at DESC);
CREATE TABLE IF NOT EXISTS app_run_history_fields (
    run_id TEXT NOT NULL COLLATE NOCASE,
    field_path TEXT NOT NULL,
    value_type TEXT NOT NULL,
    value_text TEXT,
    value_number REAL,
    value_bool INTEGER,
    PRIMARY KEY(run_id, field_path),
    FOREIGN KEY(run_id) REFERENCES app_run_history(run_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_app_run_history_fields_path ON app_run_history_fields(field_path);
CREATE TABLE IF NOT EXISTS app_run_history_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`); err != nil {
			_ = db.Close()
			return nil, err
		}
		a.maclawAppRunHistoryDB = &maclawAppRunHistoryDatabase{db: db, path: path}
	}
	store := a.maclawAppRunHistoryDB
	if !store.imported {
		if err := a.importLegacyMaclawAppRunHistory(store.db); err != nil {
			return nil, err
		}
		store.imported = true
	}
	return store.db, nil
}

func (a *App) closeMaclawAppRunHistoryDB() {
	maclawAppRunHistoryDBMu.Lock()
	defer maclawAppRunHistoryDBMu.Unlock()
	if a == nil || a.maclawAppRunHistoryDB == nil {
		return
	}
	_ = a.maclawAppRunHistoryDB.db.Close()
	a.maclawAppRunHistoryDB = nil
}

func (a *App) importLegacyMaclawAppRunHistory(db *sql.DB) error {
	var imported int
	if err := db.QueryRow(`SELECT COUNT(*) FROM app_run_history_meta WHERE key='legacy_json_imported'`).Scan(&imported); err != nil {
		return err
	}
	if imported > 0 {
		return nil
	}
	legacy := maclawAppRunHistoryStore{ByApp: map[string][]maclawAppRunHistoryEntry{}}
	data, err := os.ReadFile(a.maclawAppRunHistoryPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &legacy); err != nil {
			return fmt.Errorf("decode legacy maclaw app run history: %w", err)
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for appID, list := range legacy.ByApp {
		for index := len(list) - 1; index >= 0; index-- {
			entry := normalizeMaclawAppRunHistoryEntry(list[index])
			if entry.AppID == "" {
				entry.AppID = appID
			}
			if entry.RunID == "" || entry.AppID == "" {
				continue
			}
			if err := mergeAndWriteMaclawAppRunHistory(tx, entry); err != nil {
				return err
			}
		}
	}
	updatedAt := legacy.UpdatedAt
	if updatedAt == "" {
		updatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if _, err := tx.Exec(`INSERT INTO app_run_history_meta(key, value) VALUES('updated_at', ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, updatedAt); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO app_run_history_meta(key, value) VALUES('legacy_json_imported', '1')`); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *App) recordMaclawAppRunHistoryDB(entry maclawAppRunHistoryEntry) (maclawAppRunHistoryEntry, error) {
	db, err := a.maclawAppRunHistoryDBOpen()
	if err != nil {
		return maclawAppRunHistoryEntry{}, err
	}
	tx, err := db.Begin()
	if err != nil {
		return maclawAppRunHistoryEntry{}, err
	}
	defer func() { _ = tx.Rollback() }()
	merged, err := mergeMaclawAppRunHistoryRows(tx, entry)
	if err != nil {
		return maclawAppRunHistoryEntry{}, err
	}
	if err := writeMaclawAppRunHistoryRow(tx, merged); err != nil {
		return maclawAppRunHistoryEntry{}, err
	}
	if _, err := tx.Exec(`DELETE FROM app_run_history WHERE run_id IN (
		SELECT run_id FROM app_run_history WHERE app_id=? ORDER BY id DESC LIMIT -1 OFFSET ?
	)`, merged.AppID, maclawAppRunHistoryPerAppLimit); err != nil {
		return maclawAppRunHistoryEntry{}, err
	}
	if err := pruneMaclawAppRunHistoryApps(tx); err != nil {
		return maclawAppRunHistoryEntry{}, err
	}
	if _, err := tx.Exec(`INSERT INTO app_run_history_meta(key, value) VALUES('updated_at', ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return maclawAppRunHistoryEntry{}, err
	}
	if err := tx.Commit(); err != nil {
		return maclawAppRunHistoryEntry{}, err
	}
	return merged, nil
}

func pruneMaclawAppRunHistoryApps(tx *sql.Tx) error {
	var appCount int
	if err := tx.QueryRow(`SELECT COUNT(DISTINCT app_id) FROM app_run_history`).Scan(&appCount); err != nil {
		return err
	}
	if appCount <= maclawAppRunHistoryAppLimit {
		return nil
	}
	rows, err := tx.Query(`SELECT app_id,
		(SELECT r2.at FROM app_run_history r2 WHERE r2.app_id=r.app_id ORDER BY r2.id DESC LIMIT 1) AS newest_at
		FROM app_run_history AS r GROUP BY app_id`)
	if err != nil {
		return err
	}
	type appNewest struct {
		appID string
		at    string
	}
	items := make([]appNewest, 0)
	for rows.Next() {
		var item appNewest
		if err := rows.Scan(&item.appID, &item.at); err != nil {
			_ = rows.Close()
			return err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	for index := 1; index < len(items); index++ {
		for current := index; current > 0 && items[current].at < items[current-1].at; current-- {
			items[current], items[current-1] = items[current-1], items[current]
		}
	}
	for len(items) > maclawAppRunHistoryAppLimit {
		if _, err := tx.Exec(`DELETE FROM app_run_history WHERE app_id=?`, items[0].appID); err != nil {
			return err
		}
		items = items[1:]
	}
	return nil
}

func mergeAndWriteMaclawAppRunHistory(tx *sql.Tx, entry maclawAppRunHistoryEntry) error {
	merged, err := mergeMaclawAppRunHistoryRows(tx, entry)
	if err != nil {
		return err
	}
	return writeMaclawAppRunHistoryRow(tx, merged)
}

func mergeMaclawAppRunHistoryRows(q maclawAppRunHistoryQueryer, entry maclawAppRunHistoryEntry) (maclawAppRunHistoryEntry, error) {
	existing, err := loadMaclawAppRunHistoryEntries(q, ` WHERE run_id=?`, []interface{}{entry.RunID}, "")
	if err != nil {
		return maclawAppRunHistoryEntry{}, err
	}
	for _, previous := range existing {
		entry = mergeMaclawAppRunHistoryEntry(entry, previous)
	}
	return entry, nil
}

func writeMaclawAppRunHistoryRow(tx *sql.Tx, entry maclawAppRunHistoryEntry) error {
	if _, err := tx.Exec(`DELETE FROM app_run_history WHERE run_id=?`, entry.RunID); err != nil {
		return err
	}
	governanceRecorded := 0
	if entry.GovernanceRecorded {
		governanceRecorded = 1
	}
	if _, err := tx.Exec(`INSERT INTO app_run_history(
		run_id, app_id, status, definition_hash, test_protocol_fingerprint, workspace_layout_fingerprint,
		output_mode, input_summary, message, artifact_id, artifact_uri, artifact_name, artifact_path,
		artifact_download_state, skill_name, governance_recorded, governance_error, source, at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		entry.RunID, entry.AppID, entry.Status, entry.DefinitionHash, entry.TestProtocolFingerprint,
		entry.WorkspaceLayoutFingerprint, entry.OutputMode, entry.InputSummary, entry.Message,
		entry.ArtifactID, entry.ArtifactURI, entry.ArtifactName, entry.ArtifactPath, entry.ArtifactDownloadState,
		entry.SkillName, governanceRecorded, entry.GovernanceError, entry.Source, entry.At); err != nil {
		return err
	}
	nested := map[string]interface{}{}
	if entry.Artifacts != nil {
		nested["Artifacts"] = entry.Artifacts
	}
	if entry.ResultPayload != nil {
		nested["ResultPayload"] = entry.ResultPayload
	}
	if entry.Outputs != nil {
		nested["Outputs"] = entry.Outputs
	}
	if entry.ResultCoverage != nil {
		nested["ResultCoverage"] = entry.ResultCoverage
	}
	if entry.DependencyVerification != nil {
		nested["DependencyVerification"] = entry.DependencyVerification
	}
	if entry.ApprovalInstance != nil {
		nested["ApprovalInstance"] = entry.ApprovalInstance
	}
	for key, value := range nested {
		fields, err := flattenSQLiteValueFields(value)
		if err != nil {
			return err
		}
		for _, field := range fields {
			field.Path = sqliteMapFieldPath("", key) + field.Path
			if _, err := tx.Exec(`INSERT INTO app_run_history_fields(
				run_id, field_path, value_type, value_text, value_number, value_bool) VALUES(?,?,?,?,?,?)`,
				entry.RunID, field.Path, field.Kind, nullableSQLiteFieldText(field), nullableSQLiteFieldNumber(field), nullableSQLiteFieldBool(field)); err != nil {
				return err
			}
		}
	}
	return nil
}

type maclawAppRunHistoryQueryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

const maclawAppRunHistoryColumns = `run_id, app_id, status, definition_hash, test_protocol_fingerprint,
	workspace_layout_fingerprint, output_mode, input_summary, message, artifact_id, artifact_uri,
	artifact_name, artifact_path, artifact_download_state, skill_name, governance_recorded,
	governance_error, source, at`

func loadMaclawAppRunHistoryEntries(q maclawAppRunHistoryQueryer, where string, args []interface{}, order string) ([]maclawAppRunHistoryEntry, error) {
	rows, err := q.Query(`SELECT `+maclawAppRunHistoryColumns+` FROM app_run_history`+where+order, args...)
	if err != nil {
		return nil, err
	}
	entries := make([]maclawAppRunHistoryEntry, 0)
	indexByRunID := make(map[string]int)
	for rows.Next() {
		var entry maclawAppRunHistoryEntry
		var governanceRecorded int
		if err := rows.Scan(&entry.RunID, &entry.AppID, &entry.Status, &entry.DefinitionHash,
			&entry.TestProtocolFingerprint, &entry.WorkspaceLayoutFingerprint, &entry.OutputMode,
			&entry.InputSummary, &entry.Message, &entry.ArtifactID, &entry.ArtifactURI, &entry.ArtifactName,
			&entry.ArtifactPath, &entry.ArtifactDownloadState, &entry.SkillName, &governanceRecorded,
			&entry.GovernanceError, &entry.Source, &entry.At); err != nil {
			_ = rows.Close()
			return nil, err
		}
		entry.GovernanceRecorded = governanceRecorded != 0
		entries = append(entries, entry)
		indexByRunID[strings.ToLower(entry.RunID)] = len(entries) - 1
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	if len(entries) == 0 {
		return entries, nil
	}
	fieldQuery := `SELECT run_id, field_path, value_type, value_text, value_number, value_bool FROM app_run_history_fields`
	fieldArgs := args
	if len(entries) <= 200 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(entries)), ",")
		fieldQuery += ` WHERE run_id IN (` + placeholders + `)`
		fieldArgs = make([]interface{}, 0, len(entries))
		for _, entry := range entries {
			fieldArgs = append(fieldArgs, entry.RunID)
		}
	} else if where != "" {
		fieldQuery += ` WHERE run_id IN (SELECT run_id FROM app_run_history` + where + `)`
	}
	fieldRows, err := q.Query(fieldQuery, fieldArgs...)
	if err != nil {
		return nil, err
	}
	defer fieldRows.Close()
	fieldsByRun := make(map[string][]sqliteValueField)
	for fieldRows.Next() {
		var runID string
		var field sqliteValueField
		var text sql.NullString
		var number sql.NullFloat64
		var boolean sql.NullBool
		if err := fieldRows.Scan(&runID, &field.Path, &field.Kind, &text, &number, &boolean); err != nil {
			return nil, err
		}
		field.Text = text.String
		field.Number = number.Float64
		field.Bool = boolean.Valid && boolean.Bool
		fieldsByRun[strings.ToLower(runID)] = append(fieldsByRun[strings.ToLower(runID)], field)
	}
	if err := fieldRows.Err(); err != nil {
		return nil, err
	}
	for runID, fields := range fieldsByRun {
		index, ok := indexByRunID[runID]
		if !ok {
			continue
		}
		entry := &entries[index]
		values, err := unflattenSQLiteValueFields(fields)
		if err != nil {
			return nil, err
		}
		for key, value := range values {
			switch key {
			case "Artifacts":
				entry.Artifacts, _ = value.([]any)
			case "ResultPayload":
				entry.ResultPayload, _ = value.(map[string]any)
			case "Outputs":
				entry.Outputs, _ = value.([]any)
			case "ResultCoverage":
				entry.ResultCoverage, _ = value.(map[string]any)
			case "DependencyVerification":
				entry.DependencyVerification, _ = value.(map[string]any)
			case "ApprovalInstance":
				entry.ApprovalInstance, _ = value.(map[string]any)
			}
		}
	}
	return entries, nil
}

func (a *App) listMaclawAppRunHistoryDB(appID string, limit int) ([]maclawAppRunHistoryEntry, error) {
	db, err := a.maclawAppRunHistoryDBOpen()
	if err != nil {
		return nil, err
	}
	return loadMaclawAppRunHistoryEntries(db, ` WHERE app_id=?`, []interface{}{appID}, fmt.Sprintf(` ORDER BY id DESC LIMIT %d`, limit))
}

func (a *App) listAllMaclawAppRunHistoryDB(limit int) ([]maclawAppRunHistoryEntry, error) {
	db, err := a.maclawAppRunHistoryDBOpen()
	if err != nil {
		return nil, err
	}
	return loadMaclawAppRunHistoryEntries(db, "", nil, fmt.Sprintf(` ORDER BY at DESC, id DESC LIMIT %d`, limit))
}

func (a *App) clearMaclawAppRunHistoryDB(appID string) (bool, error) {
	db, err := a.maclawAppRunHistoryDBOpen()
	if err != nil {
		return false, err
	}
	tx, err := db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.Exec(`DELETE FROM app_run_history WHERE app_id=?`, appID)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(`INSERT INTO app_run_history_meta(key, value) VALUES('updated_at', ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (a *App) readMaclawAppRunHistoryStoreDB() (maclawAppRunHistoryStore, error) {
	store := maclawAppRunHistoryStore{Schema: maclawAppRunHistorySchema, ByApp: map[string][]maclawAppRunHistoryEntry{}}
	db, err := a.maclawAppRunHistoryDBOpen()
	if err != nil {
		return store, err
	}
	entries, err := loadMaclawAppRunHistoryEntries(db, "", nil, ` ORDER BY id DESC`)
	if err != nil {
		return store, err
	}
	for _, entry := range entries {
		store.ByApp[entry.AppID] = append(store.ByApp[entry.AppID], entry)
	}
	if err := db.QueryRow(`SELECT COALESCE((SELECT value FROM app_run_history_meta WHERE key='updated_at'), '')`).Scan(&store.UpdatedAt); err != nil {
		return store, err
	}
	return store, nil
}
