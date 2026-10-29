package agentservice

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteStore persists every control-plane entity in its own table. Normal
// reads and writes query those tables directly; no process-wide JSON snapshot
// is kept as a read model. agentservice_state_migrated retains the legacy blob
// after a successful one-time import. If an old blob cannot be imported, the
// store rolls the import back and continues through the legacy blob path.
type SQLiteStore struct {
	mu     sync.RWMutex
	db     *sql.DB
	path   string
	closed bool
	legacy bool
}

const sqliteRowsFormatVersion = 2
const sqliteStoreStateVersion = 1

const sqliteRowsSchema = `
CREATE TABLE IF NOT EXISTS as_meta (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  format_version INTEGER NOT NULL,
  write_seq INTEGER NOT NULL,
  pruned_upto INTEGER NOT NULL DEFAULT 0,
  migrated_at TEXT NOT NULL DEFAULT '',
  scope_backfill INTEGER NOT NULL DEFAULT 0,
  hotpath_backfill INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS as_tenants (k TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, user_id TEXT NOT NULL, instance_id TEXT NOT NULL, v BLOB NOT NULL) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS as_users (k TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, user_id TEXT NOT NULL, instance_id TEXT NOT NULL, v BLOB NOT NULL) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS as_credentials (k TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, user_id TEXT NOT NULL, instance_id TEXT NOT NULL, credential_id TEXT NOT NULL DEFAULT '', v BLOB NOT NULL) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS as_user_configs (k TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, user_id TEXT NOT NULL, instance_id TEXT NOT NULL, v BLOB NOT NULL) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS as_instances (k TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, user_id TEXT NOT NULL, instance_id TEXT NOT NULL, v BLOB NOT NULL) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS as_sessions (k TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, user_id TEXT NOT NULL, instance_id TEXT NOT NULL, client_session_key TEXT NOT NULL DEFAULT '', v BLOB NOT NULL) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS as_runs (k TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, user_id TEXT NOT NULL, instance_id TEXT NOT NULL, session_id TEXT NOT NULL DEFAULT '', user_message_id TEXT NOT NULL DEFAULT '', v BLOB NOT NULL) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS as_messages (session_id TEXT NOT NULL, idx INTEGER NOT NULL, id TEXT NOT NULL, client_message_id TEXT NOT NULL DEFAULT '', v BLOB NOT NULL, PRIMARY KEY (session_id, idx)) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS as_run_events (scope TEXT NOT NULL, seq INTEGER NOT NULL, tenant_id TEXT NOT NULL DEFAULT '', user_id TEXT NOT NULL DEFAULT '', run_id TEXT NOT NULL DEFAULT '', id TEXT NOT NULL, v BLOB NOT NULL, PRIMARY KEY (scope, seq)) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS as_audit_events (idx INTEGER PRIMARY KEY, tenant_id TEXT NOT NULL, user_id TEXT NOT NULL, v BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS as_changes (wseq INTEGER NOT NULL, coll TEXT NOT NULL, k TEXT NOT NULL);
`

const sqliteRowsIndexes = `
CREATE INDEX IF NOT EXISTS as_users_scope ON as_users(tenant_id, user_id);
CREATE INDEX IF NOT EXISTS as_credentials_scope ON as_credentials(tenant_id, user_id);
CREATE INDEX IF NOT EXISTS as_credentials_id ON as_credentials(tenant_id, user_id, k);
CREATE INDEX IF NOT EXISTS as_credentials_entity ON as_credentials(credential_id);
CREATE INDEX IF NOT EXISTS as_instances_scope ON as_instances(tenant_id, user_id);
CREATE INDEX IF NOT EXISTS as_sessions_scope ON as_sessions(tenant_id, user_id, instance_id);
CREATE INDEX IF NOT EXISTS as_sessions_client ON as_sessions(tenant_id, user_id, instance_id, client_session_key);
CREATE INDEX IF NOT EXISTS as_runs_scope ON as_runs(tenant_id, user_id, instance_id);
CREATE INDEX IF NOT EXISTS as_runs_session ON as_runs(session_id);
CREATE INDEX IF NOT EXISTS as_runs_user_message ON as_runs(tenant_id, user_id, instance_id, user_message_id);
CREATE INDEX IF NOT EXISTS as_messages_id ON as_messages(session_id, id);
CREATE INDEX IF NOT EXISTS as_messages_client ON as_messages(session_id, client_message_id);
CREATE INDEX IF NOT EXISTS as_run_events_run ON as_run_events(tenant_id, user_id, run_id, seq);
CREATE INDEX IF NOT EXISTS as_run_events_id ON as_run_events(scope, id);
CREATE INDEX IF NOT EXISTS as_audit_scope ON as_audit_events(tenant_id, user_id);
CREATE INDEX IF NOT EXISTS as_changes_wseq ON as_changes(wseq);
`

