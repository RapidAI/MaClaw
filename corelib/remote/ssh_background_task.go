package remote

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// SSHBackgroundTask 表示一个在远程服务器上通过 nohup 运行的后台任务。
type SSHBackgroundTask struct {
	mu        sync.Mutex
	TaskID    string    `json:"task_id"`
	SessionID string    `json:"session_id"`
	Command   string    `json:"command"`
	LogFile   string    `json:"log_file"`
	PIDFile   string    `json:"pid_file"`
	Status    string    `json:"status"` // pending, running, completed, failed, unknown
	PID       string    `json:"pid,omitempty"`
	StartedAt time.Time `json:"started_at"`
	LastCheck time.Time `json:"last_check,omitempty"`
}

// SSHBackgroundTaskManager 管理远程后台任务的生命周期。
// 长时间运行的命令（pip install、apt、make 等）通过 nohup + 日志文件执行，
// 避免 SSH 断连导致任务丢失。
type SSHBackgroundTaskManager struct {
	mu      sync.RWMutex
	tasks   map[string]*SSHBackgroundTask
	sshMgr  *SSHSessionManager
	counter int
}

// NewSSHBackgroundTaskManager 创建后台任务管理器。
func NewSSHBackgroundTaskManager(sshMgr *SSHSessionManager) *SSHBackgroundTaskManager {
	return &SSHBackgroundTaskManager{
		tasks:  make(map[string]*SSHBackgroundTask),
		sshMgr: sshMgr,
	}
}

// Submit 提交一个后台任务。
// 命令会被包装为: nohup bash -c '<command>' > <logfile> 2>&1 & echo $! > <pidfile>
//
// sudo 处理：后台任务无 TTY，无法交互式输入密码。Submit 会：
// 1. 检测命令中是否包含 sudo
// 2. 用 sudo -n true 测试是否有免密权限
// 3. 有免密 → 正常执行
// 4. 无免密 → 尝试自动降级为非 sudo 替代命令，并在日志中记录原因
func (m *SSHBackgroundTaskManager) Submit(sessionID, command string) (*SSHBackgroundTask, error) {
	session, ok := m.sshMgr.Get(sessionID)
	if !ok {
		return nil, fmt.Errorf("ssh session %s not found", sessionID)
	}

	// sudo 检测与降级
	if containsSudo(command) {
		hasNopasswd, err := m.checkSudoNopasswd(sessionID)
		if err != nil {
			// 检测失败，不阻塞，按无免密处理
			hasNopasswd = false
		}
		if !hasNopasswd {
			// 方案 3：尝试通过 PTY 交互获取 sudo token
			tokenOK, tokenMsg := m.EnsureSudoToken(sessionID)
			if tokenOK {
				// sudo token 获取成功，正常执行原始命令
				// 不需要降级
			} else {
				// 方案 2：sudo token 获取失败，降级为非 sudo 替代
				fallback, hint := sudoFallback(command)
				if fallback != command {
					command = fallback
					command = fmt.Sprintf("echo '[maclaw] %s; sudo token 获取失败 (%s)，已自动降级' && %s",
						strings.ReplaceAll(hint, "'", "'\\''"),
						strings.ReplaceAll(tokenMsg, "'", "'\\''"),
						command)
				} else {
					return nil, fmt.Errorf("命令需要 sudo 权限，但无法获取: %s\n提示: %s", tokenMsg, hint)
				}
			}
		}
	}

	m.mu.Lock()
	m.counter++
	taskID := fmt.Sprintf("bg_%d_%d", time.Now().Unix(), m.counter)
	m.mu.Unlock()

	logFile := fmt.Sprintf("/tmp/maclaw_bg_%s.log", taskID)
	pidFile := fmt.Sprintf("/tmp/maclaw_bg_%s.pid", taskID)

	// 构建后台执行命令：
	// 写一个临时脚本到远程，然后 nohup 执行它。
	// 这样避免了嵌套 bash -c 的引号地狱问题。
	scriptFile := fmt.Sprintf("/tmp/maclaw_bg_%s.sh", taskID)
	scriptContent := fmt.Sprintf(
		"#!/bin/bash\necho '=== maclaw bg task %s ==='\necho 'CMD: %s'\necho \"START: $(date)\"\necho '---'\n%s\nRET=$?\necho '---'\necho \"EXIT: $RET\"\nexit $RET\n",
		taskID, strings.ReplaceAll(command, "'", "'\\''"), command,
	)

	// 先写脚本文件
	writeScript := fmt.Sprintf("cat > %s << 'MACLAW_SCRIPT_EOF'\n%sMACLAW_SCRIPT_EOF\nchmod +x %s", scriptFile, scriptContent, scriptFile)
	if _, err := m.sshMgr.WriteInputChecked(sessionID, writeScript); err != nil {
		return nil, fmt.Errorf("write script: %w", err)
	}
	time.Sleep(500 * time.Millisecond)

	// 用 nohup 后台执行脚本
	wrappedCmd := fmt.Sprintf(
		`nohup bash %s > %s 2>&1 & echo $! > %s && sleep 0.5 && cat %s`,
		scriptFile, logFile, pidFile, pidFile,
	)

	linesBefore := session.LineCount()

	// 写入命令（使用带健康检查的写入，支持自动重连）
	if _, err := m.sshMgr.WriteInputChecked(sessionID, wrappedCmd); err != nil {
		return nil, fmt.Errorf("submit background task: %w", err)
	}

	// 短暂等待获取 PID
	time.Sleep(2 * time.Second)
	newLines, _ := session.NewLinesSince(linesBefore)
	pid := extractPID(newLines)

	task := &SSHBackgroundTask{
		TaskID:    taskID,
		SessionID: sessionID,
		Command:   command,
		LogFile:   logFile,
		PIDFile:   pidFile,
		Status:    "running",
		PID:       pid,
		StartedAt: time.Now(),
	}

	m.mu.Lock()
	m.tasks[taskID] = task
	m.mu.Unlock()

	return task, nil
}

