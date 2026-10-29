package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

const mobileStateSchema = `
CREATE TABLE IF NOT EXISTS mobile_state_meta (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS drafts (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	owner_id TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS drafts_tenant_owner_updated ON drafts(tenant_id, owner_id, updated_at);
CREATE TABLE IF NOT EXISTS exports (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	owner_id TEXT NOT NULL,
	draft_id TEXT NOT NULL,
	status TEXT NOT NULL,
	created_at TEXT NOT NULL,
	payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS exports_tenant_owner_created ON exports(tenant_id, owner_id, created_at);
CREATE INDEX IF NOT EXISTS exports_tenant_draft ON exports(tenant_id, draft_id);
CREATE TABLE IF NOT EXISTS uploads (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	owner_id TEXT NOT NULL,
	draft_id TEXT NOT NULL,
	status TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS uploads_tenant_owner_status ON uploads(tenant_id, owner_id, status, updated_at);
CREATE INDEX IF NOT EXISTS uploads_tenant_draft ON uploads(tenant_id, draft_id);
CREATE TABLE IF NOT EXISTS meeting_recordings (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	owner_id TEXT NOT NULL,
	status TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS meetings_tenant_owner_updated ON meeting_recordings(tenant_id, owner_id, updated_at);
CREATE TABLE IF NOT EXISTS digital_employee_tasks (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	owner_id TEXT NOT NULL,
	employee_id TEXT NOT NULL,
	status TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS digital_tasks_tenant_employee_status ON digital_employee_tasks(tenant_id, employee_id, status, created_at);
CREATE INDEX IF NOT EXISTS digital_tasks_tenant_owner_updated ON digital_employee_tasks(tenant_id, owner_id, updated_at);
CREATE TABLE IF NOT EXISTS push_devices (
	tenant_id TEXT NOT NULL,
	owner_id TEXT NOT NULL,
	id TEXT NOT NULL,
	device_id TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	payload TEXT NOT NULL,
	PRIMARY KEY(tenant_id, owner_id, id)
);
CREATE INDEX IF NOT EXISTS push_devices_tenant_owner ON push_devices(tenant_id, owner_id);
CREATE TABLE IF NOT EXISTS push_pending (
	tenant_id TEXT NOT NULL,
	owner_id TEXT NOT NULL,
	id TEXT NOT NULL,
	status TEXT NOT NULL,
	task_id TEXT NOT NULL,
	created_at TEXT NOT NULL,
	payload TEXT NOT NULL,
	PRIMARY KEY(tenant_id, owner_id, id)
);
CREATE INDEX IF NOT EXISTS push_pending_tenant_owner_created ON push_pending(tenant_id, owner_id, created_at);
CREATE TABLE IF NOT EXISTS server_profiles (
	tenant_id TEXT NOT NULL,
	owner_id TEXT NOT NULL,
	source_machine_id TEXT NOT NULL,
	id TEXT NOT NULL,
	host TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	payload TEXT NOT NULL,
	PRIMARY KEY(tenant_id, owner_id, source_machine_id, id)
);
CREATE INDEX IF NOT EXISTS server_profiles_tenant_owner ON server_profiles(tenant_id, owner_id);
CREATE TABLE IF NOT EXISTS ssh_configs (
	tenant_id TEXT NOT NULL,
	owner_id TEXT NOT NULL,
	profile_id TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	payload TEXT NOT NULL,
	PRIMARY KEY(tenant_id, owner_id, profile_id)
);
CREATE INDEX IF NOT EXISTS ssh_configs_tenant_owner ON ssh_configs(tenant_id, owner_id);
CREATE TABLE IF NOT EXISTS purged_owners (
	tenant_id TEXT NOT NULL,
	owner_id TEXT NOT NULL,
	purged_at TEXT NOT NULL,
	PRIMARY KEY(tenant_id, owner_id)
);
`

var mobileEntityTables = []string{
	"drafts", "exports", "uploads", "meeting_recordings", "digital_employee_tasks",
	"push_devices", "push_pending", "server_profiles", "ssh_configs",
}

var mobileSQLiteState = struct {
	sync.Mutex
	db       *sql.DB
	path     string
	fallback bool
}{}

var mobileStateBeforeMigrationCommitForTest func() error

var mobilePurgeTombstonesLoaded atomic.Bool

func mobileStateDBPath(statePath string) string {
	statePath = strings.TrimSpace(statePath)
	if statePath == "" {
		return ""
	}
	ext := filepath.Ext(statePath)
	if ext == "" {
		return statePath + ".db"
	}
	return strings.TrimSuffix(statePath, ext) + ".db"
}

