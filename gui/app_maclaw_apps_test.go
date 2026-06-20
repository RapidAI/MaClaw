package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestSubmitMaclawAppPackageQueuesLocalSubmission(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pkg := `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "local-contract",
				"name": "Contract",
				"binding": {
					"appSkill": {"id": "contract-super-app", "version": "1.0.0"},
					"dependencies": {"skills": [{"id": "contract-workflow", "version": "2.0.0", "kind": "workflow_skill", "required": true, "source": "market"}]}
				}
			}
		}]
	}`

	result, err := app.SubmitMaclawAppPackage(pkg)
	if err != nil {
		t.Fatalf("SubmitMaclawAppPackage error: %v", err)
	}
	if result["status"] != "submitted" || result["channel"] != "local" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result["package_sha256"] == "" || result["package_bytes"].(int) <= 0 {
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
	if summaries[0].AppNames[0] != "Contract" || len(summaries[0].PackageSHA) != 64 || summaries[0].PackageSize <= 0 {
		t.Fatalf("expected summary audit metadata: %#v", summaries[0])
	}
	if summaries[0].EventCount != 1 || summaries[0].LastEventAt == "" {
		t.Fatalf("expected summary event metadata: %#v", summaries[0])
	}
	if len(summaries[0].Dependencies) != 2 || summaries[0].Dependencies[1].ID != "contract-workflow" {
		t.Fatalf("expected summary dependency audit metadata: %#v", summaries[0].Dependencies)
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
			"app": {"id": "safe-append", "name": "Safe"}
		}]
	}`
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
	if dep := maclawAppPlanDepForTest(plan, "expense-approval-app"); dep == nil || !dep.Installed || dep.Action != "skip" || dep.Kind != "runtime_skill" {
		t.Fatalf("runtime app skill should be installed: %#v", dep)
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

	if _, err := app.RecordMaclawAppInstall(pkg, "market"); err != nil {
		t.Fatalf("RecordMaclawAppInstall() error = %v", err)
	}
	records, err := app.ListMaclawAppInstalls(10)
	if err != nil {
		t.Fatalf("ListMaclawAppInstalls() error = %v", err)
	}
	if len(records) != 1 || records[0].HasMissingRequired || !records[0].HasBlockingDependency {
		t.Fatalf("install record should persist blocking dependency health: %#v", records)
	}
	if dep := maclawAppPlanDepForTest(maclawAppInstallPlan{Dependencies: records[0].Dependencies}, "disabled-workflow"); dep == nil || dep.Health != "disabled" || dep.Action != "blocked" {
		t.Fatalf("install record should persist dependency health snapshot: %#v", records[0].Dependencies)
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
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# Installed app skill\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "installed-app-skill", SkillDir: skillDir, Status: "active"}}
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
			"dependencies": { "skills": [{ "id": "manual-only-skill", "required": true, "source": "local" }] }
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
		"hub":            "skillhub",
		"skillhub":       "skillhub",
		"market":         "skillmarket",
		"skillmarket":    "skillmarket",
		"enterprise_hub": "enterprise_hub",
	}
	for input, want := range cases {
		got, ok := maclawAppInstallSkillSource(input)
		if !ok || got != want {
			t.Fatalf("maclawAppInstallSkillSource(%q) = %q,%v want %q,true", input, got, ok, want)
		}
	}
	if got, ok := maclawAppInstallSkillSource("local"); ok || got != "" {
		t.Fatalf("local source should not auto-install, got %q,%v", got, ok)
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
			"kind": "tool_app",
			"binding": { "skill": { "id": "doc-archive" } }
		}
	}`
	result, err := app.RecordMaclawAppInstall(pkg, "market")
	if err != nil {
		t.Fatalf("RecordMaclawAppInstall() error = %v", err)
	}
	if result["app_count"] != 1 || result["package_sha"] == "" {
		t.Fatalf("unexpected record result: %#v", result)
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
func TestRecordMaclawAppInstallRegistersApprovalAppWithDataSrv(t *testing.T) {
	type capturedRequest struct {
		Method string
		Path   string
		Header http.Header
		Body   map[string]interface{}
	}
	captured := []capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := capturedRequest{Method: r.Method, Path: r.URL.Path, Header: r.Header.Clone()}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		captured = append(captured, item)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"app_id":"expense-approval","status":"installed"}`))
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
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
			"binding": {
				"appSkill": { "id": "expense-super-skill", "version": "1.0.0" },
				"datasrv": { "domain": "finance", "datasetID": "finance.expense_forms", "templateID": "finance.expenses" },
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

	result, err := app.RecordMaclawAppInstall(pkg, "market")
	if err != nil {
		t.Fatalf("RecordMaclawAppInstall() error = %v", err)
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
	if !ok || metadata["app_skill_id"] != "expense-super-skill" {
		t.Fatalf("registration body missing metadata: %#v", captured[0].Body)
	}
	workflowIDs, ok := metadata["workflow_skill_ids"].([]interface{})
	if !ok || len(workflowIDs) != 1 || workflowIDs[0] != "expense-workflow" {
		t.Fatalf("registration metadata missing workflow skill ids: %#v", metadata)
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
		BusinessStatus:     "approval_pending",
		ResultStatus:       "pending",
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
	pending, err := app.ListMaclawAppApprovalInstances("expense-approval", "pending_my_approval", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances() error = %v", err)
	}
	if len(pending) != 1 || pending[0].InstanceID != created.InstanceID || pending[0].WorkflowSkillID != "expense-approval-workflow" {
		t.Fatalf("unexpected filtered approval instances: %#v", pending)
	}
	handled, err := app.RecordMaclawAppApprovalInstance(maclawAppApprovalInstance{
		AppID:              "expense-approval",
		InstanceID:         created.InstanceID,
		Title:              "Expense #1",
		Lane:               "handled",
		Status:             "approved",
		CurrentNode:        "completed",
		Owner:              "alice",
		Approver:           "manager",
		Result:             "approved",
		WorkflowSkillID:    "expense-approval-workflow",
		WorkflowDecisionID: "decision-test-1",
		BusinessStatus:     "approved",
		ResultStatus:       "approved",
		ResultPayload:      map[string]any{"business_record": map[string]any{"id": "exp-1", "status": "approved"}, "text": "approved with note"},
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
	if _, err := app.ListMaclawAppApprovalInstances(" ", "all", 10); err == nil {
		t.Fatal("expected app_id required error")
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
		AppID:              "expense",
		AppName:            "Expense",
		BlueprintID:        "expense.blueprint.v1",
		DatasetID:          "finance.expenses",
		ApprovalObjectRole: "expense_report",
		ApprovalEvent:      "finance.submitted",
		InstanceID:         "appr-1",
		Title:              "Expense approval",
		Lane:               "pending_my_approval",
		Status:             "pending",
		CurrentNode:        "manager_review",
		Owner:              "alice",
		Approver:           "manager",
		Result:             "waiting",
		WorkflowSkillID:    "expense-approval-workflow",
		BusinessStatus:     "approval_pending",
		ResultStatus:       "pending",
		ResultPayload:      map[string]any{"business_record": map[string]any{"id": "exp-1"}},
		Outputs:            []maclawAppApprovalOutput{{Type: "text", Title: "Summary", Text: "waiting for manager"}},
		Artifacts:          []maclawAppApprovalArtifact{{ID: "artifact-1", Name: "receipt.pdf", URI: "artifact://approval/receipt"}},
	}
	created, err := app.SyncMaclawAppApprovalInstanceToDataSrv(maclawAppApprovalDataSrvSyncInput{DatasetID: "finance.expenses", RecordID: "exp-1", Instance: base})
	if err != nil {
		t.Fatalf("SyncMaclawAppApprovalInstanceToDataSrv create error = %v", err)
	}
	if created["synced"] != true || created["action"] != "create_record_approval" {
		t.Fatalf("unexpected create sync result: %#v", created)
	}
	base.Status = "approved"
	base.Lane = "handled"
	base.Result = "approved"
	base.WorkflowDecisionID = "decision-1"
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
	if len(captured) != 4 {
		t.Fatalf("captured %d requests, want 4: %#v", len(captured), captured)
	}
	if captured[0].Method != http.MethodPost || captured[0].Path != "/api/v1/data/datasets/finance.expenses/records/exp-1/approvals" {
		t.Fatalf("unexpected create request: %#v", captured[0])
	}
	if captured[0].Body["workflow_instance_id"] != "appr-1" || captured[0].Body["business_status"] != "approval_pending" || captured[0].Body["result_status"] != "pending" {
		t.Fatalf("create body missing approval link fields: %#v", captured[0].Body)
	}
	if captured[0].Body["app_id"] != "expense" || captured[0].Body["blueprint_id"] != "expense.blueprint.v1" || captured[0].Body["object_role"] != "expense_report" {
		t.Fatalf("create body missing app approval semantics: %#v", captured[0].Body)
	}
	request, ok := captured[0].Body["request"].(map[string]interface{})
	if !ok || request["approval_instance_id"] != "appr-1" || request["object_role"] != "expense_report" || request["blueprint_id"] != "expense.blueprint.v1" {
		t.Fatalf("create body request should keep app approval context: %#v", captured[0].Body)
	}
	if payload, ok := captured[0].Body["result_payload"].(map[string]interface{}); !ok || payload["business_record"] == nil {
		t.Fatalf("create body missing result payload: %#v", captured[0].Body)
	}
	if created["approval_id"] != "approval-remote-1" {
		t.Fatalf("create sync should expose remote approval id: %#v", created)
	}
	stored, err := app.ListMaclawAppApprovalInstances("expense", "all", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppApprovalInstances after create sync error = %v", err)
	}
	if len(stored) != 1 || stored[0].ApprovalID != "approval-remote-1" || stored[0].RecordApprovalID != "approval-remote-1" || stored[0].ObjectRole != "expense_report" {
		t.Fatalf("create sync should persist remote approval context: %#v", stored)
	}
	if outputs, ok := captured[0].Body["outputs"].([]interface{}); !ok || len(outputs) != 1 {
		t.Fatalf("create body missing outputs: %#v", captured[0].Body)
	}
	if artifacts, ok := captured[0].Body["artifacts"].([]interface{}); !ok || len(artifacts) != 1 {
		t.Fatalf("create body missing artifacts: %#v", captured[0].Body)
	}
	if captured[1].Method != http.MethodPost || captured[1].Path != "/api/v1/data/approvals/approval-remote-1/review" {
		t.Fatalf("unexpected review request: %#v", captured[1])
	}
	if captured[1].Body["decision"] != "approved" || captured[1].Body["workflow_decision_id"] != "decision-1" || captured[1].Body["business_status"] != "approved" || captured[1].Body["result_status"] != "approved" {
		t.Fatalf("review body missing decision fields: %#v", captured[1].Body)
	}
	if captured[1].Body["result_payload"] == nil || captured[1].Body["outputs"] == nil || captured[1].Body["artifacts"] == nil {
		t.Fatalf("review body missing result package: %#v", captured[1].Body)
	}
	if captured[2].Method != http.MethodGet || captured[2].Path != "/api/v1/data/datasets/finance.expenses/records/exp-1" {
		t.Fatalf("unexpected business record get request: %#v", captured[2])
	}
	if captured[3].Method != http.MethodPatch || captured[3].Path != "/api/v1/data/datasets/finance.expenses/records/exp-1" {
		t.Fatalf("unexpected business record patch request: %#v", captured[3])
	}
	patchedData, ok := captured[3].Body["data"].(map[string]interface{})
	if !ok || patchedData["amount"] != float64(1200) || patchedData["status"] != "approved" || patchedData["payment_status"] != "pending_payment" {
		t.Fatalf("business record patch should merge existing data with approval result: %#v", captured[3].Body)
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
	if len(captured) != 2 {
		t.Fatalf("captured %d requests, want 2: %#v", len(captured), captured)
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
	if captured[1].Method != http.MethodPost || captured[1].Path != "/api/v1/data/datasets/finance.expenses/records/exp-2/approvals" {
		t.Fatalf("unexpected approval create request: %#v", captured[1])
	}
}

func TestPlanMaclawAppInstallRejectsUnknownSchema(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if _, err := app.PlanMaclawAppInstall(`{"schema":"unknown","privateMarker":"x_maclaw_apps"}`); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatalf("expected schema error, got %v", err)
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
