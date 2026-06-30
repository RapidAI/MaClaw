package structureddata

import (
	"context"
	"path/filepath"
	"strings"
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

func TestUpsertAppInstallationSummarizesToolResultEvidence(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	principal := Principal{TenantID: "tenant_1", UserID: "designer_1", Role: "data_admin"}

	installed, err := svc.UpsertAppInstallation(context.Background(), principal, "tool.contract.archive", UpsertAppInstallationInput{
		AppID:  "tool.contract.archive",
		Name:   "Contract Archive",
		Kind:   "tool_app",
		Source: "hub",
		Metadata: map[string]any{
			"test_evidence": map[string]any{
				"runId": "run-contract-archive-1",
				"resultPayload": map[string]any{
					"status":     "completed",
					"resultType": "content",
					"content":    "contract archive ready",
				},
				"outputs": []any{
					map[string]any{"kind": "content", "text": "contract archive ready"},
					map[string]any{"kind": "document", "title": "Archive PDF"},
				},
				"artifacts": []any{
					map[string]any{"name": "archive.pdf", "uri": "artifact://contract/archive.pdf", "type": "document"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("UpsertAppInstallation: %v", err)
	}
	evidence, ok := installed.Metadata["test_evidence"].(map[string]any)
	if !ok {
		t.Fatalf("expected normalized test evidence: %#v", installed.Metadata)
	}
	if installed.Metadata["test_evidence_result_type"] != "content" || installed.Metadata["test_evidence_output_type"] != "content" || installed.Metadata["test_evidence_result_content"] != "contract archive ready" {
		t.Fatalf("expected result payload summaries: %#v", installed.Metadata)
	}
	if kinds := appInstallationStringList(installed.Metadata["test_evidence_output_kinds"]); len(kinds) != 2 || kinds[0] != "content" || kinds[1] != "document" {
		t.Fatalf("expected output kind summaries: %#v", installed.Metadata)
	}
	if uris := appInstallationStringList(installed.Metadata["test_evidence_artifact_uris"]); len(uris) != 1 || uris[0] != "artifact://contract/archive.pdf" {
		t.Fatalf("expected artifact URI summaries: %#v", installed.Metadata)
	}
	if names := appInstallationStringList(evidence["artifact_names"]); len(names) != 1 || names[0] != "archive.pdf" {
		t.Fatalf("expected normalized artifact names: %#v", evidence)
	}
	if installed.Metadata["test_evidence_artifact_uri"] != "artifact://contract/archive.pdf" || installed.Metadata["test_evidence_artifact_name"] != "archive.pdf" || !appInstallationNumberEquals(installed.Metadata["test_evidence_artifact_count"], 1) {
		t.Fatalf("expected primary artifact summaries: %#v", installed.Metadata)
	}

	byResultType, err := svc.ListAppInstallations(context.Background(), principal, QueryAppInstallationsInput{ResultType: "document", Limit: 10})
	if err != nil {
		t.Fatalf("ListAppInstallations: %v", err)
	}
	if len(byResultType) != 1 || byResultType[0].AppID != "tool.contract.archive" {
		t.Fatalf("expected result_type=document to match artifact/output summaries: %#v", byResultType)
	}

	audit, err := svc.QueryAuditLogs(context.Background(), principal, QueryAuditLogsInput{Action: "app.installation_upsert", TargetType: "app_installation", TargetID: "tool.contract.archive", Limit: 1})
	if err != nil {
		t.Fatalf("QueryAuditLogs: %v", err)
	}
	if len(audit) != 1 {
		t.Fatalf("expected audit log: %#v", audit)
	}
	metadata := audit[0].Metadata
	if metadata["test_evidence_artifact_uri"] != "artifact://contract/archive.pdf" || metadata["test_evidence_result_type"] != "content" || metadata["test_evidence_output_type"] != "content" {
		t.Fatalf("expected audit result summaries: %#v", metadata)
	}
	if kinds := appInstallationStringList(metadata["test_evidence_output_kinds"]); len(kinds) != 2 || kinds[1] != "document" {
		t.Fatalf("expected audit output kind summaries: %#v", metadata)
	}
	if _, ok := metadata["test_evidence_result_payload"]; ok {
		t.Fatalf("audit metadata should not include bulky test evidence result payload: %#v", metadata)
	}
}
func TestUpsertAppInstallationInfersWorkspacePrimaryAndOutputRegions(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	principal := Principal{TenantID: "tenant_1", UserID: "designer_1", Role: "data_admin"}

	installed, err := svc.UpsertAppInstallation(context.Background(), principal, "expense.approval", UpsertAppInstallationInput{
		AppID: "expense.approval",
		Name:  "Expense Approval",
		Kind:  "enterprise_approval_app",
		Metadata: map[string]any{
			"workspace_layout": map[string]any{
				"schema":   "maclaw.app.ui.v1",
				"entry":    "approval_workspace",
				"template": "classic_split",
				"regions": []any{
					map[string]any{"id": "request_form", "role": "form", "placement": "left"},
					map[string]any{"id": "approval_inbox", "role": "list", "placement": "center"},
					map[string]any{"id": "result_panel", "role": "result", "placement": "bottom"},
				},
			},
			"workflow_contract": map[string]any{
				"schema":          "maclaw.app.workflow_contract.v1",
				"workflowSkillId": "expense-workflow",
				"objectRole":      "expense_report",
			},
		},
	})
	if err != nil {
		t.Fatalf("UpsertAppInstallation: %v", err)
	}
	if installed.Metadata["workspace_layout_primary_region"] != "left" || installed.Metadata["workspace_layout_output_region"] != "bottom" {
		t.Fatalf("expected inferred primary/output workspace regions: %#v", installed.Metadata)
	}
	layout, ok := installed.Metadata["workspace_layout"].(map[string]any)
	if !ok {
		t.Fatalf("expected normalized workspace layout: %#v", installed.Metadata)
	}
	if layout["primaryRegion"] != "left" || layout["outputRegion"] != "bottom" {
		t.Fatalf("expected inferred primary/output written back to layout: %#v", layout)
	}
	if ids := appInstallationStringList(installed.Metadata["workspace_layout_region_ids"]); len(ids) != 3 || ids[0] != "request_form" || ids[2] != "result_panel" {
		t.Fatalf("expected canonical region ids: %#v", installed.Metadata)
	}
}
func TestUpsertAppInstallationNormalizesResultContractDeliveryObject(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	principal := Principal{TenantID: "tenant_1", UserID: "user_1", Role: "data_admin"}

	installed, err := svc.UpsertAppInstallation(context.Background(), principal, "sales.customer.console", UpsertAppInstallationInput{
		AppID:  "sales.customer.console",
		Name:   "Customer Console",
		Kind:   "enterprise_normal_app",
		Source: "hub",
		Metadata: map[string]any{
			"result_contract": map[string]any{
				"schema":  "maclaw.app.result.v1",
				"primary": "business_status",
				"types":   []any{"business_status", "business_record", "content", "notification"},
				"delivery": map[string]any{
					"inline_content":  true,
					"artifacts":       false,
					"business_record": true,
					"notifications":   true,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("UpsertAppInstallation: %v", err)
	}
	contract, ok := installed.Metadata["result_contract"].(map[string]any)
	if !ok {
		t.Fatalf("expected normalized result contract: %#v", installed.Metadata)
	}
	delivery, ok := contract["delivery"].(map[string]any)
	if !ok || delivery["inlineContent"] != true || delivery["artifacts"] != false || delivery["businessRecord"] != true || delivery["notifications"] != true {
		t.Fatalf("expected canonical delivery object: %#v", contract["delivery"])
	}
	if installed.Metadata["result_contract_delivery_inline_content"] != true || installed.Metadata["result_contract_delivery_artifacts"] != false || installed.Metadata["result_contract_delivery_business_record"] != true || installed.Metadata["result_contract_delivery_notifications"] != true {
		t.Fatalf("expected delivery boolean summaries: %#v", installed.Metadata)
	}
	if modes := appInstallationStringList(installed.Metadata["result_contract_delivery_modes"]); len(modes) != 3 || modes[0] != "inline_content" || modes[1] != "business_record" || modes[2] != "notifications" {
		t.Fatalf("expected enabled delivery modes summary: %#v", installed.Metadata["result_contract_delivery_modes"])
	}

	audit, err := svc.QueryAuditLogs(context.Background(), principal, QueryAuditLogsInput{Action: "app.installation_upsert", TargetType: "app_installation", TargetID: "sales.customer.console", Limit: 1})
	if err != nil {
		t.Fatalf("QueryAuditLogs: %v", err)
	}
	if len(audit) != 1 {
		t.Fatalf("expected app installation audit log: %#v", audit)
	}
	metadata := audit[0].Metadata
	if metadata["result_contract_delivery_inline_content"] != true || metadata["result_contract_delivery_business_record"] != true || metadata["result_contract_delivery_notifications"] != true {
		t.Fatalf("expected audit delivery summaries: %#v", metadata)
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
	byDefinition, err := svc.ListAppInstallations(context.Background(), principal, QueryAppInstallationsInput{DefinitionFingerprint: "sha256:customer-console"})
	if err != nil {
		t.Fatalf("ListAppInstallations by definition fingerprint: %v", err)
	}
	if len(byDefinition) != 1 || byDefinition[0].AppID != "sales.customer.console" {
		t.Fatalf("expected definition fingerprint filter to return installed app: %#v", byDefinition)
	}
	missingDefinition, err := svc.ListAppInstallations(context.Background(), principal, QueryAppInstallationsInput{DefinitionFingerprint: "sha256:other-definition"})
	if err != nil {
		t.Fatalf("ListAppInstallations by missing definition fingerprint: %v", err)
	}
	if len(missingDefinition) != 0 {
		t.Fatalf("expected missing definition fingerprint filter to return no apps: %#v", missingDefinition)
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

func TestUpsertAppInstallationPreservesFullEnterpriseApprovalTestEvidence(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	principal := Principal{TenantID: "tenant_1", UserID: "user_1", Role: "data_admin"}

	installed, err := svc.UpsertAppInstallation(context.Background(), principal, "expense.approval.full", UpsertAppInstallationInput{
		AppID: "expense.approval.full",
		Name:  "Expense Approval Full Evidence",
		Kind:  "enterprise_approval_app",
		Metadata: map[string]any{
			"governance": map[string]any{
				"testEvidence": map[string]any{
					"runId":                 "run-expense-full",
					"verifiedAt":            "2026-06-27T10:30:00Z",
					"definitionFingerprint": "sha256:expense-full",
					"primaryResult":         "approval_result",
					"resultPayload": map[string]any{
						"approval_result": "approved",
						"business_status": "finance_ready",
					},
					"outputs": []any{
						map[string]any{"kind": "text", "title": "Decision", "text": "Approved by finance", "status": "ready"},
						map[string]any{"kind": "business_record", "title": "Expense record", "data": map[string]any{"record_id": "expense-1001"}},
					},
					"artifacts": []any{
						map[string]any{"id": "artifact-expense-1001", "name": "expense-approval.pdf", "uri": "artifact://expense/1001.pdf", "status": "ready"},
					},
					"approvalInstance": map[string]any{
						"instanceId":                   "wf-expense-1001",
						"approvalID":                   "approval-expense-1001",
						"recordID":                     "expense-1001",
						"datasetID":                    "expense_reports",
						"objectRole":                   "expense_report",
						"currentNode":                  "finance.archive",
						"workflowSkillId":              "expense-approval-flow",
						"workflowVersion":              "2.1.0",
						"businessStatus":               "finance_ready",
						"resultStatus":                 "approved",
						"approvalInstanceViewVerified": true,
						"resultPayload":                map[string]any{"approval_result": "approved", "amount": 1280},
						"outputs":                      []any{map[string]any{"kind": "text", "title": "Approval note", "text": "Ready for payment"}},
						"artifacts":                    []any{map[string]any{"id": "artifact-expense-1001", "name": "expense-approval.pdf", "status": "ready"}},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("UpsertAppInstallation full approval evidence: %v", err)
	}
	evidence, ok := installed.Metadata["test_evidence"].(map[string]any)
	if !ok {
		t.Fatalf("expected normalized test evidence: %#v", installed.Metadata)
	}
	if evidence["run_id"] != "run-expense-full" || evidence["definition_fingerprint"] != "sha256:expense-full" || evidence["primary_result"] != "approval_result" {
		t.Fatalf("expected normalized test evidence identity: %#v", evidence)
	}
	if outputs, ok := evidence["outputs"].([]any); !ok || len(outputs) != 2 {
		t.Fatalf("expected full outputs to be preserved: %#v", evidence)
	}
	if artifacts, ok := evidence["artifacts"].([]any); !ok || len(artifacts) != 1 {
		t.Fatalf("expected full artifacts to be preserved: %#v", evidence)
	}
	approval, ok := evidence["approval_instance"].(map[string]any)
	if !ok {
		t.Fatalf("expected full approval instance to be preserved: %#v", evidence)
	}
	if approval["instanceId"] != "wf-expense-1001" || approval["approvalID"] != "approval-expense-1001" || approval["workflowSkillId"] != "expense-approval-flow" || approval["businessStatus"] != "finance_ready" || approval["resultStatus"] != "approved" {
		t.Fatalf("expected approval instance identity and statuses to roundtrip: %#v", approval)
	}
	if payload, ok := approval["resultPayload"].(map[string]any); !ok || payload["approval_result"] != "approved" || payload["amount"] != float64(1280) {
		t.Fatalf("expected approval instance result payload to roundtrip: %#v", approval)
	}
	if outputs, ok := approval["outputs"].([]any); !ok || len(outputs) != 1 {
		t.Fatalf("expected approval instance outputs to roundtrip: %#v", approval)
	}
	if artifacts, ok := approval["artifacts"].([]any); !ok || len(artifacts) != 1 {
		t.Fatalf("expected approval instance artifacts to roundtrip: %#v", approval)
	}
	if !appInstallationNumberEquals(installed.Metadata["test_evidence_output_count"], 2) || !appInstallationNumberEquals(installed.Metadata["test_evidence_artifact_count"], 1) || installed.Metadata["test_evidence_approval_id"] != "approval-expense-1001" || installed.Metadata["test_evidence_record_id"] != "expense-1001" || installed.Metadata["test_evidence_approval_status"] != "approved" {
		t.Fatalf("expected summaries to be derived from full evidence: %#v", installed.Metadata)
	}
	caps, err := svc.Capabilities(context.Background(), principal)
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	if len(caps.AppInstallations) != 1 {
		t.Fatalf("expected capabilities app installation: %#v", caps.AppInstallations)
	}
	capEvidence, ok := caps.AppInstallations[0].Metadata["test_evidence"].(map[string]any)
	if !ok {
		t.Fatalf("expected capabilities to expose full test evidence: %#v", caps.AppInstallations[0].Metadata)
	}
	capApproval, ok := capEvidence["approval_instance"].(map[string]any)
	if !ok {
		t.Fatalf("expected capabilities approval instance: %#v", capEvidence)
	}
	if _, ok := capApproval["resultPayload"].(map[string]any); !ok {
		t.Fatalf("expected capabilities approval instance result payload to remain nested: %#v", capApproval)
	}
	if outputs, ok := capEvidence["outputs"].([]any); !ok || len(outputs) != 2 {
		t.Fatalf("expected capabilities full outputs to remain available: %#v", capEvidence)
	}
	for _, tc := range []struct {
		name  string
		query QueryAppInstallationsInput
	}{
		{name: "workflow skill", query: QueryAppInstallationsInput{WorkflowSkillID: "expense-approval-flow"}},
		{name: "workflow node", query: QueryAppInstallationsInput{WorkflowNode: "finance.archive"}},
		{name: "approval status", query: QueryAppInstallationsInput{ApprovalStatus: "approved"}},
		{name: "approval decision", query: QueryAppInstallationsInput{ApprovalDecision: "approved"}},
		{name: "dataset", query: QueryAppInstallationsInput{DatasetID: "expense_reports"}},
		{name: "object role", query: QueryAppInstallationsInput{ObjectRole: "expense_report"}},
		{name: "record", query: QueryAppInstallationsInput{RecordID: "expense-1001"}},
		{name: "approval id", query: QueryAppInstallationsInput{ApprovalID: "approval-expense-1001"}},
		{name: "workflow instance", query: QueryAppInstallationsInput{WorkflowInstanceID: "wf-expense-1001"}},
		{name: "result type", query: QueryAppInstallationsInput{ResultType: "approval_result"}},
	} {
		matches, err := svc.ListAppInstallations(context.Background(), principal, tc.query)
		if err != nil {
			t.Fatalf("ListAppInstallations by %s: %v", tc.name, err)
		}
		if len(matches) != 1 || matches[0].AppID != "expense.approval.full" {
			t.Fatalf("expected %s filter to locate full approval evidence app: %#v", tc.name, matches)
		}
	}
}

func TestUpsertAppInstallationNormalizesTopLevelDependencyVerification(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	principal := Principal{TenantID: "tenant_1", UserID: "user_1", Role: "data_admin"}

	installed, err := svc.UpsertAppInstallation(context.Background(), principal, "expense.ready", UpsertAppInstallationInput{
		AppID: "expense.ready",
		Name:  "Ready Expense Approval",
		Kind:  "enterprise_approval_app",
		Metadata: map[string]any{
			"dependencyVerification": map[string]any{
				"schema":                "maclaw.app.install_plan.v1",
				"verifiedAt":            "2026-06-27T09:00:00Z",
				"dependencyCount":       1,
				"hasMissingRequired":    false,
				"hasBlockingDependency": false,
				"dependencies": []any{
					map[string]any{"id": "expense-workflow", "kind": "workflow_skill", "source": "enterprise_hub", "installRef": "cap-hub-expense-workflow", "required": true, "installed": true, "health": "ready", "action": "skip", "app_ids": []any{"expense.ready"}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("UpsertAppInstallation dependency verification: %v", err)
	}
	verification, ok := installed.Metadata["dependency_verification"].(map[string]any)
	if !ok || verification["schema"] != "maclaw.app.install_plan.v1" || verification["verified_at"] != "2026-06-27T09:00:00Z" || verification["has_blocking_dependency"] != false {
		t.Fatalf("expected normalized top-level dependency verification: %#v", installed.Metadata)
	}
	if _, ok := installed.Metadata["dependencyVerification"]; ok {
		t.Fatalf("camelCase dependencyVerification should be normalized away: %#v", installed.Metadata)
	}
	dependencies, ok := verification["dependencies"].([]any)
	if !ok || len(dependencies) != 1 {
		t.Fatalf("expected dependency verification dependencies to roundtrip: %#v", verification)
	}
	dependency, ok := dependencies[0].(map[string]any)
	if !ok || dependency["id"] != "expense-workflow" || dependency["health"] != "ready" || dependency["install_ref"] != "cap-hub-expense-workflow" {
		t.Fatalf("expected dependency verification dependencies to roundtrip: %#v", verification)
	}
	if _, ok := dependency["installRef"]; ok {
		t.Fatalf("expected dependency installRef to normalize to install_ref: %#v", dependency)
	}
	if installed.Metadata["test_evidence_dependency_verified_at"] != "2026-06-27T09:00:00Z" || installed.Metadata["test_evidence_dependency_count"] != float64(1) || installed.Metadata["test_evidence_dependency_blocking"] != false {
		t.Fatalf("expected dependency verification summaries: %#v", installed.Metadata)
	}
	caps, err := svc.Capabilities(context.Background(), principal)
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	if len(caps.AppInstallations) != 1 {
		t.Fatalf("expected capabilities app installation: %#v", caps.AppInstallations)
	}
	capVerification, ok := caps.AppInstallations[0].Metadata["dependency_verification"].(map[string]any)
	if !ok || capVerification["verified_at"] != "2026-06-27T09:00:00Z" {
		t.Fatalf("expected capabilities to expose normalized dependency verification: %#v", caps.AppInstallations[0].Metadata)
	}
	capDependencies, ok := capVerification["dependencies"].([]any)
	if !ok || len(capDependencies) != 1 {
		t.Fatalf("expected capabilities dependency verification dependencies: %#v", capVerification)
	}
	capDependency, ok := capDependencies[0].(map[string]any)
	if !ok || capDependency["install_ref"] != "cap-hub-expense-workflow" {
		t.Fatalf("expected capabilities dependency install_ref: %#v", capVerification)
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

func TestListAppInstallationsFiltersNestedInstallEvidence(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	principal := Principal{TenantID: "tenant_1", UserID: "user_1", Role: "data_admin"}

	_, err = svc.UpsertAppInstallation(context.Background(), principal, "expense.nested_install_evidence", UpsertAppInstallationInput{
		AppID: "expense.nested_install_evidence",
		Name:  "Nested Install Evidence Expense Approval",
		Kind:  "enterprise_approval_app",
		Metadata: map[string]any{
			"install_evidence": map[string]any{
				"dependencies": []any{
					map[string]any{"id": "expense-nested-flow", "kind": "workflow_skill", "required": true, "installed": true, "health": "ready"},
				},
				"dependency_verification": map[string]any{
					"schema":                  "maclaw.app.install_plan.v1",
					"dependency_count":        1,
					"has_missing_required":    false,
					"has_blocking_dependency": false,
				},
				"workflow_mapping": map[string]any{
					"schema":       "maclaw.app.workflow.v1",
					"submitNode":   "expense.submit",
					"approvalNode": "expense.manager_review",
					"resultNode":   "expense.result_pack",
				},
				"result_contract": map[string]any{
					"schema":  "maclaw.app.result.v1",
					"primary": "approval_result",
					"types":   []any{"approval_result", "document"},
				},
				"test_evidence": map[string]any{
					"runId":                 "run-nested-install-evidence",
					"definitionFingerprint": "sha256:nested-install-evidence",
					"primaryResult":         "approval_result",
					"approvalInstance": map[string]any{
						"instanceId":      "wf-nested-install-1",
						"approvalID":      "approval-nested-install-1",
						"recordID":        "expense-nested-1",
						"status":          "approved",
						"currentNode":     "expense.result_pack",
						"workflowSkillId": "expense-nested-flow",
						"resultPayload":   map[string]any{"decision": "approved", "approval_result": "approved"},
						"outputs": []any{
							map[string]any{"kind": "document", "title": "Approval document", "text": "approved"},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("UpsertAppInstallation nested install evidence: %v", err)
	}

	cases := []struct {
		name  string
		query QueryAppInstallationsInput
	}{
		{name: "workflow skill", query: QueryAppInstallationsInput{WorkflowSkillID: "expense-nested-flow"}},
		{name: "workflow mapping node", query: QueryAppInstallationsInput{WorkflowNode: "expense.manager_review"}},
		{name: "approval instance node", query: QueryAppInstallationsInput{WorkflowNode: "expense.result_pack"}},
		{name: "approval status", query: QueryAppInstallationsInput{ApprovalStatus: "approved"}},
		{name: "approval decision", query: QueryAppInstallationsInput{ApprovalDecision: "approved"}},
		{name: "result type", query: QueryAppInstallationsInput{ResultType: "document"}},
		{name: "definition fingerprint", query: QueryAppInstallationsInput{DefinitionFingerprint: "sha256:nested-install-evidence"}},
	}
	for _, tc := range cases {
		items, err := svc.ListAppInstallations(context.Background(), principal, tc.query)
		if err != nil {
			t.Fatalf("ListAppInstallations by %s: %v", tc.name, err)
		}
		if len(items) != 1 || items[0].AppID != "expense.nested_install_evidence" {
			t.Fatalf("expected nested install evidence to match %s, got %#v", tc.name, items)
		}
	}

	blocking := false
	ready, err := svc.ListAppInstallations(context.Background(), principal, QueryAppInstallationsInput{HasBlockingDependency: &blocking})
	if err != nil {
		t.Fatalf("ListAppInstallations by nested dependency health: %v", err)
	}
	if len(ready) != 1 || ready[0].AppID != "expense.nested_install_evidence" {
		t.Fatalf("expected nested dependency verification to match nonblocking filter, got %#v", ready)
	}
}

func TestListAppInstallationsFiltersByDependencyHealth(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	principal := Principal{TenantID: "tenant_1", UserID: "user_1", Role: "data_admin"}

	_, err = svc.UpsertAppInstallation(context.Background(), principal, "expense.blocked", UpsertAppInstallationInput{
		AppID: "expense.blocked",
		Name:  "Blocked Expense Approval",
		Kind:  "enterprise_approval_app",
		Metadata: map[string]any{
			"dependency_verification": map[string]any{
				"schema":                  "maclaw.app.install_plan.v1",
				"dependency_count":        2,
				"has_missing_required":    true,
				"has_blocking_dependency": true,
			},
		},
	})
	if err != nil {
		t.Fatalf("UpsertAppInstallation blocked: %v", err)
	}
	_, err = svc.UpsertAppInstallation(context.Background(), principal, "expense.ready", UpsertAppInstallationInput{
		AppID: "expense.ready",
		Name:  "Ready Expense Approval",
		Kind:  "enterprise_approval_app",
		Metadata: map[string]any{
			"test_evidence": map[string]any{
				"dependency_verification": map[string]any{
					"schema":                  "maclaw.app.install_plan.v1",
					"dependency_count":        2,
					"has_missing_required":    false,
					"has_blocking_dependency": false,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("UpsertAppInstallation ready: %v", err)
	}
	_, err = svc.UpsertAppInstallation(context.Background(), principal, "expense.legacy_ready", UpsertAppInstallationInput{
		AppID: "expense.legacy_ready",
		Name:  "Legacy Ready Expense Approval",
		Kind:  "enterprise_approval_app",
		Metadata: map[string]any{
			"has_missing_required_dependency": true,
			"has_blocking_dependency":         true,
			"dependency_verification": map[string]any{
				"schema":                  "maclaw.app.install_plan.v1",
				"dependency_count":        1,
				"has_missing_required":    false,
				"has_blocking_dependency": false,
			},
		},
	})
	if err != nil {
		t.Fatalf("UpsertAppInstallation legacy ready: %v", err)
	}

	blocking := true
	blocked, err := svc.ListAppInstallations(context.Background(), principal, QueryAppInstallationsInput{HasBlockingDependency: &blocking})
	if err != nil {
		t.Fatalf("ListAppInstallations blocking: %v", err)
	}
	if len(blocked) != 1 || blocked[0].AppID != "expense.blocked" {
		t.Fatalf("expected blocking dependency filter to return blocked app: %#v", blocked)
	}
	blocking = false
	ready, err := svc.ListAppInstallations(context.Background(), principal, QueryAppInstallationsInput{HasBlockingDependency: &blocking})
	if err != nil {
		t.Fatalf("ListAppInstallations nonblocking: %v", err)
	}
	readyByID := map[string]bool{}
	for _, app := range ready {
		readyByID[app.AppID] = true
	}
	if len(ready) != 2 || !readyByID["expense.ready"] || !readyByID["expense.legacy_ready"] {
		t.Fatalf("expected nonblocking dependency filter to return ready app: %#v", ready)
	}
	missingRequired := true
	missing, err := svc.ListAppInstallations(context.Background(), principal, QueryAppInstallationsInput{HasMissingRequiredDependency: &missingRequired})
	if err != nil {
		t.Fatalf("ListAppInstallations missing required: %v", err)
	}
	if len(missing) != 1 || missing[0].AppID != "expense.blocked" {
		t.Fatalf("expected missing required dependency filter to return blocked app: %#v", missing)
	}
}

func TestListAppInstallationsFiltersByRoleBinding(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	principal := Principal{TenantID: "tenant_1", UserID: "user_1", Role: "data_admin"}

	_, err = svc.UpsertAppInstallation(context.Background(), principal, "purchase.normal", UpsertAppInstallationInput{
		AppID: "purchase.normal",
		Name:  "Purchase Normal",
		Kind:  "enterprise_normal_app",
		RoleBindings: []RoleBinding{{
			ObjectRole: "purchase_order",
			Domain:     "purchase",
			DatasetID:  "purchase.orders",
			Required:   true,
		}},
		Metadata: map[string]any{"description": "role binding only"},
	})
	if err != nil {
		t.Fatalf("UpsertAppInstallation role binding app: %v", err)
	}
	_, err = svc.UpsertAppInstallation(context.Background(), principal, "purchase.other", UpsertAppInstallationInput{
		AppID: "purchase.other",
		Name:  "Purchase Other",
		Kind:  "enterprise_normal_app",
		RoleBindings: []RoleBinding{{
			ObjectRole: "supplier",
			Domain:     "purchase",
			DatasetID:  "purchase.suppliers",
			Required:   true,
		}},
		Metadata: map[string]any{"description": "other binding"},
	})
	if err != nil {
		t.Fatalf("UpsertAppInstallation other role binding app: %v", err)
	}

	byDataset, err := svc.ListAppInstallations(context.Background(), principal, QueryAppInstallationsInput{DatasetID: "purchase.orders"})
	if err != nil {
		t.Fatalf("ListAppInstallations by role binding dataset: %v", err)
	}
	if len(byDataset) != 1 || byDataset[0].AppID != "purchase.normal" || len(byDataset[0].RoleBindings) != 1 {
		t.Fatalf("expected dataset filter to match role binding app: %#v", byDataset)
	}
	byObjectRole, err := svc.ListAppInstallations(context.Background(), principal, QueryAppInstallationsInput{ObjectRole: "purchase_order"})
	if err != nil {
		t.Fatalf("ListAppInstallations by role binding object role: %v", err)
	}
	if len(byObjectRole) != 1 || byObjectRole[0].AppID != "purchase.normal" {
		t.Fatalf("expected object role filter to match role binding app: %#v", byObjectRole)
	}
}

func TestListAppInstallationsFiltersByApprovalResultMetadata(t *testing.T) {
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
			"result_contract": map[string]any{
				"schema":  "maclaw.app.result.v1",
				"primary": "approval_result",
				"types":   []any{"approval_result", "document", "inline_content"},
			},
			"test_evidence": map[string]any{
				"primary_result": "approval_result",
				"approval_instance": map[string]any{
					"approval_id":          "approval-expense-1",
					"workflow_instance_id": "workflow-expense-1",
					"dataset_id":           "finance.expenses",
					"record_id":            "expense-1",
					"status":               "approved",
					"decision":             "approved",
					"applicant_id":         "employee_1",
					"current_assignee":     "manager_1",
					"current_node":         "expense.result_pack",
				},
				"result_payload": map[string]any{
					"decision":        "approved",
					"business_status": "finance_approved",
				},
				"outputs": []any{
					map[string]any{"kind": "document", "title": "Approval PDF", "status": "ready"},
					map[string]any{"kind": "inline_content", "title": "Decision", "status": "ready"},
				},
				"result_coverage": map[string]any{
					"primary":       "approval_result",
					"covered_types": []any{"approval_result", "document", "inline_content"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("UpsertAppInstallation approval: %v", err)
	}
	_, err = svc.UpsertAppInstallation(context.Background(), principal, "expense.rejected", UpsertAppInstallationInput{
		AppID: "expense.rejected",
		Name:  "Rejected Expense",
		Kind:  "enterprise_approval_app",
		Metadata: map[string]any{
			"test_evidence_approval_status": "rejected",
			"test_evidence_approval_instance": map[string]any{
				"status":       "rejected",
				"decision":     "rejected",
				"applicant_id": "employee_2",
				"approver_id":  "manager_2",
			},
			"test_evidence_primary_result": "approval_result",
		},
	})
	if err != nil {
		t.Fatalf("UpsertAppInstallation rejected: %v", err)
	}

	cases := []struct {
		name string
		in   QueryAppInstallationsInput
	}{
		{name: "approval status", in: QueryAppInstallationsInput{ApprovalStatus: "approved"}},
		{name: "approval decision", in: QueryAppInstallationsInput{ApprovalDecision: "approved"}},
		{name: "applicant", in: QueryAppInstallationsInput{ApplicantID: "employee_1"}},
		{name: "approver", in: QueryAppInstallationsInput{ApproverID: "manager_1"}},
		{name: "approval id", in: QueryAppInstallationsInput{ApprovalID: "approval-expense-1"}},
		{name: "workflow instance id", in: QueryAppInstallationsInput{WorkflowInstanceID: "workflow-expense-1"}},
		{name: "dataset id", in: QueryAppInstallationsInput{DatasetID: "finance.expenses"}},
		{name: "record id", in: QueryAppInstallationsInput{RecordID: "expense-1"}},
		{name: "result type", in: QueryAppInstallationsInput{ResultType: "document"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items, err := svc.ListAppInstallations(context.Background(), principal, tc.in)
			if err != nil {
				t.Fatalf("ListAppInstallations: %v", err)
			}
			if len(items) != 1 || items[0].AppID != "expense.approval" {
				t.Fatalf("expected expense.approval for %s, got %#v", tc.name, items)
			}
		})
	}
}

func TestUpsertAppInstallationPromotesNestedApprovalInstanceResultPackage(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	principal := Principal{TenantID: "tenant_1", UserID: "user_1", Role: "data_admin"}

	app, err := svc.UpsertAppInstallation(context.Background(), principal, "expense.attention", UpsertAppInstallationInput{
		AppID: "expense.attention",
		Name:  "Expense Attention",
		Kind:  "enterprise_approval_app",
		Metadata: map[string]any{
			"test_evidence": map[string]any{
				"run_id": "run-attention-1",
				"approval_instance": map[string]any{
					"workflow_instance_id":            "wf-attention-1",
					"approval_id":                     "approval-attention-1",
					"record_id":                       "expense-attention-1",
					"status":                          "attention",
					"current_node":                    "expense.attention",
					"workflow_skill_id":               "expense-workflow",
					"approval_instance_view_verified": true,
					"result_payload": map[string]any{
						"approval_result":    "attention",
						"business_status":    "workflow_error",
						"result_status":      "workflow_error",
						"text":               "policy engine failed",
						"workflow_lifecycle": "error",
					},
					"outputs": []any{
						map[string]any{"kind": "document", "title": "Attention report", "status": "ready"},
					},
					"artifacts": []any{
						map[string]any{"id": "attention-log", "name": "attention-log.txt", "uri": "artifact://attention-log"},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("UpsertAppInstallation: %v", err)
	}
	evidence, ok := app.Metadata["test_evidence"].(map[string]any)
	if !ok {
		t.Fatalf("metadata missing normalized test evidence: %#v", app.Metadata)
	}
	payload, ok := evidence["result_payload"].(map[string]any)
	if !ok || payload["approval_result"] != "attention" || payload["workflow_lifecycle"] != "error" {
		t.Fatalf("nested approval result payload was not promoted: %#v", evidence)
	}
	if app.Metadata["test_evidence_result_payload"] == nil || !appInstallationNumberEquals(app.Metadata["test_evidence_output_count"], 1) || !appInstallationNumberEquals(app.Metadata["test_evidence_artifact_count"], 1) {
		t.Fatalf("metadata missing promoted result package summary: %#v", app.Metadata)
	}
	outputs, ok := evidence["outputs"].([]any)
	if !ok || len(outputs) != 1 {
		t.Fatalf("nested approval outputs were not promoted: %#v", evidence)
	}
	artifacts, ok := evidence["artifacts"].([]any)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("nested approval artifacts were not promoted: %#v", evidence)
	}

	byResultType, err := svc.ListAppInstallations(context.Background(), principal, QueryAppInstallationsInput{ResultType: "document"})
	if err != nil {
		t.Fatalf("ListAppInstallations by result type: %v", err)
	}
	if len(byResultType) != 1 || byResultType[0].AppID != "expense.attention" {
		t.Fatalf("expected promoted nested outputs to support result_type=document, got %#v", byResultType)
	}
}

func TestListAppInstallationsFiltersLegacyNestedApprovalResultPackage(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	principal := Principal{TenantID: "tenant_1", UserID: "user_1", Role: "data_admin"}

	_, err = svc.UpsertAppInstallation(context.Background(), principal, "legacy.attention", UpsertAppInstallationInput{
		AppID: "legacy.attention",
		Name:  "Legacy Attention",
		Kind:  "enterprise_approval_app",
		Metadata: map[string]any{
			"test_evidence_approval_instance": map[string]any{
				"workflow_instance_id": "legacy-wf-1",
				"approval_id":          "legacy-approval-1",
				"record_id":            "legacy-record-1",
				"status":               "attention",
				"result_payload": map[string]any{
					"approval_result":    "attention",
					"business_status":    "workflow_error",
					"workflow_lifecycle": "error",
				},
				"outputs": []any{
					map[string]any{"kind": "legacy_attention_document", "title": "Legacy attention report"},
				},
				"artifacts": []any{
					map[string]any{"kind": "legacy_log", "name": "legacy-attention.log"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("UpsertAppInstallation legacy: %v", err)
	}

	cases := []struct {
		name string
		in   QueryAppInstallationsInput
	}{
		{name: "nested output result type", in: QueryAppInstallationsInput{ResultType: "legacy_attention_document"}},
		{name: "nested artifact result type", in: QueryAppInstallationsInput{ResultType: "legacy_log"}},
		{name: "nested approval decision", in: QueryAppInstallationsInput{ApprovalDecision: "attention"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items, err := svc.ListAppInstallations(context.Background(), principal, tc.in)
			if err != nil {
				t.Fatalf("ListAppInstallations: %v", err)
			}
			if len(items) != 1 || items[0].AppID != "legacy.attention" {
				t.Fatalf("expected legacy.attention for %s, got %#v", tc.name, items)
			}
		})
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
	summaryStatusMapping, ok := installed.Metadata["workflow_contract_status_mapping"].(map[string]any)
	if !ok || summaryStatusMapping["pending"] != "approval_pending" || summaryStatusMapping["requiresInput"] != "requires_input" {
		t.Fatalf("expected workflow contract status mapping summary: %#v", installed.Metadata)
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
		"dependency_verification",
		"test_evidence_test_protocol",
		"test_evidence_dependency_count",
		"test_evidence_result_coverage_ok",
		"result_contract_delivery",
		"result_contract_delivery_modes",
		"result_contract_delivery_inline_content",
		"result_contract_delivery_artifacts",
		"result_contract_delivery_business_record",
		"result_contract_delivery_notifications",
		"version_snapshot",
		"workflow_contract",
		"workflow_contract_required_inputs",
		"workflow_contract_decision_outputs",
		"workflow_contract_status_mapping",
	} {
		if _, ok := metadata[key]; !ok {
			t.Fatalf("expected OpenAPI app installation metadata schema to document %s: %#v", key, metadata)
		}
	}
	for _, key := range []string{"test_evidence_outputs", "test_evidence_artifacts", "test_evidence_result_payload"} {
		field, ok := metadata[key].(map[string]interface{})
		if !ok {
			t.Fatalf("expected OpenAPI schema object for %s: %#v", key, metadata[key])
		}
		description, _ := field["description"].(string)
		if !strings.Contains(description, "promoted from approval_instance") {
			t.Fatalf("expected %s description to document approval_instance promotion, got %q", key, description)
		}
	}
	testProtocol, ok := metadata["test_evidence_test_protocol"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected test_evidence_test_protocol schema object: %#v", metadata["test_evidence_test_protocol"])
	}
	if description, _ := testProtocol["description"].(string); !strings.Contains(description, "App Studio test protocol") {
		t.Fatalf("expected test_evidence_test_protocol description to document App Studio protocol, got %q", description)
	}
	testProtocolProperties, ok := testProtocol["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected test_evidence_test_protocol schema properties: %#v", testProtocol)
	}
	for _, key := range []string{"schema", "fingerprint", "sample_input", "expected_output", "required_roles", "required_scopes", "risk_level"} {
		if _, ok := testProtocolProperties[key]; !ok {
			t.Fatalf("expected test_evidence_test_protocol schema to document %s: %#v", key, testProtocolProperties)
		}
	}
	resultContract, ok := metadata["result_contract"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected result_contract schema object: %#v", metadata["result_contract"])
	}
	if description, _ := resultContract["description"].(string); !strings.Contains(description, "output/result contract") {
		t.Fatalf("expected result_contract description to document output/result contract, got %q", description)
	}
	resultContractProperties, ok := resultContract["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected result_contract schema properties: %#v", resultContract)
	}
	for _, key := range []string{"schema", "primary", "types", "output_modes", "approval_decisions", "delivery"} {
		if _, ok := resultContractProperties[key]; !ok {
			t.Fatalf("expected result_contract schema to document %s: %#v", key, resultContractProperties)
		}
	}
	resultDelivery, ok := resultContractProperties["delivery"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected result_contract.delivery schema object: %#v", resultContractProperties["delivery"])
	}
	resultDeliveryProperties, ok := resultDelivery["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected result_contract.delivery properties: %#v", resultDelivery)
	}
	for _, key := range []string{"inlineContent", "artifacts", "businessRecord", "notifications"} {
		if _, ok := resultDeliveryProperties[key]; !ok {
			t.Fatalf("expected result_contract.delivery schema to document %s: %#v", key, resultDeliveryProperties)
		}
	}
	versionSnapshot, ok := metadata["version_snapshot"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected version_snapshot schema object: %#v", metadata["version_snapshot"])
	}
	if description, _ := versionSnapshot["description"].(string); !strings.Contains(description, "dependency version snapshot") {
		t.Fatalf("expected version_snapshot description to document dependency version snapshot, got %q", description)
	}
	versionSnapshotProperties, ok := versionSnapshot["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected version_snapshot schema properties: %#v", versionSnapshot)
	}
	for _, key := range []string{"app_entry_version", "app_skill", "workflow_skills", "approval_bindings"} {
		if _, ok := versionSnapshotProperties[key]; !ok {
			t.Fatalf("expected version_snapshot schema to document %s: %#v", key, versionSnapshotProperties)
		}
	}
	appSkill, ok := versionSnapshotProperties["app_skill"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected version_snapshot.app_skill schema object: %#v", versionSnapshotProperties["app_skill"])
	}
	appSkillProperties, ok := appSkill["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected version_snapshot.app_skill properties: %#v", appSkill)
	}
	for _, key := range []string{"id", "version", "kind", "source"} {
		if _, ok := appSkillProperties[key]; !ok {
			t.Fatalf("expected version_snapshot app skill schema to document %s: %#v", key, appSkillProperties)
		}
	}
	approvalBindings, ok := versionSnapshotProperties["approval_bindings"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected version_snapshot.approval_bindings schema object: %#v", versionSnapshotProperties["approval_bindings"])
	}
	approvalBindingItems, ok := approvalBindings["items"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected version_snapshot.approval_bindings item schema: %#v", approvalBindings)
	}
	approvalBindingProperties, ok := approvalBindingItems["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected version_snapshot approval binding item properties: %#v", approvalBindingItems)
	}
	for _, key := range []string{"event", "object_role", "workflow_skill_id", "workflow_version"} {
		if _, ok := approvalBindingProperties[key]; !ok {
			t.Fatalf("expected version_snapshot approval binding schema to document %s: %#v", key, approvalBindingProperties)
		}
	}
	workflowContract, ok := metadata["workflow_contract"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected workflow_contract schema object: %#v", metadata["workflow_contract"])
	}
	if description, _ := workflowContract["description"].(string); !strings.Contains(description, "Approval workflow Skill contract") {
		t.Fatalf("expected workflow_contract description to document approval workflow Skill contract, got %q", description)
	}
	workflowContractProperties, ok := workflowContract["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected workflow_contract schema properties: %#v", workflowContract)
	}
	for _, key := range []string{"schema", "workflowSkillId", "workflowVersion", "objectRole", "requiredInputs", "decisionOutputs", "statusMapping"} {
		if _, ok := workflowContractProperties[key]; !ok {
			t.Fatalf("expected workflow_contract schema to document %s: %#v", key, workflowContractProperties)
		}
	}
	workflowContractStatusMapping, ok := workflowContractProperties["statusMapping"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected workflow_contract.statusMapping schema object: %#v", workflowContractProperties["statusMapping"])
	}
	workflowContractStatusMappingProperties, ok := workflowContractStatusMapping["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected workflow_contract.statusMapping properties: %#v", workflowContractStatusMapping)
	}
	for _, key := range []string{"pending", "approved", "rejected", "attention", "requiresInput"} {
		if _, ok := workflowContractStatusMappingProperties[key]; !ok {
			t.Fatalf("expected workflow_contract.statusMapping schema to document %s: %#v", key, workflowContractStatusMappingProperties)
		}
	}
	workspaceLayout, ok := metadata["workspace_layout"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected workspace_layout schema object: %#v", metadata["workspace_layout"])
	}
	if description, _ := workspaceLayout["description"].(string); !strings.Contains(description, "workspace_layout.regions") {
		t.Fatalf("expected workspace_layout description to document preserved regions, got %q", description)
	}
	workspaceRegionIDs, ok := metadata["workspace_layout_region_ids"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected workspace_layout_region_ids schema object: %#v", metadata["workspace_layout_region_ids"])
	}
	if description, _ := workspaceRegionIDs["description"].(string); !strings.Contains(description, "workspace_layout.regions") {
		t.Fatalf("expected workspace_layout_region_ids description to document preserved regions, got %q", description)
	}
	workflowMapping, ok := metadata["workflow_mapping"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected workflow_mapping schema object: %#v", metadata["workflow_mapping"])
	}
	if description, _ := workflowMapping["description"].(string); !strings.Contains(description, "App Studio workflow_mapping") {
		t.Fatalf("expected workflow_mapping description to document App Studio mapping, got %q", description)
	}
	workflowMappingProperties, ok := workflowMapping["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected workflow_mapping schema properties: %#v", workflowMapping)
	}
	for _, key := range []string{"schema", "submitNode", "approvalNode", "resultNode", "attentionNode", "statusMapping"} {
		if _, ok := workflowMappingProperties[key]; !ok {
			t.Fatalf("expected workflow_mapping schema to document %s: %#v", key, workflowMappingProperties)
		}
	}
	statusMapping, ok := workflowMappingProperties["statusMapping"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected workflow_mapping.statusMapping schema object: %#v", workflowMappingProperties["statusMapping"])
	}
	statusMappingProperties, ok := statusMapping["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected workflow_mapping.statusMapping properties: %#v", statusMapping)
	}
	for _, key := range []string{"pending", "approved", "rejected", "attention", "requiresInput"} {
		if _, ok := statusMappingProperties[key]; !ok {
			t.Fatalf("expected workflow_mapping.statusMapping schema to document %s: %#v", key, statusMappingProperties)
		}
	}
	for _, key := range []string{
		"test_evidence_approval_current_node",
		"test_evidence_workflow_skill_id",
		"test_evidence_workflow_version",
		"test_evidence_business_status",
		"test_evidence_result_status",
		"test_evidence_dataset_id",
		"test_evidence_blueprint_id",
		"test_evidence_object_role",
		"test_evidence_approval_event",
		"test_evidence_approval_workflow_id",
		"test_evidence_detail_url",
	} {
		if _, ok := metadata[key]; !ok {
			t.Fatalf("expected app installation metadata schema to document %s: %#v", key, metadata)
		}
	}
	dependencyVerification, ok := metadata["dependency_verification"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected dependency_verification schema object: %#v", metadata["dependency_verification"])
	}
	dependencyVerificationProperties, ok := dependencyVerification["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected dependency_verification schema properties: %#v", dependencyVerification)
	}
	for _, key := range []string{
		"verified_at",
		"dependencies",
		"dependency_count",
		"has_missing_required",
		"has_blocking_dependency",
		"has_governance_review_issue",
		"governance_review_issue_count",
		"has_workflow_contract_issue",
		"workflow_contract_issue_count",
	} {
		if _, ok := dependencyVerificationProperties[key]; !ok {
			t.Fatalf("expected dependency_verification schema to document %s: %#v", key, dependencyVerificationProperties)
		}
	}
	dependencies, ok := dependencyVerificationProperties["dependencies"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected dependency_verification.dependencies schema object: %#v", dependencyVerificationProperties["dependencies"])
	}
	dependencyItems, ok := dependencies["items"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected dependency_verification.dependencies item schema: %#v", dependencies)
	}
	dependencyProperties, ok := dependencyItems["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected dependency_verification dependency item properties: %#v", dependencyItems)
	}
	for _, key := range []string{"id", "version", "kind", "source", "install_ref", "required", "installed", "health", "action", "app_ids", "installed_status", "message"} {
		if _, ok := dependencyProperties[key]; !ok {
			t.Fatalf("expected dependency item schema to document %s: %#v", key, dependencyProperties)
		}
	}
}
func TestUpsertAppInstallationBuildsApprovalEvidenceFromFlatSummaries(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	principal := Principal{TenantID: "tenant_1", UserID: "auditor_1", Role: "data_admin"}

	installed, err := svc.UpsertAppInstallation(context.Background(), principal, "expense.approval.flat", UpsertAppInstallationInput{
		AppID:  "expense.approval.flat",
		Name:   "Flat Approval Evidence",
		Kind:   "enterprise_approval_app",
		Source: "hub",
		Metadata: map[string]any{
			"test_evidence_run_id":                    "run-flat-approval",
			"test_evidence_approval_id":               "approval-flat-1",
			"test_evidence_record_id":                 "expense-flat-1",
			"test_evidence_approval_status":           "approved",
			"test_evidence_approval_current_node":     "expense.result",
			"test_evidence_workflow_skill_id":         "expense-workflow",
			"test_evidence_workflow_version":          "2.1.0",
			"test_evidence_business_status":           "finance_approved",
			"test_evidence_result_status":             "approved",
			"test_evidence_dataset_id":                "finance.expenses",
			"test_evidence_blueprint_id":              "finance.expense.approval",
			"test_evidence_object_role":               "expense_report",
			"test_evidence_approval_event":            "expense.submitted",
			"test_evidence_approval_workflow_id":      "expense-flow",
			"test_evidence_detail_url":                "https://datasrv.test/approvals/approval-flat-1",
			"test_evidence_approval_view_verified":    true,
			"test_evidence_test_protocol_fingerprint": "proto-flat-approval",
		},
	})
	if err != nil {
		t.Fatalf("UpsertAppInstallation: %v", err)
	}
	metadata := installed.Metadata
	for key, want := range map[string]any{
		"test_evidence_approval_instance_id":   "approval-flat-1",
		"test_evidence_approval_id":            "approval-flat-1",
		"test_evidence_record_id":              "expense-flat-1",
		"test_evidence_approval_status":        "approved",
		"test_evidence_approval_current_node":  "expense.result",
		"test_evidence_workflow_skill_id":      "expense-workflow",
		"test_evidence_workflow_version":       "2.1.0",
		"test_evidence_business_status":        "finance_approved",
		"test_evidence_result_status":          "approved",
		"test_evidence_dataset_id":             "finance.expenses",
		"test_evidence_blueprint_id":           "finance.expense.approval",
		"test_evidence_object_role":            "expense_report",
		"test_evidence_approval_event":         "expense.submitted",
		"test_evidence_approval_workflow_id":   "expense-flow",
		"test_evidence_detail_url":             "https://datasrv.test/approvals/approval-flat-1",
		"test_evidence_approval_view_verified": true,
	} {
		if got := metadata[key]; got != want {
			t.Fatalf("metadata[%s] = %#v, want %#v; metadata=%#v", key, got, want, metadata)
		}
	}
	auditMetadata := appInstallationAuditMetadata(*installed)
	for key, want := range map[string]any{
		"test_evidence_approval_current_node": "expense.result",
		"test_evidence_workflow_skill_id":     "expense-workflow",
		"test_evidence_business_status":       "finance_approved",
		"test_evidence_result_status":         "approved",
		"test_evidence_dataset_id":            "finance.expenses",
		"test_evidence_object_role":           "expense_report",
		"test_evidence_approval_event":        "expense.submitted",
		"test_evidence_detail_url":            "https://datasrv.test/approvals/approval-flat-1",
	} {
		if got := auditMetadata[key]; got != want {
			t.Fatalf("audit metadata[%s] = %#v, want %#v; audit=%#v", key, got, want, auditMetadata)
		}
	}
	evidence, ok := metadata["test_evidence"].(map[string]any)
	if !ok {
		t.Fatalf("expected normalized test evidence: %#v", metadata)
	}
	approval, ok := evidence["approval_instance"].(map[string]any)
	if !ok {
		t.Fatalf("expected synthesized approval instance: %#v", evidence)
	}
	for key, want := range map[string]any{
		"approval_id":                     "approval-flat-1",
		"record_id":                       "expense-flat-1",
		"status":                          "approved",
		"current_node":                    "expense.result",
		"workflow_skill_id":               "expense-workflow",
		"workflow_version":                "2.1.0",
		"business_status":                 "finance_approved",
		"result_status":                   "approved",
		"dataset_id":                      "finance.expenses",
		"blueprint_id":                    "finance.expense.approval",
		"object_role":                     "expense_report",
		"approval_event":                  "expense.submitted",
		"approval_workflow_id":            "expense-flow",
		"detail_url":                      "https://datasrv.test/approvals/approval-flat-1",
		"approval_instance_view_verified": true,
	} {
		if got := approval[key]; got != want {
			t.Fatalf("approval[%s] = %#v, want %#v; approval=%#v", key, got, want, approval)
		}
	}
}
