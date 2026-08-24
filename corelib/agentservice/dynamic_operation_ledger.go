package agentservice

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// DynamicOperationState is deliberately small. Dynamic providers cannot prove
// that a transport error had no external effect, so errors after dispatch are
// unknown until a provider-specific reconciliation capability exists.
type DynamicOperationState string

const (
	DynamicOperationRunning DynamicOperationState = "running"
	// DynamicOperationAwaitingReceipt means dispatch completed locally but the
	// host has not observed a trusted remote receipt. It is terminal for the
	// dispatch attempt: no caller may use it as permission to dispatch again.
	DynamicOperationAwaitingReceipt DynamicOperationState = "awaiting_receipt"
	DynamicOperationSucceeded       DynamicOperationState = "succeeded"
	DynamicOperationFailed          DynamicOperationState = "failed"
	DynamicOperationUnknown         DynamicOperationState = "unknown"

	// DynamicOperationRunningLease is deliberately conservative: it only
	// converts records left by a crashed/restarted host to unknown. A caller
	// must reconcile unknown effects instead of treating lease expiry as proof
	// that the provider was not reached.
	DynamicOperationRunningLease = 5 * time.Minute
)

type DynamicOperationRecord struct {
	Key           string
	TenantID      string
	UserID        string
	SessionID     string
	AdapterKind   string
	BindingID     string
	RequestDigest string
	// The following fields are populated only for a receipt-bound semantic
	// effect. They are the durable operation-to-plan mapping consumed by a
	// trusted reconciliation worker. In particular, a worker must not accept
	// caller-supplied scope/selection values and then try to prove them from a
	// mutable current catalog.
	InvocationPlanID      string
	InvocationSessionID   string
	InvocationTurnID      string
	InvocationPrincipalID string
	SelectionID           string
	SelectionDigest       string
	State                 DynamicOperationState
	// ResultDigest is the digest of the bounded local provider result. It is
	// not a remote receipt and must never be used as receipt evidence.
	ResultDigest string
	// ReceiptDigest binds a trusted external acceptance receipt to this logical
	// operation. It is written only by a settlement that won the conditional
	// awaiting_receipt transition.
	ReceiptDigest string
	ReasonCode    string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// DynamicOperationLedger is the compatibility bridge to the eventual
// PlanExecutor ledger. Acquire must be atomic across all callers sharing the
// implementation; no caller may invoke a provider unless it acquired running.
type DynamicOperationLedger interface {
	Acquire(DynamicOperationRecord) (DynamicOperationRecord, bool, error)
	Complete(key string, state DynamicOperationState, resultDigest, reasonCode string, now time.Time) (DynamicOperationRecord, error)
	// Settle records a trusted receipt for an operation that was previously
	// prepared/awaiting or unknown. A late authoritative receipt may resolve an
	// uncertain dispatch, but this method never reopens a known failed/succeeded
	// operation or redispatches it.
	Settle(key string, state DynamicOperationState, resultDigest, receiptDigest, reasonCode string, now time.Time) (DynamicOperationRecord, error)
	Get(key string) (DynamicOperationRecord, error)
	ReconcileStaleRunning(now time.Time, maxAge time.Duration) (int, error)
}

var ErrDynamicOperationNotFound = errors.New("dynamic operation not found")

type memoryDynamicOperationLedger struct {
	mu      sync.Mutex
	records map[string]DynamicOperationRecord
}

func NewMemoryDynamicOperationLedger() DynamicOperationLedger {
	return &memoryDynamicOperationLedger{records: make(map[string]DynamicOperationRecord)}
}

func (l *memoryDynamicOperationLedger) Acquire(record DynamicOperationRecord) (DynamicOperationRecord, bool, error) {
	if strings.TrimSpace(record.Key) == "" {
		return DynamicOperationRecord{}, false, fmt.Errorf("dynamic operation key is required")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if current, ok := l.records[record.Key]; ok {
		return current, false, nil
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	record.UpdatedAt = record.CreatedAt
	record.State = DynamicOperationRunning
	l.records[record.Key] = record
	return record, true, nil
}

func (l *memoryDynamicOperationLedger) Complete(key string, state DynamicOperationState, resultDigest, reasonCode string, now time.Time) (DynamicOperationRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	record, ok := l.records[key]
	if !ok {
		return DynamicOperationRecord{}, ErrDynamicOperationNotFound
	}
	if record.State != DynamicOperationRunning {
		return record, nil
	}
	record.State, record.ResultDigest, record.ReasonCode, record.UpdatedAt = state, resultDigest, strings.TrimSpace(reasonCode), now.UTC()
	l.records[key] = record
	return record, nil
}

func (l *memoryDynamicOperationLedger) Settle(key string, state DynamicOperationState, resultDigest, receiptDigest, reasonCode string, now time.Time) (DynamicOperationRecord, error) {
	if !dynamicOperationSettlementState(state) {
		return DynamicOperationRecord{}, fmt.Errorf("dynamic operation settlement state is invalid")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	record, ok := l.records[key]
	if !ok {
		return DynamicOperationRecord{}, ErrDynamicOperationNotFound
	}
	if record.State != DynamicOperationRunning && record.State != DynamicOperationAwaitingReceipt && record.State != DynamicOperationUnknown {
		return record, nil
	}
	if state == DynamicOperationSucceeded && strings.TrimSpace(receiptDigest) == "" {
		return DynamicOperationRecord{}, fmt.Errorf("dynamic operation success receipt is required")
	}
	record.State, record.ResultDigest, record.ReceiptDigest, record.ReasonCode, record.UpdatedAt = state, resultDigest, strings.TrimSpace(receiptDigest), strings.TrimSpace(reasonCode), now.UTC()
	l.records[key] = record
	return record, nil
}

func (l *memoryDynamicOperationLedger) Get(key string) (DynamicOperationRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	record, ok := l.records[key]
	if !ok {
		return DynamicOperationRecord{}, ErrDynamicOperationNotFound
	}
	return record, nil
}

func (l *memoryDynamicOperationLedger) ReconcileStaleRunning(now time.Time, maxAge time.Duration) (int, error) {
	if maxAge <= 0 {
		return 0, fmt.Errorf("dynamic operation maximum running age must be positive")
	}
	cutoff := now.UTC().Add(-maxAge)
	l.mu.Lock()
	defer l.mu.Unlock()
	changed := 0
	for key, record := range l.records {
		if record.State != DynamicOperationRunning || record.UpdatedAt.After(cutoff) {
			continue
		}
		record.State = DynamicOperationUnknown
		record.ReasonCode = "operation_lease_expired"
		record.UpdatedAt = now.UTC()
		l.records[key] = record
		changed++
	}
	return changed, nil
}

type SQLiteDynamicOperationLedger struct{ db *sql.DB }

func NewSQLiteDynamicOperationLedger(path string) (*SQLiteDynamicOperationLedger, error) {
	if err := secureMkdirAll(filepath.Dir(path)); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	ledger := &SQLiteDynamicOperationLedger{db: db}
	for _, stmt := range []string{
		`PRAGMA journal_mode=WAL`, `PRAGMA synchronous=FULL`,
		`CREATE TABLE IF NOT EXISTS dynamic_operation_ledger (
			operation_key TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, user_id TEXT NOT NULL,
			session_id TEXT NOT NULL, adapter_kind TEXT NOT NULL, binding_id TEXT NOT NULL,
			request_digest TEXT NOT NULL, invocation_plan_id TEXT NOT NULL DEFAULT '', invocation_session_id TEXT NOT NULL DEFAULT '',
			invocation_turn_id TEXT NOT NULL DEFAULT '', invocation_principal_id TEXT NOT NULL DEFAULT '', selection_id TEXT NOT NULL DEFAULT '', selection_digest TEXT NOT NULL DEFAULT '',
			state TEXT NOT NULL, result_digest TEXT NOT NULL DEFAULT '', receipt_digest TEXT NOT NULL DEFAULT '',
			reason_code TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_dynamic_operation_scope ON dynamic_operation_ledger(tenant_id, user_id, session_id, updated_at)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	for _, statement := range []string{
		`ALTER TABLE dynamic_operation_ledger ADD COLUMN receipt_digest TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE dynamic_operation_ledger ADD COLUMN invocation_plan_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE dynamic_operation_ledger ADD COLUMN invocation_session_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE dynamic_operation_ledger ADD COLUMN invocation_turn_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE dynamic_operation_ledger ADD COLUMN invocation_principal_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE dynamic_operation_ledger ADD COLUMN selection_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE dynamic_operation_ledger ADD COLUMN selection_digest TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := db.Exec(statement); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			_ = db.Close()
			return nil, err
		}
	}
	return ledger, nil
}

func (l *SQLiteDynamicOperationLedger) Close() error {
	if l == nil || l.db == nil {
		return nil
	}
	return l.db.Close()
}

func (l *SQLiteDynamicOperationLedger) Acquire(record DynamicOperationRecord) (DynamicOperationRecord, bool, error) {
	if l == nil || l.db == nil {
		return DynamicOperationRecord{}, false, fmt.Errorf("dynamic operation ledger is unavailable")
	}
	if strings.TrimSpace(record.Key) == "" {
		return DynamicOperationRecord{}, false, fmt.Errorf("dynamic operation key is required")
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	record.UpdatedAt, record.State = record.CreatedAt, DynamicOperationRunning
	res, err := l.db.Exec(`INSERT OR IGNORE INTO dynamic_operation_ledger(operation_key, tenant_id, user_id, session_id, adapter_kind, binding_id, request_digest, invocation_plan_id, invocation_session_id, invocation_turn_id, invocation_principal_id, selection_id, selection_digest, state, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, record.Key, record.TenantID, record.UserID, record.SessionID, record.AdapterKind, record.BindingID, record.RequestDigest, record.InvocationPlanID, record.InvocationSessionID, record.InvocationTurnID, record.InvocationPrincipalID, record.SelectionID, record.SelectionDigest, record.State, operationLedgerTime(record.CreatedAt), operationLedgerTime(record.UpdatedAt))
	if err != nil {
		return DynamicOperationRecord{}, false, err
	}
	inserted, err := res.RowsAffected()
	if err != nil {
		return DynamicOperationRecord{}, false, err
	}
	if inserted == 1 {
		return record, true, nil
	}
	current, err := l.Get(record.Key)
	return current, false, err
}

func (l *SQLiteDynamicOperationLedger) Complete(key string, state DynamicOperationState, resultDigest, reasonCode string, now time.Time) (DynamicOperationRecord, error) {
	if l == nil || l.db == nil {
		return DynamicOperationRecord{}, fmt.Errorf("dynamic operation ledger is unavailable")
	}
	res, err := l.db.Exec(`UPDATE dynamic_operation_ledger SET state=?, result_digest=?, reason_code=?, updated_at=? WHERE operation_key=? AND state=?`, state, resultDigest, reasonCode, operationLedgerTime(now), key, DynamicOperationRunning)
	if err != nil {
		return DynamicOperationRecord{}, err
	}
	if affected, err := res.RowsAffected(); err != nil || affected == 0 {
		return l.Get(key)
	}
	return l.Get(key)
}

func (l *SQLiteDynamicOperationLedger) Settle(key string, state DynamicOperationState, resultDigest, receiptDigest, reasonCode string, now time.Time) (DynamicOperationRecord, error) {
	if l == nil || l.db == nil {
		return DynamicOperationRecord{}, fmt.Errorf("dynamic operation ledger is unavailable")
	}
	if !dynamicOperationSettlementState(state) {
		return DynamicOperationRecord{}, fmt.Errorf("dynamic operation settlement state is invalid")
	}
	if state == DynamicOperationSucceeded && strings.TrimSpace(receiptDigest) == "" {
		return DynamicOperationRecord{}, fmt.Errorf("dynamic operation success receipt is required")
	}
	res, err := l.db.Exec(`UPDATE dynamic_operation_ledger SET state=?, result_digest=?, receipt_digest=?, reason_code=?, updated_at=? WHERE operation_key=? AND state IN (?, ?, ?)`, state, resultDigest, strings.TrimSpace(receiptDigest), strings.TrimSpace(reasonCode), operationLedgerTime(now), key, DynamicOperationRunning, DynamicOperationAwaitingReceipt, DynamicOperationUnknown)
	if err != nil {
		return DynamicOperationRecord{}, err
	}
	if affected, err := res.RowsAffected(); err != nil || affected == 0 {
		return l.Get(key)
	}
	return l.Get(key)
}

func dynamicOperationSettlementState(state DynamicOperationState) bool {
	switch state {
	case DynamicOperationSucceeded, DynamicOperationFailed, DynamicOperationUnknown:
		return true
	default:
		return false
	}
}

func (l *SQLiteDynamicOperationLedger) Get(key string) (DynamicOperationRecord, error) {
	row := l.db.QueryRow(`SELECT operation_key, tenant_id, user_id, session_id, adapter_kind, binding_id, request_digest, invocation_plan_id, invocation_session_id, invocation_turn_id, invocation_principal_id, selection_id, selection_digest, state, result_digest, receipt_digest, reason_code, created_at, updated_at FROM dynamic_operation_ledger WHERE operation_key=?`, key)
	var record DynamicOperationRecord
	var created, updated string
	if err := row.Scan(&record.Key, &record.TenantID, &record.UserID, &record.SessionID, &record.AdapterKind, &record.BindingID, &record.RequestDigest, &record.InvocationPlanID, &record.InvocationSessionID, &record.InvocationTurnID, &record.InvocationPrincipalID, &record.SelectionID, &record.SelectionDigest, &record.State, &record.ResultDigest, &record.ReceiptDigest, &record.ReasonCode, &created, &updated); errors.Is(err, sql.ErrNoRows) {
		return DynamicOperationRecord{}, ErrDynamicOperationNotFound
	} else if err != nil {
		return DynamicOperationRecord{}, err
	}
	record.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	record.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return record, nil
}

func (l *SQLiteDynamicOperationLedger) ReconcileStaleRunning(now time.Time, maxAge time.Duration) (int, error) {
	if l == nil || l.db == nil {
		return 0, fmt.Errorf("dynamic operation ledger is unavailable")
	}
	if maxAge <= 0 {
		return 0, fmt.Errorf("dynamic operation maximum running age must be positive")
	}
	res, err := l.db.Exec(`UPDATE dynamic_operation_ledger SET state=?, reason_code=?, updated_at=? WHERE state=? AND updated_at<=?`, DynamicOperationUnknown, "operation_lease_expired", operationLedgerTime(now), DynamicOperationRunning, operationLedgerTime(now.UTC().Add(-maxAge)))
	if err != nil {
		return 0, err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(changed), nil
}

func operationLedgerTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func dynamicOperationDigest(value interface{}) string {
	data, err := json.Marshal(value)
	if err != nil {
		data = []byte("invalid")
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func dynamicOperationKey(tenantID, userID, rootTaskID, adapterKind, bindingID, requestDigest string) string {
	// The adapter identity is intentionally excluded. It is short lived and
	// re-rendered on every exposure refresh; including it would make a host
	// retry with a new opaque name look like a new external operation. The
	// stable task identity, binding and canonical request digest define the
	// logical operation instead.
	return dynamicOperationDigest([]string{strings.TrimSpace(tenantID), strings.TrimSpace(userID), strings.TrimSpace(rootTaskID), strings.TrimSpace(adapterKind), strings.TrimSpace(bindingID), strings.TrimSpace(requestDigest)})
}

func dynamicOperationAdmission(ledger DynamicOperationLedger, now time.Time, tenantID, userID, rootTaskID, kind, bindingID string, args map[string]interface{}) (DynamicOperationRecord, bool, error) {
	if ledger == nil {
		return DynamicOperationRecord{}, false, fmt.Errorf("dynamic operation ledger is unavailable")
	}
	requestDigest := dynamicOperationDigest(args)
	record := DynamicOperationRecord{Key: dynamicOperationKey(tenantID, userID, rootTaskID, kind, bindingID, requestDigest), TenantID: tenantID, UserID: userID, SessionID: rootTaskID, AdapterKind: kind, BindingID: bindingID, RequestDigest: requestDigest, CreatedAt: now.UTC()}
	return ledger.Acquire(record)
}

// dynamicAdapterInvocationAdmission consumes the short-lived opaque adapter
// identity independently of the logical operation key. The former prevents a
// host from retrying the same rendered invocation with changed arguments; the
// latter prevents a fresh rendering from replaying the same external effect.
func dynamicAdapterInvocationAdmission(ledger DynamicOperationLedger, now time.Time, tenantID, userID, rootTaskID, kind, adapterName, bindingID string) (DynamicOperationRecord, bool, error) {
	if ledger == nil {
		return DynamicOperationRecord{}, false, fmt.Errorf("dynamic operation ledger is unavailable")
	}
	adapterDigest := dynamicOperationDigest([]string{"adapter", strings.TrimSpace(adapterName)})
	record := DynamicOperationRecord{
		Key:           dynamicOperationKey(tenantID, userID, rootTaskID, kind+"_adapter_grant", bindingID, adapterDigest),
		TenantID:      tenantID,
		UserID:        userID,
		SessionID:     rootTaskID,
		AdapterKind:   kind + "_adapter_grant",
		BindingID:     bindingID,
		RequestDigest: adapterDigest,
		CreatedAt:     now.UTC(),
	}
	return ledger.Acquire(record)
}
