package agentservice

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

const (
	metaCodingRuntimeRemoteHost       = "coding_runtime_remote_host"
	metaCodingRuntimeRemoteUser       = "coding_runtime_remote_user"
	metaCodingRuntimeRemotePort       = "coding_runtime_remote_port"
	metaCodingRuntimeRemoteWorkDir    = "coding_runtime_remote_workdir"
	metaCodingRuntimeRemoteHostKeyPin = "coding_runtime_remote_host_key_fingerprint"
)

// remoteCodingRuntimeBinding ties one Attempt to the exact already-verified
// SSH session. It is deliberately process-local and never serialized: a
// recovery must obtain a fresh, authenticated review and must not reconnect.
type remoteCodingRuntimeBinding struct {
	Target    codingruntime.RemoteTarget
	Identity  string
	SessionID string
}

func isExplicitRemoteCodingRuntimeRequest(req ExecuteRequest) bool {
	if req.Message.Metadata == nil || strings.TrimSpace(req.Message.Metadata[metaCodingRuntimeMode]) != "remote_workflow" {
		return false
	}
	return strings.TrimSpace(req.Message.Metadata[metaCodingRuntimeWorkflowID]) != "" && strings.TrimSpace(req.Message.Metadata[metaCodingRuntimePhaseID]) != "" && req.MutationScope == v2.MutationScopeProject
}

func remoteCodingRuntimeTargetFromRequest(req ExecuteRequest) (codingruntime.RemoteTarget, error) {
	meta := req.Message.Metadata
	if meta == nil {
		return codingruntime.RemoteTarget{}, fmt.Errorf("remote coding runtime metadata is required")
	}
	port := 22
	if raw := strings.TrimSpace(meta[metaCodingRuntimeRemotePort]); raw != "" {
		if _, err := fmt.Sscanf(raw, "%d", &port); err != nil {
			return codingruntime.RemoteTarget{}, fmt.Errorf("remote coding runtime port is invalid")
		}
	}
	return codingruntime.NormalizeRemoteTarget(codingruntime.RemoteTarget{
		Host:               meta[metaCodingRuntimeRemoteHost],
		User:               meta[metaCodingRuntimeRemoteUser],
		Port:               port,
		WorkDir:            meta[metaCodingRuntimeRemoteWorkDir],
		HostKeyFingerprint: meta[metaCodingRuntimeRemoteHostKeyPin],
	})
}

func (e *CoreAgentExecutor) bindRemoteCodingRuntime(req ExecuteRequest, target codingruntime.RemoteTarget) (remoteCodingRuntimeBinding, error) {
	identity, err := target.Identity()
	if err != nil {
		return remoteCodingRuntimeBinding{}, err
	}
	if !serviceConfiguredRemoteTargetMatches(req.Config, target) {
		return remoteCodingRuntimeBinding{}, fmt.Errorf("remote coding target is not a currently configured pinned SSH host")
	}
	if e == nil {
		return remoteCodingRuntimeBinding{}, fmt.Errorf("remote coding executor is unavailable")
	}
	resources := e.sshResourcesForUser(req.Principal.TenantID, req.Principal.UserID)
	if resources == nil || resources.mgr == nil {
		return remoteCodingRuntimeBinding{}, fmt.Errorf("remote coding SSH session manager is unavailable")
	}
	for _, session := range resources.mgr.List() {
		if session == nil || !serviceRemoteCodingSessionMatches(session, target) || !serviceRemoteCodingSessionAlive(session) {
			continue
		}
		return remoteCodingRuntimeBinding{Target: target, Identity: identity, SessionID: session.ID}, nil
	}
	return remoteCodingRuntimeBinding{}, fmt.Errorf("remote coding requires an already verified live SSH session for the pinned target")
}

func serviceConfiguredRemoteTargetMatches(cfg corelib.AppConfig, target codingruntime.RemoteTarget) bool {
	for _, host := range configuredSSHHostsFrom(cfg.SSHHosts) {
		configured, err := codingruntime.NormalizeRemoteTarget(codingruntime.RemoteTarget{Host: host.Host, User: host.User, Port: host.Port, WorkDir: target.WorkDir, HostKeyFingerprint: host.HostKeyFingerprint})
		if err == nil && configured == target {
			return true
		}
	}
	return false
}

