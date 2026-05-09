package lansenger

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestReconnectBackoffDelayCapsAtMax(t *testing.T) {
	tests := []struct {
		name    string
		attempt int
		want    time.Duration
	}{
		{name: "zero", attempt: 0, want: 0},
		{name: "first attempt", attempt: 1, want: 3 * time.Second},
		{name: "linear before cap", attempt: 3, want: 9 * time.Second},
		{name: "cap", attempt: 100, want: lansengerMaxBackoff},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reconnectBackoffDelay(tt.attempt); got != tt.want {
				t.Fatalf("reconnectBackoffDelay(%d) = %v, want %v", tt.attempt, got, tt.want)
			}
		})
	}
}

func TestGetWebSocketURLUsesConfiguredWebSocketFallback(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/ws/endpoint/create" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data":    map[string]string{"wsEndpoint": ""},
		})
	}))
	defer api.Close()

	gw := NewGateway(Config{
		AppID:            "app-1",
		AppSecret:        "secret-1",
		ApiGatewayURL:    api.URL,
		WebSocketBaseURL: "https://ws.example.test/base/",
	}, nil)

	got, err := gw.getWebSocketURL(context.Background())
	if err != nil {
		t.Fatalf("getWebSocketURL: %v", err)
	}
	want := "wss://ws.example.test/base/open-apis/im/v1/ws/app-1"
	if got != want {
		t.Fatalf("getWebSocketURL() = %q, want %q", got, want)
	}
}

func TestDoPostRetriesTransientHTTPError(t *testing.T) {
	attempts := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, "temporary", http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 0})
	}))
	defer api.Close()

	gw := NewGateway(Config{}, nil)
	if err := gw.doPost(context.Background(), api.URL, []byte(`{"hello":"world"}`)); err != nil {
		t.Fatalf("doPost: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestDoPostDoesNotRetryClientError(t *testing.T) {
	attempts := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer api.Close()

	gw := NewGateway(Config{}, nil)
	if err := gw.doPost(context.Background(), api.URL, []byte(`{"hello":"world"}`)); err == nil {
		t.Fatal("expected error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestGetWebSocketURLHonorsCanceledContext(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("request should not reach server")
	}))
	defer api.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	gw := NewGateway(Config{
		AppID:         "app-1",
		AppSecret:     "secret-1",
		ApiGatewayURL: api.URL,
	}, nil)

	if _, err := gw.getWebSocketURL(ctx); err == nil {
		t.Fatal("expected canceled context error")
	}
}

func TestReadLoopExitsPromptlyWhenContextCanceled(t *testing.T) {
	upgrader := websocket.Upgrader{}
	accepted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		close(accepted)
		<-r.Context().Done()
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("server did not accept websocket")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	gw := NewGateway(Config{}, nil)
	go func() {
		gw.readLoop(ctx, conn)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("readLoop did not exit promptly after context cancellation")
	}
}

func TestConnectLoopClearsRunningOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	gw := NewGateway(Config{}, nil)
	gw.running = true
	gw.wg.Add(1)
	go gw.connectLoop(ctx)
	gw.wg.Wait()

	if gw.IsRunning() {
		t.Fatal("gateway still reports running after connectLoop context cancellation")
	}
}
