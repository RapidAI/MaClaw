package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/lansenger"
	"github.com/RapidAI/CodeClaw/corelib/lansengerwatch"
)

func TestWatchServiceInitOnce(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	a := app.watchService()
	b := app.watchService()
	if a == nil || a != b {
		t.Fatalf("expected singleton watch service, got %p %p", a, b)
	}
}

func TestGroupReplyDedupeOnlyAfterRemember(t *testing.T) {
	s := &lansengerWatchService{replyDedupe: make(map[string]time.Time)}
	if s.recentGroupReply("g1", "hello") {
		t.Fatal("first check must not suppress")
	}
	// Without remember, second check still allows send (failed delivery path).
	if s.recentGroupReply("g1", "hello") {
		t.Fatal("unremembered reply must not suppress")
	}
	s.rememberGroupReply("g1", "hello")
	if !s.recentGroupReply("g1", "hello") {
		t.Fatal("after remember must suppress")
	}
	if s.recentGroupReply("g1", "other") {
		t.Fatal("different text must send")
	}
	s.mu.Lock()
	s.replyDedupe["g1\x00hello"] = time.Now().Add(-lansengerWatchReplyDedupeTTL - time.Second)
	s.mu.Unlock()
	if s.recentGroupReply("g1", "hello") {
		t.Fatal("after TTL must send again")
	}
}

func TestProcessMessageNoJobSkipsRoster(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	svc := app.watchService()
	// No enabled jobs — must not thrash roster disk.
	svc.processMessage(lansenger.IncomingMessage{
		ChatType: "group", GroupID: "g", FromUserID: "u", Text: "hi", SenderName: "N",
	})
	roster, err := svc.store.LoadRoster("g")
	if err != nil {
		t.Fatal(err)
	}
	if len(roster.Members) != 0 {
		t.Fatalf("expected empty roster when no jobs, got %+v", roster.Members)
	}
}

func TestProcessMessageOtherGroupSkipsRoster(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	svc := app.watchService()
	if _, err := svc.store.UpsertJob(lansengerwatch.Job{
		Name: "only-g1", GroupID: "g1", Enabled: true, TargetStaffIDs: []string{"u1"}, RecordAll: true,
	}); err != nil {
		t.Fatal(err)
	}
	svc.invalidateCache()
	// Message in g2 while only g1 is watched — no roster write for g2.
	svc.processMessage(lansenger.IncomingMessage{
		ChatType: "group", GroupID: "g2", FromUserID: "u2", Text: "hi", SenderName: "B",
	})
	r2, err := svc.store.LoadRoster("g2")
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Members) != 0 {
		t.Fatalf("expected no roster for unwatched group, got %+v", r2.Members)
	}
}

func TestListJobsCachedRespectsInvalidate(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	svc := app.watchService()
	if _, err := svc.store.UpsertJob(lansengerwatch.Job{
		Name: "j", GroupID: "g", Enabled: true, TargetStaffIDs: []string{"u1"}, RecordAll: true,
	}); err != nil {
		t.Fatal(err)
	}
	svc.invalidateCache()
	jobs := svc.listJobsCached()
	if len(jobs) != 1 {
		t.Fatalf("jobs: %+v", jobs)
	}
	// Second call hits TTL cache.
	jobs2 := svc.listJobsCached()
	if len(jobs2) != 1 || jobs2[0].ID != jobs[0].ID {
		t.Fatalf("cache: %+v vs %+v", jobs2, jobs)
	}
	svc.invalidateCache()
	if _, err := svc.store.UpsertJob(lansengerwatch.Job{
		ID: jobs[0].ID, Name: "j2", GroupID: "g", Enabled: true, TargetStaffIDs: []string{"u1"}, RecordAll: true,
	}); err != nil {
		t.Fatal(err)
	}
	// After invalidate, must not serve pre-invalidate cache.
	jobs3 := svc.listJobsCached()
	if len(jobs3) != 1 || jobs3[0].Name != "j2" {
		t.Fatalf("stale cache: %+v", jobs3)
	}
}

func TestListWatchChannelsJSON(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	raw, err := app.ListLansengerWatchChannels()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"id":"weixin"`) || !strings.Contains(raw, `"id":"hub"`) {
		t.Fatalf("channels: %s", raw)
	}
}

func TestListWatchRosterStableLocalFallbackOrder(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	svc := app.watchService()
	if err := svc.store.NoteMember("g", "", "z-id", "张三", "manual"); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.NoteMember("g", "", "a-id", "Alice", "manual"); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.NoteMember("g", "", "no-name", "", "manual"); err != nil {
		t.Fatal(err)
	}

	raw, err := app.ListLansengerWatchRoster("g", "")
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		DirectoryAvailable bool                    `json:"directory_available"`
		Members            []lansengerwatch.Member `json:"members"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.DirectoryAvailable {
		t.Fatal("directory must be unavailable without Lansenger credentials")
	}
	if len(payload.Members) != 3 || payload.Members[0].StaffID != "a-id" || payload.Members[1].StaffID != "z-id" || payload.Members[2].StaffID != "no-name" {
		t.Fatalf("members must be deterministic and unnamed entries last: %+v", payload.Members)
	}
}

func TestListWatchRosterUnavailableServiceKeepsObjectContract(t *testing.T) {
	var app *App
	raw, err := app.ListLansengerWatchRoster("g", "")
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Members            []lansengerwatch.Member `json:"members"`
		DirectoryAvailable bool                    `json:"directory_available"`
		Note               string                  `json:"note"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("roster response must stay an object: %q: %v", raw, err)
	}
	if payload.DirectoryAvailable || len(payload.Members) != 0 || payload.Note == "" {
		t.Fatalf("unexpected unavailable payload: %+v", payload)
	}
}

func TestDeliverToOwnerChannelUnknown(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	svc := app.watchService()
	if err := svc.deliverToOwnerChannel("nope", "x"); err == nil {
		t.Fatal("expected error")
	}
	// Empty text
	if err := svc.deliverToOwnerChannel(lansengerwatch.ChannelHub, "  "); err == nil {
		t.Fatal("expected empty text error")
	}
}
