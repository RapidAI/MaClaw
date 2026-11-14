package ws

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestGatewayLimitsMessageBeforeAuthentication(t *testing.T) {
	gateway := NewGateway(&testIdentityService{}, &testDeviceBinder{}, &testSessionService{})
	server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	oversized := strings.Repeat("x", int(webSocketAuthMessageLimit)+1)
	// Windows may report the peer's close on the write before the client reads
	// the close frame, so the read below is the assertion for either outcome.
	_ = conn.WriteJSON(map[string]any{"type": "auth.machine", "payload": map[string]any{"data": oversized}})
	var response Envelope
	err = conn.ReadJSON(&response)
	if !websocket.IsCloseError(err, websocket.CloseMessageTooBig) {
		t.Fatalf("oversized unauthenticated message error = %v, want close %d", err, websocket.CloseMessageTooBig)
	}
}

func TestGatewayRaisesMessageLimitAfterAuthentication(t *testing.T) {
	gateway := NewGateway(&testIdentityService{}, &testDeviceBinder{}, &testSessionService{})
	server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	if err := conn.WriteJSON(map[string]any{
		"type":    "auth.machine",
		"payload": map[string]any{"machine_id": "machine-1", "machine_token": "token"},
	}); err != nil {
		t.Fatal(err)
	}
	var authResponse Envelope
	if err := conn.ReadJSON(&authResponse); err != nil || authResponse.Type != "auth.ok" {
		t.Fatalf("machine authentication response = %#v, err=%v", authResponse, err)
	}

	if err := conn.WriteJSON(map[string]any{
		"type":    "future.large_message",
		"payload": map[string]any{"data": strings.Repeat("x", int(webSocketAuthMessageLimit)*2)},
	}); err != nil {
		t.Fatalf("authenticated message above the auth-only limit was rejected at write: %v", err)
	}
	var response Envelope
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatalf("authenticated connection did not remain usable: %v", err)
	}
	var payload struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(response.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if response.Type != "error" || payload.Code != "UNKNOWN_MESSAGE" {
		t.Fatalf("response = %#v, want UNKNOWN_MESSAGE rather than a size disconnect", response)
	}
}
