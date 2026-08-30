package main

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
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

	mu                     sync.RWMutex
	maxIterations          int // current max iterations for this loop
	iteration              int // current iteration count
	status                 LoopState
	replanRevision         int64 // increments when live user guidance should interrupt/re-plan
	replansSealed          bool  // final response won the accept/commit race; reject later steering
	currentOperationCancel context.CancelFunc
	currentOperation       *loopReplannableOperation
	cancelHooks            map[uint64]func()
	nextCancelHookID       uint64
	// semanticTurnGeneration distinguishes replacement of an inbound turn from
	// terminal loop cancellation. A supplied LoopContext may be reused before
	// the previous provider callback has returned, while CancelC intentionally
	// remains reusable only for the lifetime of this loop object. Managed
	// surfaces register a durable-revocation fence against this generation; a
	// fresh ingress advances it and runs the prior generation's fences.
	semanticTurnGeneration              uint64
	semanticTurnFences                  map[uint64]func()
	nextSemanticTurnFenceID             uint64
	semanticTurnReplacementCancels      map[uint64]context.CancelFunc
	nextSemanticTurnReplacementCancelID uint64
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
	// Loop-scoped fail-closed tools unlocked by discover_tool. Not a shared pin.
	discoveredConditionalTools map[string]bool
	// ingressFingerprint is a host-computed digest of the authenticated inbound
	// payload and request-scoped client binding. RequestID only identifies a
	// transport retry inside this exact payload; it must not let a changed body,
	// attachment set, or device tool catalog reuse a prior turn's grants.
	ingressFingerprint string
	// CodingTaskIngressToken is copied from a host-only message field. It is an
	// opaque one-shot capability, not a session/user/runtime identifier.
	CodingTaskIngressToken string
	// semanticInvocation is a host-issued, loop-private identity for the
	// managed semantic surface.  It deliberately does not reuse RequestID,
	// LoopContext.ID, a project path, or a user-controlled task description:
	// those values describe transport/runtime lifetime, not an authorization
	// lineage.  The fields stay private so neither a provider nor the model can
	// select or restore a semantic root by name.
	semanticInvocation semanticLoopInvocationIdentity
	// ComputerUseBlockedForLocalFileWork is a per-turn control-plane fence. It
	// survives tool recovery and dynamic augmentation so a staged attachment
	// cannot accidentally re-enable desktop automation later in the same loop.
	// Explicit @computer / "computer use" requests never set this flag.
	ComputerUseBlockedForLocalFileWork bool
	// ComputerUseFresh is this turn's CU gate "new task" bit (not sticky).
	// It must not be stored on the process-global session.
	ComputerUseFresh bool
	// ComputerUseActive is the latched gate decision for this request.
	ComputerUseActive bool
	// ComputerUseGateSettled is true after the first gateComputerUse for this
	// LoopContext. Prompt rebuilds and tool refresh must not re-run UIC.
	ComputerUseGateSettled bool
	// ComputerUseBegun latches Begin to this RequestID so prompt rebuilds
	// (light→full, post-tool refresh) cannot wipe TaskState.
	ComputerUseBegun bool
	// ComputerUseRoutingText is the same gate input used for tool injection
	// and the Computer Use playbook (not CompactQueryForEmbedding).
	ComputerUseRoutingText string
	// ComputerUseOwner is the SessionKey/UserID used for this turn's CU
	// session and TaskState. Playbook extra must use it, not the process-global
	// activeOwner, so a concurrent tab cannot inject the wrong contract.
	ComputerUseOwner string
	// HorizonRole is set only for LongHorizon inner episodes (cli_executor /
	// gui_executor / browser_executor). Empty means ordinary IM/CU.
	HorizonRole string
	// ResumeWorkingState is a one-shot carrier from AskUser / RecordAudio
	// consume. Ordinary chat and leftover pending maps must not set this.
	// bindLoopResumeWorkingState clears a reused LoopContext leftover.
	ResumeWorkingState *agent.WorkingState
	// ClientTools and ClientToolContext are immutable per-turn snapshots. They
	// keep dynamically declared device tools out of the global tool registry.
	ClientTools       []agent.ClientToolDefinition
	ClientToolContext *agent.ClientToolContext
	// DeliveryTarget is a copy of the authenticated inbound channel target.
	// Tool parameters and LLM text cannot alter it.
	DeliveryTarget *agent.DeliveryTarget

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

	// WorkflowType is the active V2 template type (coding, business_plan, ...).
	// Desktop sends it as X-MaClaw-Workflow-Type when talking to Hub/HubCenter.
	WorkflowType string

	// WorkflowPhaseKind is the normalized V2 phase role (document_planning,
	// execution, review, ...). Desktop prefers this over WorkflowPhaseID for
	// X-MaClaw-Phase-Kind.
	WorkflowPhaseKind string

	// WorkflowID is the durable V2 workflow identity. Coding runtime tasks use
	// it for a stable Workflow/Phase projection; LoopContext.ID is only a
	// per-process execution-loop identity and must never be substituted here.
	WorkflowID string

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

	// LansengerGroupPermissions is set only for local Lansenger group messages.
	// It keeps group-originated knowledge and filesystem access scoped all the
	// way through tool selection and execution.
	LansengerGroupPermissions *lansengerGroupPermissionPolicy
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

