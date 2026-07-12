package remote

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// SSHManagedSession 是 SSHSessionManager 管理的单个 SSH 会话。
type SSHManagedSession struct {
	mu           sync.Mutex
	ID           string
	Spec         SSHSessionSpec
	Status       SessionStatus
	Summary      SSHSessionSummary
	Handle       *SSHPTYSession
	PreviewLines []string
	CreatedAt    time.Time
	ExitCode     *int
	LastOutputAt time.Time

	// consecutiveExecFailures 记录连续 exec 失败次数（无输出或错误）。
	// 达到阈值时上层应自动关闭并重建会话，避免 LLM 在死会话上无限重试。
	consecutiveExecFailures int
}

// PreviewTail 返回最后 n 行预览输出。
func (s *SSHManagedSession) PreviewTail(n int) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n <= 0 || len(s.PreviewLines) == 0 {
		return nil
	}
	start := 0
	if len(s.PreviewLines) > n {
		start = len(s.PreviewLines) - n
	}
	out := make([]string, len(s.PreviewLines)-start)
	copy(out, s.PreviewLines[start:])
	return out
}

// LineCount 返回当前预览行数。
func (s *SSHManagedSession) LineCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.PreviewLines)
}

// NewLinesSince 返回从 afterLine 开始的新行。
func (s *SSHManagedSession) NewLinesSince(afterLine int) ([]string, SessionStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var lines []string
	if len(s.PreviewLines) > afterLine {
		lines = make([]string, len(s.PreviewLines)-afterLine)
		copy(lines, s.PreviewLines[afterLine:])
	}
	return lines, s.Status
}

// GetSummary 返回会话摘要的副本。
func (s *SSHManagedSession) GetSummary() SSHSessionSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Summary
}

// SSHSessionManager 管理所有 SSH 远程会话的生命周期。
// 复用 SSHPool 做连接管理，对上层暴露与 TUISessionManager 一致的接口模式。
type SSHSessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*SSHManagedSession
	pool     *SSHPool
	onUpdate func(sessionID string)
	counter  int
}

// NewSSHSessionManager 创建 SSH 会话管理器。
func NewSSHSessionManager(pool *SSHPool) *SSHSessionManager {
	if pool == nil {
		pool = NewSSHPool()
	}
	return &SSHSessionManager{
		sessions: make(map[string]*SSHManagedSession),
		pool:     pool,
	}
}

// SetOnUpdate 设置会话状态变更回调。
func (m *SSHSessionManager) SetOnUpdate(fn func(sessionID string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onUpdate = fn
}

// Pool 返回底层连接池（供外部查看连接状态）。
func (m *SSHSessionManager) Pool() *SSHPool { return m.pool }

// Create 创建并启动一个新的 SSH 交互会话。
func (m *SSHSessionManager) Create(spec SSHSessionSpec) (*SSHManagedSession, error) {
	spec.HostConfig.Defaults()
	hostID := spec.HostConfig.SSHHostID()

	// 从连接池获取连接
	client, err := m.pool.Acquire(spec.HostConfig)
	if err != nil {
		return nil, fmt.Errorf("acquire ssh connection: %w", err)
	}

	// 创建 PTY 会话
	ptySession := NewSSHPTYSession(client, hostID)
	if err := ptySession.Start(spec); err != nil {
		m.pool.Release(spec.HostConfig)
		return nil, fmt.Errorf("start ssh pty: %w", err)
	}

	now := time.Now()
	m.mu.Lock()
	m.counter++
	sessionID := spec.SessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("ssh_%s_%d", hostID, m.counter)
	}
	m.mu.Unlock()

	label := spec.HostConfig.Label
	if label == "" {
		label = hostID
	}

	session := &SSHManagedSession{
		ID:     sessionID,
		Spec:   spec,
		Status: SessionRunning,
		Summary: SSHSessionSummary{
			SessionID: sessionID,
			HostID:    hostID,
			HostLabel: label,
			Status:    string(SessionRunning),
			UpdatedAt: now.Unix(),
		},
		Handle:    ptySession,
		CreatedAt: now,
	}

	m.mu.Lock()
	m.sessions[sessionID] = session
	m.mu.Unlock()

	go m.runOutputLoop(session)
	go m.runExitLoop(session, spec.HostConfig)

	return session, nil
}

