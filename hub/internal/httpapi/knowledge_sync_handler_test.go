package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

func TestKnowledgeSyncNormalUserPackageFlow(t *testing.T) {
	_, identity, syncDir := newKnowledgeShareHandlerTestDeps(t)
	viewerToken, _ := issueViewerToken(t, identity, "sync-owner@example.com")
	payload := []byte("encrypted-sync-payload")

	uploadRec := doKnowledgeShareJSON(t, UploadKnowledgeSyncPackageHandler(identity, syncDir), http.MethodPut, "/api/knowledge/sync/package", viewerToken, map[string]any{
		"package_id":            "ksync_test",
		"package_version":       1,
		"compressed_size_bytes": 123,
		"payload_base64":        base64.StdEncoding.EncodeToString(payload),
		"encryption":            map[string]any{"algorithm": "AES-256-GCM", "kdf": "scrypt", "salt": "s", "nonce": "n"},
	})
	if uploadRec.Code != http.StatusOK {
		t.Fatalf("upload status = %d body=%s", uploadRec.Code, uploadRec.Body.String())
	}
	var uploaded KnowledgeSyncView
	if err := json.Unmarshal(uploadRec.Body.Bytes(), &uploaded); err != nil {
		t.Fatalf("decode upload: %v", err)
	}
	if uploaded.ServiceStatus != "normal" || uploaded.LimitBytes != knowledgeSyncNormalLimitBytes || uploaded.ExpiresAt == "" {
		t.Fatalf("uploaded status = %+v", uploaded)
	}

	statusRec := doKnowledgeShareJSON(t, KnowledgeSyncStatusHandler(identity, syncDir), http.MethodGet, "/api/knowledge/sync/status", viewerToken, nil)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", statusRec.Code, statusRec.Body.String())
	}
	var status KnowledgeSyncView
	if err := json.Unmarshal(statusRec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !status.HasPackage || status.StoredSizeBytes != int64(len(payload)) {
		t.Fatalf("status view = %+v", status)
	}

	downloadRec := doKnowledgeShareJSON(t, DownloadKnowledgeSyncPackageHandler(identity, syncDir), http.MethodGet, "/api/knowledge/sync/package", viewerToken, nil)
	if downloadRec.Code != http.StatusOK || downloadRec.Body.String() != string(payload) {
		t.Fatalf("download = %d %q", downloadRec.Code, downloadRec.Body.String())
	}

	deleteRec := doKnowledgeShareJSON(t, DeleteKnowledgeSyncPackageHandler(identity, syncDir), http.MethodDelete, "/api/knowledge/sync/package", viewerToken, nil)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete = %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
}

func TestKnowledgeSyncOfficialExpiredIsReadOnlyButDownloadable(t *testing.T) {
	_, identity, syncDir := newKnowledgeShareHandlerTestDeps(t)
	viewerToken, _ := issueViewerToken(t, identity, "expired-sync@example.com")
	ac := llmservice.NewTenantLLMAccessControl(nil)
	ac.UpdateFromHeartbeat("tenant_default", &llmservice.TenantAuthorizationStatus{
		TenantID: "tenant_default",
		Authorizations: []llmservice.AuthorizationSummary{{
			ServiceGroupID: llmservice.MaClawOfficialServiceGroupID,
			Status:         "expired",
			Active:         false,
			ExpiresAt:      time.Now().Add(-time.Hour).Format(time.RFC3339),
		}},
	})
	previous := GetMaClawModule()
	SetMaClawModule(&llmservice.MaClawModule{AccessCtrl: ac})
	t.Cleanup(func() { SetMaClawModule(previous) })

	uploadRec := doKnowledgeShareJSON(t, UploadKnowledgeSyncPackageHandler(identity, syncDir), http.MethodPut, "/api/knowledge/sync/package", viewerToken, map[string]any{
		"payload_base64": base64.StdEncoding.EncodeToString([]byte("new")),
	})
	if uploadRec.Code != http.StatusPaymentRequired {
		t.Fatalf("expired upload = %d body=%s", uploadRec.Code, uploadRec.Body.String())
	}

	SetMaClawModule(previous)
	okRec := doKnowledgeShareJSON(t, UploadKnowledgeSyncPackageHandler(identity, syncDir), http.MethodPut, "/api/knowledge/sync/package", viewerToken, map[string]any{
		"package_id":            "ksync_old",
		"package_version":       1,
		"compressed_size_bytes": 3,
		"payload_base64":        base64.StdEncoding.EncodeToString([]byte("old")),
		"encryption":            map[string]any{"algorithm": "AES-256-GCM"},
	})
	if okRec.Code != http.StatusOK {
		t.Fatalf("seed upload = %d body=%s", okRec.Code, okRec.Body.String())
	}
	SetMaClawModule(&llmservice.MaClawModule{AccessCtrl: ac})

	downloadRec := doKnowledgeShareJSON(t, DownloadKnowledgeSyncPackageHandler(identity, syncDir), http.MethodGet, "/api/knowledge/sync/package", viewerToken, nil)
	if downloadRec.Code != http.StatusOK || downloadRec.Body.String() != "old" {
		t.Fatalf("expired download = %d body=%s", downloadRec.Code, downloadRec.Body.String())
	}
}

