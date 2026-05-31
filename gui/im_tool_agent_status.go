package main

// im_tool_agent_status.go implements the agent_status tool — a read-only
// query interface for the main agent's runtime state.
//
// This tool is the mechanism-level fix for the "/btw 下载完了吗？" problem:
// instead of searching long-term memory (which stores knowledge, not runtime
// state), this tool directly queries the actual process managers and session
// managers.
//
// Data sources (single enumeration point — add new sources here):
//   - LocalBackgroundTaskManager: bash(background=true) local processes
//   - SSHBackgroundTaskManager:   ssh(submit_task) remote processes
//   - RemoteSessionManager:       coding sessions (create_session)
//   - SSHSessionManager:          SSH interactive connections
//
// Consumers:
//   - /btw SubAgent: agent_status tool call
//   - pendingBackgroundTaskHint: main agent loop recover prompt injection
//   - (future) main agent's discover_tool / system prompt injection
//
// Design: collectRuntimeStatus() returns structured data. Each consumer
// formats it for its own context. New data sources are added once in
// collectRuntimeStatus(), all consumers automatically see them.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// ---------------------------------------------------------------------------
// Structured runtime status — single data source for all consumers
// ---------------------------------------------------------------------------

// RuntimeTaskInfo represents a single background task (local or SSH).
type RuntimeTaskInfo struct {
	TaskID    string
	Source    runtimeTaskSource
	Status    runtimeTaskStatus
	Command   string
	TaskRole  string
	StartedAt time.Time
	ExitCode  int    // only meaningful for local tasks when completed/failed
	PID       string // SSH tasks use string PID
}

// RuntimeSessionInfo represents a coding session or SSH connection.
type RuntimeSessionInfo struct {
	ID     string
	Source runtimeSessionSource
	Status runtimeSessionStatus
	Label  string // host label for SSH, empty for coding
}

// RuntimeStatus is the structured snapshot of all runtime state.
type RuntimeStatus struct {
	Tasks    []RuntimeTaskInfo
	Sessions []RuntimeSessionInfo

	// MainAgentRunning is true when the main agent loop is actively
	// executing a task. When true, MainAgentTask and MainAgentElapsed
	// describe what it's doing.
	MainAgentRunning bool
	MainAgentTask    string        // the user's original request text
	MainAgentElapsed time.Duration // how long the loop has been running
}

// collectRuntimeStatus gathers all runtime state from the handler's managers.
// This is the SINGLE enumeration point for runtime data sources.
// Add new data sources (e.g. Docker containers) here — all consumers
// (toolAgentStatus, pendingBackgroundTaskHint) automatically see them.
func (h *IMMessageHandler) collectRuntimeStatus() RuntimeStatus {
	return h.collectRuntimeStatusForOwner("")
}

