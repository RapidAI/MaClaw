package structureddata

import "strings"

func openAPISpec(version string) map[string]interface{} {
	paths := map[string]interface{}{}
	add := func(path string, methods ...string) {
		item := map[string]interface{}{}
		for _, method := range methods {
			item[method] = map[string]interface{}{
				"responses": map[string]interface{}{
					"200": map[string]string{"description": "OK"},
				},
			}
		}
		paths[path] = item
	}
	add("/health", "get")
	add("/readyz", "get")
	add("/version", "get")
	paths["/api/v1/setup/status"] = map[string]interface{}{
		"get": map[string]interface{}{
			"summary":     "Check local administrator setup status",
			"description": "Reports whether the first local administrator account has been initialized.",
			"responses": map[string]interface{}{
				"200": jsonObjectOpenAPIResponse("Setup status", setupStatusOpenAPISchema()),
			},
		},
	}
	paths["/api/v1/setup/admin"] = map[string]interface{}{
		"post": map[string]interface{}{
			"summary":     "Initialize the first local administrator",
			"description": "Creates the first datasrv administrator account. This endpoint is available only before any enabled administrator exists.",
			"requestBody": map[string]interface{}{
				"required": true,
				"content": map[string]interface{}{
					"application/json": map[string]interface{}{
						"schema": map[string]interface{}{
							"type":     "object",
							"required": []string{"username", "password"},
							"properties": map[string]interface{}{
								"tenant_id":     map[string]string{"type": "string"},
								"username":      map[string]string{"type": "string"},
								"password":      map[string]string{"type": "string", "format": "password"},
								"display_name":  map[string]string{"type": "string"},
								"expires_hours": map[string]string{"type": "integer"},
							},
						},
					},
				},
			},
			"responses": map[string]interface{}{
				"201": jsonObjectOpenAPIResponse("Administrator initialized and login token issued", initializeAdminResultOpenAPISchema()),
				"409": errorResponseOpenAPISchema("Administrator has already been initialized."),
			},
		},
	}
	paths["/api/v1/setup/tenants/sync"] = map[string]interface{}{
		"post": map[string]interface{}{
			"summary":     "Refresh setup tenant options from Hub",
			"description": "Pulls the Hub tenant registry using the saved Hub registration so the login screen can show current tenant options.",
			"requestBody": objectRequestBody(false, nil, map[string]interface{}{}),
			"responses": map[string]interface{}{
				"200": jsonObjectOpenAPIResponse("Hub tenants synced", syncHubTenantsResultOpenAPISchema()),
				"400": errorResponseOpenAPISchema("Hub registration is missing or inactive."),
			},
		},
	}
	paths["/api/v1/login"] = map[string]interface{}{
		"post": map[string]interface{}{
			"summary":     "Login with a local administrator account",
			"description": "Validates the configured administrator username and password, then returns a temporary bearer token for the Web Console and data APIs.",
			"requestBody": map[string]interface{}{
				"required": true,
				"content": map[string]interface{}{
					"application/json": map[string]interface{}{
						"schema": map[string]interface{}{
							"type":     "object",
							"required": []string{"username", "password"},
							"properties": map[string]interface{}{
								"tenant_id":     map[string]string{"type": "string"},
								"username":      map[string]string{"type": "string"},
								"password":      map[string]string{"type": "string", "format": "password"},
								"expires_hours": map[string]string{"type": "integer"},
							},
						},
					},
				},
			},
			"responses": map[string]interface{}{
				"200": jsonObjectOpenAPIResponse("Login token issued", loginResultOpenAPISchema()),
				"401": errorResponseOpenAPISchema("Invalid username or password."),
			},
		},
	}
	add("/api/v1/data/capabilities", "get")
	add("/api/v1/data/admin/accounts", "get", "post")
	add("/api/v1/data/admin/accounts/{username}", "patch")
	add("/api/v1/data/admin/sessions", "get")
	add("/api/v1/data/admin/sessions/{sessionId}", "patch", "delete")
	add("/api/v1/data/admin/tenants", "get")
	add("/api/v1/data/admin/tenants/sync", "post")
	add("/api/v1/data/admin/hub-registration", "get", "post")
	add("/api/v1/data/admin/hub-registration/register", "post")
	add("/api/v1/data/admin/hub-registration/sync-tenants", "post")
	add("/api/v1/data/access/presets", "get")
	add("/api/v1/data/access/review", "get")
	add("/api/v1/data/access/remediation-plan", "get")
	add("/api/v1/data/access/check", "post")
	add("/api/v1/data/access/api-keys", "get", "post")
	add("/api/v1/data/access/api-keys/{keyId}", "get", "patch", "delete")
	add("/api/v1/data/access/api-keys/{keyId}/capabilities", "get")
	add("/api/v1/data/access/api-keys/{keyId}/rotate", "post")
	add("/api/v1/data/domains", "get")
	add("/api/v1/data/domains/{domain}", "get")
	add("/api/v1/data/business-objects", "get")
	add("/api/v1/data/object-roles/resolve", "post")
	add("/api/v1/data/app-installations", "get", "post")
	add("/api/v1/data/app-installations/{appId}", "get", "put")
	add("/api/v1/data/relationships", "get")
	add("/api/v1/data/intent/resolve", "post")
	add("/api/v1/data/inbox", "get")
	add("/api/v1/data/inbox/summary", "get")
	add("/api/v1/data/stats", "get")
	add("/api/v1/data/governance/evidence-pack", "get")
	paths["/api/v1/data/governance/evidence-pack"] = map[string]interface{}{
		"get": map[string]interface{}{
			"summary":     "Export governance evidence pack",
			"description": "Combines service stats, access review, remediation plan, audit activity, work queue, and connector health into an auditable governance pack. The top-level summary_text is a server-generated handoff summary for MaClaw agents, administrators, and audit records.",
			"parameters": []map[string]interface{}{
				{
					"name":        "min_severity",
					"in":          "query",
					"required":    false,
					"description": "Optional access finding severity filter such as medium, high, or critical.",
					"schema":      map[string]string{"type": "string"},
				},
				{
					"name":        "lang",
					"in":          "query",
					"required":    false,
					"description": "Optional language for summary_text. Supported values: en, zh.",
					"schema":      map[string]interface{}{"type": "string", "enum": []string{"en", "zh"}},
				},
			},
			"responses": map[string]interface{}{
				"200": map[string]interface{}{
					"description": "Governance evidence pack",
					"headers":     governanceEvidenceOpenAPIHeaders(),
					"content": map[string]interface{}{
						"application/json": map[string]interface{}{
							"schema": map[string]interface{}{
								"type":     "object",
								"required": []string{"tenant_id", "exported_at", "generated_by", "summary", "sections"},
								"properties": map[string]interface{}{
									"tenant_id":       map[string]string{"type": "string"},
									"exported_at":     map[string]string{"type": "string", "format": "date-time"},
									"evidence_id":     map[string]string{"type": "string", "description": "Stable reference id derived from the evidence pack digest."},
									"evidence_sha256": map[string]string{"type": "string", "description": "SHA256 digest of the evidence pack content excluding summary_text and digest fields."},
									"summary_text":    map[string]string{"type": "string", "description": "Server-generated localized plain text summary suitable for copying into agent handoff, audit notes, or rollout records."},
									"summary":         map[string]string{"type": "object"},
									"sections":        map[string]interface{}{"type": "array", "items": map[string]string{"type": "object"}},
								},
							},
						},
					},
				},
				"403": map[string]string{"description": "Requires data_admin permission."},
			},
		},
	}
	paths["/api/v1/data/governance/evidence-summary.txt"] = map[string]interface{}{
		"get": map[string]interface{}{
			"summary":     "Export governance evidence summary text",
			"description": "Returns only the localized summary_text from the governance evidence pack as text/plain for agent handoff, audit notes, rollout records, and Web Console downloads.",
			"parameters": []map[string]interface{}{
				{
					"name":        "min_severity",
					"in":          "query",
					"required":    false,
					"description": "Optional access finding severity filter such as medium, high, or critical.",
					"schema":      map[string]string{"type": "string"},
				},
				{
					"name":        "lang",
					"in":          "query",
					"required":    false,
					"description": "Optional language for the summary. Supported values: en, zh.",
					"schema":      map[string]interface{}{"type": "string", "enum": []string{"en", "zh"}},
				},
			},
			"responses": map[string]interface{}{
				"200": map[string]interface{}{
					"description": "Localized governance evidence summary text",
					"headers":     governanceEvidenceOpenAPIHeaders(),
					"content": map[string]interface{}{
						"text/plain": map[string]interface{}{
							"schema": map[string]string{"type": "string"},
						},
					},
				},
				"403": map[string]string{"description": "Requires data_admin permission."},
			},
		},
	}
	add("/api/v1/data/maintenance/run", "post")
	add("/api/v1/data/templates", "get")
	add("/api/v1/data/templates/bootstrap", "post")
	add("/api/v1/data/templates/{templateId}", "get")
	add("/api/v1/data/templates/{templateId}/create", "post")
	add("/api/v1/data/backups", "get", "post")
	add("/api/v1/data/backups/{backupId}", "get")
	add("/api/v1/data/backups/{backupId}/download", "get")
	add("/api/v1/data/backups/{backupId}/restore", "post")
	add("/api/v1/data/events/dead-letter", "get")
	add("/api/v1/data/events/dead-letter/{deadLetterId}", "get")
	add("/api/v1/data/events/dead-letter/{deadLetterId}/retry", "post")
	add("/api/v1/data/events/dead-letter/{deadLetterId}/resolve", "post")
	add("/api/v1/data/events", "get", "post")
	add("/api/v1/data/event-contracts", "get")
	add("/api/v1/data/event-contracts/{actionId}", "get")
	add("/api/v1/data/connectors", "get", "post")
	add("/api/v1/data/connectors/health", "get")
	add("/api/v1/data/connectors/{connectorId}", "get", "put", "delete")
	add("/api/v1/data/connectors/{connectorId}/test", "post")
	add("/api/v1/data/connectors/{connectorId}/config/validate", "post")
	add("/api/v1/data/connectors/{connectorId}/readiness", "post")
	add("/api/v1/data/connectors/{connectorId}/health", "get")
	add("/api/v1/data/connectors/{connectorId}/sync-state", "get", "post")
	add("/api/v1/data/connectors/{connectorId}/sync-runs", "get")
	add("/api/v1/data/connectors/{connectorId}/sync-plan", "post")
	add("/api/v1/data/connectors/{connectorId}/sync-batch", "post")
	add("/api/v1/data/connectors/{connectorId}/config/patch", "post")
	add("/api/v1/data/connectors/{connectorId}/mappings/suggest", "post")
	add("/api/v1/data/connectors/{connectorId}/events/preview", "post")
	add("/api/v1/data/connectors/{connectorId}/events", "post")
	add("/api/v1/data/business-actions", "get")
	add("/api/v1/data/business-actions/{actionId}", "get")
	add("/api/v1/data/business-actions/{actionId}/execute", "post")
	add("/api/v1/data/business-rules", "get")
	add("/api/v1/data/business-rules/evaluate", "post")
	add("/api/v1/data/views", "get")
	add("/api/v1/data/views/{viewId}", "get")
	add("/api/v1/data/views/{viewId}/query", "post")
	add("/api/v1/data/dashboards", "get")
	add("/api/v1/data/dashboards/{dashboardId}", "get")
	add("/api/v1/data/dashboards/{dashboardId}/run", "post")
	add("/api/v1/data/reports", "get")
	add("/api/v1/data/reports/{reportId}", "get")
	add("/api/v1/data/reports/{reportId}/run", "post")
	add("/api/v1/data/quality-checks", "get")
	add("/api/v1/data/audit", "get")
	add("/api/v1/data/audit/export.csv", "get")
	add("/api/v1/data/import-jobs", "get")
	add("/api/v1/data/import-jobs/{jobId}", "get")
	add("/api/v1/data/export-jobs", "get")
	add("/api/v1/data/export-jobs/{jobId}", "get")
	add("/api/v1/data/export-jobs/{jobId}/download", "get")
	add("/api/v1/data/operation-plans", "get", "post")
	add("/api/v1/data/operation-plans/{planId}", "get")
	add("/api/v1/data/operation-plans/{planId}/review", "post")
	add("/api/v1/data/operation-plans/{planId}/apply", "post")
	add("/api/v1/data/operation-plans/{planId}/cancel", "post")
	add("/api/v1/data/approvals", "get")
	add("/api/v1/data/approvals/{approvalId}", "get")
	add("/api/v1/data/approvals/{approvalId}/review", "post")
	add("/api/v1/data/datasets", "get", "post")
	add("/api/v1/data/datasets/{datasetId}", "get", "patch", "delete")
	add("/api/v1/data/datasets/{datasetId}/fields", "get", "put")
	add("/api/v1/data/datasets/{datasetId}/schema-proposals", "get", "post")
	add("/api/v1/data/datasets/{datasetId}/schema-proposals/{proposalId}", "get")
	add("/api/v1/data/datasets/{datasetId}/schema-proposals/apply", "post")
	add("/api/v1/data/datasets/{datasetId}/aggregate", "post")
	add("/api/v1/data/datasets/{datasetId}/quality/run", "post")
	add("/api/v1/data/datasets/{datasetId}/quality/runs", "get")
	add("/api/v1/data/datasets/{datasetId}/quality/runs/{runId}", "get")
	add("/api/v1/data/datasets/{datasetId}/records", "get", "post")
	add("/api/v1/data/datasets/{datasetId}/records/query", "post")
	add("/api/v1/data/datasets/{datasetId}/records/export.csv", "post")
	add("/api/v1/data/datasets/{datasetId}/records/export.jsonl", "post")
	add("/api/v1/data/datasets/{datasetId}/records/export.csv/jobs", "post")
	add("/api/v1/data/datasets/{datasetId}/records/export.jsonl/jobs", "post")
	add("/api/v1/data/datasets/{datasetId}/records/import-template.csv", "get")
	add("/api/v1/data/datasets/{datasetId}/records/validate", "post")
	add("/api/v1/data/datasets/{datasetId}/records/batch", "post")
	add("/api/v1/data/datasets/{datasetId}/records/bulk-update", "post")
	add("/api/v1/data/datasets/{datasetId}/records/bulk-delete", "post")
	add("/api/v1/data/datasets/{datasetId}/records/batch/jobs", "post")
	add("/api/v1/data/datasets/{datasetId}/records/import.csv", "post")
	add("/api/v1/data/datasets/{datasetId}/records/import.csv/jobs", "post")
	add("/api/v1/data/datasets/{datasetId}/records/import.jsonl", "post")
	add("/api/v1/data/datasets/{datasetId}/records/import.jsonl/jobs", "post")
	add("/api/v1/data/datasets/{datasetId}/records/{recordId}/approvals", "post")
	add("/api/v1/data/datasets/{datasetId}/records/{recordId}/related", "get")
	add("/api/v1/data/datasets/{datasetId}/records/{recordId}/revisions", "get")
	add("/api/v1/data/datasets/{datasetId}/records/{recordId}/timeline", "get")
	add("/api/v1/data/datasets/{datasetId}/records/{recordId}/restore", "post")
	add("/api/v1/data/datasets/{datasetId}/records/{recordId}", "get", "patch", "delete")
	addCursorParameters(paths, []string{
		"/api/v1/data/datasets/{datasetId}/records/{recordId}/related",
		"/api/v1/data/inbox",
		"/api/v1/data/datasets/{datasetId}/records/{recordId}/timeline",
	})
	setOpenAPIGetResponses(paths, "/api/v1/data/datasets/{datasetId}/records/{recordId}/related", relatedRecordsOpenAPIResponses())
	setOpenAPIGetResponses(paths, "/api/v1/data/inbox", inboxOpenAPIResponses())
	setOpenAPIGetResponses(paths, "/api/v1/data/datasets/{datasetId}/records/{recordId}/timeline", recordTimelineOpenAPIResponses())
	addCursorPagination(paths, []string{
		"/api/v1/data/access/presets",
		"/api/v1/data/access/api-keys",
		"/api/v1/data/domains",
		"/api/v1/data/business-objects",
		"/api/v1/data/app-installations",
		"/api/v1/data/relationships",
		"/api/v1/data/templates",
		"/api/v1/data/backups",
		"/api/v1/data/events/dead-letter",
		"/api/v1/data/events",
		"/api/v1/data/event-contracts",
		"/api/v1/data/connectors",
		"/api/v1/data/connectors/health",
		"/api/v1/data/connectors/{connectorId}/sync-runs",
		"/api/v1/data/business-actions",
		"/api/v1/data/business-rules",
		"/api/v1/data/views",
		"/api/v1/data/dashboards",
		"/api/v1/data/reports",
		"/api/v1/data/quality-checks",
		"/api/v1/data/audit",
		"/api/v1/data/import-jobs",
		"/api/v1/data/export-jobs",
		"/api/v1/data/operation-plans",
		"/api/v1/data/approvals",
		"/api/v1/data/datasets",
		"/api/v1/data/datasets/{datasetId}/fields",
		"/api/v1/data/datasets/{datasetId}/schema-proposals",
		"/api/v1/data/datasets/{datasetId}/quality/runs",
		"/api/v1/data/datasets/{datasetId}/records",
		"/api/v1/data/datasets/{datasetId}/records/{recordId}/revisions",
	})
	addQueryParameters(paths, map[string][]map[string]interface{}{
		"/api/v1/data/access/api-keys": {
			stringQueryParam("q", "Search API keys by id, user, or note."),
			stringQueryParam("status", "Filter managed API keys by status."),
			boolQueryParam("enabled", "Filter managed API keys by enabled state."),
		},
		"/api/v1/data/business-rules": {
			stringQueryParam("domain", "Filter business rules by domain."),
			stringQueryParam("dataset_id", "Filter business rules by dataset id."),
			stringQueryParam("business_action_id", "Filter business rules by business action id."),
			stringQueryParam("severity", "Filter business rules by exact severity: critical, high, medium, or info."),
		},
		"/api/v1/data/business-objects": {
			stringQueryParam("app_id", "Filter business objects by MaClaw app id."),
			stringQueryParam("blueprint_id", "Filter business objects by app blueprint id."),
			stringQueryParam("domain", "Filter business objects by domain."),
			stringQueryParam("object_role", "Filter by semantic object role such as expense_report or inventory_item."),
		},
		"/api/v1/data/app-installations": {
			stringQueryParam("app_id", "Filter installed MaClaw Apps by app id."),
			stringQueryParam("blueprint_id", "Filter installed MaClaw Apps by blueprint id."),
			stringQueryParam("kind", "Filter installed MaClaw Apps by app kind."),
			stringQueryParam("source", "Filter installed MaClaw Apps by distribution source."),
			stringQueryParam("status", "Filter installed MaClaw Apps by status such as installed, disabled, or error."),
			stringQueryParam("workflow_skill_id", "Filter approval apps by workflow skill id."),
			stringQueryParam("workflow_node", "Filter approval apps by submit, approval, attention, or result workflow node."),
			stringQueryParam("approval_status", "Filter approval apps by approval instance status such as approved, rejected, attention, pending, or requires_input."),
			stringQueryParam("approval_result_status", "Alias of approval_status for approval result status filters."),
			stringQueryParam("approval_decision", "Filter approval apps by approval result decision such as approved, rejected, or attention."),
			stringQueryParam("decision", "Alias of approval_decision."),
			stringQueryParam("applicant_id", "Filter approval apps by applicant or requester id for My Requests views."),
			stringQueryParam("submitted_by", "Alias of applicant_id for submitted-by filters."),
			stringQueryParam("created_by", "Alias of applicant_id for creator filters."),
			stringQueryParam("approver_id", "Filter approval apps by assigned approver or current assignee id for My Approvals views."),
			stringQueryParam("assigned_to", "Alias of approver_id for current task assignment filters."),
			stringQueryParam("current_assignee", "Alias of approver_id for current approval assignee filters."),
			stringQueryParam("approval_id", "Filter approval apps by DataSrv approval id or record approval id found in test evidence."),
			stringQueryParam("record_approval_id", "Alias of approval_id for DataSrv record approval ids."),
			stringQueryParam("workflow_instance_id", "Filter approval apps by workflow or approval instance id found in test evidence."),
			stringQueryParam("approval_instance_id", "Alias of workflow_instance_id for approval instance ids."),
			stringQueryParam("instance_id", "Alias of workflow_instance_id for compact approval instance ids."),
			stringQueryParam("dataset_id", "Filter approval apps by DataSrv dataset id found in approval or result evidence."),
			stringQueryParam("dataset", "Alias of dataset_id."),
			stringQueryParam("object_role", "Filter installed MaClaw Apps by semantic DataSrv object role from bindings or evidence."),
			stringQueryParam("object", "Alias of object_role."),
			stringQueryParam("record_id", "Filter approval apps by business record id found in approval or result evidence."),
			stringQueryParam("business_record_id", "Alias of record_id for result payload business records."),
			stringQueryParam("result_type", "Filter installed MaClaw Apps by declared or observed result type such as approval_result, document, inline_content, table, or business_status."),
			stringQueryParam("output_type", "Alias of result_type for output block filters."),
			stringQueryParam("definition_fingerprint", "Filter installed MaClaw Apps by the app definition hash or fingerprint carried by current test evidence."),
			stringQueryParam("definition_hash", "Alias of definition_fingerprint."),
			stringQueryParam("app_definition_hash", "Alias of definition_fingerprint for App Studio definition hash filters."),
			stringQueryParam("app_definition_fingerprint", "Alias of definition_fingerprint for App Studio definition fingerprint filters."),
			boolQueryParam("has_blocking_dependency", "Filter installed MaClaw Apps by whether dependency verification found a blocking dependency."),
			boolQueryParam("has_missing_required_dependency", "Filter installed MaClaw Apps by whether a required dependency is missing or unavailable."),
			boolQueryParam("has_missing_required", "Alias of has_missing_required_dependency."),
		},
		"/api/v1/data/relationships": {
			stringQueryParam("dataset_id", "Filter relationships by source or target dataset id."),
		},
		"/api/v1/data/inbox": {
			stringQueryParam("dataset_id", "Filter inbox items by dataset id."),
			stringQueryParam("app_id", "Filter approval inbox items by MaClaw App id."),
			stringQueryParam("blueprint_id", "Filter approval inbox items by App blueprint id."),
			stringQueryParam("object_role", "Filter approval inbox items by semantic business object role."),
			stringQueryParam("workflow_skill_id", "Filter approval inbox items by workflow Skill id."),
			stringQueryParam("workflow_version", "Filter approval inbox items by workflow Skill version."),
			stringQueryParam("workflow_instance_id", "Filter approval inbox items by workflow instance id."),
			stringQueryParam("workflow_node_id", "Filter approval inbox items by current workflow node id."),
			stringQueryParam("current_node_id", "Alias of workflow_node_id for app approval instance current-node filters."),
			stringQueryParam("current_node", "Alias of workflow_node_id for app approval instance current-node filters."),
			stringQueryParam("workflow_node", "Alias of workflow_node_id for app approval instance current-node filters."),
			stringQueryParam("business_status", "Filter approval inbox items by business status."),
			stringQueryParam("result_status", "Filter approval inbox items by result status."),
			stringQueryParam("lane", "Filter approval inbox items by lane: my_requests, pending_my_approval, handled, attention, or all."),
			stringQueryParam("user_id", "Evaluate approval lane filters for this user; defaults to authenticated user."),
			stringQueryParam("type", "Filter inbox items by type: approval, operation_plan, import_job, export_job, event_dead_letter, or quality."),
			stringQueryParam("status", "Filter inbox items by known workflow status such as pending, failed, open, issue, or ok."),
			boolQueryParam("include_ok", "Include informational OK items in addition to actionable work."),
		},
		"/api/v1/data/inbox/summary": {
			stringQueryParam("dataset_id", "Filter inbox summary by dataset id."),
			stringQueryParam("app_id", "Filter approval inbox summary by MaClaw App id."),
			stringQueryParam("blueprint_id", "Filter approval inbox summary by App blueprint id."),
			stringQueryParam("object_role", "Filter approval inbox summary by semantic business object role."),
			stringQueryParam("workflow_skill_id", "Filter approval inbox summary by workflow Skill id."),
			stringQueryParam("workflow_version", "Filter approval inbox summary by workflow Skill version."),
			stringQueryParam("workflow_instance_id", "Filter approval inbox summary by workflow instance id."),
			stringQueryParam("workflow_node_id", "Filter approval inbox summary by current workflow node id."),
			stringQueryParam("current_node_id", "Alias of workflow_node_id for app approval instance current-node filters."),
			stringQueryParam("current_node", "Alias of workflow_node_id for app approval instance current-node filters."),
			stringQueryParam("workflow_node", "Alias of workflow_node_id for app approval instance current-node filters."),
			stringQueryParam("business_status", "Filter approval inbox summary by business status."),
			stringQueryParam("result_status", "Filter approval inbox summary by result status."),
			stringQueryParam("lane", "Filter approval inbox summary by lane: my_requests, pending_my_approval, handled, attention, or all."),
			stringQueryParam("user_id", "Evaluate approval lane filters for this user; defaults to authenticated user."),
			stringQueryParam("type", "Filter inbox summary by type: approval, operation_plan, import_job, export_job, event_dead_letter, or quality."),
			stringQueryParam("status", "Filter inbox summary by known workflow status such as pending, failed, open, issue, or ok."),
			boolQueryParam("include_ok", "Include informational OK items in addition to actionable work."),
		},
		"/api/v1/data/event-contracts": {
			stringQueryParam("domain", "Filter event contracts by business domain."),
		},
		"/api/v1/data/connectors": {
			stringQueryParam("domain", "Filter connectors by business domain."),
			stringQueryParam("kind", "Filter connectors by integration kind."),
			boolQueryParam("enabled", "Filter connectors by enabled state."),
		},
		"/api/v1/data/connectors/health": {
			stringQueryParam("domain", "Filter connector health rows by business domain."),
			stringQueryParam("kind", "Filter connector health rows by integration kind."),
			boolQueryParam("enabled", "Filter connector health rows by enabled state."),
		},
		"/api/v1/data/events": {
			stringQueryParam("dataset_id", "Filter events by dataset id."),
			stringQueryParam("record_id", "Filter events by record id."),
			stringQueryParam("source", "Filter events by external source."),
			stringQueryParam("event_type", "Filter events by event type."),
			stringQueryParam("business_action_id", "Filter events by business action id."),
			stringQueryParam("idempotency_key", "Filter events by idempotency key."),
		},
		"/api/v1/data/events/dead-letter": {
			stringQueryParam("status", "Filter dead letters by status: open, resolved, or retried."),
			stringQueryParam("source", "Filter dead letters by external source."),
			stringQueryParam("event_type", "Filter dead letters by event type."),
			stringQueryParam("business_action_id", "Filter dead letters by business action id."),
			stringQueryParam("dataset_id", "Filter dead letters by dataset id."),
			stringQueryParam("record_id", "Filter dead letters by record id."),
			stringQueryParam("idempotency_key", "Filter dead letters by idempotency key."),
		},
		"/api/v1/data/audit": {
			stringQueryParam("dataset_id", "Filter audit logs by dataset id."),
			stringQueryParam("action", "Filter audit logs by action."),
			stringQueryParam("user_id", "Filter audit logs by user id."),
			stringQueryParam("target_type", "Filter audit logs by target type."),
			stringQueryParam("target_id", "Filter audit logs by target id."),
			stringQueryParam("q", "Search audit log summary, action, and target fields."),
		},
		"/api/v1/data/import-jobs": {
			stringQueryParam("dataset_id", "Filter import jobs by dataset id."),
			stringQueryParam("status", "Filter import jobs by status: queued, running, completed, or failed."),
		},
		"/api/v1/data/export-jobs": {
			stringQueryParam("dataset_id", "Filter export jobs by dataset id."),
			stringQueryParam("status", "Filter export jobs by status: queued, running, completed, or failed."),
		},
		"/api/v1/data/operation-plans": {
			stringQueryParam("dataset_id", "Filter operation plans by dataset id."),
			stringQueryParam("operation", "Filter operation plans by operation type: bulk_update_records or bulk_delete_records."),
			stringQueryParam("status", "Filter operation plans by status: pending, approved, rejected, applied, or canceled."),
		},
		"/api/v1/data/approvals": {
			stringQueryParam("dataset_id", "Filter approvals by dataset id."),
			stringQueryParam("record_id", "Filter approvals by record id."),
			stringQueryParam("app_id", "Filter approvals by MaClaw app id."),
			stringQueryParam("blueprint_id", "Filter approvals by MaClaw app blueprint id."),
			stringQueryParam("object_role", "Filter approvals by semantic business object role."),
			stringQueryParam("status", "Filter approvals by status: pending, approved, or rejected."),
			stringQueryParam("kind", "Filter approvals by kind."),
			stringQueryParam("approval_workflow_id", "Filter approvals by business approval workflow id."),
			stringQueryParam("trigger_event", "Filter approvals by trigger event."),
			stringQueryParam("submitted_by", "Filter approvals by submitting user."),
			stringQueryParam("current_assignee", "Filter approvals by current assignee."),
			stringQueryParam("current_assignee_type", "Filter approvals by current assignee type."),
			stringQueryParam("from_status", "Filter approvals by previous business status."),
			stringQueryParam("to_status", "Filter approvals by next business status."),
			stringQueryParam("workflow_skill_id", "Filter approvals by workflow skill id."),
			stringQueryParam("workflow_version", "Filter approvals by workflow skill version captured on the approval link."),
			stringQueryParam("workflow_instance_id", "Filter approvals by workflow instance id."),
			stringQueryParam("workflow_node_id", "Filter approvals by current workflow node id."),
			stringQueryParam("current_node_id", "Alias of workflow_node_id for app approval instance current-node filters."),
			stringQueryParam("current_node", "Alias of workflow_node_id for app approval instance current-node filters."),
			stringQueryParam("workflow_node", "Alias of workflow_node_id for app approval instance current-node filters."),
			stringQueryParam("business_status", "Filter approvals by business-facing workflow status."),
			stringQueryParam("result_status", "Filter approvals by machine-readable result status."),
			stringQueryParam("assigned_to", "Filter approvals by assignee."),
			stringQueryParam("created_by", "Filter approvals by requester user id."),
			stringQueryParam("reviewed_by", "Filter approvals by reviewer user id."),
			stringQueryParam("lane", "Approval center lane: my_requests, pending_my_approval, handled, attention, or all. Lane is evaluated for the authenticated user."),
			boolQueryParam("overdue", "Only return overdue approvals when true."),
		},
		"/api/v1/data/datasets/{datasetId}/schema-proposals": {
			stringQueryParam("status", "Filter schema proposals by status: pending or applied."),
		},
		"/api/v1/data/datasets/{datasetId}/records": {
			stringQueryParam("q", "Free-text search query."),
			stringQueryParam("tag", "Filter records by tag."),
		},
	})
	setOpenAPIPostRequestBody(paths, "/api/v1/data/templates/bootstrap", bootstrapTemplatesOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/templates/{templateId}/create", createFromTemplateOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/intent/resolve", resolveBusinessIntentOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/object-roles/resolve", resolveObjectRoleOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/app-installations", upsertAppInstallationOpenAPIRequestBody())
	setOpenAPIPutRequestBody(paths, "/api/v1/data/app-installations/{appId}", upsertAppInstallationOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/business-actions/{actionId}/execute", executeBusinessActionOpenAPIRequestBody())
	setOpenAPIPostResponses(paths, "/api/v1/data/business-actions/{actionId}/execute", executeBusinessActionOpenAPIResponses())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/business-rules/evaluate", evaluateBusinessRulesOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/reports/{reportId}/run", aggregateOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/events", ingestEventOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/events/dead-letter/{deadLetterId}/resolve", resolveDeadLetterOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/connectors", upsertConnectorOpenAPIRequestBody())
	setOpenAPIPutRequestBody(paths, "/api/v1/data/connectors/{connectorId}", upsertConnectorOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/connectors/{connectorId}/readiness", connectorReadinessOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/connectors/{connectorId}/sync-state", updateConnectorSyncStateOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/connectors/{connectorId}/sync-plan", connectorSyncPlanOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/connectors/{connectorId}/sync-batch", connectorSyncBatchOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/connectors/{connectorId}/config/patch", patchConnectorConfigOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/connectors/{connectorId}/mappings/suggest", suggestConnectorMappingOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/connectors/{connectorId}/events/preview", ingestEventOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/connectors/{connectorId}/events", ingestEventOpenAPIRequestBody())
	for _, path := range []string{"/api/v1/data/datasets/{datasetId}/records/batch", "/api/v1/data/datasets/{datasetId}/records/batch/jobs"} {
		setOpenAPIPostRequestBody(paths, path, batchImportOpenAPIRequestBody())
	}
	setOpenAPIPostRequestBody(paths, "/api/v1/data/datasets/{datasetId}/records/bulk-update", bulkUpdateOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/datasets/{datasetId}/records/bulk-delete", bulkDeleteOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/datasets", createDatasetOpenAPIRequestBody())
	setOpenAPIPatchRequestBody(paths, "/api/v1/data/datasets/{datasetId}", updateDatasetOpenAPIRequestBody())
	setOpenAPIPutRequestBody(paths, "/api/v1/data/datasets/{datasetId}/fields", upsertFieldsOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/datasets/{datasetId}/records", createRecordOpenAPIRequestBody())
	setOpenAPIPatchRequestBody(paths, "/api/v1/data/datasets/{datasetId}/records/{recordId}", updateRecordOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/datasets/{datasetId}/records/validate", validateRecordOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/datasets/{datasetId}/records/{recordId}/restore", restoreRecordOpenAPIRequestBody())
	for _, path := range []string{"/api/v1/data/datasets/{datasetId}/records/import.csv", "/api/v1/data/datasets/{datasetId}/records/import.csv/jobs"} {
		setOpenAPIPostRequestBody(paths, path, importCSVOpenAPIRequestBody())
	}
	for _, path := range []string{"/api/v1/data/datasets/{datasetId}/records/import.jsonl", "/api/v1/data/datasets/{datasetId}/records/import.jsonl/jobs"} {
		setOpenAPIPostRequestBody(paths, path, importJSONLOpenAPIRequestBody())
	}
	setOpenAPIPostRequestBody(paths, "/api/v1/data/datasets/{datasetId}/schema-proposals", schemaProposalOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/datasets/{datasetId}/schema-proposals/apply", applySchemaProposalOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/datasets/{datasetId}/aggregate", aggregateOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/datasets/{datasetId}/quality/run", runQualityCheckOpenAPIRequestBody())
	for _, path := range []string{
		"/api/v1/data/datasets/{datasetId}/records/export.csv",
		"/api/v1/data/datasets/{datasetId}/records/export.jsonl",
	} {
		setOpenAPIPostRequestBody(paths, path, queryRecordsOpenAPIRequestBody(5000, "Maximum number of records to export synchronously."))
	}
	for _, path := range []string{
		"/api/v1/data/datasets/{datasetId}/records/export.csv/jobs",
		"/api/v1/data/datasets/{datasetId}/records/export.jsonl/jobs",
	} {
		setOpenAPIPostRequestBody(paths, path, queryRecordsOpenAPIRequestBody(50000, "Maximum number of records to export in the background job."))
	}
	setOpenAPIPostRequestBody(paths, "/api/v1/data/access/check", accessCheckOpenAPIRequestBody())
	adminTenantQuery := []map[string]interface{}{stringQueryParam("tenant", "Optional administrator tenant scope. Global administrators may use all or a specific tenant id; tenant administrators are limited to their own tenant.")}
	addOperationQueryParameters(paths, "/api/v1/data/admin/accounts", []string{"get"}, adminTenantQuery)
	addOperationQueryParameters(paths, "/api/v1/data/admin/accounts/{username}", []string{"patch"}, adminTenantQuery)
	addOperationQueryParameters(paths, "/api/v1/data/admin/sessions", []string{"get"}, adminTenantQuery)
	addOperationQueryParameters(paths, "/api/v1/data/admin/sessions/{sessionId}", []string{"patch", "delete"}, adminTenantQuery)
	setOpenAPIGetResponses(paths, "/api/v1/data/admin/accounts", adminAccountListOpenAPIResponses())
	setOpenAPIPostResponses(paths, "/api/v1/data/admin/accounts", adminAccountResultOpenAPIResponses("201", "Administrator account created"))
	setOpenAPIPostRequestBody(paths, "/api/v1/data/admin/accounts", createAdminAccountOpenAPIRequestBody())
	setOpenAPIPatchResponses(paths, "/api/v1/data/admin/accounts/{username}", adminAccountResultOpenAPIResponses("200", "Administrator account updated"))
	setOpenAPIPatchRequestBody(paths, "/api/v1/data/admin/accounts/{username}", updateAdminAccountOpenAPIRequestBody())
	setOpenAPIGetResponses(paths, "/api/v1/data/admin/sessions", adminSessionListOpenAPIResponses())
	setOpenAPIPatchResponses(paths, "/api/v1/data/admin/sessions/{sessionId}", adminSessionResultOpenAPIResponses())
	setOpenAPIPatchRequestBody(paths, "/api/v1/data/admin/sessions/{sessionId}", updateAdminSessionOpenAPIRequestBody())
	setOpenAPIDeleteResponses(paths, "/api/v1/data/admin/sessions/{sessionId}", revokeAdminSessionOpenAPIResponses())
	setOpenAPIGetResponses(paths, "/api/v1/data/admin/tenants", dataTenantListOpenAPIResponses())
	setOpenAPIPostResponses(paths, "/api/v1/data/admin/tenants/sync", syncHubTenantsOpenAPIResponses())
	setOpenAPIGetResponses(paths, "/api/v1/data/admin/hub-registration", hubRegistrationOpenAPIResponses("Hub registration status"))
	setOpenAPIPostResponses(paths, "/api/v1/data/admin/hub-registration", hubRegistrationOpenAPIResponses("Hub registration saved"))
	setOpenAPIPostResponses(paths, "/api/v1/data/admin/hub-registration/register", hubRegistrationOpenAPIResponses("Hub registration completed"))
	setOpenAPIPostResponses(paths, "/api/v1/data/admin/hub-registration/sync-tenants", syncHubTenantsOpenAPIResponses())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/admin/tenants/sync", syncHubTenantsOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/admin/hub-registration", saveHubRegistrationOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/admin/hub-registration/register", objectRequestBody(false, nil, map[string]interface{}{}))
	setOpenAPIPostRequestBody(paths, "/api/v1/data/admin/hub-registration/sync-tenants", objectRequestBody(false, nil, map[string]interface{}{}))
	setOpenAPIPostRequestBody(paths, "/api/v1/data/access/api-keys", createAPIKeyPolicyOpenAPIRequestBody())
	setOpenAPIPatchRequestBody(paths, "/api/v1/data/access/api-keys/{keyId}", updateAPIKeyPolicyOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/maintenance/run", runMaintenanceOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/backups", createBackupOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/backups/{backupId}/restore", restoreBackupOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/operation-plans", createOperationPlanOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/operation-plans/{planId}/review", reviewDecisionOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/operation-plans/{planId}/apply", applyOperationPlanOpenAPIRequestBody())
	setOpenAPIGetResponses(paths, "/api/v1/data/approvals", recordApprovalListOpenAPIResponses())
	setOpenAPIGetResponses(paths, "/api/v1/data/approvals/{approvalId}", recordApprovalOpenAPIResponses())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/datasets/{datasetId}/records/{recordId}/approvals", createRecordApprovalOpenAPIRequestBody())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/approvals/{approvalId}/review", reviewRecordApprovalOpenAPIRequestBody())
	setOpenAPIPostResponses(paths, "/api/v1/data/datasets/{datasetId}/records/query", listResponseOpenAPIResponses())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/datasets/{datasetId}/records/query", queryRecordsOpenAPIRequestBody(500, "Maximum number of records to return."))
	setOpenAPIPostResponses(paths, "/api/v1/data/views/{viewId}/query", businessViewQueryOpenAPIResponses())
	setOpenAPIPostRequestBody(paths, "/api/v1/data/views/{viewId}/query", queryRecordsOpenAPIRequestBody(500, "Maximum number of records to return."))
	setOpenAPIPostResponses(paths, "/api/v1/data/reports/{reportId}/run", reportRunOpenAPIResponses())
	setOpenAPIPostResponses(paths, "/api/v1/data/dashboards/{dashboardId}/run", dashboardRunOpenAPIResponses())
	addDownloadResponseMetadata(paths, downloadOpenAPIMetadataByRoute())
	addAuthErrorResponses(paths)
	return map[string]interface{}{
		"openapi": "3.0.3",
		"info": map[string]interface{}{
			"title":       "MaClawDataSrv API",
			"version":     version,
			"description": "Controlled REST API for MaClaw MIS structured data service.",
		},
		"servers":  []map[string]string{{"url": "http://127.0.0.1:18180"}},
		"security": []map[string][]string{{"bearerAuth": []string{}}},
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{
				"bearerAuth": map[string]string{"type": "http", "scheme": "bearer"},
			},
		},
		"paths": paths,
	}
}

type downloadOpenAPIMetadata struct {
	ContentTypes   []string
	BackupChecksum bool
}

func downloadOpenAPIMetadataByRoute() map[string]downloadOpenAPIMetadata {
	return map[string]downloadOpenAPIMetadata{
		"get /api/v1/data/governance/evidence-summary.txt":                  {ContentTypes: []string{"text/plain"}},
		"get /api/v1/data/backups/{backupId}/download":                      {ContentTypes: []string{"application/octet-stream"}, BackupChecksum: true},
		"get /api/v1/data/audit/export.csv":                                 {ContentTypes: []string{"text/csv"}},
		"get /api/v1/data/export-jobs/{jobId}/download":                     {ContentTypes: []string{"text/csv", "application/x-ndjson", "text/plain"}},
		"post /api/v1/data/datasets/{datasetId}/records/export.csv":         {ContentTypes: []string{"text/csv"}},
		"post /api/v1/data/datasets/{datasetId}/records/export.jsonl":       {ContentTypes: []string{"application/x-ndjson"}},
		"get /api/v1/data/datasets/{datasetId}/records/import-template.csv": {ContentTypes: []string{"text/csv"}},
	}
}

func addDownloadResponseMetadata(paths map[string]interface{}, metadataByRoute map[string]downloadOpenAPIMetadata) {
	for route, metadata := range metadataByRoute {
		method, path, ok := strings.Cut(route, " ")
		if !ok {
			continue
		}
		pathItem, ok := paths[path].(map[string]interface{})
		if !ok {
			continue
		}
		operation, ok := pathItem[method].(map[string]interface{})
		if !ok {
			continue
		}
		responses, ok := operation["responses"].(map[string]interface{})
		if !ok {
			responses = map[string]interface{}{}
			operation["responses"] = responses
		}
		response, ok := responses["200"].(map[string]interface{})
		if !ok {
			response = map[string]interface{}{"description": "Download response"}
			responses["200"] = response
		}
		ensureOpenAPIResponseHeader(response, "Content-Disposition", "Attachment filename for downloaded content.")
		ensureOpenAPIResponseHeader(response, "X-Content-Type-Options", "Set to nosniff for downloaded content.")
		if metadata.BackupChecksum {
			ensureOpenAPIResponseHeader(response, "X-MaClaw-Backup-SHA256", "SHA256 digest of the downloaded backup bytes.")
		}
		ensureOpenAPIDownloadContent(response, metadata.ContentTypes)
	}
}

func ensureOpenAPIDownloadContent(response map[string]interface{}, contentTypes []string) {
	if len(contentTypes) == 0 {
		return
	}
	content, ok := response["content"].(map[string]interface{})
	if !ok {
		content = map[string]interface{}{}
		response["content"] = content
	}
	for _, contentType := range contentTypes {
		if _, ok := content[contentType]; ok {
			continue
		}
		content[contentType] = map[string]interface{}{
			"schema": map[string]interface{}{"type": "string", "format": "binary"},
		}
	}
}

func addAuthErrorResponses(paths map[string]interface{}) {
	for path, rawPathItem := range paths {
		if !strings.HasPrefix(path, "/api/v1/data/") {
			continue
		}
		pathItem, ok := rawPathItem.(map[string]interface{})
		if !ok {
			continue
		}
		for _, method := range []string{"get", "post", "put", "patch", "delete"} {
			operation, ok := pathItem[method].(map[string]interface{})
			if !ok {
				continue
			}
			responses, ok := operation["responses"].(map[string]interface{})
			if !ok {
				responses = map[string]interface{}{}
				operation["responses"] = responses
			}
			if _, ok := responses["401"]; !ok {
				responses["401"] = errorResponseOpenAPISchema("Missing or invalid bearer token.")
			}
			if response := ensureOpenAPIResponseHeader(responses["401"], "WWW-Authenticate", "Bearer authentication challenge."); response != nil {
				responses["401"] = response
			}
			if response := ensureOpenAPIResponseHeader(responses["401"], "X-Content-Type-Options", "Set to nosniff for JSON API responses."); response != nil {
				responses["401"] = response
			}
			if _, ok := responses["403"]; !ok {
				responses["403"] = errorResponseOpenAPISchema("Authenticated principal is not allowed to access this resource.")
			}
			if response := ensureOpenAPIResponseHeader(responses["403"], "X-Content-Type-Options", "Set to nosniff for JSON API responses."); response != nil {
				responses["403"] = response
			}
		}
	}
}

func ensureOpenAPIResponseHeader(rawResponse interface{}, name, description string) map[string]interface{} {
	response, ok := rawResponse.(map[string]interface{})
	if !ok {
		if stringMap, ok := rawResponse.(map[string]string); ok {
			response = map[string]interface{}{}
			for key, value := range stringMap {
				response[key] = value
			}
		} else {
			return nil
		}
	}
	headers, ok := response["headers"].(map[string]interface{})
	if !ok {
		headers = map[string]interface{}{}
		response["headers"] = headers
	}
	if _, ok := headers[name]; ok {
		return response
	}
	headers[name] = map[string]interface{}{
		"description": description,
		"schema":      map[string]interface{}{"type": "string"},
	}
	return response
}

func addCursorParameters(paths map[string]interface{}, pathList []string) {
	for _, path := range pathList {
		pathItem, ok := paths[path].(map[string]interface{})
		if !ok {
			continue
		}
		operation, ok := pathItem["get"].(map[string]interface{})
		if !ok {
			continue
		}
		operation["parameters"] = appendPaginationParameters(operation["parameters"])
	}
}

func setOpenAPIGetResponses(paths map[string]interface{}, path string, responses map[string]interface{}) {
	pathItem, ok := paths[path].(map[string]interface{})
	if !ok {
		return
	}
	operation, ok := pathItem["get"].(map[string]interface{})
	if !ok {
		return
	}
	operation["responses"] = responses
}

func setOpenAPIPostResponses(paths map[string]interface{}, path string, responses map[string]interface{}) {
	pathItem, ok := paths[path].(map[string]interface{})
	if !ok {
		return
	}
	operation, ok := pathItem["post"].(map[string]interface{})
	if !ok {
		return
	}
	operation["responses"] = responses
}

func setOpenAPIPatchResponses(paths map[string]interface{}, path string, responses map[string]interface{}) {
	pathItem, ok := paths[path].(map[string]interface{})
	if !ok {
		return
	}
	operation, ok := pathItem["patch"].(map[string]interface{})
	if !ok {
		return
	}
	operation["responses"] = responses
}

func setOpenAPIDeleteResponses(paths map[string]interface{}, path string, responses map[string]interface{}) {
	pathItem, ok := paths[path].(map[string]interface{})
	if !ok {
		return
	}
	operation, ok := pathItem["delete"].(map[string]interface{})
	if !ok {
		return
	}
	operation["responses"] = responses
}

func setOpenAPIPostRequestBody(paths map[string]interface{}, path string, requestBody map[string]interface{}) {
	pathItem, ok := paths[path].(map[string]interface{})
	if !ok {
		return
	}
	operation, ok := pathItem["post"].(map[string]interface{})
	if !ok {
		return
	}
	operation["requestBody"] = requestBody
}

func setOpenAPIPutRequestBody(paths map[string]interface{}, path string, requestBody map[string]interface{}) {
	pathItem, ok := paths[path].(map[string]interface{})
	if !ok {
		return
	}
	operation, ok := pathItem["put"].(map[string]interface{})
	if !ok {
		return
	}
	operation["requestBody"] = requestBody
}

func setOpenAPIPatchRequestBody(paths map[string]interface{}, path string, requestBody map[string]interface{}) {
	pathItem, ok := paths[path].(map[string]interface{})
	if !ok {
		return
	}
	operation, ok := pathItem["patch"].(map[string]interface{})
	if !ok {
		return
	}
	operation["requestBody"] = requestBody
}

func addCursorPagination(paths map[string]interface{}, pathList []string) {
	for _, path := range pathList {
		pathItem, ok := paths[path].(map[string]interface{})
		if !ok {
			continue
		}
		operation, ok := pathItem["get"].(map[string]interface{})
		if !ok {
			continue
		}
		operation["parameters"] = appendPaginationParameters(operation["parameters"])
		operation["responses"] = listResponseOpenAPIResponses()
	}
}

func addQueryParameters(paths map[string]interface{}, paramsByPath map[string][]map[string]interface{}) {
	for path, params := range paramsByPath {
		pathItem, ok := paths[path].(map[string]interface{})
		if !ok {
			continue
		}
		operation, ok := pathItem["get"].(map[string]interface{})
		if !ok {
			continue
		}
		operation["parameters"] = appendNamedParameters(operation["parameters"], params)
	}
}

func addOperationQueryParameters(paths map[string]interface{}, path string, methods []string, params []map[string]interface{}) {
	pathItem, ok := paths[path].(map[string]interface{})
	if !ok {
		return
	}
	for _, method := range methods {
		operation, ok := pathItem[strings.ToLower(method)].(map[string]interface{})
		if !ok {
			continue
		}
		operation["parameters"] = appendNamedParameters(operation["parameters"], params)
	}
}

func appendNamedParameters(existing interface{}, additions []map[string]interface{}) []map[string]interface{} {
	out := []map[string]interface{}{}
	if params, ok := existing.([]map[string]interface{}); ok {
		out = append(out, params...)
	}
	seen := map[string]struct{}{}
	for _, param := range out {
		if name, ok := param["name"].(string); ok {
			seen[name] = struct{}{}
		}
	}
	for _, param := range additions {
		name, _ := param["name"].(string)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		out = append(out, param)
		seen[name] = struct{}{}
	}
	return out
}

func stringQueryParam(name, description string) map[string]interface{} {
	return typedQueryParam(name, description, map[string]interface{}{"type": "string"})
}

func boolQueryParam(name, description string) map[string]interface{} {
	description = strings.TrimSpace(description)
	if description != "" {
		description += " "
	}
	description += "Accepted values: true, false, 1, or 0."
	return typedQueryParam(name, description, map[string]interface{}{"type": "boolean"})
}

func typedQueryParam(name, description string, schema map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"name":        name,
		"in":          "query",
		"required":    false,
		"description": description,
		"schema":      schema,
	}
}

