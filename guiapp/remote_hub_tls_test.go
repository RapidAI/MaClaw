package guiapp

import (
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/gorilla/websocket"
)

func TestHubWebsocketDialerAsksForCompression(t *testing.T) {
	dialer, err := newHubWebsocketDialer("ws://127.0.0.1/ws")
	if err != nil {
		t.Fatal(err)
	}
	if !dialer.EnableCompression {
		t.Fatal("hub dialer did not offer permessage-deflate")
	}
}

func TestHubWebsocketWriteCompressesOnlyPastTheThreshold(t *testing.T) {
	ack, err := json.Marshal(map[string]any{"type": "ack", "request_id": "abc", "payload": map[string]any{"ok": true}})
	if err != nil {
		t.Fatal(err)
	}
	ack = append(ack, '\n')
	if len(ack) >= hubWSCompressMinBytes || hubWSFrameUsesCompression(nil, len(ack)) {
		t.Fatalf("ack is %d bytes and would be compressed", len(ack))
	}
	body := strings.Repeat("The assistant explains the deployment. ", 24)
	stream := map[string]any{"type": "remote_control.stream", "text": body}
	frame, err := json.Marshal(stream)
	if err != nil {
		t.Fatal(err)
	}
	frame = append(frame, '\n')
	if len(frame) < hubWSCompressMinBytes || !hubWSFrameUsesCompression(nil, len(frame)) {
		t.Fatalf("live frame is %d bytes and would stay raw", len(frame))
	}
	for _, messageType := range []string{"session.image", "im.proactive_file"} {
		env := HubEnvelope{Type: messageType, Payload: strings.Repeat("A", 300)}
		raw, err := json.Marshal(env)
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw, '\n')
		if len(raw) <= hubWSCompressMaxBytes && hubWSFrameUsesCompression(env, len(raw)) {
			t.Fatalf("%s frame (%d bytes) would be compressed", messageType, len(raw))
		}
	}
	oversize := hubWSCompressMaxBytes + 1
	if hubWSFrameUsesCompression(nil, oversize) {
		t.Fatalf("frame of %d bytes would be compressed", oversize)
	}
}

func TestHubWebsocketWriteRoundTripsSmallAndLargeFrames(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin:       func(*http.Request) bool { return true },
		EnableCompression: true,
	}
	accepted := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		accepted <- conn
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	dialer, err := newHubWebsocketDialer(wsURL)
	if err != nil {
		t.Fatal(err)
	}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	prepareHubWebsocketConn(conn)
	defer conn.Close()

	client := &RemoteHubClient{conn: conn}
	if err := client.writeHubJSON(map[string]any{"type": "heartbeat", "ok": true}); err != nil {
		t.Fatal(err)
	}
	large := strings.Repeat("部署说明。", 40)
	if err := client.writeHubJSON(map[string]any{"type": "remote_control.stream", "text": large}); err != nil {
		t.Fatal(err)
	}

	var serverConn *websocket.Conn
	select {
	case serverConn = <-accepted:
	case <-time.After(time.Second):
		t.Fatal("websocket upgrade did not finish")
	}
	defer serverConn.Close()
	_ = serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))

	var first map[string]any
	if err := serverConn.ReadJSON(&first); err != nil {
		t.Fatal(err)
	}
	if first["type"] != "heartbeat" || first["ok"] != true {
		t.Fatalf("small frame = %#v", first)
	}
	var second map[string]any
	if err := serverConn.ReadJSON(&second); err != nil {
		t.Fatal(err)
	}
	if second["type"] != "remote_control.stream" || second["text"] != large {
		t.Fatalf("large frame = %#v", second)
	}
}

