package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/cloudworkspace"
)

func TestCloudWorkspaceBandwidthQuotaRejectsUploadWithRetryAfter(t *testing.T) {
	svc, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	id := createCloudWorkspace(t, h, "m1", "bw-upload")
	acquireCloudWorkspaceLease(t, h, id, "m1")
	if _, err := svc.SaveTenantSettings(context.Background(), "t1", cloudworkspace.Settings{
		Mode:                      cloudworkspace.ModeAllUsers,
		Quota:                     5,
		BandwidthUserBytesPerHour: 10,
	}); err != nil {
		t.Fatal(err)
	}

	body := []byte("12345678") // 8 bytes, fits the 10-byte window
	put := doCloudWorkspaceBytes(t, h, http.MethodPut, "/api/v1/cloud-workspaces/"+id+"/objects/"+cloudWorkspaceSHA256Hex(body), "m1", body)
	if put.Code != http.StatusOK {
		t.Fatalf("first put=%d %s", put.Code, put.Body.String())
	}

	second := []byte("abcd") // pushes the window to 12 > 10
	over := doCloudWorkspaceBytes(t, h, http.MethodPut, "/api/v1/cloud-workspaces/"+id+"/objects/"+cloudWorkspaceSHA256Hex(second), "m1", second)
	if over.Code != http.StatusTooManyRequests || cloudWorkspaceErrCode(t, over) != "CLOUD_WORKSPACE_BANDWIDTH" {
		t.Fatalf("over-limit put=%d %s", over.Code, over.Body.String())
	}
	retryHeader := over.Header().Get("Retry-After")
	retrySeconds, err := strconv.ParseInt(retryHeader, 10, 64)
	if err != nil || retrySeconds < 1 || retrySeconds > 3600 {
		t.Fatalf("Retry-After=%q invalid", retryHeader)
	}
	var payload struct {
		RetryAfterSeconds int64 `json:"retry_after_seconds"`
	}
	if err := json.Unmarshal(over.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.RetryAfterSeconds != retrySeconds {
		t.Fatalf("body retry_after_seconds=%d header=%d", payload.RetryAfterSeconds, retrySeconds)
	}
}

func TestCloudWorkspaceBandwidthQuotaRejectsDownloadBeforeReadingBlob(t *testing.T) {
	svc, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	id := createCloudWorkspace(t, h, "m1", "bw-download")
	acquireCloudWorkspaceLease(t, h, id, "m1")

	body := []byte("download-me")
	sum := cloudWorkspaceSHA256Hex(body)
	put := doCloudWorkspaceBytes(t, h, http.MethodPut, "/api/v1/cloud-workspaces/"+id+"/objects/"+sum, "m1", body)
	if put.Code != http.StatusOK {
		t.Fatalf("put=%d %s", put.Code, put.Body.String())
	}
	// Upload accounting is disabled here (user limit only caps downloads via
	// the tenant limit below); set the tenant window just above the upload.
	if _, err := svc.SaveTenantSettings(context.Background(), "t1", cloudworkspace.Settings{
		Mode:                       cloudworkspace.ModeAllUsers,
		Quota:                      5,
		BandwidthTenantBytesPerHour: 20,
	}); err != nil {
		t.Fatal(err)
	}
	// First download fits (11 <= 20); the second would exceed 20 and must be
	// rejected before the blob is read or decrypted.
	get := doCloudWorkspaceBytes(t, h, http.MethodGet, "/api/v1/cloud-workspaces/"+id+"/objects/"+sum, "m1", nil)
	if get.Code != http.StatusOK {
		t.Fatalf("first get=%d %s", get.Code, get.Body.String())
	}
	blocked := doCloudWorkspaceBytes(t, h, http.MethodGet, "/api/v1/cloud-workspaces/"+id+"/objects/"+sum, "m1", nil)
	if blocked.Code != http.StatusTooManyRequests || cloudWorkspaceErrCode(t, blocked) != "CLOUD_WORKSPACE_BANDWIDTH" {
		t.Fatalf("second get=%d %s", blocked.Code, blocked.Body.String())
	}
}

func TestCloudWorkspaceBandwidthQuotaCoversSidecarTransfer(t *testing.T) {
	svc, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	id := createCloudWorkspace(t, h, "m1", "bw-sidecar")
	acquireCloudWorkspaceLease(t, h, id, "m1")
	if _, err := svc.SaveTenantSettings(context.Background(), "t1", cloudworkspace.Settings{
		Mode:                      cloudworkspace.ModeAllUsers,
		Quota:                     5,
		BandwidthUserBytesPerHour: 20,
	}); err != nil {
		t.Fatal(err)
	}

	payload := []byte(`{"conversation":[]}`) // 18 bytes; limit 20 fits exactly once
	put := doCloudWorkspaceBytes(t, h, http.MethodPut, "/api/v1/cloud-workspaces/"+id+"/sidecars/session.json", "m1", payload)
	if put.Code != http.StatusOK {
		t.Fatalf("sidecar put=%d %s", put.Code, put.Body.String())
	}
	over := doCloudWorkspaceBytes(t, h, http.MethodPut, "/api/v1/cloud-workspaces/"+id+"/sidecars/session.json", "m1", payload)
	if over.Code != http.StatusTooManyRequests || cloudWorkspaceErrCode(t, over) != "CLOUD_WORKSPACE_BANDWIDTH" {
		t.Fatalf("second sidecar put=%d %s", over.Code, over.Body.String())
	}
}

func TestCloudWorkspaceBandwidthDefaultUnlimitedPreservesBehavior(t *testing.T) {
	_, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	id := createCloudWorkspace(t, h, "m1", "bw-default")
	acquireCloudWorkspaceLease(t, h, id, "m1")
	for i := 0; i < 3; i++ {
		body := []byte("default-unlimited-payload")
		put := doCloudWorkspaceBytes(t, h, http.MethodPut, "/api/v1/cloud-workspaces/"+id+"/objects/"+cloudWorkspaceSHA256Hex(body), "m1", body)
		if put.Code != http.StatusOK {
			t.Fatalf("default put %d=%d %s", i, put.Code, put.Body.String())
		}
	}
}

func TestCloudWorkspaceBandwidthMetricsProjection(t *testing.T) {
	svc, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	ctx := context.Background()
	before := svc.CollectMetrics(ctx)

	id := createCloudWorkspace(t, h, "m1", "bw-metrics")
	acquireCloudWorkspaceLease(t, h, id, "m1")
	if _, err := svc.SaveTenantSettings(ctx, "t1", cloudworkspace.Settings{
		Mode:                      cloudworkspace.ModeAllUsers,
		Quota:                     5,
		BandwidthUserBytesPerHour: 10,
	}); err != nil {
		t.Fatal(err)
	}
	body := []byte("12345678")
	put := doCloudWorkspaceBytes(t, h, http.MethodPut, "/api/v1/cloud-workspaces/"+id+"/objects/"+cloudWorkspaceSHA256Hex(body), "m1", body)
	if put.Code != http.StatusOK {
		t.Fatalf("put=%d %s", put.Code, put.Body.String())
	}
	over := doCloudWorkspaceBytes(t, h, http.MethodPut, "/api/v1/cloud-workspaces/"+id+"/objects/"+cloudWorkspaceSHA256Hex([]byte("zzzz")), "m1", []byte("zzzz"))
	if over.Code != http.StatusTooManyRequests {
		t.Fatalf("over=%d %s", over.Code, over.Body.String())
	}

	global := svc.CollectMetrics(ctx)
	if global.BandwidthRejections != before.BandwidthRejections+1 {
		t.Fatalf("bandwidth_rejections before=%d after=%d", before.BandwidthRejections, global.BandwidthRejections)
	}
	if global.BandwidthWindowBytesUp < int64(len(body)) {
		t.Fatalf("global window up=%d want >= %d", global.BandwidthWindowBytesUp, len(body))
	}
	tenant := svc.CollectMetricsForTenant(ctx, "t1")
	if tenant.BandwidthWindowBytesUp != int64(len(body)) {
		t.Fatalf("tenant window up=%d want %d", tenant.BandwidthWindowBytesUp, len(body))
	}
	other := svc.CollectMetricsForTenant(ctx, "t2")
	if other.BandwidthWindowBytesUp != 0 {
		t.Fatalf("tenant t2 leaked window bytes: %+v", other)
	}
}
