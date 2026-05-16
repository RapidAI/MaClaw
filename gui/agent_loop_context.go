package main

import (
	"context"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"net/http"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// LoopContext - per-loop mutable state, replacing shared fields on handler
// ---------------------------------------------------------------------------

// LoopContext holds per-loop mutable state, eliminating shared fields on the
// handler. Each agent loop (chat or background) gets its own LoopContext.
type LoopContext struct {
	ID          string   // unique loop identifier (e.g. "chat", "bg-coding-xxx")
	Kind        LoopKind // Chat or Background
	SlotKind    SlotKind // Coding, Scheduled, Auto (Background only)
	Description string   // human-readable task description

	mu            sync.RWMutex
	maxIterations int // current max iterations for this loop
	iteration     int // current iteration count
	status        LoopState

	Conversation []interface{}             // this loop's conversation messages
	History      []agent.ConversationEntry // loaded history (for chat loops)

	ContinueC chan int         // receive additional rounds (Background only)
	StatusC   chan StatusEvent // send status events to Chat Loop
	CancelC   chan struct{}    // signal to stop the loop
	DoneC     chan struct{}    // closed when runAgentLoop exits; used to wait for cleanup

	HTTPClient *http.Client // chat or task client
	SessionID  string       // associated remote session (if any)
	JobID      string       // associated trace job
	RunID      string       // associated trace run
	Platform   string       // originating IM platform ("desktop", "weixin_local", etc.)
	UserID     string       // owning user/session ID (e.g. "desktop-user", "desktop-user:{path}")
	Lang       string       // user language ("zh", "en"); used by i18n.T for progress messages
	StartedAt  time.Time    // when this loop was spawned

	// SkipNeedsConfirmGate is set only for non-review continuations that have
	// their own interaction state, such as attachment bypasses or ask_user
	// replies. A workflow phase awaiting review overrides this flag so every
	// NeedsConfirm phase still stops for explicit confirmation or supplement.
	//
	// This flag also bypasses the Coding Tool Gate unless the current message
	// has a genuine coding classification. When the message IS a new coding
	// task (intent=coding), the coding gate enforces the three-phase flow
	// regardless of this flag.
	SkipNeedsConfirmGate bool

	// IsAskUserResponse is true when the current message is a response to a
	// previous ask_user tool question. In this case the user's text is a
	// continuation of an existing task, not a new independent request. The
	// agent loop should skip task-level routing decisions that assume a fresh
	// task (e.g. Skill preference evaluation), because the task context is
	// already established from the previous turn.
	IsAskUserResponse bool
}

// NewLoopContext creates a LoopContext for a chat loop.
func NewLoopContext(id string, maxIter int, httpClient *http.Client) *LoopContext {
	return &LoopContext{
		ID:            id,
		Kind:          LoopKindChat,
		maxIterations: maxIter,
		status:        LoopStateRunning,
		CancelC:       make(chan struct{}),
		DoneC:         make(chan struct{}),
		HTTPClient:    httpClient,
		StartedAt:     time.Now(),
	}
}

// NewBackgroundLoopContext creates a LoopContext for a background loop.
// parentCtx is an optional external cancellation context (e.g. scheduler
// timeout). When parentCtx is cancelled, the loop's CancelC is automatically
// closed. Pass nil if no external cancellation is needed.
func NewBackgroundLoopContext(id string, slotKind SlotKind, description string,
	maxIter int, httpClient *http.Client, statusC chan StatusEvent) *LoopContext {
	ctx := &LoopContext{
		ID:            id,
		Kind:          LoopKindBackground,
		SlotKind:      slotKind,
		Description:   description,
		maxIterations: maxIter,
		status:        LoopStateRunning,
		ContinueC:     make(chan int, 1),
		StatusC:       statusC,
		CancelC:       make(chan struct{}),
		DoneC:         make(chan struct{}),
		HTTPClient:    httpClient,
		StartedAt:     time.Now(),
	}
	return ctx
}

// BindParentContext attaches an external context.Context as a cancellation
// parent. When parentCtx is cancelled (e.g. scheduler timeout, shutdown),
// the loop's CancelC is automatically closed. This is the standard
// context composition pattern — child inherits parent's cancellation.
//
// Must be called before runAgentLoop starts. The internal goroutine exits
// when either parentCtx is cancelled OR DoneC is closed (loop finishes),
// whichever comes first — no goroutine leak.
func (c *LoopContext) BindParentContext(parentCtx context.Context) {
	if parentCtx == nil {
		return
	}
	go func() {
		select {
		case <-parentCtx.Done():
			c.Cancel()
		case <-c.DoneC:
			// loop finished normally; goroutine exits cleanly
		}
	}()
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
	return c.status.String()
}

// LoopState returns the current typed loop state (thread-safe).
func (c *LoopContext) LoopState() LoopState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

// SetState sets the status string (thread-safe).
func (c *LoopContext) SetState(s string) {
	c.SetLoopState(normalizeLoopState(s))
}

// SetLoopState sets the typed loop state (thread-safe).
func (c *LoopContext) SetLoopState(s LoopState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status = normalizeLoopState(s.String())
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

// Context returns a context.Context that is cancelled when CancelC is closed.
// This allows passing cancellation into HTTP requests and other context-aware APIs.
// The returned cancel func MUST be called when the context is no longer needed
// to avoid goroutine leaks.
func (c *LoopContext) Context() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-c.CancelC:
			cancel()
		case <-ctx.Done():
			// caller cancelled; goroutine exits cleanly
		}
	}()
	return ctx, cancel
}

// Done marks the loop as finished. Must be called exactly once when
// runAgentLoop exits.
func (c *LoopContext) Done() {
	select {
	case <-c.DoneC:
	default:
		close(c.DoneC)
	}
}

// ---------------------------------------------------------------------------
// StatusEvent - background to chat loop events
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
	LoopID    string            // which background loop
	SessionID string            // related coding session (if any)
	Message   string            // human-readable description
	Remaining int               // remaining iterations (for ApproachingLimit)
	Extra     map[string]string // optional key-value metadata (e.g. screenshot)
}
