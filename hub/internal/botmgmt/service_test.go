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

	"github.com/RapidAI/CodeClaw/hub/internal/security"
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
	mu     sync.Mutex
	open   int
	stop   int
	url    string
	openFn func(context.Context) (string, error)
}

func writeInstanceSettings(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/instances/"), "/")
	_, _ = w.Write([]byte(`{"id":"` + id + `","metadata":{"llm_service_group_id":"group-1"}}`))
}

func (d *desktopCounter) Open(ctx context.Context, _, _ string) (string, error) {
	d.mu.Lock()
	d.open++
	fn := d.openFn
	url := d.url
	d.mu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	return url, nil
}
func (d *desktopCounter) Stop(context.Context, string, string) error {
	d.mu.Lock()
	d.stop++
	d.mu.Unlock()
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

func TestFilterGrantedIDsFollowsBotScope(t *testing.T) {
	svc := NewService(&memSettings{})
	ctx := context.Background()
	ids := []string{"alice", " bob ", "carol", "sam", " ", "alice"}
	if got := svc.FilterGrantedIDs(ctx, "tenant-a", ids); len(got) != 0 {
		t.Fatalf("off by default: %#v", got)
	}
	if _, err := svc.CreateGrant(ctx, "tenant-a", Grant{Scope: ScopeUser, TargetID: "bob"}); err != nil {
		t.Fatal(err)
	}
	if got := svc.FilterGrantedIDs(ctx, "tenant-a", ids); len(got) != 1 || got[0] != "bob" {
		t.Fatalf("user grant: %#v", got)
	}
	svc.Directory = chainDir{
		email:   map[string]string{"alice": "alice@example.com", "carol": "carol@example.com", "sam": "sam@example.com"},
		group:   map[string]string{"alice@example.com": "eng", "carol@example.com": "eng-child", "sam@example.com": "sales"},
		parents: map[string]string{"eng-child": "eng"},
	}
	if _, err := svc.CreateGrant(ctx, "tenant-a", Grant{Scope: ScopeDepartment, TargetID: "eng"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(svc.FilterGrantedIDs(ctx, "tenant-a", ids), ","); got != "alice,bob,carol" {
		t.Fatalf("department chain: %s", got)
	}
	if _, err := svc.CreateGrant(ctx, "tenant-a", Grant{Scope: ScopeGlobal}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(svc.FilterGrantedIDs(ctx, "tenant-a", []string{"zoe", "alice"}), ","); got != "zoe,alice" {
		t.Fatalf("global: %s", got)
	}
	if got := (*Service)(nil).FilterGrantedIDs(ctx, "tenant-a", ids); got != nil {
		t.Fatalf("nil service: %#v", got)
	}
}

func TestFilterGrantedAgreesWithEnabled(t *testing.T) {
	svc := NewService(&memSettings{})
	ctx := context.Background()
	svc.Directory = chainDir{
		email:   map[string]string{"alice": "alice@example.com", "carol": "carol@example.com", "sam": "sam@example.com"},
		group:   map[string]string{"alice@example.com": "eng", "carol@example.com": "eng-child", "sam@example.com": "sales"},
		parents: map[string]string{"eng-child": "eng"},
	}
	for _, grant := range []Grant{
		{Scope: ScopeUser, TargetID: "bob"},
		{Scope: ScopeDepartment, TargetID: "eng"},
	} {
		if _, err := svc.CreateGrant(ctx, "tenant-a", grant); err != nil {
			t.Fatal(err)
		}
	}
	ids := []string{"alice", "bob", "carol", "sam"}
	listed := map[string]bool{}
	for _, id := range svc.FilterGrantedIDs(ctx, "tenant-a", ids) {
		listed[id] = true
	}
	for _, id := range ids {
		on, err := svc.Enabled(ctx, "tenant-a", id)
		if err != nil {
			t.Fatal(err)
		}
		if on != listed[id] {
			t.Fatalf("%s enabled=%v listed=%v", id, on, listed[id])
		}
	}
}

func TestFilterGrantedDepartmentCycleStillCoversBothSides(t *testing.T) {
	svc := NewService(&memSettings{})
	ctx := context.Background()
	svc.Directory = chainDir{
		email:   map[string]string{"ann": "ann@example.com", "ben": "ben@example.com"},
		group:   map[string]string{"ann@example.com": "alpha", "ben@example.com": "beta"},
		parents: map[string]string{"alpha": "beta", "beta": "alpha"},
	}
	if _, err := svc.CreateGrant(ctx, "tenant-a", Grant{Scope: ScopeDepartment, TargetID: "alpha"}); err != nil {
		t.Fatal(err)
	}
	// ann is visited first. Her walk stops when beta points back at alpha.
	// ben still has to see alpha, or the grant would depend on list order.
	got := svc.FilterGranted(ctx, "tenant-a", []GrantSubject{
		{ID: "ann", Email: "ann@example.com"},
		{ID: "ben", Email: "ben@example.com"},
	})
	if strings.Join(got, ",") != "ann,ben" {
		t.Fatalf("cycle grant=%v", got)
	}
}

func TestFilterGrantedReusesDepartmentChain(t *testing.T) {
	svc := NewService(&memSettings{})
	ctx := context.Background()
	dir := &countingDir{chainDir: chainDir{
		email:   map[string]string{"carol": "carol@example.com", "dave": "dave@example.com"},
		group:   map[string]string{"carol@example.com": "eng-child", "dave@example.com": "eng-child"},
		parents: map[string]string{"eng-child": "eng"},
	}}
	svc.Directory = dir
	if _, err := svc.CreateGrant(ctx, "tenant-a", Grant{Scope: ScopeDepartment, TargetID: "eng"}); err != nil {
		t.Fatal(err)
	}
	got := svc.FilterGranted(ctx, "tenant-a", []GrantSubject{
		{ID: "carol", Email: "carol@example.com"},
		{ID: "dave", Email: "dave@example.com"},
	})
	if strings.Join(got, ",") != "carol,dave" {
		t.Fatalf("granted=%v", got)
	}
	if dir.emailCalls != 0 {
		t.Fatalf("email lookups=%d, known addresses should skip them", dir.emailCalls)
	}
	if dir.groupCalls != 2 {
		t.Fatalf("group lookups=%d, want one per user", dir.groupCalls)
	}
	if dir.parentCalls != 2 {
		t.Fatalf("parent lookups=%d, want the shared chain once", dir.parentCalls)
	}
}

type countingDir struct {
	chainDir
	emailCalls  int
	groupCalls  int
	parentCalls int
}

func (d *countingDir) Email(ctx context.Context, userID string) (string, error) {
	d.emailCalls++
	return d.chainDir.Email(ctx, userID)
}

func (d *countingDir) GroupID(ctx context.Context, email string) (string, error) {
	d.groupCalls++
	return d.chainDir.GroupID(ctx, email)
}

func (d *countingDir) ParentID(ctx context.Context, groupID string) (string, error) {
	d.parentCalls++
	return d.chainDir.ParentID(ctx, groupID)
}

// TestDepartmentGrantUsesSettingsTenant checks that a department grant is
// resolved in the tenant that owns the grant. The directory answers only
// for that tenant, the same way the security store filters group rows.
func TestDepartmentGrantUsesSettingsTenant(t *testing.T) {
	svc := NewService(&memSettings{})
	ctx := context.Background()
	dir := &tenantDir{
		want:   "tenant-a",
		groups: map[string]string{"carol@example.com": "eng", "sam@example.com": "sales"},
	}
	svc.Directory = dir
	if _, err := svc.CreateGrant(ctx, "tenant-a", Grant{Scope: ScopeUser, TargetID: "bob"}); err != nil {
		t.Fatal(err)
	}
	on, err := svc.Enabled(ctx, "tenant-a", "bob")
	if err != nil || !on {
		t.Fatalf("user grant enabled=%v err=%v", on, err)
	}
	if dir.calls != 0 {
		t.Fatalf("user grant directory calls=%d, want none", dir.calls)
	}
	if _, err := svc.CreateGrant(ctx, "tenant-a", Grant{Scope: ScopeDepartment, TargetID: "eng"}); err != nil {
		t.Fatal(err)
	}
	got := svc.FilterGranted(ctx, "tenant-a", []GrantSubject{
		{ID: "carol", Email: "carol@example.com"},
		{ID: "sam", Email: "sam@example.com"},
	})
	if strings.Join(got, ",") != "carol" {
		t.Fatalf("granted=%v, want carol", got)
	}
	on, err = svc.Enabled(ctx, "tenant-a", "carol")
	if err != nil || !on {
		t.Fatalf("carol enabled=%v err=%v", on, err)
	}
	on, err = svc.Enabled(ctx, "tenant-a", "sam")
	if err != nil || on {
		t.Fatalf("sam enabled=%v err=%v", on, err)
	}
}

// tenantDir answers with a department only when the context carries want.
// Any other tenant, including the unset default, looks like a user with no group.
type tenantDir struct {
	want   string
	groups map[string]string
	calls  int
}

func (d *tenantDir) Email(ctx context.Context, userID string) (string, error) {
	d.calls++
	if security.TenantIDFromContext(ctx) != d.want {
		return "", nil
	}
	return userID + "@example.com", nil
}

func (d *tenantDir) GroupID(ctx context.Context, email string) (string, error) {
	d.calls++
	if security.TenantIDFromContext(ctx) != d.want {
		return "", nil
	}
	return d.groups[email], nil
}

func (d *tenantDir) ParentID(ctx context.Context, groupID string) (string, error) {
	d.calls++
	if security.TenantIDFromContext(ctx) != d.want || groupID == "" {
		return "", nil
	}
	return "", nil
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
	if !strings.Contains(desktopBind, `"hub_user_id":"alice"`) || !strings.Contains(desktopBind, `"hub_tenant_id":"tenant-a"`) || !strings.Contains(desktopBind, `"llm_service_group_id":"system-free"`) || strings.Contains(desktopBind, "someone-else") || strings.Contains(desktopBind, "group-1") {
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

func botViewRecordJSON(t *testing.T) string {
	t.Helper()
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
	return string(raw)
}

func TestHoldDesktopViewOpensAndKeepsTheDesktopUp(t *testing.T) {
	settings := &memSettings{}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), botViewRecordJSON(t)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc.Desktop = desk
	ctx := context.Background()

	novnc, keyboard, err := svc.HoldDesktopView(ctx, "tenant-a", "alice", "bot_alice")
	if err != nil {
		t.Fatal(err)
	}
	if desk.open != 1 {
		t.Fatalf("open=%d, want the hold to start the desktop", desk.open)
	}
	if !strings.HasPrefix(novnc, "/api/v1/desktop-handoff/") {
		t.Fatalf("novnc=%q", novnc)
	}
	if keyboard {
		t.Fatal("keyboard must not go to the person while nobody logged in")
	}
	// Polling again must not open the desktop a second time.
	if _, _, err := svc.HoldDesktopView(ctx, "tenant-a", "alice", "bot_alice"); err != nil || desk.open != 1 {
		t.Fatalf("second hold opened=%d err=%v", desk.open, err)
	}
	// A run finishing now must leave the desktop up: the human is watching.
	stopped, err := svc.StopDesktopIfIdle(ctx, "tenant-a", "alice", "inst_alice")
	if err != nil {
		t.Fatal(err)
	}
	if stopped || desk.stop != 0 {
		t.Fatal("stop pulled the desktop out from under the open view")
	}
	// The hold expires on its own; the next idle stop goes through.
	svc.Now = func() time.Time { return time.Now().Add(UserDesktopViewHold + time.Minute) }
	stopped, err = svc.StopDesktopIfIdle(ctx, "tenant-a", "alice", "inst_alice")
	if err != nil {
		t.Fatal(err)
	}
	if !stopped || desk.stop != 1 {
		t.Fatalf("stopped=%v stop=%d, the expired hold must not pin the desktop", stopped, desk.stop)
	}
}

func TestHoldDesktopViewFinishesAfterThePollGivesUp(t *testing.T) {
	settings := &memSettings{}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), botViewRecordJSON(t)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	desk.openFn = func(ctx context.Context) (string, error) {
		cancel()
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return desk.url, nil
	}
	svc.Desktop = desk

	novnc, _, err := svc.HoldDesktopView(parent, "tenant-a", "alice", "bot_alice")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(novnc, "/api/v1/desktop-handoff/") {
		t.Fatalf("novnc=%q", novnc)
	}
	// The poll that gave up already recorded the watch, so a run finishing
	// now must not stop the desktop the person is still looking at.
	stopped, err := svc.StopDesktopIfIdle(context.Background(), "tenant-a", "alice", "inst_alice")
	if err != nil {
		t.Fatal(err)
	}
	if stopped || desk.stop != 0 {
		t.Fatal("a poll that gave up let the idle stop take the desktop")
	}
}

func TestHoldDesktopViewOpensOnceWhenPollsOverlap(t *testing.T) {
	settings := &memSettings{}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), botViewRecordJSON(t)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	desk.openFn = func(ctx context.Context) (string, error) {
		once.Do(func() { close(entered) })
		<-release
		return desk.url, nil
	}
	svc.Desktop = desk

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_, _, err := svc.HoldDesktopView(context.Background(), "tenant-a", "alice", "bot_alice")
			errs <- err
		}()
	}
	<-entered
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	desk.mu.Lock()
	opened := desk.open
	desk.mu.Unlock()
	if opened != 1 {
		t.Fatalf("open=%d, overlapping polls started more than one desktop", opened)
	}
}

func TestHoldDesktopViewDoesNotStartACanceledPoll(t *testing.T) {
	settings := &memSettings{}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), botViewRecordJSON(t)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	desk.openFn = func(context.Context) (string, error) {
		t.Fatal("canceled poll started a desktop")
		return "", nil
	}
	svc.Desktop = desk
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, _, err := svc.HoldDesktopView(ctx, "tenant-a", "alice", "bot_alice"); err == nil {
		t.Fatal("canceled poll returned a picture")
	}
	stopped, err := svc.StopDesktopIfIdle(context.Background(), "tenant-a", "alice", "inst_alice")
	if err != nil {
		t.Fatal(err)
	}
	if !stopped || desk.stop != 1 || desk.open != 0 {
		t.Fatalf("stopped=%v stop=%d open=%d, canceled poll pinned the desktop", stopped, desk.stop, desk.open)
	}
}

