package ws

import (
	"bytes"
	"compress/flate"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestConnContextWriteLoopDoesNotHoldASingleFrame(t *testing.T) {
	ctx := &ConnContext{
		sendCh:     make(chan any, 1),
		closeSend:  make(chan struct{}),
		writerDone: make(chan struct{}),
	}
	go ctx.writeLoop()
	start := time.Now()
	ctx.sendCh <- map[string]any{"type": "remote_control.stream"}
	select {
	case <-ctx.writerDone:
	case <-time.After(40 * time.Millisecond):
		t.Fatal("a single live frame waited for a batch timer")
	}
	if time.Since(start) >= 40*time.Millisecond {
		t.Fatalf("single frame took %s", time.Since(start))
	}
}

func TestBroadcastFrameCompressionSkipsImagesAndFatFrames(t *testing.T) {
	text := map[string]any{"type": "session.preview_delta", "text": strings.Repeat("说明", 80)}
	framed, ok := marshalWSBroadcast(text).(*preparedWSMessage)
	if !ok || !framed.compress {
		t.Fatal("preview text should compress once for every watcher")
	}
	for _, messageType := range []string{"session.image", "session.image_input", "im.proactive_file"} {
		msg := map[string]any{"type": messageType, "payload": map[string]any{"data": strings.Repeat("A", 300)}}
		got, ok := marshalWSBroadcast(msg).(*preparedWSMessage)
		if !ok || got.compress {
			t.Fatalf("%s broadcast would be compressed", messageType)
		}
	}
	fat := map[string]any{"type": "session.preview_delta", "text": strings.Repeat("a", wsCompressMaxBytes)}
	if wsPayloadUsesCompression(fat, wsCompressMaxBytes+1) {
		t.Fatal("oversize preview would be compressed")
	}
}

func TestBroadcastPreparedFrameReachesEveryWatcher(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin:       func(*http.Request) bool { return true },
		EnableCompression: true,
	}
	accepted := make(chan *websocket.Conn, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		accepted <- conn
	}))
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	dial := func(compress bool) *websocket.Conn {
		t.Helper()
		dialer := *websocket.DefaultDialer
		dialer.EnableCompression = compress
		conn, _, err := dialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return conn
	}
	compressedClient := dial(true)
	rawClient := dial(false)
	var serverConns []*websocket.Conn
	for i := 0; i < 2; i++ {
		select {
		case conn := <-accepted:
			_ = conn.SetCompressionLevel(flate.BestSpeed)
			serverConns = append(serverConns, conn)
			t.Cleanup(func() { _ = conn.Close() })
		case <-time.After(time.Second):
			t.Fatal("websocket upgrade did not finish")
		}
	}

	body := strings.Repeat("部署说明。", 40)
	framed, ok := marshalWSBroadcast(map[string]any{
		"type": "session.preview_delta",
		"text": body,
	}).(*preparedWSMessage)
	if !ok || !framed.compress {
		t.Fatal("shared preview frame was not prepared for compression")
	}
	for _, conn := range serverConns {
		if err := framed.writeTo(conn); err != nil {
			t.Fatal(err)
		}
	}
	for _, client := range []*websocket.Conn{compressedClient, rawClient} {
		_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
		var got map[string]any
		if err := client.ReadJSON(&got); err != nil {
			t.Fatal(err)
		}
		if got["type"] != "session.preview_delta" || got["text"] != body {
			t.Fatalf("watcher frame = %#v", got)
		}
	}
}

func TestLiveFrameDeflateShrinksPastTheThreshold(t *testing.T) {
	ack := []byte("{\"type\":\"ack\",\"request_id\":\"abc\",\"payload\":{\"ok\":true}}\n")
	if len(ack) >= wsCompressMinBytes {
		t.Fatalf("ack is %d bytes, the threshold would compress a frame that does not shrink", len(ack))
	}
	if deflatedLen(ack) <= len(ack) {
		t.Fatal("ack shrank; the threshold should move down to include it")
	}

	body := strings.Repeat("The assistant explains the deployment. ", 24)
	frame := []byte(`{"type":"remote_control.stream","payload":{"parts":[{"kind":"text","text":"` + body + `"}]}}` + "\n")
	if len(frame) < wsCompressMinBytes {
		t.Fatalf("sample frame is %d bytes, want it above the threshold", len(frame))
	}
	got := deflatedLen(frame)
	if got >= len(frame) {
		t.Fatalf("live frame raw=%d deflate=%d", len(frame), got)
	}
}

func TestCollectWriteBatchStopsAtTheCap(t *testing.T) {
	const extra = 5
	ch := make(chan any, wsWriteBatchCap+extra)
	for i := 0; i < wsWriteBatchCap+extra; i++ {
		ch <- i
	}
	first := <-ch
	batch, closed := collectWriteBatch(first, ch, wsWriteBatchCap)
	if closed || len(batch) != wsWriteBatchCap {
		t.Fatalf("batch=%d closed=%v", len(batch), closed)
	}
	if batch[0] != 0 || batch[wsWriteBatchCap-1] != wsWriteBatchCap-1 {
		t.Fatalf("edges %#v .. %#v", batch[0], batch[wsWriteBatchCap-1])
	}
	next := <-ch
	rest, closed := collectWriteBatch(next, ch, wsWriteBatchCap)
	if closed || len(rest) != extra {
		t.Fatalf("rest=%d closed=%v", len(rest), closed)
	}
}

