package main

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const maxRecentImportantEvents = 5

type RemoteSessionManager struct {
	app               *App
	hubClient         *RemoteHubClient
	providerFactory   func(tool string) (ProviderAdapter, error)
	executionFactory  func(spec LaunchSpec) (ExecutionStrategy, error)
	workspacePreparer WorkspacePreparer
	pipelineFactory   func() *OutputPipeline

	stallDetector      *StallDetector
	completionAnalyzer *CompletionAnalyzer

	mu       sync.RWMutex
	sessions map[string]*RemoteSession
}

func NewRemoteSessionManager(app *App) *RemoteSessionManager {
	m := &RemoteSessionManager{
		app:      app,
		sessions: map[string]*RemoteSession{},
		executionFactory: func(spec LaunchSpec) (ExecutionStrategy, error) {
			return NewLocalPTYExecutionStrategy(nil), nil
		},
		workspacePreparer: NewDefaultWorkspacePreparer(),
		pipelineFactory: func() *OutputPipeline {
			return NewOutputPipeline()
		},
		providerFactory: func(tool string) (ProviderAdapter, error) {
			return app.remoteProviderAdapter(tool)
		},
	}

	m.stallDetector = NewStallDetector(StallDetectorConfig{}, app.log)
	m.completionAnalyzer = NewCompletionAnalyzer(CompletionAnalyzerConfig{})

	m.stallDetector.OnStallStateChanged = func(sessionID string, state StallState, nudgeCount int) {
		s, ok := m.Get(sessionID)
		if !ok {
			return
		}
		s.mu.Lock()
		s.StallState = state
		s.LastNudgeCount = nudgeCount
		switch state {
		case StallStateSuspected:
			s.Summary.SuggestedAction = "编程工具输出暂停，系统正在尝试恢复"
		case StallStateStuck:
			s.Summary.SuggestedAction = "编程工具可能已卡住，建议发送具体指令或终止会话"
		case StallStateNormal:
			s.Summary.SuggestedAction = ""
		}
		s.Summary.UpdatedAt = time.Now().Unix()
		snap := s.Summary
		s.mu.Unlock()
		if m.hubClient != nil {
			_ = m.hubClient.SendSessionSummary(snap)
		}
		m.app.emitRemoteStateChanged()
	}

	return m
}

func (m *RemoteSessionManager) SetHubClient(client *RemoteHubClient) {
	m.hubClient = client
}

// GetHubClient returns the current RemoteHubClient, if set.
func (m *RemoteSessionManager) GetHubClient() *RemoteHubClient {
	return m.hubClient
}

// executionStrategyForMode returns the correct ExecutionStrategy for the
// given provider execution mode. All current providers use SDK or headless
// protocols; the PTY mode constant is retained only for backward compat.
func executionStrategyForMode(mode ExecutionMode) ExecutionStrategy {
	switch mode {
	case ExecModeSDK:
		return NewSDKExecutionStrategy()
	case ExecModeCodexSDK:
		return NewCodexSDKExecutionStrategy()
	case ExecModeIFlowSDK:
		return NewIFlowSDKExecutionStrategy()
	case ExecModeOpenCodeSDK:
		return NewOpenCodeSDKExecutionStrategy()
	case ExecModeKiloSDK:
		return NewKiloSDKExecutionStrategy()
	case ExecModeGeminiACP:
		return NewGeminiACPExecutionStrategy()
	default:
		// Fallback: SDK mode is the most common protocol.
		return NewSDKExecutionStrategy()
	}
}