// CodingRuntimeRemoteRecoveryProber resolves only an already-live SSH session
// for the frozen remote policy. It never connects or reconnects. A changed
// SSH profile (including a different host-key pin), a missing pin, or a dead
// session all fail closed and leave recovery pending explicit host review.
func (e *CoreAgentExecutor) CodingRuntimeRemoteRecoveryProber(p Principal, cfg corelib.AppConfig, task codingruntime.Task, policy codingruntime.PolicySnapshot) (codingruntime.WorkspaceProber, error) {
	if e == nil || strings.ToLower(strings.TrimSpace(task.Mode)) != "remote" {
		return nil, fmt.Errorf("remote coding recovery is unavailable")
	}
	expected := strings.TrimSpace(policy.RemoteTarget)
	if expected == "" || strings.TrimSpace(task.ProjectRef) == "" {
		return nil, fmt.Errorf("remote coding recovery target identity is unavailable")
	}
	var matches []codingruntime.RemoteTarget
	for _, host := range configuredSSHHostsFrom(cfg.SSHHosts) {
		target, err := codingruntime.NormalizeRemoteTarget(codingruntime.RemoteTarget{Host: host.Host, User: host.User, Port: host.Port, WorkDir: task.ProjectRef, HostKeyFingerprint: host.HostKeyFingerprint})
		if err != nil {
			continue
		}
		identity, err := target.Identity()
		if err == nil && identity == expected {
			matches = append(matches, target)
		}
	}
	if len(matches) != 1 {
		return nil, fmt.Errorf("remote coding recovery requires exactly one currently configured pinned target matching the interrupted task")
	}
	resources := e.sshResourcesForUser(p.TenantID, p.UserID)
	if resources == nil || resources.mgr == nil {
		return nil, fmt.Errorf("remote coding recovery session manager is unavailable")
	}
	for _, session := range resources.mgr.List() {
		if session != nil && serviceRemoteCodingSessionMatches(session, matches[0]) && serviceRemoteCodingSessionAlive(session) {
			return serviceRemoteReadOnlyWorkspaceProber(resources, remoteCodingRuntimeBinding{Target: matches[0], Identity: expected, SessionID: session.ID}), nil
		}
	}
	return nil, fmt.Errorf("remote coding recovery requires an already verified live SSH session; no reconnect was attempted")
}

func serviceRemoteCodingSessionMatches(session *remote.SSHManagedSession, target codingruntime.RemoteTarget) bool {
	if session == nil {
		return false
	}
	cfg := session.Spec.HostConfig
	port := cfg.Port
	if port == 0 {
		port = 22
	}
	return strings.EqualFold(strings.TrimSpace(cfg.Host), target.Host) && strings.TrimSpace(cfg.User) == target.User && port == target.Port && strings.TrimSpace(cfg.HostKeyFingerprint) == target.HostKeyFingerprint
}

func serviceRemoteCodingSessionAlive(session *remote.SSHManagedSession) bool {
	if session == nil || session.Handle == nil {
		return false
	}
	summary := session.GetSummary()
	return remote.SessionStatus(summary.Status).IsRunning() && session.Handle.IsAlive()
}

func serviceRemoteShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\\"'\\\"'") + "'"
}

func serviceRemoteReadOnlyWorkspaceProber(resources *coreAgentSSHResources, binding remoteCodingRuntimeBinding) codingruntime.WorkspaceProber {
	if resources == nil || resources.mgr == nil || strings.TrimSpace(binding.SessionID) == "" || strings.TrimSpace(binding.Identity) == "" {
		return nil
	}
	return codingruntime.WorkspaceProberFunc(func(ctx context.Context, task codingruntime.Task, _ codingruntime.Attempt) (*codingruntime.WorkspaceProbe, error) {
		if ctx != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		session, ok := resources.mgr.Get(binding.SessionID)
		if !ok || !serviceRemoteCodingSessionMatches(session, binding.Target) || !serviceRemoteCodingSessionAlive(session) {
			return nil, fmt.Errorf("verified remote recovery session is unavailable; reconnect and review explicitly")
		}
		markerStart, markerEnd, markerErr := serviceRemoteProbeMarkers()
		if markerErr != nil {
			return nil, markerErr
		}
		command := "git -C " + serviceRemoteShellQuote(binding.Target.WorkDir) + " rev-parse HEAD; printf '\\n" + markerStart + "\\n'; git -C " + serviceRemoteShellQuote(binding.Target.WorkDir) + " status --porcelain=v1 --untracked-files=all; printf '\\n" + markerEnd + "\\n'"
		output, err := serviceRemoteSSHExecReadOnly(resources.mgr, binding.SessionID, command, 15)
		if err != nil {
			return nil, err
		}
		return serviceRemoteWorkspaceProbeFromOutput(task, binding, output, markerStart, markerEnd, time.Now().UTC())
	})
}

