package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

// sshToolDefinitions 返回统一的 SSH 工具定义（单工具，action 分发）。
func sshToolDefinitions() []map[string]interface{} {
	return []map[string]interface{}{
		toolDef("ssh", "SSH 远程服务器管理（connect/exec/exec_background/check_task/list_tasks/kill_task/upload/download/list/close）", map[string]interface{}{
			"action":          map[string]interface{}{"type": "string", "description": "操作: connect/exec/exec_background/check_task/list_tasks/kill_task/upload/download/list/close"},
			"host":            map[string]interface{}{"type": "string", "description": "远程主机地址（connect 时必填）"},
			"user":            map[string]interface{}{"type": "string", "description": "登录用户名（connect 时必填）"},
			"port":            map[string]interface{}{"type": "integer", "description": "SSH 端口（默认 22）"},
			"auth_method":     map[string]interface{}{"type": "string", "description": "认证方式: key/password/agent（默认 key）"},
			"key_path":        map[string]interface{}{"type": "string", "description": "私钥路径（可选）"},
			"password":        map[string]interface{}{"type": "string", "description": "密码（可选）"},
			"label":           map[string]interface{}{"type": "string", "description": "主机标签（可选，如 prod-web-01）"},
			"initial_command": map[string]interface{}{"type": "string", "description": "连接后立即执行的命令（可选）"},
			"session_id":      map[string]interface{}{"type": "string", "description": "SSH 会话 ID（exec/upload/download/close 时必填）"},
			"command":         map[string]interface{}{"type": "string", "description": "要执行的命令（exec/exec_background 时必填）"},
			"wait_seconds":    map[string]interface{}{"type": "integer", "description": "等待输出秒数（exec 时可选，默认 5，最大 600）"},
			"task_id":         map[string]interface{}{"type": "string", "description": "后台任务 ID（check_task/kill_task 时必填）"},
			"tail_lines":      map[string]interface{}{"type": "integer", "description": "查看日志尾部行数（check_task 时可选，默认 50）"},
			"local_path":      map[string]interface{}{"type": "string", "description": "本地文件/目录路径（upload/download 时必填）"},
			"remote_path":     map[string]interface{}{"type": "string", "description": "远程文件/目录路径（upload/download 时必填）"},
		}, []string{"action"}),
	}
}

