package tool

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// HostCallIdentity is supplied by the trusted host protocol adapter. CallID
// alone is deliberately insufficient: different model connections are allowed
// to reuse a provider-local ID, while a reconnect of the same connection must
// find the original call.
type HostCallIdentity struct {
	Protocol     string
	ConnectionID string
	CallID       string
	// SurfaceEpoch binds a provider tool call to the exact model-request tool
	// surface that exposed it. It is empty only for explicit host-owned calls
	// that did not originate from a model response.
	SurfaceEpoch string
}

// HostCallState tracks the irreversible host-side interpretation of a model
// tool call. A stale received/admitted record becomes unknown; it is never
// eligible for a second provider invocation.
type HostCallState string

const (
	HostCallReceived  HostCallState = "received"
	HostCallAdmitted  HostCallState = "admitted"
	HostCallCompleted HostCallState = "completed"
	HostCallUnknown   HostCallState = "unknown"

	HostCallRunningLease     = 5 * time.Minute
	HostCallJournalMaxResult = 256 * 1024
)

type HostCallRecord struct {
	Identity         HostCallIdentity
	GrantFingerprint string
	RequestDigest    string
	State            HostCallState
	Result           string
	ResultDigest     string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type HostCallAcquireAction string

const (
	HostCallAcquireAdmit      HostCallAcquireAction = "admit"
	HostCallAcquireReplay     HostCallAcquireAction = "replay"
	HostCallAcquireConflict   HostCallAcquireAction = "conflict"
	HostCallAcquireInProgress HostCallAcquireAction = "in_progress"
	HostCallAcquireUnknown    HostCallAcquireAction = "unknown"
)

// HostCallJournal is the durable host protocol boundary in front of grant
// consumption. It stores only the trusted request/grant digests and a bounded
// host-projected result; provider arguments, credentials and raw artifacts do
// not belong here.
type HostCallJournal interface {
	Acquire(identity HostCallIdentity, grantFingerprint, requestDigest string, now time.Time) (HostCallRecord, HostCallAcquireAction, error)
	MarkAdmitted(identity HostCallIdentity, grantFingerprint, requestDigest string, now time.Time) (HostCallRecord, error)
	Complete(identity HostCallIdentity, grantFingerprint, requestDigest, result string, now time.Time) (HostCallRecord, error)
	MarkUnknown(identity HostCallIdentity, grantFingerprint, requestDigest string, now time.Time) (HostCallRecord, error)
	ReconcileStale(now time.Time, maxAge time.Duration) (int, error)
}

func InvocationGrantFingerprint(grant InvocationGrant) string {
	return invocationGrantFingerprint(grant)
}

func validateHostCallInputs(identity HostCallIdentity, grantFingerprint, requestDigest string) error {
	if strings.TrimSpace(identity.Protocol) == "" || strings.TrimSpace(identity.ConnectionID) == "" || strings.TrimSpace(identity.CallID) == "" {
		return fmt.Errorf("host_call_identity_required")
	}
	if strings.TrimSpace(grantFingerprint) == "" || strings.TrimSpace(requestDigest) == "" {
		return fmt.Errorf("host_call_binding_required")
	}
	return nil
}

func validateHostCallResult(result string) error {
	if len([]byte(result)) > HostCallJournalMaxResult {
		return fmt.Errorf("host_call_result_too_large")
	}
	return nil
}

func hostCallKey(identity HostCallIdentity) string {
	return SchemaDigest([]byte(strings.Join([]string{identity.Protocol, identity.ConnectionID, identity.CallID, identity.SurfaceEpoch}, "\x00")))
}

func sameHostCallBinding(record HostCallRecord, grantFingerprint, requestDigest string) bool {
	return record.GrantFingerprint == grantFingerprint && record.RequestDigest == requestDigest
}

type memoryHostCallJournal struct {
	mu      sync.Mutex
	records map[string]HostCallRecord
}

// NewMemoryHostCallJournal is for tests and explicitly single-process hosts.
// Restartable hosts must use SQLiteHostCallJournal.
func NewMemoryHostCallJournal() HostCallJournal {
	return &memoryHostCallJournal{records: make(map[string]HostCallRecord)}
}

func (j *memoryHostCallJournal) Acquire(identity HostCallIdentity, grantFingerprint, requestDigest string, now time.Time) (HostCallRecord, HostCallAcquireAction, error) {
	if j == nil {
		return HostCallRecord{}, "", fmt.Errorf("host call journal is unavailable")
	}
	if err := validateHostCallInputs(identity, grantFingerprint, requestDigest); err != nil {
		return HostCallRecord{}, "", err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	key := hostCallKey(identity)
	if record, ok := j.records[key]; ok {
		if !sameHostCallBinding(record, grantFingerprint, requestDigest) {
			return record, HostCallAcquireConflict, nil
		}
		return record, hostCallAcquireAction(record.State), nil
	}
	record := HostCallRecord{Identity: identity, GrantFingerprint: grantFingerprint, RequestDigest: requestDigest, State: HostCallReceived, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
	j.records[key] = record
	return record, HostCallAcquireAdmit, nil
}

func (j *memoryHostCallJournal) MarkAdmitted(identity HostCallIdentity, grantFingerprint, requestDigest string, now time.Time) (HostCallRecord, error) {
	return j.transition(identity, grantFingerprint, requestDigest, now, func(record *HostCallRecord) error {
		if record.State != HostCallReceived {
			return fmt.Errorf("host_call_not_receivable")
		}
		record.State = HostCallAdmitted
		return nil
	})
}

func (j *memoryHostCallJournal) Complete(identity HostCallIdentity, grantFingerprint, requestDigest, result string, now time.Time) (HostCallRecord, error) {
	if err := validateHostCallResult(result); err != nil {
		return HostCallRecord{}, err
	}
	return j.transition(identity, grantFingerprint, requestDigest, now, func(record *HostCallRecord) error {
		if record.State != HostCallReceived && record.State != HostCallAdmitted {
			return fmt.Errorf("host_call_not_completable")
		}
		record.State, record.Result, record.ResultDigest = HostCallCompleted, result, SchemaDigest([]byte(result))
		return nil
	})
}

func (j *memoryHostCallJournal) MarkUnknown(identity HostCallIdentity, grantFingerprint, requestDigest string, now time.Time) (HostCallRecord, error) {
	return j.transition(identity, grantFingerprint, requestDigest, now, func(record *HostCallRecord) error {
		if record.State == HostCallCompleted || record.State == HostCallUnknown {
			return fmt.Errorf("host_call_terminal")
		}
		record.State = HostCallUnknown
		return nil
	})
}

func (j *memoryHostCallJournal) transition(identity HostCallIdentity, grantFingerprint, requestDigest string, now time.Time, change func(*HostCallRecord) error) (HostCallRecord, error) {
	if j == nil {
		return HostCallRecord{}, fmt.Errorf("host call journal is unavailable")
	}
	if err := validateHostCallInputs(identity, grantFingerprint, requestDigest); err != nil {
		return HostCallRecord{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	key := hostCallKey(identity)
	record, ok := j.records[key]
	if !ok || !sameHostCallBinding(record, grantFingerprint, requestDigest) {
		return HostCallRecord{}, fmt.Errorf("host_call_conflict")
	}
	if err := change(&record); err != nil {
		return record, err
	}
	record.UpdatedAt = now.UTC()
	j.records[key] = record
	return record, nil
}

func (j *memoryHostCallJournal) ReconcileStale(now time.Time, maxAge time.Duration) (int, error) {
	if j == nil || maxAge <= 0 {
		return 0, fmt.Errorf("host call journal maximum running age must be positive")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	cutoff, changed := now.UTC().Add(-maxAge), 0
	for key, record := range j.records {
		if (record.State == HostCallReceived || record.State == HostCallAdmitted) && !record.UpdatedAt.After(cutoff) {
			record.State, record.UpdatedAt = HostCallUnknown, now.UTC()
			j.records[key] = record
			changed++
		}
	}
	return changed, nil
}

func hostCallAcquireAction(state HostCallState) HostCallAcquireAction {
	switch state {
	case HostCallCompleted:
		return HostCallAcquireReplay
	case HostCallUnknown:
		return HostCallAcquireUnknown
	default:
		return HostCallAcquireInProgress
	}
}

// ResolveHostCallAcquireAction rewrites a grant-fingerprint conflict into the
// stored identity's state when the request digest still matches. Repeat
// siblings reuse one model-visible name, so a retry of an earlier call ID
// looks up the later live grant and would otherwise conflict instead of
// replaying. A different digest stays a conflict.
func ResolveHostCallAcquireAction(action HostCallAcquireAction, record HostCallRecord, requestDigest string) HostCallAcquireAction {
	if action != HostCallAcquireConflict {
		return action
	}
	if strings.TrimSpace(record.RequestDigest) == "" || record.RequestDigest != requestDigest {
		return HostCallAcquireConflict
	}
	return hostCallAcquireAction(record.State)
}

// SQLiteHostCallJournal provides the cross-process journal for host call
// replay. SQLite's INSERT OR IGNORE is the linearization point for the first
// interpretation of an identity.
type SQLiteHostCallJournal struct{ db *sql.DB }

func NewSQLiteHostCallJournal(dbPath string) (*SQLiteHostCallJournal, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, fmt.Errorf("host call journal path is required")
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, fmt.Errorf("create host call journal directory: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	journal := &SQLiteHostCallJournal{db: db}
	if err := journal.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return journal, nil
}

func (j *SQLiteHostCallJournal) init() error {
	if j == nil || j.db == nil {
		return fmt.Errorf("host call journal is unavailable")
	}
	for _, statement := range []string{
		`PRAGMA journal_mode=WAL`, `PRAGMA synchronous=FULL`, `PRAGMA busy_timeout=5000`,
		`CREATE TABLE IF NOT EXISTS semantic_host_calls (
				call_key TEXT PRIMARY KEY, protocol TEXT NOT NULL, connection_id TEXT NOT NULL, call_id TEXT NOT NULL, surface_epoch TEXT NOT NULL DEFAULT '',
			grant_fingerprint TEXT NOT NULL, request_digest TEXT NOT NULL,
			state TEXT NOT NULL CHECK(state IN ('received','admitted','completed','unknown')),
			result TEXT NOT NULL DEFAULT '', result_digest TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_semantic_host_calls_reconcile ON semantic_host_calls(state, updated_at)`,
	} {
		if _, err := j.db.Exec(statement); err != nil {
			return err
		}
	}
	// semantic_host_calls predates SurfaceEpoch. Keep existing durable journals
	// readable while making the correlation column part of every new identity.
	if err := ensureHostCallJournalColumn(j.db, "surface_epoch", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	return nil
}

func ensureHostCallJournalColumn(db *sql.DB, column, definition string) error {
	rows, err := db.Query(`PRAGMA table_info(semantic_host_calls)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue any
		var primaryKey int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.Exec(`ALTER TABLE semantic_host_calls ADD COLUMN ` + column + ` ` + definition)
	return err
}

func (j *SQLiteHostCallJournal) Close() error {
	if j == nil || j.db == nil {
		return nil
	}
	return j.db.Close()
}

func (j *SQLiteHostCallJournal) Acquire(identity HostCallIdentity, grantFingerprint, requestDigest string, now time.Time) (HostCallRecord, HostCallAcquireAction, error) {
	if j == nil || j.db == nil {
		return HostCallRecord{}, "", fmt.Errorf("host call journal is unavailable")
	}
	if err := validateHostCallInputs(identity, grantFingerprint, requestDigest); err != nil {
		return HostCallRecord{}, "", err
	}
	key := hostCallKey(identity)
	result, err := j.db.Exec(`INSERT OR IGNORE INTO semantic_host_calls (call_key, protocol, connection_id, call_id, surface_epoch, grant_fingerprint, request_digest, state, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, 'received', ?, ?)`, key, identity.Protocol, identity.ConnectionID, identity.CallID, identity.SurfaceEpoch, grantFingerprint, requestDigest, hostCallTime(now), hostCallTime(now))
	if err != nil {
		return HostCallRecord{}, "", err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return HostCallRecord{}, "", err
	}
	if changed == 1 {
		record, err := j.get(key)
		return record, HostCallAcquireAdmit, err
	}
	record, err := j.get(key)
	if err != nil {
		return HostCallRecord{}, "", err
	}
	if !sameHostCallBinding(record, grantFingerprint, requestDigest) {
		return record, HostCallAcquireConflict, nil
	}
	return record, hostCallAcquireAction(record.State), nil
}

func (j *SQLiteHostCallJournal) MarkAdmitted(identity HostCallIdentity, grantFingerprint, requestDigest string, now time.Time) (HostCallRecord, error) {
	return j.update(identity, grantFingerprint, requestDigest, now, `state = 'admitted'`, "state = 'received'", nil)
}

func (j *SQLiteHostCallJournal) Complete(identity HostCallIdentity, grantFingerprint, requestDigest, result string, now time.Time) (HostCallRecord, error) {
	if err := validateHostCallResult(result); err != nil {
		return HostCallRecord{}, err
	}
	return j.update(identity, grantFingerprint, requestDigest, now, `state = 'completed', result = ?, result_digest = ?`, "state IN ('received','admitted')", []any{result, SchemaDigest([]byte(result))})
}

func (j *SQLiteHostCallJournal) MarkUnknown(identity HostCallIdentity, grantFingerprint, requestDigest string, now time.Time) (HostCallRecord, error) {
	return j.update(identity, grantFingerprint, requestDigest, now, `state = 'unknown'`, "state IN ('received','admitted')", nil)
}

func (j *SQLiteHostCallJournal) update(identity HostCallIdentity, grantFingerprint, requestDigest string, now time.Time, setClause, allowedStates string, setArgs []any) (HostCallRecord, error) {
	if j == nil || j.db == nil {
		return HostCallRecord{}, fmt.Errorf("host call journal is unavailable")
	}
	if err := validateHostCallInputs(identity, grantFingerprint, requestDigest); err != nil {
		return HostCallRecord{}, err
	}
	key := hostCallKey(identity)
	args := append(append([]any(nil), setArgs...), hostCallTime(now), key, grantFingerprint, requestDigest)
	query := `UPDATE semantic_host_calls SET ` + setClause + `, updated_at = ? WHERE call_key = ? AND grant_fingerprint = ? AND request_digest = ? AND ` + allowedStates
	result, err := j.db.Exec(query, args...)
	if err != nil {
		return HostCallRecord{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return HostCallRecord{}, err
	}
	record, err := j.get(key)
	if err != nil {
		return HostCallRecord{}, err
	}
	if changed != 1 {
		return record, fmt.Errorf("host_call_not_transitionable")
	}
	return record, nil
}

func (j *SQLiteHostCallJournal) ReconcileStale(now time.Time, maxAge time.Duration) (int, error) {
	if j == nil || j.db == nil || maxAge <= 0 {
		return 0, fmt.Errorf("host call journal maximum running age must be positive")
	}
	result, err := j.db.Exec(`UPDATE semantic_host_calls SET state = 'unknown', updated_at = ? WHERE state IN ('received','admitted') AND updated_at <= ?`, hostCallTime(now), hostCallTime(now.UTC().Add(-maxAge)))
	if err != nil {
		return 0, err
	}
	changed, err := result.RowsAffected()
	return int(changed), err
}

func (j *SQLiteHostCallJournal) get(key string) (HostCallRecord, error) {
	if j == nil || j.db == nil {
		return HostCallRecord{}, fmt.Errorf("host call journal is unavailable")
	}
	var record HostCallRecord
	var created, updated string
	err := j.db.QueryRow(`SELECT protocol, connection_id, call_id, surface_epoch, grant_fingerprint, request_digest, state, result, result_digest, created_at, updated_at FROM semantic_host_calls WHERE call_key = ?`, key).Scan(&record.Identity.Protocol, &record.Identity.ConnectionID, &record.Identity.CallID, &record.Identity.SurfaceEpoch, &record.GrantFingerprint, &record.RequestDigest, &record.State, &record.Result, &record.ResultDigest, &created, &updated)
	if err != nil {
		return HostCallRecord{}, err
	}
	record.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	record.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return record, nil
}

func hostCallTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
