package remote

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestWaitForOutput_ExitMarkerCompletesQuickly verifies that seeing EXIT: N
// ends the wait without needing the full maxWait window.
func TestWaitForOutput_ExitMarkerCompletesQuickly(t *testing.T) {
	mgr := NewSSHSessionManager(nil)
	s := &SSHManagedSession{
		ID:           "sess-exit",
		Status:       SessionRunning,
		PreviewLines: []string{},
		CreatedAt:    time.Now(),
	}
	mgr.mu.Lock()
	mgr.sessions[s.ID] = s
	mgr.mu.Unlock()

	// Simulate command echo + result + EXIT after a short delay.
	go func() {
		time.Sleep(200 * time.Millisecond)
		s.mu.Lock()
		s.PreviewLines = append(s.PreviewLines,
			"eval \"$(echo 'xxx' | base64 -d)\"",
			"hello from remote",
			"EXIT: 0",
			"root@host:~# ",
		)
		s.mu.Unlock()
	}()

	start := time.Now()
	lines, status := mgr.WaitForOutput(s.ID, 0, 30*time.Second)
	elapsed := time.Since(start)

	if status != SessionRunning {
		t.Fatalf("status=%s, want running", status)
	}
	if !hasCommandExitMarker(lines) {
		t.Fatalf("expected EXIT marker in lines: %v", lines)
	}
	// Must complete well under the old silence-based early exit (~5s) AND under maxWait.
	if elapsed > 5*time.Second {
		t.Fatalf("WaitForOutput took %v, expected quick completion on EXIT marker", elapsed)
	}
	if elapsed < 150*time.Millisecond {
		t.Fatalf("WaitForOutput returned too fast (%v) — race?", elapsed)
	}
}

// TestWaitForOutput_DoesNotEarlyCompleteOnSilenceAlone ensures slow-start commands
// are not treated as done after only a few seconds of silence without prompt/EXIT.
// This was the primary cause of intermittent SSH "instability" (command still
// running while the next one was written into the same PTY).
func TestWaitForOutput_DoesNotEarlyCompleteOnSilenceAlone(t *testing.T) {
	mgr := NewSSHSessionManager(nil)
	s := &SSHManagedSession{
		ID:     "sess-slow",
		Status: SessionRunning,
		// Only command echo — no EXIT, no prompt (slow command still running)
		PreviewLines: []string{
			`eval "$(echo 'Y2QgL3RtcA==' | base64 -d)"`,
		},
		CreatedAt: time.Now(),
	}
	mgr.mu.Lock()
	mgr.sessions[s.ID] = s
	mgr.mu.Unlock()

	// Late real output + EXIT arrives after the old 5s false-completion window.
	go func() {
		time.Sleep(6 * time.Second)
		s.mu.Lock()
		s.PreviewLines = append(s.PreviewLines, "late output line", "EXIT: 0")
		s.mu.Unlock()
	}()

	start := time.Now()
	lines, _ := mgr.WaitForOutput(s.ID, 0, 12*time.Second)
	elapsed := time.Since(start)

	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "late output line") {
		t.Fatalf("expected late output to be captured, got %q (elapsed=%v)", joined, elapsed)
	}
	if !hasCommandExitMarker(lines) {
		t.Fatalf("expected EXIT marker, got %q", joined)
	}
	// Must have waited for the late output, not returned at ~5s.
	if elapsed < 5*time.Second {
		t.Fatalf("returned too early (%v) — silence false completion regression", elapsed)
	}
}

// TestWaitForOutput_TimeoutInterruptsWithoutSignal verifies Ctrl+C path notes timeout.
func TestWaitForOutput_TimeoutInterruptsWithoutSignal(t *testing.T) {
	mgr := NewSSHSessionManager(nil)
	s := &SSHManagedSession{
		ID:           "sess-timeout",
		Status:       SessionRunning,
		PreviewLines: []string{"still running echo..."},
		CreatedAt:    time.Now(),
		// Handle nil → Interrupt skipped, still should mark timeout message
	}
	mgr.mu.Lock()
	mgr.sessions[s.ID] = s
	mgr.mu.Unlock()

	start := time.Now()
	lines, _ := mgr.WaitForOutput(s.ID, 0, 800*time.Millisecond)
	elapsed := time.Since(start)

	if elapsed < 700*time.Millisecond {
		t.Fatalf("expected to wait near maxWait, got %v", elapsed)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "命令执行超时") {
		t.Fatalf("expected timeout notice, got %q", joined)
	}
}

