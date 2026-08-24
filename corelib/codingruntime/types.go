// Package codingruntime provides the durable, transport-neutral execution
// model shared by GUI, MaclawSrv and TUI coding frontends. It intentionally
// contains no Wails, IM, SSH or LLM-provider dependency; those concerns belong
// in adapters owned by each host.
package codingruntime

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Ledger records must remain a compact recovery/audit boundary. Hosts keep
// transcripts, command output and provider diagnostics locally; these limits
// prevent an accidental adapter regression from turning the shared SQLite
// ledger into another transcript store.
const (
	maxRequestedWorkRunes = 8192
	maxErrorCodeRunes     = 128
	maxErrorSummaryRunes  = 1024
	maxEventTypeRunes     = 128
	maxPayloadDigestRunes = 512
	maxEvidenceTypeRunes  = 128
)

var (
	ErrNotFound           = errors.New("coding runtime record not found")
	ErrLeaseHeld          = errors.New("coding runtime task lease is held")
	ErrLeaseOwnerMismatch = errors.New("coding runtime lease owner mismatch")
	ErrInvalidTransition  = errors.New("invalid coding runtime state transition")
	ErrAttemptNotRunning  = errors.New("coding runtime attempt is not running")
	ErrRecoveryRequired   = errors.New("coding runtime recovery is required")
	ErrRecoveryNotReady   = errors.New("coding runtime recovery is not ready for continuation")
	ErrPolicyMismatch     = errors.New("coding runtime policy snapshot mismatch")
	ErrWriterConflict     = errors.New("coding runtime writer admission denied")
	// ErrContinuationRequired means a parent has durable child results awaiting
	// an explicit review handoff. Starting that queued task as an ordinary run
	// would bypass the fresh parent decision boundary and is therefore denied.
	ErrContinuationRequired = errors.New("coding runtime parent continuation review is required")
	// ErrContinuationConsumed means a host attempted to begin a second parent
	// review from the same delivered child-result handoff. It is intentionally
	// distinct from a lease conflict: callers must not replay the review merely
	// because a previous fresh parent Attempt already consumed the handoff.
	ErrContinuationConsumed = errors.New("coding runtime parent continuation already consumed")
	// ErrStaleAttempt means an executor callback arrived after its Attempt had
	// already been closed. The callback is discarded and recorded as a bounded
	// audit event; it must never alter a later Attempt for the same Task.
	ErrStaleAttempt = errors.New("coding runtime stale attempt callback discarded")
	// ErrSemanticAnchorNotFound means the host has not durably bound a runtime
	// attempt to a verified semantic invocation. Callers must fail closed rather
	// than treating a runtime task, lease owner, project path, or transport ID as
	// a semantic task identity.
	ErrSemanticAnchorNotFound = errors.New("coding runtime semantic anchor not found")
	// ErrSemanticAnchorConflict means a second registration tried to alter the
	// semantic lineage already attached to a runtime task/attempt.
	ErrSemanticAnchorConflict = errors.New("coding runtime semantic anchor conflict")
)

// TaskStatus is the durable state of a stable logical task. A task can have
// multiple attempts, but only one active write lease at a time.
type TaskStatus string

const (
	TaskQueued          TaskStatus = "queued"
	TaskRunning         TaskStatus = "running"
	TaskWaitingApproval TaskStatus = "waiting_approval"
	// TaskWaitingChild means the parent has admitted one or more read-only
	// children and deliberately released its write lease. A child completion
	// makes the parent queued again; it never resumes the old Attempt.
	TaskWaitingChild TaskStatus = "waiting_child"
	TaskInterrupted  TaskStatus = "interrupted"
	TaskCompleted    TaskStatus = "completed"
	TaskFailed       TaskStatus = "failed"
	TaskBlocked      TaskStatus = "blocked"
	TaskCancelled    TaskStatus = "cancelled"
)

func (s TaskStatus) Terminal() bool {
	switch s {
	case TaskCompleted, TaskFailed, TaskBlocked, TaskCancelled:
		return true
	default:
		return false
	}
}

// SideEffectState distinguishes known execution from an attempt which might
// have written files, executed shell, or sent an SSH command just before it
// was interrupted. Uncertain attempts must never be automatically replayed.
type SideEffectState string

