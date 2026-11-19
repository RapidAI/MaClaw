package httpapi

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/chat"
	"github.com/gorilla/websocket"
	_ "modernc.org/sqlite"
)

type mobileRealtimeDeadlineWriter struct {
	mobileRealtimeFakeWriter
	deadline time.Time
}

func (w *mobileRealtimeDeadlineWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadline = deadline
	return nil
}

func TestMobileRealtimeBroadcastSetsWriteDeadline(t *testing.T) {
	mobileRealtimeClients.Lock()
	previous := mobileRealtimeClients.clients
	mobileRealtimeClients.clients = make(map[string]map[*mobileRealtimeClient]struct{})
	mobileRealtimeClients.Unlock()
	t.Cleanup(func() {
		mobileRealtimeClients.Lock()
		mobileRealtimeClients.clients = previous
		mobileRealtimeClients.Unlock()
	})
	writer := &mobileRealtimeDeadlineWriter{}
	_, cleanup := mobileRealtimeRegister("tenant-1", "user-1", writer)
	defer cleanup()
	mobileRealtimeBroadcast("tenant-1", "user-1", map[string]any{"type": "test"})
	if writer.deadline.IsZero() {
		t.Fatal("broadcast did not set a write deadline")
	}
}

func TestChatWSRejectsOversizedFrame(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, _ := issueViewerToken(t, identity, "chat-ws-limit@example.com")
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := chat.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(ChatWSHandler(identity, chat.NewChannelService(store), chat.NewNotifier(store, nil)))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]string{"type": "auth", "token": token}); err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := conn.ReadJSON(&response); err != nil || response["type"] != "auth_ok" {
		t.Fatalf("auth response = %#v, err=%v", response, err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_ = conn.WriteJSON(map[string]string{"type": "ping", "data": strings.Repeat("x", 65<<10)})
	err = conn.ReadJSON(&response)
	if !websocket.IsCloseError(err, websocket.CloseMessageTooBig) {
		t.Fatalf("oversized chat frame error = %v, want close 1009", err)
	}
}

func TestChatWSPingAndNotificationsSerializeWrites(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enrollment := issueViewerToken(t, identity, "chat-ws-writes@example.com")
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := chat.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	notifier := chat.NewNotifier(store, nil)
	server := httptest.NewServer(ChatWSHandler(identity, chat.NewChannelService(store), notifier))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(map[string]string{"type": "auth", "token": token}); err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := conn.ReadJSON(&response); err != nil || response["type"] != "auth_ok" {
		t.Fatalf("auth response = %#v, err=%v", response, err)
	}
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			if err := conn.ReadJSON(&response); err != nil {
				return
			}
		}
	}()
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				notifier.NotifyCallEventForTenant(enrollment.TenantID, enrollment.UserID, chat.WsHint{Type: "test_hint"}, "", "")
			}
		}()
	}
	for i := 0; i < 50; i++ {
		if err := conn.WriteJSON(map[string]string{"type": "ping"}); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
	_ = conn.Close()
	<-readerDone
}

type mobileRealtimeCloseWriter struct {
	mobileRealtimeFakeWriter
	closed bool
}

func (w *mobileRealtimeCloseWriter) Close() error {
	w.closed = true
	return nil
}

func (w *mobileRealtimeCloseWriter) SendJSON(v any) error {
	return w.WriteJSON(v)
}

func TestChatNotifierCloseUserAndTenant(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := chat.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	notifier := chat.NewNotifier(store, nil)
	first := &mobileRealtimeCloseWriter{}
	second := &mobileRealtimeCloseWriter{}
	notifier.RegisterForTenant("tenant-1", "user-1", first)
	notifier.RegisterForTenant("tenant-1", "user-2", second)

	notifier.CloseUserForTenant("tenant-1", "user-1")
	if !first.closed || notifier.IsOnlineForTenant("tenant-1", "user-1") {
		t.Fatal("user chat connection was not closed and marked offline")
	}
	if second.closed || !notifier.IsOnlineForTenant("tenant-1", "user-2") {
		t.Fatal("another user's chat connection was closed by user revocation")
	}
	notifier.CloseTenant("tenant-1")
	if !second.closed || notifier.IsOnlineForTenant("tenant-1", "user-2") {
		t.Fatal("tenant chat connection was not closed and marked offline")
	}
}

