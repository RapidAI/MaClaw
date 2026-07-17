package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// IM tool progress uses a status-card style so WeChat/QQ/Telegram bubbles
// are not confused with the final assistant reply.
//
// Example (zh):
//
//	【工具】执行命令
//	ls -la /tmp
//
// Example (en):
//
//	[Tool] Run command
//	ls -la /tmp

// userFacingToolProgressText returns a user-visible progress message for the
// given tool name. lang should be the GUI UI language (e.g. app.CurrentLanguage).
func userFacingToolProgressText(lang, toolName string) string {
	return userFacingToolProgressTextWithArgs(lang, toolName, "")
}

// userFacingToolProgressTextWithArgs generates a user-facing IM progress message
// for the given tool, extracting key context from the tool arguments.
// The result is always styled as a tool-status card (see formatIMToolStatus).
// Language follows the GUI interface language via i18n.
func userFacingToolProgressTextWithArgs(lang, toolName, argsJSON string) string {
	toolName = normalizeToolProgressName(toolName)
	if toolName == "" {
		return formatIMToolStatus(lang, i18n.T(i18n.MsgToolActionProcessing, lang), "")
	}

	args := parseToolProgressArgs(argsJSON)
	actionKey, detail := toolProgressActionKeyDetail(toolName, args)
	return formatIMToolStatus(lang, i18n.T(actionKey, lang), detail)
}

// formatIMToolStatus builds a two-line (or one-line) status card for IM channels.
// Line 1 is always tagged with a localized tool label so it never blends with
// normal chat replies.
func formatIMToolStatus(lang, action, detail string) string {
	action = strings.TrimSpace(action)
	detail = strings.TrimSpace(detail)
	if action == "" {
		action = i18n.T(i18n.MsgToolActionProcessing, lang)
	}
	// Avoid double-wrapping if a caller already passed a styled line.
	if isIMToolStatusText(action) {
		if detail == "" || strings.Contains(action, "\n"+detail) {
			return action
		}
		return action + "\n" + detail
	}
	header := i18n.T(i18n.MsgToolStatusLabel, lang) + action
	if detail == "" {
		return header
	}
	return header + "\n" + detail
}

// isIMToolStatusText reports whether text is already a styled tool-status card
// in either supported language.
func isIMToolStatusText(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	// Check both locales without allocating for common empty/progress paths.
	if strings.HasPrefix(trimmed, i18n.T(i18n.MsgToolStatusLabel, "zh")) {
		return true
	}
	return strings.HasPrefix(trimmed, i18n.T(i18n.MsgToolStatusLabel, "en"))
}

func normalizeToolProgressName(toolName string) string {
	return strings.ToLower(strings.TrimSpace(toolName))
}

// toolProgressActionKeyDetail returns (i18n action key, optional detail line).
func toolProgressActionKeyDetail(toolName string, args map[string]any) (actionKey, detail string) {
	switch toolName {
	case "bash":
		actionKey = i18n.MsgToolActionRunCommand
		detail = firstStringArg(args, "command", "cmd")
	case "read_file", "read_code":
		actionKey = i18n.MsgToolActionReadFile
		detail = shortPathForProgress(firstStringArg(args, "path", "file", "file_path"))
	case "write_file":
		actionKey = i18n.MsgToolActionWriteFile
		detail = shortPathForProgress(firstStringArg(args, "path", "file", "file_path"))
	case "edit_file", "edit_lines":
		actionKey = i18n.MsgToolActionEditFile
		detail = shortPathForProgress(firstStringArg(args, "path", "file", "file_path"))
	case "list_directory":
		actionKey = i18n.MsgToolActionListDir
		detail = shortPathForProgress(firstStringArg(args, "path", "directory", "dir"))
	case "search_files":
		actionKey = i18n.MsgToolActionSearchFiles
		detail = searchProgressDetail(args)
	case "grep_search":
		actionKey = i18n.MsgToolActionGrep
		detail = searchProgressDetail(args)
	case "web_search":
		actionKey = i18n.MsgToolActionWebSearch
		detail = firstStringArg(args, "query", "q", "search")
	case "web_fetch":
		actionKey = i18n.MsgToolActionWebFetch
		detail = firstStringArg(args, "url", "uri")
	case "send_file", "send_to_im":
		actionKey = i18n.MsgToolActionSendFile
		detail = shortPathForProgress(firstStringArg(args, "path", "file", "file_path"))
	case "im_message":
		actionKey = i18n.MsgToolActionSendFile
		detail = firstStringArg(args, "group_name", "group_id", "user_id", "query", "channel")
		if detail == "" {
			detail = firstStringArg(args, "text", "message")
		}
	case "run_skill", "manage_skill":
		actionKey = i18n.MsgToolActionRunSkill
		detail = firstStringArg(args, "name", "skill_name", "skill")
	case "generate_pdf":
		actionKey = i18n.MsgToolActionGeneratePDF
	case "memory":
		actionKey = i18n.MsgToolActionMemory
		detail = memoryProgressDetail(args)
	case "ssh":
		actionKey, detail = sshProgressActionKeyDetail(args)
	case "screenshot":
		actionKey = i18n.MsgToolActionScreenshot
	case "tts":
		actionKey = i18n.MsgToolActionTTS
	case "asr":
		actionKey = i18n.MsgToolActionASR
		detail = shortPathForProgress(firstStringArg(args, "path", "file", "file_path", "audio_path"))
	case "browser":
		actionKey = i18n.MsgToolActionBrowser
		detail = browserProgressDetail(args)
	case "craft_tool":
		actionKey = i18n.MsgToolActionCraft
		detail = firstStringArg(args, "description", "task", "goal")
	case "open":
		actionKey = i18n.MsgToolActionOpen
		detail = firstStringArg(args, "path", "url", "target")
	case "delegate_task":
		actionKey = i18n.MsgToolActionDelegate
		detail = delegateProgressDetail(args)
	default:
		actionKey = i18n.MsgToolActionCallTool
		detail = toolName
	}

	return actionKey, truncateProgressDetail(detail, 100)
}