func (m *RemoteSessionManager) Create(spec LaunchSpec) (*RemoteSession, error) {
	now := time.Now()
	sessionID := fmt.Sprintf("sess_%d", now.UnixNano())
	originalProjectPath := spec.ProjectPath
	spec.SessionID = sessionID
	spec.LaunchSource = normalizeRemoteLaunchSource(spec.LaunchSource)

	workspace, err := m.workspacePreparer.Prepare(sessionID, spec)
	if err != nil {
		session := m.newFailedSession(sessionID, spec, nil, now, err)
		m.storeSession(session)
		m.syncFailedSession(session)
		return session, err
	}

	spec.ProjectPath = workspace.ProjectPath
	defer func() {
		if workspace != nil && workspace.Release != nil {
			workspace.Release()
		}
	}()

	provider, err := m.providerFactory(spec.Tool)
	if err != nil {
		session := m.newFailedSession(sessionID, spec, nil, now, err)
		m.storeSession(session)
		m.syncFailedSession(session)
		return session, err
	}

	// Backup tool config files before BuildCommand runs onboarding.
	// The restore function is stored on the session and called when
	// the session exits, so the user's native config is preserved.
	configRestore := backupToolConfigs(m.app, spec.Tool)

	// Ensure tool onboarding is complete (theme, trust, etc.) so the
	// tool doesn't block on first-run interactive prompts.  This must
	// run after backupToolConfigs (which snapshots the pre-onboarding
	// state) and before BuildCommand (which may rely on the config).
	ensureToolOnboardingComplete(m.app, spec.Tool, spec.ProjectPath)

	// Remote sessions (mobile/handoff) cannot show OS-level privilege
	// escalation dialogs (UAC on Windows, sudo on Unix). If AdminMode
	// is requested, check whether the current process already has
	// elevated privileges. If it does, the child process inherits them
	// automatically. If not, downgrade AdminMode and record a warning
	// so the user knows why admin was skipped.
	var adminDowngraded bool
	if spec.AdminMode && !isProcessElevated() && isHeadlessLaunchSource(spec.LaunchSource) {
		spec.AdminMode = false
		adminDowngraded = true
		if m.app != nil {
			m.app.log(fmt.Sprintf("[remote-admin] session=%s: AdminMode downgraded — process is not elevated and remote launch cannot show UAC prompt", sessionID))
		}
	}

	cmd, err := provider.BuildCommand(spec)
	if err != nil {
		configRestore() // restore immediately on failure
		session := m.newFailedSession(sessionID, spec, provider, now, err)
		m.storeSession(session)
		m.syncFailedSession(session)
		return session, err
	}

	// Choose execution strategy based on provider mode.
	// executionFactory can be overridden in tests to inject a fake strategy.
	// The default factory creates the correct strategy for the provider's mode.
	var strategy ExecutionStrategy
	strategy, err = m.executionFactory(spec)
	if err != nil {
		configRestore()
		session := m.newFailedSession(sessionID, spec, provider, now, err)
		m.storeSession(session)
		m.syncFailedSession(session)
		return session, err
	}
	// If the factory returned a nil-PTY placeholder (default), resolve the
	// real strategy from the provider's execution mode. When executionFactory
	// is overridden in tests it returns a fake strategy which is not
	// *LocalPTYExecutionStrategy, so we keep it as-is.
	if _, isPlaceholder := strategy.(*LocalPTYExecutionStrategy); isPlaceholder {
		strategy = executionStrategyForMode(provider.ExecutionMode())
	}

	execHandle, err := strategy.Start(cmd)
	if err != nil {
		configRestore()
		session := m.newFailedSession(sessionID, spec, provider, now, err)
		m.storeSession(session)
		m.syncFailedSession(session)
		return session, err
	}

	session := &RemoteSession{
		ID:             sessionID,
		Tool:           spec.Tool,
		Title:          spec.Title,
		LaunchSource:   spec.LaunchSource,
		ProjectPath:    originalProjectPath,
		WorkspacePath:  workspace.ProjectPath,
		WorkspaceRoot:  workspace.RootPath,
		WorkspaceMode:  workspace.Mode,
		WorkspaceIsGit: workspace.IsGitRepo,
		ModelID:        spec.ModelID,
		Status:         SessionStarting,
		PID:            execHandle.PID(),
		CreatedAt:      now,
		UpdatedAt:      now,
		Exec:           execHandle,
		Provider:       provider,
		Summary: SessionSummary{
			SessionID: sessionID,
			Tool:      spec.Tool,
			Title:     spec.Title,
			Source:    string(spec.LaunchSource),
			Status:    string(SessionStarting),
			Severity:  "info",
			UpdatedAt: now.Unix(),
		},
		Preview: SessionPreview{
			SessionID: sessionID,
			UpdatedAt: now.Unix(),
		},
		workspaceRelease: workspace.Release,
		configCleanup:    configRestore,
		LaunchFP:         LaunchFingerprint(spec),
	}

	// Initialize permission handler based on YoloMode setting.
	// Remote sessions (mobile/handoff) have no local confirmation dialog,
	// so auto-approve all permission requests to avoid blocking.
	permMode := PermissionModeDefault
	if spec.YoloMode || isHeadlessLaunchSource(spec.LaunchSource) {
		permMode = PermissionModeAutoApprove
	}
	session.Permissions = NewPermissionHandler(permMode, nil, nil)

	initEvent := buildSessionInitEvent(session)
	session.Events = []ImportantEvent{initEvent}

	// If admin mode was downgraded, add a warning event so the user
	// sees why the session is running without elevated privileges.
	var adminWarningEvent *ImportantEvent
	if adminDowngraded {
		evt := ImportantEvent{
			EventID:   fmt.Sprintf("evt_%d_admin_downgrade", now.UnixNano()),
			SessionID: sessionID,
			Type:      "admin_downgrade",
			Severity:  "warning",
			Title:     "Admin mode unavailable",
			Summary:   "Remote launch cannot show OS privilege dialog. Session started without admin privileges. Restart the application as administrator if admin mode is required.",
			CreatedAt: now.Unix(),
		}
		session.Events = append(session.Events, evt)
		adminWarningEvent = &evt
	}

	workspace = nil

	m.storeSession(session)

	if m.hubClient != nil {
		_ = m.hubClient.SendSessionCreated(session)
		_ = m.hubClient.SendImportantEvent(initEvent)
		if adminWarningEvent != nil {
			_ = m.hubClient.SendImportantEvent(*adminWarningEvent)
		}
	}

	// SDK sessions get a dedicated output loop that handles structured messages.
	// iFlow/OpenCode/Kilo emit pre-formatted text on Output(), so the generic
	// runOutputLoop (which reads from Output() and feeds the pipeline) works.
	// Gemini ACP emits pre-formatted text but also needs session state tracking.
	if _, isSDK := session.Exec.(*SDKExecutionHandle); isSDK {
		go m.runSDKOutputLoop(session)
	} else if _, isCodex := session.Exec.(*CodexSDKExecutionHandle); isCodex {
		go m.runCodexSDKOutputLoop(session)
	} else if acpHandle, isACP := session.Exec.(*GeminiACPExecutionHandle); isACP {
		// Wire the session's permission handler into the ACP handle so
		// permission requests from Gemini CLI are routed through it.
		acpHandle.Permissions = session.Permissions
		go m.runGeminiACPOutputLoop(session)
	} else {
		go m.runOutputLoop(session)
	}
	go m.runExitLoop(session)

	return session, nil
}

func (m *RemoteSessionManager) newFailedSession(
	sessionID string,
	spec LaunchSpec,
	provider ProviderAdapter,
	now time.Time,
	createErr error,
) *RemoteSession {
	title := spec.Title
	if title == "" {
		title = filepath.Base(spec.ProjectPath)
	}
	if title == "" || title == "." || title == string(filepath.Separator) {
		title = remoteToolDisplayName(spec.Tool) + " Session"
	}

	message := createErr.Error()
	session := &RemoteSession{
		ID:           sessionID,
		Tool:         spec.Tool,
		Title:        title,
		LaunchSource: normalizeRemoteLaunchSource(spec.LaunchSource),
		ProjectPath:  spec.ProjectPath,
		ModelID:      spec.ModelID,
		Status:       SessionError,
		PID:          0,
		CreatedAt:    now,
		UpdatedAt:    now,
		Provider:     provider,
		Summary: SessionSummary{
			SessionID:       sessionID,
			Tool:            spec.Tool,
			Title:           title,
			Source:          string(normalizeRemoteLaunchSource(spec.LaunchSource)),
			Status:          string(SessionError),
			Severity:        "error",
			CurrentTask:     fmt.Sprintf("Starting %s session", remoteToolDisplayName(spec.Tool)),
			ProgressSummary: fmt.Sprintf("%s remote launch failed before the session became interactive", remoteToolDisplayName(spec.Tool)),
			LastResult:      message,
			SuggestedAction: "Review the launch diagnostics and try again",
			UpdatedAt:       now.Unix(),
		},
		Preview: SessionPreview{
			SessionID:    sessionID,
			OutputSeq:    1,
			PreviewLines: []string{"Launch failed: " + message},
			UpdatedAt:    now.Unix(),
		},
	}
	session.Events = []ImportantEvent{buildSessionFailedEvent(session, createErr)}
	return session
}

func (m *RemoteSessionManager) storeSession(session *RemoteSession) {
	m.mu.Lock()
	m.sessions[session.ID] = session
	m.mu.Unlock()

	m.app.refreshPowerOptimizationState()
	m.app.emitRemoteStateChanged()
}

func (m *RemoteSessionManager) syncFailedSession(session *RemoteSession) {
	if m.hubClient == nil {
		return
	}
	_ = m.hubClient.SendSessionCreated(session)
	for _, event := range session.Events {
		_ = m.hubClient.SendImportantEvent(event)
	}
	_ = m.hubClient.SendSessionSummary(session.Summary)
	_ = m.hubClient.SendPreviewDelta(SessionPreviewDelta{
		SessionID:   session.ID,
		OutputSeq:   session.Preview.OutputSeq,
		AppendLines: append([]string{}, session.Preview.PreviewLines...),
		UpdatedAt:   session.Preview.UpdatedAt,
	})
}

