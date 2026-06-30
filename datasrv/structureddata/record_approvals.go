package structureddata

import (
	"context"
	"fmt"
	"reflect"
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
	appID := strings.TrimSpace(in.AppID)
	blueprintID := strings.TrimSpace(in.BlueprintID)
	objectRole := strings.TrimSpace(in.ObjectRole)
	approvalWorkflowID := strings.TrimSpace(in.ApprovalWorkflowID)
	triggerEvent := strings.TrimSpace(in.TriggerEvent)
	submittedBy := strings.TrimSpace(in.SubmittedBy)
	if submittedBy == "" {
		submittedBy = p.UserID
	}
	currentAssignee := strings.TrimSpace(in.CurrentAssignee)
	if currentAssignee == "" {
		currentAssignee = strings.TrimSpace(in.AssignedTo)
	}
	workflowNodeID, workflowNodeIDs := normalizeRecordApprovalWorkflowNodes(in.WorkflowNodeID, in.WorkflowNodeIDs)
	if existing, err := s.store.ListRecordApprovals(ctx, p.TenantID, QueryRecordApprovalsInput{
		DatasetID:   datasetID,
		RecordID:    recordID,
		AppID:       appID,
		BlueprintID: blueprintID,
		ObjectRole:  objectRole,
		Status:      recordApprovalStatusPending,
		Kind:        kind,
		Limit:       1,
	}); err != nil {
		return nil, err
	} else if len(existing) > 0 {
		reused := existing[0]
		reused.Reused = true
		s.audit(ctx, p, "approval.reuse", datasetID, "record", recordID, "Reused pending approval "+reused.ID, recordApprovalAuditMetadata(reused, nil))
		return &reused, nil
	}
	now := s.now().UTC()
	approval := RecordApproval{
		ID:                  newID("approval"),
		TenantID:            p.TenantID,
		DatasetID:           datasetID,
		RecordID:            recordID,
		AppID:               appID,
		BlueprintID:         blueprintID,
		ObjectRole:          objectRole,
		ApprovalWorkflowID:  approvalWorkflowID,
		TriggerEvent:        triggerEvent,
		SubmittedBy:         submittedBy,
		CurrentAssignee:     currentAssignee,
		CurrentAssigneeType: strings.TrimSpace(in.CurrentAssigneeType),
		FromStatus:          strings.TrimSpace(in.FromStatus),
		ToStatus:            strings.TrimSpace(in.ToStatus),
		Status:              recordApprovalStatusPending,
		Kind:                kind,
		Priority:            priority,
		Summary:             strings.TrimSpace(in.Summary),
		Request:             cloneJSONMap(in.Request),
		WorkflowSkillID:     strings.TrimSpace(in.WorkflowSkillID),
		WorkflowVersion:     strings.TrimSpace(in.WorkflowVersion),
		WorkflowInstanceID:  strings.TrimSpace(in.WorkflowInstanceID),
		WorkflowNodeID:      workflowNodeID,
		WorkflowNodeIDs:     workflowNodeIDs,
		WorkflowDecisionID:  strings.TrimSpace(in.WorkflowDecisionID),
		DetailURL:           strings.TrimSpace(in.DetailURL),
		BusinessStatus:      strings.TrimSpace(in.BusinessStatus),
		ResultStatus:        strings.TrimSpace(in.ResultStatus),
		ResultPayload:       cloneJSONMap(in.ResultPayload),
		Outputs:             cloneJSONValue(in.Outputs),
		Artifacts:           cloneJSONValue(in.Artifacts),
		AssignedTo:          strings.TrimSpace(in.AssignedTo),
		CreatedBy:           p.UserID,
		CreatedAt:           now,
		DueAt:               dueAt,
		UpdatedAt:           now,
	}
	if approval.Summary == "" {
		approval.Summary = "Approval requested for " + recordID
	}
	out, err := s.store.CreateRecordApproval(ctx, approval)
	if err != nil {
		return nil, err
	}
	businessRecordUpdated, err := s.syncBusinessRecordFromApprovalCreate(ctx, p, *out)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, p, "approval.create", datasetID, "record", recordID, "Created approval "+out.ID, recordApprovalAuditMetadata(*out, map[string]any{"business_record_updated": businessRecordUpdated}))
	return out, nil
}