// bindLoopResumeWorkingState is the one-shot ResumeWorkingState write.
// A reused LoopContext must not keep a previous turn's workspace.
func bindLoopResumeWorkingState(loopCtx *LoopContext, resume *agent.WorkingState, askUserContext string) {
	if loopCtx == nil {
		return
	}
	if resume != nil && strings.TrimSpace(askUserContext) != "" {
		loopCtx.ResumeWorkingState = agent.CloneWorkingState(resume)
		return
	}
	loopCtx.ResumeWorkingState = nil
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
	// Compatibility/test-created LoopContexts may omit CancelC.  Treat that as
	// an uninitialised cancellation signal, not as a nil channel to close: the
	// latter panics while c.mu is held and can deadlock deferred hook cleanup.
	if c.CancelC == nil {
		c.CancelC = make(chan struct{})
	}
	select {
	case <-c.CancelC:
		// already closed
		c.mu.Unlock()
		return
	default:
		close(c.CancelC)
	}
	hooks := make([]func(), 0, len(c.cancelHooks))
	for _, hook := range c.cancelHooks {
		hooks = append(hooks, hook)
	}
	// A completed coding attempt unregisters itself. Retaining callbacks after
	// cancellation would only keep host/store references alive while this loop
	// unwinds.
	c.cancelHooks = nil
	// The terminal state is now fully published while the lock is held.  An
	// unregister closure that races with hook execution must observe this
	// closed channel and become a no-op rather than waiting for c.mu.
	c.mu.Unlock()
	// Hooks persist the durable cancellation boundary (for example a coding
	// runtime task). Run them after releasing the loop lock: SQLite can block
	// briefly, and hook code must never re-enter LoopContext while it is held.
	for _, hook := range hooks {
		if hook != nil {
			hook()
		}
	}
}

// RegisterCancelHook binds a short, host-owned cleanup action to this loop's
// explicit cancellation boundary. It is used for durable runtime cancellation
// in addition to the normal in-process CancelC signal. The returned function
// unregisters the hook once the operation reaches its own terminal state.
func (c *LoopContext) RegisterCancelHook(hook func()) func() {
	if c == nil || hook == nil {
		return func() {}
	}
	c.mu.Lock()
	if c.isCancelledLocked() {
		c.mu.Unlock()
		hook()
		return func() {}
	}
	c.nextCancelHookID++
	id := c.nextCancelHookID
	if c.cancelHooks == nil {
		c.cancelHooks = make(map[uint64]func())
	}
	c.cancelHooks[id] = hook
	c.mu.Unlock()
	return func() {
		// Cancel clears the entire hook set before invoking callbacks.  A
		// terminal cleanup may race with (or be reached re-entrantly from) a
		// cancellation callback; once the signal is closed there is nothing
		// left to unregister, and taking c.mu here can deadlock that cleanup.
		select {
		case <-c.CancelC:
			return
		default:
		}
		c.mu.Lock()
		delete(c.cancelHooks, id)
		c.mu.Unlock()
	}
}

// RegisterCancelHookForContext additionally removes the hook when ctx ends.
// It is appropriate for an individual runtime Attempt: a regular operation
// completion must not leave a stale hook that cancels a later task in the
// same user loop.
func (c *LoopContext) RegisterCancelHookForContext(ctx context.Context, hook func()) func() {
	unregister := c.RegisterCancelHook(hook)
	if ctx == nil {
		return unregister
	}
	go func() {
		<-ctx.Done()
		unregister()
	}()
	return unregister
}

// ReplaceSemanticTurn discards the private semantic identity for a fresh
// inbound request and durably fences every surface published for the prior
// request. This is deliberately separate from Cancel: a caller may reuse a
// LoopContext, so closing CancelC would permanently poison the replacement.
//
// Fences run after releasing c.mu because they may synchronously persist route
// cancellation in SQLite. They are best-effort only in the sense that a
// failure is reported by the fence owner; authority is never transferred to
// the new turn while a prior fence is pending.
func (c *LoopContext) ReplaceSemanticTurn() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.semanticTurnGeneration++
	fences := make([]func(), 0, len(c.semanticTurnFences))
	for _, fence := range c.semanticTurnFences {
		fences = append(fences, fence)
	}
	c.semanticTurnFences = nil
	replacementCancels := make([]context.CancelFunc, 0, len(c.semanticTurnReplacementCancels))
	for _, cancel := range c.semanticTurnReplacementCancels {
		replacementCancels = append(replacementCancels, cancel)
	}
	c.semanticTurnReplacementCancels = nil
	c.semanticInvocation = semanticLoopInvocationIdentity{}
	c.mu.Unlock()
	// Stop in-flight planning/catalog work before revoking any published
	// surface. CancelC is deliberately not involved: it belongs to the reusable
	// loop object and would also poison the fresh replacement turn.
	for _, cancel := range replacementCancels {
		if cancel != nil {
			cancel()
		}
	}
	for _, fence := range fences {
		if fence != nil {
			fence()
		}
	}
}

