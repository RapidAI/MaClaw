package main

import (
	"fmt"
)

// CodingSessionStartRequest describes the normalized inputs for creating a
// remote coding session from IM tools, skills, or other internal callers.
type CodingSessionStartRequest struct {
	Tool               string
	ProjectID          string
	ProjectPath        string
	Provider           string
	ResumeSessionID    string
	InjectResumePrompt bool
	LaunchSource       RemoteLaunchSource

	ParentRunID string
	UserTask    string
}

// CodingSessionStartResult returns the created session plus normalized context
// that callers can surface in status messages or store for later recovery.
type CodingSessionStartResult struct {
	View                RemoteSessionView
	ResolvedProjectPath string
	ResolvedProvider    string
	ResumeApplied       bool
	ResumeSource        string
	Hints               []string
}

// CodingSessionStarter centralizes the high-level orchestration for creating
// remote coding sessions. It deliberately reuses App.StartRemoteSessionForProject
// as the single underlying launch path.
type CodingSessionStarter struct {
	app *App
}

func NewCodingSessionStarter(app *App) *CodingSessionStarter {
	return &CodingSessionStarter{app: app}
}

func (s *CodingSessionStarter) Start(req CodingSessionStartRequest) (CodingSessionStartResult, error) {
	return CodingSessionStartResult{}, fmt.Errorf("external coding session start is disabled; use the internal CodingSubAgent workflow instead")
}