func memoryProgressDetail(args map[string]any) string {
	act := firstStringArg(args, "action")
	if act == "" {
		return ""
	}
	if cat := firstStringArg(args, "category", "key"); cat != "" {
		return act + " · " + cat
	}
	return act
}

func browserProgressDetail(args map[string]any) string {
	act := firstStringArg(args, "action")
	if act == "" {
		return ""
	}
	if url := firstStringArg(args, "url"); url != "" {
		return act + " · " + truncateProgressDetail(url, 50)
	}
	return act
}

func delegateProgressDetail(args map[string]any) string {
	agentName := firstStringArg(args, "agent", "name")
	if agentName == "" {
		return ""
	}
	if task := firstStringArg(args, "task", "prompt", "description"); task != "" {
		return agentName + " · " + truncateProgressDetail(task, 40)
	}
	return agentName
}

func searchProgressDetail(args map[string]any) string {
	pattern := firstStringArg(args, "pattern", "query", "glob")
	path := firstStringArg(args, "project_path", "path", "directory")
	filePattern := firstStringArg(args, "file_pattern")
	var parts []string
	if pattern != "" {
		parts = append(parts, truncateProgressDetail(pattern, 40))
	}
	if path != "" {
		parts = append(parts, shortPathForProgress(path))
	}
	if filePattern != "" {
		parts = append(parts, truncateProgressDetail(filePattern, 20))
	}
	return strings.Join(parts, " · ")
}

func sshProgressActionKeyDetail(args map[string]any) (actionKey, detail string) {
	switch firstStringArg(args, "action") {
	case "connect":
		return i18n.MsgToolActionSSHConnect, firstStringArg(args, "host")
	case "exec":
		return i18n.MsgToolActionSSHExec, firstStringArg(args, "command", "cmd")
	case "close":
		return i18n.MsgToolActionSSHClose, ""
	case "close_all":
		return i18n.MsgToolActionSSHCloseAll, ""
	default:
		if cmd := firstStringArg(args, "command", "cmd"); cmd != "" {
			return i18n.MsgToolActionSSH, cmd
		}
		return i18n.MsgToolActionSSH, firstStringArg(args, "host")
	}
}

func parseToolProgressArgs(argsJSON string) map[string]any {
	trimmed := strings.TrimSpace(argsJSON)
	if trimmed == "" || trimmed == "{}" {
		return nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(trimmed), &args); err != nil {
		return nil
	}
	return args
}

func firstStringArg(args map[string]any, keys ...string) string {
	if args == nil {
		return ""
	}
	for _, key := range keys {
		v, ok := args[key]
		if !ok || v == nil {
			continue
		}
		switch s := v.(type) {
		case string:
			if t := strings.TrimSpace(s); t != "" {
				return t
			}
		case fmt.Stringer:
			if t := strings.TrimSpace(s.String()); t != "" {
				return t
			}
		default:
			if t := strings.TrimSpace(fmt.Sprint(v)); t != "" && t != "<nil>" {
				return t
			}
		}
	}
	return ""
}

// shortPathForProgress keeps the last 1–2 path segments so IM bubbles stay readable.
func shortPathForProgress(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	if len([]rune(filepath.ToSlash(path))) <= 48 {
		return path
	}
	base := filepath.Base(path)
	dir := filepath.Base(filepath.Dir(path))
	if dir != "" && dir != "." && dir != string(filepath.Separator) && dir != "/" {
		return truncateProgressDetail(filepath.ToSlash(filepath.Join(dir, base)), 48)
	}
	return truncateProgressDetail(base, 48)
}

