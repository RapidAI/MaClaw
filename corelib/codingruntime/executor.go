package codingruntime

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ExecutionRequest is the host-neutral input supplied to a local, SSH, or
// container adapter. The adapter must not use a prior attempt's command or
// tool payload as an implicit replay source.
type ExecutionRequest struct {
	Task    Task
	Attempt Attempt
}

// ApprovalRequiredError lets an adapter decline execution before model/tool
// invocation. The stable task remains waiting_approval rather than being
// collapsed into a generic failed/skipped result.
type ApprovalRequiredError struct{ Summary string }

func (e ApprovalRequiredError) Error() string { return e.Summary }

type ApprovalGate interface {
	Check(Task, PolicySnapshot) error
}

// ExecutionResult is deliberately bounded. Detailed provider/tool output
// stays with the host; only summaries and evidence digests belong in Ledger.
type ExecutionResult struct {
	Status          TaskStatus
	SideEffectState SideEffectState
	WorkspaceAfter  *WorkspaceProbe
	ErrorCode       string
	ErrorSummary    string
	Evidence        []Evidence
	// FinalDiffGatePassed is supplied by an isolated writer adapter after it
	// has checked its final diff and performed (or staged for) a controlled
	// merge. It is ignored for ordinary serialized writers.
	FinalDiffGatePassed bool
	// NoWorkspaceChangeEvidenceDigest permits an explicitly verified no-op
	// implementation result to complete when FinalWorkspaceGateRequired is
	// enabled and the final probe equals the baseline. It must be a bounded
	// non-secret digest produced by a host quality gate, never a model's prose
	// claim, and must be duplicated by Evidence{Type: "verified_no_change"}
	// in the same result. That paired evidence makes the no-op acceptance an
	// auditable host assertion rather than an arbitrary adapter escape hatch.
	// Empty means no exception: unchanged writers are blocked.
	NoWorkspaceChangeEvidenceDigest string
}

// Evidence is an auditable, non-secret execution fact. Digest must be a hash
// or bounded summary, never a raw command output or credential.
type Evidence struct {
	Type   string
	Digest string
}

// Executor is implemented by each host adapter. GUI may wrap its current
// CodingSubAgent or RemoteCodingSubAgent; MaclawSrv/TUI can supply different
// tool and transport implementations without duplicating runtime semantics.
type Executor interface {
	Execute(context.Context, ExecutionRequest) ExecutionResult
}

// Runner gives every adapter the same durable lifecycle: create a task,
// atomically acquire an attempt lease, record bounded evidence, then commit a
// terminal result. Cancellation is always uncertain because the host may have
// reached a side-effecting tool just before context delivery.
type Runner struct {
	Store         Store
	LeaseOwner    string
	LeaseDuration time.Duration
	Now           func() time.Time
	ApprovalGate  ApprovalGate
	// WorkspaceProber is optional and must be read-only. When present it
	// captures the baseline for recovery before the host invokes a model/tool.
	WorkspaceProber WorkspaceProber
}

// ContinuationReview carries the opaque parent Attempt identity selected by
// PrepareParentContinuation. It lets Runner atomically consume that handoff
// after acquiring the fresh attempt and before an adapter/model can execute.
// A zero value is an ordinary task execution.
type ContinuationReview struct {
	ParentAttemptID string
}

func (r Runner) Run(ctx context.Context, task Task, policy PolicySnapshot, executor Executor) (*Task, *Attempt, error) {
	return r.RunWithContinuation(ctx, task, policy, ContinuationReview{}, executor)
}

