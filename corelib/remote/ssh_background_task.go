package remote

import (
	"fmt"
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
func (m *SSHBackgroundTaskManager) Submit(sessionID, command string) (*SSHBackgroundTask, error) {
	session, ok := m.sshMgr.Get(sessionID)
	if !ok {
		return nil, fmt.Errorf("ssh session %s not found", sessionID)
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
