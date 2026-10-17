package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

func (r *haSyncOpRepo) AppendLocalWithVersion(ctx context.Context, op *store.HASyncOp) (int64, error) {
	return r.appendLocalWithVersion(ctx, op, false)
}

// AppendLocalWithVersionForced always inserts a new op even when the payload is
// identical to the latest one. Used for admin HA catch-up broadcasts (e.g. skillhub
// re-emit after history prune) so lagging peers receive a new seq to pull.
func (r *haSyncOpRepo) AppendLocalWithVersionForced(ctx context.Context, op *store.HASyncOp) (int64, error) {
	return r.appendLocalWithVersion(ctx, op, true)
}

func (r *haSyncOpRepo) appendLocalWithVersion(ctx context.Context, op *store.HASyncOp, force bool) (int64, error) {
	if op == nil {
		return 0, errors.New("nil ha sync op")
	}

	// Memory pre-check: if the payload hash matches the cached value for this
	// entity, skip the SQLite transaction entirely. This eliminates the cost of
	// BEGIN IMMEDIATE + SELECT + COMMIT for repeated identical payloads (common
	// during heartbeat cycles). Force mode skips this — catch-up needs a new seq.
	cacheKey := op.EntityType + ":" + op.EntityID
	payloadHash := strings.TrimSpace(op.PayloadHash)
	if !force && payloadHash != "" {
		if cached, ok := r.lastPayloadHash.Load(cacheKey); ok && cached.(string) == payloadHash {
			return 0, nil
		}
	}

	conn, err := r.db.Conn(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return 0, err
	}
	committed := false
	defer func() {
		if !committed {
			rollbackRawTx(conn)
		}
	}()

	var latestPayloadJSON string
	var latestPayloadHash string
	var latestOpType string
	var current sql.NullInt64
	if err := conn.QueryRowContext(ctx, `
		SELECT op_type, payload_json, payload_hash
		FROM ha_sync_ops INDEXED BY idx_ha_sync_ops_entity_seq
		WHERE entity_type = ? AND entity_id = ?
		ORDER BY seq DESC
		LIMIT 1
	`, op.EntityType, op.EntityID).Scan(&latestOpType, &latestPayloadJSON, &latestPayloadHash); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	} else if !force && err == nil && latestOpType == op.OpType && haPayloadEquivalent(op.EntityType, op.OpType, op.PayloadJSON, payloadHash, latestPayloadJSON, latestPayloadHash) {
		if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
			return 0, err
		}
		committed = true
		// Update cache so next call hits the fast path.
		if payloadHash != "" {
			r.lastPayloadHash.Store(cacheKey, payloadHash)
		}
		return 0, nil
	}

	if err := conn.QueryRowContext(ctx, `
		SELECT version
		FROM ha_entity_versions
		WHERE entity_type = ? AND entity_id = ?
	`, op.EntityType, op.EntityID).Scan(&current); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	version := int64(1)
	if current.Valid {
		version = current.Int64 + 1
	}

	if _, err := conn.ExecContext(ctx, `
		INSERT INTO ha_entity_versions (entity_type, entity_id, version, updated_at, updated_by_node_id)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(entity_type, entity_id) DO UPDATE SET
			version = excluded.version,
			updated_at = excluded.updated_at,
			updated_by_node_id = excluded.updated_by_node_id
	`, op.EntityType, op.EntityID, version, op.OccurredAt.Format(time.RFC3339), op.SourceNodeID); err != nil {
		return 0, err
	}

	op.EntityVersion = version
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO ha_sync_ops (
			op_id, source_node_id, entity_type, entity_id, op_type,
			entity_version, occurred_at, payload_json, payload_hash
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		op.OpID,
		op.SourceNodeID,
		op.EntityType,
		op.EntityID,
		op.OpType,
		op.EntityVersion,
		formatHAOccuredAt(op.OccurredAt),
		op.PayloadJSON,
		op.PayloadHash,
	); err != nil {
		return 0, err
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return 0, err
	}
	committed = true
	// Update cache with new hash after successful write.
	if payloadHash != "" {
		r.lastPayloadHash.Store(cacheKey, payloadHash)
	}
	return version, nil
}

func haPayloadEquivalent(entityType, opType, currentJSON, currentHash, previousJSON, previousHash string) bool {
	if strings.TrimSpace(currentHash) != "" && currentHash == previousHash {
		return true
	}
	// For non-upsert ops (e.g. delete), hash match above is the only dedup path.
	// If hashes are both empty for a delete, fall through to JSON comparison below
	// since delete payloads are typically small/identical.
	current, ok := normalizedNoisyHAPayload(entityType, currentJSON)
	if !ok {
		return false
	}
	previous, ok := normalizedNoisyHAPayload(entityType, previousJSON)
	if !ok {
		return false
	}
	return reflect.DeepEqual(current, previous)
}

func normalizedNoisyHAPayload(entityType, payloadJSON string) (map[string]any, bool) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return nil, false
	}
	// All entity types: remove timestamp fields that change on every write
	// but don't represent meaningful state changes.
	delete(payload, "updated_at")
	delete(payload, "created_at")
	switch entityType {
	case "hub_instance":
		delete(payload, "last_seen_at")
	}
	return payload, true
}

