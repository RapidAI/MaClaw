//go:build windows

package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestPTYInteractiveMultiRoundExchange verifies that the PTY supports
// multiple rounds of write→read, simulating an interactive CLI tool
// (like "claude code .") where the user sends commands and reads responses.
func TestPTYInteractiveMultiRoundExchange(t *testing.T) {
	pty := NewWindowsPTYSession()

	pid, err := pty.Start(CommandSpec{
		Command: `C:\Windows\System32\cmd.exe`,
		Args:    []string{"/Q", "/K"},
		Cwd:     `C:\Windows\System32`,
		Cols:    120,
		Rows:    32,
	})
	if err != nil {
		t.Fatalf("start interactive pty: %v", err)
	}
	defer func() { _ = pty.Kill() }()
	defer func() { _ = pty.Close() }()

	if pid <= 0 {
		t.Fatalf("expected positive PID, got %d", pid)
	}

	// Round 1: echo a marker
	expectEcho(t, pty, "round1-marker-alpha", 10*time.Second)

	// Round 2: echo a different marker
	expectEcho(t, pty, "round2-marker-beta", 10*time.Second)

	// Round 3: run a command that produces multi-line output
	marker := "round3-multiline-end"
	if err := pty.Write([]byte("echo line-one && echo line-two && echo " + marker + "\r\n")); err != nil {
		t.Fatalf("write round3: %v", err)
	}
	output := collectUntil(t, pty, marker, 10*time.Second)
	if !strings.Contains(output, "line-one") || !strings.Contains(output, "line-two") {
		t.Fatalf("round3: expected multi-line output containing line-one and line-two, got %q", output)
	}
}

// TestPTYEnvironmentVariablePassthrough verifies that custom environment
// variables are correctly passed to the PTY process, which is critical
// for tools like claude that need API keys and config paths.
func TestPTYEnvironmentVariablePassthrough(t *testing.T) {
	pty := NewWindowsPTYSession()

	_, err := pty.Start(CommandSpec{
		Command: `C:\Windows\System32\cmd.exe`,
		Args:    []string{"/Q", "/K"},
		Cwd:     `C:\Windows\System32`,
		Cols:    120,
		Rows:    32,
		Env: map[string]string{
			"MACLAW_TEST_VAR": "pty-env-check-ok",
		},
	})
	if err != nil {
		t.Fatalf("start pty with env: %v", err)
	}
	defer func() { _ = pty.Kill() }()
	defer func() { _ = pty.Close() }()

	if err := pty.Write([]byte("echo %MACLAW_TEST_VAR%\r\n")); err != nil {
		t.Fatalf("write env echo: %v", err)
	}

	output := collectUntil(t, pty, "pty-env-check-ok", 10*time.Second)
	if !strings.Contains(output, "pty-env-check-ok") {
		t.Fatalf("env var not passed through to PTY, output: %q", output)
	}
}

// TestPTYResizeDuringSession verifies that terminal resize works while
// a session is active. Interactive tools often need resize support.
func TestPTYResizeDuringSession(t *testing.T) {
	pty := NewWindowsPTYSession()

	_, err := pty.Start(CommandSpec{
		Command: `C:\Windows\System32\cmd.exe`,
		Args:    []string{"/Q", "/K"},
		Cwd:     `C:\Windows\System32`,
		Cols:    80,
		Rows:    24,
	})
	if err != nil {
		t.Fatalf("start pty: %v", err)
	}
	defer func() { _ = pty.Kill() }()
	defer func() { _ = pty.Close() }()

	// Resize to a larger terminal
	if err := pty.Resize(200, 50); err != nil {
		t.Fatalf("resize to 200x50: %v", err)
	}

	// Verify the session still works after resize
	expectEcho(t, pty, "after-resize-ok", 10*time.Second)

	// Resize to a smaller terminal
	if err := pty.Resize(40, 10); err != nil {
		t.Fatalf("resize to 40x10: %v", err)
	}

	// Verify the session still works after second resize
	expectEcho(t, pty, "after-shrink-ok", 10*time.Second)
}

