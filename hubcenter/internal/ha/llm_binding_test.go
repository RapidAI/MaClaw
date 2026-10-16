package ha

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

type bindingTestRepo struct {
	mu    sync.Mutex
	items map[string]*store.LLMNodeBinding
}

func (r *bindingTestRepo) Upsert(_ context.Context, binding *store.LLMNodeBinding) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.items == nil {
		r.items = map[string]*store.LLMNodeBinding{}
	}
	r.items[bindingKey(binding)] = cloneBinding(binding)
	return nil
}

func (r *bindingTestRepo) Get(_ context.Context, hubID, tenantID string) (*store.LLMNodeBinding, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneBinding(r.items[hubID+"\x00"+tenantID]), nil
}

func (r *bindingTestRepo) Delete(context.Context, string, string) error { return nil }

func (r *bindingTestRepo) ListByNode(_ context.Context, nodeID string) ([]*store.LLMNodeBinding, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []*store.LLMNodeBinding
	for _, item := range r.items {
		if item.NodeID == nodeID {
			result = append(result, cloneBinding(item))
		}
	}
	return result, nil
}

func (r *bindingTestRepo) ListAll(context.Context) ([]*store.LLMNodeBinding, error) { return nil, nil }
func (r *bindingTestRepo) DeleteExpired(_ context.Context, now time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int64
	for key, item := range r.items {
		if bindingExpired(item, now) {
			delete(r.items, key)
			n++
		}
	}
	return n, nil
}

func TestLLMBindingManagerRestoresExistingLocalLease(t *testing.T) {
	ctx := context.Background()
	boundAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	repo := &bindingTestRepo{items: map[string]*store.LLMNodeBinding{
		"hub-1\x00tenant-1": {HubID: "hub-1", TenantID: "tenant-1", NodeID: "hc-1", BoundAt: boundAt, LastActive: boundAt, ExpiresAt: boundAt.Add(BindingLeaseTTL)},
	}}
	manager := NewLLMBindingManager("hc-1", repo)

	ok, binding, err := manager.TryBind(ctx, "hub-1", "tenant-1")
	if err != nil || !ok {
		t.Fatalf("TryBind() = (%v, %v), want local lease", ok, err)
	}
	if !binding.BoundAt.Equal(boundAt) {
		t.Fatalf("BoundAt = %v, want original %v", binding.BoundAt, boundAt)
	}
	if local := manager.GetLocalBindings(); len(local) != 1 || !local[0].BoundAt.Equal(boundAt) {
		t.Fatalf("local bindings = %#v, want restored local lease", local)
	}
}

func TestLLMBindingManagerReturnsDefensiveBindingCopies(t *testing.T) {
	ctx := context.Background()
	manager := NewLLMBindingManager("hc-1", &bindingTestRepo{})
	ok, binding, err := manager.TryBind(ctx, "hub-1", "tenant-1")
	if err != nil || !ok {
		t.Fatalf("TryBind() = (%v, %v), want local lease", ok, err)
	}
	binding.NodeID = "mutated"
	local := manager.GetLocalBindings()
	if len(local) != 1 || local[0].NodeID != "hc-1" {
		t.Fatalf("internal lease was mutated through result: %#v", local)
	}
	local[0].NodeID = "mutated-again"
	if local = manager.GetLocalBindings(); local[0].NodeID != "hc-1" {
		t.Fatalf("internal lease was mutated through local list: %#v", local)
	}
}

func TestLLMBindingManagerReleaseBindingsTombstones(t *testing.T) {
	ctx := context.Background()
	repo := &bindingTestRepo{}
	manager := NewLLMBindingManager("hc-1", repo)

	ok, _, err := manager.TryBind(ctx, "hub-1", "tenant-1")
	if err != nil || !ok {
		t.Fatalf("TryBind() = (%v, %v), want local lease", ok, err)
	}

	released, err := manager.ReleaseBindingsForHubNode(ctx, "hub-1", "hc-1", time.Now().UTC())
	if err != nil || released != 1 {
		t.Fatalf("ReleaseBindingsForHubNode() = (%d, %v), want 1", released, err)
	}
	if local := manager.GetLocalBindings(); len(local) != 0 {
		t.Fatalf("local bindings = %#v, want released lease removed", local)
	}
	persisted, err := repo.Get(ctx, "hub-1", "tenant-1")
	if err != nil {
		t.Fatalf("repo.Get() = %v", err)
	}
	if persisted == nil || time.Now().UTC().Before(persisted.ExpiresAt) {
		t.Fatalf("persisted binding = %#v, want expired tombstone", persisted)
	}

	ok, binding, err := manager.TryBind(ctx, "hub-1", "tenant-1")
	if err != nil || !ok {
		t.Fatalf("TryBind() after release = (%v, %v, %v), want rebind", ok, binding, err)
	}

	released, err = manager.ReleaseBindingsForHubNode(ctx, "hub-2", "hc-1", time.Now().UTC())
	if err != nil || released != 0 {
		t.Fatalf("ReleaseBindingsForHubNode(foreign hub) = (%d, %v), want 0", released, err)
	}
}

