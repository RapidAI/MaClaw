package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
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
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
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
		"bash": true, "read_file": true, "write_file": true, "edit_file": true, "list_directory": true,
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
		"run_skill": true, "agentnet_search": true, "agentnet_publish": true,
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
	vars := normalizeRunSkillVars(args)
	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Sprintf("加载配置失败: %v", err)
	}

	var skill *corelib.NLSkillEntry
	for i := range cfg.NLSkills {
		if cfg.NLSkills[i].MatchesName(skillName) {
			skill = &cfg.NLSkills[i]
			break
		}
	}
	if skill == nil {
		return fmt.Sprintf("技能 %s 不存在。请使用 list_skills 查看已安装的技能", skillName)
	}
	if skill.Status == "disabled" {
		return fmt.Sprintf("技能 %s 已禁用。请使用 update_config 启用后再运行", skillName)
	}
	// Bug #3: Distinguish needs_setup from other states
	if skill.Status == "needs_setup" {
		return fmt.Sprintf("技能 %s 需要配置。安装时部分依赖或文件未就绪，请检查 Skill 目录 (%s) 并完成配置后重试", skill.Name, skill.SkillDir)
	}
	if skill.Status != "active" && skill.Status != "" {
		return fmt.Sprintf("技能 %s 状态为 %q，无法运行", skill.Name, skill.Status)
	}
	if len(skill.Steps) == 0 {
		// Bug #5: Better error for skills with no executable steps
		desc := strings.TrimSpace(skill.Description)
		if desc != "" && len(desc) > 150 {
			desc = desc[:150] + "..."
		}
		msg := fmt.Sprintf("技能 %s 没有定义可执行步骤", skillName)
		if len(skill.RequiredArgs) > 0 {
			msg += fmt.Sprintf("。该技能需要参数: %s", strings.Join(skill.RequiredArgs, ", "))
		}
		if desc != "" {
			msg += fmt.Sprintf("\n说明: %s", desc)
		}
		return msg
	}

	// P1: Platform compatibility check (mirrors GUI StartRun)
	if len(skill.Platforms) > 0 {
		currentOS := runtime.GOOS
		platformName := currentOS
		if platformName == "darwin" {
			platformName = "macos"
		}
		matched := false
		for _, p := range skill.Platforms {
			if strings.EqualFold(strings.TrimSpace(p), platformName) || strings.EqualFold(strings.TrimSpace(p), "universal") {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Sprintf("技能 %s 不支持当前平台 %s（支持: %s）", skill.Name, platformName, strings.Join(skill.Platforms, ", "))
		}
	}

	// P1: Required args validation
	if len(skill.RequiredArgs) > 0 {
		var missing []string
		for _, arg := range skill.RequiredArgs {
			if strings.TrimSpace(vars[arg]) == "" {
				missing = append(missing, arg)
			}
		}
		if len(missing) > 0 {
			return fmt.Sprintf("技能 %s 缺少必需参数: %s", skill.Name, strings.Join(missing, ", "))
		}
	}

	// P1: Required env validation
	if len(skill.RequiredEnv) > 0 {
		var missing []string
		for _, env := range skill.RequiredEnv {
			if strings.TrimSpace(os.Getenv(env)) == "" {
				missing = append(missing, env)
			}
		}
		if len(missing) > 0 {
			return fmt.Sprintf("技能 %s 缺少必需的环境变量: %s", skill.Name, strings.Join(missing, ", "))
		}
	}

	// IMP-002: Implicit required args detection — catch {{key}} placeholders
	// in step commands that aren't provided via vars, even when the skill
	// doesn't declare required_args explicitly.
	if len(skill.RequiredArgs) == 0 {
		implicit := detectImplicitRequiredArgsTUI(skill.Steps, vars)
		if len(implicit) > 0 {
			desc := strings.TrimSpace(skill.Description)
			if len(desc) > 120 {
				desc = desc[:120] + "..."
			}
			msg := fmt.Sprintf("技能 %s 的命令中包含未提供的参数: %s。请通过 args 传入", skill.Name, strings.Join(implicit, ", "))
			if desc != "" {
				msg += fmt.Sprintf("\n说明: %s", desc)
			}
			return msg
		}
	}

	// BUG-005: Normalize skill directory path (resolve 8.3 short paths on Windows)
	if runtime.GOOS == "windows" && skill.SkillDir != "" {
		skill.SkillDir = normalizeWindowsShortPathTUI(skill.SkillDir)
	}

	// P1: Dependency pre-check — verify commands referenced in bash steps exist
	for i, step := range skill.Steps {
		if step.Action != "bash" {
			continue
		}
		command, _ := step.Params["command"].(string)
		if command == "" || strings.Contains(command, "{{") || strings.Contains(command, "${") {
			continue
		}
		depErr := checkCommandDependencyTUI(command)
		if depErr != "" {
			return fmt.Sprintf("技能 %s 步骤 %d 依赖检查失败: %s", skill.Name, i+1, depErr)
		}
	}

	// BUG-004: Generate a unique Run ID for tracking and status queries
	runID := fmt.Sprintf("run-%d-%d", time.Now().UnixMilli(), skill.UsageCount+1)

	// Operation-based routing for api_workflow mode skills.
	var selectedLabels []string
	isAPIWorkflow := strings.EqualFold(skill.Mode, "api_workflow")
	if isAPIWorkflow {
		if opName := stringArg(args, "operation"); opName != "" {
			for _, op := range skill.Operations {
				if strings.EqualFold(op.Name, opName) {
					selectedLabels = op.Labels
					break
				}
			}
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("▶ 执行技能: %s (%d 步) [%s]\n", skill.Name, len(skill.Steps), runID))
	allSuccess := true
	hasFailure := false
	startTime := time.Now()

	for i, step := range skill.Steps {
		// Handle condition: "on_failure" — skip if no prior failure
		if step.Condition == "on_failure" && !hasFailure {
			sb.WriteString(fmt.Sprintf("\n── 步骤 %d/%d: %s ── [skipped: no prior failure]\n", i+1, len(skill.Steps), step.Action))
			continue
		}
		// Handle condition: "on_success" — skip if there was a failure
		if step.Condition == "on_success" && hasFailure {
			sb.WriteString(fmt.Sprintf("\n── 步骤 %d/%d: %s ── [skipped: prior failure]\n", i+1, len(skill.Steps), step.Action))
			continue
		}

		// api_workflow mode: skip steps not matching selected labels.
		if isAPIWorkflow && len(selectedLabels) > 0 {
			if step.Label == "" || !containsStringTUI(selectedLabels, step.Label) {
				sb.WriteString(fmt.Sprintf("\n── 步骤 %d/%d: %s ── [skipped: label %q not selected]\n", i+1, len(skill.Steps), step.Action, step.Label))
				continue
			}
		}

		// Dynamic when condition: evaluate expression with template vars.
		if step.When != "" {
			resolved := substituteSkillVarsInStringTUI(step.When, vars)
			if !evaluateSimpleConditionTUI(resolved) {
				sb.WriteString(fmt.Sprintf("\n── 步骤 %d/%d: %s ── [skipped: when %q false]\n", i+1, len(skill.Steps), step.Action, step.When))
				continue
			}
		}

		stepStart := time.Now()
		sb.WriteString(fmt.Sprintf("\n── 步骤 %d/%d: %s ──\n", i+1, len(skill.Steps), step.Action))

		// Resolve step params with captured variables from previous steps
		resolvedStep := resolveSkillStepTUI(step, vars, skill.SkillDir)

		output, execErr := runSkillStepWithPollTUI(resolvedStep, skill.SkillDir, vars)
		elapsed := time.Since(stepStart).Truncate(time.Millisecond)

		if execErr != nil {
			// Detect 404 session-not-found errors and provide actionable hint
			errClass := classifySkillStepError(output, execErr)
			sb.WriteString(fmt.Sprintf("[FAIL] %s (耗时 %s)\n", execErr.Error(), elapsed))
			if errClass != "" {
				sb.WriteString(fmt.Sprintf("[错误分类] %s\n", errClass))
			}
			if output != "" {
				appendTruncated(&sb, output, 2048)
			}
			allSuccess = false
			hasFailure = true
			if step.OnError != "continue" {
				sb.WriteString("⛔ 步骤失败且 on_error!=continue，终止执行\n")
				// Mark remaining steps as skipped
				for j := i + 1; j < len(skill.Steps); j++ {
					sb.WriteString(fmt.Sprintf("\n── 步骤 %d/%d: %s ── [skipped]\n", j+1, len(skill.Steps), skill.Steps[j].Action))
				}
				break
			}
			sb.WriteString("⚠️ 步骤失败但 on_error=continue，继续下一步\n")
		} else {
			sb.WriteString(fmt.Sprintf("[OK] 耗时 %s\n", elapsed))
			if output != "" {
				appendTruncated(&sb, output, 2048)
			}
			// Output capture: extract variables from step output via regex
			// This enables state passing between steps (e.g. sessionId from
			// step 2 used in step 3), fixing the P1-2 issue where the TUI
			// runner couldn't propagate context between skill steps.
			if len(step.Capture) > 0 && output != "" {
				captured := captureOutputVariablesTUI(output, step.Capture)
				for k, v := range captured {
					vars[k] = v
					sb.WriteString(fmt.Sprintf("[capture] %s=%s\n", k, truncateForDisplay(v, 80)))
				}
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
		skill.LastError = "执行失败，详见输出"
	}
	_ = store.SaveConfig(cfg)

	return sb.String()
}

func normalizeRunSkillVars(args map[string]interface{}) map[string]string {
	vars := make(map[string]string)
	if argsMap, ok := args["args"].(map[string]interface{}); ok {
		for k, v := range argsMap {
			if s, ok := v.(string); ok {
				vars[k] = s
			} else if v != nil {
				vars[k] = fmt.Sprintf("%v", v)
			}
		}
	}
	// Also check top-level keys that look like template variables
	for _, key := range []string{"input", "output", "query", "url", "text", "file", "path", "format", "operation"} {
		if v, ok := args[key].(string); ok && v != "" {
			if _, exists := vars[key]; !exists {
				vars[key] = v
			}
		}
	}
	return vars
}

// implicitArgReTUI matches {{key}} and ${key} placeholders in skill step commands.
var implicitArgReTUI = regexp.MustCompile(`\{\{(\w+)\}\}|\$\{(\w+)\}`)

// detectImplicitRequiredArgsTUI scans skill step commands for {{key}} / ${key}
// placeholders that aren't provided in vars. This catches skills that use
// template variables without declaring required_args (IMP-002).
func detectImplicitRequiredArgsTUI(steps []corelib.NLSkillStep, vars map[string]string) []string {
	seen := make(map[string]bool)
	var missing []string
	for _, step := range steps {
		// Check command param for bash steps, and all string params for other step types
		var textsToCheck []string
		if step.Action == "bash" {
			if cmd, _ := step.Params["command"].(string); cmd != "" {
				textsToCheck = append(textsToCheck, cmd)
			}
		}
		// Also check other string params (e.g. craft_tool task/description)
		for _, key := range []string{"task", "description", "text"} {
			if v, _ := step.Params[key].(string); v != "" {
				textsToCheck = append(textsToCheck, v)
			}
		}
		for _, text := range textsToCheck {
			matches := implicitArgReTUI.FindAllStringSubmatch(text, -1)
			for _, m := range matches {
				varName := m[1]
				if varName == "" {
					varName = m[2]
				}
				if varName == "" || seen[varName] {
					continue
				}
				seen[varName] = true
				if strings.TrimSpace(vars[varName]) == "" {
					missing = append(missing, varName)
				}
			}
		}
	}
	return missing
}

// runSkillStep 执行单个 skill 步骤，支持流式输出收集。
func runSkillStep(step corelib.NLSkillStep, skillDir string, vars map[string]string) (string, error) {
	switch step.Action {
	case "bash":
		command, _ := step.Params["command"].(string)
		if command == "" {
			return "", fmt.Errorf("missing command parameter")
		}
		command = substituteSkillVariables(command, vars)
		return runSkillBashStreaming(command, step.Params, skillDir)
	case "craft_tool":
		// BUG-003: craft_tool steps need timeout control to prevent hanging.
		// TUI doesn't have the full GUI App context, so we execute craft_tool
		// as a bash step by extracting the task description and running it
		// through the available script runtime with a strict timeout.
		return runCraftToolStepTUI(step, skillDir, vars)
	default:
		return "", fmt.Errorf("unsupported action: %s", step.Action)
	}
}

// runCraftToolStepTUI handles craft_tool steps in TUI mode (BUG-003).
// Since TUI lacks the full GUI App context for LLM-based script generation,
// this falls back to executing the task description as a bash command with
// proper timeout control to prevent hanging.
func runCraftToolStepTUI(step corelib.NLSkillStep, skillDir string, vars map[string]string) (string, error) {
	task, _ := step.Params["task"].(string)
	if task == "" {
		task, _ = step.Params["description"].(string)
	}
	if task == "" {
		return "", fmt.Errorf("craft_tool 步骤缺少 task 或 description 参数")
	}

	// Extract language preference
	lang, _ := step.Params["language"].(string)
	if lang == "" {
		lang = "python"
	}

	// Extract script if pre-generated
	script, _ := step.Params["script"].(string)
	if script != "" {
		// Execute pre-generated script with timeout
		timeout := 60.0
		if t, ok := step.Params["timeout"].(float64); ok && t > 0 {
			timeout = t
		}
		if timeout > 300 {
			timeout = 300
		}

		// Write script to temp file and execute
		var ext string
		var runner string
		switch strings.ToLower(lang) {
		case "python", "python3":
			ext = ".py"
			runner = "python"
			if runtime.GOOS != "windows" {
				runner = "python3"
			}
		case "node", "javascript", "js":
			ext = ".mjs"
			runner = "node"
		default:
			ext = ".sh"
			runner = "bash"
		}

		tmpFile, err := os.CreateTemp("", "craft-step-*"+ext)
		if err != nil {
			return "", fmt.Errorf("创建临时脚本失败: %v", err)
		}
		tmpPath := tmpFile.Name()
		defer os.Remove(tmpPath)

		if _, err := tmpFile.WriteString(script); err != nil {
			tmpFile.Close()
			return "", fmt.Errorf("写入临时脚本失败: %v", err)
		}
		tmpFile.Close()

		command := runner + " " + quoteSkillInputForShell(tmpPath)
		params := map[string]interface{}{
			"timeout":     timeout,
			"working_dir": skillDir,
		}
		return runSkillBashStreaming(command, params, skillDir)
	}

	// No pre-generated script — craft_tool requires LLM interaction which
	// is only available in GUI mode. Return a clear error.
	return "", fmt.Errorf("craft_tool 步骤需要 LLM 生成脚本，TUI 模式暂不支持动态 craft_tool。请在 GUI 模式下运行此 Skill，或将 Skill 改为 bash 步骤")
}

// resolveSkillStepTUI resolves template variables in all step params, not just
// the bash command string. This ensures captured variables (e.g. sessionId from
// a previous step) are propagated into subsequent step params like working_dir
// or any custom parameter.
// NOTE: The "command" param is intentionally skipped here because runSkillStep
// calls substituteSkillVariables() which applies proper shell quoting. Doing
// raw substitution on command would bypass shell escaping for user input.
func resolveSkillStepTUI(step corelib.NLSkillStep, vars map[string]string, skillDir string) corelib.NLSkillStep {
	resolved := step
	if resolved.Params == nil || len(vars) == 0 {
		return resolved
	}
	// Deep-resolve all string values in params except "command" (handled by runSkillStep)
	newParams := make(map[string]interface{}, len(resolved.Params))
	for k, v := range resolved.Params {
		if k == "command" {
			newParams[k] = v
			continue
		}
		if s, ok := v.(string); ok {
			newParams[k] = substituteSkillVariablesRaw(s, vars)
		} else {
			newParams[k] = v
		}
	}
	resolved.Params = newParams
	// Resolve relative working_dir against skillDir
	if workDir, _ := resolved.Params["working_dir"].(string); workDir != "" && !filepath.IsAbs(workDir) && skillDir != "" {
		resolved.Params["working_dir"] = filepath.Clean(filepath.Join(skillDir, workDir))
	}
	return resolved
}

// substituteSkillVariablesRaw replaces {{key}} and ${key} placeholders without
// shell quoting. Used for non-command params where quoting would be incorrect.
func substituteSkillVariablesRaw(text string, vars map[string]string) string {
	for key, value := range vars {
		text = strings.ReplaceAll(text, "{{"+key+"}}", value)
		text = strings.ReplaceAll(text, "${"+key+"}", value)
	}
	return text
}

// captureOutputVariablesTUI extracts named variables from step output using
// regex patterns defined in step.Capture. Each capture maps a variable name
// to a regex pattern; the first submatch group (or full match) becomes the
// variable value. This mirrors the GUI skill_runner's captureOutputVariables.
func captureOutputVariablesTUI(output string, captures map[string]string) map[string]string {
	result := make(map[string]string)
	for varName, pattern := range captures {
		re, err := regexp.Compile(pattern)
		if err != nil {
			continue
		}
		m := re.FindStringSubmatch(output)
		if len(m) > 1 {
			result[varName] = m[1] // first submatch group
		} else if len(m) == 1 {
			result[varName] = m[0] // full match
		}
	}
	return result
}

// classifySkillStepError inspects the combined output and error to classify
// the failure type. Returns a human-readable hint or empty string.
func classifySkillStepError(output string, err error) string {
	combined := output
	if err != nil {
		combined += " " + err.Error()
	}
	lower := strings.ToLower(combined)

	// P1: exit status 9009 (Windows) / 127 (Unix) = command not found
	if strings.Contains(lower, "exit status 9009") || strings.Contains(lower, "exit status 127") {
		// Check specific tools first (more specific → less specific)
		hint := "command_not_found: 命令未找到。"
		switch {
		case strings.Contains(lower, "pip3") || strings.Contains(lower, "pip"):
			hint += " 请安装 pip: python -m ensurepip --upgrade"
		case strings.Contains(lower, "npx") || strings.Contains(lower, "npm"):
			hint += " 请安装 Node.js: https://nodejs.org/"
		case strings.Contains(lower, "node"):
			hint += " 请安装 Node.js: https://nodejs.org/"
		case strings.Contains(lower, "python3") || strings.Contains(lower, "python"):
			hint += " 请安装 Python 3.x 并确保在 PATH 中。Windows 用户请从 python.org 安装，不要使用 Microsoft Store 版本。"
		default:
			hint += " 请确认所需命令已安装并在 PATH 中。"
		}
		return hint
	}

	// BUG-002: Shebang treated as command in Windows CMD/PowerShell
	if (strings.Contains(lower, "'#'") || strings.Contains(lower, "\"#\"")) &&
		strings.Contains(lower, "not recognized") {
		return "shell_compat: Bash 脚本的 shebang 行 (#!/bin/bash) 在 Windows CMD 中被当作命令执行。建议在 Skill 定义中设置 preferred_shell: bash，或改用跨平台脚本 (Python/Node.js)"
	}

	// BUG-001: Windows 8.3 short path resolution failure
	if runtime.GOOS == "windows" && strings.Contains(lower, "~") &&
		(strings.Contains(lower, "enoent") || strings.Contains(lower, "no such file")) {
		return "path_8dot3: Windows 8.3 短路径解析失败。文件路径中包含 '~' 缩写（如 ADMINI~1），Node.js/Python 可能无法识别。建议使用完整路径或通过 fs.realpathSync() 解析"
	}

	// P4: HTTP 429 rate limit
	if strings.Contains(lower, "429") && (strings.Contains(lower, "rate limit") || strings.Contains(lower, "too many requests") || strings.Contains(lower, "频率限制")) {
		return "rate_limit: API 调用过于频繁，请稍后再试。建议等待 30-60 秒后重试。"
	}

	switch {
	case strings.Contains(lower, "404") && (strings.Contains(lower, "会话不存在") || strings.Contains(lower, "session") || strings.Contains(lower, "not found")):
		return "session_not_found: 会话已过期或不存在，建议重新创建会话"
	case strings.Contains(lower, "401") || strings.Contains(lower, "unauthorized") || strings.Contains(lower, "鉴权失败"):
		return "auth_error: 认证失败，请检查 ACCESS_KEY 配置"
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "超时"):
		return "timeout: 命令执行超时"
	case strings.Contains(lower, "connection refused") || strings.Contains(lower, "连接被拒绝"):
		return "network_error: 网络连接失败"
	case strings.Contains(lower, "enoent") || strings.Contains(lower, "no such file"):
		return "file_not_found: 输入文件不存在，请检查文件路径是否正确"
	case strings.Contains(lower, "permission denied") || strings.Contains(lower, "access denied"):
		return "permission_error: 权限不足，请检查文件/目录权限"
	// BUG-003: craft_tool hanging detection
	case strings.Contains(lower, "context deadline exceeded") || strings.Contains(lower, "signal: killed"):
		return "timeout: 步骤执行超时，可能是 craft_tool 脚本挂起。建议增加 timeout 参数或检查脚本是否有阻塞操作"
	}
	return ""
}

// runSkillStepWithPollTUI wraps runSkillStep with optional poll loop.
// When step.Poll is configured, the step is re-executed at intervals until
// the output matches the termination condition or max attempts are exhausted.
func runSkillStepWithPollTUI(step corelib.NLSkillStep, skillDir string, vars map[string]string) (string, error) {
	if step.Poll == nil {
		return runSkillStep(step, skillDir, vars)
	}
	poll := step.Poll
	interval := time.Duration(poll.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	maxAttempts := poll.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 20
	}
	var matchRe *regexp.Regexp
	if poll.UntilMatch != "" {
		matchRe, _ = regexp.Compile(poll.UntilMatch)
	}
	var lastOutput string
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		output, err := runSkillStep(step, skillDir, vars)
		lastOutput = output
		if err != nil {
			return output, err
		}
		if matchRe != nil && matchRe.MatchString(output) {
			return output, nil
		}
		if poll.UntilStatus != "" && strings.Contains(output, poll.UntilStatus) {
			return output, nil
		}
		if matchRe == nil && poll.UntilStatus == "" {
			return output, nil
		}
		if attempt < maxAttempts {
			time.Sleep(interval)
		}
	}
	return lastOutput, fmt.Errorf("poll exhausted after %d attempts without matching condition", maxAttempts)
}

// containsStringTUI checks if a string slice contains a target string (case-insensitive).
func containsStringTUI(slice []string, target string) bool {
	for _, s := range slice {
		if strings.EqualFold(s, target) {
			return true
		}
	}
	return false
}

// substituteSkillVarsInStringTUI replaces {{key}} and ${key} placeholders.
func substituteSkillVarsInStringTUI(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{{"+k+"}}", v)
		s = strings.ReplaceAll(s, "${"+k+"}", v)
	}
	return s
}

