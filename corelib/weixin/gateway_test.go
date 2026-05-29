package weixin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGatewayStartEmitsConnectingBeforeConnected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ret":0,"errcode":0}`))
	}))
	defer server.Close()

	statuses := make(chan string, 4)
	gw := NewGateway(Config{Token: "token", BaseURL: server.URL}, func(IncomingMessage) {})
	gw.SetStatusCallback(func(status string) { statuses <- status })

	if err := gw.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = gw.Stop() }()

	first := waitGatewayStatus(t, statuses)
	second := waitGatewayStatus(t, statuses)
	if first != "connecting" || second != "connected" {
		t.Fatalf("status order = %q, %q; want connecting, connected", first, second)
	}
}

func waitGatewayStatus(t *testing.T, statuses <-chan string) string {
	t.Helper()
	select {
	case status := <-statuses:
		return status
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for gateway status")
		return ""
	}
}
