package oauth

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/proxyutil"
)

func TestApplyProxyFromAppConfigHonorsMaclawScope(t *testing.T) {
	t.Cleanup(func() { SetProxy(proxyutil.Config{Enabled: false}) })

	ApplyProxyFromAppConfig(corelib.AppConfig{
		DefaultProxyEnabled:     true,
		DefaultProxyScopeMaclaw: false,
		DefaultProxyHost:        "127.0.0.1",
		DefaultProxyPort:        "7890",
	})
	if currentProxyConfig().Enabled {
		t.Fatal("OAuth proxy should stay disabled when MaClaw scope is off")
	}

	ApplyProxyFromAppConfig(corelib.AppConfig{
		DefaultProxyEnabled:     true,
		DefaultProxyScopeMaclaw: true,
		DefaultProxyProtocol:    "http",
		DefaultProxyHost:        "127.0.0.1",
		DefaultProxyPort:        "7890",
	})
	got := currentProxyConfig()
	if !got.Enabled || got.Host != "127.0.0.1" || got.Port != "7890" {
		t.Fatalf("OAuth proxy = %+v, want enabled 127.0.0.1:7890", got)
	}
}

func TestSetProxyRoutesOIDCDiscovery(t *testing.T) {
	t.Cleanup(func() { SetProxy(proxyutil.Config{Enabled: false}) })

	oidc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-Via-Test-Proxy") != "1" {
			http.Error(w, "direct access blocked", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"authorization_endpoint":"http://auth.example/oauth2/authorize","token_endpoint":"http://auth.example/oauth2/token"}`))
	}))
	defer oidc.Close()

	var proxied bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied = true
		out, err := http.NewRequest(r.Method, r.URL.String(), r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		out.Header = r.Header.Clone()
		out.Header.Set("X-Via-Test-Proxy", "1")
		resp, err := http.DefaultClient.Do(out)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for k, vs := range resp.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	defer proxy.Close()

	_, err := DiscoverOIDCEndpoints(context.Background(), oidc.URL)
	if err == nil {
		t.Fatal("direct OIDC discovery should fail without the proxy header")
	}

	host, port := hostPortFromURL(t, proxy.URL)
	SetProxy(proxyutil.Config{Enabled: true, Protocol: "http", Host: host, Port: port})

	discovery, err := DiscoverOIDCEndpoints(context.Background(), oidc.URL)
	if err != nil {
		t.Fatalf("DiscoverOIDCEndpoints via proxy: %v", err)
	}
	if !proxied {
		t.Fatal("OIDC discovery did not go through the configured proxy")
	}
	if discovery.AuthorizationEndpoint != "http://auth.example/oauth2/authorize" || discovery.TokenEndpoint != "http://auth.example/oauth2/token" {
		t.Fatalf("discovery = %+v", discovery)
	}
}

func TestAnnotateOAuthNetworkErrorCertHijack(t *testing.T) {
	hijack := x509.HostnameError{Host: "auth.x.ai", Certificate: &x509.Certificate{}}
	err := annotateOAuthNetworkError("oidc discovery request", hijack)
	if err == nil {
		t.Fatal("expected annotated error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "劫持") || !strings.Contains(msg, "MaClaw 使用代理") {
		t.Fatalf("hijack hint missing: %q", msg)
	}
	if !errors.Is(err, hijack) && !strings.Contains(msg, hijack.Error()) {
		t.Fatalf("original TLS error should remain visible: %q", msg)
	}

	wrapped := annotateOAuthNetworkError("oidc discovery request", fmt.Errorf(`Get "https://auth.x.ai/.well-known/openid-configuration": tls: failed to verify certificate: x509: certificate is valid for *.facebook.com, not auth.x.ai`))
	if !strings.Contains(wrapped.Error(), "劫持") {
		t.Fatalf("string-form hijack should be annotated: %q", wrapped)
	}

	plain := annotateOAuthNetworkError("oidc discovery request", errors.New("connection refused"))
	if strings.Contains(plain.Error(), "劫持") {
		t.Fatalf("plain network errors should not use hijack copy: %q", plain)
	}

	malformed := annotateOAuthNetworkError("oidc discovery request", errors.New("x509: malformed certificate"))
	if strings.Contains(malformed.Error(), "劫持") || strings.Contains(malformed.Error(), "系统时间") {
		t.Fatalf("unrelated x509 errors should not use TLS-hijack copy: %q", malformed)
	}
}

func TestSetProxySkipsUnchangedConfig(t *testing.T) {
	t.Cleanup(func() { SetProxy(proxyutil.Config{Enabled: false}) })

	SetProxy(proxyutil.Config{Enabled: true, Protocol: "http", Host: "127.0.0.1", Port: "7890"})
	first := httpClient()
	SetProxy(proxyutil.Config{Enabled: true, Protocol: "http", Host: "127.0.0.1", Port: "7890"})
	if httpClient() != first {
		t.Fatal("identical proxy config should keep the same HTTP client")
	}

	SetProxy(proxyutil.Config{Enabled: false, Host: "127.0.0.1", Port: "7890"})
	disabled := httpClient()
	if disabled == first {
		t.Fatal("disabling the proxy should replace the HTTP client")
	}
	SetProxy(proxyutil.Config{Enabled: false})
	if httpClient() != disabled {
		t.Fatal("disabled proxy configs should normalize to the same no-op client")
	}

	SetProxy(proxyutil.Config{Enabled: true, Protocol: "SOCKS5", Host: "127.0.0.1", Port: "1080"})
	socks := httpClient()
	SetProxy(proxyutil.Config{Enabled: true, Protocol: "socks5h", Host: " 127.0.0.1 ", Port: "1080"})
	if httpClient() != socks {
		t.Fatal("equivalent SOCKS5 configs should keep the same HTTP client")
	}
}

func TestDoNoFollowWithinHonorsCallerBudget(t *testing.T) {
	started := time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	short, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DoNoFollowWithin(short, 40*time.Millisecond, time.Second); err == nil || !strings.Contains(err.Error(), "连接服务失败") {
		t.Fatalf("short budget error = %v, want an annotated timeout", err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatalf("short budget waited %s, want a fast header timeout", time.Since(started))
	}

	long, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := DoNoFollowWithin(long, time.Second, 2*time.Second)
	if err != nil {
		t.Fatalf("budget that covers the delayed headers: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	shared := httpClient()
	if shared.Timeout != oauthHTTPTimeout {
		t.Fatalf("shared client timeout = %s, want %s", shared.Timeout, oauthHTTPTimeout)
	}
}

func TestDoNoFollowWithinUsesConfiguredProxy(t *testing.T) {
	t.Cleanup(func() { SetProxy(proxyutil.Config{Enabled: false}) })
	SetProxy(proxyutil.Config{Enabled: true, Protocol: "http", Host: "127.0.0.1", Port: "7890"})
	transport := newProbeTransport(currentProxyConfig(), time.Minute)
	if transport.Proxy == nil {
		t.Fatal("generation transport dropped the configured HTTP proxy")
	}
	if transport.ResponseHeaderTimeout != time.Minute {
		t.Fatalf("header timeout = %s", transport.ResponseHeaderTimeout)
	}
	if !transport.ForceAttemptHTTP2 {
		t.Fatal("probe transport should keep HTTP/2, matching live chat")
	}
	if transport.DialContext == nil {
		t.Fatal("probe transport has no dial timeout; a black-holed host would wait for the whole generation budget")
	}
}

func TestDoNoFollowWithinStopsAtRedirect(t *testing.T) {
	followed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/next" {
			followed = true
			http.Error(w, "followed", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/next", http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := DoNoFollowWithin(req, time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if followed {
		t.Fatal("probe client followed a redirect")
	}
	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want 307", resp.StatusCode)
	}
}

func TestDialWithTimeoutClosesLateConn(t *testing.T) {
	released := make(chan struct{})
	closed := make(chan struct{})
	dial := func(string, string) (net.Conn, error) {
		<-released
		client, server := net.Pipe()
		go func() {
			buf := make([]byte, 1)
			if _, err := server.Read(buf); err != nil {
				close(closed)
			}
			_ = server.Close()
		}()
		return client, nil
	}
	_, err := dialWithTimeout(dial, 30*time.Millisecond)("tcp", "10.0.0.1:1")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want a dial timeout", err)
	}
	close(released)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("connection that arrived after the dial deadline was left open")
	}
}

func TestProbeTransportKeepsSOCKSDial(t *testing.T) {
	t.Cleanup(func() { SetProxy(proxyutil.Config{Enabled: false}) })
	SetProxy(proxyutil.Config{Enabled: true, Protocol: "socks5", Host: "127.0.0.1", Port: "1080"})
	transport := newProbeTransport(currentProxyConfig(), time.Minute)
	if transport.Dial == nil {
		t.Fatal("SOCKS probe transport dropped Dial")
	}
	if transport.DialContext != nil {
		t.Fatal("DialContext would bypass the SOCKS proxy")
	}
}

func hostPortFromURL(t *testing.T, raw string) (string, string) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("host/port %q: %v", u.Host, err)
	}
	return host, port
}
