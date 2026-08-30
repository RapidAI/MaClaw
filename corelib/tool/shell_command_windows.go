//go:build windows

package tool

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// NewWindowsShellCommand returns an *exec.Cmd that runs command through
// cmd.exe with the model-supplied command line preserved verbatim.
//
// exec.Command("cmd", "/c", command) cannot be used: Go's Windows argument
// escaping rewrites every inner quote to \" (syscall.EscapeArg), and cmd.exe
// neither unescapes \" nor strips those quotes, so they leak into the executed
// line and into child argv — python -c "import pptx; ..." loses its quotes and
// a quoted absolute path arrives at the child with literal quote characters
// (which Python then resolves against the cwd, doubling the path). Setting
// SysProcAttr.CmdLine hands CreateProcess the exact command line instead — the
// same pattern as gui restartApp — and keeps non-ASCII (e.g. CJK) workspace
// paths intact because the line stays UTF-16 end to end with no codepage
// conversion in between.
//
// /d skips AutoRun; /s /c "<line>" makes cmd strip exactly the outer quote
// pair and execute the rest verbatim, so a command that itself begins with a
// quoted path is still parsed correctly. Child stdio encoding is handled by
// the callers (AppendUTF8Env); a chcp 65001 prefix was tried and rejected —
// it does not switch cmd's own messages to UTF-8 and emits a spurious
// "path not found" line on stderr for every command.
func NewWindowsShellCommand(ctx context.Context, command, workDir string) *exec.Cmd {
	cmdExe := windowsCmdExe()
	cmd := exec.CommandContext(ctx, cmdExe)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: `"` + cmdExe + `" /d /s /c "` + command + `"`,
	}
	if strings.TrimSpace(workDir) != "" {
		cmd.Dir = workDir
	}
	return cmd
}

// windowsCmdExe mirrors gui resolveCmdExe: ComSpec first, then SystemRoot,
// then the well-known system path.
func windowsCmdExe() string {
	if comspec := os.Getenv("ComSpec"); comspec != "" {
		return comspec
	}
	if sysroot := os.Getenv("SystemRoot"); sysroot != "" {
		return filepath.Join(sysroot, "System32", "cmd.exe")
	}
	return `C:\Windows\System32\cmd.exe`
}
