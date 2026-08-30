package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// CodingRuntimeRecoveryReview is a UI/API-safe view of a recovery candidate.
// It contains only opaque IDs, policy digest and read-only workspace facts;
// it deliberately never exposes the old model prompt, commands or tool args.
type CodingRuntimeRecoveryReview struct {
	TaskID       string                             `json:"task_id"`
	AttemptID    string                             `json:"attempt_id"`
	Mode         string                             `json:"mode"`
	ProjectRef   string                             `json:"project_ref"`
	PolicyDigest string                             `json:"policy_digest"`
	Before       *codingruntime.WorkspaceProbe      `json:"before,omitempty"`
	After        *codingruntime.WorkspaceProbe      `json:"after,omitempty"`
	Observed     *codingruntime.WorkspaceProbe      `json:"observed,omitempty"`
	Children     []codingruntime.ChildRecoveryState `json:"children,omitempty"`
	Changed      bool                               `json:"changed"`
	Summary      string                             `json:"summary"`
	ReviewDigest string                             `json:"review_digest"`
}

func (h *IMMessageHandler) prepareCodingRuntimeRecoveryForSlot(taskID string) (*CodingRuntimeRecoveryReview, error) {
	if h == nil || h.app == nil {
		return nil, fmt.Errorf("coding runtime application is unavailable")
	}
	store := h.app.ensureCodingRuntimeStore()
	if store == nil {
		return nil, fmt.Errorf("coding runtime store is unavailable")
	}
	return prepareGUICodingRuntimeRecoveryWithResolver(context.Background(), store, taskID, h.guiRecoveryWorkspaceProber)
}

// PrepareCodingRuntimeRecovery runs the mandatory read-only probe for a local
// GUI candidate. It does not create an Attempt and does not call a model/tool.
func (a *App) PrepareCodingRuntimeRecovery(ctx context.Context, taskID string) (*CodingRuntimeRecoveryReview, error) {
	if a == nil {
		return nil, fmt.Errorf("coding runtime application is unavailable")
	}
	store := a.ensureCodingRuntimeStore()
	if store == nil {
		return nil, fmt.Errorf("coding runtime store is unavailable")
	}
	return prepareGUICodingRuntimeRecovery(ctx, store, taskID)
}

// ConfirmCodingRuntimeRecovery records an explicit accept/decline decision
// after independently repeating the read-only probe. A successful confirmation
// only returns the logical task to queued; it never resumes an old Attempt or
// invokes an Executor. The caller must subsequently create a new task run.
func (a *App) ConfirmCodingRuntimeRecovery(ctx context.Context, taskID, reviewDigest string, confirmed bool) (*CodingRuntimeRecoveryReview, error) {
	if a == nil {
		return nil, fmt.Errorf("coding runtime application is unavailable")
	}
	store := a.ensureCodingRuntimeStore()
	if store == nil {
		return nil, fmt.Errorf("coding runtime store is unavailable")
	}
	return confirmGUICodingRuntimeRecovery(ctx, store, taskID, reviewDigest, confirmed)
}

func prepareGUICodingRuntimeRecovery(ctx context.Context, store codingruntime.Store, taskID string) (*CodingRuntimeRecoveryReview, error) {
	return prepareGUICodingRuntimeRecoveryWithResolver(ctx, store, taskID, guiRecoveryWorkspaceProber)
}

func prepareGUICodingRuntimeRecoveryWithResolver(ctx context.Context, store codingruntime.Store, taskID string, resolve func(codingruntime.Task) (codingruntime.WorkspaceProber, error)) (*CodingRuntimeRecoveryReview, error) {
	service := codingruntime.RecoveryService{Store: store}
	plan, err := service.PrepareRecoveryForTask(strings.TrimSpace(taskID))
	if err != nil {
		return nil, err
	}
	if resolve == nil {
		return nil, fmt.Errorf("coding recovery workspace-prober resolver is unavailable")
	}
	prober, err := resolve(plan.Task)
	if err != nil {
		return nil, err
	}
	plan, err = service.ProbeWorkspace(ctx, plan, prober)
	if err != nil {
		return nil, err
	}
	summary, err := service.PresentRecoveryDiff(plan)
	if err != nil {
		return nil, err
	}
	return codingRuntimeRecoveryReview(plan, summary), nil
}