// Get 获取会话。
func (m *SSHSessionManager) Get(sessionID string) (*SSHManagedSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[sessionID]
	return s, ok
}

// List 列出所有 SSH 会话。
func (m *SSHSessionManager) List() []*SSHManagedSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*SSHManagedSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		result = append(result, s)
	}
	return result
}

// WriteInput 向 SSH 会话写入命令。
func (m *SSHSessionManager) WriteInput(sessionID, text string) error {
	s, ok := m.Get(sessionID)
	if !ok {
		return fmt.Errorf("ssh session %s not found", sessionID)
	}
	if s.Handle == nil {
		return fmt.Errorf("ssh session %s has no handle", sessionID)
	}
	return s.Handle.Write([]byte(text + "\n"))
}

// WriteInputChecked 向 SSH 会话写入命令，写入前检查连接存活。
// 如果连接已断，尝试自动重连。返回是否发生了重连。
func (m *SSHSessionManager) WriteInputChecked(sessionID, text string) (reconnected bool, err error) {
	s, ok := m.Get(sessionID)
	if !ok {
		return false, fmt.Errorf("ssh session %s not found", sessionID)
	}
	if s.Handle == nil {
		return false, fmt.Errorf("ssh session %s has no handle", sessionID)
	}

	// 先尝试直接写入
	writeErr := s.Handle.Write([]byte(text + "\n"))
	if writeErr == nil {
		return false, nil
	}

	// 写入失败，检查连接是否存活
	if s.Handle.IsAlive() {
		return false, writeErr
	}

	// 连接已断，尝试重连
	if err := m.reconnectSession(s); err != nil {
		return false, fmt.Errorf("自动重连失败: %w (原始错误: %v)", err, writeErr)
	}

	// 重连成功，重试写入
	if err := s.Handle.Write([]byte(text + "\n")); err != nil {
		return true, fmt.Errorf("重连后写入失败: %w", err)
	}
	return true, nil
}

// reconnectSession 对已断开的会话执行重连：重新建立 SSH 连接和 PTY 会话。
func (m *SSHSessionManager) reconnectSession(s *SSHManagedSession) error {
	s.mu.Lock()
	spec := s.Spec
	oldHandle := s.Handle
	s.mu.Unlock()

	spec.HostConfig.Defaults()

	// 关闭旧 handle
	if oldHandle != nil {
		_ = oldHandle.Close()
	}

	// 通过连接池重连
	client, err := m.pool.Reconnect(spec.HostConfig)
	if err != nil {
		return err
	}

	hostID := spec.HostConfig.SSHHostID()
	ptySession := NewSSHPTYSession(client, hostID)
	if err := ptySession.Start(spec); err != nil {
		m.pool.Release(spec.HostConfig)
		return fmt.Errorf("restart ssh pty: %w", err)
	}

	s.mu.Lock()
	s.Handle = ptySession
	s.Status = SessionRunning
	s.Summary.Status = string(SessionRunning)
	s.Summary.UpdatedAt = time.Now().Unix()
	s.ExitCode = nil
	s.consecutiveExecFailures = 0
	// 清空旧输出，避免重连后行号错乱
	s.PreviewLines = s.PreviewLines[:0]
	s.mu.Unlock()

	go m.runOutputLoop(s)
	go m.runExitLoop(s, spec.HostConfig)

	return nil
}

// ReconnectByID 通过会话 ID 执行重连。
// 适用于会话已处于 exited/error 状态时，由上层调用恢复会话。
func (m *SSHSessionManager) ReconnectByID(sessionID string) error {
	s, ok := m.Get(sessionID)
	if !ok {
		return fmt.Errorf("ssh session %s not found", sessionID)
	}
	return m.reconnectSession(s)
}

