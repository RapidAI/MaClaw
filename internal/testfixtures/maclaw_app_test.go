package testfixtures

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
)

func TestReadyEnterpriseApprovalMaclawAppPublishedHubPackage(t *testing.T) {
	pkg := ReadyEnterpriseApprovalMaclawAppPublishedHubPackage("cap-approval-ready-app", "enterprise_hub:skill:maclaw-app:approval-ready-app@pkg")
	if pkg["source"] != "enterprise_hub" || pkg["capability_id"] != "cap-approval-ready-app" {
		t.Fatalf("published package should expose Hub source and capability id: %#v", pkg)
	}
	capability, _ := pkg["capability"].(map[string]any)
	if capability["status"] != "published" || capability["current_version_key"] != "enterprise_hub:skill:maclaw-app:approval-ready-app@pkg" {
		t.Fatalf("published package should expose published capability metadata: %#v", capability)
	}
	reviewEvidence, _ := pkg["review_evidence"].(map[string]any)
	appEvidence, _ := reviewEvidence["approval-ready-app"].(map[string]any)
	if appEvidence["run_id"] != "run-ready-approval" || appEvidence["approval_status"] != "approved" {
		t.Fatalf("published package should expose review evidence summary: %#v", reviewEvidence)
	}
	apps, _ := pkg["apps"].([]any)
	if len(apps) != 1 {
		t.Fatalf("published package should include one app entry: %#v", apps)
	}
	entry, _ := apps[0].(map[string]any)
	app, _ := entry["app"].(map[string]any)
	binding, _ := app["binding"].(map[string]any)
	datasrv, _ := binding["datasrv"].(map[string]any)
	if datasrv["datasetID"] != "finance.expense_forms" || datasrv["objectRole"] != "expense_request" {
		t.Fatalf("published approval package should include DataSrv binding: %#v", datasrv)
	}
	deps, _ := pkg["resolved_dependencies"].([]any)
	if len(deps) != 2 {
		t.Fatalf("published approval package should expose two resolved dependencies: %#v", deps)
	}
	firstDep, _ := deps[0].(map[string]any)
	if firstDep["source"] != "enterprise_hub" || firstDep["install_ref"] != "enterprise_hub://capabilities/cap-approval-ready-app-skill@1.0.0" {
		t.Fatalf("published approval package should use enterprise Hub dependency refs: %#v", deps)
	}
	ui, _ := app["ui"].(map[string]any)
	layouts, _ := ui["layouts"].(map[string]any)
	layout, _ := layouts["approval_workspace"].(map[string]any)
	if layout["fingerprint"] == "" || layout["visibleRegionCount"] != 3 {
		t.Fatalf("published approval package should include canonical workspace layout evidence: %#v", layout)
	}
	governance, _ := app["governance"].(map[string]any)
	submission, _ := governance["submission"].(map[string]any)
	if submission["status"] != "published" || submission["capability_id"] != "cap-approval-ready-app" {
		t.Fatalf("published package should expose entry submission metadata: %#v", submission)
	}
}

func TestSignPublishedMaclawAppHubPackage(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	pkg := ReadyEnterpriseApprovalMaclawAppPublishedHubPackage("cap-approval-ready-app", "enterprise_hub:skill:maclaw-app:approval-ready-app@pkg")
	signature := SignPublishedMaclawAppHubPackage(pkg, publicKey, privateKey, strings.Repeat("a", 64), "", "", "")
	if signature["algorithm"] != "ed25519" || signature["package_sha256"] != strings.Repeat("a", 64) {
		t.Fatalf("signature should expose ed25519 package hash metadata: %#v", signature)
	}
	if signature["public_key_fingerprint"] != MaclawAppHubPackagePublicKeyFingerprint(publicKey) {
		t.Fatalf("signature should expose GUI-compatible public key fingerprint: %#v", signature)
	}
	topLevelSignature, _ := pkg["package_signature"].(map[string]any)
	if topLevelSignature["signature_base64"] != signature["signature_base64"] || topLevelSignature["payload"] != signature["payload"] {
		t.Fatalf("top-level package signature should be installed on package: %#v", topLevelSignature)
	}
	apps, _ := pkg["apps"].([]any)
	entry, _ := apps[0].(map[string]any)
	app, _ := entry["app"].(map[string]any)
	governance, _ := app["governance"].(map[string]any)
	submission, _ := governance["submission"].(map[string]any)
	submissionSignature, _ := submission["package_signature"].(map[string]any)
	if submissionSignature["signature_base64"] != signature["signature_base64"] || submissionSignature["payload"] != signature["payload"] {
		t.Fatalf("entry submission should mirror package signature: %#v", submissionSignature)
	}
}
