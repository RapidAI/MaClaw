package structureddata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

func (s *SQLiteStore) CreateOperationPlan(ctx context.Context, plan OperationPlan) (*OperationPlan, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO operation_plans(id, tenant_id, dataset_id, operation, status, summary, risk_level, request_json, preview_json, created_by, reviewed_by, applied_by, created_at, reviewed_at, applied_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		plan.ID, plan.TenantID, plan.DatasetID, plan.Operation, plan.Status, plan.Summary, plan.RiskLevel, jsonString(plan.Request), jsonString(plan.Preview), plan.CreatedBy, plan.ReviewedBy, plan.AppliedBy, formatTime(plan.CreatedAt), formatOptionalPlanTime(plan.ReviewedAt), formatOptionalPlanTime(plan.AppliedAt), formatTime(plan.UpdatedAt))
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

func (s *SQLiteStore) ListOperationPlans(ctx context.Context, tenantID string, in QueryOperationPlansInput) ([]OperationPlan, error) {
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
	if operation := strings.TrimSpace(in.Operation); operation != "" {
		clauses = append(clauses, "operation = ?")
		args = append(args, operation)
	}
	if status := strings.TrimSpace(in.Status); status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, status)
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
	rows, err := s.db.QueryContext(ctx, `SELECT id, tenant_id, dataset_id, operation, status, summary, risk_level, request_json, preview_json, created_by, reviewed_by, applied_by, created_at, reviewed_at, applied_at, updated_at FROM operation_plans WHERE `+strings.Join(clauses, " AND ")+` ORDER BY created_at DESC, id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OperationPlan{}
	for rows.Next() {
		plan, err := scanOperationPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, plan)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetOperationPlan(ctx context.Context, tenantID, planID string) (*OperationPlan, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, tenant_id, dataset_id, operation, status, summary, risk_level, request_json, preview_json, created_by, reviewed_by, applied_by, created_at, reviewed_at, applied_at, updated_at FROM operation_plans WHERE tenant_id = ? AND id = ?`, tenantID, strings.TrimSpace(planID))
	plan, err := scanOperationPlan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

func (s *SQLiteStore) UpdateOperationPlanStatus(ctx context.Context, tenantID, planID, status, reviewedBy, appliedBy string, now time.Time) (*OperationPlan, error) {
	plan, err := s.GetOperationPlan(ctx, tenantID, planID)
	if err != nil {
		return nil, err
	}
	plan.Status = strings.TrimSpace(status)
	if reviewedBy != "" {
		plan.ReviewedBy = strings.TrimSpace(reviewedBy)
		plan.ReviewedAt = now
	}
	if appliedBy != "" {
		plan.AppliedBy = strings.TrimSpace(appliedBy)
		plan.AppliedAt = now
	}
	plan.UpdatedAt = now
	res, err := s.db.ExecContext(ctx, `UPDATE operation_plans SET status = ?, reviewed_by = ?, reviewed_at = ?, applied_by = ?, applied_at = ?, updated_at = ? WHERE tenant_id = ? AND id = ?`, plan.Status, plan.ReviewedBy, formatOptionalPlanTime(plan.ReviewedAt), plan.AppliedBy, formatOptionalPlanTime(plan.AppliedAt), formatTime(plan.UpdatedAt), tenantID, planID)
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
	return plan, nil
}

func scanOperationPlan(scanner interface{ Scan(dest ...any) error }) (OperationPlan, error) {
	var plan OperationPlan
	var requestJSON, previewJSON, createdAt, reviewedAt, appliedAt, updatedAt string
	if err := scanner.Scan(&plan.ID, &plan.TenantID, &plan.DatasetID, &plan.Operation, &plan.Status, &plan.Summary, &plan.RiskLevel, &requestJSON, &previewJSON, &plan.CreatedBy, &plan.ReviewedBy, &plan.AppliedBy, &createdAt, &reviewedAt, &appliedAt, &updatedAt); err != nil {
		return OperationPlan{}, err
	}
	plan.Request = map[string]any{}
	plan.Preview = map[string]any{}
	_ = json.Unmarshal([]byte(requestJSON), &plan.Request)
	_ = json.Unmarshal([]byte(previewJSON), &plan.Preview)
	plan.CreatedAt = parseTime(createdAt)
	plan.ReviewedAt = parseTime(reviewedAt)
	plan.AppliedAt = parseTime(appliedAt)
	plan.UpdatedAt = parseTime(updatedAt)
	return plan, nil
}

func formatOptionalPlanTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return formatTime(t)
}