// Kept for existing benchmarks and migration diagnostics. Direct stores do not
// perform full cache loads or cache/row verification on each write.
var sqliteFullLoads atomic.Int64
var sqliteVerifyWrites = os.Getenv("MACLAW_AGENTSERVICE_SQLITE_VERIFY_WRITES") == "1"

type sqlq interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// mapColl adapts one map-backed MemoryStore collection to its table for legacy
// import, migration verification, and detached snapshots.
type mapColl struct {
	coll, table string
	keys        func(*MemoryStore) []string
	encode      func(*MemoryStore, string) (v []byte, t, u, i string, ok bool, err error)
	set         func(*MemoryStore, string, []byte) error
	del         func(*MemoryStore, string)
	reset       func(*MemoryStore)
}

func newMapColl[T any](coll, table string, get func(*MemoryStore) *map[string]T, scope func(T) (string, string, string)) mapColl {
	return mapColl{
		coll: coll, table: table,
		keys: func(m *MemoryStore) []string {
			mp := *get(m)
			out := make([]string, 0, len(mp))
			for k := range mp {
				out = append(out, k)
			}
			sort.Strings(out)
			return out
		},
		encode: func(m *MemoryStore, k string) ([]byte, string, string, string, bool, error) {
			v, ok := (*get(m))[k]
			if !ok {
				return nil, "", "", "", false, nil
			}
			b, err := json.Marshal(v)
			t, u, i := scope(v)
			return b, t, u, i, true, err
		},
		set: func(m *MemoryStore, k string, b []byte) error {
			var v T
			if err := json.Unmarshal(b, &v); err != nil {
				return fmt.Errorf("%s %q: %w", coll, k, err)
			}
			(*get(m))[k] = v
			return nil
		},
		del:   func(m *MemoryStore, k string) { delete(*get(m), k) },
		reset: func(m *MemoryStore) { *get(m) = map[string]T{} },
	}
}

var sqliteMapColls = []mapColl{
	newMapColl(collTenants, "as_tenants", func(m *MemoryStore) *map[string]Tenant { return &m.tenants }, func(v Tenant) (string, string, string) { return v.ID, "", "" }),
	newMapColl(collUsers, "as_users", func(m *MemoryStore) *map[string]User { return &m.users }, func(v User) (string, string, string) { return v.TenantID, v.ID, "" }),
	newMapColl(collCredentials, "as_credentials", func(m *MemoryStore) *map[string]Credential { return &m.credentials }, func(v Credential) (string, string, string) { return v.TenantID, v.UserID, "" }),
	newMapColl(collUserConfigs, "as_user_configs", func(m *MemoryStore) *map[string]UserConfig { return &m.userConfigs }, func(v UserConfig) (string, string, string) { return v.TenantID, v.UserID, "" }),
	newMapColl(collInstances, "as_instances", func(m *MemoryStore) *map[string]Instance { return &m.instances }, func(v Instance) (string, string, string) { return v.TenantID, v.UserID, v.ID }),
	newMapColl(collSessions, "as_sessions", func(m *MemoryStore) *map[string]Session { return &m.sessions }, func(v Session) (string, string, string) { return v.TenantID, v.UserID, v.InstanceID }),
	newMapColl(collRuns, "as_runs", func(m *MemoryStore) *map[string]Run { return &m.runs }, func(v Run) (string, string, string) { return v.TenantID, v.UserID, v.InstanceID }),
}