func TestLLMBindingManagerSerializesConcurrentLocalClaims(t *testing.T) {
	ctx := context.Background()
	manager := NewLLMBindingManager("hc-1", &bindingTestRepo{})
	const requests = 32
	results := make(chan error, requests)
	var wg sync.WaitGroup
	for range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, _, err := manager.TryBind(ctx, "hub-1", "tenant-1")
			if err != nil || !ok {
				results <- err
			}
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		t.Fatalf("concurrent TryBind failed: %v", err)
	}
	if bindings := manager.GetLocalBindings(); len(bindings) != 1 || bindings[0].NodeID != "hc-1" {
		t.Fatalf("local bindings = %#v, want one hc-1 lease", bindings)
	}
}

// staleListRepo returns a canned snapshot from ListByNode while reads via Get
// observe the live store, modelling the race between a release snapshot and a
// concurrent renewal that lands before the release acquires m.mu.
type staleListRepo struct {
	snapshot []*store.LLMNodeBinding
	live     *bindingTestRepo
}

func (r *staleListRepo) Upsert(ctx context.Context, b *store.LLMNodeBinding) error {
	return r.live.Upsert(ctx, b)
}

func (r *staleListRepo) Get(ctx context.Context, hubID, tenantID string) (*store.LLMNodeBinding, error) {
	return r.live.Get(ctx, hubID, tenantID)
}

func (r *staleListRepo) Delete(ctx context.Context, hubID, tenantID string) error {
	return r.live.Delete(ctx, hubID, tenantID)
}

func (r *staleListRepo) ListByNode(context.Context, string) ([]*store.LLMNodeBinding, error) {
	out := make([]*store.LLMNodeBinding, 0, len(r.snapshot))
	for _, b := range r.snapshot {
		out = append(out, cloneBinding(b))
	}
	return out, nil
}

func (r *staleListRepo) ListAll(context.Context) ([]*store.LLMNodeBinding, error) { return nil, nil }
func (r *staleListRepo) DeleteExpired(context.Context, time.Time) (int64, error)  { return 0, nil }

func TestLLMBindingManagerReleaseSkipsConcurrentlyRenewedLease(t *testing.T) {
	ctx := context.Background()
	live := &bindingTestRepo{}
	repo := &staleListRepo{live: live}
	manager := NewLLMBindingManager("hc-1", repo)

	ok, _, err := manager.TryBind(ctx, "hub-1", "tenant-1")
	if err != nil || !ok {
		t.Fatalf("TryBind() = (%v, %v), want local lease", ok, err)
	}
	repo.snapshot = manager.GetLocalBindings()
	for _, b := range repo.snapshot {
		b.LastActive = b.LastActive.Add(-time.Second)
		b.ExpiresAt = b.ExpiresAt.Add(-time.Second)
	}

	// A renewal wins after the release snapshot was taken.
	ok, _, err = manager.TryBind(ctx, "hub-1", "tenant-1")
	if err != nil || !ok {
		t.Fatalf("renew TryBind() = (%v, %v), want local lease", ok, err)
	}

	released, err := manager.ReleaseBindingsForHubNode(ctx, "hub-1", "hc-1", time.Now().UTC())
	if err != nil || released != 0 {
		t.Fatalf("ReleaseBindingsForHubNode() = (%d, %v), want 0 (renewal won)", released, err)
	}
	persisted, err := live.Get(ctx, "hub-1", "tenant-1")
	if err != nil {
		t.Fatalf("repo.Get() = %v", err)
	}
	if persisted == nil || time.Now().UTC().After(persisted.ExpiresAt) {
		t.Fatalf("persisted binding = %#v, want live lease untouched", persisted)
	}
	if local := manager.GetLocalBindings(); len(local) != 1 {
		t.Fatalf("local bindings = %#v, want renewed lease kept", local)
	}
}

