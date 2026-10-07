package botmgmt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/upstream"
)

type memSettings struct {
	mu sync.Mutex
	m  map[string]string
}

func (m *memSettings) Get(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.m[key]
	if !ok {
		return "", io.EOF
	}
	return value, nil
}

func (m *memSettings) Set(_ context.Context, key, valueJSON string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.m == nil {
		m.m = map[string]string{}
	}
	m.m[key] = valueJSON
	return nil
}

func TestBotIsMaClawSrvInstance(t *testing.T) {
	var gotAuth, gotName string
	var gotAllow bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/instances":
			var body struct {
				Name               string `json:"name"`
				AllowInvalidConfig bool   `json:"allow_invalid_config"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotName = body.Name
			gotAllow = body.AllowInvalidConfig
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"inst_duty","name":"duty"}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/instances/inst_duty":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"inst_duty"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/instances/inst_duty":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/instances":
			_, _ = w.Write([]byte(`{"items":[{},{}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	svc := NewService(&memSettings{})
	svc.HTTP = srv.Client()
	ctx := context.Background()
	view, err := svc.SaveConnection(ctx, "tenant-a", srv.URL, "secret-token", true)
	if err != nil {
		t.Fatal(err)
	}
	if !view.TokenSet || view.BaseURL != srv.URL {
		t.Fatalf("view = %#v", view)
	}
	raw, _ := json.Marshal(view)
	if strings.Contains(string(raw), "secret-token") {
		t.Fatal("settings view returned the access token")
	}

	bot, err := svc.CreateBot(ctx, "tenant-a", "duty", "night desk")
	if err != nil {
		t.Fatal(err)
	}
	if bot.InstanceID != "inst_duty" || gotName != "duty" || !gotAllow {
		t.Fatalf("bot=%#v name=%s allow=%v", bot, gotName, gotAllow)
	}
	if gotAuth != "Bearer secret-token" {
		t.Fatalf("auth = %q", gotAuth)
	}
	updated, err := svc.UpdateBot(ctx, "tenant-a", bot.ID, "duty renamed", "day desk")
	if err != nil || updated.Name != "duty renamed" || updated.InstanceID != "inst_duty" {
		t.Fatalf("updated=%#v err=%v", updated, err)
	}
	count, err := svc.TestConnection(ctx, "tenant-a")
	if err != nil || count != 2 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	if err := svc.DeleteBot(ctx, "tenant-a", bot.ID); err != nil {
		t.Fatal(err)
	}
	after, err := svc.View(ctx, "tenant-a")
	if err != nil || len(after.Bots) != 0 {
		t.Fatalf("after=%#v err=%v", after, err)
	}
}

func TestBotSettingsStayInsideTenant(t *testing.T) {
	svc := NewService(&memSettings{})
	ctx := context.Background()
	if _, err := svc.SaveConnection(ctx, "tenant-a", "https://a.example", "token-a", true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveConnection(ctx, "tenant-b", "https://b.example", "token-b", true); err != nil {
		t.Fatal(err)
	}
	kept, err := svc.SaveConnection(ctx, "tenant-a", "https://a.example/v1", "", false)
	if err != nil || !kept.TokenSet || kept.BaseURL != "https://a.example/v1" {
		t.Fatalf("kept=%#v err=%v", kept, err)
	}
	other, err := svc.View(ctx, "tenant-b")
	if err != nil || other.BaseURL != "https://b.example" {
		t.Fatalf("other=%#v err=%v", other, err)
	}
	if _, err := svc.SaveConnection(ctx, "tenant-a", "ftp://a.example", "x", true); err == nil {
		t.Fatal("expected invalid url")
	}
	if _, err := svc.CreateBot(ctx, "tenant-c", "solo", ""); err != ErrNotConfigured {
		t.Fatalf("err=%v", err)
	}
}

type chainDir struct {
	email   map[string]string
	group   map[string]string
	parents map[string]string
}

func (d chainDir) Email(_ context.Context, userID string) (string, error) {
	return d.email[userID], nil
}
func (d chainDir) GroupID(_ context.Context, email string) (string, error) {
	return d.group[email], nil
}
func (d chainDir) ParentID(_ context.Context, groupID string) (string, error) {
	return d.parents[groupID], nil
}

type desktopCounter struct {
	open int
	stop int
	url  string
}

func writeInstanceSettings(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/instances/"), "/")
	_, _ = w.Write([]byte(`{"id":"` + id + `","metadata":{"llm_service_group_id":"group-1"}}`))
}

func (d *desktopCounter) Open(context.Context, string, string) (string, error) {
	d.open++
	return d.url, nil
}
func (d *desktopCounter) Stop(context.Context, string, string) error {
	d.stop++
	return nil
}

type scriptedDesktop struct {
	open func() (string, error)
	stop func() error
}

func (d *scriptedDesktop) Open(context.Context, string, string) (string, error) {
	return d.open()
}
func (d *scriptedDesktop) Stop(context.Context, string, string) error {
	if d.stop == nil {
		return nil
	}
	return d.stop()
}

func TestBotFeatureIsOffUntilGranted(t *testing.T) {
	var posted, instanceBody, desktopBind string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/me":
			_, _ = w.Write([]byte(`{"id":"svc","tenant_id":"maclaw-tenant"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/tenants/maclaw-tenant/users":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"user_member"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/credentials"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"api_key":"key-member","api_secret":"sec-member"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-member","principal":{"user_id":"user_member"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/instances":
			raw, _ := io.ReadAll(r.Body)
			instanceBody = string(raw)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"inst_alice"}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			_, _ = w.Write([]byte(`{"id":"inst_alice","metadata":{"llm_service_group_id":"group-1","hub_user_id":"someone-else"}}`))
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			raw, _ := io.ReadAll(r.Body)
			desktopBind = string(raw)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			posted = r.URL.Path
			_, _ = w.Write([]byte(`{"message":{"content":"opened example.org"},"desktop_handoff":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	svc := NewService(&memSettings{})
	svc.HTTP = srv.Client()
	svc.Directory = chainDir{
		email:   map[string]string{"alice": "alice@example.com", "carol": "carol@example.com"},
		group:   map[string]string{"alice@example.com": "eng", "carol@example.com": "eng-child"},
		parents: map[string]string{"eng-child": "eng"},
	}
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc.Desktop = desk
	ctx := context.Background()
	if _, err := svc.SaveConnection(ctx, "tenant-a", srv.URL, "secret-token", true); err != nil {
		t.Fatal(err)
	}
	on, err := svc.Enabled(ctx, "tenant-a", "alice")
	if err != nil || on {
		t.Fatalf("default enabled=%v err=%v", on, err)
	}
	if _, err := svc.CreateBotForUser(ctx, "tenant-a", "alice", "duty", "night"); err != ErrDisabled {
		t.Fatalf("create while off: %v", err)
	}
	if _, err := svc.CreateGrant(ctx, "tenant-a", Grant{Scope: ScopeDepartment, TargetID: "eng"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateBotForUser(ctx, "tenant-a", "alice", "duty", "night"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("create without admin secret: %v", err)
	}
	if _, err := svc.SaveAdminSecret(ctx, "tenant-a", "admin-secret"); err != nil {
		t.Fatal(err)
	}
	for _, userID := range []string{"alice", "carol"} {
		bot, err := svc.CreateBotForUser(ctx, "tenant-a", userID, userID+" bot", "desk")
		if err != nil {
			t.Fatal(err)
		}
		if bot.OwnerUserID != userID || bot.InstanceID != "inst_alice" {
			t.Fatalf("bot=%#v", bot)
		}
	}
	alice, err := svc.BotsForUser(ctx, "tenant-a", "alice")
	if err != nil || len(alice) != 1 || alice[0].Name != "alice bot" {
		t.Fatalf("alice=%#v err=%v", alice, err)
	}
	reply, err := svc.PostMessage(ctx, "tenant-a", "alice", alice[0].ID, "open example.org")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != "opened example.org" || !reply.Handoff || !strings.HasPrefix(reply.NovncURL, "/api/v1/desktop-handoff/") || strings.Contains(reply.NovncURL, "dockerd.example") || strings.Contains(reply.NovncURL, ":6081") {
		t.Fatalf("reply=%#v", reply)
	}
	watch, control, err := svc.DesktopWatch(ctx, "tenant-a", "alice", alice[0].ID)
	if err != nil || !control || watch != reply.NovncURL {
		t.Fatalf("watch=%q control=%v reply=%q err=%v", watch, control, reply.NovncURL, err)
	}
	again, againControl, err := svc.DesktopWatch(ctx, "tenant-a", "alice", alice[0].ID)
	if err != nil || !againControl || again != watch {
		t.Fatalf("watch rotated %q -> %q control=%v err=%v", watch, again, againControl, err)
	}
	if _, _, err := svc.DesktopWatch(ctx, "tenant-a", "carol", alice[0].ID); err != ErrNotFound {
		t.Fatalf("other user watch err=%v", err)
	}
	if posted != "/api/v1/instances/inst_alice/messages" {
		t.Fatalf("posted %s", posted)
	}
	if !strings.Contains(instanceBody, `"hub_user_id":"carol"`) || !strings.Contains(instanceBody, `"hub_tenant_id":"tenant-a"`) || strings.Contains(instanceBody, "maclaw-tenant") {
		t.Fatalf("instance=%s", instanceBody)
	}
	if !strings.Contains(desktopBind, `"hub_user_id":"alice"`) || !strings.Contains(desktopBind, `"hub_tenant_id":"tenant-a"`) || !strings.Contains(desktopBind, `"llm_service_group_id":"group-1"`) || strings.Contains(desktopBind, "someone-else") {
		t.Fatalf("desktop bind=%s", desktopBind)
	}
	if desk.open != 1 || desk.stop != 0 {
		t.Fatalf("desktop open=%d stop=%d", desk.open, desk.stop)
	}
	if _, err := svc.PostMessage(ctx, "tenant-a", "bob", alice[0].ID, "no"); err != ErrDisabled {
		t.Fatalf("bob err=%v", err)
	}
}

func TestLoginHandoffKeepsTheDesktop(t *testing.T) {
	var sessionKey string
	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			posts++
			switch posts {
			case 1:
				var body struct {
					SessionKey string `json:"client_session_key"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				sessionKey = body.SessionKey
				_, _ = w.Write([]byte(`{"message":{"content":""},"desktop_handoff":true}`))
			case 2, 4:
				_, _ = w.Write([]byte(`{"message":{"content":"已收到"}}`))
			default:
				http.Error(w, "down", http.StatusBadGateway)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
		Bots: []Bot{
			{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"},
			{ID: "bot_other", Name: "other", InstanceID: "inst_other", OwnerUserID: "alice"},
		},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc.Desktop = desk
	reply, err := svc.PostMessage(context.Background(), "tenant-a", "alice", "bot_alice", "继续")
	if err != nil {
		t.Fatal(err)
	}
	if !reply.Handoff || !strings.HasPrefix(reply.NovncURL, "/api/v1/desktop-handoff/") || strings.Contains(reply.NovncURL, "dockerd.example") || reply.Text == "" {
		t.Fatalf("reply=%#v", reply)
	}
	if desk.open != 1 || desk.stop != 0 {
		t.Fatalf("desktop open=%d stop=%d", desk.open, desk.stop)
	}
	if sessionKey != "bot_alice" {
		t.Fatalf("session key=%q", sessionKey)
	}
	if _, err := svc.PostMessage(context.Background(), "tenant-a", "alice", "bot_other", "别的 bot"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostMessage(context.Background(), "tenant-a", "alice", "bot_other", "失败"); err == nil {
		t.Fatal("other bot failure should surface")
	}
	if desk.stop != 0 {
		t.Fatalf("another bot stopped the desktop during login: %d", desk.stop)
	}
	if _, err := svc.PostMessage(context.Background(), "tenant-a", "alice", "bot_alice", "登录完成"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostMessage(context.Background(), "tenant-a", "alice", "bot_alice", "失败"); err == nil {
		t.Fatal("failure after the handoff bot finished should surface")
	}
	if desk.stop != 1 {
		t.Fatalf("desktop was not stopped after the handoff bot finished: %d", desk.stop)
	}
}

func TestHandoffWithoutAPictureStillKeepsTheDesktop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "inst_other"):
			http.Error(w, "down", http.StatusBadGateway)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			_, _ = w.Write([]byte(`{"message":{"content":""},"desktop_handoff":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
		Bots: []Bot{
			{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"},
			{ID: "bot_other", Name: "other", InstanceID: "inst_other", OwnerUserID: "alice"},
		},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	desk := &desktopCounter{}
	svc.Desktop = desk
	svc.NoteDesktopView("tenant-a", "alice", "http://dockerd.example:6081/vnc.html?autoconnect=1")
	reply, err := svc.PostMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开网站")
	if err != nil {
		t.Fatal(err)
	}
	if !reply.Handoff || !strings.HasPrefix(reply.NovncURL, "/api/v1/desktop-handoff/") || strings.Contains(reply.NovncURL, "dockerd.example") {
		t.Fatalf("reply=%#v", reply)
	}
	if !svc.desktopHeldByPerson("tenant-a", "alice") {
		t.Fatal("handoff cleared the login hold")
	}
	if _, err := svc.PostMessage(context.Background(), "tenant-a", "alice", "bot_other", "失败"); err == nil {
		t.Fatal("other bot failure should surface")
	}
	if desk.stop != 0 {
		t.Fatalf("missing picture stopped the desktop during login: %d", desk.stop)
	}
}

func TestLoginHoldArrivesBeforeTheReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "inst_other"):
			http.Error(w, "down", http.StatusBadGateway)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			_, _ = w.Write([]byte(`{"message":{"content":"已收到"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
		Bots: []Bot{
			{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"},
			{ID: "bot_other", Name: "other", InstanceID: "inst_other", OwnerUserID: "alice"},
			{ID: "bot_bob", Name: "bob", InstanceID: "inst_bob", OwnerUserID: "bob"},
		},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc.Desktop = desk
	if err := svc.HoldDesktop(context.Background(), "tenant-a", "alice", "inst_bob"); err != ErrNotFound {
		t.Fatalf("another user's instance held the desktop: %v", err)
	}
	svc.NoteDesktopView("tenant-a", "alice", "http://dockerd.example:6081/vnc.html?autoconnect=1")
	if err := svc.HoldDesktop(context.Background(), "tenant-a", "alice", "inst_alice"); err != nil {
		t.Fatal(err)
	}
	if _, control, err := svc.DesktopWatch(context.Background(), "tenant-a", "alice", "bot_alice"); err != nil || !control {
		t.Fatalf("login bot did not get the keyboard: control=%v err=%v", control, err)
	}
	if _, control, err := svc.DesktopWatch(context.Background(), "tenant-a", "alice", "bot_other"); err != nil || control {
		t.Fatalf("another bot was given the keyboard during login: control=%v err=%v", control, err)
	}
	svc.releaseDesktopKeyboard("tenant-a", "alice", "bot_alice")
	if _, control, err := svc.DesktopWatch(context.Background(), "tenant-a", "alice", "bot_alice"); err != nil || control {
		t.Fatalf("continuation left the keyboard with the person: control=%v err=%v", control, err)
	}
	if _, err := svc.PostMessage(context.Background(), "tenant-a", "alice", "bot_other", "失败"); err == nil {
		t.Fatal("other bot failure should surface")
	}
	if desk.stop != 0 {
		t.Fatalf("another bot stopped the desktop before the login reply: %d", desk.stop)
	}
	if _, err := svc.PostMessage(context.Background(), "tenant-a", "alice", "bot_alice", "登录完成"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostMessage(context.Background(), "tenant-a", "alice", "bot_other", "再失败"); err == nil {
		t.Fatal("failure after the login finished should surface")
	}
	if desk.stop != 1 {
		t.Fatalf("desktop was not stopped after the login bot finished: %d", desk.stop)
	}
}

func TestFailedContinuationKeepsTheLoginDesktop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			http.Error(w, "down", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
		Bots: []Bot{
			{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"},
		},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc.Desktop = desk
	svc.NoteDesktopView("tenant-a", "alice", "http://dockerd.example:6081/vnc.html?autoconnect=1")
	if err := svc.HoldDesktop(context.Background(), "tenant-a", "alice", "inst_alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostMessage(context.Background(), "tenant-a", "alice", "bot_alice", "登录完成"); err == nil {
		t.Fatal("failed continuation should surface")
	}
	if desk.stop != 0 {
		t.Fatalf("failed continuation stopped the logged-in desktop: %d", desk.stop)
	}
	if _, control, err := svc.DesktopWatch(context.Background(), "tenant-a", "alice", "bot_alice"); err != nil || !control {
		t.Fatalf("failed continuation took the keyboard: control=%v err=%v", control, err)
	}
}

func TestFailedCommandDoesNotHandOverAViewOnlyDesktop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			http.Error(w, "down", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
		Bots:   []Bot{{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc.Desktop = desk
	svc.NoteDesktopView("tenant-a", "alice", "http://dockerd.example:6081/vnc.html?autoconnect=1")
	svc.keepDesktopWithoutKeyboard("tenant-a", "alice", "bot_alice")
	if _, err := svc.PostMessage(context.Background(), "tenant-a", "alice", "bot_alice", "再试一次"); err == nil {
		t.Fatal("failed command should surface")
	}
	if desk.stop != 0 {
		t.Fatalf("failed command stopped the desktop: %d", desk.stop)
	}
	if _, control, err := svc.DesktopWatch(context.Background(), "tenant-a", "alice", "bot_alice"); err != nil || control {
		t.Fatalf("failed command handed over the keyboard: control=%v err=%v", control, err)
	}
}

func TestIdentityWriteFailureKeepsTheLoginDesktop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			http.Error(w, "identity", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
		Bots: []Bot{
			{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"},
		},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc.Desktop = desk
	if _, err := svc.PostMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开网站"); err == nil {
		t.Fatal("identity failure should surface")
	}
	if desk.stop != 1 {
		t.Fatalf("a fresh command left the desktop running: %d", desk.stop)
	}
	svc.NoteDesktopView("tenant-a", "alice", "http://dockerd.example:6081/vnc.html?autoconnect=1")
	if err := svc.HoldDesktop(context.Background(), "tenant-a", "alice", "inst_alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostMessage(context.Background(), "tenant-a", "alice", "bot_alice", "登录完成"); err == nil {
		t.Fatal("identity failure during login should surface")
	}
	if desk.stop != 1 {
		t.Fatalf("identity failure stopped the logged-in desktop: %d", desk.stop)
	}
	if _, control, err := svc.DesktopWatch(context.Background(), "tenant-a", "alice", "bot_alice"); err != nil || !control {
		t.Fatalf("identity failure took the keyboard: control=%v err=%v", control, err)
	}
}

func TestUnreadInstanceSettingsAreNotReplaced(t *testing.T) {
	var patched, posted int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			http.Error(w, "unread", http.StatusBadGateway)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			patched++
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			posted++
			_, _ = w.Write([]byte(`{"message":{"content":"不应发出"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
		Bots:   []Bot{{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc.Desktop = desk
	if _, err := svc.PostMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开网站"); err == nil {
		t.Fatal("unread instance settings should surface")
	}
	if patched != 0 || posted != 0 {
		t.Fatalf("unread settings were written patched=%d posted=%d", patched, posted)
	}
	if desk.stop != 1 {
		t.Fatalf("a fresh command left the desktop running: %d", desk.stop)
	}
	svc.NoteDesktopView("tenant-a", "alice", "http://dockerd.example:6081/vnc.html?autoconnect=1")
	if err := svc.HoldDesktop(context.Background(), "tenant-a", "alice", "inst_alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostMessage(context.Background(), "tenant-a", "alice", "bot_alice", "登录完成"); err == nil {
		t.Fatal("unread settings during login should surface")
	}
	if patched != 0 || posted != 0 {
		t.Fatalf("login continuation replaced the instance patched=%d posted=%d", patched, posted)
	}
	if desk.stop != 1 {
		t.Fatalf("unread settings stopped the logged-in desktop: %d", desk.stop)
	}
	if _, control, err := svc.DesktopWatch(context.Background(), "tenant-a", "alice", "bot_alice"); err != nil || !control {
		t.Fatalf("unread settings took the keyboard: control=%v err=%v", control, err)
	}
}

func TestDeletingTheLoginBotReleasesTheSharedDesktop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
		Bots: []Bot{
			{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"},
			{ID: "bot_other", Name: "other", InstanceID: "inst_other", OwnerUserID: "alice"},
		},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc.Desktop = desk
	if err := svc.HoldDesktop(context.Background(), "tenant-a", "alice", "inst_alice"); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteBot(context.Background(), "tenant-a", "bot_alice"); err != nil {
		t.Fatal(err)
	}
	if desk.stop != 0 {
		t.Fatalf("deleting one bot stopped the desktop another bot of this user still uses: %d", desk.stop)
	}
	if svc.desktopHeldByPerson("tenant-a", "alice") {
		t.Fatal("deleted bot still holds the shared desktop")
	}
}

func TestDeletingTheOnlyLoginBotStopsTheDesktop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
		Bots: []Bot{
			{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"},
		},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc.Desktop = desk
	if err := svc.HoldDesktop(context.Background(), "tenant-a", "alice", "inst_alice"); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteBot(context.Background(), "tenant-a", "bot_alice"); err != nil {
		t.Fatal(err)
	}
	if desk.stop != 1 {
		t.Fatalf("deleting the only login bot left its desktop running: %d", desk.stop)
	}
	if svc.desktopHeldByPerson("tenant-a", "alice") {
		t.Fatal("deleted bot still holds the shared desktop")
	}
}

func TestFailedCommandDoesNotStopTheDesktopAnotherBotIsUsing(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/instances/inst_other/"):
			once.Do(func() { close(started) })
			<-release
			_, _ = w.Write([]byte(`{"message":{"content":"还在这个桌面里"}}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/instances/inst_alice/"):
			http.Error(w, "down", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
		Bots: []Bot{
			{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"},
			{ID: "bot_other", Name: "other", InstanceID: "inst_other", OwnerUserID: "alice"},
		},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc.Desktop = desk
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = svc.PostMessage(context.Background(), "tenant-a", "alice", "bot_other", "继续操作")
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the other bot did not open the shared desktop")
	}
	if _, err := svc.PostMessage(context.Background(), "tenant-a", "alice", "bot_alice", "失败"); err == nil {
		t.Fatal("rejected message should surface")
	}
	if desk.stop != 0 {
		t.Fatalf("a failed bot stopped the desktop the other bot is using: %d", desk.stop)
	}
	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the other bot did not finish")
	}
	if desk.stop != 0 {
		t.Fatalf("finishing the other bot stopped the shared desktop from Hub: %d", desk.stop)
	}
}

func TestFailedStopDoesNotCloseTheDesktopTheNextCommandIsOpening(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	aboutToLock := make(chan struct{})
	finishDone := make(chan struct{})
	var mu sync.Mutex
	var opens, stopped int
	desk := &scriptedDesktop{
		open: func() (string, error) {
			mu.Lock()
			opens++
			n := opens
			mu.Unlock()
			if n == 2 {
				close(entered)
				<-release
			}
			return "http://dockerd.example:6080/vnc.html?autoconnect=1", nil
		},
		stop: func() error {
			mu.Lock()
			stopped++
			mu.Unlock()
			return nil
		},
	}
	svc := NewService(&memSettings{})
	svc.Desktop = desk
	ctx := context.Background()
	if _, opened, err := svc.openDesktop(ctx, "tenant-a", "alice", "", ""); err != nil || !opened {
		t.Fatalf("open opened=%v err=%v", opened, err)
	}
	svc.beforeDesktopStop = func() {
		go func() {
			_, _, _ = svc.openDesktop(ctx, "tenant-a", "alice", "", "")
		}()
		<-entered
		close(aboutToLock)
	}
	go func() {
		defer close(finishDone)
		svc.finishDesktopOpen("tenant-a", "alice", "", true, true)
	}()
	select {
	case <-aboutToLock:
	case <-time.After(2 * time.Second):
		t.Fatal("stop did not reach the next open")
	}
	mu.Lock()
	if stopped != 0 {
		got := stopped
		mu.Unlock()
		t.Fatalf("stopped while the next command is opening the desktop: %d", got)
	}
	mu.Unlock()
	close(release)
	select {
	case <-finishDone:
	case <-time.After(2 * time.Second):
		t.Fatal("stop did not finish after the next open")
	}
	mu.Lock()
	defer mu.Unlock()
	if stopped != 0 {
		t.Fatalf("the failed command stopped the desktop the next command opened: %d", stopped)
	}
}

func TestMessageTimeoutLeavesTheDesktopUp(t *testing.T) {
	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			posts++
			if posts == 1 {
				http.Error(w, "down", http.StatusBadGateway)
				return
			}
			time.Sleep(200 * time.Millisecond)
			_, _ = w.Write([]byte(`{"message":{"content":"晚了"},"desktop_handoff":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
		Bots:   []Bot{{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	svc.messageTimeout = 40 * time.Millisecond
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc.Desktop = desk
	if _, err := svc.PostMessage(context.Background(), "tenant-a", "alice", "bot_alice", "失败"); err == nil {
		t.Fatal("rejected message should surface")
	}
	if desk.stop != 1 {
		t.Fatalf("failed message stop=%d", desk.stop)
	}
	if _, err := svc.PostMessage(context.Background(), "tenant-a", "alice", "bot_alice", "继续"); err == nil {
		t.Fatal("timed out message should surface")
	}
	if desk.stop != 1 {
		t.Fatalf("timeout stopped the desktop: %d", desk.stop)
	}
	watch, control, err := svc.DesktopWatch(context.Background(), "tenant-a", "alice", "bot_alice")
	if err != nil || watch == "" || control {
		t.Fatalf("timeout changed the desktop watch=%q control=%v err=%v", watch, control, err)
	}
}

func TestTimeoutKeepsTheLoginDesktopForThisBot(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/instances/inst_alice/"):
			<-release
			_, _ = w.Write([]byte(`{"message":{"content":"晚了"}}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/instances/inst_other/"):
			http.Error(w, "down", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
		Bots: []Bot{
			{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"},
			{ID: "bot_other", Name: "other", InstanceID: "inst_other", OwnerUserID: "alice"},
		},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	svc.messageTimeout = 40 * time.Millisecond
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc.Desktop = desk
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = svc.PostMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开网站")
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out command did not return")
	}
	watch, control, err := svc.DesktopWatch(context.Background(), "tenant-a", "alice", "bot_alice")
	if err != nil || watch == "" || control {
		t.Fatalf("a slow task was handed the keyboard watch=%q control=%v err=%v", watch, control, err)
	}
	if _, err := svc.PostMessage(context.Background(), "tenant-a", "alice", "bot_other", "失败"); err == nil {
		t.Fatal("rejected message should surface")
	}
	if desk.stop != 0 {
		t.Fatalf("another bot stopped the desktop after a timeout: %d", desk.stop)
	}
	close(release)
}

func TestTimeoutDuringLoginReturnsTheKeyboard(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/messages"):
			<-release
			_, _ = w.Write([]byte(`{"message":{"content":"晚了"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
		Bots:   []Bot{{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	svc.messageTimeout = 40 * time.Millisecond
	svc.Desktop = &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc.noteDesktopHeld("tenant-a", "alice", "bot_alice")
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = svc.PostMessage(context.Background(), "tenant-a", "alice", "bot_alice", "登录完成")
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out login continuation did not return")
	}
	watch, control, err := svc.DesktopWatch(context.Background(), "tenant-a", "alice", "bot_alice")
	if err != nil || watch == "" || !control {
		t.Fatalf("timeout took the keyboard during login watch=%q control=%v err=%v", watch, control, err)
	}
	close(release)
}

func TestStopLeavesTheDesktopWhileThePersonIsLoggingIn(t *testing.T) {
	svc := NewService(&memSettings{})
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc.Desktop = desk
	if _, _, err := svc.openDesktop(context.Background(), "tenant-a", "alice", "", ""); err != nil {
		t.Fatal(err)
	}
	svc.noteDesktopHeld("tenant-a", "alice", "bot_alice")
	if svc.ReleaseDesktopIfIdle("tenant-a", "alice") {
		t.Fatal("stop was allowed while the person is logging in")
	}
	if svc.desktopViewURL("tenant-a", "alice") == "" || !svc.desktopKeyboardForBot("tenant-a", "alice", "bot_alice") {
		t.Fatal("login desktop was cleared")
	}
	svc.releaseDesktopKeyboard("tenant-a", "alice", "bot_alice")
	if !svc.ReleaseDesktopIfIdle("tenant-a", "alice") {
		t.Fatal("an idle desktop could not be stopped")
	}
	if svc.desktopHeldByPerson("tenant-a", "alice") {
		t.Fatal("the idle pin survived the stop")
	}
}

func TestStopLeavesTheDesktopAnotherCommandAlreadyOpened(t *testing.T) {
	svc := NewService(&memSettings{})
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc.Desktop = desk
	if _, _, err := svc.openDesktop(context.Background(), "tenant-a", "alice", "", ""); err != nil {
		t.Fatal(err)
	}
	svc.beginDesktopUse("tenant-a", "alice", "")
	stopped, err := svc.StopDesktopIfIdle(context.Background(), "tenant-a", "alice", "")
	if err != nil || stopped || desk.stop != 0 {
		t.Fatalf("stopped=%v stops=%d err=%v", stopped, desk.stop, err)
	}
	if svc.desktopViewURL("tenant-a", "alice") == "" {
		t.Fatal("the logged-in desktop was dropped")
	}
}

func TestAnotherInstanceStopLeavesTheDesktopThisCommandOpened(t *testing.T) {
	svc := NewService(&memSettings{})
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc.Desktop = desk
	if _, _, err := svc.openDesktop(context.Background(), "tenant-a", "alice", "inst_a", ""); err != nil {
		t.Fatal(err)
	}
	stopped, err := svc.StopDesktopIfIdle(context.Background(), "tenant-a", "alice", "inst_b")
	if err != nil || stopped || desk.stop != 0 {
		t.Fatalf("another instance stopped the browser this command is using: stopped=%v stops=%d err=%v", stopped, desk.stop, err)
	}
	if svc.desktopViewURL("tenant-a", "alice") == "" {
		t.Fatal("the logged-in desktop was dropped")
	}
	stopped, err = svc.StopDesktopIfIdle(context.Background(), "tenant-a", "alice", "inst_a")
	if err != nil || !stopped || desk.stop != 1 {
		t.Fatalf("the instance that finished left the desktop up: stopped=%v stops=%d err=%v", stopped, desk.stop, err)
	}
}

func TestContinueCountsTheCommandBeforeReturningTheKeyboard(t *testing.T) {
	svc := NewService(&memSettings{})
	var opening int
	var awaiting bool
	svc.Desktop = &scriptedDesktop{open: func() (string, error) {
		key := desktopViewKey("tenant-a", "alice")
		svc.mu.Lock()
		opening = svc.desktopOpening[key]
		awaiting = svc.desktopAwaiting[key]
		svc.mu.Unlock()
		return "http://dockerd.example:6081/vnc.html?autoconnect=1", nil
	}}
	svc.noteDesktopHeld("tenant-a", "alice", "bot_1")
	if _, _, err := svc.openDesktop(context.Background(), "tenant-a", "alice", "inst_a", "bot_1"); err != nil {
		t.Fatal(err)
	}
	if opening < 1 || awaiting {
		t.Fatalf("opening=%d awaiting=%v, the logged-in browser can be stopped before this command is counted", opening, awaiting)
	}
	stopped, err := svc.StopDesktopIfIdle(context.Background(), "tenant-a", "alice", "inst_b")
	if err != nil || stopped {
		t.Fatalf("another instance stopped the browser this command is using: stopped=%v err=%v", stopped, err)
	}
}

func TestStopFinishesBeforeTheNextCommandOpensTheDesktop(t *testing.T) {
	started := make(chan struct{})
	releaseStop := make(chan struct{})
	var mu sync.Mutex
	var order []string
	svc := NewService(&memSettings{})
	svc.Desktop = &scriptedDesktop{
		open: func() (string, error) {
			mu.Lock()
			order = append(order, "open")
			mu.Unlock()
			return "http://dockerd.example:6081/vnc.html?autoconnect=1", nil
		},
		stop: func() error {
			mu.Lock()
			order = append(order, "stop")
			mu.Unlock()
			close(started)
			<-releaseStop
			return nil
		},
	}
	svc.beginDesktopUse("tenant-a", "alice", "")
	done := make(chan bool, 1)
	go func() {
		stopped, err := svc.StopDesktopIfIdle(context.Background(), "tenant-a", "alice", "")
		if err != nil {
			t.Errorf("stop: %v", err)
		}
		done <- stopped
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("stop did not start")
	}
	opened := make(chan error, 1)
	go func() {
		_, _, err := svc.openDesktop(context.Background(), "tenant-a", "alice", "", "")
		opened <- err
	}()
	select {
	case err := <-opened:
		t.Fatalf("the next command opened the desktop while the previous stop was still writing the login: %v", err)
	case <-time.After(80 * time.Millisecond):
	}
	close(releaseStop)
	select {
	case stopped := <-done:
		if !stopped {
			t.Fatal("the idle desktop was not stopped")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stop did not finish")
	}
	select {
	case err := <-opened:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the next command did not open the desktop")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "stop" || order[1] != "open" {
		t.Fatalf("order = %v", order)
	}
}

func TestBotsOfOneUserAreThatUsersInstances(t *testing.T) {
	var mu sync.Mutex
	createdUsers := map[string]int{}
	instanceAuth := map[string]string{}
	var next int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/me":
			if r.Header.Get("Authorization") != "Bearer secret-token" {
				http.Error(w, "auth", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"id":"svc","tenant_id":"maclaw-tenant"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/tenants/maclaw-tenant/users":
			if r.Header.Get("X-MaClaw-Admin-Secret") != "admin-secret" {
				http.Error(w, "admin", http.StatusUnauthorized)
				return
			}
			var body struct {
				Email string `json:"email"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			createdUsers[body.Email]++
			id := "user_bob"
			if strings.Contains(body.Email, ownerEmail("alice")[:12]) || body.Email == ownerEmail("alice") {
				id = "user_alice"
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"` + id + `"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/credentials"):
			userID := "user_bob"
			if strings.Contains(r.URL.Path, "user_alice") {
				userID = "user_alice"
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"api_key":"key-` + userID + `","api_secret":"sec-` + userID + `"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			var body struct {
				APIKey string `json:"api_key"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			userID := strings.TrimPrefix(body.APIKey, "key-")
			_, _ = w.Write([]byte(`{"access_token":"bearer-` + userID + `","principal":{"user_id":"` + userID + `"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/instances":
			next++
			id := fmt.Sprintf("inst_%d", next)
			instanceAuth[id] = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"` + id + `"}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			_, _ = w.Write([]byte(`{"message":{"content":"done"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	svc := NewService(&memSettings{})
	svc.HTTP = srv.Client()
	ctx := context.Background()
	if _, err := svc.SaveConnection(ctx, "tenant-a", srv.URL, "secret-token", true); err != nil {
		t.Fatal(err)
	}
	view, err := svc.SaveAdminSecret(ctx, "tenant-a", "admin-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !view.AdminSecretSet || strings.Contains(fmt.Sprint(view), "admin-secret") {
		t.Fatalf("view leaked the admin secret: %#v", view)
	}
	if _, err := svc.CreateGrant(ctx, "tenant-a", Grant{Scope: ScopeUser, TargetID: "alice"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateGrant(ctx, "tenant-a", Grant{Scope: ScopeUser, TargetID: "bob"}); err != nil {
		t.Fatal(err)
	}
	first, err := svc.CreateBotForUser(ctx, "tenant-a", "alice", "one", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateBotForUser(ctx, "tenant-a", "alice", "two", "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := svc.CreateBotForUser(ctx, "tenant-a", "bob", "other", "")
	if err != nil {
		t.Fatal(err)
	}
	if instanceAuth[first.InstanceID] != "Bearer bearer-user_alice" || instanceAuth[second.InstanceID] != "Bearer bearer-user_alice" {
		t.Fatalf("alice instances auth=%v", instanceAuth)
	}
	if instanceAuth[other.InstanceID] != "Bearer bearer-user_bob" {
		t.Fatalf("bob auth=%q", instanceAuth[other.InstanceID])
	}
	if createdUsers[ownerEmail("alice")] != 1 || createdUsers[ownerEmail("bob")] != 1 {
		t.Fatalf("created users=%v", createdUsers)
	}
	if _, err := svc.PostMessage(ctx, "tenant-a", "alice", first.ID, "go"); err != nil {
		t.Fatal(err)
	}
}

func TestDesktopWatchClearsWhenTheDesktopStops(t *testing.T) {
	svc := NewService(&memSettings{})
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc.Desktop = desk
	ctx := context.Background()
	if _, opened, err := svc.openDesktop(ctx, "tenant-a", "alice", "", ""); err != nil || !opened {
		t.Fatalf("open opened=%v err=%v", opened, err)
	}
	first := svc.desktopViewURL("tenant-a", "alice")
	if !strings.HasPrefix(first, "/api/v1/desktop-handoff/") || strings.Contains(first, "6081") {
		t.Fatalf("view=%q", first)
	}
	if _, _, err := svc.openDesktop(ctx, "tenant-a", "alice", "", ""); err != nil {
		t.Fatal(err)
	}
	if again := svc.desktopViewURL("tenant-a", "alice"); again != first {
		t.Fatalf("view rotated %q -> %q", first, again)
	}
	svc.NoteDesktopView("tenant-a", "alice", "http://dockerd.example:6099/vnc.html?autoconnect=1")
	if moved := svc.desktopViewURL("tenant-a", "alice"); moved == "" || moved == first || strings.Contains(moved, "6099") {
		t.Fatalf("picture did not follow the session view=%q", moved)
	}
	if svc.desktopViewURL("tenant-a", "bob") != "" {
		t.Fatal("bob can see alice's desktop")
	}
	svc.ForgetDesktopView("tenant-a", "alice")
	if svc.desktopViewURL("tenant-a", "alice") != "" {
		t.Fatal("stopped desktop is still offered to the chat")
	}
	if _, _, err := svc.openDesktop(ctx, "tenant-a", "alice", "", ""); err != nil {
		t.Fatal(err)
	}
	if svc.desktopViewURL("tenant-a", "alice") == "" {
		t.Fatal("desktop picture did not return")
	}
	if err := svc.stopDesktop(ctx, "tenant-a", "alice"); err != nil {
		t.Fatal(err)
	}
	if svc.desktopViewURL("tenant-a", "alice") != "" {
		t.Fatal("stopped desktop is still visible")
	}
	if desk.stop != 1 {
		t.Fatalf("stop=%d", desk.stop)
	}
}

func TestDesktopStateSurvivesRestart(t *testing.T) {
	settings := &memSettings{}
	raw, err := json.Marshal(record{
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
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}

	// First process: the desktop picture is noted and the bot hands the
	// browser to the person for a website login.
	svc1 := NewService(settings)
	svc1.NoteDesktopView("tenant-a", "alice", "http://dockerd.example:6081/vnc.html?autoconnect=1")
	if err := svc1.HoldDesktop(context.Background(), "tenant-a", "alice", "inst_alice"); err != nil {
		t.Fatal(err)
	}

	// Hub restarts. A fresh service over the same store must still see the
	// login pin, or the next stop kills the browser the person is typing in.
	svc2 := NewService(settings)
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc2.Desktop = desk
	stopped, err := svc2.StopDesktopIfIdle(context.Background(), "tenant-a", "alice", "inst_alice")
	if err != nil {
		t.Fatal(err)
	}
	if stopped || desk.stop != 0 {
		t.Fatal("restart forgot the login pin and stopped the person's browser")
	}

	// The restored picture is still servable through its handoff token and
	// the keyboard is still bound to the login bot.
	url, control, err := svc2.DesktopWatch(context.Background(), "tenant-a", "alice", "bot_alice")
	if err != nil || url == "" || !control {
		t.Fatalf("restored watch broken: url=%q control=%v err=%v", url, control, err)
	}
	token, _, err := ParseHandoffPath(url)
	if err != nil {
		t.Fatalf("restored gated url is not a handoff: %v", err)
	}
	if svc2.handoffOrigin(token) == "" {
		t.Fatal("restored handoff token no longer resolves")
	}

	// The release persists too: after the login finishes, a third process is
	// allowed to stop the desktop.
	svc2.releaseDesktopKeyboard("tenant-a", "alice", "bot_alice")
	svc3 := NewService(settings)
	desk3 := &desktopCounter{url: ""}
	svc3.Desktop = desk3
	stopped, err = svc3.StopDesktopIfIdle(context.Background(), "tenant-a", "alice", "inst_alice")
	if err != nil {
		t.Fatal(err)
	}
	if !stopped || desk3.stop != 1 {
		t.Fatalf("released desktop was kept after restart: stopped=%v stops=%d", stopped, desk3.stop)
	}
}

type countingSettings struct {
	memSettings
	mu   sync.Mutex
	sets int
}

func (c *countingSettings) Set(ctx context.Context, key, valueJSON string) error {
	c.mu.Lock()
	c.sets++
	c.mu.Unlock()
	return c.memSettings.Set(ctx, key, valueJSON)
}

func TestDesktopHoldPersistSkipsUnchangedState(t *testing.T) {
	settings := &countingSettings{}
	svc := NewService(settings)
	if err := svc.HoldDesktop(context.Background(), "tenant-a", "alice", "inst_alice"); err != ErrNotFound {
		t.Fatalf("expected not-found before any bot exists: %v", err)
	}
	raw, err := json.Marshal(record{
		Bots:   []Bot{{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}

	// The first hold writes the pin, the identical second hold must not.
	if err := svc.HoldDesktop(context.Background(), "tenant-a", "alice", "inst_alice"); err != nil {
		t.Fatal(err)
	}
	settings.mu.Lock()
	afterFirst := settings.sets
	settings.mu.Unlock()
	if afterFirst == 0 {
		t.Fatal("first hold never persisted the pin")
	}
	if err := svc.HoldDesktop(context.Background(), "tenant-a", "alice", "inst_alice"); err != nil {
		t.Fatal(err)
	}
	settings.mu.Lock()
	afterSecond := settings.sets
	settings.mu.Unlock()
	if afterSecond != afterFirst {
		t.Fatalf("unchanged hold rewrote the state: %d -> %d", afterFirst, afterSecond)
	}

	// A real change (release) still writes.
	svc.releaseDesktopKeyboard("tenant-a", "alice", "bot_alice")
	settings.mu.Lock()
	afterRelease := settings.sets
	settings.mu.Unlock()
	if afterRelease == afterFirst {
		t.Fatal("keyboard release did not persist")
	}
}

func TestSrvRejectionMessageKeepsStatusAndDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()

	svc := NewService(&memSettings{})
	svc.HTTP = srv.Client()
	ctx := context.Background()
	if _, err := svc.SaveConnection(ctx, "tenant-a", srv.URL, "bad-token", true); err != nil {
		t.Fatalf("SaveConnection: %v", err)
	}
	_, err := svc.TestConnection(ctx, "tenant-a")
	if !errors.Is(err, ErrSrv) {
		t.Fatalf("TestConnection error = %v want ErrSrv", err)
	}
	var statusErr *upstream.StatusError
	if !errors.As(err, &statusErr) || statusErr.Status != http.StatusUnauthorized {
		t.Fatalf("StatusError = %#v want 401", statusErr)
	}
	if statusErr.Detail != "unauthorized" {
		t.Fatalf("detail = %q want unauthorized", statusErr.Detail)
	}
	msg := SrvRejectionMessage(err)
	for _, want := range []string{"HTTP 401", "unauthorized", "access token"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message %q missing %q", msg, want)
		}
	}
}

func TestSrvRejectionMessageForNotFoundAndBareErrors(t *testing.T) {
	if msg := SrvRejectionMessage(ErrSrvNotFound); !strings.Contains(msg, "check the MaClawSrv URL") {
		t.Fatalf("404 message = %q", msg)
	}
	want := "MaClawSrv rejected the instance request"
	if msg := SrvRejectionMessage(ErrSrv); msg != want {
		t.Fatalf("bare ErrSrv message = %q", msg)
	}
	if msg := SrvRejectionMessage(nil); msg != want {
		t.Fatalf("nil message = %q", msg)
	}
}