func TestKnowledgeSyncOfficialFutureExpiryIsActive(t *testing.T) {
	_, identity, syncDir := newKnowledgeShareHandlerTestDeps(t)
	viewerToken, _ := issueViewerToken(t, identity, "active-sync@example.com")
	ac := llmservice.NewTenantLLMAccessControl(nil)
	ac.UpdateFromHeartbeat("tenant_default", &llmservice.TenantAuthorizationStatus{
		TenantID: "tenant_default",
		Authorizations: []llmservice.AuthorizationSummary{{
			ServiceGroupID:   llmservice.MaClawOfficialServiceGroupID,
			Status:           "valid",
			Active:           false,
			CreditsTotal:     81001,
			CreditsUsed:      16413,
			CreditsRemaining: 64588,
			ExpiresAt:        time.Now().AddDate(0, 0, 30).Format(time.RFC3339),
		}},
	})
	previous := GetMaClawModule()
	SetMaClawModule(&llmservice.MaClawModule{AccessCtrl: ac})
	t.Cleanup(func() { SetMaClawModule(previous) })

	statusRec := doKnowledgeShareJSON(t, KnowledgeSyncStatusHandler(identity, syncDir), http.MethodGet, "/api/knowledge/sync/status", viewerToken, nil)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", statusRec.Code, statusRec.Body.String())
	}
	var status KnowledgeSyncView
	if err := json.Unmarshal(statusRec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.ServiceStatus != "official_active" || status.LimitBytes != knowledgeSyncOfficialLimitBytes || status.ReadonlyReason != "" {
		t.Fatalf("future-expiry official status = %+v", status)
	}

	uploadRec := doKnowledgeShareJSON(t, UploadKnowledgeSyncPackageHandler(identity, syncDir), http.MethodPut, "/api/knowledge/sync/package", viewerToken, map[string]any{
		"package_id":            "ksync_active",
		"package_version":       1,
		"compressed_size_bytes": 4,
		"payload_base64":        base64.StdEncoding.EncodeToString([]byte("data")),
		"encryption":            map[string]any{"algorithm": "AES-256-GCM"},
	})
	if uploadRec.Code != http.StatusOK {
		t.Fatalf("active official upload = %d body=%s", uploadRec.Code, uploadRec.Body.String())
	}
}

