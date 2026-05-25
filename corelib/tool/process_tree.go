package tool

import (
	"context"
	"os/exec"
	"time"
)

// PrepareCommandForTreeKill configures cmd so TerminateCommandTree can stop
// shell children started by the command when the context is cancelled.
func PrepareCommandForTreeKill(cmd *exec.Cmd) {
	prepareCommandForTreeKill(cmd)
}

// WaitCommandWithContext waits for cmd and kills the process tree if ctx ends.
// exec.CommandContext only kills the direct child on some platforms, leaving
// shell grandchildren such as ssh.exe alive and waiting for input.
func WaitCommandWithContext(ctx context.Context, cmd *exec.Cmd) error {
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		TerminateCommandTree(cmd)
		select {
		case err := <-done:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		case <-time.After(5 * time.Second):
			return ctx.Err()
		}
	}
}

// TerminateCommandTree stops cmd and descendants best-effort.
func TerminateCommandTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	terminateCommandTree(cmd)
	_ = cmd.Process.Kill()
}
