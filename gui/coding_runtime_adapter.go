package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
)

// guiCodingRuntimeAdapter is intentionally small: the existing GUI SubAgent
// remains the tool/UI host while codingruntime owns attempt IDs, leases and
// terminal evidence semantics. MaclawSrv and TUI can replace this adapter
// without importing GUI internals.
type guiCodingRuntimeAdapter struct {
	run            func() *CodingSubAgentResult
	onStart        func(codingruntime.ExecutionRequest)
	finalizeWriter func(*CodingSubAgentResult) (bool, error)
	result         *CodingSubAgentResult
}

// guiCodingRuntimeOptions is an opt-in bridge for an already-isolated GUI
// execution. The finalizer runs before the ledger attempt reaches a terminal
// state, so a successful status cannot be recorded until the controlled
// worktree merge/diff gate has succeeded.
type guiCodingRuntimeOptions struct {
	// ExistingTaskID makes this a new Attempt of an existing Runtime task. It
	// is used only by an explicit child-result review handoff; callers must
	// obtain the bounded continuation before setting it.
	ExistingTaskID              string
	ParentContinuationAttemptID string
	DeclaredWrites              []string
	// PolicyProjectRoot is the stable logical workspace used for write-set
	// locking. An isolated worktree executes under a temporary checkout but
	// still locks the primary repository that its controlled merge will change.
	PolicyProjectRoot    string
	WorkspaceIsolated    bool
	RequireFinalDiffGate bool
	FinalizeWriter       func(*CodingSubAgentResult) (bool, error)
}

type codingRuntimeApprovalGate func() string

func (g codingRuntimeApprovalGate) Check(task codingruntime.Task, policy codingruntime.PolicySnapshot) error {
	if g == nil {
		return nil
	}
	if rejection := strings.TrimSpace(g()); rejection != "" {
		return codingruntime.ApprovalRequiredError{Summary: rejection}
	}
	return nil
}

type guiRemoteCodingRuntimeAdapter struct {
	run     func() *RemoteCodingSubAgentResult
	onStart func(codingruntime.ExecutionRequest)
	result  *RemoteCodingSubAgentResult
}

func (a *guiRemoteCodingRuntimeAdapter) Execute(ctx context.Context, request codingruntime.ExecutionRequest) codingruntime.ExecutionResult {
	if ctx != nil && ctx.Err() != nil {
		return codingruntime.ExecutionResult{Status: codingruntime.TaskInterrupted, SideEffectState: codingruntime.SideEffectUncertain, ErrorCode: "cancelled_before_remote_executor", ErrorSummary: "remote execution cancelled before executor started"}
	}
	if a.run == nil {
		return codingruntime.ExecutionResult{Status: codingruntime.TaskFailed, SideEffectState: codingruntime.SideEffectNone, ErrorCode: "nil_remote_executor", ErrorSummary: "remote coding executor is unavailable"}
	}
	if a.onStart != nil {
		a.onStart(request)
	}
	result := a.run()
	a.result = result
	if ctx != nil && ctx.Err() != nil {
		return codingruntime.ExecutionResult{Status: codingruntime.TaskInterrupted, SideEffectState: codingruntime.SideEffectUncertain, ErrorCode: "cancelled_during_remote_executor", ErrorSummary: "remote execution cancelled; remote workspace requires read-only recovery probe"}
	}
	if result == nil {
		return codingruntime.ExecutionResult{Status: codingruntime.TaskFailed, SideEffectState: codingruntime.SideEffectUncertain, ErrorCode: "nil_remote_result", ErrorSummary: "remote coding SubAgent returned no result"}
	}
	status, effects := codingruntime.TaskFailed, codingruntime.SideEffectUncertain
	switch strings.ToLower(strings.TrimSpace(result.Status)) {
	case "success", "passed", "completed", "done":
		status, effects = codingruntime.TaskCompleted, codingruntime.SideEffectConfirmed
	case "cancelled", "canceled":
		status = codingruntime.TaskCancelled
	}
	evidence := []codingruntime.Evidence{{Type: "remote_result_summary", Digest: codingRuntimeDigest(result.Summary)}}
	if len(result.FilesModified) > 0 || len(result.FilesCreated) > 0 {
		evidence = append(evidence, codingruntime.Evidence{Type: "remote_file_activity", Digest: codingRuntimeDigest(strings.Join(append(append([]string{}, result.FilesModified...), result.FilesCreated...), "\n"))})
	}
	noChangeDigest := ""
	if status == codingruntime.TaskCompleted && len(result.FilesModified) == 0 && len(result.FilesCreated) == 0 && verifiedGUIRemoteNoChangeResult(result) {
		// Remote verification has already gathered concrete inspection and
		// acceptance-command facts. Persist only the derived digest, paired with
		// an explicit evidence type, so corelib can accept an unchanged remote
		// workspace without trusting the model's summary prose.
		noChangeDigest = codingRuntimeDigest(result.ExplorationSummary + "\n" + result.VerificationSummary + "\n" + result.QualitySummary)
		evidence = append(evidence, codingruntime.Evidence{Type: "verified_no_change", Digest: noChangeDigest})
	}
	return codingruntime.ExecutionResult{Status: status, SideEffectState: effects, ErrorCode: remoteCodingRuntimeErrorCode(result), ErrorSummary: result.Error, Evidence: evidence, NoWorkspaceChangeEvidenceDigest: noChangeDigest}
}