func formatHAOccuredAt(t time.Time) string {
	// occurred_at is compared lexically against the RFC3339 UTC cutoff during
	// history pruning. Rows written from non-UTC times (entity paths passing
	// local wall-clock values) used to keep their "+08:00"-style offsets,
	// which sorted after same-instant UTC strings and let old ops escape the
	// time-based prune forever. Normalize every write to UTC.
	return t.UTC().Format(time.RFC3339)
}

func (r *haSyncOpRepo) Append(ctx context.Context, op *store.HASyncOp) error {
	return execWrite(ctx, r.batch, r.db, `
		INSERT INTO ha_sync_ops (
			op_id, source_node_id, entity_type, entity_id, op_type,
			entity_version, occurred_at, payload_json, payload_hash
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		op.OpID,
		op.SourceNodeID,
		op.EntityType,
		op.EntityID,
		op.OpType,
		op.EntityVersion,
		formatHAOccuredAt(op.OccurredAt),
		op.PayloadJSON,
		op.PayloadHash,
	)
}

func (r *haSyncOpRepo) AppendRemoteIfMissing(ctx context.Context, op *store.HASyncOp) error {
	if op == nil {
		return nil
	}
	if _, err := r.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO ha_sync_ops (
			op_id, source_node_id, entity_type, entity_id, op_type,
			entity_version, occurred_at, payload_json, payload_hash
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		op.OpID,
		op.SourceNodeID,
		op.EntityType,
		op.EntityID,
		op.OpType,
		op.EntityVersion,
		formatHAOccuredAt(op.OccurredAt),
		op.PayloadJSON,
		op.PayloadHash,
	); err != nil {
		return err
	}
	return nil
}

func (r *haSyncOpRepo) ListAfterSeq(ctx context.Context, afterSeq int64, limit int) ([]*store.HASyncOp, error) {
	query := `
		SELECT seq, op_id, source_node_id, entity_type, entity_id, op_type, entity_version, occurred_at, payload_json, payload_hash
		FROM ha_sync_ops
		WHERE seq > ?
		ORDER BY seq ASC`
	args := []any{afterSeq}
	if limit > 0 {
		query += `
		LIMIT ?`
		args = append(args, limit)
	}
	rows, err := r.readDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*store.HASyncOp
	for rows.Next() {
		var item store.HASyncOp
		var occurredAt string
		if err := rows.Scan(&item.Seq, &item.OpID, &item.SourceNodeID, &item.EntityType, &item.EntityID, &item.OpType, &item.EntityVersion, &occurredAt, &item.PayloadJSON, &item.PayloadHash); err != nil {
			return nil, err
		}
		item.OccurredAt = mustParseTime(occurredAt)
		out = append(out, &item)
	}
	return out, rows.Err()
}

func (r *haSyncOpRepo) ListLatestByEntityTypes(ctx context.Context, entityTypes []string) ([]*store.HASyncOp, error) {
	cleaned := cleanEntityTypes(entityTypes)
	if len(cleaned) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(cleaned))
	args := make([]any, 0, len(cleaned)*2)
	for i, entityType := range cleaned {
		placeholders[i] = "?"
		args = append(args, entityType)
	}
	args = append(args, args...)
	query := `
		SELECT seq, op_id, source_node_id, entity_type, entity_id, op_type, entity_version, occurred_at, payload_json, payload_hash
		FROM ha_sync_ops
		WHERE entity_type IN (` + strings.Join(placeholders, ",") + `)
		  AND seq IN (
			SELECT MAX(seq)
			FROM ha_sync_ops
			WHERE entity_type IN (` + strings.Join(placeholders, ",") + `)
			GROUP BY entity_type
		  )
		ORDER BY seq ASC`
	rows, err := r.readDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*store.HASyncOp
	for rows.Next() {
		var item store.HASyncOp
		var occurredAt string
		if err := rows.Scan(&item.Seq, &item.OpID, &item.SourceNodeID, &item.EntityType, &item.EntityID, &item.OpType, &item.EntityVersion, &occurredAt, &item.PayloadJSON, &item.PayloadHash); err != nil {
			return nil, err
		}
		item.OccurredAt = mustParseTime(occurredAt)
		out = append(out, &item)
	}
	return out, rows.Err()
}

func cleanEntityTypes(entityTypes []string) []string {
	cleaned := make([]string, 0, len(entityTypes))
	seen := make(map[string]struct{}, len(entityTypes))
	for _, entityType := range entityTypes {
		entityType = strings.TrimSpace(entityType)
		if entityType == "" {
			continue
		}
		if _, ok := seen[entityType]; ok {
			continue
		}
		seen[entityType] = struct{}{}
		cleaned = append(cleaned, entityType)
	}
	return cleaned
}

func (r *haSyncOpRepo) HasEntityTypeOps(ctx context.Context, entityTypes []string) (bool, error) {
	cleaned := cleanEntityTypes(entityTypes)
	if len(cleaned) == 0 {
		return false, nil
	}
	placeholders := make([]string, len(cleaned))
	args := make([]any, 0, len(cleaned))
	for i, entityType := range cleaned {
		placeholders[i] = "?"
		args = append(args, entityType)
	}
	var one int
	err := r.readDB.QueryRowContext(ctx, `SELECT 1 FROM ha_sync_ops WHERE entity_type IN (`+strings.Join(placeholders, ",")+`) LIMIT 1`, args...).Scan(&one)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *haSyncOpRepo) GetMaxSeq(ctx context.Context) (int64, error) {
	var seq sql.NullInt64
	if err := r.readDB.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM ha_sync_ops`).Scan(&seq); err != nil {
		return 0, err
	}
	return seq.Int64, nil
}

