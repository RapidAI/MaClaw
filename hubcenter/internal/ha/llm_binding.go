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
	return b == nil || now.After(b.ExpiresAt)
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
	if remote != nil && !bindingExpired(remote, now) && remote.NodeID != m.nodeID {
		return false, remote, nil
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
	if b := m.bindings[key]; b != nil && b.NodeID == m.nodeID {
		renewed := cloneBinding(b)
		renewed.LastActive = now
		renewed.ExpiresAt = now.Add(BindingLeaseTTL)
		if err := m.repo.Upsert(ctx, renewed); err == nil {
			m.bindings[key] = renewed
			m.sync(ctx, renewed)
		}
	}
	m.mu.Unlock()
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
