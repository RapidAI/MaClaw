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
	ID          string    `json:"id"`
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

// Create adds a new task and returns its ID.
func (s *Store) Create(title, description string, dependsOn []string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counter++
	id := fmt.Sprintf("task-%d", s.counter)
	now := time.Now()

	// Validate and filter dependencies: only keep IDs that exist in the store.
	var validDeps []string
	status := StatusPending
	for _, dep := range dependsOn {
		if _, ok := s.tasks[dep]; ok {
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
		Title:       title,
		Description: description,
		Status:      status,
		DependsOn:   validDeps,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	return id
}

// Update modifies a task's status and/or note.
func (s *Store) Update(id string, status Status, note string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
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
		s.unblockDependents(id)
	}
	return nil
}

// Delegate assigns a task to a session or agent.
func (s *Store) Delegate(id, delegateTo string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task %s not found", id)
	}
	t.DelegatedTo = delegateTo
	t.Status = StatusInProgress
	t.UpdatedAt = time.Now()
	return nil
}

// Get returns a task by ID.
func (s *Store) Get(id string) (*Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, false
	}
	cp := *t
	return &cp, true
}

// List returns all tasks.
func (s *Store) List() []*Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		cp := *t
		result = append(result, &cp)
	}
	return result
}

// Delete removes a task.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[id]; !ok {
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
// dependency is completed.
func (s *Store) unblockDependents(completedID string) {
	for _, t := range s.tasks {
		if t.Status != StatusBlocked {
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
