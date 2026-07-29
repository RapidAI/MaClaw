package maclawappcontract

import (
	"fmt"
	"strings"
	"testing"
)

func TestSelectHubPackageAppsFiltersAppsDependenciesAndReviewEvidence(t *testing.T) {
	pkg := multiAppHubPackageForSelectionTest()

	selected, err := SelectHubPackageApps(pkg, []string{"market-kept-app"})
	if err != nil {
		t.Fatalf("SelectHubPackageApps() error = %v", err)
	}
	selected["source"] = "mutated"
	if pkg["source"] == "mutated" {
		t.Fatalf("SelectHubPackageApps should clone package")
	}
	apps := anySlice(selected["apps"])
	if len(apps) != 1 || anyMap(anyMap(apps[0])["app"])["id"] != "kept-app" {
		t.Fatalf("selected apps mismatch: %#v", selected["apps"])
	}
	deps := anySlice(selected["resolved_dependencies"])
	if len(deps) != 1 || anyMap(deps[0])["id"] != "kept-skill" {
		t.Fatalf("selected dependencies mismatch: %#v", selected["resolved_dependencies"])
	}
	review := anyMap(selected["review_evidence"])
	if len(review) != 1 || anyMap(review["kept-app"])["run_id"] != "run-kept" || strings.Contains(fmt.Sprint(review), "run-skipped") {
		t.Fatalf("selected review evidence mismatch: %#v", review)
	}
	entry := anyMap(apps[0])
	submission := anyMap(anyMap(anyMap(entry["app"])["governance"])["submission"])
	submissionReview := anyMap(submission["review_evidence"])
	if len(submissionReview) != 1 || anyMap(submissionReview["kept-app"])["run_id"] != "run-kept" || strings.Contains(fmt.Sprint(submissionReview), "run-skipped") {
		t.Fatalf("selected submission review evidence mismatch: %#v", submissionReview)
	}
	if anyMap(selected["package_signature"])["public_key_fingerprint"] != "sha256:test-key" || submission["package_signature"] == nil {
		t.Fatalf("selected package should preserve Hub signature evidence: package=%#v submission=%#v", selected["package_signature"], submission)
	}
	if strings.Contains(fmt.Sprint(selected), "skipped-app") || strings.Contains(fmt.Sprint(selected), "skipped-skill") {
		t.Fatalf("selected package leaked skipped app/dependency: %#v", selected)
	}
}

func TestSelectHubPackageAppsKeepsFlatReviewEvidence(t *testing.T) {
	pkg := multiAppHubPackageForSelectionTest()
	pkg["review_evidence"] = map[string]any{"run_id": "flat-run", "status": "approved"}

	selected, err := SelectHubPackageApps(pkg, []string{"kept-app"})
	if err != nil {
		t.Fatalf("SelectHubPackageApps() error = %v", err)
	}
	review := anyMap(selected["review_evidence"])
	if review["run_id"] != "flat-run" || review["status"] != "approved" {
		t.Fatalf("flat review evidence should be preserved: %#v", review)
	}
}

func TestSelectHubPackageAppsRejectsMissingSelection(t *testing.T) {
	_, err := SelectHubPackageApps(multiAppHubPackageForSelectionTest(), []string{"missing-app"})
	if err == nil || !strings.Contains(err.Error(), "no matching apps") {
		t.Fatalf("expected missing selection error, got %v", err)
	}
}

func TestSelectHubPackageAppsKeepsDependencyForLegacySingularAppDependency(t *testing.T) {
	pkg := multiAppHubPackageForSelectionTest()
	apps := anySlice(pkg["apps"])
	keptApp := anyMap(anyMap(apps[1])["app"])
	keptApp["dependencies"] = map[string]any{
		"skill": map[string]any{"id": "kept-skill", "kind": "runtime_skill", "required": true},
	}
	delete(anyMap(keptApp["binding"]), "dependencies")

	selected, err := SelectHubPackageApps(pkg, []string{"kept-app"})
	if err != nil {
		t.Fatalf("SelectHubPackageApps() error = %v", err)
	}
	deps := anySlice(selected["resolved_dependencies"])
	if len(deps) != 1 || anyMap(deps[0])["id"] != "kept-skill" {
		t.Fatalf("selected legacy singular dependency mismatch: %#v", selected["resolved_dependencies"])
	}
}

