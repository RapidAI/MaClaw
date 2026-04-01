package main

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// ---------------------------------------------------------------------------
// SSH tool implementations for GUI IM handler.
// SSH sessions are registered as background tasks (SlotKindSSH) so the user
// can monitor them in the GUI "任务后台" panel without direct interaction.
// ---------------------------------------------------------------------------

// sshMgrOnce guards lazy initialisation of the SSH session manager.
var sshMgrOnce sync.Once

// ensureSSHManager lazily initialises the SSH session manager (thread-safe).
func (h *IMMessageHandler) ensureSSHManager() *remote.SSHSessionManager {
	sshMgrOnce.Do(func() {
		h.sshMgr = remote.NewSSHSessionManager(nil)
		h.bgTaskMgr = remote.NewSSHBackgroundTaskManager(h.sshMgr)

		// When an SSH session exits (abnormal disconnect, remote close, etc.)
		// automatically mark the corresponding background loop as completed.
		h.sshMgr.SetOnUpdate(func(sessionID string) {
			if h.bgManager == nil {
				return
			}
			// Check if the session has terminated.
			status, ok := h.sshMgr.GetSessionStatus(sessionID)
			if ok && (status == remote.SessionExited || status == remote.SessionError) {
				h.completeSSHBackgroundLoop(sessionID)
			}
			h.bgManager.NotifyChange()
		})
	})
	return h.sshMgr
}

// toolSSH is the unified SSH tool entry point, dispatching by action.
func (h *IMMessageHandler) toolSSH(args map[string]interface{}) string {
	action, _ := args["action"].(string)
	switch action {
	case "connect":
		return h.sshConnect(args)
	case "exec":
		return h.sshExec(args)
	case "exec_background":
		return h.sshExecBackground(args)
	case "check_task":
		return h.sshCheckTask(args)
	case "list_tasks":
		return h.sshListTasks()
	case "kill_task":
		return h.sshKillTask(args)
	case "upload":
		return h.sshUpload(args)
	case "download":
		return h.sshDownload(args)
	case "list":
		return h.sshList()
	case "close":
		return h.sshClose(args)
	default:
		return fmt.Sprintf("未知 SSH 操作: %s（支持: connect/exec/exec_background/check_task/list_tasks/kill_task/upload/download/list/close）", action)
	}
}

func (h *IMMessageHandler) sshConnect(args map[string]interface{}) string {
	mgr := h.ensureSSHManager()

	host, _ := args["host"].(string)
	user, _ := args["user"].(string)
	label, _ := args["label"].(string)

	// Resolve pre-configured host by label.
	if (host == "" || user == "") && label != "" {
		if entry := h.resolveSSHHostByLabel(label); entry != nil {
			host = entry.Host
			user = entry.User
			if args["port"] == nil && entry.Port > 0 {
				args["port"] = float64(entry.Port)
			}
			if s, _ := args["auth_method"].(string); s == "" && entry.AuthMethod != "" {
				args["auth_method"] = entry.AuthMethod
			}
			if s, _ := args["key_path"].(string); s == "" && entry.KeyPath != "" {
				args["key_path"] = entry.KeyPath
			}
			label = entry.Label
		}
	}

	if host == "" || user == "" {
		return "错误: connect 需要 host 和 user 参数（或通过 label 引用已配置主机）"
	}

	port := 22
	if p, ok := args["port"].(float64); ok && p > 0 {
		port = int(p)
	}

	cfg := remote.SSHHostConfig{
		Host:       host,
		User:       user,
		Port:       port,
		AuthMethod: sshStrArg(args, "auth_method"),
		KeyPath:    sshStrArg(args, "key_path"),
		Password:   sshStrArg(args, "password"),
		Label:      label,
	}

	spec := remote.SSHSessionSpec{
		HostConfig:     cfg,
		InitialCommand: sshStrArg(args, "initial_command"),
		Cols:           120,
		Rows:           40,
	}

	session, err := mgr.Create(spec)
	if err != nil {
		return fmt.Sprintf("SSH 连接失败: %v", err)
	}

	// Register as a background task for GUI monitoring.
	h.registerSSHBackgroundLoop(session, cfg)

	// Wait for shell init.
	time.Sleep(2 * time.Second)

	preview := strings.Join(session.PreviewTail(20), "\n")

	result := fmt.Sprintf("✅ SSH 连接成功\n会话 ID: %s\n主机: %s\n状态: running",
		session.ID, cfg.SSHHostID())
	if preview != "" {
		result += "\n\n--- 初始输出 ---\n" + preview
	}
	return result
}

