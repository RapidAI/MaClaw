package guiapp

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/goal"
)

func TestRunWindowCreateClaimAndStop(t *testing.T) {
	m := newTestRunWindowManager(t)
	now := time.Date(2026, 10, 14, 1, 0, 0, 0, time.Local)
	start := now.Add(2 * time.Hour)
	end := start.Add(4 * time.Hour)
	id, err := m.Create(RunWindowCreateRequest{
		Name:    "夜间整理",
		Prompt:  "整理本周笔记",
		Draft:   RunWindowDraft{TaskType: "chat", WorkspaceKind: "local"},
		StartAt: start.Format("2006-01-02T15:04"),
		EndAt:   end.Format("2006-01-02T15:04"),
	}, now)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := m.Claim(id, now); err == nil {
		t.Fatal("claim before start should fail")
	}
	claimed, err := m.Claim(id, start)
	if err != nil {
		t.Fatalf("claim at start: %v", err)
	}
	if claimed.Status != runWindowRunning {
		t.Fatalf("status = %s", claimed.Status)
	}
	if _, err := m.Claim(id, start); err == nil {
		t.Fatal("second claim should fail")
	}
	if err := m.MarkStarted(id, `D:\tasks\notes`, "desktop-user:D:\\tasks\\notes"); err != nil {
		t.Fatalf("mark started: %v", err)
	}

	var paused []string
	m.pauseRun = func(session string) (bool, error) {
		paused = append(paused, session)
		return true, nil
	}
	m.sweep(end)
	got, err := m.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != runWindowStopped {
		t.Fatalf("status after end = %s", got.Status)
	}
	if len(paused) != 1 || paused[0] != "desktop-user:D:\\tasks\\notes" {
		t.Fatalf("paused = %#v", paused)
	}
}

func TestRunWindowPauseFinishesCurrentUnit(t *testing.T) {
	ctx := &LoopContext{ID: "chat", UserID: "desktop-user:task", Lang: "zh-Hans", CancelC: make(chan struct{})}
	ctx.RequestPauseAfterCurrent()
	select {
	case <-ctx.CancelC:
		t.Fatal("pause closed the in-flight cancel channel")
	default:
	}
	if !ctx.PauseAfterCurrentRequested() {
		t.Fatal("pause was not requested")
	}
	note, paused := takeRunWindowPause(ctx)
	if !paused || !strings.Contains(note, "已暂停") {
		t.Fatalf("pause note = %q", note)
	}
	if _, again := takeRunWindowPause(ctx); again {
		t.Fatal("pause was consumed twice")
	}

	kept := &LoopContext{ID: "chat", UserID: "desktop-user:task", Lang: "zh-Hans", CancelC: make(chan struct{})}
	kept.RequestPauseAfterCurrent()
	note, resp, paused := runWindowPauseResponse(kept, []interface{}{
		map[string]string{"role": "assistant", "content": "整理好了三份笔记。"},
	})
	if !paused || resp == nil || resp.Text != "整理好了三份笔记。" || !strings.Contains(note, "已暂停") || !resp.runWindowPaused {
		t.Fatalf("pause replaced the answer: note=%q resp=%#v", note, resp)
	}
	parts := &LoopContext{ID: "parts", Lang: "zh-Hans", CancelC: make(chan struct{})}
	parts.RequestPauseAfterCurrent()
	_, resp, paused = runWindowPauseResponse(parts, []interface{}{
		map[string]interface{}{"role": "assistant", "content": []interface{}{
			map[string]interface{}{"type": "text", "text": "第一部分"},
		}},
	})
	if !paused || resp == nil || resp.Text != "第一部分" {
		t.Fatalf("content parts were dropped: %#v", resp)
	}

	handler := &IMMessageHandler{}
	live := &LoopContext{ID: "live", CancelC: make(chan struct{})}
	handler.sessionLoops.Store("session-1", &sessionLoopState{loopCtx: live})
	if !handler.RequestPauseAfterCurrentUnit("session-1") {
		t.Fatal("expected the live loop to accept the pause")
	}
	if !live.PauseAfterCurrentRequested() {
		t.Fatal("live loop was not marked")
	}
	select {
	case <-live.CancelC:
		t.Fatal("requesting pause cancelled the live loop")
	default:
	}
	if handler.RequestPauseAfterCurrentUnit("missing") {
		t.Fatal("idle session should not be paused")
	}
}

