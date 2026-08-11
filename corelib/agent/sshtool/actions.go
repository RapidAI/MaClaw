package sshtool

import (
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// SSHConnect establishes a new SSH connection or reuses an existing one.
func SSHConnect(deps SSHToolDeps, args map[string]interface{}) string {
	mgr := deps.Manager
	if mgr == nil {
		return "错误: SSH 会话管理器未初始化"
	}

	host := strArg(args, "host")
	user := strArg(args, "user")
	label := strArg(args, "label")

	// Resolve pre-configured host by label.
	if (host == "" || user == "") && label != "" {
		var hosts []corelib.SSHHostEntry
		if deps.HostLoader != nil {
			hosts = deps.HostLoader()
		}
		if entry := ResolveSSHHostByLabel(hosts, label); entry != nil {
			host = entry.Host
			user = entry.User
			if args["port"] == nil && entry.Port > 0 {
				args["port"] = float64(entry.Port)
			}
			if strArg(args, "auth_method") == "" && entry.AuthMethod != "" {
				args["auth_method"] = entry.AuthMethod
			}
			if strArg(args, "key_path") == "" && entry.KeyPath != "" {
				args["key_path"] = entry.KeyPath
			}
			// Secrets stay server-side on the configured entry (never required in tool args).
			if strArg(args, "password") == "" && entry.Password != "" {
				args["password"] = entry.Password
			}
			if strArg(args, "passphrase") == "" && entry.Passphrase != "" {
				args["passphrase"] = entry.Passphrase
			}
			if strArg(args, "host_key_fingerprint") == "" && entry.HostKeyFingerprint != "" {
				args["host_key_fingerprint"] = entry.HostKeyFingerprint
			}
			label = entry.Label
		}
	}

	if host == "" || user == "" {
		return "错误: connect 需要 host 和 user 参数（或通过 label 引用已配置主机）"
	}

	port := intArg(args, "port", 22)

	// Check if a running session already exists for this host.
	forceNew, _ := args["force_new"].(bool)
	if !forceNew {
		targetID := fmt.Sprintf("%s@%s:%d", user, host, port)
		if existing := FindRunningSSHSession(mgr, targetID, label); existing != nil {
			// Verify the connection is actually alive before reusing.
			if existing.Handle != nil && existing.Handle.IsAlive() {
				if mgr.CheckShellResponsive(existing.ID) {
					summary := existing.GetSummary()
					result := fmt.Sprintf("复用已有 SSH 会话（无需重连）\n会话 ID: %s\n主机: %s\n状态: %s\n\n"+
						"请直接 exec，session_id=%s。不要 force_new / 再次 connect。",
						existing.ID, summary.HostID, summary.Status, existing.ID)
					if summary.LastOutput != "" {
						result += "\n\n最近输出: " + summary.LastOutput
					}
					return result
				}
				// Shell is unresponsive. Close and recreate.
				mgr.RemoveSession(existing.ID)
				if deps.OnClosed != nil {
					deps.OnClosed(existing.ID)
				}
			} else {
				// Connection is dead. Try to reconnect.
				if err := mgr.ReconnectByID(existing.ID); err == nil {
					time.Sleep(2 * time.Second)
					preview := strings.Join(existing.PreviewTail(10), "\n")
					result := fmt.Sprintf("复用已有 SSH 会话（已自动重连）\n会话 ID: %s\n主机: %s\n状态: %s",
						existing.ID, targetID, runningSessionStatusLabel())
					if preview != "" {
						result += "\n\n--- 重连后输出 ---\n" + preview
					}
					return result
				}
				mgr.RemoveSession(existing.ID)
				if deps.OnClosed != nil {
					deps.OnClosed(existing.ID)
				}
			}
		}
	}

	cfg := remote.SSHHostConfig{
		Host:               host,
		User:               user,
		Port:               port,
		AuthMethod:         strArg(args, "auth_method"),
		KeyPath:            strArg(args, "key_path"),
		Password:           strArg(args, "password"),
		Passphrase:         strArg(args, "passphrase"),
		Label:              label,
		HostKeyFingerprint: strArg(args, "host_key_fingerprint"),
	}

	spec := remote.SSHSessionSpec{
		HostConfig:     cfg,
		InitialCommand: strArg(args, "initial_command"),
		Cols:           120,
		Rows:           40,
	}

	session, err := mgr.Create(spec)
	if err != nil {
		errMsg := fmt.Sprintf("SSH 连接失败: %v", err)
		if classifySSHError(err) == sshErrorAuthentication {
			if cfg.Password == "" {
				errMsg += "\n\n认证失败且未提供密码。请使用 password 参数重试，例如：\nssh connect host=... user=... port=... password=<密码> auth_method=password"
			} else {
				errMsg += "\n\n已提供密码但认证仍失败，请检查密码是否正确"
			}
		}
		return errMsg
	}

	if deps.OnConnected != nil {
		deps.OnConnected(session, cfg)
	}

	// Wait for shell init.
	time.Sleep(2 * time.Second)

	preview := strings.Join(session.PreviewTail(20), "\n")

	result := fmt.Sprintf("SSH 连接成功\n会话 ID: %s\n主机: %s\n状态: %s\n\n"+
		"会话管理（与桌面 GUI 相同内核）:\n"+
		"- 后续命令请用 exec，session_id=%s（不要再次 connect）\n"+
		"- 下方「初始输出」仅为 shell 横幅/预览（最多约 20 行），不是命令完整结果；不完整也禁止重连\n"+
		"- 查状态示例: ssh(action=exec, session_id=%s, command=\"uptime; free -h; df -h /\" , wait_seconds=15)\n"+
		"- 需要多个检查时复用同一 session_id；结束后 close",
		session.ID, cfg.SSHHostID(), runningSessionStatusLabel(), session.ID, session.ID)
	if preview != "" {
		result += "\n\n--- 初始输出（预览，可忽略不全）---\n" + preview
	}
	return result
}

// SSHExec executes a command on an existing SSH session.
func SSHExec(deps SSHToolDeps, args map[string]interface{}) string {
	mgr := deps.Manager
	if mgr == nil {
		return "错误: SSH 会话管理器未初始化"
	}

	sessionID := strArg(args, "session_id")
	command := strArg(args, "command")
	if sessionID == "" || command == "" {
		return "错误: exec 需要 session_id 和 command 参数"
	}

	waitSec := intArg(args, "wait_seconds", 5)
	if remote.IsLongRunningCommand(command) && waitSec <= 30 && deps.BGTaskMgr != nil {
		return SSHExecBackground(deps, args)
	}

	session, ok := mgr.Get(sessionID)
	if !ok {
		return fmt.Sprintf("错误: SSH 会话 %s 不存在", sessionID)
	}

	reconnectNote := ""

	status, _ := mgr.GetSessionStatus(sessionID)
	sessionDead := status == remote.SessionExited || status == remote.SessionError

	if sessionDead {
		if err := mgr.ReconnectByID(sessionID); err != nil {
			return fmt.Sprintf("SSH 会话已断开，自动重连失败: %v\n\n建议使用 ssh(action=close, session_id=%s) 关闭此会话，然后重新 connect", err, sessionID)
		}
		reconnectNote = "连接已断开并自动重连\n"
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
			reconnectNote = "连接已断开并自动重连\n"
			time.Sleep(2 * time.Second)
			linesBefore = session.LineCount()
		}
	}

	if waitSec <= 0 {
		waitSec = 5
	}
	if waitSec > 600 {
		waitSec = 600
	}
	maxWait := time.Duration(waitSec) * time.Second

	newLines, status := mgr.WaitForOutput(sessionID, linesBefore, maxWait)
	output := strings.Join(newLines, "\n")

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
		if failCount >= 3 {
			responsive := mgr.CheckShellResponsive(sessionID)
			if !responsive {
				mgr.RemoveSession(sessionID)
				if deps.OnClosed != nil {
					deps.OnClosed(sessionID)
				}
				return fmt.Sprintf("SSH 会话 %s 连续 %d 次执行无响应，shell 可能被挂起的进程锁住。\n"+
					"已自动关闭此会话。请使用 ssh(action=connect, ...) 重新建立连接。\n\n"+
					"如果远程服务器上有挂起的进程（如 sqlite3），重连后可用 `kill` 命令清理",
					sessionID, failCount)
			}
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

	if deps.OnExecIteration != nil {
		deps.OnExecIteration(sessionID)
	}

	return fmt.Sprintf("%s[%s] 状态: %s\n$ %s\n%s", reconnectNote, sessionID, string(status), command, output)
}

// SSHExecBackground runs a long-running command in the background via nohup.
func SSHExecBackground(deps SSHToolDeps, args map[string]interface{}) string {
	mgr := deps.Manager
	bgMgr := deps.BGTaskMgr
	if mgr == nil || bgMgr == nil {
		return "错误: SSH 会话管理器未初始化"
	}

	sessionID := strArg(args, "session_id")
	command := strArg(args, "command")
	if sessionID == "" || command == "" {
		return "错误: exec_background 需要 session_id 和 command 参数"
	}

	status, _ := mgr.GetSessionStatus(sessionID)
	if status == remote.SessionExited || status == remote.SessionError {
		if err := mgr.ReconnectByID(sessionID); err != nil {
			return fmt.Sprintf("SSH 会话已断开，自动重连失败: %v", err)
		}
		time.Sleep(2 * time.Second)
	}

	if err := requireBackgroundTaskPolicyOwner(deps); err != nil {
		return fmt.Sprintf("ssh exec_background failed: %v", err)
	}

	task, err := bgMgr.SubmitWithOwner(sessionID, command, strArg(args, "task_role"), deps.PolicyOwnerID)
	if err != nil {
		return fmt.Sprintf("提交后台任务失败: %v", err)
	}

	if deps.OnExecIteration != nil {
		deps.OnExecIteration(sessionID)
	}

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
		"状态: "+runningSessionStatusLabel()+"\n\n"+
		"使用 check_task (task_id=%s) 查看进度\n"+
		"使用 kill_task (task_id=%s) 终止任务\n"+
		"SSH 断连不影响任务执行，重连后可继续查看",
		task.TaskID, task.Command, task.LogFile, task.PID, task.TaskID, task.TaskID)
}

