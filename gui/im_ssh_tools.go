package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// ---------------------------------------------------------------------------
// SSH tool implementations for GUI IM handler.
// SSH sessions are registered as background tasks (SlotKindSSH) so the user
// can monitor them in the GUI "任务后台" panel without direct interaction.
// ---------------------------------------------------------------------------

// sshSessionAlive reports whether the given SSH session is still usable.
func (h *IMMessageHandler) sshSessionAlive(sessionID string) bool {
	if h == nil {
		return false
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return false
	}
	mgr := h.ensureSSHManager()
	if mgr == nil {
		return false
	}
	status, ok := mgr.GetSessionStatus(sessionID)
	if !ok {
		return false
	}
	return sshSessionStatusUsable(status)
}

// sshSessionStatusUsable is deliberately stricter than "record exists": a
// retained terminal session cannot accept plan execution and must be recovered.
func sshSessionStatusUsable(status remote.SessionStatus) bool {
	return status != remote.SessionExited && status != remote.SessionError
}

// ensureSSHManager lazily initialises the SSH session manager (thread-safe).
func (h *IMMessageHandler) ensureSSHManager() *remote.SSHSessionManager {
	if h == nil {
		return nil
	}
	h.sshMgrOnce.Do(func() {
		if h.sshMgr == nil {
			h.sshMgr = remote.NewSSHSessionManager(nil)
		}
		if h.bgTaskMgr == nil {
			h.bgTaskMgr = remote.NewSSHBackgroundTaskManager(h.sshMgr)
			// Layer 1: 持久化任务注册表到磁盘。
			// 进程重启后自动恢复 active 状态的任务。
			if h.app != nil {
				persistDir := h.app.GetDataDir()
				if persistDir != "" {
					h.bgTaskMgr.SetMirrorDir(filepath.Join(persistDir, "ssh_bg_task_mirrors"))
					h.bgTaskMgr.SetPersistDir(persistDir)
				}
			}
		}

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
	case sshToolActionWaitTask:
		return h.sshWaitTask(args)
	case sshToolActionListTasks:
		return h.sshListTasks(args)
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
		return fmt.Sprintf("未知 SSH 操作: %s（支持: connect/exec/exec_background/check_task/wait_task/list_tasks/kill_task/sudo_prepare/upload/download/list/close/close_all）", action)
	}
}

func (h *IMMessageHandler) sshConnect(args map[string]interface{}) string {
	mgr := h.ensureSSHManager()
	ownerID, hasRuntimeOwner := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	if hasRuntimeOwner && ownerID == "" {
		return "ssh failed: runtime owner is missing; isolated runtime will not fall back to desktop loop"
	}

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
		Host:               host,
		User:               user,
		Port:               port,
		AuthMethod:         sshStrArg(args, "auth_method"),
		KeyPath:            sshStrArg(args, "key_path"),
		Password:           sshStrArg(args, "password"),
		Label:              label,
		HostKeyFingerprint: sshStrArg(args, "host_key_fingerprint"),
	}

	// Check if a running session already exists for this host.
	// Reuse it instead of creating duplicate sessions — this prevents
	// the common problem of the LLM spawning many sessions to the same
	// host when previous ones time out or become unresponsive.
	// Use force_new=true to bypass and create a new session.
	forceNew, _ := args["force_new"].(bool)
	// A pinned connection must never reuse a session created without the same
	// host-key policy. The connection pool also isolates these configurations.
	if cfg.HostKeyFingerprint != "" {
		forceNew = true
	}
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
					h.registerSSHBackgroundLoop(existing, cfg)
					summary := existing.GetSummary()
					result := fmt.Sprintf("复用已有 SSH 会话\n会话 ID: %s\n主机: %s\n状态: %s",
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
					h.registerSSHBackgroundLoop(existing, cfg)
					// Wait for shell init after reconnect.
					time.Sleep(2 * time.Second)
					// Rediscover orphan tasks after reconnect (sync to avoid PTY race).
					if h.bgTaskMgr != nil {
						h.bgTaskMgr.RediscoverOrphanTasksForOwner(existing.ID, ownerID)
					}
					preview := strings.Join(existing.PreviewTail(10), "\n")
					result := fmt.Sprintf("复用已有 SSH 会话（已自动重连）\n会话 ID: %s\n主机: %s\n状态: running",
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
				errMsg += "\n\nSSH password was not provided; retry with password or key_path/auth_method."
			} else {
				errMsg += "\n\npassword was provided but authentication still failed; check that the password is correct"
			}
		}
		return errMsg
	}

	// Register as a background task for GUI monitoring.
	h.registerSSHBackgroundLoop(session, cfg)

	// Wait for shell init.
	time.Sleep(2 * time.Second)

	// Layer 2: 在 SSH 连接成功后，扫描远程服务器上的 orphan 后台任务。
	// 将仍在运行但本地注册表中没有的任务重新注册，避免 LLM 创建重复任务。
	// 同步执行（在 shell init 后、返回结果前）避免与后续 exec 命令竞争 PTY。
	activeTasks := 0
	if h.bgTaskMgr != nil {
		h.bgTaskMgr.RediscoverOrphanTasksForOwner(session.ID, ownerID)
		// 统计当前 running 的后台任务数
		for _, t := range h.bgTaskMgr.ListTasksForOwner(ownerID) {
			if t.Status.IsActive() {
				activeTasks++
			}
		}
	}

	preview := strings.Join(session.PreviewTail(20), "\n")

	result := fmt.Sprintf("SSH 连接成功\n会话 ID: %s\n主机: %s\n状态: running",
		session.ID, cfg.SSHHostID())
	if activeTasks > 0 {
		result += fmt.Sprintf("\n\n该服务器有 %d 个后台任务仍在运行，请先用 list_tasks 查看再决定是否需要新建任务。", activeTasks)
	}
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

	// If the command contains characters that are likely to cause shell escaping
	// issues in PTY mode (nested quotes, backslash sequences, heredoc markers),
	// automatically wrap it in a base64-decoded eval to bypass escaping entirely.
	actualCommand := command
	if needsBase64Wrapping(command) {
		b64 := base64.StdEncoding.EncodeToString([]byte(command))
		actualCommand = fmt.Sprintf("eval \"$(echo '%s' | base64 -d)\"", b64)
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
			return fmt.Sprintf("SSH 会话已断开，自动重连失败: %v\n\n建议使用 ssh(action=close, session_id=%s) 关闭此会话，然后重新 connect", err, sessionID)
		}
		reconnectNote = "连接已断开并自动重连\n"
		time.Sleep(2 * time.Second)
	}

	h.registerSSHBackgroundLoop(session, session.Spec.HostConfig)
	linesBefore := session.LineCount()

	if sessionDead {
		if err := mgr.WriteInput(sessionID, actualCommand); err != nil {
			return fmt.Sprintf("%s发送命令失败: %v", reconnectNote, err)
		}
	} else {
		reconnected, err := mgr.WriteInputChecked(sessionID, actualCommand)
		if err != nil {
			return fmt.Sprintf("发送命令失败: %v", err)
		}
		if reconnected {
			reconnectNote = "连接已断开并自动重连\n"
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
				return fmt.Sprintf("SSH 会话 %s 连续 %d 次执行无响应，shell 可能被挂起的进程锁住。\n"+
					"已自动关闭此会话。请使用 ssh(action=connect, ...) 重新建立连接。\n\n"+
					"如果远程服务器上有挂起的进程（如 sqlite3），重连后可用 `kill` 命令清理",
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
	if len([]rune(output)) > 8000 {
		output = truncateRunesMiddle(output, 4000, 4000)
	}

	// Update background loop iteration count.
	h.registerSSHBackgroundLoop(session, session.Spec.HostConfig)
	h.bumpSSHLoopIteration(sessionID)

	return fmt.Sprintf("%s[%s] 状态: %s\n$ %s\n%s", reconnectNote, sessionID, string(status), command, output)
}

// sshExecBackground runs a long-running command in the background via nohup.
func (h *IMMessageHandler) sshExecBackground(args map[string]interface{}) string {
	ownerID, hasRuntimeOwner := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	if hasRuntimeOwner && ownerID == "" {
		return "ssh failed: runtime owner is missing; isolated runtime will not fall back to desktop loop"
	}
	mgr := h.ensureSSHManager()
	if h.bgTaskMgr == nil {
		h.bgTaskMgr = remote.NewSSHBackgroundTaskManager(mgr)
		if h.app != nil {
			if persistDir := h.app.GetDataDir(); persistDir != "" {
				h.bgTaskMgr.SetMirrorDir(filepath.Join(persistDir, "ssh_bg_task_mirrors"))
				h.bgTaskMgr.SetPersistDir(persistDir)
			}
		}
	}

	sessionID, _ := args["session_id"].(string)
	command, _ := args["command"].(string)
	taskRole, _ := args["task_role"].(string)
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

	if session, ok := mgr.Get(sessionID); ok {
		h.registerSSHBackgroundLoop(session, session.Spec.HostConfig)
	}

	task, err := h.bgTaskMgr.SubmitWithOwner(sessionID, command, taskRole, ownerID)
	if err != nil {
		return fmt.Sprintf("提交后台任务失败: %v", err)
	}

	h.emitAppEvent("background-loops-changed")
	h.bumpSSHLoopIteration(sessionID)
	h.startSSHBackgroundTaskMirrorWatcher(task.TaskID, ownerID)

	if task.Reused {
		elapsed := time.Since(task.StartedAt).Round(time.Second)
		return fmt.Sprintf("检测到相同命令的任务已在运行，复用已有任务（避免重复创建）\n"+
			"任务 ID: %s\n"+
			"命令: %s\n"+
			"PID: %s\n"+
			"已运行: %s\n\n"+
			"使用 check_task (task_id=%s) 查看进度",
			task.TaskID, task.Command, task.PID, elapsed, task.TaskID)
	}

	return fmt.Sprintf("后台任务已提交\n"+
		"任务 ID: %s\n"+
		"命令: %s\n"+
		"日志文件: %s\n"+
		"PID: %s\n"+
		"状态: running\n\n"+
		"使用 check_task (task_id=%s) 查看进度\n"+
		"SSH 断连不影响任务执行，重连后可继续查看",
		task.TaskID, task.Command, task.LogFile, task.PID, task.TaskID)
}

func (h *IMMessageHandler) startSSHBackgroundTaskMirrorWatcher(taskID, ownerID string) {
	if h == nil || h.bgTaskMgr == nil {
		return
	}
	taskID = strings.TrimSpace(taskID)
	ownerID = strings.TrimSpace(ownerID)
	if taskID == "" {
		return
	}
	if !h.registerSSHBackgroundTaskMirrorWatcher(taskID) {
		return
	}
	go func() {
		defer h.unregisterSSHBackgroundTaskMirrorWatcher(taskID)
		timer := time.NewTimer(15 * time.Second)
		defer timer.Stop()
		deadline := time.Now().Add(12 * time.Hour)
		for {
			select {
			case <-timer.C:
				status, err := h.bgTaskMgr.CheckTaskForOwner(taskID, 200, ownerID)
				if err != nil {
					return
				}
				if status == nil || !normalizeRuntimeTaskStatus(status.Status).IsActive() {
					h.emitAppEvent("background-loops-changed")
					return
				}
				if time.Now().After(deadline) {
					return
				}
				timer.Reset(30 * time.Second)
			}
		}
	}()
}

func (h *IMMessageHandler) registerSSHBackgroundTaskMirrorWatcher(taskID string) bool {
	if h == nil {
		return false
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return false
	}
	h.sshMirrorWatchMu.Lock()
	defer h.sshMirrorWatchMu.Unlock()
	if h.sshMirrorWatch == nil {
		h.sshMirrorWatch = make(map[string]struct{})
	}
	if _, ok := h.sshMirrorWatch[taskID]; ok {
		return false
	}
	h.sshMirrorWatch[taskID] = struct{}{}
	return true
}

func (h *IMMessageHandler) unregisterSSHBackgroundTaskMirrorWatcher(taskID string) {
	if h == nil {
		return
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return
	}
	h.sshMirrorWatchMu.Lock()
	delete(h.sshMirrorWatch, taskID)
	h.sshMirrorWatchMu.Unlock()
}

// sshCheckTask checks the status and latest log output of a background task.
func (h *IMMessageHandler) sshCheckTask(args map[string]interface{}) string {
	if h.bgTaskMgr == nil {
		return "错误: 无后台任务"
	}
	ownerID, hasRuntimeOwner := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	if hasRuntimeOwner && ownerID == "" {
		return "ssh failed: runtime owner is missing; isolated runtime will not fall back to desktop loop"
	}

	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return "错误: check_task 需要 task_id 参数"
	}
	if err := authorizeSSHBackgroundTaskOwner(h.bgTaskMgr, taskID, ownerID); err != nil {
		return fmt.Sprintf("check_task failed: %v", err)
	}

	tailLines := 50
	if t, ok := args["tail_lines"].(float64); ok && t > 0 {
		tailLines = int(t)
	}

	result, err := h.bgTaskMgr.CheckTaskForOwner(taskID, tailLines, ownerID)
	if err != nil {
		return fmt.Sprintf("check_task failed: %v%s", err, sshBackgroundTaskSnapshotForOwner(h.bgTaskMgr, taskID, ownerID))
	}

	h.emitAppEvent("background-loops-changed")
	return formatSSHBackgroundTaskStatus(result)
}

// sshWaitTask polls a background task until it reaches a terminal state or timeout.
func (h *IMMessageHandler) sshWaitTask(args map[string]interface{}) string {
	ownerID, hasRuntimeOwner := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	if hasRuntimeOwner && ownerID == "" {
		return "ssh failed: runtime owner is missing; isolated runtime will not fall back to desktop loop"
	}
	return waitSSHBackgroundTaskForOwner(h.bgTaskMgr, args, ownerID, "error: no SSH background task manager", "error: wait_task requires task_id", func() {
		h.emitAppEvent("background-loops-changed")
	})
}

func waitSSHBackgroundTask(bgTaskMgr *remote.SSHBackgroundTaskManager, args map[string]interface{}, noManagerMsg, missingTaskMsg string, onStatusChange func()) string {
	return waitSSHBackgroundTaskForOwner(bgTaskMgr, args, "", noManagerMsg, missingTaskMsg, onStatusChange)
}

func waitSSHBackgroundTaskForOwner(bgTaskMgr *remote.SSHBackgroundTaskManager, args map[string]interface{}, ownerID, noManagerMsg, missingTaskMsg string, onStatusChange func()) string {
	if bgTaskMgr == nil {
		return noManagerMsg
	}
	taskID, _ := args["task_id"].(string)
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return missingTaskMsg
	}
	if err := authorizeSSHBackgroundTaskOwner(bgTaskMgr, taskID, ownerID); err != nil {
		return fmt.Sprintf("wait_task failed: %v", err)
	}
	tailLines := boundedIntArg(args, "tail_lines", 50, 1, 1000)
	timeout := boundedIntArg(args, "timeout", 60, 5, 600)
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	for checks := 1; ; checks++ {
		result, err := bgTaskMgr.CheckTaskForOwner(taskID, tailLines, ownerID)
		if err != nil {
			return fmt.Sprintf("wait_task failed: %v%s", err, sshBackgroundTaskSnapshotForOwner(bgTaskMgr, taskID, ownerID))
		}
		if result == nil {
			return "wait_task failed: empty task status"
		}
		formatted := formatSSHBackgroundTaskStatus(result)
		if !normalizeRuntimeTaskStatus(result.Status).IsActive() {
			if onStatusChange != nil {
				onStatusChange()
			}
			return fmt.Sprintf("wait_task reached terminal status %q after %d check(s).\n\n%s", result.Status, checks, formatted)
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Sprintf("wait_task timed out after %ds; task is still active.\n\n%s", timeout, formatted)
		}
		sleepFor := 5 * time.Second
		if remaining < sleepFor {
			sleepFor = remaining
		}
		time.Sleep(sleepFor)
	}
}

