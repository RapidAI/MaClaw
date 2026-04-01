package remote

import (
	"fmt"
	"io"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHPTYSession 通过 SSH channel 实现 ExecutionHandle 接口，
// 提供与本地 ConPTY 完全一致的交互模型。
type SSHPTYSession struct {
	mu       sync.Mutex
	client   *ssh.Client
	session  *ssh.Session
	stdin    io.WriteCloser
	outputCh chan []byte
	exitCh   chan PTYExit
	readWg   sync.WaitGroup // 等待 readLoop goroutines 结束
	started  bool
	closed   bool
	once     sync.Once
	hostID   string
	stopKA   chan struct{} // 停止 session 级 keepalive
}

// NewSSHPTYSession 基于已有的 SSH 连接创建 PTY 会话。
func NewSSHPTYSession(client *ssh.Client, hostID string) *SSHPTYSession {
	return &SSHPTYSession{
		client:   client,
		outputCh: make(chan []byte, 64),
		exitCh:   make(chan PTYExit, 1),
		hostID:   hostID,
		stopKA:   make(chan struct{}),
	}
}

// Start 在远程主机上启动一个 PTY shell 会话。
func (s *SSHPTYSession) Start(spec SSHSessionSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return fmt.Errorf("ssh session already started")
	}

	session, err := s.client.NewSession()
	if err != nil {
		return fmt.Errorf("new ssh session: %w", err)
	}

	// 设置环境变量
	for k, v := range spec.Env {
		_ = session.Setenv(k, v) // 部分 sshd 不允许 setenv，忽略错误
	}

	cols, rows := normalizeSSHPTYSize(spec.Cols, spec.Rows)

	// 请求 PTY
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		_ = session.Close()
		return fmt.Errorf("request pty: %w", err)
	}

	// 获取 stdin pipe
	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		return fmt.Errorf("stdin pipe: %w", err)
	}

	// 获取 stdout pipe
	stdout, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		return fmt.Errorf("stdout pipe: %w", err)
	}

	// stderr 合并到 stdout（PTY 模式下通常已合并，但显式处理）
	stderr, err := session.StderrPipe()
	if err != nil {
		_ = session.Close()
		return fmt.Errorf("stderr pipe: %w", err)
	}

	// 启动 shell
	if spec.InitialCommand != "" {
		err = session.Start(spec.InitialCommand)
	} else {
		err = session.Shell()
	}
	if err != nil {
		_ = session.Close()
		return fmt.Errorf("start shell: %w", err)
	}

	s.session = session
	s.stdin = stdin
	s.started = true

	s.readWg.Add(2)
	go s.readLoop(stdout)
	go s.readLoop(stderr)
	go s.waitLoop()
	go s.sessionKeepalive(spec.HostConfig.KeepaliveInterval)

	return nil
}

// PID 返回 0（远程进程无本地 PID）。
func (s *SSHPTYSession) PID() int { return 0 }

// IsAlive 检查 SSH 会话是否仍然存活。
func (s *SSHPTYSession) IsAlive() bool {
	s.mu.Lock()
	if !s.started || s.closed || s.client == nil {
		s.mu.Unlock()
		return false
	}
	client := s.client
	s.mu.Unlock()
	// 在锁外做网络 I/O，避免阻塞其他操作
	_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
	return err == nil
}

// Write 向远程 shell 写入数据。
func (s *SSHPTYSession) Write(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.closed || s.stdin == nil {
		return fmt.Errorf("ssh session not running")
	}
	_, err := s.stdin.Write(data)
	return err
}

// Interrupt 发送 Ctrl+C 到远程 shell。
func (s *SSHPTYSession) Interrupt() error {
	return s.Write([]byte{3}) // ETX = Ctrl+C
}

// Kill 通过发送 SIGKILL 信号关闭远程会话。
func (s *SSHPTYSession) Kill() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session == nil {
		return fmt.Errorf("no ssh session")
	}
	// 发送 signal 到远程进程
	return s.session.Signal(ssh.SIGKILL)
}

// Output 返回输出通道。
func (s *SSHPTYSession) Output() <-chan []byte { return s.outputCh }

// Exit 返回退出通道。
func (s *SSHPTYSession) Exit() <-chan PTYExit { return s.exitCh }

// Close 关闭 SSH 会话（不关闭底层连接，连接由 SSHPool 管理）。
func (s *SSHPTYSession) Close() error {
	s.once.Do(func() {
		s.mu.Lock()
		s.closed = true
		session := s.session
		stdin := s.stdin
		s.mu.Unlock()
		// 停止 session 级 keepalive
		close(s.stopKA)
		if stdin != nil {
			_ = stdin.Close()
		}
		if session != nil {
			_ = session.Close()
		}
	})
	return nil
}

// sessionKeepalive 定期通过底层 ssh.Client 发送 keepalive 请求，
// 防止 SSH channel 因空闲被服务端或中间网络设备断开。
// 与 SSHPool 的连接级 keepalive 互补：pool 保活 TCP 连接，这里保活 session channel。
func (s *SSHPTYSession) sessionKeepalive(interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	failCount := 0
	for {
		select {
		case <-s.stopKA:
			return
		case <-ticker.C:
			s.mu.Lock()
			closed := s.closed
			client := s.client
			s.mu.Unlock()
			if closed || client == nil {
				return
			}
			// SendRequest 在连接级别发心跳，同时也能保持 channel 活跃
			_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				failCount++
				// 连续 3 次失败才放弃，避免瞬时网络抖动误判
				if failCount >= 3 {
					return
				}
			} else {
				failCount = 0
			}
		}
	}
}

func (s *SSHPTYSession) readLoop(r io.Reader) {
	defer s.readWg.Done()
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			select {
			case s.outputCh <- chunk:
			default:
				// 输出通道满，丢弃旧数据避免阻塞
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *SSHPTYSession) waitLoop() {
	defer close(s.exitCh)

	if s.session == nil {
		s.readWg.Wait()
		close(s.outputCh)
		return
	}
	err := s.session.Wait()

	// 等待 readLoop 全部退出后再关闭 outputCh，避免 send on closed channel
	s.readWg.Wait()
	close(s.outputCh)
	var codePtr *int
	if err == nil {
		code := 0
		codePtr = &code
	} else if exitErr, ok := err.(*ssh.ExitError); ok {
		code := exitErr.ExitStatus()
		codePtr = &code
	}
	s.exitCh <- PTYExit{Code: codePtr, Err: err}
}

func normalizeSSHPTYSize(cols, rows int) (int, int) {
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 40
	}
	return cols, rows
}
