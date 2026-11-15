package botmgmt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestDesktopBotLimitCascadesUserDepartmentGlobalDefault(t *testing.T) {
	userLimit, deptLimit, globalLimit, defaultLimit := 1, 4, 9, 7
	grants := []Grant{
		{Scope: ScopeGlobal, DesktopBotLimit: &globalLimit},
		{Scope: ScopeDepartment, TargetID: "root", DesktopBotLimit: &deptLimit},
		{Scope: ScopeDepartment, TargetID: "lab", DesktopBotLimit: &deptLimit},
		{Scope: ScopeUser, TargetID: "alice", DesktopBotLimit: &userLimit},
	}
	if got := desktopBotLimit(grants, nil, "alice", []string{"lab", "root"}); got == nil || *got != 1 {
		t.Fatalf("user grant = %v", got)
	}
	if got := desktopBotLimit(grants, nil, "bob", []string{"lab", "root"}); got == nil || *got != 4 {
		t.Fatalf("nearest department = %v", got)
	}
	if got := desktopBotLimit(grants, nil, "cara", nil); got == nil || *got != 9 {
		t.Fatalf("global = %v", got)
	}
	// A blank limit inherits the next level instead of staying unlimited.
	inherit := []Grant{
		{Scope: ScopeUser, TargetID: "alice"},
		{Scope: ScopeDepartment, TargetID: "lab", DesktopBotLimit: &deptLimit},
		{Scope: ScopeGlobal, DesktopBotLimit: &globalLimit},
	}
	if got := desktopBotLimit(inherit, nil, "alice", []string{"lab"}); got == nil || *got != 4 {
		t.Fatalf("blank user grant inherits department, got %v", got)
	}
	if got := desktopBotLimit(inherit, nil, "dave", []string{"other"}); got == nil || *got != 9 {
		t.Fatalf("uncovered department falls to global, got %v", got)
	}
	// The tenant default is the last level in the cascade.
	if got := desktopBotLimit(nil, &defaultLimit, "erin", nil); got == nil || *got != 7 {
		t.Fatalf("default = %v", got)
	}
	if got := desktopBotLimit(nil, nil, "erin", nil); got != nil {
		t.Fatalf("every level blank stays unlimited, got %v", *got)
	}
}

func TestCountDesktopBotsSkipsNone(t *testing.T) {
	bots := []Bot{
		{OwnerUserID: "alice", DesktopMode: ""},
		{OwnerUserID: "alice", DesktopMode: desktop.BotDesktopModeIsolated},
		{OwnerUserID: "alice", DesktopMode: desktop.BotDesktopModeNone},
		{OwnerUserID: "bob", DesktopMode: ""},
	}
	if got := countDesktopBots(bots, "alice"); got != 2 {
		t.Fatalf("count = %d", got)
	}
}

type quotaPatchLog struct {
	mu   sync.Mutex
	last map[string]string
}

func (p *quotaPatchLog) record(metadata map[string]string) {
	if p == nil {
		return
	}
	copied := map[string]string{}
	for key, value := range metadata {
		copied[key] = value
	}
	p.mu.Lock()
	p.last = copied
	p.mu.Unlock()
}

func (p *quotaPatchLog) desktop() (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	value, ok := p.last["hub_bot_desktop"]
	return value, ok
}

