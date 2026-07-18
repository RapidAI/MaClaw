package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
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

	if projectPath != "" && a.isProjectTaskClosed(projectPath) {
		a.cancelProjectTaskLoop(projectPath)
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

	// Watch ctx for cancel → cancel AI assistant session for this project user.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			if _, err := a.CancelAIAssistantSessionForSession(userID); err != nil {
				log.Printf("[acp-mode-b] cancel session: %v", err)
			}
			if projectPath != "" {
				a.cancelProjectTaskLoop(projectPath)
			}
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
	a.reconcileAIAssistantClientHistory(handler, msg.UserID, req.RecentMessages)

	workDir := ""
	if projectPath != "" {
		workDir = a.EffectiveWorkingDirForOwner(userID)
	}
	log.Printf("[acp-mode-b] programming prompt request_id=%s user=%q project=%q work_dir=%q text_len=%d agent_text_len=%d",
		requestID, userID, projectPath, workDir, len(text), len(agentText))

	resp := handler.HandleIMMessageWithProgressAndStream(msg, onProgress, onToken, onNewRound, onStreamDone)
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
