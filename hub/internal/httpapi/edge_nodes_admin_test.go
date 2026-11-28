package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
	"github.com/RapidAI/CodeClaw/hub/internal/desktoppool"
	"github.com/RapidAI/CodeClaw/hub/internal/edgenode"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type edgeMemSettings struct {
	mu sync.Mutex
	m  map[string]string
}

func (m *edgeMemSettings) Get(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.m[key]
	if !ok {
		return "", io.EOF
	}
	return v, nil
}

func (m *edgeMemSettings) Set(_ context.Context, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.m == nil {
		m.m = map[string]string{}
	}
	m.m[key] = value
	return nil
}

func TestEdgeNodeListHidesSecretsAndDeleteRemovesIt(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	reg := edgenode.NewRegistry(&edgeMemSettings{})
	code, err := reg.IssueCode(context.Background(), store.DefaultTenantID, "lab")
	if err != nil {
		t.Fatal(err)
	}
	enrolled, err := reg.Enroll(context.Background(), code.Code, "desk-key-secret", "srv-admin-secret", "v9", true, true, "")
	if err != nil {
		t.Fatal(err)
	}
	broker := edgenode.NewBroker(reg)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/bots/nodes", nil)
	GetEdgeNodesAdminHandler(reg, broker).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status %d body %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, secret := range []string{"desk-key-secret", "srv-admin-secret", enrolled.NodeSecret, enrolled.HubToken, "secret_hash"} {
		if secret != "" && strings.Contains(body, secret) {
			t.Fatalf("list leaked %q", secret)
		}
	}
	if !strings.Contains(body, `"version":"v9"`) || !strings.Contains(body, `"has_desktopd":true`) || !strings.Contains(body, `"has_srv":true`) {
		t.Fatalf("list body %s", body)
	}
	if !strings.Contains(body, `"online":false`) && !strings.Contains(body, `"online": false`) {
		// omitempty leaves a disconnected node without online; that is offline.
		if strings.Contains(body, `"online":true`) {
			t.Fatalf("disconnected node looked online: %s", body)
		}
	}

	rec = httptest.NewRecorder()
	del := httptest.NewRequest(http.MethodDelete, "/api/admin/bots/nodes/"+enrolled.NodeID, nil)
	del.SetPathValue("id", enrolled.NodeID)
	DeleteEdgeNodeAdminHandler(reg, broker, nil, nil).ServeHTTP(rec, del)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status %d body %s", rec.Code, rec.Body.String())
	}
	if _, err := reg.Lookup(context.Background(), enrolled.NodeID); err == nil {
		t.Fatal("deleted node is still registered")
	}
}