// TestWaitForOutputContextCancellationReturnsWithoutTimeoutSideEffect covers
// the runtime-child path. A cancelled reader must stop waiting quickly, but
// must not turn its local cancellation into the timeout/Ctrl+C behavior that
// could interrupt another caller sharing the same SSH session.
func TestShellPromptReturnedSinceAcceptsASinglePromptLine(t *testing.T) {
	s := &SSHManagedSession{
		PreviewLines: []string{"root@host:~#"},
	}
	if !s.shellPromptReturnedSince(0) {
		t.Fatal("a single prompt line is the shell coming back")
	}
	s.PreviewLines = []string{"still running"}
	if s.shellPromptReturnedSince(0) {
		t.Fatal("command output was treated as a prompt")
	}
	s.PreviewLines = []string{"still running"}
	s.pendingLine = "root@host:~#"
	if !s.shellPromptReturnedSince(1) {
		t.Fatal("pending prompt after the command was ignored")
	}
	s.pendingLine = ""
	s.PreviewLines = []string{"user@host:~# leaked-from-command", "still running"}
	if s.shellPromptReturnedSince(0) {
		t.Fatal("an earlier prompt-shaped line counted after later output")
	}
	s.PreviewLines = []string{"still running", "root@host:~#"}
	if !s.shellPromptReturnedSince(1) {
		t.Fatal("prompt after the mark was ignored")
	}
	if s.shellPromptReturnedSince(2) {
		t.Fatal("prompt before the mark counted as the shell coming back")
	}
}

func TestPromptAppearedAfterIgnoresTheSamePendingLine(t *testing.T) {
	s := &SSHManagedSession{pendingLine: "root@host:~#"}
	if s.promptAppearedAfter(0, "root@host:~#") {
		t.Fatal("the prompt already on screen counted as coming back")
	}
	s.pendingLine = "root@host:~# "
	if s.promptAppearedAfter(0, "root@host:~#") {
		t.Fatal("a cursor space on the same prompt counted as coming back")
	}
	s.pendingLine = "\x1b[32mroot@host:~#\x1b[0m"
	if s.promptAppearedAfter(0, "root@host:~#") {
		t.Fatal("the same prompt with color codes counted as coming back")
	}
	s.pendingLine = "root@other:~#"
	if !s.promptAppearedAfter(0, "root@host:~#") {
		t.Fatal("a different pending prompt was ignored")
	}
	s.pendingLine = ""
	s.PreviewLines = []string{"root@host:~#"}
	if s.promptAppearedAfter(0, "root@host:~#") {
		t.Fatal("committing the pending prompt counted as coming back")
	}
	s.PreviewLines = []string{"^C", "root@host:~#"}
	if !s.promptAppearedAfter(0, "root@host:~#") {
		t.Fatal("prompt printed after interrupt output was ignored")
	}
	s.PreviewLines = []string{"user@host:~# leaked"}
	if s.promptAppearedAfter(0, "") {
		t.Fatal("command output was treated as a new prompt")
	}
}

func TestWaitForOutputContextDeadlineKeepsFinishedPrompt(t *testing.T) {
	mgr := NewSSHSessionManager(nil)
	s := &SSHManagedSession{
		ID:           "sess-deadline-done",
		Status:       SessionRunning,
		PreviewLines: []string{"ok", "root@host:~#"},
		CreatedAt:    time.Now(),
	}
	mgr.mu.Lock()
	mgr.sessions[s.ID] = s
	mgr.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	lines, _ := mgr.WaitForOutputContext(ctx, s.ID, 0, 10*time.Second)
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "命令执行超时") || strings.Contains(joined, SSHWaitTimeoutShellBusyNotice) {
		t.Fatalf("finished command was reported as a timeout: %q", joined)
	}
	if !strings.Contains(joined, "ok") || !strings.Contains(joined, "root@host:~#") {
		t.Fatalf("finished output was dropped: %q", joined)
	}
}

func TestWaitForOutputContextDeadlineReportsTimeout(t *testing.T) {
	mgr := NewSSHSessionManager(nil)
	s := &SSHManagedSession{
		ID:           "sess-deadline",
		Status:       SessionRunning,
		PreviewLines: []string{"still running"},
		CreatedAt:    time.Now(),
	}
	mgr.mu.Lock()
	mgr.sessions[s.ID] = s
	mgr.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	lines, _ := mgr.WaitForOutputContext(ctx, s.ID, 0, 10*time.Second)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("deadline did not stop the wait promptly: %v", elapsed)
	}
	if joined := strings.Join(lines, "\n"); !strings.Contains(joined, SSHWaitTimeoutShellBusyNotice) {
		t.Fatalf("deadline without a recovered prompt = %q, want the shell-busy notice", joined)
	}
}

