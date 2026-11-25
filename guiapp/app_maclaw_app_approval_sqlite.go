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

type maclawAppApprovalDatabase struct {
	db       *sql.DB
	path     string
	imported bool
}

var maclawAppApprovalDBMu sync.Mutex

func (a *App) maclawAppApprovalDBPath() string {
	return filepath.Join(a.GetDataDir(), "app_approval_instances.db")
}

func (a *App) maclawAppApprovalDBOpen() (*sql.DB, error) {
	maclawAppApprovalDBMu.Lock()
	defer maclawAppApprovalDBMu.Unlock()
	if a == nil {
		return nil, os.ErrInvalid
	}
	path := a.maclawAppApprovalDBPath()
	if a.maclawAppApprovalDB != nil && a.maclawAppApprovalDB.path != path {
		_ = a.maclawAppApprovalDB.db.Close()
		a.maclawAppApprovalDB = nil
	}
	if a.maclawAppApprovalDB == nil {
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
CREATE TABLE IF NOT EXISTS app_approval_instances (
    position INTEGER PRIMARY KEY,
    instance_id TEXT NOT NULL UNIQUE,
    signature TEXT NOT NULL,
    app_id TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    lane TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    current_node TEXT NOT NULL DEFAULT '',
    owner TEXT NOT NULL DEFAULT '',
    approver TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT '',
    record_id TEXT NOT NULL DEFAULT '',
    approval_id TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_app_approval_app_lane ON app_approval_instances(app_id, lane);
CREATE INDEX IF NOT EXISTS idx_app_approval_record ON app_approval_instances(record_id);
CREATE TABLE IF NOT EXISTS app_approval_node_ids (
    instance_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    position INTEGER NOT NULL,
    node_id TEXT NOT NULL,
    PRIMARY KEY(instance_id, kind, position),
    FOREIGN KEY(instance_id) REFERENCES app_approval_instances(instance_id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS app_approval_fields (
    instance_id TEXT NOT NULL,
    field_path TEXT NOT NULL,
    value_type TEXT NOT NULL,
    value_text TEXT,
    value_number REAL,
    value_bool INTEGER,
    PRIMARY KEY(instance_id, field_path),
    FOREIGN KEY(instance_id) REFERENCES app_approval_instances(instance_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_app_approval_fields_path ON app_approval_fields(field_path);
CREATE TABLE IF NOT EXISTS app_approval_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`); err != nil {
			_ = db.Close()
			return nil, err
		}
		a.maclawAppApprovalDB = &maclawAppApprovalDatabase{db: db, path: path}
	}
	store := a.maclawAppApprovalDB
	if !store.imported {
		if err := a.importLegacyMaclawAppApprovalRegistry(store.db); err != nil {
			return nil, err
		}
		store.imported = true
	}
	return store.db, nil
}

func (a *App) closeMaclawAppApprovalDB() {
	maclawAppApprovalDBMu.Lock()
	defer maclawAppApprovalDBMu.Unlock()
	a.closeMaclawAppApprovalDBLocked()
}

// closeMaclawAppApprovalDBLocked assumes the caller already holds
// maclawAppApprovalDBMu. Used from resetPathBoundStateForDataDirChange which
// takes the lock before calling the close helpers.
func (a *App) closeMaclawAppApprovalDBLocked() {
	if a == nil || a.maclawAppApprovalDB == nil {
		return
	}
	_ = a.maclawAppApprovalDB.db.Close()
	a.maclawAppApprovalDB = nil
}

func (a *App) importLegacyMaclawAppApprovalRegistry(db *sql.DB) error {
	var imported int
	if err := db.QueryRow(`SELECT COUNT(*) FROM app_approval_meta WHERE key='legacy_json_imported'`).Scan(&imported); err != nil {
		return err
	}
	if imported > 0 {
		return nil
	}
	registry := maclawAppApprovalRegistry{Schema: "maclaw.app.approvals.v1", Instances: []maclawAppApprovalInstance{}}
	data, err := os.ReadFile(a.maclawAppApprovalRegistryPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &registry); err != nil {
			return fmt.Errorf("decode maclaw app approval registry: %w", err)
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := writeMaclawAppApprovalRegistryTx(tx, registry); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO app_approval_meta(key, value) VALUES('legacy_json_imported', '1')`); err != nil {
		return err
	}
	return tx.Commit()
}

func maclawAppApprovalInstanceSignature(instance maclawAppApprovalInstance) (string, error) {
	raw, err := json.Marshal(instance)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}

var maclawAppApprovalMainFields = map[string]bool{
	"instance_id": true, "app_id": true, "title": true, "lane": true, "status": true,
	"current_node": true, "owner": true, "approver": true, "updated_at": true,
	"record_id": true, "approval_id": true,
}

func maclawAppApprovalInstanceMap(instance maclawAppApprovalInstance) (map[string]interface{}, error) {
	raw, err := json.Marshal(instance)
	if err != nil {
		return nil, err
	}
	var value map[string]interface{}
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func saveMaclawAppApprovalRegistry(db *sql.DB, registry maclawAppApprovalRegistry) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := writeMaclawAppApprovalRegistryTx(tx, registry); err != nil {
		return err
	}
	return tx.Commit()
}

func writeMaclawAppApprovalRegistryTx(tx *sql.Tx, registry maclawAppApprovalRegistry) error {
	type storedSignature struct {
		signature string
		position  int
	}
	existing := map[string]storedSignature{}
	rows, err := tx.Query(`SELECT instance_id, signature, position FROM app_approval_instances`)
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
	for position, instance := range registry.Instances {
		if stored, ok := existing[instance.InstanceID]; ok && stored.position != position {
			reordered = true
			break
		}
	}
	if reordered {
		if _, err := tx.Exec(`UPDATE app_approval_instances SET position=-position-1`); err != nil {
			return err
		}
	}
	kept := map[string]struct{}{}
	for position, instance := range registry.Instances {
		if strings.TrimSpace(instance.InstanceID) == "" {
			continue
		}
		signature, err := maclawAppApprovalInstanceSignature(instance)
		if err != nil {
			return err
		}
		kept[instance.InstanceID] = struct{}{}
		stored := existing[instance.InstanceID]
		if stored.signature == signature {
			if reordered {
				if _, err := tx.Exec(`UPDATE app_approval_instances SET position=? WHERE instance_id=?`, position, instance.InstanceID); err != nil {
					return fmt.Errorf("[dbg248] %w", err)
				}
				continue
			}
			if stored.position == position {
				continue
			}
		}
		if err := writeMaclawAppApprovalInstance(tx, instance, position, signature); err != nil {
			return err
		}
	}
	staleRows, err := tx.Query(`SELECT instance_id FROM app_approval_instances`)
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
		if _, err := tx.Exec(`DELETE FROM app_approval_instances WHERE instance_id=?`, id); err != nil {
			return err
		}
	}
	schema := registry.Schema
	if schema == "" {
		schema = "maclaw.app.approvals.v1"
	}
	for key, value := range map[string]string{"schema": schema, "updated_at": registry.UpdatedAt} {
		if _, err := tx.Exec(`INSERT INTO app_approval_meta(key, value) VALUES(?,?)
			ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value); err != nil {
			return err
		}
	}
	return nil
}

func writeMaclawAppApprovalInstance(tx *sql.Tx, instance maclawAppApprovalInstance, position int, signature string) error {
	exists := false
	var storedID string
	switch err := tx.QueryRow(`SELECT instance_id FROM app_approval_instances WHERE instance_id=?`, instance.InstanceID).Scan(&storedID); {
	case err == nil:
		exists = true
	case errors.Is(err, sql.ErrNoRows):
		exists = false
	default:
		return err
	}
	if exists {
		// position is the table's rowid/PRIMARY KEY. An UPSERT whose conflict
		// target is instance_id still trips "UNIQUE constraint failed:
		// app_approval_instances.position" when the row already owns that
		// position, so every in-place content update (status/lane/payload)
		// silently failed to persist. Update the row instead. Reordering
		// passes negate stored positions first, so moving onto the new
		// position is free.
		if _, err := tx.Exec(`UPDATE app_approval_instances SET position=?, signature=?, app_id=?, title=?, lane=?,
			status=?, current_node=?, owner=?, approver=?, updated_at=?, record_id=?, approval_id=?
			WHERE instance_id=?`,
			position, signature, instance.AppID, instance.Title, instance.Lane, instance.Status,
			instance.CurrentNode, instance.Owner, instance.Approver, instance.UpdatedAt, instance.RecordID, instance.ApprovalID,
			instance.InstanceID); err != nil {
			return err
		}
	} else if _, err := tx.Exec(`INSERT INTO app_approval_instances(
		position, instance_id, signature, app_id, title, lane, status, current_node, owner, approver, updated_at, record_id, approval_id)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		position, instance.InstanceID, signature, instance.AppID, instance.Title, instance.Lane, instance.Status,
		instance.CurrentNode, instance.Owner, instance.Approver, instance.UpdatedAt, instance.RecordID, instance.ApprovalID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM app_approval_node_ids WHERE instance_id=?`, instance.InstanceID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM app_approval_fields WHERE instance_id=?`, instance.InstanceID); err != nil {
		return err
	}
	for kind, nodes := range map[string][]string{"current": instance.CurrentNodeIDs, "workflow": instance.WorkflowNodeIDs} {
		for position, nodeID := range nodes {
			if _, err := tx.Exec(`INSERT INTO app_approval_node_ids(instance_id, kind, position, node_id) VALUES(?,?,?,?)`, instance.InstanceID, kind, position, nodeID); err != nil {
				return err
			}
		}
	}
	value, err := maclawAppApprovalInstanceMap(instance)
	if err != nil {
		return err
	}
	for key, fieldValue := range value {
		if maclawAppApprovalMainFields[key] || key == "current_node_ids" || key == "workflow_node_ids" {
			continue
		}
		fields, err := flattenSQLiteValueFields(fieldValue)
		if err != nil {
			return err
		}
		for _, field := range fields {
			field.Path = sqliteMapFieldPath("", key) + field.Path
			if _, err := tx.Exec(`INSERT INTO app_approval_fields(
				instance_id, field_path, value_type, value_text, value_number, value_bool) VALUES(?,?,?,?,?,?)`,
				instance.InstanceID, field.Path, field.Kind, nullableSQLiteFieldText(field), nullableSQLiteFieldNumber(field), nullableSQLiteFieldBool(field)); err != nil {
				return err
			}
		}
	}
	return nil
}

func loadMaclawAppApprovalRegistry(db *sql.DB) (maclawAppApprovalRegistry, error) {
	registry := maclawAppApprovalRegistry{Schema: "maclaw.app.approvals.v1", Instances: []maclawAppApprovalInstance{}}
	rows, err := db.Query(`SELECT instance_id, app_id, title, lane, status, current_node, owner, approver,
		updated_at, record_id, approval_id FROM app_approval_instances ORDER BY position`)
	if err != nil {
		return registry, err
	}
	valuesByID := map[string]map[string]interface{}{}
	positions := []string{}
	for rows.Next() {
		var id, appID, title, lane, status, currentNode, owner, approver, updatedAt, recordID, approvalID string
		if err := rows.Scan(&id, &appID, &title, &lane, &status, &currentNode, &owner, &approver, &updatedAt, &recordID, &approvalID); err != nil {
			_ = rows.Close()
			return registry, err
		}
		value := map[string]interface{}{
			"app_id": appID, "title": title, "lane": lane, "status": status,
			"current_node": currentNode, "owner": owner, "approver": approver,
			"updated_at": updatedAt, "record_id": recordID, "approval_id": approvalID,
		}
		value["instance_id"] = id
		valuesByID[id] = value
		positions = append(positions, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return registry, err
	}
	_ = rows.Close()
	nodeRows, err := db.Query(`SELECT instance_id, kind, node_id FROM app_approval_node_ids ORDER BY instance_id, kind, position`)
	if err != nil {
		return registry, err
	}
	for nodeRows.Next() {
		var id, kind, nodeID string
		if err := nodeRows.Scan(&id, &kind, &nodeID); err != nil {
			_ = nodeRows.Close()
			return registry, err
		}
		value := valuesByID[id]
		if kind == "current" {
			value["current_node_ids"] = appendStringAny(value["current_node_ids"], nodeID)
		} else {
			value["workflow_node_ids"] = appendStringAny(value["workflow_node_ids"], nodeID)
		}
	}
	if err := nodeRows.Err(); err != nil {
		_ = nodeRows.Close()
		return registry, err
	}
	_ = nodeRows.Close()
	fieldRows, err := db.Query(`SELECT instance_id, field_path, value_type, value_text, value_number, value_bool
		FROM app_approval_fields ORDER BY instance_id, field_path`)
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
		fieldsValue, err := unflattenSQLiteValueFields(fields)
		if err != nil {
			return registry, err
		}
		for key, fieldValue := range fieldsValue {
			valuesByID[id][key] = fieldValue
		}
	}
	for _, id := range positions {
		raw, err := json.Marshal(valuesByID[id])
		if err != nil {
			return registry, err
		}
		var instance maclawAppApprovalInstance
		if err := json.Unmarshal(raw, &instance); err != nil {
			return registry, err
		}
		registry.Instances = append(registry.Instances, instance)
	}
	if err := db.QueryRow(`SELECT COALESCE((SELECT value FROM app_approval_meta WHERE key='schema'), 'maclaw.app.approvals.v1'),
		COALESCE((SELECT value FROM app_approval_meta WHERE key='updated_at'), '')`).Scan(&registry.Schema, &registry.UpdatedAt); err != nil {
		return registry, err
	}
	return registry, nil
}

func appendStringAny(value interface{}, item string) []interface{} {
	items, _ := value.([]interface{})
	return append(items, item)
}

func (a *App) readMaclawAppApprovalRegistryDB() (maclawAppApprovalRegistry, error) {
	db, err := a.maclawAppApprovalDBOpen()
	if err != nil {
		return maclawAppApprovalRegistry{Schema: "maclaw.app.approvals.v1"}, err
	}
	return loadMaclawAppApprovalRegistry(db)
}

func (a *App) writeMaclawAppApprovalRegistryDB(registry maclawAppApprovalRegistry) error {
	db, err := a.maclawAppApprovalDBOpen()
	if err != nil {
		return err
	}
	return saveMaclawAppApprovalRegistry(db, registry)
}
