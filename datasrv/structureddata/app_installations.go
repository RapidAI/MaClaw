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
	for _, key := range []string{"schema", "package_sha256", "package_bytes", "app_entry_version", "app_skill_id", "app_skill_version", "workflow_skill_ids", "workflow_skill_versions", "approval_binding_versions", "workflow_mapping_schema", "workflow_submit_node", "workflow_approval_node", "workflow_result_node", "workflow_attention_node", "workflow_contract_schema", "workflow_contract_skill_id", "workflow_contract_version", "workflow_contract_object_role", "workflow_contract_required_inputs", "workflow_contract_decision_outputs", "dependency_count", "has_missing_required_dependency", "has_blocking_dependency", "workspace_layout_entry", "workspace_layout_template", "workspace_layout_density", "workspace_layout_primary_region", "workspace_layout_output_region", "workspace_layout_region_count", "workspace_layout_region_ids", "workspace_layout_navigation", "workspace_layout_list_columns", "result_contract_schema", "result_contract_primary", "result_contract_types", "result_contract_delivery", "test_evidence_run_id", "test_evidence_verified_at", "test_evidence_definition_fingerprint", "test_evidence_test_protocol_fingerprint", "test_evidence_artifact_present", "test_evidence_artifact_name", "test_evidence_artifact_count", "test_evidence_output_count", "test_evidence_primary_result", "test_evidence_result_coverage_ok", "test_evidence_result_coverage_primary", "test_evidence_covered_types", "test_evidence_missing_types", "test_evidence_approval_instance_id", "test_evidence_approval_id", "test_evidence_record_id", "test_evidence_approval_status", "test_evidence_approval_view_verified", "test_evidence_dependency_verified_at", "test_evidence_dependency_count", "test_evidence_dependency_missing_required", "test_evidence_dependency_blocking", "test_evidence_workflow_contract_issue", "test_evidence_workflow_contract_issue_count", "test_evidence_governance_review_issue", "test_evidence_governance_review_issue_count", "governance_status", "governance_risk_level"} {
		if value, ok := app.Metadata[key]; ok {
			metadata[key] = value
		}
	}
	if versionSnapshot := appInstallationVersionSnapshotAuditMetadata(app.Metadata["version_snapshot"]); versionSnapshot != nil {
		for key, value := range versionSnapshot {
			metadata[key] = value
		}
	}
	if resultContract := appInstallationResultContract(app.Metadata["result_contract"]); resultContract != nil {
		if schema, _ := resultContract["schema"].(string); strings.TrimSpace(schema) != "" {
			metadata["result_contract_schema"] = strings.TrimSpace(schema)
		}
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
	if err := normalizeAppInstallationWorkspaceLayoutMetadata(out); err != nil {
		return nil, err
	}
	if err := normalizeAppInstallationResultContractMetadata(out); err != nil {
		return nil, err
	}
	if err := normalizeAppInstallationWorkflowContractMetadata(out, kind); err != nil {
		return nil, err
	}
	if err := normalizeAppInstallationVersionSnapshotMetadata(out); err != nil {
		return nil, err
	}
	if err := normalizeAppInstallationTestEvidenceMetadata(out); err != nil {
		return nil, err
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

func normalizeAppInstallationWorkspaceLayoutMetadata(out map[string]any) error {
	layout := appInstallationMap(out["workspace_layout"])
	if layout == nil {
		if governance := appInstallationMap(out["governance"]); governance != nil {
			layout = appInstallationMap(governance["workspace_layout"])
			if layout == nil {
				layout = appInstallationMap(governance["workspaceLayout"])
			}
		}
	}
	if layout == nil {
		if navigation := appInstallationStringList(out["workspace_layout_navigation"]); len(navigation) > 0 {
			out["workspace_layout_navigation"] = navigation
		}
		if columns := appInstallationStringList(out["workspace_layout_list_columns"]); len(columns) > 0 {
			out["workspace_layout_list_columns"] = columns
		}
		return nil
	}
	if schema := firstNonEmptyAppInstallationString(appInstallationString(layout, "schema"), "maclaw.app.ui.v1"); schema != "maclaw.app.ui.v1" {
		return fmt.Errorf("%w: metadata.workspace_layout.schema must be maclaw.app.ui.v1", ErrInvalidInput)
	}
	layout["schema"] = "maclaw.app.ui.v1"
	if entry := firstNonEmptyAppInstallationString(appInstallationString(layout, "entry"), appInstallationString(out, "workspace_layout_entry")); entry != "" {
		layout["entry"] = entry
		out["workspace_layout_entry"] = entry
	}
	if template := firstNonEmptyAppInstallationString(appInstallationString(layout, "template"), appInstallationString(out, "workspace_layout_template")); template != "" {
		layout["template"] = template
		out["workspace_layout_template"] = template
	}
	if density := firstNonEmptyAppInstallationString(appInstallationString(layout, "density"), appInstallationString(out, "workspace_layout_density")); density != "" {
		layout["density"] = density
		out["workspace_layout_density"] = density
	}
	if primaryRegion := firstNonEmptyAppInstallationString(appInstallationString(layout, "primaryRegion"), appInstallationString(layout, "primary_region"), appInstallationString(out, "workspace_layout_primary_region")); primaryRegion != "" {
		layout["primaryRegion"] = primaryRegion
		delete(layout, "primary_region")
		out["workspace_layout_primary_region"] = primaryRegion
	}
	if outputRegion := firstNonEmptyAppInstallationString(appInstallationString(layout, "outputRegion"), appInstallationString(layout, "output_region"), appInstallationString(out, "workspace_layout_output_region")); outputRegion != "" {
		layout["outputRegion"] = outputRegion
		delete(layout, "output_region")
		out["workspace_layout_output_region"] = outputRegion
	}
	if regions, ids := normalizeAppInstallationWorkspaceRegions(layout["regions"]); len(regions) > 0 {
		layout["regions"] = regions
		out["workspace_layout_region_count"] = len(regions)
		out["workspace_layout_region_ids"] = ids
	}
	if navigation := firstNonEmptyAppInstallationStringList(appInstallationStringList(layout["navigation"]), appInstallationStringList(out["workspace_layout_navigation"])); len(navigation) > 0 {
		layout["navigation"] = navigation
		out["workspace_layout_navigation"] = navigation
	}
	list := appInstallationMap(layout["list"])
	if list == nil {
		list = map[string]any{}
	}
	if columns := firstNonEmptyAppInstallationStringList(appInstallationStringList(list["columns"]), appInstallationStringList(out["workspace_layout_list_columns"])); len(columns) > 0 {
		list["columns"] = columns
		layout["list"] = list
		out["workspace_layout_list_columns"] = columns
	}
	out["workspace_layout"] = layout
	return nil
}

func normalizeAppInstallationWorkspaceRegions(value any) ([]map[string]any, []string) {
	items, ok := value.([]any)
	if !ok {
		if typed, ok := value.([]map[string]any); ok {
			items = make([]any, 0, len(typed))
			for _, item := range typed {
				items = append(items, item)
			}
		}
	}
	regions := make([]map[string]any, 0, len(items))
	ids := []string{}
	for _, item := range items {
		region := appInstallationMap(item)
		if region == nil {
			continue
		}
		id := firstNonEmptyAppInstallationString(appInstallationString(region, "id"), appInstallationString(region, "region_id"))
		role := appInstallationString(region, "role")
		placement := appInstallationString(region, "placement")
		if id == "" || role == "" || placement == "" {
			continue
		}
		normalized := cloneJSONMap(region)
		normalized["id"] = id
		normalized["role"] = role
		normalized["placement"] = placement
		delete(normalized, "region_id")
		regions = append(regions, normalized)
		ids = append(ids, id)
	}
	return regions, ids
}

func normalizeAppInstallationResultContractMetadata(out map[string]any) error {
	contract := appInstallationMap(out["result_contract"])
	if contract == nil {
		if governance := appInstallationMap(out["governance"]); governance != nil {
			contract = appInstallationMap(governance["result_contract"])
			if contract == nil {
				contract = appInstallationMap(governance["resultContract"])
			}
		}
	}
	if contract == nil {
		return nil
	}
	if schema := firstNonEmptyAppInstallationString(appInstallationString(contract, "schema"), "maclaw.app.result.v1"); schema != "maclaw.app.result.v1" {
		return fmt.Errorf("%w: metadata.result_contract.schema must be maclaw.app.result.v1", ErrInvalidInput)
	}
	contract["schema"] = "maclaw.app.result.v1"
	if primary := firstNonEmptyAppInstallationString(appInstallationString(contract, "primary"), appInstallationString(out, "result_contract_primary")); primary != "" {
		contract["primary"] = primary
		out["result_contract_primary"] = primary
	}
	if types := appInstallationStringList(contract["types"]); len(types) > 0 {
		contract["types"] = types
		out["result_contract_types"] = types
	}
	if delivery := firstNonEmptyAppInstallationString(appInstallationString(contract, "delivery"), appInstallationString(out, "result_contract_delivery")); delivery != "" {
		contract["delivery"] = delivery
		out["result_contract_delivery"] = delivery
	}
	out["result_contract"] = contract
	out["result_contract_schema"] = "maclaw.app.result.v1"
	return nil
}

func normalizeAppInstallationWorkflowContractMetadata(out map[string]any, kind string) error {
	contract := appInstallationMap(out["workflow_contract"])
	if contract == nil {
		contract = appInstallationMap(out["workflowContract"])
	}
	if contract == nil {
		if governance := appInstallationMap(out["governance"]); governance != nil {
			contract = appInstallationMap(governance["workflow_contract"])
			if contract == nil {
				contract = appInstallationMap(governance["workflowContract"])
			}
		}
	}
	if contract == nil {
		return nil
	}
	if kind != "" && kind != "enterprise_approval_app" {
		return fmt.Errorf("%w: metadata.workflow_contract is only valid for enterprise_approval_app", ErrInvalidInput)
	}
	if schema := firstNonEmptyAppInstallationString(appInstallationString(contract, "schema"), "maclaw.app.workflow_contract.v1"); schema != "maclaw.app.workflow_contract.v1" {
		return fmt.Errorf("%w: metadata.workflow_contract.schema must be maclaw.app.workflow_contract.v1", ErrInvalidInput)
	}
	contract["schema"] = "maclaw.app.workflow_contract.v1"
	workflowSkillID := firstNonEmptyAppInstallationString(appInstallationString(contract, "workflowSkillId"), appInstallationString(contract, "workflow_skill_id"), appInstallationString(contract, "workflowId"), appInstallationString(contract, "workflow_id"), appInstallationString(out, "workflow_contract_skill_id"))
	if workflowSkillID == "" {
		return fmt.Errorf("%w: metadata.workflow_contract.workflowSkillId is required", ErrInvalidInput)
	}
	contract["workflowSkillId"] = workflowSkillID
	out["workflow_contract_skill_id"] = workflowSkillID
	delete(contract, "workflow_skill_id")
	delete(contract, "workflowId")
	delete(contract, "workflow_id")
	if version := firstNonEmptyAppInstallationString(appInstallationString(contract, "workflowVersion"), appInstallationString(contract, "workflow_version"), appInstallationString(out, "workflow_contract_version")); version != "" {
		contract["workflowVersion"] = version
		out["workflow_contract_version"] = version
	}
	delete(contract, "workflow_version")
	objectRole := firstNonEmptyAppInstallationString(appInstallationString(contract, "objectRole"), appInstallationString(contract, "object_role"), appInstallationString(contract, "businessObjectRole"), appInstallationString(contract, "business_object_role"), appInstallationString(out, "workflow_contract_object_role"))
	if objectRole == "" {
		return fmt.Errorf("%w: metadata.workflow_contract.objectRole is required", ErrInvalidInput)
	}
	contract["objectRole"] = objectRole
	out["workflow_contract_object_role"] = objectRole
	delete(contract, "object_role")
	delete(contract, "businessObjectRole")
	delete(contract, "business_object_role")
	requiredInputs := firstNonEmptyAppInstallationStringList(appInstallationStringList(contract["requiredInputs"]), appInstallationStringList(contract["required_inputs"]), appInstallationStringList(out["workflow_contract_required_inputs"]))
	if len(requiredInputs) > 0 {
		contract["requiredInputs"] = requiredInputs
		out["workflow_contract_required_inputs"] = requiredInputs
	}
	delete(contract, "required_inputs")
	decisionOutputs := firstNonEmptyAppInstallationStringList(appInstallationStringList(contract["decisionOutputs"]), appInstallationStringList(contract["decision_outputs"]), appInstallationStringList(out["workflow_contract_decision_outputs"]))
	if len(decisionOutputs) > 0 {
		contract["decisionOutputs"] = decisionOutputs
		out["workflow_contract_decision_outputs"] = decisionOutputs
	}
	delete(contract, "decision_outputs")
	statusMapping := appInstallationMap(contract["statusMapping"])
	if statusMapping == nil {
		statusMapping = appInstallationMap(contract["status_mapping"])
	}
	if statusMapping != nil {
		if value := firstNonEmptyAppInstallationString(appInstallationString(statusMapping, "requiresInput"), appInstallationString(statusMapping, "requires_input")); value != "" {
			statusMapping["requiresInput"] = value
		}
		delete(statusMapping, "requires_input")
		contract["statusMapping"] = statusMapping
		delete(contract, "status_mapping")
	}
	out["workflow_contract"] = contract
	out["workflow_contract_schema"] = "maclaw.app.workflow_contract.v1"
	delete(out, "workflowContract")
	return nil
}
func normalizeAppInstallationVersionSnapshotMetadata(out map[string]any) error {
	snapshot := appInstallationMap(out["version_snapshot"])
	if snapshot == nil {
		snapshot = appInstallationMap(out["versionSnapshot"])
	}
	if snapshot == nil {
		return nil
	}
	normalized := map[string]any{}
	if version := firstNonEmptyAppInstallationString(appInstallationString(snapshot, "app_entry_version"), appInstallationString(snapshot, "appEntryVersion"), appInstallationString(out, "app_entry_version")); version != "" {
		normalized["app_entry_version"] = version
		out["app_entry_version"] = version
	}
	if appSkill := appInstallationMap(snapshot["app_skill"]); appSkill == nil {
		if appSkill = appInstallationMap(snapshot["appSkill"]); appSkill != nil {
			normalizeAppInstallationVersionSkill(normalized, "app_skill", appSkill)
		}
	} else {
		normalizeAppInstallationVersionSkill(normalized, "app_skill", appSkill)
	}
	if appSkill := appInstallationMap(normalized["app_skill"]); appSkill != nil {
		if id := appInstallationString(appSkill, "id"); id != "" {
			out["app_skill_id"] = id
		}
		if version := appInstallationString(appSkill, "version"); version != "" {
			out["app_skill_version"] = version
		}
	}
	if workflowSkills := normalizeAppInstallationVersionSkills(snapshot["workflow_skills"]); len(workflowSkills) > 0 {
		normalized["workflow_skills"] = workflowSkills
		out["workflow_skill_versions"] = appInstallationSkillVersionRefs(workflowSkills, "id", "version")
	}
	if bindings := normalizeAppInstallationApprovalBindingVersions(snapshot["approval_bindings"]); len(bindings) > 0 {
		normalized["approval_bindings"] = bindings
		out["approval_binding_versions"] = appInstallationApprovalBindingVersionRefs(bindings)
	}
	if len(normalized) == 0 {
		return nil
	}
	out["version_snapshot"] = normalized
	delete(out, "versionSnapshot")
	return nil
}

func normalizeAppInstallationVersionSkill(target map[string]any, key string, skill map[string]any) {
	out := map[string]any{}
	for _, field := range []string{"id", "version", "kind", "source"} {
		if value := appInstallationString(skill, field); value != "" {
			out[field] = value
		}
	}
	if len(out) > 0 {
		target[key] = out
	}
}

func normalizeAppInstallationVersionSkills(value any) []map[string]any {
	out := []map[string]any{}
	seen := map[string]struct{}{}
	for _, item := range appInstallationMapList(value) {
		skill := map[string]any{}
		for _, field := range []string{"id", "version", "kind", "source"} {
			if value := appInstallationString(item, field); value != "" {
				skill[field] = value
			}
		}
		id := appInstallationString(skill, "id")
		if id == "" {
			continue
		}
		version := appInstallationString(skill, "version")
		key := id + "@" + version
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, skill)
	}
	return out
}

func normalizeAppInstallationApprovalBindingVersions(value any) []map[string]any {
	out := []map[string]any{}
	seen := map[string]struct{}{}
	for _, item := range appInstallationMapList(value) {
		binding := map[string]any{}
		if event := appInstallationString(item, "event"); event != "" {
			binding["event"] = event
		}
		if objectRole := firstNonEmptyAppInstallationString(appInstallationString(item, "object_role"), appInstallationString(item, "objectRole")); objectRole != "" {
			binding["object_role"] = objectRole
		}
		workflowID := firstNonEmptyAppInstallationString(appInstallationString(item, "workflow_skill_id"), appInstallationString(item, "workflowSkillId"), appInstallationString(item, "workflow_id"), appInstallationString(item, "workflowId"))
		if workflowID != "" {
			binding["workflow_skill_id"] = workflowID
		}
		if version := firstNonEmptyAppInstallationString(appInstallationString(item, "workflow_version"), appInstallationString(item, "workflowVersion")); version != "" {
			binding["workflow_version"] = version
		}
		if len(binding) == 0 || workflowID == "" {
			continue
		}
		key := firstNonEmptyAppInstallationString(appInstallationString(binding, "event"), "-") + ":" + workflowID + "@" + appInstallationString(binding, "workflow_version")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, binding)
	}
	return out
}

