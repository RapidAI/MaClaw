package main

import (
	"encoding/base64"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/configfile"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/security"
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
	progressTracker    *ProgressTracker

	// Second-layer Harness modules (lazily initialized via setters).
	contextInjector  *configfile.ContextInjector
	feedbackInjector *remote.FeedbackInjector
	failureLearner   *remote.FailureLearner
	harnessGate      *security.HarnessGate

	// Per-project FailureLearner cache (keyed by project path).
	failureLearners   map[string]*remote.FailureLearner
	failureLearnersMu sync.Mutex

	mu       sync.RWMutex
	sessions map[string]*RemoteSession
}

func NewRemoteSessionManager(app *App) *RemoteSessionManager {
	m := &RemoteSessionManager{
		app:             app,
		sessions:        map[string]*RemoteSession{},
		failureLearners: make(map[string]*remote.FailureLearner),
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
	m.progressTracker = NewProgressTracker()

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
			// Auto-degrade from busy to waiting_input so the user can send
			// new instructions or kill the session. Without this, the session
			// stays busy forever and blocks all interaction.
			if s.Status == SessionBusy {
				s.Status = SessionWaitingInput
				s.Summary.Status = string(SessionWaitingInput)
				s.Summary.WaitingForUser = true
			}
		case StallStateNormal:
			s.Summary.SuggestedAction = ""
		}
		s.Summary.UpdatedAt = time.Now().Unix()
		snap := s.Summary
		s.mu.Unlock()
		if m.hubClient != nil {
			_ = m.hubClient.SendSessionSummary(snap)
		}
		m.updateTraceFromSummary(s, snap)
		m.app.emitRemoteStateChanged()
	}

	return m
}

func (m *RemoteSessionManager) traceService() *AITraceService {
	if m == nil || m.app == nil {
		return nil
	}
	m.app.ensureAITrace()
	return m.app.aiTrace
}

func (m *RemoteSessionManager) attachTraceToSession(session *RemoteSession) {
	if session == nil || session.RunID != "" {
		return
	}
	traceSvc := m.traceService()
	if traceSvc == nil {
		return
	}
	title := session.Title
	if title == "" {
		title = remoteToolDisplayName(session.Tool)
	}
	job, run := traceSvc.StartJobRun(
		TraceJobKindRemoteSession,
		title,
		string(normalizeRemoteLaunchSource(session.LaunchSource)),
		"",
		session.ProjectPath,
	)
	session.JobID = job.JobID
	session.RunID = run.RunID
	traceSvc.SetRunSessionID(run.RunID, session.ID)
	m.updateTraceFromSummary(session, session.Summary)
	for _, evt := range session.Events {
		m.recordImportantEventTrace(session, evt)
	}
	if session.Status == SessionError {
		m.recordOutputEvidence(session, "error", session.Summary.LastResult, session.Preview.PreviewLines)
	}
}

func (m *RemoteSessionManager) updateTraceFromSummary(session *RemoteSession, summary SessionSummary) {
	if session == nil || session.RunID == "" {
		return
	}
	traceSvc := m.traceService()
	if traceSvc == nil {
		return
	}
	status := traceStatusFromSessionStatus(SessionStatus(summary.Status))
	summaryText := summary.ProgressSummary
	if summaryText == "" {
		summaryText = summary.CurrentTask
	}
	errText := ""
	if strings.EqualFold(summary.Severity, "error") || SessionStatus(summary.Status) == SessionError {
		errText = summary.LastResult
	}
	traceSvc.UpdateRun(session.RunID, status, summaryText, errText)
}

func (m *RemoteSessionManager) recordImportantEventTrace(session *RemoteSession, evt ImportantEvent) {
	if session == nil || session.RunID == "" {
		return
	}
	traceSvc := m.traceService()
	if traceSvc == nil {
		return
	}
	if m.app != nil && session.ProjectPath != "" {
		m.app.ensureContextBridge()
		if m.app.contextBridge != nil {
			m.app.contextBridge.ExtractFromEvents(session.ProjectPath, []ImportantEvent{evt})
		}
	}
	traceSvc.AppendEvent(session.RunID, TraceEvent{
		Kind:        evt.Type,
		Severity:    evt.Severity,
		Title:       firstNonEmptyTraceText(evt.Title, evt.Type),
		Summary:     evt.Summary,
		RelatedFile: evt.RelatedFile,
		Command:     evt.Command,
		CreatedAt:   evt.CreatedAt,
		ProjectPath: session.ProjectPath,
	})
	traceSvc.AppendEvidence(session.RunID, EvidenceRecord{
		SourceKind:     "remote_event",
		Category:       traceCategoryForImportantEvent(evt),
		Summary:        firstNonEmptyTraceText(evt.Summary, evt.Title, evt.Type),
		ContentSnippet: evt.Summary,
		RelatedFile:    evt.RelatedFile,
		Command:        evt.Command,
		CreatedAt:      evt.CreatedAt,
		ProjectPath:    session.ProjectPath,
	})
}

func (m *RemoteSessionManager) recordOutputEvidence(session *RemoteSession, category, summary string, lines []string) {
	if session == nil || session.RunID == "" {
		return
	}
	snippet := traceSnippetFromLines(lines, 4)
	if snippet == "" {
		return
	}
	traceSvc := m.traceService()
	if traceSvc == nil {
		return
	}
	if summary == "" {
		summary = "Remote output snippet"
	}
	traceSvc.AppendEvidence(session.RunID, EvidenceRecord{
		SourceKind:     "remote_output",
		Category:       category,
		Summary:        summary,
		ContentSnippet: snippet,
		CreatedAt:      traceNowMillis(),
		ProjectPath:    session.ProjectPath,
	})
}

func (m *RemoteSessionManager) syncTraceFromOutputResult(session *RemoteSession, result OutputResult) {
	if session == nil || session.RunID == "" {
		return
	}
	if result.Summary != nil {
		m.updateTraceFromSummary(session, *result.Summary)
	}
	for _, evt := range result.Events {
		m.recordImportantEventTrace(session, evt)
	}
	if result.PreviewDelta == nil {
		return
	}
	if len(result.Events) > 0 {
		m.recordOutputEvidence(session, "event", firstNonEmptyTraceText(result.SummaryText(), "Remote event output"), result.PreviewDelta.AppendLines)
		return
	}
	if result.Summary != nil && strings.EqualFold(result.Summary.Severity, "error") {
		m.recordOutputEvidence(session, "error", firstNonEmptyTraceText(result.Summary.LastResult, result.Summary.ProgressSummary, "Remote error output"), result.PreviewDelta.AppendLines)
	}
}

func traceCategoryForImportantEvent(evt ImportantEvent) string {
	typeLower := strings.ToLower(evt.Type)
	severityLower := strings.ToLower(evt.Severity)
	switch {
	case severityLower == "error" || strings.Contains(typeLower, "error") || strings.Contains(typeLower, "fail"):
		return "error"
	case strings.Contains(typeLower, "file") || evt.RelatedFile != "":
		return "file"
	case strings.Contains(typeLower, "command") || evt.Command != "":
		return "command"
	case strings.Contains(typeLower, "result") || strings.Contains(typeLower, "close") || strings.Contains(typeLower, "complete"):
		return "result"
	default:
		return "event"
	}
}

