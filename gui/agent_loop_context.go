package main

import (
	"net/http"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// LoopContext — per-loop mutable state, replacing shared fields on handler
// ---------------------------------------------------------------------------

// LoopKind distinguishes front-end chat loops from background loops.
type LoopKind int

const (
	LoopKindChat       LoopKind = iota // interactive user chat
	LoopKindBackground                 // background task (coding, scheduled, auto)
)

// SlotKind categorizes background loops for concurrency control.
type SlotKind int

const (
	SlotKindCoding    SlotKind = iota // 编程任务 — max 1
	SlotKindScheduled                 // 定时任务 — max 1
	SlotKindAuto                      // ClawNet 自动任务 — max 1
	SlotKindSSH                       // SSH 远程会话 — max 10
	SlotKindBrowser                   // 浏览器任务 — max 2
	SlotKindGUI                       // GUI 桌面自动化任务 — max 1
)

// SlotKindString returns a human-readable label for the slot kind.
func (s SlotKind) String() string {
	switch s {
	case SlotKindCoding:
		return "coding"
	case SlotKindScheduled:
		return "scheduled"
	case SlotKindAuto:
		return "auto"
	case SlotKindSSH:
		return "ssh"
	case SlotKindBrowser:
		return "browser"
	case SlotKindGUI:
		return "gui"
	default:
		return "unknown"
	}
}

// LoopContext holds per-loop mutable state, eliminating shared fields on the
// handler. Each agent loop (chat or background) gets its own LoopContext.
type LoopContext struct {
	ID          string   // unique loop identifier (e.g. "chat", "bg-coding-xxx")
	Kind        LoopKind // Chat or Background
	SlotKind    SlotKind // Coding, Scheduled, Auto (Background only)
	Description string   // human-readable task description

	mu            sync.RWMutex
	maxIterations int    // current max iterations for this loop
	iteration     int    // current iteration count
	status        string // "running", "paused", "completed", "failed"

	Conversation []interface{}       // this loop's conversation messages
	History      []conversationEntry // loaded history (for chat loops)

	ContinueC chan int         // receive additional rounds (Background only)
	StatusC   chan StatusEvent // send status events to Chat Loop
	CancelC   chan struct{}    // signal to stop the loop

	HTTPClient *http.Client // chat or task client
	SessionID  string       // associated remote session (if any)
	Platform   string       // originating IM platform ("desktop", "weixin_local", etc.)
	Lang       string       // user language ("zh", "en"); used by i18n.T for progress messages
	StartedAt  time.Time    // when this loop was spawned
}

// NewLoopContext creates a LoopContext for a chat loop.
func NewLoopContext(id string, maxIter int, httpClient *http.Client) *LoopContext {
	return &LoopContext{
		ID:            id,
		Kind:          LoopKindChat,
		maxIterations: maxIter,
		status:        "running",
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
		status:        "running",
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

// SetState sets the status string (thread-safe).
func (c *LoopContext) SetState(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status = s
}

// Cancel signals the loop to stop.
func (c *LoopContext) Cancel() {
	select {
	case <-c.CancelC:
		// already closed
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

// ---------------------------------------------------------------------------
// StatusEvent — background → chat loop events
// ---------------------------------------------------------------------------

// StatusEventType enumerates the kinds of events a background loop can emit.
type StatusEventType int

const (
	StatusEventSessionCompleted StatusEventType = iota
	StatusEventSessionFailed
	StatusEventApproachingLimit
	StatusEventStopped
	StatusEventProgress
)

// StatusEvent is pushed from a background loop (or SessionMonitor) to the
// chat loop to inform it about state changes.
type StatusEvent struct {
	Type      StatusEventType
	LoopID    string // which background loop
	SessionID string // related coding session (if any)
	Message   string // human-readable description
	Remaining int    // remaining iterations (for ApproachingLimit)
	Extra     map[string]string // optional key-value metadata (e.g. screenshot)
}
