package agentruntime

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// ToolTextResult classifies a legacy text-only host tool response using the
// markers shared by the GUI and headless executor. New typed adapters should
// return an explicit outcome; this helper exists for the remaining text
// boundary so a GUI-only wording change cannot silently alter srv retries.
func ToolTextResult(text string) agent.ToolExecutionResult {
	outcome := agent.ToolExecutionOutcomeOK
	if strings.Contains(text, "命令超时") {
		outcome = agent.ToolExecutionOutcomeTimeout
	} else if ToolTextFailure(text) {
		outcome = agent.ToolExecutionOutcomeError
	}
	return agent.ToolExecutionResult{Result: text, Outcome: outcome}
}

// ToolTextFailure reports stable, transport-neutral failure markers emitted by
// legacy host adapters. Prefix-style markers are examined on the first line
// only, because such wording may legitimately occur in later output of a
// successful command. A small set of unambiguous markers (interruptions,
// MCP/PDF generator failures, "执行失败"/"参数解析失败") is matched against the
// whole text: producers emit them only on failure, and matching them on the
// first line only would miss wrapped/prefixed error reports.
func ToolTextFailure(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	firstLine, _, _ := strings.Cut(trimmed, "\n")
	lower := strings.ToLower(firstLine)
	if strings.HasPrefix(lower, "error:") || strings.HasPrefix(firstLine, "错误:") || strings.HasPrefix(firstLine, "[错误]") ||
		strings.HasPrefix(firstLine, "目标管理器未初始化") || strings.HasPrefix(firstLine, "任务管理器未初始化") ||
		strings.HasPrefix(firstLine, "long-term memory is not initialized") || strings.HasPrefix(firstLine, "未知 task action") ||
		strings.HasPrefix(firstLine, "未知 goal action") || strings.HasPrefix(firstLine, "创建目标失败") ||
		strings.HasPrefix(firstLine, "未知 SSH") || strings.HasPrefix(firstLine, "unknown memory action") ||
		strings.HasPrefix(firstLine, "save memory failed") || strings.HasPrefix(firstLine, "delete memory failed") ||
		strings.HasPrefix(firstLine, "memory candidate rejected") || strings.HasPrefix(firstLine, "derived surgery failed") ||
		strings.HasPrefix(firstLine, "unsupported derived surgery") {
		return true
	}
	// Registered-tool handler markers (MCP envelope, parse failures, panics).
	// Sharing this vocabulary with RegisteredToolTextOutcome is the point: GUI
	// and headless hosts must classify the same text identically.
	if strings.HasPrefix(lower, "[error]") || strings.HasPrefix(lower, "[mcp error]") ||
		strings.HasPrefix(lower, "mcp call failed:") || strings.HasPrefix(lower, "mcp 调用失败") ||
		strings.HasPrefix(lower, "mcp 调用被拒绝") || strings.HasPrefix(lower, "failed:") ||
		strings.HasPrefix(lower, "failure:") || strings.HasPrefix(lower, "argument parse failed:") ||
		strings.HasPrefix(lower, "arguments json") || strings.HasPrefix(lower, "unknown tool:") ||
		strings.HasPrefix(lower, "tool execution panicked:") || strings.HasPrefix(lower, "缺少 server_id") ||
		strings.HasPrefix(lower, "本地 mcp manager") || strings.HasPrefix(lower, "mcp registry") {
		return true
	}
	if _, failed := agent.DocumentReadFailure(text); failed {
		return true
	}
	switch {
	case strings.HasPrefix(firstLine, "缺少 "),
		strings.HasPrefix(firstLine, "文件不存在或无法访问"),
		strings.HasPrefix(firstLine, "读取失败"),
		strings.HasPrefix(firstLine, "missing pattern"),
		strings.HasPrefix(firstLine, "invalid regex"),
		strings.HasPrefix(firstLine, "search cancelled"),
		strings.HasPrefix(firstLine, "Glob cancelled"),
		strings.HasPrefix(firstLine, "data 参数格式错误"),
		strings.HasPrefix(firstLine, "missing query parameter"),
		strings.HasPrefix(firstLine, "missing content parameter"),
		strings.HasPrefix(firstLine, "missing id parameter"),
		strings.HasPrefix(firstLine, "cannot combine "),
		strings.HasPrefix(firstLine, "pagination not available"),
		strings.HasPrefix(firstLine, "scroll sessions not available"),
		strings.HasPrefix(firstLine, "未知 "),
		strings.HasPrefix(firstLine, "发送失败"),
		strings.HasPrefix(firstLine, "定时任务管理器未初始化"),
		strings.HasPrefix(firstLine, "请提供 "),
		strings.Contains(firstLine, "必须在 "):
		return true
	}
	return strings.Contains(firstLine, "失败:") ||
		strings.Contains(firstLine, " in this isolated conversation") ||
		strings.Contains(firstLine, " 是目录，请使用") ||
		strings.HasPrefix(firstLine, "start_line=") || strings.HasPrefix(firstLine, "end_line=") ||
		strings.Contains(text, "tool execution interrupted:") || strings.Contains(strings.ToLower(text), "context canceled") ||
		strings.Contains(text, "工具执行异常") || strings.Contains(text, "未知工具") || strings.Contains(text, "无法转发") ||
		strings.Contains(text, "Could not forward") || strings.Contains(text, "Failed to save") ||
		strings.Contains(text, "执行失败") || strings.Contains(text, "参数解析失败") ||
		strings.Contains(text, "未找到可用的中文字体") || strings.Contains(text, "无法生成 PDF") ||
		strings.Contains(text, "PDF 生成失败:") || strings.Contains(text, "缺少 content 参数") ||
		(strings.Contains(text, "保存") && strings.Contains(text, "失败"))
}
