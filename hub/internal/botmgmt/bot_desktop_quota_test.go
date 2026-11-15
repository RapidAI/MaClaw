package botmgmt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestDesktopBotLimitPicksTheMostSpecificGrant(t *testing.T) {
	userLimit, deptLimit, globalLimit := 1, 4, 9
	grants := []Grant{
		{Scope: ScopeGlobal, DesktopBotLimit: &globalLimit},
		{Scope: ScopeDepartment, TargetID: "root", DesktopBotLimit: &deptLimit},
		{Scope: ScopeDepartment, TargetID: "lab", DesktopBotLimit: &deptLimit},
		{Scope: ScopeUser, TargetID: "alice", DesktopBotLimit: &userLimit},
	}
	if got := desktopBotLimit(grants, "alice", []string{"lab", "root"}); got == nil || *got != 1 {
		t.Fatalf("user grant = %v", got)
	}
	if got := desktopBotLimit(grants, "bob", []string{"lab", "root"}); got == nil || *got != 4 {
		t.Fatalf("nearest department = %v", got)
	}
	if got := desktopBotLimit(grants, "cara", nil); got == nil || *got != 9 {
		t.Fatalf("global = %v", got)
	}
	if got := desktopBotLimit([]Grant{{Scope: ScopeUser, TargetID: "alice"}}, "alice", nil); got != nil {
		t.Fatalf("nil limit must stay unlimited, got %v", *got)
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

func TestCreateBotDesktopQuota(t *testing.T) {
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
			if _, stillNone := body.Metadata["hub_bot_desktop"]; stillNone || body.Metadata["hub_bot"] != "1" {
				http.Error(w, "metadata", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/instances":
			var body struct {
				Metadata map[string]string `json:"metadata"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			id := "inst-" + body.Metadata["hub_user_id"]
			if body.Metadata["hub_bot_desktop"] == "none" {
				id += "-none"
			}
			id += fmt.Sprintf("-%d", len(body.Metadata))
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"` + id + `"}`))
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
	if _, err := svc.SaveAdminSecret(ctx, "tenant-a", "admin-secret"); err != nil {
		t.Fatal(err)
	}
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
	quota, err := svc.DesktopQuotaForUser(ctx, "tenant-a", "alice")
	if err != nil || quota.Limit == nil || *quota.Limit != 1 || quota.Used != 2 {
		t.Fatalf("quota %+v %v", quota, err)
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
	got := (&Service{}).MarkDesktopOver(context.Background(), "tenant-a", bots, grants)
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