func requireBackgroundTaskPolicyOwner(deps SSHToolDeps) error {
	if strings.TrimSpace(deps.PolicyOwnerID) == "" {
		return fmt.Errorf("runtime owner is missing; background task access is isolated")
	}
	return nil
}

// SSHCheckTask checks the status and latest log output of a background task.
func SSHCheckTask(deps SSHToolDeps, args map[string]interface{}) string {
	if deps.BGTaskMgr == nil {
		return "错误: 无后台任务"
	}

	if err := requireBackgroundTaskPolicyOwner(deps); err != nil {
		return fmt.Sprintf("check_task failed: %v", err)
	}

	taskID := strArg(args, "task_id")
	if taskID == "" {
		return "错误: check_task 需要 task_id 参数"
	}

	tailLines := intArg(args, "tail_lines", 50)

	result, err := deps.BGTaskMgr.CheckTaskForOwner(taskID, tailLines, deps.PolicyOwnerID)
	if err != nil {
		return fmt.Sprintf("检查任务失败: %v", err)
	}

	statusEmoji := backgroundTaskStatusIcon(result.Status)

	logTail := result.LogTail
	if logTail == "" {
		logTail = "(无日志输出)"
	}
	if len(logTail) > 6000 {
		logTail = logTail[:3000] + "\n... (截断) ...\n" + logTail[len(logTail)-3000:]
	}

	return fmt.Sprintf("%s 任务 %s\n命令: %s\n状态: %s\n进程存活: %v\n已运行: %s\n\n--- 最新日志 ---\n%s",
		statusEmoji, result.TaskID, result.Command, result.Status.String(),
		result.IsAlive, result.Elapsed, logTail)
}

