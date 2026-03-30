package main

// Tool execution: dispatcher that routes tool calls to registered handlers.

import (
	"encoding/json"
	"fmt"
	"strings"
)

func (h *IMMessageHandler) executeTool(name, argsJSON string, onProgress ProgressCallback) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = fmt.Sprintf("工具执行异常: %v", r)
		}
	}()

	var args map[string]interface{}
	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return fmt.Sprintf("参数解析失败: %s", err.Error())
		}
	}
	if args == nil {
		args = map[string]interface{}{}
	}

	// --- SecurityFirewall check (Phase 2 upgrade) ---
	if h.firewall != nil {
		ctx := &SecurityCallContext{SessionID: "local"}
		allowed, reason := h.firewall.Check(name, args, ctx)
		if !allowed {
			return reason
		}
	}

	// --- Registry-based dispatch (unified path) ---
	if h.registry != nil {
		if tool, ok := h.registry.Get(name); ok {
			if tool.HandlerProg != nil {
				return tool.HandlerProg(args, onProgress)
			}
			if tool.Handler != nil {
				return tool.Handler(args)
			}
		}
	}

	return fmt.Sprintf("未知工具: %s", name)
}

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
		status := string(s.Status)
		task := s.Summary.CurrentTask
		waiting := s.Summary.WaitingForUser
		s.mu.RUnlock()
		b.WriteString(fmt.Sprintf("- [%s] 工具=%s 标题=%s 状态=%s", s.ID, s.Tool, s.Title, status))
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

// ---------------------------------------------------------------------------
// Non-coding task guard — prevents create_session for non-coding requests
// ---------------------------------------------------------------------------
