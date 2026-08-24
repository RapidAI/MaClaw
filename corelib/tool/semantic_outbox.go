package tool

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// This file is the shared transactional-outbox substrate for every durable
// external effect owned by SQLiteSemanticExecutionCoordinator. Channel
// delivery (design 9.4) and dynamic Skill/MCP effects (design 9.7) keep their
// own kind-specific tables because their settlement projections differ, but
// both run the same lifecycle and the same fencing machinery implemented
// here:
//
//	prepare (durable intent, fencing-stamped)
//	  -> compare-and-set dispatch claim (exactly one holder, ever)
//	  -> trusted receipt/outcome settle
//	  -> stale lease or superseded revision converges to unknown, never to
//	     an automatic redispatch
//
// A single physical table was deliberately not introduced: the delivery
// ledger predates this unification, carries a unique operation-key index that
// other recovery reads depend on, and its external behavior is frozen by
// contract tests. The unification is the shared claim/settle/fencing engine
// below, which every kind-specific method must use.

// outboxFencingCounterKey is the single monotonic sequence row for this
// database. Single-node SQLite allocation happens inside the caller's write
// transaction, so tokens are linearizable with the state transition that
// consumes them. The counter row is intentionally global (not per operation)
// so a token also orders a route publish against every outbox claim.
const outboxFencingCounterKey = "semantic-outbox-fencing"

// initOutboxFencing creates the fencing counter table. It is idempotent and
// safe to run on databases that predate fencing.
func initOutboxFencing(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS semantic_fencing_counters (
		counter_key TEXT PRIMARY KEY, next_token INTEGER NOT NULL
	)`)
	return err
}

// nextOutboxFencingToken allocates the next monotonic fencing token inside an
// existing transaction. Callers must hold a write transaction; with the
// coordinator's single SQLite connection this is the linearization point.
func nextOutboxFencingToken(tx *sql.Tx) (uint64, error) {
	if _, err := tx.Exec(`INSERT OR IGNORE INTO semantic_fencing_counters(counter_key, next_token) VALUES (?, 1)`, outboxFencingCounterKey); err != nil {
		return 0, err
	}
	var token uint64
	if err := tx.QueryRow(`SELECT next_token FROM semantic_fencing_counters WHERE counter_key=?`, outboxFencingCounterKey).Scan(&token); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`UPDATE semantic_fencing_counters SET next_token=next_token+1 WHERE counter_key=?`, outboxFencingCounterKey); err != nil {
		return 0, err
	}
	return token, nil
}

// outboxRowQuerier is satisfied by both *sql.DB and *sql.Tx so fencing reads
// can join whichever transaction the caller already owns.
type outboxRowQuerier interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}

// currentLineageFencingToken returns the fencing token of the currently
// published route revision for a root/session/principal lineage. A lineage
// that does not exist (legacy Open-only routes) reports token 0, which the
// fencing predicates below treat as "unfenced legacy" rather than an error.
func currentLineageFencingToken(q outboxRowQuerier, lineageKey string) (uint64, error) {
	var token uint64
	err := q.QueryRow(`SELECT fencing_token FROM semantic_route_lineages WHERE lineage_key=?`, lineageKey).Scan(&token)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return token, err
}

// outboxFencingStale reports whether an outbox record carrying fencingToken
// was invalidated by a newer route revision. Token 0 marks a row written
// before fencing existed; such legacy rows remain governed by the pre-existing
// current-revision checks only, so a binary upgrade never strands an in-flight
// operation that was prepared by older code.
func outboxFencingStale(recordToken, lineageToken uint64) bool {
	return recordToken != 0 && lineageToken > recordToken
}

// claimDeliveryOutbox is the unified compare-and-set claim for the delivery
// outbox kind. Exactly one caller transitions prepared -> dispatching; the
// winner receives a fresh fencing token and an optional holder identity
// (reserved for multi-replica gateway ownership, empty on single-node hosts).
// A record whose prepare-time fencing token was superseded by a newer route
// revision is converged to unknown inside the same transaction and is never
// dispatched again.
//
// claimed=false is returned for every non-dispatchable outcome (missing,
// terminal, already claimed, or fencing-converged); the caller re-reads the
// record to distinguish them, matching the pre-existing ClaimDelivery
// contract. The returned token is the freshly allocated claim fencing token
// when claimed=true.
func claimDeliveryOutbox(tx *sql.Tx, deliveryKey, lineageKey, holder string, preparedToken uint64, now string) (bool, uint64, error) {
	lineageToken, err := currentLineageFencingToken(tx, lineageKey)
	if err != nil {
		return false, 0, err
	}
	if outboxFencingStale(preparedToken, lineageToken) {
		// Converge only; a superseded outbox intent must never reach the
		// channel. Reconciliation/manual resolution owns the aftermath.
		if _, err := tx.Exec(`UPDATE semantic_delivery_preparations SET state='unknown', updated_at=? WHERE delivery_key=? AND state='prepared'`, now, deliveryKey); err != nil {
			return false, 0, err
		}
		return false, 0, nil
	}
	token, err := nextOutboxFencingToken(tx)
	if err != nil {
		return false, 0, err
	}
	updated, err := tx.Exec(`UPDATE semantic_delivery_preparations SET state='dispatching', claim_fencing_token=?, claim_holder=?, updated_at=? WHERE delivery_key=? AND state='prepared'`, token, strings.TrimSpace(holder), now, deliveryKey)
	if err != nil {
		return false, 0, err
	}
	n, err := updated.RowsAffected()
	if err != nil {
		return false, 0, err
	}
	return n == 1, token, nil
}

// settleDeliveryFencingCheck enforces that a dispatch claim is settled only
// while the route revision it was claimed under is still current. A claim
// older than the latest publish is stale: its outcome is rejected and the
// record remains dispatching until lease reconciliation converges it to
// unknown. Stale claims are never re-dispatched and never settled.
func settleDeliveryFencingCheck(tx *sql.Tx, lineageKey string, claimToken uint64) error {
	lineageToken, err := currentLineageFencingToken(tx, lineageKey)
	if err != nil {
		return err
	}
	if outboxFencingStale(claimToken, lineageToken) {
		return fmt.Errorf("delivery_fencing_stale")
	}
	return nil
}
