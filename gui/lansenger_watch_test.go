package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
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

func TestWatchRosterCacheExpiresAndInvalidates(t *testing.T) {
	svc := &lansengerWatchService{rosterCache: make(map[string]lansengerWatchRosterCacheEntry)}
	svc.cacheRoster("g", "Group", []lansengerwatch.Member{{StaffID: "u1", Name: "Alice"}}, false)
	cached, ok := svc.cachedRoster("g")
	if !ok || cached.groupName != "Group" || len(cached.members) != 1 {
		t.Fatalf("cache miss: %+v, ok=%v", cached, ok)
	}
	// A caller must not be able to mutate the stored cache through its copy.
	cached.members[0].Name = "changed"
	again, ok := svc.cachedRoster("g")
	if !ok || again.members[0].Name != "Alice" {
		t.Fatalf("cache aliasing: %+v", again)
	}
	svc.invalidateRosterCache("g")
	if _, ok := svc.cachedRoster("g"); ok {
		t.Fatal("manual entry must invalidate the affected roster cache")
	}
}

func TestWatchRosterCacheLearnsInboundMemberWithoutCreatingPartialCache(t *testing.T) {
	svc := &lansengerWatchService{rosterCache: make(map[string]lansengerWatchRosterCacheEntry)}
	svc.noteCachedRosterMember("uncached", "Group", "u1", "Alice")
	if _, ok := svc.cachedRoster("uncached"); ok {
		t.Fatal("inbound traffic must not create a partial directory cache")
	}
	svc.cacheRoster("g", "Group", []lansengerwatch.Member{{StaffID: "u1", Name: "Old"}}, false)
	svc.noteCachedRosterMember("g", "Group", "u1", "Alice")
	svc.noteCachedRosterMember("g", "Group", "u2", "Bob")
	cached, ok := svc.cachedRoster("g")
	if !ok || len(cached.members) != 2 || cached.members[0].Name != "Alice" || cached.members[1].StaffID != "u2" {
		t.Fatalf("cache should reflect inbound directory changes: %+v", cached)
	}
}

func TestWatchRosterCachePreservesTruncation(t *testing.T) {
	svc := &lansengerWatchService{rosterCache: make(map[string]lansengerWatchRosterCacheEntry)}
	svc.cacheRoster("g", "Large group", []lansengerwatch.Member{{StaffID: "u1"}}, true)
	svc.noteCachedRosterMember("g", "Large group", "u2", "Bob")
	cached, ok := svc.cachedRoster("g")
	if !ok || !cached.truncated || len(cached.members) != 1 {
		t.Fatalf("cache must preserve partial-directory state: %+v", cached)
	}
}

func TestWatchRosterLocalMemberNameWinsOverCachedDirectory(t *testing.T) {
	local := lansengerwatch.Member{StaffID: "u1", Name: "Fresh name", Source: "message"}
	cached := lansengerwatch.Member{StaffID: "u1", Name: "Stale name", Source: "directory"}
	membersByID := map[string]lansengerwatch.Member{local.StaffID: local}
	mergeCachedRosterMembers(membersByID, []lansengerwatch.Member{cached, {StaffID: "u2", Name: "Directory only"}})
	if got := membersByID["u1"].Name; got != "Fresh name" {
		t.Fatalf("local roster name must win, got %q", got)
	}
	if got := membersByID["u2"].Name; got != "Directory only" {
		t.Fatalf("directory-only member must be retained, got %q", got)
	}
}

func TestWatchRosterCacheBoundsLargeGroupEntries(t *testing.T) {
	svc := &lansengerWatchService{rosterCache: make(map[string]lansengerWatchRosterCacheEntry)}
	for i := 0; i < lansengerWatchMaxCachedRosters+1; i++ {
		svc.cacheRoster(fmt.Sprintf("g-%d", i), "Group", []lansengerwatch.Member{{StaffID: fmt.Sprintf("u-%d", i)}}, false)
	}
	if got := len(svc.rosterCache); got != lansengerWatchMaxCachedRosters {
		t.Fatalf("cache size=%d, want %d", got, lansengerWatchMaxCachedRosters)
	}
	if _, ok := svc.cachedRoster(fmt.Sprintf("g-%d", lansengerWatchMaxCachedRosters)); !ok {
		t.Fatal("most recently cached group must remain available")
	}
}

