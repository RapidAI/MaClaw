package main

import (
	"fmt"
	"strings"
)

// CodingSessionStartRequest describes the normalized inputs for creating a
// remote coding session from IM tools, skills, or other internal callers.
type CodingSessionStartRequest struct {
	Tool            string
	ProjectID       string
	ProjectPath     string
	Provider        string
	ResumeSessionID string
	LaunchSource    RemoteLaunchSource

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
	if s == nil || s.app == nil {
		return CodingSessionStartResult{}, fmt.Errorf("app not initialized")
	}
	toolName := strings.TrimSpace(req.Tool)
	if toolName == "" {
		return CodingSessionStartResult{}, fmt.Errorf("missing tool parameter")
	}

	cfg, err := s.app.LoadConfig()
	if err != nil {
		return CodingSessionStartResult{}, err
	}

	resolvedProjectPath := strings.TrimSpace(req.ProjectPath)
	projectID := strings.TrimSpace(req.ProjectID)
	var hints []string
	if projectID != "" {
		var found bool
		for _, p := range cfg.Projects {
			if p.Id == projectID {
				resolvedProjectPath = p.Path
				found = true
				hints = append(hints, fmt.Sprintf("📁 通过项目 ID 解析: %s → %s", projectID, p.Path))
				break
			}
		}
		if !found {
			var available []string
			for _, p := range cfg.Projects {
				available = append(available, fmt.Sprintf("%s(%s)", p.Id, p.Name))
			}
			if len(available) == 0 {
				return CodingSessionStartResult{}, fmt.Errorf("项目 ID %q 未找到，当前没有已配置的项目", projectID)
			}
			return CodingSessionStartResult{}, fmt.Errorf("项目 ID %q 未找到，可用项目: %s", projectID, strings.Join(available, ", "))
		}
	}

	toolCfg, err := remoteToolConfig(cfg, toolName)
	if err != nil {
		return CodingSessionStartResult{}, fmt.Errorf("获取工具配置失败: %w", err)
	}
	resolver := &ProviderResolver{}
	resolveResult, err := resolver.Resolve(toolCfg, req.Provider)
	if err != nil {
		return CodingSessionStartResult{}, err
	}
	if resolveResult.Fallback {
		hints = append(hints, fmt.Sprintf("⚡ 服务商已降级: %s → %s", resolveResult.OriginalName, resolveResult.Provider.ModelName))
	}
	resolvedProvider := resolveResult.Provider.ModelName

	launchSource := req.LaunchSource
	if launchSource == "" {
		launchSource = RemoteLaunchSourceAI
	}
	resumeSessionID := strings.TrimSpace(req.ResumeSessionID)
	resumeSource := ""
	if resumeSessionID != "" {
		resumeSource = "explicit"
	}

	view, err := s.app.StartRemoteSessionForProject(RemoteStartSessionRequest{
		Tool:            toolName,
		ProjectID:       projectID,
		ProjectPath:     resolvedProjectPath,
		Provider:        resolvedProvider,
		LaunchSource:    launchSource,
		ResumeSessionID: resumeSessionID,
	})
	if err != nil {
		return CodingSessionStartResult{}, err
	}

	result := CodingSessionStartResult{
		View:                view,
		ResolvedProjectPath: strings.TrimSpace(view.ProjectPath),
		ResolvedProvider:    resolvedProvider,
		ResumeApplied:       resumeSessionID != "",
		ResumeSource:        resumeSource,
		Hints:               hints,
	}
	if result.ResolvedProjectPath == "" {
		result.ResolvedProjectPath = strings.TrimSpace(resolvedProjectPath)
	}

	if s.app.aiTrace != nil && strings.TrimSpace(req.ParentRunID) != "" && strings.TrimSpace(view.RunID) != "" {
		s.app.aiTrace.LinkRuns(req.ParentRunID, view.RunID)
		s.app.aiTrace.AppendEvent(req.ParentRunID, TraceEvent{
			Kind:        "remote_session.created",
			Severity:    "info",
			Title:       "Remote session created",
			Summary:     fmt.Sprintf("session=%s tool=%s project=%s", view.ID, view.Tool, result.ResolvedProjectPath),
			Command:     "create_session",
			ProjectPath: result.ResolvedProjectPath,
			CreatedAt:   traceNowMillis(),
		})
	}

	return result, nil
}
