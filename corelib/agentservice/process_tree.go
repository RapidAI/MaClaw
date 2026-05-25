package agentservice

import (
	"context"
	"os/exec"
	"time"
)

func waitCommandWithContext(ctx context.Context, cmd *exec.Cmd) error {
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		terminateCommandTree(cmd)
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

func terminateCommandTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	terminateCommandTreeImpl(cmd)
	_ = cmd.Process.Kill()
}
