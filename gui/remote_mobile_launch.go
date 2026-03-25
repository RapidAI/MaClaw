package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type RemoteLaunchProject struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Path          string `json:"path"`
	UseProxy      bool   `json:"use_proxy"`
	YoloMode      bool   `json:"yolo_mode"`
	AdminMode     bool   `json:"admin_mode"`
	PythonProject bool   `json:"python_project"`
	PythonEnv     string `json:"python_env"`
	IsCurrent     bool   `json:"is_current"`
}

type RemoteStartSessionRequest struct {
	Tool            string             `json:"tool"`
	ProjectID       string             `json:"project_id,omitempty"`
	ProjectPath     string             `json:"project_path,omitempty"`
	Provider        string             `json:"provider,omitempty"`
	UseProxy        *bool              `json:"use_proxy,omitempty"`
	YoloMode        *bool              `json:"yolo_mode,omitempty"`
	AdminMode       *bool              `json:"admin_mode,omitempty"`
	PythonEnv       string             `json:"python_env,omitempty"`
	LaunchSource    RemoteLaunchSource `json:"launch_source,omitempty"`
	ResumeSessionID string             `json:"resume_session_id,omitempty"`
}

func (a *App) ListRemoteLaunchProjects() ([]RemoteLaunchProject, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}

	projects := make([]RemoteLaunchProject, 0, len(cfg.Projects))
	for _, project := range cfg.Projects {
		projectPath := strings.TrimSpace(project.Path)
		if projectPath == "" {
			continue
		}
		projects = append(projects, RemoteLaunchProject{
			ID:            strings.TrimSpace(project.Id),
			Name:          strings.TrimSpace(project.Name),
			Path:          filepath.Clean(projectPath),
			UseProxy:      project.UseProxy,
			YoloMode:      project.YoloMode,
			AdminMode:     project.AdminMode,
			PythonProject: project.PythonProject,
			PythonEnv:     strings.TrimSpace(project.PythonEnv),
			IsCurrent:     project.Id == cfg.CurrentProject,
		})
	}

	return projects, nil
}

func (a *App) StartRemoteSessionForProject(req RemoteStartSessionRequest) (RemoteSessionView, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return RemoteSessionView{}, err
	}
	if !cfg.RemoteEnabled && normalizeRemoteLaunchSource(req.LaunchSource) == RemoteLaunchSourceDesktop {
		return RemoteSessionView{}, fmt.Errorf("remote mode is disabled")
	}

	project, err := resolveRemoteProject(cfg, req.ProjectID, req.ProjectPath)
	if err != nil {
		return RemoteSessionView{}, err
	}

	tool := normalizeRemoteToolName(req.Tool)
	if !remoteToolSupported(tool) {
		return RemoteSessionView{}, fmt.Errorf("tool %q does not support remote mode", tool)
	}
	if a.remoteSessions == nil {
		a.ensureRemoteInfra()
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

	useProxy := project.UseProxy
	if req.UseProxy != nil {
		useProxy = *req.UseProxy
	}
	yoloMode := project.YoloMode
	if req.YoloMode != nil {
		yoloMode = *req.YoloMode
	}
	adminMode := project.AdminMode
	if req.AdminMode != nil {
		adminMode = *req.AdminMode
	}
	pythonEnv := ""
	if project.PythonProject {
		pythonEnv = strings.TrimSpace(project.PythonEnv)
	}
	if strings.TrimSpace(req.PythonEnv) != "" {
		pythonEnv = strings.TrimSpace(req.PythonEnv)
	}

	spec, err := a.buildRemoteLaunchSpec(tool, cfg, yoloMode, adminMode, pythonEnv, project.Path, useProxy, req.Provider)
	if err != nil {
		return RemoteSessionView{}, err
	}
	spec.LaunchSource = RemoteLaunchSourceMobile
	if req.LaunchSource != "" {
		spec.LaunchSource = req.LaunchSource
	}
	// Pass through resume session ID for --resume support.
	if req.ResumeSessionID != "" {
		spec.ResumeSessionID = req.ResumeSessionID
	}

	session, err := a.remoteSessions.Create(spec)
	if err != nil && session == nil {
		return RemoteSessionView{}, err
	}

	// Mode change detection: if there's an existing active session for the
	// same project with a different fingerprint, log a warning and attach
	// a mode_change event so the user knows the parameters changed.
	if session != nil {
		newFP := session.LaunchFP
		for _, existing := range a.remoteSessions.List() {
			if existing.ID == session.ID {
				continue
			}
			// Match by any path: original project path OR resolved workspace path.
			sameProject := existing.ProjectPath == session.ProjectPath ||
				(existing.WorkspacePath != "" && existing.WorkspacePath == session.WorkspacePath) ||
				(existing.WorkspaceRoot != "" && existing.WorkspaceRoot == session.WorkspaceRoot)
			if !sameProject {
				continue
			}
			if !isActiveRemoteSessionStatus(existing.Status) {
				continue
			}
			existing.mu.RLock()
			oldFP := existing.LaunchFP
			existing.mu.RUnlock()
			if oldFP != "" && oldFP != newFP {
				a.log(fmt.Sprintf("[mode-change] project=%s old_session=%s new_session=%s old_fp=%s new_fp=%s",
					session.ProjectPath, existing.ID, session.ID, oldFP, newFP))
				evt := ImportantEvent{
					EventID:   fmt.Sprintf("evt_%d_mode_change", time.Now().UnixNano()),
					SessionID: session.ID,
					Type:      "mode_change",
					Severity:  "warning",
					Title:     "Session parameters changed",
					Summary:   fmt.Sprintf("New session uses different parameters than active session %s", existing.ID),
					CreatedAt: time.Now().Unix(),
				}
				session.mu.Lock()
				session.Events = append(session.Events, evt)
				session.mu.Unlock()
				if a.remoteSessions.hubClient != nil {
					_ = a.remoteSessions.hubClient.SendImportantEvent(evt)
				}
			}
			break
		}
	}

	a.emitRemoteStateChanged()
	if session == nil {
		return RemoteSessionView{}, err
	}
	return toRemoteSessionView(session), err
}

func resolveRemoteProject(cfg AppConfig, projectID string, projectPath string) (ProjectConfig, error) {
	projectID = strings.TrimSpace(projectID)
	projectPath = strings.TrimSpace(projectPath)

	if projectID != "" {
		for _, project := range cfg.Projects {
			if strings.TrimSpace(project.Id) == projectID {
				project.Path = filepath.Clean(strings.TrimSpace(project.Path))
				return project, nil
			}
		}
	}

	if projectPath != "" {
		cleanTarget := filepath.Clean(projectPath)
		for _, project := range cfg.Projects {
			if filepath.Clean(strings.TrimSpace(project.Path)) == cleanTarget {
				project.Path = cleanTarget
				return project, nil
			}
		}
	}

	if cfg.CurrentProject != "" {
		for _, project := range cfg.Projects {
			if strings.TrimSpace(project.Id) == strings.TrimSpace(cfg.CurrentProject) {
				project.Path = filepath.Clean(strings.TrimSpace(project.Path))
				return project, nil
			}
		}
	}

	if len(cfg.Projects) > 0 && strings.TrimSpace(cfg.Projects[0].Path) != "" {
		project := cfg.Projects[0]
		project.Path = filepath.Clean(strings.TrimSpace(project.Path))
		return project, nil
	}

	return ProjectConfig{}, fmt.Errorf("no launchable project found")
}