func (m *RemoteSessionManager) Get(sessionID string) (*RemoteSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[sessionID]
	return s, ok
}

func (m *RemoteSessionManager) List() []*RemoteSession {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]*RemoteSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s)
	}
	return out
}

func (m *RemoteSessionManager) HasActiveSessions() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, s := range m.sessions {
		if s == nil {
			continue
		}
		s.mu.RLock()
		active := isActiveRemoteSessionStatus(s.Status)
		s.mu.RUnlock()
		if active {
			return true
		}
	}
	return false
}

func (m *RemoteSessionManager) WriteInput(sessionID, text string) error {
	s, ok := m.Get(sessionID)
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	if s.Exec == nil {
		return fmt.Errorf("session execution not available: %s", sessionID)
	}

	// SDK handles accept JSON messages — skip PTY line-ending normalization.
	if _, isSDK := s.Exec.(*SDKExecutionHandle); isSDK {
		return m.writeSDKInput(s, sessionID, text, "sdk")
	}

	// Codex SDK sessions — write prompt text directly, echo to output.
	if _, isCodex := s.Exec.(*CodexSDKExecutionHandle); isCodex {
		return m.writeSDKInput(s, sessionID, text, "codex")
	}

	// Gemini ACP sessions — Write() handles echo internally via outputCh,
	// so we only need to skip PTY normalization and call Write directly.
	if _, isACP := s.Exec.(*GeminiACPExecutionHandle); isACP {
		m.app.log(fmt.Sprintf("[remote-write-gemini-acp] session=%s, len=%d, text=%q",
			sessionID, len(text), text))
		err := s.Exec.Write([]byte(text))
		if err != nil {
			m.app.log(fmt.Sprintf("[remote-write-gemini-acp] FAILED session=%s: %v", sessionID, err))
		}
		return err
	}

	// ConPTY on Windows requires "\r\n" (or "\r") to simulate pressing Enter.
	// A bare "\n" is treated as a literal linefeed and does NOT trigger command
	// execution.  Normalize all line endings to "\r\n" so that input from any
	// client (desktop, PWA, mobile) works correctly regardless of what line
	// ending the client sends.
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\n", "\r\n")
	m.app.log(fmt.Sprintf("[remote-write] session=%s, raw_len=%d, normalized_len=%d, normalized=%q, raw_output_count=%d",
		sessionID, len(text), len(normalized), normalized, len(s.RawOutputLines)))
	err := s.Exec.Write([]byte(normalized))
	if err != nil {
		m.app.log(fmt.Sprintf("[remote-write] FAILED session=%s: %v", sessionID, err))
	} else {
		m.app.log(fmt.Sprintf("[remote-write] OK session=%s", sessionID))
	}
	return err
}

// writeSDKInput writes text to an SDK-mode session (Claude or Codex) and
// echoes the user input into the raw output and preview for display.
func (m *RemoteSessionManager) writeSDKInput(s *RemoteSession, sessionID, text, tag string) error {
	m.app.log(fmt.Sprintf("[remote-write-%s] session=%s, len=%d, text=%q",
		tag, sessionID, len(text), text))
	err := s.Exec.Write([]byte(text))
	if err != nil {
		m.app.log(fmt.Sprintf("[remote-write-%s] FAILED session=%s: %v", tag, sessionID, err))
	}
	displayText := strings.TrimSpace(text)
	if displayText != "" {
		echoLine := fmt.Sprintf("❯ %s", displayText)
		s.mu.Lock()
		s.RawOutputLines = append(s.RawOutputLines, "", echoLine, "")
		s.Preview.PreviewLines = append(s.Preview.PreviewLines, "", echoLine, "")
		if len(s.RawOutputLines) > 2000 {
			s.RawOutputLines = s.RawOutputLines[len(s.RawOutputLines)-2000:]
		}
		if len(s.Preview.PreviewLines) > 500 {
			s.Preview.PreviewLines = s.Preview.PreviewLines[len(s.Preview.PreviewLines)-500:]
		}
		s.mu.Unlock()
		if m.hubClient != nil {
			_ = m.hubClient.SendPreviewDelta(SessionPreviewDelta{
				SessionID:   sessionID,
				OutputSeq:   s.Preview.OutputSeq,
				AppendLines: []string{"", echoLine, ""},
				UpdatedAt:   s.Preview.UpdatedAt,
			})
		}
	}
	return err
}

// WriteImageInput constructs a multi-part SDKUserInput containing an image
// content block and writes it to the SDK session's stdin. Only SDK-mode
// sessions support image input; PTY sessions return an error.
func (m *RemoteSessionManager) WriteImageInput(sessionID string, img ImageTransferMessage) error {
	s, ok := m.Get(sessionID)
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	if s.Exec == nil {
		return fmt.Errorf("session execution not available: %s", sessionID)
	}

	// Only SDK sessions support image input.
	sdkHandle, isSDK := s.Exec.(*SDKExecutionHandle)
	if !isSDK {
		return fmt.Errorf("Image transfer is only supported in SDK mode sessions")
	}

	// Validate the image message (media type, base64 data, size limit).
	if err := ValidateImageTransferMessage(img, ImageUploadSizeLimit); err != nil {
		return err
	}

	// Construct multi-part SDKUserInput with text + image content blocks.
	// The official Claude Code SDK requires a text part alongside the image
	// (see: docs.claude.com streaming input mode examples).
	msg := SDKUserInput{
		Type: "user",
		Message: SDKUserMessage{
			Role: "user",
			Content: []SDKUserContentPart{
				{
					Type: "text",
					Text: "[User uploaded an image]",
				},
				{
					Type: "image",
					Source: &SDKImageSource{
						Type:      "base64",
						MediaType: img.MediaType,
						Data:      img.Data,
					},
				},
			},
		},
	}

	m.app.log(fmt.Sprintf("[remote-write-image] session=%s, media_type=%s, b64_len=%d, content_parts=2(text+image)",
		sessionID, img.MediaType, len(img.Data)))

	if err := sdkHandle.WriteUserInput(msg); err != nil {
		m.app.log(fmt.Sprintf("[remote-write-image] FAILED session=%s: %v", sessionID, err))
		return err
	}

	// Echo image send into the raw output and preview so it appears in the terminal view.
	echoLine := fmt.Sprintf("❯ 📷 [Image: %s]", img.MediaType)
	s.mu.Lock()
	s.RawOutputLines = append(s.RawOutputLines, "", echoLine, "")
	s.Preview.PreviewLines = append(s.Preview.PreviewLines, "", echoLine, "")
	if len(s.RawOutputLines) > 2000 {
		s.RawOutputLines = s.RawOutputLines[len(s.RawOutputLines)-2000:]
	}
	if len(s.Preview.PreviewLines) > 500 {
		s.Preview.PreviewLines = s.Preview.PreviewLines[len(s.Preview.PreviewLines)-500:]
	}
	s.mu.Unlock()
	if m.hubClient != nil {
		_ = m.hubClient.SendPreviewDelta(SessionPreviewDelta{
			SessionID:   sessionID,
			OutputSeq:   s.Preview.OutputSeq,
			AppendLines: []string{"", echoLine, ""},
			UpdatedAt:   s.Preview.UpdatedAt,
		})
	}

	return nil
}

