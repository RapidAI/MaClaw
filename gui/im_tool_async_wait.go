package main

// async_wait tool: unified interface for managing local background tasks.
//
// This tool works with bash(background=true) to provide a complete local
// background process lifecycle, mirroring the SSH BackgroundTaskManager:
//
//   bash(command="...", background=true) → task_id  (Submit)
//   async_wait(action="check", task_id="...") → status + log tail  (Check)
//   async_wait(action="wait", task_id="...", timeout=60) → blocks until done  (Wait)
//   async_wait(action="kill", task_id="...") → terminate  (Kill)
//   async_wait(action="list") → all tasks  (List)
//
// The key mechanism: bash(background=true) starts the process and captures
// PID + log file path. async_wait queries/waits on that captured metadata.
// No guessing of file paths or PIDs by the LLM — the system manages it.

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	asyncWaitMaxTimeout  = 300 // seconds
	asyncWaitDefaultTail = 50  // lines
)

var localBgTaskMgrOnce sync.Once

// ensureLocalBgTaskMgr lazily initializes the local background task manager.
// Thread-safe via sync.Once.
func (h *IMMessageHandler) ensureLocalBgTaskMgr() *coretool.LocalBackgroundTaskManager {
	localBgTaskMgrOnce.Do(func() {
		logDir := filepath.Join(corelib.MaclawBaseDir(), "data", "bg_tasks")
		h.localBgTaskMgr = coretool.NewLocalBackgroundTaskManager(logDir)
	})
	return h.localBgTaskMgr
}

// toolBashBackground handles bash(background=true): submit a command to the
// local background task manager and return immediately with task metadata.
func (h *IMMessageHandler) toolBashBackground(command, workDir, taskRole string) string {
	return h.toolBashBackgroundForOwner(command, workDir, taskRole, h.currentRuntimePolicyOwnerID())
}

func (h *IMMessageHandler) toolBashBackgroundForOwner(command, workDir, taskRole, ownerID string) string {
	mgr := h.ensureLocalBgTaskMgr()
	if workDir != "" {
		workDir = resolvePath(workDir)
	} else {
		// When no explicit working_dir, use Project Tab's projectPath if available.
		workDir = h.projectTabWorkDirForOwner(ownerID)
	}

	task, err := mgr.SubmitWithOwner(command, workDir, taskRole, ownerID)
	if err != nil {
		return fmt.Sprintf("[错误] 后台任务启动失败: %v", err)
	}

	return fmt.Sprintf("✅ 后台任务已启动\n"+
		"task_id: %s\n"+
		"PID: %d\n"+
		"日志文件: %s\n"+
		"命令: %s\n\n"+
		"使用 async_wait(action=\"check\", task_id=\"%s\") 查询状态\n"+
		"使用 async_wait(action=\"wait\", task_id=\"%s\", timeout=60) 等待完成",
		task.TaskID, task.PID, task.LogFile, truncateCmd(command, 100),
		task.TaskID, task.TaskID)
}

// toolAsyncWait handles the async_wait tool: check/wait/kill/list background tasks.
func (h *IMMessageHandler) toolAsyncWait(args map[string]interface{}, onProgress coretool.ProgressCallback) string {
	ownerID, hasRuntimeOwner := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	if hasRuntimeOwner && ownerID == "" {
		return "async_wait failed: runtime owner is missing; isolated runtime will not fall back to desktop loop"
	}
	actionText := stringVal(args, "action")
	action := normalizeAsyncWaitAction(actionText)

	mgr := h.ensureLocalBgTaskMgr()

	switch action {
	case asyncWaitActionCheck:
		return h.asyncWaitCheckForOwner(mgr, args, ownerID)
	case asyncWaitActionWait:
		return h.asyncWaitWaitForOwner(mgr, args, onProgress, ownerID)
	case asyncWaitActionKill:
		return h.asyncWaitKillForOwner(mgr, args, ownerID)
	case asyncWaitActionList:
		return h.asyncWaitList(mgr, ownerID)
	default:
		return fmt.Sprintf("不支持的 action: %s。支持: check, wait, kill, list", action)
	}
}