func mobileOpenStateDB(statePath string) (*sql.DB, bool, error) {
	dbPath := mobileStateDBPath(statePath)
	if dbPath == "" {
		return nil, false, errors.New("mobile state path is empty")
	}
	mobileSQLiteState.Lock()
	defer mobileSQLiteState.Unlock()
	if mobileSQLiteState.db != nil && mobileSQLiteState.path == dbPath {
		return mobileSQLiteState.db, mobileSQLiteState.fallback, nil
	}
	if mobileSQLiteState.db != nil {
		_ = mobileSQLiteState.db.Close()
	}
	mobileSQLiteState.db = nil
	mobileSQLiteState.path = dbPath
	mobileSQLiteState.fallback = false
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		mobileSQLiteState.fallback = true
		return nil, true, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		mobileSQLiteState.fallback = true
		return nil, true, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL;`); err == nil {
		_, err = db.Exec(mobileStateSchema)
	}
	if err == nil {
		err = os.Chmod(dbPath, 0o600)
	}
	if err != nil {
		_ = db.Close()
		mobileSQLiteState.fallback = true
		return nil, true, err
	}
	mobileSQLiteState.db = db
	return db, false, nil
}

func mobileSyncPurgeTombstones(db *sql.DB) {
	if db == nil || mobilePurgeTombstonesLoaded.Load() {
		return
	}
	rows, err := db.Query(`SELECT tenant_id, owner_id FROM purged_owners`)
	if err != nil {
		return
	}
	type owner struct{ tenantID, ownerID string }
	var owners []owner
	scanErr := func() error {
		defer rows.Close()
		for rows.Next() {
			var item owner
			if err := rows.Scan(&item.tenantID, &item.ownerID); err != nil {
				return err
			}
			owners = append(owners, item)
		}
		return rows.Err()
	}()
	if scanErr != nil {
		return
	}
	mobileKnowledgePurgeState.Lock()
	defer mobileKnowledgePurgeState.Unlock()
	if mobilePurgeTombstonesLoaded.Load() {
		return
	}
	for _, item := range owners {
		mobileKnowledgePurgeState.purged[mobileKnowledgeOwnerKey(item.tenantID, item.ownerID)] = struct{}{}
	}
	mobilePurgeTombstonesLoaded.Store(true)
}

func mobileCloseStateDBForTest() {
	mobileSQLiteState.Lock()
	if mobileSQLiteState.db != nil {
		_ = mobileSQLiteState.db.Close()
	}
	mobileSQLiteState.db = nil
	mobileSQLiteState.path = ""
	mobileSQLiteState.fallback = false
	mobileSQLiteState.Unlock()
	mobilePurgeTombstonesLoaded.Store(false)
}

func mobileReadLegacyState(path string) (mobilePersistentState, error) {
	var state mobilePersistentState
	raw, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	if len(raw) == 0 {
		return state, ioEmptyStateError{}
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return state, err
	}
	return state, nil
}

type ioEmptyStateError struct{}

func (ioEmptyStateError) Error() string { return "mobile state file is empty" }

func mobileLoadPersistentState() mobilePersistentState {
	path := mobileStatePath()
	if path == "" {
		return mobilePersistentState{}
	}
	db, fallback, err := mobileOpenStateDB(path)
	if err != nil || fallback {
		state, _ := mobileReadLegacyState(path)
		mobileNormalizeLoadedState(&state)
		return state
	}
	initialized, err := mobileStateInitialized(db)
	if err != nil {
		mobileMarkStateFallback()
		state, _ := mobileReadLegacyState(path)
		mobileNormalizeLoadedState(&state)
		return state
	}
	if !initialized {
		legacy, legacyErr := mobileReadLegacyState(path)
		if legacyErr == nil {
			mobilePrepareLegacyStateForMigration(&legacy)
			if err := mobileImportState(db, &legacy); err != nil {
				mobileMarkStateFallback()
				mobileNormalizeLoadedState(&legacy)
				return legacy
			}
			_ = os.Rename(path, path+".migrated.bak")
			mobileNormalizeLoadedState(&legacy)
			return legacy
		}
	}
	state, err := mobileReadStateDB(db)
	if err != nil {
		mobileMarkStateFallback()
		legacy, _ := mobileReadLegacyState(path)
		mobileNormalizeLoadedState(&legacy)
		return legacy
	}
	mobileNormalizeLoadedState(&state)
	return state
}

func mobileMarkStateFallback() {
	mobileSQLiteState.Lock()
	mobileSQLiteState.fallback = true
	mobileSQLiteState.Unlock()
}

func mobileStateInitialized(db *sql.DB) (bool, error) {
	var marker int
	err := db.QueryRow(`SELECT 1 FROM mobile_state_meta WHERE key='initialized'`).Scan(&marker)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	for _, table := range mobileEntityTables {
		var one int
		if err := db.QueryRow(`SELECT 1 FROM ` + table + ` LIMIT 1`).Scan(&one); err == nil {
			return true, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
	}
	return false, nil
}

func mobilePrepareLegacyStateForMigration(state *mobilePersistentState) {
	if state == nil {
		return
	}
	mobileNormalizeLoadedState(state)
	for id, record := range state.Drafts {
		state.Drafts[id] = mobileStripDraftBlobForPersist(record)
	}
	for id, record := range state.Uploads {
		state.Uploads[id] = mobileStripUploadBlobForPersist(record)
	}
}

func mobileNormalizeLoadedState(state *mobilePersistentState) {
	if state == nil {
		return
	}
	for id, record := range state.Drafts {
		mobileNormalizePersistedTenantID(&record.TenantID)
		mobileNormalizeDraftSourceMeta(&record)
		state.Drafts[id] = record
	}
	for id, record := range state.Exports {
		mobileNormalizePersistedTenantID(&record.TenantID)
		state.Exports[id] = record
	}
	for id, record := range state.Uploads {
		mobileNormalizePersistedTenantID(&record.TenantID)
		mobileNormalizeUploadSourceMeta(&record)
		state.Uploads[id] = record
	}
	for id, record := range state.MeetingRecordings {
		mobileNormalizePersistedTenantID(&record.TenantID)
		if record.Status == "finalizing" {
			record.Status = "uploading"
			record.Message = "upload finalization interrupted; retry complete"
			record.Progress = 0
			record.UpdatedAt = time.Now().UTC()
		}
		state.MeetingRecordings[id] = record
	}
	for id, record := range state.DigitalEmployeeTasks {
		mobileNormalizePersistedTenantID(&record.TenantID)
		state.DigitalEmployeeTasks[id] = record
	}
	for id, record := range state.ServerProfiles {
		mobileNormalizePersistedTenantID(&record.TenantID)
		state.ServerProfiles[id] = record
	}
	for id, record := range state.SSHVaultSecrets {
		mobileNormalizePersistedTenantID(&record.TenantID)
		state.SSHVaultSecrets[id] = record
	}
}

func mobileImportState(db *sql.DB, state *mobilePersistentState) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := mobileWritePersistentStateTx(tx, state); err != nil {
		return err
	}
	if hook := mobileStateBeforeMigrationCommitForTest; hook != nil {
		if err := hook(); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO mobile_state_meta(key,value) VALUES('initialized','1')
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`); err != nil {
		return err
	}
	return tx.Commit()
}