func (h *IMMessageHandler) guiRecoveryWorkspaceProber(task codingruntime.Task) (codingruntime.WorkspaceProber, error) {
	if strings.ToLower(strings.TrimSpace(task.Mode)) != "remote" {
		return guiRecoveryWorkspaceProber(task)
	}
	if h == nil {
		return nil, fmt.Errorf("remote coding recovery handler is unavailable")
	}
	expectedTarget := ""
	latestAttemptNo := -1
	candidates, err := h.app.ensureCodingRuntimeStore().ListRecoveryCandidates()
	if err == nil {
		for _, candidate := range candidates {
			if candidate != nil && candidate.TaskID == task.TaskID && candidate.AttemptNo > latestAttemptNo {
				expectedTarget = candidate.Policy.RemoteTarget
				latestAttemptNo = candidate.AttemptNo
			}
		}
	}
	if expectedTarget == "" {
		return nil, fmt.Errorf("remote coding recovery target identity is unavailable")
	}
	mgr := h.ensureSSHManager()
	if mgr == nil {
		return nil, fmt.Errorf("remote coding recovery session manager is unavailable")
	}
	for _, session := range mgr.List() {
		if session == nil || guiRemoteCodingTargetIdentity(h, session.ID, task.ProjectRef) != expectedTarget {
			continue
		}
		if !h.sshSessionAlive(session.ID) {
			continue
		}
		return newGUIRemoteWorkspaceProber(h, session.ID, task.ProjectRef, expectedTarget), nil
	}
	return nil, fmt.Errorf("no live SSH session matches the interrupted task's verified remote target")
}

func confirmGUICodingRuntimeRecovery(ctx context.Context, store codingruntime.Store, taskID, reviewDigest string, confirmed bool) (*CodingRuntimeRecoveryReview, error) {
	service := codingruntime.RecoveryService{Store: store}
	plan, err := service.PrepareRecoveryForTask(strings.TrimSpace(taskID))
	if err != nil {
		return nil, err
	}
	prober, err := guiRecoveryWorkspaceProber(plan.Task)
	if err != nil {
		return nil, err
	}
	plan, err = service.ProbeWorkspace(ctx, plan, prober)
	if err != nil {
		return nil, err
	}
	summary, err := service.PresentRecoveryDiff(plan)
	if err != nil {
		return nil, err
	}
	review := codingRuntimeRecoveryReview(plan, summary)
	if strings.TrimSpace(reviewDigest) == "" || review.ReviewDigest != strings.TrimSpace(reviewDigest) {
		return review, fmt.Errorf("coding runtime recovery review changed; inspect the latest read-only probe before confirming")
	}
	if err := service.ConfirmContinuation(plan, plan.Interrupted.Policy, confirmed); err != nil {
		return nil, err
	}
	return review, nil
}

func guiRecoveryWorkspaceProber(task codingruntime.Task) (codingruntime.WorkspaceProber, error) {
	switch strings.ToLower(strings.TrimSpace(task.Mode)) {
	case "", "local":
		prober := newGUILocalWorkspaceProber(task.ProjectRef)
		if prober == nil {
			return nil, fmt.Errorf("local recovery requires a declared project path")
		}
		return prober, nil
	case "remote":
		// A recovery service without a live host adapter must fail closed. The
		// normal GUI resume route supplies this adapter through the currently
		// selected SSH session; no old executor command is reused here.
		return nil, fmt.Errorf("remote coding recovery requires a live read-only SSH workspace prober")
	default:
		return nil, fmt.Errorf("unsupported coding recovery mode %q", task.Mode)
	}
}

