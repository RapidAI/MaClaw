package app

import (
	"context"
	"testing"

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

func (f *fakeSeedSync) AppendSystemSetting(_ context.Context, _, _ string) {
	f.systemSettingSeeds++
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

type fakeSeedLinkStore struct{}

func (f *fakeSeedLinkStore) ListAll(context.Context) ([]*store.HubUserLink, error) {
	return []*store.HubUserLink{{ID: "link-1", HubID: "hub-1"}}, nil
}

type fakeSeedRouteStore struct{}

func (f *fakeSeedRouteStore) ListAll(context.Context) ([]*store.HubDomainRoute, error) {
	return []*store.HubDomainRoute{{ID: "route-1", HubID: "hub-1", Domain: "rapidai.tech"}}, nil
}

type fakeSeedSystemSettingsRepo struct{}

func (f *fakeSeedSystemSettingsRepo) List(context.Context) ([]*store.SystemSettingEntry, error) {
	return []*store.SystemSettingEntry{
		{Key: "llm_moderation_config", ValueJSON: `{"enabled":true}`},
		{Key: "admin_email", ValueJSON: `{"value":"admin@example.com"}`},
	}, nil
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
func (f *fakeSeedGossipRepo) DeletePost(context.Context, string) error     { return nil }
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