// SSHListTasks lists all background tasks.
func SSHListTasks(deps SSHToolDeps) string {
	if deps.BGTaskMgr == nil {
		return "当前无后台任务"
	}

	if err := requireBackgroundTaskPolicyOwner(deps); err != nil {
		return fmt.Sprintf("list_tasks failed: %v", err)
	}

	tasks := deps.BGTaskMgr.ListTasksForOwner(deps.PolicyOwnerID)
	if len(tasks) == 0 {
		return "当前无后台任务"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("后台任务（%d 个）:\n", len(tasks)))
	for _, t := range tasks {
		elapsed := time.Since(t.StartedAt).Round(time.Second)
		statusStr := string(t.Status)
		// 对于从磁盘恢复且超过 2 分钟未验证的 running 任务，标注状态待验证
		if t.Status.IsActive() && !t.LastCheck.IsZero() && time.Since(t.LastCheck) > 2*time.Minute {
			statusStr += " (状态待验证，请用 check_task 确认)"
		}
		sb.WriteString(fmt.Sprintf("  - %s | PID: %s | 状态: %s | 已运行: %s\n    命令: %s\n",
			t.TaskID, t.PID, statusStr, elapsed, t.Command))
	}
	return sb.String()
}

// SSHKillTask terminates a background task.
func SSHKillTask(deps SSHToolDeps, args map[string]interface{}) string {
	if deps.BGTaskMgr == nil {
		return "错误: 无后台任务"
	}

	if err := requireBackgroundTaskPolicyOwner(deps); err != nil {
		return fmt.Sprintf("kill_task failed: %v", err)
	}

	taskID := strArg(args, "task_id")
	if taskID == "" {
		return "错误: kill_task 需要 task_id 参数"
	}

	if err := deps.BGTaskMgr.KillTaskForOwner(taskID, deps.PolicyOwnerID); err != nil {
		return fmt.Sprintf("终止任务失败: %v", err)
	}
	return fmt.Sprintf("后台任务 %s 已终止", taskID)
}

