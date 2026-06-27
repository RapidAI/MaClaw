package structureddata

import (
	"context"
	"path/filepath"
	"testing"
)

func TestUpsertAppInstallationNormalizesGovernanceResultContract(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	principal := Principal{TenantID: "tenant_1", UserID: "user_1", Role: "data_admin"}

	installed, err := svc.UpsertAppInstallation(context.Background(), principal, "tool.pdf.translate", UpsertAppInstallationInput{
		AppID:   "tool.pdf.translate",
		Name:    "PDF Translate",
		Kind:    "tool_app",
		Source:  "hub",
		Version: "1.0.0",
		Metadata: map[string]any{
			"workspace_layout": map[string]any{
				"schema":        "maclaw.app.ui.v1",
				"entry":         "tool_workspace",
				"template":      "document_workspace",
				"density":       "compact",
				"primaryRegion": "left",
				"output_region": "right",
				"navigation":    []any{"input", "output"},
				"list":          map[string]any{"columns": []any{"title", "status"}},
				"regions": []any{
					map[string]any{"id": "file_queue", "role": "input", "placement": "left"},
					map[string]any{"id": "output_panel", "role": "output", "placement": "right", "visible": false},
				},
			},
			"governance": map[string]any{
				"status":     "local_tested",
				"risk_level": "low",
				"resultContract": map[string]any{
					"schema":   "maclaw.app.result.v1",
					"primary":  "document",
					"types":    []any{"document", "inline_content"},
					"delivery": "download",
				},
				"testEvidence": map[string]any{
					"runId":                 "run-pdf-translate-1",
					"verifiedAt":            "2026-06-21T10:00:00Z",
					"definitionFingerprint": "sha256:pdf-translate",
					"artifactPresent":       true,
					"artifactName":          "translated-contract.docx",
					"artifactCount":         1,
					"outputCount":           2,
					"primaryResult":         "document",
					"resultPayload": map[string]any{
						"status": "completed",
						"pages":  12,
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("UpsertAppInstallation: %v", err)
	}
	contract, ok := installed.Metadata["result_contract"].(map[string]any)
	if !ok || contract["schema"] != "maclaw.app.result.v1" || contract["primary"] != "document" || contract["delivery"] != "download" {
		t.Fatalf("expected normalized top-level result contract: %#v", installed.Metadata)
	}
	if installed.Metadata["result_contract_schema"] != "maclaw.app.result.v1" || installed.Metadata["result_contract_primary"] != "document" || installed.Metadata["result_contract_delivery"] != "download" {
		t.Fatalf("expected result contract summaries: %#v", installed.Metadata)
	}
	if types := appInstallationStringList(installed.Metadata["result_contract_types"]); len(types) != 2 || types[0] != "document" || types[1] != "inline_content" {
		t.Fatalf("expected normalized result contract types: %#v", installed.Metadata)
	}
	if installed.Metadata["workspace_layout_entry"] != "tool_workspace" || installed.Metadata["workspace_layout_template"] != "document_workspace" || installed.Metadata["workspace_layout_density"] != "compact" {
		t.Fatalf("expected workspace layout summaries: %#v", installed.Metadata)
	}
	if installed.Metadata["workspace_layout_primary_region"] != "left" || installed.Metadata["workspace_layout_output_region"] != "right" || !appInstallationNumberEquals(installed.Metadata["workspace_layout_region_count"], 2) {
		t.Fatalf("expected workspace placement summaries: %#v", installed.Metadata)
	}
	if ids := appInstallationStringList(installed.Metadata["workspace_layout_region_ids"]); len(ids) != 2 || ids[0] != "file_queue" || ids[1] != "output_panel" {
		t.Fatalf("expected workspace region ids: %#v", installed.Metadata)
	}
	if layout, ok := installed.Metadata["workspace_layout"].(map[string]any); !ok {
		t.Fatalf("expected canonical workspace layout: %#v", installed.Metadata)
	} else if regions, ok := layout["regions"].([]any); !ok || len(regions) != 2 {
		t.Fatalf("expected canonical workspace regions: %#v", layout)
	} else if output, ok := regions[1].(map[string]any); !ok || output["visible"] != false {
		t.Fatalf("expected workspace region visibility to roundtrip: %#v", regions[1])
	}
	if navigation := appInstallationStringList(installed.Metadata["workspace_layout_navigation"]); len(navigation) != 2 || navigation[0] != "input" || navigation[1] != "output" {
		t.Fatalf("expected workspace navigation summary: %#v", installed.Metadata)
	}
	if columns := appInstallationStringList(installed.Metadata["workspace_layout_list_columns"]); len(columns) != 2 || columns[0] != "title" || columns[1] != "status" {
		t.Fatalf("expected workspace list column summary: %#v", installed.Metadata)
	}
	evidence, ok := installed.Metadata["test_evidence"].(map[string]any)
	if !ok || evidence["schema"] != "maclaw.app.test_evidence.v1" || evidence["run_id"] != "run-pdf-translate-1" || evidence["primary_result"] != "document" {
		t.Fatalf("expected normalized test evidence: %#v", installed.Metadata)
	}
	if installed.Metadata["test_evidence_run_id"] != "run-pdf-translate-1" || installed.Metadata["test_evidence_definition_fingerprint"] != "sha256:pdf-translate" || installed.Metadata["test_evidence_artifact_present"] != true || installed.Metadata["test_evidence_primary_result"] != "document" {
		t.Fatalf("expected test evidence summaries: %#v", installed.Metadata)
	}
	if installed.Metadata["test_evidence_artifact_count"] != float64(1) || installed.Metadata["test_evidence_output_count"] != float64(2) {
		t.Fatalf("expected numeric test evidence summaries: %#v", installed.Metadata)
	}
	if payload, ok := evidence["result_payload"].(map[string]any); !ok || payload["status"] != "completed" || payload["pages"] != float64(12) {
		t.Fatalf("expected structured test evidence result payload: %#v", evidence)
	}

	audit, err := svc.QueryAuditLogs(context.Background(), principal, QueryAuditLogsInput{Action: "app.installation_upsert", TargetType: "app_installation", TargetID: "tool.pdf.translate", Limit: 1})
	if err != nil {
		t.Fatalf("QueryAuditLogs: %v", err)
	}
	if len(audit) != 1 {
		t.Fatalf("expected app installation audit log: %#v", audit)
	}
	metadata := audit[0].Metadata
	if metadata["result_contract_schema"] != "maclaw.app.result.v1" || metadata["result_contract_primary"] != "document" || metadata["result_contract_delivery"] != "download" {
		t.Fatalf("expected audit result contract summaries: %#v", metadata)
	}
	if types := appInstallationStringList(metadata["result_contract_types"]); len(types) != 2 || types[0] != "document" || types[1] != "inline_content" {
		t.Fatalf("expected audit result contract types: %#v", metadata)
	}
	if navigation := appInstallationStringList(metadata["workspace_layout_navigation"]); len(navigation) != 2 || navigation[0] != "input" || navigation[1] != "output" {
		t.Fatalf("expected audit workspace navigation summary: %#v", metadata)
	}
	if metadata["workspace_layout_primary_region"] != "left" || metadata["workspace_layout_output_region"] != "right" || metadata["workspace_layout_region_count"] != float64(2) {
		t.Fatalf("expected audit workspace placement summaries: %#v", metadata)
	}
	if ids := appInstallationStringList(metadata["workspace_layout_region_ids"]); len(ids) != 2 || ids[0] != "file_queue" || ids[1] != "output_panel" {
		t.Fatalf("expected audit workspace region ids: %#v", metadata)
	}
	if columns := appInstallationStringList(metadata["workspace_layout_list_columns"]); len(columns) != 2 || columns[0] != "title" || columns[1] != "status" {
		t.Fatalf("expected audit workspace list column summary: %#v", metadata)
	}
	if metadata["test_evidence_run_id"] != "run-pdf-translate-1" || metadata["test_evidence_definition_fingerprint"] != "sha256:pdf-translate" || metadata["test_evidence_artifact_present"] != true || metadata["test_evidence_primary_result"] != "document" {
		t.Fatalf("expected audit test evidence summaries: %#v", metadata)
	}
	if _, ok := metadata["test_evidence_result_payload"]; ok {
		t.Fatalf("audit metadata should not include bulky test evidence result payload: %#v", metadata)
	}
}

func TestUpsertAppInstallationSynthesizesTestEvidenceFromSummaryMetadata(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	principal := Principal{TenantID: "tenant_1", UserID: "user_1", Role: "data_admin"}

	installed, err := svc.UpsertAppInstallation(context.Background(), principal, "sales.customer.console", UpsertAppInstallationInput{
		AppID: "sales.customer.console",
		Name:  "Customer Console",
		Kind:  "enterprise_normal_app",
		RoleBindings: []RoleBinding{{
			ObjectRole: "customer",
			Domain:     "sales",
			DatasetID:  "sales.customers",
			Required:   true,
		}},
		Metadata: map[string]any{
			"test_evidence_run_id":                        "run-customer-summary",
			"test_evidence_verified_at":                   "2026-06-27T08:30:00Z",
			"test_evidence_definition_fingerprint":        "sha256:customer-console",
			"test_evidence_test_protocol_fingerprint":     "proto-customer-summary",
			"test_evidence_primary_result":                "business_status",
			"test_evidence_artifact_present":              true,
			"test_evidence_artifact_name":                 "customer-summary.zip",
			"test_evidence_artifact_count":                1,
			"test_evidence_output_count":                  1,
			"test_evidence_result_payload":                map[string]any{"business_status": "renewal_ready", "business_record": map[string]any{"id": "customer-1"}},
			"test_evidence_outputs":                       []any{map[string]any{"kind": "business_record", "title": "Customer renewal", "text": "renewal ready", "status": "ready"}},
			"test_evidence_artifacts":                     []any{map[string]any{"id": "artifact-customer-summary", "name": "customer-summary.zip", "uri": "artifact://customer/summary.zip", "status": "ready"}},
			"test_evidence_result_coverage_ok":            true,
			"test_evidence_result_coverage_primary":       "business_status",
			"test_evidence_covered_types":                 []any{"business_status", "business_record", "document"},
			"test_evidence_missing_types":                 []any{},
			"test_evidence_approval_instance_id":          "wf-customer-1",
			"test_evidence_approval_id":                   "approval-customer-1",
			"test_evidence_record_id":                     "customer-1",
			"test_evidence_approval_status":               "approved",
			"test_evidence_approval_view_verified":        true,
			"test_evidence_dependency_verified_at":        "2026-06-27T08:20:00Z",
			"test_evidence_dependency_count":              1,
			"test_evidence_dependency_missing_required":   false,
			"test_evidence_dependency_blocking":           false,
			"test_evidence_workflow_contract_issue":       false,
			"test_evidence_workflow_contract_issue_count": 0,
			"test_evidence_governance_review_issue":       false,
			"test_evidence_governance_review_issue_count": 0,
		},
	})
	if err != nil {
		t.Fatalf("UpsertAppInstallation summary evidence: %v", err)
	}
	evidence, ok := installed.Metadata["test_evidence"].(map[string]any)
	if !ok || evidence["schema"] != "maclaw.app.test_evidence.v1" || evidence["run_id"] != "run-customer-summary" || evidence["primary_result"] != "business_status" {
		t.Fatalf("expected synthesized test evidence: %#v", installed.Metadata)
	}
	if evidence["test_protocol_fingerprint"] != "proto-customer-summary" || evidence["definition_fingerprint"] != "sha256:customer-console" || evidence["artifact_present"] != true {
		t.Fatalf("expected synthesized test evidence identity summaries: %#v", evidence)
	}
	if payload, ok := evidence["result_payload"].(map[string]any); !ok || payload["business_status"] != "renewal_ready" {
		t.Fatalf("expected synthesized result payload: %#v", evidence)
	}
	if outputs, ok := evidence["outputs"].([]any); !ok || len(outputs) != 1 {
		t.Fatalf("expected synthesized outputs: %#v", evidence)
	}
	if artifacts, ok := evidence["artifacts"].([]any); !ok || len(artifacts) != 1 {
		t.Fatalf("expected synthesized artifacts: %#v", evidence)
	}
	coverage, ok := evidence["result_coverage"].(map[string]any)
	if !ok || coverage["ok"] != true || coverage["primary"] != "business_status" {
		t.Fatalf("expected synthesized result coverage: %#v", evidence)
	}
	if covered := appInstallationStringList(coverage["covered_types"]); len(covered) != 3 || covered[2] != "document" {
		t.Fatalf("expected synthesized covered types: %#v", coverage)
	}
	approval, ok := evidence["approval_instance"].(map[string]any)
	if !ok || approval["instance_id"] != "wf-customer-1" || approval["approval_id"] != "approval-customer-1" || approval["record_id"] != "customer-1" || approval["approval_instance_view_verified"] != true {
		t.Fatalf("expected synthesized approval instance: %#v", evidence)
	}
	verification, ok := evidence["dependency_verification"].(map[string]any)
	if !ok || verification["schema"] != "maclaw.app.install_plan.v1" || verification["has_blocking_dependency"] != false || verification["has_governance_review_issue"] != false {
		t.Fatalf("expected synthesized dependency verification: %#v", evidence)
	}

	caps, err := svc.Capabilities(context.Background(), principal)
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	if len(caps.AppInstallations) != 1 {
		t.Fatalf("expected capabilities app installation: %#v", caps.AppInstallations)
	}
	capEvidence, ok := caps.AppInstallations[0].Metadata["test_evidence"].(map[string]any)
	if !ok || capEvidence["run_id"] != "run-customer-summary" || caps.AppInstallations[0].Metadata["test_evidence_artifact_count"] != float64(1) {
		t.Fatalf("expected capabilities to expose synthesized evidence and summaries: %#v", caps.AppInstallations[0].Metadata)
	}
}

func TestListAppInstallationsFiltersByApprovalInstanceEvidence(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	principal := Principal{TenantID: "tenant_1", UserID: "user_1", Role: "data_admin"}

	_, err = svc.UpsertAppInstallation(context.Background(), principal, "expense.approval", UpsertAppInstallationInput{
		AppID: "expense.approval",
		Name:  "Expense Approval",
		Kind:  "enterprise_approval_app",
		Metadata: map[string]any{
			"test_evidence": map[string]any{
				"runId": "run-expense-approval",
				"approvalInstance": map[string]any{
					"instanceId":           "wf-expense-1",
					"approvalID":           "approval-expense-1",
					"recordID":             "expense-1",
					"status":               "approved",
					"currentNode":          "expense.result_feedback",
					"currentNodeIDs":       []any{"expense.result_feedback", "expense.finance_archive"},
					"workflowSkillId":      "expense-approval-flow",
					"workflowVersion":      "2.1.0",
					"resultStatus":         "approved",
					"resultPayload":        map[string]any{"approval_result": "approved"},
					"approvalViewVerified": true,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("UpsertAppInstallation approval evidence: %v", err)
	}

	bySkill, err := svc.ListAppInstallations(context.Background(), principal, QueryAppInstallationsInput{WorkflowSkillID: "expense-approval-flow"})
	if err != nil {
		t.Fatalf("ListAppInstallations by workflow skill: %v", err)
	}
	if len(bySkill) != 1 || bySkill[0].AppID != "expense.approval" {
		t.Fatalf("expected workflow skill filter to match approval evidence: %#v", bySkill)
	}

	byNode, err := svc.ListAppInstallations(context.Background(), principal, QueryAppInstallationsInput{WorkflowNode: "expense.result_feedback"})
	if err != nil {
		t.Fatalf("ListAppInstallations by workflow node: %v", err)
	}
	if len(byNode) != 1 || byNode[0].AppID != "expense.approval" {
		t.Fatalf("expected workflow node filter to match approval evidence: %#v", byNode)
	}
	byParallelNode, err := svc.ListAppInstallations(context.Background(), principal, QueryAppInstallationsInput{WorkflowNode: "expense.finance_archive"})
	if err != nil {
		t.Fatalf("ListAppInstallations by parallel workflow node: %v", err)
	}
	if len(byParallelNode) != 1 || byParallelNode[0].AppID != "expense.approval" {
		t.Fatalf("expected workflow node filter to match approval evidence parallel node IDs: %#v", byParallelNode)
	}

	missing, err := svc.ListAppInstallations(context.Background(), principal, QueryAppInstallationsInput{WorkflowSkillID: "other-flow"})
	if err != nil {
		t.Fatalf("ListAppInstallations missing workflow skill: %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("unexpected app for different workflow skill: %#v", missing)
	}
}

func appInstallationNumberEquals(value any, expected float64) bool {
	switch typed := value.(type) {
	case int:
		return float64(typed) == expected
	case int64:
		return float64(typed) == expected
	case float64:
		return typed == expected
	default:
		return false
	}
}

func TestUpsertAppInstallationNormalizesApprovalWorkflowContract(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	principal := Principal{TenantID: "tenant_1", UserID: "user_1", Role: "data_admin"}

	installed, err := svc.UpsertAppInstallation(context.Background(), principal, "approval.expense", UpsertAppInstallationInput{
		AppID: "approval.expense",
		Kind:  "enterprise_approval_app",
		Metadata: map[string]any{
			"governance": map[string]any{
				"workflowContract": map[string]any{
					"schema":            "maclaw.app.workflow_contract.v1",
					"workflow_skill_id": "expense-flow",
					"workflow_version":  "2.0.0",
					"object_role":       "expense_report",
					"required_inputs":   []any{"record_ref", "applicant", "business_payload"},
					"decision_outputs":  []any{"approved", "rejected", "attention"},
					"status_mapping":    map[string]any{"pending": "approval_pending", "approved": "approved", "rejected": "rejected", "attention": "attention", "requires_input": "requires_input"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("UpsertAppInstallation workflow contract: %v", err)
	}
	contract, ok := installed.Metadata["workflow_contract"].(map[string]any)
	if !ok || contract["schema"] != "maclaw.app.workflow_contract.v1" || contract["workflowSkillId"] != "expense-flow" || contract["workflowVersion"] != "2.0.0" || contract["objectRole"] != "expense_report" {
		t.Fatalf("expected normalized workflow contract: %#v", installed.Metadata)
	}
	statusMapping, ok := contract["statusMapping"].(map[string]any)
	if !ok || statusMapping["requiresInput"] != "requires_input" {
		t.Fatalf("expected normalized workflow contract status mapping: %#v", contract)
	}
	if installed.Metadata["workflow_contract_schema"] != "maclaw.app.workflow_contract.v1" || installed.Metadata["workflow_contract_skill_id"] != "expense-flow" || installed.Metadata["workflow_contract_version"] != "2.0.0" || installed.Metadata["workflow_contract_object_role"] != "expense_report" {
		t.Fatalf("expected workflow contract summaries: %#v", installed.Metadata)
	}
	if inputs := appInstallationStringList(installed.Metadata["workflow_contract_required_inputs"]); len(inputs) != 3 || inputs[0] != "record_ref" || inputs[2] != "business_payload" {
		t.Fatalf("expected workflow contract input summaries: %#v", installed.Metadata)
	}
	if outputs := appInstallationStringList(installed.Metadata["workflow_contract_decision_outputs"]); len(outputs) != 3 || outputs[0] != "approved" || outputs[2] != "attention" {
		t.Fatalf("expected workflow contract output summaries: %#v", installed.Metadata)
	}
}

func TestUpsertAppInstallationRejectsInvalidWorkflowContract(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	principal := Principal{TenantID: "tenant_1", UserID: "user_1", Role: "data_admin"}
	_, err = svc.UpsertAppInstallation(context.Background(), principal, "normal.bad", UpsertAppInstallationInput{AppID: "normal.bad", Kind: "enterprise_normal_app", Metadata: map[string]any{"workflow_contract": map[string]any{"schema": "maclaw.app.workflow_contract.v1", "workflowSkillId": "flow", "objectRole": "expense"}}})
	if err == nil {
		t.Fatal("expected workflow contract kind validation error")
	}
	_, err = svc.UpsertAppInstallation(context.Background(), principal, "approval.bad", UpsertAppInstallationInput{AppID: "approval.bad", Kind: "enterprise_approval_app", Metadata: map[string]any{"workflow_contract": map[string]any{"schema": "maclaw.app.workflow_contract.v0", "workflowSkillId": "flow", "objectRole": "expense"}}})
	if err == nil {
		t.Fatal("expected workflow contract schema validation error")
	}
}
func TestUpsertAppInstallationRejectsInvalidResultContractSchema(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	_, err = svc.UpsertAppInstallation(context.Background(), Principal{TenantID: "tenant_1", UserID: "user_1", Role: "data_admin"}, "tool.bad", UpsertAppInstallationInput{
		AppID: "tool.bad",
		Kind:  "tool_app",
		Metadata: map[string]any{
			"result_contract": map[string]any{"schema": "maclaw.app.result.v0", "primary": "document"},
		},
	})
	if err == nil {
		t.Fatal("expected invalid result contract schema error")
	}
}

func TestAppInstallationOpenAPISchemaDocumentsFullTestEvidence(t *testing.T) {
	metadata, ok := appInstallationMetadataOpenAPISchema()["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected app installation metadata schema properties")
	}
	for _, key := range []string{
		"app_skill_source",
		"workspace_layout_primary_region",
		"workspace_layout_output_region",
		"workspace_layout_region_count",
		"workspace_layout_region_ids",
		"test_evidence_outputs",
		"test_evidence_artifacts",
		"test_evidence_result_payload",
		"test_evidence_approval_instance",
		"test_evidence_approval_id",
		"test_evidence_record_id",
		"test_evidence_approval_status",
		"test_evidence_approval_view_verified",
		"test_evidence_dependency_count",
		"test_evidence_result_coverage_ok",
	} {
		if _, ok := metadata[key]; !ok {
			t.Fatalf("expected OpenAPI app installation metadata schema to document %s: %#v", key, metadata)
		}
	}
}
