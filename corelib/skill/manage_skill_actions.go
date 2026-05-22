package skill

import "strings"

// ManageSkillAction describes a single action supported by the manage_skill tool.
type ManageSkillAction struct {
	// Name is the action identifier used in the "action" parameter (e.g. "list", "upload").
	Name string
	// Brief is a short one-line description used in tool definitions sent to the LLM.
	Brief string
}

// ManageSkillActions is the single source of truth for all actions supported by
// the manage_skill tool. Both GUI and TUI dispatchers, tool definitions, tool
// registry entries, error messages, and tests MUST derive their action lists
// from this slice.
//
// To add a new action:
//  1. Append an entry here.
//  2. Add the handler case in gui/im_tools_misc.go toolManageSkill().
//  3. Add the handler case in tui/tool_manage_skill.go newManageSkillHandler().
//  4. Done. All descriptions, error messages, and tests auto-update.
var ManageSkillActions = []ManageSkillAction{
	{"list", "列出本地已注册 Skill（无 Skill 时展示 Hub 推荐）"},
	{"search", "在 SkillHub 搜索可用 Skill"},
	{"install", "从 Hub 安装 Skill 到本地"},
	{"uninstall", "卸载/移除本地已安装的 Skill（删除目录和配置）"},
	{"run", "执行指定 Skill"},
	{"status", "查询运行状态（run 返回 run_id 后继续观察进度）"},
	{"upload", "上传本地 Skill 到 SkillMarket"},
	{"validate", "检查 Skill 的跨平台可移植性并可选自动修复"},
	{"patch", "对 Skill 定义执行修补（mode=text: find-and-replace；mode=step: 结构化修改步骤字段）"},
	{"history", "查看 Skill 的修补历史记录"},
	{"maintenance_plan", "生成只读 Skill 维护计划（不修改、不归档、不合并、不执行 Skill）"},
	{"execute_maintenance_plan", "执行已批准的 Skill 维护动作，默认 dry_run 预演"},
}

// ManageSkillActionNames returns the ordered list of action name strings.
func ManageSkillActionNames() []string {
	names := make([]string, len(ManageSkillActions))
	for i, a := range ManageSkillActions {
		names[i] = a.Name
	}
	return names
}

// ManageSkillActionSlash returns action names joined by "/" (e.g. "list/search/install/...").
func ManageSkillActionSlash() string {
	return strings.Join(ManageSkillActionNames(), "/")
}

// ManageSkillDescription builds the full tool description from the action list.
func ManageSkillDescription() string {
	var b strings.Builder
	b.WriteString("Skill 管理（action: ")
	b.WriteString(ManageSkillActionSlash())
	b.WriteString("）。")
	for i, a := range ManageSkillActions {
		if i > 0 {
			b.WriteString("；")
		}
		b.WriteString(a.Name)
		b.WriteString(" ")
		b.WriteString(a.Brief)
	}
	b.WriteString("。")
	return b.String()
}

// ManageSkillUnknownActionError returns the standard error message for an unrecognized action.
func ManageSkillUnknownActionError(action string) string {
	return "未知 manage_skill action: " + action + "（支持: " + ManageSkillActionSlash() + "）"
}

// ManageSkillActionIsValid returns true if the given action name is in the canonical list.
func ManageSkillActionIsValid(action string) bool {
	for _, a := range ManageSkillActions {
		if a.Name == action {
			return true
		}
	}
	return false
}
