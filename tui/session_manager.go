package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// TUISession 表示一个 TUI 管理的本地会话。
type TUISession struct {
	mu            sync.Mutex
	ID            string
	Spec          remote.LaunchSpec
	Status        remote.SessionStatus
	Summary       remote.SessionSummary
	PreviewLines  []string
	Events        []remote.ImportantEvent
	Handle        remote.ExecutionHandle
	Pipeline      *remote.OutputPipeline
	CreatedAt     time.Time
	ExitCode      *int
	LastOutputAt  time.Time            // 最后一次收到输出的时间
	StallState    remote.StallState    // 停滞检测状态
	NudgeCount    int                  // 已发送的 nudge 次数
	StepProgress  string               // 当前步骤进度文本
}

// TUISessionManager 管理 TUI 端的本地会话生命周期。
// 复用 corelib/remote 的 LaunchSpec、OutputPipeline、SessionMonitor。
type TUISessionManager struct {
	mu              sync.RWMutex
	sessions        map[string]*TUISession
	onUpdate        func(sessionID string) // 会话状态变更回调
	progressTracker *remote.ProgressTracker
}

// NewTUISessionManager 创建会话管理器。
func NewTUISessionManager() *TUISessionManager {
	return &TUISessionManager{
		sessions:        make(map[string]*TUISession),
		progressTracker: remote.NewProgressTracker(),
	}
}

// SetOnUpdate 设置会话状态变更回调。
func (m *TUISessionManager) SetOnUpdate(fn func(sessionID string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onUpdate = fn
}

// Create 创建并启动一个新会话。
func (m *TUISessionManager) Create(spec remote.LaunchSpec) (*TUISession, error) {
	now := time.Now()
	sessionID := fmt.Sprintf("tui_sess_%d", now.UnixNano())
	spec.SessionID = sessionID

	cmd := remote.CommandSpec{
		Command: spec.BinaryName,
		Args:    buildSessionArgs(spec),
		Cwd:     spec.ProjectPath,
		Env:     spec.Env,
		Cols:    120,
		Rows:    40,
	}

	// 使用本地 PTY 执行
	pty, err := NewTUIPTYSession()
	if err != nil {
		return nil, fmt.Errorf("创建 PTY 失败: %w", err)
	}

	pid, err := pty.Start(cmd)
	if err != nil {
		_ = pty.Close()
		return nil, fmt.Errorf("启动会话失败: %w", err)
	}

	session := &TUISession{
		ID:   sessionID,
		Spec: spec,
		Status: remote.SessionRunning,
		Summary: remote.SessionSummary{
			SessionID: sessionID,
			Tool:      spec.Tool,
			Title:     spec.Title,
			Status:    string(remote.SessionRunning),
			UpdatedAt: now.Unix(),
		},
		Handle:    pty,
		CreatedAt: now,
	}
	_ = pid // PID 可用于日志

	m.mu.Lock()
	m.sessions[sessionID] = session
	m.mu.Unlock()

	// 启动输出循环和退出循环
	go m.runOutputLoop(session)
	go m.runExitLoop(session)

	return session, nil
}

// buildSessionArgs 根据 LaunchSpec 构建命令行参数。
func buildSessionArgs(spec remote.LaunchSpec) []string {
	return buildToolArgs(spec.Tool, spec.ProjectPath, spec.YoloMode, spec.AdminMode)
}

// Get 获取会话。
func (m *TUISessionManager) Get(sessionID string) (*TUISession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[sessionID]
	return s, ok
}

// List 列出所有会话。
func (m *TUISessionManager) List() []*TUISession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*TUISession, 0, len(m.sessions))
	for _, s := range m.sessions {
		result = append(result, s)
	}
	return result
}

// WriteInput 向会话写入输入。
func (m *TUISessionManager) WriteInput(sessionID, text string) error {
	s, ok := m.Get(sessionID)
	if !ok {
		return fmt.Errorf("会话 %s 不存在", sessionID)
	}
	if s.Handle == nil {
		return fmt.Errorf("会话 %s 无执行句柄", sessionID)
	}
	return s.Handle.Write([]byte(text + "\n"))
}

// Interrupt 中断会话。
func (m *TUISessionManager) Interrupt(sessionID string) error {
	s, ok := m.Get(sessionID)
	if !ok {
		return fmt.Errorf("会话 %s 不存在", sessionID)
	}
	if s.Handle == nil {
		return fmt.Errorf("会话 %s 无执行句柄", sessionID)
	}
	return s.Handle.Interrupt()
}

// Kill 终止会话。
func (m *TUISessionManager) Kill(sessionID string) error {
	s, ok := m.Get(sessionID)
	if !ok {
		return fmt.Errorf("会话 %s 不存在", sessionID)
	}
	if s.Handle == nil {
		return fmt.Errorf("会话 %s 无执行句柄", sessionID)
	}
	return s.Handle.Kill()
}

// GetSessionStatus 实现 remote.SessionProvider 接口。
func (m *TUISessionManager) GetSessionStatus(sessionID string) (remote.SessionStatus, bool) {
	s, ok := m.Get(sessionID)
	if !ok {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Status, true
}

func (m *TUISessionManager) runOutputLoop(s *TUISession) {
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
		lines := splitOutputLines(chunk)
		now := time.Now()
		// Track tool_use steps from PTY output
		for _, line := range lines {
			m.progressTracker.ConsumeLine(s.ID, line)
		}
		s.mu.Lock()
		s.PreviewLines = append(s.PreviewLines, lines...)
		// 限制预览行数
		if len(s.PreviewLines) > 1000 {
			s.PreviewLines = s.PreviewLines[len(s.PreviewLines)-1000:]
		}
		s.LastOutputAt = now
		s.Summary.UpdatedAt = now.Unix()
		// Update step progress
		if sp := m.progressTracker.FormatProgress(s.ID); sp != "" {
			s.StepProgress = sp
			s.Summary.StepProgress = sp
			if prog := m.progressTracker.GetProgress(s.ID); prog != nil {
				s.Summary.StepCount = prog.StepCount
			}
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

func (m *TUISessionManager) runExitLoop(s *TUISession) {
	if s.Handle == nil {
		return
	}
	exitCh := s.Handle.Exit()
	if exitCh == nil {
		return
	}
	exit := <-exitCh

	s.mu.Lock()
	s.Status = remote.SessionExited
	s.Summary.Status = string(remote.SessionExited)
	s.Summary.UpdatedAt = time.Now().Unix()
	if exit.Code != nil {
		s.ExitCode = exit.Code
	}
	if exit.Err != nil {
		s.Status = remote.SessionError
		s.Summary.Status = string(remote.SessionError)
	}
	s.mu.Unlock()

	_ = s.Handle.Close()

	// Clean up progress tracking for this session.
	m.progressTracker.Reset(s.ID)

	m.mu.RLock()
	cb := m.onUpdate
	m.mu.RUnlock()
	if cb != nil {
		cb(s.ID)
	}
}

func splitOutputLines(chunk []byte) []string {
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
