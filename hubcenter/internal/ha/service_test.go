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