// evaluateSimpleConditionTUI evaluates a simple condition expression.
// Supported: "a == b", "a != b", "a contains b", bare truthy.
func evaluateSimpleConditionTUI(expr string) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false
	}
	// Space-delimited operators first for accuracy
	if idx := strings.Index(expr, " contains "); idx > 0 {
		return strings.Contains(strings.TrimSpace(expr[:idx]), strings.TrimSpace(expr[idx+len(" contains "):]))
	}
	if idx := strings.Index(expr, " != "); idx > 0 {
		return strings.TrimSpace(expr[:idx]) != strings.TrimSpace(expr[idx+len(" != "):])
	}
	if idx := strings.Index(expr, " == "); idx > 0 {
		return strings.TrimSpace(expr[:idx]) == strings.TrimSpace(expr[idx+len(" == "):])
	}
	// Fallback: compact form
	if parts := strings.SplitN(expr, "!=", 2); len(parts) == 2 {
		return strings.TrimSpace(parts[0]) != strings.TrimSpace(parts[1])
	}
	if parts := strings.SplitN(expr, "==", 2); len(parts) == 2 {
		return strings.TrimSpace(parts[0]) == strings.TrimSpace(parts[1])
	}
	return true
}

// checkCommandDependencyTUI checks if the primary command in a bash step
// is available on the system. Returns a user-friendly error message if the
// command is missing, or empty string if OK.
// This pre-flight check prevents confusing "exit status 9009" errors by
// detecting missing dependencies before execution.
func checkCommandDependencyTUI(command string) string {
	// Extract the first word (command name) from the command string.
	// Skip shell builtins and common prefixes.
	cmd := strings.TrimSpace(command)
	// Handle multi-line: check only the first meaningful line
	for _, line := range strings.Split(cmd, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		cmd = line
		break
	}
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return ""
	}
	cmdName := fields[0]
	// Skip shell builtins, environment variable assignments, and path-based commands
	if strings.Contains(cmdName, "=") || cmdName == "echo" || cmdName == "cd" ||
		cmdName == "export" || cmdName == "set" || cmdName == "if" ||
		cmdName == "for" || cmdName == "while" || cmdName == "test" ||
		cmdName == "@echo" || cmdName == "chcp" ||
		strings.HasPrefix(cmdName, "./") || strings.HasPrefix(cmdName, "../") ||
		filepath.IsAbs(cmdName) {
		return ""
	}
	// On Windows, map python3 → python for the check
	checkName := cmdName
	if runtime.GOOS == "windows" && strings.EqualFold(checkName, "python3") {
		checkName = "python"
	}
	if _, err := exec.LookPath(checkName); err != nil {
		switch strings.ToLower(cmdName) {
		case "python", "python3":
			return fmt.Sprintf("需要 Python 3 但未找到。请从 https://python.org 安装 Python 3.x")
		case "pip", "pip3":
			return fmt.Sprintf("需要 pip 但未找到。请运行: python -m ensurepip --upgrade")
		case "node", "npm", "npx":
			return fmt.Sprintf("需要 Node.js 但未找到。请从 https://nodejs.org 安装")
		default:
			return fmt.Sprintf("需要命令 %q 但未找到，请确认已安装并在 PATH 中", cmdName)
		}
	}
	return ""
}