func bootstrapTemplatesOpenAPIRequestBody() map[string]interface{} {
	return map[string]interface{}{
		"required": false,
		"content": map[string]interface{}{
			"application/json": map[string]interface{}{
				"schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"template_ids": map[string]interface{}{
							"type":        "array",
							"description": "Optional explicit dataset template ids to bootstrap.",
							"items":       map[string]interface{}{"type": "string"},
						},
						"domains": map[string]interface{}{
							"type":        "array",
							"description": "Optional business domains to bootstrap, such as sales or finance.",
							"items":       map[string]interface{}{"type": "string"},
						},
						"skip_existing": map[string]interface{}{
							"type":        "boolean",
							"description": "Skip datasets that already exist instead of returning conflicts.",
						},
						"dry_run": map[string]interface{}{
							"type":        "boolean",
							"description": "Preview templates that would be created without writing datasets.",
						},
					},
				},
			},
		},
	}
}

func createFromTemplateOpenAPIRequestBody() map[string]interface{} {
	return map[string]interface{}{
		"required": false,
		"content": map[string]interface{}{
			"application/json": map[string]interface{}{
				"schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id":          map[string]interface{}{"type": "string", "description": "Optional dataset id override."},
						"domain":      map[string]interface{}{"type": "string", "description": "Optional business domain override."},
						"name":        map[string]interface{}{"type": "string", "description": "Optional dataset name override."},
						"title":       map[string]interface{}{"type": "string", "description": "Optional display title override."},
						"description": map[string]interface{}{"type": "string", "description": "Optional dataset description override."},
					},
				},
			},
		},
	}
}

func resolveBusinessIntentOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, []string{"query"}, map[string]interface{}{
		"query":  map[string]interface{}{"type": "string", "description": "Natural language business intent to match against domains and use cases."},
		"domain": map[string]interface{}{"type": "string", "description": "Optional business domain filter."},
		"limit":  map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 100},
	})
}

func upsertAppInstallationOpenAPIRequestBody() map[string]interface{} {
	roleBindingSchema := map[string]interface{}{
		"type":     "object",
		"required": []string{"object_role", "dataset_id"},
		"properties": map[string]interface{}{
			"object_role": map[string]interface{}{"type": "string", "description": "Semantic object role used by the app UI and workflow, for example expense_report."},
			"domain":      map[string]interface{}{"type": "string", "description": "Business domain for the bound dataset."},
			"dataset_id":  map[string]interface{}{"type": "string", "description": "Concrete tenant dataset id resolved for this object role."},
			"template_id": map[string]interface{}{"type": "string", "description": "Optional template id used to initialize the dataset."},
			"required":    map[string]interface{}{"type": "boolean", "description": "Whether the app cannot run without this binding."},
		},
	}
	return objectRequestBody(true, []string{"app_id"}, map[string]interface{}{
		"id":            map[string]interface{}{"type": "string", "description": "Optional installation id; must match app_id when provided."},
		"app_id":        map[string]interface{}{"type": "string", "description": "Installed MaClaw App id."},
		"blueprint_id":  map[string]interface{}{"type": "string", "description": "Optional app blueprint id."},
		"name":          map[string]interface{}{"type": "string", "description": "Human-readable app name."},
		"version":       map[string]interface{}{"type": "string", "description": "Installed app version."},
		"kind":          map[string]interface{}{"type": "string", "description": "App kind such as enterprise_approval_app, enterprise_normal_app, or tool_app."},
		"status":        map[string]interface{}{"type": "string", "description": "Installation status; defaults to installed."},
		"source":        map[string]interface{}{"type": "string", "description": "Distribution source, usually hub."},
		"role_bindings": map[string]interface{}{"type": "array", "items": roleBindingSchema, "description": "Semantic object_role to dataset bindings installed for this app."},
		"metadata":      appInstallationMetadataOpenAPISchema(),
	})
}
func appInstallationMetadataOpenAPISchema() map[string]interface{} {
	stringArray := map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}}
	dependencySchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id":               map[string]interface{}{"type": "string", "description": "Declared Skill dependency id."},
			"version":          map[string]interface{}{"type": "string", "description": "Required or installed dependency version."},
			"kind":             map[string]interface{}{"type": "string", "description": "Dependency kind such as runtime_skill, workflow_skill, connector_skill, or policy_skill."},
			"source":           map[string]interface{}{"type": "string", "description": "Dependency distribution source such as hub, market, enterprise_hub, local, or builtin."},
			"required":         map[string]interface{}{"type": "boolean", "description": "Whether the app is blocked when this dependency is unavailable."},
			"installed":        map[string]interface{}{"type": "boolean", "description": "Whether the dependency was present during install verification."},
			"health":           map[string]interface{}{"type": "string", "description": "Dependency health such as ready, missing, disabled, needs_setup, or unknown."},
			"action":           map[string]interface{}{"type": "string", "description": "Dependency plan action such as skip, installed, blocked, failed, optional_missing, or optional_unhealthy."},
			"app_ids":          stringArray,
			"installed_status": map[string]interface{}{"type": "string", "description": "Raw installed Skill status when present."},
			"message":          map[string]interface{}{"type": "string", "description": "Dependency verification or install detail."},
		},
	}
	versionSkillSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id":      map[string]interface{}{"type": "string"},
			"version": map[string]interface{}{"type": "string"},
			"kind":    map[string]interface{}{"type": "string"},
			"source":  map[string]interface{}{"type": "string"},
		},
	}
	versionApprovalBindingSchema := map[string]interface{}{
		"type":        "object",
		"description": "Approval binding version captured with the installed app so approval workflow dependencies can be checked and reproduced.",
		"properties": map[string]interface{}{
			"event":             map[string]interface{}{"type": "string", "description": "Approval trigger event bound by the app."},
			"object_role":       map[string]interface{}{"type": "string", "description": "Business object role covered by this approval binding."},
			"workflow_skill_id": map[string]interface{}{"type": "string", "description": "Workflow Skill dependency selected for the binding."},
			"workflow_version":  map[string]interface{}{"type": "string", "description": "Workflow Skill version selected for the binding."},
		},
	}
	workflowStatusMappingSchema := map[string]interface{}{
		"type":        "object",
		"description": "Approval workflow status mapping used by App Studio and approval instance views.",
		"properties": map[string]interface{}{
			"pending":       map[string]interface{}{"type": "string"},
			"approved":      map[string]interface{}{"type": "string"},
			"rejected":      map[string]interface{}{"type": "string"},
			"attention":     map[string]interface{}{"type": "string"},
			"requiresInput": map[string]interface{}{"type": "string"},
		},
	}
	workflowMappingSchema := map[string]interface{}{
		"type":        "object",
		"description": "Approval workflow node mapping for enterprise approval apps, preserved from App Studio workflow_mapping.",
		"properties": map[string]interface{}{
			"schema":        map[string]interface{}{"type": "string", "description": "Always maclaw.app.workflow.v1."},
			"submitNode":    map[string]interface{}{"type": "string", "description": "Workflow node used when the applicant submits the request."},
			"approvalNode":  map[string]interface{}{"type": "string", "description": "Workflow node shown in pending approval lanes."},
			"resultNode":    map[string]interface{}{"type": "string", "description": "Workflow node used for result feedback."},
			"attentionNode": map[string]interface{}{"type": "string", "description": "Workflow node used for needs-attention review."},
			"statusMapping": workflowStatusMappingSchema,
		},
	}
	workflowContractSchema := map[string]interface{}{
		"type":        "object",
		"description": "Approval workflow Skill contract captured for enterprise approval apps and used by install gates, run routing, and approval instance views.",
		"properties": map[string]interface{}{
			"schema":          map[string]interface{}{"type": "string", "description": "Always maclaw.app.workflow_contract.v1."},
			"workflowSkillId": map[string]interface{}{"type": "string", "description": "Workflow Skill dependency that runs the approval flow."},
			"workflowVersion": map[string]interface{}{"type": "string", "description": "Workflow Skill version expected by the app."},
			"objectRole":      map[string]interface{}{"type": "string", "description": "Business object role governed by this approval workflow."},
			"requiredInputs":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Input fields required before the workflow can be started."},
			"decisionOutputs": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Workflow decision outputs the app expects to receive."},
			"statusMapping":   workflowStatusMappingSchema,
		},
	}
	testProtocolSchema := map[string]interface{}{
		"type":        "object",
		"description": "Full App Studio test protocol used to reproduce the local app verification run.",
		"properties": map[string]interface{}{
			"schema":          map[string]interface{}{"type": "string", "description": "Always maclaw.app.test_protocol.v1."},
			"fingerprint":     map[string]interface{}{"type": "string", "description": "Stable fingerprint of the test protocol."},
			"sample_input":    map[string]interface{}{"type": "object", "description": "Sample input used by App Studio for the verification run."},
			"expected_output": map[string]interface{}{"type": "object", "description": "Expected output/result assertions for the verification run."},
			"required_roles":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Roles required to reproduce the test."},
			"required_scopes": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Runtime scopes required by the test."},
			"risk_level":      map[string]interface{}{"type": "string", "description": "Risk level evaluated during App Studio testing."},
		},
	}
	resultContractDeliverySchema := map[string]interface{}{
		"type":        "object",
		"description": "Delivery channels enabled by the app result contract.",
		"properties": map[string]interface{}{
			"inlineContent":  map[string]interface{}{"type": "boolean"},
			"artifacts":      map[string]interface{}{"type": "boolean"},
			"businessRecord": map[string]interface{}{"type": "boolean"},
			"notifications":  map[string]interface{}{"type": "boolean"},
		},
	}
	resultContractSchema := map[string]interface{}{
		"type":        "object",
		"description": "Declared MaClaw App output/result contract used by runtime rendering, publish governance, and install evidence.",
		"properties": map[string]interface{}{
			"schema":             map[string]interface{}{"type": "string", "description": "Always maclaw.app.result.v1."},
			"primary":            map[string]interface{}{"type": "string", "description": "Primary result channel such as content, document, business_status, or approval_result."},
			"types":              map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Result types the app promises to return."},
			"output_modes":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Tool output modes associated with the result contract."},
			"approval_decisions": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Approval decisions surfaced by enterprise approval apps."},
			"delivery":           resultContractDeliverySchema,
		},
	}
	dependencyVerificationSchema := map[string]interface{}{
		"type":        "object",
		"description": "Canonical dependency verification evidence for the installed app, scoped to the app id and used by install, run, and publish gates.",
		"properties": map[string]interface{}{
			"verified_at":                   map[string]interface{}{"type": "string", "format": "date-time"},
			"dependencies":                  map[string]interface{}{"type": "array", "items": dependencySchema, "description": "Per-dependency verification details after app-scoped alias and requirement checks."},
			"dependency_count":              map[string]interface{}{"type": "integer"},
			"has_missing_required":          map[string]interface{}{"type": "boolean"},
			"has_blocking_dependency":       map[string]interface{}{"type": "boolean"},
			"has_governance_review_issue":   map[string]interface{}{"type": "boolean"},
			"governance_review_issue_count": map[string]interface{}{"type": "integer"},
			"has_workflow_contract_issue":   map[string]interface{}{"type": "boolean"},
			"workflow_contract_issue_count": map[string]interface{}{"type": "integer"},
		},
	}
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": true,
		"description":          "MaClaw App install metadata used for audit, dependency health checks, dynamic UI layout, result contracts, and approval workflow traceability.",
		"properties": map[string]interface{}{
			"schema":                          map[string]interface{}{"type": "string", "description": "Installed app manifest schema, usually maclaw.app.v1."},
			"package_sha256":                  map[string]interface{}{"type": "string", "description": "SHA-256 fingerprint of the installed MaClaw App package."},
			"package_bytes":                   map[string]interface{}{"type": "integer", "description": "Installed MaClaw App package size in bytes."},
			"app_entry_version":               map[string]interface{}{"type": "string", "description": "Version of the app entry saved in the manifest."},
			"app_skill_id":                    map[string]interface{}{"type": "string", "description": "Super Skill or runtime Skill id that owns the app entry."},
			"app_skill_version":               map[string]interface{}{"type": "string", "description": "Installed app Skill version."},
			"app_skill_source":                map[string]interface{}{"type": "string", "description": "Source used to resolve the app Skill dependency, such as hub, enterprise_hub, or skillmarket."},
			"workflow_skill_ids":              stringArray,
			"workflow_skill_versions":         stringArray,
			"approval_binding_versions":       stringArray,
			"dependencies":                    map[string]interface{}{"type": "array", "items": dependencySchema, "description": "Backend-authoritative dependency verification snapshot."},
			"dependency_verification":         dependencyVerificationSchema,
			"dependency_count":                map[string]interface{}{"type": "integer", "description": "Number of dependency records captured for this app."},
			"has_missing_required_dependency": map[string]interface{}{"type": "boolean", "description": "True when a required dependency was missing in the verification snapshot."},
			"has_blocking_dependency":         map[string]interface{}{"type": "boolean", "description": "True when a required dependency was unavailable or blocked."},
			"version_snapshot": map[string]interface{}{
				"type":        "object",
				"description": "Resolved MaClaw App dependency version snapshot captured at install time for app Skill, workflow Skills, and approval bindings.",
				"properties": map[string]interface{}{
					"app_entry_version": map[string]interface{}{"type": "string"},
					"app_skill":         versionSkillSchema,
					"workflow_skills":   map[string]interface{}{"type": "array", "items": versionSkillSchema},
					"approval_bindings": map[string]interface{}{"type": "array", "items": versionApprovalBindingSchema},
				},
			},
			"workspace_layout":                            map[string]interface{}{"type": "object", "description": "Saved dynamic UI layout generated by App Studio, including preserved workspace_layout.regions placement metadata."},
			"workspace_layout_entry":                      map[string]interface{}{"type": "string"},
			"workspace_layout_template":                   map[string]interface{}{"type": "string"},
			"workspace_layout_density":                    map[string]interface{}{"type": "string"},
			"workspace_layout_primary_region":             map[string]interface{}{"type": "string", "description": "Primary App Studio region placement saved in the dynamic UI layout."},
			"workspace_layout_output_region":              map[string]interface{}{"type": "string", "description": "Output/result region placement saved in the dynamic UI layout."},
			"workspace_layout_region_count":               map[string]interface{}{"type": "integer", "description": "Number of layout regions preserved in workspace_layout.regions."},
			"workspace_layout_region_ids":                 map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Canonical region ids preserved from workspace_layout.regions."},
			"workspace_layout_navigation":                 stringArray,
			"workspace_layout_list_columns":               stringArray,
			"workflow_mapping":                            workflowMappingSchema,
			"workflow_contract":                           workflowContractSchema,
			"workflow_contract_skill_id":                  map[string]interface{}{"type": "string"},
			"workflow_contract_version":                   map[string]interface{}{"type": "string"},
			"workflow_contract_object_role":               map[string]interface{}{"type": "string"},
			"workflow_contract_required_inputs":           stringArray,
			"workflow_contract_decision_outputs":          stringArray,
			"workflow_contract_status_mapping":            workflowStatusMappingSchema,
			"result_contract":                             resultContractSchema,
			"result_contract_primary":                     map[string]interface{}{"type": "string"},
			"result_contract_types":                       stringArray,
			"result_contract_delivery":                    map[string]interface{}{"type": "string", "description": "Legacy or compact delivery mode summary for the result contract."},
			"result_contract_delivery_modes":              map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Enabled delivery channels derived from result_contract.delivery."},
			"result_contract_delivery_inline_content":     map[string]interface{}{"type": "boolean", "description": "True when inline text/content delivery is enabled."},
			"result_contract_delivery_artifacts":          map[string]interface{}{"type": "boolean", "description": "True when artifact/file delivery is enabled."},
			"result_contract_delivery_business_record":    map[string]interface{}{"type": "boolean", "description": "True when business record delivery is enabled."},
			"result_contract_delivery_notifications":      map[string]interface{}{"type": "boolean", "description": "True when notification delivery is enabled."},
			"test_evidence":                               map[string]interface{}{"type": "object", "description": "Canonical local test evidence captured before market upload or install, including full outputs, artifacts, approval instance evidence, and result payload when present."},
			"test_evidence_run_id":                        map[string]interface{}{"type": "string"},
			"test_evidence_verified_at":                   map[string]interface{}{"type": "string", "format": "date-time"},
			"test_evidence_definition_fingerprint":        map[string]interface{}{"type": "string", "description": "Fingerprint of the tested app definition."},
			"test_evidence_test_protocol":                 testProtocolSchema,
			"test_evidence_test_protocol_fingerprint":     map[string]interface{}{"type": "string", "description": "Fingerprint of the App Studio test protocol used to verify the package."},
			"test_evidence_artifact_present":              map[string]interface{}{"type": "boolean", "description": "Whether test execution produced at least one artifact."},
			"test_evidence_artifact_name":                 map[string]interface{}{"type": "string"},
			"test_evidence_artifact_count":                map[string]interface{}{"type": "integer"},
			"test_evidence_output_count":                  map[string]interface{}{"type": "integer"},
			"test_evidence_primary_result":                map[string]interface{}{"type": "string", "description": "Primary result channel observed during test execution."},
			"test_evidence_outputs":                       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object"}, "description": "Full dynamic output blocks produced by the tested MaClaw App, normalized from test_evidence.outputs or promoted from approval_instance.outputs."},
			"test_evidence_artifacts":                     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object"}, "description": "Full artifact descriptors produced by the tested MaClaw App, normalized from test_evidence.artifacts or promoted from approval_instance.artifacts."},
			"test_evidence_result_payload":                map[string]interface{}{"type": "object", "description": "Structured result payload returned by the app or workflow Skill, normalized from test_evidence.result_payload or promoted from approval_instance.result_payload."},
			"test_evidence_approval_instance":             map[string]interface{}{"type": "object", "description": "Full approval instance evidence captured for enterprise approval apps."},
			"test_evidence_approval_instance_id":          map[string]interface{}{"type": "string", "description": "Workflow or approval instance id used to locate the tested approval."},
			"test_evidence_approval_id":                   map[string]interface{}{"type": "string", "description": "DataSrv approval id returned while syncing approval evidence."},
			"test_evidence_record_id":                     map[string]interface{}{"type": "string", "description": "Business record id associated with the approval evidence."},
			"test_evidence_approval_status":               map[string]interface{}{"type": "string", "description": "Approval result status observed in test evidence."},
			"test_evidence_approval_view_verified":        map[string]interface{}{"type": "boolean", "description": "Whether App Studio verified at least one approval lane containing the instance."},
			"test_evidence_result_coverage_ok":            map[string]interface{}{"type": "boolean", "description": "Whether test outputs cover the declared result contract."},
			"test_evidence_result_coverage_primary":       map[string]interface{}{"type": "string"},
			"test_evidence_covered_types":                 stringArray,
			"test_evidence_missing_types":                 stringArray,
			"test_evidence_dependency_verified_at":        map[string]interface{}{"type": "string", "format": "date-time"},
			"test_evidence_dependency_count":              map[string]interface{}{"type": "integer"},
			"test_evidence_dependency_missing_required":   map[string]interface{}{"type": "boolean"},
			"test_evidence_dependency_blocking":           map[string]interface{}{"type": "boolean"},
			"test_evidence_governance_review_issue":       map[string]interface{}{"type": "boolean", "description": "True when local dependency verification found blocking app governance review issues."},
			"test_evidence_governance_review_issue_count": map[string]interface{}{"type": "integer", "description": "Number of governance review issues captured in local dependency verification."},
			"test_evidence_workflow_contract_issue":       map[string]interface{}{"type": "boolean", "description": "True when local workflow contract verification found blocking issues."},
			"test_evidence_workflow_contract_issue_count": map[string]interface{}{"type": "integer", "description": "Number of workflow contract issues captured in local dependency verification."},
			"governance_status":                           map[string]interface{}{"type": "string"},
			"governance_risk_level":                       map[string]interface{}{"type": "string"},
		},
	}
}
func resolveObjectRoleOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, []string{"object_role"}, map[string]interface{}{
		"app_id":              map[string]interface{}{"type": "string", "description": "Optional MaClaw app id used to select an app-specific role binding."},
		"blueprint_id":        map[string]interface{}{"type": "string", "description": "Optional blueprint id used to select a blueprint-specific role binding."},
		"object_role":         map[string]interface{}{"type": "string", "description": "Semantic object role such as expense_report, employee, inventory_item, or inventory_movement."},
		"require_initialized": map[string]interface{}{"type": "boolean", "description": "When true, return 404 until the mapped dataset exists for the current tenant."},
	})
}

