package agent

import (
	"net/http"
	"sync"
	"time"
)

// LoopContext holds per-loop mutable state. Each agent loop (chat or
// background) gets its own LoopContext.
type LoopContext struct {
	ID          string   // unique loop identifier (e.g. "chat", "bg-coding-xxx")
	Kind        LoopKind // Chat or Background
	SlotKind    SlotKind // Coding, Scheduled, Auto (Background only)
	Description string   // human-readable task description

	mu            sync.RWMutex
	maxIterations int
	iteration     int
	status        string

	Conversation []interface{} // this loop's conversation messages
	History      []interface{} // loaded history (for chat loops)

	ContinueC chan int         // receive additional rounds (Background only)
	StatusC   chan StatusEvent // send status events to Chat Loop
	CancelC   chan struct{}    // signal to stop the loop

	HTTPClient *http.Client
	SessionID  string    // associated remote session (if any)
	StartedAt  time.Time // when this loop was spawned
}

// NewLoopContext creates a LoopContext for a chat loop.
func NewLoopContext(id string, maxIter int, httpClient *http.Client) *LoopContext {
	return &LoopContext{
		ID:            id,
		Kind:          LoopKindChat,
		maxIterations: maxIter,
		status:        LoopStateRunning.String(),
		CancelC:       make(chan struct{}),
		HTTPClient:    httpClient,
		StartedAt:     time.Now(),
	}
}

// NewBackgroundLoopContext creates a LoopContext for a background loop.
func NewBackgroundLoopContext(id string, slotKind SlotKind, description string,
	maxIter int, httpClient *http.Client, statusC chan StatusEvent) *LoopContext {
	return &LoopContext{
		ID:            id,
		Kind:          LoopKindBackground,
		SlotKind:      slotKind,
		Description:   description,
		maxIterations: maxIter,
		status:        LoopStateRunning.String(),
		ContinueC:     make(chan int, 1),
		StatusC:       statusC,
		CancelC:       make(chan struct{}),
		HTTPClient:    httpClient,
		StartedAt:     time.Now(),
	}
}

// MaxIterations returns the current max iterations (thread-safe).
func (c *LoopContext) MaxIterations() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.maxIterations
}

// SetMaxIterations sets the max iterations (thread-safe).
func (c *LoopContext) SetMaxIterations(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.maxIterations = n
}

// AddMaxIterations atomically adds n to max iterations.
func (c *LoopContext) AddMaxIterations(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.maxIterations += n
}

// Iteration returns the current iteration count (thread-safe).
func (c *LoopContext) Iteration() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.iteration
}

// SetIteration sets the current iteration count (thread-safe).
func (c *LoopContext) SetIteration(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.iteration = n
}

// IncrementIteration atomically increments the iteration counter by 1.
func (c *LoopContext) IncrementIteration() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.iteration++
	return c.iteration
}

// State returns the current status string (thread-safe).
func (c *LoopContext) State() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

// StateKind returns the current status as a typed state.
func (c *LoopContext) StateKind() LoopStateKind {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return normalizeLoopStateKind(c.status)
}

// SetState sets the status string (thread-safe).
func (c *LoopContext) SetState(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status = s
}

// SetStateKind sets the status using a typed state.
func (c *LoopContext) SetStateKind(s LoopStateKind) {
	c.SetState(s.String())
}

// Cancel signals the loop to stop.
func (c *LoopContext) Cancel() {
	select {
	case <-c.CancelC:
	default:
		close(c.CancelC)
	}
}

// IsCancelled returns true if the loop has been cancelled.
func (c *LoopContext) IsCancelled() bool {
	select {
	case <-c.CancelC:
		return true
	default:
		return false
	}
}
