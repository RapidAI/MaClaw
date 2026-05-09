package main

// Tool execution: dispatcher that routes tool calls to registered handlers.

import (
	"encoding/json"
	"fmt"
	"strings"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func (h *IMMessageHandler) executeTool(name, argsJSON string, onProgress coretool.ProgressCallback) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = fmt.Sprintf("工具执行异常: %v", r)
		}
	}()

	var args map[string]interface{}
	if argsJSON != "" {
		// Sanitize LLM-returned JSON: strip code fences, fix over-escaped
		// quotes, remove single-quote wrappers. Small models (DeepSeek, Qwen)
		// frequently return malformed JSON that fails json.Unmarshal.
		cleaned := coretool.CleanToolArguments(argsJSON)
		if err := json.Unmarshal([]byte(cleaned), &args); err != nil {
			errMsg := fmt.Sprintf("参数解析失败: %s", err.Error())
			// When JSON is truncated (common with large write_file content),
			// provide actionable guidance to the LLM.
			if strings.Contains(err.Error(), "unexpected end of JSON input") && len(argsJSON) > 8000 {
				errMsg += "\n\n⚠️ 参数内容过长导致 JSON 被截断。请将内容拆分为多次调用：先用 write_file 写入前半部分，再用 write_file(mode=\"append\") 追加后半部分。单次 content 建议不超过 6000 字符。"
			}
			return errMsg
		}
	}
	if args == nil {
		args = map[string]interface{}{}
	}

	// Track file paths for steering fileMatch resolution.
	h.trackSteeringFileFromArgs(name, args)

	// --- Registry-based dispatch (unified path) ---
	if h.registry != nil {
		if tool, ok := h.registry.Get(name); ok {
			if h.emitRegisteredToolAgentViewIfNeeded(name, args) {
				return "Tool parameters are incomplete. A task panel form has been opened on the right."
			}
			if validationIssues := registeredToolValidateArgIssues(*tool, args); len(validationIssues) > 0 {
				if h.app != nil {
					if view := buildRegisteredToolAgentView(*tool, args, nil); view != nil {
						applyRegisteredToolFieldIssues(view, validationIssues)
						h.app.emitAgentView(view)
					}
				}
				return "Tool parameters need correction. A task panel form has been opened on the right."
			}
			securityCtx := &SecurityCallContext{SessionID: localSessionIDFromToolArgs(args)}
			if h.emitRegisteredToolApprovalAgentViewIfNeeded(name, args, securityCtx) {
				return "Tool execution needs approval. An approval panel has been opened on the right."
			}
			if h.firewall != nil {
				allowed, reason := h.firewall.Check(name, args, securityCtx)
				if !allowed {
					return reason
				}
			}
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

func localSessionIDFromToolArgs(args map[string]interface{}) string {
	if args == nil {
		return "local"
	}
	for _, key := range []string{"session_id", "browser_session_id"} {
		if value, ok := args[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "local"
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

// ---------------------------------------------------------------------------
// Non-coding task guard — prevents create_session for non-coding requests
// ---------------------------------------------------------------------------
