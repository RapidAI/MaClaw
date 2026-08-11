package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
)

// codingRuntimeRecoveryResponse is intentionally narrower than
// codingruntime.RecoveryPlan. In particular, it never exposes RequestedWork,
// old provider output, commands, tool arguments, or any replay payload.
type codingRuntimeRecoveryResponse struct {
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

type codingRuntimeRecoveryConfirmation struct {
	ReviewDigest string `json:"review_digest"`
	Confirmed    bool   `json:"confirmed"`
}

func (s *HTTPServer) handleGetCodingRuntimeRecovery(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	review, err := s.prepareCodingRuntimeRecovery(r.Context(), p, r.PathValue("instanceId"), r.PathValue("sessionId"), r.PathValue("taskId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, review)
}

func (s *HTTPServer) handleConfirmCodingRuntimeRecovery(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in codingRuntimeRecoveryConfirmation
	if !decodeJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.ReviewDigest) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "review_digest is required"})
		return
	}
	plan, summary, err := s.prepareCodingRuntimeRecoveryPlan(r.Context(), p, r.PathValue("instanceId"), r.PathValue("sessionId"), r.PathValue("taskId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	review := newCodingRuntimeRecoveryResponse(plan, summary)
	if review.ReviewDigest != strings.TrimSpace(in.ReviewDigest) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "coding runtime recovery review changed; inspect the latest read-only probe before confirming", "review": review})
		return
	}
	// The review was created from a fresh read-only probe just above. Its policy
	// is frozen in the interrupted attempt, and confirmation only queues a new
	// attempt; it never executes a model, tool, or old command here.
	if err := (codingruntime.RecoveryService{Store: s.codingRuntimeStore}).ConfirmContinuation(plan, plan.Interrupted.Policy, in.Confirmed); err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"review": review, "confirmed": in.Confirmed, "status": recoveryConfirmationStatus(in.Confirmed)})
}

func recoveryConfirmationStatus(confirmed bool) string {
	if confirmed {
		return "queued_without_replay"
	}
	return "declined_without_replay"
}

func (s *HTTPServer) prepareCodingRuntimeRecovery(ctx context.Context, p agentservice.Principal, instanceID, sessionID, taskID string) (*codingRuntimeRecoveryResponse, error) {
	plan, summary, err := s.prepareCodingRuntimeRecoveryPlan(ctx, p, instanceID, sessionID, taskID)
	if err != nil {
		return nil, err
	}
	return newCodingRuntimeRecoveryResponse(plan, summary), nil
}

func (s *HTTPServer) prepareCodingRuntimeRecoveryPlan(ctx context.Context, p agentservice.Principal, instanceID, sessionID, taskID string) (*codingruntime.RecoveryPlan, string, error) {
	if s == nil || s.svc == nil || s.codingRuntimeStore == nil {
		return nil, "", fmt.Errorf("coding runtime ledger is unavailable")
	}
	inst, err := s.svc.GetInstance(ctx, p, strings.TrimSpace(instanceID))
	if err != nil {
		return nil, "", err
	}
	session, err := s.svc.GetSession(ctx, p, inst.ID, strings.TrimSpace(sessionID))
	if err != nil {
		return nil, "", err
	}
	service := codingruntime.RecoveryService{Store: s.codingRuntimeStore}
	plan, err := service.PrepareRecoveryForTask(strings.TrimSpace(taskID))
	if err != nil {
		return nil, "", err
	}
	if err := authorizeServiceCodingRuntimeRecovery(p, *inst, *session, plan.Task, plan.Interrupted.Policy); err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(plan.Task.ProjectRef) == "" {
		return nil, "", fmt.Errorf("service recovery requires a declared coding workspace")
	}
	prober, err := s.codingRuntimeRecoveryWorkspaceProber(p, plan.Task, plan.Interrupted.Policy)
	if err != nil {
		return nil, "", err
	}
	plan, err = service.ProbeWorkspace(ctx, plan, prober)
	if err != nil {
		return nil, "", err
	}
	summary, err := service.PresentRecoveryDiff(plan)
	if err != nil {
		return nil, "", err
	}
	return plan, summary, nil
}