func (h *IMMessageHandler) sshExec(args map[string]interface{}) string {
	mgr := h.ensureSSHManager()

	sessionID, _ := args["session_id"].(string)
	command, _ := args["command"].(string)
	if sessionID == "" || command == "" {
		return "错误: exec 需要 session_id 和 command 参数"
	}

	// 自动升级：长时间命令且 wait_seconds 未显式设置大值时，自动转为后台模式
	waitSec := 5
	if w, ok := args["wait_seconds"].(float64); ok && w > 0 {
		waitSec = int(w)
	}
	if remote.IsLongRunningCommand(command) && waitSec <= 30 {
		return h.sshExecBackground(args)
	}

	session, ok := mgr.Get(sessionID)
	if !ok {
		return fmt.Sprintf("错误: SSH 会话 %s 不存在", sessionID)
	}

	reconnectNote := ""

	// 检查会话是否已断开，如果是则自动重连
	status, _ := mgr.GetSessionStatus(sessionID)
	sessionDead := status == remote.SessionExited || status == remote.SessionError

	if sessionDead {
		if err := mgr.ReconnectByID(sessionID); err != nil {
			return fmt.Sprintf("SSH 会话已断开，自动重连失败: %v", err)
		}
		reconnectNote = "⚠️ 连接已断开并自动重连\n"
		time.Sleep(2 * time.Second)
	}

	linesBefore := session.LineCount()

	if sessionDead {
		if err := mgr.WriteInput(sessionID, command); err != nil {
			return fmt.Sprintf("%s发送命令失败: %v", reconnectNote, err)
		}
	} else {
		reconnected, err := mgr.WriteInputChecked(sessionID, command)
		if err != nil {
			return fmt.Sprintf("发送命令失败: %v", err)
		}
		if reconnected {
			reconnectNote = "⚠️ 连接已断开并自动重连\n"
			time.Sleep(2 * time.Second)
			linesBefore = session.LineCount()
		}
	}

	if waitSec > 600 {
		waitSec = 600
	}
	maxWait := time.Duration(waitSec) * time.Second

	newLines, status := mgr.WaitForOutput(sessionID, linesBefore, maxWait)

	output := strings.Join(newLines, "\n")
	if output == "" {
		output = "(无新输出)"
	}
	if len(output) > 8000 {
		output = output[:4000] + "\n... (截断) ...\n" + output[len(output)-4000:]
	}

	// Update background loop iteration count.
	h.bumpSSHLoopIteration(sessionID)

	return fmt.Sprintf("%s[%s] 状态: %s\n$ %s\n%s", reconnectNote, sessionID, string(status), command, output)
}

// sshExecBackground runs a long-running command in the background via nohup.
func (h *IMMessageHandler) sshExecBackground(args map[string]interface{}) string {
	mgr := h.ensureSSHManager()
	if h.bgTaskMgr == nil {
		h.bgTaskMgr = remote.NewSSHBackgroundTaskManager(mgr)
	}

	sessionID, _ := args["session_id"].(string)
	command, _ := args["command"].(string)
	if sessionID == "" || command == "" {
		return "错误: exec_background 需要 session_id 和 command 参数"
	}

	// Auto-reconnect if needed.
	status, _ := mgr.GetSessionStatus(sessionID)
	if status == remote.SessionExited || status == remote.SessionError {
		if err := mgr.ReconnectByID(sessionID); err != nil {
			return fmt.Sprintf("SSH 会话已断开，自动重连失败: %v", err)
		}
		time.Sleep(2 * time.Second)
	}

	task, err := h.bgTaskMgr.Submit(sessionID, command)
	if err != nil {
		return fmt.Sprintf("提交后台任务失败: %v", err)
	}

	h.bumpSSHLoopIteration(sessionID)

	return fmt.Sprintf("✅ 后台任务已提交\n"+
		"任务 ID: %s\n"+
		"命令: %s\n"+
		"日志文件: %s\n"+
		"PID: %s\n"+
		"状态: running\n\n"+
		"💡 使用 check_task (task_id=%s) 查看进度\n"+
		"💡 SSH 断连不影响任务执行，重连后可继续查看",
		task.TaskID, task.Command, task.LogFile, task.PID, task.TaskID)
}

