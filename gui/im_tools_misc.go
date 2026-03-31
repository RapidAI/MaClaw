package main

// Miscellaneous tools: MCP, skills, memory, templates, config, nickname,
// LLM provider switch, scheduled tasks, ClawNet, audit log, web search/fetch.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

func (h *IMMessageHandler) toolListMCPTools() string {
	var b strings.Builder
	hasAny := false

	// List local (stdio) MCP servers
	if mgr := h.app.localMCPManager; mgr != nil {
		for _, ts := range mgr.GetAllTools() {
			hasAny = true
			b.WriteString(fmt.Sprintf("## %s (%s) [本地/stdio]\n", ts.ServerName, ts.ServerID))
			for _, t := range ts.Tools {
				b.WriteString(fmt.Sprintf("  - %s: %s\n", t.Name, t.Description))
			}
		}
	}

	// List remote (HTTP) MCP servers
	registry := h.app.mcpRegistry
	if registry != nil {
		servers := registry.ListServers()
		for _, s := range servers {
			hasAny = true
			b.WriteString(fmt.Sprintf("## %s (%s) [远程/HTTP] 状态=%s\n", s.Name, s.ID, s.HealthStatus))
			tools := registry.GetServerTools(s.ID)
			if len(tools) == 0 {
				b.WriteString("  (无工具或无法获取)\n")
				continue
			}
			for _, t := range tools {
				b.WriteString(fmt.Sprintf("  - %s: %s\n", t.Name, t.Description))
			}
		}
	}

	if !hasAny {
		return "没有已注册的 MCP Server"
	}
	return b.String()
}

func (h *IMMessageHandler) toolCallMCPTool(args map[string]interface{}) string {
	serverID, _ := args["server_id"].(string)
	toolName, _ := args["tool_name"].(string)
	if serverID == "" || toolName == "" {
		return "缺少 server_id 或 tool_name 参数"
	}
	toolArgs, _ := args["arguments"].(map[string]interface{})

	// Try local MCP manager first (stdio-based servers)
	if mgr := h.app.localMCPManager; mgr != nil && mgr.IsRunning(serverID) {
		result, err := mgr.CallTool(serverID, toolName, toolArgs)
		if err != nil {
			return fmt.Sprintf("本地 MCP 调用失败: %s", err.Error())
		}
		return result
	}

	// Fall back to remote MCP registry (HTTP-based servers)
	registry := h.app.mcpRegistry
	if registry == nil {
		return "MCP Registry 未初始化"
	}
	result, err := registry.CallTool(serverID, toolName, toolArgs)
	if err != nil {
		return fmt.Sprintf("MCP 调用失败: %s", err.Error())
	}
	return result
}

func (h *IMMessageHandler) toolListSkills() string {
	exec := h.app.skillExecutor
	if exec == nil {
		return "Skill Executor 未初始化"
	}
	skills := exec.List()

	var b strings.Builder

	// Show local skills
	if len(skills) > 0 {
		b.WriteString("=== 本地已注册 Skill ===\n")
		for _, s := range skills {
			line := fmt.Sprintf("- %s [%s]: %s", s.Name, s.Status, s.Description)
			if s.Source == "hub" {
				line += fmt.Sprintf(" (来源: Hub, trust: %s)", s.TrustLevel)
			} else if s.Source == "file" {
				line += " (来源: 本地文件)"
			}
			if s.UsageCount > 0 {
				line += fmt.Sprintf(" (用过%d次, 成功率%.0f%%)", s.UsageCount, s.SuccessRate*100)
			}
			b.WriteString(line + "\n")
		}
	} else {
		b.WriteString("本地没有已注册的 Skill。\n")
	}

	// If local skills are empty or few, also show Hub recommendations
	if len(skills) < 3 && h.app.skillHubClient != nil {
		recs := h.app.skillHubClient.GetRecommendations()
		if len(recs) > 0 {
			b.WriteString("\n=== SkillHub 推荐 Skill（可用 install_skill_hub 安装）===\n")
			for _, r := range recs {
				b.WriteString(fmt.Sprintf("- [%s] %s: %s (trust: %s, downloads: %d, hub: %s)\n",
					r.ID, r.Name, r.Description, r.TrustLevel, r.Downloads, r.HubURL))
			}
		} else {
			b.WriteString("\n提示：可以使用 search_skill_hub 工具在 SkillHub 上搜索更多 Skill。\n")
		}
	}

	return b.String()
}

