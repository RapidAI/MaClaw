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
	if reviewedCommandTimeout(filepath.Join("tex", "tlmgr.bat")) != 5*time.Minute {
		t.Fatalf("tlmgr=%s", reviewedCommandTimeout("tlmgr.bat"))
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