func truncateProgressDetail(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	// Collapse whitespace/newlines so multi-line commands fit on one detail line.
	s = strings.Join(strings.Fields(s), " ")
	if maxLen <= 0 || s == "" {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

// extractSkillNameFromArgs extracts the skill name field from run_skill/manage_skill
// JSON arguments. Kept for callers that still use the lightweight helper.
func extractSkillNameFromArgs(argsJSON string) string {
	return firstStringArg(parseToolProgressArgs(argsJSON), "name", "skill_name", "skill")
}

func shouldExposeToolInternalProgress(toolName string) bool {
	switch normalizeToolProgressName(toolName) {
	case "craft_tool", "bash", "run_skill":
		return true
	default:
		return false
	}
}

// stripInternalProgressNoise removes boilerplate prefixes from tool-internal
// progress so restyled cards keep only the useful body.
func stripInternalProgressNoise(msg string) string {
	trimmed := strings.TrimSpace(msg)
	prefixes := []string{
		"正在生成并执行", "正在生成", "正在执行", "正在处理",
		"Preparing", "Running", "Generating", "Executing",
	}
	runes := []rune(trimmed)
	lower := strings.ToLower(trimmed)
	for _, prefix := range prefixes {
		prefixRunes := []rune(prefix)
		pl := strings.ToLower(prefix)
		if !strings.HasPrefix(lower, pl) || len(runes) < len(prefixRunes) {
			continue
		}
		rest := strings.TrimSpace(string(runes[len(prefixRunes):]))
		rest = strings.TrimLeft(rest, "：: \t-—")
		if rest != "" {
			return rest
		}
	}
	return trimmed
}

func filterUserFacingToolProgress(lang, toolName, msg string) string {
	trimmed := strings.TrimSpace(msg)
	if trimmed == "" {
		return ""
	}
	// Already styled tool status — pass through.
	if isIMToolStatusText(trimmed) {
		return trimmed
	}
	if !shouldExposeToolInternalProgress(toolName) {
		return ""
	}

	var actionKey string
	switch normalizeToolProgressName(toolName) {
	case "craft_tool":
		if !hasAnyPrefixFold(trimmed, "正在生成", "正在执行", "Preparing", "Running", "Generating") {
			return ""
		}
		actionKey = i18n.MsgToolActionScriptProgress
	case "bash":
		if !hasAnyPrefixFold(trimmed, "正在执行", "Running", "Executing") {
			return ""
		}
		actionKey = i18n.MsgToolActionCommandProgress
	case "run_skill":
		if !hasAnyPrefixFold(trimmed, "正在执行", "Running", "Preparing", "Generating") {
			return ""
		}
		actionKey = i18n.MsgToolActionSkillProgress
	default:
		return ""
	}

	detail := stripInternalProgressNoise(trimmed)
	// If stripping left nothing useful, keep a short original body.
	if detail == "" {
		detail = truncateProgressDetail(trimmed, 80)
	} else {
		detail = truncateProgressDetail(detail, 80)
	}
	return formatIMToolStatus(lang, i18n.T(actionKey, lang), detail)
}

func hasAnyPrefixFold(s string, prefixes ...string) bool {
	lower := strings.ToLower(s)
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}

// styleIMIntermediateProgress marks non-final IM progress so it is not confused
// with the assistant's final reply. Tool-status cards and heartbeats pass through.
func styleIMIntermediateProgress(lang, text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || trimmed == imHeartbeatMsg {
		return trimmed
	}
	if isIMToolStatusText(trimmed) || isIMProgressStatusText(trimmed) {
		return trimmed
	}
	label := i18n.T(i18n.MsgProgressStatusLabel, lang)
	if label == "" {
		return trimmed
	}
	return label + trimmed
}

func isIMProgressStatusText(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, i18n.T(i18n.MsgProgressStatusLabel, "zh")) {
		return true
	}
	return strings.HasPrefix(trimmed, i18n.T(i18n.MsgProgressStatusLabel, "en"))
}

func filteredToolProgressCallback(lang, toolName string, onProgress tool.ProgressCallback, debug bool) tool.ProgressCallback {
	if onProgress == nil {
		return nil
	}
	if debug {
		return onProgress
	}
	if !shouldExposeToolInternalProgress(toolName) {
		return nil
	}
	return func(msg string) {
		if filtered := filterUserFacingToolProgress(lang, toolName, msg); filtered != "" {
			onProgress(filtered)
		}
	}
}

// imUILang returns the GUI interface language for IM tool/status localization.
// Prefers App.CurrentLanguage so IM text matches the desktop UI language setting.
func (h *IMMessageHandler) imUILang() string {
	if h != nil {
		return appUILang(h.app)
	}
	return "zh"
}

// appUILang returns the GUI interface language from App settings (zh/en tags).
// Used by IM gateways and tool progress so localized text matches the desktop UI language.
func appUILang(app *App) string {
	if app != nil {
		// Prefer structured tag (zh-Hans / en) so i18n.NormalizeLang maps correctly.
		if tag := strings.TrimSpace(normalizeAppLanguageKind(app.CurrentLanguage).TranslationTag()); tag != "" {
			return tag
		}
		if lang := strings.TrimSpace(app.CurrentLanguage); lang != "" {
			return lang
		}
	}
	return "zh"
}