func (r *haSyncOpRepo) GetMinSeq(ctx context.Context) (int64, error) {
	var seq sql.NullInt64
	if err := r.readDB.QueryRowContext(ctx, `SELECT COALESCE(MIN(seq), 0) FROM ha_sync_ops`).Scan(&seq); err != nil {
		return 0, err
	}
	return seq.Int64, nil
}

func (r *haSyncOpRepo) Count(ctx context.Context) (int64, error) {
	var count int64
	err := r.readDB.QueryRowContext(ctx, `SELECT COUNT(1) FROM ha_sync_ops`).Scan(&count)
	return count, err
}

func (r *haSyncOpRepo) PruneHistory(ctx context.Context, cutoff time.Time, maxRetainedOps, batchSize int64) (*store.HAPruneResult, error) {
	return r.PruneHistoryWithAck(ctx, cutoff, maxRetainedOps, batchSize, store.HAPeerAckFloor{})
}

// PruneHistoryWithAck prunes HA history.
//
// Without an ack floor the historical rules apply: a last-write-wins op is
// removed only when a newer op for the same entity exists, and append-only
// ops (usage and ledger batches) are removed once they are older than cutoff.
// That keeps a lagging peer able to converge, but it does not bound the table
// when every row is already the newest op of its entity.
//
// With Floor.Known set, last-write-wins ops that every pinning peer has
// acknowledged are copied into ha_entity_heads and then deleted in short
// autocommit batches, including newest-per-entity rows. The snapshot rows
// stay, so a peer that restarts behind the truncated log can catch up
// without a full history replay. Append-only ops stay until the age cutoff.
// ForceWindow is the offline compact escape hatch: it checkpoints and then
// drops last-write-wins ops outside the retained seq window even when acks
// are incomplete. It never deletes append-only rows that are still inside
// the age window, and it never writes to any table other than ha_sync_ops,
// ha_applied_ops, and ha_entity_heads.
func (r *haSyncOpRepo) PruneHistoryWithAck(ctx context.Context, cutoff time.Time, maxRetainedOps, batchSize int64, floor store.HAPeerAckFloor) (*store.HAPruneResult, error) {
	return r.pruneHistory(ctx, historyPruneOptions{
		Cutoff:         cutoff,
		MaxRetainedOps: maxRetainedOps,
		BatchSize:      batchSize,
		Floor:          floor,
	})
}

type historyPruneOptions struct {
	Cutoff         time.Time
	MaxRetainedOps int64
	BatchSize      int64
	Floor          store.HAPeerAckFloor
	ForceWindow    bool
}

func (r *haSyncOpRepo) pruneHistory(ctx context.Context, opts historyPruneOptions) (*store.HAPruneResult, error) {
	batchSize := clampPruneBatch(opts.BatchSize)
	maxSeq, err := r.GetMaxSeq(ctx)
	if err != nil {
		return nil, err
	}
	seqFloor := int64(-1)
	if opts.MaxRetainedOps > 0 && maxSeq > opts.MaxRetainedOps {
		seqFloor = maxSeq - opts.MaxRetainedOps
	}
	cutoff := opts.Cutoff
	cutoffText := cutoff.UTC().Format(time.RFC3339)
	deleteByTime := !cutoff.IsZero()
	result := &store.HAPruneResult{MaxSeq: maxSeq, AckKnown: opts.Floor.Known, AckSeq: opts.Floor.PinAck}
	if deleteByTime {
		deleted, err := r.pruneSyncOpsBefore(ctx, cutoffText, batchSize)
		if err != nil {
			return nil, err
		}
		result.DeletedOps += deleted
		// One-shot entities (a new entity id per op) never receive a newer op,
		// so the newest-op protection above would pin every one of them forever
		// and the table would grow without bound. Their ops are immutable,
		// self-contained facts, so the configured time cutoff alone decides how
		// long peers get to pull them — the same intent as retention_days.
		deleted, err = r.pruneOneShotOpsBefore(ctx, cutoffText, batchSize)
		if err != nil {
			return nil, err
		}
		result.DeletedOps += deleted
	}
	if seqFloor >= 0 {
		deleted, err := r.pruneSyncOpsAtOrBeforeSeq(ctx, seqFloor, batchSize)
		if err != nil {
			return nil, err
		}
		result.DeletedOps += deleted
	}
	if deleteByTime {
		for {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			res, err := r.db.ExecContext(ctx, `
				DELETE FROM ha_applied_ops
				WHERE op_id IN (
					SELECT op_id
					FROM ha_applied_ops
					WHERE applied_at < ?
					ORDER BY applied_at ASC
					LIMIT ?
				)
			`, cutoffText, batchSize)
			if err != nil {
				return nil, err
			}
			rows, _ := res.RowsAffected()
			result.DeletedAppliedOps += rows
			if rows > 0 {
				if err := r.releaseFreedPages(ctx); err != nil {
					return nil, err
				}
			}
			if rows == 0 || rows < batchSize {
				break
			}
		}
	}
	if err := r.pruneAckedOps(ctx, result, opts, batchSize); err != nil {
		return nil, err
	}
	if opts.ForceWindow {
		if err := r.pruneForceWindow(ctx, result, opts.MaxRetainedOps, batchSize); err != nil {
			return nil, err
		}
	}
	remaining, err := r.Count(ctx)
	if err != nil {
		return nil, err
	}
	result.RemainingOps = remaining
	maxSeq, err = r.GetMaxSeq(ctx)
	if err != nil {
		return nil, err
	}
	result.MaxSeq = maxSeq
	return result, nil
}