func (r Runner) RunWithContinuation(ctx context.Context, task Task, policy PolicySnapshot, continuation ContinuationReview, executor Executor) (*Task, *Attempt, error) {
	if r.Store == nil || executor == nil {
		return nil, nil, fmt.Errorf("coding runtime runner requires store and executor")
	}
	if r.LeaseOwner == "" {
		return nil, nil, fmt.Errorf("coding runtime runner requires lease owner")
	}
	parentAttemptID := strings.TrimSpace(continuation.ParentAttemptID)
	// A continuation is a capability for one already-persisted parent task; it
	// must never turn an unknown task ID (or an empty task ID) into a newly
	// created task. Besides rejecting forged cross-task references, this avoids
	// leaving orphan queued tasks behind when a host presents stale handoff data.
	if parentAttemptID != "" && strings.TrimSpace(task.TaskID) == "" {
		return nil, nil, fmt.Errorf("%w: parent continuation requires an existing task", ErrInvalidTransition)
	}
	leaseFor := r.LeaseDuration
	if leaseFor <= 0 {
		leaseFor = 10 * time.Minute
	}
	now := r.now()
	var created *Task
	var err error
	if task.TaskID != "" {
		created, err = r.Store.GetTask(task.TaskID)
		if err != nil && err != ErrNotFound {
			return nil, nil, err
		}
		if err == ErrNotFound && parentAttemptID != "" {
			return nil, nil, ErrNotFound
		}
	}
	if created == nil {
		if task.PolicyDigest == "" {
			task.PolicyDigest = policy.Digest
		}
		created, err = r.Store.CreateTask(task)
		if err != nil {
			return nil, nil, err
		}
	}
	// A parent that admitted children is intentionally queued once every
	// result is durable. That does not make it an ordinary runnable task:
	// only the bounded ParentContinuation may begin its next Attempt. Enforce
	// this in core so GUI, TUI, and service hosts cannot bypass the review
	// boundary by calling Run with only the stable TaskID.
	attempts, listErr := r.Store.ListAttempts(created.TaskID)
	if listErr != nil {
		return created, nil, listErr
	}
	pendingParentAttemptID := ""
	for _, prior := range attempts {
		if prior == nil || prior.Status != TaskWaitingChild {
			continue
		}
		consumed, consumedErr := r.Store.IsParentContinuationConsumed(prior.AttemptID)
		if consumedErr != nil {
			return created, nil, consumedErr
		}
		if !consumed {
			if pendingParentAttemptID != "" {
				return created, nil, fmt.Errorf("%w: multiple pending parent continuations", ErrInvalidTransition)
			}
			pendingParentAttemptID = prior.AttemptID
		}
	}
	if pendingParentAttemptID != "" && parentAttemptID != pendingParentAttemptID {
		return created, nil, ErrContinuationRequired
	}
	if parentAttemptID != "" {
		// A supplied identity must name the actual waiting_child Attempt for
		// this task. This rejects cross-task handoffs before a new lease/Attempt
		// is created.
		if pendingParentAttemptID == "" {
			prior, priorErr := r.Store.GetAttempt(parentAttemptID)
			if priorErr != nil {
				return created, nil, priorErr
			}
			if prior.TaskID != created.TaskID || prior.Status != TaskWaitingChild {
				return created, nil, fmt.Errorf("%w: parent continuation does not match task", ErrInvalidTransition)
			}
			return created, nil, ErrContinuationConsumed
		}
	}
	// A recovery/new attempt cannot silently run under a looser or unrelated
	// policy. Hosts must make a new task when they need a materially different
	// policy snapshot.
	if created.PolicyDigest != "" && policy.Digest != "" && created.PolicyDigest != policy.Digest {
		return created, nil, ErrPolicyMismatch
	}
	if len(attempts) > 0 {
		// The task digest remains compatible with older hosts, but an existing
		// task can never change actual authority fields under the same digest.
		// Compare the new frozen policy with the latest persisted snapshot.
		if same, compareErr := sameFrozenPolicy(attempts[len(attempts)-1].Policy, policy); compareErr != nil {
			return created, nil, compareErr
		} else if !same {
			return created, nil, ErrPolicyMismatch
		}
	}
	if r.ApprovalGate != nil {
		if err := r.ApprovalGate.Check(*created, policy); err != nil {
			if _, markErr := r.Store.MarkTaskWaitingApproval(created.TaskID, r.now()); markErr != nil {
				return created, nil, markErr
			}
			created.Status = TaskWaitingApproval
			return created, nil, err
		}
	}
	attempt, err := r.Store.StartAttempt(created.TaskID, r.LeaseOwner, leaseFor, policy, now)
	if err != nil {
		return created, nil, err
	}
	if parentAttemptID != "" {
		if err := r.Store.ConsumeParentContinuation(created.TaskID, parentAttemptID, attempt.AttemptID, r.now()); err != nil {
			// StartAttempt acquired the lease first. Close only this new attempt;
			// never mutate a previously consumed review or resurrect its handoff.
			_, _ = r.Store.FinishAttempt(attempt.AttemptID, r.LeaseOwner, FinishInput{
				Status:          TaskBlocked,
				SideEffectState: SideEffectNone,
				ErrorCode:       "parent_continuation_unavailable",
				ErrorSummary:    "parent child-result continuation was already consumed or is invalid",
			}, r.now())
			return created, attempt, err
		}
	}
	// StartAttempt owns normalization because it must compare the exact
	// persisted policy against other live attempts. Continue from its returned
	// snapshot so adapter-visible ExecutionRequest and final-diff enforcement
	// cannot drift from the ledger fact.
	policy = attempt.Policy
	_, _ = r.Store.AppendEvent(attempt.AttemptID, r.LeaseOwner, "attempt_started", policy.Digest, now)
	if r.WorkspaceProber != nil {
		probe, probeErr := r.WorkspaceProber.ProbeWorkspace(ctx, *created, *attempt)
		if probeErr != nil || probe == nil {
			// Baseline capture is diagnostic, not authorization. A host may lack
			// git/SSH reachability at this instant; retain the failure as bounded
			// evidence while never inventing a baseline.
			digest := "workspace_before_probe_failed"
			if probeErr != nil {
				digest = codingRuntimeErrorDigest(probeErr.Error())
			}
			_, _ = r.Store.AppendEvent(attempt.AttemptID, r.LeaseOwner, "workspace_before_probe_failed", digest, r.now())
		} else if updatedAttempt, recordErr := r.Store.RecordWorkspaceBefore(attempt.AttemptID, r.LeaseOwner, probe, r.now()); recordErr == nil {
			attempt = updatedAttempt
			_, _ = r.Store.AppendEvent(attempt.AttemptID, r.LeaseOwner, "workspace_before_probed", workspaceProbeDigest(*probe), r.now())
		} else {
			_, _ = r.Store.AppendEvent(attempt.AttemptID, r.LeaseOwner, "workspace_before_probe_record_failed", codingRuntimeErrorDigest(recordErr.Error()), r.now())
		}
	}

	result := executor.Execute(ctx, ExecutionRequest{Task: *created, Attempt: *attempt})
	// An executor may admit read-only child work through ChildTaskService. That
	// transition closes the parent attempt and releases its lease while the
	// host callback is still on the stack. Do not append a competing terminal
	// event or overwrite it with the executor's ordinary return value.
	currentAttempt, currentErr := r.Store.GetAttempt(attempt.AttemptID)
	if currentErr != nil {
		return created, attempt, currentErr
	}
	if currentAttempt.Status == TaskWaitingChild {
		updated, err := r.Store.GetTask(created.TaskID)
		if err != nil {
			return created, currentAttempt, err
		}
		return updated, currentAttempt, nil
	}
	if currentAttempt.Status != TaskRunning {
		// A host executor may finish after an explicit cancellation, lease
		// sweep, or another terminal transition has already closed this
		// Attempt. Never let that late result overwrite task state (especially
		// a newer recovery Attempt); persist only a bounded audit fact.
		_, _ = r.Store.RecordStaleCallback(attempt.AttemptID, staleCallbackDigest(result), r.now())
		updated, err := r.Store.GetTask(created.TaskID)
		if err != nil {
			return created, currentAttempt, err
		}
		return updated, currentAttempt, ErrStaleAttempt
	}
	if ctx != nil && ctx.Err() != nil {
		result = ExecutionResult{
			Status:          TaskInterrupted,
			SideEffectState: SideEffectUncertain,
			ErrorCode:       "cancelled_or_interrupted",
			ErrorSummary:    "execution context ended; side effects require read-only recovery probe",
		}
	}
	if !validTerminalStatus(result.Status) {
		// An adapter cannot leave a live attempt without a lease holder. An
		// unexpected/non-terminal return is treated as an uncertain interruption.
		result.Status = TaskInterrupted
		result.SideEffectState = SideEffectUncertain
		if result.ErrorCode == "" {
			result.ErrorCode = "non_terminal_executor_result"
		}
		if result.ErrorSummary == "" {
			result.ErrorSummary = "executor returned without a terminal result"
		}
	}
	if r.WorkspaceProber != nil && result.Status == TaskCompleted {
		probe, probeErr := r.WorkspaceProber.ProbeWorkspace(ctx, *created, *attempt)
		if probeErr != nil || probe == nil {
			digest := "workspace_after_probe_failed"
			if probeErr != nil {
				digest = codingRuntimeErrorDigest(probeErr.Error())
			}
			_, _ = r.Store.AppendEvent(attempt.AttemptID, r.LeaseOwner, "workspace_after_probe_failed", digest, r.now())
			if attempt.Policy.FinalWorkspaceGateRequired && !attempt.Policy.ReadOnly {
				result.Status = TaskBlocked
				result.SideEffectState = SideEffectObserved
				result.ErrorCode = "final_workspace_probe_failed"
				result.ErrorSummary = "writer completion requires a successful read-only final workspace probe"
			}
		} else {
			result.WorkspaceAfter = probe
			_, _ = r.Store.AppendEvent(attempt.AttemptID, r.LeaseOwner, "workspace_after_probed", workspaceProbeDigest(*probe), r.now())
			if attempt.Policy.FinalWorkspaceGateRequired && !attempt.Policy.ReadOnly {
				if attempt.WorkspaceBefore == nil {
					result.Status = TaskBlocked
					result.SideEffectState = SideEffectObserved
					result.ErrorCode = "workspace_baseline_missing"
					result.ErrorSummary = "writer completion requires a read-only workspace baseline"
				} else if !workspaceProbeChanged(attempt.WorkspaceBefore, nil, probe) {
					if noChangeEvidenceAccepted(result) {
						// A host-side quality gate may establish that the requested
						// implementation was already satisfied. Record only its digest;
						// never persist the model explanation or raw tool output.
						_, _ = r.Store.AppendEvent(attempt.AttemptID, r.LeaseOwner, "final_workspace_no_change_accepted", strings.TrimSpace(result.NoWorkspaceChangeEvidenceDigest), r.now())
					} else if strings.TrimSpace(result.NoWorkspaceChangeEvidenceDigest) != "" {
						result.Status = TaskBlocked
						result.SideEffectState = SideEffectObserved
						result.ErrorCode = "verified_no_change_evidence_missing"
						result.ErrorSummary = "writer no-change completion requires a matching verified_no_change evidence digest"
					} else {
						result.Status = TaskBlocked
						result.SideEffectState = SideEffectObserved
						result.ErrorCode = "final_workspace_unchanged"
						result.ErrorSummary = "writer completion requires observable workspace change evidence or verified no-change evidence"
					}
				} else {
					_, _ = r.Store.AppendEvent(attempt.AttemptID, r.LeaseOwner, "final_workspace_gate_passed", workspaceProbeDigest(*probe), r.now())
				}
			}
		}
	}
	if attempt.Policy.FinalDiffGateRequired && !attempt.Policy.ReadOnly && result.Status == TaskCompleted && !result.FinalDiffGatePassed {
		result.Status = TaskBlocked
		result.SideEffectState = SideEffectObserved
		result.ErrorCode = "final_diff_gate_missing"
		result.ErrorSummary = "isolated writer did not report a successful final diff/merge gate"
	}
	for _, evidence := range result.Evidence {
		evidence = normalizeEvidenceForLedger(evidence)
		if evidence.Type != "" {
			_, _ = r.Store.AppendEvent(attempt.AttemptID, r.LeaseOwner, evidence.Type, evidence.Digest, r.now())
		}
	}
	terminalDigest := result.ErrorCode
	if terminalDigest == "" {
		terminalDigest = string(result.Status)
	}
	_, _ = r.Store.AppendEvent(attempt.AttemptID, r.LeaseOwner, "attempt_terminal_"+string(result.Status), terminalDigest, r.now())
	finished, err := r.Store.FinishAttempt(attempt.AttemptID, r.LeaseOwner, FinishInput{
		Status: result.Status, SideEffectState: result.SideEffectState, WorkspaceAfter: result.WorkspaceAfter,
		ErrorCode: result.ErrorCode, ErrorSummary: result.ErrorSummary,
	}, r.now())
	if err != nil {
		return created, attempt, err
	}
	updated, err := r.Store.GetTask(created.TaskID)
	if err != nil {
		return created, finished, err
	}
	return updated, finished, nil
}

