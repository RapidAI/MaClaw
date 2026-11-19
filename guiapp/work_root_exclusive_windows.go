//go:build windows

package guiapp

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// configureExclusiveGitCommand hides the git console and kills the process
// tree when the timeout fires. CommandContext otherwise terminates only the
// direct child, and Wait can sit behind a grandchild that inherited the pipes.
func configureExclusiveGitCommand(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: _CREATE_NO_WINDOW}
	cmd.Cancel = func() error {
		proc := cmd.Process
		if proc == nil {
			return nil
		}
		// CommandContext treats a nil Cancel error as "this context killed the
		// process", and then a zero exit is reported as the context error.
		// A git that already finished must keep that exit status: the caller
		// deletes the new checkout when it sees a timeout.
		if exclusiveGitProcessExited(proc.Pid) {
			return os.ErrProcessDone
		}
		// taskkill is synchronous inside Cancel, and WaitDelay does not start
		// until Cancel returns. Bound it so a stuck taskkill cannot hold the
		// UI for the rest of the git deadline.
		killCtx, cancel := context.WithTimeout(context.Background(), exclusiveGitWaitDelay)
		defer cancel()
		kill := exec.CommandContext(killCtx, "taskkill", "/F", "/T", "/PID", strconv.Itoa(proc.Pid))
		kill.SysProcAttr = &syscall.SysProcAttr{CreationFlags: _CREATE_NO_WINDOW}
		killErr := kill.Run()
		if killErr != nil && exclusiveGitProcessExited(proc.Pid) {
			return os.ErrProcessDone
		}
		_ = proc.Kill()
		return nil
	}
}

// exclusiveGitProcessExited reports that pid has an exit code. OpenProcess
// failure stays false: an unqueryable live git must still be killed.
func exclusiveGitProcessExited(pid int) bool {
	if pid <= 0 {
		return false
	}
	const stillActive = 259
	h, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code != stillActive
}