// verifiedGUIRemoteNoChangeResult accepts only the remote subagent's
// quality-gated outcome. applyRemoteVerificationOutcome sets VerifiedNoChange
// after read/command/diff audit establishes a verified existing result.
func verifiedGUIRemoteNoChangeResult(result *RemoteCodingSubAgentResult) bool {
	if result == nil {
		return false
	}
	return result.VerifiedNoChange
}

func (a *guiCodingRuntimeAdapter) Execute(ctx context.Context, request codingruntime.ExecutionRequest) codingruntime.ExecutionResult {
	if ctx != nil && ctx.Err() != nil {
		return codingruntime.ExecutionResult{Status: codingruntime.TaskInterrupted, SideEffectState: codingruntime.SideEffectUncertain, ErrorCode: "cancelled_before_executor", ErrorSummary: "execution cancelled before GUI executor started"}
	}
	if a.run == nil {
		return codingruntime.ExecutionResult{Status: codingruntime.TaskFailed, SideEffectState: codingruntime.SideEffectNone, ErrorCode: "nil_gui_executor", ErrorSummary: "GUI coding executor is unavailable"}
	}
	if a.onStart != nil {
		a.onStart(request)
	}
	result := a.run()
	a.result = result
	if ctx != nil && ctx.Err() != nil {
		return codingruntime.ExecutionResult{Status: codingruntime.TaskInterrupted, SideEffectState: codingruntime.SideEffectUncertain, ErrorCode: "cancelled_during_executor", ErrorSummary: "execution cancelled; workspace requires read-only recovery probe"}
	}
	if result == nil {
		return codingruntime.ExecutionResult{Status: codingruntime.TaskFailed, SideEffectState: codingruntime.SideEffectUncertain, ErrorCode: "nil_subagent_result", ErrorSummary: "coding SubAgent returned no result"}
	}
	status := codingruntime.TaskFailed
	sideEffects := codingruntime.SideEffectNone
	if len(result.FilesModified) > 0 || len(result.FilesCreated) > 0 || len(result.CommandsRun) > 0 || len(result.DynamicToolsRun) > 0 {
		sideEffects = codingruntime.SideEffectObserved
	}
	switch result.Status {
	case TaskExecPassed:
		status, sideEffects = codingruntime.TaskCompleted, codingruntime.SideEffectConfirmed
	case TaskExecSkipped:
		status = codingruntime.TaskBlocked
	}
	finalDiffGatePassed := false
	if status == codingruntime.TaskCompleted && a.finalizeWriter != nil {
		passed, finalizeErr := a.finalizeWriter(result)
		if finalizeErr != nil {
			result.Status = TaskExecSkipped
			result.Error = compactSubAgentErrorSummary(finalizeErr.Error())
			result.QualityStatus = codingSubAgentQualityFailed
			result.QualitySummary = result.Error
			return codingruntime.ExecutionResult{
				Status: codingruntime.TaskBlocked, SideEffectState: codingruntime.SideEffectObserved,
				ErrorCode: "final_diff_gate_failed", ErrorSummary: result.Error,
				Evidence: []codingruntime.Evidence{{Type: "final_diff_gate_failed", Digest: codingRuntimeDigest(finalizeErr.Error())}},
			}
		}
		finalDiffGatePassed = passed
	}
	if status == codingruntime.TaskFailed && sideEffects != codingruntime.SideEffectNone {
		sideEffects = codingruntime.SideEffectUncertain
	}
	evidence := []codingruntime.Evidence{{Type: "result_summary", Digest: codingRuntimeDigest(result.Summary)}}
	if len(result.FilesModified) > 0 || len(result.FilesCreated) > 0 {
		evidence = append(evidence, codingruntime.Evidence{Type: "file_activity", Digest: codingRuntimeDigest(strings.Join(append(append([]string{}, result.FilesModified...), result.FilesCreated...), "\n"))})
	}
	if result.VerificationSummary != "" {
		evidence = append(evidence, codingruntime.Evidence{Type: "verification", Digest: codingRuntimeDigest(result.VerificationSummary)})
	}
	noChangeDigest := ""
	if status == codingruntime.TaskCompleted && len(result.FilesModified) == 0 && len(result.FilesCreated) == 0 && verifiedGUINoChangeResult(result) {
		// The GUI quality gate has already rejected no-op turns without
		// inspection/verification evidence. Preserve a digest of that bounded
		// audit, rather than the model's free-form completion text, so corelib
		// can distinguish verified "already satisfied" work from false success.
		noChangeDigest = codingRuntimeDigest(result.ExplorationSummary + "\n" + result.VerificationSummary + "\n" + result.QualitySummary)
		evidence = append(evidence, codingruntime.Evidence{Type: "verified_no_change", Digest: noChangeDigest})
	}
	return codingruntime.ExecutionResult{Status: status, SideEffectState: sideEffects, ErrorCode: codingRuntimeErrorCode(result), ErrorSummary: compactSubAgentErrorSummary(result.Error), Evidence: evidence, FinalDiffGatePassed: finalDiffGatePassed, NoWorkspaceChangeEvidenceDigest: noChangeDigest}
}

