package qqbot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestStopClosesIdleSocketImmediately(t *testing.T) {
	hold := make(chan struct{})
	defer close(hold)

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "tok",
			"expires_in":   "7200",
		})
	})
	mux.HandleFunc("/gateway", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"url": "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws",
		})
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if err := conn.WriteJSON(map[string]any{
			"op": 10,
			"d":  map[string]any{"heartbeat_interval": 60000},
		}); err != nil {
			return
		}
		var identify map[string]any
		if err := conn.ReadJSON(&identify); err != nil {
			return
		}
		if err := conn.WriteJSON(map[string]any{
			"op": 0,
			"t":  "READY",
			"d":  map[string]any{"session_id": "s1"},
		}); err != nil {
			return
		}
		<-hold
	})

	gw := NewGateway(Config{AppID: "1020", AppSecret: "secret"}, func(IncomingMessage) {})
	gw.setTestEndpoints(srv.URL+"/token", srv.URL)

	connected := make(chan struct{})
	var once sync.Once
	gw.SetStatusCallback(func(status string) {
		if status == "connected" {
			once.Do(func() { close(connected) })
		}
	})
	if err := gw.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer gw.Stop()

	select {
	case <-connected:
	case <-time.After(3 * time.Second):
		t.Fatal("gateway did not reach connected")
	}

	started := time.Now()
	if err := gw.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("Stop took %s; idle read should close with the context, not the next heartbeat", elapsed)
	}
}

func TestStopReturnsWhileMessageHandlerIsRunning(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": "7200"})
	})
	mux.HandleFunc("/gateway", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"url": "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws",
		})
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if err := conn.WriteJSON(map[string]any{"op": 10, "d": map[string]any{"heartbeat_interval": 60000}}); err != nil {
			return
		}
		var identify map[string]any
		if err := conn.ReadJSON(&identify); err != nil {
			return
		}
		if err := conn.WriteJSON(map[string]any{"op": 0, "t": "READY", "d": map[string]any{"session_id": "s1"}}); err != nil {
			return
		}
		_ = conn.WriteJSON(map[string]any{
			"op": 0,
			"t":  "C2C_MESSAGE_CREATE",
			"d": map[string]any{
				"id":      "m1",
				"content": "hi",
				"author":  map[string]any{"user_openid": "oid"},
			},
		})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})

	entered := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	gw := NewGateway(Config{AppID: "1020", AppSecret: "secret"}, func(IncomingMessage) {
		close(entered)
		<-release
	})
	gw.setTestEndpoints(srv.URL+"/token", srv.URL)
	connected := make(chan struct{})
	var once sync.Once
	gw.SetStatusCallback(func(status string) {
		if status == "connected" {
			once.Do(func() { close(connected) })
		}
	})
	if err := gw.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer gw.Stop()

	select {
	case <-connected:
	case <-time.After(3 * time.Second):
		t.Fatal("gateway did not reach connected")
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("handler was not entered")
	}

	started := time.Now()
	if err := gw.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("Stop took %s while a message handler was still running", elapsed)
	}
}
