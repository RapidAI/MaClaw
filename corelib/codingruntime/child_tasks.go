package codingruntime

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"
)

const maxChildResultSummaryRunes = 4096

// ChildTaskService owns the parent/child transition protocol. It intentionally
// does not start a goroutine or an Executor: a GUI, TUI, or service host may
// schedule the returned handle using its own transport and tool policy.
//
// Admission closes the parent Attempt as waiting_child and releases its lease
// before the child can run. Once a bounded result is delivered, a future new
// parent Attempt can decide how to proceed; no in-memory parent conversation is
// resumed automatically.
type ChildTaskService struct {
	Store Store
	Now   func() time.Time
}

// storesShareLedger verifies that two Store interfaces name the same concrete
// ledger instance. Store is intentionally an interface, so direct interface
// comparison would panic for a valid non-comparable implementation (for
// example, a value store containing a map or slice).
func storesShareLedger(left, right Store) bool {
	if left == nil || right == nil {
		return false
	}
	leftValue := reflect.ValueOf(left)
	rightValue := reflect.ValueOf(right)
	if !leftValue.IsValid() || !rightValue.IsValid() || leftValue.Type() != rightValue.Type() {
		return false
	}
	switch leftValue.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.UnsafePointer:
		if (leftValue.Kind() != reflect.UnsafePointer && leftValue.IsNil()) || (rightValue.Kind() != reflect.UnsafePointer && rightValue.IsNil()) {
			return false
		}
		return leftValue.Pointer() == rightValue.Pointer()
	default:
		// Value stores do not provide stable instance identity. Refuse them for
		// child execution instead of risking a read/write split across copies.
		return false
	}
}

// ChildRunOutcome reports the eventual durable outcome of an asynchronously
// dispatched read-only child. It is a notification only: callers must read
// the Store for recovery/audit, and must never use it to resume an old parent
// Attempt.
type ChildRunOutcome struct {
	Task    *Task
	Attempt *Attempt
	Parent  *Task
	Err     error
}

func (s ChildTaskService) AdmitReadOnlyChild(parentAttemptID, leaseOwner string, spec ChildTaskSpec, policy PolicySnapshot) (*ChildTaskHandle, error) {
	handles, err := s.AdmitReadOnlyChildren(parentAttemptID, leaseOwner, []ChildTaskSpec{spec}, policy)
	if err != nil {
		return nil, err
	}
	return &handles[0], nil
}

// AdmitReadOnlyChildren atomically admits a bounded fan-out. This is the only
// way to create sibling children: after admission the parent no longer owns a
// running lease, so a later independent admission cannot race the children.
func (s ChildTaskService) AdmitReadOnlyChildren(parentAttemptID, leaseOwner string, specs []ChildTaskSpec, policy PolicySnapshot) ([]ChildTaskHandle, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("coding runtime child task requires store")
	}
	if len(specs) == 0 {
		return nil, fmt.Errorf("%w: at least one child is required", ErrInvalidTransition)
	}
	normalized := make([]ChildTaskSpec, len(specs))
	for i, spec := range specs {
		normalized[i] = normalizeChildTaskSpec(spec)
	}
	return s.Store.AdmitReadOnlyChildren(strings.TrimSpace(parentAttemptID), strings.TrimSpace(leaseOwner), normalized, policy, s.now())
}

func (s ChildTaskService) CompleteChildTask(childTaskID string, result ChildTaskResult) (*Task, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("coding runtime child task requires store")
	}
	result.TaskID = strings.TrimSpace(childTaskID)
	result.Summary = boundedChildTaskSummary(result.Summary)
	result.EvidenceDigest = strings.TrimSpace(result.EvidenceDigest)
	if result.CompletedAt.IsZero() {
		result.CompletedAt = s.now()
	}
	return s.Store.CompleteChildTask(result.TaskID, result, s.now())
}

func (s ChildTaskService) ListChildResults(parentTaskID string) ([]ChildTaskResult, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("coding runtime child task requires store")
	}
	return s.Store.ListChildResults(strings.TrimSpace(parentTaskID))
}