// truncateForDisplay truncates a string for display purposes.
// Uses rune count to avoid cutting multi-byte characters (CJK, emoji, etc.).
func truncateForDisplay(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}

func substituteSkillVariables(command string, vars map[string]string) string {
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		value := quoteSkillInputForShell(vars[key])
		command = strings.ReplaceAll(command, "{{"+key+"}}", value)
		command = strings.ReplaceAll(command, "${"+key+"}", value)
	}
	return command
}

// quoteSkillInputForShell wraps a user-supplied value for safe embedding
// in a shell command string.
// On Windows we use double-quotes (cmd.exe does not recognise single-quotes).
func quoteSkillInputForShell(input string) string {
	if input == "" {
		if runtime.GOOS == "windows" {
			return `""`
		}
		return "''"
	}
	if runtime.GOOS == "windows" {
		// Double-quote for cmd.exe compatibility (TUI uses cmd.exe on Windows).
		escaped := strings.ReplaceAll(input, `"`, `\"`)
		return `"` + escaped + `"`
	}
	return "'" + strings.ReplaceAll(input, "'", `'"'"'`) + "'"
}

// mapPython3ToWindowsTUI replaces `python3` with `python` in commands on Windows.
// Result of LookPath is cached to avoid repeated filesystem lookups.
// On Windows, the Microsoft Store installs a stub `python3.exe` in
// WindowsApps that opens the Store instead of running Python.
var python3NeedsMappingTUI = sync.OnceValue(func() bool {
	p3, err := exec.LookPath("python3")
	if err == nil {
		if runtime.GOOS == "windows" && strings.Contains(strings.ToLower(p3), "windowsapps") {
			// Windows Store stub, not a real python3
		} else {
			return false
		}
	}
	_, err2 := exec.LookPath("python")
	return err2 == nil
})

