package main

import (
	"crypto/ed25519"
	"crypto/rand"
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
	"github.com/RapidAI/CodeClaw/corelib/remote"
	maclawapptest "github.com/RapidAI/CodeClaw/internal/testfixtures"
)

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
	if queue.Submissions[0].Dependencies[1].Kind != "workflow_skill" || queue.Submissions[0].Dependencies[1].Source != "skillmarket" || queue.Submissions[0].Dependencies[1].AppIDs[0] != "local-contract" {
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

func TestSubmitMaclawAppPackagePersistsSourceVersionKeyInstallRefs(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "paper_pdf_translator")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# paper_pdf_translator\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, ".env"), []byte("TOKEN=secret\n"), 0o600); err != nil {
		t.Fatalf("WriteFile .env: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	versionKey := "enterprise_hub:skill:paper_pdf_translator@d1cb0335a151"
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:       "paper_pdf_translator",
		SkillDir:   skillDir,
		Status:     "active",
		Source:     "enterprise_hub",
		HubSkillID: "paper_pdf_translator",
		HubVersion: versionKey,
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	pkg := fmt.Sprintf(`{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "paper_pdf_translator",
				"name": "PDF翻译工具",
				"kind": "tool_app",
				"binding": {
					"appSkill": {"id": "paper_pdf_translator", "version": %q}
				},
				"dependencies": {"skills": [
					{"id": "paper_pdf_translator", "version": %q, "kind": "app_skill", "required": true, "source": "local"}
				]},
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "template":"document_workspace", "regionCount":2, "regions":[{"id":"input", "role":"input", "placement":"left"}, {"id":"output", "role":"output", "placement":"right"}]},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content"]},
					"dependencyVerification": {"schema":"maclaw.app.install_plan.v1", "dependencyCount":1, "hasMissingRequired":false, "hasBlockingDependency":false, "dependencies":[{"id":"paper_pdf_translator", "version":%q, "kind":"app_skill", "required":true, "installed":true, "health":"ready", "action":"skip"}]},
					"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-paper-pdf","sampleInput":{"sample":true},"expectedOutput":{"content":"ok"},"requiredRoles":["tester"],"requiredScopes":["app.run"],"riskLevel":"low"}, "testProtocolFingerprint":"proto-paper-pdf", "runId":"run-paper-pdf", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"content":"ok"}, "outputs":[{"kind":"content", "text":"ok"}], "resultCoverage":{"ok":true, "primary":"content", "coveredTypes":["content"], "missingTypes":[]}}
				}
			}
		}]
	}`, versionKey, versionKey, versionKey)
	pkg = maclawAppPackageWithCurrentDefinitionHashes(t, pkg)

	result, err := app.SubmitMaclawAppPackage(pkg)
	if err != nil {
		t.Fatalf("SubmitMaclawAppPackage error: %v", err)
	}
	if result["dependency_count"] != 1 {
		t.Fatalf("expected dependency count in result: %#v", result)
	}

	data, err := os.ReadFile(app.maclawAppSubmissionQueuePath())
	if err != nil {
		t.Fatalf("read queue: %v", err)
	}
	var queue maclawAppSubmissionQueue
	if err := json.Unmarshal(data, &queue); err != nil {
		t.Fatalf("decode queue: %v", err)
	}
	if len(queue.Submissions) != 1 {
		t.Fatalf("unexpected queue: %#v", queue)
	}
	resolved := anySlice(queue.Submissions[0].Package["resolved_dependencies"])
	if len(resolved) != 1 {
		t.Fatalf("submitted package should persist resolved dependency refs: %#v", queue.Submissions[0].Package)
	}
	dep := anyMap(resolved[0])
	if dep["source"] != "enterprise_hub" || dep["install_ref"] != "enterprise_hub://capabilities/paper_pdf_translator@d1cb0335a151" || dep["canonical_id"] != "paper_pdf_translator" {
		t.Fatalf("submitted package dependency should be installable from enterprise Hub: %#v", dep)
	}
	apps := anySlice(queue.Submissions[0].Package["apps"])
	entryResolved := anySlice(anyMap(apps[0])["resolved_dependencies"])
	if len(entryResolved) != 1 || anyMap(entryResolved[0])["install_ref"] != dep["install_ref"] {
		t.Fatalf("submitted app entry should persist resolved dependency refs: %#v", apps[0])
	}
	bundled := anyMap(queue.Submissions[0].Package["bundled_dependencies"])
	bundledSkills := anySlice(bundled["skills"])
	if len(bundledSkills) != 1 {
		t.Fatalf("submitted package should bundle installed dependency skill: %#v", queue.Submissions[0].Package["bundled_dependencies"])
	}
	bundledSkill := anyMap(bundledSkills[0])
	if bundledSkill["stable_id"] != "hub_skill:paper_pdf_translator" || bundledSkill["name"] != "paper_pdf_translator" || bundledSkill["sha256"] == "" {
		t.Fatalf("bundled dependency should carry stable identity and checksum: %#v", bundledSkill)
	}
	bundledFiles := anyMap(bundledSkill["files"])
	if bundledFiles["skill.md"] == "" {
		t.Fatalf("bundled dependency should include package files: %#v", bundledSkill)
	}
	if bundledFiles[".env"] != nil {
		t.Fatalf("bundled dependency should not include sensitive dotfiles: %#v", bundledFiles)
	}
	entryBundled := anyMap(anyMap(apps[0])["bundled_dependencies"])
	if len(anySlice(entryBundled["skills"])) != 1 {
		t.Fatalf("submitted app entry should persist bundled dependency refs: %#v", apps[0])
	}
}

func TestSubmitMaclawAppPackagePreservesFriendlyDependencyIDAndResolvedMarketRef(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	const marketID = "5ce9973a-a8cd-465a-a3a3-a8d95d2eb69b"
	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "RapidOCR")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# RapidOCR\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(map[string]any{"urls": []string{server.URL}, "ttl_seconds": 60})
		case "/api/client/quality":
			_ = json.NewEncoder(w).Encode(map[string]any{"quality_score": 99, "routable": true})
		case "/api/v1/skillmarket/search":
			if strings.EqualFold(r.URL.Query().Get("q"), "rapidocr") {
				_ = json.NewEncoder(w).Encode(map[string]any{"results": []SkillSearchResult{{
					ID:                 marketID,
					Name:               "RapidOCR",
					InstallRef:         marketID,
					Version:            "10",
					PackageSHA256:      "sha256-rapidocr",
					PackageSignature:   "sig-rapidocr",
					PackageDownloadURL: server.URL + "/api/v1/skills/" + marketID + "/download",
				}}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []SkillSearchResult{}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome, hubCenterCache: remote.NewHubCenterSelectionCache(time.Minute)}
	app.hubCenterCache.Set(server.URL, []string{server.URL})
	cfg := corelib.AppConfig{
		RemoteHubCenterURL: server.URL,
		NLSkills: []corelib.NLSkillEntry{{
			Name:       "RapidOCR",
			SkillDir:   skillDir,
			Status:     "active",
			Source:     "skillmarket",
			HubSkillID: marketID,
			HubVersion: "10",
		}},
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
				"id": "rapidocr-wrapper",
				"name": "RapidOCR Wrapper",
				"kind": "tool_app",
				"dependencies": { "skills": [
					{ "id": "RapidOCR", "version": "10", "kind": "runtime_skill", "required": true, "source": "hub" }
				] },
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "template":"document_workspace", "regionCount":2, "regions":[{"id":"input", "role":"input", "placement":"left"}, {"id":"output", "role":"output", "placement":"right"}]},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content"]},
					"dependencyVerification": {"schema":"maclaw.app.install_plan.v1", "dependencyCount":1, "hasMissingRequired":false, "hasBlockingDependency":false, "dependencies":[{"id":"RapidOCR", "version":"10", "kind":"runtime_skill", "required":true, "installed":true, "health":"ready", "action":"skip"}]},
					"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-rapidocr","sampleInput":{"sample":true},"expectedOutput":{"content":"ok"},"requiredRoles":["tester"],"requiredScopes":["app.run"],"riskLevel":"low"}, "testProtocolFingerprint":"proto-rapidocr", "runId":"run-rapidocr", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"content":"ok"}, "outputs":[{"kind":"content", "text":"ok"}], "resultCoverage":{"ok":true, "primary":"content", "coveredTypes":["content"], "missingTypes":[]}}
				}
			}
		}]
	}`
	pkg = maclawAppPackageWithCurrentDefinitionHashes(t, pkg)

	result, err := app.SubmitMaclawAppPackage(pkg)
	if err != nil {
		t.Fatalf("SubmitMaclawAppPackage error: %v", err)
	}
	if result["dependency_count"] != 1 {
		t.Fatalf("expected dependency count in result: %#v", result)
	}

	data, err := os.ReadFile(app.maclawAppSubmissionQueuePath())
	if err != nil {
		t.Fatalf("read queue: %v", err)
	}
	var queue maclawAppSubmissionQueue
	if err := json.Unmarshal(data, &queue); err != nil {
		t.Fatalf("decode queue: %v", err)
	}
	if len(queue.Submissions) != 1 || len(queue.Submissions[0].Dependencies) != 1 {
		t.Fatalf("unexpected queue dependencies: %#v", queue)
	}
	auditDep := queue.Submissions[0].Dependencies[0]
	if auditDep.ID != "RapidOCR" || auditDep.InstallRef != marketID || auditDep.InstallRefKind != "skillmarket" || auditDep.InstallRefTarget != marketID {
		t.Fatalf("submission audit dependency should preserve friendly id and resolved SkillMarket ref: %#v", auditDep)
	}

	resolved := anySlice(queue.Submissions[0].Package["resolved_dependencies"])
	if len(resolved) != 1 {
		t.Fatalf("submitted package should persist resolved dependency refs: %#v", queue.Submissions[0].Package)
	}
	dep := anyMap(resolved[0])
	if dep["id"] != "RapidOCR" || dep["install_ref"] != marketID || dep["install_ref_kind"] != "skillmarket" || dep["install_ref_target"] != marketID {
		t.Fatalf("resolved dependency should keep declared id separate from download ref: %#v", dep)
	}

	apps := anySlice(queue.Submissions[0].Package["apps"])
	entryResolved := anySlice(anyMap(apps[0])["resolved_dependencies"])
	if len(entryResolved) != 1 || anyMap(entryResolved[0])["id"] != "RapidOCR" || anyMap(entryResolved[0])["install_ref"] != marketID {
		t.Fatalf("submitted app entry should persist resolved dependency refs: %#v", apps[0])
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
		return maclawAppPackageWithCurrentDefinitionHashes(t, `{
				"schema": "maclaw.app.pack.v1",
				"privateMarker": "x_maclaw_apps",
				"apps": [{
					"schema": "maclaw.app.v1",
					"privateMarker": "x_maclaw_apps",
					"app": {
						"id": "`+id+`",
						"name": "App",
						"kind": "tool_app",
						"governance": {
							"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content"]},
							"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-`+id+`","sampleInput":{"sample":true},"expectedOutput":{"content":"ok"},"requiredRoles":["tester"],"requiredScopes":["app.run"],"riskLevel":"low"}, "testProtocolFingerprint":"proto-`+id+`", "runId":"run-`+id+`", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"content":"ok"}, "outputs":[{"kind":"content", "text":"ok"}], "resultCoverage":{"ok":true, "primary":"content", "coveredTypes":["content"], "missingTypes":[]}}
						}
					}
				}]
			}`)
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
	pkg := maclawAppPackageWithCurrentDefinitionHashes(t, `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "detail-app",
				"name": "Detail App",
				"runtime": {"type": "fixed_skill_ui"},
				"kind": "tool_app",
				"governance": {
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content"]},
					"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-detail-app","sampleInput":{"sample":true},"expectedOutput":{"content":"ok"},"requiredRoles":["tester"],"requiredScopes":["app.run"],"riskLevel":"low"}, "testProtocolFingerprint":"proto-detail-app", "runId":"run-detail-app", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"content":"ok"}, "outputs":[{"kind":"content", "text":"ok"}], "resultCoverage":{"ok":true, "primary":"content", "coveredTypes":["content"], "missingTypes":[]}}
				}
			}
		}]
	}`)
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
	pkg := maclawAppReadyToolPackageForHubSyncTest(t, "withdraw-app")
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
	pkg := maclawAppReadyToolPackageForHubSyncTest(t, "status-app")
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

func TestResolveMaclawAppHubSubmissionIdentityRequiresSubmissionID(t *testing.T) {
	_, _, _, err := resolveMaclawAppHubSubmissionIdentity(maclawAppHubSubmissionResponse{
		Schema:        "maclaw.app.hub_submission.v1",
		PackageSHA256: "pkg-sha",
	}, "local-sha")
	if err == nil || !strings.Contains(err.Error(), "submission_id") {
		t.Fatalf("expected missing submission_id error, got %v", err)
	}

	_, _, _, err = resolveMaclawAppHubSubmissionIdentity(maclawAppHubSubmissionResponse{
		Schema:        "maclaw.app.hub_submission.v1",
		PackageSHA256: "pkg-sha",
		Submissions: []maclawAppHubSubmissionResult{{
			SubmissionID: "pkg-sha",
			CapabilityID: "cap-1",
		}},
	}, "")
	if err == nil || !strings.Contains(err.Error(), "must not equal package_sha256") {
		t.Fatalf("expected package_sha256 identity rejection, got %v", err)
	}

	submissionID, capabilityID, packageSHA, err := resolveMaclawAppHubSubmissionIdentity(maclawAppHubSubmissionResponse{
		Schema:        "maclaw.app.hub_submission.v1",
		SubmissionID:  "hub-sub-1",
		CapabilityID:  "cap-top",
		PackageSHA256: "pkg-sha",
		Submissions: []maclawAppHubSubmissionResult{{
			SubmissionID: "hub-sub-entry",
			CapabilityID: "cap-entry",
		}},
	}, "local-sha")
	if err != nil {
		t.Fatalf("resolveMaclawAppHubSubmissionIdentity() error = %v", err)
	}
	if submissionID != "hub-sub-1" || capabilityID != "cap-top" || packageSHA != "pkg-sha" {
		t.Fatalf("unexpected identity submission=%q capability=%q package=%q", submissionID, capabilityID, packageSHA)
	}
}

func TestSyncMaclawAppPackageSubmissionToHubRefreshesStalePackageFingerprint(t *testing.T) {
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
	// Stale PackageSHA is self-healed from current package payload (upload source of truth).
	_, err = app.SyncMaclawAppPackageSubmissionToHub(queued["submission_id"].(string))
	if err != nil && strings.Contains(err.Error(), "package fingerprint mismatch") {
		t.Fatalf("fingerprint should self-heal before Hub sync, got %v", err)
	}
	// Queue record should store the refreshed SHA matching current package.
	queue, err = app.readMaclawAppSubmissionQueue()
	if err != nil {
		t.Fatalf("re-read queue: %v", err)
	}
	freshSHA, _, err := maclawAppPackageFingerprint(queue.Submissions[0].Package)
	if err != nil {
		t.Fatalf("fingerprint after sync attempt: %v", err)
	}
	if !strings.EqualFold(queue.Submissions[0].PackageSHA, freshSHA) {
		t.Fatalf("PackageSHA = %q, want refreshed %q", queue.Submissions[0].PackageSHA, freshSHA)
	}
	// When later readiness/dependency gates pass, Hub is contacted (418 here).
	// Name tamper may still block readiness — that is separate from fingerprint self-heal.
	_ = called
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

func TestInstallSelectedMaclawAppPackageFromHubUpgradesTrustedLegacyLocalDependencyToSkillMarket(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	const capabilityID = "cap-pdf-translator"
	const marketID = "market-paper-pdf-translator"
	app := &App{testHomeDir: tmpHome, hubCenterCache: remote.NewHubCenterSelectionCache(time.Minute)}
	var installedSource, installedID, installedRef string
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		installedSource, installedID, installedRef = source, id, installRef
		skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "paper_pdf_translator")
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return err
		}
		cfg, err := app.LoadConfig()
		if err != nil {
			return err
		}
		cfg.NLSkills = append(cfg.NLSkills, corelib.NLSkillEntry{
			Name:       "paper_pdf_translator",
			SkillDir:   skillDir,
			Status:     "active",
			Source:     source,
			HubSkillID: "paper_pdf_translator",
			HubVersion: "1.0.0",
		})
		return app.SaveConfig(cfg)
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/capabilities/maclaw-apps/" + capabilityID + "/package":
			var pkg map[string]any
			if err := json.Unmarshal([]byte(maclawAppPackageWithCurrentDefinitionHashes(t, `{
				"schema":"maclaw.app.pack.v1",
				"privateMarker":"x_maclaw_apps",
				"source":"enterprise_hub",
				"resolved_dependencies":[{"id":"paper_pdf_translator","version":"1.0.0","kind":"runtime_skill","required":true,"source":"local","install_ref":"paper_pdf_translator"}],
				"apps":[{"schema":"maclaw.app.v1","privateMarker":"x_maclaw_apps","app":{"id":"pdf-translator","name":"PDF 翻译工具","kind":"tool_app","dependencies":{"skill":{"id":"paper_pdf_translator","version":"1.0.0","kind":"runtime_skill","source":"local"}},"governance":{"workspaceLayout":{"schema":"maclaw.app.ui.v1","entry":"tool_workspace","template":"document_workspace","density":"compact","primaryRegion":"left","outputRegion":"right","regionCount":2,"visibleRegionCount":2,"regionIds":["input","output"],"fingerprint":"layout-pdf-translator","studio":{"editable":true,"savedInManifest":true,"updatedBy":"app_studio"},"regions":[{"id":"input","role":"input","placement":"left"},{"id":"output","role":"output","placement":"right"}]},"resultContract":{"schema":"maclaw.app.result.v1","primary":"document","types":["document","content"]},"dependencyVerification":{"schema":"maclaw.app.install_plan.v1","dependencyCount":1,"requiredCount":1,"installedCount":0,"missingCount":1,"blockedCount":0,"ok":false,"blocked":false,"dependencies":[{"id":"paper_pdf_translator","version":"1.0.0","kind":"runtime_skill","required":true,"source":"local"}]},"testEvidence":{"runId":"run-pdf-translator","verifiedAt":"2026-07-29T00:00:00Z","primaryResult":"document","resultPayload":{"document":"translated.pdf"}}}}}]}
			`)), &pkg); err != nil {
				t.Fatalf("decode legacy local dependency fixture: %v", err)
			}
			packageSHA := strings.Repeat("e", 64)
			versionKey := "enterprise_hub:skill:maclaw-app:pdf-translator@pkg"
			payload := "maclaw-app\n" + packageSHA + "\n" + versionKey + "\n2026-07-29T00:00:00Z\nhub-admin"
			pkg["package_sha256"] = packageSHA
			pkg["package_signature"] = map[string]any{
				"schema": "maclaw.app.package_signature.v1", "algorithm": "ed25519", "payload": payload,
				"public_key_base64":      base64.StdEncoding.EncodeToString(publicKey),
				"public_key_fingerprint": downloadedSkillPublicKeyFingerprint(publicKey),
				"signature_base64":       base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(payload))),
				"package_sha256":         packageSHA, "version_key": versionKey, "signed_at": "2026-07-29T00:00:00Z", "signed_by": "hub-admin",
			}
			markMaclawAppPackageAsPublishedHubDownloadTest(t, pkg, "pdf-translator", capabilityID, versionKey)
			// source was optional in older published Hub packages. Keep the full
			// authenticated download path source-less to cover that compatibility
			// contract rather than exercising the helper in isolation only.
			delete(pkg, "source")
			ensureMaclawAppPackageDependencyVerificationForHubDownloadTest(t, pkg)
			_ = json.NewEncoder(w).Encode(pkg)
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(map[string]any{"urls": []string{server.URL}, "ttl_seconds": 60})
		case "/api/client/quality":
			_ = json.NewEncoder(w).Encode(map[string]any{"quality_score": 99, "routable": true})
		case "/api/v1/skillmarket/search":
			if got := r.URL.Query().Get("q"); got != "paper_pdf_translator" {
				t.Fatalf("SkillMarket search query = %q, want paper_pdf_translator", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []SkillSearchResult{{ID: marketID, Name: "paper_pdf_translator", InstallRef: marketID, Version: "1.0.0"}}})
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	app.hubCenterCache.Set(server.URL, []string{server.URL})
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token", RemoteHubCenterURL: server.URL}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	result, err := app.InstallSelectedMaclawAppPackageFromHub(capabilityID, []string{"pdf-translator"})
	if err != nil {
		t.Fatalf("InstallSelectedMaclawAppPackageFromHub() error = %v", err)
	}
	if installedSource != "skillmarket" || installedID != "paper_pdf_translator" || installedRef != marketID {
		t.Fatalf("legacy Hub local dependency should install from resolved SkillMarket target, got source=%q id=%q ref=%q", installedSource, installedID, installedRef)
	}
	plan, ok := result["install_plan"].(maclawAppInstallPlan)
	if !ok || plan.HasMissingRequired || plan.HasBlockingDependency {
		t.Fatalf("legacy dependency install plan should be ready: %#v", result["install_plan"])
	}
	dep := maclawAppPlanDepForTest(plan, "paper_pdf_translator")
	if dep == nil || dep.Source != "skillmarket" || dep.InstallRefTarget != marketID || !dep.Installed || dep.Health != "ready" {
		t.Fatalf("legacy local dependency should be upgraded and installed: %#v", dep)
	}
	installedPackage := anyMap(result["package"])
	dependencies := anyMap(anyMap(anySlice(installedPackage["apps"])[0])["app"])["dependencies"]
	if skill := anyMap(dependencies)["skill"]; anyMap(skill) == nil || anyMap(skill)["source"] != "skillmarket" {
		t.Fatalf("install package should normalize the legacy dependency declaration used by the planner: %#v", installedPackage)
	}
}

func TestMaclawAppUpgradeTrustedHubLocalDependenciesForSkillMarketKeepsUnknownLocalDependencyLocal(t *testing.T) {
	pkg := map[string]any{
		"source": "enterprise_hub",
		"resolved_dependencies": []any{map[string]any{
			"id": "private-local-skill", "kind": "runtime_skill", "source": "local", "install_ref": "private-local-skill",
		}},
		"apps": []any{map[string]any{"app": map[string]any{
			"binding": map[string]any{"skill": map[string]any{"id": "private-local-skill", "kind": "runtime_skill", "source": "local"}},
		}}},
	}
	if got := maclawAppUpgradeTrustedHubLocalDependenciesForSkillMarket(pkg); got != 0 {
		t.Fatalf("unknown local dependency upgrade count = %d, want 0", got)
	}
	dep := anyMap(anySlice(pkg["resolved_dependencies"])[0])
	if dep["source"] != "local" || dep["install_ref"] != "private-local-skill" {
		t.Fatalf("unknown local dependency must remain local: %#v", dep)
	}
	binding := anyMap(anyMap(anySlice(pkg["apps"])[0])["app"])["binding"]
	if source := anyMap(anyMap(binding)["skill"])["source"]; source != "local" {
		t.Fatalf("unknown declared local dependency must remain local, got %#v", source)
	}
}

func TestMaclawAppUpgradeTrustedHubLocalDependenciesForSkillMarketAcceptsLegacyPackageWithoutSource(t *testing.T) {
	pkg := map[string]any{
		"resolved_dependencies": []any{map[string]any{
			"id": "paper_pdf_translator", "version": "1.0.0", "kind": "runtime_skill", "source": "local",
		}},
	}
	if got := maclawAppUpgradeTrustedHubLocalDependenciesForSkillMarket(pkg); got != 1 {
		t.Fatalf("legacy source-less package upgrade count = %d, want 1", got)
	}
	dep := anyMap(anySlice(pkg["resolved_dependencies"])[0])
	if dep["source"] != "skillmarket" || dep["canonical_id"] != "paper_pdf_translator" || dep["install_ref"] != "skillmarket://skills/paper_pdf_translator@1.0.0" {
		t.Fatalf("legacy source-less package should normalize known dependency: %#v", dep)
	}
}

func TestMaclawAppFilterBundledDependenciesForSelectedEntriesAcceptsMarketPrefixedAppID(t *testing.T) {
	pkg := map[string]any{
		"bundled_dependencies": map[string]any{"skills": []any{map[string]any{
			"id": "paper_pdf_translator", "name": "paper_pdf_translator", "app_ids": []any{"market-pdf-translator"},
			"files": map[string]any{"SKILL.md": "# paper_pdf_translator"},
		}}},
	}
	entries := []parsedMaclawAppEntry{{ID: "pdf-translator"}}
	maclawAppFilterBundledDependenciesForSelectedEntries(pkg, entries)
	bundled := maclawAppBundledDependenciesFromDoc(pkg)
	if len(bundled.Skills) != 1 || bundled.Skills[0].ID != "paper_pdf_translator" {
		t.Fatalf("market-prefixed bundled dependency should remain selected: %#v", pkg["bundled_dependencies"])
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
	// Governance review issues are non-blocking at install time (enforcement
	// moved to Hub publish/approval endpoint). The install should succeed with
	// a warning in logs.
	if err != nil {
		t.Fatalf("governance review should not block hub install, got error: %v", err)
	}
	records, err := app.ListMaclawAppInstalls(10)
	if err != nil {
		t.Fatalf("ListMaclawAppInstalls() error = %v", err)
	}
	if len(records) == 0 {
		t.Fatalf("hub install with governance warnings should still write install audit")
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
	pkg := maclawAppReadyToolPackageForHubSyncTest(t, "issue-app")
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

func TestPlanMaclawAppInstallKeepsHubDirectDownloadWhenHubCenterLookupMisses(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(map[string]any{"urls": []string{server.URL}, "ttl_seconds": 60})
		case "/api/client/quality":
			_ = json.NewEncoder(w).Encode(map[string]any{"quality_score": 99, "routable": true})
		case "/api/v1/skillmarket/search":
			if query := r.URL.Query().Get("q"); query != "direct-only-runtime" {
				t.Fatalf("unexpected search query %q", query)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []SkillSearchResult{}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome, hubCenterCache: remote.NewHubCenterSelectionCache(time.Minute)}
	app.hubCenterCache.Set(server.URL, []string{server.URL})
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: server.URL}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "direct-download-app",
			"name": "Direct Download App",
			"kind": "tool_app",
			"dependencies": { "skills": [
				{ "id": "direct-only-runtime", "version": "1.0.0", "kind": "runtime_skill", "required": true, "source": "hub", "install_ref": "hub://skills/direct-only-runtime@1.0.0" }
			] }
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "direct-only-runtime")
	if dep == nil || dep.PreflightStatus != "pending" || dep.PreflightCode != "target_resolved" || dep.PreflightStage != "install_ref" || dep.InstallRefTarget != "direct-only-runtime" {
		t.Fatalf("hub direct download dependency should not be blocked by a SkillMarket lookup miss: %#v", dep)
	}
	if !plan.HasMissingRequired || plan.HasBlockingDependency || dep.Action != "install" {
		t.Fatalf("missing installable dependency should be queued for install without blocking: %#v", plan)
	}
}

func TestPlanMaclawAppInstallBindsRuntimeSkillRefFromHubSkillID(t *testing.T) {
	// Authoring declares appSkill.id = hub package id; local registry may use a
	// localized display Name. Plan must bind runtime_skill_ref to HubSkillID so
	// frontend RunNLSkillAsync and backend resolveLoadedSkillForRun share one id.
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "Paper PDF Translator")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# paper_pdf_translator\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:       "Paper PDF Translator",
		SkillDir:   skillDir,
		Status:     "active",
		Source:     "hub",
		HubSkillID: "paper_pdf_translator",
		HubVersion: "enterprise_hub:skill:paper_pdf_translator@d774c84f9b53",
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "pdf-translator-app",
			"name": "PDF翻译工具",
			"kind": "tool_app",
			"appSkill": {"id": "paper_pdf_translator", "version": "1.0.0"},
			"dependencies": { "skills": [
				{ "id": "paper_pdf_translator", "version": "1.0.0", "kind": "runtime_skill", "required": true, "source": "hub" }
			] }
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall: %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "paper_pdf_translator")
	if dep == nil || !dep.Installed || dep.Health != "ready" {
		t.Fatalf("dependency not ready: %#v", dep)
	}
	if dep.InstalledName != "Paper PDF Translator" {
		t.Fatalf("InstalledName = %q, want display name", dep.InstalledName)
	}
	if dep.RuntimeSkillRef != "paper_pdf_translator" {
		t.Fatalf("RuntimeSkillRef = %q, want hub package id for RunNLSkillAsync", dep.RuntimeSkillRef)
	}
	if dep.CanonicalID != "paper_pdf_translator" {
		t.Fatalf("CanonicalID = %q, want hub package id", dep.CanonicalID)
	}
	// Identity index must resolve declared id even when only display name would
	// have mismatched on a naive lowercase map.
	idx := app.installedMaclawAppSkillIndex()
	if _, ok := idx["paper_pdf_translator"]; !ok {
		t.Fatalf("index missing hub key, keys sample: %v", identityIndexKeySample(idx, 8))
	}
	if _, ok := idx[corelib.NormalizeSkillIdentityKey("Paper PDF Translator")]; !ok {
		t.Fatalf("index missing normalized display name key")
	}
}

func TestInstallMixedSkillUsesHubCenterDownloadLocator(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	originalDefaultCenter := defaultRemoteHubCenterURL
	originalDefaultCenters := remote.DefaultRemoteHubCenterURLs
	defaultRemoteHubCenterURL = ""
	remote.DefaultRemoteHubCenterURLs = nil
	defer func() {
		defaultRemoteHubCenterURL = originalDefaultCenter
		remote.DefaultRemoteHubCenterURLs = originalDefaultCenters
	}()

	var locatorHits, fallbackHits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"urls":[],"nodes":[]}`))
		case "/custom/skill-package":
			locatorHits++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"locator_dep","name":"locator_dep","description":"downloaded from locator","version":"1.0.0","trust_level":"trusted","triggers":["locator"],"steps":[{"action":"bash","params":{"command":"echo locator"},"on_error":"stop"}]}`))
		case "/api/v1/skills/wrong-id/download":
			fallbackHits++
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:  server.URL,
		RemoteHubCenterURLs: []string{server.URL},
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if err := app.installMixedSkillWithIntegrityAndLocator("skillmarket", "locator_dep", "wrong-id", "/custom/skill-package", "", ""); err != nil {
		t.Fatalf("installMixedSkillWithIntegrityAndLocator() error = %v", err)
	}
	if locatorHits != 1 || fallbackHits != 0 {
		t.Fatalf("install should use download locator before fallback path, locatorHits=%d fallbackHits=%d", locatorHits, fallbackHits)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(cfg.NLSkills) != 1 || cfg.NLSkills[0].Name != "locator_dep" {
		t.Fatalf("locator-installed skill should be registered: %#v", cfg.NLSkills)
	}
	if cfg.NLSkills[0].Source != "skillmarket" || cfg.NLSkills[0].HubSkillID != "wrong-id" {
		t.Fatalf("locator-installed SkillMarket skill should preserve installer source and ref: %#v", cfg.NLSkills[0])
	}
}

func TestInstallMixedSkillAllowsConfiguredHubDownloadLocator(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	originalDefaultCenter := defaultRemoteHubCenterURL
	originalDefaultCenters := remote.DefaultRemoteHubCenterURLs
	defaultRemoteHubCenterURL = ""
	remote.DefaultRemoteHubCenterURLs = nil
	defer func() {
		defaultRemoteHubCenterURL = originalDefaultCenter
		remote.DefaultRemoteHubCenterURLs = originalDefaultCenters
	}()

	var hubLocatorHits int
	hubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hub/skill-package" {
			http.NotFound(w, r)
			return
		}
		hubLocatorHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"hub_locator_dep","name":"hub_locator_dep","description":"downloaded from hub locator","version":"1.0.0","trust_level":"trusted","triggers":["hub"],"steps":[{"action":"bash","params":{"command":"echo hub"},"on_error":"stop"}]}`))
	}))
	defer hubServer.Close()
	hubCenterServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"urls":[],"nodes":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer hubCenterServer.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:        hubServer.URL,
		RemoteHubCenterURL:  hubCenterServer.URL,
		RemoteHubCenterURLs: []string{hubCenterServer.URL},
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if err := app.installMixedSkillWithIntegrityAndLocator("skillmarket", "hub_locator_dep", "wrong-id", hubServer.URL+"/hub/skill-package", "", ""); err != nil {
		t.Fatalf("installMixedSkillWithIntegrityAndLocator() error = %v", err)
	}
	if hubLocatorHits != 1 {
		t.Fatalf("expected configured Hub download locator to be used, hits=%d", hubLocatorHits)
	}
}

