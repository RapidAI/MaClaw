package guiapp

import (
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/goal"
)

func newGoalBarApp(t *testing.T) (*App, *goal.Store, *GoalContinuationEngine) {
	t.Helper()
	store := goal.NewStore("")
	app := &App{}
	engine := NewGoalContinuationEngine(store, app)
	engine.cooldown = time.Hour
	app.goalContinuation = engine
	app.imHandler = &IMMessageHandler{app: app, goalStore: store}
	t.Cleanup(func() { engine.CancelPending("desktop-user") })
	return app, store, engine
}

func goalBarPending(engine *GoalContinuationEngine, userID string) bool {
	engine.mu.Lock()
	_, pending := engine.scheduledTimers[userID]
	engine.mu.Unlock()
	return pending
}

func TestSessionGoalBarContinueAndCancel(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const userID = "desktop-user"
	g, err := store.Set(userID, "提升20%")
	if err != nil {
		t.Fatal(err)
	}
	store.Pause(userID, g.GoalID)

	snap := app.ContinueSessionGoal(userID)
	if snap["visible"] != true || snap["status"] != string(goal.StatusActive) {
		t.Fatalf("continue snap = %#v", snap)
	}
	if !goalBarPending(engine, userID) {
		t.Fatal("continue should schedule the next turn")
	}
	got := store.Get(userID)
	if got == nil || got.Status != goal.StatusActive || !got.ShouldContinue() {
		t.Fatalf("goal after continue = %+v", got)
	}

	store.UpdateStatus(userID, g.GoalID, goal.StatusFailed, "编译失败")
	engine.CancelPending(userID)
	snap = app.ContinueSessionGoal(userID)
	if snap["status"] != string(goal.StatusActive) || !goalBarPending(engine, userID) {
		t.Fatalf("failed continue snap=%#v pending=%v", snap, goalBarPending(engine, userID))
	}

	snap = app.PauseSessionGoal(userID)
	if snap["status"] != string(goal.StatusPaused) || goalBarPending(engine, userID) {
		t.Fatalf("pause snap=%#v pending=%v", snap, goalBarPending(engine, userID))
	}

	snap = app.CancelSessionGoal(userID)
	if snap["visible"] != false || store.Get(userID) != nil {
		t.Fatalf("cancel snap=%#v goal=%+v", snap, store.Get(userID))
	}
}

func TestSessionGoalBarContinuePastBudget(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const userID = "desktop-user"
	g, err := store.Set(userID, "keep going")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < g.MaxTurns; i++ {
		if !store.AccountUsage(userID, g.GoalID, 1, 1) {
			t.Fatalf("account turn %d", i)
		}
	}
	if got := store.Get(userID); got == nil || got.Status != goal.StatusBudgetLimit || got.ShouldContinue() {
		t.Fatalf("expected a turn-limit stop, got %+v", got)
	}

	snap := app.ContinueSessionGoal(userID)
	got := store.Get(userID)
	if snap["visible"] != true || got == nil || got.Status != goal.StatusActive || !got.ShouldContinue() {
		t.Fatalf("snap=%#v goal=%+v", snap, got)
	}
	if !goalBarPending(engine, userID) {
		t.Fatal("budget continue should schedule")
	}
}

func TestSessionGoalBarUpdateObjectiveKeepsTheSameGoal(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const userID = "desktop-user"
	g, err := store.Set(userID, "old command")
	if err != nil {
		t.Fatal(err)
	}
	store.Pause(userID, g.GoalID)

	snap := app.UpdateSessionGoal(userID, "  new command  ")
	if snap["objective"] != "new command" || snap["status"] != string(goal.StatusPaused) || snap["goal_id"] != g.GoalID {
		t.Fatalf("update snap = %#v", snap)
	}
	if goalBarPending(engine, userID) {
		t.Fatal("editing the command must not start a turn")
	}
	if app.UpdateSessionGoal(userID, " ")["objective"] != "new command" {
		t.Fatal("an empty command must leave the goal text in place")
	}
}

func TestSessionGoalBarUpdateObjectiveKeepsDecomposedPlan(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	userID := stickyTestUserID(t)
	t.Cleanup(func() { engine.CancelPending(userID) })
	g, err := store.Set(userID, "old command")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlan(userID, g.GoalID, "understand the work", []goal.Step{{
		Title: "do the thing",
	}}, []string{"it works"}); err != nil {
		t.Fatal(err)
	}
	app.imHandler.storeStickyCodingWorkbenchMemory(userID, stickyCodingWorkbenchMemory{
		Kind:        "local",
		SessionPlan: "custom plan",
	})

	snap := app.UpdateSessionGoal(userID, "new command")
	if snap["objective"] != "new command" || snap["status"] != string(goal.StatusActive) {
		t.Fatalf("update snap = %#v", snap)
	}
	got := store.Get(userID)
	if got == nil || got.Objective != "new command" || got.Understanding != "understand the work" || len(got.Steps) != 1 || got.Steps[0].Title != "do the thing" {
		t.Fatalf("goal = %+v", got)
	}
	if mem := app.imHandler.getStickyCodingWorkbenchMemory(userID); mem.SessionPlan != "custom plan" {
		t.Fatalf("session plan = %q", mem.SessionPlan)
	}
	if goalBarPending(engine, userID) {
		t.Fatal("editing the command must not start a turn")
	}

	plainID := stickyTestUserID(t)
	t.Cleanup(func() { engine.CancelPending(plainID) })
	if _, err := store.Set(plainID, "line goal"); err != nil {
		t.Fatal(err)
	}
	app.imHandler.storeStickyCodingWorkbenchMemory(plainID, stickyCodingWorkbenchMemory{
		Kind:        "local",
		SessionPlan: "old line",
	})
	if snap := app.UpdateSessionGoal(plainID, "fresh command"); snap["objective"] != "fresh command" {
		t.Fatalf("plain snap = %#v", snap)
	}
	if mem := app.imHandler.getStickyCodingWorkbenchMemory(plainID); mem.SessionPlan != "fresh command" {
		t.Fatalf("plain session plan = %q", mem.SessionPlan)
	}
}

func TestSessionGoalBarMatchesFrontendSessionKey(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const stored = `desktop-user:F:\bird2`
	const asked = `desktop-user:F:/bird2`
	t.Cleanup(func() { engine.CancelPending(stored) })
	if _, err := store.Set(stored, "做愤怒的小鸟"); err != nil {
		t.Fatal(err)
	}
	snap := app.GetSessionGoalBar(asked)
	if snap["visible"] != true || snap["objective"] != "做愤怒的小鸟" || snap["user_id"] != stored {
		t.Fatalf("slash variant should show the stored goal: %#v", snap)
	}
	paused := app.PauseSessionGoal(asked)
	if paused["status"] != string(goal.StatusPaused) {
		t.Fatalf("pause = %#v", paused)
	}
	if got := store.Get(stored); got == nil || got.Status != goal.StatusPaused {
		t.Fatalf("stored goal = %+v", got)
	}
	if app.CancelSessionGoal(asked)["visible"] != false || store.Get(stored) != nil {
		t.Fatal("cancel from the frontend key should delete the stored goal")
	}
}

