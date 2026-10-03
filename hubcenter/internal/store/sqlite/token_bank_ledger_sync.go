package sqlite

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// TokenBankLedgerBatch is one replicated group of ledger movements.
//
// The ledger is append-only and every row carries a deterministic primary key,
// so replication is a plain INSERT OR IGNORE and redelivery is harmless. That
// is the whole point of the E1 decision: a balance stored as a column would have
// to be replicated as a value, and two nodes writing concurrently would each
// publish "the" balance — last writer wins, and one node's credits vanish.
//
// Batching matters for the same reason it matters for usage records: settlement
// runs once per proxied request, and one op per request would swamp the HA op
// log. A batch is the unit of redelivery, so it also gives the sync loop a
// single item to requeue when the op log write fails.
type TokenBankLedgerBatch struct {
	BatchID      string                 `json:"batch_id"`
	OriginNodeID string                 `json:"origin_node_id"`
	Entries      []TokenBankLedgerEntry `json:"entries"`
	CreatedAt    time.Time              `json:"created_at"`
}

// InsertLedgerBatch applies a replicated batch. It reports applied=false when
// the batch was already seen, so the caller can count real work rather than
// redelivery.
//
// Note what this deliberately does NOT do: it does not recompute the cached
// account balances by summing the ledger. The cache is bumped only for rows this
// node actually inserted, which is correct — a row that arrives from a peer was
// already added to that peer's cache, and rebuilding from the ledger here would
// race with this node's own concurrent settlements. If a cache does drift,
// RebuildAccount is the repair tool, not the steady state.
func (r *TokenBankRepo) InsertLedgerBatch(ctx context.Context, batchID, sourceNodeID string, entries []TokenBankLedgerEntry) (applied bool, err error) {
	batchID = strings.TrimSpace(batchID)
	if batchID == "" || len(entries) == 0 {
		return false, nil
	}
	// Rows applied with an empty origin would look locally-originated to this
	// node's own sync seed and be republished back to the cluster.
	sourceNodeID = strings.TrimSpace(sourceNodeID)
	if sourceNodeID == "" {
		return false, fmt.Errorf("token bank ledger batch %s has no origin node id", batchID)
	}

	tx, err := r.write.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin token bank ledger batch apply: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	res, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO token_bank_ledger_batches (batch_id, source_node_id, record_count, applied_at)
		 VALUES (?, ?, ?, ?)`,
		batchID, sourceNodeID, len(entries), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return false, fmt.Errorf("record token bank ledger batch: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_ = tx.Rollback()
		return false, nil
	}

	now := time.Now().UTC()
	for _, entry := range entries {
		entry.UserID = strings.TrimSpace(entry.UserID)
		entry.ID = strings.TrimSpace(entry.ID)
		if entry.UserID == "" || entry.ID == "" {
			return false, fmt.Errorf("replicated token bank entry requires user id and id")
		}
		if !isTokenBankBucket(entry.Bucket) {
			return false, fmt.Errorf("replicated token bank entry %q has unknown bucket %q", entry.ID, entry.Bucket)
		}
		createdAt := entry.CreatedAt
		if createdAt.IsZero() {
			createdAt = now
		}
		// INSERT OR IGNORE on the primary key is the idempotency boundary. A
		// row republished under a different batch (a seed pass after a node
		// restart, or a peer that never received the first batch) is skipped
		// here rather than credited twice.
		rowRes, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO token_bank_ledger (id, user_id, bucket, amount_micro, biz_key, ref_type, ref_id, note, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			entry.ID, entry.UserID, entry.Bucket, entry.AmountMicro,
			strings.TrimSpace(entry.BizKey), strings.TrimSpace(entry.RefType),
			strings.TrimSpace(entry.RefID), strings.TrimSpace(entry.Note),
			createdAt.UTC().Format(time.RFC3339))
		if err != nil {
			return false, fmt.Errorf("insert replicated token bank ledger: %w", err)
		}
		inserted, err := rowRes.RowsAffected()
		if err != nil {
			return false, err
		}
		if inserted == 0 {
			// This node already had the row. Its cache was bumped when the row
			// first landed, so bumping again would double-count.
			continue
		}
		if err := bumpTokenBankAccount(ctx, tx, entry.UserID, entry.Bucket, entry.AmountMicro, createdAt); err != nil {
			return false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return false, fmt.Errorf("commit token bank ledger batch apply: %w", err)
	}
	return true, nil
}

// ListLedgerAfter returns ledger rows created after the (created_at, id)
// cursor, oldest first, capped at limit. The HA seed path uses it to catch a
// peer up without shipping the entire history in one op.
//
// The cursor must include the id, not just the timestamp: created_at is stored
// at RFC3339 second precision, and a busy second can hold more rows than one
// page. A timestamp-only cursor would skip every remaining row of that second
// as soon as a page boundary fell inside it — a silent, permanent gap in the
// peer's ledger. Pagination restarts with afterID="" and after=zero time.
func (r *TokenBankRepo) ListLedgerAfter(ctx context.Context, after time.Time, afterID string, limit int) ([]TokenBankLedgerEntry, error) {
	if limit <= 0 {
		limit = 1000
	}
	afterKey := after.UTC().Format(time.RFC3339)
	rows, err := r.read.QueryContext(ctx,
		`SELECT id, user_id, bucket, amount_micro, biz_key, ref_type, ref_id, note, created_at
		   FROM token_bank_ledger
		  WHERE created_at > ? OR (created_at = ? AND id > ?)
		  ORDER BY created_at ASC, id ASC
		  LIMIT ?`,
		afterKey, afterKey, strings.TrimSpace(afterID), limit)
	if err != nil {
		return nil, fmt.Errorf("list token bank ledger after: %w", err)
	}
	defer rows.Close()
	out := []TokenBankLedgerEntry{}
	for rows.Next() {
		var entry TokenBankLedgerEntry
		var createdAt string
		if err := rows.Scan(&entry.ID, &entry.UserID, &entry.Bucket, &entry.AmountMicro,
			&entry.BizKey, &entry.RefType, &entry.RefID, &entry.Note, &createdAt); err != nil {
			return nil, err
		}
		entry.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		out = append(out, entry)
	}
	return out, rows.Err()
}

// CountLedgerBatches reports how many replicated batches this node has applied.
// It exists so a health check can tell "the ledger is quiet" from "replication
// is broken", which look identical from the balance alone.
func (r *TokenBankRepo) CountLedgerBatches(ctx context.Context) (int, error) {
	var n int
	if err := r.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM token_bank_ledger_batches`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// TokenBankLedgerBatchID builds the deterministic batch id for a set of entries.
// It is a pure function of the origin node and the row ids, so a seed pass that
// is re-derived after a restart produces the same batch id and the receiver's
// batch-level dedup catches it even before the row-level dedup does.
func TokenBankLedgerBatchID(originNodeID string, entries []TokenBankLedgerEntry) string {
	var sb strings.Builder
	sb.WriteString(strings.TrimSpace(originNodeID))
	for _, e := range entries {
		sb.WriteByte('|')
		sb.WriteString(strings.TrimSpace(e.ID))
	}
	return "tbkledger_" + hashTokenBankKey(sb.String())
}
