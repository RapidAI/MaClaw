package ha

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

type fakeHASyncOpRepo struct {
	mu        sync.Mutex
	ops       []*store.HASyncOp
	remoteOps []*store.HASyncOp
	applied   map[string]bool
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

func (r *fakeHASyncOpRepo) HasApplied(_ context.Context, opID string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.applied != nil && r.applied[opID], nil
}

func (r *fakeHASyncOpRepo) MarkApplied(_ context.Context, item *store.HAAppliedOp) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.applied == nil {
		r.applied = map[string]bool{}
	}
	r.applied[item.OpID] = true
	return nil
}

func (r *fakeHASyncOpRepo) AppendRemoteIfMissing(_ context.Context, op *store.HASyncOp) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.remoteOps {
		if existing.OpID == op.OpID {
			return nil
		}
	}
	cp := *op
	r.remoteOps = append(r.remoteOps, &cp)
	return nil
}

type fakeHAEntityVersionRepo struct {
	mu    sync.Mutex
	items map[string]*store.HAEntityVersion
	delay time.Duration
}

type fakeHAPeerCursorRepo struct {
	items map[string]*store.HAPeerCursor
}

type fakeHANewsRepo struct {
	items map[string]*store.NewsArticle
}

func (r *fakeHANewsRepo) Create(_ context.Context, article *store.NewsArticle) error {
	if r.items == nil {
		r.items = map[string]*store.NewsArticle{}
	}
	cp := *article
	r.items[article.ID] = &cp
	return nil
}

func (r *fakeHANewsRepo) Update(_ context.Context, article *store.NewsArticle) error {
	return r.Create(context.Background(), article)
}

func (r *fakeHANewsRepo) Delete(_ context.Context, id string) error {
	delete(r.items, id)
	return nil
}

func (r *fakeHANewsRepo) GetByID(_ context.Context, id string) (*store.NewsArticle, error) {
	if r.items == nil {
		return nil, nil
	}
	item := r.items[id]
	if item == nil {
		return nil, nil
	}
	cp := *item
	return &cp, nil
}

func (r *fakeHANewsRepo) List(_ context.Context, _, _ int) ([]*store.NewsArticle, int, error) {
	return nil, 0, nil
}

func (r *fakeHANewsRepo) ListLatest(_ context.Context, _ int) ([]*store.NewsArticle, error) {
	return nil, nil
}

