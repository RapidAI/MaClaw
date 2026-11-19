//go:build !windows

package tool

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

func prepareCommandForTreeKill(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func terminateCommandTree(cmd *exec.Cmd) {
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

func commandPIDRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	if runtime.GOOS == "linux" {
		data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if err != nil {
			if os.IsNotExist(err) {
				return false
			}
			return true
		}
		text := string(data)
		end := strings.LastIndexByte(text, ')')
		if end < 0 || end+1 >= len(text) {
			return true
		}
		fields := strings.Fields(text[end+1:])
		if len(fields) == 0 {
			return true
		}
		// Z and X are exited. A zombie still has an exit status for Wait.
		return fields[0] != "Z" && fields[0] != "X"
	}
	err := syscall.Kill(pid, 0)
	return err != syscall.ESRCH
}
