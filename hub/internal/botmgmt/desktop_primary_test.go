package botmgmt

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func primaryFixture(t *testing.T, bots []Bot) *Service {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusNotFound)
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
		Bots:   bots,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	return svc
}

func TestPrimaryBotIsEarliestCreatedUntilSet(t *testing.T) {
	svc := primaryFixture(t, []Bot{
		{ID: "bot_late", Name: "晚", OwnerUserID: "alice", InstanceID: "inst_late", CreatedAt: "2026-01-01T18:00:00Z"},
		{ID: "bot_early", Name: "早", OwnerUserID: "alice", InstanceID: "inst_early", CreatedAt: "2026-01-02T00:00:00+08:00"},
		{ID: "bot_bad", Name: "坏", OwnerUserID: "alice", InstanceID: "inst_bad", CreatedAt: "not-a-time"},
		{ID: "bot_bob", Name: "别人", OwnerUserID: "bob", InstanceID: "inst_bob", CreatedAt: "2020-01-01T00:00:00Z"},
	})
	items, err := svc.ListBotsForUser(context.Background(), "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("items = %#v", items)
	}
	primary := 0
	var marked string
	for _, item := range items {
		if item.Primary {
			primary++
			marked = item.ID
		}
	}
	if primary != 1 || marked != "bot_early" {
		t.Fatalf("primary count=%d id=%s items=%#v", primary, marked, items)
	}
	raw, err := svc.System.Get(context.Background(), storageKey("tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, `"primary"`) {
		t.Fatalf("primary was written onto the bot row: %s", raw)
	}

	if err := svc.SetPrimaryBot(context.Background(), "tenant-a", "alice", "bot_late"); err != nil {
		t.Fatal(err)
	}
	items, err = svc.ListBotsForUser(context.Background(), "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	primary = 0
	marked = ""
	for _, item := range items {
		if item.Primary {
			primary++
			marked = item.ID
		}
	}
	if primary != 1 || marked != "bot_late" {
		t.Fatalf("after set primary count=%d id=%s", primary, marked)
	}
	raw, err = svc.System.Get(context.Background(), storageKey("tenant-a"))
	if err != nil || !strings.Contains(raw, `"primary_bot":{"alice":"bot_late"}`) {
		t.Fatalf("stored primary = %s err=%v", raw, err)
	}
}

func TestDeletePrimaryClearsTheSlotAndCreateDoesNotWriteIt(t *testing.T) {
	svc := primaryFixture(t, []Bot{
		{ID: "bot_old", Name: "旧", OwnerUserID: "alice", InstanceID: "inst_old", CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "bot_new", Name: "新", OwnerUserID: "alice", InstanceID: "inst_new", CreatedAt: "2026-02-01T00:00:00Z"},
	})
	if err := svc.SetPrimaryBot(context.Background(), "tenant-a", "alice", "bot_new"); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteBot(context.Background(), "tenant-a", "bot_new"); err != nil {
		t.Fatal(err)
	}
	raw, err := svc.System.Get(context.Background(), storageKey("tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "bot_new") {
		t.Fatalf("deleted bot still stored: %s", raw)
	}
	var rec record
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.PrimaryBot["alice"] != "" {
		t.Fatalf("primary slot = %#v", rec.PrimaryBot)
	}
	items, err := svc.ListBotsForUser(context.Background(), "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !items[0].Primary || items[0].ID != "bot_old" {
		t.Fatalf("remaining primary = %#v", items)
	}
}

func TestNonPrimaryCrossTalkIsRejectedAndAResultKeepsTheStoredSender(t *testing.T) {
	svc := primaryFixture(t, []Bot{
		{ID: "bot_main", Name: "主", OwnerUserID: "alice", InstanceID: "inst_main", CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "bot_other", Name: "次", OwnerUserID: "alice", InstanceID: "inst_other", CreatedAt: "2026-02-01T00:00:00Z"},
	})
	if _, err := svc.PostBotCrossTalk(context.Background(), "tenant-a", "alice", "bot_other", BotCrossNote{
		Kind: "notify", Target: "bot_main", Text: "看一下",
	}); !errors.Is(err, ErrNotPrimary) {
		t.Fatalf("non-primary err = %v", err)
	}
	note, err := svc.PostBotCrossTalk(context.Background(), "tenant-a", "alice", "bot_main", BotCrossNote{
		Kind: "delegate", Target: "bot_other", Text: "装这个", Order: "在这台云桌面里装这个软件",
	})
	if err != nil {
		t.Fatal(err)
	}
	if note.Kind != "delegate" || note.From != "bot_main" || note.RunID != "" {
		t.Fatalf("note = %#v", note)
	}
	first, claimed, err := svc.ClaimBotCrossStart(context.Background(), "tenant-a", "alice", "bot_other", note.ID)
	if err != nil || !claimed || first.StartedAt == "" {
		t.Fatalf("first claim claimed=%v note=%#v err=%v", claimed, first, err)
	}
	second, claimed, err := svc.ClaimBotCrossStart(context.Background(), "tenant-a", "alice", "bot_other", note.ID)
	if err != nil || claimed || second.StartedAt != first.StartedAt {
		t.Fatalf("second claim claimed=%v started=%q first=%q err=%v", claimed, second.StartedAt, first.StartedAt, err)
	}
	if err := svc.SetPrimaryBot(context.Background(), "tenant-a", "alice", "bot_other"); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteBot(context.Background(), "tenant-a", "bot_main"); err != nil {
		t.Fatal(err)
	}
	result, err := svc.PostBotCrossTalk(context.Background(), "tenant-a", "alice", "bot_other", BotCrossNote{
		Kind: "result", ReplyTo: note.ID, Text: "装好了。",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != "result" || result.From != "bot_main" || result.Target != "bot_other" || result.ReplyTo != note.ID {
		t.Fatalf("result = %#v", result)
	}
}

func TestStaleCrossClaimCanBeTakenAgain(t *testing.T) {
	svc := primaryFixture(t, []Bot{
		{ID: "bot_main", Name: "主", OwnerUserID: "alice", InstanceID: "inst_main", CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "bot_other", Name: "次", OwnerUserID: "alice", InstanceID: "inst_other", CreatedAt: "2026-02-01T00:00:00Z"},
	})
	note, err := svc.PostBotCrossTalk(context.Background(), "tenant-a", "alice", "bot_main", BotCrossNote{
		Kind: "delegate", Target: "bot_other", Text: "打开页面", Order: "打开示例网站",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := svc.ClaimBotCrossStart(context.Background(), "tenant-a", "alice", "bot_other", note.ID); err != nil || !claimed {
		t.Fatalf("claim claimed=%v err=%v", claimed, err)
	}
	svc.Now = func() time.Time { return time.Now().Add(31 * time.Second) }
	again, claimed, err := svc.ClaimBotCrossStart(context.Background(), "tenant-a", "alice", "bot_other", note.ID)
	if err != nil || !claimed || again.StartedAt == "" {
		t.Fatalf("stale claim claimed=%v note=%#v err=%v", claimed, again, err)
	}
}
