package app

import (
	"context"
	"log"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/ha"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skill"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

const haSeedPageSize = 500

type haSystemSettings struct {
	inner store.SystemSettingsRepository
	sync  haSystemSettingRecorder
}

type haSystemSettingRecorder interface {
	AppendSystemSetting(ctx context.Context, key, valueJSON string)
}

func (r *haSystemSettings) Set(ctx context.Context, key, valueJSON string) error {
	if err := r.inner.Set(ctx, key, valueJSON); err != nil {
		return err
	}
	if r.sync != nil {
		r.sync.AppendSystemSetting(ctx, key, valueJSON)
	}
	return nil
}

func (r *haSystemSettings) Get(ctx context.Context, key string) (string, error) {
	return r.inner.Get(ctx, key)
}

func (r *haSystemSettings) List(ctx context.Context) ([]*store.SystemSettingEntry, error) {
	return r.inner.List(ctx)
}

type haGossipRepo struct {
	inner       store.GossipRepository
	sync        gossipSnapshotRecorder
	syncMu      sync.Mutex
	syncRunning bool
	syncPending bool
}

type gossipSnapshotRecorder interface {
	AppendGossipSnapshot(ctx context.Context, snap *ha.GossipSnapshot)
}

func (r *haGossipRepo) syncSnapshot(ctx context.Context) {
	if r == nil || r.sync == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	} else {
		ctx = context.WithoutCancel(ctx)
	}
	if !r.beginSyncSnapshot() {
		return
	}
	go r.runSyncSnapshot(ctx)
}

func (r *haGossipRepo) beginSyncSnapshot() bool {
	r.syncMu.Lock()
	defer r.syncMu.Unlock()
	if r.syncRunning {
		r.syncPending = true
		return false
	}
	r.syncRunning = true
	return true
}

func (r *haGossipRepo) finishOrContinueSyncSnapshot() bool {
	r.syncMu.Lock()
	defer r.syncMu.Unlock()
	if r.syncPending {
		r.syncPending = false
		return true
	}
	r.syncRunning = false
	return false
}

func (r *haGossipRepo) runSyncSnapshot(ctx context.Context) {
	for {
		snap, err := buildGossipSnapshot(ctx, r.inner)
		if err != nil {
			log.Printf("[hubcenter][ha] build gossip snapshot: %v", err)
		} else if r.sync != nil {
			r.sync.AppendGossipSnapshot(ctx, snap)
		}
		if !r.finishOrContinueSyncSnapshot() {
			return
		}
	}
}

func buildGossipSnapshot(ctx context.Context, repo store.GossipRepository) (*ha.GossipSnapshot, error) {
	posts, err := listAllGossipPostsPaged(ctx, repo)
	if err != nil {
		return nil, err
	}
	comments := make([]*store.GossipComment, 0)
	for _, post := range posts {
		if post == nil {
			continue
		}
		items, err := listAllGossipCommentsPaged(ctx, repo, post.ID)
		if err != nil {
			return nil, err
		}
		comments = append(comments, items...)
	}
	return &ha.GossipSnapshot{Posts: posts, Comments: comments}, nil
}

func listAllGossipPostsPaged(ctx context.Context, repo store.GossipRepository) ([]*store.GossipPost, error) {
	var out []*store.GossipPost
	for offset := 0; ; offset += haSeedPageSize {
		items, total, err := repo.ListAllPosts(ctx, offset, haSeedPageSize)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if len(items) == 0 || len(out) >= total || len(items) < haSeedPageSize {
			return out, nil
		}
	}
}

func listAllGossipCommentsPaged(ctx context.Context, repo store.GossipRepository, postID string) ([]*store.GossipComment, error) {
	var out []*store.GossipComment
	for offset := 0; ; offset += haSeedPageSize {
		items, total, err := repo.ListComments(ctx, postID, offset, haSeedPageSize)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if len(items) == 0 || len(out) >= total || len(items) < haSeedPageSize {
			return out, nil
		}
	}
}