func sshBackgroundTaskSnapshot(bgTaskMgr *remote.SSHBackgroundTaskManager, taskID string) string {
	return sshBackgroundTaskSnapshotForOwner(bgTaskMgr, taskID, "")
}

func sshBackgroundTaskSnapshotForOwner(bgTaskMgr *remote.SSHBackgroundTaskManager, taskID, ownerID string) string {
	if bgTaskMgr == nil {
		return ""
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return ""
	}
	if err := bgTaskMgr.AuthorizeTaskOwner(taskID, ownerID); err != nil {
		return ""
	}
	for _, t := range bgTaskMgr.ListTasks() {
		if strings.TrimSpace(t.TaskID) != taskID {
			continue
		}
		elapsed := time.Since(t.StartedAt).Round(time.Second)
		return fmt.Sprintf("\n\n--- last known task snapshot ---\ntask_id: %s\nstatus: %s\npid: %s\nelapsed: %s\ncommand: %s%s",
			t.TaskID, t.Status, t.PID, elapsed, truncateRunesMiddle(t.Command, 1200, 1200), sshBackgroundTaskMirrorSnapshot(t.MirrorFile))
	}
	return "\n\n--- last known task snapshot ---\nnot found in SSH background task registry"
}

func sshBackgroundTaskMirrorSnapshot(mirrorFile string) string {
	mirrorFile = strings.TrimSpace(mirrorFile)
	if mirrorFile == "" {
		return ""
	}
	data, err := os.ReadFile(mirrorFile)
	if err != nil || len(data) == 0 {
		return fmt.Sprintf("\nlocal_mirror: %s", mirrorFile)
	}
	content := truncateRunesMiddle(string(data), 2000, 4000)
	return fmt.Sprintf("\nlocal_mirror: %s\n\n--- last local mirror ---\n%s", mirrorFile, content)
}

