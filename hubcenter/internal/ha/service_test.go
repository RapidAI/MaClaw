package ha

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

type fakeHASyncOpRepo struct {
	mu  sync.Mutex
	ops []*store.HASyncOp
}

func (r *fakeHASyncOpRepo) Append(_ context.Context, op *store.HASyncOp) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *op
	r.ops = append(r.ops, &cp)
	return nil
}

func (r *fakeHASyncOpRepo) ListAfterSeq(_ context.Context, afterSeq int64, limit int) ([]*store.HASyncOp, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*store.HASyncOp, 0, len(r.ops))
	for _, op := range r.ops {
		if op.Seq > afterSeq || op.Seq == 0 {
			cp := *op
			out = append(out, &cp)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (r *fakeHASyncOpRepo) GetMaxSeq(_ context.Context) (int64, error) { return 0, nil }

func (r *fakeHASyncOpRepo) HasApplied(_ context.Context, _ string) (bool, error) { return false, nil }

func (r *fakeHASyncOpRepo) MarkApplied(_ context.Context, _ *store.HAAppliedOp) error { return nil }

type fakeHAEntityVersionRepo struct {
	mu    sync.Mutex
	items map[string]*store.HAEntityVersion
	delay time.Duration
}

type fakeHAPeerCursorRepo struct {
	items map[string]*store.HAPeerCursor
}

func (r *fakeHAPeerCursorRepo) Get(_ context.Context, peerNodeID string) (*store.HAPeerCursor, error) {
	if r == nil || r.items == nil {
		return nil, nil
	}
	item := r.items[peerNodeID]
	if item == nil {
		return nil, nil
	}
	cp := *item
	return &cp, nil
}

func (r *fakeHAPeerCursorRepo) Upsert(_ context.Context, item *store.HAPeerCursor) error {
	if r.items == nil {
		r.items = map[string]*store.HAPeerCursor{}
	}
	cp := *item
	r.items[item.PeerNodeID] = &cp
	return nil
}

func (r *fakeHAEntityVersionRepo) key(entityType, entityID string) string {
	return entityType + ":" + entityID
}

func (r *fakeHAEntityVersionRepo) Get(_ context.Context, entityType, entityID string) (*store.HAEntityVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.delay > 0 {
		time.Sleep(r.delay)
	}
	item := r.items[r.key(entityType, entityID)]
	if item == nil {
		return nil, nil
	}
	cp := *item
	return &cp, nil
}

func (r *fakeHAEntityVersionRepo) Upsert(_ context.Context, item *store.HAEntityVersion) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *item
	r.items[r.key(item.EntityType, item.EntityID)] = &cp
	return nil
}

func TestShouldApplyRemoteVersion(t *testing.T) {
	now := time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		current *store.HAEntityVersion
		op      *store.HASyncOp
		want    bool
	}{
		{
			name:    "apply when current is missing",
			current: nil,
			op:      &store.HASyncOp{EntityVersion: 1, OccurredAt: now, SourceNodeID: "node-b"},
			want:    true,
		},
		{
			name:    "apply higher version",
			current: &store.HAEntityVersion{Version: 2, UpdatedAt: now, UpdatedByNodeID: "node-a"},
			op:      &store.HASyncOp{EntityVersion: 3, OccurredAt: now.Add(-time.Second), SourceNodeID: "node-b"},
			want:    true,
		},
		{
			name:    "ignore lower version",
			current: &store.HAEntityVersion{Version: 3, UpdatedAt: now, UpdatedByNodeID: "node-a"},
			op:      &store.HASyncOp{EntityVersion: 2, OccurredAt: now.Add(time.Second), SourceNodeID: "node-b"},
			want:    false,
		},
		{
			name:    "apply newer timestamp on same version",
			current: &store.HAEntityVersion{Version: 3, UpdatedAt: now, UpdatedByNodeID: "node-a"},
			op:      &store.HASyncOp{EntityVersion: 3, OccurredAt: now.Add(time.Second), SourceNodeID: "node-b"},
			want:    true,
		},
		{
			name:    "ignore older timestamp on same version",
			current: &store.HAEntityVersion{Version: 3, UpdatedAt: now, UpdatedByNodeID: "node-a"},
			op:      &store.HASyncOp{EntityVersion: 3, OccurredAt: now.Add(-time.Second), SourceNodeID: "node-b"},
			want:    false,
		},
		{
			name:    "use node id tie breaker when timestamp matches",
			current: &store.HAEntityVersion{Version: 3, UpdatedAt: now, UpdatedByNodeID: "node-a"},
			op:      &store.HASyncOp{EntityVersion: 3, OccurredAt: now, SourceNodeID: "node-z"},
			want:    true,
		},
		{
			name:    "keep current when tie breaker loses",
			current: &store.HAEntityVersion{Version: 3, UpdatedAt: now, UpdatedByNodeID: "node-z"},
			op:      &store.HASyncOp{EntityVersion: 3, OccurredAt: now, SourceNodeID: "node-a"},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldApplyRemoteVersion(tt.current, tt.op); got != tt.want {
				t.Fatalf("shouldApplyRemoteVersion() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAppendOpSerializesLocalEntityVersions(t *testing.T) {
	opsRepo := &fakeHASyncOpRepo{}
	versionsRepo := &fakeHAEntityVersionRepo{
		items: make(map[string]*store.HAEntityVersion),
		delay: 2 * time.Millisecond,
	}
	svc := &Service{
		nodeID:   "hc-1",
		ops:      opsRepo,
		versions: versionsRepo,
	}

	const writers = 16
	var wg sync.WaitGroup
	errCh := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := svc.AppendUpsert(context.Background(), EntityNewsArticle, "news-1", map[string]any{"i": i}, time.Now().UTC().Add(time.Duration(i)*time.Millisecond)); err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("AppendUpsert() error = %v", err)
		}
	}

	if len(opsRepo.ops) != writers {
		t.Fatalf("ops len = %d, want %d", len(opsRepo.ops), writers)
	}
	versions := make([]int, 0, len(opsRepo.ops))
	for _, op := range opsRepo.ops {
		versions = append(versions, int(op.EntityVersion))
	}
	sort.Ints(versions)
	for i, version := range versions {
		want := i + 1
		if version != want {
			t.Fatalf("sorted version[%d] = %d, want %d; all=%v", i, version, want, versions)
		}
	}
}

func TestNewOpIDGeneratesUniqueValuesForRepeatedHistoricalTimestamp(t *testing.T) {
	occurredAt := time.Date(2026, 4, 8, 6, 38, 54, 0, time.UTC)
	first := newOpID("hc-1", EntityNewsArticle, "news-1", occurredAt)
	second := newOpID("hc-1", EntityNewsArticle, "news-1", occurredAt)
	if first == second {
		t.Fatalf("newOpID() produced duplicate ids for identical historical input: %q", first)
	}
}

func TestComputeQualityKeepsHealthyWhenClusterIsReachableWithoutBacklog(t *testing.T) {
	score, status, routable := computeQuality(3, 2, 108, 0, false)
	if score != 95 {
		t.Fatalf("score = %d, want 95", score)
	}
	if status != "healthy" {
		t.Fatalf("status = %q, want healthy", status)
	}
	if !routable {
		t.Fatalf("routable = %v, want true", routable)
	}
}

func TestComputeQualityDegradesWhenLagAndBacklogAreBothHigh(t *testing.T) {
	score, status, routable := computeQuality(3, 2, 700, 150, false)
	if score != 60 {
		t.Fatalf("score = %d, want 60", score)
	}
	if status != "degraded" {
		t.Fatalf("status = %q, want degraded", status)
	}
	if !routable {
		t.Fatalf("routable = %v, want true", routable)
	}
}

func TestGetAdminStatusIncludesSyncCategoryDetails(t *testing.T) {
	now := time.Date(2026, 4, 25, 9, 0, 0, 0, time.UTC)
	opsRepo := &fakeHASyncOpRepo{ops: []*store.HASyncOp{
		{Seq: 5, EntityType: EntityHubDomainRoute, OccurredAt: now.Add(-5 * time.Minute)},
		{Seq: 9, EntityType: EntityGossipSnapshot, OccurredAt: now.Add(-2 * time.Minute)},
		{Seq: 10, EntityType: EntitySkillHubSnapshot, OccurredAt: now.Add(-90 * time.Second)},
		{Seq: 12, EntityType: EntitySkillMarketSnapshot, OccurredAt: now.Add(-time.Minute)},
	}}
	cursorTime := now.Add(-30 * time.Second)
	svc := &Service{
		nodeID:                   "hc-1",
		nodeName:                 "HubCenter 1",
		advertiseURL:             "https://hubs.mypapers.top",
		ops:                      opsRepo,
		cursors:                  &fakeHAPeerCursorRepo{items: map[string]*store.HAPeerCursor{"hc-2": {PeerNodeID: "hc-2", LastPulledSeq: 12, LastSuccessAt: &cursorTime}, "hc-3": {PeerNodeID: "hc-3", LastPulledSeq: 8, LastSuccessAt: &cursorTime, LastError: "pull failed"}}},
		heartbeatSyncMinInterval: 10 * time.Second,
		peers: map[string]*PeerRuntimeState{
			"hc-2": {NodeID: "hc-2", NodeName: "HubCenter 2", BaseURL: "https://hubs.maclaw.top", Reachable: true, QualityScore: 95, ServiceStatus: "healthy", ClusterStatus: "healthy", LastSuccessAt: &now},
			"hc-3": {NodeID: "hc-3", NodeName: "HubCenter 3", BaseURL: "https://hubs2.maclaw.top", Reachable: true, QualityScore: 90, ServiceStatus: "healthy", ClusterStatus: "healthy", LastSuccessAt: &now},
		},
	}

	status, err := svc.GetAdminStatus(context.Background())
	if err != nil {
		t.Fatalf("GetAdminStatus() error = %v", err)
	}
	if len(status.Sync.Details) != 6 {
		t.Fatalf("sync details len = %d, want 6", len(status.Sync.Details))
	}

	find := func(key string) *AdminSyncCategoryView {
		for i := range status.Sync.Details {
			if status.Sync.Details[i].Key == key {
				return &status.Sync.Details[i]
			}
		}
		return nil
	}

	routing := find("routing")
	if routing == nil || routing.Status != "error" || routing.LastOpSeq != 5 || routing.ErrorPeers != 1 {
		t.Fatalf("routing detail = %#v", routing)
	}
	gossip := find("gossip")
	if gossip == nil || gossip.Status != "error" || gossip.PendingOps != 1 || gossip.PendingPeers != 1 {
		t.Fatalf("gossip detail = %#v", gossip)
	}
	skillhub := find("skillhub")
	if skillhub == nil || skillhub.Status != "error" || skillhub.PendingOps != 2 {
		t.Fatalf("skillhub detail = %#v", skillhub)
	}
	skillmarket := find("skillmarket")
	if skillmarket == nil || skillmarket.Status != "error" || skillmarket.PendingOps != 4 {
		t.Fatalf("skillmarket detail = %#v", skillmarket)
	}
	system := find("system")
	if system == nil || system.Status != "idle" {
		t.Fatalf("system detail = %#v", system)
	}
}

func TestBuildAdminSyncDetailsMarksNeedsSeedWhenLocalDataExistsWithoutOps(t *testing.T) {
	details := buildAdminSyncDetails(nil, []AdminPeerView{{NodeID: "hc-2", NodeName: "HubCenter 2"}, {NodeID: "hc-3", NodeName: "HubCenter 3"}}, map[string]int64{"gossip": 12, "skillhub": 5, "skillmarket": 8, "news": 3})
	find := func(key string) *AdminSyncCategoryView {
		for i := range details {
			if details[i].Key == key {
				return &details[i]
			}
		}
		return nil
	}
	for _, key := range []string{"gossip", "skillhub", "skillmarket", "news"} {
		item := find(key)
		if item == nil {
			t.Fatalf("missing detail for %s", key)
		}
		if item.Status != "needs_seed" {
			t.Fatalf("%s status = %q, want needs_seed", key, item.Status)
		}
		if item.LocalRecords == 0 {
			t.Fatalf("%s local_records = 0, want > 0", key)
		}
		for _, peer := range item.Peers {
			if peer.Status != "needs_seed" {
				t.Fatalf("%s peer %s status = %q, want needs_seed", key, peer.NodeID, peer.Status)
			}
		}
	}
}