// clampPruneBatch keeps each DELETE statement small enough that the single
// writer lock is released between batches. Callers may ask for more; the
// loop still runs until the matching rows are gone.
func clampPruneBatch(batchSize int64) int64 {
	if batchSize <= 0 {
		return 1000
	}
	if batchSize > 2000 {
		return 2000
	}
	return batchSize
}

func (r *haSyncOpRepo) pruneAckedOps(ctx context.Context, result *store.HAPruneResult, opts historyPruneOptions, batchSize int64) error {
	floor := opts.Floor
	if !floor.Known || floor.PinAck <= 0 {
		return nil
	}
	maxSeq := result.MaxSeq
	deleteThrough := floor.PinAck
	if opts.MaxRetainedOps > 0 && maxSeq > opts.MaxRetainedOps {
		windowFloor := maxSeq - opts.MaxRetainedOps
		if deleteThrough > windowFloor {
			deleteThrough = windowFloor
		}
	}
	if deleteThrough <= 0 {
		return nil
	}
	// Copy the latest last-write-wins payload for every entity whose op is
	// about to leave the log. The copy stays in ha_entity_heads after the
	// delete, so a peer that restarts behind the truncated log can catch up
	// from the snapshot instead of a full history replay. Append-only ops
	// stay in the log until the age cutoff: they are facts, not heads.
	copied, err := r.checkpointHeads(ctx, 0, deleteThrough, batchSize)
	if err != nil {
		return err
	}
	result.CheckpointedHeads += copied
	deleted, err := r.deleteSyncOpsSeqRange(ctx, 0, deleteThrough, batchSize, true)
	if err != nil {
		return err
	}
	result.DeletedOps += deleted
	return nil
}

// pruneForceWindow drops last-write-wins ops outside the retained seq window
// after copying their latest payload into ha_entity_heads. Append-only ops
// are not touched here; age-based pruning is the only way those leave.
func (r *haSyncOpRepo) pruneForceWindow(ctx context.Context, result *store.HAPruneResult, maxRetainedOps, batchSize int64) error {
	if maxRetainedOps <= 0 {
		return nil
	}
	count, err := r.Count(ctx)
	if err != nil {
		return err
	}
	maxSeq, err := r.GetMaxSeq(ctx)
	if err != nil {
		return err
	}
	if count <= maxRetainedOps || maxSeq <= maxRetainedOps {
		return nil
	}
	floor := maxSeq - maxRetainedOps
	copied, err := r.checkpointHeads(ctx, 0, floor, batchSize)
	if err != nil {
		return err
	}
	result.CheckpointedHeads += copied
	deleted, err := r.deleteSyncOpsSeqRange(ctx, 0, floor, batchSize, true)
	if err != nil {
		return err
	}
	result.DeletedOps += deleted
	return nil
}

func (r *haSyncOpRepo) pruneSyncOpsBefore(ctx context.Context, cutoffText string, batchSize int64) (int64, error) {
	return r.pruneSyncOps(ctx, batchSize, `
		SELECT candidate.seq
		FROM ha_sync_ops candidate INDEXED BY idx_ha_sync_ops_occurred_at
		WHERE candidate.occurred_at < ?
		  AND EXISTS (
			SELECT 1
			FROM ha_sync_ops newer INDEXED BY idx_ha_sync_ops_entity_seq
			WHERE newer.entity_type = candidate.entity_type
			  AND newer.entity_id = candidate.entity_id
			  AND newer.seq > candidate.seq
			LIMIT 1
		  )
		ORDER BY candidate.occurred_at ASC, candidate.seq ASC
		LIMIT ?
	`, cutoffText, batchSize)
}

func (r *haSyncOpRepo) pruneSyncOpsAtOrBeforeSeq(ctx context.Context, seqFloor int64, batchSize int64) (int64, error) {
	return r.pruneSyncOps(ctx, batchSize, `
		SELECT candidate.seq
		FROM ha_sync_ops candidate
		WHERE candidate.seq <= ?
		  AND EXISTS (
			SELECT 1
			FROM ha_sync_ops newer INDEXED BY idx_ha_sync_ops_entity_seq
			WHERE newer.entity_type = candidate.entity_type
			  AND newer.entity_id = candidate.entity_id
			  AND newer.seq > candidate.seq
			LIMIT 1
		  )
		ORDER BY candidate.seq ASC
		LIMIT ?
	`, seqFloor, batchSize)
}

