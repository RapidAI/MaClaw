package main

import "fmt"

func (h *IMMessageHandler) runCreateSessionTool(args map[string]interface{}) string {
	if hint := h.checkSessionTaskGuard(); hint != "" {
		return hint
	}

	tool, _ := args["tool"].(string)
	projectPath, _ := args["project_path"].(string)
	projectID, _ := args["project_id"].(string)
	provider, _ := args["provider"].(string)

	contextResolution := h.resolveCreateSessionContext(tool, projectPath)
	if contextResolution.Error != "" {
		return contextResolution.Error
	}
	tool = contextResolution.Tool
	projectPath = contextResolution.ProjectPath
	hints := append([]string{}, contextResolution.Hints...)

	cfg, cfgErr := h.loadConfig()
	if cfgErr != nil {
		return fmt.Sprintf("鍔犺浇閰嶇疆澶辫触: %s", cfgErr.Error())
	}
	projectSelection := resolveCreateSessionProjectSelection(cfg, projectID, projectPath)
	if projectSelection.Error != "" {
		return projectSelection.Error
	}
	projectPath = projectSelection.ProjectPath
	hints = append(hints, projectSelection.Hints...)

	precheckResolution := runCreateSessionPrecheck(h.sessionPrecheck, tool, projectPath)
	if precheckResolution.Error != "" {
		return precheckResolution.Error
	}
	hints = append(hints, precheckResolution.Hints...)

	providerResolution := resolveCreateSessionProvider(cfg, tool, provider)
	if providerResolution.Error != "" {
		return providerResolution.Error
	}
	hints = append(hints, providerResolution.Hints...)
	resolvedProvider := providerResolution.ResolvedProvider

	hints = append(hints, renderCreateSessionLaunchBanner(tool, resolvedProvider, projectPath))

	resumeSessionID, _ := args["resume_session_id"].(string)

	starter := h.ensureCreateSessionStarter()
	if starter == nil {
		return "浼氳瘽鍚姩鍣ㄦ湭鍒濆鍖?"
	}
	startReq := h.buildCreateSessionStartRequest(tool, projectID, projectPath, resolvedProvider, resumeSessionID)
	startResult, err := starter.Start(startReq)
	if err != nil {
		return renderCreateSessionStartError(err, tool, projectPath)
	}
	view := startResult.View
	if len(startResult.Hints) > 0 {
		hints = append(hints, startResult.Hints...)
	}

	h.handleCreateSessionStarted(view.ID)

	return renderCreateSessionStartedMessage(hints, view.ID)
}
