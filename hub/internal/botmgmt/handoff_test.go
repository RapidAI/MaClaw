package botmgmt

import (
	"bufio"
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
