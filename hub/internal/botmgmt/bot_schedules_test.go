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

func TestListBotSchedulesReadsTheHostClock(t *testing.T) {
	var seenPath, seenBot, seenDelete string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/bot-schedules"):
			seenPath = r.URL.Path
			seenBot = r.URL.Query().Get("bot_id")
			_, _ = w.Write([]byte(`{"items":[{"id":"t1","name":"天气","action":"报告天气","interval_minutes":5,"day_of_week":-1,"status":"active"}]}`))
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/bot-schedules/"):
			seenDelete = r.URL.Path
			if r.URL.Query().Get("bot_id") != "bot_4" {
				t.Errorf("delete bot_id = %q", r.URL.Query().Get("bot_id"))
			}
			_, _ = w.Write([]byte(`{"deleted":1}`))
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

	items, err := svc.ListBotSchedules(context.Background(), "tenant-a", "alice", "bot_4")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "t1" || items[0].Name != "天气" || items[0].IntervalMinutes != 5 {
		t.Fatalf("items = %+v", items)
	}
	if !strings.Contains(seenPath, "/instances/inst_4/bot-schedules") || seenBot != "bot_4" {
		t.Fatalf("path=%s bot=%s", seenPath, seenBot)
	}
	if _, err := svc.ListBotSchedules(context.Background(), "tenant-a", "alice", "bot_gone"); err == nil {
		t.Fatal("missing bot listed")
	}
	if err := svc.DeleteBotSchedule(context.Background(), "tenant-a", "alice", "bot_4", "t1"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(seenDelete, "/bot-schedules/t1") {
		t.Fatalf("delete path = %s", seenDelete)
	}
}

func TestFailedScheduleSweepRetriesWithinAMinute(t *testing.T) {
	retains := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/bot-schedules"):
			_, _ = w.Write([]byte(`{"items":[]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/bot-schedules/retain"):
			retains++
			w.WriteHeader(http.StatusBadGateway)
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
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	svc.Now = func() time.Time { return now }
	if _, err := svc.ListBotSchedules(context.Background(), "tenant-a", "alice", "bot_4"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListBotSchedules(context.Background(), "tenant-a", "alice", "bot_4"); err != nil {
		t.Fatal(err)
	}
	if retains != 1 {
		t.Fatalf("immediate retry retains=%d", retains)
	}
	now = now.Add(2 * time.Minute)
	if _, err := svc.ListBotSchedules(context.Background(), "tenant-a", "alice", "bot_4"); err != nil {
		t.Fatal(err)
	}
	if retains != 2 {
		t.Fatalf("after a minute retains=%d", retains)
	}
}
