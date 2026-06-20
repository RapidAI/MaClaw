package structureddata

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	recordApprovalStatusPending  = "pending"
	recordApprovalStatusApproved = "approved"
	recordApprovalStatusRejected = "rejected"
)

func (s *Service) CreateRecordApproval(ctx context.Context, p Principal, datasetID, recordID string, in CreateRecordApprovalInput) (*RecordApproval, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	datasetID = strings.TrimSpace(datasetID)
	recordID = strings.TrimSpace(recordID)
	if _, err := s.store.GetRecord(ctx, p.TenantID, datasetID, recordID); err != nil {
		return nil, err
	}
	dueAt, err := parseOptionalBusinessTime(in.DueAt)
	if err != nil {
		return nil, err
	}
	priority, err := normalizeApprovalPriority(in.Priority)
	if err != nil {
		return nil, err
	}
	kind := normalizeApprovalKind(in.Kind)
	if existing, err := s.store.ListRecordApprovals(ctx, p.TenantID, QueryRecordApprovalsInput{
		DatasetID: datasetID,
		RecordID:  recordID,
		Status:    recordApprovalStatusPending,
		Kind:      kind,
		Limit:     1,
	}); err != nil {
		return nil, err
	} else if len(existing) > 0 {
		reused := existing[0]
		reused.Reused = true
		s.audit(ctx, p, "approval.reuse", datasetID, "record", recordID, "Reused pending approval "+reused.ID, map[string]any{"approval_id": reused.ID, "kind": reused.Kind})
		return &reused, nil
	}
	now := s.now().UTC()
	approval := RecordApproval{
		ID:                 newID("approval"),
		TenantID:           p.TenantID,
		DatasetID:          datasetID,
		RecordID:           recordID,
		Status:             recordApprovalStatusPending,
		Kind:               kind,
		Priority:           priority,
		Summary:            strings.TrimSpace(in.Summary),
		Request:            cloneJSONMap(in.Request),
		WorkflowSkillID:    strings.TrimSpace(in.WorkflowSkillID),
		WorkflowVersion:    strings.TrimSpace(in.WorkflowVersion),
		WorkflowInstanceID: strings.TrimSpace(in.WorkflowInstanceID),
		WorkflowNodeID:     strings.TrimSpace(in.WorkflowNodeID),
		WorkflowDecisionID: strings.TrimSpace(in.WorkflowDecisionID),
		BusinessStatus:     strings.TrimSpace(in.BusinessStatus),
		ResultStatus:       strings.TrimSpace(in.ResultStatus),
		ResultPayload:      cloneJSONMap(in.ResultPayload),
		Outputs:            cloneJSONValue(in.Outputs),
		Artifacts:          cloneJSONValue(in.Artifacts),
		AssignedTo:         strings.TrimSpace(in.AssignedTo),
		CreatedBy:          p.UserID,
		CreatedAt:          now,
		DueAt:              dueAt,
		UpdatedAt:          now,
	}
	if approval.Summary == "" {
		approval.Summary = "Approval requested for " + recordID
	}
	out, err := s.store.CreateRecordApproval(ctx, approval)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, p, "approval.create", datasetID, "record", recordID, "Created approval "+out.ID, map[string]any{"approval_id": out.ID, "kind": out.Kind, "priority": out.Priority, "assigned_to": out.AssignedTo, "due_at": formatOptionalPlanTime(out.DueAt)})
	return out, nil
}

func (s *Service) ListRecordApprovals(ctx context.Context, p Principal, in QueryRecordApprovalsInput) ([]RecordApproval, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	in.DatasetID = strings.TrimSpace(in.DatasetID)
	in.RecordID = strings.TrimSpace(in.RecordID)
	in.Status = strings.TrimSpace(in.Status)
	if in.Status != "" && !validRecordApprovalStatus(in.Status) {
		return nil, fmt.Errorf("%w: invalid approval status", ErrInvalidInput)
	}
	in.Kind = strings.TrimSpace(in.Kind)
	in.WorkflowSkillID = strings.TrimSpace(in.WorkflowSkillID)
	in.WorkflowInstanceID = strings.TrimSpace(in.WorkflowInstanceID)
	in.BusinessStatus = strings.TrimSpace(in.BusinessStatus)
	in.ResultStatus = strings.TrimSpace(in.ResultStatus)
	in.AssignedTo = strings.TrimSpace(in.AssignedTo)
	return s.store.ListRecordApprovals(ctx, p.TenantID, in)
}