func (r *haGossipRepo) CreatePost(ctx context.Context, post *store.GossipPost) error {
	if err := r.inner.CreatePost(ctx, post); err != nil {
		return err
	}
	r.syncSnapshot(ctx)
	return nil
}

func (r *haGossipRepo) ListPosts(ctx context.Context, offset, limit int) ([]*store.GossipPost, int, error) {
	return r.inner.ListPosts(ctx, offset, limit)
}

func (r *haGossipRepo) ListAllPosts(ctx context.Context, offset, limit int) ([]*store.GossipPost, int, error) {
	return r.inner.ListAllPosts(ctx, offset, limit)
}

func (r *haGossipRepo) ListFlaggedPosts(ctx context.Context, offset, limit int) ([]*store.GossipPost, int, error) {
	return r.inner.ListFlaggedPosts(ctx, offset, limit)
}

func (r *haGossipRepo) GetPost(ctx context.Context, id string) (*store.GossipPost, error) {
	return r.inner.GetPost(ctx, id)
}

func (r *haGossipRepo) DeletePost(ctx context.Context, id string) error {
	if err := r.inner.DeletePost(ctx, id); err != nil {
		return err
	}
	r.syncSnapshot(ctx)
	return nil
}

func (r *haGossipRepo) DeleteFlaggedPosts(ctx context.Context) (int, error) {
	deleted, err := r.inner.DeleteFlaggedPosts(ctx)
	if err != nil {
		return 0, err
	}
	if deleted > 0 {
		r.syncSnapshot(ctx)
	}
	return deleted, nil
}

func (r *haGossipRepo) LockPost(ctx context.Context, id string, locked bool) error {
	if err := r.inner.LockPost(ctx, id, locked); err != nil {
		return err
	}
	r.syncSnapshot(ctx)
	return nil
}

func (r *haGossipRepo) FlagPost(ctx context.Context, id string, flagged bool) error {
	if err := r.inner.FlagPost(ctx, id, flagged); err != nil {
		return err
	}
	r.syncSnapshot(ctx)
	return nil
}

func (r *haGossipRepo) ReplaceAll(ctx context.Context, posts []*store.GossipPost, comments []*store.GossipComment) error {
	return r.inner.ReplaceAll(ctx, posts, comments)
}

func (r *haGossipRepo) CreateComment(ctx context.Context, comment *store.GossipComment) error {
	if err := r.inner.CreateComment(ctx, comment); err != nil {
		return err
	}
	r.syncSnapshot(ctx)
	return nil
}

func (r *haGossipRepo) ListComments(ctx context.Context, postID string, offset, limit int) ([]*store.GossipComment, int, error) {
	return r.inner.ListComments(ctx, postID, offset, limit)
}

func (r *haGossipRepo) DeleteComment(ctx context.Context, id string) error {
	if err := r.inner.DeleteComment(ctx, id); err != nil {
		return err
	}
	r.syncSnapshot(ctx)
	return nil
}

func (r *haGossipRepo) UpdatePostScore(ctx context.Context, postID string) error {
	if err := r.inner.UpdatePostScore(ctx, postID); err != nil {
		return err
	}
	r.syncSnapshot(ctx)
	return nil
}

func (r *haGossipRepo) HasRated(ctx context.Context, postID, machineID string) (bool, error) {
	return r.inner.HasRated(ctx, postID, machineID)
}

func (r *haGossipRepo) RateComment(ctx context.Context, comment *store.GossipComment) error {
	if err := r.inner.RateComment(ctx, comment); err != nil {
		return err
	}
	r.syncSnapshot(ctx)
	return nil
}

type haSeedNewsRepo interface {
	List(ctx context.Context, offset, limit int) ([]*store.NewsArticle, int, error)
}

