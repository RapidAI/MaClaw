package app

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/ha"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skill"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

type fakeSeedSync struct {
	has                map[string]bool
	versions           map[string]bool
	routingHubs        int
	routingLinks       int
	routingRoutes      int
	systemSettingSeeds int
	systemSettingKeys  []string
	systemSettingVals  map[string]string
	gossipSeeds        int
	newsSeeds          int
	skillHubSeeds      int
	skillMarketSeeds   int
	refreshCalls       int
}

func (f *fakeSeedSync) HasEntityTypeOps(_ context.Context, entityTypes ...string) (bool, error) {
	for _, entityType := range entityTypes {
		if f.has[entityType] {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeSeedSync) HasEntityVersion(_ context.Context, entityType, entityID string) (bool, error) {
	if f.versions == nil {
		return false, nil
	}
	return f.versions[entityType+":"+entityID], nil
}

func (f *fakeSeedSync) AppendHubInstance(_ context.Context, _ *store.HubInstance) {
	f.routingHubs++
}

func (f *fakeSeedSync) AppendHubUserLink(_ context.Context, _ *store.HubUserLink) {
	f.routingLinks++
}

func (f *fakeSeedSync) AppendHubDomainRoute(_ context.Context, _ *store.HubDomainRoute) {
	f.routingRoutes++
}

func (f *fakeSeedSync) AppendSystemSetting(_ context.Context, key, valueJSON string) {
	f.systemSettingSeeds++
	f.systemSettingKeys = append(f.systemSettingKeys, key)
	if f.systemSettingVals == nil {
		f.systemSettingVals = map[string]string{}
	}
	f.systemSettingVals[key] = valueJSON
}

func (f *fakeSeedSync) AppendGossipSnapshot(_ context.Context, _ *ha.GossipSnapshot) {
	f.gossipSeeds++
}

func (f *fakeSeedSync) AppendNewsArticle(_ context.Context, _ *store.NewsArticle) {
	f.newsSeeds++
}

func (f *fakeSeedSync) AppendSkillHubSnapshot(_ context.Context, _ *skill.Snapshot) {
	f.skillHubSeeds++
}

func (f *fakeSeedSync) AppendSkillMarketSnapshot(_ context.Context, _ *skillmarket.Snapshot) {
	f.skillMarketSeeds++
}

type fakeSeedRefresher struct {
	calls int
}

func (f *fakeSeedRefresher) Rebuild(context.Context) error {
	f.calls++
	return nil
}

type fakeSeedHubStore struct{}

func (f *fakeSeedHubStore) ListAll(context.Context) ([]*store.HubInstance, error) {
	return []*store.HubInstance{{ID: "hub-1"}, {ID: "hub-2"}}, nil
}

type fakePagedSeedHubStore struct {
	items     []*store.HubInstance
	pageCalls int
	listCalls int
}

func (f *fakePagedSeedHubStore) ListAll(context.Context) ([]*store.HubInstance, error) {
	f.listCalls++
	return f.items, nil
}

func (f *fakePagedSeedHubStore) ListPage(_ context.Context, offset, limit int) ([]*store.HubInstance, error) {
	f.pageCalls++
	if offset >= len(f.items) {
		return nil, nil
	}
	end := offset + limit
	if end > len(f.items) {
		end = len(f.items)
	}
	return f.items[offset:end], nil
}

type fakeSeedLinkStore struct{}

func (f *fakeSeedLinkStore) ListAll(context.Context) ([]*store.HubUserLink, error) {
	return []*store.HubUserLink{{ID: "link-1", HubID: "hub-1"}}, nil
}

type fakePagedSeedLinkStore struct {
	items     []*store.HubUserLink
	pageCalls int
	listCalls int
}

func (f *fakePagedSeedLinkStore) ListAll(context.Context) ([]*store.HubUserLink, error) {
	f.listCalls++
	return f.items, nil
}

func (f *fakePagedSeedLinkStore) ListPage(_ context.Context, offset, limit int) ([]*store.HubUserLink, error) {
	f.pageCalls++
	if offset >= len(f.items) {
		return nil, nil
	}
	end := offset + limit
	if end > len(f.items) {
		end = len(f.items)
	}
	return f.items[offset:end], nil
}

type fakeSeedRouteStore struct{}

func (f *fakeSeedRouteStore) ListAll(context.Context) ([]*store.HubDomainRoute, error) {
	return []*store.HubDomainRoute{{ID: "route-1", HubID: "hub-1", Domain: "rapidai.tech"}}, nil
}

type fakePagedSeedRouteStore struct {
	items     []*store.HubDomainRoute
	pageCalls int
	listCalls int
}

func (f *fakePagedSeedRouteStore) ListAll(context.Context) ([]*store.HubDomainRoute, error) {
	f.listCalls++
	return f.items, nil
}

func (f *fakePagedSeedRouteStore) ListPage(_ context.Context, offset, limit int) ([]*store.HubDomainRoute, error) {
	f.pageCalls++
	if offset >= len(f.items) {
		return nil, nil
	}
	end := offset + limit
	if end > len(f.items) {
		end = len(f.items)
	}
	return f.items[offset:end], nil
}

type fakeSeedSystemSettingsRepo struct{}

func (f *fakeSeedSystemSettingsRepo) List(context.Context) ([]*store.SystemSettingEntry, error) {
	return []*store.SystemSettingEntry{
		{Key: "llm_moderation_config", ValueJSON: `{"enabled":true}`},
		{Key: "admin_email", ValueJSON: `{"value":"admin@example.com"}`},
	}, nil
}

type fakeHASystemSettingsRepo struct {
	items map[string]string
}

func (f *fakeHASystemSettingsRepo) Set(_ context.Context, key, valueJSON string) error {
	if f.items == nil {
		f.items = map[string]string{}
	}
	f.items[key] = valueJSON
	return nil
}

func (f *fakeHASystemSettingsRepo) Get(_ context.Context, key string) (string, error) {
	return f.items[key], nil
}

func (f *fakeHASystemSettingsRepo) List(context.Context) ([]*store.SystemSettingEntry, error) {
	items := make([]*store.SystemSettingEntry, 0, len(f.items))
	for key, value := range f.items {
		items = append(items, &store.SystemSettingEntry{Key: key, ValueJSON: value})
	}
	return items, nil
}

type fakeSeedGossipRepo struct{}

func (f *fakeSeedGossipRepo) CreatePost(context.Context, *store.GossipPost) error { return nil }
func (f *fakeSeedGossipRepo) ListPosts(context.Context, int, int) ([]*store.GossipPost, int, error) {
	return nil, 0, nil
}
func (f *fakeSeedGossipRepo) ListAllPosts(context.Context, int, int) ([]*store.GossipPost, int, error) {
	return []*store.GossipPost{{ID: "post-1"}}, 1, nil
}
func (f *fakeSeedGossipRepo) ListFlaggedPosts(context.Context, int, int) ([]*store.GossipPost, int, error) {
	return nil, 0, nil
}
func (f *fakeSeedGossipRepo) GetPost(context.Context, string) (*store.GossipPost, error) {
	return nil, nil
}
func (f *fakeSeedGossipRepo) DeletePost(context.Context, string) error { return nil }
func (f *fakeSeedGossipRepo) DeleteFlaggedPosts(context.Context) (int, error) {
	return 0, nil
}
func (f *fakeSeedGossipRepo) LockPost(context.Context, string, bool) error { return nil }
func (f *fakeSeedGossipRepo) FlagPost(context.Context, string, bool) error { return nil }
func (f *fakeSeedGossipRepo) ReplaceAll(context.Context, []*store.GossipPost, []*store.GossipComment) error {
	return nil
}
func (f *fakeSeedGossipRepo) CreateComment(context.Context, *store.GossipComment) error { return nil }
func (f *fakeSeedGossipRepo) ListComments(context.Context, string, int, int) ([]*store.GossipComment, int, error) {
	return []*store.GossipComment{{ID: "comment-1", PostID: "post-1"}}, 1, nil
}
func (f *fakeSeedGossipRepo) DeleteComment(context.Context, string) error   { return nil }
func (f *fakeSeedGossipRepo) UpdatePostScore(context.Context, string) error { return nil }
func (f *fakeSeedGossipRepo) HasRated(context.Context, string, string) (bool, error) {
	return false, nil
}
func (f *fakeSeedGossipRepo) RateComment(context.Context, *store.GossipComment) error { return nil }

type fakeSeedNewsRepo struct{}

func (f *fakeSeedNewsRepo) List(context.Context, int, int) ([]*store.NewsArticle, int, error) {
	return []*store.NewsArticle{{ID: "news-1"}, {ID: "news-2"}}, 2, nil
}

type fakePagedNewsRepo struct {
	items  []*store.NewsArticle
	limits []int
}

func (f *fakePagedNewsRepo) List(_ context.Context, offset, limit int) ([]*store.NewsArticle, int, error) {
	f.limits = append(f.limits, limit)
	if offset >= len(f.items) {
		return nil, len(f.items), nil
	}
	end := offset + limit
	if end > len(f.items) {
		end = len(f.items)
	}
	return f.items[offset:end], len(f.items), nil
}

type fakePagedGossipRepo struct {
	posts         []*store.GossipPost
	comments      map[string][]*store.GossipComment
	postLimits    []int
	commentLimits []int
}

func (f *fakePagedGossipRepo) CreatePost(context.Context, *store.GossipPost) error { return nil }
func (f *fakePagedGossipRepo) ListPosts(context.Context, int, int) ([]*store.GossipPost, int, error) {
	return nil, 0, nil
}
func (f *fakePagedGossipRepo) ListAllPosts(_ context.Context, offset, limit int) ([]*store.GossipPost, int, error) {
	f.postLimits = append(f.postLimits, limit)
	if offset >= len(f.posts) {
		return nil, len(f.posts), nil
	}
	end := offset + limit
	if end > len(f.posts) {
		end = len(f.posts)
	}
	return f.posts[offset:end], len(f.posts), nil
}
func (f *fakePagedGossipRepo) ListFlaggedPosts(context.Context, int, int) ([]*store.GossipPost, int, error) {
	return nil, 0, nil
}
func (f *fakePagedGossipRepo) GetPost(context.Context, string) (*store.GossipPost, error) {
	return nil, nil
}
func (f *fakePagedGossipRepo) DeletePost(context.Context, string) error        { return nil }
func (f *fakePagedGossipRepo) DeleteFlaggedPosts(context.Context) (int, error) { return 0, nil }
func (f *fakePagedGossipRepo) LockPost(context.Context, string, bool) error    { return nil }
func (f *fakePagedGossipRepo) FlagPost(context.Context, string, bool) error    { return nil }
func (f *fakePagedGossipRepo) ReplaceAll(context.Context, []*store.GossipPost, []*store.GossipComment) error {
	return nil
}
func (f *fakePagedGossipRepo) CreateComment(context.Context, *store.GossipComment) error { return nil }
func (f *fakePagedGossipRepo) ListComments(_ context.Context, postID string, offset, limit int) ([]*store.GossipComment, int, error) {
	f.commentLimits = append(f.commentLimits, limit)
	items := f.comments[postID]
	if offset >= len(items) {
		return nil, len(items), nil
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end], len(items), nil
}
func (f *fakePagedGossipRepo) DeleteComment(context.Context, string) error   { return nil }
func (f *fakePagedGossipRepo) UpdatePostScore(context.Context, string) error { return nil }
func (f *fakePagedGossipRepo) HasRated(context.Context, string, string) (bool, error) {
	return false, nil
}
func (f *fakePagedGossipRepo) RateComment(context.Context, *store.GossipComment) error { return nil }

type blockingGossipRepo struct {
	fakeSeedGossipRepo
	calls   chan struct{}
	release chan struct{}
}

func (r *blockingGossipRepo) ListAllPosts(context.Context, int, int) ([]*store.GossipPost, int, error) {
	r.calls <- struct{}{}
	<-r.release
	return []*store.GossipPost{{ID: "post-1"}}, 1, nil
}

type fakeGossipSnapshotRecorder struct {
	mu    sync.Mutex
	count int
	done  chan struct{}
}

func (r *fakeGossipSnapshotRecorder) AppendGossipSnapshot(context.Context, *ha.GossipSnapshot) {
	r.mu.Lock()
	r.count++
	r.mu.Unlock()
	r.done <- struct{}{}
}

func (r *fakeGossipSnapshotRecorder) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}

type fakeSeedSkillStore struct{}

func (f *fakeSeedSkillStore) DumpSnapshot() (*skill.Snapshot, error) {
	return &skill.Snapshot{Skills: []skill.HubSkillFull{{HubSkillMeta: skill.HubSkillMeta{ID: "skill-1", Name: "Skill 1", Visible: true}}}}, nil
}

type fakeSeedSkillMarketStore struct{}

func (f *fakeSeedSkillMarketStore) DumpSnapshot(context.Context) (*skillmarket.Snapshot, error) {
	return &skillmarket.Snapshot{Users: []skillmarket.SnapshotUser{{ID: "user-1"}}}, nil
}

func TestSeedInitialHASnapshotsSeedsMissingCategories(t *testing.T) {
	sync := &fakeSeedSync{has: map[string]bool{}}
	refresher := &fakeSeedRefresher{}
	seedInitialHASnapshots(context.Background(), sync, refresher, &fakeSeedHubStore{}, &fakeSeedLinkStore{}, &fakeSeedRouteStore{}, &fakeSeedSystemSettingsRepo{}, &fakeSeedGossipRepo{}, &fakeSeedNewsRepo{}, &fakeSeedSkillStore{}, &fakeSeedSkillMarketStore{})
	if sync.routingHubs != 2 || sync.routingLinks != 1 || sync.routingRoutes != 1 {
		t.Fatalf("routing seeds = hubs:%d links:%d routes:%d, want 2/1/1", sync.routingHubs, sync.routingLinks, sync.routingRoutes)
	}
	if sync.systemSettingSeeds != 2 {
		t.Fatalf("system setting seeds = %d, want 2", sync.systemSettingSeeds)
	}
	if sync.gossipSeeds != 1 {
		t.Fatalf("gossip seeds = %d, want 1", sync.gossipSeeds)
	}
	if sync.newsSeeds != 2 {
		t.Fatalf("news seeds = %d, want 2", sync.newsSeeds)
	}
	if sync.skillHubSeeds != 1 {
		t.Fatalf("skill hub seeds = %d, want 1", sync.skillHubSeeds)
	}
	if sync.skillMarketSeeds != 1 {
		t.Fatalf("skill market seeds = %d, want 1", sync.skillMarketSeeds)
	}
	if refresher.calls != 1 {
		t.Fatalf("refresher calls = %d, want 1", refresher.calls)
	}
}

func TestHASystemSettingsRecordsTenantDigitalEmployeeAuthorization(t *testing.T) {
	inner := &fakeHASystemSettingsRepo{}
	sync := &fakeSeedSync{has: map[string]bool{}}

	key := "tenant_digital_employee_authorizations:hub-1"
	value := `{"tenant_a":{"quota":2,"enabled":true}}`
	settings := &haSystemSettings{inner: inner, sync: sync}
	if err := settings.Set(context.Background(), key, value); err != nil {
		t.Fatalf("wrapped set: %v", err)
	}
	if got, _ := settings.Get(context.Background(), key); got != value {
		t.Fatalf("wrapped get = %q, want %q", got, value)
	}
	if sync.systemSettingSeeds != 1 || sync.systemSettingVals[key] != value {
		t.Fatalf("system setting sync = count:%d values:%+v, want tenant auth setting", sync.systemSettingSeeds, sync.systemSettingVals)
	}
}

func TestSeedInitialHASnapshotsSkipsExistingCategories(t *testing.T) {
	sync := &fakeSeedSync{has: map[string]bool{ha.EntityHubInstance: true, ha.EntityHubUserLink: true, ha.EntityHubDomainRoute: true, ha.EntitySystemSetting: true, ha.EntityGossipSnapshot: true, ha.EntityNewsArticle: true, ha.EntitySkillHubSnapshot: true, ha.EntitySkillMarketSnapshot: true}}
	refresher := &fakeSeedRefresher{}
	seedInitialHASnapshots(context.Background(), sync, refresher, &fakeSeedHubStore{}, &fakeSeedLinkStore{}, &fakeSeedRouteStore{}, &fakeSeedSystemSettingsRepo{}, &fakeSeedGossipRepo{}, &fakeSeedNewsRepo{}, &fakeSeedSkillStore{}, &fakeSeedSkillMarketStore{})
	if sync.routingHubs != 0 || sync.routingLinks != 0 || sync.routingRoutes != 0 || sync.systemSettingSeeds != 0 || sync.gossipSeeds != 0 || sync.newsSeeds != 0 || sync.skillHubSeeds != 0 || sync.skillMarketSeeds != 0 {
		t.Fatalf("unexpected seeds: routing=%d/%d/%d settings=%d gossip=%d news=%d skillhub=%d skillmarket=%d", sync.routingHubs, sync.routingLinks, sync.routingRoutes, sync.systemSettingSeeds, sync.gossipSeeds, sync.newsSeeds, sync.skillHubSeeds, sync.skillMarketSeeds)
	}
	if refresher.calls != 1 {
		t.Fatalf("refresher calls = %d, want 1", refresher.calls)
	}
}

func TestSeedInitialHASnapshotsSkipsNewsWithExistingEntityVersion(t *testing.T) {
	sync := &fakeSeedSync{has: map[string]bool{}, versions: map[string]bool{"news_article:news-1": true}}
	refresher := &fakeSeedRefresher{}
	seedInitialHASnapshots(context.Background(), sync, refresher, &fakeSeedHubStore{}, &fakeSeedLinkStore{}, &fakeSeedRouteStore{}, &fakeSeedSystemSettingsRepo{}, &fakeSeedGossipRepo{}, &fakeSeedNewsRepo{}, &fakeSeedSkillStore{}, &fakeSeedSkillMarketStore{})
	if sync.newsSeeds != 1 {
		t.Fatalf("news seeds = %d, want 1", sync.newsSeeds)
	}
	if sync.systemSettingSeeds != 2 {
		t.Fatalf("system setting seeds = %d, want 2", sync.systemSettingSeeds)
	}
	if sync.skillHubSeeds != 1 || sync.skillMarketSeeds != 1 {
		t.Fatalf("skill seeds = hub:%d market:%d, want 1/1", sync.skillHubSeeds, sync.skillMarketSeeds)
	}
	if refresher.calls != 1 {
		t.Fatalf("refresher calls = %d, want 1", refresher.calls)
	}
}

func TestSeedInitialHASnapshotsUsesPagedRoutingStores(t *testing.T) {
	hubs := &fakePagedSeedHubStore{items: make([]*store.HubInstance, haSeedPageSize+1)}
	links := &fakePagedSeedLinkStore{items: make([]*store.HubUserLink, haSeedPageSize+1)}
	routes := &fakePagedSeedRouteStore{items: make([]*store.HubDomainRoute, haSeedPageSize+1)}
	for i := range hubs.items {
		hubs.items[i] = &store.HubInstance{ID: "hub-" + strconv.Itoa(i)}
	}
	for i := range links.items {
		links.items[i] = &store.HubUserLink{ID: "link-" + strconv.Itoa(i), HubID: "hub-0"}
	}
	for i := range routes.items {
		routes.items[i] = &store.HubDomainRoute{ID: "route-" + strconv.Itoa(i), HubID: "hub-0", Domain: "rapidai.tech"}
	}
	sync := &fakeSeedSync{has: map[string]bool{ha.EntitySystemSetting: true, ha.EntityGossipSnapshot: true, ha.EntityNewsArticle: true, ha.EntitySkillHubSnapshot: true, ha.EntitySkillMarketSnapshot: true}}
	seedInitialHASnapshots(context.Background(), sync, nil, hubs, links, routes, nil, nil, nil, nil, nil)

	want := haSeedPageSize + 1
	if sync.routingHubs != want || sync.routingLinks != want || sync.routingRoutes != want {
		t.Fatalf("routing seeds = hubs:%d links:%d routes:%d, want %d/%d/%d", sync.routingHubs, sync.routingLinks, sync.routingRoutes, want, want, want)
	}
	if hubs.listCalls != 0 || links.listCalls != 0 || routes.listCalls != 0 {
		t.Fatalf("ListAll calls = hubs:%d links:%d routes:%d, want 0", hubs.listCalls, links.listCalls, routes.listCalls)
	}
	if hubs.pageCalls != 2 || links.pageCalls != 2 || routes.pageCalls != 2 {
		t.Fatalf("page calls = hubs:%d links:%d routes:%d, want 2/2/2", hubs.pageCalls, links.pageCalls, routes.pageCalls)
	}
}

func TestSeedNewsArticlesPagedUsesBoundedPages(t *testing.T) {
	items := make([]*store.NewsArticle, haSeedPageSize+1)
	for i := range items {
		items[i] = &store.NewsArticle{ID: "news-" + strconv.Itoa(i)}
	}
	repo := &fakePagedNewsRepo{items: items}
	sync := &fakeSeedSync{has: map[string]bool{}}
	seeded, skipped, err := seedNewsArticlesPaged(context.Background(), sync, repo)
	if err != nil {
		t.Fatalf("seedNewsArticlesPaged() error = %v", err)
	}
	if seeded != len(items) || skipped != 0 || sync.newsSeeds != len(items) {
		t.Fatalf("seeded/skipped/newsSeeds = %d/%d/%d, want %d/0/%d", seeded, skipped, sync.newsSeeds, len(items), len(items))
	}
	if len(repo.limits) != 2 || repo.limits[0] != haSeedPageSize || repo.limits[1] != haSeedPageSize {
		t.Fatalf("limits = %v, want two bounded pages", repo.limits)
	}
}

func TestBuildGossipSnapshotUsesBoundedPages(t *testing.T) {
	posts := make([]*store.GossipPost, haSeedPageSize+1)
	comments := make([]*store.GossipComment, haSeedPageSize+1)
	for i := range posts {
		posts[i] = &store.GossipPost{ID: "post-" + strconv.Itoa(i)}
	}
	for i := range comments {
		comments[i] = &store.GossipComment{ID: "comment-" + strconv.Itoa(i), PostID: posts[0].ID}
	}
	repo := &fakePagedGossipRepo{posts: posts, comments: map[string][]*store.GossipComment{posts[0].ID: comments}}
	snap, err := buildGossipSnapshot(context.Background(), repo)
	if err != nil {
		t.Fatalf("buildGossipSnapshot() error = %v", err)
	}
	if len(snap.Posts) != len(posts) || len(snap.Comments) != len(comments) {
		t.Fatalf("snapshot sizes = posts:%d comments:%d, want %d/%d", len(snap.Posts), len(snap.Comments), len(posts), len(comments))
	}
	if len(repo.postLimits) != 2 || repo.postLimits[0] != haSeedPageSize || repo.postLimits[1] != haSeedPageSize {
		t.Fatalf("post limits = %v, want two bounded pages", repo.postLimits)
	}
	if len(repo.commentLimits) < 2 || repo.commentLimits[0] != haSeedPageSize || repo.commentLimits[1] != haSeedPageSize {
		t.Fatalf("comment limits = %v, want bounded pages", repo.commentLimits)
	}
}

func TestHAGossipRepoCoalescesConcurrentSnapshotEmits(t *testing.T) {
	inner := &blockingGossipRepo{calls: make(chan struct{}, 4), release: make(chan struct{}, 4)}
	recorder := &fakeGossipSnapshotRecorder{done: make(chan struct{}, 4)}
	repo := &haGossipRepo{inner: inner, sync: recorder}

	repo.syncSnapshot(context.Background())
	select {
	case <-inner.calls:
	case <-time.After(time.Second):
		t.Fatal("first snapshot build did not start")
	}
	repo.syncSnapshot(context.Background())
	repo.syncSnapshot(context.Background())

	inner.release <- struct{}{}
	select {
	case <-recorder.done:
	case <-time.After(time.Second):
		t.Fatal("first snapshot was not recorded")
	}
	select {
	case <-inner.calls:
	case <-time.After(time.Second):
		t.Fatal("coalesced pending snapshot build did not start")
	}
	inner.release <- struct{}{}
	select {
	case <-recorder.done:
	case <-time.After(time.Second):
		t.Fatal("coalesced snapshot was not recorded")
	}

	select {
	case <-inner.calls:
		t.Fatal("unexpected third snapshot build")
	case <-time.After(100 * time.Millisecond):
	}
	if got := recorder.Count(); got != 2 {
		t.Fatalf("recorded snapshots = %d, want 2", got)
	}
}
