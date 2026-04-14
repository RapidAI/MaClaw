package main

import (
	"fmt"
	"strings"
)

// toolDiscoverTool handles the discover_tool tool call for TUI.
// In TUI mode, all tools are always available in the dispatch, so this
// just lists the available tools matching the user's need description.
func (h *TUIAgentHandler) toolDiscoverTool(args map[string]interface{}) string {
	need := stringArg(args, "need")
	if need == "" {
		return "请描述你需要的能力（如'修改配置'、'定时执行'、'搜索知识网络'）"
	}

	// Simple keyword matching against known tool groups.
	type toolGroup struct {
		keywords []string
		tools    string
	}
	groups := []toolGroup{
		{[]string{"配置", "config", "设置", "settings"}, "manage_config(action=get/set/batch/schema/export/import) — 配置管理"},
		{[]string{"定时", "schedule", "cron", "timer", "间隔"}, "manage_schedule(action=create/list/delete/update) — 定时任务管理"},
		{[]string{"模板", "template"}, "manage_template(action=create/list/launch) — 会话模板管理"},
		{[]string{"agentnet", "知识网络", "p2p"}, "agentnet_search / agentnet_publish — AgentNet 知识网络"},
		{[]string{"审计", "audit", "日志"}, "query_audit_log — 审计日志查询"},
		{[]string{"mcp", "扩展"}, "list_mcp_tools / call_mcp_tool — MCP 扩展工具"},
		{[]string{"skill", "技能", "市场"}, "search_skill_hub / install_skill_hub — Skill 市场搜索安装"},
		{[]string{"并行", "parallel"}, "parallel_execute — 并行执行多个任务"},
		{[]string{"切换", "模型", "provider", "llm"}, "switch_llm_provider — 切换 LLM 服务商"},
	}

	needLower := strings.ToLower(need)
	var matches []string
	for _, g := range groups {
		for _, kw := range g.keywords {
			if strings.Contains(needLower, kw) {
				matches = append(matches, g.tools)
				break
			}
		}
	}

	if len(matches) == 0 {
		return fmt.Sprintf("未找到匹配 %q 的工具。可用工具组：配置管理、定时任务、模板、AgentNet、审计日志、MCP、Skill 市场、并行执行、LLM 切换。", need)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("找到 %d 个匹配工具:\n", len(matches)))
	for i, m := range matches {
		b.WriteString(fmt.Sprintf("\n%d. %s", i+1, m))
	}
	b.WriteString("\n\n这些工具已可直接调用。")
	return b.String()
}