// CheckTask 检查后台任务的状态和最新输出。
// 通过 SSH 会话执行 tail 和 ps 命令来获取信息。
func (m *SSHBackgroundTaskManager) CheckTask(taskID string, tailLines int) (*BackgroundTaskStatus, error) {
	m.mu.RLock()
	task, ok := m.tasks[taskID]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("background task %s not found", taskID)
	}

	session, ok := m.sshMgr.Get(task.SessionID)
	if !ok {
		return nil, fmt.Errorf("ssh session %s not found", task.SessionID)
	}

	if tailLines <= 0 {
		tailLines = 50
	}
	if tailLines > 200 {
		tailLines = 200
	}

	// 构建检查命令：同时获取进程状态和日志尾部
	// 如果 PID 为空（提交时未能获取），跳过进程检查，只看日志
	var checkCmd string
	if task.PID != "" {
		checkCmd = fmt.Sprintf(
			`echo "===MACLAW_CHECK_START===" && `+
				`echo "PID_ALIVE:" && { kill -0 %s 2>/dev/null && echo "YES" || echo "NO"; } && `+
				`echo "LOG_LINES:" && tail -n %d %s 2>/dev/null && `+
				`echo "LOG_SIZE:" && wc -c < %s 2>/dev/null && `+
				`echo "===MACLAW_CHECK_END==="`,
			task.PID, tailLines, task.LogFile, task.LogFile,
		)
	} else {
		// 无 PID，尝试从 pidfile 读取
		checkCmd = fmt.Sprintf(
			`echo "===MACLAW_CHECK_START===" && `+
				`PID=$(cat %s 2>/dev/null) && echo "PID_ALIVE:" && { kill -0 $PID 2>/dev/null && echo "YES" || echo "NO"; } && `+
				`echo "LOG_LINES:" && tail -n %d %s 2>/dev/null && `+
				`echo "LOG_SIZE:" && wc -c < %s 2>/dev/null && `+
				`echo "===MACLAW_CHECK_END==="`,
			task.PIDFile, tailLines, task.LogFile, task.LogFile,
		)
	}

	linesBefore := session.LineCount()
	if _, err := m.sshMgr.WriteInputChecked(task.SessionID, checkCmd); err != nil {
		return nil, fmt.Errorf("check task: %w", err)
	}

	// 等待输出
	newLines, _ := m.sshMgr.WaitForOutput(task.SessionID, linesBefore, 10*time.Second)

	result := parseCheckOutput(newLines)
	result.TaskID = taskID
	result.Command = task.Command
	result.StartedAt = task.StartedAt
	result.Elapsed = time.Since(task.StartedAt).Round(time.Second).String()

	// 更新任务状态
	task.mu.Lock()
	task.LastCheck = time.Now()
	if result.IsAlive {
		task.Status = "running"
	} else {
		// 检查日志中是否有 EXIT 标记
		if strings.Contains(result.LogTail, "EXIT: 0") {
			task.Status = "completed"
		} else if strings.Contains(result.LogTail, "EXIT:") {
			task.Status = "failed"
		} else {
			task.Status = "unknown"
		}
	}
	result.Status = task.Status
	task.mu.Unlock()

	return result, nil
}

