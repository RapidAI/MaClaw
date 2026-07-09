package structureddata

import (
	"context"
	"sort"
	"strings"
)

func (s *Service) Capabilities(ctx context.Context, p Principal) (*DataCapabilities, error) {
	datasets, err := s.store.ListDatasets(ctx, p.TenantID)
	if err != nil {
		return nil, err
	}
	connectors, err := s.store.ListExternalConnectors(ctx, p.TenantID, QueryExternalConnectorsInput{Limit: 100})
	if err != nil {
		return nil, err
	}
	appInstallations, err := s.store.ListAppInstallations(ctx, p.TenantID, QueryAppInstallationsInput{Status: defaultAppInstallationStatus, Limit: 100})
	if err != nil {
		return nil, err
	}
	datasetCaps := make([]DatasetCapability, 0, len(datasets))
	domainSet := map[string]struct{}{}
	for _, dataset := range datasets {
		if strings.TrimSpace(dataset.Domain) != "" {
			domainSet[dataset.Domain] = struct{}{}
		}
		fields, err := s.store.ListFields(ctx, p.TenantID, dataset.ID)
		if err != nil {
			return nil, err
		}
		datasetCaps = append(datasetCaps, DatasetCapability{Dataset: dataset, Fields: fields})
	}
	for _, tmpl := range datasetTemplates {
		if strings.TrimSpace(tmpl.Domain) != "" {
			domainSet[tmpl.Domain] = struct{}{}
		}
	}
	for _, action := range businessActions {
		if strings.TrimSpace(action.Domain) != "" {
			domainSet[action.Domain] = struct{}{}
		}
	}
	for _, view := range businessViews {
		if strings.TrimSpace(view.Domain) != "" {
			domainSet[view.Domain] = struct{}{}
		}
	}
	for _, report := range reportDefinitions {
		if strings.TrimSpace(report.Domain) != "" {
			domainSet[report.Domain] = struct{}{}
		}
	}
	for _, dashboard := range dashboardDefinitions {
		if strings.TrimSpace(dashboard.Domain) != "" {
			domainSet[dashboard.Domain] = struct{}{}
		}
	}
	domains := make([]string, 0, len(domainSet))
	for domain := range domainSet {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	sort.Slice(datasetCaps, func(i, j int) bool { return datasetCaps[i].Dataset.ID < datasetCaps[j].Dataset.ID })
	templates := cloneDatasetTemplates(datasetTemplates)
	actions := cloneBusinessActions(businessActions)
	views := cloneBusinessViews(businessViews)
	dashboards := cloneDashboardDefinitions(dashboardDefinitions)
	reports := cloneReportDefinitions(reportDefinitions)
	businessObjects, err := s.businessObjectCatalogsLocked(ctx, p, datasetMapByID(datasets), QueryBusinessObjectsInput{})
	if err != nil {
		return nil, err
	}
	if p.Policy != nil {
		domains = filterCapabilityDomains(p, domains)
		datasetCaps = filterCapabilityDatasets(p, datasetCaps)
		templates = filterCapabilityTemplates(p, templates)
		actions = filterCapabilityActions(p, actions)
		views = filterCapabilityViews(p, views)
		dashboards = filterCapabilityDashboards(p, dashboards)
		reports = filterCapabilityReports(p, reports)
	}
	return &DataCapabilities{
		Service:          "MaClawDataSrv",
		Engine:           s.engine,
		TenantID:         p.TenantID,
		UserID:           p.UserID,
		Role:             p.Role,
		APIKeyID:         p.APIKeyID,
		Policy:           capabilityPolicy(),
		Access:           capabilityAccessSummary(p, domains, datasetCaps, businessObjects, actions, views, dashboards, reports),
		Domains:          domains,
		AgentPlaybooks:   agentBusinessPlaybooks(domains),
		Relationships:    relationshipsForCapabilities(datasetCaps),
		BusinessObjects:  businessObjects,
		AppInstallations: appInstallations,
		Datasets:         datasetCaps,
		Templates:        templates,
		BusinessActions:  actions,
		EventContracts:   cloneEventContracts(actions),
		Connectors:       connectors,
		BusinessViews:    views,
		Dashboards:       dashboards,
		Reports:          reports,
		QualityChecks:    cloneQualityChecks(qualityChecks),
		BusinessRules:    cloneBusinessRules(businessRules),
		ToolActions:      toolActionCapabilities(),
	}, nil
}

func filterCapabilityDomains(p Principal, items []string) []string {
	out := []string{}
	for _, item := range items {
		if principalCanUseDomain(p, item) {
			out = append(out, item)
		}
	}
	return out
}

func filterCapabilityDatasets(p Principal, items []DatasetCapability) []DatasetCapability {
	out := []DatasetCapability{}
	for _, item := range items {
		if principalCanUseDataset(p, item.Dataset.ID) {
			out = append(out, item)
		}
	}
	return out
}

func filterCapabilityTemplates(p Principal, items []DatasetTemplate) []DatasetTemplate {
	out := []DatasetTemplate{}
	for _, item := range items {
		if principalCanUseDataset(p, item.ID) && principalCanUseDomain(p, item.Domain) {
			out = append(out, item)
		}
	}
	return out
}

func filterCapabilityActions(p Principal, items []BusinessAction) []BusinessAction {
	out := []BusinessAction{}
	for _, item := range items {
		if principalCanUseAction(p, item.ID) && principalCanUseDomain(p, item.Domain) {
			out = append(out, item)
		}
	}
	return out
}

func filterCapabilityViews(p Principal, items []BusinessViewDefinition) []BusinessViewDefinition {
	out := []BusinessViewDefinition{}
	for _, item := range items {
		if principalCanUseView(p, item.ID) && principalCanUseDomain(p, item.Domain) {
			out = append(out, item)
		}
	}
	return out
}

func filterCapabilityDashboards(p Principal, items []DashboardDefinition) []DashboardDefinition {
	out := []DashboardDefinition{}
	for _, item := range items {
		if principalCanUseDashboard(p, item.ID) && principalCanUseDomain(p, item.Domain) {
			out = append(out, item)
		}
	}
	return out
}

func filterCapabilityReports(p Principal, items []ReportDefinition) []ReportDefinition {
	out := []ReportDefinition{}
	for _, item := range items {
		if principalCanUseReport(p, item.ID) && principalCanUseDomain(p, item.Domain) {
			out = append(out, item)
		}
	}
	return out
}

func capabilityAccessSummary(p Principal, domains []string, datasets []DatasetCapability, businessObjects []BusinessObjectCatalog, actions []BusinessAction, views []BusinessViewDefinition, dashboards []DashboardDefinition, reports []ReportDefinition) AccessCapabilitySummary {
	authenticatedBy := "root_token_or_headers"
	scopeMode := "role_based"
	rawAllowed := principalCanAdmin(p)
	sensitiveAllowed := principalCanReadSensitive(p)
	adminAllowed := principalCanAdmin(p)
	allowedDomains := []string(nil)
	allowedDatasets := []string(nil)
	allowedActions := []string(nil)
	allowedViews := []string(nil)
	allowedReports := []string(nil)
	allowedDashboards := []string(nil)
	if p.Policy != nil {
		authenticatedBy = "api_key"
		scopeMode = "api_key_scoped"
		rawAllowed = p.Policy.AllowRawData || len(p.Policy.AllowedDatasets) > 0
		sensitiveAllowed = principalCanReadSensitive(p)
		adminAllowed = principalCanAdmin(p)
		allowedDomains = append([]string(nil), p.Policy.AllowedDomains...)
		allowedDatasets = append([]string(nil), p.Policy.AllowedDatasets...)
		allowedActions = append([]string(nil), p.Policy.AllowedActions...)
		allowedViews = append([]string(nil), p.Policy.AllowedViews...)
		allowedReports = append([]string(nil), p.Policy.AllowedReports...)
		allowedDashboards = append([]string(nil), p.Policy.AllowedDashboards...)
		if !policyHasAnyScope(p.Policy) && !p.Policy.AllowRawData && !p.Policy.AllowAdmin {
			scopeMode = "api_key_business_default"
		}
	}
	guardrails := []string{
		"Prefer resolve_intent, business actions, business views, dashboards, reports, and aggregate APIs.",
		"Use execute_business_action with dry_run=true before uncertain business writes.",
		"Raw dataset, field, schema, backup restore, maintenance, and approval review actions require explicit admin scope.",
		"Never use raw SQL; DataSrv exposes controlled REST operations only.",
	}
	if !rawAllowed {
		guardrails = append(guardrails, "Raw dataset access is not allowed for this credential; use business capabilities instead.")
	}
	if !sensitiveAllowed {
		guardrails = append(guardrails, "Sensitive fields are masked or denied unless the key explicitly allows sensitive access.")
	}
	if !adminAllowed {
		guardrails = append(guardrails, "Administrative operations are not allowed for this credential.")
	}
	nextActions := []string{"resolve_intent", "list_domains", "get_inbox_summary"}
	if len(dashboards) > 0 {
		nextActions = append(nextActions, "run_dashboard")
	}
	if len(actions) > 0 {
		nextActions = append(nextActions, "execute_business_action")
	}
	if len(reports) > 0 {
		nextActions = append(nextActions, "run_report")
	}
	if adminAllowed {
		nextActions = append(nextActions, "export_governance_evidence_pack")
	}
	return AccessCapabilitySummary{
		AuthenticatedBy:        authenticatedBy,
		ScopeMode:              scopeMode,
		BusinessOperationFirst: true,
		RawDatasetAllowed:      rawAllowed,
		SensitiveAllowed:       sensitiveAllowed,
		AdminAllowed:           adminAllowed,
		AllowedDomains:         normalizeStringList(allowedDomains),
		AllowedDatasets:        normalizeStringList(allowedDatasets),
		AllowedActions:         normalizeStringList(allowedActions),
		AllowedViews:           normalizeStringList(allowedViews),
		AllowedReports:         normalizeStringList(allowedReports),
		AllowedDashboards:      normalizeStringList(allowedDashboards),
		VisibleCounts: map[string]int{
			"domains":          len(domains),
			"datasets":         len(datasets),
			"business_actions": len(actions),
			"business_views":   len(views),
			"business_objects": len(businessObjects),
			"dashboards":       len(dashboards),
			"reports":          len(reports),
		},
		Guardrails:             guardrails,
		RecommendedNextActions: uniqueCapabilityStrings(nextActions),
	}
}

func uniqueCapabilityStrings(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func capabilityPolicy() map[string]any {
	return map[string]any{
		"business_writes":                 "prefer execute_business_action; use dry_run=true before uncertain writes; use raw record writes only for low-level maintenance or explicitly requested imports; bulk_update_records and bulk_delete_records are data_admin-only and should dry-run before confirm",
		"external_connectors":             "register CRM, ERP, HRIS, finance, warehouse, or asset connectors with subscribed business actions; use paginated event contracts plus ingest_event dry-run/commit flow for all connector writes",
		"connector_event_ingest":          "prefer connector-scoped event ingest for external systems so DataSrv checks connector enabled state and subscribed business_action_id before writing",
		"event_dead_letters":              "failed non-dry-run event ingests are captured as dead letters with original payload, error, and retry/resolve workflow for data_admin remediation",
		"schema_changes":                  "use bootstrap_templates or create_dataset_from_template for common enterprise MIS structures; propose_schema first for observed custom fields; apply_schema_proposal requires explicit user/admin confirmation",
		"business_intent":                 "for a natural-language business request, call resolve_intent first, follow agent_playbooks and returned next_steps, and prefer tool_call_template payloads over manually composing raw dataset operations",
		"business_references":             "record_ref/person_ref/org_ref/file_ref fields are reference-shaped strings; record_ref may include config.ref_dataset so agents can link related MIS objects without raw SQL or hard-coded joins",
		"business_object_roles":           "list_business_objects and resolve_object_role expose stable semantic roles such as expense_report, employee, inventory_item, and inventory_movement so apps and skills can bind to object_role instead of hard-coded dataset ids",
		"querying":                        "use query_records for details, run_report or aggregate_records for analysis, export_records for CSV handoff, export_records_jsonl for system handoff, list_audit_logs for compliance review, export_audit_logs_csv for compliance handoff, and export job actions for larger handoffs; default record, revision, audit, event, inbox, job, approval, operation-plan, dataset, field, quality-run, schema-proposal, backup, connector, connector-health, connector-sync-run, api-key-policy, domain, template, access-preset, quality-check, business-view, business-action, event-contract, dashboard, and report pagination return stable cursor metadata; timestamp-backed lists return next_before and next_before_id, while static definition lists return next_before_id",
		"business_views":                  "prefer query_business_view when a stable employee-facing view exists for the task; use returned next_before for additional pages and next_before_id only when present",
		"dashboards":                      "use run_dashboard for company or domain overview before drilling into reports, inbox, views, or records",
		"quality_checks":                  "run_quality_check can scan records for schema, unknown fields, unique duplicates, and missing record_ref target records before or after imports",
		"business_rules":                  "list_business_rules and evaluate_business_rules expose built-in governance requirements for business actions, including dry-run, approval, backup, quality, admin gates, governance_status, status_reasons, next_steps, conditional triggers such as high-value sales order thresholds, and next_before_id pagination",
		"api_key_scopes":                  "scoped API keys can bind tenant_id, user_id, role, allowed_domains, allowed_actions, allowed_views, allowed_reports, allowed_dashboards, explicit allowed_datasets, allow_raw_data, allow_sensitive, and allow_admin; policy, domain, template, access-preset, quality-check, business action, business view, report, and dashboard lists return cursor pagination; business actions/views/reports/dashboards are preferred, raw dataset access is denied unless explicitly granted, and request headers cannot elevate beyond the matched key policy",
		"bulk_import":                     "batch_import_records, import_records_csv, and import_records_jsonl support synchronous imports; start_batch_import_job, start_csv_import_job, and start_jsonl_import_job run asynchronously with job status tracking",
		"sql":                             "raw SQL is not exposed",
		"unknown_fields":                  "allowed for flexible intake, but validate_record reports them",
		"sensitive_fields":                "fields marked sensitive are masked for data_user reads and exports",
		"business_unique_keys":            "fields with config.unique=true are checked before writes and batch imports",
		"backup_restore":                  "create backups before risky bulk changes; backup metadata includes sha256, download_url, and next_before/next_before_id list pagination for external archival; backup download and restore require data_admin, restore requires confirm=true; restore_record can recover one deleted record from revisions",
		"operation_plans":                 "create_operation_plan records high-risk bulk update/delete intent and preview; review_operation_plan approves or rejects; apply_operation_plan executes approved plans only",
		"approvals":                       "create_record_approval submits one business record for approval and can bind app_id, blueprint_id, and object_role for MaClaw App approval instances; list_record_approvals can filter by those fields; review_record_approval approves or rejects and requires data_admin",
		"maintenance":                     "run_maintenance performs SQLite integrity_check, optimize, and vacuum; data_admin required",
		"admin_required":                  "data_admin is required for template dataset creation, raw dataset create/update/delete, field upsert, bulk updates/deletes, schema proposal apply, backup download, and backup restore",
		"default_local_endpoint":          "http://127.0.0.1:18180",
		"recommended_agent_start_action":  "get_capabilities",
		"recommended_human_console_route": "/ui",
		"operations":                      "get_stats provides service-level dataset, record, import job, quality, audit, backup, and database size counters",
		"inbox":                           "get_inbox lists pending approvals, pending operation plans, failed jobs, and latest quality issues; get_inbox_summary returns counts by type/severity/status and overdue count",
	}
}

func agentBusinessPlaybooks(domains []string) []AgentBusinessPlaybook {
	out := []AgentBusinessPlaybook{}
	for _, domain := range domains {
		for _, useCase := range businessDomainUseCases(domain) {
			steps := businessUseCasePlaybookSteps(domain, useCase)
			out = append(out, AgentBusinessPlaybook{
				ID:          useCase.ID,
				Domain:      domain,
				Title:       useCase.Title,
				Description: useCase.Description,
				IntentHints: append([]string(nil), useCase.IntentHints...),
				Policy:      "Use business actions, views, dashboards, and reports first. Raw dataset or field mutation is schema administration, not normal business operation.",
				UseCase:     useCase,
				Steps:       steps,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Domain == out[j].Domain {
			return out[i].ID < out[j].ID
		}
		return out[i].Domain < out[j].Domain
	})
	return out
}

func businessUseCasePlaybookSteps(domain string, useCase BusinessDomainUseCase) []BusinessIntentNextStep {
	steps := []BusinessIntentNextStep{}
	order := 1
	addStep := func(action, purpose, description string, adminOnly, dryRun bool, params map[string]any, requiredFields []string, inputFields []DatasetTemplateField) {
		dataTemplate := businessActionDataTemplate(inputFields)
		steps = append(steps, BusinessIntentNextStep{
			Order:            order,
			Action:           action,
			Purpose:          purpose,
			Description:      description,
			AdminOnly:        adminOnly,
			DryRun:           dryRun,
			RequiredFields:   append([]string(nil), requiredFields...),
			InputFields:      append([]DatasetTemplateField(nil), inputFields...),
			DataTemplate:     dataTemplate,
			BodyTemplate:     businessIntentBodyTemplate(action, dryRun, params, dataTemplate),
			ToolCallTemplate: businessIntentToolCallTemplate(action, dryRun, params, dataTemplate),
			Params:           params,
		})
		order++
	}
	if domain != "company" {
		addStep("bootstrap_templates", "initialize", "Preview standard MIS datasets for this domain when the domain is not initialized.", true, true, map[string]any{"domains": []string{domain}, "dry_run": true}, nil, nil)
	}
	if useCase.PreferredDashboard != "" {
		addStep("run_dashboard", "overview", "Read the domain overview before making or explaining business changes.", false, false, map[string]any{"dashboard_id": useCase.PreferredDashboard}, nil, nil)
	}
	if useCase.PreferredAction != "" {
		requiredFields, inputFields := businessActionInputContract(useCase.PreferredAction)
		if useCase.DryRunRecommended {
			addStep("execute_business_action", "business_write_preview", "Validate and preview the business write before committing it.", false, true, map[string]any{"business_action_id": useCase.PreferredAction, "dry_run": true}, requiredFields, inputFields)
		}
		addStep("execute_business_action", "business_write", "Execute the preferred business action with caller-supplied business data.", false, false, map[string]any{"business_action_id": useCase.PreferredAction}, requiredFields, inputFields)
	}
	if useCase.PreferredView != "" {
		addStep("query_business_view", "business_read", "Read curated records for this business use case; pass before/ before_id from the previous response when continuing pages.", false, false, map[string]any{"view_id": useCase.PreferredView}, nil, nil)
	}
	if useCase.PreferredReport != "" {
		addStep("run_report", "analysis", "Run the built-in report for this business use case.", false, false, map[string]any{"report_id": useCase.PreferredReport}, nil, nil)
	}
	return steps
}

func relationshipsForCapabilities(datasets []DatasetCapability) []DatasetRelationship {
	initialized := map[string]struct{}{}
	out := []DatasetRelationship{}
	for _, dataset := range datasets {
		initialized[dataset.Dataset.ID] = struct{}{}
		out = append(out, relationshipsFromFields(dataset.Dataset.ID, dataset.Fields, false, true)...)
	}
	for _, tmpl := range datasetTemplates {
		if _, ok := initialized[tmpl.ID]; ok {
			continue
		}
		out = append(out, relationshipsFromTemplate(tmpl)...)
	}
	sortRelationships(out)
	return out
}

func toolActionCapabilities() []ToolActionCapability {
	return []ToolActionCapability{
		{Action: "get_capabilities", Purpose: "discover", Preferred: true, Description: "Read datasets, fields, templates, business actions, reports, and policy before deciding how to operate."},
		{Action: "list_domains", Purpose: "discover", Preferred: true, Description: "Read business-domain catalogs that group initialized datasets, missing templates, actions, views, dashboards, and reports."},
		{Action: "get_domain", Purpose: "discover", Preferred: true, Requires: []string{"domain"}, Description: "Read one business-domain catalog before operating in sales, finance, HR, legal, procurement, inventory, or assets."},
		{Action: "resolve_intent", Purpose: "discover", Preferred: true, Requires: []string{"query"}, Description: "Resolve a natural-language business request into candidate domain use cases and preferred actions/views/reports."},
		{Action: "list_business_objects", Purpose: "discover", Preferred: true, Description: "Read semantic business object roles such as expense_report or inventory_item and their mapped datasets."},
		{Action: "resolve_object_role", Purpose: "discover", Preferred: true, Requires: []string{"object_role"}, Description: "Resolve an app or blueprint object_role to the real dataset, template, and recommended business actions."},
		{Action: "list_relationships", Purpose: "discover", Preferred: true, Description: "Read dataset relationship hints generated from record_ref/person_ref/org_ref/file_ref fields with next_before_id pagination."},
		{Action: "get_inbox", Purpose: "operations", Preferred: true, Description: "Read MIS work items that need attention across approvals, operation plans, failed jobs, and quality issues."},
		{Action: "get_inbox_summary", Purpose: "operations", Preferred: true, Description: "Read MIS inbox counts by type, severity, status, and overdue count."},
		{Action: "get_stats", Purpose: "operations", Preferred: true, Description: "Read service health counters and per-dataset storage/import statistics."},
		{Action: "bootstrap_templates", Purpose: "schema", Preferred: true, AdminOnly: true, Description: "Create all common enterprise MIS datasets from built-in templates, skipping datasets that already exist."},
		{Action: "execute_business_action", Purpose: "business_write", Preferred: true, Requires: []string{"business_action_id", "data"}, Description: "Preferred way to change sales, HR, finance, and legal business data; pass dry_run=true to validate and preview first."},
		{Action: "list_business_rules", Purpose: "governance", Preferred: true, Description: "List built-in MIS business rules by domain, dataset, business action, or severity with next_before_id pagination."},
		{Action: "evaluate_business_rules", Purpose: "governance", Preferred: true, Requires: []string{"business_action_id"}, Description: "Evaluate governance gates and next-step tool templates before a business write or connector sync."},
		{Action: "list_event_contracts", Purpose: "system_integration", Preferred: true, Description: "List connector-facing event contracts derived from business actions, including dry-run and commit body templates, with next_before_id pagination."},
		{Action: "get_event_contract", Purpose: "system_integration", Preferred: true, Requires: []string{"business_action_id"}, Description: "Read one connector-facing event contract before wiring external CRM, ERP, HR, or finance events."},
		{Action: "ingest_event", Purpose: "system_integration", Preferred: true, Requires: []string{"source", "business_action_id", "record_id", "idempotency_key", "data"}, Description: "Preferred way for external CRM, ERP, HR, and finance connectors to submit business events without knowing raw dataset structure; pass dry_run=true to validate and preview without writing."},
		{Action: "list_data_events", Purpose: "system_integration", Description: "List applied data events with next_before and next_before_id pagination for connector trace review."},
		{Action: "list_event_dead_letters", Purpose: "system_integration", Preferred: true, Description: "List failed connector event ingests captured with payload and error; use next_before and next_before_id for stable remediation pagination."},
		{Action: "retry_event_dead_letter", Purpose: "system_integration", Requires: []string{"dead_letter_id"}, AdminOnly: true, Description: "Retry an open failed event using its saved payload and mark it retried on success."},
		{Action: "resolve_event_dead_letter", Purpose: "system_integration", Requires: []string{"dead_letter_id"}, AdminOnly: true, Description: "Mark a failed event as resolved when it was handled outside the retry flow."},
		{Action: "list_connectors", Purpose: "system_integration", Preferred: true, Description: "List registered external connectors and subscribed business actions."},
		{Action: "list_connector_health", Purpose: "system_integration", Preferred: true, Description: "Read health for all registered connectors from recent event logs and open dead letters."},
		{Action: "upsert_connector", Purpose: "system_integration", Requires: []string{"name", "subscribed_actions"}, AdminOnly: true, Description: "Register or update an external CRM, ERP, HRIS, finance, warehouse, or asset connector without storing raw secrets."},
		{Action: "test_connector", Purpose: "system_integration", Requires: []string{"connector_id"}, Description: "Validate connector subscriptions against event contracts without calling the remote system."},
		{Action: "get_connector_health", Purpose: "system_integration", Preferred: true, Requires: []string{"connector_id"}, Description: "Read connector health from recent event logs and open dead letters."},
		{Action: "get_connector_sync_state", Purpose: "system_integration", Preferred: true, Requires: []string{"connector_id"}, Description: "Read the connector sync cursor/checkpoint and last sync status."},
		{Action: "list_connector_sync_runs", Purpose: "system_integration", Preferred: true, Requires: []string{"connector_id"}, Description: "List recent connector batch sync run summaries for troubleshooting and resume decisions."},
		{Action: "update_connector_sync_state", Purpose: "system_integration", Requires: []string{"connector_id", "status"}, Description: "Update connector sync status, cursor, checkpoint, last_error, and synced record count after a pull/batch sync step."},
		{Action: "list_connector_sync_runs", Purpose: "system_integration", Requires: []string{"connector_id"}, Description: "List recent connector sync runs with next_before and next_before_id pagination."},
		{Action: "plan_connector_sync", Purpose: "system_integration", Preferred: true, Requires: []string{"connector_id"}, Description: "Generate a guarded connector sync rollout plan with readiness checks, first-page dry-run, checkpointing, and rollback steps."},
		{Action: "sync_connector_batch", Purpose: "system_integration", Preferred: true, Requires: []string{"connector_id", "events"}, Description: "Submit a batch of connector-scoped business events with per-item results, dead-letter capture, and optional sync cursor update."},
		{Action: "validate_connector_config", Purpose: "system_integration", Preferred: true, Requires: []string{"connector_id"}, Description: "Validate connector config field_mappings against subscribed business actions before production sync."},
		{Action: "check_connector_readiness", Purpose: "system_integration", Preferred: true, Requires: []string{"connector_id"}, Description: "Run connector contract, config, health, and optional sample preview checks before enabling production sync."},
		{Action: "suggest_connector_mapping", Purpose: "system_integration", Preferred: true, Requires: []string{"connector_id", "business_action_id", "sample_data"}, Description: "Suggest connector field_mappings from a sample external payload and a business action contract."},
		{Action: "patch_connector_config", Purpose: "system_integration", Requires: []string{"connector_id", "patch"}, AdminOnly: true, Description: "Deep-merge a reviewed connector config patch, such as suggested field_mappings, without replacing connector metadata."},
		{Action: "preview_connector_event", Purpose: "system_integration", Preferred: true, Requires: []string{"connector_id", "business_action_id", "data"}, Description: "Preview connector field mappings, resolved record/idempotency keys, and dry-run validation before writing connector data."},
		{Action: "ingest_connector_event", Purpose: "system_integration", Preferred: true, Requires: []string{"connector_id", "business_action_id", "record_id", "idempotency_key", "data"}, Description: "Submit a connector-scoped event; DataSrv verifies connector enabled state and business action subscription before using the normal event ingest path."},
		{Action: "query_business_view", Purpose: "business_read", Preferred: true, Requires: []string{"view_id"}, Description: "Read a curated business view with stable field projection, role-based masking, and next_before pagination cursor."},
		{Action: "list_dashboards", Purpose: "analysis", Preferred: true, Description: "List company and domain overview dashboards."},
		{Action: "run_dashboard", Purpose: "analysis", Preferred: true, Requires: []string{"dashboard_id"}, Description: "Run a business dashboard that combines stats, inbox summary, and built-in reports."},
		{Action: "run_report", Purpose: "analysis", Preferred: true, Requires: []string{"report_id"}, Description: "Run a built-in business report."},
		{Action: "aggregate_records", Purpose: "analysis", Requires: []string{"dataset_id", "metrics"}, Description: "Create an ad-hoc aggregate with count, count_distinct, sum, avg, min, max, group_by, filter, sort, limit, and scan_limit."},
		{Action: "run_quality_check", Purpose: "quality", Preferred: true, Requires: []string{"dataset_id"}, Description: "Scan dataset records for schema validation, unknown fields, duplicate unique keys, and relationship refs with stable internal pagination."},
		{Action: "query_records", Purpose: "read", Requires: []string{"dataset_id"}, Description: "Read detailed records through controlled filters; use next_before and next_before_id for default-order pagination."},
		{Action: "export_records", Purpose: "handoff", Requires: []string{"dataset_id"}, Description: "Export current query results as CSV with role-based masking; default-order exports page internally with before/before_id."},
		{Action: "export_records_jsonl", Purpose: "handoff", Requires: []string{"dataset_id"}, Description: "Export current query results as JSONL/NDJSON envelopes with role-based masking; default-order exports page internally with before/before_id."},
		{Action: "start_csv_export_job", Purpose: "handoff", Requires: []string{"dataset_id"}, Description: "Queue CSV export as an asynchronous job and download when completed; default-order jobs page internally with before/before_id up to the job limit."},
		{Action: "start_jsonl_export_job", Purpose: "handoff", Requires: []string{"dataset_id"}, Description: "Queue JSONL export as an asynchronous job and download when completed; default-order jobs page internally with before/before_id up to the job limit."},
		{Action: "list_export_jobs", Purpose: "operations", Description: "List recent export jobs by dataset or status with next_before and next_before_id pagination."},
		{Action: "get_export_job", Purpose: "operations", Requires: []string{"export_job_id"}, Description: "Read export job status and download path."},
		{Action: "download_export_job", Purpose: "handoff", Requires: []string{"export_job_id"}, Description: "Download completed export job content."},
		{Action: "list_audit_logs", Purpose: "compliance", Description: "Read filtered audit logs with next_before and next_before_id pagination for stable review."},
		{Action: "export_audit_logs_csv", Purpose: "compliance", Description: "Export filtered audit logs as CSV for review or archival."},
		{Action: "get_record_timeline", Purpose: "traceability", Requires: []string{"dataset_id", "id"}, Description: "Read a combined record timeline from revisions, approvals, data events, and audit logs with next_before and next_before_id pagination."},
		{Action: "get_related_records", Purpose: "traceability", Requires: []string{"dataset_id", "id"}, Description: "Read outgoing and incoming related business records discovered from record_ref relationships with before_id pagination."},
		{Action: "create_record_approval", Purpose: "business_approval", Requires: []string{"dataset_id", "id"}, Description: "Submit one business record for approval without changing schema or raw SQL; pass app_id, blueprint_id, and object_role for MaClaw App approval instances."},
		{Action: "list_record_approvals", Purpose: "business_approval", Description: "List business approvals by app, object role, dataset, record, workflow instance, status, or kind with next_before and next_before_id pagination."},
		{Action: "get_record_approval", Purpose: "business_approval", Requires: []string{"approval_id"}, Description: "Read one business approval request."},
		{Action: "review_record_approval", Purpose: "business_approval", Requires: []string{"approval_id", "decision"}, AdminOnly: true, Description: "Approve or reject a pending business approval request."},
		{Action: "validate_record", Purpose: "quality", Requires: []string{"dataset_id", "data"}, Description: "Dry-run one record against required/type/enum/unique checks."},
		{Action: "batch_import_records", Purpose: "bulk_write", Requires: []string{"dataset_id", "records"}, Description: "Validate then import records transactionally; use dry_run first."},
		{Action: "bulk_update_records", Purpose: "data_processing", Requires: []string{"dataset_id", "query", "set"}, AdminOnly: true, Description: "Dry-run or confirm a controlled bulk data correction with validation, revisions, and audit logging."},
		{Action: "bulk_delete_records", Purpose: "data_processing", Requires: []string{"dataset_id", "query"}, AdminOnly: true, Description: "Dry-run or confirm a controlled bulk deletion with record previews, revisions, and audit logging."},
		{Action: "restore_record", Purpose: "recovery", Requires: []string{"dataset_id", "id", "confirm"}, AdminOnly: true, Description: "Restore a deleted record from its delete revision with validation and audit logging."},
		{Action: "create_operation_plan", Purpose: "governance", Requires: []string{"dataset_id", "operation", "request"}, Description: "Save an auditable plan and impact preview for high-risk bulk update/delete work; plans require business scope unless allow_full_scan=true."},
		{Action: "list_operation_plans", Purpose: "governance", Description: "List operation plans by dataset, operation, or status with next_before and next_before_id pagination."},
		{Action: "get_operation_plan", Purpose: "governance", Requires: []string{"operation_plan_id"}, Description: "Read a saved operation plan and preview."},
		{Action: "review_operation_plan", Purpose: "governance", Requires: []string{"operation_plan_id", "decision"}, AdminOnly: true, Description: "Approve or reject a pending operation plan before execution."},
		{Action: "apply_operation_plan", Purpose: "governance", Requires: []string{"operation_plan_id", "confirm"}, AdminOnly: true, Description: "Execute an approved operation plan with confirmation."},
		{Action: "cancel_operation_plan", Purpose: "governance", Requires: []string{"operation_plan_id"}, AdminOnly: true, Description: "Cancel a pending operation plan."},
		{Action: "start_batch_import_job", Purpose: "bulk_write", Requires: []string{"dataset_id", "records"}, Description: "Queue structured record batch import as an asynchronous job and poll list_import_jobs/get_import_job for status."},
		{Action: "get_import_template_csv", Purpose: "bulk_write", Requires: []string{"dataset_id"}, Description: "Return a CSV header generated from field definitions and observed data keys before preparing imports."},
		{Action: "import_records_csv", Purpose: "bulk_write", Requires: []string{"dataset_id", "csv"}, Description: "Import header-based CSV through the same validation path as batch imports; use dry_run first."},
		{Action: "start_csv_import_job", Purpose: "bulk_write", Requires: []string{"dataset_id", "csv"}, Description: "Queue CSV import as an asynchronous job and poll list_import_jobs/get_import_job for status."},
		{Action: "import_records_jsonl", Purpose: "bulk_write", Requires: []string{"dataset_id", "jsonl"}, Description: "Import newline-delimited JSON records through the same validation path as batch imports; use dry_run first."},
		{Action: "start_jsonl_import_job", Purpose: "bulk_write", Requires: []string{"dataset_id", "jsonl"}, Description: "Queue JSONL import as an asynchronous job and poll list_import_jobs/get_import_job for status."},
		{Action: "list_import_jobs", Purpose: "operations", Description: "List recent import jobs by dataset or status with next_before and next_before_id pagination."},
		{Action: "get_import_job", Purpose: "operations", Requires: []string{"import_job_id"}, Description: "Read import job status and final validation/import result."},
		{Action: "propose_schema", Purpose: "schema", Requires: []string{"dataset_id", "data"}, Description: "Suggest schema additions from observed business data; list saved proposals with next_before and next_before_id pagination."},
		{Action: "apply_schema_proposal", Purpose: "schema", Requires: []string{"dataset_id", "proposal_id", "confirm"}, AdminOnly: true, Description: "Apply a saved schema proposal only after confirmation."},
		{Action: "create_backup", Purpose: "reliability", Description: "Create a checkpoint before risky imports or schema work."},
		{Action: "get_backup", Purpose: "reliability", Requires: []string{"backup_id"}, Description: "Read backup metadata including size, SHA-256 checksum, and download URL."},
		{Action: "download_backup", Purpose: "reliability", Requires: []string{"backup_id"}, AdminOnly: true, Description: "Download completed SQLite backup bytes for external archival."},
		{Action: "restore_backup", Purpose: "reliability", Requires: []string{"backup_id", "confirm"}, AdminOnly: true, Description: "Restore a checkpoint when explicitly confirmed."},
		{Action: "run_maintenance", Purpose: "operations", Requires: []string{"tasks"}, AdminOnly: true, Description: "Run database maintenance tasks such as integrity_check, optimize, and vacuum."},
	}
}

func cloneDatasetTemplates(in []DatasetTemplate) []DatasetTemplate {
	out := make([]DatasetTemplate, 0, len(in))
	for _, tmpl := range in {
		clone := tmpl
		clone.Fields = append([]DatasetTemplateField(nil), tmpl.Fields...)
		clone.SampleData = append([]map[string]interface{}(nil), tmpl.SampleData...)
		out = append(out, clone)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func cloneBusinessActions(in []BusinessAction) []BusinessAction {
	out := make([]BusinessAction, 0, len(in))
	for _, action := range in {
		clone := action
		clone.RequiredFields = append([]string(nil), action.RequiredFields...)
		clone.SuggestedTags = append([]string(nil), action.SuggestedTags...)
		clone.InputFields = append([]DatasetTemplateField(nil), action.InputFields...)
		out = append(out, clone)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func cloneEventContracts(actions []BusinessAction) []EventContract {
	out := make([]EventContract, 0, len(actions))
	for _, action := range actions {
		out = append(out, eventContractFromAction(action))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func cloneReportDefinitions(in []ReportDefinition) []ReportDefinition {
	out := make([]ReportDefinition, 0, len(in))
	for _, report := range in {
		clone := report
		clone.Aggregate.GroupBy = append([]string(nil), report.Aggregate.GroupBy...)
		clone.Aggregate.Metrics = append([]AggregateMetric(nil), report.Aggregate.Metrics...)
		clone.Aggregate.Sort = append([]SortSpec(nil), report.Aggregate.Sort...)
		out = append(out, clone)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func cloneDashboardDefinitions(in []DashboardDefinition) []DashboardDefinition {
	out := make([]DashboardDefinition, 0, len(in))
	for _, dashboard := range in {
		clone := dashboard
		clone.ReportIDs = append([]string(nil), dashboard.ReportIDs...)
		out = append(out, clone)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func cloneQualityChecks(in []QualityCheckDefinition) []QualityCheckDefinition {
	out := append([]QualityCheckDefinition(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func cloneBusinessRules(in []BusinessRuleDefinition) []BusinessRuleDefinition {
	out := make([]BusinessRuleDefinition, 0, len(in))
	for _, rule := range in {
		clone := rule
		clone.ConditionsMode = rule.ConditionsMode
		clone.Conditions = append([]BusinessRuleCondition(nil), rule.Conditions...)
		clone.DefaultApprover = rule.DefaultApprover
		clone.RecommendedChecks = append([]string(nil), rule.RecommendedChecks...)
		clone.ToolCallTemplates = cloneToolCallTemplates(rule.ToolCallTemplates)
		out = append(out, clone)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func cloneToolCallTemplates(in []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, item := range in {
		out = append(out, cloneJSONMap(item))
	}
	return out
}
