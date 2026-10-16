//go:build windows

package tinytex

import (
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func applyBatCommandLine(cmd *exec.Cmd, bin string, args []string) {
	if cmd == nil || !strings.EqualFold(filepath.Ext(bin), ".bat") {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CmdLine = batCommandLine(cmd.Path, bin, args)
}

func batCommandLine(cmdExe, bin string, args []string) string {
	parts := make([]string, 0, len(args)+5)
	parts = append(parts, cmdQuote(cmdExe), "/d", "/c", "call", cmdQuote(bin))
	for _, arg := range args {
		parts = append(parts, cmdQuote(arg))
	}
	return strings.Join(parts, " ")
}

func cmdQuote(s string) string {
	// Quote only when cmd would otherwise split the token. A lone % is left
	// as itself on the command line; doubling it is a batch-file rule and
	// would turn 100% into 100.
	if s == "" || strings.ContainsAny(s, " \t\"&|<>^()%") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}
