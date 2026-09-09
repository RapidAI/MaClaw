package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/cloudworkspace"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	storesqlite "github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

var cloudWorkspaceHTTPTestSessions sync.Map

type cloudWorkspaceHTTPTestSessionKey struct {
	h         http.Handler
	machineID string
	token     string
}

type cloudWorkspaceHTTPTestLeaseKey struct {
	h           http.Handler
	machineID   string
	workspaceID string
}

type cloudWorkspaceHTTPTestLease struct {
	leaseID      string
	fencingToken int64
}

var cloudWorkspaceHTTPTestLeases sync.Map

func cloudWorkspaceHTTPTestNeedsSession(path string) bool {
	return (strings.HasPrefix(path, "/api/v1/cloud-workspaces") && path != "/api/v1/cloud-workspaces/entitlement") || strings.HasPrefix(path, "/api/v1/cloud-workspace-tasks")
}

func cloudWorkspaceHTTPTestSession(t *testing.T, h http.Handler, machineID, token string) (string, *httptest.ResponseRecorder) {
	t.Helper()
	key := cloudWorkspaceHTTPTestSessionKey{h: h, machineID: machineID, token: token}
	if cached, ok := cloudWorkspaceHTTPTestSessions.Load(key); ok {
		return cached.(string), nil
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cloud-workspace-sessions", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Machine-ID", machineID)
	req.Header.Set("X-Cloud-Workspace-Protocol", cloudworkspace.CloudWorkspaceProtocol)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		return "", rec
	}
	var session cloudworkspace.InstanceSession
	if err := json.Unmarshal(rec.Body.Bytes(), &session); err != nil {
		t.Fatalf("decode instance session: %v", err)
	}
	if session.Token == "" {
		t.Fatal("instance session response has no token")
	}
	cloudWorkspaceHTTPTestSessions.Store(key, session.Token)
	return session.Token, nil
}

func cloudWorkspaceHTTPTestWorkspaceID(path string) string {
	const prefix = "/api/v1/cloud-workspaces/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(path, prefix)
	if cut := strings.IndexByte(rest, '/'); cut >= 0 {
		rest = rest[:cut]
	}
	if rest == "" || rest == "entitlement" {
		return ""
	}
	return rest
}

func setCloudWorkspaceHTTPTestAuth(t *testing.T, h http.Handler, req *http.Request, machineID, token string) *httptest.ResponseRecorder {
	t.Helper()
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Machine-ID", machineID)
	if !cloudWorkspaceHTTPTestNeedsSession(req.URL.Path) {
		return nil
	}
	sessionToken, failed := cloudWorkspaceHTTPTestSession(t, h, machineID, token)
	if failed != nil {
		return failed
	}
	req.Header.Set("X-Cloud-Workspace-Protocol", cloudworkspace.CloudWorkspaceProtocol)
	req.Header.Set(cloudWorkspaceInstanceSessionHeader, sessionToken)
	if workspaceID := cloudWorkspaceHTTPTestWorkspaceID(req.URL.Path); workspaceID != "" {
		key := cloudWorkspaceHTTPTestLeaseKey{h: h, machineID: machineID, workspaceID: workspaceID}
		if cached, ok := cloudWorkspaceHTTPTestLeases.Load(key); ok {
			lease := cached.(cloudWorkspaceHTTPTestLease)
			req.Header.Set("X-Cloud-Workspace-Session", lease.leaseID)
			req.Header.Set("X-Cloud-Workspace-Fencing", fmt.Sprintf("%d", lease.fencingToken))
		}
	}
	return nil
}

func mustSetCloudWorkspaceHTTPTestAuth(t *testing.T, h http.Handler, req *http.Request, machineID, token string) {
	t.Helper()
	if failed := setCloudWorkspaceHTTPTestAuth(t, h, req, machineID, token); failed != nil {
		t.Fatalf("issue cloud workspace instance session: status=%d body=%s", failed.Code, failed.Body.String())
	}
}