// toolSSH 统一 SSH 工具入口，按 action 分发。
func (h *TUIAgentHandler) toolSSH(args map[string]interface{}) string {
	action := stringArg(args, "action")
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

func (h *TUIAgentHandler) sshConnect(args map[string]interface{}) string {
	if h.sshMgr == nil {
		return "错误: SSH 会话管理器未初始化"
	}

	host := stringArg(args, "host")
	user := stringArg(args, "user")
	label := stringArg(args, "label")

	// 支持通过标签名引用预配置主机
	if (host == "" || user == "") && label != "" {
		if entry := h.resolveSSHHostByLabel(label); entry != nil {
			host = entry.Host
			user = entry.User
			if args["port"] == nil && entry.Port > 0 {
				args["port"] = float64(entry.Port)
			}
			if stringArg(args, "auth_method") == "" && entry.AuthMethod != "" {
				args["auth_method"] = entry.AuthMethod
			}
			if stringArg(args, "key_path") == "" && entry.KeyPath != "" {
				args["key_path"] = entry.KeyPath
			}
			label = entry.Label
		}
	}

	if host == "" || user == "" {
		return "错误: connect 需要 host 和 user 参数（或通过 label 引用已配置主机）"
	}

	cfg := remote.SSHHostConfig{
		Host:       host,
		User:       user,
		Port:       intArg(args, "port", 22),
		AuthMethod: stringArg(args, "auth_method"),
		KeyPath:    stringArg(args, "key_path"),
		Password:   stringArg(args, "password"),
		Label:      label,
	}

	spec := remote.SSHSessionSpec{
		HostConfig:     cfg,
		InitialCommand: stringArg(args, "initial_command"),
		Cols:           120,
		Rows:           40,
	}

	session, err := h.sshMgr.Create(spec)
	if err != nil {
		return fmt.Sprintf("SSH 连接失败: %v", err)
	}

	// 等 shell 初始化
	time.Sleep(2 * time.Second)

	preview := strings.Join(session.PreviewTail(20), "\n")

	result := fmt.Sprintf("✅ SSH 连接成功\n会话 ID: %s\n主机: %s\n状态: running",
		session.ID, cfg.SSHHostID())
	if preview != "" {
		result += "\n\n--- 初始输出 ---\n" + preview
	}
	return result
}

func (h *TUIAgentHandler) sshExec(args map[string]interface{}) string {
	if h.sshMgr == nil {
		return "错误: SSH 会话管理器未初始化"
	}

	sessionID := stringArg(args, "session_id")
	command := stringArg(args, "command")
	if sessionID == "" || command == "" {
		return "错误: exec 需要 session_id 和 command 参数"
	}

	// 自动升级：长时间命令且 wait_seconds 未显式设置大值时，自动转为后台模式
	waitSec := intArg(args, "wait_seconds", 5)
	if remote.IsLongRunningCommand(command) && waitSec <= 30 && h.bgTaskMgr != nil {
		return h.sshExecBackground(args)
	}

	session, ok := h.sshMgr.Get(sessionID)
	if !ok {
		return fmt.Sprintf("错误: SSH 会话 %s 不存在", sessionID)
	}

	reconnectNote := ""

	// 检查会话是否已断开，如果是则自动重连
	status, _ := h.sshMgr.GetSessionStatus(sessionID)
	sessionDead := status == remote.SessionExited || status == remote.SessionError

	if sessionDead {
		if err := h.sshMgr.ReconnectByID(sessionID); err != nil {
			return fmt.Sprintf("SSH 会话已断开，自动重连失败: %v", err)
		}
		reconnectNote = "⚠️ 连接已断开并自动重连\n"
		time.Sleep(2 * time.Second)
	}

	linesBefore := session.LineCount()

	if sessionDead {
		if err := h.sshMgr.WriteInput(sessionID, command); err != nil {
			return fmt.Sprintf("%s发送命令失败: %v", reconnectNote, err)
		}
	} else {
		reconnected, err := h.sshMgr.WriteInputChecked(sessionID, command)
		if err != nil {
			return fmt.Sprintf("发送命令失败: %v", err)
		}
		if reconnected {
			reconnectNote = "⚠️ 连接已断开并自动重连\n"
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

	newLines, status := h.sshMgr.WaitForOutput(sessionID, linesBefore, maxWait)

	output := strings.Join(newLines, "\n")
	if output == "" {
		output = "(无新输出)"
	}
	if len(output) > 8000 {
		output = output[:4000] + "\n... (截断) ...\n" + output[len(output)-4000:]
	}

	return fmt.Sprintf("%s[%s] 状态: %s\n$ %s\n%s", reconnectNote, sessionID, string(status), command, output)
}

// sshExecBackground 在远程服务器上以后台模式执行长时间命令。
// 命令通过 nohup 包装，输出重定向到日志文件，SSH 断连不影响执行。
func (h *TUIAgentHandler) sshExecBackground(args map[string]interface{}) string {
	if h.sshMgr == nil || h.bgTaskMgr == nil {
		return "错误: SSH 会话管理器未初始化"
	}

	sessionID := stringArg(args, "session_id")
	command := stringArg(args, "command")
	if sessionID == "" || command == "" {
		return "错误: exec_background 需要 session_id 和 command 参数"
	}

	// 检查会话是否存在且存活，必要时自动重连
	status, _ := h.sshMgr.GetSessionStatus(sessionID)
	if status == remote.SessionExited || status == remote.SessionError {
		if err := h.sshMgr.ReconnectByID(sessionID); err != nil {
			return fmt.Sprintf("SSH 会话已断开，自动重连失败: %v", err)
		}
		time.Sleep(2 * time.Second)
	}

	task, err := h.bgTaskMgr.Submit(sessionID, command)
	if err != nil {
		return fmt.Sprintf("提交后台任务失败: %v", err)
	}

	return fmt.Sprintf("✅ 后台任务已提交\n"+
		"任务 ID: %s\n"+
		"命令: %s\n"+
		"日志文件: %s\n"+
		"PID: %s\n"+
		"状态: running\n\n"+
		"💡 使用 check_task (task_id=%s) 查看进度\n"+
		"💡 使用 kill_task (task_id=%s) 终止任务\n"+
		"💡 SSH 断连不影响任务执行，重连后可继续查看",
		task.TaskID, task.Command, task.LogFile, task.PID, task.TaskID, task.TaskID)
}

// sshCheckTask 检查后台任务的状态和最新日志输出。
func (h *TUIAgentHandler) sshCheckTask(args map[string]interface{}) string {
	if h.bgTaskMgr == nil {
		return "错误: 后台任务管理器未初始化"
	}

	taskID := stringArg(args, "task_id")
	if taskID == "" {
		return "错误: check_task 需要 task_id 参数"
	}

	tailLines := intArg(args, "tail_lines", 50)
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

	return fmt.Sprintf("%s 任务 %s\n"+
		"命令: %s\n"+
		"状态: %s\n"+
		"进程存活: %v\n"+
		"已运行: %s\n"+
		"日志大小: %s bytes\n\n"+
		"--- 最新日志 ---\n%s",
		statusEmoji, result.TaskID, result.Command, result.Status,
		result.IsAlive, result.Elapsed, result.LogSize, logTail)
}

// sshListTasks 列出所有后台任务。
func (h *TUIAgentHandler) sshListTasks() string {
	if h.bgTaskMgr == nil {
		return "当前无后台任务（管理器未初始化）"
	}

	tasks := h.bgTaskMgr.ListTasks()
	if len(tasks) == 0 {
		return "当前无后台任务"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("后台任务（%d 个）:\n", len(tasks)))
	for _, t := range tasks {
		elapsed := time.Since(t.StartedAt).Round(time.Second)
		sb.WriteString(fmt.Sprintf("  - %s | %s | PID: %s | 状态: %s | 已运行: %s\n    命令: %s\n",
			t.TaskID, t.SessionID, t.PID, t.Status, elapsed, t.Command))
	}
	return sb.String()
}

// sshKillTask 终止后台任务。
func (h *TUIAgentHandler) sshKillTask(args map[string]interface{}) string {
	if h.bgTaskMgr == nil {
		return "错误: 后台任务管理器未初始化"
	}

	taskID := stringArg(args, "task_id")
	if taskID == "" {
		return "错误: kill_task 需要 task_id 参数"
	}

	if err := h.bgTaskMgr.KillTask(taskID); err != nil {
		return fmt.Sprintf("终止任务失败: %v", err)
	}
	return fmt.Sprintf("✅ 后台任务 %s 已终止", taskID)
}

// sshUpload 上传本地文件/目录到远程服务器。
func (h *TUIAgentHandler) sshUpload(args map[string]interface{}) string {
	if h.sshMgr == nil {
		return "错误: SSH 会话管理器未初始化"
	}
	sessionID := stringArg(args, "session_id")
	localPath := stringArg(args, "local_path")
	remotePath := stringArg(args, "remote_path")
	if sessionID == "" || localPath == "" || remotePath == "" {
		return "错误: upload 需要 session_id、local_path 和 remote_path 参数"
	}
	result, err := h.sshMgr.SFTPTransfer(sessionID, "upload", localPath, remotePath)
	if err != nil {
		return fmt.Sprintf("上传失败: %v", err)
	}
	return fmt.Sprintf("✅ 上传完成: %s → %s\n%s", localPath, remotePath, result)
}

// sshDownload 从远程服务器下载文件/目录到本地。
func (h *TUIAgentHandler) sshDownload(args map[string]interface{}) string {
	if h.sshMgr == nil {
		return "错误: SSH 会话管理器未初始化"
	}
	sessionID := stringArg(args, "session_id")
	localPath := stringArg(args, "local_path")
	remotePath := stringArg(args, "remote_path")
	if sessionID == "" || localPath == "" || remotePath == "" {
		return "错误: download 需要 session_id、local_path 和 remote_path 参数"
	}
	result, err := h.sshMgr.SFTPTransfer(sessionID, "download", localPath, remotePath)
	if err != nil {
		return fmt.Sprintf("下载失败: %v", err)
	}
	return fmt.Sprintf("✅ 下载完成: %s → %s\n%s", remotePath, localPath, result)
}

func (h *TUIAgentHandler) sshList() string {
	if h.sshMgr == nil {
		return "当前无活跃 SSH 会话（管理器未初始化）"
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

func (h *TUIAgentHandler) sshClose(args map[string]interface{}) string {
	if h.sshMgr == nil {
		return "错误: SSH 会话管理器未初始化"
	}

	sessionID := stringArg(args, "session_id")
	if sessionID == "" {
		return "错误: close 需要 session_id 参数"
	}

	if err := h.sshMgr.Kill(sessionID); err != nil {
		return fmt.Sprintf("关闭失败: %v", err)
	}
	return fmt.Sprintf("✅ SSH 会话 %s 已关闭", sessionID)
}

// sshSessionsToJSON 序列化 SSH 会话列表（供其他模块使用）。
func sshSessionsToJSON(sessions []*remote.SSHManagedSession) string {
	summaries := make([]remote.SSHSessionSummary, 0, len(sessions))
	for _, s := range sessions {
		summaries = append(summaries, s.GetSummary())
	}
	data, _ := json.Marshal(summaries)
	return string(data)
}

// loadSSHHosts 从配置中加载预配置的 SSH 主机列表。
func (h *TUIAgentHandler) loadSSHHosts() []corelib.SSHHostEntry {
	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return nil
	}
	return cfg.SSHHosts
}

// resolveSSHHostByLabel 根据标签名查找预配置的 SSH 主机。
func (h *TUIAgentHandler) resolveSSHHostByLabel(label string) *corelib.SSHHostEntry {
	hosts := h.loadSSHHosts()
	label = strings.ToLower(strings.TrimSpace(label))
	for i := range hosts {
		if strings.ToLower(hosts[i].Label) == label {
			return &hosts[i]
		}
	}
	// 模糊匹配：标签包含关键词
	for i := range hosts {
		if strings.Contains(strings.ToLower(hosts[i].Label), label) {
			return &hosts[i]
		}
	}
	return nil
}
