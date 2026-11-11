package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// Daniel, Nov 11 16:32: 只要在推进就可以不停，除非用户主动要求. A run is ended by
// a genuine stall, a loop, the person's stop or a runaway ceiling, never by
// a 60-minute clock.

func watchdogTestClock(t *testing.T, start time.Time) *time.Time {
	t.Helper()
	now := start
	prevW, prevJ := watchdogNow, desktopLongJobNow
	watchdogNow = func() time.Time { return now }
	desktopLongJobNow = func() time.Time { return now }
	t.Cleanup(func() { watchdogNow, desktopLongJobNow = prevW, prevJ })
	return &now
}

func TestD102ProgressingRunKeepsGoingPastAnHour(t *testing.T) {
	now := watchdogTestClock(t, time.Date(2026, 11, 11, 15, 0, 0, 0, time.Local))
	scope := agentruntime.Scope{RunID: "run_d102_progress"}
	t.Cleanup(func() { desktopWatchdogEnd(scope.RunID) })
	tail := map[string]any{"action": "bash", "command": "tail -n 3 ~/train/log.txt"}
	// Two and a half hours of tailing a training log every 90 s, plus a
	// file write now and then: the same command, a new result each time.
	for i := 0; i < 100; i++ {
		*now = now.Add(90 * time.Second)
		args := tail
		action := "bash"
		if i%25 == 0 {
			action, args = "file_write", map[string]any{"action": "file_write", "path": "/home/desktop/notes.md", "content": "step " + itoa(i)}
		}
		if _, err := desktopWatchdogBefore(scope, action, args); err != nil {
			t.Fatalf("step %d at %s stopped a progressing run: %v", i, now.Format("15:04"), err)
		}
		out := "epoch 2 step " + itoa(i*8) + "/803 loss=0." + itoa(300-i)
		desktopWatchdogAfter(scope, nil)
		desktopWatchdogResult(scope, action, args, out, nil)
	}
	if now.Sub(time.Date(2026, 11, 11, 15, 0, 0, 0, time.Local)) < 2*time.Hour {
		t.Fatal("test did not pass the old 60 minute limit")
	}
	if stop, ok := desktopWatchdogStopped(scope.RunID); ok {
		t.Fatalf("progressing run stopped: %s", stop)
	}
	if n := desktopWatchdogGrant(scope.RunID, 300, 100); n <= 0 {
		t.Fatal("a progressing run got no more iterations")
	}
	if _, ok := desktopWatchdogVerdict(scope.RunID); ok {
		t.Fatal("verdict on a progressing run")
	}
}

func TestD102StalledRunStopsWithEvidence(t *testing.T) {
	now := watchdogTestClock(t, time.Date(2026, 11, 11, 15, 0, 0, 0, time.Local))
	scope := agentruntime.Scope{RunID: "run_d102_stall"}
	t.Cleanup(func() { desktopWatchdogEnd(scope.RunID) })
	desktopWatchdogBefore(scope, "file_write", map[string]any{"path": "/home/desktop/a.py", "content": "x"})
	desktopWatchdogResult(scope, "file_write", map[string]any{"path": "/home/desktop/a.py", "content": "x"}, "wrote", nil)
	// Then different calls that all change nothing (the same screen and
	// the same log line, with only the clock moving).
	stoppedAt := time.Time{}
	for i := 0; i < 40; i++ {
		*now = now.Add(time.Minute)
		action := []string{"screenshot", "bash", "wait"}[i%3]
		args := map[string]any{"action": action, "n": i}
		if _, err := desktopWatchdogBefore(scope, action, args); err == errWatchdogStop {
			stoppedAt = *now
			break
		}
		desktopWatchdogAfter(scope, nil)
		desktopWatchdogResult(scope, action, args, "same page at "+now.Format("15:04:05"), nil)
	}
	if stoppedAt.IsZero() {
		t.Fatal("stalled run never stopped")
	}
	snap, _ := desktopWatchdogSnapshot(scope.RunID)
	if got := stoppedAt.Sub(snap.lastProgress); got < watchdogStallAfter || got > watchdogStallAfter+time.Minute {
		t.Fatalf("stopped %s after the last progress, stall window %s", got, watchdogStallAfter)
	}
	if stoppedAt.Sub(time.Date(2026, 11, 11, 15, 0, 0, 0, time.Local)) > watchdogStallAfter+5*time.Minute {
		t.Fatalf("calls that change nothing kept the run alive until %s", stoppedAt.Format("15:04"))
	}
	if stop, _ := desktopWatchdogStopped(scope.RunID); stop != "stalled" {
		t.Fatalf("stop = %q", stop)
	}
	if n := desktopWatchdogGrant(scope.RunID, 300, 40); n != 0 {
		t.Fatal("a stalled run got more iterations")
	}
	if _, ok := desktopWatchdogVerdict(scope.RunID); ok {
		t.Fatal("no turn for the model to say what is left")
	}
	reply, ok := desktopWatchdogVerdict(scope.RunID)
	for _, want := range []string{"分钟没有可测的进展", "卡在这里", "最后一次可测的进展在", "已写出的文件：/home/desktop/a.py", "剩下的部分还没做"} {
		if !ok || !strings.Contains(reply, want) {
			t.Fatalf("reply %q lacks %q", reply, want)
		}
	}
	desktopRecStateMu.Lock()
	desktopRecRunSteps[scope.RunID] = true
	desktopRecStateMu.Unlock()
	t.Cleanup(func() { desktopRecoveryEndRun(scope.RunID) })
	outcome, why := desktopRunOutcome(scope.RunID, &agentservice.Message{Content: "写好了 a.py"})
	if outcome != agentservice.TaskOutcomePartial || !strings.Contains(why, "没有可测的进展") || !strings.Contains(why, "最后一次可测的进展") {
		t.Fatalf("outcome=%s why=%q", outcome, why)
	}
}

