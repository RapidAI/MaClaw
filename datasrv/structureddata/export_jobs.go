package structureddata

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	exportJobStatusQueued    = "queued"
	exportJobStatusRunning   = "running"
	exportJobStatusCompleted = "completed"
	exportJobStatusFailed    = "failed"
)

func (s *Service) StartExportJob(ctx context.Context, p Principal, datasetID string, in StartExportJobInput) (*ExportJob, error) {
	datasetID = strings.TrimSpace(datasetID)
	format, err := normalizeExportFormat(in.Format)
	if err != nil {
		return nil, err
	}
	if _, err := s.GetDataset(ctx, p, datasetID); err != nil {
		return nil, err
	}
	limit, err := normalizeExportLimit(in.Limit, 50000)
	if err != nil {
		return nil, err
	}
	in.Limit = limit
	now := s.now().UTC()
	job := ExportJob{
		ID:        newID("export_job"),
		TenantID:  p.TenantID,
		DatasetID: datasetID,
		Format:    format,
		Status:    exportJobStatusQueued,
		CreatedBy: p.UserID,
		CreatedAt: now,
	}
	out, err := s.store.UpsertExportJob(ctx, job)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, p, "export_job.create", datasetID, "export_job", job.ID, "Queued "+strings.ToUpper(format)+" export job", map[string]any{"format": format, "limit": in.Limit})
	go s.runExportJob(p, job, in)
	return out, nil
}

func normalizeExportLimit(limit, max int) (int, error) {
	if max <= 0 {
		max = 5000
	}
	if limit > max {
		return 0, fmt.Errorf("%w: export limit must be less than or equal to %d", ErrInvalidInput, max)
	}
	if limit <= 0 {
		return max, nil
	}
	return limit, nil
}

func (s *Service) queryRecordsForExport(ctx context.Context, p Principal, datasetID string, in QueryRecordsInput, maxLimit int) ([]Record, error) {
	if maxLimit <= 0 {
		maxLimit = 5000
	}
	limit := in.Limit
	if limit <= 0 || limit > maxLimit {
		limit = maxLimit
	}
	if len(in.Sort) > 0 {
		if limit > 500 {
			limit = 500
		}
		in.Limit = limit
		return s.QueryRecords(ctx, p, datasetID, in)
	}

	records := []Record{}
	before := strings.TrimSpace(in.Before)
	beforeID := strings.TrimSpace(in.BeforeID)
	for len(records) < limit {
		pageLimit := limit - len(records)
		if pageLimit > 500 {
			pageLimit = 500
		}
		pageInput := in
		pageInput.Limit = pageLimit
		pageInput.Before = before
		pageInput.BeforeID = beforeID
		page, err := s.QueryRecords(ctx, p, datasetID, pageInput)
		if err != nil {
			return nil, err
		}
		records = append(records, page...)
		if len(page) < pageLimit || len(page) == 0 {
			break
		}
		last := page[len(page)-1]
		before = last.CreatedAt.Format(time.RFC3339Nano)
		beforeID = last.ID
	}
	return records, nil
}
func (s *Service) runExportJob(p Principal, job ExportJob, in StartExportJobInput) {
	ctx := context.Background()
	job.Status = exportJobStatusRunning
	job.StartedAt = s.now().UTC()
	_, _ = s.store.UpsertExportJob(ctx, job)

	records, err := s.queryRecordsForExport(ctx, p, job.DatasetID, in.QueryRecordsInput, 50000)
	if err == nil {
		var buf bytes.Buffer
		switch job.Format {
		case "csv":
			var fields []FieldDefinition
			fields, err = s.ListFields(ctx, p, job.DatasetID)
			if err == nil {
				err = writeRecordsCSV(&buf, fields, records)
			}
		case "jsonl":
			err = writeRecordsJSONL(&buf, records)
		default:
			err = fmt.Errorf("%w: unsupported export format", ErrInvalidInput)
		}
		if err == nil {
			job.ResultText = buf.String()
			job.Bytes = buf.Len()
			job.Total = len(records)
		}
	}
	job.FinishedAt = s.now().UTC()
	if err != nil {
		job.Status = exportJobStatusFailed
		job.Error = err.Error()
		_, _ = s.store.UpsertExportJob(ctx, job)
		s.audit(ctx, p, "export_job.failed", job.DatasetID, "export_job", job.ID, "Export job failed", map[string]any{"format": job.Format, "error": err.Error()})
		return
	}
	job.Status = exportJobStatusCompleted
	_, _ = s.store.UpsertExportJob(ctx, job)
	s.audit(ctx, p, "export_job.completed", job.DatasetID, "export_job", job.ID, "Export job completed", map[string]any{"format": job.Format, "total": job.Total, "bytes": job.Bytes})
}

func (s *Service) ListExportJobs(ctx context.Context, p Principal, in QueryExportJobsInput) ([]ExportJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	in.Status = strings.TrimSpace(in.Status)
	if in.Status != "" && !validExportJobStatus(in.Status) {
		return nil, fmt.Errorf("%w: invalid export job status", ErrInvalidInput)
	}
	return s.store.ListExportJobs(ctx, p.TenantID, in)
}

func validExportJobStatus(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case exportJobStatusQueued, exportJobStatusRunning, exportJobStatusCompleted, exportJobStatusFailed:
		return true
	default:
		return false
	}
}

func (s *Service) GetExportJob(ctx context.Context, p Principal, jobID string) (*ExportJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.store.GetExportJob(ctx, p.TenantID, strings.TrimSpace(jobID))
}

func normalizeExportFormat(format string) (string, error) {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "jsonl"
	}
	switch format {
	case "csv", "jsonl":
		return format, nil
	default:
		return "", fmt.Errorf("%w: format must be csv or jsonl", ErrInvalidInput)
	}
}
