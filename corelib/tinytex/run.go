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
	// tlmgrSelfUpdateTimeout is one infra update. On Windows, perl only
	// writes temp\updater-w32; tlmgr.bat applies it afterwards, and that
	// copy is larger than one package install.
	tlmgrSelfUpdateTimeout = 30 * time.Minute
)

func reviewedCommandTimeout(bin string, args ...string) time.Duration {
	base := strings.TrimSuffix(filepath.Base(bin), filepath.Ext(bin))
	if strings.EqualFold(base, "tlmgr") {
		if tlmgrUpdatingSelf(args) {
			return tlmgrSelfUpdateTimeout
		}
		return tlmgrCommandTimeout
	}
	return engineCommandTimeout
}

// tlmgrUpdatingSelf reports `tlmgr update --self`. A package whose name
// happens to be --self must not inherit the infra budget.
func tlmgrUpdatingSelf(args []string) bool {
	sawUpdate := false
	for _, arg := range args {
		if arg == "update" {
			sawUpdate = true
			continue
		}
		if sawUpdate && arg == "--self" {
			return true
		}
	}
	return false
}

// RunReviewed is the per-command budget used by 编译预览 and build_verify.
// The caller's context still caps the whole job.
func RunReviewed(ctx context.Context, bin, dir string, args ...string) (string, int, error) {
	return runReviewed(ctx, bin, dir, args...)
}

// runReviewed applies the per-command budget around RunCommand. The directory
// budget still caps the whole build.
func runReviewed(ctx context.Context, bin, dir string, args ...string) (string, int, error) {
	return runLimited(ctx, reviewedCommandTimeout(bin, args...), bin, dir, args...)
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
	return runCommand(ctx, bin, dir, nil, args...)
}

// RunCommandWithPath is RunCommand with directories placed in front of PATH.
// update-tlmgr-latest finds this tree by running kpsewhich, so the TinyTeX
// bin directory has to precede the updater's own folder and any other TeX.
func RunCommandWithPath(ctx context.Context, bin, dir string, prepend []string, args ...string) (string, int, error) {
	return runCommand(ctx, bin, dir, prepend, args...)
}

func runCommand(ctx context.Context, bin, dir string, prepend []string, args ...string) (string, int, error) {
	if err := prepareTlmgrLaunch(bin, args); err != nil {
		return "", -1, err
	}
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
	front := make([]string, 0, len(prepend)+1)
	for _, entry := range prepend {
		entry = strings.TrimSpace(entry)
		if entry != "" {
			front = append(front, entry)
		}
	}
	front = append(front, filepath.Dir(bin))
	cmd.Env = append(os.Environ(), "PATH="+strings.Join(front, string(os.PathListSeparator))+string(os.PathListSeparator)+os.Getenv("PATH"))
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
