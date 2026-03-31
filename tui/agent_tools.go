package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	configPkg "github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/project"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/security"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

// ===================== 会话管理扩展 =====================

func (h *TUIAgentHandler) toolCreateSession(args map[string]interface{}) string {
	if h.sessionMgr == nil {
		return "会话管理器未初始化"
	}
	toolName := stringArg(args, "tool")
	projectPath := stringArg(args, "project_path")
	if toolName == "" || projectPath == "" {
		return "错误: 缺少 tool 或 project_path 参数"
	}

	// 编程工具健康检查
	if h.codingToolHealth != nil {
		// 先查缓存：如果已标记为不可用/认证失败，直接拒绝
		if blocked, reason := h.codingToolHealth.IsBlocked(toolName); blocked {
			return codingToolFallbackHint(toolName, reason)
		}
		// 未检查过 → 执行预检查
		if _, checked := h.codingToolHealth.Get(toolName); !checked {
			ok, reason := checkCodingToolHealth(toolName)
			if ok {
				h.codingToolHealth.Set(toolName, codingToolAvailable, "")
			} else {
				h.codingToolHealth.Set(toolName, codingToolUnavailable, reason)
				return codingToolFallbackHint(toolName, reason)
			}
		}
	}

	spec := remote.LaunchSpec{
		Tool:        toolName,
		ProjectPath: projectPath,
		Title:       fmt.Sprintf("%s @ %s", toolName, filepath.Base(projectPath)),
	}
	sess, err := h.sessionMgr.Create(spec)
	if err != nil {
		return fmt.Sprintf("创建会话失败: %v", err)
	}
	return fmt.Sprintf("会话已创建: ID=%s, 工具=%s", sess.ID, toolName)
}

func (h *TUIAgentHandler) toolGetSessionOutput(args map[string]interface{}) string {
	if h.sessionMgr == nil {
		return "会话管理器未初始化"
	}
	sid := stringArg(args, "session_id")
	if sid == "" {
		return "错误: 缺少 session_id"
	}
	s, ok := h.sessionMgr.Get(sid)
	if !ok {
		return fmt.Sprintf("会话 %s 不存在", sid)
	}
	s.mu.Lock()
	lines := make([]string, len(s.PreviewLines))
	copy(lines, s.PreviewLines)
	status := s.Status
	stallState := s.StallState
	nudgeCount := s.NudgeCount
	lastOutputAt := s.LastOutputAt
	createdAt := s.CreatedAt
	stepProgress := s.StepProgress
	s.mu.Unlock()

	tailLines := intArg(args, "tail_lines", 0)
	if tailLines > 0 && len(lines) > tailLines {
		lines = lines[len(lines)-tailLines:]
	}

	// 构建诊断头
	var diag strings.Builder
	diag.WriteString(fmt.Sprintf("[diag] status=%s", string(status)))
	if !lastOutputAt.IsZero() {
		ago := time.Since(lastOutputAt).Truncate(time.Second)
		diag.WriteString(fmt.Sprintf(" last_output_ago=%s", ago))
	} else {
		diag.WriteString(fmt.Sprintf(" last_output_ago=never (created %s ago)", time.Since(createdAt).Truncate(time.Second)))
	}
	switch stallState {
	case remote.StallStateSuspected:
		diag.WriteString(fmt.Sprintf(" stall=suspected nudge_count=%d", nudgeCount))
	case remote.StallStateStuck:
		diag.WriteString(fmt.Sprintf(" stall=stuck nudge_count=%d", nudgeCount))
	}

	// 状态提示
	hint := sessionDiagHint(status, stallState, stepProgress)
	if hint != "" {
		diag.WriteString("\n" + hint)
	}

	if len(lines) == 0 {
		return diag.String() + "\n(无输出)"
	}

	// 运行时认证失败检测（仅在工具尚未标记为不可用时检测，且只扫描最近 50 行）
	output := strings.Join(lines, "\n")
	if h.codingToolHealth != nil {
		if toolName := h.sessionToolName(sid); toolName != "" {
			if blocked, _ := h.codingToolHealth.IsBlocked(toolName); !blocked {
				scanLines := lines
				if len(scanLines) > 50 {
					scanLines = scanLines[len(scanLines)-50:]
				}
				scanText := strings.Join(scanLines, "\n")
				if failed, pattern := DetectAuthFailure(scanText); failed {
					h.codingToolHealth.MarkAuthFailed(toolName,
						fmt.Sprintf("运行时检测到认证错误 (匹配: %s)", pattern))
					diag.WriteString(fmt.Sprintf("\n⚠️ 检测到认证失败，工具 %s 已标记为不可用。请使用 bash/read_file/write_file 等基础工具继续。", toolName))
				}
			}
		}
	}

	return diag.String() + "\n" + output
}

// sessionDiagHint 根据会话状态和停滞状态生成诊断提示。
func sessionDiagHint(status remote.SessionStatus, stallState remote.StallState, stepProgress string) string {
	var parts []string
	// Show step progress if available
	if stepProgress != "" {
		parts = append(parts, stepProgress)
	}
	switch status {
	case remote.SessionBusy, remote.SessionRunning:
		switch stallState {
		case remote.StallStateSuspected:
			parts = append(parts, i18n.T(i18n.MsgStallSuspected, "zh"))
		case remote.StallStateStuck:
			parts = append(parts, i18n.T(i18n.MsgStallStuck, "zh"))
		default:
			parts = append(parts, i18n.T(i18n.MsgToolWorking, "zh"))
		}
	case remote.SessionWaitingInput:
		parts = append(parts, i18n.T(i18n.MsgWaitingInput, "zh"))
	case remote.SessionExited:
		parts = append(parts, i18n.T(i18n.MsgSessionExited, "zh"))
	case remote.SessionError:
		parts = append(parts, i18n.T(i18n.MsgSessionError, "zh"))
	}
	return strings.Join(parts, "\n")
}

// sessionToolName 从会话 ID 获取对应的工具名称。
func (h *TUIAgentHandler) sessionToolName(sessionID string) string {
	if h.sessionMgr == nil {
		return ""
	}
	s, ok := h.sessionMgr.Get(sessionID)
	if !ok {
		return ""
	}
	return s.Spec.Tool
}