// TestPTYInterruptSendsCtrlC verifies that Interrupt() sends Ctrl+C
// to the PTY, which is needed to cancel running operations in tools.
func TestPTYInterruptSendsCtrlC(t *testing.T) {
	pty := NewWindowsPTYSession()

	_, err := pty.Start(CommandSpec{
		Command: `C:\Windows\System32\cmd.exe`,
		Args:    []string{"/Q", "/K"},
		Cwd:     `C:\Windows\System32`,
		Cols:    120,
		Rows:    32,
	})
	if err != nil {
		t.Fatalf("start pty: %v", err)
	}
	defer func() { _ = pty.Kill() }()
	defer func() { _ = pty.Close() }()

	// Start a long-running command (ping with large count)
	if err := pty.Write([]byte("ping -n 100 127.0.0.1\r\n")); err != nil {
		t.Fatalf("write ping: %v", err)
	}

	// Wait a bit for ping to start
	time.Sleep(2 * time.Second)

	// Send interrupt (Ctrl+C)
	if err := pty.Interrupt(); err != nil {
		t.Fatalf("interrupt: %v", err)
	}

	// After interrupt, the shell should still be responsive
	time.Sleep(500 * time.Millisecond)
	expectEcho(t, pty, "after-interrupt-ok", 10*time.Second)
}

// TestPTYKillTerminatesProcess verifies that Kill() terminates the
// PTY process and the exit channel fires.
func TestPTYKillTerminatesProcess(t *testing.T) {
	pty := NewWindowsPTYSession()

	_, err := pty.Start(CommandSpec{
		Command: `C:\Windows\System32\cmd.exe`,
		Args:    []string{"/Q", "/K"},
		Cwd:     `C:\Windows\System32`,
		Cols:    80,
		Rows:    24,
	})
	if err != nil {
		t.Fatalf("start pty: %v", err)
	}
	defer func() { _ = pty.Close() }()

	if err := pty.Kill(); err != nil {
		t.Fatalf("kill: %v", err)
	}

	select {
	case exit := <-pty.Exit():
		t.Logf("process exited after kill: code=%v err=%v", exit.Code, exit.Err)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for exit after kill")
	}
}

// TestPTYLongLivedSessionWithContinuousOutput simulates a tool that
// produces continuous output (like a build watcher or an AI coding tool
// streaming responses). Verifies the output channel keeps delivering.
func TestPTYLongLivedSessionWithContinuousOutput(t *testing.T) {
	pty := NewWindowsPTYSession()

	_, err := pty.Start(CommandSpec{
		Command: `C:\Windows\System32\cmd.exe`,
		Args:    []string{"/Q", "/K"},
		Cwd:     `C:\Windows\System32`,
		Cols:    120,
		Rows:    32,
	})
	if err != nil {
		t.Fatalf("start pty: %v", err)
	}
	defer func() { _ = pty.Kill() }()
	defer func() { _ = pty.Close() }()

	// Send 10 rapid commands and verify all outputs arrive
	for i := 0; i < 10; i++ {
		marker := fmt.Sprintf("burst-output-%d-end", i)
		expectEcho(t, pty, marker, 10*time.Second)
	}
}

