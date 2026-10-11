//go:build windows

package httpapi

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// newMeetingWorkerCommand runs one operator-configured worker command line
// through cmd.exe with the line preserved verbatim.
//
// exec.Command("cmd", "/C", command) cannot be used: Go's Windows argument
// escaping rewrites every inner quote to \" (syscall.EscapeArg), and cmd.exe
// neither unescapes \" nor strips those quotes, so the worker receives
// mangled arguments — a quoted executable path or a JSON literal argument
// loses its quotes. Handing CreateProcess the exact line via
// SysProcAttr.CmdLine is the same fix corelib/tool applies for model-supplied
// shell commands. /d skips AutoRun; /s /c "<line>" makes cmd strip exactly
// the outer quote pair and execute the rest verbatim.
func newMeetingWorkerCommand(ctx context.Context, command string) *exec.Cmd {
	cmdExe := meetingWorkerCmdExe()
	cmd := exec.CommandContext(ctx, cmdExe)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: `"` + cmdExe + `" /d /s /c "` + command + `"`,
	}
	return cmd
}

// meetingWorkerCmdExe mirrors corelib/tool windowsCmdExe: ComSpec first, then
// SystemRoot, then the well-known system path.
func meetingWorkerCmdExe() string {
	if comspec := os.Getenv("ComSpec"); comspec != "" {
		return comspec
	}
	if sysroot := os.Getenv("SystemRoot"); sysroot != "" {
		return filepath.Join(sysroot, "System32", "cmd.exe")
	}
	return `C:\Windows\System32\cmd.exe`
}
