package httpapi

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hub/internal/capability"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func TestBuildMaclawAppSkillZipForHubCenter(t *testing.T) {
	t.Parallel()
	item := &capability.CapabilitySummary{
		ID:             "cap-app-1",
		CapabilityType: corelib.CapabilityTypeSkill,
		CapabilityID:   "invoice-review",
		DisplayName:    "Invoice Review",
		Description:    "Review invoices with a guided panel",
		GlobalKey:      "enterprise_hub:skill:maclaw-app:invoice-review",
		Status:         "published",
		ManagedBy:      "maclaw_app_upload",
		MetadataJSON:   `{"is_maclaw_app":true,"product_kind":"maclaw_app_skill","maclaw_app_id":"invoice-review","maclaw_app_name":"Invoice Review","maclaw_app_category":"finance","maclaw_app_icon":"receipt","maclaw_app_input_mode":"file","maclaw_app_output_modes":["pdf","docx"],"package_sha256":"abc123"}`,
	}
	version := &capability.VersionSummary{
		VersionKey:      "enterprise_hub:skill:maclaw-app:invoice-review@abc123",
		PackageChecksum: "abc123",
		ManifestJSON: `{
  "schema": "maclaw.app.v1",
  "privateMarker": "x_maclaw_apps",
  "app": {"id": "invoice-review", "name": "Invoice Review", "description": "Review invoices with a guided panel", "kind": "tool_app"}
}`,
	}
	metadata := mapFromRawJSON(json.RawMessage(item.MetadataJSON))

	zipPath, cleanup, err := buildMaclawAppSkillZipForHubCenter(item, version, metadata)
	if err != nil {
		t.Fatalf("buildMaclawAppSkillZipForHubCenter: %v", err)
	}
	defer cleanup()

	files := readZipFiles(t, zipPath)
	for _, name := range []string{"skill.yaml", "skill_package_manifest.json", "maclaw.app.json"} {
		if _, ok := files[name]; !ok {
			t.Fatalf("zip missing %s; got %#v", name, keysOf(files))
		}
	}
	if !strings.Contains(files["skill.yaml"], "invoice-review") {
		t.Fatalf("skill.yaml missing skill name: %s", files["skill.yaml"])
	}
	var manifest map[string]any
	if err := json.Unmarshal([]byte(files["skill_package_manifest.json"]), &manifest); err != nil {
		t.Fatalf("manifest json: %v", err)
	}
	if manifest["product_kind"] != "maclaw_app_skill" || manifest["is_maclaw_app"] != true {
		t.Fatalf("unexpected product metadata: %#v", manifest)
	}
	if manifest["maclaw_app_id"] != "invoice-review" || manifest["maclaw_app_entry"] != "maclaw.app.json" {
		t.Fatalf("unexpected app id/entry: %#v", manifest)
	}
	if !strings.Contains(files["maclaw.app.json"], `"invoice-review"`) {
		t.Fatalf("maclaw.app.json missing app id: %s", files["maclaw.app.json"])
	}
}

