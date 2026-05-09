package structureddata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

func (s *SQLiteStore) CreateDataEventDeadLetter(ctx context.Context, entry DataEventDeadLetter) (*DataEventDeadLetter, error) {
	payloadJSON, err := json.Marshal(entry.Payload)
	if err != nil {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO data_event_dead_letters(id, tenant_id, status, source, event_type, business_action_id, dataset_id, record_id, idempotency_key, error, payload_json, created_by, resolved_by, resolution, created_at, updated_at, resolved_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.TenantID, entry.Status, entry.Source, entry.EventType, entry.BusinessAction, entry.DatasetID, entry.RecordID, entry.IdempotencyKey, entry.Error, string(payloadJSON), entry.CreatedBy, entry.ResolvedBy, entry.Resolution, formatTime(entry.CreatedAt), formatTime(entry.UpdatedAt), formatOptionalPlainTime(entry.ResolvedAt))
	if err != nil {
		return nil, err
	}
	return s.GetDataEventDeadLetter(ctx, entry.TenantID, entry.ID)
}

func (s *SQLiteStore) QueryDataEventDeadLetters(ctx context.Context, tenantID string, in QueryDataEventDeadLettersInput) ([]DataEventDeadLetter, error) {
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT id, tenant_id, status, source, event_type, business_action_id, dataset_id, record_id, idempotency_key, error, payload_json, created_by, resolved_by, resolution, created_at, updated_at, resolved_at FROM data_event_dead_letters WHERE tenant_id = ?`
	args := []any{tenantID}
	if strings.TrimSpace(in.Status) != "" {
		query += ` AND status = ?`
		args = append(args, strings.TrimSpace(in.Status))
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
	if strings.TrimSpace(in.DatasetID) != "" {
		query += ` AND dataset_id = ?`
		args = append(args, strings.TrimSpace(in.DatasetID))
	}
	if strings.TrimSpace(in.RecordID) != "" {
		query += ` AND record_id = ?`
		args = append(args, strings.TrimSpace(in.RecordID))
	}
	if strings.TrimSpace(in.IdempotencyKey) != "" {
		query += ` AND idempotency_key = ?`
		args = append(args, strings.TrimSpace(in.IdempotencyKey))
	}
	before := strings.TrimSpace(in.Before)
	beforeID := strings.TrimSpace(in.BeforeID)
	if before != "" {
		if beforeID != "" {
			query += ` AND (created_at < ? OR (created_at = ? AND id < ?))`
			args = append(args, before, before, beforeID)
		} else {
			query += ` AND created_at < ?`
			args = append(args, before)
		}
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DataEventDeadLetter{}
	for rows.Next() {
		item, err := scanDataEventDeadLetter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetDataEventDeadLetter(ctx context.Context, tenantID, deadLetterID string) (*DataEventDeadLetter, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, tenant_id, status, source, event_type, business_action_id, dataset_id, record_id, idempotency_key, error, payload_json, created_by, resolved_by, resolution, created_at, updated_at, resolved_at FROM data_event_dead_letters WHERE tenant_id = ? AND id = ?`, tenantID, strings.TrimSpace(deadLetterID))
	item, err := scanDataEventDeadLetter(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *SQLiteStore) UpdateDataEventDeadLetterStatus(ctx context.Context, tenantID, deadLetterID, status, resolvedBy, resolution string, now time.Time) (*DataEventDeadLetter, error) {
	resolvedAt := ""
	if strings.TrimSpace(status) == "resolved" || strings.TrimSpace(status) == "retried" {
		resolvedAt = formatTime(now)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE data_event_dead_letters SET status = ?, resolved_by = ?, resolution = ?, updated_at = ?, resolved_at = ? WHERE tenant_id = ? AND id = ?`,
		strings.TrimSpace(status), strings.TrimSpace(resolvedBy), strings.TrimSpace(resolution), formatTime(now), resolvedAt, tenantID, strings.TrimSpace(deadLetterID))
	if err != nil {
		return nil, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrRecordNotFound
	}
	return s.GetDataEventDeadLetter(ctx, tenantID, deadLetterID)
}

func scanDataEventDeadLetter(scanner interface{ Scan(dest ...any) error }) (DataEventDeadLetter, error) {
	var item DataEventDeadLetter
	var payloadJSON, createdAt, updatedAt, resolvedAt string
	if err := scanner.Scan(&item.ID, &item.TenantID, &item.Status, &item.Source, &item.EventType, &item.BusinessAction, &item.DatasetID, &item.RecordID, &item.IdempotencyKey, &item.Error, &payloadJSON, &item.CreatedBy, &item.ResolvedBy, &item.Resolution, &createdAt, &updatedAt, &resolvedAt); err != nil {
		return DataEventDeadLetter{}, err
	}
	_ = json.Unmarshal([]byte(payloadJSON), &item.Payload)
	if item.Payload == nil {
		item.Payload = map[string]any{}
	}
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	if strings.TrimSpace(resolvedAt) != "" {
		item.ResolvedAt = parseTime(resolvedAt)
	}
	return item, nil
}

func formatOptionalPlainTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return formatTime(value)
}