func TestKnowledgeSyncUsesLocalTenantHeaderForOfficialStatus(t *testing.T) {
	_, identity, syncDir := newKnowledgeShareHandlerTestDeps(t)
	viewerToken, _ := issueViewerToken(t, identity, "remote-tenant-sync@example.com")
	ac := llmservice.NewTenantLLMAccessControl(nil)
	ac.UpdateFromHeartbeat("tenant_remote", &llmservice.TenantAuthorizationStatus{
		TenantID: "tenant_remote",
		Authorizations: []llmservice.AuthorizationSummary{{
			ServiceGroupID: llmservice.MaClawOfficialServiceGroupID,
			Status:         "valid",
			ExpiresAt:      time.Now().AddDate(0, 0, 30).Format(time.RFC3339),
		}},
	})
	previous := GetMaClawModule()
	SetMaClawModule(&llmservice.MaClawModule{AccessCtrl: ac})
	t.Cleanup(func() { SetMaClawModule(previous) })

	req := httptest.NewRequest(http.MethodGet, "/api/knowledge/sync/status", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("X-Maclaw-Tenant-ID", "tenant_remote")
	req.Header.Set("X-Maclaw-User-Email", "remote-tenant-sync@example.com")
	rec := httptest.NewRecorder()
	KnowledgeSyncStatusHandler(identity, syncDir)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var status KnowledgeSyncView
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.ServiceStatus != "official_active" || status.LimitBytes != knowledgeSyncOfficialLimitBytes {
		t.Fatalf("local tenant official status = %+v", status)
	}
}

func TestKnowledgeSyncIgnoresLocalTenantHeaderForDifferentEmail(t *testing.T) {
	_, identity, syncDir := newKnowledgeShareHandlerTestDeps(t)
	viewerToken, _ := issueViewerToken(t, identity, "owner-sync@example.com")
	ac := llmservice.NewTenantLLMAccessControl(nil)
	ac.UpdateFromHeartbeat("tenant_remote", &llmservice.TenantAuthorizationStatus{
		TenantID: "tenant_remote",
		Authorizations: []llmservice.AuthorizationSummary{{
			ServiceGroupID: llmservice.MaClawOfficialServiceGroupID,
			Status:         "valid",
			ExpiresAt:      time.Now().AddDate(0, 0, 30).Format(time.RFC3339),
		}},
	})
	previous := GetMaClawModule()
	SetMaClawModule(&llmservice.MaClawModule{AccessCtrl: ac})
	t.Cleanup(func() { SetMaClawModule(previous) })

	req := httptest.NewRequest(http.MethodGet, "/api/knowledge/sync/status", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("X-Maclaw-Tenant-ID", "tenant_remote")
	req.Header.Set("X-Maclaw-User-Email", "other@example.com")
	rec := httptest.NewRecorder()
	KnowledgeSyncStatusHandler(identity, syncDir)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var status KnowledgeSyncView
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.ServiceStatus != "normal" || status.LimitBytes != knowledgeSyncNormalLimitBytes {
		t.Fatalf("mismatched local tenant header should be ignored: %+v", status)
	}
}

func TestKnowledgeSyncOfficialPastExpiryOverridesValidStatus(t *testing.T) {
	_, identity, syncDir := newKnowledgeShareHandlerTestDeps(t)
	viewerToken, _ := issueViewerToken(t, identity, "stale-valid-sync@example.com")
	ac := llmservice.NewTenantLLMAccessControl(nil)
	ac.UpdateFromHeartbeat("tenant_default", &llmservice.TenantAuthorizationStatus{
		TenantID: "tenant_default",
		Authorizations: []llmservice.AuthorizationSummary{{
			ServiceGroupID: llmservice.MaClawOfficialServiceGroupID,
			Status:         "valid",
			Active:         true,
			ExpiresAt:      time.Now().Add(-time.Hour).Format(time.RFC3339),
		}},
	})
	previous := GetMaClawModule()
	SetMaClawModule(&llmservice.MaClawModule{AccessCtrl: ac})
	t.Cleanup(func() { SetMaClawModule(previous) })

	statusRec := doKnowledgeShareJSON(t, KnowledgeSyncStatusHandler(identity, syncDir), http.MethodGet, "/api/knowledge/sync/status", viewerToken, nil)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", statusRec.Code, statusRec.Body.String())
	}
	var status KnowledgeSyncView
	if err := json.Unmarshal(statusRec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.ServiceStatus != "official_expired" || status.ReadonlyReason == "" {
		t.Fatalf("past-expiry official status = %+v", status)
	}
}

func TestKnowledgeSyncRefreshesStaleExpiredOfficialStatus(t *testing.T) {
	_, identity, syncDir := newKnowledgeShareHandlerTestDeps(t)
	viewerToken, _ := issueViewerToken(t, identity, "refresh-sync@example.com")
	expiresAt := time.Now().AddDate(0, 0, 30).Format(time.RFC3339)
	center := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/llm/v1/authorization" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("tenant_id"); got != "tenant_default" {
			t.Fatalf("tenant_id = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(llmservice.TenantAuthorizationStatus{
			HubID:    "hub_test",
			TenantID: "tenant_default",
			Authorizations: []llmservice.AuthorizationSummary{{
				ServiceGroupID:   llmservice.MaClawOfficialServiceGroupID,
				Status:           "valid",
				ExpiresAt:        expiresAt,
				CreditsTotal:     81001,
				CreditsUsed:      16413,
				CreditsRemaining: 64588,
			}},
		})
	}))
	defer center.Close()

	client := llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
		HubCenterURL: center.URL,
		HubID:        "hub_test",
		MachineToken: "machine_token",
	})
	ac := llmservice.NewTenantLLMAccessControl(client)
	ac.UpdateFromHeartbeat("tenant_default", &llmservice.TenantAuthorizationStatus{
		TenantID: "tenant_default",
		Authorizations: []llmservice.AuthorizationSummary{{
			ServiceGroupID: llmservice.MaClawOfficialServiceGroupID,
			Status:         "expired",
			Active:         false,
			ExpiresAt:      time.Now().Add(-time.Hour).Format(time.RFC3339),
		}},
	})
	previous := GetMaClawModule()
	SetMaClawModule(&llmservice.MaClawModule{Client: client, AccessCtrl: ac})
	t.Cleanup(func() { SetMaClawModule(previous) })

	statusRec := doKnowledgeShareJSON(t, KnowledgeSyncStatusHandler(identity, syncDir), http.MethodGet, "/api/knowledge/sync/status", viewerToken, nil)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", statusRec.Code, statusRec.Body.String())
	}
	var status KnowledgeSyncView
	if err := json.Unmarshal(statusRec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.ServiceStatus != "official_active" || status.LimitBytes != knowledgeSyncOfficialLimitBytes {
		t.Fatalf("refreshed official status = %+v", status)
	}

	uploadRec := doKnowledgeShareJSON(t, UploadKnowledgeSyncPackageHandler(identity, syncDir), http.MethodPut, "/api/knowledge/sync/package", viewerToken, map[string]any{
		"package_id":            "ksync_refreshed",
		"package_version":       1,
		"compressed_size_bytes": 4,
		"payload_base64":        base64.StdEncoding.EncodeToString([]byte("data")),
		"encryption":            map[string]any{"algorithm": "AES-256-GCM"},
	})
	if uploadRec.Code != http.StatusOK {
		t.Fatalf("upload after refresh = %d body=%s", uploadRec.Code, uploadRec.Body.String())
	}
}