// PrepareParentContinuation returns the bounded child-delivery view only after
// the parent has released its lease, every child result is durable, and the
// parent task is queued for a new Attempt. The caller must explicitly decide
// whether to start that Attempt; this API intentionally has no executor
// parameter and cannot replay the old parent loop.
func (s ChildTaskService) PrepareParentContinuation(parentTaskID string) (*ParentContinuation, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("coding runtime child task requires store")
	}
	parent, err := s.Store.GetTask(strings.TrimSpace(parentTaskID))
	if err != nil {
		return nil, err
	}
	attempts, err := s.Store.ListAttempts(parent.TaskID)
	if err != nil {
		return nil, err
	}
	var admission *Attempt
	for _, attempt := range attempts {
		if attempt != nil && attempt.Status == TaskWaitingChild {
			admission = attempt
		}
	}
	if admission == nil {
		return nil, fmt.Errorf("%w: parent was not admitted for child work", ErrInvalidTransition)
	}
	// A consumed handoff is never useful to a host, regardless of whether the
	// review Attempt has since reached a terminal state. Check it before loading
	// child summaries so callers cannot accidentally construct a second review
	// prompt from stale delivered results.
	consumed, err := s.Store.IsParentContinuationConsumed(admission.AttemptID)
	if err != nil {
		return nil, err
	}
	if consumed {
		return nil, ErrContinuationConsumed
	}
	if parent.Status != TaskQueued {
		return nil, fmt.Errorf("%w: parent is not ready for a new attempt", ErrInvalidTransition)
	}
	results, err := s.Store.ListChildResults(parent.TaskID)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("%w: parent has no delivered child results", ErrInvalidTransition)
	}
	return &ParentContinuation{Task: *parent, ChildResults: results, ParentAttemptID: admission.AttemptID}, nil
}

// RunReadOnlyChild starts the admitted child under a normal fresh Attempt,
// executes the host's strictly read-only adapter, and delivers only its
// bounded result back to the parent ledger. It is synchronous by design: a
// host can call it in its own worker/goroutine after it has persisted the
// ChildTaskHandle. The parent remains without a write lease throughout.
func (s ChildTaskService) RunReadOnlyChild(ctx context.Context, runner Runner, childTaskID string, policy PolicySnapshot, executor ReadOnlyChildExecutor) (*Task, *Attempt, *Task, error) {
	if s.Store == nil || runner.Store == nil {
		return nil, nil, nil, fmt.Errorf("coding runtime read-only child requires service and runner stores")
	}
	if !storesShareLedger(s.Store, runner.Store) {
		return nil, nil, nil, fmt.Errorf("%w: child service and runner must use the same store", ErrInvalidTransition)
	}
	if executor == nil {
		return nil, nil, nil, fmt.Errorf("coding runtime read-only child requires executor")
	}
	if !policy.ReadOnly {
		return nil, nil, nil, fmt.Errorf("%w: child policy must be read-only", ErrInvalidTransition)
	}
	child, err := runner.Store.GetTask(strings.TrimSpace(childTaskID))
	if err != nil {
		return nil, nil, nil, err
	}
	if child.ParentTaskID == "" {
		return child, nil, nil, fmt.Errorf("%w: task is not an admitted child", ErrInvalidTransition)
	}
	// A child may have completed while a caller was between scheduling and
	// execution (or a duplicate worker may race a delivered result). Never
	// create another Attempt for that terminal child: deliver the existing
	// durable result if necessary, then return it as an idempotent outcome.
	if child.Status.Terminal() {
		parent, deliverErr := s.ensureTerminalChildDelivered(child, s.now())
		return child, nil, parent, deliverErr
	}
	var outcome ChildTaskResult
	finishedTask, attempt, err := runner.Run(ctx, Task{TaskID: child.TaskID}, policy, childExecutorAdapter(func(runCtx context.Context, request ExecutionRequest) ExecutionResult {
		outcome = executor.ExecuteReadOnlyChild(runCtx, request)
		status := outcome.Status
		if !status.Terminal() {
			status = TaskFailed
		}
		digest := strings.TrimSpace(outcome.EvidenceDigest)
		if digest == "" {
			digest = codingRuntimeErrorDigest(outcome.Summary)
		}
		return ExecutionResult{Status: status, SideEffectState: SideEffectNone, ErrorSummary: boundedChildTaskSummary(outcome.Summary), Evidence: []Evidence{{Type: "child_result", Digest: digest}}}
	}))
	if err != nil {
		return finishedTask, attempt, nil, err
	}
	if attempt == nil || !attempt.Status.Terminal() {
		return finishedTask, attempt, nil, fmt.Errorf("%w: read-only child did not reach a terminal state", ErrInvalidTransition)
	}
	if outcome.EvidenceDigest == "" {
		outcome.EvidenceDigest = codingRuntimeErrorDigest(outcome.Summary)
	}
	outcome.AttemptID, outcome.Status = attempt.AttemptID, attempt.Status
	parent, err := s.CompleteChildTask(child.TaskID, outcome)
	return finishedTask, attempt, parent, err
}