func mapPython3ToWindowsTUI(command string) string {
	if !python3NeedsMappingTUI() {
		return command
	}
	lines := strings.Split(command, "\n")
	changed := false
	for i, line := range lines {
		ltrimmed := strings.TrimSpace(line)
		ll := strings.ToLower(ltrimmed)
		if strings.HasPrefix(ll, "python3 ") || ll == "python3" {
			lines[i] = strings.Replace(line, "python3", "python", 1)
			changed = true
		}
	}
	if changed {
		return strings.Join(lines, "\n")
	}
	return command
}

// needsBashShellTUI detects whether a command requires a Unix shell (bash/sh)
// rather than cmd.exe on Windows. This prevents shebang lines and bash-specific
// syntax from being misinterpreted by CMD/PowerShell (BUG-002).
func needsBashShellTUI(command string) bool {
	lower := strings.TrimSpace(strings.ToLower(command))

	// Unix shell builtins must use bash even if the command also contains .py/.js paths
	if strings.HasPrefix(lower, "export ") || strings.HasPrefix(lower, "source ") ||
		strings.HasPrefix(lower, "#!/") {
		return true
	}
	// Multi-line commands containing export lines
	for _, line := range strings.Split(command, "\n") {
		trimmed := strings.TrimSpace(strings.ToLower(line))
		if strings.HasPrefix(trimmed, "export ") {
			return true
		}
	}

	// Known interpreters that run fine under cmd.exe — prefer cmd.exe
	for _, prefix := range []string{"node ", "python ", "python3 ", "java ", "npm ", "pip ", "npx ", "go run ", "cargo run ", "pnpm "} {
		if strings.HasPrefix(lower, prefix) {
			return false
		}
	}
	// Direct script path invocation — cmd.exe handles this well
	if strings.Contains(lower, ".mjs") || strings.Contains(lower, ".js") ||
		strings.Contains(lower, ".py") || strings.Contains(lower, ".bat") ||
		strings.Contains(lower, ".cmd") {
		return false
	}
	// Bash-specific syntax: pipes, redirections, heredocs
	if strings.ContainsAny(command, "|<>") {
		return true
	}
	if strings.Contains(command, "&&") || strings.Contains(command, "||") {
		return true
	}
	// Command substitution
	if strings.Contains(command, "$(") || strings.Contains(command, "`") {
		return true
	}
	// Globbing with path separators
	if strings.Contains(command, "*/") || strings.Contains(command, "/*") {
		return true
	}
	// Tilde expansion
	if strings.Contains(command, "~/") {
		return true
	}
	return false
}

