package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestApplyMaclawAppDataSrvTestEvidenceMetadataPromotesNestedApprovalResultPackage(t *testing.T) {
	metadata := map[string]interface{}{}
	testEvidence := map[string]any{
		"runId": "run-attention-1",
		"approvalInstance": map[string]any{
			"instanceId":                   "wf-attention-1",
			"approvalID":                   "approval-attention-1",
			"recordID":                     "expense-attention-1",
			"status":                       "attention",
			"currentNode":                  "expense.attention",
			"workflowSkillId":              "expense-workflow",
			"businessStatus":               "workflow_error",
			"resultStatus":                 "workflow_error",
			"approvalInstanceViewVerified": true,
			"resultPayload": map[string]any{
				"approval_result":    "attention",
				"business_status":    "workflow_error",
				"result_status":      "workflow_error",
				"text":               "policy engine failed",
				"workflow_lifecycle": "error",
			},
			"outputs": []any{
				map[string]any{"kind": "approval_result", "text": "policy engine failed", "status": "attention"},
			},
			"artifacts": []any{
				map[string]any{"id": "attention-log", "name": "attention-log.txt", "uri": "artifact://attention-log"},
			},
		},
	}

	applyMaclawAppDataSrvTestEvidenceMetadata(metadata, testEvidence)

	if metadata["test_evidence_run_id"] != "run-attention-1" || metadata["test_evidence_approval_instance_id"] != "wf-attention-1" || metadata["test_evidence_approval_id"] != "approval-attention-1" || metadata["test_evidence_record_id"] != "expense-attention-1" || metadata["test_evidence_approval_status"] != "attention" || metadata["test_evidence_approval_view_verified"] != true {
		t.Fatalf("metadata missing nested approval identity summary: %#v", metadata)
	}
	payload, ok := metadata["test_evidence_result_payload"].(map[string]any)
	if !ok || payload["approval_result"] != "attention" || payload["business_status"] != "workflow_error" || payload["workflow_lifecycle"] != "error" {
		t.Fatalf("metadata did not promote nested approval result payload: %#v", metadata)
	}
	outputs, ok := metadata["test_evidence_outputs"].([]any)
	if !ok || len(outputs) != 1 || metadata["test_evidence_output_count"] != 1 {
		t.Fatalf("metadata did not promote nested approval outputs: %#v", metadata)
	}
	artifacts, ok := metadata["test_evidence_artifacts"].([]any)
	if !ok || len(artifacts) != 1 || metadata["test_evidence_artifact_count"] != 1 {
		t.Fatalf("metadata did not promote nested approval artifacts: %#v", metadata)
	}
}