func (s *Service) ListRecordApprovals(ctx context.Context, p Principal, in QueryRecordApprovalsInput) ([]RecordApproval, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	in.DatasetID = strings.TrimSpace(in.DatasetID)
	in.RecordID = strings.TrimSpace(in.RecordID)
	in.AppID = strings.TrimSpace(in.AppID)
	in.BlueprintID = strings.TrimSpace(in.BlueprintID)
	in.ObjectRole = strings.TrimSpace(in.ObjectRole)
	in.ApprovalWorkflowID = strings.TrimSpace(in.ApprovalWorkflowID)
	in.TriggerEvent = strings.TrimSpace(in.TriggerEvent)
	in.SubmittedBy = strings.TrimSpace(in.SubmittedBy)
	in.CurrentAssignee = strings.TrimSpace(in.CurrentAssignee)
	in.CurrentAssigneeType = strings.TrimSpace(in.CurrentAssigneeType)
	in.FromStatus = strings.TrimSpace(in.FromStatus)
	in.ToStatus = strings.TrimSpace(in.ToStatus)
	in.Status = strings.TrimSpace(in.Status)
	if in.Status != "" && !validRecordApprovalStatus(in.Status) {
		return nil, fmt.Errorf("%w: invalid approval status", ErrInvalidInput)
	}
	in.Kind = strings.TrimSpace(in.Kind)
	in.WorkflowSkillID = strings.TrimSpace(in.WorkflowSkillID)
	in.WorkflowVersion = strings.TrimSpace(in.WorkflowVersion)
	in.WorkflowInstanceID = strings.TrimSpace(in.WorkflowInstanceID)
	in.WorkflowNodeID = strings.TrimSpace(in.WorkflowNodeID)
	in.BusinessStatus = strings.TrimSpace(in.BusinessStatus)
	in.ResultStatus = strings.TrimSpace(in.ResultStatus)
	in.AssignedTo = strings.TrimSpace(in.AssignedTo)
	in.CreatedBy = strings.TrimSpace(in.CreatedBy)
	in.ReviewedBy = strings.TrimSpace(in.ReviewedBy)
	in.Lane = strings.TrimSpace(in.Lane)
	if in.Lane != "" && !validRecordApprovalLane(in.Lane) {
		return nil, fmt.Errorf("%w: invalid approval lane", ErrInvalidInput)
	}
	in.UserID = p.UserID
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

func validRecordApprovalLane(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "my_requests", "pending_my_approval", "handled", "attention", "all":
		return true
	default:
		return false
	}
}

func normalizeRecordApprovalWorkflowNodes(workflowNodeID string, workflowNodeIDs []string) (string, []string) {
	primary := strings.TrimSpace(workflowNodeID)
	seen := map[string]struct{}{}
	nodes := make([]string, 0, len(workflowNodeIDs)+1)
	for _, node := range workflowNodeIDs {
		node = strings.TrimSpace(node)
		if node == "" {
			continue
		}
		key := strings.ToLower(node)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		nodes = append(nodes, node)
	}
	if primary != "" {
		key := strings.ToLower(primary)
		if _, ok := seen[key]; !ok {
			nodes = append([]string{primary}, nodes...)
		}
	}
	if primary == "" && len(nodes) > 0 {
		primary = nodes[0]
	}
	if len(nodes) == 0 && primary != "" {
		nodes = []string{primary}
	}
	return primary, nodes
}
func recordApprovalMetadata(approval RecordApproval) map[string]any {
	return map[string]any{
		"approval_id":           approval.ID,
		"app_id":                approval.AppID,
		"blueprint_id":          approval.BlueprintID,
		"object_role":           approval.ObjectRole,
		"approval_workflow_id":  approval.ApprovalWorkflowID,
		"trigger_event":         approval.TriggerEvent,
		"submitted_by":          approval.SubmittedBy,
		"current_assignee":      approval.CurrentAssignee,
		"current_assignee_type": approval.CurrentAssigneeType,
		"from_status":           approval.FromStatus,
		"to_status":             approval.ToStatus,
		"kind":                  approval.Kind,
		"priority":              approval.Priority,
		"assigned_to":           approval.AssignedTo,
		"due_at":                formatOptionalPlanTime(approval.DueAt),
		"workflow_skill_id":     approval.WorkflowSkillID,
		"workflow_version":      approval.WorkflowVersion,
		"workflow_instance_id":  approval.WorkflowInstanceID,
		"workflow_node_id":      approval.WorkflowNodeID,
		"workflow_node_ids":     append([]string(nil), approval.WorkflowNodeIDs...),
		"workflow_decision_id":  approval.WorkflowDecisionID,
		"detail_url":            approval.DetailURL,
		"business_status":       approval.BusinessStatus,
		"result_status":         approval.ResultStatus,
		"request":               cloneJSONMap(approval.Request),
		"result_payload":        cloneJSONMap(approval.ResultPayload),
		"outputs":               cloneJSONValue(approval.Outputs),
		"artifacts":             cloneJSONValue(approval.Artifacts),
		"decision":              approval.Decision,
		"reason":                approval.Reason,
		"created_by":            approval.CreatedBy,
		"reviewed_by":           approval.ReviewedBy,
	}
}