func normalizeAppInstallationTestEvidenceMetadata(out map[string]any) error {
	evidence := appInstallationMap(out["test_evidence"])
	governance := appInstallationMap(out["governance"])
	if evidence == nil && governance != nil {
		evidence = appInstallationMap(governance["test_evidence"])
		if evidence == nil {
			evidence = appInstallationMap(governance["testEvidence"])
		}
	}
	if evidence == nil {
		return nil
	}
	normalized := map[string]any{}
	if schema := firstNonEmptyAppInstallationString(appInstallationString(evidence, "schema"), "maclaw.app.test_evidence.v1"); schema != "maclaw.app.test_evidence.v1" {
		return fmt.Errorf("%w: metadata.test_evidence.schema must be maclaw.app.test_evidence.v1", ErrInvalidInput)
	}
	normalized["schema"] = "maclaw.app.test_evidence.v1"
	for _, pair := range []struct{ camel, snake, summary string }{
		{"runId", "run_id", "test_evidence_run_id"},
		{"verifiedAt", "verified_at", "test_evidence_verified_at"},
		{"artifactName", "artifact_name", "test_evidence_artifact_name"},
		{"primaryResult", "primary_result", "test_evidence_primary_result"},
	} {
		if value := firstNonEmptyAppInstallationString(appInstallationString(evidence, pair.camel), appInstallationString(evidence, pair.snake), appInstallationString(out, pair.summary)); value != "" {
			normalized[pair.snake] = value
			out[pair.summary] = value
		}
	}
	if fingerprint := firstNonEmptyAppInstallationString(appInstallationString(evidence, "definitionFingerprint"), appInstallationString(evidence, "definition_fingerprint"), appInstallationString(evidence, "definitionHash"), appInstallationString(evidence, "definition_hash"), appInstallationString(out, "test_evidence_definition_fingerprint")); fingerprint != "" {
		normalized["definition_fingerprint"] = fingerprint
		out["test_evidence_definition_fingerprint"] = fingerprint
	}
	if protocolValue, ok := firstAppInstallationPresent(evidence["testProtocol"], evidence["test_protocol"], out["test_evidence_test_protocol"]); ok {
		if protocol := appInstallationMap(protocolValue); protocol != nil {
			if schema := firstNonEmptyAppInstallationString(appInstallationString(protocol, "schema"), "maclaw.app.test_protocol.v1"); schema == "maclaw.app.test_protocol.v1" {
				normalized["test_protocol"] = cloneJSONValue(protocol)
				out["test_evidence_test_protocol"] = cloneJSONValue(protocol)
			}
		}
	}
	if fingerprint := firstNonEmptyAppInstallationString(appInstallationString(evidence, "testProtocolFingerprint"), appInstallationString(evidence, "test_protocol_fingerprint"), appInstallationString(evidence, "testProtocolHash"), appInstallationString(evidence, "test_protocol_hash"), appInstallationString(out, "test_evidence_test_protocol_fingerprint")); fingerprint != "" {
		normalized["test_protocol_fingerprint"] = fingerprint
		out["test_evidence_test_protocol_fingerprint"] = fingerprint
	} else if protocol := appInstallationMap(normalized["test_protocol"]); protocol != nil {
		if fingerprint := firstNonEmptyAppInstallationString(appInstallationString(protocol, "fingerprint"), appInstallationString(protocol, "hash")); fingerprint != "" {
			normalized["test_protocol_fingerprint"] = fingerprint
			out["test_evidence_test_protocol_fingerprint"] = fingerprint
		}
	}
	if value, ok := firstAppInstallationBool(evidence["artifactPresent"], evidence["artifact_present"], out["test_evidence_artifact_present"]); ok {
		normalized["artifact_present"] = value
		out["test_evidence_artifact_present"] = value
	}
	for _, pair := range []struct{ camel, snake, summary string }{
		{"artifactCount", "artifact_count", "test_evidence_artifact_count"},
		{"outputCount", "output_count", "test_evidence_output_count"},
	} {
		if value, ok := firstAppInstallationNumber(evidence[pair.camel], evidence[pair.snake], out[pair.summary]); ok {
			normalized[pair.snake] = value
			out[pair.summary] = value
		}
	}
	if payload, ok := firstAppInstallationPresent(evidence["resultPayload"], evidence["result_payload"], out["test_evidence_result_payload"]); ok {
		normalized["result_payload"] = cloneJSONValue(payload)
		out["test_evidence_result_payload"] = cloneJSONValue(payload)
	}
	if outputs, ok := firstAppInstallationPresent(evidence["outputs"], evidence["output_blocks"], evidence["outputBlocks"], out["test_evidence_outputs"]); ok {
		normalized["outputs"] = cloneJSONValue(outputs)
		out["test_evidence_outputs"] = cloneJSONValue(outputs)
		if _, hasCount := out["test_evidence_output_count"]; !hasCount {
			if items, ok := outputs.([]any); ok {
				normalized["output_count"] = len(items)
				out["test_evidence_output_count"] = len(items)
			}
		}
	}
	if artifacts, ok := firstAppInstallationPresent(evidence["artifacts"], out["test_evidence_artifacts"]); ok {
		normalized["artifacts"] = cloneJSONValue(artifacts)
		out["test_evidence_artifacts"] = cloneJSONValue(artifacts)
		if _, hasCount := out["test_evidence_artifact_count"]; !hasCount {
			if items, ok := artifacts.([]any); ok {
				normalized["artifact_count"] = len(items)
				out["test_evidence_artifact_count"] = len(items)
			}
		}
	}
	if approvalValue, ok := firstAppInstallationPresent(evidence["approvalInstance"], evidence["approval_instance"], evidence["approval"], out["test_evidence_approval_instance"]); ok {
		if approval := appInstallationMap(approvalValue); approval != nil {
			normalized["approval_instance"] = cloneJSONValue(approval)
			out["test_evidence_approval_instance"] = cloneJSONValue(approval)
			approvalID := firstNonEmptyAppInstallationString(appInstallationString(approval, "approvalID", "approvalId", "approval_id"), appInstallationString(approval, "recordApprovalID", "record_approval_id"))
			instanceID := firstNonEmptyAppInstallationString(appInstallationString(approval, "instanceId", "instance_id"), appInstallationString(approval, "approvalInstanceId", "approval_instance_id"), appInstallationString(approval, "workflowInstanceId", "workflow_instance_id"), approvalID)
			if instanceID != "" {
				out["test_evidence_approval_instance_id"] = instanceID
			}
			if approvalID != "" {
				out["test_evidence_approval_id"] = approvalID
			}
			if recordID := appInstallationString(approval, "recordID", "record_id"); recordID != "" {
				out["test_evidence_record_id"] = recordID
			}
			if status := firstNonEmptyAppInstallationString(appInstallationString(approval, "status"), appInstallationString(approval, "approvalStatus", "approval_status"), appInstallationString(approval, "resultStatus", "result_status")); status != "" {
				out["test_evidence_approval_status"] = status
			}
			if verified, ok := firstAppInstallationBool(approval["approvalInstanceViewVerified"], approval["approval_instance_view_verified"], approval["approvalViewVerified"], approval["approval_view_verified"], approval["viewVerified"], approval["view_verified"]); ok {
				out["test_evidence_approval_view_verified"] = verified
			}
		}
	}
	if coverage := normalizeAppInstallationResultCoverage(evidence, out); coverage != nil {
		normalized["result_coverage"] = coverage
	}
	if verification := normalizeAppInstallationDependencyVerification(evidence, governance, out); verification != nil {
		normalized["dependency_verification"] = verification
	}
	out["test_evidence"] = normalized
	return nil
}