func traceSnippetFromLines(lines []string, limit int) string {
	if len(lines) == 0 {
		return ""
	}
	meaningful := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			meaningful = append(meaningful, trimmed)
		}
	}
	if len(meaningful) == 0 {
		return ""
	}
	if limit > 0 && len(meaningful) > limit {
		meaningful = meaningful[len(meaningful)-limit:]
	}
	return truncateTraceText(strings.Join(meaningful, "\n"), 600)
}

func firstNonEmptyTraceText(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (m *RemoteSessionManager) SetHubClient(client *RemoteHubClient) {
	m.hubClient = client
}

// GetHubClient returns the current RemoteHubClient, if set.
func (m *RemoteSessionManager) GetHubClient() *RemoteHubClient {
	return m.hubClient
}

// SetContextInjector configures the layered context injection module.
func (m *RemoteSessionManager) SetContextInjector(ci *configfile.ContextInjector) {
	m.contextInjector = ci
}

// SetFeedbackInjector configures the feedback injection module.
func (m *RemoteSessionManager) SetFeedbackInjector(fi *remote.FeedbackInjector) {
	m.feedbackInjector = fi
}

// SetFailureLearner configures the failure learning module.
func (m *RemoteSessionManager) SetFailureLearner(fl *remote.FailureLearner) {
	m.failureLearner = fl
}

// SetHarnessGate configures the output validation gate module.
func (m *RemoteSessionManager) SetHarnessGate(hg *security.HarnessGate) { m.harnessGate = hg }

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
		ModelName:      spec.ModelName,
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
		workspaceRelease:   workspace.Release,
		configCleanup:      configRestore,
		LaunchFP:           LaunchFingerprint(spec),
		InjectResumePrompt: spec.InjectResumePrompt,
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

	m.attachTraceToSession(session)
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
		ModelName:    spec.ModelName,
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
	m.attachTraceToSession(session)
	return session
}

func (m *RemoteSessionManager) storeSession(session *RemoteSession) {
	m.mu.Lock()
	m.sessions[session.ID] = session
	m.mu.Unlock()

	m.app.refreshPowerOptimizationState()
	m.app.emitRemoteStateChanged()
}

func (m *RemoteSessionManager) CreateAIBackgroundSession(title, projectPath string, loopCtx *LoopContext) *RemoteSession {
	now := time.Now()
	sessionID := fmt.Sprintf("ai_bg_%d", now.UnixNano())
	trimmedTitle := strings.TrimSpace(title)
	if trimmedTitle == "" {
		trimmedTitle = "AI background task"
	}
	trimmedProjectPath := strings.TrimSpace(projectPath)
	session := &RemoteSession{
		ID:           sessionID,
		Tool:         "ai-assistant",
		Title:        trimmedTitle,
		LaunchSource: RemoteLaunchSourceAI,
		ProjectPath:  trimmedProjectPath,
		ModelID:      "maclaw-ai-assistant",
		Status:       SessionBusy,
		CreatedAt:    now,
		UpdatedAt:    now,
		AgentLoop:    loopCtx,
		Summary: SessionSummary{
			SessionID:       sessionID,
			Tool:            "ai-assistant",
			Title:           trimmedTitle,
			Source:          string(RemoteLaunchSourceAI),
			Status:          string(SessionBusy),
			Severity:        "info",
			CurrentTask:     trimmedTitle,
			ProgressSummary: "后台 AI 任务已创建",
			UpdatedAt:       now.Unix(),
		},
		Preview: SessionPreview{
			SessionID:    sessionID,
			OutputSeq:    1,
			PreviewLines: []string{"[AI background task created]", trimmedTitle},
			UpdatedAt:    now.Unix(),
		},
	}
	if loopCtx != nil {
		loopCtx.SessionID = sessionID
		if loopCtx.JobID != "" {
			session.JobID = loopCtx.JobID
		}
		if loopCtx.RunID != "" {
			session.RunID = loopCtx.RunID
		}
	}
	m.attachTraceToSession(session)
	m.storeSession(session)
	return session
}

func (m *RemoteSessionManager) AppendBackgroundAIOutput(sessionID string, lines ...string) {
	s, ok := m.Get(sessionID)
	if !ok || len(lines) == 0 {
		return
	}
	appendLines := make([]string, 0, len(lines))
	now := time.Now()
	var outputSeq int64
	var updatedAt int64
	var snap *SessionSummary
	for _, line := range lines {
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			continue
		}
		appendLines = append(appendLines, trimmed)
	}
	if len(appendLines) == 0 {
		return
	}
	s.mu.Lock()
	appendRawOutputLines(s, appendLines)
	s.Preview.PreviewLines = append(s.Preview.PreviewLines, appendLines...)
	if len(s.Preview.PreviewLines) > 500 {
		s.Preview.PreviewLines = s.Preview.PreviewLines[len(s.Preview.PreviewLines)-500:]
	}
	s.Preview.OutputSeq++
	s.Preview.UpdatedAt = now.Unix()
	s.UpdatedAt = now
	s.Summary.UpdatedAt = now.Unix()
	outputSeq = s.Preview.OutputSeq
	updatedAt = s.Preview.UpdatedAt
	snapVal := s.Summary
	snap = &snapVal
	s.mu.Unlock()
	if m.hubClient != nil {
		_ = m.hubClient.SendPreviewDelta(SessionPreviewDelta{
			SessionID:   sessionID,
			OutputSeq:   outputSeq,
			AppendLines: append([]string(nil), appendLines...),
			UpdatedAt:   updatedAt,
		})
		if snap != nil {
			_ = m.hubClient.SendSessionSummary(*snap)
		}
	}
	m.recordOutputEvidence(s, "progress", firstNonEmptyTraceText(appendLines[len(appendLines)-1], s.Title), appendLines)
	m.app.emitRemoteStateChanged()
}

func (m *RemoteSessionManager) UpdateBackgroundAISummary(sessionID string, mutate func(*RemoteSession)) {
	s, ok := m.Get(sessionID)
	if !ok || mutate == nil {
		return
	}
	now := time.Now()
	s.mu.Lock()
	mutate(s)
	s.UpdatedAt = now
	s.Summary.UpdatedAt = now.Unix()
	snap := s.Summary
	s.mu.Unlock()
	if m.hubClient != nil {
		_ = m.hubClient.SendSessionSummary(snap)
	}
	m.updateTraceFromSummary(s, snap)
	m.app.refreshPowerOptimizationState()
	m.app.emitRemoteStateChanged()
}