func mobileWritePersistentStateTx(tx *sql.Tx, state *mobilePersistentState) error {
	for _, record := range state.Drafts {
		if err := mobileWriteDraftTx(tx, record); err != nil {
			return err
		}
	}
	for _, record := range state.Exports {
		if err := mobileWriteExportTx(tx, record); err != nil {
			return err
		}
	}
	for _, record := range state.Uploads {
		if err := mobileWriteUploadTx(tx, record); err != nil {
			return err
		}
	}
	for _, record := range state.MeetingRecordings {
		if err := mobileWriteMeetingTx(tx, record); err != nil {
			return err
		}
	}
	for _, record := range state.DigitalEmployeeTasks {
		if err := mobileWriteDigitalTaskTx(tx, record); err != nil {
			return err
		}
	}
	for key, records := range state.PushDevices {
		tenantID, ownerID := mobilePushSplitUserKey(key)
		for _, record := range records {
			if err := mobileWritePushDeviceTx(tx, tenantID, ownerID, record); err != nil {
				return err
			}
		}
	}
	for key, records := range state.PushPending {
		tenantID, ownerID := mobilePushSplitUserKey(key)
		for _, record := range records {
			if err := mobileWritePushPendingTx(tx, tenantID, ownerID, record); err != nil {
				return err
			}
		}
	}
	for _, record := range state.ServerProfiles {
		if err := mobileWriteServerProfileTx(tx, record); err != nil {
			return err
		}
	}
	for _, record := range state.SSHVaultSecrets {
		if err := mobileWriteSSHConfigTx(tx, record); err != nil {
			return err
		}
	}
	return nil
}

func mobilePushSplitUserKey(key string) (string, string) {
	parts := strings.SplitN(key, "|", 2)
	if len(parts) != 2 {
		return mobileMeetingRecordingTenantID(""), key
	}
	return parts[0], parts[1]
}

