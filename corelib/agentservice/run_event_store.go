package agentservice

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	_ "modernc.org/sqlite"
)

// RunEvent is the durable, transport-neutral event emitted by the shared
// Runtime. Sequence is scoped to one authenticated run and is monotonic.
type RunEvent struct {
	ID            string         `json:"event_id"`
	TenantID      string         `json:"tenant_id"`
	UserID        string         `json:"user_id"`
	InstanceID    string         `json:"instance_id"`
	SessionID     string         `json:"session_id"`
	RunID         string         `json:"run_id"`
	Sequence      uint64         `json:"sequence"`
	Type          string         `json:"type"`
	SchemaVersion string         `json:"schema_version"`
	Payload       map[string]any `json:"payload,omitempty"`
	OccurredAt    time.Time      `json:"occurred_at"`
}

const RunEventSchemaVersion = "maclaw.run-event/v1"

type RunEventStore interface {
	// Append is idempotent within (tenant, user, run): a duplicate sequence or
	// event id returns the canonical event already persisted.
	Append(RunEvent) (RunEvent, error)
	ListAfter(tenantID, userID, runID string, after uint64, limit int) ([]RunEvent, error)
	Close() error
}

// runEventSequenceResolver is an optional extension used by transports that
// need to resume from an opaque event id (for example SSE Last-Event-ID).
// Keeping it optional preserves compatibility with custom RunEventStore
// implementations that only provide the core append/list contract.
type runEventSequenceResolver interface {
	SequenceForID(tenantID, userID, runID, eventID string) (uint64, bool, error)
}

type MemoryRunEventStore struct {
	mu     sync.RWMutex
	events map[string][]RunEvent
	closed bool
}

func NewMemoryRunEventStore() *MemoryRunEventStore {
	return &MemoryRunEventStore{events: make(map[string][]RunEvent)}
}

func runEventScope(tenantID, userID, runID string) string {
	return tenantID + "\x00" + userID + "\x00" + runID
}

func cloneRunEvent(event RunEvent) RunEvent {
	if event.Payload != nil {
		event.Payload = cloneRunEventPayload(event.Payload)
	}
	return event
}

// cloneRunEventPayload copies the JSON-shaped values normally used by Runtime
// events without sharing nested maps/slices with the caller.  Keeping the
// common JSON container types explicit preserves scalar types (for example an
// int supplied by an in-process GUI sink) while still isolating the mutable
// structures that caused replay payloads to be overwritten in the past.
func cloneRunEventPayload(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	out := make(map[string]any, len(payload))
	for key, value := range payload {
		out[key] = cloneRunEventValue(value)
	}
	return out
}

func cloneRunEventValue(value any) any {
	switch current := value.(type) {
	case map[string]any:
		return cloneRunEventPayload(current)
	case []any:
		out := make([]any, len(current))
		for i, item := range current {
			out[i] = cloneRunEventValue(item)
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(current))
		for key, item := range current {
			out[key] = item
		}
		return out
	case []string:
		return append([]string(nil), current...)
	case []map[string]any:
		out := make([]map[string]any, len(current))
		for i, item := range current {
			out[i] = cloneRunEventPayload(item)
		}
		return out
	default:
		return value
	}
}

// normalizeRunEvent applies the Runtime event envelope defaults and validates
// payload serializability before a store mutates state.  SQLite naturally
// performs this validation while encoding its transaction; doing it here too
// keeps memory/file repositories from accepting an event that cannot survive
// a restart and prevents partial lifecycle commits.
func normalizeRunEvent(event RunEvent) (RunEvent, error) {
	if event.ID == "" {
		event.ID = NewID("evt")
	}
	if event.SchemaVersion == "" {
		event.SchemaVersion = RunEventSchemaVersion
	}
	// Apply the same envelope validation used by Runtime producers before any
	// repository mutates state.  This protects direct Store callers (including
	// custom service adapters) from bypassing event-id/type newline limits and
	// keeps replay behavior identical for GUI and headless sinks.
	normalized, err := agentruntime.NormalizeEventEnvelope(agentruntime.Event{
		SchemaVersion: event.SchemaVersion,
		EventID:       event.ID,
		Type:          event.Type,
		Scope: agentruntime.Scope{
			TenantID: event.TenantID, UserID: event.UserID, InstanceID: event.InstanceID,
			SessionID: event.SessionID, RunID: event.RunID,
		},
		RunID:      event.RunID,
		OccurredAt: event.OccurredAt,
		Sequence:   event.Sequence,
		Payload:    event.Payload,
	})
	if err != nil {
		return RunEvent{}, err
	}
	event.ID = normalized.EventID
	event.Type = normalized.Type
	event.SchemaVersion = normalized.SchemaVersion
	event.RunID = normalized.RunID
	event.OccurredAt = normalized.OccurredAt
	event.Payload = normalized.Payload
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	return event, nil
}

