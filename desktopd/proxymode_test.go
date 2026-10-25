package desktopd

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func modeStoreWith(t *testing.T, dir string, ips map[string]string) *ProxyModeStore {
	t.Helper()
	return NewProxyModeStore(dir, func(context.Context) (map[string]string, error) {
		out := map[string]string{}
		for k, v := range ips {
			out[k] = v
		}
		return out, nil
	})
}

func TestNormalizeProxyMode(t *testing.T) {
	for in, want := range map[string]string{"": "", "proxy": "proxy", "ALWAYS": "proxy", " direct ": "direct", "smart": "smart"} {
		got, err := NormalizeProxyMode(in)
		if err != nil || got != want {
			t.Fatalf("NormalizeProxyMode(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := NormalizeProxyMode("vpn"); err == nil {
		t.Fatal("unknown mode accepted")
	}
}

func TestRouteDirectModeMatrix(t *testing.T) {
	ips := map[string]string{
		"172.17.0.2": containerName("t1", "alice"),
		"172.17.0.3": containerName("t1", "bob"),
		"172.17.0.4": containerName("t1", "carol"),
	}
	store := modeStoreWith(t, "", ips)
	ctx := context.Background()
	if _, err := store.Set(ctx, "t1", "alice", "proxy"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Set(ctx, "t1", "bob", "direct"); err != nil {
		t.Fatal(err)
	}
	// carol keeps the default (smart).
	p := NewForwardProxy(ProxyConfig{DirectSuffixes: []string{"xiaohongshu.com"}, Modes: store})
	cases := []struct {
		remote, target string
		want           bool
	}{
		{"172.17.0.2:5000", "www.jd.com:443", false},
		{"172.17.0.2:5000", "www.google.com:443", false},
		{"172.17.0.2:5000", "www.xiaohongshu.com:443", true},
		{"172.17.0.3:5000", "www.google.com:443", true},
		{"172.17.0.3:5000", "www.jd.com:443", true},
		{"172.17.0.4:5000", "www.jd.com:443", true},
		{"172.17.0.4:5000", "img10.360buyimg.com:443", true},
		{"172.17.0.4:5000", "www.taobao.com:443", true},
		{"172.17.0.4:5000", "www.xiaohongshu.com:443", true},
		{"172.17.0.4:5000", "www.google.com:443", false},
		{"172.17.0.4:5000", "notjd.com:443", false},
		{"172.17.0.4:5000", "example.cn:443", true},
		// Unknown source IP: host default (smart), never another user's mode.
		{"172.17.0.99:5000", "www.jd.com:443", true},
		{"172.17.0.99:5000", "www.google.com:443", false},
	}
	for _, c := range cases {
		if got := p.routeDirect(c.remote, c.target); got != c.want {
			t.Errorf("routeDirect(%s, %s) = %v, want %v", c.remote, c.target, got, c.want)
		}
	}
}

func TestRouteDirectWithoutModesKeepsHostList(t *testing.T) {
	p := NewForwardProxy(ProxyConfig{DirectSuffixes: []string{"xiaohongshu.com"}})
	if p.routeDirect("172.17.0.2:1", "www.jd.com:443") {
		t.Fatal("listener without modes routed jd.com directly")
	}
	if !p.routeDirect("172.17.0.2:1", "www.xiaohongshu.com:443") {
		t.Fatal("host direct list ignored")
	}
}

func TestProxyModePersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	store := modeStoreWith(t, dir, nil)
	if _, err := store.Set(context.Background(), "t1", "alice", "direct"); err != nil {
		t.Fatal(err)
	}
	again := modeStoreWith(t, dir, nil)
	if got := again.Mode("t1", "alice"); got != ProxyModeDirect {
		t.Fatalf("restored mode = %q", got)
	}
	if got := again.Mode("t1", "bob"); got != DefaultProxyMode {
		t.Fatalf("default mode = %q", got)
	}
	if _, err := again.Set(context.Background(), "t1", "alice", "smart"); err != nil {
		t.Fatal(err)
	}
	if len(modeStoreWith(t, dir, nil).Snapshot()) != 0 {
		t.Fatal("default mode should not be stored")
	}
}

func TestProxyModeSwitchDropsOnlyThatDesktopsTunnels(t *testing.T) {
	store := modeStoreWith(t, "", map[string]string{
		"172.17.0.2": containerName("t1", "alice"),
		"172.17.0.3": containerName("t1", "bob"),
	})
	p := NewForwardProxy(ProxyConfig{Modes: store})
	aliceClient, aliceRemote := net.Pipe()
	bobClient, bobRemote := net.Pipe()
	defer aliceRemote.Close()
	defer bobRemote.Close()
	p.mu.Lock()
	p.tunnel[aliceClient] = "172.17.0.2"
	p.tunnel[bobClient] = "172.17.0.3"
	p.mu.Unlock()
	changed, err := store.Set(context.Background(), "t1", "alice", "direct")
	if err != nil || !changed {
		t.Fatalf("Set = %v, %v", changed, err)
	}
	if !connClosed(aliceClient) {
		t.Fatal("alice's tunnel survived the switch")
	}
	if connClosed(bobClient) {
		t.Fatal("bob's tunnel was closed by alice's switch")
	}
	// Same mode again: nothing changes, nothing is dropped.
	if changed, _ := store.Set(context.Background(), "t1", "bob", "smart"); changed {
		t.Fatal("re-setting the default reported a change")
	}
	if connClosed(bobClient) {
		t.Fatal("unchanged mode dropped a tunnel")
	}
}

func connClosed(c net.Conn) bool {
	_ = c.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
	_, err := c.Write([]byte{0})
	return err != nil && !strings.Contains(err.Error(), "timeout")
}

func TestDirectModeStillRefusesPrivateTargets(t *testing.T) {
	store := modeStoreWith(t, "", map[string]string{"172.17.0.2": containerName("t1", "alice")})
	if _, err := store.Set(context.Background(), "t1", "alice", "direct"); err != nil {
		t.Fatal(err)
	}
	p := NewForwardProxy(ProxyConfig{Anonymous: true, Token: "k", Modes: store})
	for _, target := range []string{"127.0.0.1:22", "169.254.169.254:80", "10.0.0.5:443", "localhost:80"} {
		req := httptest.NewRequest(http.MethodConnect, "http://"+target, nil)
		req.Host = target
		req.RemoteAddr = "172.17.0.2:40000"
		rec := httptest.NewRecorder()
		p.serveConnect(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("CONNECT %s in direct mode = %d, want 403", target, rec.Code)
		}
	}
}

func TestParseContainerIPs(t *testing.T) {
	got := parseContainerIPs("/maclaw-desktop-abc 172.17.0.2\n/other 172.17.0.9\n/maclaw-desktop-def 172.18.0.3 10.1.0.4\n/maclaw-desktop-x <no value>\n")
	if got["172.17.0.2"] != "maclaw-desktop-abc" || got["172.18.0.3"] != "maclaw-desktop-def" || got["10.1.0.4"] != "maclaw-desktop-def" {
		t.Fatalf("parse = %v", got)
	}
	if _, ok := got["172.17.0.9"]; ok || len(got) != 3 {
		t.Fatalf("unexpected entries: %v", got)
	}
}

func TestProxyModeHTTPEndpoint(t *testing.T) {
	svc := &Service{Run: func(context.Context, ...string) (string, error) { return "", nil }}
	svc.ProxyModes = NewProxyModeStore(t.TempDir(), nil)
	h := Handler(svc, "tok", "", nil, nil)
	post := func(body, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/desktops/proxy-mode", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	if rec := post(`{"tenant_id":"t1","user_id":"alice","mode":"direct"}`, "bad"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token = %d", rec.Code)
	}
	rec := post(`{"tenant_id":"t1","user_id":"alice","mode":"direct"}`, "tok")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"mode":"direct"`) {
		t.Fatalf("set = %d %s", rec.Code, rec.Body.String())
	}
	if svc.ProxyModes.Mode("t1", "alice") != ProxyModeDirect {
		t.Fatal("mode not stored")
	}
	if rec := post(`{"tenant_id":"t1","user_id":"alice","mode":"vpn"}`, "tok"); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid mode = %d", rec.Code)
	}
}