func recordApprovalAuditMetadata(approval RecordApproval, extra map[string]any) map[string]any {
	metadata := recordApprovalMetadata(approval)
	metadata["status"] = approval.Status
	metadata["output_count"] = len(approval.Outputs)
	metadata["artifact_count"] = len(approval.Artifacts)
	if summary := recordApprovalResultSummary(approval); summary != "" {
		metadata["result_summary"] = summary
	}
	if artifact := recordApprovalPrimaryArtifactName(approval); artifact != "" {
		metadata["primary_artifact"] = artifact
	}
	if len(approval.Outputs) == 0 {
		delete(metadata, "outputs")
	}
	if len(approval.Artifacts) == 0 {
		delete(metadata, "artifacts")
	}
	for key, value := range extra {
		metadata[key] = value
	}
	return metadata
}
func (s *Service) GetRecordApproval(ctx context.Context, p Principal, approvalID string) (*RecordApproval, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.store.GetRecordApproval(ctx, p.TenantID, strings.TrimSpace(approvalID))
}

func (s *Service) UpdateRecordApprovalProgress(ctx context.Context, p Principal, approvalID string, in UpdateRecordApprovalProgressInput) (*RecordApproval, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	approvalID = strings.TrimSpace(approvalID)
	current, err := s.store.GetRecordApproval(ctx, p.TenantID, approvalID)
	if err != nil {
		return nil, err
	}
	if current.Status != recordApprovalStatusPending {
		return nil, recordApprovalNotPendingError(*current)
	}
	fromStatus := strings.TrimSpace(in.FromStatus)
	if fromStatus == "" {
		fromStatus = current.BusinessStatus
	}
	toStatus := strings.TrimSpace(in.ToStatus)
	if toStatus == "" {
		toStatus = strings.TrimSpace(in.BusinessStatus)
	}
	if toStatus == "" {
		toStatus = strings.TrimSpace(current.ToStatus)
	}
	workflowNodeID, workflowNodeIDs := normalizeRecordApprovalWorkflowNodes(in.WorkflowNodeID, in.WorkflowNodeIDs)
	out, err := s.store.UpdateRecordApprovalProgress(ctx, p.TenantID, approvalID, in.WorkflowInstanceID, workflowNodeID, workflowNodeIDs, in.WorkflowVersion, in.WorkflowDecisionID, in.DetailURL, in.BusinessStatus, in.ResultStatus, in.CurrentAssignee, in.CurrentAssigneeType, fromStatus, toStatus, in.ResultPayload, in.Outputs, in.Artifacts, s.now().UTC())
	if err != nil {
		return nil, err
	}
	businessRecordUpdated, err := s.syncBusinessRecordFromApproval(ctx, p, *out, "approval.progress")
	if err != nil {
		return nil, err
	}
	s.audit(ctx, p, "approval.progress", out.DatasetID, "record", out.RecordID, "Updated approval progress "+approvalID, recordApprovalAuditMetadata(*out, map[string]any{"progress": strings.TrimSpace(in.Progress), "business_record_updated": businessRecordUpdated}))
	return out, nil
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
		return nil, recordApprovalNotPendingError(*current)
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
	fromStatus := strings.TrimSpace(in.FromStatus)
	if fromStatus == "" {
		fromStatus = current.BusinessStatus
	}
	toStatus := strings.TrimSpace(in.ToStatus)
	if toStatus == "" {
		toStatus = strings.TrimSpace(in.BusinessStatus)
	}
	if toStatus == "" {
		toStatus = status
	}
	workflowNodeID, workflowNodeIDs := normalizeRecordApprovalWorkflowNodes(in.WorkflowNodeID, in.WorkflowNodeIDs)
	out, err := s.store.UpdateRecordApprovalStatus(ctx, p.TenantID, approvalID, status, decision, strings.TrimSpace(in.Reason), p.UserID, in.WorkflowInstanceID, workflowNodeID, workflowNodeIDs, in.WorkflowVersion, in.WorkflowDecisionID, in.DetailURL, in.BusinessStatus, in.ResultStatus, in.CurrentAssignee, in.CurrentAssigneeType, fromStatus, toStatus, in.ResultPayload, in.Outputs, in.Artifacts, s.now().UTC())
	if err != nil {
		return nil, err
	}
	businessRecordUpdated, err := s.syncBusinessRecordFromApprovalReview(ctx, p, *out)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, p, "approval."+decision, out.DatasetID, "record", out.RecordID, "Reviewed approval "+approvalID, recordApprovalAuditMetadata(*out, map[string]any{"decision": decision, "reason": strings.TrimSpace(in.Reason), "business_record_updated": businessRecordUpdated}))
	return out, nil
}