func TestHoldDesktopViewStopsWhenThePanelClosesDuringOpen(t *testing.T) {
	settings := &memSettings{}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), botViewRecordJSON(t)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	entered := make(chan struct{})
	var once sync.Once
	var stoppedDuringOpen bool
	desk.openFn = func(context.Context) (string, error) {
		once.Do(func() { close(entered) })
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if !svc.userDesktopViewActive("tenant-a", "alice") {
				desk.mu.Lock()
				stoppedDuringOpen = desk.stop != 0
				desk.mu.Unlock()
				return desk.url, nil
			}
			time.Sleep(time.Millisecond)
		}
		return "", fmt.Errorf("panel close did not drop the watch")
	}
	svc.Desktop = desk

	// Release takes the same gate as open, so it has to run beside open.
	// Calling it on this goroutine would wait for a gate this goroutine holds.
	releaseErr := make(chan error, 1)
	go func() {
		<-entered
		releaseErr <- svc.ReleaseUserDesktopView(context.Background(), "tenant-a", "alice", "bot_alice")
	}()

	novnc, _, err := svc.HoldDesktopView(context.Background(), "tenant-a", "alice", "bot_alice")
	relErr := <-releaseErr
	if err != nil {
		t.Fatal(err)
	}
	if relErr != nil {
		t.Fatal(relErr)
	}
	if novnc != "" {
		t.Fatalf("novnc=%q, closed panel still received a picture", novnc)
	}
	if stoppedDuringOpen {
		t.Fatal("release stopped the desktop while open still held it")
	}
	desk.mu.Lock()
	stopped := desk.stop
	desk.mu.Unlock()
	if stopped == 0 {
		t.Fatal("desktop stayed up after the panel closed during open")
	}
	if svc.desktopViewURL("tenant-a", "alice") != "" {
		t.Fatal("closed panel left a desktop picture published")
	}
}