// RegisterSemanticTurnReplacementCancel associates request-local planning
// with exactly one inbound generation. A replacement stops this work even
// though the LoopContext itself remains usable. If it already lost the race,
// the cancel runs immediately and the caller must fail the stale request
// closed rather than publishing a model-visible surface.
func (c *LoopContext) RegisterSemanticTurnReplacementCancel(generation uint64, cancel context.CancelFunc) (func(), bool) {
	if c == nil || cancel == nil {
		return func() {}, c != nil
	}
	c.mu.Lock()
	if c.semanticTurnGeneration != generation {
		c.mu.Unlock()
		cancel()
		return func() {}, false
	}
	c.nextSemanticTurnReplacementCancelID++
	id := c.nextSemanticTurnReplacementCancelID
	if c.semanticTurnReplacementCancels == nil {
		c.semanticTurnReplacementCancels = make(map[uint64]context.CancelFunc)
	}
	c.semanticTurnReplacementCancels[id] = cancel
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		delete(c.semanticTurnReplacementCancels, id)
		c.mu.Unlock()
	}, true
}

// SemanticTurnContext is a request context for work that must not survive a
// fresh ingress replacement. It composes terminal loop cancellation with the
// generation-specific replacement fence; callers must use the returned cleanup
// after their request-bound classification, catalog, or planning work ends.
func (c *LoopContext) SemanticTurnContext(generation uint64) (context.Context, func(), bool) {
	if c == nil {
		ctx, cancel := context.WithCancel(context.Background())
		return ctx, cancel, generation == 0
	}
	base, cancelBase := c.Context()
	ctx, cancelReplacement := context.WithCancel(base)
	remove, current := c.RegisterSemanticTurnReplacementCancel(generation, cancelReplacement)
	cleanup := func() {
		remove()
		cancelReplacement()
		cancelBase()
	}
	if !current {
		cleanup()
	}
	return ctx, cleanup, current
}

// SemanticTurnCurrent reports whether a host-captured generation still owns
// this reusable LoopContext. Generation zero is the initial, valid generation
// of a newly created LoopContext; only a nil context has no host turn.
func (c *LoopContext) SemanticTurnCurrent(generation uint64) bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.semanticTurnGeneration == generation
}

// SemanticTurnGeneration returns the host-private replacement generation for
// the current inbound turn. It is not a transport, runtime, or model-visible
// identity and must never be used in an InvocationScope or host-call key.
func (c *LoopContext) SemanticTurnGeneration() uint64 {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.semanticTurnGeneration
}

// RegisterSemanticTurnFence associates durable authority cleanup with exactly
// one inbound generation. If the request was already replaced while a surface
// was being planned/published, the fence runs immediately and registration
// fails; the caller must fail the stale surface closed rather than returning
// definitions for it.
func (c *LoopContext) RegisterSemanticTurnFence(generation uint64, fence func()) (func(), bool) {
	if c == nil || fence == nil {
		return func() {}, c != nil
	}
	c.mu.Lock()
	if c.semanticTurnGeneration != generation {
		c.mu.Unlock()
		fence()
		return func() {}, false
	}
	c.nextSemanticTurnFenceID++
	id := c.nextSemanticTurnFenceID
	if c.semanticTurnFences == nil {
		c.semanticTurnFences = make(map[uint64]func())
	}
	c.semanticTurnFences[id] = fence
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		delete(c.semanticTurnFences, id)
		c.mu.Unlock()
	}, true
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
	if c != nil {
		select {
		case <-c.CancelC:
			cancel()
			return ctx, cancel
		default:
		}
	}
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

func (c *LoopContext) rememberDiscoveredConditionalTool(name string) bool {
	name = strings.TrimSpace(name)
	if c == nil || name == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.discoveredConditionalTools == nil {
		c.discoveredConditionalTools = make(map[string]bool)
	}
	c.discoveredConditionalTools[name] = true
	return true
}

func (c *LoopContext) discoveredConditionalToolNames() []string {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.discoveredConditionalTools) == 0 {
		return nil
	}
	names := make([]string, 0, len(c.discoveredConditionalTools))
	for name := range c.discoveredConditionalTools {
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
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
