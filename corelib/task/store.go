// Package task provides a lightweight in-memory task store for the agent loop.
// Unlike the full Swarm orchestrator, this is a simple task tracker that the
// LLM can use to manage its own work items, track dependencies, and share
// progress across sub-agents.
package task

import (
	"fmt"
	"sync"
	"time"
)

// Status represents the lifecycle state of a task.
type Status string

const (
	StatusPending    Status = "pending"
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
	StatusBlocked    Status = "blocked"
)

// Task represents a single work item managed by the agent.
type Task struct {
	ID string `json:"id"`
	// OwnerID scopes a task to the principal it belongs to. Empty means the
	// single-user surfaces — TUI and the un-migrated GUI task tool — which
	// have no principal to scope by and share one list, as they always have.
	//
	// Ownership lives on the record rather than in a map of per-owner stores
	// so that there is one storage model rather than two, and so a filtered
	// List cannot be bypassed by reaching for a different store.
	OwnerID     string    `json:"owner_id,omitempty"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Status      Status    `json:"status"`
	DependsOn   []string  `json:"depends_on,omitempty"`
	DelegatedTo string    `json:"delegated_to,omitempty"` // session ID or agent name
	StatusNote  string    `json:"status_note,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Store is a thread-safe in-memory task store.
type Store struct {
	mu      sync.RWMutex
	tasks   map[string]*Task
	counter int
}

// NewStore creates an empty task store.
func NewStore() *Store {
	return &Store{tasks: make(map[string]*Task)}
}

// Create adds a new unowned task and returns its ID.
func (s *Store) Create(title, description string, dependsOn []string) string {
	return s.CreateOwned("", title, description, dependsOn)
}

// CreateOwned adds a task belonging to ownerID.
func (s *Store) CreateOwned(ownerID, title, description string, dependsOn []string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counter++
	id := fmt.Sprintf("task-%d", s.counter)
	now := time.Now()

	// Validate and filter dependencies: only keep IDs this owner can see.
	// Accepting another owner's ID would both cross the scope and disclose
	// that the task exists, since a rejected dependency is silently dropped
	// while an accepted one can block this task.
	var validDeps []string
	status := StatusPending
	for _, dep := range dependsOn {
		if t, ok := s.tasks[dep]; ok && t.OwnerID == ownerID {
			validDeps = append(validDeps, dep)
		}
	}
	// Auto-block if any valid dependency is not completed
	for _, dep := range validDeps {
		if t := s.tasks[dep]; t.Status != StatusCompleted {
			status = StatusBlocked
			break
		}
	}
	s.tasks[id] = &Task{
		ID:          id,
		OwnerID:     ownerID,
		Title:       title,
		Description: description,
		Status:      status,
		DependsOn:   validDeps,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	return id
}

// ownedTask resolves a task only when it belongs to ownerID.
//
// A mismatch is reported as absence rather than as a distinct error: telling
// one principal that another principal's task ID exists but is not theirs is
// itself the disclosure this scoping is meant to prevent. Callers must hold
// the lock.
func (s *Store) ownedTask(ownerID, id string) (*Task, bool) {
	t, ok := s.tasks[id]
	if !ok || t.OwnerID != ownerID {
		return nil, false
	}
	return t, true
}

// Update modifies an unowned task's status and/or note.
func (s *Store) Update(id string, status Status, note string) error {
	return s.UpdateOwned("", id, status, note)
}

// UpdateOwned modifies a task belonging to ownerID.
func (s *Store) UpdateOwned(ownerID, id string, status Status, note string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.ownedTask(ownerID, id)
	if !ok {
		return fmt.Errorf("task %s not found", id)
	}
	if status != "" {
		t.Status = status
	}
	if note != "" {
		t.StatusNote = note
	}
	t.UpdatedAt = time.Now()
	// When a task completes, unblock dependents
	if status == StatusCompleted {
		s.unblockDependents(ownerID, id)
	}
	return nil
}

// Delegate assigns an unowned task to a session or agent. There is no owned
// form because the managed surface does not expose delegation; scoping it to
// the empty owner keeps the un-migrated tool from reaching an owned task.
func (s *Store) Delegate(id, delegateTo string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.ownedTask("", id)
	if !ok {
		return fmt.Errorf("task %s not found", id)
	}
	t.DelegatedTo = delegateTo
	t.Status = StatusInProgress
	t.UpdatedAt = time.Now()
	return nil
}

// Get returns an unowned task by ID.
func (s *Store) Get(id string) (*Task, bool) {
	return s.GetOwned("", id)
}

// GetOwned returns a task by ID only when it belongs to ownerID.
func (s *Store) GetOwned(ownerID, id string) (*Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.ownedTask(ownerID, id)
	if !ok {
		return nil, false
	}
	cp := *t
	return &cp, true
}

// List returns the unowned tasks.
func (s *Store) List() []*Task {
	return s.ListOwned("")
}

// ListOwned returns the tasks belonging to ownerID.
func (s *Store) ListOwned(ownerID string) []*Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		if t.OwnerID != ownerID {
			continue
		}
		cp := *t
		result = append(result, &cp)
	}
	return result
}

// Delete removes an unowned task.
func (s *Store) Delete(id string) error {
	return s.DeleteOwned("", id)
}

// DeleteOwned removes a task belonging to ownerID.
func (s *Store) DeleteOwned(ownerID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.ownedTask(ownerID, id); !ok {
		return fmt.Errorf("task %s not found", id)
	}
	delete(s.tasks, id)
	return nil
}

// Reset clears all tasks (e.g. on session reset).
func (s *Store) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = make(map[string]*Task)
	s.counter = 0
}

// unblockDependents transitions blocked tasks to pending when their
// dependency is completed. It stays within one owner because dependencies
// can only be formed within one owner.
func (s *Store) unblockDependents(ownerID, completedID string) {
	for _, t := range s.tasks {
		if t.Status != StatusBlocked || t.OwnerID != ownerID {
			continue
		}
		allMet := true
		for _, dep := range t.DependsOn {
			if d, ok := s.tasks[dep]; ok && d.Status != StatusCompleted {
				allMet = false
				break
			}
		}
		if allMet {
			t.Status = StatusPending
			t.UpdatedAt = time.Now()
		}
	}
}
