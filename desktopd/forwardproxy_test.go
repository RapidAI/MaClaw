package desktopd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// startTestProxy runs a proxy on a random loopback port and returns the
// bound address. withDialHook installs a plain dialer: the real dial refuses
// loopback targets, which a test tunnel has to use, so tests that exercise
// the happy path bypass the guard.
func startTestProxy(t *testing.T, token string, withDialHook bool) string {
	t.Helper()
	p := NewForwardProxy(ProxyConfig{Addr: "127.0.0.1:0", Token: token})
	if withDialHook {
		p.dialHook = func(ctx context.Context, address string) (net.Conn, error) {
			dialer := &net.Dialer{Timeout: 5 * time.Second}
			return dialer.DialContext(ctx, "tcp", address)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); _ = p.Stop() })
	if err := p.Start(ctx); err != nil {
		t.Fatalf("start proxy: %v", err)
	}
	return p.listener.Addr().String()
}

// echoBackend answers /hello with the path so tests can tell the tunnel works.
// /auth echoes the Authorization header it received, so tests can verify that
// target-site credentials travel through the proxy untouched.
func echoBackend(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth" {
			_, _ = w.Write([]byte("auth:" + r.Header.Get("Authorization")))
			return
		}
		_, _ = w.Write([]byte("hello:" + r.URL.Path))
	}))
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().String()
}

// readProxyStatus reads the first response line of a raw proxy exchange.
func readProxyStatus(t *testing.T, conn net.Conn) string {
	t.Helper()
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("read proxy response: %v", err)
	}
	return strings.TrimSpace(line)
}

func TestForwardProxyConnectTunnel(t *testing.T) {
	proxyAddr := startTestProxy(t, "sekret", true)
	backend := echoBackend(t)

	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nAuthorization: Bearer sekret\r\nHost: %s\r\n\r\n", backend, backend)
	if status := readProxyStatus(t, conn); !strings.Contains(status, "200") {
		t.Fatalf("CONNECT expected 200, got %q", status)
	}
	fmt.Fprintf(conn, "GET /hello HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", backend)
	body, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read tunnel: %v", err)
	}
	if !strings.Contains(string(body), "hello:/hello") {
		t.Fatalf("tunnel body %q missing echo", string(body))
	}
}

func TestForwardProxyConnectBasicAuth(t *testing.T) {
	proxyAddr := startTestProxy(t, "sekret", true)
	backend := echoBackend(t)

	basic := base64.StdEncoding.EncodeToString([]byte("docker:sekret"))
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nProxy-Authorization: Basic %s\r\nHost: %s\r\n\r\n", backend, basic, backend)
	if status := readProxyStatus(t, conn); !strings.Contains(status, "200") {
		t.Fatalf("CONNECT with Basic auth expected 200, got %q", status)
	}
}

func TestForwardProxyRejectsBadAuth(t *testing.T) {
	proxyAddr := startTestProxy(t, "sekret", true)
	backend := echoBackend(t)

	// No credential at all.
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", backend, backend)
	if status := readProxyStatus(t, conn); !strings.Contains(status, "407") {
		t.Fatalf("CONNECT without auth expected 407, got %q", status)
	}

	// Right scheme, wrong byte.
	wrong := base64.StdEncoding.EncodeToString([]byte("docker:sekreb"))
	conn2, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn2.Close()
	fmt.Fprintf(conn2, "CONNECT %s HTTP/1.1\r\nProxy-Authorization: Basic %s\r\nHost: %s\r\n\r\n", backend, wrong, backend)
	if status := readProxyStatus(t, conn2); !strings.Contains(status, "407") {
		t.Fatalf("CONNECT with wrong Basic password expected 407, got %q", status)
	}
}

func TestForwardProxyConnectRefusesPrivateTarget(t *testing.T) {
	// No dial hook: the guard must refuse before any dial happens.
	proxyAddr := startTestProxy(t, "sekret", false)

	for _, target := range []string{"127.0.0.1:80", "10.1.2.3:443", "169.254.169.254:80", "192.168.1.4:22", "localhost:80"} {
		conn, err := net.Dial("tcp", proxyAddr)
		if err != nil {
			t.Fatalf("dial proxy: %v", err)
		}
		fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nAuthorization: Bearer sekret\r\nHost: %s\r\n\r\n", target, target)
		status := readProxyStatus(t, conn)
		conn.Close()
		if !strings.Contains(status, "403") {
			t.Fatalf("CONNECT %s expected 403, got %q", target, status)
		}
	}
}

