package tinytex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestReviewedCommandTimeoutMatchesThePreviewBudget(t *testing.T) {
	if reviewedCommandTimeout("xelatex.exe") != 2*time.Minute {
		t.Fatalf("engine=%s", reviewedCommandTimeout("xelatex.exe"))
	}
	if reviewedCommandTimeout(filepath.Join("tex", "tlmgr.bat"), "install", "amsmath") != 5*time.Minute {
		t.Fatalf("tlmgr=%s", reviewedCommandTimeout("tlmgr.bat", "install", "amsmath"))
	}
	if reviewedCommandTimeout(filepath.Join("tex", "tlmgr.bat"), "update", "--self") != 30*time.Minute {
		t.Fatalf("self=%s", reviewedCommandTimeout("tlmgr.bat", "update", "--self"))
	}
	if reviewedCommandTimeout(filepath.Join("tex", "tlmgr.bat"), "install", "--self") != 5*time.Minute {
		t.Fatalf("package named --self took the infra budget")
	}
}

func TestRunLimitedStopsAHungTool(t *testing.T) {
	dir := t.TempDir()
	bin := "sleep"
	args := []string{"30"}
	if runtime.GOOS == "windows" {
		bin = filepath.Join(dir, "hang.bat")
		script := "@echo off\r\n:loop\r\ngoto loop\r\n"
		if err := os.WriteFile(bin, []byte(script), 0o644); err != nil {
			t.Fatal(err)
		}
		args = nil
	}
	start := time.Now()
	_, _, err := runLimited(context.Background(), 500*time.Millisecond, bin, dir, args...)
	if elapsed := time.Since(start); elapsed > 8*time.Second {
		t.Fatalf("ran for %s, err=%v", elapsed, err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
}

func TestRunCommandClearsAReadOnlyTlmgrUpdaterBeforeTheBatchStarts(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("updater-w32 is the Windows tlmgr.bat lock")
	}
	dist := t.TempDir()
	bin := filepath.Join(dist, "bin", "windows")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dist, "temp", "updater-w32")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("stale"), 0o444); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dist, "seen.txt")
	script := filepath.Join(bin, "tlmgr.bat")
	body := strings.Join([]string{
		"@echo off",
		"if exist \"" + stale + "\" (",
		"  echo STILL>\"" + logPath + "\"",
		"  exit /b 1",
		")",
		"echo GONE>\"" + logPath + "\"",
		"exit /b 0",
		"",
	}, "\r\n")
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, code, err := RunCommand(context.Background(), script, bin, "update", "--self")
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != "GONE" {
		t.Fatalf("batch saw the leftover updater: %q", raw)
	}
}