func (h *TUIAgentHandler) toolGetSessionEvents(args map[string]interface{}) string {
	if h.sessionMgr == nil {
		return "会话管理器未初始化"
	}
	sid := stringArg(args, "session_id")
	if sid == "" {
		return "错误: 缺少 session_id"
	}
	s, ok := h.sessionMgr.Get(sid)
	if !ok {
		return fmt.Sprintf("会话 %s 不存在", sid)
	}
	s.mu.Lock()
	events := make([]remote.ImportantEvent, len(s.Events))
	copy(events, s.Events)
	s.mu.Unlock()
	if len(events) == 0 {
		return "(无事件)"
	}
	var sb strings.Builder
	for _, e := range events {
		sb.WriteString(fmt.Sprintf("[%s] %s\n", e.Type, e.Summary))
	}
	return sb.String()
}

func (h *TUIAgentHandler) toolInterruptSession(args map[string]interface{}) string {
	if h.sessionMgr == nil {
		return "会话管理器未初始化"
	}
	sid := stringArg(args, "session_id")
	if sid == "" {
		return "错误: 缺少 session_id"
	}
	if err := h.sessionMgr.Interrupt(sid); err != nil {
		return fmt.Sprintf("中断失败: %v", err)
	}
	return "已发送中断信号"
}

func (h *TUIAgentHandler) toolKillSession(args map[string]interface{}) string {
	if h.sessionMgr == nil {
		return "会话管理器未初始化"
	}
	sid := stringArg(args, "session_id")
	if sid == "" {
		return "错误: 缺少 session_id"
	}
	if err := h.sessionMgr.Kill(sid); err != nil {
		return fmt.Sprintf("终止失败: %v", err)
	}
	return "会话已终止"
}

func (h *TUIAgentHandler) toolSendAndObserve(args map[string]interface{}) string {
	if h.sessionMgr == nil {
		return "会话管理器未初始化"
	}
	sid := stringArg(args, "session_id")
	text := stringArg(args, "text")
	if sid == "" || text == "" {
		return "错误: 缺少 session_id 或 text"
	}
	waitSec := intArg(args, "wait_seconds", 3)
	if waitSec < 1 {
		waitSec = 1
	}
	if waitSec > 30 {
		waitSec = 30
	}

	s, ok := h.sessionMgr.Get(sid)
	if !ok {
		return fmt.Sprintf("会话 %s 不存在", sid)
	}
	s.mu.Lock()
	beforeLen := len(s.PreviewLines)
	s.mu.Unlock()

	if err := h.sessionMgr.WriteInput(sid, text); err != nil {
		return fmt.Sprintf("发送失败: %v", err)
	}
	time.Sleep(time.Duration(waitSec) * time.Second)

	s.mu.Lock()
	var newLines []string
	if len(s.PreviewLines) > beforeLen {
		newLines = make([]string, len(s.PreviewLines)-beforeLen)
		copy(newLines, s.PreviewLines[beforeLen:])
	}
	status := s.Status
	stallState := s.StallState
	nudgeCount := s.NudgeCount
	lastOutputAt := s.LastOutputAt
	stepProgress2 := s.StepProgress
	s.mu.Unlock()

	// 诊断头
	var diag strings.Builder
	diag.WriteString(fmt.Sprintf("[diag] status=%s", string(status)))
	if !lastOutputAt.IsZero() {
		ago := time.Since(lastOutputAt).Truncate(time.Second)
		diag.WriteString(fmt.Sprintf(" last_output_ago=%s", ago))
	} else {
		diag.WriteString(" last_output_ago=never")
	}
	if stallState == remote.StallStateSuspected {
		diag.WriteString(fmt.Sprintf(" stall=suspected nudge_count=%d", nudgeCount))
	} else if stallState == remote.StallStateStuck {
		diag.WriteString(fmt.Sprintf(" stall=stuck nudge_count=%d", nudgeCount))
	}
	hint := sessionDiagHint(status, stallState, stepProgress2)
	if hint != "" {
		diag.WriteString("\n" + hint)
	}

	if len(newLines) == 0 {
		return diag.String() + "\n(等待后无新输出)"
	}

	// 运行时认证失败检测（仅在工具尚未标记为不可用时检测）
	output := strings.Join(newLines, "\n")
	if h.codingToolHealth != nil {
		if toolName := h.sessionToolName(sid); toolName != "" {
			if blocked, _ := h.codingToolHealth.IsBlocked(toolName); !blocked {
				if failed, pattern := DetectAuthFailure(output); failed {
					h.codingToolHealth.MarkAuthFailed(toolName,
						fmt.Sprintf("运行时检测到认证错误 (匹配: %s)", pattern))
					diag.WriteString(fmt.Sprintf("\n⚠️ 检测到认证失败，工具 %s 已标记为不可用。请使用 bash/read_file/write_file 等基础工具继续。", toolName))
				}
			}
		}
	}

	return diag.String() + "\n" + output
}

func (h *TUIAgentHandler) toolControlSession(args map[string]interface{}) string {
	if h.sessionMgr == nil {
		return "会话管理器未初始化"
	}
	sid := stringArg(args, "session_id")
	action := stringArg(args, "action")
	if sid == "" || action == "" {
		return "错误: 缺少 session_id 或 action"
	}
	switch action {
	case "pause":
		return "暂停功能暂不支持本地 PTY 会话"
	case "resume":
		return "恢复功能暂不支持本地 PTY 会话"
	case "restart":
		if err := h.sessionMgr.Kill(sid); err != nil {
			return fmt.Sprintf("重启失败（终止阶段）: %v", err)
		}
		s, ok := h.sessionMgr.Get(sid)
		if !ok {
			return "会话已终止但无法重启（会话不存在）"
		}
		newSess, err := h.sessionMgr.Create(s.Spec)
		if err != nil {
			return fmt.Sprintf("重启失败（创建阶段）: %v", err)
		}
		return fmt.Sprintf("会话已重启: 新 ID=%s", newSess.ID)
	default:
		return fmt.Sprintf("未知操作: %s (支持: pause/resume/restart)", action)
	}
}

// ===================== 配置管理 =====================

func (h *TUIAgentHandler) toolGetConfig(args map[string]interface{}) string {
	mgr := h.getConfigMgr()
	section := stringArg(args, "section")
	if section == "" {
		section = "all"
	}
	result, err := mgr.GetConfig(section, true)
	if err != nil {
		return fmt.Sprintf("读取配置失败: %v", err)
	}
	return result
}

