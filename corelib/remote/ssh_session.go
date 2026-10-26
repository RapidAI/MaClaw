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
	mu            sync.Mutex
	client        *ssh.Client
	session       *ssh.Session
	stdin         io.WriteCloser
	outputCh      chan []byte
	exitCh        chan PTYExit
	readWg        sync.WaitGroup // 等待 readLoop goroutines 结束
	started       bool
	closed        bool
	transportDead bool // 这次 PTY 上的 keepalive 已经以传输错误结束
	verdictAt     time.Time
	verdictAlive  bool
	verdictValid  bool
	once          sync.Once
	hostID        string
	stopKA        chan struct{} // 停止 session 级 keepalive
}

// NewSSHPTYSession 基于已有的 SSH 连接创建 PTY 会话。
func NewSSHPTYSession(client *ssh.Client, hostID string) *SSHPTYSession {
	return &SSHPTYSession{
		client: client,
		// 较大缓冲：高吞吐命令（ps/grep/du）短时突发输出时减少丢包。
		// 仍满时 readLoop 优先丢弃旧数据，保留最新（含 EXIT/prompt）。
		outputCh: make(chan []byte, 256),
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

// SSHClient is the connection this PTY was started on. The pool owns it.
// Closing the PTY does not close the connection.
func (s *SSHPTYSession) SSHClient() *ssh.Client {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client
}

// IsAlive 报告这条 PTY 的传输还可以继续用。
// 未启动、已关闭、keepalive 以传输错误结束，或同一次请求超过
// sshProbeStuckAfter 仍无应答，返回 false。复用方重连的是这一条会话。
// 更短的未完成等待仍是同一次请求，返回 true。传输还在，就是这一条
// 会话；shell 正忙不是再拨一条的理由。
func (s *SSHPTYSession) IsAlive() bool {
	s.mu.Lock()
	if !s.started || s.closed || s.client == nil || s.transportDead {
		s.mu.Unlock()
		return false
	}
	client := s.client
	s.mu.Unlock()

	ok, finished, running, _ := awaitSSHProbe(client, sshProbeWait)
	// The shell can exit while this wait runs. A closed PTY is not alive,
	// even when the TCP keepalive succeeded.
	s.mu.Lock()
	unusable := !s.started || s.closed || s.client == nil || s.transportDead
	s.mu.Unlock()
	if unusable {
		return false
	}
	if finished && !ok {
		s.markTransportDead()
		return false
	}
	return !sshProbeCountsAsFailure(finished, ok, running)
}

func (s *SSHPTYSession) markTransportDead() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.transportDead = true
	s.mu.Unlock()
}

// TransportFailed 报告这条 PTY 的传输已经结束并且失败。
// 未启动、已关闭，或 keepalive 以传输错误结束，都是失败。
// 请求还没结束时不是失败，哪怕已经超过 sshProbeStuckAfter：
// 连接池的 keepalive 会关掉那条连接，探测随之以错误结束。
// 在关掉之前就解除目录绑定，会让仍开着的会话旁边再被 connect 出一条。
func (s *SSHPTYSession) TransportFailed() bool {
	s.mu.Lock()
	if !s.started || s.closed || s.client == nil || s.transportDead {
		s.mu.Unlock()
		return true
	}
	client := s.client
	s.mu.Unlock()

	ok, finished, _, _ := awaitSSHProbe(client, sshProbeWait)
	s.mu.Lock()
	unusable := !s.started || s.closed || s.client == nil || s.transportDead
	s.mu.Unlock()
	if unusable {
		return true
	}
	if finished && !ok {
		s.markTransportDead()
		return true
	}
	return false
}

// ObservedTransportFailure reports a keepalive that has already finished
// with a transport error. It does not start a probe. A session with no
// probe yet, or one whose request is still in flight, returns false.
func (s *SSHPTYSession) ObservedTransportFailure() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	if s.transportDead {
		s.mu.Unlock()
		return true
	}
	if !s.started || s.closed || s.client == nil {
		s.mu.Unlock()
		return false
	}
	client := s.client
	s.mu.Unlock()
	ok, finished, present := peekSSHProbe(client)
	if present && finished && !ok {
		s.markTransportDead()
		return true
	}
	return false
}