func (m *RemoteSessionManager) Interrupt(sessionID string) error {
	s, ok := m.Get(sessionID)
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	if s.Exec == nil {
		return fmt.Errorf("session execution not available: %s", sessionID)
	}
	return s.Exec.Interrupt()
}

func (m *RemoteSessionManager) Kill(sessionID string) error {
	s, ok := m.Get(sessionID)
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	if s.Exec == nil {
		return fmt.Errorf("session execution not available: %s", sessionID)
	}
	return s.Exec.Kill()
}

func (m *RemoteSessionManager) runOutputLoop(s *RemoteSession) {
	pipeline := m.pipelineFactory()
	responder := newStartupAutoResponder(m.app, s)

	output := sessionOutput(s)
	if output == nil {
		return
	}

	for chunk := range output {
		// Capture raw output (ANSI-stripped only, no filtering) for terminal view
		rawResult := rawChunkLines(chunk)
		rawLines := rawResult.Lines

		s.mu.Lock()
		if len(rawLines) > 0 {
			if rawResult.IsScreenRefresh && len(rawLines) >= 5 {
				// TUI screen redraw detected — replace the buffer so we
				// don't accumulate stale screen frames.
				// Guard: only replace when the new chunk has >= 5 lines,
				// avoiding spurious clears from stray cursor-home sequences.
				m.app.log(fmt.Sprintf("[remote-output] screen-refresh: session=%s, replacing %d lines with %d",
					s.ID, len(s.RawOutputLines), len(rawLines)))
				s.RawOutputLines = make([]string, len(rawLines))
				copy(s.RawOutputLines, rawLines)
			} else {
				s.RawOutputLines = append(s.RawOutputLines, rawLines...)
			}
			if len(s.RawOutputLines) > 2000 {
				s.RawOutputLines = s.RawOutputLines[len(s.RawOutputLines)-2000:]
			}
		}
		s.mu.Unlock()

		if len(rawLines) > 0 {
			m.app.log(fmt.Sprintf("[remote-output] session=%s, chunk_bytes=%d, new_raw_lines=%d",
				s.ID, len(chunk), len(rawLines)))
			// Check for startup prompts and auto-respond
			responder.feed(rawLines)
		}

		result := pipeline.Consume(s, chunk)

		s.mu.Lock()
		applyOutputResult(s, result)
		s.mu.Unlock()

		syncOutputResult(m.hubClient, result)

		m.app.refreshPowerOptimizationState()
		m.app.emitRemoteStateChanged()
	}
}

