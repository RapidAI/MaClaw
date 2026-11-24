package guiapp

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/goal"
)

func TestRunWindowCreateClaimAndStop(t *testing.T) {
	m := newTestRunWindowManager(t)
	// time.Now, not a fixed date: MarkStarted pauses a task whose
	// window already ended against the real clock, so the window must
	// stay in the future. Truncate to the minute because Create parses
	// StartAt/EndAt at minute precision and Equal() compares instants.
	now := time.Now().Truncate(time.Minute)
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
	// time.Now, not a fixed date: MarkStarted pauses a task whose
	// window already ended against the real clock, so the window must
	// stay in the future. Truncate to the minute because Create parses
	// StartAt/EndAt at minute precision and Equal() compares instants.
	now := time.Now().Truncate(time.Minute)
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
	// time.Now, not a fixed date: MarkStarted pauses a task whose
	// window already ended against the real clock, so the window must
	// stay in the future. Truncate to the minute because Create parses
	// StartAt/EndAt at minute precision and Equal() compares instants.
	now := time.Now().Truncate(time.Minute)
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

func TestRunWindowWorkFinishedSeesTheStoredSlashLoop(t *testing.T) {
	const asked = `desktop-user:F:/bird2`
	const stored = `desktop-user:F:\bird2`
	h := &IMMessageHandler{}
	started := time.Now().Add(-time.Minute)
	store := goal.NewStore("")
	g, err := store.Set(stored, "图形版本愤怒的小鸟，c++语言")
	if err != nil {
		t.Fatal(err)
	}
	store.UpdateStatus(stored, g.GoalID, goal.StatusPaused, "用户已停止当前回合。")
	engine := NewGoalContinuationEngine(store, nil)

	storedLoop := NewLoopContext("stored-live", 1, nil)
	cleanupStored := h.beginAgentLoopRuntime(storedLoop, stored, "续接", "goal-continuation")
	if runWindowWorkFinishedOn(h, engine, asked, started, time.Now(), false) {
		cleanupStored()
		t.Fatal("a continuation still running on the stored spelling looked finished")
	}
	if !h.RequestPauseAfterCurrentUnit(asked) || !storedLoop.ConsumePauseAfterCurrent() {
		cleanupStored()
		t.Fatal("pause after the current unit missed the stored loop")
	}
	cleanupStored()
	if !runWindowWorkFinishedOn(h, engine, asked, started, time.Now(), false) {
		t.Fatal("the window stayed open after the stored loop ended")
	}

	m := &runWindowManager{tasks: []RunWindowTask{{
		ID: "rw-slash", Status: runWindowRunning, SessionUserID: asked,
	}}}
	if !m.tracksRunningSession(stored) {
		t.Fatal("settle on the stored spelling missed the composer window")
	}
	if m.tracksRunningSession(`desktop-user:F:/other`) {
		t.Fatal("a different project matched this window")
	}
}

func TestRunWindowDeleteStopsTheStoredSlashLoop(t *testing.T) {
	const asked = `desktop-user:F:/bird2`
	const stored = `desktop-user:F:\bird2`
	h := &IMMessageHandler{}
	loop := NewLoopContext("stored-live", 1, nil)
	cleanup := h.beginAgentLoopRuntime(loop, asked, "续接", "goal-continuation")
	defer cleanup()
	go func() {
		<-loop.CancelC
		loop.Done()
	}()
	// The window id is the normalized backslash row. That cancel found no
	// loop, so the forward-slash continuation still has to be stopped.
	if !h.stopOtherRunWindowLoops(stored, stored) {
		t.Fatal("delete left the continuation loop running")
	}
	if !loop.IsCancelled() || !loop.UserCancelled() {
		t.Fatal("stored-spelling cancel did not stop the forward-slash loop")
	}
	cleanup()
	if h.stopOtherRunWindowLoops(stored, stored) {
		t.Fatal("a second delete found another loop")
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
	// time.Now, not a fixed date: MarkStarted pauses a task whose
	// window already ended against the real clock, so the window must
	// stay in the future. Truncate to the minute because Create parses
	// StartAt/EndAt at minute precision and Equal() compares instants.
	now := time.Now().Truncate(time.Minute)
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
	t.Cleanup(m.Close)
	return m
}

func TestRunWindowMigratesLegacyJSONIntoNormalizedRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run_window_tasks.json")
	start := time.Now().Add(time.Hour).Truncate(time.Second)
	legacy := []RunWindowTask{{
		ID: "legacy-window", Name: "legacy", Prompt: "run", Status: runWindowPending,
		StartAt: start, CreatedAt: start.Add(-time.Minute),
		Draft: RunWindowDraft{
			TaskType: "chat", WorkspaceKind: "remote", RemoteHost: "example.internal", RemotePort: 22,
			WorkflowParams: map[string]string{"topic": "storage"},
		},
	}}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	manager, err := newRunWindowManager(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := manager.Get("legacy-window")
	if err != nil {
		t.Fatal(err)
	}
	if got.Draft.RemoteHost != "example.internal" || got.Draft.WorkflowParams["topic"] != "storage" || !got.StartAt.Equal(start) {
		t.Fatalf("imported task = %#v", got)
	}
	var remoteHost string
	if err := manager.store.db.QueryRow(`SELECT remote_host FROM run_window_tasks WHERE id='legacy-window'`).Scan(&remoteHost); err != nil {
		t.Fatal(err)
	}
	if remoteHost != "example.internal" {
		t.Fatalf("remote_host column = %q", remoteHost)
	}
	var topic string
	if err := manager.store.db.QueryRow(`SELECT value FROM run_window_workflow_params WHERE task_id='legacy-window' AND key='topic'`).Scan(&topic); err != nil {
		t.Fatal(err)
	}
	if topic != "storage" {
		t.Fatalf("workflow parameter = %q", topic)
	}
	var jsonColumns int
	if err := manager.store.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE sql LIKE '%payload%' OR sql LIKE '%_json%'`).Scan(&jsonColumns); err != nil {
		t.Fatal(err)
	}
	if jsonColumns != 0 {
		t.Fatalf("run window schema contains JSON document columns: %d", jsonColumns)
	}
	if err := manager.Delete("legacy-window"); err != nil {
		t.Fatal(err)
	}
	manager.Close()
	reopened, err := newRunWindowManager(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.Get("legacy-window"); err == nil {
		t.Fatal("legacy JSON resurrected a deleted run window")
	}
}

func TestRunWindowIncrementalPersistenceAndSignatureBackfill(t *testing.T) {
	manager := newTestRunWindowManager(t)
	path := manager.path
	now := time.Now()
	for _, item := range []struct {
		id    string
		name  string
		topic string
	}{{"window-a", "first", "one"}, {"window-b", "second", "two"}} {
		manager.tasks = append(manager.tasks, RunWindowTask{
			ID: item.id, Name: item.name, Status: runWindowPending, StartAt: now.Add(time.Hour), CreatedAt: now,
			Draft: RunWindowDraft{TaskType: "chat", WorkflowParams: map[string]string{"topic": item.topic}},
		})
	}
	if err := manager.saveLocked(); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.store.db.Exec(`
CREATE TABLE run_window_write_audit(changes INTEGER NOT NULL);
INSERT INTO run_window_write_audit VALUES(0);
CREATE TRIGGER run_window_audit_insert AFTER INSERT ON run_window_tasks BEGIN UPDATE run_window_write_audit SET changes=changes+1; END;
CREATE TRIGGER run_window_audit_update AFTER UPDATE ON run_window_tasks BEGIN UPDATE run_window_write_audit SET changes=changes+1; END;
CREATE TRIGGER run_window_audit_delete AFTER DELETE ON run_window_tasks BEGIN UPDATE run_window_write_audit SET changes=changes+1; END;
CREATE TRIGGER run_param_audit_insert AFTER INSERT ON run_window_workflow_params BEGIN UPDATE run_window_write_audit SET changes=changes+1; END;
CREATE TRIGGER run_param_audit_update AFTER UPDATE ON run_window_workflow_params BEGIN UPDATE run_window_write_audit SET changes=changes+1; END;
CREATE TRIGGER run_param_audit_delete AFTER DELETE ON run_window_workflow_params BEGIN UPDATE run_window_write_audit SET changes=changes+1; END;
`); err != nil {
		t.Fatal(err)
	}
	changes := func() int {
		var count int
		if err := manager.store.db.QueryRow(`SELECT changes FROM run_window_write_audit`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	if err := manager.saveLocked(); err != nil {
		t.Fatal(err)
	}
	if got := changes(); got != 0 {
		t.Fatalf("unchanged tasks rewrote %d rows", got)
	}
	manager.tasks[0].Name = "first edited"
	if err := manager.saveLocked(); err != nil {
		t.Fatal(err)
	}
	if got := changes(); got != 1 {
		t.Fatalf("name-only edit wrote %d rows, want one task update", got)
	}
	manager.tasks[0].Draft.WorkflowParams["topic"] = "updated"
	if err := manager.saveLocked(); err != nil {
		t.Fatal(err)
	}
	if got := changes(); got != 4 {
		t.Fatalf("parameter edit changed %d total rows, want one more update and parameter delete+insert", got)
	}
	manager.tasks[0], manager.tasks[1] = manager.tasks[1], manager.tasks[0]
	if err := manager.saveLocked(); err != nil {
		t.Fatal(err)
	}
	if got := changes(); got != 6 {
		t.Fatalf("reordering changed %d total rows, want two task updates without parameter rewrites", got)
	}
	ordered, err := loadRunWindowTasks(manager.store.db)
	if err != nil {
		t.Fatal(err)
	}
	if len(ordered) != 2 || ordered[0].ID != "window-b" || ordered[1].ID != "window-a" {
		t.Fatalf("reordered positions were not persisted: %#v", ordered)
	}
	manager.Close()

	reopened, err := newRunWindowManager(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.store.db.Exec(`ALTER TABLE run_window_tasks DROP COLUMN signature`); err != nil {
		t.Fatal(err)
	}
	reopened.Close()
	backfilled, err := newRunWindowManager(path)
	if err != nil {
		t.Fatal(err)
	}
	defer backfilled.Close()
	for _, id := range []string{"window-a", "window-b"} {
		var signature string
		if err := backfilled.store.db.QueryRow(`SELECT signature FROM run_window_tasks WHERE id=?`, id).Scan(&signature); err != nil {
			t.Fatal(err)
		}
		if signature == "" {
			t.Fatalf("task %s signature was not backfilled", id)
		}
	}
	got, err := backfilled.Get("window-a")
	if err != nil || got.Name != "first edited" || got.Draft.WorkflowParams["topic"] != "updated" {
		t.Fatalf("backfilled task = %#v, %v", got, err)
	}
}

func TestCloudRunWindowIdentity(t *testing.T) {
	id := "cag_" + strings.Repeat("ab", 16)
	path := "cloud-agent:" + id
	if got := cloudAgentInstanceFromProjectPath(path); got != id {
		t.Fatalf("instance = %q, want %s", got, id)
	}
	if got := runWindowSessionForStart(path, "expert-9"); got != "cloud-agent:"+id {
		t.Fatalf("session = %q, want the cloud instance even when an expert is on the draft", got)
	}
	if got := cloudAgentInstanceFromRunWindowSession("cloud-agent:" + id); got != id {
		t.Fatalf("session instance = %q", got)
	}
	if cloudAgentInstanceFromProjectPath("cloud-agent:"+id+":task-1") != "" {
		t.Fatal("a stream key is not the scheduled instance")
	}
	if cloudAgentInstanceFromProjectPath(`D:\tasks\notes`) != "" {
		t.Fatal("a local path was read as a cloud instance")
	}
	if got := runWindowSessionForStart(`D:\tasks\notes`, "expert-9"); got != expertSessionUserID("expert-9") {
		t.Fatalf("local expert session = %q", got)
	}
	if got := runWindowSessionForStart(`D:\tasks\notes`, ""); got != projectSessionOwnerID(`D:\tasks\notes`) {
		t.Fatalf("local session = %q", got)
	}
}

func TestCloudRunWindowTurnFinished(t *testing.T) {
	if cloudRunWindowTurnFinished("running", true, true) {
		t.Fatal("a running cloud turn was treated as finished")
	}
	if cloudRunWindowTurnFinished("provisioning", true, true) {
		t.Fatal("a provisioning instance was treated as finished")
	}
	if !cloudRunWindowTurnFinished("idle", true, true) {
		t.Fatal("an idle instance still has its turn open")
	}
	if !cloudRunWindowTurnFinished("stopped", true, true) || !cloudRunWindowTurnFinished("provision_failed", true, true) {
		t.Fatal("a stopped or failed instance is still executing")
	}
	if cloudRunWindowTurnFinished("idle", true, false) {
		t.Fatal("a failed Hub lookup finished the scheduled row")
	}
	if !cloudRunWindowTurnFinished("", false, true) {
		t.Fatal("a deleted instance stayed running")
	}
	started := time.Now()
	if cloudRunWindowObservedFinished("", false, true, started.Add(-time.Second), started) {
		t.Fatal("a list from before the task started deleted the new instance")
	}
	if !cloudRunWindowObservedFinished("idle", true, true, started.Add(time.Second), started) {
		t.Fatal("an idle status read after the start was ignored")
	}
	if cloudRunWindowObservedFinished("running", true, true, started.Add(time.Second), started) {
		t.Fatal("a running status read after the start finished the row")
	}
}

func TestCloudRunWindowLastTaskID(t *testing.T) {
	if got := lastCloudTaskID(`{"tasks":[{"id":"task-1"},{"id":" task-2 "}]}`); got != "task-2" {
		t.Fatalf("last task = %q", got)
	}
	if lastCloudTaskID(`{"tasks":[]}`) != "" || lastCloudTaskID("nope") != "" {
		t.Fatal("missing tasks returned an id")
	}
}

func resetCloudStopFlightsForTest() {
	cloudStopFlights.mu.Lock()
	cloudStopFlights.byID = nil
	cloudStopFlights.mu.Unlock()
}

func TestCloudRunWindowStopFlight(t *testing.T) {
	resetCloudStopFlightsForTest()
	t.Cleanup(resetCloudStopFlightsForTest)
	const id = "cag_flight"
	start, accepted, err := cloudStopBegin(id)
	if !start || accepted || err != nil {
		t.Fatalf("begin = start %v accepted %v err %v", start, accepted, err)
	}
	start, accepted, err = cloudStopBegin(id)
	if start || accepted || err != nil {
		t.Fatalf("second begin = start %v accepted %v err %v", start, accepted, err)
	}
	cloudStopDropFinished(id)
	start, _, _ = cloudStopBegin(id)
	if start {
		t.Fatal("drop removed an in-flight cancel")
	}
	cloudStopFinish(id, nil)
	start, accepted, err = cloudStopBegin(id)
	if start || !accepted || err != nil {
		t.Fatalf("finished begin = start %v accepted %v err %v", start, accepted, err)
	}
	start, _, _ = cloudStopBegin(id)
	if !start {
		t.Fatal("a read result did not allow the next cancel")
	}
	stopErr := errors.New("云端运行时不可用")
	cloudStopFinish(id, stopErr)
	start, accepted, err = cloudStopBegin(id)
	if start || accepted || !errors.Is(err, stopErr) {
		t.Fatalf("failed begin = start %v accepted %v err %v", start, accepted, err)
	}
	start, _, _ = cloudStopBegin(id)
	if !start {
		t.Fatal("a read failure did not allow the next cancel")
	}
	cloudStopFinish(id, nil)
	cloudStopDropFinished(id)
	start, _, _ = cloudStopBegin(id)
	if !start {
		t.Fatal("an unread result stayed behind after the task was gone")
	}
	cloudStopFinish(id, nil)
}

func TestCloudRunWindowListDoesNotWaitForCancel(t *testing.T) {
	resetCloudStopFlightsForTest()
	t.Cleanup(resetCloudStopFlightsForTest)
	m := newTestRunWindowManager(t)
	now := time.Now().Truncate(time.Minute)
	start := now.Add(time.Hour)
	end := start.Add(time.Hour)
	instanceID := "cag_" + strings.Repeat("ab", 16)
	session := "cloud-agent:" + instanceID
	cloudID, err := m.Create(RunWindowCreateRequest{
		Prompt:  "云端定时",
		StartAt: start.Format("2006-01-02T15:04"),
		EndAt:   end.Format("2006-01-02T15:04"),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	localID, err := m.Create(RunWindowCreateRequest{
		Prompt:  "本机定时",
		StartAt: start.Format("2006-01-02T15:04"),
		EndAt:   end.Format("2006-01-02T15:04"),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Claim(cloudID, start); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Claim(localID, start); err != nil {
		t.Fatal(err)
	}
	if err := m.MarkStarted(cloudID, session, session); err != nil {
		t.Fatal(err)
	}
	if err := m.MarkStarted(localID, `D:\tasks\notes`, "desktop-user:D:\\tasks\\notes"); err != nil {
		t.Fatal(err)
	}
	// The window has been running longer than the local attach grace, and
	// its end has passed. A local miss stops the row. A cloud miss stays
	// until the Hub cancel actually returns.
	startedAt := time.Now().Add(-10 * time.Minute)
	endedAt := startedAt
	m.mu.Lock()
	for _, id := range []string{cloudID, localID} {
		task, _, findErr := m.find(id)
		if findErr != nil {
			m.mu.Unlock()
			t.Fatal(findErr)
		}
		task.StartedAt = &startedAt
		task.EndAt = &endedAt
	}
	m.mu.Unlock()

	release := make(chan struct{})
	started := make(chan struct{})
	done := make(chan struct{})
	var releaseOnce sync.Once
	closeRelease := func() { releaseOnce.Do(func() { close(release) }) }
	defer func() {
		closeRelease()
		select {
		case <-started:
			select {
			case <-done:
			case <-time.After(2 * time.Second):
			}
		default:
		}
	}()
	m.pauseRun = func(gotSession string) (bool, error) {
		if cloudAgentInstanceFromRunWindowSession(gotSession) == "" {
			return false, nil
		}
		begin, accepted, beginErr := cloudStopBegin(instanceID)
		if !begin {
			return accepted, beginErr
		}
		go func() {
			defer close(done)
			close(started)
			<-release
			cloudStopFinish(instanceID, nil)
			m.sweep(time.Now())
			cloudStopDropFinished(instanceID)
		}()
		return false, nil
	}

	listDone := make(chan []RunWindowTask, 1)
	go func() {
		listDone <- m.List()
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("cloud cancel did not start")
	}
	var listed []RunWindowTask
	select {
	case listed = <-listDone:
	case <-time.After(2 * time.Second):
		t.Fatal("listing scheduled tasks waited on the cloud cancel")
	}
	var cloudStatus, localStatus string
	for _, task := range listed {
		switch task.ID {
		case cloudID:
			cloudStatus = task.Status
		case localID:
			localStatus = task.Status
		}
	}
	if cloudStatus != runWindowRunning {
		t.Fatalf("cloud row = %s, want running while the cancel is in flight", cloudStatus)
	}
	if localStatus != runWindowStopped {
		t.Fatalf("local row = %s, want stopped once its grace has passed", localStatus)
	}
	closeRelease()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cloud cancel did not finish")
	}
	got, err := m.Get(cloudID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != runWindowStopped {
		t.Fatalf("cloud row after cancel = %s", got.Status)
	}
}

func TestCloudRunWindowFailedCancelRetries(t *testing.T) {
	m := newTestRunWindowManager(t)
	now := time.Now().Truncate(time.Minute)
	start := now.Add(time.Hour)
	end := start.Add(time.Hour)
	retrySession := "cloud-agent:cag_" + strings.Repeat("cd", 16)
	giveUpSession := "cloud-agent:cag_" + strings.Repeat("ef", 16)
	retryID, err := m.Create(RunWindowCreateRequest{
		Prompt:  "云端取消会再试",
		StartAt: start.Format("2006-01-02T15:04"),
		EndAt:   end.Format("2006-01-02T15:04"),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	giveUpID, err := m.Create(RunWindowCreateRequest{
		Prompt:  "云端取消到点放弃",
		StartAt: start.Format("2006-01-02T15:04"),
		EndAt:   end.Format("2006-01-02T15:04"),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	localID, err := m.Create(RunWindowCreateRequest{
		Prompt:  "本机取消失败",
		StartAt: start.Format("2006-01-02T15:04"),
		EndAt:   end.Format("2006-01-02T15:04"),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{retryID, giveUpID} {
		if _, err := m.Claim(id, start); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Claim(localID, start); err != nil {
		t.Fatal(err)
	}
	if err := m.MarkStarted(retryID, retrySession, retrySession); err != nil {
		t.Fatal(err)
	}
	if err := m.MarkStarted(giveUpID, giveUpSession, giveUpSession); err != nil {
		t.Fatal(err)
	}
	if err := m.MarkStarted(localID, `D:\tasks\notes`, "desktop-user:D:\\tasks\\notes"); err != nil {
		t.Fatal(err)
	}
	// The task itself started long enough ago that the local attach grace
	// has expired. The window ended a moment ago, so the cloud retry budget
	// is still open.
	startedAt := time.Now().Add(-10 * time.Minute)
	endedAt := time.Now().Add(-time.Second)
	m.mu.Lock()
	for _, id := range []string{retryID, giveUpID, localID} {
		task, _, findErr := m.find(id)
		if findErr != nil {
			m.mu.Unlock()
			t.Fatal(findErr)
		}
		task.StartedAt = &startedAt
		task.EndAt = &endedAt
	}
	m.mu.Unlock()
	retryCalls := 0
	m.pauseRun = func(session string) (bool, error) {
		if session == giveUpSession || cloudAgentInstanceFromRunWindowSession(session) == "" {
			return false, errors.New("云端运行时不可用")
		}
		retryCalls++
		if retryCalls == 1 {
			return false, errors.New("云端运行时不可用")
		}
		return true, nil
	}
	m.sweep(time.Now())
	retry, err := m.Get(retryID)
	if err != nil {
		t.Fatal(err)
	}
	giveUp, err := m.Get(giveUpID)
	if err != nil {
		t.Fatal(err)
	}
	local, err := m.Get(localID)
	if err != nil {
		t.Fatal(err)
	}
	if retry.Status != runWindowRunning || giveUp.Status != runWindowRunning {
		t.Fatalf("cloud after first failure retry=%s giveUp=%s", retry.Status, giveUp.Status)
	}
	if local.Status != runWindowStopped {
		t.Fatalf("local status = %s, the attach grace had already passed", local.Status)
	}
	m.sweep(time.Now().Add(time.Second))
	retry, err = m.Get(retryID)
	if err != nil {
		t.Fatal(err)
	}
	giveUp, err = m.Get(giveUpID)
	if err != nil {
		t.Fatal(err)
	}
	if retry.Status != runWindowStopped || retryCalls != 2 {
		t.Fatalf("retry after a later success = %s calls=%d", retry.Status, retryCalls)
	}
	if giveUp.Status != runWindowRunning {
		t.Fatalf("give-up row stopped while its budget was open: %s", giveUp.Status)
	}
	m.sweep(endedAt.Add(runWindowPauseAttachGrace + time.Second))
	giveUp, err = m.Get(giveUpID)
	if err != nil {
		t.Fatal(err)
	}
	if giveUp.Status != runWindowStopped {
		t.Fatalf("give-up row after the budget = %s", giveUp.Status)
	}
}

func TestCloudRunWindowPlaceSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run_window_tasks.json")
	// A database created before place existed. Opening it must add the
	// column, and a cloud window saved after that must still be cloud.
	created, err := newRunWindowManager(path)
	if err != nil {
		t.Fatal(err)
	}
	created.Close()
	db, err := sql.Open("sqlite", runWindowDBPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE run_window_tasks DROP COLUMN place`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := newRunWindowManager(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(upgraded.Close)
	now := time.Now().Truncate(time.Minute)
	start := now.Add(time.Hour)
	cloudID, err := upgraded.Create(RunWindowCreateRequest{
		Prompt:  "云端定时",
		StartAt: start.Format("2006-01-02T15:04"),
		Draft:   RunWindowDraft{TaskType: "coding", Place: "cloud"},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	localID, err := upgraded.Create(RunWindowCreateRequest{
		Prompt:  "本机定时",
		StartAt: start.Format("2006-01-02T15:04"),
		Draft:   RunWindowDraft{TaskType: "chat", Place: "local"},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	upgraded.Close()

	reopened, err := newRunWindowManager(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	cloud, err := reopened.Get(cloudID)
	if err != nil {
		t.Fatal(err)
	}
	local, err := reopened.Get(localID)
	if err != nil {
		t.Fatal(err)
	}
	if cloud.Draft.Place != "cloud" || cloud.Draft.TaskType != "coding" {
		t.Fatalf("cloud window reloaded as %#v", cloud.Draft)
	}
	if local.Draft.Place != "" {
		t.Fatalf("local window place = %q", local.Draft.Place)
	}
}