func validRecordApprovalStatus(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case recordApprovalStatusPending, recordApprovalStatusApproved, recordApprovalStatusRejected:
		return true
	default:
		return false
	}
}

func recordApprovalMetadata(approval RecordApproval) map[string]any {
	return map[string]any{
		"kind":                 approval.Kind,
		"priority":             approval.Priority,
		"assigned_to":          approval.AssignedTo,
		"due_at":               formatOptionalPlanTime(approval.DueAt),
		"workflow_skill_id":    approval.WorkflowSkillID,
		"workflow_version":     approval.WorkflowVersion,
		"workflow_instance_id": approval.WorkflowInstanceID,
		"workflow_node_id":     approval.WorkflowNodeID,
		"workflow_decision_id": approval.WorkflowDecisionID,
		"business_status":      approval.BusinessStatus,
		"result_status":        approval.ResultStatus,
		"request":              cloneJSONMap(approval.Request),
		"result_payload":       cloneJSONMap(approval.ResultPayload),
		"outputs":              cloneJSONValue(approval.Outputs),
		"artifacts":            cloneJSONValue(approval.Artifacts),
		"decision":             approval.Decision,
		"reason":               approval.Reason,
		"created_by":           approval.CreatedBy,
		"reviewed_by":          approval.ReviewedBy,
	}
}

func (s *Service) GetRecordApproval(ctx context.Context, p Principal, approvalID string) (*RecordApproval, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.store.GetRecordApproval(ctx, p.TenantID, strings.TrimSpace(approvalID))
}

func (s *Service) ReviewRecordApproval(ctx context.Context, p Principal, approvalID string, in ReviewRecordApprovalInput) (*RecordApproval, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	approvalID = strings.TrimSpace(approvalID)
	current, err := s.store.GetRecordApproval(ctx, p.TenantID, approvalID)
	if err != nil {
		return nil, err
	}
	if current.Status != recordApprovalStatusPending {
		return nil, fmt.Errorf("%w: approval is not pending", ErrInvalidInput)
	}
	decision := strings.ToLower(strings.TrimSpace(in.Decision))
	status := ""
	switch decision {
	case "approve", "approved":
		decision = "approve"
		status = recordApprovalStatusApproved
	case "reject", "rejected":
		decision = "reject"
		status = recordApprovalStatusRejected
	default:
		return nil, fmt.Errorf("%w: decision must be approve or reject", ErrInvalidInput)
	}
	out, err := s.store.UpdateRecordApprovalStatus(ctx, p.TenantID, approvalID, status, decision, strings.TrimSpace(in.Reason), p.UserID, in.WorkflowNodeID, in.WorkflowDecisionID, in.BusinessStatus, in.ResultStatus, in.ResultPayload, in.Outputs, in.Artifacts, s.now().UTC())
	if err != nil {
		return nil, err
	}
	s.audit(ctx, p, "approval."+decision, out.DatasetID, "record", out.RecordID, "Reviewed approval "+approvalID, map[string]any{"approval_id": approvalID, "decision": decision, "reason": strings.TrimSpace(in.Reason)})
	return out, nil
}

func normalizeApprovalKind(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "general"
	}
	return value
}

func normalizeApprovalPriority(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "low", "medium", "high", "critical":
		return value, nil
	case "":
		return "medium", nil
	default:
		return "", fmt.Errorf("%w: priority must be low, medium, high, or critical", ErrInvalidInput)
	}
}

func parseOptionalBusinessTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return parsed.UTC(), nil
	}
	if parsed, err := time.Parse("2006-01-02", raw); err == nil {
		return parsed.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("%w: due_at must be RFC3339 or YYYY-MM-DD", ErrInvalidInput)
}