// runSDKOutputLoop handles output for SDK-mode sessions (Claude Code stream-json).
// It reads from the Output() channel for text preview and also processes
// structured SDK messages from Messages() for proper event generation.
func (m *RemoteSessionManager) runSDKOutputLoop(s *RemoteSession) {
	sdkHandle, ok := s.Exec.(*SDKExecutionHandle)
	if !ok {
		m.runOutputLoop(s)
		return
	}

	pipeline := m.pipelineFactory()
	output := sdkHandle.Output()
	messages := sdkHandle.Messages()
	ctrlReqs := sdkHandle.ControlRequests()

	sessionStarted := false

	// updateThinking transitions the session's thinking state and syncs
	// the summary to Hub when the state actually changes. Inspired by
	// happy-coder's fd3-based thinking tracker, but driven by SDK
	// message types instead of fetch interception.
	updateThinking := func(active bool) {
		s.mu.Lock()
		newState := ThinkingIdle
		if active {
			newState = ThinkingActive
		}
		if s.ThinkingState == newState {
			s.mu.Unlock()
			return
		}
		s.ThinkingState = newState
		s.ThinkingSince = time.Now()
		s.Summary.Thinking = active
		if active {
			s.Summary.ThinkingSince = s.ThinkingSince.UnixMilli()
		} else {
			s.Summary.ThinkingSince = 0
		}
		s.Summary.UpdatedAt = time.Now().Unix()
		snap := s.Summary
		s.mu.Unlock()

		if m.hubClient != nil {
			_ = m.hubClient.SendSessionSummary(snap)
		}
		m.app.emitRemoteStateChanged()
	}

	// eventCoalescer buffers tool_use events for a short window so that
	// fast tool calls (use + result within 300ms) are merged into a single
	// IM push instead of two separate messages.
	eventCoalescer := NewEventCoalescer(300*time.Millisecond, func(events []ImportantEvent) {
		for _, evt := range events {
			if m.hubClient != nil {
				_ = m.hubClient.SendImportantEvent(evt)
			}
		}
	})
	defer eventCoalescer.Close()

	// toolUseToEventID maps SDK tool_use block IDs to coalescer event IDs
	// so that incoming tool_result blocks can trigger CompleteToolCall.
	toolUseToEventID := make(map[string]string)

	// streamAccum accumulates streaming text_delta fragments into the
	// current line.  The in-progress text is kept as the last element of
	// RawOutputLines so the frontend always sees it.  When a newline
	// arrives the line is "committed" and a new empty accumulator starts.
	streamAccum := ""
	// streamAccumActive tracks whether the last element of RawOutputLines
	// is the in-progress accumulator (needs updating) vs a committed line.
	streamAccumActive := false

	// previewAccum accumulates streaming text fragments for the preview
	// pipeline. Only complete lines (terminated by \n) are sent to the
	// pipeline so that the PWA receives whole lines instead of tiny
	// fragments that get incorrectly joined with spaces.
	previewAccum := ""

	// appendStreamText must be called with s.mu held.
	appendStreamText := func(text string) {
		beforeCount := len(s.RawOutputLines)
		parts := strings.Split(text, "\n")
		for i, part := range parts {
			if i > 0 {
				streamAccum = ""
				streamAccumActive = false
			}
			streamAccum += part
			if streamAccum == "" && i > 0 {
				s.RawOutputLines = append(s.RawOutputLines, "")
				streamAccumActive = false
				continue
			}
			if streamAccumActive && len(s.RawOutputLines) > 0 {
				s.RawOutputLines[len(s.RawOutputLines)-1] = streamAccum
			} else if streamAccum != "" {
				s.RawOutputLines = append(s.RawOutputLines, streamAccum)
				streamAccumActive = true
			}
		}
		if len(s.RawOutputLines) > 2000 {
			s.RawOutputLines = s.RawOutputLines[len(s.RawOutputLines)-2000:]
		}
		afterCount := len(s.RawOutputLines)
		if afterCount < beforeCount {
			m.app.log(fmt.Sprintf("[sdk-stream-WARNING] session=%s raw_lines DECREASED: %d -> %d, text=%q",
				s.ID, beforeCount, afterCount, text))
		}
	}

	// flushStreamAccum must be called with s.mu held.
	flushStreamAccum := func() {
		if streamAccum != "" {
			streamAccum = ""
			streamAccumActive = false
		}
	}

	for {
		select {
		case chunk, ok := <-output:
			if !ok {
				output = nil
				s.mu.Lock()
				flushStreamAccum()
				s.mu.Unlock()
				// Flush any remaining preview accumulator
				if previewAccum != "" {
					result := pipeline.Consume(s, []byte(previewAccum))
					previewAccum = ""
					s.mu.Lock()
					applyOutputResult(s, result)
					s.mu.Unlock()
					syncOutputResult(m.hubClient, result)
				}

				// If the output channel closed without the session ever
				// reaching "running" state, the process likely crashed on
				// startup (missing API key, bad config, etc.).  Update the
				// summary so the user sees a clear diagnostic instead of a
				// generic "exit code 1" message.
				if !sessionStarted {
					s.mu.Lock()
					s.Summary.Severity = "error"
					s.Summary.CurrentTask = "SDK process exited without initializing"
					s.Summary.SuggestedAction = "Check tool installation, API key configuration, and network connectivity"
					s.Summary.UpdatedAt = time.Now().Unix()
					snap := s.Summary
					s.mu.Unlock()
					if m.hubClient != nil {
						_ = m.hubClient.SendSessionSummary(snap)
					}
					m.app.emitRemoteStateChanged()
				}

				if messages == nil {
					return
				}
				continue
			}

			text := string(chunk)

			// Accumulate text for RawOutputLines (desktop terminal)
			s.mu.Lock()
			appendStreamText(text)
			s.mu.Unlock()

			m.stallDetector.ResetTimer(s.ID, len(text) > 0)

			// Accumulate text for preview pipeline — only send complete
			// lines (containing \n) to avoid fragmenting words/characters
			// into separate preview lines that get joined with spaces.
			previewAccum += text
			if !strings.Contains(text, "\n") {
				// No complete line yet — skip pipeline processing.
				// Update timestamp and notify UI of raw line changes.
				s.mu.Lock()
				s.UpdatedAt = time.Now()
				s.mu.Unlock()
				m.app.emitRemoteStateChanged()
				continue
			}

			// We have at least one complete line. Send everything up to
			// the last newline to the pipeline; keep the remainder.
			lastNL := strings.LastIndex(previewAccum, "\n")
			toSend := previewAccum[:lastNL+1]
			previewAccum = previewAccum[lastNL+1:]

			result := pipeline.Consume(s, []byte(toSend))

			s.mu.Lock()
			applyOutputResult(s, result)
			s.mu.Unlock()

			syncOutputResult(m.hubClient, result)

			m.app.refreshPowerOptimizationState()
			m.app.emitRemoteStateChanged()

		case msg, ok := <-messages:
			if !ok {
				messages = nil
				if output == nil {
					return
				}
				continue
			}

			// Flush any pending preview accumulator on message boundaries
			// so the PWA sees complete text before status changes.
			if previewAccum != "" {
				pResult := pipeline.Consume(s, []byte(previewAccum))
				previewAccum = ""
				s.mu.Lock()
				applyOutputResult(s, pResult)
				s.mu.Unlock()
				syncOutputResult(m.hubClient, pResult)
			}

			now := time.Now()

			// Collect hub events to send after releasing the lock
			var summaryToSync *SessionSummary
			var eventsToSync []ImportantEvent
			var imagesToSync []ImageTransferMessage

			s.mu.Lock()
			s.UpdatedAt = now

			switch msg.Type {
			case "system":
				if msg.Subtype == "init" && !sessionStarted {
					sessionStarted = true
					s.Status = SessionRunning
					s.Summary.Status = string(SessionRunning)
					s.Summary.Severity = "info"
					s.Summary.CurrentTask = "Session initialized"
					s.Summary.UpdatedAt = now.Unix()
					snap := s.Summary
					summaryToSync = &snap
				}

			case "assistant":
				s.Status = SessionBusy
				s.Summary.Status = string(SessionBusy)
				s.Summary.UpdatedAt = now.Unix()
				flushStreamAccum()

				if msg.Message != nil {
					for _, block := range msg.Message.Content {
						if block.Type == "tool_use" && block.Name != "" {
							evt := buildSDKToolUseEvent(s, block)
							s.Events = appendRecentEvents(s.Events, evt, maxRecentImportantEvents)
							eventsToSync = append(eventsToSync, evt)
							// Track block.ID → EventID for coalescer completion
							if block.ID != "" {
								toolUseToEventID[block.ID] = evt.EventID
							}
						}
					}
					extracted := extractImagesFromBlocks(s.ID, msg.Message.Content, "sdk-image", m.app)
					imagesToSync = append(imagesToSync, extracted...)
					for _, img := range extracted {
						s.OutputImages = append(s.OutputImages, SessionOutputImage{
							ImageID:      img.ImageID,
							MediaType:    img.MediaType,
							Data:         img.Data,
							AfterLineIdx: len(s.RawOutputLines) - 1,
						})
					}
				}
				snap := s.Summary
				summaryToSync = &snap

			case "user":
				// Extract images from tool_result content blocks (e.g. screenshots
				// captured by Claude Code's Bash/Read tools).
				if msg.Message != nil {
					extracted := extractImagesFromBlocks(s.ID, msg.Message.Content, "sdk-image-user", m.app)
					imagesToSync = append(imagesToSync, extracted...)
					for _, img := range extracted {
						s.OutputImages = append(s.OutputImages, SessionOutputImage{
							ImageID:      img.ImageID,
							MediaType:    img.MediaType,
							Data:         img.Data,
							AfterLineIdx: len(s.RawOutputLines) - 1,
						})
					}
					// Notify coalescer that tool calls completed, enabling
					// merged "tool X ✓" events for fast tool calls.
					for _, block := range msg.Message.Content {
						if block.Type == "tool_result" && block.ToolUseID != "" {
							if eid, ok := toolUseToEventID[block.ToolUseID]; ok {
								eventCoalescer.CompleteToolCall(eid)
								delete(toolUseToEventID, block.ToolUseID)
							}
						}
					}
				}

			case "result":
				flushStreamAccum()
				s.Status = SessionWaitingInput
				s.Summary.Status = string(SessionWaitingInput)
				s.Summary.WaitingForUser = true
				// Clear thinking state inline so the snapshot is consistent.
				s.ThinkingState = ThinkingIdle
				s.ThinkingSince = time.Time{}
				s.Summary.Thinking = false
				s.Summary.ThinkingSince = 0
				s.Summary.UpdatedAt = now.Unix()
				if msg.Result != nil {
					s.Summary.ProgressSummary = fmt.Sprintf("Completed in %.1fs, %d turns", msg.Result.Duration/1000, msg.Result.NumTurns)
				}
				// Analyze completion level (pure function, safe under s.mu)
				level := m.completionAnalyzer.Analyze(s.RawOutputLines, s.Tool, msg.Result)
				s.CompletionLevel = level
				snap := s.Summary
				summaryToSync = &snap

				// Clear toolUseToEventID on result to prevent unbounded
				// growth when tool_use blocks have no matching tool_result
				// (e.g. interrupted turns).
				toolUseToEventID = make(map[string]string)
			}
			s.mu.Unlock()

			if summaryToSync != nil && m.hubClient != nil {
				_ = m.hubClient.SendSessionSummary(*summaryToSync)
			}
			for _, evt := range eventsToSync {
				eventCoalescer.Enqueue(evt)
			}
			for _, img := range imagesToSync {
				if m.hubClient != nil {
					_ = m.hubClient.SendSessionImage(img)
				}
			}

			// Stall detector integration (outside s.mu lock — StallDetector has its own lock)
			switch msg.Type {
			case "assistant":
				updateThinking(true)
				m.stallDetector.StartMonitoring(s.ID, s.Exec, s.Tool)
			case "result":
				// Thinking state already cleared inline under s.mu above;
				// updateThinking is a no-op here but kept for symmetry.
				m.stallDetector.StopMonitoring(s.ID)
			}

			m.app.refreshPowerOptimizationState()
			m.app.emitRemoteStateChanged()

		case req, ok := <-ctrlReqs:
			if !ok {
				ctrlReqs = nil
				continue
			}

			// Use the session's permission handler to decide approval.
			permReq := PermissionRequest{
				RequestID: req.RequestID,
				SessionID: s.ID,
				ToolName:  req.Request.ToolName,
				Input:     req.Request.Input,
				CreatedAt: time.Now(),
			}
			comp := s.Permissions.HandleRequest(permReq)

			approved := comp.Decision == PermissionApproved || comp.Decision == PermissionApprovedForSession
			m.app.log(fmt.Sprintf("[sdk-control] session=%s, request_id=%s, tool=%s — decision=%s",
				s.ID, req.RequestID, req.Request.ToolName, comp.Decision))
			_ = sdkHandle.RespondToControlRequest(req.RequestID, approved, req.Request.Input)

			s.mu.Lock()
			s.UpdatedAt = time.Now()
			s.mu.Unlock()
			m.app.emitRemoteStateChanged()
		}
	}
}