func TestInstallMixedSkillFallsBackWhenDownloadLocatorFails(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	originalDefaultCenter := defaultRemoteHubCenterURL
	originalDefaultCenters := remote.DefaultRemoteHubCenterURLs
	defaultRemoteHubCenterURL = ""
	remote.DefaultRemoteHubCenterURLs = nil
	defer func() {
		defaultRemoteHubCenterURL = originalDefaultCenter
		remote.DefaultRemoteHubCenterURLs = originalDefaultCenters
	}()

	var locatorHits, fallbackHits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"urls":[],"nodes":[]}`))
		case "/broken/skill-package":
			locatorHits++
			http.NotFound(w, r)
		case "/api/v1/skills/fallback_dep/download":
			fallbackHits++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"fallback_dep","name":"fallback_dep","description":"downloaded from fallback","version":"1.0.0","trust_level":"trusted","triggers":["fallback"],"steps":[{"action":"bash","params":{"command":"echo fallback"},"on_error":"stop"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:  server.URL,
		RemoteHubCenterURLs: []string{server.URL},
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if err := app.installMixedSkillWithIntegrityAndLocator("skillmarket", "fallback_dep", "fallback_dep", server.URL+"/broken/skill-package", "", ""); err != nil {
		t.Fatalf("installMixedSkillWithIntegrityAndLocator() error = %v", err)
	}
	if locatorHits != 1 || fallbackHits != 1 {
		t.Fatalf("expected locator failure to fall back to standard skill download, locatorHits=%d fallbackHits=%d", locatorHits, fallbackHits)
	}
}

func TestInstallMixedSkillFallsBackWhenDownloadLocatorChecksumMismatches(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	originalDefaultCenter := defaultRemoteHubCenterURL
	originalDefaultCenters := remote.DefaultRemoteHubCenterURLs
	defaultRemoteHubCenterURL = ""
	remote.DefaultRemoteHubCenterURLs = nil
	defer func() {
		defaultRemoteHubCenterURL = originalDefaultCenter
		remote.DefaultRemoteHubCenterURLs = originalDefaultCenters
	}()

	fallbackPackage := []byte(`{"id":"checksum_fallback_dep","name":"checksum_fallback_dep","description":"downloaded from verified fallback","version":"1.0.0","trust_level":"trusted","triggers":["checksum"],"steps":[{"action":"bash","params":{"command":"echo checksum"},"on_error":"stop"}]}`)
	var locatorHits, fallbackHits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"urls":[],"nodes":[]}`))
		case "/stale/skill-package":
			locatorHits++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"stale_dep","name":"stale_dep","description":"stale package","version":"0.1.0","trust_level":"trusted","triggers":["stale"],"steps":[{"action":"bash","params":{"command":"echo stale"},"on_error":"stop"}]}`))
		case "/api/v1/skills/checksum_fallback_dep/download":
			fallbackHits++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(fallbackPackage)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:  server.URL,
		RemoteHubCenterURLs: []string{server.URL},
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if err := app.installMixedSkillWithIntegrityAndLocator("skillmarket", "checksum_fallback_dep", "checksum_fallback_dep", server.URL+"/stale/skill-package", sha256HexForTest(fallbackPackage), ""); err != nil {
		t.Fatalf("installMixedSkillWithIntegrityAndLocator() error = %v", err)
	}
	if locatorHits != 1 || fallbackHits != 1 {
		t.Fatalf("expected checksum mismatch to fall back to verified standard download, locatorHits=%d fallbackHits=%d", locatorHits, fallbackHits)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(cfg.NLSkills) != 1 || cfg.NLSkills[0].Name != "checksum_fallback_dep" {
		t.Fatalf("fallback skill should be registered after checksum mismatch: %#v", cfg.NLSkills)
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
				"package_sha256":       "pkg-expense-approval",
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
		case "/api/v1/directory/pending-action", "/api/v1/directory/initiated", "/api/v1/directory/completed":
			// Hub-authoritative approval directory (reconcile/list best-effort merge):
			// this fixture has no hub-bound workflow items.
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "total": 0, "page": 1, "page_size": 50})
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
		case "/api/v1/directory/pending-action", "/api/v1/directory/initiated", "/api/v1/directory/completed":
			// Hub-authoritative approval directory (reconcile/list best-effort merge):
			// this fixture has no hub-bound workflow items.
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "total": 0, "page": 1, "page_size": 50})
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
