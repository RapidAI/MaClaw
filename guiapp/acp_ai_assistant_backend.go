package guiapp

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

// assistantPromptUsesMutatingRuntime decides whether this prompt opens the
// durable coding ledger. Explicit MCP chat is a conversation task, so the
// text heuristic must not turn it into a workspace write. ACP and coding
// tasks keep that heuristic.
func assistantPromptUsesMutatingRuntime(profile, taskMode, text, projectPath string) bool {
	if strings.TrimSpace(projectPath) == "" {
		return false
	}
	if assistantHostProfile(profile) == assistantHostMCP && strings.EqualFold(strings.TrimSpace(taskMode), "chat") {
		return false
	}
	return acpPromptMayMutateWorkspace(text)
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

const acpProgrammingWorkspacePrefix = "[VS Code / ACP programming workspace]"
const acpProgrammingUserRequestMarker = "User request:"

const (
	assistantHostACP = "acp"
	assistantHostMCP = "mcp"
)

// assistantHostProfile keeps every existing caller on the ACP path.
// Only an explicit mcp profile selects the inbound MCP host.
func assistantHostProfile(v string) string {
	if strings.EqualFold(strings.TrimSpace(v), assistantHostMCP) {
		return assistantHostMCP
	}
	return assistantHostACP
}

// mcpProgrammingRuntimeTaskID is the durable ledger identity for one MCP
// coding prompt. It is separate from the ACP hash so the two hosts cannot
// cancel each other's tasks.
func mcpProgrammingRuntimeTaskID(ownerID, workspace, requestID string) string {
	sum := sha256.Sum256([]byte("mcp-programming-runtime-v1\n" + strings.TrimSpace(ownerID) + "\n" + normalizeProjectSessionPath(workspace) + "\n" + strings.TrimSpace(requestID)))
	return fmt.Sprintf("mcp-coding-%x", sum[:16])
}

// programmingHostPolicy is the wrapper ledger for one ACP or MCP prompt.
// ACP keeps the git workspace gate because its project path is the editor
// folder. MCP does not: a missing repository must not skip the turn before
// the coding workbench can admit the real writer.
func programmingHostPolicy(hostMCP bool, workspace, mode string) codingruntime.PolicySnapshot {
	return codingruntime.PolicySnapshot{
		ProjectRoot:                workspace,
		Mode:                       mode,
		FinalWorkspaceGateRequired: !hostMCP,
	}
}

// programmingHostProber probes only when the wrapper gate will read the
// result. Runner still runs the probe before and after the executor. MCP
// leaves the gate off, so a git status there only delays the turn.
func programmingHostProber(policy codingruntime.PolicySnapshot, workspace string) codingruntime.WorkspaceProber {
	if !policy.FinalWorkspaceGateRequired {
		return nil
	}
	return codingruntime.NewLocalGitWorkspaceProber(workspace)
}

// mcpPromptWorkspace is the directory MCP tells the model to edit and the
// directory the coding ledger watches. toolDir is the cwd tools will use.
// An empty tool dir keeps the task record path.
func mcpPromptWorkspace(taskPath, toolDir string) string {
	if dir := normalizeProjectSessionPath(toolDir); dir != "" {
		return dir
	}
	return normalizeProjectSessionPath(taskPath)
}

// mcpTaskUserText is the MCP preamble. It is not the VS Code workspace
// wrapper, so ACP continue-detection does not treat it as an editor prompt.
func mcpTaskUserText(workspace, userText string) string {
	userText = strings.TrimSpace(userText)
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return "[MCP task]\n\nUser request:\n" + userText
	}
	return fmt.Sprintf("[MCP task]\nworkspace: %s\n\nUser request:\n%s", workspace, userText)
}

