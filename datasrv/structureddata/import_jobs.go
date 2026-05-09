package structureddata

import (
	"context"
	"fmt"
	"strings"
)

const (
	importJobStatusQueued    = "queued"
	importJobStatusRunning   = "running"
	importJobStatusCompleted = "completed"
	importJobStatusFailed    = "failed"
)

func (s *Service) StartCSVImportJob(ctx context.Context, p Principal, datasetID string, in ImportCSVInput) (*ImportJob, error) {
	datasetID = strings.TrimSpace(datasetID)
	if strings.TrimSpace(in.CSVText) == "" {
		return nil, fmt.Errorf("%w: csv is required", ErrInvalidInput)
	}
	if _, err := s.GetDataset(ctx, p, datasetID); err != nil {
		return nil, err
	}
	fields, err := s.ListFields(ctx, p, datasetID)
	if err != nil {
		return nil, err
	}
	records, err := parseCSVRecords(in.CSVText, fields)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	job := ImportJob{
		ID:        newID("import_job"),
		TenantID:  p.TenantID,
		DatasetID: datasetID,
		Kind:      "csv",
		Status:    importJobStatusQueued,
		DryRun:    in.DryRun,
		Total:     len(records),
		CreatedBy: p.UserID,
		CreatedAt: now,
	}
	out, err := s.store.UpsertImportJob(ctx, job)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, p, "import_job.create", datasetID, "import_job", job.ID, "Queued CSV import job", map[string]any{"dry_run": in.DryRun})
	go s.runCSVImportJob(p, job, in)
	return out, nil
}

func (s *Service) StartJSONLImportJob(ctx context.Context, p Principal, datasetID string, in ImportJSONLInput) (*ImportJob, error) {
	datasetID = strings.TrimSpace(datasetID)
	if strings.TrimSpace(in.JSONLText) == "" {
		return nil, fmt.Errorf("%w: jsonl is required", ErrInvalidInput)
	}
	records, err := parseJSONLRecords(in.JSONLText)
	if err != nil {
		return nil, err
	}
	if _, err := s.GetDataset(ctx, p, datasetID); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	job := ImportJob{
		ID:        newID("import_job"),
		TenantID:  p.TenantID,
		DatasetID: datasetID,
		Kind:      "jsonl",
		Status:    importJobStatusQueued,
		DryRun:    in.DryRun,
		Total:     len(records),
		CreatedBy: p.UserID,
		CreatedAt: now,
	}
	out, err := s.store.UpsertImportJob(ctx, job)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, p, "import_job.create", datasetID, "import_job", job.ID, "Queued JSONL import job", map[string]any{"dry_run": in.DryRun, "record_count": len(records)})
	go s.runJSONLImportJob(p, job, in)
	return out, nil
}

func (s *Service) StartBatchImportJob(ctx context.Context, p Principal, datasetID string, in BatchImportRecordsInput) (*ImportJob, error) {
	datasetID = strings.TrimSpace(datasetID)
	if len(in.Records) == 0 {
		return nil, fmt.Errorf("%w: records are required", ErrInvalidInput)
	}
	if err := validateBatchImportRecordCount(len(in.Records)); err != nil {
		return nil, err
	}
	if _, err := s.GetDataset(ctx, p, datasetID); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	job := ImportJob{
		ID:        newID("import_job"),
		TenantID:  p.TenantID,
		DatasetID: datasetID,
		Kind:      "batch",
		Status:    importJobStatusQueued,
		DryRun:    in.DryRun,
		Total:     len(in.Records),
		CreatedBy: p.UserID,
		CreatedAt: now,
	}
	out, err := s.store.UpsertImportJob(ctx, job)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, p, "import_job.create", datasetID, "import_job", job.ID, "Queued batch import job", map[string]any{"dry_run": in.DryRun, "record_count": len(in.Records)})
	go s.runBatchImportJob(p, job, in)
	return out, nil
}