func TestRunCommandStartsWhenOnlyTheRenamedUpdaterScriptIsLocked(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("updater-w32.bat is the Windows rename target")
	}
	dist := t.TempDir()
	bin := filepath.Join(dist, "bin", "windows")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	renamed := filepath.Join(dist, "temp", "updater-w32.bat")
	if err := os.MkdirAll(filepath.Dir(renamed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(renamed, []byte("ran"), 0o644); err != nil {
		t.Fatal(err)
	}
	held, err := os.Open(renamed)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	logPath := filepath.Join(dist, "seen.txt")
	script := filepath.Join(bin, "tlmgr.bat")
	body := "@echo off\r\necho RAN>\"" + logPath + "\"\r\nexit /b 0\r\n"
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, code, err := RunCommand(context.Background(), script, bin, "install", "scheme-small")
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil || strings.TrimSpace(string(raw)) != "RAN" {
		t.Fatalf("batch did not start: %q err=%v", raw, err)
	}
	if _, statErr := os.Stat(renamed); statErr != nil {
		t.Fatalf("locked updater-w32.bat = %v", statErr)
	}
}

func TestRunCommandRefusesSelfUpdateWhenTheRenamedUpdaterScriptIsLocked(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("updater-w32.bat is the Windows rename target")
	}
	dist := t.TempDir()
	bin := filepath.Join(dist, "bin", "windows")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	renamed := filepath.Join(dist, "temp", "updater-w32.bat")
	if err := os.MkdirAll(filepath.Dir(renamed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(renamed, []byte("ran"), 0o644); err != nil {
		t.Fatal(err)
	}
	held, err := os.Open(renamed)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	logPath := filepath.Join(dist, "seen.txt")
	script := filepath.Join(bin, "tlmgr.bat")
	body := "@echo off\r\necho RAN>\"" + logPath + "\"\r\nexit /b 0\r\n"
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err = RunCommand(context.Background(), script, bin, "update", "--self")
	if !errors.Is(err, ErrStaleUpdater) {
		t.Fatalf("err=%v", err)
	}
	if _, statErr := os.Stat(logPath); !os.IsNotExist(statErr) {
		t.Fatalf("self-update started with a locked updater-w32.bat (%v)", statErr)
	}
}

func TestRunCommandKeepsAPercentEncodedRepositoryThroughTheBatchParser(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("tlmgr.bat rebuilds arguments with shift")
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "args.txt")
	script := filepath.Join(dir, "tlmgr.bat")
	body := strings.Join([]string{
		"@echo off",
		"setlocal enableextensions",
		"if x == x%1 goto noargs",
		"set args=",
		":rebuildargs",
		"shift",
		"if x == x%0 goto nomoreargs",
		"set args=%args% %0",
		"goto rebuildargs",
		":nomoreargs",
		">>\"" + logPath + "\" echo %args%",
		"exit /b 0",
		":noargs",
		">>\"" + logPath + "\" echo NOARGS",
		"exit /b 1",
		"",
	}, "\r\n")
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	const repo = "https://mirror.example/CTAN%20files/systems/texlive/tlnet"
	_, code, err := RunCommand(context.Background(), script, dir, "option", "repository", repo)
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	// cmdQuote wraps a token that contains %. The batch stores those quotes;
	// the later perl line strips them. %20 must stay %20 through the one-pass
	// `set args=%args% %0` rebuild.
	got := strings.TrimRight(string(raw), "\r\n")
	want := ` option repository "` + repo + `"`
	if got != want {
		t.Fatalf("rebuilt args = %q", raw)
	}
}

func TestRunCommandPreservesTlmgrSelfUpdateThroughTheBatchParser(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("tlmgr.bat rebuilds arguments with shift")
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "args.txt")
	script := filepath.Join(dir, "tlmgr.bat")
	// Same rebuild tlmgr.bat uses: shift, then read %0, so call's script name
	// is dropped and --self is a real argument. `if x == x%0` must not treat
	// --self as the end of the list.
	body := strings.Join([]string{
		"@echo off",
		"setlocal enableextensions",
		"if x == x%1 goto noargs",
		"set args=",
		":rebuildargs",
		"shift",
		"if x == x%0 goto nomoreargs",
		"if gui == %0 goto guibailout",
		"if -gui == %0 goto guibailout",
		"if --gui == %0 goto guibailout",
		"set args=%args% %0",
		"goto rebuildargs",
		":nomoreargs",
		">>\"" + logPath + "\" echo %args%",
		"exit /b 0",
		":noargs",
		">>\"" + logPath + "\" echo NOARGS",
		"exit /b 1",
		":guibailout",
		">>\"" + logPath + "\" echo GUI",
		"exit /b 1",
		"",
	}, "\r\n")
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, code, err := RunCommand(context.Background(), script, dir, "update", "--self")
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != "update --self" {
		t.Fatalf("rebuilt args = %q", raw)
	}
}

func TestRunCommandWithPathPutsTheRequestedDirectoryFirst(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("cmd.exe path order")
	}
	dir := t.TempDir()
	texBin := filepath.Join(dir, "texbin")
	if err := os.MkdirAll(texBin, 0o755); err != nil {
		t.Fatal(err)
	}
	outFile := filepath.Join(dir, "path.txt")
	script := filepath.Join(dir, "showpath.bat")
	body := "@echo off\r\n> \"" + outFile + "\" echo %PATH%\r\nexit /b 0\r\n"
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, code, err := RunCommandWithPath(context.Background(), script, dir, []string{texBin})
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	raw, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	first := strings.TrimSpace(strings.Split(string(raw), ";")[0])
	if !strings.EqualFold(first, texBin) {
		t.Fatalf("first PATH entry = %q, want %q\n%s", first, texBin, raw)
	}
}

func TestRunCommandReturnsTheBatchExitCode(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("cmd.exe batch quoting")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "tool.bat")
	body := "@echo off\r\nexit /b 3\r\n"
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code, err := RunCommand(context.Background(), script, dir)
	if err != nil || code != 3 {
		t.Fatalf("out=%q code=%d err=%v", out, code, err)
	}
}

func TestRunCommandExecutesABatchFile(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("cmd.exe batch quoting")
	}
	for _, name := range []string{"plain", "path with spaces", "path (with) parens", "a&b", "100%"} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), name)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			script := filepath.Join(dir, "tool.bat")
			body := "@echo off\r\necho ok %1\r\nexit /b 0\r\n"
			if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			out, code, err := RunCommand(context.Background(), script, dir, "amsmath")
			if err != nil || code != 0 || !strings.Contains(out, "ok amsmath") {
				t.Fatalf("out=%q code=%d err=%v", out, code, err)
			}
		})
	}
}
