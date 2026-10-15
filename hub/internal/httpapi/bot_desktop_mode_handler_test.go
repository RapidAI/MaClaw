package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

func putDesktopMode(t *testing.T, h http.HandlerFunc, botID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/bots/"+botID+"/desktop-mode", strings.NewReader(body))
	req.SetPathValue("id", botID)
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// The owner switches their own bot without an admin token; another user
// cannot, and the list shows the mode either way.
func TestOwnerSwitchesOwnBotDesktopMode(t *testing.T) {
	svc := grantedBotService(t, botViewerStub(t))
	bots, err := svc.BotsForUser(context.Background(), "tenant-a", "alice")
	if err != nil || len(bots) == 0 {
		t.Fatalf("bots %v %v", bots, err)
	}
	botID := bots[0].ID
	if bots[0].DesktopMode != "shared" {
		t.Fatalf("default mode %q", bots[0].DesktopMode)
	}
	alice := fakeVEMachineAuth{token: "viewer-token", viewer: &auth.ViewerPrincipal{TenantID: "tenant-a", UserID: "alice"}}
	mallory := fakeVEMachineAuth{token: "viewer-token", viewer: &auth.ViewerPrincipal{TenantID: "tenant-a", UserID: "mallory"}}

	if rec := putDesktopMode(t, PutBotDesktopModeUserHandler(svc, mallory), botID, `{"desktop_mode":"isolated"}`); rec.Code == http.StatusOK {
		t.Fatalf("another user switched the bot: %s", rec.Body.String())
	}
	if rec := putDesktopMode(t, PutBotDesktopModeUserHandler(svc, alice), botID, `{"desktop_mode":"own"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad mode status=%d", rec.Code)
	}
	rec := putDesktopMode(t, PutBotDesktopModeUserHandler(svc, alice), botID, `{"desktop_mode":"isolated"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"desktop_mode":"isolated"`) {
		t.Fatalf("switch status=%d body=%s", rec.Code, rec.Body.String())
	}
	list := httptest.NewRequest(http.MethodGet, "/api/v1/bots", nil)
	list.Header.Set("Authorization", "Bearer viewer-token")
	out := httptest.NewRecorder()
	ListBotsHandler(svc, alice).ServeHTTP(out, list)
	if !strings.Contains(out.Body.String(), `"desktop_mode":"isolated"`) {
		t.Fatalf("list %s", out.Body.String())
	}
	rec = putDesktopMode(t, PutBotDesktopModeUserHandler(svc, alice), botID, `{"desktop_mode":"shared"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"desktop_mode":"shared"`) {
		t.Fatalf("switch back status=%d body=%s", rec.Code, rec.Body.String())
	}
}