// InterruptByID sends Ctrl+C to the managed SSH PTY session.
func (m *SSHSessionManager) InterruptByID(sessionID string) error {
	s, ok := m.Get(sessionID)
	if !ok {
		return fmt.Errorf("ssh session %s not found", sessionID)
	}
	if s.Handle == nil {
		return fmt.Errorf("ssh session %s has no handle", sessionID)
	}
	return s.Handle.Interrupt()
}

// CheckShellResponsive 验证 SSH 会话的 shell 是否真正可响应命令。
// 与 IsAlive() 不同：IsAlive 只检查 SSH 连接级别的心跳，
// CheckShellResponsive 实际发送一个 echo 命令并等待输出，
// 能检测到 shell 被 sqlite3/vim/less 等交互式程序锁住的情况，
// 以及 PTY stdout 管道断裂的情况。
//
// 如果 shell 无响应，会先发送 Ctrl+C 尝试中断当前命令，
// 然后再次检测。如果仍无响应，返回 false。
func (m *SSHSessionManager) CheckShellResponsive(sessionID string) bool {
	s, ok := m.Get(sessionID)
	if !ok {
		return false
	}
	if s.Handle == nil {
		return false
	}

	// 先检查连接级别是否存活
	if !s.Handle.IsAlive() {
		return false
	}

	marker := fmt.Sprintf("__mclw_%d__", time.Now().UnixNano()%1000000)

	// 第一次探测
	if m.probeShell(s, marker) {
		return true
	}

	// Shell 无响应，尝试发送 Ctrl+C 中断可能挂起的命令
	_ = s.Handle.Interrupt()
	time.Sleep(500 * time.Millisecond)

	// 第二次探测
	return m.probeShell(s, marker)
}

// probeShell 发送一个带标记的 echo 命令并等待输出。
func (m *SSHSessionManager) probeShell(s *SSHManagedSession, marker string) bool {
	probe := fmt.Sprintf("echo %s", marker)
	linesBefore := s.LineCount()
	if err := s.Handle.Write([]byte(probe + "\n")); err != nil {
		return false
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(300 * time.Millisecond)
		newLines, _ := s.NewLinesSince(linesBefore)
		for _, line := range newLines {
			if strings.Contains(line, marker) {
				return true
			}
		}
	}
	return false
}

// RecordExecSuccess 记录一次成功的 exec 操作，重置连续失败计数。
func (m *SSHSessionManager) RecordExecSuccess(sessionID string) {
	s, ok := m.Get(sessionID)
	if !ok {
		return
	}
	s.mu.Lock()
	s.consecutiveExecFailures = 0
	s.mu.Unlock()
}

// RecordExecFailure 记录一次失败的 exec 操作。
// 返回当前连续失败次数。上层可据此决定是否自动重建会话。
func (m *SSHSessionManager) RecordExecFailure(sessionID string) int {
	s, ok := m.Get(sessionID)
	if !ok {
		return 0
	}
	s.mu.Lock()
	s.consecutiveExecFailures++
	count := s.consecutiveExecFailures
	s.mu.Unlock()
	return count
}

