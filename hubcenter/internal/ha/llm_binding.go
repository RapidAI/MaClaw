package ha

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

// ---------------------------------------------------------------------------
// LLM Node Binding — prevents double-spending across HubCenter HA nodes
// ---------------------------------------------------------------------------

const (
	BindingLeaseTTL     = 10 * time.Minute
	BindingCooldown     = 5 * time.Minute
	BindingSyncInterval = 30 * time.Second
)

// LLMBinding is an alias to the store type for convenience.
type LLMBinding = store.LLMNodeBinding

// LLMBindingRepository is an alias to the store interface.
type LLMBindingRepository = store.LLMNodeBindingRepository

func bindingExpired(b *LLMBinding, now time.Time) bool {
	// A lease is valid only while ExpiresAt is strictly in the future. A
	// tombstone carries ExpiresAt=now and must read as expired immediately,
	// even on hosts whose coarse clock has not advanced past the tombstone
	// timestamp yet.
	return b == nil || !b.ExpiresAt.After(now)
}

func bindingKey(b *LLMBinding) string {
	if b == nil {
		return ""
	}
	return b.HubID + "\x00" + b.TenantID
}

func cloneBinding(b *LLMBinding) *LLMBinding {
	if b == nil {
		return nil
	}
	copy := *b
	return &copy
}

// LLMBindingManager manages tenant-node bindings for the current node.
type LLMBindingManager struct {
	nodeID         string
	repo           LLMBindingRepository
	mu             sync.RWMutex
	bindings       map[string]*LLMBinding
	remoteMu       sync.RWMutex
	remoteBindings map[string]*LLMBinding
	syncMu         sync.Mutex
	lastSynced     map[string]time.Time
	syncBinding    func(context.Context, *LLMBinding)
}

// NewLLMBindingManager creates a binding manager for the given node.
func NewLLMBindingManager(nodeID string, repo LLMBindingRepository) *LLMBindingManager {
	return &LLMBindingManager{
		nodeID:         nodeID,
		repo:           repo,
		bindings:       map[string]*LLMBinding{},
		remoteBindings: map[string]*LLMBinding{},
		lastSynced:     map[string]time.Time{},
	}
}

// SetSyncBinding registers a throttled durable HA replication hook.
func (m *LLMBindingManager) SetSyncBinding(fn func(context.Context, *LLMBinding)) {
	if m == nil {
		return
	}
	m.syncMu.Lock()
	defer m.syncMu.Unlock()
	m.syncBinding = fn
}

func (m *LLMBindingManager) sync(ctx context.Context, binding *LLMBinding) {
	if m == nil || binding == nil {
		return
	}
	key := bindingKey(binding)
	now := time.Now().UTC()
	m.syncMu.Lock()
	fn := m.syncBinding
	if fn == nil {
		m.syncMu.Unlock()
		return
	}
	if last := m.lastSynced[key]; !last.IsZero() && now.Sub(last) < BindingSyncInterval {
		m.syncMu.Unlock()
		return
	}
	m.lastSynced[key] = now
	m.syncMu.Unlock()
	fn(ctx, cloneBinding(binding))
}

