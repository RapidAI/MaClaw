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
	actionText, _ := args["action"].(string)
	action := classifySSHToolAction(actionText)
	switch action {
	case sshToolActionConnect:
		return h.sshConnect(args)
	case sshToolActionExec:
		return h.sshExec(args)
	case sshToolActionExecBackground:
		return h.sshExecBackground(args)
	case sshToolActionCheckTask:
		return h.sshCheckTask(args)
	case sshToolActionListTasks:
		return h.sshListTasks()
	case sshToolActionKillTask:
		return h.sshKillTask(args)
	case sshToolActionSudoPrepare:
		return h.sshSudoPrepare(args)
	case sshToolActionUpload:
		return h.sshUpload(args)
	case sshToolActionDownload:
		return h.sshDownload(args)
	case sshToolActionList:
		return h.sshList()
	case sshToolActionClose:
		return h.sshClose(args)
	case sshToolActionCloseAll:
		return h.sshCloseAll()
	default:
		return fmt.Sprintf("未知 SSH 操作: %s（支持: connect/exec/exec_background/check_task/list_tasks/kill_task/sudo_prepare/upload/download/list/close/close_all）", action)
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

	// Check if a running session already exists for this host.
	// Reuse it instead of creating duplicate sessions — this prevents
	// the common problem of the LLM spawning many sessions to the same
	// host when previous ones time out or become unresponsive.
	// Use force_new=true to bypass and create a new session.
	forceNew, _ := args["force_new"].(bool)
	if !forceNew {
		targetID := fmt.Sprintf("%s@%s:%d", user, host, port)
		if existing := h.findRunningSSHSession(mgr, targetID, label); existing != nil {
			// Verify the connection is actually alive before reusing.
			// The session status may say "running" but the underlying TCP
			// connection could be dead (e.g. network interruption, server
			// restart, docker compose down).
			if existing.Handle != nil && existing.Handle.IsAlive() {
				// Connection is alive, but shell might be stuck (e.g. sqlite3 lock,
				// interactive program). Verify shell responsiveness.
				if mgr.CheckShellResponsive(existing.ID) {
					summary := existing.GetSummary()
					result := fmt.Sprintf("♻️ 复用已有 SSH 会话\n会话 ID: %s\n主机: %s\n状态: %s",
						existing.ID, summary.HostID, summary.Status)
					if summary.LastOutput != "" {
						result += "\n\n最近输出: " + summary.LastOutput
					}
					return result
				}
				// Shell is unresponsive (stuck process). Close and recreate.
				mgr.RemoveSession(existing.ID)
				h.completeSSHBackgroundLoop(existing.ID)
			} else {
				// Connection is dead. Try to reconnect the existing session.
				if err := mgr.ReconnectByID(existing.ID); err == nil {
					// Wait for shell init after reconnect.
					time.Sleep(2 * time.Second)
					preview := strings.Join(existing.PreviewTail(10), "\n")
					result := fmt.Sprintf("♻️ 复用已有 SSH 会话（已自动重连）\n会话 ID: %s\n主机: %s\n状态: running",
						existing.ID, targetID)
					if preview != "" {
						result += "\n\n--- 重连后输出 ---\n" + preview
					}
					return result
				}
				// Reconnection failed. Close the dead session so we can create
				// a fresh one below instead of looping on the same dead session.
				mgr.RemoveSession(existing.ID)
				h.completeSSHBackgroundLoop(existing.ID)
			}
		}
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
		errMsg := fmt.Sprintf("SSH 连接失败: %v", err)
		if classifySSHError(err) == sshErrorAuthentication {
			if cfg.Password == "" {
				errMsg += "\n\n💡 认证失败且未提供密码。请使用 password 参数重试，例如：\nssh connect host=... user=... port=... password=<密码> auth_method=password"
			} else {
				errMsg += "\n\n💡 已提供密码但认证仍失败，请检查密码是否正确"
			}
		}
		return errMsg
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

// findRunningSSHSession looks for an existing SSH session matching the
// given hostID (user@host:port) or label that is still running.
func (h *IMMessageHandler) findRunningSSHSession(mgr interface {
	List() []*remote.SSHManagedSession
}, hostID, label string) *remote.SSHManagedSession {
	for _, s := range mgr.List() {
		summary := s.GetSummary()
		if !remote.SessionStatus(summary.Status).IsRunning() {
			continue
		}
		if summary.HostID == hostID || (label != "" && summary.HostLabel == label) {
			return s
		}
	}
	return nil
}

func (h *IMMessageHandler) sshExec(args map[string]interface{}) string {
	mgr := h.ensureSSHManager()

	sessionID, _ := args["session_id"].(string)
	command, _ := args["command"].(string)
	if sessionID == "" || command == "" {
		return "错误: exec 需要 session_id 和 command 参数"
	}

	// 自动升级：长时间命令且 wait_seconds 未显式设置大值时，自动转为后台模式
	waitSec := 15
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
			return fmt.Sprintf("SSH 会话已断开，自动重连失败: %v\n\n💡 建议使用 ssh(action=close, session_id=%s) 关闭此会话，然后重新 connect", err, sessionID)
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

	// 判断 exec 是否有效产出（排除 maclaw 系统消息）
	hasOutput := false
	for _, line := range newLines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "[maclaw]") {
			hasOutput = true
			break
		}
	}
	if hasOutput {
		mgr.RecordExecSuccess(sessionID)
	} else {
		failCount := mgr.RecordExecFailure(sessionID)
		// 连续 3 次无输出，shell 可能被锁住。
		// 尝试发送 Ctrl+C 中断 + 验证 shell 响应性。
		if failCount >= 3 {
			responsive := mgr.CheckShellResponsive(sessionID)
			if !responsive {
				// Shell 无响应，自动关闭并提示重建
				mgr.RemoveSession(sessionID)
				h.completeSSHBackgroundLoop(sessionID)
				return fmt.Sprintf("⚠️ SSH 会话 %s 连续 %d 次执行无响应，shell 可能被挂起的进程锁住。\n"+
					"已自动关闭此会话。请使用 ssh(action=connect, ...) 重新建立连接。\n\n"+
					"💡 如果远程服务器上有挂起的进程（如 sqlite3），重连后可用 `kill` 命令清理",
					sessionID, failCount)
			}
			// Ctrl+C 恢复了 shell，重置计数
			mgr.RecordExecSuccess(sessionID)
			output = "(前几次命令无响应，已发送 Ctrl+C 恢复 shell。请重新执行命令)"
		}
	}

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
	switch normalizeLocalBackgroundTaskStatus(result.Status) {
	case localBackgroundTaskStatusCompleted:
		statusEmoji = "✅"
	case localBackgroundTaskStatusFailed:
		statusEmoji = "❌"
	case localBackgroundTaskStatusKilled:
		statusEmoji = "🛑"
	case localBackgroundTaskStatusUnknownID:
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

// sshSudoPrepare 预先获取 sudo token，使后续后台任务可以使用 sudo。
func (h *IMMessageHandler) sshSudoPrepare(args map[string]interface{}) string {
	if h.bgTaskMgr == nil {
		return "错误: 后台任务管理器未初始化"
	}

	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return "错误: sudo_prepare 需要 session_id 参数"
	}

	ok, msg := h.bgTaskMgr.EnsureSudoToken(sessionID)
	if ok {
		return fmt.Sprintf("✅ %s", msg)
	}
	return fmt.Sprintf("⚠️ %s", msg)
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

	// 先尝试发送 SIGKILL 终止远程进程（忽略错误，会话可能已断开）
	_ = h.sshMgr.Kill(sessionID)

	// 从 sessions map 中移除会话并关闭 handle，
	// 确保后续 sshConnect 不会复用这个已关闭的会话。
	h.sshMgr.RemoveSession(sessionID)

	// Complete the corresponding background loop.
	h.completeSSHBackgroundLoop(sessionID)

	return fmt.Sprintf("✅ SSH 会话 %s 已关闭", sessionID)
}

// sshCloseAll closes all running SSH sessions.
func (h *IMMessageHandler) sshCloseAll() string {
	if h.sshMgr == nil {
		return "当前无 SSH 会话"
	}
	sessions := h.sshMgr.List()
	running := make([]*remote.SSHManagedSession, 0, len(sessions))
	for _, s := range sessions {
		summary := s.GetSummary()
		if remote.SessionStatus(summary.Status).IsRunning() {
			running = append(running, s)
		}
	}
	if len(running) == 0 {
		return "当前无运行中的 SSH 会话"
	}
	for _, s := range running {
		_ = h.sshMgr.Kill(s.ID)
		h.sshMgr.RemoveSession(s.ID)
		h.completeSSHBackgroundLoop(s.ID)
	}
	return fmt.Sprintf("✅ 已关闭 %d 个 SSH 会话", len(running))
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
		ctx.SetLoopState(LoopStateRunning)
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
	cfg, err := h.loadConfig()
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