func NewSQLiteStore(path string) (*SQLiteStore, error) {
	if err := secureMkdirAll(filepath.Dir(path)); err != nil {
		return nil, err
	}
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(0)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	store := &SQLiteStore{db: db, path: path}
	if err := store.open(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) open() error {
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, `PRAGMA journal_mode=WAL;`); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, sqliteRowsSchema); err != nil {
		return err
	}
	if err := s.ensureRowColumns(ctx); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, sqliteRowsIndexes); err != nil {
		return err
	}
	var format int
	err := s.db.QueryRowContext(ctx, `SELECT format_version FROM as_meta WHERE id=1`).Scan(&format)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if err := s.initializeRows(ctx); err != nil {
			legacyErr := s.enableLegacyFallback(ctx)
			if legacyErr == nil {
				s.legacy = true
				log.Printf("[agentservice] normalized SQLite migration failed; using preserved legacy state: %v", err)
				return nil
			}
			return fmt.Errorf("migrate agentservice state: %w (legacy fallback: %v)", err, legacyErr)
		}
	case err != nil:
		return err
	case format != sqliteRowsFormatVersion:
		return fmt.Errorf("unsupported agentservice SQLite format version %d", format)
	}
	return nil
}

// ensureRowColumns additively upgrades the earlier row format. Payload remains
// authoritative for fields not needed by indexes.
func (s *SQLiteStore) ensureRowColumns(ctx context.Context) error {
	var scopeBackfill, hotpathBackfill int
	err := s.db.QueryRowContext(ctx, `SELECT scope_backfill, hotpath_backfill FROM as_meta WHERE id=1`).Scan(&scopeBackfill, &hotpathBackfill)
	if err == nil && scopeBackfill == 1 && hotpathBackfill == 1 {
		return nil
	}
	for _, stmt := range []string{
		`ALTER TABLE as_runs ADD COLUMN session_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE as_runs ADD COLUMN user_message_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE as_run_events ADD COLUMN tenant_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE as_run_events ADD COLUMN user_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE as_run_events ADD COLUMN run_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE as_meta ADD COLUMN scope_backfill INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE as_meta ADD COLUMN hotpath_backfill INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE as_credentials ADD COLUMN credential_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE as_sessions ADD COLUMN client_session_key TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE as_messages ADD COLUMN client_message_id TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	var backfilled int
	err = s.db.QueryRowContext(ctx, `SELECT scope_backfill FROM as_meta WHERE id=1`).Scan(&backfilled)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && backfilled == 1) {
		return s.ensureHotpathBackfill(ctx)
	}
	if err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT k, v FROM as_runs WHERE session_id='' OR user_message_id=''`)
	if err != nil {
		return err
	}
	type runPatch struct {
		key, session, message string
	}
	var patches []runPatch
	for rows.Next() {
		var key string
		var payload []byte
		if err := rows.Scan(&key, &payload); err != nil {
			rows.Close()
			return err
		}
		var run Run
		if err := json.Unmarshal(payload, &run); err != nil {
			rows.Close()
			return err
		}
		patches = append(patches, runPatch{key, run.SessionID, run.UserMessageID})
	}
	if err := closeRows(rows); err != nil {
		return err
	}
	for _, patch := range patches {
		if _, err := s.db.ExecContext(ctx, `UPDATE as_runs SET session_id=?, user_message_id=? WHERE k=?`, patch.session, patch.message, patch.key); err != nil {
			return err
		}
	}
	rows, err = s.db.QueryContext(ctx, `SELECT scope, seq, v FROM as_run_events WHERE tenant_id='' OR user_id='' OR run_id=''`)
	if err != nil {
		return err
	}
	type eventPatch struct {
		scope               string
		seq                 uint64
		tenant, user, runID string
	}
	var eventPatches []eventPatch
	for rows.Next() {
		var scope string
		var seq uint64
		var payload []byte
		if err := rows.Scan(&scope, &seq, &payload); err != nil {
			rows.Close()
			return err
		}
		var event RunEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			rows.Close()
			return err
		}
		eventPatches = append(eventPatches, eventPatch{scope, seq, event.TenantID, event.UserID, event.RunID})
	}
	if err := closeRows(rows); err != nil {
		return err
	}
	for _, patch := range eventPatches {
		if _, err := s.db.ExecContext(ctx, `UPDATE as_run_events SET tenant_id=?, user_id=?, run_id=? WHERE scope=? AND seq=?`, patch.tenant, patch.user, patch.runID, patch.scope, patch.seq); err != nil {
			return err
		}
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE as_meta SET scope_backfill=1 WHERE id=1`); err != nil {
		return err
	}
	return s.ensureHotpathBackfill(ctx)
}

// ensureHotpathBackfill copies lookup fields out of JSON once. Later saves
// keep the columns current, so identity checks do not reread every payload.
func (s *SQLiteStore) ensureHotpathBackfill(ctx context.Context) error {
	var done int
	err := s.db.QueryRowContext(ctx, `SELECT hotpath_backfill FROM as_meta WHERE id=1`).Scan(&done)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && done == 1) {
		return nil
	}
	if err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT k, v FROM as_credentials WHERE credential_id=''`)
	if err != nil {
		return err
	}
	type credPatch struct{ key, id string }
	var creds []credPatch
	for rows.Next() {
		var key string
		var payload []byte
		if err := rows.Scan(&key, &payload); err != nil {
			rows.Close()
			return err
		}
		var cred Credential
		if err := json.Unmarshal(payload, &cred); err != nil {
			rows.Close()
			return err
		}
		if cred.ID != "" {
			creds = append(creds, credPatch{key, cred.ID})
		}
	}
	if err := closeRows(rows); err != nil {
		return err
	}
	for _, patch := range creds {
		if _, err := s.db.ExecContext(ctx, `UPDATE as_credentials SET credential_id=? WHERE k=?`, patch.id, patch.key); err != nil {
			return err
		}
	}
	rows, err = s.db.QueryContext(ctx, `SELECT k, v FROM as_sessions WHERE client_session_key=''`)
	if err != nil {
		return err
	}
	type sessionPatch struct{ key, clientKey string }
	var sessions []sessionPatch
	for rows.Next() {
		var key string
		var payload []byte
		if err := rows.Scan(&key, &payload); err != nil {
			rows.Close()
			return err
		}
		var session Session
		if err := json.Unmarshal(payload, &session); err != nil {
			rows.Close()
			return err
		}
		if clientKey := sessionClientKey(session); clientKey != "" {
			sessions = append(sessions, sessionPatch{key, clientKey})
		}
	}
	if err := closeRows(rows); err != nil {
		return err
	}
	for _, patch := range sessions {
		if _, err := s.db.ExecContext(ctx, `UPDATE as_sessions SET client_session_key=? WHERE k=?`, patch.clientKey, patch.key); err != nil {
			return err
		}
	}
	rows, err = s.db.QueryContext(ctx, `SELECT session_id, idx, v FROM as_messages WHERE client_message_id=''`)
	if err != nil {
		return err
	}
	type messagePatch struct {
		sessionID string
		idx       int64
		clientID  string
	}
	var messages []messagePatch
	for rows.Next() {
		var patch messagePatch
		var payload []byte
		if err := rows.Scan(&patch.sessionID, &patch.idx, &payload); err != nil {
			rows.Close()
			return err
		}
		var message Message
		if err := json.Unmarshal(payload, &message); err != nil {
			rows.Close()
			return err
		}
		if clientID := messageClientID(message); clientID != "" {
			patch.clientID = clientID
			messages = append(messages, patch)
		}
	}
	if err := closeRows(rows); err != nil {
		return err
	}
	for _, patch := range messages {
		if _, err := s.db.ExecContext(ctx, `UPDATE as_messages SET client_message_id=? WHERE session_id=? AND idx=?`, patch.clientID, patch.sessionID, patch.idx); err != nil {
			return err
		}
	}
	_, err = s.db.ExecContext(ctx, `UPDATE as_meta SET hotpath_backfill=1 WHERE id=1`)
	return err
}

