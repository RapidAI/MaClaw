package browser

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
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

func TestConnectCDPDialsWhenGateTokenIsInTheURL(t *testing.T) {
	type hit struct {
		auth string
		path string
	}
	hits := make(chan hit, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits <- hit{auth: r.Header.Get("Authorization"), path: r.URL.Path}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		_ = conn.Close()
	}))
	defer srv.Close()

	endpoint := "http://desktop:gate-token@" + strings.TrimPrefix(srv.URL, "http://")
	wsURL := cdpSocketOnEndpoint(endpoint, "ws://127.0.0.1:18020/devtools/page/login")
	dial, token := cdpDialTarget(wsURL)
	if token != "gate-token" {
		t.Fatalf("token=%q url=%s", token, wsURL)
	}
	wantDial := "ws" + strings.TrimPrefix(srv.URL, "http") + "/devtools/page/login"
	if dial != wantDial {
		t.Fatalf("dial=%q want %q", dial, wantDial)
	}

	client, err := ConnectCDP(wsURL)
	if err != nil {
		t.Fatal(err)
	}
	client.Close()
	select {
	case got := <-hits:
		if got.auth != "Bearer gate-token" {
			t.Fatalf("auth=%q", got.auth)
		}
		if got.path != "/devtools/page/login" {
			t.Fatalf("path=%q", got.path)
		}
	default:
		t.Fatal("websocket handler did not run")
	}
}

func TestCdpDialTargetPreservesIPv6DebuggerURL(t *testing.T) {
	endpoint := "https://desktop:abc123@[2001:db8::1]:49264"
	rewritten := cdpSocketOnEndpoint(endpoint, "ws://127.0.0.1:18020/devtools/browser/uuid")
	parsed, err := url.Parse(rewritten)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "wss" || parsed.Host != "[2001:db8::1]:49264" {
		t.Fatalf("rewrite: %s", rewritten)
	}
	if pw, _ := parsed.User.Password(); pw != "abc123" {
		t.Fatalf("token dropped: %s", rewritten)
	}
	dial, token := cdpDialTarget(rewritten)
	if token != "abc123" {
		t.Fatalf("token=%q", token)
	}
	if dial != "wss://[2001:db8::1]:49264/devtools/browser/uuid" {
		t.Fatalf("dial=%q", dial)
	}
	bareDial, bareToken := cdpDialTarget("ws://desktop@[::1]:9/devtools/page/1")
	if bareToken != "" || bareDial != "ws://[::1]:9/devtools/page/1" {
		t.Fatalf("username-only dial=%q token=%q", bareDial, bareToken)
	}
}

func TestBrowserWebSocketURLSendsGateToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if gotAuth == "" || strings.HasPrefix(gotAuth, "Basic ") {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/json/version" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"webSocketDebuggerUrl":"ws://127.0.0.1:18020/devtools/browser/uuid"}`))
	}))
	defer srv.Close()

	endpoint := strings.Replace(srv.URL, "http://", "http://desktop:tok123@", 1)
	got, err := browserWebSocketURL(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok123" {
		t.Fatalf("auth=%q", gotAuth)
	}
	dial, token := cdpDialTarget(got)
	if token != "tok123" {
		t.Fatalf("token=%q url=%s", token, got)
	}
	want := "ws" + strings.TrimPrefix(srv.URL, "http") + "/devtools/browser/uuid"
	if dial != want {
		t.Fatalf("dial=%q want %q", dial, want)
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
	dial, token := cdpDialTarget(targets[0].WebSocketDebugURL)
	if token != "tok123" {
		t.Fatalf("rewritten socket dropped token: %s", targets[0].WebSocketDebugURL)
	}
	want := "ws" + strings.TrimPrefix(srv.URL, "http") + "/devtools/page/1"
	if dial != want {
		t.Fatalf("dial=%q want %q", dial, want)
	}
}