// RememberTransportVerdict stores the latest catalog read on this handle.
// The verdict dies with the handle, so a closed PTY is not kept alive by a
// process-wide cache. ttl is applied by TransportVerdict.
func (s *SSHPTYSession) RememberTransportVerdict(alive bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.verdictAt = time.Now()
	s.verdictAlive = alive
	s.verdictValid = true
	s.mu.Unlock()
}

// TransportVerdict returns the catalog read stored on this handle.
// ok is false when nothing is stored or ttl has elapsed.
func (s *SSHPTYSession) TransportVerdict(ttl time.Duration) (alive bool, ok bool) {
	if s == nil {
		return false, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.verdictValid || time.Since(s.verdictAt) > ttl {
		s.verdictValid = false
		return false, false
	}
	return s.verdictAlive, true
}

// Client 返回底层池化 SSH 连接，供非 PTY 的 per-command exec channel 复用。
// 会话已关闭或尚未启动时返回 nil；连接生命周期仍由 SSHPool 管理。
func (s *SSHPTYSession) Client() *ssh.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.closed {
		return nil
	}
	return s.client
}

// Write 向远程 shell 写入数据。
func (s *SSHPTYSession) Write(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.closed || s.transportDead || s.stdin == nil {
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
// 与 SSHPool 的连接级 keepalive 共用同一次探测，避免超时后的 SendRequest
// 占着全局锁，把活连接计成断线。这里不关闭连接。
func (s *SSHPTYSession) sessionKeepalive(interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
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
			ok, finished, running, stopped, probe := s.readSSHProbe(client)
			if stopped {
				return
			}
			if finished && ok {
				continue
			}
			if !sshProbeCountsAsFailure(finished, ok, running) {
				continue
			}
			if finished && !ok {
				// 传输错误说明这条连接的 mux 已经结束。记在句柄上，
				// 目录和情境提示不用再等探测表里的那条结果过期。
				s.markTransportDead()
				return
			}
			// 同一次请求已经超过 sshProbeStuckAfter，但还没结束。等的是这一次，
			// 不另起探测。传输错误记在句柄上；迟到的成功不记失败，句柄仍可用。
			// 卡住的当下不记失败，目录继续绑定这条还开着的 PTY。
			// 连接池会关掉半开连接，这次探测随后结束。循环到此停止。
			s.recordStuckProbe(probe)
			return
		}
	}
}

// recordStuckProbe waits for the keepalive that has already run past
// sshProbeStuckAfter. The failure is recorded only when that request
// finishes with a transport error. Close unblocks the wait.
func (s *SSHPTYSession) recordStuckProbe(probe *sharedSSHProbe) {
	if probe == nil {
		return
	}
	select {
	case <-s.stopKA:
		return
	case <-probe.done:
	}
	if !probe.ok {
		s.markTransportDead()
	}
}

// readSSHProbe waits for the shared keepalive, or returns early when the
// session is closed. stopped means the session asked this loop to end.
// probe is the request that was waited on.
func (s *SSHPTYSession) readSSHProbe(client *ssh.Client) (ok bool, finished bool, running time.Duration, stopped bool, probe *sharedSSHProbe) {
	type result struct {
		ok       bool
		finished bool
		running  time.Duration
		probe    *sharedSSHProbe
	}
	// Buffer the result so a close during the wait does not leave the
	// goroutine blocked, and so it never writes this function's results
	// after return.
	got := make(chan result, 1)
	go func() {
		ok, finished, running, probe := awaitSSHProbe(client, sshProbeWait)
		got <- result{ok: ok, finished: finished, running: running, probe: probe}
	}()
	select {
	case <-s.stopKA:
		return false, false, 0, true, nil
	case result := <-got:
		return result.ok, result.finished, result.running, false, result.probe
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
			// 优先投递最新数据。通道满时丢弃最旧 chunk，保留最新
			// （EXIT 标记 / prompt 通常在输出尾部）。
			select {
			case s.outputCh <- chunk:
			default:
				select {
				case <-s.outputCh: // 丢弃最旧
				default:
				}
				select {
				case s.outputCh <- chunk:
				default:
					// 极端并发下仍满则放弃本次（极少见）
				}
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
