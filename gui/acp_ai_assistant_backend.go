package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
)

// AIAssistantExternalCallbacks streams desktop AI assistant output to external
// hosts (ACP Mode B). Used for programming-agent sessions where projectPath is
// the editor workspace (session/new.cwd).
type AIAssistantExternalCallbacks struct {
	OnToken      func(delta string)
	OnProgress   func(text string)
	OnNewRound   func()
	OnStreamDone func()
	// OnToolEvent powers Cursor-like tool chips in VS Code (tool_call UI).
	OnToolEvent func(ev ACPToolEvent)
}

// acpProgrammingRuntimeTaskID is an opaque stable task identity for exactly
// one ACP prompt. It intentionally includes the ACP session owner and cwd so
// two editor windows in the same repository cannot accidentally share a
// cancellation or lease. The request ID remains part of the identity because
// ACP has no user-approved cross-prompt continuation/replay contract.
func acpProgrammingRuntimeTaskID(ownerID, workspace, requestID string) string {
	sum := sha256.Sum256([]byte("acp-programming-runtime-v1\n" + strings.TrimSpace(ownerID) + "\n" + normalizeProjectSessionPath(workspace) + "\n" + strings.TrimSpace(requestID)))
	return fmt.Sprintf("acp-coding-%x", sum[:16])
}