func mobileReadStateDB(db *sql.DB) (mobilePersistentState, error) {
	state := mobilePersistentState{
		Drafts: make(map[string]mobileDocumentDraftRecord), Exports: make(map[string]mobileDocumentExportRecord),
		Uploads: make(map[string]mobileDocumentUploadRecord), MeetingRecordings: make(map[string]mobileMeetingRecording),
		DigitalEmployeeTasks: make(map[string]mobileDigitalEmployeeTaskRecord), PushDevices: make(map[string][]mobilePushDevice),
		PushPending: make(map[string][]mobilePushPendingItem), ServerProfiles: make(map[string]mobileServerProfileRecord),
		SSHVaultSecrets: make(map[string]mobileSSHVaultRecord),
	}
	if err := mobileReadPayloads(db, "drafts", func(raw []byte) error {
		var record mobileDocumentDraftRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			return err
		}
		state.Drafts[record.ID] = record
		return nil
	}); err != nil {
		return state, err
	}
	if err := mobileReadPayloads(db, "exports", func(raw []byte) error {
		var record mobileDocumentExportRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			return err
		}
		state.Exports[record.JobID] = record
		return nil
	}); err != nil {
		return state, err
	}
	if err := mobileReadPayloads(db, "uploads", func(raw []byte) error {
		var record mobileDocumentUploadRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			return err
		}
		state.Uploads[record.TaskID] = record
		return nil
	}); err != nil {
		return state, err
	}
	if err := mobileReadPayloads(db, "meeting_recordings", func(raw []byte) error {
		var record mobileMeetingRecording
		if err := json.Unmarshal(raw, &record); err != nil {
			return err
		}
		state.MeetingRecordings[record.ID] = record
		return nil
	}); err != nil {
		return state, err
	}
	if err := mobileReadPayloads(db, "digital_employee_tasks", func(raw []byte) error {
		var record mobileDigitalEmployeeTaskRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			return err
		}
		state.DigitalEmployeeTasks[record.TaskID] = record
		return nil
	}); err != nil {
		return state, err
	}
	if err := mobileReadPushDevices(db, state.PushDevices); err != nil {
		return state, err
	}
	if err := mobileReadPushPending(db, state.PushPending); err != nil {
		return state, err
	}
	if err := mobileReadPayloads(db, "server_profiles", func(raw []byte) error {
		var record mobileServerProfileRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			return err
		}
		key := record.TenantID + "\x00" + record.OwnerID + "\x00" + record.SourceMachineID + "\x00" + record.ProfileID
		state.ServerProfiles[key] = record
		return nil
	}); err != nil {
		return state, err
	}
	if err := mobileReadPayloads(db, "ssh_configs", func(raw []byte) error {
		var record mobileSSHVaultRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			return err
		}
		state.SSHVaultSecrets[mobileSSHVaultMapKey(record.TenantID, record.OwnerID, record.ProfileID)] = record
		return nil
	}); err != nil {
		return state, err
	}
	return state, nil
}

func mobileReadPayloads(db *sql.DB, table string, consume func([]byte) error) error {
	rows, err := db.Query(`SELECT payload FROM ` + table)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		if err := consume(raw); err != nil {
			return err
		}
	}
	return rows.Err()
}

func mobileReadPushDevices(db *sql.DB, out map[string][]mobilePushDevice) error {
	rows, err := db.Query(`SELECT tenant_id,owner_id,payload FROM push_devices`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var tenantID, ownerID string
		var raw []byte
		if err := rows.Scan(&tenantID, &ownerID, &raw); err != nil {
			return err
		}
		var record mobilePushDevice
		if err := json.Unmarshal(raw, &record); err != nil {
			return err
		}
		key := mobilePushUserKey(tenantID, ownerID)
		out[key] = append(out[key], record)
	}
	return rows.Err()
}

func mobileReadPushPending(db *sql.DB, out map[string][]mobilePushPendingItem) error {
	rows, err := db.Query(`SELECT tenant_id,owner_id,payload FROM push_pending`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var tenantID, ownerID string
		var raw []byte
		if err := rows.Scan(&tenantID, &ownerID, &raw); err != nil {
			return err
		}
		var record mobilePushPendingItem
		if err := json.Unmarshal(raw, &record); err != nil {
			return err
		}
		key := mobilePushUserKey(tenantID, ownerID)
		out[key] = append(out[key], record)
	}
	return rows.Err()
}

func mobileJSONPayload(record any) (string, error) {
	raw, err := json.Marshal(record)
	return string(raw), err
}

func mobileWriteDraftTx(tx *sql.Tx, record mobileDocumentDraftRecord) error {
	payload, err := mobileJSONPayload(record)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO drafts(id,tenant_id,owner_id,updated_at,payload) VALUES(?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET tenant_id=excluded.tenant_id,owner_id=excluded.owner_id,updated_at=excluded.updated_at,payload=excluded.payload`,
		record.ID, record.TenantID, record.OwnerID, record.UpdatedAt.UTC().Format(time.RFC3339Nano), payload)
	return err
}

func mobileWriteExportTx(tx *sql.Tx, record mobileDocumentExportRecord) error {
	payload, err := mobileJSONPayload(record)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO exports(id,tenant_id,owner_id,draft_id,status,created_at,payload) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET tenant_id=excluded.tenant_id,owner_id=excluded.owner_id,draft_id=excluded.draft_id,status=excluded.status,created_at=excluded.created_at,payload=excluded.payload`,
		record.JobID, record.TenantID, record.OwnerID, record.DraftID, record.Status, record.CreatedAt.UTC().Format(time.RFC3339Nano), payload)
	return err
}

