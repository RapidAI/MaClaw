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
	metadataFiltered := strings.TrimSpace(in.WorkflowSkillID) != "" || strings.TrimSpace(in.WorkflowNode) != ""
	if !metadataFiltered {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	out := []AppInstallation{}
	for rows.Next() {
		app, err := scanAppInstallation(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		if !appInstallationMatchesMetadataFilters(app, in) {
			continue
		}
		out = append(out, app)
		if metadataFiltered && len(out) >= limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
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
	return true
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