// initializeRows imports a legacy blob (or historical store.json) and verifies
// a complete row round trip before atomically publishing as_meta. The original
// table is renamed, never dropped. Any error rolls back every inserted row.
func (s *SQLiteStore) initializeRows(ctx context.Context) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, `ROLLBACK`)
		}
	}()
	var exists int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM as_meta`).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		if _, err := conn.ExecContext(ctx, `ROLLBACK`); err != nil {
			return err
		}
		committed = true
		return nil
	}
	empty, err := normalizedRowsEmpty(ctx, conn)
	if err != nil {
		return err
	}
	if !empty {
		return errors.New("normalized tables are not empty but as_meta is missing")
	}
	source := NewMemoryStore()
	hasBlob := false
	var blobTable int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='agentservice_state'`).Scan(&blobTable); err != nil {
		return err
	}
	if blobTable > 0 {
		var version int
		var payload []byte
		err := conn.QueryRowContext(ctx, `SELECT schema_version, state_json FROM agentservice_state WHERE id=1`).Scan(&version, &payload)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return fmt.Errorf("read legacy agentservice_state: %w", err)
		case version != sqliteStoreStateVersion:
			return fmt.Errorf("unsupported legacy agentservice SQLite state schema version %d", version)
		default:
			if source, err = memoryStoreFromJSON(payload); err != nil {
				return fmt.Errorf("parse legacy agentservice_state: %w", err)
			}
		}
		hasBlob = true
	} else {
		legacy, readErr := os.ReadFile(filepath.Join(filepath.Dir(s.path), "store.json"))
		if readErr == nil && len(legacy) > 0 {
			if source, err = memoryStoreFromJSON(legacy); err != nil {
				return fmt.Errorf("parse legacy store.json: %w", err)
			}
		}
	}
	if err := insertAllRows(ctx, conn, source); err != nil {
		return fmt.Errorf("write normalized rows: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO as_meta(id, format_version, write_seq, pruned_upto, migrated_at, scope_backfill, hotpath_backfill) VALUES(1, ?, 1, 1, ?, 1, 1)`, sqliteRowsFormatVersion, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if err := verifyRowsMatch(ctx, conn, source); err != nil {
		return fmt.Errorf("migration verification failed: %w", err)
	}
	if hasBlob {
		if _, err := conn.ExecContext(ctx, `ALTER TABLE agentservice_state RENAME TO agentservice_state_migrated`); err != nil {
			return fmt.Errorf("preserve legacy state backup: %w", err)
		}
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return err
	}
	committed = true
	if hasBlob {
		log.Printf("[agentservice] migrated agentservice_state to normalized rows (%s); original preserved in agentservice_state_migrated", rowCountsString(source))
	}
	return nil
}