// ListTasks 列出所有后台任务。
func (m *SSHBackgroundTaskManager) ListTasks() []*SSHBackgroundTask {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*SSHBackgroundTask, 0, len(m.tasks))
	for _, t := range m.tasks {
		result = append(result, t)
	}
	return result
}

// KillTask 终止后台任务。
func (m *SSHBackgroundTaskManager) KillTask(taskID string) error {
	m.mu.RLock()
	task, ok := m.tasks[taskID]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("background task %s not found", taskID)
	}

	if task.PID == "" {
		return fmt.Errorf("task %s has no PID", taskID)
	}

	// kill 进程组（负 PID）以确保子进程也被终止
	killCmd := fmt.Sprintf("kill -- -%s 2>/dev/null; kill %s 2>/dev/null; kill -9 %s 2>/dev/null", task.PID, task.PID, task.PID)
	if _, err := m.sshMgr.WriteInputChecked(task.SessionID, killCmd); err != nil {
		return fmt.Errorf("kill task: %w", err)
	}

	task.mu.Lock()
	task.Status = "killed"
	task.mu.Unlock()

	return nil
}

// BackgroundTaskStatus 是 CheckTask 的返回结果。
type BackgroundTaskStatus struct {
	TaskID    string    `json:"task_id"`
	Command   string    `json:"command"`
	Status    string    `json:"status"`
	IsAlive   bool      `json:"is_alive"`
	LogTail   string    `json:"log_tail"`
	LogSize   string    `json:"log_size"`
	StartedAt time.Time `json:"started_at"`
	Elapsed   string    `json:"elapsed"`
}

// --- 辅助函数 ---

// extractPID 从命令输出中提取 PID（echo $! 的输出）。
func extractPID(lines []string) string {
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// PID 是纯数字
		allDigit := true
		for _, c := range line {
			if c < '0' || c > '9' {
				allDigit = false
				break
			}
		}
		if allDigit && len(line) > 0 && len(line) < 10 {
			return line
		}
	}
	return ""
}

// parseCheckOutput 解析 CheckTask 的输出。
func parseCheckOutput(lines []string) *BackgroundTaskStatus {
	result := &BackgroundTaskStatus{}
	section := "" // "", "pid", "log", "size"
	var logLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "===MACLAW_CHECK_START===" || trimmed == "===MACLAW_CHECK_END===" {
			section = ""
			continue
		}
		if trimmed == "PID_ALIVE:" {
			section = "pid"
			continue
		}
		if trimmed == "LOG_LINES:" {
			section = "log"
			continue
		}
		if trimmed == "LOG_SIZE:" {
			section = "size"
			continue
		}

		switch section {
		case "pid":
			if trimmed == "YES" {
				result.IsAlive = true
			} else if trimmed == "NO" {
				result.IsAlive = false
			}
			section = "" // PID 状态只有一行
		case "log":
			logLines = append(logLines, line)
		case "size":
			result.LogSize = strings.TrimSpace(trimmed)
			section = ""
		}
	}

	result.LogTail = strings.Join(logLines, "\n")
	return result
}

// --- sudo 检测与降级 ---

// sudoRe 匹配命令中的 sudo 调用（独立单词，不匹配 visudo 等）
var sudoRe = regexp.MustCompile(`(?:^|[;&|]\s*)sudo\s`)