// StartReadOnlyChild dispatches a child after its admission handle has been
// durably recorded. The caller must supply an execution-lifetime context, not
// the parent loop's request context: admission deliberately ends that parent
// attempt. The returned channel is buffered, so a host may ignore live
// notifications without blocking durable result delivery.
func (s ChildTaskService) StartReadOnlyChild(ctx context.Context, runner Runner, childTaskID string, policy PolicySnapshot, executor ReadOnlyChildExecutor) <-chan ChildRunOutcome {
	completed := make(chan ChildRunOutcome, 1)
	go func() {
		task, attempt, parent, err := s.RunReadOnlyChild(ctx, runner, childTaskID, policy, executor)
		completed <- ChildRunOutcome{Task: task, Attempt: attempt, Parent: parent, Err: err}
		close(completed)
	}()
	return completed
}

// ensureTerminalChildDelivered repairs the narrow crash window after a child
// Attempt reaches a terminal state but before its bounded result is delivered
// to the parent. It never invokes a child executor. A result already present
// is treated as success so duplicate dispatchers remain harmless.
func (s ChildTaskService) ensureTerminalChildDelivered(child *Task, now time.Time) (*Task, error) {
	if child == nil || child.ParentTaskID == "" || !child.Status.Terminal() {
		return nil, fmt.Errorf("%w: child task is not terminal", ErrInvalidTransition)
	}
	results, err := s.Store.ListChildResults(child.ParentTaskID)
	if err != nil {
		return nil, err
	}
	for _, result := range results {
		if result.TaskID == child.TaskID {
			return s.Store.GetTask(child.ParentTaskID)
		}
	}
	attempts, err := s.Store.ListAttempts(child.TaskID)
	if err != nil {
		return nil, err
	}
	var terminalAttempt *Attempt
	for _, attempt := range attempts {
		if attempt != nil && attempt.Status.Terminal() && (terminalAttempt == nil || attempt.AttemptNo > terminalAttempt.AttemptNo) {
			terminalAttempt = attempt
		}
	}
	if terminalAttempt == nil || terminalAttempt.Status != child.Status {
		return nil, fmt.Errorf("%w: terminal child has no matching attempt", ErrInvalidTransition)
	}
	result := ChildTaskResult{
		TaskID:         child.TaskID,
		AttemptID:      terminalAttempt.AttemptID,
		Status:         terminalAttempt.Status,
		Summary:        boundedChildTaskSummary(terminalAttempt.ErrorSummary),
		EvidenceDigest: codingRuntimeErrorDigest(terminalAttempt.AttemptID + "|" + string(terminalAttempt.Status)),
		CompletedAt:    now,
	}
	parent, err := s.CompleteChildTask(child.TaskID, result)
	if err == nil {
		return parent, nil
	}
	// Another dispatcher may have persisted the result after our snapshot.
	// Re-read rather than treating that expected race as an execution failure.
	if refreshed, listErr := s.Store.ListChildResults(child.ParentTaskID); listErr == nil {
		for _, existing := range refreshed {
			if existing.TaskID == child.TaskID {
				return s.Store.GetTask(child.ParentTaskID)
			}
		}
	}
	return nil, err
}

type childExecutorAdapter func(context.Context, ExecutionRequest) ExecutionResult

func (f childExecutorAdapter) Execute(ctx context.Context, request ExecutionRequest) ExecutionResult {
	return f(ctx, request)
}

func normalizeChildTaskSpec(spec ChildTaskSpec) ChildTaskSpec {
	spec.Name = strings.TrimSpace(spec.Name)
	spec.RequestedWork = strings.TrimSpace(spec.RequestedWork)
	spec.ProjectRef = strings.TrimSpace(spec.ProjectRef)
	spec.Mode = strings.TrimSpace(spec.Mode)
	return spec
}

func validateReadOnlyChildSpec(parent Task, spec ChildTaskSpec, policy PolicySnapshot) error {
	if spec.Name == "" || spec.RequestedWork == "" {
		return fmt.Errorf("%w: child name and requested work are required", ErrInvalidTransition)
	}
	if !policy.ReadOnly {
		return fmt.Errorf("%w: child policy must be read-only", ErrInvalidTransition)
	}
	if spec.ProjectRef != "" && spec.ProjectRef != parent.ProjectRef {
		return fmt.Errorf("%w: child project differs from parent", ErrInvalidTransition)
	}
	if spec.Mode != "" && spec.Mode != parent.Mode {
		return fmt.Errorf("%w: child execution mode differs from parent", ErrInvalidTransition)
	}
	if policy.ProjectRoot != "" && parent.ProjectRef != "" && policy.ProjectRoot != parent.ProjectRef {
		return fmt.Errorf("%w: child policy root differs from parent", ErrInvalidTransition)
	}
	return nil
}

func boundedChildTaskSummary(value string) string {
	value = strings.TrimSpace(value)
	if len([]rune(value)) <= maxChildResultSummaryRunes {
		return value
	}
	return string([]rune(value)[:maxChildResultSummaryRunes])
}

func (s ChildTaskService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
