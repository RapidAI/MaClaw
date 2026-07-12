package main

import (
	"fmt"
	"strings"
)

type createSessionPrecheckResolution struct {
	Hints []string
	Error string
}

func runCreateSessionPrecheck(precheck *SessionPrecheck, toolName, projectPath string) createSessionPrecheckResolution {
	if precheck == nil {
		return createSessionPrecheckResolution{}
	}
	return resolveCreateSessionPrecheckResult(precheck.Check(toolName, projectPath), toolName)
}

func resolveCreateSessionPrecheckResult(result PrecheckResult, toolName string) createSessionPrecheckResolution {
	var hints []string
	if !result.ToolReady {
		hints = append(hints, fmt.Sprintf("工具预检未通过: %s", result.ToolHint))
	}
	if !result.ProjectReady {
		hints = append(hints, "项目路径不存在或无法访问")
	}
	if !result.ModelReady {
		hints = append(hints, fmt.Sprintf("模型预检未通过: %s", result.ModelHint))
	}
	if result.AllPassed {
		hints = append(hints, "环境预检全部通过")
	}

	resolution := createSessionPrecheckResolution{Hints: hints}
	if !result.ToolReady {
		resolution.Error = strings.Join(hints, "\n") + "\n工具未安装，无法创建会话。请先在桌面端安装 " + toolName + " 后重试。"
	}
	return resolution
}