// containsSudo 检测命令中是否包含 sudo 调用。
func containsSudo(cmd string) bool {
	return sudoRe.MatchString(cmd)
}

// checkSudoNopasswd 通过 SSH 会话测试当前用户是否有免密 sudo 权限。
// 使用 sudo -n true（non-interactive），成功返回 true。
func (m *SSHBackgroundTaskManager) checkSudoNopasswd(sessionID string) (bool, error) {
	session, ok := m.sshMgr.Get(sessionID)
	if !ok {
		return false, fmt.Errorf("session %s not found", sessionID)
	}

	linesBefore := session.LineCount()
	testCmd := `sudo -n true 2>/dev/null && echo "MACLAW_SUDO_OK" || echo "MACLAW_SUDO_FAIL"`
	if _, err := m.sshMgr.WriteInputChecked(sessionID, testCmd); err != nil {
		return false, err
	}

	newLines, _ := m.sshMgr.WaitForOutput(sessionID, linesBefore, 5*time.Second)
	for _, line := range newLines {
		if strings.Contains(line, "MACLAW_SUDO_OK") {
			return true, nil
		}
		if strings.Contains(line, "MACLAW_SUDO_FAIL") {
			return false, nil
		}
	}
	return false, nil
}

// EnsureSudoToken 在交互式 SSH 会话中预先获取 sudo token。
// 通过向 PTY stdin 写入密码来完成 sudo 认证，之后在 token 有效期内
// （默认 15 分钟）提交的后台任务可以直接使用 sudo 而无需再次输入密码。
//
// 流程：
// 1. 先用 sudo -n true 检测是否已有 token 或 NOPASSWD
// 2. 如果没有，执行 sudo -v 触发密码提示
// 3. 检测到密码提示后，通过 PTY stdin 写入密码
// 4. 验证 sudo token 是否获取成功
//
// 返回值：
// - ok: sudo token 是否可用
// - msg: 人类可读的状态信息
func (m *SSHBackgroundTaskManager) EnsureSudoToken(sessionID string) (ok bool, msg string) {
	session, ok2 := m.sshMgr.Get(sessionID)
	if !ok2 {
		return false, "SSH 会话不存在"
	}

	// 1. 先检查是否已有 token 或 NOPASSWD
	hasToken, _ := m.checkSudoNopasswd(sessionID)
	if hasToken {
		return true, "sudo 已就绪（NOPASSWD 或已有 token）"
	}

	// 2. 获取密码——从 session 的 HostConfig 中读取
	password := session.Spec.HostConfig.Password
	if password == "" {
		return false, "无法获取 sudo token：SSH 连接使用密钥认证，未存储密码。请配置 NOPASSWD 或使用密码认证"
	}

	// 3. 执行 sudo -v 触发密码提示
	linesBefore := session.LineCount()
	if _, err := m.sshMgr.WriteInputChecked(sessionID, "sudo -v"); err != nil {
		return false, fmt.Sprintf("执行 sudo -v 失败: %v", err)
	}

	// 4. 等待密码提示（[sudo] password for xxx: 或 Password:）
	deadline := time.Now().Add(8 * time.Second)
	promptDetected := false
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		newLines, _ := session.NewLinesSince(linesBefore)
		for _, line := range newLines {
			lower := strings.ToLower(line)
			if strings.Contains(lower, "password") && (strings.Contains(lower, ":") || strings.Contains(lower, "：")) {
				promptDetected = true
				break
			}
		}
		if promptDetected {
			break
		}
	}

	if !promptDetected {
		// 可能 sudo 不需要密码（已有 token），或者提示格式不同
		// 再检查一次
		hasToken2, _ := m.checkSudoNopasswd(sessionID)
		if hasToken2 {
			return true, "sudo 已就绪"
		}
		return false, "未检测到 sudo 密码提示，可能 sudo 配置异常"
	}

	// 5. 写入密码（不带 echo，直接写入 PTY stdin）
	if err := session.Handle.Write([]byte(password + "\n")); err != nil {
		return false, fmt.Sprintf("写入密码失败: %v", err)
	}

	// 6. 等待 sudo -v 完成，同时检测密码错误
	// 如果密码错误，sudo 会输出 "Sorry" 或再次提示密码
	wrongDeadline := time.Now().Add(5 * time.Second)
	passwordWrong := false
	for time.Now().Before(wrongDeadline) {
		time.Sleep(500 * time.Millisecond)
		newLines2, _ := session.NewLinesSince(linesBefore)
		for _, line := range newLines2 {
			lower := strings.ToLower(line)
			if strings.Contains(lower, "sorry") || strings.Contains(lower, "incorrect") ||
				strings.Contains(lower, "authentication failure") || strings.Contains(lower, "try again") {
				passwordWrong = true
				break
			}
		}
		if passwordWrong {
			break
		}
	}

	if passwordWrong {
		// 发送 Ctrl+C 中断 sudo 的重试循环
		_ = session.Handle.Write([]byte{0x03}) // Ctrl+C
		time.Sleep(500 * time.Millisecond)
		return false, "sudo 密码验证失败，请检查密码是否正确"
	}

	// 7. 验证 token 是否获取成功
	hasToken3, _ := m.checkSudoNopasswd(sessionID)
	if hasToken3 {
		return true, "sudo token 获取成功（有效期约 15 分钟）"
	}

	return false, "sudo 密码验证失败，请检查密码是否正确"
}