// runCodexSDKOutputLoop handles output for Codex SDK-mode sessions.
// Codex exec --json emits complete JSONL lines (not streaming fragments),
// so we don't need the streaming accumulator used by Claude's SDK loop.
func (m *RemoteSessionManager) runCodexSDKOutputLoop(s *RemoteSession) {
	codexHandle, ok := s.Exec.(*CodexSDKExecutionHandle)
	if !ok {
		m.runOutputLoop(s)
		return
	}

	pipeline := m.pipelineFactory()
	output := codexHandle.Output()
	sessionStarted := false
	gotRealOutput := false

	for chunk := range output {
		text := string(chunk)

		// Mark session as running on first output
		if !sessionStarted {
			sessionStarted = true
			s.mu.Lock()
			s.Status = SessionRunning
			s.Summary.Status = string(SessionRunning)
			s.Summary.Severity = "info"
			s.Summary.CurrentTask = "Codex session started"
			s.Summary.UpdatedAt = time.Now().Unix()
			snap := s.Summary
			s.mu.Unlock()
			if m.hubClient != nil {
				_ = m.hubClient.SendSessionSummary(snap)
			}
		}

		// Track whether we got any real (non-diagnostic) output from codex.
		if !strings.HasPrefix(text, "[codex-") {
			gotRealOutput = true
		}

		// Codex emits complete lines — split and append directly.
		lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
		s.mu.Lock()
		appendRawOutputLines(s, lines)
		s.mu.Unlock()

		result := pipeline.Consume(s, chunk)

		s.mu.Lock()
		applyOutputResult(s, result)
		s.mu.Unlock()

		syncOutputResult(m.hubClient, result)

		m.app.refreshPowerOptimizationState()
		m.app.emitRemoteStateChanged()
	}

	// If the output channel closed without any real codex output, the process
	// likely crashed on startup.  Update the summary so the user sees the issue.
	if !gotRealOutput {
		s.mu.Lock()
		s.Summary.Severity = "error"
		s.Summary.CurrentTask = "Codex process exited without producing output"
		s.Summary.SuggestedAction = "Check codex installation and API key configuration"
		s.Summary.UpdatedAt = time.Now().Unix()
		snap := s.Summary
		s.mu.Unlock()
		if m.hubClient != nil {
			_ = m.hubClient.SendSessionSummary(snap)
		}
		m.app.emitRemoteStateChanged()
	}

	// `codex exec` is one-shot — the process exits after the output channel
	// closes.  The exit loop (runExitLoop) handles the final status transition,
	// so we don't set SessionWaitingInput here.
}