type haSeedSkillStore interface {
	DumpSnapshot() (*skill.Snapshot, error)
}

type haSeedSkillMarketStore interface {
	DumpSnapshot(ctx context.Context) (*skillmarket.Snapshot, error)
}

type haSeedHubStore interface {
	ListAll(ctx context.Context) ([]*store.HubInstance, error)
}

type haSeedHubPager interface {
	ListPage(ctx context.Context, offset, limit int) ([]*store.HubInstance, error)
}

type haSeedLinkStore interface {
	ListAll(ctx context.Context) ([]*store.HubUserLink, error)
}

type haSeedLinkPager interface {
	ListPage(ctx context.Context, offset, limit int) ([]*store.HubUserLink, error)
}

type haSeedRouteStore interface {
	ListAll(ctx context.Context) ([]*store.HubDomainRoute, error)
}

type haSeedRoutePager interface {
	ListPage(ctx context.Context, offset, limit int) ([]*store.HubDomainRoute, error)
}

type haSeedSystemSettingsStore interface {
	List(ctx context.Context) ([]*store.SystemSettingEntry, error)
}

type haSeedSyncChecker interface {
	HasEntityTypeOps(ctx context.Context, entityTypes ...string) (bool, error)
	HasEntityVersion(ctx context.Context, entityType, entityID string) (bool, error)
	AppendHubInstance(ctx context.Context, item *store.HubInstance)
	AppendHubUserLink(ctx context.Context, item *store.HubUserLink)
	AppendHubDomainRoute(ctx context.Context, item *store.HubDomainRoute)
	AppendSystemSetting(ctx context.Context, key, valueJSON string)
	AppendGossipSnapshot(ctx context.Context, snap *ha.GossipSnapshot)
	AppendNewsArticle(ctx context.Context, item *store.NewsArticle)
	AppendSkillHubSnapshot(ctx context.Context, snap *skill.Snapshot)
	AppendSkillMarketSnapshot(ctx context.Context, snap *skillmarket.Snapshot)
}

type haSeedRouteSnapshotRefresher interface {
	Rebuild(ctx context.Context) error
}