func (h *TUIAgentHandler) toolUpdateConfig(args map[string]interface{}) string {
	mgr := h.getConfigMgr()
	section := stringArg(args, "section")
	key := stringArg(args, "key")
	value := stringArg(args, "value")
	if section == "" || key == "" {
		return "错误: 缺少 section 或 key"
	}
	oldVal, err := mgr.UpdateConfig(section, key, value)
	if err != nil {
		return fmt.Sprintf("更新失败: %v", err)
	}
	return fmt.Sprintf("已更新 %s.%s: %s → %s", section, key, oldVal, value)
}

func (h *TUIAgentHandler) toolBatchUpdateConfig(args map[string]interface{}) string {
	mgr := h.getConfigMgr()
	changesRaw, ok := args["changes"]
	if !ok {
		return "错误: 缺少 changes 参数"
	}
	data, _ := json.Marshal(changesRaw)
	var changes []configChange
	if err := json.Unmarshal(data, &changes); err != nil {
		return fmt.Sprintf("解析 changes 失败: %v", err)
	}
	var cfgChanges []configPkg.ConfigChange
	for _, c := range changes {
		cfgChanges = append(cfgChanges, configPkg.ConfigChange{
			Section: c.Section, Key: c.Key, Value: c.Value,
		})
	}
	if err := mgr.BatchUpdate(cfgChanges); err != nil {
		return fmt.Sprintf("批量更新失败: %v", err)
	}
	return fmt.Sprintf("已批量更新 %d 项配置", len(cfgChanges))
}

type configChange struct {
	Section string `json:"section"`
	Key     string `json:"key"`
	Value   string `json:"value"`
}

func (h *TUIAgentHandler) toolListConfigSchema() string {
	mgr := h.getConfigMgr()
	result, err := mgr.SchemaJSON()
	if err != nil {
		return fmt.Sprintf("获取配置模式失败: %v", err)
	}
	return result
}

func (h *TUIAgentHandler) toolExportConfig() string {
	mgr := h.getConfigMgr()
	result, err := mgr.ExportConfig()
	if err != nil {
		return fmt.Sprintf("导出配置失败: %v", err)
	}
	return result
}

func (h *TUIAgentHandler) toolImportConfig(args map[string]interface{}) string {
	mgr := h.getConfigMgr()
	jsonData := stringArg(args, "json_data")
	if jsonData == "" {
		return "错误: 缺少 json_data"
	}
	report, err := mgr.ImportConfig(jsonData)
	if err != nil {
		return fmt.Sprintf("导入失败: %v", err)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("导入完成: 应用 %d 项, 跳过 %d 项\n", report.Applied, report.Skipped))
	for _, w := range report.Warnings {
		sb.WriteString(fmt.Sprintf("  ⚠ %s\n", w))
	}
	return sb.String()
}

func (h *TUIAgentHandler) getConfigMgr() *configPkg.Manager {
	if h.configMgr != nil {
		return h.configMgr
	}
	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	return configPkg.NewManager(store)
}

// ===================== 模板 =====================

func (h *TUIAgentHandler) toolCreateTemplate(args map[string]interface{}) string {
	name := stringArg(args, "name")
	toolName := stringArg(args, "tool")
	projectPath := stringArg(args, "project_path")
	if name == "" || toolName == "" || projectPath == "" {
		return "错误: 缺少 name、tool 或 project_path"
	}
	tmpl := map[string]string{
		"name": name, "tool": toolName, "project_path": projectPath,
	}
	data, _ := json.MarshalIndent(tmpl, "", "  ")
	dir := filepath.Join(commands.ResolveDataDir(), "templates")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, name+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Sprintf("创建模板失败: %v", err)
	}
	return fmt.Sprintf("模板已创建: %s", path)
}

func (h *TUIAgentHandler) toolListTemplates() string {
	dir := filepath.Join(commands.ResolveDataDir(), "templates")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "无模板"
		}
		return fmt.Sprintf("读取模板目录失败: %v", err)
	}
	var sb strings.Builder
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			sb.WriteString(strings.TrimSuffix(e.Name(), ".json") + "\n")
		}
	}
	if sb.Len() == 0 {
		return "无模板"
	}
	return sb.String()
}

func (h *TUIAgentHandler) toolLaunchTemplate(args map[string]interface{}) string {
	if h.sessionMgr == nil {
		return "会话管理器未初始化"
	}
	name := stringArg(args, "template_name")
	if name == "" {
		return "错误: 缺少 template_name"
	}
	path := filepath.Join(commands.ResolveDataDir(), "templates", name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("模板 %s 不存在", name)
	}
	var tmpl map[string]string
	if err := json.Unmarshal(data, &tmpl); err != nil {
		return fmt.Sprintf("解析模板失败: %v", err)
	}
	spec := remote.LaunchSpec{
		Tool:        tmpl["tool"],
		ProjectPath: tmpl["project_path"],
		Title:       fmt.Sprintf("%s (模板: %s)", tmpl["tool"], name),
	}
	sess, err := h.sessionMgr.Create(spec)
	if err != nil {
		return fmt.Sprintf("从模板启动失败: %v", err)
	}
	return fmt.Sprintf("会话已从模板启动: ID=%s", sess.ID)
}

// ===================== 定时任务 =====================

func (h *TUIAgentHandler) toolCreateScheduledTask(args map[string]interface{}) string {
	if h.schedulerMgr == nil {
		return "定时任务管理器未初始化"
	}
	task := scheduler.ScheduledTask{
		Name:            stringArg(args, "name"),
		Action:          stringArg(args, "action"),
		Hour:            intArg(args, "hour", 0),
		Minute:          intArg(args, "minute", 0),
		DayOfWeek:       intArg(args, "day_of_week", -1),
		DayOfMonth:      intArg(args, "day_of_month", -1),
		IntervalMinutes: intArg(args, "interval_minutes", 0),
		TaskType:        stringArg(args, "task_type"),
	}
	id, err := h.schedulerMgr.Add(task)
	if err != nil {
		return fmt.Sprintf("创建定时任务失败: %v", err)
	}
	return fmt.Sprintf("定时任务已创建: ID=%s", id)
}