func TestPreviewDeltaSendsTheFirstLineBeforeTheTicker(t *testing.T) {
	client, msgs, cleanup := previewDeltaTestPair(t)
	defer cleanup()

	start := time.Now()
	if err := client.SendPreviewDelta(SessionPreviewDelta{SessionID: "s", OutputSeq: 1, AppendLines: []string{"first"}}); err != nil {
		t.Fatal(err)
	}
	msg := recvPreviewDelta(t, msgs, previewFlushInterval/2)
	if time.Since(start) > previewFlushInterval/2 {
		t.Fatalf("first preview line waited %s", time.Since(start))
	}
	if lines := previewDeltaLines(t, msg); len(lines) != 1 || lines[0] != "first" {
		t.Fatalf("first lines = %#v", lines)
	}

	// The follow-up is its own frame. It leaves with the burst that was armed
	// on the lead, which is much shorter than the retry ticker. A slow lead
	// write can consume that first burst, so the budget is half the ticker
	// and not a few milliseconds.
	start = time.Now()
	if err := client.SendPreviewDelta(SessionPreviewDelta{SessionID: "s", OutputSeq: 2, AppendLines: []string{"second"}}); err != nil {
		t.Fatal(err)
	}
	msg = recvPreviewDelta(t, msgs, previewFlushInterval/2)
	if waited := time.Since(start); waited >= previewFlushInterval {
		t.Fatalf("follow-up waited for the retry ticker: %s", waited)
	}
	if lines := previewDeltaLines(t, msg); len(lines) != 1 || lines[0] != "second" {
		t.Fatalf("follow-up lines = %#v", lines)
	}

	client.closeIdlePreviewWindows()
	start = time.Now()
	if err := client.SendPreviewDelta(SessionPreviewDelta{SessionID: "s", OutputSeq: 3, AppendLines: []string{"third"}}); err != nil {
		t.Fatal(err)
	}
	msg = recvPreviewDelta(t, msgs, previewFlushInterval/2)
	if time.Since(start) > previewFlushInterval/2 {
		t.Fatalf("line after the window waited %s", time.Since(start))
	}
	if lines := previewDeltaLines(t, msg); len(lines) != 1 || lines[0] != "third" {
		t.Fatalf("third lines = %#v", lines)
	}
}

func TestPreviewDeltaKeepsLinesWhenTheWriteCannotFinish(t *testing.T) {
	client := &RemoteHubClient{
		previewPending: map[string]*pendingPreviewDelta{},
		previewWindow:  map[string]bool{},
	}
	t.Cleanup(func() {
		client.previewMu.Lock()
		client.stopPreviewBurstLocked()
		client.previewMu.Unlock()
	})
	if err := client.SendPreviewDelta(SessionPreviewDelta{SessionID: "s", OutputSeq: 1, AppendLines: []string{"old"}}); err != nil {
		t.Fatal(err)
	}
	// The lead armed a burst. Stop it before the rest of the lines arrive so
	// the callback cannot swap the buffer out from under the assertion, and
	// so a rejected socket is not written once per line.
	client.previewMu.Lock()
	client.stopPreviewBurstLocked()
	client.previewMu.Unlock()
	if err := client.SendPreviewDelta(SessionPreviewDelta{SessionID: "s", OutputSeq: 2, AppendLines: []string{"new"}}); err != nil {
		t.Fatal(err)
	}
	lines := previewPendingLines(t, client, "s")
	if len(lines) != 2 || lines[0] != "old" || lines[1] != "new" {
		t.Fatalf("unsent preview = %#v", lines)
	}

	for i := 0; i < previewPendingLineCap+1; i++ {
		if err := client.SendPreviewDelta(SessionPreviewDelta{
			SessionID:   "s",
			OutputSeq:   int64(i + 3),
			AppendLines: []string{fmt.Sprintf("line-%d", i)},
		}); err != nil {
			t.Fatal(err)
		}
	}
	client.previewMu.Lock()
	if client.previewBurst != nil {
		t.Fatal("a rejected preview armed a burst for every line")
	}
	pending := client.previewPending["s"]
	if pending == nil {
		client.previewMu.Unlock()
		t.Fatal("pending preview missing")
	}
	lines = append([]string(nil), pending.Lines...)
	client.previewMu.Unlock()
	if len(lines) != previewPendingLineCap {
		t.Fatalf("pending lines = %d, want %d", len(lines), previewPendingLineCap)
	}
	if lines[0] != "line-1" || lines[len(lines)-1] != fmt.Sprintf("line-%d", previewPendingLineCap) {
		t.Fatalf("kept tail = %q .. %q", lines[0], lines[len(lines)-1])
	}
}