func authorizeSSHBackgroundTaskOwner(bgTaskMgr *remote.SSHBackgroundTaskManager, taskID, ownerID string) error {
	return bgTaskMgr.AuthorizeTaskOwner(taskID, ownerID)
}

func boundedIntArg(args map[string]interface{}, key string, defaultValue, minValue, maxValue int) int {
	value := defaultValue
	switch raw := args[key].(type) {
	case int:
		value = raw
	case int64:
		value = int(raw)
	case float64:
		value = int(raw)
	case float32:
		value = int(raw)
	case json.Number:
		if n, err := raw.Int64(); err == nil {
			value = int(n)
		}
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil {
			value = n
		}
	}
	if value <= 0 {
		value = defaultValue
	}
	if value > maxValue {
		return maxValue
	}
	if value < minValue {
		return minValue
	}
	return value
}

func formatSSHBackgroundTaskStatus(result *remote.BackgroundTaskStatus) string {
	if result == nil {
		return ""
	}
	statusEmoji := "[running]"
	switch normalizeRuntimeTaskStatus(result.Status) {
	case runtimeTaskStatusCompleted:
		statusEmoji = "[completed]"
	case runtimeTaskStatusFailed:
		statusEmoji = "[failed]"
	case runtimeTaskStatusKilled:
		statusEmoji = "[killed]"
	case runtimeTaskStatusUnknown:
		statusEmoji = "[unknown]"
	}

	logTail := result.LogTail
	if logTail == "" {
		logTail = "(no log output)"
	}
	if len([]rune(logTail)) > 6000 {
		logTail = truncateRunesMiddle(logTail, 3000, 3000)
	}

	exitCode := "unknown"
	if result.ExitCodeKnown {
		exitCode = fmt.Sprintf("%d", result.ExitCode)
	}
	return fmt.Sprintf("%s task %s\ncommand: %s\nstatus: %s\nexit_code: %s\nprocess_alive: %v\nelapsed: %s\nlocal_mirror: %s\n\n--- latest log ---\n%s",
		statusEmoji, result.TaskID, result.Command, result.Status,
		exitCode, result.IsAlive, result.Elapsed, result.MirrorFile, logTail)
}