// haAppendOnlyEntityTypes are facts that must not be collapsed into a single
// head. Kept as literals to avoid an import cycle with the ha package.
var haAppendOnlyEntityTypes = []string{"llm_usage_batch", "token_bank_ledger_batch"}

// pruneOneShotOpsBefore deletes ops whose entity id is unique per op (so the
// newest-op protection can never apply) once they are older than the time
// cutoff.
func (r *haSyncOpRepo) pruneOneShotOpsBefore(ctx context.Context, cutoffText string, batchSize int64) (int64, error) {
	var deleted int64
	for _, entityType := range haAppendOnlyEntityTypes {
		n, err := r.pruneOneShotEntityBefore(ctx, entityType, cutoffText, batchSize)
		deleted += n
		if err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}

func (r *haSyncOpRepo) pruneOneShotEntityBefore(ctx context.Context, entityType, cutoffText string, batchSize int64) (int64, error) {
	var deleted int64
	for {
		if err := ctx.Err(); err != nil {
			return deleted, err
		}
		res, err := r.db.ExecContext(ctx, `
			DELETE FROM ha_sync_ops WHERE seq IN (
				SELECT seq
				FROM ha_sync_ops INDEXED BY idx_ha_sync_ops_occurred_at
				WHERE entity_type = ?
				  AND occurred_at < ?
				ORDER BY occurred_at ASC, seq ASC
				LIMIT ?
			)
		`, entityType, cutoffText, batchSize)
		if err != nil {
			return deleted, err
		}
		rows, _ := res.RowsAffected()
		deleted += rows
		if rows > 0 {
			if err := r.releaseFreedPages(ctx); err != nil {
				return deleted, err
			}
		}
		if rows == 0 || rows < batchSize {
			break
		}
	}
	return deleted, nil
}

// NormalizeHAOccuredAtUTC rewrites legacy occurred_at values that carry a
// numeric timezone offset (e.g. "+08:00") into canonical UTC strings. Before
// formatHAOccuredAt existed, entity paths passing local wall-clock times wrote
// offset-formatted values, which sort after same-instant UTC strings and
// escape the lexical time cutoff used by history pruning. Runs batched;
// returns the number of rows rewritten.
func NormalizeHAOccuredAtUTC(ctx context.Context, db *sql.DB, batchSize int64) (int64, error) {
	if db == nil {
		return 0, errors.New("nil sqlite database")
	}
	if batchSize <= 0 {
		batchSize = 500
	}
	var total int64
	for {
		rows, err := db.QueryContext(ctx, `
			SELECT seq, occurred_at
			FROM ha_sync_ops
			WHERE occurred_at LIKE '%+%'
			ORDER BY seq ASC
			LIMIT ?
		`, batchSize)
		if err != nil {
			return total, err
		}
		type fix struct {
			seq int64
			at  string
		}
		var batch []fix
		for rows.Next() {
			var f fix
			if err := rows.Scan(&f.seq, &f.at); err != nil {
				rows.Close()
				return total, err
			}
			batch = append(batch, f)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return total, err
		}
		if len(batch) == 0 {
			break
		}
		for _, f := range batch {
			t, err := time.Parse(time.RFC3339, f.at)
			if err != nil {
				continue
			}
			if _, err := db.ExecContext(ctx, `UPDATE ha_sync_ops SET occurred_at = ? WHERE seq = ?`, formatHAOccuredAt(t), f.seq); err != nil {
				return total, err
			}
			total++
		}
		if int64(len(batch)) < batchSize {
			break
		}
	}
	return total, nil
}

func (r *haSyncOpRepo) pruneSyncOps(ctx context.Context, batchSize int64, selectSQL string, args ...any) (int64, error) {
	var deleted int64
	for {
		if err := ctx.Err(); err != nil {
			return deleted, err
		}
		queryArgs := append([]any(nil), args...)
		res, err := r.db.ExecContext(ctx, `DELETE FROM ha_sync_ops WHERE seq IN (`+selectSQL+`)`, queryArgs...)
		if err != nil {
			return deleted, err
		}
		rows, _ := res.RowsAffected()
		deleted += rows
		if rows > 0 {
			if err := r.releaseFreedPages(ctx); err != nil {
				return deleted, err
			}
		}
		if rows == 0 || rows < batchSize {
			break
		}
	}
	return deleted, nil
}

// HistoryPruneOptions configures an offline or in-process history prune.
// PeerIDs, when set and Floor.Known is false, loads the ack floor from
// ha_peer_acks. ForceWindow checkpoints and drops last-write-wins ops outside
// the retained window; leave it false on a live server.
type HistoryPruneOptions struct {
	Cutoff         time.Time
	MaxRetainedOps int64
	BatchSize      int64
	Floor          store.HAPeerAckFloor
	ForceWindow    bool
	PeerIDs        []string
	PeerLagGrace   time.Duration
}

func PruneHAHistory(ctx context.Context, db *sql.DB, cutoff time.Time, maxRetainedOps, batchSize int64) (*store.HAPruneResult, error) {
	return PruneHAHistoryOptions(ctx, db, HistoryPruneOptions{
		Cutoff:         cutoff,
		MaxRetainedOps: maxRetainedOps,
		BatchSize:      batchSize,
	})
}

func PruneHAHistoryOptions(ctx context.Context, db *sql.DB, opts HistoryPruneOptions) (*store.HAPruneResult, error) {
	if db == nil {
		return nil, errors.New("nil sqlite database")
	}
	repo := &haSyncOpRepo{db: db, readDB: db}
	if len(opts.PeerIDs) > 0 && !opts.Floor.Known {
		floor, err := repo.MinPeerAck(ctx, opts.PeerIDs, opts.PeerLagGrace)
		if err != nil {
			return nil, err
		}
		opts.Floor = floor
	}
	return repo.pruneHistory(ctx, historyPruneOptions{
		Cutoff:         opts.Cutoff,
		MaxRetainedOps: opts.MaxRetainedOps,
		BatchSize:      opts.BatchSize,
		Floor:          opts.Floor,
		ForceWindow:    opts.ForceWindow,
	})
}

// RecordPeerAck stores how far peerNodeID has applied this node's op log.
// The ack only moves forward. A rebuilt peer that reports a lower seq does
// not reopen a hole in a log that may already have been truncated; it catches
// up from snapshot heads or an operator restore.
func (r *haSyncOpRepo) RecordPeerAck(ctx context.Context, peerNodeID string, ackSeq int64, protocol int) error {
	peerNodeID = strings.TrimSpace(peerNodeID)
	if peerNodeID == "" || ackSeq < 0 {
		return nil
	}
	if protocol <= 0 {
		protocol = 1
	}
	return RetryBusy(ctx, func() error {
		_, err := r.db.ExecContext(ctx, `
			INSERT INTO ha_peer_acks (peer_node_id, acked_seq, protocol_version, acked_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(peer_node_id) DO UPDATE SET
				acked_seq = CASE
					WHEN excluded.acked_seq > ha_peer_acks.acked_seq THEN excluded.acked_seq
					ELSE ha_peer_acks.acked_seq
				END,
				protocol_version = excluded.protocol_version,
				acked_at = excluded.acked_at
		`, peerNodeID, ackSeq, protocol, time.Now().UTC().Format(time.RFC3339))
		return err
	})
}

// MinPeerAck computes the truncation floor for the configured peers.
// A peer with no ack row makes the floor unknown so newest ops stay put
// (rolling upgrade: old binaries do not report acks). A protocol-2 peer
// whose ack is older than lagGrace does not pin the log; Checkpoint asks
// the pruner to copy last-write-wins heads before deleting past that peer.
func (r *haSyncOpRepo) MinPeerAck(ctx context.Context, peerIDs []string, lagGrace time.Duration) (store.HAPeerAckFloor, error) {
	if lagGrace <= 0 {
		lagGrace = 6 * time.Hour
	}
	var floor store.HAPeerAckFloor
	seen := 0
	var pin *int64
	var strict *int64
	now := time.Now().UTC()
	for _, id := range peerIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		seen++
		var seq int64
		var proto int
		var at sql.NullString
		err := r.readDB.QueryRowContext(ctx, `
			SELECT acked_seq, protocol_version, acked_at
			FROM ha_peer_acks
			WHERE peer_node_id = ?
		`, id).Scan(&seq, &proto, &at)
		if errors.Is(err, sql.ErrNoRows) {
			return store.HAPeerAckFloor{}, nil
		}
		if err != nil {
			return store.HAPeerAckFloor{}, err
		}
		if strict == nil || seq < *strict {
			v := seq
			strict = &v
		}
		fresh := true
		if proto >= 2 && at.Valid {
			if t, parseErr := time.Parse(time.RFC3339, at.String); parseErr == nil && now.Sub(t) > lagGrace {
				fresh = false
				floor.Checkpoint = true
			}
		}
		if fresh {
			if pin == nil || seq < *pin {
				v := seq
				pin = &v
			}
		}
	}
	if seen == 0 || pin == nil || strict == nil {
		return store.HAPeerAckFloor{}, nil
	}
	floor.Known = true
	floor.PinAck = *pin
	floor.StrictAck = *strict
	return floor, nil
}

