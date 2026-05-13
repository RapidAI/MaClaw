package main

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// userFacingToolProgressText returns a user-visible progress message for the
// given tool name. This is the primary entry point used by executeAgentLoopToolCall.
func userFacingToolProgressText(toolName string) string {
	return userFacingToolProgressTextWithArgs(toolName, "")
}

// userFacingToolProgressTextWithArgs generates a user-facing progress message
// for the given tool, optionally extracting context from the tool arguments
// (e.g. skill name from run_skill args). This provides dynamic feedback so
// users see exactly which tool/skill is being executed.
func userFacingToolProgressTextWithArgs(toolName, argsJSON string) string {
	switch toolName {
	case "craft_tool":
		return "🛠️ 正在生成并执行脚本，准备继续完成交付..."
	case "bash":
		return "🖥️ 正在执行命令..."
	case "run_skill", "manage_skill":
		if skillName := extractSkillNameFromArgs(argsJSON); skillName != "" {
			return fmt.Sprintf("🚀 正在执行 Skill「%s」...", skillName)
		}
		return "🚀 正在执行 Skill..."
	case "send_file":
		return "📎 正在整理并发送文件..."
	case "generate_pdf":
		return "📄 正在生成 PDF 文件..."
	case "web_search", "web_fetch":
		return "🔍 正在搜索网络..."
	case "read_file", "list_directory":
		return "📂 正在读取文件..."
	case "write_file", "edit_file", "edit_lines":
		return "✏️ 正在编辑文件..."
	case "memory":
		return "💾 正在访问记忆..."
	case "ssh":
		return "🔗 正在执行远程操作..."
	case "screenshot":
		return "📸 正在截取屏幕..."
	case "tts":
		return "🔊 正在生成语音..."
	case "browser":
		return "🌐 正在操作浏览器..."
	default:
		return fmt.Sprintf("⚙️ 正在执行 %s...", toolName)
	}
}

// extractSkillNameFromArgs extracts the "name" field from run_skill/manage_skill
// JSON arguments for display in progress messages. Returns empty string on failure.
// Uses lightweight string scanning instead of full JSON parse for performance
// (this is on the interactive progress path, called per tool invocation).
func extractSkillNameFromArgs(argsJSON string) string {
	if argsJSON == "" {
		return ""
	}
	// Look for "name":"value" or "name": "value" pattern.
	idx := strings.Index(argsJSON, `"name"`)
	if idx < 0 {
		return ""
	}
	rest := argsJSON[idx+6:]
	// Skip whitespace and colon.
	for len(rest) > 0 && (rest[0] == ' ' || rest[0] == ':' || rest[0] == '\t') {
		rest = rest[1:]
	}
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	rest = rest[1:]
	end := strings.IndexByte(rest, '"')
	if end < 0 || end > 60 {
		return ""
	}
	return rest[:end]
}

func shouldExposeToolInternalProgress(toolName string) bool {
	switch toolName {
	case "craft_tool", "bash", "run_skill":
		return true
	default:
		return false
	}
}

func filterUserFacingToolProgress(toolName, msg string) string {
	trimmed := strings.TrimSpace(msg)
	if trimmed == "" {
		return ""
	}
	if shouldExposeToolInternalProgress(toolName) {
		switch toolName {
		case "craft_tool":
			allowedPrefixes := []string{"🧠 ", "💾 ", "🚀 ", "📦 ", "⏳"}
			for _, prefix := range allowedPrefixes {
				if strings.HasPrefix(trimmed, prefix) {
					return trimmed
				}
			}
			return ""
		case "bash":
			if strings.HasPrefix(trimmed, "⏳") {
				return trimmed
			}
			return ""
		case "run_skill":
			allowedPrefixes := []string{"🚀 ", "⏳", "✅", "❌"}
			for _, prefix := range allowedPrefixes {
				if strings.HasPrefix(trimmed, prefix) {
					return trimmed
				}
			}
			return ""
		}
	}
	return ""
}

func filteredToolProgressCallback(toolName string, onProgress tool.ProgressCallback, debug bool) tool.ProgressCallback {
	if debug {
		return onProgress
	}
	if onProgress == nil || !shouldExposeToolInternalProgress(toolName) {
		return nil
	}
	return func(msg string) {
		if filtered := filterUserFacingToolProgress(toolName, msg); filtered != "" {
			onProgress(filtered)
		}
	}
}