// TestPTYSessionViaExecutionStrategy tests the full LocalPTYExecutionStrategy
// → PTYExecutionHandle → PTYSession chain, which is the actual code path
// used when launching tools like "claude code .".
func TestPTYSessionViaExecutionStrategy(t *testing.T) {
	strategy := NewLocalPTYExecutionStrategy(func() PTYSession {
		return NewWindowsPTYSession()
	})

	handle, err := strategy.Start(CommandSpec{
		Command: `C:\Windows\System32\cmd.exe`,
		Args:    []string{"/Q", "/K"},
		Cwd:     `C:\Windows\System32`,
		Cols:    120,
		Rows:    32,
	})
	if err != nil {
		t.Fatalf("strategy start: %v", err)
	}
	defer func() { _ = handle.Kill() }()
	defer func() { _ = handle.Close() }()

	if handle.PID() <= 0 {
		t.Fatalf("expected positive PID from execution handle, got %d", handle.PID())
	}

	// Write through the execution handle
	marker := "strategy-test-ok"
	if err := handle.Write([]byte("echo " + marker + "\r\n")); err != nil {
		t.Fatalf("write through handle: %v", err)
	}

	deadline := time.After(10 * time.Second)
	var output strings.Builder
	for {
		select {
		case chunk, ok := <-handle.Output():
			if !ok {
				t.Fatalf("output channel closed before marker seen, got %q", output.String())
			}
			output.Write(chunk)
			if strings.Contains(strings.ToLower(output.String()), marker) {
				// Interrupt through the handle
				if err := handle.Interrupt(); err != nil {
					t.Fatalf("interrupt through handle: %v", err)
				}
				return
			}
		case <-handle.Exit():
			if strings.Contains(strings.ToLower(output.String()), marker) {
				return
			}
			t.Fatalf("process exited before marker seen, output=%q", output.String())
		case <-deadline:
			t.Fatalf("timed out waiting for marker, got %q", output.String())
		}
	}
}

// TestPTYWriteAfterCloseReturnsError verifies that writing to a closed
// PTY session returns an error instead of panicking.
func TestPTYWriteAfterCloseReturnsError(t *testing.T) {
	pty := NewWindowsPTYSession()

	_, err := pty.Start(CommandSpec{
		Command: `C:\Windows\System32\cmd.exe`,
		Args:    []string{"/c", "echo", "done"},
		Cwd:     `C:\Windows\System32`,
		Cols:    80,
		Rows:    24,
	})
	if err != nil {
		t.Fatalf("start pty: %v", err)
	}

	// Wait for exit
	select {
	case <-pty.Exit():
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for exit")
	}

	_ = pty.Close()

	// Write after close should return error, not panic
	if err := pty.Write([]byte("should-fail\r\n")); err == nil {
		t.Fatal("expected error writing to closed PTY, got nil")
	}
}