func TestKnowledgeSyncUnrelatedAuthorizationStaysTemporary(t *testing.T) {
	_, identity, syncDir := newKnowledgeShareHandlerTestDeps(t)
	viewerToken, _ := issueViewerToken(t, identity, "ordinary-sync@example.com")
	ac := llmservice.NewTenantLLMAccessControl(nil)
	ac.UpdateFromHeartbeat("tenant_default", &llmservice.TenantAuthorizationStatus{
		TenantID: "tenant_default",
		Authorizations: []llmservice.AuthorizationSummary{{
			ServiceGroupID: "other_service",
			Status:         "active",
			Active:         true,
			Source:         "enterprise",
		}},
	})
	previous := GetMaClawModule()
	SetMaClawModule(&llmservice.MaClawModule{AccessCtrl: ac})
	t.Cleanup(func() { SetMaClawModule(previous) })

	statusRec := doKnowledgeShareJSON(t, KnowledgeSyncStatusHandler(identity, syncDir), http.MethodGet, "/api/knowledge/sync/status", viewerToken, nil)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", statusRec.Code, statusRec.Body.String())
	}
	var status KnowledgeSyncView
	if err := json.Unmarshal(statusRec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.ServiceStatus != "normal" || status.LimitBytes != knowledgeSyncNormalLimitBytes || status.RetentionDays != knowledgeSyncNormalRetentionDays {
		t.Fatalf("status with unrelated authorization = %+v", status)
	}

	uploadRec := doKnowledgeShareJSON(t, UploadKnowledgeSyncPackageHandler(identity, syncDir), http.MethodPut, "/api/knowledge/sync/package", viewerToken, map[string]any{
		"package_id":            "ksync_normal_auth",
		"package_version":       1,
		"compressed_size_bytes": 4,
		"payload_base64":        base64.StdEncoding.EncodeToString([]byte("data")),
		"encryption":            map[string]any{"algorithm": "AES-256-GCM"},
	})
	if uploadRec.Code != http.StatusOK {
		t.Fatalf("upload with unrelated authorization = %d body=%s", uploadRec.Code, uploadRec.Body.String())
	}
}