// TryBind attempts to bind a tenant to this node.
func (m *LLMBindingManager) TryBind(ctx context.Context, hubID, tenantID string) (bool, *LLMBinding, error) {
	if m == nil || m.repo == nil {
		return false, nil, fmt.Errorf("binding repository is not configured")
	}
	key := hubID + "\x00" + tenantID
	now := time.Now().UTC()

	// Serialize claim/renewal attempts on this node. This cannot replace a
	// cluster-wide compare-and-swap, but it prevents two concurrent requests
	// handled by one HubCenter from racing through the local lease check.
	m.mu.Lock()
	defer m.mu.Unlock()
	existing := m.bindings[key]
	if existing != nil && !bindingExpired(existing, now) && existing.NodeID == m.nodeID {
		renewed := cloneBinding(existing)
		renewed.LastActive = now
		renewed.ExpiresAt = now.Add(BindingLeaseTTL)
		if err := m.repo.Upsert(ctx, renewed); err != nil {
			return false, nil, fmt.Errorf("renew binding: %w", err)
		}
		m.bindings[key] = renewed
		m.sync(ctx, renewed)
		return true, cloneBinding(renewed), nil
	}
	if existing != nil && bindingExpired(existing, now) {
		delete(m.bindings, key)
	}

	m.remoteMu.RLock()
	remote := cloneBinding(m.remoteBindings[key])
	m.remoteMu.RUnlock()
	if remote != nil {
		if !bindingExpired(remote, now) && remote.NodeID != m.nodeID {
			return false, remote, nil
		}
		if bindingExpired(remote, now) {
			// Treat a wall-clock-expired cached lease as a miss and evict it
			// so a laggard tombstone (replication delay or HA outage) cannot
			// keep redirecting tenants to a dead owner for the whole TTL.
			m.remoteMu.Lock()
			delete(m.remoteBindings, key)
			m.remoteMu.Unlock()
		}
	}

	persisted, err := m.repo.Get(ctx, hubID, tenantID)
	if err != nil {
		return false, nil, fmt.Errorf("check binding: %w", err)
	}
	if persisted != nil && !bindingExpired(persisted, now) && persisted.NodeID != m.nodeID {
		m.remoteMu.Lock()
		m.remoteBindings[key] = cloneBinding(persisted)
		m.remoteMu.Unlock()
		return false, cloneBinding(persisted), nil
	}
	if persisted != nil && !bindingExpired(persisted, now) && persisted.NodeID == m.nodeID {
		renewed := cloneBinding(persisted)
		renewed.LastActive = now
		renewed.ExpiresAt = now.Add(BindingLeaseTTL)
		if err := m.repo.Upsert(ctx, renewed); err != nil {
			return false, nil, fmt.Errorf("renew persisted binding: %w", err)
		}
		m.bindings[key] = renewed
		m.sync(ctx, renewed)
		return true, cloneBinding(renewed), nil
	}

	binding := &LLMBinding{
		HubID:      hubID,
		TenantID:   tenantID,
		NodeID:     m.nodeID,
		BoundAt:    now,
		LastActive: now,
		ExpiresAt:  now.Add(BindingLeaseTTL),
	}
	if err := m.repo.Upsert(ctx, binding); err != nil {
		return false, nil, fmt.Errorf("create binding: %w", err)
	}
	m.bindings[key] = binding
	m.sync(ctx, binding)
	return true, cloneBinding(binding), nil
}

// RenewBinding extends the lease for an existing binding.
func (m *LLMBindingManager) RenewBinding(ctx context.Context, hubID, tenantID string) {
	if m == nil || m.repo == nil {
		return
	}
	key := hubID + "\x00" + tenantID
	now := time.Now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	if b := m.bindings[key]; b != nil && b.NodeID == m.nodeID {
		if bindingExpired(b, now) {
			// An expired cached lease must not be revived by a renewal; drop
			// it so the next TryBind competes for a fresh binding.
			delete(m.bindings, key)
			return
		}
		renewed := cloneBinding(b)
		renewed.LastActive = now
		renewed.ExpiresAt = now.Add(BindingLeaseTTL)
		if err := m.repo.Upsert(ctx, renewed); err == nil {
			m.bindings[key] = renewed
			m.sync(ctx, renewed)
		}
	}
}

