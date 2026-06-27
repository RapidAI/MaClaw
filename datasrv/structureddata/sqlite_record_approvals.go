package structureddata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const recordApprovalSelectColumns = `id, tenant_id, dataset_id, record_id, app_id, blueprint_id, object_role, approval_workflow_id, trigger_event, submitted_by, current_assignee, current_assignee_type, from_status, to_status, status, kind, summary, request_json, workflow_skill_id, workflow_version, workflow_instance_id, workflow_node_id, workflow_node_ids_json, workflow_decision_id, detail_url, business_status, result_status, decision, reason, created_by, reviewed_by, created_at, reviewed_at, updated_at, assigned_to, due_at, priority, result_payload_json, outputs_json, artifacts_json`

func (s *SQLiteStore) CreateRecordApproval(ctx context.Context, approval RecordApproval) (*RecordApproval, error) {
	approval.Request = cloneJSONMap(approval.Request)
	approval.ResultPayload = cloneJSONMap(approval.ResultPayload)
	approval.Outputs = cloneJSONValue(approval.Outputs)
	approval.Artifacts = cloneJSONValue(approval.Artifacts)
	_, err := s.db.ExecContext(ctx, `INSERT INTO record_approvals(id, tenant_id, dataset_id, record_id, app_id, blueprint_id, object_role, approval_workflow_id, trigger_event, submitted_by, current_assignee, current_assignee_type, from_status, to_status, status, kind, summary, request_json, workflow_skill_id, workflow_version, workflow_instance_id, workflow_node_id, workflow_node_ids_json, workflow_decision_id, detail_url, business_status, result_status, decision, reason, created_by, reviewed_by, created_at, reviewed_at, updated_at, assigned_to, due_at, priority, result_payload_json, outputs_json, artifacts_json) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		approval.ID, approval.TenantID, approval.DatasetID, approval.RecordID, approval.AppID, approval.BlueprintID, approval.ObjectRole, approval.ApprovalWorkflowID, approval.TriggerEvent, approval.SubmittedBy, approval.CurrentAssignee, approval.CurrentAssigneeType, approval.FromStatus, approval.ToStatus, approval.Status, approval.Kind, approval.Summary, jsonString(approval.Request), approval.WorkflowSkillID, approval.WorkflowVersion, approval.WorkflowInstanceID, approval.WorkflowNodeID, jsonArrayString(approval.WorkflowNodeIDs), approval.WorkflowDecisionID, approval.DetailURL, approval.BusinessStatus, approval.ResultStatus, approval.Decision, approval.Reason, approval.CreatedBy, approval.ReviewedBy, formatTime(approval.CreatedAt), formatOptionalPlanTime(approval.ReviewedAt), formatTime(approval.UpdatedAt), approval.AssignedTo, formatOptionalPlanTime(approval.DueAt), approval.Priority, jsonObjectString(approval.ResultPayload), jsonArrayString(approval.Outputs), jsonArrayString(approval.Artifacts))
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
	if appID := strings.TrimSpace(in.AppID); appID != "" {
		clauses = append(clauses, "app_id = ?")
		args = append(args, appID)
	}
	if blueprintID := strings.TrimSpace(in.BlueprintID); blueprintID != "" {
		clauses = append(clauses, "blueprint_id = ?")
		args = append(args, blueprintID)
	}
	if objectRole := strings.TrimSpace(in.ObjectRole); objectRole != "" {
		clauses = append(clauses, "object_role = ?")
		args = append(args, objectRole)
	}
	if approvalWorkflowID := strings.TrimSpace(in.ApprovalWorkflowID); approvalWorkflowID != "" {
		clauses = append(clauses, "approval_workflow_id = ?")
		args = append(args, approvalWorkflowID)
	}
	if triggerEvent := strings.TrimSpace(in.TriggerEvent); triggerEvent != "" {
		clauses = append(clauses, "trigger_event = ?")
		args = append(args, triggerEvent)
	}
	if submittedBy := strings.TrimSpace(in.SubmittedBy); submittedBy != "" {
		clauses = append(clauses, "submitted_by = ?")
		args = append(args, submittedBy)
	}
	if currentAssignee := strings.TrimSpace(in.CurrentAssignee); currentAssignee != "" {
		clauses = append(clauses, "current_assignee = ?")
		args = append(args, currentAssignee)
	}
	if currentAssigneeType := strings.TrimSpace(in.CurrentAssigneeType); currentAssigneeType != "" {
		clauses = append(clauses, "current_assignee_type = ?")
		args = append(args, currentAssigneeType)
	}
	if fromStatus := strings.TrimSpace(in.FromStatus); fromStatus != "" {
		clauses = append(clauses, "from_status = ?")
		args = append(args, fromStatus)
	}
	if toStatus := strings.TrimSpace(in.ToStatus); toStatus != "" {
		clauses = append(clauses, "to_status = ?")
		args = append(args, toStatus)
	}
	if status := strings.TrimSpace(in.Status); status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, status)
	}
	if kind := strings.TrimSpace(in.Kind); kind != "" {
		clauses = append(clauses, "kind = ?")
		args = append(args, kind)
	}
	if workflowSkillID := strings.TrimSpace(in.WorkflowSkillID); workflowSkillID != "" {
		clauses = append(clauses, "workflow_skill_id = ?")
		args = append(args, workflowSkillID)
	}
	if workflowVersion := strings.TrimSpace(in.WorkflowVersion); workflowVersion != "" {
		clauses = append(clauses, "workflow_version = ?")
		args = append(args, workflowVersion)
	}
	if workflowInstanceID := strings.TrimSpace(in.WorkflowInstanceID); workflowInstanceID != "" {
		clauses = append(clauses, "workflow_instance_id = ?")
		args = append(args, workflowInstanceID)
	}
	if workflowNodeID := strings.TrimSpace(in.WorkflowNodeID); workflowNodeID != "" {
		clauses = append(clauses, "(workflow_node_id = ? OR EXISTS (SELECT 1 FROM json_each(workflow_node_ids_json) WHERE value = ?))")
		args = append(args, workflowNodeID, workflowNodeID)
	}
	if businessStatus := strings.TrimSpace(in.BusinessStatus); businessStatus != "" {
		clauses = append(clauses, "business_status = ?")
		args = append(args, businessStatus)
	}
	if resultStatus := strings.TrimSpace(in.ResultStatus); resultStatus != "" {
		clauses = append(clauses, "result_status = ?")
		args = append(args, resultStatus)
	}
	if assignedTo := strings.TrimSpace(in.AssignedTo); assignedTo != "" {
		clauses = append(clauses, "assigned_to = ?")
		args = append(args, assignedTo)
	}
	if createdBy := strings.TrimSpace(in.CreatedBy); createdBy != "" {
		clauses = append(clauses, "created_by = ?")
		args = append(args, createdBy)
	}
	if reviewedBy := strings.TrimSpace(in.ReviewedBy); reviewedBy != "" {
		clauses = append(clauses, "reviewed_by = ?")
		args = append(args, reviewedBy)
	}
	if lane := strings.ToLower(strings.TrimSpace(in.Lane)); lane != "" && lane != "all" {
		userID := strings.TrimSpace(in.UserID)
		switch lane {
		case "my_requests":
			clauses = append(clauses, "created_by = ?")
			args = append(args, userID)
		case "pending_my_approval":
			clauses = append(clauses, "status = ? AND (assigned_to = ? OR current_assignee = ?)")
			args = append(args, recordApprovalStatusPending, userID, userID)
		case "handled":
			clauses = append(clauses, "status IN (?, ?) AND reviewed_by = ?")
			args = append(args, recordApprovalStatusApproved, recordApprovalStatusRejected, userID)
		case "attention":
			clauses = append(clauses, "(business_status = ? OR result_status = ? OR kind = ?)")
			args = append(args, "attention", "attention", "attention")
		}
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
	rows, err := s.db.QueryContext(ctx, `SELECT `+recordApprovalSelectColumns+` FROM record_approvals WHERE `+strings.Join(clauses, " AND ")+` ORDER BY created_at DESC, id DESC LIMIT ?`, args...)
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
	row := s.db.QueryRowContext(ctx, `SELECT `+recordApprovalSelectColumns+` FROM record_approvals WHERE tenant_id = ? AND id = ?`, tenantID, strings.TrimSpace(approvalID))
	approval, err := scanRecordApproval(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	return &approval, nil
}

func (s *SQLiteStore) UpdateRecordApprovalStatus(ctx context.Context, tenantID, approvalID, status, decision, reason, reviewedBy, workflowInstanceID, workflowNodeID string, workflowNodeIDs []string, workflowVersion, workflowDecisionID, detailURL, businessStatus, resultStatus, currentAssignee, currentAssigneeType, fromStatus, toStatus string, resultPayload map[string]any, outputs []RecordApprovalOutput, artifacts []RecordApprovalArtifact, now time.Time) (*RecordApproval, error) {
	approval, err := s.GetRecordApproval(ctx, tenantID, approvalID)
	if err != nil {
		return nil, err
	}
	approval.Status = strings.TrimSpace(status)
	approval.Decision = strings.TrimSpace(decision)
	approval.Reason = strings.TrimSpace(reason)
	approval.ReviewedBy = strings.TrimSpace(reviewedBy)
	if workflowInstanceID = strings.TrimSpace(workflowInstanceID); workflowInstanceID != "" {
		approval.WorkflowInstanceID = workflowInstanceID
	}
	workflowNodeID, workflowNodeIDs = normalizeRecordApprovalWorkflowNodes(workflowNodeID, workflowNodeIDs)
	if workflowNodeID != "" {
		approval.WorkflowNodeID = workflowNodeID
	}
	if len(workflowNodeIDs) > 0 {
		approval.WorkflowNodeIDs = workflowNodeIDs
	}
	if workflowVersion = strings.TrimSpace(workflowVersion); workflowVersion != "" {
		approval.WorkflowVersion = workflowVersion
	}
	if workflowDecisionID = strings.TrimSpace(workflowDecisionID); workflowDecisionID != "" {
		approval.WorkflowDecisionID = workflowDecisionID
	}
	if detailURL = strings.TrimSpace(detailURL); detailURL != "" {
		approval.DetailURL = detailURL
	}
	if businessStatus = strings.TrimSpace(businessStatus); businessStatus != "" {
		approval.BusinessStatus = businessStatus
	}
	if resultStatus = strings.TrimSpace(resultStatus); resultStatus != "" {
		approval.ResultStatus = resultStatus
	}
	if currentAssignee = strings.TrimSpace(currentAssignee); currentAssignee != "" {
		approval.CurrentAssignee = currentAssignee
	}
	if currentAssigneeType = strings.TrimSpace(currentAssigneeType); currentAssigneeType != "" {
		approval.CurrentAssigneeType = currentAssigneeType
	}
	if fromStatus = strings.TrimSpace(fromStatus); fromStatus != "" {
		approval.FromStatus = fromStatus
	}
	if toStatus = strings.TrimSpace(toStatus); toStatus != "" {
		approval.ToStatus = toStatus
	}
	if resultPayload != nil {
		approval.ResultPayload = cloneJSONMap(resultPayload)
	}
	if outputs != nil {
		approval.Outputs = cloneJSONValue(outputs)
	}
	if artifacts != nil {
		approval.Artifacts = cloneJSONValue(artifacts)
	}
	approval.ReviewedAt = now
	approval.UpdatedAt = now
	res, err := s.db.ExecContext(ctx, `UPDATE record_approvals SET status = ?, decision = ?, reason = ?, reviewed_by = ?, reviewed_at = ?, updated_at = ?, workflow_instance_id = ?, workflow_node_id = ?, workflow_node_ids_json = ?, workflow_version = ?, workflow_decision_id = ?, detail_url = ?, business_status = ?, result_status = ?, current_assignee = ?, current_assignee_type = ?, from_status = ?, to_status = ?, result_payload_json = ?, outputs_json = ?, artifacts_json = ? WHERE tenant_id = ? AND id = ?`,
		approval.Status, approval.Decision, approval.Reason, approval.ReviewedBy, formatOptionalPlanTime(approval.ReviewedAt), formatTime(approval.UpdatedAt), approval.WorkflowInstanceID, approval.WorkflowNodeID, jsonArrayString(approval.WorkflowNodeIDs), approval.WorkflowVersion, approval.WorkflowDecisionID, approval.DetailURL, approval.BusinessStatus, approval.ResultStatus, approval.CurrentAssignee, approval.CurrentAssigneeType, approval.FromStatus, approval.ToStatus, jsonObjectString(approval.ResultPayload), jsonArrayString(approval.Outputs), jsonArrayString(approval.Artifacts), tenantID, approvalID)
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
	var requestJSON, workflowNodeIDsJSON, resultPayloadJSON, outputsJSON, artifactsJSON, createdAt, reviewedAt, updatedAt, dueAt string
	if err := scanner.Scan(&approval.ID, &approval.TenantID, &approval.DatasetID, &approval.RecordID, &approval.AppID, &approval.BlueprintID, &approval.ObjectRole, &approval.ApprovalWorkflowID, &approval.TriggerEvent, &approval.SubmittedBy, &approval.CurrentAssignee, &approval.CurrentAssigneeType, &approval.FromStatus, &approval.ToStatus, &approval.Status, &approval.Kind, &approval.Summary, &requestJSON, &approval.WorkflowSkillID, &approval.WorkflowVersion, &approval.WorkflowInstanceID, &approval.WorkflowNodeID, &workflowNodeIDsJSON, &approval.WorkflowDecisionID, &approval.DetailURL, &approval.BusinessStatus, &approval.ResultStatus, &approval.Decision, &approval.Reason, &approval.CreatedBy, &approval.ReviewedBy, &createdAt, &reviewedAt, &updatedAt, &approval.AssignedTo, &dueAt, &approval.Priority, &resultPayloadJSON, &outputsJSON, &artifactsJSON); err != nil {
		return RecordApproval{}, err
	}
	approval.Request = map[string]any{}
	_ = json.Unmarshal([]byte(defaultJSONString(requestJSON, "{}")), &approval.Request)
	approval.ResultPayload = map[string]any{}
	_ = json.Unmarshal([]byte(defaultJSONString(workflowNodeIDsJSON, "[]")), &approval.WorkflowNodeIDs)
	if len(approval.WorkflowNodeIDs) == 0 && strings.TrimSpace(approval.WorkflowNodeID) != "" {
		approval.WorkflowNodeIDs = []string{strings.TrimSpace(approval.WorkflowNodeID)}
	}
	_ = json.Unmarshal([]byte(defaultJSONString(resultPayloadJSON, "{}")), &approval.ResultPayload)
	_ = json.Unmarshal([]byte(defaultJSONString(outputsJSON, "[]")), &approval.Outputs)
	_ = json.Unmarshal([]byte(defaultJSONString(artifactsJSON, "[]")), &approval.Artifacts)
	approval.CreatedAt = parseTime(createdAt)
	approval.ReviewedAt = parseTime(reviewedAt)
	approval.UpdatedAt = parseTime(updatedAt)
	approval.DueAt = parseTime(dueAt)
	return approval, nil
}

func jsonObjectString(v map[string]any) string {
	if v == nil {
		return "{}"
	}
	return jsonString(v)
}

func jsonArrayString[T any](v []T) string {
	if v == nil {
		return "[]"
	}
	return jsonString(v)
}

func defaultJSONString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "null" {
		return fallback
	}
	return value
}