// verifiedGUINoChangeResult recognizes only the GUI SubAgent's quality-gated
// no-op outcome. This deliberately does not inspect Summary: prose such as
// "already implemented" is not evidence. At least one concrete inspection or
// verification fact and a passing aggregate quality result are required.
func verifiedGUINoChangeResult(result *CodingSubAgentResult) bool {
	if result == nil || result.QualityStatus != codingSubAgentQualityPassed {
		return false
	}
	return strings.TrimSpace(result.ExplorationSummary) != "" || strings.TrimSpace(result.VerificationSummary) != ""
}

func codingRuntimeDigest(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return fmt.Sprintf("sha256:%x", sum[:])
}

func codingRuntimeErrorCode(result *CodingSubAgentResult) string {
	if result == nil || strings.TrimSpace(result.Error) == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(result.Error)), "scope_approval_required:") {
		return "scope_approval_required"
	}
	return "executor_failed"
}

func runGUICodingTaskWithLedger(ctx context.Context, store codingruntime.Store, ownerID, workflowID, phaseID, projectPath, requestedWork string, approvalGate codingruntime.ApprovalGate, run func() *CodingSubAgentResult) (*CodingSubAgentResult, *codingruntime.Attempt, error) {
	return runGUICodingTaskWithLedgerWithStart(ctx, store, ownerID, workflowID, phaseID, projectPath, requestedWork, approvalGate, nil, run)
}

func runGUICodingTaskWithLedgerWithStart(ctx context.Context, store codingruntime.Store, ownerID, workflowID, phaseID, projectPath, requestedWork string, approvalGate codingruntime.ApprovalGate, onStart func(codingruntime.ExecutionRequest), run func() *CodingSubAgentResult) (*CodingSubAgentResult, *codingruntime.Attempt, error) {
	return runGUICodingTaskWithLedgerWithOptions(ctx, store, ownerID, workflowID, phaseID, projectPath, requestedWork, approvalGate, onStart, nil, run)
}

