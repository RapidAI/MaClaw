package tool

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SemanticExternalEffectState describes the durable lifecycle of an effect
// performed by a selected dynamic provider. It is intentionally separate from
// a provider response: only a trusted receipt may move an operation to
// succeeded.
type SemanticExternalEffectState string

const (
	SemanticExternalEffectRunning         SemanticExternalEffectState = "running"
	SemanticExternalEffectAwaitingReceipt SemanticExternalEffectState = "awaiting_receipt"
	SemanticExternalEffectSucceeded       SemanticExternalEffectState = "succeeded"
	SemanticExternalEffectFailed          SemanticExternalEffectState = "failed"
	SemanticExternalEffectUnknown         SemanticExternalEffectState = "unknown"
)

// SemanticExternalEffectOperation is the minimal, immutable operation binding
// required to prevent a dynamic Skill/MCP effect from being redispatched after
// a reconnect or a fresh adapter rendering. OperationKey is computed by the
// trusted dynamic adapter from the semantic task, binding and canonical,
// authorized request; callers cannot attach it to another selection.
type SemanticExternalEffectOperation struct {
	OperationKey    string
	Scope           InvocationScope
	TenantID        string
	UserID          string
	SelectionID     string
	SelectionDigest string
	BindingID       string
	RequestDigest   string
	State           SemanticExternalEffectState
	ResultDigest    string
	ReceiptDigest   string
	ReasonCode      string
	// FencingToken is the route-lineage fencing token stamped when the
	// operation was prepared. A newer published revision fences the operation
	// off from dispatch-outcome and receipt transitions; token 0 marks a
	// pre-fencing legacy row governed only by the current-revision checks.
	FencingToken uint64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (c *SQLiteSemanticExecutionCoordinator) initExternalEffects() error {
	if c == nil || c.db == nil {
		return fmt.Errorf("semantic execution coordinator is unavailable")
	}
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS semantic_external_effect_operations (
			operation_key TEXT PRIMARY KEY,
			root_task_id TEXT NOT NULL, plan_id TEXT NOT NULL, session_id TEXT NOT NULL, turn_id TEXT NOT NULL, principal_id TEXT NOT NULL,
			tenant_id TEXT NOT NULL DEFAULT '', user_id TEXT NOT NULL DEFAULT '',
			selection_id TEXT NOT NULL, selection_digest TEXT NOT NULL, binding_id TEXT NOT NULL, request_digest TEXT NOT NULL,
			state TEXT NOT NULL ` + semanticExternalEffectStateCheckConstraint() + `,
			result_digest TEXT NOT NULL DEFAULT '', receipt_digest TEXT NOT NULL DEFAULT '', reason_code TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_semantic_external_effect_operations_scope ON semantic_external_effect_operations(root_task_id, plan_id, session_id, turn_id, principal_id, selection_id, state)`,
		`CREATE TABLE IF NOT EXISTS semantic_external_effect_receipts (
			operation_key TEXT PRIMARY KEY, receipt_digest TEXT NOT NULL, accepted_at TEXT NOT NULL
		)`,
		// An out-of-band resolution is kept apart from the receipt table on
		// purpose. Both end an operation, but one was confirmed by the channel
		// and the other was asserted by a person, and no later reader should
		// have to guess which. The primary key admits one resolution per
		// operation, so a second, different verdict is a conflict rather than
		// an overwrite.
		`CREATE TABLE IF NOT EXISTS semantic_external_effect_resolutions (
			operation_key TEXT PRIMARY KEY, outcome TEXT NOT NULL CHECK(outcome IN ('succeeded','failed')),
			evidence_digest TEXT NOT NULL, resolved_by TEXT NOT NULL, resolved_at TEXT NOT NULL
		)`,
	} {
		if _, err := c.db.Exec(statement); err != nil {
			return err
		}
	}
	for _, statement := range []string{
		`ALTER TABLE semantic_external_effect_operations ADD COLUMN tenant_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE semantic_external_effect_operations ADD COLUMN user_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE semantic_external_effect_operations ADD COLUMN fencing_token INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := c.db.Exec(statement); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	return nil
}

// ReconcileStaleExternalEffects marks abandoned pre-dispatch operations as
// unknown. It never makes them dispatchable again: a later provider receipt
// may still settle the same operation, but restart recovery must not infer
// that no outward effect occurred.
//
// The operation and its selection execution share the same transaction. Host
// calls are reconciled by the journal with the same conservative unknown
// outcome; keeping the external-operation transition atomic with execution
// avoids an executable-looking running selection after a restart.
func (c *SQLiteSemanticExecutionCoordinator) ReconcileStaleExternalEffects(now time.Time, maxAge time.Duration) (int, error) {
	if c == nil || c.db == nil || maxAge <= 0 {
		return 0, fmt.Errorf("semantic external effect maximum running age must be positive")
	}
	now = now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := c.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	cutoff := now.Add(-maxAge).Format(time.RFC3339Nano)
	updated, err := tx.Exec(`UPDATE semantic_external_effect_operations
		SET state='unknown', reason_code='operation_lease_expired', updated_at=?
		WHERE state IN (`+semanticOperationLeaseSweptStatesSQL()+`) AND updated_at <= ?`, now.Format(time.RFC3339Nano), cutoff)
	if err != nil {
		return 0, err
	}
	changed, err := updated.RowsAffected()
	if err != nil {
		return 0, err
	}
	// All selected executions from an abandoned process are equally unsafe to
	// redispatch. This mirrors the plan-execution recovery rule while ensuring
	// the effect operation is transitioned first in this shared transaction.
	if _, err := tx.Exec(`UPDATE semantic_plan_executions
		SET state='unknown', reason_code='operation_lease_expired', updated_at=?
		WHERE state='running' AND updated_at <= ?`, now.Format(time.RFC3339Nano), cutoff); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int(changed), nil
}

// ExternalEffectReceiptLease bounds how long an operation may sit waiting for
// a receipt that is not coming.
//
// This was a day when the lease was introduced, chosen out of a fear of
// judging too early that the design had already made groundless: converging is
// not a verdict. unknown asserts strictly less than awaiting_receipt
// (dispatched, outcome not established), and SettleExternalEffectReceipt
// accepts both as starting states, so a receipt that arrives after the lease
// lapses still settles the operation normally
// (TestExpiredReceiptWaitStillAcceptsALateReceipt). Expiring early therefore
// costs nothing that can be lost.
//
// Expiring late does cost something. An operation still in awaiting_receipt is
// out of reach of the manual exit, which refuses a live expectation by design.
// A day of that is a day of an operator watching an operation they cannot
// resolve — the exact dead end this lease exists to open.
//
// The floor is human, not mechanical. An operation shown as unknown invites
// someone to rule on it, and a manual verdict races a receipt that may still
// be in flight. The lease must outlast a real integration's settle time (which
// is seconds) by enough margin that nobody is asked to adjudicate something
// still moving. An hour buys that margin without parking anything for a shift.
//
// It remains an uncalibrated ceiling in one respect: no production receipt
// latency has been measured. What changed is the direction of the guess.
const ExternalEffectReceiptLease = time.Hour

// ReconcileExpiredReceiptWaits converges an operation that has waited past the
// receipt lease from awaiting_receipt to unknown.
//
// Without this, awaiting_receipt is a state with no way out. Nothing else
// leaves it: the running-lease reconciler only looks at 'running', a receipt
// only arrives if some integration sends one, and manual resolution
// deliberately refuses to race a live expectation. An operation could
// therefore be dispatched, never confirmed, and stay that way forever.
//
// Converging gives up the expectation and nothing else. unknown says strictly
// less than awaiting_receipt -- dispatched, outcome not established -- and
// SettleExternalEffectReceipt accepts both as a starting point, so a receipt
// that arrives late still settles the operation exactly as it would have. What
// changes is that the operation becomes eligible for an out-of-band
// resolution, which is the only thing that can still reach it.
func (c *SQLiteSemanticExecutionCoordinator) ReconcileExpiredReceiptWaits(now time.Time, maxAge time.Duration) (int, error) {
	if c == nil || c.db == nil || maxAge <= 0 {
		return 0, fmt.Errorf("semantic external effect maximum receipt wait must be positive")
	}
	now = now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := c.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	stamp := now.Format(time.RFC3339Nano)
	cutoff := now.Add(-maxAge).Format(time.RFC3339Nano)
	type expiredWait struct {
		operationKey string
		executionKey string
	}
	rows, err := tx.Query(`SELECT operation_key, root_task_id, plan_id, session_id, turn_id, principal_id, selection_id
		FROM semantic_external_effect_operations WHERE state IN (`+semanticReceiptLeaseSweptStatesSQL()+`) AND updated_at <= ?`, cutoff)
	if err != nil {
		return 0, err
	}
	expired := make([]expiredWait, 0)
	for rows.Next() {
		var operationKey, selectionID string
		var scope InvocationScope
		if err := rows.Scan(&operationKey, &scope.RootTaskID, &scope.PlanID, &scope.SessionID, &scope.TurnID, &scope.PrincipalID, &selectionID); err != nil {
			_ = rows.Close()
			return 0, err
		}
		expired = append(expired, expiredWait{operationKey: operationKey, executionKey: planExecutionKey(scope, selectionID)})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	// Each execution is converged by its own key rather than by a sweep over
	// every awaiting_receipt row. Channel deliveries park their executions in
	// the same state, and they are settled by a different path with its own
	// lease; a blanket update here would quietly desynchronize them from
	// their delivery records.
	changed := 0
	for _, wait := range expired {
		if _, err := tx.Exec(`UPDATE semantic_plan_executions SET state='unknown', reason_code='receipt_lease_expired', updated_at=?
			WHERE execution_key=? AND state='awaiting_receipt'`, planExecutionTime(now), wait.executionKey); err != nil {
			return 0, err
		}
		updated, err := tx.Exec(`UPDATE semantic_external_effect_operations SET state='unknown', reason_code='receipt_lease_expired', updated_at=?
			WHERE operation_key=? AND state='awaiting_receipt'`, stamp, wait.operationKey)
		if err != nil {
			return 0, err
		}
		if n, _ := updated.RowsAffected(); n == 1 {
			changed++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return changed, nil
}

func validateSemanticExternalEffectOperation(operation SemanticExternalEffectOperation) error {
	if err := ValidateArtifactScope(operation.Scope); err != nil {
		return err
	}
	if strings.TrimSpace(operation.OperationKey) == "" || strings.TrimSpace(operation.TenantID) == "" || strings.TrimSpace(operation.UserID) == "" || strings.TrimSpace(operation.SelectionID) == "" || strings.TrimSpace(operation.SelectionDigest) == "" || strings.TrimSpace(operation.BindingID) == "" || strings.TrimSpace(operation.RequestDigest) == "" {
		return fmt.Errorf("semantic_external_effect_operation_invalid")
	}
	return nil
}

// PrepareExternalEffect durably acquires the logical external operation before
// provider I/O. It must be called only after Admit returned HostCallAcquireAdmit.
// A duplicate key is never dispatchable again, including after a new opaque
// adapter has been rendered.
func (c *SQLiteSemanticExecutionCoordinator) PrepareExternalEffect(admission SemanticExecutionAdmission, operation SemanticExternalEffectOperation) (SemanticExternalEffectOperation, bool, error) {
	if c == nil || c.db == nil {
		return SemanticExternalEffectOperation{}, false, fmt.Errorf("semantic execution coordinator is unavailable")
	}
	if err := admission.validate(); err != nil {
		return SemanticExternalEffectOperation{}, false, err
	}
	if operation.Scope != admission.Scope || strings.TrimSpace(operation.SelectionID) != strings.TrimSpace(admission.Selection.ID) {
		return SemanticExternalEffectOperation{}, false, fmt.Errorf("semantic_external_effect_scope_mismatch")
	}
	if err := validateSemanticExternalEffectOperation(operation); err != nil {
		return SemanticExternalEffectOperation{}, false, err
	}
	now := admission.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	operation.State, operation.CreatedAt, operation.UpdatedAt = SemanticExternalEffectRunning, now, now
	tx, err := c.db.Begin()
	if err != nil {
		return SemanticExternalEffectOperation{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	var hostState string
	if err := tx.QueryRow(`SELECT state FROM semantic_host_calls WHERE call_key=? AND grant_fingerprint=? AND request_digest=?`, hostCallKey(admission.Identity), InvocationGrantFingerprint(admission.Grant), admission.RequestDigest).Scan(&hostState); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SemanticExternalEffectOperation{}, false, fmt.Errorf("host_call_not_admitted")
		}
		return SemanticExternalEffectOperation{}, false, err
	}
	if hostState != string(HostCallAdmitted) {
		return SemanticExternalEffectOperation{}, false, fmt.Errorf("host_call_not_admitted")
	}
	// Stamp the current lineage fencing token so a newer route revision can
	// fence this operation off from later dispatch/receipt transitions.
	fencingToken, err := currentLineageFencingToken(tx, routeLineageKey(operation.Scope))
	if err != nil {
		return SemanticExternalEffectOperation{}, false, err
	}
	operation.FencingToken = fencingToken
	result, err := tx.Exec(`INSERT OR IGNORE INTO semantic_external_effect_operations(operation_key, root_task_id, plan_id, session_id, turn_id, principal_id, tenant_id, user_id, selection_id, selection_digest, binding_id, request_digest, state, fencing_token, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'running', ?, ?, ?)`, operation.OperationKey, operation.Scope.RootTaskID, operation.Scope.PlanID, operation.Scope.SessionID, operation.Scope.TurnID, operation.Scope.PrincipalID, operation.TenantID, operation.UserID, operation.SelectionID, operation.SelectionDigest, operation.BindingID, operation.RequestDigest, fencingToken, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return SemanticExternalEffectOperation{}, false, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return SemanticExternalEffectOperation{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return SemanticExternalEffectOperation{}, false, err
	}
	if inserted == 1 {
		return operation, true, nil
	}
	existing, err := c.ExternalEffectOperation(operation.OperationKey)
	if err != nil {
		return SemanticExternalEffectOperation{}, false, err
	}
	if !sameSemanticExternalEffectBinding(existing, operation) {
		return SemanticExternalEffectOperation{}, false, fmt.Errorf("semantic_external_effect_operation_conflict")
	}
	return existing, false, nil
}

// CompleteExternalEffectDispatch atomically records the post-I/O dispatch
// observation, plan state and host-call result. A crash can therefore expose
// only an admitted/running operation (recovered as unknown), never a provider
// response without the matching execution record.
func (c *SQLiteSemanticExecutionCoordinator) CompleteExternalEffectDispatch(admission SemanticExecutionAdmission, operationKey string, outcome SemanticExternalEffectState, result, reasonCode string, now time.Time) (SemanticExternalEffectOperation, error) {
	if c == nil || c.db == nil {
		return SemanticExternalEffectOperation{}, fmt.Errorf("semantic execution coordinator is unavailable")
	}
	if err := admission.validate(); err != nil {
		return SemanticExternalEffectOperation{}, err
	}
	if outcome != SemanticExternalEffectAwaitingReceipt && outcome != SemanticExternalEffectUnknown && outcome != SemanticExternalEffectFailed {
		return SemanticExternalEffectOperation{}, fmt.Errorf("semantic_external_effect_dispatch_outcome_invalid")
	}
	if err := validateHostCallResult(result); err != nil {
		return SemanticExternalEffectOperation{}, err
	}
	now = now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	state := PlanExecutionUnknown
	if outcome == SemanticExternalEffectAwaitingReceipt {
		state = PlanExecutionAwaitingReceipt
	} else if outcome == SemanticExternalEffectFailed {
		state = PlanExecutionFailed
	}
	tx, err := c.db.Begin()
	if err != nil {
		return SemanticExternalEffectOperation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	updated, err := tx.Exec(`UPDATE semantic_external_effect_operations SET state=?, result_digest=?, reason_code=?, updated_at=? WHERE operation_key=? AND root_task_id=? AND plan_id=? AND session_id=? AND turn_id=? AND principal_id=? AND selection_id=? AND state='running'`, outcome, SchemaDigest([]byte(result)), strings.TrimSpace(reasonCode), now.Format(time.RFC3339Nano), strings.TrimSpace(operationKey), admission.Scope.RootTaskID, admission.Scope.PlanID, admission.Scope.SessionID, admission.Scope.TurnID, admission.Scope.PrincipalID, admission.Selection.ID)
	if err != nil {
		return SemanticExternalEffectOperation{}, err
	}
	if n, _ := updated.RowsAffected(); n != 1 {
		return SemanticExternalEffectOperation{}, fmt.Errorf("semantic_external_effect_not_running")
	}
	// A dispatch outcome recorded under a superseded revision must not become
	// a durable awaiting_receipt intent; the transaction rolls back and
	// recovery converges the operation to unknown instead.
	var fencingToken uint64
	if err := tx.QueryRow(`SELECT fencing_token FROM semantic_external_effect_operations WHERE operation_key=?`, strings.TrimSpace(operationKey)).Scan(&fencingToken); err != nil {
		return SemanticExternalEffectOperation{}, err
	}
	lineageToken, err := currentLineageFencingToken(tx, routeLineageKey(admission.Scope))
	if err != nil {
		return SemanticExternalEffectOperation{}, err
	}
	if outboxFencingStale(fencingToken, lineageToken) {
		return SemanticExternalEffectOperation{}, fmt.Errorf("semantic_external_effect_fencing_stale")
	}
	updated, err = tx.Exec(`UPDATE semantic_plan_executions SET state=?, result_digest=?, reason_code=?, updated_at=? WHERE execution_key=? AND state='running'`, state, SchemaDigest([]byte(result)), strings.TrimSpace(reasonCode), now.Format(time.RFC3339Nano), planExecutionKey(admission.Scope, admission.Selection.ID))
	if err != nil {
		return SemanticExternalEffectOperation{}, err
	}
	if n, _ := updated.RowsAffected(); n != 1 {
		return SemanticExternalEffectOperation{}, fmt.Errorf("selection_execution_not_running")
	}
	updated, err = tx.Exec(`UPDATE semantic_host_calls SET state='completed', result=?, result_digest=?, updated_at=? WHERE call_key=? AND grant_fingerprint=? AND request_digest=? AND state='admitted'`, result, SchemaDigest([]byte(result)), now.Format(time.RFC3339Nano), hostCallKey(admission.Identity), InvocationGrantFingerprint(admission.Grant), admission.RequestDigest)
	if err != nil {
		return SemanticExternalEffectOperation{}, err
	}
	if n, _ := updated.RowsAffected(); n != 1 {
		return SemanticExternalEffectOperation{}, fmt.Errorf("host_call_not_transitionable")
	}
	plan, err := coordinatedPublishedPlan(tx, admission.Scope)
	if err != nil {
		return SemanticExternalEffectOperation{}, err
	}
	revision, continuityFencingToken, err := continuityRouteRevisionTx(tx, admission.Scope)
	if err != nil {
		return SemanticExternalEffectOperation{}, err
	}
	completedIDs, err := continuityCompletedSelectionIDsTx(tx, admission.Scope)
	if err != nil {
		return SemanticExternalEffectOperation{}, err
	}
	if err := recordContinuityProjectionTx(c, tx, continuityProjectionExecutionUpdate, admission.Scope, revision, continuityFencingToken, plan, completedIDs, now); err != nil {
		return SemanticExternalEffectOperation{}, err
	}
	if err := tx.Commit(); err != nil {
		return SemanticExternalEffectOperation{}, err
	}
	return c.ExternalEffectOperation(operationKey)
}

// SettleExternalEffectReceipt commits receipt evidence, the logical operation,
// plan execution and RouteState completion in one transaction. It is the only
// path that may report a receipt-bound dynamic selection as succeeded.
func (c *SQLiteSemanticExecutionCoordinator) SettleExternalEffectReceipt(scope InvocationScope, selectionID, selectionDigest, bindingID, operationKey string, outcome SemanticExternalEffectState, receiptDigest, reasonCode string, now time.Time) (SemanticExternalEffectOperation, error) {
	return c.settleExternalEffectReceipt(scope, selectionID, selectionDigest, bindingID, operationKey, outcome, receiptDigest, reasonCode, now, false)
}

// settleExternalEffectReceipt carries the settlement itself.
//
// overSupersededRoute admits an operation whose route revision has been
// replaced. A receipt must never take that path: settling a superseded claim
// as though its revision still held is exactly what the fencing token exists
// to stop. A person's out-of-band verdict is a different thing. The side
// effect really happened, the operation is really unknown, and refusing to
// write down what it was leaves a permanent hole in the ledger to protect a
// route that no longer exists.
//
// Admitting it is narrow rather than trusting: the scope carries the
// superseded plan id, so both the operation row and the execution row it
// touches belong to the old revision and cannot reach the live one. The one
// thing that could -- projecting a completed selection -- is skipped, which
// costs nothing, because that projection reads the published plan and a
// superseded route refuses to produce one anyway.
func (c *SQLiteSemanticExecutionCoordinator) settleExternalEffectReceipt(scope InvocationScope, selectionID, selectionDigest, bindingID, operationKey string, outcome SemanticExternalEffectState, receiptDigest, reasonCode string, now time.Time, overSupersededRoute bool) (SemanticExternalEffectOperation, error) {
	if c == nil || c.db == nil {
		return SemanticExternalEffectOperation{}, fmt.Errorf("semantic execution coordinator is unavailable")
	}
	if err := ValidateArtifactScope(scope); err != nil {
		return SemanticExternalEffectOperation{}, err
	}
	if outcome != SemanticExternalEffectSucceeded && outcome != SemanticExternalEffectFailed && outcome != SemanticExternalEffectUnknown {
		return SemanticExternalEffectOperation{}, fmt.Errorf("semantic_external_effect_settlement_outcome_invalid")
	}
	receiptDigest = strings.TrimSpace(receiptDigest)
	if outcome == SemanticExternalEffectSucceeded && receiptDigest == "" {
		return SemanticExternalEffectOperation{}, fmt.Errorf("semantic_external_effect_receipt_required")
	}
	if strings.TrimSpace(selectionID) == "" || strings.TrimSpace(selectionDigest) == "" || strings.TrimSpace(bindingID) == "" || strings.TrimSpace(operationKey) == "" {
		return SemanticExternalEffectOperation{}, fmt.Errorf("semantic_external_effect_settlement_identity_required")
	}
	now = now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := c.db.Begin()
	if err != nil {
		return SemanticExternalEffectOperation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var existing SemanticExternalEffectOperation
	var created, updated string
	err = tx.QueryRow(`SELECT operation_key, root_task_id, plan_id, session_id, turn_id, principal_id, tenant_id, user_id, selection_id, selection_digest, binding_id, request_digest, state, result_digest, receipt_digest, reason_code, fencing_token, created_at, updated_at FROM semantic_external_effect_operations WHERE operation_key=?`, operationKey).Scan(&existing.OperationKey, &existing.Scope.RootTaskID, &existing.Scope.PlanID, &existing.Scope.SessionID, &existing.Scope.TurnID, &existing.Scope.PrincipalID, &existing.TenantID, &existing.UserID, &existing.SelectionID, &existing.SelectionDigest, &existing.BindingID, &existing.RequestDigest, &existing.State, &existing.ResultDigest, &existing.ReceiptDigest, &existing.ReasonCode, &existing.FencingToken, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return SemanticExternalEffectOperation{}, fmt.Errorf("semantic_external_effect_not_found")
	}
	if err != nil {
		return SemanticExternalEffectOperation{}, err
	}
	existing.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	existing.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	if existing.Scope != scope || existing.SelectionID != selectionID || existing.SelectionDigest != selectionDigest || existing.BindingID != bindingID {
		return SemanticExternalEffectOperation{}, fmt.Errorf("semantic_external_effect_operation_conflict")
	}
	if existing.State == outcome {
		if outcome != SemanticExternalEffectSucceeded || existing.ReceiptDigest == receiptDigest {
			if err := tx.Commit(); err != nil {
				return SemanticExternalEffectOperation{}, err
			}
			return existing, nil
		}
		return SemanticExternalEffectOperation{}, fmt.Errorf("semantic_external_effect_receipt_conflict")
	}
	if existing.State != SemanticExternalEffectAwaitingReceipt && existing.State != SemanticExternalEffectUnknown {
		return SemanticExternalEffectOperation{}, fmt.Errorf("semantic_external_effect_settlement_conflict")
	}
	// The idempotent replay above already returned for terminal states, so
	// this is a real transition: a claim prepared under a superseded revision
	// is fenced off here. A stale operation can only be reconciled or
	// manually resolved, never settled as though the old revision still held.
	lineageToken, err := currentLineageFencingToken(tx, routeLineageKey(scope))
	if err != nil {
		return SemanticExternalEffectOperation{}, err
	}
	supersededRoute := outboxFencingStale(existing.FencingToken, lineageToken)
	if supersededRoute && !overSupersededRoute {
		return SemanticExternalEffectOperation{}, fmt.Errorf("semantic_external_effect_fencing_stale")
	}
	if outcome == SemanticExternalEffectSucceeded {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO semantic_external_effect_receipts(operation_key, receipt_digest, accepted_at) VALUES (?, ?, ?)`, operationKey, receiptDigest, now.Format(time.RFC3339Nano)); err != nil {
			return SemanticExternalEffectOperation{}, err
		}
		var persisted string
		if err := tx.QueryRow(`SELECT receipt_digest FROM semantic_external_effect_receipts WHERE operation_key=?`, operationKey).Scan(&persisted); err != nil || persisted != receiptDigest {
			if err != nil {
				return SemanticExternalEffectOperation{}, err
			}
			return SemanticExternalEffectOperation{}, fmt.Errorf("semantic_external_effect_receipt_conflict")
		}
	}
	resultDigest := existing.ResultDigest
	updatedResult, err := tx.Exec(`UPDATE semantic_external_effect_operations SET state=?, receipt_digest=?, reason_code=?, updated_at=? WHERE operation_key=? AND state IN ('awaiting_receipt','unknown')`, outcome, receiptDigest, strings.TrimSpace(reasonCode), now.Format(time.RFC3339Nano), operationKey)
	if err != nil {
		return SemanticExternalEffectOperation{}, err
	}
	if n, _ := updatedResult.RowsAffected(); n != 1 {
		return SemanticExternalEffectOperation{}, fmt.Errorf("semantic_external_effect_settlement_conflict")
	}
	executionState := PlanExecutionUnknown
	if outcome == SemanticExternalEffectSucceeded {
		executionState = PlanExecutionSucceeded
	} else if outcome == SemanticExternalEffectFailed {
		executionState = PlanExecutionFailed
	}
	updatedResult, err = tx.Exec(`UPDATE semantic_plan_executions SET state=?, result_digest=?, reason_code=?, updated_at=? WHERE execution_key=? AND state IN ('awaiting_receipt','unknown')`, executionState, resultDigest, strings.TrimSpace(reasonCode), now.Format(time.RFC3339Nano), planExecutionKey(scope, selectionID))
	if err != nil {
		return SemanticExternalEffectOperation{}, err
	}
	if n, _ := updatedResult.RowsAffected(); n != 1 {
		return SemanticExternalEffectOperation{}, fmt.Errorf("selection_execution_not_awaiting_receipt")
	}
	// A superseded route has no selection left to complete, and asking for its
	// published plan would only fail. The outcome recorded above is the whole
	// of what this resolution has to say.
	if executionState == PlanExecutionSucceeded && !supersededRoute {
		plan, err := coordinatedPublishedPlan(tx, scope)
		if err != nil {
			return SemanticExternalEffectOperation{}, err
		}
		var purpose string
		for _, selection := range plan.Selections {
			if selection.ID == selectionID {
				// selectionDigest is the immutable external-effect binding digest
				// (provider identity + selected contract) checked against the
				// operation record above. RouteState deliberately stores its own
				// purpose digest, which represents the plan-DAG selection purpose.
				// They are distinct commitments and must not be compared as though
				// they were serialized by the same function.
				purpose = selectionPurposeDigest(selection)
				break
			}
		}
		if purpose == "" {
			return SemanticExternalEffectOperation{}, fmt.Errorf("route_state_selection_not_found")
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO semantic_route_completed_selections(route_key, selection_id, purpose_digest, completed_at) VALUES (?, ?, ?, ?)`, routeStateKey(scope), selectionID, purpose, now.Format(time.RFC3339Nano)); err != nil {
			return SemanticExternalEffectOperation{}, err
		}
		if _, err := tx.Exec(`UPDATE semantic_route_states SET updated_at=? WHERE route_key=?`, now.Format(time.RFC3339Nano), routeStateKey(scope)); err != nil {
			return SemanticExternalEffectOperation{}, err
		}
	}
	if !supersededRoute {
		plan, err := coordinatedPublishedPlan(tx, scope)
		if err != nil {
			return SemanticExternalEffectOperation{}, err
		}
		revision, fencingToken, err := continuityRouteRevisionTx(tx, scope)
		if err != nil {
			return SemanticExternalEffectOperation{}, err
		}
		completedIDs, err := continuityCompletedSelectionIDsTx(tx, scope)
		if err != nil {
			return SemanticExternalEffectOperation{}, err
		}
		if err := recordContinuityProjectionTx(c, tx, continuityProjectionExecutionUpdate, scope, revision, fencingToken, plan, completedIDs, now); err != nil {
			return SemanticExternalEffectOperation{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return SemanticExternalEffectOperation{}, err
	}
	return c.ExternalEffectOperation(operationKey)
}

// SemanticExternalEffectResolution is a person's verdict on an operation whose
// real outcome was established outside the system. Evidence is what the
// operator checked -- a channel console entry, a support ticket, a message ID
// they read with their own eyes -- and it is recorded as a digest so the same
// finding settles the operation twice without conflict while a different one
// cannot quietly overwrite it.
type SemanticExternalEffectResolution struct {
	OperationKey string
	Outcome      SemanticExternalEffectState
	Evidence     string
	ResolvedBy   string
	ReasonCode   string
}

func (r SemanticExternalEffectResolution) validate() error {
	if strings.TrimSpace(r.OperationKey) == "" {
		return fmt.Errorf("semantic_external_effect_resolution_operation_required")
	}
	// One direction only. Reopening a settled operation, or moving one back to
	// running or awaiting_receipt, would let a keystroke undo a fact the
	// ledger earned; there is no evidence a person can hold that means "this
	// has not happened yet".
	if r.Outcome != SemanticExternalEffectSucceeded && r.Outcome != SemanticExternalEffectFailed {
		return fmt.Errorf("semantic_external_effect_resolution_outcome_invalid")
	}
	if strings.TrimSpace(r.Evidence) == "" {
		return fmt.Errorf("semantic_external_effect_resolution_evidence_required")
	}
	if strings.TrimSpace(r.ResolvedBy) == "" {
		return fmt.Errorf("semantic_external_effect_resolution_operator_required")
	}
	return nil
}

// ResolveUnknownExternalEffect is the out-of-band exit for an operation that
// ended unknown: the request was dispatched, no trusted receipt ever arrived,
// and a person has since established what actually happened.
//
// It only ever accepts an operation that is unknown right now. An
// awaiting_receipt operation still has a live expectation and belongs to the
// receipt path; taking it by hand would race the very answer it is waiting
// for. An operation that reached a terminal state has an answer already.
// Narrowing to unknown is what keeps this from being a general override.
//
// The verdict is written to its own table before the state moves, so a failure
// in between leaves a true record -- "this person asserted this, on this
// evidence" -- and the operation simply stays unknown for a retry. The reverse
// order could settle an operation with no record of who decided it.
func (c *SQLiteSemanticExecutionCoordinator) ResolveUnknownExternalEffect(scope InvocationScope, selectionID, selectionDigest, bindingID string, resolution SemanticExternalEffectResolution, now time.Time) (SemanticExternalEffectOperation, error) {
	if c == nil || c.db == nil {
		return SemanticExternalEffectOperation{}, fmt.Errorf("semantic execution coordinator is unavailable")
	}
	if err := resolution.validate(); err != nil {
		return SemanticExternalEffectOperation{}, err
	}
	now = now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	operationKey := strings.TrimSpace(resolution.OperationKey)
	evidenceDigest := SchemaDigest([]byte(resolution.Evidence))
	if err := c.recordExternalEffectResolution(operationKey, resolution, evidenceDigest, now); err != nil {
		return SemanticExternalEffectOperation{}, err
	}
	// A superseded revision is admitted here and nowhere else. The operation
	// is unknown and a person has established what happened; leaving it that
	// way because the plan behind it was replaced would keep the hole and
	// protect nothing.
	return c.settleExternalEffectReceipt(scope, selectionID, selectionDigest, bindingID, operationKey, resolution.Outcome, evidenceDigest, strings.TrimSpace(resolution.ReasonCode), now, true)
}

func (c *SQLiteSemanticExecutionCoordinator) recordExternalEffectResolution(operationKey string, resolution SemanticExternalEffectResolution, evidenceDigest string, now time.Time) error {
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var state string
	err = tx.QueryRow(`SELECT state FROM semantic_external_effect_operations WHERE operation_key=?`, operationKey).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("semantic_external_effect_not_found")
	}
	if err != nil {
		return err
	}
	if SemanticExternalEffectState(state) != SemanticExternalEffectUnknown {
		return fmt.Errorf("semantic_external_effect_resolution_not_unknown")
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO semantic_external_effect_resolutions(operation_key, outcome, evidence_digest, resolved_by, resolved_at) VALUES (?, ?, ?, ?, ?)`,
		operationKey, string(resolution.Outcome), evidenceDigest, strings.TrimSpace(resolution.ResolvedBy), now.Format(time.RFC3339Nano)); err != nil {
		return err
	}
	var storedOutcome, storedEvidence string
	if err := tx.QueryRow(`SELECT outcome, evidence_digest FROM semantic_external_effect_resolutions WHERE operation_key=?`, operationKey).Scan(&storedOutcome, &storedEvidence); err != nil {
		return err
	}
	if storedOutcome != string(resolution.Outcome) || storedEvidence != evidenceDigest {
		return fmt.Errorf("semantic_external_effect_resolution_conflict")
	}
	return tx.Commit()
}

// SemanticExternalEffectResolutionRecord is what survives a resolution. The
// operator's evidence is kept only as a digest: it is written down to bind the
// verdict, not to be read back, and the console entries and ticket numbers it
// summarizes have no business living in this ledger.
type SemanticExternalEffectResolutionRecord struct {
	OperationKey   string
	Outcome        SemanticExternalEffectState
	EvidenceDigest string
	ResolvedBy     string
	ResolvedAt     time.Time
}

// ExternalEffectResolution reports the out-of-band verdict recorded for an
// operation, so an auditor can tell a person's judgement from a channel
// receipt long after both have become the same terminal state.
func (c *SQLiteSemanticExecutionCoordinator) ExternalEffectResolution(operationKey string) (SemanticExternalEffectResolutionRecord, bool, error) {
	if c == nil || c.db == nil {
		return SemanticExternalEffectResolutionRecord{}, false, fmt.Errorf("semantic execution coordinator is unavailable")
	}
	var record SemanticExternalEffectResolutionRecord
	var outcome, resolvedAt string
	err := c.db.QueryRow(`SELECT operation_key, outcome, evidence_digest, resolved_by, resolved_at FROM semantic_external_effect_resolutions WHERE operation_key=?`, strings.TrimSpace(operationKey)).
		Scan(&record.OperationKey, &outcome, &record.EvidenceDigest, &record.ResolvedBy, &resolvedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return SemanticExternalEffectResolutionRecord{}, false, nil
	}
	if err != nil {
		return SemanticExternalEffectResolutionRecord{}, false, err
	}
	record.Outcome = SemanticExternalEffectState(outcome)
	record.ResolvedAt, _ = time.Parse(time.RFC3339Nano, resolvedAt)
	return record, true, nil
}

// ExternalEffectOperation is a trusted recovery lookup. It returns only the
// operation identity and digests, never a provider request, credential or raw
// receipt.
func (c *SQLiteSemanticExecutionCoordinator) ExternalEffectOperation(operationKey string) (SemanticExternalEffectOperation, error) {
	if c == nil || c.db == nil {
		return SemanticExternalEffectOperation{}, fmt.Errorf("semantic execution coordinator is unavailable")
	}
	var operation SemanticExternalEffectOperation
	var created, updated string
	err := c.db.QueryRow(`SELECT operation_key, root_task_id, plan_id, session_id, turn_id, principal_id, tenant_id, user_id, selection_id, selection_digest, binding_id, request_digest, state, result_digest, receipt_digest, reason_code, fencing_token, created_at, updated_at FROM semantic_external_effect_operations WHERE operation_key=?`, strings.TrimSpace(operationKey)).Scan(&operation.OperationKey, &operation.Scope.RootTaskID, &operation.Scope.PlanID, &operation.Scope.SessionID, &operation.Scope.TurnID, &operation.Scope.PrincipalID, &operation.TenantID, &operation.UserID, &operation.SelectionID, &operation.SelectionDigest, &operation.BindingID, &operation.RequestDigest, &operation.State, &operation.ResultDigest, &operation.ReceiptDigest, &operation.ReasonCode, &operation.FencingToken, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return SemanticExternalEffectOperation{}, fmt.Errorf("semantic_external_effect_not_found")
	}
	if err != nil {
		return SemanticExternalEffectOperation{}, err
	}
	operation.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	operation.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return operation, nil
}

func sameSemanticExternalEffectBinding(left, right SemanticExternalEffectOperation) bool {
	return left.Scope == right.Scope && left.TenantID == right.TenantID && left.UserID == right.UserID && left.SelectionID == right.SelectionID && left.SelectionDigest == right.SelectionDigest && left.BindingID == right.BindingID && left.RequestDigest == right.RequestDigest
}