func (h *TUIAgentHandler) toolListScheduledTasks() string {
	if h.schedulerMgr == nil {
		return "定时任务管理器未初始化"
	}
	tasks := h.schedulerMgr.List()
	if len(tasks) == 0 {
		return "无定时任务"
	}
	var sb strings.Builder
	for _, t := range tasks {
		next := "N/A"
		if t.NextRunAt != nil {
			next = t.NextRunAt.Format("2006-01-02 15:04")
		}
		taskType := t.TaskType
		if taskType == "" {
			taskType = "reminder"
		}
		schedStr := fmt.Sprintf("%02d:%02d", t.Hour, t.Minute)
		if t.IntervalMinutes > 0 {
			schedStr = "每" + scheduler.FormatInterval(t.IntervalMinutes)
		}
		sb.WriteString(fmt.Sprintf("ID: %s  名称: %s  类型: %s  状态: %s  周期: %s  下次: %s\n", t.ID, t.Name, taskType, t.Status, schedStr, next))
	}
	return sb.String()
}

func (h *TUIAgentHandler) toolDeleteScheduledTask(args map[string]interface{}) string {
	if h.schedulerMgr == nil {
		return "定时任务管理器未初始化"
	}
	taskID := stringArg(args, "task_id")
	if taskID == "" {
		return "错误: 缺少 task_id"
	}
	if err := h.schedulerMgr.Delete(taskID); err != nil {
		return fmt.Sprintf("删除失败: %v", err)
	}
	return "定时任务已删除"
}

func (h *TUIAgentHandler) toolUpdateScheduledTask(args map[string]interface{}) string {
	if h.schedulerMgr == nil {
		return "定时任务管理器未初始化"
	}
	taskID := stringArg(args, "task_id")
	if taskID == "" {
		return "错误: 缺少 task_id"
	}
	updates, ok := args["updates"].(map[string]interface{})
	if !ok {
		return "错误: updates 参数格式不正确"
	}
	if err := h.schedulerMgr.Update(taskID, updates); err != nil {
		return fmt.Sprintf("更新失败: %v", err)
	}
	return "定时任务已更新"
}

// ===================== 记忆 =====================

func (h *TUIAgentHandler) toolMemory(args map[string]interface{}) string {
	if h.memoryStore == nil {
		return "记忆存储未初始化"
	}
	action := stringArg(args, "action")
	switch action {
	case "save":
		content := stringArg(args, "content")
		if content == "" {
			return "错误: 缺少 content"
		}
		cat := memory.Category(stringArg(args, "category"))
		if cat == "" {
			cat = memory.CategoryProjectKnowledge
		}
		var tags []string
		if rawTags, ok := args["tags"]; ok {
			data, _ := json.Marshal(rawTags)
			_ = json.Unmarshal(data, &tags)
		}
		entry := memory.Entry{Content: content, Category: cat, Tags: tags}
		if err := h.memoryStore.Save(entry); err != nil {
			return fmt.Sprintf("保存失败: %v", err)
		}
		return "记忆已保存"
	case "list":
		cat := memory.Category(stringArg(args, "category"))
		keyword := stringArg(args, "keyword")
		entries := h.memoryStore.List(cat, keyword)
		if len(entries) == 0 {
			return "无匹配记忆"
		}
		var sb strings.Builder
		for _, e := range entries {
			prefix := ""
			if e.Pinned {
				prefix = "📌 "
			}
			sb.WriteString(fmt.Sprintf("%s[%s] %s: %s (tags: %s)\n", prefix, e.ID, e.Category, scheduler.TruncateStr(e.Content, 80), strings.Join(e.Tags, ",")))
		}
		return sb.String()
	case "search":
		cat := memory.Category(stringArg(args, "category"))
		keyword := stringArg(args, "keyword")
		entries := h.memoryStore.Search(cat, keyword, 20)
		if len(entries) == 0 {
			return "无匹配记忆"
		}
		var sb strings.Builder
		for _, e := range entries {
			prefix := ""
			if e.Pinned {
				prefix = "📌 "
			}
			sb.WriteString(fmt.Sprintf("%s[%s] %s: %s\n", prefix, e.ID, e.Category, scheduler.TruncateStr(e.Content, 100)))
		}
		return sb.String()
	case "delete":
		id := stringArg(args, "id")
		if id == "" {
			return "错误: 缺少 id"
		}
		if err := h.memoryStore.Delete(id); err != nil {
			return fmt.Sprintf("删除失败: %v", err)
		}
		return "记忆已删除"
	case "pin":
		id := stringArg(args, "id")
		if id == "" {
			return "错误: 缺少 id"
		}
		if err := h.memoryStore.PinEntry(id); err != nil {
			return fmt.Sprintf("钉住失败: %v", err)
		}
		return fmt.Sprintf("📌 已钉住记忆 %s", id)
	case "unpin":
		id := stringArg(args, "id")
		if id == "" {
			return "错误: 缺少 id"
		}
		if err := h.memoryStore.UnpinEntry(id); err != nil {
			return fmt.Sprintf("取消钉住失败: %v", err)
		}
		return fmt.Sprintf("已取消钉住记忆 %s", id)
	case "list_archive":
		cat := memory.Category(stringArg(args, "category"))
		keyword := stringArg(args, "keyword")
		entries := h.memoryStore.ListArchive(cat, keyword)
		if len(entries) == 0 {
			return "无归档记忆"
		}
		var sb strings.Builder
		for _, e := range entries {
			sb.WriteString(fmt.Sprintf("[%s] %s: %s (tags: %s)\n", e.ID, e.Category, scheduler.TruncateStr(e.Content, 80), strings.Join(e.Tags, ",")))
		}
		return sb.String()
	case "restore":
		id := stringArg(args, "id")
		if id == "" {
			return "错误: 缺少 id"
		}
		if err := h.memoryStore.RestoreFromArchive(id); err != nil {
			return fmt.Sprintf("恢复失败: %v", err)
		}
		return fmt.Sprintf("已从归档恢复记忆 %s", id)
	default:
		return "错误: action 必须是 save/list/search/delete/pin/unpin/list_archive/restore"
	}
}

// ===================== MCP =====================

func (h *TUIAgentHandler) toolListMCPTools() string {
	if h.defGenerator == nil {
		return "MCP 工具提供者未初始化"
	}
	// 通过 DefinitionGenerator 获取所有工具，过滤出非 builtin 的
	allDefs := h.defGenerator.Generate()
	var mcpTools []string
	for _, def := range allDefs {
		name := toolExtractName(def)
		if name != "" && !isOriginalBuiltin(name) {
			desc := toolExtractDesc(def)
			mcpTools = append(mcpTools, fmt.Sprintf("  %s: %s", name, desc))
		}
	}
	if len(mcpTools) == 0 {
		return "无 MCP 工具（未配置或服务器不健康）"
	}
	return "MCP 工具列表:\n" + strings.Join(mcpTools, "\n")
}