func truncateRunesMiddle(s string, prefixRunes, suffixRunes int) string {
	runes := []rune(s)
	if prefixRunes < 0 {
		prefixRunes = 0
	}
	if suffixRunes < 0 {
		suffixRunes = 0
	}
	if len(runes) <= prefixRunes+suffixRunes {
		return s
	}
	return string(runes[:prefixRunes]) + "\n... (truncated) ...\n" + string(runes[len(runes)-suffixRunes:])
}

// sshListTasks lists all background tasks.
func (h *IMMessageHandler) sshListTasks(args map[string]interface{}) string {
	if h.bgTaskMgr == nil {
		return "当前无后台任务"
	}

	ownerID, hasRuntimeOwner := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	if hasRuntimeOwner && ownerID == "" {
		return "ssh failed: runtime owner is missing; isolated runtime will not fall back to desktop loop"
	}

	tasks := h.bgTaskMgr.ListTasksForOwner(ownerID)
	if len(tasks) == 0 {
		return "当前无后台任务"
	}

	rows := make([]string, 0, len(tasks))
	for _, t := range tasks {
		elapsed := time.Since(t.StartedAt).Round(time.Second)
		statusStr := string(t.Status)
		// 对于从磁盘恢复且超过 2 分钟未验证的 running 任务，标注状态待验证
		if t.Status.IsActive() && !t.LastCheck.IsZero() && time.Since(t.LastCheck) > 2*time.Minute {
			statusStr += " (状态待验证，请用 check_task 确认)"
		}
		var row strings.Builder
		fmt.Fprintf(&row, "  - %s | PID: %s | 状态: %s | 已运行: %s\n    命令: %s\n",
			t.TaskID, t.PID, statusStr, elapsed, t.Command)
		if strings.TrimSpace(t.MirrorFile) != "" {
			fmt.Fprintf(&row, "    local_mirror: %s\n", t.MirrorFile)
		}
		rows = append(rows, row.String())
	}
	if len(rows) == 0 {
		return "当前无后台任务"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "后台任务（%d 个）:\n", len(rows))
	for _, row := range rows {
		sb.WriteString(row)
	}
	return sb.String()
}

