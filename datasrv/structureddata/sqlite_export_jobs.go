package structureddata

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

func (s *SQLiteStore) UpsertExportJob(ctx context.Context, job ExportJob) (*ExportJob, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO export_jobs(id, tenant_id, dataset_id, format, status, total, bytes, error, result_text, created_by, created_at, started_at, finished_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET status = excluded.status, total = excluded.total, bytes = excluded.bytes, error = excluded.error, result_text = excluded.result_text, started_at = excluded.started_at, finished_at = excluded.finished_at`,
		job.ID, job.TenantID, job.DatasetID, job.Format, job.Status, job.Total, job.Bytes, job.Error, job.ResultText, job.CreatedBy, formatTime(job.CreatedAt), nullableTime(job.StartedAt), nullableTime(job.FinishedAt))
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (s *SQLiteStore) ListExportJobs(ctx context.Context, tenantID string, in QueryExportJobsInput) ([]ExportJob, error) {
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT id, tenant_id, dataset_id, format, status, total, bytes, error, result_text, created_by, created_at, started_at, finished_at FROM export_jobs WHERE tenant_id = ?`
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
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExportJob{}
	for rows.Next() {
		job, err := scanExportJob(rows)
		if err != nil {
			return nil, err
		}
		job.ResultText = ""
		job.DownloadPath = exportJobDownloadPath(job.ID)
		out = append(out, job)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetExportJob(ctx context.Context, tenantID, jobID string) (*ExportJob, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, tenant_id, dataset_id, format, status, total, bytes, error, result_text, created_by, created_at, started_at, finished_at FROM export_jobs WHERE tenant_id = ? AND id = ?`, tenantID, jobID)
	job, err := scanExportJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	job.DownloadPath = exportJobDownloadPath(job.ID)
	return &job, nil
}

func scanExportJob(scanner interface{ Scan(dest ...any) error }) (ExportJob, error) {
	var job ExportJob
	var createdAt, startedAt, finishedAt string
	if err := scanner.Scan(&job.ID, &job.TenantID, &job.DatasetID, &job.Format, &job.Status, &job.Total, &job.Bytes, &job.Error, &job.ResultText, &job.CreatedBy, &createdAt, &startedAt, &finishedAt); err != nil {
		return ExportJob{}, err
	}
	job.CreatedAt = parseTime(createdAt)
	job.StartedAt = parseTime(startedAt)
	job.FinishedAt = parseTime(finishedAt)
	return job, nil
}

func exportJobDownloadPath(jobID string) string {
	if strings.TrimSpace(jobID) == "" {
		return ""
	}
	return "/api/v1/data/export-jobs/" + strings.TrimSpace(jobID) + "/download"
}