func (h *TUIAgentHandler) toolCallMCPTool(args map[string]interface{}) string {
	// MCP 工具调用通过 DefinitionGenerator 动态注册的工具名直接路由
	// 这里作为显式调用入口
	serverID := stringArg(args, "server_id")
	toolName := stringArg(args, "tool_name")
	if serverID == "" || toolName == "" {
		return "错误: 缺少 server_id 或 tool_name"
	}
	return fmt.Sprintf("MCP 工具调用: server=%s, tool=%s (需要通过 MCP 协议转发，当前 TUI 暂不支持直接调用)", serverID, toolName)
}

func toolExtractName(def map[string]interface{}) string {
	fn, ok := def["function"].(map[string]interface{})
	if !ok {
		return ""
	}
	name, _ := fn["name"].(string)
	return name
}

func toolExtractDesc(def map[string]interface{}) string {
	fn, ok := def["function"].(map[string]interface{})
	if !ok {
		return ""
	}
	desc, _ := fn["description"].(string)
	return desc
}

func isOriginalBuiltin(name string) bool {
	builtins := map[string]bool{
		"bash": true, "read_file": true, "write_file": true, "list_directory": true,
		"list_sessions": true, "send_input": true, "create_session": true,
		"get_session_output": true, "get_session_events": true,
		"interrupt_session": true, "kill_session": true, "send_and_observe": true,
		"control_session": true, "get_config": true, "update_config": true,
		"batch_update_config": true, "list_config_schema": true,
		"export_config": true, "import_config": true,
		"create_template": true, "list_templates": true, "launch_template": true,
		"create_scheduled_task": true, "list_scheduled_tasks": true,
		"delete_scheduled_task": true, "update_scheduled_task": true,
		"memory": true, "list_mcp_tools": true, "call_mcp_tool": true,
		"list_skills": true, "search_skill_hub": true, "install_skill_hub": true,
		"run_skill": true, "clawnet_search": true, "clawnet_publish": true,
		"query_audit_log": true, "send_file": true, "parallel_execute": true,
		"switch_llm_provider": true, "set_max_iterations": true,
		"recommend_tool": true, "screenshot": true,
		"project_manage": true, "web_search": true, "web_fetch": true,
	}
	return builtins[name]
}

// ===================== 技能 =====================

func (h *TUIAgentHandler) toolListSkills() string {
	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Sprintf("加载配置失败: %v", err)
	}
	if len(cfg.NLSkills) == 0 {
		return "无已安装技能"
	}
	var sb strings.Builder
	for _, sk := range cfg.NLSkills {
		status := sk.Status
		if status == "" {
			status = "active"
		}
		sb.WriteString(fmt.Sprintf("  %s: %s [%s]\n", sk.Name, sk.Description, status))
	}
	return sb.String()
}

func (h *TUIAgentHandler) toolSearchSkillHub(args map[string]interface{}) string {
	query := stringArg(args, "query")
	if query == "" {
		return "错误: 缺少 query"
	}

	var b strings.Builder
	found := 0

	// 1) SkillMarket (HubCenter)
	base := commands.ResolveHubCenterURL()
	if base != "" {
		smResults, err := commands.SearchSkillMarket(base, query, 10)
		if err == nil && len(smResults) > 0 {
			b.WriteString(fmt.Sprintf("## SkillMarket 结果 (%d 个)\n", len(smResults)))
			for _, r := range smResults {
				b.WriteString(fmt.Sprintf("- %s: %s (source: skillmarket)\n", r.Name, r.Description))
			}
			found += len(smResults)
		}
	}

	// 2) SkillHub
	hubResults, err := commands.SearchSkillHub(query)
	if err == nil && len(hubResults) > 0 {
		b.WriteString(fmt.Sprintf("## SkillHub 结果 (%d 个)\n", len(hubResults)))
		for _, r := range hubResults {
			b.WriteString(fmt.Sprintf("- %s: %s (source: skillhub)\n", r.Name, r.Description))
		}
		found += len(hubResults)
	}

	// 3) GitHub fallback
	if found == 0 {
		gs := cskill.NewGitHubSearcher("")
		candidates, ghErr := gs.SearchGitHub(query)
		if ghErr == nil && len(candidates) > 0 {
			limit := len(candidates)
			if limit > 5 {
				limit = 5
			}
			b.WriteString(fmt.Sprintf("## GitHub 结果 (%d 个)\n", limit))
			for _, c := range candidates[:limit] {
				b.WriteString(fmt.Sprintf("- %s: %s (★%d, source: github, url: %s)\n",
					c.RepoFullName, c.Description, c.Stars, c.RepoURL))
			}
			found += limit
		}
	}

	if found == 0 {
		return fmt.Sprintf("在 SkillMarket、SkillHub 和 GitHub 上均未找到与 %q 相关的 Skill", query)
	}

	return b.String()
}

func (h *TUIAgentHandler) toolInstallSkillHub(args map[string]interface{}) string {
	skillName := stringArg(args, "skill_name")
	if skillName == "" {
		return "错误: 缺少 skill_name"
	}
	return fmt.Sprintf("请使用 CLI: maclaw-tui skillhub install %s", skillName)
}

