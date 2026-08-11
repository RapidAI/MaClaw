package codingruntime

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
)

// WorkspaceProber is supplied by the host and must perform only read-only
// inspection (for example git status/diff hashes, file metadata, or an SSH
// read command). It must not invoke a model, write files, or run mutating
// commands.
type WorkspaceProber interface {
	ProbeWorkspace(context.Context, Task, Attempt) (*WorkspaceProbe, error)
}

type WorkspaceProberFunc func(context.Context, Task, Attempt) (*WorkspaceProbe, error)

func (f WorkspaceProberFunc) ProbeWorkspace(ctx context.Context, task Task, attempt Attempt) (*WorkspaceProbe, error) {
	return f(ctx, task, attempt)
}

// RecoveryPlan is a durable, UI-safe explanation of an interrupted attempt.
// It intentionally carries only probes and digests—never prior command/tool
// payloads that could be used to silently replay work.
type RecoveryPlan struct {
	Task             Task
	Interrupted      Attempt
	Before           *WorkspaceProbe
	After            *WorkspaceProbe
	Observed         *WorkspaceProbe
	WorkspaceChanged bool
	Summary          string
	Children         []ChildRecoveryState
}

// RecoveryService implements the fixed recovery protocol:
// PrepareRecovery -> ProbeWorkspace -> PresentRecoveryDiff ->
// ConfirmContinuation. Confirming permits a *new* attempt only; it never
// resumes or replays the interrupted one.
type RecoveryService struct {
	Store Store
	Now   func() time.Time
}

// PrepareRecoveryForTask resolves the latest interrupted/uncertain Attempt
// for a stable logical TaskID. UI and conversation projections retain only the
// TaskID, so they never need to persist an attempt-specific replay handle.
func (s RecoveryService) PrepareRecoveryForTask(taskID string) (*RecoveryPlan, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("coding runtime recovery requires store")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, ErrNotFound
	}
	candidates, err := s.Store.ListRecoveryCandidates()
	if err != nil {
		return nil, err
	}
	var latest *Attempt
	for _, candidate := range candidates {
		if candidate == nil || candidate.TaskID != taskID {
			continue
		}
		if latest == nil || candidate.AttemptNo > latest.AttemptNo || (candidate.AttemptNo == latest.AttemptNo && candidate.StartedAt.After(latest.StartedAt)) {
			latest = candidate
		}
	}
	if latest == nil {
		return nil, ErrRecoveryRequired
	}
	return s.PrepareRecovery(latest.AttemptID)
}

func (s RecoveryService) PrepareRecovery(attemptID string) (*RecoveryPlan, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("coding runtime recovery requires store")
	}
	attempt, err := s.Store.GetAttempt(attemptID)
	if err != nil {
		return nil, err
	}
	if attempt.Status != TaskInterrupted && attempt.SideEffectState != SideEffectUncertain {
		return nil, ErrRecoveryRequired
	}
	task, err := s.Store.GetTask(attempt.TaskID)
	if err != nil {
		return nil, err
	}
	plan := &RecoveryPlan{Task: *task, Interrupted: *attempt, Before: cloneProbe(attempt.WorkspaceBefore), After: cloneProbe(attempt.WorkspaceAfter)}
	children, err := s.Store.ListChildTasks(task.TaskID)
	if err != nil {
		return nil, err
	}
	for _, child := range children {
		if child != nil {
			plan.Children = append(plan.Children, ChildRecoveryState{TaskID: child.TaskID, Status: child.Status})
		}
	}
	if _, err := s.Store.AppendRecoveryEvent(attemptID, "recovery_prepared", "", s.now()); err != nil {
		return nil, err
	}
	return plan, nil
}

