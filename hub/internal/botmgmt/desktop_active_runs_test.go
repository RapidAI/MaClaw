package botmgmt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestActiveDesktopRunsListsRunningAndQueued(t *testing.T) {
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		Bots:   []Bot{{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"}, {ID: "bot_bob", Name: "x", InstanceID: "inst_bob", OwnerUserID: "bob"}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	started := time.Date(2026, 10, 22, 7, 0, 0, 0, time.UTC)
	svc.desktopAdmits = map[string]*desktopAdmitState{
		desktopAdmitKey("tenant-a", "alice", "bot_alice", "run_a"): {task: "打开百度", status: "running", userID: "alice", botID: "bot_alice", runID: "run_a", started: started, activity: "正在打开网页"},
		desktopAdmitKey("tenant-a", "alice", "bot_alice", ""):      {task: "下一件", userID: "alice", botID: "bot_alice", started: started.Add(time.Minute), posting: 1},
		desktopAdmitKey("tenant-a", "alice", "bot_alice", "run_d"): {task: "done", status: "succeeded", done: true, userID: "alice", botID: "bot_alice", runID: "run_d"},
	}
	items, err := svc.ActiveDesktopRuns(context.Background(), "tenant-a", "alice", "bot_alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %#v", items)
	}
	if items[0].RunID != "run_a" || items[0].Status != "running" || items[0].Step != "正在打开网页" || items[0].StartedAt == nil || !items[0].StartedAt.Equal(started) {
		t.Fatalf("running row = %#v", items[0])
	}
	if items[1].RunID != "" || items[1].Status != "queued" || items[1].Task != "下一件" {
		t.Fatalf("queued row = %#v", items[1])
	}
	if _, err := svc.ActiveDesktopRuns(context.Background(), "tenant-a", "alice", "bot_bob"); err == nil {
		t.Fatal("another owner's bot must not be listed")
	}
}

func TestActiveDesktopRunsShowsALiveServerTurn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/runs"):
			_, _ = w.Write([]byte(`{"items":[
				{"id":"run_gui","status":"running","waiting_for_user":false,"started_at":"2026-11-14T13:36:00Z","metadata":{"bot_id":"bot_4","bot_task":"打开论文"}},
				{"id":"run_weather","status":"running","waiting_for_user":false,"started_at":"2026-11-14T13:40:00Z","metadata":{"bot_id":"bot_4","bot_phase":"execute","bot_task":"北京天气PDF"}},
				{"id":"run_other","status":"running","waiting_for_user":false,"metadata":{"bot_id":"bot_other","bot_task":"别人的任务"}},
				{"id":"run_done","status":"succeeded","metadata":{"bot_task":"已经结束"}}
			]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
		Bots: []Bot{{
			ID: "bot_4", Name: "Bot 4", OwnerUserID: "alice", InstanceID: "inst_4", CreatedAt: "2026-11-14T00:00:00Z",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	svc.desktopAdmits = map[string]*desktopAdmitState{
		desktopAdmitKey("tenant-a", "alice", "bot_4", "run_gui"): {task: "", status: "running", userID: "alice", botID: "bot_4", runID: "run_gui", started: time.Date(2026, 11, 14, 13, 36, 0, 0, time.UTC)},
	}
	items, err := svc.ActiveDesktopRuns(context.Background(), "tenant-a", "alice", "bot_4")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %#v", items)
	}
	byID := map[string]ActiveDesktopRun{}
	for _, item := range items {
		byID[item.RunID] = item
		if item.Stale {
			t.Fatalf("live turn flagged stale: %#v", item)
		}
	}
	gui := byID["run_gui"]
	if gui.Source != "desktop" || gui.Status != "running" || gui.Task != "打开论文" {
		t.Fatalf("gui row = %#v", gui)
	}
	weather := byID["run_weather"]
	if weather.Source != "server" || weather.Status != "running" || weather.Task != "北京天气PDF" || weather.Phase != "execute" || weather.WaitingForUser {
		t.Fatalf("weather row = %#v", weather)
	}
}