func TestPrepareMaclawAppZipRequiresPublished(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	ctx := capability.WithTenant(context.Background(), store.DefaultTenantID)
	item, err := svc.UpsertCapability(ctx, capability.UpsertCapabilityInput{
		ID:                "cap-pending-app",
		CapabilityType:    corelib.CapabilityTypeSkill,
		CapabilityID:      "pending-app",
		DisplayName:       "Pending App",
		Publisher:         "admin@example.com",
		Source:            corelib.CapabilitySourceEnterpriseHub,
		ManagedBy:         "maclaw_app_upload",
		Status:            "pending_review",
		GlobalKey:         "enterprise_hub:skill:maclaw-app:pending-app",
		MetadataJSON:      `{"is_maclaw_app":true,"product_kind":"maclaw_app_skill","maclaw_app_id":"pending-app","publisher_email":"admin@example.com"}`,
		Version:           "1",
		VersionKey:        "enterprise_hub:skill:maclaw-app:pending-app@v1",
		ManifestJSON:      `{"schema":"maclaw.app.v1","privateMarker":"x_maclaw_apps","app":{"id":"pending-app","name":"Pending App"}}`,
		SetCurrentVersion: true,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	_, _, _, err = prepareMaclawAppZipForHubCenterUpload(ctx, svc, item)
	if err == nil {
		t.Fatal("expected not-published error")
	}
	var appErr *maclawAppHubCenterUploadError
	if !errors.As(err, &appErr) || appErr.Code != "MACLAW_APP_NOT_PUBLISHED" {
		t.Fatalf("err=%v want MACLAW_APP_NOT_PUBLISHED", err)
	}
}

func TestBuildMaclawAppSkillZipRequiresManifest(t *testing.T) {
	t.Parallel()
	item := &capability.CapabilitySummary{
		ID:           "cap-no-manifest",
		CapabilityID: "no-manifest-app",
		DisplayName:  "No Manifest",
	}
	version := &capability.VersionSummary{VersionKey: "k", ManifestJSON: ""}
	_, _, err := buildMaclawAppSkillZipForHubCenter(item, version, map[string]any{"maclaw_app_id": "no-manifest-app"})
	var appErr *maclawAppHubCenterUploadError
	if !errors.As(err, &appErr) || appErr.Code != "MACLAW_APP_MANIFEST_MISSING" {
		t.Fatalf("err=%v want MACLAW_APP_MANIFEST_MISSING", err)
	}
}

func TestBuildMaclawAppSkillZipRejectsInvalidEntry(t *testing.T) {
	t.Parallel()
	item := &capability.CapabilitySummary{
		ID:           "cap-bad-entry",
		CapabilityID: "bad-entry",
		DisplayName:  "Bad Entry",
	}
	version := &capability.VersionSummary{
		VersionKey:   "k",
		ManifestJSON: `{"schema":"maclaw.app.v1","privateMarker":"x_maclaw_apps","app":{"name":"No ID"}}`,
	}
	_, _, err := buildMaclawAppSkillZipForHubCenter(item, version, map[string]any{"maclaw_app_id": "bad-entry"})
	var appErr *maclawAppHubCenterUploadError
	if !errors.As(err, &appErr) || appErr.Code != "MACLAW_APP_MANIFEST_INVALID" {
		t.Fatalf("err=%v want MACLAW_APP_MANIFEST_INVALID", err)
	}

	_, _, err = buildMaclawAppSkillZipForHubCenter(item, &capability.VersionSummary{
		VersionKey:   "k2",
		ManifestJSON: `{not-json`,
	}, map[string]any{"maclaw_app_id": "bad-entry"})
	if !errors.As(err, &appErr) || appErr.Code != "MACLAW_APP_MANIFEST_INVALID" {
		t.Fatalf("err=%v want invalid JSON MACLAW_APP_MANIFEST_INVALID", err)
	}
}

func TestHubCenterMaclawAppTestEvidenceSubsetCanonicalizesKeys(t *testing.T) {
	t.Parallel()
	got := hubCenterMaclawAppTestEvidenceSubset(map[string]any{
		"runId":                 "run-1",
		"verifiedAt":            "2026-01-01T00:00:00Z",
		"definitionFingerprint": "fp-1",
		"artifactPresent":       true,
		"artifactName":          "out.pdf",
		"outputCount":           2,
		"primaryResult":         "ok",
		"resultPayload":         map[string]any{"status": "ok"},
	})
	if got["run_id"] != "run-1" || got["verified_at"] != "2026-01-01T00:00:00Z" {
		t.Fatalf("canonical keys missing: %#v", got)
	}
	if _, ok := got["runId"]; ok {
		t.Fatalf("camelCase alias should not remain: %#v", got)
	}
	if got["definition_fingerprint"] != "fp-1" || got["artifact_name"] != "out.pdf" {
		t.Fatalf("unexpected subset: %#v", got)
	}
}

func TestAdminCapabilityUploadToMarketMaclawAppSuccess(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	ctx := capability.WithTenant(context.Background(), store.DefaultTenantID)
	manifest := `{
  "schema": "maclaw.app.v1",
  "privateMarker": "x_maclaw_apps",
  "app": {"id": "hubcenter-push-app", "name": "HubCenter Push App", "description": "Push me", "kind": "tool_app"}
}`
	item, err := svc.UpsertCapability(ctx, capability.UpsertCapabilityInput{
		ID:                "cap-hubcenter-push-app",
		CapabilityType:    corelib.CapabilityTypeSkill,
		CapabilityID:      "hubcenter-push-app",
		DisplayName:       "HubCenter Push App",
		Description:       "Push me",
		Publisher:         "admin@example.com",
		Source:            corelib.CapabilitySourceEnterpriseHub,
		ManagedBy:         "maclaw_app_upload",
		Status:            "published",
		GlobalKey:         "enterprise_hub:skill:maclaw-app:hubcenter-push-app",
		MetadataJSON:      `{"is_maclaw_app":true,"product_kind":"maclaw_app_skill","maclaw_app_id":"hubcenter-push-app","maclaw_app_name":"HubCenter Push App","publisher_email":"admin@example.com","package_sha256":"pkgsha"}`,
		Version:           "1",
		VersionKey:        "enterprise_hub:skill:maclaw-app:hubcenter-push-app@pkgsha",
		PackageChecksum:   "pkgsha",
		ManifestJSON:      manifest,
		SetCurrentVersion: true,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	var gotPath string
	var gotHasZip bool
	centerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/api/v1/skills/submit"):
			gotPath = r.URL.Path
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				t.Errorf("parse multipart: %v", err)
				http.Error(w, err.Error(), 400)
				return
			}
			file, _, err := r.FormFile("zip")
			if err != nil {
				t.Errorf("form zip: %v", err)
				http.Error(w, err.Error(), 400)
				return
			}
			defer file.Close()
			data, _ := io.ReadAll(file)
			gotHasZip = len(data) > 0
			tmp := filepath.Join(t.TempDir(), "upload.zip")
			if err := os.WriteFile(tmp, data, 0o644); err != nil {
				t.Errorf("write zip: %v", err)
			} else {
				files := readZipFiles(t, tmp)
				if _, ok := files["maclaw.app.json"]; !ok {
					t.Errorf("uploaded zip missing maclaw.app.json")
				}
				if _, ok := files["skill_package_manifest.json"]; !ok {
					t.Errorf("uploaded zip missing skill_package_manifest.json")
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"submission_id": "sub-app-1",
				"status":        "pending",
			})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/api/v1/skill-submissions/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"submission_id": "sub-app-1",
				"status":        "success",
				"skill_id":      "skill-app-1",
			})
		default:
			// Reconcile / discovery probes may hit other paths.
			http.NotFound(w, r)
		}
	}))
	defer centerSrv.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/admin/capabilities/"+item.ID+"/upload-to-market", nil)
	req.SetPathValue("id", item.ID)
	rec := httptest.NewRecorder()
	AdminCapabilityUploadToMarketHandler(svc, fakeCapabilityMarketCenterStatus{
		state: &center.RegistrationState{ActiveBaseURL: centerSrv.URL, HubID: "hub-test-1"},
	}, t.TempDir())(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(gotPath, "/api/v1/skills/submit") || !gotHasZip {
		t.Fatalf("hubcenter not called correctly path=%q hasZip=%v", gotPath, gotHasZip)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["submission_id"] != "sub-app-1" || body["status"] != "published" {
		t.Fatalf("unexpected body: %#v", body)
	}
	updated, err := svc.Get(ctx, item.ID)
	if err != nil {
		t.Fatalf("get updated: %v", err)
	}
	meta := mapFromRawJSON(json.RawMessage(updated.MetadataJSON))
	if stringFromAny(meta["hubcenter_submission_id"]) != "sub-app-1" {
		t.Fatalf("metadata not stamped: %#v", meta)
	}
	if stringFromAny(meta["hubcenter_skill_id"]) != "skill-app-1" {
		t.Fatalf("skill id not stamped: %#v", meta)
	}
}

func TestAdminCapabilityUploadToMarketMaclawAppNotPublished(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	ctx := capability.WithTenant(context.Background(), store.DefaultTenantID)
	item, err := svc.UpsertCapability(ctx, capability.UpsertCapabilityInput{
		ID:                "cap-app-approved-only",
		CapabilityType:    corelib.CapabilityTypeSkill,
		CapabilityID:      "approved-only-app",
		DisplayName:       "Approved Only",
		Publisher:         "admin@example.com",
		Source:            corelib.CapabilitySourceEnterpriseHub,
		ManagedBy:         "maclaw_app_upload",
		Status:            "approved",
		GlobalKey:         "enterprise_hub:skill:maclaw-app:approved-only-app",
		MetadataJSON:      `{"is_maclaw_app":true,"product_kind":"maclaw_app_skill","maclaw_app_id":"approved-only-app","publisher_email":"admin@example.com"}`,
		Version:           "1",
		VersionKey:        "enterprise_hub:skill:maclaw-app:approved-only-app@v1",
		ManifestJSON:      `{"schema":"maclaw.app.v1","privateMarker":"x_maclaw_apps","app":{"id":"approved-only-app","name":"Approved Only"}}`,
		SetCurrentVersion: true,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/capabilities/"+item.ID+"/upload-to-market", nil)
	req.SetPathValue("id", item.ID)
	rec := httptest.NewRecorder()
	AdminCapabilityUploadToMarketHandler(svc, fakeCapabilityMarketCenterStatus{
		state: &center.RegistrationState{ActiveBaseURL: "http://127.0.0.1:9", HubID: "hub-test"},
	}, t.TempDir())(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "MACLAW_APP_NOT_PUBLISHED") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func readZipFiles(t *testing.T, path string) map[string]string {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer r.Close()
	out := map[string]string{}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open entry %s: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read entry %s: %v", f.Name, err)
		}
		out[filepath.ToSlash(f.Name)] = string(data)
	}
	return out
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
