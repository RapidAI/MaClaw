package desktopd

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
)

// newProxyTokenTestServer runs the panel with a real file-backed proxy key
// source (fallback "env-key") and a logged-in admin client (cookie jar).
func newProxyTokenTestServer(t *testing.T) (*httptest.Server, *http.Client, *ProxyKeySource) {
	t.Helper()
	svc := &Service{Run: func(ctx context.Context, args ...string) (string, error) { return "", nil }, AdvertiseHost: "dockerd.example"}
	stateDir := t.TempDir()
	keys := NewProxyKeySource(stateDir, "env-key")
	srv := httptest.NewServer(Handler(svc, "primary-token", stateDir, keys, nil))
	t.Cleanup(srv.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	status, payload := adminJSON(t, srv, client, "POST", "/admin/api/setup", `{"username":"op","password":"longenough1"}`, nil)
	if status != 200 {
		t.Fatalf("setup: %d %v", status, payload)
	}
	return srv, client, keys
}

// newEgressTestServer runs the panel with a real file-backed egress source
// and a logged-in admin client (cookie jar).
func newEgressTestServer(t *testing.T) (*httptest.Server, *http.Client, *EgressSource) {
	t.Helper()
	svc := &Service{Run: func(ctx context.Context, args ...string) (string, error) { return "", nil }, AdvertiseHost: "dockerd.example"}
	stateDir := t.TempDir()
	egress := NewEgressSource(stateDir, EgressConfig{
		Upstream:        "https://docker:envkey@env.example:18082",
		DesktopProxyURL: "http://172.17.0.1:18083",
	})
	srv := httptest.NewServer(Handler(svc, "primary-token", stateDir, nil, egress))
	t.Cleanup(srv.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	status, payload := adminJSON(t, srv, client, "POST", "/admin/api/setup", `{"username":"op","password":"longenough1"}`, nil)
	if status != 200 {
		t.Fatalf("setup: %d %v", status, payload)
	}
	return srv, client, egress
}

func TestAdminEgressProxyLifecycle(t *testing.T) {
	srv, client, egress := newEgressTestServer(t)

	// Overview starts from the env fallback and decomposes host + key for
	// the panel's two inputs.
	status, payload := adminJSON(t, srv, client, "GET", "/admin/api/overview", "", nil)
	if status != 200 || payload["egress_source"] != "env" {
		t.Fatalf("initial overview: %d %v", status, payload)
	}
	if payload["egress_upstream_host"] != "https://env.example:18082" || payload["egress_upstream_key"] != "envkey" {
		t.Fatalf("decomposition: %v", payload)
	}

	// Invalid upstreams are 400 without touching the store.
	status, _ = adminJSON(t, srv, client, "POST", "/admin/api/egress-proxy", `{"upstream":"socks5://x:1"}`, nil)
	if status != 400 {
		t.Fatalf("bad scheme: %d", status)
	}
	status, _ = adminJSON(t, srv, client, "POST", "/admin/api/egress-proxy", `{"desktop_proxy_url":"http://8.8.8.8:1"}`, nil)
	if status != 400 {
		t.Fatalf("public desktop bind: %d", status)
	}

	// The simplified form: host + key, no URL assembly by hand. The key is
	// embedded URL-safely by url.UserPassword.
	status, payload = adminJSON(t, srv, client, "POST", "/admin/api/egress-proxy",
		`{"upstream_host":"dockerd.maclaw.top:18082","upstream_key":"new+key/with=chars","desktop_proxy_url":"http://172.17.0.1:18083"}`, nil)
	if status != 200 || payload["ok"] != true {
		t.Fatalf("save: %d %v", status, payload)
	}
	if cfg, fromPanel, err := egress.Get(); err != nil || !fromPanel {
		t.Fatalf("file config=%#v fromPanel=%v err=%v", cfg, fromPanel, err)
	} else if want := "https://docker:new+key%2Fwith=chars@dockerd.maclaw.top:18082"; cfg.Upstream != want {
		t.Fatalf("composed upstream=%q want %q", cfg.Upstream, want)
	}
	status, payload = adminJSON(t, srv, client, "GET", "/admin/api/overview", "", nil)
	if status != 200 || payload["egress_source"] != "panel" {
		t.Fatalf("overview after save: %d %v", status, payload)
	}
	if payload["egress_upstream_host"] != "https://dockerd.maclaw.top:18082" || payload["egress_upstream_key"] != "new+key/with=chars" {
		t.Fatalf("overview decomposition: %v", payload)
	}

	// DELETE restores the env fallback for the whole chain.
	status, _ = adminJSON(t, srv, client, "DELETE", "/admin/api/egress-proxy", "", nil)
	if status != 200 {
		t.Fatalf("delete: %d", status)
	}
	status, payload = adminJSON(t, srv, client, "GET", "/admin/api/overview", "", nil)
	if status != 200 || payload["egress_source"] != "env" || payload["egress_upstream_host"] != "https://env.example:18082" {
		t.Fatalf("overview after delete: %d %v", status, payload)
	}
}

func TestAdminEgressProxyTestEndpoint(t *testing.T) {
	srv, client, _ := newEgressTestServer(t)

	// Input errors are 400 with the key redacted; host missing first.
	status, payload := adminJSON(t, srv, client, "POST", "/admin/api/egress-proxy/test", `{"upstream_key":"sekret"}`, nil)
	if status != 400 {
		t.Fatalf("missing host: %d %v", status, payload)
	}
	status, payload = adminJSON(t, srv, client, "POST", "/admin/api/egress-proxy/test", `{"upstream_host":"ftp://x"}`, nil)
	if status != 400 {
		t.Fatalf("bad scheme host: %d %v", status, payload)
	}

	// The network call is stubbed: the endpoint reports ok/egress_ip as the
	// probe produced, no upstream keys leak in failure messages.
	original := testEgressUpstream
	t.Cleanup(func() { testEgressUpstream = original })
	testEgressUpstream = func(string) (int64, string, error) { return 87, "203.0.113.7", nil }
	status, payload = adminJSON(t, srv, client, "POST", "/admin/api/egress-proxy/test",
		`{"upstream_host":"dockerd.maclaw.top","upstream_key":"sekret"}`, nil)
	// adminJSON decodes JSON numbers as float64.
	if status != 200 || payload["ok"] != true || payload["egress_ip"] != "203.0.113.7" || payload["latency_ms"] != float64(87) {
		t.Fatalf("stubbed success: %d %v", status, payload)
	}
	testEgressUpstream = func(string) (int64, string, error) {
		return 0, "", fmt.Errorf("proxyconnect: dial upstream docker:sekret@host: i/o timeout")
	}
	status, payload = adminJSON(t, srv, client, "POST", "/admin/api/egress-proxy/test",
		`{"upstream_host":"dockerd.maclaw.top","upstream_key":"sekret"}`, nil)
	if status != 200 || payload["ok"] != false {
		t.Fatalf("stubbed failure: %d %v", status, payload)
	}
	if strings.Contains(payload["message"].(string), "sekret") {
		t.Fatalf("failure message leaks the key: %v", payload["message"])
	}
}

// composeMust composes and fails the test on error.
func composeMust(t *testing.T, host, key string) string {
	t.Helper()
	composed, err := composeUpstreamURL(host, key)
	if err != nil {
		t.Fatalf("compose %q+%q: %v", host, key, err)
	}
	return composed
}

func TestComposeUpstreamURLEdges(t *testing.T) {
	cases := []struct{ host, key, want string }{
		// Bare host gets the https scheme.
		{"dockerd.maclaw.top", "k", "https://docker:k@dockerd.maclaw.top"},
		// Existing scheme is kept verbatim.
		{"http://dockerd.maclaw.top:18082", "k", "http://docker:k@dockerd.maclaw.top:18082"},
		// Pasted full URL: the userinfo is replaced, path query fragment dropped.
		{"https://docker:old@dockerd.maclaw.top:18082", "k", "https://docker:k@dockerd.maclaw.top:18082"},
		{"dockerd.maclaw.top/v2?x=1#frag", "", "https://docker@dockerd.maclaw.top"},
		// Keyuserinfo escaping: "/" needs encoding, "+" and "=" do not.
		{"dockerd.maclaw.top", "a/b", "https://docker:a%2Fb@dockerd.maclaw.top"},
		{"dockerd.maclaw.top", "a+b=c", "https://docker:a+b=c@dockerd.maclaw.top"},
	}
	for _, tc := range cases {
		if got := composeMust(t, tc.host, tc.key); got != tc.want {
			t.Errorf("host=%q key=%q composed=%q want %q", tc.host, tc.key, got, tc.want)
		}
	}
	// Roundtrip: splitting recomposes to the same key.
	_, key, err := splitUpstreamCredentials(composeMust(t, "dockerd.maclaw.top", "a/b"))
	if err != nil || key != "a/b" {
		t.Fatalf("roundtrip key=%q err=%v", key, err)
	}
	// Errors: unsupported scheme, junk host.
	for _, bad := range []string{"ftp://x", "socks5://x:1080", "ho st"} {
		if _, err := composeUpstreamURL(bad, "k"); err == nil {
			t.Errorf("compose %q should fail", bad)
		}
	}
}

func TestForwardProxyUpstreamHotSwitch(t *testing.T) {
	upstreamAddr := startTestProxy(t, "sekret", true)
	backend := echoBackend(t)

	egress := NewEgressSource(t.TempDir(), EgressConfig{})
	chain := NewForwardProxy(ProxyConfig{
		Addr:      "127.0.0.1:0",
		Anonymous: true,
		Upstreams: egress,
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
	connect := func(key string) string {
		conn, err := net.Dial("tcp", chain.listener.Addr().String())
		if err != nil {
			t.Fatalf("dial chain: %v", err)
		}
		defer conn.Close()
		fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nAuthorization: Bearer %s\r\nHost: %s\r\n\r\n", backend, key, backend)
		return readProxyStatus(t, conn)
	}

	// No upstream configured: direct egress (the hook lets it reach loopback).
	if status := connect("any"); !strings.Contains(status, "200") {
		t.Fatalf("direct CONNECT expected 200, got %q", status)
	}

	// The panel points the chain at the upstream: the NEXT connection tunnels
	// through it, without a restart.
	if err := egress.Set(EgressConfig{Upstream: "http://docker:sekret@" + upstreamAddr}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if status := connect("anything"); !strings.Contains(status, "200") {
		t.Fatalf("chained CONNECT expected 200, got %q", status)
	}

	// A wrong upstream key surfaces as 502 on the next connection.
	if err := egress.Set(EgressConfig{Upstream: "http://docker:wrong@" + upstreamAddr}); err != nil {
		t.Fatalf("set wrong: %v", err)
	}
	if status := connect("anything"); !strings.Contains(status, "502") {
		t.Fatalf("chained CONNECT with wrong upstream key expected 502, got %q", status)
	}
}

func TestServiceDesktopProxyFollowsEgressSource(t *testing.T) {
	egress := NewEgressSource(t.TempDir(), EgressConfig{DesktopProxyURL: "http://172.17.0.1:18083"})
	svc := &Service{AdvertiseHost: "docker.example", DesktopProxyURL: "http://10.0.0.9:9999", Egress: egress}
	if got := svc.desktopProxy(); got != "http://10.0.0.9:9999" {
		t.Fatalf("no panel file: got %q", got)
	}
	// A panel file wins wholesale — an empty desktop URL there means direct.
	if err := egress.Set(EgressConfig{DesktopProxyURL: "http://172.17.0.2:18084"}); err != nil {
		t.Fatal(err)
	}
	if got := svc.desktopProxy(); got != "http://172.17.0.2:18084" {
		t.Fatalf("panel file: got %q", got)
	}
	if err := egress.Set(EgressConfig{}); err != nil {
		t.Fatal(err)
	}
	if got := svc.desktopProxy(); got != "" {
		t.Fatalf("panel direct: got %q", got)
	}
}

func TestAdminProxyTokenLifecycle(t *testing.T) {
	srv, client, keys := newProxyTokenTestServer(t)

	// Overview starts from the env fallback.
	status, payload := adminJSON(t, srv, client, "GET", "/admin/api/overview", "", nil)
	if status != 200 || payload["proxy_token"] != "env-key" || payload["proxy_token_source"] != "env" {
		t.Fatalf("initial overview: %d %v", status, payload)
	}

	// POST with an empty token generates one; the source flips to panel.
	status, payload = adminJSON(t, srv, client, "POST", "/admin/api/proxy-token", `{}`, nil)
	if status != 200 {
		t.Fatalf("auto-generate: %d %v", status, payload)
	}
	generated, ok := payload["token"].(string)
	if !ok || len(generated) != 48 {
		t.Fatalf("generated token %v is not hex 24", payload["token"])
	}
	status, payload = adminJSON(t, srv, client, "GET", "/admin/api/overview", "", nil)
	if status != 200 || payload["proxy_token"] != generated || payload["proxy_token_source"] != "panel" {
		t.Fatalf("overview after generate: %d %v", status, payload)
	}

	// A custom token replaces it; unsendable charsets are refused.
	status, payload = adminJSON(t, srv, client, "POST", "/admin/api/proxy-token", `{"token":"custom-key-1"}`, nil)
	if status != 200 || payload["token"] != "custom-key-1" {
		t.Fatalf("custom set: %d %v", status, payload)
	}
	status, _ = adminJSON(t, srv, client, "POST", "/admin/api/proxy-token", `{"token":"has space"}`, nil)
	if status != 400 {
		t.Fatalf("charset check: %d", status)
	}
	if _, _, err := keys.Get(); err != nil {
		t.Fatalf("source after failed set: %v", err)
	}

	// DELETE removes the panel file: back to the env fallback.
	status, _ = adminJSON(t, srv, client, "DELETE", "/admin/api/proxy-token", "", nil)
	if status != 200 {
		t.Fatalf("delete: %d", status)
	}
	status, payload = adminJSON(t, srv, client, "GET", "/admin/api/overview", "", nil)
	if status != 200 || payload["proxy_token"] != "env-key" || payload["proxy_token_source"] != "env" {
		t.Fatalf("overview after delete: %d %v", status, payload)
	}
}

func TestForwardProxyPanelKeyRotationWithoutRestart(t *testing.T) {
	_, _, keys := newProxyTokenTestServer(t)

	p := NewForwardProxy(ProxyConfig{Addr: "127.0.0.1:0", Keys: keys})
	p.dialHook = func(ctx context.Context, address string) (net.Conn, error) {
		return net.Dial("tcp", address)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Stop() })
	backend := echoBackend(t)

	connect := func(key string) string {
		conn, err := net.Dial("tcp", p.listener.Addr().String())
		if err != nil {
			t.Fatalf("dial proxy: %v", err)
		}
		defer conn.Close()
		fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nAuthorization: Bearer %s\r\nHost: %s\r\n\r\n", backend, key, backend)
		return readProxyStatus(t, conn)
	}

	if status := connect("env-key"); !strings.Contains(status, "200") {
		t.Fatalf("env key expected 200, got %q", status)
	}
	if status := connect("nope"); !strings.Contains(status, "407") {
		t.Fatalf("wrong key expected 407, got %q", status)
	}

	// The panel sets a new key: the OLD one dies and the new one works on
	// the very next connection — no restart involved.
	if err := keys.Set("panel-key-1"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if status := connect("env-key"); !strings.Contains(status, "407") {
		t.Fatalf("old key after rotation expected 407, got %q", status)
	}
	if status := connect("panel-key-1"); !strings.Contains(status, "200") {
		t.Fatalf("new key after rotation expected 200, got %q", status)
	}

	// Clearing restores the env fallback.
	if err := keys.Clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if status := connect("panel-key-1"); !strings.Contains(status, "407") {
		t.Fatalf("panel key after clear expected 407, got %q", status)
	}
	if status := connect("env-key"); !strings.Contains(status, "200") {
		t.Fatalf("env key after clear expected 200, got %q", status)
	}
}

// TestForwardProxyAcceptsBasicFromPanelKey ties the dockerd credential form
// (Basic in Proxy-Authorization) to the panel-rotated key.
func TestForwardProxyAcceptsBasicFromPanelKey(t *testing.T) {
	_, _, keys := newProxyTokenTestServer(t)
	p := NewForwardProxy(ProxyConfig{Addr: "127.0.0.1:0", Keys: keys})
	p.dialHook = func(ctx context.Context, address string) (net.Conn, error) {
		return net.Dial("tcp", address)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("start proxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Stop() })
	backend := echoBackend(t)

	if err := keys.Set("panel-key-1"); err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", p.listener.Addr().String())
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	basic := base64.StdEncoding.EncodeToString([]byte("docker:panel-key-1"))
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nProxy-Authorization: Basic %s\r\nHost: %s\r\n\r\n", backend, basic, backend)
	if status := readProxyStatus(t, conn); !strings.Contains(status, "200") {
		t.Fatalf("CONNECT with panel-key Basic expected 200, got %q", status)
	}
}