func (r *haSyncOpRepo) deleteSyncOpsSeqRange(ctx context.Context, afterSeq, throughSeq, batchSize int64, lwwOnly bool) (int64, error) {
	if throughSeq <= afterSeq {
		return 0, nil
	}
	extra := ""
	if lwwOnly {
		extra = " AND entity_type NOT IN ('llm_usage_batch', 'token_bank_ledger_batch')"
	}
	var deleted int64
	for {
		if err := ctx.Err(); err != nil {
			return deleted, err
		}
		res, err := r.db.ExecContext(ctx, `
			DELETE FROM ha_sync_ops WHERE seq IN (
				SELECT seq FROM ha_sync_ops
				WHERE seq > ? AND seq <= ?`+extra+`
				ORDER BY seq ASC
				LIMIT ?
			)
		`, afterSeq, throughSeq, batchSize)
		if err != nil {
			return deleted, err
		}
		rows, _ := res.RowsAffected()
		deleted += rows
		if rows > 0 {
			if err := r.releaseFreedPages(ctx); err != nil {
				return deleted, err
			}
		}
		if rows == 0 || rows < batchSize {
			break
		}
	}
	return deleted, nil
}

// checkpointHeads copies the latest last-write-wins op at or below upTo into
// ha_entity_heads. Each row is its own autocommit so a multi-GB payload is
// not wrapped in one transaction. Append-only entities are skipped.
func (r *haSyncOpRepo) checkpointHeads(ctx context.Context, afterSeq, upTo, batchSize int64) (int64, error) {
	if upTo <= afterSeq {
		return 0, nil
	}
	readDB := r.readDB
	if readDB == nil {
		readDB = r.db
	}
	after := afterSeq
	var copied int64
	for {
		if err := ctx.Err(); err != nil {
			return copied, err
		}
		rows, err := readDB.QueryContext(ctx, `
			SELECT o.seq
			FROM ha_sync_ops o
			WHERE o.seq > ? AND o.seq <= ?
			  AND o.entity_type NOT IN ('llm_usage_batch', 'token_bank_ledger_batch')
			  AND NOT EXISTS (
				SELECT 1 FROM ha_sync_ops newer
				WHERE newer.entity_type = o.entity_type
				  AND newer.entity_id = o.entity_id
				  AND newer.seq > o.seq
			  )
			ORDER BY o.seq ASC
			LIMIT ?
		`, after, upTo, batchSize)
		if err != nil {
			return copied, err
		}
		var seqs []int64
		for rows.Next() {
			var seq int64
			if err := rows.Scan(&seq); err != nil {
				rows.Close()
				return copied, err
			}
			seqs = append(seqs, seq)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return copied, err
		}
		rows.Close()
		if len(seqs) == 0 {
			return copied, nil
		}
		for _, seq := range seqs {
			if seq > after {
				after = seq
			}
			if err := ctx.Err(); err != nil {
				return copied, err
			}
			if _, err := r.db.ExecContext(ctx, `
				INSERT INTO ha_entity_heads (
					entity_type, entity_id, seq, op_id, source_node_id, op_type,
					entity_version, occurred_at, payload_json, payload_hash
				)
				SELECT entity_type, entity_id, seq, op_id, source_node_id, op_type,
					entity_version, occurred_at, payload_json, payload_hash
				FROM ha_sync_ops WHERE seq = ?
				ON CONFLICT(entity_type, entity_id) DO UPDATE SET
					seq = excluded.seq,
					op_id = excluded.op_id,
					source_node_id = excluded.source_node_id,
					op_type = excluded.op_type,
					entity_version = excluded.entity_version,
					occurred_at = excluded.occurred_at,
					payload_json = excluded.payload_json,
					payload_hash = excluded.payload_hash
				WHERE excluded.seq >= ha_entity_heads.seq
			`, seq); err != nil {
				return copied, err
			}
			copied++
		}
		if int64(len(seqs)) < batchSize {
			return copied, nil
		}
	}
}