// runGeminiACPOutputLoop handles output for Gemini ACP sessions.
// Gemini ACP emits pre-formatted text on Output() (no ANSI), so the
// pipeline works like the generic loop.  Additionally, this loop tracks
// session state transitions based on ACP-specific markers emitted by
// the GeminiACPExecutionHandle.
func (m *RemoteSessionManager) runGeminiACPOutputLoop(s *RemoteSession) {
	acpHandle, ok := s.Exec.(*GeminiACPExecutionHandle)
	if !ok {
		m.runOutputLoop(s)
		return
	}

	pipeline := m.pipelineFactory()
	output := acpHandle.Output()
	sessionStarted := false

	for chunk := range output {
		text := string(chunk)

		// Mark session as running on first output
		if !sessionStarted {
			sessionStarted = true
			s.mu.Lock()
			s.Status = SessionRunning
			s.Summary.Status = string(SessionRunning)
			s.Summary.Severity = "info"
			s.Summary.CurrentTask = "Gemini ACP session started"
			s.Summary.UpdatedAt = time.Now().Unix()
			snap := s.Summary
			s.mu.Unlock()
			if m.hubClient != nil {
				_ = m.hubClient.SendSessionSummary(snap)
			}
		}

		// Detect state transitions from ACP markers.
		trimmedText := strings.TrimSpace(text)
		if strings.HasPrefix(trimmedText, "❯ ") {
			// User input echo — session is now busy processing
			s.mu.Lock()
			s.Status = SessionBusy
			s.Summary.Status = string(SessionBusy)
			s.Summary.WaitingForUser = false
			s.Summary.UpdatedAt = time.Now().Unix()
			snap := s.Summary
			s.mu.Unlock()
			if m.hubClient != nil {
				_ = m.hubClient.SendSessionSummary(snap)
			}
			m.stallDetector.StartMonitoring(s.ID, s.Exec, s.Tool)
		} else if strings.HasPrefix(trimmedText, "[gemini-acp] turn complete:") {
			// Prompt completed — session is waiting for next input
			s.mu.Lock()
			s.Status = SessionWaitingInput
			s.Summary.Status = string(SessionWaitingInput)
			s.Summary.WaitingForUser = true
			s.Summary.UpdatedAt = time.Now().Unix()
			// Analyze completion level (pure function, safe under s.mu)
			level := m.completionAnalyzer.Analyze(s.RawOutputLines, s.Tool, nil)
			s.CompletionLevel = level
			snap := s.Summary
			s.mu.Unlock()
			if m.hubClient != nil {
				_ = m.hubClient.SendSessionSummary(snap)
			}
			m.stallDetector.StopMonitoring(s.ID)
		} else if strings.HasPrefix(trimmedText, "[gemini-acp] prompt error:") {
			// Prompt failed — session is waiting for next input
			s.mu.Lock()
			s.Status = SessionWaitingInput
			s.Summary.Status = string(SessionWaitingInput)
			s.Summary.WaitingForUser = true
			s.Summary.Severity = "warn"
			s.Summary.LastResult = trimmedText
			s.Summary.UpdatedAt = time.Now().Unix()
			snap := s.Summary
			s.mu.Unlock()
			if m.hubClient != nil {
				_ = m.hubClient.SendSessionSummary(snap)
			}
		} else if strings.HasPrefix(trimmedText, "[gemini-acp] session error:") {
			// Session-level error from Gemini
			s.mu.Lock()
			s.Summary.Severity = "warn"
			s.Summary.LastResult = trimmedText
			s.Summary.UpdatedAt = time.Now().Unix()
			snap := s.Summary
			s.mu.Unlock()
			if m.hubClient != nil {
				_ = m.hubClient.SendSessionSummary(snap)
			}
		}

		// Append to raw output lines
		lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
		s.mu.Lock()
		appendRawOutputLines(s, lines)
		s.mu.Unlock()

		m.stallDetector.ResetTimer(s.ID, true)

		result := pipeline.Consume(s, chunk)

		s.mu.Lock()
		applyOutputResult(s, result)
		s.mu.Unlock()

		syncOutputResult(m.hubClient, result)

		m.app.refreshPowerOptimizationState()
		m.app.emitRemoteStateChanged()
	}
}


func appendRecentEvents(events []ImportantEvent, event ImportantEvent, limit int) []ImportantEvent {
	if event.EventID == "" && event.Type == "" && event.Summary == "" && event.Title == "" {
		return events
	}

	// Use explicit copy to avoid slice aliasing when trimming
	out := make([]ImportantEvent, len(events), len(events)+1)
	copy(out, events)
	out = append(out, event)
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

func (m *RemoteSessionManager) runExitLoop(s *RemoteSession) {
	// Ensure config cleanup runs even if the exit channel is nil or closed
	// unexpectedly, so the user's native tool config is always restored.
	defer func() {
		m.stallDetector.StopMonitoring(s.ID)
		if s.configCleanup != nil {
			s.configCleanup()
			s.configCleanup = nil
		}
		// Reset permission handler to abort any pending requests.
		if s.Permissions != nil {
			s.Permissions.Reset()
		}
	}()

	exitCh := sessionExit(s)
	if exitCh == nil {
		return
	}

	exit, ok := <-exitCh
	if !ok {
		return
	}
	now := time.Now()

	s.mu.Lock()
	s.UpdatedAt = now
	if exit.Code != nil {
		s.ExitCode = exit.Code
	}
	if exit.Err != nil {
		s.Status = SessionError
	} else {
		s.Status = SessionExited
	}
	s.Summary.Status = string(s.Status)
	s.Summary.UpdatedAt = now.Unix()
	s.Summary.WaitingForUser = false

	// When the session exits very quickly (within 10 seconds of creation),
	// it usually means the tool binary failed to start properly (bad config,
	// missing dependency, auth error, etc.).  Capture the last few lines of
	// output so the error reason is visible in the summary and relayed to
	// the IM user, who otherwise only sees a generic "exit code 1" message.
	quickExit := now.Sub(s.CreatedAt) < 10*time.Second
	var stderrHint string
	if quickExit && len(s.RawOutputLines) > 0 {
		tail := s.RawOutputLines
		if len(tail) > 5 {
			tail = tail[len(tail)-5:]
		}
		var meaningful []string
		for _, line := range tail {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				meaningful = append(meaningful, trimmed)
			}
		}
		if len(meaningful) > 0 {
			stderrHint = strings.Join(meaningful, "; ")
			// Cap at 200 chars to keep the summary concise.
			if len(stderrHint) > 200 {
				stderrHint = stderrHint[:200] + "..."
			}
		}
	}

	if exit.Err != nil {
		s.Summary.Severity = "error"
		s.Summary.LastResult = exit.Err.Error()
		if stderrHint != "" {
			s.Summary.LastResult = stderrHint
		}
		s.Summary.ProgressSummary = "Session terminated with an execution error"
		s.Summary.SuggestedAction = "Review the error output and retry"
	} else {
		s.Summary.Severity = "info"
		if exit.Code != nil {
			s.Summary.LastResult = fmt.Sprintf("Session exited with code %d", *exit.Code)
			if *exit.Code != 0 {
				s.Summary.Severity = "warn"
				if stderrHint != "" {
					s.Summary.LastResult += " — " + stderrHint
				}
				s.Summary.SuggestedAction = "Check tool installation and configuration, then retry"
			}
		} else {
			s.Summary.LastResult = "Session exited"
		}
		if s.Summary.SuggestedAction == "" {
			s.Summary.ProgressSummary = "Session is no longer running"
			s.Summary.SuggestedAction = "Start a new session when ready"
		}
	}
	closedEvent := buildSessionClosedEvent(s, exit)
	s.Events = appendRecentEvents(s.Events, closedEvent, maxRecentImportantEvents)
	summarySnap := s.Summary
	exitStatus := s.Status
	var exitCodeVal *int
	if s.ExitCode != nil {
		cp := *s.ExitCode
		exitCodeVal = &cp
	}
	s.mu.Unlock()

	if m.hubClient != nil {
		_ = m.hubClient.SendSessionSummary(summarySnap)
		_ = m.hubClient.SendImportantEvent(closedEvent)
		_ = m.hubClient.SendSessionClosed(s)
	}
	// Trigger experience extraction for successfully completed sessions
	// (exited with code 0). Failed sessions are poor candidates for
	// reusable patterns.
	if exitStatus == SessionExited && exitCodeVal != nil && *exitCodeVal == 0 && m.app.experienceExtractor != nil {
		go func() {
			_ = m.app.experienceExtractor.Extract(s)
		}()
	}
	// Save session checkpoint to memory store so the next session on the
	// same project can resume where this one left off.
	if m.app.sessionCheckpointer != nil {
		go func() {
			_ = m.app.sessionCheckpointer.SaveCheckpoint(s)
		}()
	}
	if s.workspaceRelease != nil {
		s.workspaceRelease()
		s.workspaceRelease = nil
	}
	m.app.refreshPowerOptimizationState()
	m.app.emitRemoteStateChanged()
}

