package structureddata

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	operationPlanStatusPending   = "pending"
	operationPlanStatusApproved  = "approved"
	operationPlanStatusRejected  = "rejected"
	operationPlanStatusApplied   = "applied"
	operationPlanStatusCanceled  = "canceled"
	maxOperationPlanPreviewLimit = 100
)

func (s *Service) CreateOperationPlan(ctx context.Context, p Principal, in CreateOperationPlanInput) (*OperationPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	operation := strings.ToLower(strings.TrimSpace(in.Operation))
	if operation != "bulk_update_records" && operation != "bulk_delete_records" {
		return nil, fmt.Errorf("%w: unsupported operation plan operation", ErrInvalidInput)
	}
	datasetID := strings.TrimSpace(in.DatasetID)
	if datasetID == "" {
		return nil, fmt.Errorf("%w: dataset_id is required", ErrInvalidInput)
	}
	if _, err := s.store.GetDataset(ctx, p.TenantID, datasetID); err != nil {
		return nil, err
	}
	riskLevel, err := normalizeRiskLevel(in.RiskLevel)
	if err != nil {
		return nil, err
	}
	preview, err := s.previewOperationPlan(ctx, p, datasetID, operation, in.Request)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	plan := OperationPlan{
		ID:        newID("opplan"),
		TenantID:  p.TenantID,
		DatasetID: datasetID,
		Operation: operation,
		Status:    operationPlanStatusPending,
		Summary:   strings.TrimSpace(in.Summary),
		RiskLevel: riskLevel,
		Request:   cloneJSONMap(in.Request),
		Preview:   preview,
		CreatedBy: p.UserID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	out, err := s.store.CreateOperationPlan(ctx, plan)
	if err == nil {
		s.audit(ctx, p, "operation_plan.create", datasetID, "operation_plan", plan.ID, "Created operation plan "+plan.ID, map[string]any{"operation": operation, "risk_level": plan.RiskLevel, "matched": preview["matched"]})
	}
	return out, err
}

func (s *Service) ListOperationPlans(ctx context.Context, p Principal, in QueryOperationPlansInput) ([]OperationPlan, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	in.Operation = strings.ToLower(strings.TrimSpace(in.Operation))
	if in.Operation != "" && in.Operation != "bulk_update_records" && in.Operation != "bulk_delete_records" {
		return nil, fmt.Errorf("%w: invalid operation plan operation", ErrInvalidInput)
	}
	in.Status = strings.TrimSpace(in.Status)
	if in.Status != "" && !validOperationPlanStatus(in.Status) {
		return nil, fmt.Errorf("%w: invalid operation plan status", ErrInvalidInput)
	}
	return s.store.ListOperationPlans(ctx, p.TenantID, in)
}

func validOperationPlanStatus(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case operationPlanStatusPending, operationPlanStatusApproved, operationPlanStatusRejected, operationPlanStatusApplied, operationPlanStatusCanceled:
		return true
	default:
		return false
	}
}

func (s *Service) GetOperationPlan(ctx context.Context, p Principal, planID string) (*OperationPlan, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.store.GetOperationPlan(ctx, p.TenantID, strings.TrimSpace(planID))
}

func (s *Service) CancelOperationPlan(ctx context.Context, p Principal, planID string) (*OperationPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, err := s.store.GetOperationPlan(ctx, p.TenantID, strings.TrimSpace(planID))
	if err != nil {
		return nil, err
	}
	if plan.Status != operationPlanStatusPending && plan.Status != operationPlanStatusApproved {
		return nil, fmt.Errorf("%w: only pending or approved operation plans can be canceled", ErrInvalidInput)
	}
	out, err := s.store.UpdateOperationPlanStatus(ctx, p.TenantID, plan.ID, operationPlanStatusCanceled, p.UserID, "", s.now().UTC())
	if err == nil {
		s.audit(ctx, p, "operation_plan.cancel", plan.DatasetID, "operation_plan", plan.ID, "Canceled operation plan "+plan.ID, map[string]any{"operation": plan.Operation})
	}
	return out, err
}

func (s *Service) ReviewOperationPlan(ctx context.Context, p Principal, planID string, in ReviewOperationPlanInput) (*OperationPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, err := s.store.GetOperationPlan(ctx, p.TenantID, strings.TrimSpace(planID))
	if err != nil {
		return nil, err
	}
	if plan.Status != operationPlanStatusPending {
		return nil, fmt.Errorf("%w: only pending operation plans can be reviewed", ErrInvalidInput)
	}
	decision := strings.ToLower(strings.TrimSpace(in.Decision))
	status := ""
	action := ""
	summary := ""
	switch decision {
	case "approve", "approved":
		status = operationPlanStatusApproved
		action = "operation_plan.approve"
		summary = "Approved operation plan " + plan.ID
	case "reject", "rejected":
		status = operationPlanStatusRejected
		action = "operation_plan.reject"
		summary = "Rejected operation plan " + plan.ID
	default:
		return nil, fmt.Errorf("%w: review decision must be approve or reject", ErrInvalidInput)
	}
	out, err := s.store.UpdateOperationPlanStatus(ctx, p.TenantID, plan.ID, status, p.UserID, "", s.now().UTC())
	if err == nil {
		s.audit(ctx, p, action, plan.DatasetID, "operation_plan", plan.ID, summary, map[string]any{"operation": plan.Operation, "reason": strings.TrimSpace(in.Reason)})
	}
	return out, err
}

