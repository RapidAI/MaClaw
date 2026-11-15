package desktopd

import (
	"net/http"
	"net/http/httptest"
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
