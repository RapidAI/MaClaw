package main

import (
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

	mu       sync.RWMutex
	sessions map[string]*RemoteSession
}

func NewRemoteSessionManager(app *App) *RemoteSessionManager {
	return &RemoteSessionManager{
		app:      app,
		sessions: map[string]*RemoteSession{},
		executionFactory: func(spec LaunchSpec) (ExecutionStrategy, error) {
			return NewLocalPTYExecutionStrategy(func() PTYSession {
				return NewWindowsPTYSession()
			}), nil
		},
		workspacePreparer: NewDefaultWorkspacePreparer(),
		pipelineFactory: func() *OutputPipeline {
			return NewOutputPipeline()
		},
		providerFactory: func(tool string) (ProviderAdapter, error) {
			return app.remoteProviderAdapter(tool)
		},
	}
}

func (m *RemoteSessionManager) SetHubClient(client *RemoteHubClient) {
	m.hubClient = client
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
	cmd, err := provider.BuildCommand(spec)
	if err != nil {
		session := m.newFailedSession(sessionID, spec, provider, now, err)
		m.storeSession(session)
		m.syncFailedSession(session)
		return session, err
	}

	strategy, err := m.executionFactory(spec)
	if err != nil {
		session := m.newFailedSession(sessionID, spec, provider, now, err)
		m.storeSession(session)
		m.syncFailedSession(session)
		return session, err
	}

	execHandle, err := strategy.Start(cmd)
	if err != nil {
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
	}
	initEvent := buildSessionInitEvent(session)
	session.Events = []ImportantEvent{initEvent}
	workspace = nil

	m.storeSession(session)

	if m.hubClient != nil {
		_ = m.hubClient.SendSessionCreated(session)
		_ = m.hubClient.SendImportantEvent(initEvent)
	}

	go m.runOutputLoop(session)
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
		title = "Claude Session"
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
			CurrentTask:     "Starting Claude session",
			ProgressSummary: "Claude remote launch failed before the session became interactive",
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
		if isActiveRemoteSessionStatus(s.Status) {
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
	// ConPTY on Windows requires "\r\n" (or "\r") to simulate pressing Enter.
	// A bare "\n" is treated as a literal linefeed and does NOT trigger command
	// execution.  Normalize all line endings to "\r\n" so that input from any
	// client (desktop, PWA, mobile) works correctly regardless of what line
	// ending the client sends.
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\n", "\r\n")
	return s.Exec.Write([]byte(normalized))
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

	output := sessionOutput(s)
	if output == nil {
		return
	}

	for chunk := range output {
		result := pipeline.Consume(s, chunk)
		s.UpdatedAt = time.Now()
		changed := false

		if result.Summary != nil {
			s.Summary = *result.Summary
			s.Status = SessionStatus(result.Summary.Status)
			changed = true
			if m.hubClient != nil {
				_ = m.hubClient.SendSessionSummary(s.Summary)
			}
		}

		if result.PreviewDelta != nil {
			s.Preview.SessionID = s.ID
			s.Preview.OutputSeq = result.PreviewDelta.OutputSeq
			s.Preview.UpdatedAt = result.PreviewDelta.UpdatedAt
			s.Preview.PreviewLines = append(s.Preview.PreviewLines, result.PreviewDelta.AppendLines...)
			if len(s.Preview.PreviewLines) > 100 {
				s.Preview.PreviewLines = s.Preview.PreviewLines[len(s.Preview.PreviewLines)-100:]
			}
			changed = true
			if m.hubClient != nil {
				_ = m.hubClient.SendPreviewDelta(*result.PreviewDelta)
			}
		}

		for _, evt := range result.Events {
			s.Events = appendRecentEvents(s.Events, evt, maxRecentImportantEvents)
			changed = true
			if m.hubClient != nil {
				_ = m.hubClient.SendImportantEvent(evt)
			}
		}

		if changed {
			m.app.refreshPowerOptimizationState()
			m.app.emitRemoteStateChanged()
		}
	}
}

func appendRecentEvents(events []ImportantEvent, event ImportantEvent, limit int) []ImportantEvent {
	if event.EventID == "" && event.Type == "" && event.Summary == "" && event.Title == "" {
		return events
	}

	out := append(events, event)
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

func (m *RemoteSessionManager) runExitLoop(s *RemoteSession) {
	exitCh := sessionExit(s)
	if exitCh == nil {
		return
	}

	exit, ok := <-exitCh
	if !ok {
		return
	}
	now := time.Now()

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
	if exit.Err != nil {
		s.Summary.Severity = "error"
		s.Summary.LastResult = exit.Err.Error()
		s.Summary.ProgressSummary = "Session terminated with an execution error"
		s.Summary.SuggestedAction = "Review the error output and retry"
	} else {
		s.Summary.Severity = "info"
		if exit.Code != nil {
			s.Summary.LastResult = fmt.Sprintf("Session exited with code %d", *exit.Code)
			if *exit.Code != 0 {
				s.Summary.Severity = "warn"
			}
		} else {
			s.Summary.LastResult = "Session exited"
		}
		s.Summary.ProgressSummary = "Session is no longer running"
		s.Summary.SuggestedAction = "Start a new session when ready"
	}
	closedEvent := buildSessionClosedEvent(s, exit)
	s.Events = appendRecentEvents(s.Events, closedEvent, maxRecentImportantEvents)

	if m.hubClient != nil {
		_ = m.hubClient.SendSessionSummary(s.Summary)
		_ = m.hubClient.SendImportantEvent(closedEvent)
		_ = m.hubClient.SendSessionClosed(s)
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