func TestLLMBindingManagerReleaseClearsRemoteCacheAndCountsAll(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	repo := &bindingTestRepo{items: map[string]*store.LLMNodeBinding{
		"hub-1\x00tenant-1": {HubID: "hub-1", TenantID: "tenant-1", NodeID: "hc-dead", BoundAt: now, LastActive: now, ExpiresAt: now.Add(BindingLeaseTTL)},
		"hub-1\x00tenant-2": {HubID: "hub-1", TenantID: "tenant-2", NodeID: "hc-dead", BoundAt: now, LastActive: now, ExpiresAt: now.Add(BindingLeaseTTL)},
		"hub-2\x00tenant-9": {HubID: "hub-2", TenantID: "tenant-9", NodeID: "hc-dead", BoundAt: now, LastActive: now, ExpiresAt: now.Add(BindingLeaseTTL)},
	}}
	manager := NewLLMBindingManager("hc-1", repo)

	for _, tenant := range []string{"tenant-1", "tenant-2"} {
		ok, existing, err := manager.TryBind(ctx, "hub-1", tenant)
		if err != nil || ok || existing == nil || existing.NodeID != "hc-dead" {
			t.Fatalf("TryBind(%s) = (%v, %#v, %v), want redirect to hc-dead", tenant, ok, existing, err)
		}
	}

	released, err := manager.ReleaseBindingsForHubNode(ctx, "hub-1", "hc-dead", time.Now().UTC())
	if err != nil || released != 2 {
		t.Fatalf("ReleaseBindingsForHubNode() = (%d, %v), want 2", released, err)
	}
	persisted, err := repo.Get(ctx, "hub-2", "tenant-9")
	if err != nil {
		t.Fatalf("repo.Get() = %v", err)
	}
	if persisted == nil || time.Now().UTC().After(persisted.ExpiresAt) {
		t.Fatalf("foreign hub binding = %#v, want untouched live lease", persisted)
	}

	for _, tenant := range []string{"tenant-1", "tenant-2"} {
		ok, _, err := manager.TryBind(ctx, "hub-1", tenant)
		if err != nil || !ok {
			t.Fatalf("TryBind(%s) after release = (%v, %v), want rebind on hc-1", tenant, ok, err)
		}
	}
}

func TestLLMBindingManagerInvalidateCachedBindingUnblocksRebind(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	repo := &bindingTestRepo{items: map[string]*store.LLMNodeBinding{
		"hub-1\x00tenant-1": {HubID: "hub-1", TenantID: "tenant-1", NodeID: "hc-dead", BoundAt: now, LastActive: now, ExpiresAt: now.Add(BindingLeaseTTL)},
	}}
	manager := NewLLMBindingManager("hc-1", repo)

	ok, existing, err := manager.TryBind(ctx, "hub-1", "tenant-1")
	if err != nil || ok || existing == nil || existing.NodeID != "hc-dead" {
		t.Fatalf("TryBind() = (%v, %#v, %v), want redirect to hc-dead", ok, existing, err)
	}

	// A replicated release tombstone lands in the durable store; the HA apply
	// path calls InvalidateCachedBinding to evict the stale cached lease.
	if err := repo.Upsert(ctx, &store.LLMNodeBinding{HubID: "hub-1", TenantID: "tenant-1", NodeID: "hc-dead", BoundAt: now, LastActive: now, ExpiresAt: now}); err != nil {
		t.Fatalf("Upsert tombstone: %v", err)
	}
	manager.InvalidateCachedBinding("hub-1", "tenant-1")

	ok, _, err = manager.TryBind(ctx, "hub-1", "tenant-1")
	if err != nil || !ok {
		t.Fatalf("TryBind() after invalidate = (%v, %v), want rebind on hc-1", ok, err)
	}
}