func TestRunWindowRejectsInvalidRangeAndMissesClosedWindow(t *testing.T) {
	m := newTestRunWindowManager(t)
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.Local)
	start := now.Add(time.Hour)
	_, err := m.Create(RunWindowCreateRequest{
		Prompt:  "太早",
		StartAt: now.Add(-2 * time.Hour).Format("2006-01-02T15:04"),
	}, now)
	if err == nil {
		t.Fatal("past start should be rejected")
	}
	_, err = m.Create(RunWindowCreateRequest{
		Prompt:  "结束早于开始",
		StartAt: start.Format("2006-01-02T15:04"),
		EndAt:   start.Add(-time.Minute).Format("2006-01-02T15:04"),
	}, now)
	if err == nil {
		t.Fatal("end before start should be rejected")
	}

	end := start.Add(30 * time.Minute)
	id, err := m.Create(RunWindowCreateRequest{
		Prompt:  "错过窗口",
		StartAt: start.Format("2006-01-02T15:04"),
		EndAt:   end.Format("2006-01-02T15:04"),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Claim(id, end); err == nil {
		t.Fatal("claim after end should fail")
	}
	got, err := m.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != runWindowMissed {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestRunWindowUpdateTimesAndDeleteCancels(t *testing.T) {
	m := newTestRunWindowManager(t)
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.Local)
	start := now.Add(time.Hour)
	id, err := m.Create(RunWindowCreateRequest{
		Prompt:  "可改时间",
		StartAt: start.Format("2006-01-02T15:04"),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	next := start.Add(2 * time.Hour)
	end := next.Add(time.Hour)
	if err := m.UpdateTimes(id, next.Format("2006-01-02T15:04"), end.Format("2006-01-02T15:04"), now); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := m.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if !got.StartAt.Equal(next) || got.EndAt == nil || !got.EndAt.Equal(end) {
		t.Fatalf("times = %s / %v", got.StartAt, got.EndAt)
	}

	if _, err := m.Claim(id, next); err != nil {
		t.Fatal(err)
	}
	if err := m.MarkStarted(id, `D:\tasks\edit`, "session-edit"); err != nil {
		t.Fatal(err)
	}
	laterEnd := end.Add(time.Hour)
	if err := m.UpdateTimes(id, now.Format(time.RFC3339), laterEnd.Format("2006-01-02T15:04"), next); err != nil {
		t.Fatalf("update running end: %v", err)
	}
	got, err = m.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if !got.StartAt.Equal(next) || got.EndAt == nil || !got.EndAt.Equal(laterEnd) {
		t.Fatalf("running edit changed start or end: %s / %v", got.StartAt, got.EndAt)
	}

	var stopped []string
	m.stopRun = func(session string) error {
		stopped = append(stopped, session)
		return nil
	}
	if err := m.Delete(id); err != nil {
		t.Fatal(err)
	}
	if len(stopped) != 1 || stopped[0] != "session-edit" {
		t.Fatalf("delete cancel = %#v", stopped)
	}
	if _, err := m.Get(id); err == nil {
		t.Fatal("deleted task still listed")
	}
}

func TestRunWindowNoEndRunsUntilComplete(t *testing.T) {
	m := newTestRunWindowManager(t)
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.Local)
	start := now.Add(time.Hour)
	id, err := m.Create(RunWindowCreateRequest{
		Prompt:  "做到完成为止",
		StartAt: start.Format("2006-01-02T15:04"),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Claim(id, start); err != nil {
		t.Fatal(err)
	}
	if err := m.MarkStarted(id, `D:\tasks\open`, "session-open"); err != nil {
		t.Fatal(err)
	}
	m.sweep(start.Add(48 * time.Hour))
	got, err := m.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != runWindowRunning {
		t.Fatalf("status = %s, want running until the work finishes", got.Status)
	}
}

func TestRunWindowNoEndCompletesWhenWorkFinishes(t *testing.T) {
	m := newTestRunWindowManager(t)
	now := time.Date(2026, 10, 13, 5, 0, 0, 0, time.Local)
	start := now.Add(51 * time.Minute)
	id, err := m.Create(RunWindowCreateRequest{
		Prompt:  "北京天气，生成pdf",
		StartAt: start.Format("2006-01-02T15:04"),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Claim(id, start); err != nil {
		t.Fatal(err)
	}
	if err := m.MarkStarted(id, `D:\tasks\weather`, "session-weather"); err != nil {
		t.Fatal(err)
	}
	var implicit []bool
	m.workFinished = func(session string, startedAt, at time.Time, allowImplicit bool) bool {
		if session != "session-weather" || startedAt.IsZero() {
			return false
		}
		implicit = append(implicit, allowImplicit)
		return !at.Before(start.Add(2 * time.Hour))
	}
	m.sweep(start.Add(time.Hour))
	got, err := m.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != runWindowRunning {
		t.Fatalf("status = %s, want running while the session is still working", got.Status)
	}
	m.sweep(start.Add(3 * time.Hour))
	got, err = m.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != runWindowCompleted {
		t.Fatalf("status = %s, want completed after the work finishes", got.Status)
	}
	if len(implicit) != 2 || !implicit[0] || !implicit[1] {
		t.Fatalf("allowImplicit = %v, want true for a task with no deadline", implicit)
	}
}

func TestRunWindowCompletesInsideOpenWindow(t *testing.T) {
	m := newTestRunWindowManager(t)
	now := time.Date(2026, 10, 14, 1, 0, 0, 0, time.Local)
	start := now.Add(time.Hour)
	end := start.Add(4 * time.Hour)
	id, err := m.Create(RunWindowCreateRequest{
		Prompt:  "窗口内做完",
		StartAt: start.Format("2006-01-02T15:04"),
		EndAt:   end.Format("2006-01-02T15:04"),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Claim(id, start); err != nil {
		t.Fatal(err)
	}
	if err := m.MarkStarted(id, `D:\tasks\open-window`, "session-open"); err != nil {
		t.Fatal(err)
	}
	sawImplicit := true
	m.workFinished = func(session string, startedAt, at time.Time, allowImplicit bool) bool {
		sawImplicit = allowImplicit
		return true
	}
	m.sweep(start.Add(time.Hour))
	got, err := m.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != runWindowCompleted {
		t.Fatalf("status = %s, want completed before the window ends", got.Status)
	}
	if sawImplicit {
		t.Fatal("open window was offered implicit completion")
	}
}

func TestRunWindowWorkFinishedFollowsSession(t *testing.T) {
	h := &IMMessageHandler{}
	session := "desktop-user:D:\\tasks\\weather"
	started := time.Now().Add(-time.Minute)
	if runWindowWorkFinishedOn(h, nil, session, started, time.Now(), true) {
		t.Fatal("idle session with no ended loop should stay running inside the launch grace")
	}
	later := time.Now().Add(runWindowPauseAttachGrace + time.Second)
	if !runWindowWorkFinishedOn(h, nil, session, started, later, true) {
		t.Fatal("idle session past the launch grace should count as finished")
	}
	if runWindowWorkFinishedOn(h, nil, session, started, later, false) {
		t.Fatal("an open window should stay running until a loop end is observed")
	}

	ctx := NewLoopContext("loop-weather", 4, nil)
	cleanup := h.beginAgentLoopRuntime(ctx, session, "北京天气，生成pdf", "desktop")
	if runWindowWorkFinishedOn(h, nil, session, started, later, true) {
		t.Fatal("a live loop should keep the task running")
	}
	cleanup()
	h.setPendingForegroundText(session, "北京天气，生成pdf")
	if !runWindowWorkFinishedOn(h, nil, session, started, time.Now(), false) {
		t.Fatal("a finished loop should not stay running while the request lock is still held")
	}
	next := NewLoopContext("loop-weather-next", 1, nil)
	h.beginInFlightTurn(session, "下一轮", next)
	if runWindowWorkFinishedOn(h, nil, session, started, time.Now(), false) {
		t.Fatal("an in-flight turn should keep the task running")
	}
	h.endInFlightTurn(session, next)
	if runWindowWorkFinishedOn(h, nil, session, time.Now().Add(time.Minute), time.Now().Add(2*time.Minute), true) {
		t.Fatal("an older loop end should not finish a task that has not run yet")
	}

	store := goal.NewStore("")
	if _, err := store.Set(session, "把天气写成 pdf"); err != nil {
		t.Fatal(err)
	}
	engine := NewGoalContinuationEngine(store, nil)
	if runWindowWorkFinishedOn(h, engine, session, started, time.Now(), true) {
		t.Fatal("an active goal should keep the scheduled task running")
	}
}

func TestRunWindowKeepsClaimUntilSessionAttaches(t *testing.T) {
	m := newTestRunWindowManager(t)
	now := time.Date(2026, 10, 14, 1, 0, 0, 0, time.Local)
	start := now.Add(time.Hour)
	end := start.Add(4 * time.Hour)
	id, err := m.Create(RunWindowCreateRequest{
		Prompt:  "等启动完成",
		StartAt: start.Format("2006-01-02T15:04"),
		EndAt:   end.Format("2006-01-02T15:04"),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Claim(id, start); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	task, _, err := m.find(id)
	if err != nil {
		m.mu.Unlock()
		t.Fatal(err)
	}
	closed := start.Add(20 * time.Second)
	task.EndAt = &closed
	m.mu.Unlock()

	m.sweep(start.Add(30 * time.Second))
	got, err := m.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != runWindowRunning {
		t.Fatalf("status = %s, want running until the session attaches", got.Status)
	}
	if err := m.MarkStarted(id, `D:\tasks\late`, "session-late"); err != nil {
		t.Fatalf("mark started after the window: %v", err)
	}
}

func TestRunWindowReclaimsAbandonedClaim(t *testing.T) {
	m := newTestRunWindowManager(t)
	now := time.Date(2026, 10, 14, 1, 0, 0, 0, time.Local)
	start := now.Add(time.Hour)
	end := start.Add(4 * time.Hour)
	id, err := m.Create(RunWindowCreateRequest{
		Prompt:  "启动中断",
		StartAt: start.Format("2006-01-02T15:04"),
		EndAt:   end.Format("2006-01-02T15:04"),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Claim(id, start); err != nil {
		t.Fatal(err)
	}
	m.sweep(start.Add(runWindowClaimLease + time.Minute))
	got, err := m.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != runWindowPending {
		t.Fatalf("status = %s, want the lease returned", got.Status)
	}
	if _, err := m.Claim(id, start.Add(runWindowClaimLease+time.Minute)); err != nil {
		t.Fatalf("reclaim did not allow a new claim: %v", err)
	}

	closed, err := m.Create(RunWindowCreateRequest{
		Prompt:  "窗口已过",
		StartAt: start.Format("2006-01-02T15:04"),
		EndAt:   start.Add(time.Minute).Format("2006-01-02T15:04"),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Claim(closed, start); err != nil {
		t.Fatal(err)
	}
	m.sweep(start.Add(runWindowClaimLease + time.Minute))
	got, err = m.Get(closed)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != runWindowFailed || !strings.Contains(got.LastError, "启动超时") {
		t.Fatalf("closed lease = %s %q", got.Status, got.LastError)
	}
}

func TestRunWindowRetriesPauseUntilLoopExists(t *testing.T) {
	m := newTestRunWindowManager(t)
	now := time.Date(2026, 10, 14, 1, 0, 0, 0, time.Local)
	start := now.Add(time.Hour)
	end := start.Add(time.Hour)
	id, err := m.Create(RunWindowCreateRequest{
		Prompt:  "等循环出现",
		StartAt: start.Format("2006-01-02T15:04"),
		EndAt:   end.Format("2006-01-02T15:04"),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Claim(id, start); err != nil {
		t.Fatal(err)
	}
	if err := m.MarkStarted(id, `D:\tasks\wait`, "session-wait"); err != nil {
		t.Fatal(err)
	}
	sweepAt := end
	m.mu.Lock()
	task, _, err := m.find(id)
	if err != nil {
		m.mu.Unlock()
		t.Fatal(err)
	}
	task.StartedAt = &sweepAt
	m.mu.Unlock()

	calls := 0
	m.pauseRun = func(session string) (bool, error) {
		calls++
		if session != "session-wait" {
			t.Fatalf("session = %s", session)
		}
		return calls > 1, nil
	}
	m.sweep(sweepAt)
	got, err := m.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != runWindowRunning || calls != 1 {
		t.Fatalf("first sweep status=%s calls=%d", got.Status, calls)
	}
	m.sweep(sweepAt.Add(time.Second))
	got, err = m.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != runWindowStopped || calls != 2 {
		t.Fatalf("second sweep status=%s calls=%d", got.Status, calls)
	}
}

func TestRunWindowMarkStartedPausesWhenEndAlreadyPassed(t *testing.T) {
	m := newTestRunWindowManager(t)
	base := time.Now().Add(-2 * time.Hour).Truncate(time.Minute)
	start := base.Add(time.Hour)
	end := start.Add(time.Minute)
	id, err := m.Create(RunWindowCreateRequest{
		Prompt:  "窗口先到",
		StartAt: start.Format(time.RFC3339),
		EndAt:   end.Format(time.RFC3339),
	}, base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Claim(id, start); err != nil {
		t.Fatal(err)
	}
	var paused []string
	m.pauseRun = func(session string) (bool, error) {
		paused = append(paused, session)
		return true, nil
	}
	if err := m.MarkStarted(id, `D:\tasks\late`, "session-late"); err != nil {
		t.Fatal(err)
	}
	got, err := m.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != runWindowStopped || len(paused) != 1 || paused[0] != "session-late" {
		t.Fatalf("status=%s paused=%#v", got.Status, paused)
	}
}

func TestRunWindowReleaseKeepsReasonAndMissesClosedWindow(t *testing.T) {
	m := newTestRunWindowManager(t)
	base := time.Now().Add(-2 * time.Hour).Truncate(time.Minute)
	start := base.Add(time.Hour)
	end := time.Now().Add(time.Hour)
	id, err := m.Create(RunWindowCreateRequest{
		Prompt:  "启动失败再试",
		StartAt: start.Format(time.RFC3339),
		EndAt:   end.Format(time.RFC3339),
	}, base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Claim(id, start); err != nil {
		t.Fatal(err)
	}
	if err := m.release(id, "创建任务失败", time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err := m.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != runWindowPending || got.LastError != "创建任务失败" || got.ClaimedAt != nil {
		t.Fatalf("release = %s %q claimed=%v", got.Status, got.LastError, got.ClaimedAt)
	}
	if _, err := m.Claim(id, time.Now()); err != nil {
		t.Fatal(err)
	}
	closed := time.Now().Add(-time.Minute)
	m.mu.Lock()
	task, _, err := m.find(id)
	if err != nil {
		m.mu.Unlock()
		t.Fatal(err)
	}
	task.EndAt = &closed
	m.mu.Unlock()
	if err := m.release(id, "窗口已关", time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err = m.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != runWindowMissed || got.LastError != "窗口已关" {
		t.Fatalf("closed release = %s %q", got.Status, got.LastError)
	}
}

func newTestRunWindowManager(t *testing.T) *runWindowManager {
	t.Helper()
	m, err := newRunWindowManager(filepath.Join(t.TempDir(), "run_window_tasks.json"))
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	return m
}