func normalizedRowsEmpty(ctx context.Context, q sqlq) (bool, error) {
	tables := []string{"as_messages", "as_run_events", "as_audit_events"}
	for _, c := range sqliteMapColls {
		tables = append(tables, c.table)
	}
	for _, table := range tables {
		var n int
		if err := q.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil {
			return false, err
		}
		if n != 0 {
			return false, nil
		}
	}
	return true, nil
}

func (s *SQLiteStore) enableLegacyFallback(ctx context.Context) error {
	var table int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='agentservice_state'`).Scan(&table); err != nil {
		return err
	}
	if table == 0 {
		return errors.New("legacy agentservice_state is unavailable")
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE agentservice_state ADD COLUMN write_seq INTEGER NOT NULL DEFAULT 0`); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return err
	}
	_, err := loadLegacyMemory(ctx, s.db)
	return err
}

func loadLegacyMemory(ctx context.Context, q sqlq) (*MemoryStore, error) {
	var version int
	var payload []byte
	if err := q.QueryRowContext(ctx, `SELECT schema_version, state_json FROM agentservice_state WHERE id=1`).Scan(&version, &payload); err != nil {
		return nil, err
	}
	if version != sqliteStoreStateVersion {
		return nil, fmt.Errorf("unsupported agentservice SQLite state schema version %d", version)
	}
	return memoryStoreFromJSON(payload)
}