// WaitForOutput 等待命令输出完成。
//
// 完成检测机制（Prompt-Driven Completion Detection）：
//
// 核心原理：命令执行完毕后，shell 会打印新的 prompt（如 root@server:~# ）。
// 这是命令完成的唯一确定性信号。"沉默时间"只是辅助 fallback。
//
// 检测优先级：
//  1. 会话退出（SessionExited/SessionError）→ 立即返回
//  2. Shell prompt 出现在新输出的最后一行 → 立即返回（主信号，零延迟）
//  3. 稳定性 fallback：连续无新输出超过阈值 → 返回（辅助信号）
//
// 两阶段稳定阈值：
//   - 阶段 1（等待首行实际输出）：命令回显后，等待实际输出出现。阈值较高（~4s）。
//   - 阶段 2（等待输出结束）：已有实际输出，等待输出停止。阈值较低（~2.4s）。
//
// maxWait 是最大等待时间上限。超时后发送 Ctrl+C 防止 shell 被锁住。
func (m *SSHSessionManager) WaitForOutput(sessionID string, afterLine int, maxWait time.Duration) ([]string, SessionStatus) {
	s, ok := m.Get(sessionID)
	if !ok {
		return nil, SessionError
	}

	if maxWait <= 0 {
		maxWait = 30 * time.Second
	}

	const pollInterval = 300 * time.Millisecond

	// 阶段 1 稳定阈值：等待首行实际输出出现。
	// 13 次 × 300ms ≈ 3.9s — 覆盖 cp/docker/apt 等命令的启动延迟。
	const phase1StableThreshold = 13

	// 阶段 2 稳定阈值：已有实际输出，等待输出停止。
	// 8 次 × 300ms ≈ 2.4s — 命令已在输出，短暂停顿后判定完成。
	const phase2StableThreshold = 8

	// 从阶段 1 进入阶段 2 的条件：收到超过 1 行新输出（排除命令回显本身）。
	// PTY 回显通常是 1-2 行（命令文本 + 可能的换行），实际输出从第 3 行开始。
	const phase2TriggerLines = 3

	deadline := time.Now().Add(maxWait)
	stableCount := 0
	lastLineCount := afterLine
	exitedByBreak := false
	totalNewLines := 0

	for time.Now().Before(deadline) {
		time.Sleep(pollInterval)

		currentCount := s.LineCount()

		// 检查会话是否已退出
		s.mu.Lock()
		status := s.Status
		s.mu.Unlock()
		if status == SessionExited || status == SessionError {
			lines, st := s.NewLinesSince(afterLine)
			return lines, st
		}

		if currentCount > lastLineCount {
			// 有新输出
			totalNewLines += currentCount - lastLineCount
			stableCount = 0
			lastLineCount = currentCount

			// 主信号：每次有新输出时检查最后一行是否是 shell prompt。
			// 如果是，说明命令已完成，shell 在等待下一条命令。立即返回。
			// 只在收到至少 2 行后才检查（第 1 行通常是命令回显）。
			if totalNewLines >= 2 && looksLikeShellPrompt(s.PreviewTail(1)) {
				exitedByBreak = true
				break
			}
		} else {
			stableCount++

			// 辅助信号：稳定性 fallback
			threshold := phase1StableThreshold
			if totalNewLines >= phase2TriggerLines {
				threshold = phase2StableThreshold
			}

			if stableCount >= threshold {
				// 再次检查 prompt（可能在之前的轮询间隙中出现）
				if looksLikeShellPrompt(s.PreviewTail(1)) {
					exitedByBreak = true
					break
				}
				// 不像 prompt，再多等几轮
				if stableCount >= threshold+4 {
					exitedByBreak = true
					break
				}
			}
		}
	}

	// 超时处理：deadline 到期且非 break 退出。
	// 如果最后一行不是 prompt，命令可能挂起了。发送 Ctrl+C 中断。
	timedOut := !exitedByBreak && !looksLikeShellPrompt(s.PreviewTail(1))
	if timedOut && s.Handle != nil {
		_ = s.Handle.Interrupt()
		time.Sleep(500 * time.Millisecond)
	}

	lines, status := s.NewLinesSince(afterLine)
	if timedOut && len(lines) > 0 {
		lines = append(lines, "[maclaw] 命令执行超时，已发送 Ctrl+C 中断")
	}
	return lines, status
}