func TestHoldDesktopViewClosedEpochDoesNotPin(t *testing.T) {
	settings := &memSettings{}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), botViewRecordJSON(t)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc.Desktop = desk

	opened, _, err := svc.HoldDesktopView(WithDesktopWatchEpoch(context.Background(), 1), "tenant-a", "alice", "bot_alice")
	if err != nil || !strings.HasPrefix(opened, "/api/v1/desktop-handoff/") {
		t.Fatalf("open novnc=%q err=%v", opened, err)
	}
	if err := svc.ReleaseUserDesktopView(WithDesktopWatchEpoch(context.Background(), 1), "tenant-a", "alice", "bot_alice"); err != nil {
		t.Fatal(err)
	}
	// The poll that belonged to the closed page must not start it again.
	again, _, err := svc.HoldDesktopView(WithDesktopWatchEpoch(context.Background(), 1), "tenant-a", "alice", "bot_alice")
	if err != nil {
		t.Fatal(err)
	}
	if again != "" || desk.open != 1 {
		t.Fatalf("again=%q open=%d, closed generation pinned the desktop", again, desk.open)
	}
	// The page that opened next is a new generation and may start it.
	reopened, _, err := svc.HoldDesktopView(WithDesktopWatchEpoch(context.Background(), 2), "tenant-a", "alice", "bot_alice")
	if err != nil || !strings.HasPrefix(reopened, "/api/v1/desktop-handoff/") || desk.open != 2 {
		t.Fatalf("reopen novnc=%q open=%d err=%v", reopened, desk.open, err)
	}
}

