//go:build !windows

package guiapp

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// configureExclusiveGitCommand puts git in its own process group and kills
// that group when the timeout fires. CommandContext otherwise signals only
// the direct child, and Wait can sit behind a grandchild that inherited the pipes.
func configureExclusiveGitCommand(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		proc := cmd.Process
		if proc == nil {
			return nil
		}
		// A nil Cancel error makes CommandContext replace a zero exit with the
		// context error. ErrProcessDone tells it the process had already
		// finished, so a completed worktree add is kept.
		if err := proc.Signal(syscall.Signal(0)); errors.Is(err, os.ErrProcessDone) {
			return os.ErrProcessDone
		}
		_ = syscall.Kill(-proc.Pid, syscall.SIGKILL)
		if err := proc.Kill(); errors.Is(err, os.ErrProcessDone) {
			return os.ErrProcessDone
		}
		return nil
	}
}
