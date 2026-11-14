package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/gorilla/websocket"
)

type countingAuthIdentity struct {
	viewerTokens atomic.Int32
}

func (s *countingAuthIdentity) AuthenticateMachine(_ context.Context, machineID, _ string) (*auth.MachinePrincipal, error) {
	return &auth.MachinePrincipal{TenantID: store.DefaultTenantID, UserID: "user-1", MachineID: machineID}, nil
}

func (s *countingAuthIdentity) AuthenticateViewer(context.Context, string) (*auth.ViewerPrincipal, error) {
	return &auth.ViewerPrincipal{TenantID: store.DefaultTenantID, UserID: "user-1", Email: "viewer@example.com"}, nil
}

func (s *countingAuthIdentity) IssueViewerTokenForUser(context.Context, string) (string, error) {
	s.viewerTokens.Add(1)
	return "viewer-token", nil
}

func TestGatewayRejectsRepeatedAuthenticationAndRoleSwitch(t *testing.T) {
	for _, secondType := range []string{"auth.machine", "auth.viewer"} {
		t.Run(secondType, func(t *testing.T) {
			identity := &countingAuthIdentity{}
			gateway := NewGateway(identity, &testDeviceBinder{}, &testSessionService{})
			server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
			defer server.Close()
			conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteJSON(map[string]any{
				"type": "auth.machine", "payload": map[string]any{"machine_id": "machine-1", "machine_token": "token"},
			}); err != nil {
				t.Fatal(err)
			}
			var response Envelope
			if err := conn.ReadJSON(&response); err != nil || response.Type != "auth.ok" {
				t.Fatalf("initial authentication response=%#v err=%v", response, err)
			}

			var payload any = map[string]any{"machine_id": "machine-1", "machine_token": "token"}
			if secondType == "auth.viewer" {
				payload = map[string]any{"access_token": "viewer-token"}
			}
			if err := conn.WriteJSON(map[string]any{"type": secondType, "payload": payload}); err != nil {
				t.Fatal(err)
			}
			if err := conn.ReadJSON(&response); err != nil {
				t.Fatal(err)
			}
			var body struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(response.Payload, &body); err != nil {
				t.Fatal(err)
			}
			if response.Type != "error" || body.Code != "ALREADY_AUTHENTICATED" {
				t.Fatalf("reauthentication response=%#v code=%q", response, body.Code)
			}
			if got := identity.viewerTokens.Load(); got != 1 {
				t.Fatalf("viewer tokens issued = %d, want exactly one per connection", got)
			}
		})
	}
}
