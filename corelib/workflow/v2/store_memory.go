package v2

import "sync"

// MemoryStore is an in-memory WorkflowStore for testing.
// It never touches the filesystem.
type MemoryStore struct {
	mu     sync.RWMutex
	states map[string]*WorkflowState
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{states: make(map[string]*WorkflowState)}
}

func (m *MemoryStore) Save(state *WorkflowState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[state.UserID] = state
	return nil
}

func (m *MemoryStore) Load(userID string) (*WorkflowState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.states[userID], nil
}

func (m *MemoryStore) Delete(userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.states, userID)
	return nil
}
