package device

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/session"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/ws"
	"github.com/gorilla/websocket"
)

type tenantAuthIdentity struct{}

func (tenantAuthIdentity) AuthenticateMachine(_ context.Context, machineID, _ string) (*auth.MachinePrincipal, error) {
	return &auth.MachinePrincipal{TenantID: store.DefaultTenantID, UserID: "user-1", MachineID: machineID}, nil
}
func (tenantAuthIdentity) AuthenticateViewer(context.Context, string) (*auth.ViewerPrincipal, error) {
	return &auth.ViewerPrincipal{TenantID: store.DefaultTenantID, UserID: "user-1", Email: "viewer@example.com"}, nil
}
func (tenantAuthIdentity) IssueViewerTokenForUser(context.Context, string) (string, error) {
	return "viewer-token", nil
}

func TestMachineConnectionClosedOnTenantRevoke(t *testing.T) {
	devices := NewService(nil, NewRuntime())
	gateway := ws.NewGateway(tenantAuthIdentity{}, devices, session.NewService(session.NewCache(), nil))
	server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]any{"type": "auth.machine", "payload": map[string]any{"machine_id": "machine-1", "machine_token": "token"}}); err != nil {
		t.Fatal(err)
	}
	var response ws.Envelope
	if err := conn.ReadJSON(&response); err != nil || response.Type != "auth.ok" {
		t.Fatalf("machine auth response = %#v, err=%v", response, err)
	}
	devices.CloseTenant(store.DefaultTenantID)
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := conn.ReadJSON(&response); err == nil {
		t.Fatal("machine connection remained open after tenant revocation")
	}
}

func TestViewerConnectionClosedOnTenantRevoke(t *testing.T) {
	devices := NewService(nil, NewRuntime())
	gateway := ws.NewGateway(tenantAuthIdentity{}, devices, session.NewService(session.NewCache(), nil))
	server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]any{"type": "auth.viewer", "payload": map[string]any{"access_token": "viewer-token"}}); err != nil {
		t.Fatal(err)
	}
	var response ws.Envelope
	if err := conn.ReadJSON(&response); err != nil || response.Type != "auth.ok" {
		t.Fatalf("viewer auth response = %#v, err=%v", response, err)
	}
	gateway.CloseTenantViewers(store.DefaultTenantID)
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := conn.ReadJSON(&response); err == nil {
		t.Fatal("viewer connection remained open after tenant revocation")
	}
}