func (h *IMMessageHandler) collectRuntimeStatusForOwner(ownerID string) RuntimeStatus {
	var rs RuntimeStatus

	// Main agent loop state.
	// currentLoopCtx is set at the start of runAgentLoop and cleared in defer.
	// Reading it from /btw's goroutine is a benign race (same pattern as
	// /cancel command). The worst case is a stale nil/non-nil read, which
	// produces a slightly outdated "running"/"idle" report — acceptable for
	// a status query.
	h.collectMainAgentRuntimeStatusForOwner(&rs, ownerID)

	// Local background tasks.
	if h.localBgTaskMgr != nil {
		for _, t := range h.localBgTaskMgr.List() {
			t.Lock()
			rs.Tasks = append(rs.Tasks, RuntimeTaskInfo{
				TaskID:    t.TaskID,
				Source:    runtimeTaskSourceLocal,
				Status:    normalizeRuntimeTaskStatus(t.Status),
				Command:   t.Command,
				TaskRole:  t.TaskRole,
				StartedAt: t.StartedAt,
				ExitCode:  t.ExitCode,
			})
			t.Unlock()
		}
	}

	// SSH background tasks.
	if h.bgTaskMgr != nil {
		for _, t := range h.bgTaskMgr.ListTasks() {
			rs.Tasks = append(rs.Tasks, RuntimeTaskInfo{
				TaskID:    t.TaskID,
				Source:    runtimeTaskSourceSSH,
				Status:    normalizeRuntimeTaskStatus(t.Status),
				Command:   t.Command,
				TaskRole:  t.TaskRole,
				StartedAt: t.StartedAt,
				PID:       t.PID,
			})
		}
	}

	// Coding sessions.
	if h.manager != nil {
		for _, s := range h.manager.List() {
			s.mu.RLock()
			rs.Sessions = append(rs.Sessions, RuntimeSessionInfo{
				ID:     s.ID,
				Source: runtimeSessionSourceCoding,
				Status: normalizeRuntimeSessionStatus(s.Status),
			})
			s.mu.RUnlock()
		}
	}

	// SSH connections.
	if h.sshMgr != nil {
		for _, s := range h.sshMgr.List() {
			summary := s.GetSummary()
			rs.Sessions = append(rs.Sessions, RuntimeSessionInfo{
				ID:     s.ID,
				Source: runtimeSessionSourceSSH,
				Status: normalizeRuntimeSessionStatus(summary.Status),
				Label:  summary.HostLabel,
			})
		}
	}

	return rs
}

func (h *IMMessageHandler) collectMainAgentRuntimeStatusForOwner(rs *RuntimeStatus, ownerID string) {
	if h == nil || rs == nil {
		return
	}
	ownerID = strings.TrimSpace(ownerID)
	if ownerID != "" {
		if v, ok := h.sessionLoops.Load(ownerID); ok {
			state := v.(*sessionLoopState)
			state.stateMu.RLock()
			ctx := state.loopCtx
			userText := state.userText
			state.stateMu.RUnlock()
			if ctx != nil && !ctx.IsCancelled() {
				rs.MainAgentRunning = true
				rs.MainAgentTask = userText
				rs.MainAgentElapsed = time.Since(ctx.StartedAt).Round(time.Second)
			}
		}
		return
	}

	// Legacy fallback for callers that do not yet have an owner. New runtime
	// paths should pass ownerID so concurrent channels cannot see each other.
	h.globalLoopMu.RLock()
	ctx := h.currentLoopCtx
	userText := h.lastUserText
	h.globalLoopMu.RUnlock()
	if ctx != nil && !ctx.IsCancelled() {
		rs.MainAgentRunning = true
		rs.MainAgentTask = userText
		rs.MainAgentElapsed = time.Since(ctx.StartedAt).Round(time.Second)
	}
}

// ---------------------------------------------------------------------------
// agent_status tool — consumed by /btw SubAgent
// ---------------------------------------------------------------------------

// toolAgentStatus queries the main agent's runtime state.
// This is a read-only tool — it does not modify any state.
func (h *IMMessageHandler) toolAgentStatus(args map[string]interface{}) string {
	category, _ := args["category"].(string)
	categoryKind := normalizeAgentStatusCategory(category)
	taskID, _ := args["task_id"].(string)
	ownerID := h.consumeRuntimePolicyOwnerIDFromToolArgsOrCurrent(args)

	// If a specific task_id is provided, do a targeted lookup with log tail.
	if taskID != "" {
		return h.agentStatusByTaskID(taskID)
	}

	rs := h.collectRuntimeStatusForOwner(ownerID)
	var sections []string

	// Main agent loop status — always included for "all" category.
	if categoryKind.IncludesMainAgent() {
		if rs.MainAgentRunning {
			taskPreview := rs.MainAgentTask
			if len([]rune(taskPreview)) > 80 {
				taskPreview = string([]rune(taskPreview)[:80]) + "..."
			}
			sections = append(sections, fmt.Sprintf(
				"🔄 **主 Agent**: 正在执行任务（已运行 %s）\n   任务: %s",
				rs.MainAgentElapsed, taskPreview))
		} else {
			sections = append(sections, "🟢 **主 Agent**: 空闲")
		}
	}

	// Filter and format tasks by category.
	if categoryKind.IncludesLocalTasks() {
		if s := formatTaskSection("📋 **本地后台任务**", rs.Tasks, "local"); s != "" {
			sections = append(sections, s)
		}
	}
	if categoryKind.IncludesSSHTasks() {
		if s := formatTaskSection("🖥️ **SSH 后台任务**", rs.Tasks, "ssh"); s != "" {
			sections = append(sections, s)
		}
	}
	if categoryKind.IncludesCodingSessions() {
		if s := formatSessionSection("💻 **编程会话**", rs.Sessions, "coding"); s != "" {
			sections = append(sections, s)
		}
	}
	if categoryKind.IncludesSSHSessions() {
		if s := formatSessionSection("🔗 **SSH 连接**", rs.Sessions, "ssh"); s != "" {
			sections = append(sections, s)
		}
	}

	if len(sections) == 0 {
		return fmt.Sprintf("查询类别 %q 没有活跃的资源。", categoryKind.String())
	}

	return strings.Join(sections, "\n\n")
}