func validateRunEventPayloads(events []RunEvent) error {
	for _, event := range events {
		if event.Payload == nil {
			continue
		}
		if _, err := json.Marshal(event.Payload); err != nil {
			return err
		}
	}
	return nil
}

func (s *MemoryRunEventStore) Append(event RunEvent) (RunEvent, error) {
	if s == nil {
		return RunEvent{}, errors.New("run event store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return RunEvent{}, ErrServiceClosed
	}
	key := runEventScope(event.TenantID, event.UserID, event.RunID)
	items := s.events[key]
	if event.Sequence == 0 {
		var maxSequence uint64
		for _, existing := range items {
			if existing.Sequence > maxSequence {
				maxSequence = existing.Sequence
			}
		}
		event.Sequence = maxSequence + 1
	}
	event, err := normalizeRunEvent(event)
	if err != nil {
		return RunEvent{}, err
	}
	for _, existing := range items {
		if existing.Sequence == event.Sequence || (event.ID != "" && existing.ID == event.ID) {
			return cloneRunEvent(existing), nil
		}
	}
	items = append(items, event)
	sort.Slice(items, func(i, j int) bool { return items[i].Sequence < items[j].Sequence })
	s.events[key] = items
	return event, nil
}

func (s *MemoryRunEventStore) SequenceForID(tenantID, userID, runID, eventID string) (uint64, bool, error) {
	if s == nil {
		return 0, false, errors.New("run event store is unavailable")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return 0, false, ErrServiceClosed
	}
	for _, event := range s.events[runEventScope(tenantID, userID, runID)] {
		if event.ID == eventID {
			return event.Sequence, true, nil
		}
	}
	return 0, false, nil
}

func (s *MemoryRunEventStore) ListAfter(tenantID, userID, runID string, after uint64, limit int) ([]RunEvent, error) {
	if s == nil {
		return nil, errors.New("run event store is unavailable")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, ErrServiceClosed
	}
	items := s.events[runEventScope(tenantID, userID, runID)]
	out := make([]RunEvent, 0, minIntRunEventStore(limit, len(items)))
	for _, event := range items {
		if event.Sequence <= after {
			continue
		}
		out = append(out, cloneRunEvent(event))
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *MemoryRunEventStore) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return nil
}

type SQLiteRunEventStore struct {
	mu     sync.Mutex
	db     *sql.DB
	closed bool
}

func NewSQLiteRunEventStore(path string) (*SQLiteRunEventStore, error) {
	if err := secureMkdirAll(filepath.Dir(path)); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &SQLiteRunEventStore{db: db}
	if err := ensureSQLiteRunEventSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

// ensureSQLiteRunEventSchema creates the scoped event table and upgrades the
// original schema, which used event_id as a global primary key.  Producer IDs
// are intentionally idempotent only within (tenant, user, run); keeping that
// scope in the uniqueness constraint allows two independent runs to use the
// same deterministic callback ID without leaking state across runs.
func ensureSQLiteRunEventSchema(db *sql.DB) error {
	if db == nil {
		return errors.New("run event database is unavailable")
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL;`); err != nil {
		return err
	}
	var exists int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='run_events'`).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		legacy, err := sqliteRunEventSchemaUsesGlobalPrimaryKey(db)
		if err != nil {
			return err
		}
		if legacy {
			if err := migrateSQLiteRunEventSchema(db); err != nil {
				return err
			}
		}
	}
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS run_events (
		event_id TEXT NOT NULL,
		tenant_id TEXT NOT NULL,
		user_id TEXT NOT NULL,
		instance_id TEXT NOT NULL,
		session_id TEXT NOT NULL,
		run_id TEXT NOT NULL,
		sequence INTEGER NOT NULL,
		type TEXT NOT NULL,
		schema_version TEXT NOT NULL,
		payload_json TEXT NOT NULL,
		occurred_at TEXT NOT NULL,
		UNIQUE(tenant_id, user_id, run_id, event_id),
		UNIQUE(tenant_id, user_id, run_id, sequence)
	); CREATE INDEX IF NOT EXISTS idx_run_events_scope ON run_events(tenant_id, user_id, run_id, sequence);`)
	return err
}

func sqliteRunEventSchemaUsesGlobalPrimaryKey(db *sql.DB) (bool, error) {
	rows, err := db.Query(`PRAGMA table_info(run_events)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	var (
		cid       int
		name      string
		columnTyp string
		notNull   int
		defaultV  any
		pk        int
	)
	for rows.Next() {
		if err := rows.Scan(&cid, &name, &columnTyp, &notNull, &defaultV, &pk); err != nil {
			return false, err
		}
		if name == "event_id" && pk > 0 {
			return true, nil
		}
	}
	return false, rows.Err()
}

func migrateSQLiteRunEventSchema(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// The old named scope index would otherwise collide with the new table's
	// index during the transactional table rebuild.
	if _, err := tx.Exec(`DROP INDEX IF EXISTS idx_run_events_scope; ALTER TABLE run_events RENAME TO run_events_legacy`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE TABLE run_events (
		event_id TEXT NOT NULL,
		tenant_id TEXT NOT NULL,
		user_id TEXT NOT NULL,
		instance_id TEXT NOT NULL,
		session_id TEXT NOT NULL,
		run_id TEXT NOT NULL,
		sequence INTEGER NOT NULL,
		type TEXT NOT NULL,
		schema_version TEXT NOT NULL,
		payload_json TEXT NOT NULL,
		occurred_at TEXT NOT NULL,
		UNIQUE(tenant_id, user_id, run_id, event_id),
		UNIQUE(tenant_id, user_id, run_id, sequence)
	)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO run_events(event_id, tenant_id, user_id, instance_id, session_id, run_id, sequence, type, schema_version, payload_json, occurred_at)
		SELECT event_id, tenant_id, user_id, instance_id, session_id, run_id, sequence, type, schema_version, payload_json, occurred_at FROM run_events_legacy`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE run_events_legacy; CREATE INDEX idx_run_events_scope ON run_events(tenant_id, user_id, run_id, sequence)`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteRunEventStore) Append(event RunEvent) (RunEvent, error) {
	if s == nil || s.db == nil {
		return RunEvent{}, errors.New("run event store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return RunEvent{}, ErrServiceClosed
	}
	var err error
	event, err = normalizeRunEvent(event)
	if err != nil {
		return RunEvent{}, err
	}
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return RunEvent{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return RunEvent{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if event.Sequence == 0 {
		if err = tx.QueryRow(`SELECT COALESCE(MAX(sequence), 0) + 1 FROM run_events WHERE tenant_id=? AND user_id=? AND run_id=?`, event.TenantID, event.UserID, event.RunID).Scan(&event.Sequence); err != nil {
			return RunEvent{}, err
		}
	}
	result, err := tx.Exec(`INSERT INTO run_events(event_id, tenant_id, user_id, instance_id, session_id, run_id, sequence, type, schema_version, payload_json, occurred_at) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(tenant_id,user_id,run_id,sequence) DO NOTHING`, event.ID, event.TenantID, event.UserID, event.InstanceID, event.SessionID, event.RunID, event.Sequence, event.Type, event.SchemaVersion, string(payload), event.OccurredAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		// A replay may carry the same event id but a different sequence. Treat
		// the existing row as the authoritative result when it is in scope.
		if existing, found, lookupErr := scanSQLiteRunEvent(tx, `SELECT event_id, tenant_id, user_id, instance_id, session_id, run_id, sequence, type, schema_version, payload_json, occurred_at FROM run_events WHERE tenant_id=? AND user_id=? AND run_id=? AND event_id=?`, event.TenantID, event.UserID, event.RunID, event.ID); lookupErr == nil && found {
			return existing, nil
		}
		return RunEvent{}, err
	}
	if affected, rowsErr := result.RowsAffected(); rowsErr == nil && affected == 0 {
		// Sequence conflict: return the row already committed at this cursor,
		// rather than echoing the attempted event payload.
		if existing, found, lookupErr := scanSQLiteRunEvent(tx, `SELECT event_id, tenant_id, user_id, instance_id, session_id, run_id, sequence, type, schema_version, payload_json, occurred_at FROM run_events WHERE tenant_id=? AND user_id=? AND run_id=? AND sequence=?`, event.TenantID, event.UserID, event.RunID, event.Sequence); lookupErr == nil && found {
			return existing, nil
		}
		return RunEvent{}, errors.New("run event sequence conflict could not be resolved")
	}
	if err = tx.Commit(); err != nil {
		return RunEvent{}, err
	}
	return event, nil
}

func scanSQLiteRunEvent(tx *sql.Tx, query string, args ...any) (RunEvent, bool, error) {
	var event RunEvent
	var payload, occurred string
	err := tx.QueryRow(query, args...).Scan(&event.ID, &event.TenantID, &event.UserID, &event.InstanceID, &event.SessionID, &event.RunID, &event.Sequence, &event.Type, &event.SchemaVersion, &payload, &occurred)
	if errors.Is(err, sql.ErrNoRows) {
		return RunEvent{}, false, nil
	}
	if err != nil {
		return RunEvent{}, false, err
	}
	if err := json.Unmarshal([]byte(payload), &event.Payload); err != nil {
		return RunEvent{}, false, err
	}
	event.OccurredAt, err = time.Parse(time.RFC3339Nano, occurred)
	if err != nil {
		return RunEvent{}, false, err
	}
	return event, true, nil
}

func (s *SQLiteRunEventStore) SequenceForID(tenantID, userID, runID, eventID string) (uint64, bool, error) {
	if s == nil || s.db == nil {
		return 0, false, errors.New("run event store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, false, ErrServiceClosed
	}
	var sequence uint64
	err := s.db.QueryRow(`SELECT sequence FROM run_events WHERE tenant_id=? AND user_id=? AND run_id=? AND event_id=?`, tenantID, userID, runID, eventID).Scan(&sequence)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return sequence, true, nil
}

func (s *SQLiteRunEventStore) ListAfter(tenantID, userID, runID string, after uint64, limit int) ([]RunEvent, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("run event store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrServiceClosed
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT event_id, tenant_id, user_id, instance_id, session_id, run_id, sequence, type, schema_version, payload_json, occurred_at FROM run_events WHERE tenant_id=? AND user_id=? AND run_id=? AND sequence>? ORDER BY sequence LIMIT ?`, tenantID, userID, runID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunEvent
	for rows.Next() {
		var event RunEvent
		var payload, occurred string
		if err := rows.Scan(&event.ID, &event.TenantID, &event.UserID, &event.InstanceID, &event.SessionID, &event.RunID, &event.Sequence, &event.Type, &event.SchemaVersion, &payload, &occurred); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(payload), &event.Payload); err != nil {
			return nil, err
		}
		event.OccurredAt, err = time.Parse(time.RFC3339Nano, occurred)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func (s *SQLiteRunEventStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.db.Close()
}

// minIntRunEventStore is file-local in intent. The agentservice package also
// exposes a minInt helper for knowledge import validation, so keep this name
// distinct to avoid duplicate package symbols when both features are built.
func minIntRunEventStore(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var _ RunEventStore = (*MemoryRunEventStore)(nil)
var _ RunEventStore = (*SQLiteRunEventStore)(nil)