// looksLikeShellPrompt 判断最后一行是否像 shell prompt。
// 常见 prompt 模式：以 $ # > % 结尾（可能跟随空格）。
// 处理 ANSI 转义序列：PTY 输出的 prompt 通常包含颜色/标题设置等转义码，
// 需要剥离后再检查末尾字符。
//
// 为避免误判（如 `echo "price is 100$"` 的输出以 $ 结尾），
// 增加结构性验证：真正的 prompt 通常包含 user@host:path 格式的前缀。
func looksLikeShellPrompt(lines []string) bool {
	if len(lines) == 0 {
		return false
	}
	last := lines[len(lines)-1]
	// 剥离 ANSI 转义序列（CSI 和 OSC）
	last = stripANSIForPromptCheck(last)
	last = strings.TrimRight(last, " \t\r\n")
	if last == "" {
		return false
	}
	lastChar := last[len(last)-1]
	if lastChar != '$' && lastChar != '#' && lastChar != '>' && lastChar != '%' {
		return false
	}

	// 结构性验证：真正的 shell prompt 通常包含以下特征之一：
	//   - user@host 格式（含 @）
	//   - 路径分隔符（含 : 或 ~）
	//   - 纯 $ 或 # 或 > 或 %（单字符 prompt，如 sh 的默认 prompt）
	if len(last) == 1 {
		return true // 纯单字符 prompt（sh 默认）
	}
	// 检查 prompt 前缀是否有结构性特征
	prefix := last[:len(last)-1]
	return strings.ContainsAny(prefix, "@:~")
}

// stripANSIForPromptCheck 剥离 ANSI 转义序列用于 prompt 检测。
// 支持 CSI (\x1b[...X) 和 OSC (\x1b]...BEL/ST) 两种常见格式。
func stripANSIForPromptCheck(s string) string {
	var result []byte
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' {
			i++
			if i >= len(s) {
				break
			}
			if s[i] == '[' {
				// CSI sequence: \x1b[ ... (终止于 0x40-0x7E)
				i++
				for i < len(s) && (s[i] < 0x40 || s[i] > 0x7E) {
					i++
				}
				if i < len(s) {
					i++ // 跳过终止字符
				}
			} else if s[i] == ']' {
				// OSC sequence: \x1b] ... (终止于 BEL=0x07 或 ST=\x1b\\)
				i++
				for i < len(s) {
					if s[i] == 0x07 {
						i++
						break
					}
					if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}
			} else {
				// 其他转义序列：
				// - \x1b(X / \x1b)X：字符集选择（2 字符参数）
				// - \x1bX：其他单字符转义
				if i < len(s) && (s[i] == '(' || s[i] == ')') {
					i++ // 跳过 ( 或 )
					if i < len(s) {
						i++ // 跳过字符集标识符（如 B）
					}
				} else if i < len(s) {
					i++ // 跳过单字符
				}
			}
		} else {
			result = append(result, s[i])
			i++
		}
	}
	return string(result)
}

// Interrupt 向 SSH 会话发送 Ctrl+C。
func (m *SSHSessionManager) Interrupt(sessionID string) error {
	s, ok := m.Get(sessionID)
	if !ok {
		return fmt.Errorf("ssh session %s not found", sessionID)
	}
	if s.Handle == nil {
		return fmt.Errorf("ssh session %s has no handle", sessionID)
	}
	return s.Handle.Interrupt()
}

// Kill 终止 SSH 会话。
func (m *SSHSessionManager) Kill(sessionID string) error {
	s, ok := m.Get(sessionID)
	if !ok {
		return fmt.Errorf("ssh session %s not found", sessionID)
	}
	if s.Handle == nil {
		return fmt.Errorf("ssh session %s has no handle", sessionID)
	}
	return s.Handle.Kill()
}