func TestWaitForOutputContextCancellationReturnsWithoutTimeoutSideEffect(t *testing.T) {
	mgr := NewSSHSessionManager(nil)
	s := &SSHManagedSession{
		ID:           "sess-context-cancel",
		Status:       SessionRunning,
		PreviewLines: []string{"command is still running"},
		CreatedAt:    time.Now(),
	}
	mgr.mu.Lock()
	mgr.sessions[s.ID] = s
	mgr.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(80*time.Millisecond, cancel)
	start := time.Now()
	lines, status := mgr.WaitForOutputContext(ctx, s.ID, 0, 10*time.Second)
	elapsed := time.Since(start)

	if status != SessionRunning {
		t.Fatalf("status=%s, want running", status)
	}
	if elapsed > time.Second {
		t.Fatalf("context cancellation did not stop wait promptly: %v", elapsed)
	}
	if joined := strings.Join(lines, "\n"); strings.Contains(joined, "命令执行超时") {
		t.Fatalf("context cancellation must not report timeout/interrupt: %q", joined)
	}
}

// TestWaitForOutput_IgnoresPriorCommandExitMarker ensures residual EXIT/prompt
// from a previous command (before afterLine) cannot complete the new wait early.
func TestWaitForOutput_IgnoresPriorCommandExitMarker(t *testing.T) {
	mgr := NewSSHSessionManager(nil)
	s := &SSHManagedSession{
		ID:     "sess-prior-exit",
		Status: SessionRunning,
		PreviewLines: []string{
			"old command",
			"EXIT: 0",
			"root@host:~# ",
		},
		CreatedAt: time.Now(),
	}
	mgr.mu.Lock()
	mgr.sessions[s.ID] = s
	mgr.mu.Unlock()

	afterLine := len(s.PreviewLines) // new command starts after prior output

	// Late new EXIT only after 1.2s — if residual EXIT triggered completion,
	// we'd return before the new marker arrives.
	go func() {
		time.Sleep(1200 * time.Millisecond)
		s.mu.Lock()
		s.PreviewLines = append(s.PreviewLines, "new output", "EXIT: 0")
		s.mu.Unlock()
	}()

	start := time.Now()
	lines, _ := mgr.WaitForOutput(s.ID, afterLine, 5*time.Second)
	elapsed := time.Since(start)

	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "new output") {
		t.Fatalf("expected new command output, got %q (elapsed=%v)", joined, elapsed)
	}
	if elapsed < 1*time.Second {
		t.Fatalf("completed too early (%v) — prior EXIT false positive?", elapsed)
	}
}