func mobileWriteUploadTx(tx *sql.Tx, record mobileDocumentUploadRecord) error {
	payload, err := mobileJSONPayload(record)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO uploads(id,tenant_id,owner_id,draft_id,status,updated_at,payload) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET tenant_id=excluded.tenant_id,owner_id=excluded.owner_id,draft_id=excluded.draft_id,status=excluded.status,updated_at=excluded.updated_at,payload=excluded.payload`,
		record.TaskID, record.TenantID, record.OwnerID, record.DraftID, record.Status, record.UpdatedAt.UTC().Format(time.RFC3339Nano), payload)
	return err
}

func mobileWriteMeetingTx(tx *sql.Tx, record mobileMeetingRecording) error {
	payload, err := mobileJSONPayload(record)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO meeting_recordings(id,tenant_id,owner_id,status,updated_at,payload) VALUES(?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET tenant_id=excluded.tenant_id,owner_id=excluded.owner_id,status=excluded.status,updated_at=excluded.updated_at,payload=excluded.payload`,
		record.ID, record.TenantID, record.OwnerID, record.Status, record.UpdatedAt.UTC().Format(time.RFC3339Nano), payload)
	return err
}

func mobileWriteDigitalTaskTx(tx *sql.Tx, record mobileDigitalEmployeeTaskRecord) error {
	payload, err := mobileJSONPayload(record)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO digital_employee_tasks(id,tenant_id,owner_id,employee_id,status,created_at,updated_at,payload) VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET tenant_id=excluded.tenant_id,owner_id=excluded.owner_id,employee_id=excluded.employee_id,status=excluded.status,created_at=excluded.created_at,updated_at=excluded.updated_at,payload=excluded.payload`,
		record.TaskID, record.TenantID, record.OwnerID, record.EmployeeID, record.Status, record.CreatedAt.UTC().Format(time.RFC3339Nano), record.UpdatedAt.UTC().Format(time.RFC3339Nano), payload)
	return err
}

func mobileWritePushDeviceTx(tx *sql.Tx, tenantID, ownerID string, record mobilePushDevice) error {
	payload, err := mobileJSONPayload(record)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO push_devices(tenant_id,owner_id,id,device_id,updated_at,payload) VALUES(?,?,?,?,?,?)
		ON CONFLICT(tenant_id,owner_id,id) DO UPDATE SET device_id=excluded.device_id,updated_at=excluded.updated_at,payload=excluded.payload`,
		tenantID, ownerID, record.Token, record.DeviceID, record.UpdatedAt.UTC().Format(time.RFC3339Nano), payload)
	return err
}

func mobileWritePushPendingTx(tx *sql.Tx, tenantID, ownerID string, record mobilePushPendingItem) error {
	payload, err := mobileJSONPayload(record)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO push_pending(tenant_id,owner_id,id,status,task_id,created_at,payload) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(tenant_id,owner_id,id) DO UPDATE SET status=excluded.status,task_id=excluded.task_id,created_at=excluded.created_at,payload=excluded.payload`,
		tenantID, ownerID, record.ID, record.Status, record.TaskID, record.CreatedAt.UTC().Format(time.RFC3339Nano), payload)
	return err
}

func mobileWriteServerProfileTx(tx *sql.Tx, record mobileServerProfileRecord) error {
	payload, err := mobileJSONPayload(record)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO server_profiles(tenant_id,owner_id,source_machine_id,id,host,updated_at,payload) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(tenant_id,owner_id,source_machine_id,id) DO UPDATE SET host=excluded.host,updated_at=excluded.updated_at,payload=excluded.payload`,
		record.TenantID, record.OwnerID, record.SourceMachineID, record.ProfileID, record.Host, record.UpdatedAt.UTC().Format(time.RFC3339Nano), payload)
	return err
}

