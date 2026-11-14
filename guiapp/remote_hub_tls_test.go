package guiapp

import (
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/gorilla/websocket"
)

func TestDefaultHubDialVerifiesWSSCertificate(t *testing.T) {
	t.Setenv(remote.HubTLSInsecureEnv, "")
	t.Setenv(remote.HubCAFileEnv, "")
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer server.Close()
	wsURL := "wss" + strings.TrimPrefix(server.URL, "https")

	conn, err := defaultHubDial(wsURL)
	if err == nil {
		conn.Close()
		t.Fatal("WSS accepted an untrusted self-signed certificate")
	}

	caFile := filepath.Join(t.TempDir(), "hub-ca.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(caFile, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(remote.HubCAFileEnv, caFile)
	conn, err = defaultHubDial(wsURL)
	if err != nil {
		t.Fatalf("WSS rejected the explicitly trusted Hub CA: %v", err)
	}
	conn.Close()
}

func TestDefaultHubDialRejectsCleartextPublicHub(t *testing.T) {
	t.Setenv(remote.HubAllowHTTPEnv, "")
	conn, err := defaultHubDial("ws://192.0.2.1/ws")
	if err == nil {
		conn.Close()
		t.Fatal("cleartext public WebSocket connection was attempted")
	}
	if !strings.Contains(err.Error(), remote.HubAllowHTTPEnv) {
		t.Fatalf("error = %v, want cleartext policy failure", err)
	}
}
