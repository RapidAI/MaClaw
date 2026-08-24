package browser

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

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