func (h *IMMessageHandler) toolSearchSkillHub(args map[string]interface{}) string {
	query, _ := args["query"].(string)
	if query == "" {
		return "缺少 query 参数"
	}

	if h.app.skillHubClient == nil {
		h.app.ensureSkillHubClient()
	}
	if h.app.skillHubClient == nil {
		return "SkillHub 客户端未初始化，请检查配置中的 skill_hub_urls"
	}

	results, err := h.app.skillHubClient.Search(context.Background(), query)
	if err != nil {
		return fmt.Sprintf("搜索失败: %s", err.Error())
	}
	if len(results) == 0 {
		return fmt.Sprintf("在 SkillHub 上未找到与 %q 相关的 Skill", query)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("找到 %d 个 Skill：\n", len(results)))
	for _, r := range results {
		tags := ""
		if len(r.Tags) > 0 {
			tags = " [" + strings.Join(r.Tags, ", ") + "]"
		}
		b.WriteString(fmt.Sprintf("- ID: %s | %s: %s%s (trust: %s, downloads: %d, hub: %s)\n",
			r.ID, r.Name, r.Description, tags, r.TrustLevel, r.Downloads, r.HubURL))
	}
	b.WriteString("\n使用 install_skill_hub 工具安装，需提供 skill_id 和 hub_url 参数。")
	return b.String()
}

func (h *IMMessageHandler) toolInstallSkillHub(args map[string]interface{}) string {
	skillID, _ := args["skill_id"].(string)
	hubURL, _ := args["hub_url"].(string)
	if skillID == "" {
		return "缺少 skill_id 参数"
	}
	if hubURL == "" {
		return "缺少 hub_url 参数"
	}

	if h.app.skillHubClient == nil {
		h.app.ensureSkillHubClient()
	}
	if h.app.skillHubClient == nil {
		return "SkillHub 客户端未初始化"
	}
	if h.app.skillExecutor == nil {
		return "Skill Executor 未初始化"
	}

	// Download from Hub
	entry, err := h.app.skillHubClient.Install(context.Background(), skillID, hubURL)
	if err != nil {
		return fmt.Sprintf("安装失败: %s", err.Error())
	}

	// Security review if risk assessor is available
	if h.app.riskAssessor != nil {
		assessment := h.app.riskAssessor.AssessSkill(entry, entry.TrustLevel)
		if assessment.Level == RiskCritical {
			if h.app.auditLog != nil {
				_ = h.app.auditLog.Log(AuditEntry{
					Timestamp:    time.Now(),
					Action:       AuditActionHubSkillReject,
					ToolName:     "hub_skill_install",
					RiskLevel:    RiskCritical,
					PolicyAction: PolicyDeny,
					Result:       fmt.Sprintf("rejected skill %s from %s: critical risk", skillID, hubURL),
				})
			}
			return fmt.Sprintf("⚠️ Skill %q 包含高风险操作，已拒绝自动安装。风险因素: %s",
				entry.Name, strings.Join(assessment.Factors, ", "))
		}
	}

	// Register locally
	if err := h.app.skillExecutor.Register(*entry); err != nil {
		return fmt.Sprintf("注册失败: %s", err.Error())
	}

	// Audit log
	if h.app.auditLog != nil {
		_ = h.app.auditLog.Log(AuditEntry{
			Timestamp:    time.Now(),
			Action:       AuditActionHubSkillInstall,
			ToolName:     "hub_skill_install",
			RiskLevel:    RiskLow,
			PolicyAction: PolicyAllow,
			Result:       fmt.Sprintf("installed skill %s (%s) from %s, trust: %s", entry.Name, skillID, hubURL, entry.TrustLevel),
		})
	}

	// Auto-run: default to true unless explicitly set to false.
	autoRun := true
	if v, ok := args["auto_run"]; ok {
		switch val := v.(type) {
		case bool:
			autoRun = val
		case string:
			autoRun = strings.EqualFold(val, "true")
		}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("✅ 已成功安装 Skill「%s」\n描述: %s\n来源: %s\n信任等级: %s\n",
		entry.Name, entry.Description, hubURL, entry.TrustLevel))

	if autoRun {
		b.WriteString(fmt.Sprintf("\n正在立即执行 Skill「%s」...\n", entry.Name))
		result, err := h.app.skillExecutor.Execute(entry.Name)
		if err != nil {
			b.WriteString(fmt.Sprintf("执行失败: %s\n%s", err.Error(), result))
		} else {
			b.WriteString(fmt.Sprintf("执行结果:\n%s", result))
		}
	} else {
		b.WriteString(fmt.Sprintf("\n可以使用 run_skill 工具执行，名称为: %s", entry.Name))
	}

	return b.String()
}

func (h *IMMessageHandler) toolRunSkill(args map[string]interface{}) string {
	exec := h.app.skillExecutor
	if exec == nil {
		return "Skill Executor 未初始化"
	}
	name, _ := args["name"].(string)
	if name == "" {
		return "缺少 name 参数"
	}
	result, err := exec.Execute(name)
	if err != nil {
		return fmt.Sprintf("Skill 执行失败: %s\n%s", err.Error(), result)
	}
	return result
}

