package botmgmt

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDesktopHandoffStaysOnHub(t *testing.T) {
	var seen string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.Path
		w.Header().Set("X-Frame-Options", "DENY")
		_, _ = w.Write([]byte("novnc"))
	}))
	defer upstream.Close()
	svc := NewService(&memSettings{})
	gated := svc.gateDesktopHandoff(upstream.URL + "/vnc.html?autoconnect=1")
	if strings.Contains(gated, "127.0.0.1") || strings.Contains(gated, upstream.URL) || !strings.Contains(gated, "/vnc.html") || !strings.Contains(gated, "reconnect=1") {
		t.Fatalf("gated=%q", gated)
	}
	token, rest, err := ParseHandoffPath(gated)
	if err != nil || rest != "vnc.html" {
		t.Fatalf("token=%q rest=%q err=%v", token, rest, err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, gated, nil)
	svc.ProxyDesktopHandoff(rec, req, token, rest)
	if rec.Code != http.StatusOK || rec.Body.String() != "novnc" || seen != "/vnc.html" {
		t.Fatalf("status=%d body=%q seen=%q", rec.Code, rec.Body.String(), seen)
	}
	if rec.Header().Get("X-Frame-Options") == "DENY" || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "frame-ancestors") {
		t.Fatalf("headers=%v", rec.Header())
	}
	denied := httptest.NewRecorder()
	svc.ProxyDesktopHandoff(denied, req, strings.Repeat("ab", 32), "vnc.html")
	if denied.Code != http.StatusNotFound {
		t.Fatalf("status=%d", denied.Code)
	}
}

func TestDesktopHandoffKeepsTheTunnelPrefix(t *testing.T) {
	var seenPath, seenAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	svc := NewService(&memSettings{})
	raw := strings.Replace(upstream.URL, "://", "://desktop:gate-token@", 1) + "/v1/desktops/tunnel/abcdef0123456789/vnc/vnc.html?autoconnect=1"
	gated := svc.gateDesktopHandoff(raw)
	if strings.Contains(gated, upstream.URL) || strings.Contains(gated, "abcdef0123456789") || !strings.Contains(gated, "/vnc.html") {
		t.Fatalf("gated=%q", gated)
	}
	token, rest, err := ParseHandoffPath(gated)
	if err != nil || rest != "vnc.html" {
		t.Fatalf("rest=%q err=%v", rest, err)
	}
	rec := httptest.NewRecorder()
	svc.ProxyDesktopHandoff(rec, httptest.NewRequest(http.MethodGet, gated, nil), token, rest)
	want := "/v1/desktops/tunnel/abcdef0123456789/vnc/vnc.html"
	if rec.Code != http.StatusNoContent || seenPath != want || seenAuth != "Bearer gate-token" {
		t.Fatalf("status=%d path=%q auth=%q", rec.Code, seenPath, seenAuth)
	}
	svc.ProxyDesktopHandoff(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/websockify", nil), token, "websockify")
	if seenPath != "/v1/desktops/tunnel/abcdef0123456789/vnc/websockify" {
		t.Fatalf("socket path=%q", seenPath)
	}
}

func TestDesktopHandoffTunnelSurvivesRestart(t *testing.T) {
	var seenPath, seenAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	settings := &memSettings{}
	rawRec, err := json.Marshal(record{
		BaseURL:     "http://maclawsrv.example",
		AccessToken: "secret-token",
		Bots: []Bot{
			{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"},
		},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(rawRec)); err != nil {
		t.Fatal(err)
	}
	svc1 := NewService(settings)
	raw := strings.Replace(upstream.URL, "://", "://desktop:gate-token@", 1) + "/v1/desktops/tunnel/abcdef0123456789/vnc/vnc.html?autoconnect=1"
	svc1.NoteDesktopView("tenant-a", "alice", raw)

	svc2 := NewService(settings)
	url, _, err := svc2.DesktopWatch(context.Background(), "tenant-a", "alice", "bot_alice")
	if err != nil || url == "" {
		t.Fatalf("restored watch url=%q err=%v", url, err)
	}
	token, rest, err := ParseHandoffPath(url)
	if err != nil || rest != "vnc.html" {
		t.Fatalf("rest=%q err=%v", rest, err)
	}
	rec := httptest.NewRecorder()
	svc2.ProxyDesktopHandoff(rec, httptest.NewRequest(http.MethodGet, url, nil), token, rest)
	want := "/v1/desktops/tunnel/abcdef0123456789/vnc/vnc.html"
	if rec.Code != http.StatusNoContent || seenPath != want || seenAuth != "Bearer gate-token" {
		t.Fatalf("status=%d path=%q auth=%q", rec.Code, seenPath, seenAuth)
	}
	svc2.ProxyDesktopHandoff(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/websockify", nil), token, "websockify")
	if seenPath != "/v1/desktops/tunnel/abcdef0123456789/vnc/websockify" {
		t.Fatalf("socket path=%q", seenPath)
	}
}

func TestDesktopHandoffCarriesThePointer(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/websockify" || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.NotFound(w, r)
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", http.StatusInternalServerError)
			return
		}
		conn, rw, err := hj.Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\nready\n")
		_ = rw.Flush()
	}))
	defer upstream.Close()
	svc := NewService(&memSettings{})
	gated := svc.gateDesktopHandoff(upstream.URL + "/vnc.html?autoconnect=1")
	token, _, err := ParseHandoffPath(gated)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		svc.ProxyDesktopHandoff(w, r, token, "websockify")
	}))
	defer proxy.Close()
	conn, err := net.Dial("tcp", proxy.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = conn.Write([]byte("GET /websockify HTTP/1.1\r\nHost: hub\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n"))
	status, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || !strings.Contains(status, "101") {
		t.Fatalf("status=%q err=%v", status, err)
	}
}
