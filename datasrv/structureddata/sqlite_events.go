package structureddata

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

func (s *SQLiteStore) GetDataEventByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey string) (*DataEventLog, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx, `SELECT id, tenant_id, source, event_type, operation, business_action_id, dataset_id, record_id, idempotency_key, result_status, created_by, applied_at FROM data_events WHERE tenant_id = ? AND idempotency_key = ?`, tenantID, idempotencyKey)
	event, err := scanDataEvent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func (s *SQLiteStore) AppendDataEventLog(ctx context.Context, entry DataEventLog) (*DataEventLog, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO data_events(id, tenant_id, source, event_type, operation, business_action_id, dataset_id, record_id, idempotency_key, result_status, created_by, applied_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.TenantID, entry.Source, entry.EventType, entry.Operation, entry.BusinessAction, entry.DatasetID, entry.RecordID, entry.IdempotencyKey, entry.ResultStatus, entry.CreatedBy, formatTime(entry.AppliedAt))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "constraint") {
			return nil, ErrAlreadyExists
		}
		return nil, err
	}
	return &entry, nil
}

func (s *SQLiteStore) QueryDataEvents(ctx context.Context, tenantID string, in QueryDataEventsInput) ([]DataEventLog, error) {
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT id, tenant_id, source, event_type, operation, business_action_id, dataset_id, record_id, idempotency_key, result_status, created_by, applied_at FROM data_events WHERE tenant_id = ?`
	args := []any{tenantID}
	if strings.TrimSpace(in.DatasetID) != "" {
		query += ` AND dataset_id = ?`
		args = append(args, strings.TrimSpace(in.DatasetID))
	}
	if strings.TrimSpace(in.RecordID) != "" {
		query += ` AND record_id = ?`
		args = append(args, strings.TrimSpace(in.RecordID))
	}
	if strings.TrimSpace(in.Source) != "" {
		query += ` AND source = ?`
		args = append(args, strings.TrimSpace(in.Source))
	}
	if strings.TrimSpace(in.EventType) != "" {
		query += ` AND event_type = ?`
		args = append(args, strings.TrimSpace(in.EventType))
	}
	if strings.TrimSpace(in.BusinessAction) != "" {
		query += ` AND business_action_id = ?`
		args = append(args, strings.TrimSpace(in.BusinessAction))
	}
	if strings.TrimSpace(in.IdempotencyKey) != "" {
		query += ` AND idempotency_key = ?`
		args = append(args, strings.TrimSpace(in.IdempotencyKey))
	}
	before := strings.TrimSpace(in.Before)
	beforeID := strings.TrimSpace(in.BeforeID)
	if before != "" {
		if beforeID != "" {
			query += ` AND (applied_at < ? OR (applied_at = ? AND id < ?))`
			args = append(args, before, before, beforeID)
		} else {
			query += ` AND applied_at < ?`
			args = append(args, before)
		}
	}
	query += ` ORDER BY applied_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DataEventLog{}
	for rows.Next() {
		event, err := scanDataEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func scanDataEvent(scanner interface{ Scan(dest ...any) error }) (DataEventLog, error) {
	var event DataEventLog
	var appliedAt string
	if err := scanner.Scan(&event.ID, &event.TenantID, &event.Source, &event.EventType, &event.Operation, &event.BusinessAction, &event.DatasetID, &event.RecordID, &event.IdempotencyKey, &event.ResultStatus, &event.CreatedBy, &appliedAt); err != nil {
		return DataEventLog{}, err
	}
	event.AppliedAt = parseTime(appliedAt)
	return event, nil
}
