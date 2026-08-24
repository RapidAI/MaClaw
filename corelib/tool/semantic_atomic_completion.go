package tool

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// atomicSelectionCompleter commits a selection's terminal execution state and,
// when that state is success, the RouteState completion that it projects, in
// one transaction.
//
// Without it those are two writes. A crash or a failed second write between
// them leaves a selection durably succeeded whose dependents the recovery view
// still reports as unmet: the execution store says the work is done and the
// route store says it never completed. The two views then disagree about the
// same fact, and no later read can tell which one is stale.
//
// SemanticExecutionCoordinator already commits both together for the paths that
// go through it. This is the same guarantee for the paths that do not: legacy
// direct ExecuteTool calls with no host call ID, hosts constructed without a
// coordinator, and trusted receipt settlement.
type atomicSelectionCompleter interface {
	// CompleteSelection advances a running selection to its terminal state.
	CompleteSelection(scope InvocationScope, planID, selectionID string, state PlanExecutionState, resultDigest, reasonCode string, now time.Time) (PlanExecutionRecord, error)
	// SettleSelectionReceipt resolves an awaiting_receipt or unknown selection
	// from a trusted transport receipt.
	SettleSelectionReceipt(scope InvocationScope, planID, selectionID string, state PlanExecutionState, resultDigest, reasonCode string, now time.Time) (PlanExecutionRecord, error)
}

// newAtomicSelectionCompleter returns a completer only when both stores are
// SQLite-backed by the same database handle, which is the condition under which
// one transaction can actually span both tables. Any other pairing — memory
// stores, a test double, two separate database files — has no cross-store
// transaction to offer, so it keeps the sequential writes rather than claiming
// an atomicity it cannot provide.
func newAtomicSelectionCompleter(store PlanExecutionStore, routes RouteStateStore) atomicSelectionCompleter {
	executions, ok := store.(*SQLitePlanExecutionStore)
	if !ok || executions == nil || executions.db == nil {
		return nil
	}
	routeStore, ok := routes.(*SQLiteRouteStateStore)
	if !ok || routeStore == nil || routeStore.db == nil {
		return nil
	}
	if executions.db != routeStore.db {
		return nil
	}
	return &sqliteAtomicSelectionCompleter{db: executions.db, executions: executions, routes: routeStore}
}

type sqliteAtomicSelectionCompleter struct {
	db         *sql.DB
	executions *SQLitePlanExecutionStore
	routes     *SQLiteRouteStateStore
}

func (c *sqliteAtomicSelectionCompleter) CompleteSelection(scope InvocationScope, planID, selectionID string, state PlanExecutionState, resultDigest, reasonCode string, now time.Time) (PlanExecutionRecord, error) {
	return c.commit(scope, planID, selectionID, state, resultDigest, reasonCode, "state = 'running'", now)
}

func (c *sqliteAtomicSelectionCompleter) SettleSelectionReceipt(scope InvocationScope, planID, selectionID string, state PlanExecutionState, resultDigest, reasonCode string, now time.Time) (PlanExecutionRecord, error) {
	if !planExecutionReceiptSettlementState(state) {
		return PlanExecutionRecord{}, fmt.Errorf("plan execution receipt settlement state is invalid")
	}
	return c.commit(scope, planID, selectionID, state, resultDigest, reasonCode, "state IN ('awaiting_receipt', 'unknown')", now)
}

func (c *sqliteAtomicSelectionCompleter) commit(scope InvocationScope, planID, selectionID string, state PlanExecutionState, resultDigest, reasonCode, allowedState string, now time.Time) (PlanExecutionRecord, error) {
	if c == nil || c.db == nil {
		return PlanExecutionRecord{}, fmt.Errorf("plan execution store is unavailable")
	}
	key := planExecutionKey(scope, selectionID)
	tx, err := c.db.Begin()
	if err != nil {
		return PlanExecutionRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`UPDATE semantic_plan_executions SET state = ?, result_digest = ?, reason_code = ?, updated_at = ? WHERE execution_key = ? AND `+allowedState, state, resultDigest, strings.TrimSpace(reasonCode), planExecutionTime(now), key); err != nil {
		return PlanExecutionRecord{}, err
	}
	record, err := planExecutionRecordFrom(tx, key)
	if err != nil {
		return PlanExecutionRecord{}, err
	}
	// A no-op update means some other writer already moved this selection.
	// Reporting its actual state without projecting a completion keeps the
	// caller's own conflict handling in charge, exactly as the two-write path
	// left it.
	if record.State != state {
		return record, nil
	}
	if state == PlanExecutionSucceeded {
		if err := c.routes.recordSelectionCompletionTx(tx, scope, planID, selectionID, now); err != nil {
			return PlanExecutionRecord{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return PlanExecutionRecord{}, err
	}
	return record, nil
}