func TestKnowledgeSyncUsesOnePackagePerEmail(t *testing.T) {
	_, identity, syncDir := newKnowledgeShareHandlerTestDeps(t)
	firstToken, _ := issueViewerToken(t, identity, "same-sync@example.com")
	secondToken, _ := issueViewerToken(t, identity, "same-sync@example.com")
	payload := []byte("email-owned")

	uploadRec := doKnowledgeShareJSON(t, UploadKnowledgeSyncPackageHandler(identity, syncDir), http.MethodPut, "/api/knowledge/sync/package", firstToken, map[string]any{
		"package_id":            "ksync_email",
		"package_version":       1,
		"compressed_size_bytes": int64(len(payload)),
		"payload_base64":        base64.StdEncoding.EncodeToString(payload),
		"encryption":            map[string]any{"algorithm": "AES-256-GCM"},
	})
	if uploadRec.Code != http.StatusOK {
		t.Fatalf("upload = %d body=%s", uploadRec.Code, uploadRec.Body.String())
	}

	statusRec := doKnowledgeShareJSON(t, KnowledgeSyncStatusHandler(identity, syncDir), http.MethodGet, "/api/knowledge/sync/status", secondToken, nil)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", statusRec.Code, statusRec.Body.String())
	}
	var status KnowledgeSyncView
	if err := json.Unmarshal(statusRec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !status.HasPackage || status.PackageID != "ksync_email" {
		t.Fatalf("same email status = %+v", status)
	}

	downloadRec := doKnowledgeShareJSON(t, DownloadKnowledgeSyncPackageHandler(identity, syncDir), http.MethodGet, "/api/knowledge/sync/package", secondToken, nil)
	if downloadRec.Code != http.StatusOK || downloadRec.Body.String() != string(payload) {
		t.Fatalf("download = %d body=%s", downloadRec.Code, downloadRec.Body.String())
	}
}

func TestKnowledgeSyncUsesOnePackagePerEmailAcrossTenants(t *testing.T) {
	_, identity, syncDir := newKnowledgeShareHandlerTestDeps(t)
	firstToken := issueViewerTokenForTenant(t, identity, "tenant_a", "same-cross-tenant@example.com")
	secondToken := issueViewerTokenForTenant(t, identity, "tenant_b", "same-cross-tenant@example.com")
	payload := []byte("email-owned-cross-tenant")

	uploadRec := doKnowledgeShareJSON(t, UploadKnowledgeSyncPackageHandler(identity, syncDir), http.MethodPut, "/api/knowledge/sync/package", firstToken, map[string]any{
		"package_id":            "ksync_email_cross_tenant",
		"package_version":       1,
		"compressed_size_bytes": int64(len(payload)),
		"payload_base64":        base64.StdEncoding.EncodeToString(payload),
		"encryption":            map[string]any{"algorithm": "AES-256-GCM"},
	})
	if uploadRec.Code != http.StatusOK {
		t.Fatalf("upload = %d body=%s", uploadRec.Code, uploadRec.Body.String())
	}

	statusRec := doKnowledgeShareJSON(t, KnowledgeSyncStatusHandler(identity, syncDir), http.MethodGet, "/api/knowledge/sync/status", secondToken, nil)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", statusRec.Code, statusRec.Body.String())
	}
	var status KnowledgeSyncView
	if err := json.Unmarshal(statusRec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !status.HasPackage || status.PackageID != "ksync_email_cross_tenant" {
		t.Fatalf("same email cross tenant status = %+v", status)
	}

	downloadRec := doKnowledgeShareJSON(t, DownloadKnowledgeSyncPackageHandler(identity, syncDir), http.MethodGet, "/api/knowledge/sync/package", secondToken, nil)
	if downloadRec.Code != http.StatusOK || downloadRec.Body.String() != string(payload) {
		t.Fatalf("download = %d body=%s", downloadRec.Code, downloadRec.Body.String())
	}
}