func (h *TUIAgentHandler) toolRunSkill(args map[string]interface{}) string {
	skillName := stringArg(args, "skill_name")
	if skillName == "" {
		return "错误: 缺少 skill_name"
	}
	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Sprintf("加载配置失败: %v", err)
	}

	var skill *corelib.NLSkillEntry
	for i := range cfg.NLSkills {
		if cfg.NLSkills[i].Name == skillName {
			skill = &cfg.NLSkills[i]
			break
		}
	}
	if skill == nil {
		return fmt.Sprintf("技能 %s 不存在", skillName)
	}
	if skill.Status == "disabled" {
		return fmt.Sprintf("技能 %s 已禁用", skillName)
	}
	if len(skill.Steps) == 0 {
		return fmt.Sprintf("技能 %s 没有定义步骤", skillName)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("▶ 执行技能: %s (%d 步)\n", skill.Name, len(skill.Steps)))
	allSuccess := true
	startTime := time.Now()

	for i, step := range skill.Steps {
		stepStart := time.Now()
		sb.WriteString(fmt.Sprintf("\n── 步骤 %d/%d: %s ──\n", i+1, len(skill.Steps), step.Action))

		output, execErr := runSkillStep(step, skill.SkillDir)
		elapsed := time.Since(stepStart).Truncate(time.Millisecond)

		if execErr != nil {
			sb.WriteString(fmt.Sprintf("[FAIL] %s (耗时 %s)\n", execErr.Error(), elapsed))
			if output != "" {
				appendTruncated(&sb, output, 2048)
			}
			allSuccess = false
			if step.OnError != "continue" {
				sb.WriteString("⛔ 步骤失败且 on_error!=continue，终止执行\n")
				break
			}
			sb.WriteString("⚠️ 步骤失败但 on_error=continue，继续下一步\n")
		} else {
			sb.WriteString(fmt.Sprintf("[OK] 耗时 %s\n", elapsed))
			if output != "" {
				appendTruncated(&sb, output, 2048)
			}
		}
	}

	totalElapsed := time.Since(startTime).Truncate(time.Millisecond)
	if allSuccess {
		sb.WriteString(fmt.Sprintf("\n✅ 技能 '%s' 全部完成 (总耗时 %s)\n", skill.Name, totalElapsed))
	} else {
		sb.WriteString(fmt.Sprintf("\n❌ 技能 '%s' 执行失败 (总耗时 %s)\n", skill.Name, totalElapsed))
	}

	// 更新使用统计
	skill.UsageCount++
	skill.LastUsedAt = time.Now().Format(time.RFC3339)
	if allSuccess {
		skill.SuccessCount++
		skill.LastError = ""
	} else {
		// 记录最后一个错误到 skill 元数据
		skill.LastError = "执行失败，详见输出"
	}
	_ = store.SaveConfig(cfg)

	return sb.String()
}

// runSkillStep 执行单个 skill 步骤，支持流式输出收集。
func runSkillStep(step corelib.NLSkillStep, skillDir string) (string, error) {
	switch step.Action {
	case "bash":
		command, _ := step.Params["command"].(string)
		if command == "" {
			return "", fmt.Errorf("missing command parameter")
		}
		return runSkillBashStreaming(command, step.Params, skillDir)
	default:
		return "", fmt.Errorf("unsupported action: %s", step.Action)
	}
}

// runSkillBashStreaming 执行 bash 命令，使用流式输出收集而非等待完成。
// 每秒检查一次输出，超时后报告已收集的部分输出。
func runSkillBashStreaming(command string, params map[string]interface{}, skillDir string) (string, error) {
	timeout := 30
	if t, ok := params["timeout"].(float64); ok && t > 0 {
		timeout = int(t)
		if timeout > 120 {
			timeout = 120
		}
	}

	workDir, _ := params["working_dir"].(string)
	if workDir == "" && skillDir != "" {
		workDir = skillDir
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	var shellName string
	var shellArgs []string
	if runtime.GOOS == "windows" {
		shellName = "powershell"
		shellArgs = []string{"-NoProfile", "-NonInteractive", "-Command", command}
	} else {
		shellName = "bash"
		shellArgs = []string{"-c", command}
	}

	cmd := exec.CommandContext(ctx, shellName, shellArgs...)
	if workDir != "" {
		cmd.Dir = workDir
	}

	// 使用 pipe 实现流式读取
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("创建 stdout pipe 失败: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("创建 stderr pipe 失败: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("启动命令失败: %w", err)
	}

	// 并发收集 stdout 和 stderr
	var mu sync.Mutex
	var stdoutBuf, stderrBuf strings.Builder
	var wg sync.WaitGroup

	collect := func(pipe io.ReadCloser, buf *strings.Builder) {
		defer wg.Done()
		const maxBufSize = 64 * 1024 // 64KB per stream
		tmp := make([]byte, 4096)
		for {
			n, readErr := pipe.Read(tmp)
			if n > 0 {
				mu.Lock()
				if buf.Len() < maxBufSize {
					remaining := maxBufSize - buf.Len()
					if n > remaining {
						n = remaining
					}
					buf.Write(tmp[:n])
				}
				mu.Unlock()
			}
			if readErr != nil {
				return
			}
		}
	}

	wg.Add(2)
	go collect(stdoutPipe, &stdoutBuf)
	go collect(stderrPipe, &stderrBuf)

	wg.Wait()
	cmdErr := cmd.Wait()

	mu.Lock()
	stdout := stdoutBuf.String()
	stderr := stderrBuf.String()
	mu.Unlock()

	var b strings.Builder
	if len(stdout) > 0 {
		b.WriteString(stdout)
	}
	if len(stderr) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("[stderr] ")
		b.WriteString(stderr)
	}

	if cmdErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			b.WriteString(fmt.Sprintf("\n[timeout] 命令超时 (%ds)，已收集部分输出", timeout))
		}
		return b.String(), cmdErr
	}
	if b.Len() == 0 {
		return "(completed, no output)", nil
	}
	return b.String(), nil
}

// appendTruncated 将 text 追加到 sb，超过 maxLen 时截断。
func appendTruncated(sb *strings.Builder, text string, maxLen int) {
	if len(text) > maxLen {
		sb.WriteString(text[:maxLen])
		sb.WriteString("\n... (truncated)\n")
	} else {
		sb.WriteString(text)
		if !strings.HasSuffix(text, "\n") {
			sb.WriteString("\n")
		}
	}
}

// ===================== ClawNet =====================

func (h *TUIAgentHandler) toolClawnetSearch(args map[string]interface{}) string {
	if h.clawnetClient == nil {
		return "ClawNet 客户端未初始化"
	}
	query := stringArg(args, "query")
	if query == "" {
		return "错误: 缺少 query"
	}
	entries, err := h.clawnetClient.SearchKnowledge(query)
	if err != nil {
		return fmt.Sprintf("搜索失败: %v", err)
	}
	if len(entries) == 0 {
		return "无匹配结果"
	}
	var sb strings.Builder
	for _, e := range entries {
		sb.WriteString(fmt.Sprintf("[%s] %s (by %s, ↑%d)\n", e.ID, e.Title, e.Author, e.Upvotes))
	}
	return sb.String()
}

