package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubmitMaclawAppPackageQueuesLocalSubmission(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pkg := `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {"id": "local-contract", "name": "Contract"}
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
			Message:    "缺少运行证据",
			Suggestion: "先运行一次应用并重新提交",
		}, {
			Severity: "invalid",
			Message:  "补充权限说明",
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
	if summaries[0].ReviewIssues[0].Severity != "error" || summaries[0].ReviewIssues[0].Message != "缺少运行证据" {
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
	if again.ReviewIssues[0].Message != "缺少运行证据" {
		t.Fatalf("review issues should be cloned: %#v", again.ReviewIssues)
	}
}
