package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/cloudworkspace"
)

func TestGetCloudWorkspaceMetricsJSONShape(t *testing.T) {
	svc, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	_ = createCloudWorkspace(t, h, "m1", "A")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cloud-workspaces/metrics", nil)
	GetCloudWorkspaceMetricsAdminHandler(svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got cloudworkspace.Metrics
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.TenantsEnabled != 1 {
		t.Fatalf("tenants_enabled=%d want 1 body=%s", got.TenantsEnabled, rec.Body.String())
	}
	if got.VolumeFreeBytes < 0 {
		t.Fatalf("volume_free_bytes=%d", got.VolumeFreeBytes)
	}
}

func TestCloudWorkspaceMetricsCountersFromHandlers(t *testing.T) {
	svc, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 1, nil)
	before := svc.CollectMetrics(httptest.NewRequest(http.MethodGet, "/", nil).Context())

	id := createCloudWorkspace(t, h, "m1", "A")
	blocked := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces", "m1", "secret", map[string]any{"name": "B"})
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("quota=%d %s", blocked.Code, blocked.Body.String())
	}
	acquireCloudWorkspaceLease(t, h, id, "m1")
	conflict := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces/"+id+"/leases", "m1b", "secret", map[string]any{"force": false})
	if conflict.Code != http.StatusConflict {
		t.Fatalf("lease conflict=%d %s", conflict.Code, conflict.Body.String())
	}
	body := []byte("metric-bytes")
	sum := cloudWorkspaceSHA256Hex(body)
	put := doCloudWorkspaceBytes(t, h, http.MethodPut, "/api/v1/cloud-workspaces/"+id+"/objects/"+sum, "m1", body)
	if put.Code != http.StatusOK {
		t.Fatalf("put=%d %s", put.Code, put.Body.String())
	}
	get := doCloudWorkspaceBytes(t, h, http.MethodGet, "/api/v1/cloud-workspaces/"+id+"/objects/"+sum, "m1", nil)
	if get.Code != http.StatusOK {
		t.Fatalf("get=%d %s", get.Code, get.Body.String())
	}

	afterRec := doCloudWorkspaceRequest(t, h, http.MethodGet, "/api/admin/cloud-workspaces/metrics", "", "", nil)
	if afterRec.Code != http.StatusOK {
		t.Fatalf("metrics=%d %s", afterRec.Code, afterRec.Body.String())
	}
	var after cloudworkspace.Metrics
	if err := json.Unmarshal(afterRec.Body.Bytes(), &after); err != nil {
		t.Fatal(err)
	}
	if after.QuotaRejections <= before.QuotaRejections {
		t.Fatalf("quota_rejections before=%d after=%d", before.QuotaRejections, after.QuotaRejections)
	}
	if after.LeaseConflicts <= before.LeaseConflicts {
		t.Fatalf("lease_conflicts before=%d after=%d", before.LeaseConflicts, after.LeaseConflicts)
	}
	if after.SyncBytesUp < before.SyncBytesUp+uint64(len(body)) {
		t.Fatalf("sync_bytes_up before=%d after=%d", before.SyncBytesUp, after.SyncBytesUp)
	}
	if after.SyncBytesDown < before.SyncBytesDown+uint64(len(body)) {
		t.Fatalf("sync_bytes_down before=%d after=%d", before.SyncBytesDown, after.SyncBytesDown)
	}
	if after.OpenLeases < 1 {
		t.Fatalf("open_leases=%d", after.OpenLeases)
	}
}
