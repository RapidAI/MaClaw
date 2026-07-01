package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	maclawapptest "github.com/RapidAI/CodeClaw/internal/testfixtures"
)

func sha256HexForTest(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:])
}
func maclawAppPackageWithCurrentDefinitionHashes(t *testing.T, packageJSON string) string {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(packageJSON), &doc); err != nil {
		t.Fatalf("decode maclaw app package fixture: %v", err)
	}
	var entries []parsedMaclawAppEntry
	if stringMapValue(doc, "schema") == "maclaw.app.v1" {
		entry, err := parseMaclawAppEntryFromMap(doc, "maclaw app", map[string]struct{}{})
		if err != nil {
			t.Fatalf("parse maclaw app fixture: %v", err)
		}
		entries = []parsedMaclawAppEntry{entry}
	} else {
		var err error
		entries, err = parseMaclawAppPackageEntriesFromMap(doc, false)
		if err != nil {
			t.Fatalf("parse maclaw app package fixture: %v", err)
		}
	}
	for _, entry := range entries {
		governance := anyMap(entry.App["governance"])
		if governance == nil {
			continue
		}
		testEvidence := anyMap(governance["testEvidence"])
		if testEvidence == nil {
			testEvidence = anyMap(governance["test_evidence"])
		}
		if testEvidence == nil {
			continue
		}
		testEvidence["definitionHash"] = maclawAppDefinitionFingerprintForEntry(entry)
		if fingerprint := maclawAppCurrentWorkspaceLayoutFingerprint(entry, governance); fingerprint != "" {
			testEvidence["workspaceLayoutFingerprint"] = fingerprint
		}
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("encode maclaw app package fixture: %v", err)
	}
	return string(data)
}

func assertSubmitMaclawAppPackageBlocked(t *testing.T, app *App, packageJSON, want string) {
	t.Helper()
	_, err := app.SubmitMaclawAppPackage(packageJSON)
	if err == nil {
		t.Fatalf("expected SubmitMaclawAppPackage to block unready package")
	}
	if want != "" && !strings.Contains(err.Error(), want) {
		t.Fatalf("expected submit error to contain %q, got %v", want, err)
	}
	if _, readErr := os.ReadFile(app.maclawAppSubmissionQueuePath()); readErr == nil {
		t.Fatalf("blocked submission should not create a queue file")
	} else if !os.IsNotExist(readErr) {
		t.Fatalf("read queue after blocked submission: %v", readErr)
	}
}
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

func TestSubmitMaclawAppPackageQueuesLocalSubmission(t *testing.T) {
	tmpHome := t.TempDir()
	for _, id := range []string{"contract-super-app", "contract-workflow"} {
		skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", id)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatalf("MkdirAll skillDir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# "+id+"\n"), 0o644); err != nil {
			t.Fatalf("WriteFile skill.md: %v", err)
		}
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{
		{Name: "contract-super-app", SkillDir: filepath.Join(tmpHome, ".maclaw", "data", "skills", "contract-super-app"), Status: "active", HubVersion: "1.0.0"},
		{Name: "contract-workflow", SkillDir: filepath.Join(tmpHome, ".maclaw", "data", "skills", "contract-workflow"), Status: "active", HubVersion: "2.0.0"},
	}
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
				"id": "local-contract",
				"name": "Contract",
				"kind": "enterprise_normal_app",
				"binding": {
					"appSkill": {"id": "contract-super-app", "version": "1.0.0"},
					"dependencies": {"skills": [{"id": "contract-workflow", "version": "2.0.0", "kind": "workflow_skill", "required": true, "source": "market"}]}
				},
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"business_workspace", "template":"classic_split", "regionCount":4},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content"]},
					"dependencyVerification": {"schema":"maclaw.app.install_plan.v1", "dependencyCount":2, "hasMissingRequired":false, "hasBlockingDependency":false, "dependencies":[{"id":"contract-super-app", "kind":"app_skill", "required":true, "installed":true, "health":"ready", "action":"skip"}, {"id":"contract-workflow", "kind":"workflow_skill", "version":"2.0.0", "required":true, "installed":true, "health":"ready", "action":"skip"}]},
					"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-contract","sampleInput":{"sample":true},"expectedOutput":{"content":"ok"},"requiredRoles":["tester"],"requiredScopes":["app.run"],"riskLevel":"low"}, "testProtocolFingerprint":"proto-contract", "runId":"run-contract", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"content":"ok"}, "outputs":[{"kind":"content", "text":"ok"}], "resultCoverage":{"ok":true, "primary":"content", "coveredTypes":["content"], "missingTypes":[]}}
				}
			}
		}]
	}`

	pkg = maclawAppPackageWithCurrentDefinitionHashes(t, pkg)
	result, err := app.SubmitMaclawAppPackage(pkg)
	if err != nil {
		t.Fatalf("SubmitMaclawAppPackage error: %v", err)
	}
	if result["status"] != "submitted" || result["channel"] != "local" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result["package_sha"] == "" || result["package_sha256"] == "" || result["package_sha"] != result["package_sha256"] || result["package_bytes"].(int) <= 0 {
		t.Fatalf("expected package fingerprint in result: %#v", result)
	}
	if result["dependency_count"] != 2 {
		t.Fatalf("expected dependency count in result: %#v", result)
	}
	submissionID, _ := result["submission_id"].(string)
	if !strings.HasPrefix(submissionID, "local-review-local-contract-") {
		t.Fatalf("submission_id = %q", submissionID)
	}

	data, err := os.ReadFile(app.maclawAppSubmissionQueuePath())
	if err != nil {
		t.Fatalf("read queue: %v", err)
	}
	var queue maclawAppSubmissionQueue
	if err := json.Unmarshal(data, &queue); err != nil {
		t.Fatalf("decode queue: %v", err)
	}
	if queue.Schema != "maclaw.app.submissions.v1" || len(queue.Submissions) != 1 {
		t.Fatalf("unexpected queue: %#v", queue)
	}
	if queue.Submissions[0].SubmissionID != submissionID || queue.Submissions[0].AppIDs[0] != "local-contract" {
		t.Fatalf("unexpected record: %#v", queue.Submissions[0])
	}
	if queue.Submissions[0].AppNames[0] != "Contract" || queue.Submissions[0].PackageSHA == "" || queue.Submissions[0].PackageSize <= 0 {
		t.Fatalf("expected record audit metadata: %#v", queue.Submissions[0])
	}
	if len(queue.Submissions[0].Events) != 1 || queue.Submissions[0].Events[0].Status != "submitted" {
		t.Fatalf("expected initial submission event: %#v", queue.Submissions[0].Events)
	}
	if len(queue.Submissions[0].Dependencies) != 2 || queue.Submissions[0].Dependencies[0].ID != "contract-super-app" || queue.Submissions[0].Dependencies[1].ID != "contract-workflow" {
		t.Fatalf("expected dependency audit metadata: %#v", queue.Submissions[0].Dependencies)
	}
	if queue.Submissions[0].Dependencies[1].Kind != "workflow_skill" || queue.Submissions[0].Dependencies[1].Source != "market" || queue.Submissions[0].Dependencies[1].AppIDs[0] != "local-contract" {
		t.Fatalf("expected workflow dependency audit detail: %#v", queue.Submissions[0].Dependencies[1])
	}

	summaries, err := app.ListMaclawAppPackageSubmissions(10)
	if err != nil {
		t.Fatalf("ListMaclawAppPackageSubmissions error: %v", err)
	}
	if len(summaries) != 1 || summaries[0].SubmissionID != submissionID || summaries[0].AppIDs[0] != "local-contract" {
		t.Fatalf("unexpected summaries: %#v", summaries)
	}
	if summaries[0].AppNames[0] != "Contract" || len(summaries[0].PackageSHA) != 64 || summaries[0].PackageSHA256 != summaries[0].PackageSHA || summaries[0].PackageSize <= 0 {
		t.Fatalf("expected summary audit metadata: %#v", summaries[0])
	}
	summaryJSON, err := json.Marshal(summaries[0])
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	var summaryFields map[string]any
	if err := json.Unmarshal(summaryJSON, &summaryFields); err != nil {
		t.Fatalf("decode summary json: %v", err)
	}
	if summaryFields["package_sha"] != summaries[0].PackageSHA || summaryFields["package_sha256"] != summaries[0].PackageSHA {
		t.Fatalf("summary json should expose both package sha aliases: %s", summaryJSON)
	}
	if summaries[0].EventCount != 1 || summaries[0].LastEventAt == "" {
		t.Fatalf("expected summary event metadata: %#v", summaries[0])
	}
	if len(summaries[0].Dependencies) != 2 || summaries[0].Dependencies[1].ID != "contract-workflow" {
		t.Fatalf("expected summary dependency audit metadata: %#v", summaries[0].Dependencies)
	}
}
func TestMaclawAppSubmissionSummaryIncludesReviewEvidence(t *testing.T) {
	record := maclawAppSubmissionRecord{
		SubmissionID: "local-review-expense-approval-1",
		SubmittedAt:  "2026-06-30T10:00:00Z",
		Status:       "submitted",
		Channel:      "local",
		AppIDs:       []string{"expense-approval"},
		AppNames:     []string{"Expense Approval"},
		Dependencies: []maclawAppInstallPlanDependency{{ID: "expense-workflow", Kind: "workflow_skill", Version: "2.1.0", Required: true, Installed: true, Health: "ready", Action: "skip", AppIDs: []string{"expense-approval"}}},
		Package: map[string]any{
			"schema":        "maclaw.app.pack.v1",
			"privateMarker": "x_maclaw_apps",
			"apps": []any{map[string]any{
				"schema":        "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"app": map[string]any{
					"id":           "expense-approval",
					"name":         "Expense Approval",
					"kind":         "enterprise_approval_app",
					"ui":           map[string]any{"schema": "maclaw.app.ui.v1", "generated": true, "entry": "approval_workspace", "layouts": map[string]any{"approval_workspace": map[string]any{"template": "classic_split", "density": "compact", "primaryRegion": "left", "outputRegion": "right", "regions": []any{map[string]any{"id": "request_form", "role": "input", "placement": "left"}, map[string]any{"id": "approval_inbox", "role": "instance_list", "placement": "center"}, map[string]any{"id": "approval_detail", "role": "detail", "placement": "center"}, map[string]any{"id": "result_panel", "role": "output", "placement": "right"}}, "studio": map[string]any{"savedInManifest": true, "editable": true, "updatedBy": "app_studio"}}}},
					"dependencies": map[string]any{"skills": []any{map[string]any{"id": "expense-workflow", "kind": "workflow_skill", "version": "2.1.0", "required": true, "source": "market"}}},
					"binding": map[string]any{
						"workflowContract": map[string]any{
							"schema":                      "maclaw.app.workflow_contract.v1",
							"requires_approval_instance":  true,
							"requires_progress_instances": true,
						},
					},
					"governance": map[string]any{
						"workspaceLayout":        map[string]any{"template": "left_nav", "entry": "approval_workspace", "studio": map[string]any{"savedInManifest": true, "editable": true, "updatedBy": "app_studio"}},
						"resultContract":         map[string]any{"schema": "maclaw.app.result.v1", "primary": "approval_result", "types": []any{"approval_result", "business_status", "content"}},
						"dependencyVerification": map[string]any{"dependency_count": 1, "has_blocking_dependency": false},
						"datasrv_registration":   map[string]any{"status": "partial", "eligible_count": 2, "synced_count": 1},
						"testEvidence": map[string]any{
							"runId":          "run-expense-review",
							"verifiedAt":     "2026-06-30T10:01:00Z",
							"testProtocol":   map[string]any{"schema": "maclaw.app.test_protocol.v1", "fingerprint": "proto-expense-review"},
							"resultCoverage": map[string]any{"ok": true, "primary": "approval_result", "coveredTypes": []any{"approval_result", "business_status"}, "missingTypes": []any{}},
							"outputs":        []any{map[string]any{"kind": "approval_result", "status": "approved"}},
							"artifacts":      []any{map[string]any{"id": "approval-report", "name": "approval-report.pdf"}},
							"approvalInstance": map[string]any{
								"approval_id":      "approval-review-1",
								"status":           "approved",
								"workflow_node_id": "expense.result",
							},
							"progress_instances": []any{map[string]any{
								"approval_id":      "approval-review-1",
								"status":           "pending",
								"workflow_node_id": "manager.approval",
							}},
							"approvalViews": map[string]any{"my_requests": true, "pending_my_approval": true},
						},
					},
				},
			}},
		},
	}

	summary := record.maclawAppSubmissionSummary()
	reviewEvidence := anyMap(summary.ReviewEvidence["expense-approval"])
	if len(reviewEvidence) == 0 {
		t.Fatalf("expected review evidence in summary: %#v", summary.ReviewEvidence)
	}
	if reviewEvidence["app_kind"] != "enterprise_approval_app" || reviewEvidence["has_approval_instance"] != true || reviewEvidence["approval_status"] != "approved" {
		t.Fatalf("approval review evidence mismatch: %#v", reviewEvidence)
	}
	if reviewEvidence["progress_count"] != 1 || reviewEvidence["has_dependency_verification"] != true || reviewEvidence["dependency_count"] != 1 {
		t.Fatalf("progress/dependency review evidence mismatch: %#v", reviewEvidence)
	}
	if reviewEvidence["has_workspace_layout"] != true || reviewEvidence["workspace_template"] != "classic_split" || reviewEvidence["workspace_saved_in_manifest"] != true || reviewEvidence["workspace_studio_editable"] != true || reviewEvidence["workspace_updated_by"] != "app_studio" || reviewEvidence["datasrv_registration_status"] != "partial" {
		t.Fatalf("layout/DataSrv review evidence mismatch: %#v", reviewEvidence)
	}
	if reviewEvidence["has_workflow_contract"] != true || reviewEvidence["workflow_contract_version"] != "maclaw.app.workflow_contract.v1" {
		t.Fatalf("workflow contract review evidence mismatch: %#v", reviewEvidence)
	}
	if reviewEvidence["has_result_contract"] != true || reviewEvidence["result_contract_primary"] != "approval_result" || reviewEvidence["result_contract_type_count"] != 3 {
		t.Fatalf("result contract review evidence mismatch: %#v", reviewEvidence)
	}
	if reviewEvidence["has_test_protocol"] != true || reviewEvidence["test_protocol_fingerprint"] != "proto-expense-review" || reviewEvidence["result_coverage_ok"] != true || reviewEvidence["result_coverage_primary"] != "approval_result" || reviewEvidence["result_coverage_covered_count"] != 2 || reviewEvidence["result_coverage_missing_count"] != 0 || reviewEvidence["output_count"] != 1 || reviewEvidence["artifact_count"] != 1 {
		t.Fatalf("test protocol/result coverage review evidence mismatch: %#v", reviewEvidence)
	}
	summaryJSON, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(summaryJSON, &fields); err != nil {
		t.Fatalf("unmarshal summary: %v", err)
	}
	jsonReviewEvidence := anyMap(anyMap(fields["review_evidence"])["expense-approval"])
	if jsonReviewEvidence["approval_id"] != "approval-review-1" || jsonReviewEvidence["progress_count"] != float64(1) || jsonReviewEvidence["workspace_saved_in_manifest"] != true || jsonReviewEvidence["workspace_updated_by"] != "app_studio" || jsonReviewEvidence["result_contract_primary"] != "approval_result" || jsonReviewEvidence["test_protocol_fingerprint"] != "proto-expense-review" || jsonReviewEvidence["result_coverage_primary"] != "approval_result" || jsonReviewEvidence["result_coverage_covered_count"] != float64(2) {
		t.Fatalf("summary JSON should expose review_evidence: %s", summaryJSON)
	}
}
func TestSubmitMaclawAppPackagePersistsNormalizedWorkspaceLayout(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pkg := `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "studio-layout-app",
				"name": "Studio Layout App",
				"kind": "enterprise_normal_app",
				"ui": {
					"schema": "maclaw.app.ui.v1",
					"entry": "business_workspace",
					"layouts": {
						"business_workspace": {
							"template": "dashboard",
							"density": "spacious",
							"primaryRegion": "center",
							"outputRegion": "right",
							"studio": {"savedInManifest": true, "designerVersion": "2026.06"},
							"regions": [
								{"id":"operation_form","role":"input","placement":"left"},
								{"id":"output_panel","role":"output","placement":"right","visible":false}
							]
						}
					}
				},
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"business_workspace", "template":"dashboard", "density":"spacious", "primaryRegion":"center", "outputRegion":"right", "regionCount":3, "regions":[{"id":"operation_form","role":"input","placement":"left"},{"id":"record_grid","role":"record_list","placement":"center"},{"id":"output_panel","role":"output","placement":"right"}]},
						"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types": ["content"]},
					"testEvidence": {"testProtocol": {"schema":"maclaw.app.test_protocol.v1", "fingerprint":"proto-studio-layout", "sampleInput": {"sample":true}, "expectedOutput": {"content":"ok"}, "requiredRoles": ["tester"], "requiredScopes": ["app.run"], "riskLevel":"low"}, "testProtocolFingerprint":"proto-studio-layout", "runId":"run-studio-layout", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload": {"content":"ok"}, "outputs": [{"kind":"content", "text":"ok"}], "resultCoverage": {"ok":true, "primary":"content", "coveredTypes": ["content"], "missingTypes": []}}
				}
			}
		}, {
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {"id": "default-tool-layout", "name": "Default Tool", "kind": "tool_app", "governance": {"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "template":"document_workspace", "regionCount":2, "regions":[{"id":"input","role":"input","placement":"left"},{"id":"output","role":"output","placement":"right"}]}, "resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content"]}, "testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-default-tool-layout","sampleInput":{"sample":true},"expectedOutput":{"content":"ok"},"requiredRoles":["tester"],"requiredScopes":["app.run"],"riskLevel":"low"}, "testProtocolFingerprint":"proto-default-tool-layout", "runId":"run-default-tool-layout", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"content":"ok"}, "outputs":[{"kind":"content", "text":"ok"}], "resultCoverage":{"ok":true, "primary":"content", "coveredTypes":["content"], "missingTypes":[]}}}}
		}]
	}`

	pkg = maclawAppPackageWithCurrentDefinitionHashes(t, pkg)
	result, err := app.SubmitMaclawAppPackage(pkg)
	if err != nil {
		t.Fatalf("SubmitMaclawAppPackage error: %v", err)
	}
	detail, err := app.GetMaclawAppPackageSubmission(result["submission_id"].(string))
	if err != nil {
		t.Fatalf("GetMaclawAppPackageSubmission error: %v", err)
	}
	apps, _ := detail.Package["apps"].([]any)
	if len(apps) != 2 {
		t.Fatalf("expected two queued package apps, got %#v", detail.Package["apps"])
	}

	first := anyMap(apps[0])
	firstApp := anyMap(first["app"])
	ui := anyMap(firstApp["ui"])
	layout := anyMap(anyMap(ui["layouts"])["business_workspace"])
	if ui["schema"] != "maclaw.app.ui.v1" || ui["entry"] != "business_workspace" {
		t.Fatalf("expected normalized studio ui in queued package: %#v", ui)
	}
	if layout["template"] != "dashboard" || layout["density"] != "spacious" || layout["type"] != "split_view" {
		t.Fatalf("expected custom layout fields plus backend defaults: %#v", layout)
	}
	regions := anySlice(layout["regions"])
	if len(regions) != 2 || anyMap(regions[1])["visible"] != false {
		t.Fatalf("expected hidden region visibility to survive queueing: %#v", layout["regions"])
	}
	studio := anyMap(layout["studio"])
	if studio == nil || studio["savedInManifest"] != true || studio["designerVersion"] != "2026.06" {
		t.Fatalf("expected studio layout metadata to survive queueing: %#v", layout["studio"])
	}
	submissionEvidence := anyMap(result["submission_evidence"])
	studioInstallEvidence := anyMap(submissionEvidence["studio-layout-app"])
	studioWorkspaceLayout := anyMap(studioInstallEvidence["workspace_layout"])
	studioWorkspaceStudio := anyMap(studioWorkspaceLayout["studio"])
	if studioWorkspaceStudio == nil || studioWorkspaceStudio["savedInManifest"] != true || studioWorkspaceLayout["studio_saved_in_manifest"] != true {
		t.Fatalf("submission evidence should expose App Studio saved layout metadata: %#v", studioWorkspaceLayout)
	}
	reviewEvidence := anyMap(result["review_evidence"])
	studioReviewEvidence := anyMap(reviewEvidence["studio-layout-app"])
	if studioReviewEvidence["workspace_saved_in_manifest"] != true {
		t.Fatalf("review evidence should expose App Studio saved layout state: %#v", studioReviewEvidence)
	}

	second := anyMap(apps[1])
	secondApp := anyMap(second["app"])
	defaultUI := anyMap(secondApp["ui"])
	defaultLayout := anyMap(anyMap(defaultUI["layouts"])["tool_workspace"])
	if defaultUI["entry"] != "tool_workspace" || defaultLayout["template"] != "document_workspace" {
		t.Fatalf("expected default tool workspace layout in queued package: %#v", defaultUI)
	}
}
func TestSubmitMaclawAppPackageRecordsGovernanceReviewIssues(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pkg := `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {"id": "missing-governance", "name": "Missing Governance", "kind": "tool_app"}
		}]
	}`

	result := map[string]any{}
	assertSubmitMaclawAppPackageBlocked(t, app, pkg, "apps[0].app.governance.testEvidence")
	return
	if result["review_issue_count"] != 3 {
		t.Fatalf("expected three local governance issues, got %#v", result)
	}
	detail, err := app.GetMaclawAppPackageSubmission(result["submission_id"].(string))
	if err != nil {
		t.Fatalf("GetMaclawAppPackageSubmission error: %v", err)
	}
	if len(detail.ReviewIssues) != 3 {
		t.Fatalf("expected three review issues in durable queue: %#v", detail.ReviewIssues)
	}
	paths := map[string]string{}
	for _, issue := range detail.ReviewIssues {
		paths[issue.Path] = issue.Severity
	}
	if paths["apps[0].app.governance"] != "warning" || paths["apps[0].app.governance.testEvidence"] != "error" || paths["apps[0].app.governance.resultContract"] != "error" {
		t.Fatalf("unexpected governance review issues: %#v", detail.ReviewIssues)
	}
}

func TestSubmitMaclawAppPackageFlagsWorkspaceLayoutMissingRequiredRegionRole(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pkg := `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "missing-output-region",
				"name": "Missing Output Region",
				"kind": "tool_app",
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "template":"document_workspace", "regionCount":1, "regions":[{"id":"file_queue", "role":"input", "placement":"left"}]},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content"]},
					"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-layout","sampleInput":{"sample":true},"expectedOutput":{"content":"ok"},"requiredRoles":["tester"],"requiredScopes":["app.run"],"riskLevel":"low"}, "testProtocolFingerprint":"proto-layout", "runId":"run-layout", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"content":"ok"}}
				}
			}
		}]
	}`

	var rawPackage map[string]any
	if err := json.Unmarshal([]byte(pkg), &rawPackage); err != nil {
		t.Fatalf("decode package: %v", err)
	}
	rawApps := rawPackage["apps"].([]any)
	rawEntry := rawApps[0].(map[string]any)
	rawApp := rawEntry["app"].(map[string]any)
	rawGovernance := rawApp["governance"].(map[string]any)
	if maclawAppWorkspaceLayoutHasRequiredRoles(rawGovernance["workspaceLayout"].(map[string]any), "tool_app") {
		t.Fatalf("expected raw workspace layout to miss required output role")
	}
	pkg = maclawAppPackageWithCurrentDefinitionHashes(t, pkg)
	result := map[string]any{}
	assertSubmitMaclawAppPackageBlocked(t, app, pkg, "apps[0].app.governance.workspaceLayout")
	return
	if result["review_issue_count"] != 1 {
		t.Fatalf("expected one workspace layout issue, got %#v", result)
	}
	detail, err := app.GetMaclawAppPackageSubmission(result["submission_id"].(string))
	if err != nil {
		t.Fatalf("GetMaclawAppPackageSubmission error: %v", err)
	}
	if len(detail.ReviewIssues) != 1 || detail.ReviewIssues[0].Path != "apps[0].app.governance.workspaceLayout" || !strings.Contains(detail.ReviewIssues[0].Message, "workspace layout") {
		t.Fatalf("unexpected workspace layout issue: %#v", detail.ReviewIssues)
	}
}
func TestPlanMaclawAppInstallReportsGovernanceReviewErrors(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	plan, err := app.PlanMaclawAppInstall(maclawAppPackageWithCurrentDefinitionHashes(t, `{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"installUnit": "skill",
		"app": {
			"id": "bad-layout-plan",
			"name": "Bad Layout Plan",
			"kind": "tool_app",
			"governance": {
				"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "template":"document_workspace", "regionCount":1, "regions":[{"id":"file_queue", "role":"input", "placement":"left"}]},
				"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content"]},
				"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-layout","sampleInput":{"sample":true},"expectedOutput":{"content":"ok"},"requiredRoles":["tester"],"requiredScopes":["app.run"],"riskLevel":"low"}, "testProtocolFingerprint":"proto-layout", "runId":"run-layout", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"content":"ok"}}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	if !plan.HasGovernanceReviewIssue || len(plan.GovernanceReviewIssues) != 1 {
		t.Fatalf("expected governance review issue in install plan: %#v", plan)
	}
	issue := plan.GovernanceReviewIssues[0]
	if issue.Path != "apps[0].app.governance.workspaceLayout" || !strings.Contains(issue.Message, "workspace layout") {
		t.Fatalf("unexpected governance review issue: %#v", issue)
	}
}
func TestSubmitMaclawAppPackageAcceptsCompleteGovernanceEvidence(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pkg := `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "complete-governance",
				"name": "Complete Governance",
				"kind": "tool_app",
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "template":"document_workspace", "regionCount":4},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"artifact", "types":["content", "document", "artifact"]},
					"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-basic","sampleInput":{"sample":true},"expectedOutput":{"status":"ok"},"requiredRoles":["tester"],"requiredScopes":["app.run"],"riskLevel":"low"}, "testProtocolFingerprint":"proto-basic", "runId":"run-1", "artifactPresent":true, "artifactCount":1, "verifiedAt":"2026-06-17T01:00:00Z"}
				}
			}
	}]
	}`

	pkg = maclawAppPackageWithCurrentDefinitionHashes(t, pkg)
	result, err := app.SubmitMaclawAppPackage(pkg)
	if err != nil {
		t.Fatalf("SubmitMaclawAppPackage error: %v", err)
	}
	if result["review_issue_count"] != 0 {
		t.Fatalf("expected no governance review issues, got %#v", result)
	}
	resultEvidence, ok := result["submission_evidence"].(map[string]any)
	if !ok || len(resultEvidence) != 1 {
		t.Fatalf("expected submission evidence in submit result: %#v", result["submission_evidence"])
	}
	completeEvidence, ok := resultEvidence["complete-governance"].(map[string]any)
	if !ok {
		t.Fatalf("expected complete-governance evidence: %#v", resultEvidence)
	}
	if layout := completeEvidence["workspace_layout"].(map[string]any); layout["template"] != "document_workspace" {
		t.Fatalf("expected workspace layout evidence: %#v", layout)
	}
	if resultContract := completeEvidence["result_contract"].(map[string]any); resultContract["primary"] != "artifact" {
		t.Fatalf("expected result contract evidence: %#v", resultContract)
	}
	if testEvidence := completeEvidence["test_evidence"].(map[string]any); testEvidence["runId"] != "run-1" {
		t.Fatalf("expected test evidence snapshot: %#v", testEvidence)
	}
	detail, err := app.GetMaclawAppPackageSubmission(result["submission_id"].(string))
	if err != nil {
		t.Fatalf("GetMaclawAppPackageSubmission error: %v", err)
	}
	if len(detail.ReviewIssues) != 0 {
		t.Fatalf("expected clean governance queue record: %#v", detail.ReviewIssues)
	}
	summaries, err := app.ListMaclawAppPackageSubmissions(10)
	if err != nil {
		t.Fatalf("ListMaclawAppPackageSubmissions error: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected one submission summary: %#v", summaries)
	}
	summaryEvidence := summaries[0].Evidence["complete-governance"].(map[string]any)
	if layout := summaryEvidence["workspace_layout"].(map[string]any); layout["entry"] != "tool_workspace" {
		t.Fatalf("expected summary workspace layout evidence: %#v", layout)
	}
	if testEvidence := summaryEvidence["test_evidence"].(map[string]any); testEvidence["testProtocolFingerprint"] != "proto-basic" {
		t.Fatalf("expected summary test evidence: %#v", testEvidence)
	}
}

func TestSubmitMaclawAppPackageFlagsMissingTestProtocol(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pkg := `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "missing-test-protocol",
				"name": "Missing Test Protocol",
				"kind": "tool_app",
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "template":"document_workspace", "regionCount":4},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"text", "types":["content", "text"]},
					"testEvidence":{"runId":"run-without-protocol", "testProtocolFingerprint":"proto-missing", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"text":"ok"}}
				}
			}
	}]
	}`

	pkg = maclawAppPackageWithCurrentDefinitionHashes(t, pkg)
	result := map[string]any{}
	assertSubmitMaclawAppPackageBlocked(t, app, pkg, "apps[0].app.governance.testProtocol")
	return
	if result["review_issue_count"] != 1 {
		t.Fatalf("expected one test protocol issue, got %#v", result)
	}
	detail, err := app.GetMaclawAppPackageSubmission(result["submission_id"].(string))
	if err != nil {
		t.Fatalf("GetMaclawAppPackageSubmission error: %v", err)
	}
	if len(detail.ReviewIssues) != 1 || detail.ReviewIssues[0].Path != "apps[0].app.governance.testProtocol" || !strings.Contains(detail.ReviewIssues[0].Message, "test protocol") {
		t.Fatalf("unexpected test protocol issue: %#v", detail.ReviewIssues)
	}
}
func TestSubmitMaclawAppPackageFlagsMissingDependencyVerification(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pkg := `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "missing-dependency-verification",
				"name": "Missing Dependency Verification",
				"kind": "enterprise_normal_app",
				"binding": {
					"appSkill": {"id":"customer-renewal-skill", "source":"hub"}
				},
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"business_workspace", "template":"classic_split", "regionCount":4},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"business_status", "types":["business_status", "business_record", "content"]},
					"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-basic","sampleInput":{"sample":true},"expectedOutput":{"status":"ok"},"requiredRoles":["tester"],"requiredScopes":["app.run"],"riskLevel":"low"}, "testProtocolFingerprint":"proto-basic", "runId":"run-business", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"business_status":"ready"}}
				}
			}
	}]
	}`

	pkg = maclawAppPackageWithCurrentDefinitionHashes(t, pkg)
	result := map[string]any{}
	assertSubmitMaclawAppPackageBlocked(t, app, pkg, "apps[0].app.governance.dependencyVerification")
	return
	if result["review_issue_count"] != 1 {
		t.Fatalf("expected one dependency verification issue, got %#v", result)
	}
	detail, err := app.GetMaclawAppPackageSubmission(result["submission_id"].(string))
	if err != nil {
		t.Fatalf("GetMaclawAppPackageSubmission error: %v", err)
	}
	issue := detail.ReviewIssues[0]
	if issue.Path != "apps[0].app.governance.dependencyVerification" || !strings.Contains(issue.Message, "dependency verification") {
		t.Fatalf("unexpected dependency verification issue: %#v", issue)
	}
}

func TestSubmitMaclawAppPackageFlagsStaleDependencyVerification(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pkg := `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "stale-dependency-verification",
				"name": "Stale Dependency Verification",
				"kind": "enterprise_normal_app",
				"binding": {
					"appSkill": {"id":"customer-renewal-skill", "source":"hub"}
				},
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"business_workspace", "template":"classic_split", "regionCount":4},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"business_status", "types":["business_status", "business_record", "content"]},
					"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-basic","sampleInput":{"sample":true},"expectedOutput":{"status":"ok"},"requiredRoles":["tester"],"requiredScopes":["app.run"],"riskLevel":"low"}, "testProtocolFingerprint":"proto-basic", "runId":"run-business", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"business_status":"ready"}},
					"dependencyVerification": {"schema":"maclaw.app.install_plan.v1", "dependencies":[{"id":"customer-renewal-skill", "kind":"runtime_skill", "required":true, "installed":true, "health":"ready", "action":"skip"}]}
				}
			}
	}]
	}`

	pkg = maclawAppPackageWithCurrentDefinitionHashes(t, pkg)
	result := map[string]any{}
	assertSubmitMaclawAppPackageBlocked(t, app, pkg, "authoritative dependency plan found required dependency not ready")
	return
	if result["review_issue_count"] != 1 {
		t.Fatalf("expected one authoritative dependency issue, got %#v", result)
	}
	detail, err := app.GetMaclawAppPackageSubmission(result["submission_id"].(string))
	if err != nil {
		t.Fatalf("GetMaclawAppPackageSubmission error: %v", err)
	}
	if len(detail.ReviewIssues) != 1 || detail.ReviewIssues[0].Path != "apps[0].app.governance.dependencyVerification" || !strings.Contains(detail.ReviewIssues[0].Message, "required dependency not ready") {
		t.Fatalf("unexpected authoritative dependency issue: %#v", detail.ReviewIssues)
	}
	if len(detail.Dependencies) != 1 || detail.Dependencies[0].ID != "customer-renewal-skill" || detail.Dependencies[0].Installed || detail.Dependencies[0].Action != "blocked" {
		t.Fatalf("expected backend install plan dependency state in submission: %#v", detail.Dependencies)
	}
}

func TestSubmitMaclawAppPackageScopesDependencyVerificationToCurrentApp(t *testing.T) {
	tmpHome := t.TempDir()
	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "customer-renewal-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# Customer renewal\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "customer-renewal-skill", SkillDir: skillDir, Status: "active", HubVersion: "1.0.0"}}
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
				"id": "scoped-dependency-verification",
				"name": "Scoped Dependency Verification",
				"kind": "enterprise_normal_app",
				"binding": {
					"appSkill": {"id":"customer-renewal-skill", "version":"1.0.0", "source":"hub"}
				},
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"business_workspace", "template":"classic_split", "regionCount":4},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"business_status", "types":["business_status", "business_record", "content"]},
					"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-basic","sampleInput":{"sample":true},"expectedOutput":{"business_status":"ready"},"requiredRoles":["tester"],"requiredScopes":["app.run"],"riskLevel":"low"}, "testProtocolFingerprint":"proto-basic", "runId":"run-business", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"business_status":"ready"}},
					"dependencyVerification": {
						"schema":"maclaw.app.install_plan.v1",
						"hasBlockingDependency": true,
						"hasMissingRequired": true,
						"dependencies":[
							{"id":"customer-renewal-skill", "kind":"runtime_skill", "required":true, "installed":true, "health":"ready", "action":"skip", "app_ids":["scoped-dependency-verification"]},
							{"id":"other-blocked-skill", "kind":"runtime_skill", "required":true, "installed":false, "health":"missing", "action":"blocked", "app_ids":["other-blocked-app"]}
						],
						"governanceReviewIssues":[{"path":"apps[1].app.governance.testEvidence", "severity":"error", "message":"other app missing evidence"}],
						"hasGovernanceReviewIssue": true
					}
				}
			}
		}]
	}`

	pkg = maclawAppPackageWithCurrentDefinitionHashes(t, pkg)
	result, err := app.SubmitMaclawAppPackage(pkg)
	if err != nil {
		t.Fatalf("SubmitMaclawAppPackage error: %v", err)
	}
	if result["review_issue_count"] != 0 {
		t.Fatalf("expected no dependency verification issue for foreign app dependency state, got %#v", result)
	}
	detail, err := app.GetMaclawAppPackageSubmission(result["submission_id"].(string))
	if err != nil {
		t.Fatalf("GetMaclawAppPackageSubmission error: %v", err)
	}
	if len(detail.ReviewIssues) != 0 {
		t.Fatalf("expected clean scoped dependency verification record: %#v", detail.ReviewIssues)
	}
}

func TestDependencyVerificationReviewFlagsInstallTraceFailure(t *testing.T) {
	entry := parsedMaclawAppEntry{
		ID: "dependency-install-trace-failure",
		App: map[string]any{
			"id":   "dependency-install-trace-failure",
			"kind": "enterprise_normal_app",
			"binding": map[string]any{
				"appSkill": map[string]any{"id": "customer-renewal-skill", "source": "hub"},
			},
		},
	}
	governance := map[string]any{
		"dependencyVerification": map[string]any{
			"schema":                "maclaw.app.install_plan.v1",
			"hasBlockingDependency": false,
			"hasMissingRequired":    false,
			"dependencies": []any{
				map[string]any{
					"id":                  "customer-renewal-skill",
					"kind":                "runtime_skill",
					"required":            true,
					"installed":           true,
					"health":              "ready",
					"action":              "skip",
					"preflight_status":    "ready",
					"integrity_status":    "failed",
					"integrity_code":      "package_integrity_failed",
					"install_error_code":  "package_integrity_failed",
					"install_error_stage": "skillhub_download",
				},
			},
		},
	}

	issue := maclawAppDependencyVerificationReviewIssue(entry, governance, "apps[0].app")
	if issue == nil || issue.Path != "apps[0].app.governance.dependencyVerification" || !strings.Contains(issue.Message, "required dependency is missing or blocked") {
		t.Fatalf("expected dependency install trace failure to block review: %#v", issue)
	}
}

func TestMaclawAppPlanDependencyMatchesWrappedAppIDs(t *testing.T) {
	deps := []maclawAppInstallPlanDependency{
		{ID: "market-skill", Required: true, Installed: true, Health: "ready", Action: "skip", AppIDs: []string{"market-customer-console"}},
		{ID: "datasrv-skill", Required: true, Installed: false, Health: "missing", Action: "blocked", AppIDs: []string{"datasrv-installed-expense-approval"}},
	}
	if got := cloneMaclawAppPlanDependenciesForApp(deps, "customer-console"); len(got) != 1 || got[0].ID != "market-skill" {
		t.Fatalf("expected market wrapped dependency to match canonical app id: %#v", got)
	}
	if !hasMissingMaclawAppRequiredDependencyForApp(deps, "expense-approval") {
		t.Fatalf("expected datasrv wrapped dependency to match canonical app id")
	}
	if !hasBlockingMaclawAppRequiredDependencyForApp(deps, "expense-approval") {
		t.Fatalf("expected datasrv wrapped dependency to count as blocking for canonical app id")
	}
	if got := cloneMaclawAppPlanDependenciesForApp(deps, "unrelated-app"); len(got) != 0 {
		t.Fatalf("expected unrelated app id to stay isolated: %#v", got)
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

func TestSubmitMaclawAppPackageFlagsStaleRunEvidenceDefinitionHash(t *testing.T) {
	tmpHome := t.TempDir()
	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "stale-run-tool")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# Stale run tool\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "stale-run-tool", SkillDir: skillDir, Status: "active"}}
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
				"id": "stale-run-evidence",
				"name": "Stale Run Evidence",
				"description": "Definition changed after test run",
				"category": "Ops",
				"kind": "tool_app",
				"icon": "tool",
				"version": 2,
				"binding": {"skill": {"id":"stale-run-tool", "inputMode":"form", "outputModes":["json"]}},
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "template":"document_workspace", "regionCount":4},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content", "text"]},
					"dependencyVerification": {"schema":"maclaw.app.install_plan.v1", "dependencies":[{"id":"stale-run-tool", "kind":"runtime_skill", "required":true, "installed":true, "health":"ready", "action":"skip"}]},
					"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-basic","sampleInput":{"sample":true},"expectedOutput":{"status":"ok"},"requiredRoles":["tester"],"requiredScopes":["app.run"],"riskLevel":"low"}, "testProtocolFingerprint":"proto-basic", "runId":"run-old-definition", "definitionHash":"00000000", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"text":"ok"}, "outputs":[{"kind":"text", "text":"ok"}]}
				}
			}
		}]
	}`

	result := map[string]any{}
	assertSubmitMaclawAppPackageBlocked(t, app, pkg, "apps[0].app.governance.testEvidence.definitionHash")
	return
	if result["review_issue_count"] != 1 {
		t.Fatalf("expected one stale definition hash issue, got %#v", result)
	}
	detail, err := app.GetMaclawAppPackageSubmission(result["submission_id"].(string))
	if err != nil {
		t.Fatalf("GetMaclawAppPackageSubmission error: %v", err)
	}
	if len(detail.ReviewIssues) != 1 {
		t.Fatalf("expected one durable review issue: %#v", detail.ReviewIssues)
	}
	issue := detail.ReviewIssues[0]
	if issue.Path != "apps[0].app.governance.testEvidence.definitionHash" || issue.Severity != "error" || !strings.Contains(issue.Message, "definition hash") {
		t.Fatalf("unexpected stale definition hash issue: %#v", issue)
	}
}

func TestSubmitMaclawAppPackageFlagsStaleRunEvidenceWhenLegacyBindingUILayoutChanges(t *testing.T) {
	tmpHome := t.TempDir()
	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "stale-layout-tool")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# Stale layout tool\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "stale-layout-tool", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	pkg := maclawAppPackageWithCurrentDefinitionHashes(t, `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "stale-layout-evidence",
				"name": "Stale Layout Evidence",
				"description": "Layout changed after test run",
				"category": "Ops",
				"kind": "tool_app",
				"icon": "tool",
				"version": 2,
				"binding": {
					"skill": {"id":"stale-layout-tool", "inputMode":"form", "outputModes":["json"]},
					"ui": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "layouts":{"tool_workspace":{"template":"document_workspace", "density":"compact", "primaryRegion":"left", "outputRegion":"right", "regions":[{"id":"input","role":"input","placement":"left"},{"id":"result","role":"output","placement":"right"}], "studio":{"savedInManifest":true}}}},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content"]},
					"testProtocol": {"schema":"maclaw.app.test_protocol.v1", "requiredRuns":1, "cases":[{"id":"smoke", "name":"Smoke", "required":true, "expectedOutputs":["content"]}]}
				},
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "template":"document_workspace", "density":"compact", "primaryRegion":"left", "outputRegion":"right", "regionCount":2, "regions":[{"id":"input","role":"input","placement":"left"},{"id":"result","role":"output","placement":"right"}]},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content"]},
					"dependencyVerification": {"schema":"maclaw.app.install_plan.v1", "dependencies":[{"id":"stale-layout-tool", "kind":"runtime_skill", "required":true, "installed":true, "health":"ready", "action":"skip"}]},
					"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-basic","sampleInput":{"sample":true},"expectedOutput":{"status":"ok"},"requiredRoles":["tester"],"requiredScopes":["app.run"],"riskLevel":"low"}, "testProtocolFingerprint":"proto-basic", "runId":"run-old-layout", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"text":"ok"}, "outputs":[{"kind":"text", "text":"ok"}]}
				}
			}
		}]
	}`)
	var doc map[string]any
	if err := json.Unmarshal([]byte(pkg), &doc); err != nil {
		t.Fatalf("decode package: %v", err)
	}
	appEntry := anyMap(anySlice(doc["apps"])[0])
	appBody := anyMap(appEntry["app"])
	delete(appBody, "ui")
	binding := anyMap(appBody["binding"])
	ui := anyMap(binding["ui"])
	layouts := anyMap(ui["layouts"])
	toolWorkspace := anyMap(layouts["tool_workspace"])
	toolWorkspace["density"] = "comfortable"
	mutated, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("encode package: %v", err)
	}

	result := map[string]any{}
	assertSubmitMaclawAppPackageBlocked(t, app, string(mutated), "apps[0].app.governance.testEvidence.definitionHash")
	return
	if result["review_issue_count"] != 1 {
		t.Fatalf("expected one stale definition hash issue, got %#v", result)
	}
	detail, err := app.GetMaclawAppPackageSubmission(result["submission_id"].(string))
	if err != nil {
		t.Fatalf("GetMaclawAppPackageSubmission error: %v", err)
	}
	if len(detail.ReviewIssues) != 1 || detail.ReviewIssues[0].Path != "apps[0].app.governance.testEvidence.definitionHash" || !strings.Contains(detail.ReviewIssues[0].Message, "definition hash") {
		t.Fatalf("unexpected stale legacy binding layout definition hash issue: %#v", detail.ReviewIssues)
	}
}

func TestSubmitMaclawAppPackageFlagsStaleRunEvidenceWhenTopLevelUILayoutChanges(t *testing.T) {
	tmpHome := t.TempDir()
	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "stale-top-level-ui-tool")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# Stale top-level ui tool\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "stale-top-level-ui-tool", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	pkg := maclawAppPackageWithCurrentDefinitionHashes(t, `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "stale-top-level-ui-evidence",
				"name": "Stale Top Level UI Evidence",
				"description": "Top-level UI changed after test run",
				"category": "Ops",
				"kind": "tool_app",
				"icon": "tool",
				"version": 2,
				"ui": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "layouts":{"tool_workspace":{"template":"document_workspace", "density":"compact", "primaryRegion":"left", "outputRegion":"right", "regions":[{"id":"input","role":"input","placement":"left"},{"id":"result","role":"output","placement":"right"}], "studio":{"savedInManifest":true}}}},
				"binding": {
					"skill": {"id":"stale-top-level-ui-tool", "inputMode":"form", "outputModes":["json"]},
					"ui": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "layouts":{"tool_workspace":{"template":"document_workspace", "density":"compact", "primaryRegion":"left", "outputRegion":"right", "regions":[{"id":"input","role":"input","placement":"left"},{"id":"result","role":"output","placement":"right"}], "studio":{"savedInManifest":true}}}},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content"]},
					"testProtocol": {"schema":"maclaw.app.test_protocol.v1", "requiredRuns":1, "cases":[{"id":"smoke", "name":"Smoke", "required":true, "expectedOutputs":["content"]}]}
				},
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "template":"document_workspace", "density":"compact", "primaryRegion":"left", "outputRegion":"right", "regionCount":2, "regions":[{"id":"input","role":"input","placement":"left"},{"id":"result","role":"output","placement":"right"}]},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content"]},
					"dependencyVerification": {"schema":"maclaw.app.install_plan.v1", "dependencies":[{"id":"stale-top-level-ui-tool", "kind":"runtime_skill", "required":true, "installed":true, "health":"ready", "action":"skip"}]},
					"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-basic","sampleInput":{"sample":true},"expectedOutput":{"status":"ok"},"requiredRoles":["tester"],"requiredScopes":["app.run"],"riskLevel":"low"}, "testProtocolFingerprint":"proto-basic", "runId":"run-old-top-level-ui", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"text":"ok"}, "outputs":[{"kind":"text", "text":"ok"}]}
				}
			}
		}]
	}`)
	var doc map[string]any
	if err := json.Unmarshal([]byte(pkg), &doc); err != nil {
		t.Fatalf("decode package: %v", err)
	}
	appEntry := anyMap(anySlice(doc["apps"])[0])
	appBody := anyMap(appEntry["app"])
	ui := anyMap(appBody["ui"])
	layouts := anyMap(ui["layouts"])
	toolWorkspace := anyMap(layouts["tool_workspace"])
	toolWorkspace["density"] = "spacious"
	mutated, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("encode package: %v", err)
	}

	result := map[string]any{}
	assertSubmitMaclawAppPackageBlocked(t, app, string(mutated), "apps[0].app.governance.testEvidence.definitionHash")
	return
	detail, err := app.GetMaclawAppPackageSubmission(result["submission_id"].(string))
	if err != nil {
		t.Fatalf("GetMaclawAppPackageSubmission error: %v", err)
	}
	if len(detail.ReviewIssues) != 1 || detail.ReviewIssues[0].Path != "apps[0].app.governance.testEvidence.definitionHash" || !strings.Contains(detail.ReviewIssues[0].Message, "definition hash") {
		t.Fatalf("top-level app.ui change should stale run evidence even when binding.ui is unchanged: %#v", detail.ReviewIssues)
	}
}

func TestSubmitMaclawAppPackageRejectsWorkspaceLayoutFingerprintMismatch(t *testing.T) {
	tmpHome := t.TempDir()
	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "layout-fingerprint-tool")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# Layout fingerprint tool\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "layout-fingerprint-tool", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	layout := map[string]any{
		"template":      "document_workspace",
		"density":       "compact",
		"primaryRegion": "left",
		"outputRegion":  "right",
		"regions": []any{
			map[string]any{"id": "input", "role": "input", "placement": "left", "order": 1},
			map[string]any{"id": "result", "role": "output", "placement": "right", "order": 2},
		},
		"studio": map[string]any{"savedInManifest": true, "updatedBy": "app_studio"},
	}
	layout["fingerprint"] = maclawAppWorkspaceLayoutFingerprint("tool_workspace", layout)
	ui := map[string]any{
		"schema":    "maclaw.app.ui.v1",
		"generated": true,
		"entry":     "tool_workspace",
		"layouts":   map[string]any{"tool_workspace": layout},
	}
	governanceLayout := cloneMapAny(layout)
	governanceLayout["schema"] = "maclaw.app.ui.v1"
	governanceLayout["entry"] = "tool_workspace"
	governanceLayout["regionCount"] = 2
	governanceLayout["visibleRegionCount"] = 2
	governanceLayout["regionIds"] = []any{"input", "result"}
	governanceLayout["fingerprint"] = "deadbeef"
	doc := map[string]any{
		"schema":        "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": []any{
			map[string]any{
				"schema":        "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"installUnit":   "enterprise_app_pack",
				"app": map[string]any{
					"id":          "layout-fingerprint-app",
					"name":        "Layout Fingerprint App",
					"description": "Reject mismatched workspace layout fingerprints",
					"category":    "Tools",
					"kind":        "tool_app",
					"icon":        "pdf",
					"version":     1,
					"launchMode":  "agent_dynamic_ui",
					"ui":          ui,
					"binding": map[string]any{
						"skill":          map[string]any{"id": "layout-fingerprint-tool", "inputMode": "form", "outputModes": []any{"text"}},
						"dependencies":   map[string]any{"skills": []any{map[string]any{"id": "layout-fingerprint-tool", "kind": "runtime_skill", "required": true, "source": "local"}}},
						"ui":             ui,
						"resultContract": map[string]any{"schema": "maclaw.app.result.v1", "primary": "content", "types": []any{"content", "text"}, "delivery": map[string]any{"inlineContent": true}},
						"testProtocol":   map[string]any{"schema": "maclaw.app.test_protocol.v1", "fingerprint": "proto-layout-fp", "sampleInput": map[string]any{"sample": true}, "expectedOutput": map[string]any{"content": "ok"}, "requiredRoles": []any{"tester"}, "requiredScopes": []any{"app.run"}, "riskLevel": "low"},
					},
					"governance": map[string]any{
						"workspaceLayout":        governanceLayout,
						"resultContract":         map[string]any{"schema": "maclaw.app.result.v1", "primary": "content", "types": []any{"content", "text"}, "delivery": map[string]any{"inlineContent": true}},
						"dependencyVerification": map[string]any{"schema": "maclaw.app.install_plan.v1", "hasMissingRequired": false, "hasBlockingDependency": false, "dependencies": []any{map[string]any{"id": "layout-fingerprint-tool", "kind": "runtime_skill", "required": true, "installed": true, "health": "ready", "action": "skip"}}},
						"testProtocol":           map[string]any{"schema": "maclaw.app.test_protocol.v1", "fingerprint": "proto-layout-fp", "sampleInput": map[string]any{"sample": true}, "expectedOutput": map[string]any{"content": "ok"}, "requiredRoles": []any{"tester"}, "requiredScopes": []any{"app.run"}, "riskLevel": "low"},
						"testEvidence":           map[string]any{"testProtocol": map[string]any{"schema": "maclaw.app.test_protocol.v1", "fingerprint": "proto-layout-fp", "sampleInput": map[string]any{"sample": true}, "expectedOutput": map[string]any{"content": "ok"}, "requiredRoles": []any{"tester"}, "requiredScopes": []any{"app.run"}, "riskLevel": "low"}, "testProtocolFingerprint": "proto-layout-fp", "runId": "run-layout-fp", "verifiedAt": "2026-07-01T01:00:00Z", "resultPayload": map[string]any{"content": "ok"}, "outputs": []any{map[string]any{"kind": "content", "text": "ok"}}, "resultCoverage": map[string]any{"ok": true, "primary": "content", "coveredTypes": []any{"content", "text"}, "missingTypes": []any{}}},
					},
				},
			},
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal package: %v", err)
	}
	pkg := maclawAppPackageWithCurrentDefinitionHashes(t, string(raw))

	assertSubmitMaclawAppPackageBlocked(t, app, pkg, "apps[0].app.governance.workspaceLayout.fingerprint")
}

func TestSubmitMaclawAppPackageRequiresRunEvidenceWorkspaceLayoutFingerprint(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pkg := maclawAppPackageWithCurrentDefinitionHashes(t, `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "missing-layout-evidence-fingerprint",
				"name": "Missing Layout Evidence Fingerprint",
				"kind": "tool_app",
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "template":"document_workspace", "density":"compact", "regionCount":2, "regions":[{"id":"input","role":"input","placement":"left"},{"id":"output","role":"output","placement":"right"}]},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content", "text"]},
					"testProtocol": {"schema":"maclaw.app.test_protocol.v1", "fingerprint":"proto-layout-evidence", "sampleInput":{"sample":true}, "expectedOutput":{"content":"ok"}, "requiredRoles":["tester"], "requiredScopes":["app.run"], "riskLevel":"low"},
					"testEvidence": {"runId":"run-missing-layout-fp", "testProtocolFingerprint":"proto-layout-evidence", "verifiedAt":"2026-07-01T01:00:00Z", "resultPayload":{"content":"ok"}, "outputs":[{"kind":"content", "text":"ok"}], "resultCoverage":{"ok":true, "primary":"content", "coveredTypes":["content", "text"], "missingTypes":[]}}
				}
			}
		}]
	}`)
	var doc map[string]any
	if err := json.Unmarshal([]byte(pkg), &doc); err != nil {
		t.Fatalf("decode package: %v", err)
	}
	entry := anyMap(anySlice(doc["apps"])[0])
	appBody := anyMap(entry["app"])
	governance := anyMap(appBody["governance"])
	testEvidence := anyMap(governance["testEvidence"])
	delete(testEvidence, "workspaceLayoutFingerprint")
	mutated, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("encode package: %v", err)
	}

	assertSubmitMaclawAppPackageBlocked(t, app, string(mutated), "apps[0].app.governance.testEvidence.workspaceLayoutFingerprint")
}

func TestSubmitMaclawAppPackageBlocksStaleRunEvidenceWorkspaceLayoutFingerprint(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pkg := maclawAppPackageWithCurrentDefinitionHashes(t, `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "stale-layout-evidence-fingerprint",
				"name": "Stale Layout Evidence Fingerprint",
				"kind": "tool_app",
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "template":"document_workspace", "density":"compact", "regionCount":2, "regions":[{"id":"input","role":"input","placement":"left"},{"id":"output","role":"output","placement":"right"}]},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content", "text"]},
					"testProtocol": {"schema":"maclaw.app.test_protocol.v1", "fingerprint":"proto-stale-layout-evidence", "sampleInput":{"sample":true}, "expectedOutput":{"content":"ok"}, "requiredRoles":["tester"], "requiredScopes":["app.run"], "riskLevel":"low"},
					"testEvidence": {"runId":"run-stale-layout-fp", "testProtocolFingerprint":"proto-stale-layout-evidence", "verifiedAt":"2026-07-01T01:00:00Z", "resultPayload":{"content":"ok"}, "outputs":[{"kind":"content", "text":"ok"}], "resultCoverage":{"ok":true, "primary":"content", "coveredTypes":["content", "text"], "missingTypes":[]}}
				}
			}
		}]
	}`)
	var doc map[string]any
	if err := json.Unmarshal([]byte(pkg), &doc); err != nil {
		t.Fatalf("decode package: %v", err)
	}
	entry := anyMap(anySlice(doc["apps"])[0])
	appBody := anyMap(entry["app"])
	governance := anyMap(appBody["governance"])
	testEvidence := anyMap(governance["testEvidence"])
	testEvidence["workspaceLayoutFingerprint"] = "deadbeef"
	mutated, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("encode package: %v", err)
	}

	assertSubmitMaclawAppPackageBlocked(t, app, string(mutated), "apps[0].app.governance.testEvidence.workspaceLayoutFingerprint")
}

func TestRecordMaclawAppInstallBlocksStaleRunEvidenceWorkspaceLayoutFingerprint(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pkg := maclawAppPackageWithCurrentDefinitionHashes(t, `{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "install-stale-layout-evidence-fingerprint",
			"name": "Install Stale Layout Evidence Fingerprint",
			"kind": "tool_app",
			"governance": {
				"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "template":"document_workspace", "density":"compact", "regionCount":2, "regions":[{"id":"input","role":"input","placement":"left"},{"id":"output","role":"output","placement":"right"}]},
				"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content", "text"]},
				"testProtocol": {"schema":"maclaw.app.test_protocol.v1", "fingerprint":"proto-install-layout-evidence", "sampleInput":{"sample":true}, "expectedOutput":{"content":"ok"}, "requiredRoles":["tester"], "requiredScopes":["app.run"], "riskLevel":"low"},
				"testEvidence": {"runId":"run-install-stale-layout-fp", "testProtocolFingerprint":"proto-install-layout-evidence", "verifiedAt":"2026-07-01T01:00:00Z", "resultPayload":{"content":"ok"}, "outputs":[{"kind":"content", "text":"ok"}], "resultCoverage":{"ok":true, "primary":"content", "coveredTypes":["content", "text"], "missingTypes":[]}}
			}
		}
	}`)
	var doc map[string]any
	if err := json.Unmarshal([]byte(pkg), &doc); err != nil {
		t.Fatalf("decode package: %v", err)
	}
	governance := anyMap(anyMap(doc["app"])["governance"])
	testEvidence := anyMap(governance["testEvidence"])
	testEvidence["workspaceLayoutFingerprint"] = "deadbeef"
	mutated, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("encode package: %v", err)
	}

	_, err = app.RecordMaclawAppInstall(string(mutated), "market")
	if err == nil {
		t.Fatalf("expected stale workspace layout run evidence to block install")
	}
	if !strings.Contains(err.Error(), "workspace layout fingerprint") {
		t.Fatalf("expected install error to mention workspace layout fingerprint, got %v", err)
	}
}

func TestSubmitMaclawAppPackageRequiresRunEvidenceDefinitionHash(t *testing.T) {
	tmpHome := t.TempDir()
	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "missing-hash-tool")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# Missing hash tool\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "missing-hash-tool", SkillDir: skillDir, Status: "active"}}
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
				"id": "missing-run-evidence-hash",
				"name": "Missing Run Evidence Hash",
				"kind": "tool_app",
				"binding": {"skill": {"id":"missing-hash-tool", "inputMode":"form", "outputModes":["json"]}},
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "template":"document_workspace", "regionCount":4},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content", "text"]},
					"dependencyVerification": {"schema":"maclaw.app.install_plan.v1", "dependencies":[{"id":"missing-hash-tool", "kind":"runtime_skill", "required":true, "installed":true, "health":"ready", "action":"skip"}]},
					"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-basic","sampleInput":{"sample":true},"expectedOutput":{"status":"ok"},"requiredRoles":["tester"],"requiredScopes":["app.run"],"riskLevel":"low"}, "testProtocolFingerprint":"proto-basic", "runId":"run-missing-definition-hash", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"text":"ok"}, "outputs":[{"kind":"text", "text":"ok"}]}
				}
			}
		}]
	}`

	result := map[string]any{}
	assertSubmitMaclawAppPackageBlocked(t, app, pkg, "apps[0].app.governance.testEvidence.definitionHash")
	return
	if result["review_issue_count"] != 1 {
		t.Fatalf("expected one missing definition hash issue, got %#v", result)
	}
	detail, err := app.GetMaclawAppPackageSubmission(result["submission_id"].(string))
	if err != nil {
		t.Fatalf("GetMaclawAppPackageSubmission error: %v", err)
	}
	if len(detail.ReviewIssues) != 1 {
		t.Fatalf("expected one durable review issue: %#v", detail.ReviewIssues)
	}
	issue := detail.ReviewIssues[0]
	if issue.Path != "apps[0].app.governance.testEvidence.definitionHash" || issue.Severity != "error" || !strings.Contains(issue.Message, "missing") {
		t.Fatalf("unexpected missing definition hash issue: %#v", issue)
	}
}

func TestSubmitMaclawAppPackageFlagsResultCoverageMismatch(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pkg := `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "coverage-mismatch",
				"name": "Coverage Mismatch",
				"kind": "enterprise_normal_app",
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"business_workspace", "template":"classic_split", "regionCount":4},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"business_status", "types":["business_status", "business_record", "content"]},
					"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-basic","sampleInput":{"sample":true},"expectedOutput":{"status":"ok"},"requiredRoles":["tester"],"requiredScopes":["app.run"],"riskLevel":"low"}, "testProtocolFingerprint":"proto-basic", "runId":"run-text-only", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"text":"plain completion only"}, "outputs":[{"kind":"text", "text":"plain completion only"}]}
				}
			}
	}]
	}`

	pkg = maclawAppPackageWithCurrentDefinitionHashes(t, pkg)
	result := map[string]any{}
	assertSubmitMaclawAppPackageBlocked(t, app, pkg, "apps[0].app.governance.testEvidence.resultCoverage")
	return
	if result["review_issue_count"] != 1 {
		t.Fatalf("expected one result coverage review issue, got %#v", result)
	}
	detail, err := app.GetMaclawAppPackageSubmission(result["submission_id"].(string))
	if err != nil {
		t.Fatalf("GetMaclawAppPackageSubmission error: %v", err)
	}
	if len(detail.ReviewIssues) != 1 {
		t.Fatalf("expected one durable review issue: %#v", detail.ReviewIssues)
	}
	issue := detail.ReviewIssues[0]
	if issue.Path != "apps[0].app.governance.testEvidence.resultCoverage" || issue.Severity != "error" || !strings.Contains(issue.Message, "business_status") {
		t.Fatalf("unexpected result coverage issue: %#v", issue)
	}
}

func TestSubmitMaclawAppPackageFlagsExplicitResultCoverageMissingTypes(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pkg := `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "coverage-explicit-missing",
				"name": "Coverage Explicit Missing",
				"kind": "enterprise_normal_app",
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"business_workspace", "template":"classic_split", "regionCount":4},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"business_status", "types":["business_status", "business_record", "content"]},
					"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-coverage-missing","sampleInput":{"sample":true},"expectedOutput":{"status":"ok"},"requiredRoles":["tester"],"requiredScopes":["app.run"],"riskLevel":"low"}, "testProtocolFingerprint":"proto-coverage-missing", "runId":"run-coverage-missing", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"business_status":"ready"}, "resultCoverage":{"ok":true, "primary":"business_status", "coveredTypes":["business_status"], "missingTypes":["business_record"]}}
				}
			}
		}]
	}`

	pkg = maclawAppPackageWithCurrentDefinitionHashes(t, pkg)
	result := map[string]any{}
	assertSubmitMaclawAppPackageBlocked(t, app, pkg, "apps[0].app.governance.testEvidence.resultCoverage")
	return
	if result["review_issue_count"] != 1 {
		t.Fatalf("expected one explicit result coverage review issue, got %#v", result)
	}
	detail, err := app.GetMaclawAppPackageSubmission(result["submission_id"].(string))
	if err != nil {
		t.Fatalf("GetMaclawAppPackageSubmission error: %v", err)
	}
	if len(detail.ReviewIssues) != 1 {
		t.Fatalf("expected one durable review issue: %#v", detail.ReviewIssues)
	}
	issue := detail.ReviewIssues[0]
	if issue.Path != "apps[0].app.governance.testEvidence.resultCoverage" || issue.Severity != "error" || !strings.Contains(issue.Message, "business_record") {
		t.Fatalf("unexpected explicit result coverage issue: %#v", issue)
	}
}

func TestSubmitMaclawAppPackageRejectsInvalidManifest(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if _, err := app.SubmitMaclawAppPackage(`{"schema":"maclaw.app.v1","privateMarker":"x_maclaw_apps"}`); err == nil {
		t.Fatal("expected schema error")
	}
	if _, err := os.Stat(app.maclawAppSubmissionQueuePath()); !os.IsNotExist(err) {
		t.Fatalf("queue should not be created for invalid package, stat err=%v", err)
	}
}

func TestSubmitMaclawAppPackageDoesNotOverwriteCorruptQueue(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := os.MkdirAll(filepath.Dir(app.maclawAppSubmissionQueuePath()), 0o755); err != nil {
		t.Fatalf("make data dir: %v", err)
	}
	if err := os.WriteFile(app.maclawAppSubmissionQueuePath(), []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("write corrupt queue: %v", err)
	}
	pkg := `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {"id": "safe-append", "name": "Safe", "kind": "tool_app", "governance": {"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "template":"document_workspace", "regionCount":2, "regions":[{"id":"input","role":"input","placement":"left"},{"id":"output","role":"output","placement":"right"}]}, "resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content"]}, "testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-safe-append","sampleInput":{"sample":true},"expectedOutput":{"content":"ok"},"requiredRoles":["tester"],"requiredScopes":["app.run"],"riskLevel":"low"}, "testProtocolFingerprint":"proto-safe-append", "runId":"run-safe-append", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"content":"ok"}, "outputs":[{"kind":"content", "text":"ok"}], "resultCoverage":{"ok":true, "primary":"content", "coveredTypes":["content"], "missingTypes":[]}}}}
		}]
	}`
	pkg = maclawAppPackageWithCurrentDefinitionHashes(t, pkg)
	if _, err := app.SubmitMaclawAppPackage(pkg); err == nil || !strings.Contains(err.Error(), "decode maclaw app submission queue") {
		t.Fatalf("expected corrupt queue error, got %v", err)
	}
	data, err := os.ReadFile(app.maclawAppSubmissionQueuePath())
	if err != nil {
		t.Fatalf("read queue: %v", err)
	}
	if string(data) != "{not-json" {
		t.Fatalf("corrupt queue should be preserved, got %q", string(data))
	}
}

func TestSubmitMaclawAppPackageRejectsDuplicateAppIDs(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pkg := `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {"id": "duplicate-app", "name": "First"}
		}, {
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {"id": "duplicate-app", "name": "Second"}
		}]
	}`
	if _, err := app.SubmitMaclawAppPackage(pkg); err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("expected duplicate app id error, got %v", err)
	}
}

func TestListMaclawAppPackageSubmissionsHandlesEmptyAndLimit(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	empty, err := app.ListMaclawAppPackageSubmissions(10)
	if err != nil {
		t.Fatalf("empty list error: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty list len=%d", len(empty))
	}

	pkg := func(id string) string {
		return `{
			"schema": "maclaw.app.pack.v1",
			"privateMarker": "x_maclaw_apps",
			"apps": [{
				"schema": "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"app": {"id": "` + id + `", "name": "App"}
			}]
		}`
	}
	if _, err := app.SubmitMaclawAppPackage(pkg("first-app")); err != nil {
		t.Fatalf("submit first: %v", err)
	}
	if _, err := app.SubmitMaclawAppPackage(pkg("second-app")); err != nil {
		t.Fatalf("submit second: %v", err)
	}
	summaries, err := app.ListMaclawAppPackageSubmissions(1)
	if err != nil {
		t.Fatalf("list limited: %v", err)
	}
	if len(summaries) != 1 || summaries[0].AppIDs[0] != "second-app" {
		t.Fatalf("expected newest limited summary, got %#v", summaries)
	}
}

func TestGetMaclawAppPackageSubmissionReturnsFullPackage(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pkg := `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "detail-app",
				"name": "Detail App",
				"runtime": {"type": "fixed_skill_ui"}
			}
		}]
	}`
	result, err := app.SubmitMaclawAppPackage(pkg)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	submissionID := result["submission_id"].(string)

	detail, err := app.GetMaclawAppPackageSubmission(submissionID)
	if err != nil {
		t.Fatalf("get detail: %v", err)
	}
	if detail == nil {
		t.Fatal("expected detail record")
	}
	if detail.SubmissionID != submissionID || detail.AppIDs[0] != "detail-app" {
		t.Fatalf("unexpected detail metadata: %#v", detail)
	}
	apps, _ := detail.Package["apps"].([]any)
	first, _ := apps[0].(map[string]any)
	appManifest, _ := first["app"].(map[string]any)
	if appManifest["id"] != "detail-app" || appManifest["name"] != "Detail App" {
		t.Fatalf("unexpected package app manifest: %#v", appManifest)
	}

	detail.AppIDs[0] = "mutated"
	appManifest["id"] = "mutated"
	again, err := app.GetMaclawAppPackageSubmission(submissionID)
	if err != nil {
		t.Fatalf("get detail again: %v", err)
	}
	againApps, _ := again.Package["apps"].([]any)
	againFirst, _ := againApps[0].(map[string]any)
	againAppManifest, _ := againFirst["app"].(map[string]any)
	if again.AppIDs[0] != "detail-app" || againAppManifest["id"] != "detail-app" {
		t.Fatalf("detail should be cloned, got appIDs=%#v manifest=%#v", again.AppIDs, againAppManifest)
	}
}

func TestGetMaclawAppPackageSubmissionHandlesMissingID(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if detail, err := app.GetMaclawAppPackageSubmission("missing"); err != nil || detail != nil {
		t.Fatalf("missing detail=%#v err=%v", detail, err)
	}
	if _, err := app.GetMaclawAppPackageSubmission(" "); err == nil {
		t.Fatal("expected required id error")
	}
}

func TestWithdrawMaclawAppPackageSubmissionRemovesLocalOnly(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pkg := `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {"id": "withdraw-app", "name": "App"}
		}]
	}`
	result, err := app.SubmitMaclawAppPackage(pkg)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	submissionID := result["submission_id"].(string)
	ok, err := app.WithdrawMaclawAppPackageSubmission(submissionID)
	if err != nil || !ok {
		t.Fatalf("withdraw local ok=%v err=%v", ok, err)
	}
	summaries, err := app.ListMaclawAppPackageSubmissions(10)
	if err != nil {
		t.Fatalf("list after withdraw: %v", err)
	}
	if len(summaries) != 0 {
		t.Fatalf("expected empty queue, got %#v", summaries)
	}

	err = app.appendMaclawAppSubmission(maclawAppSubmissionRecord{
		SubmissionID: "hub-review-1",
		SubmittedAt:  "2026-06-17T01:00:00Z",
		Status:       "submitted",
		Channel:      "hub",
		AppIDs:       []string{"hub-app"},
		Message:      "uploaded",
	})
	if err != nil {
		t.Fatalf("append hub: %v", err)
	}
	if ok, err := app.WithdrawMaclawAppPackageSubmission("hub-review-1"); err == nil || ok {
		t.Fatalf("expected hub withdraw to fail, ok=%v err=%v", ok, err)
	}
}

func TestUpdateMaclawAppPackageSubmissionStatus(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pkg := `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {"id": "status-app", "name": "App"}
		}]
	}`
	result, err := app.SubmitMaclawAppPackage(pkg)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	localID := result["submission_id"].(string)
	ok, err := app.UpdateMaclawAppPackageSubmissionStatus(localID, maclawAppSubmissionStatusUpdate{
		Status:         "published",
		Channel:        "hub",
		Message:        "published by enterprise market",
		SubmissionID:   "market-review-status-app",
		ReviewedAt:     "2026-06-17T01:30:00Z",
		PublishedAt:    "2026-06-17T01:40:00Z",
		Reviewer:       "market-reviewer",
		RiskLevel:      "high",
		ApprovedScopes: []string{"finance.expense_submit", "finance.expense_submit", "finance.audit"},
	})
	if err != nil || !ok {
		t.Fatalf("update ok=%v err=%v", ok, err)
	}
	summaries, err := app.ListMaclawAppPackageSubmissions(10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(summaries) != 1 || summaries[0].SubmissionID != "market-review-status-app" || summaries[0].Status != "published" || summaries[0].Channel != "hub" {
		t.Fatalf("unexpected summaries after update: %#v", summaries)
	}
	if summaries[0].ReviewedAt != "2026-06-17T01:30:00Z" || summaries[0].PublishedAt != "2026-06-17T01:40:00Z" || summaries[0].Reviewer != "market-reviewer" {
		t.Fatalf("expected review metadata after update: %#v", summaries[0])
	}
	if summaries[0].RiskLevel != "high" || len(summaries[0].ApprovedScopes) != 2 || summaries[0].ApprovedScopes[1] != "finance.audit" {
		t.Fatalf("expected risk and approved scopes after update: %#v", summaries[0])
	}
	if summaries[0].EventCount != 2 || summaries[0].LastEventAt == "" {
		t.Fatalf("expected two status events after update: %#v", summaries[0])
	}
	detail, err := app.GetMaclawAppPackageSubmission("market-review-status-app")
	if err != nil {
		t.Fatalf("detail after update: %v", err)
	}
	if detail == nil || detail.ReviewedAt != "2026-06-17T01:30:00Z" || detail.PublishedAt != "2026-06-17T01:40:00Z" || detail.Reviewer != "market-reviewer" {
		t.Fatalf("expected detail review metadata: %#v", detail)
	}
	if len(detail.Events) != 2 || detail.Events[0].Status != "submitted" || detail.Events[1].Status != "published" || detail.Events[1].SubmissionID != "market-review-status-app" {
		t.Fatalf("expected detail event history: %#v", detail.Events)
	}
	detail.ApprovedScopes[0] = "mutated"
	detail.Events[0].Status = "mutated"
	again, err := app.GetMaclawAppPackageSubmission("market-review-status-app")
	if err != nil {
		t.Fatalf("detail again: %v", err)
	}
	if again.ApprovedScopes[0] != "finance.expense_submit" {
		t.Fatalf("approved scopes should be cloned: %#v", again.ApprovedScopes)
	}
	if again.Events[0].Status != "submitted" {
		t.Fatalf("events should be cloned: %#v", again.Events)
	}
	if ok, err := app.UpdateMaclawAppPackageSubmissionStatus("market-review-status-app", maclawAppSubmissionStatusUpdate{Status: "bad"}); err == nil || ok {
		t.Fatalf("expected invalid status error, ok=%v err=%v", ok, err)
	}
}

func maclawAppReadyToolPackageForHubSyncTest(t *testing.T, appID string) string {
	t.Helper()
	pkg := fmt.Sprintf(`{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": %q,
				"name": "Hub Sync Ready Tool",
				"kind": "tool_app",
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "template":"document_workspace", "regionCount":2, "regions":[{"id":"input", "role":"input", "placement":"left"}, {"id":"output", "role":"output", "placement":"right"}]},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content"]},
					"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1", "fingerprint":"proto-hub-ready", "sampleInput":{"sample":true}, "expectedOutput":{"content":"ok"}, "requiredRoles":["tester"], "requiredScopes":["app.run"], "riskLevel":"low"}, "testProtocolFingerprint":"proto-hub-ready", "runId":"run-hub-ready", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"content":"ok"}, "outputs":[{"kind":"content", "text":"ok", "status":"ready"}], "resultCoverage":{"ok":true, "primary":"content", "coveredTypes":["content"], "missingTypes":[]}}
				}
			}
		}]
	}`, appID)
	return maclawAppPackageWithCurrentDefinitionHashes(t, pkg)
}

func markMaclawAppPackageAsPublishedHubDownloadTest(t *testing.T, pkg map[string]any, appID, capabilityID, versionKey string) {
	t.Helper()
	pkg["source"] = "enterprise_hub"
	pkg["capability_id"] = capabilityID
	pkg["capability"] = map[string]any{
		"id":                  capabilityID,
		"capability_id":       appID,
		"display_name":        appID,
		"status":              "published",
		"current_version_key": versionKey,
	}
	reviewEvidence := map[string]any{
		appID: map[string]any{
			"run_id":                           "run-" + appID + "-published",
			"test_protocol_fingerprint":        "proto-hub-ready",
			"result_contract_primary":          "content",
			"result_coverage_primary":          "content",
			"result_coverage_covered_count":    1,
			"result_coverage_missing_count":    0,
			"output_count":                     1,
			"artifact_count":                   0,
			"has_workspace_layout":             true,
			"workspace_saved_in_manifest":      true,
			"has_dependency_verification":      true,
			"has_blocking_dependency":          false,
			"datasrv_registration_status":      "skipped",
			"datasrv_registration_skip_reason": "no datasrv role bindings",
		},
	}
	pkg["review_evidence"] = reviewEvidence
	pkg["maclaw_app_review_evidence"] = reviewEvidence
	pkg["resolved_dependencies"] = []any{map[string]any{
		"id":       appID + "-skill",
		"kind":     "runtime_skill",
		"required": true,
		"source":   "enterprise_hub",
		"app_ids":  []string{appID},
	}}
	apps := anySlice(pkg["apps"])
	if len(apps) == 0 {
		t.Fatalf("package fixture has no apps")
	}
	entry := anyMap(apps[0])
	if entry == nil {
		t.Fatalf("package fixture app entry is not an object: %#v", apps[0])
	}
	app := anyMap(entry["app"])
	if app == nil {
		t.Fatalf("package fixture app payload is not an object: %#v", entry["app"])
	}
	governance := anyMap(app["governance"])
	if governance == nil {
		governance = map[string]any{}
		app["governance"] = governance
	}
	submission := map[string]any{
		"schema":          "maclaw.app.hub_submission.v1",
		"status":          "published",
		"capability_id":   capabilityID,
		"version_key":     versionKey,
		"submitted_at":    "2026-06-30T07:50:00Z",
		"approved_at":     "2026-06-30T07:55:00Z",
		"published_at":    "2026-06-30T08:00:00Z",
		"review_evidence": reviewEvidence,
	}
	if packageSHA := strings.TrimSpace(maclawAppStringFromAny(pkg["package_sha256"])); packageSHA != "" {
		submission["package_sha256"] = packageSHA
	}
	if signature := anyMap(pkg["package_signature"]); len(signature) > 0 {
		submission["package_signature"] = signature
	}
	governance["submission"] = submission
}

func ensureMaclawAppPackageDependencyVerificationForHubDownloadTest(t *testing.T, pkg map[string]any) {
	t.Helper()
	apps := anySlice(pkg["apps"])
	if len(apps) == 0 {
		t.Fatalf("package fixture has no apps")
	}
	resolvedDeps := anySlice(pkg["resolved_dependencies"])
	for _, rawEntry := range apps {
		entry := anyMap(rawEntry)
		appMap := anyMap(entry["app"])
		if appMap == nil {
			t.Fatalf("package fixture app payload is not an object: %#v", entry["app"])
		}
		appID := strings.TrimSpace(maclawAppStringFromAny(appMap["id"]))
		governance := anyMap(appMap["governance"])
		if governance == nil {
			governance = map[string]any{}
			appMap["governance"] = governance
		}
		if len(anyMap(governance["dependencyVerification"])) > 0 || len(anyMap(governance["dependency_verification"])) > 0 {
			continue
		}
		dependencies := []any{}
		for _, rawDep := range resolvedDeps {
			dep := cloneMapAny(anyMap(rawDep))
			if dep == nil {
				continue
			}
			appIDs := maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(dep["app_ids"], dep["appIDs"]))
			if len(appIDs) > 0 && !maclawAppStringListContains(appIDs, appID) {
				continue
			}
			if dep["installed"] == nil {
				dep["installed"] = true
			}
			if dep["health"] == nil {
				dep["health"] = "ready"
			}
			if dep["action"] == nil {
				dep["action"] = "skip"
			}
			dependencies = append(dependencies, dep)
		}
		if len(dependencies) == 0 {
			dependencies = append(dependencies, map[string]any{
				"id":        appID + "-skill",
				"kind":      "runtime_skill",
				"source":    "enterprise_hub",
				"required":  true,
				"installed": true,
				"health":    "ready",
				"action":    "skip",
				"app_ids":   []string{appID},
			})
		}
		verification := map[string]any{
			"schema":                      "maclaw.app.install_plan.v1",
			"dependency_count":            len(dependencies),
			"dependencyCount":             len(dependencies),
			"has_missing_required":        false,
			"hasMissingRequired":          false,
			"has_blocking_dependency":     false,
			"hasBlockingDependency":       false,
			"has_workflow_contract_issue": false,
			"hasWorkflowContractIssue":    false,
			"has_governance_review_issue": false,
			"hasGovernanceReviewIssue":    false,
			"has_dependency_verification": true,
			"hasDependencyVerification":   true,
			"workspace_saved_in_manifest": true,
			"workspaceSavedInManifest":    true,
			"dependencies":                dependencies,
		}
		governance["dependencyVerification"] = verification
		governance["dependency_verification"] = verification
	}
}

func normalizeMaclawAppPackageWorkspaceFingerprintsForTest(t *testing.T, pkg map[string]any) map[string]string {
	t.Helper()
	out := map[string]string{}
	entries, err := parseMaclawAppPackageEntriesFromMap(pkg, false)
	if err != nil {
		t.Fatalf("parse package before workspace fingerprint normalization: %v", err)
	}
	for _, entry := range entries {
		app := entry.App
		appID := entry.ID
		governance := anyMap(app["governance"])
		layout := anyMap(governance["workspaceLayout"])
		if len(layout) == 0 {
			layout = anyMap(governance["workspace_layout"])
		}
		entryName := strings.TrimSpace(maclawAppStringFromAny(layout["entry"]))
		if entryName == "" {
			entryName = strings.TrimSpace(maclawAppStringFromAny(anyMap(app["ui"])["entry"]))
		}
		if entryName == "" {
			continue
		}
		fingerprint := maclawAppWorkspaceLayoutFingerprint(entryName, layout)
		if fingerprint == "" {
			continue
		}
		layout["fingerprint"] = fingerprint
		out[appID] = fingerprint
		for _, uiMap := range []map[string]any{anyMap(app["ui"]), anyMap(anyMap(app["binding"])["ui"])} {
			layouts := anyMap(uiMap["layouts"])
			if len(layouts) > 0 {
				layouts[entryName] = cloneMapAny(layout)
			}
		}
	}
	entries, err = parseMaclawAppPackageEntriesFromMap(pkg, false)
	if err != nil {
		t.Fatalf("parse package after workspace fingerprint normalization: %v", err)
	}
	for _, entry := range entries {
		governance := anyMap(entry.App["governance"])
		testEvidence := anyMap(governance["testEvidence"])
		if testEvidence == nil {
			testEvidence = anyMap(governance["test_evidence"])
		}
		if testEvidence != nil {
			testEvidence["definitionHash"] = maclawAppDefinitionFingerprintForEntry(entry)
		}
	}
	return out
}

func TestSyncMaclawAppPackageSubmissionToHubUpdatesLocalQueue(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	var capturedPath string
	var capturedAuth string
	var capturedSourceID string
	var capturedPackage map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedAuth = r.Header.Get("Authorization")
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		capturedSourceID, _ = payload["source_submission_id"].(string)
		pkg, _ := payload["package"].(map[string]any)
		capturedPackage = pkg
		if pkg["schema"] != "maclaw.app.pack.v1" {
			t.Fatalf("unexpected package payload: %#v", pkg)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"schema": "maclaw.app.hub_submission.v1",
			"status": "pending_review",
			"package_sha256": "hub-sha",
			"app_count": 1,
			"submissions": [{
				"submission_id": "hub-version-sync-app",
				"capability_id": "sync-app",
				"app_id": "sync-app",
				"app_name": "Sync App",
				"status": "pending_review",
				"version_key": "hub-version-sync-app"
			}]
		}`))
	}))
	defer server.Close()
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	pkg := `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
	"id": "sync-app",
	"name": "Sync App",
	"governance": {
		"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"approval_workspace", "regionCount":3, "regions": [{"id":"approval_inbox", "role":"instance_list", "placement":"left"}, {"id":"request_form", "role":"input", "placement":"center"}, {"id":"result_panel", "role":"output", "placement":"bottom"}]},
		"resultContract": {"schema":"maclaw.app.result.v1", "primary":"approval_result", "types":["approval_result", "business_status"]},
		"dependencyVerification": {"schema":"maclaw.app.install_plan.v1", "dependencyCount":2, "hasMissingRequired":false, "hasBlockingDependency":false, "dependencies": [{"id":"sync-super-skill", "kind":"app_skill", "required":true, "installed":true, "health":"ready", "action":"skip", "install_ref":"hub-sync-super-skill"}, {"id":"sync-workflow", "kind":"workflow_skill", "required":true, "installed":true, "health":"ready", "action":"skip", "install_ref":"hub-sync-workflow"}]},
		"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1", "fingerprint":"proto-sync-approval", "sampleInput":{"sample":true}, "expectedOutput":{"approval_result":"approved"}, "requiredRoles":["tester"], "requiredScopes":["app.run"], "riskLevel":"low"}, "testProtocolFingerprint":"proto-sync-approval", "runId":"run-sync-approval", "primaryResult":"approval_result", "resultPayload": {"approval_result":"approved", "business_status":"approved"}, "outputs": [{"kind":"approval_result", "title":"Decision", "text":"approved", "status":"ready"}], "artifacts": [{"id":"sync-artifact", "name":"sync-approval.pdf", "uri":"artifact://sync/approval.pdf", "status":"ready"}], "resultCoverage":{"ok":true, "primary":"approval_result", "coveredTypes":["approval_result", "business_status"], "missingTypes":[]}, "approvalInstance": {"instanceId":"wf-sync-approval", "approvalID":"approval-sync-1", "recordID":"sync-1", "currentNode":"sync.result", "workflowSkillId":"sync-workflow", "status":"approved", "businessStatus":"approved", "resultStatus":"approved", "resultPayload": {"approval_result":"approved"}, "outputs": [{"kind":"approval_result", "title":"Decision", "text":"approved", "status":"ready"}], "artifacts": [{"id":"sync-artifact", "name":"sync-approval.pdf", "uri":"artifact://sync/approval.pdf", "status":"ready"}]}}
	}
}
		}]
	}`
	pkg = maclawAppPackageWithCurrentDefinitionHashes(t, pkg)
	queued, err := app.SubmitMaclawAppPackage(pkg)
	if err != nil {
		t.Fatalf("SubmitMaclawAppPackage() error = %v", err)
	}
	localID := queued["submission_id"].(string)
	result, err := app.SyncMaclawAppPackageSubmissionToHub(localID)
	if err != nil {
		t.Fatalf("SyncMaclawAppPackageSubmissionToHub() error = %v", err)
	}
	if capturedPath != "/api/capabilities/maclaw-apps/submit" || capturedAuth != "Bearer viewer-token" || capturedSourceID != localID {
		t.Fatalf("unexpected hub request path=%q auth=%q source=%q", capturedPath, capturedAuth, capturedSourceID)
	}
	capturedApps := anySlice(capturedPackage["apps"])
	if len(capturedApps) != 1 {
		t.Fatalf("synced package should preserve one app entry: %#v", capturedPackage)
	}
	capturedEntry := anyMap(capturedApps[0])
	capturedApp := anyMap(capturedEntry["app"])
	capturedGovernance := anyMap(capturedApp["governance"])
	capturedTestEvidence := anyMap(capturedGovernance["testEvidence"])
	if capturedTestEvidence == nil {
		capturedTestEvidence = anyMap(capturedGovernance["test_evidence"])
	}
	capturedApproval := anyMap(capturedTestEvidence["approvalInstance"])
	if capturedApproval == nil {
		capturedApproval = anyMap(capturedTestEvidence["approval_instance"])
	}
	if capturedTestEvidence == nil || capturedTestEvidence["runId"] != "run-sync-approval" || capturedApproval == nil || maclawAppStringValue(capturedApproval, "workflowSkillId", "workflow_skill_id") != "sync-workflow" {
		t.Fatalf("Hub sync should preserve Studio approval test evidence: governance=%#v", capturedGovernance)
	}
	capturedLayout := anyMap(capturedGovernance["workspaceLayout"])
	if capturedLayout == nil {
		capturedLayout = anyMap(capturedGovernance["workspace_layout"])
	}
	if capturedLayout == nil || len(anySlice(capturedLayout["regions"])) != 3 {
		t.Fatalf("Hub sync should preserve dynamic workspace layout: %#v", capturedLayout)
	}
	capturedVerification := anyMap(capturedGovernance["dependencyVerification"])
	if capturedVerification == nil {
		capturedVerification = anyMap(capturedGovernance["dependency_verification"])
	}
	if capturedVerification == nil || capturedVerification["hasMissingRequired"] != false || len(anySlice(capturedVerification["dependencies"])) != 2 {
		t.Fatalf("Hub sync should preserve dependency verification evidence: %#v", capturedVerification)
	}
	if result["submission_id"] != "hub-version-sync-app" || result["status"] != "pending_review" || result["channel"] != "hub" {
		t.Fatalf("unexpected sync result: %#v", result)
	}
	if result["package_sha"] != "hub-sha" || result["package_sha256"] != "hub-sha" {
		t.Fatalf("Hub sync result should expose both package sha aliases: %#v", result)
	}
	summaries, err := app.ListMaclawAppPackageSubmissions(10)
	if err != nil {
		t.Fatalf("list submissions: %v", err)
	}
	if len(summaries) != 1 || summaries[0].SubmissionID != "hub-version-sync-app" || summaries[0].Status != "pending_review" || summaries[0].Channel != "hub" {
		t.Fatalf("unexpected synced summary: %#v", summaries)
	}
	if summaries[0].EventCount != 2 || !strings.Contains(summaries[0].Message, "enterprise Hub") {
		t.Fatalf("expected hub sync event summary: %#v", summaries[0])
	}
	if _, err := app.SyncMaclawAppPackageSubmissionToHub("hub-version-sync-app"); err == nil {
		t.Fatal("expected already hub-backed submission to be rejected")
	}
}

func TestSyncMaclawAppPackageSubmissionToHubRejectsTamperedPackageFingerprint(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	}))
	defer server.Close()
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	queued, err := app.SubmitMaclawAppPackage(maclawAppReadyToolPackageForHubSyncTest(t, "tamper-fingerprint-app"))
	if err != nil {
		t.Fatalf("SubmitMaclawAppPackage() error = %v", err)
	}
	queue, err := app.readMaclawAppSubmissionQueue()
	if err != nil {
		t.Fatalf("read submission queue: %v", err)
	}
	if len(queue.Submissions) != 1 {
		t.Fatalf("expected one queued submission: %#v", queue.Submissions)
	}
	apps := anySlice(queue.Submissions[0].Package["apps"])
	entry := anyMap(apps[0])
	appBody := anyMap(entry["app"])
	appBody["name"] = "Tampered Name"
	if err := app.writeMaclawAppSubmissionQueue(queue); err != nil {
		t.Fatalf("write tampered queue: %v", err)
	}
	_, err = app.SyncMaclawAppPackageSubmissionToHub(queued["submission_id"].(string))
	if err == nil || !strings.Contains(err.Error(), "package fingerprint mismatch") {
		t.Fatalf("expected fingerprint mismatch before Hub sync, got %v", err)
	}
	if called {
		t.Fatal("Hub server should not be called for tampered package fingerprint")
	}
}

func TestSyncMaclawAppPackageSubmissionToHubRejectsUnreadyQueuedPackage(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	}))
	defer server.Close()
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	queued, err := app.SubmitMaclawAppPackage(maclawAppReadyToolPackageForHubSyncTest(t, "unready-sync-app"))
	if err != nil {
		t.Fatalf("SubmitMaclawAppPackage() error = %v", err)
	}
	queue, err := app.readMaclawAppSubmissionQueue()
	if err != nil {
		t.Fatalf("read submission queue: %v", err)
	}
	apps := anySlice(queue.Submissions[0].Package["apps"])
	entry := anyMap(apps[0])
	appBody := anyMap(entry["app"])
	governance := anyMap(appBody["governance"])
	delete(governance, "testEvidence")
	delete(governance, "test_evidence")
	sha, size, err := maclawAppPackageFingerprint(queue.Submissions[0].Package)
	if err != nil {
		t.Fatalf("fingerprint tampered package: %v", err)
	}
	queue.Submissions[0].PackageSHA = sha
	queue.Submissions[0].PackageSize = size
	if err := app.writeMaclawAppSubmissionQueue(queue); err != nil {
		t.Fatalf("write unready queue: %v", err)
	}
	_, err = app.SyncMaclawAppPackageSubmissionToHub(queued["submission_id"].(string))
	if err == nil || !strings.Contains(err.Error(), "not ready for Hub sync") || !strings.Contains(err.Error(), "apps[0].app.governance.testEvidence") {
		t.Fatalf("expected ready gate error before Hub sync, got %v", err)
	}
	if called {
		t.Fatal("Hub server should not be called for unready queued package")
	}
}
func TestInstallMaclawAppPackageFromHubDownloadsAndRecordsInstall(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	var capturedPath string
	var capturedAuth string
	var capturedDataSrvPath string
	var capturedDataSrvAuth string
	var capturedDataSrvBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedAuth = r.Header.Get("Authorization")
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		var pkg map[string]any
		if err := json.Unmarshal([]byte(maclawAppPackageWithCurrentDefinitionHashes(t, `{
			"schema": "maclaw.app.pack.v1",
			"privateMarker": "x_maclaw_apps",
			"source": "enterprise_hub",
			"apps": [{
				"schema": "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
					"app": {
						"id": "hub-install-app",
						"name": "Hub Install App",
						"description": "Install from Hub",
						"kind": "enterprise_normal_app",
						"version": 2,
						"ui": {
							"schema": "maclaw.app.ui.v1",
							"entry": "normal_workspace",
							"generated": true,
							"layouts": {
								"normal_workspace": {
									"template": "classic_split",
									"density": "compact",
									"primaryRegion": "left",
									"outputRegion": "right",
									"fingerprint": "layout-normal-hub-install",
									"visibleRegionCount": 3,
									"studio": {"editable": true, "savedInManifest": true, "updatedBy": "app_studio"},
									"navigation": ["records", "result"],
									"list": {"columns": ["title", "status"]},
									"regions": [
										{"id": "input_form", "role": "input", "placement": "left", "visible": true},
										{"id": "record_grid", "role": "record_list", "placement": "center", "visible": true},
										{"id": "result_panel", "role": "output", "placement": "right", "visible": true}
									]
								}
							}
						},
						"governance": {
							"submission": {"channel": "hub", "status": "approved", "capability_id": "cap-hub-install-app"},
							"workspaceLayout": {"schema": "maclaw.app.ui.v1", "entry": "normal_workspace", "template": "classic_split", "density": "compact", "primaryRegion": "left", "outputRegion": "right", "regionCount": 3, "visibleRegionCount": 3, "regionIds": ["input_form", "record_grid", "result_panel"], "fingerprint": "layout-normal-hub-install", "studio": {"editable": true, "savedInManifest": true, "updatedBy": "app_studio"}, "regions": [{"id": "input_form", "role": "input", "placement": "left", "visible": true}, {"id": "record_grid", "role": "record_list", "placement": "center", "visible": true}, {"id": "result_panel", "role": "output", "placement": "right", "visible": true}]},
							"resultContract": {"schema": "maclaw.app.result.v1", "primary": "business_status", "types": ["business_status", "business_record", "content", "artifact"], "delivery": {"inlineContent": true, "artifacts": true, "businessRecord": true}},
							"testProtocol": {"schema": "maclaw.app.test_protocol.v1", "sampleInput": {"text": "hello"}, "expectedOutput": {"type": "content"}, "fingerprint": "proto-hub-install"},
	                        "testEvidence": {"runId": "run-hub-install", "verifiedAt": "2026-06-27T08:00:00Z", "testProtocolFingerprint": "proto-hub-install", "primaryResult": "business_status", "resultPayload": {"business_status": "done", "business_record": {"id": "hub-run-1"}, "content": "done"}, "outputs": [{"kind": "business_record", "title": "Result", "text": "done", "status": "ready", "data": {"id": "hub-run-1"}}], "artifacts": [{"id": "hub-install-export", "name": "hub-install.csv", "uri": "artifact://hub/install.csv", "status": "ready"}], "resultCoverage": {"ok": true, "primary": "business_status", "coveredTypes": ["business_status", "business_record", "content", "artifact"], "missingTypes": []}}
						},
						"binding": {
							"datasrv": {"domain": "tools", "datasetID": "tools.hub_install_runs", "templateID": "tools.hub_install_runs", "objectRole": "hub_install_run"}
						}
					}
					}]
				}`)), &pkg); err != nil {
			t.Fatalf("decode Hub install package fixture: %v", err)
		}
		packageSHA := strings.Repeat("b", 64)
		versionKey := "enterprise_hub:skill:maclaw-app:hub-install-app@pkg"
		publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("GenerateKey() error = %v", err)
		}
		payload := "maclaw-app\n" + packageSHA + "\n" + versionKey + "\n2026-06-30T08:00:00Z\nhub-admin"
		pkg["package_sha256"] = packageSHA
		pkg["package_signature"] = map[string]any{
			"schema":                 "maclaw.app.package_signature.v1",
			"algorithm":              "ed25519",
			"payload":                payload,
			"public_key_base64":      base64.StdEncoding.EncodeToString(publicKey),
			"public_key_fingerprint": downloadedSkillPublicKeyFingerprint(publicKey),
			"signature_base64":       base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(payload))),
			"package_sha256":         packageSHA,
			"version_key":            versionKey,
			"signed_at":              "2026-06-30T08:00:00Z",
			"signed_by":              "hub-admin",
		}
		markMaclawAppPackageAsPublishedHubDownloadTest(t, pkg, "hub-install-app", "cap-hub-install-app", versionKey)
		normalizeMaclawAppPackageWorkspaceFingerprintsForTest(t, pkg)
		ensureMaclawAppPackageDependencyVerificationForHubDownloadTest(t, pkg)
		_ = json.NewEncoder(w).Encode(pkg)
	}))
	defer server.Close()
	dataSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedDataSrvPath = r.URL.Path
		capturedDataSrvAuth = r.Header.Get("Authorization")
		if r.Method != http.MethodPut {
			t.Fatalf("expected DataSrv PUT, got %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&capturedDataSrvBody); err != nil {
			t.Fatalf("decode DataSrv registration body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"installed"}`))
	}))
	defer dataSrv.Close()
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: dataSrv.URL, Token: "data-token", TenantID: "tenant", UserID: "alice", Role: "data_admin"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}
	result, err := app.InstallMaclawAppPackageFromHub("cap-hub-install-app")
	if err != nil {
		t.Fatalf("InstallMaclawAppPackageFromHub() error = %v", err)
	}
	if capturedPath != "/api/capabilities/maclaw-apps/cap-hub-install-app/package" || capturedAuth != "Bearer viewer-token" {
		t.Fatalf("unexpected hub request path=%q auth=%q", capturedPath, capturedAuth)
	}
	if result["schema"] != "maclaw.app.hub_install.v1" || result["capability_id"] != "cap-hub-install-app" || result["app_count"] != 1 {
		t.Fatalf("unexpected install result: %#v", result)
	}
	if result["package_sha"] == "" || result["package_sha256"] == "" || result["package_sha"] != result["package_sha256"] {
		t.Fatalf("Hub install result should expose both package sha aliases: %#v", result)
	}
	installRecord, ok := result["install_record"].(map[string]any)
	if !ok {
		t.Fatalf("missing install record: %#v", result)
	}
	registration, ok := installRecord["datasrv_registration"].(map[string]any)
	if !ok || registration["synced"] != true || registration["eligible_count"] != 1 || registration["synced_count"] != 1 {
		t.Fatalf("expected Hub install to register DataSrv installation: %#v", installRecord["datasrv_registration"])
	}
	installEvidenceByApp, ok := installRecord["install_evidence"].(map[string]any)
	if !ok {
		t.Fatalf("Hub install result should expose per-app install evidence: %#v", installRecord["install_evidence"])
	}
	hubInstallEvidence := anyMap(installEvidenceByApp["hub-install-app"])
	if hubInstallEvidence == nil {
		t.Fatalf("Hub install evidence should include installed app id: %#v", installEvidenceByApp)
	}
	evidenceRegistration := anyMap(hubInstallEvidence["datasrv_registration"])
	if evidenceRegistration["status"] != "ready" || evidenceRegistration["synced"] != true || maclawAppIntValueForTest(evidenceRegistration["eligible_count"]) != 1 || maclawAppIntValueForTest(evidenceRegistration["synced_count"]) != 1 {
		t.Fatalf("Hub install evidence should include per-app DataSrv registration: %#v", hubInstallEvidence["datasrv_registration"])
	}
	if capturedDataSrvPath != "/api/v1/data/app-installations/hub-install-app" || capturedDataSrvAuth != "Bearer data-token" {
		t.Fatalf("unexpected DataSrv request path=%q auth=%q", capturedDataSrvPath, capturedDataSrvAuth)
	}
	if capturedDataSrvBody["app_id"] != "hub-install-app" || capturedDataSrvBody["kind"] != "enterprise_normal_app" || capturedDataSrvBody["source"] != "enterprise_hub" {
		t.Fatalf("DataSrv registration missing Hub app body: %#v", capturedDataSrvBody)
	}
	metadata, ok := capturedDataSrvBody["metadata"].(map[string]interface{})
	if !ok {
		t.Fatalf("DataSrv registration missing metadata: %#v", capturedDataSrvBody)
	}
	if metadata["test_evidence_run_id"] != "run-hub-install" || metadata["test_evidence_verified_at"] != "2026-06-27T08:00:00Z" || metadata["test_evidence_test_protocol_fingerprint"] != "proto-hub-install" || metadata["test_evidence_primary_result"] != "business_status" {
		t.Fatalf("DataSrv registration missing Hub test evidence summary: %#v", metadata)
	}
	if metadata["test_evidence_output_count"] != float64(1) || metadata["test_evidence_artifact_count"] != float64(1) || metadata["test_evidence_result_coverage_ok"] != true || metadata["test_evidence_result_coverage_primary"] != "business_status" || metadata["test_evidence_result_coverage_covered_count"] != float64(4) {
		t.Fatalf("DataSrv registration missing Hub result coverage summary: %#v", metadata)
	}
	if metadata["result_contract_primary"] != "business_status" {
		t.Fatalf("DataSrv registration missing enterprise normal result contract summary: %#v", metadata)
	}
	if resultTypes := maclawAppStringListFromAny(metadata["result_contract_types"]); len(resultTypes) != 4 || resultTypes[0] != "business_status" || resultTypes[3] != "artifact" {
		t.Fatalf("DataSrv registration missing enterprise normal result contract types: %#v", metadata)
	}
	workspaceFingerprint, _ := metadata["workspace_layout_fingerprint"].(string)
	if metadata["workspace_layout_primary_region"] != "left" || metadata["workspace_layout_output_region"] != "right" || workspaceFingerprint == "" || metadata["workspace_layout_visible_region_count"] != float64(3) {
		t.Fatalf("DataSrv registration missing Hub workspace region summary: %#v", metadata)
	}
	if metadata["test_evidence_workspace_layout_fingerprint"] != workspaceFingerprint || metadata["current_workspace_layout_fingerprint"] != workspaceFingerprint || metadata["test_evidence_workspace_layout_matches_current"] != true || metadata["test_evidence_definition_matches_current"] != true || metadata["test_evidence_test_protocol_matches_current"] != true || metadata["design_consistency_ok"] != true {
		t.Fatalf("DataSrv registration missing design consistency summary: %#v", metadata)
	}
	designConsistency, ok := metadata["design_consistency"].(map[string]interface{})
	if !ok || anyMap(designConsistency["workspace_layout"])["matches_current"] != true || anyMap(designConsistency["test_protocol"])["evidence_fingerprint"] != "proto-hub-install" {
		t.Fatalf("DataSrv registration missing nested design consistency evidence: %#v", metadata["design_consistency"])
	}
	workspaceLayout, ok := metadata["workspace_layout"].(map[string]interface{})
	if !ok || workspaceLayout["entry"] != "normal_workspace" || workspaceLayout["template"] != "classic_split" || workspaceLayout["primary_region"] != "left" || workspaceLayout["output_region"] != "right" || workspaceLayout["fingerprint"] != workspaceFingerprint {
		t.Fatalf("DataSrv registration missing Hub workspace layout: %#v", metadata)
	}
	if workspaceLayout["region_count"] != float64(3) {
		t.Fatalf("DataSrv registration missing Hub workspace region count: %#v", workspaceLayout)
	}
	regions, ok := workspaceLayout["regions"].([]interface{})
	if !ok || len(regions) != 3 {
		t.Fatalf("DataSrv registration should preserve Hub workspace regions: %#v", workspaceLayout)
	}
	outputRegion, _ := regions[2].(map[string]interface{})
	if outputRegion["id"] != "result_panel" || outputRegion["placement"] != "right" || outputRegion["visible"] != true {
		t.Fatalf("DataSrv registration should preserve Hub workspace region placement: %#v", regions)
	}
	resultPayload, ok := metadata["test_evidence_result_payload"].(map[string]interface{})
	if !ok || resultPayload["business_status"] != "done" || resultPayload["content"] != "done" {
		t.Fatalf("DataSrv registration missing Hub result payload summary: %#v", metadata)
	}
	businessRecord, ok := resultPayload["business_record"].(map[string]interface{})
	if !ok || businessRecord["id"] != "hub-run-1" {
		t.Fatalf("DataSrv registration missing Hub business record payload: %#v", resultPayload)
	}
	outputs, ok := metadata["test_evidence_outputs"].([]interface{})
	if !ok || len(outputs) != 1 {
		t.Fatalf("DataSrv registration missing Hub output summary: %#v", metadata)
	}
	artifacts, ok := metadata["test_evidence_artifacts"].([]interface{})
	if !ok || len(artifacts) != 1 {
		t.Fatalf("DataSrv registration missing Hub artifact summary: %#v", metadata)
	}
	roleBindings, ok := capturedDataSrvBody["role_bindings"].([]interface{})
	if !ok || len(roleBindings) != 1 {
		t.Fatalf("DataSrv registration missing role bindings: %#v", capturedDataSrvBody)
	}
	roleBinding, ok := roleBindings[0].(map[string]interface{})
	if !ok || roleBinding["object_role"] != "hub_install_run" || roleBinding["dataset_id"] != "tools.hub_install_runs" {
		t.Fatalf("DataSrv registration missing normal app role binding: %#v", roleBindings)
	}
	records, err := app.ListMaclawAppInstalls(10)
	if err != nil {
		t.Fatalf("ListMaclawAppInstalls() error = %v", err)
	}
	if len(records) != 1 || records[0].AppID != "hub-install-app" || records[0].Source != "enterprise_hub" {
		t.Fatalf("unexpected install records: %#v", records)
	}
	if records[0].VersionSnapshot.AppEntryVersion != "2" {
		t.Fatalf("expected app version snapshot: %#v", records[0].VersionSnapshot)
	}
	if records[0].WorkspaceLayout["fingerprint"] != workspaceFingerprint || records[0].WorkspaceLayout["visibleRegionCount"] != float64(3) {
		t.Fatalf("install audit should persist normal app workspace layout evidence: %#v", records[0].WorkspaceLayout)
	}
	if records[0].ResultContract["primary"] != "business_status" {
		t.Fatalf("install audit should persist normal app result contract: %#v", records[0].ResultContract)
	}
	if records[0].TestEvidence["primaryResult"] != "business_status" {
		t.Fatalf("install audit should persist normal app business run evidence: %#v", records[0].TestEvidence)
	}
	if records[0].DataSrvRegistration["synced"] != true || maclawAppIntValueForTest(records[0].DataSrvRegistration["eligible_count"]) != 1 || maclawAppIntValueForTest(records[0].DataSrvRegistration["synced_count"]) != 1 {
		t.Fatalf("install audit should persist DataSrv registration status: %#v", records[0].DataSrvRegistration)
	}
}
func TestRecordMaclawAppInstallBlocksDataSrvRegistrationFailure(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pkg := maclawAppPackageWithCurrentDefinitionHashes(t, `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"source": "market",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "datasrv-offline-app",
				"name": "DataSrv Offline App",
				"version": "1.0.0",
				"kind": "enterprise_normal_app",
				"binding": {
					"datasrv": {"domain":"ops", "datasetID":"ops.offline_records", "templateID":"ops.offline_template", "objectRole":"offline_record", "preferredAction":"ops.offline_upsert"}
				},
				"ui": {"schema":"maclaw.app.ui.v1", "entry":"normal_workspace", "layouts":{"normal_workspace":{"template":"classic_split", "density":"compact", "regions":[{"id":"input_form", "role":"input", "placement":"left"}, {"id":"record_grid", "role":"record_list", "placement":"center"}, {"id":"result_panel", "role":"output", "placement":"right"}]}}},
				"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content"]},
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"normal_workspace", "template":"classic_split", "density":"compact", "regionCount":3, "regions":[{"id":"input_form", "role":"input", "placement":"left"}, {"id":"record_grid", "role":"record_list", "placement":"center"}, {"id":"result_panel", "role":"output", "placement":"right"}]},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content"]},
					"testProtocol": {"schema":"maclaw.app.test_protocol.v1", "fingerprint":"proto-datasrv-offline", "sampleInput":{"id":"offline-1"}, "expectedOutput":{"content":"ok"}},
					"testEvidence": {"runId":"run-datasrv-offline", "testProtocolFingerprint":"proto-datasrv-offline", "resultPayload":{"content":"ok"}, "outputs":[{"kind":"content", "title":"Result", "text":"ok"}], "resultCoverage":{"ok":true, "primary":"content", "coveredTypes":["content"], "missingTypes":[]}}
				}
			}
		}]
	}`)
	dataSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/data/app-installations/datasrv-offline-app" {
			t.Fatalf("unexpected DataSrv request: %s %s", r.Method, r.URL.Path)
		}
		http.Error(w, "datasrv offline", http.StatusServiceUnavailable)
	}))
	defer dataSrv.Close()
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: dataSrv.URL, Token: "data-token", TenantID: "tenant", UserID: "alice", Role: "data_admin"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	_, err := app.RecordMaclawAppInstall(pkg, "market")
	if err == nil {
		t.Fatalf("RecordMaclawAppInstall should reject failed DataSrv registration")
	}
	errText := strings.ToLower(err.Error())
	if !strings.Contains(errText, "datasrv app installation registration failed") || !strings.Contains(errText, "status=failed") || !strings.Contains(errText, "datasrv-offline-app") || !strings.Contains(errText, "datasrv offline") {
		t.Fatalf("DataSrv registration failure should preserve app id and HTTP reason, got %v", err)
	}
	records, err := app.ListMaclawAppInstalls(10)
	if err != nil {
		t.Fatalf("ListMaclawAppInstalls() error = %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("failed DataSrv registration should not persist local install audit: %#v", records)
	}
}
func TestDownloadMaclawAppPackageFromHubTrustsSignedPackageFingerprint(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	packageSHA := strings.Repeat("a", 64)
	payload := "maclaw-app\n" + packageSHA + "\nenterprise_hub:skill:maclaw-app:signed-app@pkg\n2026-06-30T08:00:00Z\nhub-admin"
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	fingerprint := downloadedSkillPublicKeyFingerprint(publicKey)

	var pkg map[string]any
	if err := json.Unmarshal([]byte(maclawAppReadyToolPackageForHubSyncTest(t, "signed-app")), &pkg); err != nil {
		t.Fatalf("decode package fixture: %v", err)
	}
	pkg["package_sha256"] = packageSHA
	pkg["package_signature"] = map[string]any{
		"schema":                 "maclaw.app.package_signature.v1",
		"algorithm":              "ed25519",
		"payload":                payload,
		"public_key_base64":      base64.StdEncoding.EncodeToString(publicKey),
		"public_key_fingerprint": fingerprint,
		"signature_base64":       base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(payload))),
		"package_sha256":         packageSHA,
		"version_key":            "enterprise_hub:skill:maclaw-app:signed-app@pkg",
		"signed_at":              "2026-06-30T08:00:00Z",
		"signed_by":              "hub-admin",
	}
	markMaclawAppPackageAsPublishedHubDownloadTest(t, pkg, "signed-app", "cap-signed-app", "enterprise_hub:skill:maclaw-app:signed-app@pkg")
	ensureMaclawAppPackageDependencyVerificationForHubDownloadTest(t, pkg)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/capabilities/maclaw-apps/cap-signed-app/package" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer viewer-token" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(pkg)
	}))
	defer server.Close()

	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	result, err := app.DownloadMaclawAppPackageFromHub("cap-signed-app")
	if err != nil {
		t.Fatalf("DownloadMaclawAppPackageFromHub() error = %v", err)
	}
	trusted, ok := result["trusted_package_key_fingerprints"].([]string)
	if !ok || len(trusted) != 1 || trusted[0] != fingerprint {
		t.Fatalf("download result should expose trusted fingerprint: %#v", result["trusted_package_key_fingerprints"])
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(cfg.TrustedSkillPackageKeyFingerprints) != 1 || normalizeDownloadedSkillPublicKeyFingerprint(cfg.TrustedSkillPackageKeyFingerprints[0]) != fingerprint {
		t.Fatalf("trusted package key fingerprints not merged into config: %#v", cfg.TrustedSkillPackageKeyFingerprints)
	}
}

func TestDownloadMaclawAppPackageFromHubAcceptsSharedApprovalFixture(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	fingerprint := downloadedSkillPublicKeyFingerprint(publicKey)
	capabilityID := "cap-approval-ready-app"
	versionKey := "enterprise_hub:skill:maclaw-app:approval-ready-app@pkg"
	fixtureData, err := json.Marshal(maclawapptest.ReadyEnterpriseApprovalMaclawAppPublishedHubPackage(capabilityID, versionKey))
	if err != nil {
		t.Fatalf("marshal shared approval fixture: %v", err)
	}
	var pkg map[string]any
	if err := json.Unmarshal([]byte(maclawAppPackageWithCurrentDefinitionHashes(t, string(fixtureData))), &pkg); err != nil {
		t.Fatalf("decode shared approval fixture: %v", err)
	}
	packageSHA := strings.Repeat("e", 64)
	signature := maclawapptest.SignPublishedMaclawAppHubPackage(pkg, publicKey, privateKey, packageSHA, versionKey, "2026-07-01T01:00:00Z", "hub-admin")
	if anyMap(pkg["package_signature"])["public_key_fingerprint"] != fingerprint || anyMap(signature)["package_sha256"] != packageSHA {
		t.Fatalf("shared signed package fixture should expose expected package signature: %#v", signature)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/capabilities/maclaw-apps/"+capabilityID+"/package" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(pkg)
	}))
	defer server.Close()
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	downloaded, err := app.DownloadMaclawAppPackageFromHub(capabilityID)
	if err != nil {
		t.Fatalf("DownloadMaclawAppPackageFromHub() shared fixture error = %v", err)
	}
	downloadedPackage := anyMap(downloaded["package"])
	capability := anyMap(downloadedPackage["capability"])
	reviewEvidence := anyMap(downloadedPackage["review_evidence"])
	appEvidence := anyMap(reviewEvidence["approval-ready-app"])
	if capability["id"] != capabilityID || capability["status"] != "published" || appEvidence["run_id"] == "" {
		t.Fatalf("downloaded shared fixture should preserve Hub package identity and review evidence: %#v", downloadedPackage)
	}
	appIDs := maclawAppStringListFromAny(downloaded["app_ids"])
	if len(appIDs) != 1 || appIDs[0] != "approval-ready-app" {
		t.Fatalf("downloaded shared fixture should expose approval-ready-app id: %#v", downloaded["app_ids"])
	}
}

func TestInstallSharedPublishedApprovalFixtureFromHubInstallsDependenciesAndRegistersDataSrv(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	fingerprint := downloadedSkillPublicKeyFingerprint(publicKey)
	capabilityID := "cap-approval-ready-app"
	versionKey := "enterprise_hub:skill:maclaw-app:approval-ready-app@pkg"
	fixtureData, err := json.Marshal(maclawapptest.ReadyEnterpriseApprovalMaclawAppPublishedHubPackage(capabilityID, versionKey))
	if err != nil {
		t.Fatalf("marshal shared approval fixture: %v", err)
	}
	var pkg map[string]any
	if err := json.Unmarshal([]byte(maclawAppPackageWithCurrentDefinitionHashes(t, string(fixtureData))), &pkg); err != nil {
		t.Fatalf("decode shared approval fixture: %v", err)
	}
	packageSHA := strings.Repeat("f", 64)
	maclawapptest.SignPublishedMaclawAppHubPackage(pkg, publicKey, privateKey, packageSHA, versionKey, "2026-07-01T02:00:00Z", "hub-admin")
	appSkillBody, appSkillSHA, appSkillSignature, err := maclawapptest.SignedEnterpriseHubSkillPackage("approval-ready-app-skill", "1.0.0", "run approval app", publicKey, privateKey)
	if err != nil {
		t.Fatalf("signed app skill fixture: %v", err)
	}
	workflowBody, workflowSHA, workflowSignature, err := maclawapptest.SignedEnterpriseHubSkillPackage("approval-ready-workflow", "1.0.0", "run approval workflow", publicKey, privateKey)
	if err != nil {
		t.Fatalf("signed workflow fixture: %v", err)
	}
	var installedAppSkill bool
	var installedWorkflow bool
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer viewer-token" {
			t.Fatalf("Authorization = %q for %s", got, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/capabilities/maclaw-apps/cap-approval-ready-app/package":
			_ = json.NewEncoder(w).Encode(pkg)
		case "/api/capabilities/cap-approval-ready-app-skill":
			_ = json.NewEncoder(w).Encode(maclawapptest.PublishedEnterpriseHubSkillCapability("approval-ready-app-skill", "cap-approval-ready-app-skill", "1.0.0", appSkillSHA, appSkillSignature))
		case "/api/capabilities/cap-approval-ready-workflow":
			_ = json.NewEncoder(w).Encode(maclawapptest.PublishedEnterpriseHubSkillCapability("approval-ready-workflow", "cap-approval-ready-workflow", "1.0.0", workflowSHA, workflowSignature))
		case "/api/v1/skills/approval-ready-app-skill/download":
			installedAppSkill = true
			_, _ = w.Write(appSkillBody)
		case "/api/v1/skills/approval-ready-workflow/download":
			installedWorkflow = true
			_, _ = w.Write(workflowBody)
		case "/api/capabilities/inventory":
			if r.Method != http.MethodPut {
				t.Fatalf("inventory method = %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			t.Fatalf("unexpected Hub path: %s", r.URL.Path)
		}
	}))
	defer hub.Close()
	var appInstallationPayload map[string]any
	type capturedRequest struct {
		Method   string
		Path     string
		RawQuery string
		Body     map[string]any
	}
	dataSrvRequests := []capturedRequest{}
	finalSynced := false
	dataSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		item := capturedRequest{Method: r.Method, Path: r.URL.Path, RawQuery: r.URL.RawQuery}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		dataSrvRequests = append(dataSrvRequests, item)
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/data/app-installations/approval-ready-app":
			appInstallationPayload = item.Body
			_, _ = w.Write([]byte(`{"app_id":"approval-ready-app","status":"installed"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-ready-run-1":
			_, _ = w.Write([]byte(`{"id":"expense-ready-run-1","data":{"status":"draft","amount":960}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-ready-run-1":
			_, _ = w.Write([]byte(`{"id":"expense-ready-run-1","status":"updated"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-ready-run-1/approvals":
			_, _ = w.Write([]byte(`{"id":"approval-ready-run-1","status":"pending"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/approvals/approval-ready-run-1/progress":
			_, _ = w.Write([]byte(`{"id":"approval-ready-run-1","status":"pending","progress":"manager review started"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/approvals/approval-ready-run-1/review":
			if item.Body["decision"] == "approved" && item.Body["workflow_node_id"] == "expense.result" {
				finalSynced = true
			}
			_, _ = w.Write([]byte(`{"id":"approval-ready-run-1","status":"approved"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/approvals":
			if finalSynced {
				_, _ = w.Write([]byte(`{"items":[{"id":"approval-ready-run-1","app_id":"approval-ready-app","dataset_id":"finance.expense_forms","record_id":"expense-ready-run-1","status":"approved","summary":"Shared Ready Run","workflow_skill_id":"approval-ready-workflow","workflow_version":"1.0.0","workflow_instance_id":"wf-ready-run-1","workflow_decision_id":"decision-ready-run-1","workflow_node_id":"expense.result","workflow_node_ids":["expense.submit","expense.manager_review","expense.result"],"current_node_status":"completed","node_tasks":[{"id":"task-result","title":"Result package","status":"done"}],"business_status":"finance_approved","result_status":"approved","result_payload":{"approval_result":"approved","business_status":"finance_approved","business_record":{"id":"expense-ready-run-1","status":"finance_approved"},"text":"approved by shared workflow"},"outputs":[{"type":"content","title":"Workflow Decision","text":"approved by shared workflow"},{"type":"artifact","title":"Approval PDF","artifact":{"id":"ready-run-pdf","name":"approval-ready-run.pdf","uri":"artifact://ready/run.pdf","status":"ready"}}],"artifacts":[{"id":"ready-run-pdf","name":"approval-ready-run.pdf","uri":"artifact://ready/run.pdf","status":"ready"}],"request":{"approval_instance_id":"wf-ready-run-1","appID":"approval-ready-app","blueprintID":"finance.expense.v1","objectRole":"expense_request","approvalEvent":"finance.submitted","workflowSkillId":"approval-ready-workflow","workflowVersion":"1.0.0","workflowNodeId":"expense.result","workflowNodeIds":["expense.submit","expense.manager_review","expense.result"],"currentNodeStatus":"completed","nodeTasks":[{"id":"task-result","title":"Result package","status":"done"}],"businessStatus":"finance_approved","resultStatus":"approved"},"created_by":"alice","submitted_by":"alice","reviewed_by":"manager","assigned_to":"manager","created_at":"2026-07-01T02:00:00Z","updated_at":"2026-07-01T02:03:00Z"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			t.Fatalf("unexpected DataSrv request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer dataSrv.Close()
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: hub.URL, RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: dataSrv.URL, Token: "data-token", TenantID: "tenant", UserID: "alice", Role: "data_admin"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}
	installed, err := app.InstallSelectedMaclawAppPackageFromHub(capabilityID, []string{"approval-ready-app"})
	if err != nil {
		t.Fatalf("InstallSelectedMaclawAppPackageFromHub() shared fixture error = %v", err)
	}
	if !installedAppSkill || !installedWorkflow {
		t.Fatalf("expected both shared fixture dependencies to be installed, appSkill=%v workflow=%v", installedAppSkill, installedWorkflow)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(cfg.TrustedSkillPackageKeyFingerprints) != 1 || normalizeDownloadedSkillPublicKeyFingerprint(cfg.TrustedSkillPackageKeyFingerprints[0]) != fingerprint {
		t.Fatalf("shared fixture install should seed dependency trust from package signature: %#v", cfg.TrustedSkillPackageKeyFingerprints)
	}
	plan, ok := installed["install_plan"].(maclawAppInstallPlan)
	if !ok || plan.HasMissingRequired || plan.HasBlockingDependency {
		t.Fatalf("shared fixture install plan should be ready: %#v", installed["install_plan"])
	}
	if dep := maclawAppPlanDepForTest(plan, "approval-ready-workflow"); dep == nil || !dep.Installed || dep.PackageSHA256 != workflowSHA || dep.PackageSignature == "" || dep.PreflightStatus != "ready" {
		t.Fatalf("shared fixture workflow dependency should be signed, preflighted, and installed: %#v", dep)
	}
	registration := anyMap(anyMap(installed["install_record"])["datasrv_registration"])
	if registration["status"] != "ready" || registration["synced_count"] != 1 {
		t.Fatalf("shared fixture approval app should register with DataSrv: %#v", registration)
	}
	metadata := anyMap(appInstallationPayload["metadata"])
	if appInstallationPayload["kind"] != "enterprise_approval_app" || len(anySlice(appInstallationPayload["role_bindings"])) != 1 {
		t.Fatalf("DataSrv registration should preserve approval app role binding: %#v", appInstallationPayload)
	}
	if metadata["hub_capability_id"] != capabilityID || metadata["hub_review_status"] != "published" || metadata["workflow_contract_skill_id"] != "approval-ready-workflow" || metadata["workspace_layout_fingerprint"] == "" {
		t.Fatalf("DataSrv metadata should preserve Hub governance, workflow, and layout evidence: %#v", metadata)
	}
	records, err := app.ListMaclawAppInstalls(10)
	if err != nil {
		t.Fatalf("ListMaclawAppInstalls() error = %v", err)
	}
	if len(records) != 1 || records[0].AppID != "approval-ready-app" || records[0].DataSrvRegistration["status"] != "ready" {
		t.Fatalf("shared fixture install should be persisted with DataSrv evidence: %#v", records)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	workflowJSON := `{"progress_instances":[{"status":"pending","workflow_instance_id":"wf-ready-run-1","approval_id":"approval-ready-run-1","record_id":"expense-ready-run-1","dataset_id":"finance.expense_forms","object_role":"expense_request","workflow_skill_id":"approval-ready-workflow","workflow_version":"1.0.0","workflow_node_id":"expense.manager_review","workflow_node_ids":["expense.submit","expense.manager_review"],"current_node_status":"waiting_review","node_tasks":[{"id":"task-manager-review","title":"Manager review","assignee":"manager","status":"open"}],"current_assignee":"manager","current_assignee_type":"user","business_status":"finance_pending","result_status":"running","result":"manager review started","result_payload":{"text":"manager review started","business_record":{"id":"expense-ready-run-1","status":"finance_pending"}},"outputs":[{"type":"content","title":"Workflow Progress","text":"manager review started","status":"running"}]}],"approval_instance":{"status":"approved","lane":"handled","workflow_instance_id":"wf-ready-run-1","approval_id":"approval-ready-run-1","record_id":"expense-ready-run-1","dataset_id":"finance.expense_forms","object_role":"expense_request","workflow_skill_id":"approval-ready-workflow","workflow_version":"1.0.0","workflow_node_id":"expense.result","workflow_node_ids":["expense.submit","expense.manager_review","expense.result"],"current_node_status":"completed","node_tasks":[{"id":"task-result","title":"Result package","status":"done"}],"workflow_decision_id":"decision-ready-run-1","business_status":"finance_approved","result_status":"approved","result":"approved by shared workflow","result_payload":{"approval_result":"approved","business_status":"finance_approved","business_record":{"id":"expense-ready-run-1","status":"finance_approved"},"text":"approved by shared workflow"},"outputs":[{"type":"content","title":"Workflow Decision","text":"approved by shared workflow"},{"type":"artifact","title":"Approval PDF","artifact":{"id":"ready-run-pdf","name":"approval-ready-run.pdf","uri":"artifact://ready/run.pdf","status":"ready"}}],"artifacts":[{"id":"ready-run-pdf","name":"approval-ready-run.pdf","uri":"artifact://ready/run.pdf","status":"ready"}]}}`
	workflowResultPath := filepath.Join(app.testHomeDir, "shared-ready-workflow-result.txt")
	if err := os.WriteFile(workflowResultPath, []byte("workflow_result="+workflowJSON+"\n"), 0o644); err != nil {
		t.Fatalf("write shared workflow result fixture: %v", err)
	}
	workflowCommand := `cat "` + workflowResultPath + `"`
	if os.PathSeparator == '\\' {
		workflowCommand = `type "` + workflowResultPath + `"`
	}
	if err := app.skillExecutor.Update(corelib.NLSkillEntry{
		Name:   "approval-ready-workflow",
		Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action:  "bash",
			Params:  map[string]interface{}{"command": workflowCommand},
			Capture: map[string]string{"workflow_result": `workflow_result=(.+)`},
		}},
	}); err != nil {
		t.Fatalf("update shared workflow skill: %v", err)
	}
	started, err := app.StartMaclawAppApprovalWorkflow(MaclawAppApprovalWorkflowStartInput{
		AppID:            "approval-ready-app",
		RecordID:         "expense-ready-run-1",
		Title:            "Shared Ready Run",
		Applicant:        "alice",
		Approver:         "manager",
		BusinessNote:     "submit shared fixture workflow",
		BusinessPayload:  map[string]any{"amount": float64(960), "currency": "CNY"},
		RunWorkflowSkill: true,
	})
	if err != nil {
		t.Fatalf("StartMaclawAppApprovalWorkflow() shared fixture error = %v", err)
	}
	workflowRun, ok := started["workflow_run"].(map[string]any)
	if !ok || workflowRun["ran"] != true {
		t.Fatalf("expected shared fixture workflow run evidence: %#v", started["workflow_run"])
	}
	resultFeedback := anyMap(started["result_feedback"])
	workflowResultFeedback := anyMap(workflowRun["result_feedback"])
	if resultFeedback["approval_result"] != "approved" || resultFeedback["business_status"] != "finance_approved" || resultFeedback["content"] != "approved by shared workflow" || maclawAppIntValueForTest(resultFeedback["artifact_count"]) != 1 || anyMap(resultFeedback["primary_artifact"])["name"] != "approval-ready-run.pdf" {
		t.Fatalf("shared fixture should expose top-level approval result feedback: %#v", resultFeedback)
	}
	if workflowResultFeedback["approval_result"] != "approved" || workflowResultFeedback["business_status"] != "finance_approved" || maclawAppIntValueForTest(workflowResultFeedback["output_count"]) != 2 {
		t.Fatalf("shared fixture should expose workflow result feedback: %#v", workflowResultFeedback)
	}
	progressInstances, ok := workflowRun["progress_instances"].([]maclawAppApprovalInstance)
	if !ok || len(progressInstances) != 1 || progressInstances[0].CurrentNode != "expense.manager_review" || progressInstances[0].ResultStatus != "running" {
		t.Fatalf("shared fixture workflow should sync running progress: %#v", workflowRun["progress_instances"])
	}
	if progressInstances[0].DatasetID != "finance.expense_forms" || progressInstances[0].ObjectRole != "expense_request" || progressInstances[0].BlueprintID != "finance.expense.v1" || progressInstances[0].ApprovalEvent != "finance.submitted" {
		t.Fatalf("shared fixture progress should inherit runtime contract fields from installed app: %#v", progressInstances[0])
	}
	if progressInstances[0].CurrentNodeStatus != "waiting_review" || len(progressInstances[0].NodeTasks) != 1 || progressInstances[0].NodeTasks[0]["id"] != "task-manager-review" {
		t.Fatalf("shared fixture progress should preserve current node status and tasks: %#v", progressInstances[0])
	}
	finalInstance, ok := workflowRun["instance"].(maclawAppApprovalInstance)
	if !ok || finalInstance.ApprovalID != "approval-ready-run-1" || finalInstance.InstanceID != "wf-ready-run-1" || finalInstance.Status != "approved" || finalInstance.WorkflowDecisionID != "decision-ready-run-1" {
		t.Fatalf("shared fixture workflow should finish approval instance: %#v", workflowRun["instance"])
	}
	if finalInstance.DatasetID != "finance.expense_forms" || finalInstance.ObjectRole != "expense_request" || finalInstance.BlueprintID != "finance.expense.v1" || finalInstance.ApprovalEvent != "finance.submitted" {
		t.Fatalf("shared fixture final instance should inherit runtime contract fields from installed app: %#v", finalInstance)
	}
	if finalInstance.CurrentNodeStatus != "completed" || len(finalInstance.NodeTasks) != 1 || finalInstance.NodeTasks[0]["id"] != "task-result" {
		t.Fatalf("shared fixture final instance should preserve current node status and tasks: %#v", finalInstance)
	}
	appHandled, err := app.ListMaclawAppApprovalInstances("approval-ready-app", "handled", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances shared handled error = %v", err)
	}
	globalHandled, err := app.ListMaclawAppApprovalInstancesAll("handled", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstancesAll shared handled error = %v", err)
	}
	if len(appHandled) != 1 || len(globalHandled) != 1 {
		t.Fatalf("shared fixture workflow should be readable from app and global approval centers, app=%#v global=%#v", appHandled, globalHandled)
	}
	readBack := appHandled[0]
	assertMaclawAppApprovalReadbackSameInstanceForTest(t, readBack, globalHandled[0])
	if readBack.AppID != "approval-ready-app" || readBack.WorkflowSkillID != "approval-ready-workflow" || readBack.WorkflowVersion != "1.0.0" || readBack.CurrentNode != "expense.result" {
		t.Fatalf("shared fixture readback should preserve app/workflow identity: %#v", readBack)
	}
	if readBack.DatasetID != "finance.expense_forms" || readBack.ObjectRole != "expense_request" || readBack.BlueprintID != "finance.expense.v1" || readBack.ApprovalEvent != "finance.submitted" {
		t.Fatalf("shared fixture readback should preserve installed runtime contract fields: %#v", readBack)
	}
	if readBack.CurrentNodeStatus != "completed" || len(readBack.NodeTasks) != 1 || readBack.NodeTasks[0]["id"] != "task-result" {
		t.Fatalf("shared fixture readback should preserve DataSrv current node status and tasks: %#v", readBack)
	}
	if readBack.ResultPayload["approval_result"] != "approved" || len(readBack.Outputs) != 2 || len(readBack.Artifacts) != 1 || readBack.Artifacts[0].Name != "approval-ready-run.pdf" {
		t.Fatalf("shared fixture readback should preserve result text and file artifact: %#v", readBack)
	}
	sawProgress := false
	sawReview := false
	sawProgressResultPackage := false
	sawReviewResultPackage := false
	sawAppScopedHandled := false
	sawGlobalHandled := false
	for _, req := range dataSrvRequests {
		if req.Method == http.MethodPost && req.Path == "/api/v1/data/approvals/approval-ready-run-1/progress" {
			sawProgress = req.Body["current_node_status"] == "waiting_review" && len(anySlice(req.Body["node_tasks"])) == 1
			progressPayload := anyMap(req.Body["result_payload"])
			progressRecord := anyMap(progressPayload["business_record"])
			progressOutputs := anySlice(req.Body["outputs"])
			sawProgressResultPackage = progressPayload["text"] == "manager review started" && progressRecord["status"] == "finance_pending" && len(progressOutputs) == 1
		}
		if req.Method == http.MethodPost && req.Path == "/api/v1/data/approvals/approval-ready-run-1/review" {
			sawReview = req.Body["decision"] == "approved" && req.Body["workflow_node_id"] == "expense.result" && req.Body["current_node_status"] == "completed" && len(anySlice(req.Body["node_tasks"])) == 1
			reviewPayload := anyMap(req.Body["result_payload"])
			reviewRecord := anyMap(reviewPayload["business_record"])
			reviewOutputs := anySlice(req.Body["outputs"])
			reviewArtifacts := anySlice(req.Body["artifacts"])
			var reviewArtifact map[string]any
			if len(reviewArtifacts) > 0 {
				reviewArtifact = anyMap(reviewArtifacts[0])
			}
			sawReviewResultPackage = reviewPayload["approval_result"] == "approved" && reviewPayload["business_status"] == "finance_approved" && reviewRecord["id"] == "expense-ready-run-1" && len(reviewOutputs) == 2 && len(reviewArtifacts) == 1 && reviewArtifact["name"] == "approval-ready-run.pdf"
		}
		if req.Method == http.MethodGet && req.Path == "/api/v1/data/approvals" && req.RawQuery == "app_id=approval-ready-app&lane=handled&limit=10" {
			sawAppScopedHandled = true
		}
		if req.Method == http.MethodGet && req.Path == "/api/v1/data/approvals" && req.RawQuery == "lane=handled&limit=10" {
			sawGlobalHandled = true
		}
	}
	if !sawProgress || !sawReview || !sawAppScopedHandled || !sawGlobalHandled {
		t.Fatalf("shared fixture should sync progress/review and query both approval centers, progress=%v review=%v app=%v global=%v requests=%#v", sawProgress, sawReview, sawAppScopedHandled, sawGlobalHandled, dataSrvRequests)
	}
	if !sawProgressResultPackage || !sawReviewResultPackage {
		t.Fatalf("shared fixture should sync progress and final result packages to DataSrv, progressPackage=%v reviewPackage=%v requests=%#v", sawProgressResultPackage, sawReviewResultPackage, dataSrvRequests)
	}
}

func TestInstallSharedPublishedApprovalFixtureBlocksInvalidHubPackageSignature(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey error = %v", err)
	}
	capabilityID := "cap-approval-ready-app"
	versionKey := "enterprise_hub:skill:maclaw-app:approval-ready-app@pkg"
	fixtureData, err := json.Marshal(maclawapptest.ReadyEnterpriseApprovalMaclawAppPublishedHubPackage(capabilityID, versionKey))
	if err != nil {
		t.Fatalf("marshal shared approval fixture: %v", err)
	}
	var pkg map[string]any
	if err := json.Unmarshal([]byte(maclawAppPackageWithCurrentDefinitionHashes(t, string(fixtureData))), &pkg); err != nil {
		t.Fatalf("decode shared approval fixture: %v", err)
	}
	maclawapptest.SignPublishedMaclawAppHubPackage(pkg, publicKey, privateKey, strings.Repeat("b", 64), versionKey, "2026-07-01T03:30:00Z", "hub-admin")
	if signature := anyMap(pkg["package_signature"]); signature != nil {
		signature["signature_base64"] = base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
	}
	dependencyRequests := 0
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer viewer-token" {
			t.Fatalf("Authorization = %q for %s", got, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/capabilities/maclaw-apps/cap-approval-ready-app/package":
			_ = json.NewEncoder(w).Encode(pkg)
		default:
			dependencyRequests++
			http.Error(w, "dependency endpoints should not be reached after Hub package signature failure", http.StatusInternalServerError)
		}
	}))
	defer hub.Close()
	dataSrvCalled := false
	dataSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dataSrvCalled = true
		t.Fatalf("DataSrv should not be called when Hub package signature is invalid: %s %s", r.Method, r.URL.Path)
	}))
	defer dataSrv.Close()
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: hub.URL, RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: dataSrv.URL, Token: "data-token", TenantID: "tenant", UserID: "alice", Role: "data_admin"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}
	_, err = app.InstallSelectedMaclawAppPackageFromHub(capabilityID, []string{"approval-ready-app"})
	if err == nil {
		t.Fatalf("InstallSelectedMaclawAppPackageFromHub should reject invalid Hub package signature")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "package signature") || !strings.Contains(strings.ToLower(err.Error()), "verification failed") {
		t.Fatalf("install error should expose Hub package signature verification failure, got %v", err)
	}
	if dependencyRequests != 0 {
		t.Fatalf("dependency endpoints should not be called after Hub package signature failure, got %d", dependencyRequests)
	}
	if dataSrvCalled {
		t.Fatalf("DataSrv registration should not run after invalid Hub package signature")
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(cfg.TrustedSkillPackageKeyFingerprints) != 0 {
		t.Fatalf("invalid Hub package signature should not seed trusted fingerprints: %#v", cfg.TrustedSkillPackageKeyFingerprints)
	}
	records, listErr := app.ListMaclawAppInstalls(10)
	if listErr != nil {
		t.Fatalf("ListMaclawAppInstalls() error = %v", listErr)
	}
	if len(records) != 0 {
		t.Fatalf("failed Hub package signature install should not persist app install audit: %#v", records)
	}
}

func TestInstallSharedPublishedApprovalFixtureBlocksUntrustedDependencySkillSignature(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	trustedPublicKey, trustedPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey trusted error = %v", err)
	}
	untrustedPublicKey, untrustedPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey untrusted error = %v", err)
	}
	capabilityID := "cap-approval-ready-app"
	versionKey := "enterprise_hub:skill:maclaw-app:approval-ready-app@pkg"
	fixtureData, err := json.Marshal(maclawapptest.ReadyEnterpriseApprovalMaclawAppPublishedHubPackage(capabilityID, versionKey))
	if err != nil {
		t.Fatalf("marshal shared approval fixture: %v", err)
	}
	var pkg map[string]any
	if err := json.Unmarshal([]byte(maclawAppPackageWithCurrentDefinitionHashes(t, string(fixtureData))), &pkg); err != nil {
		t.Fatalf("decode shared approval fixture: %v", err)
	}
	maclawapptest.SignPublishedMaclawAppHubPackage(pkg, trustedPublicKey, trustedPrivateKey, strings.Repeat("a", 64), versionKey, "2026-07-01T03:00:00Z", "hub-admin")
	appSkillBody, appSkillSHA, appSkillSignature, err := maclawapptest.SignedEnterpriseHubSkillPackage("approval-ready-app-skill", "1.0.0", "run approval app", trustedPublicKey, trustedPrivateKey)
	if err != nil {
		t.Fatalf("signed trusted app skill fixture: %v", err)
	}
	workflowBody, workflowSHA, workflowSignature, err := maclawapptest.SignedEnterpriseHubSkillPackage("approval-ready-workflow", "1.0.0", "run approval workflow", untrustedPublicKey, untrustedPrivateKey)
	if err != nil {
		t.Fatalf("signed untrusted workflow fixture: %v", err)
	}
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer viewer-token" {
			t.Fatalf("Authorization = %q for %s", got, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/capabilities/maclaw-apps/cap-approval-ready-app/package":
			_ = json.NewEncoder(w).Encode(pkg)
		case "/api/capabilities/cap-approval-ready-app-skill":
			_ = json.NewEncoder(w).Encode(maclawapptest.PublishedEnterpriseHubSkillCapability("approval-ready-app-skill", "cap-approval-ready-app-skill", "1.0.0", appSkillSHA, appSkillSignature))
		case "/api/capabilities/cap-approval-ready-workflow":
			_ = json.NewEncoder(w).Encode(maclawapptest.PublishedEnterpriseHubSkillCapability("approval-ready-workflow", "cap-approval-ready-workflow", "1.0.0", workflowSHA, workflowSignature))
		case "/api/v1/skills/approval-ready-app-skill/download":
			_, _ = w.Write(appSkillBody)
		case "/api/v1/skills/approval-ready-workflow/download":
			_, _ = w.Write(workflowBody)
		case "/api/capabilities/inventory":
			if r.Method != http.MethodPut {
				t.Fatalf("inventory method = %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			t.Fatalf("unexpected Hub path: %s", r.URL.Path)
		}
	}))
	defer hub.Close()
	dataSrvCalled := false
	dataSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dataSrvCalled = true
		t.Fatalf("DataSrv should not be called when dependency Skill signature is untrusted: %s %s", r.Method, r.URL.Path)
	}))
	defer dataSrv.Close()
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: hub.URL, RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: dataSrv.URL, Token: "data-token", TenantID: "tenant", UserID: "alice", Role: "data_admin"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}
	_, err = app.InstallSelectedMaclawAppPackageFromHub(capabilityID, []string{"approval-ready-app"})
	if err == nil {
		t.Fatalf("InstallSelectedMaclawAppPackageFromHub should reject untrusted dependency Skill signature")
	}
	errText := err.Error()
	if !strings.Contains(errText, "approval-ready-workflow") || !strings.Contains(errText, "package_integrity_failed") || !strings.Contains(errText, "enterprise_hub_install") || !strings.Contains(strings.ToLower(errText), "signature") {
		t.Fatalf("install error should expose workflow dependency signature diagnostics, got %v", err)
	}
	if dataSrvCalled {
		t.Fatalf("DataSrv registration should not run after untrusted dependency Skill signature")
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	trustedFingerprint := downloadedSkillPublicKeyFingerprint(trustedPublicKey)
	untrustedFingerprint := downloadedSkillPublicKeyFingerprint(untrustedPublicKey)
	if len(cfg.TrustedSkillPackageKeyFingerprints) != 1 || normalizeDownloadedSkillPublicKeyFingerprint(cfg.TrustedSkillPackageKeyFingerprints[0]) != trustedFingerprint {
		t.Fatalf("trusted fingerprint should come only from app package signature: %#v", cfg.TrustedSkillPackageKeyFingerprints)
	}
	for _, fingerprint := range cfg.TrustedSkillPackageKeyFingerprints {
		if normalizeDownloadedSkillPublicKeyFingerprint(fingerprint) == untrustedFingerprint {
			t.Fatalf("untrusted dependency Skill key should not be added to trust store: %#v", cfg.TrustedSkillPackageKeyFingerprints)
		}
	}
	records, listErr := app.ListMaclawAppInstalls(10)
	if listErr != nil {
		t.Fatalf("ListMaclawAppInstalls() error = %v", listErr)
	}
	if len(records) != 0 {
		t.Fatalf("failed untrusted dependency install should not persist app install audit: %#v", records)
	}
}

func TestInstallSharedPublishedApprovalFixtureBlocksDataSrvRegistrationFailure(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey error = %v", err)
	}
	capabilityID := "cap-approval-ready-app"
	versionKey := "enterprise_hub:skill:maclaw-app:approval-ready-app@pkg"
	fixtureData, err := json.Marshal(maclawapptest.ReadyEnterpriseApprovalMaclawAppPublishedHubPackage(capabilityID, versionKey))
	if err != nil {
		t.Fatalf("marshal shared approval fixture: %v", err)
	}
	var pkg map[string]any
	if err := json.Unmarshal([]byte(maclawAppPackageWithCurrentDefinitionHashes(t, string(fixtureData))), &pkg); err != nil {
		t.Fatalf("decode shared approval fixture: %v", err)
	}
	maclawapptest.SignPublishedMaclawAppHubPackage(pkg, publicKey, privateKey, strings.Repeat("c", 64), versionKey, "2026-07-01T04:00:00Z", "hub-admin")
	appSkillBody, appSkillSHA, appSkillSignature, err := maclawapptest.SignedEnterpriseHubSkillPackage("approval-ready-app-skill", "1.0.0", "run approval app", publicKey, privateKey)
	if err != nil {
		t.Fatalf("signed app skill fixture: %v", err)
	}
	workflowBody, workflowSHA, workflowSignature, err := maclawapptest.SignedEnterpriseHubSkillPackage("approval-ready-workflow", "1.0.0", "run approval workflow", publicKey, privateKey)
	if err != nil {
		t.Fatalf("signed workflow fixture: %v", err)
	}
	dependencyDownloads := 0
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer viewer-token" {
			t.Fatalf("Authorization = %q for %s", got, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/capabilities/maclaw-apps/cap-approval-ready-app/package":
			_ = json.NewEncoder(w).Encode(pkg)
		case "/api/capabilities/cap-approval-ready-app-skill":
			_ = json.NewEncoder(w).Encode(maclawapptest.PublishedEnterpriseHubSkillCapability("approval-ready-app-skill", "cap-approval-ready-app-skill", "1.0.0", appSkillSHA, appSkillSignature))
		case "/api/capabilities/cap-approval-ready-workflow":
			_ = json.NewEncoder(w).Encode(maclawapptest.PublishedEnterpriseHubSkillCapability("approval-ready-workflow", "cap-approval-ready-workflow", "1.0.0", workflowSHA, workflowSignature))
		case "/api/v1/skills/approval-ready-app-skill/download":
			dependencyDownloads++
			_, _ = w.Write(appSkillBody)
		case "/api/v1/skills/approval-ready-workflow/download":
			dependencyDownloads++
			_, _ = w.Write(workflowBody)
		case "/api/capabilities/inventory":
			if r.Method != http.MethodPut {
				t.Fatalf("inventory method = %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			t.Fatalf("unexpected Hub path: %s", r.URL.Path)
		}
	}))
	defer hub.Close()
	dataSrvCalls := 0
	dataSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer data-token" {
			t.Fatalf("DataSrv Authorization = %q for %s", got, r.URL.Path)
		}
		if r.Method == http.MethodPut && r.URL.Path == "/api/v1/data/app-installations/approval-ready-app" {
			dataSrvCalls++
			http.Error(w, `{"error":"sqlite locked"}`, http.StatusInternalServerError)
			return
		}
		t.Fatalf("unexpected DataSrv request after registration failure boundary: %s %s", r.Method, r.URL.Path)
	}))
	defer dataSrv.Close()
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: hub.URL, RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: dataSrv.URL, Token: "data-token", TenantID: "tenant", UserID: "alice", Role: "data_admin"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}
	_, err = app.InstallSelectedMaclawAppPackageFromHub(capabilityID, []string{"approval-ready-app"})
	if err == nil {
		t.Fatalf("InstallSelectedMaclawAppPackageFromHub should reject DataSrv app installation registration failure")
	}
	errText := strings.ToLower(err.Error())
	if !strings.Contains(errText, "datasrv app installation registration failed") || !strings.Contains(errText, "status=failed") || !strings.Contains(errText, "approval-ready-app") {
		t.Fatalf("install error should expose DataSrv registration failure detail, got %v", err)
	}
	if dependencyDownloads != 2 {
		t.Fatalf("dependencies should be downloaded before DataSrv registration, got %d downloads", dependencyDownloads)
	}
	if dataSrvCalls != 1 {
		t.Fatalf("DataSrv registration should be attempted exactly once, got %d", dataSrvCalls)
	}
	records, listErr := app.ListMaclawAppInstalls(10)
	if listErr != nil {
		t.Fatalf("ListMaclawAppInstalls() error = %v", listErr)
	}
	if len(records) != 0 {
		t.Fatalf("failed DataSrv registration should not persist app install audit: %#v", records)
	}
}

func TestDownloadMaclawAppPackageFromHubRejectsSignedPackageWithoutPublishedGovernance(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	packageSHA := strings.Repeat("c", 64)
	payload := "maclaw-app\n" + packageSHA + "\nenterprise_hub:skill:maclaw-app:unsigned-governance@pkg\n2026-06-30T08:00:00Z\nhub-admin"
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	var pkg map[string]any
	if err := json.Unmarshal([]byte(maclawAppReadyToolPackageForHubSyncTest(t, "unsigned-governance")), &pkg); err != nil {
		t.Fatalf("decode package fixture: %v", err)
	}
	pkg["source"] = "enterprise_hub"
	pkg["package_sha256"] = packageSHA
	pkg["package_signature"] = map[string]any{
		"schema":                 "maclaw.app.package_signature.v1",
		"algorithm":              "ed25519",
		"payload":                payload,
		"public_key_base64":      base64.StdEncoding.EncodeToString(publicKey),
		"public_key_fingerprint": downloadedSkillPublicKeyFingerprint(publicKey),
		"signature_base64":       base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(payload))),
		"package_sha256":         packageSHA,
		"version_key":            "enterprise_hub:skill:maclaw-app:unsigned-governance@pkg",
		"signed_at":              "2026-06-30T08:00:00Z",
		"signed_by":              "hub-admin",
	}
	pkg["review_evidence"] = map[string]any{
		"unsigned-governance": map[string]any{"run_id": "run-unsigned-governance"},
	}
	pkg["maclaw_app_review_evidence"] = pkg["review_evidence"]
	pkg["resolved_dependencies"] = []any{
		map[string]any{"id": "unsigned-governance-skill", "kind": "runtime_skill", "required": true, "source": "enterprise_hub"},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(pkg)
	}))
	defer server.Close()
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	_, err = app.DownloadMaclawAppPackageFromHub("cap-unsigned-governance")
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "governance submission") {
		t.Fatalf("expected missing published governance failure, got %v", err)
	}
}

func TestDownloadMaclawAppPackageFromHubRejectsSignedPackageWithoutTopLevelReviewEvidence(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	packageSHA := strings.Repeat("d", 64)
	payload := "maclaw-app\n" + packageSHA + "\nenterprise_hub:skill:maclaw-app:missing-package-review@pkg\n2026-06-30T08:00:00Z\nhub-admin"
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	var pkg map[string]any
	if err := json.Unmarshal([]byte(maclawAppReadyToolPackageForHubSyncTest(t, "missing-package-review")), &pkg); err != nil {
		t.Fatalf("decode package fixture: %v", err)
	}
	pkg["package_sha256"] = packageSHA
	pkg["package_signature"] = map[string]any{
		"schema":                 "maclaw.app.package_signature.v1",
		"algorithm":              "ed25519",
		"payload":                payload,
		"public_key_base64":      base64.StdEncoding.EncodeToString(publicKey),
		"public_key_fingerprint": downloadedSkillPublicKeyFingerprint(publicKey),
		"signature_base64":       base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(payload))),
		"package_sha256":         packageSHA,
		"version_key":            "enterprise_hub:skill:maclaw-app:missing-package-review@pkg",
		"signed_at":              "2026-06-30T08:00:00Z",
		"signed_by":              "hub-admin",
	}
	markMaclawAppPackageAsPublishedHubDownloadTest(t, pkg, "missing-package-review", "cap-missing-package-review", "enterprise_hub:skill:maclaw-app:missing-package-review@pkg")
	delete(pkg, "review_evidence")
	delete(pkg, "maclaw_app_review_evidence")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(pkg)
	}))
	defer server.Close()
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	_, err = app.DownloadMaclawAppPackageFromHub("cap-missing-package-review")
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "package review_evidence") {
		t.Fatalf("expected missing package review evidence failure, got %v", err)
	}
}

func TestDownloadMaclawAppPackageFromHubRejectsInvalidPackageSignature(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	packageSHA := strings.Repeat("b", 64)
	payload := "maclaw-app\n" + packageSHA + "\nenterprise_hub:skill:maclaw-app:bad-signed-app@pkg\n2026-06-30T08:00:00Z\nhub-admin"
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	var pkg map[string]any
	if err := json.Unmarshal([]byte(maclawAppReadyToolPackageForHubSyncTest(t, "bad-signed-app")), &pkg); err != nil {
		t.Fatalf("decode package fixture: %v", err)
	}
	pkg["source"] = "enterprise_hub"
	pkg["package_sha256"] = packageSHA
	pkg["package_signature"] = map[string]any{
		"schema":                 "maclaw.app.package_signature.v1",
		"algorithm":              "ed25519",
		"payload":                payload,
		"public_key_base64":      base64.StdEncoding.EncodeToString(publicKey),
		"public_key_fingerprint": downloadedSkillPublicKeyFingerprint(publicKey),
		"signature_base64":       base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, []byte("tampered"))),
		"package_sha256":         packageSHA,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(pkg)
	}))
	defer server.Close()
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	_, err = app.DownloadMaclawAppPackageFromHub("bad-signed-app")
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "signature verification failed") {
		t.Fatalf("expected signature verification failure, got %v", err)
	}
}
func TestInstallSelectedMaclawAppPackageFromHubUsesPackageSignatureTrustForDependencySkill(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	fingerprint := downloadedSkillPublicKeyFingerprint(publicKey)
	skillBody, skillSHA, skillSignatureJSON, err := maclawapptest.SignedEnterpriseHubSkillPackage("signed-skill", "1.0.0", "do it", publicKey, privateKey)
	if err != nil {
		t.Fatalf("signed skill fixture: %v", err)
	}

	pkgJSON := maclawAppPackageWithCurrentDefinitionHashes(t, `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"source": "enterprise_hub",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "signed-install-app",
				"name": "Signed Install App",
				"kind": "tool_app",
				"binding": {
					"skill": {"id": "signed-skill", "version": "1.0.0", "source": "enterprise_hub", "install_ref": "enterprise_hub://capabilities/cap-signed-skill@1.0.0"}
				},
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "template":"document_workspace", "density":"compact", "primaryRegion":"left", "outputRegion":"right", "regionCount":2, "visibleRegionCount":2, "regionIds":["input","output"], "fingerprint":"layout-tool-signed-install", "studio":{"editable":true, "savedInManifest":true, "updatedBy":"app_studio"}, "regions":[{"id":"input", "role":"input", "placement":"left"}, {"id":"output", "role":"output", "placement":"right"}]},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"document", "types":["document","content","artifact"], "delivery":{"inlineContent":true, "artifacts":true}},
					"dependencyVerification": {"schema":"maclaw.app.install_plan.v1", "dependencyCount":1, "requiredCount":1, "installedCount":1, "missingCount":0, "blockedCount":0, "ok":true, "blocked":false, "dependencies":[{"id":"signed-skill", "version":"1.0.0", "kind":"app_skill", "required":true, "source":"enterprise_hub", "install_ref":"enterprise_hub://capabilities/cap-signed-skill@1.0.0", "installed":true, "health":"ready", "action":"skip"}]},
					"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1", "fingerprint":"proto-signed-install", "sampleInput":{"sample":true}, "expectedOutput":{"document":"signed-output.pdf"}, "requiredRoles":["tester"], "requiredScopes":["app.run"], "riskLevel":"low"}, "testProtocolFingerprint":"proto-signed-install", "runId":"run-signed-install", "verifiedAt":"2026-06-30T08:00:00Z", "primaryResult":"document", "resultPayload":{"document":"signed-output.pdf", "content":"ok"}, "outputs":[{"kind":"document", "title":"Signed output", "text":"signed-output.pdf", "status":"ready"}], "artifacts":[{"id":"signed-output-pdf", "name":"signed-output.pdf", "uri":"artifact://signed/output.pdf", "status":"ready"}], "resultCoverage":{"ok":true, "primary":"document", "coveredTypes":["document","content","artifact"], "missingTypes":[]}}
				}
			}
		}]
	}`)
	var pkg map[string]any
	if err := json.Unmarshal([]byte(pkgJSON), &pkg); err != nil {
		t.Fatalf("decode app package: %v", err)
	}
	pkgSHA := strings.Repeat("c", 64)
	pkgPayload := "maclaw-app\n" + pkgSHA + "\nenterprise_hub:skill:maclaw-app:signed-install-app@pkg\n2026-06-30T08:00:00Z\nhub-admin"
	pkg["package_sha256"] = pkgSHA
	pkg["package_signature"] = map[string]any{
		"schema":                 "maclaw.app.package_signature.v1",
		"algorithm":              "ed25519",
		"payload":                pkgPayload,
		"public_key_base64":      base64.StdEncoding.EncodeToString(publicKey),
		"public_key_fingerprint": fingerprint,
		"signature_base64":       base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(pkgPayload))),
		"package_sha256":         pkgSHA,
		"version_key":            "enterprise_hub:skill:maclaw-app:signed-install-app@pkg",
		"signed_at":              "2026-06-30T08:00:00Z",
		"signed_by":              "hub-admin",
	}
	markMaclawAppPackageAsPublishedHubDownloadTest(t, pkg, "signed-install-app", "cap-signed-install", "enterprise_hub:skill:maclaw-app:signed-install-app@pkg")
	layoutFingerprints := normalizeMaclawAppPackageWorkspaceFingerprintsForTest(t, pkg)
	signedInstallLayoutFingerprint := layoutFingerprints["signed-install-app"]

	var servedSkillDownload bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer viewer-token" {
			t.Fatalf("Authorization = %q for %s", got, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/capabilities/maclaw-apps/cap-signed-install/package":
			_ = json.NewEncoder(w).Encode(pkg)
		case "/api/capabilities/cap-signed-skill":
			_ = json.NewEncoder(w).Encode(maclawapptest.PublishedEnterpriseHubSkillCapability("signed-skill", "cap-signed-skill", "1.0.0", skillSHA, skillSignatureJSON))
		case "/api/v1/skills/signed-skill/download":
			servedSkillDownload = true
			_, _ = w.Write(skillBody)
		case "/api/capabilities/inventory":
			if r.Method != http.MethodPut {
				t.Fatalf("inventory method = %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	result, err := app.InstallSelectedMaclawAppPackageFromHub("cap-signed-install", nil)
	if err != nil {
		t.Fatalf("InstallSelectedMaclawAppPackageFromHub() error = %v", err)
	}
	if !servedSkillDownload {
		t.Fatalf("expected signed dependency skill download")
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(cfg.TrustedSkillPackageKeyFingerprints) != 1 || normalizeDownloadedSkillPublicKeyFingerprint(cfg.TrustedSkillPackageKeyFingerprints[0]) != fingerprint {
		t.Fatalf("expected app package signature fingerprint to seed dependency trust: %#v", cfg.TrustedSkillPackageKeyFingerprints)
	}
	plan, ok := result["install_plan"].(maclawAppInstallPlan)
	if !ok || plan.HasMissingRequired || plan.HasBlockingDependency {
		t.Fatalf("signed dependency install plan should be ready: %#v", result["install_plan"])
	}
	dep := maclawAppPlanDepForTest(plan, "signed-skill")
	if dep == nil || !dep.Installed || dep.Health != "ready" || dep.PackageSHA256 != skillSHA || dep.PackageSignature == "" {
		t.Fatalf("signed dependency should be installed with integrity metadata: %#v", dep)
	}
	installRecord, ok := result["install_record"].(map[string]any)
	if !ok {
		t.Fatalf("signed tool install result should include install record: %#v", result)
	}
	registration := anyMap(installRecord["datasrv_registration"])
	if registration["status"] != "skipped" || registration["reason"] != "no datasrv role bindings" {
		t.Fatalf("tool app install should skip DataSrv registration without role bindings: %#v", registration)
	}
	installEvidence := anyMap(installRecord["install_evidence"])
	toolEvidence := anyMap(installEvidence["signed-install-app"])
	if toolEvidence["result_contract"] == nil || toolEvidence["workspace_layout"] == nil {
		t.Fatalf("signed tool install evidence should include result contract and workspace layout: %#v", installRecord)
	}
	records, err := app.ListMaclawAppInstalls(10)
	if err != nil {
		t.Fatalf("ListMaclawAppInstalls() error = %v", err)
	}
	if len(records) != 1 || records[0].AppID != "signed-install-app" || records[0].Kind != "tool_app" {
		t.Fatalf("signed tool app install should be persisted: %#v", records)
	}
	if records[0].WorkspaceLayout["fingerprint"] != signedInstallLayoutFingerprint || records[0].WorkspaceLayout["visibleRegionCount"] != float64(2) {
		t.Fatalf("signed tool install audit should preserve workspace layout evidence: %#v", records[0].WorkspaceLayout)
	}
	if records[0].ResultContract["primary"] != "document" {
		t.Fatalf("signed tool install audit should preserve document result contract: %#v", records[0].ResultContract)
	}
	if records[0].TestEvidence["primaryResult"] != "document" {
		t.Fatalf("signed tool install audit should preserve document test evidence: %#v", records[0].TestEvidence)
	}
	if artifacts := anySlice(records[0].TestEvidence["artifacts"]); len(artifacts) != 1 {
		t.Fatalf("signed tool install audit should preserve artifact evidence: %#v", records[0].TestEvidence)
	}
}
func TestInstallSignedHubApprovalAppRunsApprovalThroughDataSrv(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	fingerprint := downloadedSkillPublicKeyFingerprint(publicKey)
	appSkillBody, appSkillSHA, appSkillSignature, err := maclawapptest.SignedEnterpriseHubSkillPackage("expense-super-skill", "1.4.0", "run approval", publicKey, privateKey)
	if err != nil {
		t.Fatalf("signed app skill fixture: %v", err)
	}
	workflowBody, workflowSHA, workflowSignature, err := maclawapptest.SignedEnterpriseHubSkillPackage("expense-workflow", "2.1.0", "run approval", publicKey, privateKey)
	if err != nil {
		t.Fatalf("signed workflow skill fixture: %v", err)
	}

	pkgJSON := maclawAppPackageWithCurrentDefinitionHashes(t, `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"source": "enterprise_hub",
		"resolved_dependencies": [
			{"id":"expense-super-skill", "source":"enterprise_hub", "install_ref":"enterprise_hub://capabilities/cap-expense-super-skill@1.4.0", "kind":"app_skill", "required":true, "app_ids":["expense-approval"]},
			{"id":"expense-workflow", "source":"enterprise_hub", "install_ref":"enterprise_hub://capabilities/cap-expense-workflow@2.1.0", "kind":"workflow_skill", "required":true, "app_ids":["expense-approval"]}
		],
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "expense-approval",
				"name": "Expense Approval",
				"version": "1.4.0",
				"kind": "enterprise_approval_app",
				"binding": {
					"appSkill": {"id":"expense-super-skill", "version":"1.4.0", "source":"enterprise_hub", "install_ref":"enterprise_hub://capabilities/cap-expense-super-skill@1.4.0"},
					"datasrv": {"domain":"finance", "datasetID":"finance.expense_forms", "templateID":"finance.expense_form", "objectRole":"expense_report", "blueprintID":"finance.expense.v1"},
					"mis": {"approvalBindings": [{"event":"finance.submitted", "objectRole":"expense_report", "workflowSkillId":"expense-workflow", "workflowVersion":"2.1.0"}]},
					"workflow": {"schema":"maclaw.app.workflow.v1", "submitNode":"expense.submit", "approvalNode":"manager.approval", "resultNode":"expense.result"},
					"dependencies": {"skills": [
						{"id":"expense-super-skill", "kind":"app_skill", "version":"1.4.0", "required":true, "source":"enterprise_hub", "install_ref":"enterprise_hub://capabilities/cap-expense-super-skill@1.4.0"},
						{"id":"expense-workflow", "kind":"workflow_skill", "version":"2.1.0", "required":true, "source":"enterprise_hub", "install_ref":"enterprise_hub://capabilities/cap-expense-workflow@2.1.0"}
					]},
						"ui": {"schema":"maclaw.app.ui.v1", "entry":"approval_workspace", "layouts":{"approval_workspace":{"template":"left_nav", "density":"compact", "primaryRegion":"center", "outputRegion":"bottom", "regionIds":["approval_inbox","request_form","approval_detail","result_panel"], "visibleRegionCount":3, "fingerprint":"layout-expense-golden", "studio":{"editable":true, "savedInManifest":true, "updatedBy":"app_studio"}, "regions":[{"id":"approval_inbox", "role":"instance_list", "placement":"left", "order":1}, {"id":"request_form", "role":"input", "placement":"center", "order":2}, {"id":"approval_detail", "role":"detail", "placement":"right", "visible":false, "order":3}, {"id":"result_panel", "role":"output", "placement":"bottom", "order":4}]}}},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"approval_result", "types":["approval_result", "business_status", "business_record", "artifact"], "delivery":{"inlineContent":true, "artifacts":true}}
				},
				"governance": {
						"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"approval_workspace", "template":"left_nav", "density":"compact", "primaryRegion":"center", "outputRegion":"bottom", "regionCount":4, "visibleRegionCount":3, "regionIds":["approval_inbox","request_form","approval_detail","result_panel"], "fingerprint":"layout-expense-golden", "studio":{"editable":true, "savedInManifest":true, "updatedBy":"app_studio"}, "regions":[{"id":"approval_inbox", "role":"instance_list", "placement":"left", "order":1}, {"id":"request_form", "role":"input", "placement":"center", "order":2}, {"id":"approval_detail", "role":"detail", "placement":"right", "visible":false, "order":3}, {"id":"result_panel", "role":"output", "placement":"bottom", "order":4}]},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"approval_result", "types":["approval_result", "business_status", "business_record", "artifact"], "delivery":{"inlineContent":true, "artifacts":true}},
					"testProtocol": {"schema":"maclaw.app.test_protocol.v1", "fingerprint":"proto-expense-golden", "sampleInput":{"amount":860, "applicant":"alice"}, "expectedOutput":{"approval_result":"approved", "business_status":"finance_approved"}},
					"workflowContract": {"schema":"maclaw.app.workflow_contract.v1", "workflowSkillId":"expense-workflow", "workflowVersion":"2.1.0", "objectRole":"expense_report", "requiredInputs":["record_ref", "applicant", "business_payload"], "decisionOutputs":["approved", "rejected", "attention"], "statusMapping":{"pending":"finance_pending", "approved":"finance_approved", "rejected":"finance_rejected", "attention":"finance_attention"}},
					"dependencyVerification":{"schema":"maclaw.app.install_plan.v1", "dependencyCount":2, "hasMissingRequired":false, "hasBlockingDependency":false, "hasWorkflowContractIssue":false, "hasGovernanceReviewIssue":false, "dependencies":[{"id":"expense-super-skill", "kind":"app_skill", "required":true, "installed":true, "health":"ready", "action":"skip"}, {"id":"expense-workflow", "kind":"workflow_skill", "required":true, "installed":true, "health":"ready", "action":"skip"}]},
					"testEvidence": {"runId":"run-expense-golden", "testProtocolFingerprint":"proto-expense-golden", "primaryResult":"approval_result", "resultPayload":{"approval_result":"approved", "business_status":"finance_approved", "record_id":"expense-golden-1"}, "outputs":[{"kind":"approval_result", "title":"Decision", "text":"approved", "status":"ready"}], "artifacts":[{"id":"artifact-expense-golden", "name":"expense-golden.pdf", "uri":"artifact://expense/golden.pdf", "status":"ready"}], "resultCoverage":{"ok":true, "primary":"approval_result", "coveredTypes":["approval_result", "business_status", "business_record", "artifact"], "missingTypes":[]}, "dependencyVerification":{"schema":"maclaw.app.install_plan.v1", "dependencyCount":2, "hasMissingRequired":false, "hasBlockingDependency":false, "hasWorkflowContractIssue":false, "hasGovernanceReviewIssue":false}, "approvalInstance":{"instanceId":"wf-evidence-golden", "approvalID":"approval-evidence-golden", "recordID":"expense-golden-1", "datasetID":"finance.expense_forms", "blueprintID":"finance.expense.v1", "objectRole":"expense_report", "approvalEvent":"finance.submitted", "workflowSkillId":"expense-workflow", "workflowVersion":"2.1.0", "status":"approved", "currentNode":"expense.result", "businessStatus":"finance_approved", "resultStatus":"approved", "viewVerified":true, "resultPayload":{"approval_result":"approved", "business_status":"finance_approved", "business_record":{"id":"expense-golden-1"}}, "outputs":[{"kind":"approval_result", "title":"Decision", "text":"approved", "status":"ready"}], "artifacts":[{"id":"artifact-expense-golden", "name":"expense-golden.pdf", "uri":"artifact://expense/golden.pdf", "status":"ready"}]}},
					"reviewEvidence": {"run_id":"run-expense-golden", "test_protocol_fingerprint":"proto-expense-golden", "result_contract_primary":"approval_result", "result_coverage_primary":"approval_result", "result_coverage_covered_count":4, "result_coverage_missing_count":0, "output_count":1, "artifact_count":1, "approval_status":"approved", "current_node":"expense.result"},
					"submission": {"channel":"hub", "status":"published", "capability_id":"cap-expense-approval", "market_capability_id":"expense-approval", "submission_id":"enterprise_hub:skill:maclaw-app:expense-approval@pkg", "version_key":"enterprise_hub:skill:maclaw-app:expense-approval@pkg", "package_sha256":"pkg-expense-approval-golden", "review_evidence":{"expense-approval":{"run_id":"run-expense-golden", "approval_status":"approved", "current_node":"expense.result"}}}
				}
			}
		}]
	}`)
	var pkg map[string]any
	if err := json.Unmarshal([]byte(pkgJSON), &pkg); err != nil {
		t.Fatalf("decode app package: %v", err)
	}
	pkg["capability"] = map[string]any{
		"id":                  "cap-expense-approval",
		"capability_id":       "expense-approval",
		"display_name":        "Expense Approval",
		"status":              "published",
		"current_version_key": "enterprise_hub:skill:maclaw-app:expense-approval@pkg",
	}
	pkg["review_evidence"] = map[string]any{
		"expense-approval": map[string]any{
			"run_id":                        "run-expense-golden",
			"test_protocol_fingerprint":     "proto-expense-golden",
			"result_contract_primary":       "approval_result",
			"result_coverage_primary":       "approval_result",
			"result_coverage_covered_count": 4,
			"result_coverage_missing_count": 0,
			"output_count":                  1,
			"artifact_count":                1,
			"approval_status":               "approved",
			"current_node":                  "expense.result",
		},
	}
	pkg["maclaw_app_review_evidence"] = pkg["review_evidence"]
	pkgSHA := strings.Repeat("d", 64)
	pkgPayload := "maclaw-app\n" + pkgSHA + "\nenterprise_hub:skill:maclaw-app:expense-approval@pkg\n2026-06-30T09:00:00Z\nhub-admin"
	pkg["package_sha256"] = pkgSHA
	pkg["package_signature"] = map[string]any{
		"schema":                 "maclaw.app.package_signature.v1",
		"algorithm":              "ed25519",
		"payload":                pkgPayload,
		"public_key_base64":      base64.StdEncoding.EncodeToString(publicKey),
		"public_key_fingerprint": fingerprint,
		"signature_base64":       base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(pkgPayload))),
		"package_sha256":         pkgSHA,
		"version_key":            "enterprise_hub:skill:maclaw-app:expense-approval@pkg",
		"signed_at":              "2026-06-30T09:00:00Z",
		"signed_by":              "hub-admin",
	}
	if apps := anySlice(pkg["apps"]); len(apps) == 1 {
		entry := anyMap(apps[0])
		appMap := anyMap(entry["app"])
		governance := anyMap(appMap["governance"])
		submission := anyMap(governance["submission"])
		submission["package_signature"] = pkg["package_signature"]
	}
	layoutFingerprints := normalizeMaclawAppPackageWorkspaceFingerprintsForTest(t, pkg)
	expenseLayoutFingerprint := layoutFingerprints["expense-approval"]

	remoteFinal := false
	supplementalFinal := false
	supplementalCreateCount := 0
	var dataSrvRequests []string
	var dataSrvApprovalSyncs []struct {
		Path string
		Body map[string]any
	}
	var appInstallationPayload map[string]any
	dataSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dataSrvRequests = append(dataSrvRequests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		captureApprovalSync := func() {
			body := map[string]any{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode approval sync body for %s: %v", r.URL.Path, err)
			}
			dataSrvApprovalSyncs = append(dataSrvApprovalSyncs, struct {
				Path string
				Body map[string]any
			}{Path: r.URL.Path, Body: body})
		}
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/data/app-installations/expense-approval":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read app installation body: %v", err)
			}
			if err := json.Unmarshal(body, &appInstallationPayload); err != nil {
				t.Fatalf("decode app installation body: %v body=%s", err, string(body))
			}
			_, _ = w.Write([]byte(`{"app_id":"expense-approval","status":"installed"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/app-installations":
			items := []map[string]any{}
			metadata := anyMap(appInstallationPayload["metadata"])
			matches := appInstallationPayload != nil
			if value := r.URL.Query().Get("hub_capability_id"); value != "" && metadata["hub_capability_id"] != value {
				matches = false
			}
			if value := r.URL.Query().Get("hub_version_key"); value != "" && metadata["hub_version_key"] != value {
				matches = false
			}
			if value := r.URL.Query().Get("hub_review_status"); value != "" && metadata["hub_review_status"] != value {
				matches = false
			}
			if value := r.URL.Query().Get("workspace_layout_fingerprint"); value != "" && metadata["workspace_layout_fingerprint"] != value {
				matches = false
			}
			if matches {
				items = append(items, appInstallationPayload)
			}
			if err := json.NewEncoder(w).Encode(map[string]any{"items": items}); err != nil {
				t.Fatalf("encode app installation query response: %v", err)
			}
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-golden-1":
			_, _ = w.Write([]byte(`{"id":"expense-golden-1","data":{"status":"draft","amount":860}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-golden-1":
			_, _ = w.Write([]byte(`{"id":"expense-golden-1","status":"updated"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-golden-2":
			_, _ = w.Write([]byte(`{"id":"expense-golden-2","data":{"status":"draft","amount":280}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-golden-2":
			_, _ = w.Write([]byte(`{"id":"expense-golden-2","status":"updated"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-golden-1/approvals":
			_, _ = w.Write([]byte(`{"id":"approval-golden-1","status":"pending"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/datasets/finance.expense_forms/records/expense-golden-2/approvals":
			supplementalCreateCount++
			_, _ = w.Write([]byte(`{"id":"approval-golden-2","status":"pending"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/approvals/approval-golden-1/progress":
			captureApprovalSync()
			_, _ = w.Write([]byte(`{"id":"approval-golden-1","status":"pending","progress":"director review"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/approvals/approval-golden-2/progress":
			captureApprovalSync()
			_, _ = w.Write([]byte(`{"id":"approval-golden-2","status":"pending","progress":"supplemental input"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/approvals/approval-golden-1/review":
			captureApprovalSync()
			remoteFinal = true
			_, _ = w.Write([]byte(`{"id":"approval-golden-1","status":"approved"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/data/approvals/approval-golden-2/review":
			captureApprovalSync()
			supplementalFinal = true
			_, _ = w.Write([]byte(`{"id":"approval-golden-2","status":"approved"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/approvals":
			if supplementalFinal {
				_, _ = w.Write([]byte(`{"items":[{"id":"approval-golden-1","app_id":"expense-approval","dataset_id":"finance.expense_forms","record_id":"expense-golden-1","status":"approved","summary":"Golden Expense","workflow_skill_id":"expense-workflow","workflow_version":"2.1.0","workflow_instance_id":"wf-golden-1","workflow_node_id":"expense.result","workflow_node_ids":["expense.submit","manager.approval","expense.result"],"business_status":"finance_approved","result_status":"approved","result_payload":{"approval_result":"approved","business_status":"finance_approved","business_record":{"id":"expense-golden-1","status":"finance_approved"},"text":"approved by manager"},"outputs":[{"type":"content","title":"Approval Decision","text":"approved by manager"},{"type":"artifact","title":"Approval PDF","artifact":{"id":"artifact-golden-pdf","name":"expense-golden-approved.pdf","uri":"artifact://expense/golden-approved.pdf","status":"ready"}}],"artifacts":[{"id":"artifact-golden-pdf","name":"expense-golden-approved.pdf","uri":"artifact://expense/golden-approved.pdf","status":"ready"}],"request":{"approval_instance_id":"wf-golden-1","appID":"expense-approval","blueprintID":"finance.expense.v1","objectRole":"expense_report","approvalEvent":"finance.submitted","applicant":"alice","currentAssignee":"manager","currentAssigneeType":"user","workflowSkillId":"expense-workflow","workflowVersion":"2.1.0","workflowNodeId":"expense.result","workflowNodeIds":["expense.submit","manager.approval","expense.result"],"businessStatus":"finance_approved","resultStatus":"approved"},"created_by":"alice","submitted_by":"alice","reviewed_by":"manager","assigned_to":"manager","created_at":"2026-06-30T09:00:00Z","updated_at":"2026-06-30T09:03:00Z"},{"id":"approval-golden-2","app_id":"expense-approval","dataset_id":"finance.expense_forms","record_id":"expense-golden-2","status":"approved","summary":"Supplemented Expense","lane":"handled","workflow_skill_id":"expense-workflow","workflow_version":"2.1.0","workflow_instance_id":"wf-golden-2","workflow_decision_id":"decision-golden-2","workflow_node_id":"expense.result","workflow_node_ids":["expense.require_input","manager.approval","expense.result"],"business_status":"finance_approved","result_status":"approved","result_payload":{"approval_result":"approved","business_status":"finance_approved","business_record":{"id":"expense-golden-2","status":"finance_approved"},"text":"approved after supplemental input"},"outputs":[{"type":"content","title":"Workflow Decision","text":"approved after supplemental input","status":"approved"}],"request":{"approval_instance_id":"wf-golden-2","appID":"expense-approval","objectRole":"expense_report","approvalEvent":"finance.submitted","workflowSkillId":"expense-workflow","workflowVersion":"2.1.0","workflowNodeId":"expense.result","workflowNodeIds":["expense.require_input","manager.approval","expense.result"],"businessStatus":"finance_approved","resultStatus":"approved"},"created_by":"alice","submitted_by":"alice","reviewed_by":"manager","assigned_to":"manager","created_at":"2026-06-30T09:10:00Z","updated_at":"2026-06-30T09:13:00Z"}]}`))
				return
			}
			if remoteFinal {
				_, _ = w.Write([]byte(`{"items":[{"id":"approval-golden-1","app_id":"expense-approval","dataset_id":"finance.expense_forms","record_id":"expense-golden-1","status":"approved","summary":"Golden Expense","workflow_skill_id":"expense-workflow","workflow_version":"2.1.0","workflow_instance_id":"wf-golden-1","workflow_node_id":"expense.result","workflow_node_ids":["expense.submit","manager.approval","expense.result"],"business_status":"finance_approved","result_status":"approved","result_payload":{"approval_result":"approved","business_status":"finance_approved","business_record":{"id":"expense-golden-1","status":"finance_approved"},"text":"approved by manager"},"outputs":[{"type":"content","title":"Approval Decision","text":"approved by manager"},{"type":"artifact","title":"Approval PDF","artifact":{"id":"artifact-golden-pdf","name":"expense-golden-approved.pdf","uri":"artifact://expense/golden-approved.pdf","status":"ready"}}],"artifacts":[{"id":"artifact-golden-pdf","name":"expense-golden-approved.pdf","uri":"artifact://expense/golden-approved.pdf","status":"ready"}],"request":{"approval_instance_id":"wf-golden-1","appID":"expense-approval","blueprintID":"finance.expense.v1","objectRole":"expense_report","approvalEvent":"finance.submitted","applicant":"alice","currentAssignee":"manager","currentAssigneeType":"user","workflowSkillId":"expense-workflow","workflowVersion":"2.1.0","workflowNodeId":"expense.result","workflowNodeIds":["expense.submit","manager.approval","expense.result"],"businessStatus":"finance_approved","resultStatus":"approved"},"created_by":"alice","submitted_by":"alice","reviewed_by":"manager","assigned_to":"manager","created_at":"2026-06-30T09:00:00Z","updated_at":"2026-06-30T09:03:00Z"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"items":[{"id":"approval-golden-1","app_id":"expense-approval","dataset_id":"finance.expense_forms","record_id":"expense-golden-1","status":"pending","summary":"Golden Expense","workflow_skill_id":"expense-workflow","workflow_version":"2.1.0","workflow_instance_id":"wf-golden-1","workflow_node_id":"manager.approval","workflow_node_ids":["expense.submit","manager.approval"],"business_status":"finance_pending","result_status":"running","result_payload":{"text":"waiting for manager","business_record":{"id":"expense-golden-1"}},"outputs":[{"type":"content","title":"Running Progress","text":"waiting for manager"}],"request":{"approval_instance_id":"wf-golden-1","appID":"expense-approval","objectRole":"expense_report","approvalEvent":"finance.submitted","workflowSkillId":"expense-workflow","workflowVersion":"2.1.0","workflowNodeId":"manager.approval","workflowNodeIds":["expense.submit","manager.approval"],"businessStatus":"finance_pending","resultStatus":"running"},"created_by":"alice","submitted_by":"alice","assigned_to":"manager","created_at":"2026-06-30T09:00:00Z","updated_at":"2026-06-30T09:01:00Z"}]}`))
		default:
			t.Fatalf("unexpected DataSrv request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer dataSrv.Close()

	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer viewer-token" {
			t.Fatalf("Authorization = %q for %s", got, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/capabilities/maclaw-apps/cap-expense-approval/package":
			_ = json.NewEncoder(w).Encode(pkg)
		case "/api/capabilities/cap-expense-super-skill":
			_ = json.NewEncoder(w).Encode(maclawapptest.PublishedEnterpriseHubSkillCapability("expense-super-skill", "cap-expense-super-skill", "1.4.0", appSkillSHA, appSkillSignature))
		case "/api/capabilities/cap-expense-workflow":
			_ = json.NewEncoder(w).Encode(maclawapptest.PublishedEnterpriseHubSkillCapability("expense-workflow", "cap-expense-workflow", "2.1.0", workflowSHA, workflowSignature))
		case "/api/v1/skills/expense-super-skill/download":
			_, _ = w.Write(appSkillBody)
		case "/api/v1/skills/expense-workflow/download":
			_, _ = w.Write(workflowBody)
		case "/api/capabilities/inventory":
			if r.Method != http.MethodPut {
				t.Fatalf("inventory method = %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			t.Fatalf("unexpected Hub path: %s", r.URL.Path)
		}
	}))
	defer hub.Close()

	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: hub.URL, RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: dataSrv.URL, Token: "data-token", TenantID: "tenant", UserID: "alice", Role: "data_admin"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	installed, err := app.InstallSelectedMaclawAppPackageFromHub("cap-expense-approval", []string{"expense-approval"})
	if err != nil {
		t.Fatalf("InstallSelectedMaclawAppPackageFromHub() error = %v", err)
	}
	installedPackage := anyMap(installed["package"])
	packageCapability := anyMap(installedPackage["capability"])
	packageReviewEvidence := anyMap(installedPackage["review_evidence"])
	packageAppEvidence := anyMap(packageReviewEvidence["expense-approval"])
	if installedPackage["source"] != "enterprise_hub" ||
		packageCapability["id"] != "cap-expense-approval" ||
		packageCapability["status"] != "published" ||
		packageCapability["current_version_key"] != "enterprise_hub:skill:maclaw-app:expense-approval@pkg" ||
		packageAppEvidence["run_id"] != "run-expense-golden" ||
		packageAppEvidence["approval_status"] != "approved" {
		t.Fatalf("installed selected package should preserve Hub published package identity and review evidence: %#v", installedPackage)
	}
	installedPackageApps := anySlice(installedPackage["apps"])
	if len(installedPackageApps) != 1 {
		t.Fatalf("installed selected package should preserve exactly one selected app entry: %#v", installedPackage["apps"])
	}
	installedEntry := anyMap(installedPackageApps[0])
	installedEntryApp := anyMap(installedEntry["app"])
	installedEntryGovernance := anyMap(installedEntryApp["governance"])
	installedSubmission := anyMap(installedEntryGovernance["submission"])
	installedSubmissionReviewEvidence := anyMap(installedSubmission["review_evidence"])
	installedSubmissionAppEvidence := anyMap(installedSubmissionReviewEvidence["expense-approval"])
	if installedSubmission["status"] != "published" ||
		installedSubmission["capability_id"] != "cap-expense-approval" ||
		installedSubmission["market_capability_id"] != "expense-approval" ||
		installedSubmission["version_key"] != "enterprise_hub:skill:maclaw-app:expense-approval@pkg" ||
		len(anyMap(installedSubmission["package_signature"])) == 0 ||
		installedSubmissionAppEvidence["run_id"] != "run-expense-golden" {
		t.Fatalf("installed selected package entry should preserve Hub submission identity and review evidence: %#v", installedSubmission)
	}
	plan, ok := installed["install_plan"].(maclawAppInstallPlan)
	if !ok || plan.HasMissingRequired || plan.HasBlockingDependency {
		t.Fatalf("signed approval install plan should be ready: %#v", installed["install_plan"])
	}
	for _, id := range []string{"expense-super-skill", "expense-workflow"} {
		dep := maclawAppPlanDepForTest(plan, id)
		if dep == nil || !dep.Installed || dep.Health != "ready" || dep.PackageSHA256 == "" || dep.PackageSignature == "" {
			t.Fatalf("signed dependency %s should be installed with integrity metadata: %#v", id, dep)
		}
	}
	assertMaclawAppDependencyInstallTraceReadyForTest(t, "signed approval install plan", maclawAppDependencyInstallTraceSummary(plan.Dependencies), 2, 0)
	installHistory, err := app.ListMaclawAppInstalls(5)
	if err != nil {
		t.Fatalf("ListMaclawAppInstalls() error = %v", err)
	}
	if len(installHistory) == 0 || installHistory[0].AppID != "expense-approval" {
		t.Fatalf("signed approval app install should be persisted in local audit history: %#v", installHistory)
	}
	installRecord := installHistory[0]
	if installRecord.Kind != "enterprise_approval_app" || installRecord.Source != "enterprise_hub" || installRecord.PackageSHA == "" {
		t.Fatalf("install audit should preserve app identity and package hash: %#v", installRecord)
	}
	if len(installRecord.Dependencies) != 2 || installRecord.HasMissingRequired || installRecord.HasBlockingDependency {
		t.Fatalf("install audit should preserve ready dependency plan: %#v", installRecord.Dependencies)
	}
	assertMaclawAppDependencyInstallTraceReadyForTest(t, "signed approval install audit", anyMap(firstNonEmptyMaclawAppAny(installRecord.DependencyVerification["install_trace"], installRecord.DependencyVerification["installTrace"])), 2, 0)
	if installRecord.DataSrvRegistration["synced"] != true || fmt.Sprint(installRecord.DataSrvRegistration["synced_count"]) != "1" {
		t.Fatalf("install audit should preserve successful DataSrv registration: %#v", installRecord.DataSrvRegistration)
	}
	registrationItems := anySlice(installRecord.DataSrvRegistration["items"])
	registrationItem := map[string]any{}
	if len(registrationItems) > 0 {
		registrationItem = anyMap(registrationItems[0])
	}
	if registrationItem["app_id"] != "expense-approval" || registrationItem["status"] != "installed" || registrationItem["synced"] != true {
		t.Fatalf("install audit should preserve DataSrv registration item: %#v", installRecord.DataSrvRegistration)
	}
	if installRecord.WorkflowContract["workflowSkillId"] != "expense-workflow" && installRecord.WorkflowContract["workflow_skill_id"] != "expense-workflow" {
		t.Fatalf("install audit should preserve approval workflow contract: %#v", installRecord.WorkflowContract)
	}
	if len(installRecord.VersionSnapshot.ApprovalBindings) != 1 ||
		installRecord.VersionSnapshot.ApprovalBindings[0].DatasetID != "finance.expense_forms" ||
		installRecord.VersionSnapshot.ApprovalBindings[0].BlueprintID != "finance.expense.v1" ||
		installRecord.VersionSnapshot.ApprovalBindings[0].ObjectRole != "expense_report" ||
		installRecord.VersionSnapshot.ApprovalBindings[0].WorkflowSkillID != "expense-workflow" ||
		installRecord.VersionSnapshot.ApprovalBindings[0].WorkflowVersion != "2.1.0" {
		t.Fatalf("install audit should preserve approval binding runtime snapshot: %#v", installRecord.VersionSnapshot.ApprovalBindings)
	}
	if installRecord.WorkspaceLayout["entry"] != "approval_workspace" || installRecord.ResultContract["primary"] != "approval_result" {
		t.Fatalf("install audit should preserve UI layout and result contract: layout=%#v result=%#v", installRecord.WorkspaceLayout, installRecord.ResultContract)
	}
	if installRecord.WorkspaceLayout["template"] != "left_nav" || installRecord.WorkspaceLayout["density"] != "compact" || installRecord.WorkspaceLayout["fingerprint"] != expenseLayoutFingerprint {
		t.Fatalf("install audit should preserve App Studio workspace layout identity: %#v", installRecord.WorkspaceLayout)
	}
	if fmt.Sprint(installRecord.WorkspaceLayout["regionCount"]) != "4" || fmt.Sprint(installRecord.WorkspaceLayout["visibleRegionCount"]) != "3" {
		t.Fatalf("install audit should preserve workspace layout region counts: %#v", installRecord.WorkspaceLayout)
	}
	if gotRegionIDs := strings.Join(maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(installRecord.WorkspaceLayout["regionIds"], installRecord.WorkspaceLayout["region_ids"])), ","); gotRegionIDs != "approval_inbox,request_form,approval_detail,result_panel" {
		t.Fatalf("install audit should preserve workspace layout region order, got %q in %#v", gotRegionIDs, installRecord.WorkspaceLayout)
	}
	studioLayout := anyMap(installRecord.WorkspaceLayout["studio"])
	if studioLayout["editable"] != true || studioLayout["savedInManifest"] != true || studioLayout["updatedBy"] != "app_studio" {
		t.Fatalf("install audit should preserve App Studio layout edit evidence: %#v", installRecord.WorkspaceLayout)
	}
	appInstallationMetadata := anyMap(appInstallationPayload["metadata"])
	if appInstallationPayload["app_id"] != "expense-approval" || appInstallationMetadata["workspace_layout_fingerprint"] != expenseLayoutFingerprint {
		t.Fatalf("DataSrv app installation should receive workspace layout fingerprint metadata: payload=%#v metadata=%#v", appInstallationPayload, appInstallationMetadata)
	}
	if appInstallationMetadata["workspace_layout_template"] != "left_nav" || appInstallationMetadata["workspace_layout_density"] != "compact" || appInstallationMetadata["workspace_layout_primary_region"] != "center" || appInstallationMetadata["workspace_layout_output_region"] != "bottom" {
		t.Fatalf("DataSrv app installation should receive workspace layout placement metadata: %#v", appInstallationMetadata)
	}
	if fmt.Sprint(appInstallationMetadata["workspace_layout_region_count"]) != "4" || fmt.Sprint(appInstallationMetadata["workspace_layout_visible_region_count"]) != "3" {
		t.Fatalf("DataSrv app installation should receive workspace layout region counts: %#v", appInstallationMetadata)
	}
	if gotRegionIDs := strings.Join(maclawAppStringListFromAny(appInstallationMetadata["workspace_layout_region_ids"]), ","); gotRegionIDs != "approval_inbox,request_form,approval_detail,result_panel" {
		t.Fatalf("DataSrv app installation should receive workspace layout region order, got %q in %#v", gotRegionIDs, appInstallationMetadata)
	}
	if appInstallationMetadata["hub_capability_id"] != "cap-expense-approval" || appInstallationMetadata["hub_version_key"] != "enterprise_hub:skill:maclaw-app:expense-approval@pkg" || appInstallationMetadata["hub_package_sha256"] != "pkg-expense-approval-golden" || appInstallationMetadata["hub_review_status"] != "published" {
		t.Fatalf("DataSrv app installation should receive Hub published identity metadata: %#v", appInstallationMetadata)
	}
	dataSrvSubmission := anyMap(appInstallationMetadata["submission"])
	dataSrvSubmissionSignature := anyMap(dataSrvSubmission["package_signature"])
	if dataSrvSubmission["capability_id"] != "cap-expense-approval" ||
		dataSrvSubmissionSignature["package_sha256"] != pkgSHA ||
		dataSrvSubmissionSignature["public_key_fingerprint"] != fingerprint {
		t.Fatalf("DataSrv app installation should receive Hub submission package signature: %#v", appInstallationMetadata)
	}
	dataSrvHubSignature := anyMap(appInstallationMetadata["hub_package_signature"])
	if dataSrvHubSignature["package_sha256"] != pkgSHA ||
		appInstallationMetadata["hub_package_signature_algorithm"] != "ed25519" ||
		appInstallationMetadata["hub_package_signature_fingerprint"] != fingerprint ||
		appInstallationMetadata["hub_package_signature_signed_at"] != "2026-06-30T09:00:00Z" ||
		appInstallationMetadata["hub_package_signature_signed_by"] != "hub-admin" {
		t.Fatalf("DataSrv app installation should receive flattened Hub package signature summaries: %#v", appInstallationMetadata)
	}
	if appInstallationMetadata["review_evidence_run_id"] != "run-expense-golden" || appInstallationMetadata["review_evidence_approval_status"] != "approved" || appInstallationMetadata["review_evidence_current_node"] != "expense.result" {
		t.Fatalf("DataSrv app installation should receive Hub review evidence summaries: %#v", appInstallationMetadata)
	}
	if appInstallationMetadata["result_contract_primary"] != "approval_result" || appInstallationMetadata["test_evidence_result_coverage_primary"] != "approval_result" || appInstallationMetadata["test_evidence_result_coverage_covered_count"] != float64(4) || appInstallationMetadata["test_evidence_artifact_count"] != float64(1) {
		t.Fatalf("DataSrv app installation should receive result contract and test coverage summaries: %#v", appInstallationMetadata)
	}
	if appInstallationMetadata["dependency_count"] != float64(2) || appInstallationMetadata["has_missing_required_dependency"] != false || appInstallationMetadata["has_blocking_dependency"] != false {
		t.Fatalf("DataSrv app installation should receive dependency verification summaries: %#v", appInstallationMetadata)
	}
	assertMaclawAppDependencyInstallTraceReadyForTest(t, "signed DataSrv app installation metadata", anyMap(appInstallationMetadata["dependency_install_trace"]), 2, 0)
	if maclawAppIntValueForTest(appInstallationMetadata["dependency_preflight_checked_count"]) != 2 ||
		maclawAppIntValueForTest(appInstallationMetadata["dependency_preflight_ready_count"]) != 2 ||
		maclawAppIntValueForTest(appInstallationMetadata["dependency_integrity_checked_count"]) != 2 ||
		maclawAppIntValueForTest(appInstallationMetadata["dependency_integrity_ready_count"]) != 2 ||
		maclawAppIntValueForTest(appInstallationMetadata["dependency_download_available_count"]) != 0 ||
		maclawAppIntValueForTest(appInstallationMetadata["dependency_signature_available_count"]) != 2 ||
		maclawAppIntValueForTest(appInstallationMetadata["dependency_install_error_count"]) != 0 ||
		appInstallationMetadata["dependency_install_trace_ok"] != true {
		t.Fatalf("DataSrv app installation should receive flattened dependency install trace summaries: %#v", appInstallationMetadata)
	}
	queryURL := dataSrv.URL + "/api/v1/data/app-installations?hub_capability_id=cap-expense-approval&hub_version_key=" + url.QueryEscape("enterprise_hub:skill:maclaw-app:expense-approval@pkg") + "&hub_review_status=published&workspace_layout_fingerprint=" + url.QueryEscape(expenseLayoutFingerprint)
	queryReq, err := http.NewRequest(http.MethodGet, queryURL, nil)
	if err != nil {
		t.Fatalf("create signed DataSrv app installation query: %v", err)
	}
	queryReq.Header.Set("Authorization", "Bearer data-token")
	queryResp, err := http.DefaultClient.Do(queryReq)
	if err != nil {
		t.Fatalf("query signed DataSrv app installation by Hub/layout identity: %v", err)
	}
	defer queryResp.Body.Close()
	if queryResp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(queryResp.Body)
		t.Fatalf("signed DataSrv app installation query status=%d body=%s", queryResp.StatusCode, string(bodyBytes))
	}
	var queriedInstallations struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(queryResp.Body).Decode(&queriedInstallations); err != nil {
		t.Fatalf("decode signed DataSrv app installation query: %v", err)
	}
	if len(queriedInstallations.Items) != 1 || queriedInstallations.Items[0]["app_id"] != "expense-approval" {
		t.Fatalf("signed DataSrv app installation query should return installed approval app: %#v", queriedInstallations.Items)
	}
	queriedMetadata := anyMap(queriedInstallations.Items[0]["metadata"])
	if queriedMetadata["hub_capability_id"] != "cap-expense-approval" || queriedMetadata["hub_review_status"] != "published" || queriedMetadata["workspace_layout_fingerprint"] != expenseLayoutFingerprint || queriedMetadata["review_evidence_run_id"] != "run-expense-golden" {
		t.Fatalf("signed DataSrv query should preserve Hub review and layout summaries: %#v", queriedMetadata)
	}
	if anyMap(queriedMetadata["hub_package_signature"])["public_key_fingerprint"] != fingerprint || queriedMetadata["hub_package_signature_signed_by"] != "hub-admin" {
		t.Fatalf("signed DataSrv query should preserve Hub package signature summaries: %#v", queriedMetadata)
	}
	queriedVersionSnapshot := anyMap(queriedMetadata["version_snapshot"])
	queriedApprovalBindings := anySlice(queriedVersionSnapshot["approval_bindings"])
	if len(queriedApprovalBindings) != 1 {
		t.Fatalf("signed DataSrv query should preserve approval binding snapshot: %#v", queriedVersionSnapshot)
	}
	queriedApprovalBinding := anyMap(queriedApprovalBindings[0])
	if queriedApprovalBinding["dataset_id"] != "finance.expense_forms" ||
		queriedApprovalBinding["blueprint_id"] != "finance.expense.v1" ||
		queriedApprovalBinding["object_role"] != "expense_report" ||
		queriedApprovalBinding["workflow_skill_id"] != "expense-workflow" ||
		queriedApprovalBinding["workflow_version"] != "2.1.0" {
		t.Fatalf("signed DataSrv query should preserve approval binding runtime identity: %#v", queriedApprovalBinding)
	}
	assertMaclawAppDependencyInstallTraceReadyForTest(t, "signed DataSrv app installation query", anyMap(queriedMetadata["dependency_install_trace"]), 2, 0)
	approvalEvidence := anyMap(installRecord.TestEvidence["approval_instance"])
	if len(approvalEvidence) == 0 {
		approvalEvidence = anyMap(installRecord.TestEvidence["approvalInstance"])
	}
	if approvalEvidence["approval_id"] != "approval-evidence-golden" && approvalEvidence["approvalID"] != "approval-evidence-golden" {
		t.Fatalf("install audit should preserve imported approval test evidence: %#v", installRecord.TestEvidence)
	}
	if installRecord.Submission["capability_id"] != "cap-expense-approval" || installRecord.Submission["version_key"] != "enterprise_hub:skill:maclaw-app:expense-approval@pkg" || installRecord.Submission["status"] != "published" {
		t.Fatalf("signed install audit should preserve Hub submission identity: %#v", installRecord.Submission)
	}
	if installRecord.ReviewEvidence["run_id"] != "run-expense-golden" || installRecord.ReviewEvidence["approval_status"] != "approved" || installRecord.ReviewEvidence["current_node"] != "expense.result" {
		t.Fatalf("signed install audit should preserve Hub review evidence: %#v", installRecord.ReviewEvidence)
	}

	started, err := app.StartMaclawAppApprovalWorkflow(MaclawAppApprovalWorkflowStartInput{AppID: "expense-approval", RecordID: "expense-golden-1", Title: "Golden Expense", Applicant: "alice", Approver: "manager", BusinessNote: "golden sample", BusinessPayload: map[string]any{"amount": float64(860), "currency": "CNY"}, FormData: map[string]any{"reason": "travel"}})
	if err != nil {
		t.Fatalf("StartMaclawAppApprovalWorkflow() error = %v", err)
	}
	if started["started"] != true || started["approval_id"] != "approval-golden-1" || started["workflow_skill_id"] != "expense-workflow" || started["workflow_version"] != "2.1.0" {
		t.Fatalf("unexpected start result: %#v", started)
	}
	pending, err := app.ListMaclawAppApprovalInstances("expense-approval", "all", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances running error = %v", err)
	}
	if len(pending) != 1 || pending[0].ApprovalID != "approval-golden-1" || pending[0].CurrentNode != "manager.approval" || pending[0].ResultStatus != "running" {
		t.Fatalf("signed installed approval app should read running DataSrv node: %#v", pending)
	}
	if pending[0].DatasetID != "finance.expense_forms" || pending[0].ObjectRole != "expense_report" || pending[0].BlueprintID != "finance.expense.v1" || pending[0].ApprovalEvent != "finance.submitted" {
		t.Fatalf("signed installed approval app should restore DataSrv workflow identity from install snapshot: %#v", pending[0])
	}

	running := pending[0]
	running.CurrentNode = "manager.approval"
	running.CurrentNodeIDs = []string{"expense.submit", "manager.approval"}
	running.BusinessStatus = "finance_pending"
	running.ResultStatus = "running"
	running.Result = "manager review"
	running.ResultPayload = map[string]any{"text": "manager review", "business_record": map[string]any{"id": "expense-golden-1", "status": "finance_pending"}}
	running.Outputs = []maclawAppApprovalOutput{{Type: "content", Title: "Running Progress", Text: "manager review", Status: "running"}}
	if _, err := app.SyncMaclawAppApprovalInstanceToDataSrv(maclawAppApprovalDataSrvSyncInput{DatasetID: "finance.expense_forms", ObjectRole: "expense_report", RecordID: "expense-golden-1", ApprovalID: "approval-golden-1", Instance: running}); err != nil {
		t.Fatalf("SyncMaclawAppApprovalInstanceToDataSrv running error = %v", err)
	}

	final := running
	final.Status = "approved"
	final.Lane = "handled"
	final.CurrentNode = "expense.result"
	final.CurrentNodeIDs = []string{"expense.submit", "manager.approval", "expense.result"}
	final.BusinessStatus = "finance_approved"
	final.ResultStatus = "approved"
	final.Result = "approved by manager"
	final.WorkflowDecisionID = "decision-golden-1"
	final.ResultPayload = map[string]any{"approval_result": "approved", "business_status": "finance_approved", "business_record": map[string]any{"id": "expense-golden-1", "status": "finance_approved"}, "text": "approved by manager"}
	final.Outputs = []maclawAppApprovalOutput{{Type: "content", Title: "Approval Decision", Text: "approved by manager"}, {Type: "artifact", Title: "Approval PDF", ArtifactID: "artifact-golden-pdf", Artifact: &maclawAppApprovalArtifact{ID: "artifact-golden-pdf", Name: "expense-golden-approved.pdf", URI: "artifact://expense/golden-approved.pdf", Status: "ready"}}}
	final.Artifacts = []maclawAppApprovalArtifact{{ID: "artifact-golden-pdf", Name: "expense-golden-approved.pdf", URI: "artifact://expense/golden-approved.pdf", Status: "ready"}}
	if _, err := app.SyncMaclawAppApprovalInstanceToDataSrv(maclawAppApprovalDataSrvSyncInput{DatasetID: "finance.expense_forms", ObjectRole: "expense_report", RecordID: "expense-golden-1", ApprovalID: "approval-golden-1", Instance: final}); err != nil {
		t.Fatalf("SyncMaclawAppApprovalInstanceToDataSrv final error = %v", err)
	}
	handled, err := app.ListMaclawAppApprovalInstancesAll("handled", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstancesAll handled error = %v", err)
	}
	if len(handled) != 1 {
		t.Fatalf("expected handled approval from DataSrv, got %#v", handled)
	}
	got := handled[0]
	if got.AppID != "expense-approval" || got.ApprovalID != "approval-golden-1" || got.InstanceID != "wf-golden-1" || got.CurrentNode != "expense.result" || got.WorkflowSkillID != "expense-workflow" || got.WorkflowVersion != "2.1.0" {
		t.Fatalf("handled approval should preserve signed installed app workflow identity: %#v", got)
	}
	if got.ResultPayload["approval_result"] != "approved" || got.ResultPayload["business_status"] != "finance_approved" || len(got.Outputs) != 2 || got.Outputs[1].Title != "Approval PDF" || len(got.Artifacts) != 1 || got.Artifacts[0].Name != "expense-golden-approved.pdf" {
		t.Fatalf("handled approval should expose final content and file result package: %#v", got)
	}
	var goldenFinalSync map[string]any
	for _, sync := range dataSrvApprovalSyncs {
		if sync.Path == "/api/v1/data/approvals/approval-golden-1/review" {
			goldenFinalSync = sync.Body
		}
	}
	if len(goldenFinalSync) == 0 {
		t.Fatalf("signed installed approval app should sync final review payload to DataSrv: %#v", dataSrvApprovalSyncs)
	}
	if goldenFinalSync["decision"] != "approved" || goldenFinalSync["workflow_instance_id"] != "wf-golden-1" || goldenFinalSync["workflow_node_id"] != "expense.result" || goldenFinalSync["workflow_version"] != "2.1.0" {
		t.Fatalf("final review sync should preserve workflow identity: %#v", goldenFinalSync)
	}
	goldenFinalPayload := anyMap(goldenFinalSync["result_payload"])
	goldenFinalOutputs := anySlice(goldenFinalSync["outputs"])
	goldenFinalArtifacts := anySlice(goldenFinalSync["artifacts"])
	if goldenFinalPayload["approval_result"] != "approved" || goldenFinalPayload["business_status"] != "finance_approved" || len(goldenFinalOutputs) != 2 || len(goldenFinalArtifacts) != 1 || anyMap(goldenFinalArtifacts[0])["name"] != "expense-golden-approved.pdf" {
		t.Fatalf("final review sync should preserve result payload, output blocks and artifact package: %#v", goldenFinalSync)
	}

	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	workflowResultPath := filepath.Join(app.testHomeDir, "installed-golden-workflow-result.txt")
	writeWorkflowResult := func(body string) {
		t.Helper()
		if err := os.WriteFile(workflowResultPath, []byte("workflow_result="+body+"\n"), 0o644); err != nil {
			t.Fatalf("write installed workflow result fixture: %v", err)
		}
	}
	workflowCommand := `cat "` + workflowResultPath + `"`
	if os.PathSeparator == '\\' {
		workflowCommand = `type "` + workflowResultPath + `"`
	}
	if err := app.skillExecutor.Update(corelib.NLSkillEntry{
		Name:   "expense-workflow",
		Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action:  "bash",
			Params:  map[string]interface{}{"command": workflowCommand},
			Capture: map[string]string{"workflow_result": `workflow_result=(.+)`},
		}},
	}); err != nil {
		t.Fatalf("update installed workflow skill: %v", err)
	}
	writeWorkflowResult(`{"approval_instance":{"status":"requires_input","lane":"my_requests","workflow_instance_id":"wf-golden-2","approval_id":"approval-golden-2","record_id":"expense-golden-2","dataset_id":"finance.expense_forms","object_role":"expense_report","workflow_skill_id":"expense-workflow","workflow_version":"2.1.0","workflow_node_id":"expense.require_input","workflow_node_ids":["expense.submit","expense.require_input"],"workflow_decision_id":"decision-golden-2-input","business_status":"waiting_for_requester","result_status":"requires_input","result":"missing invoice attachment","result_payload":{"approval_result":"requires_input","business_status":"waiting_for_requester","requires_input":{"fields":["invoice_attachment"],"message":"missing invoice attachment"},"business_record":{"id":"expense-golden-2","status":"waiting_for_requester"},"text":"missing invoice attachment"},"outputs":[{"type":"content","kind":"requires_input","title":"Missing materials","text":"missing invoice attachment","status":"requires_input"}]}}`)
	needsInput, err := app.StartMaclawAppApprovalWorkflow(MaclawAppApprovalWorkflowStartInput{AppID: "expense-approval", DatasetID: "finance.expense_forms", ObjectRole: "expense_report", RecordID: "expense-golden-2", Title: "Supplemented Expense", Applicant: "alice", Approver: "manager", BusinessNote: "needs invoice", BusinessPayload: map[string]any{"amount": float64(280), "currency": "CNY"}, FormData: map[string]any{"reason": "client travel"}, RunWorkflowSkill: true})
	if err != nil {
		t.Fatalf("StartMaclawAppApprovalWorkflow() installed requires_input error = %v", err)
	}
	if needsInput["approval_id"] != "approval-golden-2" || supplementalCreateCount != 1 {
		t.Fatalf("installed requires_input run should create one DataSrv approval: result=%#v createCount=%d", needsInput, supplementalCreateCount)
	}
	needsInputRun, ok := needsInput["workflow_run"].(map[string]any)
	if !ok || needsInputRun["ran"] != true {
		t.Fatalf("expected installed requires_input workflow evidence: %#v", needsInput["workflow_run"])
	}
	needsInputInstance, ok := needsInputRun["instance"].(maclawAppApprovalInstance)
	if !ok || needsInputInstance.ApprovalID != "approval-golden-2" || needsInputInstance.InstanceID != "wf-golden-2" || needsInputInstance.Status != "requires_input" || needsInputInstance.Lane != "my_requests" {
		t.Fatalf("installed workflow should surface requires_input requester instance: %#v", needsInputRun["instance"])
	}
	if needsInputInstance.ResultPayload["approval_result"] != "requires_input" || len(needsInputInstance.Outputs) != 1 || needsInputInstance.Outputs[0].Kind != "requires_input" {
		t.Fatalf("installed requires_input instance should preserve result package: %#v", needsInputInstance)
	}

	writeWorkflowResult(`{"approval_instance":{"status":"approved","lane":"handled","workflow_instance_id":"wf-golden-2","approval_id":"approval-golden-2","record_id":"expense-golden-2","dataset_id":"finance.expense_forms","object_role":"expense_report","workflow_skill_id":"expense-workflow","workflow_version":"2.1.0","workflow_node_id":"expense.result","workflow_node_ids":["expense.require_input","manager.approval","expense.result"],"workflow_decision_id":"decision-golden-2","business_status":"finance_approved","result_status":"approved","result":"approved after supplemental input","result_payload":{"approval_result":"approved","business_status":"finance_approved","business_record":{"id":"expense-golden-2","status":"finance_approved"},"text":"approved after supplemental input"},"outputs":[{"type":"content","title":"Workflow Decision","text":"approved after supplemental input","status":"approved"}]}}`)
	continued, err := app.StartMaclawAppApprovalWorkflow(MaclawAppApprovalWorkflowStartInput{AppID: "expense-approval", ApprovalID: "approval-golden-2", ContinueFromID: "wf-golden-2", DatasetID: "finance.expense_forms", ObjectRole: "expense_report", RecordID: "expense-golden-2", Title: "Supplemented Expense", Applicant: "alice", Approver: "manager", CurrentAssignee: "manager", BusinessNote: "supplemental input submitted", BusinessPayload: map[string]any{"amount": float64(280), "invoice_attachment": "artifact://invoice/golden-2.pdf"}, FormData: map[string]any{"invoice_attachment": "artifact://invoice/golden-2.pdf"}, RunWorkflowSkill: true})
	if err != nil {
		t.Fatalf("StartMaclawAppApprovalWorkflow() installed continue requires_input error = %v", err)
	}
	if continued["approval_id"] != "approval-golden-2" || supplementalCreateCount != 1 {
		t.Fatalf("installed continuation should reuse existing approval id without creating a new one: result=%#v createCount=%d", continued, supplementalCreateCount)
	}
	continuedRun, ok := continued["workflow_run"].(map[string]any)
	if !ok || continuedRun["ran"] != true {
		t.Fatalf("expected installed continued workflow evidence: %#v", continued["workflow_run"])
	}
	continuedInstance, ok := continuedRun["instance"].(maclawAppApprovalInstance)
	if !ok || continuedInstance.ApprovalID != "approval-golden-2" || continuedInstance.InstanceID != "wf-golden-2" || continuedInstance.Status != "approved" || continuedInstance.Lane != "handled" || continuedInstance.WorkflowDecisionID != "decision-golden-2" {
		t.Fatalf("installed continuation should finish same approval instance: %#v", continuedRun["instance"])
	}
	if supplemental, ok := anyMap(continuedInstance.ResultPayload["supplemental_input"])["form_data"].(map[string]any); !ok || supplemental["invoice_attachment"] != "artifact://invoice/golden-2.pdf" {
		t.Fatalf("continued installed instance should retain supplemental form input: %#v", continuedInstance.ResultPayload)
	}
	goldenHandled, err := app.ListMaclawAppApprovalInstancesAll("handled", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstancesAll handled after supplement error = %v", err)
	}
	foundSupplemented := false
	for _, item := range goldenHandled {
		if item.ApprovalID == "approval-golden-2" {
			foundSupplemented = item.InstanceID == "wf-golden-2" && item.Status == "approved" && item.ResultPayload["approval_result"] == "approved"
		}
	}
	if !foundSupplemented {
		t.Fatalf("installed supplemental approval should be visible as the same handled instance from DataSrv: %#v", goldenHandled)
	}
	var supplementalFinalSync map[string]any
	for _, sync := range dataSrvApprovalSyncs {
		if sync.Path == "/api/v1/data/approvals/approval-golden-2/review" {
			supplementalFinalSync = sync.Body
		}
	}
	if len(supplementalFinalSync) == 0 {
		t.Fatalf("installed supplemental approval should sync final review payload to DataSrv: %#v", dataSrvApprovalSyncs)
	}
	supplementalPayload := anyMap(supplementalFinalSync["result_payload"])
	supplementalInput := anyMap(supplementalPayload["supplemental_input"])
	supplementalFormData := anyMap(supplementalInput["form_data"])
	if supplementalFinalSync["decision"] != "approved" ||
		supplementalFinalSync["workflow_instance_id"] != "wf-golden-2" ||
		supplementalFinalSync["workflow_decision_id"] != "decision-golden-2" ||
		supplementalPayload["approval_result"] != "approved" ||
		supplementalFormData["invoice_attachment"] != "artifact://invoice/golden-2.pdf" {
		t.Fatalf("supplemental final review sync should preserve continuation identity and submitted form data: %#v", supplementalFinalSync)
	}
	if len(dataSrvRequests) == 0 || dataSrvRequests[0] != "PUT /api/v1/data/app-installations/expense-approval" {
		t.Fatalf("signed hub install should register approval app before runtime, got %#v", dataSrvRequests)
	}
}
func TestInstallSelectedMaclawAppPackageFromHubReportsDependencyInstallDiagnostics(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		if source != "skillhub" || id != "signed-workflow" || installRef != "signed-workflow" {
			t.Fatalf("unexpected dependency install call: source=%s id=%s installRef=%s", source, id, installRef)
		}
		return fmt.Errorf("signature verification failed: public key fingerprint not trusted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/capabilities/maclaw-apps/cap-failing-dep/package" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		var pkg map[string]any
		if err := json.Unmarshal([]byte(maclawAppPackageWithCurrentDefinitionHashes(t, `{
			"schema": "maclaw.app.pack.v1",
			"privateMarker": "x_maclaw_apps",
			"source": "enterprise_hub",
			"apps": [{
				"schema": "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"app": {
					"id": "failing-dependency-app",
					"name": "Failing Dependency App",
					"kind": "enterprise_approval_app",
					"binding": {
						"datasrv": {"datasetID":"finance.expense_forms", "objectRole":"expense_report"},
						"mis": {"approvalBindings": [{"event":"finance.submitted", "objectRole":"expense_report", "workflowSkillId":"signed-workflow", "workflowVersion":"1.0.0"}]},
						"workflow": {"schema":"maclaw.app.workflow.v1", "submitNode":"expense.submit", "approvalNode":"manager.approval", "resultNode":"expense.result"},
						"dependencies": {"skills": [{"id":"signed-workflow", "kind":"workflow_skill", "version":"1.0.0", "required":true, "source":"hub", "install_ref":"hub://skills/signed-workflow@1.0.0", "package_sha256":"sha-signed-workflow", "package_signature":"sig-signed-workflow"}]},
						"ui": {"schema":"maclaw.app.ui.v1", "entry":"approval_workspace", "layouts":{"approval_workspace":{"template":"left_nav", "density":"compact"}}},
						"resultContract": {"schema":"maclaw.app.result.v1", "primary":"approval_result", "types":["approval_result"]}
					},
					"governance": {
						"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"approval_workspace", "template":"left_nav", "density":"compact"},
						"resultContract": {"schema":"maclaw.app.result.v1", "primary":"approval_result", "types":["approval_result"]},
						"testEvidence": {"runId":"run-failing-dep", "resultPayload":{"approval_result":"approved"}, "approvalInstance":{"approvalID":"approval-failing-dep", "status":"approved", "currentNode":"expense.result"}}
					}
				}
			}]
		}`)), &pkg); err != nil {
			t.Fatalf("decode failing dependency package fixture: %v", err)
		}
		packageSHA := strings.Repeat("d", 64)
		versionKey := "enterprise_hub:skill:maclaw-app:failing-dependency-app@pkg"
		publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("GenerateKey() error = %v", err)
		}
		payload := "maclaw-app\n" + packageSHA + "\n" + versionKey + "\n2026-06-30T08:00:00Z\nhub-admin"
		pkg["package_sha256"] = packageSHA
		pkg["package_signature"] = map[string]any{
			"schema":                 "maclaw.app.package_signature.v1",
			"algorithm":              "ed25519",
			"payload":                payload,
			"public_key_base64":      base64.StdEncoding.EncodeToString(publicKey),
			"public_key_fingerprint": downloadedSkillPublicKeyFingerprint(publicKey),
			"signature_base64":       base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(payload))),
			"package_sha256":         packageSHA,
			"version_key":            versionKey,
			"signed_at":              "2026-06-30T08:00:00Z",
			"signed_by":              "hub-admin",
		}
		markMaclawAppPackageAsPublishedHubDownloadTest(t, pkg, "failing-dependency-app", "cap-failing-dep", versionKey)
		ensureMaclawAppPackageDependencyVerificationForHubDownloadTest(t, pkg)
		_ = json.NewEncoder(w).Encode(pkg)
	}))
	defer server.Close()
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	_, err := app.InstallSelectedMaclawAppPackageFromHub("cap-failing-dep", []string{"failing-dependency-app"})
	if err == nil {
		t.Fatalf("expected dependency install diagnostic error")
	}
	message := err.Error()
	for _, want := range []string{"required Skill dependencies are missing or unavailable", "signed-workflow", "package_integrity_failed", "skillhub_download", "signature verification failed", "public key fingerprint not trusted"} {
		if !strings.Contains(message, want) {
			t.Fatalf("install error should include %q, got %q", want, message)
		}
	}
}
func TestInstallSelectedMaclawAppPackageFromHubFiltersPackageApps(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	fingerprint := downloadedSkillPublicKeyFingerprint(publicKey)
	capabilityID := "cap-multi-app"
	versionKey := "enterprise_hub:skill:maclaw-app:multi-app@pkg"
	packageSHA := strings.Repeat("f", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/capabilities/maclaw-apps/"+capabilityID+"/package" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		var pkg map[string]any
		if err := json.Unmarshal([]byte(maclawAppPackageWithCurrentDefinitionHashes(t, `{
					"schema": "maclaw.app.pack.v1",
					"privateMarker": "x_maclaw_apps",
					"source": "enterprise_hub",
			"apps": [{
				"schema": "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"app": {
					"id": "hub-skip",
					"name": "Hub Skip",
					"description": "This app is not selected",
					"kind": "tool_app",
					"binding": {
						"skill": {"id": "hub-skip-skill", "appDefinitionFile": "maclaw.app.json", "inputMode": "form"},
						"dependencies": {"skills": [{"id": "local-only-skill", "kind": "runtime_skill", "source": "local", "required": true}]}
					},
					"governance": {
						"workspaceLayout": {"schema": "maclaw.app.ui.v1", "entry": "tool_workspace", "template": "classic_split", "density": "compact", "primaryRegion": "left", "outputRegion": "right", "regionCount": 2, "regions": [{"id":"input", "role":"input", "placement":"left"}, {"id":"output", "role":"output", "placement":"right"}]},
						"resultContract": {"schema": "maclaw.app.result.v1", "primary": "content", "types": ["content"]},
						"testProtocol": {"schema": "maclaw.app.test_protocol.v1", "sampleInput": {"text": "skip"}, "expectedOutput": {"type": "content"}, "fingerprint": "proto-skip"},
						"testEvidence": {"runId": "run-skip", "testProtocolFingerprint": "proto-skip", "resultPayload": {"content": "skip"}, "dependencyVerification": {"schema": "maclaw.app.install_plan.v1", "dependency_count": 1, "has_missing_required": true, "has_blocking_dependency": true, "dependencies": [{"id": "local-only-skill", "kind": "runtime_skill", "source": "local", "required": true, "installed": false, "health": "missing", "action": "blocked", "app_ids": ["hub-skip"]}]}}
					}
				}
			}, {
				"schema": "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"app": {
					"id": "hub-kept",
					"name": "Hub Kept",
					"description": "This app is selected",
					"kind": "tool_app",
					"binding": {"skill": {"id": "hub-kept-skill", "appDefinitionFile": "maclaw.app.json", "inputMode": "form"}},
					"governance": {
						"workspaceLayout": {"schema": "maclaw.app.ui.v1", "entry": "tool_workspace", "template": "classic_split", "density": "compact", "primaryRegion": "left", "outputRegion": "right", "regionCount": 2, "regions": [{"id":"input", "role":"input", "placement":"left"}, {"id":"output", "role":"output", "placement":"right"}]},
						"resultContract": {"schema": "maclaw.app.result.v1", "primary": "content", "types": ["content"]},
						"testProtocol": {"schema": "maclaw.app.test_protocol.v1", "sampleInput": {"text": "kept"}, "expectedOutput": {"type": "content"}, "fingerprint": "proto-kept"},
						"testEvidence": {"runId": "run-kept", "testProtocolFingerprint": "proto-kept", "resultPayload": {"content": "kept"}}, "dependencyVerification": {"schema": "maclaw.app.install_plan.v1", "dependency_count": 1, "has_missing_required": false, "has_blocking_dependency": false, "has_workflow_contract_issue": false, "has_governance_review_issue": false, "dependencies": [{"id": "hub-kept-skill", "kind": "runtime_skill", "source": "hub", "required": true, "installed": true, "health": "ready", "action": "skip", "app_ids": ["hub-kept"]}]}
					}
				}
				}]
			}`)), &pkg); err != nil {
			t.Fatalf("decode signed multi-app package fixture: %v", err)
		}
		reviewEvidence := map[string]any{
			"hub-skip": map[string]any{"run_id": "run-skip-published", "approval_status": "ready", "current_node": "tool_workspace"},
			"hub-kept": map[string]any{"run_id": "run-kept-published", "approval_status": "ready", "current_node": "tool_workspace"},
		}
		pkg["capability_id"] = capabilityID
		pkg["capability"] = map[string]any{"id": capabilityID, "capability_id": "multi-app", "display_name": "Multi App", "status": "published", "current_version_key": versionKey}
		pkg["review_evidence"] = reviewEvidence
		pkg["maclaw_app_review_evidence"] = reviewEvidence
		pkg["resolved_dependencies"] = []any{
			map[string]any{"id": "local-only-skill", "kind": "runtime_skill", "source": "local", "required": true, "app_ids": []string{"hub-skip"}},
			map[string]any{"id": "hub-kept-skill", "kind": "runtime_skill", "source": "enterprise_hub", "required": true, "app_ids": []string{"hub-kept"}},
		}
		signature := maclawapptest.SignPublishedMaclawAppHubPackage(pkg, publicKey, privateKey, packageSHA, versionKey, "2026-07-01T02:00:00Z", "hub-admin")
		for _, rawEntry := range anySlice(pkg["apps"]) {
			entry := anyMap(rawEntry)
			appMap := anyMap(entry["app"])
			appID := strings.TrimSpace(maclawAppStringFromAny(appMap["id"]))
			governance := anyMap(appMap["governance"])
			if governance == nil {
				governance = map[string]any{}
				appMap["governance"] = governance
			}
			governance["submission"] = map[string]any{
				"schema":               "maclaw.app.hub_submission.v1",
				"status":               "published",
				"capability_id":        capabilityID,
				"market_capability_id": appID,
				"version_key":          versionKey,
				"package_sha256":       packageSHA,
				"package_signature":    signature,
				"review_evidence":      reviewEvidence,
			}
		}
		ensureMaclawAppPackageDependencyVerificationForHubDownloadTest(t, pkg)
		_ = json.NewEncoder(w).Encode(pkg)
	}))
	defer server.Close()
	keptSkillDir := filepath.Join(app.GetDataDir(), "skills", "hub-kept-skill")
	if err := os.MkdirAll(keptSkillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll keptSkillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(keptSkillDir, "skill.md"), []byte("# Hub kept skill\n"), 0o644); err != nil {
		t.Fatalf("WriteFile kept skill.md: %v", err)
	}
	cfg := corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "hub-kept-skill", SkillDir: keptSkillDir, Status: "active", HubVersion: "1.0.0"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	result, err := app.InstallSelectedMaclawAppPackageFromHub("cap-multi-app", []string{"market-hub-kept"})
	if err != nil {
		t.Fatalf("InstallSelectedMaclawAppPackageFromHub() error = %v", err)
	}
	if result["app_count"] != 1 || result["source_app_count"] != 2 {
		t.Fatalf("unexpected selected install counts: %#v", result)
	}
	installedPackage := anyMap(result["package"])
	installedSignature := anyMap(installedPackage["package_signature"])
	if installedSignature["public_key_fingerprint"] != fingerprint || installedSignature["package_sha256"] != packageSHA {
		t.Fatalf("selected install package should preserve original Hub package signature: %#v", installedSignature)
	}
	appIDs, _ := result["app_ids"].([]string)
	if len(appIDs) != 1 || appIDs[0] != "hub-kept" {
		t.Fatalf("unexpected selected app ids: %#v", result["app_ids"])
	}
	installRecord, ok := result["install_record"].(map[string]any)
	if !ok {
		t.Fatalf("selected hub install should return install record: %#v", result["install_record"])
	}
	installRecordApps, ok := installRecord["apps"].([]maclawAppInstallPlanApp)
	if !ok || len(installRecordApps) != 1 || installRecordApps[0].ID != "hub-kept" {
		t.Fatalf("selected hub install record should summarize only selected apps: %#v", installRecord["apps"])
	}
	sourceAppIDs, _ := result["source_app_ids"].([]string)
	if len(sourceAppIDs) != 2 || sourceAppIDs[0] != "hub-skip" || sourceAppIDs[1] != "hub-kept" {
		t.Fatalf("unexpected source app ids: %#v", result["source_app_ids"])
	}
	packageJSON := maclawAppStringFromAny(result["package_json"])
	if !strings.Contains(packageJSON, "hub-kept") || strings.Contains(packageJSON, "hub-skip") || strings.Contains(packageJSON, "local-only-skill") {
		t.Fatalf("selected install package was not filtered: %s", packageJSON)
	}
	records, err := app.ListMaclawAppInstalls(10)
	if err != nil {
		t.Fatalf("ListMaclawAppInstalls() error = %v", err)
	}
	if len(records) != 1 || records[0].AppID != "hub-kept" || records[0].Source != "enterprise_hub" {
		t.Fatalf("unexpected selected install records: %#v", records)
	}
	if records[0].ReviewEvidence["run_id"] != "run-kept-published" || strings.Contains(fmt.Sprint(records[0].Package), "run-skip-published") {
		t.Fatalf("selected hub install audit should preserve only selected app review evidence: record=%#v package=%#v", records[0].ReviewEvidence, records[0].Package)
	}
}
func TestInstallSelectedMaclawAppPackageFromHubInstallsDepsAndRegistersDataSrv(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
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
		if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# "+id+"\n"), 0o644); err != nil {
			return err
		}
		cfg, err := app.LoadConfig()
		if err != nil {
			return err
		}
		cfg.NLSkills = append(cfg.NLSkills, corelib.NLSkillEntry{Name: id, SkillDir: skillDir, Status: "active", Source: source, HubSkillID: installRef, HubVersion: "1.0.0"})
		return app.SaveConfig(cfg)
	}

	var dataSrvRequests []map[string]interface{}
	dataSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/data/app-installations/kept-normal" {
			t.Fatalf("unexpected DataSrv request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode DataSrv registration body: %v", err)
		}
		dataSrvRequests = append(dataSrvRequests, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"app_id":"kept-normal","status":"installed"}`))
	}))
	defer dataSrv.Close()

	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/capabilities/maclaw-apps/cap-selected-normal/package" {
			t.Fatalf("unexpected Hub request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		var pkg map[string]any
		if err := json.Unmarshal([]byte(maclawAppPackageWithCurrentDefinitionHashes(t, `{
			"schema": "maclaw.app.pack.v1",
			"privateMarker": "x_maclaw_apps",
			"source": "enterprise_hub",
			"resolved_dependencies": [
				{"id":"skipped-skill", "source":"hub", "install_ref":"hub-skipped-skill", "kind":"app_skill", "required":true, "app_ids":["skipped-normal"]},
				{"id":"kept-skill", "source":"hub", "install_ref":"hub-kept-skill", "kind":"app_skill", "required":true, "app_ids":["kept-normal"]}
			],
			"apps": [{
				"schema": "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"app": {
					"id": "skipped-normal",
					"name": "Skipped Normal",
					"kind": "enterprise_normal_app",
					"binding": {"appSkill": {"id":"skipped-skill", "source":"hub"}, "datasrv": {"domain":"ops", "datasetID":"ops.skipped", "objectRole":"skipped_record"}},
					"ui": {"schema":"maclaw.app.ui.v1", "entry":"business_workspace", "layouts":{"business_workspace":{"template":"classic_split", "density":"compact", "regions":[{"id":"input", "role":"input", "placement":"left"}, {"id":"records", "role":"record_list", "placement":"center"}, {"id":"output", "role":"output", "placement":"right"}]}}},
					"governance": {"workspaceLayout":{"schema":"maclaw.app.ui.v1", "entry":"business_workspace", "template":"classic_split", "density":"compact", "regionCount":3, "regions":[{"id":"input", "role":"input", "placement":"left"}, {"id":"records", "role":"record_list", "placement":"center"}, {"id":"output", "role":"output", "placement":"right"}]}, "resultContract":{"schema":"maclaw.app.result.v1", "primary":"content", "types":["content"]}, "dependencyVerification":{"schema":"maclaw.app.install_plan.v1", "dependencyCount":1, "hasMissingRequired":false, "hasBlockingDependency":false, "hasWorkflowContractIssue":false, "hasGovernanceReviewIssue":false, "dependencies":[{"id":"skipped-skill", "kind":"app_skill", "source":"hub", "install_ref":"hub-skipped-skill", "required":true, "installed":true, "health":"ready", "action":"skip", "app_ids":["skipped-normal"]}]}, "testProtocol":{"schema":"maclaw.app.test_protocol.v1", "fingerprint":"proto-skipped", "sampleInput":{"text":"skip"}, "expectedOutput":{"type":"content"}}, "testEvidence":{"runId":"run-skipped", "testProtocolFingerprint":"proto-skipped", "resultPayload":{"content":"skip"}, "resultCoverage":{"ok":true, "primary":"content", "coveredTypes":["content"], "missingTypes":[]}}}
				}
			}, {
				"schema": "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"app": {
					"id": "kept-normal",
					"name": "Kept Normal",
					"kind": "enterprise_normal_app",
					"binding": {"appSkill": {"id":"kept-skill", "source":"hub"}, "datasrv": {"domain":"ops", "datasetID":"ops.kept", "templateID":"ops.kept_template", "objectRole":"kept_record"}},
					"ui": {"schema":"maclaw.app.ui.v1", "entry":"business_workspace", "layouts":{"business_workspace":{"template":"classic_split", "density":"compact", "primaryRegion":"center", "outputRegion":"right", "regions":[{"id":"input", "role":"input", "placement":"left"}, {"id":"records", "role":"record_list", "placement":"center"}, {"id":"output", "role":"output", "placement":"right"}]}}},
					"governance": {"workspaceLayout":{"schema":"maclaw.app.ui.v1", "entry":"business_workspace", "template":"classic_split", "density":"compact", "primaryRegion":"center", "outputRegion":"right", "regionCount":3, "regions":[{"id":"input", "role":"input", "placement":"left"}, {"id":"records", "role":"record_list", "placement":"center"}, {"id":"output", "role":"output", "placement":"right"}]}, "resultContract":{"schema":"maclaw.app.result.v1", "primary":"content", "types":["content"]}, "dependencyVerification":{"schema":"maclaw.app.install_plan.v1", "dependencyCount":1, "hasMissingRequired":false, "hasBlockingDependency":false, "hasWorkflowContractIssue":false, "hasGovernanceReviewIssue":false, "dependencies":[{"id":"kept-skill", "kind":"app_skill", "source":"hub", "install_ref":"hub-kept-skill", "required":true, "installed":true, "health":"ready", "action":"skip", "app_ids":["kept-normal"]}]}, "testProtocol":{"schema":"maclaw.app.test_protocol.v1", "fingerprint":"proto-kept", "sampleInput":{"text":"kept"}, "expectedOutput":{"type":"content"}}, "testEvidence":{"runId":"run-kept-normal", "testProtocolFingerprint":"proto-kept", "resultPayload":{"content":"kept"}, "outputs":[{"kind":"text", "title":"Result", "text":"kept", "status":"ready"}], "resultCoverage":{"ok":true, "primary":"content", "coveredTypes":["content"], "missingTypes":[]}}}
				}
			}]
		}`)), &pkg); err != nil {
			t.Fatalf("decode selected normal package fixture: %v", err)
		}
		packageSHA := strings.Repeat("8", 64)
		versionKey := "enterprise_hub:skill:maclaw-app:selected-normal@pkg"
		publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("GenerateKey() error = %v", err)
		}
		signature := maclawapptest.SignPublishedMaclawAppHubPackage(pkg, publicKey, privateKey, packageSHA, versionKey, "2026-07-01T03:00:00Z", "hub-admin")
		reviewEvidence := map[string]any{
			"skipped-normal": map[string]any{"run_id": "run-skipped-published", "result_contract_primary": "content", "result_coverage_primary": "content", "result_coverage_covered_count": 1},
			"kept-normal":    map[string]any{"run_id": "run-kept-normal-published", "result_contract_primary": "content", "result_coverage_primary": "content", "result_coverage_covered_count": 1},
		}
		pkg["capability_id"] = "cap-selected-normal"
		pkg["capability"] = map[string]any{"id": "cap-selected-normal", "capability_id": "selected-normal", "display_name": "Selected Normal", "status": "published", "current_version_key": versionKey}
		pkg["review_evidence"] = reviewEvidence
		pkg["maclaw_app_review_evidence"] = reviewEvidence
		for _, rawEntry := range anySlice(pkg["apps"]) {
			entry := anyMap(rawEntry)
			appMap := anyMap(entry["app"])
			appID := strings.TrimSpace(maclawAppStringFromAny(appMap["id"]))
			governance := anyMap(appMap["governance"])
			if governance == nil {
				governance = map[string]any{}
				appMap["governance"] = governance
			}
			governance["submission"] = map[string]any{
				"schema":            "maclaw.app.hub_submission.v1",
				"status":            "published",
				"capability_id":     "cap-selected-normal",
				"market_app_id":     appID,
				"version_key":       versionKey,
				"package_sha256":    packageSHA,
				"package_signature": signature,
				"review_evidence":   reviewEvidence,
			}
		}
		ensureMaclawAppPackageDependencyVerificationForHubDownloadTest(t, pkg)
		_ = json.NewEncoder(w).Encode(pkg)
	}))
	defer hub.Close()

	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: hub.URL, RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: dataSrv.URL, Token: "data-token", TenantID: "tenant", UserID: "data-admin", Role: "data_admin"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	result, err := app.InstallSelectedMaclawAppPackageFromHub("cap-selected-normal", []string{"kept-normal"})
	if err != nil {
		t.Fatalf("InstallSelectedMaclawAppPackageFromHub() error = %v", err)
	}
	if len(installCalls) != 1 || installCalls[0] != (installCall{source: "skillhub", id: "kept-skill", installRef: "hub-kept-skill"}) {
		t.Fatalf("selected install should install only kept dependency by install_ref, got %#v", installCalls)
	}
	if result["app_count"] != 1 || result["source_app_count"] != 2 {
		t.Fatalf("unexpected selected install result: %#v", result)
	}
	installPlan, ok := result["install_plan"].(maclawAppInstallPlan)
	if !ok {
		t.Fatalf("missing install plan: %#v", result["install_plan"])
	}
	if dep := maclawAppPlanDepForTest(installPlan, "kept-skill"); dep == nil || dep.InstallRef != "hub-kept-skill" || dep.Action != "installed" || !dep.Installed {
		t.Fatalf("install plan should mark kept dependency installed with install_ref: %#v", dep)
	}
	if dep := maclawAppPlanDepForTest(installPlan, "skipped-skill"); dep != nil {
		t.Fatalf("install plan should not include unselected dependency: %#v", dep)
	}
	if len(dataSrvRequests) != 1 {
		t.Fatalf("expected one DataSrv registration, got %#v", dataSrvRequests)
	}
	body := dataSrvRequests[0]
	if body["app_id"] != "kept-normal" || body["kind"] != "enterprise_normal_app" || body["source"] != "enterprise_hub" {
		t.Fatalf("DataSrv registration should describe selected app only: %#v", body)
	}
	roleBindings, ok := body["role_bindings"].([]interface{})
	if !ok || len(roleBindings) != 1 {
		t.Fatalf("DataSrv registration should include selected role binding: %#v", body)
	}
	binding, ok := roleBindings[0].(map[string]interface{})
	if !ok || binding["dataset_id"] != "ops.kept" || binding["object_role"] != "kept_record" {
		t.Fatalf("unexpected selected role binding: %#v", roleBindings)
	}
	metadata, ok := body["metadata"].(map[string]interface{})
	if !ok || metadata["app_skill_id"] != "kept-skill" || metadata["test_evidence_run_id"] != "run-kept-normal" {
		t.Fatalf("DataSrv registration missing selected app metadata: %#v", body)
	}
	depVerification, ok := metadata["dependency_verification"].(map[string]interface{})
	if !ok || depVerification["hasMissingRequired"] != false || depVerification["hasBlockingDependency"] != false {
		t.Fatalf("DataSrv metadata should include successful dependency verification: %#v", metadata)
	}
	deps, ok := depVerification["dependencies"].([]interface{})
	if !ok || len(deps) != 1 {
		t.Fatalf("dependency verification should include only selected dependency: %#v", depVerification)
	}
	depMap, ok := deps[0].(map[string]interface{})
	if !ok || depMap["id"] != "kept-skill" || depMap["install_ref"] != "hub-kept-skill" || depMap["action"] != "installed" || depMap["installed"] != true || depMap["install_ref_status"] != "ok" || depMap["preflight_status"] == "" {
		t.Fatalf("dependency verification should preserve selected installed dependency trace: %#v", deps)
	}
	records, err := app.ListMaclawAppInstalls(10)
	if err != nil {
		t.Fatalf("ListMaclawAppInstalls() error = %v", err)
	}
	if len(records) != 1 || records[0].AppID != "kept-normal" || len(records[0].Dependencies) != 1 || records[0].Dependencies[0].ID != "kept-skill" {
		t.Fatalf("local install audit should contain only selected app/dependency: %#v", records)
	}
	if records[0].DataSrvRegistration["synced"] != true || maclawAppIntValueForTest(records[0].DataSrvRegistration["eligible_count"]) != 1 || maclawAppIntValueForTest(records[0].DataSrvRegistration["synced_count"]) != 1 {
		t.Fatalf("selected install audit should persist DataSrv registration status: %#v", records[0].DataSrvRegistration)
	}
}
func TestInstallSelectedMaclawApprovalAppFromHubPreservesApprovalEvidence(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
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
		if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# "+id+"\n"), 0o644); err != nil {
			return err
		}
		cfg, err := app.LoadConfig()
		if err != nil {
			return err
		}
		hubVersion := "2.1.0"
		if id == "expense-super-skill" {
			hubVersion = "1.4.0"
		}
		cfg.NLSkills = append(cfg.NLSkills, corelib.NLSkillEntry{Name: id, SkillDir: skillDir, Status: "active", Source: source, HubSkillID: installRef, HubVersion: hubVersion})
		return app.SaveConfig(cfg)
	}

	var dataSrvRequests []map[string]interface{}
	dataSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer data-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/data/app-installations/expense-approval":
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode DataSrv registration body: %v", err)
			}
			dataSrvRequests = append(dataSrvRequests, body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"app_id":"expense-approval","status":"installed"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/data/app-installations":
			items := []map[string]interface{}{}
			for _, body := range dataSrvRequests {
				metadata, _ := body["metadata"].(map[string]interface{})
				if r.URL.Query().Get("hub_capability_id") != "" && metadata["hub_capability_id"] != r.URL.Query().Get("hub_capability_id") {
					continue
				}
				if r.URL.Query().Get("hub_version_key") != "" && metadata["hub_version_key"] != r.URL.Query().Get("hub_version_key") {
					continue
				}
				if r.URL.Query().Get("hub_review_status") != "" && metadata["hub_review_status"] != r.URL.Query().Get("hub_review_status") {
					continue
				}
				items = append(items, body)
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]interface{}{"items": items}); err != nil {
				t.Fatalf("encode DataSrv query response: %v", err)
			}
		default:
			t.Fatalf("unexpected DataSrv request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer dataSrv.Close()

	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/capabilities/maclaw-apps/cap-expense-approval/package" {
			t.Fatalf("unexpected Hub request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		var pkg map[string]any
		if err := json.Unmarshal([]byte(maclawAppPackageWithCurrentDefinitionHashes(t, `{
			"schema": "maclaw.app.pack.v1",
			"privateMarker": "x_maclaw_apps",
			"source": "enterprise_hub",
			"resolved_dependencies": [
				{"id":"expense-super-skill", "source":"hub", "install_ref":"hub-expense-super-skill", "kind":"app_skill", "required":true, "app_ids":["expense-approval"]},
				{"id":"expense-workflow", "source":"hub", "install_ref":"hub-expense-workflow", "kind":"workflow_skill", "required":true, "app_ids":["expense-approval"]}
			],
			"apps": [{
				"schema": "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"installUnit": "enterprise_app_pack",
				"app": {
					"id": "expense-approval",
					"name": "Expense Approval",
					"version": "1.4.0",
					"kind": "enterprise_approval_app",
					"binding": {
						"appSkill": {"id":"expense-super-skill", "version":"1.4.0", "source":"hub"},
						"datasrv": {"domain":"finance", "datasetID":"finance.expenses", "templateID":"finance.expense_form", "objectRole":"expense_report", "blueprintID":"finance.expense.v1"},
						"mis": {"approvalBindings": [{"event":"expense.submitted", "objectRole":"expense_report", "workflowSkillId":"expense-workflow", "workflowVersion":"2.1.0"}]},
						"workflow": {"schema":"maclaw.app.workflow.v1", "submitNode":"expense.submit", "approvalNode":"manager.approval", "resultNode":"expense.result"},
						"dependencies": {"skills": [
							{"id":"expense-super-skill", "kind":"app_skill", "version":"1.4.0", "required":true, "source":"hub", "install_ref":"hub-expense-super-skill"},
							{"id":"expense-workflow", "kind":"workflow_skill", "version":"2.1.0", "required":true, "source":"hub", "install_ref":"hub-expense-workflow"}
						]},
						"ui": {"schema":"maclaw.app.ui.v1", "entry":"approval_workspace", "layouts":{"approval_workspace":{"template":"left_nav", "density":"compact", "primaryRegion":"center", "outputRegion":"bottom", "regionIds":["approval_inbox","request_form","approval_detail","result_panel"], "visibleRegionCount":3, "fingerprint":"layout-expense-golden", "studio":{"editable":true, "savedInManifest":true, "updatedBy":"app_studio"}, "regions":[{"id":"approval_inbox", "role":"instance_list", "placement":"left", "order":1}, {"id":"request_form", "role":"input", "placement":"center", "order":2}, {"id":"approval_detail", "role":"detail", "placement":"right", "visible":false, "order":3}, {"id":"result_panel", "role":"output", "placement":"bottom", "order":4}]}}},
						"resultContract": {"schema":"maclaw.app.result.v1", "primary":"approval_result", "types":["approval_result", "business_status", "artifact"], "delivery":{"inlineContent":true, "artifacts":true}},
						"testProtocol": {"schema":"maclaw.app.test_protocol.v1", "fingerprint":"proto-expense", "sampleInput":{"amount":860, "applicant":"alice"}, "expectedOutput":{"approval_result":"approved", "business_status":"finance_approved"}}
					},
					"governance": {
						"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"approval_workspace", "template":"left_nav", "density":"compact", "primaryRegion":"center", "outputRegion":"bottom", "regionCount":4, "visibleRegionCount":3, "regionIds":["approval_inbox","request_form","approval_detail","result_panel"], "fingerprint":"layout-expense-golden", "studio":{"editable":true, "savedInManifest":true, "updatedBy":"app_studio"}, "regions":[{"id":"approval_inbox", "role":"instance_list", "placement":"left", "order":1}, {"id":"request_form", "role":"input", "placement":"center", "order":2}, {"id":"approval_detail", "role":"detail", "placement":"right", "visible":false, "order":3}, {"id":"result_panel", "role":"output", "placement":"bottom", "order":4}]},
						"resultContract": {"schema":"maclaw.app.result.v1", "primary":"approval_result", "types":["approval_result", "business_status", "artifact"], "delivery":{"inlineContent":true, "artifacts":true}},
							"testProtocol": {"schema":"maclaw.app.test_protocol.v1", "fingerprint":"proto-expense", "sampleInput":{"amount":860, "applicant":"alice"}, "expectedOutput":{"approval_result":"approved", "business_status":"finance_approved"}},
						"workflowContract": {"schema":"maclaw.app.workflow_contract.v1", "workflowSkillId":"expense-workflow", "workflowVersion":"2.1.0", "objectRole":"expense_report", "requiredInputs":["record_ref", "applicant", "business_payload"], "decisionOutputs":["approved", "rejected", "attention"], "statusMapping":{"pending":"pending", "approved":"approved", "rejected":"rejected", "attention":"attention"}},
							"submission": {"channel":"hub", "status":"published", "capability_id":"cap-expense-approval", "market_capability_id":"expense-approval", "submission_id":"enterprise_hub:skill:maclaw-app:expense-approval@pkg", "version_key":"enterprise_hub:skill:maclaw-app:expense-approval@pkg", "package_sha256":"pkg-expense-approval", "review_evidence":{"expense-approval":{"run_id":"run-hub-reviewed-expense", "test_protocol_fingerprint":"proto-expense", "result_contract_primary":"approval_result", "result_contract_type_count":3, "result_coverage_primary":"approval_result", "result_coverage_covered_count":3, "result_coverage_missing_count":0, "output_count":2, "artifact_count":1, "approval_status":"approved", "current_node":"expense.result"}}},
							"dependencyVerification":{"schema":"maclaw.app.install_plan.v1", "dependencyCount":2, "hasMissingRequired":false, "hasBlockingDependency":false, "hasWorkflowContractIssue":false, "hasGovernanceReviewIssue":false, "dependencies":[{"id":"expense-super-skill", "kind":"app_skill", "required":true, "installed":true, "health":"ready", "action":"skip"}, {"id":"expense-workflow", "kind":"workflow_skill", "required":true, "installed":true, "health":"ready", "action":"skip"}]},
						"testEvidence": {
							"runId":"run-expense-approval",
							"testProtocolFingerprint":"proto-expense",
							"primaryResult":"approval_result",
							"resultPayload":{"approval_result":"approved", "business_status":"finance_approved", "record_id":"expense-1001"},
							"outputs":[{"kind":"approval_result", "title":"Decision", "text":"approved", "status":"ready"}, {"kind":"table", "title":"Approval rows", "status":"ready", "data":{"rows":[{"id":"expense-1001"}]}}],
							"artifacts":[{"id":"artifact-expense-approval", "name":"expense-approval.pdf", "uri":"artifact://expense/approval.pdf", "status":"ready"}],
							"resultCoverage":{"ok":true, "primary":"approval_result", "coveredTypes":["approval_result", "business_status", "artifact"], "missingTypes":[]},
							"dependencyVerification":{"schema":"maclaw.app.install_plan.v1", "dependencyCount":2, "hasMissingRequired":false, "hasBlockingDependency":false, "hasWorkflowContractIssue":false, "hasGovernanceReviewIssue":false},
							"approvalInstance": {
								"instanceId":"wf-expense-1001",
								"approvalID":"approval-expense-1001",
								"recordID":"expense-1001",
								"datasetID":"finance.expenses",
								"blueprintID":"finance.expense.v1",
								"objectRole":"expense_report",
								"approvalEvent":"expense.submitted",
								"approvalWorkflowID":"expense.approval.workflow",
								"workflowSkillId":"expense-workflow",
								"workflowVersion":"2.1.0",
								"status":"approved",
								"currentNode":"expense.result",
								"businessStatus":"finance_approved",
								"resultStatus":"approved",
									"viewVerified":true,
								"detailURL":"approval://instances/wf-expense-1001",
								"resultPayload":{"approval_result":"approved", "business_status":"finance_approved"},
								"outputs":[{"kind":"approval_result", "title":"Decision", "text":"approved", "status":"ready"}],
								"artifacts":[{"id":"artifact-expense-approval", "name":"expense-approval.pdf", "uri":"artifact://expense/approval.pdf", "status":"ready"}]
							}
						}
					}
				}
			}]
		}`)), &pkg); err != nil {
			t.Fatalf("decode selected approval package fixture: %v", err)
		}
		packageSHA := strings.Repeat("9", 64)
		versionKey := "enterprise_hub:skill:maclaw-app:expense-approval@pkg"
		publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("GenerateKey() error = %v", err)
		}
		signature := maclawapptest.SignPublishedMaclawAppHubPackage(pkg, publicKey, privateKey, packageSHA, versionKey, "2026-07-01T04:00:00Z", "hub-admin")
		reviewEvidence := map[string]any{
			"expense-approval": map[string]any{
				"run_id":                         "run-hub-reviewed-expense",
				"test_protocol_fingerprint":      "proto-expense",
				"result_contract_primary":        "approval_result",
				"result_coverage_primary":        "approval_result",
				"result_coverage_covered_count":  3,
				"result_coverage_missing_count":  0,
				"output_count":                   2,
				"artifact_count":                 1,
				"approval_status":                "approved",
				"current_node":                   "expense.result",
				"has_dependency_verification":    true,
				"has_blocking_dependency":        false,
				"has_workflow_contract":          true,
				"has_workspace_layout":           true,
				"workspace_saved_in_manifest":    true,
				"datasrv_registration_status":    "ready",
				"datasrv_registration_app_count": 1,
			},
		}
		pkg["capability_id"] = "cap-expense-approval"
		pkg["capability"] = map[string]any{"id": "cap-expense-approval", "capability_id": "expense-approval", "display_name": "Expense Approval", "status": "published", "current_version_key": versionKey}
		pkg["review_evidence"] = reviewEvidence
		pkg["maclaw_app_review_evidence"] = reviewEvidence
		for _, rawEntry := range anySlice(pkg["apps"]) {
			entry := anyMap(rawEntry)
			appMap := anyMap(entry["app"])
			governance := anyMap(appMap["governance"])
			if governance == nil {
				governance = map[string]any{}
				appMap["governance"] = governance
			}
			governance["submission"] = map[string]any{
				"schema":               "maclaw.app.hub_submission.v1",
				"status":               "published",
				"capability_id":        "cap-expense-approval",
				"market_capability_id": "expense-approval",
				"submission_id":        versionKey,
				"version_key":          versionKey,
				"package_sha256":       packageSHA,
				"package_signature":    signature,
				"review_evidence":      reviewEvidence,
			}
		}
		ensureMaclawAppPackageDependencyVerificationForHubDownloadTest(t, pkg)
		_ = json.NewEncoder(w).Encode(pkg)
	}))
	defer hub.Close()

	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: hub.URL, RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: dataSrv.URL, Token: "data-token", TenantID: "tenant", UserID: "data-admin", Role: "data_admin"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	result, err := app.InstallSelectedMaclawAppPackageFromHub("cap-expense-approval", []string{"expense-approval"})
	if err != nil {
		t.Fatalf("InstallSelectedMaclawAppPackageFromHub() error = %v", err)
	}
	if len(installCalls) != 2 {
		t.Fatalf("selected approval install should install app and workflow dependencies, got %#v", installCalls)
	}
	installRefs := map[string]string{}
	for _, call := range installCalls {
		installRefs[call.id] = call.installRef
	}
	if installRefs["expense-super-skill"] != "hub-expense-super-skill" || installRefs["expense-workflow"] != "hub-expense-workflow" {
		t.Fatalf("install should use resolved dependency refs: %#v", installCalls)
	}
	installedPackage := anyMap(result["package"])
	resolvedDeps := anySlice(installedPackage["resolved_dependencies"])
	if len(resolvedDeps) != 2 {
		t.Fatalf("selected approval install package should keep only current app resolved deps: %#v", installedPackage["resolved_dependencies"])
	}
	resolvedByID := map[string]map[string]any{}
	for _, raw := range resolvedDeps {
		dep := anyMap(raw)
		resolvedByID[maclawAppStringValue(dep, "id")] = dep
		appIDs := anySlice(dep["app_ids"])
		if len(appIDs) != 1 || appIDs[0] != "expense-approval" {
			t.Fatalf("resolved dependency should be scoped to selected approval app: %#v", dep)
		}
	}
	if maclawAppStringValue(resolvedByID["expense-super-skill"], "install_ref") != "hub-expense-super-skill" || maclawAppStringValue(resolvedByID["expense-workflow"], "install_ref") != "hub-expense-workflow" {
		t.Fatalf("selected approval package should preserve resolved install refs: %#v", resolvedByID)
	}
	installRecord := anyMap(result["install_record"])
	if installRecord == nil {
		t.Fatalf("missing install record: %#v", result["install_record"])
	}
	installEvidenceByApp := anyMap(installRecord["install_evidence"])
	if installEvidenceByApp == nil {
		t.Fatalf("missing install evidence: %#v", installRecord)
	}
	installEvidence := anyMap(installEvidenceByApp["expense-approval"])
	if installEvidence == nil {
		t.Fatalf("missing approval install evidence: %#v", installEvidenceByApp)
	}
	installTestEvidence := anyMap(installEvidence["test_evidence"])
	installApproval := anyMap(installTestEvidence["approval_instance"])
	if installApproval == nil {
		installApproval = anyMap(installTestEvidence["approvalInstance"])
	}
	if installApproval == nil || maclawAppStringValue(installApproval, "current_node", "currentNode") != "expense.result" || maclawAppStringValue(installApproval, "workflow_skill_id", "workflowSkillId") != "expense-workflow" || maclawAppStringValue(installApproval, "business_status", "businessStatus") != "finance_approved" || maclawAppStringValue(installApproval, "result_status", "resultStatus") != "approved" {
		t.Fatalf("install evidence should preserve approval instance contract: %#v", installApproval)
	}
	installReviewEvidence := anyMap(installEvidence["review_evidence"])
	if installReviewEvidence == nil || installReviewEvidence["run_id"] != "run-hub-reviewed-expense" || installReviewEvidence["approval_status"] != "approved" || installReviewEvidence["current_node"] != "expense.result" {
		t.Fatalf("install evidence should preserve Hub review evidence: %#v", installEvidence)
	}
	installSubmission := anyMap(installEvidence["submission"])
	if installSubmission == nil || installSubmission["capability_id"] != "cap-expense-approval" || installSubmission["version_key"] != "enterprise_hub:skill:maclaw-app:expense-approval@pkg" || installSubmission["package_sha256"] != "pkg-expense-approval" {
		t.Fatalf("install evidence should preserve Hub submission identity: %#v", installEvidence)
	}
	if outputs := anySlice(installApproval["outputs"]); len(outputs) != 1 {
		t.Fatalf("install evidence should preserve approval outputs: %#v", installApproval)
	}
	if artifacts := anySlice(installApproval["artifacts"]); len(artifacts) != 1 {
		t.Fatalf("install evidence should preserve approval artifacts: %#v", installApproval)
	}

	if len(dataSrvRequests) != 1 {
		t.Fatalf("expected one DataSrv registration, got %#v", dataSrvRequests)
	}
	metadata := anyMap(dataSrvRequests[0]["metadata"])
	if metadata == nil || metadata["test_evidence_run_id"] != "run-expense-approval" || metadata["test_evidence_approval_current_node"] != "expense.result" || metadata["test_evidence_workflow_skill_id"] != "expense-workflow" || metadata["test_evidence_business_status"] != "finance_approved" || metadata["test_evidence_result_status"] != "approved" {
		t.Fatalf("DataSrv metadata should expose approval evidence summaries: %#v", metadata)
	}
	dataSrvReviewEvidence := anyMap(metadata["review_evidence"])
	if dataSrvReviewEvidence == nil || dataSrvReviewEvidence["run_id"] != "run-hub-reviewed-expense" || dataSrvReviewEvidence["result_coverage_covered_count"] != float64(3) {
		t.Fatalf("DataSrv metadata should preserve Hub review evidence: %#v", metadata)
	}
	dataSrvSubmission := anyMap(metadata["submission"])
	if dataSrvSubmission == nil || metadata["hub_capability_id"] != "cap-expense-approval" || metadata["hub_market_capability_id"] != "expense-approval" || metadata["hub_version_key"] != "enterprise_hub:skill:maclaw-app:expense-approval@pkg" || metadata["hub_package_sha256"] != "pkg-expense-approval" || metadata["hub_review_status"] != "published" {
		t.Fatalf("DataSrv metadata should preserve Hub submission identity: %#v", metadata)
	}
	if metadata["workspace_layout_primary_region"] != "center" || metadata["workspace_layout_output_region"] != "bottom" || metadata["workspace_layout_region_count"] != float64(4) || metadata["workspace_layout_visible_region_count"] != float64(3) || metadata["workspace_layout_fingerprint"] != "layout-expense-golden" {
		t.Fatalf("DataSrv metadata should expose approval workspace layout summaries: %#v", metadata)
	}
	workspaceLayout := anyMap(metadata["workspace_layout"])
	if workspaceLayout == nil || workspaceLayout["entry"] != "approval_workspace" || workspaceLayout["template"] != "left_nav" || workspaceLayout["primary_region"] != "center" || workspaceLayout["output_region"] != "bottom" || workspaceLayout["region_count"] != float64(4) || workspaceLayout["visible_region_count"] != float64(3) || workspaceLayout["fingerprint"] != "layout-expense-golden" {
		t.Fatalf("DataSrv metadata should preserve approval workspace layout: %#v", metadata)
	}
	workspaceRegions := anySlice(workspaceLayout["regions"])
	if len(workspaceRegions) != 4 {
		t.Fatalf("DataSrv metadata should preserve approval workspace regions: %#v", workspaceLayout)
	}
	workspaceRegionByID := map[string]map[string]any{}
	for _, raw := range workspaceRegions {
		region := anyMap(raw)
		workspaceRegionByID[maclawAppStringValue(region, "id")] = region
	}
	if maclawAppStringValue(workspaceRegionByID["request_form"], "placement") != "center" || maclawAppStringValue(workspaceRegionByID["approval_inbox"], "placement") != "left" || maclawAppStringValue(workspaceRegionByID["approval_detail"], "placement") != "right" || workspaceRegionByID["approval_detail"]["visible"] != false || maclawAppStringValue(workspaceRegionByID["result_panel"], "placement") != "bottom" {
		t.Fatalf("DataSrv metadata should preserve approval workspace region placements: %#v", workspaceLayout)
	}
	regionIDs := anySlice(metadata["workspace_layout_region_ids"])
	if len(regionIDs) != 4 || regionIDs[0] != "approval_inbox" || regionIDs[1] != "request_form" || regionIDs[2] != "approval_detail" || regionIDs[3] != "result_panel" {
		t.Fatalf("DataSrv metadata should expose approval workspace region ids: %#v", metadata["workspace_layout_region_ids"])
	}
	depVerification := anyMap(metadata["dependency_verification"])
	verificationDeps := anySlice(depVerification["dependencies"])
	if len(verificationDeps) != 2 {
		t.Fatalf("DataSrv metadata should preserve approval app dependency verification: %#v", depVerification)
	}
	verificationByID := map[string]map[string]any{}
	for _, raw := range verificationDeps {
		dep := anyMap(raw)
		verificationByID[maclawAppStringValue(dep, "id")] = dep
	}
	if maclawAppStringValue(verificationByID["expense-super-skill"], "install_ref") != "hub-expense-super-skill" || maclawAppStringValue(verificationByID["expense-workflow"], "install_ref") != "hub-expense-workflow" {
		t.Fatalf("DataSrv dependency verification should preserve resolved install refs: %#v", depVerification)
	}
	dataSrvEvidence := anyMap(metadata["test_evidence"])
	dataSrvApproval := anyMap(dataSrvEvidence["approval_instance"])
	if dataSrvApproval == nil {
		dataSrvApproval = anyMap(dataSrvEvidence["approvalInstance"])
	}
	if dataSrvApproval == nil || maclawAppStringValue(dataSrvApproval, "approval_id", "approvalID") != "approval-expense-1001" || maclawAppStringValue(dataSrvApproval, "record_id", "recordID") != "expense-1001" {
		t.Fatalf("DataSrv metadata should preserve nested approval evidence: %#v", dataSrvEvidence)
	}
	if payload := anyMap(firstNonEmptyMaclawAppAny(dataSrvApproval["result_payload"], dataSrvApproval["resultPayload"])); payload == nil || payload["approval_result"] != "approved" {
		t.Fatalf("DataSrv approval evidence should preserve nested result payload: %#v", dataSrvApproval)
	}
	if outputs := anySlice(dataSrvApproval["outputs"]); len(outputs) != 1 {
		t.Fatalf("DataSrv approval evidence should preserve outputs: %#v", dataSrvApproval)
	}
	if artifacts := anySlice(dataSrvApproval["artifacts"]); len(artifacts) != 1 {
		t.Fatalf("DataSrv approval evidence should preserve artifacts: %#v", dataSrvApproval)
	}

	queryURL := dataSrv.URL + "/api/v1/data/app-installations?hub_capability_id=cap-expense-approval&hub_version_key=" + url.QueryEscape("enterprise_hub:skill:maclaw-app:expense-approval@pkg") + "&hub_review_status=published"
	queryReq, err := http.NewRequest(http.MethodGet, queryURL, nil)
	if err != nil {
		t.Fatalf("create DataSrv Hub identity query: %v", err)
	}
	queryReq.Header.Set("Authorization", "Bearer data-token")
	queryResp, err := http.DefaultClient.Do(queryReq)
	if err != nil {
		t.Fatalf("query DataSrv app installations by Hub identity: %v", err)
	}
	defer queryResp.Body.Close()
	if queryResp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(queryResp.Body)
		t.Fatalf("DataSrv Hub identity query status=%d body=%s", queryResp.StatusCode, string(bodyBytes))
	}
	var queried struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(queryResp.Body).Decode(&queried); err != nil {
		t.Fatalf("decode DataSrv Hub identity query: %v", err)
	}
	if len(queried.Items) != 1 || queried.Items[0]["app_id"] != "expense-approval" {
		t.Fatalf("DataSrv Hub identity query should return installed approval app: %#v", queried.Items)
	}
	queriedMetadata := anyMap(queried.Items[0]["metadata"])
	if queriedMetadata == nil || queriedMetadata["hub_capability_id"] != "cap-expense-approval" || queriedMetadata["hub_review_status"] != "published" || queriedMetadata["test_evidence_approval_id"] != "approval-expense-1001" {
		t.Fatalf("DataSrv queried install should preserve Hub identity and approval summaries: %#v", queriedMetadata)
	}

	records, err := app.ListMaclawAppInstalls(10)
	if err != nil {
		t.Fatalf("ListMaclawAppInstalls() error = %v", err)
	}
	if len(records) != 1 || records[0].AppID != "expense-approval" || records[0].Kind != "enterprise_approval_app" {
		t.Fatalf("local install audit should contain selected approval app: %#v", records)
	}
	auditReviewEvidence := records[0].ReviewEvidence
	if auditReviewEvidence == nil || auditReviewEvidence["run_id"] != "run-hub-reviewed-expense" || auditReviewEvidence["approval_status"] != "approved" {
		t.Fatalf("local install audit should preserve Hub review evidence: %#v", records[0].ReviewEvidence)
	}
	auditSubmission := records[0].Submission
	if auditSubmission == nil || auditSubmission["capability_id"] != "cap-expense-approval" || auditSubmission["version_key"] != "enterprise_hub:skill:maclaw-app:expense-approval@pkg" {
		t.Fatalf("local install audit should preserve Hub submission identity: %#v", records[0].Submission)
	}
	auditApproval := anyMap(records[0].TestEvidence["approval_instance"])
	if auditApproval == nil {
		auditApproval = anyMap(records[0].TestEvidence["approvalInstance"])
	}
	if auditApproval == nil || maclawAppStringValue(auditApproval, "current_node", "currentNode") != "expense.result" || maclawAppStringValue(auditApproval, "workflow_skill_id", "workflowSkillId") != "expense-workflow" {
		t.Fatalf("local install audit should preserve approval instance evidence: %#v", records[0].TestEvidence)
	}
}
func TestInstallMaclawAppPackageFromHubRejectsWorkflowContractIssues(t *testing.T) {
	tmpHome := t.TempDir()
	workflowDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "expense-flow")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatalf("MkdirAll workflowDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowDir, "skill.md"), []byte("# Expense flow\n"), 0o644); err != nil {
		t.Fatalf("WriteFile workflow skill.md: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/capabilities/maclaw-apps/cap-bad-approval/package" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		var pkg map[string]any
		if err := json.Unmarshal([]byte(maclawAppPackageWithCurrentDefinitionHashes(t, `{
			"schema": "maclaw.app.pack.v1",
			"privateMarker": "x_maclaw_apps",
			"source": "enterprise_hub",
			"resolved_dependencies": [{"id":"expense-flow", "source":"hub", "install_ref":"expense-flow", "kind":"workflow_skill", "required":true, "app_ids":["hub-bad-approval"]}],
			"apps": [{
				"schema": "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"app": {
					"id": "hub-bad-approval",
					"name": "Hub Bad Approval",
					"kind": "enterprise_approval_app",
					"binding": {
							"datasrv": {"domain": "finance", "datasetID": "finance.expenses", "objectRole": "expense_report"},
							"mis": {"approvalBindings": [{"event": "finance.expense.submitted", "objectRole": "expense_report", "workflowSkillId": "expense-flow", "workflowVersion": "9.9.9"}]}
					},
					"dependencies": {"skills": [{"id": "expense-flow", "kind": "workflow_skill", "version": "9.9.9", "required": true, "source": "hub"}]},
					"governance": {
						"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"approval_workspace", "template":"left_nav", "regionCount":3, "regions":[{"id":"inbox", "role":"instance_list", "placement":"left"}, {"id":"form", "role":"input", "placement":"center"}, {"id":"result", "role":"output", "placement":"right"}]},
						"resultContract": {"schema":"maclaw.app.result.v1", "primary":"approval_result", "types":["approval_result"]},
						"testEvidence": {"runId":"run-bad-approval", "testProtocolFingerprint":"proto-bad-approval", "resultPayload":{"approval_result":"approved"}, "resultCoverage":{"ok":true, "primary":"approval_result", "coveredTypes":["approval_result"], "missingTypes":[]}},
						"workflowContract": {"schema":"maclaw.app.workflow_contract.v1", "workflowSkillId":"expense-flow", "workflowVersion":"9.9.9", "objectRole":"expense_report", "requiredInputs":["record_ref", "applicant", "business_payload"], "decisionOutputs":["approved", "rejected", "attention"], "statusMapping":{"pending":"pending", "approved":"approved", "rejected":"rejected", "attention":"attention"}}
					}
				}
			}]
		}`)), &pkg); err != nil {
			t.Fatalf("decode bad workflow package fixture: %v", err)
		}
		packageSHA := strings.Repeat("6", 64)
		versionKey := "enterprise_hub:skill:maclaw-app:hub-bad-approval@pkg"
		publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("GenerateKey() error = %v", err)
		}
		signature := maclawapptest.SignPublishedMaclawAppHubPackage(pkg, publicKey, privateKey, packageSHA, versionKey, "2026-07-01T05:00:00Z", "hub-admin")
		pkg["capability_id"] = "cap-bad-approval"
		pkg["capability"] = map[string]any{"id": "cap-bad-approval", "capability_id": "hub-bad-approval", "display_name": "Hub Bad Approval", "status": "published", "current_version_key": versionKey}
		reviewEvidence := map[string]any{"hub-bad-approval": map[string]any{"run_id": "run-bad-approval", "result_contract_primary": "approval_result", "result_coverage_primary": "approval_result", "result_coverage_covered_count": 1, "has_dependency_verification": true, "has_blocking_dependency": false, "has_workspace_layout": true, "workspace_saved_in_manifest": true}}
		pkg["review_evidence"] = reviewEvidence
		pkg["maclaw_app_review_evidence"] = reviewEvidence
		appMap := anyMap(anyMap(anySlice(pkg["apps"])[0])["app"])
		governance := anyMap(appMap["governance"])
		governance["submission"] = map[string]any{"schema": "maclaw.app.hub_submission.v1", "status": "published", "capability_id": "cap-bad-approval", "market_capability_id": "hub-bad-approval", "version_key": versionKey, "package_sha256": packageSHA, "package_signature": signature, "review_evidence": reviewEvidence}
		ensureMaclawAppPackageDependencyVerificationForHubDownloadTest(t, pkg)
		normalizeMaclawAppPackageWorkspaceFingerprintsForTest(t, pkg)
		_ = json.NewEncoder(w).Encode(pkg)
	}))
	defer server.Close()
	cfg := corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "expense-flow", SkillDir: workflowDir, Status: "active", HubVersion: "2.1.0"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	_, err := app.InstallMaclawAppPackageFromHub("cap-bad-approval")
	if err == nil || !strings.Contains(err.Error(), "approval workflow contract is invalid") || !strings.Contains(err.Error(), "version 2.1.0 does not match required 9.9.9") {
		t.Fatalf("expected workflow contract install error, got %v", err)
	}
	records, err := app.ListMaclawAppInstalls(10)
	if err != nil {
		t.Fatalf("ListMaclawAppInstalls() error = %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("blocked hub install should not write install audit: %#v", records)
	}
}

func TestInstallMaclawAppPackageFromHubRejectsGovernanceReviewIssues(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/capabilities/maclaw-apps/cap-bad-governance/package" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		var pkg map[string]any
		if err := json.Unmarshal([]byte(maclawAppPackageWithCurrentDefinitionHashes(t, `{
			"schema": "maclaw.app.pack.v1",
			"privateMarker": "x_maclaw_apps",
			"source": "enterprise_hub",
			"apps": [{
				"schema": "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"app": {
					"id": "hub-bad-governance",
					"name": "Hub Bad Governance",
					"kind": "tool_app",
					"governance": {
						"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "template":"document_workspace", "regionCount":4},
						"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content"]}
					}
				}
			}]
		}`)), &pkg); err != nil {
			t.Fatalf("decode bad governance package fixture: %v", err)
		}
		packageSHA := strings.Repeat("7", 64)
		versionKey := "enterprise_hub:skill:maclaw-app:hub-bad-governance@pkg"
		publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("GenerateKey() error = %v", err)
		}
		signature := maclawapptest.SignPublishedMaclawAppHubPackage(pkg, publicKey, privateKey, packageSHA, versionKey, "2026-07-01T05:10:00Z", "hub-admin")
		pkg["capability_id"] = "cap-bad-governance"
		pkg["capability"] = map[string]any{"id": "cap-bad-governance", "capability_id": "hub-bad-governance", "display_name": "Hub Bad Governance", "status": "published", "current_version_key": versionKey}
		pkg["resolved_dependencies"] = []any{map[string]any{"id": "hub-bad-governance-skill", "kind": "runtime_skill", "source": "enterprise_hub", "required": true, "app_ids": []string{"hub-bad-governance"}}}
		reviewEvidence := map[string]any{"hub-bad-governance": map[string]any{"run_id": "run-bad-governance", "result_contract_primary": "content", "result_coverage_primary": "content", "result_coverage_covered_count": 0, "has_dependency_verification": true, "has_blocking_dependency": false, "has_workspace_layout": true, "workspace_saved_in_manifest": true}}
		pkg["review_evidence"] = reviewEvidence
		pkg["maclaw_app_review_evidence"] = reviewEvidence
		appMap := anyMap(anyMap(anySlice(pkg["apps"])[0])["app"])
		governance := anyMap(appMap["governance"])
		governance["submission"] = map[string]any{"schema": "maclaw.app.hub_submission.v1", "status": "published", "capability_id": "cap-bad-governance", "market_capability_id": "hub-bad-governance", "version_key": versionKey, "package_sha256": packageSHA, "package_signature": signature, "review_evidence": reviewEvidence}
		ensureMaclawAppPackageDependencyVerificationForHubDownloadTest(t, pkg)
		_ = json.NewEncoder(w).Encode(pkg)
	}))
	defer server.Close()
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	_, err := app.InstallMaclawAppPackageFromHub("cap-bad-governance")
	if err == nil || !strings.Contains(err.Error(), "package governance review failed") || !strings.Contains(err.Error(), "missing successful local run evidence") {
		t.Fatalf("expected governance install error, got %v", err)
	}
	records, err := app.ListMaclawAppInstalls(10)
	if err != nil {
		t.Fatalf("ListMaclawAppInstalls() error = %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("blocked hub install should not write install audit: %#v", records)
	}
}
func TestInstallMaclawAppPackageFromHubUsesSessionTokenFallback(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	var capturedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/api/capabilities/maclaw-apps/cap-session-app/package" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		var pkg map[string]any
		if err := json.Unmarshal([]byte(maclawAppPackageWithCurrentDefinitionHashes(t, `{
				"schema": "maclaw.app.pack.v1",
				"privateMarker": "x_maclaw_apps",
				"apps": [{
				"schema": "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"app": {
					"id": "session-install-app",
					"name": "Session Install App",
					"kind": "tool_app",
					"governance": {
						"submission": {"channel": "hub", "status": "approved", "capability_id": "cap-session-app"},
						"resultContract": {"schema": "maclaw.app.result.v1", "primary": "content", "types": ["content"]},
						"testProtocol": {"schema": "maclaw.app.test_protocol.v1", "sampleInput": {"text": "hello"}, "expectedOutput": {"type": "content"}, "fingerprint": "proto-session-install"},
						"testEvidence": {"runId": "run-session-install", "testProtocolFingerprint": "proto-session-install", "resultPayload": {"content": "done"}}
					}
				}
				}]
			}`)), &pkg); err != nil {
			t.Fatalf("decode Hub install package fixture: %v", err)
		}
		packageSHA := strings.Repeat("c", 64)
		versionKey := "enterprise_hub:skill:maclaw-app:session-install-app@pkg"
		publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("GenerateKey() error = %v", err)
		}
		payload := "maclaw-app\n" + packageSHA + "\n" + versionKey + "\n2026-06-30T08:00:00Z\nhub-admin"
		pkg["package_sha256"] = packageSHA
		pkg["package_signature"] = map[string]any{
			"schema":                 "maclaw.app.package_signature.v1",
			"algorithm":              "ed25519",
			"payload":                payload,
			"public_key_base64":      base64.StdEncoding.EncodeToString(publicKey),
			"public_key_fingerprint": downloadedSkillPublicKeyFingerprint(publicKey),
			"signature_base64":       base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(payload))),
			"package_sha256":         packageSHA,
			"version_key":            versionKey,
			"signed_at":              "2026-06-30T08:00:00Z",
			"signed_by":              "hub-admin",
		}
		markMaclawAppPackageAsPublishedHubDownloadTest(t, pkg, "session-install-app", "cap-session-app", versionKey)
		ensureMaclawAppPackageDependencyVerificationForHubDownloadTest(t, pkg)
		_ = json.NewEncoder(w).Encode(pkg)
	}))
	defer server.Close()
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.SkillMarketSessionToken = "session-token"
	}); err != nil {
		t.Fatalf("PatchConfig(SkillMarketSessionToken) error = %v", err)
	}

	result, err := app.InstallMaclawAppPackageFromHub("cap-session-app")
	if err != nil {
		t.Fatalf("InstallMaclawAppPackageFromHub() error = %v", err)
	}
	if capturedAuth != "Bearer session-token" {
		t.Fatalf("Authorization = %q, want session token", capturedAuth)
	}
	if result["schema"] != "maclaw.app.hub_install.v1" || result["app_count"] != 1 {
		t.Fatalf("unexpected install result: %#v", result)
	}
}
func TestRefreshMaclawAppPackageSubmissionFromHubUpdatesReviewState(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	var capturedPath string
	var capturedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedAuth = r.Header.Get("Authorization")
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		metadata, _ := json.Marshal(map[string]any{
			"review_state":    "approved",
			"reviewer":        "hub-admin",
			"reviewed_at":     "2026-06-17T02:30:00Z",
			"approved_at":     "2026-06-17T02:30:00Z",
			"risk_level":      "low",
			"approved_scopes": []string{"app.run", "app.run"},
			"review_issues": []map[string]any{{
				"path":     "app.governance",
				"severity": "info",
				"message":  "review passed",
			}},
			"review_evidence": map[string]any{"sync-app": map[string]any{
				"run_id":                        "run-hub-reviewed",
				"test_protocol_fingerprint":     "proto-hub-reviewed",
				"result_coverage_primary":       "approval_result",
				"result_coverage_covered_count": 2,
				"result_coverage_missing_count": 0,
				"output_count":                  1,
				"artifact_count":                1,
				"approval_status":               "approved",
				"current_node":                  "sync.result",
			}},
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":                  "cap-sync-app",
			"capability_id":       "sync-app",
			"status":              "approved",
			"current_version_key": "hub-version-sync-app",
			"metadata_json":       string(metadata),
		})
	}))
	defer server.Close()
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if err := app.appendMaclawAppSubmission(maclawAppSubmissionRecord{
		SubmissionID:    "hub-version-sync-app",
		HubCapabilityID: "cap-sync-app",
		SubmittedAt:     "2026-06-17T02:00:00Z",
		Status:          "pending_review",
		Channel:         "hub",
		AppIDs:          []string{"sync-app"},
		Package:         map[string]any{"schema": "maclaw.app.pack.v1"},
		Events: []maclawAppSubmissionEvent{{
			At:           "2026-06-17T02:00:00Z",
			Status:       "pending_review",
			Channel:      "hub",
			SubmissionID: "hub-version-sync-app",
		}},
	}); err != nil {
		t.Fatalf("append submission: %v", err)
	}
	result, err := app.RefreshMaclawAppPackageSubmissionFromHub("hub-version-sync-app")
	if err != nil {
		t.Fatalf("RefreshMaclawAppPackageSubmissionFromHub() error = %v", err)
	}
	if capturedPath != "/api/capabilities/cap-sync-app" || capturedAuth != "Bearer viewer-token" {
		t.Fatalf("unexpected hub detail request path=%q auth=%q", capturedPath, capturedAuth)
	}
	if result["status"] != "approved" || result["hub_capability_id"] != "cap-sync-app" {
		t.Fatalf("unexpected refresh result: %#v", result)
	}
	summaries, err := app.ListMaclawAppPackageSubmissions(10)
	if err != nil {
		t.Fatalf("list submissions: %v", err)
	}
	if len(summaries) != 1 || summaries[0].Status != "approved" || summaries[0].HubCapabilityID != "cap-sync-app" {
		t.Fatalf("unexpected refreshed summary: %#v", summaries)
	}
	if summaries[0].Reviewer != "hub-admin" || summaries[0].ReviewedAt != "2026-06-17T02:30:00Z" || summaries[0].RiskLevel != "low" {
		t.Fatalf("expected review metadata: %#v", summaries[0])
	}
	if len(summaries[0].ApprovedScopes) != 1 || summaries[0].ApprovedScopes[0] != "app.run" {
		t.Fatalf("expected deduped approved scopes: %#v", summaries[0].ApprovedScopes)
	}
	if len(summaries[0].ReviewIssues) != 1 || summaries[0].ReviewIssues[0].Message != "review passed" {
		t.Fatalf("expected review issues: %#v", summaries[0].ReviewIssues)
	}
	if summaries[0].EventCount != 2 || !strings.Contains(summaries[0].Message, "approved") {
		t.Fatalf("expected refresh event summary: %#v", summaries[0])
	}
}

func TestUpdateMaclawAppPackageSubmissionStatusRejectsDuplicateNextID(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.appendMaclawAppSubmission(maclawAppSubmissionRecord{
		SubmissionID: "local-review-a",
		SubmittedAt:  "2026-06-17T01:00:00Z",
		Status:       "submitted",
		Channel:      "local",
		AppIDs:       []string{"app-a"},
	}); err != nil {
		t.Fatalf("append a: %v", err)
	}
	if err := app.appendMaclawAppSubmission(maclawAppSubmissionRecord{
		SubmissionID: "market-review-b",
		SubmittedAt:  "2026-06-17T01:02:00Z",
		Status:       "submitted",
		Channel:      "hub",
		AppIDs:       []string{"app-b"},
	}); err != nil {
		t.Fatalf("append b: %v", err)
	}
	ok, err := app.UpdateMaclawAppPackageSubmissionStatus("local-review-a", maclawAppSubmissionStatusUpdate{
		Status:       "published",
		Channel:      "hub",
		SubmissionID: "market-review-b",
	})
	if err == nil || ok {
		t.Fatalf("expected duplicate next id error, ok=%v err=%v", ok, err)
	}
	summaries, err := app.ListMaclawAppPackageSubmissions(10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected two submissions, got %#v", summaries)
	}
	ids := map[string]bool{}
	for _, summary := range summaries {
		ids[summary.SubmissionID] = true
	}
	if !ids["local-review-a"] || !ids["market-review-b"] {
		t.Fatalf("submission ids should remain unchanged: %#v", summaries)
	}
}

func TestUpdateMaclawAppPackageSubmissionStatusStoresReviewIssues(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pkg := `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {"id": "issue-app", "name": "Issue App"}
		}]
	}`
	result, err := app.SubmitMaclawAppPackage(pkg)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	localID := result["submission_id"].(string)
	ok, err := app.UpdateMaclawAppPackageSubmissionStatus(localID, maclawAppSubmissionStatusUpdate{
		Status:  "review_failed",
		Channel: "hub",
		Message: "changes requested",
		ReviewIssues: []maclawAppReviewIssue{{
			Path:       "apps[0].app.governance.testEvidence",
			Severity:   "error",
			Message:    "missing test evidence",
			Suggestion: "run a local test before publishing",
		}, {
			Severity: "invalid",
			Message:  "unknown severity",
		}, {
			Path: "empty-message",
		}},
	})
	if err != nil || !ok {
		t.Fatalf("update ok=%v err=%v", ok, err)
	}
	summaries, err := app.ListMaclawAppPackageSubmissions(10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(summaries) != 1 || len(summaries[0].ReviewIssues) != 2 {
		t.Fatalf("expected two review issues: %#v", summaries)
	}
	if summaries[0].ReviewIssues[0].Severity != "error" || summaries[0].ReviewIssues[0].Message != "missing test evidence" {
		t.Fatalf("unexpected first issue: %#v", summaries[0].ReviewIssues[0])
	}
	if summaries[0].ReviewIssues[1].Severity != "warning" {
		t.Fatalf("invalid severity should normalize to warning: %#v", summaries[0].ReviewIssues[1])
	}
	detail, err := app.GetMaclawAppPackageSubmission(localID)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	detail.ReviewIssues[0].Message = "mutated"
	again, err := app.GetMaclawAppPackageSubmission(localID)
	if err != nil {
		t.Fatalf("detail again: %v", err)
	}
	if again.ReviewIssues[0].Message != "missing test evidence" {
		t.Fatalf("review issues should be cloned: %#v", again.ReviewIssues)
	}
}
func TestPlanMaclawAppInstallSingleAppChecksDependencies(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "expense-approval-app")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# Expense approval app\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "expense-approval-app", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "expense-approval",
			"name": "Expense Approval",
			"kind": "enterprise_approval_app",
			"binding": { "appSkill": { "id": "expense-approval-app", "version": "1.0.0" } },
			"dependencies": {
				"skills": [
					{ "id": "expense-approval-workflow", "version": ">=1.0.0 <2.0.0", "kind": "workflow_skill", "required": true, "source": "hub" },
					{ "id": "expense-exporter", "kind": "runtime_skill", "required": false }
				]
			}
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	if plan.Schema != "maclaw.app.install_plan.v1" || len(plan.Apps) != 1 || plan.Apps[0].Kind != "enterprise_approval_app" {
		t.Fatalf("unexpected apps plan: %#v", plan)
	}
	if !plan.HasMissingRequired || len(plan.Dependencies) != 3 {
		t.Fatalf("unexpected dependency summary: %#v", plan)
	}
	if dep := maclawAppPlanDepForTest(plan, "expense-approval-app"); dep == nil || !dep.Installed || dep.Action != "skip" || dep.Kind != "app_skill" {
		t.Fatalf("super app skill should be installed: %#v", dep)
	}
	if dep := maclawAppPlanDepForTest(plan, "expense-approval-workflow"); dep == nil || dep.Installed || dep.Action != "blocked" || !dep.Required {
		t.Fatalf("required workflow should block: %#v", dep)
	}
	if dep := maclawAppPlanDepForTest(plan, "expense-exporter"); dep == nil || dep.Action != "optional_missing" || dep.Required {
		t.Fatalf("optional dependency should not block: %#v", dep)
	}
}

func TestPlanMaclawAppInstallBlocksInstalledInactiveRequiredDependency(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	requiredDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "disabled-workflow")
	optionalDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "optional-exporter")
	for _, dir := range []string{requiredDir, optionalDir} {
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
		{Name: "disabled-workflow", SkillDir: requiredDir, Status: "disabled"},
		{Name: "optional-exporter", SkillDir: optionalDir, Status: "needs_setup"},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	pkg := `{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "inactive-deps-app",
			"name": "Inactive Deps App",
			"kind": "enterprise_approval_app",
			"dependencies": {
				"skills": [
					{ "id": "disabled-workflow", "kind": "workflow_skill", "required": true, "source": "hub" },
					{ "id": "optional-exporter", "kind": "runtime_skill", "required": false, "source": "hub" }
				]
			}
		}
	}`

	plan, err := app.PlanMaclawAppInstall(pkg)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	if plan.HasMissingRequired || !plan.HasBlockingDependency {
		t.Fatalf("inactive required dependency should block without counting as missing: %#v", plan)
	}
	required := maclawAppPlanDepForTest(plan, "disabled-workflow")
	if required == nil || !required.Installed || required.Action != "blocked" || required.Health != "disabled" || required.InstalledStatus != "disabled" || !strings.Contains(required.Message, "not active") {
		t.Fatalf("disabled required dependency should block: %#v", required)
	}
	optional := maclawAppPlanDepForTest(plan, "optional-exporter")
	if optional == nil || !optional.Installed || optional.Action != "optional_unhealthy" || optional.Health != "needs_setup" || optional.Required {
		t.Fatalf("inactive optional dependency should degrade without blocking: %#v", optional)
	}

	installedPlan, err := app.InstallMaclawAppDependencies(pkg)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	if installedPlan.HasMissingRequired || !installedPlan.HasBlockingDependency {
		t.Fatalf("install should preserve inactive required dependency block: %#v", installedPlan)
	}
	if dep := maclawAppPlanDepForTest(installedPlan, "disabled-workflow"); dep == nil || dep.Action != "blocked" || dep.Health != "disabled" {
		t.Fatalf("disabled required dependency should remain blocked after install attempt: %#v", dep)
	}

	if _, err := app.RecordMaclawAppInstall(pkg, "market"); err == nil || !strings.Contains(err.Error(), "required Skill dependencies") {
		t.Fatalf("RecordMaclawAppInstall should reject blocking dependencies, got %v", err)
	}
	records, err := app.ListMaclawAppInstalls(10)
	if err != nil {
		t.Fatalf("ListMaclawAppInstalls() error = %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("blocked install should not write install audit records: %#v", records)
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
func TestPlanMaclawAppInstallPackDedupesDependenciesAndNormalizesLegacyKind(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "legacy-enterprise",
				"name": "Legacy Enterprise",
				"kind": "enterprise_app",
				"binding": { "appSkill": { "id": "shared-workflow" } }
			}
		}, {
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "normal-enterprise",
				"name": "Normal Enterprise",
				"kind": "enterprise_normal_app",
				"dependencies": { "skills": [{ "id": "shared-workflow", "required": true }] }
			}
		}]
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	if len(plan.Apps) != 2 || plan.Apps[0].Kind != "enterprise_normal_app" {
		t.Fatalf("legacy kind was not normalized: %#v", plan.Apps)
	}
	dep := maclawAppPlanDepForTest(plan, "shared-workflow")
	if dep == nil || dep.Action != "blocked" || len(dep.AppIDs) != 2 {
		t.Fatalf("shared dependency should be deduped across apps: %#v", dep)
	}
}

func TestInstallMaclawAppDependenciesSkipsInstalledAndBlocksUnsupportedSource(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "installed-app-skill")
	workflowDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "dependency-install-workflow")
	for _, dir := range []string{skillDir, workflowDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll skill dir: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# Installed app skill\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowDir, "skill.md"), []byte("# Dependency install workflow\n"), 0o644); err != nil {
		t.Fatalf("WriteFile workflow skill.md: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "installed-app-skill", SkillDir: skillDir, Status: "active"}, {Name: "dependency-install-workflow", SkillDir: workflowDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "dependency-install-app",
			"name": "Dependency Install App",
			"kind": "enterprise_approval_app",
			"binding": { "appSkill": { "id": "installed-app-skill" } },
			"dependencies": { "skills": [{ "id": "dependency-install-workflow", "kind": "workflow_skill", "required": true, "source": "local" }, { "id": "manual-only-skill", "required": true, "source": "builtin" }] }
		}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	if !plan.HasMissingRequired {
		t.Fatalf("manual-only required dependency should remain missing: %#v", plan)
	}
	if dep := maclawAppPlanDepForTest(plan, "installed-app-skill"); dep == nil || dep.Action != "skip" || !dep.Installed {
		t.Fatalf("installed app skill should be skipped: %#v", dep)
	}
	if dep := maclawAppPlanDepForTest(plan, "manual-only-skill"); dep == nil || dep.Action != "blocked" || !strings.Contains(dep.Message, "cannot be installed automatically") {
		t.Fatalf("unsupported required dependency should be blocked: %#v", dep)
	}
}

func TestMaclawAppInstallSkillSourceNormalizesHubAndMarket(t *testing.T) {
	cases := map[string]string{
		"":               "skillhub",
		"local":          "skillhub",
		"hub":            "skillhub",
		"skillhub":       "skillhub",
		"market":         "skillmarket",
		"skillmarket":    "skillmarket",
		"enterprise_hub": "enterprise_hub",
		"github":         "github",
	}
	for input, want := range cases {
		got, ok := maclawAppInstallSkillSource(input)
		if !ok || got != want {
			t.Fatalf("maclawAppInstallSkillSource(%q) = %q,%v want %q,true", input, got, ok, want)
		}
	}
	if got, ok := maclawAppInstallSkillSource("builtin"); ok || got != "" {
		t.Fatalf("builtin source should not auto-install, got %q,%v", got, ok)
	}
}

func TestInstallMaclawAppDependenciesInstallsHubBackedSources(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	type installCall struct {
		source     string
		id         string
		installRef string
	}
	var calls []installCall
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		calls = append(calls, installCall{source: source, id: id, installRef: installRef})
		skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", id)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# "+id+"\n"), 0o644); err != nil {
			return err
		}
		cfg, err := app.LoadConfig()
		if err != nil {
			return err
		}
		cfg.NLSkills = append(cfg.NLSkills, corelib.NLSkillEntry{Name: id, SkillDir: skillDir, Status: "active", Source: source, HubSkillID: id})
		return app.SaveConfig(cfg)
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "dependency-source-app",
			"name": "Dependency Source App",
			"kind": "enterprise_approval_app",
			"dependencies": { "skills": [
				{ "id": "market-workflow", "kind": "workflow_skill", "required": true, "source": "skillmarket" },
				{ "id": "enterprise-workflow", "kind": "workflow_skill", "required": true, "source": "enterprise_hub", "capability_id": "cap-enterprise-workflow" },
				{ "id": "github-workflow", "kind": "workflow_skill", "required": true, "source": "github", "install_ref": "{\"repo_full_name\":\"acme/github-workflow\",\"raw_url\":\"https://raw.githubusercontent.com/acme/github-workflow/main/SKILL.md\"}" }
			] }
		}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	if len(calls) != 3 {
		t.Fatalf("expected three dependency install calls, got %#v", calls)
	}
	if calls[0] != (installCall{source: "skillmarket", id: "market-workflow"}) || calls[1] != (installCall{source: "enterprise_hub", id: "enterprise-workflow", installRef: "cap-enterprise-workflow"}) || calls[2].source != "github" || calls[2].id != "github-workflow" || !strings.Contains(calls[2].installRef, "raw.githubusercontent.com/acme/github-workflow") {
		t.Fatalf("unexpected dependency install calls: %#v", calls)
	}
	for _, id := range []string{"market-workflow", "enterprise-workflow", "github-workflow"} {
		dep := maclawAppPlanDepForTest(plan, id)
		if dep == nil || !dep.Installed || dep.Action != "installed" || dep.Health != "ready" {
			t.Fatalf("dependency %s should be installed and ready: %#v", id, dep)
		}
	}
	if dep := maclawAppPlanDepForTest(plan, "enterprise-workflow"); dep == nil || dep.InstallRef != "cap-enterprise-workflow" {
		t.Fatalf("enterprise dependency should preserve capability install ref: %#v", dep)
	}
	if dep := maclawAppPlanDepForTest(plan, "github-workflow"); dep == nil || !strings.Contains(dep.InstallRef, "raw.githubusercontent.com/acme/github-workflow") {
		t.Fatalf("github dependency should preserve github install ref: %#v", dep)
	}
	if plan.HasMissingRequired || plan.HasBlockingDependency {
		t.Fatalf("installed hub-backed dependencies should clear blocking flags: %#v", plan)
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
	if !initialPlan.HasMissingRequired || !initialPlan.HasBlockingDependency {
		t.Fatalf("initial plan should block on missing workflow dependency: %#v", initialPlan)
	}
	if dep := maclawAppPlanDepForTest(initialPlan, "expense-workflow"); dep == nil || dep.Action != "blocked" || dep.Health != "missing" {
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

func TestInstallMaclawAppDependenciesClassifiesInstallFailures(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		switch id {
		case "missing-market-workflow":
			return fmt.Errorf("404 skill not found")
		case "policy-enterprise-workflow":
			return fmt.Errorf("blocked by capability market policy: enterprise-only installs required")
		case "checksum-hub-workflow":
			return fmt.Errorf("package checksum mismatch for %s", installRef)
		default:
			return fmt.Errorf("unexpected install %s/%s/%s", source, id, installRef)
		}
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "install-failure-app",
			"name": "Install Failure App",
			"kind": "enterprise_approval_app",
			"dependencies": { "skills": [
				{ "id": "missing-market-workflow", "kind": "workflow_skill", "required": true, "source": "skillmarket" },
				{ "id": "policy-enterprise-workflow", "kind": "workflow_skill", "required": true, "source": "enterprise_hub", "install_ref": "enterprise_hub://capabilities/policy-enterprise-workflow@1.0.0" },
				{ "id": "checksum-hub-workflow", "kind": "workflow_skill", "required": true, "source": "hub", "install_ref": "hub://skills/checksum-hub-workflow@1.0.0" }
			] }
		}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	cases := map[string]struct {
		code  string
		stage string
	}{
		"missing-market-workflow":    {code: "not_found", stage: "skillmarket_download"},
		"policy-enterprise-workflow": {code: "policy_rejected", stage: "enterprise_hub_install"},
		"checksum-hub-workflow":      {code: "package_integrity_failed", stage: "skillhub_download"},
	}
	for id, want := range cases {
		dep := maclawAppPlanDepForTest(plan, id)
		if dep == nil || dep.Action != "failed" || dep.Health != "missing" || dep.InstallErrorCode != want.code || dep.InstallErrorStage != want.stage || dep.InstallErrorDetail == "" {
			t.Fatalf("dependency %s failure diagnostics mismatch: got %#v want code=%s stage=%s", id, dep, want.code, want.stage)
		}
	}
	if !plan.HasMissingRequired || !plan.HasBlockingDependency {
		t.Fatalf("failed required installs should set blocking flags: %#v", plan)
	}
}
func TestInstallMaclawAppDependenciesParsesInstallRefs(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	type installCall struct {
		source     string
		id         string
		installRef string
	}
	var calls []installCall
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		calls = append(calls, installCall{source: source, id: id, installRef: installRef})
		skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", id)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# "+id+"\n"), 0o644); err != nil {
			return err
		}
		cfg, err := app.LoadConfig()
		if err != nil {
			return err
		}
		cfg.NLSkills = append(cfg.NLSkills, corelib.NLSkillEntry{Name: id, SkillDir: skillDir, Status: "active", Source: source, HubSkillID: installRef})
		return app.SaveConfig(cfg)
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "install-ref-app",
			"name": "Install Ref App",
			"kind": "enterprise_approval_app",
			"dependencies": { "skills": [
				{ "id": "uri-workflow", "kind": "workflow_skill", "required": true, "source": "hub", "install_ref": "hub://skills/uri-workflow@2.1.0" },
				{ "id": "enterprise-workflow", "kind": "workflow_skill", "required": true, "source": "enterprise_hub", "install_ref": "enterprise_hub://capabilities/cap-enterprise-workflow@1.4.0" }
			] }
		}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected two dependency install calls, got %#v", calls)
	}
	if calls[0] != (installCall{source: "skillhub", id: "uri-workflow", installRef: "uri-workflow"}) {
		t.Fatalf("hub URI should install by parsed target, got %#v", calls[0])
	}
	if calls[1] != (installCall{source: "enterprise_hub", id: "enterprise-workflow", installRef: "cap-enterprise-workflow"}) {
		t.Fatalf("enterprise URI should install by parsed capability target, got %#v", calls[1])
	}
	dep := maclawAppPlanDepForTest(plan, "uri-workflow")
	if dep == nil || dep.InstallRefKind != "hub" || dep.InstallRefTarget != "uri-workflow" || dep.InstallRefVersion != "2.1.0" || dep.InstallRefStatus != "ok" || dep.PreflightStatus != "pending" || dep.PreflightCode != "target_resolved" {
		t.Fatalf("hub dependency install_ref diagnostics missing: %#v", dep)
	}
	dep = maclawAppPlanDepForTest(plan, "enterprise-workflow")
	if dep == nil || dep.InstallRefKind != "enterprise_hub" || dep.InstallRefTarget != "cap-enterprise-workflow" || dep.InstallRefVersion != "1.4.0" || dep.InstallRefStatus != "ok" || dep.PreflightStatus != "pending" || dep.PreflightCode != "remote_preflight_unavailable" || dep.PreflightStage != "enterprise_hub_preflight" {
		t.Fatalf("enterprise dependency install_ref diagnostics missing: %#v", dep)
	}
}

func TestPlanMaclawAppInstallPreflightsSkillMarketDependency(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(map[string]any{"urls": []string{server.URL}, "ttl_seconds": 60})
		case "/api/client/quality":
			_ = json.NewEncoder(w).Encode(map[string]any{"quality_score": 99, "routable": true})
		case "/api/v1/skillmarket/search":
			query := r.URL.Query().Get("q")
			var results []SkillSearchResult
			switch query {
			case "market-ready-workflow":
				results = []SkillSearchResult{{ID: "market-ready-workflow", Name: "Market Ready Workflow", Version: "1.2.0", PackageSHA256: "sha-ready", PackageSignature: "sig-ready", PackageDownloadURL: "https://skillmarket.example/download/market-ready-workflow"}}
			case "market-old-workflow":
				results = []SkillSearchResult{{ID: "market-old-workflow", Name: "Market Old Workflow", Version: "1.0.0", PackageSHA256: "sha-old"}}
			case "market-missing-workflow":
				results = []SkillSearchResult{}
			default:
				t.Fatalf("unexpected search query %q", query)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: server.URL}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "skillmarket-preflight-app",
			"name": "SkillMarket Preflight App",
			"kind": "enterprise_approval_app",
			"dependencies": { "skills": [
				{ "id": "market-ready-workflow", "version": "1.2.0", "kind": "workflow_skill", "required": true, "source": "skillmarket" },
				{ "id": "market-old-workflow", "version": "2.0.0", "kind": "workflow_skill", "required": true, "source": "skillmarket" },
				{ "id": "market-missing-workflow", "version": "1.0.0", "kind": "workflow_skill", "required": true, "source": "skillmarket" }
			] }
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	ready := maclawAppPlanDepForTest(plan, "market-ready-workflow")
	if ready == nil || ready.PreflightStatus != "ready" || ready.PreflightCode != "skillmarket_target_ready" || ready.PreflightStage != "skillmarket_preflight" || ready.IntegrityStatus != "ready" || ready.IntegrityCode != "package_integrity_metadata_ready" || ready.PackageSHA256 != "sha-ready" || ready.PackageSignature != "sig-ready" || ready.PackageDownloadURL == "" {
		t.Fatalf("ready SkillMarket dependency preflight mismatch: %#v", ready)
	}
	old := maclawAppPlanDepForTest(plan, "market-old-workflow")
	if old == nil || old.PreflightStatus != "blocked" || old.PreflightCode != "version_mismatch" || old.Action != "blocked" || old.IntegrityStatus != "partial" || old.IntegrityCode != "signature_unavailable" || old.PackageSHA256 != "sha-old" || !strings.Contains(old.PreflightMessage, "version 1.0.0") {
		t.Fatalf("version mismatch SkillMarket dependency should be blocked: %#v", old)
	}
	missing := maclawAppPlanDepForTest(plan, "market-missing-workflow")
	if missing == nil || missing.PreflightStatus != "blocked" || missing.PreflightCode != "not_found" || missing.Action != "blocked" {
		t.Fatalf("missing SkillMarket dependency should be blocked: %#v", missing)
	}
	if !plan.HasMissingRequired || !plan.HasBlockingDependency {
		t.Fatalf("blocked SkillMarket preflight should set blocking flags: %#v", plan)
	}
}
func TestPlanMaclawAppInstallPreflightsEnterpriseHubCapability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer viewer-token" {
			t.Fatalf("Authorization header = %q", got)
		}
		switch r.URL.Path {
		case "/api/capabilities/cap-ready-workflow":
			_ = json.NewEncoder(w).Encode(HubCapabilitySummary{ID: "cap-ready-workflow", CapabilityID: "ready-workflow", CapabilityType: "skill", Status: "published", CurrentVersionKey: "1.2.0", MetadataJSON: `{"package_sha256":"enterprise-sha-ready","package_signature":"enterprise-sig-ready","package_download_url":"https://hub.example/packages/cap-ready-workflow"}`})
		case "/api/capabilities/cap-old-workflow":
			_ = json.NewEncoder(w).Encode(HubCapabilitySummary{ID: "cap-old-workflow", CapabilityID: "old-workflow", CapabilityType: "skill", Status: "published", CurrentVersionKey: "1.0.0", PackageSHA256: "enterprise-sha-old"})
		case "/api/capabilities/cap-missing-workflow":
			http.NotFound(w, r)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "enterprise-preflight-app",
			"name": "Enterprise Preflight App",
			"kind": "enterprise_approval_app",
			"dependencies": { "skills": [
				{ "id": "ready-workflow", "version": "1.2.0", "kind": "workflow_skill", "required": true, "source": "enterprise_hub", "install_ref": "enterprise_hub://capabilities/cap-ready-workflow@1.2.0" },
				{ "id": "old-workflow", "version": "2.0.0", "kind": "workflow_skill", "required": true, "source": "enterprise_hub", "install_ref": "enterprise_hub://capabilities/cap-old-workflow@2.0.0" },
				{ "id": "missing-workflow", "version": "1.0.0", "kind": "workflow_skill", "required": true, "source": "enterprise_hub", "install_ref": "enterprise_hub://capabilities/cap-missing-workflow@1.0.0" }
			] }
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	ready := maclawAppPlanDepForTest(plan, "ready-workflow")
	if ready == nil || ready.PreflightStatus != "ready" || ready.PreflightCode != "enterprise_hub_target_ready" || ready.PreflightStage != "enterprise_hub_preflight" || ready.IntegrityStatus != "ready" || ready.PackageSHA256 != "enterprise-sha-ready" || ready.PackageSignature != "enterprise-sig-ready" || ready.PackageDownloadURL == "" {
		t.Fatalf("ready enterprise dependency preflight mismatch: %#v", ready)
	}
	old := maclawAppPlanDepForTest(plan, "old-workflow")
	if old == nil || old.PreflightStatus != "blocked" || old.PreflightCode != "version_mismatch" || old.Action != "blocked" || old.IntegrityStatus != "partial" || old.IntegrityCode != "signature_unavailable" || old.PackageSHA256 != "enterprise-sha-old" || !strings.Contains(old.PreflightMessage, "version 1.0.0") {
		t.Fatalf("version mismatch enterprise dependency should be blocked: %#v", old)
	}
	missing := maclawAppPlanDepForTest(plan, "missing-workflow")
	if missing == nil || missing.PreflightStatus != "blocked" || missing.PreflightCode != "not_found" || missing.Action != "blocked" {
		t.Fatalf("missing enterprise dependency should be blocked: %#v", missing)
	}
	if !plan.HasMissingRequired || !plan.HasBlockingDependency {
		t.Fatalf("blocked enterprise preflight should set blocking flags: %#v", plan)
	}
}
func TestPlanMaclawAppInstallBlocksInstallRefVersionMismatch(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "version-mismatch-app",
			"name": "Version Mismatch App",
			"kind": "enterprise_approval_app",
			"dependencies": { "skills": [{ "id": "approval-workflow", "version": "2.0.0", "kind": "workflow_skill", "required": true, "source": "hub", "install_ref": "hub://skills/approval-workflow@1.0.0" }] }
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "approval-workflow")
	if dep == nil || dep.PreflightStatus != "blocked" || dep.PreflightCode != "version_mismatch" || dep.PreflightStage != "install_ref" || dep.Action != "blocked" || !strings.Contains(dep.PreflightMessage, "install_ref version 1.0.0") {
		t.Fatalf("install_ref version mismatch should be blocked by preflight: %#v", dep)
	}
	if !plan.HasMissingRequired || !plan.HasBlockingDependency {
		t.Fatalf("install_ref version mismatch should set blocking flags: %#v", plan)
	}
}
func TestPlanMaclawAppInstallBlocksEnterpriseDependencyWithoutInstallRef(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "missing-install-ref-app",
			"name": "Missing Install Ref App",
			"kind": "enterprise_approval_app",
			"dependencies": { "skills": [{ "id": "enterprise-only-workflow", "kind": "workflow_skill", "required": true, "source": "enterprise_hub" }] }
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "enterprise-only-workflow")
	if dep == nil || dep.InstallRefStatus != "missing" || dep.PreflightStatus != "blocked" || dep.PreflightCode != "missing_install_ref" || dep.Action != "blocked" || !strings.Contains(dep.Message, "must include install_ref") {
		t.Fatalf("enterprise dependency should be blocked with install_ref diagnostic: %#v", dep)
	}
	if !plan.HasMissingRequired || !plan.HasBlockingDependency {
		t.Fatalf("missing enterprise install_ref should set blocking flags: %#v", plan)
	}
}
func TestPlanMaclawAppInstallNormalizesWorkspaceLayout(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "layout-approval",
			"name": "Layout Approval",
			"kind": "enterprise_approval_app",
			"dependencies": { "skills": [{ "id": "layout-approval-workflow", "kind": "workflow_skill", "required": true, "source": "hub" }] },
			"ui": {
				"entry": "approval_workspace",
				"layouts": {
					"approval_workspace": {
						"template": "left_nav",
						"density": "compact",
						"regions": [
							{"id":"request_form","role":"input","placement":"left"},
							{"id":"approval_detail","role":"detail","placement":"center"}
						]
					}
				}
			}
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	if len(plan.Apps) != 1 || plan.Apps[0].ID != "layout-approval" {
		t.Fatalf("unexpected plan apps: %#v", plan.Apps)
	}

	entries, err := parseMaclawAppInstallEntries(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {"id":"layout-tool","name":"Layout Tool","kind":"tool_app"}
	}`)
	if err != nil {
		t.Fatalf("parse tool app error = %v", err)
	}
	ui := anyMap(entries[0].App["ui"])
	if ui == nil || ui["schema"] != "maclaw.app.ui.v1" || ui["entry"] != "tool_workspace" {
		t.Fatalf("default tool workspace layout missing: %#v", entries[0].App["ui"])
	}
	layouts := anyMap(ui["layouts"])
	layout := anyMap(layouts["tool_workspace"])
	if layout == nil || layout["type"] != "tool_workspace" {
		t.Fatalf("default tool layout missing: %#v", ui)

		studioEntries, err := parseMaclawAppInstallEntries(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "studio-layout-tool",
			"name": "Studio Layout Tool",
			"kind": "tool_app",
			"binding": {
				"skill": {"id":"studio-layout-skill", "appDefinitionFile":"maclaw.apps.json"},
				"ui": {
					"schema": "maclaw.app.ui.v1",
					"entry": "tool_workspace",
					"generated": true,
					"layouts": {
						"tool_workspace": {
							"template": "classic_split",
							"density": "compact",
							"primaryRegion": "left",
							"outputRegion": "right",
							"regions": [
								{"id":"file_queue","role":"input","placement":"left"},
								{"id":"output_panel","role":"output","placement":"right"}
							],
							"studio": {"savedInManifest": true}
						}
					}
				}
			}
		}
	}`)
		if err != nil {
			t.Fatalf("parse studio binding ui app error = %v", err)
		}
		studioUI := anyMap(studioEntries[0].App["ui"])
		studioBinding := anyMap(studioEntries[0].App["binding"])
		studioBindingUI := anyMap(studioBinding["ui"])
		if studioUI == nil || studioUI["schema"] != "maclaw.app.ui.v1" || studioUI["entry"] != "tool_workspace" {
			t.Fatalf("studio binding ui should be promoted to app ui: %#v", studioEntries[0].App)
		}
		studioLayouts := anyMap(studioUI["layouts"])
		studioLayout := anyMap(studioLayouts["tool_workspace"])
		if studioLayout == nil || studioLayout["template"] != "classic_split" || studioLayout["density"] != "compact" || studioLayout["primaryRegion"] != "left" || studioLayout["outputRegion"] != "right" {
			t.Fatalf("promoted studio ui should preserve adjusted layout: %#v", studioUI)
		}
		if regions := anySlice(studioLayout["regions"]); len(regions) != 2 {
			t.Fatalf("promoted studio ui should preserve region placement: %#v", studioLayout["regions"])
		}
		if studioBindingUI == nil || anyMap(anyMap(studioBindingUI["layouts"])["tool_workspace"])["template"] != "classic_split" {
			t.Fatalf("original binding ui should remain readable for install evidence: %#v", studioBinding)
		}

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

func TestPlanMaclawAppInstallRejectsInvalidWorkspaceLayout(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	_, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "bad-layout",
			"name": "Bad Layout",
			"kind": "enterprise_normal_app",
			"ui": {"entry":"business_workspace","layouts":"not-an-object"}
		}
	}`)
	if err == nil || !strings.Contains(err.Error(), "ui.layouts") {
		t.Fatalf("expected ui.layouts validation error, got %v", err)
	}
}

func TestPlanMaclawAppInstallEnforcesAppKindContracts(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	cases := []struct {
		name    string
		pkg     string
		wantErr string
	}{
		{
			name: "approval app requires workflow binding",
			pkg: `{
				"schema": "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"app": {
					"id": "approval-without-workflow",
					"name": "Approval Without Workflow",
					"kind": "enterprise_approval_app",
					"binding": {"datasrv": {"domain": "finance", "datasetID": "finance.expenses", "objectRole": "expense_report"}}
				}
			}`,
			wantErr: "workflow_skill dependency is required for enterprise_approval_app",
		},
		{
			name: "normal app cannot declare approval bindings",
			pkg: `{
				"schema": "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"app": {
					"id": "normal-with-approval",
					"name": "Normal With Approval",
					"kind": "enterprise_normal_app",
					"binding": {
						"datasrv": {"domain": "crm", "datasetID": "crm.customers", "objectRole": "customer"},
						"mis": {"approvalBindings": [{"event": "crm.customer.changed", "workflowSkillId": "customer-approval"}]}
					}
				}
			}`,
			wantErr: "approvalBindings is only valid for enterprise_approval_app",
		},
		{
			name: "tool app cannot declare datasrv",
			pkg: `{
				"schema": "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"installUnit": "skill",
				"app": {
					"id": "tool-with-datasrv",
					"name": "Tool With DataSrv",
					"kind": "tool_app",
					"binding": {
						"skill": {"id": "pdf-tool"},
						"datasrv": {"domain": "finance", "datasetID": "finance.expenses"}
					}
				}
			}`,
			wantErr: "binding.datasrv is not valid for tool_app",
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

func TestPlanMaclawAppInstallTreatsBindingSkillAsDependency(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"installUnit": "skill",
		"app": {
			"id": "tool-binding-skill",
			"name": "Tool Binding Skill",
			"kind": "tool_app",
			"binding": { "skill": { "id": "doc-archive", "version": "1.0.0" } }
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "doc-archive")
	if dep == nil || dep.Kind != "runtime_skill" || !dep.Required || dep.Action != "blocked" {
		t.Fatalf("binding.skill should be a required runtime dependency: %#v", dep)
	}
}
func TestPlanMaclawAppInstallScopesSharedDependencyRequirementPerApp(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [
			{
				"schema": "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"app": {
					"id": "required-app",
					"name": "Required App",
					"kind": "enterprise_normal_app",
					"dependencies": {"skills": [{"id":"shared-skill", "kind":"runtime_skill", "required":true, "source":"hub"}]}
				}
			},
			{
				"schema": "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"app": {
					"id": "optional-app",
					"name": "Optional App",
					"kind": "enterprise_normal_app",
					"dependencies": {"skills": [{"id":"shared-skill", "kind":"runtime_skill", "required":false, "source":"hub"}]}
				}
			}
		]
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	var requiredDep, optionalDep *maclawAppInstallPlanDependency
	for i := range plan.Dependencies {
		dep := &plan.Dependencies[i]
		if dep.ID != "shared-skill" {
			continue
		}
		if dep.Required {
			requiredDep = dep
		} else {
			optionalDep = dep
		}
	}
	if requiredDep == nil || len(requiredDep.AppIDs) != 1 || requiredDep.AppIDs[0] != "required-app" || requiredDep.Action != "blocked" {
		t.Fatalf("required app should retain its blocking shared dependency: %#v", plan.Dependencies)
	}
	if optionalDep == nil || len(optionalDep.AppIDs) != 1 || optionalDep.AppIDs[0] != "optional-app" || optionalDep.Action != "optional_missing" || optionalDep.Required {
		t.Fatalf("optional app should retain optional shared dependency: %#v", plan.Dependencies)
	}
	if hasBlockingMaclawAppRequiredDependencyForApp(plan.Dependencies, "optional-app") {
		t.Fatalf("optional app should not be blocked by required app dependency: %#v", plan.Dependencies)
	}
}

func TestPlanMaclawAppInstallHonorsBindingSkillSourcesAndSnakeCaseAppSkill(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "source-aware-app",
			"name": "Source Aware App",
			"kind": "enterprise_normal_app",
			"binding": {
				"skill": { "id": "doc-archive", "version": "1.0.0", "source": "market" },
				"app_skill": { "id": "source-aware-super-skill", "version": "2.0.0", "source": "local" }
			}
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	if dep := maclawAppPlanDepForTest(plan, "doc-archive"); dep == nil || dep.Source != "market" || dep.Version != "1.0.0" || dep.Kind != "runtime_skill" || !dep.Required {
		t.Fatalf("binding.skill source/version should be preserved: %#v", dep)
	}
	if dep := maclawAppPlanDepForTest(plan, "source-aware-super-skill"); dep == nil || dep.Source != "local" || dep.Version != "2.0.0" || dep.Kind != "app_skill" || !dep.Required {
		t.Fatalf("binding.app_skill should be a source-aware app dependency: %#v", dep)
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
	if dep == nil || dep.Kind != "workflow_skill" || dep.Version != "3.0.0" || !dep.Required || dep.Action != "blocked" {
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
func TestRecordMaclawAppInstallRejectsGovernanceReviewErrors(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pkg := `{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"installUnit": "skill",
		"app": {
			"id": "bad-layout-install",
			"name": "Bad Layout Install",
			"kind": "tool_app",
			"governance": {
				"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "template":"document_workspace", "regionCount":1, "regions":[{"id":"file_queue", "role":"input", "placement":"left"}]},
				"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content"]},
				"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-layout","sampleInput":{"sample":true},"expectedOutput":{"content":"ok"},"requiredRoles":["tester"],"requiredScopes":["app.run"],"riskLevel":"low"}, "testProtocolFingerprint":"proto-layout", "runId":"run-layout", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"content":"ok"}}
			}
		}
	}`

	if _, err := app.RecordMaclawAppInstall(pkg, "market"); err == nil || !strings.Contains(err.Error(), "package governance review failed") {
		t.Fatalf("expected governance review install error, got %v", err)
	}
	records, err := app.ListMaclawAppInstalls(10)
	if err != nil {
		t.Fatalf("ListMaclawAppInstalls() error = %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("governance-blocked install should not write audit records: %#v", records)
	}
}
func TestMaclawAppWorkspaceLayoutMetadataFallsBackToGovernanceLayout(t *testing.T) {
	entry := parsedMaclawAppEntry{
		ID:   "layout-governance-only",
		Name: "Governance Layout Only",
		Kind: "enterprise_normal_app",
		App: map[string]any{
			"id":   "layout-governance-only",
			"name": "Governance Layout Only",
			"kind": "enterprise_normal_app",
			"governance": map[string]any{
				"workspaceLayout": map[string]any{
					"schema":        "maclaw.app.ui.v1",
					"entry":         "business_workspace",
					"template":      "dashboard",
					"density":       "spacious",
					"primaryRegion": "right",
					"outputRegion":  "modal",
					"regions": []any{
						map[string]any{"id": "operation_form", "role": "input", "placement": "right"},
						map[string]any{"id": "record_list", "role": "record_list", "placement": "center"},
						map[string]any{"id": "result_panel", "role": "output", "placement": "modal"},
					},
				},
			},
		},
	}

	layout := maclawAppWorkspaceLayoutMetadataForEntry(entry)
	if layout == nil {
		t.Fatal("expected governance workspace layout fallback")
	}
	if layout["entry"] != "business_workspace" || layout["template"] != "dashboard" || layout["density"] != "spacious" {
		t.Fatalf("unexpected governance workspace layout identity: %#v", layout)
	}
	if layout["primaryRegion"] != "right" || layout["primary_region"] != "right" || layout["outputRegion"] != "modal" || layout["output_region"] != "modal" {
		t.Fatalf("workspace layout should expose camel and snake region aliases: %#v", layout)
	}
	if layout["regionCount"] != 3 || layout["region_count"] != 3 {
		t.Fatalf("workspace layout should expose camel and snake region counts: %#v", layout)
	}
	regions, ok := layout["regions"].([]any)
	if !ok || len(regions) != 3 {
		t.Fatalf("workspace layout should preserve regions: %#v", layout["regions"])
	}
}
func TestRecordMaclawAppInstallPersistsNewestInstallAudit(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "doc-archive")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# Doc archive\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "doc-archive", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	pkg := `{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"installUnit": "skill",
			"app": {
				"id": "market-doc-archive",
				"name": "Doc Archive",
				"version": 7,
				"kind": "tool_app",
				"binding": {
					"skill": { "id": "doc-archive", "version": "1.2.3", "source": "hub" },
					"ui": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "generated":true, "layouts":{"tool_workspace":{"template":"document_workspace", "density":"compact", "regions":[{"id":"input", "role":"input", "placement":"left"}, {"id":"status", "role":"status", "placement":"right"}, {"id":"output", "role":"output", "placement":"right"}]}}},
					"testProtocol": {"schema":"maclaw.app.test_protocol.v1", "fingerprint":"proto-doc-archive", "sampleInput":{"file":"demo.pdf"}, "expectedOutput":{"content":"archived"}, "requiredRoles":["tester"], "requiredScopes":["app.run"], "riskLevel":"low"}
				},
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "template":"document_workspace", "density":"compact", "regionCount":3},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content", "document"]},
					"dependencyVerification": {"schema":"maclaw.app.install_plan.v1", "verifiedAt":"2026-06-17T00:59:00Z", "dependencyCount":1, "hasMissingRequired":false, "hasBlockingDependency":false, "dependencies":[{"id":"doc-archive", "installed":true, "health":"ready"}]},
					"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1", "fingerprint":"proto-doc-archive", "sampleInput":{"file":"demo.pdf"}, "expectedOutput":{"content":"archived"}, "requiredRoles":["tester"], "requiredScopes":["app.run"], "riskLevel":"low"}, "testProtocolFingerprint":"proto-doc-archive", "runId":"run-doc-archive", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"content":"archived"}}
				}
			}
			}`
	pkg = maclawAppPackageWithCurrentDefinitionHashes(t, pkg)
	result, err := app.RecordMaclawAppInstall(pkg, "market")
	if err != nil {
		t.Fatalf("RecordMaclawAppInstall() error = %v", err)
	}
	if result["app_count"] != 1 || result["package_sha"] == "" || result["package_sha256"] != result["package_sha"] || result["has_missing_required"] != false || result["has_blocking_dependency"] != false {
		t.Fatalf("unexpected record result: %#v", result)
	}
	if deps, ok := result["dependencies"].([]maclawAppInstallPlanDependency); !ok || len(deps) != 1 || !deps[0].Installed {
		t.Fatalf("install result should expose dependency state: %#v", result["dependencies"])
	}
	installEvidence, ok := result["install_evidence"].(map[string]any)
	if !ok {
		t.Fatalf("install result should expose per-app evidence: %#v", result["install_evidence"])
	}
	docEvidence, ok := installEvidence["market-doc-archive"].(map[string]interface{})
	if !ok {
		t.Fatalf("install evidence should include app id: %#v", installEvidence)
	}
	if workspace := anyMap(docEvidence["workspace_layout"]); maclawAppStringValue(workspace, "entry") != "tool_workspace" || maclawAppStringValue(workspace, "template") != "document_workspace" {
		t.Fatalf("install evidence missing workspace layout: %#v", docEvidence["workspace_layout"])
	}
	if resultContract := anyMap(docEvidence["result_contract"]); maclawAppStringValue(resultContract, "primary") != "content" {
		t.Fatalf("install evidence missing result contract: %#v", docEvidence["result_contract"])
	}
	if testEvidence := anyMap(docEvidence["test_evidence"]); maclawAppStringValue(testEvidence, "runId", "run_id") != "run-doc-archive" || maclawAppStringValue(testEvidence, "testProtocolFingerprint", "test_protocol_fingerprint") != "proto-doc-archive" {
		t.Fatalf("install evidence missing test evidence: %#v", docEvidence["test_evidence"])
	}
	if verification := anyMap(docEvidence["dependency_verification"]); maclawAppStringValue(verification, "schema") != "maclaw.app.install_plan.v1" {
		t.Fatalf("install evidence missing dependency verification: %#v", docEvidence["dependency_verification"])
	}
	appVersions, ok := result["app_versions"].(map[string]maclawAppInstallVersionSnapshot)
	if !ok || appVersions["market-doc-archive"].AppEntryVersion != "7" || appVersions["market-doc-archive"].AppSkill == nil || appVersions["market-doc-archive"].AppSkill.Version != "1.2.3" {
		t.Fatalf("install result should expose app version snapshot: %#v", result["app_versions"])
	}
	records, err := app.ListMaclawAppInstalls(10)
	if err != nil {
		t.Fatalf("ListMaclawAppInstalls() error = %v", err)
	}
	if len(records) != 1 || records[0].AppID != "market-doc-archive" || records[0].Source != "market" {
		t.Fatalf("unexpected install records: %#v", records)
	}
	if records[0].HasMissingRequired || len(records[0].Dependencies) != 1 || !records[0].Dependencies[0].Installed {
		t.Fatalf("expected installed dependency snapshot: %#v", records[0])
	}
	if verification := anyMap(records[0].DependencyVerification); maclawAppStringValue(verification, "schema") != "maclaw.app.install_plan.v1" || verification["dependencyCount"] != float64(1) {
		t.Fatalf("expected install record dependency verification snapshot: %#v", records[0].DependencyVerification)
	}
	if records[0].VersionSnapshot.AppEntryVersion != "7" || records[0].VersionSnapshot.AppSkill == nil || records[0].VersionSnapshot.AppSkill.ID != "doc-archive" || records[0].VersionSnapshot.AppSkill.Version != "1.2.3" {
		t.Fatalf("expected install record version snapshot: %#v", records[0].VersionSnapshot)
	}
	if records[0].Package == nil || stringMapValue(records[0].Package, "schema") != "maclaw.app.v1" {
		t.Fatalf("expected install record package snapshot: %#v", records[0].Package)
	}
	if maclawAppStringValue(records[0].WorkspaceLayout, "entry") != "tool_workspace" || maclawAppStringValue(records[0].ResultContract, "primary") != "content" || maclawAppStringValue(records[0].TestEvidence, "runId", "run_id") != "run-doc-archive" {
		t.Fatalf("expected install record evidence snapshots: workspace=%#v result=%#v test=%#v", records[0].WorkspaceLayout, records[0].ResultContract, records[0].TestEvidence)
	}
	packageJSON, err := json.Marshal(records[0].Package)
	if err != nil {
		t.Fatalf("marshal install record package: %v", err)
	}
	recheck, err := app.PlanMaclawAppInstall(string(packageJSON))
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall from install record package error = %v", err)
	}
	if len(recheck.Apps) != 1 || recheck.Apps[0].ID != "market-doc-archive" || recheck.HasMissingRequired || recheck.HasBlockingDependency {
		t.Fatalf("install record package should recheck dependency health: %#v", recheck)
	}
	if _, err := app.RecordMaclawAppInstall(pkg, "market"); err != nil {
		t.Fatalf("RecordMaclawAppInstall second call error = %v", err)
	}
	records, err = app.ListMaclawAppInstalls(10)
	if err != nil {
		t.Fatalf("ListMaclawAppInstalls after upsert error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("install records should upsert by app id: %#v", records)
	}
}
func TestMaclawAppInstallEvidenceGeneratesDependencyVerification(t *testing.T) {
	entry := parsedMaclawAppEntry{
		ID:   "expense-approval",
		Name: "Expense Approval",
		Kind: "enterprise_approval_app",
		Entry: map[string]any{
			"schema": "maclaw.app.v1",
		},
		App: map[string]any{
			"id":   "expense-approval",
			"name": "Expense Approval",
			"kind": "enterprise_approval_app",
			"binding": map[string]any{
				"workflow": map[string]any{
					"schema":        "maclaw.app.workflow.v1",
					"submitNode":    "expense.submit",
					"approvalNode":  "expense.manager_review",
					"resultNode":    "expense.result",
					"attentionNode": "expense.attention",
					"statusMapping": map[string]any{"pending": "approval_pending", "approved": "approved", "rejected": "rejected", "attention": "attention"},
				},
			},
		},
	}
	dependencies := []maclawAppInstallPlanDependency{
		{ID: "expense-workflow", Kind: "workflow_skill", Required: true, AppIDs: []string{"expense-approval"}, Installed: true, Health: "ready", Action: "skip"},
		{ID: "expense-export", Kind: "skill", Required: false, AppIDs: []string{"expense-approval"}, Installed: false, Health: "missing", Action: "optional_missing"},
	}

	evidence := maclawAppInstallEvidenceByApp([]parsedMaclawAppEntry{entry}, dependencies, nil)
	appEvidence, ok := evidence["expense-approval"].(map[string]interface{})
	if !ok {
		t.Fatalf("install evidence should include app id: %#v", evidence)
	}
	verification := anyMap(appEvidence["dependency_verification"])
	if verification == nil || maclawAppStringValue(verification, "schema") != "maclaw.app.install_plan.v1" || verification["app_count"] != 1 || verification["dependency_count"] != 2 || verification["has_missing_required"] != false || verification["has_blocking_dependency"] != false {
		t.Fatalf("install evidence should generate dependency verification: %#v", appEvidence["dependency_verification"])
	}
	if _, err := time.Parse(time.RFC3339, maclawAppStringValue(verification, "verified_at")); err != nil {
		t.Fatalf("generated dependency verification should include install-time verified_at: %#v", verification)
	}
	verifiedDependencies, ok := verification["dependencies"].([]maclawAppInstallPlanDependency)
	if !ok || len(verifiedDependencies) != 2 || verifiedDependencies[0].ID != "expense-workflow" || !verifiedDependencies[0].Installed || verifiedDependencies[1].Action != "optional_missing" {
		t.Fatalf("generated dependency verification should carry per-app dependencies: %#v", verification["dependencies"])
	}
	workflowMapping := anyMap(appEvidence["workflow_mapping"])
	if workflowMapping == nil || workflowMapping["schema"] != "maclaw.app.workflow.v1" || workflowMapping["approvalNode"] != "expense.manager_review" || workflowMapping["resultNode"] != "expense.result" {
		t.Fatalf("install evidence should preserve approval workflow mapping: %#v", appEvidence["workflow_mapping"])
	}
	statusMapping := anyMap(workflowMapping["statusMapping"])
	if statusMapping == nil || statusMapping["pending"] != "approval_pending" || statusMapping["attention"] != "attention" {
		t.Fatalf("install evidence should preserve workflow status mapping: %#v", workflowMapping)
	}
}
func TestMaclawAppDataSrvInstallationPayloadsScopeDependenciesPerApp(t *testing.T) {
	entries := []parsedMaclawAppEntry{
		{
			ID:   "selected-app",
			Name: "Selected App",
			Kind: "enterprise_normal_app",
			Entry: map[string]any{
				"schema": "maclaw.app.v1",
			},
			App: map[string]any{
				"id":   "selected-app",
				"name": "Selected App",
				"kind": "enterprise_normal_app",
				"binding": map[string]any{
					"datasrv": map[string]any{"domain": "finance", "datasetID": "finance.selected", "objectRole": "selected_record"},
					"ui": map[string]any{
						"schema":    "maclaw.app.ui.v1",
						"generated": true,
						"entry":     "business_workspace",
						"layouts": map[string]any{
							"business_workspace": map[string]any{
								"template":      "dashboard",
								"density":       "compact",
								"primaryRegion": "center",
								"outputRegion":  "bottom",
								"regions": []any{
									map[string]any{"id": "operation_form", "role": "input", "placement": "center", "order": 1},
									map[string]any{"id": "record_list", "role": "record_list", "placement": "left", "order": 2},
									map[string]any{"id": "output_panel", "role": "output", "placement": "bottom", "order": 3},
									map[string]any{"id": "record_detail", "role": "detail", "placement": "right", "visible": false, "order": 4},
								},
								"fingerprint": "layout-selected",
								"studio": map[string]any{
									"editable":        true,
									"savedInManifest": true,
									"updatedBy":       "app_studio",
								},
							},
						},
					},
				},
				"governance": map[string]any{
					"workspaceLayout": map[string]any{
						"entry":              "business_workspace",
						"template":           "dashboard",
						"density":            "compact",
						"primaryRegion":      "center",
						"outputRegion":       "bottom",
						"fingerprint":        "layout-selected",
						"savedInManifest":    true,
						"regionIds":          []any{"operation_form", "record_list", "output_panel", "record_detail"},
						"visibleRegionCount": 3,
					},
					"testEvidence": map[string]any{
						"runId": "run-selected-1",
						"resultPayload": map[string]any{
							"status":     "completed",
							"resultType": "content",
							"content":    "selected app ready",
						},
						"outputs": []any{
							map[string]any{"kind": "content", "text": "selected app ready"},
							map[string]any{"kind": "document", "title": "Selected export"},
						},
						"artifacts": []any{
							map[string]any{"name": "selected-export.pdf", "uri": "artifact://selected/export.pdf", "type": "document"},
						},
					},
					"dependencyVerification": map[string]any{
						"schema":                "maclaw.app.install_plan.v1",
						"verifiedAt":            "2026-07-01T03:00:00Z",
						"dependencyCount":       1,
						"hasMissingRequired":    false,
						"hasBlockingDependency": false,
						"dependencies": []any{
							map[string]any{"id": "selected-skill", "kind": "runtime_skill", "source": "hub", "required": true, "installed": true, "health": "ready"},
						},
					},
				},
			},
		},
		{
			ID:   "other-app",
			Name: "Other App",
			Kind: "enterprise_normal_app",
			Entry: map[string]any{
				"schema": "maclaw.app.v1",
			},
			App: map[string]any{
				"id":   "other-app",
				"name": "Other App",
				"kind": "enterprise_normal_app",
				"binding": map[string]any{
					"datasrv": map[string]any{"domain": "finance", "datasetID": "finance.other", "objectRole": "other_record"},
				},
			},
		},
	}
	dependencies := []maclawAppInstallPlanDependency{
		{ID: "selected-skill", Kind: "runtime_skill", Required: true, Source: "hub", InstallRef: "hub://skills/selected-skill@1.2.0", InstallRefKind: "hub", InstallRefTarget: "selected-skill", InstallRefVersion: "1.2.0", InstallRefStatus: "ok", PreflightStatus: "ready", PreflightCode: "skillhub_target_ready", PreflightStage: "skillhub_preflight", PackageSHA256: "sha-selected-skill", PackageSignature: "sig-selected-skill", PackageDownloadURL: "https://hub.example/skills/selected-skill/download", IntegrityStatus: "ready", IntegrityCode: "package_integrity_metadata_ready", IntegrityStage: "skillhub_preflight", AppIDs: []string{"market-selected-app"}, Installed: true, Health: "ready", Action: "skip"},
		{ID: "other-skill", Kind: "runtime_skill", Required: true, AppIDs: []string{"other-app"}, Installed: false, Health: "missing", Action: "blocked"},
	}

	payloads := maclawAppDataSrvInstallationPayloads(entries, "market", "sha-selected", 4096, dependencies)
	if len(payloads) != 2 {
		t.Fatalf("expected one DataSrv payload per role-bound app, got %#v", payloads)
	}
	payloadByAppID := map[string]maclawAppDataSrvInstallationPayload{}
	for _, payload := range payloads {
		payloadByAppID[payload.AppID] = payload
	}
	selectedPayload, ok := payloadByAppID["selected-app"]
	if !ok {
		t.Fatalf("missing selected app payload: %#v", payloads)
	}
	selectedMetadata, ok := selectedPayload.Body["metadata"].(map[string]interface{})
	if !ok {
		t.Fatalf("selected app payload missing metadata: %#v", selectedPayload.Body)
	}
	selectedVerification, ok := selectedMetadata["dependency_verification"].(map[string]interface{})
	if !ok || selectedVerification["dependency_count"] != 1 || selectedVerification["has_missing_required"] != false || selectedVerification["has_blocking_dependency"] != false {
		t.Fatalf("selected app dependency verification should be scoped and ready: %#v", selectedMetadata["dependency_verification"])
	}
	if _, err := time.Parse(time.RFC3339, maclawAppStringValue(selectedVerification, "verified_at")); err != nil {
		t.Fatalf("selected app dependency verification should include verified_at: %#v", selectedVerification)
	}
	if selectedMetadata["test_evidence_dependency_verified_at"] != selectedVerification["verified_at"] {
		t.Fatalf("selected app metadata should mirror dependency verification time: %#v", selectedMetadata)
	}
	if selectedMetadata["test_evidence_result_type"] != "content" || selectedMetadata["test_evidence_output_type"] != "content" || selectedMetadata["test_evidence_result_content"] != "selected app ready" {
		t.Fatalf("selected app metadata should carry result evidence summaries: %#v", selectedMetadata)
	}
	if kinds := maclawAppStringListFromAny(selectedMetadata["test_evidence_output_kinds"]); len(kinds) != 2 || kinds[0] != "content" || kinds[1] != "document" {
		t.Fatalf("selected app metadata should carry output kind summaries: %#v", selectedMetadata)
	}
	if selectedMetadata["test_evidence_artifact_uri"] != "artifact://selected/export.pdf" || selectedMetadata["test_evidence_artifact_name"] != "selected-export.pdf" {
		t.Fatalf("selected app metadata should carry primary artifact summaries: %#v", selectedMetadata)
	}
	if uris := maclawAppStringListFromAny(selectedMetadata["test_evidence_artifact_uris"]); len(uris) != 1 || uris[0] != "artifact://selected/export.pdf" {
		t.Fatalf("selected app metadata should carry artifact URI summaries: %#v", selectedMetadata)
	}
	selectedWorkspace := anyMap(selectedMetadata["workspace_layout"])
	if selectedWorkspace == nil || maclawAppStringValue(selectedWorkspace, "entry") != "business_workspace" || maclawAppStringValue(selectedWorkspace, "template") != "dashboard" || maclawAppStringValue(selectedWorkspace, "density") != "compact" {
		t.Fatalf("selected app metadata should carry workspace layout: %#v", selectedMetadata["workspace_layout"])
	}
	if selectedMetadata["workspace_layout_primary_region"] != "center" || selectedMetadata["workspace_layout_output_region"] != "bottom" || selectedMetadata["workspace_layout_fingerprint"] != "layout-selected" {
		t.Fatalf("selected app metadata should flatten workspace layout routing: %#v", selectedMetadata)
	}
	if selectedMetadata["workspace_layout_region_count"] != 4 || selectedMetadata["workspace_layout_visible_region_count"] != 3 {
		t.Fatalf("selected app metadata should flatten workspace layout region counts: %#v", selectedMetadata)
	}
	if regionIDs := maclawAppStringListFromAny(selectedMetadata["workspace_layout_region_ids"]); len(regionIDs) != 4 || regionIDs[0] != "operation_form" || regionIDs[2] != "output_panel" {
		t.Fatalf("selected app metadata should flatten workspace layout region ids: %#v", selectedMetadata["workspace_layout_region_ids"])
	}
	selectedInstallEvidence := anyMap(selectedMetadata["install_evidence"])
	if selectedInstallEvidence == nil {
		t.Fatalf("selected app metadata should carry install evidence: %#v", selectedMetadata)
	}
	installWorkspace := anyMap(selectedInstallEvidence["workspace_layout"])
	if installWorkspace == nil || maclawAppStringValue(installWorkspace, "fingerprint") != "layout-selected" || maclawAppStringValue(installWorkspace, "outputRegion", "output_region") != "bottom" {
		t.Fatalf("selected app install evidence should carry the same workspace layout: %#v", selectedInstallEvidence["workspace_layout"])
	}
	installTestEvidence := anyMap(selectedInstallEvidence["test_evidence"])
	if installTestEvidence == nil || maclawAppStringValue(installTestEvidence, "runId", "run_id") != "run-selected-1" {
		t.Fatalf("selected app install evidence should carry test evidence: %#v", selectedInstallEvidence["test_evidence"])
	}
	installVerification := anyMap(selectedInstallEvidence["dependency_verification"])
	if installVerification == nil || installVerification["dependency_count"] != 1 || installVerification["has_blocking_dependency"] != false {
		t.Fatalf("selected app install evidence should carry scoped dependency verification: %#v", selectedInstallEvidence["dependency_verification"])
	}
	selectedVerificationDependencies := anySlice(selectedVerification["dependencies"])
	if len(selectedVerificationDependencies) != 1 {
		t.Fatalf("selected app verification dependencies should be scoped: %#v", selectedVerification)
	}
	selectedVerificationDependency := anyMap(selectedVerificationDependencies[0])
	if maclawAppStringValue(selectedVerificationDependency, "id") != "selected-skill" || maclawAppStringValue(selectedVerificationDependency, "health") != "ready" {
		t.Fatalf("selected app verification should not include other app dependency: %#v", selectedVerificationDependencies)
	}
	if maclawAppStringValue(selectedVerificationDependency, "install_ref_kind") != "hub" ||
		maclawAppStringValue(selectedVerificationDependency, "install_ref_target") != "selected-skill" ||
		maclawAppStringValue(selectedVerificationDependency, "install_ref_version") != "1.2.0" ||
		maclawAppStringValue(selectedVerificationDependency, "preflight_status") != "ready" ||
		maclawAppStringValue(selectedVerificationDependency, "preflight_code") != "skillhub_target_ready" ||
		maclawAppStringValue(selectedVerificationDependency, "package_sha256") != "sha-selected-skill" ||
		maclawAppStringValue(selectedVerificationDependency, "package_signature") != "sig-selected-skill" ||
		maclawAppStringValue(selectedVerificationDependency, "integrity_status") != "ready" ||
		maclawAppStringValue(selectedVerificationDependency, "integrity_code") != "package_integrity_metadata_ready" {
		t.Fatalf("selected app verification should preserve dependency preflight and integrity diagnostics: %#v", selectedVerificationDependency)
	}
	installTrace := anyMap(selectedVerification["install_trace"])
	if installTrace == nil ||
		installTrace["schema"] != "maclaw.app.dependency_install_trace.v1" ||
		installTrace["dependency_count"] != 1 ||
		installTrace["preflight_checked_count"] != 1 ||
		installTrace["preflight_ready_count"] != 1 ||
		installTrace["integrity_checked_count"] != 1 ||
		installTrace["integrity_ready_count"] != 1 ||
		installTrace["download_available_count"] != 1 ||
		installTrace["signature_available_count"] != 1 ||
		installTrace["install_error_count"] != 0 ||
		installTrace["ok"] != true {
		t.Fatalf("selected app verification should expose dependency install trace summary: %#v", installTrace)
	}
	if selectedMetadata["dependency_preflight_checked_count"] != 1 ||
		selectedMetadata["dependency_preflight_ready_count"] != 1 ||
		selectedMetadata["dependency_integrity_checked_count"] != 1 ||
		selectedMetadata["dependency_integrity_ready_count"] != 1 ||
		selectedMetadata["dependency_download_available_count"] != 1 ||
		selectedMetadata["dependency_signature_available_count"] != 1 ||
		selectedMetadata["dependency_install_error_count"] != 0 ||
		selectedMetadata["dependency_install_trace_ok"] != true {
		t.Fatalf("selected app metadata should flatten dependency install trace summary: %#v", selectedMetadata)
	}
	installVerificationDependencies := anySlice(installVerification["dependencies"])
	if len(installVerificationDependencies) != 1 {
		t.Fatalf("selected app install evidence verification dependencies should be scoped: %#v", installVerification)
	}
	installVerificationDependency := anyMap(installVerificationDependencies[0])
	if maclawAppStringValue(installVerificationDependency, "package_download_url") != "https://hub.example/skills/selected-skill/download" ||
		maclawAppStringValue(installVerificationDependency, "integrity_stage") != "skillhub_preflight" {
		t.Fatalf("selected app install evidence should preserve dependency download and integrity diagnostics: %#v", installVerificationDependency)
	}
	selectedDependencies, ok := selectedMetadata["dependencies"].([]maclawAppInstallPlanDependency)
	if !ok || len(selectedDependencies) != 1 || selectedMetadata["dependency_count"] != 1 || selectedMetadata["has_missing_required_dependency"] != false || selectedMetadata["has_blocking_dependency"] != false {
		t.Fatalf("selected app metadata dependencies should be scoped and ready: %#v", selectedMetadata)
	}
	if selectedDependencies[0].ID != "selected-skill" {
		t.Fatalf("selected app metadata should only include selected dependency: %#v", selectedDependencies)
	}
	otherPayload, ok := payloadByAppID["other-app"]
	if !ok {
		t.Fatalf("missing other app payload: %#v", payloads)
	}
	otherMetadata, ok := otherPayload.Body["metadata"].(map[string]interface{})
	if !ok || otherMetadata["has_blocking_dependency"] != true {
		t.Fatalf("other app payload should retain its own blocking dependency: %#v", otherPayload.Body)
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

func assertMaclawAppApprovalReadbackSameInstanceForTest(t *testing.T, appView, globalView maclawAppApprovalInstance) {
	t.Helper()
	if appView.AppID != globalView.AppID || appView.ApprovalID != globalView.ApprovalID || appView.InstanceID != globalView.InstanceID || appView.WorkflowDecisionID != globalView.WorkflowDecisionID {
		t.Fatalf("single app and global approval centers should show the same workflow instance, app=%#v global=%#v", appView, globalView)
	}
	if appView.Status != globalView.Status || appView.Lane != globalView.Lane || appView.CurrentNode != globalView.CurrentNode || appView.ResultStatus != globalView.ResultStatus || appView.BusinessStatus != globalView.BusinessStatus {
		t.Fatalf("single app and global approval centers should preserve the same status/node fields, app=%#v global=%#v", appView, globalView)
	}
	if len(appView.Outputs) != len(globalView.Outputs) || len(appView.Artifacts) != len(globalView.Artifacts) {
		t.Fatalf("single app and global approval centers should preserve the same result package, app=%#v global=%#v", appView, globalView)
	}
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
func TestExecuteMaclawAppBusinessOperationRunsPreferredAction(t *testing.T) {
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
		if r.Header.Get("X-MaClaw-User-ID") != "operator" {
			t.Fatalf("expected user header, got %q", r.Header.Get("X-MaClaw-User-ID"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"committed","result_status":"success","record_id":"cust-1","record":{"id":"cust-1"}}`))
	}))
	defer server.Close()
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "operator", Role: "ops"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	result, err := app.ExecuteMaclawAppBusinessOperation(maclawAppBusinessOperationInput{
		AppID:           "customer-profile",
		AppName:         "Customer Profile",
		DatasetID:       "sales.customers",
		ObjectRole:      "customer",
		BlueprintID:     "sales.customer.v1",
		BusinessEntity:  "sales",
		BusinessAction:  "upsert",
		BusinessNote:    "new customer from card",
		PreferredAction: "sales.customer_upsert",
		Data:            map[string]any{"customer_name": "Acme"},
	})
	if err != nil {
		t.Fatalf("ExecuteMaclawAppBusinessOperation() error = %v", err)
	}
	if result["synced"] != true || result["mode"] != "business_action" || result["target"] != "sales.customer_upsert" || result["result_status"] != "success" {
		t.Fatalf("unexpected business operation result: %#v", result)
	}
	if result["primary_result"] != "business_record" || result["business_status"] != "success" {
		t.Fatalf("business action should expose standard result identity: %#v", result)
	}
	resultPayload, ok := result["result_payload"].(map[string]any)
	if !ok || resultPayload["app_id"] != "customer-profile" || resultPayload["dataset_id"] != "sales.customers" || resultPayload["object_role"] != "customer" || resultPayload["business_action"] != "upsert" || resultPayload["record_id"] != "cust-1" || resultPayload["result_status"] != "success" {
		t.Fatalf("business action should expose standard result payload: %#v", result["result_payload"])
	}
	outputs, ok := result["outputs"].([]map[string]any)
	if !ok || len(outputs) != 1 || outputs[0]["kind"] != "business_record" || outputs[0]["title"] != "sales.customer_upsert" || outputs[0]["status"] != "success" {
		t.Fatalf("business action should expose default output package: %#v", result["outputs"])
	}
	artifacts, ok := result["artifacts"].([]map[string]any)
	if !ok || len(artifacts) != 0 {
		t.Fatalf("business action should expose empty artifact package: %#v", result["artifacts"])
	}
	if len(captured) != 1 || captured[0].Method != http.MethodPost || captured[0].Path != "/api/v1/data/business-actions/sales.customer_upsert/execute" {
		t.Fatalf("unexpected request: %#v", captured)
	}
	data, ok := captured[0].Body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("request body missing data: %#v", captured[0].Body)
	}
	if captured[0].Body["dry_run"] != false || data["app_id"] != "customer-profile" || data["object_role"] != "customer" || data["preferred_action"] != "sales.customer_upsert" || data["customer_name"] != "Acme" {
		t.Fatalf("request body missing app business semantics: %#v", captured[0].Body)
	}
}

func TestExecuteMaclawAppBusinessOperationQueriesPreferredView(t *testing.T) {
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
		_, _ = w.Write([]byte(`{"status":"ok","primary_result":"records","business_status":"ready","result_status":"ready","result_payload":{"business_status":"ready","view_id":"sales.customer_directory","records":[{"id":"cust-1","dataset_id":"sales.customers"}],"record_count":1},"outputs":[{"kind":"table","title":"Customer directory","status":"ready","data":{"rows":[{"id":"cust-1"}]}}],"artifacts":[{"id":"cust-export","name":"customers.csv","uri":"artifact://customers.csv","status":"ready"}],"records":[{"id":"cust-1"}]}`))
	}))
	defer server.Close()
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "operator", Role: "ops"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	result, err := app.ExecuteMaclawAppBusinessOperation(maclawAppBusinessOperationInput{AppID: "customer-profile", PreferredView: "sales.customer_directory", BusinessNote: "Acme", Limit: 25})
	if err != nil {
		t.Fatalf("ExecuteMaclawAppBusinessOperation view error = %v", err)
	}
	if result["mode"] != "business_view" || result["target"] != "sales.customer_directory" || result["result_status"] != "ready" {
		t.Fatalf("unexpected view operation result: %#v", result)
	}
	if result["primary_result"] != "records" || result["business_status"] != "ready" {
		t.Fatalf("view operation should preserve upstream result identity: %#v", result)
	}
	resultPayload, ok := result["result_payload"].(map[string]any)
	if !ok || resultPayload["view_id"] != "sales.customer_directory" || resultPayload["record_count"] != float64(1) {
		t.Fatalf("view operation should preserve upstream result payload: %#v", result["result_payload"])
	}
	outputs, ok := result["outputs"].([]map[string]any)
	if !ok || len(outputs) != 1 || outputs[0]["kind"] != "table" || outputs[0]["title"] != "Customer directory" {
		t.Fatalf("view operation should preserve upstream outputs: %#v", result["outputs"])
	}
	artifacts, ok := result["artifacts"].([]map[string]any)
	if !ok || len(artifacts) != 1 || artifacts[0]["id"] != "cust-export" {
		t.Fatalf("view operation should preserve upstream artifacts: %#v", result["artifacts"])
	}
	if len(captured) != 1 || captured[0].Method != http.MethodPost || captured[0].Path != "/api/v1/data/views/sales.customer_directory/query" {
		t.Fatalf("unexpected view request: %#v", captured)
	}
	if captured[0].Body["q"] != "Acme" || captured[0].Body["limit"] != float64(25) {
		t.Fatalf("view query body missing q/limit: %#v", captured[0].Body)
	}
}

func TestExecuteMaclawAppBusinessOperationRunsPreferredReport(t *testing.T) {
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
		_, _ = w.Write([]byte(`{"status":"ready","primary_result":"report","business_status":"ready","result_status":"ready","result_payload":{"business_status":"ready","report_id":"procurement.purchase_by_status","rows":[{"id":"report-1"}],"row_count":1},"outputs":[{"kind":"report","title":"Purchase by status","status":"ready","data":{"rows":[{"id":"report-1"}]}}],"artifacts":[{"id":"report-pdf","name":"purchase-by-status.pdf","uri":"artifact://reports/purchase-by-status.pdf","status":"ready"}],"rows":[{"id":"report-1"}]}`))
	}))
	defer server.Close()
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "operator", Role: "ops"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	result, err := app.ExecuteMaclawAppBusinessOperation(maclawAppBusinessOperationInput{AppID: "purchase-report", PreferredReport: "procurement.purchase_by_status", Filter: map[string]any{"status": "open"}, Limit: 15})
	if err != nil {
		t.Fatalf("ExecuteMaclawAppBusinessOperation report error = %v", err)
	}
	if result["mode"] != "business_report" || result["target"] != "procurement.purchase_by_status" || result["result_status"] != "ready" {
		t.Fatalf("unexpected report operation result: %#v", result)
	}
	if result["primary_result"] != "report" || result["business_status"] != "ready" {
		t.Fatalf("report operation should preserve upstream result identity: %#v", result)
	}
	reportPayload, ok := result["result_payload"].(map[string]any)
	if !ok || reportPayload["report_id"] != "procurement.purchase_by_status" || reportPayload["row_count"] != float64(1) {
		t.Fatalf("report operation should preserve upstream result payload: %#v", result["result_payload"])
	}
	reportOutputs, ok := result["outputs"].([]map[string]any)
	if !ok || len(reportOutputs) != 1 || reportOutputs[0]["kind"] != "report" || reportOutputs[0]["title"] != "Purchase by status" {
		t.Fatalf("report operation should preserve upstream outputs: %#v", result["outputs"])
	}
	reportArtifacts, ok := result["artifacts"].([]map[string]any)
	if !ok || len(reportArtifacts) != 1 || reportArtifacts[0]["id"] != "report-pdf" {
		t.Fatalf("report operation should preserve upstream artifacts: %#v", result["artifacts"])
	}
	if len(captured) != 1 || captured[0].Method != http.MethodPost || captured[0].Path != "/api/v1/data/reports/procurement.purchase_by_status/run" {
		t.Fatalf("unexpected report request: %#v", captured)
	}
	filter, ok := captured[0].Body["filter"].(map[string]interface{})
	if !ok || filter["status"] != "open" || captured[0].Body["limit"] != float64(15) {
		t.Fatalf("report body missing filter/limit: %#v", captured[0].Body)
	}
}

func TestExecuteMaclawAppBusinessOperationRunsPreferredDashboard(t *testing.T) {
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
		_, _ = w.Write([]byte(`{"status":"ok","primary_result":"dashboard","business_status":"ready","result_status":"ready","result_payload":{"business_status":"ready","dashboard_id":"inventory.overview","cards":[{"id":"inventory"}],"card_count":1},"outputs":[{"kind":"dashboard","title":"Inventory overview","status":"ready","data":{"cards":[{"id":"inventory"}]}}],"artifacts":[{"id":"dashboard-shot","name":"inventory.png","uri":"artifact://dashboards/inventory.png","status":"ready"}],"cards":[{"id":"inventory"}]}`))
	}))
	defer server.Close()
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "operator", Role: "ops"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	result, err := app.ExecuteMaclawAppBusinessOperation(maclawAppBusinessOperationInput{AppID: "inventory-dashboard", PreferredDashboard: "inventory.overview"})
	if err != nil {
		t.Fatalf("ExecuteMaclawAppBusinessOperation dashboard error = %v", err)
	}
	if result["mode"] != "business_dashboard" || result["target"] != "inventory.overview" || result["result_status"] != "ready" {
		t.Fatalf("unexpected dashboard operation result: %#v", result)
	}
	if result["primary_result"] != "dashboard" || result["business_status"] != "ready" {
		t.Fatalf("dashboard operation should preserve upstream result identity: %#v", result)
	}
	dashboardPayload, ok := result["result_payload"].(map[string]any)
	if !ok || dashboardPayload["dashboard_id"] != "inventory.overview" || dashboardPayload["card_count"] != float64(1) {
		t.Fatalf("dashboard operation should preserve upstream result payload: %#v", result["result_payload"])
	}
	dashboardOutputs, ok := result["outputs"].([]map[string]any)
	if !ok || len(dashboardOutputs) != 1 || dashboardOutputs[0]["kind"] != "dashboard" || dashboardOutputs[0]["title"] != "Inventory overview" {
		t.Fatalf("dashboard operation should preserve upstream outputs: %#v", result["outputs"])
	}
	dashboardArtifacts, ok := result["artifacts"].([]map[string]any)
	if !ok || len(dashboardArtifacts) != 1 || dashboardArtifacts[0]["id"] != "dashboard-shot" {
		t.Fatalf("dashboard operation should preserve upstream artifacts: %#v", result["artifacts"])
	}
	if len(captured) != 1 || captured[0].Method != http.MethodPost || captured[0].Path != "/api/v1/data/dashboards/inventory.overview/run" {
		t.Fatalf("unexpected dashboard request: %#v", captured)
	}
	if captured[0].Body != nil {
		t.Fatalf("dashboard request should not send a body: %#v", captured[0].Body)
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

func TestPlanMaclawAppInstallRejectsUnknownSchema(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if _, err := app.PlanMaclawAppInstall(`{"schema":"unknown","privateMarker":"x_maclaw_apps"}`); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatalf("expected schema error, got %v", err)
	}
}

func TestInstallMaclawAppDependenciesPreservesInstallRefFromDuplicateDependency(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	type installCall struct {
		source     string
		id         string
		installRef string
	}
	var calls []installCall
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		calls = append(calls, installCall{source: source, id: id, installRef: installRef})
		skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", id)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# "+id+"\n"), 0o644); err != nil {
			return err
		}
		cfg, err := app.LoadConfig()
		if err != nil {
			return err
		}
		cfg.NLSkills = append(cfg.NLSkills, corelib.NLSkillEntry{Name: id, SkillDir: skillDir, Status: "active", Source: source, HubSkillID: id})
		return app.SaveConfig(cfg)
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "duplicate-install-ref-app",
			"name": "Duplicate Install Ref App",
			"kind": "enterprise_approval_app",
			"binding": {
				"appSkill": { "id": "expense-app-skill" }
			},
			"dependencies": {
				"skills": [
					{ "id": "expense-app-skill", "kind": "app_skill", "required": true, "source": "enterprise_hub", "capability_id": "cap-expense-app-skill" },
					{ "id": "expense-approval-workflow", "kind": "workflow_skill", "required": true, "source": "enterprise_hub", "capability_id": "cap-expense-workflow" }
				]
			}
		}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "expense-app-skill")
	if dep == nil {
		t.Fatalf("missing app skill dependency: %#v", plan.Dependencies)
	}
	if dep.Source != "enterprise_hub" || dep.InstallRef != "cap-expense-app-skill" || dep.Action != "installed" {
		t.Fatalf("duplicate dependency should preserve precise install ref, got %#v", dep)
	}
	if len(calls) != 2 {
		t.Fatalf("expected app skill and workflow installs, got %#v", calls)
	}
	if calls[0] != (installCall{source: "enterprise_hub", id: "expense-app-skill", installRef: "cap-expense-app-skill"}) {
		t.Fatalf("app skill install should use precise install ref, got %#v", calls)
	}
}

func TestSelectedMaclawAppPackageFiltersResolvedDependencies(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pkg := map[string]any{
		"schema":        "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": []any{
			map[string]any{
				"schema":        "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"app": map[string]any{
					"id":   "expense-app",
					"name": "Expense App",
					"kind": "enterprise_approval_app",
					"dependencies": map[string]any{"skills": []any{
						map[string]any{"id": "expense-workflow", "kind": "workflow_skill", "required": true, "source": "local"},
					}},
				},
			},
			map[string]any{
				"schema":        "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"app": map[string]any{
					"id":   "contract-app",
					"name": "Contract App",
					"kind": "enterprise_approval_app",
					"dependencies": map[string]any{"skills": []any{
						map[string]any{"id": "contract-workflow", "kind": "workflow_skill", "required": true, "source": "local"},
					}},
				},
			},
		},
		"resolved_dependencies": []any{
			map[string]any{"id": "expense-workflow", "install_ref": "hub-expense-workflow", "source": "hub", "required": true, "app_ids": []any{"expense-app"}},
			map[string]any{"id": "contract-workflow", "install_ref": "hub-contract-workflow", "source": "hub", "required": true, "app_ids": []any{"contract-app"}},
		},
	}

	for _, raw := range anySlice(pkg["apps"]) {
		if appEntry := anyMap(raw); appEntry != nil {
			appEntry["resolved_dependencies"] = pkg["resolved_dependencies"]
		}
	}

	selected, entries, err := maclawAppPackageForSelectedAppIDs(pkg, []string{"expense-app"})
	if err != nil {
		t.Fatalf("maclawAppPackageForSelectedAppIDs() error = %v", err)
	}
	if len(entries) != 1 || entries[0].ID != "expense-app" {
		t.Fatalf("expected only selected app entry, got %#v", entries)
	}
	resolved := anySlice(selected["resolved_dependencies"])
	if len(resolved) != 1 || stringMapValue(anyMap(resolved[0]), "id") != "expense-workflow" {
		t.Fatalf("selected package should keep only selected app resolved dependency: %#v", selected["resolved_dependencies"])
	}
	selectedApps := anySlice(selected["apps"])
	if len(selectedApps) != 1 {
		t.Fatalf("selected package should keep one app entry, got %#v", selectedApps)
	}
	entryResolved := anySlice(anyMap(selectedApps[0])["resolved_dependencies"])
	if len(entryResolved) != 1 || stringMapValue(anyMap(entryResolved[0]), "id") != "expense-workflow" {
		t.Fatalf("selected app entry should keep only selected resolved dependency: %#v", entryResolved)
	}
	if stringMapValue(anyMap(entryResolved[0]), "install_ref") != "hub-expense-workflow" {
		t.Fatalf("selected app entry should preserve selected install ref: %#v", entryResolved[0])
	}
	packageJSON, err := maclawAppStableJSON(selected)
	if err != nil {
		t.Fatalf("maclawAppStableJSON() error = %v", err)
	}
	plan, err := app.PlanMaclawAppInstall(packageJSON)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	if dep := maclawAppPlanDepForTest(plan, "expense-workflow"); dep == nil || dep.Source != "hub" || dep.InstallRef != "hub-expense-workflow" || len(dep.AppIDs) != 1 || dep.AppIDs[0] != "expense-app" {
		t.Fatalf("selected plan should use selected app install ref only: %#v", dep)
	}
	if dep := maclawAppPlanDepForTest(plan, "contract-workflow"); dep != nil {
		t.Fatalf("unselected app dependency should not appear in selected plan: %#v", dep)
	}
}

func TestMaclawAppSerializableResolvedDepsIncludesAppIDs(t *testing.T) {
	deps := []maclawAppInstallPlanDependency{{ID: "expense-workflow", Kind: "workflow_skill", Required: true, Source: "hub", InstallRef: "hub-expense-workflow", AppIDs: []string{"expense-app"}}}
	resolved := maclawAppSerializableResolvedDeps(deps)
	if len(resolved) != 1 {
		t.Fatalf("expected one resolved dependency, got %#v", resolved)
	}
	appIDs := maclawAppStringListFromAny(resolved[0]["app_ids"])
	if len(appIDs) != 1 || appIDs[0] != "expense-app" {
		t.Fatalf("resolved dependency should carry app_ids for selected installs: %#v", resolved[0])
	}
}
func maclawAppIntValueForTest(value any) int {
	if number, ok := maclawAppNumberFromAny(value); ok {
		return int(number)
	}
	return 0
}

func assertMaclawAppDependencyInstallTraceReadyForTest(t *testing.T, label string, trace map[string]any, wantCount, wantDownloadCount int) {
	t.Helper()
	if trace == nil {
		t.Fatalf("%s missing dependency install trace summary", label)
	}
	if trace["schema"] != "maclaw.app.dependency_install_trace.v1" ||
		maclawAppIntValueForTest(trace["dependency_count"]) != wantCount ||
		maclawAppIntValueForTest(trace["preflight_checked_count"]) != wantCount ||
		maclawAppIntValueForTest(trace["preflight_ready_count"]) != wantCount ||
		maclawAppIntValueForTest(trace["preflight_failed_count"]) != 0 ||
		maclawAppIntValueForTest(trace["integrity_checked_count"]) != wantCount ||
		maclawAppIntValueForTest(trace["integrity_ready_count"]) != wantCount ||
		maclawAppIntValueForTest(trace["integrity_failed_count"]) != 0 ||
		maclawAppIntValueForTest(trace["download_available_count"]) != wantDownloadCount ||
		maclawAppIntValueForTest(trace["signature_available_count"]) != wantCount ||
		maclawAppIntValueForTest(trace["install_error_count"]) != 0 ||
		trace["ok"] != true {
		t.Fatalf("%s should expose ready dependency install trace summary: %#v", label, trace)
	}
}

func maclawAppPlanDepForTest(plan maclawAppInstallPlan, id string) *maclawAppInstallPlanDependency {
	for i := range plan.Dependencies {
		if plan.Dependencies[i].ID == id {
			return &plan.Dependencies[i]
		}
	}
	return nil
}