func TestSelectHubPackageAppsKeepsDependencyForLegacyBindingSkill(t *testing.T) {
	pkg := multiAppHubPackageForSelectionTest()
	apps := anySlice(pkg["apps"])
	keptApp := anyMap(anyMap(apps[1])["app"])
	keptBinding := anyMap(keptApp["binding"])
	delete(keptBinding, "dependencies")
	keptBinding["skill"] = map[string]any{"id": "kept-skill", "kind": "runtime_skill", "required": true}

	selected, err := SelectHubPackageApps(pkg, []string{"kept-app"})
	if err != nil {
		t.Fatalf("SelectHubPackageApps() error = %v", err)
	}
	deps := anySlice(selected["resolved_dependencies"])
	if len(deps) != 1 || anyMap(deps[0])["id"] != "kept-skill" {
		t.Fatalf("selected legacy binding skill dependency mismatch: %#v", selected["resolved_dependencies"])
	}
}

func TestSelectHubPackageAppsKeepsDependencyWhenResolvedAppIDUsesMarketPrefix(t *testing.T) {
	pkg := multiAppHubPackageForSelectionTest()
	deps := anySlice(pkg["resolved_dependencies"])
	anyMap(deps[1])["app_ids"] = []any{"market-kept-app"}

	selected, err := SelectHubPackageApps(pkg, []string{"kept-app"})
	if err != nil {
		t.Fatalf("SelectHubPackageApps() error = %v", err)
	}
	deps = anySlice(selected["resolved_dependencies"])
	if len(deps) != 1 || anyMap(deps[0])["id"] != "kept-skill" {
		t.Fatalf("selected dependency with market-prefixed app ID mismatch: %#v", selected["resolved_dependencies"])
	}
}

func TestSelectHubPackageAppsScopesSharedResolvedDependencyToSelectedApp(t *testing.T) {
	pkg := multiAppHubPackageForSelectionTest()
	deps := anySlice(pkg["resolved_dependencies"])
	anyMap(deps[1])["app_ids"] = []any{"kept-app", "skipped-app"}

	selected, err := SelectHubPackageApps(pkg, []string{"kept-app"})
	if err != nil {
		t.Fatalf("SelectHubPackageApps() error = %v", err)
	}
	deps = anySlice(selected["resolved_dependencies"])
	appIDs := stringList(anyMap(deps[0])["app_ids"])
	if len(deps) != 1 || len(appIDs) != 1 || appIDs[0] != "kept-app" {
		t.Fatalf("shared dependency should be scoped to selected app: %#v", selected["resolved_dependencies"])
	}
}

func multiAppHubPackageForSelectionTest() map[string]any {
	signature := map[string]any{"algorithm": "ed25519", "public_key_fingerprint": "sha256:test-key", "package_sha256": "sha-test"}
	reviewEvidence := map[string]any{
		"skipped-app": map[string]any{"run_id": "run-skipped"},
		"kept-app":    map[string]any{"run_id": "run-kept"},
	}
	appEntry := func(id, skill string) map[string]any {
		return map[string]any{
			"schema": "maclaw.app.v1",
			"app": map[string]any{
				"id": id,
				"binding": map[string]any{
					"dependencies": map[string]any{"skills": []any{map[string]any{"id": skill, "kind": "runtime_skill", "required": true}}},
				},
				"governance": map[string]any{
					"submission": map[string]any{
						"status":            "published",
						"package_signature": signature,
						"review_evidence":   reviewEvidence,
					},
				},
			},
		}
	}
	return map[string]any{
		"schema":                     "maclaw.app.pack.v1",
		"package_signature":          signature,
		"review_evidence":            reviewEvidence,
		"maclaw_app_review_evidence": reviewEvidence,
		"apps":                       []any{appEntry("skipped-app", "skipped-skill"), appEntry("kept-app", "kept-skill")},
		"resolved_dependencies":      []any{map[string]any{"id": "skipped-skill", "app_ids": []any{"skipped-app"}}, map[string]any{"id": "kept-skill", "app_ids": []any{"kept-app"}}},
	}
}
