//go:build windows

package tool

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runWindowsShellCommandForTest executes command through NewWindowsShellCommand
// the same way the trusted shell adapters do (UTF-8 env, combined output).
func runWindowsShellCommandForTest(t *testing.T, command, workDir string) (string, string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := NewWindowsShellCommand(ctx, command, workDir)
	cmd.Env = AppendUTF8Env(os.Environ())
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func requirePython(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python"); err != nil {
		t.Skip("python not on PATH")
	}
}

// Regression: production incident shape 1. exec.Command("cmd", "/c", command)
// let Go's EscapeArg rewrite inner quotes to \" which cmd.exe passed through
// literally, so Python received an unterminated `"import` fragment and died
// with SyntaxError. The verbatim CmdLine must keep the quotes intact.
func TestNewWindowsShellCommandPythonInlineQuotes(t *testing.T) {
	requirePython(t)
	stdout, stderr, err := runWindowsShellCommandForTest(t, `python -c "import sys; print('OK')"`, "")
	if err != nil {
		t.Fatalf("python -c with inner quotes failed: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "OK") {
		t.Fatalf("stdout=%q, want OK (stderr=%q)", stdout, stderr)
	}
}

// Regression: production incident shape 3. A quoted absolute path with CJK
// characters used to reach Python with literal quote characters in argv (so
// Python resolved it against the cwd, doubling the path) and the console
// codepage turned the path into GBK mojibake.
func TestNewWindowsShellCommandCJKWorkspaceAndQuotedPath(t *testing.T) {
	requirePython(t)
	base := t.TempDir()
	workDir := filepath.Join(base, "个人介绍")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(workDir, "gen_ppt.py")
	if err := os.WriteFile(script, []byte("print('PPT-OK')\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Shape 3: quoted absolute CJK path, cwd bound to the same CJK workspace.
	stdout, stderr, err := runWindowsShellCommandForTest(t, `python "`+script+`"`, workDir)
	if err != nil {
		t.Fatalf("quoted CJK absolute path failed: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "PPT-OK") {
		t.Fatalf("stdout=%q, want PPT-OK (stderr=%q)", stdout, stderr)
	}

	// Shape 2: cd into the CJK directory && run the script must not trip cmd's
	// "filename, directory name, or volume label syntax is incorrect". Run
	// from the parent so the drive matches (cmd's cd without /d cannot switch
	// drives, same as an interactive session).
	stdout, stderr, err = runWindowsShellCommandForTest(t, `cd "个人介绍" && python gen_ppt.py`, base)
	if err != nil {
		t.Fatalf("cd CJK && python failed: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "PPT-OK") {
		t.Fatalf("stdout=%q, want PPT-OK (stderr=%q)", stdout, stderr)
	}
}

// A command that itself begins with a quoted executable path must survive the
// /s /c "..." wrapping (cmd strips exactly the outer quote pair).
func TestNewWindowsShellCommandLeadingQuotedExecutable(t *testing.T) {
	requirePython(t)
	py, err := exec.LookPath("python")
	if err != nil {
		t.Skip("python not on PATH")
	}
	stdout, stderr, err := runWindowsShellCommandForTest(t, `"`+py+`" --version`, "")
	if err != nil {
		t.Fatalf("leading quoted executable failed: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "Python") {
		t.Fatalf("stdout=%q stderr=%q, want Python version output", stdout, stderr)
	}
}

func TestNewWindowsShellCommandSetsWorkDirAndCmdLine(t *testing.T) {
	workDir := t.TempDir()
	cmd := NewWindowsShellCommand(context.Background(), "echo hi", workDir)
	if cmd.Dir != workDir {
		t.Fatalf("cmd.Dir=%q, want %q", cmd.Dir, workDir)
	}
	line := cmd.SysProcAttr.CmdLine
	if !strings.Contains(line, `/d /s /c "`) || !strings.HasSuffix(line, `echo hi"`) {
		t.Fatalf("CmdLine=%q, want /d /s /c wrapped verbatim command", line)
	}
}
