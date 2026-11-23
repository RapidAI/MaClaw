package desktopd

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/edgelink"
)

func TestViewerRejectsABadTicketAndLeavesPostView(t *testing.T) {
	link := &HubLink{Key: "desk-key"}
	link.creds.NodeID = "node-1"
	svc := &Service{HubLink: link}
	handler := Handler(svc, "api-token", t.TempDir(), nil, nil)

	post := httptest.NewRequest(http.MethodPost, "/v1/desktops/view", strings.NewReader(`{"tenant_id":"tenant-a","user_id":"alice"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, post)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("post view status %d", rec.Code)
	}

	bad := httptest.NewRequest(http.MethodGet, "/v1/desktops/viewer/not-a-ticket/vnc.html", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, bad)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("bad ticket status %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" || rec.Header().Get("Access-Control-Allow-Private-Network") != "true" {
		t.Fatalf("probe headers: %v", rec.Header())
	}
	preflight := httptest.NewRequest(http.MethodOptions, "/v1/desktops/viewer/not-a-ticket/vnc.html", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, preflight)
	if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("preflight %d %v", rec.Code, rec.Header())
	}

	key, err := desktop.UserKey("tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := edgelink.SignViewer("desk-key", edgelink.ViewerClaims{
		NodeID: "node-1", TenantID: "tenant-a", UserID: "alice", UserKey: key,
		Display: "desktop", Control: false, Exp: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	other, err := edgelink.SignViewer("desk-key", edgelink.ViewerClaims{
		NodeID: "node-other", TenantID: "tenant-a", UserID: "alice", UserKey: key,
		Display: "desktop", Exp: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	wrong := httptest.NewRequest(http.MethodGet, "/v1/desktops/viewer/"+other+"/vnc.html", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, wrong)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("other node status %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	rec.Header().Set("X-Frame-Options", "DENY")
	(&frameOK{ResponseWriter: rec}).WriteHeader(http.StatusOK)
	if rec.Header().Get("X-Frame-Options") != "" || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "frame-ancestors") {
		t.Fatalf("frame headers: %v", rec.Header())
	}

	page := httptest.NewRequest(http.MethodGet, "/v1/desktops/viewer/"+ticket+"/vnc.html", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, page)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("open screen status %d body %s", rec.Code, rec.Body.String())
	}

	socket := httptest.NewRequest(http.MethodGet, "/v1/desktops/viewer/"+ticket+"/websockify", nil)
	socket.Header.Set("Connection", "Upgrade")
	socket.Header.Set("Upgrade", "websocket")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, socket)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unconfirmed socket status %d", rec.Code)
	}
}

func TestViewerReadOnlyForcesH264ViewOnly(t *testing.T) {
	seenQuery := make(chan url.Values, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenQuery <- r.URL.Query()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	_, port, err := net.SplitHostPort(strings.TrimPrefix(upstream.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	link := &HubLink{Key: "desk-key"}
	link.creds.NodeID = "node-1"
	svc := &Service{HubLink: link, PublishBind: "127.0.0.1", Run: func(context.Context, ...string) (string, error) {
		return "127.0.0.1:" + port, nil
	}}
	key, err := desktop.UserKey("tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := edgelink.SignViewer("desk-key", edgelink.ViewerClaims{
		NodeID: "node-1", TenantID: "tenant-a", UserID: "alice", UserKey: key,
		Display: "desktop", Control: false, Exp: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	const gateToken = "0123456789abcdef0123456789abcdef"
	desktopGates.remember(gateToken, gateScreen{TenantID: "tenant-a", UserID: "alice", Display: ":20"})
	defer desktopGates.forget("tenant-a", "alice")
	req := httptest.NewRequest(http.MethodGet, "/v1/desktops/viewer/"+ticket+"/h264?input=1&view_only=0", nil)
	rec := httptest.NewRecorder()
	svc.serveViewer(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	query := <-seenQuery
	if query.Get("view_only") != "1" || query.Get("input") != "0" {
		t.Fatalf("read-only query was not enforced: %v", query)
	}
}

func TestViewerPageCarriesTheReconnectHook(t *testing.T) {
	rec := httptest.NewRecorder()
	page := &viewerHTMLWriter{ResponseWriter: &frameOK{ResponseWriter: rec}}
	page.Header().Set("Content-Type", "text/html; charset=utf-8")
	page.Header().Set("Content-Length", "9")
	page.Header().Set("X-Frame-Options", "DENY")
	page.WriteHeader(http.StatusOK)
	if _, err := page.Write([]byte("<head><title>desktop</title></head>")); err != nil {
		t.Fatal(err)
	}
	if rec.Body.Len() != 0 {
		t.Fatal("player html was sent before the hook")
	}
	page.finish()
	body := rec.Body.String()
	head := strings.Index(body, "<head>")
	hook := strings.Index(body, "maclaw-desktop-down")
	if head < 0 || hook < head || !strings.Contains(body, "<title>desktop</title>") {
		t.Fatalf("hook placement: %s", body)
	}
	if rec.Header().Get("Content-Length") != "" || rec.Header().Get("X-Frame-Options") != "" {
		t.Fatalf("headers: %v", rec.Header())
	}
	if strings.Count(body, "maclaw-desktop-down") != 1 {
		t.Fatalf("hook repeated: %s", body)
	}

	script := httptest.NewRecorder()
	js := &viewerHTMLWriter{ResponseWriter: script}
	js.Header().Set("Content-Type", "application/javascript")
	js.WriteHeader(http.StatusOK)
	if _, err := js.Write([]byte("var x=1")); err != nil {
		t.Fatal(err)
	}
	js.finish()
	if script.Body.String() != "var x=1" {
		t.Fatalf("script was rewritten: %s", script.Body.String())
	}
	if !viewerPageWantsFallback("vnc.html") || !viewerPageWantsFallback("h264.html") || viewerPageWantsFallback("websockify") {
		t.Fatal("fallback pages")
	}
}