func runGUICodingTaskWithLedgerWithOptions(ctx context.Context, store codingruntime.Store, ownerID, workflowID, phaseID, projectPath, requestedWork string, approvalGate codingruntime.ApprovalGate, onStart func(codingruntime.ExecutionRequest), options *guiCodingRuntimeOptions, run func() *CodingSubAgentResult) (*CodingSubAgentResult, *codingruntime.Attempt, error) {
	if store == nil {
		return nil, nil, fmt.Errorf("coding runtime store is unavailable")
	}
	if strings.TrimSpace(ownerID) == "" {
		ownerID = "gui:local"
	}
	runner := codingruntime.Runner{
		Store: store, LeaseOwner: ownerID, LeaseDuration: 15 * time.Minute,
		ApprovalGate: approvalGate, WorkspaceProber: newGUILocalWorkspaceProber(projectPath),
	}
	adapter := &guiCodingRuntimeAdapter{run: run, onStart: onStart}
	// GUI's existing quality and isolated-worktree gates support non-Git local
	// projects as well. The stricter final-workspace Git gate is therefore an
	// explicit host policy (currently used by TUI/MaClawSrv), not an implicit
	// behavior change for every established GUI coding session.
	policy := codingruntime.PolicySnapshot{ProjectRoot: projectPath, Mode: "local"}
	if options != nil {
		if root := strings.TrimSpace(options.PolicyProjectRoot); root != "" {
			policy.ProjectRoot = root
		}
		policy.WorkspaceIsolated = options.WorkspaceIsolated
		policy.FinalDiffGateRequired = options.RequireFinalDiffGate
		policy.WriteSet.Claims = make([]codingruntime.WriteClaim, 0, len(options.DeclaredWrites))
		for _, path := range options.DeclaredWrites {
			policy.WriteSet.Claims = append(policy.WriteSet.Claims, codingruntime.WriteClaim{Path: path, Directory: strings.HasSuffix(strings.TrimSpace(path), "/") || strings.HasSuffix(strings.TrimSpace(path), `\`)})
		}
		adapter.finalizeWriter = options.FinalizeWriter
	}
	policyDigest, digestErr := codingruntime.PolicyDigest(policy)
	if digestErr != nil {
		return nil, nil, fmt.Errorf("freeze GUI local coding policy: %w", digestErr)
	}
	policy.Digest = policyDigest
	runtimeTask := codingruntime.Task{WorkflowID: workflowID, PhaseID: phaseID, OwnerID: ownerID, ProjectRef: projectPath, Mode: "local", RequestedWork: requestedWork, PolicyDigest: policyDigest}
	if options != nil {
		runtimeTask.TaskID = strings.TrimSpace(options.ExistingTaskID)
	}
	continuation := codingruntime.ContinuationReview{}
	if options != nil {
		continuation.ParentAttemptID = strings.TrimSpace(options.ParentContinuationAttemptID)
	}
	task, attempt, err := runner.RunWithContinuation(ctx, runtimeTask, policy, continuation, adapter)
	if _, waiting := err.(codingruntime.ApprovalRequiredError); waiting {
		return scopeApprovalRequiredCodingSubAgentResult(err.Error()), nil, nil
	}
	if options != nil && strings.TrimSpace(options.ExistingTaskID) != "" && errors.Is(err, codingruntime.ErrLeaseHeld) && task != nil {
		// A second explicit review click raced an already-started fresh parent
		// attempt. Keep the handoff eligible rather than collapsing it into a
		// failed checkpoint that could overwrite the in-flight review's result.
		return &CodingSubAgentResult{
			Status:         TaskExecSkipped,
			Summary:        "Child-result review is already in progress; no second parent attempt was started.",
			Error:          "runtime_child_review_in_progress",
			RuntimeTaskID:  task.TaskID,
			RuntimeHandoff: true,
		}, attempt, nil
	}
	if options != nil && strings.TrimSpace(options.ExistingTaskID) != "" && errors.Is(err, codingruntime.ErrContinuationConsumed) && task != nil {
		return &CodingSubAgentResult{Status: TaskExecSkipped, Summary: "Child-result handoff was already consumed; no parent executor was started.", Error: "runtime_child_review_consumed", RuntimeTaskID: task.TaskID}, attempt, nil
	}
	if options != nil && strings.TrimSpace(options.ExistingTaskID) != "" && errors.Is(err, codingruntime.ErrContinuationRequired) && task != nil {
		return &CodingSubAgentResult{Status: TaskExecSkipped, Summary: "Child results require an explicit review handoff; no parent executor was started.", Error: "runtime_child_review_required", RuntimeTaskID: task.TaskID, RuntimeHandoff: true}, attempt, nil
	}
	if errors.Is(err, codingruntime.ErrStaleAttempt) && task != nil && task.Status == codingruntime.TaskCancelled {
		return &CodingSubAgentResult{Status: TaskExecInterrupted, Summary: "Execution cancelled by user; no further tool work will be accepted.", Error: "task_cancelled", RuntimeTaskID: task.TaskID}, attempt, nil
	}
	if err != nil {
		return nil, attempt, err
	}
	if task.Status == codingruntime.TaskInterrupted {
		return &CodingSubAgentResult{Status: TaskExecInterrupted, Summary: "Execution interrupted; read-only recovery probe required", Error: attempt.ErrorSummary, RuntimeTaskID: task.TaskID}, attempt, nil
	}
	if attempt != nil && attempt.Status == codingruntime.TaskWaitingChild {
		return &CodingSubAgentResult{Status: TaskExecWaitingChild, Summary: "Read-only child tasks completed; start a fresh parent attempt to review their bounded results.", RuntimeTaskID: task.TaskID}, attempt, nil
	}
	if task.Status == codingruntime.TaskBlocked && attempt != nil && strings.HasPrefix(attempt.ErrorCode, "final_diff_gate") {
		if adapter.result != nil {
			adapter.result.RuntimeTaskID = task.TaskID
			return adapter.result, attempt, nil
		}
		return &CodingSubAgentResult{Status: TaskExecSkipped, Summary: "Final diff/merge gate blocked the isolated writer", Error: attempt.ErrorSummary, RuntimeTaskID: task.TaskID}, attempt, nil
	}
	if adapter.result != nil {
		adapter.result.RuntimeTaskID = task.TaskID
		return adapter.result, attempt, nil
	}
	status := TaskExecFailed
	switch task.Status {
	case codingruntime.TaskCompleted:
		status = TaskExecPassed
	case codingruntime.TaskBlocked:
		status = TaskExecSkipped
	}
	return &CodingSubAgentResult{Status: status, Summary: "Execution ledger attempt: " + attempt.AttemptID, Error: attempt.ErrorSummary, RuntimeTaskID: task.TaskID}, attempt, nil
}

// newGUILocalWorkspaceProber records a compact baseline using only git's
// read-only inspection commands. A non-git directory or unavailable git is a
// recoverable probe failure, not a reason to execute a mutating fallback.
func newGUILocalWorkspaceProber(projectPath string) codingruntime.WorkspaceProber {
	return codingruntime.NewLocalGitWorkspaceProber(projectPath)
}

func openGUICodingRuntimeStore(handler *IMMessageHandler) (codingruntime.Store, func(), error) {
	if handler == nil || handler.app == nil {
		return codingruntime.NewMemoryStore(), func() {}, nil
	}
	store := handler.app.ensureCodingRuntimeStore()
	if store == nil {
		return nil, nil, fmt.Errorf("unable to initialize coding runtime store")
	}
	return store, func() {}, nil
}

func runGUIRemoteCodingTaskWithLedger(ctx context.Context, store codingruntime.Store, ownerID, workflowID, phaseID, remoteTarget, projectPath, requestedWork string, workspaceProber codingruntime.WorkspaceProber, run func() *RemoteCodingSubAgentResult) (*RemoteCodingSubAgentResult, *codingruntime.Attempt, error) {
	return runGUIRemoteCodingTaskWithStartAndContinuation(ctx, store, ownerID, workflowID, phaseID, remoteTarget, projectPath, requestedWork, workspaceProber, "", "", nil, run)
}

func runGUIRemoteCodingTaskWithLedgerWithStart(ctx context.Context, store codingruntime.Store, ownerID, workflowID, phaseID, remoteTarget, projectPath, requestedWork string, workspaceProber codingruntime.WorkspaceProber, onStart func(codingruntime.ExecutionRequest), run func() *RemoteCodingSubAgentResult) (*RemoteCodingSubAgentResult, *codingruntime.Attempt, error) {
	return runGUIRemoteCodingTaskWithStartAndContinuation(ctx, store, ownerID, workflowID, phaseID, remoteTarget, projectPath, requestedWork, workspaceProber, "", "", onStart, run)
}

// runGUIRemoteCodingTaskWithLedgerWithStartAndTaskID starts a new attempt of
// an existing task only when the caller has already performed the Runtime's
// explicit recovery/continuation admission.
func runGUIRemoteCodingTaskWithStartAndContinuation(ctx context.Context, store codingruntime.Store, ownerID, workflowID, phaseID, remoteTarget, projectPath, requestedWork string, workspaceProber codingruntime.WorkspaceProber, existingTaskID, parentContinuationAttemptID string, onStart func(codingruntime.ExecutionRequest), run func() *RemoteCodingSubAgentResult) (*RemoteCodingSubAgentResult, *codingruntime.Attempt, error) {
	if store == nil {
		return nil, nil, fmt.Errorf("coding runtime store is unavailable")
	}
	if strings.TrimSpace(ownerID) == "" {
		ownerID = "gui:remote"
	}
	adapter := &guiRemoteCodingRuntimeAdapter{run: run, onStart: onStart}
	remoteTarget = strings.TrimSpace(remoteTarget)
	runner := codingruntime.Runner{Store: store, LeaseOwner: ownerID, LeaseDuration: 15 * time.Minute, WorkspaceProber: workspaceProber}
	policy := codingruntime.PolicySnapshot{ProjectRoot: projectPath, RemoteTarget: remoteTarget, Mode: "remote", FinalWorkspaceGateRequired: workspaceProber != nil}
	policyDigest, digestErr := codingruntime.PolicyDigest(policy)
	if digestErr != nil {
		return nil, nil, fmt.Errorf("freeze GUI remote coding policy: %w", digestErr)
	}
	policy.Digest = policyDigest
	task, attempt, err := runner.RunWithContinuation(ctx, codingruntime.Task{TaskID: strings.TrimSpace(existingTaskID), WorkflowID: workflowID, PhaseID: phaseID, OwnerID: ownerID, ProjectRef: projectPath, Mode: "remote", RequestedWork: requestedWork, PolicyDigest: policyDigest}, policy, codingruntime.ContinuationReview{ParentAttemptID: strings.TrimSpace(parentContinuationAttemptID)}, adapter)
	if strings.TrimSpace(existingTaskID) != "" && errors.Is(err, codingruntime.ErrLeaseHeld) && task != nil {
		return &RemoteCodingSubAgentResult{
			Status:         "skipped",
			Summary:        "Child-result review is already in progress; no second remote parent attempt was started.",
			Error:          "runtime_child_review_in_progress",
			RuntimeTaskID:  task.TaskID,
			RuntimeHandoff: true,
		}, attempt, nil
	}
	if strings.TrimSpace(existingTaskID) != "" && errors.Is(err, codingruntime.ErrContinuationConsumed) && task != nil {
		return &RemoteCodingSubAgentResult{Status: "skipped", Summary: "Child-result handoff was already consumed; no remote parent executor was started.", Error: "runtime_child_review_consumed", RuntimeTaskID: task.TaskID}, attempt, nil
	}
	if strings.TrimSpace(existingTaskID) != "" && errors.Is(err, codingruntime.ErrContinuationRequired) && task != nil {
		return &RemoteCodingSubAgentResult{Status: "skipped", Summary: "Child results require an explicit review handoff; no remote parent executor was started.", Error: "runtime_child_review_required", RuntimeTaskID: task.TaskID, RuntimeHandoff: true}, attempt, nil
	}
	if errors.Is(err, codingruntime.ErrStaleAttempt) && task != nil && task.Status == codingruntime.TaskCancelled {
		return &RemoteCodingSubAgentResult{Status: "cancelled", Summary: "Execution cancelled by user; no further remote tool work will be accepted.", Error: "task_cancelled", RuntimeTaskID: task.TaskID}, attempt, nil
	}
	if err != nil {
		return nil, attempt, err
	}
	if task.Status == codingruntime.TaskInterrupted {
		return &RemoteCodingSubAgentResult{Status: "interrupted", Summary: "Execution interrupted; read-only recovery probe required", Error: attempt.ErrorSummary, RuntimeTaskID: task.TaskID}, attempt, nil
	}
	if attempt != nil && attempt.Status == codingruntime.TaskWaitingChild {
		return &RemoteCodingSubAgentResult{Status: "waiting_child", Summary: "Read-only child tasks were admitted; start a fresh parent attempt after their bounded results are delivered.", RuntimeTaskID: task.TaskID}, attempt, nil
	}
	if adapter.result != nil {
		adapter.result.RuntimeTaskID = task.TaskID
		return adapter.result, attempt, nil
	}
	status := "failed"
	if task.Status == codingruntime.TaskCompleted {
		status = "success"
	}
	return &RemoteCodingSubAgentResult{Status: status, Summary: "Execution ledger attempt: " + attempt.AttemptID, Error: attempt.ErrorSummary, RuntimeTaskID: task.TaskID}, attempt, nil
}

func remoteCodingRuntimeErrorCode(result *RemoteCodingSubAgentResult) string {
	if result == nil || strings.TrimSpace(result.Error) == "" {
		return ""
	}
	if strings.Contains(strings.ToLower(result.Error), "cancel") {
		return "cancelled_or_interrupted"
	}
	return "remote_executor_failed"
}
