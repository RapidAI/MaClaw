package main

import (
	"fmt"
	"os"
)

func (h *IMMessageHandler) ensureCreateSessionStarter() *CodingSessionStarter {
	starter := h.getSessionStarter()
	if starter != nil {
		return starter
	}
	h.ensureInteractionInfra()
	return h.getSessionStarter()
}

func (h *IMMessageHandler) buildCreateSessionStartRequest(toolName, projectID, projectPath, provider, resumeSessionID string) CodingSessionStartRequest {
	return CodingSessionStartRequest{
		Tool:               toolName,
		ProjectID:          projectID,
		ProjectPath:        projectPath,
		Provider:           provider,
		ResumeSessionID:    resumeSessionID,
		InjectResumePrompt: false,
		LaunchSource:       RemoteLaunchSourceAI,
		ParentRunID:        h.createSessionParentRunID(),
	}
}

func (h *IMMessageHandler) createSessionParentRunID() string {
	if h == nil {
		return ""
	}
	h.globalLoopMu.RLock()
	defer h.globalLoopMu.RUnlock()
	if h.currentLoopCtx == nil {
		return ""
	}
	return h.currentLoopCtx.RunID
}

func renderCreateSessionStartError(err error, toolName, projectPath string) string {
	errMsg := fmt.Sprintf("创建会话失败: %s", err.Error())
	errMsg += fmt.Sprintf("\n修复建议:\n- 检查 %s 是否已安装并可正常运行\n- 确认项目路径 %s 存在且可访问\n- 使用 list_providers 查看可用服务商配置", toolName, projectPath)
	return errMsg
}

func (h *IMMessageHandler) handleCreateSessionStarted(sessionID string) {
	h.app.ensureStartupFeedback()
	if h.startupFeedback != nil {
		h.startupFeedback.WatchStartup(sessionID, func(msg string) {
			// Progress messages are logged; in a real IM context the
			// onProgress callback from the agent loop would relay these.
			fmt.Fprintf(os.Stderr, "startup_feedback[%s]: %s\n", sessionID, msg)
		})
	}

	if h.app != nil && h.app.codeEventEmitter != nil {
		h.app.codeEventEmitter.EmitSessionStart(sessionID)
	}
}