// RefreshSudoToken 刷新 sudo token。
// 如果当前 token 仍有效，sudo -v 会重置计时器（再续 15 分钟）。
// 如果 token 已过期，会尝试重新输入密码。
func (m *SSHBackgroundTaskManager) RefreshSudoToken(sessionID string) (ok bool, msg string) {
	// 先尝试无密码刷新
	hasToken, _ := m.checkSudoNopasswd(sessionID)
	if hasToken {
		// token 仍有效，用 sudo -v 续期
		if _, err := m.sshMgr.WriteInputChecked(sessionID, "sudo -v"); err != nil {
			return false, fmt.Sprintf("刷新 sudo token 失败: %v", err)
		}
		time.Sleep(1 * time.Second)
		return true, "sudo token 已刷新"
	}
	// token 已过期，重新走完整流程
	return m.EnsureSudoToken(sessionID)
}

// sudoFallbackRule 定义一条 sudo 命令的降级规则。
type sudoFallbackRule struct {
	// pattern 匹配 sudo 后面的命令模式（正则）
	pattern *regexp.Regexp
	// replacer 接收原始完整命令，返回降级后的命令
	replacer func(cmd string) string
	// hint 给用户的提示信息
	hint string
}

// sudoFallbackRules 定义常见 sudo 命令的非 sudo 替代方案。
// 按优先级排列，第一个匹配的规则生效。
var sudoFallbackRules = func() []sudoFallbackRule {
	// 预编译所有正则，replacer 直接引用
	truncateRe := regexp.MustCompile(`sudo\s+truncate\s+-s\s+0\s+(\S+)`)
	rmLogRe := regexp.MustCompile(`sudo\s+rm\s+(-[rf]+\s+)?(/var/log/\S+)`)
	journalRe := regexp.MustCompile(`sudo\s+(journalctl\s+--vacuum\S*)`)
	systemctlRe := regexp.MustCompile(`sudo\s+(systemctl\s+\S+(\s+\S+)?)`)
	pkgMgrRe := regexp.MustCompile(`sudo\s+((apt|apt-get|yum|dnf)\s+\S+(\s+\S+)*)`)

	return []sudoFallbackRule{
		{
			pattern: truncateRe,
			replacer: func(cmd string) string {
				return truncateRe.ReplaceAllString(cmd, `sudo -n truncate -s 0 $1 2>/dev/null || echo "[maclaw] 跳过清理 $1 (需要 sudo)"`)
			},
			hint: "truncate 需要 root 权限，已改为 sudo -n 尝试 + 跳过",
		},
		{
			pattern: rmLogRe,
			replacer: func(cmd string) string {
				return rmLogRe.ReplaceAllString(cmd, `sudo -n rm $1$2 2>/dev/null || echo "[maclaw] 跳过删除 $2 (需要 sudo)"`)
			},
			hint: "删除系统日志需要 root 权限，已改为 sudo -n 尝试 + 跳过",
		},
		{
			pattern: journalRe,
			replacer: func(cmd string) string {
				return journalRe.ReplaceAllString(cmd, `sudo -n $1 2>/dev/null || echo "[maclaw] 跳过 journalctl 清理 (需要 sudo)"`)
			},
			hint: "journalctl 清理需要 root 权限，已改为 sudo -n 尝试 + 跳过",
		},
		{
			pattern: systemctlRe,
			replacer: func(cmd string) string {
				return systemctlRe.ReplaceAllString(cmd, `sudo -n $1 2>/dev/null || echo "[maclaw] 跳过 systemctl 操作 (需要 sudo)"`)
			},
			hint: "systemctl 操作需要 root 权限，已改为 sudo -n 尝试 + 跳过",
		},
		{
			pattern: pkgMgrRe,
			replacer: func(cmd string) string {
				return pkgMgrRe.ReplaceAllString(cmd, `sudo -n $1 2>/dev/null || echo "[maclaw] 跳过包管理操作 (需要 sudo)"`)
			},
			hint: "包管理操作需要 root 权限，已改为 sudo -n 尝试 + 跳过",
		},
		{
			pattern: regexp.MustCompile(`sudo\s+docker\s`),
			replacer: func(cmd string) string {
				return strings.ReplaceAll(cmd, "sudo docker", "docker")
			},
			hint: "已去掉 sudo，如果失败请将用户加入 docker 组: sudo usermod -aG docker $USER",
		},
		// 通用 sudo → 改为 sudo -n（non-interactive，有权限就执行，没有就报错跳过）
		{
			pattern: regexp.MustCompile(`sudo\s`),
			replacer: func(cmd string) string {
				return sudoRe.ReplaceAllStringFunc(cmd, func(match string) string {
					return strings.Replace(match, "sudo ", "sudo -n ", 1)
				})
			},
			hint: "已将 sudo 改为 sudo -n (non-interactive)，无免密权限的命令会被跳过",
		},
	}
}()