func mobileWriteSSHConfigTx(tx *sql.Tx, record mobileSSHVaultRecord) error {
	payload, err := mobileJSONPayload(record)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO ssh_configs(tenant_id,owner_id,profile_id,updated_at,payload) VALUES(?,?,?,?,?)
		ON CONFLICT(tenant_id,owner_id,profile_id) DO UPDATE SET updated_at=excluded.updated_at,payload=excluded.payload`,
		record.TenantID, record.OwnerID, record.ProfileID, record.UpdatedAt.UTC().Format(time.RFC3339Nano), payload)
	return err
}

func mobileRunStateTransaction(write func(*sql.Tx) error) {
	mobileStateWriteMu.Lock()
	defer mobileStateWriteMu.Unlock()
	db, fallback, err := mobileOpenStateDB(mobileStatePath())
	if err != nil || fallback {
		mobilePersistLegacyStateLocked()
		return
	}
	tx, err := db.Begin()
	if err != nil {
		log.Printf("[mobile-state] begin failed: %v", err)
		return
	}
	defer tx.Rollback()
	if err := write(tx); err != nil {
		if !errors.Is(err, errMobileOwnerPurged) {
			log.Printf("[mobile-state] write rolled back: %v", err)
		}
		return
	}
	if _, err := tx.Exec(`INSERT INTO mobile_state_meta(key,value) VALUES('initialized','1')
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`); err != nil {
		log.Printf("[mobile-state] meta update rolled back: %v", err)
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("[mobile-state] commit failed: %v", err)
	}
}

type mobileStateOwner struct {
	tenantID string
	ownerID  string
}

func mobileRunOwnedStateTransaction(owners []mobileStateOwner, write func(*sql.Tx) error) {
	if db, fallback, err := mobileOpenStateDB(mobileStatePath()); err == nil && !fallback {
		mobileSyncPurgeTombstones(db)
	}
	mobileKnowledgePurgeState.RLock()
	for _, owner := range owners {
		if !mobileOwnerWriteAllowedLocked(owner.tenantID, owner.ownerID) {
			mobileKnowledgePurgeState.RUnlock()
			return
		}
	}
	mobileKnowledgePurgeState.RUnlock()
	mobileRunStateTransaction(func(tx *sql.Tx) error {
		for _, owner := range owners {
			purged, err := mobileOwnerPurgedTx(tx, owner.tenantID, owner.ownerID)
			if err != nil {
				return err
			}
			if purged {
				return errMobileOwnerPurged
			}
		}
		return write(tx)
	})
}

func mobileOwnerPurgedTx(tx *sql.Tx, tenantID, ownerID string) (bool, error) {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return true, nil
	}
	tenants := []string{mobileMeetingRecordingTenantID(tenantID)}
	if raw := strings.TrimSpace(tenantID); raw != "" && raw != tenants[0] {
		tenants = append(tenants, raw)
	}
	for _, tenant := range tenants {
		var one int
		err := tx.QueryRow(`SELECT 1 FROM purged_owners WHERE tenant_id=? AND owner_id=?`, tenant, ownerID).Scan(&one)
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
	}
	return false, nil
}

type mobileDocumentChanges struct {
	drafts         []mobileDocumentDraftRecord
	exports        []mobileDocumentExportRecord
	uploads        []mobileDocumentUploadRecord
	deletedDrafts  []string
	deletedUploads []string
}

func mobilePersistDocumentChanges(changes mobileDocumentChanges) {
	mobilePrepareDocumentChangesForPersist(&changes)
	owners := make([]mobileStateOwner, 0, len(changes.drafts)+len(changes.exports)+len(changes.uploads))
	for _, record := range changes.drafts {
		owners = append(owners, mobileStateOwner{tenantID: record.TenantID, ownerID: record.OwnerID})
	}
	for _, record := range changes.exports {
		owners = append(owners, mobileStateOwner{tenantID: record.TenantID, ownerID: record.OwnerID})
	}
	for _, record := range changes.uploads {
		owners = append(owners, mobileStateOwner{tenantID: record.TenantID, ownerID: record.OwnerID})
	}
	mobileRunOwnedStateTransaction(owners, func(tx *sql.Tx) error {
		return mobileWriteDocumentChangesTx(tx, changes)
	})
}

func mobilePrepareDocumentChangesForPersist(changes *mobileDocumentChanges) {
	for i := range changes.drafts {
		changes.drafts[i] = mobilePrepareDraftForPersist(changes.drafts[i])
	}
	for i := range changes.uploads {
		changes.uploads[i] = mobilePrepareUploadForPersist(changes.uploads[i])
	}
}

func mobileWriteDocumentChangesTx(tx *sql.Tx, changes mobileDocumentChanges) error {
	for _, record := range changes.drafts {
		if err := mobileWriteDraftTx(tx, record); err != nil {
			return err
		}
	}
	for _, record := range changes.exports {
		if err := mobileWriteExportTx(tx, record); err != nil {
			return err
		}
	}
	for _, record := range changes.uploads {
		if err := mobileWriteUploadTx(tx, record); err != nil {
			return err
		}
	}
	for _, id := range changes.deletedDrafts {
		if _, err := tx.Exec(`DELETE FROM drafts WHERE id=?`, id); err != nil {
			return err
		}
	}
	for _, id := range changes.deletedUploads {
		if _, err := tx.Exec(`DELETE FROM uploads WHERE id=?`, id); err != nil {
			return err
		}
	}
	return nil
}

func mobilePrepareDraftForPersist(record mobileDocumentDraftRecord) mobileDocumentDraftRecord {
	stripped := mobileStripDraftBlobForPersist(record)
	mobileDocuments.Lock()
	if live, ok := mobileDocuments.drafts[record.ID]; ok && strings.TrimSpace(live.SourcePath) == "" && strings.TrimSpace(stripped.SourcePath) != "" {
		live.SourcePath = stripped.SourcePath
		if live.SourceSize == 0 {
			live.SourceSize = stripped.SourceSize
		}
		if len(live.SourceBytes) > mobileDocumentSourceHotCacheMax {
			live.SourceBytes = nil
		}
		mobileDocuments.drafts[record.ID] = live
	}
	mobileDocuments.Unlock()
	return stripped
}

func mobilePrepareUploadForPersist(record mobileDocumentUploadRecord) mobileDocumentUploadRecord {
	stripped := mobileStripUploadBlobForPersist(record)
	mobileDocuments.Lock()
	if live, ok := mobileDocuments.uploads[record.TaskID]; ok && strings.TrimSpace(live.SourcePath) == "" && strings.TrimSpace(stripped.SourcePath) != "" {
		live.SourcePath = stripped.SourcePath
		if live.SourceSize == 0 {
			live.SourceSize = stripped.SourceSize
		}
		if len(live.SourceBytes) > mobileDocumentSourceHotCacheMax {
			live.SourceBytes = nil
		}
		mobileDocuments.uploads[record.TaskID] = live
	}
	mobileDocuments.Unlock()
	return stripped
}

func mobilePersistDraft(record mobileDocumentDraftRecord) {
	mobilePersistDocumentChanges(mobileDocumentChanges{drafts: []mobileDocumentDraftRecord{record}})
}

func mobilePersistUpload(record mobileDocumentUploadRecord) {
	mobilePersistDocumentChanges(mobileDocumentChanges{uploads: []mobileDocumentUploadRecord{record}})
}

func mobilePersistExport(record mobileDocumentExportRecord) {
	mobilePersistDocumentChanges(mobileDocumentChanges{exports: []mobileDocumentExportRecord{record}})
}

func mobilePersistMeeting(record mobileMeetingRecording) {
	owners := []mobileStateOwner{{tenantID: record.TenantID, ownerID: record.OwnerID}}
	mobileRunOwnedStateTransaction(owners, func(tx *sql.Tx) error { return mobileWriteMeetingTx(tx, record) })
}

func mobilePersistMeetingWithDocuments(record mobileMeetingRecording, changes mobileDocumentChanges) {
	mobilePrepareDocumentChangesForPersist(&changes)
	owners := []mobileStateOwner{{tenantID: record.TenantID, ownerID: record.OwnerID}}
	mobileRunOwnedStateTransaction(owners, func(tx *sql.Tx) error {
		if err := mobileWriteMeetingTx(tx, record); err != nil {
			return err
		}
		return mobileWriteDocumentChangesTx(tx, changes)
	})
}

func mobileDeleteMeeting(id string) {
	mobileRunStateTransaction(func(tx *sql.Tx) error {
		_, err := tx.Exec(`DELETE FROM meeting_recordings WHERE id=?`, id)
		return err
	})
}

func mobileDeleteMeetingWithDocuments(id string, changes mobileDocumentChanges) {
	mobileRunStateTransaction(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM meeting_recordings WHERE id=?`, id); err != nil {
			return err
		}
		return mobileWriteDocumentChangesTx(tx, changes)
	})
}