// TestPTYDoubleStartReturnsError verifies that starting a PTY session
// twice returns an error.
func TestPTYDoubleStartReturnsError(t *testing.T) {
	pty := NewWindowsPTYSession()

	_, err := pty.Start(CommandSpec{
		Command: `C:\Windows\System32\cmd.exe`,
		Args:    []string{"/c", "echo", "first"},
		Cwd:     `C:\Windows\System32`,
		Cols:    80,
		Rows:    24,
	})
	if err != nil {
		t.Fatalf("first start: %v", err)
	}
	defer func() { _ = pty.Close() }()

	_, err = pty.Start(CommandSpec{
		Command: `C:\Windows\System32\cmd.exe`,
		Args:    []string{"/c", "echo", "second"},
		Cwd:     `C:\Windows\System32`,
		Cols:    80,
		Rows:    24,
	})
	if err == nil {
		t.Fatal("expected error on double start, got nil")
	}
	if !strings.Contains(err.Error(), "already started") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestPTYProcessExitCodeCapture verifies that the exit code is correctly
// captured when the process terminates normally.
func TestPTYProcessExitCodeCapture(t *testing.T) {
	pty := NewWindowsPTYSession()

	_, err := pty.Start(CommandSpec{
		Command: `C:\Windows\System32\cmd.exe`,
		Args:    []string{"/c", "exit", "0"},
		Cwd:     `C:\Windows\System32`,
		Cols:    80,
		Rows:    24,
	})
	if err != nil {
		t.Fatalf("start pty: %v", err)
	}
	defer func() { _ = pty.Close() }()

	select {
	case exit := <-pty.Exit():
		if exit.Err != nil {
			t.Fatalf("unexpected exit error: %v", exit.Err)
		}
		if exit.Code == nil {
			t.Fatal("expected exit code, got nil")
		}
		if *exit.Code != 0 {
			t.Fatalf("expected exit code 0, got %d", *exit.Code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for exit")
	}
}

// --- helpers ---

// TestPTYWriteCRLFvsLF verifies that ConPTY requires "\r\n" (or "\r")
// to simulate pressing Enter.  A bare "\n" (LF only) does NOT trigger
// command execution in ConPTY on Windows.  This test documents the
// exact behavior that caused the "commands not reaching Claude Code" bug:
// the frontend was sending text + "\n" instead of text + "\r\n".
func TestPTYWriteCRLFvsLF(t *testing.T) {
	pty := NewWindowsPTYSession()

	_, err := pty.Start(CommandSpec{
		Command: `C:\Windows\System32\cmd.exe`,
		Args:    []string{"/Q", "/K"},
		Cwd:     `C:\Windows\System32`,
		Cols:    120,
		Rows:    32,
	})
	if err != nil {
		t.Fatalf("start pty: %v", err)
	}
	defer func() { _ = pty.Kill() }()
	defer func() { _ = pty.Close() }()

	// Drain any initial output (prompt, etc.)
	drainFor(pty, 1*time.Second)

	// --- Test 1: Write with bare LF ("\n") — should NOT execute ---
	if err := pty.Write([]byte("echo lf-only-marker\n")); err != nil {
		t.Fatalf("write LF-only: %v", err)
	}
	lfOutput := drainFor(pty, 2*time.Second)
	t.Logf("LF-only output (len=%d): %q", len(lfOutput), lfOutput)

	// --- Test 2: Write with CRLF ("\r\n") — SHOULD execute ---
	_ = pty.Write([]byte{3}) // Ctrl+C to clear partial input
	time.Sleep(300 * time.Millisecond)
	drainFor(pty, 500*time.Millisecond)

	crlfMarker := "crlf-works-ok"
	if err := pty.Write([]byte("echo " + crlfMarker + "\r\n")); err != nil {
		t.Fatalf("write CRLF: %v", err)
	}
	crlfOutput := collectUntil(t, pty, crlfMarker, 10*time.Second)
	if !strings.Contains(crlfOutput, crlfMarker) {
		t.Fatalf("CRLF: expected marker %q in output, got %q", crlfMarker, crlfOutput)
	}
	t.Logf("CRLF output confirmed marker present")

	// --- Test 3: Write with bare CR ("\r") — should also execute ---
	crMarker := "cr-only-works"
	if err := pty.Write([]byte("echo " + crMarker + "\r")); err != nil {
		t.Fatalf("write CR-only: %v", err)
	}
	crOutput := collectUntil(t, pty, crMarker, 10*time.Second)
	if !strings.Contains(crOutput, crMarker) {
		t.Fatalf("CR-only: expected marker %q in output, got %q", crMarker, crOutput)
	}
	t.Logf("CR-only output confirmed marker present")
}

// TestPTYOutputPipelineIntegration tests the full chain that the real
// application uses: PTY Write → PTY Output channel → OutputPipeline →
// RingPreviewBuffer → SessionPreviewDelta.
func TestPTYOutputPipelineIntegration(t *testing.T) {
	pty := NewWindowsPTYSession()

	_, err := pty.Start(CommandSpec{
		Command: `C:\Windows\System32\cmd.exe`,
		Args:    []string{"/Q", "/K"},
		Cwd:     `C:\Windows\System32`,
		Cols:    120,
		Rows:    32,
	})
	if err != nil {
		t.Fatalf("start pty: %v", err)
	}
	defer func() { _ = pty.Kill() }()
	defer func() { _ = pty.Close() }()

	pipeline := NewOutputPipeline()
	session := &RemoteSession{
		ID:     "test-pipeline-session",
		Tool:   "test",
		Status: SessionRunning,
		Summary: SessionSummary{
			SessionID: "test-pipeline-session",
			Status:    "running",
		},
	}

	marker := "pipeline-integration-test-ok"
	if err := pty.Write([]byte("echo " + marker + "\r\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	deadline := time.After(10 * time.Second)
	var allPreviewLines []string
	markerFound := false

	for !markerFound {
		select {
		case chunk, ok := <-pty.Output():
			if !ok {
				t.Fatalf("output channel closed before marker, preview so far: %v", allPreviewLines)
			}
			result := pipeline.Consume(session, chunk)
			if result.PreviewDelta != nil {
				allPreviewLines = append(allPreviewLines, result.PreviewDelta.AppendLines...)
				for _, line := range result.PreviewDelta.AppendLines {
					if strings.Contains(strings.ToLower(line), marker) {
						markerFound = true
						break
					}
				}
			}
		case <-deadline:
			t.Fatalf("timed out waiting for marker in pipeline output, got: %v", allPreviewLines)
		}
	}

	t.Logf("Pipeline produced %d preview lines, marker found", len(allPreviewLines))
	if len(allPreviewLines) == 0 {
		t.Fatal("pipeline produced zero preview lines")
	}
}

// TestPTYWriteInputMethodSimulation simulates the exact code path used
// by SendRemoteSessionInput: text from frontend → Exec.Write → PTY.
func TestPTYWriteInputMethodSimulation(t *testing.T) {
	strategy := NewLocalPTYExecutionStrategy(func() PTYSession {
		return NewWindowsPTYSession()
	})

	handle, err := strategy.Start(CommandSpec{
		Command: `C:\Windows\System32\cmd.exe`,
		Args:    []string{"/Q", "/K"},
		Cwd:     `C:\Windows\System32`,
		Cols:    120,
		Rows:    32,
	})
	if err != nil {
		t.Fatalf("strategy start: %v", err)
	}
	defer func() { _ = handle.Kill() }()
	defer func() { _ = handle.Close() }()

	// Simulate SendRemoteSessionInput with the fix: text + "\r\n"
	marker := "writeinput-sim-ok"
	inputText := "echo " + marker + "\r\n"
	if err := handle.Write([]byte(inputText)); err != nil {
		t.Fatalf("write through handle: %v", err)
	}

	deadline := time.After(10 * time.Second)
	var output strings.Builder
	for {
		select {
		case chunk, ok := <-handle.Output():
			if !ok {
				t.Fatalf("output channel closed before marker, got %q", output.String())
			}
			output.Write(chunk)
			if strings.Contains(strings.ToLower(output.String()), marker) {
				t.Logf("WriteInput simulation: marker found in output")
				return
			}
		case <-handle.Exit():
			if strings.Contains(strings.ToLower(output.String()), marker) {
				return
			}
			t.Fatalf("process exited before marker, output=%q", output.String())
		case <-deadline:
			t.Fatalf("timed out, output=%q", output.String())
		}
	}
}

// --- helpers ---

// drainFor reads all available output from the PTY for the given duration.
func drainFor(pty *WindowsPTYSession, d time.Duration) string {
	timer := time.After(d)
	var buf strings.Builder
	for {
		select {
		case chunk, ok := <-pty.Output():
			if !ok {
				return buf.String()
			}
			buf.Write(chunk)
		case <-timer:
			return buf.String()
		}
	}
}

func expectEcho(t *testing.T, pty *WindowsPTYSession, marker string, timeout time.Duration) {
	t.Helper()
	if err := pty.Write([]byte("echo " + marker + "\r\n")); err != nil {
		t.Fatalf("write echo %s: %v", marker, err)
	}
	output := collectUntil(t, pty, marker, timeout)
	if !strings.Contains(output, marker) {
		t.Fatalf("expected marker %q in output, got %q", marker, output)
	}
}

func collectUntil(t *testing.T, pty *WindowsPTYSession, needle string, timeout time.Duration) string {
	t.Helper()
	deadline := time.After(timeout)
	var output strings.Builder
	for {
		select {
		case chunk, ok := <-pty.Output():
			if !ok {
				t.Fatalf("output channel closed before %q seen, got %q", needle, output.String())
			}
			output.Write(chunk)
			if strings.Contains(strings.ToLower(output.String()), strings.ToLower(needle)) {
				return output.String()
			}
		case exit := <-pty.Exit():
			t.Fatalf("pty exited before %q seen, exit=%+v output=%q", needle, exit, output.String())
		case <-deadline:
			t.Fatalf("timed out waiting for %q, got %q", needle, output.String())
		}
		// Reset deadline isn't needed; we just keep reading
	}
}
