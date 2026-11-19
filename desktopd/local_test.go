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

func TestLocalDataRejectsABadTicket(t *testing.T) {
	link := &HubLink{Key: "desk-key"}
	link.creds.NodeID = "node-1"
	handler := Handler(&Service{HubLink: link}, "api-token", t.TempDir(), nil, nil)

	bad := httptest.NewRequest(http.MethodPost, "/v1/desktops/local/not-a-ticket", strings.NewReader(`{"tenant_id":"tenant-a","user_id":"alice","action":"read","path":"/home/desktop/a.txt"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, bad)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("bad ticket status %d", rec.Code)
	}

	other, err := edgelink.SignLocal("desk-key", edgelink.LocalClaims{
		NodeID: "node-other", TenantID: "tenant-a", UserID: "alice", Action: "file",
		Exp: time.Now().Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	wrong := httptest.NewRequest(http.MethodPost, "/v1/desktops/local/"+other, strings.NewReader(`{"tenant_id":"tenant-a","user_id":"alice","action":"read","path":"/home/desktop/a.txt"}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, wrong)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("other node status %d", rec.Code)
	}

	ticket, err := edgelink.SignLocal("desk-key", edgelink.LocalClaims{
		NodeID: "node-1", TenantID: "tenant-a", UserID: "alice", Action: "file",
		Exp: time.Now().Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	mismatch := httptest.NewRequest(http.MethodPost, "/v1/desktops/local/"+ticket, strings.NewReader(`{"tenant_id":"tenant-a","user_id":"bob","action":"read","path":"/home/desktop/a.txt"}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, mismatch)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("mismatched user status %d", rec.Code)
	}
}

func TestLocalDataAcceptsTheHubKey(t *testing.T) {
	const primary = "primary-token-value-0123456789"
	const hubKey = "hub-key-0123456789abcdef"
	dir := t.TempDir()
	admin := newAdminServer(nil, primary, dir, nil, nil, nil)
	if err := admin.PutHubKey("hub-node-1", hubKey); err != nil {
		t.Fatal(err)
	}
	link := &HubLink{Key: primary}
	link.creds.NodeID = "node-1"
	// Handler installs the live admin for this state dir and replaces any
	// Keys pointer. The hub key has to be in that directory.
	handler := Handler(&Service{HubLink: link}, "api-token", dir, nil, nil)
	if got := link.signingKey(); got != hubKey {
		t.Fatalf("signing key = %q", got)
	}
	ticket, err := edgelink.SignLocal(hubKey, edgelink.LocalClaims{
		NodeID: "node-1", TenantID: "tenant-a", UserID: "alice", Action: "file",
		Exp: time.Now().Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/desktops/local/"+ticket, strings.NewReader(`{"tenant_id":"tenant-a","user_id":"alice","action":"read","path":"/home/desktop/a.txt"}`)))
	if rec.Code == http.StatusNotFound {
		t.Fatalf("the hub key was rejected: %s", rec.Body.String())
	}
	primaryTicket, err := edgelink.SignLocal(primary, edgelink.LocalClaims{
		NodeID: "node-1", TenantID: "tenant-a", UserID: "alice", Action: "file",
		Exp: time.Now().Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/desktops/local/"+primaryTicket, strings.NewReader(`{"tenant_id":"tenant-a","user_id":"alice","action":"read","path":"/home/desktop/a.txt"}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("primary token still opened local data: %d", rec.Code)
	}
	userKey, err := desktop.UserKey("tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	view, err := edgelink.SignViewer(hubKey, edgelink.ViewerClaims{
		NodeID: "node-1", TenantID: "tenant-a", UserID: "alice", UserKey: userKey,
		Display: "desktop", Exp: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/desktops/viewer/"+view+"/vnc.html", nil))
	if rec.Code == http.StatusNotFound {
		t.Fatalf("viewer hub key was rejected: %s", rec.Body.String())
	}
}

func TestLocalShotBodyKeepsScaleAndCursor(t *testing.T) {
	body := localShotBody(desktop.Shot{
		Data: []byte("png"), MIME: "image/png",
		ScreenWidth: 100, ScreenHeight: 80,
		ImageWidth: 50, ImageHeight: 40,
		Scale:  0.5,
		Region: desktop.ShotRegion{X: 1, Y: 2, Width: 10, Height: 8},
		CursorX: 3, CursorY: 4, HasCursor: true,
	}, "/home/desktop/Desktop/a.png")
	if body["screen_width"] != 100 || body["screen_height"] != 80 || body["image_width"] != 50 || body["scale"] != 0.5 {
		t.Fatalf("size fields missing: %v", body)
	}
	if body["saved_path"] != "/home/desktop/Desktop/a.png" {
		t.Fatalf("saved %v", body["saved_path"])
	}
	cursor, _ := body["cursor"].([]int)
	if len(cursor) != 2 || cursor[0] != 3 || cursor[1] != 4 {
		t.Fatalf("cursor %v", body["cursor"])
	}
}