func TestD102SweepFlagsARunThatTakesNoStep(t *testing.T) {
	now := watchdogTestClock(t, time.Date(2026, 11, 11, 15, 0, 0, 0, time.Local))
	scope := agentruntime.Scope{RunID: "run_d102_idle"}
	t.Cleanup(func() { desktopWatchdogEnd(scope.RunID) })
	desktopWatchdogBefore(scope, "click", map[string]any{"x": 1})
	// One long step still running is not a stall by itself.
	*now = now.Add(watchdogStallAfter + time.Minute)
	desktopWatchdogSweepOnce(*now)
	if _, ok := desktopWatchdogStopped(scope.RunID); ok {
		t.Fatal("a step still running was called stalled")
	}
	desktopWatchdogResult(scope, "click", map[string]any{"x": 1}, "clicked", nil)
	*now = now.Add(watchdogStallAfter + time.Minute)
	desktopWatchdogSweepOnce(*now)
	if stop, _ := desktopWatchdogStopped(scope.RunID); stop != "stalled" {
		t.Fatalf("idle run not flagged: %q", stop)
	}
}

func TestD102UserStopEndsAProgressingRun(t *testing.T) {
	watchdogTestClock(t, time.Now())
	scope := agentruntime.Scope{TenantID: "t", UserID: "alice", InstanceID: "inst_stop", RunID: "run_d102_user"}
	t.Cleanup(func() { desktopWatchdogEnd(scope.RunID) })
	desktopWatchdogBefore(scope, "bash", map[string]any{"command": "tail log"})
	desktopWatchdogResult(scope, "bash", map[string]any{"command": "tail log"}, "epoch 1 step 5/10", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	turn := agentruntime.TurnRequest{Scope: scope}
	_, err := (desktopRuntimeModule{}).InvokeTool(ctx, turn, desktopToolName, map[string]any{"action": "bash", "command": "tail log"})
	if err != errWatchdogStop {
		t.Fatalf("a step ran after the stop: %v", err)
	}
	if stop, _ := desktopWatchdogStopped(scope.RunID); stop != "user" {
		t.Fatalf("stop = %q", stop)
	}
	if _, err := desktopWatchdogBefore(scope, "bash", map[string]any{"command": "tail log"}); err != errWatchdogStop {
		t.Fatal("the next step was not refused")
	}
	if n := desktopWatchdogGrant(scope.RunID, 300, 3); n != 0 {
		t.Fatal("a stopped run got more iterations")
	}
}

func TestD102LoopNeedsTheSameResult(t *testing.T) {
	watchdogTestClock(t, time.Now())
	scope := agentruntime.Scope{RunID: "run_d102_loop"}
	t.Cleanup(func() { desktopWatchdogEnd(scope.RunID) })
	click := map[string]any{"action": "click", "selector": "#publish"}
	stopped := 0
	for i := 1; i <= 10; i++ {
		if _, err := desktopWatchdogBefore(scope, "click", click); err == errWatchdogStop {
			stopped = i
			break
		}
		desktopWatchdogResult(scope, "click", click, "", watchdogError("element not found: #publish"))
	}
	if stopped != watchdogLoopStop {
		t.Fatalf("identical failing click stopped at %d, want %d", stopped, watchdogLoopStop)
	}
	if stop, _ := desktopWatchdogStopped(scope.RunID); stop != "loop" {
		t.Fatalf("stop = %q", stop)
	}
	// The same tail command with a new log line each time is not a loop.
	tail := agentruntime.Scope{RunID: "run_d102_tail"}
	t.Cleanup(func() { desktopWatchdogEnd(tail.RunID) })
	args := map[string]any{"action": "bash", "command": "tail -n 1 log"}
	for i := 0; i < 20; i++ {
		if _, err := desktopWatchdogBefore(tail, "bash", args); err != nil {
			t.Fatalf("tailing an advancing log stopped at %d", i)
		}
		desktopWatchdogResult(tail, "bash", args, "step "+itoa(i)+"/803", nil)
	}
}

func TestD102CeilingStopsHonestlyAndLeavesTheJob(t *testing.T) {
	d100Env(t)
	now := watchdogTestClock(t, time.Date(2026, 11, 11, 3, 0, 0, 0, time.Local))
	scope := agentruntime.Scope{RunID: "run_d102_ceiling"}
	if err := desktopLongJobSave(&desktopLongJob{ID: "job_ceiling1", SourceRun: scope.RunID, Status: "running", Alive: true, Command: "nohup python train.py", Started: *now}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { desktopWatchdogEnd(scope.RunID) })
	desktopWatchdogBefore(scope, "bash", map[string]any{"i": 0})
	desktopWatchdogResult(scope, "bash", map[string]any{"i": 0}, "epoch 1 step 1/9", nil)
	*now = now.Add(watchdogMaxWall)
	watchdogMu.Lock()
	watchdogRuns[scope.RunID].lastProgress = *now
	watchdogRuns[scope.RunID].lastStrong = *now
	watchdogMu.Unlock()
	if _, err := desktopWatchdogBefore(scope, "bash", map[string]any{"i": 1}); err != errWatchdogStop {
		t.Fatal("runaway ceiling not applied")
	}
	desktopWatchdogVerdict(scope.RunID) // the model's turn
	reply, ok := desktopWatchdogVerdict(scope.RunID)
	if !ok || !strings.Contains(reply, "安全时间上限") || !strings.Contains(reply, itoa(int(watchdogMaxWall.Hours()))+" 小时") || !strings.Contains(reply, "后台任务没有被停掉") || !strings.Contains(reply, "train.py") {
		t.Fatalf("reply %q", reply)
	}
}

func TestD102StatusIsSaidAndHeartbeatOnlyWhenNothingMoved(t *testing.T) {
	// Daniel 11-11 20:38: first note after 30 min, at most one per 30 min,
	// only on a meaningful change; a stall warning once.
	base := time.Date(2026, 11, 11, 15, 0, 0, 0, time.Local)
	now := watchdogTestClock(t, base)
	said := make(chan string, 8)
	prev := desktopLongJobSayFunc
	desktopLongJobSayFunc = func(_ context.Context, b map[string]any) error { s, _ := b["text"].(string); said <- s; return nil }
	t.Cleanup(func() { desktopLongJobSayFunc = prev })
	scope := agentruntime.Scope{TenantID: "tenant_default", UserID: "u_1::desk::bot_1", InstanceID: "inst_1", RunID: "run_d102_status"}
	t.Cleanup(func() { desktopWatchdogEnd(scope.RunID) })
	n := 0
	step := func(action, out string) {
		n++
		args := map[string]any{"action": action, "n": n}
		desktopWatchdogBefore(scope, action, args)
		desktopWatchdogResult(scope, action, args, out, nil)
		desktopRunProgressSay(scope, action, out, nil)
	}
	wait := func() string {
		select {
		case s := <-said:
			return s
		case <-time.After(2 * time.Second):
			return ""
		}
	}
	quiet := func(why string) {
		select {
		case s := <-said:
			t.Fatalf("%s: said %q", why, s)
		case <-time.After(80 * time.Millisecond):
		}
	}
	at := func(m int) { *now = base.Add(time.Duration(m) * time.Minute) }
	for m := 1; m < 30; m += 2 {
		at(m)
		step("bash", "epoch 1 step "+itoa(m*10)+"/803 loss=0.5")
		step("click", "clicked "+itoa(m))
		step("screenshot", "screen "+itoa(m))
	}
	quiet("before 30 minutes")
	at(31)
	step("bash", "epoch 1 step 400/803 loss=0.3")
	if s := wait(); !strings.Contains(s, "已 30 分钟") || !strings.Contains(s, "指标推进") || !strings.Contains(s, "step 400/803") || strings.Contains(s, "步（成功") {
		t.Fatalf("status %q", s)
	}
	at(45)
	step("bash", "epoch 1 step 600/803 loss=0.2")
	quiet("within the interval")
	// The change held back by the interval is said once the interval is up.
	at(62)
	step("screenshot", "screen 62")
	if s := wait(); !strings.Contains(s, "step 600/803") {
		t.Fatalf("held-back status %q", s)
	}
	// New output and screens, but no new measured result: not news.
	for m := 63; m < 93; m += 3 {
		at(m)
		step("screenshot", "screen "+itoa(m))
		step("bash", "listing "+itoa(m))
	}
	at(93)
	step("bash", "listing 93")
	step("screenshot", "screen 93")
	step("bash", "epoch 1 step 600/803 loss=0.2")
	quiet("no meaningful change")
	at(94)
	step("bash", "epoch 2 step 10/803 loss=0.19")
	if s := wait(); !strings.Contains(s, "epoch 2 step 10/803") {
		t.Fatalf("second status %q", s)
	}
	// Then nothing changes: one stall warning, not a stream.
	for m := 95; m < 110; m++ {
		at(m)
		args := map[string]any{"action": "wait", "n": m}
		desktopWatchdogBefore(scope, "wait", args)
		desktopWatchdogResult(scope, "wait", args, "", nil)
		desktopRunStatusTick(scope, *now)
	}
	if s := wait(); !strings.Contains(s, "可能卡住了") || !strings.Contains(s, "没有可测的进展") {
		t.Fatalf("stall warning %q", s)
	}
	quiet("stall warned twice")
}

func TestD102LongJobHeartbeatWhenUnchanged(t *testing.T) {
	now := watchdogTestClock(t, time.Date(2026, 11, 11, 15, 0, 0, 0, time.Local))
	j := &desktopLongJob{ID: "job_hb", Status: "stalled", Alive: true, Fingerprint: "fp", Started: now.Add(-time.Hour), LastChange: now.Add(-40 * time.Minute)}
	if dec := desktopLongJobDecide(j, "stalled", "fp", now.Add(-desktopLongJobHeartbeatEvery+time.Minute)); dec != longJobSilent {
		t.Fatalf("spam within the window: %s", dec)
	}
	dec := desktopLongJobDecide(j, "stalled", "fp", now.Add(-desktopLongJobHeartbeatEvery-time.Minute))
	if dec != longJobHeartbeat {
		t.Fatalf("no heartbeat: %s", dec)
	}
	if text := desktopLongJobNoticeText(j, dec); !strings.Contains(text, "自 14:20 起没有新的输出") {
		t.Fatalf("text %q", text)
	}
}

func TestD102OnlyClicksAndScreensForAnHourIsAStall(t *testing.T) {
	now := watchdogTestClock(t, time.Date(2026, 11, 11, 15, 0, 0, 0, time.Local))
	scope := agentruntime.Scope{RunID: "run_d102_wander"}
	t.Cleanup(func() { desktopWatchdogEnd(scope.RunID) })
	stopped := false
	for i := 0; i < 200 && !stopped; i++ {
		*now = now.Add(time.Minute)
		action := []string{"click", "screenshot"}[i%2]
		args := map[string]any{"action": action, "x": i}
		if _, err := desktopWatchdogBefore(scope, action, args); err == errWatchdogStop {
			stopped = true
			break
		}
		desktopWatchdogResult(scope, action, args, action+" "+itoa(i), nil)
	}
	if !stopped || now.Sub(time.Date(2026, 11, 11, 15, 0, 0, 0, time.Local)) > watchdogWeakOnly+2*time.Minute {
		t.Fatalf("wandering run stopped=%v at %s", stopped, now.Format("15:04"))
	}
}

func TestD103RepeatedMeasurementIsNotProgress(t *testing.T) {
	base := time.Date(2026, 11, 11, 20, 0, 0, 0, time.Local)
	now := watchdogTestClock(t, base)
	scope := agentruntime.Scope{TenantID: "t", UserID: "u::desk::bot_x", InstanceID: "inst_x", RunID: "run_d103_shape"}
	t.Cleanup(func() { desktopWatchdogEnd(scope.RunID) })
	step := func(m int, cmd, out string) {
		*now = base.Add(time.Duration(m) * time.Minute)
		args := map[string]any{"action": "bash", "command": cmd}
		desktopWatchdogBefore(scope, "bash", args)
		desktopWatchdogResult(scope, "bash", args, out, nil)
	}
	step(1, "python3 diff.py a b", "changed pixels: 3200 of 1024000")
	first, _ := desktopWatchdogSnapshot(scope.RunID)
	for m := 2; m < 12; m++ {
		step(m, "python3 diff.py a"+itoa(m)+" b", "changed pixels: "+itoa(m*7)+" of 1024000")
	}
	snap, _ := desktopWatchdogSnapshot(scope.RunID)
	if !snap.lastProgress.Equal(first.lastProgress) {
		t.Fatalf("re-measurement counted as progress: %v vs %v", snap.lastProgress, first.lastProgress)
	}
	step(12, "ls", "snake.py README.txt")
	snap, _ = desktopWatchdogSnapshot(scope.RunID)
	if snap.lastProgress.Equal(first.lastProgress) {
		t.Fatal("new output not counted")
	}
}