func TestSubmitMaclawAppPackageFlagsDependencyVerificationWorkflowIssueDetails(t *testing.T) {
	tmpHome := t.TempDir()
	workflowDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "expense-flow")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatalf("MkdirAll workflowDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowDir, "skill.md"), []byte("# Expense flow\n"), 0o644); err != nil {
		t.Fatalf("WriteFile workflow skill.md: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "expense-flow", SkillDir: workflowDir, Status: "active", HubVersion: "2.1.0"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	pkg := `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "approval-verification-issue",
				"name": "Approval Verification Issue",
				"kind": "enterprise_approval_app",
				"binding": {
					"datasrv": {"domain": "finance", "datasetID": "finance.expenses", "objectRole": "expense_report"},
					"mis": {"approvalBindings": [{"event": "expense.submitted", "workflowSkillId": "expense-flow", "workflowVersion": "2.1.0", "objectRole": "expense_report"}]}
				},
				"dependencies": {"skills": [{"id": "expense-flow", "kind": "workflow_skill", "version": "2.1.0", "required": true, "source": "hub"}]},
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"approval_workspace", "template":"dashboard", "regionCount":4},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"approval_result", "types":["approval_result", "business_status", "content"]},
					"workflowContract": {"schema":"maclaw.app.workflow_contract.v1", "workflowSkillId":"expense-flow", "workflowVersion":"2.1.0", "objectRole":"expense_report", "requiredInputs":["record_ref", "applicant", "business_payload"], "decisionOutputs":["approved", "rejected", "attention"], "statusMapping":{"pending":"pending", "approved":"approved", "rejected":"rejected", "attention":"attention"}},
				"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-approval","sampleInput":{"amount":10},"expectedOutput":{"decision":"approved"},"requiredRoles":["applicant"],"requiredScopes":["approval.submit"],"riskLevel":"medium"}, "testProtocolFingerprint":"proto-approval", "runId":"run-approval", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"decision":"approved", "business_status":"approved"}, "approvalInstance":{"instanceId":"wf-test-1", "approvalID":"approval-remote-install-1", "recordID":"expense-1", "status":"approved", "currentNode":"expense.result", "workflowSkillId":"expense-flow", "workflowVersion":"2.1.0", "businessStatus":"approved", "resultStatus":"approved", "resultPayload":{"decision":"approved", "business_status":"approved"}, "outputs":[{"kind":"approval_result", "title":"Approval decision", "text":"approved", "status":"approved"}], "viewVerified":true}, "resultCoverage":{"ok":true, "primary":"approval_result", "coveredTypes":["approval_result", "business_status", "content"], "missingTypes":[]}},
					"dependencyVerification": {"schema":"maclaw.app.install_plan.v1", "hasWorkflowContractIssue":true, "workflowContractIssueCount":1, "workflowContractIssues":[{"path":"apps[0].app.governance.workflowContract.workflowSkillId", "severity":"error", "message":"approval workflow contract does not match approval binding", "suggestion":"align workflowSkillId", "metadata":{"workflow_skill_id":"expense-flow", "required_version":"2.1.0", "installed_version":"2.0.0"}}], "dependencies":[{"id":"expense-flow", "kind":"workflow_skill", "version":"2.1.0", "required":true, "installed":true, "health":"ready", "action":"skip"}]}
				}
			}
		}]
	}`

	pkg = maclawAppPackageWithCurrentDefinitionHashes(t, pkg)
	result := map[string]any{}
	assertSubmitMaclawAppPackageBlocked(t, app, pkg, "approval workflow contract verification failed")
	return
	if result["review_issue_count"] != 1 {
		t.Fatalf("expected one dependency workflow issue, got %#v", result)
	}
	detail, err := app.GetMaclawAppPackageSubmission(result["submission_id"].(string))
	if err != nil {
		t.Fatalf("GetMaclawAppPackageSubmission error: %v", err)
	}
	if len(detail.ReviewIssues) != 1 {
		t.Fatalf("expected one review issue: %#v", detail.ReviewIssues)
	}
	issue := detail.ReviewIssues[0]
	if issue.Path != "apps[0].app.governance.dependencyVerification.workflowContractIssues" || !strings.Contains(issue.Message, "approval workflow contract verification failed: approval workflow contract does not match approval binding") {
		t.Fatalf("unexpected dependency verification workflow issue: %#v", issue)
	}
	if issue.Suggestion != "align workflowSkillId" || issue.Metadata["workflow_skill_id"] != "expense-flow" || issue.Metadata["installed_version"] != "2.0.0" {
		t.Fatalf("expected carried issue detail metadata: %#v", issue)
	}
}

func TestSubmitMaclawAppPackageValidatesApprovalWorkflowContract(t *testing.T) {
	tmpHome := t.TempDir()
	workflowSkillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "expense-flow")
	if err := os.MkdirAll(workflowSkillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll workflowSkillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowSkillDir, "skill.md"), []byte("# Expense workflow\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "expense-flow", SkillDir: workflowSkillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	base := func(contract string) string {
		return `{
			"schema": "maclaw.app.pack.v1",
			"privateMarker": "x_maclaw_apps",
			"apps": [{
				"schema": "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"app": {
					"id": "approval-contract",
					"name": "Approval Contract",
					"kind": "enterprise_approval_app",
					"binding": {
						"datasrv": {"domain":"finance", "datasetID":"finance.expenses", "objectRole":"expense_report"},
						"mis": {"approvalBindings": [{"event":"expense.submitted", "workflowSkillId":"expense-flow", "workflowVersion":"1.0.0", "objectRole":"expense_report"}]}
					},
					"governance": {
						"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"approval_workspace", "template":"classic_split", "regionCount":4},
						"resultContract": {"schema":"maclaw.app.result.v1", "primary":"approval_result", "types":["approval_result", "business_status", "content"]},
						"dependencyVerification": {"schema":"maclaw.app.install_plan.v1", "dependencies":[{"id":"expense-flow", "kind":"workflow_skill", "installed":true, "health":"ready"}]},
						"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-basic","sampleInput":{"sample":true},"expectedOutput":{"status":"ok"},"requiredRoles":["tester"],"requiredScopes":["app.run"],"riskLevel":"low"}, "testProtocolFingerprint":"proto-basic", "runId":"run-approval", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"approval_result":"approved"}, "approvalInstance":{"instanceId":"wf-test-1", "approvalID":"approval-remote-install-1", "recordID":"expense-1", "status":"approved", "currentNode":"expense.result", "workflowSkillId":"expense-flow", "workflowVersion":"1.0.0", "businessStatus":"approved", "resultStatus":"approved", "resultPayload":{"approval_result":"approved"}, "outputs":[{"kind":"approval_result", "title":"Approval decision", "text":"approved", "status":"approved"}], "viewVerified":true}}` + contract + `
					}
				}
			}]
		}`
	}

	cases := []struct {
		name       string
		contract   string
		wantPath   string
		wantIssues int
	}{
		{name: "missing", wantPath: "apps[0].app.governance.workflowContract", wantIssues: 1},
		{name: "mismatched workflow", contract: `, "workflowContract":{"schema":"maclaw.app.workflow_contract.v1", "workflowSkillId":"other-flow", "objectRole":"expense_report", "requiredInputs":["record_ref", "applicant", "business_payload"], "decisionOutputs":["approved", "rejected", "attention"], "statusMapping":{"pending":"approval_pending", "approved":"approved", "rejected":"rejected", "attention":"attention"}}`, wantPath: "apps[0].app.governance.workflowContract.workflowSkillId", wantIssues: 1},
		{name: "complete", contract: `, "workflowContract":{"schema":"maclaw.app.workflow_contract.v1", "workflowSkillId":"expense-flow", "workflowVersion":"1.0.0", "objectRole":"expense_report", "requiredInputs":["record_ref", "applicant", "business_payload"], "decisionOutputs":["approved", "rejected", "attention"], "statusMapping":{"pending":"approval_pending", "approved":"approved", "rejected":"rejected", "attention":"attention"}}`, wantIssues: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pkg := maclawAppPackageWithCurrentDefinitionHashes(t, base(tc.contract))
			if tc.wantIssues > 0 {
				assertSubmitMaclawAppPackageBlocked(t, app, pkg, tc.wantPath)
				return
			}
			result, err := app.SubmitMaclawAppPackage(pkg)
			if err != nil {
				t.Fatalf("SubmitMaclawAppPackage error: %v", err)
			}
			if result["review_issue_count"] != tc.wantIssues {
				t.Fatalf("expected %d workflow contract issue(s), got %#v", tc.wantIssues, result)
			}
			detail, err := app.GetMaclawAppPackageSubmission(result["submission_id"].(string))
			if err != nil {
				t.Fatalf("GetMaclawAppPackageSubmission error: %v", err)
			}
			if len(detail.ReviewIssues) != tc.wantIssues {
				t.Fatalf("expected %d durable issue(s), got %#v", tc.wantIssues, detail.ReviewIssues)
			}
		})
	}
}

func TestSubmitMaclawAppPackageRequiresApprovalInstanceTestEvidence(t *testing.T) {
	tmpHome := t.TempDir()
	workflowSkillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "expense-flow")
	if err := os.MkdirAll(workflowSkillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll workflowSkillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowSkillDir, "skill.md"), []byte("# Expense workflow\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "expense-flow", SkillDir: workflowSkillDir, Status: "active", HubVersion: "1.0.0"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	pkg := `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "approval-instance-evidence",
				"name": "Approval Instance Evidence",
				"kind": "enterprise_approval_app",
				"binding": {
					"datasrv": {"domain":"finance", "datasetID":"finance.expenses", "objectRole":"expense_report"},
					"workflow": {"submitNode":"expense.intake", "approvalNode":"manager_approval", "resultNode":"expense.result"},
					"mis": {"approvalBindings": [{"event":"expense.submitted", "workflowSkillId":"expense-flow", "workflowVersion":"1.0.0", "objectRole":"expense_report"}]}
				},
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"approval_workspace", "template":"classic_split", "regionCount":4, "regions":[{"role":"input"},{"role":"instance_list"},{"role":"output"}]},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"approval_result", "types":["approval_result", "business_status", "content"]},
					"dependencyVerification": {"schema":"maclaw.app.install_plan.v1", "hasMissingRequired":false, "hasBlockingDependency":false, "dependencies":[{"id":"expense-flow", "kind":"workflow_skill", "required":true, "installed":true, "health":"ready", "action":"skip"}]},
					"workflowContract": {"schema":"maclaw.app.workflow_contract.v1", "workflowSkillId":"expense-flow", "workflowVersion":"1.0.0", "objectRole":"expense_report", "requiredInputs":["record_ref", "applicant", "business_payload"], "decisionOutputs":["approved", "rejected", "attention"], "statusMapping":{"pending":"pending", "approved":"approved", "rejected":"rejected", "attention":"attention"}},
					"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-approval-instance","sampleInput":{"amount":10},"expectedOutput":{"decision":"approved"},"requiredRoles":["applicant"],"requiredScopes":["approval.submit"],"riskLevel":"medium"}, "testProtocolFingerprint":"proto-approval-instance", "runId":"run-approval-instance", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"approval_result":"approved", "business_status":"approved"}, "resultCoverage":{"ok":true, "primary":"approval_result", "coveredTypes":["approval_result", "business_status", "content"], "missingTypes":[]}}
				}
			}
		}]
	}`

	pkg = maclawAppPackageWithCurrentDefinitionHashes(t, pkg)
	result := map[string]any{}
	assertSubmitMaclawAppPackageBlocked(t, app, pkg, "apps[0].app.governance.testEvidence.approvalInstance")
	return
	if result["review_issue_count"] != 1 {
		t.Fatalf("expected one approval instance evidence issue, got %#v", result)
	}
	detail, err := app.GetMaclawAppPackageSubmission(result["submission_id"].(string))
	if err != nil {
		t.Fatalf("GetMaclawAppPackageSubmission error: %v", err)
	}
	if len(detail.ReviewIssues) != 1 || detail.ReviewIssues[0].Path != "apps[0].app.governance.testEvidence.approvalInstance" || !strings.Contains(detail.ReviewIssues[0].Message, "approval instance") {
		t.Fatalf("unexpected approval instance evidence issue: %#v", detail.ReviewIssues)
	}
}

func TestSubmitMaclawAppPackageRequiresApprovalInstanceResultPackage(t *testing.T) {
	tmpHome := t.TempDir()
	workflowSkillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "expense-flow")
	if err := os.MkdirAll(workflowSkillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll workflowSkillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowSkillDir, "skill.md"), []byte("# Expense workflow\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "expense-flow", SkillDir: workflowSkillDir, Status: "active", HubVersion: "1.0.0"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	pkg := `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "approval-instance-result-package",
				"name": "Approval Instance Result Package",
				"kind": "enterprise_approval_app",
				"binding": {
					"datasrv": {"domain":"finance", "datasetID":"finance.expenses", "objectRole":"expense_report"},
					"workflow": {"submitNode":"expense.intake", "approvalNode":"manager_approval", "resultNode":"expense.result"},
					"mis": {"approvalBindings": [{"event":"expense.submitted", "workflowSkillId":"expense-flow", "workflowVersion":"1.0.0", "objectRole":"expense_report"}]}
				},
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"approval_workspace", "template":"classic_split", "regionCount":4, "regions":[{"role":"input"},{"role":"instance_list"},{"role":"output"}]},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"approval_result", "types":["approval_result", "business_status", "content"]},
					"dependencyVerification": {"schema":"maclaw.app.install_plan.v1", "hasMissingRequired":false, "hasBlockingDependency":false, "dependencies":[{"id":"expense-flow", "kind":"workflow_skill", "required":true, "installed":true, "health":"ready", "action":"skip"}]},
					"workflowContract": {"schema":"maclaw.app.workflow_contract.v1", "workflowSkillId":"expense-flow", "workflowVersion":"1.0.0", "objectRole":"expense_report", "requiredInputs":["record_ref", "applicant", "business_payload"], "decisionOutputs":["approved", "rejected", "attention"], "statusMapping":{"pending":"pending", "approved":"approved", "rejected":"rejected", "attention":"attention"}},
					"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-approval-instance-result","sampleInput":{"amount":10},"expectedOutput":{"decision":"approved"},"requiredRoles":["applicant"],"requiredScopes":["approval.submit"],"riskLevel":"medium"}, "testProtocolFingerprint":"proto-approval-instance-result", "runId":"run-approval-instance-result", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"approval_result":"approved", "business_status":"approved"}, "approvalInstance":{"instanceId":"wf-test-1", "approvalID":"approval-remote-1", "recordID":"expense-1", "datasetID":"finance.expenses", "objectRole":"expense_report", "approvalEvent":"expense.submitted", "approvalWorkflowID":"expense-flow", "status":"approved", "currentNode":"expense.result", "workflowSkillId":"expense-flow", "workflowVersion":"1.0.0", "businessStatus":"approved", "resultStatus":"approved", "viewVerified":true}, "resultCoverage":{"ok":true, "primary":"approval_result", "coveredTypes":["approval_result", "business_status", "content"], "missingTypes":[]}}
				}
			}
		}]
	}`

	pkg = maclawAppPackageWithCurrentDefinitionHashes(t, pkg)
	result := map[string]any{}
	assertSubmitMaclawAppPackageBlocked(t, app, pkg, "apps[0].app.governance.testEvidence.approvalInstance.resultPayload")
	return
	if result["review_issue_count"] != 1 {
		t.Fatalf("expected one approval result package issue, got %#v", result)
	}
	detail, err := app.GetMaclawAppPackageSubmission(result["submission_id"].(string))
	if err != nil {
		t.Fatalf("GetMaclawAppPackageSubmission error: %v", err)
	}
	if len(detail.ReviewIssues) != 1 || detail.ReviewIssues[0].Path != "apps[0].app.governance.testEvidence.approvalInstance.resultPayload" || !strings.Contains(detail.ReviewIssues[0].Message, "result package") {
		t.Fatalf("unexpected approval result package issue: %#v", detail.ReviewIssues)
	}
}

func TestPlanMaclawAppInstallChecksInstalledApprovalWorkflowVersion(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	workflowDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "expense-flow")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatalf("MkdirAll workflowDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowDir, "skill.md"), []byte("# Expense flow\n"), 0o644); err != nil {
		t.Fatalf("WriteFile workflow skill.md: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "expense-flow", SkillDir: workflowDir, Status: "active", HubVersion: "2.0.0"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	pkg := `{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "expense-approval",
			"name": "Expense Approval",
			"kind": "enterprise_approval_app",
			"binding": {
				"datasrv": {"domain": "finance", "datasetID": "finance.expenses", "objectRole": "expense_report"},
				"mis": {"approvalBindings": [{"event": "expense.submitted", "workflowSkillId": "expense-flow", "workflowVersion": "2.1.0", "objectRole": "expense_report"}]}
			},
			"dependencies": {"skills": [{"id": "expense-flow", "kind": "workflow_skill", "version": "2.1.0", "required": true, "source": "hub"}]},
			"governance": {
				"workflowContract": {
					"schema": "maclaw.app.workflow_contract.v1",
					"workflowSkillId": "expense-flow",
					"workflowVersion": "2.1.0",
					"objectRole": "expense_report",
					"requiredInputs": ["record_ref", "applicant", "business_payload"],
					"decisionOutputs": ["approved", "rejected", "attention"],
					"statusMapping": {"pending":"pending", "approved":"approved", "rejected":"rejected", "attention":"attention"}
				}
			}
		}
	}`
	plan, err := app.PlanMaclawAppInstall(pkg)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	if dep := maclawAppPlanDepForTest(plan, "expense-flow"); dep == nil || !dep.Installed || dep.Action != "blocked" || dep.Health != "version_mismatch" || dep.VersionStatus != "mismatch" {
		t.Fatalf("workflow dependency should be installed but blocked by version mismatch: %#v", dep)
	} else if dep.RequiredVersion != "2.1.0" || dep.InstalledVersion != "2.0.0" || !strings.Contains(dep.Message, "2.0.0") || !strings.Contains(dep.Message, "2.1.0") {
		t.Fatalf("expected workflow dependency version evidence, got %#v", dep)
	}
	if !plan.HasBlockingDependency || plan.HasMissingRequired {
		t.Fatalf("version mismatch should block required dependency install: %#v", plan)
	}
	if !plan.HasWorkflowContractIssue || len(plan.WorkflowContractIssues) == 0 || !strings.Contains(plan.WorkflowContractIssues[0].Message, "version 2.0.0 does not match required 2.1.0") {
		t.Fatalf("expected installed workflow version mismatch issue: %#v", plan.WorkflowContractIssues)
	}
	issue := plan.WorkflowContractIssues[0]
	if issue.Metadata["workflow_skill_id"] != "expense-flow" || issue.Metadata["required_version"] != "2.1.0" || issue.Metadata["installed_version"] != "2.0.0" {
		t.Fatalf("expected workflow version metadata, got %#v", issue.Metadata)
	}
	if issue.Metadata["binding_event"] != "expense.submitted" || issue.Metadata["object_role"] != "expense_report" || issue.Metadata["health"] != "ready" {
		t.Fatalf("expected workflow binding metadata, got %#v", issue.Metadata)
	}
	installedPlan, err := app.InstallMaclawAppDependencies(pkg)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies mismatch version error = %v", err)
	}
	if !installedPlan.HasWorkflowContractIssue || len(installedPlan.WorkflowContractIssues) == 0 || !strings.Contains(installedPlan.WorkflowContractIssues[0].Message, "version 2.0.0 does not match required 2.1.0") {
		t.Fatalf("install plan should recheck workflow version after dependency refresh: %#v", installedPlan.WorkflowContractIssues)
	}
	if dep := maclawAppPlanDepForTest(installedPlan, "expense-flow"); dep == nil || dep.Action != "blocked" || dep.Health != "version_mismatch" || dep.VersionStatus != "mismatch" {
		t.Fatalf("install plan should preserve dependency version mismatch block: %#v", dep)
	}

	cfg.NLSkills[0].HubVersion = "2.1.0"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig matching version error = %v", err)
	}
	plan, err = app.PlanMaclawAppInstall(pkg)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall matching version error = %v", err)
	}
	if plan.HasWorkflowContractIssue || len(plan.WorkflowContractIssues) != 0 {
		t.Fatalf("matching installed workflow version should not produce contract issues: %#v", plan.WorkflowContractIssues)
	}
	if dep := maclawAppPlanDepForTest(plan, "expense-flow"); dep == nil || dep.Action != "skip" || dep.Health != "ready" || dep.VersionStatus != "matched" || dep.InstalledVersion != "2.1.0" || dep.RequiredVersion != "2.1.0" {
		t.Fatalf("matching workflow dependency should be ready with version evidence: %#v", dep)
	}
	installedPlan, err = app.InstallMaclawAppDependencies(pkg)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies matching version error = %v", err)
	}
	if installedPlan.HasWorkflowContractIssue || len(installedPlan.WorkflowContractIssues) != 0 {
		t.Fatalf("install plan should clear workflow issues after dependency refresh: %#v", installedPlan.WorkflowContractIssues)
	}
}

func TestInstallMaclawAppDependenciesUsesDeclaredCanonicalWorkflowDependencyTarget(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		if source != "skillhub" || id != "Approval Workflow" || installRef != "approval-flow" {
			t.Fatalf("unexpected dependency install call: source=%s id=%s installRef=%s", source, id, installRef)
		}
		skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "approval-flow")
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return err
		}
		cfg, err := app.LoadConfig()
		if err != nil {
			return err
		}
		cfg.NLSkills = append(cfg.NLSkills, corelib.NLSkillEntry{
			Name:       "approval-flow",
			SkillDir:   skillDir,
			Status:     "active",
			Source:     "skillhub",
			HubSkillID: "approval-flow",
			HubVersion: "v2.0.0",
		})
		return app.SaveConfig(cfg)
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "approval-app",
			"name": "Approval App",
			"kind": "enterprise_approval_app",
			"dependencies": { "skills": [
				{ "id": "Approval Workflow", "canonical_id": "approval-flow", "aliases": ["ApprovalFlow"], "version": "2.0.0", "kind": "workflow_skill", "required": true, "source": "hub" }
			] }
		}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "Approval Workflow")
	if dep == nil || dep.CanonicalID != "approval-flow" || dep.InstallRefTarget != "approval-flow" || dep.InstallRefStatus != "ok" {
		t.Fatalf("workflow dependency should use declared canonical target: %#v", dep)
	}
	if dep == nil || !dep.Installed || dep.Action != "installed" || dep.Health != "ready" || dep.InstalledName != "approval-flow" || dep.VersionStatus != "matched" {
		t.Fatalf("workflow dependency should install through declared canonical target: %#v", dep)
	}
	for _, candidate := range maclawAppInstalledSkillCandidateIDs(*dep) {
		if candidate == "approval-flow-runtime" {
			t.Fatalf("workflow canonical target should not add runtime-only local candidate: %#v", maclawAppInstalledSkillCandidateIDs(*dep))
		}
	}
}

func TestMaclawAppDependencyRepairAllowsInstallAndWorkflowRun(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	appSkillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "expense-super-skill")
	if err := os.MkdirAll(appSkillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll appSkillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appSkillDir, "SKILL.md"), []byte("# Expense Super Skill\n"), 0o644); err != nil {
		t.Fatalf("write app skill: %v", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "expense-super-skill", SkillDir: appSkillDir, Status: "active", Source: "enterprise_hub", HubSkillID: "expense-super-skill"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	workflowJSON := `{"approval_instance":{"status":"approved","lane":"handled","workflow_instance_id":"wf-repaired-1","approval_id":"approval-repaired-1","record_id":"expense-repaired-1","dataset_id":"finance.expense_forms","object_role":"expense_report","workflow_skill_id":"expense-workflow","workflow_version":"2.0.0","workflow_node_id":"expense.result","workflow_node_ids":["expense.submit","manager.approval","expense.result"],"workflow_decision_id":"decision-repaired-1","business_status":"finance_approved","result_status":"approved","result":"approved after dependency repair","result_payload":{"approval_result":"approved","business_status":"finance_approved","business_record":{"id":"expense-repaired-1","status":"finance_approved"},"text":"approved after dependency repair"},"outputs":[{"type":"content","title":"Workflow Decision","text":"approved after dependency repair","status":"approved"}]}}`
	workflowResultPath := filepath.Join(tmpHome, "workflow-repaired-result.txt")
	if err := os.WriteFile(workflowResultPath, []byte("workflow_result="+workflowJSON+"\n"), 0o644); err != nil {
		t.Fatalf("write workflow result fixture: %v", err)
	}
	workflowCommand := `cat "` + workflowResultPath + `"`
	if os.PathSeparator == '\\' {
		workflowCommand = `type "` + workflowResultPath + `"`
	}

	packageJSON := maclawAppPackageWithCurrentDefinitionHashes(t, `{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "expense-approval",
			"name": "Expense Approval",
			"version": "1.0.0",
			"kind": "enterprise_approval_app",
			"binding": {
				"appSkill": {"id":"expense-super-skill", "version":"1.0.0", "source":"enterprise_hub", "install_ref":"cap-expense-super-skill"},
				"datasrv": {"domain":"finance", "datasetID":"finance.expense_forms", "objectRole":"expense_report", "blueprintID":"finance.expense.v1"},
				"mis": {"approvalBindings": [{"event":"finance.submitted", "objectRole":"expense_report", "workflowSkillId":"expense-workflow", "workflowVersion":"2.0.0"}]},
				"workflow": {"schema":"maclaw.app.workflow.v1", "submitNode":"expense.submit", "approvalNode":"manager.approval", "resultNode":"expense.result"},
				"dependencies": {"skills": [
					{"id":"expense-super-skill", "kind":"app_skill", "version":"1.0.0", "required":true, "source":"enterprise_hub", "install_ref":"cap-expense-super-skill"},
					{"id":"expense-workflow", "kind":"workflow_skill", "version":"2.0.0", "required":true, "source":"enterprise_hub", "install_ref":"cap-expense-workflow"}
				]}
			},
			"governance": {
				"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"approval_workspace", "template":"left_nav", "regionCount":3, "visibleRegionCount":3, "regions":[{"id":"approval_inbox", "role":"instance_list", "placement":"left"}, {"id":"request_form", "role":"input", "placement":"center"}, {"id":"result_panel", "role":"output", "placement":"bottom"}]},
				"resultContract": {"schema":"maclaw.app.result.v1", "primary":"approval_result", "types":["approval_result","business_status","artifact"]},
				"workflowContract": {"schema":"maclaw.app.workflow_contract.v1", "workflowSkillId":"expense-workflow", "workflowVersion":"2.0.0", "objectRole":"expense_report", "requiredInputs":["record_ref","applicant","business_payload"], "decisionOutputs":["approved","rejected","attention"], "statusMapping":{"pending":"pending","approved":"finance_approved","rejected":"finance_rejected","attention":"finance_attention"}},
				"testProtocol": {"schema":"maclaw.app.test_protocol.v1", "fingerprint":"proto-dependency-repair", "sampleInput":{"amount":640}, "expectedOutput":{"approval_result":"approved"}, "requiredRoles":["tester"], "requiredScopes":["app.run"], "riskLevel":"medium"},
				"dependencyVerification": {"schema":"maclaw.app.install_plan.v1", "dependencyCount":2, "requiredCount":2, "installedCount":2, "missingCount":0, "blockedCount":0, "ok":true, "blocked":false, "dependencies":[{"id":"expense-super-skill", "kind":"app_skill", "required":true, "installed":true, "health":"ready", "action":"skip"}, {"id":"expense-workflow", "kind":"workflow_skill", "required":true, "installed":true, "health":"ready", "action":"installed"}]},
				"testEvidence": {"runId":"run-dependency-repair", "testProtocolFingerprint":"proto-dependency-repair", "resultPayload":{"approval_result":"approved", "business_status":"finance_approved"}, "outputs":[{"kind":"approval_result", "title":"Decision", "text":"approved", "status":"ready"}], "artifacts":[{"id":"dependency-repair-artifact", "name":"dependency-repair.pdf", "uri":"artifact://dependency/repair.pdf", "status":"ready"}], "resultCoverage":{"ok":true, "primary":"approval_result", "coveredTypes":["approval_result","business_status","artifact"], "missingTypes":[]}, "approvalViews":["my_requests","handled"], "approvalInstance":{"instanceId":"wf-dependency-repair-evidence", "approvalID":"approval-dependency-repair-evidence", "recordID":"expense-repaired-evidence", "datasetID":"finance.expense_forms", "objectRole":"expense_report", "workflowSkillId":"expense-workflow", "workflowVersion":"2.0.0", "status":"approved", "currentNode":"expense.result", "businessStatus":"finance_approved", "resultStatus":"approved", "viewVerified":true, "resultPayload":{"approval_result":"approved", "business_status":"finance_approved"}, "outputs":[{"kind":"approval_result", "title":"Decision", "text":"approved", "status":"ready"}], "artifacts":[{"id":"dependency-repair-artifact", "name":"dependency-repair.pdf", "uri":"artifact://dependency/repair.pdf", "status":"ready"}]}}
			}
		}
	}`)

	initialPlan, err := app.PlanMaclawAppInstall(packageJSON)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() initial error = %v", err)
	}
	if !initialPlan.HasMissingRequired || initialPlan.HasBlockingDependency {
		t.Fatalf("initial plan should report missing installable workflow dependency without hard blocking: %#v", initialPlan)
	}
	if dep := maclawAppPlanDepForTest(initialPlan, "expense-workflow"); dep == nil || dep.Action != "install" || dep.Health != "missing" {
		t.Fatalf("workflow dependency should be missing before repair: %#v", dep)
	}
	if _, err := app.RecordMaclawAppInstall(packageJSON, "enterprise_hub"); err == nil || !strings.Contains(err.Error(), "required Skill dependencies") {
		t.Fatalf("RecordMaclawAppInstall should reject missing workflow dependency, got %v", err)
	}

	type installCall struct {
		source     string
		id         string
		installRef string
	}
	var installCalls []installCall
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		installCalls = append(installCalls, installCall{source: source, id: id, installRef: installRef})
		skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", id)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# "+id+"\n"), 0o644); err != nil {
			return err
		}
		cfg, err := app.LoadConfig()
		if err != nil {
			return err
		}
		cfg.NLSkills = append(cfg.NLSkills, corelib.NLSkillEntry{
			Name:       id,
			SkillDir:   skillDir,
			Status:     "active",
			Source:     source,
			HubSkillID: id,
			HubVersion: "2.0.0",
			Steps: []corelib.NLSkillStep{{
				Action:  "bash",
				Params:  map[string]interface{}{"command": workflowCommand},
				Capture: map[string]string{"workflow_result": `workflow_result=(.+)`},
			}},
		})
		return app.SaveConfig(cfg)
	}

	repairedPlan, err := app.InstallMaclawAppDependencies(packageJSON)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	if len(installCalls) != 1 || installCalls[0] != (installCall{source: "enterprise_hub", id: "expense-workflow", installRef: "cap-expense-workflow"}) {
		t.Fatalf("dependency repair should install workflow skill from Hub capability: %#v", installCalls)
	}
	if repairedPlan.HasMissingRequired || repairedPlan.HasBlockingDependency {
		t.Fatalf("repaired plan should clear dependency blockers: %#v", repairedPlan)
	}
	if dep := maclawAppPlanDepForTest(repairedPlan, "expense-workflow"); dep == nil || !dep.Installed || dep.Action != "installed" || dep.Health != "ready" {
		t.Fatalf("workflow dependency should be ready after repair: %#v", dep)
	}

	var captured []struct {
		Method   string
		Path     string
		RawQuery string
		Body     map[string]interface{}
	}
	finalSynced := false
	dataSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := struct {
			Method   string
			Path     string
			RawQuery string
			Body     map[string]interface{}
		}{Method: r.Method, Path: r.URL.Path, RawQuery: r.URL.RawQuery}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		captured = append(captured, item)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/data/app-installations/expense-approval":
			_, _ = w.Write([]byte(`{"app_id":"expense-approval","status":"installed"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-repaired-1":
			_, _ = w.Write([]byte(`{"id":"expense-repaired-1","data":{"status":"draft","amount":640}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-repaired-1":
			_, _ = w.Write([]byte(`{"id":"expense-repaired-1","status":"finance_approved"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-repaired-1/approvals":
			_, _ = w.Write([]byte(`{"id":"approval-repaired-1","status":"pending"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/approvals/approval-repaired-1/review":
			finalSynced = true
			_, _ = w.Write([]byte(`{"id":"approval-repaired-1","status":"approved"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/approvals":
			if finalSynced {
				_, _ = w.Write([]byte(`{"items":[{"id":"approval-repaired-1","app_id":"expense-approval","dataset_id":"finance.expense_forms","record_id":"expense-repaired-1","status":"approved","summary":"Expense Approved After Dependency Repair","workflow_skill_id":"expense-workflow","workflow_version":"2.0.0","workflow_instance_id":"wf-repaired-1","workflow_decision_id":"decision-repaired-1","workflow_node_id":"expense.result","workflow_node_ids":["expense.submit","manager.approval","expense.result"],"business_status":"finance_approved","result_status":"approved","result_payload":{"approval_result":"approved","business_status":"finance_approved","business_record":{"id":"expense-repaired-1","status":"finance_approved"},"text":"approved after dependency repair"},"outputs":[{"type":"content","title":"Workflow Decision","text":"approved after dependency repair","status":"approved"}],"request":{"approval_instance_id":"wf-repaired-1","workflowSkillId":"expense-workflow","workflowVersion":"2.0.0","workflowNodeId":"expense.result"},"created_by":"alice","submitted_by":"alice","reviewed_by":"manager"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			t.Fatalf("unexpected DataSrv request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer dataSrv.Close()
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: dataSrv.URL, Token: "token", TenantID: "tenant", UserID: "alice", Role: "requester"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	record, err := app.RecordMaclawAppInstall(packageJSON, "enterprise_hub")
	if err != nil {
		t.Fatalf("RecordMaclawAppInstall after dependency repair error = %v", err)
	}
	if record["has_missing_required"] == true || record["has_blocking_dependency"] == true {
		t.Fatalf("install record should be unblocked after dependency repair: %#v", record)
	}
	records, err := app.ListMaclawAppInstalls(10)
	if err != nil {
		t.Fatalf("ListMaclawAppInstalls() error = %v", err)
	}
	if len(records) != 1 || records[0].HasMissingRequired || records[0].HasBlockingDependency {
		t.Fatalf("local install audit should preserve repaired dependency state: %#v", records)
	}
	if dep := maclawAppPlanDepForTest(maclawAppInstallPlan{Dependencies: records[0].Dependencies}, "expense-workflow"); dep == nil || !dep.Installed || dep.Health != "ready" {
		t.Fatalf("install audit should record ready workflow dependency: %#v", records[0].Dependencies)
	}

	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	started, err := app.StartMaclawAppApprovalWorkflow(MaclawAppApprovalWorkflowStartInput{
		AppID:            "expense-approval",
		DatasetID:        "finance.expense_forms",
		ObjectRole:       "expense_report",
		RecordID:         "expense-repaired-1",
		Title:            "Expense Approved After Dependency Repair",
		Applicant:        "alice",
		Approver:         "manager",
		BusinessNote:     "run after dependency repair",
		BusinessPayload:  map[string]any{"amount": float64(640)},
		RunWorkflowSkill: true,
	})
	if err != nil {
		t.Fatalf("StartMaclawAppApprovalWorkflow() after dependency repair error = %v", err)
	}
	workflowRun, ok := started["workflow_run"].(map[string]any)
	if !ok || workflowRun["ran"] != true {
		t.Fatalf("expected workflow run after dependency repair: %#v", started["workflow_run"])
	}
	instance, ok := workflowRun["instance"].(maclawAppApprovalInstance)
	if !ok || instance.Status != "approved" || instance.WorkflowSkillID != "expense-workflow" || instance.WorkflowDecisionID != "decision-repaired-1" {
		t.Fatalf("dependency-repaired workflow should finish approval: %#v", workflowRun["instance"])
	}
	handled, err := app.ListMaclawAppApprovalInstances("expense-approval", "handled", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances handled error = %v", err)
	}
	globalHandled, err := app.ListMaclawAppApprovalInstancesAll("handled", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstancesAll handled error = %v", err)
	}
	if len(handled) != 1 || len(globalHandled) != 1 {
		t.Fatalf("dependency-repaired workflow should be visible in app/global handled lanes, app=%#v global=%#v", handled, globalHandled)
	}
	assertMaclawAppApprovalReadbackSameInstanceForTest(t, handled[0], globalHandled[0])
	sawInstallRegistration := false
	sawReview := false
	for _, req := range captured {
		if req.Method == http.MethodPut && req.Path == "/api/v1/data/app-installations/expense-approval" {
			sawInstallRegistration = true
			metadata := anyMap(req.Body["metadata"])
			dependencyVerification := anyMap(metadata["dependency_verification"])
			if metadata == nil ||
				metadata["dependency_count"] != float64(2) ||
				metadata["has_blocking_dependency"] != false ||
				dependencyVerification == nil ||
				dependencyVerification["blockedCount"] != float64(0) ||
				dependencyVerification["has_blocking_dependency"] != false {
				t.Fatalf("DataSrv registration should preserve repaired dependency summaries: %#v", req.Body)
			}
		}
		if req.Method == http.MethodPost && req.Path == "/api/v1/data/approvals/approval-repaired-1/review" {
			sawReview = true
		}
	}
	if !sawInstallRegistration || !sawReview {
		t.Fatalf("dependency repair flow should register install and review final approval, install=%v review=%v captured=%#v", sawInstallRegistration, sawReview, captured)
	}
}

func TestPlanMaclawAppInstallNormalizesApprovalWorkflowMapping(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	entries, err := parseMaclawAppInstallEntries(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "workflow-defaults",
			"name": "Workflow Defaults",
			"kind": "enterprise_approval_app",
			"binding": {
				"datasrv": {"domain": "finance", "datasetID": "finance.expenses", "objectRole": "expense_report"},
				"mis": {"approvalBindings": [{"event": "expense.submitted", "workflowSkillId": "expense-flow", "objectRole": "expense_report"}]}
			}
		}
	}`)
	if err != nil {
		t.Fatalf("parseMaclawAppInstallEntries() error = %v", err)
	}
	workflow := maclawAppWorkflowMappingForEntry(entries[0])
	if workflow == nil || workflow["schema"] != "maclaw.app.workflow.v1" || workflow["approvalNode"] != "expense_report.manager_approval" {
		t.Fatalf("expected default workflow mapping from object role: %#v", workflow)
	}
	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "workflow-explicit",
			"name": "Workflow Explicit",
			"kind": "enterprise_approval_app",
			"binding": {
				"datasrv": {"domain": "finance", "datasetID": "finance.expenses", "objectRole": "expense_report"},
				"workflow": {"schema":"maclaw.app.workflow.v1", "submit_node":"expense.intake", "approval_node":"finance.director_review", "result_node":"expense.result_pack", "status_mapping":{"pending":"finance_pending", "approved":"finance_approved", "rejected":"finance_rejected", "attention":"finance_attention"}},
				"mis": {"approvalBindings": [{"event": "expense.submitted", "workflowSkillId": "expense-flow", "objectRole": "expense_report"}]}
			}
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	if dep := maclawAppPlanDepForTest(plan, "expense-flow"); dep == nil || dep.Kind != "workflow_skill" {
		t.Fatalf("expected workflow dependency remains present: %#v", plan.Dependencies)
	}
}

func TestPlanMaclawAppInstallRejectsInvalidWorkflowMapping(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	cases := []struct {
		name    string
		pkg     string
		wantErr string
	}{
		{
			name: "bad workflow schema",
			pkg: `{
				"schema": "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"app": {
					"id": "bad-workflow-schema",
					"name": "Bad Workflow Schema",
					"kind": "enterprise_approval_app",
					"binding": {
						"workflow": {"schema":"bad.workflow", "submitNode":"a", "approvalNode":"b", "resultNode":"c"},
						"mis": {"approvalBindings": [{"workflowSkillId": "approval-flow"}]}
					}
				}
			}`,
			wantErr: "workflow.schema must be maclaw.app.workflow.v1",
		},
		{
			name: "tool app workflow mapping",
			pkg: `{
				"schema": "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"app": {
					"id": "tool-workflow",
					"name": "Tool Workflow",
					"kind": "tool_app",
					"binding": {"skill": {"id":"pdf-tool"}, "workflow": {"schema":"maclaw.app.workflow.v1", "submitNode":"a", "approvalNode":"b", "resultNode":"c"}}
				}
			}`,
			wantErr: "binding.workflow is only valid for enterprise_approval_app",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := app.PlanMaclawAppInstall(tc.pkg)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected %q error, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestPlanMaclawAppInstallReportsApprovalWorkflowContractIssues(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "approval-contract-plan",
			"name": "Approval Contract Plan",
			"kind": "enterprise_approval_app",
			"binding": {
				"datasrv": {"domain":"finance", "datasetID":"finance.expenses", "objectRole":"expense_report"},
				"mis": {"approvalBindings": [{"event":"expense.submitted", "workflowSkillId":"expense-flow", "workflowVersion":"1.0.0", "objectRole":"expense_report"}]}
			},
			"governance": {
				"workflowContract": {"schema":"maclaw.app.workflow_contract.v1", "workflowSkillId":"other-flow", "objectRole":"expense_report", "requiredInputs":["record_ref", "applicant", "business_payload"], "decisionOutputs":["approved", "rejected", "attention"], "statusMapping":{"pending":"approval_pending", "approved":"approved", "rejected":"rejected", "attention":"attention"}}
			}
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	if !plan.HasWorkflowContractIssue || len(plan.WorkflowContractIssues) != 1 {
		t.Fatalf("expected workflow contract issue in install plan: %#v", plan)
	}
	if plan.WorkflowContractIssues[0].Path != "apps[0].app.governance.workflowContract.workflowSkillId" {
		t.Fatalf("unexpected workflow contract issue: %#v", plan.WorkflowContractIssues[0])
	}
}

func TestPlanMaclawAppInstallTreatsApprovalBindingsAsWorkflowDependencies(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "bound-approval",
			"name": "Bound Approval",
			"kind": "enterprise_approval_app",
			"binding": {
				"mis": {
					"approvalBindings": [{
						"event": "finance.submitted",
						"workflowSkillId": "binding-workflow",
						"workflowVersion": "3.0.0"
					}]
				}
			}
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "binding-workflow")
	if dep == nil || dep.Kind != "workflow_skill" || dep.Version != "3.0.0" || !dep.Required || dep.Action != "install" || plan.HasBlockingDependency {
		t.Fatalf("approval binding workflow should be a required dependency: %#v", dep)
	}
}

func TestRecordMaclawAppInstallRejectsWorkflowContractIssues(t *testing.T) {
	tmpHome := t.TempDir()
	workflowDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "expense-flow")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatalf("MkdirAll workflowDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowDir, "skill.md"), []byte("# Expense flow\n"), 0o644); err != nil {
		t.Fatalf("WriteFile workflow skill.md: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "expense-flow", SkillDir: workflowDir, Status: "active", HubVersion: "2.1.0"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	pkg := `{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "bad-workflow-install",
			"name": "Bad Workflow Install",
			"kind": "enterprise_approval_app",
			"binding": {
				"datasrv": {"domain":"finance", "datasetID":"finance.expenses", "objectRole":"expense_report"},
				"mis": {"approvalBindings": [{"event":"expense.submitted", "workflowSkillId":"expense-flow", "workflowVersion":"9.9.9", "objectRole":"expense_report"}]}
			},
			"dependencies": {"skills": [{"id":"expense-flow", "kind":"workflow_skill", "version":"9.9.9", "required":true, "source":"hub"}]},
			"governance": {
				"workflowContract": {"schema":"maclaw.app.workflow_contract.v1", "workflowSkillId":"expense-flow", "workflowVersion":"9.9.9", "objectRole":"expense_report", "requiredInputs":["record_ref", "applicant", "business_payload"], "decisionOutputs":["approved", "rejected", "attention"], "statusMapping":{"pending":"pending", "approved":"approved", "rejected":"rejected", "attention":"attention"}}
			}
		}
	}`

	_, err = app.RecordMaclawAppInstall(pkg, "market")
	if err == nil || !strings.Contains(err.Error(), "approval workflow contract is invalid") || !strings.Contains(err.Error(), "version 2.1.0 does not match required 9.9.9") {
		t.Fatalf("expected workflow contract install error with detail, got %v", err)
	}
	records, err := app.ListMaclawAppInstalls(10)
	if err != nil {
		t.Fatalf("ListMaclawAppInstalls() error = %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("workflow-blocked install should not write audit records: %#v", records)
	}
}

func TestRecordMaclawAppInstallRegistersApprovalAppWithDataSrv(t *testing.T) {
	type capturedRequest struct {
		Method string
		Path   string
		Header http.Header
		Body   map[string]interface{}
	}
	captured := []capturedRequest{}
	remoteApproved := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := capturedRequest{Method: r.Method, Path: r.URL.Path, Header: r.Header.Clone()}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		captured = append(captured, item)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/data/app-installations/expense-approval":
			_, _ = w.Write([]byte(`{"app_id":"expense-approval","status":"installed"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-runtime-1":
			_, _ = w.Write([]byte(`{"id":"expense-runtime-1","data":{"amount":860,"status":"draft"}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-runtime-1":
			_, _ = w.Write([]byte(`{"id":"expense-runtime-1","status":"updated"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-runtime-1/approvals":
			_, _ = w.Write([]byte(`{"id":"approval-runtime-1","status":"pending"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/approvals/approval-runtime-1/review":
			remoteApproved = true
			_, _ = w.Write([]byte(`{"id":"approval-runtime-1","status":"approved"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/approvals":
			if remoteApproved {
				_, _ = w.Write([]byte(`{"items":[{"id":"approval-runtime-1","app_id":"expense-approval","dataset_id":"finance.expense_forms","record_id":"expense-runtime-1","status":"approved","summary":"Runtime Expense","workflow_skill_id":"expense-workflow","workflow_version":"2.0.0","workflow_instance_id":"wf-runtime-1","workflow_node_id":"expense.result_pack","workflow_node_ids":["expense.intake","finance.director_review","expense.result_pack"],"business_status":"finance_approved","result_status":"approved","result_payload":{"approval_result":"approved","business_status":"finance_approved","business_record":{"id":"expense-runtime-1","status":"finance_approved"},"text":"director approved"},"outputs":[{"type":"content","title":"Runtime Decision","text":"director approved"},{"type":"artifact","title":"Runtime Approval PDF","artifact":{"id":"runtime-approval-pdf","name":"runtime-approval.pdf","uri":"artifact://runtime/approval.pdf","status":"ready"}}],"artifacts":[{"id":"runtime-approval-pdf","name":"runtime-approval.pdf","uri":"artifact://runtime/approval.pdf","status":"ready"}],"request":{"approval_instance_id":"wf-runtime-1","appID":"expense-approval","blueprintID":"expense.blueprint.v1","objectRole":"expense_report","approvalEvent":"finance.submitted","applicant":"alice","currentAssignee":"manager","currentAssigneeType":"user","workflowSkillId":"expense-workflow","workflowVersion":"2.0.0","workflowNodeId":"expense.result_pack","workflowNodeIds":["expense.intake","finance.director_review","expense.result_pack"],"businessStatus":"finance_approved","resultStatus":"approved"},"created_by":"alice","submitted_by":"alice","reviewed_by":"alice","assigned_to":"manager","created_at":"2026-06-28T01:00:00Z","updated_at":"2026-06-28T01:02:00Z"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"items":[{"id":"approval-runtime-1","dataset_id":"finance.expense_forms","record_id":"expense-runtime-1","status":"pending","summary":"Runtime Expense","request":{"approval_instance_id":"wf-runtime-1","appID":"expense-approval","blueprintID":"expense.blueprint.v1","objectRole":"expense_report","approvalEvent":"finance.submitted","applicant":"alice","currentAssignee":"manager","currentAssigneeType":"user","workflowSkillId":"expense-workflow","workflowVersion":"2.0.0","workflowNodeId":"finance.director_review","workflowNodeIds":["expense.intake","finance.director_review"],"businessStatus":"finance_pending","resultStatus":"pending","resultPayload":{"text":"waiting for director","business_record":{"id":"expense-runtime-1"}},"outputs":[{"type":"content","title":"Runtime Summary","text":"waiting for director"}],"artifacts":[{"id":"runtime-receipt","name":"runtime-receipt.pdf","uri":"artifact://runtime-receipt"}]},"created_by":"alice","submitted_by":"alice","assigned_to":"manager","created_at":"2026-06-28T01:00:00Z","updated_at":"2026-06-28T01:01:00Z"}]}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer server.Close()

	tmpHome := t.TempDir()
	appSkillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "expense-super-skill")
	workflowSkillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "expense-workflow")
	for _, dir := range []string{appSkillDir, workflowSkillDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll skill dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "skill.md"), []byte("# Skill\n"), 0o644); err != nil {
			t.Fatalf("WriteFile skill.md: %v", err)
		}
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{
		{Name: "expense-super-skill", SkillDir: appSkillDir, Status: "active"},
		{Name: "expense-workflow", SkillDir: workflowSkillDir, Status: "active"},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "alice", Role: "data_admin"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}
	pkg := `{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "expense-approval",
			"name": "Expense Approval",
			"version": "1.2.3",
			"kind": "enterprise_approval_app",
			"governance": {
				"status": "local_tested",
				"riskLevel": "medium",
				"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"approval_workspace", "template":"classic_split", "density":"comfortable", "regionCount":4},
				"resultContract": {
					"schema": "maclaw.app.result.v1",
					"primary": "approval_result",
					"types": ["approval_result", "business_status", "business_record", "document", "notification"],
					"delivery": {"artifacts": true, "businessRecord": true, "notifications": true}
				},
				"workflowContract": {
					"schema": "maclaw.app.workflow_contract.v1",
					"workflowSkillId": "expense-workflow",
					"workflowVersion": "2.0.0",
					"objectRole": "expense_report",
					"requiredInputs": ["record_ref", "applicant", "business_payload"],
					"decisionOutputs": ["approved", "rejected", "attention"],
					"statusMapping": {"pending":"finance_pending", "approved":"finance_approved", "rejected":"finance_rejected", "attention":"finance_attention", "requiresInput":"finance_more_input"}
				},
				"dependencyVerification": {"schema":"maclaw.app.install_plan.v1", "verifiedAt":"2026-06-17T00:59:00Z", "dependencyCount":2, "hasMissingRequired":false, "hasBlockingDependency":false, "hasWorkflowContractIssue":false, "workflowContractIssueCount":0, "hasGovernanceReviewIssue":false, "governanceReviewIssueCount":0, "dependencies":[{"id":"expense-super-skill", "installed":true, "health":"ready"}, {"id":"expense-workflow", "installed":true, "health":"ready"}]},
				"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-basic","sampleInput":{"sample":true},"expectedOutput":{"status":"ok"},"requiredRoles":["tester"],"requiredScopes":["app.run"],"riskLevel":"low"}, "testProtocolFingerprint":"proto-basic", "runId":"run-expense-1", "artifactPresent":true, "artifacts":[{"id":"artifact-expense-evidence", "uri":"artifact://expense/evidence.zip", "name":"expense-approval-evidence.zip", "status":"ready"}], "outputs":[{"kind":"table", "title":"Approval rows", "text":"expense approved", "status":"ready", "data":{"rows":[{"id":"expense-1", "status":"finance_approved"}]}}], "resultPayload":{"approval_result":"approved", "business_status":"finance_approved", "business_record":{"id":"expense-1"}}, "verifiedAt":"2026-06-17T01:00:00Z", "approvalInstance":{"instanceId":"wf-test-1", "approvalID":"approval-remote-install-1", "recordID":"expense-1", "datasetID":"finance.expenses", "objectRole":"expense_report", "approvalEvent":"expense.submitted", "approvalWorkflowID":"expense-workflow", "status":"approved", "currentNode":"expense.result", "workflowSkillId":"expense-workflow", "workflowVersion":"2.0.0", "businessStatus":"finance_approved", "resultStatus":"approved", "resultPayload":{"approval_result":"approved", "business_status":"finance_approved", "business_record":{"id":"expense-1"}}, "outputs":[{"kind":"table", "title":"Approval rows", "text":"expense approved", "status":"ready", "data":{"rows":[{"id":"expense-1", "status":"finance_approved"}]}}], "artifacts":[{"id":"artifact-expense-evidence", "uri":"artifact://expense/evidence.zip", "name":"expense-approval-evidence.zip", "status":"ready"}], "viewVerified":true}, "resultCoverage":{"ok":true, "primary":"approval_result", "coveredTypes":["approval_result", "business_status", "business_record", "document"], "missingTypes":[]}, "dependencyVerification":{"schema":"maclaw.app.install_plan.v1", "verifiedAt":"2026-06-17T00:59:00Z", "dependencyCount":2, "hasMissingRequired":false, "hasBlockingDependency":false, "hasWorkflowContractIssue":false, "workflowContractIssueCount":0, "hasGovernanceReviewIssue":false, "governanceReviewIssueCount":0}}
			},
			"binding": {
					"appSkill": { "id": "expense-super-skill", "version": "1.0.0", "source": "hub" },
				"datasrv": { "domain": "finance", "datasetID": "finance.expense_forms", "templateID": "finance.expenses" },
				"ui": {
					"schema": "maclaw.app.ui.v1",
					"entry": "approval_workspace",
					"layouts": {
						"approval_workspace": {
							"template": "classic_split",
							"density": "compact",
							"primaryRegion": "left",
							"outputRegion": "right",
							"navigation": ["my_requests", "pending_my_approval", "attention"],
							"list": {"columns": ["title", "applicant", "current_node", "status"]},
							"studio": {"savedInManifest": true, "editable": true, "updatedBy": "app_studio"}
						}
					}
				},
				"workflow": {
					"schema": "maclaw.app.workflow.v1",
					"submitNode": "expense.intake",
					"approvalNode": "finance.director_review",
					"resultNode": "expense.result_pack",
					"attentionNode": "expense.attention",
					"statusMapping": {"pending":"finance_pending", "approved":"finance_approved", "rejected":"finance_rejected", "attention":"finance_attention", "requiresInput":"finance_more_input"}
				},
				"mis": {
					"approvalBindings": [{
						"event": "finance.submitted",
						"workflowSkillId": "expense-workflow",
						"workflowVersion": "2.0.0",
						"objectRole": "expense_report"
					}]
				}
			}
			}
		}`

	pkg = maclawAppPackageWithCurrentDefinitionHashes(t, pkg)
	result, err := app.RecordMaclawAppInstall(pkg, "market")
	if err != nil {
		t.Fatalf("RecordMaclawAppInstall() error = %v", err)
	}
	installEvidence, ok := result["install_evidence"].(map[string]any)
	if !ok {
		t.Fatalf("install result should expose per-app evidence: %#v", result["install_evidence"])
	}
	expenseEvidence, ok := installEvidence["expense-approval"].(map[string]interface{})
	if !ok {
		t.Fatalf("install evidence should include approval app id: %#v", installEvidence)
	}
	expenseTestEvidence, ok := expenseEvidence["test_evidence"].(map[string]interface{})
	if !ok || expenseTestEvidence["runId"] != "run-expense-1" {
		t.Fatalf("install evidence should preserve approval app test evidence: %#v", expenseEvidence)
	}
	expenseApprovalInstance, ok := expenseTestEvidence["approvalInstance"].(map[string]interface{})
	if !ok || expenseApprovalInstance["approvalID"] != "approval-remote-install-1" || expenseApprovalInstance["currentNode"] != "expense.result" || expenseApprovalInstance["workflowSkillId"] != "expense-workflow" || expenseApprovalInstance["businessStatus"] != "finance_approved" || expenseApprovalInstance["resultStatus"] != "approved" {
		t.Fatalf("install evidence should preserve approval instance core state: %#v", expenseTestEvidence)
	}
	expenseApprovalResultPayload, ok := expenseApprovalInstance["resultPayload"].(map[string]interface{})
	if !ok || expenseApprovalResultPayload["approval_result"] != "approved" || expenseApprovalResultPayload["business_status"] != "finance_approved" {
		t.Fatalf("install evidence should preserve approval instance result payload: %#v", expenseApprovalInstance)
	}
	expenseApprovalOutputs, ok := expenseApprovalInstance["outputs"].([]interface{})
	if !ok || len(expenseApprovalOutputs) != 1 {
		t.Fatalf("install evidence should preserve approval instance outputs: %#v", expenseApprovalInstance)
	}
	expenseApprovalArtifacts, ok := expenseApprovalInstance["artifacts"].([]interface{})
	if !ok || len(expenseApprovalArtifacts) != 1 {
		t.Fatalf("install evidence should preserve approval instance artifacts: %#v", expenseApprovalInstance)
	}
	registration, ok := result["datasrv_registration"].(map[string]any)
	if !ok || registration["synced"] != true || registration["eligible_count"] != 1 || registration["synced_count"] != 1 {
		t.Fatalf("expected DataSrv registration success: %#v", result["datasrv_registration"])
	}
	if len(captured) != 1 {
		t.Fatalf("captured %d requests, want 1: %#v", len(captured), captured)
	}
	if captured[0].Method != http.MethodPut || captured[0].Path != "/api/v1/data/app-installations/expense-approval" {
		t.Fatalf("unexpected registration request: %#v", captured[0])
	}
	if got := captured[0].Header.Get("Authorization"); got != "Bearer token" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := captured[0].Header.Get("X-MaClaw-Role"); got != "data_admin" {
		t.Fatalf("X-MaClaw-Role = %q", got)
	}
	if captured[0].Body["app_id"] != "expense-approval" || captured[0].Body["kind"] != "enterprise_approval_app" || captured[0].Body["source"] != "market" {
		t.Fatalf("registration body missing app metadata: %#v", captured[0].Body)
	}
	roleBindings, ok := captured[0].Body["role_bindings"].([]interface{})
	if !ok || len(roleBindings) != 1 {
		t.Fatalf("registration body missing role bindings: %#v", captured[0].Body)
	}
	binding, ok := roleBindings[0].(map[string]interface{})
	if !ok || binding["object_role"] != "expense_report" || binding["domain"] != "finance" || binding["dataset_id"] != "finance.expense_forms" || binding["template_id"] != "finance.expenses" || binding["required"] != true {
		t.Fatalf("unexpected role binding: %#v", roleBindings[0])
	}
	metadata, ok := captured[0].Body["metadata"].(map[string]interface{})
	if !ok || metadata["app_skill_id"] != "expense-super-skill" || metadata["app_skill_source"] != "hub" {
		t.Fatalf("registration body missing metadata: %#v", captured[0].Body)
	}
	versionSnapshot, ok := metadata["version_snapshot"].(map[string]interface{})
	if !ok || versionSnapshot["app_entry_version"] != "1.2.3" {
		t.Fatalf("registration metadata missing version snapshot: %#v", metadata)
	}
	appSkillSnapshot, ok := versionSnapshot["app_skill"].(map[string]interface{})
	if !ok || appSkillSnapshot["id"] != "expense-super-skill" || appSkillSnapshot["version"] != "1.0.0" {
		t.Fatalf("registration metadata missing app skill version snapshot: %#v", versionSnapshot)
	}
	workflowSnapshots, ok := versionSnapshot["workflow_skills"].([]interface{})
	if !ok || len(workflowSnapshots) != 1 {
		t.Fatalf("registration metadata missing workflow skill version snapshot: %#v", versionSnapshot)
	}
	workflowSnapshot, ok := workflowSnapshots[0].(map[string]interface{})
	if !ok || workflowSnapshot["id"] != "expense-workflow" || workflowSnapshot["version"] != "2.0.0" {
		t.Fatalf("registration metadata missing workflow version: %#v", versionSnapshot)
	}
	approvalBindingsSnapshot, ok := versionSnapshot["approval_bindings"].([]interface{})
	if !ok || len(approvalBindingsSnapshot) != 1 {
		t.Fatalf("registration metadata missing approval binding snapshot: %#v", versionSnapshot)
	}
	approvalBindingSnapshot, ok := approvalBindingsSnapshot[0].(map[string]interface{})
	if !ok || approvalBindingSnapshot["event"] != "finance.submitted" || approvalBindingSnapshot["workflow_skill_id"] != "expense-workflow" || approvalBindingSnapshot["workflow_version"] != "2.0.0" || approvalBindingSnapshot["object_role"] != "expense_report" {
		t.Fatalf("registration metadata missing approval binding version snapshot: %#v", versionSnapshot)
	}
	workflowIDs, ok := metadata["workflow_skill_ids"].([]interface{})
	if !ok || len(workflowIDs) != 1 || workflowIDs[0] != "expense-workflow" {
		t.Fatalf("registration metadata missing workflow skill ids: %#v", metadata)
	}
	if metadata["workspace_layout_entry"] != "approval_workspace" || metadata["workspace_layout_template"] != "classic_split" || metadata["workspace_layout_density"] != "compact" || metadata["workspace_layout_primary_region"] != "left" || metadata["workspace_layout_output_region"] != "right" {
		t.Fatalf("registration metadata missing workspace layout summary: %#v", metadata)
	}
	if metadata["workspace_layout_studio_saved_in_manifest"] != true || metadata["workspace_layout_studio_editable"] != true || metadata["workspace_layout_studio_updated_by"] != "app_studio" {
		t.Fatalf("registration metadata missing App Studio layout summary: %#v", metadata)
	}
	layoutNavigation, ok := metadata["workspace_layout_navigation"].([]interface{})
	if !ok || len(layoutNavigation) != 3 || layoutNavigation[0] != "my_requests" || layoutNavigation[1] != "pending_my_approval" || layoutNavigation[2] != "attention" {
		t.Fatalf("registration metadata missing workspace navigation summary: %#v", metadata)
	}
	layoutColumns, ok := metadata["workspace_layout_list_columns"].([]interface{})
	if !ok || len(layoutColumns) != 4 || layoutColumns[0] != "title" || layoutColumns[1] != "applicant" || layoutColumns[2] != "current_node" || layoutColumns[3] != "status" {
		t.Fatalf("registration metadata missing workspace list column summary: %#v", metadata)
	}
	workflowMapping, ok := metadata["workflow_mapping"].(map[string]interface{})
	if !ok || workflowMapping["schema"] != "maclaw.app.workflow.v1" || workflowMapping["approvalNode"] != "finance.director_review" {
		t.Fatalf("registration metadata missing workflow mapping: %#v", metadata)
	}
	nestedInstallEvidence, ok := metadata["install_evidence"].(map[string]interface{})
	if !ok {
		t.Fatalf("registration metadata missing nested install evidence: %#v", metadata)
	}
	installWorkflowMapping := anyMap(nestedInstallEvidence["workflow_mapping"])
	if installWorkflowMapping == nil || installWorkflowMapping["approvalNode"] != "finance.director_review" {
		t.Fatalf("nested install evidence missing workflow mapping: %#v", nestedInstallEvidence)
	}
	installDependencyVerification := anyMap(nestedInstallEvidence["dependency_verification"])
	if installDependencyVerification == nil || installDependencyVerification["schema"] != "maclaw.app.install_plan.v1" {
		t.Fatalf("nested install evidence missing dependency verification: %#v", nestedInstallEvidence)
	}
	installTestEvidence := anyMap(nestedInstallEvidence["test_evidence"])
	if installTestEvidence == nil || installTestEvidence["runId"] != "run-expense-1" {
		t.Fatalf("nested install evidence missing test evidence: %#v", nestedInstallEvidence)
	}
	statusMapping, ok := workflowMapping["statusMapping"].(map[string]interface{})
	if !ok || statusMapping["approved"] != "finance_approved" {
		t.Fatalf("registration metadata missing workflow status mapping: %#v", workflowMapping)
	}
	if metadata["workflow_mapping_schema"] != "maclaw.app.workflow.v1" || metadata["workflow_submit_node"] != "expense.intake" || metadata["workflow_approval_node"] != "finance.director_review" || metadata["workflow_result_node"] != "expense.result_pack" {
		t.Fatalf("registration metadata missing workflow node summary: %#v", metadata)
	}
	dependencyVerification, ok := metadata["dependency_verification"].(map[string]interface{})
	if !ok || dependencyVerification["schema"] != "maclaw.app.install_plan.v1" || dependencyVerification["verifiedAt"] != "2026-06-17T00:59:00Z" || dependencyVerification["dependencyCount"] != float64(2) || dependencyVerification["hasBlockingDependency"] != false {
		t.Fatalf("registration metadata missing dependency verification evidence: %#v", metadata)
	}
	verifiedDependencies, ok := dependencyVerification["dependencies"].([]interface{})
	if !ok || len(verifiedDependencies) != 2 {
		t.Fatalf("registration dependency verification missing dependencies: %#v", dependencyVerification)
	}
	if metadata["test_evidence_dependency_verified_at"] != "2026-06-17T00:59:00Z" || metadata["test_evidence_dependency_count"] != float64(2) || metadata["test_evidence_dependency_missing_required"] != false || metadata["test_evidence_dependency_blocking"] != false {
		t.Fatalf("registration metadata missing dependency verification summary: %#v", metadata)
	}
	workflowContract, ok := metadata["workflow_contract"].(map[string]interface{})
	if !ok || workflowContract["schema"] != "maclaw.app.workflow_contract.v1" || workflowContract["workflowSkillId"] != "expense-workflow" || workflowContract["objectRole"] != "expense_report" {
		t.Fatalf("registration metadata missing workflow contract: %#v", metadata)
	}
	if metadata["workflow_contract_schema"] != "maclaw.app.workflow_contract.v1" || metadata["workflow_contract_skill_id"] != "expense-workflow" || metadata["workflow_contract_object_role"] != "expense_report" {
		t.Fatalf("registration metadata missing workflow contract summary: %#v", metadata)
	}
	workspaceLayout, ok := metadata["workspace_layout"].(map[string]interface{})
	if !ok || workspaceLayout["entry"] != "approval_workspace" || workspaceLayout["template"] != "classic_split" || workspaceLayout["density"] != "compact" {
		t.Fatalf("registration metadata missing workspace layout payload: %#v", metadata)
	}
	workspaceStudio := anyMap(workspaceLayout["studio"])
	if workspaceStudio == nil || workspaceStudio["savedInManifest"] != true || workspaceLayout["studio_saved_in_manifest"] != true || workspaceLayout["studio_updated_by"] != "app_studio" {
		t.Fatalf("registration metadata missing nested App Studio layout payload: %#v", workspaceLayout)
	}
	workspaceList, ok := workspaceLayout["list"].(map[string]interface{})
	workspaceColumns, columnsOK := workspaceList["columns"].([]interface{})
	if !ok || !columnsOK || len(workspaceColumns) != 4 || workspaceColumns[2] != "current_node" {
		t.Fatalf("registration metadata missing workspace list payload: %#v", workspaceLayout)
	}
	governance, ok := metadata["governance"].(map[string]interface{})
	if !ok || metadata["governance_status"] != "local_tested" || metadata["governance_risk_level"] != "medium" || governance["status"] != "local_tested" {
		t.Fatalf("registration metadata missing governance snapshot: %#v", metadata)
	}
	resultContract, ok := governance["result_contract"].(map[string]interface{})
	if !ok || resultContract["primary"] != "approval_result" {
		t.Fatalf("registration metadata missing result contract: %#v", governance)
	}
	governanceWorkflowContract, ok := governance["workflow_contract"].(map[string]interface{})
	if !ok || governanceWorkflowContract["workflowSkillId"] != "expense-workflow" {
		t.Fatalf("registration metadata missing governance workflow contract: %#v", governance)
	}
	governanceDependencyVerification, ok := governance["dependency_verification"].(map[string]interface{})
	if !ok || governanceDependencyVerification["schema"] != "maclaw.app.install_plan.v1" || governanceDependencyVerification["dependencyCount"] != float64(2) || governanceDependencyVerification["hasBlockingDependency"] != false || governanceDependencyVerification["hasGovernanceReviewIssue"] != false || governanceDependencyVerification["governanceReviewIssueCount"] != float64(0) {
		t.Fatalf("registration metadata missing governance dependency verification: %#v", governance)
	}
	governanceTestEvidence, ok := governance["test_evidence"].(map[string]interface{})
	if !ok || governanceTestEvidence["runId"] != "run-expense-1" || governanceTestEvidence["artifactPresent"] != true {
		t.Fatalf("registration metadata missing governance test evidence: %#v", governance)
	}
	governanceTestProtocol, ok := governanceTestEvidence["testProtocol"].(map[string]interface{})
	if !ok || governanceTestProtocol["schema"] != "maclaw.app.test_protocol.v1" || governanceTestProtocol["fingerprint"] != "proto-basic" {
		t.Fatalf("registration metadata missing governance test protocol: %#v", governanceTestEvidence)
	}
	resultCoverage, ok := governanceTestEvidence["resultCoverage"].(map[string]interface{})
	if !ok || resultCoverage["ok"] != true || resultCoverage["primary"] != "approval_result" {
		t.Fatalf("registration metadata missing result coverage evidence: %#v", governanceTestEvidence)
	}
	coveredTypes, ok := resultCoverage["coveredTypes"].([]interface{})
	if !ok || len(coveredTypes) != 4 || coveredTypes[2] != "business_record" {
		t.Fatalf("registration metadata missing covered result types: %#v", resultCoverage)
	}
	governanceOutputs, ok := governanceTestEvidence["outputs"].([]interface{})
	if !ok || len(governanceOutputs) != 1 {
		t.Fatalf("registration metadata missing governance outputs evidence: %#v", governanceTestEvidence)
	}
	governanceOutput, ok := governanceOutputs[0].(map[string]interface{})
	if !ok || governanceOutput["kind"] != "table" || governanceOutput["title"] != "Approval rows" {
		t.Fatalf("registration metadata missing governance table output evidence: %#v", governanceOutputs)
	}
	governanceArtifacts, ok := governanceTestEvidence["artifacts"].([]interface{})
	if !ok || len(governanceArtifacts) != 1 {
		t.Fatalf("registration metadata missing governance artifacts evidence: %#v", governanceTestEvidence)
	}
	governanceArtifact, ok := governanceArtifacts[0].(map[string]interface{})
	if !ok || governanceArtifact["id"] != "artifact-expense-evidence" || governanceArtifact["name"] != "expense-approval-evidence.zip" {
		t.Fatalf("registration metadata missing governance artifact evidence: %#v", governanceArtifacts)
	}
	governanceResultPayload, ok := governanceTestEvidence["resultPayload"].(map[string]interface{})
	if !ok || governanceResultPayload["approval_result"] != "approved" || governanceResultPayload["business_status"] != "finance_approved" {
		t.Fatalf("registration metadata missing governance result payload: %#v", governanceTestEvidence)
	}
	governanceApprovalInstance, ok := governanceTestEvidence["approvalInstance"].(map[string]interface{})
	if !ok || governanceApprovalInstance["instanceId"] != "wf-test-1" || governanceApprovalInstance["approvalID"] != "approval-remote-install-1" || governanceApprovalInstance["recordID"] != "expense-1" {
		t.Fatalf("registration metadata missing governance approval instance remote id: %#v", governanceTestEvidence)
	}
	governanceApprovalResultPayload, ok := governanceApprovalInstance["resultPayload"].(map[string]interface{})
	if !ok || governanceApprovalInstance["currentNode"] != "expense.result" || governanceApprovalInstance["workflowSkillId"] != "expense-workflow" || governanceApprovalInstance["businessStatus"] != "finance_approved" || governanceApprovalInstance["resultStatus"] != "approved" || governanceApprovalResultPayload["approval_result"] != "approved" {
		t.Fatalf("registration metadata missing governance approval instance result package: %#v", governanceApprovalInstance)
	}
	governanceApprovalOutputs, ok := governanceApprovalInstance["outputs"].([]interface{})
	if !ok || len(governanceApprovalOutputs) != 1 {
		t.Fatalf("registration metadata missing governance approval instance outputs: %#v", governanceApprovalInstance)
	}
	governanceApprovalArtifacts, ok := governanceApprovalInstance["artifacts"].([]interface{})
	if !ok || len(governanceApprovalArtifacts) != 1 {
		t.Fatalf("registration metadata missing governance approval instance artifacts: %#v", governanceApprovalInstance)
	}
	testDependencyVerification, ok := governanceTestEvidence["dependencyVerification"].(map[string]interface{})
	if !ok || testDependencyVerification["verifiedAt"] != "2026-06-17T00:59:00Z" || testDependencyVerification["hasWorkflowContractIssue"] != false || testDependencyVerification["hasGovernanceReviewIssue"] != false || testDependencyVerification["governanceReviewIssueCount"] != float64(0) {
		t.Fatalf("registration metadata missing test dependency verification: %#v", governanceTestEvidence)
	}
	topLevelTestEvidence, ok := metadata["test_evidence"].(map[string]interface{})
	if !ok || topLevelTestEvidence["testProtocolFingerprint"] != "proto-basic" || topLevelTestEvidence["runId"] != "run-expense-1" || metadata["test_evidence_test_protocol_fingerprint"] != "proto-basic" {
		t.Fatalf("registration metadata missing top-level test evidence for DataSrv: %#v", metadata)
	}
	if metadata["test_evidence_run_id"] != "run-expense-1" || metadata["test_evidence_verified_at"] != "2026-06-17T01:00:00Z" || metadata["test_evidence_definition_fingerprint"] != topLevelTestEvidence["definitionHash"] {
		t.Fatalf("registration metadata missing stable test evidence identity summary: %#v", metadata)
	}
	if metadata["test_evidence_workspace_layout_fingerprint"] != topLevelTestEvidence["workspaceLayoutFingerprint"] || metadata["current_workspace_layout_fingerprint"] != topLevelTestEvidence["workspaceLayoutFingerprint"] || metadata["test_evidence_workspace_layout_matches_current"] != true || metadata["test_evidence_definition_matches_current"] != true || metadata["test_evidence_test_protocol_matches_current"] != true || metadata["design_consistency_ok"] != true {
		t.Fatalf("registration metadata missing design consistency summary: %#v", metadata)
	}
	designConsistency, ok := metadata["design_consistency"].(map[string]interface{})
	if !ok || anyMap(designConsistency["definition"])["matches_current"] != true || anyMap(designConsistency["workspace_layout"])["matches_current"] != true {
		t.Fatalf("registration metadata missing nested design consistency evidence: %#v", metadata["design_consistency"])
	}
	if metadata["test_evidence_artifact_present"] != true || metadata["test_evidence_artifact_count"] != float64(1) || metadata["test_evidence_output_count"] != float64(1) {
		t.Fatalf("registration metadata missing stable test evidence count summary: %#v", metadata)
	}
	topLevelTestProtocol, ok := topLevelTestEvidence["testProtocol"].(map[string]interface{})
	if !ok || topLevelTestProtocol["schema"] != "maclaw.app.test_protocol.v1" || topLevelTestProtocol["fingerprint"] != "proto-basic" {
		t.Fatalf("registration metadata missing top-level test protocol for DataSrv: %#v", topLevelTestEvidence)
	}
	topLevelSummaryOutputs, ok := metadata["test_evidence_outputs"].([]interface{})
	if !ok || len(topLevelSummaryOutputs) != 1 {
		t.Fatalf("registration metadata missing stable output summary for DataSrv: %#v", metadata)
	}
	topLevelSummaryOutput, ok := topLevelSummaryOutputs[0].(map[string]interface{})
	if !ok || topLevelSummaryOutput["kind"] != "table" || topLevelSummaryOutput["title"] != "Approval rows" {
		t.Fatalf("registration metadata missing stable table output summary for DataSrv: %#v", topLevelSummaryOutputs)
	}
	topLevelOutputs, ok := topLevelTestEvidence["outputs"].([]interface{})
	if !ok || len(topLevelOutputs) != 1 {
		t.Fatalf("registration metadata missing top-level outputs for DataSrv: %#v", topLevelTestEvidence)
	}
	topLevelOutput, ok := topLevelOutputs[0].(map[string]interface{})
	if !ok || topLevelOutput["kind"] != "table" || topLevelOutput["text"] != "expense approved" {
		t.Fatalf("registration metadata missing top-level table output for DataSrv: %#v", topLevelOutputs)
	}
	topLevelArtifacts, ok := topLevelTestEvidence["artifacts"].([]interface{})
	if !ok || len(topLevelArtifacts) != 1 {
		t.Fatalf("registration metadata missing top-level artifacts for DataSrv: %#v", topLevelTestEvidence)
	}
	topLevelArtifact, ok := topLevelArtifacts[0].(map[string]interface{})
	if !ok || topLevelArtifact["uri"] != "artifact://expense/evidence.zip" {
		t.Fatalf("registration metadata missing top-level artifact for DataSrv: %#v", topLevelArtifacts)
	}
	topLevelSummaryArtifacts, ok := metadata["test_evidence_artifacts"].([]interface{})
	if !ok || len(topLevelSummaryArtifacts) != 1 {
		t.Fatalf("registration metadata missing stable artifact summary for DataSrv: %#v", metadata)
	}
	topLevelSummaryArtifact, ok := topLevelSummaryArtifacts[0].(map[string]interface{})
	if !ok || topLevelSummaryArtifact["name"] != "expense-approval-evidence.zip" {
		t.Fatalf("registration metadata missing stable artifact name summary for DataSrv: %#v", topLevelSummaryArtifacts)
	}
	topLevelResultPayload, ok := topLevelTestEvidence["resultPayload"].(map[string]interface{})
	if !ok || topLevelResultPayload["approval_result"] != "approved" || topLevelResultPayload["business_status"] != "finance_approved" {
		t.Fatalf("registration metadata missing top-level result payload for DataSrv: %#v", topLevelTestEvidence)
	}
	topLevelSummaryResultPayload, ok := metadata["test_evidence_result_payload"].(map[string]interface{})
	if !ok || topLevelSummaryResultPayload["approval_result"] != "approved" || topLevelSummaryResultPayload["business_status"] != "finance_approved" {
		t.Fatalf("registration metadata missing stable result payload summary for DataSrv: %#v", metadata)
	}
	topLevelApprovalInstance, ok := topLevelTestEvidence["approvalInstance"].(map[string]interface{})
	if !ok || topLevelApprovalInstance["instanceId"] != "wf-test-1" || topLevelApprovalInstance["approvalID"] != "approval-remote-install-1" || topLevelApprovalInstance["recordID"] != "expense-1" {
		t.Fatalf("registration metadata missing top-level approval instance remote id for DataSrv: %#v", topLevelTestEvidence)
	}
	if metadata["test_evidence_approval_instance_id"] != "wf-test-1" || metadata["test_evidence_approval_id"] != "approval-remote-install-1" || metadata["test_evidence_record_id"] != "expense-1" || metadata["test_evidence_approval_status"] != "approved" || metadata["test_evidence_approval_view_verified"] != true {
		t.Fatalf("registration metadata missing stable approval instance summary for DataSrv: %#v", metadata)
	}
	topLevelSummaryApprovalInstance, ok := metadata["test_evidence_approval_instance"].(map[string]interface{})
	if !ok || topLevelSummaryApprovalInstance["instanceId"] != "wf-test-1" || topLevelSummaryApprovalInstance["approvalID"] != "approval-remote-install-1" {
		t.Fatalf("registration metadata missing stable approval instance payload for DataSrv: %#v", metadata)
	}
	topLevelApprovalResultPayload, ok := topLevelApprovalInstance["resultPayload"].(map[string]interface{})
	if !ok || topLevelApprovalInstance["currentNode"] != "expense.result" || topLevelApprovalInstance["workflowSkillId"] != "expense-workflow" || topLevelApprovalInstance["businessStatus"] != "finance_approved" || topLevelApprovalInstance["resultStatus"] != "approved" || topLevelApprovalResultPayload["business_record"] == nil {
		t.Fatalf("registration metadata missing top-level approval instance result package for DataSrv: %#v", topLevelApprovalInstance)
	}
	topLevelApprovalOutputs, ok := topLevelApprovalInstance["outputs"].([]interface{})
	if !ok || len(topLevelApprovalOutputs) != 1 {
		t.Fatalf("registration metadata missing top-level approval instance outputs for DataSrv: %#v", topLevelApprovalInstance)
	}
	topLevelApprovalArtifacts, ok := topLevelApprovalInstance["artifacts"].([]interface{})
	if !ok || len(topLevelApprovalArtifacts) != 1 {
		t.Fatalf("registration metadata missing top-level approval instance artifacts for DataSrv: %#v", topLevelApprovalInstance)
	}
	topLevelSummaryCoverage, ok := metadata["test_evidence_result_coverage"].(map[string]interface{})
	if !ok || topLevelSummaryCoverage["ok"] != true || topLevelSummaryCoverage["primary"] != "approval_result" || metadata["test_evidence_result_coverage_ok"] != true || metadata["test_evidence_result_coverage_primary"] != "approval_result" {
		t.Fatalf("registration metadata missing stable result coverage summary for DataSrv: %#v", metadata)
	}
	topLevelSummaryCoveredTypes, ok := metadata["test_evidence_covered_types"].([]interface{})
	if !ok || len(topLevelSummaryCoveredTypes) != 4 || topLevelSummaryCoveredTypes[2] != "business_record" {
		t.Fatalf("registration metadata missing stable covered result types for DataSrv: %#v", metadata)
	}
	topLevelResultContract, ok := metadata["result_contract"].(map[string]interface{})
	if !ok || topLevelResultContract["schema"] != "maclaw.app.result.v1" || topLevelResultContract["primary"] != "approval_result" || metadata["result_contract_schema"] != "maclaw.app.result.v1" || metadata["result_contract_primary"] != "approval_result" {
		t.Fatalf("registration metadata missing top-level result contract summary: %#v", metadata)
	}
	resultTypes, ok := metadata["result_contract_types"].([]interface{})
	if !ok || len(resultTypes) != 5 || resultTypes[0] != "approval_result" {
		t.Fatalf("registration metadata missing top-level result contract types: %#v", metadata)
	}
	dependencies, ok := metadata["dependencies"].([]interface{})
	if !ok || len(dependencies) != 2 || metadata["dependency_count"] != float64(2) || metadata["has_missing_required_dependency"] != false || metadata["has_blocking_dependency"] != false {
		t.Fatalf("registration metadata missing dependency snapshot: %#v", metadata)
	}
	dependencyByID := map[string]map[string]interface{}{}
	for _, item := range dependencies {
		dep, ok := item.(map[string]interface{})
		if !ok {
			t.Fatalf("dependency metadata item should be an object: %#v", item)
		}
		id, _ := dep["id"].(string)
		dependencyByID[id] = dep
	}
	if dep := dependencyByID["expense-super-skill"]; dep == nil || dep["kind"] != "app_skill" || dep["source"] != "hub" || dep["required"] != true || dep["action"] != "skip" || dep["health"] != "ready" {
		t.Fatalf("registration metadata missing appSkill dependency state: %#v", dependencyByID)
	}
	if dep := dependencyByID["expense-workflow"]; dep == nil || dep["kind"] != "workflow_skill" || dep["version"] != "2.0.0" || dep["source"] != "hub" || dep["required"] != true || dep["action"] != "skip" || dep["health"] != "ready" {
		t.Fatalf("registration metadata missing workflow dependency state: %#v", dependencyByID)
	}
	records, err := app.ListMaclawAppInstalls(10)
	if err != nil {
		t.Fatalf("ListMaclawAppInstalls() error = %v", err)
	}
	if len(records) != 1 || records[0].AppID != "expense-approval" {
		t.Fatalf("expected one local install audit record: %#v", records)
	}
	if records[0].WorkspaceLayout["entry"] != "approval_workspace" || records[0].WorkspaceLayout["template"] != "classic_split" || records[0].WorkspaceLayout["density"] != "compact" {
		t.Fatalf("install audit should keep workspace layout evidence: %#v", records[0].WorkspaceLayout)
	}
	if anyMap(records[0].WorkspaceLayout["studio"])["savedInManifest"] != true || records[0].WorkspaceLayout["studio_updated_by"] != "app_studio" {
		t.Fatalf("install audit should keep App Studio layout evidence: %#v", records[0].WorkspaceLayout)
	}
	if records[0].ResultContract["schema"] != "maclaw.app.result.v1" || records[0].ResultContract["primary"] != "approval_result" {
		t.Fatalf("install audit should keep result contract evidence: %#v", records[0].ResultContract)
	}
	if records[0].TestEvidence["runId"] != "run-expense-1" || records[0].TestEvidence["testProtocolFingerprint"] != "proto-basic" {
		t.Fatalf("install audit should keep test evidence: %#v", records[0].TestEvidence)
	}
	auditApprovalInstance, ok := records[0].TestEvidence["approvalInstance"].(map[string]interface{})
	if !ok || auditApprovalInstance["approvalID"] != "approval-remote-install-1" || auditApprovalInstance["recordID"] != "expense-1" {
		t.Fatalf("install audit should keep approval instance remote id evidence: %#v", records[0].TestEvidence)
	}
	auditApprovalResultPayload, ok := auditApprovalInstance["resultPayload"].(map[string]interface{})
	if !ok || auditApprovalInstance["currentNode"] != "expense.result" || auditApprovalInstance["workflowSkillId"] != "expense-workflow" || auditApprovalInstance["businessStatus"] != "finance_approved" || auditApprovalInstance["resultStatus"] != "approved" || auditApprovalResultPayload["approval_result"] != "approved" {
		t.Fatalf("install audit should keep approval instance result package evidence: %#v", auditApprovalInstance)
	}
	auditApprovalOutputs, ok := auditApprovalInstance["outputs"].([]interface{})
	if !ok || len(auditApprovalOutputs) != 1 {
		t.Fatalf("install audit should keep approval instance outputs evidence: %#v", auditApprovalInstance)
	}
	auditApprovalArtifacts, ok := auditApprovalInstance["artifacts"].([]interface{})
	if !ok || len(auditApprovalArtifacts) != 1 {
		t.Fatalf("install audit should keep approval instance artifacts evidence: %#v", auditApprovalInstance)
	}
	auditProtocol, ok := records[0].TestEvidence["testProtocol"].(map[string]interface{})
	if !ok || auditProtocol["schema"] != "maclaw.app.test_protocol.v1" || auditProtocol["fingerprint"] != "proto-basic" {
		t.Fatalf("install audit should keep test protocol evidence: %#v", records[0].TestEvidence)
	}
	if records[0].WorkflowContract["workflowSkillId"] != "expense-workflow" || records[0].WorkflowContract["objectRole"] != "expense_report" {
		t.Fatalf("install audit should keep workflow contract evidence: %#v", records[0].WorkflowContract)
	}
	runtimeInstance := maclawAppApprovalInstance{
		AppID:               "expense-approval",
		AppName:             "Expense Approval",
		BlueprintID:         "expense.blueprint.v1",
		DatasetID:           "finance.expense_forms",
		ObjectRole:          "expense_report",
		ApprovalObjectRole:  "expense_report",
		ApprovalEvent:       "finance.submitted",
		ApprovalWorkflowID:  "expense-workflow",
		InstanceID:          "wf-runtime-1",
		Title:               "Runtime Expense",
		Lane:                "pending_my_approval",
		Status:              "pending",
		CurrentNode:         "finance.director_review",
		CurrentNodeIDs:      []string{"expense.intake", "finance.director_review"},
		Owner:               "alice",
		Applicant:           "alice",
		Approver:            "manager",
		CurrentAssignee:     "manager",
		CurrentAssigneeType: "user",
		WorkflowSkillID:     "expense-workflow",
		WorkflowVersion:     "2.0.0",
		BusinessStatus:      "finance_pending",
		ResultStatus:        "pending",
		FromStatus:          "draft",
		ToStatus:            "finance_pending",
		RecordID:            "expense-runtime-1",
		Result:              "waiting for director",
		ResultPayload:       map[string]any{"text": "waiting for director", "business_record": map[string]any{"id": "expense-runtime-1"}},
		Outputs:             []maclawAppApprovalOutput{{Type: "content", Title: "Runtime Summary", Text: "waiting for director"}},
		Artifacts:           []maclawAppApprovalArtifact{{ID: "runtime-receipt", Name: "runtime-receipt.pdf", URI: "artifact://runtime-receipt"}},
	}
	synced, err := app.SyncMaclawAppApprovalInstanceToDataSrv(maclawAppApprovalDataSrvSyncInput{DatasetID: "finance.expense_forms", ObjectRole: "expense_report", RecordID: "expense-runtime-1", Instance: runtimeInstance})
	if err != nil {
		t.Fatalf("SyncMaclawAppApprovalInstanceToDataSrv runtime error = %v", err)
	}
	if synced["synced"] != true || synced["action"] != "create_record_approval" || synced["approval_id"] != "approval-runtime-1" {
		t.Fatalf("runtime approval should sync to DataSrv and expose remote id: %#v", synced)
	}
	if len(captured) < 4 {
		t.Fatalf("runtime sync should add DataSrv record and approval requests: %#v", captured)
	}
	createRequest := captured[len(captured)-1].Body
	if captured[len(captured)-1].Method != http.MethodPost || captured[len(captured)-1].Path != "/api/v1/data/datasets/finance.expense_forms/records/expense-runtime-1/approvals" {
		t.Fatalf("runtime approval create request should use installed app DataSrv binding: %#v", captured[len(captured)-1])
	}
	request, ok := createRequest["request"].(map[string]interface{})
	if !ok || request["app_id"] != "expense-approval" || request["workflowSkillId"] != "expense-workflow" || request["workflowNodeId"] != "finance.director_review" {
		t.Fatalf("runtime approval create request should keep installed app workflow context: %#v", createRequest)
	}
	if payload, ok := request["resultPayload"].(map[string]interface{}); !ok || payload["business_record"] == nil {
		t.Fatalf("runtime approval create request should keep result payload for feedback: %#v", request)
	}
	if payload, ok := createRequest["result_payload"].(map[string]interface{}); !ok || payload["business_record"] == nil {
		t.Fatalf("runtime approval create request should expose top-level result payload for DataSrv: %#v", createRequest)
	}
	if outputs, ok := createRequest["outputs"].([]interface{}); !ok || len(outputs) != 1 {
		t.Fatalf("runtime approval create request should expose top-level outputs for DataSrv: %#v", createRequest)
	}
	if artifacts, ok := createRequest["artifacts"].([]interface{}); !ok || len(artifacts) != 1 {
		t.Fatalf("runtime approval create request should expose top-level artifacts for DataSrv: %#v", createRequest)
	}
	if nodes, ok := createRequest["workflow_node_ids"].([]interface{}); !ok || len(nodes) != 2 || nodes[1] != "finance.director_review" {
		t.Fatalf("runtime approval create request should expose workflow node list for DataSrv: %#v", createRequest)
	}
	runtimeList, err := app.ListMaclawAppApprovalInstances("expense-approval", "pending_my_approval", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances runtime error = %v", err)
	}
	if len(runtimeList) != 1 {
		t.Fatalf("expected one runtime approval in pending lane, got %#v", runtimeList)
	}
	gotRuntime := runtimeList[0]
	if gotRuntime.AppID != "expense-approval" || gotRuntime.ApprovalID != "approval-runtime-1" || gotRuntime.InstanceID != "wf-runtime-1" || gotRuntime.DatasetID != "finance.expense_forms" || gotRuntime.ObjectRole != "expense_report" {
		t.Fatalf("runtime approval list should preserve app and DataSrv identity: %#v", gotRuntime)
	}
	if gotRuntime.WorkflowSkillID != "expense-workflow" || gotRuntime.WorkflowVersion != "2.0.0" || gotRuntime.CurrentNode != "finance.director_review" || len(gotRuntime.CurrentNodeIDs) != 2 {
		t.Fatalf("runtime approval list should preserve workflow state: %#v", gotRuntime)
	}
	if gotRuntime.ResultPayload["text"] != "waiting for director" || len(gotRuntime.Outputs) != 1 || gotRuntime.Outputs[0].Title != "Runtime Summary" || len(gotRuntime.Artifacts) != 1 || gotRuntime.Artifacts[0].Name != "runtime-receipt.pdf" {
		t.Fatalf("runtime approval list should preserve result feedback package: %#v", gotRuntime)
	}
	reviewedInstance := gotRuntime
	reviewedInstance.Status = "approved"
	reviewedInstance.Lane = "handled"
	reviewedInstance.CurrentNode = "expense.result_pack"
	reviewedInstance.CurrentNodeIDs = []string{"expense.intake", "finance.director_review", "expense.result_pack"}
	reviewedInstance.BusinessStatus = "finance_approved"
	reviewedInstance.ResultStatus = "approved"
	reviewedInstance.Result = "director approved"
	reviewedInstance.ResultPayload = map[string]any{"approval_result": "approved", "business_status": "finance_approved", "business_record": map[string]any{"id": "expense-runtime-1", "status": "finance_approved"}, "text": "director approved"}
	reviewedInstance.Outputs = []maclawAppApprovalOutput{{Type: "content", Title: "Runtime Decision", Text: "director approved"}, {Type: "artifact", Title: "Runtime Approval PDF", ArtifactID: "runtime-approval-pdf", Artifact: &maclawAppApprovalArtifact{ID: "runtime-approval-pdf", Name: "runtime-approval.pdf", URI: "artifact://runtime/approval.pdf", Status: "ready"}}}
	reviewedInstance.Artifacts = []maclawAppApprovalArtifact{{ID: "runtime-approval-pdf", Name: "runtime-approval.pdf", URI: "artifact://runtime/approval.pdf", Status: "ready"}}
	reviewed, err := app.SyncMaclawAppApprovalInstanceToDataSrv(maclawAppApprovalDataSrvSyncInput{DatasetID: "finance.expense_forms", ObjectRole: "expense_report", RecordID: "expense-runtime-1", ApprovalID: "approval-runtime-1", Instance: reviewedInstance})
	if err != nil {
		t.Fatalf("SyncMaclawAppApprovalInstanceToDataSrv review error = %v", err)
	}
	if reviewed["synced"] != true || reviewed["action"] != "review_record_approval" || reviewed["approval_id"] != "approval-runtime-1" {
		t.Fatalf("runtime approval review should sync final decision to DataSrv: %#v", reviewed)
	}
	globalHandled, err := app.ListMaclawAppApprovalInstancesAll("handled", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstancesAll handled error = %v", err)
	}
	if len(globalHandled) != 1 {
		t.Fatalf("expected one handled approval from DataSrv, got %#v", globalHandled)
	}
	gotHandled := globalHandled[0]
	if gotHandled.AppID != "expense-approval" || gotHandled.ApprovalID != "approval-runtime-1" || gotHandled.InstanceID != "wf-runtime-1" || gotHandled.Status != "approved" || gotHandled.Lane != "handled" {
		t.Fatalf("global handled list should preserve same app approval identity: %#v", gotHandled)
	}
	if gotHandled.WorkflowSkillID != "expense-workflow" || gotHandled.WorkflowVersion != "2.0.0" || gotHandled.CurrentNode != "expense.result_pack" || len(gotHandled.CurrentNodeIDs) != 3 {
		t.Fatalf("global handled list should preserve final workflow state: %#v", gotHandled)
	}
	if gotHandled.ResultPayload["approval_result"] != "approved" || gotHandled.ResultPayload["business_status"] != "finance_approved" || len(gotHandled.Outputs) != 2 || gotHandled.Outputs[1].Title != "Runtime Approval PDF" || len(gotHandled.Artifacts) != 1 || gotHandled.Artifacts[0].Name != "runtime-approval.pdf" {
		t.Fatalf("global handled list should read final result package from DataSrv: %#v", gotHandled)
	}
}

func TestMaclawAppApprovalInstancesPersistAndFilter(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	created, err := app.RecordMaclawAppApprovalInstance(maclawAppApprovalInstance{
		AppID:              "expense-approval",
		AppName:            "Expense Approval",
		BlueprintID:        "expense.blueprint.v1",
		DatasetID:          "finance.expenses",
		ApprovalObjectRole: "expense_report",
		ApprovalEvent:      "finance.submitted",
		Title:              "Expense #1",
		Lane:               "pending_my_approval",
		Status:             "pending",
		CurrentNode:        "manager_approval",
		Owner:              "alice",
		Applicant:          "alice",
		Approver:           "manager",
		Result:             "waiting",
		WorkflowSkillID:    "expense-approval-workflow",
		WorkflowVersion:    "2.1.0",
		DetailURL:          "approval://instances/appr-1",
		BusinessStatus:     "approval_pending",
		ResultStatus:       "pending",
		FromStatus:         "submitted",
		ToStatus:           "approval_pending",
		RecordID:           "exp-1",
		BusinessEntity:     "expense",
		BusinessAction:     "submit",
		BusinessNote:       "taxi receipt",
		ResultPayload:      map[string]any{"business_record": map[string]any{"id": "exp-1"}},
		Outputs:            []maclawAppApprovalOutput{{Type: "text", Title: "Summary", Text: "waiting for manager"}},
		Artifacts:          []maclawAppApprovalArtifact{{ID: "artifact-1", Name: "receipt.pdf", URI: "artifact://approval/receipt"}},
	})
	if err != nil {
		t.Fatalf("RecordMaclawAppApprovalInstance() error = %v", err)
	}
	if created.InstanceID == "" || created.AppID != "expense-approval" || created.Lane != "pending_my_approval" || len(created.Events) != 1 {
		t.Fatalf("unexpected created instance: %#v", created)
	}
	if created.DatasetID != "finance.expenses" || created.ObjectRole != "expense_report" || created.ApprovalObjectRole != "expense_report" || created.BlueprintID != "expense.blueprint.v1" || created.ApprovalEvent != "finance.submitted" || created.RecordID != "exp-1" {
		t.Fatalf("approval instance should persist app business context: %#v", created)
	}
	if created.CreatedAt == "" || created.Applicant != "alice" || created.BusinessEntity != "expense" || created.BusinessAction != "submit" || created.BusinessNote != "taxi receipt" {
		t.Fatalf("approval instance should persist submission context: %#v", created)
	}
	if _, err := os.Stat(app.maclawAppApprovalRegistryPath()); err != nil {
		t.Fatalf("approval registry should exist: %v", err)
	}
	if _, err := app.RecordMaclawAppApprovalInstance(maclawAppApprovalInstance{AppID: "other-app", Title: "Other", Lane: "my_requests"}); err != nil {
		t.Fatalf("record other app: %v", err)
	}
	pending, err := app.ListMaclawAppApprovalInstances("expense-approval", "all", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances() error = %v", err)
	}
	if len(pending) != 1 || pending[0].InstanceID != created.InstanceID || pending[0].WorkflowSkillID != "expense-approval-workflow" {
		t.Fatalf("unexpected filtered approval instances: %#v", pending)
	}
	handled, err := app.RecordMaclawAppApprovalInstance(maclawAppApprovalInstance{
		AppID:               "expense-approval",
		InstanceID:          created.InstanceID,
		Title:               "Expense #1",
		Lane:                "handled",
		Status:              "approved",
		CurrentNode:         "completed",
		Owner:               "alice",
		Approver:            "manager",
		CurrentAssignee:     "manager",
		CurrentAssigneeType: "user",
		Result:              "approved",
		WorkflowSkillID:     "expense-approval-workflow",
		WorkflowDecisionID:  "decision-test-1",
		BusinessStatus:      "approved",
		ResultStatus:        "approved",
		ResultPayload:       map[string]any{"business_record": map[string]any{"id": "exp-1", "status": "approved"}, "text": "approved with note"},
		Outputs: []maclawAppApprovalOutput{{
			Type:  "business_record",
			Title: "Expense record",
			Data:  map[string]any{"id": "exp-1", "amount": float64(120)},
		}, {
			Type:       "artifact",
			Title:      "Approval PDF",
			ArtifactID: "artifact-1",
			Artifact:   &maclawAppApprovalArtifact{ID: "artifact-1", Name: "approval.pdf", URI: "artifact://approval/1", Status: "ready"},
		}},
		Artifacts: []maclawAppApprovalArtifact{{ID: "artifact-1", Name: "approval.pdf", URI: "artifact://approval/1", Status: "ready"}},
	})
	if err != nil {
		t.Fatalf("RecordMaclawAppApprovalInstance decision error = %v", err)
	}
	if handled.WorkflowDecisionID != "decision-test-1" || handled.BusinessStatus != "approved" || handled.ResultStatus != "approved" {
		t.Fatalf("decision result fields should persist: %#v", handled)
	}
	if handled.DatasetID != "finance.expenses" || handled.ObjectRole != "expense_report" || handled.BlueprintID != "expense.blueprint.v1" || handled.RecordID != "exp-1" || handled.BusinessNote != "taxi receipt" {
		t.Fatalf("decision update should keep existing approval context: %#v", handled)
	}
	if handled.ResultPayload["text"] != "approved with note" || len(handled.Outputs) != 2 || len(handled.Artifacts) != 1 {
		t.Fatalf("decision result payload should persist: %#v", handled)
	}
	pending, err = app.ListMaclawAppApprovalInstances("expense-approval", "pending_my_approval", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances pending after decision error = %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("handled approval should move out of pending lane: %#v", pending)
	}
	again, err := app.ListMaclawAppApprovalInstances("expense-approval", "all", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances all error = %v", err)
	}
	if len(again) != 1 || again[0].WorkflowDecisionID != "decision-test-1" {
		t.Fatalf("unexpected all approval instances after decision: %#v", again)
	}
	again[0].Events[0].Decision = "mutated"
	again[0].ResultPayload["text"] = "mutated"
	again[0].Outputs[0].Data["id"] = "mutated"
	again[0].Outputs[1].Artifact.Name = "mutated.pdf"
	again, err = app.ListMaclawAppApprovalInstances("expense-approval", "all", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances all after mutation error = %v", err)
	}
	if len(again) != 1 || again[0].Events[0].Decision == "mutated" || again[0].ResultPayload["text"] == "mutated" || again[0].Outputs[0].Data["id"] == "mutated" || again[0].Outputs[1].Artifact.Name == "mutated.pdf" {
		t.Fatalf("approval instances should be cloned: %#v", again)
	}
	if _, err := app.RecordMaclawAppApprovalInstance(maclawAppApprovalInstance{
		AppID:          "expense-approval",
		InstanceID:     "legacy-approved-lane",
		Title:          "Legacy approved lane",
		Lane:           "pending_my_approval",
		Status:         "approved",
		CurrentNode:    "completed",
		Owner:          "alice",
		Approver:       "manager",
		Result:         "approved before lane migration",
		BusinessStatus: "approved",
		ResultStatus:   "approved",
	}); err != nil {
		t.Fatalf("record legacy approved lane: %v", err)
	}
	handledList, err := app.ListMaclawAppApprovalInstances("expense-approval", "handled", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances handled error = %v", err)
	}
	if len(handledList) != 2 || handledList[0].InstanceID != "legacy-approved-lane" {
		t.Fatalf("approved status should be visible in handled lane even when stored lane is stale: %#v", handledList)
	}
	pending, err = app.ListMaclawAppApprovalInstances("expense-approval", "pending_my_approval", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances pending with stale lane error = %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("approved status should not remain visible in pending lane even when stored lane is stale: %#v", pending)
	}
	if _, err := app.ListMaclawAppApprovalInstances(" ", "all", 10); err == nil {
		t.Fatal("expected app_id required error")
	}
}

func TestMaclawAppApprovalPendingRequestAlsoAppearsForApprover(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	created, err := app.RecordMaclawAppApprovalInstance(maclawAppApprovalInstance{
		AppID:               "expense-approval",
		Title:               "Expense pending",
		Lane:                "my_requests",
		Status:              "pending",
		CurrentNode:         "manager_approval",
		Owner:               "alice",
		Applicant:           "alice",
		Approver:            "manager",
		CurrentAssignee:     "manager",
		CurrentAssigneeType: "user",
		Result:              "waiting for manager",
		WorkflowSkillID:     "expense-approval-workflow",
		BusinessStatus:      "approval_pending",
		ResultStatus:        "pending",
		RecordID:            "exp-2",
	})
	if err != nil {
		t.Fatalf("RecordMaclawAppApprovalInstance() error = %v", err)
	}
	requests, err := app.ListMaclawAppApprovalInstances("expense-approval", "my_requests", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances my_requests error = %v", err)
	}
	if len(requests) != 1 || requests[0].InstanceID != created.InstanceID {
		t.Fatalf("pending request should remain visible in my_requests: %#v", requests)
	}
	pending, err := app.ListMaclawAppApprovalInstances("expense-approval", "all", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances pending_my_approval error = %v", err)
	}
	if len(pending) != 1 || pending[0].InstanceID != created.InstanceID || pending[0].CurrentAssignee != "manager" {
		t.Fatalf("pending request with assignee should be visible to approver lane: %#v", pending)
	}
}

func TestStartMaclawAppApprovalWorkflowRunsWorkflowSkillResult(t *testing.T) {
	type capturedRequest struct {
		Method   string
		Path     string
		RawQuery string
		Body     map[string]interface{}
	}
	captured := []capturedRequest{}
	finalSynced := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := capturedRequest{Method: r.Method, Path: r.URL.Path, RawQuery: r.URL.RawQuery}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		captured = append(captured, item)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-runner-1":
			_, _ = w.Write([]byte(`{"id":"expense-runner-1","data":{"status":"draft","amount":1200}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-runner-1":
			_, _ = w.Write([]byte(`{"id":"expense-runner-1","status":"updated"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-runner-1/approvals":
			_, _ = w.Write([]byte(`{"id":"approval-runner-1","status":"pending"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/approvals/approval-runner-1/progress":
			_, _ = w.Write([]byte(`{"id":"approval-runner-1","status":"pending","progress":"manager review started"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/approvals/approval-runner-1/review":
			if item.Body["decision"] == "approved" && item.Body["workflow_node_id"] == "expense.result" {
				finalSynced = true
			}
			_, _ = w.Write([]byte(`{"id":"approval-runner-1","status":"approved"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/approvals":
			if finalSynced {
				_, _ = w.Write([]byte(`{"items":[{"id":"approval-runner-1","app_id":"expense-approval","dataset_id":"finance.expense_forms","record_id":"expense-runner-1","status":"approved","summary":"Expense Runner","workflow_skill_id":"expense-workflow","workflow_version":"2.0.0","workflow_instance_id":"wf-runner-1","workflow_decision_id":"decision-runner-1","workflow_node_id":"expense.result","workflow_node_ids":["expense.submit","manager.approval","expense.result"],"business_status":"finance_approved","result_status":"approved","result_payload":{"approval_result":"approved","business_status":"finance_approved","business_record":{"id":"expense-runner-1","status":"finance_approved"},"text":"approved by workflow skill"},"outputs":[{"type":"content","title":"Workflow Decision","text":"approved by workflow skill"},{"type":"artifact","title":"Workflow PDF","artifact":{"id":"runner-pdf","name":"runner-approved.pdf","uri":"artifact://runner/approved.pdf","status":"ready"}}],"artifacts":[{"id":"runner-pdf","name":"runner-approved.pdf","uri":"artifact://runner/approved.pdf","status":"ready"}],"request":{"approval_instance_id":"wf-runner-1","appID":"expense-approval","blueprintID":"expense.blueprint.v1","objectRole":"expense_report","approvalEvent":"finance.submitted","workflowSkillId":"expense-workflow","workflowVersion":"2.0.0","workflowNodeId":"expense.result","workflowNodeIds":["expense.submit","manager.approval","expense.result"],"businessStatus":"finance_approved","resultStatus":"approved"},"created_by":"alice","submitted_by":"alice","reviewed_by":"manager","assigned_to":"manager","created_at":"2026-06-30T10:00:00Z","updated_at":"2026-06-30T10:03:00Z"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			t.Fatalf("unexpected DataSrv request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	workflowJSON := `{"progress_instances":[{"status":"pending","workflow_instance_id":"wf-runner-1","approval_id":"approval-runner-1","record_id":"expense-runner-1","dataset_id":"finance.expense_forms","object_role":"expense_report","workflow_skill_id":"expense-workflow","workflow_version":"2.0.0","workflow_node_id":"manager.approval","workflow_node_ids":["expense.submit","manager.approval"],"current_assignee":"manager","current_assignee_type":"user","business_status":"finance_pending","result_status":"running","result":"manager review started","result_payload":{"text":"manager review started","business_record":{"id":"expense-runner-1","status":"finance_pending"}},"outputs":[{"type":"content","title":"Workflow Progress","text":"manager review started","status":"running"}]}],"approval_instance":{"status":"approved","lane":"handled","workflow_instance_id":"wf-runner-1","approval_id":"approval-runner-1","record_id":"expense-runner-1","dataset_id":"finance.expense_forms","object_role":"expense_report","workflow_skill_id":"expense-workflow","workflow_version":"2.0.0","workflow_node_id":"expense.result","workflow_node_ids":["expense.submit","manager.approval","expense.result"],"workflow_decision_id":"decision-runner-1","business_status":"finance_approved","result_status":"approved","result":"approved by workflow skill","result_payload":{"approval_result":"approved","business_status":"finance_approved","business_record":{"id":"expense-runner-1","status":"finance_approved"},"text":"approved by workflow skill"},"outputs":[{"type":"content","title":"Workflow Decision","text":"approved by workflow skill"},{"type":"artifact","title":"Workflow PDF","artifact":{"id":"runner-pdf","name":"runner-approved.pdf","uri":"artifact://runner/approved.pdf","status":"ready"}}],"artifacts":[{"id":"runner-pdf","name":"runner-approved.pdf","uri":"artifact://runner/approved.pdf","status":"ready"}]}}`
	workflowResultPath := filepath.Join(app.testHomeDir, "workflow-result.txt")
	if err := os.WriteFile(workflowResultPath, []byte("workflow_result="+workflowJSON+"\n"), 0o644); err != nil {
		t.Fatalf("write workflow result fixture: %v", err)
	}
	workflowCommand := `cat "` + workflowResultPath + `"`
	if os.PathSeparator == '\\' {
		workflowCommand = `type "` + workflowResultPath + `"`
	}
	if err := app.skillExecutor.Register(corelib.NLSkillEntry{
		Name:   "expense-workflow",
		Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action:  "bash",
			Params:  map[string]interface{}{"command": workflowCommand},
			Capture: map[string]string{"workflow_result": `workflow_result=(.+)`},
		}},
	}); err != nil {
		t.Fatalf("register workflow skill: %v", err)
	}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "alice", Role: "requester"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}
	if err := app.writeMaclawAppInstallRegistry(maclawAppInstallRegistry{
		Schema:    "maclaw.app.installs.v1",
		UpdatedAt: "2026-06-30T10:00:00Z",
		Installs: []maclawAppInstallRecord{{
			AppID:       "expense-approval",
			AppName:     "Expense Approval",
			Kind:        "enterprise_approval_app",
			InstalledAt: "2026-06-30T10:00:00Z",
			VersionSnapshot: maclawAppInstallVersionSnapshot{
				WorkflowSkills:   []maclawAppInstallSkillVersionSnapshot{{ID: "expense-workflow", Version: "2.0.0", Kind: "workflow_skill", Source: "hub"}},
				ApprovalBindings: []maclawAppInstallApprovalBindingSnapshot{{Event: "finance.submitted", DatasetID: "finance.expense_forms", BlueprintID: "expense.blueprint.v1", ObjectRole: "expense_report", WorkflowSkillID: "expense-workflow", WorkflowVersion: "2.0.0"}},
			},
			WorkflowContract: map[string]any{"schema": "maclaw.app.workflow_contract.v1", "workflowSkillId": "expense-workflow", "workflowVersion": "2.0.0", "objectRole": "expense_report"},
			Package:          map[string]any{"apps": []any{map[string]any{"app": map[string]any{"binding": map[string]any{"workflow": map[string]any{"submitNode": "expense.submit", "approvalNode": "manager.approval"}}}}}},
		}},
	}); err != nil {
		t.Fatalf("write install registry: %v", err)
	}

	started, err := app.StartMaclawAppApprovalWorkflow(MaclawAppApprovalWorkflowStartInput{
		AppID:            "expense-approval",
		DatasetID:        "finance.expense_forms",
		ObjectRole:       "expense_report",
		RecordID:         "expense-runner-1",
		Title:            "Expense Runner",
		Applicant:        "alice",
		Approver:         "manager",
		BusinessNote:     "submit through real workflow skill",
		BusinessPayload:  map[string]any{"amount": float64(1200)},
		RunWorkflowSkill: true,
	})
	if err != nil {
		t.Fatalf("StartMaclawAppApprovalWorkflow() with runner error = %v", err)
	}
	workflowRun, ok := started["workflow_run"].(map[string]any)
	if !ok || workflowRun["ran"] != true {
		t.Fatalf("expected workflow skill run evidence: %#v", started["workflow_run"])
	}
	progressInstances, ok := workflowRun["progress_instances"].([]maclawAppApprovalInstance)
	if !ok || len(progressInstances) != 1 || progressInstances[0].Status != "pending" || progressInstances[0].CurrentNode != "manager.approval" || progressInstances[0].ResultStatus != "running" {
		t.Fatalf("workflow skill progress should become running approval instance: %#v", workflowRun["progress_instances"])
	}
	instance, ok := workflowRun["instance"].(maclawAppApprovalInstance)
	if !ok || instance.ApprovalID != "approval-runner-1" || instance.Status != "approved" || instance.CurrentNode != "expense.result" || instance.WorkflowDecisionID != "decision-runner-1" {
		t.Fatalf("workflow skill result should become approval instance: %#v", workflowRun["instance"])
	}
	if len(instance.WorkflowNodeIDs) != 3 || instance.WorkflowNodeIDs[0] != "expense.submit" || instance.WorkflowNodeIDs[2] != "expense.result" {
		t.Fatalf("workflow skill result should preserve workflow node path: %#v", instance.WorkflowNodeIDs)
	}
	if instance.ResultPayload["approval_result"] != "approved" || len(instance.Outputs) != 2 || instance.Outputs[1].Title != "Workflow PDF" || len(instance.Artifacts) != 1 || instance.Artifacts[0].Name != "runner-approved.pdf" {
		t.Fatalf("workflow skill result should preserve result content and file artifact: %#v", instance)
	}
	if len(captured) < 5 {
		t.Fatalf("expected create approval and final review sync requests, got %#v", captured)
	}
	progressSynced := false
	lastReview := false
	for _, req := range captured {
		if req.Method == http.MethodPost && req.Path == "/api/v1/data/approvals/approval-runner-1/progress" {
			progressSynced = true
			if req.Body["workflow_node_id"] != "manager.approval" || req.Body["business_status"] != "finance_pending" || req.Body["result_status"] != "running" || req.Body["current_assignee"] != "manager" || req.Body["progress"] != "manager review started" {
				t.Fatalf("progress request should carry workflow skill running fields: %#v", req.Body)
			}
			if outputs, ok := req.Body["outputs"].([]interface{}); !ok || len(outputs) != 1 {
				t.Fatalf("progress request should carry running outputs: %#v", req.Body)
			}
		}
		if req.Method == http.MethodPost && req.Path == "/api/v1/data/approvals/approval-runner-1/review" {
			lastReview = true
			if req.Body["decision"] != "approved" || req.Body["workflow_decision_id"] != "decision-runner-1" || req.Body["workflow_node_id"] != "expense.result" {
				t.Fatalf("review request should carry workflow skill final fields: %#v", req.Body)
			}
			if nodes, ok := req.Body["workflow_node_ids"].([]interface{}); !ok || len(nodes) != 3 || nodes[2] != "expense.result" {
				t.Fatalf("review request should carry workflow node path: %#v", req.Body)
			}
		}
	}
	if !progressSynced {
		t.Fatalf("workflow skill running progress should update DataSrv approval progress, captured=%#v", captured)
	}
	if !lastReview {
		t.Fatalf("workflow skill final result should review DataSrv approval, captured=%#v", captured)
	}
	appHandledRemote, err := app.ListMaclawAppApprovalInstances("expense-approval", "handled", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances handled after runner sync error = %v", err)
	}
	if len(appHandledRemote) != 1 {
		t.Fatalf("workflow runner final sync should be readable back from single app handled lane: %#v", appHandledRemote)
	}
	handledRemote, err := app.ListMaclawAppApprovalInstancesAll("handled", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstancesAll handled after runner sync error = %v", err)
	}
	if len(handledRemote) != 1 {
		t.Fatalf("workflow runner final sync should be readable back from global DataSrv handled lane: %#v", handledRemote)
	}
	readBack := appHandledRemote[0]
	globalReadBack := handledRemote[0]
	if globalReadBack.AppID != readBack.AppID || globalReadBack.ApprovalID != readBack.ApprovalID || globalReadBack.InstanceID != readBack.InstanceID || globalReadBack.WorkflowDecisionID != readBack.WorkflowDecisionID {
		t.Fatalf("single app and global approval centers should show the same workflow instance, app=%#v global=%#v", readBack, globalReadBack)
	}
	if readBack.ApprovalID != "approval-runner-1" || readBack.InstanceID != "wf-runner-1" || readBack.Status != "approved" || readBack.CurrentNode != "expense.result" || readBack.WorkflowDecisionID != "decision-runner-1" {
		t.Fatalf("DataSrv readback should preserve final workflow identity and node state: %#v", readBack)
	}
	if len(readBack.WorkflowNodeIDs) != 3 || readBack.WorkflowNodeIDs[0] != "expense.submit" || readBack.WorkflowNodeIDs[2] != "expense.result" {
		t.Fatalf("DataSrv readback should preserve workflow node path: %#v", readBack.WorkflowNodeIDs)
	}
	if readBack.ResultPayload["approval_result"] != "approved" || readBack.ResultPayload["text"] != "approved by workflow skill" || len(readBack.Outputs) != 2 || len(readBack.Artifacts) != 1 || readBack.Artifacts[0].Name != "runner-approved.pdf" {
		t.Fatalf("DataSrv readback should preserve result payload, content output, and file artifact: %#v", readBack)
	}
	if len(globalReadBack.Outputs) != len(readBack.Outputs) || len(globalReadBack.Artifacts) != len(readBack.Artifacts) || globalReadBack.Artifacts[0].URI != readBack.Artifacts[0].URI {
		t.Fatalf("global approval center should preserve the same result outputs and artifacts as app detail, app=%#v global=%#v", readBack, globalReadBack)
	}
	sawAppScopedQuery := false
	sawGlobalQuery := false
	for _, req := range captured {
		if req.Method == http.MethodGet && req.Path == "/api/v1/data/approvals" {
			if req.RawQuery == "app_id=expense-approval&lane=handled&limit=10" {
				sawAppScopedQuery = true
			}
			if req.RawQuery == "lane=handled&limit=10" {
				sawGlobalQuery = true
			}
		}
	}
	if !sawAppScopedQuery || !sawGlobalQuery {
		t.Fatalf("approval readback should query both app-scoped and global handled lanes, appScoped=%v global=%v captured=%#v", sawAppScopedQuery, sawGlobalQuery, captured)
	}
}

func TestStartMaclawAppApprovalWorkflowRunsAttentionViewOnlyWorkflowResult(t *testing.T) {
	type capturedRequest struct {
		Method   string
		Path     string
		RawQuery string
		Body     map[string]interface{}
	}
	captured := []capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := capturedRequest{Method: r.Method, Path: r.URL.Path, RawQuery: r.URL.RawQuery}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		captured = append(captured, item)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-attn-runner-1":
			_, _ = w.Write([]byte(`{"id":"expense-attn-runner-1","data":{"status":"draft","amount":760}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-attn-runner-1":
			_, _ = w.Write([]byte(`{"id":"expense-attn-runner-1","status":"attention"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-attn-runner-1/approvals":
			_, _ = w.Write([]byte(`{"id":"approval-attn-runner-1","status":"pending"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/approvals":
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			t.Fatalf("unexpected DataSrv request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	workflowJSON := `{"approval_instance":{"status":"attention","lane":"attention","workflow_instance_id":"wf-attn-runner-1","approval_id":"approval-attn-runner-1","record_id":"expense-attn-runner-1","dataset_id":"finance.expense_forms","object_role":"expense_report","workflow_skill_id":"expense-workflow","workflow_version":"2.0.0","workflow_node_id":"expense.attention","workflow_node_ids":["expense.submit","manager.approval","expense.attention"],"business_status":"finance_attention","result_status":"attention","result":"invoice missing, view only","result_payload":{"approval_result":"attention","business_status":"finance_attention","business_record":{"id":"expense-attn-runner-1","status":"finance_attention"},"text":"invoice missing, view only"},"outputs":[{"type":"content","title":"Needs attention","text":"invoice missing, view only","status":"attention"}]}}`
	workflowResultPath := filepath.Join(app.testHomeDir, "workflow-attention-result.txt")
	if err := os.WriteFile(workflowResultPath, []byte("workflow_result="+workflowJSON+"\n"), 0o644); err != nil {
		t.Fatalf("write workflow attention result fixture: %v", err)
	}
	workflowCommand := `cat "` + workflowResultPath + `"`
	if os.PathSeparator == '\\' {
		workflowCommand = `type "` + workflowResultPath + `"`
	}
	if err := app.skillExecutor.Register(corelib.NLSkillEntry{
		Name:   "expense-workflow",
		Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action:  "bash",
			Params:  map[string]interface{}{"command": workflowCommand},
			Capture: map[string]string{"workflow_result": `workflow_result=(.+)`},
		}},
	}); err != nil {
		t.Fatalf("register workflow skill: %v", err)
	}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "alice", Role: "requester"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}
	if err := app.writeMaclawAppInstallRegistry(maclawAppInstallRegistry{
		Schema:    "maclaw.app.installs.v1",
		UpdatedAt: "2026-06-30T10:00:00Z",
		Installs: []maclawAppInstallRecord{{
			AppID:       "expense-approval",
			AppName:     "Expense Approval",
			Kind:        "enterprise_approval_app",
			InstalledAt: "2026-06-30T10:00:00Z",
			VersionSnapshot: maclawAppInstallVersionSnapshot{
				WorkflowSkills:   []maclawAppInstallSkillVersionSnapshot{{ID: "expense-workflow", Version: "2.0.0", Kind: "workflow_skill", Source: "hub"}},
				ApprovalBindings: []maclawAppInstallApprovalBindingSnapshot{{Event: "finance.submitted", DatasetID: "finance.expense_forms", BlueprintID: "expense.blueprint.v1", ObjectRole: "expense_report", WorkflowSkillID: "expense-workflow", WorkflowVersion: "2.0.0"}},
			},
			WorkflowContract: map[string]any{"schema": "maclaw.app.workflow_contract.v1", "workflowSkillId": "expense-workflow", "workflowVersion": "2.0.0", "objectRole": "expense_report"},
			Package:          map[string]any{"apps": []any{map[string]any{"app": map[string]any{"binding": map[string]any{"workflow": map[string]any{"submitNode": "expense.submit", "approvalNode": "manager.approval", "attentionNode": "expense.attention"}}}}}},
		}},
	}); err != nil {
		t.Fatalf("write install registry: %v", err)
	}

	started, err := app.StartMaclawAppApprovalWorkflow(MaclawAppApprovalWorkflowStartInput{
		AppID:            "expense-approval",
		DatasetID:        "finance.expense_forms",
		ObjectRole:       "expense_report",
		RecordID:         "expense-attn-runner-1",
		Title:            "Expense Attention",
		Applicant:        "alice",
		Approver:         "manager",
		BusinessNote:     "submit attention workflow",
		BusinessPayload:  map[string]any{"amount": float64(760)},
		RunWorkflowSkill: true,
	})
	if err != nil {
		t.Fatalf("StartMaclawAppApprovalWorkflow() attention error = %v", err)
	}
	workflowRun, ok := started["workflow_run"].(map[string]any)
	if !ok || workflowRun["ran"] != true {
		t.Fatalf("expected attention workflow run evidence: %#v", started["workflow_run"])
	}
	instance, ok := workflowRun["instance"].(maclawAppApprovalInstance)
	if !ok || instance.Status != "attention" || instance.Lane != "attention" || instance.ResultStatus != "attention" || instance.CurrentNode != "expense.attention" {
		t.Fatalf("workflow attention result should become attention approval instance: %#v", workflowRun["instance"])
	}
	if instance.ResultPayload["approval_result"] != "attention" || instance.ResultPayload["business_status"] != "finance_attention" || len(instance.Outputs) != 1 || instance.Outputs[0].Status != "attention" {
		t.Fatalf("workflow attention result should preserve result package: %#v", instance)
	}
	reviewCalled := false
	businessRecordPatched := false
	for _, req := range captured {
		if strings.Contains(req.Path, "/review") {
			reviewCalled = true
		}
		if req.Method == http.MethodPatch && req.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-attn-runner-1" {
			data := anyMap(req.Body["data"])
			if data["approval_status"] == "attention" || req.Body["approval_status"] == "attention" {
				businessRecordPatched = true
				lane := firstNonEmptyMaclawAppString(maclawAppStringFromAny(data["approval_lane"]), maclawAppStringFromAny(req.Body["approval_lane"]))
				if lane != "attention" {
					t.Fatalf("attention result should patch business record as view-only: %#v", req.Body)
				}
			}
		}
	}
	if reviewCalled {
		t.Fatalf("attention result is view-only and must not review DataSrv approval: %#v", captured)
	}
	if !businessRecordPatched {
		t.Fatalf("attention result should still update business record evidence: %#v", captured)
	}
	attention, err := app.ListMaclawAppApprovalInstances("expense-approval", "attention", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances attention error = %v", err)
	}
	if len(attention) != 1 || attention[0].Status != "attention" || attention[0].ResultPayload["approval_result"] != "attention" {
		t.Fatalf("workflow attention result should be visible in attention lane: %#v", attention)
	}
	globalAttention, err := app.ListMaclawAppApprovalInstancesAll("attention", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstancesAll attention error = %v", err)
	}
	if len(globalAttention) != 1 {
		t.Fatalf("workflow attention result should be visible in global attention lane: %#v", globalAttention)
	}
	assertMaclawAppApprovalReadbackSameInstanceForTest(t, attention[0], globalAttention[0])
}

func TestStartMaclawAppApprovalWorkflowRunsRejectedWorkflowResult(t *testing.T) {
	type capturedRequest struct {
		Method   string
		Path     string
		RawQuery string
		Body     map[string]interface{}
	}
	captured := []capturedRequest{}
	finalSynced := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := capturedRequest{Method: r.Method, Path: r.URL.Path, RawQuery: r.URL.RawQuery}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		captured = append(captured, item)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-reject-runner-1":
			_, _ = w.Write([]byte(`{"id":"expense-reject-runner-1","data":{"status":"draft","amount":430}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-reject-runner-1":
			_, _ = w.Write([]byte(`{"id":"expense-reject-runner-1","status":"finance_rejected"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-reject-runner-1/approvals":
			_, _ = w.Write([]byte(`{"id":"approval-reject-runner-1","status":"pending"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/approvals/approval-reject-runner-1/review":
			if item.Body["decision"] == "rejected" && item.Body["workflow_node_id"] == "expense.result" {
				finalSynced = true
			}
			_, _ = w.Write([]byte(`{"id":"approval-reject-runner-1","status":"rejected"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/approvals":
			if finalSynced {
				_, _ = w.Write([]byte(`{"items":[{"id":"approval-reject-runner-1","app_id":"expense-approval","dataset_id":"finance.expense_forms","record_id":"expense-reject-runner-1","status":"rejected","summary":"Expense Rejected","workflow_skill_id":"expense-workflow","workflow_version":"2.0.0","workflow_instance_id":"wf-reject-runner-1","workflow_decision_id":"decision-reject-runner-1","workflow_node_id":"expense.result","workflow_node_ids":["expense.submit","manager.approval","expense.result"],"business_status":"finance_rejected","result_status":"rejected","result_payload":{"approval_result":"rejected","business_status":"finance_rejected","business_record":{"id":"expense-reject-runner-1","status":"finance_rejected"},"text":"rejected by policy"},"outputs":[{"type":"content","title":"Workflow Decision","text":"rejected by policy","status":"rejected"}],"request":{"approval_instance_id":"wf-reject-runner-1","appID":"expense-approval","objectRole":"expense_report","approvalEvent":"finance.submitted","workflowSkillId":"expense-workflow","workflowVersion":"2.0.0","workflowNodeId":"expense.result","workflowNodeIds":["expense.submit","manager.approval","expense.result"],"businessStatus":"finance_rejected","resultStatus":"rejected"},"created_by":"alice","submitted_by":"alice","reviewed_by":"manager","assigned_to":"manager"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			t.Fatalf("unexpected DataSrv request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	workflowJSON := `{"approval_instance":{"status":"rejected","lane":"handled","workflow_instance_id":"wf-reject-runner-1","approval_id":"approval-reject-runner-1","record_id":"expense-reject-runner-1","dataset_id":"finance.expense_forms","object_role":"expense_report","workflow_skill_id":"expense-workflow","workflow_version":"2.0.0","workflow_node_id":"expense.result","workflow_node_ids":["expense.submit","manager.approval","expense.result"],"workflow_decision_id":"decision-reject-runner-1","business_status":"finance_rejected","result_status":"rejected","result":"rejected by policy","result_payload":{"approval_result":"rejected","business_status":"finance_rejected","business_record":{"id":"expense-reject-runner-1","status":"finance_rejected"},"text":"rejected by policy"},"outputs":[{"type":"content","title":"Workflow Decision","text":"rejected by policy","status":"rejected"}]}}`
	workflowResultPath := filepath.Join(app.testHomeDir, "workflow-rejected-result.txt")
	if err := os.WriteFile(workflowResultPath, []byte("workflow_result="+workflowJSON+"\n"), 0o644); err != nil {
		t.Fatalf("write workflow rejected result fixture: %v", err)
	}
	workflowCommand := `cat "` + workflowResultPath + `"`
	if os.PathSeparator == '\\' {
		workflowCommand = `type "` + workflowResultPath + `"`
	}
	if err := app.skillExecutor.Register(corelib.NLSkillEntry{
		Name:   "expense-workflow",
		Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action:  "bash",
			Params:  map[string]interface{}{"command": workflowCommand},
			Capture: map[string]string{"workflow_result": `workflow_result=(.+)`},
		}},
	}); err != nil {
		t.Fatalf("register workflow skill: %v", err)
	}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "alice", Role: "requester"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}
	if err := app.writeMaclawAppInstallRegistry(maclawAppInstallRegistry{
		Schema:    "maclaw.app.installs.v1",
		UpdatedAt: "2026-06-30T10:00:00Z",
		Installs: []maclawAppInstallRecord{{
			AppID:       "expense-approval",
			AppName:     "Expense Approval",
			Kind:        "enterprise_approval_app",
			InstalledAt: "2026-06-30T10:00:00Z",
			VersionSnapshot: maclawAppInstallVersionSnapshot{
				WorkflowSkills:   []maclawAppInstallSkillVersionSnapshot{{ID: "expense-workflow", Version: "2.0.0", Kind: "workflow_skill", Source: "hub"}},
				ApprovalBindings: []maclawAppInstallApprovalBindingSnapshot{{Event: "finance.submitted", DatasetID: "finance.expense_forms", BlueprintID: "expense.blueprint.v1", ObjectRole: "expense_report", WorkflowSkillID: "expense-workflow", WorkflowVersion: "2.0.0"}},
			},
			WorkflowContract: map[string]any{"schema": "maclaw.app.workflow_contract.v1", "workflowSkillId": "expense-workflow", "workflowVersion": "2.0.0", "objectRole": "expense_report"},
			Package:          map[string]any{"apps": []any{map[string]any{"app": map[string]any{"binding": map[string]any{"workflow": map[string]any{"submitNode": "expense.submit", "approvalNode": "manager.approval", "resultNode": "expense.result"}}}}}},
		}},
	}); err != nil {
		t.Fatalf("write install registry: %v", err)
	}

	started, err := app.StartMaclawAppApprovalWorkflow(MaclawAppApprovalWorkflowStartInput{
		AppID:            "expense-approval",
		DatasetID:        "finance.expense_forms",
		ObjectRole:       "expense_report",
		RecordID:         "expense-reject-runner-1",
		Title:            "Expense Rejected",
		Applicant:        "alice",
		Approver:         "manager",
		BusinessNote:     "submit rejected workflow",
		BusinessPayload:  map[string]any{"amount": float64(430)},
		RunWorkflowSkill: true,
	})
	if err != nil {
		t.Fatalf("StartMaclawAppApprovalWorkflow() rejected error = %v", err)
	}
	workflowRun, ok := started["workflow_run"].(map[string]any)
	if !ok || workflowRun["ran"] != true {
		t.Fatalf("expected rejected workflow run evidence: %#v", started["workflow_run"])
	}
	instance, ok := workflowRun["instance"].(maclawAppApprovalInstance)
	if !ok || instance.Status != "rejected" || instance.Lane != "handled" || instance.ResultStatus != "rejected" || instance.CurrentNode != "expense.result" || instance.WorkflowDecisionID != "decision-reject-runner-1" {
		t.Fatalf("workflow rejected result should become handled approval instance: %#v", workflowRun["instance"])
	}
	if instance.ResultPayload["approval_result"] != "rejected" || instance.ResultPayload["business_status"] != "finance_rejected" || len(instance.Outputs) != 1 || instance.Outputs[0].Status != "rejected" {
		t.Fatalf("workflow rejected result should preserve result package: %#v", instance)
	}
	reviewSynced := false
	for _, req := range captured {
		if req.Method == http.MethodPost && req.Path == "/api/v1/data/approvals/approval-reject-runner-1/review" {
			reviewSynced = true
			if req.Body["decision"] != "rejected" || req.Body["workflow_decision_id"] != "decision-reject-runner-1" || req.Body["workflow_node_id"] != "expense.result" || req.Body["business_status"] != "finance_rejected" || req.Body["result_status"] != "rejected" {
				t.Fatalf("rejected review should carry workflow final fields: %#v", req.Body)
			}
			if outputs, ok := req.Body["outputs"].([]interface{}); !ok || len(outputs) != 1 {
				t.Fatalf("rejected review should carry output block: %#v", req.Body)
			}
		}
	}
	if !reviewSynced {
		t.Fatalf("rejected workflow result should review DataSrv approval, captured=%#v", captured)
	}
	handled, err := app.ListMaclawAppApprovalInstances("expense-approval", "handled", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances handled error = %v", err)
	}
	if len(handled) != 1 || handled[0].Status != "rejected" || handled[0].ResultPayload["approval_result"] != "rejected" || len(handled[0].Outputs) != 1 {
		t.Fatalf("workflow rejected result should be visible in handled lane: %#v", handled)
	}
	globalHandled, err := app.ListMaclawAppApprovalInstancesAll("handled", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstancesAll handled error = %v", err)
	}
	if len(globalHandled) != 1 || globalHandled[0].Status != "rejected" || globalHandled[0].ResultPayload["approval_result"] != "rejected" || len(globalHandled[0].Outputs) != 1 {
		t.Fatalf("workflow rejected result should be visible in global handled lane: %#v", globalHandled)
	}
	assertMaclawAppApprovalReadbackSameInstanceForTest(t, handled[0], globalHandled[0])
	sawAppScopedQuery := false
	sawGlobalQuery := false
	for _, req := range captured {
		if req.Method == http.MethodGet && req.Path == "/api/v1/data/approvals" {
			if req.RawQuery == "app_id=expense-approval&lane=handled&limit=10" {
				sawAppScopedQuery = true
			}
			if req.RawQuery == "lane=handled&limit=10" {
				sawGlobalQuery = true
			}
		}
	}
	if !sawAppScopedQuery || !sawGlobalQuery {
		t.Fatalf("rejected approval readback should query both app-scoped and global handled lanes, appScoped=%v global=%v captured=%#v", sawAppScopedQuery, sawGlobalQuery, captured)
	}
}

func TestStartMaclawAppApprovalWorkflowRunsTimeoutWorkflowResult(t *testing.T) {
	type capturedRequest struct {
		Method   string
		Path     string
		RawQuery string
		Body     map[string]interface{}
	}
	captured := []capturedRequest{}
	finalSynced := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := capturedRequest{Method: r.Method, Path: r.URL.Path, RawQuery: r.URL.RawQuery}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		captured = append(captured, item)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-timeout-runner-1":
			_, _ = w.Write([]byte(`{"id":"expense-timeout-runner-1","data":{"status":"draft","amount":520}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-timeout-runner-1":
			_, _ = w.Write([]byte(`{"id":"expense-timeout-runner-1","status":"approval_timeout"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-timeout-runner-1/approvals":
			_, _ = w.Write([]byte(`{"id":"approval-timeout-runner-1","status":"pending"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/approvals/approval-timeout-runner-1/review":
			if item.Body["decision"] == "timeout" && item.Body["workflow_node_id"] == "expense.timeout" {
				finalSynced = true
			}
			_, _ = w.Write([]byte(`{"id":"approval-timeout-runner-1","status":"timeout"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/approvals":
			if finalSynced {
				_, _ = w.Write([]byte(`{"items":[{"id":"approval-timeout-runner-1","app_id":"expense-approval","dataset_id":"finance.expense_forms","record_id":"expense-timeout-runner-1","status":"timeout","summary":"Expense Timeout","workflow_skill_id":"expense-workflow","workflow_version":"2.0.0","workflow_instance_id":"wf-timeout-runner-1","workflow_decision_id":"decision-timeout-runner-1","workflow_node_id":"expense.timeout","workflow_node_ids":["expense.submit","manager.approval","expense.timeout"],"business_status":"approval_timeout","result_status":"timeout","result_payload":{"approval_result":"timeout","business_status":"approval_timeout","business_record":{"id":"expense-timeout-runner-1","status":"approval_timeout"},"text":"approval timed out"},"outputs":[{"type":"content","title":"Workflow Timeout","text":"approval timed out","status":"timeout"}],"request":{"approval_instance_id":"wf-timeout-runner-1","appID":"expense-approval","objectRole":"expense_report","approvalEvent":"finance.submitted","workflowSkillId":"expense-workflow","workflowVersion":"2.0.0","workflowNodeId":"expense.timeout","workflowNodeIds":["expense.submit","manager.approval","expense.timeout"],"businessStatus":"approval_timeout","resultStatus":"timeout"},"created_by":"alice","submitted_by":"alice","assigned_to":"manager"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			t.Fatalf("unexpected DataSrv request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	workflowJSON := `{"approval_instance":{"status":"timeout","lane":"handled","workflow_instance_id":"wf-timeout-runner-1","approval_id":"approval-timeout-runner-1","record_id":"expense-timeout-runner-1","dataset_id":"finance.expense_forms","object_role":"expense_report","workflow_skill_id":"expense-workflow","workflow_version":"2.0.0","workflow_node_id":"expense.timeout","workflow_node_ids":["expense.submit","manager.approval","expense.timeout"],"workflow_decision_id":"decision-timeout-runner-1","business_status":"approval_timeout","result_status":"timeout","result":"approval timed out","result_payload":{"approval_result":"timeout","business_status":"approval_timeout","business_record":{"id":"expense-timeout-runner-1","status":"approval_timeout"},"text":"approval timed out"},"outputs":[{"type":"content","title":"Workflow Timeout","text":"approval timed out","status":"timeout"}]}}`
	workflowResultPath := filepath.Join(app.testHomeDir, "workflow-timeout-result.txt")
	if err := os.WriteFile(workflowResultPath, []byte("workflow_result="+workflowJSON+"\n"), 0o644); err != nil {
		t.Fatalf("write workflow timeout result fixture: %v", err)
	}
	workflowCommand := `cat "` + workflowResultPath + `"`
	if os.PathSeparator == '\\' {
		workflowCommand = `type "` + workflowResultPath + `"`
	}
	if err := app.skillExecutor.Register(corelib.NLSkillEntry{
		Name:   "expense-workflow",
		Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action:  "bash",
			Params:  map[string]interface{}{"command": workflowCommand},
			Capture: map[string]string{"workflow_result": `workflow_result=(.+)`},
		}},
	}); err != nil {
		t.Fatalf("register workflow skill: %v", err)
	}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "alice", Role: "requester"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}
	if err := app.writeMaclawAppInstallRegistry(maclawAppInstallRegistry{
		Schema:    "maclaw.app.installs.v1",
		UpdatedAt: "2026-06-30T10:00:00Z",
		Installs: []maclawAppInstallRecord{{
			AppID:       "expense-approval",
			AppName:     "Expense Approval",
			Kind:        "enterprise_approval_app",
			InstalledAt: "2026-06-30T10:00:00Z",
			VersionSnapshot: maclawAppInstallVersionSnapshot{
				WorkflowSkills:   []maclawAppInstallSkillVersionSnapshot{{ID: "expense-workflow", Version: "2.0.0", Kind: "workflow_skill", Source: "hub"}},
				ApprovalBindings: []maclawAppInstallApprovalBindingSnapshot{{Event: "finance.submitted", ObjectRole: "expense_report", WorkflowSkillID: "expense-workflow", WorkflowVersion: "2.0.0"}},
			},
			WorkflowContract: map[string]any{"schema": "maclaw.app.workflow_contract.v1", "workflowSkillId": "expense-workflow", "workflowVersion": "2.0.0", "objectRole": "expense_report"},
			Package:          map[string]any{"apps": []any{map[string]any{"app": map[string]any{"binding": map[string]any{"workflow": map[string]any{"submitNode": "expense.submit", "approvalNode": "manager.approval", "resultNode": "expense.timeout"}}}}}},
		}},
	}); err != nil {
		t.Fatalf("write install registry: %v", err)
	}

	started, err := app.StartMaclawAppApprovalWorkflow(MaclawAppApprovalWorkflowStartInput{
		AppID:            "expense-approval",
		DatasetID:        "finance.expense_forms",
		ObjectRole:       "expense_report",
		RecordID:         "expense-timeout-runner-1",
		Title:            "Expense Timeout",
		Applicant:        "alice",
		Approver:         "manager",
		BusinessNote:     "submit timeout workflow",
		BusinessPayload:  map[string]any{"amount": float64(520)},
		RunWorkflowSkill: true,
	})
	if err != nil {
		t.Fatalf("StartMaclawAppApprovalWorkflow() timeout error = %v", err)
	}
	workflowRun, ok := started["workflow_run"].(map[string]any)
	if !ok || workflowRun["ran"] != true {
		t.Fatalf("expected timeout workflow run evidence: %#v", started["workflow_run"])
	}
	instance, ok := workflowRun["instance"].(maclawAppApprovalInstance)
	if !ok || instance.Status != "timeout" || instance.Lane != "handled" || instance.ResultStatus != "timeout" || instance.CurrentNode != "expense.timeout" || instance.WorkflowDecisionID != "decision-timeout-runner-1" {
		t.Fatalf("workflow timeout result should become handled approval instance: %#v", workflowRun["instance"])
	}
	if instance.ResultPayload["approval_result"] != "timeout" || instance.ResultPayload["business_status"] != "approval_timeout" || len(instance.Outputs) != 1 || instance.Outputs[0].Status != "timeout" {
		t.Fatalf("workflow timeout result should preserve result package: %#v", instance)
	}
	reviewSynced := false
	for _, req := range captured {
		if req.Method == http.MethodPost && req.Path == "/api/v1/data/approvals/approval-timeout-runner-1/review" {
			reviewSynced = true
			if req.Body["decision"] != "timeout" || req.Body["workflow_decision_id"] != "decision-timeout-runner-1" || req.Body["workflow_node_id"] != "expense.timeout" || req.Body["business_status"] != "approval_timeout" || req.Body["result_status"] != "timeout" {
				t.Fatalf("timeout review should carry workflow final fields: %#v", req.Body)
			}
			if outputs, ok := req.Body["outputs"].([]interface{}); !ok || len(outputs) != 1 {
				t.Fatalf("timeout review should carry output block: %#v", req.Body)
			}
		}
	}
	if !reviewSynced {
		t.Fatalf("timeout workflow result should review DataSrv approval, captured=%#v", captured)
	}
	handled, err := app.ListMaclawAppApprovalInstances("expense-approval", "handled", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances handled error = %v", err)
	}
	if len(handled) != 1 || handled[0].Status != "timeout" || handled[0].ResultPayload["approval_result"] != "timeout" || len(handled[0].Outputs) != 1 {
		t.Fatalf("workflow timeout result should be visible in handled lane: %#v", handled)
	}
	globalHandled, err := app.ListMaclawAppApprovalInstancesAll("handled", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstancesAll handled error = %v", err)
	}
	if len(globalHandled) != 1 || globalHandled[0].Status != "timeout" || globalHandled[0].ResultPayload["approval_result"] != "timeout" || len(globalHandled[0].Outputs) != 1 {
		t.Fatalf("workflow timeout result should be visible in global handled lane: %#v", globalHandled)
	}
	assertMaclawAppApprovalReadbackSameInstanceForTest(t, handled[0], globalHandled[0])
	sawAppScopedQuery := false
	sawGlobalQuery := false
	for _, req := range captured {
		if req.Method == http.MethodGet && req.Path == "/api/v1/data/approvals" {
			if req.RawQuery == "app_id=expense-approval&lane=handled&limit=10" {
				sawAppScopedQuery = true
			}
			if req.RawQuery == "lane=handled&limit=10" {
				sawGlobalQuery = true
			}
		}
	}
	if !sawAppScopedQuery || !sawGlobalQuery {
		t.Fatalf("timeout approval readback should query both app-scoped and global handled lanes, appScoped=%v global=%v captured=%#v", sawAppScopedQuery, sawGlobalQuery, captured)
	}
}

func TestStartMaclawAppApprovalWorkflowRunsCancelledWorkflowResult(t *testing.T) {
	type capturedRequest struct {
		Method   string
		Path     string
		RawQuery string
		Body     map[string]interface{}
	}
	captured := []capturedRequest{}
	finalSynced := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := capturedRequest{Method: r.Method, Path: r.URL.Path, RawQuery: r.URL.RawQuery}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		captured = append(captured, item)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-cancel-runner-1":
			_, _ = w.Write([]byte(`{"id":"expense-cancel-runner-1","data":{"status":"draft","amount":310}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-cancel-runner-1":
			_, _ = w.Write([]byte(`{"id":"expense-cancel-runner-1","status":"approval_cancelled"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-cancel-runner-1/approvals":
			_, _ = w.Write([]byte(`{"id":"approval-cancel-runner-1","status":"pending"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/approvals/approval-cancel-runner-1/review":
			if item.Body["decision"] == "cancelled" && item.Body["workflow_node_id"] == "expense.cancelled" {
				finalSynced = true
			}
			_, _ = w.Write([]byte(`{"id":"approval-cancel-runner-1","status":"cancelled"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/approvals":
			if finalSynced {
				_, _ = w.Write([]byte(`{"items":[{"id":"approval-cancel-runner-1","app_id":"expense-approval","dataset_id":"finance.expense_forms","record_id":"expense-cancel-runner-1","status":"cancelled","summary":"Expense Cancelled","workflow_skill_id":"expense-workflow","workflow_version":"2.0.0","workflow_instance_id":"wf-cancel-runner-1","workflow_decision_id":"decision-cancel-runner-1","workflow_node_id":"expense.cancelled","workflow_node_ids":["expense.submit","manager.approval","expense.cancelled"],"business_status":"approval_cancelled","result_status":"cancelled","result_payload":{"approval_result":"cancelled","business_status":"approval_cancelled","business_record":{"id":"expense-cancel-runner-1","status":"approval_cancelled"},"text":"approval cancelled by requester"},"outputs":[{"type":"content","title":"Workflow Cancelled","text":"approval cancelled by requester","status":"cancelled"}],"request":{"approval_instance_id":"wf-cancel-runner-1","appID":"expense-approval","objectRole":"expense_report","approvalEvent":"finance.submitted","workflowSkillId":"expense-workflow","workflowVersion":"2.0.0","workflowNodeId":"expense.cancelled","workflowNodeIds":["expense.submit","manager.approval","expense.cancelled"],"businessStatus":"approval_cancelled","resultStatus":"cancelled"},"created_by":"alice","submitted_by":"alice","assigned_to":"manager"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			t.Fatalf("unexpected DataSrv request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	workflowJSON := `{"approval_instance":{"status":"cancelled","lane":"handled","workflow_instance_id":"wf-cancel-runner-1","approval_id":"approval-cancel-runner-1","record_id":"expense-cancel-runner-1","dataset_id":"finance.expense_forms","object_role":"expense_report","workflow_skill_id":"expense-workflow","workflow_version":"2.0.0","workflow_node_id":"expense.cancelled","workflow_node_ids":["expense.submit","manager.approval","expense.cancelled"],"workflow_decision_id":"decision-cancel-runner-1","business_status":"approval_cancelled","result_status":"cancelled","result":"approval cancelled by requester","result_payload":{"approval_result":"cancelled","business_status":"approval_cancelled","business_record":{"id":"expense-cancel-runner-1","status":"approval_cancelled"},"text":"approval cancelled by requester"},"outputs":[{"type":"content","title":"Workflow Cancelled","text":"approval cancelled by requester","status":"cancelled"}]}}`
	workflowResultPath := filepath.Join(app.testHomeDir, "workflow-cancelled-result.txt")
	if err := os.WriteFile(workflowResultPath, []byte("workflow_result="+workflowJSON+"\n"), 0o644); err != nil {
		t.Fatalf("write workflow cancelled result fixture: %v", err)
	}
	workflowCommand := `cat "` + workflowResultPath + `"`
	if os.PathSeparator == '\\' {
		workflowCommand = `type "` + workflowResultPath + `"`
	}
	if err := app.skillExecutor.Register(corelib.NLSkillEntry{
		Name:   "expense-workflow",
		Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action:  "bash",
			Params:  map[string]interface{}{"command": workflowCommand},
			Capture: map[string]string{"workflow_result": `workflow_result=(.+)`},
		}},
	}); err != nil {
		t.Fatalf("register workflow skill: %v", err)
	}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "alice", Role: "requester"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}
	if err := app.writeMaclawAppInstallRegistry(maclawAppInstallRegistry{
		Schema:    "maclaw.app.installs.v1",
		UpdatedAt: "2026-06-30T10:00:00Z",
		Installs: []maclawAppInstallRecord{{
			AppID:       "expense-approval",
			AppName:     "Expense Approval",
			Kind:        "enterprise_approval_app",
			InstalledAt: "2026-06-30T10:00:00Z",
			VersionSnapshot: maclawAppInstallVersionSnapshot{
				WorkflowSkills:   []maclawAppInstallSkillVersionSnapshot{{ID: "expense-workflow", Version: "2.0.0", Kind: "workflow_skill", Source: "hub"}},
				ApprovalBindings: []maclawAppInstallApprovalBindingSnapshot{{Event: "finance.submitted", ObjectRole: "expense_report", WorkflowSkillID: "expense-workflow", WorkflowVersion: "2.0.0"}},
			},
			WorkflowContract: map[string]any{"schema": "maclaw.app.workflow_contract.v1", "workflowSkillId": "expense-workflow", "workflowVersion": "2.0.0", "objectRole": "expense_report"},
			Package:          map[string]any{"apps": []any{map[string]any{"app": map[string]any{"binding": map[string]any{"workflow": map[string]any{"submitNode": "expense.submit", "approvalNode": "manager.approval", "resultNode": "expense.cancelled"}}}}}},
		}},
	}); err != nil {
		t.Fatalf("write install registry: %v", err)
	}

	started, err := app.StartMaclawAppApprovalWorkflow(MaclawAppApprovalWorkflowStartInput{
		AppID:            "expense-approval",
		DatasetID:        "finance.expense_forms",
		ObjectRole:       "expense_report",
		RecordID:         "expense-cancel-runner-1",
		Title:            "Expense Cancelled",
		Applicant:        "alice",
		Approver:         "manager",
		BusinessNote:     "submit cancelled workflow",
		BusinessPayload:  map[string]any{"amount": float64(310)},
		RunWorkflowSkill: true,
	})
	if err != nil {
		t.Fatalf("StartMaclawAppApprovalWorkflow() cancelled error = %v", err)
	}
	workflowRun, ok := started["workflow_run"].(map[string]any)
	if !ok || workflowRun["ran"] != true {
		t.Fatalf("expected cancelled workflow run evidence: %#v", started["workflow_run"])
	}
	instance, ok := workflowRun["instance"].(maclawAppApprovalInstance)
	if !ok || instance.Status != "cancelled" || instance.Lane != "handled" || instance.ResultStatus != "cancelled" || instance.CurrentNode != "expense.cancelled" || instance.WorkflowDecisionID != "decision-cancel-runner-1" {
		t.Fatalf("workflow cancelled result should become handled approval instance: %#v", workflowRun["instance"])
	}
	if instance.ResultPayload["approval_result"] != "cancelled" || instance.ResultPayload["business_status"] != "approval_cancelled" || len(instance.Outputs) != 1 || instance.Outputs[0].Status != "cancelled" {
		t.Fatalf("workflow cancelled result should preserve result package: %#v", instance)
	}
	reviewSynced := false
	for _, req := range captured {
		if req.Method == http.MethodPost && req.Path == "/api/v1/data/approvals/approval-cancel-runner-1/review" {
			reviewSynced = true
			if req.Body["decision"] != "cancelled" || req.Body["workflow_decision_id"] != "decision-cancel-runner-1" || req.Body["workflow_node_id"] != "expense.cancelled" || req.Body["business_status"] != "approval_cancelled" || req.Body["result_status"] != "cancelled" {
				t.Fatalf("cancelled review should carry workflow final fields: %#v", req.Body)
			}
			if outputs, ok := req.Body["outputs"].([]interface{}); !ok || len(outputs) != 1 {
				t.Fatalf("cancelled review should carry output block: %#v", req.Body)
			}
		}
	}
	if !reviewSynced {
		t.Fatalf("cancelled workflow result should review DataSrv approval, captured=%#v", captured)
	}
	handled, err := app.ListMaclawAppApprovalInstances("expense-approval", "handled", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances handled error = %v", err)
	}
	if len(handled) != 1 || handled[0].Status != "cancelled" || handled[0].ResultPayload["approval_result"] != "cancelled" || len(handled[0].Outputs) != 1 {
		t.Fatalf("workflow cancelled result should be visible in handled lane: %#v", handled)
	}
	globalHandled, err := app.ListMaclawAppApprovalInstancesAll("handled", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstancesAll handled error = %v", err)
	}
	if len(globalHandled) != 1 || globalHandled[0].Status != "cancelled" || globalHandled[0].ResultPayload["approval_result"] != "cancelled" || len(globalHandled[0].Outputs) != 1 {
		t.Fatalf("workflow cancelled result should be visible in global handled lane: %#v", globalHandled)
	}
	assertMaclawAppApprovalReadbackSameInstanceForTest(t, handled[0], globalHandled[0])
	sawAppScopedQuery := false
	sawGlobalQuery := false
	for _, req := range captured {
		if req.Method == http.MethodGet && req.Path == "/api/v1/data/approvals" {
			if req.RawQuery == "app_id=expense-approval&lane=handled&limit=10" {
				sawAppScopedQuery = true
			}
			if req.RawQuery == "lane=handled&limit=10" {
				sawGlobalQuery = true
			}
		}
	}
	if !sawAppScopedQuery || !sawGlobalQuery {
		t.Fatalf("cancelled approval readback should query both app-scoped and global handled lanes, appScoped=%v global=%v captured=%#v", sawAppScopedQuery, sawGlobalQuery, captured)
	}
}

func TestStartMaclawAppApprovalWorkflowRunsRequiresInputWorkflowResult(t *testing.T) {
	type capturedRequest struct {
		Method   string
		Path     string
		RawQuery string
		Body     map[string]interface{}
	}
	captured := []capturedRequest{}
	progressSynced := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := capturedRequest{Method: r.Method, Path: r.URL.Path, RawQuery: r.URL.RawQuery}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		captured = append(captured, item)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-input-runner-1":
			_, _ = w.Write([]byte(`{"id":"expense-input-runner-1","data":{"status":"draft","amount":280}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-input-runner-1":
			_, _ = w.Write([]byte(`{"id":"expense-input-runner-1","status":"requires_input"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-input-runner-1/approvals":
			_, _ = w.Write([]byte(`{"id":"approval-input-runner-1","status":"pending"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/approvals/approval-input-runner-1/progress":
			progressSynced = true
			_, _ = w.Write([]byte(`{"id":"approval-input-runner-1","status":"requires_input","progress":"missing invoice attachment"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/approvals/approval-input-runner-1/review":
			t.Fatalf("requires_input workflow result must not review DataSrv approval: %#v", item.Body)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/approvals":
			if progressSynced {
				_, _ = w.Write([]byte(`{"items":[{"id":"approval-input-runner-1","app_id":"expense-approval","dataset_id":"finance.expense_forms","record_id":"expense-input-runner-1","status":"requires_input","summary":"Expense Needs Input","lane":"my_requests","workflow_skill_id":"expense-workflow","workflow_version":"2.0.0","workflow_instance_id":"wf-input-runner-1","workflow_decision_id":"decision-input-runner-1","workflow_node_id":"expense.require_input","workflow_node_ids":["expense.submit","expense.require_input"],"business_status":"waiting_for_requester","result_status":"requires_input","result_payload":{"approval_result":"requires_input","business_status":"waiting_for_requester","requires_input":{"fields":["invoice_attachment"],"message":"missing invoice attachment"},"business_record":{"id":"expense-input-runner-1","status":"waiting_for_requester"},"text":"missing invoice attachment"},"outputs":[{"type":"content","kind":"requires_input","title":"Missing materials","text":"missing invoice attachment","status":"requires_input"}],"request":{"approval_instance_id":"wf-input-runner-1","appID":"expense-approval","objectRole":"expense_report","approvalEvent":"finance.submitted","workflowSkillId":"expense-workflow","workflowVersion":"2.0.0","workflowNodeId":"expense.require_input","workflowNodeIds":["expense.submit","expense.require_input"],"businessStatus":"waiting_for_requester","resultStatus":"requires_input"},"created_by":"alice","submitted_by":"alice","assigned_to":"alice"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			t.Fatalf("unexpected DataSrv request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	workflowJSON := `{"approval_instance":{"status":"requires_input","workflow_instance_id":"wf-input-runner-1","approval_id":"approval-input-runner-1","record_id":"expense-input-runner-1","dataset_id":"finance.expense_forms","object_role":"expense_report","workflow_skill_id":"expense-workflow","workflow_version":"2.0.0","workflow_node_id":"expense.require_input","workflow_node_ids":["expense.submit","expense.require_input"],"workflow_decision_id":"decision-input-runner-1","business_status":"waiting_for_requester","result_status":"requires_input","result":"missing invoice attachment","result_payload":{"approval_result":"requires_input","business_status":"waiting_for_requester","requires_input":{"fields":["invoice_attachment"],"message":"missing invoice attachment"},"business_record":{"id":"expense-input-runner-1","status":"waiting_for_requester"},"text":"missing invoice attachment"},"outputs":[{"type":"content","kind":"requires_input","title":"Missing materials","text":"missing invoice attachment","status":"requires_input"}]}}`
	workflowResultPath := filepath.Join(app.testHomeDir, "workflow-requires-input-result.txt")
	if err := os.WriteFile(workflowResultPath, []byte("workflow_result="+workflowJSON+"\n"), 0o644); err != nil {
		t.Fatalf("write workflow requires_input result fixture: %v", err)
	}
	workflowCommand := `cat "` + workflowResultPath + `"`
	if os.PathSeparator == '\\' {
		workflowCommand = `type "` + workflowResultPath + `"`
	}
	if err := app.skillExecutor.Register(corelib.NLSkillEntry{
		Name:   "expense-workflow",
		Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action:  "bash",
			Params:  map[string]interface{}{"command": workflowCommand},
			Capture: map[string]string{"workflow_result": `workflow_result=(.+)`},
		}},
	}); err != nil {
		t.Fatalf("register workflow skill: %v", err)
	}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "alice", Role: "requester"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}
	if err := app.writeMaclawAppInstallRegistry(maclawAppInstallRegistry{
		Schema:    "maclaw.app.installs.v1",
		UpdatedAt: "2026-06-30T10:00:00Z",
		Installs: []maclawAppInstallRecord{{
			AppID:       "expense-approval",
			AppName:     "Expense Approval",
			Kind:        "enterprise_approval_app",
			InstalledAt: "2026-06-30T10:00:00Z",
			VersionSnapshot: maclawAppInstallVersionSnapshot{
				WorkflowSkills:   []maclawAppInstallSkillVersionSnapshot{{ID: "expense-workflow", Version: "2.0.0", Kind: "workflow_skill", Source: "hub"}},
				ApprovalBindings: []maclawAppInstallApprovalBindingSnapshot{{Event: "finance.submitted", ObjectRole: "expense_report", WorkflowSkillID: "expense-workflow", WorkflowVersion: "2.0.0"}},
			},
			WorkflowContract: map[string]any{"schema": "maclaw.app.workflow_contract.v1", "workflowSkillId": "expense-workflow", "workflowVersion": "2.0.0", "objectRole": "expense_report"},
			Package:          map[string]any{"apps": []any{map[string]any{"app": map[string]any{"binding": map[string]any{"workflow": map[string]any{"submitNode": "expense.submit", "approvalNode": "manager.approval", "resultNode": "expense.require_input"}}}}}},
		}},
	}); err != nil {
		t.Fatalf("write install registry: %v", err)
	}

	started, err := app.StartMaclawAppApprovalWorkflow(MaclawAppApprovalWorkflowStartInput{
		AppID:            "expense-approval",
		DatasetID:        "finance.expense_forms",
		ObjectRole:       "expense_report",
		RecordID:         "expense-input-runner-1",
		Title:            "Expense Needs Input",
		Applicant:        "alice",
		Approver:         "manager",
		BusinessNote:     "submit requires input workflow",
		BusinessPayload:  map[string]any{"amount": float64(280)},
		RunWorkflowSkill: true,
	})
	if err != nil {
		t.Fatalf("StartMaclawAppApprovalWorkflow() requires_input error = %v", err)
	}
	workflowRun, ok := started["workflow_run"].(map[string]any)
	if !ok || workflowRun["ran"] != true {
		t.Fatalf("expected requires_input workflow run evidence: %#v", started["workflow_run"])
	}
	instance, ok := workflowRun["instance"].(maclawAppApprovalInstance)
	if !ok || instance.Status != "requires_input" || instance.Lane != "my_requests" || instance.ResultStatus != "requires_input" || instance.CurrentNode != "expense.require_input" || instance.WorkflowDecisionID != "decision-input-runner-1" {
		t.Fatalf("workflow requires_input result should become requester-side approval instance: %#v", workflowRun["instance"])
	}
	if instance.ResultPayload["approval_result"] != "requires_input" || instance.ResultPayload["business_status"] != "waiting_for_requester" || len(instance.Outputs) != 1 || instance.Outputs[0].Kind != "requires_input" {
		t.Fatalf("workflow requires_input result should preserve result package: %#v", instance)
	}
	progressRequest := false
	for _, req := range captured {
		if req.Method == http.MethodPost && req.Path == "/api/v1/data/approvals/approval-input-runner-1/progress" {
			progressRequest = true
			if req.Body["workflow_decision_id"] != "decision-input-runner-1" || req.Body["workflow_node_id"] != "expense.require_input" || req.Body["business_status"] != "waiting_for_requester" || req.Body["result_status"] != "requires_input" {
				t.Fatalf("requires_input progress should carry requester input fields: %#v", req.Body)
			}
			if outputs, ok := req.Body["outputs"].([]interface{}); !ok || len(outputs) != 1 {
				t.Fatalf("requires_input progress should carry output block: %#v", req.Body)
			}
		}
	}
	if !progressRequest {
		t.Fatalf("requires_input workflow result should update DataSrv approval progress, captured=%#v", captured)
	}
	requests, err := app.ListMaclawAppApprovalInstances("expense-approval", "my_requests", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances my_requests error = %v", err)
	}
	if len(requests) != 1 || requests[0].Status != "requires_input" || requests[0].Lane != "my_requests" || requests[0].ResultPayload["approval_result"] != "requires_input" || len(requests[0].Outputs) != 1 {
		t.Fatalf("workflow requires_input result should be visible in requester lane: %#v", requests)
	}
	globalRequests, err := app.ListMaclawAppApprovalInstancesAll("my_requests", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstancesAll my_requests error = %v", err)
	}
	if len(globalRequests) != 1 || globalRequests[0].Status != "requires_input" || globalRequests[0].Lane != "my_requests" || globalRequests[0].ResultPayload["approval_result"] != "requires_input" || len(globalRequests[0].Outputs) != 1 {
		t.Fatalf("workflow requires_input result should be visible in global requester lane: %#v", globalRequests)
	}
	assertMaclawAppApprovalReadbackSameInstanceForTest(t, requests[0], globalRequests[0])
	handled, err := app.ListMaclawAppApprovalInstances("expense-approval", "handled", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances handled error = %v", err)
	}
	if len(handled) != 0 {
		t.Fatalf("requires_input should not appear in handled lane: %#v", handled)
	}
	sawAppScopedRequestQuery := false
	sawGlobalRequestQuery := false
	for _, req := range captured {
		if req.Method == http.MethodGet && req.Path == "/api/v1/data/approvals" {
			if req.RawQuery == "app_id=expense-approval&lane=my_requests&limit=10" {
				sawAppScopedRequestQuery = true
			}
			if req.RawQuery == "lane=my_requests&limit=10" {
				sawGlobalRequestQuery = true
			}
		}
	}
	if !sawAppScopedRequestQuery || !sawGlobalRequestQuery {
		t.Fatalf("requires_input approval readback should query both app-scoped and global requester lanes, appScoped=%v global=%v captured=%#v", sawAppScopedRequestQuery, sawGlobalRequestQuery, captured)
	}
}

func TestStartMaclawAppApprovalWorkflowContinuesRequiresInputWithSupplement(t *testing.T) {
	type capturedRequest struct {
		Method   string
		Path     string
		RawQuery string
		Body     map[string]interface{}
	}
	captured := []capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := capturedRequest{Method: r.Method, Path: r.URL.Path, RawQuery: r.URL.RawQuery}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		captured = append(captured, item)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-input-runner-2":
			_, _ = w.Write([]byte(`{"id":"expense-input-runner-2","data":{"status":"waiting_for_requester","amount":280}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-input-runner-2":
			_, _ = w.Write([]byte(`{"id":"expense-input-runner-2","status":"updated"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/approvals/approval-input-runner-2/progress":
			_, _ = w.Write([]byte(`{"id":"approval-input-runner-2","status":"pending","progress":"supplemental input submitted"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/approvals/approval-input-runner-2/review":
			_, _ = w.Write([]byte(`{"id":"approval-input-runner-2","status":"approved"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-input-runner-2/approvals":
			t.Fatalf("continuing requires_input must reuse existing DataSrv approval, not create a new one: %#v", item.Body)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/approvals":
			_, _ = w.Write([]byte(`{"items":[{"id":"approval-input-runner-2","app_id":"expense-approval","dataset_id":"finance.expense_forms","record_id":"expense-input-runner-2","status":"approved","summary":"Expense Approved After Supplement","lane":"handled","workflow_skill_id":"expense-workflow","workflow_version":"2.0.0","workflow_instance_id":"wf-input-runner-2","workflow_decision_id":"decision-input-runner-2","workflow_node_id":"expense.result","workflow_node_ids":["expense.require_input","manager.approval","expense.result"],"business_status":"finance_approved","result_status":"approved","result_payload":{"approval_result":"approved","business_status":"finance_approved","business_record":{"id":"expense-input-runner-2","status":"finance_approved"},"text":"approved after supplemental input"},"outputs":[{"type":"content","title":"Workflow Decision","text":"approved after supplemental input","status":"approved"}],"request":{"approval_instance_id":"wf-input-runner-2","workflowSkillId":"expense-workflow","workflowVersion":"2.0.0","workflowNodeId":"expense.result"},"created_by":"alice","submitted_by":"alice","reviewed_by":"manager"}]}`))
		default:
			t.Fatalf("unexpected DataSrv request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	workflowJSON := `{"approval_instance":{"status":"approved","lane":"handled","workflow_instance_id":"wf-input-runner-2","approval_id":"approval-input-runner-2","record_id":"expense-input-runner-2","dataset_id":"finance.expense_forms","object_role":"expense_report","workflow_skill_id":"expense-workflow","workflow_version":"2.0.0","workflow_node_id":"expense.result","workflow_node_ids":["expense.require_input","manager.approval","expense.result"],"workflow_decision_id":"decision-input-runner-2","business_status":"finance_approved","result_status":"approved","result":"approved after supplemental input","result_payload":{"approval_result":"approved","business_status":"finance_approved","business_record":{"id":"expense-input-runner-2","status":"finance_approved"},"text":"approved after supplemental input"},"outputs":[{"type":"content","title":"Workflow Decision","text":"approved after supplemental input","status":"approved"}]}}`
	workflowResultPath := filepath.Join(app.testHomeDir, "workflow-supplement-approved-result.txt")
	if err := os.WriteFile(workflowResultPath, []byte("workflow_result="+workflowJSON+"\n"), 0o644); err != nil {
		t.Fatalf("write workflow supplement result fixture: %v", err)
	}
	workflowCommand := `cat "` + workflowResultPath + `"`
	if os.PathSeparator == '\\' {
		workflowCommand = `type "` + workflowResultPath + `"`
	}
	if err := app.skillExecutor.Register(corelib.NLSkillEntry{
		Name:   "expense-workflow",
		Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action:  "bash",
			Params:  map[string]interface{}{"command": workflowCommand},
			Capture: map[string]string{"workflow_result": `workflow_result=(.+)`},
		}},
	}); err != nil {
		t.Fatalf("register workflow skill: %v", err)
	}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "alice", Role: "requester"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}
	if err := app.writeMaclawAppInstallRegistry(maclawAppInstallRegistry{
		Schema:    "maclaw.app.installs.v1",
		UpdatedAt: "2026-06-30T10:00:00Z",
		Installs: []maclawAppInstallRecord{{
			AppID:       "expense-approval",
			AppName:     "Expense Approval",
			Kind:        "enterprise_approval_app",
			InstalledAt: "2026-06-30T10:00:00Z",
			VersionSnapshot: maclawAppInstallVersionSnapshot{
				WorkflowSkills:   []maclawAppInstallSkillVersionSnapshot{{ID: "expense-workflow", Version: "2.0.0", Kind: "workflow_skill", Source: "hub"}},
				ApprovalBindings: []maclawAppInstallApprovalBindingSnapshot{{Event: "finance.submitted", ObjectRole: "expense_report", WorkflowSkillID: "expense-workflow", WorkflowVersion: "2.0.0"}},
			},
			WorkflowContract: map[string]any{"schema": "maclaw.app.workflow_contract.v1", "workflowSkillId": "expense-workflow", "workflowVersion": "2.0.0", "objectRole": "expense_report"},
			Package:          map[string]any{"apps": []any{map[string]any{"app": map[string]any{"binding": map[string]any{"workflow": map[string]any{"submitNode": "expense.submit", "approvalNode": "manager.approval", "resultNode": "expense.result"}}}}}},
		}},
	}); err != nil {
		t.Fatalf("write install registry: %v", err)
	}
	if _, err := app.RecordMaclawAppApprovalInstance(maclawAppApprovalInstance{
		AppID:              "expense-approval",
		AppName:            "Expense Approval",
		InstanceID:         "wf-input-runner-2",
		ApprovalID:         "approval-input-runner-2",
		RecordApprovalID:   "approval-input-runner-2",
		DatasetID:          "finance.expense_forms",
		ObjectRole:         "expense_report",
		ApprovalObjectRole: "expense_report",
		RecordID:           "expense-input-runner-2",
		Title:              "Expense Needs Input",
		Lane:               "my_requests",
		Status:             "requires_input",
		CurrentNode:        "expense.require_input",
		CurrentNodeIDs:     []string{"expense.submit", "expense.require_input"},
		WorkflowNodeIDs:    []string{"expense.submit", "expense.require_input"},
		WorkflowSkillID:    "expense-workflow",
		WorkflowVersion:    "2.0.0",
		BusinessStatus:     "waiting_for_requester",
		ResultStatus:       "requires_input",
		Applicant:          "alice",
		Approver:           "manager",
		CurrentAssignee:    "alice",
		Result:             "missing invoice attachment",
		ResultPayload:      map[string]any{"approval_result": "requires_input", "business_status": "waiting_for_requester", "requires_input": map[string]any{"fields": []any{"invoice_attachment"}}},
	}); err != nil {
		t.Fatalf("seed requires_input instance: %v", err)
	}

	started, err := app.StartMaclawAppApprovalWorkflow(MaclawAppApprovalWorkflowStartInput{
		AppID:            "expense-approval",
		ApprovalID:       "approval-input-runner-2",
		ContinueFromID:   "wf-input-runner-2",
		DatasetID:        "finance.expense_forms",
		ObjectRole:       "expense_report",
		RecordID:         "expense-input-runner-2",
		Title:            "Expense Approved After Supplement",
		Applicant:        "alice",
		Approver:         "manager",
		CurrentAssignee:  "manager",
		BusinessNote:     "supplemental input submitted",
		FormData:         map[string]any{"invoice_attachment": "artifact://invoice/new.pdf"},
		BusinessPayload:  map[string]any{"amount": float64(280), "invoice_attachment": "artifact://invoice/new.pdf"},
		RunWorkflowSkill: true,
	})
	if err != nil {
		t.Fatalf("StartMaclawAppApprovalWorkflow() continue requires_input error = %v", err)
	}
	if started["approval_id"] != "approval-input-runner-2" {
		t.Fatalf("continued workflow should preserve approval id: %#v", started)
	}
	workflowRun, ok := started["workflow_run"].(map[string]any)
	if !ok || workflowRun["ran"] != true {
		t.Fatalf("expected continued workflow run evidence: %#v", started["workflow_run"])
	}
	instance, ok := workflowRun["instance"].(maclawAppApprovalInstance)
	if !ok || instance.InstanceID != "wf-input-runner-2" || instance.ApprovalID != "approval-input-runner-2" || instance.Status != "approved" || instance.Lane != "handled" {
		t.Fatalf("continued workflow should finish same approval instance: %#v", workflowRun["instance"])
	}
	progressCount := 0
	reviewCount := 0
	createCount := 0
	for _, req := range captured {
		switch {
		case req.Method == http.MethodPost && req.Path == "/api/v1/data/approvals/approval-input-runner-2/progress":
			progressCount++
			if req.Body["workflow_instance_id"] != "wf-input-runner-2" || req.Body["business_status"] != "supplemented" || req.Body["result_status"] != "pending" {
				t.Fatalf("supplement progress should preserve instance and supplemental state: %#v", req.Body)
			}
			payload, ok := req.Body["result_payload"].(map[string]interface{})
			if !ok || payload["supplemental_input"] == nil {
				t.Fatalf("supplement progress should carry supplemental input payload: %#v", req.Body)
			}
		case req.Method == http.MethodPost && req.Path == "/api/v1/data/approvals/approval-input-runner-2/review":
			reviewCount++
			if req.Body["decision"] != "approved" || req.Body["workflow_instance_id"] != "wf-input-runner-2" || req.Body["workflow_decision_id"] != "decision-input-runner-2" {
				t.Fatalf("final review should preserve continued instance context: %#v", req.Body)
			}
		case req.Method == http.MethodPost && req.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-input-runner-2/approvals":
			createCount++
		}
	}
	if progressCount != 1 || reviewCount != 1 || createCount != 0 {
		t.Fatalf("continued workflow should progress then review existing approval only, progress=%d review=%d create=%d captured=%#v", progressCount, reviewCount, createCount, captured)
	}
	handled, err := app.ListMaclawAppApprovalInstances("expense-approval", "handled", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances handled error = %v", err)
	}
	if len(handled) != 1 || handled[0].ApprovalID != "approval-input-runner-2" || handled[0].Status != "approved" || handled[0].ResultPayload["approval_result"] != "approved" {
		t.Fatalf("continued approved result should be visible in handled lane: %#v", handled)
	}
	globalHandled, err := app.ListMaclawAppApprovalInstancesAll("handled", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstancesAll handled error = %v", err)
	}
	if len(globalHandled) != 1 || globalHandled[0].ApprovalID != "approval-input-runner-2" || globalHandled[0].Status != "approved" || globalHandled[0].ResultPayload["approval_result"] != "approved" {
		t.Fatalf("continued approved result should be visible in global handled lane: %#v", globalHandled)
	}
	assertMaclawAppApprovalReadbackSameInstanceForTest(t, handled[0], globalHandled[0])
	requests, err := app.ListMaclawAppApprovalInstances("expense-approval", "my_requests", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances my_requests after supplement error = %v", err)
	}
	if len(requests) != 0 {
		t.Fatalf("continued approved result should leave requester input lane: %#v", requests)
	}
	sawAppScopedHandledQuery := false
	sawGlobalHandledQuery := false
	for _, req := range captured {
		if req.Method == http.MethodGet && req.Path == "/api/v1/data/approvals" {
			if req.RawQuery == "app_id=expense-approval&lane=handled&limit=10" {
				sawAppScopedHandledQuery = true
			}
			if req.RawQuery == "lane=handled&limit=10" {
				sawGlobalHandledQuery = true
			}
		}
	}
	if !sawAppScopedHandledQuery || !sawGlobalHandledQuery {
		t.Fatalf("continued approval readback should query both app-scoped and global handled lanes, appScoped=%v global=%v captured=%#v", sawAppScopedHandledQuery, sawGlobalHandledQuery, captured)
	}
}

func TestStartMaclawAppApprovalWorkflowRecordsFailedWorkflowSkillResult(t *testing.T) {
	type capturedRequest struct {
		Method   string
		Path     string
		RawQuery string
		Body     map[string]interface{}
	}
	captured := []capturedRequest{}
	failedReviewed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := capturedRequest{Method: r.Method, Path: r.URL.Path, RawQuery: r.URL.RawQuery}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		captured = append(captured, item)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-fail-1":
			_, _ = w.Write([]byte(`{"id":"expense-fail-1","data":{"status":"draft","amount":900}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-fail-1":
			_, _ = w.Write([]byte(`{"id":"expense-fail-1","status":"updated"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-fail-1/approvals":
			_, _ = w.Write([]byte(`{"id":"approval-fail-1","status":"pending"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/approvals/approval-fail-1/review":
			failedReviewed = true
			_, _ = w.Write([]byte(`{"id":"approval-fail-1","status":"failed"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/approvals":
			if failedReviewed {
				_, _ = w.Write([]byte(`{"items":[{"id":"approval-fail-1","app_id":"expense-approval","dataset_id":"finance.expense_forms","record_id":"expense-fail-1","status":"failed","workflow_skill_id":"expense-workflow","workflow_version":"2.0.0","workflow_instance_id":"placeholder","workflow_node_id":"workflow.failed","workflow_node_ids":["manager.approval","workflow.failed"],"business_status":"workflow_failed","result_status":"failed","result_payload":{"approval_result":"failed","business_status":"workflow_failed","result_status":"failed","error":"exit status 7","text":"workflow failed"},"outputs":[{"kind":"approval_result","type":"approval_result","title":"Workflow failed","text":"workflow failed","status":"failed"}],"request":{"approval_instance_id":"placeholder","workflowSkillId":"expense-workflow","workflowVersion":"2.0.0","workflowNodeId":"workflow.failed","businessStatus":"workflow_failed","resultStatus":"failed"},"created_by":"alice","submitted_by":"alice","assigned_to":"manager"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"items":[{"id":"approval-fail-1","app_id":"expense-approval","dataset_id":"finance.expense_forms","record_id":"expense-fail-1","status":"pending","workflow_skill_id":"expense-workflow","workflow_version":"2.0.0","workflow_instance_id":"placeholder"}]}`))
		default:
			t.Fatalf("unexpected DataSrv request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	failCommand := `exit 7`
	if os.PathSeparator == '\\' {
		failCommand = `cmd /c exit 7`
	}
	if err := app.skillExecutor.Register(corelib.NLSkillEntry{
		Name:   "expense-workflow",
		Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": failCommand},
		}},
	}); err != nil {
		t.Fatalf("register workflow skill: %v", err)
	}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "alice", Role: "requester"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}
	if err := app.writeMaclawAppInstallRegistry(maclawAppInstallRegistry{
		Schema:    "maclaw.app.installs.v1",
		UpdatedAt: "2026-06-30T10:00:00Z",
		Installs: []maclawAppInstallRecord{{
			AppID:       "expense-approval",
			AppName:     "Expense Approval",
			Kind:        "enterprise_approval_app",
			InstalledAt: "2026-06-30T10:00:00Z",
			VersionSnapshot: maclawAppInstallVersionSnapshot{
				WorkflowSkills:   []maclawAppInstallSkillVersionSnapshot{{ID: "expense-workflow", Version: "2.0.0", Kind: "workflow_skill", Source: "hub"}},
				ApprovalBindings: []maclawAppInstallApprovalBindingSnapshot{{Event: "finance.submitted", ObjectRole: "expense_report", WorkflowSkillID: "expense-workflow", WorkflowVersion: "2.0.0"}},
			},
			WorkflowContract: map[string]any{"schema": "maclaw.app.workflow_contract.v1", "workflowSkillId": "expense-workflow", "workflowVersion": "2.0.0", "objectRole": "expense_report"},
			Package:          map[string]any{"apps": []any{map[string]any{"app": map[string]any{"binding": map[string]any{"workflow": map[string]any{"submitNode": "expense.submit", "approvalNode": "manager.approval", "resultNode": "expense.result"}}}}}},
		}},
	}); err != nil {
		t.Fatalf("write install registry: %v", err)
	}

	started, err := app.StartMaclawAppApprovalWorkflow(MaclawAppApprovalWorkflowStartInput{
		AppID:            "expense-approval",
		DatasetID:        "finance.expense_forms",
		ObjectRole:       "expense_report",
		RecordID:         "expense-fail-1",
		Title:            "Expense Failure",
		Applicant:        "alice",
		Approver:         "manager",
		BusinessNote:     "submit failing workflow",
		BusinessPayload:  map[string]any{"amount": float64(900)},
		RunWorkflowSkill: true,
	})
	if err != nil {
		t.Fatalf("StartMaclawAppApprovalWorkflow() should return failed workflow result, not error: %v", err)
	}
	workflowRun, ok := started["workflow_run"].(map[string]any)
	if !ok || workflowRun["ran"] != false || workflowRun["error"] == "" {
		t.Fatalf("expected failed workflow run evidence: %#v", started["workflow_run"])
	}
	instance, ok := workflowRun["instance"].(maclawAppApprovalInstance)
	if !ok || instance.Status != "failed" || instance.Lane != "handled" || instance.ResultStatus != "failed" || instance.BusinessStatus != "workflow_failed" {
		t.Fatalf("failed workflow should become handled approval instance: %#v", workflowRun["instance"])
	}
	if instance.ApprovalID != "approval-fail-1" || instance.CurrentNode != "workflow.failed" || instance.ResultPayload["approval_result"] != "failed" || instance.ResultPayload["business_status"] != "workflow_failed" || len(instance.Outputs) != 1 || instance.Outputs[0].Status != "failed" {
		t.Fatalf("failed workflow should preserve result payload and output: %#v", instance)
	}
	reviewSynced := false
	for _, req := range captured {
		if req.Method == http.MethodPost && req.Path == "/api/v1/data/approvals/approval-fail-1/review" {
			reviewSynced = true
			if req.Body["decision"] != "failed" || req.Body["workflow_node_id"] != "workflow.failed" || req.Body["business_status"] != "workflow_failed" || req.Body["result_status"] != "failed" {
				t.Fatalf("failed workflow review should carry failure result package: %#v", req.Body)
			}
			if outputs, ok := req.Body["outputs"].([]interface{}); !ok || len(outputs) != 1 {
				t.Fatalf("failed workflow review should carry output block: %#v", req.Body)
			}
		}
	}
	if !reviewSynced {
		t.Fatalf("failed workflow result should review DataSrv approval, captured=%#v", captured)
	}
	handled, err := app.ListMaclawAppApprovalInstances("expense-approval", "handled", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances handled error = %v", err)
	}
	if len(handled) != 1 || handled[0].Status != "failed" || handled[0].ResultPayload["approval_result"] != "failed" {
		t.Fatalf("failed workflow should be visible in handled lane: %#v", handled)
	}
	globalHandled, err := app.ListMaclawAppApprovalInstancesAll("handled", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstancesAll handled error = %v", err)
	}
	if len(globalHandled) != 1 || globalHandled[0].Status != "failed" || globalHandled[0].ResultPayload["approval_result"] != "failed" || len(globalHandled[0].Outputs) != 1 {
		t.Fatalf("failed workflow should be visible in global handled lane: %#v", globalHandled)
	}
	assertMaclawAppApprovalReadbackSameInstanceForTest(t, handled[0], globalHandled[0])
	if handled[0].ResultPayload["error"] == "" || globalHandled[0].ResultPayload["error"] == "" {
		t.Fatalf("failed workflow readback should preserve error evidence, app=%#v global=%#v", handled[0].ResultPayload, globalHandled[0].ResultPayload)
	}
	sawAppScopedQuery := false
	sawGlobalQuery := false
	for _, req := range captured {
		if req.Method == http.MethodGet && req.Path == "/api/v1/data/approvals" {
			if req.RawQuery == "app_id=expense-approval&lane=handled&limit=10" {
				sawAppScopedQuery = true
			}
			if req.RawQuery == "lane=handled&limit=10" {
				sawGlobalQuery = true
			}
		}
	}
	if !sawAppScopedQuery || !sawGlobalQuery {
		t.Fatalf("failed approval readback should query both app-scoped and global handled lanes, appScoped=%v global=%v captured=%#v", sawAppScopedQuery, sawGlobalQuery, captured)
	}
}

func TestStartMaclawAppApprovalWorkflowCreatesDataSrvApproval(t *testing.T) {
	type capturedRequest struct {
		Method string
		Path   string
		Body   map[string]interface{}
	}
	captured := []capturedRequest{}
	remoteFinal := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := capturedRequest{Method: r.Method, Path: r.URL.Path}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		captured = append(captured, item)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-start-1":
			_, _ = w.Write([]byte(`{"id":"expense-start-1","data":{"status":"draft"}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-start-1":
			_, _ = w.Write([]byte(`{"id":"expense-start-1","status":"updated"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-start-1/approvals":
			_, _ = w.Write([]byte(`{"id":"approval-start-1","status":"pending"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/approvals":
			if remoteFinal {
				_, _ = w.Write([]byte(`{"items":[{"id":"approval-start-1","app_id":"expense-approval","dataset_id":"finance.expense_forms","record_id":"expense-start-1","status":"approved","summary":"Expense Start","workflow_skill_id":"expense-workflow","workflow_version":"2.0.0","workflow_instance_id":"wf-start-1","workflow_node_id":"expense.result_pack","workflow_node_ids":["expense.intake","finance.manager_review","expense.result_pack"],"business_status":"finance_approved","result_status":"approved","result_payload":{"approval_result":"approved","business_status":"finance_approved","business_record":{"id":"expense-start-1","status":"finance_approved"},"text":"manager approved"},"outputs":[{"type":"content","title":"Approval Decision","text":"manager approved"},{"type":"artifact","title":"Approval File","artifact":{"id":"approval-start-pdf","name":"approval-start.pdf","uri":"artifact://approval/start.pdf","status":"ready"}}],"artifacts":[{"id":"approval-start-pdf","name":"approval-start.pdf","uri":"artifact://approval/start.pdf","status":"ready"}],"request":{"approval_instance_id":"wf-start-1","appID":"expense-approval","blueprintID":"expense.blueprint.v1","objectRole":"expense_report","approvalEvent":"finance.submitted","workflowSkillId":"expense-workflow","workflowVersion":"2.0.0","workflowNodeId":"expense.result_pack","workflowNodeIds":["expense.intake","finance.manager_review","expense.result_pack"],"businessStatus":"finance_approved","resultStatus":"approved"},"created_by":"alice","submitted_by":"alice","reviewed_by":"manager","assigned_to":"manager","created_at":"2026-06-30T01:00:00Z","updated_at":"2026-06-30T01:02:00Z"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "alice", Role: "requester"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}
	if err := app.writeMaclawAppInstallRegistry(maclawAppInstallRegistry{
		Schema:    "maclaw.app.installs.v1",
		UpdatedAt: "2026-06-30T01:00:00Z",
		Installs: []maclawAppInstallRecord{{
			AppID:       "expense-approval",
			AppName:     "Expense Approval",
			Kind:        "enterprise_approval_app",
			InstalledAt: "2026-06-30T01:00:00Z",
			VersionSnapshot: maclawAppInstallVersionSnapshot{
				WorkflowSkills:   []maclawAppInstallSkillVersionSnapshot{{ID: "expense-workflow", Version: "2.0.0", Kind: "workflow_skill", Source: "hub"}},
				ApprovalBindings: []maclawAppInstallApprovalBindingSnapshot{{Event: "finance.submitted", DatasetID: "finance.expense_forms", BlueprintID: "expense.blueprint.v1", ObjectRole: "expense_report", WorkflowSkillID: "expense-workflow", WorkflowVersion: "2.0.0"}},
			},
			WorkflowContract: map[string]any{"schema": "maclaw.app.workflow_contract.v1", "workflowSkillId": "expense-workflow", "workflowVersion": "2.0.0", "objectRole": "expense_report"},
			Package:          map[string]any{"apps": []any{map[string]any{"app": map[string]any{"binding": map[string]any{"workflow": map[string]any{"submitNode": "expense.intake", "approvalNode": "finance.manager_review"}}}}}},
		}},
	}); err != nil {
		t.Fatalf("write install registry: %v", err)
	}

	started, err := app.StartMaclawAppApprovalWorkflow(MaclawAppApprovalWorkflowStartInput{
		AppID:           "expense-approval",
		RecordID:        "expense-start-1",
		Title:           "Expense Start",
		Applicant:       "alice",
		Approver:        "manager",
		BusinessNote:    "submit expense for manager review",
		BusinessPayload: map[string]any{"amount": float64(860), "currency": "CNY"},
		FormData:        map[string]any{"reason": "travel"},
	})
	if err != nil {
		t.Fatalf("StartMaclawAppApprovalWorkflow() error = %v", err)
	}
	if started["started"] != true || started["approval_id"] != "approval-start-1" || started["workflow_skill_id"] != "expense-workflow" || started["workflow_version"] != "2.0.0" {
		t.Fatalf("unexpected start result: %#v", started)
	}
	create := captured[len(captured)-1]
	if create.Method != http.MethodPost || create.Path != "/api/v1/data/datasets/finance.expense_forms/records/expense-start-1/approvals" {
		t.Fatalf("workflow start should create DataSrv approval, captured=%#v", captured)
	}
	if create.Body["app_id"] != "expense-approval" || create.Body["blueprint_id"] != "expense.blueprint.v1" || create.Body["workflow_skill_id"] != "expense-workflow" || create.Body["workflow_version"] != "2.0.0" || create.Body["workflow_node_id"] != "finance.manager_review" {
		t.Fatalf("create approval should carry app workflow context: %#v", create.Body)
	}
	if nodes, ok := create.Body["workflow_node_ids"].([]interface{}); !ok || len(nodes) != 1 || nodes[0] != "finance.manager_review" {
		t.Fatalf("create approval should carry workflow node ids: %#v", create.Body)
	}
	request, ok := create.Body["request"].(map[string]interface{})
	if !ok || request["approval_instance_id"] == "" || request["trigger_event"] != "finance.submitted" || request["object_role"] != "expense_report" {
		t.Fatalf("create approval request should carry workflow submission payload: %#v", create.Body)
	}
	payload, ok := create.Body["result_payload"].(map[string]interface{})
	if !ok || payload["business_record"] == nil || payload["business_payload"] == nil || payload["form_data"] == nil {
		t.Fatalf("create approval should carry form/business payload result context: %#v", create.Body)
	}
	instances, err := app.ListMaclawAppApprovalInstances("expense-approval", "all", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances() error = %v", err)
	}
	if len(instances) != 1 || instances[0].ApprovalID != "approval-start-1" || instances[0].CurrentNode != "finance.manager_review" || instances[0].WorkflowSkillID != "expense-workflow" || instances[0].DatasetID != "finance.expense_forms" || instances[0].ObjectRole != "expense_report" || instances[0].BlueprintID != "expense.blueprint.v1" {
		t.Fatalf("workflow start should persist local approval instance with DataSrv id: %#v", instances)
	}
	if len(instances[0].WorkflowNodeIDs) != 1 || instances[0].WorkflowNodeIDs[0] != "finance.manager_review" {
		t.Fatalf("workflow start should persist local workflow node ids: %#v", instances[0])
	}
	remoteFinal = true
	handled, err := app.ListMaclawAppApprovalInstancesAll("handled", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstancesAll handled error = %v", err)
	}
	if len(handled) != 1 {
		t.Fatalf("expected final approval from DataSrv after workflow start, got %#v", handled)
	}
	final := handled[0]
	if final.AppID != "expense-approval" || final.ApprovalID != "approval-start-1" || final.InstanceID != "wf-start-1" || final.Status != "approved" || final.Lane != "handled" {
		t.Fatalf("final DataSrv approval should preserve app and workflow identity: %#v", final)
	}
	if final.CurrentNode != "expense.result_pack" || len(final.CurrentNodeIDs) != 3 || final.WorkflowSkillID != "expense-workflow" || final.WorkflowVersion != "2.0.0" {
		t.Fatalf("final DataSrv approval should preserve workflow node path: %#v", final)
	}
	if len(final.WorkflowNodeIDs) != 3 || final.WorkflowNodeIDs[0] != "expense.intake" || final.WorkflowNodeIDs[2] != "expense.result_pack" {
		t.Fatalf("final DataSrv approval should preserve workflow_node_ids alias: %#v", final.WorkflowNodeIDs)
	}
	if final.ResultPayload["approval_result"] != "approved" || final.ResultPayload["business_status"] != "finance_approved" || len(final.Outputs) != 2 || final.Outputs[1].Title != "Approval File" || len(final.Artifacts) != 1 || final.Artifacts[0].Name != "approval-start.pdf" {
		t.Fatalf("final DataSrv approval should expose result content and file package: %#v", final)
	}
}

func TestMaclawAppApprovalRuntimeContractUsesInstallSnapshot(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.writeMaclawAppInstallRegistry(maclawAppInstallRegistry{
		Schema:    "maclaw.app.installs.v1",
		UpdatedAt: "2026-06-23T01:00:00Z",
		Installs: []maclawAppInstallRecord{{
			AppID:       "expense-approval",
			AppName:     "Expense Approval",
			Kind:        "enterprise_approval_app",
			InstalledAt: "2026-06-23T01:00:00Z",
			VersionSnapshot: maclawAppInstallVersionSnapshot{
				WorkflowSkills:   []maclawAppInstallSkillVersionSnapshot{{ID: "expense-workflow", Version: "2.0.0", Kind: "workflow_skill", Source: "hub"}},
				ApprovalBindings: []maclawAppInstallApprovalBindingSnapshot{{Event: "finance.submitted", ObjectRole: "expense_report", WorkflowSkillID: "expense-workflow", WorkflowVersion: "2.0.0"}},
			},
			WorkflowContract: map[string]any{
				"schema":          "maclaw.app.workflow_contract.v1",
				"workflowSkillId": "expense-workflow",
				"workflowVersion": "2.0.0",
				"objectRole":      "expense_report",
			},
		}},
	}); err != nil {
		t.Fatalf("write install registry: %v", err)
	}

	created, err := app.RecordMaclawAppApprovalInstance(maclawAppApprovalInstance{AppID: "expense-approval", InstanceID: "appr-runtime-1", Title: "Expense #1", Status: "pending", CurrentNode: "submit", Owner: "alice"})
	if err != nil {
		t.Fatalf("RecordMaclawAppApprovalInstance runtime contract error = %v", err)
	}
	if created.WorkflowSkillID != "expense-workflow" || created.WorkflowVersion != "2.0.0" || created.ObjectRole != "expense_report" || created.ApprovalObjectRole != "expense_report" || created.ApprovalEvent != "finance.submitted" {
		t.Fatalf("runtime contract should fill installed workflow context: %#v", created)
	}

	_, err = app.RecordMaclawAppApprovalInstance(maclawAppApprovalInstance{AppID: "expense-approval", InstanceID: "appr-runtime-2", Title: "Expense #2", Status: "pending", WorkflowSkillID: "expense-workflow", WorkflowVersion: "9.9.9", ObjectRole: "expense_report"})
	if err == nil || !strings.Contains(err.Error(), "workflow_version") {
		t.Fatalf("expected workflow version drift error, got %v", err)
	}
	_, err = app.SyncMaclawAppApprovalInstanceToDataSrv(maclawAppApprovalDataSrvSyncInput{DatasetID: "finance.expenses", ObjectRole: "customer", RecordID: "exp-3", Instance: maclawAppApprovalInstance{AppID: "expense-approval", InstanceID: "appr-runtime-3", Title: "Expense #3", Status: "pending", WorkflowSkillID: "expense-workflow", WorkflowVersion: "2.0.0", ObjectRole: "customer"}})
	if err == nil || !strings.Contains(err.Error(), "object_role") {
		t.Fatalf("expected object role drift error, got %v", err)
	}
}

func TestListMaclawAppApprovalInstancesAll(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	empty, err := app.ListMaclawAppApprovalInstancesAll("all", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstancesAll empty error = %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected empty global approval list, got %#v", empty)
	}
	first, err := app.RecordMaclawAppApprovalInstance(maclawAppApprovalInstance{AppID: "expense", Title: "Expense", Lane: "pending_my_approval", Status: "pending"})
	if err != nil {
		t.Fatalf("record first approval: %v", err)
	}
	second, err := app.RecordMaclawAppApprovalInstance(maclawAppApprovalInstance{AppID: "contract", Title: "Contract", Lane: "attention", Status: "attention"})
	if err != nil {
		t.Fatalf("record second approval: %v", err)
	}
	all, err := app.ListMaclawAppApprovalInstancesAll("all", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstancesAll all error = %v", err)
	}
	if len(all) != 2 || all[0].InstanceID != second.InstanceID || all[1].InstanceID != first.InstanceID {
		t.Fatalf("unexpected global approval order: %#v", all)
	}
	pending, err := app.ListMaclawAppApprovalInstancesAll("pending_my_approval", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstancesAll pending error = %v", err)
	}
	if len(pending) != 1 || pending[0].AppID != "expense" {
		t.Fatalf("unexpected pending global approvals: %#v", pending)
	}
	limited, err := app.ListMaclawAppApprovalInstancesAll("all", 1)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstancesAll limited error = %v", err)
	}
	if len(limited) != 1 || limited[0].AppID != "contract" {
		t.Fatalf("unexpected limited global approvals: %#v", limited)
	}
}

func TestListMaclawAppApprovalInstancesAllLoadsDataSrvLane(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	var capturedPath string
	var capturedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedQuery = r.URL.RawQuery
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/data/approvals" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		if r.Header.Get("X-MaClaw-User-ID") != "manager" {
			t.Fatalf("expected user header, got %q", r.Header.Get("X-MaClaw-User-ID"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"approval-remote-1","dataset_id":"finance.expenses","record_id":"exp-1","status":"pending","summary":"Expense #1","request":{"approval_instance_id":"wf-1","owner":"alice","applicant":"alice","businessEntity":"expense","businessAction":"submit","businessNote":"taxi","objectRole":"expense_report","approvalEvent":"expense.submitted","detailURL":"approval://instances/wf-1","maclaw_app_id":"expense","blueprintID":"expense.v1","workflowSkillId":"expense-workflow","workflowVersion":"4.0.0","approvalWorkflowID":"expense-flow","current_node_ids":["manager_approval","finance_review"]},"workflow_instance_id":"wf-1","business_status":"approval_pending","result_status":"pending","result_payload":{"text":"waiting for manager"},"outputs":[{"type":"content","title":"Summary","text":"waiting"}],"artifacts":[{"id":"receipt-1","name":"receipt.pdf","uri":"artifact://receipt"}],"assigned_to":"manager","created_by":"alice","created_at":"2026-06-21T01:00:00Z","updated_at":"2026-06-21T02:00:00Z"}]}`))
	}))
	defer server.Close()
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "manager", Role: "approver"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	items, err := app.ListMaclawAppApprovalInstancesAll("pending_my_approval", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstancesAll() error = %v", err)
	}
	if capturedPath != "/api/v1/data/approvals" || !strings.Contains(capturedQuery, "lane=pending_my_approval") || !strings.Contains(capturedQuery, "limit=10") {
		t.Fatalf("unexpected DataSrv query: %s?%s", capturedPath, capturedQuery)
	}
	if len(items) != 1 {
		t.Fatalf("expected one remote approval, got %#v", items)
	}
	got := items[0]
	if got.AppID != "expense" || got.ApprovalID != "approval-remote-1" || got.RecordApprovalID != "approval-remote-1" || got.InstanceID != "wf-1" || got.DetailURL != "approval://instances/wf-1" || got.WorkflowVersion != "4.0.0" || got.Lane != "pending_my_approval" {
		t.Fatalf("unexpected remote approval identity: %#v", got)
	}
	if got.BlueprintID != "expense.v1" || got.ApprovalWorkflowID != "expense-flow" || got.WorkflowSkillID != "expense-workflow" || got.CurrentNode != "manager_approval" || len(got.CurrentNodeIDs) != 2 || got.CurrentNodeIDs[1] != "finance_review" {
		t.Fatalf("remote approval should preserve request aliases for app/workflow/current nodes: %#v", got)
	}
	if got.DatasetID != "finance.expenses" || got.ObjectRole != "expense_report" || got.ApprovalObjectRole != "expense_report" || got.ApprovalEvent != "expense.submitted" || got.BusinessEntity != "expense" || got.BusinessAction != "submit" || got.BusinessNote != "taxi" {
		t.Fatalf("remote approval should preserve business context: %#v", got)
	}
	if got.ResultPayload["text"] != "waiting for manager" || len(got.Outputs) != 1 || len(got.Artifacts) != 1 || got.Artifacts[0].Name != "receipt.pdf" {
		t.Fatalf("remote approval should preserve result fields: %#v", got)
	}
}

func TestListMaclawAppApprovalInstancesLoadsRequestOnlyRuntimeResult(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/data/approvals" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"approval-request-only-1","dataset_id":"finance.expenses","record_id":"exp-request-only-1","status":"pending","summary":"Request-only approval","request":{"approval_instance_id":"wf-request-only-1","appID":"expense","applicant":"alice","currentAssignee":"manager","currentAssigneeType":"user","workflowNodeId":"manager_approval","workflowNodeIds":["submit","manager_approval"],"workflowDecisionId":"decision-9","businessStatus":"approval_pending","resultStatus":"pending_review","fromStatus":"submitted","toStatus":"approval_pending","resultPayload":{"text":"waiting from request","amount":128},"outputs":[{"type":"content","title":"Request Summary","text":"waiting from request"}],"artifacts":[{"id":"request-artifact-1","name":"request.pdf","uri":"artifact://request"}]},"created_by":"alice","created_at":"2026-06-21T01:00:00Z","updated_at":"2026-06-21T02:00:00Z"}]}`))
	}))
	defer server.Close()
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "manager", Role: "approver"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	items, err := app.ListMaclawAppApprovalInstancesAll("pending_my_approval", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstancesAll() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one request-only approval, got %#v", items)
	}
	got := items[0]
	if got.AppID != "expense" || got.InstanceID != "wf-request-only-1" || got.CurrentNode != "manager_approval" || got.WorkflowDecisionID != "decision-9" {
		t.Fatalf("request-only approval should restore identity and workflow context: %#v", got)
	}
	if len(got.CurrentNodeIDs) != 2 || got.CurrentNodeIDs[1] != "manager_approval" || got.BusinessStatus != "approval_pending" || got.ResultStatus != "pending_review" || got.FromStatus != "submitted" || got.ToStatus != "approval_pending" {
		t.Fatalf("request-only approval should restore status transition context: %#v", got)
	}
	if got.Result != "waiting from request" || got.ResultPayload["text"] != "waiting from request" || len(got.Outputs) != 1 || got.Outputs[0].Title != "Request Summary" || len(got.Artifacts) != 1 || got.Artifacts[0].Name != "request.pdf" {
		t.Fatalf("request-only approval should restore result package: %#v", got)
	}
}

func TestListMaclawAppApprovalInstancesRestoresDataSrvNodeStatusAndTasks(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/data/approvals" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"approval-node-task-1","dataset_id":"finance.expenses","record_id":"exp-node-task-1","app_id":"expense","status":"pending","summary":"Node task approval","request":{"approval_instance_id":"wf-node-task-1","applicant":"alice","current_assignee":"manager","current_node_status":"waiting_for_manager","node_tasks":[{"id":"task-manager","node":"manager_approval","status":"pending","assignee":"manager","label":"Manager approval"}]},"workflow_instance_id":"wf-node-task-1","workflow_node_id":"manager_approval","result_payload":{"text":"waiting for node task","node_status":"waiting_for_manager","approval_tasks":[{"id":"task-finance","node":"finance_review","status":"queued","assignee":"finance"}]},"created_by":"alice","created_at":"2026-06-21T01:00:00Z","updated_at":"2026-06-21T02:00:00Z"}]}`))
	}))
	defer server.Close()
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "manager", Role: "approver"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	items, err := app.ListMaclawAppApprovalInstancesAll("pending_my_approval", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstancesAll() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one node task approval, got %#v", items)
	}
	got := items[0]
	if got.CurrentNode != "manager_approval" || got.CurrentNodeStatus != "waiting_for_manager" {
		t.Fatalf("approval should restore current node status: %#v", got)
	}
	if len(got.NodeTasks) != 1 || got.NodeTasks[0]["id"] != "task-manager" || got.NodeTasks[0]["status"] != "pending" || got.NodeTasks[0]["assignee"] != "manager" {
		t.Fatalf("approval should prefer request node tasks for current node management: %#v", got.NodeTasks)
	}
	cloned := cloneMaclawAppApprovalInstance(got)
	cloned.NodeTasks[0]["status"] = "mutated"
	if got.NodeTasks[0]["status"] != "pending" {
		t.Fatalf("node tasks should be cloned defensively: original=%#v cloned=%#v", got.NodeTasks, cloned.NodeTasks)
	}
}

func TestListMaclawAppApprovalInstancesMapsDataSrvAttentionStatus(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	var capturedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/data/approvals" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"approval-attention-1","dataset_id":"finance.expenses","record_id":"exp-attention-1","app_id":"expense","status":"pending","summary":"Expense needs attention","request":{"approval_instance_id":"wf-attention-1","owner":"alice","applicant":"alice"},"workflow_skill_id":"expense-workflow","workflow_instance_id":"wf-attention-1","workflow_node_id":"finance_review","detail_url":"approval://instances/wf-attention-1","business_status":"attention","result_status":"attention","result_payload":{"summary":"missing invoice"},"assigned_to":"manager","created_by":"alice","created_at":"2026-06-21T01:00:00Z","updated_at":"2026-06-21T02:00:00Z"}]}`))
	}))
	defer server.Close()
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "manager", Role: "approver"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	items, err := app.ListMaclawAppApprovalInstancesAll("attention", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstancesAll attention error = %v", err)
	}
	if !strings.Contains(capturedQuery, "lane=attention") {
		t.Fatalf("expected attention lane query, got %s", capturedQuery)
	}
	if len(items) != 1 {
		t.Fatalf("expected one attention approval, got %#v", items)
	}
	got := items[0]
	if got.Lane != "attention" || got.Status != "attention" || got.BusinessStatus != "attention" || got.ResultStatus != "attention" || got.DetailURL != "approval://instances/wf-attention-1" {
		t.Fatalf("attention approval should preserve attention lane and status: %#v", got)
	}
	if got.Result != "missing invoice" || got.ResultPayload["summary"] != "missing invoice" {
		t.Fatalf("attention approval should expose result summary: %#v", got)
	}
}

func TestListMaclawAppApprovalInstancesAllInfersDataSrvLanesForCurrentUser(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	var capturedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/data/approvals" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"approval-request-1","dataset_id":"finance.expenses","record_id":"exp-request-1","app_id":"expense","status":"pending","summary":"My request","request":{"approval_instance_id":"wf-request-1","applicant":"manager"},"workflow_instance_id":"wf-request-1","workflow_node_id":"director_approval","assigned_to":"director","submitted_by":"manager","created_by":"manager","created_at":"2026-06-21T01:00:00Z","updated_at":"2026-06-21T02:00:00Z"},{"id":"approval-pending-1","dataset_id":"finance.expenses","record_id":"exp-pending-1","app_id":"expense","status":"pending","summary":"Needs my approval","request":{"approval_instance_id":"wf-pending-1","applicant":"alice","current_assignee":"manager","current_assignee_type":"user"},"workflow_instance_id":"wf-pending-1","workflow_node_id":"manager_approval","submitted_by":"alice","created_by":"alice","created_at":"2026-06-21T01:01:00Z","updated_at":"2026-06-21T02:01:00Z"},{"id":"approval-handled-1","dataset_id":"finance.expenses","record_id":"exp-handled-1","app_id":"expense","status":"approved","summary":"Handled by me","request":{"approval_instance_id":"wf-handled-1","applicant":"alice"},"workflow_instance_id":"wf-handled-1","workflow_node_id":"completed","assigned_to":"manager","reviewed_by":"alice","submitted_by":"alice","created_by":"alice","created_at":"2026-06-21T01:02:00Z","updated_at":"2026-06-21T02:02:00Z"}]}`))
	}))
	defer server.Close()
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "manager", Role: "approver"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	items, err := app.ListMaclawAppApprovalInstancesAll("all", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstancesAll all error = %v", err)
	}
	if strings.Contains(capturedQuery, "lane=") {
		t.Fatalf("all lane should not send a lane filter, got %s", capturedQuery)
	}
	lanes := map[string]string{}
	owners := map[string]string{}
	assignees := map[string]string{}
	assigneeTypes := map[string]string{}
	for _, item := range items {
		lanes[item.ApprovalID] = item.Lane
		owners[item.ApprovalID] = item.Owner
		assignees[item.ApprovalID] = item.CurrentAssignee
		assigneeTypes[item.ApprovalID] = item.CurrentAssigneeType
	}
	if lanes["approval-request-1"] != "my_requests" || lanes["approval-pending-1"] != "pending_my_approval" || lanes["approval-handled-1"] != "handled" {
		t.Fatalf("unexpected inferred lanes: %#v items=%#v", lanes, items)
	}
	if owners["approval-request-1"] != "manager" || owners["approval-pending-1"] != "alice" {
		t.Fatalf("submitted_by should drive owner context: %#v", owners)
	}
	if assignees["approval-pending-1"] != "manager" || assigneeTypes["approval-pending-1"] != "user" {
		t.Fatalf("request assignee aliases should drive pending lane context: assignees=%#v types=%#v items=%#v", assignees, assigneeTypes, items)
	}
}

func TestListMaclawAppApprovalInstancesDoesNotTrustRequestedLaneForDataSrvItems(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	var capturedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/data/approvals" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"approval-request-1","app_id":"expense","status":"pending","summary":"My request","request":{"approval_instance_id":"wf-request-1","applicant":"manager"},"workflow_instance_id":"wf-request-1","assigned_to":"director","submitted_by":"manager","created_by":"manager","created_at":"2026-06-21T01:00:00Z","updated_at":"2026-06-21T02:00:00Z"},{"id":"approval-other-pending-1","app_id":"expense","status":"pending","summary":"Other pending","request":{"approval_instance_id":"wf-other-pending-1","applicant":"alice","current_assignee":"director"},"workflow_instance_id":"wf-other-pending-1","assigned_to":"director","submitted_by":"alice","created_by":"alice","created_at":"2026-06-21T01:01:00Z","updated_at":"2026-06-21T02:01:00Z"},{"id":"approval-my-pending-1","app_id":"expense","status":"pending","summary":"Needs my approval","request":{"approval_instance_id":"wf-my-pending-1","applicant":"alice","current_assignee":"manager"},"workflow_instance_id":"wf-my-pending-1","assigned_to":"manager","submitted_by":"alice","created_by":"alice","created_at":"2026-06-21T01:02:00Z","updated_at":"2026-06-21T02:02:00Z"}]}`))
	}))
	defer server.Close()
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "manager", Role: "approver"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	items, err := app.ListMaclawAppApprovalInstancesAll("pending_my_approval", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstancesAll pending_my_approval error = %v", err)
	}
	if !strings.Contains(capturedQuery, "lane=pending_my_approval") {
		t.Fatalf("expected pending lane query, got %s", capturedQuery)
	}
	if len(items) != 1 || items[0].ApprovalID != "approval-my-pending-1" || items[0].Lane != "pending_my_approval" {
		t.Fatalf("requested lane should not override DataSrv item lane inference: %#v", items)
	}

	all, err := app.ListMaclawAppApprovalInstancesAll("all", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstancesAll all error = %v", err)
	}
	lanes := map[string]string{}
	for _, item := range all {
		lanes[item.ApprovalID] = item.Lane
	}
	if lanes["approval-request-1"] != "my_requests" || lanes["approval-other-pending-1"] != "pending" || lanes["approval-my-pending-1"] != "pending_my_approval" {
		t.Fatalf("all lane should preserve inferred buckets for diagnosis: lanes=%#v items=%#v", lanes, all)
	}
}

func TestListMaclawAppApprovalInstancesKeepsSelfSubmittedPendingApprovalVisible(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	var capturedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/data/approvals" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"approval-self-pending-1","app_id":"expense","status":"pending","summary":"Self submitted and assigned","request":{"approval_instance_id":"wf-self-pending-1","applicant":"manager","current_assignee":"director; manager"},"workflow_instance_id":"wf-self-pending-1","assigned_to":"director; manager","submitted_by":"manager","created_by":"manager","created_at":"2026-06-21T01:00:00Z","updated_at":"2026-06-21T02:00:00Z"}]}`))
	}))
	defer server.Close()
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "manager", Role: "approver"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	items, err := app.ListMaclawAppApprovalInstancesAll("pending_my_approval", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstancesAll pending_my_approval error = %v", err)
	}
	if !strings.Contains(capturedQuery, "lane=pending_my_approval") {
		t.Fatalf("expected pending lane query, got %s", capturedQuery)
	}
	if len(items) != 1 || items[0].ApprovalID != "approval-self-pending-1" || items[0].Lane != "pending_my_approval" || items[0].Owner != "manager" {
		t.Fatalf("self-submitted pending approval should remain visible in pending lane when assigned to current user: %#v", items)
	}
}

func TestListMaclawAppApprovalInstancesHonorsExplicitDataSrvLaneCaseInsensitively(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/data/approvals" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"approval-explicit-lane-1","app_id":"expense","status":"PENDING","summary":"Explicit lane","request":{"approval_instance_id":"wf-explicit-lane-1","approval_lane":"PENDING_MY_APPROVAL","applicant":"alice"},"workflow_instance_id":"wf-explicit-lane-1","assigned_to":"director","submitted_by":"alice","created_by":"alice","created_at":"2026-06-21T01:00:00Z","updated_at":"2026-06-21T02:00:00Z"}]}`))
	}))
	defer server.Close()
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "manager", Role: "approver"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	items, err := app.ListMaclawAppApprovalInstancesAll("pending_my_approval", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstancesAll pending_my_approval error = %v", err)
	}
	if len(items) != 1 || items[0].ApprovalID != "approval-explicit-lane-1" || items[0].Lane != "pending_my_approval" || items[0].Status != "pending" {
		t.Fatalf("explicit DataSrv lane/status should be normalized case-insensitively: %#v", items)
	}
}

func TestListMaclawAppApprovalInstancesMergesDataSrvWithLocal(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	local, err := app.RecordMaclawAppApprovalInstance(maclawAppApprovalInstance{
		AppID:            "expense",
		AppName:          "Expense Approval",
		InstanceID:       "wf-merge-1",
		Title:            "Local title",
		Lane:             "pending_my_approval",
		Status:           "pending",
		Owner:            "alice",
		Approver:         "manager",
		WorkflowSkillID:  "expense-workflow",
		RecordApprovalID: "approval-merge-1",
		ApprovalID:       "approval-merge-1",
		ResultPayload:    map[string]any{"local": "kept only when remote empty"},
		Events:           []maclawAppApprovalEvent{{At: "2026-06-21T00:30:00Z", Actor: "alice", Message: "created locally"}},
	})
	if err != nil {
		t.Fatalf("RecordMaclawAppApprovalInstance() error = %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/data/approvals" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		if r.URL.Query().Get("app_id") != "expense" || r.URL.Query().Get("lane") != "handled" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"approval-merge-1","dataset_id":"finance.expenses","record_id":"exp-merge-1","app_id":"expense","status":"approved","summary":"Remote approved title","request":{"approval_instance_id":"wf-merge-1","business_note":"remote note"},"workflow_skill_id":"expense-workflow","workflow_instance_id":"wf-merge-1","workflow_node_id":"completed","workflow_node_ids":["completed","archive"],"business_status":"approved","result_status":"approved","result_payload":{"text":"approved remotely","business_record":{"id":"exp-merge-1","status":"approved"}},"outputs":[{"type":"approval_result","title":"Remote decision","text":"approved remotely","status":"approved","data":{"approval_result":"approved"}}],"artifacts":[{"id":"remote-artifact-1","name":"approved-expense.pdf","uri":"artifact://expense/approved","status":"ready"}],"decision":"approved","reason":"approved remotely","assigned_to":"manager","created_by":"alice","reviewed_by":"alice","created_at":"2026-06-21T01:00:00Z","updated_at":"2026-06-21T03:00:00Z"}]}`))
	}))
	defer server.Close()
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "manager", Role: "approver"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	items, err := app.ListMaclawAppApprovalInstances("expense", "handled", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected merged approval, got %#v", items)
	}
	got := items[0]
	if got.InstanceID != local.InstanceID || got.ApprovalID != "approval-merge-1" || got.Status != "approved" || got.Lane != "handled" || got.Result != "approved remotely" {
		t.Fatalf("remote approval should win status fields while deduping local: %#v", got)
	}
	if got.AppName != "Expense Approval" || len(got.Events) != 1 || got.BusinessNote != "remote note" || got.ResultPayload["text"] != "approved remotely" || len(got.CurrentNodeIDs) != 2 || got.CurrentNodeIDs[0] != "completed" || got.CurrentNodeIDs[1] != "archive" || len(got.Outputs) != 1 || got.Outputs[0].Title != "Remote decision" || len(got.Artifacts) != 1 || got.Artifacts[0].Name != "approved-expense.pdf" {
		t.Fatalf("merged approval should keep local display context and remote result context: %#v", got)
	}
}

func TestSyncMaclawAppApprovalInstanceToDataSrvCreatesMissingBusinessRecord(t *testing.T) {
	type capturedRequest struct {
		Method string
		Path   string
		Body   map[string]interface{}
	}
	captured := []capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := capturedRequest{Method: r.Method, Path: r.URL.Path}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		captured = append(captured, item)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/datasets/finance.expenses/records/exp-new":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"record not found"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/datasets/finance.expenses/records":
			_, _ = w.Write([]byte(`{"id":"exp-new","data":{"amount":1200,"status":"approval_pending"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/datasets/finance.expenses/records/exp-new/approvals":
			_, _ = w.Write([]byte(`{"id":"approval-new-1","status":"pending"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"unexpected request"}`))
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "alice", Role: "approver"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}
	base := maclawAppApprovalInstance{
		AppID:           "expense",
		AppName:         "Expense",
		BlueprintID:     "expense.blueprint.v1",
		DatasetID:       "finance.expenses",
		ObjectRole:      "expense_report",
		InstanceID:      "appr-new-1",
		Title:           "Expense approval",
		Lane:            "my_requests",
		Status:          "pending",
		CurrentNode:     "manager_review",
		Owner:           "alice",
		Approver:        "manager",
		Result:          "waiting",
		WorkflowSkillID: "expense-approval-workflow",
		BusinessStatus:  "approval_pending",
		ResultStatus:    "pending",
		ResultPayload: map[string]any{"business_record": map[string]any{
			"id":     "exp-new",
			"amount": float64(1200),
			"status": "approval_pending",
		}},
	}

	created, err := app.SyncMaclawAppApprovalInstanceToDataSrv(maclawAppApprovalDataSrvSyncInput{DatasetID: "finance.expenses", ObjectRole: "expense_report", RecordID: "exp-new", Instance: base})
	if err != nil {
		t.Fatalf("SyncMaclawAppApprovalInstanceToDataSrv create missing record error = %v", err)
	}
	if created["synced"] != true || created["action"] != "create_record_approval" || created["approval_id"] != "approval-new-1" {
		t.Fatalf("unexpected sync result: %#v", created)
	}
	if sync, ok := created["business_record_sync"].(map[string]any); !ok || sync["synced"] != true || sync["action"] != "create_business_record" {
		t.Fatalf("sync should create missing business record before approval: %#v", created)
	}
	if len(captured) != 3 {
		t.Fatalf("captured %d requests, want 3: %#v", len(captured), captured)
	}

	if captured[0].Method != http.MethodGet || captured[0].Path != "/api/v1/data/datasets/finance.expenses/records/exp-new" {
		t.Fatalf("unexpected preflight get request: %#v", captured[0])
	}
	if captured[1].Method != http.MethodPost || captured[1].Path != "/api/v1/data/datasets/finance.expenses/records" {
		t.Fatalf("unexpected business record create request: %#v", captured[1])
	}
	if captured[1].Body["id"] != "exp-new" || captured[1].Body["source_id"] != "appr-new-1" || captured[1].Body["title"] != "Expense approval" {
		t.Fatalf("business record create body missing identity fields: %#v", captured[1].Body)
	}
	data, ok := captured[1].Body["data"].(map[string]interface{})
	if !ok || data["amount"] != float64(1200) || data["status"] != "approval_pending" {
		t.Fatalf("business record create body missing approval data: %#v", captured[1].Body)
	}
	if captured[2].Method != http.MethodPost || captured[2].Path != "/api/v1/data/datasets/finance.expenses/records/exp-new/approvals" {
		t.Fatalf("unexpected approval create request: %#v", captured[2])
	}
	if captured[2].Body["workflow_instance_id"] != "appr-new-1" || captured[2].Body["business_status"] != "approval_pending" {
		t.Fatalf("approval create body missing workflow link: %#v", captured[2].Body)
	}
}

func TestSyncMaclawAppApprovalInstanceToDataSrvCreatesMinimalBusinessRecord(t *testing.T) {
	type capturedRequest struct {
		Method string
		Path   string
		Body   map[string]interface{}
	}
	captured := []capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := capturedRequest{Method: r.Method, Path: r.URL.Path}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		captured = append(captured, item)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/datasets/finance.expenses/records/appr-min-1":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"record not found"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/datasets/finance.expenses/records":
			_, _ = w.Write([]byte(`{"id":"appr-min-1","data":{"approval_instance_id":"appr-min-1"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/datasets/finance.expenses/records/appr-min-1/approvals":
			_, _ = w.Write([]byte(`{"id":"approval-min-1","status":"pending"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"unexpected request"}`))
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "alice", Role: "approver"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}
	base := maclawAppApprovalInstance{
		AppID:              "expense",
		AppName:            "Expense",
		BlueprintID:        "expense.blueprint.v1",
		ObjectRole:         "expense_report",
		ApprovalObjectRole: "expense_report",
		ApprovalEvent:      "expense.submitted",
		InstanceID:         "appr-min-1",
		Title:              "Expense approval",
		Lane:               "my_requests",
		Status:             "pending",
		CurrentNode:        "manager_review",
		Owner:              "alice",
		Applicant:          "alice",
		Approver:           "manager",
		WorkflowSkillID:    "expense-approval-workflow",
		BusinessStatus:     "approval_pending",
		ResultStatus:       "pending",
		FromStatus:         "submitted",
		ToStatus:           "approval_pending",
		BusinessEntity:     "expense_report",
		BusinessAction:     "submit",
		BusinessNote:       "submitted from app UI",
	}

	created, err := app.SyncMaclawAppApprovalInstanceToDataSrv(maclawAppApprovalDataSrvSyncInput{DatasetID: "finance.expenses", ObjectRole: "expense_report", RecordID: "appr-min-1", Instance: base})
	if err != nil {
		t.Fatalf("SyncMaclawAppApprovalInstanceToDataSrv minimal record error = %v", err)
	}
	if sync, ok := created["business_record_sync"].(map[string]any); !ok || sync["synced"] != true || sync["action"] != "create_business_record" {
		t.Fatalf("sync should create minimal business record before approval: %#v", created)
	}
	if len(captured) != 3 {
		t.Fatalf("captured %d requests, want 3: %#v", len(captured), captured)
	}
	data, ok := captured[1].Body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("minimal business record create body missing data: %#v", captured[1].Body)
	}
	if data["approval_instance_id"] != "appr-min-1" || data["workflow_skill_id"] != "expense-approval-workflow" || data["business_status"] != "approval_pending" || data["business_action"] != "submit" {
		t.Fatalf("minimal business record data missing app approval context: %#v", data)
	}
	if captured[2].Method != http.MethodPost || captured[2].Path != "/api/v1/data/datasets/finance.expenses/records/appr-min-1/approvals" {
		t.Fatalf("unexpected approval create request: %#v", captured[2])
	}
}

func TestSyncMaclawAppApprovalInstanceToDataSrvCreatesAttentionApproval(t *testing.T) {
	type capturedRequest struct {
		Method string
		Path   string
		Body   map[string]interface{}
	}
	captured := []capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := capturedRequest{Method: r.Method, Path: r.URL.Path}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		captured = append(captured, item)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/datasets/finance.expenses/records/exp-attention-1":
			_, _ = w.Write([]byte(`{"id":"exp-attention-1","data":{"amount":880,"status":"review_attention"}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/data/datasets/finance.expenses/records/exp-attention-1":
			_, _ = w.Write([]byte(`{"id":"exp-attention-1","data":{"status":"attention"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/datasets/finance.expenses/records/exp-attention-1/approvals":
			_, _ = w.Write([]byte(`{"id":"approval-attention-1","status":"pending","kind":"attention"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"unexpected request"}`))
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "alice", Role: "approver"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}
	base := maclawAppApprovalInstance{
		AppID:              "expense",
		AppName:            "Expense",
		BlueprintID:        "expense.blueprint.v1",
		DatasetID:          "finance.expenses",
		ObjectRole:         "expense_report",
		ApprovalObjectRole: "expense_report",
		ApprovalEvent:      "finance.submitted",
		InstanceID:         "appr-attention-1",
		Title:              "Expense needs attention",
		Lane:               "attention",
		Status:             "attention",
		CurrentNode:        "finance_attention",
		Owner:              "alice",
		Applicant:          "alice",
		Approver:           "manager",
		Result:             "receipt needs a manual look",
		WorkflowSkillID:    "expense-approval-workflow",
		WorkflowVersion:    "2.1.0",
		WorkflowDecisionID: "decision-attention-1",
		DetailURL:          "approval://instances/appr-attention-1",
		BusinessStatus:     "attention",
		ResultStatus:       "attention",
		ResultPayload:      map[string]any{"summary": "manual review only", "business_record": map[string]any{"id": "exp-attention-1", "status": "attention"}},
		Outputs:            []maclawAppApprovalOutput{{Type: "text", Kind: "attention", Title: "Attention", Text: "receipt needs a manual look", Status: "attention"}},
		Artifacts:          []maclawAppApprovalArtifact{{ID: "artifact-attention-1", Name: "receipt.pdf", URI: "artifact://approval/attention-receipt", Status: "available"}},
	}

	created, err := app.SyncMaclawAppApprovalInstanceToDataSrv(maclawAppApprovalDataSrvSyncInput{DatasetID: "finance.expenses", ObjectRole: "expense_report", RecordID: "exp-attention-1", Instance: base})
	if err != nil {
		t.Fatalf("SyncMaclawAppApprovalInstanceToDataSrv attention create error = %v", err)
	}
	if created["synced"] != true || created["action"] != "create_record_approval" || created["approval_id"] != "approval-attention-1" {
		t.Fatalf("unexpected attention sync result: %#v", created)
	}
	if len(captured) != 3 {
		t.Fatalf("captured %d requests, want 3: %#v", len(captured), captured)
	}
	attentionPatchedData, ok := captured[1].Body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("attention business record patch missing data: %#v", captured[1].Body)
	}
	if payload, ok := attentionPatchedData["approval_result_payload"].(map[string]interface{}); !ok || payload["summary"] != "manual review only" {
		t.Fatalf("attention business record patch missing result payload: %#v", captured[1].Body)
	}
	if outputs, ok := attentionPatchedData["approval_outputs"].([]interface{}); !ok || len(outputs) != 1 {
		t.Fatalf("attention business record patch missing outputs: %#v", captured[1].Body)
	}
	if artifacts, ok := attentionPatchedData["approval_artifacts"].([]interface{}); !ok || len(artifacts) != 1 {
		t.Fatalf("attention business record patch missing artifacts: %#v", captured[1].Body)
	}
	if captured[2].Method != http.MethodPost || captured[2].Path != "/api/v1/data/datasets/finance.expenses/records/exp-attention-1/approvals" {
		t.Fatalf("unexpected attention approval create request: %#v", captured[2])
	}
	if captured[2].Body["kind"] != "attention" || captured[2].Body["workflow_instance_id"] != "appr-attention-1" || captured[2].Body["workflow_node_id"] != "finance_attention" || captured[2].Body["workflow_decision_id"] != "decision-attention-1" || captured[2].Body["detail_url"] != "approval://instances/appr-attention-1" {
		t.Fatalf("attention create body missing workflow attention fields: %#v", captured[2].Body)
	}
	if captured[2].Body["business_status"] != "attention" || captured[2].Body["result_status"] != "attention" {
		t.Fatalf("attention create body missing attention statuses: %#v", captured[2].Body)
	}
	if payload, ok := captured[2].Body["result_payload"].(map[string]interface{}); !ok || payload["summary"] != "manual review only" {
		t.Fatalf("attention create body missing result payload: %#v", captured[2].Body)
	}
	if outputs, ok := captured[2].Body["outputs"].([]interface{}); !ok || len(outputs) != 1 {
		t.Fatalf("attention create body missing outputs: %#v", captured[2].Body)
	}
	if artifacts, ok := captured[2].Body["artifacts"].([]interface{}); !ok || len(artifacts) != 1 {
		t.Fatalf("attention create body missing artifacts: %#v", captured[2].Body)
	}
	stored, err := app.ListMaclawAppApprovalInstances("expense", "attention", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances after attention sync error = %v", err)
	}
	if len(stored) != 1 || stored[0].ApprovalID != "approval-attention-1" || stored[0].RecordApprovalID != "approval-attention-1" || stored[0].Status != "attention" || stored[0].Lane != "attention" {
		t.Fatalf("attention sync should persist remote approval context: %#v", stored)
	}
}

func TestSyncMaclawAppApprovalInstanceToDataSrvAttentionViewOnlyUpdatesBusinessRecord(t *testing.T) {
	type capturedRequest struct {
		Method string
		Path   string
		Body   map[string]interface{}
	}
	captured := []capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := capturedRequest{Method: r.Method, Path: r.URL.Path}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		captured = append(captured, item)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/datasets/finance.expenses/records/exp-attention-existing":
			_, _ = w.Write([]byte(`{"id":"exp-attention-existing","data":{"amount":420,"status":"approval_pending"}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/data/datasets/finance.expenses/records/exp-attention-existing":
			_, _ = w.Write([]byte(`{"id":"exp-attention-existing","data":{"status":"attention"}}`))
		default:
			w.WriteHeader(http.StatusTeapot)
			_, _ = w.Write([]byte(`{"error":"attention view-only must not create or review approvals"}`))
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "alice", Role: "approver"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}
	base := maclawAppApprovalInstance{
		AppID:              "expense",
		AppName:            "Expense",
		BlueprintID:        "expense.blueprint.v1",
		DatasetID:          "finance.expenses",
		ObjectRole:         "expense_report",
		ApprovalObjectRole: "expense_report",
		ApprovalEvent:      "finance.submitted",
		InstanceID:         "appr-attention-existing",
		Title:              "Existing attention approval",
		Lane:               "attention",
		Status:             "attention",
		CurrentNode:        "finance_attention",
		Owner:              "alice",
		Applicant:          "alice",
		Approver:           "manager",
		CurrentAssignee:    "manager",
		Result:             "watch only",
		WorkflowSkillID:    "expense-approval-workflow",
		WorkflowVersion:    "2.1.0",
		DetailURL:          "approval://instances/appr-attention-existing",
		BusinessStatus:     "attention",
		ResultStatus:       "attention",
		ResultPayload:      map[string]any{"summary": "watch only", "business_record": map[string]any{"id": "exp-attention-existing", "status": "attention"}},
		Outputs:            []maclawAppApprovalOutput{{Type: "text", Kind: "attention", Title: "Attention", Text: "watch only", Status: "attention"}},
		Artifacts:          []maclawAppApprovalArtifact{{ID: "artifact-attention-existing", Name: "attention-note.pdf", URI: "artifact://approval/attention-note", Status: "available"}},
	}

	synced, err := app.SyncMaclawAppApprovalInstanceToDataSrv(maclawAppApprovalDataSrvSyncInput{DatasetID: "finance.expenses", ObjectRole: "expense_report", RecordID: "exp-attention-existing", ApprovalID: "approval-attention-existing", Instance: base})
	if err != nil {
		t.Fatalf("SyncMaclawAppApprovalInstanceToDataSrv attention view-only error = %v", err)
	}
	if synced["synced"] != true || synced["action"] != "attention_view_only" || synced["approval_id"] != "approval-attention-existing" {
		t.Fatalf("unexpected attention view-only sync result: %#v", synced)
	}
	if sync, ok := synced["business_record_sync"].(map[string]any); !ok || sync["synced"] != true || sync["action"] != "update_business_record" {
		t.Fatalf("attention view-only should update business record: %#v", synced)
	}
	if len(captured) != 2 {
		t.Fatalf("captured %d requests, want only business record get+patch: %#v", len(captured), captured)
	}
	if captured[0].Method != http.MethodGet || captured[0].Path != "/api/v1/data/datasets/finance.expenses/records/exp-attention-existing" {
		t.Fatalf("unexpected attention view-only get request: %#v", captured[0])
	}
	if captured[1].Method != http.MethodPatch || captured[1].Path != "/api/v1/data/datasets/finance.expenses/records/exp-attention-existing" {
		t.Fatalf("unexpected attention view-only patch request: %#v", captured[1])
	}
	patchedData, ok := captured[1].Body["data"].(map[string]interface{})
	if !ok || patchedData["amount"] != float64(420) || patchedData["status"] != "attention" || patchedData["approval_lane"] != "attention" || patchedData["approval_status"] != "attention" {
		t.Fatalf("attention view-only business patch should merge result semantics: %#v", captured[1].Body)
	}
	if payload, ok := patchedData["approval_result_payload"].(map[string]interface{}); !ok || payload["summary"] != "watch only" {
		t.Fatalf("attention view-only business patch missing full result payload: %#v", captured[1].Body)
	}
	if outputs, ok := patchedData["approval_outputs"].([]interface{}); !ok || len(outputs) != 1 {
		t.Fatalf("attention view-only business patch missing outputs: %#v", captured[1].Body)
	}
	if artifacts, ok := patchedData["approval_artifacts"].([]interface{}); !ok || len(artifacts) != 1 {
		t.Fatalf("attention view-only business patch missing artifacts: %#v", captured[1].Body)
	}
	stored, err := app.ListMaclawAppApprovalInstances("expense", "attention", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances after attention view-only sync error = %v", err)
	}
	if len(stored) != 1 || stored[0].ApprovalID != "approval-attention-existing" || stored[0].RecordApprovalID != "approval-attention-existing" || stored[0].Status != "attention" || stored[0].Lane != "attention" || stored[0].ResultPayload["summary"] != "watch only" {
		t.Fatalf("attention view-only should persist approval id and result context: %#v", stored)
	}
}

func TestSyncMaclawAppApprovalInstanceToDataSrvUpdatesPendingProgress(t *testing.T) {
	type capturedRequest struct {
		Method string
		Path   string
		Body   map[string]interface{}
	}
	captured := []capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := capturedRequest{Method: r.Method, Path: r.URL.Path}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		captured = append(captured, item)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/approvals/approval-progress-1/progress":
			_, _ = w.Write([]byte(`{"id":"approval-progress-1","status":"pending"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/datasets/finance.expenses/records/exp-progress-1":
			_, _ = w.Write([]byte(`{"id":"exp-progress-1","data":{"amount":1200,"status":"approval_pending"}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/data/datasets/finance.expenses/records/exp-progress-1":
			_, _ = w.Write([]byte(`{"id":"exp-progress-1","data":{"status":"finance_reviewing"}}`))
		default:
			w.WriteHeader(http.StatusTeapot)
			_, _ = w.Write([]byte(`{"error":"pending progress must not create or review approvals"}`))
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "alice", Role: "approver"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}
	base := maclawAppApprovalInstance{
		AppID:               "expense",
		AppName:             "Expense",
		BlueprintID:         "expense.blueprint.v1",
		DatasetID:           "finance.expenses",
		ApprovalObjectRole:  "expense_report",
		ApprovalEvent:       "finance.submitted",
		ApprovalWorkflowID:  "expense_approval",
		InstanceID:          "appr-progress-1",
		Title:               "Expense approval progress",
		Lane:                "pending_my_approval",
		Status:              "pending",
		CurrentNode:         "finance.director_review",
		CurrentNodeStatus:   "running",
		CurrentNodeIDs:      []string{"manager_review", "finance.director_review"},
		NodeTasks:           []map[string]any{{"id": "task-director-review", "status": "running", "assignee": "finance_director"}},
		Owner:               "alice",
		Applicant:           "alice",
		Approver:            "manager",
		CurrentAssignee:     "finance_director",
		CurrentAssigneeType: "role",
		Result:              "Director review started",
		WorkflowSkillID:     "expense-approval-workflow",
		WorkflowVersion:     "2.1.0",
		WorkflowDecisionID:  "progress-tick-1",
		DetailURL:           "approval://instances/appr-progress-1?node=finance.director_review",
		BusinessStatus:      "finance_reviewing",
		ResultStatus:        "running",
		FromStatus:          "approval_pending",
		ToStatus:            "finance_reviewing",
		ResultPayload:       map[string]any{"progress": "Director review started", "business_record": map[string]any{"id": "exp-progress-1", "status": "finance_reviewing"}},
		Outputs:             []maclawAppApprovalOutput{{Type: "text", Kind: "progress", Title: "Progress", Text: "Director review started", Status: "running"}},
		Artifacts:           []maclawAppApprovalArtifact{{ID: "progress-log", Name: "progress-log.txt", URI: "artifact://approval/progress-log", Status: "running"}},
	}

	synced, err := app.SyncMaclawAppApprovalInstanceToDataSrv(maclawAppApprovalDataSrvSyncInput{DatasetID: "finance.expenses", ObjectRole: "expense_report", RecordID: "exp-progress-1", ApprovalID: "approval-progress-1", Instance: base})
	if err != nil {
		t.Fatalf("SyncMaclawAppApprovalInstanceToDataSrv progress error = %v", err)
	}
	if synced["synced"] != true || synced["action"] != "update_record_approval_progress" || synced["approval_id"] != "approval-progress-1" {
		t.Fatalf("unexpected progress sync result: %#v", synced)
	}
	if sync, ok := synced["business_record_sync"].(map[string]any); !ok || sync["synced"] != true || sync["action"] != "update_business_record" {
		t.Fatalf("progress sync should update business record: %#v", synced)
	}
	if len(captured) != 3 {
		t.Fatalf("captured %d requests, want progress post + business get/patch: %#v", len(captured), captured)
	}
	if captured[0].Method != http.MethodPost || captured[0].Path != "/api/v1/data/approvals/approval-progress-1/progress" {
		t.Fatalf("unexpected progress request: %#v", captured[0])
	}
	if captured[0].Body["workflow_instance_id"] != "appr-progress-1" || captured[0].Body["workflow_node_id"] != "finance.director_review" || fmt.Sprint(captured[0].Body["workflow_node_ids"]) != "[manager_review finance.director_review]" || captured[0].Body["current_node_status"] != "running" || captured[0].Body["business_status"] != "finance_reviewing" || captured[0].Body["result_status"] != "running" || captured[0].Body["current_assignee"] != "finance_director" || captured[0].Body["current_assignee_type"] != "role" || captured[0].Body["progress"] != "Director review started" {
		t.Fatalf("progress body missing running workflow fields: %#v", captured[0].Body)
	}
	if tasks, ok := captured[0].Body["node_tasks"].([]interface{}); !ok || len(tasks) != 1 {
		t.Fatalf("progress body missing node tasks: %#v", captured[0].Body)
	}
	if captured[0].Body["result_payload"] == nil || captured[0].Body["outputs"] == nil || captured[0].Body["artifacts"] == nil {
		t.Fatalf("progress body missing result package: %#v", captured[0].Body)
	}
	if captured[1].Method != http.MethodGet || captured[1].Path != "/api/v1/data/datasets/finance.expenses/records/exp-progress-1" {
		t.Fatalf("unexpected business record get request: %#v", captured[1])
	}
	if captured[2].Method != http.MethodPatch || captured[2].Path != "/api/v1/data/datasets/finance.expenses/records/exp-progress-1" {
		t.Fatalf("unexpected business record patch request: %#v", captured[2])
	}
	patchedData, ok := captured[2].Body["data"].(map[string]interface{})
	if !ok || patchedData["approval_id"] != "approval-progress-1" || patchedData["approval_status"] != "pending" || patchedData["approval_current_node"] != "finance.director_review" || patchedData["approval_current_node_status"] != "running" || patchedData["business_status"] != "finance_reviewing" || patchedData["result_status"] != "running" || patchedData["approval_current_assignee"] != "finance_director" {
		t.Fatalf("business record patch should preserve running progress semantics: %#v", captured[2].Body)
	}
	if tasks, ok := patchedData["approval_node_tasks"].([]interface{}); !ok || len(tasks) != 1 {
		t.Fatalf("business record patch should preserve running node tasks: %#v", captured[2].Body)
	}
	stored, err := app.ListMaclawAppApprovalInstances("expense", "all", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances after progress sync error = %v", err)
	}
	if len(stored) != 1 || stored[0].ApprovalID != "approval-progress-1" || stored[0].RecordApprovalID != "approval-progress-1" || stored[0].Status != "pending" || stored[0].CurrentNode != "finance.director_review" || stored[0].CurrentNodeStatus != "running" || len(stored[0].NodeTasks) != 1 || stored[0].BusinessStatus != "finance_reviewing" || stored[0].ResultStatus != "running" {
		t.Fatalf("progress sync should persist remote approval context: %#v", stored)
	}
}

func TestSyncMaclawAppApprovalInstanceToDataSrv(t *testing.T) {
	type capturedRequest struct {
		Method string
		Path   string
		Body   map[string]interface{}
	}
	captured := []capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := capturedRequest{Method: r.Method, Path: r.URL.Path}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		captured = append(captured, item)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/datasets/finance.expenses/records/exp-1" {
			_, _ = w.Write([]byte(`{"id":"exp-1","data":{"amount":1200,"status":"approval_pending"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"approval-remote-1","ok":true}`))
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "alice", Role: "approver"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}
	base := maclawAppApprovalInstance{
		AppID:               "expense",
		AppName:             "Expense",
		BlueprintID:         "expense.blueprint.v1",
		DatasetID:           "finance.expenses",
		ApprovalObjectRole:  "expense_report",
		ApprovalEvent:       "finance.submitted",
		ApprovalWorkflowID:  "expense_approval",
		InstanceID:          "appr-1",
		Title:               "Expense approval",
		Lane:                "pending_my_approval",
		Status:              "pending",
		CurrentNode:         "manager_review",
		CurrentNodeStatus:   "waiting",
		CurrentNodeIDs:      []string{"manager_review", "finance_review"},
		NodeTasks:           []map[string]any{{"id": "task-manager-review", "status": "pending", "assignee": "manager"}},
		Owner:               "alice",
		Approver:            "manager",
		CurrentAssignee:     "manager",
		CurrentAssigneeType: "user",
		Result:              "waiting",
		WorkflowSkillID:     "expense-approval-workflow",
		WorkflowVersion:     "2.1.0",
		DetailURL:           "approval://instances/appr-1",
		BusinessStatus:      "approval_pending",
		ResultStatus:        "pending",
		FromStatus:          "submitted",
		ToStatus:            "approval_pending",
		ResultPayload:       map[string]any{"business_record": map[string]any{"id": "exp-1"}},
		Outputs:             []maclawAppApprovalOutput{{Type: "text", Title: "Summary", Text: "waiting for manager"}},
		Artifacts:           []maclawAppApprovalArtifact{{ID: "artifact-1", Name: "receipt.pdf", URI: "artifact://approval/receipt"}},
	}
	created, err := app.SyncMaclawAppApprovalInstanceToDataSrv(maclawAppApprovalDataSrvSyncInput{DatasetID: "finance.expenses", RecordID: "exp-1", Instance: base})
	if err != nil {
		t.Fatalf("SyncMaclawAppApprovalInstanceToDataSrv create error = %v", err)
	}
	if created["synced"] != true || created["action"] != "create_record_approval" {
		t.Fatalf("unexpected create sync result: %#v", created)
	}
	if sync, ok := created["business_record_sync"].(map[string]any); !ok || sync["synced"] != true || sync["action"] != "update_business_record" {
		t.Fatalf("create sync should update business record before approval: %#v", created)
	}
	base.Status = "approved"
	base.Lane = "handled"
	base.Result = "approved"
	base.WorkflowDecisionID = "decision-1"
	base.CurrentNode = "finance_review"
	base.CurrentNodeStatus = "completed"
	base.CurrentNodeIDs = []string{"finance_review", "legal_review"}
	base.NodeTasks = []map[string]any{{"id": "task-finance-review", "status": "done", "assignee": "finance_queue"}}
	base.CurrentAssignee = "finance_queue"
	base.CurrentAssigneeType = "queue"
	base.FromStatus = "approval_pending"
	base.ToStatus = "approved"
	base.BusinessStatus = "approved"
	base.ResultStatus = "approved"
	base.ResultPayload = map[string]any{"business_record": map[string]any{"id": "exp-1", "status": "approved", "payment_status": "pending_payment"}}
	reviewed, err := app.SyncMaclawAppApprovalInstanceToDataSrv(maclawAppApprovalDataSrvSyncInput{DatasetID: "finance.expenses", RecordID: "exp-1", ApprovalID: "approval-remote-1", Instance: base})
	if err != nil {
		t.Fatalf("SyncMaclawAppApprovalInstanceToDataSrv review error = %v", err)
	}
	if reviewed["synced"] != true || reviewed["action"] != "review_record_approval" || reviewed["approval_id"] != "approval-remote-1" {
		t.Fatalf("unexpected review sync result: %#v", reviewed)
	}
	if sync, ok := reviewed["business_record_sync"].(map[string]any); !ok || sync["synced"] != true || sync["action"] != "update_business_record" {
		t.Fatalf("review sync should update business record: %#v", reviewed)
	}
	if len(captured) != 6 {
		t.Fatalf("captured %d requests, want 6: %#v", len(captured), captured)
	}
	if captured[0].Method != http.MethodGet || captured[0].Path != "/api/v1/data/datasets/finance.expenses/records/exp-1" {
		t.Fatalf("unexpected pre-create business record get request: %#v", captured[0])
	}
	if captured[1].Method != http.MethodPatch || captured[1].Path != "/api/v1/data/datasets/finance.expenses/records/exp-1" {
		t.Fatalf("unexpected pre-create business record patch request: %#v", captured[1])
	}
	preCreatePatchedData, ok := captured[1].Body["data"].(map[string]interface{})
	if !ok || preCreatePatchedData["amount"] != float64(1200) || preCreatePatchedData["status"] != "approval_pending" {
		t.Fatalf("pre-create business record patch should keep pending approval data: %#v", captured[1].Body)
	}
	if preCreatePatchedData["app_id"] != "expense" || preCreatePatchedData["blueprint_id"] != "expense.blueprint.v1" || preCreatePatchedData["object_role"] != "expense_report" || preCreatePatchedData["approval_lane"] != "pending_my_approval" || preCreatePatchedData["approval_current_node"] != "manager_review" || preCreatePatchedData["approval_current_node_status"] != "waiting" || fmt.Sprint(preCreatePatchedData["approval_current_nodes"]) != "[manager_review finance_review]" || fmt.Sprint(preCreatePatchedData["workflow_node_ids"]) != "[manager_review finance_review]" || preCreatePatchedData["workflow_version"] != "2.1.0" {
		t.Fatalf("pre-create business record patch should include app approval semantics: %#v", captured[1].Body)
	}
	if tasks, ok := preCreatePatchedData["approval_node_tasks"].([]interface{}); !ok || len(tasks) != 1 {
		t.Fatalf("pre-create business record patch should include node tasks: %#v", captured[1].Body)
	}
	if preCreatePatchedData["approval_workflow_id"] != "expense_approval" || preCreatePatchedData["approval_trigger_event"] != "finance.submitted" || preCreatePatchedData["approval_submitted_by"] != "alice" || preCreatePatchedData["approval_current_assignee"] != "manager" || preCreatePatchedData["approval_current_assignee_type"] != "user" || preCreatePatchedData["approval_from_status"] != "submitted" || preCreatePatchedData["approval_to_status"] != "approval_pending" {
		t.Fatalf("pre-create business record patch should include approval workflow fact fields: %#v", captured[1].Body)
	}
	if preCreatePatchedData["approval_result_summary"] != "waiting for manager" || preCreatePatchedData["approval_primary_artifact"] != "receipt.pdf" || preCreatePatchedData["approval_output_count"] != float64(1) || preCreatePatchedData["approval_artifact_count"] != float64(1) {
		t.Fatalf("pre-create business record patch should include approval result summary fields: %#v", captured[1].Body)
	}
	if payload, ok := preCreatePatchedData["approval_result_payload"].(map[string]interface{}); !ok || payload["business_record"] == nil {
		t.Fatalf("pre-create business record patch should include full result payload: %#v", captured[1].Body)
	}
	if outputs, ok := preCreatePatchedData["approval_outputs"].([]interface{}); !ok || len(outputs) != 1 {
		t.Fatalf("pre-create business record patch should include full outputs: %#v", captured[1].Body)
	}
	if artifacts, ok := preCreatePatchedData["approval_artifacts"].([]interface{}); !ok || len(artifacts) != 1 {
		t.Fatalf("pre-create business record patch should include full artifacts: %#v", captured[1].Body)
	}
	if captured[2].Method != http.MethodPost || captured[2].Path != "/api/v1/data/datasets/finance.expenses/records/exp-1/approvals" {
		t.Fatalf("unexpected create request: %#v", captured[2])
	}
	if captured[2].Body["workflow_instance_id"] != "appr-1" || fmt.Sprint(captured[2].Body["workflow_node_ids"]) != "[manager_review finance_review]" || captured[2].Body["current_node_status"] != "waiting" || captured[2].Body["workflow_version"] != "2.1.0" || captured[2].Body["detail_url"] != "approval://instances/appr-1" || captured[2].Body["business_status"] != "approval_pending" || captured[2].Body["result_status"] != "pending" {
		t.Fatalf("create body missing approval link fields: %#v", captured[2].Body)
	}
	if tasks, ok := captured[2].Body["node_tasks"].([]interface{}); !ok || len(tasks) != 1 {
		t.Fatalf("create body missing node tasks: %#v", captured[2].Body)
	}
	if captured[2].Body["approval_workflow_id"] != "expense_approval" || captured[2].Body["trigger_event"] != "finance.submitted" || captured[2].Body["submitted_by"] != "alice" || captured[2].Body["current_assignee"] != "manager" || captured[2].Body["current_assignee_type"] != "user" || captured[2].Body["from_status"] != "submitted" || captured[2].Body["to_status"] != "approval_pending" {
		t.Fatalf("create body missing approval workflow fact fields: %#v", captured[2].Body)
	}
	if captured[2].Body["app_id"] != "expense" || captured[2].Body["blueprint_id"] != "expense.blueprint.v1" || captured[2].Body["object_role"] != "expense_report" {
		t.Fatalf("create body missing app approval semantics: %#v", captured[2].Body)
	}
	request, ok := captured[2].Body["request"].(map[string]interface{})
	if !ok || request["approval_instance_id"] != "appr-1" || request["object_role"] != "expense_report" || request["blueprint_id"] != "expense.blueprint.v1" || request["detail_url"] != "approval://instances/appr-1" {
		t.Fatalf("create body request should keep app approval context: %#v", captured[2].Body)
	}
	if request["currentAssignee"] != "manager" || request["current_assignee"] != "manager" || request["currentAssigneeType"] != "user" || request["fromStatus"] != "submitted" || request["toStatus"] != "approval_pending" {
		t.Fatalf("create body request should keep approval assignee and status transition: %#v", request)
	}
	if request["workflowSkillId"] != "expense-approval-workflow" || request["workflowNodeId"] != "manager_review" || request["currentNodeStatus"] != "waiting" || fmt.Sprint(request["workflowNodeIds"]) != "[manager_review finance_review]" || request["workflowVersion"] != "2.1.0" {
		t.Fatalf("create body request should keep workflow node context: %#v", request)
	}
	if tasks, ok := request["nodeTasks"].([]interface{}); !ok || len(tasks) != 1 {
		t.Fatalf("create body request should keep node tasks: %#v", request)
	}
	if payload, ok := request["resultPayload"].(map[string]interface{}); !ok || payload["business_record"] == nil {
		t.Fatalf("create body request should keep result payload: %#v", request)
	}
	if outputs, ok := request["outputs"].([]interface{}); !ok || len(outputs) != 1 {
		t.Fatalf("create body request should keep outputs: %#v", request)
	}
	if artifacts, ok := request["artifacts"].([]interface{}); !ok || len(artifacts) != 1 {
		t.Fatalf("create body request should keep artifacts: %#v", request)
	}
	if payload, ok := captured[2].Body["result_payload"].(map[string]interface{}); !ok || payload["business_record"] == nil {
		t.Fatalf("create body missing result payload: %#v", captured[2].Body)
	}
	if created["approval_id"] != "approval-remote-1" {
		t.Fatalf("create sync should expose remote approval id: %#v", created)
	}
	stored, err := app.ListMaclawAppApprovalInstances("expense", "all", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances after create sync error = %v", err)
	}
	if len(stored) != 1 || stored[0].ApprovalID != "approval-remote-1" || stored[0].RecordApprovalID != "approval-remote-1" || stored[0].ObjectRole != "expense_report" || stored[0].WorkflowVersion != "2.1.0" || stored[0].DetailURL != "approval://instances/appr-1" {
		t.Fatalf("create sync should persist remote approval context: %#v", stored)
	}
	if outputs, ok := captured[2].Body["outputs"].([]interface{}); !ok || len(outputs) != 1 {
		t.Fatalf("create body missing outputs: %#v", captured[2].Body)
	}
	if artifacts, ok := captured[2].Body["artifacts"].([]interface{}); !ok || len(artifacts) != 1 {
		t.Fatalf("create body missing artifacts: %#v", captured[2].Body)
	}
	if captured[3].Method != http.MethodPost || captured[3].Path != "/api/v1/data/approvals/approval-remote-1/review" {
		t.Fatalf("unexpected review request: %#v", captured[3])
	}
	if captured[3].Body["decision"] != "approved" || captured[3].Body["workflow_decision_id"] != "decision-1" || captured[3].Body["detail_url"] != "approval://instances/appr-1" || captured[3].Body["workflow_version"] != "2.1.0" || captured[3].Body["business_status"] != "approved" || captured[3].Body["result_status"] != "approved" {
		t.Fatalf("review body missing decision fields: %#v", captured[3].Body)
	}
	if captured[3].Body["workflow_instance_id"] != "appr-1" || captured[3].Body["workflow_node_id"] != "finance_review" || fmt.Sprint(captured[3].Body["workflow_node_ids"]) != "[finance_review legal_review]" || captured[3].Body["current_node_status"] != "completed" || captured[3].Body["current_assignee"] != "finance_queue" || captured[3].Body["current_assignee_type"] != "queue" || captured[3].Body["from_status"] != "approval_pending" || captured[3].Body["to_status"] != "approved" {
		t.Fatalf("review body missing approval workflow transition fields: %#v", captured[3].Body)
	}
	if tasks, ok := captured[3].Body["node_tasks"].([]interface{}); !ok || len(tasks) != 1 {
		t.Fatalf("review body missing node tasks: %#v", captured[3].Body)
	}
	if captured[3].Body["result_payload"] == nil || captured[3].Body["outputs"] == nil || captured[3].Body["artifacts"] == nil {
		t.Fatalf("review body missing result package: %#v", captured[3].Body)
	}
	if captured[4].Method != http.MethodGet || captured[4].Path != "/api/v1/data/datasets/finance.expenses/records/exp-1" {
		t.Fatalf("unexpected business record get request: %#v", captured[4])
	}
	if captured[5].Method != http.MethodPatch || captured[5].Path != "/api/v1/data/datasets/finance.expenses/records/exp-1" {
		t.Fatalf("unexpected business record patch request: %#v", captured[5])
	}
	patchedData, ok := captured[5].Body["data"].(map[string]interface{})
	if !ok || patchedData["amount"] != float64(1200) || patchedData["status"] != "approved" || patchedData["payment_status"] != "pending_payment" {
		t.Fatalf("business record patch should merge existing data with approval result: %#v", captured[5].Body)
	}
	if patchedData["app_id"] != "expense" || patchedData["blueprint_id"] != "expense.blueprint.v1" || patchedData["object_role"] != "expense_report" || patchedData["approval_lane"] != "handled" || patchedData["approval_status"] != "approved" || patchedData["approval_detail_url"] != "approval://instances/appr-1" || patchedData["approval_current_node"] != "finance_review" || patchedData["approval_current_node_status"] != "completed" || fmt.Sprint(patchedData["approval_current_nodes"]) != "[finance_review legal_review]" || fmt.Sprint(patchedData["workflow_node_ids"]) != "[finance_review legal_review]" || patchedData["workflow_version"] != "2.1.0" {
		t.Fatalf("business record patch should preserve app approval semantics after workflow result patch: %#v", captured[5].Body)
	}
	if tasks, ok := patchedData["approval_node_tasks"].([]interface{}); !ok || len(tasks) != 1 {
		t.Fatalf("business record patch should preserve node tasks after workflow result patch: %#v", captured[5].Body)
	}
	if patchedData["approval_workflow_id"] != "expense_approval" || patchedData["approval_current_assignee"] != "finance_queue" || patchedData["approval_current_assignee_type"] != "queue" || patchedData["approval_from_status"] != "approval_pending" || patchedData["approval_to_status"] != "approved" {
		t.Fatalf("business record patch should preserve approval workflow fact fields after workflow result patch: %#v", captured[5].Body)
	}
	if patchedData["approval_result_summary"] != "approved" || patchedData["approval_primary_artifact"] != "receipt.pdf" || patchedData["approval_output_count"] != float64(1) || patchedData["approval_artifact_count"] != float64(1) {
		t.Fatalf("business record patch should preserve approval result summary fields after workflow result patch: %#v", captured[5].Body)
	}
	if payload, ok := patchedData["approval_result_payload"].(map[string]interface{}); !ok || payload["business_record"] == nil {
		t.Fatalf("business record patch should preserve full result payload after workflow result patch: %#v", captured[5].Body)
	}
	if outputs, ok := patchedData["approval_outputs"].([]interface{}); !ok || len(outputs) != 1 {
		t.Fatalf("business record patch should preserve full outputs after workflow result patch: %#v", captured[5].Body)
	}
	if artifacts, ok := patchedData["approval_artifacts"].([]interface{}); !ok || len(artifacts) != 1 {
		t.Fatalf("business record patch should preserve full artifacts after workflow result patch: %#v", captured[5].Body)
	}
	handled, err := app.ListMaclawAppApprovalInstances("expense", "handled", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances after review sync error = %v", err)
	}
	if len(handled) != 1 || handled[0].ApprovalID != "approval-remote-1" || handled[0].WorkflowDecisionID != "decision-1" || handled[0].CurrentNodeStatus != "completed" || len(handled[0].NodeTasks) != 1 || handled[0].BusinessStatus != "approved" || handled[0].ResultStatus != "approved" || handled[0].FromStatus != "approval_pending" || handled[0].ToStatus != "approved" {
		t.Fatalf("review sync should persist final approval fields: %#v", handled)
	}
	if payload := handled[0].ResultPayload; payload["business_record"] == nil || len(handled[0].Outputs) != 1 || handled[0].Outputs[0].Title != "Summary" || len(handled[0].Artifacts) != 1 || handled[0].Artifacts[0].Name != "receipt.pdf" {
		t.Fatalf("review sync should persist final result package: %#v", handled[0])
	}
}

func TestSyncMaclawAppApprovalInstanceToDataSrvFindsRemoteApprovalID(t *testing.T) {
	type capturedRequest struct {
		Method string
		Path   string
		Query  string
		Body   map[string]interface{}
	}
	captured := []capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := capturedRequest{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		captured = append(captured, item)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/approvals":
			_, _ = w.Write([]byte(`{"items":[{"id":"approval-remote-7","status":"pending"}],"limit":1}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/approvals/approval-remote-7/review":
			_, _ = w.Write([]byte(`{"id":"approval-remote-7","status":"approved"}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "alice", Role: "approver"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}
	base := maclawAppApprovalInstance{
		AppID:           "expense",
		BlueprintID:     "expense.blueprint.v1",
		DatasetID:       "finance.expenses",
		ObjectRole:      "expense_report",
		InstanceID:      "appr-7",
		Title:           "Expense approval",
		Lane:            "handled",
		Status:          "approved",
		CurrentNode:     "completed",
		Owner:           "alice",
		Approver:        "manager",
		Result:          "approved",
		WorkflowSkillID: "expense-approval-workflow",
	}

	reviewed, err := app.SyncMaclawAppApprovalInstanceToDataSrv(maclawAppApprovalDataSrvSyncInput{RecordID: "exp-7", Instance: base})
	if err != nil {
		t.Fatalf("SyncMaclawAppApprovalInstanceToDataSrv lookup review error = %v", err)
	}
	if reviewed["synced"] != true || reviewed["action"] != "review_record_approval" || reviewed["approval_id"] != "approval-remote-7" {
		t.Fatalf("unexpected lookup review result: %#v", reviewed)
	}
	if len(captured) != 2 {
		t.Fatalf("captured %d requests, want 2: %#v", len(captured), captured)
	}
	if captured[0].Method != http.MethodGet || captured[0].Path != "/api/v1/data/approvals" || !strings.Contains(captured[0].Query, "workflow_instance_id=appr-7") || !strings.Contains(captured[0].Query, "status=pending") || !strings.Contains(captured[0].Query, "app_id=expense") || !strings.Contains(captured[0].Query, "object_role=expense_report") {
		t.Fatalf("unexpected lookup request: %#v", captured[0])
	}
	if captured[1].Method != http.MethodPost || captured[1].Path != "/api/v1/data/approvals/approval-remote-7/review" || captured[1].Body["decision"] != "approved" {
		t.Fatalf("unexpected review request: %#v", captured[1])
	}
	stored, err := app.ListMaclawAppApprovalInstances("expense", "all", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances after lookup review error = %v", err)
	}
	if len(stored) != 1 || stored[0].ApprovalID != "approval-remote-7" || stored[0].DatasetID != "finance.expenses" || stored[0].ObjectRole != "expense_report" {
		t.Fatalf("lookup review should persist remote approval context: %#v", stored)
	}
}

func TestSyncMaclawAppApprovalInstanceToDataSrvResolvesObjectRole(t *testing.T) {
	type capturedRequest struct {
		Method string
		Path   string
		Body   map[string]interface{}
	}
	captured := []capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := capturedRequest{Method: r.Method, Path: r.URL.Path}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		captured = append(captured, item)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/object-roles/resolve" {
			_, _ = w.Write([]byte(`{"object_role":"expense_report","dataset_id":"finance.expenses","initialized":true,"business_object":{"object_role":"expense_report","dataset_id":"finance.expenses","initialized":true}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"approval-remote-2","ok":true}`))
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "alice", Role: "approver"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}
	base := maclawAppApprovalInstance{
		AppID:           "expense-approval",
		InstanceID:      "appr-2",
		Title:           "Expense approval",
		Lane:            "pending_my_approval",
		Status:          "pending",
		CurrentNode:     "manager_review",
		Owner:           "alice",
		Approver:        "manager",
		Result:          "waiting",
		WorkflowSkillID: "expense-approval-workflow",
		BusinessStatus:  "approval_pending",
		ResultStatus:    "pending",
		ResultPayload:   map[string]any{"business_record": map[string]any{"id": "exp-2"}},
	}
	created, err := app.SyncMaclawAppApprovalInstanceToDataSrv(maclawAppApprovalDataSrvSyncInput{ObjectRole: "expense_report", RecordID: "exp-2", Instance: base})
	if err != nil {
		t.Fatalf("SyncMaclawAppApprovalInstanceToDataSrv object role error = %v", err)
	}
	if created["synced"] != true || created["action"] != "create_record_approval" || created["dataset_id"] != "finance.expenses" {
		t.Fatalf("unexpected object-role sync result: %#v", created)
	}
	if len(captured) != 4 {
		t.Fatalf("captured %d requests, want 4: %#v", len(captured), captured)
	}
	if captured[0].Method != http.MethodPost || captured[0].Path != "/api/v1/data/object-roles/resolve" {
		t.Fatalf("unexpected resolver request: %#v", captured[0])
	}
	if got := strings.TrimSpace(asTestString(captured[0].Body["object_role"])); got != "expense_report" {
		t.Fatalf("resolver object_role = %q; body=%#v", got, captured[0].Body)
	}
	if got := strings.TrimSpace(asTestString(captured[0].Body["app_id"])); got != "expense-approval" {
		t.Fatalf("resolver app_id = %q; body=%#v", got, captured[0].Body)
	}
	if got, ok := captured[0].Body["require_initialized"].(bool); !ok || !got {
		t.Fatalf("resolver should require initialized dataset: %#v", captured[0].Body)
	}
	if captured[1].Method != http.MethodGet || captured[1].Path != "/api/v1/data/datasets/finance.expenses/records/exp-2" {
		t.Fatalf("unexpected pre-create business record get request: %#v", captured[1])
	}
	if captured[2].Method != http.MethodPatch || captured[2].Path != "/api/v1/data/datasets/finance.expenses/records/exp-2" {
		t.Fatalf("unexpected pre-create business record patch request: %#v", captured[2])
	}
	patchedData, ok := captured[2].Body["data"].(map[string]interface{})
	if !ok || patchedData["workflow_instance_id"] != "appr-2" || patchedData["business_status"] != "approval_pending" {
		t.Fatalf("pre-create business record patch should include approval context: %#v", captured[2].Body)
	}
	if captured[3].Method != http.MethodPost || captured[3].Path != "/api/v1/data/datasets/finance.expenses/records/exp-2/approvals" {
		t.Fatalf("unexpected approval create request: %#v", captured[3])
	}
}
