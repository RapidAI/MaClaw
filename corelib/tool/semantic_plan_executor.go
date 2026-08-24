package tool

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var ErrPlanExecutionNotFound = errors.New("plan execution not found")

// PlanExecutionState is a durable execution fact, distinct from the signed
// invocation grant. A consumed grant authorizes one attempt; it never proves
// that a provider completed its effect.
type PlanExecutionState string

const (
	PlanExecutionRunning PlanExecutionState = "running"
	// PlanExecutionAwaitingReceipt means the local adapter prepared a durable
	// external operation, but no trusted transport receipt has been observed.
	// It is terminal for this selection attempt and, unlike succeeded, never
	// satisfies a DAG dependency or projects completion into a new revision.
	PlanExecutionAwaitingReceipt PlanExecutionState = "awaiting_receipt"
	PlanExecutionSucceeded       PlanExecutionState = "succeeded"
	PlanExecutionFailed          PlanExecutionState = "failed"
	PlanExecutionUnknown         PlanExecutionState = "unknown"
	// PlanExecutionRunningLease bounds only recovery bookkeeping. Expiry makes
	// a record unknown; it never authorizes a repeat provider invocation.
	PlanExecutionRunningLease = 5 * time.Minute
)

type PlanExecutionRecord struct {
	Scope        InvocationScope
	SelectionID  string
	State        PlanExecutionState
	ResultDigest string
	ReasonCode   string
	StartedAt    time.Time
	UpdatedAt    time.Time
}

// PlanExecutionStore is the durable owner of a plan selection's run state.
// Acquire must be conditional: only its winner may invoke the provider. A
// process crash leaves running for explicit reconciliation, never auto-retry.
type PlanExecutionStore interface {
	Acquire(PlanExecutionRecord) (PlanExecutionRecord, bool, error)
	Complete(scope InvocationScope, selectionID string, state PlanExecutionState, resultDigest, reasonCode string, now time.Time) (PlanExecutionRecord, error)
	// SettleAwaitingReceipt is reserved for a trusted external-operation
	// reconciler. It can advance an awaiting_receipt selection, or resolve an
	// unknown selection when a late authoritative receipt arrives. It never
	// reopens a known failed/succeeded selection or creates execution authority.
	SettleAwaitingReceipt(scope InvocationScope, selectionID string, state PlanExecutionState, resultDigest, reasonCode string, now time.Time) (PlanExecutionRecord, error)
	// Execution exposes durable attempt state for host-surface recovery. It
	// returns no provider result or invocation authority; callers use it only
	// to retire a model function whose one-time grant was already consumed.
	Execution(scope InvocationScope, selectionID string) (PlanExecutionRecord, error)
	Succeeded(scope InvocationScope) (map[string]bool, error)
	ReconcileStaleRunning(now time.Time, maxAge time.Duration) (int, error)
}

type memoryPlanExecutionStore struct {
	mu      sync.Mutex
	records map[string]PlanExecutionRecord
}

// NewMemoryPlanExecutionStore is suitable only for unit tests and explicitly
// single-process development. Restartable hosts must use a durable store.
func NewMemoryPlanExecutionStore() PlanExecutionStore {
	return &memoryPlanExecutionStore{records: make(map[string]PlanExecutionRecord)}
}