func TestKnowledgeSyncUsesOnePackagePerEmailCaseInsensitive(t *testing.T) {
	_, identity, syncDir := newKnowledgeShareHandlerTestDeps(t)
	firstToken := issueViewerTokenForTenant(t, identity, "tenant_a", "Case.Email@example.com")
	secondToken := issueViewerTokenForTenant(t, identity, "tenant_b", "case.email@example.com")
	payload := []byte("email-owned-case-insensitive")

	uploadRec := doKnowledgeShareJSON(t, UploadKnowledgeSyncPackageHandler(identity, syncDir), http.MethodPut, "/api/knowledge/sync/package", firstToken, map[string]any{
		"package_id":            "ksync_email_case",
		"package_version":       1,
		"compressed_size_bytes": int64(len(payload)),
		"payload_base64":        base64.StdEncoding.EncodeToString(payload),
		"encryption":            map[string]any{"algorithm": "AES-256-GCM"},
	})
	if uploadRec.Code != http.StatusOK {
		t.Fatalf("upload = %d body=%s", uploadRec.Code, uploadRec.Body.String())
	}

	statusRec := doKnowledgeShareJSON(t, KnowledgeSyncStatusHandler(identity, syncDir), http.MethodGet, "/api/knowledge/sync/status", secondToken, nil)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", statusRec.Code, statusRec.Body.String())
	}
	var status KnowledgeSyncView
	if err := json.Unmarshal(statusRec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !status.HasPackage || status.PackageID != "ksync_email_case" {
		t.Fatalf("same email with different case status = %+v", status)
	}

	downloadRec := doKnowledgeShareJSON(t, DownloadKnowledgeSyncPackageHandler(identity, syncDir), http.MethodGet, "/api/knowledge/sync/package", secondToken, nil)
	if downloadRec.Code != http.StatusOK || downloadRec.Body.String() != string(payload) {
		t.Fatalf("download = %d body=%s", downloadRec.Code, downloadRec.Body.String())
	}
}

func TestKnowledgeSyncMigratesLegacyEmailCaseDirectory(t *testing.T) {
	_, identity, syncDir := newKnowledgeShareHandlerTestDeps(t)
	viewerToken := issueViewerTokenForTenant(t, identity, "tenant_case", "Case.Legacy@example.com")
	payload := []byte("legacy-email-case-dir")
	legacyDir := filepath.Join(syncDir, "_email", "Case.Legacy@example.com")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "package.mksync"), payload, 0o600); err != nil {
		t.Fatalf("write legacy package: %v", err)
	}
	metaBytes, err := json.Marshal(knowledgeSyncMeta{
		OwnerUserEmail:      "Case.Legacy@example.com",
		TenantID:            "tenant_case",
		PackageID:           "ksync_legacy_case",
		PackageVersion:      1,
		CompressedSizeBytes: int64(len(payload)),
		StoredSizeBytes:     int64(len(payload)),
		CreatedAt:           time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:           time.Now().UTC().Format(time.RFC3339),
		ServiceStatus:       "normal",
	})
	if err != nil {
		t.Fatalf("marshal legacy meta: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "meta.json"), metaBytes, 0o600); err != nil {
		t.Fatalf("write legacy meta: %v", err)
	}

	statusRec := doKnowledgeShareJSON(t, KnowledgeSyncStatusHandler(identity, syncDir), http.MethodGet, "/api/knowledge/sync/status", viewerToken, nil)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", statusRec.Code, statusRec.Body.String())
	}
	var status KnowledgeSyncView
	if err := json.Unmarshal(statusRec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !status.HasPackage || status.PackageID != "ksync_legacy_case" {
		t.Fatalf("legacy email case status = %+v", status)
	}

	canonicalPackage := filepath.Join(syncDir, "_email", "case.legacy@example.com", "package.mksync")
	migratedPayload, err := os.ReadFile(canonicalPackage)
	if err != nil {
		t.Fatalf("canonical package was not readable: %v", err)
	}
	if string(migratedPayload) != string(payload) {
		t.Fatalf("canonical package payload = %q", string(migratedPayload))
	}

	downloadRec := doKnowledgeShareJSON(t, DownloadKnowledgeSyncPackageHandler(identity, syncDir), http.MethodGet, "/api/knowledge/sync/package", viewerToken, nil)
	if downloadRec.Code != http.StatusOK || downloadRec.Body.String() != string(payload) {
		t.Fatalf("download = %d body=%s", downloadRec.Code, downloadRec.Body.String())
	}
}