func recordCloudWorkspaceHTTPTestLease(t *testing.T, h http.Handler, req *http.Request, machineID string, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec == nil || rec.Code < 200 || rec.Code >= 300 {
		return
	}
	workspaceID := cloudWorkspaceHTTPTestWorkspaceID(req.URL.Path)
	if workspaceID == "" {
		return
	}
	key := cloudWorkspaceHTTPTestLeaseKey{h: h, machineID: machineID, workspaceID: workspaceID}
	if req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/leases") {
		var out cloudworkspace.AcquireOutcome
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode lease outcome: %v", err)
		}
		if out.LeaseID != "" && out.FencingToken > 0 {
			cloudWorkspaceHTTPTestLeases.Store(key, cloudWorkspaceHTTPTestLease{leaseID: out.LeaseID, fencingToken: out.FencingToken})
		}
	}
	if req.Method == http.MethodDelete && strings.Contains(req.URL.Path, "/leases/") {
		cloudWorkspaceHTTPTestLeases.Delete(key)
	}
}

type cloudWorkspaceUserDir map[string]*store.User

func (m cloudWorkspaceUserDir) GetByID(ctx context.Context, id string) (*store.User, error) {
	_ = ctx
	return m[id], nil
}

func newCloudWorkspaceUserEnv(t *testing.T, mode string, quota int, departmentIDs []string) (*cloudworkspace.Service, http.Handler, fakeVEMachineAuth) {
	t.Helper()
	provider, err := storesqlite.NewProvider(storesqlite.Config{
		DSN:               filepath.Join(t.TempDir(), "cws-http.db"),
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  4,
		MaxReadIdleConns:  2,
		MaxWriteOpenConns: 4,
		MaxWriteIdleConns: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := storesqlite.RunMigrations(provider.Write); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	hub := storesqlite.NewStore(provider)
	for _, u := range []*store.User{
		{ID: "u1", TenantID: "t1", Email: "u1@x.com", SN: "SN-u1", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
		{ID: "u2", TenantID: "t1", Email: "u2@x.com", SN: "SN-u2", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
	} {
		if err := hub.Users.Create(context.Background(), u); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []*store.Machine{
		{ID: "m1", TenantID: "t1", UserID: "u1", Name: "pc-m1", Hostname: "DESKTOP-M1", Platform: "windows", Status: "online", CreatedAt: now, UpdatedAt: now},
		{ID: "m1b", TenantID: "t1", UserID: "u1", Name: "pc-m1b", Hostname: "DESKTOP-M1B", Platform: "windows", Status: "online", CreatedAt: now, UpdatedAt: now},
		{ID: "m2", TenantID: "t1", UserID: "u2", Name: "pc-m2", Hostname: "DESKTOP-M2", Platform: "windows", Status: "online", CreatedAt: now, UpdatedAt: now},
	} {
		if err := hub.Machines.Create(context.Background(), m); err != nil {
			t.Fatal(err)
		}
	}
	blobRoot := filepath.Join(t.TempDir(), "cws-blobs")
	svc := &cloudworkspace.Service{
		System: memoryCloudWorkspaceSettings{},
		Users: cloudWorkspaceUserDir{
			"u1": {ID: "u1", TenantID: "t1", Email: "u1@x.com"},
			"u2": {ID: "u2", TenantID: "t1", Email: "u2@x.com"},
		},
		Groups:     &fakeCloudWorkspaceOrg{},
		Workspaces: cloudworkspace.NewStore(provider.Write),
		Blobs:      &cloudworkspace.BlobStore{Root: blobRoot, KeyDir: filepath.Join(blobRoot, "keys"), DB: provider.Write},
	}
	if mode != "" {
		if _, err := svc.SaveTenantSettings(context.Background(), "t1", cloudworkspace.Settings{
			Mode:          mode,
			Quota:         quota,
			DepartmentIDs: departmentIDs,
		}); err != nil {
			t.Fatal(err)
		}
	}
	authn := fakeVEMachineAuth{
		token: "secret",
		principals: map[string]*auth.MachinePrincipal{
			"m1":      {TenantID: "t1", UserID: "u1", MachineID: "m1"},
			"m1b":     {TenantID: "t1", UserID: "u1", MachineID: "m1b"},
			"m2":      {TenantID: "t1", UserID: "u2", MachineID: "m2"},
			"m-empty": {TenantID: "t1", UserID: "", MachineID: "m-empty"},
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/cloud-workspaces/entitlement", CloudWorkspaceEntitlementHandler(svc, authn))
	mux.HandleFunc("POST /api/v1/cloud-workspace-sessions", CloudWorkspaceIssueInstanceSessionHandler(svc, authn))
	mux.HandleFunc("DELETE /api/v1/cloud-workspace-sessions/{session_id}", CloudWorkspaceRevokeInstanceSessionHandler(svc, authn))
	mux.HandleFunc("POST /api/v1/cloud-workspaces", CloudWorkspaceCreateHandler(svc, authn))
	mux.HandleFunc("POST /api/v1/cloud-workspace-tasks", CloudWorkspaceTaskProvisionHandler(svc, authn))
	mux.HandleFunc("GET /api/v1/cloud-workspace-tasks/{operation_id}", CloudWorkspaceTaskProvisionStatusHandler(svc, authn))
	mux.HandleFunc("POST /api/v1/cloud-workspace-tasks/{operation_id}/complete", cloudWorkspaceTaskProvisionTransitionHandler(svc, authn, false))
	mux.HandleFunc("POST /api/v1/cloud-workspace-tasks/{operation_id}/abort", cloudWorkspaceTaskProvisionTransitionHandler(svc, authn, true))
	mux.HandleFunc("PATCH /api/v1/cloud-workspaces/{id}", CloudWorkspaceRenameHandler(svc, authn))
	mux.HandleFunc("DELETE /api/v1/cloud-workspaces/{id}", CloudWorkspaceDeleteHandler(svc, authn))
	mux.HandleFunc("DELETE /api/v1/cloud-workspaces/{id}/purge", CloudWorkspaceHardDeleteHandler(svc, authn))
	mux.HandleFunc("POST /api/v1/cloud-workspaces/{id}/restore", CloudWorkspaceRestoreHandler(svc, authn))
	mux.HandleFunc("POST /api/v1/cloud-workspaces/{id}/leases", CloudWorkspaceAcquireLeaseHandler(svc, authn))
	mux.HandleFunc("POST /api/v1/cloud-workspaces/{id}/leases/handoff-request", CloudWorkspaceHandoffRequestHandler(svc, authn))
	mux.HandleFunc("POST /api/v1/cloud-workspaces/{id}/leases/{lease_id}/heartbeat", CloudWorkspaceHeartbeatLeaseHandler(svc, authn))
	mux.HandleFunc("DELETE /api/v1/cloud-workspaces/{id}/leases/{lease_id}", CloudWorkspaceReleaseLeaseHandler(svc, authn))
	mux.HandleFunc("GET /api/v1/cloud-workspaces/{id}/manifest", CloudWorkspaceGetManifestHandler(svc, authn))
	mux.HandleFunc("PUT /api/v1/cloud-workspaces/{id}/manifest", CloudWorkspacePutManifestHandler(svc, authn))
	mux.HandleFunc("POST /api/v1/cloud-workspaces/{id}/audit", CloudWorkspaceRecordAuditHandler(svc, authn))
	mux.HandleFunc("GET /api/v1/cloud-workspaces/{id}/audit", CloudWorkspaceListAuditHandler(svc, authn))
	mux.HandleFunc("GET /api/v1/cloud-workspaces/{id}/objects/{sha256}", CloudWorkspaceGetObjectHandler(svc, authn))
	mux.HandleFunc("PUT /api/v1/cloud-workspaces/{id}/objects/{sha256}", CloudWorkspacePutObjectHandler(svc, authn))
	mux.HandleFunc("PUT /api/v1/cloud-workspaces/{id}/objects/{sha256}/chunks/{index}", CloudWorkspacePutObjectChunkHandler(svc, authn))
	mux.HandleFunc("POST /api/v1/cloud-workspaces/{id}/objects/{sha256}/complete", CloudWorkspaceCompleteObjectHandler(svc, authn))
	mux.HandleFunc("GET /api/v1/cloud-workspaces/{id}/sidecars/{name}", CloudWorkspaceGetSidecarHandler(svc, authn))
	mux.HandleFunc("PUT /api/v1/cloud-workspaces/{id}/sidecars/{name}", CloudWorkspacePutSidecarHandler(svc, authn))
	mux.HandleFunc("GET /api/admin/cloud-workspaces/settings", GetCloudWorkspaceSettingsAdminHandler(svc))
	mux.HandleFunc("GET /api/admin/cloud-workspaces/metrics", GetCloudWorkspaceMetricsAdminHandler(svc))
	return svc, mux, authn
}

func doCloudWorkspaceRequest(t *testing.T, h http.Handler, method, path, machineID, token string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, body)
	if token != "" || machineID != "" {
		if failed := setCloudWorkspaceHTTPTestAuth(t, h, req, machineID, token); failed != nil {
			return failed
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	recordCloudWorkspaceHTTPTestLease(t, h, req, machineID, rec)
	return rec
}

func mustJSONBody(t *testing.T, payload any) io.Reader {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(raw)
}

func cloudWorkspaceErrCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var payload struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return payload.Code
}

func TestCloudWorkspaceInstanceSessionRequiredBoundAndRevocable(t *testing.T) {
	_, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)

	missingReq := httptest.NewRequest(http.MethodPost, "/api/v1/cloud-workspaces", mustJSONBody(t, map[string]any{"name": "missing"}))
	missingReq.Header.Set("Authorization", "Bearer secret")
	missingReq.Header.Set("X-Machine-ID", "m1")
	missingReq.Header.Set("X-Cloud-Workspace-Protocol", cloudworkspace.CloudWorkspaceProtocol)
	missing := httptest.NewRecorder()
	h.ServeHTTP(missing, missingReq)
	if missing.Code != http.StatusUnauthorized || cloudWorkspaceErrCode(t, missing) != "CLOUD_WORKSPACE_SESSION_REQUIRED" {
		t.Fatalf("missing session=%d %s", missing.Code, missing.Body.String())
	}

	issueReq := httptest.NewRequest(http.MethodPost, "/api/v1/cloud-workspace-sessions", nil)
	issueReq.Header.Set("Authorization", "Bearer secret")
	issueReq.Header.Set("X-Machine-ID", "m1")
	issueReq.Header.Set("X-Cloud-Workspace-Protocol", cloudworkspace.CloudWorkspaceProtocol)
	issueReq.Header.Set("X-Cloud-Workspace-Instance", "cwi_attacker_chosen")
	issuedRec := httptest.NewRecorder()
	h.ServeHTTP(issuedRec, issueReq)
	if issuedRec.Code != http.StatusCreated {
		t.Fatalf("issue=%d %s", issuedRec.Code, issuedRec.Body.String())
	}
	var issued cloudworkspace.InstanceSession
	if err := json.Unmarshal(issuedRec.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	if issued.Token == "" || issued.ID == "" || issued.ClientInstanceID == "" || issued.ClientInstanceID == "cwi_attacker_chosen" {
		t.Fatalf("issued=%+v", issued)
	}

	spoofReq := httptest.NewRequest(http.MethodPost, "/api/v1/cloud-workspaces", mustJSONBody(t, map[string]any{"name": "spoof"}))
	spoofReq.Header.Set("Authorization", "Bearer secret")
	spoofReq.Header.Set("X-Machine-ID", "m1")
	spoofReq.Header.Set("X-Cloud-Workspace-Protocol", cloudworkspace.CloudWorkspaceProtocol)
	spoofReq.Header.Set(cloudWorkspaceInstanceSessionHeader, issued.Token)
	spoofReq.Header.Set("X-Cloud-Workspace-Instance", "cwi_other")
	spoofed := httptest.NewRecorder()
	h.ServeHTTP(spoofed, spoofReq)
	if spoofed.Code != http.StatusUnauthorized || cloudWorkspaceErrCode(t, spoofed) != "CLOUD_WORKSPACE_SESSION_INVALID" {
		t.Fatalf("spoofed=%d %s", spoofed.Code, spoofed.Body.String())
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/cloud-workspaces", mustJSONBody(t, map[string]any{"name": "bound"}))
	createReq.Header.Set("Authorization", "Bearer secret")
	createReq.Header.Set("X-Machine-ID", "m1")
	createReq.Header.Set("X-Cloud-Workspace-Protocol", cloudworkspace.CloudWorkspaceProtocol)
	createReq.Header.Set(cloudWorkspaceInstanceSessionHeader, issued.Token)
	created := httptest.NewRecorder()
	h.ServeHTTP(created, createReq)
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}

	revokeReq := httptest.NewRequest(http.MethodDelete, "/api/v1/cloud-workspace-sessions/"+issued.ID, nil)
	revokeReq.Header.Set("Authorization", "Bearer secret")
	revokeReq.Header.Set("X-Machine-ID", "m1")
	revokeReq.Header.Set("X-Cloud-Workspace-Protocol", cloudworkspace.CloudWorkspaceProtocol)
	revokeReq.Header.Set(cloudWorkspaceInstanceSessionHeader, issued.Token)
	revoked := httptest.NewRecorder()
	h.ServeHTTP(revoked, revokeReq)
	if revoked.Code != http.StatusOK {
		t.Fatalf("revoke=%d %s", revoked.Code, revoked.Body.String())
	}

	afterReq := httptest.NewRequest(http.MethodPost, "/api/v1/cloud-workspaces", mustJSONBody(t, map[string]any{"name": "after"}))
	afterReq.Header.Set("Authorization", "Bearer secret")
	afterReq.Header.Set("X-Machine-ID", "m1")
	afterReq.Header.Set("X-Cloud-Workspace-Protocol", cloudworkspace.CloudWorkspaceProtocol)
	afterReq.Header.Set(cloudWorkspaceInstanceSessionHeader, issued.Token)
	after := httptest.NewRecorder()
	h.ServeHTTP(after, afterReq)
	if after.Code != http.StatusUnauthorized || cloudWorkspaceErrCode(t, after) != "CLOUD_WORKSPACE_SESSION_INVALID" {
		t.Fatalf("after revoke=%d %s", after.Code, after.Body.String())
	}
}

func TestCloudWorkspaceEntitlementDisabledWhenModeOff(t *testing.T) {
	_, h, _ := newCloudWorkspaceUserEnv(t, "", 0, nil)
	rec := doCloudWorkspaceRequest(t, h, http.MethodGet, "/api/v1/cloud-workspaces/entitlement", "m1", "secret", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var ent cloudworkspace.Entitlement
	if err := json.Unmarshal(rec.Body.Bytes(), &ent); err != nil {
		t.Fatal(err)
	}
	if ent.Enabled {
		t.Fatalf("enabled=%v", ent.Enabled)
	}
	if ent.Quota != 5 || ent.Workspaces == nil || ent.Deleted == nil {
		t.Fatalf("ent=%+v", ent)
	}
}

func TestCloudWorkspaceCreateDeniedWithoutGrant(t *testing.T) {
	_, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeOff, 5, nil)
	rec := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces", "m1", "secret", map[string]any{"name": "标书项目"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if cloudWorkspaceErrCode(t, rec) != "CLOUD_WORKSPACE_FORBIDDEN" {
		t.Fatalf("code=%q body=%s", cloudWorkspaceErrCode(t, rec), rec.Body.String())
	}
}

func TestCloudWorkspaceCreateDeniedWhenDepartmentsEmpty(t *testing.T) {
	svc, h, _ := newCloudWorkspaceUserEnv(t, "", 0, nil)
	raw, _ := json.Marshal(cloudworkspace.Settings{Mode: cloudworkspace.ModeDepartments, Quota: 5, DepartmentIDs: []string{}})
	if err := svc.System.Set(context.Background(), "tenant:t1:cloud_workspace", string(raw)); err != nil {
		t.Fatal(err)
	}
	rec := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces", "m1", "secret", map[string]any{"name": "x"})
	if rec.Code != http.StatusForbidden || cloudWorkspaceErrCode(t, rec) != "CLOUD_WORKSPACE_FORBIDDEN" {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCloudWorkspaceCreateSucceedsWithCWSID(t *testing.T) {
	_, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	rec := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces", "m1", "secret", map[string]any{"name": "标书项目"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Status    string `json:"status"`
		UsedBytes int64  `json:"used_bytes"`
		CreatedAt string `json:"created_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "标书项目" || got.Status != "active" || got.UsedBytes != 0 || got.CreatedAt == "" {
		t.Fatalf("got=%+v", got)
	}
	if !strings.HasPrefix(got.ID, "cws_") {
		t.Fatalf("id=%q", got.ID)
	}
	hexPart := strings.TrimPrefix(got.ID, "cws_")
	if len(hexPart) != 32 {
		t.Fatalf("hex len=%d id=%q", len(hexPart), got.ID)
	}
	for _, c := range hexPart {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Fatalf("non lowercase hex in %q", got.ID)
		}
	}
	empty := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces", "m1", "secret", map[string]any{})
	if empty.Code != http.StatusCreated {
		t.Fatalf("default create status=%d body=%s", empty.Code, empty.Body.String())
	}
	var def struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(empty.Body.Bytes(), &def); err != nil {
		t.Fatal(err)
	}
	if def.Name != "工作区 1" {
		t.Fatalf("default name=%q", def.Name)
	}
}

func TestCloudWorkspaceTaskProvisionLifecycleAndIdempotency(t *testing.T) {
	_, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	payload := map[string]any{"name": "编排任务", "device_task_id": "local-1", "mode": "coding_dev"}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cloud-workspace-tasks", mustJSONBody(t, payload))
	mustSetCloudWorkspaceHTTPTestAuth(t, h, req, "m1", "secret")
	req.Header.Set("Idempotency-Key", "provision-1")
	first := httptest.NewRecorder()
	h.ServeHTTP(first, req)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	var op map[string]any
	if err := json.Unmarshal(first.Body.Bytes(), &op); err != nil {
		t.Fatal(err)
	}
	opID, _ := op["operation_id"].(string)
	workspaceID, _ := op["workspace_id"].(string)
	if opID == "" || workspaceID == "" || op["state"] != cloudworkspace.ProvisionStateProvisioning {
		t.Fatalf("op=%v", op)
	}
	// Same key/payload replays the original operation instead of consuming quota.
	replayReq := httptest.NewRequest(http.MethodPost, "/api/v1/cloud-workspace-tasks", mustJSONBody(t, payload))
	mustSetCloudWorkspaceHTTPTestAuth(t, h, replayReq, "m1", "secret")
	replayReq.Header.Set("Idempotency-Key", "provision-1")
	replay := httptest.NewRecorder()
	h.ServeHTTP(replay, replayReq)
	var replayOp map[string]any
	if err := json.Unmarshal(replay.Body.Bytes(), &replayOp); err != nil {
		t.Fatal(err)
	}
	if replay.Code != http.StatusAccepted || replayOp["operation_id"] != opID || replayOp["workspace_id"] != workspaceID {
		t.Fatalf("replay status=%d body=%s want operation=%s workspace=%s", replay.Code, replay.Body.String(), opID, workspaceID)
	}
	lease := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces/"+workspaceID+"/leases", "m1", "secret", map[string]any{"force": false})
	if lease.Code != http.StatusOK {
		t.Fatalf("lease status=%d body=%s", lease.Code, lease.Body.String())
	}
	completeReq := httptest.NewRequest(http.MethodPost, "/api/v1/cloud-workspace-tasks/"+opID+"/complete", nil)
	mustSetCloudWorkspaceHTTPTestAuth(t, h, completeReq, "m1", "secret")
	if cached, ok := cloudWorkspaceHTTPTestLeases.Load(cloudWorkspaceHTTPTestLeaseKey{h: h, machineID: "m1", workspaceID: workspaceID}); ok {
		lease := cached.(cloudWorkspaceHTTPTestLease)
		completeReq.Header.Set("X-Cloud-Workspace-Session", lease.leaseID)
		completeReq.Header.Set("X-Cloud-Workspace-Fencing", fmt.Sprintf("%d", lease.fencingToken))
	} else {
		t.Fatal("missing cached provisioning lease")
	}
	complete := httptest.NewRecorder()
	h.ServeHTTP(complete, completeReq)
	if complete.Code != http.StatusOK {
		t.Fatalf("complete status=%d body=%s", complete.Code, complete.Body.String())
	}
	statusReq := httptest.NewRequest(http.MethodGet, "/api/v1/cloud-workspace-tasks/"+opID, nil)
	mustSetCloudWorkspaceHTTPTestAuth(t, h, statusReq, "m1", "secret")
	status := httptest.NewRecorder()
	h.ServeHTTP(status, statusReq)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"state":"active"`) {
		t.Fatalf("status=%d body=%s", status.Code, status.Body.String())
	}
}

func TestCloudWorkspaceTaskProvisionRecoversOperationWhenLedgerResponseWasLost(t *testing.T) {
	svc, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	ctx := context.Background()
	payload := map[string]any{
		"name": "崩溃恢复任务", "device_task_id": "local-recover", "mode": "coding_dev",
	}
	payloadForHash := struct {
		Name         string `json:"name"`
		CloudTaskID  string `json:"cloud_task_id"`
		DeviceTaskID string `json:"device_task_id"`
		Mode         string `json:"mode"`
		Tag          string `json:"tag"`
	}{Name: "崩溃恢复任务", DeviceTaskID: "local-recover", Mode: "coding_dev"}
	payloadHash, _, err := cloudWorkspacePayloadHash(payloadForHash)
	if err != nil {
		t.Fatal(err)
	}
	ledgerKey := "workspace-task:provision:provision-recover"
	if _, err := svc.Workspaces.BeginIdempotency(ctx, "t1", "u1", "", "cwi-old", ledgerKey, payloadHash, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	op, err := svc.Workspaces.BeginWorkspaceTaskProvision(ctx, cloudworkspace.WorkspaceTaskProvisionParams{
		TenantID: "t1", UserID: "u1", Name: "崩溃恢复任务", DeviceTaskID: "local-recover", Mode: "coding_dev",
		IdempotencyKey: "provision-recover", IdempotencyPayloadHash: payloadHash, Quota: 5,
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if op == nil || op.OperationID == "" {
		t.Fatalf("operation=%+v", op)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cloud-workspace-tasks", mustJSONBody(t, payload))
	mustSetCloudWorkspaceHTTPTestAuth(t, h, req, "m1", "secret")
	req.Header.Set("Idempotency-Key", "provision-recover")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var recovered map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &recovered); err != nil {
		t.Fatal(err)
	}
	if recovered["operation_id"] != op.OperationID || recovered["workspace_id"] != op.WorkspaceID {
		t.Fatalf("recovered=%v want op=%s workspace=%s", recovered, op.OperationID, op.WorkspaceID)
	}
}

func TestCloudWorkspaceCreateAtQuotaAndDuplicateName(t *testing.T) {
	_, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 1, nil)
	first := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces", "m1", "secret", map[string]any{"name": "Foo"})
	if first.Code != http.StatusCreated {
		t.Fatalf("first=%d body=%s", first.Code, first.Body.String())
	}
	quota := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces", "m1", "secret", map[string]any{"name": "Bar"})
	if quota.Code != http.StatusForbidden || cloudWorkspaceErrCode(t, quota) != "CLOUD_WORKSPACE_QUOTA" {
		t.Fatalf("quota status=%d body=%s", quota.Code, quota.Body.String())
	}
	_, h2, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	a := doCloudWorkspaceRequest(t, h2, http.MethodPost, "/api/v1/cloud-workspaces", "m1", "secret", map[string]any{"name": "Foo"})
	if a.Code != http.StatusCreated {
		t.Fatalf("a=%d %s", a.Code, a.Body.String())
	}
	dup := doCloudWorkspaceRequest(t, h2, http.MethodPost, "/api/v1/cloud-workspaces", "m1", "secret", map[string]any{"name": "foo"})
	if dup.Code != http.StatusConflict || cloudWorkspaceErrCode(t, dup) != "CLOUD_WORKSPACE_NAME_TAKEN" {
		t.Fatalf("dup status=%d body=%s", dup.Code, dup.Body.String())
	}
}

func TestCloudWorkspaceSoftDeleteRestoreAndNonOwner(t *testing.T) {
	_, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 1, nil)
	created := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces", "m1", "secret", map[string]any{"name": "A"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	var ws struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &ws); err != nil {
		t.Fatal(err)
	}
	other := doCloudWorkspaceRequest(t, h, http.MethodDelete, "/api/v1/cloud-workspaces/"+ws.ID, "m2", "secret", nil)
	if other.Code != http.StatusNotFound {
		t.Fatalf("non-owner delete=%d %s", other.Code, other.Body.String())
	}
	del := doCloudWorkspaceRequest(t, h, http.MethodDelete, "/api/v1/cloud-workspaces/"+ws.ID, "m1", "secret", nil)
	if del.Code != http.StatusOK {
		t.Fatalf("delete=%d %s", del.Code, del.Body.String())
	}
	second := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces", "m1", "secret", map[string]any{"name": "B"})
	if second.Code != http.StatusCreated {
		t.Fatalf("second after delete=%d %s", second.Code, second.Body.String())
	}
	blocked := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces/"+ws.ID+"/restore", "m1", "secret", nil)
	if blocked.Code != http.StatusForbidden || cloudWorkspaceErrCode(t, blocked) != "CLOUD_WORKSPACE_QUOTA" {
		t.Fatalf("restore over quota=%d %s", blocked.Code, blocked.Body.String())
	}
	var secondWS struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondWS); err != nil {
		t.Fatal(err)
	}
	if rec := doCloudWorkspaceRequest(t, h, http.MethodDelete, "/api/v1/cloud-workspaces/"+secondWS.ID, "m1", "secret", nil); rec.Code != http.StatusOK {
		t.Fatalf("delete second=%d %s", rec.Code, rec.Body.String())
	}
	restored := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces/"+ws.ID+"/restore", "m1", "secret", nil)
	if restored.Code != http.StatusOK {
		t.Fatalf("restore=%d %s", restored.Code, restored.Body.String())
	}
	foreignRestore := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces/"+ws.ID+"/restore", "m2", "secret", nil)
	if foreignRestore.Code != http.StatusNotFound {
		t.Fatalf("non-owner restore=%d %s", foreignRestore.Code, foreignRestore.Body.String())
	}
	emptyUser := doCloudWorkspaceRequest(t, h, http.MethodGet, "/api/v1/cloud-workspaces/entitlement", "m-empty", "secret", nil)
	if emptyUser.Code != http.StatusOK {
		t.Fatalf("empty user entitlement=%d %s", emptyUser.Code, emptyUser.Body.String())
	}
	var emptyEnt cloudworkspace.Entitlement
	if err := json.Unmarshal(emptyUser.Body.Bytes(), &emptyEnt); err != nil {
		t.Fatal(err)
	}
	if emptyEnt.Enabled {
		t.Fatalf("empty user must not be granted: %+v", emptyEnt)
	}
}

func TestCloudWorkspaceAdminPreviewOverQuotaUsers(t *testing.T) {
	svc, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 1, nil)
	if rec := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces", "m1", "secret", map[string]any{"name": "A"}); rec.Code != http.StatusCreated {
		t.Fatalf("create A=%d %s", rec.Code, rec.Body.String())
	}
	if _, err := svc.SaveTenantSettings(context.Background(), "t1", cloudworkspace.Settings{Mode: cloudworkspace.ModeAllUsers, Quota: 5}); err != nil {
		t.Fatal(err)
	}
	if rec := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces", "m1", "secret", map[string]any{"name": "B"}); rec.Code != http.StatusCreated {
		t.Fatalf("create B=%d %s", rec.Code, rec.Body.String())
	}
	if _, err := svc.SaveTenantSettings(context.Background(), "t1", cloudworkspace.Settings{Mode: cloudworkspace.ModeAllUsers, Quota: 1}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cloud-workspaces/settings", nil)
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{ID: "adm", Scope: "tenant", TenantID: "t1"}))
	GetCloudWorkspaceSettingsAdminHandler(svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin get=%d %s", rec.Code, rec.Body.String())
	}
	var got cloudworkspace.SettingsView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Preview.OverQuotaUsers) != 1 {
		t.Fatalf("over_quota_users=%v", got.Preview.OverQuotaUsers)
	}
	item := got.Preview.OverQuotaUsers[0]
	if item.SN != "SN-u1" || item.Used != 2 || item.Quota != 1 {
		t.Fatalf("item=%+v", item)
	}
}

func TestCloudWorkspaceEntitlementEmptyUserIDIsDisabledNotUnauthorized(t *testing.T) {
	_, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	rec := doCloudWorkspaceRequest(t, h, http.MethodGet, "/api/v1/cloud-workspaces/entitlement", "m-empty", "secret", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("entitlement=%d %s", rec.Code, rec.Body.String())
	}
	var ent cloudworkspace.Entitlement
	if err := json.Unmarshal(rec.Body.Bytes(), &ent); err != nil {
		t.Fatal(err)
	}
	if ent.Enabled {
		t.Fatalf("empty user granted: %+v", ent)
	}
	if ent.Reason != cloudworkspace.ReasonMachineUnbound {
		t.Fatalf("reason=%q want %q", ent.Reason, cloudworkspace.ReasonMachineUnbound)
	}
	if ent.Quota != 5 {
		t.Fatalf("quota=%d want tenant quota", ent.Quota)
	}
	create := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces", "m-empty", "secret", map[string]any{"name": "X"})
	if create.Code != http.StatusUnauthorized || cloudWorkspaceErrCode(t, create) != "MACHINE_UNAUTHORIZED" {
		t.Fatalf("empty user create=%d %s", create.Code, create.Body.String())
	}
}

func TestCloudWorkspaceEntitlementEnabledIncludesWorkspaces(t *testing.T) {
	_, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	created := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces", "m1", "secret", map[string]any{"name": "A"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	rec := doCloudWorkspaceRequest(t, h, http.MethodGet, "/api/v1/cloud-workspaces/entitlement", "m1", "secret", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var ent cloudworkspace.Entitlement
	if err := json.Unmarshal(rec.Body.Bytes(), &ent); err != nil {
		t.Fatal(err)
	}
	if !ent.Enabled || ent.Used != 1 || len(ent.Workspaces) != 1 || ent.Workspaces[0].Name != "A" {
		t.Fatalf("ent=%+v", ent)
	}
}
