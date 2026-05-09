package structureddata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

func (s *SQLiteStore) AppendQualityRun(ctx context.Context, run QualityCheckResult) (*QualityCheckResult, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO quality_runs(id, tenant_id, dataset_id, checks_json, scanned, valid, issue_count, issues_json, limit_value, include_warnings, created_by, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.TenantID, run.DatasetID, jsonString(run.Checks), run.Scanned, boolInt(run.Valid), run.IssueCount, jsonString(run.Issues), run.Limit, boolInt(run.IncludeWarnings), run.CreatedBy, formatTime(run.CreatedAt))
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *SQLiteStore) ListQualityRuns(ctx context.Context, tenantID, datasetID string, in QueryQualityRunsInput) ([]QualityCheckResult, error) {
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT id, tenant_id, dataset_id, checks_json, scanned, valid, issue_count, issues_json, limit_value, include_warnings, created_by, created_at FROM quality_runs WHERE tenant_id = ? AND dataset_id = ?`
	args := []any{tenantID, datasetID}
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
	out := []QualityCheckResult{}
	for rows.Next() {
		run, err := scanQualityRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetQualityRun(ctx context.Context, tenantID, datasetID, runID string) (*QualityCheckResult, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, tenant_id, dataset_id, checks_json, scanned, valid, issue_count, issues_json, limit_value, include_warnings, created_by, created_at FROM quality_runs WHERE tenant_id = ? AND dataset_id = ? AND id = ?`, tenantID, datasetID, runID)
	run, err := scanQualityRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func scanQualityRun(scanner interface{ Scan(dest ...any) error }) (QualityCheckResult, error) {
	var run QualityCheckResult
	var checksJSON, issuesJSON, createdAt string
	var valid, includeWarnings int
	if err := scanner.Scan(&run.ID, &run.TenantID, &run.DatasetID, &checksJSON, &run.Scanned, &valid, &run.IssueCount, &issuesJSON, &run.Limit, &includeWarnings, &run.CreatedBy, &createdAt); err != nil {
		return QualityCheckResult{}, err
	}
	_ = json.Unmarshal([]byte(checksJSON), &run.Checks)
	_ = json.Unmarshal([]byte(issuesJSON), &run.Issues)
	run.Valid = intBool(valid)
	run.IncludeWarnings = intBool(includeWarnings)
	run.CreatedAt = parseTime(createdAt)
	return run, nil
}
