package memoryshot

import (
	"sync"
	"time"
)

// Manager provides a high-level interface for managing snapshots.
// It handles periodic saves and coordinates between UI and storage.
type Manager struct {
	mu        sync.RWMutex
	store     *Store
	snapshot  *Snapshot
	dirty     bool
	stopCh    chan struct{}
	stopOnce  sync.Once
}

// NewManager creates a new Manager with the given store.
func NewManager(store *Store) *Manager {
	return &Manager{
		store:    store,
		snapshot: &Snapshot{},
		stopCh:   make(chan struct{}),
	}
}

// Start begins the background auto-save goroutine.
// The manager will save every interval (default 30s) if dirty.
func (m *Manager) Start(autoSaveInterval time.Duration) {
	if autoSaveInterval <= 0 {
		autoSaveInterval = 30 * time.Second
	}

	go m.autoSaveLoop(autoSaveInterval)
}

// Stop stops the background auto-save and performs a final save if dirty.
func (m *Manager) Stop() {
	m.stopOnce.Do(func() {
		close(m.stopCh)

		// Final save if dirty
		m.mu.RLock()
		dirty := m.dirty
		m.mu.RUnlock()

		if dirty {
			_ = m.Save()
		}
	})
}

// autoSaveLoop periodically saves if dirty.
func (m *Manager) autoSaveLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.mu.RLock()
			dirty := m.dirty
			m.mu.RUnlock()

			if dirty {
				_ = m.Save()
			}
		}
	}
}

// Save immediately persists the current snapshot to disk.
func (m *Manager) Save() error {
	m.mu.RLock()
	snapshot := m.snapshot.Clone()
	m.mu.RUnlock()

	if err := m.store.Save(snapshot); err != nil {
		return err
	}

	m.mu.Lock()
	m.dirty = false
	m.mu.Unlock()

	return nil
}

// Load reads the snapshot from disk and replaces the current one.
// Returns true if a snapshot was found and loaded.
func (m *Manager) Load() (bool, error) {
	snapshot, err := m.store.Load()
	if err != nil {
		return false, err
	}

	if snapshot == nil {
		return false, nil
	}

	m.mu.Lock()
	m.snapshot = snapshot
	m.dirty = false
	m.mu.Unlock()

	return true, nil
}

// Clear removes the snapshot from disk and memory.
func (m *Manager) Clear() error {
	m.mu.Lock()
	m.snapshot = &Snapshot{}
	m.dirty = false
	m.mu.Unlock()

	return m.store.Clear()
}

// GetSnapshot returns a copy of the current snapshot.
func (m *Manager) GetSnapshot() *Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshot.Clone()
}

// SetSnapshot replaces the current snapshot.
func (m *Manager) SetSnapshot(snapshot *Snapshot) {
	m.mu.Lock()
	m.snapshot = snapshot
	m.dirty = true
	m.mu.Unlock()
}

// UpdateChatHistory updates the chat history in the snapshot.
func (m *Manager) UpdateChatHistory(history []ChatMessage) {
	m.mu.Lock()
	m.snapshot.ChatHistory = history
	m.dirty = true
	m.mu.Unlock()
}

// GetChatHistory returns the current chat history.
func (m *Manager) GetChatHistory() []ChatMessage {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.snapshot.ChatHistory) == 0 {
		return nil
	}

	result := make([]ChatMessage, len(m.snapshot.ChatHistory))
	copy(result, m.snapshot.ChatHistory)
	return result
}

// AppendChatMessage appends a message to the chat history.
func (m *Manager) AppendChatMessage(role, content string) {
	m.mu.Lock()
	m.snapshot.ChatHistory = append(m.snapshot.ChatHistory, ChatMessage{
		Role:      role,
		Content:   content,
		Timestamp: time.Now().Unix(),
	})
	m.dirty = true
	m.mu.Unlock()
}

// ClearChatHistory clears the chat history.
func (m *Manager) ClearChatHistory() {
	m.mu.Lock()
	m.snapshot.ChatHistory = nil
	m.dirty = true
	m.mu.Unlock()
}

// SetCurrentProject sets the current project path.
func (m *Manager) SetCurrentProject(path string) {
	m.mu.Lock()
	m.snapshot.CurrentProject = path
	m.dirty = true
	m.mu.Unlock()
}

// GetCurrentProject returns the current project path.
func (m *Manager) GetCurrentProject() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshot.CurrentProject
}

// SetActiveTool sets the currently active tool.
func (m *Manager) SetActiveTool(tool *ToolState) {
	m.mu.Lock()
	m.snapshot.ActiveTool = tool
	m.dirty = true
	m.mu.Unlock()
}

// GetActiveTool returns the currently active tool.
func (m *Manager) GetActiveTool() *ToolState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.snapshot.ActiveTool == nil {
		return nil
	}

	clone := *m.snapshot.ActiveTool
	if m.snapshot.ActiveTool.Metadata != nil {
		clone.Metadata = make(map[string]string)
		for k, v := range m.snapshot.ActiveTool.Metadata {
			clone.Metadata[k] = v
		}
	}
	return &clone
}

// SetUIState updates the UI state.
func (m *Manager) SetUIState(state UIState) {
	m.mu.Lock()
	m.snapshot.UIState = state
	m.dirty = true
	m.mu.Unlock()
}

// GetUIState returns the current UI state.
func (m *Manager) GetUIState() UIState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshot.UIState
}

// IsDirty returns true if there are unsaved changes.
func (m *Manager) IsDirty() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.dirty
}

// MarkDirty marks the snapshot as needing a save.
func (m *Manager) MarkDirty() {
	m.mu.Lock()
	m.dirty = true
	m.mu.Unlock()
}

// DefaultManager creates a Manager at the default location.
func DefaultManager() (*Manager, error) {
	store, err := DefaultStore()
	if err != nil {
		return nil, err
	}
	return NewManager(store), nil
}