// acpInnerUserRequest returns the User request body from acpProgrammingUserText.
// Bare-continue planning must see "继续", not the VS Code workspace wrapper.
func acpInnerUserRequest(userText string) string {
	trimmed := strings.TrimSpace(userText)
	if !strings.HasPrefix(trimmed, acpProgrammingWorkspacePrefix) {
		return trimmed
	}
	idx := strings.Index(trimmed, acpProgrammingUserRequestMarker)
	if idx < 0 {
		return trimmed
	}
	inner := strings.TrimSpace(trimmed[idx+len(acpProgrammingUserRequestMarker):])
	if inner == "" {
		return trimmed
	}
	return inner
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
	profile := assistantHostProfile(req.PromptProfile)
	hostMCP := profile == assistantHostMCP
	logTag := "[acp-mode-b]"
	slashSource := "acp-mode-b:"
	policyMode := "acp"
	leasePrefix := "gui:acp:"
	workflowID := "acp"
	if hostMCP {
		logTag = "[mcp-host]"
		slashSource = "mcp-host:"
		policyMode = "mcp"
		leasePrefix = "gui:mcp:"
		workflowID = "mcp"
	}
	userID := desktopAIAssistantUserIDForProjectPath(projectPath)
	if !hostMCP {
		if sessionOwnerID := strings.TrimSpace(req.SessionOwnerID); sessionOwnerID != "" {
			if !isACPAssistantSessionUserID(sessionOwnerID) {
				return nil, fmt.Errorf("invalid ACP session owner: %q", sessionOwnerID)
			}
			userID = sessionOwnerID
			a.bindACPAssistantSessionWorkingDir(userID, projectPath)
		}
	}

	if projectPath != "" && a.isProjectTaskClosed(projectPath) {
		a.stopAIAssistantOwnerRuntime(userID)
		return nil, fmt.Errorf("project task is closed: %s", projectPath)
	}

	// Passthrough slash commands (same as GUI). Match original text, not the
	// workspace-wrapped body (wrappers would break /slash detection).
	if resp, handled := a.TryHandlePassthroughSlashCommandWithSource(text, slashSource+userID); handled {
		return resp, nil
	}

	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return nil, fmt.Errorf("AI assistant backend is unavailable (hub/local handler not ready)")
	}

	requestID := strings.TrimSpace(req.RequestID)
	if requestID == "" {
		idPrefix := "acp-prog-"
		if hostMCP {
			idPrefix = "mcp-prog-"
		}
		requestID = fmt.Sprintf("%s%d", idPrefix, time.Now().UnixNano())
	}
	// Keep ordinary questions on the established shared-chat path. Only a
	// likely workspace mutation gets a durable writer task (and consequently a
	// cancellation record) so cancelling a simple question never creates a
	// needless Ledger database/task. Explicit MCP chat never takes that ledger.
	mutatingRuntime := assistantPromptUsesMutatingRuntime(profile, req.TaskMode, text, projectPath)

	msgLang := strings.TrimSpace(req.Lang)
	if msgLang == "" {
		msgLang = a.CurrentLanguage
	}
	// Same GUI IM path; tools resolve cwd via desktop-user:{projectPath}.
	agentText := acpProgrammingUserText(projectPath, text)
	if hostMCP {
		agentText = mcpTaskUserText(projectPath, text)
	}
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
		if hostMCP {
			runtimeTaskID = mcpProgrammingRuntimeTaskID(userID, projectPath, requestID)
		}
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			a.cancelACPProgrammingRuntimeTask(runtimeTaskID)
			if _, err := a.CancelAIAssistantSessionForSession(userID); err != nil {
				log.Printf("%s cancel session: %v", logTag, err)
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
	a.restoreProjectAgentSession(handler, msg.UserID, projectPath)
	if projectContinuationHistoryMissing(req, handler, msg.UserID, projectPath) {
		log.Printf("%s continuation blocked without task history request_id=%s user=%q project=%q", logTag, requestID, msg.UserID, projectPath)
		resp := projectContinuationHistoryMissingResponse(req, msg.UserID)
		resp.RequestID = requestID
		return resp, nil
	}
	// ACP programming runs in a project-owned session. Apply the same guard as
	// the desktop binding: its browser/bridge transcript must never replenish a
	// project session with context captured from a different conversation.
	if shouldReconcileAIAssistantClientHistory(req, msg.UserID) {
		a.reconcileAIAssistantClientHistory(handler, msg.UserID, req.RecentMessages)
	}

	// The task record path is the session identity. Tools for a managed task
	// execute in the tagged work root (or the task workspace/), not in the
	// folder that only holds task.md. MCP must name that same directory in
	// the prompt and in the coding ledger. ACP's project path is already the
	// editor workspace, so it stays unchanged.
	workspace := projectPath
	if hostMCP {
		workspace = mcpPromptWorkspace(projectPath, handler.projectTabWorkDirForOwner(userID))
		if workspace == "" {
			workspace = projectPath
		}
		if req.HostRawText || mcpCommandSkipsWorkspaceWrap(text) {
			msg.Text = text
			agentText = text
		} else {
			msg.Text = mcpTaskUserText(workspace, text)
			agentText = msg.Text
		}
	}
	workDir := ""
	if projectPath != "" {
		workDir = a.EffectiveWorkingDirForOwner(userID)
	}
	if hostMCP && workspace != "" {
		workDir = workspace
		a.mcpSetRunWorkspace(projectPath, requestID, workspace)
	}
	log.Printf("%s programming prompt request_id=%s user=%q project=%q work_dir=%q text_len=%d agent_text_len=%d",
		logTag, requestID, userID, projectPath, workDir, len(text), len(agentText))

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
		// ACP's project path is the editor folder, so the wrapper keeps the git
		// gate. MCP's tool directory often has no repository. The GUI coding
		// workbench is the writer: it creates a baseline only when an
		// implementation step is admitted. Requiring that gate here runs first
		// and, on a missing HEAD, never calls the executor. The host then
		// reported an empty success. Mode stays "mcp" so this wrapper does not
		// take the local write lock the workbench needs.
		policy := programmingHostPolicy(hostMCP, workspace, policyMode)
		policyDigest, digestErr := codingruntime.PolicyDigest(policy)
		if digestErr != nil {
			return nil, fmt.Errorf("freeze %s coding runtime policy: %w", strings.ToUpper(policyMode), digestErr)
		}
		policy.Digest = policyDigest
		// Let the shared IM entry build and own its usual loop context. Giving it
		// a pre-created context bypasses the per-session serialization boundary,
		// which would break normal ACP confirmation and history semantics.
		hostWord := strings.ToUpper(policyMode)
		errorCode := policyMode
		executorStarted := false
		executor := acpProgrammingRuntimeExecutor(func(runCtx context.Context, runtimeRequest codingruntime.ExecutionRequest) codingruntime.ExecutionResult {
			executorStarted = true
			if runCtx != nil && runCtx.Err() != nil {
				return codingruntime.ExecutionResult{Status: codingruntime.TaskInterrupted, SideEffectState: codingruntime.SideEffectUncertain, ErrorCode: errorCode + "_request_cancelled", ErrorSummary: hostWord + " coding request was cancelled; inspect workspace before continuation"}
			}
			resp = handler.HandleIMMessageWithProgressAndStream(msg, onProgress, onToken, onNewRound, onStreamDone)
			if runCtx != nil && runCtx.Err() != nil {
				return codingruntime.ExecutionResult{Status: codingruntime.TaskInterrupted, SideEffectState: codingruntime.SideEffectUncertain, ErrorCode: errorCode + "_request_cancelled", ErrorSummary: hostWord + " coding request was cancelled; inspect workspace before continuation"}
			}
			if resp != nil && resp.Confirmation != nil {
				return codingruntime.ExecutionResult{Status: codingruntime.TaskBlocked, SideEffectState: codingruntime.SideEffectNone, ErrorCode: errorCode + "_confirmation_required", ErrorSummary: hostWord + " coding request requires explicit confirmation before execution"}
			}
			if resp != nil && strings.TrimSpace(resp.Error) != "" {
				return codingruntime.ExecutionResult{Status: codingruntime.TaskFailed, SideEffectState: codingruntime.SideEffectUncertain, ErrorCode: errorCode + "_agent_failed", ErrorSummary: hostWord + " coding agent failed; inspect host-local diagnostics"}
			}
			return codingruntime.ExecutionResult{Status: codingruntime.TaskCompleted, SideEffectState: codingruntime.SideEffectObserved, Evidence: []codingruntime.Evidence{{Type: errorCode + "_agent_completion", Digest: codingRuntimeDigest(requestID)}}}
		})
		runner := codingruntime.Runner{
			Store:           store,
			LeaseOwner:      leasePrefix + userID,
			LeaseDuration:   15 * time.Minute,
			WorkspaceProber: programmingHostProber(policy, workspace),
		}
		// ProjectRef stays the task record. Coding-subagent admission compares
		// a child with the workbench attempt, whose project is already the
		// tool directory. This wrapper is not that parent. Pointing ProjectRef
		// at the tool directory would only relabel the probe.
		task, attempt, runErr := runner.Run(ctx, codingruntime.Task{
			TaskID:        runtimeTaskID,
			WorkflowID:    workflowID,
			PhaseID:       requestID,
			OwnerID:       leasePrefix + userID,
			ProjectRef:    projectPath,
			Mode:          policyMode,
			RequestedWork: text,
			PolicyDigest:  policyDigest,
		}, policy, executor)
		if hostMCP && !executorStarted && attempt != nil && attempt.Status == codingruntime.TaskBlocked {
			reason := strings.TrimSpace(attempt.ErrorSummary)
			if reason == "" {
				reason = "编程任务在开始前被工作区检查拦住"
			}
			if resp == nil {
				resp = &IMAgentResponse{}
			}
			resp.RequestID = requestID
			resp.SessionKey = userID
			if task != nil {
				resp.CodingRuntimeTaskID = task.TaskID
			}
			resp.CodingRuntimeAttemptID = attempt.AttemptID
			return resp, errors.New(reason)
		}
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