// newGUIRemoteWorkspaceProber checks the currently live SSH session before it
// submits a deliberately read-only git command. It never reconnects a dead
// session: reconnecting can silently bind recovery to a different host.
func newGUIRemoteWorkspaceProber(handler *IMMessageHandler, sessionID, workDir string, expectedIdentity ...string) codingruntime.WorkspaceProber {
	sessionID, workDir = strings.TrimSpace(sessionID), strings.TrimSpace(workDir)
	if handler == nil || sessionID == "" || workDir == "" {
		return nil
	}
	expected := guiRemoteCodingTargetIdentity(handler, sessionID, workDir)
	if len(expectedIdentity) > 0 && strings.TrimSpace(expectedIdentity[0]) != "" {
		expected = strings.TrimSpace(expectedIdentity[0])
	}
	if expected == "" {
		return nil
	}
	return codingruntime.WorkspaceProberFunc(func(ctx context.Context, task codingruntime.Task, _ codingruntime.Attempt) (*codingruntime.WorkspaceProbe, error) {
		if ctx != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		mgr := handler.ensureSSHManager()
		if mgr == nil {
			return nil, fmt.Errorf("remote recovery session is unavailable; reconnect and verify the host explicitly")
		}
		session, ok := mgr.Get(sessionID)
		if !ok || !guiRuntimeSSHSessionAlive(session) {
			return nil, fmt.Errorf("remote recovery session %q is unavailable; no reconnect was attempted", sessionID)
		}
		hostIdentity := guiRemoteCodingTargetIdentity(handler, sessionID, workDir)
		if hostIdentity == "" || hostIdentity != expected {
			return nil, fmt.Errorf("remote recovery session no longer matches the verified target")
		}
		markerStart, markerEnd, err := guiRuntimeRemoteProbeMarkers()
		if err != nil {
			return nil, err
		}
		command := "git -C " + remoteShellQuote(workDir) + " rev-parse HEAD; printf '\\n" + markerStart + "\\n'; git -C " + remoteShellQuote(workDir) + " status --porcelain=v1 --untracked-files=all; printf '\\n" + markerEnd + "\\n'"
		output, err := handler.sshExecRuntimeBound(sessionID, command, 15, expected, workDir)
		if err != nil {
			return nil, err
		}
		return guiRemoteWorkspaceProbeFromOutput(task, hostIdentity, workDir, output, markerStart, markerEnd, time.Now().UTC())
	})
}

// guiRemoteCodingTargetIdentity is a stable, non-secret binding for a remote
// coding task. It includes the canonical host identity and configured pin
// digest, never passwords/key paths or a user-provided display label.
func guiRemoteCodingTargetIdentity(handler *IMMessageHandler, sessionID, workDir string) string {
	if handler == nil || strings.TrimSpace(sessionID) == "" {
		return ""
	}
	mgr := handler.ensureSSHManager()
	if mgr == nil {
		return ""
	}
	session, ok := mgr.Get(strings.TrimSpace(sessionID))
	if !ok || session == nil {
		return ""
	}
	return guiRemoteCodingTargetIdentityForConfig(session.Spec.HostConfig, workDir)
}

// guiRemoteCodingTargetIdentityForConfig preserves the common corelib remote
// target identity semantics while making the authority boundary independently
// testable: every connection-coordinate and the declared working directory is
// part of the durable Runtime binding.
func guiRemoteCodingTargetIdentityForConfig(config remote.SSHHostConfig, workDir string) string {
	target, err := codingruntime.NormalizeRemoteTarget(codingruntime.RemoteTarget{
		Host:               config.Host,
		User:               config.User,
		Port:               config.Port,
		WorkDir:            workDir,
		HostKeyFingerprint: config.HostKeyFingerprint,
	})
	if err != nil {
		return ""
	}
	identity, err := target.Identity()
	if err != nil {
		return ""
	}
	return identity
}

func guiRuntimeSSHSessionAlive(session *remote.SSHManagedSession) bool {
	if session == nil || session.Handle == nil || !session.Handle.IsAlive() {
		return false
	}
	// A live SSH PTY can legitimately be reported as busy while the previous
	// command's output is being drained, or as waiting_input while the shell is
	// idle.  Both states are usable for a runtime-bound command.  Only terminal
	// states make the frozen session unavailable; requiring exactly "running"
	// caused a race after remote-isolate/bootstrap commands and produced the
	// misleading "session is unavailable" failure.
	status := remote.SessionStatus(session.GetSummary().Status)
	return status != remote.SessionExited && status != remote.SessionError
}