func executeBusinessActionOpenAPIRequestBody() map[string]interface{} {
	return map[string]interface{}{
		"required": true,
		"content": map[string]interface{}{
			"application/json": map[string]interface{}{
				"schema": map[string]interface{}{
					"type":     "object",
					"required": []string{"data"},
					"properties": map[string]interface{}{
						"record_id":       map[string]interface{}{"type": "string", "description": "Optional business record id to create or update."},
						"idempotency_key": map[string]interface{}{"type": "string", "description": "Stable idempotency key for commit operations."},
						"title":           map[string]interface{}{"type": "string", "description": "Optional record title."},
						"tags":            map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
						"data":            map[string]interface{}{"type": "object", "description": "Business payload validated against the action target dataset."},
						"occurred_at":     map[string]interface{}{"type": "string", "format": "date-time", "description": "Optional source event timestamp."},
						"dry_run":         map[string]interface{}{"type": "boolean", "description": "Validate and preview without writing when true."},
					},
				},
			},
		},
	}
}

func executeBusinessActionOpenAPIResponses() map[string]interface{} {
	return map[string]interface{}{
		"200": jsonObjectOpenAPIResponse("Business action execution result with MaClaw App result package.", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action":          map[string]interface{}{"type": "object", "description": "Business action definition that was executed."},
				"dry_run":         map[string]interface{}{"type": "boolean", "description": "True when the action was validated without writing."},
				"valid":           map[string]interface{}{"type": "boolean", "description": "Validation status for dry-run and committed action results."},
				"validation":      map[string]interface{}{"type": "object", "description": "Record validation result when available."},
				"preview":         map[string]interface{}{"type": "object", "description": "Preview business record for dry-run execution."},
				"event":           map[string]interface{}{"type": "object", "description": "Committed data event result when dry_run is false."},
				"rules":           map[string]interface{}{"type": "object", "description": "Business rule and governance gate evaluation."},
				"primary_result":  map[string]interface{}{"type": "string", "description": "Primary MaClaw App result channel, usually business_record."},
				"business_status": map[string]interface{}{"type": "string", "description": "Business-facing action status such as dry_run_valid, dry_run_invalid, or committed event status."},
				"result_status":   map[string]interface{}{"type": "string", "description": "Machine-readable result status for MaClaw App run evidence."},
				"result_payload":  map[string]interface{}{"type": "object", "description": "Structured MaClaw App result payload with business_status, business_record, dataset_id, record_id, and action metadata."},
				"outputs":         map[string]interface{}{"type": "array", "description": "Structured output blocks for GUI run history and app evidence.", "items": map[string]interface{}{"type": "object"}},
				"artifacts":       map[string]interface{}{"type": "array", "description": "Generated artifact descriptors; empty when no files were produced.", "items": map[string]interface{}{"type": "object"}},
			},
		}),
	}
}

func evaluateBusinessRulesOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(false, nil, map[string]interface{}{
		"domain":             map[string]interface{}{"type": "string", "description": "Optional business domain filter."},
		"dataset_id":         map[string]interface{}{"type": "string", "description": "Optional dataset id filter."},
		"business_action_id": map[string]interface{}{"type": "string", "description": "Optional business action id filter."},
		"record_id":          map[string]interface{}{"type": "string", "description": "Optional record id to evaluate."},
		"dry_run":            map[string]interface{}{"type": "boolean", "description": "Evaluate governance gates for a dry-run workflow."},
		"data":               map[string]interface{}{"type": "object", "description": "Optional record payload to evaluate against business rules."},
	})
}

func ingestEventOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, []string{"source", "event_type", "dataset_id"}, dataEventOpenAPIProperties())
}

func dataEventOpenAPIProperties() map[string]interface{} {
	return map[string]interface{}{
		"source":             map[string]interface{}{"type": "string", "description": "External source system name."},
		"event_type":         map[string]interface{}{"type": "string", "description": "Source event type."},
		"operation":          map[string]interface{}{"type": "string", "description": "Raw dataset operation such as upsert_record or delete_record."},
		"business_action_id": map[string]interface{}{"type": "string", "description": "Preferred business action id for connector-facing events."},
		"dataset_id":         map[string]interface{}{"type": "string", "description": "Target dataset id for raw event ingestion."},
		"record_id":          map[string]interface{}{"type": "string", "description": "Business record id."},
		"idempotency_key":    map[string]interface{}{"type": "string", "description": "Stable idempotency key for commit operations."},
		"title":              map[string]interface{}{"type": "string", "description": "Optional record title."},
		"tags":               map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		"data":               map[string]interface{}{"type": "object", "description": "Event payload or record data."},
		"occurred_at":        map[string]interface{}{"type": "string", "format": "date-time", "description": "Optional source event timestamp."},
		"dry_run":            map[string]interface{}{"type": "boolean", "description": "Validate and preview without writing when true."},
	}
}

func dataEventOpenAPISchema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"required":   []string{"source", "event_type", "dataset_id"},
		"properties": dataEventOpenAPIProperties(),
	}
}

func upsertConnectorOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, []string{"name"}, map[string]interface{}{
		"id":                 map[string]interface{}{"type": "string", "description": "Optional connector id when creating a connector."},
		"domain":             map[string]interface{}{"type": "string", "description": "Business domain served by this connector."},
		"name":               map[string]interface{}{"type": "string", "description": "Human-readable connector name."},
		"kind":               map[string]interface{}{"type": "string", "description": "Integration kind or source system."},
		"base_url":           map[string]interface{}{"type": "string", "description": "Optional source system base URL."},
		"auth_type":          map[string]interface{}{"type": "string", "description": "Connector authentication mode."},
		"token_ref":          map[string]interface{}{"type": "string", "description": "Reference to a managed secret or token."},
		"enabled":            map[string]interface{}{"type": "boolean", "description": "Enable or disable ingestion for this connector."},
		"subscribed_actions": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Business action ids this connector can publish."},
		"config":             map[string]interface{}{"type": "object", "description": "Connector-specific mapping and runtime configuration."},
	})
}

func connectorReadinessOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(false, nil, map[string]interface{}{
		"sample_event": dataEventOpenAPISchema(),
	})
}

func connectorSyncPlanOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(false, nil, map[string]interface{}{
		"sample_event":      dataEventOpenAPISchema(),
		"first_page_events": map[string]interface{}{"type": "array", "items": dataEventOpenAPISchema(), "description": "Optional first page of source events used to dry-run the sync plan."},
		"page_size":         map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 500, "description": "Preferred source page size for the generated plan."},
		"cursor":            map[string]interface{}{"type": "string", "description": "Current external source cursor or checkpoint."},
	})
}

func updateConnectorSyncStateOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, nil, updateConnectorSyncStateOpenAPIProperties())
}

func updateConnectorSyncStateOpenAPIProperties() map[string]interface{} {
	return map[string]interface{}{
		"status":         map[string]interface{}{"type": "string", "description": "Sync status: idle, running, success, failed, or paused."},
		"cursor":         map[string]interface{}{"type": "string", "description": "External source cursor to persist."},
		"checkpoint":     map[string]interface{}{"type": "object", "description": "Arbitrary connector checkpoint data."},
		"last_error":     map[string]interface{}{"type": "string", "description": "Last sync error message."},
		"message":        map[string]interface{}{"type": "string", "description": "Human-readable sync status message."},
		"synced_records": map[string]interface{}{"type": "integer", "minimum": 0, "description": "Number of records synced so far."},
		"started_at":     map[string]interface{}{"type": "string", "format": "date-time", "description": "Sync start time as RFC3339/RFC3339Nano timestamp or YYYY-MM-DD."},
		"finished_at":    map[string]interface{}{"type": "string", "format": "date-time", "description": "Sync finish time as RFC3339/RFC3339Nano timestamp or YYYY-MM-DD."},
	}
}

func connectorSyncBatchOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, []string{"events"}, map[string]interface{}{
		"events":        map[string]interface{}{"type": "array", "items": dataEventOpenAPISchema(), "description": "Source events to ingest as one connector sync batch."},
		"dry_run":       map[string]interface{}{"type": "boolean", "description": "Validate and preview the batch without writing records."},
		"stop_on_error": map[string]interface{}{"type": "boolean", "description": "Stop processing after the first failed event."},
		"sync_state":    map[string]interface{}{"type": "object", "properties": updateConnectorSyncStateOpenAPIProperties(), "description": "Optional sync state update to apply after the batch."},
	})
}

func patchConnectorConfigOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, []string{"patch"}, map[string]interface{}{
		"patch":   map[string]interface{}{"type": "object", "description": "Partial connector config to merge with the existing config."},
		"dry_run": map[string]interface{}{"type": "boolean", "description": "Preview the patched config without saving it."},
	})
}

func suggestConnectorMappingOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, []string{"business_action_id", "sample_data"}, map[string]interface{}{
		"business_action_id": map[string]interface{}{"type": "string", "description": "Business action whose dataset schema should guide the mapping."},
		"sample_data":        map[string]interface{}{"type": "object", "description": "Representative source payload used to infer field mappings."},
	})
}

func resolveDeadLetterOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(false, nil, map[string]interface{}{
		"resolution": map[string]interface{}{"type": "string", "description": "Resolution note saved with the dead-letter event."},
	})
}

func batchImportOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, []string{"records"}, map[string]interface{}{
		"records": map[string]interface{}{
			"type":        "array",
			"maxItems":    maxBatchImportRecords,
			"description": "Records to create or update in one controlled batch. At most 1000 records are accepted.",
			"items": map[string]interface{}{
				"type":     "object",
				"required": []string{"data"},
				"properties": map[string]interface{}{
					"id":        map[string]interface{}{"type": "string"},
					"title":     map[string]interface{}{"type": "string"},
					"tags":      map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					"data":      map[string]interface{}{"type": "object"},
					"source_id": map[string]interface{}{"type": "string"},
				},
			},
		},
		"dry_run": map[string]interface{}{"type": "boolean", "description": "Validate without writing when true."},
	})
}

func createDatasetOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, []string{"domain", "name"}, map[string]interface{}{
		"id":          map[string]interface{}{"type": "string", "description": "Optional dataset id override."},
		"domain":      map[string]interface{}{"type": "string", "description": "Business domain for the dataset."},
		"name":        map[string]interface{}{"type": "string", "description": "Dataset machine name."},
		"title":       map[string]interface{}{"type": "string", "description": "Optional display title."},
		"description": map[string]interface{}{"type": "string", "description": "Optional dataset description."},
	})
}

func updateDatasetOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, nil, map[string]interface{}{
		"title":       map[string]interface{}{"type": "string", "description": "Updated display title."},
		"description": map[string]interface{}{"type": "string", "description": "Updated dataset description."},
	})
}

func fieldDefinitionOpenAPISchema() map[string]interface{} {
	return map[string]interface{}{
		"type":     "object",
		"required": []string{"key", "type"},
		"properties": map[string]interface{}{
			"id":        map[string]interface{}{"type": "string"},
			"key":       map[string]interface{}{"type": "string", "description": "Record data key managed by this field."},
			"type":      map[string]interface{}{"type": "string", "description": "Field value type such as string, number, boolean, date, or object."},
			"title":     map[string]interface{}{"type": "string"},
			"required":  map[string]interface{}{"type": "boolean"},
			"indexed":   map[string]interface{}{"type": "boolean"},
			"sensitive": map[string]interface{}{"type": "boolean"},
			"config":    map[string]interface{}{"type": "object"},
		},
	}
}

func upsertFieldsOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, []string{"fields"}, map[string]interface{}{
		"fields": map[string]interface{}{"type": "array", "items": fieldDefinitionOpenAPISchema(), "description": "Full field definition set for the dataset."},
	})
}

func createRecordOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, []string{"data"}, map[string]interface{}{
		"id":        map[string]interface{}{"type": "string", "description": "Optional record id override."},
		"title":     map[string]interface{}{"type": "string", "description": "Optional record title."},
		"tags":      map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		"data":      map[string]interface{}{"type": "object", "description": "Record data validated against dataset fields."},
		"source_id": map[string]interface{}{"type": "string", "description": "Optional external source id."},
	})
}

func updateRecordOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, nil, map[string]interface{}{
		"title": map[string]interface{}{"type": "string", "description": "Optional updated record title."},
		"tags":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		"data":  map[string]interface{}{"type": "object", "description": "Partial record data update."},
	})
}

func validateRecordOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, []string{"data"}, map[string]interface{}{
		"data": map[string]interface{}{"type": "object", "description": "Record data to validate against dataset fields."},
	})
}

func restoreRecordOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, []string{"confirm"}, map[string]interface{}{
		"confirm":     map[string]interface{}{"type": "boolean", "description": "Required confirmation before restoring a record revision."},
		"revision_id": map[string]interface{}{"type": "string", "description": "Optional explicit revision id to restore."},
		"reason":      map[string]interface{}{"type": "string", "description": "Audit reason for the restore."},
	})
}

func bulkUpdateOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, nil, map[string]interface{}{
		"query":   map[string]interface{}{"type": "object", "description": "Record query selecting candidates to update."},
		"set":     map[string]interface{}{"type": "object", "description": "Fields to set on matched records."},
		"unset":   map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		"title":   map[string]interface{}{"type": "string"},
		"tags":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		"limit":   map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 500},
		"dry_run": map[string]interface{}{"type": "boolean", "description": "Validate without writing when true."},
		"confirm": map[string]interface{}{"type": "boolean", "description": "Required for non-dry-run bulk changes."},
		"reason":  map[string]interface{}{"type": "string", "description": "Audit reason for the controlled operation."},
	})
}

func bulkDeleteOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, nil, map[string]interface{}{
		"query":   map[string]interface{}{"type": "object", "description": "Record query selecting candidates to delete."},
		"limit":   map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 500},
		"dry_run": map[string]interface{}{"type": "boolean", "description": "Preview deletion without writing when true."},
		"confirm": map[string]interface{}{"type": "boolean", "description": "Required for non-dry-run deletion."},
		"reason":  map[string]interface{}{"type": "string", "description": "Audit reason for the controlled operation."},
	})
}

func importCSVOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, []string{"csv"}, map[string]interface{}{
		"csv":     map[string]interface{}{"type": "string", "description": "CSV document text. text/csv and text/plain bodies are also accepted. At most 1000 data rows are accepted."},
		"dry_run": map[string]interface{}{"type": "boolean", "description": "Validate without writing when true."},
	})
}

func importJSONLOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, []string{"jsonl"}, map[string]interface{}{
		"jsonl":   map[string]interface{}{"type": "string", "description": "JSON Lines document text. application/x-ndjson, application/jsonl, and text/plain bodies are also accepted. At most 1000 records are accepted."},
		"dry_run": map[string]interface{}{"type": "boolean", "description": "Validate without writing when true."},
	})
}

func schemaProposalOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, []string{"sample_data"}, map[string]interface{}{
		"sample_data": map[string]interface{}{"type": "object", "description": "Sample business record used to infer missing fields."},
		"reason":      map[string]interface{}{"type": "string", "description": "Audit reason for the proposal."},
	})
}

func applySchemaProposalOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, []string{"fields", "confirm"}, map[string]interface{}{
		"proposal_id": map[string]interface{}{"type": "string", "description": "Optional proposal id to apply."},
		"fields":      map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object"}},
		"confirm":     map[string]interface{}{"type": "boolean", "description": "Required confirmation for schema changes."},
		"reason":      map[string]interface{}{"type": "string", "description": "Audit reason for the schema change."},
	})
}

func aggregateOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, nil, map[string]interface{}{
		"filter":     map[string]interface{}{"type": "object", "description": "Structured filter expression."},
		"group_by":   map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		"metrics":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object"}},
		"sort":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object"}},
		"limit":      map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 500},
		"scan_limit": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 5000},
	})
}

func runQualityCheckOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(false, nil, map[string]interface{}{
		"checks":           map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		"limit":            map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 5000},
		"include_warnings": map[string]interface{}{"type": "boolean", "description": "Include warning-level findings when true."},
	})
}

func accessCheckOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, []string{"resource_type"}, map[string]interface{}{
		"key_id":        map[string]interface{}{"type": "string", "description": "Optional managed API key id to evaluate instead of the caller principal."},
		"resource_type": map[string]interface{}{"type": "string", "description": "Resource type such as business_action, report, dashboard, dataset, domain, admin, or sensitive."},
		"resource_id":   map[string]interface{}{"type": "string", "description": "Optional concrete resource id."},
	})
}

func createAdminAccountOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, []string{"username", "password"}, map[string]interface{}{
		"tenant_id":    map[string]interface{}{"type": "string"},
		"admin_scope":  map[string]interface{}{"type": "string", "enum": []string{"global", "tenant"}},
		"username":     map[string]interface{}{"type": "string"},
		"password":     map[string]interface{}{"type": "string", "format": "password"},
		"display_name": map[string]interface{}{"type": "string"},
		"role":         map[string]interface{}{"type": "string", "enum": []string{"data_admin", "data_auditor", "data_user"}},
	})
}

func updateAdminAccountOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, nil, map[string]interface{}{
		"display_name": map[string]interface{}{"type": "string"},
		"admin_scope":  map[string]interface{}{"type": "string", "enum": []string{"global", "tenant"}},
		"role":         map[string]interface{}{"type": "string", "enum": []string{"data_admin", "data_auditor", "data_user"}},
		"enabled":      map[string]interface{}{"type": "boolean"},
	})
}

func syncHubTenantsOpenAPIRequestBody() map[string]interface{} {
	tenantSchema := map[string]interface{}{"type": "object", "properties": map[string]interface{}{
		"id":                  map[string]interface{}{"type": "string"},
		"hub_tenant_id":       map[string]interface{}{"type": "string"},
		"slug":                map[string]interface{}{"type": "string"},
		"name":                map[string]interface{}{"type": "string"},
		"status":              map[string]interface{}{"type": "string"},
		"primary_domain":      map[string]interface{}{"type": "string"},
		"domains":             map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		"virtual_mail_domain": map[string]interface{}{"type": "string"},
	}}
	return objectRequestBody(true, []string{"tenants"}, map[string]interface{}{
		"source":  map[string]interface{}{"type": "string", "description": "Tenant source, normally hub."},
		"tenants": map[string]interface{}{"type": "array", "items": tenantSchema},
	})
}

func saveHubRegistrationOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(false, nil, map[string]interface{}{
		"hub_base_url":        map[string]interface{}{"type": "string", "description": "Hub base URL. Omit to preserve the existing value; send an empty string to clear it."},
		"platform_id":         map[string]interface{}{"type": "string", "description": "Platform id registered with Hub. Omit to preserve the existing value."},
		"platform_name":       map[string]interface{}{"type": "string", "description": "Human-readable platform name. Omit to preserve the existing value."},
		"callback_base_url":   map[string]interface{}{"type": "string", "description": "Callback base URL. Omit to preserve the existing value; send an empty string to clear it."},
		"virtual_mail_domain": map[string]interface{}{"type": "string", "description": "Virtual mail domain used by Hub tenant sync. Omit to preserve the existing value; send an empty string to clear it."},
	})
}

func createAPIKeyPolicyOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, nil, apiKeyPolicyProperties(true))
}

func updateAPIKeyPolicyOpenAPIRequestBody() map[string]interface{} {
	props := apiKeyPolicyProperties(false)
	props["enabled"] = map[string]interface{}{"type": "boolean", "description": "Enable or disable this managed API key."}
	props["user_id"].(map[string]interface{})["description"] = "Optional owner or agent id. Omit to preserve the existing value; send an empty string to clear it."
	props["note"].(map[string]interface{})["description"] = "Operator note. Omit to preserve the existing value; send an empty string to clear it."
	props["expires_at"].(map[string]interface{})["description"] = "Expiration timestamp. Omit to preserve the existing value; send an empty string to clear expiration."
	props["allow_raw_data"].(map[string]interface{})["description"] = "Whether raw dataset APIs are allowed. Omit to preserve the existing value; send false to disable."
	props["allow_sensitive"].(map[string]interface{})["description"] = "Whether sensitive fields are allowed. Omit to preserve the existing value; send false to disable."
	props["allow_admin"].(map[string]interface{})["description"] = "Whether admin operations are allowed. Omit to preserve the existing value; send false to disable."
	return objectRequestBody(true, nil, props)
}

func apiKeyPolicyProperties(includeSecret bool) map[string]interface{} {
	props := map[string]interface{}{
		"id":                 map[string]interface{}{"type": "string"},
		"user_id":            map[string]interface{}{"type": "string"},
		"role":               map[string]interface{}{"type": "string"},
		"allowed_domains":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		"allowed_datasets":   map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		"allowed_actions":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		"allowed_views":      map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		"allowed_reports":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		"allowed_dashboards": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		"allow_raw_data":     map[string]interface{}{"type": "boolean"},
		"allow_sensitive":    map[string]interface{}{"type": "boolean"},
		"allow_admin":        map[string]interface{}{"type": "boolean"},
		"note":               map[string]interface{}{"type": "string"},
		"expires_at":         map[string]interface{}{"type": "string", "format": "date-time"},
	}
	if includeSecret {
		props["key"] = map[string]interface{}{"type": "string", "description": "Optional caller-provided secret; generated when omitted."}
	}
	return props
}

func runMaintenanceOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(false, nil, map[string]interface{}{
		"tasks": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Maintenance tasks such as integrity_check, vacuum, or optimize."},
	})
}

func createBackupOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(false, nil, map[string]interface{}{
		"name": map[string]interface{}{"type": "string"},
		"note": map[string]interface{}{"type": "string"},
	})
}

func restoreBackupOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, []string{"confirm"}, map[string]interface{}{
		"confirm": map[string]interface{}{"type": "boolean", "description": "Required confirmation for restore."},
		"reason":  map[string]interface{}{"type": "string", "description": "Audit reason for the restore."},
	})
}

func createOperationPlanOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, []string{"operation", "request"}, map[string]interface{}{
		"dataset_id": map[string]interface{}{"type": "string"},
		"operation":  map[string]interface{}{"type": "string"},
		"summary":    map[string]interface{}{"type": "string"},
		"risk_level": map[string]interface{}{"type": "string", "description": "Risk level: low, medium, high, or critical. Defaults to medium when omitted."},
		"request":    map[string]interface{}{"type": "object", "description": "Operation-specific request payload to preview and later apply. query.limit or top-level limit must be at most 100."},
	})
}

func reviewDecisionOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, []string{"decision"}, reviewDecisionOpenAPIProperties())
}

func reviewDecisionOpenAPIProperties() map[string]interface{} {
	return map[string]interface{}{
		"decision": map[string]interface{}{"type": "string", "description": "Review decision such as approved or rejected."},
		"reason":   map[string]interface{}{"type": "string", "description": "Review reason for audit trail."},
	}
}

func reviewRecordApprovalOpenAPIRequestBody() map[string]interface{} {
	properties := reviewDecisionOpenAPIProperties()
	for key, value := range recordApprovalWorkflowOpenAPIProperties() {
		properties[key] = value
	}
	for key, value := range recordApprovalResultPackageOpenAPIProperties() {
		properties[key] = value
	}
	return objectRequestBody(true, []string{"decision"}, properties)
}

func applyOperationPlanOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(true, []string{"confirm"}, map[string]interface{}{
		"confirm": map[string]interface{}{"type": "boolean", "description": "Required confirmation before applying the operation plan."},
		"reason":  map[string]interface{}{"type": "string", "description": "Audit reason for applying the operation plan."},
	})
}

func createRecordApprovalOpenAPIRequestBody() map[string]interface{} {
	properties := map[string]interface{}{
		"app_id":       map[string]interface{}{"type": "string", "description": "MaClaw App id that owns this approval instance."},
		"blueprint_id": map[string]interface{}{"type": "string", "description": "Optional MaClaw App blueprint id that produced this approval instance."},
		"object_role":  map[string]interface{}{"type": "string", "description": "Semantic object role for the approved business record, such as expense_report."},
		"kind":         map[string]interface{}{"type": "string"},
		"priority":     map[string]interface{}{"type": "string", "description": "Approval priority: low, medium, high, or critical. Defaults to medium when omitted."},
		"summary":      map[string]interface{}{"type": "string"},
		"request":      recordApprovalRequestOpenAPISchema(),
		"assigned_to":  map[string]interface{}{"type": "string"},
		"due_at":       map[string]interface{}{"type": "string", "format": "date-time"},
	}
	for key, value := range recordApprovalWorkflowOpenAPIProperties() {
		properties[key] = value
	}
	for key, value := range recordApprovalResultPackageOpenAPIProperties() {
		properties[key] = value
	}
	return objectRequestBody(false, nil, properties)
}