func newDesktopQuotaService(t *testing.T, patches *quotaPatchLog) *Service {
	t.Helper()
	var seq sync.Mutex
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/me":
			_, _ = w.Write([]byte(`{"id":"svc","tenant_id":"maclaw-tenant"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/tenants/maclaw-tenant/users":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"user_alice"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/credentials"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"api_key":"key-user_alice","api_secret":"sec-user_alice"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-user_alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			_, _ = w.Write([]byte(`{"id":"inst-none","metadata":{"hub_bot":"1","hub_bot_desktop":"none"}}`))
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			var body struct {
				Metadata map[string]string `json:"metadata"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Metadata["hub_bot"] != "1" {
				http.Error(w, "metadata", http.StatusBadRequest)
				return
			}
			patches.record(body.Metadata)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/instances":
			var body struct {
				Metadata map[string]string `json:"metadata"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			seq.Lock()
			n++
			id := fmt.Sprintf("inst-%d", n)
			seq.Unlock()
			if body.Metadata["hub_bot_desktop"] == "none" {
				id += "-none"
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"` + id + `"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	svc := NewService(&memSettings{})
	svc.HTTP = srv.Client()
	ctx := context.Background()
	if _, err := svc.SaveConnection(ctx, "tenant-a", srv.URL, "secret-token", true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveAdminSecret(ctx, "tenant-a", "admin-secret"); err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestCreateBotDesktopQuota(t *testing.T) {
	patches := &quotaPatchLog{}
	svc := newDesktopQuotaService(t, patches)
	ctx := context.Background()
	limit := 1
	if _, err := svc.CreateGrant(ctx, "tenant-a", Grant{Scope: ScopeUser, TargetID: "alice", DesktopBotLimit: &limit}); err != nil {
		t.Fatal(err)
	}
	no := false
	yes := true
	first, err := svc.CreateBotForUserDesktop(ctx, "tenant-a", "alice", "with-desk", "", &yes)
	if err != nil {
		t.Fatal(err)
	}
	if first.DesktopMode != desktop.BotDesktopModeShared && first.DesktopMode != "" {
		t.Fatalf("desktop bot mode %q", first.DesktopMode)
	}
	if _, err := svc.CreateBotForUserDesktop(ctx, "tenant-a", "alice", "second-desk", "", &yes); !errors.Is(err, ErrDesktopQuota) {
		t.Fatalf("second desktop bot: %v", err)
	}
	none, err := svc.CreateBotForUserDesktop(ctx, "tenant-a", "alice", "no-desk", "", &no)
	if err != nil {
		t.Fatal(err)
	}
	if none.DesktopMode != desktop.BotDesktopModeNone {
		t.Fatalf("none mode %q", none.DesktopMode)
	}
	if _, err := svc.SetBotDesktopModeForUser(ctx, "tenant-a", "alice", none.ID, "shared"); !errors.Is(err, ErrDesktopQuota) {
		t.Fatalf("upgrade: %v", err)
	}
	if _, err := svc.SetBotDesktopMode(ctx, "tenant-a", none.ID, "shared"); err != nil {
		t.Fatalf("admin upgrade: %v", err)
	}
	if got := svc.DesktopOverWarning(ctx, "tenant-a", "alice", "shared"); got != desktopOverWarning {
		t.Fatalf("admin warning = %q", got)
	}
	if got := svc.DesktopOverWarning(ctx, "tenant-a", "alice", "none"); got != "" {
		t.Fatalf("none warning = %q", got)
	}
	quota, err := svc.DesktopQuotaForUser(ctx, "tenant-a", "alice")
	if err != nil || quota.Limit == nil || *quota.Limit != 1 || quota.Used != 2 {
		t.Fatalf("quota %+v %v", quota, err)
	}
	if _, kept := patches.desktop(); kept {
		t.Fatal("upgrade left hub_bot_desktop on the instance")
	}
	if _, err := svc.SetBotDesktopModeForUser(ctx, "tenant-a", "alice", first.ID, "none"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetBotDesktopModeForUser(ctx, "tenant-a", "alice", none.ID, "none"); err != nil {
		t.Fatal(err)
	}
	if mode, kept := patches.desktop(); !kept || mode != desktop.BotDesktopModeNone {
		t.Fatalf("downgrade metadata kept=%v mode=%q", kept, mode)
	}
	if _, err := svc.CreateBotForUserDesktop(ctx, "tenant-a", "alice", "freed-desk", "", &yes); err != nil {
		t.Fatalf("freed slot: %v", err)
	}
	if _, err := svc.CreateBotForUserDesktop(ctx, "tenant-a", "alice", "still-full", "", &yes); !errors.Is(err, ErrDesktopQuota) {
		t.Fatalf("one desktop bot still fills the grant: %v", err)
	}
}

func TestDesktopQuotaFallsToTheTenantDefault(t *testing.T) {
	svc := newDesktopQuotaService(t, nil)
	ctx := context.Background()
	limit := 1
	if _, err := svc.SaveDefaultDesktopBotLimit(ctx, "tenant-a", &limit); err != nil {
		t.Fatal(err)
	}
	// A blank user grant inherits the default instead of staying unlimited.
	if _, err := svc.CreateGrant(ctx, "tenant-a", Grant{Scope: ScopeUser, TargetID: "alice"}); err != nil {
		t.Fatal(err)
	}
	yes := true
	if _, err := svc.CreateBotForUserDesktop(ctx, "tenant-a", "alice", "desk-1", "", &yes); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateBotForUserDesktop(ctx, "tenant-a", "alice", "desk-2", "", &yes); !errors.Is(err, ErrDesktopQuota) {
		t.Fatalf("default cap: %v", err)
	}
	quota, err := svc.DesktopQuotaForUser(ctx, "tenant-a", "alice")
	if err != nil || quota.Limit == nil || *quota.Limit != 1 {
		t.Fatalf("inherited quota %+v %v", quota, err)
	}
	// Clearing the default lifts the cap again.
	if _, err := svc.SaveDefaultDesktopBotLimit(ctx, "tenant-a", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateBotForUserDesktop(ctx, "tenant-a", "alice", "desk-2", "", &yes); err != nil {
		t.Fatalf("cleared default: %v", err)
	}
}

func TestConcurrentDesktopCreatesStopAtTheGrant(t *testing.T) {
	svc := newDesktopQuotaService(t, nil)
	ctx := context.Background()
	limit := 1
	if _, err := svc.CreateGrant(ctx, "tenant-a", Grant{Scope: ScopeUser, TargetID: "alice", DesktopBotLimit: &limit}); err != nil {
		t.Fatal(err)
	}
	yes := true
	var wg sync.WaitGroup
	var mu sync.Mutex
	okN, quotaN, other := 0, 0, 0
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := svc.CreateBotForUserDesktop(ctx, "tenant-a", "alice", fmt.Sprintf("desk-%d", i), "", &yes)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				okN++
			case errors.Is(err, ErrDesktopQuota):
				quotaN++
			default:
				other++
			}
		}(i)
	}
	wg.Wait()
	if okN != 1 || quotaN != 5 || other != 0 {
		t.Fatalf("created=%d rejected=%d other=%d", okN, quotaN, other)
	}
}

func TestMarkDesktopOverFlagsOnlyDesktopBotsPastTheGrant(t *testing.T) {
	limit := 1
	bots := []Bot{
		{ID: "a", OwnerUserID: "alice", DesktopMode: desktop.BotDesktopModeShared},
		{ID: "b", OwnerUserID: "alice", DesktopMode: desktop.BotDesktopModeIsolated},
		{ID: "c", OwnerUserID: "alice", DesktopMode: desktop.BotDesktopModeNone, DesktopOver: true},
		{ID: "d", OwnerUserID: "bob"},
	}
	grants := []Grant{{Scope: ScopeUser, TargetID: "alice", DesktopBotLimit: &limit}}
	got := (&Service{}).MarkDesktopOver(context.Background(), "tenant-a", bots, grants, nil)
	if !got[0].DesktopOver || !got[1].DesktopOver {
		t.Fatalf("desktop bots past the grant: %+v", got[:2])
	}
	if got[2].DesktopOver || got[3].DesktopOver {
		t.Fatalf("none and unlimited owners stay clear: %+v", got[2:])
	}
	if !bots[2].DesktopOver {
		t.Fatal("marking the response must not clear the caller's bot")
	}
	if bots[0].DesktopOver {
		t.Fatal("the stored record must stay unmarked")
	}
}

func TestNormalizeDesktopModeNone(t *testing.T) {
	got, err := NormalizeDesktopMode(" none ")
	if err != nil || got != desktop.BotDesktopModeNone {
		t.Fatalf("%q %v", got, err)
	}
}