func (s RecoveryService) ProbeWorkspace(ctx context.Context, plan *RecoveryPlan, prober WorkspaceProber) (*RecoveryPlan, error) {
	if plan == nil || prober == nil {
		return nil, fmt.Errorf("coding runtime recovery requires plan and read-only prober")
	}
	if plan.Interrupted.Status != TaskInterrupted && plan.Interrupted.SideEffectState != SideEffectUncertain {
		return nil, ErrRecoveryRequired
	}
	probe, err := prober.ProbeWorkspace(ctx, plan.Task, plan.Interrupted)
	if err != nil {
		return nil, err
	}
	if probe == nil {
		return nil, fmt.Errorf("read-only recovery probe returned no workspace state")
	}
	plan.Observed = cloneProbe(probe)
	plan.WorkspaceChanged = workspaceProbeChanged(plan.Before, plan.After, plan.Observed)
	if _, err := s.Store.AppendRecoveryEvent(plan.Interrupted.AttemptID, "workspace_probed", workspaceProbeDigest(*probe), s.now()); err != nil {
		return nil, err
	}
	return plan, nil
}

// PresentRecoveryDiff returns a compact explanation suitable for GUI, TUI or
// a service API. It does not execute anything and does not infer safety.
func (s RecoveryService) PresentRecoveryDiff(plan *RecoveryPlan) (string, error) {
	if plan == nil || plan.Observed == nil {
		return "", ErrRecoveryNotReady
	}
	if len(plan.Children) > 0 {
		plan.Summary = fmt.Sprintf("%d child task(s) are attached; inspect their durable status before creating a new parent attempt", len(plan.Children))
	} else if plan.WorkspaceChanged {
		plan.Summary = "workspace differs from the interrupted attempt; review the read-only probe before creating a new attempt"
	} else {
		plan.Summary = "workspace probe completed; interrupted side effects remain unconfirmed and require explicit continuation"
	}
	if _, err := s.Store.AppendRecoveryEvent(plan.Interrupted.AttemptID, "recovery_diff_presented", recoveryPlanDigest(*plan), s.now()); err != nil {
		return "", err
	}
	return plan.Summary, nil
}

// ConfirmContinuation records an explicit human decision. A positive result
// only authorizes a caller to invoke Runner with the stable TaskID; Runner will
// allocate a new AttemptID. It never calls an Executor itself.
func (s RecoveryService) ConfirmContinuation(plan *RecoveryPlan, policy PolicySnapshot, confirmed bool) error {
	if plan == nil || plan.Observed == nil {
		return ErrRecoveryNotReady
	}
	if policy.Digest != "" && plan.Interrupted.Policy.Digest != "" && policy.Digest != plan.Interrupted.Policy.Digest {
		return ErrPolicyMismatch
	}
	eventType := "recovery_continuation_declined"
	if confirmed {
		eventType = "recovery_continuation_confirmed"
	}
	if _, err := s.Store.AppendRecoveryEvent(plan.Interrupted.AttemptID, eventType, recoveryPlanDigest(*plan), s.now()); err != nil {
		return err
	}
	if !confirmed {
		return nil
	}
	_, err := s.Store.MarkTaskReadyForRecovery(plan.Task.TaskID, s.now())
	return err
}

func (s RecoveryService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func workspaceProbeChanged(before, after, observed *WorkspaceProbe) bool {
	baseline := after
	if baseline == nil {
		baseline = before
	}
	if baseline == nil || observed == nil {
		return true
	}
	return baseline.ProjectRef != observed.ProjectRef || baseline.Head != observed.Head || baseline.StatusHash != observed.StatusHash || baseline.FilesHash != observed.FilesHash || baseline.HostKey != observed.HostKey || baseline.WorkDir != observed.WorkDir
}

func workspaceProbeDigest(probe WorkspaceProbe) string {
	sum := sha256.Sum256([]byte(probe.ProjectRef + "|" + probe.Head + "|" + probe.StatusHash + "|" + probe.FilesHash + "|" + probe.HostKey + "|" + probe.WorkDir))
	return fmt.Sprintf("sha256:%x", sum[:])
}

func recoveryPlanDigest(plan RecoveryPlan) string {
	return workspaceProbeDigest(*plan.Observed)
}
