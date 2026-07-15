package maclawappcontract

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateGUIInstallHubPackageAcceptsPublishedPackage(t *testing.T) {
	pkg := validGUIInstallHubPackage()

	if err := ValidateGUIInstallHubPackage(pkg, "cap-approval-ready-app"); err != nil {
		t.Fatalf("ValidateGUIInstallHubPackage() error = %v", err)
	}
}

func TestValidateGUIInstallHubPackageRejectsMissingSignature(t *testing.T) {
	pkg := validGUIInstallHubPackage()
	delete(pkg, "package_signature")

	err := ValidateGUIInstallHubPackage(pkg, "cap-approval-ready-app")
	if err == nil || !strings.Contains(err.Error(), "package_signature") {
		t.Fatalf("expected package_signature error, got %v", err)
	}
}

func TestValidateGUIInstallHubPackageRejectsEntrySignatureFingerprintDrift(t *testing.T) {
	pkg := validGUIInstallHubPackage()
	apps := pkg["apps"].([]any)
	entry := apps[0].(map[string]any)
	submission := entry["app"].(map[string]any)["governance"].(map[string]any)["submission"].(map[string]any)
	submission["package_signature"].(map[string]any)["public_key_fingerprint"] = "sha256:other-key"

	err := ValidateGUIInstallHubPackage(pkg, "cap-approval-ready-app")
	if err == nil || !strings.Contains(err.Error(), "fingerprint") {
		t.Fatalf("expected fingerprint drift error, got %v", err)
	}
}

func TestValidateGUIInstallHubPackageRejectsMissingDependencyVerification(t *testing.T) {
	pkg := validGUIInstallHubPackage()
	apps := pkg["apps"].([]any)
	governance := apps[0].(map[string]any)["app"].(map[string]any)["governance"].(map[string]any)
	delete(governance, "dependencyVerification")

	err := ValidateGUIInstallHubPackage(pkg, "cap-approval-ready-app")
	if err == nil || !strings.Contains(err.Error(), "dependency_verification") {
		t.Fatalf("expected dependency_verification error, got %v", err)
	}
}

func TestValidateGUIInstallHubPackageRejectsBlockingDependencyVerification(t *testing.T) {
	pkg := validGUIInstallHubPackage()
	apps := pkg["apps"].([]any)
	verification := apps[0].(map[string]any)["app"].(map[string]any)["governance"].(map[string]any)["dependencyVerification"].(map[string]any)
	verification["blocked"] = true

	err := ValidateGUIInstallHubPackage(pkg, "cap-approval-ready-app")
	if err == nil || !strings.Contains(err.Error(), "blocking dependencies") {
		t.Fatalf("expected blocking dependency_verification error, got %v", err)
	}
}

func TestValidateGUIInstallHubPackageSynthesizesMissingResolvedDependencies(t *testing.T) {
	pkg := validGUIInstallHubPackage()
	delete(pkg, "resolved_dependencies")

	if err := ValidateGUIInstallHubPackage(pkg, "cap-approval-ready-app"); err != nil {
		t.Fatalf("legacy package without resolved_dependencies should validate after synthesis: %v", err)
	}
	deps := anySlice(pkg["resolved_dependencies"])
	if len(deps) == 0 {
		t.Fatalf("expected synthesized resolved_dependencies, got %#v", pkg["resolved_dependencies"])
	}
	compat := anyMap(pkg["compatibility"])
	if compat["resolved_dependencies_synthesized"] != true {
		t.Fatalf("expected compatibility.resolved_dependencies_synthesized marker, got %#v", compat)
	}
	first := anyMap(deps[0])
	if strings.TrimSpace(stringValue(first["id"])) != "approval-workflow" {
		t.Fatalf("synthesized dep id = %#v", first)
	}
	if strings.TrimSpace(stringValue(first["install_ref"])) == "" {
		t.Fatalf("synthesized dep missing install_ref: %#v", first)
	}
}

func TestNormalizeGUIInstallHubPackageNoopWhenResolvedPresent(t *testing.T) {
	pkg := validGUIInstallHubPackage()
	synthesized, notes := NormalizeGUIInstallHubPackage(pkg)
	if synthesized {
		t.Fatalf("expected no synthesis when resolved_dependencies already present, notes=%v", notes)
	}
}

func TestDownloadGUIInstallHubPackageFetchesAndValidatesPackage(t *testing.T) {
	var gotPath string
	var gotAuth string
	servedPackage, fingerprint := signedHubPackage(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(servedPackage)
	}))
	defer server.Close()

	pkg, err := DownloadGUIInstallHubPackage(context.Background(), server.Client(), server.URL, "viewer-token", "cap-approval-ready-app")
	if err != nil {
		t.Fatalf("DownloadGUIInstallHubPackage() error = %v", err)
	}
	if gotPath != "/api/capabilities/maclaw-apps/cap-approval-ready-app/package" {
		t.Fatalf("path=%q", gotPath)
	}
	if gotAuth != "Bearer viewer-token" {
		t.Fatalf("Authorization=%q", gotAuth)
	}
	if pkg["schema"] != "maclaw.app.pack.v1" {
		t.Fatalf("unexpected package=%+v", pkg)
	}
	if err := ValidateGUIInstallHubPackage(pkg, "cap-approval-ready-app"); err != nil {
		t.Fatalf("downloaded package should satisfy GUI install contract: %v", err)
	}
	gotFingerprint, err := VerifyHubPackageSignature(pkg)
	if err != nil {
		t.Fatalf("downloaded package signature should verify: %v", err)
	}
	if gotFingerprint != fingerprint {
		t.Fatalf("downloaded package fingerprint=%q want %q", gotFingerprint, fingerprint)
	}
}

func TestDownloadGUIInstallHubPackageReportsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not published", http.StatusForbidden)
	}))
	defer server.Close()

	_, err := DownloadGUIInstallHubPackage(context.Background(), server.Client(), server.URL, "viewer-token", "cap-approval-ready-app")
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "not published") {
		t.Fatalf("expected HTTP error detail, got %v", err)
	}
}

func TestVerifyHubPackageSignatureAcceptsSignedPackage(t *testing.T) {
	pkg, fingerprint := signedHubPackage(t)

	got, err := VerifyHubPackageSignature(pkg)
	if err != nil {
		t.Fatalf("VerifyHubPackageSignature() error = %v", err)
	}
	if got != fingerprint {
		t.Fatalf("fingerprint=%q want %q", got, fingerprint)
	}
}

func TestVerifyHubPackageSignatureRejectsChecksumMismatch(t *testing.T) {
	pkg, _ := signedHubPackage(t)
	pkg["package_sha256"] = strings.Repeat("b", 64)

	_, err := VerifyHubPackageSignature(pkg)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
}

func TestVerifyHubPackageSignatureRejectsTamperedPayload(t *testing.T) {
	pkg, _ := signedHubPackage(t)
	pkg["package_signature"].(map[string]any)["payload"] = "tampered"

	_, err := VerifyHubPackageSignature(pkg)
	if err == nil || !strings.Contains(err.Error(), "verification failed") {
		t.Fatalf("expected verification failure, got %v", err)
	}
}

func validGUIInstallHubPackage() map[string]any {
	signature := map[string]any{
		"schema":                 "maclaw.app.package_signature.v1",
		"algorithm":              "ed25519",
		"payload":                "maclaw-app\nsha256-package\nversion-key\n2026-07-01T01:00:00Z\nhub-admin",
		"signature_base64":       "signature",
		"public_key_base64":      "public-key",
		"public_key_fingerprint": "sha256:test-key",
		"package_sha256":         "sha256-package",
		"signed_at":              "2026-07-01T01:00:00Z",
		"signed_by":              "hub-admin",
	}
	entrySignature := map[string]any{}
	for key, value := range signature {
		entrySignature[key] = value
	}
	return map[string]any{
		"schema":            "maclaw.app.pack.v1",
		"source":            "enterprise_hub",
		"package_sha256":    "sha256-package",
		"package_signature": signature,
		"capability": map[string]any{
			"id":                  "cap-approval-ready-app",
			"status":              "published",
			"current_version_key": "enterprise_hub:skill:maclaw-app:approval-ready-app@pkg",
		},
		"review_evidence": map[string]any{
			"approval-ready-app": map[string]any{"run_id": "run-ready-approval"},
		},
		"maclaw_app_review_evidence": map[string]any{
			"approval-ready-app": map[string]any{"run_id": "run-ready-approval"},
		},
		"resolved_dependencies": []any{
			map[string]any{"id": "approval-workflow", "kind": "workflow_skill", "required": true},
		},
		"apps": []any{
			map[string]any{
				"schema": "maclaw.app.v1",
				"app": map[string]any{
					"id": "approval-ready-app",
					"governance": map[string]any{
						"dependencyVerification": map[string]any{
							"schema":          "maclaw.app.install_plan.v1",
							"dependencyCount": 1,
							"blocked":         false,
							"dependencies": []any{
								map[string]any{"id": "approval-workflow", "kind": "workflow_skill", "required": true, "source": "enterprise_hub", "installed": true, "health": "ready"},
							},
						},
						"submission": map[string]any{
							"status":            "published",
							"capability_id":     "cap-approval-ready-app",
							"version_key":       "enterprise_hub:skill:maclaw-app:approval-ready-app@pkg",
							"package_sha256":    "sha256-package",
							"package_signature": entrySignature,
							"review_evidence": map[string]any{
								"approval-ready-app": map[string]any{"run_id": "run-ready-approval"},
							},
						},
					},
				},
			},
		},
	}
}

func signedHubPackage(t *testing.T) (map[string]any, string) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	packageSHA := strings.Repeat("a", 64)
	payload := "maclaw-app\n" + packageSHA + "\nenterprise_hub:skill:maclaw-app:approval-ready-app@pkg\n2026-07-01T01:00:00Z\nhub-admin"
	fingerprint := HubPackagePublicKeyFingerprint(publicKey)
	pkg := validGUIInstallHubPackage()
	pkg["package_sha256"] = packageSHA
	pkg["package_signature"] = map[string]any{
		"schema":                 "maclaw.app.package_signature.v1",
		"algorithm":              "ed25519",
		"payload":                payload,
		"public_key_base64":      base64.StdEncoding.EncodeToString(publicKey),
		"public_key_fingerprint": fingerprint,
		"signature_base64":       base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(payload))),
		"package_sha256":         packageSHA,
		"version_key":            "enterprise_hub:skill:maclaw-app:approval-ready-app@pkg",
		"signed_at":              "2026-07-01T01:00:00Z",
		"signed_by":              "hub-admin",
	}
	apps := pkg["apps"].([]any)
	entry := apps[0].(map[string]any)
	submission := entry["app"].(map[string]any)["governance"].(map[string]any)["submission"].(map[string]any)
	submission["package_sha256"] = packageSHA
	submission["package_signature"] = pkg["package_signature"]
	return pkg, fingerprint
}