func mobilePersistMeetings(records []mobileMeetingRecording) {
	owners := make([]mobileStateOwner, 0, len(records))
	for _, record := range records {
		owners = append(owners, mobileStateOwner{tenantID: record.TenantID, ownerID: record.OwnerID})
	}
	mobileRunOwnedStateTransaction(owners, func(tx *sql.Tx) error {
		for _, record := range records {
			if err := mobileWriteMeetingTx(tx, record); err != nil {
				return err
			}
		}
		return nil
	})
}

func mobilePersistDigitalTask(record mobileDigitalEmployeeTaskRecord) {
	owners := []mobileStateOwner{{tenantID: record.TenantID, ownerID: record.OwnerID}}
	mobileRunOwnedStateTransaction(owners, func(tx *sql.Tx) error { return mobileWriteDigitalTaskTx(tx, record) })
}

func mobilePersistPushUser(tenantID, ownerID string) {
	key := mobilePushUserKey(tenantID, ownerID)
	mobilePushState.Lock()
	devices := append([]mobilePushDevice(nil), mobilePushState.devices[key]...)
	pending := append([]mobilePushPendingItem(nil), mobilePushState.pending[key]...)
	mobilePushState.Unlock()
	owners := []mobileStateOwner{{tenantID: tenantID, ownerID: ownerID}}
	mobileRunOwnedStateTransaction(owners, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM push_devices WHERE tenant_id=? AND owner_id=?`, tenantID, ownerID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM push_pending WHERE tenant_id=? AND owner_id=?`, tenantID, ownerID); err != nil {
			return err
		}
		for _, record := range devices {
			if err := mobileWritePushDeviceTx(tx, tenantID, ownerID, record); err != nil {
				return err
			}
		}
		for _, record := range pending {
			if err := mobileWritePushPendingTx(tx, tenantID, ownerID, record); err != nil {
				return err
			}
		}
		return nil
	})
}