func TestHoldDesktopViewOldReleaseDoesNotStopANewerWatch(t *testing.T) {
	settings := &memSettings{}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), botViewRecordJSON(t)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	entered := make(chan struct{})
	released := make(chan struct{})
	var once sync.Once
	desk.openFn = func(context.Context) (string, error) {
		once.Do(func() { close(entered) })
		<-released
		return desk.url, nil
	}
	svc.Desktop = desk

	newer := make(chan string, 1)
	newerErr := make(chan error, 1)
	go func() {
		<-entered
		novnc, _, err := svc.HoldDesktopView(WithDesktopWatchEpoch(context.Background(), 2), "tenant-a", "alice", "bot_alice")
		newer <- novnc
		newerErr <- err
	}()
	releaseErr := make(chan error, 1)
	go func() {
		<-entered
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			svc.mu.Lock()
			gen := svc.desktopWatchGen[desktopViewKey("tenant-a", "alice")]
			svc.mu.Unlock()
			if gen >= 2 {
				releaseErr <- svc.ReleaseUserDesktopView(WithDesktopWatchEpoch(context.Background(), 1), "tenant-a", "alice", "bot_alice")
				close(released)
				return
			}
			time.Sleep(time.Millisecond)
		}
		releaseErr <- fmt.Errorf("newer watch did not note")
		close(released)
	}()

	first, _, err := svc.HoldDesktopView(WithDesktopWatchEpoch(context.Background(), 1), "tenant-a", "alice", "bot_alice")
	relErr := <-releaseErr
	secondErr := <-newerErr
	second := <-newer
	if err != nil {
		t.Fatal(err)
	}
	if relErr != nil {
		t.Fatal(relErr)
	}
	if secondErr != nil {
		t.Fatal(secondErr)
	}
	if !strings.HasPrefix(first, "/api/v1/desktop-handoff/") || !strings.HasPrefix(second, "/api/v1/desktop-handoff/") {
		t.Fatalf("first=%q second=%q", first, second)
	}
	if desk.open != 1 || desk.stop != 0 {
		t.Fatalf("open=%d stop=%d, old release restarted or stopped the new watch", desk.open, desk.stop)
	}
}

