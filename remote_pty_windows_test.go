//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWindowsPTYSessionSmoke(t *testing.T) {
	pty := NewWindowsPTYSession()

	_, err := pty.Start(CommandSpec{
		Command: `C:\Windows\System32\cmd.exe`,
		Args:    []string{"/c", "echo", "hello-from-conpty"},
		Cwd:     `C:\Windows\System32`,
		Cols:    80,
		Rows:    24,
	})
	if err != nil {
		t.Fatalf("start pty: %v", err)
	}
	defer func() { _ = pty.Close() }()

	deadline := time.After(10 * time.Second)
	var output strings.Builder

	for {
		select {
		case chunk, ok := <-pty.Output():
			if ok {
				output.Write(chunk)
				if strings.Contains(strings.ToLower(output.String()), "hello-from-conpty") {
					return
				}
			}
		case exit := <-pty.Exit():
			if strings.Contains(strings.ToLower(output.String()), "hello-from-conpty") {
				return
			}
			t.Fatalf("pty exited before expected output, exit=%+v output=%q", exit, output.String())
		case <-deadline:
			t.Fatalf("timed out waiting for conpty output, got %q", output.String())
		}
	}
}

func TestWindowsPTYSessionInteractiveWrite(t *testing.T) {
	pty := NewWindowsPTYSession()

	_, err := pty.Start(CommandSpec{
		Command: `C:\Windows\System32\cmd.exe`,
		Args:    []string{"/Q", "/K"},
		Cwd:     `C:\Windows\System32`,
		Cols:    80,
		Rows:    24,
	})
	if err != nil {
		t.Fatalf("start interactive pty: %v", err)
	}
	defer func() { _ = pty.Kill() }()
	defer func() { _ = pty.Close() }()

	if err := pty.Write([]byte("echo hello-interactive-conpty\r\n")); err != nil {
		t.Fatalf("write interactive command: %v", err)
	}

	deadline := time.After(10 * time.Second)
	var output strings.Builder

	for {
		select {
		case chunk, ok := <-pty.Output():
			if !ok {
				t.Fatalf("pty output channel closed before interactive echo was observed; output=%q", output.String())
			}
			output.Write(chunk)
			if strings.Contains(strings.ToLower(output.String()), "hello-interactive-conpty") {
				return
			}
		case exit := <-pty.Exit():
			t.Fatalf("pty exited before interactive echo was observed, exit=%+v output=%q", exit, output.String())
		case <-deadline:
			t.Fatalf("timed out waiting for interactive conpty output, got %q", output.String())
		}
	}
}

func TestBuildEnvListMergesProcessEnv(t *testing.T) {
	t.Setenv("MACLAW_TEST_BASE", "from-process")

	items := buildEnvList(map[string]string{
		"MACLAW_TEST_EXTRA": "from-extra",
		"MACLAW_TEST_BASE":  "overridden",
	})

	values := map[string]string{}
	for _, item := range items {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) == 2 {
			values[parts[0]] = parts[1]
		}
	}

	if values["MACLAW_TEST_EXTRA"] != "from-extra" {
		t.Fatalf("MACLAW_TEST_EXTRA = %q, want %q", values["MACLAW_TEST_EXTRA"], "from-extra")
	}
	if values["MACLAW_TEST_BASE"] != "overridden" {
		t.Fatalf("MACLAW_TEST_BASE = %q, want %q", values["MACLAW_TEST_BASE"], "overridden")
	}
	if values["PATH"] == "" {
		t.Fatal("PATH should be preserved when merging env")
	}
}

func TestWindowsPTYSessionStartValidatesCommandAndCwd(t *testing.T) {
	pty := NewWindowsPTYSession()

	if _, err := pty.Start(CommandSpec{}); err == nil || !strings.Contains(err.Error(), "pty command is empty") {
		t.Fatalf("empty command error = %v", err)
	}

	if _, err := pty.Start(CommandSpec{
		Command: filepath.Join(t.TempDir(), "missing.exe"),
	}); err == nil || !strings.Contains(err.Error(), "pty command not accessible") {
		t.Fatalf("missing command error = %v", err)
	}

	cmdFile := filepath.Join(t.TempDir(), "cmd.exe")
	if err := os.WriteFile(cmdFile, []byte("stub"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := pty.Start(CommandSpec{
		Command: cmdFile,
		Cwd:     filepath.Join(t.TempDir(), "missing-cwd"),
	}); err == nil || !strings.Contains(err.Error(), "pty working directory not accessible") {
		t.Fatalf("missing cwd error = %v", err)
	}
}

func TestBuildWindowsCommandLineEscapesArgs(t *testing.T) {
	got := buildWindowsCommandLine(`C:\Program Files\Claude\claude.exe`, []string{
		`--flag`,
		`value with spaces`,
		`quote"inside`,
	})

	if !strings.Contains(got, `"C:\Program Files\Claude\claude.exe"`) {
		t.Fatalf("command line %q should quote executable path", got)
	}
	if !strings.Contains(got, `"value with spaces"`) {
		t.Fatalf("command line %q should quote spaced argument", got)
	}
	if !strings.Contains(got, `quote\"inside`) {
		t.Fatalf("command line %q should escape embedded quotes", got)
	}
}

func TestNormalizePTYSizeUsesDefaults(t *testing.T) {
	cols, rows := normalizePTYSize(0, -1)
	if cols != 120 || rows != 32 {
		t.Fatalf("normalizePTYSize(0, -1) = (%d, %d), want (120, 32)", cols, rows)
	}

	cols, rows = normalizePTYSize(200, 50)
	if cols != 200 || rows != 50 {
		t.Fatalf("normalizePTYSize(200, 50) = (%d, %d), want (200, 50)", cols, rows)
	}
}