// sshCheckTask checks the status and latest log output of a background task.
func (h *IMMessageHandler) sshCheckTask(args map[string]interface{}) string {
	if h.bgTaskMgr == nil {
		return "错误: 无后台任务"
	}

	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return "错误: check_task 需要 task_id 参数"
	}

	tailLines := 50
	if t, ok := args["tail_lines"].(float64); ok && t > 0 {
		tailLines = int(t)
	}

	result, err := h.bgTaskMgr.CheckTask(taskID, tailLines)
	if err != nil {
		return fmt.Sprintf("检查任务失败: %v", err)
	}

	statusEmoji := "🔄"
	switch result.Status {
	case "completed":
		statusEmoji = "✅"
	case "failed":
		statusEmoji = "❌"
	case "killed":
		statusEmoji = "🛑"
	case "unknown":
		statusEmoji = "❓"
	}

	logTail := result.LogTail
	if logTail == "" {
		logTail = "(无日志输出)"
	}
	if len(logTail) > 6000 {
		logTail = logTail[:3000] + "\n... (截断) ...\n" + logTail[len(logTail)-3000:]
	}

	return fmt.Sprintf("%s 任务 %s\n命令: %s\n状态: %s\n进程存活: %v\n已运行: %s\n\n--- 最新日志 ---\n%s",
		statusEmoji, result.TaskID, result.Command, result.Status,
		result.IsAlive, result.Elapsed, logTail)
}

// sshListTasks lists all background tasks.
func (h *IMMessageHandler) sshListTasks() string {
	if h.bgTaskMgr == nil {
		return "当前无后台任务"
	}

	tasks := h.bgTaskMgr.ListTasks()
	if len(tasks) == 0 {
		return "当前无后台任务"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("后台任务（%d 个）:\n", len(tasks)))
	for _, t := range tasks {
		elapsed := time.Since(t.StartedAt).Round(time.Second)
		sb.WriteString(fmt.Sprintf("  - %s | PID: %s | 状态: %s | 已运行: %s\n    命令: %s\n",
			t.TaskID, t.PID, t.Status, elapsed, t.Command))
	}
	return sb.String()
}

// sshKillTask terminates a background task.
func (h *IMMessageHandler) sshKillTask(args map[string]interface{}) string {
	if h.bgTaskMgr == nil {
		return "错误: 无后台任务"
	}

	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return "错误: kill_task 需要 task_id 参数"
	}

	if err := h.bgTaskMgr.KillTask(taskID); err != nil {
		return fmt.Sprintf("终止任务失败: %v", err)
	}
	return fmt.Sprintf("✅ 后台任务 %s 已终止", taskID)
}

// sshUpload uploads a local file/directory to the remote server via SFTP.
func (h *IMMessageHandler) sshUpload(args map[string]interface{}) string {
	mgr := h.ensureSSHManager()
	sessionID, _ := args["session_id"].(string)
	localPath, _ := args["local_path"].(string)
	remotePath, _ := args["remote_path"].(string)
	if sessionID == "" || localPath == "" || remotePath == "" {
		return "错误: upload 需要 session_id、local_path 和 remote_path 参数"
	}
	result, err := mgr.SFTPTransfer(sessionID, "upload", localPath, remotePath)
	if err != nil {
		return fmt.Sprintf("上传失败: %v", err)
	}
	return fmt.Sprintf("✅ 上传完成: %s → %s\n%s", localPath, remotePath, result)
}

// sshDownload downloads a remote file/directory to local via SFTP.
func (h *IMMessageHandler) sshDownload(args map[string]interface{}) string {
	mgr := h.ensureSSHManager()
	sessionID, _ := args["session_id"].(string)
	localPath, _ := args["local_path"].(string)
	remotePath, _ := args["remote_path"].(string)
	if sessionID == "" || localPath == "" || remotePath == "" {
		return "错误: download 需要 session_id、local_path 和 remote_path 参数"
	}
	result, err := mgr.SFTPTransfer(sessionID, "download", localPath, remotePath)
	if err != nil {
		return fmt.Sprintf("下载失败: %v", err)
	}
	return fmt.Sprintf("✅ 下载完成: %s → %s\n%s", remotePath, localPath, result)
}