func TestCDPChromeHost(t *testing.T) {
	cases := []struct{ in, want string }{
		{"http://desktop:tok@dockerd.example:19020", "127.0.0.1:19020"},
		{"ws://desktop:tok@dockerd.example:19020/devtools/page/1", "127.0.0.1:19020"},
		{"http://desktop:tok@192.0.2.7:19020", ""},
		{"http://127.0.0.1:9222", ""},
		{"http://localhost:9222", ""},
		{"https://desktop:tok@[2001:db8::1]:49264", ""},
		{"http://desktop:tok@dockerd.example", "127.0.0.1:80"},
	}
	for _, tc := range cases {
		if got := cdpChromeHost(tc.in); got != tc.want {
			t.Fatalf("%s: got %q want %q", tc.in, got, tc.want)
		}
	}
}

// chromeHostGate is the rejection Chromium's DevTools server sends when Host
// is a DNS name. The listener stands in for that server.
func chromeHostGate(t *testing.T, ok http.HandlerFunc) (int, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if name, _, splitErr := net.SplitHostPort(host); splitErr == nil {
			host = name
		}
		if net.ParseIP(host) == nil && !strings.EqualFold(host, "localhost") {
			http.Error(w, "Host header is specified and is not an IP address or localhost.", http.StatusInternalServerError)
			return
		}
		ok(w, r)
	})}
	go func() { _ = srv.Serve(ln) }()
	return port, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
}

func dialNamedDesktop(t *testing.T, port int) func() {
	t.Helper()
	prev := cdpDialContext
	cdpDialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if !strings.Contains(addr, "dockerd.example:") {
			t.Errorf("dialed %s", addr)
		}
		return (&net.Dialer{}).DialContext(ctx, network, "127.0.0.1:"+strconv.Itoa(port))
	}
	return func() { cdpDialContext = prev }
}

func TestDiscoverTargetsNamesTheHostLoopback(t *testing.T) {
	var gotHost, gotAuth string
	port, stop := chromeHostGate(t, func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`[{"id":"p","type":"page","url":"https://www.baidu.com/","webSocketDebuggerUrl":"ws://` + r.Host + `/devtools/page/p"}]`))
	})
	defer stop()
	defer dialNamedDesktop(t, port)()

	endpoint := "http://desktop:tok123@dockerd.example:" + strconv.Itoa(port)
	targets, err := DiscoverTargets(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok123" {
		t.Fatalf("auth=%q", gotAuth)
	}
	if gotHost != "127.0.0.1:"+strconv.Itoa(port) {
		t.Fatalf("host=%q", gotHost)
	}
	if len(targets) != 1 || targets[0].URL != "https://www.baidu.com/" {
		t.Fatalf("targets=%+v", targets)
	}
	parsed, err := url.Parse(targets[0].WebSocketDebugURL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Host != "dockerd.example:"+strconv.Itoa(port) || parsed.Path != "/devtools/page/p" {
		t.Fatalf("socket=%s", targets[0].WebSocketDebugURL)
	}
	if pw, _ := parsed.User.Password(); pw != "tok123" {
		t.Fatalf("token=%q", pw)
	}
}

func TestConnectCDPNamesTheHostLoopback(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	hits := make(chan string, 1)
	port, stop := chromeHostGate(t, func(w http.ResponseWriter, r *http.Request) {
		hits <- r.Host + " " + r.Header.Get("Authorization")
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		_ = conn.Close()
	})
	defer stop()
	defer dialNamedDesktop(t, port)()

	wsURL := "ws://desktop:gate-token@dockerd.example:" + strconv.Itoa(port) + "/devtools/page/login"
	client, err := ConnectCDP(wsURL)
	if err != nil {
		t.Fatal(err)
	}
	client.Close()
	select {
	case got := <-hits:
		want := "127.0.0.1:" + strconv.Itoa(port) + " Bearer gate-token"
		if got != want {
			t.Fatalf("handshake=%q want %q", got, want)
		}
	default:
		t.Fatal("websocket handler did not run")
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