func TestLLMBindingManagerReleaseConcurrentWithRenewal(t *testing.T) {
	ctx := context.Background()
	repo := &bindingTestRepo{}
	manager := NewLLMBindingManager("hc-1", repo)
	if _, _, err := manager.TryBind(ctx, "hub-1", "tenant-1"); err != nil {
		t.Fatalf("TryBind: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				manager.TryBind(ctx, "hub-1", "tenant-1")
				manager.RenewBinding(ctx, "hub-1", "tenant-1")
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 50; j++ {
			_, _ = manager.ReleaseBindingsForHubNode(ctx, "hub-1", "hc-1", time.Now().UTC())
		}
	}()
	wg.Wait()

	// Whatever interleaving won, in-memory and durable state must agree:
	// either a live local lease whose repo record is also live, or an
	// expired/absent repo record with no cached local lease.
	persisted, err := repo.Get(ctx, "hub-1", "tenant-1")
	if err != nil {
		t.Fatalf("repo.Get() = %v", err)
	}
	liveLocal := len(manager.GetLocalBindings()) > 0
	persistedLive := persisted != nil && time.Now().UTC().Before(persisted.ExpiresAt)
	if liveLocal != persistedLive {
		t.Fatalf("cache/durable mismatch: local=%v persisted=%#v", liveLocal, persisted)
	}
}

func TestApplyLLMNodeBindingOpInvalidatesCachedBinding(t *testing.T) {
	ctx := context.Background()
	repo := &bindingTestRepo{}
	svc := &Service{llmBindings: repo}
	var invalidated [2]string
	svc.SetBindingInvalidator(func(hubID, tenantID string) {
		invalidated[0], invalidated[1] = hubID, tenantID
	})

	now := time.Now().UTC().Truncate(time.Second)
	item := store.LLMNodeBinding{HubID: "hub-1", TenantID: "tenant-1", NodeID: "hc-dead", BoundAt: now, LastActive: now, ExpiresAt: now}
	payload, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	op := &store.HASyncOp{
		OpID:          "op-binding-1",
		EntityType:    EntityLLMNodeBinding,
		EntityID:      llmBindingEntityID(&item),
		OpType:        OpUpsert,
		EntityVersion: 1,
		OccurredAt:    now,
		PayloadJSON:   string(payload),
	}
	if err := svc.applyLLMNodeBindingOp(ctx, op); err != nil {
		t.Fatalf("applyLLMNodeBindingOp: %v", err)
	}
	if invalidated != [2]string{"hub-1", "tenant-1"} {
		t.Fatalf("invalidated = %v, want [hub-1 tenant-1]", invalidated)
	}
	persisted, err := repo.Get(ctx, "hub-1", "tenant-1")
	if err != nil || persisted == nil || !persisted.ExpiresAt.Equal(now) {
		t.Fatalf("persisted = %#v, %v; want applied tombstone", persisted, err)
	}
}

// syncCountingRepo wraps bindingTestRepo and counts sync-binding invocations,
// so tests can prove a code path did (or did not) trigger HA replication.
type syncCountingRepo struct {
	*bindingTestRepo
	syncCalls int
}

func TestLLMBindingManagerRepeatedReleaseIsIdempotent(t *testing.T) {
	ctx := context.Background()
	repo := &syncCountingRepo{bindingTestRepo: &bindingTestRepo{}}
	manager := NewLLMBindingManager("hc-1", repo)
	manager.SetSyncBinding(func(context.Context, *store.LLMNodeBinding) { repo.syncCalls++ })

	if _, _, err := manager.TryBind(ctx, "hub-1", "tenant-1"); err != nil {
		t.Fatalf("TryBind: %v", err)
	}
	afterBind := repo.syncCalls

	released, err := manager.ReleaseBindingsForHubNode(ctx, "hub-1", "hc-1", time.Now().UTC())
	if err != nil || released != 1 {
		t.Fatalf("first release = (%d, %v), want 1", released, err)
	}
	if repo.syncCalls != afterBind+1 {
		t.Fatalf("sync calls after first release = %d, want %d (tombstone must replicate)", repo.syncCalls, afterBind+1)
	}

	// Hub retry: the tombstone row is already expired, so the second call
	// must release nothing and must not force another HA op into the log.
	released, err = manager.ReleaseBindingsForHubNode(ctx, "hub-1", "hc-1", time.Now().UTC())
	if err != nil || released != 0 {
		t.Fatalf("second release = (%d, %v), want (0, nil)", released, err)
	}
	if repo.syncCalls != afterBind+1 {
		t.Fatalf("sync calls after retry = %d, want %d (idempotent, no extra HA op)", repo.syncCalls, afterBind+1)
	}
}

func TestLLMBindingManagerReleaseSkipsLeaseThatMovedNodes(t *testing.T) {
	ctx := context.Background()
	live := &bindingTestRepo{}
	repo := &staleListRepo{live: live}
	manager := NewLLMBindingManager("hc-1", repo)

	base := time.Now().UTC().Add(-time.Minute)
	repo.snapshot = []*store.LLMNodeBinding{
		{HubID: "hub-1", TenantID: "tenant-1", NodeID: "hc-dead", BoundAt: base, LastActive: base, ExpiresAt: base.Add(BindingLeaseTTL)},
	}
	// Between the snapshot and the release acquiring m.mu, the tenant
	// rebound onto a healthy node. Identical timestamps keep the NodeID
	// guard, not the newer-lease guard, load-bearing for this test.
	if err := live.Upsert(ctx, &store.LLMNodeBinding{HubID: "hub-1", TenantID: "tenant-1", NodeID: "hc-2", BoundAt: base, LastActive: base, ExpiresAt: base.Add(BindingLeaseTTL)}); err != nil {
		t.Fatalf("seed moved lease: %v", err)
	}

	released, err := manager.ReleaseBindingsForHubNode(ctx, "hub-1", "hc-dead", time.Now().UTC())
	if err != nil || released != 0 {
		t.Fatalf("release = (%d, %v), want 0 (lease moved off the target node)", released, err)
	}
	persisted, err := live.Get(ctx, "hub-1", "tenant-1")
	if err != nil || persisted == nil || persisted.NodeID != "hc-2" || time.Now().UTC().After(persisted.ExpiresAt) {
		t.Fatalf("moved lease = %#v, %v; want live hc-2 lease untouched", persisted, err)
	}
}

func TestLLMBindingManagerCleanupExpiredPurgesTombstones(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	repo := &bindingTestRepo{items: map[string]*store.LLMNodeBinding{
		"hub-1\x00tenant-1": {HubID: "hub-1", TenantID: "tenant-1", NodeID: "hc-dead", BoundAt: now, LastActive: now, ExpiresAt: now},
		"hub-1\x00tenant-2": {HubID: "hub-1", TenantID: "tenant-2", NodeID: "hc-1", BoundAt: now, LastActive: now, ExpiresAt: now.Add(BindingLeaseTTL)},
		"hub-2\x00tenant-9": {HubID: "hub-2", TenantID: "tenant-9", NodeID: "hc-dead", BoundAt: now, LastActive: now, ExpiresAt: now.Add(-time.Minute)},
	}}
	manager := NewLLMBindingManager("hc-1", repo)

	manager.CleanupExpired(ctx)

	if persisted, _ := repo.Get(ctx, "hub-1", "tenant-1"); persisted != nil {
		t.Fatalf("tombstone row still present after cleanup: %#v", persisted)
	}
	if persisted, _ := repo.Get(ctx, "hub-2", "tenant-9"); persisted != nil {
		t.Fatalf("lapsed lease row still present after cleanup: %#v", persisted)
	}
	persisted, err := repo.Get(ctx, "hub-1", "tenant-2")
	if err != nil || persisted == nil {
		t.Fatalf("live lease removed by cleanup: %#v, %v", persisted, err)
	}
}

func TestLLMBindingManagerExpiredRemoteCacheEntryIsAMiss(t *testing.T) {
	ctx := context.Background()
	repo := &bindingTestRepo{}
	manager := NewLLMBindingManager("hc-1", repo)

	// Simulate a cache entry left behind when a tombstone was slow to
	// replicate: the cached lease is owned by a dead node but its wall-clock
	// expiry has passed.
	past := time.Now().UTC().Add(-time.Minute)
	manager.remoteBindings["hub-1\x00tenant-1"] = &store.LLMNodeBinding{
		HubID: "hub-1", TenantID: "tenant-1", NodeID: "hc-dead",
		BoundAt: past, LastActive: past, ExpiresAt: past,
	}

	ok, existing, err := manager.TryBind(ctx, "hub-1", "tenant-1")
	if err != nil || !ok {
		t.Fatalf("TryBind() = (%v, %#v, %v), want rebind on hc-1 instead of stale owner", ok, existing, err)
	}
	if existing.NodeID != "hc-1" {
		t.Fatalf("binding.NodeID = %q, want hc-1", existing.NodeID)
	}
	if cached := manager.remoteBindings["hub-1\x00tenant-1"]; cached != nil {
		t.Fatalf("expired cache entry was not evicted: %#v", cached)
	}
	if persisted, _ := repo.Get(ctx, "hub-1", "tenant-1"); persisted == nil || persisted.NodeID != "hc-1" {
		t.Fatalf("persisted = %#v, want fresh hc-1 binding", persisted)
	}
}

func TestLLMBindingManagerTombstoneRemoteCacheEntryIsAMiss(t *testing.T) {
	ctx := context.Background()
	repo := &bindingTestRepo{}
	manager := NewLLMBindingManager("hc-1", repo)

	// A tombstone (ExpiresAt == now) must read as expired even when the
	// clock has not advanced past the tombstone timestamp.
	now := time.Now().UTC()
	manager.remoteBindings["hub-1\x00tenant-1"] = &store.LLMNodeBinding{
		HubID: "hub-1", TenantID: "tenant-1", NodeID: "hc-dead",
		BoundAt: now, LastActive: now, ExpiresAt: now,
	}

	ok, existing, err := manager.TryBind(ctx, "hub-1", "tenant-1")
	if err != nil || !ok || existing.NodeID != "hc-1" {
		t.Fatalf("TryBind() = (%v, %#v, %v), want tombstone cache entry treated as miss", ok, existing, err)
	}
}

func TestLLMBindingManagerLiveRemoteCacheEntryStillRedirects(t *testing.T) {
	ctx := context.Background()
	repo := &bindingTestRepo{}
	manager := NewLLMBindingManager("hc-1", repo)

	live := time.Now().UTC()
	manager.remoteBindings["hub-1\x00tenant-1"] = &store.LLMNodeBinding{
		HubID: "hub-1", TenantID: "tenant-1", NodeID: "hc-2",
		BoundAt: live, LastActive: live, ExpiresAt: live.Add(BindingLeaseTTL),
	}

	ok, existing, err := manager.TryBind(ctx, "hub-1", "tenant-1")
	if err != nil || ok || existing == nil || existing.NodeID != "hc-2" {
		t.Fatalf("TryBind() = (%v, %#v, %v), want redirect to live owner hc-2", ok, existing, err)
	}
}

func TestLLMBindingManagerRenewBindingDoesNotReviveExpiredLease(t *testing.T) {
	ctx := context.Background()
	repo := &bindingTestRepo{}
	manager := NewLLMBindingManager("hc-1", repo)

	past := time.Now().UTC().Add(-time.Minute)
	manager.bindings["hub-1\x00tenant-1"] = &store.LLMNodeBinding{
		HubID: "hub-1", TenantID: "tenant-1", NodeID: "hc-1",
		BoundAt: past, LastActive: past, ExpiresAt: past,
	}

	manager.RenewBinding(ctx, "hub-1", "tenant-1")

	if persisted, _ := repo.Get(ctx, "hub-1", "tenant-1"); persisted != nil {
		t.Fatalf("expired lease was revived in the repo: %#v", persisted)
	}
	if cached := manager.bindings["hub-1\x00tenant-1"]; cached != nil {
		t.Fatalf("expired cache entry was kept after renewal attempt: %#v", cached)
	}
}

func TestLLMBindingManagerRenewBindingExtendsLiveLease(t *testing.T) {
	ctx := context.Background()
	repo := &bindingTestRepo{}
	manager := NewLLMBindingManager("hc-1", repo)

	if _, _, err := manager.TryBind(ctx, "hub-1", "tenant-1"); err != nil {
		t.Fatalf("TryBind: %v", err)
	}
	before, _ := repo.Get(ctx, "hub-1", "tenant-1")

	manager.RenewBinding(ctx, "hub-1", "tenant-1")

	after, err := repo.Get(ctx, "hub-1", "tenant-1")
	if err != nil || after == nil {
		t.Fatalf("repo.Get() = %#v, %v; want renewed lease", after, err)
	}
	if after.LastActive.Before(before.LastActive) {
		t.Fatalf("renewal moved LastActive backwards: before=%v after=%v", before.LastActive, after.LastActive)
	}
	if time.Now().UTC().Add(BindingLeaseTTL / 2).After(after.ExpiresAt) {
		t.Fatalf("renewal did not extend ExpiresAt: %v", after.ExpiresAt)
	}
}
