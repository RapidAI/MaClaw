package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

func (r *haSyncOpRepo) AppendLocalWithVersion(ctx context.Context, op *store.HASyncOp) (int64, error) {
	if op == nil {
		return 0, errors.New("nil ha sync op")
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

	var current sql.NullInt64
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
	return version, nil
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

func (r *haSyncOpRepo) GetMaxSeq(ctx context.Context) (int64, error) {
	var seq sql.NullInt64
	if err := r.readDB.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM ha_sync_ops`).Scan(&seq); err != nil {
		return 0, err
	}
	return seq.Int64, nil
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
