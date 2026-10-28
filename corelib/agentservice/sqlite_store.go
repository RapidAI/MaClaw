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

// SQLiteStore persists the agentservice control plane as normalized rows:
// one table per entity collection (tenants, users, credentials, user configs,
// instances, sessions, runs), plus per-row message, run-event and audit
// tables. An in-memory MemoryStore is kept as the read model; it is loaded
// once and then kept coherent with per-row catch-up driven by as_changes.
//
//   - Reads take a shared lock and only probe as_meta.write_seq.
//   - Writes run BEGIN IMMEDIATE, catch up, run the mutation on the cache with
//     dirty tracking, and write only the touched rows plus a change record.
//
// The legacy single-row agentservice_state blob is migrated once on first
// start, verified (counts + full round trip), and then dropped.
type SQLiteStore struct {
	mu     sync.RWMutex
	db     *sql.DB
	path   string
	inner  *MemoryStore
	closed bool
	seq    int64 // as_meta.write_seq the cache reflects
}

const sqliteRowsFormatVersion = 2

const sqliteRowsSchema = `
CREATE TABLE IF NOT EXISTS as_meta (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  format_version INTEGER NOT NULL,
  write_seq INTEGER NOT NULL,
  pruned_upto INTEGER NOT NULL DEFAULT 0,
  migrated_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS as_tenants (k TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, user_id TEXT NOT NULL, instance_id TEXT NOT NULL, v BLOB NOT NULL) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS as_users (k TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, user_id TEXT NOT NULL, instance_id TEXT NOT NULL, v BLOB NOT NULL) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS as_credentials (k TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, user_id TEXT NOT NULL, instance_id TEXT NOT NULL, v BLOB NOT NULL) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS as_user_configs (k TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, user_id TEXT NOT NULL, instance_id TEXT NOT NULL, v BLOB NOT NULL) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS as_instances (k TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, user_id TEXT NOT NULL, instance_id TEXT NOT NULL, v BLOB NOT NULL) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS as_sessions (k TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, user_id TEXT NOT NULL, instance_id TEXT NOT NULL, v BLOB NOT NULL) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS as_runs (k TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, user_id TEXT NOT NULL, instance_id TEXT NOT NULL, v BLOB NOT NULL) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS as_users_scope ON as_users(tenant_id, user_id);
CREATE INDEX IF NOT EXISTS as_credentials_scope ON as_credentials(tenant_id, user_id);
CREATE INDEX IF NOT EXISTS as_instances_scope ON as_instances(tenant_id, user_id);
CREATE INDEX IF NOT EXISTS as_sessions_scope ON as_sessions(tenant_id, user_id, instance_id);
CREATE INDEX IF NOT EXISTS as_runs_scope ON as_runs(tenant_id, user_id, instance_id);
CREATE TABLE IF NOT EXISTS as_messages (session_id TEXT NOT NULL, idx INTEGER NOT NULL, id TEXT NOT NULL, v BLOB NOT NULL, PRIMARY KEY (session_id, idx)) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS as_run_events (scope TEXT NOT NULL, seq INTEGER NOT NULL, id TEXT NOT NULL, v BLOB NOT NULL, PRIMARY KEY (scope, seq)) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS as_audit_events (idx INTEGER PRIMARY KEY, tenant_id TEXT NOT NULL, user_id TEXT NOT NULL, v BLOB NOT NULL);
CREATE INDEX IF NOT EXISTS as_audit_scope ON as_audit_events(tenant_id, user_id);
CREATE TABLE IF NOT EXISTS as_changes (wseq INTEGER NOT NULL, coll TEXT NOT NULL, k TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS as_changes_wseq ON as_changes(wseq);
`

// sqliteFullLoads counts full cache loads from rows (tests).
var sqliteFullLoads atomic.Int64

type sqlq interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// mapColl adapts one map-backed MemoryStore collection to its table.
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