// sshKillTask terminates a background task.
func (h *IMMessageHandler) sshKillTask(args map[string]interface{}) string {
	if h.bgTaskMgr == nil {
		return "错误: 无后台任务"
	}
	ownerID, hasRuntimeOwner := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	if hasRuntimeOwner && ownerID == "" {
		return "ssh failed: runtime owner is missing; isolated runtime will not fall back to desktop loop"
	}

	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return "错误: kill_task 需要 task_id 参数"
	}
	if err := authorizeSSHBackgroundTaskOwner(h.bgTaskMgr, taskID, ownerID); err != nil {
		return fmt.Sprintf("终止任务失败: %v", err)
	}

	if err := h.bgTaskMgr.KillTaskForOwner(taskID, ownerID); err != nil {
		return fmt.Sprintf("终止任务失败: %v", err)
	}
	h.emitAppEvent("background-loops-changed")
	return fmt.Sprintf("后台任务 %s 已终止", taskID)
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
		return fmt.Sprintf("%s", msg)
	}
	return fmt.Sprintf("%s", msg)
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
	return fmt.Sprintf("上传完成: %s → %s\n%s", localPath, remotePath, result)
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
	return fmt.Sprintf("下载完成: %s → %s\n%s", remotePath, localPath, result)
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

	return fmt.Sprintf("SSH 会话 %s 已关闭", sessionID)
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
	return fmt.Sprintf("已关闭 %d 个 SSH 会话", len(running))
}

