package structureddata

import (
	"context"
	"fmt"
	"strings"
)

const defaultAppInstallationStatus = "installed"

func (s *Service) UpsertAppInstallation(ctx context.Context, p Principal, appID string, in UpsertAppInstallationInput) (*AppInstallation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	requestedAppID := strings.TrimSpace(appID)
	if requestedAppID == "" {
		requestedAppID = strings.TrimSpace(in.AppID)
	}
	requestedAppID, err := normalizeAppInstallationToken(requestedAppID, "app_id")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.AppID) != "" && strings.TrimSpace(in.AppID) != requestedAppID {
		return nil, fmt.Errorf("%w: app_id must match path app id", ErrInvalidInput)
	}
	blueprintID := strings.TrimSpace(in.BlueprintID)
	installationID := strings.TrimSpace(in.ID)
	if installationID == "" {
		installationID = requestedAppID
	}
	installationID, err = normalizeAppInstallationToken(installationID, "id")
	if err != nil {
		return nil, err
	}
	if installationID != requestedAppID {
		return nil, fmt.Errorf("%w: id must match app_id", ErrInvalidInput)
	}
	roleBindings, err := normalizeAppInstallationRoleBindings(requestedAppID, blueprintID, in.RoleBindings)
	if err != nil {
		return nil, err
	}
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	metadata, err := normalizeAppInstallationMetadata(in.Metadata, kind)
	if err != nil {
		return nil, err
	}
	now := formatTime(s.now().UTC())
	app := AppInstallation{
		ID:           installationID,
		TenantID:     p.TenantID,
		AppID:        requestedAppID,
		BlueprintID:  blueprintID,
		Name:         strings.TrimSpace(in.Name),
		Version:      strings.TrimSpace(in.Version),
		Kind:         kind,
		Status:       normalizeAppInstallationStatus(in.Status),
		Source:       strings.TrimSpace(in.Source),
		RoleBindings: roleBindings,
		Metadata:     metadata,
		InstalledBy:  p.UserID,
		InstalledAt:  now,
		UpdatedAt:    now,
	}
	out, err := s.store.UpsertAppInstallation(ctx, app)
	if err == nil {
		s.audit(ctx, p, "app.installation_upsert", "", "app_installation", requestedAppID, "Upserted app installation "+requestedAppID, appInstallationAuditMetadata(app))
	}
	return out, err
}

func (s *Service) ListAppInstallations(ctx context.Context, p Principal, in QueryAppInstallationsInput) ([]AppInstallation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	in.AppID = strings.TrimSpace(in.AppID)
	in.BlueprintID = strings.TrimSpace(in.BlueprintID)
	in.Kind = strings.ToLower(strings.TrimSpace(in.Kind))
	in.Source = strings.TrimSpace(in.Source)
	in.WorkflowSkillID = strings.TrimSpace(in.WorkflowSkillID)
	in.WorkflowNode = strings.TrimSpace(in.WorkflowNode)
	in.Status = strings.ToLower(strings.TrimSpace(in.Status))
	in.Before = strings.TrimSpace(in.Before)
	in.BeforeID = strings.TrimSpace(in.BeforeID)
	return s.store.ListAppInstallations(ctx, p.TenantID, in)
}

func (s *Service) GetAppInstallation(ctx context.Context, p Principal, appID string) (*AppInstallation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.store.GetAppInstallation(ctx, p.TenantID, strings.TrimSpace(appID))
}
func appInstallationAuditMetadata(app AppInstallation) map[string]any {
	metadata := map[string]any{
		"app_id":             app.AppID,
		"blueprint_id":       app.BlueprintID,
		"role_binding_count": len(app.RoleBindings),
		"status":             app.Status,
	}
	if app.Kind != "" {
		metadata["kind"] = app.Kind
	}
	if app.Source != "" {
		metadata["source"] = app.Source
	}
	for _, key := range []string{"app_skill_id", "workflow_skill_ids", "workflow_mapping_schema", "workflow_submit_node", "workflow_approval_node", "workflow_result_node", "workflow_attention_node", "dependency_count", "has_missing_required_dependency", "has_blocking_dependency", "workspace_layout_entry", "workspace_layout_template", "workspace_layout_density", "result_contract_primary", "result_contract_types", "governance_status", "governance_risk_level"} {
		if value, ok := app.Metadata[key]; ok {
			metadata[key] = value
		}
	}
	if resultContract := appInstallationResultContract(app.Metadata["result_contract"]); resultContract != nil {
		if primary, _ := resultContract["primary"].(string); strings.TrimSpace(primary) != "" {
			metadata["result_contract_primary"] = strings.TrimSpace(primary)
		}
		if types := appInstallationStringList(resultContract["types"]); len(types) > 0 {
			metadata["result_contract_types"] = types
		}
	}
	if dependencyIDs := appInstallationDependencyIDs(app.Metadata["dependencies"]); len(dependencyIDs) > 0 {
		metadata["dependency_ids"] = dependencyIDs
	}
	return metadata
}