func TestLineLooksLikeExitMarker(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"EXIT: 0", true},
		{"EXIT:0", true},
		{"exit: 127", true},
		{"EXIT: 0x", false},
		{"printf EXIT: %s", false},
		{"EXIT:", false},
		{"price EXIT: 1", false},
		{"\x1b[0mEXIT: 0\x1b[0m", true},
	}
	for _, tc := range cases {
		if got := lineLooksLikeExitMarker(tc.in); got != tc.want {
			t.Errorf("lineLooksLikeExitMarker(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}

func TestCompletionSignalSinceIgnoresOldLines(t *testing.T) {
	s := &SSHManagedSession{
		PreviewLines: []string{"EXIT: 0", "root@h:~# ", "new line only"},
	}
	sig := s.completionSignalSince(2) // only "new line only"
	if sig.hasExit {
		t.Fatal("must not see EXIT from before afterLine")
	}
	if sig.newLines != 1 {
		t.Fatalf("newLines=%d", sig.newLines)
	}
	sig = s.completionSignalSince(0)
	if !sig.hasExit {
		t.Fatal("expected EXIT when afterLine=0")
	}
}

// TestPreviewRingKeepsAbsoluteLineIndices ensures WaitForOutput afterLine stays
// valid when the preview ring drops old lines (previously broke long dumps).
func TestPreviewRingKeepsAbsoluteLineIndices(t *testing.T) {
	s := &SSHManagedSession{}
	s.mu.Lock()
	// Fill past the ring cap with noise, then a real command region.
	for i := 0; i < sshPreviewMaxLines+50; i++ {
		s.PreviewLines = append(s.PreviewLines, "noise")
		s.trimPreviewLocked()
	}
	// Absolute line count includes dropped prefix.
	absBeforeCmd := s.droppedLines + len(s.PreviewLines)
	s.PreviewLines = append(s.PreviewLines, "cmd echo", "payload", "EXIT: 0")
	s.trimPreviewLocked()
	s.mu.Unlock()

	if s.LineCount() != absBeforeCmd+3 {
		t.Fatalf("LineCount=%d want %d (dropped=%d buf=%d)",
			s.LineCount(), absBeforeCmd+3, s.droppedLines, len(s.PreviewLines))
	}

	// NewLinesSince from absolute afterLine must still see EXIT
	lines, _ := s.NewLinesSince(absBeforeCmd)
	if !hasCommandExitMarker(lines) {
		t.Fatalf("expected EXIT in new lines after ring trim, got %v (dropped=%d)", lines, s.droppedLines)
	}
	sig := s.completionSignalSince(absBeforeCmd)
	if !sig.hasExit {
		t.Fatal("completionSignalSince missed EXIT after ring trim")
	}

	// WaitForOutput must complete on that EXIT without treating it as timeout
	mgr := NewSSHSessionManager(nil)
	s.ID = "ring-sess"
	s.Status = SessionRunning
	s.CreatedAt = time.Now()
	mgr.mu.Lock()
	mgr.sessions[s.ID] = s
	mgr.mu.Unlock()

	start := time.Now()
	out, _ := mgr.WaitForOutput(s.ID, absBeforeCmd, 3*time.Second)
	if !hasCommandExitMarker(out) {
		t.Fatalf("WaitForOutput missed EXIT after ring growth: %v", out)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("WaitForOutput too slow with pre-existing EXIT: %v", time.Since(start))
	}
}

func TestNewLinesSinceClampsDroppedPrefix(t *testing.T) {
	s := &SSHManagedSession{
		droppedLines: 100,
		PreviewLines: []string{"a", "b", "EXIT: 0"},
	}
	// afterLine inside dropped region → start from buffer head
	lines, _ := s.NewLinesSince(50)
	if len(lines) != 3 || lines[2] != "EXIT: 0" {
		t.Fatalf("got %v", lines)
	}
	// afterLine into buffer
	lines, _ = s.NewLinesSince(101)
	if len(lines) != 2 || lines[0] != "b" {
		t.Fatalf("got %v", lines)
	}
}

// TestProbeFrameSurvivesRingResize pins the durable-runtime recovery probe's
// availability floor: the probe frames its read-only git status between
// markers inside this preview ring, and a git status larger than the
// historical 2000-line ring evicted the begin marker, failing the whole task
// closed on an otherwise healthy workspace. Frames above the historical cap
// but below the current cap must survive intact (markers + HEAD line).
// The residual limit (frames larger than sshPreviewMaxLines itself) must
// stay fail-closed: eviction of the begin marker, never a wrong frame.
func TestProbeFrameSurvivesRingResize(t *testing.T) {
	const (
		markerStart = "PROBE-BEGIN-9f3a"
		markerEnd   = "PROBE-END-9f3a"
		headLine    = "9f3aeadc1234567890abcdef1234567890abcdef12"
	)
	buildFrame := func(statusLines int) []string {
		frame := make([]string, 0, statusLines+3)
		frame = append(frame, markerStart)
		for i := 0; i < statusLines; i++ {
			frame = append(frame, " M internal/pkg/file_0001.go")
		}
		frame = append(frame, markerEnd, headLine)
		return frame
	}
	appendFrame := func(s *SSHManagedSession, frame []string) int {
		absBefore := s.droppedLines + len(s.PreviewLines)
		s.PreviewLines = append(s.PreviewLines, frame...)
		s.trimPreviewLocked()
		return absBefore
	}

	t.Run("frame above historical 2000-line cap survives", func(t *testing.T) {
		s := &SSHManagedSession{}
		frame := buildFrame(15000)
		s.mu.Lock()
		absBefore := appendFrame(s, frame)
		s.mu.Unlock()

		lines, _ := s.NewLinesSince(absBefore)
		joined := strings.Join(lines, "\n")
		if !strings.Contains(joined, markerStart) {
			t.Fatalf("begin marker evicted; ring=%d dropped=%d", len(s.PreviewLines), s.droppedLines)
		}
		if !strings.Contains(joined, markerEnd) {
			t.Fatal("end marker missing from capture")
		}
		if !strings.Contains(joined, headLine) {
			t.Fatal("HEAD line missing from capture")
		}
	})

	t.Run("frame larger than the new cap still fails closed", func(t *testing.T) {
		s := &SSHManagedSession{}
		frame := buildFrame(sshPreviewMaxLines + 500)
		s.mu.Lock()
		absBefore := appendFrame(s, frame)
		s.mu.Unlock()

		lines, _ := s.NewLinesSince(absBefore)
		joined := strings.Join(lines, "\n")
		if strings.Contains(joined, markerStart) {
			t.Fatal("begin marker unexpectedly survived: eviction floor regressed")
		}
	})
}