func TestPreviewDeltaRestoresALineTheSocketRejected(t *testing.T) {
	client, msgs, cleanup := previewDeltaTestPair(t)
	defer cleanup()
	if err := client.SendPreviewDelta(SessionPreviewDelta{SessionID: "s", OutputSeq: 1, AppendLines: []string{"first"}}); err != nil {
		t.Fatal(err)
	}
	if lines := previewDeltaLines(t, recvPreviewDelta(t, msgs, time.Second)); len(lines) != 1 || lines[0] != "first" {
		t.Fatalf("first lines = %#v", lines)
	}
	// Close before the tail. The burst is only 16ms, so closing after the
	// send races the callback. The lead flush then fails on the dead socket
	// and puts the line back.
	client.mu.Lock()
	err := client.conn.Close()
	client.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SendPreviewDelta(SessionPreviewDelta{SessionID: "s", OutputSeq: 2, AppendLines: []string{"tail"}}); err != nil {
		t.Fatal(err)
	}
	lines := previewPendingLines(t, client, "s")
	if len(lines) != 1 || lines[0] != "tail" {
		t.Fatalf("rejected tail = %#v", lines)
	}
	select {
	case extra, ok := <-msgs:
		if ok {
			t.Fatalf("rejected tail was delivered: %#v", extra)
		}
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSessionCloseFlushesTheTailAheadOfTheCloseFrame(t *testing.T) {
	client, msgs, cleanup := previewDeltaTestPair(t)
	defer cleanup()

	if err := client.SendPreviewDelta(SessionPreviewDelta{SessionID: "s", OutputSeq: 1, AppendLines: []string{"first"}}); err != nil {
		t.Fatal(err)
	}
	if lines := previewDeltaLines(t, recvPreviewDelta(t, msgs, time.Second)); len(lines) != 1 || lines[0] != "first" {
		t.Fatalf("first = %#v", lines)
	}
	// Keep the burst window open and stop its timer so the tail stays buffered.
	// Close has to write that tail itself; the retry ticker is not running.
	client.previewMu.Lock()
	client.stopPreviewBurstLocked()
	client.previewWindow["s"] = true
	client.previewMu.Unlock()
	if err := client.SendPreviewDelta(SessionPreviewDelta{SessionID: "s", OutputSeq: 2, AppendLines: []string{"tail"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-msgs:
		t.Fatalf("tail left before close: %#v", msg["type"])
	default:
	}

	start := time.Now()
	if err := client.SendSessionClosed(&RemoteSession{ID: "s", Status: SessionExited}); err != nil {
		t.Fatal(err)
	}
	if waited := time.Since(start); waited >= previewFlushInterval {
		t.Fatalf("close waited for the retry ticker: %s", waited)
	}
	if lines := previewDeltaLines(t, recvPreviewDelta(t, msgs, time.Second)); len(lines) != 1 || lines[0] != "tail" {
		t.Fatalf("tail = %#v", lines)
	}
	select {
	case closed, ok := <-msgs:
		if !ok {
			t.Fatal("preview connection closed")
		}
		if closed["type"] != "session.closed" || closed["session_id"] != "s" {
			t.Fatalf("frame after tail = %#v", closed)
		}
	case <-time.After(time.Second):
		t.Fatal("session.closed did not follow the preview tail")
	}
}

func TestSessionCloseWaitsForTheInFlightPreview(t *testing.T) {
	client, msgs, cleanup := previewDeltaTestPair(t)
	defer cleanup()

	client.previewMu.Lock()
	client.ensurePreviewLocked()
	client.previewFlushing = true
	client.previewWindow["s"] = true
	client.previewPending["s"] = &pendingPreviewDelta{SessionID: "s", Lines: []string{"tail"}, OutputSeq: 2}
	client.previewMu.Unlock()

	done := make(chan error, 1)
	go func() {
		done <- client.SendSessionClosed(&RemoteSession{ID: "s", Status: SessionExited})
	}()
	deadline := time.Now().Add(time.Second)
	for {
		client.previewMu.Lock()
		closing := client.previewClosing["s"]
		client.previewMu.Unlock()
		if closing {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("close did not start draining the preview")
		}
		time.Sleep(time.Millisecond)
	}

	if err := client.SendPreviewDelta(SessionPreviewDelta{SessionID: "s", OutputSeq: 3, AppendLines: []string{"during"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-msgs:
		t.Fatalf("preview left while a flush was still in flight: %#v", msg["type"])
	default:
	}

	client.previewMu.Lock()
	client.previewFlushing = false
	client.previewFlushCond.Broadcast()
	client.previewMu.Unlock()

	if err := <-done; err != nil {
		t.Fatal(err)
	}
	lines := previewDeltaLines(t, recvPreviewDelta(t, msgs, time.Second))
	if len(lines) != 2 || lines[0] != "tail" || lines[1] != "during" {
		t.Fatalf("tail = %#v", lines)
	}
	select {
	case closed, ok := <-msgs:
		if !ok {
			t.Fatal("preview connection closed")
		}
		if closed["type"] != "session.closed" {
			t.Fatalf("frame after tail = %#v", closed["type"])
		}
	case <-time.After(time.Second):
		t.Fatal("session.closed did not follow the preview tail")
	}
}

func previewDeltaTestPair(t *testing.T) (*RemoteHubClient, <-chan map[string]any, func()) {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	accepted := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		accepted <- conn
	}))
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	prepareHubWebsocketConn(conn)
	var serverConn *websocket.Conn
	select {
	case serverConn = <-accepted:
	case <-time.After(time.Second):
		conn.Close()
		server.Close()
		t.Fatal("websocket upgrade did not finish")
	}
	msgs := make(chan map[string]any, 8)
	go func() {
		for {
			var msg map[string]any
			if err := serverConn.ReadJSON(&msg); err != nil {
				return
			}
			msgs <- msg
		}
	}()
	client := &RemoteHubClient{
		app:            &App{},
		conn:           conn,
		connected:      true,
		machineID:      "machine-1",
		previewPending: map[string]*pendingPreviewDelta{},
		previewWindow:  map[string]bool{},
		// lastSummary stays nil. Dedup must create the map and drop the lock
		// before returning: a panic while summaryMu is held is recovered by
		// the output loop and would otherwise block session.closed.
	}
	return client, msgs, func() {
		client.previewMu.Lock()
		client.stopPreviewBurstLocked()
		client.previewMu.Unlock()
		_ = conn.Close()
		_ = serverConn.Close()
		server.Close()
	}
}

func previewPendingLines(t *testing.T, client *RemoteHubClient, sessionID string) []string {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		client.previewMu.Lock()
		client.stopPreviewBurstLocked()
		flushing := client.previewFlushing
		pending := client.previewPending[sessionID]
		var lines []string
		if pending != nil {
			lines = append([]string(nil), pending.Lines...)
		}
		client.previewMu.Unlock()
		if !flushing || time.Now().After(deadline) {
			return lines
		}
		time.Sleep(time.Millisecond)
	}
}

func recvPreviewDelta(t *testing.T, msgs <-chan map[string]any, wait time.Duration) map[string]any {
	t.Helper()
	select {
	case msg, ok := <-msgs:
		if !ok {
			t.Fatal("preview connection closed")
		}
		if msg["type"] != "session.preview_delta" {
			t.Fatalf("frame type = %#v", msg["type"])
		}
		return msg
	case <-time.After(wait):
		t.Fatal("preview frame did not arrive")
	}
	return nil
}

func previewDeltaLines(t *testing.T, msg map[string]any) []string {
	t.Helper()
	payload, ok := msg["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload = %#v", msg["payload"])
	}
	raw, ok := payload["append_lines"].([]any)
	if !ok {
		t.Fatalf("append_lines = %#v", payload["append_lines"])
	}
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		text, ok := line.(string)
		if !ok {
			t.Fatalf("line = %#v", line)
		}
		lines = append(lines, text)
	}
	return lines
}

func TestHubWebsocketWriteRejectsAMissingConn(t *testing.T) {
	if err := (&RemoteHubClient{}).writeHubJSON(map[string]any{"type": "ack"}); err == nil {
		t.Fatal("nil conn wrote a frame")
	}
}

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