func normalizeAppInstallationResultCoverage(evidence, out map[string]any) map[string]any {
	coverage := appInstallationMap(evidence["resultCoverage"])
	if coverage == nil {
		coverage = appInstallationMap(evidence["result_coverage"])
	}
	if coverage == nil {
		coverage = appInstallationMap(out["test_evidence_result_coverage"])
	}
	if coverage == nil {
		return nil
	}
	normalized := map[string]any{}
	if value, ok := firstAppInstallationBool(coverage["ok"], out["test_evidence_result_coverage_ok"]); ok {
		normalized["ok"] = value
		out["test_evidence_result_coverage_ok"] = value
	}
	if primary := firstNonEmptyAppInstallationString(appInstallationString(coverage, "primary"), appInstallationString(out, "test_evidence_result_coverage_primary")); primary != "" {
		normalized["primary"] = primary
		out["test_evidence_result_coverage_primary"] = primary
	}
	if covered := firstNonEmptyAppInstallationStringList(appInstallationStringList(coverage["coveredTypes"]), appInstallationStringList(coverage["covered_types"]), appInstallationStringList(out["test_evidence_covered_types"])); len(covered) > 0 {
		normalized["covered_types"] = covered
		out["test_evidence_covered_types"] = covered
	}
	if missing := firstNonEmptyAppInstallationStringList(appInstallationStringList(coverage["missingTypes"]), appInstallationStringList(coverage["missing_types"]), appInstallationStringList(out["test_evidence_missing_types"])); len(missing) > 0 {
		normalized["missing_types"] = missing
		out["test_evidence_missing_types"] = missing
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func normalizeAppInstallationDependencyVerification(evidence, governance, out map[string]any) map[string]any {
	verification := appInstallationMap(evidence["dependencyVerification"])
	if verification == nil {
		verification = appInstallationMap(evidence["dependency_verification"])
	}
	if verification == nil && governance != nil {
		verification = appInstallationMap(governance["dependencyVerification"])
		if verification == nil {
			verification = appInstallationMap(governance["dependency_verification"])
		}
	}
	if verification == nil {
		verification = appInstallationMap(out["test_evidence_dependency_verification"])
	}
	if verification == nil {
		return nil
	}
	normalized := map[string]any{}
	if schema := firstNonEmptyAppInstallationString(appInstallationString(verification, "schema"), "maclaw.app.install_plan.v1"); schema != "maclaw.app.install_plan.v1" {
		return nil
	}
	normalized["schema"] = "maclaw.app.install_plan.v1"
	if verifiedAt := firstNonEmptyAppInstallationString(appInstallationString(verification, "verifiedAt"), appInstallationString(verification, "verified_at"), appInstallationString(out, "test_evidence_dependency_verified_at")); verifiedAt != "" {
		normalized["verified_at"] = verifiedAt
		out["test_evidence_dependency_verified_at"] = verifiedAt
	}
	for _, pair := range []struct{ camel, snake, summary string }{
		{"dependencyCount", "dependency_count", "test_evidence_dependency_count"},
		{"workflowContractIssueCount", "workflow_contract_issue_count", "test_evidence_workflow_contract_issue_count"},
		{"governanceReviewIssueCount", "governance_review_issue_count", "test_evidence_governance_review_issue_count"},
	} {
		if value, ok := firstAppInstallationNumber(verification[pair.camel], verification[pair.snake], out[pair.summary]); ok {
			normalized[pair.snake] = value
			out[pair.summary] = value
		}
	}
	for _, pair := range []struct{ camel, snake, summary string }{
		{"hasMissingRequired", "has_missing_required", "test_evidence_dependency_missing_required"},
		{"hasBlockingDependency", "has_blocking_dependency", "test_evidence_dependency_blocking"},
		{"hasWorkflowContractIssue", "has_workflow_contract_issue", "test_evidence_workflow_contract_issue"},
		{"hasGovernanceReviewIssue", "has_governance_review_issue", "test_evidence_governance_review_issue"},
	} {
		if value, ok := firstAppInstallationBool(verification[pair.camel], verification[pair.snake], out[pair.summary]); ok {
			normalized[pair.snake] = value
			out[pair.summary] = value
		}
	}
	if dependencies := appInstallationMapList(verification["dependencies"]); len(dependencies) > 0 {
		normalized["dependencies"] = dependencies
	}
	return normalized
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

func firstNonEmptyAppInstallationStringList(values ...[]string) []string {
	for _, value := range values {
		if len(value) > 0 {
			return value
		}
	}
	return nil
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

func firstAppInstallationBool(values ...any) (bool, bool) {
	for _, value := range values {
		if typed, ok := value.(bool); ok {
			return typed, true
		}
	}
	return false, false
}

func firstAppInstallationNumber(values ...any) (any, bool) {
	for _, value := range values {
		switch typed := value.(type) {
		case int:
			return typed, true
		case int64:
			return typed, true
		case float64:
			return typed, true
		}
	}
	return nil, false
}

func firstAppInstallationPresent(values ...any) (any, bool) {
	for _, value := range values {
		if value != nil {
			return value, true
		}
	}
	return nil, false
}

func appInstallationVersionSnapshotAuditMetadata(value any) map[string]any {
	snapshot := appInstallationMap(value)
	if snapshot == nil {
		return nil
	}
	out := map[string]any{}
	if version := appInstallationString(snapshot, "app_entry_version", "appEntryVersion"); version != "" {
		out["app_entry_version"] = version
	}
	if appSkill := appInstallationMap(snapshot["app_skill"]); appSkill == nil {
		if appSkill = appInstallationMap(snapshot["appSkill"]); appSkill != nil {
			if id := appInstallationString(appSkill, "id"); id != "" {
				out["app_skill_id"] = id
			}
			if version := appInstallationString(appSkill, "version"); version != "" {
				out["app_skill_version"] = version
			}
		}
	} else {
		if id := appInstallationString(appSkill, "id"); id != "" {
			out["app_skill_id"] = id
		}
		if version := appInstallationString(appSkill, "version"); version != "" {
			out["app_skill_version"] = version
		}
	}
	if refs := appInstallationSkillVersionRefs(snapshot["workflow_skills"], "id", "version"); len(refs) > 0 {
		out["workflow_skill_versions"] = refs
	}
	if refs := appInstallationApprovalBindingVersionRefs(snapshot["approval_bindings"]); len(refs) > 0 {
		out["approval_binding_versions"] = refs
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func appInstallationSkillVersionRefs(value any, idKey, versionKey string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, item := range appInstallationMapList(value) {
		id := appInstallationString(item, idKey)
		version := appInstallationString(item, versionKey)
		ref := id
		if version != "" {
			ref = id + "@" + version
		}
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func appInstallationApprovalBindingVersionRefs(value any) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, item := range appInstallationMapList(value) {
		workflowID := firstNonEmptyAppInstallationString(appInstallationString(item, "workflow_skill_id"), appInstallationString(item, "workflowSkillId"), appInstallationString(item, "workflow_id"), appInstallationString(item, "workflowId"))
		version := firstNonEmptyAppInstallationString(appInstallationString(item, "workflow_version"), appInstallationString(item, "workflowVersion"))
		event := appInstallationString(item, "event")
		ref := workflowID
		if version != "" {
			ref = workflowID + "@" + version
		}
		if event != "" && ref != "" {
			ref = event + ":" + ref
		}
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func appInstallationMapList(value any) []map[string]any {
	out := []map[string]any{}
	switch items := value.(type) {
	case []any:
		for _, item := range items {
			if mapped := appInstallationMap(item); mapped != nil {
				out = append(out, mapped)
			}
		}
	case []map[string]any:
		out = append(out, items...)
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
