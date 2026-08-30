package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/cloudworkspace"
)

func acquireCloudWorkspaceLease(t *testing.T, h http.Handler, id, machineID string) {
	t.Helper()
	rec := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces/"+id+"/leases", machineID, "secret", map[string]any{"force": false})
	if rec.Code != http.StatusOK {
		t.Fatalf("lease=%d %s", rec.Code, rec.Body.String())
	}
}

func doCloudWorkspaceBytes(t *testing.T, h http.Handler, method, path, machineID string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("X-Machine-ID", machineID)
	if body != nil {
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func cloudWorkspaceSHA256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func TestCloudWorkspaceManifestAndObjectLeaseRequired(t *testing.T) {
	_, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	id := createCloudWorkspace(t, h, "m1", "A")
	rec := doCloudWorkspaceRequest(t, h, http.MethodGet, "/api/v1/cloud-workspaces/"+id+"/manifest", "m1", "secret", nil)
	if rec.Code != http.StatusForbidden || cloudWorkspaceErrCode(t, rec) != "CLOUD_WORKSPACE_LEASE_REQUIRED" {
		t.Fatalf("get manifest=%d %s", rec.Code, rec.Body.String())
	}
	putMan := doCloudWorkspaceRequest(t, h, http.MethodPut, "/api/v1/cloud-workspaces/"+id+"/manifest", "m1", "secret", map[string]any{
		"if_match_revision": "",
		"entries":           []any{},
	})
	if putMan.Code != http.StatusForbidden || cloudWorkspaceErrCode(t, putMan) != "CLOUD_WORKSPACE_LEASE_REQUIRED" {
		t.Fatalf("put manifest=%d %s", putMan.Code, putMan.Body.String())
	}
	body := []byte("hello")
	sum := cloudWorkspaceSHA256Hex(body)
	put := doCloudWorkspaceBytes(t, h, http.MethodPut, "/api/v1/cloud-workspaces/"+id+"/objects/"+sum, "m1", body)
	if put.Code != http.StatusForbidden || cloudWorkspaceErrCode(t, put) != "CLOUD_WORKSPACE_LEASE_REQUIRED" {
		t.Fatalf("put object=%d %s", put.Code, put.Body.String())
	}
}

func TestCloudWorkspaceSyncRequiresGrantEvenForOwner(t *testing.T) {
	svc, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	id := createCloudWorkspace(t, h, "m1", "A")
	acquireCloudWorkspaceLease(t, h, id, "m1")
	if _, err := svc.SaveTenantSettings(context.Background(), "t1", cloudworkspace.Settings{Mode: cloudworkspace.ModeOff, Quota: 5}); err != nil {
		t.Fatal(err)
	}
	rec := doCloudWorkspaceRequest(t, h, http.MethodGet, "/api/v1/cloud-workspaces/"+id+"/manifest", "m1", "secret", nil)
	if rec.Code != http.StatusForbidden || cloudWorkspaceErrCode(t, rec) != "CLOUD_WORKSPACE_FORBIDDEN" {
		t.Fatalf("disabled grant=%d %s", rec.Code, rec.Body.String())
	}
}

func TestCloudWorkspaceObjectSHA256MustBeLowerHex(t *testing.T) {
	_, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	id := createCloudWorkspace(t, h, "m1", "A")
	acquireCloudWorkspaceLease(t, h, id, "m1")
	upper := strings.Repeat("A", 64)
	rec := doCloudWorkspaceBytes(t, h, http.MethodPut, "/api/v1/cloud-workspaces/"+id+"/objects/"+upper, "m1", []byte("x"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("uppercase=%d %s", rec.Code, rec.Body.String())
	}
	short := strings.Repeat("a", 8)
	rec = doCloudWorkspaceBytes(t, h, http.MethodGet, "/api/v1/cloud-workspaces/"+id+"/objects/"+short, "m1", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("short=%d %s", rec.Code, rec.Body.String())
	}
}

func TestCloudWorkspaceManifestRejectsPathTraversal(t *testing.T) {
	_, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	id := createCloudWorkspace(t, h, "m1", "A")
	acquireCloudWorkspaceLease(t, h, id, "m1")
	for _, p := range []string{"../secret", "/etc/passwd", `foo\bar`, "foo/../bar"} {
		rec := doCloudWorkspaceRequest(t, h, http.MethodPut, "/api/v1/cloud-workspaces/"+id+"/manifest", "m1", "secret", map[string]any{
			"if_match_revision": "",
			"entries":           []map[string]any{{"path": p, "sha256": strings.Repeat("a", 64), "size": 1}},
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("path %q status=%d %s", p, rec.Code, rec.Body.String())
		}
	}
}

func TestCloudWorkspaceObjectPutIsPlaintextAndManifestUpdatesUsage(t *testing.T) {
	_, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	id := createCloudWorkspace(t, h, "m1", "A")
	acquireCloudWorkspaceLease(t, h, id, "m1")
	body := []byte("hello cloud")
	sum := cloudWorkspaceSHA256Hex(body)
	put := doCloudWorkspaceBytes(t, h, http.MethodPut, "/api/v1/cloud-workspaces/"+id+"/objects/"+sum, "m1", body)
	if put.Code != http.StatusOK {
		t.Fatalf("put=%d %s", put.Code, put.Body.String())
	}
	again := doCloudWorkspaceBytes(t, h, http.MethodPut, "/api/v1/cloud-workspaces/"+id+"/objects/"+sum, "m1", body)
	if again.Code != http.StatusOK {
		t.Fatalf("idempotent=%d %s", again.Code, again.Body.String())
	}
	ent := doCloudWorkspaceRequest(t, h, http.MethodGet, "/api/v1/cloud-workspaces/entitlement", "m1", "secret", nil)
	var before cloudworkspace.Entitlement
	if err := json.Unmarshal(ent.Body.Bytes(), &before); err != nil {
		t.Fatal(err)
	}
	if len(before.Workspaces) != 1 || before.Workspaces[0].UsedBytes != 0 {
		t.Fatalf("used_bytes after object put=%+v", before.Workspaces)
	}

	wrong := doCloudWorkspaceRequest(t, h, http.MethodPut, "/api/v1/cloud-workspaces/"+id+"/manifest", "m1", "secret", map[string]any{
		"if_match_revision": "nope",
		"entries":           []map[string]any{{"path": "a.txt", "sha256": sum, "size": len(body)}},
	})
	if wrong.Code != http.StatusConflict || cloudWorkspaceErrCode(t, wrong) != "CLOUD_WORKSPACE_REVISION_CONFLICT" {
		t.Fatalf("revision=%d %s", wrong.Code, wrong.Body.String())
	}

	man := doCloudWorkspaceRequest(t, h, http.MethodPut, "/api/v1/cloud-workspaces/"+id+"/manifest", "m1", "secret", map[string]any{
		"if_match_revision": "",
		"entries":           []map[string]any{{"path": "a.txt", "sha256": sum, "size": len(body)}},
	})
	if man.Code != http.StatusOK {
		t.Fatalf("manifest=%d %s", man.Code, man.Body.String())
	}
	var got cloudworkspace.Manifest
	if err := json.Unmarshal(man.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Revision == "" || len(got.Entries) != 1 || got.Entries[0].Path != "a.txt" {
		t.Fatalf("manifest body=%+v", got)
	}
	if _, ok := recHasMtime(man.Body.Bytes()); ok {
		t.Fatalf("mtime leaked: %s", man.Body.String())
	}

	ent = doCloudWorkspaceRequest(t, h, http.MethodGet, "/api/v1/cloud-workspaces/entitlement", "m1", "secret", nil)
	var after cloudworkspace.Entitlement
	if err := json.Unmarshal(ent.Body.Bytes(), &after); err != nil {
		t.Fatal(err)
	}
	if after.Workspaces[0].UsedBytes != int64(len(body)) {
		t.Fatalf("used_bytes after manifest=%d", after.Workspaces[0].UsedBytes)
	}

	get := doCloudWorkspaceBytes(t, h, http.MethodGet, "/api/v1/cloud-workspaces/"+id+"/objects/"+sum, "m1", nil)
	if get.Code != http.StatusOK || !bytes.Equal(get.Body.Bytes(), body) {
		t.Fatalf("get object=%d %q", get.Code, get.Body.Bytes())
	}

	mismatch := doCloudWorkspaceBytes(t, h, http.MethodPut, "/api/v1/cloud-workspaces/"+id+"/objects/"+sum, "m1", []byte("other"))
	if mismatch.Code != http.StatusBadRequest {
		t.Fatalf("hash mismatch=%d %s", mismatch.Code, mismatch.Body.String())
	}
}

func recHasMtime(raw []byte) (any, bool) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, false
	}
	if _, ok := payload["mtime"]; ok {
		return payload["mtime"], true
	}
	entries, _ := payload["entries"].([]any)
	for _, e := range entries {
		m, _ := e.(map[string]any)
		if _, ok := m["mtime"]; ok {
			return m["mtime"], true
		}
	}
	return nil, false
}

func TestCloudWorkspaceChunkComplete(t *testing.T) {
	_, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	id := createCloudWorkspace(t, h, "m1", "A")
	acquireCloudWorkspaceLease(t, h, id, "m1")
	plain := []byte("abcdefghij")
	sum := cloudWorkspaceSHA256Hex(plain)
	base := "/api/v1/cloud-workspaces/" + id + "/objects/" + sum
	if rec := doCloudWorkspaceBytes(t, h, http.MethodPut, base+"/chunks/0", "m1", plain[:5]); rec.Code != http.StatusOK {
		t.Fatalf("chunk0=%d %s", rec.Code, rec.Body.String())
	}
	if rec := doCloudWorkspaceBytes(t, h, http.MethodPut, base+"/chunks/1", "m1", plain[5:]); rec.Code != http.StatusOK {
		t.Fatalf("chunk1=%d %s", rec.Code, rec.Body.String())
	}
	done := doCloudWorkspaceRequest(t, h, http.MethodPost, base+"/complete", "m1", "secret", nil)
	if done.Code != http.StatusOK {
		t.Fatalf("complete=%d %s", done.Code, done.Body.String())
	}
	get := doCloudWorkspaceBytes(t, h, http.MethodGet, base, "m1", nil)
	if get.Code != http.StatusOK || !bytes.Equal(get.Body.Bytes(), plain) {
		t.Fatalf("get=%d %q", get.Code, get.Body.Bytes())
	}
}

func TestCloudWorkspaceSidecarLeaseGrantAndAllowlist(t *testing.T) {
	svc, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	id := createCloudWorkspace(t, h, "m1", "A")
	body := []byte(`{"name":"标书","mode":"coding_dev","tag":"cloud_workspace:` + id + `"}`)
	path := "/api/v1/cloud-workspaces/" + id + "/sidecars/" + cloudworkspace.SidecarTask
	if rec := doCloudWorkspaceBytes(t, h, http.MethodPut, path, "m1", body); rec.Code != http.StatusForbidden || cloudWorkspaceErrCode(t, rec) != "CLOUD_WORKSPACE_LEASE_REQUIRED" {
		t.Fatalf("put without lease=%d %s", rec.Code, rec.Body.String())
	}
	acquireCloudWorkspaceLease(t, h, id, "m1")
	if rec := doCloudWorkspaceBytes(t, h, http.MethodPut, path, "m1", body); rec.Code != http.StatusOK {
		t.Fatalf("put=%d %s", rec.Code, rec.Body.String())
	}
	got := doCloudWorkspaceBytes(t, h, http.MethodGet, path, "m1", nil)
	if got.Code != http.StatusOK || !bytes.Equal(got.Body.Bytes(), body) {
		t.Fatalf("get=%d %q", got.Code, got.Body.Bytes())
	}
	var lease cloudworkspace.AcquireOutcome
	leaseRec := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces/"+id+"/leases", "m1", "secret", map[string]any{"force": false})
	if leaseRec.Code != http.StatusOK {
		t.Fatalf("renew lease=%d %s", leaseRec.Code, leaseRec.Body.String())
	}
	if err := json.Unmarshal(leaseRec.Body.Bytes(), &lease); err != nil {
		t.Fatal(err)
	}
	released := doCloudWorkspaceRequest(t, h, http.MethodDelete, "/api/v1/cloud-workspaces/"+id+"/leases/"+lease.LeaseID, "m1", "secret", nil)
	if released.Code != http.StatusOK {
		t.Fatalf("release=%d %s", released.Code, released.Body.String())
	}
	afterRelease := doCloudWorkspaceBytes(t, h, http.MethodGet, path, "m1", nil)
	if afterRelease.Code != http.StatusOK || !bytes.Equal(afterRelease.Body.Bytes(), body) {
		t.Fatalf("get without lease=%d %q", afterRelease.Code, afterRelease.Body.Bytes())
	}
	ent := doCloudWorkspaceRequest(t, h, http.MethodGet, "/api/v1/cloud-workspaces/entitlement", "m1", "secret", nil)
	if ent.Code != http.StatusOK {
		t.Fatalf("entitlement=%d %s", ent.Code, ent.Body.String())
	}
	var grant cloudworkspace.Entitlement
	if err := json.Unmarshal(ent.Body.Bytes(), &grant); err != nil {
		t.Fatal(err)
	}
	if len(grant.Workspaces) != 1 || grant.Workspaces[0].TaskName != "标书" || grant.Workspaces[0].TaskMode != "coding_dev" {
		t.Fatalf("entitlement task=%+v", grant.Workspaces)
	}
	missing := doCloudWorkspaceBytes(t, h, http.MethodGet, "/api/v1/cloud-workspaces/"+id+"/sidecars/"+cloudworkspace.SidecarSession, "m1", nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing session=%d %s", missing.Code, missing.Body.String())
	}
	bad := doCloudWorkspaceBytes(t, h, http.MethodPut, "/api/v1/cloud-workspaces/"+id+"/sidecars/secret.json", "m1", []byte("{}"))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("allowlist=%d %s", bad.Code, bad.Body.String())
	}
	other := doCloudWorkspaceBytes(t, h, http.MethodGet, path, "m2", nil)
	if other.Code != http.StatusNotFound && other.Code != http.StatusForbidden {
		t.Fatalf("non-owner=%d %s", other.Code, other.Body.String())
	}
	disk, err := svc.Blobs.SidecarPath("t1", "u1", id, cloudworkspace.SidecarTask)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(disk)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, body) {
		t.Fatal("hub sidecar leaked plaintext")
	}
	if _, err := svc.SaveTenantSettings(context.Background(), "t1", cloudworkspace.Settings{Mode: cloudworkspace.ModeOff, Quota: 5}); err != nil {
		t.Fatal(err)
	}
	denied := doCloudWorkspaceBytes(t, h, http.MethodGet, path, "m1", nil)
	if denied.Code != http.StatusForbidden || cloudWorkspaceErrCode(t, denied) != "CLOUD_WORKSPACE_FORBIDDEN" {
		t.Fatalf("grant revoked=%d %s", denied.Code, denied.Body.String())
	}
}