func (m *RemoteSessionManager) AddBackgroundAIEvent(sessionID string, evt ImportantEvent) {
	s, ok := m.Get(sessionID)
	if !ok {
		return
	}
	if evt.EventID == "" {
		evt.EventID = fmt.Sprintf("evt_%d", time.Now().UnixNano())
	}
	evt.SessionID = sessionID
	if evt.CreatedAt == 0 {
		evt.CreatedAt = time.Now().UnixMilli()
	}
	s.mu.Lock()
	s.Events = append([]ImportantEvent{evt}, s.Events...)
	if len(s.Events) > maxRecentImportantEvents {
		s.Events = s.Events[:maxRecentImportantEvents]
	}
	s.UpdatedAt = time.Now()
	s.Summary.UpdatedAt = s.UpdatedAt.Unix()
	s.mu.Unlock()
	if m.hubClient != nil {
		_ = m.hubClient.SendImportantEvent(evt)
	}
	m.recordImportantEventTrace(s, evt)
	m.app.emitRemoteStateChanged()
}

func (m *RemoteSessionManager) cancelAgentLoopSession(sessionID string) error {
	s, ok := m.Get(sessionID)
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	s.mu.RLock()
	loopCtx := s.AgentLoop
	s.mu.RUnlock()
	if loopCtx == nil {
		return fmt.Errorf("session execution not available: %s", sessionID)
	}
	loopCtx.Cancel()
	return nil
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

	s.mu.RLock()
	hasPendingQuestion := s.PendingUserQuestion != nil
	s.mu.RUnlock()
	if hasPendingQuestion {
		return m.writeSDKInput(s, sessionID, text, "structured")
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

	trimmed := strings.TrimSpace(text)

	s.mu.RLock()
	pending := s.PendingUserQuestion
	currentStatus := s.Status
	s.mu.RUnlock()

	// Reject new user messages while the session is busy (LLM thinking or
	// tool executing). Sending messages during an active turn can corrupt
	// the Claude Code SDK's internal state, causing it to stop responding.
	// AskUserQuestion answers are exempt — they are tool_result messages
	// that Claude Code expects while busy.
	//
	// Codex exec sessions are exempt from the busy check: Codex reads the
	// prompt from stdin on startup, so the process may appear "busy" (due
	// to the summary reducer matching "reading" in stderr output) when it
	// is actually waiting for input. Blocking writes here would deadlock
	// the session.
	_, isCodexSession := s.Exec.(*CodexSDKExecutionHandle)
	if pending == nil && currentStatus == SessionBusy && !isCodexSession {
		m.app.log(fmt.Sprintf("[remote-write-%s] REJECTED session=%s: session is busy, cannot send new user message", tag, sessionID))
		return fmt.Errorf("会话正忙，请等待当前操作完成后再发送新消息 (session is busy)")
	}

	markWaitingInputSubmitted := func(currentTask string) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.Status != SessionWaitingInput {
			return
		}
		s.Status = SessionBusy
		s.Summary.Status = string(SessionBusy)
		s.Summary.WaitingForUser = false
		if strings.TrimSpace(currentTask) != "" {
			s.Summary.CurrentTask = currentTask
		}
		s.Summary.SuggestedAction = ""
		s.Summary.UpdatedAt = time.Now().Unix()
	}

	if pending != nil {
		if responder, ok := s.Exec.(AskUserQuestionResponder); ok {
			m.app.log(fmt.Sprintf("[remote-write-%s] session=%s: answering AskUserQuestion tool_use_id=%s",
				tag, sessionID, pending.ToolUseID))
			if err := responder.WriteAskUserQuestionAnswer(pending, trimmed); err != nil {
				m.app.log(fmt.Sprintf("[remote-write-%s] tool_result FAILED session=%s: %v", tag, sessionID, err))
				return err
			}
			s.mu.Lock()
			if s.PendingUserQuestion != nil && s.PendingUserQuestion.ToolUseID == pending.ToolUseID {
				s.PendingUserQuestion = nil
				s.Summary.PendingQuestion = nil
				s.Summary.WaitingForUser = false
				s.Status = SessionBusy
				s.Summary.Status = string(SessionBusy)
				s.Summary.CurrentTask = "Submitting your answer"
				s.Summary.SuggestedAction = ""
				s.Summary.UpdatedAt = time.Now().Unix()
			}
			s.mu.Unlock()
			m.echoUserInput(s, sessionID, text)
			return nil
		}
	}

	err := s.Exec.Write([]byte(text))
	if err != nil {
		m.app.log(fmt.Sprintf("[remote-write-%s] FAILED session=%s: %v", tag, sessionID, err))
		return err
	}
	markWaitingInputSubmitted("Processing your input")
	m.echoUserInput(s, sessionID, text)
	return nil
}

// echoUserInput appends user input to raw output and preview for display.
func (m *RemoteSessionManager) echoUserInput(s *RemoteSession, sessionID, text string) {
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
		SessionID:       "default",
		ParentToolUseID: nil,
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
		return m.cancelAgentLoopSession(sessionID)
	}
	return s.Exec.Interrupt()
}

func (m *RemoteSessionManager) Kill(sessionID string) error {
	s, ok := m.Get(sessionID)
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	if s.Exec == nil {
		return m.cancelAgentLoopSession(sessionID)
	}
	return s.Exec.Kill()
}

// RemoveTerminated removes a session from the manager only if it is in a
// terminal (non-active) state. Returns true if the session was removed.
func (m *RemoteSessionManager) RemoveTerminated(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[sessionID]
	if !ok {
		return false
	}
	s.mu.RLock()
	active := isActiveRemoteSessionStatus(s.Status)
	s.mu.RUnlock()
	if active {
		return false // refuse to remove a live session
	}
	delete(m.sessions, sessionID)
	return true
}

// KillAllActive kills all sessions that are currently in an active state.
// Returns the list of session IDs that were killed.
func (m *RemoteSessionManager) KillAllActive() []string {
	m.mu.RLock()
	var targets []*RemoteSession
	for _, s := range m.sessions {
		if s == nil {
			continue
		}
		s.mu.RLock()
		active := isActiveRemoteSessionStatus(s.Status)
		s.mu.RUnlock()
		if active {
			targets = append(targets, s)
		}
	}
	m.mu.RUnlock()

	var killed []string
	for _, s := range targets {
		if s.Exec != nil {
			_ = s.Exec.Kill()
		} else {
			_ = m.cancelAgentLoopSession(s.ID)
		}
		killed = append(killed, s.ID)
	}
	return killed
}

func (m *RemoteSessionManager) runOutputLoop(s *RemoteSession) {
	defer func() {
		if r := recover(); r != nil {
			m.app.log(fmt.Sprintf("[remote-output-panic] session=%s recovered: %v", s.ID, r))
		}
	}()
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
			programLogger.WriteLines(s.ID, s.Tool, rawLines)
			m.app.log(fmt.Sprintf("[remote-output] session=%s, chunk_bytes=%d, new_raw_lines=%d",
				s.ID, len(chunk), len(rawLines)))
			// Check for startup prompts and auto-respond
			responder.feed(rawLines)
			// Track tool_use steps from PTY output
			for _, line := range rawLines {
				m.progressTracker.ConsumeLine(s.ID, line)
			}
		}

		result := pipeline.Consume(s, chunk)

		s.mu.Lock()
		applyOutputResult(s, result)
		// Update step progress for PTY sessions
		if sp := m.progressTracker.FormatProgress(s.ID); sp != "" {
			s.Summary.StepProgress = sp
			if prog := m.progressTracker.GetProgress(s.ID); prog != nil {
				s.Summary.StepCount = prog.StepCount
			}
		}
		s.mu.Unlock()

		m.syncTraceFromOutputResult(s, result)
		syncOutputResult(m.hubClient, result)

		// Emit code file events for the code preview panel.
		m.emitCodeFileEvents(s, result.Events)

		m.app.refreshPowerOptimizationState()
		m.app.emitRemoteStateChanged()
	}
}