// acpPromptMayMutateWorkspace is deliberately conservative and is used only
// to decide whether the existing generic ACP turn gets the writer-quality
// workspace gate. It does not route a request or grant tool authority: ACP's
// normal confirmation and per-tool permission checks remain authoritative.
func acpPromptMayMutateWorkspace(text string) bool {
	text = strings.ToLower(acpUserFacingText(text))
	for _, marker := range []string{
		"implement", "create", "add ", "write ", "edit ", "modify", "change ", "fix ", "refactor", "patch ",
		"实现", "创建", "新增", "添加", "编写", "修改", "修复", "重构", "开发", "代码",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

type acpProgrammingRuntimeExecutor func(context.Context, codingruntime.ExecutionRequest) codingruntime.ExecutionResult

func (f acpProgrammingRuntimeExecutor) Execute(ctx context.Context, request codingruntime.ExecutionRequest) codingruntime.ExecutionResult {
	return f(ctx, request)
}

func (a *App) cancelACPProgrammingRuntimeTask(taskID string) {
	if a == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	if err := cancelACPProgrammingRuntimeTaskInStore(a.ensureCodingRuntimeStore(), taskID, time.Now().UTC()); err != nil {
		log.Printf("[acp-mode-b] durable runtime cancel task=%s: %v", taskID, err)
	}
}

// cancelACPProgrammingRuntimeTaskInStore closes the Ledger task before the
// host cancels its in-process ACP/assistant context. It is deliberately
// idempotent for an already absent task so cancellation remains best-effort
// when a non-mutating ACP turn never created a Runtime record.
func cancelACPProgrammingRuntimeTaskInStore(store codingruntime.Store, taskID string, now time.Time) error {
	if store == nil || strings.TrimSpace(taskID) == "" {
		return nil
	}
	_, err := store.CancelTask(taskID, now.UTC())
	if errors.Is(err, codingruntime.ErrNotFound) {
		return nil
	}
	return err
}

// acpProgrammingUserText wraps the VS Code user prompt with an explicit
// workspace contract so the shared GUI agent edits the editor folder on disk
// (VS Code refreshes those files — no second agent stack).
func acpProgrammingUserText(workspace, userText string) string {
	userText = strings.TrimSpace(userText)
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return userText
	}
	return fmt.Sprintf(`[VS Code / ACP programming workspace]
cwd: %s

Instructions for this turn:
- You are the programming agent for the VS Code workspace above (same MaClaw GUI AI assistant).
- Prefer tools that read/write/list files and run commands under this cwd.
- Persist real file changes to disk so the user can open and use them immediately in VS Code.
- In the final reply, list any paths you created or modified (relative to cwd when possible).

User request:
%s`, workspace, userText)
}

// RunAIAssistantProgrammingPrompt runs the same desktop AI assistant path as
// the GUI chat, bound to a project/workspace directory (programming agent).
// It blocks until the turn finishes or ctx is cancelled.
//
// Product rule: this is the ONLY agent brain for ACP Mode B. The bridge is a
// thin protocol adapter — do not reimplement tools/LLM in the bridge binary.
func (a *App) RunAIAssistantProgrammingPrompt(
	ctx context.Context,
	req AIAssistantSendRequest,
	cb AIAssistantExternalCallbacks,
) (*IMAgentResponse, error) {
	if a == nil {
		return nil, fmt.Errorf("app is nil")
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return nil, fmt.Errorf("message text is required")
	}
	a.ensureInteractionInfra()
	projectPath := normalizeProjectSessionPath(req.ProjectPath)
	req.ProjectPath = projectPath
	userID := desktopAIAssistantUserIDForProjectPath(projectPath)
	if sessionOwnerID := strings.TrimSpace(req.SessionOwnerID); sessionOwnerID != "" {
		if !isACPAssistantSessionUserID(sessionOwnerID) {
			return nil, fmt.Errorf("invalid ACP session owner: %q", sessionOwnerID)
		}
		userID = sessionOwnerID
		a.bindACPAssistantSessionWorkingDir(userID, projectPath)
	}

	if projectPath != "" && a.isProjectTaskClosed(projectPath) {
		a.stopAIAssistantOwnerRuntime(userID)
		return nil, fmt.Errorf("project task is closed: %s", projectPath)
	}

	// Passthrough slash commands (same as GUI). Match original text, not the
	// workspace-wrapped body (wrappers would break /slash detection).
	if resp, handled := a.TryHandlePassthroughSlashCommandWithSource(text, "acp-mode-b:"+userID); handled {
		return resp, nil
	}

	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return nil, fmt.Errorf("AI assistant backend is unavailable (hub/local handler not ready)")
	}

	requestID := strings.TrimSpace(req.RequestID)
	if requestID == "" {
		requestID = fmt.Sprintf("acp-prog-%d", time.Now().UnixNano())
	}
	// Keep ordinary ACP questions on the established shared-chat path. Only a
	// likely workspace mutation gets a durable writer task (and consequently a
	// cancellation record) so cancelling a simple question never creates a
	// needless Ledger database/task.
	mutatingRuntime := acpPromptMayMutateWorkspace(text) && projectPath != ""

	msgLang := strings.TrimSpace(req.Lang)
	if msgLang == "" {
		msgLang = a.CurrentLanguage
	}
	// Same GUI IM path; tools resolve cwd via desktop-user:{projectPath}.
	agentText := acpProgrammingUserText(projectPath, text)
	msg := IMUserMessage{
		RequestID:    requestID,
		UserID:       userID,
		Platform:     desktopPlatform,
		Text:         agentText,
		CancelCtx:    ctx,
		Lang:         msgLang,
		ResumeSlotID: strings.TrimSpace(req.ResumeSlotID),
		StartNewTask: req.StartNewTask,
		UIAction:     req.UIAction,
	}

	onProgress := func(progressText string) {
		if cb.OnProgress != nil {
			cb.OnProgress(progressText)
		}
	}
	streamDeltaNormalizer := &aiAssistantStreamDeltaNormalizer{}
	onToken := func(delta string) {
		delta = streamDeltaNormalizer.Normalize(delta)
		if delta == "" {
			return
		}
		if cb.OnToken != nil {
			cb.OnToken(delta)
		}
	}
	onNewRound := func() {
		streamDeltaNormalizer.Reset()
		if cb.OnNewRound != nil {
			cb.OnNewRound()
		}
	}
	onStreamDone := func() {
		if cb.OnStreamDone != nil {
			cb.OnStreamDone()
		}
	}

	// Watch ctx for cancel → close the durable task before stopping the live
	// assistant session. The ledger transition is the cross-restart authority;
	// LoopContext cancellation only stops this process's current work.
	runtimeTaskID := ""
	if mutatingRuntime {
		runtimeTaskID = acpProgrammingRuntimeTaskID(userID, projectPath, requestID)
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			a.cancelACPProgrammingRuntimeTask(runtimeTaskID)
			if _, err := a.CancelAIAssistantSessionForSession(userID); err != nil {
				log.Printf("[acp-mode-b] cancel session: %v", err)
			}
			a.stopAIAssistantOwnerRuntime(userID)
		case <-done:
		}
	}()

	// Tool chips: agent loop emits start/end → Mode B host → VS Code tool_call UI.
	if cb.OnToolEvent != nil {
		clearSink := globalACPToolSinks.set(requestID, cb.OnToolEvent)
		defer clearSink()
	}

	handler := hubClient.ensureIMHandler()
	if handler == nil {
		return nil, fmt.Errorf("AI assistant handler not ready")
	}
	// ACP programming runs in a project-owned session. Apply the same guard as
	// the desktop binding: its browser/bridge transcript must never replenish a
	// project session with context captured from a different conversation.
	if shouldReconcileAIAssistantClientHistory(req, msg.UserID) {
		a.reconcileAIAssistantClientHistory(handler, msg.UserID, req.RecentMessages)
	}

	workDir := ""
	if projectPath != "" {
		workDir = a.EffectiveWorkingDirForOwner(userID)
	}
	log.Printf("[acp-mode-b] programming prompt request_id=%s user=%q project=%q work_dir=%q text_len=%d agent_text_len=%d",
		requestID, userID, projectPath, workDir, len(text), len(agentText))

	// ACP remains on the shared GUI agent brain, but a likely workspace-mutating
	// prompt is now explicitly bound to a durable Runtime task. We do not force
	// greetings, questions, or generic ACP chat into a write workflow: doing so
	// would change the established request classification and confirmation
	// semantics. Once admitted, cancellation and stale callbacks use the same
	// ledger behavior as GUI/TUI/MaClawSrv coding tasks.
	var resp *IMAgentResponse
	if mutatingRuntime {
		store := a.ensureCodingRuntimeStore()
		if store == nil {
			return nil, fmt.Errorf("coding execution ledger is unavailable")
		}
		policy := codingruntime.PolicySnapshot{
			ProjectRoot:                projectPath,
			Mode:                       "acp",
			FinalWorkspaceGateRequired: true,
		}
		policyDigest, digestErr := codingruntime.PolicyDigest(policy)
		if digestErr != nil {
			return nil, fmt.Errorf("freeze ACP coding runtime policy: %w", digestErr)
		}
		policy.Digest = policyDigest
		// Let the shared IM entry build and own its usual loop context. Giving it
		// a pre-created context bypasses the per-session serialization boundary,
		// which would break normal ACP confirmation and history semantics.
		executor := acpProgrammingRuntimeExecutor(func(runCtx context.Context, runtimeRequest codingruntime.ExecutionRequest) codingruntime.ExecutionResult {
			if runCtx != nil && runCtx.Err() != nil {
				return codingruntime.ExecutionResult{Status: codingruntime.TaskInterrupted, SideEffectState: codingruntime.SideEffectUncertain, ErrorCode: "acp_request_cancelled", ErrorSummary: "ACP coding request was cancelled; inspect workspace before continuation"}
			}
			resp = handler.HandleIMMessageWithProgressAndStream(msg, onProgress, onToken, onNewRound, onStreamDone)
			if runCtx != nil && runCtx.Err() != nil {
				return codingruntime.ExecutionResult{Status: codingruntime.TaskInterrupted, SideEffectState: codingruntime.SideEffectUncertain, ErrorCode: "acp_request_cancelled", ErrorSummary: "ACP coding request was cancelled; inspect workspace before continuation"}
			}
			if resp != nil && resp.Confirmation != nil {
				return codingruntime.ExecutionResult{Status: codingruntime.TaskBlocked, SideEffectState: codingruntime.SideEffectNone, ErrorCode: "acp_confirmation_required", ErrorSummary: "ACP coding request requires explicit confirmation before execution"}
			}
			if resp != nil && strings.TrimSpace(resp.Error) != "" {
				return codingruntime.ExecutionResult{Status: codingruntime.TaskFailed, SideEffectState: codingruntime.SideEffectUncertain, ErrorCode: "acp_agent_failed", ErrorSummary: "ACP coding agent failed; inspect host-local diagnostics"}
			}
			return codingruntime.ExecutionResult{Status: codingruntime.TaskCompleted, SideEffectState: codingruntime.SideEffectObserved, Evidence: []codingruntime.Evidence{{Type: "acp_agent_completion", Digest: codingRuntimeDigest(requestID)}}}
		})
		runner := codingruntime.Runner{
			Store:           store,
			LeaseOwner:      "gui:acp:" + userID,
			LeaseDuration:   15 * time.Minute,
			WorkspaceProber: codingruntime.NewLocalGitWorkspaceProber(projectPath),
		}
		task, attempt, runErr := runner.Run(ctx, codingruntime.Task{
			TaskID:        runtimeTaskID,
			WorkflowID:    "acp",
			PhaseID:       requestID,
			OwnerID:       "gui:acp:" + userID,
			ProjectRef:    projectPath,
			Mode:          "acp",
			RequestedWork: text,
			PolicyDigest:  policyDigest,
		}, policy, executor)
		if resp == nil {
			resp = &IMAgentResponse{}
		}
		if task != nil {
			resp.CodingRuntimeTaskID = task.TaskID
		}
		if attempt != nil {
			resp.CodingRuntimeAttemptID = attempt.AttemptID
		}
		if runErr != nil && !errors.Is(runErr, codingruntime.ErrStaleAttempt) {
			return resp, runErr
		}
	} else {
		resp = handler.HandleIMMessageWithProgressAndStream(msg, onProgress, onToken, onNewRound, onStreamDone)
	}
	if resp == nil {
		resp = &IMAgentResponse{}
	}
	resp.RequestID = requestID
	resp.SessionKey = userID
	normalizeArtifactResponseSource(resp)
	// Surface on-disk artifacts so VS Code users know what to open (files are
	// already under cwd when tools used the project owner working dir).
	if paths := collectACPResultPaths(resp); len(paths) > 0 {
		suffix := "\n\n[workspace files]\n- " + strings.Join(paths, "\n- ")
		if strings.TrimSpace(resp.Text) == "" {
			resp.Text = strings.TrimSpace(suffix)
		} else if !strings.Contains(resp.Text, paths[0]) {
			resp.Text = strings.TrimSpace(resp.Text) + suffix
		}
	}

	if ctx.Err() != nil {
		return resp, ctx.Err()
	}
	return resp, nil
}

func collectACPResultPaths(resp *IMAgentResponse) []string {
	if resp == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	add(resp.LocalFilePath)
	for _, p := range resp.LocalFilePaths {
		add(p)
	}
	if resp.FileName != "" && resp.LocalFilePath == "" {
		add(resp.FileName)
	}
	return out
}