func memoryStoreFromJSON(payload []byte) (*MemoryStore, error) {
	var state storeState
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &state); err != nil {
			return nil, err
		}
	}
	return newMemoryStoreFromState(state), nil
}

func canonicalStateJSON(m *MemoryStore) ([]byte, error) {
	st := m.snapshot()
	for k, v := range st.Messages {
		if len(v) == 0 {
			delete(st.Messages, k)
		}
	}
	for k, v := range st.RunEvents {
		if len(v) == 0 {
			delete(st.RunEvents, k)
		}
	}
	return json.Marshal(st)
}

func rowCounts(m *MemoryStore) map[string]int {
	out := map[string]int{
		collTenants: len(m.tenants), collUsers: len(m.users), collCredentials: len(m.credentials),
		collUserConfigs: len(m.userConfigs), collInstances: len(m.instances), collSessions: len(m.sessions),
		collRuns: len(m.runs), collAudit: len(m.auditEvents),
	}
	for _, v := range m.messages {
		out[collMessages] += len(v)
	}
	for _, v := range m.runEvents {
		out[collRunEvents] += len(v)
	}
	return out
}

func rowCountsString(m *MemoryStore) string {
	c := rowCounts(m)
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+strconv.Itoa(c[k]))
	}
	return strings.Join(parts, " ")
}

func verifyRowsMatch(ctx context.Context, q sqlq, source *MemoryStore) error {
	want := rowCounts(source)
	tables := map[string]string{collMessages: "as_messages", collRunEvents: "as_run_events", collAudit: "as_audit_events"}
	for _, c := range sqliteMapColls {
		tables[c.coll] = c.table
	}
	for coll, table := range tables {
		var n int
		if err := q.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil {
			return err
		}
		if n != want[coll] {
			return fmt.Errorf("%s: %d rows, want %d", coll, n, want[coll])
		}
	}
	got, err := loadAllRows(ctx, q)
	if err != nil {
		return err
	}
	a, err := canonicalStateJSON(source)
	if err != nil {
		return err
	}
	b, err := canonicalStateJSON(got)
	if err != nil {
		return err
	}
	if !bytes.Equal(a, b) {
		return fmt.Errorf("round trip differs (%d vs %d bytes)", len(a), len(b))
	}
	return nil
}

func insertAllRows(ctx context.Context, q sqlq, m *MemoryStore) error {
	for _, c := range sqliteMapColls {
		for _, k := range c.keys(m) {
			if err := upsertMapRow(ctx, q, c, m, k, false); err != nil {
				return err
			}
		}
	}
	for sid, items := range m.messages {
		if err := insertMessages(ctx, q, sid, items, 0); err != nil {
			return err
		}
	}
	for scope, items := range m.runEvents {
		for i := range items {
			if err := insertRunEvent(ctx, q, scope, &items[i], false); err != nil {
				return err
			}
		}
	}
	return insertAudit(ctx, q, m.auditEvents, 0)
}

