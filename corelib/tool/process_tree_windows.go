//go:build windows

package tool

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// windowsStillActive is the GetExitCodeProcess value for a process that has
// not exited. It is not an exit code this waiter treats as completion.
const windowsStillActive = 259

func prepareCommandForTreeKill(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= _CREATE_NO_WINDOW
}

func terminateCommandTree(cmd *exec.Cmd) {
	killer := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", cmd.Process.Pid))
	HideCommandWindow(killer)
	_ = killer.Run()
}

func commandPIDRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		// A pid that is already gone has exited. Any other open failure stays
		// "running" so a stall kill is not reported as the script's status.
		return !errors.Is(err, windows.ERROR_INVALID_PARAMETER)
	}
	defer windows.CloseHandle(handle)
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return true
	}
	return code == windowsStillActive
}
