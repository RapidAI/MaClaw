package main

import (
	"fmt"
	"log"
)

type LocalPTYExecutionStrategy struct {
	newPTY func() PTYSession
}

func NewLocalPTYExecutionStrategy(newPTY func() PTYSession) *LocalPTYExecutionStrategy {
	return &LocalPTYExecutionStrategy{newPTY: newPTY}
}

func (s *LocalPTYExecutionStrategy) Start(cmd CommandSpec) (ExecutionHandle, error) {
	if s == nil || s.newPTY == nil {
		return nil, fmt.Errorf("local pty strategy is not configured")
	}

	pty := s.newPTY()
	if pty == nil {
		return nil, fmt.Errorf("local pty session is not available")
	}

	log.Printf("[pty-lifecycle] ▶ Starting PTY process: cmd=%q, args_summary=%s, cwd=%q, cols=%d, rows=%d",
		cmd.Command, summarizeLaunchArgs(cmd.Args), cmd.Cwd, cmd.Cols, cmd.Rows)

	pid, err := pty.Start(cmd)
	if err != nil {
		log.Printf("[pty-lifecycle] ✖ PTY start failed: cmd=%q, error=%v", cmd.Command, err)
		return nil, err
	}

	log.Printf("[pty-lifecycle] ✔ PTY process started: pid=%d, cmd=%q", pid, cmd.Command)

	return &PTYExecutionHandle{
		pid: pid,
		pty: pty,
	}, nil
}

type PTYExecutionHandle struct {
	pid int
	pty PTYSession
}

func (h *PTYExecutionHandle) PID() int {
	if h == nil {
		return 0
	}
	return h.pid
}

func (h *PTYExecutionHandle) Write(data []byte) error {
	if h == nil || h.pty == nil {
		return fmt.Errorf("execution handle is not available")
	}
	return h.pty.Write(data)
}

func (h *PTYExecutionHandle) Interrupt() error {
	if h == nil || h.pty == nil {
		return fmt.Errorf("execution handle is not available")
	}
	return h.pty.Interrupt()
}

func (h *PTYExecutionHandle) Kill() error {
	if h == nil || h.pty == nil {
		return fmt.Errorf("execution handle is not available")
	}
	return h.pty.Kill()
}

func (h *PTYExecutionHandle) Output() <-chan []byte {
	if h == nil || h.pty == nil {
		return nil
	}
	return h.pty.Output()
}

func (h *PTYExecutionHandle) Exit() <-chan PTYExit {
	if h == nil || h.pty == nil {
		return nil
	}
	return h.pty.Exit()
}

func (h *PTYExecutionHandle) Close() error {
	if h == nil || h.pty == nil {
		return fmt.Errorf("execution handle is not available")
	}
	return h.pty.Close()
}