func upsertMapRow(ctx context.Context, q sqlq, c mapColl, m *MemoryStore, k string, replace bool) error {
	b, t, u, i, ok, err := c.encode(m, k)
	if err != nil {
		return err
	}
	if !ok {
		_, err = q.ExecContext(ctx, `DELETE FROM `+c.table+` WHERE k=?`, k)
		return err
	}
	verb := "INSERT"
	if replace {
		verb = "INSERT OR REPLACE"
	}
	switch c.coll {
	case collRuns:
		run := m.runs[k]
		_, err = q.ExecContext(ctx, verb+` INTO as_runs(k, tenant_id, user_id, instance_id, session_id, user_message_id, v) VALUES(?,?,?,?,?,?,?)`, k, t, u, i, run.SessionID, run.UserMessageID, b)
	case collCredentials:
		_, err = q.ExecContext(ctx, verb+` INTO as_credentials(k, tenant_id, user_id, instance_id, credential_id, v) VALUES(?,?,?,?,?,?)`, k, t, u, i, m.credentials[k].ID, b)
	case collSessions:
		_, err = q.ExecContext(ctx, verb+` INTO as_sessions(k, tenant_id, user_id, instance_id, client_session_key, v) VALUES(?,?,?,?,?,?)`, k, t, u, i, sessionClientKey(m.sessions[k]), b)
	default:
		_, err = q.ExecContext(ctx, verb+` INTO `+c.table+`(k, tenant_id, user_id, instance_id, v) VALUES(?,?,?,?,?)`, k, t, u, i, b)
	}
	return err
}

func insertMessages(ctx context.Context, q sqlq, sid string, items []Message, from int) error {
	for idx := from; idx < len(items); idx++ {
		b, err := json.Marshal(items[idx])
		if err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO as_messages(session_id, idx, id, client_message_id, v) VALUES(?,?,?,?,?)`, sid, idx, items[idx].ID, messageClientID(items[idx]), b); err != nil {
			return err
		}
		var norm Message
		if err := json.Unmarshal(b, &norm); err != nil {
			return err
		}
		items[idx] = norm
	}
	return nil
}

func insertRunEvent(ctx context.Context, q sqlq, scope string, ev *RunEvent, replace bool) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	verb := "INSERT"
	if replace {
		verb = "INSERT OR REPLACE"
	}
	if _, err = q.ExecContext(ctx, verb+` INTO as_run_events(scope, seq, tenant_id, user_id, run_id, id, v) VALUES(?,?,?,?,?,?,?)`, scope, ev.Sequence, ev.TenantID, ev.UserID, ev.RunID, ev.ID, b); err != nil {
		return err
	}
	var norm RunEvent
	if err := json.Unmarshal(b, &norm); err != nil {
		return err
	}
	*ev = norm
	return nil
}

func insertAudit(ctx context.Context, q sqlq, items []AuditEvent, from int) error {
	for idx := from; idx < len(items); idx++ {
		b, err := json.Marshal(items[idx])
		if err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO as_audit_events(idx, tenant_id, user_id, v) VALUES(?,?,?,?)`, idx, items[idx].TenantID, items[idx].UserID, b); err != nil {
			return err
		}
		var norm AuditEvent
		if err := json.Unmarshal(b, &norm); err != nil {
			return err
		}
		items[idx] = norm
	}
	return nil
}

func loadAllRows(ctx context.Context, q sqlq) (*MemoryStore, error) {
	m := NewMemoryStore()
	for _, c := range sqliteMapColls {
		rows, err := q.QueryContext(ctx, `SELECT k, v FROM `+c.table)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var k string
			var b []byte
			if err := rows.Scan(&k, &b); err != nil {
				rows.Close()
				return nil, err
			}
			if err := c.set(m, k, b); err != nil {
				rows.Close()
				return nil, err
			}
		}
		if err := closeRows(rows); err != nil {
			return nil, err
		}
	}
	if err := scanJSONRows(ctx, q, `SELECT session_id, v FROM as_messages ORDER BY session_id, idx`, func(k string, b []byte) error {
		var v Message
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		m.messages[k] = append(m.messages[k], v)
		return nil
	}); err != nil {
		return nil, err
	}
	if err := scanJSONRows(ctx, q, `SELECT scope, v FROM as_run_events ORDER BY scope, seq`, func(k string, b []byte) error {
		var v RunEvent
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		m.runEvents[k] = append(m.runEvents[k], v)
		return nil
	}); err != nil {
		return nil, err
	}
	if err := scanJSONRows(ctx, q, `SELECT '', v FROM as_audit_events ORDER BY idx`, func(_ string, b []byte) error {
		var v AuditEvent
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		m.auditEvents = append(m.auditEvents, v)
		return nil
	}); err != nil {
		return nil, err
	}
	return m, nil
}

