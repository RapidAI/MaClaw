package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
	"github.com/RapidAI/CodeClaw/hub/internal/desktoppool"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// desktopSettingsMem is a per-key system settings store, so one pool and one
// botmgmt service can share the same record the way the running Hub does.
type desktopSettingsMem struct {
	values map[string]string
}

func newDesktopSettingsMem() *desktopSettingsMem {
	return &desktopSettingsMem{values: map[string]string{}}
}

func (m *desktopSettingsMem) Get(_ context.Context, key string) (string, error) {
	value, ok := m.values[key]
	if !ok {
		return "", context.Canceled
	}
	return value, nil
}

func (m *desktopSettingsMem) Set(_ context.Context, key, valueJSON string) error {
	m.values[key] = valueJSON
	return nil
}

// dockerDesktopFake is one Docker service that answers the desktop calls Hub
// makes. novnc is the noVNC address it reports, "" when it reports none. The
// returned counter counts the stop calls that reached it.
func dockerDesktopFake(t *testing.T, novnc string) (*httptest.Server, *int32) {
	t.Helper()
	var stops int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/desktops/session":
			writeJSON(w, http.StatusOK, map[string]string{
				"cdp_url":   "http://dockerd.example:19020",
				"display":   ":1",
				"novnc_url": novnc,
			})
		case "/v1/desktops/stop":
			atomic.AddInt32(&stops, 1)
			writeJSON(w, http.StatusOK, map[string]string{"container": "desk-1", "status": "stopped"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, &stops
}

// desktopPoolFake registers one Docker service for every user in tenant-a.
func desktopPoolFake(t *testing.T, system *desktopSettingsMem, baseURL string) *desktoppool.Pool {
	t.Helper()
	pool := desktoppool.New(system, nil)
	server, err := pool.CreateServer(context.Background(), "tenant-a", desktoppool.Server{Name: "srv", BaseURL: baseURL, AccessToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.CreateAssignment(context.Background(), "tenant-a", desktoppool.Assignment{Scope: desktoppool.ScopeGlobal, ServerID: server.ID}); err != nil {
		t.Fatal(err)
	}
	return pool
}

func adminRequest(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	return req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{Scope: "tenant", TenantID: "tenant-a"}))
}

func desktopViewRequest(body string) *http.Request {
	return adminRequest(http.MethodPost, "/api/admin/desktop-services/desktops/view", body)
}

func TestDesktopViewAdminHandlerReturnsGatedNovnc(t *testing.T) {
	docker, _ := dockerDesktopFake(t, "http://dockerd.example:6080/vnc.html?autoconnect=1")
	pool := desktopPoolFake(t, newDesktopSettingsMem(), docker.URL)
	bots := botmgmt.NewService(&botSettingsMem{})

	rec := httptest.NewRecorder()
	PostDesktopViewAdminHandler(pool, bots).ServeHTTP(rec, desktopViewRequest(`{"user_id":"alice"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Novnc string `json:"novnc_url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	// The Docker-side token must stay behind Hub, not reach the browser.
	if !strings.HasPrefix(out.Novnc, "/api/v1/desktop-handoff/") || strings.Contains(out.Novnc, "dockerd.example") {
		t.Fatalf("novnc_url=%q", out.Novnc)
	}
	if bots.DesktopViewURL("tenant-a", "alice") != out.Novnc {
		t.Fatalf("view not remembered: %q", bots.DesktopViewURL("tenant-a", "alice"))
	}
}

func TestDesktopViewAdminHandlerRejectsMissingNovnc(t *testing.T) {
	docker, _ := dockerDesktopFake(t, "")
	pool := desktopPoolFake(t, newDesktopSettingsMem(), docker.URL)

	rec := httptest.NewRecorder()
	PostDesktopViewAdminHandler(pool, botmgmt.NewService(&botSettingsMem{})).ServeHTTP(rec, desktopViewRequest(`{"user_id":"alice"}`))
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "DESKTOP_NOVNC_UNAVAILABLE") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDesktopStopAdminHandlerForgetsNovncView(t *testing.T) {
	docker, _ := dockerDesktopFake(t, "http://dockerd.example:6080/vnc.html?autoconnect=1")
	pool := desktopPoolFake(t, newDesktopSettingsMem(), docker.URL)
	bots := botmgmt.NewService(&botSettingsMem{})
	bots.NoteDesktopView("tenant-a", "alice", "http://dockerd.example:6080/vnc.html?autoconnect=1")
	if bots.DesktopViewURL("tenant-a", "alice") == "" {
		t.Fatal("view was not recorded")
	}

	rec := httptest.NewRecorder()
	PostDesktopStopAdminHandler(pool, bots).ServeHTTP(rec, adminRequest(http.MethodPost, "/api/admin/desktop-services/desktops/stop", `{"user_id":"alice"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if bots.DesktopViewURL("tenant-a", "alice") != "" {
		t.Fatalf("view survived the stop: %q", bots.DesktopViewURL("tenant-a", "alice"))
	}
}

// adminCheckedDesktop runs the admin VNC check for one user and returns the
// botmgmt service that now holds the desktop.
func adminCheckedDesktop(t *testing.T, system *desktopSettingsMem, pool *desktoppool.Pool, base time.Time) *botmgmt.Service {
	t.Helper()
	bots := botmgmt.NewService(system)
	bots.Desktop = botDesktopControl{pool: pool}
	bots.Now = func() time.Time { return base }
	rec := httptest.NewRecorder()
	PostDesktopViewAdminHandler(pool, bots).ServeHTTP(rec, desktopViewRequest(`{"user_id":"alice"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("view status=%d body=%s", rec.Code, rec.Body.String())
	}
	return bots
}

func TestDesktopAdminCheckKeepsTheDesktopAlive(t *testing.T) {
	base := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	system := newDesktopSettingsMem()
	docker, stops := dockerDesktopFake(t, "http://dockerd.example:6080/vnc.html?autoconnect=1")
	bots := adminCheckedDesktop(t, system, desktopPoolFake(t, system, docker.URL), base)
	// A bot command finishing now must leave the watched desktop up.
	stopped, err := bots.StopDesktopIfIdle(context.Background(), "tenant-a", "alice", "inst_alice")
	if err != nil || stopped {
		t.Fatalf("stopped=%v err=%v, the admin's desktop was pulled away", stopped, err)
	}
	if got := atomic.LoadInt32(stops); got != 0 {
		t.Fatalf("docker stops=%d, want 0 while an admin watches", got)
	}

	// The hold expires on its own, so an idle desktop can stop again.
	bots.Now = func() time.Time { return base.Add(botmgmt.AdminDesktopViewHold + time.Minute) }
	stopped, err = bots.StopDesktopIfIdle(context.Background(), "tenant-a", "alice", "inst_alice")
	if err != nil || !stopped {
		t.Fatalf("stopped=%v err=%v, an expired hold must release the desktop", stopped, err)
	}
	if got := atomic.LoadInt32(stops); got != 1 {
		t.Fatalf("docker stops=%d, want 1 after the hold expired", got)
	}
}

func TestDesktopAdminCheckSurvivesHubRestart(t *testing.T) {
	base := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	system := newDesktopSettingsMem()
	docker, _ := dockerDesktopFake(t, "http://dockerd.example:6080/vnc.html?autoconnect=1")
	adminCheckedDesktop(t, system, desktopPoolFake(t, system, docker.URL), base)

	// A restart must not blank the picture the admin is still watching.
	restarted := botmgmt.NewService(system)
	restarted.Now = func() time.Time { return base.Add(time.Minute) }
	stopped, err := restarted.StopDesktopIfIdle(context.Background(), "tenant-a", "alice", "inst_alice")
	if err != nil || stopped {
		t.Fatalf("stopped=%v err=%v, the admin hold did not survive a restart", stopped, err)
	}
}

func TestDesktopAdminStopEndsTheHold(t *testing.T) {
	base := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	system := newDesktopSettingsMem()
	docker, stops := dockerDesktopFake(t, "http://dockerd.example:6080/vnc.html?autoconnect=1")
	pool := desktopPoolFake(t, system, docker.URL)
	bots := adminCheckedDesktop(t, system, pool, base)

	// The admin stops the desktop on purpose: that outranks the hold.
	rec := httptest.NewRecorder()
	PostDesktopStopAdminHandler(pool, bots).ServeHTTP(rec, adminRequest(http.MethodPost, "/api/admin/desktop-services/desktops/stop", `{"user_id":"alice"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("stop status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := atomic.LoadInt32(stops); got != 1 {
		t.Fatalf("docker stops=%d, want 1 after the admin stopped it", got)
	}
}

// desktopTestUsers is an in-memory user repository for the view handler's
// authorized-user list; the tenant filter mirrors ListByTenant semantics.
type desktopTestUsers struct{ items []*store.User }

func (u desktopTestUsers) Create(context.Context, *store.User) error               { return nil }
func (u desktopTestUsers) GetByID(context.Context, string) (*store.User, error)    { return nil, nil }
func (u desktopTestUsers) GetByEmail(context.Context, string) (*store.User, error) { return nil, nil }
func (u desktopTestUsers) GetByTenantEmail(context.Context, string, string) (*store.User, error) {
	return nil, nil
}
func (u desktopTestUsers) List(context.Context) ([]*store.User, error) { return u.items, nil }
func (u desktopTestUsers) ListByTenant(_ context.Context, tenantID string) ([]*store.User, error) {
	if tenantID == "tenant-a" {
		return u.items, nil
	}
	return nil, nil
}
func (u desktopTestUsers) DeleteByEmail(context.Context, string) error               { return nil }
func (u desktopTestUsers) DeleteByTenantEmail(context.Context, string, string) error { return nil }
func (u desktopTestUsers) UpdateSmartRoute(context.Context, string, bool) error      { return nil }
func (u desktopTestUsers) MarkEmailVerified(context.Context, string, string) error   { return nil }
func (u desktopTestUsers) GetByTenantIdentity(context.Context, string, string, string) (*store.User, error) {
	return nil, nil
}
func (u desktopTestUsers) ListIdentitiesByUser(context.Context, string, string) ([]*store.UserIdentity, error) {
	return nil, nil
}
func (u desktopTestUsers) UpsertIdentity(context.Context, *store.UserIdentity) error { return nil }

// desktopUserPool builds a pool whose tenant-a assignments are exactly items.
func desktopUserPool(t *testing.T, system *desktopSettingsMem, items []desktoppool.Assignment) *desktoppool.Pool {
	t.Helper()
	docker, _ := dockerDesktopFake(t, "http://dockerd.example:6080/vnc.html?autoconnect=1")
	pool := desktoppool.New(system, nil)
	server, err := pool.CreateServer(context.Background(), "tenant-a", desktoppool.Server{Name: "srv", BaseURL: docker.URL, AccessToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		item.ServerID = server.ID
		if _, err := pool.CreateAssignment(context.Background(), "tenant-a", item); err != nil {
			t.Fatal(err)
		}
	}
	return pool
}

func TestDesktopServicesViewListsOnlyAuthorizedUsers(t *testing.T) {
	tenantUsers := []*store.User{
		{ID: "alice", TenantID: "tenant-a", Email: "alice@example.com"},
		{ID: "bob", TenantID: "tenant-a", Email: "bob@example.com"},
		{ID: "llmsys", TenantID: "tenant-a", Email: "sys_user@example.com"},
	}
	system := newDesktopSettingsMem()
	users := desktopTestUsers{items: tenantUsers}

	// A user-scope assignment for alice: the view lists alice only, never the
	// user the panel could not open a desktop for.
	pool := desktopUserPool(t, system, []desktoppool.Assignment{{Scope: desktoppool.ScopeUser, TargetID: "alice"}})
	rec := httptest.NewRecorder()
	GetDesktopServicesAdminHandler(pool, users).ServeHTTP(rec, adminRequest(http.MethodGet, "/api/admin/desktop-services", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("view status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Users []desktoppool.AuthorizedUser `json:"users"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Users) != 1 || out.Users[0].ID != "alice" || out.Users[0].Label != "alice@example.com" {
		t.Fatalf("users=%s want alice only", mustJSONOrNil(out.Users))
	}
}

func TestDesktopServicesViewGlobalAssignmentCoversEveryone(t *testing.T) {
	tenantUsers := []*store.User{
		{ID: "alice", TenantID: "tenant-a", Email: "alice@example.com"},
		{ID: "bob", TenantID: "tenant-a", Email: "bob@example.com"},
	}
	system := newDesktopSettingsMem()
	pool := desktopUserPool(t, system, []desktoppool.Assignment{{Scope: desktoppool.ScopeGlobal}})
	rec := httptest.NewRecorder()
	GetDesktopServicesAdminHandler(pool, desktopTestUsers{items: tenantUsers}).ServeHTTP(rec, adminRequest(http.MethodGet, "/api/admin/desktop-services", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("view status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Users []desktoppool.AuthorizedUser `json:"users"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Users) != 2 {
		t.Fatalf("users=%s, want every tenant user", mustJSONOrNil(out.Users))
	}
}

func mustJSONOrNil(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "<unserializable>"
	}
	return string(raw)
}
