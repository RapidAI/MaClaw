package main

func (h *IMMessageHandler) handlePassthroughSlashCommand(text string) *IMAgentResponse {
	if h == nil || h.app == nil {
		return &IMAgentResponse{Error: "直通命令执行器未初始化。"}
	}
	return h.app.handlePassthroughSlashCommand(text, "")
}