func TestForwardProxyPlainHTTPForward(t *testing.T) {
	proxyAddr := startTestProxy(t, "sekret", true)
	backend := echoBackend(t)

	basic := base64.StdEncoding.EncodeToString([]byte("docker:sekret"))
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "GET http://%s/hello HTTP/1.1\r\nProxy-Authorization: Basic %s\r\nHost: %s\r\nConnection: close\r\n\r\n", backend, basic, backend)
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read proxied response: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read proxied body: %v", err)
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "hello:/hello") {
		t.Fatalf("proxied GET: status %d body %q", resp.StatusCode, string(body))
	}
}

func TestForwardProxyPlainHTTPRefusesHTTPSTarget(t *testing.T) {
	proxyAddr := startTestProxy(t, "sekret", true)

	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "GET https://example.com/x HTTP/1.1\r\nAuthorization: Bearer sekret\r\nHost: example.com\r\n\r\n")
	if status := readProxyStatus(t, conn); !strings.Contains(status, "400") {
		t.Fatalf("absolute-form HTTPS expected 400, got %q", status)
	}
	conn.Close()

	// Origin-form (normal server-style) requests are not a proxy exchange.
	conn2, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn2.Close()
	fmt.Fprintf(conn2, "GET /x HTTP/1.1\r\nAuthorization: Bearer sekret\r\nHost: example.com\r\n\r\n")
	if status := readProxyStatus(t, conn2); !strings.Contains(status, "400") {
		t.Fatalf("origin-form request expected 400, got %q", status)
	}
}

func TestForwardProxyChainsToUpstream(t *testing.T) {
	// The upstream is an authenticated desktopd proxy. Its dial hook lets it
	// reach the loopback echo backend (the public-only guard refuses that).
	upstreamAddr := startTestProxy(t, "sekret", true)
	backend := echoBackend(t)

	// The chained listener answers without a key and holds the upstream key
	// in its own configuration; containers never see credentials.
	chain := NewForwardProxy(ProxyConfig{
		Addr:      "127.0.0.1:0",
		Token:     "unused",
		Anonymous: true,
		Upstream:  "http://docker:sekret@" + upstreamAddr,
	})
	// The hook bypasses the target gate so the chain can reach the loopback
	// upstream in tests; dialUpstream itself is a plain dial.
	chain.dialHook = func(ctx context.Context, address string) (net.Conn, error) {
		return net.Dial("tcp", address)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := chain.Start(ctx); err != nil {
		t.Fatalf("start chain: %v", err)
	}
	chainAddr := chain.listener.Addr().String()
	t.Cleanup(func() { _ = chain.Stop() })

	conn, err := net.Dial("tcp", chainAddr)
	if err != nil {
		t.Fatalf("dial chain: %v", err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", backend, backend)
	if status := readProxyStatus(t, conn); !strings.Contains(status, "200") {
		t.Fatalf("chained CONNECT expected 200, got %q", status)
	}
	fmt.Fprintf(conn, "GET /hello HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", backend)
	body, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read chain tunnel: %v", err)
	}
	if !strings.Contains(string(body), "hello:/hello") {
		t.Fatalf("chained tunnel body %q missing echo", string(body))
	}
}

// rawEchoBackend echoes every byte it receives, so tests can check what the
// proxy does with client bytes beyond the request line.
func rawEchoBackend(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(conn)
		}
	}()
	return ln.Addr().String()
}

func TestForwardProxyConnectReplaysPipelinedBytes(t *testing.T) {
	proxyAddr := startTestProxy(t, "sekret", true)
	backend := rawEchoBackend(t)

	// CONNECT and the tunneled payload arrive in ONE write, before the 200:
	// the payload waits in the server's bufio reader and must be replayed
	// into the tunnel instead of being dropped.
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nAuthorization: Bearer sekret\r\nHost: %s\r\n\r\nPIPELINE", backend, backend)
	br := bufio.NewReader(conn)
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	status, err := br.ReadString('\n')
	if err != nil || !strings.Contains(status, "200") {
		t.Fatalf("CONNECT expected 200, got %q err=%v", status, err)
	}
	if _, err := br.ReadString('\n'); err != nil { // the blank header line
		t.Fatalf("read header end: %v", err)
	}
	echoed := make([]byte, len("PIPELINE"))
	if _, err := io.ReadFull(br, echoed); err != nil {
		t.Fatalf("read pipelined echo: %v", err)
	}
	if string(echoed) != "PIPELINE" {
		t.Fatalf("pipelined bytes were not replayed: %q", string(echoed))
	}
}