// GetSessionStatus 实现 SessionProvider 接口。
func (m *SSHSessionManager) GetSessionStatus(sessionID string) (SessionStatus, bool) {
	s, ok := m.Get(sessionID)
	if !ok {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Status, true
}

// RemoveSession 从管理器中移除指定会话并关闭其 handle。
// 用于清理底层连接已死但状态仍为 running 的僵尸会话。
//
// 不直接调用 pool.Release —— 连接池引用由 runExitLoop 在 handle 关闭后
// 自然释放。如果 reconnectSession 已经失败过，旧连接已被 pool.Reconnect
// 清理，runExitLoop 的 Release 调用是安全的 no-op。
func (m *SSHSessionManager) RemoveSession(sessionID string) {
	m.mu.Lock()
	s, ok := m.sessions[sessionID]
	if ok {
		delete(m.sessions, sessionID)
	}
	m.mu.Unlock()

	if ok && s != nil {
		if s.Handle != nil {
			_ = s.Handle.Close()
		}
		s.mu.Lock()
		s.Status = SessionExited
		s.Summary.Status = string(SessionExited)
		s.mu.Unlock()
	}
}

// Close 关闭所有会话和连接池。
func (m *SSHSessionManager) Close() {
	m.mu.Lock()
	sessions := make([]*SSHManagedSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = make(map[string]*SSHManagedSession)
	m.mu.Unlock()

	for _, s := range sessions {
		if s.Handle != nil {
			_ = s.Handle.Close()
		}
	}
	m.pool.CloseAll()
}

func (m *SSHSessionManager) runOutputLoop(s *SSHManagedSession) {
	if s.Handle == nil {
		return
	}
	outCh := s.Handle.Output()
	if outCh == nil {
		return
	}
	for chunk := range outCh {
		if len(chunk) == 0 {
			continue
		}
		lines := splitSSHOutputLines(chunk)
		now := time.Now()

		s.mu.Lock()
		s.PreviewLines = append(s.PreviewLines, lines...)
		if len(s.PreviewLines) > 2000 {
			s.PreviewLines = s.PreviewLines[len(s.PreviewLines)-2000:]
		}
		s.LastOutputAt = now
		s.Summary.UpdatedAt = now.Unix()
		if len(lines) > 0 {
			s.Summary.LastOutput = lines[len(lines)-1]
		}
		s.mu.Unlock()

		m.mu.RLock()
		cb := m.onUpdate
		m.mu.RUnlock()
		if cb != nil {
			cb(s.ID)
		}
	}
}

func (m *SSHSessionManager) runExitLoop(s *SSHManagedSession, hostCfg SSHHostConfig) {
	if s.Handle == nil {
		return
	}
	// 在 goroutine 启动时捕获当前 handle 引用。
	// 关键：reconnectSession 会替换 s.Handle 为新的 PTY 会话，
	// 如果这里不捕获，后面的 Close() 会关闭新会话而非旧会话，
	// pool.Release 也会错误地释放新连接的引用计数。
	handle := s.Handle
	exitCh := handle.Exit()
	if exitCh == nil {
		return
	}
	exit := <-exitCh

	// 检查 handle 是否仍然是当前活跃的 handle。
	// 如果 reconnectSession 已经替换了 s.Handle，说明这是旧会话的退出信号，
	// 不应该修改 session 状态（新会话可能正在运行）。
	s.mu.Lock()
	isStale := s.Handle != handle
	if !isStale {
		s.Status = SessionExited
		s.Summary.Status = string(SessionExited)
		s.Summary.UpdatedAt = time.Now().Unix()
		if exit.Code != nil {
			s.ExitCode = exit.Code
		}
		if exit.Err != nil {
			s.Status = SessionError
			s.Summary.Status = string(SessionError)
		}
	}
	s.mu.Unlock()

	// 关闭捕获的旧 handle（不是 s.Handle，避免关闭新会话）。
	_ = handle.Close()

	// 仅当 handle 未被替换时释放连接池引用。
	// reconnectSession 使用 pool.Reconnect 获取新连接（refCount=1），
	// 如果这里也 Release，新连接的 refCount 会归零被关闭。
	if !isStale {
		m.pool.Release(hostCfg)
	}

	m.mu.RLock()
	cb := m.onUpdate
	m.mu.RUnlock()
	if cb != nil {
		cb(s.ID)
	}
}

func splitSSHOutputLines(chunk []byte) []string {
	text := string(chunk)
	var lines []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			lines = append(lines, text[start:i])
			start = i + 1
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}
