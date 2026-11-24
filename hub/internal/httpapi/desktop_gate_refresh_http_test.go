package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
	"github.com/RapidAI/CodeClaw/hub/internal/upstream"
)

// gateRefreshDesktop stands in for desktopd over HTTP. Each look returns the
// next noVNC address, so a container restart can change the gate bearer
// while the screen stays running. A typed status error is a tunnel reject.
type gateRefreshDesktop struct {
	mu        sync.Mutex
	urls      []string
	n         int
	views     int
	opens     int
	failAfter int
	fail      error
}

func (d *gateRefreshDesktop) Open(context.Context, string, string) (string, error) {
	d.mu.Lock()
	d.opens++
	d.mu.Unlock()
	return "", nil
}

func (d *gateRefreshDesktop) Stop(context.Context, string, string) error {
	return nil
}

func (d *gateRefreshDesktop) DesktopViewState(context.Context, string, string) (string, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.views++
	if d.fail != nil && d.views > d.failAfter {
		return "", "", d.fail
	}
	if len(d.urls) == 0 {
		return botmgmt.DesktopStateAsleep, "", nil
	}
	url := d.urls[d.n]
	if d.n+1 < len(d.urls) {
		d.n++
	}
	return botmgmt.DesktopStateRunning, url, nil
}

func (d *gateRefreshDesktop) viewCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.views
}

func (d *gateRefreshDesktop) openCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.opens
}

func gateRefreshMux(svc *botmgmt.Service, identity botAuthenticator) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/bots/{id}/desktop", PostBotDesktopWatchHandler(svc, identity))
	mux.HandleFunc("GET /api/v1/desktop-handoff/{token}/{rest...}", GetDesktopHandoffHandler(svc))
	return mux
}

func gateRefreshAuth() fakeVEMachineAuth {
	return fakeVEMachineAuth{
		token:  "viewer-token",
		viewer: &auth.ViewerPrincipal{TenantID: "tenant-a", UserID: "alice"},
	}
}

func postDesktopWatch(t *testing.T, mux *http.ServeMux, botID string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/bots/"+botID+"/desktop", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("watch status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func watchNovnc(t *testing.T, body map[string]any) string {
	t.Helper()
	novnc, _ := body["novnc_url"].(string)
	return novnc
}

// A container restart keeps the desktop user and the issued handoff path,
// but replaces the gate bearer. The watch must publish the new page, and
// the token already in the browser must proxy with the new bearer.
func TestDesktopWatchHTTPRefreshesIssuedHandoffBearer(t *testing.T) {
	var seenAuth string
	gate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "novnc")
	}))
	defer gate.Close()
	oldRaw := strings.Replace(gate.URL, "://", "://desktop:old-bearer@", 1) + "/v1/desktops/tunnel/af24af109248707f/vnc/vnc.html"
	newRaw := strings.Replace(gate.URL, "://", "://desktop:new-bearer@", 1) + "/v1/desktops/tunnel/af24af109248707f/vnc/vnc.html"

	svc := grantedBotService(t, botViewerStub(t))
	bots, err := svc.BotsForUser(context.Background(), "tenant-a", "alice")
	if err != nil || len(bots) == 0 {
		t.Fatalf("bots %v %v", bots, err)
	}
	botID := bots[0].ID
	desktopUser := svc.DesktopUserForBot("tenant-a", "alice", botID)
	desk := &gateRefreshDesktop{urls: []string{oldRaw, newRaw}}
	svc.Desktop = desk
	svc.NoteDesktopView("tenant-a", desktopUser, oldRaw)
	svc.NoteDesktopAdminView("tenant-a", desktopUser)

	mux := gateRefreshMux(svc, gateRefreshAuth())
	issued := svc.DesktopViewURL("tenant-a", desktopUser)
	if issued == "" || !strings.HasPrefix(issued, "/api/v1/desktop-handoff/") {
		t.Fatalf("seed handoff=%q", issued)
	}
	first := watchNovnc(t, postDesktopWatch(t, mux, botID))
	if first == "" {
		t.Fatal("first watch published no page")
	}
	if desk.openCount() != 0 {
		t.Fatalf("watch started the desktop: opens=%d", desk.openCount())
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, issued, nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "novnc" || seenAuth != "Bearer old-bearer" {
		t.Fatalf("first proxy status=%d body=%q auth=%q", rec.Code, rec.Body.String(), seenAuth)
	}

	second := watchNovnc(t, postDesktopWatch(t, mux, botID))
	if second == "" {
		t.Fatal("second watch published no page")
	}
	if svc.DesktopViewRaw("tenant-a", desktopUser) != newRaw {
		t.Fatalf("cached raw=%q", svc.DesktopViewRaw("tenant-a", desktopUser))
	}
	refreshed := svc.DesktopViewURL("tenant-a", desktopUser)
	if refreshed == "" || refreshed == issued {
		t.Fatalf("same desktop user kept the old handoff page: %q", refreshed)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, issued, nil))
	if rec.Code != http.StatusOK || seenAuth != "Bearer new-bearer" {
		t.Fatalf("issued token status=%d auth=%q", rec.Code, seenAuth)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, refreshed, nil))
	if rec.Code != http.StatusOK || seenAuth != "Bearer new-bearer" {
		t.Fatalf("new token status=%d auth=%q", rec.Code, seenAuth)
	}
	if desk.openCount() != 0 {
		t.Fatalf("refresh started the desktop: opens=%d", desk.openCount())
	}
}

// A definite tunnel reject drops the cached gate address. The next poll
// stays inside the recover gap and does not ask desktopd again.
func TestDesktopWatchHTTPDropsRejectedGateOncePerRecoverGap(t *testing.T) {
	svc := grantedBotService(t, botViewerStub(t))
	bots, err := svc.BotsForUser(context.Background(), "tenant-a", "alice")
	if err != nil || len(bots) == 0 {
		t.Fatalf("bots %v %v", bots, err)
	}
	botID := bots[0].ID
	desktopUser := svc.DesktopUserForBot("tenant-a", "alice", botID)
	desk := &gateRefreshDesktop{
		failAfter: -1,
		fail:      upstream.NewStatusError(botmgmt.ErrSrv, http.StatusBadGateway, []byte(`{"error":"desktop tunnel unavailable"}`)),
	}
	svc.Desktop = desk
	svc.NoteDesktopView("tenant-a", desktopUser, "http://dockerd.example/old/vnc.html")
	svc.NoteDesktopAdminView("tenant-a", desktopUser)
	if svc.DesktopViewURL("tenant-a", desktopUser) == "" {
		t.Fatal("seed cache missing")
	}

	mux := gateRefreshMux(svc, gateRefreshAuth())
	first := postDesktopWatch(t, mux, botID)
	if watchNovnc(t, first) != "" {
		t.Fatalf("rejected watch kept url=%q body=%v", watchNovnc(t, first), first)
	}
	if svc.DesktopViewURL("tenant-a", desktopUser) != "" {
		t.Fatal("rejected view kept the cached gate address")
	}
	if desk.openCount() != 0 {
		t.Fatalf("rejected watch started the desktop: opens=%d", desk.openCount())
	}
	views := desk.viewCount()
	again := postDesktopWatch(t, mux, botID)
	if watchNovnc(t, again) != "" {
		t.Fatalf("cooling poll published %q", watchNovnc(t, again))
	}
	if desk.viewCount() != views {
		t.Fatalf("cooling did not skip the rejected read: %d -> %d", views, desk.viewCount())
	}
}
