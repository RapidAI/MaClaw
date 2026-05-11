package main

import (
	"fmt"
	"strings"
)

func (h *IMMessageHandler) toolListSessions() string {
	if h.manager == nil {
		return "会话管理器未初始化"
	}
	sessions := h.manager.List()
	if len(sessions) == 0 {
		return "当前没有活跃会话。"
	}
	var b strings.Builder
	for _, s := range sessions {
		s.mu.RLock()
		status := s.Status
		task := s.Summary.CurrentTask
		waiting := s.Summary.WaitingForUser
		modelName := s.ModelName
		s.mu.RUnlock()
		b.WriteString(fmt.Sprintf("- [%s] 工具=%s 标题=%s 状态=%s", s.ID, s.Tool, s.Title, status))
		if modelName != "" {
			b.WriteString(fmt.Sprintf(" 服务商=%s", modelName))
		}
		if task != "" {
			b.WriteString(fmt.Sprintf(" 任务=%s", task))
		}
		if waiting {
			b.WriteString(" [等待用户输入]")
		}
		b.WriteString("\n")
	}
	return b.String()
}
