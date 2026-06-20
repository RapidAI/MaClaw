package structureddata

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (s *Service) MISInbox(ctx context.Context, p Principal, in QueryMISInboxInput) (*MISInboxResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	datasetID := strings.TrimSpace(in.DatasetID)
	typeFilter := strings.TrimSpace(in.Type)
	statusFilter := strings.TrimSpace(in.Status)
	if err := validateMISInboxTypeFilter(typeFilter); err != nil {
		return nil, err
	}
	if err := validateMISInboxStatusFilter(statusFilter); err != nil {
		return nil, err
	}
	before := strings.TrimSpace(in.Before)
	beforeID := strings.TrimSpace(in.BeforeID)
	items := []MISInboxItem{}
	if typeMatches(typeFilter, "approval") {
		approvals, err := s.store.ListRecordApprovals(ctx, p.TenantID, QueryRecordApprovalsInput{DatasetID: datasetID, Status: defaultInboxStatus(statusFilter, recordApprovalStatusPending, in.IncludeOK), Limit: limit, Before: before, BeforeID: beforeID})
		if err != nil {
			return nil, err
		}
		for _, approval := range approvals {
			if !in.IncludeOK && approval.Status != recordApprovalStatusPending {
				continue
			}
			overdue := approval.Status == recordApprovalStatusPending && !approval.DueAt.IsZero() && approval.DueAt.Before(s.now().UTC())
			metadata := recordApprovalMetadata(approval)
			metadata["overdue"] = overdue
			items = append(items, MISInboxItem{
				ID:          approval.ID,
				Type:        "approval",
				Severity:    approvalInboxSeverity(approval, overdue),
				Status:      approval.Status,
				DatasetID:   approval.DatasetID,
				RecordID:    approval.RecordID,
				Title:       "Approval pending: " + approval.RecordID,
				Summary:     approval.Summary,
				Action:      "review_record_approval",
				TargetURL:   "/api/v1/data/approvals/" + approval.ID,
				Metadata:    metadata,
				CreatedBy:   approval.CreatedBy,
				CreatedAt:   approval.CreatedAt,
				UpdatedAt:   approval.UpdatedAt,
				Recommended: approvalInboxRecommendation(approval, overdue),
			})
		}
	}
	if typeMatches(typeFilter, "operation_plan") {
		plans, err := s.store.ListOperationPlans(ctx, p.TenantID, QueryOperationPlansInput{DatasetID: datasetID, Status: defaultInboxStatus(statusFilter, operationPlanStatusPending, in.IncludeOK), Limit: limit, Before: before, BeforeID: beforeID})
		if err != nil {
			return nil, err
		}
		for _, plan := range plans {
			if !in.IncludeOK && plan.Status != operationPlanStatusPending {
				continue
			}
			items = append(items, MISInboxItem{
				ID:          plan.ID,
				Type:        "operation_plan",
				Severity:    operationPlanInboxSeverity(plan.RiskLevel),
				Status:      plan.Status,
				DatasetID:   plan.DatasetID,
				Title:       "Operation plan pending: " + plan.Operation,
				Summary:     plan.Summary,
				Action:      "review_operation_plan",
				TargetURL:   "/api/v1/data/operation-plans/" + plan.ID,
				Metadata:    map[string]any{"operation": plan.Operation, "risk_level": plan.RiskLevel, "preview": plan.Preview},
				CreatedBy:   plan.CreatedBy,
				CreatedAt:   plan.CreatedAt,
				UpdatedAt:   plan.UpdatedAt,
				Recommended: "Review the impact preview before approval or rejection.",
			})
		}
	}
	if typeMatches(typeFilter, "import_job") {
		jobs, err := s.store.ListImportJobs(ctx, p.TenantID, QueryImportJobsInput{DatasetID: datasetID, Status: defaultInboxStatus(statusFilter, importJobStatusFailed, in.IncludeOK), Limit: limit, Before: before, BeforeID: beforeID})
		if err != nil {
			return nil, err
		}
		for _, job := range jobs {
			if !in.IncludeOK && job.Status != importJobStatusFailed {
				continue
			}
			items = append(items, MISInboxItem{
				ID:          job.ID,
				Type:        "import_job",
				Severity:    "high",
				Status:      job.Status,
				DatasetID:   job.DatasetID,
				Title:       "Import job needs attention",
				Summary:     job.Error,
				Action:      "get_import_job",
				TargetURL:   "/api/v1/data/import-jobs/" + job.ID,
				Metadata:    map[string]any{"kind": job.Kind, "total": job.Total, "imported": job.Imported, "valid": job.Valid},
				CreatedBy:   job.CreatedBy,
				CreatedAt:   job.CreatedAt,
				UpdatedAt:   firstNonZeroTime(job.FinishedAt, job.StartedAt, job.CreatedAt),
				Recommended: "Inspect validation errors, fix source data, then retry import.",
			})
		}
	}
	if typeMatches(typeFilter, "export_job") {
		jobs, err := s.store.ListExportJobs(ctx, p.TenantID, QueryExportJobsInput{DatasetID: datasetID, Status: defaultInboxStatus(statusFilter, exportJobStatusFailed, in.IncludeOK), Limit: limit, Before: before, BeforeID: beforeID})
		if err != nil {
			return nil, err
		}
		for _, job := range jobs {
			if !in.IncludeOK && job.Status != exportJobStatusFailed {
				continue
			}
			items = append(items, MISInboxItem{
				ID:          job.ID,
				Type:        "export_job",
				Severity:    "medium",
				Status:      job.Status,
				DatasetID:   job.DatasetID,
				Title:       "Export job needs attention",
				Summary:     job.Error,
				Action:      "get_export_job",
				TargetURL:   "/api/v1/data/export-jobs/" + job.ID,
				Metadata:    map[string]any{"format": job.Format, "total": job.Total, "bytes": job.Bytes},
				CreatedBy:   job.CreatedBy,
				CreatedAt:   job.CreatedAt,
				UpdatedAt:   firstNonZeroTime(job.FinishedAt, job.StartedAt, job.CreatedAt),
				Recommended: "Inspect export error and retry with a smaller or corrected query.",
			})
		}
	}
	if typeMatches(typeFilter, "event_dead_letter") {
		deadLetters, err := s.store.QueryDataEventDeadLetters(ctx, p.TenantID, QueryDataEventDeadLettersInput{DatasetID: datasetID, Status: defaultInboxStatus(statusFilter, "open", in.IncludeOK), Limit: limit, Before: before, BeforeID: beforeID})
		if err != nil {
			return nil, err
		}
		for _, item := range deadLetters {
			if !in.IncludeOK && item.Status != "open" {
				continue
			}
			items = append(items, MISInboxItem{
				ID:          item.ID,
				Type:        "event_dead_letter",
				Severity:    "high",
				Status:      item.Status,
				DatasetID:   item.DatasetID,
				RecordID:    item.RecordID,
				Title:       "Connector event failed",
				Summary:     item.Error,
				Action:      "retry_event_dead_letter",
				TargetURL:   "/api/v1/data/events/dead-letter/" + item.ID,
				Metadata:    map[string]any{"source": item.Source, "event_type": item.EventType, "business_action_id": item.BusinessAction, "idempotency_key": item.IdempotencyKey},
				CreatedBy:   item.CreatedBy,
				CreatedAt:   item.CreatedAt,
				UpdatedAt:   item.UpdatedAt,
				Recommended: "Inspect the saved payload and error, then retry after fixing data/schema or mark resolved.",
			})
		}
	}
	if typeMatches(typeFilter, "quality") {
		datasets, err := s.store.ListDatasets(ctx, p.TenantID)
		if err != nil {
			return nil, err
		}
		for _, dataset := range datasets {
			if datasetID != "" && dataset.ID != datasetID {
				continue
			}
			runs, err := s.store.ListQualityRuns(ctx, p.TenantID, dataset.ID, QueryQualityRunsInput{Limit: 10, Before: before, BeforeID: beforeID})
			if err != nil {
				return nil, err
			}
			for _, run := range runs {
				if !in.IncludeOK && run.Valid {
					continue
				}
				if statusFilter != "" && statusFilter != qualityStatus(run.Valid) {
					continue
				}
				items = append(items, MISInboxItem{
					ID:          run.ID,
					Type:        "quality",
					Severity:    qualityInboxSeverity(run),
					Status:      qualityStatus(run.Valid),
					DatasetID:   run.DatasetID,
					Title:       "Quality issues in " + run.DatasetID,
					Summary:     qualityInboxSummary(run),
					Action:      "get_quality_run",
					TargetURL:   "/api/v1/data/datasets/" + run.DatasetID + "/quality/runs/" + run.ID,
					Metadata:    map[string]any{"issue_count": run.IssueCount, "scanned": run.Scanned, "checks": run.Checks},
					CreatedBy:   run.CreatedBy,
					CreatedAt:   run.CreatedAt,
					UpdatedAt:   run.CreatedAt,
					Recommended: "Inspect quality issues, fix data or propose schema changes, then rerun quality check.",
				})
				break
			}
		}
	}
	sort.Slice(items, func(i, j int) bool {
		left := firstNonZeroTime(items[i].UpdatedAt, items[i].CreatedAt)
		right := firstNonZeroTime(items[j].UpdatedAt, items[j].CreatedAt)
		if left.Equal(right) {
			return items[i].ID > items[j].ID
		}
		return left.After(right)
	})
	hasMore := len(items) >= limit
	if hasMore {
		items = items[:limit]
	}
	result := &MISInboxResult{Items: items, Limit: limit, HasMore: hasMore, GeneratedAt: s.now().UTC()}
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		result.NextBefore = firstNonZeroTime(last.UpdatedAt, last.CreatedAt).Format(time.RFC3339Nano)
		result.NextBeforeID = last.ID
	}
	return result, nil
}