func (r *haSyncOpRepo) ListSnapshotHeads(ctx context.Context, afterSeq int64, limit int) ([]*store.HASyncOp, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 2000 {
		limit = 2000
	}
	readDB := r.readDB
	if readDB == nil {
		readDB = r.db
	}
	rows, err := readDB.QueryContext(ctx, `
		SELECT seq, op_id, source_node_id, entity_type, entity_id, op_type, entity_version, occurred_at, payload_json, payload_hash
		FROM ha_entity_heads
		WHERE seq > ?
		ORDER BY seq ASC
		LIMIT ?
	`, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*store.HASyncOp
	for rows.Next() {
		var item store.HASyncOp
		var occurredAt string
		if err := rows.Scan(&item.Seq, &item.OpID, &item.SourceNodeID, &item.EntityType, &item.EntityID, &item.OpType, &item.EntityVersion, &occurredAt, &item.PayloadJSON, &item.PayloadHash); err != nil {
			return nil, err
		}
		item.OccurredAt = mustParseTime(occurredAt)
		out = append(out, &item)
	}
	return out, rows.Err()
}

// Vacuum rebuilds the SQLite file to reclaim the free pages that repeated
// delete/insert churn leaves behind (ha_sync_ops pruning can bloat the main
// database file to several times its live data). It also persists
// auto_vacuum=INCREMENTAL in the rebuilt database header, so afterwards the
// runtime checkpointer can release newly freed pages with
// PRAGMA incremental_vacuum instead of the file re-bloating between manual
// VACUUMs. VACUUM needs an exclusive lock briefly — run it only while
// Hub Center is stopped (offline `maintenance vacuum` / ha-prune --vacuum),
// with free disk space for the rebuilt copy.
func Vacuum(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("nil sqlite database")
	}
	// Merge and truncate the WAL first so the rebuild starts from a canonical
	// main-database image and no stale WAL lingers beside it.
	if _, err := db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return err
	}
	// Switch the mode before the rebuild: per SQLite semantics, a VACUUM that
	// follows this pragma persists auto_vacuum=INCREMENTAL in the new file
	// header (setting it on a non-empty database is otherwise a no-op).
	if _, err := db.ExecContext(ctx, `PRAGMA auto_vacuum = INCREMENTAL`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `VACUUM`); err != nil {
		return err
	}
	// In WAL mode VACUUM writes the rebuilt pages into a fresh WAL; truncate
	// it so the disk space is actually handed back to the OS.
	_, err := db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}