func TestRotateEnrollmentKeepsTheSameDesktopServer(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	settings := &edgeMemSettings{}
	reg := edgenode.NewRegistry(settings)
	pool := desktoppool.New(settings, nil)
	ctx := context.Background()
	code, err := reg.IssueCode(ctx, store.DefaultTenantID, "lab")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/edge/enroll", strings.NewReader(`{"code":"`+code.Code+`","desktopd_key":"key-1","version":"v1","has_desktopd":true}`))
	PostEdgeEnrollHandler(reg, pool, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("enroll %d %s", rec.Code, rec.Body.String())
	}
	var first edgenode.EnrollResult
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	rotateReq := httptest.NewRequest(http.MethodPost, "/api/admin/bots/nodes/"+first.NodeID+"/code", strings.NewReader(`{}`))
	rotateReq.SetPathValue("id", first.NodeID)
	PostEdgeRotateCodeAdminHandler(reg).ServeHTTP(rec, rotateReq)
	if rec.Code != http.StatusCreated {
		t.Fatalf("rotate code %d %s", rec.Code, rec.Body.String())
	}
	var issued struct {
		Code   string `json:"code"`
		NodeID string `json:"node_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	if issued.NodeID != first.NodeID || issued.Code == "" {
		t.Fatalf("issued = %+v", issued)
	}
	rec = httptest.NewRecorder()
	again := httptest.NewRequest(http.MethodPost, "/api/v1/edge/enroll", strings.NewReader(`{"code":"`+issued.Code+`","desktopd_key":"key-2","version":"v2","has_desktopd":true}`))
	PostEdgeEnrollHandler(reg, pool, nil).ServeHTTP(rec, again)
	if rec.Code != http.StatusCreated {
		t.Fatalf("second enroll %d %s", rec.Code, rec.Body.String())
	}
	view, err := pool.View(ctx, store.DefaultTenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Servers) != 1 || view.Servers[0].NodeID != first.NodeID {
		t.Fatalf("servers = %+v", view.Servers)
	}
	if _, err := reg.Authenticate(ctx, first.NodeID, first.NodeSecret); err == nil {
		t.Fatal("the old secret still opens the tunnel")
	}
}

func TestOwnedSrvOnlyNodeUpgradeOnRotationCreatesAnOwnedDesktop(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	settings := &edgeMemSettings{}
	reg := edgenode.NewRegistry(settings)
	pool := desktoppool.New(settings, nil)
	bots := botmgmt.NewService(&botSettingsMem{})
	ctx := context.Background()
	code, err := reg.IssueOwnedCode(ctx, store.DefaultTenantID, "alice", "alice-pc")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/edge/enroll", strings.NewReader(`{"code":"`+code.Code+`","srv_admin_secret":"srv-1","version":"v1","has_srv":true}`))
	PostEdgeEnrollHandler(reg, pool, bots).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("srv-only enroll %d %s", rec.Code, rec.Body.String())
	}
	var first edgenode.EnrollResult
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	rotate, err := reg.IssueRotateCode(ctx, store.DefaultTenantID, first.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	upgrade := httptest.NewRequest(http.MethodPost, "/api/v1/edge/enroll", strings.NewReader(`{"code":"`+rotate.Code+`","desktopd_key":"key-2","srv_admin_secret":"srv-2","version":"v2","has_desktopd":true,"has_srv":true,"host_ram_bytes":15032385536,"host_cpus":8}`))
	PostEdgeEnrollHandler(reg, pool, bots).ServeHTTP(rec, upgrade)
	if rec.Code != http.StatusCreated {
		t.Fatalf("desktop upgrade %d %s", rec.Code, rec.Body.String())
	}
	servers, active, err := pool.OwnedServers(ctx, store.DefaultTenantID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0].NodeID != first.NodeID || servers[0].OwnerUserID != "alice" || !servers[0].Intranet {
		t.Fatalf("owned servers = %+v", servers)
	}
	if active != servers[0].ID {
		t.Fatalf("active = %q, want %q", active, servers[0].ID)
	}
	if servers[0].Memory != "4g" || servers[0].CPUs != "4" || !servers[0].ResourcesAuto {
		t.Fatalf("host resources were not recorded: %+v", servers[0])
	}
}

func TestRefreshRotatedDesktopReconcilesOwnerAndHostSize(t *testing.T) {
	settings := &edgeMemSettings{}
	pool := desktoppool.New(settings, nil)
	ctx := context.Background()
	if _, err := pool.CreateServer(ctx, store.DefaultTenantID, desktoppool.Server{
		Name: "node-pc", BaseURL: desktoppool.NodeBaseURL("node-1"), AccessToken: "old-key",
		NodeID: "node-1", OwnerUserID: "bob", Intranet: true,
		Memory: "2g", CPUs: "1", ShmSize: "1g", ResourcesAuto: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := refreshRotatedDesktop(ctx, pool, store.DefaultTenantID, "node-1", "node-pc", "alice", "new-key", 15032385536, 8); err != nil {
		t.Fatal(err)
	}
	servers, active, err := pool.OwnedServers(ctx, store.DefaultTenantID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0].OwnerUserID != "alice" || active != servers[0].ID {
		t.Fatalf("owned servers=%+v active=%q", servers, active)
	}
	if servers[0].Memory != "4g" || servers[0].CPUs != "4" || !servers[0].ResourcesAuto {
		t.Fatalf("rotation did not refit the existing desktop: %+v", servers[0])
	}
	if servers[0].AccessToken != "new-key" {
		t.Fatalf("rotation did not refresh the desktop key: %+v", servers[0])
	}
}

func TestOwnedSrvOnlyNodeUpgradeOnRotationRefusesASmallHost(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	settings := &edgeMemSettings{}
	reg := edgenode.NewRegistry(settings)
	pool := desktoppool.New(settings, nil)
	bots := botmgmt.NewService(&botSettingsMem{})
	ctx := context.Background()
	code, err := reg.IssueOwnedCode(ctx, store.DefaultTenantID, "alice", "alice-pc")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/edge/enroll", strings.NewReader(`{"code":"`+code.Code+`","srv_admin_secret":"srv-1","version":"v1","has_srv":true}`))
	PostEdgeEnrollHandler(reg, pool, bots).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("srv-only enroll %d %s", rec.Code, rec.Body.String())
	}
	var first edgenode.EnrollResult
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	rotate, err := reg.IssueRotateCode(ctx, store.DefaultTenantID, first.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	upgrade := httptest.NewRequest(http.MethodPost, "/api/v1/edge/enroll", strings.NewReader(`{"code":"`+rotate.Code+`","desktopd_key":"key-2","srv_admin_secret":"srv-2","version":"v2","has_desktopd":true,"has_srv":true,"host_ram_bytes":6442450944,"host_cpus":4}`))
	PostEdgeEnrollHandler(reg, pool, bots).ServeHTTP(rec, upgrade)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "内存不到 8G") {
		t.Fatalf("small host upgrade %d %s", rec.Code, rec.Body.String())
	}
	servers, _, err := pool.OwnedServers(ctx, store.DefaultTenantID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 0 {
		t.Fatalf("small host still recorded a desktop: %+v", servers)
	}
	node, err := reg.Lookup(ctx, first.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	if node.HasDesktopd {
		t.Fatal("failed upgrade left the node marked as having a desktop")
	}
}

func TestEdgeLinkRestoresAMissingDesktopRecord(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	settings := &edgeMemSettings{}
	reg := edgenode.NewRegistry(settings)
	pool := desktoppool.New(settings, nil)
	ctx := context.Background()
	code, err := reg.IssueOwnedCode(ctx, store.DefaultTenantID, "alice", "alice-pc")
	if err != nil {
		t.Fatal(err)
	}
	enrolled, err := reg.Enroll(ctx, code.Code, "desk-key", "srv-admin", "v1", true, true, "")
	if err != nil {
		t.Fatal(err)
	}
	node, err := reg.Lookup(ctx, enrolled.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureEdgeDesktopRecorded(ctx, pool, node); err != nil {
		t.Fatal(err)
	}
	servers, active, err := pool.OwnedServers(ctx, store.DefaultTenantID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0].NodeID != node.ID || servers[0].OwnerUserID != "alice" || active != servers[0].ID {
		t.Fatalf("restored servers=%+v active=%q err=%v", servers, active, err)
	}
}

func TestEdgeCodeNamesTheIssuingAdmin(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	reg := edgenode.NewRegistry(&edgeMemSettings{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/bots/nodes/codes", strings.NewReader(`{"name":"lab"}`))
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{Username: "ops-admin", Scope: "tenant", TenantID: store.DefaultTenantID}))
	PostEdgeCodeAdminHandler(reg).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("code status %d body %s", rec.Code, rec.Body.String())
	}
	var issued struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	enrolled, err := reg.Enroll(context.Background(), issued.Code, "desk-key", "", "v1", true, false, "")
	if err != nil {
		t.Fatal(err)
	}
	node, err := reg.Lookup(context.Background(), enrolled.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	if node.IssuedBy != "ops-admin" {
		t.Fatalf("issued_by %q", node.IssuedBy)
	}
	rec = httptest.NewRecorder()
	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/bots/nodes", nil)
	GetEdgeNodesAdminHandler(reg, nil).ServeHTTP(rec, listReq)
	if !strings.Contains(rec.Body.String(), `"issued_by":"ops-admin"`) {
		t.Fatalf("list body %s", rec.Body.String())
	}
}

func TestEdgeNodeDeleteHidesAnotherTenant(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	reg := edgenode.NewRegistry(&edgeMemSettings{})
	code, err := reg.IssueCode(context.Background(), "tenant-other", "theirs")
	if err != nil {
		t.Fatal(err)
	}
	enrolled, err := reg.Enroll(context.Background(), code.Code, "desk-key", "", "v1", true, false, "")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/bots/nodes/"+enrolled.NodeID, nil)
	req.SetPathValue("id", enrolled.NodeID)
	DeleteEdgeNodeAdminHandler(reg, nil, nil, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if _, err := reg.Lookup(context.Background(), enrolled.NodeID); err != nil {
		t.Fatal("another tenant's node was removed")
	}
}

func TestShareNodeTurnsAPersonalNodeShared(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	settings := &edgeMemSettings{}
	reg := edgenode.NewRegistry(settings)
	pool := desktoppool.New(settings, nil)
	ctx := context.Background()
	code, err := reg.IssueOwnedCode(ctx, store.DefaultTenantID, "alice", "desk")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	enroll := httptest.NewRequest(http.MethodPost, "/api/v1/edge/enroll", strings.NewReader(`{"code":"`+code.Code+`","desktopd_key":"key-1","version":"v1","has_desktopd":true}`))
	PostEdgeEnrollHandler(reg, pool, nil).ServeHTTP(rec, enroll)
	if rec.Code != http.StatusCreated {
		t.Fatalf("enroll %d %s", rec.Code, rec.Body.String())
	}
	var first edgenode.EnrollResult
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	view, err := pool.View(ctx, store.DefaultTenantID)
	if err != nil || len(view.Servers) != 1 || view.Servers[0].OwnerID != "alice" {
		t.Fatalf("owned servers = %+v err=%v", view.Servers, err)
	}
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/bots/nodes/"+first.NodeID+"/settings", strings.NewReader(`{"name":"desk","owner_user_id":""}`))
	req.SetPathValue("id", first.NodeID)
	PostEdgeNodeSettingsAdminHandler(reg, pool, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("share %d %s", rec.Code, rec.Body.String())
	}
	node, err := reg.Lookup(ctx, first.NodeID)
	if err != nil || node.OwnerUserID != "" {
		t.Fatalf("node owner %q err=%v", node.OwnerUserID, err)
	}
	view, err = pool.View(ctx, store.DefaultTenantID)
	if err != nil || len(view.Servers) != 1 || view.Servers[0].OwnerID != "" {
		t.Fatalf("shared servers = %+v err=%v", view.Servers, err)
	}
	rec = httptest.NewRecorder()
	again := httptest.NewRequest(http.MethodPost, "/api/admin/bots/nodes/"+first.NodeID+"/settings", strings.NewReader(`{"name":"desk","owner_user_id":""}`))
	again.SetPathValue("id", first.NodeID)
	PostEdgeNodeSettingsAdminHandler(reg, pool, nil).ServeHTTP(rec, again)
	if rec.Code != http.StatusOK {
		t.Fatalf("second share %d %s", rec.Code, rec.Body.String())
	}
}

func TestShareNodeHidesAnotherTenant(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	reg := edgenode.NewRegistry(&edgeMemSettings{})
	code, err := reg.IssueOwnedCode(context.Background(), "tenant-other", "alice", "desk")
	if err != nil {
		t.Fatal(err)
	}
	enrolled, err := reg.Enroll(context.Background(), code.Code, "desk-key", "", "v1", true, false, "")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/bots/nodes/"+enrolled.NodeID+"/settings", strings.NewReader(`{"name":"desk","owner_user_id":""}`))
	req.SetPathValue("id", enrolled.NodeID)
	PostEdgeNodeSettingsAdminHandler(reg, nil, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	node, err := reg.Lookup(context.Background(), enrolled.NodeID)
	if err != nil || node.OwnerUserID != "alice" {
		t.Fatalf("another tenant's node was shared: owner=%q err=%v", node.OwnerUserID, err)
	}
}

func TestNodeSettingsRenamesAndReassigns(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	settings := &edgeMemSettings{}
	reg := edgenode.NewRegistry(settings)
	pool := desktoppool.New(settings, nil)
	ctx := context.Background()
	code, err := reg.IssueOwnedCode(ctx, store.DefaultTenantID, "alice", "desk")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	enroll := httptest.NewRequest(http.MethodPost, "/api/v1/edge/enroll", strings.NewReader(`{"code":"`+code.Code+`","desktopd_key":"key-1","version":"v1","has_desktopd":true}`))
	PostEdgeEnrollHandler(reg, pool, nil).ServeHTTP(rec, enroll)
	if rec.Code != http.StatusCreated {
		t.Fatalf("enroll %d %s", rec.Code, rec.Body.String())
	}
	var first edgenode.EnrollResult
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	post := func(body string) *httptest.ResponseRecorder {
		out := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/admin/bots/nodes/"+first.NodeID+"/settings", strings.NewReader(body))
		req.SetPathValue("id", first.NodeID)
		PostEdgeNodeSettingsAdminHandler(reg, pool, nil).ServeHTTP(out, req)
		return out
	}
	if rec := post(`{"name":"lab-west","owner_user_id":"phone:bob"}`); rec.Code != http.StatusOK {
		t.Fatalf("settings %d %s", rec.Code, rec.Body.String())
	}
	node, err := reg.Lookup(ctx, first.NodeID)
	if err != nil || node.Name != "lab-west" || node.OwnerUserID != "phone:bob" {
		t.Fatalf("node = %+v err=%v", node, err)
	}
	view, err := pool.View(ctx, store.DefaultTenantID)
	if err != nil || len(view.Servers) != 1 || view.Servers[0].OwnerID != "phone:bob" {
		t.Fatalf("exclusive servers = %+v err=%v", view.Servers, err)
	}
	if rec := post(`{"name":"lab-west","owner_user_id":""}`); rec.Code != http.StatusOK {
		t.Fatalf("share %d %s", rec.Code, rec.Body.String())
	}
	node, err = reg.Lookup(ctx, first.NodeID)
	if err != nil || node.OwnerUserID != "" {
		t.Fatalf("node owner after share %q err=%v", node.OwnerUserID, err)
	}
	view, err = pool.View(ctx, store.DefaultTenantID)
	if err != nil || len(view.Servers) != 1 || view.Servers[0].OwnerID != "" {
		t.Fatalf("shared servers = %+v err=%v", view.Servers, err)
	}
	if rec := post(`{"name":"","owner_user_id":""}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty name status %d", rec.Code)
	}
	if rec := post(`{"name":"lab-west","owner_user_id":"not a user!!"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid owner status %d", rec.Code)
	}
	other := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/bots/nodes/"+first.NodeID+"/settings", strings.NewReader(`{"name":"x"}`))
	req.SetPathValue("id", first.NodeID)
	ctx2 := context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{Username: "op", Scope: "tenant", TenantID: "tenant-other"})
	PostEdgeNodeSettingsAdminHandler(reg, pool, nil).ServeHTTP(other, req.WithContext(ctx2))
	if other.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant status %d", other.Code)
	}
}

type failingBotSettings struct{}

func (failingBotSettings) Get(context.Context, string) (string, error) { return "", io.EOF }

func (failingBotSettings) Set(context.Context, string, string) error {
	return errors.New("store down")
}

func TestEnrollWithSrvRollsBackWhenTheConnectionIsNotStored(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	settings := &edgeMemSettings{}
	reg := edgenode.NewRegistry(settings)
	pool := desktoppool.New(settings, nil)
	bots := botmgmt.NewService(failingBotSettings{})
	ctx := context.Background()
	code, err := reg.IssueCode(ctx, store.DefaultTenantID, "lab")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	body := `{"code":"` + code.Code + `","desktopd_key":"key-1","srv_admin_secret":"srv-admin","version":"v1","has_desktopd":true,"has_srv":true}`
	PostEdgeEnrollHandler(reg, pool, bots).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/edge/enroll", strings.NewReader(body)))
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "srv connection was not recorded") {
		t.Fatalf("enroll %d %s", rec.Code, rec.Body.String())
	}
	nodes, err := reg.List(ctx, store.DefaultTenantID)
	if err != nil || len(nodes) != 0 {
		t.Fatalf("nodes = %+v err %v", nodes, err)
	}
	view, err := pool.View(ctx, store.DefaultTenantID)
	if err != nil || len(view.Servers) != 0 {
		t.Fatalf("servers = %+v err %v", view.Servers, err)
	}
}

func TestOwnedEnrollRollbackDetachesTheDesktopAssignment(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	settings := &edgeMemSettings{}
	reg := edgenode.NewRegistry(settings)
	pool := desktoppool.New(settings, nil)
	bots := botmgmt.NewService(failingBotSettings{})
	ctx := context.Background()
	code, err := reg.IssueOwnedCode(ctx, store.DefaultTenantID, "alice", "alice-pc")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	body := `{"code":"` + code.Code + `","desktopd_key":"key-1","srv_admin_secret":"srv-admin","version":"v1","has_desktopd":true,"has_srv":true,"host_ram_bytes":15032385536,"host_cpus":8}`
	PostEdgeEnrollHandler(reg, pool, bots).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/edge/enroll", strings.NewReader(body)))
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "srv connection was not recorded") {
		t.Fatalf("enroll %d %s", rec.Code, rec.Body.String())
	}
	servers, active, err := pool.OwnedServers(ctx, store.DefaultTenantID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 0 || active != "" {
		t.Fatalf("rollback left owned desktop servers=%+v active=%q", servers, active)
	}
	view, err := pool.View(ctx, store.DefaultTenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Servers) != 0 || len(view.Assignments) != 0 {
		t.Fatalf("rollback left pool state servers=%+v assignments=%+v", view.Servers, view.Assignments)
	}
}

func TestRotationUpgradeRollbackRemovesTheNewDesktopRecord(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	settings := &edgeMemSettings{}
	reg := edgenode.NewRegistry(settings)
	pool := desktoppool.New(settings, nil)
	ctx := context.Background()
	code, err := reg.IssueOwnedCode(ctx, store.DefaultTenantID, "alice", "alice-pc")
	if err != nil {
		t.Fatal(err)
	}
	goodBots := botmgmt.NewService(&botSettingsMem{})
	firstRec := httptest.NewRecorder()
	firstBody := `{"code":"` + code.Code + `","srv_admin_secret":"srv-1","version":"v1","has_srv":true}`
	PostEdgeEnrollHandler(reg, pool, goodBots).ServeHTTP(firstRec, httptest.NewRequest(http.MethodPost, "/api/v1/edge/enroll", strings.NewReader(firstBody)))
	if firstRec.Code != http.StatusCreated {
		t.Fatalf("srv-only enroll %d %s", firstRec.Code, firstRec.Body.String())
	}
	var first edgenode.EnrollResult
	if err := json.Unmarshal(firstRec.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	rotate, err := reg.IssueRotateCode(ctx, store.DefaultTenantID, first.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	failingBots := botmgmt.NewService(failingBotSettings{})
	rec := httptest.NewRecorder()
	upgradeBody := `{"code":"` + rotate.Code + `","desktopd_key":"key-2","srv_admin_secret":"srv-2","version":"v2","has_desktopd":true,"has_srv":true,"host_ram_bytes":15032385536,"host_cpus":8}`
	PostEdgeEnrollHandler(reg, pool, failingBots).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/edge/enroll", strings.NewReader(upgradeBody)))
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "srv connection was not recorded") {
		t.Fatalf("rotation %d %s", rec.Code, rec.Body.String())
	}
	node, err := reg.Lookup(ctx, first.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	if node.HasDesktopd || node.DesktopdKey != "" {
		t.Fatalf("rollback node still has desktop: %+v", node)
	}
	servers, active, err := pool.OwnedServers(ctx, store.DefaultTenantID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 0 || active != "" {
		t.Fatalf("rotation rollback left servers=%+v active=%q", servers, active)
	}
}

func TestSrvOnlyEnrollmentDoesNotBecomeTheDefaultBotHome(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	settings := &edgeMemSettings{}
	reg := edgenode.NewRegistry(settings)
	pool := desktoppool.New(settings, nil)
	botSettings := &botSettingsMem{}
	bots := botmgmt.NewService(botSettings)
	ctx := context.Background()
	code, err := reg.IssueOwnedCode(ctx, store.DefaultTenantID, "alice", "srv only")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	body := `{"code":"` + code.Code + `","srv_admin_secret":"srv-admin","version":"v1","has_srv":true}`
	PostEdgeEnrollHandler(reg, pool, bots).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/edge/enroll", strings.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("enroll %d %s", rec.Code, rec.Body.String())
	}
	var stored struct {
		OwnerNode map[string]string `json:"owner_node"`
	}
	_ = json.Unmarshal([]byte(botSettings.value), &stored)
	if stored.OwnerNode["alice"] != "" {
		t.Fatalf("srv-only node became the default bot home: %+v", stored.OwnerNode)
	}
}

func TestNodeSettingsMovesTheSrvPinToTheNewOwner(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	settings := &edgeMemSettings{}
	reg := edgenode.NewRegistry(settings)
	pool := desktoppool.New(settings, nil)
	botSettings := &botSettingsMem{}
	bots := botmgmt.NewService(botSettings)
	ctx := context.Background()
	code, err := reg.IssueOwnedCode(ctx, store.DefaultTenantID, "alice", "desk")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	enroll := httptest.NewRequest(http.MethodPost, "/api/v1/edge/enroll", strings.NewReader(`{"code":"`+code.Code+`","desktopd_key":"key-1","srv_admin_secret":"srv-admin","version":"v1","has_desktopd":true,"has_srv":true}`))
	PostEdgeEnrollHandler(reg, pool, bots).ServeHTTP(rec, enroll)
	if rec.Code != http.StatusCreated {
		t.Fatalf("enroll %d %s", rec.Code, rec.Body.String())
	}
	var first edgenode.EnrollResult
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	pins := func() map[string]string {
		var rec struct {
			OwnerNode map[string]string `json:"owner_node"`
		}
		_ = json.Unmarshal([]byte(botSettings.value), &rec)
		return rec.OwnerNode
	}
	if pins()["alice"] != first.NodeID {
		t.Fatalf("alice pin = %+v", pins())
	}
	out := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/bots/nodes/"+first.NodeID+"/settings", strings.NewReader(`{"name":"desk","owner_user_id":"bob"}`))
	req.SetPathValue("id", first.NodeID)
	PostEdgeNodeSettingsAdminHandler(reg, pool, bots).ServeHTTP(out, req)
	if out.Code != http.StatusOK {
		t.Fatalf("settings %d %s", out.Code, out.Body.String())
	}
	if _, ok := pins()["alice"]; ok {
		t.Fatalf("alice kept her pin to an exclusive node: %+v", pins())
	}
	if pins()["bob"] != first.NodeID {
		t.Fatalf("bob was not pinned: %+v", pins())
	}
	// Shared again: pins stay because they still resolve for a shared
	// desktop. Assigning a user to it pins them too.
	out = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/admin/bots/nodes/"+first.NodeID+"/settings", strings.NewReader(`{"name":"desk","owner_user_id":""}`))
	req.SetPathValue("id", first.NodeID)
	PostEdgeNodeSettingsAdminHandler(reg, pool, bots).ServeHTTP(out, req)
	if out.Code != http.StatusOK {
		t.Fatalf("share %d %s", out.Code, out.Body.String())
	}
	if pins()["bob"] != first.NodeID {
		t.Fatalf("a shared node dropped bob's pin: %+v", pins())
	}
	if err := bots.PinOwnerNode(ctx, store.DefaultTenantID, "alice", first.NodeID); err != nil {
		t.Fatal(err)
	}
	// Exclusive to carol strands every other pin on this node.
	out = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/admin/bots/nodes/"+first.NodeID+"/settings", strings.NewReader(`{"name":"desk","owner_user_id":"carol"}`))
	req.SetPathValue("id", first.NodeID)
	PostEdgeNodeSettingsAdminHandler(reg, pool, bots).ServeHTTP(out, req)
	if out.Code != http.StatusOK {
		t.Fatalf("settings %d %s", out.Code, out.Body.String())
	}
	for _, user := range []string{"alice", "bob"} {
		if _, ok := pins()[user]; ok {
			t.Fatalf("%s kept a pin to an exclusive node: %+v", user, pins())
		}
	}
	if pins()["carol"] != first.NodeID {
		t.Fatalf("carol was not pinned: %+v", pins())
	}
}

type gateBotSettings struct {
	botSettingsMem
	fail bool
}

func (m *gateBotSettings) Set(ctx context.Context, key, valueJSON string) error {
	if m.fail {
		return errors.New("store down")
	}
	return m.botSettingsMem.Set(ctx, key, valueJSON)
}

func TestNodeOwnerChangeRollsBackWhenTheNewPinIsNotStored(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	settings := &edgeMemSettings{}
	reg := edgenode.NewRegistry(settings)
	pool := desktoppool.New(settings, nil)
	storeMem := &gateBotSettings{}
	bots := botmgmt.NewService(storeMem)
	ctx := context.Background()
	code, err := reg.IssueOwnedCode(ctx, store.DefaultTenantID, "alice", "desk")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	body := `{"code":"` + code.Code + `","desktopd_key":"key-1","srv_admin_secret":"srv-admin","version":"v1","has_desktopd":true,"has_srv":true}`
	PostEdgeEnrollHandler(reg, pool, bots).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/edge/enroll", strings.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("enroll %d %s", rec.Code, rec.Body.String())
	}
	var enrolled edgenode.EnrollResult
	if err := json.Unmarshal(rec.Body.Bytes(), &enrolled); err != nil {
		t.Fatal(err)
	}
	storeMem.fail = true
	out := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/bots/nodes/"+enrolled.NodeID+"/settings", strings.NewReader(`{"name":"desk","owner_user_id":"bob"}`))
	req.SetPathValue("id", enrolled.NodeID)
	PostEdgeNodeSettingsAdminHandler(reg, pool, bots).ServeHTTP(out, req)
	if out.Code != http.StatusBadGateway || !strings.Contains(out.Body.String(), "NODE_PIN") {
		t.Fatalf("settings %d %s", out.Code, out.Body.String())
	}
	node, err := reg.Lookup(ctx, enrolled.NodeID)
	if err != nil || node.OwnerUserID != "alice" {
		t.Fatalf("owner = %+v err %v", node, err)
	}
	var saved struct {
		OwnerNode map[string]string `json:"owner_node"`
	}
	if err := json.Unmarshal([]byte(storeMem.value), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.OwnerNode["alice"] != enrolled.NodeID || saved.OwnerNode["bob"] != "" {
		t.Fatalf("pins = %+v", saved.OwnerNode)
	}
	view, err := pool.View(ctx, store.DefaultTenantID)
	if err != nil {
		t.Fatal(err)
	}
	for _, server := range view.Servers {
		if server.NodeID == enrolled.NodeID && server.OwnerID != "alice" {
			t.Fatalf("desktop owner = %s", server.OwnerID)
		}
	}
}