func (h *TUIAgentHandler) toolClawnetPublish(args map[string]interface{}) string {
	if h.clawnetClient == nil {
		return "ClawNet 客户端未初始化"
	}
	title := stringArg(args, "title")
	body := stringArg(args, "body")
	if title == "" || body == "" {
		return "错误: 缺少 title 或 body"
	}
	entry, err := h.clawnetClient.PublishKnowledge(title, body)
	if err != nil {
		return fmt.Sprintf("发布失败: %v", err)
	}
	return fmt.Sprintf("已发布: ID=%s, 标题=%s", entry.ID, entry.Title)
}

// ===================== 审计 =====================

func (h *TUIAgentHandler) toolQueryAuditLog(args map[string]interface{}) string {
	if h.auditLog == nil {
		return "审计日志未初始化"
	}
	filter := security.AuditFilter{
		ToolName: stringArg(args, "tool_name"),
	}
	if rl := stringArg(args, "risk_level"); rl != "" {
		filter.RiskLevels = []security.RiskLevel{security.RiskLevel(rl)}
	}
	if sd := stringArg(args, "start_date"); sd != "" {
		if t, err := time.Parse("2006-01-02", sd); err == nil {
			filter.StartTime = &t
		}
	}
	if ed := stringArg(args, "end_date"); ed != "" {
		if t, err := time.Parse("2006-01-02", ed); err == nil {
			filter.EndTime = &t
		}
	}
	entries, err := h.auditLog.Query(filter)
	if err != nil {
		return fmt.Sprintf("查询失败: %v", err)
	}
	if len(entries) == 0 {
		return "无匹配审计记录"
	}
	var sb strings.Builder
	for _, e := range entries {
		sb.WriteString(fmt.Sprintf("[%s] %s risk=%s action=%s result=%s\n",
			e.Timestamp.Format("01-02 15:04"), e.ToolName, e.RiskLevel, e.PolicyAction, scheduler.TruncateStr(e.Result, 60)))
	}
	return sb.String()
}

// ===================== 实用工具 =====================

func (h *TUIAgentHandler) toolSendFile(args map[string]interface{}) string {
	if h.sessionMgr == nil {
		return "会话管理器未初始化"
	}
	sid := stringArg(args, "session_id")
	filePath := stringArg(args, "file_path")
	if sid == "" || filePath == "" {
		return "错误: 缺少 session_id 或 file_path"
	}
	filePath = resolvePath(filePath)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Sprintf("读取文件失败: %v", err)
	}
	if err := h.sessionMgr.WriteInput(sid, string(data)); err != nil {
		return fmt.Sprintf("发送失败: %v", err)
	}
	return fmt.Sprintf("已发送文件内容 (%d 字节) 到会话 %s", len(data), sid)
}

func (h *TUIAgentHandler) toolParallelExecute(args map[string]interface{}) string {
	cmdsRaw, ok := args["commands"]
	if !ok {
		return "错误: 缺少 commands"
	}
	data, _ := json.Marshal(cmdsRaw)
	var cmds []string
	if err := json.Unmarshal(data, &cmds); err != nil {
		return fmt.Sprintf("解析 commands 失败: %v", err)
	}
	if len(cmds) == 0 {
		return "错误: commands 为空"
	}
	if len(cmds) > 10 {
		cmds = cmds[:10]
	}

	type cmdResult struct {
		index  int
		output string
	}
	results := make([]cmdResult, len(cmds))
	var wg sync.WaitGroup
	for i, c := range cmds {
		wg.Add(1)
		go func(idx int, command string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var cmd *exec.Cmd
			if runtime.GOOS == "windows" {
				cmd = exec.CommandContext(ctx, "cmd", "/c", command)
			} else {
				cmd = exec.CommandContext(ctx, "sh", "-c", command)
			}
			out, err := cmd.CombinedOutput()
			result := string(out)
			if err != nil {
				result += "\n错误: " + err.Error()
			}
			results[idx] = cmdResult{index: idx, output: result}
		}(i, c)
	}
	wg.Wait()

	var sb strings.Builder
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("=== 命令 %d: %s ===\n%s\n", i+1, cmds[i], scheduler.TruncateStr(r.output, 1000)))
	}
	return sb.String()
}

func (h *TUIAgentHandler) toolSwitchLLMProvider(args map[string]interface{}) string {
	provider := stringArg(args, "provider")
	if provider == "" {
		return "错误: 缺少 provider"
	}
	mgr := h.getConfigMgr()
	oldVal, err := mgr.UpdateConfig("maclaw_llm", "maclaw_llm_current_provider", provider)
	if err != nil {
		return fmt.Sprintf("切换失败: %v", err)
	}
	return fmt.Sprintf("LLM 提供商已切换: %s → %s", oldVal, provider)
}

func (h *TUIAgentHandler) toolSetMaxIterations(args map[string]interface{}) string {
	value := intArg(args, "value", 0)
	if value <= 0 {
		return "错误: value 必须为正整数"
	}
	if value < 30 {
		value = 30
	}
	if value > 300 {
		value = 300
	}
	h.maxIterations = value
	return fmt.Sprintf("Agent 最大推理轮次已设置为 %d", value)
}

func (h *TUIAgentHandler) toolRecommendTool(args map[string]interface{}) string {
	if h.selector == nil {
		return "工具推荐器未初始化"
	}
	desc := stringArg(args, "task_description")
	if desc == "" {
		return "错误: 缺少 task_description"
	}
	installed := commands.DetectInstalledToolNames()
	name, reason := h.selector.Recommend(desc, installed)
	return fmt.Sprintf("推荐工具: %s\n原因: %s", name, reason)
}

