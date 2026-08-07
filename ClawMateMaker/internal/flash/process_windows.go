//go:build windows

package flash

import (
	"fmt"
	"os/exec"
	"syscall"
)

const (
	createNewProcessGroup = 0x00000200
	createNoWindow        = 0x08000000
)

func prepareProcessTree(cmd *exec.Cmd) error {
	if cmd == nil {
		return fmt.Errorf("nil sidecar command")
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup | createNoWindow}
	return nil
}

func terminateProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil || cmd.Process.Pid <= 0 {
		return fmt.Errorf("sidecar process is unavailable")
	}
	kill := exec.Command("taskkill.exe", "/PID", fmt.Sprintf("%d", cmd.Process.Pid), "/T", "/F")
	kill.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	if output, err := kill.CombinedOutput(); err != nil {
		return fmt.Errorf("taskkill: %w (%s)", err, string(output))
	}
	return nil
}
