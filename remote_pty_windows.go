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

	"github.com/UserExistsError/conpty"
	"golang.org/x/sys/windows"
)

type WindowsPTYSession struct {
	mu sync.Mutex

	outputCh chan []byte
	exitCh   chan PTYExit

	conpty    *conpty.ConPty
	proc      *os.Process
	started   bool
	closed    bool
	closeOnce sync.Once
}

func NewWindowsPTYSession() *WindowsPTYSession {
	return &WindowsPTYSession{
		outputCh: make(chan []byte, 64),
		exitCh:   make(chan PTYExit, 1),
	}
}

func (p *WindowsPTYSession) Start(cmd CommandSpec) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started {
		return 0, fmt.Errorf("pty session already started")
	}

	if !conpty.IsConPtyAvailable() {
		return 0, conpty.ErrConPtyUnsupported
	}
	cmd.Command = strings.TrimSpace(cmd.Command)
	if cmd.Command == "" {
		return 0, fmt.Errorf("pty command is empty")
	}
	commandPath, err := filepath.Abs(cmd.Command)
	if err == nil {
		cmd.Command = commandPath
	}
	if info, err := os.Stat(cmd.Command); err != nil {
		return 0, fmt.Errorf("pty command not accessible: %w", err)
	} else if info.IsDir() {
		return 0, fmt.Errorf("pty command points to a directory: %s", cmd.Command)
	}
	if strings.TrimSpace(cmd.Cwd) != "" {
		if info, err := os.Stat(cmd.Cwd); err != nil {
			return 0, fmt.Errorf("pty working directory not accessible: %w", err)
		} else if !info.IsDir() {
			return 0, fmt.Errorf("pty working directory is not a directory: %s", cmd.Cwd)
		}
	}

	commandLine := buildWindowsCommandLine(cmd.Command, cmd.Args)
	env := buildEnvList(cmd.Env)
	width, height := normalizePTYSize(cmd.Cols, cmd.Rows)

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

	p.conpty = pty
	p.proc = proc
	p.started = true

	p.startReadLoopLocked(pty)
	p.startWaitLoopLocked(pty)

	return pty.Pid(), nil
}

func (p *WindowsPTYSession) Write(data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.started || p.closed {
		return fmt.Errorf("pty session not running")
	}
	if p.conpty == nil {
		return fmt.Errorf("conpty session not available")
	}

	_, err := p.conpty.Write(data)
	return err
}

func (p *WindowsPTYSession) Interrupt() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.started || p.closed {
		return fmt.Errorf("pty session not running")
	}

	if p.conpty == nil {
		return fmt.Errorf("conpty session not available")
	}

	_, err := p.conpty.Write([]byte{3})
	return err
}

func (p *WindowsPTYSession) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.proc == nil {
		return fmt.Errorf("process not found")
	}
	return p.proc.Kill()
}

func (p *WindowsPTYSession) Resize(cols, rows int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.started || p.closed {
		return fmt.Errorf("pty session not running")
	}
	if p.conpty == nil {
		return fmt.Errorf("conpty session not available")
	}

	width, height := normalizePTYSize(cols, rows)
	return p.conpty.Resize(width, height)
}

func (p *WindowsPTYSession) Close() error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		pty := p.conpty
		p.mu.Unlock()

		if pty != nil {
			_ = pty.Close()
		}
	})
	return nil
}

func (p *WindowsPTYSession) Output() <-chan []byte {
	return p.outputCh
}

func (p *WindowsPTYSession) Exit() <-chan PTYExit {
	return p.exitCh
}

func (p *WindowsPTYSession) startReadLoopLocked(pty *conpty.ConPty) {
	go func() {
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
					// Let wait loop report the terminal exit state; read loop only stops streaming.
				}
				return
			}
		}
	}()
}

func (p *WindowsPTYSession) startWaitLoopLocked(pty *conpty.ConPty) {
	go func() {
		defer close(p.exitCh)

		exitCode, err := pty.Wait(context.Background())
		var codePtr *int
		if err == nil {
			code := int(exitCode)
			codePtr = &code
		}

		p.exitCh <- PTYExit{
			Code: codePtr,
			Err:  err,
		}

		_ = p.Close()
	}()
}

func buildWindowsCommandLine(command string, args []string) string {
	parts := make([]string, 0, 1+len(args))
	parts = append(parts, windows.EscapeArg(command))
	for _, arg := range args {
		parts = append(parts, windows.EscapeArg(arg))
	}
	return strings.Join(parts, " ")
}

func buildEnvList(env map[string]string) []string {
	base := os.Environ()
	merged := make(map[string]string, len(base)+len(env))
	for _, item := range base {
		if k, v, ok := strings.Cut(item, "="); ok {
			merged[k] = v
		}
	}
	for key, value := range env {
		merged[key] = value
	}

	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	items := make([]string, 0, len(keys))
	for _, key := range keys {
		items = append(items, key+"="+merged[key])
	}
	return items
}

func normalizePTYSize(cols, rows int) (int, int) {
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 32
	}
	return cols, rows
}