func recordApprovalWorkflowOpenAPIProperties() map[string]interface{} {
	return map[string]interface{}{
		"approval_workflow_id":  map[string]interface{}{"type": "string", "description": "Stable business approval workflow id selected by the MaClaw App binding."},
		"trigger_event":         map[string]interface{}{"type": "string", "description": "Business event that started the approval workflow."},
		"submitted_by":          map[string]interface{}{"type": "string", "description": "User id that submitted the business approval request."},
		"current_assignee":      map[string]interface{}{"type": "string", "description": "Current assignee user, role, or queue shown by app approval views."},
		"current_assignee_type": map[string]interface{}{"type": "string", "description": "Assignee kind such as user, role, department, queue, or ve."},
		"from_status":           map[string]interface{}{"type": "string", "description": "Business status before this approval transition."},
		"to_status":             map[string]interface{}{"type": "string", "description": "Business status after this approval transition."},
		"workflow_skill_id":     map[string]interface{}{"type": "string", "description": "Workflow skill that owns the approval run."},
		"workflow_version":      map[string]interface{}{"type": "string", "description": "Workflow skill version."},
		"workflow_instance_id":  map[string]interface{}{"type": "string", "description": "External workflow instance id."},
		"workflow_node_id":      map[string]interface{}{"type": "string", "description": "Current or completed workflow node id."},
		"workflow_node_ids":     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Ordered workflow node ids that describe the current approval path or active node set."},
		"workflow_decision_id":  map[string]interface{}{"type": "string", "description": "Decision event id from the workflow skill."},
		"detail_url":            map[string]interface{}{"type": "string", "description": "URL or deep link for opening the full workflow approval instance trace."},
		"business_status":       map[string]interface{}{"type": "string", "description": "Business-facing state produced by the workflow."},
		"result_status":         map[string]interface{}{"type": "string", "description": "Machine-readable output state produced by the workflow."},
	}
}

func recordApprovalRequestOpenAPISchema() map[string]interface{} {
	stringProp := func(description string) map[string]interface{} {
		return map[string]interface{}{"type": "string", "description": description}
	}
	return map[string]interface{}{
		"type":        "object",
		"description": "MaClaw App approval request context persisted with the approval instance. Both snake_case and camelCase aliases are documented for enterprise-system interoperability.",
		"properties": map[string]interface{}{
			"approval_instance_id":  stringProp("Stable app/workflow approval instance id."),
			"approvalInstanceId":    stringProp("CamelCase alias of approval_instance_id."),
			"workflowInstanceId":    stringProp("External workflow instance id alias used by workflow engines."),
			"app_id":                stringProp("MaClaw App id."),
			"appID":                 stringProp("CamelCase alias of app_id."),
			"maclaw_app_id":         stringProp("MaClaw App id alias used by older install evidence."),
			"blueprint_id":          stringProp("MaClaw App blueprint id."),
			"blueprintID":           stringProp("CamelCase alias of blueprint_id."),
			"object_role":           stringProp("Semantic business object role."),
			"objectRole":            stringProp("CamelCase alias of object_role."),
			"business_object_role":  stringProp("Business object role alias."),
			"businessObjectRole":    stringProp("CamelCase alias of business_object_role."),
			"approval_event":        stringProp("Approval trigger event."),
			"approvalEvent":         stringProp("CamelCase alias of approval_event."),
			"trigger_event":         stringProp("Business trigger event alias."),
			"triggerEvent":          stringProp("CamelCase alias of trigger_event."),
			"business_entity":       stringProp("Business entity label shown in approval workspaces."),
			"businessEntity":        stringProp("CamelCase alias of business_entity."),
			"business_action":       stringProp("Business action label shown in approval workspaces."),
			"businessAction":        stringProp("CamelCase alias of business_action."),
			"business_note":         stringProp("Business note or summary shown in approval workspaces."),
			"businessNote":          stringProp("CamelCase alias of business_note."),
			"submitted_by":          stringProp("Submitting user id."),
			"submittedBy":           stringProp("CamelCase alias of submitted_by."),
			"owner":                 stringProp("Owner/requester alias."),
			"applicant":             stringProp("Applicant alias."),
			"assigned_to":           stringProp("Assigned approver user, role, or queue."),
			"assignedTo":            stringProp("CamelCase alias of assigned_to."),
			"current_assignee":      stringProp("Current approver user, role, or queue."),
			"currentAssignee":       stringProp("CamelCase alias of current_assignee."),
			"current_assignee_type": stringProp("Current assignee type such as user, role, department, queue, or ve."),
			"currentAssigneeType":   stringProp("CamelCase alias of current_assignee_type."),
			"reviewed_by":           stringProp("Reviewer user id."),
			"reviewedBy":            stringProp("CamelCase alias of reviewed_by."),
			"current_node":          stringProp("Current workflow node id."),
			"currentNode":           stringProp("CamelCase alias of current_node."),
			"workflow_node":         stringProp("Workflow node alias."),
			"workflowNode":          stringProp("CamelCase alias of workflow_node."),
			"current_node_ids":      map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Current or active workflow node ids."},
			"currentNodeIDs":        map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "CamelCase alias of current_node_ids."},
			"workflow_node_ids":     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Workflow node path ids."},
			"workflowNodeIDs":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "CamelCase alias of workflow_node_ids."},
			"workflow_skill_id":     stringProp("Workflow skill id."),
			"workflowSkillID":       stringProp("CamelCase alias of workflow_skill_id."),
			"workflowSkillId":       stringProp("JavaScript-style alias of workflow_skill_id."),
			"workflow_version":      stringProp("Workflow skill version."),
			"workflowVersion":       stringProp("CamelCase alias of workflow_version."),
			"approval_workflow_id":  stringProp("Approval workflow id."),
			"approvalWorkflowID":    stringProp("CamelCase alias of approval_workflow_id."),
			"detail_url":            stringProp("Approval detail deep link."),
			"detailURL":             stringProp("CamelCase alias of detail_url."),
			"detailUrl":             stringProp("JavaScript-style alias of detail_url."),
		},
	}
}

func recordApprovalResultPackageOpenAPIProperties() map[string]interface{} {
	return map[string]interface{}{
		"result_payload": map[string]interface{}{"type": "object", "description": "Structured approval result payload, such as approval_result, business_status, business_record, text, table, dashboard, notification, external_receipt, requires_input, or error."},
		"outputs":        map[string]interface{}{"type": "array", "items": recordApprovalOutputOpenAPISchema(), "description": "Displayable approval outputs generated by the workflow skill."},
		"artifacts":      map[string]interface{}{"type": "array", "items": recordApprovalArtifactOpenAPISchema(), "description": "Files or downloadable artifacts generated by the workflow skill."},
	}
}

func recordApprovalOutputOpenAPISchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"type":        map[string]interface{}{"type": "string"},
			"kind":        map[string]interface{}{"type": "string"},
			"title":       map[string]interface{}{"type": "string"},
			"text":        map[string]interface{}{"type": "string"},
			"status":      map[string]interface{}{"type": "string"},
			"artifact_id": map[string]interface{}{"type": "string"},
			"artifact":    recordApprovalArtifactOpenAPISchema(),
			"data":        map[string]interface{}{"type": "object"},
		},
	}
}

func recordApprovalArtifactOpenAPISchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id":             map[string]interface{}{"type": "string"},
			"uri":            map[string]interface{}{"type": "string"},
			"name":           map[string]interface{}{"type": "string"},
			"path":           map[string]interface{}{"type": "string"},
			"mime_type":      map[string]interface{}{"type": "string"},
			"size_bytes":     map[string]interface{}{"type": "integer", "format": "int64"},
			"remote_url":     map[string]interface{}{"type": "string"},
			"checksum":       map[string]interface{}{"type": "string"},
			"download_state": map[string]interface{}{"type": "string"},
			"status":         map[string]interface{}{"type": "string"},
			"presentation":   map[string]interface{}{"type": "string"},
		},
	}
}

func recordApprovalOpenAPIResponses() map[string]interface{} {
	return map[string]interface{}{
		"200": jsonObjectOpenAPIResponse("Approval instance with workflow state and MaClaw App result package.", recordApprovalOpenAPISchema()),
	}
}

func recordApprovalListOpenAPIResponses() map[string]interface{} {
	schema := listResponseOpenAPISchema()
	if properties, ok := schema["properties"].(map[string]interface{}); ok {
		properties["items"] = map[string]interface{}{
			"type":        "array",
			"description": "Current page of approval instances.",
			"items":       recordApprovalOpenAPISchema(),
		}
	}
	return map[string]interface{}{
		"200": jsonObjectOpenAPIResponse("Approval instances with cursor metadata.", schema),
	}
}

func recordApprovalOpenAPISchema() map[string]interface{} {
	properties := map[string]interface{}{
		"id":           map[string]interface{}{"type": "string"},
		"tenant_id":    map[string]interface{}{"type": "string"},
		"dataset_id":   map[string]interface{}{"type": "string"},
		"record_id":    map[string]interface{}{"type": "string"},
		"app_id":       map[string]interface{}{"type": "string", "description": "MaClaw App id that owns this approval instance."},
		"blueprint_id": map[string]interface{}{"type": "string", "description": "Optional MaClaw App blueprint id that produced this approval instance."},
		"object_role":  map[string]interface{}{"type": "string", "description": "Semantic object role for the approved business record."},
		"status":       map[string]interface{}{"type": "string"},
		"kind":         map[string]interface{}{"type": "string"},
		"priority":     map[string]interface{}{"type": "string"},
		"summary":      map[string]interface{}{"type": "string"},
		"request":      recordApprovalRequestOpenAPISchema(),
		"decision":     map[string]interface{}{"type": "string"},
		"reason":       map[string]interface{}{"type": "string"},
		"assigned_to":  map[string]interface{}{"type": "string"},
		"reused":       map[string]interface{}{"type": "boolean"},
		"created_by":   map[string]interface{}{"type": "string"},
		"reviewed_by":  map[string]interface{}{"type": "string"},
		"created_at":   map[string]interface{}{"type": "string", "format": "date-time"},
		"due_at":       map[string]interface{}{"type": "string", "format": "date-time"},
		"reviewed_at":  map[string]interface{}{"type": "string", "format": "date-time"},
		"updated_at":   map[string]interface{}{"type": "string", "format": "date-time"},
	}
	for key, value := range recordApprovalWorkflowOpenAPIProperties() {
		properties[key] = value
	}
	for key, value := range recordApprovalResultPackageOpenAPIProperties() {
		properties[key] = value
	}
	return map[string]interface{}{
		"type":       "object",
		"required":   []string{"id", "dataset_id", "record_id", "status", "created_at", "updated_at"},
		"properties": properties,
	}
}

func objectRequestBody(required bool, requiredFields []string, properties map[string]interface{}) map[string]interface{} {
	schema := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}
	if len(requiredFields) > 0 {
		schema["required"] = requiredFields
	}
	return map[string]interface{}{
		"required": required,
		"content": map[string]interface{}{
			"application/json": map[string]interface{}{
				"schema": schema,
			},
		},
	}
}

func jsonObjectOpenAPIResponse(description string, schema map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"description": description,
		"content": map[string]interface{}{
			"application/json": map[string]interface{}{
				"schema": schema,
			},
		},
	}
}

func setupStatusOpenAPISchema() map[string]interface{} {
	return map[string]interface{}{
		"type":     "object",
		"required": []string{"initialized"},
		"properties": map[string]interface{}{
			"initialized":  map[string]interface{}{"type": "boolean"},
			"tenant_id":    map[string]interface{}{"type": "string"},
			"mode":         map[string]interface{}{"type": "string", "enum": []string{"local_admin", "hub_tenant_admin"}},
			"admin_scopes": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string", "enum": []string{"global", "tenant"}}},
			"tenants":      map[string]interface{}{"type": "array", "items": dataTenantOpenAPISchema()},
			"hub_registration": map[string]interface{}{
				"type":       "object",
				"properties": publicHubRegistrationOpenAPIProperties(),
			},
			"password_policy": map[string]interface{}{
				"type":     "object",
				"required": []string{"min_length", "lockout_enabled", "offline_reset_available"},
				"properties": map[string]interface{}{
					"min_length":              map[string]interface{}{"type": "integer", "minimum": 8, "maximum": 128},
					"rotation_days":           map[string]interface{}{"type": "integer", "description": "Advisory password rotation interval in days. Zero means no enforced rotation."},
					"lockout_enabled":         map[string]interface{}{"type": "boolean"},
					"login_max_failures":      map[string]interface{}{"type": "integer"},
					"login_lockout_minutes":   map[string]interface{}{"type": "integer"},
					"offline_reset_available": map[string]interface{}{"type": "boolean", "description": "Offline admin reset-password command is available for existing SQLite databases."},
				},
			},
		},
	}
}

func dataTenantOpenAPISchema() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{
		"id":                  map[string]interface{}{"type": "string"},
		"hub_tenant_id":       map[string]interface{}{"type": "string"},
		"slug":                map[string]interface{}{"type": "string"},
		"name":                map[string]interface{}{"type": "string"},
		"status":              map[string]interface{}{"type": "string"},
		"primary_domain":      map[string]interface{}{"type": "string"},
		"domains":             map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		"source":              map[string]interface{}{"type": "string"},
		"synced_at":           map[string]interface{}{"type": "string", "format": "date-time"},
		"updated_at":          map[string]interface{}{"type": "string", "format": "date-time"},
		"virtual_mail_domain": map[string]interface{}{"type": "string"},
	}}
}

func syncHubTenantsResultOpenAPISchema() map[string]interface{} {
	return map[string]interface{}{
		"type":     "object",
		"required": []string{"synced", "tenants"},
		"properties": map[string]interface{}{
			"synced":  map[string]interface{}{"type": "integer"},
			"tenants": map[string]interface{}{"type": "array", "items": dataTenantOpenAPISchema()},
		},
	}
}

func dataTenantListOpenAPIResponses() map[string]interface{} {
	return map[string]interface{}{
		"200": jsonObjectOpenAPIResponse("Data tenant list", map[string]interface{}{
			"type":     "object",
			"required": []string{"items"},
			"properties": map[string]interface{}{
				"items": map[string]interface{}{"type": "array", "items": dataTenantOpenAPISchema()},
			},
		}),
	}
}

func syncHubTenantsOpenAPIResponses() map[string]interface{} {
	return map[string]interface{}{
		"200": jsonObjectOpenAPIResponse("Hub tenants synced", syncHubTenantsResultOpenAPISchema()),
	}
}

func hubRegistrationOpenAPISchema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"required":   []string{"configured", "registered"},
		"properties": hubRegistrationOpenAPIProperties(),
	}
}

func hubRegistrationOpenAPIResponses(description string) map[string]interface{} {
	return map[string]interface{}{
		"200": jsonObjectOpenAPIResponse(description, map[string]interface{}{
			"type":     "object",
			"required": []string{"status"},
			"properties": map[string]interface{}{
				"status": hubRegistrationOpenAPISchema(),
			},
		}),
	}
}

func hubRegistrationOpenAPIProperties() map[string]interface{} {
	return map[string]interface{}{
		"configured":          map[string]interface{}{"type": "boolean"},
		"registered":          map[string]interface{}{"type": "boolean"},
		"hub_base_url":        map[string]interface{}{"type": "string"},
		"platform_id":         map[string]interface{}{"type": "string"},
		"platform_name":       map[string]interface{}{"type": "string"},
		"callback_base_url":   map[string]interface{}{"type": "string"},
		"virtual_mail_domain": map[string]interface{}{"type": "string"},
		"last_registered_at":  map[string]interface{}{"type": "string", "format": "date-time"},
		"last_synced_at":      map[string]interface{}{"type": "string", "format": "date-time"},
		"last_error":          map[string]interface{}{"type": "string"},
	}
}

func publicHubRegistrationOpenAPIProperties() map[string]interface{} {
	return map[string]interface{}{
		"configured":         map[string]interface{}{"type": "boolean"},
		"registered":         map[string]interface{}{"type": "boolean"},
		"last_registered_at": map[string]interface{}{"type": "string", "format": "date-time"},
		"last_synced_at":     map[string]interface{}{"type": "string", "format": "date-time"},
	}
}

