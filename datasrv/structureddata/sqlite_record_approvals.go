package structureddata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

func (s *SQLiteStore) CreateRecordApproval(ctx context.Context, approval RecordApproval) (*RecordApproval, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO record_approvals(id, tenant_id, dataset_id, record_id, status, kind, summary, request_json, decision, reason, created_by, reviewed_by, created_at, reviewed_at, updated_at, assigned_to, due_at, priority) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		approval.ID, approval.TenantID, approval.DatasetID, approval.RecordID, approval.Status, approval.Kind, approval.Summary, jsonString(approval.Request), approval.Decision, approval.Reason, approval.CreatedBy, approval.ReviewedBy, formatTime(approval.CreatedAt), formatOptionalPlanTime(approval.ReviewedAt), formatTime(approval.UpdatedAt), approval.AssignedTo, formatOptionalPlanTime(approval.DueAt), approval.Priority)
	if err != nil {
		return nil, err
	}
	return &approval, nil
}

func (s *SQLiteStore) ListRecordApprovals(ctx context.Context, tenantID string, in QueryRecordApprovalsInput) ([]RecordApproval, error) {
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	clauses := []string{"tenant_id = ?"}
	args := []any{tenantID}
	if datasetID := strings.TrimSpace(in.DatasetID); datasetID != "" {
		clauses = append(clauses, "dataset_id = ?")
		args = append(args, datasetID)
	}
	if recordID := strings.TrimSpace(in.RecordID); recordID != "" {
		clauses = append(clauses, "record_id = ?")
		args = append(args, recordID)
	}
	if status := strings.TrimSpace(in.Status); status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, status)
	}
	if kind := strings.TrimSpace(in.Kind); kind != "" {
		clauses = append(clauses, "kind = ?")
		args = append(args, kind)
	}
	if assignedTo := strings.TrimSpace(in.AssignedTo); assignedTo != "" {
		clauses = append(clauses, "assigned_to = ?")
		args = append(args, assignedTo)
	}
	if in.Overdue {
		clauses = append(clauses, "status = ? AND due_at != '' AND due_at < ?")
		args = append(args, recordApprovalStatusPending, formatTime(time.Now().UTC()))
	}
	before := strings.TrimSpace(in.Before)
	beforeID := strings.TrimSpace(in.BeforeID)
	if before != "" {
		if beforeID != "" {
			clauses = append(clauses, "(created_at < ? OR (created_at = ? AND id < ?))")
			args = append(args, before, before, beforeID)
		} else {
			clauses = append(clauses, "created_at < ?")
			args = append(args, before)
		}
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `SELECT id, tenant_id, dataset_id, record_id, status, kind, summary, request_json, decision, reason, created_by, reviewed_by, created_at, reviewed_at, updated_at, assigned_to, due_at, priority FROM record_approvals WHERE `+strings.Join(clauses, " AND ")+` ORDER BY created_at DESC, id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RecordApproval{}
	for rows.Next() {
		approval, err := scanRecordApproval(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, approval)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetRecordApproval(ctx context.Context, tenantID, approvalID string) (*RecordApproval, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, tenant_id, dataset_id, record_id, status, kind, summary, request_json, decision, reason, created_by, reviewed_by, created_at, reviewed_at, updated_at, assigned_to, due_at, priority FROM record_approvals WHERE tenant_id = ? AND id = ?`, tenantID, strings.TrimSpace(approvalID))
	approval, err := scanRecordApproval(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	return &approval, nil
}

func (s *SQLiteStore) UpdateRecordApprovalStatus(ctx context.Context, tenantID, approvalID, status, decision, reason, reviewedBy string, now time.Time) (*RecordApproval, error) {
	approval, err := s.GetRecordApproval(ctx, tenantID, approvalID)
	if err != nil {
		return nil, err
	}
	approval.Status = strings.TrimSpace(status)
	approval.Decision = strings.TrimSpace(decision)
	approval.Reason = strings.TrimSpace(reason)
	approval.ReviewedBy = strings.TrimSpace(reviewedBy)
	approval.ReviewedAt = now
	approval.UpdatedAt = now
	res, err := s.db.ExecContext(ctx, `UPDATE record_approvals SET status = ?, decision = ?, reason = ?, reviewed_by = ?, reviewed_at = ?, updated_at = ? WHERE tenant_id = ? AND id = ?`,
		approval.Status, approval.Decision, approval.Reason, approval.ReviewedBy, formatOptionalPlanTime(approval.ReviewedAt), formatTime(approval.UpdatedAt), tenantID, approvalID)
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
	return approval, nil
}

func scanRecordApproval(scanner interface{ Scan(dest ...any) error }) (RecordApproval, error) {
	var approval RecordApproval
	var requestJSON, createdAt, reviewedAt, updatedAt, dueAt string
	if err := scanner.Scan(&approval.ID, &approval.TenantID, &approval.DatasetID, &approval.RecordID, &approval.Status, &approval.Kind, &approval.Summary, &requestJSON, &approval.Decision, &approval.Reason, &approval.CreatedBy, &approval.ReviewedBy, &createdAt, &reviewedAt, &updatedAt, &approval.AssignedTo, &dueAt, &approval.Priority); err != nil {
		return RecordApproval{}, err
	}
	approval.Request = map[string]any{}
	_ = json.Unmarshal([]byte(requestJSON), &approval.Request)
	approval.CreatedAt = parseTime(createdAt)
	approval.ReviewedAt = parseTime(reviewedAt)
	approval.UpdatedAt = parseTime(updatedAt)
	approval.DueAt = parseTime(dueAt)
	return approval, nil
}