func TestHoldDesktopViewNewerWatchDuringOpenIsNotStopped(t *testing.T) {
	settings := &memSettings{}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), botViewRecordJSON(t)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	entered := make(chan struct{})
	var once sync.Once
	desk.openFn = func(context.Context) (string, error) {
		once.Do(func() { close(entered) })
		deadline := time.Now().Add(2 * time.Second)
		sawClear := false
		for time.Now().Before(deadline) {
			active := svc.userDesktopViewActive("tenant-a", "alice")
			if !active {
				sawClear = true
			}
			if sawClear && active {
				return desk.url, nil
			}
			time.Sleep(time.Millisecond)
		}
		return "", fmt.Errorf("reopen did not note the watch")
	}
	svc.Desktop = desk

	releaseErr := make(chan error, 1)
	go func() {
		<-entered
		releaseErr <- svc.ReleaseUserDesktopView(WithDesktopWatchEpoch(context.Background(), 1), "tenant-a", "alice", "bot_alice")
	}()
	type holdResult struct {
		novnc string
		err   error
	}
	newer := make(chan holdResult, 1)
	go func() {
		<-entered
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if !svc.userDesktopViewActive("tenant-a", "alice") {
				break
			}
			time.Sleep(time.Millisecond)
		}
		novnc, _, err := svc.HoldDesktopView(WithDesktopWatchEpoch(context.Background(), 2), "tenant-a", "alice", "bot_alice")
		newer <- holdResult{novnc, err}
	}()

	first, _, err := svc.HoldDesktopView(WithDesktopWatchEpoch(context.Background(), 1), "tenant-a", "alice", "bot_alice")
	relErr := <-releaseErr
	second := <-newer
	if err != nil {
		t.Fatal(err)
	}
	if relErr != nil {
		t.Fatal(relErr)
	}
	if second.err != nil {
		t.Fatal(second.err)
	}
	if !strings.HasPrefix(first, "/api/v1/desktop-handoff/") || !strings.HasPrefix(second.novnc, "/api/v1/desktop-handoff/") {
		t.Fatalf("first=%q second=%q", first, second.novnc)
	}
	desk.mu.Lock()
	opened, stopped := desk.open, desk.stop
	desk.mu.Unlock()
	if opened != 1 || stopped != 0 {
		t.Fatalf("open=%d stop=%d, release during reopen stopped the desktop", opened, stopped)
	}
}