func (s *Service) ApplyOperationPlan(ctx context.Context, p Principal, planID string, in ApplyOperationPlanInput) (*OperationPlanApplyResult, error) {
	if !in.Confirm {
		return nil, fmt.Errorf("%w: operation plan apply requires confirm=true", ErrInvalidInput)
	}
	plan, err := s.GetOperationPlan(ctx, p, planID)
	if err != nil {
		return nil, err
	}
	if plan.Status != operationPlanStatusApproved {
		return nil, fmt.Errorf("%w: only approved operation plans can be applied", ErrInvalidInput)
	}
	if _, err := operationPlanQuery(plan.Request); err != nil {
		return nil, err
	}
	var result any
	switch plan.Operation {
	case "bulk_update_records":
		var req BulkUpdateRecordsInput
		if err := mapToStruct(plan.Request, &req); err != nil {
			return nil, err
		}
		req.DryRun = false
		req.Confirm = true
		if strings.TrimSpace(req.Reason) == "" {
			req.Reason = firstNonEmpty(strings.TrimSpace(in.Reason), "operation plan "+plan.ID)
		}
		result, err = s.BulkUpdateRecords(ctx, p, plan.DatasetID, req)
	case "bulk_delete_records":
		var req BulkDeleteRecordsInput
		if err := mapToStruct(plan.Request, &req); err != nil {
			return nil, err
		}
		req.DryRun = false
		req.Confirm = true
		if strings.TrimSpace(req.Reason) == "" {
			req.Reason = firstNonEmpty(strings.TrimSpace(in.Reason), "operation plan "+plan.ID)
		}
		result, err = s.BulkDeleteRecords(ctx, p, plan.DatasetID, req)
	default:
		err = fmt.Errorf("%w: unsupported operation plan operation", ErrInvalidInput)
	}
	if err != nil {
		return nil, err
	}
	updated, err := s.store.UpdateOperationPlanStatus(ctx, p.TenantID, plan.ID, operationPlanStatusApplied, p.UserID, p.UserID, s.now().UTC())
	if err != nil {
		return nil, err
	}
	s.audit(ctx, p, "operation_plan.apply", plan.DatasetID, "operation_plan", plan.ID, "Applied operation plan "+plan.ID, map[string]any{"operation": plan.Operation, "reason": strings.TrimSpace(in.Reason)})
	return &OperationPlanApplyResult{Plan: *updated, Result: result}, nil
}

func (s *Service) previewOperationPlan(ctx context.Context, p Principal, datasetID, operation string, request map[string]any) (map[string]any, error) {
	query, err := operationPlanQuery(request)
	if err != nil {
		return nil, err
	}
	if query.Limit <= 0 {
		query.Limit = maxOperationPlanPreviewLimit
	}
	records, err := s.store.QueryRecords(ctx, p.TenantID, datasetID, query)
	if err != nil {
		return nil, err
	}
	recordIDs := make([]string, 0, len(records))
	for _, record := range records {
		recordIDs = append(recordIDs, record.ID)
	}
	return map[string]any{
		"operation":  operation,
		"dataset_id": datasetID,
		"matched":    len(records),
		"record_ids": recordIDs,
		"limit":      query.Limit,
	}, nil
}

func operationPlanQuery(request map[string]any) (QueryRecordsInput, error) {
	var envelope struct {
		Query         QueryRecordsInput `json:"query"`
		Limit         int               `json:"limit,omitempty"`
		AllowFullScan bool              `json:"allow_full_scan,omitempty"`
	}
	if err := mapToStruct(request, &envelope); err != nil {
		return QueryRecordsInput{}, err
	}
	if envelope.Limit > 0 {
		envelope.Query.Limit = envelope.Limit
	}
	if envelope.Query.Limit > maxOperationPlanPreviewLimit {
		return QueryRecordsInput{}, fmt.Errorf("%w: operation plan limit must be less than or equal to %d", ErrInvalidInput, maxOperationPlanPreviewLimit)
	}
	if !envelope.AllowFullScan && !operationPlanHasBusinessScope(envelope.Query) {
		return QueryRecordsInput{}, fmt.Errorf("%w: operation plans require query.q, query.tag, query.filter, or allow_full_scan=true", ErrInvalidInput)
	}
	return envelope.Query, nil
}

func operationPlanHasBusinessScope(query QueryRecordsInput) bool {
	return strings.TrimSpace(query.Q) != "" || strings.TrimSpace(query.Tag) != "" || len(query.Filter) > 0
}

func normalizeRiskLevel(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "low", "medium", "high", "critical":
		return value, nil
	case "":
		return "medium", nil
	default:
		return "", fmt.Errorf("%w: risk_level must be low, medium, high, or critical", ErrInvalidInput)
	}
}

func mapToStruct(in map[string]any, out any) error {
	data, err := json.Marshal(in)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%w: invalid operation plan request", ErrInvalidInput)
	}
	return nil
}
