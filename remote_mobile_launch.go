package main

import (
	"fmt"
	"path/filepath"
	"strings"
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
	Tool        string `json:"tool"`
	ProjectID   string `json:"project_id,omitempty"`
	ProjectPath string `json:"project_path,omitempty"`
	UseProxy    *bool  `json:"use_proxy,omitempty"`
	YoloMode    *bool  `json:"yolo_mode,omitempty"`
	AdminMode   *bool  `json:"admin_mode,omitempty"`
	PythonEnv   string `json:"python_env,omitempty"`
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
	if !cfg.RemoteEnabled {
		return RemoteSessionView{}, fmt.Errorf("remote mode is disabled")
	}

	project, err := resolveRemoteProject(cfg, req.ProjectID, req.ProjectPath)
	if err != nil {
		return RemoteSessionView{}, err
	}

	tool := normalizeRemoteToolName(req.Tool)
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

	spec, err := a.buildRemoteLaunchSpec(tool, cfg, yoloMode, adminMode, pythonEnv, project.Path, useProxy)
	if err != nil {
		return RemoteSessionView{}, err
	}
	spec.LaunchSource = RemoteLaunchSourceMobile

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