func TestWatchRosterSkipsDeletedDirectoryMembers(t *testing.T) {
	member := lansenger.GroupMember{StaffID: "deleted", FromType: 0, Status: 3}
	if isWatchTargetMember(member) {
		t.Fatal("deleted directory member must not be selectable")
	}
	if !isWatchTargetMember(lansenger.GroupMember{StaffID: "active", FromType: 0, Status: 1}) {
		t.Fatal("active human member must be selectable")
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

// watchRosterServerOpts tunes the fake group-member directory.
type watchRosterServerOpts struct {
	total           int  // directory entries
	serverPageSize  int  // effective page cap regardless of requested page_size
	failAfterOffset int  // -1 = never fail; requests with offset >= this get errCode 500
	botsFirst       int  // first N directory entries are bots (fromType=1)
	reportTotal     bool // include totalMembers in responses (omitted otherwise)
	replay          bool // ignore page_offset and always return the first page
	emptyEvery      int  // every Nth directory entry (1-based) has an empty staffId; 0 = none
}

// watchRosterTestServer stubs the app-token and group-member directory
// endpoints. The members handler caps the effective page size at serverPageSize
// regardless of the requested page_size, mimicking deployments that ignore or
// clamp pagination params.
func watchRosterTestServer(t *testing.T, opts watchRosterServerOpts) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/apptoken/create", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data":    map[string]any{"appToken": "tok", "expiresIn": 3600},
		})
	})
	mux.HandleFunc("/v2/groups/g/members/fetch", func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("page_offset"))
		if opts.failAfterOffset >= 0 && offset >= opts.failAfterOffset {
			_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 500, "errMsg": "boom"})
			return
		}
		if opts.replay {
			offset = 0
		}
		end := offset + opts.serverPageSize
		if end > opts.total {
			end = opts.total
		}
		members := []map[string]any{}
		for i := offset; i < end; i++ {
			fromType := 0
			if i < opts.botsFirst {
				fromType = 1
			}
			staffID := fmt.Sprintf("u-%d", i)
			if opts.emptyEvery > 0 && (i+1)%opts.emptyEvery == 0 {
				staffID = ""
			}
			members = append(members, map[string]any{
				"staffId":  staffID,
				"name":     fmt.Sprintf("Member %d", i),
				"fromType": fromType,
				"status":   1,
			})
		}
		data := map[string]any{"members": members}
		if opts.reportTotal {
			data["totalMembers"] = opts.total
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data":    data,
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func watchRosterTestApp(t *testing.T, srv *httptest.Server) *App {
	t.Helper()
	app := &App{testHomeDir: t.TempDir()}
	app.lansengerGateway = &lansengerGatewayManager{
		gateway: lansenger.NewGateway(lansenger.Config{AppID: "app", AppSecret: "sec", ApiGatewayURL: srv.URL}, nil),
	}
	return app
}

func TestListWatchRosterLoadsAllMembersWhenServerCapsPageSize(t *testing.T) {
	// 230 members, server answers at most 50 per page even though the client
	// asks for 100. A short page must not be treated as end-of-directory.
	srv := watchRosterTestServer(t, watchRosterServerOpts{total: 230, serverPageSize: 50, failAfterOffset: -1})
	app := watchRosterTestApp(t, srv)

	raw, err := app.ListLansengerWatchRoster("g", "")
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		DirectoryAvailable bool                    `json:"directory_available"`
		DirectoryTruncated bool                    `json:"directory_truncated"`
		Members            []lansengerwatch.Member `json:"members"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.DirectoryAvailable {
		t.Fatalf("directory must be available: %s", raw)
	}
	if payload.DirectoryTruncated {
		t.Fatalf("230 members fit under the cap; must not be truncated: %s", raw)
	}
	if len(payload.Members) != 230 {
		t.Fatalf("members=%d, want all 230 despite server-capped pages", len(payload.Members))
	}
}

func TestListWatchRosterKeepsPartialDirectoryOnMidPaginationError(t *testing.T) {
	// First page succeeds (50 members, no reported total), the next offset
	// fails. The partial directory must be kept and flagged, not replaced by
	// only locally learned members.
	srv := watchRosterTestServer(t, watchRosterServerOpts{total: 230, serverPageSize: 50, failAfterOffset: 50})
	app := watchRosterTestApp(t, srv)
	svc := app.watchService()
	if err := svc.store.NoteMember("g", "", "local-1", "本地成员", "manual"); err != nil {
		t.Fatal(err)
	}

	raw, err := app.ListLansengerWatchRoster("g", "")
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		DirectoryAvailable bool                    `json:"directory_available"`
		DirectoryTruncated bool                    `json:"directory_truncated"`
		Note               string                  `json:"note"`
		Members            []lansengerwatch.Member `json:"members"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.DirectoryAvailable || !payload.DirectoryTruncated {
		t.Fatalf("partial directory must stay available but truncated: %s", raw)
	}
	// 50 directory members + 1 locally learned member.
	if len(payload.Members) != 51 {
		t.Fatalf("members=%d, want 51 (50 partial directory + 1 local): %s", len(payload.Members), raw)
	}
	if !strings.Contains(payload.Note, "不完整") {
		t.Fatalf("note must warn about the interrupted directory: %q", payload.Note)
	}
}

func TestListWatchRosterLocalOverlapDoesNotStopPagination(t *testing.T) {
	// The local roster already knows exactly the first server page. The replay
	// guard must not mistake that overlap for an offset-ignoring server and
	// stop before the later pages.
	srv := watchRosterTestServer(t, watchRosterServerOpts{total: 230, serverPageSize: 50, failAfterOffset: -1})
	app := watchRosterTestApp(t, srv)
	svc := app.watchService()
	for i := 0; i < 50; i++ {
		if err := svc.store.NoteMember("g", "", fmt.Sprintf("u-%d", i), fmt.Sprintf("Member %d", i), "message"); err != nil {
			t.Fatal(err)
		}
	}

	raw, err := app.ListLansengerWatchRoster("g", "")
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Members []lansengerwatch.Member `json:"members"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Members) != 230 {
		t.Fatalf("members=%d, want all 230 even though page one was already known locally", len(payload.Members))
	}
}

func TestListWatchRosterPartialDirectoryIsNotCached(t *testing.T) {
	// An interrupted (partial) directory must not be cached: the refresh button
	// is the user's retry path and has to re-hit the directory, not replay a
	// stale partial for the cache TTL.
	srv := watchRosterTestServer(t, watchRosterServerOpts{total: 230, serverPageSize: 50, failAfterOffset: 50})
	app := watchRosterTestApp(t, srv)
	svc := app.watchService()

	assertPartial := func(raw string) {
		t.Helper()
		var payload struct {
			DirectoryTruncated bool   `json:"directory_truncated"`
			Note               string `json:"note"`
		}
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			t.Fatal(err)
		}
		if !payload.DirectoryTruncated || !strings.Contains(payload.Note, "不完整") {
			t.Fatalf("interrupted directory must be flagged with a warning note: %s", raw)
		}
	}

	raw, err := app.ListLansengerWatchRoster("g", "")
	if err != nil {
		t.Fatal(err)
	}
	assertPartial(raw)
	if _, ok := svc.cachedRoster("g"); ok {
		t.Fatal("partial directory must not be cached; refresh is the retry path")
	}
	// No cache entry exists, so this second call must have re-fetched — and it
	// still reports the interruption rather than a stale cached view.
	raw2, err := app.ListLansengerWatchRoster("g", "")
	if err != nil {
		t.Fatal(err)
	}
	assertPartial(raw2)
}

func TestListWatchRosterAllBotPageDoesNotStopPagination(t *testing.T) {
	// The first directory page contains only bots (ineligible as watch
	// targets). The replay guard must not treat "no new eligible member" as an
	// offset-ignoring server; the people on later pages must still load.
	srv := watchRosterTestServer(t, watchRosterServerOpts{total: 230, serverPageSize: 50, failAfterOffset: -1, botsFirst: 50})
	app := watchRosterTestApp(t, srv)

	raw, err := app.ListLansengerWatchRoster("g", "")
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Members []lansengerwatch.Member `json:"members"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	// 230 directory entries, of which the first 50 are bots.
	if len(payload.Members) != 180 {
		t.Fatalf("members=%d, want 180 people (230 entries - 50 bots)", len(payload.Members))
	}
}

func TestListWatchRosterTruncatesAtMemberCap(t *testing.T) {
	// 2500 directory entries exceed the picker cap: the fetch must stop at the
	// cap, flag truncation, and say so in Chinese.
	srv := watchRosterTestServer(t, watchRosterServerOpts{total: 2500, serverPageSize: 100, failAfterOffset: -1})
	app := watchRosterTestApp(t, srv)

	raw, err := app.ListLansengerWatchRoster("g", "")
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		DirectoryAvailable bool                    `json:"directory_available"`
		DirectoryTruncated bool                    `json:"directory_truncated"`
		Note               string                  `json:"note"`
		Members            []lansengerwatch.Member `json:"members"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.DirectoryAvailable || !payload.DirectoryTruncated {
		t.Fatalf("cap-hit directory must stay available but truncated: %s", raw)
	}
	if len(payload.Members) != lansengerWatchMaxRosterMembers {
		t.Fatalf("members=%d, want cap %d", len(payload.Members), lansengerWatchMaxRosterMembers)
	}
	if !strings.Contains(payload.Note, "2000") {
		t.Fatalf("note must state the loaded cap: %q", payload.Note)
	}
}

func TestListWatchRosterStopsAtReportedTotal(t *testing.T) {
	// totalMembers reported: pagination must stop exactly at the reported
	// coverage (no extra empty-page request needed) and stay untruncated.
	srv := watchRosterTestServer(t, watchRosterServerOpts{total: 230, serverPageSize: 100, failAfterOffset: -1, reportTotal: true})
	app := watchRosterTestApp(t, srv)

	raw, err := app.ListLansengerWatchRoster("g", "")
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		DirectoryAvailable bool                    `json:"directory_available"`
		DirectoryTruncated bool                    `json:"directory_truncated"`
		Members            []lansengerwatch.Member `json:"members"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.DirectoryAvailable || payload.DirectoryTruncated {
		t.Fatalf("complete directory with reported total: %s", raw)
	}
	if len(payload.Members) != 230 {
		t.Fatalf("members=%d, want 230", len(payload.Members))
	}
}

func TestListWatchRosterReportedTotalAboveCapTruncates(t *testing.T) {
	// totalMembers=2500 exceeds the picker cap: stop at the cap and flag
	// truncation via the TotalMembers > cap expression.
	srv := watchRosterTestServer(t, watchRosterServerOpts{total: 2500, serverPageSize: 100, failAfterOffset: -1, reportTotal: true})
	app := watchRosterTestApp(t, srv)

	raw, err := app.ListLansengerWatchRoster("g", "")
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		DirectoryTruncated bool                    `json:"directory_truncated"`
		Note               string                  `json:"note"`
		Members            []lansengerwatch.Member `json:"members"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.DirectoryTruncated || len(payload.Members) != lansengerWatchMaxRosterMembers {
		t.Fatalf("cap-hit with reported total: truncated=%v members=%d", payload.DirectoryTruncated, len(payload.Members))
	}
	if !strings.Contains(payload.Note, "2000") {
		t.Fatalf("note must state the loaded cap: %q", payload.Note)
	}
}

func TestListWatchRosterFirstPageFailureFallsBackToLocal(t *testing.T) {
	// Gateway exists but the very first directory request fails: fall back to
	// locally learned members only, mark the directory unavailable, no cache.
	srv := watchRosterTestServer(t, watchRosterServerOpts{total: 230, serverPageSize: 50, failAfterOffset: 0})
	app := watchRosterTestApp(t, srv)
	svc := app.watchService()
	if err := svc.store.NoteMember("g", "", "local-1", "本地成员", "manual"); err != nil {
		t.Fatal(err)
	}

	raw, err := app.ListLansengerWatchRoster("g", "")
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		DirectoryAvailable bool                    `json:"directory_available"`
		Note               string                  `json:"note"`
		Members            []lansengerwatch.Member `json:"members"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.DirectoryAvailable {
		t.Fatalf("first-page failure must mark directory unavailable: %s", raw)
	}
	if len(payload.Members) != 1 || payload.Members[0].StaffID != "local-1" {
		t.Fatalf("must show only locally learned members: %+v", payload.Members)
	}
	if !strings.Contains(payload.Note, "本地已学习成员") {
		t.Fatalf("note must explain the local fallback: %q", payload.Note)
	}
	if _, ok := svc.cachedRoster("g"); ok {
		t.Fatal("failed directory fetch must not populate the roster cache")
	}
}

func TestListWatchRosterReplayedPagesTreatedAsPartial(t *testing.T) {
	// The server ignores page_offset and replays the first page. The replay
	// guard must stop the loop AND treat the result as partial: flagged,
	// warning note, and NOT cached as a "complete" roster.
	srv := watchRosterTestServer(t, watchRosterServerOpts{total: 500, serverPageSize: 50, failAfterOffset: -1, replay: true})
	app := watchRosterTestApp(t, srv)
	svc := app.watchService()

	raw, err := app.ListLansengerWatchRoster("g", "")
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		DirectoryAvailable bool                    `json:"directory_available"`
		DirectoryTruncated bool                    `json:"directory_truncated"`
		Note               string                  `json:"note"`
		Members            []lansengerwatch.Member `json:"members"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.DirectoryAvailable || !payload.DirectoryTruncated {
		t.Fatalf("replayed directory must stay available but truncated: %s", raw)
	}
	if len(payload.Members) != 50 {
		t.Fatalf("members=%d, want the 50 from the first page", len(payload.Members))
	}
	if !strings.Contains(payload.Note, "不完整") {
		t.Fatalf("note must warn about the incomplete directory: %q", payload.Note)
	}
	if _, ok := svc.cachedRoster("g"); ok {
		t.Fatal("replayed (partial) directory must not be cached as complete")
	}
}

func TestListWatchRosterStepsByRawCountWithEmptyStaffIDs(t *testing.T) {
	// Every second entry has an empty staffId: PageCount (raw) exceeds
	// len(page.Members). Stepping by the filtered length would drift the
	// offset into overlapping pages and lose members; stepping by PageCount
	// must keep the coordinates exact.
	srv := watchRosterTestServer(t, watchRosterServerOpts{total: 230, serverPageSize: 50, failAfterOffset: -1, emptyEvery: 2})
	app := watchRosterTestApp(t, srv)

	raw, err := app.ListLansengerWatchRoster("g", "")
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Members []lansengerwatch.Member `json:"members"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	// Entries 0..229; odd 1-based positions (i.e. even index i+1 divisible by
	// 2) are empty: 115 usable members.
	if len(payload.Members) != 115 {
		t.Fatalf("members=%d, want 115 (offset must step by raw page count)", len(payload.Members))
	}
}

func TestListWatchRosterLargeLocalRosterDoesNotMislabelTruncation(t *testing.T) {
	// The member cap bounds the directory fetch, not the locally learned
	// roster. With 1999 local members preloaded, a small directory must still
	// load completely and must NOT be labeled "群成员较多" merely because the
	// merged map crossed the cap.
	srv := watchRosterTestServer(t, watchRosterServerOpts{total: 60, serverPageSize: 50, failAfterOffset: -1})
	app := watchRosterTestApp(t, srv)
	svc := app.watchService()

	local := lansengerwatch.GroupRoster{GroupID: "g", Members: make([]lansengerwatch.Member, 0, 1999)}
	for i := 0; i < 1999; i++ {
		local.Members = append(local.Members, lansengerwatch.Member{
			StaffID: fmt.Sprintf("local-%d", i), Name: fmt.Sprintf("本地%d", i), Source: "message",
		})
	}
	data, err := json.Marshal(local)
	if err != nil {
		t.Fatal(err)
	}
	rosterPath := filepath.Join(svc.store.Root(), "roster", "g.json")
	if err := os.MkdirAll(filepath.Dir(rosterPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rosterPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	raw, err := app.ListLansengerWatchRoster("g", "")
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		DirectoryTruncated bool                    `json:"directory_truncated"`
		Note               string                  `json:"note"`
		Members            []lansengerwatch.Member `json:"members"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.DirectoryTruncated || strings.Contains(payload.Note, "群成员较多") {
		t.Fatalf("small complete directory over a large local roster must not be mislabeled: %s", raw)
	}
	// 1999 local + 60 directory, and pagination must have run to completion.
	if len(payload.Members) != 2059 {
		t.Fatalf("members=%d, want 2059 (1999 local + 60 directory)", len(payload.Members))
	}
}
