package ha

import (
	"context"
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
func (r *bindingTestRepo) DeleteExpired(context.Context, time.Time) (int64, error)  { return 0, nil }

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