// runSDKOutputLoop handles output for SDK-mode sessions (Claude Code stream-json).
// It reads from the Output() channel for text preview and also processes
// structured SDK messages from Messages() for proper event generation.
func (m *RemoteSessionManager) runSDKOutputLoop(s *RemoteSession) {
	defer func() {
		if r := recover(); r != nil {
			m.app.log(fmt.Sprintf("[sdk-output-panic] session=%s recovered: %v", s.ID, r))
		}
	}()
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

	// initTimeout breaks the deadlock where Claude Code waits for stdin
	// input before emitting system/init, but the UI waits for system/init
	// before allowing user input. After 5 seconds without system/init,
	// we transition to SessionWaitingInput so the user can send a message,
	// which will trigger Claude Code to emit system/init.
	initTimer := time.NewTimer(5 * time.Second)
	defer initTimer.Stop()
	initTimeoutCh := initTimer.C // nil-able so we can disable after firing

	// busyIdleTimer fires when the session has been in busy state for too
	// long without receiving a result message. This catches the case where
	// Claude Code finishes a task but the result message is lost or never
	// sent (e.g. API timeout, interrupted turn). Without this, the session
	// stays busy forever and blocks all user interaction.
	busyIdleTimeout := 90 * time.Second
	busyIdleTimer := time.NewTimer(busyIdleTimeout)
	busyIdleTimer.Stop() // start stopped; armed when entering busy state
	defer busyIdleTimer.Stop()
	var busyIdleTimerCh <-chan time.Time // nil when disarmed

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
		m.updateTraceFromSummary(s, snap)
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

	// busyTicker emits periodic progress lines into RawOutputLines while
	// the SDK is executing tool_use operations (busy state with no streaming
	// output).  This prevents the terminal from appearing frozen/blank.
	var busyTicker *time.Ticker
	var busyTickerDone chan struct{}

	stopBusyTicker := func() {
		if busyTicker != nil {
			busyTicker.Stop()
			close(busyTickerDone)
			busyTicker = nil
			busyTickerDone = nil
		}
	}
	defer stopBusyTicker()

	startBusyTicker := func(toolName string) {
		stopBusyTicker()
		startTime := time.Now()
		busyTicker = time.NewTicker(5 * time.Second)
		busyTickerDone = make(chan struct{})
		go func(name string, start time.Time, ticker *time.Ticker, done chan struct{}) {
			for {
				select {
				case <-done:
					return
				case t := <-ticker.C:
					elapsed := int(t.Sub(start).Seconds())
					line := fmt.Sprintf("⏳ %s running... (%ds)", name, elapsed)
					s.mu.Lock()
					s.RawOutputLines = append(s.RawOutputLines, line)
					if len(s.RawOutputLines) > 2000 {
						s.RawOutputLines = s.RawOutputLines[len(s.RawOutputLines)-2000:]
					}
					s.Preview.PreviewLines = append(s.Preview.PreviewLines, line)
					if len(s.Preview.PreviewLines) > 500 {
						s.Preview.PreviewLines = s.Preview.PreviewLines[len(s.Preview.PreviewLines)-500:]
					}
					s.UpdatedAt = time.Now()
					s.mu.Unlock()
					// Push preview delta to remote clients (mobile/PWA)
					if m.hubClient != nil {
						_ = m.hubClient.SendPreviewDelta(SessionPreviewDelta{
							SessionID:   s.ID,
							OutputSeq:   s.Preview.OutputSeq,
							AppendLines: []string{line},
							UpdatedAt:   time.Now().Unix(),
						})
					}
					m.app.emitRemoteStateChanged()
				}
			}
		}(toolName, startTime, busyTicker, busyTickerDone)
	}

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
					m.syncTraceFromOutputResult(s, result)
					syncOutputResult(m.hubClient, result)
				}

				// If the output channel closed without the session ever
				// initializing (no system/init received), the process
				// likely crashed on startup (missing API key, bad config,
				// etc.).  Update the summary so the user sees a clear
				// diagnostic instead of a generic "exit code 1" message.
				if !sessionStarted {
					s.mu.Lock()
					s.Summary.Severity = "error"
					s.Summary.CurrentTask = "SDK process exited without initializing"
					s.Summary.SuggestedAction = "Check tool installation, API key configuration, and network connectivity"
					s.Summary.UpdatedAt = time.Now().Unix()
					snap := s.Summary
					s.mu.Unlock()
					m.updateTraceFromSummary(s, snap)
					m.recordOutputEvidence(s, "error", firstNonEmptyTraceText(snap.CurrentTask, snap.SuggestedAction), []string{snap.CurrentTask})
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

			// New streaming output arrived — stop the busy ticker since
			// the terminal is no longer blank.
			stopBusyTicker()

			// Always reset the stall timer — any output (even nudge echoes)
			// proves the tool is alive.
			m.stallDetector.ResetTimer(s.ID, len(text) > 0)
			// Reset the busy idle timer too — streaming output means the
			// tool is actively working, not stuck.
			if busyIdleTimerCh != nil {
				if !busyIdleTimer.Stop() {
					select {
					case <-busyIdleTimer.C:
					default:
					}
				}
				busyIdleTimer.Reset(busyIdleTimeout)
			}
			// Resume stall monitoring if it was paused during thinking —
			// streaming output means the LLM has finished thinking and is
			// now producing content, so stall detection should be active.
			m.stallDetector.ResumeMonitoring(s.ID)

			// Filter nudge echoes — when the stall detector sends a nudge,
			// the tool may echo it back. Strip those lines to avoid clutter.
			text = m.filterNudgeEchoLines(s.ID, text)
			if text == "" {
				continue
			}

			// Accumulate text for RawOutputLines (desktop terminal)
			s.mu.Lock()
			appendStreamText(text)
			s.mu.Unlock()

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

			programLogger.WriteLines(s.ID, s.Tool, normalizeChunkLines([]byte(toSend)))

			s.mu.Lock()
			applyOutputResult(s, result)
			s.mu.Unlock()

			m.syncTraceFromOutputResult(s, result)
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
				m.syncTraceFromOutputResult(s, pResult)
				syncOutputResult(m.hubClient, pResult)
			}

			now := time.Now()

			// Collect hub events to send after releasing the lock
			var summaryToSync *SessionSummary
			var eventsToSync []ImportantEvent
			var imagesToSync []ImageTransferMessage
			var lastToolName string // tracks last tool_use name for busy ticker

			s.mu.Lock()
			s.UpdatedAt = now

			switch msg.Type {
			case "system":
				if msg.Subtype == "init" && !sessionStarted {
					sessionStarted = true
					initTimer.Stop()    // Cancel the deadlock-breaker timer
					initTimeoutCh = nil // Prevent select from spinning on fired timer
					// SDK init means the tool is ready for user input —
					// transition directly to waiting_input so maclaw
					// (and the mobile PWA) see the session as interactive
					// immediately, instead of lingering in "running/starting".
					s.Status = SessionWaitingInput
					s.Summary.Status = string(SessionWaitingInput)
					s.Summary.WaitingForUser = true
					s.Summary.Severity = "info"
					s.Summary.CurrentTask = "Session initialized"
					s.Summary.SuggestedAction = "Send the first instruction to start working"
					s.Summary.UpdatedAt = now.Unix()
					snap := s.Summary
					summaryToSync = &snap
				}

			case "assistant":
				s.Status = SessionBusy
				s.Summary.Status = string(SessionBusy)
				s.Summary.WaitingForUser = false
				s.Summary.UpdatedAt = now.Unix()
				flushStreamAccum()

				if msg.Message != nil {
					for _, block := range msg.Message.Content {
						if block.Type == "tool_use" && block.Name != "" {
							lastToolName = block.Name
							// Record step in progress tracker
							m.progressTracker.RecordSDKToolUse(s.ID, block.Name, block.Input)
							evt := buildSDKToolUseEvent(s, block)
							s.Events = appendRecentEvents(s.Events, evt, maxRecentImportantEvents)
							eventsToSync = append(eventsToSync, evt)
							// Track block.ID → EventID for coalescer completion
							if block.ID != "" {
								toolUseToEventID[block.ID] = evt.EventID
							}
							// AskUserQuestion means Claude Code is waiting for
							// user input via tool_result. Transition to
							// waiting_input and record the pending tool_use so
							// WriteInput can wrap the reply as a tool_result.
							if block.Name == "AskUserQuestion" && block.ID != "" {
								questionView := buildAskUserQuestionView(block.ID, block.Name, block.Input)
								rawInput, _ := block.Input.(map[string]interface{})
								s.PendingUserQuestion = &PendingToolUse{
									ToolUseID: block.ID,
									ToolName:  block.Name,
									Question:  questionView,
									RawInput:  rawInput,
								}
								s.Status = SessionWaitingInput
								s.Summary.Status = string(SessionWaitingInput)
								s.Summary.WaitingForUser = true
								s.Summary.PendingQuestion = clonePendingQuestionView(questionView)
								if questionView != nil && strings.TrimSpace(questionView.Question) != "" {
									s.Summary.CurrentTask = questionView.Question
								} else {
									s.Summary.CurrentTask = "Waiting for your answer"
								}
								if questionView != nil && len(questionView.Options) > 0 {
									s.Summary.SuggestedAction = "Choose an option or answer to continue"
								} else {
									s.Summary.SuggestedAction = "Answer the question to continue"
								}
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
				// Update step progress in summary before snapshotting
				if sp := m.progressTracker.FormatProgress(s.ID); sp != "" {
					s.Summary.StepProgress = sp
					if prog := m.progressTracker.GetProgress(s.ID); prog != nil {
						s.Summary.StepCount = prog.StepCount
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
				s.PendingUserQuestion = nil // Clear any stale pending question
				s.Summary.PendingQuestion = nil
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

			// Emit code file events for the code preview panel.
			m.emitCodeFileEvents(s, eventsToSync)

			// Stall detector integration (outside s.mu lock — StallDetector has its own lock)
			switch msg.Type {
			case "assistant":
				updateThinking(true)
				m.stallDetector.StartMonitoring(s.ID, s.Exec, s.Tool)
				// Pause stall detection during thinking phase — the LLM may
				// take minutes to respond via slow API proxies, and no
				// streaming output is expected during thinking. Without this
				// pause, the stall detector fires an interrupt that kills
				// the in-progress turn.
				m.stallDetector.PauseMonitoring(s.ID)
				// Start busy ticker if assistant message contained tool_use
				// blocks, so the terminal shows periodic progress while
				// tools execute (prevents blank screen during busy state).
				if lastToolName != "" {
					startBusyTicker(lastToolName)
				}
				// Arm the busy idle timer — if no result message arrives
				// within busyIdleTimeout, auto-degrade to waiting_input.
				if !busyIdleTimer.Stop() {
					select {
					case <-busyIdleTimer.C:
					default:
					}
				}
				busyIdleTimer.Reset(busyIdleTimeout)
				busyIdleTimerCh = busyIdleTimer.C
			case "user":
				// Tool result arrived — stop the busy progress ticker.
				stopBusyTicker()
				// Reset the busy idle timer — tool_result means the session
				// is actively processing (tool completed, next step coming).
				if busyIdleTimerCh != nil {
					if !busyIdleTimer.Stop() {
						select {
						case <-busyIdleTimer.C:
						default:
						}
					}
					busyIdleTimer.Reset(busyIdleTimeout)
				}
			case "result":
				// Thinking state already cleared inline under s.mu above;
				// updateThinking is a no-op here but kept for symmetry.
				stopBusyTicker()
				m.stallDetector.StopMonitoring(s.ID)
				// Disarm the busy idle timer — result received normally.
				if !busyIdleTimer.Stop() {
					select {
					case <-busyIdleTimer.C:
					default:
					}
				}
				busyIdleTimerCh = nil
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

		case <-busyIdleTimerCh:
			// Busy idle timeout: session has been busy for too long without
			// a result message. Auto-degrade to waiting_input so the user
			// can send new instructions or kill the session.
			busyIdleTimerCh = nil
			s.mu.Lock()
			if s.Status == SessionBusy {
				m.app.log(fmt.Sprintf("[sdk-busy-idle-timeout] session=%s: no result after %v, forcing SessionWaitingInput", s.ID, busyIdleTimeout))
				s.Status = SessionWaitingInput
				s.Summary.Status = string(SessionWaitingInput)
				s.Summary.WaitingForUser = true
				s.Summary.SuggestedAction = "编程工具长时间无响应，已自动解锁。可发送新指令或终止会话"
				s.Summary.UpdatedAt = time.Now().Unix()
				snap := s.Summary
				s.mu.Unlock()
				if m.hubClient != nil {
					_ = m.hubClient.SendSessionSummary(snap)
				}
				m.updateTraceFromSummary(s, snap)
			} else {
				s.mu.Unlock()
			}
			m.app.emitRemoteStateChanged()

		case <-initTimeoutCh:
			// Deadlock breaker: Claude Code in -p --input-format stream-json
			// mode waits for stdin input before emitting system/init. If we
			// haven't received system/init within 5 seconds, transition to
			// SessionWaitingInput so the user can send a message (which will
			// trigger Claude Code to emit system/init and start working).
			initTimeoutCh = nil // Prevent re-entry on subsequent select iterations
			if !sessionStarted {
				sessionStarted = true
				m.app.log(fmt.Sprintf("[sdk-init-timeout] session=%s: no system/init after 5s, forcing SessionWaitingInput to break deadlock", s.ID))
				s.mu.Lock()
				s.Status = SessionWaitingInput
				s.Summary.Status = string(SessionWaitingInput)
				s.Summary.WaitingForUser = true
				s.Summary.Severity = "info"
				s.Summary.CurrentTask = "Session ready (waiting for first message)"
				s.Summary.SuggestedAction = "Send the first instruction to start working"
				s.Summary.UpdatedAt = time.Now().Unix()
				snap := s.Summary
				s.mu.Unlock()
				if m.hubClient != nil {
					_ = m.hubClient.SendSessionSummary(snap)
				}
				m.updateTraceFromSummary(s, snap)
				m.app.emitRemoteStateChanged()
			}
		}
	}
}

// runCodexSDKOutputLoop handles output for Codex SDK-mode sessions.
// Codex exec --json emits complete JSONL lines (not streaming fragments),
// so we don't need the streaming accumulator used by Claude's SDK loop.
func (m *RemoteSessionManager) runCodexSDKOutputLoop(s *RemoteSession) {
	defer func() {
		if r := recover(); r != nil {
			m.app.log(fmt.Sprintf("[codex-output-panic] session=%s recovered: %v", s.ID, r))
		}
	}()
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

		// Mark session as running on first output. Codex is one-shot
		// (codex exec) — it runs the task and exits, so "running" is
		// the correct state (it doesn't wait for user input).
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
			m.updateTraceFromSummary(s, snap)
		}

		// Detect Codex waiting for stdin input — this means the process
		// is ready to receive a prompt, NOT that it's busy working.
		// Without this, the summary reducer's "reading" keyword match
		// would set the status to SessionBusy, blocking WriteInput.
		if strings.Contains(text, "Reading prompt from stdin") {
			s.mu.Lock()
			s.Status = SessionWaitingInput
			s.Summary.Status = string(SessionWaitingInput)
			s.Summary.WaitingForUser = true
			s.Summary.CurrentTask = "Codex ready — waiting for prompt"
			s.Summary.SuggestedAction = "Send the task instruction to start working"
			s.Summary.UpdatedAt = time.Now().Unix()
			snap := s.Summary
			s.mu.Unlock()
			if m.hubClient != nil {
				_ = m.hubClient.SendSessionSummary(snap)
			}
			m.updateTraceFromSummary(s, snap)
			m.app.emitRemoteStateChanged()
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

		programLogger.WriteLines(s.ID, s.Tool, lines)

		result := pipeline.Consume(s, chunk)

		s.mu.Lock()
		applyOutputResult(s, result)
		s.mu.Unlock()

		m.syncTraceFromOutputResult(s, result)
		syncOutputResult(m.hubClient, result)

		// Emit code file events for the code preview panel.
		m.emitCodeFileEvents(s, result.Events)

		m.app.refreshPowerOptimizationState()
		m.app.emitRemoteStateChanged()
	}

	// If the output channel closed without any real codex output, the process
	// likely crashed on startup.
	// likely crashed on startup.  Update the summary so the user sees the issue.
	if !gotRealOutput {
		s.mu.Lock()
		s.Summary.Severity = "error"
		s.Summary.CurrentTask = "Codex process exited without producing output"
		s.Summary.SuggestedAction = "Check codex installation and API key configuration"
		s.Summary.UpdatedAt = time.Now().Unix()
		snap := s.Summary
		s.mu.Unlock()
		m.updateTraceFromSummary(s, snap)
		m.recordOutputEvidence(s, "error", firstNonEmptyTraceText(snap.CurrentTask, snap.SuggestedAction), []string{snap.CurrentTask})
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
	defer func() {
		if r := recover(); r != nil {
			m.app.log(fmt.Sprintf("[gemini-acp-output-panic] session=%s recovered: %v", s.ID, r))
		}
	}()
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

		// Mark session as ready on first output. The ACP handshake
		// (initialize + session/new) completes inside Start(), so the
		// first output line means the tool is ready for user input.
		if !sessionStarted {
			sessionStarted = true
			s.mu.Lock()
			s.Status = SessionWaitingInput
			s.Summary.Status = string(SessionWaitingInput)
			s.Summary.WaitingForUser = true
			s.Summary.Severity = "info"
			s.Summary.CurrentTask = "Gemini ACP session started"
			s.Summary.SuggestedAction = "Send the first instruction to start working"
			s.Summary.UpdatedAt = time.Now().Unix()
			snap := s.Summary
			s.mu.Unlock()
			if m.hubClient != nil {
				_ = m.hubClient.SendSessionSummary(snap)
			}
			m.updateTraceFromSummary(s, snap)
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
			m.updateTraceFromSummary(s, snap)
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
			if level == CompletionIncomplete {
				s.AutoContinueCount++
			}
			snap := s.Summary
			s.mu.Unlock()
			if m.hubClient != nil {
				_ = m.hubClient.SendSessionSummary(snap)
			}
			m.updateTraceFromSummary(s, snap)
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
			m.updateTraceFromSummary(s, snap)
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
			m.updateTraceFromSummary(s, snap)
		}

		// Append to raw output lines — filter nudge echoes first.
		lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
		filtered := make([]string, 0, len(lines))
		for _, line := range lines {
			if !m.stallDetector.IsNudgeEcho(s.ID, line) {
				filtered = append(filtered, line)
			}
		}

		// Always reset the stall timer — even nudge echoes prove the tool
		// is alive and responding.
		m.stallDetector.ResetTimer(s.ID, true)

		if len(filtered) == 0 {
			continue
		}
		s.mu.Lock()
		appendRawOutputLines(s, filtered)
		s.mu.Unlock()

		programLogger.WriteLines(s.ID, s.Tool, filtered)

		filteredText := strings.Join(filtered, "\n") + "\n"
		result := pipeline.Consume(s, []byte(filteredText))

		s.mu.Lock()
		applyOutputResult(s, result)
		s.mu.Unlock()

		m.syncTraceFromOutputResult(s, result)
		syncOutputResult(m.hubClient, result)

		// Emit code file events for the code preview panel.
		m.emitCodeFileEvents(s, result.Events)

		m.app.refreshPowerOptimizationState()
		m.app.emitRemoteStateChanged()
	}
}

// filterNudgeEchoLines removes lines that are echoes of stall-detector nudge
// messages. Returns the filtered text; may return "" if all lines were echoes.
func (m *RemoteSessionManager) filterNudgeEchoLines(sessionID, text string) string {
	if !strings.Contains(text, "\n") {
		// Single incomplete line — check as-is.
		if m.stallDetector.IsNudgeEcho(sessionID, text) {
			return ""
		}
		return text
	}
	lines := strings.Split(text, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if !m.stallDetector.IsNudgeEcho(sessionID, line) {
			filtered = append(filtered, line)
		}
	}
	if len(filtered) == 0 {
		return ""
	}
	return strings.Join(filtered, "\n")
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

// buildResumeContext creates a SessionResumeContext from the current session
// state. Must be called with s.mu held. The reason parameter is typically
// "token_limit" or "api_error".
func buildResumeContext(s *RemoteSession, reason string) *SessionResumeContext {
	rc := &SessionResumeContext{
		ProjectPath:  s.ProjectPath,
		Tool:         s.Tool,
		LastProgress: s.Summary.ProgressSummary,
		ExitReason:   reason,
	}
	// Capture provider-native structured session IDs for --resume support.
	// This allows the auto-resume logic to continue the exact conversation
	// instead of starting fresh, preserving full context history.
	if sdkHandle, ok := s.Exec.(*SDKExecutionHandle); ok {
		rc.ResumeSessionID = sdkHandle.ClaudeSessionID()
		rc.ClaudeSessionID = rc.ResumeSessionID
	}
	if codexHandle, ok := s.Exec.(*CodexSDKExecutionHandle); ok {
		rc.ResumeSessionID = codexHandle.ThreadID()
	}
	// Carry forward resume count and original task from previous session.
	if s.ResumeContext != nil {
		rc.ResumeCount = s.ResumeContext.ResumeCount + 1
		rc.OriginalTask = s.ResumeContext.OriginalTask
	}
	// Capture important files from summary.
	if len(s.Summary.ImportantFiles) > 0 {
		rc.CompletedFiles = make([]string, len(s.Summary.ImportantFiles))
		copy(rc.CompletedFiles, s.Summary.ImportantFiles)
	}
	// Capture tail of output for context.
	if tail := s.RawOutputLines; len(tail) > 0 {
		if len(tail) > 20 {
			tail = tail[len(tail)-20:]
		}
		rc.LastOutput = strings.Join(tail, "\n")
		if len(rc.LastOutput) > 500 {
			rc.LastOutput = rc.LastOutput[len(rc.LastOutput)-500:]
		}
	}
	return rc
}

func (m *RemoteSessionManager) runExitLoop(s *RemoteSession) {
	defer func() {
		if r := recover(); r != nil {
			m.app.log(fmt.Sprintf("[remote-exit-panic] session=%s recovered: %v", s.ID, r))
		}
	}()
	// Ensure config cleanup runs even if the exit channel is nil or closed
	// unexpectedly, so the user's native tool config is always restored.
	defer func() {
		m.stallDetector.StopMonitoring(s.ID)
		m.progressTracker.Reset(s.ID)
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
	s.ThinkingState = ThinkingIdle
	s.ThinkingSince = time.Time{}
	s.Summary.Thinking = false
	s.Summary.ThinkingSince = 0

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
				// Structured sessions (Claude Code SDK, Gemini ACP, Codex, iFlow)
				// normally exit with code 1 — treat this as a benign exit, not a warning.
				if s.isStructuredSession() && *exit.Code == 1 {
					s.Summary.Severity = "info"
				} else {
					s.Summary.Severity = "warn"
				}
				if stderrHint != "" {
					s.Summary.LastResult += " — " + stderrHint
				}
				if s.Summary.Severity == "warn" {
					s.Summary.SuggestedAction = "Check tool installation and configuration, then retry"
				}
			}
		} else {
			s.Summary.LastResult = "Session exited"
		}
		if s.Summary.SuggestedAction == "" {
			s.Summary.ProgressSummary = "Session is no longer running"
			s.Summary.SuggestedAction = "Start a new session when ready"
		}
	}
	// Build resume context for structured sessions so the Agent can
	// auto-continue unfinished tasks or retry after transient errors.
	if s.isStructuredSession() && s.ExitCode != nil {
		var reason string
		switch {
		case exit.Err == nil && (*s.ExitCode == 0 || *s.ExitCode == 1):
			reason = "token_limit"
		case *s.ExitCode > 1:
			reason = "api_error"
		}
		if reason != "" {
			s.ResumeContext = buildResumeContext(s, reason)
		}

		// Force CompletionIncomplete when the session exits early with
		// very few output lines. This ensures the Agent receives a strong
		// auto-resume hint instead of an ambiguous "uncertain" signal,
		// which often causes it to give up rather than retry.
		if len(s.RawOutputLines) <= earlyExitLineThreshold && s.CompletionLevel != CompletionCompleted {
			s.CompletionLevel = CompletionIncomplete
		}
	}

	closedEvent := buildSessionClosedEvent(s, exit)
	s.Events = appendRecentEvents(s.Events, closedEvent, maxRecentImportantEvents)
	summarySnap := s.Summary
	exitStatus := s.Status
	traceRunID := s.RunID
	traceLastResult := s.Summary.LastResult
	traceTailLines := append([]string(nil), s.RawOutputLines...)
	var exitCodeVal *int
	if s.ExitCode != nil {
		cp := *s.ExitCode
		exitCodeVal = &cp
	}
	s.mu.Unlock()

	m.updateTraceFromSummary(s, summarySnap)
	m.recordImportantEventTrace(s, closedEvent)
	if traceRunID != "" {
		traceSvc := m.traceService()
		if exit.Err != nil || exitStatus == SessionError {
			if traceSvc != nil {
				traceSvc.UpdateRun(traceRunID, TraceRunStatusFailed, summarySnap.ProgressSummary, traceLastResult)
			}
			m.recordOutputEvidence(s, "error", firstNonEmptyTraceText(traceLastResult, summarySnap.ProgressSummary, "Remote session failed"), traceTailLines)
		} else {
			if traceSvc != nil {
				traceSvc.UpdateRun(traceRunID, TraceRunStatusExited, firstNonEmptyTraceText(summarySnap.ProgressSummary, summarySnap.LastResult), "")
			}
			m.recordOutputEvidence(s, "result", firstNonEmptyTraceText(summarySnap.LastResult, summarySnap.ProgressSummary, "Remote session exited"), traceTailLines)
		}
	}

	if m.hubClient != nil {
		_ = m.hubClient.SendSessionSummary(summarySnap)
		_ = m.hubClient.SendImportantEvent(closedEvent)
		_ = m.hubClient.SendSessionClosed(s)
	}

	// --- Harness: FeedbackInjector — consume session events for next-session feedback ---
	// Note: feedbackInjector uses corelib/remote.ImportantEvent which differs
	// from the GUI's local ImportantEvent type. Integration deferred until
	// the types are unified or an adapter is added.

	// --- Harness: FailureLearner — record errors for constraint generation ---
	if s.ProjectPath != "" && (exit.Err != nil || exitStatus == SessionError) {
		errDetail := firstNonEmptyTraceText(summarySnap.LastResult, summarySnap.ProgressSummary)
		if errDetail != "" {
			m.failureLearnersMu.Lock()
			learner, ok := m.failureLearners[s.ProjectPath]
			if !ok {
				learner = remote.NewFailureLearner(s.ProjectPath)
				m.failureLearners[s.ProjectPath] = learner
			}
			m.failureLearnersMu.Unlock()
			learner.RecordError(errDetail, errDetail)
		}
	}

	// --- Harness: HarnessGate — validate changed files against project constraints ---
	if exitStatus == SessionExited && s.ProjectPath != "" && len(summarySnap.ImportantFiles) > 0 {
		gate := security.NewHarnessGate(nil, s.ProjectPath)
		if err := gate.LoadConstraints(s.ProjectPath); err == nil {
			violations := gate.CheckOutput(s.ID, summarySnap.ImportantFiles)
			if len(violations) > 0 {
				report := gate.BuildViolationReport(violations)
				log.Printf("[HarnessGate] session %s: %d violations\n%s", s.ID, len(violations), report)
			}
		}
	}

	// Trigger experience extraction for successfully completed sessions
	// (exited with code 0). Failed sessions are poor candidates for
	// reusable patterns.
	// Structured sessions (Claude Code, Gemini, Codex, iFlow) normally
	// exit with code 1, so treat exit code ≤ 1 as success for them.
	exitOK := exitStatus == SessionExited && exitCodeVal != nil &&
		(*exitCodeVal == 0 || (s.isStructuredSession() && *exitCodeVal == 1))
	if exitOK {
		m.app.ensureExperienceExtractor()
		if m.app.experienceExtractor != nil {
			go func() {
				_ = m.app.experienceExtractor.Extract(s)
			}()
		}
	}
	// Save session checkpoint to memory store so the next session on the
	// same project can resume where this one left off.
	m.app.ensureSessionCheckpointer()
	if m.app.sessionCheckpointer != nil {
		if strings.TrimSpace(m.app.testHomeDir) != "" {
			_ = m.app.sessionCheckpointer.SaveCheckpoint(s)
		} else {
			go func() {
				_ = m.app.sessionCheckpointer.SaveCheckpoint(s)
			}()
		}
	}
	if shouldCreatePendingResumeSlot(s) && m.app != nil {
		mem := m.app.ensureConversationMemory()
		if mem != nil {
			slotID := fmt.Sprintf("unfinished-%s", s.ID)
			resumePrompt := ""
			if m.app.sessionCheckpointer != nil {
				resumePrompt = m.app.sessionCheckpointer.BuildResumePrompt(s.ProjectPath)
			}
			mem.UpsertUnfinishedSlot("desktop-user", &unfinishedTaskSlot{
				SlotID:           slotID,
				UserID:           "desktop-user",
				ProjectPath:      s.ProjectPath,
				Tool:             firstNonEmptyTraceText(strings.TrimSpace(s.Tool), strings.TrimSpace(s.ResumeContext.Tool), "claude"),
				Status:           "pending_resume",
				Summary:          firstNonEmptyTraceText(s.Summary.ProgressSummary, s.ResumeContext.LastProgress, s.Summary.LastResult),
				LastTask:         firstNonEmptyTraceText(s.Summary.CurrentTask, s.ResumeContext.OriginalTask),
				ResumePrompt:     resumePrompt,
				Source:           "session_exit",
				EvidenceScopeKey: firstNonEmptyTraceText(s.RunID, s.ProjectPath, s.ID),
				CreatedAt:        time.Now(),
				UpdatedAt:        time.Now(),
			})
		}
	}
	if m.app != nil && strings.TrimSpace(m.app.testHomeDir) != "" && m.app.memoryStore != nil {
		m.app.memoryStore.Stop()
		m.app.memoryStore = nil
		m.app.sessionCheckpointer = nil
	}
	if s.workspaceRelease != nil {
		s.workspaceRelease()
		s.workspaceRelease = nil
	}

	// Emit code:session_end event for the code preview panel.
	if m.app != nil && m.app.codeEventEmitter != nil {
		m.app.codeEventEmitter.EmitSessionEnd(s.ID)
	}

	m.app.refreshPowerOptimizationState()
	m.app.emitRemoteStateChanged()
}

func shouldCreatePendingResumeSlot(s *RemoteSession) bool {
	if s == nil || s.ResumeContext == nil {
		return false
	}
	return s.CompletionLevel == CompletionIncomplete
}

func (m *RemoteSessionManager) SuppressResumeForSession(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	s, ok := m.Get(sessionID)
	if !ok || s == nil {
		return
	}
	s.mu.Lock()
	s.ResumeContext = nil
	s.mu.Unlock()
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

func buildAskUserQuestionView(toolUseID, toolName string, input interface{}) *PendingQuestionView {
	view := &PendingQuestionView{
		ToolUseID: strings.TrimSpace(toolUseID),
		ToolName:  strings.TrimSpace(toolName),
	}
	payload, ok := input.(map[string]interface{})
	if !ok {
		if view.ToolUseID == "" && view.ToolName == "" {
			return nil
		}
		return view
	}
	questions := parseAskUserQuestionEntries(payload)
	if len(questions) > 0 {
		first := questions[0]
		view.Header = first.Header
		view.Question = first.Question
		view.Hint = first.Hint
		view.Multi = first.Multi
		view.Options = first.Options
		return view
	}
	if text := firstNonEmptyString(payload, "question", "prompt", "text", "message", "title"); text != "" {
		view.Question = text
	}
	if hint := firstNonEmptyString(payload, "hint", "description", "instructions"); hint != "" {
		view.Hint = hint
	}
	if view.ToolUseID == "" && view.ToolName == "" && view.Question == "" && view.Hint == "" {
		return nil
	}
	return view
}

func parseAskUserQuestionEntries(payload map[string]interface{}) []PendingQuestionView {
	raw, ok := payload["questions"]
	if !ok {
		return nil
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	views := make([]PendingQuestionView, 0, len(items))
	for _, item := range items {
		qmap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		question := PendingQuestionView{
			Header:   firstNonEmptyString(qmap, "header", "title", "label"),
			Question: firstNonEmptyString(qmap, "question", "prompt", "text", "title"),
			Hint:     firstNonEmptyString(qmap, "description", "hint", "help_text"),
			Multi:    askUserQuestionBoolValue(qmap["multiSelect"]),
		}
		if rawOptions, ok := qmap["options"].([]interface{}); ok {
			for _, rawOpt := range rawOptions {
				omap, ok := rawOpt.(map[string]interface{})
				if !ok {
					continue
				}
				question.Options = append(question.Options, PendingQuestionOption{
					Label:       firstNonEmptyString(omap, "label", "title", "value"),
					Description: firstNonEmptyString(omap, "description", "hint"),
					Preview:     firstNonEmptyString(omap, "preview"),
				})
			}
		}
		if question.Header == "" && question.Question == "" && len(question.Options) == 0 {
			continue
		}
		views = append(views, question)
	}
	return views
}

func firstNonEmptyString(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		value, ok := m[key]
		if !ok {
			continue
		}
		if s, ok := value.(string); ok {
			if trimmed := strings.TrimSpace(s); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func askUserQuestionBoolValue(v interface{}) bool {
	b, _ := v.(bool)
	return b
}

func buildAskUserQuestionAnswerContent(pending *PendingToolUse, text string) interface{} {
	trimmed := strings.TrimSpace(text)
	if pending == nil || pending.Question == nil {
		return trimmed
	}
	question := pending.Question
	if len(question.Options) == 0 {
		return trimmed
	}
	answer := map[string]interface{}{
		"text": trimmed,
	}
	matched := make([]string, 0, 1)
	lowered := strings.ToLower(trimmed)
	for _, option := range question.Options {
		label := strings.TrimSpace(option.Label)
		if label == "" {
			continue
		}
		if strings.EqualFold(label, trimmed) || strings.Contains(strings.ToLower(label), lowered) || strings.Contains(lowered, strings.ToLower(label)) {
			matched = append(matched, label)
			if !question.Multi {
				break
			}
		}
	}
	if len(matched) > 0 {
		if question.Multi {
			answer["selected_options"] = matched
		} else {
			answer["selected_option"] = matched[0]
		}
	}
	return answer
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