// serviceRemoteProbeMarkers creates a fresh delimiter pair for one fixed
// read-only probe. PTY shells normally echo the submitted command, so a
// fixed marker would occur before the actual Git output and can be parsed as
// a fabricated result. A per-probe nonce plus a closing marker lets the
// parser select the final, command-produced frame without trusting terminal
// echo or repository-controlled filenames.
func serviceRemoteProbeMarkers() (string, string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", "", fmt.Errorf("generate remote recovery probe marker: %w", err)
	}
	base := "__CODING_RUNTIME_GIT_" + hex.EncodeToString(nonce[:])
	return base + "_BEGIN__", base + "_END__", nil
}

// serviceRemoteWorkspaceProbeFromOutput extracts only the final nonce-framed
// Git result. It intentionally uses the last begin marker: the first pair is
// commonly present in the PTY's command echo. The markers are generated only
// after the live session is verified, so a workspace cannot pre-create a
// matching porcelain filename to forge the frame.
func serviceRemoteWorkspaceProbeFromOutput(task codingruntime.Task, binding remoteCodingRuntimeBinding, output, markerStart, markerEnd string, observedAt time.Time) (*codingruntime.WorkspaceProbe, error) {
	markerStart = strings.TrimSpace(markerStart)
	markerEnd = strings.TrimSpace(markerEnd)
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
	head := serviceRemoteLastNonEmptyLine(output[:begin])
	if len(strings.Fields(head)) != 1 {
		return nil, fmt.Errorf("remote read-only git probe returned no unambiguous HEAD")
	}
	status := strings.Trim(output[begin+len(markerStart):end], "\r\n")
	sum := sha256.Sum256([]byte(status))
	return &codingruntime.WorkspaceProbe{
		ProjectRef: firstNonEmptyString(task.ProjectRef, binding.Target.WorkDir),
		Head:       head,
		StatusHash: fmt.Sprintf("sha256:%x", sum[:]),
		HostKey:    binding.Identity,
		WorkDir:    binding.Target.WorkDir,
		ObservedAt: observedAt.UTC(),
	}, nil
}

func serviceRemoteLastNonEmptyLine(value string) string {
	lines := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}

// serviceRemoteSSHExecReadOnly does not call sshtool.SSHExec because that
// generic helper reconnects a dead session. Recovery must never silently
// bind a task to a newly connected machine. The command is constructed only
// by this package from a frozen workdir and fixed git inspection arguments.
func serviceRemoteSSHExecReadOnly(mgr *remote.SSHSessionManager, sessionID, command string, waitSeconds int) (string, error) {
	if mgr == nil {
		return "", fmt.Errorf("remote recovery session manager is unavailable")
	}
	session, ok := mgr.Get(sessionID)
	if !ok || !serviceRemoteCodingSessionAlive(session) {
		return "", fmt.Errorf("remote recovery session is unavailable")
	}
	before := session.LineCount()
	if err := mgr.WriteInput(sessionID, command); err != nil {
		return "", fmt.Errorf("submit remote read-only git probe: %w", err)
	}
	if waitSeconds <= 0 {
		waitSeconds = 15
	}
	lines, _ := mgr.WaitForOutput(sessionID, before, time.Duration(waitSeconds)*time.Second)
	output := strings.Join(lines, "\n")
	if strings.TrimSpace(output) == "" {
		return "", fmt.Errorf("remote read-only git probe returned no output")
	}
	return output, nil
}

