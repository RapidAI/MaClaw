package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"encoding/json"
)

var ErrRemoteSessionsUnavailable = errors.New("remote sessions are not initialized")

type RemoteConnectionStatus struct {
	Enabled      bool   `json:"enabled"`
	HubURL       string `json:"hub_url"`
	MachineID    string `json:"machine_id"`
	Connected    bool   `json:"connected"`
	LastError    string `json:"last_error"`
	SessionCount int    `json:"session_count"`
}

type RemoteSmokeSnapshot struct {
	Exists bool               `json:"exists"`
	Path   string             `json:"path"`
	Report *RemoteSmokeReport `json:"report,omitempty"`
}

type RemoteSessionView struct {
	ID             string           `json:"id"`
	Tool           string           `json:"tool"`
	Title          string           `json:"title"`
	LaunchSource   string           `json:"launch_source,omitempty"`
	ProjectPath    string           `json:"project_path"`
	WorkspacePath  string           `json:"workspace_path"`
	WorkspaceRoot  string           `json:"workspace_root"`
	WorkspaceMode  WorkspaceMode    `json:"workspace_mode"`
	WorkspaceIsGit bool             `json:"workspace_is_git"`
	ModelID        string           `json:"model_id"`
	Status         SessionStatus    `json:"status"`
	PID            int              `json:"pid"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
	Summary        SessionSummary   `json:"summary"`
	Preview        SessionPreview   `json:"preview"`
	Events         []ImportantEvent `json:"events"`
}

func toRemoteSessionView(s *RemoteSession) RemoteSessionView {
	summary := s.Summary
	preview := s.Preview
	events := append([]ImportantEvent(nil), s.Events...)

	sanitizeSessionSummary(&summary)
	sanitizeSessionPreview(&preview)
	sanitizeImportantEvents(events)

	return RemoteSessionView{
		ID:             s.ID,
		Tool:           s.Tool,
		Title:          s.Title,
		LaunchSource:   string(normalizeRemoteLaunchSource(s.LaunchSource)),
		ProjectPath:    s.ProjectPath,
		WorkspacePath:  s.WorkspacePath,
		WorkspaceRoot:  s.WorkspaceRoot,
		WorkspaceMode:  s.WorkspaceMode,
		WorkspaceIsGit: s.WorkspaceIsGit,
		ModelID:        s.ModelID,
		Status:         s.Status,
		PID:            s.PID,
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
		Summary:        summary,
		Preview:        preview,
		Events:         events,
	}
}

func sanitizeSessionSummary(summary *SessionSummary) {
	if summary == nil {
		return
	}

	summary.SessionID = sanitizeRemoteText(summary.SessionID)
	summary.MachineID = sanitizeRemoteText(summary.MachineID)
	summary.Tool = sanitizeRemoteText(summary.Tool)
	summary.Title = sanitizeRemoteText(summary.Title)
	summary.Source = sanitizeRemoteText(summary.Source)
	summary.Status = sanitizeRemoteText(summary.Status)
	summary.Severity = sanitizeRemoteText(summary.Severity)
	summary.CurrentTask = sanitizeRemoteText(summary.CurrentTask)
	summary.ProgressSummary = sanitizeRemoteText(summary.ProgressSummary)
	summary.LastResult = sanitizeRemoteText(summary.LastResult)
	summary.SuggestedAction = sanitizeRemoteText(summary.SuggestedAction)
	summary.LastCommand = sanitizeRemoteText(summary.LastCommand)

	for i := range summary.ImportantFiles {
		summary.ImportantFiles[i] = sanitizeRemoteText(summary.ImportantFiles[i])
	}
}

func sanitizeSessionPreview(preview *SessionPreview) {
	if preview == nil {
		return
	}

	preview.SessionID = sanitizeRemoteText(preview.SessionID)
	for i := range preview.PreviewLines {
		preview.PreviewLines[i] = sanitizeRemoteText(preview.PreviewLines[i])
	}
}

func sanitizeImportantEvents(events []ImportantEvent) {
	for i := range events {
		events[i].EventID = sanitizeRemoteText(events[i].EventID)
		events[i].SessionID = sanitizeRemoteText(events[i].SessionID)
		events[i].MachineID = sanitizeRemoteText(events[i].MachineID)
		events[i].Type = sanitizeRemoteText(events[i].Type)
		events[i].Severity = sanitizeRemoteText(events[i].Severity)
		events[i].Title = sanitizeRemoteText(events[i].Title)
		events[i].Summary = sanitizeRemoteText(events[i].Summary)
		events[i].RelatedFile = sanitizeRemoteText(events[i].RelatedFile)
		events[i].Command = sanitizeRemoteText(events[i].Command)
	}
}

func sanitizeRemoteText(value string) string {
	if value == "" {
		return ""
	}

	if !utf8.ValidString(value) {
		value = strings.ToValidUTF8(value, "?")
	}

	value = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		default:
			return r
		}
	}, value)

	value = strings.TrimSpace(value)
	value = strings.Join(strings.Fields(value), " ")
	return value
}

func (a *App) GetRemoteConnectionStatus() RemoteConnectionStatus {
	cfg, err := a.LoadConfig()
	if err != nil {
		return RemoteConnectionStatus{
			LastError: err.Error(),
		}
	}

	status := RemoteConnectionStatus{
		Enabled:   cfg.RemoteEnabled,
		HubURL:    cfg.RemoteHubURL,
		MachineID: cfg.RemoteMachineID,
	}

	if a.remoteSessions != nil {
		status.SessionCount = len(a.remoteSessions.List())
	}

	if a.remoteSessions != nil && a.remoteSessions.hubClient != nil {
		status.Connected = a.remoteSessions.hubClient.IsConnected()
		status.LastError = a.remoteSessions.hubClient.LastError()
	}

	return status
}

func (a *App) ListRemoteToolMetadata() []RemoteToolMetadataView {
	return listRemoteToolMetadataForApp(a)
}

func (a *App) emitRemoteStateChanged() {
	a.emitEvent("remote-state-changed")
}

func (a *App) GetRemoteClaudeReadiness(projectDir string, useProxy bool) RemoteClaudeReadiness {
	return a.CheckRemoteClaudeReadiness(projectDir, useProxy)
}

func (a *App) GetRemoteToolReadiness(toolName, projectDir string, useProxy bool) RemoteToolReadiness {
	return a.CheckRemoteToolReadiness(toolName, projectDir, useProxy)
}

func (a *App) GetRemotePTYProbe() RemotePTYProbeResult {
	return a.CheckRemotePTYProbe()
}

func (a *App) GetLastRemoteSmokeReport() (RemoteSmokeSnapshot, error) {
	root, err := os.Getwd()
	if err != nil || root == "" {
		root = "."
	}
	path := filepath.Join(root, ".last_remote_demo.json")
	snapshot := RemoteSmokeSnapshot{
		Exists: false,
		Path:   path,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return snapshot, nil
		}
		return snapshot, err
	}

	var report RemoteSmokeReport
	if err := json.Unmarshal(data, &report); err != nil {
		return snapshot, err
	}

	snapshot.Exists = true
	snapshot.Report = &report
	return snapshot, nil
}

func (a *App) GetRemoteClaudeLaunchProbe(projectDir string, useProxy bool) RemoteClaudeLaunchProbeResult {
	return a.CheckRemoteClaudeLaunchProbe(projectDir, useProxy)
}

func (a *App) GetRemoteToolLaunchProbe(toolName, projectDir string, useProxy bool) RemoteToolLaunchProbeResult {
	return a.CheckRemoteToolLaunchProbe(toolName, projectDir, useProxy)
}

func (a *App) RunRemoteClaudeSmoke(projectDir string, useProxy bool) (RemoteSmokeReport, error) {
	return a.RunRemoteToolSmoke("claude", projectDir, useProxy)
}

func (a *App) RunRemoteToolSmoke(toolName, projectDir string, useProxy bool) (RemoteSmokeReport, error) {
	toolName = normalizeRemoteToolName(toolName)
	if projectDir == "" {
		projectDir = a.GetCurrentProjectPath()
	}

	report := RemoteSmokeReport{
		Tool:        toolName,
		ProjectPath: projectDir,
		UseProxy:    useProxy,
		Connection:  a.GetRemoteConnectionStatus(),
		Readiness:   a.GetRemoteToolReadiness(toolName, projectDir, useProxy),
	}

	cfg, err := a.LoadConfig()
	if err != nil {
		return report, err
	}
	if cfg.RemoteEmail == "" {
		return report, fmt.Errorf("remote email is required before running full smoke")
	}

	if cfg.RemoteMachineID == "" || cfg.RemoteMachineToken == "" {
		activation, err := a.ActivateRemote(cfg.RemoteEmail, "")
		if err != nil {
			return report, err
		}
		report.Activation = &activation
	}

	report.Connection = a.GetRemoteConnectionStatus()
	report.Readiness = a.GetRemoteToolReadiness(toolName, projectDir, useProxy)

	ptyProbe := a.GetRemotePTYProbe()
	report.PTYProbe = &ptyProbe

	launchProbe := a.GetRemoteToolLaunchProbe(toolName, projectDir, useProxy)
	report.LaunchProbe = &launchProbe
	if !launchProbe.Ready {
		return report, fmt.Errorf("%s launch probe failed: %s", toolName, launchProbe.Message)
	}

	session, err := a.StartRemoteSession(toolName, projectDir, useProxy)
	report.StartedSession = &session
	if err != nil {
		return report, err
	}

	visibility, verifyErr := verifyRemoteHubVisibility(a, report, 20*time.Second)
	report.HubVisibility = &visibility
	if verifyErr != nil {
		return report, verifyErr
	}

	return report, nil
}

func (a *App) ListRemoteSessions() []RemoteSessionView {
	if a.remoteSessions == nil {
		return []RemoteSessionView{}
	}

	sessions := a.remoteSessions.List()
	out := make([]RemoteSessionView, 0, len(sessions))
	for _, s := range sessions {
		if s == nil {
			continue
		}
		view := toRemoteSessionView(s)
		a.log(fmt.Sprintf("[remote-list] session %s: status=%s, preview_lines=%d", view.ID, view.Status, len(view.Preview.PreviewLines)))
		out = append(out, view)
	}
	return out
}

func (a *App) StartRemoteClaudeSession(projectDir string, useProxy bool) (RemoteSessionView, error) {
	return a.StartRemoteSession("claude", projectDir, useProxy)
}

func (a *App) StartRemoteSession(toolName, projectDir string, useProxy bool) (RemoteSessionView, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return RemoteSessionView{}, err
	}
	if !cfg.RemoteEnabled {
		return RemoteSessionView{}, fmt.Errorf("remote mode is disabled")
	}

	if projectDir == "" {
		projectDir = a.GetCurrentProjectPath()
	}

	if a.remoteSessions == nil {
		a.remoteSessions = NewRemoteSessionManager(a)
	}

	hubClient := a.remoteSessions.hubClient
	if hubClient == nil {
		hubClient = NewRemoteHubClient(a, a.remoteSessions)
		a.remoteSessions.SetHubClient(hubClient)
	}

	if cfg.RemoteHubURL != "" && cfg.RemoteMachineID != "" && cfg.RemoteMachineToken != "" && !hubClient.IsConnected() {
		if err := hubClient.Connect(); err != nil {
			a.log("remote hub connect before launch failed: " + err.Error())
		}
	}

	spec, err := a.buildRemoteLaunchSpec(toolName, cfg, false, false, "", projectDir, useProxy)
	if err != nil {
		return RemoteSessionView{}, err
	}

	session, err := a.remoteSessions.Create(spec)
	if err != nil && session == nil {
		return RemoteSessionView{}, err
	}

	a.emitRemoteStateChanged()
	if session == nil {
		return RemoteSessionView{}, err
	}
	return toRemoteSessionView(session), err
}

func (a *App) StartRemoteHandoffSession(toolName, projectDir string, useProxy bool) (RemoteSessionView, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return RemoteSessionView{}, err
	}
	if !cfg.RemoteEnabled {
		return RemoteSessionView{}, fmt.Errorf("remote mode is disabled")
	}

	if projectDir == "" {
		projectDir = a.GetCurrentProjectPath()
	}

	if a.remoteSessions == nil {
		a.remoteSessions = NewRemoteSessionManager(a)
	}

	hubClient := a.remoteSessions.hubClient
	if hubClient == nil {
		hubClient = NewRemoteHubClient(a, a.remoteSessions)
		a.remoteSessions.SetHubClient(hubClient)
	}

	if cfg.RemoteHubURL != "" && cfg.RemoteMachineID != "" && cfg.RemoteMachineToken != "" && !hubClient.IsConnected() {
		if err := hubClient.Connect(); err != nil {
			a.log("remote hub connect before handoff failed: " + err.Error())
		}
	}

	spec, err := a.buildRemoteLaunchSpec(toolName, cfg, false, false, "", projectDir, useProxy)
	if err != nil {
		return RemoteSessionView{}, err
	}
	spec.LaunchSource = RemoteLaunchSourceHandoff

	session, err := a.remoteSessions.Create(spec)
	if err != nil && session == nil {
		return RemoteSessionView{}, err
	}

	a.emitRemoteStateChanged()
	if session == nil {
		return RemoteSessionView{}, err
	}
	return toRemoteSessionView(session), err
}

func (a *App) ReconnectRemoteHub() error {
	if a.remoteSessions == nil {
		a.remoteSessions = NewRemoteSessionManager(a)
	}

	hubClient := a.remoteSessions.hubClient
	if hubClient == nil {
		hubClient = NewRemoteHubClient(a, a.remoteSessions)
		a.remoteSessions.SetHubClient(hubClient)
	}

	_ = hubClient.Disconnect()
	return hubClient.Connect()
}

func (a *App) SendRemoteSessionInput(sessionID, text string) error {
	if a.remoteSessions == nil {
		return ErrRemoteSessionsUnavailable
	}
	a.log(fmt.Sprintf("[remote-input] writing to session %s: %q", sessionID, text))
	err := a.remoteSessions.WriteInput(sessionID, text)
	if err != nil {
		a.log(fmt.Sprintf("[remote-input] write failed for session %s: %v", sessionID, err))
	} else {
		a.log(fmt.Sprintf("[remote-input] write succeeded for session %s", sessionID))
	}
	return err
}

func (a *App) InterruptRemoteSession(sessionID string) error {
	if a.remoteSessions == nil {
		return ErrRemoteSessionsUnavailable
	}
	return a.remoteSessions.Interrupt(sessionID)
}

func (a *App) KillRemoteSession(sessionID string) error {
	if a.remoteSessions == nil {
		return ErrRemoteSessionsUnavailable
	}
	return a.remoteSessions.Kill(sessionID)
}
