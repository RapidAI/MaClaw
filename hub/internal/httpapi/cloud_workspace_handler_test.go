package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/cloudworkspace"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	storesqlite "github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

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
	svc := &cloudworkspace.Service{
		System: memoryCloudWorkspaceSettings{},
		Users: cloudWorkspaceUserDir{
			"u1": {ID: "u1", TenantID: "t1", Email: "u1@x.com"},
			"u2": {ID: "u2", TenantID: "t1", Email: "u2@x.com"},
		},
		Groups:     &fakeCloudWorkspaceOrg{},
		Workspaces: cloudworkspace.NewStore(provider.Write),
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
	mux.HandleFunc("POST /api/v1/cloud-workspaces", CloudWorkspaceCreateHandler(svc, authn))
	mux.HandleFunc("PATCH /api/v1/cloud-workspaces/{id}", CloudWorkspaceRenameHandler(svc, authn))
	mux.HandleFunc("DELETE /api/v1/cloud-workspaces/{id}", CloudWorkspaceDeleteHandler(svc, authn))
	mux.HandleFunc("POST /api/v1/cloud-workspaces/{id}/restore", CloudWorkspaceRestoreHandler(svc, authn))
	mux.HandleFunc("POST /api/v1/cloud-workspaces/{id}/leases", CloudWorkspaceAcquireLeaseHandler(svc, authn))
	mux.HandleFunc("POST /api/v1/cloud-workspaces/{id}/leases/{lease_id}/heartbeat", CloudWorkspaceHeartbeatLeaseHandler(svc, authn))
	mux.HandleFunc("DELETE /api/v1/cloud-workspaces/{id}/leases/{lease_id}", CloudWorkspaceReleaseLeaseHandler(svc, authn))
	mux.HandleFunc("GET /api/admin/cloud-workspaces/settings", GetCloudWorkspaceSettingsAdminHandler(svc))
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
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if machineID != "" {
		req.Header.Set("X-Machine-ID", machineID)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
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
	if emptyUser.Code != http.StatusUnauthorized || cloudWorkspaceErrCode(t, emptyUser) != "MACHINE_UNAUTHORIZED" {
		t.Fatalf("empty user=%d %s", emptyUser.Code, emptyUser.Body.String())
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
