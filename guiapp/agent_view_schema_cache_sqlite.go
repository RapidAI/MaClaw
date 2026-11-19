package guiapp

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

type agentViewSchemaDatabase struct {
	db       *sql.DB
	jsonPath string
	imported bool
}

func agentViewSchemaDBPath(jsonPath string) string {
	return filepath.Join(filepath.Dir(jsonPath), "agent_view_schemas.db")
}

func openAgentViewSchemaDBLocked(jsonPath string) (*sql.DB, error) {
	store := agentViewSchemaPersistentStore.database
	if store != nil && store.jsonPath != jsonPath {
		_ = store.db.Close()
		agentViewSchemaPersistentStore.database = nil
		store = nil
	}
	if store == nil {
		dbPath := agentViewSchemaDBPath(jsonPath)
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			return nil, err
		}
		db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
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
CREATE TABLE IF NOT EXISTS agent_view_schemas (
    version TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    schema_id TEXT NOT NULL,
    view_id TEXT NOT NULL DEFAULT '',
    view_type TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    first_seen_unix_nano INTEGER NOT NULL,
    last_seen_unix_nano INTEGER NOT NULL,
    use_count INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_agent_view_schemas_last_seen ON agent_view_schemas(last_seen_unix_nano);
CREATE TABLE IF NOT EXISTS agent_view_schema_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`); err != nil {
			_ = db.Close()
			return nil, err
		}
		store = &agentViewSchemaDatabase{db: db, jsonPath: jsonPath}
		agentViewSchemaPersistentStore.database = store
	}
	if !store.imported {
		if err := importLegacyAgentViewSchemas(store.db, jsonPath); err != nil {
			return nil, err
		}
		store.imported = true
	}
	return store.db, nil
}

func importLegacyAgentViewSchemas(db *sql.DB, jsonPath string) error {
	var imported int
	if err := db.QueryRow(`SELECT COUNT(*) FROM agent_view_schema_meta WHERE key='legacy_json_imported'`).Scan(&imported); err != nil {
		return err
	}
	if imported > 0 {
		return nil
	}
	var snapshot agentViewSchemaSnapshot
	data, err := os.ReadFile(jsonPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &snapshot); err != nil {
			return err
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, record := range snapshot.Records {
		if strings.TrimSpace(record.Version) == "" {
			continue
		}
		if err := writeAgentViewSchemaRecord(tx, record); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO agent_view_schema_meta(key, value) VALUES('legacy_json_imported', '1')`); err != nil {
		return err
	}
	return tx.Commit()
}

func agentViewSchemaUnixNano(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UnixNano()
}

func agentViewSchemaTimeFromUnixNano(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.Unix(0, value)
}

func upsertAgentViewSchemaRecordLocked(jsonPath string, record agentViewSchemaRecord) error {
	db, err := openAgentViewSchemaDBLocked(jsonPath)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO agent_view_schemas(
		version, source, schema_id, view_id, view_type, title, first_seen_unix_nano, last_seen_unix_nano, use_count)
		VALUES(?,?,?,?,?,?,?,?,1)
		ON CONFLICT(version) DO UPDATE SET
			source=excluded.source, schema_id=excluded.schema_id, view_id=excluded.view_id,
			view_type=excluded.view_type, title=excluded.title, last_seen_unix_nano=excluded.last_seen_unix_nano,
			use_count=agent_view_schemas.use_count+1`,
		record.Version, record.Source, record.ID, record.ViewID, record.ViewType, record.Title,
		agentViewSchemaUnixNano(record.FirstSeen), agentViewSchemaUnixNano(record.LastSeen))
	return err
}

func writeAgentViewSchemaRecord(tx *sql.Tx, record agentViewSchemaRecord) error {
	_, err := tx.Exec(`INSERT INTO agent_view_schemas(
		version, source, schema_id, view_id, view_type, title, first_seen_unix_nano, last_seen_unix_nano, use_count)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(version) DO UPDATE SET
			source=excluded.source, schema_id=excluded.schema_id, view_id=excluded.view_id,
			view_type=excluded.view_type, title=excluded.title, first_seen_unix_nano=excluded.first_seen_unix_nano,
			last_seen_unix_nano=excluded.last_seen_unix_nano, use_count=excluded.use_count`,
		record.Version, record.Source, record.ID, record.ViewID, record.ViewType, record.Title,
		agentViewSchemaUnixNano(record.FirstSeen), agentViewSchemaUnixNano(record.LastSeen), record.UseCount)
	return err
}

func closeAgentViewSchemaStoreLocked() {
	if agentViewSchemaPersistentStore.database == nil {
		return
	}
	_ = agentViewSchemaPersistentStore.database.db.Close()
	agentViewSchemaPersistentStore.database = nil
}