func (h *IMMessageHandler) asyncWaitCheck(mgr *coretool.LocalBackgroundTaskManager, args map[string]interface{}) string {
	return h.asyncWaitCheckForOwner(mgr, args, h.currentRuntimePolicyOwnerID())
}

func (h *IMMessageHandler) asyncWaitCheckForOwner(mgr *coretool.LocalBackgroundTaskManager, args map[string]interface{}, ownerID string) string {
	taskID := stringVal(args, "task_id")
	if taskID == "" {
		return "缺少 task_id 参数"
	}
	if err := authorizeLocalBackgroundTaskOwner(mgr, taskID, ownerID); err != nil {
		return fmt.Sprintf("[error] %v", err)
	}

	tailLines := asyncWaitDefaultTail
	if n, ok := args["tail_lines"].(float64); ok && n > 0 {
		tailLines = int(n)
	}

	status, err := mgr.CheckForOwner(taskID, tailLines, ownerID)
	if err != nil {
		return fmt.Sprintf("[error] %v%s", err, localBackgroundTaskSnapshotForOwner(mgr, taskID, ownerID))
	}

	return formatTaskStatus(status)
}

func (h *IMMessageHandler) asyncWaitWait(mgr *coretool.LocalBackgroundTaskManager, args map[string]interface{}, onProgress coretool.ProgressCallback) string {
	return h.asyncWaitWaitForOwner(mgr, args, onProgress, h.currentRuntimePolicyOwnerID())
}

func (h *IMMessageHandler) asyncWaitWaitForOwner(mgr *coretool.LocalBackgroundTaskManager, args map[string]interface{}, onProgress coretool.ProgressCallback, ownerID string) string {
	taskID := stringVal(args, "task_id")
	if taskID == "" {
		return "缺少 task_id 参数"
	}
	if err := authorizeLocalBackgroundTaskOwner(mgr, taskID, ownerID); err != nil {
		return fmt.Sprintf("[error] %v", err)
	}

	timeout := 60 // default 60s
	if t, ok := args["timeout"].(float64); ok && t > 0 {
		timeout = int(t)
	}
	if timeout > asyncWaitMaxTimeout {
		timeout = asyncWaitMaxTimeout
	}

	tailLines := asyncWaitDefaultTail
	if n, ok := args["tail_lines"].(float64); ok && n > 0 {
		tailLines = int(n)
	}

	if onProgress != nil {
		onProgress(fmt.Sprintf("⏳ 等待后台任务 %s 完成（最长 %ds）...", taskID, timeout))
	}

	// Build a context that cancels when the agent loop is cancelled.
	ctx := context.Background()
	if loopCtx := h.runtimeLoopContextForOwner(ownerID); loopCtx != nil {
		var cancel context.CancelFunc
		ctx, cancel = loopCtx.Context()
		defer cancel()
	}

	status, err := mgr.WaitForOwner(ctx, taskID, time.Duration(timeout)*time.Second, tailLines, ownerID)
	if err != nil {
		return fmt.Sprintf("[error] %v%s", err, localBackgroundTaskSnapshotForOwner(mgr, taskID, ownerID))
	}

	return formatTaskStatus(status)
}

func (h *IMMessageHandler) runtimeLoopContextForOwner(ownerID string) *LoopContext {
	if h == nil {
		return nil
	}
	ownerID = strings.TrimSpace(ownerID)
	if ownerID != "" {
		return h.getSessionLoopCtx(ownerID)
	}
	return nil
}

func (h *IMMessageHandler) asyncWaitKill(mgr *coretool.LocalBackgroundTaskManager, args map[string]interface{}) string {
	return h.asyncWaitKillForOwner(mgr, args, h.currentRuntimePolicyOwnerID())
}

