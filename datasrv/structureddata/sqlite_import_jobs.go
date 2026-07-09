package structureddata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

func (s *SQLiteStore) UpsertImportJob(ctx context.Context, job ImportJob) (*ImportJob, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO import_jobs(id, tenant_id, dataset_id, kind, status, dry_run, total, imported, valid, error, result_json, created_by, created_at, started_at, finished_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET status = excluded.status, total = excluded.total, imported = excluded.imported, valid = excluded.valid, error = excluded.error, result_json = excluded.result_json, started_at = excluded.started_at, finished_at = excluded.finished_at`,
		job.ID, job.TenantID, job.DatasetID, job.Kind, job.Status, boolInt(job.DryRun), job.Total, job.Imported, boolInt(job.Valid), job.Error, jsonString(job.Result), job.CreatedBy, formatTime(job.CreatedAt), nullableTime(job.StartedAt), nullableTime(job.FinishedAt))
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (s *SQLiteStore) ListImportJobs(ctx context.Context, tenantID string, in QueryImportJobsInput) ([]ImportJob, error) {
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT id, tenant_id, dataset_id, kind, status, dry_run, total, imported, valid, error, result_json, created_by, created_at, started_at, finished_at FROM import_jobs WHERE tenant_id = ?`
	args := []any{tenantID}
	if strings.TrimSpace(in.DatasetID) != "" {
		query += ` AND dataset_id = ?`
		args = append(args, strings.TrimSpace(in.DatasetID))
	}
	if strings.TrimSpace(in.Status) != "" {
		query += ` AND status = ?`
		args = append(args, strings.TrimSpace(in.Status))
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
	rows, err := s.queryDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ImportJob{}
	for rows.Next() {
		job, err := scanImportJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetImportJob(ctx context.Context, tenantID, jobID string) (*ImportJob, error) {
	row := s.queryDB().QueryRowContext(ctx, `SELECT id, tenant_id, dataset_id, kind, status, dry_run, total, imported, valid, error, result_json, created_by, created_at, started_at, finished_at FROM import_jobs WHERE tenant_id = ? AND id = ?`, tenantID, jobID)
	job, err := scanImportJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func scanImportJob(scanner interface{ Scan(dest ...any) error }) (ImportJob, error) {
	var job ImportJob
	var dryRun, valid int
	var resultJSON, createdAt, startedAt, finishedAt string
	if err := scanner.Scan(&job.ID, &job.TenantID, &job.DatasetID, &job.Kind, &job.Status, &dryRun, &job.Total, &job.Imported, &valid, &job.Error, &resultJSON, &job.CreatedBy, &createdAt, &startedAt, &finishedAt); err != nil {
		return ImportJob{}, err
	}
	job.DryRun = intBool(dryRun)
	job.Valid = intBool(valid)
	job.CreatedAt = parseTime(createdAt)
	job.StartedAt = parseTime(startedAt)
	job.FinishedAt = parseTime(finishedAt)
	if strings.TrimSpace(resultJSON) != "" && strings.TrimSpace(resultJSON) != "{}" {
		var result BatchImportRecordsResult
		if err := json.Unmarshal([]byte(resultJSON), &result); err == nil {
			job.Result = &result
		}
	}
	return job, nil
}

func nullableTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return formatTime(t)
}