func initializeAdminResultOpenAPISchema() map[string]interface{} {
	return map[string]interface{}{
		"type":     "object",
		"required": []string{"initialized", "tenant_id", "username", "role"},
		"properties": map[string]interface{}{
			"initialized":  map[string]interface{}{"type": "boolean"},
			"tenant_id":    map[string]interface{}{"type": "string"},
			"username":     map[string]interface{}{"type": "string"},
			"display_name": map[string]interface{}{"type": "string"},
			"role":         map[string]interface{}{"type": "string"},
			"admin_scope":  map[string]interface{}{"type": "string", "enum": []string{"global", "tenant"}},
			"token":        map[string]interface{}{"type": "string", "description": "Temporary bearer token returned once after setup."},
			"expires_at":   map[string]interface{}{"type": "string", "format": "date-time"},
		},
	}
}

func loginResultOpenAPISchema() map[string]interface{} {
	return map[string]interface{}{
		"type":     "object",
		"required": []string{"tenant_id", "username", "role", "token", "expires_at"},
		"properties": map[string]interface{}{
			"tenant_id":   map[string]interface{}{"type": "string"},
			"username":    map[string]interface{}{"type": "string"},
			"role":        map[string]interface{}{"type": "string"},
			"admin_scope": map[string]interface{}{"type": "string", "enum": []string{"global", "tenant"}},
			"token":       map[string]interface{}{"type": "string", "description": "Temporary bearer token for the Web Console and data APIs."},
			"expires_at":  map[string]interface{}{"type": "string", "format": "date-time"},
		},
	}
}

func adminAccountOpenAPISchema() map[string]interface{} {
	return map[string]interface{}{
		"type":     "object",
		"required": []string{"id", "tenant_id", "username", "role", "enabled", "created_at", "updated_at"},
		"properties": map[string]interface{}{
			"id":            map[string]interface{}{"type": "string"},
			"tenant_id":     map[string]interface{}{"type": "string"},
			"username":      map[string]interface{}{"type": "string"},
			"display_name":  map[string]interface{}{"type": "string"},
			"role":          map[string]interface{}{"type": "string"},
			"admin_scope":   map[string]interface{}{"type": "string", "enum": []string{"global", "tenant"}},
			"enabled":       map[string]interface{}{"type": "boolean"},
			"last_login_at": map[string]interface{}{"type": "string", "format": "date-time"},
			"created_at":    map[string]interface{}{"type": "string", "format": "date-time"},
			"updated_at":    map[string]interface{}{"type": "string", "format": "date-time"},
		},
	}
}

func adminAccountResultOpenAPIResponses(status, description string) map[string]interface{} {
	return map[string]interface{}{
		status: jsonObjectOpenAPIResponse(description, map[string]interface{}{
			"type":     "object",
			"required": []string{"account"},
			"properties": map[string]interface{}{
				"account": adminAccountOpenAPISchema(),
			},
		}),
	}
}

func adminAccountListOpenAPIResponses() map[string]interface{} {
	return map[string]interface{}{
		"200": jsonObjectOpenAPIResponse("Administrator account list", map[string]interface{}{
			"type":     "object",
			"required": []string{"items"},
			"properties": map[string]interface{}{
				"items": map[string]interface{}{"type": "array", "items": adminAccountOpenAPISchema()},
			},
		}),
	}
}

func adminSessionOpenAPISchema() map[string]interface{} {
	return map[string]interface{}{
		"type":     "object",
		"required": []string{"id", "tenant_id", "user_id", "username", "role", "expires_at", "created_at"},
		"properties": map[string]interface{}{
			"id":          map[string]interface{}{"type": "string"},
			"tenant_id":   map[string]interface{}{"type": "string"},
			"user_id":     map[string]interface{}{"type": "string"},
			"username":    map[string]interface{}{"type": "string"},
			"role":        map[string]interface{}{"type": "string"},
			"admin_scope": map[string]interface{}{"type": "string", "enum": []string{"global", "tenant"}},
			"current":     map[string]interface{}{"type": "boolean"},
			"expires_at":  map[string]interface{}{"type": "string", "format": "date-time"},
			"created_at":  map[string]interface{}{"type": "string", "format": "date-time"},
		},
	}
}

func adminSessionListOpenAPIResponses() map[string]interface{} {
	return map[string]interface{}{
		"200": jsonObjectOpenAPIResponse("Administrator session list", map[string]interface{}{
			"type":     "object",
			"required": []string{"items"},
			"properties": map[string]interface{}{
				"items": map[string]interface{}{"type": "array", "items": adminSessionOpenAPISchema()},
			},
		}),
	}
}

func adminSessionResultOpenAPIResponses() map[string]interface{} {
	return map[string]interface{}{
		"200": jsonObjectOpenAPIResponse("Administrator session updated", map[string]interface{}{
			"type":     "object",
			"required": []string{"session"},
			"properties": map[string]interface{}{
				"session": adminSessionOpenAPISchema(),
			},
		}),
	}
}

func updateAdminSessionOpenAPIRequestBody() map[string]interface{} {
	return objectRequestBody(false, nil, map[string]interface{}{
		"expires_hours": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 168},
		"expires_at":    map[string]interface{}{"type": "string", "format": "date-time"},
	})
}

func revokeAdminSessionOpenAPIResponses() map[string]interface{} {
	return map[string]interface{}{
		"200": jsonObjectOpenAPIResponse("Administrator session revoked", map[string]interface{}{
			"type":     "object",
			"required": []string{"session_id", "revoked"},
			"properties": map[string]interface{}{
				"session_id": map[string]interface{}{"type": "string"},
				"revoked":    map[string]interface{}{"type": "boolean"},
			},
		}),
	}
}

func queryRecordsOpenAPIRequestBody(maxLimit int, limitDescription string) map[string]interface{} {
	if maxLimit <= 0 {
		maxLimit = 500
	}
	if strings.TrimSpace(limitDescription) == "" {
		limitDescription = "Maximum number of records to return."
	}
	return map[string]interface{}{
		"required": false,
		"content": map[string]interface{}{
			"application/json": map[string]interface{}{
				"schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"q":         map[string]interface{}{"type": "string", "description": "Free-text search query."},
						"tag":       map[string]interface{}{"type": "string", "description": "Record tag filter."},
						"filter":    map[string]interface{}{"type": "object", "description": "Structured filter expression."},
						"sort":      map[string]interface{}{"type": "array", "items": map[string]string{"type": "object"}},
						"limit":     map[string]interface{}{"type": "integer", "minimum": 1, "maximum": maxLimit, "description": limitDescription},
						"before":    map[string]interface{}{"type": "string", "description": "Opaque timestamp cursor from next_before."},
						"before_id": map[string]interface{}{"type": "string", "description": "Stable tie-break cursor from next_before_id."},
					},
				},
			},
		},
	}
}

func errorResponseOpenAPISchema(description string) map[string]interface{} {
	response := map[string]interface{}{
		"description": description,
		"headers": map[string]interface{}{
			"X-Content-Type-Options": map[string]interface{}{
				"description": "Set to nosniff for JSON API responses.",
				"schema":      map[string]interface{}{"type": "string"},
			},
		},
		"content": map[string]interface{}{
			"application/json": map[string]interface{}{
				"schema": map[string]interface{}{
					"type":     "object",
					"required": []string{"error"},
					"properties": map[string]interface{}{
						"error": map[string]interface{}{"type": "string"},
					},
				},
			},
		},
	}
	if strings.Contains(strings.ToLower(description), "bearer token") {
		headers := response["headers"].(map[string]interface{})
		headers["WWW-Authenticate"] = map[string]interface{}{
			"description": "Bearer authentication challenge.",
			"schema":      map[string]interface{}{"type": "string"},
		}
	}
	return response
}

func businessViewQueryOpenAPIResponses() map[string]interface{} {
	properties := map[string]interface{}{
		"view":    map[string]interface{}{"type": "object", "description": "Business view definition used for projection."},
		"records": map[string]interface{}{"type": "array", "description": "Projected records for the current page.", "items": map[string]string{"type": "object"}},
	}
	for key, value := range maclawResultPackageOpenAPIProperties() {
		properties[key] = value
	}
	return map[string]interface{}{
		"200": map[string]interface{}{
			"description": "Business view records with cursor metadata",
			"content": map[string]interface{}{
				"application/json": map[string]interface{}{
					"schema": cursorEnvelopeOpenAPISchema([]string{"view", "records", "limit", "has_more"}, properties),
				},
			},
		},
	}
}

func reportRunOpenAPIResponses() map[string]interface{} {
	properties := map[string]interface{}{
		"report": map[string]interface{}{"type": "object", "description": "Report definition that was executed."},
		"result": map[string]interface{}{"type": "object", "description": "Aggregate report result."},
	}
	for key, value := range maclawResultPackageOpenAPIProperties() {
		properties[key] = value
	}
	return map[string]interface{}{
		"200": jsonObjectOpenAPIResponse("Report run result with MaClaw App result package.", map[string]interface{}{"type": "object", "properties": properties}),
	}
}

func dashboardRunOpenAPIResponses() map[string]interface{} {
	properties := map[string]interface{}{
		"dashboard":     map[string]interface{}{"type": "object", "description": "Dashboard definition that was executed."},
		"stats":         map[string]interface{}{"type": "object", "description": "Optional system stats snapshot."},
		"inbox_summary": map[string]interface{}{"type": "object", "description": "Optional MIS inbox summary."},
		"reports":       map[string]interface{}{"type": "array", "description": "Dashboard report cards.", "items": map[string]interface{}{"type": "object"}},
		"generated_at":  map[string]interface{}{"type": "string", "format": "date-time"},
	}
	for key, value := range maclawResultPackageOpenAPIProperties() {
		properties[key] = value
	}
	return map[string]interface{}{
		"200": jsonObjectOpenAPIResponse("Dashboard run result with MaClaw App result package.", map[string]interface{}{"type": "object", "properties": properties}),
	}
}

func maclawResultPackageOpenAPIProperties() map[string]interface{} {
	return map[string]interface{}{
		"primary_result":  map[string]interface{}{"type": "string", "description": "Primary MaClaw App result channel."},
		"business_status": map[string]interface{}{"type": "string", "description": "Business-facing result status."},
		"result_status":   map[string]interface{}{"type": "string", "description": "Machine-readable result status."},
		"result_payload":  map[string]interface{}{"type": "object", "description": "Structured MaClaw App result payload."},
		"outputs":         map[string]interface{}{"type": "array", "description": "Structured output blocks for GUI run history and app evidence.", "items": map[string]interface{}{"type": "object"}},
		"artifacts":       map[string]interface{}{"type": "array", "description": "Generated artifact descriptors; empty when no files were produced.", "items": map[string]interface{}{"type": "object"}},
	}
}

func inboxOpenAPIResponses() map[string]interface{} {
	return map[string]interface{}{
		"200": map[string]interface{}{
			"description": "MIS inbox page with cursor metadata",
			"content": map[string]interface{}{
				"application/json": map[string]interface{}{
					"schema": cursorEnvelopeOpenAPISchema([]string{"items", "limit", "has_more", "generated_at"}, map[string]interface{}{
						"generated_at": map[string]interface{}{"type": "string", "format": "date-time"},
					}),
				},
			},
		},
	}
}

func recordTimelineOpenAPIResponses() map[string]interface{} {
	return map[string]interface{}{
		"200": map[string]interface{}{
			"description": "Record timeline page with cursor metadata",
			"content": map[string]interface{}{
				"application/json": map[string]interface{}{
					"schema": cursorEnvelopeOpenAPISchema([]string{"dataset_id", "record_id", "items", "limit", "has_more"}, map[string]interface{}{
						"dataset_id": map[string]interface{}{"type": "string"},
						"record_id":  map[string]interface{}{"type": "string"},
					}),
				},
			},
		},
	}
}

func relatedRecordsOpenAPIResponses() map[string]interface{} {
	return map[string]interface{}{
		"200": map[string]interface{}{
			"description": "Related records with cursor metadata",
			"content": map[string]interface{}{
				"application/json": map[string]interface{}{
					"schema": relatedRecordsOpenAPISchema(),
				},
			},
		},
	}
}

func relatedRecordsOpenAPISchema() map[string]interface{} {
	return map[string]interface{}{
		"type":     "object",
		"required": []string{"dataset_id", "record_id", "links", "limit", "has_more"},
		"properties": map[string]interface{}{
			"dataset_id": map[string]interface{}{"type": "string"},
			"record_id":  map[string]interface{}{"type": "string"},
			"record":     map[string]interface{}{"type": "object", "description": "Masked source record when available."},
			"links": map[string]interface{}{
				"type":        "array",
				"description": "Outgoing and incoming related record links for the current page.",
				"items":       map[string]string{"type": "object"},
			},
			"limit":          map[string]interface{}{"type": "integer", "description": "Effective page size used by the service."},
			"has_more":       map[string]interface{}{"type": "boolean", "description": "True when another page may be available."},
			"next_before_id": map[string]interface{}{"type": "string", "description": "Stable link cursor for continuing related-record pages."},
		},
	}
}

func listResponseOpenAPIResponses() map[string]interface{} {
	return map[string]interface{}{
		"200": map[string]interface{}{
			"description": "Paginated list response",
			"content": map[string]interface{}{
				"application/json": map[string]interface{}{
					"schema": listResponseOpenAPISchema(),
				},
			},
		},
	}
}

func listResponseOpenAPISchema() map[string]interface{} {
	return cursorEnvelopeOpenAPISchema([]string{"items", "limit", "has_more"}, nil)
}

func cursorEnvelopeOpenAPISchema(required []string, extraProperties map[string]interface{}) map[string]interface{} {
	properties := map[string]interface{}{
		"items": map[string]interface{}{
			"type":        "array",
			"description": "Current page of resources.",
			"items":       map[string]string{"type": "object"},
		},
		"limit": map[string]interface{}{
			"type":        "integer",
			"description": "Effective page size used by the service.",
		},
		"has_more": map[string]interface{}{
			"type":        "boolean",
			"description": "True when another page may be available.",
		},
		"next_before": map[string]interface{}{
			"type":        "string",
			"description": "Timestamp cursor for continuing timestamp-backed lists.",
		},
		"next_before_id": map[string]interface{}{
			"type":        "string",
			"description": "Stable tie-break cursor for continuing pages; static definition lists use this cursor by itself.",
		},
	}
	for key, value := range extraProperties {
		properties[key] = value
	}
	return map[string]interface{}{
		"type":       "object",
		"required":   required,
		"properties": properties,
	}
}

func appendPaginationParameters(existing interface{}) []map[string]interface{} {
	out := []map[string]interface{}{}
	if params, ok := existing.([]map[string]interface{}); ok {
		out = append(out, params...)
	}
	seen := map[string]struct{}{}
	for _, param := range out {
		if name, ok := param["name"].(string); ok {
			seen[name] = struct{}{}
		}
	}
	add := func(name, description string, schema map[string]interface{}) {
		if _, ok := seen[name]; ok {
			return
		}
		out = append(out, map[string]interface{}{
			"name":        name,
			"in":          "query",
			"required":    false,
			"description": description,
			"schema":      schema,
		})
	}
	add("limit", "Maximum number of items to return. Defaults to 100 and is capped by the service.", map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 500})
	add("before", "Opaque timestamp cursor from next_before for timestamp-backed lists.", map[string]interface{}{"type": "string"})
	add("before_id", "Stable tie-break cursor from next_before_id. Static definition lists use this cursor by itself.", map[string]interface{}{"type": "string"})
	return out
}

func governanceEvidenceOpenAPIHeaders() map[string]interface{} {
	return map[string]interface{}{
		"X-MaClaw-Evidence-ID": map[string]interface{}{
			"description": "Stable reference id derived from the governance evidence pack digest.",
			"schema":      map[string]string{"type": "string"},
		},
		"X-MaClaw-Evidence-SHA256": map[string]interface{}{
			"description": "SHA256 digest of the governance evidence pack content excluding summary_text and digest fields.",
			"schema":      map[string]string{"type": "string"},
		},
	}
}
