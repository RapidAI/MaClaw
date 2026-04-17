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
	s.mu.Unlock()

	spec.HostConfig.Defaults()

	// 关闭旧 handle
	if s.Handle != nil {
		_ = s.Handle.Close()
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

// WaitForOutput 智能等待命令输出完成。
// 不再盲等固定秒数，而是检测输出是否稳定（连续 stableRounds 次轮询无新输出即认为完成）。
// 对于长时间运行的命令（如 du、find），使用更宽松的稳定阈值避免误判。
// maxWait 是最大等待时间上限。
func (m *SSHSessionManager) WaitForOutput(sessionID string, afterLine int, maxWait time.Duration) ([]string, SessionStatus) {
	s, ok := m.Get(sessionID)
	if !ok {
		return nil, SessionError
	}

	if maxWait <= 0 {
		maxWait = 30 * time.Second
	}

	const pollInterval = 300 * time.Millisecond
	// 提高稳定阈值：连续 8 次（约 2.4s）无新输出才判定完成，
	// 避免 du/find 等命令在扫描大目录时短暂停顿被误判。
	const stableThreshold = 8

	deadline := time.Now().Add(maxWait)
	stableCount := 0
	lastLineCount := afterLine

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
			// 有新输出，重置稳定计数
			stableCount = 0
			lastLineCount = currentCount
		} else {
			stableCount++
			if stableCount >= stableThreshold {
				// 额外检查：如果最后一行看起来像 shell prompt，说明命令确实结束了
				if looksLikeShellPrompt(s.PreviewTail(1)) {
					break
				}
				// 不像 prompt，可能命令还在跑但暂时没输出，再多等几轮
				if stableCount >= stableThreshold+4 {
					break
				}
			}
		}
	}

	return s.NewLinesSince(afterLine)
}

// looksLikeShellPrompt 简单判断最后一行是否像 shell prompt。
// 常见 prompt 模式：以 $ # > % 结尾。
func looksLikeShellPrompt(lines []string) bool {
	if len(lines) == 0 {
		return false
	}
	last := strings.TrimRight(lines[len(lines)-1], " \t")
	if last == "" {
		return false
	}
	lastChar := last[len(last)-1]
	return lastChar == '$' || lastChar == '#' || lastChar == '>' || lastChar == '%'
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
	exitCh := s.Handle.Exit()
	if exitCh == nil {
		return
	}
	exit := <-exitCh

	s.mu.Lock()
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
	s.mu.Unlock()

	_ = s.Handle.Close()
	m.pool.Release(hostCfg)

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
