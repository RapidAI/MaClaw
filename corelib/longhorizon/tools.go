package longhorizon

import "strings"

var GUIExecutorTools = []string{
	"computer_observe",
	"computer_click",
	"computer_type",
	"computer_key",
	"computer_scroll",
	"computer_select",
	"computer_scroll_into_view",
	"computer_drag",
	"computer_wait",
	"computer_focus",
	"computer_find",
	"computer_done",
	"computer_playbook",
}

var BrowserExecutorTools = []string{
	"browser_session_start",
	"browser_session_stop",
	"browser_observe",
	"browser_navigate",
	"browser_click",
	"browser_type",
	"browser_wait",
	"browser_refresh",
	"browser_back",
	"browser_extract",
	"browser_scroll",
	"browser_select",
	"browser_list_pages",
	"browser_switch_page",
	"browser_hover",
	"browser_press",
	"browser_dialog",
	"browser_info",
}

var CLIExecutorTools = []string{
	"Glob",
	"ripgrep",
	"read_file",
	"edit_file",
	"edit_lines",
	"write_file",
	"bash",
	"list_directory",
	"git_diff",
	"code_navigation",
	"report_localization",
	"todo_write",
}

var forbiddenExecutorTools = []string{
	"computer_observe",
	"computer_action",
	"computer_screenshot",
	"coding_knowledge_search",
	"knowledge_search",
	"knowledge_image_search",
	"call_mcp_tool",
	"manage_skill",
	"spawn_coding_agent",
	"goal",
}

func NormalizeToolName(name string) string {
	return strings.TrimSpace(name)
}

func ToolAllowed(surface []string, name string) bool {
	name = NormalizeToolName(name)
	if name == "" {
		return false
	}
	for _, allowed := range surface {
		if NormalizeToolName(allowed) == name {
			return true
		}
	}
	return false
}

func FilterToolNames(names []string, surface []string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		if ToolAllowed(surface, name) {
			out = append(out, NormalizeToolName(name))
		}
	}
	return out
}

func RejectForbiddenExecutorTools(names []string) []string {
	var bad []string
	seen := map[string]bool{}
	for _, name := range names {
		n := NormalizeToolName(name)
		for _, forbidden := range forbiddenExecutorTools {
			if n == forbidden && !seen[n] {
				bad = append(bad, n)
				seen[n] = true
			}
		}
	}
	return bad
}

func DefaultSurfaceForRole(role string) []string {
	switch role {
	case RoleCLIExecutor:
		return append([]string(nil), CLIExecutorTools...)
	case RoleGUIExecutor:
		return append([]string(nil), GUIExecutorTools...)
	case RoleBrowserExecutor:
		return append([]string(nil), BrowserExecutorTools...)
	default:
		return nil
	}
}

func IsAuditorRole(role string) bool {
	switch role {
	case RoleCLIAuditor, RoleGUIAuditor, RoleBrowserAuditor:
		return true
	default:
		return false
	}
}

func SurfaceViolatesRole(role string, names []string) bool {
	for _, name := range names {
		if ToolForbiddenForRole(role, name) {
			return true
		}
	}
	return false
}

func ToolForbiddenForRole(role, name string) bool {
	n := NormalizeToolName(name)
	if n == "" {
		return false
	}
	switch role {
	case RoleGUIExecutor:
		if strings.HasPrefix(n, "browser_") {
			return true
		}
		return toolInList(n, guiForbiddenTools)
	case RoleGUIAuditor:
		if n == "computer_focus" || strings.HasPrefix(n, "computer_") {
			return true
		}
		return toolInList(n, forbiddenExecutorTools)
	case RoleBrowserExecutor:
		if strings.HasPrefix(n, "computer_") {
			return true
		}
		return toolInList(n, browserForbiddenTools)
	default:
		return toolInList(n, forbiddenExecutorTools)
	}
}

var guiForbiddenTools = []string{
	"bash", "write_file", "edit_file", "edit_lines",
	"coding_knowledge_search", "knowledge_search", "knowledge_image_search",
	"call_mcp_tool", "manage_skill", "spawn_coding_agent", "goal",
}

var browserForbiddenTools = []string{
	"bash", "write_file", "edit_file", "edit_lines",
	"computer_observe", "computer_action", "computer_screenshot",
	"coding_knowledge_search", "knowledge_search", "knowledge_image_search",
	"call_mcp_tool", "manage_skill", "spawn_coding_agent", "goal",
}

func toolInList(name string, names []string) bool {
	for _, item := range names {
		if name == item {
			return true
		}
	}
	return false
}