func (s *Service) MISInboxSummary(ctx context.Context, p Principal, in QueryMISInboxInput) (*MISInboxSummary, error) {
	in.Limit = 500
	result, err := s.MISInbox(ctx, p, in)
	if err != nil {
		return nil, err
	}
	out := &MISInboxSummary{
		ByType:      map[string]int{},
		BySeverity:  map[string]int{},
		ByStatus:    map[string]int{},
		GeneratedAt: result.GeneratedAt,
	}
	for _, item := range result.Items {
		out.Total++
		out.ByType[strings.TrimSpace(item.Type)]++
		out.BySeverity[strings.TrimSpace(item.Severity)]++
		out.ByStatus[strings.TrimSpace(item.Status)]++
		if item.Severity == "critical" {
			out.Critical++
		}
		if item.Severity == "high" {
			out.High++
		}
		if metadataBool(item.Metadata, "overdue") {
			out.Overdue++
		}
	}
	return out, nil
}

func typeMatches(filter, value string) bool {
	return strings.TrimSpace(filter) == "" || strings.EqualFold(strings.TrimSpace(filter), value)
}

func validateMISInboxTypeFilter(value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "approval", "operation_plan", "import_job", "export_job", "event_dead_letter", "quality":
		return nil
	default:
		return fmt.Errorf("%w: invalid inbox type", ErrInvalidInput)
	}
}