func normalizeAppInstallationMetadata(metadata map[string]any, kind string) (map[string]any, error) {
	out := cloneJSONMap(metadata)
	if out == nil {
		out = map[string]any{}
	}
	workflow := appInstallationMap(out["workflow_mapping"])
	if workflow == nil {
		return out, nil
	}
	if schema := firstNonEmptyAppInstallationString(appInstallationString(workflow, "schema"), "maclaw.app.workflow.v1"); schema != "maclaw.app.workflow.v1" {
		return nil, fmt.Errorf("%w: metadata.workflow_mapping.schema must be maclaw.app.workflow.v1", ErrInvalidInput)
	}
	if kind != "" && kind != "enterprise_approval_app" {
		return nil, fmt.Errorf("%w: metadata.workflow_mapping is only valid for enterprise_approval_app", ErrInvalidInput)
	}
	workflow["schema"] = "maclaw.app.workflow.v1"
	for _, pair := range []struct{ camel, snake, meta string }{
		{"submitNode", "submit_node", "workflow_submit_node"},
		{"approvalNode", "approval_node", "workflow_approval_node"},
		{"resultNode", "result_node", "workflow_result_node"},
		{"attentionNode", "attention_node", "workflow_attention_node"},
	} {
		value := firstNonEmptyAppInstallationString(appInstallationString(workflow, pair.camel), appInstallationString(workflow, pair.snake), appInstallationString(out, pair.meta))
		if value == "" && pair.camel != "attentionNode" {
			return nil, fmt.Errorf("%w: metadata.workflow_mapping.%s is required", ErrInvalidInput, pair.camel)
		}
		if value != "" {
			workflow[pair.camel] = value
			out[pair.meta] = value
		}
		delete(workflow, pair.snake)
	}
	statusMapping := appInstallationMap(workflow["statusMapping"])
	if statusMapping == nil {
		statusMapping = appInstallationMap(workflow["status_mapping"])
	}
	if statusMapping != nil {
		if value := firstNonEmptyAppInstallationString(appInstallationString(statusMapping, "requiresInput"), appInstallationString(statusMapping, "requires_input")); value != "" {
			statusMapping["requiresInput"] = value
		}
		delete(statusMapping, "requires_input")
		workflow["statusMapping"] = statusMapping
		delete(workflow, "status_mapping")
	}
	out["workflow_mapping"] = workflow
	out["workflow_mapping_schema"] = "maclaw.app.workflow.v1"
	return out, nil
}

func appInstallationMap(value any) map[string]any {
	if item, ok := value.(map[string]any); ok {
		return item
	}
	if item, ok := value.(map[string]interface{}); ok {
		return map[string]any(item)
	}
	return nil
}

func appInstallationString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok {
			if value = strings.TrimSpace(value); value != "" {
				return value
			}
		}
	}
	return ""
}

func firstNonEmptyAppInstallationString(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func appInstallationResultContract(value any) map[string]any {
	contract, _ := value.(map[string]any)
	return contract
}

func appInstallationStringList(value any) []string {
	out := []string{}
	add := func(text string) {
		text = strings.TrimSpace(text)
		if text != "" {
			out = append(out, text)
		}
	}
	switch items := value.(type) {
	case []string:
		for _, item := range items {
			add(item)
		}
	case []any:
		for _, item := range items {
			text, _ := item.(string)
			add(text)
		}
	}
	return out
}

func appInstallationDependencyIDs(value any) []string {
	out := []string{}
	seen := map[string]struct{}{}
	add := func(dep map[string]any) {
		id, _ := dep["id"].(string)
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	switch items := value.(type) {
	case []any:
		for _, item := range items {
			if dep, ok := item.(map[string]any); ok {
				add(dep)
			}
		}
	case []map[string]any:
		for _, dep := range items {
			add(dep)
		}
	}
	return out
}

func normalizeAppInstallationToken(value, name string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%w: %s is required", ErrInvalidInput, name)
	}
	if len(value) > 160 || strings.ContainsAny(value, " \t\r\n") {
		return "", fmt.Errorf("%w: %s must be a compact identifier", ErrInvalidInput, name)
	}
	return value, nil
}

func normalizeAppInstallationStatus(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return defaultAppInstallationStatus
	}
	return value
}

func normalizeAppInstallationRoleBindings(appID, blueprintID string, items []RoleBinding) ([]RoleBinding, error) {
	out := make([]RoleBinding, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		objectRole, err := normalizeIdentifier(item.ObjectRole, "object_role")
		if err != nil {
			return nil, err
		}
		datasetID := strings.ToLower(strings.TrimSpace(item.DatasetID))
		if datasetID == "" {
			return nil, fmt.Errorf("%w: dataset_id is required for role binding %s", ErrInvalidInput, objectRole)
		}
		if err := validateDatasetID(datasetID); err != nil {
			return nil, err
		}
		templateID := strings.ToLower(strings.TrimSpace(item.TemplateID))
		if templateID == "" {
			templateID = datasetID
		}
		if err := validateDatasetID(templateID); err != nil {
			return nil, err
		}
		domain := strings.ToLower(strings.TrimSpace(item.Domain))
		if domain == "" {
			domain = datasetDomain(datasetID)
		}
		if _, err := normalizeIdentifier(domain, "domain"); err != nil {
			return nil, err
		}
		if _, ok := seen[objectRole]; ok {
			return nil, fmt.Errorf("%w: duplicate role binding %s", ErrInvalidInput, objectRole)
		}
		seen[objectRole] = struct{}{}
		out = append(out, RoleBinding{AppID: appID, BlueprintID: blueprintID, ObjectRole: objectRole, Domain: domain, DatasetID: datasetID, TemplateID: templateID, Required: item.Required})
	}
	return out, nil
}