// agentStatusByTaskID looks up a specific task by ID across all managers.
// Returns detailed status + log tail (not available from collectRuntimeStatus).
func (h *IMMessageHandler) agentStatusByTaskID(taskID string) string {
	// Try local background tasks first.
	if h.localBgTaskMgr != nil {
		if status, err := h.localBgTaskMgr.Check(taskID, 30); err == nil {
			return formatTaskStatus(status)
		}
	}

	// Try SSH background tasks.
	if h.bgTaskMgr != nil {
		if status, err := h.bgTaskMgr.CheckTask(taskID, 30); err == nil {
			return formatSSHTaskStatusForBtw(status)
		}
	}

	return fmt.Sprintf("未找到任务 %s（已检查本地后台任务和 SSH 后台任务）", taskID)
}

// ---------------------------------------------------------------------------
// Formatting helpers
// ---------------------------------------------------------------------------

func formatTaskSection(header string, tasks []RuntimeTaskInfo, source runtimeTaskSource) string {
	var b strings.Builder
	count := 0
	for _, t := range tasks {
		if t.Source != source {
			continue
		}
		if count == 0 {
			b.WriteString(header + "\n")
		}
		count++
		icon := t.Status.Icon()
		cmdShort := truncateRunesForSubAgent(t.Command, 60)
		elapsed := time.Since(t.StartedAt).Round(time.Second)
		role := strings.TrimSpace(t.TaskRole)
		if role == "" {
			role = "command"
		}
		fmt.Fprintf(&b, "%s %s [%s role=%s] 已运行=%s 命令=%s", icon, t.TaskID, t.Status, role, elapsed, cmdShort)
		if t.Status.HasExitCode() && t.Source == runtimeTaskSourceLocal {
			fmt.Fprintf(&b, " 退出码=%d", t.ExitCode)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func formatSessionSection(header string, sessions []RuntimeSessionInfo, source runtimeSessionSource) string {
	var b strings.Builder
	count := 0
	for _, s := range sessions {
		if s.Source != source {
			continue
		}
		if count == 0 {
			b.WriteString(header + "\n")
		}
		count++
		if s.Label != "" {
			fmt.Fprintf(&b, "- %s [%s] %s\n", s.ID, s.Status, s.Label)
		} else {
			fmt.Fprintf(&b, "- %s [%s]\n", s.ID, s.Status)
		}
	}
	return b.String()
}

// formatSSHTaskStatusForBtw formats an SSH background task status for /btw display.
func formatSSHTaskStatusForBtw(s *remote.BackgroundTaskStatus) string {
	var b strings.Builder
	icon := normalizeRuntimeTaskStatus(s.Status).Icon()
	fmt.Fprintf(&b, "%s SSH 任务 %s: %s\n", icon, s.TaskID, s.Status)
	fmt.Fprintf(&b, "已运行: %s | 日志大小: %s\n", s.Elapsed, s.LogSize)
	if s.LogTail != "" {
		fmt.Fprintf(&b, "\n--- 日志最后内容 ---\n%s", s.LogTail)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// pendingBackgroundTaskHint consumer — formats runtime status for the main
// agent loop's recover prompt. Only includes running tasks started after
// loopStart to avoid stale tasks from previous conversations.
// ---------------------------------------------------------------------------

// pendingBackgroundTaskHintFromStatus formats running tasks from a
// RuntimeStatus snapshot for the recover prompt. Filters by loopStart.
// This replaces the old pendingBackgroundTaskHint's inline enumeration.
func pendingBackgroundTaskHintFromStatus(rs RuntimeStatus, loopStart time.Time) string {
	var hints []string

	for _, t := range pendingBackgroundTasksFromStatus(rs, loopStart) {
		elapsed := time.Since(t.StartedAt).Round(time.Second)
		cmd := t.Command
		if len([]rune(cmd)) > 80 {
			cmd = string([]rune(cmd)[:80]) + "..."
		}

		switch t.Source {
		case runtimeTaskSourceSSH:
			hints = append(hints, fmt.Sprintf(
				"- SSH 后台任务 %s 仍在运行（已 %s），命令: %s → 请调用 ssh(action=\"check_task\", task_id=\"%s\") 查看进度；若预计很快结束，可调用 ssh(action=\"wait_task\", task_id=\"%s\", timeout=60) 有界等待",
				t.TaskID, elapsed, cmd, t.TaskID, t.TaskID))
		case runtimeTaskSourceLocal:
			hints = append(hints, fmt.Sprintf(
				"- 本地后台任务 %s 仍在运行（已 %s），命令: %s → 请调用 async_wait(action=\"check\", task_id=\"%s\") 查看进度",
				t.TaskID, elapsed, cmd, t.TaskID))
		}
	}

	if len(hints) == 0 {
		return ""
	}
	return "⚠️ 检测到以下后台任务仍在运行，请优先检查其状态：\n" + strings.Join(hints, "\n")
}

func hasPendingBackgroundTaskFromStatus(rs RuntimeStatus, loopStart time.Time) bool {
	return len(pendingBackgroundTasksFromStatus(rs, loopStart)) > 0
}

func pendingBackgroundTaskKeyFromStatus(rs RuntimeStatus, loopStart time.Time) string {
	tasks := pendingBackgroundTasksFromStatus(rs, loopStart)
	if len(tasks) == 0 {
		return ""
	}
	ids := make([]string, 0, len(tasks))
	seen := make(map[string]struct{}, len(tasks))
	for _, t := range tasks {
		id := fmt.Sprintf("%s:%s", t.Source, t.TaskID)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

func pendingBackgroundTasksFromStatus(rs RuntimeStatus, loopStart time.Time) []RuntimeTaskInfo {
	pending := make([]RuntimeTaskInfo, 0, len(rs.Tasks))
	seen := make(map[string]struct{}, len(rs.Tasks))
	for _, t := range rs.Tasks {
		t.TaskID = strings.TrimSpace(t.TaskID)
		if t.TaskID == "" {
			continue
		}
		if !isCommandRuntimeTaskRole(t.TaskRole) {
			continue
		}
		key := fmt.Sprintf("%s:%s", t.Source, t.TaskID)
		if _, ok := seen[key]; ok {
			continue
		}
		if !t.Status.IsActive() {
			continue
		}
		if t.StartedAt.Before(loopStart) {
			continue
		}
		seen[key] = struct{}{}
		pending = append(pending, t)
	}
	sort.Slice(pending, func(i, j int) bool {
		if pending[i].Source != pending[j].Source {
			return pending[i].Source < pending[j].Source
		}
		if pending[i].TaskID != pending[j].TaskID {
			return pending[i].TaskID < pending[j].TaskID
		}
		return pending[i].StartedAt.Before(pending[j].StartedAt)
	})
	return pending
}

func isCommandRuntimeTaskRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "", "command":
		return true
	case "monitor", "poll":
		return false
	default:
		return true
	}
}