func TestSessionGoalBarDoesNotRetargetCleanedPath(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const bird = `desktop-user:F:\bird2`
	const other = `desktop-user:F:/other`
	t.Cleanup(func() {
		engine.CancelPending(bird)
		engine.CancelPending(other)
	})
	if _, err := store.Set(bird, "小鸟"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Set(other, "别的项目"); err != nil {
		t.Fatal(err)
	}
	snap := app.GetSessionGoalBar(`desktop-user:F:/bird2/../other`)
	if snap["visible"] == true {
		t.Fatalf("a cleaned path must not open another project: %#v", snap)
	}
	if app.CancelSessionGoal(`desktop-user:F:/bird2/../other`)["visible"] != false {
		t.Fatal("cancel of an unknown session should hide the bar")
	}
	if store.Get(bird) == nil || store.Get(other) == nil {
		t.Fatal("cancel must not delete a different project's goal")
	}
}

func TestSessionGoalBarPrefersLiveGoalOverFinishedSlashTwin(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const front = `desktop-user:F:/bird2`
	const back = `desktop-user:F:\bird2`
	t.Cleanup(func() {
		engine.CancelPending(front)
		engine.CancelPending(back)
	})
	finished, err := store.Set(front, "已经做完的")
	if err != nil {
		t.Fatal(err)
	}
	if !store.UpdateStatus(front, finished.GoalID, goal.StatusComplete, "done") {
		t.Fatal("complete")
	}
	if _, err := store.Set(back, "正在做的小鸟"); err != nil {
		t.Fatal(err)
	}

	snap := app.GetSessionGoalBar(front)
	if snap["visible"] != true || snap["objective"] != "正在做的小鸟" || snap["user_id"] != back {
		t.Fatalf("active slash twin should win over a finished one: %#v", snap)
	}
	if got := store.Get(front); got == nil || got.Status != goal.StatusComplete {
		t.Fatalf("finished twin = %+v", got)
	}
}

func TestSessionGoalBarPauseStaysOnTheLiveSlashTwin(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const front = `desktop-user:F:/bird2`
	const back = `desktop-user:F:\bird2`
	t.Cleanup(func() {
		engine.CancelPending(front)
		engine.CancelPending(back)
	})
	stale, err := store.Set(front, "旧的暂停")
	if err != nil {
		t.Fatal(err)
	}
	if !store.UpdateStatus(front, stale.GoalID, goal.StatusPaused, "旧的") {
		t.Fatal("pause stale")
	}
	time.Sleep(2 * time.Millisecond)
	if _, err := store.Set(back, "正在做的小鸟"); err != nil {
		t.Fatal(err)
	}

	snap := app.GetSessionGoalBar(front)
	if snap["visible"] != true || snap["objective"] != "正在做的小鸟" || snap["user_id"] != back {
		t.Fatalf("active slash twin should win over an older pause: %#v", snap)
	}
	paused := app.PauseSessionGoal(front)
	if paused["objective"] != "正在做的小鸟" || paused["status"] != string(goal.StatusPaused) {
		t.Fatalf("pause = %#v", paused)
	}
	again := app.GetSessionGoalBar(front)
	if again["objective"] != "正在做的小鸟" || again["user_id"] != back {
		t.Fatalf("pause must not fall through to the older twin: %#v", again)
	}
	if got := store.Get(front); got == nil || got.Objective != "旧的暂停" || got.Status != goal.StatusPaused {
		t.Fatalf("older twin changed: %+v", got)
	}
}

func TestContinueSessionGoalKeepsSpentPlanMiss(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const stored = `desktop-user:F:\bird2`
	const asked = `desktop-user:F:/bird2`
	t.Cleanup(func() {
		engine.CancelPending(stored)
		engine.CancelPending(asked)
	})
	g, err := store.Set(stored, "做愤怒的小鸟")
	if err != nil {
		t.Fatal(err)
	}
	if !store.Pause(stored, g.GoalID) {
		t.Fatal("pause")
	}
	engine.scheduleDelayed(asked, "stale-twin")
	engine.mu.Lock()
	engine.planMiss[stored] = goalPlanMiss{goalID: g.GoalID, retried: true}
	engine.planMiss[asked] = goalPlanMiss{goalID: "twin", retried: true}
	engine.mu.Unlock()

	snap := app.ContinueSessionGoal(asked)
	if snap["visible"] != true || snap["status"] != string(goal.StatusActive) || snap["user_id"] != stored {
		t.Fatalf("continue = %#v", snap)
	}
	engine.mu.Lock()
	kept := engine.planMiss[stored]
	_, twinMiss := engine.planMiss[asked]
	_, twinTimer := engine.scheduledTimers[asked]
	_, scheduled := engine.scheduledTimers[stored]
	engine.mu.Unlock()
	if !kept.retried || kept.goalID != g.GoalID || twinMiss || twinTimer || !scheduled {
		t.Fatalf("plan miss kept=%+v twinMiss=%v twinTimer=%v scheduled=%v", kept, twinMiss, twinTimer, scheduled)
	}

	paused := app.PauseSessionGoal(asked)
	if paused["status"] != string(goal.StatusPaused) {
		t.Fatalf("pause = %#v", paused)
	}
	engine.mu.Lock()
	_, missLeft := engine.planMiss[stored]
	_, stillScheduled := engine.scheduledTimers[stored]
	engine.mu.Unlock()
	if missLeft || stillScheduled {
		t.Fatalf("pause should drop the spent miss and the timer miss=%v scheduled=%v", missLeft, stillScheduled)
	}
}

func TestSessionGoalBarHidesFinishedContinuationBeforeHandler(t *testing.T) {
	app, store, _ := newGoalBarApp(t)
	const stored = `desktop-user:F:\bird2`
	finished, err := store.Set(stored, "引擎里已完成")
	if err != nil {
		t.Fatal(err)
	}
	if !store.UpdateStatus(stored, finished.GoalID, goal.StatusComplete, "done") {
		t.Fatal("complete")
	}
	other := goal.NewStore("")
	app.imHandler.goalStore = other
	if _, err := other.Set(stored, "处理器里还在跑"); err != nil {
		t.Fatal(err)
	}
	snap := app.GetSessionGoalBar(`desktop-user:F:/bird2`)
	if snap["visible"] == true {
		t.Fatalf("a finished continuation row stays ahead of the handler store: %#v", snap)
	}
}

func TestSessionGoalBarReadsHandlerStoreAndDriveCase(t *testing.T) {
	app, _, engine := newGoalBarApp(t)
	other := goal.NewStore("")
	app.imHandler.goalStore = other
	const stored = `desktop-user:F:\bird2`
	t.Cleanup(func() { engine.CancelPending(stored) })
	if _, err := other.Set(stored, "只在处理器里"); err != nil {
		t.Fatal(err)
	}
	snap := app.GetSessionGoalBar(`desktop-user:f:/bird2`)
	if snap["visible"] != true || snap["objective"] != "只在处理器里" || snap["user_id"] != stored {
		t.Fatalf("handler store + drive case snap=%#v", snap)
	}
	if app.PauseSessionGoal(`desktop-user:f:/bird2`)["status"] != string(goal.StatusPaused) {
		t.Fatal("pause should update the handler store")
	}
	if got := other.Get(stored); got == nil || got.Status != goal.StatusPaused {
		t.Fatalf("handler goal=%+v", got)
	}
}

func TestGoalBarCandidateIDsKeepBothSeparators(t *testing.T) {
	ids := goalBarCandidateIDs(`desktop-user:F:/bird2`)
	if !goalBarIDsContain(ids, `desktop-user:F:/bird2`) || !goalBarIDsContain(ids, `desktop-user:F:\bird2`) {
		t.Fatalf("ids = %#v", ids)
	}
	taskIDs := goalBarCandidateIDs(`desktop-user:F:/bird2:task:abc`)
	for _, id := range taskIDs {
		if !strings.Contains(id, ":task:") {
			t.Fatalf("task session must keep its suffix: %#v", taskIDs)
		}
	}
	if flipProjectSessionSlashes("desktop-user:expert:builtin-latex") != "" {
		t.Fatal("expert sessions are not project paths")
	}
}

func goalBarIDsContain(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func TestSessionGoalBarHidesCompletedGoal(t *testing.T) {
	app, store, _ := newGoalBarApp(t)
	const userID = "desktop-user"
	g, err := store.Set(userID, "done work")
	if err != nil {
		t.Fatal(err)
	}
	store.UpdateStatus(userID, g.GoalID, goal.StatusComplete, "done")
	snap := app.GetSessionGoalBar(userID)
	if snap["visible"] != false {
		t.Fatalf("completed goal should hide the bar: %#v", snap)
	}
	if app.ContinueSessionGoal(userID)["visible"] != false {
		t.Fatal("continue must not revive a completed goal")
	}
	if got := store.Get(userID); got == nil || got.Status != goal.StatusComplete {
		t.Fatalf("completed goal changed: %+v", got)
	}
}

func TestSessionGoalBarFreezesElapsedAfterFailure(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const userID = "desktop-user"
	t.Cleanup(func() { engine.CancelPending(userID) })
	g, err := store.Set(userID, "提升性能")
	if err != nil {
		t.Fatal(err)
	}
	running := app.GetSessionGoalBar(userID)
	if running["visible"] != true || running["stopped_at"] != int64(0) || running["clock_hold_seconds"] != int64(0) {
		t.Fatalf("running clock = %#v", running)
	}
	if app.PauseSessionGoal(userID)["stopped_at"] != int64(0) {
		t.Fatal("pause should keep the clock running")
	}
	store.UpdateStatus(userID, g.GoalID, goal.StatusFailed, "编译失败")
	failed := app.GetSessionGoalBar(userID)
	stopped, _ := failed["stopped_at"].(int64)
	created, _ := failed["created_at"].(int64)
	if failed["status"] != string(goal.StatusFailed) || stopped == 0 || stopped < created {
		t.Fatalf("failed clock created=%d stopped=%#v snap=%#v", created, failed["stopped_at"], failed)
	}
	if !store.UpdateObjective(userID, g.GoalID, "失败后改一句") {
		t.Fatal("edit")
	}
	edited := app.GetSessionGoalBar(userID)
	if edited["stopped_at"] != stopped {
		t.Fatalf("edit moved the clock from %d to %#v", stopped, edited["stopped_at"])
	}
}

func TestSessionGoalBarRemoteSSHStaysPaused(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	userID := stickyTestUserID(t)
	t.Cleanup(func() { engine.CancelPending(userID) })
	g, err := store.Set(userID, "remote fix")
	if err != nil {
		t.Fatal(err)
	}
	store.UpdateStatus(userID, g.GoalID, goal.StatusFailed, "目标失败：远程 SSH 未连接")
	app.imHandler.storeStickyCodingWorkbenchMemory(userID, stickyCodingWorkbenchMemory{
		Kind:          "remote",
		RemoteHost:    "dev.example",
		RemoteWorkDir: "/src/app",
	})

	snap := app.ContinueSessionGoal(userID)
	if snap["visible"] != true || snap["status"] != string(goal.StatusPaused) {
		t.Fatalf("ssh continue snap = %#v", snap)
	}
	summary, _ := snap["summary"].(string)
	if !strings.Contains(summary, "SSH") {
		t.Fatalf("summary = %q", summary)
	}
	if goalBarPending(engine, userID) {
		t.Fatal("dead SSH must not schedule a desktop turn")
	}
	got := store.Get(userID)
	if got == nil || got.Status != goal.StatusPaused {
		t.Fatalf("goal = %+v", got)
	}
}

func TestContinueSessionGoalRunsPastUserStop(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const userID = "desktop-user"
	g, err := store.Set(userID, "keep working")
	if err != nil {
		t.Fatal(err)
	}
	app.imHandler.markTaskCancelledByUser(userID)
	app.imHandler.maybeScheduleGoalContinuation(userID, &IMAgentResponse{Text: "partial", userStopped: true}, "desktop")
	paused := store.Get(userID)
	if paused == nil || paused.GoalID != g.GoalID || paused.Status != goal.StatusPaused || goalBarPending(engine, userID) {
		t.Fatalf("stop should pause without a timer: pending=%v goal=%+v", goalBarPending(engine, userID), paused)
	}

	snap := app.ContinueSessionGoal(userID)
	if snap["status"] != string(goal.StatusActive) || !goalBarPending(engine, userID) {
		t.Fatalf("continue snap=%#v pending=%v", snap, goalBarPending(engine, userID))
	}
	if !app.imHandler.hasCancelledTaskBoundary(userID) {
		t.Fatal("continue must leave the cancel fence for the next ordinary message")
	}
	host := IMUserMessage{UserID: userID, Platform: "goal-continuation", Text: "[系统续接][execute]"}
	if got := app.imHandler.rejectCancelledTaskTurn(host); got != nil {
		t.Fatalf("host continuation was rejected: %+v", got)
	}
	ordinary := IMUserMessage{UserID: userID, Platform: "desktop", Text: "hello"}
	if got := app.imHandler.rejectCancelledTaskTurn(ordinary); got == nil || !strings.Contains(got.Text, "Task cancelled") {
		t.Fatalf("ordinary message = %+v", got)
	}
	_, fresh, clearUI := app.imHandler.applyUnifiedTaskContextDecision(host, host.Text, explicitTaskSlotDecision{}, nil, nil, "", false, false, false)
	if fresh || clearUI || !app.imHandler.hasCancelledTaskBoundary(userID) {
		t.Fatalf("admitted continuation changed the task: fresh=%v clear=%v fence=%v", fresh, clearUI, app.imHandler.hasCancelledTaskBoundary(userID))
	}

	app.imHandler.maybeScheduleGoalContinuation(userID, &IMAgentResponse{Text: "Task cancelled.", Error: "cancelled", userStopped: true}, "goal-continuation")
	kept := store.Get(userID)
	if kept == nil || kept.GoalID != g.GoalID || kept.Status != goal.StatusActive || !goalBarPending(engine, userID) {
		t.Fatalf("dying turn undid continue: pending=%v goal=%+v", goalBarPending(engine, userID), kept)
	}

	engine.CancelPending(userID)
	app.imHandler.maybeScheduleGoalContinuation(userID, &IMAgentResponse{Text: "step done", TraceEventCount: 1}, "goal-continuation")
	if got := store.Get(userID); got == nil || got.Status != goal.StatusActive || !goalBarPending(engine, userID) {
		t.Fatalf("admitted turn should schedule the next one: pending=%v goal=%+v", goalBarPending(engine, userID), got)
	}

	app.imHandler.markTaskCancelledByUser(userID)
	app.imHandler.maybeScheduleGoalContinuation(userID, &IMAgentResponse{Text: "Task cancelled.", userStopped: true}, "goal-continuation")
	stopped := store.Get(userID)
	if stopped == nil || stopped.GoalID != g.GoalID || stopped.Status != goal.StatusPaused || goalBarPending(engine, userID) {
		t.Fatalf("a later stop should pause again: pending=%v goal=%+v", goalBarPending(engine, userID), stopped)
	}
	if !strings.Contains(stopped.Summary, "停止") || strings.Contains(stopped.Summary, "SSH") || strings.Contains(stopped.Summary, "远程") {
		t.Fatalf("summary = %q", stopped.Summary)
	}
}

func TestPauseSessionGoalStopsAnUnpublishedTurnAndReleasesTheQuestion(t *testing.T) {
	app, store, _ := newGoalBarApp(t)
	const userID = "desktop-user"
	if _, err := store.Set(userID, "keep going"); err != nil {
		t.Fatal(err)
	}
	app.imHandler.setPendingForegroundText(userID, "goal turn in flight")
	app.imHandler.pendingAskUser.Store(userID, "which file?")

	if app.PauseSessionGoal(userID)["status"] != string(goal.StatusPaused) {
		t.Fatal("pause did not pause")
	}
	if _, ok := app.imHandler.pendingAskUser.Load(userID); ok {
		t.Fatal("pause left the question owning the next message")
	}
	if app.imHandler.hasCancelledTaskBoundary(userID) {
		t.Fatal("pause must not fence the next chat message")
	}
	ctx := NewLoopContext("goal-bar-unpublished", 4, nil)
	if !app.imHandler.takeGoalBarStop(userID, ctx) || !ctx.IsCancelled() {
		t.Fatal("the unpublished turn should stop itself once")
	}
	if app.imHandler.takeGoalBarStop(userID, NewLoopContext("again", 4, nil)) {
		t.Fatal("the stop mark is one shot")
	}

	app.imHandler.setPendingForegroundText(userID, "held turn")
	app.imHandler.noteGoalBarStopPending(userID)
	app.imHandler.setPendingForegroundText(userID, "next chat message")
	next := NewLoopContext("next", 4, nil)
	if app.imHandler.takeGoalBarStop(userID, next) || next.IsCancelled() {
		t.Fatal("a later message must not inherit the stop")
	}
}

func TestPauseSessionGoalRejectsAQuestionTheStoppedTurnPublishes(t *testing.T) {
	app, store, _ := newGoalBarApp(t)
	const userID = "desktop-user"
	if _, err := store.Set(userID, "keep going"); err != nil {
		t.Fatal(err)
	}
	h := app.imHandler
	h.setPendingForegroundText(userID, "goal turn in flight")
	ctx := NewLoopContext("goal-bar-ask", 4, nil)
	h.setSessionLoopCtx(userID, ctx)
	h.commitPendingAskUser(userID, &AskUserRequest{Question: "which file?"}, nil, nil)
	if _, ok := h.pendingAskUser.Load(userID); !ok {
		t.Fatal("an unmarked turn should publish its question")
	}

	if app.PauseSessionGoal(userID)["status"] != string(goal.StatusPaused) {
		t.Fatal("pause did not pause")
	}
	if !ctx.IsCancelled() {
		t.Fatal("pause should stop the live turn")
	}
	if _, ok := h.pendingAskUser.Load(userID); ok {
		t.Fatal("pause left the question owning the next message")
	}
	if h.hasCancelledTaskBoundary(userID) {
		t.Fatal("pause must not fence the next chat message")
	}
	// A published loop is not the one-shot unpublished stop. Taking that mark
	// must not reopen a question for this generation.
	if h.takeGoalBarStop(userID, NewLoopContext("later", 4, nil)) {
		t.Fatal("a published loop is not the one-shot unpublished stop")
	}
	h.commitPendingAskUser(userID, &AskUserRequest{Question: "which file?"}, nil, nil)
	if _, ok := h.pendingAskUser.Load(userID); ok {
		t.Fatal("the stopped turn published a question after pause")
	}

	h.setPendingForegroundText(userID, "next chat message")
	h.commitPendingAskUser(userID, &AskUserRequest{Question: "which model?"}, nil, nil)
	raw, ok := h.pendingAskUser.Load(userID)
	state, _ := raw.(*pendingAskUserState)
	if !ok || state == nil || state.Question != "which model?" {
		t.Fatalf("a later message should still be able to ask: %#v", raw)
	}
	if h.hasCancelledTaskBoundary(userID) {
		t.Fatal("pause must not fence the next chat message")
	}
}

func TestPauseSessionGoalRejectsAQuestionAfterTheTurnUnlocks(t *testing.T) {
	app, store, _ := newGoalBarApp(t)
	const userID = "desktop-user"
	if _, err := store.Set(userID, "keep going"); err != nil {
		t.Fatal(err)
	}
	h := app.imHandler
	h.setPendingForegroundText(userID, "goal turn in flight")
	h.setPendingForegroundText(userID, "")

	if app.PauseSessionGoal(userID)["status"] != string(goal.StatusPaused) {
		t.Fatal("pause did not pause")
	}
	if h.hasCancelledTaskBoundary(userID) {
		t.Fatal("pause must not fence the next chat message")
	}
	h.commitPendingAskUser(userID, &AskUserRequest{Question: "which file?"}, nil, nil)
	if _, ok := h.pendingAskUser.Load(userID); ok {
		t.Fatal("the finished turn published a question after pause")
	}

	h.setPendingForegroundText(userID, "next chat message")
	h.commitPendingAskUser(userID, &AskUserRequest{Question: "which model?"}, nil, nil)
	raw, ok := h.pendingAskUser.Load(userID)
	state, _ := raw.(*pendingAskUserState)
	if !ok || state == nil || state.Question != "which model?" {
		t.Fatalf("a later message should still be able to ask: %#v", raw)
	}
}

func TestPauseSessionGoalDropsAProseQuestionFromTheStoppedTurn(t *testing.T) {
	app, store, _ := newGoalBarApp(t)
	const userID = "desktop-user"
	if _, err := store.Set(userID, "keep going"); err != nil {
		t.Fatal(err)
	}
	h := app.imHandler
	older := &pendingUserReplyState{Question: "which model?"}

	synctest.Test(t, func(t *testing.T) {
		h.pendingUserReply.Store(userID, older)
		h.setPendingForegroundText(userID, "goal turn in flight")
		if app.PauseSessionGoal(userID)["status"] != string(goal.StatusPaused) {
			t.Fatal("pause did not pause")
		}
		var calls atomic.Int32
		h.pendingReplyPromptClassifier = func(string) (bool, error) {
			calls.Add(1)
			return false, nil
		}
		h.updatePendingUserReplyFromHistory(userID, goalBarPlainHistory(""), &IMAgentResponse{})
		h.updatePendingUserReplyFromHistory(userID, goalBarPlainHistory("which file?"), &IMAgentResponse{Text: "which file?"})
		synctest.Wait()
		if calls.Load() != 0 {
			t.Fatalf("stopped turn classified %d times", calls.Load())
		}
		if got, ok := goalBarPendingQuestion(h, userID); !ok || got != "which model?" {
			t.Fatalf("stopped turn changed the waiting question: %q ok=%v", got, ok)
		}
		if h.hasCancelledTaskBoundary(userID) {
			t.Fatal("pause must not fence the next chat message")
		}

		g := store.Get(userID)
		if g == nil || !store.ReleaseForContinue(userID, g.GoalID) {
			t.Fatal("could not re-arm the goal")
		}
		release := make(chan struct{})
		h.pendingReplyPromptClassifier = func(string) (bool, error) {
			<-release
			return false, nil
		}
		h.setPendingForegroundText(userID, "classifying turn")
		h.updatePendingUserReplyFromHistory(userID, goalBarPlainHistory("which file?"), &IMAgentResponse{Text: "which file?"})
		synctest.Wait()
		if app.PauseSessionGoal(userID)["status"] != string(goal.StatusPaused) {
			t.Fatal("pause during classification did not pause")
		}
		close(release)
		synctest.Wait()
		if got, ok := goalBarPendingQuestion(h, userID); !ok || got != "which model?" {
			t.Fatalf("a negative classification after pause deleted the waiting question: %q ok=%v", got, ok)
		}

		g = store.Get(userID)
		if g == nil || !store.ReleaseForContinue(userID, g.GoalID) {
			t.Fatal("could not re-arm the goal")
		}
		release = make(chan struct{})
		h.pendingReplyPromptClassifier = func(string) (bool, error) {
			<-release
			return true, nil
		}
		h.setPendingForegroundText(userID, "classifying again")
		h.updatePendingUserReplyFromHistory(userID, goalBarPlainHistory("which directory?"), &IMAgentResponse{Text: "which directory?"})
		synctest.Wait()
		h.setPendingForegroundText(userID, "newer message")
		if app.PauseSessionGoal(userID)["status"] != string(goal.StatusPaused) {
			t.Fatal("pause during a newer message did not pause")
		}
		close(release)
		synctest.Wait()
		if got, ok := goalBarPendingQuestion(h, userID); !ok || got != "which model?" {
			t.Fatalf("an older turn published a question after pause: %q ok=%v", got, ok)
		}
		if h.hasCancelledTaskBoundary(userID) {
			t.Fatal("pause must not fence the next chat message")
		}

		g = store.Get(userID)
		if g == nil || !store.ReleaseForContinue(userID, g.GoalID) {
			t.Fatal("could not re-arm the goal")
		}
		h.pendingUserReply.Delete(userID)
		h.pendingReplyPromptClassifier = func(string) (bool, error) {
			return true, nil
		}
		h.setPendingForegroundText(userID, "next chat message")
		h.updatePendingUserReplyFromHistory(userID, goalBarPlainHistory("which plan?"), &IMAgentResponse{Text: "which plan?"})
		synctest.Wait()
		if got, ok := goalBarPendingQuestion(h, userID); !ok || got != "which plan?" {
			t.Fatalf("a later message should still be able to ask: %q ok=%v", got, ok)
		}
	})
}

func goalBarPlainHistory(assistant string) []agent.ConversationEntry {
	history := []agent.ConversationEntry{{Role: "user", Content: "help me pick"}}
	if assistant != "" {
		history = append(history, agent.ConversationEntry{Role: "assistant", Content: assistant})
	}
	return history
}

func goalBarPendingQuestion(h *IMMessageHandler, userID string) (string, bool) {
	raw, ok := h.pendingUserReply.Load(userID)
	if !ok {
		return "", false
	}
	state, _ := raw.(*pendingUserReplyState)
	if state == nil {
		return "", true
	}
	return state.Question, true
}

func TestPauseSessionGoalDropsADispatchedContinuation(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const userID = "desktop-user"
	g, err := store.Set(userID, "keep going")
	if err != nil {
		t.Fatal(err)
	}
	const requestID = "goal-cont-test-1"
	if !engine.admitContinuationIfContinuable(userID, g.GoalID, requestID) {
		t.Fatal("an active goal should admit the continuation")
	}
	if engine.continuationAdmitRevoked(requestID) || engine.continuationAdmitRevoked("ordinary-chat") {
		t.Fatal("a fresh admit and an ordinary request must stay open")
	}

	if app.PauseSessionGoal(userID)["status"] != string(goal.StatusPaused) {
		t.Fatal("pause did not pause")
	}
	if !engine.continuationAdmitRevoked(requestID) {
		t.Fatal("pause should revoke the continuation that already passed the cooldown check")
	}
	if engine.continuationAdmitRevoked("ordinary-chat") {
		t.Fatal("pause must not treat an ordinary request as a continuation")
	}
	if engine.admitContinuationIfContinuable(userID, g.GoalID, "goal-cont-while-paused") {
		t.Fatal("a paused goal must not admit another continuation")
	}
	if app.imHandler.hasCancelledTaskBoundary(userID) {
		t.Fatal("pause must not fence the next chat message")
	}
	// Continue re-arms the goal. It must not make the revoked dispatch valid.
	if app.ContinueSessionGoal(userID)["status"] != string(goal.StatusActive) {
		t.Fatal("continue did not re-arm")
	}
	if !engine.continuationAdmitRevoked(requestID) {
		t.Fatal("continue revived the continuation pause already stopped")
	}
	if app.PauseSessionGoal(userID)["status"] != string(goal.StatusPaused) {
		t.Fatal("pause did not pause again")
	}
	if !engine.continuationAdmitRevoked(requestID) {
		t.Fatal("the dispatched continuation must stay revoked")
	}
	paused := store.Get(userID)
	if paused == nil || paused.GoalID != g.GoalID || paused.Status != goal.StatusPaused || paused.Summary != "用户已暂停" {
		t.Fatalf("goal after pause = %+v", paused)
	}

	app.imHandler.pendingAskUser.Store(userID, &pendingAskUserState{Question: "which file?"})
	engine.runForegroundContinuation(app.imHandler, IMUserMessage{
		RequestID: requestID,
		UserID:    userID,
		Platform:  "goal-continuation",
		Text:      "[系统续接] keep going",
	})
	if engine.continuationAdmitRevoked(requestID) {
		t.Fatal("the dropped turn should retire its ticket")
	}
	got := store.Get(userID)
	if got == nil || got.GoalID != g.GoalID || got.Status != goal.StatusPaused || got.Summary != "用户已暂停" {
		t.Fatalf("dropped turn changed the goal: %+v", got)
	}
	raw, ok := app.imHandler.pendingAskUser.Load(userID)
	state, _ := raw.(*pendingAskUserState)
	if !ok || state == nil || state.Question != "which file?" {
		t.Fatal("the dropped turn consumed the question")
	}
	if app.imHandler.hasCancelledTaskBoundary(userID) {
		t.Fatal("pause must not fence the next chat message")
	}

	if app.ContinueSessionGoal(userID)["status"] != string(goal.StatusActive) {
		t.Fatal("continue after the drop did not re-arm")
	}
	const nextID = "goal-cont-test-2"
	if !engine.admitContinuationIfContinuable(userID, g.GoalID, nextID) || engine.continuationAdmitRevoked(nextID) {
		t.Fatal("the next fire should admit a new continuation")
	}
	if app.imHandler.hasCancelledTaskBoundary(userID) {
		t.Fatal("continue must not fence the next chat message")
	}

	const back = `desktop-user:F:\bird2`
	const front = `desktop-user:F:/bird2`
	twin, err := store.Set(back, "twin goal")
	if err != nil {
		t.Fatal(err)
	}
	const twinReq = "goal-cont-twin"
	if !engine.admitContinuationIfContinuable(back, twin.GoalID, twinReq) {
		t.Fatal("the slash twin should admit")
	}
	if app.PauseSessionGoal(front)["status"] != string(goal.StatusPaused) {
		t.Fatal("pause on the other slash spelling did not pause")
	}
	if !engine.continuationAdmitRevoked(twinReq) {
		t.Fatal("pause on the slash twin should revoke the admitted continuation")
	}
	if app.imHandler.hasCancelledTaskBoundary(back) || app.imHandler.hasCancelledTaskBoundary(front) {
		t.Fatal("pause must not fence the next chat message")
	}
}

func TestAdmittedContinuationStopsWhenTheGoalIsGone(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const userID = "desktop-user"
	g, err := store.Set(userID, "keep going")
	if err != nil {
		t.Fatal(err)
	}
	const removedID = "goal-cont-removed"
	if !engine.admitContinuationIfContinuable(userID, g.GoalID, removedID) {
		t.Fatal("an active goal should admit")
	}
	store.Clear(userID)
	if engine.admittedContinuationGoal(userID, g.GoalID, removedID) != nil {
		t.Fatal("a removed goal must not be fired")
	}
	if engine.continuationAdmitRevoked(removedID) {
		t.Fatal("the dropped fire should retire its ticket")
	}

	g, err = store.Set(userID, "keep going")
	if err != nil {
		t.Fatal(err)
	}
	const pausedID = "goal-cont-paused-fire"
	if !engine.admitContinuationIfContinuable(userID, g.GoalID, pausedID) {
		t.Fatal("an active goal should admit")
	}
	store.Pause(userID, g.GoalID)
	if engine.admittedContinuationGoal(userID, g.GoalID, pausedID) != nil {
		t.Fatal("a paused goal must not open the fire that just lost it")
	}
	if !store.ReleaseForContinue(userID, g.GoalID) {
		t.Fatal("could not re-arm")
	}
	const liveID = "goal-cont-live-fire"
	if !engine.admitContinuationIfContinuable(userID, g.GoalID, liveID) {
		t.Fatal("continue should let the next fire admit")
	}
	if got := engine.admittedContinuationGoal(userID, g.GoalID, liveID); got == nil || got.GoalID != g.GoalID {
		t.Fatalf("an active goal should still fire: %+v", got)
	}
	if engine.continuationAdmitRevoked(liveID) {
		t.Fatal("a live fire must keep its ticket")
	}
	engine.finishContinuationAdmit(liveID)

	app.remoteInfraReady.Store(true)
	engine.dispatchContinuation(userID, g.GoalID, goalCorrectionNone)
	failed := store.Get(userID)
	if failed == nil || failed.Status != goal.StatusFailed {
		t.Fatalf("a missing assistant channel should fail the goal: %+v", failed)
	}
	engine.mu.Lock()
	left := len(engine.continuationTicket)
	engine.mu.Unlock()
	if left != 0 {
		t.Fatalf("a fire that never started left %d tickets", left)
	}
	if app.imHandler.hasCancelledTaskBoundary(userID) {
		t.Fatal("a failed fire must not fence the next chat message")
	}
}

func TestGoalContinuationDoesNotInterruptTheLiveTurn(t *testing.T) {
	app, _, _ := newGoalBarApp(t)
	const userID = "desktop-user"
	h := app.imHandler
	h.memory = agent.NewConversationMemory()
	h.interruptHandler = &imInterruptHandler{handler: h}
	ctx := NewLoopContext("live-turn", 4, nil)
	h.setSessionLoopCtx(userID, ctx)
	if !h.hasActiveLoopForUser(userID) {
		t.Fatal("the live turn should accept an interrupt")
	}
	// The continuation contract is full of these words. The interrupt
	// scheduler treats them as the person stopping the live turn.
	const text = "[系统续接][execute] 不要重做。不要开始后续步骤。"

	user := h.enterIMMessageSerializationBoundary(IMUserMessage{
		UserID: userID, Platform: "desktop", Text: text,
	}, nil, nil, nil, explicitTaskSlotDecision{})
	if !user.Handled {
		t.Fatal("the same words from the user should still interrupt")
	}
	if h.hasCancelledTaskBoundary(userID) || ctx.IsCancelled() {
		t.Fatal("this wording asks before it cancels")
	}

	host := h.enterIMMessageSerializationBoundary(IMUserMessage{
		UserID: userID, Platform: "goal-continuation", Text: text, RequestID: "goal-cont-host",
	}, nil, nil, nil, explicitTaskSlotDecision{})
	defer host.Unlock()
	if host.Handled {
		t.Fatal("a goal continuation must wait for the session, not interrupt the live turn")
	}
	if h.hasCancelledTaskBoundary(userID) || ctx.IsCancelled() {
		t.Fatal("a goal continuation must not fence or cancel the live turn")
	}
	if _, injected := h.pendingInjection.Load(userID); injected {
		t.Fatal("a goal continuation must not steer the live turn")
	}
}

func TestPauseSessionGoalStopsTheTurnAndRejectsALateSchedule(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const userID = "desktop-user"
	g, err := store.Set(userID, "keep going")
	if err != nil {
		t.Fatal(err)
	}
	ctx := NewLoopContext("goal-bar-pause", 4, nil)
	app.imHandler.sessionLoops.Store(userID, &sessionLoopState{loopCtx: ctx})

	snap := app.PauseSessionGoal(userID)
	if snap["status"] != string(goal.StatusPaused) || snap["objective"] != "keep going" {
		t.Fatalf("pause = %#v", snap)
	}
	if !ctx.IsCancelled() {
		t.Fatal("pause should stop the live turn")
	}
	if app.imHandler.hasCancelledTaskBoundary(userID) {
		t.Fatal("pause must not fence the next chat message")
	}
	engine.scheduleDelayed(userID, g.GoalID)
	if goalBarPending(engine, userID) {
		t.Fatal("a paused goal must not accept another turn")
	}

	if app.ContinueSessionGoal(userID)["status"] != string(goal.StatusActive) || !goalBarPending(engine, userID) {
		t.Fatal("continue should arm the same goal")
	}
	app.imHandler.maybeScheduleGoalContinuation(userID, &IMAgentResponse{
		Text: "cancelled", Error: "cancelled", userStopped: true,
	}, "goal-continuation")
	got := store.Get(userID)
	if got == nil || got.Status != goal.StatusActive || !goalBarPending(engine, userID) {
		t.Fatalf("dying turn undid continue: pending=%v goal=%+v", goalBarPending(engine, userID), got)
	}
}

func TestCancelSessionGoalStopsTheLiveTurn(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const userID = "desktop-user"
	g, err := store.Set(userID, "stop this")
	if err != nil {
		t.Fatal(err)
	}
	engine.scheduleDelayed(userID, g.GoalID)
	ctx := NewLoopContext("goal-bar-cancel", 4, nil)
	app.imHandler.sessionLoops.Store(userID, &sessionLoopState{loopCtx: ctx})

	snap := app.CancelSessionGoal(userID)
	if snap["visible"] != false || store.Get(userID) != nil || goalBarPending(engine, userID) {
		t.Fatalf("cancel = %#v pending=%v goal=%+v", snap, goalBarPending(engine, userID), store.Get(userID))
	}
	if !ctx.IsCancelled() {
		t.Fatal("cancel should stop the live turn")
	}
	if app.imHandler.hasCancelledTaskBoundary(userID) {
		t.Fatal("cancel must not fence the next chat message")
	}
	engine.scheduleDelayed(userID, g.GoalID)
	if goalBarPending(engine, userID) {
		t.Fatal("a deleted goal must not accept another turn")
	}
}

func TestUpdateSessionGoalReachesTheLiveTurn(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const userID = "desktop-user"
	g, err := store.Set(userID, "old command")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlan(userID, g.GoalID, "understand the work", []goal.Step{{
		Title: "do the thing",
	}}, []string{"it works"}); err != nil {
		t.Fatal(err)
	}
	ctx := NewLoopContext("goal-bar-edit", 4, nil)
	app.imHandler.sessionLoops.Store(userID, &sessionLoopState{loopCtx: ctx})

	snap := app.UpdateSessionGoal(userID, "new command")
	if snap["objective"] != "new command" || snap["goal_id"] != g.GoalID || snap["status"] != string(goal.StatusActive) {
		t.Fatalf("update = %#v", snap)
	}
	got := store.Get(userID)
	if got == nil || got.Objective != "new command" || len(got.Steps) != 1 || got.Steps[0].Title != "do the thing" {
		t.Fatalf("plan changed: %+v", got)
	}
	raw, ok := app.imHandler.pendingInjection.Load(userID)
	text, _ := raw.(string)
	if !ok || !strings.Contains(text, "new command") {
		t.Fatalf("live turn did not receive the new command: %#v", raw)
	}
	if ctx.IsCancelled() {
		t.Fatal("edit must leave the live turn running")
	}
	if goalBarPending(engine, userID) {
		t.Fatal("edit must not start a turn")
	}
	if _, held := app.imHandler.goalBarSteer.Load(userID); held {
		t.Fatal("a published loop should take the command immediately")
	}
}

func TestUpdateSessionGoalReachesThePendingTurn(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const userID = "desktop-user"
	g, err := store.Set(userID, "old command")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlan(userID, g.GoalID, "understand the work", []goal.Step{{
		Title: "do the thing",
	}}, []string{"it works"}); err != nil {
		t.Fatal(err)
	}
	app.imHandler.setPendingForegroundText(userID, "goal turn in flight")

	snap := app.UpdateSessionGoal(userID, "new command")
	if snap["objective"] != "new command" || snap["goal_id"] != g.GoalID || snap["status"] != string(goal.StatusActive) {
		t.Fatalf("update = %#v", snap)
	}
	got := store.Get(userID)
	if got == nil || got.Objective != "new command" || len(got.Steps) != 1 || got.Steps[0].Title != "do the thing" {
		t.Fatalf("plan changed: %+v", got)
	}
	if _, leaked := app.imHandler.pendingInjection.Load(userID); leaked {
		t.Fatal("the command must wait until the turn publishes a loop")
	}
	raw, ok := app.imHandler.goalBarSteer.Load(userID)
	text, _ := raw.(string)
	if !ok || !strings.Contains(text, "new command") {
		t.Fatalf("pending turn did not keep the new command: %#v", raw)
	}
	if goalBarPending(engine, userID) {
		t.Fatal("edit must not start a turn")
	}

	app.imHandler.adoptGoalCommandSteer(userID)
	raw, ok = app.imHandler.pendingInjection.Load(userID)
	text, _ = raw.(string)
	if !ok || !strings.Contains(text, "new command") {
		t.Fatalf("published turn did not receive the new command: %#v", raw)
	}
	if _, held := app.imHandler.goalBarSteer.Load(userID); held {
		t.Fatal("publish should take the held command once")
	}
}

func TestUpdateSessionGoalDropsWhenThePendingTurnNeverStarts(t *testing.T) {
	app, store, _ := newGoalBarApp(t)
	const userID = "desktop-user"
	if _, err := store.Set(userID, "old command"); err != nil {
		t.Fatal(err)
	}
	app.imHandler.setPendingForegroundText(userID, "goal turn in flight")
	if app.UpdateSessionGoal(userID, "new command")["objective"] != "new command" {
		t.Fatal("update did not keep the new command")
	}
	if _, ok := app.imHandler.goalBarSteer.Load(userID); !ok {
		t.Fatal("expected the command to wait for publish")
	}

	app.imHandler.dropGoalCommandSteer(userID)
	if _, ok := app.imHandler.goalBarSteer.Load(userID); ok {
		t.Fatal("a turn that never publishes must drop the command")
	}
	if _, ok := app.imHandler.pendingInjection.Load(userID); ok {
		t.Fatal("a dropped command must not reach the next message")
	}
	if got := store.Get(userID); got == nil || got.Objective != "new command" {
		t.Fatalf("the stored command should remain: %+v", got)
	}
}

func TestUpdateSessionGoalSkipsAPendingTurnTheBarAlreadyStopped(t *testing.T) {
	app, store, _ := newGoalBarApp(t)
	const userID = "desktop-user"
	if _, err := store.Set(userID, "old command"); err != nil {
		t.Fatal(err)
	}
	app.imHandler.setPendingForegroundText(userID, "goal turn in flight")
	app.imHandler.noteGoalBarStopPending(userID)

	if app.UpdateSessionGoal(userID, "new command")["objective"] != "new command" {
		t.Fatal("update did not keep the new command")
	}
	if _, ok := app.imHandler.goalBarSteer.Load(userID); ok {
		t.Fatal("a stopped generation must not keep a command for the next message")
	}
	if _, ok := app.imHandler.pendingInjection.Load(userID); ok {
		t.Fatal("a stopped generation must not inject the command")
	}
}

func TestPauseDropsACommandHeldForThePendingTurn(t *testing.T) {
	app, store, _ := newGoalBarApp(t)
	const userID = "desktop-user"
	if _, err := store.Set(userID, "old command"); err != nil {
		t.Fatal(err)
	}
	app.imHandler.setPendingForegroundText(userID, "goal turn in flight")
	if app.UpdateSessionGoal(userID, "new command")["objective"] != "new command" {
		t.Fatal("update did not keep the new command")
	}

	if app.PauseSessionGoal(userID)["status"] != string(goal.StatusPaused) {
		t.Fatal("pause did not pause")
	}
	if _, ok := app.imHandler.goalBarSteer.Load(userID); ok {
		t.Fatal("pause left a command waiting for publish")
	}
	if _, ok := app.imHandler.pendingInjection.Load(userID); ok {
		t.Fatal("pause left a steer for the next chat message")
	}
	if app.imHandler.hasCancelledTaskBoundary(userID) {
		t.Fatal("pause must not fence the next chat message")
	}
	app.imHandler.adoptGoalCommandSteer(userID)
	if _, ok := app.imHandler.pendingInjection.Load(userID); ok {
		t.Fatal("publish after pause must not revive the command")
	}
}

func TestUpdateSessionGoalReachesATurnContinuedPastStop(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const userID = "desktop-user"
	if _, err := store.Set(userID, "old command"); err != nil {
		t.Fatal(err)
	}
	ctx := NewLoopContext("goal-bar-edit-fence", 4, nil)
	app.imHandler.sessionLoops.Store(userID, &sessionLoopState{loopCtx: ctx})
	app.imHandler.markTaskCancelledByUser(userID)
	if app.imHandler.InjectSupplementary(userID, "ordinary chat") {
		t.Fatal("an ordinary supplement must still stop at the chat fence")
	}

	snap := app.UpdateSessionGoal(userID, "new command")
	if snap["objective"] != "new command" || snap["status"] != string(goal.StatusActive) {
		t.Fatalf("update = %#v", snap)
	}
	raw, ok := app.imHandler.pendingInjection.Load(userID)
	text, _ := raw.(string)
	if !ok || !strings.Contains(text, "new command") {
		t.Fatalf("continued turn did not receive the new command: %#v", raw)
	}
	if !app.imHandler.hasCancelledTaskBoundary(userID) {
		t.Fatal("edit must leave the chat fence in place")
	}
	if ctx.IsCancelled() {
		t.Fatal("edit must leave the continued turn running")
	}
	if goalBarPending(engine, userID) {
		t.Fatal("edit must not start a turn")
	}
}

func TestPauseAndCancelDropASteerForTheStoppedTurn(t *testing.T) {
	app, store, _ := newGoalBarApp(t)
	const userID = "desktop-user"
	if _, err := store.Set(userID, "old command"); err != nil {
		t.Fatal(err)
	}
	ctx := NewLoopContext("goal-bar-steer-drop", 4, nil)
	app.imHandler.sessionLoops.Store(userID, &sessionLoopState{loopCtx: ctx})
	if app.UpdateSessionGoal(userID, "new command")["objective"] != "new command" {
		t.Fatal("update did not keep the new command")
	}
	if _, ok := app.imHandler.pendingInjection.Load(userID); !ok {
		t.Fatal("expected a steer before pause")
	}

	if app.PauseSessionGoal(userID)["status"] != string(goal.StatusPaused) {
		t.Fatal("pause did not pause")
	}
	if _, ok := app.imHandler.pendingInjection.Load(userID); ok {
		t.Fatal("pause left a steer for the next chat message")
	}
	if app.imHandler.hasCancelledTaskBoundary(userID) {
		t.Fatal("pause must not fence the next chat message")
	}
	if !ctx.IsCancelled() {
		t.Fatal("pause should stop the live turn")
	}

	ctx = NewLoopContext("goal-bar-steer-drop-cancel", 4, nil)
	app.imHandler.sessionLoops.Store(userID, &sessionLoopState{loopCtx: ctx})
	app.imHandler.pendingInjection.Store(userID, "[用户补充] 目标命令已改为：later")
	if app.CancelSessionGoal(userID)["visible"] != false {
		t.Fatal("cancel did not hide the bar")
	}
	if _, ok := app.imHandler.pendingInjection.Load(userID); ok {
		t.Fatal("cancel left a steer for the next chat message")
	}
	if app.imHandler.hasCancelledTaskBoundary(userID) {
		t.Fatal("cancel must not fence the next chat message")
	}
}