func isActiveRemoteSessionStatus(status SessionStatus) bool {
	switch status {
	case SessionStarting, SessionRunning, SessionBusy, SessionWaitingInput:
		return true
	default:
		return false
	}
}

func sessionOutput(session *RemoteSession) <-chan []byte {
	if session == nil || session.Exec == nil {
		return nil
	}
	return session.Exec.Output()
}

func sessionExit(session *RemoteSession) <-chan PTYExit {
	if session == nil || session.Exec == nil {
		return nil
	}
	return session.Exec.Exit()
}

// extractImagesFromBlocks collects image transfer messages from a slice of
// SDK content blocks. It handles both direct image blocks (type="image") and
// nested content arrays inside tool_result blocks (e.g. when Claude Code's
// Read tool returns a PNG file as an image content block).
func extractImagesFromBlocks(sessionID string, blocks []SDKContentBlock, logPrefix string, app *App) []ImageTransferMessage {
	var images []ImageTransferMessage
	for _, block := range blocks {
		// Direct image block
		if block.Type == "image" && block.Source != nil {
			if img, ok := validateAndBuildImage(sessionID, block.Source, logPrefix, app); ok {
				images = append(images, img)
			}
		}
		// tool_result with nested content array (e.g. Read tool returning images)
		if block.Type == "tool_result" && len(block.NestedContent) > 0 {
			for _, nested := range block.NestedContent {
				if nested.Type == "image" && nested.Source != nil {
					if img, ok := validateAndBuildImage(sessionID, nested.Source, logPrefix+"-nested", app); ok {
						images = append(images, img)
					}
				}
			}
		}
	}
	return images
}

func validateAndBuildImage(sessionID string, source *SDKImageSource, logPrefix string, app *App) (ImageTransferMessage, bool) {
	if !IsValidImageMediaType(source.MediaType) {
		if app != nil {
			app.log(fmt.Sprintf("[%s] session=%s: skipping image with unsupported media_type %q", logPrefix, sessionID, source.MediaType))
		}
		return ImageTransferMessage{}, false
	}
	decoded, err := base64.StdEncoding.DecodeString(source.Data)
	if err != nil {
		if app != nil {
			app.log(fmt.Sprintf("[%s] session=%s: skipping image with invalid base64 data: %v", logPrefix, sessionID, err))
		}
		return ImageTransferMessage{}, false
	}
	if len(decoded) > ImageOutputSizeLimit {
		// Attempt to downsize PNG images instead of dropping them.
		if source.MediaType == "image/png" {
			downsized, dsErr := downsizeScreenshotBase64(source.Data, ImageOutputSizeLimit)
			if dsErr == nil {
				if app != nil {
					app.log(fmt.Sprintf("[%s] session=%s: downsized image from %d to fit limit", logPrefix, sessionID, len(decoded)))
				}
				return NewImageTransferMessage(sessionID, source.MediaType, downsized), true
			}
			if app != nil {
				app.log(fmt.Sprintf("[%s] session=%s: downsize failed: %v, skipping", logPrefix, sessionID, dsErr))
			}
		} else if app != nil {
			app.log(fmt.Sprintf("[%s] session=%s: skipping non-PNG image exceeding size limit (%d > %d)", logPrefix, sessionID, len(decoded), ImageOutputSizeLimit))
		}
		return ImageTransferMessage{}, false
	}
	if app != nil {
		app.log(fmt.Sprintf("[%s] session=%s: extracted image, media_type=%s, size=%d", logPrefix, sessionID, source.MediaType, len(decoded)))
	}
	return NewImageTransferMessage(sessionID, source.MediaType, source.Data), true
}

// buildSDKToolUseEvent creates an ImportantEvent from an SDK tool_use content block.
func buildSDKToolUseEvent(s *RemoteSession, block SDKContentBlock) ImportantEvent {
	now := time.Now()
	eventType := "tool.use"
	title := fmt.Sprintf("Tool: %s", block.Name)
	summary := title

	// Map well-known tool names to file/command events
	switch block.Name {
	case "Read", "ReadFile", "View":
		eventType = "file.read"
		if input, ok := block.Input.(map[string]interface{}); ok {
			if file, ok := input["file_path"].(string); ok {
				title = fmt.Sprintf("Read %s", filepath.Base(file))
				summary = fmt.Sprintf("Inspected %s", file)
			}
		}
	case "Write", "WriteFile", "Edit", "MultiEdit":
		eventType = "file.change"
		if input, ok := block.Input.(map[string]interface{}); ok {
			if file, ok := input["file_path"].(string); ok {
				title = fmt.Sprintf("Edited %s", filepath.Base(file))
				summary = fmt.Sprintf("Modified %s", file)
			}
		}
	case "Bash", "Execute":
		eventType = "command.started"
		if input, ok := block.Input.(map[string]interface{}); ok {
			if cmd, ok := input["command"].(string); ok {
				title = fmt.Sprintf("Running: %s", cmd)
				summary = cmd
				if len(summary) > 120 {
					summary = summary[:120] + "..."
				}
			}
		}
	}

	return ImportantEvent{
		EventID:   fmt.Sprintf("sdk_%s_%d", block.ID, now.UnixNano()),
		SessionID: s.ID,
		Type:      eventType,
		Severity:  "info",
		Title:     title,
		Summary:   summary,
		CreatedAt: now.Unix(),
	}
}