func (h *IMMessageHandler) asyncWaitKillForOwner(mgr *coretool.LocalBackgroundTaskManager, args map[string]interface{}, ownerID string) string {
	taskID := stringVal(args, "task_id")
	if taskID == "" {
		return "缺少 task_id 参数"
	}
	if err := authorizeLocalBackgroundTaskOwner(mgr, taskID, ownerID); err != nil {
		return fmt.Sprintf("[error] %v", err)
	}

	if err := mgr.KillForOwner(taskID, ownerID); err != nil {
		return fmt.Sprintf("[error] %v%s", err, localBackgroundTaskSnapshotForOwner(mgr, taskID, ownerID))
	}

	return fmt.Sprintf("✅ 后台任务 %s 已终止", taskID)
}

func authorizeLocalBackgroundTaskOwner(mgr *coretool.LocalBackgroundTaskManager, taskID, ownerID string) error {
	return mgr.AuthorizeTaskOwner(taskID, ownerID)
}

func (h *IMMessageHandler) asyncWaitList(mgr *coretool.LocalBackgroundTaskManager, ownerID string) string {
	tasks := mgr.ListForOwner(ownerID)
	if len(tasks) == 0 {
		return "当前没有后台任务"
	}

	rows := make([]string, 0, len(tasks))
	for _, t := range tasks {
		t.Lock()
		status := t.Status
		elapsed := time.Since(t.StartedAt).Round(time.Second).String()
		cmd := truncateCmd(t.Command, 60)
		taskID := t.TaskID
		pid := t.PID
		t.Unlock()
		rows = append(rows, fmt.Sprintf("- %s [%s] PID=%d 已运行=%s 命令=%s\n",
			taskID, status, pid, elapsed, cmd))
	}
	if len(rows) == 0 {
		return "当前没有后台任务"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "后台任务列表（%d 个）:\n", len(rows))
	for _, row := range rows {
		b.WriteString(row)
	}
	return b.String()
}

func localBackgroundTaskSnapshot(mgr *coretool.LocalBackgroundTaskManager, taskID string) string {
	return localBackgroundTaskSnapshotForOwner(mgr, taskID, "")
}

func localBackgroundTaskSnapshotForOwner(mgr *coretool.LocalBackgroundTaskManager, taskID, ownerID string) string {
	if mgr == nil {
		return ""
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return ""
	}
	if err := mgr.AuthorizeTaskOwner(taskID, ownerID); err != nil {
		return ""
	}
	for _, t := range mgr.List() {
		t.Lock()
		matches := strings.TrimSpace(t.TaskID) == taskID
		taskID := t.TaskID
		status := t.Status
		pid := t.PID
		startedAt := t.StartedAt
		command := t.Command
		t.Unlock()
		if !matches {
			continue
		}
		elapsed := time.Since(startedAt).Round(time.Second)
		return fmt.Sprintf("\n\n--- last known task snapshot ---\ntask_id: %s\nstatus: %s\npid: %d\nelapsed: %s\ncommand: %s",
			taskID, status, pid, elapsed, truncateRunesMiddle(command, 1200, 1200))
	}
	return "\n\n--- last known task snapshot ---\nnot found in local background task registry"
}

func formatTaskStatus(s *coretool.LocalTaskStatus) string {
	var b strings.Builder

	icon := "🔄"
	switch normalizeLocalBackgroundTaskStatus(s.Status) {
	case localBackgroundTaskStatusCompleted:
		icon = "✅"
	case localBackgroundTaskStatusFailed:
		icon = "❌"
	case localBackgroundTaskStatusKilled:
		icon = "⏹️"
	}

	fmt.Fprintf(&b, "%s 任务 %s: %s\n", icon, s.TaskID, s.Status)
	fmt.Fprintf(&b, "PID: %d | 已运行: %s | 退出码: %d | 日志大小: %d 字节\n",
		s.PID, s.Elapsed, s.ExitCode, s.LogSize)

	if s.LogTail != "" {
		fmt.Fprintf(&b, "\n--- 日志最后内容 ---\n%s", s.LogTail)
	}

	return b.String()
}

func truncateCmd(cmd string, maxLen int) string {
	if len(cmd) <= maxLen {
		return cmd
	}
	return cmd[:maxLen] + "..."
}