func (r *fakeHANewsRepo) CountPinned(_ context.Context) (int, error) {
	return 0, nil
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

func TestApplyRemoteOpRecordsRemoteOpForTransitiveSync(t *testing.T) {
	opsRepo := &fakeHASyncOpRepo{}
	versionsRepo := &fakeHAEntityVersionRepo{items: make(map[string]*store.HAEntityVersion)}
	svc := &Service{
		nodeID:   "hc-1",
		ops:      opsRepo,
		versions: versionsRepo,
		news:     &fakeHANewsRepo{},
	}
	payload := `{"id":"news-1","title":"hello","content":"","category":"notice","pinned":false,"created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-10T00:00:00Z"}`
	op := &store.HASyncOp{
		OpID:          "op-remote",
		SourceNodeID:  "hc-2",
		EntityType:    EntityNewsArticle,
		EntityID:      "news-1",
		OpType:        OpUpsert,
		EntityVersion: 1,
		OccurredAt:    time.Now().UTC(),
		PayloadJSON:   payload,
		PayloadHash:   testPayloadHash(payload),
	}

	if err := svc.ApplyRemoteOp(context.Background(), op); err != nil {
		t.Fatalf("ApplyRemoteOp() error = %v", err)
	}
	if len(opsRepo.remoteOps) != 1 || opsRepo.remoteOps[0].OpID != op.OpID {
		t.Fatalf("remoteOps = %+v", opsRepo.remoteOps)
	}
	applied, err := opsRepo.HasApplied(context.Background(), op.OpID)
	if err != nil {
		t.Fatalf("HasApplied() error = %v", err)
	}
	if !applied {
		t.Fatal("remote op was not marked applied")
	}
}

func TestApplyRemoteOpRejectsInvalidHashBeforeRecording(t *testing.T) {
	opsRepo := &fakeHASyncOpRepo{}
	svc := &Service{
		nodeID:   "hc-1",
		ops:      opsRepo,
		versions: &fakeHAEntityVersionRepo{items: make(map[string]*store.HAEntityVersion)},
		news:     &fakeHANewsRepo{},
	}
	op := &store.HASyncOp{
		OpID:          "op-bad-hash",
		SourceNodeID:  "hc-2",
		EntityType:    EntityNewsArticle,
		EntityID:      "news-1",
		OpType:        OpUpsert,
		EntityVersion: 1,
		OccurredAt:    time.Now().UTC(),
		PayloadJSON:   `{"id":"news-1"}`,
		PayloadHash:   "not-the-hash",
	}

	if err := svc.ApplyRemoteOp(context.Background(), op); err == nil {
		t.Fatal("ApplyRemoteOp() succeeded with invalid payload hash")
	}
	if len(opsRepo.remoteOps) != 0 {
		t.Fatalf("invalid remote op was recorded: %+v", opsRepo.remoteOps)
	}
}

func TestApplyRemoteOpsValidatesBatchBeforeApplying(t *testing.T) {
	opsRepo := &fakeHASyncOpRepo{}
	svc := &Service{
		nodeID:   "hc-1",
		ops:      opsRepo,
		versions: &fakeHAEntityVersionRepo{items: make(map[string]*store.HAEntityVersion)},
		news:     &fakeHANewsRepo{},
	}
	goodPayload := `{"id":"news-1","title":"hello","content":"","category":"notice","pinned":false,"created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-10T00:00:00Z"}`
	badPayload := `{"id":"news-2"}`
	ops := []*store.HASyncOp{
		{
			OpID:          "op-good",
			SourceNodeID:  "hc-2",
			EntityType:    EntityNewsArticle,
			EntityID:      "news-1",
			OpType:        OpUpsert,
			EntityVersion: 1,
			OccurredAt:    time.Now().UTC(),
			PayloadJSON:   goodPayload,
			PayloadHash:   testPayloadHash(goodPayload),
		},
		{
			OpID:          "op-bad",
			SourceNodeID:  "hc-2",
			EntityType:    EntityNewsArticle,
			EntityID:      "news-2",
			OpType:        OpUpsert,
			EntityVersion: 1,
			OccurredAt:    time.Now().UTC(),
			PayloadJSON:   badPayload,
			PayloadHash:   "bad-hash",
		},
	}

	if err := svc.ApplyRemoteOps(context.Background(), ops); err == nil {
		t.Fatal("ApplyRemoteOps() succeeded with invalid op in batch")
	}
	if len(opsRepo.remoteOps) != 0 {
		t.Fatalf("batch was partially recorded before validation failed: %+v", opsRepo.remoteOps)
	}
	if _, err := opsRepo.HasApplied(context.Background(), "op-good"); err != nil {
		t.Fatalf("HasApplied() error = %v", err)
	} else if opsRepo.applied["op-good"] {
		t.Fatal("batch was partially applied before validation failed")
	}
}

func TestApplyRemoteOpsRejectsNilOp(t *testing.T) {
	svc := NewService("node-a", "Node A", "", "", nil)
	err := svc.ApplyRemoteOps(context.Background(), []*store.HASyncOp{nil})
	var invalid InvalidRemoteOpError
	if !errors.As(err, &invalid) {
		t.Fatalf("ApplyRemoteOps() error = %v, want InvalidRemoteOpError", err)
	}
}

func TestMarkPeerPushDeferredDoesNotIsolateHealthyPeer(t *testing.T) {
	svc := NewService("hc-1", "hc-1", "", "secret", []StaticPeer{{NodeID: "hc-2", NodeName: "hc-2", BaseURL: "http://hc-2"}})
	svc.updatePeerSync("hc-2", 0)

	svc.markPeerPushDeferred("hc-2", "push queue saturated")

	peers := svc.listPeerStates()
	if len(peers) != 1 {
		t.Fatalf("peers len = %d, want 1", len(peers))
	}
	peer := peers[0]
	if !peer.Reachable {
		t.Fatalf("peer was marked unreachable: %+v", peer)
	}
	if peer.ServiceStatus == "isolated" || peer.ClusterStatus == "isolated" {
		t.Fatalf("peer was isolated after deferred push: %+v", peer)
	}
	if peer.Backlog < 1 || peer.LastError == "" {
		t.Fatalf("peer deferred push state not recorded: %+v", peer)
	}
}

func TestTakePeerPushBatchCoalescesQueuedOps(t *testing.T) {
	svc := NewService("hc-1", "hc-1", "", "secret", []StaticPeer{{NodeID: "hc-2", NodeName: "hc-2", BaseURL: "http://hc-2"}})
	svc.pushMu.Lock()
	for i := 0; i < 3; i++ {
		svc.pushPending["hc-2"] = append(svc.pushPending["hc-2"], &store.HASyncOp{OpID: fmt.Sprintf("op-%d", i)})
	}
	svc.pushRunning["hc-2"] = true
	svc.pushMu.Unlock()

	ops := svc.takePeerPushBatch("hc-2", 2)
	if len(ops) != 2 {
		t.Fatalf("first batch len = %d, want 2", len(ops))
	}
	ops = svc.takePeerPushBatch("hc-2", 2)
	if len(ops) != 1 {
		t.Fatalf("second batch len = %d, want 1", len(ops))
	}
	ops = svc.takePeerPushBatch("hc-2", 2)
	if len(ops) != 0 {
		t.Fatalf("final batch len = %d, want 0", len(ops))
	}
	svc.pushMu.Lock()
	running := svc.pushRunning["hc-2"]
	svc.pushMu.Unlock()
	if running {
		t.Fatal("push queue still marked running after draining")
	}
}

func TestEnqueuePushOpTrimsPeerQueueUnderBackpressure(t *testing.T) {
	svc := NewService("hc-1", "hc-1", "", "secret", []StaticPeer{{NodeID: "hc-2", NodeName: "hc-2", BaseURL: "http://hc-2"}})
	peer := svc.listPeerStates()[0]
	svc.pushMu.Lock()
	svc.pushRunning["hc-2"] = true
	for i := 0; i < maxPendingPushOpsPerPeer; i++ {
		svc.pushPending["hc-2"] = append(svc.pushPending["hc-2"], &store.HASyncOp{OpID: fmt.Sprintf("old-%d", i)})
	}
	svc.pushMu.Unlock()

	svc.enqueuePushOp(peer, &store.HASyncOp{OpID: "newest"})

	svc.pushMu.Lock()
	pending := append([]*store.HASyncOp(nil), svc.pushPending["hc-2"]...)
	svc.pushMu.Unlock()
	if len(pending) != maxPendingPushOpsPerPeer {
		t.Fatalf("pending len = %d, want %d", len(pending), maxPendingPushOpsPerPeer)
	}
	if pending[len(pending)-1].OpID != "newest" {
		t.Fatalf("newest op was not retained: last=%q", pending[len(pending)-1].OpID)
	}
	peers := svc.listPeerStates()
	if peers[0].Backlog < 1 || peers[0].LastError == "" {
		t.Fatalf("peer deferred state not recorded after trim: %+v", peers[0])
	}
}

func TestEnqueuePushOpCoalescesSameEntityToLatestVersion(t *testing.T) {
	svc := NewService("hc-1", "hc-1", "", "secret", []StaticPeer{{NodeID: "hc-2", NodeName: "hc-2", BaseURL: "http://hc-2"}})
	peer := svc.listPeerStates()[0]
	now := time.Now().UTC()
	svc.pushMu.Lock()
	svc.pushRunning["hc-2"] = true
	svc.pushMu.Unlock()

	svc.enqueuePushOp(peer, &store.HASyncOp{OpID: "old", EntityType: EntitySkillMarketSnapshot, EntityID: "skillmarket", EntityVersion: 1, OccurredAt: now})
	svc.enqueuePushOp(peer, &store.HASyncOp{OpID: "new", EntityType: EntitySkillMarketSnapshot, EntityID: "skillmarket", EntityVersion: 2, OccurredAt: now.Add(time.Second)})
	svc.enqueuePushOp(peer, &store.HASyncOp{OpID: "other", EntityType: EntityNewsArticle, EntityID: "news-1", EntityVersion: 1, OccurredAt: now})

	svc.pushMu.Lock()
	pending := append([]*store.HASyncOp(nil), svc.pushPending["hc-2"]...)
	svc.pushMu.Unlock()
	if len(pending) != 2 {
		t.Fatalf("pending len = %d, want 2 coalesced ops", len(pending))
	}
	if pending[0].OpID != "new" {
		t.Fatalf("same entity op was not coalesced to latest: %+v", pending[0])
	}
}

func TestUpdatePeerPushSuccessPreservesDeferredBacklog(t *testing.T) {
	svc := NewService("hc-1", "hc-1", "", "secret", []StaticPeer{{NodeID: "hc-2", NodeName: "hc-2", BaseURL: "http://hc-2"}})
	svc.markPeerPushDeferred("hc-2", "push queue trimmed; waiting for pull sync")

	svc.updatePeerPushSuccess("hc-2")

	peer := svc.listPeerStates()[0]
	if !peer.Reachable {
		t.Fatalf("peer reachable = false after push success: %+v", peer)
	}
	if peer.Backlog < 1 {
		t.Fatalf("peer backlog was cleared by push success: %+v", peer)
	}
	if peer.LastError == "" {
		t.Fatalf("peer last error was cleared before pull compensation: %+v", peer)
	}

	svc.updatePeerSync("hc-2", 0)
	peer = svc.listPeerStates()[0]
	if peer.Backlog != 0 || peer.LastError != "" {
		t.Fatalf("pull sync did not clear deferred backlog: %+v", peer)
	}
}

func testPayloadHash(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
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
	if gossip == nil || gossip.Status != "error" || gossip.PendingOps != 0 || gossip.PendingPeers != 0 {
		t.Fatalf("gossip detail = %#v", gossip)
	}
	skillhub := find("skillhub")
	if skillhub == nil || skillhub.Status != "error" || skillhub.PendingOps != 0 {
		t.Fatalf("skillhub detail = %#v", skillhub)
	}
	skillmarket := find("skillmarket")
	if skillmarket == nil || skillmarket.Status != "error" || skillmarket.PendingOps != 0 {
		t.Fatalf("skillmarket detail = %#v", skillmarket)
	}
	system := find("system")
	if system == nil || system.Status != "idle" {
		t.Fatalf("system detail = %#v", system)
	}
}

func TestBuildAdminSyncDetailsDoesNotUseInboundCursorAsOutboundPending(t *testing.T) {
	now := time.Date(2026, 5, 11, 2, 30, 0, 0, time.UTC)
	details := buildAdminSyncDetails(
		[]*store.HASyncOp{{Seq: 70076112, EntityType: EntityHubDomainRoute, OccurredAt: now}},
		[]AdminPeerView{
			{NodeID: "hubcenter-1", NodeName: "hubcenter-1", CursorLastPulledSeq: 3403577},
			{NodeID: "hubcenter-3", NodeName: "hubcenter-3", CursorLastPulledSeq: 5724077},
		},
		map[string]int64{"routing": 281},
	)
	var routing *AdminSyncCategoryView
	for i := range details {
		if details[i].Key == "routing" {
			routing = &details[i]
			break
		}
	}
	if routing == nil {
		t.Fatal("missing routing detail")
	}
	if routing.Status != "healthy" || routing.PendingOps != 0 || routing.PendingPeers != 0 {
		t.Fatalf("routing detail = %#v, want healthy without false pending from inbound cursor", routing)
	}
	for _, peer := range routing.Peers {
		if peer.Status != "synced" || peer.PendingOps != 0 {
			t.Fatalf("peer detail = %#v, want synced without false pending", peer)
		}
	}
}

func TestBuildAdminSyncDetailsDoesNotSpreadGlobalBacklogAcrossCategories(t *testing.T) {
	now := time.Date(2026, 5, 11, 2, 30, 0, 0, time.UTC)
	details := buildAdminSyncDetails(
		[]*store.HASyncOp{{Seq: 70076112, EntityType: EntitySkillMarketSnapshot, OccurredAt: now}},
		[]AdminPeerView{{NodeID: "hubcenter-3", NodeName: "hubcenter-3", CursorLastPulledSeq: 5724077, Backlog: 42}},
		map[string]int64{"skillmarket": 14},
	)
	var skillmarket *AdminSyncCategoryView
	for i := range details {
		if details[i].Key == "skillmarket" {
			skillmarket = &details[i]
			break
		}
	}
	if skillmarket == nil {
		t.Fatal("missing skillmarket detail")
	}
	if skillmarket.Status != "healthy" || skillmarket.PendingOps != 0 || skillmarket.PendingPeers != 0 {
		t.Fatalf("skillmarket detail = %#v, want healthy without category pending from global backlog", skillmarket)
	}
	if len(skillmarket.Peers) != 1 || skillmarket.Peers[0].Status != "synced" || skillmarket.Peers[0].PendingOps != 0 {
		t.Fatalf("skillmarket peer detail = %#v", skillmarket.Peers)
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