func (r *haSyncOpRepo) HasApplied(ctx context.Context, opID string) (bool, error) {
	var count int
	if err := r.readDB.QueryRowContext(ctx, `SELECT COUNT(1) FROM ha_applied_ops WHERE op_id = ?`, opID).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *haSyncOpRepo) MarkApplied(ctx context.Context, item *store.HAAppliedOp) error {
	return execWrite(ctx, r.batch, r.db, `
		INSERT INTO ha_applied_ops (op_id, source_node_id, entity_type, entity_id, applied_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(op_id) DO NOTHING
	`, item.OpID, item.SourceNodeID, item.EntityType, item.EntityID, item.AppliedAt.Format(time.RFC3339))
}

func (r *haPeerCursorRepo) Get(ctx context.Context, peerNodeID string) (*store.HAPeerCursor, error) {
	row := r.readDB.QueryRowContext(ctx, `
		SELECT peer_node_id, last_pulled_seq, last_pulled_at, last_success_at, last_error
		FROM ha_peer_cursors
		WHERE peer_node_id = ?
	`, peerNodeID)
	var item store.HAPeerCursor
	var lastPulledAt sql.NullString
	var lastSuccessAt sql.NullString
	if err := row.Scan(&item.PeerNodeID, &item.LastPulledSeq, &lastPulledAt, &lastSuccessAt, &item.LastError); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if lastPulledAt.Valid {
		t := mustParseTime(lastPulledAt.String)
		item.LastPulledAt = &t
	}
	if lastSuccessAt.Valid {
		t := mustParseTime(lastSuccessAt.String)
		item.LastSuccessAt = &t
	}
	return &item, nil
}

func (r *haPeerCursorRepo) Upsert(ctx context.Context, item *store.HAPeerCursor) error {
	return execWrite(ctx, r.batch, r.db, `
		INSERT INTO ha_peer_cursors (peer_node_id, last_pulled_seq, last_pulled_at, last_success_at, last_error)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(peer_node_id) DO UPDATE SET
			last_pulled_seq = excluded.last_pulled_seq,
			last_pulled_at = excluded.last_pulled_at,
			last_success_at = excluded.last_success_at,
			last_error = excluded.last_error
	`, item.PeerNodeID, item.LastPulledSeq, timePtrString(item.LastPulledAt), timePtrString(item.LastSuccessAt), item.LastError)
}

func (r *haEntityVersionRepo) Get(ctx context.Context, entityType, entityID string) (*store.HAEntityVersion, error) {
	row := r.readDB.QueryRowContext(ctx, `
		SELECT entity_type, entity_id, version, updated_at, updated_by_node_id
		FROM ha_entity_versions
		WHERE entity_type = ? AND entity_id = ?
	`, entityType, entityID)
	var item store.HAEntityVersion
	var updatedAt string
	if err := row.Scan(&item.EntityType, &item.EntityID, &item.Version, &updatedAt, &item.UpdatedByNodeID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	item.UpdatedAt = mustParseTime(updatedAt)
	return &item, nil
}

func (r *haEntityVersionRepo) Upsert(ctx context.Context, item *store.HAEntityVersion) error {
	return execWrite(ctx, r.batch, r.db, `
		INSERT INTO ha_entity_versions (entity_type, entity_id, version, updated_at, updated_by_node_id)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(entity_type, entity_id) DO UPDATE SET
			version = excluded.version,
			updated_at = excluded.updated_at,
			updated_by_node_id = excluded.updated_by_node_id
	`, item.EntityType, item.EntityID, item.Version, item.UpdatedAt.Format(time.RFC3339), item.UpdatedByNodeID)
}

func (r *haHeartbeatSyncStateRepo) Get(ctx context.Context, hubID string) (*store.HAHeartbeatSyncState, error) {
	row := r.readDB.QueryRowContext(ctx, `
		SELECT hub_id, last_synced_seen_at
		FROM ha_heartbeat_sync_state
		WHERE hub_id = ?
	`, hubID)
	var item store.HAHeartbeatSyncState
	var lastSynced sql.NullString
	if err := row.Scan(&item.HubID, &lastSynced); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if lastSynced.Valid {
		t := mustParseTime(lastSynced.String)
		item.LastSyncedSeenAt = &t
	}
	return &item, nil
}

func (r *haHeartbeatSyncStateRepo) Upsert(ctx context.Context, item *store.HAHeartbeatSyncState) error {
	return execWrite(ctx, r.batch, r.db, `
		INSERT INTO ha_heartbeat_sync_state (hub_id, last_synced_seen_at)
		VALUES (?, ?)
		ON CONFLICT(hub_id) DO UPDATE SET
			last_synced_seen_at = excluded.last_synced_seen_at
	`, item.HubID, timePtrString(item.LastSyncedSeenAt))
}
