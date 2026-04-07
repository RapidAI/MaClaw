package remote

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// RemoteLaunchProject 描述一个可启动远程会话的项目。
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

// RemoteStartSessionRequest 描述启动远程会话的请求。
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

// ResolveRemoteProject resolves a project from config by ID, path, or current project.
func ResolveRemoteProject(projects []corelib.ProjectConfig, currentProject string, projectID string, projectPath string) (corelib.ProjectConfig, error) {
	projectID = strings.TrimSpace(projectID)
	projectPath = strings.TrimSpace(projectPath)

	if projectID != "" {
		for _, project := range projects {
			if strings.TrimSpace(project.Id) == projectID {
				project.Path = filepath.Clean(strings.TrimSpace(project.Path))
				return project, nil
			}
		}
	}

	if projectPath != "" {
		cleanTarget := filepath.Clean(projectPath)
		for _, project := range projects {
			if filepath.Clean(strings.TrimSpace(project.Path)) == cleanTarget {
				project.Path = cleanTarget
				return project, nil
			}
		}
	}

	if currentProject != "" {
		for _, project := range projects {
			if strings.TrimSpace(project.Id) == strings.TrimSpace(currentProject) {
				project.Path = filepath.Clean(strings.TrimSpace(project.Path))
				return project, nil
			}
		}
	}

	if len(projects) > 0 && strings.TrimSpace(projects[0].Path) != "" {
		project := projects[0]
		project.Path = filepath.Clean(strings.TrimSpace(project.Path))
		return project, nil
	}

	return corelib.ProjectConfig{}, fmt.Errorf("no launchable project found")
}