func (s *HTTPServer) codingRuntimeRecoveryWorkspaceProber(p agentservice.Principal, task codingruntime.Task, policy codingruntime.PolicySnapshot) (codingruntime.WorkspaceProber, error) {
	if s != nil && s.codingRuntimeRecoveryProber != nil {
		return s.codingRuntimeRecoveryProber(task), nil
	}
	switch strings.ToLower(strings.TrimSpace(task.Mode)) {
	case "", "local":
		return codingruntime.NewLocalGitWorkspaceProber(task.ProjectRef), nil
	case "remote":
		if s == nil || s.svc == nil {
			return nil, fmt.Errorf("remote coding recovery service is unavailable")
		}
		cfg, err := s.svc.GetUserConfig(context.Background(), p)
		if err != nil || cfg == nil {
			return nil, fmt.Errorf("remote coding recovery configuration is unavailable")
		}
		return s.svc.CodingRuntimeRemoteRecoveryProber(p, cfg.AppConfig, task, policy)
	default:
		return nil, fmt.Errorf("unsupported coding recovery mode %q", task.Mode)
	}
}

func authorizeServiceCodingRuntimeRecovery(p agentservice.Principal, inst agentservice.Instance, session agentservice.Session, task codingruntime.Task, policy codingruntime.PolicySnapshot) error {
	expectedOwner := "srv:" + strings.TrimSpace(p.TenantID) + ":" + strings.TrimSpace(p.UserID) + ":" + strings.TrimSpace(session.ID)
	if task.OwnerID != expectedOwner {
		// Deliberately avoid telling an authenticated but unrelated principal
		// whether a runtime task exists or which workspace it used.
		return agentservice.ErrSessionNotFound
	}
	if strings.EqualFold(strings.TrimSpace(task.Mode), "remote") {
		// Remote ProjectRef is a remote POSIX workdir, not the MaClawSrv
		// instance's local workspace. Bind it to the current instance through
		// the stable task ID, which includes tenant/user/instance/session,
		// workflow/phase, mode and frozen remote target identity.
		expectedTaskID := serviceCodingRuntimeTaskIDForRecovery(p, inst, session, task, policy)
		if task.TaskID != expectedTaskID {
			return agentservice.ErrSessionNotFound
		}
		return nil
	}
	if strings.TrimSpace(task.ProjectRef) != strings.TrimSpace(inst.Workspace) {
		return agentservice.ErrSessionNotFound
	}
	return nil
}

func serviceCodingRuntimeTaskIDForRecovery(p agentservice.Principal, inst agentservice.Instance, session agentservice.Session, task codingruntime.Task, policy codingruntime.PolicySnapshot) string {
	fields := []string{strings.TrimSpace(p.TenantID), strings.TrimSpace(p.UserID), strings.TrimSpace(inst.ID), strings.TrimSpace(session.ID), strings.TrimSpace(task.WorkflowID), strings.TrimSpace(task.PhaseID), "remote", strings.TrimSpace(policy.RemoteTarget)}
	sum := sha256.Sum256([]byte(strings.Join(fields, "\n")))
	return fmt.Sprintf("srv-coding-%x", sum[:16])
}

func newCodingRuntimeRecoveryResponse(plan *codingruntime.RecoveryPlan, summary string) *codingRuntimeRecoveryResponse {
	if plan == nil {
		return nil
	}
	review := &codingRuntimeRecoveryResponse{
		TaskID: plan.Task.TaskID, AttemptID: plan.Interrupted.AttemptID, Mode: plan.Task.Mode,
		ProjectRef: plan.Task.ProjectRef, PolicyDigest: plan.Interrupted.Policy.Digest,
		Before: plan.Before, After: plan.After, Observed: plan.Observed,
		Children: append([]codingruntime.ChildRecoveryState(nil), plan.Children...), Changed: plan.WorkspaceChanged, Summary: summary,
	}
	review.ReviewDigest = codingRuntimeRecoveryReviewDigest(*review)
	return review
}

func codingRuntimeRecoveryReviewDigest(review codingRuntimeRecoveryResponse) string {
	fields := []string{review.TaskID, review.AttemptID, review.PolicyDigest, review.Mode, review.ProjectRef}
	if review.Observed != nil {
		fields = append(fields, review.Observed.ProjectRef, review.Observed.Head, review.Observed.StatusHash, review.Observed.FilesHash, review.Observed.HostKey, review.Observed.WorkDir)
	}
	for _, child := range review.Children {
		fields = append(fields, child.TaskID, string(child.Status))
	}
	sum := sha256.Sum256([]byte(strings.Join(fields, "\x00")))
	return fmt.Sprintf("sha256:%x", sum[:])
}