func serviceRemoteSSHExecBound(resources *coreAgentSSHResources, binding remoteCodingRuntimeBinding, command string, waitSeconds int) (string, error) {
	if resources == nil || resources.mgr == nil {
		return "", fmt.Errorf("remote coding SSH session manager is unavailable")
	}
	session, ok := resources.mgr.Get(binding.SessionID)
	if !ok || !serviceRemoteCodingSessionMatches(session, binding.Target) || !serviceRemoteCodingSessionAlive(session) {
		return "", fmt.Errorf("verified remote coding session is unavailable; no reconnect was attempted")
	}
	before := session.LineCount()
	if err := resources.mgr.WriteInput(binding.SessionID, command); err != nil {
		return "", fmt.Errorf("submit remote coding command: %w", err)
	}
	if waitSeconds <= 0 {
		waitSeconds = 15
	}
	if waitSeconds > 600 {
		waitSeconds = 600
	}
	lines, status := resources.mgr.WaitForOutput(binding.SessionID, before, time.Duration(waitSeconds)*time.Second)
	output := strings.Join(lines, "\n")
	if len(output) > 8000 {
		output = output[:4000] + "\n... (truncated) ...\n" + output[len(output)-4000:]
	}
	return fmt.Sprintf("[%s] status: %s\n$ %s\n%s", binding.SessionID, status, command, output), nil
}

func (e *CoreAgentExecutor) executeRemoteCodingRuntime(ctx context.Context, req ExecuteRequest, store codingruntime.Store) (*ExecuteResult, error) {
	target, err := remoteCodingRuntimeTargetFromRequest(req)
	if err != nil {
		return nil, err
	}
	identity, err := target.Identity()
	if err != nil {
		return nil, err
	}
	policy := codingruntime.PolicySnapshot{ProjectRoot: target.WorkDir, RemoteTarget: identity, Mode: "remote", FinalWorkspaceGateRequired: true}
	digest, err := codingruntime.PolicyDigest(policy)
	if err != nil {
		return nil, fmt.Errorf("freeze remote coding runtime policy: %w", err)
	}
	policy.Digest = digest
	taskID := serviceCodingRuntimeTaskID(req)
	if existing, getErr := store.GetTask(taskID); getErr == nil && existing.Status == codingruntime.TaskInterrupted {
		return nil, codingruntime.ErrRecoveryRequired
	} else if getErr != nil && getErr != codingruntime.ErrNotFound {
		return nil, fmt.Errorf("load remote coding runtime task: %w", getErr)
	} else if getErr == nil && existing.Status != codingruntime.TaskQueued && existing.Status != codingruntime.TaskWaitingApproval {
		return nil, fmt.Errorf("remote coding runtime task is not ready for a new attempt: %s", existing.Status)
	}
	binding, err := e.bindRemoteCodingRuntime(req, target)
	if err != nil {
		return nil, err
	}

	resources := e.sshResourcesForUser(req.Principal.TenantID, req.Principal.UserID)
	var directResult *ExecuteResult
	executor := codingruntimeExecutorFunc(func(runCtx context.Context, runtimeRequest codingruntime.ExecutionRequest) codingruntime.ExecutionResult {
		out, executeErr := e.executeDirectWithRuntimeBinding(runCtx, req, store, &runtimeRequest.Attempt, &binding)
		if runCtx != nil && runCtx.Err() != nil {
			return codingruntime.ExecutionResult{Status: codingruntime.TaskInterrupted, SideEffectState: codingruntime.SideEffectUncertain, ErrorCode: "remote_service_request_cancelled", ErrorSummary: "remote coding request was cancelled; side effects require a read-only recovery probe"}
		}
		if executeErr != nil {
			return codingruntime.ExecutionResult{Status: codingruntime.TaskFailed, SideEffectState: codingruntime.SideEffectUncertain, ErrorCode: "remote_service_agent_loop_failed", ErrorSummary: "remote coding-agent loop failed; inspect host-local diagnostics"}
		}
		directResult = out
		if out != nil && out.Metadata != nil && normalizeResponseSourceKind(out.Metadata[metaResponseSource]).IsWaitingForUser() {
			return codingruntime.ExecutionResult{Status: codingruntime.TaskBlocked, SideEffectState: codingruntime.SideEffectNone, ErrorCode: "remote_service_agent_waiting_for_user", ErrorSummary: "remote coding-agent requires explicit user input before a new attempt", Evidence: []codingruntime.Evidence{{Type: "remote_service_agent_waiting_for_user", Digest: serviceCodingRuntimeDigest(out)}}}
		}
		if out != nil && out.Metadata != nil && strings.EqualFold(strings.TrimSpace(out.Metadata["hard_exit"]), "true") {
			return codingruntime.ExecutionResult{Status: codingruntime.TaskFailed, SideEffectState: codingruntime.SideEffectUncertain, ErrorCode: "remote_service_agent_hard_exit", ErrorSummary: "remote coding-agent exited abnormally; inspect host-local diagnostics", Evidence: []codingruntime.Evidence{{Type: "remote_service_agent_hard_exit", Digest: serviceCodingRuntimeDigest(out)}}}
		}
		return codingruntime.ExecutionResult{Status: codingruntime.TaskCompleted, SideEffectState: codingruntime.SideEffectObserved, Evidence: []codingruntime.Evidence{{Type: "remote_service_agent_completion", Digest: serviceCodingRuntimeDigest(out)}}}
	})
	runner := codingruntime.Runner{Store: store, LeaseOwner: serviceCodingRuntimeOwner(req), LeaseDuration: 15 * time.Minute, WorkspaceProber: serviceRemoteReadOnlyWorkspaceProber(resources, binding)}
	task, attempt, runErr := runner.Run(ctx, codingruntime.Task{TaskID: taskID, WorkflowID: strings.TrimSpace(req.Message.Metadata[metaCodingRuntimeWorkflowID]), PhaseID: strings.TrimSpace(req.Message.Metadata[metaCodingRuntimePhaseID]), OwnerID: serviceCodingRuntimeOwner(req), ProjectRef: target.WorkDir, Mode: "remote", RequestedWork: strings.TrimSpace(req.Message.Content), PolicyDigest: digest}, policy, executor)
	if runErr != nil {
		return nil, runErr
	}
	if directResult == nil {
		directResult = &ExecuteResult{OutputType: "text/plain"}
	}
	if directResult.Metadata == nil {
		directResult.Metadata = map[string]string{}
	}
	directResult.Metadata[metaCodingRuntimeTaskID] = task.TaskID
	directResult.Metadata[metaCodingRuntimeTaskStatus] = string(task.Status)
	if attempt != nil {
		directResult.Metadata[metaCodingRuntimeAttemptID] = attempt.AttemptID
	}
	if task.Status == codingruntime.TaskInterrupted {
		return nil, context.Canceled
	}
	if task.Status == codingruntime.TaskFailed {
		return nil, fmt.Errorf("remote coding runtime attempt failed; inspect host-local diagnostics")
	}
	if task.Status == codingruntime.TaskBlocked && directResult.Metadata[metaResponseSource] == "" {
		directResult.Content = "Remote coding runtime attempt ended as " + string(task.Status) + ". No automatic replay was performed."
	}
	return directResult, nil
}