func (s *memoryPlanExecutionStore) Acquire(record PlanExecutionRecord) (PlanExecutionRecord, bool, error) {
	if s == nil {
		return PlanExecutionRecord{}, false, fmt.Errorf("plan execution store is unavailable")
	}
	if err := validatePlanExecutionRecord(record); err != nil {
		return PlanExecutionRecord{}, false, err
	}
	key := planExecutionKey(record.Scope, record.SelectionID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, exists := s.records[key]; exists {
		return current, false, nil
	}
	if record.StartedAt.IsZero() {
		record.StartedAt = time.Now().UTC()
	}
	record.UpdatedAt, record.State = record.StartedAt.UTC(), PlanExecutionRunning
	s.records[key] = record
	return record, true, nil
}

func (s *memoryPlanExecutionStore) Complete(scope InvocationScope, selectionID string, state PlanExecutionState, resultDigest, reasonCode string, now time.Time) (PlanExecutionRecord, error) {
	if s == nil {
		return PlanExecutionRecord{}, fmt.Errorf("plan execution store is unavailable")
	}
	key := planExecutionKey(scope, selectionID)
	s.mu.Lock()
	defer s.mu.Unlock()
	record, exists := s.records[key]
	if !exists {
		return PlanExecutionRecord{}, fmt.Errorf("plan execution record not found")
	}
	if record.State != PlanExecutionRunning {
		return record, nil
	}
	record.State, record.ResultDigest, record.ReasonCode, record.UpdatedAt = state, resultDigest, strings.TrimSpace(reasonCode), now.UTC()
	s.records[key] = record
	return record, nil
}

func (s *memoryPlanExecutionStore) SettleAwaitingReceipt(scope InvocationScope, selectionID string, state PlanExecutionState, resultDigest, reasonCode string, now time.Time) (PlanExecutionRecord, error) {
	if s == nil {
		return PlanExecutionRecord{}, fmt.Errorf("plan execution store is unavailable")
	}
	if !planExecutionReceiptSettlementState(state) {
		return PlanExecutionRecord{}, fmt.Errorf("plan execution receipt settlement state is invalid")
	}
	key := planExecutionKey(scope, selectionID)
	s.mu.Lock()
	defer s.mu.Unlock()
	record, exists := s.records[key]
	if !exists {
		return PlanExecutionRecord{}, ErrPlanExecutionNotFound
	}
	if record.State != PlanExecutionAwaitingReceipt && record.State != PlanExecutionUnknown {
		return record, nil
	}
	record.State, record.ResultDigest, record.ReasonCode, record.UpdatedAt = state, resultDigest, strings.TrimSpace(reasonCode), now.UTC()
	s.records[key] = record
	return record, nil
}

func (s *memoryPlanExecutionStore) Execution(scope InvocationScope, selectionID string) (PlanExecutionRecord, error) {
	if s == nil {
		return PlanExecutionRecord{}, fmt.Errorf("plan execution store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, exists := s.records[planExecutionKey(scope, selectionID)]
	if !exists {
		return PlanExecutionRecord{}, ErrPlanExecutionNotFound
	}
	return record, nil
}

func (s *memoryPlanExecutionStore) Succeeded(scope InvocationScope) (map[string]bool, error) {
	if s == nil {
		return nil, fmt.Errorf("plan execution store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	completed := make(map[string]bool)
	for _, record := range s.records {
		if record.Scope == scope && record.State == PlanExecutionSucceeded {
			completed[record.SelectionID] = true
		}
	}
	return completed, nil
}

func (s *memoryPlanExecutionStore) ReconcileStaleRunning(now time.Time, maxAge time.Duration) (int, error) {
	if s == nil || maxAge <= 0 {
		return 0, fmt.Errorf("plan execution maximum running age must be positive")
	}
	cutoff, changed := now.UTC().Add(-maxAge), 0
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, record := range s.records {
		if record.State == PlanExecutionRunning && !record.UpdatedAt.After(cutoff) {
			record.State, record.ReasonCode, record.UpdatedAt = PlanExecutionUnknown, "operation_lease_expired", now.UTC()
			s.records[key] = record
			changed++
		}
	}
	return changed, nil
}

// SelectionExecutionResult is the raw provider result plus a governed outcome
// classification. Result remains local until the host's result projector runs.
type SelectionExecutionResult struct {
	Result    string
	Succeeded bool
	// AwaitingReceipt is the generic execution-plane boundary for an external
	// operation that has been durably prepared but not observed as accepted or
	// failed by its trusted transport adapter. It is deliberately distinct from
	// both success and unknown: the local preparation is known, while the
	// remote outcome has not occurred (or has not yet been reconciled).
	AwaitingReceipt bool
	// Unknown means the provider may have accepted an externally observable
	// operation before its result was lost. It is terminal for this plan
	// revision: the executor records unknown and never turns a retry into a
	// fresh call.
	Unknown    bool
	ReasonCode string
}

type SelectionExecutor func(PlannedSelection) SelectionExecutionResult

// PlanExecutor combines grant admission with durable DAG execution facts. It
// deliberately does not decide parameter authorization, receipts, retry class
// or artifact egress; those remain explicit host boundaries during migration.
type PlanExecutor struct {
	issuer *InvocationIssuer
	store  PlanExecutionStore
	routes RouteStateStore
	// atomic, when the store pair supports it, commits a terminal execution
	// state and the route completion it projects together. It is nil for store
	// pairs that cannot span one transaction, in which case the two writes stay
	// sequential and the window between them remains.
	atomic atomicSelectionCompleter
	now    func() time.Time
}

func NewPlanExecutor(issuer *InvocationIssuer, store PlanExecutionStore) (*PlanExecutor, error) {
	return NewPlanExecutorWithRouteState(issuer, store, nil)
}

// NewPlanExecutorWithRouteState rejects calls from a superseded published
// revision before grant consumption. Passing nil retains compatibility for
// migration families that have not yet adopted CompareAndPublish.
func NewPlanExecutorWithRouteState(issuer *InvocationIssuer, store PlanExecutionStore, routes RouteStateStore) (*PlanExecutor, error) {
	if issuer == nil || store == nil {
		return nil, fmt.Errorf("plan executor requires issuer and store")
	}
	executor := &PlanExecutor{issuer: issuer, store: store, routes: routes, now: func() time.Time { return time.Now().UTC() }}
	if routes != nil {
		executor.atomic = newAtomicSelectionCompleter(store, routes)
	}
	return executor, nil
}

// Completed returns only durably succeeded selection dependencies for this
// exact invocation scope. A host may merge separate trusted confirmation facts
// afterwards, but callback order and model text cannot appear here.
func (e *PlanExecutor) Completed(scope InvocationScope) (map[string]bool, error) {
	if e == nil || e.store == nil {
		return nil, fmt.Errorf("plan executor is unavailable")
	}
	return e.store.Succeeded(scope)
}

// Execution returns only durable attempt state for one immutable selection.
// It never returns a result payload, callable grant, provider binding, or
// authorization to retry.
func (e *PlanExecutor) Execution(scope InvocationScope, selectionID string) (PlanExecutionRecord, error) {
	if e == nil || e.store == nil {
		return PlanExecutionRecord{}, fmt.Errorf("plan executor is unavailable")
	}
	return e.store.Execution(scope, selectionID)
}

// SettleAwaitingReceipt is the trusted transport-receipt boundary for an
// already admitted external-effect selection. It deliberately accepts no
// grant, adapter name, or model input: a receipt can settle only the exact
// durable selection that previously entered awaiting_receipt. A successful
// settlement is projected into RouteState so a later plan revision sees the
// same completion fact as an ordinary successful selection.
func (e *PlanExecutor) SettleAwaitingReceipt(scope InvocationScope, selectionID string, state PlanExecutionState, resultDigest, reasonCode string) (PlanExecutionRecord, error) {
	if e == nil || e.store == nil {
		return PlanExecutionRecord{}, fmt.Errorf("plan executor is unavailable")
	}
	if e.atomic != nil {
		record, err := e.atomic.SettleSelectionReceipt(scope, scope.PlanID, selectionID, state, resultDigest, reasonCode, e.now().UTC())
		if err != nil {
			return PlanExecutionRecord{}, err
		}
		if record.State != state {
			return record, fmt.Errorf("plan execution receipt settlement conflict")
		}
		return record, nil
	}
	record, err := e.store.SettleAwaitingReceipt(scope, selectionID, state, resultDigest, reasonCode, e.now().UTC())
	if err != nil {
		return PlanExecutionRecord{}, err
	}
	if record.State != state {
		return record, fmt.Errorf("plan execution receipt settlement conflict")
	}
	if state == PlanExecutionSucceeded && e.routes != nil {
		if _, err := e.routes.RecordSelectionCompletion(scope, scope.PlanID, selectionID, e.now().UTC()); err != nil {
			return PlanExecutionRecord{}, fmt.Errorf("record settled route selection completion: %w", err)
		}
	}
	return record, nil
}

// Execute admits exactly one materialized selection. extraSatisfied can carry
// separately-verified confirmation/channel facts; successful predecessor
// selections always come from the execution store, not callback ordering.
func (e *PlanExecutor) Execute(grant InvocationGrant, scope InvocationScope, plan ToolPlan, extraSatisfied map[string]bool, invoke SelectionExecutor) (SelectionExecutionResult, PlannedSelection, error) {
	if e == nil || e.issuer == nil || e.store == nil || invoke == nil {
		return SelectionExecutionResult{}, PlannedSelection{}, fmt.Errorf("plan executor is unavailable")
	}
	if e.routes != nil {
		if err := e.routes.IsCurrent(scope); err != nil {
			return SelectionExecutionResult{}, PlannedSelection{}, err
		}
	}
	completed, err := e.store.Succeeded(scope)
	if err != nil {
		return SelectionExecutionResult{}, PlannedSelection{}, fmt.Errorf("load plan execution facts: %w", err)
	}
	for requirement, satisfied := range extraSatisfied {
		if satisfied {
			completed[requirement] = true
		}
	}
	selection, err := e.issuer.ValidateAndConsume(grant, scope, plan, completed)
	if err != nil {
		return SelectionExecutionResult{}, PlannedSelection{}, err
	}
	record, execute, err := e.store.Acquire(PlanExecutionRecord{Scope: scope, SelectionID: selection.ID, StartedAt: e.now().UTC()})
	if err != nil {
		return SelectionExecutionResult{}, selection, fmt.Errorf("acquire plan execution: %w", err)
	}
	if !execute {
		return planExecutionReplayResult(record), selection, nil
	}
	return e.executeAdmitted(selection, scope, plan, invoke)
}

// ExecuteAdmitted invokes an already admitted selection without changing
// durable state. A trusted SemanticExecutionCoordinator owns the surrounding
// pre-I/O admission and post-I/O completion transactions; keeping this method
// side-effect free with respect to persistence ensures external I/O remains
// strictly outside that transaction boundary.
func (e *PlanExecutor) ExecuteAdmitted(selection PlannedSelection, scope InvocationScope, plan ToolPlan, invoke SelectionExecutor) (SelectionExecutionResult, PlannedSelection, error) {
	if e == nil || e.store == nil || invoke == nil {
		return SelectionExecutionResult{}, selection, fmt.Errorf("plan executor is unavailable")
	}
	if e.routes != nil {
		if err := e.routes.IsCurrent(scope); err != nil {
			return SelectionExecutionResult{}, selection, err
		}
	}
	if strings.TrimSpace(selection.ID) == "" {
		return SelectionExecutionResult{}, selection, fmt.Errorf("admitted selection is required")
	}
	record, err := e.store.Execution(scope, selection.ID)
	if err != nil {
		return SelectionExecutionResult{}, selection, fmt.Errorf("load admitted plan execution: %w", err)
	}
	if record.State != PlanExecutionRunning {
		return planExecutionReplayResult(record), selection, nil
	}
	return invoke(selection), selection, nil
}

func (e *PlanExecutor) executeAdmitted(selection PlannedSelection, scope InvocationScope, plan ToolPlan, invoke SelectionExecutor) (SelectionExecutionResult, PlannedSelection, error) {
	result := invoke(selection)
	state := PlanExecutionSucceeded
	if result.Unknown {
		state = PlanExecutionUnknown
		result.Succeeded = false
		if strings.TrimSpace(result.ReasonCode) == "" {
			result.ReasonCode = "selection_execution_unknown"
		}
	} else if result.AwaitingReceipt {
		state = PlanExecutionAwaitingReceipt
		result.Succeeded = false
		if strings.TrimSpace(result.ReasonCode) == "" {
			result.ReasonCode = "selection_awaiting_receipt"
		}
	} else if !result.Succeeded {
		state = PlanExecutionFailed
		if strings.TrimSpace(result.ReasonCode) == "" {
			result.ReasonCode = "selection_execution_failed"
		}
	}
	if e.atomic != nil {
		// One commit for the terminal state and the route completion it
		// projects, so no recovery view can observe success without the
		// completion fact that success unlocks.
		if _, err := e.atomic.CompleteSelection(scope, plan.ID, selection.ID, state, SchemaDigest([]byte(result.Result)), result.ReasonCode, e.now().UTC()); err != nil {
			return SelectionExecutionResult{}, selection, fmt.Errorf("record plan execution result: %w", err)
		}
		return result, selection, nil
	}
	if _, err := e.store.Complete(scope, selection.ID, state, SchemaDigest([]byte(result.Result)), result.ReasonCode, e.now().UTC()); err != nil {
		// The provider may have completed, but it cannot be asserted without a
		// durable record. Make the uncertainty explicit rather than re-running.
		return SelectionExecutionResult{}, selection, fmt.Errorf("record plan execution result: %w", err)
	}
	if state == PlanExecutionSucceeded && e.routes != nil {
		// A route completion is a separate, deliberately narrow projection of a
		// durable selection result. It carries no grant/host-call/parameter data;
		// if its write fails, do not expose dependent work from an uncertain
		// recovery view.
		if _, err := e.routes.RecordSelectionCompletion(scope, plan.ID, selection.ID, e.now().UTC()); err != nil {
			return SelectionExecutionResult{}, selection, fmt.Errorf("record route selection completion: %w", err)
		}
	}
	return result, selection, nil
}

func planExecutionReplayResult(record PlanExecutionRecord) SelectionExecutionResult {
	switch record.State {
	case PlanExecutionSucceeded:
		return SelectionExecutionResult{Result: "[system rejected] selection_already_completed", ReasonCode: "selection_already_completed"}
	case PlanExecutionRunning:
		return SelectionExecutionResult{Result: "[system rejected] selection_execution_in_progress", ReasonCode: "selection_execution_in_progress"}
	case PlanExecutionAwaitingReceipt:
		return SelectionExecutionResult{Result: "[system rejected] selection_awaiting_receipt", ReasonCode: "selection_awaiting_receipt"}
	case PlanExecutionUnknown:
		return SelectionExecutionResult{Result: "[system rejected] selection_execution_unknown", ReasonCode: "selection_execution_unknown"}
	default:
		return SelectionExecutionResult{Result: "[system rejected] selection_execution_failed", ReasonCode: "selection_execution_failed"}
	}
}

func validatePlanExecutionRecord(record PlanExecutionRecord) error {
	if strings.TrimSpace(record.Scope.RootTaskID) == "" || strings.TrimSpace(record.Scope.PlanID) == "" || strings.TrimSpace(record.SelectionID) == "" {
		return fmt.Errorf("plan execution scope and selection are required")
	}
	return nil
}

func planExecutionKey(scope InvocationScope, selectionID string) string {
	return SchemaDigest([]byte(strings.Join([]string{scope.RootTaskID, scope.PlanID, scope.SessionID, scope.TurnID, scope.PrincipalID, strings.TrimSpace(selectionID)}, "\x00")))
}

// SQLitePlanExecutionStore persists selection outcomes for restartable hosts.
// The conditional INSERT is the admission point; stale running records must be
// reconciled to unknown, never silently acquired again.
type SQLitePlanExecutionStore struct{ db *sql.DB }

func NewSQLitePlanExecutionStore(dbPath string) (*SQLitePlanExecutionStore, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, fmt.Errorf("plan execution store path is required")
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, fmt.Errorf("create plan execution store directory: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &SQLitePlanExecutionStore{db: db}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLitePlanExecutionStore) init() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("plan execution store is unavailable")
	}
	for _, statement := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=FULL`,
		`PRAGMA busy_timeout=5000`,
		`CREATE TABLE IF NOT EXISTS semantic_plan_executions (
			execution_key TEXT PRIMARY KEY,
			root_task_id TEXT NOT NULL,
			plan_id TEXT NOT NULL,
			session_id TEXT NOT NULL,
			turn_id TEXT NOT NULL,
			principal_id TEXT NOT NULL,
			selection_id TEXT NOT NULL,
			state TEXT NOT NULL CHECK(state IN ('running','awaiting_receipt','succeeded','failed','unknown')),
			result_digest TEXT NOT NULL DEFAULT '',
			reason_code TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_semantic_plan_executions_scope ON semantic_plan_executions(root_task_id, plan_id, session_id, turn_id, principal_id, state)`,
	} {
		if _, err := s.db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLitePlanExecutionStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLitePlanExecutionStore) Acquire(record PlanExecutionRecord) (PlanExecutionRecord, bool, error) {
	if s == nil || s.db == nil {
		return PlanExecutionRecord{}, false, fmt.Errorf("plan execution store is unavailable")
	}
	if err := validatePlanExecutionRecord(record); err != nil {
		return PlanExecutionRecord{}, false, err
	}
	if record.StartedAt.IsZero() {
		record.StartedAt = time.Now().UTC()
	}
	record.UpdatedAt, record.State = record.StartedAt.UTC(), PlanExecutionRunning
	key := planExecutionKey(record.Scope, record.SelectionID)
	result, err := s.db.Exec(`INSERT OR IGNORE INTO semantic_plan_executions
		(execution_key, root_task_id, plan_id, session_id, turn_id, principal_id, selection_id, state, started_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		key, record.Scope.RootTaskID, record.Scope.PlanID, record.Scope.SessionID, record.Scope.TurnID, record.Scope.PrincipalID,
		record.SelectionID, record.State, planExecutionTime(record.StartedAt), planExecutionTime(record.UpdatedAt))
	if err != nil {
		return PlanExecutionRecord{}, false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return PlanExecutionRecord{}, false, err
	}
	if changed == 1 {
		return record, true, nil
	}
	current, err := s.get(key)
	return current, false, err
}

func (s *SQLitePlanExecutionStore) Complete(scope InvocationScope, selectionID string, state PlanExecutionState, resultDigest, reasonCode string, now time.Time) (PlanExecutionRecord, error) {
	if s == nil || s.db == nil {
		return PlanExecutionRecord{}, fmt.Errorf("plan execution store is unavailable")
	}
	key := planExecutionKey(scope, selectionID)
	result, err := s.db.Exec(`UPDATE semantic_plan_executions SET state = ?, result_digest = ?, reason_code = ?, updated_at = ? WHERE execution_key = ? AND state = 'running'`, state, resultDigest, strings.TrimSpace(reasonCode), planExecutionTime(now), key)
	if err != nil {
		return PlanExecutionRecord{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return PlanExecutionRecord{}, err
	}
	if changed == 0 {
		return s.get(key)
	}
	return s.get(key)
}

func (s *SQLitePlanExecutionStore) SettleAwaitingReceipt(scope InvocationScope, selectionID string, state PlanExecutionState, resultDigest, reasonCode string, now time.Time) (PlanExecutionRecord, error) {
	if s == nil || s.db == nil {
		return PlanExecutionRecord{}, fmt.Errorf("plan execution store is unavailable")
	}
	if !planExecutionReceiptSettlementState(state) {
		return PlanExecutionRecord{}, fmt.Errorf("plan execution receipt settlement state is invalid")
	}
	key := planExecutionKey(scope, selectionID)
	result, err := s.db.Exec(`UPDATE semantic_plan_executions SET state = ?, result_digest = ?, reason_code = ?, updated_at = ? WHERE execution_key = ? AND state IN ('awaiting_receipt', 'unknown')`, state, resultDigest, strings.TrimSpace(reasonCode), planExecutionTime(now), key)
	if err != nil {
		return PlanExecutionRecord{}, err
	}
	if changed, err := result.RowsAffected(); err != nil || changed == 0 {
		return s.get(key)
	}
	return s.get(key)
}

func planExecutionReceiptSettlementState(state PlanExecutionState) bool {
	switch state {
	case PlanExecutionSucceeded, PlanExecutionFailed, PlanExecutionUnknown:
		return true
	default:
		return false
	}
}

func (s *SQLitePlanExecutionStore) Execution(scope InvocationScope, selectionID string) (PlanExecutionRecord, error) {
	if s == nil || s.db == nil {
		return PlanExecutionRecord{}, fmt.Errorf("plan execution store is unavailable")
	}
	record, err := s.get(planExecutionKey(scope, selectionID))
	if errors.Is(err, sql.ErrNoRows) {
		return PlanExecutionRecord{}, ErrPlanExecutionNotFound
	}
	return record, err
}

func (s *SQLitePlanExecutionStore) Succeeded(scope InvocationScope) (map[string]bool, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("plan execution store is unavailable")
	}
	rows, err := s.db.Query(`SELECT selection_id FROM semantic_plan_executions WHERE root_task_id = ? AND plan_id = ? AND session_id = ? AND turn_id = ? AND principal_id = ? AND state = 'succeeded'`, scope.RootTaskID, scope.PlanID, scope.SessionID, scope.TurnID, scope.PrincipalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	completed := make(map[string]bool)
	for rows.Next() {
		var selectionID string
		if err := rows.Scan(&selectionID); err != nil {
			return nil, err
		}
		completed[selectionID] = true
	}
	return completed, rows.Err()
}

func (s *SQLitePlanExecutionStore) ReconcileStaleRunning(now time.Time, maxAge time.Duration) (int, error) {
	if s == nil || s.db == nil || maxAge <= 0 {
		return 0, fmt.Errorf("plan execution maximum running age must be positive")
	}
	result, err := s.db.Exec(`UPDATE semantic_plan_executions SET state = 'unknown', reason_code = 'operation_lease_expired', updated_at = ? WHERE state = 'running' AND updated_at <= ?`, planExecutionTime(now), planExecutionTime(now.UTC().Add(-maxAge)))
	if err != nil {
		return 0, err
	}
	changed, err := result.RowsAffected()
	return int(changed), err
}

func (s *SQLitePlanExecutionStore) get(key string) (PlanExecutionRecord, error) {
	return planExecutionRecordFrom(s.db, key)
}

// planExecutionRecordFrom reads one execution row from either the store's
// database or an open transaction. A caller holding a transaction must pass it:
// the store keeps a single connection, so a database-level read taken while the
// transaction is open would block on that transaction.
func planExecutionRecordFrom(q routeStateRowQuerier, key string) (PlanExecutionRecord, error) {
	var record PlanExecutionRecord
	var started, updated string
	err := q.QueryRow(`SELECT root_task_id, plan_id, session_id, turn_id, principal_id, selection_id, state, result_digest, reason_code, started_at, updated_at FROM semantic_plan_executions WHERE execution_key = ?`, key).Scan(
		&record.Scope.RootTaskID, &record.Scope.PlanID, &record.Scope.SessionID, &record.Scope.TurnID, &record.Scope.PrincipalID,
		&record.SelectionID, &record.State, &record.ResultDigest, &record.ReasonCode, &started, &updated)
	if err != nil {
		return PlanExecutionRecord{}, err
	}
	record.StartedAt, _ = time.Parse(time.RFC3339Nano, started)
	record.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return record, nil
}

func planExecutionTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