const (
	SideEffectNone      SideEffectState = "none"
	SideEffectObserved  SideEffectState = "observed"
	SideEffectUncertain SideEffectState = "uncertain"
	SideEffectConfirmed SideEffectState = "confirmed"
)

type Task struct {
	TaskID        string
	WorkflowID    string
	PhaseID       string
	OwnerID       string
	ParentTaskID  string
	ProjectRef    string
	Mode          string
	RequestedWork string
	PolicyDigest  string
	Status        TaskStatus
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// PolicySnapshot is deliberately a bounded policy summary, not raw provider
// prompts, secrets, or full tool output.
type PolicySnapshot struct {
	Digest       string
	ProjectRoot  string
	RemoteTarget string
	Mode         string
	ReadOnly     bool
	// WriteSet is the frozen, normalized declaration used for cross-task
	// writer admission. Empty declarations are persisted as Unknown and are
	// therefore serialized within their workspace.
	WriteSet WriteSet
	// WorkspaceIsolated and FinalDiffGateRequired are capability assertions
	// from a host adapter. They are both required before two disjoint writers
	// may run in the same workspace. Runner also requires a successful final
	// diff gate result before such an attempt may complete.
	WorkspaceIsolated     bool
	FinalDiffGateRequired bool
	// FinalWorkspaceGateRequired is for serialized local writers whose host
	// does not use an isolated worktree. Before recording completed, Runner
	// must capture a second read-only workspace probe and confirm that it
	// differs from the baseline. This prevents a bare model success response
	// from becoming a completed coding task without observable workspace
	// evidence. An unchanged implementation can complete only when its host
	// explicitly supplies independently verified, bounded no-change evidence;
	// it is never silently treated as a model-success response.
	FinalWorkspaceGateRequired bool
}

// WorkspaceProbe is a compact, non-secret summary used to compare an
// interrupted attempt with a later read-only recovery probe.
type WorkspaceProbe struct {
	ProjectRef string
	Head       string
	StatusHash string
	FilesHash  string
	HostKey    string
	WorkDir    string
	ObservedAt time.Time
}

type Attempt struct {
	AttemptID       string
	TaskID          string
	AttemptNo       int
	LeaseOwner      string
	LeaseUntil      time.Time
	Status          TaskStatus
	Policy          PolicySnapshot
	SideEffectState SideEffectState
	WorkspaceBefore *WorkspaceProbe
	WorkspaceAfter  *WorkspaceProbe
	ErrorCode       string
	ErrorSummary    string
	StartedAt       time.Time
	FinishedAt      time.Time
}

// SemanticTaskAnchor is the durable host-side mapping from one execution-ledger
// attempt to a semantic invocation lineage. RuntimeTaskID is deliberately not
// RootTaskID: the former identifies recoverable execution bookkeeping, while
// the latter is a task relation issued by the authenticated host. This package
// stores and checks the mapping but never manufactures any of these values.
//
// An anchor is attempt-scoped because each model turn needs a fresh TurnID.
// All attempts of one runtime task must retain the same tenant/principal/
// session/root tuple; RegisterSemanticTaskAnchor enforces that invariant.
type SemanticTaskAnchor struct {
	RuntimeTaskID    string
	RuntimeAttemptID string
	TenantID         string
	PrincipalID      string
	SessionID        string
	RootTaskID       string
	TurnID           string
	CreatedAt        time.Time
}

type Event struct {
	TaskID        string
	AttemptID     string
	Sequence      uint64
	Type          string
	PayloadDigest string
	CreatedAt     time.Time
}

type FinishInput struct {
	Status          TaskStatus
	SideEffectState SideEffectState
	WorkspaceAfter  *WorkspaceProbe
	ErrorCode       string
	ErrorSummary    string
}

func boundedLedgerText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}

func normalizeTaskForLedger(task Task) Task {
	task.RequestedWork = boundedLedgerText(task.RequestedWork, maxRequestedWorkRunes)
	return task
}

func normalizeFinishInputForLedger(input FinishInput) FinishInput {
	input.ErrorCode = boundedLedgerText(input.ErrorCode, maxErrorCodeRunes)
	input.ErrorSummary = boundedLedgerText(input.ErrorSummary, maxErrorSummaryRunes)
	return input
}

