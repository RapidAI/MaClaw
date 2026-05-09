package structureddata

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

func (s *SQLiteStore) AppendAuditLog(ctx context.Context, entry AuditLog) (*AuditLog, error) {
	metadataJSON, err := json.Marshal(entry.Metadata)
	if err != nil {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO audit_logs(id, tenant_id, user_id, action, dataset_id, target_type, target_id, summary, metadata_json, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, entry.ID, entry.TenantID, entry.UserID, entry.Action, entry.DatasetID, entry.TargetType, entry.TargetID, entry.Summary, string(metadataJSON), formatTime(entry.CreatedAt))
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

func (s *SQLiteStore) QueryAuditLogs(ctx context.Context, tenantID string, in QueryAuditLogsInput) ([]AuditLog, error) {
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT id, tenant_id, user_id, action, dataset_id, target_type, target_id, summary, metadata_json, created_at FROM audit_logs WHERE tenant_id = ?`
	args := []any{tenantID}
	if strings.TrimSpace(in.DatasetID) != "" {
		query += ` AND dataset_id = ?`
		args = append(args, strings.TrimSpace(in.DatasetID))
	}
	if strings.TrimSpace(in.Action) != "" {
		query += ` AND action = ?`
		args = append(args, strings.TrimSpace(in.Action))
	}
	if strings.TrimSpace(in.UserID) != "" {
		query += ` AND user_id = ?`
		args = append(args, strings.TrimSpace(in.UserID))
	}
	if strings.TrimSpace(in.TargetType) != "" {
		query += ` AND target_type = ?`
		args = append(args, strings.TrimSpace(in.TargetType))
	}
	if strings.TrimSpace(in.TargetID) != "" {
		query += ` AND target_id = ?`
		args = append(args, strings.TrimSpace(in.TargetID))
	}
	if strings.TrimSpace(in.Q) != "" {
		query += ` AND (summary LIKE ? OR action LIKE ? OR target_id LIKE ?)`
		like := "%" + strings.TrimSpace(in.Q) + "%"
		args = append(args, like, like, like)
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
	out := []AuditLog{}
	for rows.Next() {
		entry, err := scanAuditLog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

func scanAuditLog(scanner interface{ Scan(dest ...any) error }) (AuditLog, error) {
	var entry AuditLog
	var metadataJSON, createdAt string
	if err := scanner.Scan(&entry.ID, &entry.TenantID, &entry.UserID, &entry.Action, &entry.DatasetID, &entry.TargetType, &entry.TargetID, &entry.Summary, &metadataJSON, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return AuditLog{}, err
		}
		return AuditLog{}, err
	}
	entry.Metadata = map[string]any{}
	_ = json.Unmarshal([]byte(metadataJSON), &entry.Metadata)
	entry.CreatedAt = parseTime(createdAt)
	return entry, nil
}
