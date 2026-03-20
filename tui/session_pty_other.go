//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// TUIPTYSession 是非 Windows 平台的 PTY 实现（使用 os/exec）。
type TUIPTYSession struct {
	mu       sync.Mutex
	outputCh chan []byte
	exitCh   chan remote.PTYExit
	cmd      *exec.Cmd
	started  bool
	closed   bool
	once     sync.Once
}

// NewTUIPTYSession 创建 PTY 会话。
func NewTUIPTYSession() (*TUIPTYSession, error) {
	return &TUIPTYSession{
		outputCh: make(chan []byte, 64),
		exitCh:   make(chan remote.PTYExit, 1),
	}, nil
}

// Start 启动进程（非 Windows 使用 exec.Command）。
func (p *TUIPTYSession) Start(cmd remote.CommandSpec) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started {
		return 0, fmt.Errorf("pty session already started")
	}

	c := exec.Command(cmd.Command, cmd.Args...)
	c.Dir = cmd.Cwd
	for k, v := range cmd.Env {
		c.Env = append(c.Env, k+"="+v)
	}
	if len(c.Env) > 0 {
		c.Env = append(os.Environ(), c.Env...)
	}

	stdout, err := c.StdoutPipe()
	if err != nil {
		return 0, err
	}
	c.Stderr = c.Stdout

	if err := c.Start(); err != nil {
		return 0, err
	}

	p.cmd = c
	p.started = true

	go func() {
		defer close(p.outputCh)
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				p.outputCh <- chunk
			}
			if err != nil {
				return
			}
		}
	}()

	go func() {
		defer close(p.exitCh)
		err := c.Wait()
		var codePtr *int
		if c.ProcessState != nil {
			code := c.ProcessState.ExitCode()
			codePtr = &code
		}
		p.exitCh <- remote.PTYExit{Code: codePtr, Err: err}
	}()

	return c.Process.Pid, nil
}

func (p *TUIPTYSession) PID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil && p.cmd.Process != nil {
		return p.cmd.Process.Pid
	}
	return 0
}

func (p *TUIPTYSession) Write(data []byte) error {
	return fmt.Errorf("write not supported on non-Windows PTY stub")
}

func (p *TUIPTYSession) Interrupt() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil && p.cmd.Process != nil {
		return p.cmd.Process.Signal(os.Interrupt)
	}
	return fmt.Errorf("process not found")
}

func (p *TUIPTYSession) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil && p.cmd.Process != nil {
		return p.cmd.Process.Kill()
	}
	return fmt.Errorf("process not found")
}

func (p *TUIPTYSession) Output() <-chan []byte       { return p.outputCh }
func (p *TUIPTYSession) Exit() <-chan remote.PTYExit  { return p.exitCh }

func (p *TUIPTYSession) Close() error {
	p.once.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.mu.Unlock()
	})
	return nil
}