func scanJSONRows(ctx context.Context, q sqlq, query string, fn func(string, []byte) error, args ...any) error {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var k string
		var b []byte
		if err := rows.Scan(&k, &b); err != nil {
			rows.Close()
			return err
		}
		if err := fn(k, b); err != nil {
			rows.Close()
			return err
		}
	}
	return closeRows(rows)
}

func closeRows(rows *sql.Rows) error {
	err := rows.Err()
	if cerr := rows.Close(); err == nil {
		err = cerr
	}
	return err
}

func (s *SQLiteStore) ensureOpenLocked() error {
	if s == nil || s.db == nil || s.closed {
		return ErrServiceClosed
	}
	return nil
}

func (s *SQLiteStore) read(rowsFn func(context.Context, sqlq) error, legacyFn func(*MemoryStore) error) error {
	if s == nil {
		return ErrServiceClosed
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := s.ensureOpenLocked(); err != nil {
		return err
	}
	ctx := context.Background()
	if s.legacy {
		m, err := loadLegacyMemory(ctx, s.db)
		if err != nil {
			return err
		}
		return legacyFn(m)
	}
	return rowsFn(ctx, s.db)
}

func (s *SQLiteStore) write(rowsFn func(context.Context, sqlq) error, legacyFn func(*MemoryStore) error) error {
	if s == nil {
		return ErrServiceClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureOpenLocked(); err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		var err error
		if s.legacy {
			err = s.writeLegacyOnce(legacyFn)
		} else {
			err = s.writeRowsOnce(rowsFn)
		}
		if !isSQLiteBusy(err) || attempt >= 6 {
			return err
		}
		time.Sleep(time.Duration(10*(1<<attempt)) * time.Millisecond)
	}
}

func (s *SQLiteStore) writeRowsOnce(fn func(context.Context, sqlq) error) error {
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, `ROLLBACK`)
		}
	}()
	if err := fn(ctx, conn); err != nil {
		return err
	}
	var next int64
	if err := conn.QueryRowContext(ctx, `UPDATE as_meta SET write_seq=write_seq+1 WHERE id=1 RETURNING write_seq`).Scan(&next); err != nil {
		return err
	}
	// A wildcard keeps older row-cache binaries coherent during rolling updates.
	if _, err := conn.ExecContext(ctx, `INSERT INTO as_changes(wseq, coll, k) VALUES(?, '*', '')`, next); err != nil {
		return err
	}
	if next%1000 == 0 && next > sqliteChangesKeep {
		cut := next - sqliteChangesKeep
		if _, err := conn.ExecContext(ctx, `DELETE FROM as_changes WHERE wseq <= ?`, cut); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, `UPDATE as_meta SET pruned_upto=? WHERE id=1`, cut); err != nil {
			return err
		}
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) writeLegacyOnce(fn func(*MemoryStore) error) error {
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, `ROLLBACK`)
		}
	}()
	working, err := loadLegacyMemory(ctx, conn)
	if err != nil {
		return err
	}
	if err := fn(working); err != nil {
		return err
	}
	payload, err := json.Marshal(working.snapshot())
	if err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `UPDATE agentservice_state SET state_json=?, updated_at=?, write_seq=write_seq+1 WHERE id=1`, payload, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return err
	}
	committed = true
	return nil
}

const sqliteChangesKeep = 5000

func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "database is locked") || strings.Contains(text, "sqlite_busy") || strings.Contains(text, "database table is locked")
}

func firstStateDiff(a, b *MemoryStore) string {
	var ma, mb map[string]json.RawMessage
	x, _ := canonicalStateJSON(a)
	y, _ := canonicalStateJSON(b)
	_ = json.Unmarshal(x, &ma)
	_ = json.Unmarshal(y, &mb)
	for k := range ma {
		if !bytes.Equal(ma[k], mb[k]) {
			return k + ": " + string(ma[k]) + " VS " + string(mb[k])
		}
	}
	for k := range mb {
		if _, ok := ma[k]; !ok {
			return k + " only in rows"
		}
	}
	return ""
}