// noChangeEvidenceAccepted checks the paired host-quality assertion required
// to bypass a writer's final workspace-change gate. The digest is intentionally
// opaque to corelib, but it must be a non-empty value also recorded as bounded
// evidence in this exact result. Model prose alone cannot satisfy this rule.
func noChangeEvidenceAccepted(result ExecutionResult) bool {
	digest := strings.TrimSpace(result.NoWorkspaceChangeEvidenceDigest)
	if digest == "" {
		return false
	}
	for _, evidence := range result.Evidence {
		if strings.TrimSpace(evidence.Type) == "verified_no_change" && strings.TrimSpace(evidence.Digest) == digest {
			return true
		}
	}
	return false
}

func codingRuntimeErrorDigest(value string) string {
	if value == "" {
		return ""
	}
	return workspaceProbeDigest(WorkspaceProbe{ProjectRef: value})
}

func staleCallbackDigest(result ExecutionResult) string {
	value := string(result.Status) + "|" + result.ErrorCode
	if value == "|" {
		value = "terminal_result"
	}
	return codingRuntimeErrorDigest(value)
}

func sameFrozenPolicy(left, right PolicySnapshot) (bool, error) {
	left.Digest, right.Digest = "", ""
	leftDigest, err := PolicyDigest(left)
	if err != nil {
		return false, err
	}
	rightDigest, err := PolicyDigest(right)
	if err != nil {
		return false, err
	}
	return leftDigest == rightDigest, nil
}

func (r Runner) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}
