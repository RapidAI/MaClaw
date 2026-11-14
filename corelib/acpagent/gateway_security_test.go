package acpagent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/remote"
)

func TestGatewayClientRejectsCleartextPublicEndpoint(t *testing.T) {
	t.Setenv(remote.HubAllowHTTPEnv, "")
	client := NewGatewayClient(GatewayEndpoint{BaseURL: "http://192.0.2.1/api", Token: "secret"})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := client.Health(ctx)
	if err == nil || !strings.Contains(err.Error(), remote.HubAllowHTTPEnv) {
		t.Fatalf("public cleartext health error = %v, want protocol rejection", err)
	}
}

func TestGatewayClientRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", (16<<20)+1)))
	}))
	defer server.Close()
	client := NewGatewayClient(GatewayEndpoint{BaseURL: server.URL, Token: "secret"})
	_, err := client.Health(context.Background())
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized gateway response error = %v", err)
	}
}

func TestGatewayErrorResponsePreviewIsBounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(strings.Repeat("x", 8192)))
	}))
	defer server.Close()
	client := NewGatewayClient(GatewayEndpoint{BaseURL: server.URL, Token: "secret"})
	_, err := client.Health(context.Background())
	if err == nil {
		t.Fatal("gateway error response was not returned")
	}
	if len(err.Error()) > 5000 || strings.Contains(err.Error(), strings.Repeat("x", 8192)) {
		t.Fatalf("gateway error included an unbounded response body: length=%d", len(err.Error()))
	}
}
