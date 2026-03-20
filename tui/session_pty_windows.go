//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/UserExistsError/conpty"
	"golang.org/x/sys/windows"
)

// TUIPTYSession 是 TUI 端的 Windows ConPTY 实现。
// 实现 remote.ExecutionHandle 接口。
type TUIPTYSession struct {
	mu       sync.Mutex
	outputCh chan []byte
	exitCh   chan remote.PTYExit
	cpty     *conpty.ConPty
	proc     *os.Process
	started  bool
	closed   bool
	once     sync.Once
}

// NewTUIPTYSession 创建 PTY 会话。
func NewTUIPTYSession() (*TUIPTYSession, error) {
	if !conpty.IsConPtyAvailable() {
		return nil, conpty.ErrConPtyUnsupported
	}
	return &TUIPTYSession{
		outputCh: make(chan []byte, 64),
		exitCh:   make(chan remote.PTYExit, 1),
	}, nil
}

// Start 启动 PTY 进程。
func (p *TUIPTYSession) Start(cmd remote.CommandSpec) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started {
		return 0, fmt.Errorf("pty session already started")
	}

	cmd.Command = strings.TrimSpace(cmd.Command)
	if cmd.Command == "" {
		return 0, fmt.Errorf("pty command is empty")
	}
	if absPath, err := filepath.Abs(cmd.Command); err == nil {
		cmd.Command = absPath
	}

	commandLine := buildTUICommandLine(cmd.Command, cmd.Args)
	env := buildTUIEnvList(cmd.Env)
	width, height := normalizeTUIPTYSize(cmd.Cols, cmd.Rows)

	pty, err := conpty.Start(
		commandLine,
		conpty.ConPtyDimensions(width, height),
		conpty.ConPtyWorkDir(cmd.Cwd),
		conpty.ConPtyEnv(env),
	)
	if err != nil {
		return 0, fmt.Errorf("start conpty: %w", err)
	}

	proc, err := os.FindProcess(pty.Pid())
	if err != nil {
		_ = pty.Close()
		return 0, fmt.Errorf("find process: %w", err)
	}

	p.cpty = pty
	p.proc = proc
	p.started = true

	go p.readLoop(pty)
	go p.waitLoop(pty)

	return pty.Pid(), nil
}

// PID 返回进程 ID。
func (p *TUIPTYSession) PID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.proc != nil {
		return p.proc.Pid
	}
	return 0
}

// Write 向 PTY 写入数据。
func (p *TUIPTYSession) Write(data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.started || p.closed || p.cpty == nil {
		return fmt.Errorf("pty session not running")
	}
	_, err := p.cpty.Write(data)
	return err
}

// Interrupt 发送 Ctrl+C。
func (p *TUIPTYSession) Interrupt() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.started || p.closed || p.cpty == nil {
		return fmt.Errorf("pty session not running")
	}
	_, err := p.cpty.Write([]byte{3})
	return err
}

// Kill 终止进程。
func (p *TUIPTYSession) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.proc == nil {
		return fmt.Errorf("process not found")
	}
	return p.proc.Kill()
}

// Output 返回输出通道。
func (p *TUIPTYSession) Output() <-chan []byte { return p.outputCh }

// Exit 返回退出通道。
func (p *TUIPTYSession) Exit() <-chan remote.PTYExit { return p.exitCh }

// Close 关闭 PTY。
func (p *TUIPTYSession) Close() error {
	p.once.Do(func() {
		p.mu.Lock()
		p.closed = true
		pty := p.cpty
		p.mu.Unlock()
		if pty != nil {
			_ = pty.Close()
		}
	})
	return nil
}

func (p *TUIPTYSession) readLoop(pty *conpty.ConPty) {
	defer close(p.outputCh)
	buf := make([]byte, 4096)
	for {
		n, err := pty.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			p.outputCh <- chunk
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				// read error, let wait loop handle exit
			}
			return
		}
	}
}

func (p *TUIPTYSession) waitLoop(pty *conpty.ConPty) {
	defer close(p.exitCh)
	exitCode, err := pty.Wait(context.Background())
	var codePtr *int
	if err == nil {
		code := int(exitCode)
		codePtr = &code
	}
	p.exitCh <- remote.PTYExit{Code: codePtr, Err: err}
	_ = p.Close()
}

func buildTUICommandLine(command string, args []string) string {
	parts := make([]string, 0, 1+len(args))
	parts = append(parts, windows.EscapeArg(command))
	for _, arg := range args {
		parts = append(parts, windows.EscapeArg(arg))
	}
	return strings.Join(parts, " ")
}

func buildTUIEnvList(env map[string]string) []string {
	base := os.Environ()
	merged := make(map[string]string, len(base)+len(env))
	for _, item := range base {
		if k, v, ok := strings.Cut(item, "="); ok {
			merged[k] = v
		}
	}
	for k, v := range env {
		merged[k] = v
	}
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	items := make([]string, 0, len(keys))
	for _, k := range keys {
		items = append(items, k+"="+merged[k])
	}
	return items
}

func normalizeTUIPTYSize(cols, rows int) (int, int) {
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 32
	}
	return cols, rows
}
