package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/oauth"
	"github.com/RapidAI/CodeClaw/corelib/proxyutil"
)

func TestApplyAgentProxyRoutesOAuthThroughMaclawScope(t *testing.T) {
	t.Cleanup(func() {
		oauth.SetProxy(proxyutil.Config{Enabled: false})
		setMaclawLLMProxy(proxyutil.Config{})
	})

	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	oidc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Via-Test-Proxy") != "1" {
			http.Error(w, "direct access blocked", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"authorization_endpoint":"http://auth.example/oauth2/authorize","token_endpoint":"http://auth.example/oauth2/token"}`))
	}))
	t.Cleanup(oidc.Close)

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	t.Cleanup(proxy.Close)

	u, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("proxy host/port: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	if _, err := app.PatchConfigFields(map[string]interface{}{
		"default_proxy_enabled":      true,
		"default_proxy_protocol":     "http",
		"default_proxy_host":         host,
		"default_proxy_port":         port,
		"default_proxy_scope_maclaw": true,
		"default_proxy_scope_agent":  false,
	}); err != nil {
		t.Fatalf("PatchConfigFields: %v", err)
	}

	if _, err := oauth.DiscoverOIDCEndpoints(context.Background(), oidc.URL); err != nil {
		t.Fatalf("OIDC discovery after applying MaClaw proxy: %v", err)
	}

	if _, err := app.PatchConfigFields(map[string]interface{}{
		"default_proxy_scope_maclaw": false,
	}); err != nil {
		t.Fatalf("disable MaClaw proxy scope: %v", err)
	}
	if _, err := oauth.DiscoverOIDCEndpoints(context.Background(), oidc.URL); err == nil {
		t.Fatal("OIDC discovery should fail when MaClaw proxy scope is off")
	}
}

func TestProxyConfigProbesThroughFormProxy(t *testing.T) {
	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Via-Test-Proxy") != "1" {
			http.Error(w, "direct access blocked", http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(probe.Close)

	trace := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("fl=1\nip=203.0.113.9\n"))
	}))
	t.Cleanup(trace.Close)

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	t.Cleanup(proxy.Close)

	u, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("proxy host/port: %v", err)
	}

	prevProbe, prevTrace := proxyProbeURL, proxyTraceURL
	proxyProbeURL, proxyTraceURL = probe.URL, trace.URL
	t.Cleanup(func() {
		proxyProbeURL, proxyTraceURL = prevProbe, prevTrace
	})

	app := &App{}
	result, err := app.TestProxyConfig(map[string]interface{}{
		"enabled":  true,
		"protocol": "http",
		"host":     host,
		"port":     port,
		"username": "alice",
		"password": "s3cret",
	})
	if err != nil {
		t.Fatalf("TestProxyConfig: %v", err)
	}
	if ok, _ := result["ok"].(bool); !ok {
		t.Fatalf("result = %#v, want ok", result)
	}
	if ip, _ := result["egress_ip"].(string); ip != "203.0.113.9" {
		t.Fatalf("egress_ip = %v, want 203.0.113.9", result["egress_ip"])
	}

	if _, err := app.TestProxyConfig(map[string]interface{}{"enabled": false, "host": host, "port": port}); err == nil {
		t.Fatal("disabled proxy should fail validation")
	}
	if _, err := app.TestProxyConfig(map[string]interface{}{"enabled": true, "host": "", "port": port}); err == nil {
		t.Fatal("missing host should fail validation")
	}
	if _, err := app.TestProxyConfig(map[string]interface{}{"enabled": true, "host": host, "port": "99999"}); err == nil {
		t.Fatal("out-of-range port should fail validation")
	}

	numeric, err := app.TestProxyConfig(map[string]interface{}{
		"enabled":  true,
		"protocol": "http",
		"host":     host,
		"port":     float64(mustAtoi(port)),
	})
	if err != nil {
		t.Fatalf("numeric port TestProxyConfig: %v", err)
	}
	if ok, _ := numeric["ok"].(bool); !ok {
		t.Fatalf("numeric port result = %#v, want ok", numeric)
	}
}

func mustAtoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		panic(err)
	}
	return n
}

func TestSaveProxyConfigNormalizesFormValues(t *testing.T) {
	t.Cleanup(func() { setMaclawLLMProxy(proxyutil.Config{}) })
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveProxyConfig(map[string]interface{}{
		"enabled":      true,
		"protocol":     "SOCKS5",
		"host":         " 127.0.0.1 ",
		"port":         float64(1080),
		"scope_maclaw": true,
	}); err != nil {
		t.Fatalf("SaveProxyConfig: %v", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.DefaultProxyEnabled || cfg.DefaultProxyProtocol != "socks5" || cfg.DefaultProxyHost != "127.0.0.1" || cfg.DefaultProxyPort != "1080" || !cfg.DefaultProxyScopeMaclaw {
		t.Fatalf("saved proxy = enabled=%v proto=%q host=%q port=%q scope=%v", cfg.DefaultProxyEnabled, cfg.DefaultProxyProtocol, cfg.DefaultProxyHost, cfg.DefaultProxyPort, cfg.DefaultProxyScopeMaclaw)
	}
	if err := app.SaveProxyConfig(map[string]interface{}{"port": "99999"}); err == nil {
		t.Fatal("invalid port should fail")
	}
}

func TestRedactProxySecrets(t *testing.T) {
	pc := proxyutil.Config{Username: "alice", Password: "p@ss word"}
	got := redactProxySecrets("proxy auth failed user=alice pass=p@ss word extra="+url.QueryEscape("p@ss word"), pc)
	if strings.Contains(got, "alice") || strings.Contains(got, "p@ss") {
		t.Fatalf("secrets leaked: %q", got)
	}

	hijack := annotateProxyProbeError("tls: failed to verify certificate: x509: certificate is valid for *.facebook.com, not www.google.com")
	if !strings.Contains(hijack, "劫持") {
		t.Fatalf("hijack hint missing: %q", hijack)
	}
}

func TestMaclawLLMProxyUsedByChatTransport(t *testing.T) {
	t.Cleanup(func() {
		oauth.SetProxy(proxyutil.Config{Enabled: false})
		setMaclawLLMProxy(proxyutil.Config{})
	})
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveProxyConfig(map[string]interface{}{
		"enabled":      true,
		"protocol":     "HTTP",
		"host":         "127.0.0.1",
		"port":         "10808",
		"scope_maclaw": true,
	}); err != nil {
		t.Fatalf("SaveProxyConfig: %v", err)
	}
	h := NewIMMessageHandler(app, nil)
	tr, ok := h.client.Transport.(*http.Transport)
	if !ok || tr.Proxy == nil {
		t.Fatalf("chat transport = %T", h.client.Transport)
	}
	req, err := http.NewRequest(http.MethodGet, "https://api.x.ai/v1/models", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	u, err := tr.Proxy(req)
	if err != nil || u == nil || u.Host != "127.0.0.1:10808" {
		t.Fatalf("chat proxy = %v (%v), want 127.0.0.1:10808", u, err)
	}

	if _, err := app.PatchConfigFields(map[string]interface{}{"default_proxy_scope_maclaw": false}); err != nil {
		t.Fatalf("disable MaClaw scope: %v", err)
	}
	u, err = tr.Proxy(req)
	if err != nil {
		t.Fatalf("chat proxy after disable: %v", err)
	}
	if u != nil && u.Host == "127.0.0.1:10808" {
		t.Fatal("MaClaw proxy still applied after scope off")
	}
}
