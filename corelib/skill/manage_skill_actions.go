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
	{"list", "列出本地已注册 Skill（无 Skill 时展示 Hub 推荐；行内附带 params 摘要）"},
	{"info", "查看指定 Skill 的参数契约（完整 params + JSON Schema；run 前建议先 inspect）"},
	{"search", "在 SkillHub 搜索可用 Skill"},
	{"install", "从 Hub 安装 Skill 到本地"},
	{"uninstall", "卸载/移除本地已安装的 Skill（删除目录和配置）"},
	{"run", "执行指定 Skill"},
	{"status", "查询运行状态（run 返回 run_id 后继续观察进度）"},
	{"upload", "上传/发布本地 Skill 到 SkillMarket/HubCenter/能力市场（publish/pub/submit/发布/上架 都使用 action=\"upload\"；上传前自动检查并修正绝对路径、补全缺失文件等可移植性问题）"},
	{"validate", "检查 Skill 的跨平台可移植性并可选自动修复"},
	{"patch", "对 Skill 定义执行修补（mode=text: find-and-replace；mode=step: 结构化修改步骤字段）"},
	{"history", "查看 Skill 的修补历史记录"},
	{"maintenance_plan", "生成只读 Skill 维护计划（不修改、不归档、不合并、不执行 Skill）"},
	{"maintenance_drafts", "收集需人审的 patch_draft / merge_draft 与排队自修复项（只读 dry-run；不修改 Skill）"},
	{"execute_maintenance_plan", "执行已批准的 Skill 维护动作，默认 dry_run 预演"},
	{"evolution_status", "查询 Skill 自进化管道状态（自修复/优化/自动发现是否启用、排队数、冷却时间等；只读）"},
	{"evolution_compensations", "查询待恢复补偿队列的安全摘要（只读；不暴露快照内容、不触发恢复）"},
	{"evolution_audit", "查询 Skill 自进化持久审计日志（只读；limit 默认 50 最大 200；可选 name 过滤技能；路径 ~/.maclaw/skill_evolution/audit.jsonl）"},
	{"set_evolution_enabled", "开关自动自进化（enabled=true/false 必填；写入 skill_evolution_enabled 配置；true 时同时清除 session 禁用；不影响手动 trigger_repair/trigger_optimize；环境变量 MACLAW_DISABLE_SKILL_EVOLUTION 仍优先强制关闭）"},
	{"trigger_repair", "立即对指定 Skill 尝试 LLM 自修复（name 必填；force=true 时跳过成功率门槛但仍受安全与次数限制；wait=true 时同步等待结果）"},
	{"trigger_optimize", "立即对指定 Skill 尝试 LLM 优化（name 必填；force=true 时跳过自动优化门槛与 24h 节流；默认同步执行）"},
	{"list_repair_drafts", "列出 file-backed Skill 待人审的修复草稿（可选 name 过滤；只读；草稿位于 <skill_dir>/.evolution-drafts/）"},
	{"apply_repair_draft", "应用指定的待审修复草稿（name + draft 文件名必填；写回 skill.yaml 与配置后删除草稿）"},
	{"reject_repair_draft", "拒绝并删除指定的待审修复草稿（name + draft 文件名必填；不修改 Skill）"},
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

// NormalizeManageSkillAction maps common LLM/user aliases to the canonical
// manage_skill action identifiers. Unknown actions are returned trimmed and
// lower-cased so callers can still report the original unsupported intent.
func NormalizeManageSkillAction(action string) string {
	normalized := strings.ToLower(strings.TrimSpace(action))
	switch normalized {
	case "publish", "pub", "submit", "release", "发布", "發布", "上架", "提交":
		return "upload"
	case "info", "inspect", "show", "describe", "get", "detail", "schema", "params":
		return "info"
	case "evolution", "evol_status", "self_repair_status", "optimize_status":
		return "evolution_status"
	case "compensations", "recovery_queue", "audit_pending":
		return "evolution_compensations"
	case "evolution_log", "audit", "audit_log", "evolution_history", "skill_evolution_audit":
		return "evolution_audit"
	case "list_drafts", "review_drafts", "patch_drafts", "governance_drafts":
		return "maintenance_drafts"
	case "set_evolution", "evolution_enable", "enable_evolution", "disable_evolution", "set_skill_evolution":
		return "set_evolution_enabled"
	case "repair", "repair_now", "self_repair", "attempt_repair", "fix_skill":
		return "trigger_repair"
	case "optimize", "optimize_now", "trigger_opt", "improve_skill":
		return "trigger_optimize"
	default:
		return normalized
	}
}
