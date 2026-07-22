package main

import (
	"context"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"net/http"
	"strings"
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

	mu                                  sync.RWMutex
	maxIterations                       int // current max iterations for this loop
	iteration                           int // current iteration count
	status                              LoopState
	replanRevision                      int64 // increments when live user guidance should interrupt/re-plan
	replansSealed                       bool  // final response won the accept/commit race; reject later steering
	currentOperationCancel              context.CancelFunc
	currentOperation                    *loopReplannableOperation
	backgroundTaskBoundaryExtensionKeys map[string]struct{}

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
	Runtime    RuntimeContext

	// codeSessionID scopes source-preview events emitted by nested coding
	// SubAgents to the same UI code session opened by the workflow runner.
	codeSessionID string

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

	// WorkflowAgentLoop is true when this loop was launched by the workflow
	// engine to produce the current phase deliverable. Workflow tool policy must
	// remain active in this case even if the user message was a confirmation that
	// set SkipNeedsConfirmGate.
	WorkflowAgentLoop bool

	// WorkflowDocBuffer accumulates all non-tool text output across iterations
	// during a V2 workflow agent loop. Used by captureWorkflowDocAfterAgentLoop
	// to capture the complete phase document instead of relying on resp.Text
	// (which only contains the last iteration's text).
	WorkflowDocBuffer strings.Builder

	// WorkflowDocPhase is true when this V2 workflow agent loop is running a
	// phase that requires user confirmation (NeedsConfirm=true). The LLM produces
	// structured output (analysis report, design doc, task list) and the system
	// waits for user review before advancing to the next phase.
	// The phase MAY still use tools (e.g. read_file to parse a disclosure document)
	// but its primary output is a confirmable document, not free-form execution.
	// When false (implementation, verification), the LLM freely uses tools to
	// complete the task without intermediate confirmation.
	WorkflowDocPhase bool

	// WorkflowPhaseID is the active V2 phase ID for protocol-aware cleanup of
	// model output before it reaches workflow persistence or UI.
	WorkflowPhaseID string

	// SkipWorkflowDocCapture is set for workflow-launched execution paths whose
	// response is already a terminal execution report rather than a reviewable
	// phase document (e.g. pure-coding workbench turns that already emit a final report).
	SkipWorkflowDocCapture bool

	// WorkflowWrittenFiles tracks files produced during a workflow agent loop,
	// regardless of which tool created them. Sources:
	//   - write_file: file written directly to disk
	//   - send_file / send_to_im: file delivered to user (created via bash/Python/etc)
	// Used by captureWorkflowDocAfterAgentLoop to read the actual document
	// content when the LLM produces output via tools instead of streaming text.
	// Each entry is an absolute file path that was successfully written/delivered.
	WorkflowWrittenFiles []string

	// IsAskUserResponse is true when the current message is a response to a
	// previous ask_user tool question. In this case the user's text is a
	// continuation of an existing task, not a new independent request. The
	// agent loop should skip task-level routing decisions that assume a fresh
	// task (e.g. Skill preference evaluation), because the task context is
	// already established from the previous turn.
	IsAskUserResponse bool

	// CodingAttachments carries user image/file attachments into pure-coding
	// SubAgent turns (create-task coding_dev / remote_coding_dev). Cleared when
	// the template path finishes so follow-up re-arms do not re-send stale media.
	CodingAttachments []agent.MessageAttachment
}

type loopReplannableOperation struct {
	cancel context.CancelFunc
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

func (c *LoopContext) MarkBackgroundTaskBoundaryExtended(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := strings.Split(key, ",")
	if c.backgroundTaskBoundaryExtensionKeys == nil {
		c.backgroundTaskBoundaryExtensionKeys = make(map[string]struct{}, len(keys))
	}
	hasNewKey := false
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if _, exists := c.backgroundTaskBoundaryExtensionKeys[k]; !exists {
			hasNewKey = true
		}
	}
	if !hasNewKey {
		return false
	}
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k != "" {
			c.backgroundTaskBoundaryExtensionKeys[k] = struct{}{}
		}
	}
	return true
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

func (c *LoopContext) RequestReplan() int64 {
	revision, _ := c.TryRequestReplan(nil)
	return revision
}

// TryRequestReplan atomically queues steering and advances the replan revision.
// enqueue runs while the final-response gate is held, so an accepted guide can
// never lose a race to final response commit.
func (c *LoopContext) TryRequestReplan(enqueue func()) (int64, bool) {
	if c == nil {
		return 0, false
	}
	c.mu.Lock()
	if c.replansSealed || c.isCancelledLocked() {
		c.mu.Unlock()
		return c.replanRevision, false
	}
	if enqueue != nil {
		enqueue()
	}
	c.replanRevision++
	cancel := c.currentOperationCancel
	revision := c.replanRevision
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return revision, true
}

// TrySealReplans atomically commits a final-response boundary. It succeeds
// only when TransformConversation has incorporated every accepted revision.
func (c *LoopContext) TrySealReplans(processedRevision int64) bool {
	if c == nil {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.replanRevision > processedRevision {
		return false
	}
	c.replansSealed = true
	return true
}

func (c *LoopContext) AcceptingReplans() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return !c.replansSealed && !c.isCancelledLocked()
}

func (c *LoopContext) ReplanRevision() int64 {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.replanRevision
}

func (c *LoopContext) BeginReplannableOperation(parent context.Context) (context.Context, context.CancelFunc, int64) {
	if c == nil {
		if parent == nil {
			parent = context.Background()
		}
		ctx, cancel := context.WithCancel(parent)
		return ctx, cancel, 0
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	op := &loopReplannableOperation{cancel: cancel}
	c.mu.Lock()
	c.currentOperationCancel = cancel
	c.currentOperation = op
	revision := c.replanRevision
	c.mu.Unlock()
	go func() {
		select {
		case <-c.CancelC:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, func() { c.endReplannableOperation(op) }, revision
}

func (c *LoopContext) endReplannableOperation(op *loopReplannableOperation) {
	if c == nil || op == nil {
		return
	}
	op.cancel()
	c.mu.Lock()
	if c.currentOperation == op {
		c.currentOperationCancel = nil
		c.currentOperation = nil
	}
	c.mu.Unlock()
}

func (c *LoopContext) ReplanRequestedSince(revision int64) bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.replanRevision > revision
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
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.CancelC:
		// already closed
	default:
		close(c.CancelC)
	}
}

// isCancelledLocked reports cancellation while c.mu is held. Cancel closes
// CancelC under the same mutex, making cancellation and steer acceptance one
// atomic decision instead of a check-then-enqueue race.
func (c *LoopContext) isCancelledLocked() bool {
	select {
	case <-c.CancelC:
		return true
	default:
		return false
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