// SSHSudoPrepare pre-acquires a sudo token via PTY interaction.
func SSHSudoPrepare(deps SSHToolDeps, args map[string]interface{}) string {
	if deps.BGTaskMgr == nil {
		return "错误: 后台任务管理器未初始化"
	}

	sessionID := strArg(args, "session_id")
	if sessionID == "" {
		return "错误: sudo_prepare 需要 session_id 参数"
	}

	ok, msg := deps.BGTaskMgr.EnsureSudoToken(sessionID)
	if ok {
		return fmt.Sprintf("%s", msg)
	}
	return fmt.Sprintf("%s", msg)
}

// SSHUpload uploads a local file/directory to the remote server via SFTP.
func SSHUpload(deps SSHToolDeps, args map[string]interface{}) string {
	mgr := deps.Manager
	if mgr == nil {
		return "错误: SSH 会话管理器未初始化"
	}
	sessionID := strArg(args, "session_id")
	localPath := strArg(args, "local_path")
	remotePath := strArg(args, "remote_path")
	if sessionID == "" || localPath == "" || remotePath == "" {
		return "错误: upload 需要 session_id、local_path 和 remote_path 参数"
	}
	result, err := mgr.SFTPTransfer(sessionID, "upload", localPath, remotePath)
	if err != nil {
		return fmt.Sprintf("上传失败: %v", err)
	}
	return fmt.Sprintf("上传完成: %s → %s\n%s", localPath, remotePath, result)
}

// SSHDownload downloads a remote file/directory to local via SFTP.
func SSHDownload(deps SSHToolDeps, args map[string]interface{}) string {
	mgr := deps.Manager
	if mgr == nil {
		return "错误: SSH 会话管理器未初始化"
	}
	sessionID := strArg(args, "session_id")
	localPath := strArg(args, "local_path")
	remotePath := strArg(args, "remote_path")
	if sessionID == "" || localPath == "" || remotePath == "" {
		return "错误: download 需要 session_id、local_path 和 remote_path 参数"
	}
	result, err := mgr.SFTPTransfer(sessionID, "download", localPath, remotePath)
	if err != nil {
		return fmt.Sprintf("下载失败: %v", err)
	}
	return fmt.Sprintf("下载完成: %s → %s\n%s", remotePath, localPath, result)
}

// SSHList lists all active SSH sessions.
func SSHList(deps SSHToolDeps) string {
	mgr := deps.Manager
	if mgr == nil {
		return "当前无活跃 SSH 会话"
	}

	sessions := mgr.List()
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

	poolStats := mgr.Pool().Stats()
	if len(poolStats) > 0 {
		sb.WriteString("连接池:\n")
		for hostID, ref := range poolStats {
			sb.WriteString(fmt.Sprintf("  - %s (引用: %d)\n", hostID, ref))
		}
	}
	return sb.String()
}

// SSHClose closes a single SSH session.
func SSHClose(deps SSHToolDeps, args map[string]interface{}) string {
	mgr := deps.Manager
	if mgr == nil {
		return "错误: SSH 会话管理器未初始化"
	}

	sessionID := strArg(args, "session_id")
	if sessionID == "" {
		return "错误: close 需要 session_id 参数"
	}

	_ = mgr.Kill(sessionID)
	mgr.RemoveSession(sessionID)

	if deps.OnClosed != nil {
		deps.OnClosed(sessionID)
	}

	return fmt.Sprintf("SSH 会话 %s 已关闭", sessionID)
}

// SSHCloseAll closes all running SSH sessions.
func SSHCloseAll(deps SSHToolDeps) string {
	mgr := deps.Manager
	if mgr == nil {
		return "当前无 SSH 会话"
	}
	sessions := mgr.List()
	running := make([]*remote.SSHManagedSession, 0, len(sessions))
	for _, s := range sessions {
		summary := s.GetSummary()
		if isRunningSessionStatus(summary.Status) {
			running = append(running, s)
		}
	}
	if len(running) == 0 {
		return "当前无运行中的 SSH 会话"
	}
	for _, s := range running {
		_ = mgr.Kill(s.ID)
		mgr.RemoveSession(s.ID)
		if deps.OnClosed != nil {
			deps.OnClosed(s.ID)
		}
	}
	return fmt.Sprintf("已关闭 %d 个 SSH 会话", len(running))
}