func TestForwardProxyPlainHTTPForwardsTargetSiteAuth(t *testing.T) {
	proxyAddr := startTestProxy(t, "sekret", true)
	backend := echoBackend(t)

	// Proxy-Authorization carries the proxy key; Authorization carries the
	// TARGET site's credential and must reach the target untouched.
	proxyBasic := base64.StdEncoding.EncodeToString([]byte("docker:sekret"))
	siteAuth := base64.StdEncoding.EncodeToString([]byte("siteuser:sitepass"))
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	fmt.Fprintf(conn,
		"GET http://%s/auth HTTP/1.1\r\nProxy-Authorization: Basic %s\r\nAuthorization: Basic %s\r\nHost: %s\r\nConnection: close\r\n\r\n",
		backend, proxyBasic, siteAuth, backend)
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read proxied response: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read proxied body: %v", err)
	}
	if resp.StatusCode != http.StatusOK || string(body) != "auth:Basic "+siteAuth {
		t.Fatalf("proxied GET: status %d body %q", resp.StatusCode, string(body))
	}
}

func TestForwardProxyStopClosesOpenTunnels(t *testing.T) {
	p := NewForwardProxy(ProxyConfig{Addr: "127.0.0.1:0", Token: "sekret"})
	p.dialHook = func(ctx context.Context, address string) (net.Conn, error) {
		return net.Dial("tcp", address)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("start proxy: %v", err)
	}
	backend := echoBackend(t)
	conn, err := net.Dial("tcp", p.listener.Addr().String())
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nAuthorization: Bearer sekret\r\nHost: %s\r\n\r\n", backend, backend)
	if status := readProxyStatus(t, conn); !strings.Contains(status, "200") {
		t.Fatalf("CONNECT expected 200, got %q", status)
	}
	// Stop tears the tunnel down: the client sees EOF rather than a
	// connection that keeps serving after the service is gone.
	if err := p.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Read(make([]byte, 16)); err == nil {
		t.Fatal("tunnel stayed open after Stop")
	}
}

