package structureddata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

func (s *SQLiteStore) UpsertAppInstallation(ctx context.Context, app AppInstallation) (*AppInstallation, error) {
	metadataJSON, err := json.Marshal(app.Metadata)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	_, err = tx.ExecContext(ctx, `INSERT INTO app_installations(id, tenant_id, app_id, blueprint_id, name, version, kind, status, source, metadata_json, installed_by, installed_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, id) DO UPDATE SET
			app_id = excluded.app_id,
			blueprint_id = excluded.blueprint_id,
			name = excluded.name,
			version = excluded.version,
			kind = excluded.kind,
			status = excluded.status,
			source = excluded.source,
			metadata_json = excluded.metadata_json,
			installed_by = CASE WHEN app_installations.installed_by = '' THEN excluded.installed_by ELSE app_installations.installed_by END,
			installed_at = CASE WHEN app_installations.installed_at = '' THEN excluded.installed_at ELSE app_installations.installed_at END,
			updated_at = excluded.updated_at`,
		app.ID, app.TenantID, app.AppID, app.BlueprintID, app.Name, app.Version, app.Kind, app.Status, app.Source, string(metadataJSON), app.InstalledBy, app.InstalledAt, app.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM app_role_bindings WHERE tenant_id = ? AND app_installation_id = ?`, app.TenantID, app.ID); err != nil {
		return nil, err
	}
	for _, binding := range app.RoleBindings {
		if _, err := tx.ExecContext(ctx, `INSERT INTO app_role_bindings(tenant_id, app_installation_id, app_id, blueprint_id, object_role, domain, dataset_id, template_id, required) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			app.TenantID, app.ID, binding.AppID, binding.BlueprintID, binding.ObjectRole, binding.Domain, binding.DatasetID, binding.TemplateID, boolInt(binding.Required)); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return s.GetAppInstallation(ctx, app.TenantID, app.AppID)
}

