package browser

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestDiscoverTargetsUsesTheDesktopBrowser(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"login","type":"page","url":"https://mail.example/inbox","webSocketDebuggerUrl":"ws://127.0.0.1:18020/devtools/page/login"}]`))
	}))
	defer ts.Close()

	targets, err := DiscoverTargets(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets=%d", len(targets))
	}
	want := "ws" + strings.TrimPrefix(ts.URL, "http") + "/devtools/page/login"
	if targets[0].WebSocketDebugURL != want {
		t.Fatalf("websocket=%q want %q", targets[0].WebSocketDebugURL, want)
	}
	version := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"webSocketDebuggerUrl":"ws://127.0.0.1:18020/devtools/browser/login"}`))
	}))
	defer version.Close()
	got, err := browserWebSocketURL(version.URL)
	if err != nil {
		t.Fatal(err)
	}
	want = "ws" + strings.TrimPrefix(version.URL, "http") + "/devtools/browser/login"
	if got != want {
		t.Fatalf("browser websocket=%q want %q", got, want)
	}
}

func TestDiscoverTargetsIncludesHTTPStatusAndBodyLength(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not cdp"))
	}))
	defer ts.Close()

	_, err := DiscoverTargets(ts.URL)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "404") || !strings.Contains(msg, "body_len=7") || strings.Contains(msg, "not cdp") {
		t.Fatalf("DiscoverTargets error = %q", msg)
	}
}

func TestDiscoverTargetsIncludesBodyLengthOnParseError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>bad json</html>"))
	}))
	defer ts.Close()

	_, err := DiscoverTargets(ts.URL)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "parse targets") || !strings.Contains(msg, "body_len=21") || strings.Contains(msg, "bad json") {
		t.Fatalf("DiscoverTargets error = %q", msg)
	}
}

func TestIsCriticalCDPEvent(t *testing.T) {
	critical := []string{
		"Target.targetDestroyed",
		"Target.detachedFromTarget",
		"Target.targetCreated",
		"Inspector.detached",
		"Page.frameNavigated",
		"Page.frameStartedNavigating",
		"Page.loadEventFired",
	}
	for _, m := range critical {
		if !isCriticalCDPEvent(m) {
			t.Fatalf("isCriticalCDPEvent(%q) = false, want true", m)
		}
	}
	nonCritical := []string{
		"Runtime.consoleAPICalled",
		"Network.requestWillBeSent",
		"Network.loadingFinished",
		"Log.entryAdded",
		"Page.domContentEventFired",
		"",
	}
	for _, m := range nonCritical {
		if isCriticalCDPEvent(m) {
			t.Fatalf("isCriticalCDPEvent(%q) = true, want false", m)
		}
	}
}

// TestCriticalEventSurvivesEventFlood verifies end-to-end (real websocket →
// readLoop → Events channel) that a lifecycle event is still delivered when
// the event buffer is full of droppable events — the regression this guards
// is IsTargetAlive() getting stuck on true because Target.targetDestroyed
// was silently dropped during a console/network flood.
func TestCriticalEventSurvivesEventFlood(t *testing.T) {
	old := cdpEventBufferSize
	cdpEventBufferSize = 4
	defer func() { cdpEventBufferSize = old }()

	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// Flood with droppable events, then one critical event.
		for i := 0; i < 8; i++ {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"method":"Network.requestWillBeSent","params":{}}`))
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"method":"Target.targetDestroyed","params":{"targetId":"t1"}}`))
		// Give the client time to read everything before the close.
		time.Sleep(300 * time.Millisecond)
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, err := ConnectCDP(wsURL)
	if err != nil {
		t.Fatalf("ConnectCDP: %v", err)
	}
	defer client.Close()

	// Let the buffer fill (4 flood events) before draining; the critical
	// event's 100ms delivery window must still be open when we start.
	time.Sleep(50 * time.Millisecond)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case ev, ok := <-client.Events():
			if !ok {
				t.Fatal("events channel closed before the critical event was delivered")
			}
			if ev.Method == "Target.targetDestroyed" {
				return // delivered under flood pressure — pass
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("critical Target.targetDestroyed event was dropped under event flood")
}

func TestCDPClientClosedSnapshot(t *testing.T) {
	if !(*CDPClient)(nil).isClosed() {
		t.Fatal("nil client should be closed")
	}
	if (*CDPClient)(nil).IsAlive() {
		t.Fatal("nil client should not be alive")
	}
	open := &CDPClient{closed: make(chan struct{})}
	if open.isClosed() {
		t.Fatal("open channel should not look closed")
	}
	closed := &CDPClient{closed: make(chan struct{})}
	close(closed.closed)
	if !closed.isClosed() {
		t.Fatal("closed channel should look closed")
	}
	if closed.IsAlive() {
		t.Fatal("closed client should not be alive")
	}
}

func TestCdpSocketOnEndpointCarriesGateToken(t *testing.T) {
	endpoint := "http://desktop:abc123@192.0.2.7:19020"
	got := cdpSocketOnEndpoint(endpoint, "ws://127.0.0.1:9222/devtools/browser/x")
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "ws" || parsed.Host != "192.0.2.7:19020" {
		t.Fatalf("unexpected rewrite: %s", got)
	}
	if parsed.User == nil {
		t.Fatalf("gate token was dropped: %s", got)
	}
	password, _ := parsed.User.Password()
	if password != "abc123" {
		t.Fatalf("unexpected token %q", password)
	}
	if got := bearerFromEndpoint(endpoint); got != "abc123" {
		t.Fatalf("bearer extraction got %q", got)
	}
	if got := bearerFromEndpoint("http://192.0.2.7:19020"); got != "" {
		t.Fatalf("tokenless endpoint returned %q", got)
	}
}

func TestDiscoverTargetsSendsGateToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`[{"id":"p1","type":"page","webSocketDebuggerUrl":"ws://127.0.0.1/devtools/page/1"}]`))
	}))
	defer srv.Close()
	srvURL := strings.Replace(srv.URL, "http://", "http://desktop:tok123@", 1)
	targets, err := DiscoverTargets(srvURL)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok123" {
		t.Fatalf("gate token header missing: %q", gotAuth)
	}
	if len(targets) != 1 {
		t.Fatalf("unexpected targets: %+v", targets)
	}
}

func TestRedactUserInfoHidesGateToken(t *testing.T) {
	got := redactUserInfo("http://desktop:secret-token@192.0.2.7:19020")
	if strings.Contains(got, "secret-token") {
		t.Fatalf("token leaked into log output: %s", got)
	}
	if !strings.Contains(got, "192.0.2.7:19020") {
		t.Fatalf("host lost: %s", got)
	}
	if redactUserInfo("http://192.0.2.7:19020") != "http://192.0.2.7:19020" {
		t.Fatal("tokenless endpoint changed")
	}
}
