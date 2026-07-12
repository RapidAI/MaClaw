package main

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

type createSessionProjectIDResolution struct {
	ProjectPath string
	Hint        string
	Error       string
}

type createSessionProjectSelection struct {
	ProjectPath string
	Hints       []string
	Error       string
}

func resolveCreateSessionProjectSelection(cfg corelib.AppConfig, projectID, projectPath string) createSessionProjectSelection {
	selection := createSessionProjectSelection{ProjectPath: projectPath}
	resolvedProject := resolveCreateSessionProjectID(cfg, projectID)
	if resolvedProject.Error != "" {
		selection.Error = resolvedProject.Error
		return selection
	}
	if resolvedProject.ProjectPath != "" {
		selection.ProjectPath = resolvedProject.ProjectPath
	}
	if resolvedProject.Hint != "" {
		selection.Hints = append(selection.Hints, resolvedProject.Hint)
	}
	return selection
}

func resolveCreateSessionProjectID(cfg corelib.AppConfig, projectID string) createSessionProjectIDResolution {
	if projectID == "" {
		return createSessionProjectIDResolution{}
	}
	for _, p := range cfg.Projects {
		if p.Id == projectID {
			return createSessionProjectIDResolution{
				ProjectPath: p.Path,
				Hint:        fmt.Sprintf("通过项目 ID 解析: %s → %s", projectID, p.Path),
			}
		}
	}
	if len(cfg.Projects) == 0 {
		return createSessionProjectIDResolution{
			Error: fmt.Sprintf("项目 ID %q 未找到，当前没有已配置的项目", projectID),
		}
	}
	available := make([]string, 0, len(cfg.Projects))
	for _, p := range cfg.Projects {
		available = append(available, fmt.Sprintf("%s(%s)", p.Id, p.Name))
	}
	return createSessionProjectIDResolution{
		Error: fmt.Sprintf("项目 ID %q 未找到，可用项目: %s", projectID, strings.Join(available, ", ")),
	}
}