// ReleaseBindingsForHubNode tombstones every lease this hub holds on the
// given node so its tenants can rebind immediately instead of waiting out
// the TTL. A tombstone loses to any newer LastActive, so an owner that is
// actually alive simply renews over it through normal replication.
func (m *LLMBindingManager) ReleaseBindingsForHubNode(ctx context.Context, hubID, nodeID string, now time.Time) (int, error) {
	if m == nil || m.repo == nil {
		return 0, fmt.Errorf("binding repository is not configured")
	}
	bindings, err := m.repo.ListByNode(ctx, nodeID)
	if err != nil {
		return 0, fmt.Errorf("list bindings for node: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	released := 0
	for _, b := range bindings {
		if b == nil || b.HubID != hubID {
			continue
		}
		// Already-expired rows (a previous tombstone, or a lease that lapsed
		// naturally before cleanup ran) need no tombstone: tenants can already
		// rebind. Skipping them also keeps repeated release calls idempotent
		// instead of forcing a fresh HA op per retry.
		if bindingExpired(b, now) {
			continue
		}
		// The list is a snapshot taken before m.mu was acquired. A concurrent
		// TryBind may have rebound this tenant elsewhere or renewed the lease
		// after the snapshot; re-read the durable record and skip when it no
		// longer matches the target node or is a strictly newer lease, so
		// release never clobbers a live or moved binding.
		current, err := m.repo.Get(ctx, b.HubID, b.TenantID)
		if err != nil {
			return released, fmt.Errorf("check binding: %w", err)
		}
		if current == nil || current.NodeID != nodeID || bindingExpired(current, now) ||
			current.LastActive.After(b.LastActive) || current.ExpiresAt.After(b.ExpiresAt) {
			continue
		}
		key := bindingKey(b)
		tombstone := cloneBinding(current)
		tombstone.LastActive = now
		tombstone.ExpiresAt = now
		if err := m.repo.Upsert(ctx, tombstone); err != nil {
			return released, fmt.Errorf("release binding: %w", err)
		}
		// m.sync throttles per key; a tombstone must replicate even when it
		// lands right after the lease it replaces.
		m.syncMu.Lock()
		delete(m.lastSynced, key)
		m.syncMu.Unlock()
		m.sync(ctx, tombstone)
		delete(m.bindings, key)
		released++
	}
	// Evict cached foreign leases for this hub on the released node even when
	// the repo loop above found nothing (e.g. the records were already
	// cleaned up), so a stale cache entry cannot keep redirecting tenants to
	// a dead owner.
	m.remoteMu.Lock()
	for key, b := range m.remoteBindings {
		if b.HubID == hubID && b.NodeID == nodeID {
			delete(m.remoteBindings, key)
		}
	}
	m.remoteMu.Unlock()
	return released, nil
}

// InvalidateCachedBinding evicts in-memory binding state for a (hub, tenant)
// key. HA replication calls it after applying a binding op written by another
// node, so a replicated tombstone takes effect immediately instead of after
// the cached lease's wall-clock expiry. The next TryBind re-reads the durable
// record and rebuilds whatever cache state is still valid.
func (m *LLMBindingManager) InvalidateCachedBinding(hubID, tenantID string) {
	if m == nil {
		return
	}
	key := hubID + "\x00" + tenantID
	m.mu.Lock()
	delete(m.bindings, key)
	m.mu.Unlock()
	m.remoteMu.Lock()
	delete(m.remoteBindings, key)
	m.remoteMu.Unlock()
}

// GetLocalBindings returns all bindings owned by this node.
func (m *LLMBindingManager) GetLocalBindings() []*LLMBinding {
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := time.Now().UTC()
	var result []*LLMBinding
	for _, b := range m.bindings {
		if !bindingExpired(b, now) {
			result = append(result, cloneBinding(b))
		}
	}
	return result
}

// ApplyRemoteBindings updates remote binding cache from a peer.
func (m *LLMBindingManager) ApplyRemoteBindings(peerNodeID string, bindings []*LLMBinding) {
	m.remoteMu.Lock()
	defer m.remoteMu.Unlock()
	now := time.Now().UTC()
	for key, b := range m.remoteBindings {
		if b.NodeID == peerNodeID {
			delete(m.remoteBindings, key)
		}
	}
	for _, b := range bindings {
		if b.NodeID == peerNodeID && !bindingExpired(b, now) {
			m.remoteBindings[bindingKey(b)] = cloneBinding(b)
		}
	}
}

// CleanupExpired removes expired bindings.
func (m *LLMBindingManager) CleanupExpired(ctx context.Context) {
	now := time.Now().UTC()
	m.mu.Lock()
	for key, b := range m.bindings {
		if bindingExpired(b, now) {
			delete(m.bindings, key)
		}
	}
	m.mu.Unlock()
	m.remoteMu.Lock()
	for key, b := range m.remoteBindings {
		if bindingExpired(b, now) {
			delete(m.remoteBindings, key)
		}
	}
	m.remoteMu.Unlock()
	_, _ = m.repo.DeleteExpired(ctx, now)
}

// BindingSyncMessage is broadcast between HubCenter nodes.
type BindingSyncMessage struct {
	NodeID   string        `json:"node_id"`
	Bindings []*LLMBinding `json:"bindings"`
	SentAt   time.Time     `json:"sent_at"`
}