// stringVal extracts a string value from a map, returning "" if the key is
// missing or not a string.
func stringVal(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

func (h *IMMessageHandler) toolParallelExecute(args map[string]interface{}) string {
	orch := h.app.orchestrator
	if orch == nil {
		return "Orchestrator 未初始化"
	}
	tasksRaw, ok := args["tasks"].([]interface{})
	if !ok || len(tasksRaw) == 0 {
		return "缺少 tasks 参数"
	}
	var tasks []TaskRequest
	for _, t := range tasksRaw {
		tm, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		tr := TaskRequest{
			Tool:        stringVal(tm, "tool"),
			Description: stringVal(tm, "description"),
			ProjectPath: stringVal(tm, "project_path"),
		}
		if tr.Tool == "" {
			continue
		}
		tasks = append(tasks, tr)
	}
	if len(tasks) == 0 {
		return "没有有效的任务"
	}
	result, err := orch.ExecuteParallel(tasks)
	if err != nil {
		return fmt.Sprintf("并行执行失败: %s", err.Error())
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("任务 %s: %s\n", result.TaskID, result.Summary))
	for key, sr := range result.Results {
		b.WriteString(fmt.Sprintf("- %s: tool=%s status=%s", key, sr.Tool, sr.Status))
		if sr.SessionID != "" {
			b.WriteString(fmt.Sprintf(" session=%s", sr.SessionID))
		}
		if sr.Error != "" {
			b.WriteString(fmt.Sprintf(" error=%s", sr.Error))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (h *IMMessageHandler) toolRecommendTool(args map[string]interface{}) string {
	selector := h.app.toolSelector
	if selector == nil {
		return "ToolSelector 未初始化"
	}
	desc, _ := args["task_description"].(string)
	if desc == "" {
		return "缺少 task_description 参数"
	}
	// Build list of installed tools by checking if their binaries are on PATH.
	var installed []string
	for _, tool := range []string{"claude", "codex", "gemini", "cursor", "opencode", "iflow", "kilo"} {
		meta, ok := remoteToolCatalog[tool]
		if !ok {
			continue
		}
		if _, err := exec.LookPath(meta.BinaryName); err == nil {
			installed = append(installed, tool)
		}
	}
	name, reason := selector.Recommend(desc, installed)
	return fmt.Sprintf("推荐工具: %s\n理由: %s", name, reason)
}

// ---------------------------------------------------------------------------
// 本机直接操作工具 (bash, read_file, write_file, list_directory)
// ---------------------------------------------------------------------------

const (
	bashDefaultTimeout = 30
	bashMaxTimeout     = 120
	readFileMaxLines   = 200
	writeFileMaxSize   = 1 << 20 // 1 MB
)

// resolvePath resolves a path, expanding ~ and making relative paths relative
// to the user's home directory.
func resolvePath(p string) string {
	if p == "" {
		home, _ := os.UserHomeDir()
		return home
	}
	if strings.HasPrefix(p, "~") {
		home, _ := os.UserHomeDir()
		p = filepath.Join(home, p[1:])
	}
	if !filepath.IsAbs(p) {
		home, _ := os.UserHomeDir()
		p = filepath.Join(home, p)
	}
	return filepath.Clean(p)
}
// toolMemory merges save/list/delete/recall memory operations into a single tool.
func (h *IMMessageHandler) toolMemory(args map[string]interface{}) string {
	if h.memoryStore == nil {
		return "长期记忆未初始化"
	}

	action := stringVal(args, "action")
	switch action {
	case "recall":
		query := stringVal(args, "query")
		if query == "" {
			return "缺少 query 参数"
		}
		category := MemoryCategory(stringVal(args, "category"))
		// Resolve current project path for affinity boosting.
		var projectPath string
		if h.contextResolver != nil {
			projectPath, _ = h.contextResolver.ResolveProject()
		}
		entries := h.memoryStore.RecallDynamic(query, category, projectPath)
		if len(entries) == 0 {
			return "没有找到相关记忆。"
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("召回 %d 条相关记忆:\n", len(entries)))
		for _, e := range entries {
			b.WriteString(fmt.Sprintf("- [%s] %s\n", string(e.Category), e.Content))
		}
		// Touch access counts.
		ids := make([]string, len(entries))
		for i, e := range entries {
			ids[i] = e.ID
		}
		h.memoryStore.TouchAccess(ids)
		return b.String()

	case "save":
		content := stringVal(args, "content")
		if content == "" {
			return "缺少 content 参数"
		}
		category := stringVal(args, "category")
		if category == "" {
			category = "user_fact"
		}
		var tags []string
		if rawTags, ok := args["tags"]; ok {
			if tagSlice, ok := rawTags.([]interface{}); ok {
				for _, t := range tagSlice {
					if s, ok := t.(string); ok && s != "" {
						tags = append(tags, s)
					}
				}
			}
		}
		entry := MemoryEntry{
			Content:  content,
			Category: MemoryCategory(category),
			Tags:     tags,
		}
		if err := h.memoryStore.Save(entry); err != nil {
			return fmt.Sprintf("保存记忆失败: %s", err.Error())
		}
		summary := content
		if len(summary) > 50 {
			summary = summary[:50] + "..."
		}
		return fmt.Sprintf("已保存记忆: %s", summary)

	case "list":
		category := MemoryCategory(stringVal(args, "category"))
		keyword := stringVal(args, "keyword")
		entries := h.memoryStore.List(category, keyword)
		if len(entries) == 0 {
			return "没有找到匹配的记忆条目。"
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("找到 %d 条记忆:\n", len(entries)))
		for _, e := range entries {
			b.WriteString(fmt.Sprintf("- [%s] (%s) %s", e.ID, e.Category, e.Content))
			if len(e.Tags) > 0 {
				b.WriteString(fmt.Sprintf(" 标签=%v", e.Tags))
			}
			b.WriteString("\n")
		}
		return b.String()

	case "delete":
		id := stringVal(args, "id")
		if id == "" {
			return "缺少 id 参数"
		}
		if err := h.memoryStore.Delete(id); err != nil {
			return fmt.Sprintf("删除记忆失败: %s", err.Error())
		}
		return fmt.Sprintf("已删除记忆: %s", id)

	default:
		return "action 参数无效，可选值: recall, save, list, delete"
	}
}

// ---------------------------------------------------------------------------
// Template Tools
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) toolCreateTemplate(args map[string]interface{}) string {
	if h.templateManager == nil {
		return "模板管理器未初始化"
	}

	name := stringVal(args, "name")
	tool := stringVal(args, "tool")
	if name == "" || tool == "" {
		return "缺少 name 或 tool 参数"
	}

	tpl := SessionTemplate{
		Name:        name,
		Tool:        tool,
		ProjectPath: stringVal(args, "project_path"),
		ModelConfig: stringVal(args, "model_config"),
	}

	// Parse yolo_mode (can arrive as bool or string).
	if yolo, ok := args["yolo_mode"].(bool); ok {
		tpl.YoloMode = yolo
	} else if yoloStr, ok := args["yolo_mode"].(string); ok {
		tpl.YoloMode = yoloStr == "true"
	}

	if err := h.templateManager.Create(tpl); err != nil {
		return fmt.Sprintf("创建模板失败: %s", err.Error())
	}
	return fmt.Sprintf("模板已创建: %s（工具=%s, 项目=%s）", name, tool, tpl.ProjectPath)
}

func (h *IMMessageHandler) toolListTemplates() string {
	if h.templateManager == nil {
		return "模板管理器未初始化"
	}

	templates := h.templateManager.List()
	if len(templates) == 0 {
		return "当前没有会话模板。"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("共 %d 个模板:\n", len(templates)))
	for _, t := range templates {
		b.WriteString(fmt.Sprintf("- %s: 工具=%s 项目=%s", t.Name, t.Tool, t.ProjectPath))
		if t.ModelConfig != "" {
			b.WriteString(fmt.Sprintf(" 模型=%s", t.ModelConfig))
		}
		if t.YoloMode {
			b.WriteString(" [Yolo]")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (h *IMMessageHandler) toolLaunchTemplate(args map[string]interface{}) string {
	if h.templateManager == nil {
		return "模板管理器未初始化"
	}

	name := stringVal(args, "template_name")
	if name == "" {
		return "缺少 template_name 参数"
	}

	tpl, err := h.templateManager.Get(name)
	if err != nil {
		return fmt.Sprintf("获取模板失败: %s", err.Error())
	}

	// Build args from template config and delegate to toolCreateSession.
	sessionArgs := map[string]interface{}{
		"tool":         tpl.Tool,
		"project_path": tpl.ProjectPath,
	}
	return h.toolCreateSession(sessionArgs)
}

// ---------------------------------------------------------------------------
// Config Tools
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) toolGetConfig(args map[string]interface{}) string {
	if h.configManager == nil {
		return "配置管理器未初始化"
	}

	section := stringVal(args, "section")
	result, err := h.configManager.GetConfig(section, true)
	if err != nil {
		return fmt.Sprintf("读取配置失败: %s", err.Error())
	}
	return result
}

func (h *IMMessageHandler) toolUpdateConfig(args map[string]interface{}) string {
	if h.configManager == nil {
		return "配置管理器未初始化"
	}

	section := stringVal(args, "section")
	key := stringVal(args, "key")
	value := stringVal(args, "value")
	if section == "" || key == "" {
		return "缺少 section 或 key 参数"
	}

	oldValue, err := h.configManager.UpdateConfig(section, key, value)
	if err != nil {
		return fmt.Sprintf("修改配置失败: %s", err.Error())
	}
	return fmt.Sprintf("配置已更新: %s.%s\n旧值: %s\n新值: %s", section, key, oldValue, value)
}

func (h *IMMessageHandler) toolBatchUpdateConfig(args map[string]interface{}) string {
	if h.configManager == nil {
		return "配置管理器未初始化"
	}

	changesStr := stringVal(args, "changes")
	if changesStr == "" {
		return "缺少 changes 参数"
	}

	var changes []ConfigChange
	if err := json.Unmarshal([]byte(changesStr), &changes); err != nil {
		return fmt.Sprintf("解析 changes 参数失败: %s", err.Error())
	}
	if len(changes) == 0 {
		return "changes 列表为空"
	}

	if err := h.configManager.BatchUpdate(changes); err != nil {
		return fmt.Sprintf("批量更新配置失败: %s", err.Error())
	}
	return fmt.Sprintf("批量更新成功，共应用 %d 项变更", len(changes))
}

func (h *IMMessageHandler) toolListConfigSchema() string {
	if h.configManager == nil {
		return "配置管理器未初始化"
	}

	result, err := h.configManager.SchemaJSON()
	if err != nil {
		return fmt.Sprintf("获取配置 Schema 失败: %s", err.Error())
	}
	return result
}

func (h *IMMessageHandler) toolExportConfig() string {
	if h.configManager == nil {
		return "配置管理器未初始化"
	}

	result, err := h.configManager.ExportConfig()
	if err != nil {
		return fmt.Sprintf("导出配置失败: %s", err.Error())
	}
	return result
}

func (h *IMMessageHandler) toolImportConfig(args map[string]interface{}) string {
	if h.configManager == nil {
		return "配置管理器未初始化"
	}

	jsonData := stringVal(args, "json_data")
	if jsonData == "" {
		return "缺少 json_data 参数"
	}

	report, err := h.configManager.ImportConfig(jsonData)
	if err != nil {
		return fmt.Sprintf("导入配置失败: %s", err.Error())
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("配置导入完成: 应用 %d 项, 跳过 %d 项", report.Applied, report.Skipped))
	if len(report.Warnings) > 0 {
		b.WriteString("\n警告:")
		for _, w := range report.Warnings {
			b.WriteString(fmt.Sprintf("\n  - %s", w))
		}
	}
	return b.String()
}

// toolSetMaxIterations allows the agent to dynamically adjust the max
// iterations for the current conversation loop. This does NOT change the
// persisted config — it only affects the in-flight loop.
func (h *IMMessageHandler) toolSetMaxIterations(args map[string]interface{}) string {
	n, ok := args["max_iterations"].(float64)
	if !ok || n < 1 {
		return fmt.Sprintf("缺少或无效的 max_iterations 参数（需要 %d-%d 的整数）", minAgentIterations, maxAgentIterationsCap)
	}
	limit := int(n)
	if limit < minAgentIterations {
		limit = minAgentIterations
	}
	if limit > maxAgentIterationsCap {
		limit = maxAgentIterationsCap
	}
	reason := stringVal(args, "reason")
	h.loopMaxOverride = limit
	// Also update the active LoopContext so background loops see the change.
	if h.currentLoopCtx != nil {
		h.currentLoopCtx.SetMaxIterations(limit)
	}

	// Persist to config so the setting survives across conversations.
	if err := h.app.SetMaclawAgentMaxIterations(limit); err != nil {
		// Non-fatal: the override still applies to the current loop.
		_ = err
	}

	if reason != "" {
		return fmt.Sprintf("✅ 已将最大轮数调整为 %d（已持久化，原因: %s）", limit, reason)
	}
	return fmt.Sprintf("✅ 已将最大轮数调整为 %d（已持久化）", limit)
}

// ---------------------------------------------------------------------------
// Nickname (set_nickname) tool
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) toolSetNickname(args map[string]interface{}) string {
	nickname := strings.TrimSpace(stringVal(args, "nickname"))
	if nickname == "" {
		return "❌ nickname 不能为空"
	}
	// Persist to local config.
	cfg, err := h.app.LoadConfig()
	if err == nil {
		cfg.RemoteNickname = nickname
		_ = h.app.SaveConfig(cfg)
	}
	// Send to Hub via WebSocket.
	if hc := h.app.hubClient(); hc != nil {
		if err := hc.SendNicknameUpdate(nickname); err != nil {
			log.Printf("[set_nickname] SendNicknameUpdate error: %v", err)
			return fmt.Sprintf("⚠️ 昵称已保存到本地（%s），但上报 Hub 失败：%v", nickname, err)
		}
	}
	return fmt.Sprintf("✅ 昵称已更新为「%s」，Hub 已同步。", nickname)
}

// ---------------------------------------------------------------------------
// LLM provider switch tool
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) toolSwitchLLMProvider(args map[string]interface{}) string {
	providerName := stringVal(args, "provider")
	if providerName == "" {
		// No provider specified — list available providers and current selection.
		info := h.app.GetMaclawLLMProviders()
		var b strings.Builder
		b.WriteString(fmt.Sprintf("当前 LLM 服务商: %s\n可用服务商:\n", info.Current))
		for _, p := range info.Providers {
			if p.URL == "" && p.Key == "" && p.Model == "" {
				continue // skip unconfigured custom slots
			}
			marker := ""
			if p.Name == info.Current {
				marker = " [当前]"
			}
			b.WriteString(fmt.Sprintf("  - %s (model=%s)%s\n", p.Name, p.Model, marker))
		}
		return b.String()
	}

	info := h.app.GetMaclawLLMProviders()

	// Collect only configured providers for matching.
	var configured []MaclawLLMProvider
	for _, p := range info.Providers {
		if p.URL != "" || p.Key != "" || p.Model != "" {
			configured = append(configured, p)
		}
	}

	// Match: exact (case-insensitive) first, then substring fallback.
	needle := strings.ToLower(strings.TrimSpace(providerName))
	var match *MaclawLLMProvider
	for i := range configured {
		if strings.ToLower(configured[i].Name) == needle {
			match = &configured[i]
			break
		}
	}
	if match == nil {
		// Fuzzy: check if provider name contains the needle (not the reverse,
		// to avoid short provider names matching arbitrary long input).
		for i := range configured {
			lower := strings.ToLower(configured[i].Name)
			if strings.Contains(lower, needle) {
				match = &configured[i]
				break
			}
		}
	}

	if match == nil {
		var names []string
		for _, p := range configured {
			names = append(names, p.Name)
		}
		return fmt.Sprintf("未找到服务商 %q，可用: %s", providerName, strings.Join(names, ", "))
	}

	if match.Name == info.Current {
		return fmt.Sprintf("当前已经是 %s，无需切换", match.Name)
	}

	if err := h.app.SaveMaclawLLMProviders(info.Providers, match.Name); err != nil {
		return fmt.Sprintf("切换失败: %s", err.Error())
	}
	return fmt.Sprintf("✅ 已将 LLM 服务商切换为 %s (model=%s)", match.Name, match.Model)
}

// ---------------------------------------------------------------------------
// Scheduled task tool implementations
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) toolCreateScheduledTask(args map[string]interface{}) string {
	if h.scheduledTaskManager == nil {
		return "定时任务管理器未初始化"
	}
	name := stringVal(args, "name")
	action := stringVal(args, "action")
	if name == "" || action == "" {
		return "缺少 name 或 action 参数"
	}
	hour := -1
	if v, ok := args["hour"].(float64); ok {
		hour = int(v)
	}
	if hour < 0 || hour > 23 {
		return "hour 必须在 0-23 之间"
	}
	minute := 0
	if v, ok := args["minute"].(float64); ok {
		minute = int(v)
	}
	dow := -1
	if v, ok := args["day_of_week"].(float64); ok {
		dow = int(v)
	}
	dom := -1
	if v, ok := args["day_of_month"].(float64); ok {
		dom = int(v)
	}

	intervalMin := 0
	if v, ok := args["interval_minutes"].(float64); ok {
		intervalMin = int(v)
	}

	t := ScheduledTask{
		Name:            name,
		Action:          action,
		Hour:            hour,
		Minute:          minute,
		DayOfWeek:       dow,
		DayOfMonth:      dom,
		IntervalMinutes: intervalMin,
		StartDate:       stringVal(args, "start_date"),
		EndDate:         stringVal(args, "end_date"),
		TaskType:        stringVal(args, "task_type"),
	}

	id, err := h.scheduledTaskManager.Add(t)
	if err != nil {
		return fmt.Sprintf("创建定时任务失败: %s", err.Error())
	}

	// Notify frontend to refresh the scheduled tasks panel.
	h.app.emitEvent("scheduled-tasks-changed")

	// 非一次性任务同步到系统日历
	if created := h.scheduledTaskManager.Get(id); created != nil && isRecurringTask(created) {
		go func() {
			if err := SyncTaskToSystemCalendar(created); err != nil {
				h.app.log(fmt.Sprintf("[scheduled-task] calendar sync failed: %v", err))
			}
		}()
	}

	// Format next run time for display.
	if task := h.scheduledTaskManager.Get(id); task != nil && task.NextRunAt != nil {
		return fmt.Sprintf("✅ 定时任务已创建\nID: %s\n名称: %s\n操作: %s\n下次执行: %s", id, name, action, task.NextRunAt.Format("2006-01-02 15:04"))
	}
	return fmt.Sprintf("✅ 定时任务已创建（ID: %s）", id)
}

func (h *IMMessageHandler) toolListScheduledTasks() string {
	if h.scheduledTaskManager == nil {
		return "定时任务管理器未初始化"
	}
	tasks := h.scheduledTaskManager.List()
	if len(tasks) == 0 {
		return "当前没有定时任务。"
	}

	weekdays := []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("共 %d 个定时任务：\n\n", len(tasks)))
	for _, t := range tasks {
		b.WriteString(fmt.Sprintf("📋 [%s] %s\n", t.ID, t.Name))
		b.WriteString(fmt.Sprintf("   操作: %s\n", t.Action))

		// Schedule description
		var sched string
		if t.IntervalMinutes > 0 {
			sched = fmt.Sprintf("每%s（首次 %02d:%02d）", FormatInterval(t.IntervalMinutes), t.Hour, t.Minute)
		} else {
			sched = fmt.Sprintf("每天 %02d:%02d", t.Hour, t.Minute)
			if t.DayOfWeek >= 0 && t.DayOfWeek <= 6 {
				sched = fmt.Sprintf("每%s %02d:%02d", weekdays[t.DayOfWeek], t.Hour, t.Minute)
			}
			if t.DayOfMonth > 0 {
				sched = fmt.Sprintf("每月%d号 %02d:%02d", t.DayOfMonth, t.Hour, t.Minute)
			}
		}
		if t.StartDate != "" || t.EndDate != "" {
			sched += fmt.Sprintf("（%s ~ %s）", t.StartDate, t.EndDate)
		}
		b.WriteString(fmt.Sprintf("   时间: %s\n", sched))
		b.WriteString(fmt.Sprintf("   状态: %s", t.Status))
		if t.NextRunAt != nil {
			b.WriteString(fmt.Sprintf(" | 下次执行: %s", t.NextRunAt.Format("2006-01-02 15:04")))
		}
		if t.RunCount > 0 {
			b.WriteString(fmt.Sprintf(" | 已执行 %d 次", t.RunCount))
		}
		b.WriteString("\n\n")
	}
	return b.String()
}

func (h *IMMessageHandler) toolDeleteScheduledTask(args map[string]interface{}) string {
	if h.scheduledTaskManager == nil {
		return "定时任务管理器未初始化"
	}
	id := stringVal(args, "id")
	name := stringVal(args, "name")
	if id == "" && name == "" {
		return "请提供 id 或 name 参数"
	}
	var err error
	if id != "" {
		err = h.scheduledTaskManager.Delete(id)
	} else {
		err = h.scheduledTaskManager.DeleteByName(name)
	}
	if err != nil {
		return fmt.Sprintf("删除失败: %s", err.Error())
	}
	h.app.emitEvent("scheduled-tasks-changed")
	return "✅ 定时任务已删除"
}

func (h *IMMessageHandler) toolUpdateScheduledTask(args map[string]interface{}) string {
	if h.scheduledTaskManager == nil {
		return "定时任务管理器未初始化"
	}
	id := stringVal(args, "id")
	if id == "" {
		return "缺少 id 参数"
	}
	err := h.scheduledTaskManager.Update(id, args)
	if err != nil {
		return fmt.Sprintf("更新失败: %s", err.Error())
	}
	h.app.emitEvent("scheduled-tasks-changed")
	// Show updated task info.
	if t := h.scheduledTaskManager.Get(id); t != nil {
		next := "-"
		if t.NextRunAt != nil {
			next = t.NextRunAt.Format("2006-01-02 15:04")
		}
		return fmt.Sprintf("✅ 定时任务已更新\nID: %s\n名称: %s\n操作: %s\n时间: %02d:%02d\n下次执行: %s", t.ID, t.Name, t.Action, t.Hour, t.Minute, next)
	}
	return "✅ 定时任务已更新"
}

// ---------- ClawNet Knowledge Tools ----------

func (h *IMMessageHandler) toolClawNetSearch(args map[string]interface{}) string {
	if h.app.clawNetClient == nil || !h.app.clawNetClient.IsRunning() {
		return "虾网未连接，请先在设置中启用 ClawNet"
	}
	query := stringVal(args, "query")
	if query == "" {
		return "缺少 query 参数"
	}
	entries, err := h.app.clawNetClient.SearchKnowledge(query)
	if err != nil {
		return fmt.Sprintf("搜索失败: %s", err.Error())
	}
	if len(entries) == 0 {
		return fmt.Sprintf("未找到与「%s」相关的知识条目", query)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("🔍 虾网知识搜索「%s」— 找到 %d 条:\n\n", query, len(entries)))
	for i, e := range entries {
		if i >= 10 {
			b.WriteString(fmt.Sprintf("... 还有 %d 条结果\n", len(entries)-10))
			break
		}
		b.WriteString(fmt.Sprintf("%d. **%s**\n", i+1, e.Title))
		if e.Body != "" {
			body := e.Body
			if len(body) > 200 {
				body = body[:200] + "…"
			}
			b.WriteString(fmt.Sprintf("   %s\n", body))
		}
		if e.Author != "" {
			b.WriteString(fmt.Sprintf("   — %s", e.Author))
		}
		if e.Domain != "" {
			b.WriteString(fmt.Sprintf(" [%s]", e.Domain))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (h *IMMessageHandler) toolClawNetPublish(args map[string]interface{}) string {
	if h.app.clawNetClient == nil || !h.app.clawNetClient.IsRunning() {
		return "虾网未连接，请先在设置中启用 ClawNet"
	}
	title := stringVal(args, "title")
	body := stringVal(args, "body")
	if title == "" {
		return "缺少 title 参数"
	}
	if body == "" {
		return "缺少 body 参数"
	}
	entry, err := h.app.clawNetClient.PublishKnowledge(title, body)
	if err != nil {
		return fmt.Sprintf("发布失败: %s", err.Error())
	}
	return fmt.Sprintf("✅ 知识已发布到虾网\nID: %s\n标题: %s", entry.ID, entry.Title)
}

func (h *IMMessageHandler) toolQueryAuditLog(args map[string]interface{}) string {
	if h.app == nil || h.app.auditLog == nil {
		return "审计日志未初始化"
	}

	filter := AuditFilter{}
	if since := stringVal(args, "since"); since != "" {
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			filter.StartTime = &t
		}
	}
	if until := stringVal(args, "until"); until != "" {
		if t, err := time.Parse(time.RFC3339, until); err == nil {
			filter.EndTime = &t
		}
	}
	if tn := stringVal(args, "tool_name"); tn != "" {
		filter.ToolName = tn
	}
	if rl := stringVal(args, "risk_level"); rl != "" {
		filter.RiskLevels = []RiskLevel{RiskLevel(rl)}
	}

	entries, err := h.app.auditLog.Query(filter)
	if err != nil {
		return fmt.Sprintf("查询失败: %s", err.Error())
	}

	limit := 20
	if l, ok := args["limit"]; ok {
		if lf, ok := l.(float64); ok && lf > 0 {
			limit = int(lf)
		}
	}
	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}

	if len(entries) == 0 {
		return "没有找到匹配的审计记录"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("找到 %d 条审计记录:\n\n", len(entries)))
	for i, e := range entries {
		b.WriteString(fmt.Sprintf("%d. [%s] %s | 风险: %s | 决策: %s | 结果: %s\n",
			i+1, e.Timestamp.Format("01-02 15:04"), e.ToolName, e.RiskLevel, e.PolicyAction, e.Result))
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Web Search & Fetch Tools
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) toolWebSearch(args map[string]interface{}) string {
	query := stringVal(args, "query")
	if query == "" {
		return "缺少 query 参数"
	}
	maxResults := 8
	if n, ok := args["max_results"].(float64); ok && n > 0 {
		maxResults = int(n)
	}

	results, err := websearch.Search(query, maxResults)
	if err != nil {
		return fmt.Sprintf("搜索失败: %s", err.Error())
	}
	if len(results) == 0 {
		return "未找到相关结果"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("搜索 \"%s\" 找到 %d 条结果:\n\n", query, len(results)))
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n", i+1, r.Title, r.URL))
		if r.Snippet != "" {
			sb.WriteString(fmt.Sprintf("   %s\n", r.Snippet))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func (h *IMMessageHandler) toolWebFetch(args map[string]interface{}) string {
	rawURL := stringVal(args, "url")
	if rawURL == "" {
		return "缺少 url 参数"
	}

	opts := &websearch.FetchOptions{}
	if renderJS, ok := args["render_js"].(bool); ok {
		opts.RenderJS = renderJS
	}
	if savePath := stringVal(args, "save_path"); savePath != "" {
		opts.SavePath = resolvePath(savePath)
		opts.MaxBytes = 10 * 1024 * 1024 // 10MB for file downloads
	} else {
		// For text content, allow up to 2MB raw, we'll truncate the output
		opts.MaxBytes = 2 * 1024 * 1024
	}
	if t, ok := args["timeout"].(float64); ok && t > 0 {
		opts.TimeoutS = int(t)
	}

	result, err := websearch.Fetch(rawURL, opts)
	if err != nil {
		return fmt.Sprintf("抓取失败: %s", err.Error())
	}

	// If saved to file, return short message
	if result.SavedTo != "" {
		return result.Content
	}

	var sb strings.Builder
	if result.Title != "" {
		sb.WriteString(fmt.Sprintf("标题: %s\n", result.Title))
	}
	sb.WriteString(fmt.Sprintf("URL: %s\n", result.URL))
	sb.WriteString(fmt.Sprintf("类型: %s | 大小: %d 字节\n\n", result.ContentType, result.BytesRead))

	content := result.Content
	// web_fetch allows longer content: up to 16KB for text return
	const webFetchMaxContent = 16384
	if len(content) > webFetchMaxContent {
		headLen := webFetchMaxContent * 2 / 3
		tailLen := webFetchMaxContent - headLen - 60
		content = content[:headLen] + "\n\n... (内容已截断，共 " + fmt.Sprintf("%d", len(content)) + " 字符) ...\n\n" + content[len(content)-tailLen:]
	}
	sb.WriteString(content)
	return sb.String()
}

