package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
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