func (s *SQLiteStore) ListAppInstallations(ctx context.Context, tenantID string, in QueryAppInstallationsInput) ([]AppInstallation, error) {
	query := `SELECT id, tenant_id, app_id, blueprint_id, name, version, kind, status, source, metadata_json, installed_by, installed_at, updated_at FROM app_installations WHERE tenant_id = ?`
	args := []any{tenantID}
	if strings.TrimSpace(in.AppID) != "" {
		query += ` AND app_id = ?`
		args = append(args, strings.TrimSpace(in.AppID))
	}
	if strings.TrimSpace(in.BlueprintID) != "" {
		query += ` AND blueprint_id = ?`
		args = append(args, strings.TrimSpace(in.BlueprintID))
	}
	if strings.TrimSpace(in.Kind) != "" {
		query += ` AND kind = ?`
		args = append(args, strings.ToLower(strings.TrimSpace(in.Kind)))
	}
	if strings.TrimSpace(in.Source) != "" {
		query += ` AND source = ?`
		args = append(args, strings.TrimSpace(in.Source))
	}
	if strings.TrimSpace(in.Status) != "" {
		query += ` AND status = ?`
		args = append(args, strings.ToLower(strings.TrimSpace(in.Status)))
	}
	before := strings.TrimSpace(in.Before)
	beforeID := strings.TrimSpace(in.BeforeID)
	if before != "" {
		if beforeID != "" {
			query += ` AND (updated_at < ? OR (updated_at = ? AND id < ?))`
			args = append(args, before, before, beforeID)
		} else {
			query += ` AND updated_at < ?`
			args = append(args, before)
		}
	}
	query += ` ORDER BY updated_at DESC, id DESC`
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	metadataFiltered := appInstallationHasMetadataFilters(in)
	roleBindingFiltered := appInstallationHasRoleBindingFilters(in)
	if !metadataFiltered {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	out := []AppInstallation{}
	candidates := []AppInstallation{}
	for rows.Next() {
		app, err := scanAppInstallation(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		if roleBindingFiltered {
			candidates = append(candidates, app)
			continue
		}
		if appInstallationMatchesMetadataFilters(app, in) {
			out = append(out, app)
			if metadataFiltered && len(out) >= limit {
				break
			}
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if roleBindingFiltered {
		for _, app := range candidates {
			bindings, err := s.listAppRoleBindings(ctx, tenantID, app.ID)
			if err != nil {
				return nil, err
			}
			app.RoleBindings = bindings
			if !appInstallationMatchesMetadataFilters(app, in) {
				continue
			}
			out = append(out, app)
			if metadataFiltered && len(out) >= limit {
				break
			}
		}
	}
	for i := range out {
		if len(out[i].RoleBindings) > 0 {
			continue
		}
		bindings, err := s.listAppRoleBindings(ctx, tenantID, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].RoleBindings = bindings
	}
	return out, nil
}

func (s *SQLiteStore) GetAppInstallation(ctx context.Context, tenantID, appID string) (*AppInstallation, error) {
	appID = strings.TrimSpace(appID)
	row := s.db.QueryRowContext(ctx, `SELECT id, tenant_id, app_id, blueprint_id, name, version, kind, status, source, metadata_json, installed_by, installed_at, updated_at FROM app_installations WHERE tenant_id = ? AND (app_id = ? OR id = ?)`, tenantID, appID, appID)
	app, err := scanAppInstallation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	bindings, err := s.listAppRoleBindings(ctx, tenantID, app.ID)
	if err != nil {
		return nil, err
	}
	app.RoleBindings = bindings
	return &app, nil
}

func (s *SQLiteStore) listAppRoleBindings(ctx context.Context, tenantID, installationID string) ([]RoleBinding, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT app_id, blueprint_id, object_role, domain, dataset_id, template_id, required FROM app_role_bindings WHERE tenant_id = ? AND app_installation_id = ? ORDER BY object_role`, tenantID, installationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RoleBinding{}
	for rows.Next() {
		binding, err := scanAppRoleBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, binding)
	}
	return out, rows.Err()
}

func appInstallationMatchesMetadataFilters(app AppInstallation, in QueryAppInstallationsInput) bool {
	if workflowSkillID := strings.TrimSpace(in.WorkflowSkillID); workflowSkillID != "" && !appInstallationHasWorkflowSkillID(app.Metadata, workflowSkillID) {
		return false
	}
	if workflowNode := strings.TrimSpace(in.WorkflowNode); workflowNode != "" && !appInstallationHasWorkflowNode(app.Metadata, workflowNode) {
		return false
	}
	if approvalStatus := strings.TrimSpace(in.ApprovalStatus); approvalStatus != "" && !appInstallationHasApprovalStatus(app.Metadata, approvalStatus) {
		return false
	}
	if approvalDecision := strings.TrimSpace(in.ApprovalDecision); approvalDecision != "" && !appInstallationHasApprovalDecision(app.Metadata, approvalDecision) {
		return false
	}
	if applicantID := strings.TrimSpace(in.ApplicantID); applicantID != "" && !appInstallationHasApprovalActor(app.Metadata, applicantID, "applicant") {
		return false
	}
	if approverID := strings.TrimSpace(in.ApproverID); approverID != "" && !appInstallationHasApprovalActor(app.Metadata, approverID, "approver") {
		return false
	}
	if approvalID := strings.TrimSpace(in.ApprovalID); approvalID != "" && !appInstallationHasIdentifier(app.Metadata, approvalID, []string{"approval_id", "approvalID", "approvalId", "record_approval_id", "recordApprovalID", "recordApprovalId"}) {
		return false
	}
	if workflowInstanceID := strings.TrimSpace(in.WorkflowInstanceID); workflowInstanceID != "" && !appInstallationHasIdentifier(app.Metadata, workflowInstanceID, []string{"workflow_instance_id", "workflowInstanceID", "workflowInstanceId", "approval_instance_id", "approvalInstanceID", "approvalInstanceId", "instance_id", "instanceID", "instanceId"}) {
		return false
	}
	if datasetID := strings.TrimSpace(in.DatasetID); datasetID != "" && !appInstallationHasIdentifier(app.Metadata, datasetID, []string{"dataset_id", "datasetID", "datasetId", "dataset"}) {
		if !appInstallationHasRoleBindingIdentifier(app.RoleBindings, datasetID, "dataset") {
			return false
		}
	}
	if objectRole := strings.TrimSpace(in.ObjectRole); objectRole != "" && !appInstallationHasIdentifier(app.Metadata, objectRole, []string{"object_role", "objectRole", "approval_object_role", "approvalObjectRole"}) {
		if !appInstallationHasRoleBindingIdentifier(app.RoleBindings, objectRole, "object_role") {
			return false
		}
	}
	if recordID := strings.TrimSpace(in.RecordID); recordID != "" && !appInstallationHasIdentifier(app.Metadata, recordID, []string{"record_id", "recordID", "recordId", "business_record_id", "businessRecordID", "businessRecordId"}) {
		return false
	}
	if resultType := strings.TrimSpace(in.ResultType); resultType != "" && !appInstallationHasResultType(app.Metadata, resultType) {
		return false
	}
	if definitionFingerprint := strings.TrimSpace(in.DefinitionFingerprint); definitionFingerprint != "" && !appInstallationHasIdentifier(app.Metadata, definitionFingerprint, []string{"definition_fingerprint", "definitionFingerprint", "definition_hash", "definitionHash", "test_evidence_definition_fingerprint", "app_definition_hash", "appDefinitionHash", "app_definition_fingerprint", "appDefinitionFingerprint"}) {
		return false
	}
	if in.HasBlockingDependency != nil && appInstallationMetadataBool(app.Metadata, "has_blocking_dependency", "test_evidence_dependency_blocking", "dependency_verification.has_blocking_dependency", "dependency_verification.hasBlockingDependency", "test_evidence.dependency_verification.has_blocking_dependency", "test_evidence.dependency_verification.hasBlockingDependency") != *in.HasBlockingDependency {
		return false
	}
	if in.HasMissingRequiredDependency != nil && appInstallationMetadataBool(app.Metadata, "has_missing_required_dependency", "has_missing_required", "test_evidence_dependency_missing_required", "dependency_verification.has_missing_required", "dependency_verification.hasMissingRequired", "test_evidence.dependency_verification.has_missing_required", "test_evidence.dependency_verification.hasMissingRequired") != *in.HasMissingRequiredDependency {
		return false
	}
	return true
}

func appInstallationHasMetadataFilters(in QueryAppInstallationsInput) bool {
	return strings.TrimSpace(in.WorkflowSkillID) != "" ||
		strings.TrimSpace(in.WorkflowNode) != "" ||
		strings.TrimSpace(in.ApprovalStatus) != "" ||
		strings.TrimSpace(in.ApprovalDecision) != "" ||
		strings.TrimSpace(in.ApplicantID) != "" ||
		strings.TrimSpace(in.ApproverID) != "" ||
		strings.TrimSpace(in.ApprovalID) != "" ||
		strings.TrimSpace(in.WorkflowInstanceID) != "" ||
		strings.TrimSpace(in.DatasetID) != "" ||
		strings.TrimSpace(in.ObjectRole) != "" ||
		strings.TrimSpace(in.RecordID) != "" ||
		strings.TrimSpace(in.ResultType) != "" ||
		strings.TrimSpace(in.DefinitionFingerprint) != "" ||
		in.HasBlockingDependency != nil ||
		in.HasMissingRequiredDependency != nil
}

func appInstallationHasRoleBindingFilters(in QueryAppInstallationsInput) bool {
	return strings.TrimSpace(in.DatasetID) != "" || strings.TrimSpace(in.ObjectRole) != ""
}

func appInstallationHasRoleBindingIdentifier(bindings []RoleBinding, expected, kind string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return true
	}
	for _, binding := range bindings {
		switch kind {
		case "dataset":
			if strings.TrimSpace(binding.DatasetID) == expected {
				return true
			}
		case "object_role":
			if strings.TrimSpace(binding.ObjectRole) == expected {
				return true
			}
		}
	}
	return false
}

func appInstallationHasIdentifier(metadata map[string]any, expected string, keys []string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return true
	}
	if appInstallationMapHasAnyIdentifier(metadata, expected, keys) {
		return true
	}
	for _, approval := range appInstallationApprovalInstances(metadata) {
		if appInstallationMapHasAnyIdentifier(approval, expected, keys) {
			return true
		}
	}
	for _, payload := range appInstallationResultPayloads(metadata) {
		if appInstallationMapHasAnyIdentifier(payload, expected, keys) {
			return true
		}
	}
	for _, evidence := range appInstallationTestEvidenceMaps(metadata) {
		if appInstallationMapHasAnyIdentifier(evidence, expected, keys) {
			return true
		}
	}
	return false
}

func appInstallationMapHasAnyIdentifier(values map[string]any, expected string, keys []string) bool {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) == expected {
			return true
		}
		for _, value := range appInstallationStringList(values[key]) {
			if value == expected {
				return true
			}
		}
	}
	return false
}

func appInstallationMetadataBool(metadata map[string]any, keys ...string) bool {
	values := make([]any, 0, len(keys))
	for _, key := range keys {
		cursor := metadata
		parts := strings.Split(key, ".")
		for index, part := range parts {
			if index == len(parts)-1 {
				values = append(values, cursor[part])
				break
			}
			cursor = appInstallationMap(cursor[part])
			if cursor == nil {
				break
			}
		}
	}
	value, _ := firstAppInstallationBool(values...)
	return value
}

func appInstallationHasApprovalStatus(metadata map[string]any, status string) bool {
	status = strings.TrimSpace(status)
	if status == "" {
		return true
	}
	for _, value := range []string{
		appInstallationString(metadata, "approval_status"),
		appInstallationString(metadata, "test_evidence_approval_status"),
		appInstallationString(metadata, "result_status"),
		appInstallationString(metadata, "business_status"),
	} {
		if strings.TrimSpace(value) == status {
			return true
		}
	}
	for _, approval := range appInstallationApprovalInstances(metadata) {
		for _, key := range []string{"status", "approvalStatus", "approval_status", "resultStatus", "result_status", "businessStatus", "business_status"} {
			if value, ok := approval[key].(string); ok && strings.TrimSpace(value) == status {
				return true
			}
		}
	}
	return false
}

func appInstallationHasApprovalDecision(metadata map[string]any, decision string) bool {
	decision = strings.TrimSpace(decision)
	if decision == "" {
		return true
	}
	for _, value := range []string{
		appInstallationString(metadata, "approval_decision"),
		appInstallationString(metadata, "decision"),
		appInstallationString(metadata, "test_evidence_approval_decision"),
	} {
		if strings.TrimSpace(value) == decision {
			return true
		}
	}
	for _, approval := range appInstallationApprovalInstances(metadata) {
		for _, key := range []string{"decision", "approvalDecision", "approval_decision", "result", "approvalResult", "approval_result"} {
			if value, ok := approval[key].(string); ok && strings.TrimSpace(value) == decision {
				return true
			}
		}
	}
	for _, result := range appInstallationResultPayloads(metadata) {
		for _, key := range []string{"decision", "approval_decision", "approvalResult", "approval_result"} {
			if value, ok := result[key].(string); ok && strings.TrimSpace(value) == decision {
				return true
			}
		}
	}
	return false
}

func appInstallationHasApprovalActor(metadata map[string]any, actorID, role string) bool {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return true
	}
	var keys []string
	switch role {
	case "applicant":
		keys = []string{"applicant_id", "applicantId", "submitted_by", "submittedBy", "created_by", "createdBy", "requester_id", "requesterId"}
	case "approver":
		keys = []string{"approver_id", "approverId", "assigned_to", "assignedTo", "current_assignee", "currentAssignee", "reviewer_id", "reviewerId", "handled_by", "handledBy"}
	default:
		return false
	}
	for _, key := range keys {
		if value, ok := metadata[key].(string); ok && strings.TrimSpace(value) == actorID {
			return true
		}
		for _, value := range appInstallationStringList(metadata[key]) {
			if value == actorID {
				return true
			}
		}
	}
	for _, approval := range appInstallationApprovalInstances(metadata) {
		for _, key := range keys {
			if value, ok := approval[key].(string); ok && strings.TrimSpace(value) == actorID {
				return true
			}
			for _, value := range appInstallationStringList(approval[key]) {
				if value == actorID {
					return true
				}
			}
		}
	}
	return false
}

func appInstallationHasResultType(metadata map[string]any, resultType string) bool {
	resultType = strings.TrimSpace(resultType)
	if resultType == "" {
		return true
	}
	for _, value := range []string{
		appInstallationString(metadata, "result_type"),
		appInstallationString(metadata, "output_type"),
		appInstallationString(metadata, "result_contract_primary"),
		appInstallationString(metadata, "test_evidence_primary_result"),
		appInstallationString(metadata, "test_evidence_result_coverage_primary"),
	} {
		if strings.TrimSpace(value) == resultType {
			return true
		}
	}
	for _, value := range appInstallationStringList(metadata["result_contract_types"]) {
		if value == resultType {
			return true
		}
	}
	for _, value := range appInstallationStringList(metadata["test_evidence_covered_types"]) {
		if value == resultType {
			return true
		}
	}
	if contract := appInstallationMap(metadata["result_contract"]); contract != nil {
		if appInstallationString(contract, "primary") == resultType {
			return true
		}
		for _, value := range appInstallationStringList(contract["types"]) {
			if value == resultType {
				return true
			}
		}
	}
	for _, evidence := range appInstallationTestEvidenceMaps(metadata) {
		if appInstallationString(evidence, "primary_result") == resultType || appInstallationString(evidence, "primaryResult") == resultType {
			return true
		}
		if coverage := appInstallationMap(evidence["result_coverage"]); coverage != nil {
			if appInstallationString(coverage, "primary") == resultType {
				return true
			}
			for _, value := range appInstallationStringList(coverage["covered_types"]) {
				if value == resultType {
					return true
				}
			}
		}
		for _, key := range []string{"outputs", "artifacts"} {
			for _, item := range appInstallationMapList(evidence[key]) {
				for _, itemKey := range []string{"kind", "type", "result_type", "resultType"} {
					if appInstallationString(item, itemKey) == resultType {
						return true
					}
				}
			}
		}
	}
	return false
}

func appInstallationApprovalInstances(metadata map[string]any) []map[string]any {
	out := []map[string]any{}
	for _, key := range []string{"approval_instance", "approvalInstance", "test_evidence_approval_instance"} {
		if value := appInstallationMap(metadata[key]); value != nil {
			out = append(out, value)
		}
	}
	for _, evidence := range appInstallationTestEvidenceMaps(metadata) {
		if value := appInstallationMap(evidence["approval_instance"]); value != nil {
			out = append(out, value)
		}
		if value := appInstallationMap(evidence["approvalInstance"]); value != nil {
			out = append(out, value)
		}
	}
	return out
}

func appInstallationResultPayloads(metadata map[string]any) []map[string]any {
	out := []map[string]any{}
	for _, key := range []string{"result_payload", "resultPayload", "test_evidence_result_payload"} {
		if value := appInstallationMap(metadata[key]); value != nil {
			out = append(out, value)
		}
	}
	for _, evidence := range appInstallationTestEvidenceMaps(metadata) {
		if value := appInstallationMap(evidence["result_payload"]); value != nil {
			out = append(out, value)
		}
		if value := appInstallationMap(evidence["resultPayload"]); value != nil {
			out = append(out, value)
		}
	}
	return out
}

func appInstallationTestEvidenceMaps(metadata map[string]any) []map[string]any {
	out := []map[string]any{}
	for _, key := range []string{"test_evidence", "testEvidence"} {
		if value := appInstallationMap(metadata[key]); value != nil {
			out = append(out, value)
		}
	}
	return out
}

func appInstallationHasWorkflowSkillID(metadata map[string]any, workflowSkillID string) bool {
	workflowSkillID = strings.TrimSpace(workflowSkillID)
	if workflowSkillID == "" {
		return true
	}
	if value, ok := metadata["workflow_skill_id"].(string); ok && strings.TrimSpace(value) == workflowSkillID {
		return true
	}
	for _, value := range appInstallationStringList(metadata["workflow_skill_ids"]) {
		if value == workflowSkillID {
			return true
		}
	}
	if evidence := appInstallationMap(metadata["test_evidence"]); evidence != nil {
		if appInstallationApprovalInstanceHasWorkflowSkillID(appInstallationMap(evidence["approval_instance"]), workflowSkillID) {
			return true
		}
	}
	if appInstallationApprovalInstanceHasWorkflowSkillID(appInstallationMap(metadata["test_evidence_approval_instance"]), workflowSkillID) {
		return true
	}
	return false
}

func appInstallationHasWorkflowNode(metadata map[string]any, workflowNode string) bool {
	workflowNode = strings.TrimSpace(workflowNode)
	if workflowNode == "" {
		return true
	}
	for _, key := range []string{"workflow_node", "workflow_submit_node", "workflow_approval_node", "workflow_result_node", "workflow_attention_node"} {
		if value, ok := metadata[key].(string); ok && strings.TrimSpace(value) == workflowNode {
			return true
		}
	}
	workflow := appInstallationMap(metadata["workflow_mapping"])
	if workflow != nil {
		for _, key := range []string{"submitNode", "approvalNode", "resultNode", "attentionNode", "submit_node", "approval_node", "result_node", "attention_node"} {
			if value, ok := workflow[key].(string); ok && strings.TrimSpace(value) == workflowNode {
				return true
			}
		}
	}
	if evidence := appInstallationMap(metadata["test_evidence"]); evidence != nil {
		if appInstallationApprovalInstanceHasWorkflowNode(appInstallationMap(evidence["approval_instance"]), workflowNode) {
			return true
		}
	}
	if appInstallationApprovalInstanceHasWorkflowNode(appInstallationMap(metadata["test_evidence_approval_instance"]), workflowNode) {
		return true
	}
	return false
}

func appInstallationApprovalInstanceHasWorkflowSkillID(approval map[string]any, workflowSkillID string) bool {
	if approval == nil {
		return false
	}
	for _, key := range []string{"workflowSkillId", "workflowSkillID", "workflow_skill_id", "approvalWorkflowID", "approvalWorkflowId", "approval_workflow_id"} {
		if value, ok := approval[key].(string); ok && strings.TrimSpace(value) == workflowSkillID {
			return true
		}
	}
	return false
}

func appInstallationApprovalInstanceHasWorkflowNode(approval map[string]any, workflowNode string) bool {
	if approval == nil {
		return false
	}
	for _, key := range []string{"currentNode", "current_node", "node", "workflowNode", "workflow_node"} {
		if value, ok := approval[key].(string); ok && strings.TrimSpace(value) == workflowNode {
			return true
		}
	}
	for _, key := range []string{"currentNodeIDs", "current_node_ids", "workflowNodeIDs", "workflow_node_ids", "workflowNodes", "workflow_nodes"} {
		for _, value := range appInstallationStringList(approval[key]) {
			if value == workflowNode {
				return true
			}
		}
	}
	return false
}

func scanAppInstallation(scanner interface{ Scan(dest ...any) error }) (AppInstallation, error) {
	var app AppInstallation
	var metadataJSON string
	if err := scanner.Scan(&app.ID, &app.TenantID, &app.AppID, &app.BlueprintID, &app.Name, &app.Version, &app.Kind, &app.Status, &app.Source, &metadataJSON, &app.InstalledBy, &app.InstalledAt, &app.UpdatedAt); err != nil {
		return AppInstallation{}, err
	}
	_ = json.Unmarshal([]byte(metadataJSON), &app.Metadata)
	if app.Metadata == nil {
		app.Metadata = map[string]any{}
	}
	return app, nil
}

func scanAppRoleBinding(scanner interface{ Scan(dest ...any) error }) (RoleBinding, error) {
	var binding RoleBinding
	var required int
	if err := scanner.Scan(&binding.AppID, &binding.BlueprintID, &binding.ObjectRole, &binding.Domain, &binding.DatasetID, &binding.TemplateID, &required); err != nil {
		return RoleBinding{}, err
	}
	binding.Required = intBool(required)
	return binding, nil
}