func (h *TUIAgentHandler) toolScreenshot() string {
	// Enforce cooldown to prevent accidental repeated screenshots.
	if !h.lastScreenshotAt.IsZero() {
		elapsed := time.Since(h.lastScreenshotAt)
		if elapsed < 30*time.Second {
			remaining := 30*time.Second - elapsed
			return fmt.Sprintf("截屏冷却中，请等待 %d 秒后再试", int(remaining.Seconds())+1)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command",
			`Add-Type -AssemblyName System.Windows.Forms; $bmp = New-Object System.Drawing.Bitmap([System.Windows.Forms.Screen]::PrimaryScreen.Bounds.Width, [System.Windows.Forms.Screen]::PrimaryScreen.Bounds.Height); $g = [System.Drawing.Graphics]::FromImage($bmp); $g.CopyFromScreen(0,0,0,0,$bmp.Size); $ms = New-Object System.IO.MemoryStream; $bmp.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png); [Convert]::ToBase64String($ms.ToArray())`)
	} else if runtime.GOOS == "darwin" {
		if !remote.CheckScreenRecordingPermission() {
			if remote.IsScreenRecordingStale() {
				return "截图权限已过期（macOS 26 TCC 记录失效）- 请在终端执行: sudo tccutil reset ScreenCapture com.wails.MaClaw 然后重启 maclaw"
			}
			return "截图权限未授予 - 请打开 系统设置 > 隐私与安全性 > 屏幕录制，授权 MaClaw，然后重启"
		}
		cmd = exec.CommandContext(ctx, "bash", "-c", `screencapture -x /tmp/_maclaw_ss.png && base64 /tmp/_maclaw_ss.png && rm -f /tmp/_maclaw_ss.png`)
	} else {
		cmd = exec.CommandContext(ctx, "bash", "-c", `import -window root /tmp/_maclaw_ss.png 2>/dev/null && base64 /tmp/_maclaw_ss.png && rm -f /tmp/_maclaw_ss.png`)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("截图失败: %v", err)
	}
	b64, parseErr := remote.ParseScreenshotOutput(string(out))
	if parseErr != nil {
		return fmt.Sprintf("截图解析失败: %v", parseErr)
	}
	// 缩小到合理大小
	b64, _ = remote.DownsizeScreenshotBase64(b64, 200*1024)
	h.lastScreenshotAt = time.Now()
	return fmt.Sprintf("截图已获取 (base64, %d 字符)", len(b64))
}

// ===================== 辅助函数 =====================

func intArg(args map[string]interface{}, key string, defaultVal int) int {
	if args == nil {
		return defaultVal
	}
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	case string:
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return defaultVal
}

// ===================== Web Search & Fetch =====================

func (h *TUIAgentHandler) toolWebSearch(args map[string]interface{}) string {
	query := stringArg(args, "query")
	if query == "" {
		return "缺少 query 参数"
	}
	maxResults := intArg(args, "max_results", 8)

	results, err := websearch.Search(query, maxResults)
	if err != nil {
		return fmt.Sprintf("搜索失败: %v", err)
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

func (h *TUIAgentHandler) toolWebFetch(args map[string]interface{}) string {
	rawURL := stringArg(args, "url")
	if rawURL == "" {
		return "缺少 url 参数"
	}

	opts := &websearch.FetchOptions{}
	if renderJS, ok := args["render_js"].(bool); ok {
		opts.RenderJS = renderJS
	}
	if savePath := stringArg(args, "save_path"); savePath != "" {
		opts.SavePath = savePath
		opts.MaxBytes = 10 * 1024 * 1024
	} else {
		opts.MaxBytes = 2 * 1024 * 1024
	}
	opts.TimeoutS = intArg(args, "timeout", 30)

	result, err := websearch.Fetch(rawURL, opts)
	if err != nil {
		return fmt.Sprintf("抓取失败: %v", err)
	}

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
	const webFetchMaxContent = 16384
	if len(content) > webFetchMaxContent {
		headLen := webFetchMaxContent * 2 / 3
		tailLen := webFetchMaxContent - headLen - 60
		content = content[:headLen] + "\n\n... (内容已截断，共 " + fmt.Sprintf("%d", len(content)) + " 字符) ...\n\n" + content[len(content)-tailLen:]
	}
	sb.WriteString(content)
	return sb.String()
}

// ===================== GUI 自动化 =====================
// TODO: 当 TUI 获得完整 GUI 依赖（Accessibility Bridge、InputSimulator、截图引擎）时，
// 在此处调用 guiautomation.RegisterTools 注册 GUI 自动化工具（gui_record_start、
// gui_record_stop、gui_replay、gui_list_flows、gui_click、gui_type、gui_screenshot）。
// 参考 gui/tools_gui_automation.go 中的 registerGUIAutomationTools 实现模式。

// ===================== 项目管理 =====================

func (h *TUIAgentHandler) toolProjectManage(args map[string]interface{}) string {
	action := stringArg(args, "action")
	dataDir := commands.ResolveDataDir()
	store := commands.NewFileConfigStore(dataDir)

	switch action {
	case "create":
		return h.projectCreate(store, args)
	case "list":
		return h.projectList(store)
	case "delete":
		return h.projectDelete(store, args)
	case "switch":
		return h.projectSwitch(store, args)
	default:
		return fmt.Sprintf("未知 action: %s（支持 create/list/delete/switch）", action)
	}
}

func (h *TUIAgentHandler) projectCreate(store project.ConfigStore, args map[string]interface{}) string {
	name := stringArg(args, "name")
	path := stringArg(args, "path")
	if name == "" || path == "" {
		return "create 需要 name 和 path 参数"
	}

	res, err := project.Create(store, name, path)
	if err != nil {
		return fmt.Sprintf("创建项目失败: %v", err)
	}

	result, _ := json.Marshal(map[string]string{"id": res.Id, "name": res.Name, "path": res.Path, "status": "created"})
	return string(result)
}

func (h *TUIAgentHandler) projectList(store project.ConfigStore) string {
	items, err := project.List(store)
	if err != nil {
		return fmt.Sprintf("加载配置失败: %v", err)
	}
	data, _ := json.Marshal(items)
	return string(data)
}

func (h *TUIAgentHandler) projectDelete(store project.ConfigStore, args map[string]interface{}) string {
	target := stringArg(args, "target")
	if target == "" {
		return "delete 需要 target 参数（项目名称或 ID）"
	}

	res, err := project.Delete(store, target)
	if err != nil {
		return fmt.Sprintf("删除项目失败: %v", err)
	}

	result, _ := json.Marshal(map[string]string{"id": res.Id, "name": res.Name, "status": "deleted"})
	return string(result)
}

func (h *TUIAgentHandler) projectSwitch(store project.ConfigStore, args map[string]interface{}) string {
	target := stringArg(args, "target")
	if target == "" {
		return "switch 需要 target 参数（项目名称或 ID）"
	}

	res, err := project.Switch(store, target)
	if err != nil {
		return fmt.Sprintf("切换项目失败: %v", err)
	}

	result, _ := json.Marshal(map[string]string{"id": res.Id, "name": res.Name, "path": res.Path, "status": "switched"})
	return string(result)
}
