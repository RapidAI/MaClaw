package ha

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	corecardstore "github.com/RapidAI/CodeClaw/corelib/cardstore"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/cardstore"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/notification"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skill"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

type fakeHASyncOpRepo struct {
	mu             sync.Mutex
	ops            []*store.HASyncOp
	remoteOps      []*store.HASyncOp
	applied        map[string]bool
	listCalls      int
	listLimits     []int
	onAppendRemote func(*store.HASyncOp) error
}

func TestNewServiceSkipsSelfPeer(t *testing.T) {
	svc := NewService("hc-1", "hc-1", "https://hc-1.example.com", "secret", []StaticPeer{
		{NodeID: "hc-1", NodeName: "self", BaseURL: "https://hc-1.example.com"},
		{NodeID: "hc-2", NodeName: "peer", BaseURL: "https://hc-2.example.com"},
	})

	peers := svc.listPeerStates()
	if len(peers) != 1 {
		t.Fatalf("peers len = %d, want 1", len(peers))
	}
	if peers[0].NodeID != "hc-2" {
		t.Fatalf("peer NodeID = %q, want hc-2", peers[0].NodeID)
	}
}

func TestForceBroadcastSkillHubSnapshotBypassesHashDedup(t *testing.T) {
	skillStore := skill.NewSkillStore(t.TempDir())
	opsRepo := &fakeHASyncOpRepo{}
	versionsRepo := &fakeHAEntityVersionRepo{items: map[string]*store.HAEntityVersion{}}
	svc := &Service{nodeID: "hc-1", ops: opsRepo, versions: versionsRepo}
	svc.AttachSkillStore(skillStore)

	if err := skillStore.Publish(skill.HubSkillFull{
		HubSkillMeta: skill.HubSkillMeta{
			ID:          "codex-restore",
			Name:        "CodexRestore",
			Description: "restore codex sessions",
			Version:     "1.0.0",
			TrustLevel:  "trusted",
			Visible:     true,
		},
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	// Drain any async emitSync from Publish so force-broadcast counts are stable.
	time.Sleep(50 * time.Millisecond)
	before := countEntityOps(opsRepo, EntitySkillHubSnapshot)

	n1, err := svc.ForceBroadcastSkillHubSnapshot(context.Background())
	if err != nil {
		t.Fatalf("ForceBroadcastSkillHubSnapshot #1: %v", err)
	}
	if n1 != 1 {
		t.Fatalf("skillCount = %d, want 1", n1)
	}
	n2, err := svc.ForceBroadcastSkillHubSnapshot(context.Background())
	if err != nil {
		t.Fatalf("ForceBroadcastSkillHubSnapshot #2: %v", err)
	}
	if n2 != 1 {
		t.Fatalf("skillCount = %d, want 1", n2)
	}
	after := countEntityOps(opsRepo, EntitySkillHubSnapshot)
	if after-before < 2 {
		t.Fatalf("skillhub ops delta = %d, want >= 2 (force broadcast must bypass hash dedup)", after-before)
	}
}

func TestForceBroadcastSkillHubSnapshotRefusesEmptyCatalog(t *testing.T) {
	skillStore := skill.NewSkillStore(t.TempDir())
	opsRepo := &fakeHASyncOpRepo{}
	versionsRepo := &fakeHAEntityVersionRepo{items: map[string]*store.HAEntityVersion{}}
	svc := &Service{nodeID: "hc-1", ops: opsRepo, versions: versionsRepo}
	svc.AttachSkillStore(skillStore)

	if _, err := svc.ForceBroadcastSkillHubSnapshot(context.Background()); err == nil {
		t.Fatal("expected error refusing empty skillhub force broadcast")
	}
	if countEntityOps(opsRepo, EntitySkillHubSnapshot) != 0 {
		t.Fatalf("empty force broadcast must not append ops")
	}
}

func countEntityOps(repo *fakeHASyncOpRepo, entityType string) int {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	n := 0
	for _, op := range repo.ops {
		if op.EntityType == entityType {
			n++
		}
	}
	return n
}

type fakeNotificationStore struct {
	items map[string]*notification.Notification
}

func (s *fakeNotificationStore) Create(ctx context.Context, n *notification.Notification) error {
	return s.Upsert(ctx, n)
}

func (s *fakeNotificationStore) Upsert(_ context.Context, n *notification.Notification) error {
	if s.items == nil {
		s.items = map[string]*notification.Notification{}
	}
	cp := *n
	s.items[n.ID] = &cp
	return nil
}

func (s *fakeNotificationStore) GetByID(_ context.Context, id string) (*notification.Notification, error) {
	if item := s.items[id]; item != nil {
		cp := *item
		return &cp, nil
	}
	return nil, nil
}

func (s *fakeNotificationStore) List(context.Context, notification.ListFilter) ([]*notification.Notification, int, error) {
	items := make([]*notification.Notification, 0, len(s.items))
	for _, item := range s.items {
		cp := *item
		items = append(items, &cp)
	}
	return items, len(items), nil
}

func (s *fakeNotificationStore) UpdateStatus(_ context.Context, id string, status notification.Status, updatedAt time.Time) error {
	if item := s.items[id]; item != nil {
		item.Status = status
		item.UpdatedAt = updatedAt
		return nil
	}
	return sql.ErrNoRows
}

func (s *fakeNotificationStore) Delete(_ context.Context, id string) error {
	if _, ok := s.items[id]; !ok {
		return sql.ErrNoRows
	}
	delete(s.items, id)
	return nil
}

func (s *fakeNotificationStore) RecordCascadeResult(context.Context, *notification.CascadeResult) error {
	return nil
}

func (s *fakeNotificationStore) GetCascadeResults(context.Context, string) ([]*notification.CascadeResult, error) {
	return nil, nil
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
	r.listCalls++
	r.listLimits = append(r.listLimits, limit)
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

func (r *fakeHASyncOpRepo) ListStats() (int, []int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.listCalls, append([]int(nil), r.listLimits...)
}

func (r *fakeHASyncOpRepo) GetMaxSeq(_ context.Context) (int64, error) { return 0, nil }

func TestApplyNotificationOpUpsertsAndDeletesIdempotently(t *testing.T) {
	ctx := context.Background()
	notifications := &fakeNotificationStore{}
	svc := &Service{notifications: notifications}
	now := time.Date(2026, 7, 3, 9, 30, 0, 0, time.UTC)
	notif := &notification.Notification{
		ID:           "notif-1",
		Title:        "HA notice",
		Content:      "Replicate me.",
		Category:     notification.CategorySystemAnnouncement,
		Priority:     notification.PriorityNormal,
		AudienceType: notification.AudienceAll,
		Status:       notification.StatusPublished,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	payloadJSON, payloadHash, err := marshalPayload(notif)
	if err != nil {
		t.Fatalf("marshalPayload notification: %v", err)
	}

	if err := svc.applyNotificationOp(ctx, &store.HASyncOp{OpType: OpUpsert, PayloadJSON: payloadJSON, PayloadHash: payloadHash}); err != nil {
		t.Fatalf("apply upsert: %v", err)
	}
	if got, _ := notifications.GetByID(ctx, notif.ID); got == nil || got.Title != notif.Title {
		t.Fatalf("notification after upsert = %#v", got)
	}

	deleteJSON, deleteHash, err := marshalPayload(map[string]string{"id": notif.ID})
	if err != nil {
		t.Fatalf("marshalPayload delete: %v", err)
	}
	deleteOp := &store.HASyncOp{OpType: OpDelete, PayloadJSON: deleteJSON, PayloadHash: deleteHash}
	if err := svc.applyNotificationOp(ctx, deleteOp); err != nil {
		t.Fatalf("apply delete: %v", err)
	}
	if got, _ := notifications.GetByID(ctx, notif.ID); got != nil {
		t.Fatalf("notification after delete = %#v, want nil", got)
	}
	if err := svc.applyNotificationOp(ctx, deleteOp); err != nil {
		t.Fatalf("apply missing delete: %v", err)
	}
}

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
	if r.onAppendRemote != nil {
		return r.onAppendRemote(&cp)
	}
	return nil
}

type fakeHAEntityVersionRepo struct {
	mu    sync.Mutex
	items map[string]*store.HAEntityVersion
	delay time.Duration
}

type fakeHAPeerCursorRepo struct {
	mu    sync.Mutex
	items map[string]*store.HAPeerCursor
}

type fakeHANewsRepo struct {
	items     map[string]*store.NewsArticle
	createErr error
}

type fakeHACardOrderRepo struct {
	items map[string]*cardstore.PurchaseOrder
}

type fakeRouteSnapshotRefresher struct {
	mu    sync.Mutex
	calls int
}

func (r *fakeRouteSnapshotRefresher) Rebuild(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	return nil
}

func (r *fakeRouteSnapshotRefresher) Calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func (r *fakeHANewsRepo) Create(_ context.Context, article *store.NewsArticle) error {
	if r.createErr != nil {
		return r.createErr
	}
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

func (r *fakeHACardOrderRepo) Create(_ context.Context, order *cardstore.PurchaseOrder) error {
	if r.items == nil {
		r.items = map[string]*cardstore.PurchaseOrder{}
	}
	cp := *order
	r.items[order.OrderNo] = &cp
	return nil
}

func (r *fakeHACardOrderRepo) GetByOrderNo(_ context.Context, orderNo string) (*cardstore.PurchaseOrder, error) {
	if r.items == nil {
		return nil, nil
	}
	item := r.items[orderNo]
	if item == nil {
		return nil, nil
	}
	cp := *item
	return &cp, nil
}

func (r *fakeHACardOrderRepo) List(_ context.Context, _ cardstore.OrderFilter) ([]*cardstore.PurchaseOrder, int, error) {
	return nil, 0, nil
}

func (r *fakeHACardOrderRepo) UpdateStatus(_ context.Context, orderNo, status string, now time.Time) error {
	item, _ := r.GetByOrderNo(context.Background(), orderNo)
	if item == nil {
		return nil
	}
	item.Status = status
	item.UpdatedAt = now
	r.items[orderNo] = item
	return nil
}

func (r *fakeHACardOrderRepo) Update(_ context.Context, order *cardstore.PurchaseOrder) error {
	return r.Create(context.Background(), order)
}

func (r *fakeHACardOrderRepo) Delete(_ context.Context, orderNo string) error {
	delete(r.items, orderNo)
	return nil
}

func (r *fakeHACardOrderRepo) Archive(_ context.Context, orderNo string, archivedAt time.Time) error {
	item, _ := r.GetByOrderNo(context.Background(), orderNo)
	if item == nil {
		return nil
	}
	item.ArchivedAt = archivedAt.Format(time.RFC3339)
	item.UpdatedAt = archivedAt
	r.items[orderNo] = item
	return nil
}

func (r *fakeHACardOrderRepo) Unarchive(_ context.Context, orderNo string, now time.Time) error {
	item, _ := r.GetByOrderNo(context.Background(), orderNo)
	if item == nil {
		return nil
	}
	item.ArchivedAt = ""
	item.UpdatedAt = now
	r.items[orderNo] = item
	return nil
}

func (r *fakeHAPeerCursorRepo) Get(_ context.Context, peerNodeID string) (*store.HAPeerCursor, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.mu.Lock()
	defer r.mu.Unlock()
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
			if err := svc.AppendUpsert(context.Background(), EntityNewsArticle, "news-1", map[string]any{"id": "news-1", "i": i}, time.Now().UTC().Add(time.Duration(i)*time.Millisecond)); err != nil {
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

func TestAppendUpsertRejectsMissingLocalPayloadID(t *testing.T) {
	opsRepo := &fakeHASyncOpRepo{}
	versionsRepo := &fakeHAEntityVersionRepo{items: make(map[string]*store.HAEntityVersion)}
	svc := &Service{nodeID: "hc-1", ops: opsRepo, versions: versionsRepo}

	err := svc.AppendUpsert(context.Background(), EntityNewsArticle, "news-1", map[string]any{"title": "missing id"}, time.Now().UTC())
	var invalid InvalidRemoteOpError
	if !errors.As(err, &invalid) {
		t.Fatalf("AppendUpsert() error = %v, want InvalidRemoteOpError", err)
	}
	if len(opsRepo.ops) != 0 {
		t.Fatalf("invalid local op was appended: %+v", opsRepo.ops)
	}
}

func TestAppendLLMCardOrderOmitsDerivedAuthorizationFields(t *testing.T) {
	opsRepo := &fakeHASyncOpRepo{}
	versionsRepo := &fakeHAEntityVersionRepo{items: make(map[string]*store.HAEntityVersion)}
	svc := &Service{nodeID: "hc-1", ops: opsRepo, versions: versionsRepo}
	now := time.Date(2026, 6, 19, 14, 0, 0, 0, time.UTC)
	used := 120.0
	remaining := 880.0
	order := &cardstore.PurchaseOrder{
		Order: corecardstore.Order{
			OrderNo:   "HC-1",
			Email:     "owner@example.com",
			Status:    corecardstore.StatusActivated,
			CreatedAt: now,
			UpdatedAt: now,
		},
		HubID:                  "hub-1",
		TenantID:               "tenant-a",
		CardTypeID:             "card-month",
		ServiceGroupID:         "redeem",
		Credits:                1000,
		Period:                 "month",
		AuthorizationID:        "auth-1",
		AuthorizationStatus:    "active",
		AuthorizationStartsAt:  &now,
		AuthorizationExpiresAt: ptrTime(now.AddDate(0, 1, 0)),
		CreditsUsed:            &used,
		CreditsRemaining:       &remaining,
	}

	svc.AppendLLMCardOrder(context.Background(), order)

	if len(opsRepo.ops) != 1 {
		t.Fatalf("ops len = %d, want 1", len(opsRepo.ops))
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(opsRepo.ops[0].PayloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	for _, key := range []string{"authorization_id", "authorization_status", "authorization_starts_at", "authorization_expires_at", "credits_used", "credits_remaining"} {
		if _, ok := payload[key]; ok {
			t.Fatalf("payload includes derived field %q: %s", key, opsRepo.ops[0].PayloadJSON)
		}
	}
	if order.AuthorizationID == "" || order.CreditsUsed == nil {
		t.Fatalf("AppendLLMCardOrder mutated source order: %#v", order)
	}
}

func TestAppendLLMNodeBindingUsesStableLeaseIdentity(t *testing.T) {
	opsRepo := &fakeHASyncOpRepo{}
	versionsRepo := &fakeHAEntityVersionRepo{items: make(map[string]*store.HAEntityVersion)}
	svc := &Service{nodeID: "hc-1", ops: opsRepo, versions: versionsRepo}
	now := time.Date(2026, 7, 14, 3, 0, 0, 0, time.UTC)
	binding := &store.LLMNodeBinding{
		HubID:      "hub-1",
		TenantID:   "tenant-a",
		NodeID:     "hc-1",
		BoundAt:    now,
		LastActive: now,
		ExpiresAt:  now.Add(BindingLeaseTTL),
	}

	svc.AppendLLMNodeBinding(context.Background(), binding)

	if len(opsRepo.ops) != 1 {
		t.Fatalf("ops len = %d, want 1", len(opsRepo.ops))
	}
	op := opsRepo.ops[0]
	if op.EntityType != EntityLLMNodeBinding || op.EntityID != "hub-1\x00tenant-a" {
		t.Fatalf("binding op identity = %s/%q", op.EntityType, op.EntityID)
	}
	var payload store.LLMNodeBinding
	if err := json.Unmarshal([]byte(op.PayloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.NodeID != "hc-1" || payload.ExpiresAt.IsZero() {
		t.Fatalf("binding payload = %#v", payload)
	}
}

func ptrTime(t time.Time) *time.Time {
	return &t
}

func TestAppendDeleteRejectsUnsupportedLocalEntityOp(t *testing.T) {
	opsRepo := &fakeHASyncOpRepo{}
	versionsRepo := &fakeHAEntityVersionRepo{items: make(map[string]*store.HAEntityVersion)}
	svc := &Service{nodeID: "hc-1", ops: opsRepo, versions: versionsRepo}

	err := svc.AppendDelete(context.Background(), EntitySystemSetting, "admin_initialized", systemSettingPayload{Key: "admin_initialized"}, time.Now().UTC())
	var invalid InvalidRemoteOpError
	if !errors.As(err, &invalid) {
		t.Fatalf("AppendDelete() error = %v, want InvalidRemoteOpError", err)
	}
	if len(opsRepo.ops) != 0 {
		t.Fatalf("invalid local delete op was appended: %+v", opsRepo.ops)
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

func TestApplyRemoteOpStillAppliesWhenRecorderUpdatesEntityVersion(t *testing.T) {
	versionsRepo := &fakeHAEntityVersionRepo{items: make(map[string]*store.HAEntityVersion)}
	opsRepo := &fakeHASyncOpRepo{onAppendRemote: func(op *store.HASyncOp) error {
		return versionsRepo.Upsert(context.Background(), &store.HAEntityVersion{
			EntityType:      op.EntityType,
			EntityID:        op.EntityID,
			Version:         op.EntityVersion,
			UpdatedAt:       op.OccurredAt,
			UpdatedByNodeID: op.SourceNodeID,
		})
	}}
	newsRepo := &fakeHANewsRepo{}
	svc := &Service{
		nodeID:   "hc-1",
		ops:      opsRepo,
		versions: versionsRepo,
		news:     newsRepo,
	}
	payload := `{"id":"news-versioned","title":"hello","content":"","category":"notice","pinned":false,"created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-10T00:00:00Z"}`
	op := &store.HASyncOp{
		OpID:          "op-remote-versioned",
		SourceNodeID:  "hc-2",
		EntityType:    EntityNewsArticle,
		EntityID:      "news-versioned",
		OpType:        OpUpsert,
		EntityVersion: 3,
		OccurredAt:    time.Now().UTC(),
		PayloadJSON:   payload,
		PayloadHash:   testPayloadHash(payload),
	}

	if err := svc.ApplyRemoteOp(context.Background(), op); err != nil {
		t.Fatalf("ApplyRemoteOp() error = %v", err)
	}
	got, err := newsRepo.GetByID(context.Background(), "news-versioned")
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got == nil || got.Title != "hello" {
		t.Fatalf("news item = %+v, want applied remote payload", got)
	}
	applied, err := opsRepo.HasApplied(context.Background(), op.OpID)
	if err != nil {
		t.Fatalf("HasApplied() error = %v", err)
	}
	if !applied {
		t.Fatal("remote op was not marked applied")
	}
}

func TestApplyRemoteOpRetryAfterApplyFailureIsNotSkippedByRecordedVersion(t *testing.T) {
	versionsRepo := &fakeHAEntityVersionRepo{items: make(map[string]*store.HAEntityVersion)}
	opsRepo := &fakeHASyncOpRepo{}
	newsRepo := &fakeHANewsRepo{createErr: errors.New("transient write failure")}
	svc := &Service{
		nodeID:   "hc-1",
		ops:      opsRepo,
		versions: versionsRepo,
		news:     newsRepo,
	}
	payload := `{"id":"news-retry","title":"retry me","content":"","category":"notice","pinned":false,"created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-10T00:00:00Z"}`
	op := &store.HASyncOp{
		OpID:          "op-remote-retry",
		SourceNodeID:  "hc-2",
		EntityType:    EntityNewsArticle,
		EntityID:      "news-retry",
		OpType:        OpUpsert,
		EntityVersion: 3,
		OccurredAt:    time.Now().UTC(),
		PayloadJSON:   payload,
		PayloadHash:   testPayloadHash(payload),
	}

	if err := svc.ApplyRemoteOp(context.Background(), op); err == nil {
		t.Fatal("ApplyRemoteOp() succeeded, want transient failure")
	}
	if current, err := versionsRepo.Get(context.Background(), op.EntityType, op.EntityID); err != nil {
		t.Fatalf("version Get() error = %v", err)
	} else if current != nil {
		t.Fatalf("version advanced after failed apply: %+v", current)
	}
	if applied, err := opsRepo.HasApplied(context.Background(), op.OpID); err != nil {
		t.Fatalf("HasApplied() error = %v", err)
	} else if applied {
		t.Fatal("failed op was marked applied")
	}

	newsRepo.createErr = nil
	if err := svc.ApplyRemoteOp(context.Background(), op); err != nil {
		t.Fatalf("retry ApplyRemoteOp() error = %v", err)
	}
	got, err := newsRepo.GetByID(context.Background(), "news-retry")
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got == nil || got.Title != "retry me" {
		t.Fatalf("news item = %+v, want retry payload applied", got)
	}
}

func TestApplyRemoteLLMCardOrderUpsertAndDelete(t *testing.T) {
	opsRepo := &fakeHASyncOpRepo{}
	versionsRepo := &fakeHAEntityVersionRepo{items: make(map[string]*store.HAEntityVersion)}
	ordersRepo := &fakeHACardOrderRepo{}
	svc := &Service{
		nodeID:     "hc-1",
		ops:        opsRepo,
		versions:   versionsRepo,
		cardOrders: ordersRepo,
	}
	now := time.Now().UTC().Truncate(time.Second)
	order := &cardstore.PurchaseOrder{
		Order: corecardstore.Order{
			OrderNo:   "HC-REMOTE",
			ProductID: "ct-old",
			Email:     "owner@example.com",
			Amount:    10,
			Status:    corecardstore.StatusPending,
			CreatedAt: now,
			UpdatedAt: now,
		},
		HubID: "hub-1", TenantID: "tenant-a", CardTypeID: "ct-old", ServiceGroupID: "group-old", Credits: 100, Period: "month",
	}
	payloadBytes, err := json.Marshal(order)
	if err != nil {
		t.Fatalf("Marshal(order) error = %v", err)
	}
	upsert := &store.HASyncOp{
		OpID:          "op-card-order-upsert-1",
		SourceNodeID:  "hc-2",
		EntityType:    EntityLLMCardOrder,
		EntityID:      order.OrderNo,
		OpType:        OpUpsert,
		EntityVersion: 1,
		OccurredAt:    now,
		PayloadJSON:   string(payloadBytes),
		PayloadHash:   testPayloadHash(string(payloadBytes)),
	}
	if err := svc.ApplyRemoteOp(context.Background(), upsert); err != nil {
		t.Fatalf("ApplyRemoteOp(upsert create) error = %v", err)
	}
	got, err := ordersRepo.GetByOrderNo(context.Background(), order.OrderNo)
	if err != nil {
		t.Fatalf("GetByOrderNo() error = %v", err)
	}
	if got == nil || got.CardTypeID != "ct-old" || got.Credits != 100 {
		t.Fatalf("created order = %+v", got)
	}

	order.CardTypeID = "ct-new"
	order.ProductID = "ct-new"
	order.ServiceGroupID = "group-new"
	order.Credits = 200
	order.Amount = 25000
	order.UpdatedAt = now.Add(time.Minute)
	payloadBytes, err = json.Marshal(order)
	if err != nil {
		t.Fatalf("Marshal(updated order) error = %v", err)
	}
	upsert2 := &store.HASyncOp{
		OpID:          "op-card-order-upsert-2",
		SourceNodeID:  "hc-2",
		EntityType:    EntityLLMCardOrder,
		EntityID:      order.OrderNo,
		OpType:        OpUpsert,
		EntityVersion: 2,
		OccurredAt:    now.Add(time.Minute),
		PayloadJSON:   string(payloadBytes),
		PayloadHash:   testPayloadHash(string(payloadBytes)),
	}
	if err := svc.ApplyRemoteOp(context.Background(), upsert2); err != nil {
		t.Fatalf("ApplyRemoteOp(upsert update) error = %v", err)
	}
	got, err = ordersRepo.GetByOrderNo(context.Background(), order.OrderNo)
	if err != nil {
		t.Fatalf("GetByOrderNo(updated) error = %v", err)
	}
	if got.CardTypeID != "ct-new" || got.ServiceGroupID != "group-new" || got.Credits != 200 || got.Amount != 25000 {
		t.Fatalf("updated order = %+v", got)
	}

	deletePayload := `{"order_no":"HC-REMOTE"}`
	del := &store.HASyncOp{
		OpID:          "op-card-order-delete",
		SourceNodeID:  "hc-2",
		EntityType:    EntityLLMCardOrder,
		EntityID:      order.OrderNo,
		OpType:        OpDelete,
		EntityVersion: 3,
		OccurredAt:    now.Add(2 * time.Minute),
		PayloadJSON:   deletePayload,
		PayloadHash:   testPayloadHash(deletePayload),
	}
	if err := svc.ApplyRemoteOp(context.Background(), del); err != nil {
		t.Fatalf("ApplyRemoteOp(delete) error = %v", err)
	}
	got, err = ordersRepo.GetByOrderNo(context.Background(), order.OrderNo)
	if err != nil {
		t.Fatalf("GetByOrderNo(deleted) error = %v", err)
	}
	if got != nil {
		t.Fatalf("order after delete = %+v, want nil", got)
	}
}

func TestApplyRemoteComputeMarketOpRequiresAttachedRepository(t *testing.T) {
	opsRepo := &fakeHASyncOpRepo{}
	versionsRepo := &fakeHAEntityVersionRepo{items: make(map[string]*store.HAEntityVersion)}
	svc := &Service{nodeID: "hc-1", ops: opsRepo, versions: versionsRepo}
	payload := `{"order_no":"HC-NOT-READY"}`
	op := &store.HASyncOp{
		OpID:          "op-card-order-not-ready",
		SourceNodeID:  "hc-2",
		EntityType:    EntityLLMCardOrder,
		EntityID:      "HC-NOT-READY",
		OpType:        OpUpsert,
		EntityVersion: 1,
		OccurredAt:    time.Now().UTC(),
		PayloadJSON:   payload,
		PayloadHash:   testPayloadHash(payload),
	}

	err := svc.ApplyRemoteOp(context.Background(), op)
	if !errors.Is(err, ErrReplicaNotReady) {
		t.Fatalf("ApplyRemoteOp() error = %v, want ErrReplicaNotReady", err)
	}
	if version, err := versionsRepo.Get(context.Background(), op.EntityType, op.EntityID); err != nil {
		t.Fatalf("version Get() error = %v", err)
	} else if version != nil {
		t.Fatalf("operation without a repository updated version: %#v", version)
	}
	if applied, err := opsRepo.HasApplied(context.Background(), op.OpID); err != nil {
		t.Fatalf("HasApplied() error = %v", err)
	} else if applied {
		t.Fatal("operation without a repository was marked applied")
	}
}

func TestValidateRemoteLLMCardOrderRejectsMismatchedOrderNo(t *testing.T) {
	payload := `{"order_no":"HC-OTHER"}`
	op := &store.HASyncOp{
		OpID:          "op-card-order-bad-id",
		SourceNodeID:  "hc-2",
		EntityType:    EntityLLMCardOrder,
		EntityID:      "HC-EXPECTED",
		OpType:        OpUpsert,
		EntityVersion: 1,
		OccurredAt:    time.Now().UTC(),
		PayloadJSON:   payload,
		PayloadHash:   testPayloadHash(payload),
	}

	err := validateRemoteOp(op)
	var invalid InvalidRemoteOpError
	if !errors.As(err, &invalid) {
		t.Fatalf("validateRemoteOp() error = %v, want InvalidRemoteOpError", err)
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

func TestApplyRemoteOpsRebuildsRouteSnapshotOncePerBatch(t *testing.T) {
	opsRepo := &fakeHASyncOpRepo{}
	versionsRepo := &fakeHAEntityVersionRepo{items: make(map[string]*store.HAEntityVersion)}
	refresher := &fakeRouteSnapshotRefresher{}
	svc := &Service{
		nodeID:    "hc-1",
		ops:       opsRepo,
		versions:  versionsRepo,
		news:      &fakeHANewsRepo{},
		refresher: refresher,
	}
	now := time.Now().UTC()
	newsPayload := `{"id":"news-1","title":"hello","content":"","category":"notice","pinned":false,"created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-10T00:00:00Z"}`
	hubPayload := `{"id":"hub-1","installation_id":"inst-1","owner_email":"owner@example.com","name":"Hub","description":"","base_url":"https://hub.example.com","host":"hub.example.com","port":443,"visibility":"public","enrollment_mode":"open","corporate_email_domain":"","accept_public_signup":false,"status":"online","is_disabled":false,"disabled_reason":"","capabilities_json":"{}","hub_secret_hash":"","invitation_code_required":false,"digital_employee_quota":0,"digital_employee_authorization_enabled":false,"last_seen_at":null,"created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-10T00:00:00Z"}`
	ops := []*store.HASyncOp{
		{OpID: "op-news", SourceNodeID: "hc-2", EntityType: EntityNewsArticle, EntityID: "news-1", OpType: OpUpsert, EntityVersion: 1, OccurredAt: now, PayloadJSON: newsPayload, PayloadHash: testPayloadHash(newsPayload)},
		{OpID: "op-hub-1", SourceNodeID: "hc-2", EntityType: EntityHubInstance, EntityID: "hub-1", OpType: OpUpsert, EntityVersion: 1, OccurredAt: now.Add(time.Second), PayloadJSON: hubPayload, PayloadHash: testPayloadHash(hubPayload)},
		{OpID: "op-hub-2", SourceNodeID: "hc-2", EntityType: EntityHubInstance, EntityID: "hub-1", OpType: OpUpsert, EntityVersion: 2, OccurredAt: now.Add(2 * time.Second), PayloadJSON: hubPayload, PayloadHash: testPayloadHash(hubPayload)},
	}

	if err := svc.ApplyRemoteOps(context.Background(), ops); err != nil {
		t.Fatalf("ApplyRemoteOps() error = %v", err)
	}
	if refresher.Calls() != 1 {
		t.Fatalf("route snapshot rebuild calls = %d, want 1", refresher.Calls())
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

func TestApplyRemoteOpsRejectsMismatchedHubPayloadBeforeRecording(t *testing.T) {
	opsRepo := &fakeHASyncOpRepo{}
	versionsRepo := &fakeHAEntityVersionRepo{items: make(map[string]*store.HAEntityVersion)}
	svc := &Service{nodeID: "hc-1", ops: opsRepo, versions: versionsRepo}
	payload := `{"id":"hub-other","base_url":"https://hub.example.com"}`
	op := &store.HASyncOp{
		OpID:          "op-bad-hub",
		SourceNodeID:  "hc-2",
		EntityType:    EntityHubInstance,
		EntityID:      "hub-1",
		OpType:        OpUpsert,
		EntityVersion: 1,
		OccurredAt:    time.Now().UTC(),
		PayloadJSON:   payload,
		PayloadHash:   testPayloadHash(payload),
	}

	err := svc.ApplyRemoteOps(context.Background(), []*store.HASyncOp{op})
	var invalid InvalidRemoteOpError
	if !errors.As(err, &invalid) {
		t.Fatalf("ApplyRemoteOps() error = %v, want InvalidRemoteOpError", err)
	}
	if len(opsRepo.remoteOps) != 0 {
		t.Fatalf("invalid op was recorded before rejection: %+v", opsRepo.remoteOps)
	}
}

func TestApplyRemoteOpsRejectsUnsupportedEntityBeforeRecording(t *testing.T) {
	opsRepo := &fakeHASyncOpRepo{}
	versionsRepo := &fakeHAEntityVersionRepo{items: make(map[string]*store.HAEntityVersion)}
	svc := &Service{nodeID: "hc-1", ops: opsRepo, versions: versionsRepo}
	payload := `{"id":"mystery-1"}`
	op := &store.HASyncOp{
		OpID:          "op-unsupported",
		SourceNodeID:  "hc-2",
		EntityType:    "mystery_entity",
		EntityID:      "mystery-1",
		OpType:        OpUpsert,
		EntityVersion: 1,
		OccurredAt:    time.Now().UTC(),
		PayloadJSON:   payload,
		PayloadHash:   testPayloadHash(payload),
	}

	err := svc.ApplyRemoteOps(context.Background(), []*store.HASyncOp{op})
	var invalid InvalidRemoteOpError
	if !errors.As(err, &invalid) {
		t.Fatalf("ApplyRemoteOps() error = %v, want InvalidRemoteOpError", err)
	}
	if len(opsRepo.remoteOps) != 0 {
		t.Fatalf("unsupported op was recorded before rejection: %+v", opsRepo.remoteOps)
	}
}

func TestApplyRemoteOpsRejectsUnsupportedEntityOpBeforeRecording(t *testing.T) {
	opsRepo := &fakeHASyncOpRepo{}
	versionsRepo := &fakeHAEntityVersionRepo{items: make(map[string]*store.HAEntityVersion)}
	svc := &Service{nodeID: "hc-1", ops: opsRepo, versions: versionsRepo}
	payload := `{"key":"admin_initialized"}`
	op := &store.HASyncOp{
		OpID:          "op-delete-setting",
		SourceNodeID:  "hc-2",
		EntityType:    EntitySystemSetting,
		EntityID:      "admin_initialized",
		OpType:        OpDelete,
		EntityVersion: 1,
		OccurredAt:    time.Now().UTC(),
		PayloadJSON:   payload,
		PayloadHash:   testPayloadHash(payload),
	}

	err := svc.ApplyRemoteOps(context.Background(), []*store.HASyncOp{op})
	var invalid InvalidRemoteOpError
	if !errors.As(err, &invalid) {
		t.Fatalf("ApplyRemoteOps() error = %v, want InvalidRemoteOpError", err)
	}
	if len(opsRepo.remoteOps) != 0 {
		t.Fatalf("unsupported entity op was recorded before rejection: %+v", opsRepo.remoteOps)
	}
}

func TestValidateRemoteOpPayloadIdentityRejectsMismatchedRoutingPayload(t *testing.T) {
	payload := `{"id":"route-other","hub_id":"hub-1","domain":"example.com"}`
	op := &store.HASyncOp{
		OpID:          "op-bad-route",
		SourceNodeID:  "hc-2",
		EntityType:    EntityHubDomainRoute,
		EntityID:      "route-1",
		OpType:        OpUpsert,
		EntityVersion: 1,
		OccurredAt:    time.Now().UTC(),
		PayloadJSON:   payload,
		PayloadHash:   testPayloadHash(payload),
	}

	err := validateRemoteOp(op)
	var invalid InvalidRemoteOpError
	if !errors.As(err, &invalid) {
		t.Fatalf("validateRemoteOp() error = %v, want InvalidRemoteOpError", err)
	}
}

func TestValidateRemoteOpPayloadIdentityRejectsMissingRoutingPayloadID(t *testing.T) {
	payload := `{"hub_id":"hub-1","domain":"example.com"}`
	op := &store.HASyncOp{
		OpID:          "op-missing-route-id",
		SourceNodeID:  "hc-2",
		EntityType:    EntityHubDomainRoute,
		EntityID:      "route-1",
		OpType:        OpUpsert,
		EntityVersion: 1,
		OccurredAt:    time.Now().UTC(),
		PayloadJSON:   payload,
		PayloadHash:   testPayloadHash(payload),
	}

	err := validateRemoteOp(op)
	var invalid InvalidRemoteOpError
	if !errors.As(err, &invalid) {
		t.Fatalf("validateRemoteOp() error = %v, want InvalidRemoteOpError", err)
	}
}

func TestValidateRemoteOpPayloadIdentityAllowsMissingHubPayloadID(t *testing.T) {
	payload := `{"base_url":"https://hub.example.com"}`
	op := &store.HASyncOp{
		OpID:          "op-hub-no-id",
		SourceNodeID:  "hc-2",
		EntityType:    EntityHubInstance,
		EntityID:      "hub-1",
		OpType:        OpUpsert,
		EntityVersion: 1,
		OccurredAt:    time.Now().UTC(),
		PayloadJSON:   payload,
		PayloadHash:   testPayloadHash(payload),
	}

	if err := validateRemoteOp(op); err != nil {
		t.Fatalf("validateRemoteOp() error = %v", err)
	}
}

func TestValidateRemoteOpPayloadIdentityAllowsCaseInsensitiveBlockedEmail(t *testing.T) {
	payload := `{"email":"User@Example.com"}`
	op := &store.HASyncOp{
		OpID:          "op-block-email",
		SourceNodeID:  "hc-2",
		EntityType:    EntityBlockedEmail,
		EntityID:      "user@example.com",
		OpType:        OpDelete,
		EntityVersion: 1,
		OccurredAt:    time.Now().UTC(),
		PayloadJSON:   payload,
		PayloadHash:   testPayloadHash(payload),
	}

	if err := validateRemoteOp(op); err != nil {
		t.Fatalf("validateRemoteOp() error = %v", err)
	}
}

func TestNormalizeHubPayloadID(t *testing.T) {
	item := &store.HubInstance{}
	if err := normalizeHubPayloadID(item, "hub-1"); err != nil {
		t.Fatalf("normalizeHubPayloadID() error = %v", err)
	}
	if item.ID != "hub-1" {
		t.Fatalf("ID = %q, want hub-1", item.ID)
	}
	if err := normalizeHubPayloadID(&store.HubInstance{ID: "hub-other"}, "hub-1"); err == nil {
		t.Fatal("normalizeHubPayloadID() succeeded with mismatched id")
	}
}

func TestHasEntityTypeOpsFallbackScansPaged(t *testing.T) {
	opsRepo := &fakeHASyncOpRepo{}
	for i := 1; i <= haOpsFallbackPageSize+25; i++ {
		entityType := EntityNewsArticle
		if i == haOpsFallbackPageSize+25 {
			entityType = EntitySkillMarketSnapshot
		}
		opsRepo.ops = append(opsRepo.ops, &store.HASyncOp{Seq: int64(i), EntityType: entityType, EntityID: fmt.Sprintf("entity-%d", i), OccurredAt: time.Now().UTC().Add(time.Duration(i) * time.Millisecond)})
	}
	svc := &Service{ops: opsRepo}

	found, err := svc.HasEntityTypeOps(context.Background(), EntitySkillMarketSnapshot)
	if err != nil {
		t.Fatalf("HasEntityTypeOps() error = %v", err)
	}
	if !found {
		t.Fatal("expected fallback scan to find entity type after first page")
	}
	_, limits := opsRepo.ListStats()
	if len(limits) < 2 || limits[0] != haOpsFallbackPageSize || limits[1] != haOpsFallbackPageSize {
		t.Fatalf("expected paged ListAfterSeq calls, limits=%+v", limits)
	}
}

func TestListAdminSyncDetailOpsFallbackKeepsLatestPerEntityType(t *testing.T) {
	opsRepo := &fakeHASyncOpRepo{}
	for i := 1; i <= haOpsFallbackPageSize+10; i++ {
		opsRepo.ops = append(opsRepo.ops, &store.HASyncOp{Seq: int64(i), EntityType: EntityHubInstance, EntityID: "hub-1", OccurredAt: time.Now().UTC().Add(time.Duration(i) * time.Millisecond)})
	}
	opsRepo.ops = append(opsRepo.ops, &store.HASyncOp{Seq: int64(haOpsFallbackPageSize + 11), EntityType: EntityBlockedEmail, EntityID: "blocked@example.com", OccurredAt: time.Now().UTC().Add(time.Hour)})
	svc := &Service{ops: opsRepo}

	ops, err := svc.listAdminSyncDetailOps(context.Background())
	if err != nil {
		t.Fatalf("listAdminSyncDetailOps() error = %v", err)
	}
	latest := map[string]int64{}
	for _, op := range ops {
		latest[op.EntityType] = op.Seq
	}
	if latest[EntityHubInstance] != int64(haOpsFallbackPageSize+10) || latest[EntityBlockedEmail] != int64(haOpsFallbackPageSize+11) {
		t.Fatalf("unexpected latest ops: %+v", latest)
	}
	_, limits := opsRepo.ListStats()
	if len(limits) < 2 || limits[0] != haOpsFallbackPageSize {
		t.Fatalf("expected paged fallback calls, limits=%+v", limits)
	}
}

func TestAppendSnapshotSkipsUnchangedPayload(t *testing.T) {
	opsRepo := &fakeHASyncOpRepo{}
	svc := NewService("hc-1", "hc-1", "https://hc-1.example.com", "secret", nil)
	svc.ops = opsRepo
	svc.versions = &fakeHAEntityVersionRepo{items: map[string]*store.HAEntityVersion{}}
	ctx := context.Background()

	svc.AppendGossipSnapshot(ctx, &GossipSnapshot{Posts: []*store.GossipPost{{ID: "post-1", Content: "hello"}}})
	svc.AppendGossipSnapshot(ctx, &GossipSnapshot{Posts: []*store.GossipPost{{ID: "post-1", Content: "hello"}}})
	svc.AppendGossipSnapshot(ctx, &GossipSnapshot{Posts: []*store.GossipPost{{ID: "post-1", Content: "changed"}}})

	opsRepo.mu.Lock()
	defer opsRepo.mu.Unlock()
	if len(opsRepo.ops) != 2 {
		t.Fatalf("ops len = %d, want 2", len(opsRepo.ops))
	}
	if opsRepo.ops[0].EntityType != EntityGossipSnapshot || opsRepo.ops[1].EntityType != EntityGossipSnapshot {
		t.Fatalf("unexpected ops: %+v", opsRepo.ops)
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

func TestRunPeerPushQueueDefersWhenSemaphoreSaturated(t *testing.T) {
	svc := NewService("hc-1", "hc-1", "", "secret", []StaticPeer{{NodeID: "hc-2", NodeName: "hc-2", BaseURL: "http://hc-2"}})
	svc.pushSem = make(chan struct{}, 1)
	svc.pushSem <- struct{}{}
	op := &store.HASyncOp{OpID: "op-1", EntityType: EntityNewsArticle, EntityID: "news-1", EntityVersion: 1, OccurredAt: time.Now().UTC()}
	svc.pushMu.Lock()
	svc.pushPending["hc-2"] = []*store.HASyncOp{op}
	svc.pushRunning["hc-2"] = true
	svc.pushMu.Unlock()

	svc.runPeerPushQueue(PeerRuntimeState{NodeID: "hc-2", BaseURL: "http://hc-2"})

	svc.pushMu.Lock()
	pending := append([]*store.HASyncOp(nil), svc.pushPending["hc-2"]...)
	running := svc.pushRunning["hc-2"]
	svc.pushMu.Unlock()
	if len(pending) != 1 || pending[0].OpID != "op-1" {
		t.Fatalf("pending = %+v, want requeued op", pending)
	}
	if running {
		t.Fatal("push queue still marked running after deferred batch")
	}
	peer := svc.listPeerStates()[0]
	if peer.Backlog < 1 || peer.LastError == "" {
		t.Fatalf("peer deferred state not recorded: %+v", peer)
	}
}

func TestRunPeerPushQueueRequeuesHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	svc := NewService("hc-1", "hc-1", "", "secret", []StaticPeer{{NodeID: "hc-2", NodeName: "hc-2", BaseURL: server.URL}})
	op := &store.HASyncOp{OpID: "op-http-fail", EntityType: EntityNewsArticle, EntityID: "news-1", EntityVersion: 1, OccurredAt: time.Now().UTC()}
	svc.pushMu.Lock()
	svc.pushPending["hc-2"] = []*store.HASyncOp{op}
	svc.pushRunning["hc-2"] = true
	svc.pushMu.Unlock()

	svc.runPeerPushQueue(PeerRuntimeState{NodeID: "hc-2", BaseURL: server.URL})

	svc.pushMu.Lock()
	pending := append([]*store.HASyncOp(nil), svc.pushPending["hc-2"]...)
	running := svc.pushRunning["hc-2"]
	svc.pushMu.Unlock()
	if len(pending) != 1 || pending[0].OpID != "op-http-fail" {
		t.Fatalf("pending = %+v, want failed op retained", pending)
	}
	if running {
		t.Fatal("push queue still marked running after HTTP failure")
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

func TestInternalURLForPublicOriginUsesPeerTransportURL(t *testing.T) {
	svc := NewService("hc-1", "HubCenter 1", "http://10.0.0.1:9388", "secret", []StaticPeer{{
		NodeID:    "hc-2",
		NodeName:  "HubCenter 2",
		BaseURL:   "http://10.0.0.2:9388",
		PublicURL: "https://hubs-2.example.com",
	}})
	if got := svc.InternalURLForPublicOrigin("https://hubs-2.example.com/"); got != "http://10.0.0.2:9388" {
		t.Fatalf("InternalURLForPublicOrigin() = %q", got)
	}
	if got := svc.InternalURLForPublicOrigin("https://unknown.example.com"); got != "" {
		t.Fatalf("unexpected unknown origin mapping: %q", got)
	}
}

func TestOwnsPublicOriginUsesNodeLocalPublicURL(t *testing.T) {
	svc := NewService("hc-1", "hc-1", "http://10.0.0.1:9388", "secret", nil)
	svc.SetPublicURL("https://hubs-1.example.com/")

	if !svc.OwnsPublicOrigin("https://HUBS-1.example.com") {
		t.Fatal("expected this node to own its configured public origin")
	}
	if svc.OwnsPublicOrigin("https://hubs-2.example.com") {
		t.Fatal("must not claim a peer public origin")
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
		pushDebounce:             3 * time.Minute,
		peers: map[string]*PeerRuntimeState{
			"hc-2": {NodeID: "hc-2", NodeName: "HubCenter 2", BaseURL: "https://hubs.maclaw.top", Reachable: true, QualityScore: 95, ServiceStatus: "healthy", ClusterStatus: "healthy", LastSuccessAt: &now},
			"hc-3": {NodeID: "hc-3", NodeName: "HubCenter 3", BaseURL: "https://hubs2.maclaw.top", Reachable: true, QualityScore: 90, ServiceStatus: "healthy", ClusterStatus: "healthy", LastSuccessAt: &now},
		},
		notifications: &fakeNotificationStore{items: map[string]*notification.Notification{
			"notif-1": {ID: "notif-1", Title: "Notice 1"},
			"notif-2": {ID: "notif-2", Title: "Notice 2"},
		}},
	}

	status, err := svc.GetAdminStatus(context.Background())
	if err != nil {
		t.Fatalf("GetAdminStatus() error = %v", err)
	}
	if len(status.Sync.Details) != 8 {
		t.Fatalf("sync details len = %d, want 8", len(status.Sync.Details))
	}
	if status.Sync.PushDebounceSeconds != 180 {
		t.Fatalf("push debounce seconds = %d, want 180", status.Sync.PushDebounceSeconds)
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
	computeMarket := find("compute_market")
	if computeMarket == nil || computeMarket.Status != "idle" || computeMarket.PendingOps != 0 || computeMarket.PendingPeers != 0 {
		t.Fatalf("compute_market detail = %#v", computeMarket)
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
	notifications := find("notifications")
	if notifications == nil || notifications.Status != "needs_seed" || notifications.LocalRecords != 2 {
		t.Fatalf("notifications detail = %#v", notifications)
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
