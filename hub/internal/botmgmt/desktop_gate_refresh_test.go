package botmgmt

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
	"github.com/RapidAI/CodeClaw/hub/internal/upstream"
)

// rotatingGateDesktop is a desktopd stand-in. Each view returns the next
// noVNC address, so a container restart can change the gate bearer while
// the screen stays running.
type rotatingGateDesktop struct {
	desktopCounter
	mu        sync.Mutex
	urls      []string
	n         int
	views     int
	failAfter int
	fail      error
}

func (d *rotatingGateDesktop) DesktopViewState(context.Context, string, string) (string, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.views++
	if d.fail != nil && d.views > d.failAfter {
		return "", "", d.fail
	}
	if len(d.urls) == 0 {
		return DesktopStateAsleep, "", nil
	}
	url := d.urls[d.n]
	if d.n+1 < len(d.urls) {
		d.n++
	}
	return DesktopStateRunning, url, nil
}

func (d *rotatingGateDesktop) PeekDesktopView(ctx context.Context, tenantID, userID string) (string, error) {
	_, novnc, err := d.DesktopViewState(ctx, tenantID, userID)
	return novnc, err
}

func (d *rotatingGateDesktop) viewCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.views
}

func TestHoldDesktopViewRefreshesGateBearerOnIssuedHandoff(t *testing.T) {
	var seenAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "novnc")
	}))
	defer upstream.Close()
	oldRaw := strings.Replace(upstream.URL, "://", "://desktop:old-bearer@", 1) + "/v1/desktops/tunnel/af24af109248707f/vnc/vnc.html"
	newRaw := strings.Replace(upstream.URL, "://", "://desktop:new-bearer@", 1) + "/v1/desktops/tunnel/af24af109248707f/vnc/vnc.html"

	settings := &memSettings{}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), botViewRecordJSON(t)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	desk := &rotatingGateDesktop{
		desktopCounter: desktopCounter{url: oldRaw},
		urls:           []string{oldRaw, newRaw},
	}
	svc.Desktop = desk
	ctx := context.Background()
	if !svc.noteDesktopUserView("tenant-a", "alice", 0) {
		t.Fatal("note view")
	}

	first, _, err := svc.HoldDesktopView(ctx, "tenant-a", "alice", "bot_alice")
	if err != nil || first == "" {
		t.Fatalf("first hold url=%q err=%v", first, err)
	}
	if svc.DesktopViewRaw("tenant-a", "alice") != oldRaw {
		t.Fatalf("cached raw=%q", svc.DesktopViewRaw("tenant-a", "alice"))
	}
	token, rest, err := ParseHandoffPath(first)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	svc.ProxyDesktopHandoff(rec, httptest.NewRequest(http.MethodGet, first, nil), token, rest)
	if seenAuth != "Bearer old-bearer" {
		t.Fatalf("first auth=%q", seenAuth)
	}

	second, _, err := svc.HoldDesktopView(ctx, "tenant-a", "alice", "bot_alice")
	if err != nil {
		t.Fatal(err)
	}
	if second == "" || second == first {
		t.Fatalf("second hold did not publish the new page: %q", second)
	}
	if svc.DesktopViewRaw("tenant-a", "alice") != newRaw {
		t.Fatalf("same desktop user kept old raw=%q", svc.DesktopViewRaw("tenant-a", "alice"))
	}
	picture, _, state, err := svc.DesktopPicture(ctx, "tenant-a", "alice", "bot_alice", false)
	if err != nil || state != DesktopStateRunning || picture == "" || !strings.Contains(picture, "/vnc.html") {
		t.Fatalf("picture=%q state=%q err=%v", picture, state, err)
	}
	if picture == first {
		t.Fatal("DesktopPicture kept the stale handoff page")
	}

	rec = httptest.NewRecorder()
	svc.ProxyDesktopHandoff(rec, httptest.NewRequest(http.MethodGet, first, nil), token, rest)
	if rec.Code != http.StatusOK || seenAuth != "Bearer new-bearer" {
		t.Fatalf("issued token status=%d auth=%q", rec.Code, seenAuth)
	}
	newToken, newRest, err := ParseHandoffPath(second)
	if err != nil {
		t.Fatal(err)
	}
	svc.ProxyDesktopHandoff(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, second, nil), newToken, newRest)
	if seenAuth != "Bearer new-bearer" {
		t.Fatalf("new token auth=%q", seenAuth)
	}
}

func TestHoldDesktopViewDropsRejectedCacheOncePerRecoverGap(t *testing.T) {
	settings := &memSettings{}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), botViewRecordJSON(t)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	desk := &rotatingGateDesktop{
		desktopCounter: desktopCounter{url: "http://dockerd.example/vnc.html"},
		urls:           []string{"http://dockerd.example/old/vnc.html"},
		failAfter:      -1,
		fail:           upstream.NewStatusError(ErrSrv, http.StatusBadGateway, []byte(`{"error":"desktop tunnel unavailable"}`)),
	}
	svc.Desktop = desk
	svc.noteDesktopUserView("tenant-a", "alice", 0)
	svc.rememberDesktopView("tenant-a", "alice", "http://dockerd.example/old/vnc.html")
	// The first poll publishes the page. Later polls see the tunnel reject it.
	if svc.DesktopViewURL("tenant-a", "alice") == "" {
		t.Fatal("seed cache missing")
	}
	ctx := context.Background()
	url, _, err := svc.HoldDesktopView(ctx, "tenant-a", "alice", "bot_alice")
	if err != nil || url != "" {
		t.Fatalf("rejected hold url=%q err=%v", url, err)
	}
	if svc.DesktopViewURL("tenant-a", "alice") != "" {
		t.Fatal("rejected view kept the cached gate address")
	}
	firstViews := desk.viewCount()
	if _, _, err := svc.HoldDesktopView(ctx, "tenant-a", "alice", "bot_alice"); err != nil {
		t.Fatal(err)
	}
	if desk.viewCount() != firstViews {
		t.Fatalf("cooling did not skip the rejected read: %d -> %d", firstViews, desk.viewCount())
	}
	if !desktopbot.IsDesktopAsleep(desktopbot.AsleepError{}) && !desktopViewRejected(upstream.NewStatusError(ErrSrv, http.StatusBadGateway, nil)) {
		t.Fatal("rejection helpers no longer match the typed signals")
	}
}