// guiRuntimeRemoteProbeMarkers creates an unpredictable begin/end pair for a
// single PTY probe. The terminal echoes submitted commands, so fixed markers
// could be mistaken for a repository-controlled response.
func guiRuntimeRemoteProbeMarkers() (string, string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", "", fmt.Errorf("generate remote workspace probe marker: %w", err)
	}
	base := "__CODING_RUNTIME_GIT_" + hex.EncodeToString(nonce[:])
	return base + "_BEGIN__", base + "_END__", nil
}

// guiRemoteWorkspaceProbeFromOutput accepts only the final complete nonce
// frame. Choosing the last start marker discards the PTY command echo; a
// closing delimiter prevents partial output from being treated as a probe.
func guiRemoteWorkspaceProbeFromOutput(task codingruntime.Task, identity, workDir, output, markerStart, markerEnd string, observedAt time.Time) (*codingruntime.WorkspaceProbe, error) {
	markerStart, markerEnd = strings.TrimSpace(markerStart), strings.TrimSpace(markerEnd)
	if markerStart == "" || markerEnd == "" || markerStart == markerEnd {
		return nil, fmt.Errorf("remote read-only git probe markers are invalid")
	}
	begin := strings.LastIndex(output, markerStart)
	if begin < 0 {
		return nil, fmt.Errorf("remote read-only git probe did not return its status start marker")
	}
	endOffset := strings.Index(output[begin+len(markerStart):], markerEnd)
	if endOffset < 0 {
		return nil, fmt.Errorf("remote read-only git probe did not return its status end marker")
	}
	end := begin + len(markerStart) + endOffset
	head := guiRuntimeLastNonEmptyLine(output[:begin])
	if len(strings.Fields(head)) != 1 {
		return nil, fmt.Errorf("remote read-only git probe returned no unambiguous HEAD")
	}
	status := strings.Trim(output[begin+len(markerStart):end], "\r\n")
	return &codingruntime.WorkspaceProbe{
		ProjectRef: firstNonEmptyTraceText(task.ProjectRef, workDir),
		Head:       head,
		StatusHash: codingRuntimeDigest(status),
		HostKey:    identity,
		WorkDir:    workDir,
		ObservedAt: observedAt.UTC(),
	}, nil
}

func guiRuntimeLastNonEmptyLine(value string) string {
	lines := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}

func codingRuntimeRecoveryReview(plan *codingruntime.RecoveryPlan, summary string) *CodingRuntimeRecoveryReview {
	if plan == nil {
		return nil
	}
	review := &CodingRuntimeRecoveryReview{
		TaskID: plan.Task.TaskID, AttemptID: plan.Interrupted.AttemptID,
		Mode: plan.Task.Mode, ProjectRef: plan.Task.ProjectRef,
		PolicyDigest: plan.Interrupted.Policy.Digest, Before: plan.Before,
		After: plan.After, Observed: plan.Observed, Children: append([]codingruntime.ChildRecoveryState(nil), plan.Children...), Changed: plan.WorkspaceChanged,
		Summary: summary,
	}
	review.ReviewDigest = codingRuntimeRecoveryDigest(*review)
	return review
}

func codingRuntimeRecoveryDigest(review CodingRuntimeRecoveryReview) string {
	observed := review.Observed
	fields := []string{review.TaskID, review.AttemptID, review.PolicyDigest, review.Mode, review.ProjectRef}
	if observed != nil {
		fields = append(fields, observed.ProjectRef, observed.Head, observed.StatusHash, observed.FilesHash, observed.HostKey, observed.WorkDir)
	}
	for _, child := range review.Children {
		fields = append(fields, child.TaskID, string(child.Status))
	}
	sum := sha256.Sum256([]byte(strings.Join(fields, "\x00")))
	return fmt.Sprintf("sha256:%x", sum[:])
}
