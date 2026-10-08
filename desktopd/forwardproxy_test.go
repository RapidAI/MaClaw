package desktopd

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
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
