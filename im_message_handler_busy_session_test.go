package main

import (
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Exploratory tests for coding-session-premature-abandon bugfix.
//
// These tests confirm the bug conditions on UNFIXED code:
// 1. send_and_observe returns no busy-wait guidance when session stays busy
// 2. get_session_output returns no busy hint for busy sessions
// 3. System prompt discourages polling without exception for busy sessions
//
// All tests are expected to PASS on unfixed code (confirming the bug exists).
// ---------------------------------------------------------------------------

// newBusyTestSession creates a RemoteSession in busy status with a fake
// ExecutionHandle, suitable for testing toolSendAndObserve and
// toolGetSessionOutput without launching a real process.
func newBusyTestSession(id string) *RemoteSession {
	return &RemoteSession{
		ID:        id,
		Tool:      "claude-code",
		Title:     "test-busy",
		Status:    SessionBusy,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Exec:      newFakeExecutionHandle(100),
		RawOutputLines: []string{
			"❯ 用 C++ 写一个贪吃蛇游戏",
		},
	}
}

// newTestIMHandler creates a minimal IMMessageHandler with a
// RemoteSessionManager pre-populated with the given sessions.
func newTestIMHandler(sessions map[string]*RemoteSession) *IMMessageHandler {
	app := &App{}
	mgr := &RemoteSessionManager{
		app:      app,
		sessions: sessions,
	}
	return &IMMessageHandler{
		app:     app,
		manager: mgr,
	}
}

// ---------------------------------------------------------------------------
// 1.2 TestSendAndObserve_BusySession_NoBusyHint
//
// Calls toolSendAndObserve with a mock session that stays busy for 60s.
// On unfixed code, the return value should NOT contain busy-wait guidance
// like "编程工具仍在工作中", confirming the bug.
// ---------------------------------------------------------------------------
func TestSendAndObserve_BusySession_NoBusyHint(t *testing.T) {
	session := newBusyTestSession("sess-busy-1")

	h := newTestIMHandler(map[string]*RemoteSession{
		"sess-busy-1": session,
	})

	// Run toolSendAndObserve — the session stays busy throughout the
	// polling loop (~8s on unfixed code). We use a goroutine to keep
	// the session in busy status (it already is, we just don't change it).
	start := time.Now()
	result := h.toolSendAndObserve(map[string]interface{}{
		"session_id": "sess-busy-1",
		"text":       "用 C++ 写一个贪吃蛇游戏",
	})
	elapsed := time.Since(start)

	t.Logf("toolSendAndObserve returned after %v", elapsed)
	t.Logf("result:\n%s", result)

	// On unfixed code, the result should NOT contain busy-wait guidance.
	// This confirms the bug: Agent gets no hint to wait for busy sessions.
	busyHints := []string{
		"编程工具仍在工作中",
		"请等待",
		"不要终止会话",
	}
	for _, hint := range busyHints {
		if strings.Contains(result, hint) {
			t.Errorf("UNEXPECTED: result contains busy-wait hint %q — bug may already be fixed", hint)
		}
	}
}

// ---------------------------------------------------------------------------
// 1.3 TestGetSessionOutput_BusyStatus_NoHint
//
// Calls toolGetSessionOutput with a session in busy status.
// On unfixed code, the return value should NOT contain a busy-state hint,
// confirming the missing hint bug.
// ---------------------------------------------------------------------------
func TestGetSessionOutput_BusyStatus_NoHint(t *testing.T) {
	session := &RemoteSession{
		ID:        "sess-busy-2",
		Tool:      "claude-code",
		Title:     "test-busy-output",
		Status:    SessionBusy,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		RawOutputLines: []string{
			"❯ 重构错误处理模块",
			"⏺ I'll analyze the error handling patterns...",
			"⏺ TodoWrite: Planning refactoring steps",
		},
		Summary: SessionSummary{
			CurrentTask: "Analyzing error handling",
		},
	}

	h := newTestIMHandler(map[string]*RemoteSession{
		"sess-busy-2": session,
	})

	result := h.toolGetSessionOutput(map[string]interface{}{
		"session_id": "sess-busy-2",
	})

	t.Logf("toolGetSessionOutput result:\n%s", result)

	// Verify the status is reported as busy.
	if !strings.Contains(result, "busy") {
		t.Fatal("expected result to contain 'busy' status")
	}

	// On unfixed code, there should be NO busy-state hint.
	// The existing code has hints for "running" (no output) and "starting",
	// but nothing for "busy".
	busyHints := []string{
		"编程工具正在工作中",
		"请等待",
		"不要终止正在工作的会话",
	}
	for _, hint := range busyHints {
		if strings.Contains(result, hint) {
			t.Errorf("UNEXPECTED: result contains busy hint %q — bug may already be fixed", hint)
		}
	}
}

// ---------------------------------------------------------------------------
// 1.4 TestSystemPrompt_DiscoursagesPolling
//
// Verifies the system prompt contains "不要反复轮询 get_session_output"
// without any qualification for busy sessions. On unfixed code, this
// blanket prohibition confirms the contradictory guidance bug.
// ---------------------------------------------------------------------------
func TestSystemPrompt_DiscoursagesPolling(t *testing.T) {
	h := newTestIMHandler(map[string]*RemoteSession{})

	prompt := h.buildSystemPrompt()

	t.Logf("system prompt length: %d chars", len(prompt))

	// On unfixed code, the prompt should contain the blanket prohibition.
	if !strings.Contains(prompt, "不要反复轮询 get_session_output") {
		t.Fatal("expected system prompt to contain '不要反复轮询 get_session_output'")
	}

	// On unfixed code, the prompt should NOT contain guidance for
	// periodically checking busy sessions (e.g., "每 15-30 秒" or
	// "busy" session-specific polling guidance).
	busyPollingGuidance := []string{
		"每 15-30 秒",
		"busy 状态",
		"编程工具正在工作",
	}
	for _, guidance := range busyPollingGuidance {
		if strings.Contains(prompt, guidance) {
			t.Errorf("UNEXPECTED: system prompt contains busy-session polling guidance %q — bug may already be fixed", guidance)
		}
	}
}

// ===========================================================================
// Fix-checking tests — verify the bug is fixed.
//
// These tests confirm the fix works correctly:
// 6.1 send_and_observe returns busy-wait guidance when session stays busy
// 6.2 send_and_observe polling duration is ~30s (intentionally slow test)
// 6.3 get_session_output returns busy hint for busy sessions
// 6.4 System prompt contains long-running task guidance
// ===========================================================================

// ---------------------------------------------------------------------------
// 6.1 TestSendAndObserve_BusySession_ReturnsBusyHint
//
// Calls toolSendAndObserve with a mock session that stays busy.
// On fixed code, the return value MUST contain busy-wait guidance
// "编程工具仍在工作中".
//
// Validates: Requirements 2.1, 2.2
// ---------------------------------------------------------------------------
func TestSendAndObserve_BusySession_ReturnsBusyHint(t *testing.T) {
	session := newBusyTestSession("sess-fix-1")

	h := newTestIMHandler(map[string]*RemoteSession{
		"sess-fix-1": session,
	})

	result := h.toolSendAndObserve(map[string]interface{}{
		"session_id": "sess-fix-1",
		"text":       "用 C++ 写一个贪吃蛇游戏",
	})

	t.Logf("result:\n%s", result)

	if !strings.Contains(result, "编程工具仍在工作中") {
		t.Errorf("expected result to contain busy-wait guidance '编程工具仍在工作中', got:\n%s", result)
	}
}

// ---------------------------------------------------------------------------
// 6.2 TestSendAndObserve_ExtendedPolling
//
// Verifies the polling duration is ~30s by measuring elapsed time when the
// session stays busy throughout. The test asserts elapsed >= 25s.
//
// NOTE: This test is intentionally slow (~30s). Run with -timeout 120s.
//
// Validates: Requirements 2.1
// ---------------------------------------------------------------------------
func TestSendAndObserve_ExtendedPolling(t *testing.T) {
	session := newBusyTestSession("sess-fix-2")

	h := newTestIMHandler(map[string]*RemoteSession{
		"sess-fix-2": session,
	})

	start := time.Now()
	_ = h.toolSendAndObserve(map[string]interface{}{
		"session_id": "sess-fix-2",
		"text":       "重构整个项目的错误处理",
	})
	elapsed := time.Since(start)

	t.Logf("toolSendAndObserve returned after %v", elapsed)

	if elapsed < 25*time.Second {
		t.Errorf("expected polling duration >= 25s, got %v — polling window may not have been extended", elapsed)
	}
}

// ---------------------------------------------------------------------------
// 6.3 TestGetSessionOutput_BusyStatus_ReturnsHint
//
// Calls toolGetSessionOutput with a session in busy status.
// On fixed code, the return value MUST contain "编程工具正在工作中".
//
// Validates: Requirements 2.2
// ---------------------------------------------------------------------------
func TestGetSessionOutput_BusyStatus_ReturnsHint(t *testing.T) {
	session := &RemoteSession{
		ID:        "sess-fix-3",
		Tool:      "claude-code",
		Title:     "test-busy-hint",
		Status:    SessionBusy,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		RawOutputLines: []string{
			"❯ 重构错误处理模块",
			"⏺ I'll analyze the error handling patterns...",
			"⏺ TodoWrite: Planning refactoring steps",
		},
		Summary: SessionSummary{
			CurrentTask: "Analyzing error handling",
		},
	}

	h := newTestIMHandler(map[string]*RemoteSession{
		"sess-fix-3": session,
	})

	result := h.toolGetSessionOutput(map[string]interface{}{
		"session_id": "sess-fix-3",
	})

	t.Logf("toolGetSessionOutput result:\n%s", result)

	if !strings.Contains(result, "编程工具正在工作中") {
		t.Errorf("expected result to contain '编程工具正在工作中', got:\n%s", result)
	}
}

// ---------------------------------------------------------------------------
// 6.4 TestSystemPrompt_ContainsLongRunningGuidance
//
// Verifies the updated system prompt contains guidance for periodic polling
// of busy sessions and does NOT contain the unqualified blanket prohibition
// "不要反复轮询 get_session_output".
//
// The fixed prompt should:
// - Contain "每 15-30 秒" (periodic polling guidance)
// - Contain "busy" (busy session reference)
// - NOT contain the unqualified "不要反复轮询 get_session_output"
//   (the new prompt qualifies it as "对已退出或出错的会话不要反复轮询 get_session_output")
//
// Validates: Requirements 2.3
// ---------------------------------------------------------------------------
func TestSystemPrompt_ContainsLongRunningGuidance(t *testing.T) {
	h := newTestIMHandler(map[string]*RemoteSession{})

	prompt := h.buildSystemPrompt()

	// Must contain periodic polling guidance.
	if !strings.Contains(prompt, "每 15-30 秒") {
		t.Error("expected system prompt to contain '每 15-30 秒' (periodic polling guidance)")
	}

	// Must reference busy sessions.
	if !strings.Contains(prompt, "busy") {
		t.Error("expected system prompt to contain 'busy' (busy session reference)")
	}

	// Must NOT contain the old unqualified blanket prohibition.
	// The old text was: "不要反复轮询 get_session_output"
	// The new text qualifies it: "对已退出或出错的会话不要反复轮询 get_session_output"
	if strings.Contains(prompt, "不要反复轮询 get_session_output") &&
		!strings.Contains(prompt, "对已退出或出错的会话不要反复轮询 get_session_output") {
		t.Error("system prompt still contains unqualified '不要反复轮询 get_session_output' — should be qualified for exited/error sessions only")
	}
}

// ===========================================================================
// Preservation tests — verify no regressions.
//
// These tests confirm that existing non-busy behaviors remain unchanged
// after the bugfix:
// 7.1 send_and_observe returns immediately when session exits during polling
// 7.2 send_and_observe returns immediately when session enters waiting_input
// 7.3 send_and_observe returns immediately when meaningful output appears fast
// 7.4 get_session_output still shows 🛑 stop-loss for exited error sessions
// 7.5 get_session_output still shows "编程工具在等待输入" for running no-output
// 7.6 get_session_output still shows "会话正在启动中" for starting sessions
//
// Validates: Requirements 3.1, 3.2, 3.4
// ===========================================================================

// ---------------------------------------------------------------------------
// 7.1 TestSendAndObserve_ExitedSession_PreservesEarlyReturn
//
// Creates a session with Status=SessionExited and a non-zero ExitCode.
// Verifies send_and_observe returns immediately (< 2s) and does NOT
// contain the busy hint "编程工具仍在工作中".
//
// Validates: Requirements 3.1, 3.4
// ---------------------------------------------------------------------------
func TestSendAndObserve_ExitedSession_PreservesEarlyReturn(t *testing.T) {
	exitCode := 1
	session := &RemoteSession{
		ID:        "sess-exited-1",
		Tool:      "claude-code",
		Title:     "test-exited",
		Status:    SessionExited,
		ExitCode:  &exitCode,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Exec:      newFakeExecutionHandle(200),
		RawOutputLines: []string{
			"❯ some command",
			"Error: process exited",
		},
	}

	h := newTestIMHandler(map[string]*RemoteSession{
		"sess-exited-1": session,
	})

	start := time.Now()
	result := h.toolSendAndObserve(map[string]interface{}{
		"session_id": "sess-exited-1",
		"text":       "ls -la",
	})
	elapsed := time.Since(start)

	t.Logf("toolSendAndObserve returned after %v", elapsed)
	t.Logf("result:\n%s", result)

	if elapsed >= 2*time.Second {
		t.Errorf("expected early return (< 2s) for exited session, got %v", elapsed)
	}

	if strings.Contains(result, "编程工具仍在工作中") {
		t.Errorf("exited session should NOT contain busy hint '编程工具仍在工作中'")
	}
}

// ---------------------------------------------------------------------------
// 7.2 TestSendAndObserve_WaitingInput_PreservesEarlyReturn
//
// Creates a session initially in SessionBusy status, then transitions it
// to SessionWaitingInput after 500ms via a goroutine. Verifies
// send_and_observe returns immediately (< 2s) and does NOT contain the
// busy hint "编程工具仍在工作中".
//
// Validates: Requirements 3.2, 3.4
// ---------------------------------------------------------------------------
func TestSendAndObserve_WaitingInput_PreservesEarlyReturn(t *testing.T) {
	session := &RemoteSession{
		ID:        "sess-waiting-1",
		Tool:      "claude-code",
		Title:     "test-waiting",
		Status:    SessionBusy,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Exec:      newFakeExecutionHandle(201),
		RawOutputLines: []string{
			"❯ some task",
		},
	}

	h := newTestIMHandler(map[string]*RemoteSession{
		"sess-waiting-1": session,
	})

	// After 500ms, transition session to waiting_input.
	go func() {
		time.Sleep(500 * time.Millisecond)
		session.mu.Lock()
		session.Status = SessionWaitingInput
		session.Summary.WaitingForUser = true
		session.mu.Unlock()
	}()

	start := time.Now()
	result := h.toolSendAndObserve(map[string]interface{}{
		"session_id": "sess-waiting-1",
		"text":       "do something",
	})
	elapsed := time.Since(start)

	t.Logf("toolSendAndObserve returned after %v", elapsed)
	t.Logf("result:\n%s", result)

	if elapsed >= 2*time.Second {
		t.Errorf("expected early return (< 2s) for waiting_input session, got %v", elapsed)
	}

	if strings.Contains(result, "编程工具仍在工作中") {
		t.Errorf("waiting_input session should NOT contain busy hint '编程工具仍在工作中'")
	}
}

// ---------------------------------------------------------------------------
// 7.3 TestSendAndObserve_FastOutput_PreservesEarlyReturn
//
// Creates a session in SessionBusy status with 1 initial output line, then
// appends 2+ more lines after 500ms via a goroutine (so newLines > 1).
// Verifies send_and_observe returns early (< 2s). The session stays busy
// so the busy hint WILL be appended — that's correct behavior.
//
// Validates: Requirements 3.4
// ---------------------------------------------------------------------------
func TestSendAndObserve_FastOutput_PreservesEarlyReturn(t *testing.T) {
	session := &RemoteSession{
		ID:        "sess-fast-1",
		Tool:      "claude-code",
		Title:     "test-fast-output",
		Status:    SessionBusy,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Exec:      newFakeExecutionHandle(202),
		RawOutputLines: []string{
			"❯ echo hello",
		},
	}

	h := newTestIMHandler(map[string]*RemoteSession{
		"sess-fast-1": session,
	})

	// After 500ms, append 2 more lines to simulate fast output.
	go func() {
		time.Sleep(500 * time.Millisecond)
		session.mu.Lock()
		session.RawOutputLines = append(session.RawOutputLines, "hello", "world")
		session.mu.Unlock()
	}()

	start := time.Now()
	result := h.toolSendAndObserve(map[string]interface{}{
		"session_id": "sess-fast-1",
		"text":       "echo hello",
	})
	elapsed := time.Since(start)

	t.Logf("toolSendAndObserve returned after %v", elapsed)
	t.Logf("result:\n%s", result)

	if elapsed >= 2*time.Second {
		t.Errorf("expected early return (< 2s) for fast output, got %v", elapsed)
	}
}

// ---------------------------------------------------------------------------
// 7.4 TestGetSessionOutput_ExitedError_PreservesStopLoss
//
// Creates a session with Status=SessionExited, ExitCode=1, Tool="claude-code".
// Verifies toolGetSessionOutput still contains the 🛑 stop-loss hint and
// "会话已失败退出".
//
// Validates: Requirements 3.1
// ---------------------------------------------------------------------------
func TestGetSessionOutput_ExitedError_PreservesStopLoss(t *testing.T) {
	exitCode := 1
	session := &RemoteSession{
		ID:        "sess-stoploss-1",
		Tool:      "claude-code",
		Title:     "test-stoploss",
		Status:    SessionExited,
		ExitCode:  &exitCode,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		RawOutputLines: []string{
			"Error: something went wrong",
		},
	}

	h := newTestIMHandler(map[string]*RemoteSession{
		"sess-stoploss-1": session,
	})

	result := h.toolGetSessionOutput(map[string]interface{}{
		"session_id": "sess-stoploss-1",
	})

	t.Logf("toolGetSessionOutput result:\n%s", result)

	if !strings.Contains(result, "🛑") {
		t.Errorf("expected result to contain '🛑' stop-loss emoji, got:\n%s", result)
	}
	if !strings.Contains(result, "会话已失败退出") {
		t.Errorf("expected result to contain '会话已失败退出', got:\n%s", result)
	}
}

// ---------------------------------------------------------------------------
// 7.5 TestGetSessionOutput_RunningNoOutput_PreservesHint
//
// Creates a session with Status=SessionRunning and no RawOutputLines.
// Verifies toolGetSessionOutput still contains "编程工具在等待输入".
//
// Validates: Requirements 3.2
// ---------------------------------------------------------------------------
func TestGetSessionOutput_RunningNoOutput_PreservesHint(t *testing.T) {
	session := &RemoteSession{
		ID:        "sess-running-1",
		Tool:      "claude-code",
		Title:     "test-running-no-output",
		Status:    SessionRunning,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	h := newTestIMHandler(map[string]*RemoteSession{
		"sess-running-1": session,
	})

	result := h.toolGetSessionOutput(map[string]interface{}{
		"session_id": "sess-running-1",
	})

	t.Logf("toolGetSessionOutput result:\n%s", result)

	if !strings.Contains(result, "编程工具在等待输入") {
		t.Errorf("expected result to contain '编程工具在等待输入', got:\n%s", result)
	}
}

// ---------------------------------------------------------------------------
// 7.6 TestGetSessionOutput_StartingState_PreservesHint
//
// Creates a session with Status=SessionStarting and no output. Uses a
// goroutine to transition the session out of starting state after 500ms
// (to avoid the 5s wait loop in toolGetSessionOutput). Verifies the
// result contains "会话正在启动中".
//
// Validates: Requirements 3.4
// ---------------------------------------------------------------------------
func TestGetSessionOutput_StartingState_PreservesHint(t *testing.T) {
	session := &RemoteSession{
		ID:        "sess-starting-1",
		Tool:      "claude-code",
		Title:     "test-starting",
		Status:    SessionStarting,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	h := newTestIMHandler(map[string]*RemoteSession{
		"sess-starting-1": session,
	})

	// The session stays in starting state with no output. The wait loop
	// in toolGetSessionOutput will run for ~5s before returning with the
	// "会话正在启动中" hint.
	result := h.toolGetSessionOutput(map[string]interface{}{
		"session_id": "sess-starting-1",
	})

	t.Logf("toolGetSessionOutput result:\n%s", result)

	if !strings.Contains(result, "会话正在启动中") {
		t.Errorf("expected result to contain '会话正在启动中', got:\n%s", result)
	}
}
