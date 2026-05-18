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

func TestLansengerConnectionAgeExceeded(t *testing.T) {
	now := time.Now()
	if lansengerConnectionAgeExceeded(time.Time{}, now) {
		t.Fatal("zero connectedAt should not exceed max age")
	}
	if lansengerConnectionAgeExceeded(now.Add(time.Minute), now) {
		t.Fatal("future connectedAt should not exceed max age")
	}
	if lansengerConnectionAgeExceeded(now.Add(-lansengerMaxConnAge+time.Second), now) {
		t.Fatal("connection younger than max age should not exceed")
	}
	if !lansengerConnectionAgeExceeded(now.Add(-lansengerMaxConnAge), now) {
		t.Fatal("connection at max age should exceed")
	}
	if !lansengerConnectionAgeExceeded(now.Add(-lansengerMaxConnAge-time.Second), now) {
		t.Fatal("connection older than max age should exceed")
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

func TestGetAppTokenRetriesTransientHTTPError(t *testing.T) {
	attempts := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, "temporary", http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data": map[string]any{
				"appToken":  "tok-1",
				"expiresIn": 3600,
			},
		})
	}))
	defer api.Close()

	gw := NewGateway(Config{AppID: "app-1", AppSecret: "secret-1", ApiGatewayURL: api.URL}, nil)
	tok, err := gw.getAppToken(context.Background())
	if err != nil {
		t.Fatalf("getAppToken: %v", err)
	}
	if tok != "tok-1" {
		t.Fatalf("token = %q, want tok-1", tok)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestGetWebSocketURLRetriesTransientHTTPError(t *testing.T) {
	attempts := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, "temporary", http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data":    map[string]string{"wsEndpoint": "wss://ws.example.test/live"},
		})
	}))
	defer api.Close()

	gw := NewGateway(Config{AppID: "app-1", AppSecret: "secret-1", ApiGatewayURL: api.URL}, nil)
	got, err := gw.getWebSocketURL(context.Background())
	if err != nil {
		t.Fatalf("getWebSocketURL: %v", err)
	}
	if got != "wss://ws.example.test/live" {
		t.Fatalf("getWebSocketURL() = %q", got)
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

func TestSendTextRefreshesExpiredTokenAndRetriesOnce(t *testing.T) {
	tokenAttempts := 0
	sendAttempts := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/apptoken/create":
			tokenAttempts++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errCode": 0,
				"data": map[string]any{
					"appToken":  "tok-1",
					"expiresIn": 3600,
				},
			})
		case "/v1/bot/messages/create":
			sendAttempts++
			if sendAttempts == 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 42001, "errMsg": "token expired"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 0})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer api.Close()

	gw := NewGateway(Config{AppID: "app-1", AppSecret: "secret-1", ApiGatewayURL: api.URL}, nil)
	if err := gw.SendText(context.Background(), OutgoingText{ToUserID: "user-1", Text: "hello"}); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if tokenAttempts != 2 {
		t.Fatalf("tokenAttempts = %d, want 2", tokenAttempts)
	}
	if sendAttempts != 2 {
		t.Fatalf("sendAttempts = %d, want 2", sendAttempts)
	}
}

func TestSendMediaRefreshesExpiredTokenDuringUploadAndRetriesOnce(t *testing.T) {
	tokenAttempts := 0
	uploadAttempts := 0
	sendAttempts := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/apptoken/create":
			tokenAttempts++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errCode": 0,
				"data": map[string]any{
					"appToken":  "tok-1",
					"expiresIn": 3600,
				},
			})
		case "/v1/medias/create":
			uploadAttempts++
			if uploadAttempts == 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 42001, "errMsg": "token expired"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errCode": 0,
				"data":    map[string]string{"mediaId": "media-1"},
			})
		case "/v1/bot/messages/create":
			sendAttempts++
			_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 0})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer api.Close()

	gw := NewGateway(Config{AppID: "app-1", AppSecret: "secret-1", ApiGatewayURL: api.URL}, nil)
	if err := gw.SendMedia(context.Background(), OutgoingMedia{ToUserID: "user-1", FileData: []byte("png"), FileName: "a.png", MediaType: "image"}); err != nil {
		t.Fatalf("SendMedia: %v", err)
	}
	if tokenAttempts != 2 {
		t.Fatalf("tokenAttempts = %d, want 2", tokenAttempts)
	}
	if uploadAttempts != 2 {
		t.Fatalf("uploadAttempts = %d, want 2", uploadAttempts)
	}
	if sendAttempts != 1 {
		t.Fatalf("sendAttempts = %d, want 1", sendAttempts)
	}
}

func TestIsLansengerTokenExpiredError(t *testing.T) {
	if !isLansengerTokenExpiredError(&lansengerAPIError{Code: 42001, Msg: "expired"}) {
		t.Fatal("expected 42001 to be treated as token expired")
	}
	if !isLansengerTokenExpiredError(&lansengerAPIError{Code: 123, Msg: "app token invalid"}) {
		t.Fatal("expected token invalid message to be treated as token expired")
	}
	if isLansengerTokenExpiredError(&lansengerAPIError{Code: 123, Msg: "permission denied"}) {
		t.Fatal("permission denied should not be treated as token expired")
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