func validateMISInboxStatusFilter(value string) error {
	status := strings.ToLower(strings.TrimSpace(value))
	if status == "" {
		return nil
	}
	if _, ok := validMISInboxStatuses[status]; ok {
		return nil
	}
	return fmt.Errorf("%w: invalid inbox status", ErrInvalidInput)
}

var validMISInboxStatuses = map[string]struct{}{
	recordApprovalStatusPending:  {},
	recordApprovalStatusApproved: {},
	recordApprovalStatusRejected: {},
	operationPlanStatusApplied:   {},
	operationPlanStatusCanceled:  {},
	importJobStatusQueued:        {},
	importJobStatusRunning:       {},
	importJobStatusCompleted:     {},
	importJobStatusFailed:        {},
	"open":                       {},
	"resolved":                   {},
	"retried":                    {},
	"ok":                         {},
	"issue":                      {},
}

func defaultInboxStatus(status, fallback string, includeOK bool) string {
	status = strings.TrimSpace(status)
	if status != "" || includeOK {
		return status
	}
	return fallback
}

func operationPlanInboxSeverity(risk string) string {
	switch strings.ToLower(strings.TrimSpace(risk)) {
	case "critical":
		return "critical"
	case "high":
		return "high"
	case "low":
		return "low"
	default:
		return "medium"
	}
}

func approvalInboxSeverity(approval RecordApproval, overdue bool) string {
	if overdue {
		return "high"
	}
	switch strings.ToLower(strings.TrimSpace(approval.Priority)) {
	case "critical":
		return "critical"
	case "high":
		return "high"
	case "low":
		return "low"
	default:
		return "medium"
	}
}

func approvalInboxRecommendation(approval RecordApproval, overdue bool) string {
	if overdue {
		return "Approval is overdue; review or reassign immediately."
	}
	if strings.TrimSpace(approval.AssignedTo) != "" {
		return "Assigned approval; reviewer should approve or reject before due date."
	}
	return "Review and approve or reject the business approval."
}

func qualityStatus(valid bool) string {
	if valid {
		return "ok"
	}
	return "issue"
}

func qualityInboxSeverity(run QualityCheckResult) string {
	if run.IssueCount >= 100 {
		return "critical"
	}
	if run.IssueCount >= 10 {
		return "high"
	}
	return "medium"
}

func qualityInboxSummary(run QualityCheckResult) string {
	if run.IssueCount == 1 {
		return "1 quality issue found"
	}
	return strconv.Itoa(run.IssueCount) + " quality issues found"
}

func metadataBool(metadata map[string]any, key string) bool {
	if metadata == nil {
		return false
	}
	value, ok := metadata[key]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}
