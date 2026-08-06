//go:build !windows

package flash

import (
	"fmt"
	"os/exec"
	"syscall"
)

// Give every sidecar its own process group. A negative PID sent to kill(2)
// addresses that group and therefore reaches Python/helper descendants too.
func prepareProcessTree(cmd *exec.Cmd) error {
	if cmd == nil {
		return fmt.Errorf("nil sidecar command")
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return nil
}

func terminateProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil || cmd.Process.Pid <= 0 {
		return fmt.Errorf("sidecar process is unavailable")
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		return err
	}
	return nil
}
