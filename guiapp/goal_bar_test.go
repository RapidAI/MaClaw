package guiapp

import (
	"strings"
	"testing"
	"time"

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