// sudoFallback 尝试将包含 sudo 的命令降级为非 sudo 版本。
// 返回 (降级后的命令, 提示信息)。
// 如果无法降级，返回原命令和配置建议。
func sudoFallback(cmd string) (string, string) {
	for _, rule := range sudoFallbackRules {
		if rule.pattern.MatchString(cmd) {
			newCmd := rule.replacer(cmd)
			if newCmd != cmd {
				return newCmd, rule.hint
			}
		}
	}
	// 不应该到这里（通用规则兜底），但以防万一
	return cmd, "建议在服务器上配置 NOPASSWD: sudo visudo → 添加 'username ALL=(ALL) NOPASSWD: ALL'"
}

// IsLongRunningCommand 判断命令是否可能是长时间运行的命令，
// 建议使用后台模式执行。
func IsLongRunningCommand(cmd string) bool {
	cmd = strings.ToLower(strings.TrimSpace(cmd))
	longPatterns := []string{
		"pip install", "pip3 install",
		"apt install", "apt-get install", "apt update", "apt-get update",
		"yum install", "dnf install",
		"conda install", "conda create", "conda env create", "mamba install",
		"npm install", "yarn install", "pnpm install",
		"cmake --build", "cargo build", "cargo install",
		"docker build", "docker pull", "docker compose", "docker-compose",
		"git clone", "git lfs pull",
		"wget ", "curl -o", "curl -O",
		"rsync ", "scp ",
		"python setup.py", "python -m pip", "python3 -m pip",
		"go build", "go install",
		"huggingface-cli download",
		"dpkg -i", "rpm -i",
	}
	for _, p := range longPatterns {
		if strings.Contains(cmd, p) {
			return true
		}
	}
	// 需要精确匹配的模式（避免 "make" 匹配 "mkdir" 等）
	exactPrefixes := []string{"make ", "make\n", "tar ", "unzip ", "gzip ", "7z "}
	for _, p := range exactPrefixes {
		if strings.HasPrefix(cmd, p) || strings.Contains(cmd, " "+p) || strings.Contains(cmd, "&&"+p) || strings.Contains(cmd, ";"+p) {
			return true
		}
	}
	// 单独的 "make"（整个命令就是 make）
	if cmd == "make" {
		return true
	}
	return false
}