func normalizeEventForLedger(eventType, payloadDigest string) (string, string) {
	return boundedLedgerText(eventType, maxEventTypeRunes), boundedLedgerText(payloadDigest, maxPayloadDigestRunes)
}

func normalizeEvidenceForLedger(evidence Evidence) Evidence {
	evidence.Type = boundedLedgerText(evidence.Type, maxEvidenceTypeRunes)
	evidence.Digest = boundedLedgerText(evidence.Digest, maxPayloadDigestRunes)
	return evidence
}

// ChildTaskSpec is the bounded, host-neutral admission request for a
// read-only child. Hosts retain the actual prompt, model and tool wiring; the
// ledger stores only the work summary needed for recovery and audit.
type ChildTaskSpec struct {
	Name          string
	RequestedWork string
	ProjectRef    string
	Mode          string
}

// ChildTaskHandle is returned as soon as a child is admitted. It is not a
// future result and must not be used as an implicit replay or prompt handle.
type ChildTaskHandle struct {
	TaskID          string
	ParentTaskID    string
	ParentAttemptID string
	Name            string
	Status          TaskStatus
	ExecutionTarget string
}

// ChildTaskResult is the only child output retained for a later parent
// attempt. Summary and EvidenceDigest are bounded by Store implementations;
// transcripts, command arguments and raw tool output stay with the host.
type ChildTaskResult struct {
	TaskID         string
	AttemptID      string
	Status         TaskStatus
	Summary        string
	EvidenceDigest string
	CompletedAt    time.Time
}

// ParentContinuation is the explicit, read-only handoff from completed child
// work to a later parent Attempt. It contains no transcript, command or tool
// arguments; a host uses it to construct a fresh prompt/decision boundary.
// It never authorizes an automatic replay of the parent executor.
type ParentContinuation struct {
	Task         Task
	ChildResults []ChildTaskResult
	// ParentAttemptID identifies the waiting_child Attempt that produced this
	// handoff. A host must present it unchanged when beginning the one allowed
	// fresh parent review Attempt; it is not a prompt, transcript, or replay
	// token.
	ParentAttemptID string
}

// ChildRecoveryState is the UI/API-safe state of a child task that was
// admitted by a parent. It deliberately excludes the child transcript and
// tool arguments; a host can use TaskID to begin an explicit recovery review.
type ChildRecoveryState struct {
	TaskID string
	Status TaskStatus
}

// ReadOnlyChildExecutor is the common host contract for explorer/reviewer
// work. GUI, TUI and MaClawSrv may provide different LLM/tool adapters while
// sharing child admission, parent lease release and bounded result delivery.
// The executor must honor request.Attempt.Policy.ReadOnly and must not expose
// a write-capable tool surface.
type ReadOnlyChildExecutor interface {
	ExecuteReadOnlyChild(context.Context, ExecutionRequest) ChildTaskResult
}