func TestMobileRealtimeCloseTenant(t *testing.T) {
	mobileRealtimeClients.Lock()
	previous := mobileRealtimeClients.clients
	mobileRealtimeClients.clients = make(map[string]map[*mobileRealtimeClient]struct{})
	mobileRealtimeClients.Unlock()
	t.Cleanup(func() {
		mobileRealtimeClients.Lock()
		mobileRealtimeClients.clients = previous
		mobileRealtimeClients.Unlock()
	})
	writer := &mobileRealtimeCloseWriter{}
	_, _ = mobileRealtimeRegister("tenant-1", "user-1", writer)
	mobileRealtimeCloseTenant("tenant-1")
	if !writer.closed {
		t.Fatal("mobile realtime connection was not closed")
	}
	mobileRealtimeClients.Lock()
	remaining := len(mobileRealtimeClients.clients[mobileRealtimeKey("tenant-1", "user-1")])
	mobileRealtimeClients.Unlock()
	if remaining != 0 {
		t.Fatalf("mobile realtime client remained registered: %d", remaining)
	}
}

func TestMobileRealtimeCloseUser(t *testing.T) {
	mobileRealtimeClients.Lock()
	previous := mobileRealtimeClients.clients
	mobileRealtimeClients.clients = make(map[string]map[*mobileRealtimeClient]struct{})
	mobileRealtimeClients.Unlock()
	t.Cleanup(func() {
		mobileRealtimeClients.Lock()
		mobileRealtimeClients.clients = previous
		mobileRealtimeClients.Unlock()
	})
	writer := &mobileRealtimeCloseWriter{}
	_, _ = mobileRealtimeRegister("tenant-1", "user-1", writer)
	mobileRealtimeCloseUser("tenant-1", "user-1")
	if !writer.closed {
		t.Fatal("user realtime connection was not closed")
	}
}

func TestMobileRealtimeClientLimitAndCleanup(t *testing.T) {
	mobileRealtimeClients.Lock()
	previous := mobileRealtimeClients.clients
	mobileRealtimeClients.clients = make(map[string]map[*mobileRealtimeClient]struct{})
	mobileRealtimeClients.Unlock()
	t.Cleanup(func() {
		mobileRealtimeClients.Lock()
		mobileRealtimeClients.clients = previous
		mobileRealtimeClients.Unlock()
	})
	cleanups := make([]func(), 0, mobileRealtimeMaxClientsPerUser)
	for i := 0; i < mobileRealtimeMaxClientsPerUser; i++ {
		client, cleanup := mobileRealtimeRegister("tenant-1", "user-1", &mobileRealtimeFakeWriter{})
		if client == nil {
			t.Fatal("valid realtime client was rejected")
		}
		cleanups = append(cleanups, cleanup)
	}
	client, _ := mobileRealtimeRegister("tenant-1", "user-1", &mobileRealtimeFakeWriter{})
	if client != nil {
		t.Fatal("realtime client above the per-user bound was accepted")
	}
	mobileRealtimeClients.Lock()
	remaining := len(mobileRealtimeClients.clients[mobileRealtimeKey("tenant-1", "user-1")])
	mobileRealtimeClients.Unlock()
	if remaining != mobileRealtimeMaxClientsPerUser {
		t.Fatalf("registered clients = %d, want %d", remaining, mobileRealtimeMaxClientsPerUser)
	}
	for _, cleanup := range cleanups {
		cleanup()
	}
	mobileRealtimeClients.Lock()
	remaining = len(mobileRealtimeClients.clients[mobileRealtimeKey("tenant-1", "user-1")])
	mobileRealtimeClients.Unlock()
	if remaining != 0 {
		t.Fatalf("clients remained after cleanup: %d", remaining)
	}
}

func TestMobileRealtimeInputBackpressure(t *testing.T) {
	previous := mobileRealtimeInputSlots
	mobileRealtimeInputSlots = make(chan struct{}, 1)
	t.Cleanup(func() { mobileRealtimeInputSlots = previous })
	if !mobileRealtimeInputAdmit() {
		t.Fatal("first input was not admitted")
	}
	if mobileRealtimeInputAdmit() {
		t.Fatal("input was admitted above the configured bound")
	}
	mobileRealtimeInputRelease()
	if !mobileRealtimeInputAdmit() {
		t.Fatal("released input slot was not reusable")
	}
	mobileRealtimeInputRelease()
}

func TestMobileRealtimeRejectsOversizedFrame(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, _ := issueViewerToken(t, identity, "mobile-ws-limit@example.com")
	server := httptest.NewServer(MobileRealtimeHandler(identity))
	defer server.Close()
	header := http.Header{"Authorization": []string{"Bearer " + token}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var response map[string]any
	if err := conn.ReadJSON(&response); err != nil || response["type"] != "ready" {
		t.Fatalf("ready response = %#v, err=%v", response, err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_ = conn.WriteJSON(map[string]string{"type": "ping", "data": strings.Repeat("x", (1<<20)+1)})
	err = conn.ReadJSON(&response)
	if !websocket.IsCloseError(err, websocket.CloseMessageTooBig) {
		t.Fatalf("oversized mobile frame error = %v, want close 1009", err)
	}
}