func mobilePersistServerProfiles(tenantID, ownerID, sourceMachineID string, replaceSource bool, records []mobileServerProfileRecord) {
	owners := []mobileStateOwner{{tenantID: tenantID, ownerID: ownerID}}
	mobileRunOwnedStateTransaction(owners, func(tx *sql.Tx) error {
		if replaceSource {
			if _, err := tx.Exec(`DELETE FROM server_profiles WHERE tenant_id=? AND owner_id=? AND source_machine_id=?`, tenantID, ownerID, sourceMachineID); err != nil {
				return err
			}
		}
		for _, record := range records {
			if err := mobileWriteServerProfileTx(tx, record); err != nil {
				return err
			}
		}
		return nil
	})
}

func mobilePersistSSHQuick(profile mobileServerProfileRecord, secret mobileSSHVaultRecord) {
	owners := []mobileStateOwner{{tenantID: secret.TenantID, ownerID: secret.OwnerID}}
	mobileRunOwnedStateTransaction(owners, func(tx *sql.Tx) error {
		if err := mobileWriteServerProfileTx(tx, profile); err != nil {
			return err
		}
		return mobileWriteSSHConfigTx(tx, secret)
	})
}

func mobilePersistSSHConfig(secret mobileSSHVaultRecord) {
	owners := []mobileStateOwner{{tenantID: secret.TenantID, ownerID: secret.OwnerID}}
	mobileRunOwnedStateTransaction(owners, func(tx *sql.Tx) error { return mobileWriteSSHConfigTx(tx, secret) })
}

func mobileDeleteSSHConfig(tenantID, ownerID, profileID string) {
	mobileRunStateTransaction(func(tx *sql.Tx) error {
		_, err := tx.Exec(`DELETE FROM ssh_configs WHERE tenant_id=? AND owner_id=? AND profile_id=?`, tenantID, ownerID, profileID)
		return err
	})
}

func mobilePersistPurgeLocked(tenantID string, ownerIDs map[string]struct{}) {
	db, fallback, err := mobileOpenStateDB(mobileStatePath())
	if err != nil || fallback {
		mobilePersistLegacyStateLocked()
		return
	}
	tx, err := db.Begin()
	if err != nil {
		log.Printf("[mobile-state] purge begin failed: %v", err)
		return
	}
	defer tx.Rollback()
	purgedAt := time.Now().UTC().Format(time.RFC3339Nano)
	for ownerID := range ownerIDs {
		for _, table := range mobileEntityTables {
			if _, err := tx.Exec(`DELETE FROM `+table+` WHERE tenant_id IN (?,?) AND owner_id=?`, tenantID, mobileMeetingRecordingTenantID(tenantID), ownerID); err != nil {
				log.Printf("[mobile-state] purge delete rolled back: %v", err)
				return
			}
		}
		tenants := []string{mobileMeetingRecordingTenantID(tenantID)}
		if raw := strings.TrimSpace(tenantID); raw != "" && raw != tenants[0] {
			tenants = append(tenants, raw)
		}
		for _, tenant := range tenants {
			if _, err := tx.Exec(`INSERT INTO purged_owners(tenant_id, owner_id, purged_at) VALUES(?,?,?)
				ON CONFLICT(tenant_id, owner_id) DO UPDATE SET purged_at=excluded.purged_at`, tenant, strings.TrimSpace(ownerID), purgedAt); err != nil {
				log.Printf("[mobile-state] purge tombstone rolled back: %v", err)
				return
			}
		}
	}
	if _, err := tx.Exec(`INSERT INTO mobile_state_meta(key,value) VALUES('initialized','1')
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`); err != nil {
		log.Printf("[mobile-state] purge meta rolled back: %v", err)
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("[mobile-state] purge commit failed: %v", err)
	}
}

func mobileStateRowPayloadForTest(db *sql.DB, table, tenantID, id string) (string, error) {
	for _, candidate := range mobileEntityTables {
		if candidate == table {
			var payload string
			var err error
			switch table {
			case "server_profiles", "ssh_configs", "push_devices", "push_pending":
				err = fmt.Errorf("composite-key table %s requires a direct SQL query", table)
			default:
				err = db.QueryRow(`SELECT payload FROM `+table+` WHERE tenant_id=? AND id=?`, tenantID, id).Scan(&payload)
			}
			return payload, err
		}
	}
	return "", fmt.Errorf("unknown mobile state table %q", table)
}