// Store is the execution ledger boundary. All mutating calls are expected to
// validate ownership and state transitions atomically.
type Store interface {
	CreateTask(Task) (*Task, error)
	GetTask(taskID string) (*Task, error)
	ListChildTasks(parentTaskID string) ([]*Task, error)
	GetAttempt(attemptID string) (*Attempt, error)
	ListAttempts(taskID string) ([]*Attempt, error)
	// ConsumeParentContinuation atomically marks one waiting_child parent
	// Attempt as consumed by a fresh review Attempt. It returns ErrContinuationConsumed
	// when the handoff was already consumed; the caller must not run an executor.
	ConsumeParentContinuation(taskID, parentAttemptID, reviewAttemptID string, now time.Time) error
	IsParentContinuationConsumed(parentAttemptID string) (bool, error)
	MarkTaskWaitingApproval(taskID string, now time.Time) (*Task, error)
	MarkTaskReadyForRecovery(taskID string, now time.Time) (*Task, error)
	StartAttempt(taskID, leaseOwner string, leaseFor time.Duration, policy PolicySnapshot, now time.Time) (*Attempt, error)
	RecordWorkspaceBefore(attemptID, leaseOwner string, probe *WorkspaceProbe, now time.Time) (*Attempt, error)
	AdmitReadOnlyChildren(parentAttemptID, leaseOwner string, specs []ChildTaskSpec, policy PolicySnapshot, now time.Time) ([]ChildTaskHandle, error)
	CompleteChildTask(childTaskID string, result ChildTaskResult, now time.Time) (*Task, error)
	ListChildResults(parentTaskID string) ([]ChildTaskResult, error)
	AppendEvent(attemptID, leaseOwner, eventType, payloadDigest string, now time.Time) (*Event, error)
	// RecordStaleCallback appends the fixed audit event for an executor result
	// that arrived after its Attempt was closed. Unlike AppendEvent it accepts
	// no lease owner and may be used only for non-running attempts.
	RecordStaleCallback(attemptID, payloadDigest string, now time.Time) (*Event, error)
	// ListEvents exposes compact Ledger facts for audit/review. It never
	// contains raw commands, transcripts, credentials, or tool output.
	ListEvents(attemptID string) ([]Event, error)
	AppendRecoveryEvent(attemptID, eventType, payloadDigest string, now time.Time) (*Event, error)
	FinishAttempt(attemptID, leaseOwner string, input FinishInput, now time.Time) (*Attempt, error)
	// CancelTask records an explicit user cancellation for a task and every
	// unfinished descendant. It closes any live or waiting-child Attempts so a
	// late executor callback cannot resurrect work after cancellation. Hosts
	// must still cancel their in-process contexts; this durable transition is
	// the cross-restart source of truth and never replays work.
	CancelTask(taskID string, now time.Time) ([]*Attempt, error)
	ExpireLeases(now time.Time) ([]*Attempt, error)
	InterruptUnstartedChildren(now time.Time) ([]*Attempt, error)
	ListRecoveryCandidates() ([]*Attempt, error)
}

// SemanticTaskAnchorStore is an optional extension implemented by durable
// coding-runtime stores. It keeps semantic identity separate from the compact
// generic Task fields so runtime IDs can never be silently reinterpreted as
// authorization scope. Hosts without this extension must leave dynamic
// capabilities unavailable.
type SemanticTaskAnchorStore interface {
	RegisterSemanticTaskAnchor(anchor SemanticTaskAnchor) (*SemanticTaskAnchor, error)
	ResolveSemanticTaskAnchor(runtimeTaskID, runtimeAttemptID string) (*SemanticTaskAnchor, error)
}

func normalizeSemanticTaskAnchor(anchor SemanticTaskAnchor) (SemanticTaskAnchor, error) {
	anchor.RuntimeTaskID = strings.TrimSpace(anchor.RuntimeTaskID)
	anchor.RuntimeAttemptID = strings.TrimSpace(anchor.RuntimeAttemptID)
	anchor.TenantID = strings.TrimSpace(anchor.TenantID)
	anchor.PrincipalID = strings.TrimSpace(anchor.PrincipalID)
	anchor.SessionID = strings.TrimSpace(anchor.SessionID)
	anchor.RootTaskID = strings.TrimSpace(anchor.RootTaskID)
	anchor.TurnID = strings.TrimSpace(anchor.TurnID)
	if anchor.RuntimeTaskID == "" || anchor.RuntimeAttemptID == "" || anchor.TenantID == "" || anchor.PrincipalID == "" || anchor.SessionID == "" || anchor.RootTaskID == "" || anchor.TurnID == "" {
		return SemanticTaskAnchor{}, ErrSemanticAnchorNotFound
	}
	if anchor.CreatedAt.IsZero() {
		anchor.CreatedAt = time.Now().UTC()
	} else {
		anchor.CreatedAt = anchor.CreatedAt.UTC()
	}
	return anchor, nil
}

func semanticAnchorLineageEqual(left, right SemanticTaskAnchor) bool {
	return left.TenantID == right.TenantID && left.PrincipalID == right.PrincipalID &&
		left.SessionID == right.SessionID && left.RootTaskID == right.RootTaskID
}

func validStartStatus(status TaskStatus) bool {
	return status == TaskQueued || status == TaskWaitingApproval
}

func validTerminalStatus(status TaskStatus) bool {
	// Interrupted closes the current attempt but does not close the logical
	// task: a later read-only recovery flow may create a new attempt.
	return status.Terminal() || status == TaskInterrupted || status == TaskWaitingChild
}
