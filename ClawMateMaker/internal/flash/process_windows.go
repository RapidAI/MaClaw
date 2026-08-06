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

// CREATE_NEW_PROCESS_GROUP makes this task's descendants a distinct kill
// target; CREATE_NO_WINDOW avoids a visible console during all normal Wails
// operations. taskkill /T below then terminates that entire tree.
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
	// taskkill is a Windows system binary. Arguments are fixed and the PID was
	// supplied by os/exec, so this does not introduce a command-injection path.
	kill := exec.Command("taskkill.exe", "/PID", fmt.Sprintf("%d", cmd.Process.Pid), "/T", "/F")
	kill.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	if output, err := kill.CombinedOutput(); err != nil {
		return fmt.Errorf("taskkill: %w (%s)", err, string(output))
	}
	return nil
}
