package computeruse

import "strings"

// ToolNames is the full Computer Use tool surface exposed to the agent.
var ToolNames = []string{
	"computer_observe",
	"computer_click",
	"computer_type",
	"computer_key",
	"computer_scroll",
	"computer_wait",
	"computer_focus",
	"computer_done",
	"computer_playbook",
}

// LegacyGUICompeteTools are older raw GUI tools that should be de-prioritized
// when Computer Use is active (prefer ref-based computer_* instead).
var LegacyGUICompeteTools = map[string]bool{
	"gui_click":      true,
	"gui_type":       true,
	"gui_screenshot": true,
}

// ShouldActivate reports whether the user message (or ongoing CU session)
// should expose Computer Use tools and inject the text-primary playbook.
// This is a cheap lexical gate — not a full intent classifier.
func ShouldActivate(userText string) bool {
	t := strings.ToLower(strings.TrimSpace(userText))
	if t == "" {
		return false
	}
	// Explicit triggers always win (even if message also mentions a browser).
	if strings.Contains(t, "@computer") || strings.Contains(t, "computer use") ||
		strings.Contains(t, "computer_use") || strings.Contains(t, "computer-use") {
		return true
	}
	// Pure web tasks → browser_*, not Computer Use.
	webish := strings.Contains(t, "浏览器") || strings.Contains(t, "browser") ||
		strings.Contains(t, "网页") || strings.Contains(t, "http://") || strings.Contains(t, "https://")
	// Strong desktop signals
	zhHints := []string{
		"桌面操作", "操作桌面", "点击屏幕", "截屏看", "看屏幕",
		"电脑上打开", "打开软件", "打开应用", "gui 操作", "gui操作",
		"记事本", "计算器", "控制面板", "桌面自动化", "屏幕上",
		"鼠标点击", "键盘输入", "本机应用", "桌面软件",
	}
	for _, h := range zhHints {
		if strings.Contains(t, strings.ToLower(h)) {
			return true
		}
	}
	enHints := []string{
		"desktop app", "native app", "open notepad", "open calculator",
		"use the gui", "gui test", "operate the desktop", "control the mouse",
		"on the desktop", "screen click",
	}
	for _, h := range enHints {
		if strings.Contains(t, h) {
			return true
		}
	}
	// Weaker "click/type" language only if not clearly a web task.
	if !webish {
		weak := []string{"帮我点", "帮我操作", "点一下", "click on the", "type into", "click the button"}
		for _, h := range weak {
			if strings.Contains(t, h) {
				return true
			}
		}
	}
	return false
}

// IsComputerUseTool reports whether name is part of the CU surface.
func IsComputerUseTool(name string) bool {
	name = strings.TrimSpace(name)
	for _, n := range ToolNames {
		if n == name {
			return true
		}
	}
	return false
}
