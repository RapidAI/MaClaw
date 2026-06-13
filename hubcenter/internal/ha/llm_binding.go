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

// LLMBindingManager manages tenant-node bindings for the current node.
type LLMBindingManager struct {
	nodeID   string
	repo     LLMBindingRepository
	mu       sync.RWMutex
	bindings map[string]*LLMBinding
	remoteMu       sync.RWMutex
	remoteBindings map[string]*LLMBinding
}

// NewLLMBindingManager creates a binding manager for the given node.
func NewLLMBindingManager(nodeID string, repo LLMBindingRepository) *LLMBindingManager {
	return &LLMBindingManager{
		nodeID:         nodeID,
		repo:           repo,
		bindings:       map[string]*LLMBinding{},
		remoteBindings: map[string]*LLMBinding{},
	}
}

// TryBind attempts to bind a tenant to this node.
func (m *LLMBindingManager) TryBind(ctx context.Context, hubID, tenantID string) (bool, *LLMBinding, error) {
	key := hubID + "\x00" + tenantID
	now := time.Now().UTC()

	m.mu.RLock()
	existing := m.bindings[key]
	m.mu.RUnlock()
	if existing != nil && !bindingExpired(existing, now) && existing.NodeID == m.nodeID {
		existing.LastActive = now
		existing.ExpiresAt = now.Add(BindingLeaseTTL)
		_ = m.repo.Upsert(ctx, existing)
		return true, existing, nil
	}

	m.remoteMu.RLock()
	remote := m.remoteBindings[key]
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
		m.remoteBindings[key] = persisted
		m.remoteMu.Unlock()
		return false, persisted, nil
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
	m.mu.Lock()
	m.bindings[key] = binding
	m.mu.Unlock()
	return true, binding, nil
}

// RenewBinding extends the lease for an existing binding.
func (m *LLMBindingManager) RenewBinding(ctx context.Context, hubID, tenantID string) {
	key := hubID + "\x00" + tenantID
	now := time.Now().UTC()
	m.mu.Lock()
	if b := m.bindings[key]; b != nil && b.NodeID == m.nodeID {
		b.LastActive = now
		b.ExpiresAt = now.Add(BindingLeaseTTL)
		_ = m.repo.Upsert(ctx, b)
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
			result = append(result, b)
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
			m.remoteBindings[bindingKey(b)] = b
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
