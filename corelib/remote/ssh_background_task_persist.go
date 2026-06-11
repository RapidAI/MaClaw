package remote

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// persistedTask 是 SSHBackgroundTask 的持久化格式。
// 只保留恢复所需的字段，不保留 sync.Mutex。
type persistedTask struct {
	TaskID    string                  `json:"task_id"`
	SessionID string                  `json:"session_id"`
	HostID    string                  `json:"host_id"` // user@host:port，用于跨会话匹配
	Command   string                  `json:"command"`
	TaskRole  string                  `json:"task_role,omitempty"`
	LogFile   string                  `json:"log_file"`
	PIDFile   string                  `json:"pid_file"`
	Status    SSHBackgroundTaskStatus `json:"status"`
	PID       string                  `json:"pid,omitempty"`
	StartedAt time.Time               `json:"started_at"`
	LastCheck time.Time               `json:"last_check,omitempty"`
}

// persistedRegistry 是持久化到磁盘的任务注册表。
type persistedRegistry struct {
	Tasks     []persistedTask `json:"tasks"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// --- 持久化能力注入 ---

// SetPersistDir 设置任务注册表的持久化目录。
// 调用后立即从文件加载已有任务。如果不调用，任务不持久化（向后兼容）。
func (m *SSHBackgroundTaskManager) SetPersistDir(dir string) {
	m.mu.Lock()
	m.persistDir = dir
	m.mu.Unlock()

	// 从文件加载已有任务
	m.loadPersistedTasks()
}

// persistPath 返回持久化文件路径。
func (m *SSHBackgroundTaskManager) persistPath() string {
	m.mu.RLock()
	dir := m.persistDir
	m.mu.RUnlock()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "ssh_bg_tasks.json")
}

// saveToDisk 将当前任务注册表持久化到磁盘。
// 只保存 active 或最近 24 小时内的任务（避免文件无限膨胀）。
func (m *SSHBackgroundTaskManager) saveToDisk() {
	path := m.persistPath()
	if path == "" {
		return
	}

	m.mu.RLock()
	reg := persistedRegistry{
		UpdatedAt: time.Now(),
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	for _, t := range m.tasks {
		t.mu.Lock()
		// 只持久化 active 任务或 24 小时内的任务
		if t.Status.IsActive() || t.StartedAt.After(cutoff) || t.LastCheck.After(cutoff) {
			pt := persistedTask{
				TaskID:    t.TaskID,
				SessionID: t.SessionID,
				HostID:    m.resolveHostIDForTask(t),
				Command:   t.Command,
				TaskRole:  t.TaskRole,
				LogFile:   t.LogFile,
				PIDFile:   t.PIDFile,
				Status:    t.Status,
				PID:       t.PID,
				StartedAt: t.StartedAt,
				LastCheck: t.LastCheck,
			}
			reg.Tasks = append(reg.Tasks, pt)
		}
		t.mu.Unlock()
	}
	m.mu.RUnlock()

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("[ssh-bg-persist] mkdir failed: %v", err)
		return
	}

	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		log.Printf("[ssh-bg-persist] marshal failed: %v", err)
		return
	}

	// 原子写入（写临时文件再 rename）
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		log.Printf("[ssh-bg-persist] write failed: %v", err)
		return
	}
	if err := os.Rename(tmpPath, path); err != nil {
		log.Printf("[ssh-bg-persist] rename failed: %v", err)
		_ = os.Remove(tmpPath)
	}
}

// loadPersistedTasks 从磁盘加载任务注册表。
// 只恢复 active 状态的任务（completed/failed/killed 的不需要恢复）。
func (m *SSHBackgroundTaskManager) loadPersistedTasks() {
	path := m.persistPath()
	if path == "" {
		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[ssh-bg-persist] read failed: %v", err)
		}
		return
	}

	var reg persistedRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		log.Printf("[ssh-bg-persist] unmarshal failed: %v", err)
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	loaded := 0
	for _, pt := range reg.Tasks {
		// 只恢复 active 状态的任务
		if !pt.Status.IsActive() {
			continue
		}
		// 跳过已存在的任务（不覆盖内存中的最新状态）
		if _, exists := m.tasks[pt.TaskID]; exists {
			continue
		}
		task := &SSHBackgroundTask{
			TaskID:    pt.TaskID,
			SessionID: pt.SessionID,
			Command:   pt.Command,
			TaskRole:  pt.TaskRole,
			LogFile:   pt.LogFile,
			PIDFile:   pt.PIDFile,
			Status:    pt.Status,
			PID:       pt.PID,
			StartedAt: pt.StartedAt,
			LastCheck: pt.LastCheck,
		}
		m.tasks[pt.TaskID] = task
		loaded++
	}
	if loaded > 0 {
		log.Printf("[ssh-bg-persist] restored %d active tasks from disk", loaded)
	}
}

// resolveHostIDForTask 为持久化的任务解析 hostID。
// 优先从 session 的 spec 中获取，回退到 session ID 中提取。
func (m *SSHBackgroundTaskManager) resolveHostIDForTask(t *SSHBackgroundTask) string {
	if m.sshMgr == nil {
		return extractHostIDFromSessionID(t.SessionID)
	}
	session, ok := m.sshMgr.Get(t.SessionID)
	if !ok {
		return extractHostIDFromSessionID(t.SessionID)
	}
	return session.Spec.HostConfig.SSHHostID()
}

// extractHostIDFromSessionID 从 session ID 中提取 host ID。
// session ID 格式: ssh_{user}@{host}:{port}_{counter}
func extractHostIDFromSessionID(sessionID string) string {
	// ssh_root@api.example.com:22_1 → root@api.example.com:22
	if !strings.HasPrefix(sessionID, "ssh_") {
		return sessionID
	}
	rest := sessionID[4:] // root@api.example.com:22_1
	// 从右边找最后一个 _数字 后缀
	lastUnderscore := strings.LastIndex(rest, "_")
	if lastUnderscore > 0 {
		suffix := rest[lastUnderscore+1:]
		allDigit := true
		for _, c := range suffix {
			if c < '0' || c > '9' {
				allDigit = false
				break
			}
		}
		if allDigit {
			return rest[:lastUnderscore]
		}
	}
	return rest
}

// --- Orphan 任务重新发现 ---

// findDuplicateActiveTask 在注册表中查找与给定命令相同或高度相似的
// active 任务。用于 Submit 时去重——防止 LLM 重复提交相同命令。
//
// 匹配策略（保守）：
// - 精确匹配：命令完全相同
// - 核心命令匹配：去除前缀的 echo/cd 等 wrapper 后的核心命令相同
//
// 返回 nil 表示无重复。
func (m *SSHBackgroundTaskManager) findDuplicateActiveTask(command string) *SSHBackgroundTask {
	normalized := normalizeCommandForDedup(command)
	if normalized == "" {
		return nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, t := range m.tasks {
		t.mu.Lock()
		status := t.Status
		existingCmd := t.Command
		t.mu.Unlock()

		if !status.IsActive() {
			continue
		}

		// 精确匹配
		if existingCmd == command {
			return t
		}
		// 核心命令匹配
		if normalizeCommandForDedup(existingCmd) == normalized {
			return t
		}
	}
	return nil
}

// normalizeCommandForDedup 提取命令的核心部分用于去重比较。
// 去除常见的 wrapper（echo 前缀、sudo 前缀、环境变量前缀等），
// 保留实际执行的命令。
func normalizeCommandForDedup(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}
	// 去除 VAR=value 环境变量前缀（如 SSHPASS='...' timeout 60 sshpass ...）
	for strings.Contains(cmd, "=") {
		// 检查是否以 WORD= 开头（环境变量赋值）
		spaceIdx := strings.IndexByte(cmd, ' ')
		eqIdx := strings.IndexByte(cmd, '=')
		if eqIdx > 0 && (spaceIdx < 0 || eqIdx < spaceIdx) {
			// 跳过这个 VAR=value token
			// value 可能被引号包裹
			rest := cmd[eqIdx+1:]
			if len(rest) > 0 && (rest[0] == '\'' || rest[0] == '"') {
				quote := rest[0]
				closeIdx := strings.IndexByte(rest[1:], quote)
				if closeIdx >= 0 {
					cmd = strings.TrimSpace(rest[closeIdx+2:])
					continue
				}
			}
			// value 无引号，到下一个空格结束
			if spaceAfterVal := strings.IndexByte(rest, ' '); spaceAfterVal >= 0 {
				cmd = strings.TrimSpace(rest[spaceAfterVal+1:])
				continue
			}
			break // value 是最后一个 token
		}
		break
	}
	// 去除 timeout N 前缀
	if strings.HasPrefix(cmd, "timeout ") {
		parts := strings.SplitN(cmd, " ", 3)
		if len(parts) >= 3 {
			// parts[1] 应该是数字
			allDigit := true
			for _, c := range parts[1] {
				if c < '0' || c > '9' {
					allDigit = false
					break
				}
			}
			if allDigit {
				cmd = strings.TrimSpace(parts[2])
			}
		}
	}
	// 去除 sudo -n / sudo 前缀
	if strings.HasPrefix(cmd, "sudo ") {
		after := strings.TrimSpace(cmd[5:])
		if strings.HasPrefix(after, "-n ") {
			cmd = strings.TrimSpace(after[3:])
		} else {
			cmd = after
		}
	}
	// 去除 echo + && 前缀（maclaw 的 sudo 降级会加 echo '[maclaw]...' &&）
	if idx := strings.Index(cmd, "' && "); idx > 0 && strings.HasPrefix(cmd, "echo '") {
		cmd = strings.TrimSpace(cmd[idx+5:])
	}
	// 去除多余空白
	cmd = strings.TrimSpace(cmd)
	// 截断到前 200 字符比较（避免长命令因 wrapper 尾部不同而不匹配）
	if len(cmd) > 200 {
		cmd = cmd[:200]
	}
	return cmd
}

// findAlternateSession 当任务的原始 session 不存在时，
// 查找当前任何 running 状态的会话作为替代（用于 CheckTask/KillTask）。
// 优先匹配同一 host 的会话。
func (m *SSHBackgroundTaskManager) findAlternateSession(task *SSHBackgroundTask) *SSHManagedSession {
	if m.sshMgr == nil {
		return nil
	}
	sessions := m.sshMgr.List()
	hostID := extractHostIDFromSessionID(task.SessionID)

	// 优先找同 host 的 running 会话
	for _, s := range sessions {
		summary := s.GetSummary()
		if SessionStatus(summary.Status).IsRunning() && summary.HostID == hostID {
			return s
		}
	}
	// 退而求其次：任何 running 会话（同一台机器可能用了不同端口/用户）
	// 但只在有 1 个会话时使用（避免发到错误的服务器）
	var running []*SSHManagedSession
	for _, s := range sessions {
		summary := s.GetSummary()
		if SessionStatus(summary.Status).IsRunning() {
			running = append(running, s)
		}
	}
	if len(running) == 1 {
		return running[0]
	}
	return nil
}

// RediscoverOrphanTasks 在 SSH 连接成功后，扫描远程服务器上的
// maclaw 后台任务 PID 文件，将仍在运行但注册表中没有的进程重新注册。
//
// 工作原理：
// 1. 列出 /tmp/maclaw_bg_*.pid 文件
// 2. 读取每个 PID 文件内容
// 3. 检查 PID 是否存活（kill -0）
// 4. 存活且注册表中没有 → 重新注册
func (m *SSHBackgroundTaskManager) RediscoverOrphanTasks(sessionID string) (discovered int) {
	if m.sshMgr == nil {
		return 0
	}
	session, ok := m.sshMgr.Get(sessionID)
	if !ok {
		return 0
	}

	// 一次性扫描所有 maclaw 后台任务的 PID 文件和状态
	// 同时读取日志第二行恢复原始命令（格式: CMD: <command>）
	scanCmd := `for f in /tmp/maclaw_bg_*.pid; do [ -f "$f" ] || continue; PID=$(cat "$f" 2>/dev/null); TASKID=$(basename "$f" .pid | sed 's/maclaw_bg_//'); ALIVE="no"; kill -0 "$PID" 2>/dev/null && ALIVE="yes"; LOGF="/tmp/maclaw_bg_${TASKID}.log"; CMD=""; [ -f "$LOGF" ] && CMD=$(sed -n '2s/^CMD: //p' "$LOGF" 2>/dev/null); echo "MACLAW_ORPHAN:$TASKID:$PID:$ALIVE:$CMD"; done; echo "MACLAW_ORPHAN_SCAN_DONE"`

	linesBefore := session.LineCount()
	if _, err := m.sshMgr.WriteInputChecked(sessionID, scanCmd); err != nil {
		return 0
	}

	// 等待扫描完成（5 秒超时——扫描 /tmp 文件通常很快）
	newLines, _ := m.sshMgr.WaitForOutput(sessionID, linesBefore, 5*time.Second)

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, line := range newLines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "MACLAW_ORPHAN:") {
			continue
		}
		// Format: MACLAW_ORPHAN:taskID:pid:alive:command
		// Command may contain colons, so use SplitN with limit 5.
		parts := strings.SplitN(line, ":", 5)
		if len(parts) < 4 {
			continue
		}
		taskID := parts[1]
		pid := parts[2]
		alive := parts[3] == "yes"

		if !alive {
			continue
		}

		// 检查是否已在注册表中
		if _, exists := m.tasks[taskID]; exists {
			continue
		}

		// 恢复原始命令（从日志文件的 CMD: 行）
		command := "(recovered orphan task)"
		if len(parts) == 5 && parts[4] != "" {
			command = parts[4]
		}

		// 重新注册 orphan 任务
		logFile := fmt.Sprintf("/tmp/maclaw_bg_%s.log", taskID)
		pidFile := fmt.Sprintf("/tmp/maclaw_bg_%s.pid", taskID)

		task := &SSHBackgroundTask{
			TaskID:    taskID,
			SessionID: sessionID, // 用当前会话关联
			Command:   command,
			LogFile:   logFile,
			PIDFile:   pidFile,
			Status:    SSHBackgroundTaskStatusRunning,
			PID:       pid,
			StartedAt: time.Now(), // 无法确定原始启动时间
			LastCheck: time.Now(),
		}
		m.tasks[taskID] = task
		discovered++
	}

	if discovered > 0 {
		log.Printf("[ssh-bg-persist] rediscovered %d orphan tasks on %s", discovered, sessionID)
		// 持久化新发现的任务
		go m.saveToDisk()
	}

	return discovered
}

// --- 持久化触发点（在 Submit/CheckTask/KillTask 后调用） ---

// signalPersist 异步触发持久化。使用 debounce 避免频繁写盘。
func (m *SSHBackgroundTaskManager) signalPersist() {
	m.mu.RLock()
	dir := m.persistDir
	m.mu.RUnlock()
	if dir == "" {
		return
	}

	// 简单的异步 debounce：如果已有 pending 的写盘，不重复调度
	m.persistOnce.Do(func() {
		m.persistCh = make(chan struct{}, 1)
		go m.persistLoop()
	})

	select {
	case m.persistCh <- struct{}{}:
	default:
	}
}

// persistLoop 是后台持久化 goroutine，使用 150ms debounce。
// 此 goroutine 与进程同生命周期（persistCh 永不关闭）。
// SSHBackgroundTaskManager 是进程级单例，不存在 GC 泄漏问题。
func (m *SSHBackgroundTaskManager) persistLoop() {
	for range m.persistCh {
		time.Sleep(150 * time.Millisecond)
		// drain any additional signals during debounce
		for {
			select {
			case <-m.persistCh:
			default:
				goto flush
			}
		}
	flush:
		m.saveToDisk()
	}
}