func TestCollectWriteBatchDoesNotWaitForAShortTail(t *testing.T) {
	ch := make(chan any, 1)
	start := time.Now()
	batch, closed := collectWriteBatch("only", ch, wsWriteBatchCap)
	if closed || len(batch) != 1 || batch[0] != "only" {
		t.Fatalf("batch=%#v closed=%v", batch, closed)
	}
	if time.Since(start) >= 40*time.Millisecond {
		t.Fatalf("empty follow-up waited %s", time.Since(start))
	}

	close(ch)
	batch, closed = collectWriteBatch("tail", ch, wsWriteBatchCap)
	if !closed || len(batch) != 1 || batch[0] != "tail" {
		t.Fatalf("closed batch=%#v closed=%v", batch, closed)
	}
}

func TestConnContextWriteLoopWritesABurstImmediately(t *testing.T) {
	ctx, client, cleanup := startWriteLoopPair(t)
	defer cleanup()

	const n = wsWriteBatchCap + 8
	for i := 0; i < n; i++ {
		ctx.sendCh <- map[string]int{"n": i}
	}
	start := time.Now()
	go ctx.writeLoop()
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	for i := 0; i < n; i++ {
		var got map[string]int
		if err := client.ReadJSON(&got); err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if got["n"] != i {
			t.Fatalf("frame %d = %d", i, got["n"])
		}
	}
	if elapsed := time.Since(start); elapsed >= 200*time.Millisecond {
		t.Fatalf("burst of %d took %s", n, elapsed)
	}
	ctx.closeWriter()
	select {
	case <-ctx.writerDone:
	case <-time.After(time.Second):
		t.Fatal("writer did not exit")
	}
}

func TestConnContextWriteLoopFlushesQueuedTailOnClose(t *testing.T) {
	ctx, client, cleanup := startWriteLoopPair(t)
	defer cleanup()

	ctx.sendCh <- map[string]string{"type": "stream_end", "n": "1"}
	ctx.sendCh <- map[string]string{"type": "stream_end", "n": "2"}
	close(ctx.closeSend)
	go ctx.writeLoop()

	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	for _, want := range []string{"1", "2"} {
		var got map[string]string
		if err := client.ReadJSON(&got); err != nil {
			t.Fatal(err)
		}
		if got["type"] != "stream_end" || got["n"] != want {
			t.Fatalf("got %#v, want n=%s", got, want)
		}
	}
	select {
	case <-ctx.writerDone:
	case <-time.After(time.Second):
		t.Fatal("writer did not exit after close")
	}
}

func TestConnContextWriteLoopCloseFlushGivesUpOnAStalledPeer(t *testing.T) {
	ctx, client, cleanup := startWriteLoopPair(t)
	defer cleanup()
	// A peer that stops reading must not pin the writer on TCP retransmission.
	if tcp, ok := ctx.Conn.NetConn().(*net.TCPConn); ok {
		_ = tcp.SetWriteBuffer(1024)
	}
	if tcp, ok := client.NetConn().(*net.TCPConn); ok {
		_ = tcp.SetReadBuffer(1024)
	}
	payload := strings.Repeat("x", 256<<10)
	for i := 0; i < 8; i++ {
		ctx.sendCh <- map[string]string{"type": "blob", "n": strconv.Itoa(i), "data": payload}
	}
	close(ctx.closeSend)
	start := time.Now()
	go ctx.writeLoop()
	select {
	case <-ctx.writerDone:
	case <-time.After(wsCloseFlushBudget + 5*time.Second):
		t.Fatal("close flush blocked on a peer that stopped reading")
	}
	if elapsed := time.Since(start); elapsed > wsCloseFlushBudget+2*time.Second {
		t.Fatalf("close flush took %s, budget %s", elapsed, wsCloseFlushBudget)
	}
}

func startWriteLoopPair(t *testing.T) (*ConnContext, *websocket.Conn, func()) {
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
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	var serverConn *websocket.Conn
	select {
	case serverConn = <-accepted:
	case <-time.After(time.Second):
		_ = client.Close()
		server.Close()
		t.Fatal("websocket upgrade did not finish")
	}
	ctx := &ConnContext{
		Conn:       serverConn,
		sendCh:     make(chan any, sendChSize),
		closeSend:  make(chan struct{}),
		writerDone: make(chan struct{}),
	}
	return ctx, client, func() {
		_ = client.Close()
		_ = serverConn.Close()
		server.Close()
	}
}

func deflatedLen(raw []byte) int {
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.BestSpeed)
	if err != nil {
		return len(raw)
	}
	_, _ = w.Write(raw)
	_ = w.Close()
	return buf.Len()
}