func TestHoldDesktopViewKeepsAHandoffWhenThePanelClosesDuringOpen(t *testing.T) {
	settings := &memSettings{}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), botViewRecordJSON(t)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	desk.openFn = func(context.Context) (string, error) {
		svc.mu.Lock()
		if svc.desktopHeld == nil {
			svc.desktopHeld = map[string]string{}
		}
		svc.desktopHeld[desktopViewKey("tenant-a", "alice")] = "bot_alice"
		delete(svc.desktopUserView, desktopViewKey("tenant-a", "alice"))
		svc.mu.Unlock()
		return desk.url, nil
	}
	svc.Desktop = desk

	novnc, _, err := svc.HoldDesktopView(context.Background(), "tenant-a", "alice", "bot_alice")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(novnc, "/api/v1/desktop-handoff/") {
		t.Fatalf("novnc=%q", novnc)
	}
	if desk.stop != 0 {
		t.Fatal("closing the watch stopped a desktop the bot had handed over")
	}
}

func TestHoldDesktopViewRefusesOtherPeopleAndDisabledUsers(t *testing.T) {
	settings := &memSettings{}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), botViewRecordJSON(t)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.Desktop = &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}

	// A user whose grant lets them in still cannot point the hold at
	// someone else's bot.
	svc.CreateGrant(context.Background(), "tenant-a", Grant{Scope: ScopeUser, TargetID: "bob"})
	if _, _, err := svc.HoldDesktopView(context.Background(), "tenant-a", "bob", "bot_alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign bot err=%v", err)
	}
	if desk, ok := svc.Desktop.(*desktopCounter); !ok || desk.open != 0 {
		t.Fatal("foreign hold opened the desktop")
	}
	// A user with no grant stays disabled and must not reach the desktop.
	if _, _, err := svc.HoldDesktopView(context.Background(), "tenant-b", "alice", "bot_alice"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled tenant err=%v", err)
	}
}

func TestUserViewHoldSurvivesRestartButExpires(t *testing.T) {
	settings := &memSettings{}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), botViewRecordJSON(t)); err != nil {
		t.Fatal(err)
	}
	svc1 := NewService(settings)
	svc1.Desktop = &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc1.Now = func() time.Time { return time.Now() }
	if _, _, err := svc1.HoldDesktopView(context.Background(), "tenant-a", "alice", "bot_alice"); err != nil {
		t.Fatal(err)
	}

	// Hub restarts. The fresh process restores the fresh hold.
	svc2 := NewService(settings)
	desk2 := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc2.Desktop = desk2
	stopped, err := svc2.StopDesktopIfIdle(context.Background(), "tenant-a", "alice", "inst_alice")
	if err != nil {
		t.Fatal(err)
	}
	if stopped || desk2.stop != 0 {
		t.Fatal("restart forgot the user view hold")
	}

	// A persisted hold that was old when the process came up must not pin
	// the desktop; the view expired long before the restart.
	old := NewService(settings)
	old.Desktop = &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	old.Now = func() time.Time { return time.Now().Add(time.Hour) }
	stopped, err = old.StopDesktopIfIdle(context.Background(), "tenant-a", "alice", "inst_alice")
	if err != nil {
		t.Fatal(err)
	}
	if !stopped {
		t.Fatal("a stale restored hold still pins the desktop")
	}
}