// remote runtime requests may execute only on their frozen session. The
// generic ssh tool remains available for normal non-runtime use, but a remote
// coding attempt cannot create/reconnect/switch sessions or transfer files.
func (c *coreAgentCallbacks) remoteCodingRuntimeToolCallAllowed(name string, args map[string]interface{}) (bool, string) {
	if c == nil || c.runtimeRemoteBinding == nil {
		return true, ""
	}
	if strings.TrimSpace(name) != "ssh" {
		return false, "remote coding runtime exposes only its session-bound SSH execution tool"
	}
	action := strings.TrimSpace(agent.StringArg(args, "action"))
	if action != "exec" {
		return false, "remote coding runtime allows only SSH exec on its verified session"
	}
	if strings.TrimSpace(agent.StringArg(args, "session_id")) != c.runtimeRemoteBinding.SessionID {
		return false, "remote coding runtime SSH session does not match the verified task target"
	}
	if v2.IsDangerousCommand(agent.StringArg(args, "command")) {
		return false, "remote coding runtime rejected a dangerous SSH command"
	}
	return true, ""
}

func (c *coreAgentCallbacks) remoteCodingRuntimeToolAllowed(name string) bool {
	return c == nil || c.runtimeRemoteBinding == nil || strings.TrimSpace(name) == "ssh"
}