func (s *Service) runJSONLImportJob(p Principal, job ImportJob, in ImportJSONLInput) {
	ctx := context.Background()
	now := s.now().UTC()
	job.Status = importJobStatusRunning
	job.StartedAt = now
	_, _ = s.store.UpsertImportJob(ctx, job)
	result, err := s.ImportRecordsJSONL(ctx, p, job.DatasetID, in)
	job.FinishedAt = s.now().UTC()
	if err != nil {
		job.Status = importJobStatusFailed
		job.Error = err.Error()
		_, _ = s.store.UpsertImportJob(ctx, job)
		s.audit(ctx, p, "import_job.failed", job.DatasetID, "import_job", job.ID, "JSONL import job failed", map[string]any{"error": err.Error()})
		return
	}
	job.Status = importJobStatusCompleted
	job.Result = result
	job.Total = result.Total
	job.Imported = result.Imported
	job.Valid = result.Valid
	_, _ = s.store.UpsertImportJob(ctx, job)
	s.audit(ctx, p, "import_job.completed", job.DatasetID, "import_job", job.ID, "JSONL import job completed", map[string]any{"total": result.Total, "imported": result.Imported, "valid": result.Valid, "dry_run": result.DryRun})
}

func (s *Service) runCSVImportJob(p Principal, job ImportJob, in ImportCSVInput) {
	ctx := context.Background()
	now := s.now().UTC()
	job.Status = importJobStatusRunning
	job.StartedAt = now
	_, _ = s.store.UpsertImportJob(ctx, job)
	result, err := s.ImportRecordsCSV(ctx, p, job.DatasetID, in)
	job.FinishedAt = s.now().UTC()
	if err != nil {
		job.Status = importJobStatusFailed
		job.Error = err.Error()
		_, _ = s.store.UpsertImportJob(ctx, job)
		s.audit(ctx, p, "import_job.failed", job.DatasetID, "import_job", job.ID, "CSV import job failed", map[string]any{"error": err.Error()})
		return
	}
	job.Status = importJobStatusCompleted
	job.Result = result
	job.Total = result.Total
	job.Imported = result.Imported
	job.Valid = result.Valid
	_, _ = s.store.UpsertImportJob(ctx, job)
	s.audit(ctx, p, "import_job.completed", job.DatasetID, "import_job", job.ID, "CSV import job completed", map[string]any{"total": result.Total, "imported": result.Imported, "valid": result.Valid, "dry_run": result.DryRun})
}

func (s *Service) runBatchImportJob(p Principal, job ImportJob, in BatchImportRecordsInput) {
	ctx := context.Background()
	now := s.now().UTC()
	job.Status = importJobStatusRunning
	job.StartedAt = now
	_, _ = s.store.UpsertImportJob(ctx, job)
	result, err := s.BatchImportRecords(ctx, p, job.DatasetID, in)
	job.FinishedAt = s.now().UTC()
	if err != nil {
		job.Status = importJobStatusFailed
		job.Error = err.Error()
		_, _ = s.store.UpsertImportJob(ctx, job)
		s.audit(ctx, p, "import_job.failed", job.DatasetID, "import_job", job.ID, "Batch import job failed", map[string]any{"error": err.Error()})
		return
	}
	job.Status = importJobStatusCompleted
	job.Result = result
	job.Total = result.Total
	job.Imported = result.Imported
	job.Valid = result.Valid
	_, _ = s.store.UpsertImportJob(ctx, job)
	s.audit(ctx, p, "import_job.completed", job.DatasetID, "import_job", job.ID, "Batch import job completed", map[string]any{"total": result.Total, "imported": result.Imported, "valid": result.Valid, "dry_run": result.DryRun})
}

func (s *Service) ListImportJobs(ctx context.Context, p Principal, in QueryImportJobsInput) ([]ImportJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	in.Status = strings.TrimSpace(in.Status)
	if in.Status != "" && !validImportJobStatus(in.Status) {
		return nil, fmt.Errorf("%w: invalid import job status", ErrInvalidInput)
	}
	return s.store.ListImportJobs(ctx, p.TenantID, in)
}

func validImportJobStatus(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case importJobStatusQueued, importJobStatusRunning, importJobStatusCompleted, importJobStatusFailed:
		return true
	default:
		return false
	}
}

func (s *Service) GetImportJob(ctx context.Context, p Principal, jobID string) (*ImportJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.store.GetImportJob(ctx, p.TenantID, strings.TrimSpace(jobID))
}
