package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
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

func identityIndexKeySample(idx map[string]NLSkillDefinition, n int) []string {
	out := make([]string, 0, n)
	for k := range idx {
		out = append(out, k)
		if len(out) >= n {
			break
		}
	}
	return out
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