// ---------------------------------------------------------------------------
// Background loop integration helpers
// ---------------------------------------------------------------------------

// registerSSHBackgroundLoop creates a BackgroundLoopManager entry for an SSH
// session so it appears in the GUI "任务后台" panel.
func (h *IMMessageHandler) registerSSHBackgroundLoop(session *remote.SSHManagedSession, cfg remote.SSHHostConfig) {
	if h == nil || h.bgManager == nil || session == nil || session.ID == "" {
		return
	}
	for _, ctx := range h.bgManager.List() {
		if ctx.SlotKind == SlotKindSSH && ctx.SessionID == session.ID {
			ctx.SetLoopState(LoopStateRunning)
			h.bgManager.NotifyChange()
			return
		}
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

// needsBase64Wrapping returns true if a command contains characters that are
// likely to cause shell escaping issues when sent through a PTY. When true,
// sshExec wraps the command in a base64-decoded eval to bypass escaping entirely.
//
// After JSON deserialization, the command is a plain Go string with actual
// characters. The heuristic detects scenarios that break PTY passthrough:
// - Both single AND double quotes (nesting makes quoting impossible)
// - Multi-line command with quotes (PTY sends line-by-line, shell may partially execute)
// - Backticks (command substitution, interacts with PTY echo)
func needsBase64Wrapping(command string) bool {
	hasSingle := strings.Contains(command, "'")
	hasDouble := strings.Contains(command, "\"")

	// Both quote types present — high risk of nesting issues in PTY.
	if hasSingle && hasDouble {
		return true
	}

	// Multi-line command (>2 lines) with any quotes — PTY sends each line
	// followed by \n which the shell interprets as "execute". Multi-line
	// commands with quotes often break when split across PTY line boundaries.
	if strings.Count(command, "\n") > 2 && (hasSingle || hasDouble) {
		return true
	}

	// Backticks — command substitution that interacts poorly with PTY echo mode.
	if strings.Contains(command, "`") && (hasSingle || hasDouble) {
		return true
	}

	return false
}
