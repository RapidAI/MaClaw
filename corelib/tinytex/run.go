package tinytex

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	// engineCommandTimeout matches one 编译预览 engine pass. A hung xelatex
	// must not consume the whole directory budget.
	engineCommandTimeout = 2 * time.Minute
	// tlmgrCommandTimeout matches one 编译预览 package install.
	tlmgrCommandTimeout = 5 * time.Minute
)

func reviewedCommandTimeout(bin string) time.Duration {
	base := strings.TrimSuffix(filepath.Base(bin), filepath.Ext(bin))
	if strings.EqualFold(base, "tlmgr") {
		return tlmgrCommandTimeout
	}
	return engineCommandTimeout
}

// runReviewed applies the per-command budget around RunCommand. The directory
// budget still caps the whole build.
func runReviewed(ctx context.Context, bin, dir string, args ...string) (string, int, error) {
	return runLimited(ctx, reviewedCommandTimeout(bin), bin, dir, args...)
}

func runLimited(ctx context.Context, limit time.Duration, bin, dir string, args ...string) (string, int, error) {
	cctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	out, code, err := RunCommand(cctx, bin, dir, args...)
	// RunCommand turns a killed process into a nil error plus an exit code, so
	// the deadline has to be read from the context. A clean exit that wins the
	// race with the timer stays a success.
	if cctx.Err() == context.DeadlineExceeded && ctx.Err() == nil && (err != nil || code != 0) {
		return out, code, context.DeadlineExceeded
	}
	return out, code, err
}

// RunCommand starts one reviewed TeX tool directly. A non-zero exit is the
// process status, not a start failure. A Windows .bat (tlmgr) is launched
// through cmd.exe because the kernel will not execute a batch file as an
// image. call receives that argv as separate arguments, not a command
// string supplied by the model.
func RunCommand(ctx context.Context, bin, dir string, args ...string) (string, int, error) {
	cmdName, cmdArgs := commandArgs(bin, args)
	cmd := tool.CommandContext(ctx, cmdName, cmdArgs...)
	// cmd.exe re-parses its command line. Go's argv quoting does not hide
	// &, %, or parentheses in an unspaced path, so a batch file gets an
	// explicit command line instead.
	applyBatCommandLine(cmd, bin, args)
	if strings.TrimSpace(dir) == "" {
		dir = filepath.Dir(bin)
	}
	cmd.Dir = dir
	binDir := filepath.Dir(bin)
	cmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return string(out), exitErr.ExitCode(), nil
	}
	return string(out), -1, err
}

func commandArgs(bin string, args []string) (string, []string) {
	if runtime.GOOS == "windows" && strings.EqualFold(filepath.Ext(bin), ".bat") {
		// One pre-quoted /c string is quoted again by Go, and cmd /s then
		// treats the backslash-escaped quotes as part of the program name.
		// Separate arguments let Go quote a spaced path once. call is required
		// so cmd runs a batch file instead of searching it as a command token.
		cmdArgs := make([]string, 0, len(args)+4)
		cmdArgs = append(cmdArgs, "/d", "/c", "call", bin)
		cmdArgs = append(cmdArgs, args...)
		return tool.ResolveCmdExe(), cmdArgs
	}
	return bin, args
}