func seedInitialHASnapshots(ctx context.Context, sync haSeedSyncChecker, refresher haSeedRouteSnapshotRefresher, hubs haSeedHubStore, links haSeedLinkStore, routes haSeedRouteStore, settings haSeedSystemSettingsStore, gossip store.GossipRepository, news haSeedNewsRepo, skills haSeedSkillStore, sm haSeedSkillMarketStore) {

	if sync == nil {
		return
	}
	seededHubs := 0
	seededLinks := 0
	seededRoutes := 0
	if hubs != nil {
		if seeded, err := sync.HasEntityTypeOps(ctx, ha.EntityHubInstance); err == nil {
			if !seeded {
				count, listErr := seedHubsPaged(ctx, sync, hubs)
				if listErr != nil {
					log.Printf("[hubcenter][ha] seed routing hubs failed: %v", listErr)
				} else {
					seededHubs += count
				}
			}
		} else {
			log.Printf("[hubcenter][ha] inspect routing hub seed state failed: %v", err)
		}
	}
	if links != nil {
		if seeded, err := sync.HasEntityTypeOps(ctx, ha.EntityHubUserLink); err == nil {
			if !seeded {
				count, listErr := seedLinksPaged(ctx, sync, links)
				if listErr != nil {
					log.Printf("[hubcenter][ha] seed routing links failed: %v", listErr)
				} else {
					seededLinks += count
				}
			}
		} else {
			log.Printf("[hubcenter][ha] inspect routing link seed state failed: %v", err)
		}
	}
	if routes != nil {
		if seeded, err := sync.HasEntityTypeOps(ctx, ha.EntityHubDomainRoute); err == nil {
			if !seeded {
				count, listErr := seedRoutesPaged(ctx, sync, routes)
				if listErr != nil {
					log.Printf("[hubcenter][ha] seed routing routes failed: %v", listErr)
				} else {
					seededRoutes += count
				}
			}
		} else {
			log.Printf("[hubcenter][ha] inspect routing route seed state failed: %v", err)
		}
	}
	if seededHubs > 0 || seededLinks > 0 || seededRoutes > 0 {
		log.Printf("[hubcenter][ha] seeded routing snapshot: hubs=%d links=%d routes=%d", seededHubs, seededLinks, seededRoutes)
	}
	if refresher != nil {
		if err := refresher.Rebuild(ctx); err != nil {
			log.Printf("[hubcenter][ha] refresh routing snapshot after seed failed: %v", err)
		}
	}
	if seeded, err := sync.HasEntityTypeOps(ctx, ha.EntitySystemSetting); err == nil && !seeded && settings != nil {
		items, listErr := settings.List(ctx)
		if listErr != nil {
			log.Printf("[hubcenter][ha] seed system settings failed: %v", listErr)
		} else {
			seededCount := 0
			for _, item := range items {
				if item == nil || strings.TrimSpace(item.Key) == "" {
					continue
				}
				sync.AppendSystemSetting(ctx, item.Key, item.ValueJSON)
				seededCount++
			}
			if seededCount > 0 {
				log.Printf("[hubcenter][ha] seeded system settings: count=%d", seededCount)
			}
		}
	} else if err != nil {
		log.Printf("[hubcenter][ha] inspect system setting seed state failed: %v", err)
	}
	if seeded, err := sync.HasEntityTypeOps(ctx, ha.EntityGossipSnapshot); err == nil && !seeded && gossip != nil {
		snap, snapErr := buildGossipSnapshot(ctx, gossip)
		if snapErr != nil {
			log.Printf("[hubcenter][ha] seed gossip snapshot failed: %v", snapErr)
		} else if len(snap.Posts) > 0 || len(snap.Comments) > 0 {
			sync.AppendGossipSnapshot(ctx, snap)
			log.Printf("[hubcenter][ha] seeded gossip snapshot: posts=%d comments=%d", len(snap.Posts), len(snap.Comments))
		}
	} else if err != nil {
		log.Printf("[hubcenter][ha] inspect gossip seed state failed: %v", err)
	}

	if seeded, err := sync.HasEntityTypeOps(ctx, ha.EntityNewsArticle); err == nil && !seeded && news != nil {
		seededCount, skippedCount, listErr := seedNewsArticlesPaged(ctx, sync, news)
		if listErr != nil {
			log.Printf("[hubcenter][ha] seed news snapshot failed: %v", listErr)
		} else if seededCount > 0 || skippedCount > 0 {
			log.Printf("[hubcenter][ha] seeded news snapshot: articles=%d skipped=%d", seededCount, skippedCount)
		}
	} else if err != nil {
		log.Printf("[hubcenter][ha] inspect news seed state failed: %v", err)
	}

	if seeded, err := sync.HasEntityTypeOps(ctx, ha.EntitySkillHubSnapshot); err == nil && !seeded && skills != nil {
		snap, snapErr := skills.DumpSnapshot()
		if snapErr != nil {
			log.Printf("[hubcenter][ha] seed skillhub snapshot failed: %v", snapErr)
		} else if snap != nil && len(snap.Skills) > 0 {
			sync.AppendSkillHubSnapshot(ctx, snap)
			log.Printf("[hubcenter][ha] seeded skillhub snapshot: skills=%d", len(snap.Skills))
		}
	} else if err != nil {
		log.Printf("[hubcenter][ha] inspect skillhub seed state failed: %v", err)
	}

	if seeded, err := sync.HasEntityTypeOps(ctx, ha.EntitySkillMarketSnapshot); err == nil && !seeded && sm != nil {
		snap, snapErr := sm.DumpSnapshot(ctx)
		if snapErr != nil {
			log.Printf("[hubcenter][ha] seed skillmarket snapshot failed: %v", snapErr)
		} else if snap != nil {
			sync.AppendSkillMarketSnapshot(ctx, snap)
			log.Printf("[hubcenter][ha] seeded skillmarket snapshot")
		}
	} else if err != nil {
		log.Printf("[hubcenter][ha] inspect skillmarket seed state failed: %v", err)
	}
}

