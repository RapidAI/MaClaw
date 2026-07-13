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
			_, _ = conn.ExecContext(ctx, "ROLLBACK")
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
		op.OccurredAt.Format(time.RFC3339),
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
		op.OccurredAt.Format(time.RFC3339),
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
		op.OccurredAt.Format(time.RFC3339),
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

func (r *haSyncOpRepo) Count(ctx context.Context) (int64, error) {
	var count int64
	err := r.readDB.QueryRowContext(ctx, `SELECT COUNT(1) FROM ha_sync_ops`).Scan(&count)
	return count, err
}

func (r *haSyncOpRepo) PruneHistory(ctx context.Context, cutoff time.Time, maxRetainedOps, batchSize int64) (*store.HAPruneResult, error) {
	if batchSize <= 0 {
		batchSize = 50000
	}
	if batchSize > 200000 {
		batchSize = 200000
	}
	maxSeq, err := r.GetMaxSeq(ctx)
	if err != nil {
		return nil, err
	}
	seqFloor := int64(-1)
	if maxRetainedOps > 0 && maxSeq > maxRetainedOps {
		seqFloor = maxSeq - maxRetainedOps
	}
	cutoffText := cutoff.UTC().Format(time.RFC3339)
	deleteByTime := !cutoff.IsZero()
	result := &store.HAPruneResult{MaxSeq: maxSeq}
	if deleteByTime {
		deleted, err := r.pruneSyncOpsBefore(ctx, cutoffText, batchSize)
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
			if rows == 0 || rows < batchSize {
				break
			}
		}
	}
	remaining, err := r.Count(ctx)
	if err != nil {
		return nil, err
	}
	result.RemainingOps = remaining
	return result, nil
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

func (r *haSyncOpRepo) pruneSyncOps(ctx context.Context, batchSize int64, selectSQL string, args ...any) (int64, error) {
	var deleted int64
	for {
		queryArgs := append([]any(nil), args...)
		res, err := r.db.ExecContext(ctx, `DELETE FROM ha_sync_ops WHERE seq IN (`+selectSQL+`)`, queryArgs...)
		if err != nil {
			return deleted, err
		}
		rows, _ := res.RowsAffected()
		deleted += rows
		if rows == 0 || rows < batchSize {
			break
		}
	}
	return deleted, nil
}

func PruneHAHistory(ctx context.Context, db *sql.DB, cutoff time.Time, maxRetainedOps, batchSize int64) (*store.HAPruneResult, error) {
	if db == nil {
		return nil, errors.New("nil sqlite database")
	}
	repo := &haSyncOpRepo{db: db, readDB: db}
	return repo.PruneHistory(ctx, cutoff, maxRetainedOps, batchSize)
}

func Vacuum(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("nil sqlite database")
	}
	if _, err := db.ExecContext(ctx, `VACUUM`); err != nil {
		return err
	}
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
