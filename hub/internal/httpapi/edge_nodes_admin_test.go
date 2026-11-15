package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

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
	DeleteEdgeNodeAdminHandler(reg, broker, nil).ServeHTTP(rec, del)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status %d body %s", rec.Code, rec.Body.String())
	}
	if _, err := reg.Lookup(context.Background(), enrolled.NodeID); err == nil {
		t.Fatal("deleted node is still registered")
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
	DeleteEdgeNodeAdminHandler(reg, nil, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if _, err := reg.Lookup(context.Background(), enrolled.NodeID); err != nil {
		t.Fatal("another tenant's node was removed")
	}
}