func recordApprovalNotPendingError(approval RecordApproval) error {
	status := strings.TrimSpace(approval.Status)
	message := "Approval " + approval.ID + " cannot be reviewed because it is already " + status + "."
	if status == "" {
		message = "Approval " + approval.ID + " cannot be reviewed because it is not pending."
	}
	err := newBusinessError(ErrInvalidInput, "approval_not_pending", message)
	err.Target = firstNonEmptyStructuredDataString(approval.Summary, approval.RecordID, approval.ID)
	err.Required = "approval_status = pending"
	err.Actual = "approval_status = " + firstNonEmptyStructuredDataString(status, "unknown")
	err.NextActions = []BusinessErrorAction{
		{Label: "View approval detail", Action: "get_record_approval", Args: map[string]any{"approval_id": approval.ID}},
		{Label: "Refresh approval list", Action: "list_record_approvals", Args: map[string]any{"record_id": approval.RecordID, "status": status}},
	}
	err.Metadata = map[string]any{
		"approval_id":          approval.ID,
		"dataset_id":           approval.DatasetID,
		"record_id":            approval.RecordID,
		"workflow_instance_id": approval.WorkflowInstanceID,
		"status":               status,
	}
	return err
}

func (s *Service) syncBusinessRecordFromApprovalReview(ctx context.Context, p Principal, approval RecordApproval) (bool, error) {
	return s.syncBusinessRecordFromApproval(ctx, p, approval, "approval.review")
}

func (s *Service) syncBusinessRecordFromApprovalCreate(ctx context.Context, p Principal, approval RecordApproval) (bool, error) {
	return s.syncBusinessRecordFromApproval(ctx, p, approval, "approval.create")
}

func (s *Service) syncBusinessRecordFromApproval(ctx context.Context, p Principal, approval RecordApproval, revisionAction string) (bool, error) {
	record, err := s.store.GetRecord(ctx, p.TenantID, approval.DatasetID, approval.RecordID)
	if err != nil {
		return false, err
	}
	nextData := cloneJSONMap(record.Data)
	if nextData == nil {
		nextData = map[string]any{}
	}
	changed := false
	setString := func(key, value string) {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			return
		}
		if existing, ok := nextData[key].(string); ok && strings.TrimSpace(existing) == value {
			return
		}
		nextData[key] = value
		changed = true
	}
	setJSONValue := func(key string, value any) {
		key = strings.TrimSpace(key)
		if key == "" {
			return
		}
		value = cloneJSONValue(value)
		if reflect.DeepEqual(nextData[key], value) {
			return
		}
		nextData[key] = value
		changed = true
	}
	setString("status", approvalBusinessRecordStatus(approval))
	setString("business_status", approval.BusinessStatus)
	setString("result_status", approval.ResultStatus)
	setString("app_id", approval.AppID)
	setString("blueprint_id", approval.BlueprintID)
	setString("object_role", approval.ObjectRole)
	setString("approval_workflow_id", approval.ApprovalWorkflowID)
	setString("approval_trigger_event", approval.TriggerEvent)
	setString("approval_submitted_by", approval.SubmittedBy)
	setString("approval_current_assignee_type", approval.CurrentAssigneeType)
	setString("approval_from_status", approval.FromStatus)
	setString("approval_to_status", approval.ToStatus)
	setString("approval_status", approval.Status)
	setString("approval_lane", approvalBusinessRecordLane(approval))
	setString("approval_decision", approval.Decision)
	setString("approval_id", approval.ID)
	setString("approval_workflow_instance_id", approval.WorkflowInstanceID)
	setString("approval_workflow_skill_id", approval.WorkflowSkillID)
	setString("approval_workflow_version", approval.WorkflowVersion)
	setString("approval_workflow_node_id", approval.WorkflowNodeID)
	setString("approval_current_node", approval.WorkflowNodeID)
	if len(approval.WorkflowNodeIDs) > 0 {
		nextData["approval_workflow_node_ids"] = append([]string(nil), approval.WorkflowNodeIDs...)
		nextData["approval_current_nodes"] = append([]string(nil), approval.WorkflowNodeIDs...)
		changed = true
	}
	setString("approval_detail_url", approval.DetailURL)
	setString("approval_result_summary", recordApprovalResultSummary(approval))
	setString("approval_primary_artifact", recordApprovalPrimaryArtifactName(approval))
	if len(approval.ResultPayload) > 0 {
		setJSONValue("approval_result_payload", approval.ResultPayload)
	}
	if len(approval.Outputs) > 0 {
		setJSONValue("approval_outputs", approval.Outputs)
		nextData["approval_output_count"] = len(approval.Outputs)
		changed = true
	}
	if len(approval.Artifacts) > 0 {
		setJSONValue("approval_artifacts", approval.Artifacts)
		nextData["approval_artifact_count"] = len(approval.Artifacts)
		changed = true
	}
	if approval.CurrentAssignee != "" {
		setString("approval_current_assignee", approval.CurrentAssignee)
	} else {
		setString("approval_current_assignee", approval.AssignedTo)
	}
	if !changed {
		return false, nil
	}
	out, err := s.store.UpdateRecord(ctx, p.TenantID, approval.DatasetID, approval.RecordID, UpdateRecordInput{Data: nextData}, p.UserID, s.now().UTC())
	if err != nil {
		return false, err
	}
	if err := s.appendRecordRevision(ctx, p, revisionAction, *out); err != nil {
		return false, err
	}
	return true, nil
}