func mapCollByName(name string) (mapColl, bool) {
	for _, c := range sqliteMapColls {
		if c.coll == name {
			return c, true
		}
	}
	return mapColl{}, false
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
	var format int
	err := s.db.QueryRowContext(ctx, `SELECT format_version FROM as_meta WHERE id=1`).Scan(&format)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		vacuum, err := s.initializeRows(ctx)
		if err != nil {
			return err
		}
		if vacuum {
			// Outside the migration transaction; failure only leaves free pages.
			if _, err := s.db.ExecContext(ctx, `VACUUM`); err != nil {
				log.Printf("[agentservice] sqlite VACUUM after blob migration failed: %v", err)
			}
		}
	case err != nil:
		return err
	case format != sqliteRowsFormatVersion:
		return fmt.Errorf("unsupported agentservice SQLite format version %d", format)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fullLoadLocked(s.db)
}

// initializeRows creates as_meta for a new database. A legacy
// agentservice_state blob (or, for a brand new DB, the historical
// state/store.json) is imported, verified and — for the blob — dropped, all
// in one transaction. Any failure aborts startup with nothing deleted.
func (s *SQLiteStore) initializeRows(ctx context.Context) (vacuum bool, err error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return false, err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, `ROLLBACK`)
		}
	}()
	var exists int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM as_meta`).Scan(&exists); err != nil {
		return false, err
	}
	if exists > 0 { // another process won the race
		return false, nil
	}
	source := NewMemoryStore()
	hasBlob := false
	var blobTable int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='agentservice_state'`).Scan(&blobTable); err != nil {
		return false, err
	}
	if blobTable > 0 {
		var version int
		var payload []byte
		err := conn.QueryRowContext(ctx, `SELECT schema_version, state_json FROM agentservice_state WHERE id=1`).Scan(&version, &payload)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return false, fmt.Errorf("read legacy agentservice_state: %w", err)
		case version != 1:
			return false, fmt.Errorf("unsupported legacy agentservice SQLite state schema version %d", version)
		default:
			if source, err = memoryStoreFromJSON(payload); err != nil {
				return false, fmt.Errorf("parse legacy agentservice_state: %w", err)
			}
		}
		hasBlob = true
	} else {
		// MaClawSrv historically persisted the control plane as state/store.json.
		// Import it once into a new database; the file is retained as a backup.
		legacy, readErr := os.ReadFile(filepath.Join(filepath.Dir(s.path), "store.json"))
		if readErr == nil && len(legacy) > 0 {
			if source, err = memoryStoreFromJSON(legacy); err != nil {
				return false, fmt.Errorf("parse legacy store.json: %w", err)
			}
		}
	}
	if err := insertAllRows(ctx, conn, source); err != nil {
		return false, fmt.Errorf("migrate agentservice state to rows: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO as_meta(id, format_version, write_seq, pruned_upto, migrated_at) VALUES(1, ?, 1, 1, ?)`, sqliteRowsFormatVersion, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return false, err
	}
	if err := verifyRowsMatch(ctx, conn, source); err != nil {
		return false, fmt.Errorf("agentservice migration verification failed (nothing deleted): %w", err)
	}
	if hasBlob {
		if _, err := conn.ExecContext(ctx, `DROP TABLE agentservice_state`); err != nil {
			return false, err
		}
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return false, err
	}
	committed = true
	if hasBlob {
		log.Printf("[agentservice] migrated agentservice_state blob to normalized rows (%s); blob dropped", rowCountsString(source))
	}
	return hasBlob, nil
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

// canonicalStateJSON is the comparison form for migration verification:
// MemoryStore snapshot with empty per-session/per-run slices dropped (rows
// cannot represent "present but empty").
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

// verifyRowsMatch checks per-table row counts in SQL and a full round trip
// (rows -> MemoryStore -> canonical JSON) against the source.
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

func deleteAllRows(ctx context.Context, q sqlq) error {
	for _, c := range sqliteMapColls {
		if _, err := q.ExecContext(ctx, `DELETE FROM `+c.table); err != nil {
			return err
		}
	}
	for _, t := range []string{"as_messages", "as_run_events", "as_audit_events"} {
		if _, err := q.ExecContext(ctx, `DELETE FROM `+t); err != nil {
			return err
		}
	}
	return nil
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
	if _, err = q.ExecContext(ctx, verb+` INTO `+c.table+`(k, tenant_id, user_id, instance_id, v) VALUES(?,?,?,?,?)`, k, t, u, i, b); err != nil {
		return err
	}
	// Keep the cache identical to what a reload would produce (the blob
	// store always served JSON-round-tripped values).
	return c.set(m, k, b)
}

func insertMessages(ctx context.Context, q sqlq, sid string, items []Message, from int) error {
	for idx := from; idx < len(items); idx++ {
		b, err := json.Marshal(items[idx])
		if err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO as_messages(session_id, idx, id, v) VALUES(?,?,?,?)`, sid, idx, items[idx].ID, b); err != nil {
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
	if _, err = q.ExecContext(ctx, verb+` INTO as_run_events(scope, seq, id, v) VALUES(?,?,?,?)`, scope, int64(ev.Sequence), ev.ID, b); err != nil {
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

func (s *SQLiteStore) fullLoadLocked(q sqlq) error {
	ctx := context.Background()
	var seq int64
	if err := q.QueryRowContext(ctx, `SELECT write_seq FROM as_meta WHERE id=1`).Scan(&seq); err != nil {
		return err
	}
	m, err := loadAllRows(ctx, q)
	if err != nil {
		return err
	}
	s.inner = m
	s.seq = seq
	sqliteFullLoads.Add(1)
	return nil
}

// catchUpLocked applies other handles' committed changes, row by row.
func (s *SQLiteStore) catchUpLocked(q sqlq) error {
	ctx := context.Background()
	var seq, pruned int64
	if err := q.QueryRowContext(ctx, `SELECT write_seq, pruned_upto FROM as_meta WHERE id=1`).Scan(&seq, &pruned); err != nil {
		return err
	}
	if seq == s.seq && s.inner != nil {
		return nil
	}
	if s.inner == nil || s.seq < pruned {
		return s.fullLoadLocked(q)
	}
	type change struct{ coll, k string }
	var changes []change
	seen := map[change]bool{}
	rows, err := q.QueryContext(ctx, `SELECT coll, k FROM as_changes WHERE wseq > ? AND wseq <= ? ORDER BY wseq`, s.seq, seq)
	if err != nil {
		return err
	}
	for rows.Next() {
		var c change
		if err := rows.Scan(&c.coll, &c.k); err != nil {
			rows.Close()
			return err
		}
		if c.coll == "*" {
			rows.Close()
			return s.fullLoadLocked(q)
		}
		if !seen[c] {
			seen[c] = true
			changes = append(changes, c)
		}
	}
	if err := closeRows(rows); err != nil {
		return err
	}
	for _, c := range changes {
		if err := reloadEntity(ctx, q, s.inner, c.coll, c.k); err != nil {
			return err
		}
	}
	s.seq = seq
	return nil
}

// reloadEntity replaces one cached entity with its committed rows.
func reloadEntity(ctx context.Context, q sqlq, m *MemoryStore, coll, k string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := mapCollByName(coll); ok {
		var b []byte
		err := q.QueryRowContext(ctx, `SELECT v FROM `+c.table+` WHERE k=?`, k).Scan(&b)
		if errors.Is(err, sql.ErrNoRows) {
			c.del(m, k)
			return nil
		}
		if err != nil {
			return err
		}
		return c.set(m, k, b)
	}
	switch coll {
	case collMessages:
		var items []Message
		err := scanJSONRows(ctx, q, `SELECT session_id, v FROM as_messages WHERE session_id=? ORDER BY idx`, func(_ string, b []byte) error {
			var v Message
			if err := json.Unmarshal(b, &v); err != nil {
				return err
			}
			items = append(items, v)
			return nil
		}, k)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			delete(m.messages, k)
		} else {
			m.messages[k] = items
		}
	case collRunEvents:
		var items []RunEvent
		err := scanJSONRows(ctx, q, `SELECT scope, v FROM as_run_events WHERE scope=? ORDER BY seq`, func(_ string, b []byte) error {
			var v RunEvent
			if err := json.Unmarshal(b, &v); err != nil {
				return err
			}
			items = append(items, v)
			return nil
		}, k)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			delete(m.runEvents, k)
		} else {
			m.runEvents[k] = items
		}
	case collAudit:
		from, _ := strconv.Atoi(k)
		if from > len(m.auditEvents) {
			from = len(m.auditEvents)
		}
		kept := append([]AuditEvent(nil), m.auditEvents[:from]...)
		err := scanJSONRows(ctx, q, `SELECT '', v FROM as_audit_events WHERE idx >= ? ORDER BY idx`, func(_ string, b []byte) error {
			var v AuditEvent
			if err := json.Unmarshal(b, &v); err != nil {
				return err
			}
			kept = append(kept, v)
			return nil
		}, from)
		if err != nil {
			return err
		}
		m.auditEvents = kept
	default:
		return fmt.Errorf("unknown agentservice change collection %q", coll)
	}
	return nil
}

// reloadDirty restores the entities a failed write touched from committed
// rows, so a rejected mutation never lingers in the cache.
func (s *SQLiteStore) reloadDirty(q sqlq, d *storeDirty) error {
	if d.all || d.auditRewrite || d.auditAppend > 0 {
		if d.all {
			return s.fullLoadLocked(q)
		}
	}
	ctx := context.Background()
	for coll, keys := range d.keys {
		for k := range keys {
			if err := reloadEntity(ctx, q, s.inner, coll, k); err != nil {
				return s.fullLoadLocked(q)
			}
		}
	}
	for sid := range d.msgRewrite {
		if err := reloadEntity(ctx, q, s.inner, collMessages, sid); err != nil {
			return s.fullLoadLocked(q)
		}
	}
	for sid := range d.msgAppend {
		if err := reloadEntity(ctx, q, s.inner, collMessages, sid); err != nil {
			return s.fullLoadLocked(q)
		}
	}
	for scope := range d.evRewrite {
		if err := reloadEntity(ctx, q, s.inner, collRunEvents, scope); err != nil {
			return s.fullLoadLocked(q)
		}
	}
	for scope := range d.evInsert {
		if err := reloadEntity(ctx, q, s.inner, collRunEvents, scope); err != nil {
			return s.fullLoadLocked(q)
		}
	}
	if d.auditRewrite || d.auditAppend > 0 {
		if err := reloadEntity(ctx, q, s.inner, collAudit, "0"); err != nil {
			return s.fullLoadLocked(q)
		}
	}
	return nil
}

// persistDirty writes exactly the rows a write touched plus change records.
func persistDirty(ctx context.Context, q sqlq, m *MemoryStore, d *storeDirty, wseq int64) error {
	change := func(coll, k string) error {
		_, err := q.ExecContext(ctx, `INSERT INTO as_changes(wseq, coll, k) VALUES(?,?,?)`, wseq, coll, k)
		return err
	}
	if d.all {
		if err := deleteAllRows(ctx, q); err != nil {
			return err
		}
		if err := insertAllRows(ctx, q, m); err != nil {
			return err
		}
		return change("*", "")
	}
	for coll, keys := range d.keys {
		c, ok := mapCollByName(coll)
		if !ok {
			return fmt.Errorf("unknown agentservice collection %q", coll)
		}
		for k := range keys {
			if err := upsertMapRow(ctx, q, c, m, k, true); err != nil {
				return err
			}
			if err := change(coll, k); err != nil {
				return err
			}
		}
	}
	for sid := range d.msgRewrite {
		if _, err := q.ExecContext(ctx, `DELETE FROM as_messages WHERE session_id=?`, sid); err != nil {
			return err
		}
		if err := insertMessages(ctx, q, sid, m.messages[sid], 0); err != nil {
			return err
		}
		if err := change(collMessages, sid); err != nil {
			return err
		}
	}
	for sid, n := range d.msgAppend {
		items := m.messages[sid]
		from := len(items) - n
		if from < 0 {
			from = 0
		}
		if err := insertMessages(ctx, q, sid, items, from); err != nil {
			return err
		}
		if err := change(collMessages, sid); err != nil {
			return err
		}
	}
	for scope := range d.evRewrite {
		if _, err := q.ExecContext(ctx, `DELETE FROM as_run_events WHERE scope=?`, scope); err != nil {
			return err
		}
		items := m.runEvents[scope]
		for i := range items {
			if err := insertRunEvent(ctx, q, scope, &items[i], false); err != nil {
				return err
			}
		}
		if err := change(collRunEvents, scope); err != nil {
			return err
		}
	}
	for scope, seqs := range d.evInsert {
		want := make(map[uint64]bool, len(seqs))
		for _, seq := range seqs {
			want[seq] = true
		}
		items := m.runEvents[scope]
		for i := range items {
			if want[items[i].Sequence] {
				if err := insertRunEvent(ctx, q, scope, &items[i], true); err != nil {
					return err
				}
			}
		}
		if err := change(collRunEvents, scope); err != nil {
			return err
		}
	}
	if d.auditRewrite {
		if _, err := q.ExecContext(ctx, `DELETE FROM as_audit_events`); err != nil {
			return err
		}
		if err := insertAudit(ctx, q, m.auditEvents, 0); err != nil {
			return err
		}
		if err := change(collAudit, "0"); err != nil {
			return err
		}
	} else if d.auditAppend > 0 {
		from := len(m.auditEvents) - d.auditAppend
		if from < 0 {
			from = 0
		}
		if err := insertAudit(ctx, q, m.auditEvents, from); err != nil {
			return err
		}
		if err := change(collAudit, strconv.Itoa(from)); err != nil {
			return err
		}
	}
	return nil
}

const sqliteChangesKeep = 5000

func (s *SQLiteStore) read(fn func(*MemoryStore) error) error {
	if s == nil {
		return ErrServiceClosed
	}
	s.mu.RLock()
	if err := s.ensureOpenLocked(); err != nil {
		s.mu.RUnlock()
		return err
	}
	var seq int64
	err := s.db.QueryRowContext(context.Background(), `SELECT write_seq FROM as_meta WHERE id=1`).Scan(&seq)
	if err == nil && seq == s.seq && s.inner != nil {
		defer s.mu.RUnlock()
		return fn(s.inner)
	}
	s.mu.RUnlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureOpenLocked(); err != nil {
		return err
	}
	if err := s.catchUpLocked(s.db); err != nil {
		return err
	}
	return fn(s.inner)
}

func (s *SQLiteStore) write(fn func(*MemoryStore) error) error {
	if s == nil {
		return ErrServiceClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureOpenLocked(); err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		err := s.writeOnceLocked(fn)
		if !isSQLiteBusy(err) || attempt >= 6 {
			return err
		}
		time.Sleep(time.Duration(10*(1<<attempt)) * time.Millisecond)
	}
}

// errSQLiteWriteRolledBack wraps a failure after the closure already ran.
func (s *SQLiteStore) writeOnceLocked(fn func(*MemoryStore) error) (err error) {
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
	if err := s.catchUpLocked(conn); err != nil {
		return err
	}
	d := newStoreDirty()
	s.inner.dirty = d
	ferr := fn(s.inner)
	s.inner.dirty = nil
	fail := func(cause error) error {
		_, _ = conn.ExecContext(ctx, `ROLLBACK`)
		committed = true // rolled back explicitly
		if rerr := s.reloadDirty(s.db, d); rerr != nil {
			s.inner = nil // force a full reload on next access
			s.seq = -1
		}
		return cause
	}
	if ferr != nil {
		return fail(ferr)
	}
	if d.empty() {
		return nil
	}
	next := s.seq + 1
	s.inner.mu.Lock()
	perr := persistDirty(ctx, conn, s.inner, d, next)
	s.inner.mu.Unlock()
	if perr != nil {
		return fail(perr)
	}
	if _, err := conn.ExecContext(ctx, `UPDATE as_meta SET write_seq=? WHERE id=1`, next); err != nil {
		return fail(err)
	}
	if next%1000 == 0 && next > sqliteChangesKeep {
		cut := next - sqliteChangesKeep
		if _, err := conn.ExecContext(ctx, `DELETE FROM as_changes WHERE wseq <= ?`, cut); err != nil {
			return fail(err)
		}
		if _, err := conn.ExecContext(ctx, `UPDATE as_meta SET pruned_upto=? WHERE id=1`, cut); err != nil {
			return fail(err)
		}
	}
	if sqliteVerifyWrites {
		got, lerr := loadAllRows(ctx, conn)
		if lerr != nil {
			return fail(lerr)
		}
		a, _ := canonicalStateJSON(s.inner)
		b, _ := canonicalStateJSON(got)
		if !bytes.Equal(a, b) {
			return fail(fmt.Errorf("sqliteVerifyWrites: cache and rows diverged after write %d: %s", next, firstStateDiff(s.inner, got)))
		}
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return fail(err)
	}
	committed = true
	s.seq = next
	return nil
}

// sqliteVerifyWrites makes every write compare the cache with a full reload
// from rows (tests only; catches an un-instrumented mutation site).
var sqliteVerifyWrites = os.Getenv("MACLAW_AGENTSERVICE_SQLITE_VERIFY_WRITES") == "1"

func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "database is locked") || strings.Contains(text, "sqlite_busy") || strings.Contains(text, "database table is locked")
}

// Store methods. Reads are wrapped as well as writes so a second process's
// committed changes are visible without requiring a service restart.
func (s *SQLiteStore) SaveTenant(v Tenant) error {
	return s.write(func(m *MemoryStore) error { return m.SaveTenant(v) })
}
func (s *SQLiteStore) GetTenant(id string) (Tenant, error) {
	var out Tenant
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.GetTenant(id); return e })
	return out, err
}
func (s *SQLiteStore) ListTenants() ([]Tenant, error) {
	var out []Tenant
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.ListTenants(); return e })
	return out, err
}
func (s *SQLiteStore) DeleteTenant(id string) error {
	return s.write(func(m *MemoryStore) error { return m.DeleteTenant(id) })
}
func (s *SQLiteStore) SaveUser(v User) error {
	return s.write(func(m *MemoryStore) error { return m.SaveUser(v) })
}
func (s *SQLiteStore) GetUser(t, u string) (User, error) {
	var out User
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.GetUser(t, u); return e })
	return out, err
}
func (s *SQLiteStore) ListUsers(t string) ([]User, error) {
	var out []User
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.ListUsers(t); return e })
	return out, err
}
func (s *SQLiteStore) DeleteUser(t, u string) error {
	return s.write(func(m *MemoryStore) error { return m.DeleteUser(t, u) })
}
func (s *SQLiteStore) SaveCredential(v Credential) error {
	return s.write(func(m *MemoryStore) error { return m.SaveCredential(v) })
}
func (s *SQLiteStore) GetCredential(t, u, id string) (Credential, error) {
	var out Credential
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.GetCredential(t, u, id); return e })
	return out, err
}
func (s *SQLiteStore) ListCredentials(t, u string) ([]Credential, error) {
	var out []Credential
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.ListCredentials(t, u); return e })
	return out, err
}
func (s *SQLiteStore) GetCredentialByAPIKey(key string) (Credential, error) {
	var out Credential
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.GetCredentialByAPIKey(key); return e })
	return out, err
}
func (s *SQLiteStore) SaveUserConfig(v UserConfig) error {
	return s.write(func(m *MemoryStore) error { return m.SaveUserConfig(v) })
}
func (s *SQLiteStore) GetUserConfig(t, u string) (UserConfig, error) {
	var out UserConfig
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.GetUserConfig(t, u); return e })
	return out, err
}
func (s *SQLiteStore) SaveInstance(v Instance) error {
	return s.write(func(m *MemoryStore) error { return m.SaveInstance(v) })
}
func (s *SQLiteStore) GetInstance(t, u, id string) (Instance, error) {
	var out Instance
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.GetInstance(t, u, id); return e })
	return out, err
}
func (s *SQLiteStore) ListInstances(t, u string) ([]Instance, error) {
	var out []Instance
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.ListInstances(t, u); return e })
	return out, err
}
func (s *SQLiteStore) DeleteInstance(t, u, id string) error {
	return s.write(func(m *MemoryStore) error { return m.DeleteInstance(t, u, id) })
}
func (s *SQLiteStore) SaveSession(v Session) error {
	return s.write(func(m *MemoryStore) error { return m.SaveSession(v) })
}
func (s *SQLiteStore) GetSession(t, u, i, id string) (Session, error) {
	var out Session
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.GetSession(t, u, i, id); return e })
	return out, err
}
func (s *SQLiteStore) ListSessions(t, u, i string) ([]Session, error) {
	var out []Session
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.ListSessions(t, u, i); return e })
	return out, err
}
func (s *SQLiteStore) DeleteSession(t, u, i, id string) error {
	return s.write(func(m *MemoryStore) error { return m.DeleteSession(t, u, i, id) })
}
func (s *SQLiteStore) SaveMessage(v Message) error {
	return s.write(func(m *MemoryStore) error { return m.SaveMessage(v) })
}
func (s *SQLiteStore) GetMessage(sessionID, messageID string) (Message, error) {
	var out Message
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.GetMessage(sessionID, messageID); return e })
	return out, err
}
func (s *SQLiteStore) ListMessages(id string) ([]Message, error) {
	var out []Message
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.ListMessages(id); return e })
	return out, err
}
func (s *SQLiteStore) DeleteMessages(id string, ids []string) (int, error) {
	var n int
	err := s.write(func(m *MemoryStore) error { var e error; n, e = m.DeleteMessages(id, ids); return e })
	return n, err
}
func (s *SQLiteStore) SaveRun(v Run) error {
	return s.write(func(m *MemoryStore) error { return m.SaveRun(v) })
}
func (s *SQLiteStore) GetRun(t, u, i, id string) (Run, error) {
	var out Run
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.GetRun(t, u, i, id); return e })
	return out, err
}
func (s *SQLiteStore) ListRuns(t, u, i string) ([]Run, error) {
	var out []Run
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.ListRuns(t, u, i); return e })
	return out, err
}
func (s *SQLiteStore) GetRunByUserMessageID(t, u, i, id string) (Run, error) {
	var out Run
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.GetRunByUserMessageID(t, u, i, id); return e })
	return out, err
}
func (s *SQLiteStore) SaveAuditEvent(v AuditEvent) error {
	return s.write(func(m *MemoryStore) error { return m.SaveAuditEvent(v) })
}
func (s *SQLiteStore) ListAuditEvents(t, u string) ([]AuditEvent, error) {
	var out []AuditEvent
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.ListAuditEvents(t, u); return e })
	return out, err
}
func (s *SQLiteStore) DeleteAuditEvents(t, u string) (int, error) {
	var n int
	err := s.write(func(m *MemoryStore) error { var e error; n, e = m.DeleteAuditEvents(t, u); return e })
	return n, err
}

func (s *SQLiteStore) SaveRunAdmission(message Message, run Run) error {
	return s.SaveRunAdmissionWithEvents(message, run, nil)
}

func (s *SQLiteStore) SaveRunAdmissionWithEvents(message Message, run Run, events []RunEvent) error {
	return s.write(func(m *MemoryStore) error {
		if err := m.SaveMessage(message); err != nil {
			return err
		}
		if err := m.SaveRun(run); err != nil {
			return err
		}
		for _, event := range events {
			if _, err := m.Append(event); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *SQLiteStore) SaveRunCompletion(message Message, run Run) error {
	return s.SaveRunCompletionWithEvents(message, run, nil)
}

func (s *SQLiteStore) SaveRunCompletionWithEvents(message Message, run Run, events []RunEvent) error {
	return s.write(func(m *MemoryStore) error {
		if err := m.SaveMessage(message); err != nil {
			return err
		}
		if err := m.SaveRun(run); err != nil {
			return err
		}
		for _, event := range events {
			if _, err := m.Append(event); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *SQLiteStore) SaveRunTerminalWithEvents(run Run, events []RunEvent) error {
	return s.write(func(m *MemoryStore) error {
		if err := m.SaveRun(run); err != nil {
			return err
		}
		for _, event := range events {
			if _, err := m.Append(event); err != nil {
				return err
			}
		}
		return nil
	})
}

// CommitRunLifecycle is the single transaction seam custom hosts can mirror
// when they provide a SQL-backed Store. The generic contract is validated
// before entering SQLite, then the phase-specific operation runs inside the
// existing write transaction.
func (s *SQLiteStore) CommitRunLifecycle(mutation RunLifecycleMutation) error {
	if err := validateRunLifecycleMutation(mutation); err != nil {
		return err
	}
	return s.write(func(m *MemoryStore) error {
		switch mutation.Phase {
		case RunLifecyclePhaseAdmission:
			if err := m.SaveMessage(*mutation.Message); err != nil {
				return err
			}
			if err := m.SaveRun(mutation.Run); err != nil {
				return err
			}
		case RunLifecyclePhaseCompletion:
			if err := m.SaveMessage(*mutation.Message); err != nil {
				return err
			}
			if err := m.SaveRun(mutation.Run); err != nil {
				return err
			}
		case RunLifecyclePhaseTerminal:
			if err := m.SaveRun(mutation.Run); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported run lifecycle phase %q", mutation.Phase)
		}
		for _, event := range mutation.Events {
			if _, err := m.Append(event); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *SQLiteStore) Append(event RunEvent) (RunEvent, error) {
	var out RunEvent
	err := s.write(func(m *MemoryStore) error { var err error; out, err = m.Append(event); return err })
	return out, err
}

func (s *SQLiteStore) SequenceForID(t, u, r, id string) (uint64, bool, error) {
	var seq uint64
	var found bool
	err := s.read(func(m *MemoryStore) error { var err error; seq, found, err = m.SequenceForID(t, u, r, id); return err })
	return seq, found, err
}

func (s *SQLiteStore) ListAfter(t, u, r string, after uint64, limit int) ([]RunEvent, error) {
	var out []RunEvent
	err := s.read(func(m *MemoryStore) error { var err error; out, err = m.ListAfter(t, u, r, after, limit); return err })
	return out, err
}

func (s *SQLiteStore) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

var (
	_ Store                        = (*SQLiteStore)(nil)
	_ RunEventStore                = (*SQLiteStore)(nil)
	_ RunAdmissionEventStore       = (*SQLiteStore)(nil)
	_ RunCompletionEventStore      = (*SQLiteStore)(nil)
	_ RunTerminalEventStore        = (*SQLiteStore)(nil)
	_ RunLifecycleTransactionStore = (*SQLiteStore)(nil)
)

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