func (h *IMMessageHandler) sshList() string {
	if h.sshMgr == nil {
		return "当前无活跃 SSH 会话"
	}

	sessions := h.sshMgr.List()
	if len(sessions) == 0 {
		return "当前无活跃 SSH 会话"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("SSH 会话（%d 个）:\n", len(sessions)))
	for _, s := range sessions {
		summary := s.GetSummary()
		sb.WriteString(fmt.Sprintf("  - %s | %s | 状态: %s\n",
			s.ID, summary.HostLabel, summary.Status))
	}

	poolStats := h.sshMgr.Pool().Stats()
	if len(poolStats) > 0 {
		sb.WriteString("连接池:\n")
		for hostID, ref := range poolStats {
			sb.WriteString(fmt.Sprintf("  - %s (引用: %d)\n", hostID, ref))
		}
	}
	return sb.String()
}

func (h *IMMessageHandler) sshClose(args map[string]interface{}) string {
	if h.sshMgr == nil {
		return "错误: SSH 会话管理器未初始化"
	}

	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return "错误: close 需要 session_id 参数"
	}

	if err := h.sshMgr.Kill(sessionID); err != nil {
		return fmt.Sprintf("关闭失败: %v", err)
	}

	// Complete the corresponding background loop.
	h.completeSSHBackgroundLoop(sessionID)

	return fmt.Sprintf("✅ SSH 会话 %s 已关闭", sessionID)
}

// ---------------------------------------------------------------------------
// Background loop integration helpers
// ---------------------------------------------------------------------------

// registerSSHBackgroundLoop creates a BackgroundLoopManager entry for an SSH
// session so it appears in the GUI "任务后台" panel.
func (h *IMMessageHandler) registerSSHBackgroundLoop(session *remote.SSHManagedSession, cfg remote.SSHHostConfig) {
	if h.bgManager == nil {
		return
	}
	desc := fmt.Sprintf("SSH: %s", cfg.SSHHostID())
	if cfg.Label != "" {
		desc = fmt.Sprintf("SSH: %s (%s)", cfg.SSHHostID(), cfg.Label)
	}

	ctx := h.bgManager.Spawn(SlotKindSSH, "", desc, 0, nil)
	if ctx != nil {
		ctx.SessionID = session.ID
		ctx.SetState("running")
	}
}

// completeSSHBackgroundLoop marks the background loop as completed when the
// SSH session is closed or disconnected.
func (h *IMMessageHandler) completeSSHBackgroundLoop(sessionID string) {
	if h.bgManager == nil {
		return
	}
	for _, ctx := range h.bgManager.List() {
		if ctx.SlotKind == SlotKindSSH && ctx.SessionID == sessionID {
			h.bgManager.Complete(ctx.ID)
			return
		}
	}
}

// bumpSSHLoopIteration increments the iteration counter of the background
// loop associated with the given SSH session, giving the user a sense of
// activity in the "任务后台" panel.
func (h *IMMessageHandler) bumpSSHLoopIteration(sessionID string) {
	if h.bgManager == nil {
		return
	}
	for _, ctx := range h.bgManager.List() {
		if ctx.SlotKind == SlotKindSSH && ctx.SessionID == sessionID {
			ctx.IncrementIteration()
			h.bgManager.NotifyChange()
			return
		}
	}
}

// ---------------------------------------------------------------------------
// Config helpers
// ---------------------------------------------------------------------------

// resolveSSHHostByLabel looks up a pre-configured SSH host by label.
func (h *IMMessageHandler) resolveSSHHostByLabel(label string) *corelib.SSHHostEntry {
	hosts := h.loadSSHHosts()
	label = strings.ToLower(strings.TrimSpace(label))
	for i := range hosts {
		if strings.ToLower(hosts[i].Label) == label {
			return &hosts[i]
		}
	}
	// Fuzzy fallback: label contains keyword.
	for i := range hosts {
		if strings.Contains(strings.ToLower(hosts[i].Label), label) {
			return &hosts[i]
		}
	}
	return nil
}

func (h *IMMessageHandler) loadSSHHosts() []corelib.SSHHostEntry {
	cfg, err := h.app.LoadConfig()
	if err != nil {
		return nil
	}
	return cfg.SSHHosts
}

// sshStrArg extracts a string from an args map (SSH-tool-specific helper).
func sshStrArg(args map[string]interface{}, key string) string {
	s, _ := args[key].(string)
	return s
}
