package main

import "fmt"

type createSessionContextResolution struct {
	Tool        string
	ProjectPath string
	Hints       []string
	Error       string
}

func (h *IMMessageHandler) resolveCreateSessionContext(toolName, projectPath string) createSessionContextResolution {
	resolution := createSessionContextResolution{
		Tool:        toolName,
		ProjectPath: projectPath,
	}

	if resolution.Tool == "" && h.contextResolver != nil {
		recommended, reason := h.contextResolver.ResolveTool(resolution.ProjectPath, "")
		if recommended != "" {
			resolution.Tool = recommended
			resolution.Hints = append(resolution.Hints, fmt.Sprintf("自动推荐工具: %s（%s）", resolution.Tool, reason))
		}
	}
	if resolution.Tool == "" {
		resolution.Error = "缺少 tool 参数，且无法自动推荐工具"
		return resolution
	}

	if resolution.ProjectPath == "" && h.contextResolver != nil {
		detected, reason := h.contextResolver.ResolveProject()
		if detected != "" {
			resolution.ProjectPath = detected
			resolution.Hints = append(resolution.Hints, fmt.Sprintf("自动检测项目: %s（%s）", resolution.ProjectPath, reason))
		}
	}
	return resolution
}