func approvalBusinessRecordLane(approval RecordApproval) string {
	switch strings.TrimSpace(approval.Status) {
	case recordApprovalStatusApproved, recordApprovalStatusRejected:
		return "handled"
	case recordApprovalStatusPending:
		return "pending_my_approval"
	default:
		return ""
	}
}
func approvalBusinessRecordStatus(approval RecordApproval) string {
	if businessRecord, ok := approval.ResultPayload["business_record"].(map[string]any); ok {
		if status, ok := businessRecord["status"].(string); ok && strings.TrimSpace(status) != "" {
			return strings.TrimSpace(status)
		}
	}
	if status, ok := approval.ResultPayload["business_status"].(string); ok && strings.TrimSpace(status) != "" {
		return strings.TrimSpace(status)
	}
	if strings.TrimSpace(approval.BusinessStatus) != "" {
		return strings.TrimSpace(approval.BusinessStatus)
	}
	return strings.TrimSpace(approval.Status)
}

func recordApprovalResultSummary(approval RecordApproval) string {
	for _, key := range []string{"summary", "text", "content", "message", "result"} {
		if value, ok := approval.ResultPayload[key].(string); ok && strings.TrimSpace(value) != "" {
			return truncateRecordApprovalSummary(value)
		}
	}
	businessRecordSummary := ""
	if businessRecord, ok := approval.ResultPayload["business_record"].(map[string]any); ok {
		for _, key := range []string{"summary", "title", "name", "status"} {
			if value, ok := businessRecord[key].(string); ok && strings.TrimSpace(value) != "" {
				businessRecordSummary = value
				break
			}
		}
	}
	if (approval.Status == recordApprovalStatusApproved || approval.Status == recordApprovalStatusRejected) && businessRecordSummary != "" {
		return truncateRecordApprovalSummary(businessRecordSummary)
	}
	for _, output := range approval.Outputs {
		for _, value := range []string{output.Text, output.Title, output.Status, output.Kind, output.Type} {
			if strings.TrimSpace(value) != "" {
				return truncateRecordApprovalSummary(value)
			}
		}
	}
	if businessRecordSummary != "" {
		return truncateRecordApprovalSummary(businessRecordSummary)
	}
	for _, value := range []string{approval.Reason, approval.Summary, approval.ResultStatus, approval.BusinessStatus, approval.Status} {
		if strings.TrimSpace(value) != "" {
			return truncateRecordApprovalSummary(value)
		}
	}
	return ""
}

func recordApprovalPrimaryArtifactName(approval RecordApproval) string {
	for _, artifact := range approval.Artifacts {
		for _, value := range []string{artifact.Name, artifact.URI, artifact.ID, artifact.Path, artifact.RemoteURL} {
			if strings.TrimSpace(value) != "" {
				return truncateRecordApprovalSummary(value)
			}
		}
	}
	for _, output := range approval.Outputs {
		if output.Artifact != nil {
			for _, value := range []string{output.Artifact.Name, output.Artifact.URI, output.Artifact.ID, output.Artifact.Path, output.Artifact.RemoteURL} {
				if strings.TrimSpace(value) != "" {
					return truncateRecordApprovalSummary(value)
				}
			}
		}
	}
	return ""
}

func truncateRecordApprovalSummary(value string) string {
	value = strings.TrimSpace(value)
	const maxRunes = 240
	if len([]rune(value)) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:maxRunes]))
}

func firstNonEmptyStructuredDataString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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
