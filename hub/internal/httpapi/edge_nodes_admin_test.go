package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

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