func TestForwardProxyChainsToUpstreamRejectsBadKey(t *testing.T) {
	upstreamAddr := startTestProxy(t, "sekret", true)
	chain := NewForwardProxy(ProxyConfig{
		Addr:      "127.0.0.1:0",
		Anonymous: true,
		Upstream:  "http://docker:wrong@" + upstreamAddr,
	})
	chain.dialHook = func(ctx context.Context, address string) (net.Conn, error) {
		return net.Dial("tcp", address)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := chain.Start(ctx); err != nil {
		t.Fatalf("start chain: %v", err)
	}
	t.Cleanup(func() { _ = chain.Stop() })

	conn, err := net.Dial("tcp", chain.listener.Addr().String())
	if err != nil {
		t.Fatalf("dial chain: %v", err)
	}
	defer conn.Close()
	backend := echoBackend(t)
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", backend, backend)
	if status := readProxyStatus(t, conn); !strings.Contains(status, "502") {
		t.Fatalf("chained CONNECT with a wrong upstream key expected 502, got %q", status)
	}
}

func TestForwardProxyAnonymousListenerNeedsNoCredentials(t *testing.T) {
	p := NewForwardProxy(ProxyConfig{Addr: "127.0.0.1:0", Anonymous: true})
	p.dialHook = func(ctx context.Context, address string) (net.Conn, error) {
		return net.Dial("tcp", address)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("start anonymous proxy: %v", err)
	}
	proxyAddr := p.listener.Addr().String()
	t.Cleanup(func() { _ = p.Stop() })
	backend := echoBackend(t)

	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", backend, backend)
	if status := readProxyStatus(t, conn); !strings.Contains(status, "200") {
		t.Fatalf("anonymous CONNECT expected 200, got %q", status)
	}
}

func TestDesktopProxyBind(t *testing.T) {
	bind, canonical, err := DesktopProxyBind("http://172.17.0.1:18083")
	if err != nil || bind != "172.17.0.1:18083" || canonical != "http://172.17.0.1:18083" {
		t.Fatalf("bind=%q canonical=%q err=%v", bind, canonical, err)
	}
	// Path, query and userinfo never reach the containers: the canonical URL
	// is scheme + IP + port only.
	bind, canonical, err = DesktopProxyBind(" http://docker:sekret@10.0.0.5:8080/x?y=1 ")
	if err != nil || bind != "10.0.0.5:8080" || canonical != "http://10.0.0.5:8080" {
		t.Fatalf("trimming bind=%q canonical=%q err=%v", bind, canonical, err)
	}
	for _, bad := range []string{
		"http://proxy.example.com:18083", // hostname, not a bridge IP
		"http://127.0.0.1:18083",         // containers cannot reach host loopback
		"http://8.8.8.8:18083",           // public bind would make the relay open
		"http://172.17.0.1",              // no port
		"https://172.17.0.1:18083",       // containers speak plain http to it
		"172.17.0.1:18083",               // missing scheme
		"",
	} {
		if _, _, err := DesktopProxyBind(bad); err == nil {
			t.Fatalf("%q should be refused", bad)
		}
	}
}

func TestProxyTargetAllowed(t *testing.T) {
	allowed := []string{"example.com:443", "registry-1.docker.io:443", "example.com", "[2001:db8::1]:443",
		// Names are gated by resolution at dial time, not by the gate itself:
		// a name like metadata.tencentyun.com (LAN-only in Tencent) passes here
		// and is refused once its private address is resolved.
		"metadata.tencentyun.com:80"}
	blocked := []string{"localhost:80", "localhost", "127.0.0.1:443", "10.0.0.1:443",
		"192.168.1.10:80", "172.16.0.9:443", "169.254.169.254:80", "100.64.0.1:443",
		"redis.internal:6379", "nas.lan:445", "0.0.0.0:80"}
	for _, target := range allowed {
		if !proxyTargetAllowed(target) {
			t.Fatalf("%q should be allowed", target)
		}
	}
	for _, target := range blocked {
		if proxyTargetAllowed(target) {
			t.Fatalf("%q should be blocked", target)
		}
	}
}

func TestParseDirectSuffixes(t *testing.T) {
	got, err := ParseDirectSuffixes("")
	if err != nil || !containsString(got, "xiaohongshu.com") || !containsString(got, "xhscdn.com") {
		t.Fatalf("default suffixes = %v, %v", got, err)
	}
	got, err = ParseDirectSuffixes("off")
	if err != nil || len(got) != 0 {
		t.Fatalf("off = %v, %v", got, err)
	}
	got, err = ParseDirectSuffixes("Foo.COM, *.bar.com")
	if err != nil || strings.Join(got, ",") != "foo.com,bar.com" {
		t.Fatalf("explicit = %v, %v", got, err)
	}
	if _, err := ParseDirectSuffixes("bad host"); err == nil {
		t.Fatal("suffix with a space was accepted")
	}
}

func TestDirectTargetMatchesSubdomainOnly(t *testing.T) {
	p := NewForwardProxy(ProxyConfig{DirectSuffixes: []string{"xiaohongshu.com"}})
	if !p.directTarget("as.xiaohongshu.com:443") || !p.directTarget("xiaohongshu.com") {
		t.Fatal("xiaohongshu names should bypass the upstream")
	}
	if p.directTarget("evilxiaohongshu.com:443") || p.directTarget("googlevideo.com:443") {
		t.Fatal("unrelated names matched the direct list")
	}
}

func TestRetryableUpstream(t *testing.T) {
	if !retryableUpstream(&upstreamStatusError{host: "dockerd.maclaw.top", code: 502}) {
		t.Fatal("502 should be retried")
	}
	if retryableUpstream(&upstreamStatusError{host: "dockerd.maclaw.top", code: 407}) {
		t.Fatal("407 should not be retried")
	}
	if !retryableUpstream(errors.New("dial tcp 1.2.3.4:443: i/o timeout")) {
		t.Fatal("i/o timeout should be retried")
	}
	if classifyProxyError(&upstreamStatusError{code: 502}) != "http_502" {
		t.Fatal("502 was not classified")
	}
	if classifyProxyError(errors.New("dial tcp: i/o timeout")) != "timeout" {
		t.Fatal("timeout was not classified")
	}
}

func TestForwardProxyRetriesUpstream502(t *testing.T) {
	hits := &atomic.Int32{}
	upstream := scriptedUpstream(t, hits, 502, 502, 200)
	backend := echoBackend(t)
	chain := newChain(t, upstream, nil)
	conn := dialProxy(t, chain)
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", backend, backend)
	if status := readProxyStatus(t, conn); !strings.Contains(status, "200") {
		t.Fatalf("retried CONNECT expected 200, got %q", status)
	}
	if got := hits.Load(); got != 3 {
		t.Fatalf("upstream attempts = %d, want 3", got)
	}
	if SnapshotProxyMetrics().Retries < 2 {
		t.Fatalf("retries = %d", SnapshotProxyMetrics().Retries)
	}
}

func TestForwardProxyDoesNotRetryUpstream407(t *testing.T) {
	hits := &atomic.Int32{}
	upstream := scriptedUpstream(t, hits, 407)
	backend := echoBackend(t)
	chain := newChain(t, upstream, nil)
	conn := dialProxy(t, chain)
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", backend, backend)
	if status := readProxyStatus(t, conn); !strings.Contains(status, "502") {
		t.Fatalf("bad upstream status expected 502 to the client, got %q", status)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("407 was retried: attempts = %d", got)
	}
}

func TestForwardProxyDirectSuffixSkipsUpstream(t *testing.T) {
	hits := &atomic.Int32{}
	upstream := scriptedUpstream(t, hits, 502)
	chain := newChain(t, upstream, []string{"example.test"})
	conn := dialProxy(t, chain)
	fmt.Fprintf(conn, "CONNECT example.test:443 HTTP/1.1\r\nHost: example.test:443\r\n\r\n")
	if status := readProxyStatus(t, conn); !strings.Contains(status, "200") {
		t.Fatalf("direct CONNECT expected 200, got %q", status)
	}
	fmt.Fprintf(conn, "GET /hello HTTP/1.1\r\nHost: example.test\r\nConnection: close\r\n\r\n")
	body, err := io.ReadAll(conn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "hello:/hello") {
		t.Fatalf("direct tunnel body %q", body)
	}
	if hits.Load() != 0 {
		t.Fatalf("direct target still used the upstream (%d hits)", hits.Load())
	}

	conn = dialProxy(t, chain)
	fmt.Fprintf(conn, "CONNECT other.test:443 HTTP/1.1\r\nHost: other.test:443\r\n\r\n")
	if status := readProxyStatus(t, conn); !strings.Contains(status, "502") {
		t.Fatalf("non-direct target expected the upstream 502, got %q", status)
	}
	if hits.Load() == 0 {
		t.Fatal("non-direct target did not reach the upstream")
	}
}

func TestProxyFailureLogsAreAggregated(t *testing.T) {
	resetProxyMetrics()
	t.Cleanup(resetProxyMetrics)
	logs := captureLog(t)
	err := errors.New("upstream proxy dockerd.maclaw.top: dial tcp 1.2.3.4:443: i/o timeout")
	for i := 0; i < 20; i++ {
		noteProxyFailure("as.xiaohongshu.com:443", "172.17.0.2:1", err)
	}
	noteProxyFailure("googlevideo.com:443", "172.17.0.2:1", &upstreamStatusError{host: "dockerd.maclaw.top", code: 502})
	text := logs.String()
	if strings.Count(text, "as.xiaohongshu.com:443") != 1 {
		t.Fatalf("timeout log lines = %d\n%s", strings.Count(text, "as.xiaohongshu.com:443"), text)
	}
	if strings.Count(text, "googlevideo.com:443") != 1 {
		t.Fatalf("502 log lines = %d\n%s", strings.Count(text, "googlevideo.com:443"), text)
	}
	snap := SnapshotProxyMetrics()
	if snap.Timeout != 20 || snap.HTTP502 != 1 || snap.SuppressedLogs != 19 {
		t.Fatalf("metrics = %+v", snap)
	}
}

func TestProxyErrorSummaryAggregates(t *testing.T) {
	resetProxyMetrics()
	prevInterval := proxySummaryInterval
	proxySummaryInterval = 20 * time.Millisecond
	t.Cleanup(func() {
		resetProxyMetrics()
		proxySummaryInterval = prevInterval
	})
	logs := &lockedBuffer{}
	prev := log.Writer()
	log.SetOutput(logs)
	t.Cleanup(func() { log.SetOutput(prev) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	StartProxyErrorSummary(ctx)
	noteProxyFailure("a.example:443", "x", errors.New("dial tcp: i/o timeout"))
	noteProxyFailure("a.example:443", "x", errors.New("dial tcp: i/o timeout"))
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && !strings.Contains(logs.String(), "upstream errors +") {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(logs.String(), "timeout=2") && !strings.Contains(logs.String(), "timeout=1") {
		// The first line is the per-target log; the summary's delta is 2 if it
		// started from zero, or less if a tick landed between the two notes.
		if !strings.Contains(logs.String(), "upstream errors +") {
			t.Fatalf("summary missing:\n%s", logs.String())
		}
	}
}

func TestHealthIncludesProxyMetrics(t *testing.T) {
	resetProxyMetrics()
	t.Cleanup(resetProxyMetrics)
	noteProxyRetry()
	srv := httptest.NewServer(Handler(&Service{}, "tok", "", nil, nil))
	t.Cleanup(srv.Close)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/v1/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status %d", resp.StatusCode)
	}
	var body struct {
		OK    bool         `json:"ok"`
		Proxy ProxyMetrics `json:"proxy"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.OK || body.Proxy.Retries != 1 {
		t.Fatalf("health body = %+v", body)
	}
}

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func newChain(t *testing.T, upstream string, direct []string) *ForwardProxy {
	t.Helper()
	resetProxyMetrics()
	t.Cleanup(resetProxyMetrics)
	chain := NewForwardProxy(ProxyConfig{
		Addr:           "127.0.0.1:0",
		Anonymous:      true,
		Upstream:       "http://docker:sekret@" + upstream,
		DirectSuffixes: direct,
	})
	// The hook bypasses the public-target gate. Direct names are delivered to
	// the echo backend; the upstream path dials the scripted proxy itself.
	var directBackend string
	if len(direct) > 0 {
		// Created on the test goroutine: the dial hook runs on the proxy's
		// serve goroutine and must not touch t.
		directBackend = echoBackend(t)
	}
	chain.dialHook = func(ctx context.Context, address string) (net.Conn, error) {
		if directBackend != "" && strings.Contains(address, "example.test") {
			return net.Dial("tcp", directBackend)
		}
		return net.Dial("tcp", address)
	}
	chain.retryWait = func(context.Context, time.Duration) error { return nil }
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); _ = chain.Stop() })
	if err := chain.Start(ctx); err != nil {
		t.Fatalf("start chain: %v", err)
	}
	return chain
}

func dialProxy(t *testing.T, p *ForwardProxy) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", p.listener.Addr().String())
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	return conn
}

// scriptedUpstream answers CONNECT with the given status codes, one per
// connection, and proxies a 200 through to the requested target. Statuses past
// the end of the list are 200.
func scriptedUpstream(t *testing.T, hits *atomic.Int32, statuses ...int) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				line, err := br.ReadString('\n')
				if err != nil {
					return
				}
				for {
					h, err := br.ReadString('\n')
					if err != nil || h == "\r\n" || h == "\n" {
						break
					}
				}
				n := int(hits.Add(1)) - 1
				code := 200
				if n < len(statuses) {
					code = statuses[n]
				}
				if code != 200 {
					fmt.Fprintf(c, "HTTP/1.1 %d nope\r\nContent-Length: 0\r\n\r\n", code)
					return
				}
				fields := strings.Fields(line)
				if len(fields) < 2 {
					return
				}
				backend, err := net.Dial("tcp", fields[1])
				if err != nil {
					fmt.Fprintf(c, "HTTP/1.1 502 no backend\r\nContent-Length: 0\r\n\r\n")
					return
				}
				defer backend.Close()
				fmt.Fprintf(c, "HTTP/1.1 200 Connection Established\r\n\r\n")
				done := make(chan struct{}, 2)
				go func() { _, _ = io.Copy(backend, br); done <- struct{}{} }()
				go func() { _, _ = io.Copy(c, backend); done <- struct{}{} }()
				<-done
			}(conn)
		}
	}()
	return ln.Addr().String()
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}
