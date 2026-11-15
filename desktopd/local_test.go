package desktopd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