// winPathInQuotesReTUI matches Windows-style paths inside quotes.
var winPathInQuotesReTUI = regexp.MustCompile(`["'][A-Za-z]:\\[^"']+["']`)

// winPathInCommandReTUI matches unquoted Windows-style paths.
var winPathInCommandReTUI = regexp.MustCompile(`[A-Za-z]:\\[\w\\.-]+`)

// convertWindowsPathsInCommandTUI converts backslash paths to forward slashes
// for bash execution on Windows (BUG-001 related).
func convertWindowsPathsInCommandTUI(command string) string {
	if !strings.Contains(command, `\`) {
		return command
	}
	result := winPathInQuotesReTUI.ReplaceAllStringFunc(command, func(match string) string {
		return strings.ReplaceAll(match, `\`, `/`)
	})
	result = winPathInCommandReTUI.ReplaceAllStringFunc(result, func(match string) string {
		return strings.ReplaceAll(match, `\`, `/`)
	})
	return result
}

// normalizeWindowsShortPathTUI resolves Windows 8.3 short paths (e.g.
// C:\Users\ADMINI~1\...) to their full long-path equivalents (BUG-001).
// On non-Windows or if resolution fails, returns the original path unchanged.
func normalizeWindowsShortPathTUI(p string) string {
	if runtime.GOOS != "windows" {
		return p
	}
	if !strings.Contains(p, "~") {
		return p
	}
	// Use filepath.EvalSymlinks which on Windows resolves short names
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return p
	}
	return resolved
}

// normalizePathsInCommandTUI scans a command string for Windows 8.3 short
// paths and replaces them with their long-path equivalents (BUG-001).
var win83PathRe = regexp.MustCompile(`[A-Za-z]:\\[^\s"']+~\d[^\s"']*`)

func normalizePathsInCommandTUI(command string) string {
	if runtime.GOOS != "windows" || !strings.Contains(command, "~") {
		return command
	}
	return win83PathRe.ReplaceAllStringFunc(command, func(match string) string {
		resolved := normalizeWindowsShortPathTUI(match)
		if resolved != match {
			return resolved
		}
		return match
	})
}

// findShTUI locates a Unix shell (sh.exe / bash.exe) on Windows,
// typically provided by Git for Windows.
func findShTUI() (string, error) {
	// Try sh.exe first (Git for Windows)
	if shPath, err := exec.LookPath("sh.exe"); err == nil {
		return shPath, nil
	}
	// Try bash.exe (but skip WSL bash)
	if bashPath, err := exec.LookPath("bash.exe"); err == nil {
		// Skip WSL bash (typically in System32)
		if !strings.Contains(strings.ToLower(bashPath), "system32") {
			return bashPath, nil
		}
	}
	// Try common Git for Windows locations
	for _, candidate := range []string{
		`C:\Program Files\Git\bin\sh.exe`,
		`C:\Program Files\Git\usr\bin\bash.exe`,
		`C:\Program Files (x86)\Git\bin\sh.exe`,
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("找不到 Unix shell (sh.exe/bash.exe)，请安装 Git for Windows: https://git-scm.com/download/win")
}

// runSkillBashStreaming 执行 bash 命令，使用流式输出收集而非等待完成。
// 每秒检查一次输出，超时后报告已收集的部分输出。
//
// BUG-002 fix: On Windows, detects bash-specific syntax (shebang, pipes, etc.)
// and routes to sh.exe/bash.exe via temp script file instead of PowerShell.
// BUG-001 fix: Normalizes Windows 8.3 short paths before execution.
func runSkillBashStreaming(command string, params map[string]interface{}, skillDir string) (string, error) {
	// Strip UTF-8 BOM if present
	command = strings.TrimPrefix(command, "\xef\xbb\xbf")

	timeout := 30
	if t, ok := params["timeout"].(float64); ok && t > 0 {
		timeout = int(t)
		if timeout > 300 {
			timeout = 300
		}
	}

	// [Fix] On Windows, map `python3` to `python` since Windows Python
	// installs typically only provide `python.exe`, not `python3.exe`.
	if runtime.GOOS == "windows" {
		command = mapPython3ToWindowsTUI(command)
	}

	// BUG-001: Normalize Windows 8.3 short paths to long paths
	if runtime.GOOS == "windows" {
		command = normalizePathsInCommandTUI(command)
	}

	workDir, _ := params["working_dir"].(string)
	if workDir == "" && skillDir != "" {
		workDir = skillDir
	}
	// BUG-001: Also normalize the working directory path
	if runtime.GOOS == "windows" && workDir != "" {
		workDir = normalizeWindowsShortPathTUI(workDir)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	var shellName string
	var shellArgs []string
	var tmpScript string // temp script file for Windows execution

	if runtime.GOOS == "windows" {
		// BUG-002: Detect whether the command needs a Unix shell (bash) or
		// can run under cmd.exe. This prevents shebang lines from being
		// treated as commands in CMD/PowerShell.
		useBash := needsBashShellTUI(command)
		preferredShell, _ := params["preferred_shell"].(string)
		if strings.EqualFold(preferredShell, "bash") {
			useBash = true
		}

		if useBash {
			// Route to sh.exe/bash.exe via temp script file
			shPath, err := findShTUI()
			if err != nil {
				return "", err
			}
			shellName = shPath

			bashCommand := convertWindowsPathsInCommandTUI(command)
			scriptFile, err := os.CreateTemp("", "skill-step-*.sh")
			if err != nil {
				return "", fmt.Errorf("创建临时脚本文件失败: %v", err)
			}
			tmpScript = scriptFile.Name()
			scriptContent := "#!/bin/bash\n" + bashCommand + "\n"
			if _, err := scriptFile.WriteString(scriptContent); err != nil {
				scriptFile.Close()
				os.Remove(tmpScript)
				return "", fmt.Errorf("写入临时脚本文件失败: %v", err)
			}
			scriptFile.Close()
			shellArgs = []string{filepath.ToSlash(tmpScript)}
		} else {
			// Use cmd.exe with a temp .cmd script to avoid argument escaping issues
			cmdPath := os.Getenv("ComSpec")
			if cmdPath == "" {
				cmdPath = `C:\WINDOWS\system32\cmd.exe`
				if _, err := os.Stat(cmdPath); err != nil {
					cmdPath = "cmd.exe"
				}
			}
			shellName = cmdPath

			scriptFile, err := os.CreateTemp("", "skill-step-*.cmd")
			if err != nil {
				return "", fmt.Errorf("创建临时脚本文件失败: %v", err)
			}
			tmpScript = scriptFile.Name()
			// chcp 65001 switches cmd.exe to UTF-8 mode for non-ASCII paths
			scriptContent := "@echo off\r\nchcp 65001 >nul\r\n" + command + "\r\n"
			if _, err := scriptFile.WriteString(scriptContent); err != nil {
				scriptFile.Close()
				os.Remove(tmpScript)
				return "", fmt.Errorf("写入临时脚本文件失败: %v", err)
			}
			scriptFile.Close()
			shellArgs = []string{"/c", tmpScript}
		}
	} else {
		shellName = "bash"
		shellArgs = []string{"-c", command}
	}

	if tmpScript != "" {
		defer os.Remove(tmpScript)
	}

	cmd := exec.CommandContext(ctx, shellName, shellArgs...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	// Force UTF-8 encoding for subprocess I/O on Windows to prevent
	// GBK/CP936 mojibake when scripts output non-ASCII text.
	cmd.Env = coretool.AppendUTF8Env(os.Environ())

	// Inject caller-supplied extra env vars (from skill params).
	if extraEnv, ok := params["extra_env"].(map[string]interface{}); ok {
		for k, v := range extraEnv {
			if s, ok := v.(string); ok && k != "" {
				cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, s))
			}
		}
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

// ===================== AgentNet =====================

func (h *TUIAgentHandler) toolAgentNetSearch(args map[string]interface{}) string {
	if h.AgentNetClient == nil {
		return "AgentNet 客户端未初始化"
	}
	query := stringArg(args, "query")
	if query == "" {
		return "错误: 缺少 query"
	}
	entries, err := h.AgentNetClient.SearchKnowledge(query)
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

func (h *TUIAgentHandler) toolAgentNetPublish(args map[string]interface{}) string {
	if h.AgentNetClient == nil {
		return "AgentNet 客户端未初始化"
	}
	title := stringArg(args, "title")
	body := stringArg(args, "body")
	if title == "" || body == "" {
		return "错误: 缺少 title 或 body"
	}
	entry, err := h.AgentNetClient.PublishKnowledge(title, body)
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

	offset := intArg(args, "offset", 0)
	maxChars := intArg(args, "max_chars", 16384)
	if _, ok := args["max_chars"]; ok && maxChars <= 0 {
		maxChars = 0
	}
	opts := &websearch.FetchOptions{Offset: offset, MaxChars: maxChars}
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

	start := offset
	if start < 0 {
		start = 0
	}
	end := start + len([]rune(result.Content))

	var sb strings.Builder
	if result.Title != "" {
		sb.WriteString(fmt.Sprintf("标题: %s\n", result.Title))
	}
	sb.WriteString(fmt.Sprintf("URL: %s\n", result.URL))
	sb.WriteString(fmt.Sprintf("类型: %s | 大小: %d 字节\n", result.ContentType, result.BytesRead))
	sb.WriteString(fmt.Sprintf("已读取: %d-%d / %d 字符\n", start, end, result.TotalChars))
	sb.WriteString(fmt.Sprintf("truncated: %t | has_more: %t | next_offset: %d\n\n", result.Truncated, result.HasMore, result.NextOffset))
	sb.WriteString(result.Content)
	if result.HasMore {
		sb.WriteString(fmt.Sprintf("\n\n--- 完整性信号 ---\nhas_more: true\nnext_offset: %d\n继续读取时请传入 offset=%d\n", result.NextOffset, result.NextOffset))
	}
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