func seedHubsPaged(ctx context.Context, sync haSeedSyncChecker, hubs haSeedHubStore) (int, error) {
	seed := func(items []*store.HubInstance) int {
		count := 0
		for _, item := range items {
			if item == nil {
				continue
			}
			sync.AppendHubInstance(ctx, item)
			count++
		}
		return count
	}
	if pager, ok := hubs.(haSeedHubPager); ok {
		count := 0
		for offset := 0; ; offset += haSeedPageSize {
			items, err := pager.ListPage(ctx, offset, haSeedPageSize)
			if err != nil {
				return count, err
			}
			count += seed(items)
			if len(items) < haSeedPageSize {
				return count, nil
			}
		}
	}
	items, err := hubs.ListAll(ctx)
	if err != nil {
		return 0, err
	}
	return seed(items), nil
}

func seedLinksPaged(ctx context.Context, sync haSeedSyncChecker, links haSeedLinkStore) (int, error) {
	seed := func(items []*store.HubUserLink) int {
		count := 0
		for _, item := range items {
			if item == nil {
				continue
			}
			sync.AppendHubUserLink(ctx, item)
			count++
		}
		return count
	}
	if pager, ok := links.(haSeedLinkPager); ok {
		count := 0
		for offset := 0; ; offset += haSeedPageSize {
			items, err := pager.ListPage(ctx, offset, haSeedPageSize)
			if err != nil {
				return count, err
			}
			count += seed(items)
			if len(items) < haSeedPageSize {
				return count, nil
			}
		}
	}
	items, err := links.ListAll(ctx)
	if err != nil {
		return 0, err
	}
	return seed(items), nil
}

func seedRoutesPaged(ctx context.Context, sync haSeedSyncChecker, routes haSeedRouteStore) (int, error) {
	seed := func(items []*store.HubDomainRoute) int {
		count := 0
		for _, item := range items {
			if item == nil {
				continue
			}
			sync.AppendHubDomainRoute(ctx, item)
			count++
		}
		return count
	}
	if pager, ok := routes.(haSeedRoutePager); ok {
		count := 0
		for offset := 0; ; offset += haSeedPageSize {
			items, err := pager.ListPage(ctx, offset, haSeedPageSize)
			if err != nil {
				return count, err
			}
			count += seed(items)
			if len(items) < haSeedPageSize {
				return count, nil
			}
		}
	}
	items, err := routes.ListAll(ctx)
	if err != nil {
		return 0, err
	}
	return seed(items), nil
}

func seedNewsArticlesPaged(ctx context.Context, sync haSeedSyncChecker, news haSeedNewsRepo) (int, int, error) {
	seededCount := 0
	skippedCount := 0
	for offset := 0; ; offset += haSeedPageSize {
		items, total, err := news.List(ctx, offset, haSeedPageSize)
		if err != nil {
			return seededCount, skippedCount, err
		}
		for _, item := range items {
			if item == nil {
				continue
			}
			exists, existsErr := sync.HasEntityVersion(ctx, ha.EntityNewsArticle, item.ID)
			if existsErr != nil {
				log.Printf("[hubcenter][ha] inspect news entity version failed for %s: %v", item.ID, existsErr)
				continue
			}
			if exists {
				skippedCount++
				continue
			}
			sync.AppendNewsArticle(ctx, item)
			seededCount++
		}
		if len(items) == 0 || offset+len(items) >= total || len(items) < haSeedPageSize {
			return seededCount, skippedCount, nil
		}
	}
}
