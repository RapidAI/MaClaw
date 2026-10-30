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
	app := &App{testHomeDir: t.TempDir()}
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

func TestUserContinueRearmsStoppedGoalWithoutScheduling(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const userID = "desktop-user"
	g, err := store.Set(userID, "图形版本愤怒的小鸟，c++语言")
	if err != nil {
		t.Fatal(err)
	}
	if !store.UpdateStatus(userID, g.GoalID, goal.StatusFailed, "Billing daily count exceeded") {
		t.Fatal("failed to mark the goal failed")
	}
	handler := app.imHandler
	handler.releaseStoppedGoalForUserContinue(IMUserMessage{UserID: userID, Platform: "desktop", Text: "继续这个任务"}, stoppedGoalUserTurn{})
	got := store.Get(userID)
	if got == nil || got.Status != goal.StatusActive || got.Summary != "" || !got.ShouldContinue() || got.ComposerClockStopped() {
		t.Fatalf("continue this task should re-arm the failed goal, got %+v", got)
	}
	if goalBarPending(engine, userID) {
		t.Fatal("the user message is the turn; continue must not schedule another one")
	}

	if !store.UpdateStatus(userID, g.GoalID, goal.StatusPaused, "用户已暂停") {
		t.Fatal("failed to pause")
	}
	handler.releaseStoppedGoalForUserContinue(IMUserMessage{UserID: userID, Platform: "desktop", Text: "继续"}, stoppedGoalUserTurn{})
	got = store.Get(userID)
	if got == nil || got.Status != goal.StatusActive || got.ComposerClockStopped() {
		t.Fatalf("继续 should re-arm a paused goal, got %+v", got)
	}
	if goalBarPending(engine, userID) {
		t.Fatal("paused continue must not schedule beside the user turn")
	}

	// The phrase list never contained this wording. The turn is still the
	// same session's work, so the stopped goal leaves the stop.
	if !store.UpdateStatus(userID, g.GoalID, goal.StatusFailed, "编译失败") {
		t.Fatal("failed to fail again")
	}
	handler.releaseStoppedGoalForUserContinue(IMUserMessage{UserID: userID, Platform: "desktop", Text: "Үргэлжлүүлээд энэ ажлыг хий"}, stoppedGoalUserTurn{})
	if got = store.Get(userID); got == nil || got.Status != goal.StatusActive || got.Summary != "" || got.ComposerClockStopped() {
		t.Fatalf("an unlisted wording must re-arm the same session, got %+v", got)
	}
	if goalBarPending(engine, userID) {
		t.Fatal("an unlisted continue must not schedule beside the user turn")
	}

	// A new checklist request is still this session's work. Plan resume may
	// treat the words as a new requirement; the goal bar follows the work.
	if !store.UpdateStatus(userID, g.GoalID, goal.StatusFailed, "编译失败") {
		t.Fatal("failed to fail again")
	}
	handler.releaseStoppedGoalForUserContinue(IMUserMessage{UserID: userID, Platform: "desktop", Text: "继续完善界面"}, stoppedGoalUserTurn{})
	if got = store.Get(userID); got == nil || got.Status != goal.StatusActive {
		t.Fatalf("same-session work must re-arm even when the words add a requirement, got %+v", got)
	}

	if !store.UpdateStatus(userID, g.GoalID, goal.StatusBudgetLimit, "Turn limit reached") {
		t.Fatal("failed to mark the turn limit")
	}
	handler.releaseStoppedGoalForUserContinue(IMUserMessage{UserID: userID, Platform: "desktop", Text: "keep going"}, stoppedGoalUserTurn{})
	if got = store.Get(userID); got == nil || got.Status != goal.StatusActive || !got.ShouldContinue() {
		t.Fatalf("a budget stop must re-arm with the session's work, got %+v", got)
	}

	if !store.UpdateStatus(userID, g.GoalID, goal.StatusFailed, "编译失败") {
		t.Fatal("failed to fail again")
	}
	handler.releaseStoppedGoalForUserContinue(IMUserMessage{UserID: userID, Platform: "desktop", Text: "继续这个任务"}, stoppedGoalUserTurn{FreshTask: true})
	if got = store.Get(userID); got == nil || got.Status != goal.StatusFailed || got.Summary != "编译失败" {
		t.Fatalf("a new task must leave the failed goal, got %+v", got)
	}
	handler.releaseStoppedGoalForUserContinue(IMUserMessage{UserID: userID, Platform: "goal-continuation", Text: "继续这个任务"}, stoppedGoalUserTurn{})
	if got = store.Get(userID); got == nil || got.Status != goal.StatusFailed {
		t.Fatalf("a host continuation must not re-arm from this path, got %+v", got)
	}

	if !store.UpdateStatus(userID, g.GoalID, goal.StatusComplete, "做完了") {
		t.Fatal("failed to complete")
	}
	handler.releaseStoppedGoalForUserContinue(IMUserMessage{UserID: userID, Platform: "desktop", Text: "继续这个任务"}, stoppedGoalUserTurn{})
	if got = store.Get(userID); got == nil || got.Status != goal.StatusComplete {
		t.Fatalf("a completed goal stays finished, got %+v", got)
	}
}

func TestDocumentConfirmLeavesStoppedGoalAlone(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const userID = "desktop-user"
	g, err := store.Set(userID, "图形版本愤怒的小鸟，c++语言")
	if err != nil {
		t.Fatal(err)
	}
	const summary = "Billing daily count exceeded"
	if !store.UpdateStatus(userID, g.GoalID, goal.StatusFailed, summary) {
		t.Fatal("failed to mark the goal failed")
	}
	handler := app.imHandler
	handler.releaseStoppedGoalForUserContinue(IMUserMessage{UserID: userID, Platform: "desktop", Text: "继续"}, stoppedGoalUserTurn{DocPhase: true})
	got := store.Get(userID)
	if got == nil || got.Status != goal.StatusFailed || got.Summary != summary || goalBarPending(engine, userID) {
		t.Fatalf("a document confirm must leave the failed goal, pending=%v goal=%+v", goalBarPending(engine, userID), got)
	}
	engine.mu.Lock()
	_, marked := engine.continueRearmed[userID]
	engine.mu.Unlock()
	if marked {
		t.Fatal("a document confirm must not record continueRearmed")
	}
}

func TestUserTurnExecutesSessionGoalByRoute(t *testing.T) {
	app, _, _ := newGoalBarApp(t)
	handler := app.imHandler
	const userID = "desktop-user"
	if handler.userTurnExecutesSessionGoal(userID, false, false) {
		t.Fatal("a shared chat reply is not the goal's work")
	}
	if handler.userTurnExecutesSessionGoal(userID, true, true) {
		t.Fatal("a document confirmation is not the goal's work")
	}
	if !handler.userTurnExecutesSessionGoal(userID, true, false) {
		t.Fatal("a workflow implementation turn is the goal's work")
	}
	handler.storeStickyCodingWorkbenchMemory(userID, stickyCodingWorkbenchMemory{Kind: "local"})
	t.Cleanup(func() { handler.clearStickyCodingWorkbenchMemory(userID) })
	if !handler.userTurnExecutesSessionGoal(userID, false, false) {
		t.Fatal("a coding workbench turn is the goal's work")
	}
	if handler.userTurnExecutesSessionGoal(userID, false, true) {
		t.Fatal("a document confirmation stays out of a coding tab")
	}
}

func TestApplyDerivedWorkflowPhase(t *testing.T) {
	doc, id := applyDerivedWorkflowPhase(false, "", "design", true)
	if !doc || id != "design" {
		t.Fatalf("confirmation gate must take the active document phase, doc=%v id=%q", doc, id)
	}
	doc, id = applyDerivedWorkflowPhase(false, "", "implement", false)
	if doc || id != "implement" {
		t.Fatalf("an execution phase stays execution, doc=%v id=%q", doc, id)
	}
	doc, id = applyDerivedWorkflowPhase(false, "implement", "design", true)
	if doc || id != "implement" {
		t.Fatalf("a named execution phase keeps its route, doc=%v id=%q", doc, id)
	}
	doc, id = applyDerivedWorkflowPhase(true, "", "", false)
	if !doc || id != "" {
		t.Fatalf("a routed document phase with no active id stays a document, doc=%v id=%q", doc, id)
	}
}

func TestUserContinueThenStopPausesAgain(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const userID = "desktop-user"
	g, err := store.Set(userID, "图形版本愤怒的小鸟，c++语言")
	if err != nil {
		t.Fatal(err)
	}
	if !store.UpdateStatus(userID, g.GoalID, goal.StatusFailed, "Billing daily count exceeded") {
		t.Fatal("failed to mark the goal failed")
	}
	handler := app.imHandler
	handler.releaseStoppedGoalForUserContinue(IMUserMessage{UserID: userID, Platform: "desktop", Text: "继续这个任务吧"}, stoppedGoalUserTurn{})
	if got := store.Get(userID); got == nil || got.Status != goal.StatusActive || got.ComposerClockStopped() {
		t.Fatalf("particle continue should re-arm, got %+v", got)
	}
	// The continue mark belongs to the turn that was already dying. It must
	// not swallow the next explicit stop.
	handler.pauseStillActiveUserStop(userID)
	if got := store.Get(userID); got == nil || got.Status != goal.StatusActive {
		t.Fatalf("the continue mark should leave this turn active, got %+v", got)
	}
	handler.markTaskCancelledByUser(userID)
	handler.pauseStillActiveUserStop(userID)
	got := store.Get(userID)
	if got == nil || got.Status != goal.StatusPaused || goalBarPending(engine, userID) {
		t.Fatalf("a later stop should pause, pending=%v goal=%+v", goalBarPending(engine, userID), got)
	}
	if !strings.Contains(got.Summary, "停止") {
		t.Fatalf("summary = %q", got.Summary)
	}
}

func TestUserContinueLeavesUnarmedRemoteStatusUntouched(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	userID := stickyTestUserID(t)
	t.Cleanup(func() { engine.CancelPending(userID) })
	g, err := store.Set(userID, "remote fix")
	if err != nil {
		t.Fatal(err)
	}
	const summary = "Billing daily count exceeded"
	if !store.UpdateStatus(userID, g.GoalID, goal.StatusFailed, summary) {
		t.Fatal("failed to mark the goal failed")
	}
	handler := app.imHandler
	handler.storeStickyCodingWorkbenchMemory(userID, stickyCodingWorkbenchMemory{
		Kind:          "remote",
		RemoteHost:    "dev.example",
		RemoteWorkDir: "/src/app",
	})
	t.Cleanup(func() { handler.clearStickyCodingWorkbenchMemory(userID) })

	handler.releaseStoppedGoalForUserContinue(IMUserMessage{UserID: userID, Platform: "desktop", Text: "继续这个任务"}, stoppedGoalUserTurn{})
	got := store.Get(userID)
	if got == nil || got.Status != goal.StatusFailed || got.Summary != summary || goalBarPending(engine, userID) {
		t.Fatalf("dead SSH must keep the failed row, pending=%v goal=%+v", goalBarPending(engine, userID), got)
	}
	engine.mu.Lock()
	_, marked := engine.continueRearmed[userID]
	engine.mu.Unlock()
	if marked {
		t.Fatal("an unarmed continue must not record continueRearmed")
	}
}

func TestUserContinueRearmsWhileRemoteEngineIsStillPending(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	userID := stickyTestUserID(t)
	t.Cleanup(func() { engine.CancelPending(userID) })
	g, err := store.Set(userID, "remote fix")
	if err != nil {
		t.Fatal(err)
	}
	if !store.UpdateStatus(userID, g.GoalID, goal.StatusFailed, "Billing daily count exceeded") {
		t.Fatal("failed to mark the goal failed")
	}
	handler := app.imHandler
	handler.storeStickyCodingWorkbenchMemory(userID, stickyCodingWorkbenchMemory{
		Kind:            "remote",
		RemoteHost:      "dev.example",
		RemoteSessionID: "ssh-1",
		RemoteWorkDir:   "/src/app",
	})
	handler.pendingV2SubAgentExecution.Store(userID, true)
	handler.pendingTemplateRemoteCoding.Store(userID, remoteCodingTemplateContext{
		SessionID: "ssh-1",
		WorkDir:   "/src/app",
	})
	t.Cleanup(func() {
		handler.pendingV2SubAgentExecution.Delete(userID)
		handler.pendingTemplateRemoteCoding.Delete(userID)
		handler.clearStickyCodingWorkbenchMemory(userID)
	})

	// Same order as pure remote coding: re-arm while the SSH engine flag
	// is still stored, then consume it. Deleting first looks unarmed.
	handler.releaseStoppedGoalForUserContinue(IMUserMessage{UserID: userID, Platform: "desktop", Text: "继续这个任务"}, stoppedGoalUserTurn{})
	handler.pendingTemplateRemoteCoding.Delete(userID)
	handler.pendingV2SubAgentExecution.Delete(userID)
	got := store.Get(userID)
	if got == nil || got.Status != goal.StatusActive || got.Summary != "" || got.ComposerClockStopped() || goalBarPending(engine, userID) {
		t.Fatalf("armed remote continue should re-arm without a second timer, pending=%v goal=%+v", goalBarPending(engine, userID), got)
	}
}

func TestSessionGoalBarContinuePastBudget(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const userID = "desktop-user"
	g, err := store.Set(userID, "keep going", goal.WithMaxTurns(3))
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

func TestContinueSessionGoalWaitsForTheLiveDesktopTurn(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const front = `desktop-user:F:/bird2`
	const back = `desktop-user:F:\bird2`
	t.Cleanup(func() {
		engine.CancelPending(front)
		engine.CancelPending(back)
	})
	g, err := store.Set(back, "正在做的小鸟")
	if err != nil {
		t.Fatal(err)
	}
	if !store.Pause(back, g.GoalID) {
		t.Fatal("pause")
	}
	ctx := NewLoopContext("coding", 4, nil)
	cleanup := app.imHandler.beginPureCodingRuntime(ctx, front, "keep working")
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()

	snap := app.ContinueSessionGoal(front)
	if snap["status"] != string(goal.StatusActive) || snap["user_id"] != back {
		t.Fatalf("continue = %#v", snap)
	}
	if goalBarPending(engine, front) || goalBarPending(engine, back) {
		t.Fatal("continue armed a continuation beside the live desktop turn")
	}
	cleanup()
	cleanup = nil
	app.imHandler.scheduleGoalContinuationUnlessLive(back, g.GoalID)
	if !goalBarPending(engine, back) || goalBarPending(engine, front) {
		t.Fatal("continuation should arm on the stored row once the desktop turn is gone")
	}
}

func TestContinueSessionGoalArmsWhileContinuationTextHoldsTheWindow(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const userID = "desktop-user"
	g, err := store.Set(userID, "先把当前回合做完")
	if err != nil {
		t.Fatal(err)
	}
	if !store.Pause(userID, g.GoalID) {
		t.Fatal("pause")
	}
	app.imHandler.setPendingForegroundText(userID, "[系统续接][execute] 当前步骤")

	snap := app.ContinueSessionGoal(userID)
	if snap["status"] != string(goal.StatusActive) || !goalBarPending(engine, userID) {
		t.Fatalf("continue during continuation unwind = %#v pending=%v", snap, goalBarPending(engine, userID))
	}
}

func TestContinuePastUserStopCoversTheSlashTwin(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const front = `desktop-user:F:/bird2`
	const back = `desktop-user:F:\bird2`
	t.Cleanup(func() {
		engine.CancelPending(front)
		engine.CancelPending(back)
	})
	g, err := store.Set(back, "正在做的小鸟")
	if err != nil {
		t.Fatal(err)
	}
	if !store.Pause(back, g.GoalID) {
		t.Fatal("pause")
	}
	app.imHandler.markTaskCancelledByUser(front)

	snap := app.ContinueSessionGoal(front)
	if snap["status"] != string(goal.StatusActive) || snap["user_id"] != back || !goalBarPending(engine, back) || goalBarPending(engine, front) {
		t.Fatalf("continue = %#v pending back=%v front=%v", snap, goalBarPending(engine, back), goalBarPending(engine, front))
	}
	ordinary := IMUserMessage{UserID: front, Platform: "desktop", Text: "hello"}
	if got := app.imHandler.rejectCancelledTaskTurn(ordinary); got == nil || !strings.Contains(got.Text, "Task cancelled") {
		t.Fatalf("ordinary message = %+v", got)
	}

	app.imHandler.maybeScheduleGoalContinuation(front, &IMAgentResponse{Text: "Task cancelled.", userStopped: true}, "desktop")
	kept := store.Get(back)
	if kept == nil || kept.GoalID != g.GoalID || kept.Status != goal.StatusActive || !goalBarPending(engine, back) {
		t.Fatalf("dying turn undid continue: pending=%v goal=%+v", goalBarPending(engine, back), kept)
	}

	app.imHandler.markTaskCancelledByUser(front)
	app.imHandler.maybeScheduleGoalContinuation(front, &IMAgentResponse{Text: "stopped", userStopped: true}, "desktop")
	stopped := store.Get(back)
	if stopped == nil || stopped.Status != goal.StatusPaused || goalBarPending(engine, back) || goalBarPending(engine, front) {
		t.Fatalf("a later stop was swallowed: pending back=%v front=%v goal=%+v", goalBarPending(engine, back), goalBarPending(engine, front), stopped)
	}
}

func TestCancelSlashTwinStopsTheContinuationLoop(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const front = `desktop-user:F:/bird2`
	const back = `desktop-user:F:\bird2`
	t.Cleanup(func() {
		engine.CancelPending(front)
		engine.CancelPending(back)
	})
	g, err := store.Set(back, "正在做的小鸟")
	if err != nil {
		t.Fatal(err)
	}
	engine.scheduleDelayed(back, g.GoalID)
	ctx := NewLoopContext("coding", 4, nil)
	ctx.Platform = "goal-continuation"
	cleanup := app.imHandler.beginPureCodingRuntime(ctx, back, "[系统续接][execute] 当前步骤")
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()

	text, err := app.imHandler.CancelSessionForUser(front)
	if err != nil {
		t.Fatal(err)
	}
	if text != "[系统续接][execute] 当前步骤" {
		t.Fatalf("cancelled text = %q", text)
	}
	if !ctx.IsCancelled() || !ctx.UserCancelled() {
		t.Fatal("continuation loop kept running")
	}
	if !app.imHandler.hasCancelledTaskBoundary(back) {
		t.Fatal("stored spelling was not fenced")
	}
	if app.imHandler.hasCancelledTaskBoundary(front) {
		t.Fatal("composer spelling must not become a new-task fence")
	}
	got := store.Get(back)
	if got == nil || got.Status != goal.StatusPaused || !strings.Contains(got.Summary, "停止") {
		t.Fatalf("goal = %+v", got)
	}
	if goalBarPending(engine, front) || goalBarPending(engine, back) {
		t.Fatal("continuation timer survived the stop")
	}
	cleanup()
	cleanup = nil
}

func TestRequestCancelSlashTwinPausesAQueuedContinuation(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const front = `desktop-user:F:/bird2`
	const back = `desktop-user:F:\bird2`
	t.Cleanup(func() {
		engine.CancelPending(front)
		engine.CancelPending(back)
	})
	g, err := store.Set(back, "正在做的小鸟")
	if err != nil {
		t.Fatal(err)
	}
	engine.scheduleDelayed(back, g.GoalID)
	app.imHandler.noteGoalContinuePastUserStop(back)

	text, err := app.imHandler.RequestCancelSessionForUser(front)
	if err != nil {
		t.Fatal(err)
	}
	if text != "" {
		t.Fatalf("timer-only stop text = %q", text)
	}
	if app.imHandler.hasCancelledTaskBoundary(front) || app.imHandler.hasCancelledTaskBoundary(back) {
		t.Fatal("a queued continuation is not a chat turn")
	}
	got := store.Get(back)
	if got == nil || got.Status != goal.StatusPaused || !strings.Contains(got.Summary, "停止") {
		t.Fatalf("continue mark swallowed the stop: %+v", got)
	}
	if goalBarPending(engine, front) || goalBarPending(engine, back) {
		t.Fatal("continuation timer survived the stop")
	}
}

func TestRequestCancelKeepsTheGoalForTheLiveTurnToPause(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const front = `desktop-user:F:/bird2`
	const back = `desktop-user:F:\bird2`
	t.Cleanup(func() {
		engine.CancelPending(front)
		engine.CancelPending(back)
	})
	g, err := store.Set(back, "正在做的小鸟")
	if err != nil {
		t.Fatal(err)
	}
	engine.scheduleDelayed(back, g.GoalID)
	ctx := NewLoopContext("coding", 4, nil)
	cleanup := app.imHandler.beginPureCodingRuntime(ctx, front, "keep working")
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()

	if _, err := app.imHandler.RequestCancelSessionForUser(front); err != nil {
		t.Fatal(err)
	}
	if !ctx.IsCancelled() || !ctx.UserCancelled() {
		t.Fatal("desktop turn was not cancelled")
	}
	if goalBarPending(engine, front) || goalBarPending(engine, back) {
		t.Fatal("twin timer kept running after stop")
	}
	live := store.Get(back)
	if live == nil || live.Status != goal.StatusActive {
		t.Fatalf("stop must leave the pause to the dying turn: %+v", live)
	}
	app.imHandler.maybeScheduleGoalContinuation(front, &IMAgentResponse{Text: "Task cancelled.", userStopped: true}, "desktop")
	stopped := store.Get(back)
	if stopped == nil || stopped.Status != goal.StatusPaused || !strings.Contains(stopped.Summary, "停止") {
		t.Fatalf("dying turn did not pause: %+v", stopped)
	}
	cleanup()
	cleanup = nil
}

func TestIdleCancelWithGoalEngineDoesNotFence(t *testing.T) {
	app, _, _ := newGoalBarApp(t)
	const front = `desktop-user:F:/bird2`
	const back = `desktop-user:F:\bird2`
	_, err := app.imHandler.CancelSessionForUser(front)
	if err == nil {
		t.Fatal("idle cancel must fail when no turn and no continuation are running")
	}
	if app.imHandler.hasCancelledTaskBoundary(front) || app.imHandler.hasCancelledTaskBoundary(back) {
		t.Fatal("idle cancel must not fence either spelling")
	}
}

func TestCancelSlashTwinDropsAnAdmittedContinuation(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const front = `desktop-user:F:/bird2`
	const back = `desktop-user:F:\bird2`
	t.Cleanup(func() {
		engine.CancelPending(front)
		engine.CancelPending(back)
	})
	g, err := store.Set(back, "正在做的小鸟")
	if err != nil {
		t.Fatal(err)
	}
	const requestID = "goal-cont-slash-stop"
	if !engine.admitContinuationIfContinuable(back, g.GoalID, requestID) {
		t.Fatal("an active goal should admit the continuation")
	}
	app.imHandler.noteGoalContinuePastUserStop(back)

	text, err := app.imHandler.CancelSessionForUser(front)
	if err != nil || text != "" {
		t.Fatalf("stop = %q err=%v", text, err)
	}
	if !engine.continuationAdmitRevoked(requestID) {
		t.Fatal("stop left the admitted continuation runnable")
	}
	if app.imHandler.hasCancelledTaskBoundary(front) || app.imHandler.hasCancelledTaskBoundary(back) {
		t.Fatal("an admitted continuation is not a chat fence")
	}
	got := store.Get(back)
	if got == nil || got.Status != goal.StatusPaused || !strings.Contains(got.Summary, "停止") {
		t.Fatalf("continue mark swallowed the stop: %+v", got)
	}
	if app.ContinueSessionGoal(front)["status"] != string(goal.StatusActive) {
		t.Fatal("continue did not re-arm")
	}
	if !engine.continuationAdmitRevoked(requestID) {
		t.Fatal("continue revived the continuation stop already dropped")
	}
	const nextID = "goal-cont-slash-next"
	if !engine.admitContinuationIfContinuable(back, g.GoalID, nextID) || engine.continuationAdmitRevoked(nextID) {
		t.Fatal("the next fire should admit a new continuation")
	}
	engine.runForegroundContinuation(app.imHandler, IMUserMessage{
		RequestID: requestID,
		UserID:    back,
		Platform:  "goal-continuation",
		Text:      "[系统续接][execute] 当前步骤",
	})
	kept := store.Get(back)
	if kept == nil || kept.Status != goal.StatusActive || kept.GoalID != g.GoalID {
		t.Fatalf("dropped turn changed the goal: %+v", kept)
	}
	if engine.continuationAdmitRevoked(requestID) {
		t.Fatal("the dropped turn should retire its ticket")
	}
}

func TestRequestCancelRevokesTwinAdmitBeforeTheTurnPauses(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const front = `desktop-user:F:/bird2`
	const back = `desktop-user:F:\bird2`
	t.Cleanup(func() {
		engine.CancelPending(front)
		engine.CancelPending(back)
	})
	g, err := store.Set(back, "正在做的小鸟")
	if err != nil {
		t.Fatal(err)
	}
	const requestID = "goal-cont-live-desktop"
	if !engine.admitContinuationIfContinuable(back, g.GoalID, requestID) {
		t.Fatal("an active goal should admit the continuation")
	}
	ctx := NewLoopContext("coding", 4, nil)
	cleanup := app.imHandler.beginPureCodingRuntime(ctx, front, "keep working")
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()

	if _, err := app.imHandler.RequestCancelSessionForUser(front); err != nil {
		t.Fatal(err)
	}
	if !engine.continuationAdmitRevoked(requestID) {
		t.Fatal("the dying desktop turn left the admitted continuation runnable")
	}
	live := store.Get(back)
	if live == nil || live.Status != goal.StatusActive {
		t.Fatalf("stop must leave the pause to the dying turn: %+v", live)
	}
	app.imHandler.maybeScheduleGoalContinuation(front, &IMAgentResponse{Text: "Task cancelled.", userStopped: true}, "desktop")
	stopped := store.Get(back)
	if stopped == nil || stopped.Status != goal.StatusPaused || !strings.Contains(stopped.Summary, "停止") {
		t.Fatalf("dying turn did not pause: %+v", stopped)
	}
	cleanup()
	cleanup = nil
}

func TestCancelRevokesTwinAdmitBeforeTheDesktopTurnExits(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const front = `desktop-user:F:/bird2`
	const back = `desktop-user:F:\bird2`
	t.Cleanup(func() {
		engine.CancelPending(front)
		engine.CancelPending(back)
	})
	g, err := store.Set(back, "正在做的小鸟")
	if err != nil {
		t.Fatal(err)
	}
	const requestID = "goal-cont-during-wait"
	if !engine.admitContinuationIfContinuable(back, g.GoalID, requestID) {
		t.Fatal("an active goal should admit the continuation")
	}
	ctx := NewLoopContext("coding", 4, nil)
	cleanup := app.imHandler.beginPureCodingRuntime(ctx, front, "keep working")
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()
	// DoneC stays open until this observes the cancel. That is the window
	// CancelSessionForUser waits in, up to 30s. The admit has to be dead
	// before the wait, or the continuation enters while stop is blocked.
	saw := make(chan struct{})
	go func() {
		<-ctx.CancelC
		if !engine.continuationAdmitRevoked(requestID) {
			t.Error("admitted continuation can still enter while stop waits for the desktop turn")
		}
		if got := store.Get(back); got == nil || got.Status != goal.StatusActive {
			t.Errorf("stop paused before the dying turn: %+v", got)
		}
		ctx.Done()
		close(saw)
	}()

	if _, err := app.imHandler.CancelSessionForUser(front); err != nil {
		t.Fatal(err)
	}
	select {
	case <-saw:
	case <-time.After(2 * time.Second):
		t.Fatal("cancel returned without the desktop turn observing it")
	}
	app.imHandler.maybeScheduleGoalContinuation(front, &IMAgentResponse{Text: "Task cancelled.", userStopped: true}, "desktop")
	stopped := store.Get(back)
	if stopped == nil || stopped.Status != goal.StatusPaused || !strings.Contains(stopped.Summary, "停止") {
		t.Fatalf("dying turn did not pause: %+v", stopped)
	}
	cleanup()
	cleanup = nil
}

func TestUserStopFenceBlocksTheStoredContinuation(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const front = `desktop-user:F:/bird2`
	const back = `desktop-user:F:\bird2`
	g, err := store.Set(back, "正在做的小鸟")
	if err != nil {
		t.Fatal(err)
	}
	app.imHandler.markTaskCancelledByUser(front)
	if engine.admitContinuationIfContinuable(back, g.GoalID, "goal-cont-fenced") {
		t.Fatal("composer stop must keep the stored continuation from starting")
	}
	app.imHandler.noteGoalContinuePastUserStop(front)
	const requestID = "goal-cont-continued"
	if !engine.admitContinuationIfContinuable(back, g.GoalID, requestID) || engine.continuationAdmitRevoked(requestID) {
		t.Fatal("continue past this stop should admit the next continuation")
	}
	app.imHandler.markTaskCancelledByUser(front)
	if engine.admitContinuationIfContinuable(back, g.GoalID, "goal-cont-stopped-again") {
		t.Fatal("a later stop must block a new continuation")
	}
	engine.runForegroundContinuation(app.imHandler, IMUserMessage{
		RequestID: requestID,
		UserID:    back,
		Platform:  "goal-continuation",
		Text:      "[系统续接][execute] 当前步骤",
	})
	kept := store.Get(back)
	if kept == nil || kept.Status != goal.StatusActive || kept.GoalID != g.GoalID {
		t.Fatalf("stopped continuation changed the goal: %+v", kept)
	}
}

func TestStopFenceDoesNotFailAnUnplannedGoal(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const front = `desktop-user:F:/bird2`
	const back = `desktop-user:F:\bird2`
	g, err := store.Set(back, "正在做的小鸟")
	if err != nil {
		t.Fatal(err)
	}
	if engine.consumePlanMiss(back, g.GoalID) {
		t.Fatal("the first miss records the corrective turn")
	}
	app.imHandler.markTaskCancelledByUser(front)
	engine.dispatchContinuation(back, g.GoalID, goalCorrectionPlan)
	got := store.Get(back)
	if got == nil || got.Status != goal.StatusActive || strings.Contains(got.Summary, "未能分解") {
		t.Fatalf("stop fence failed the unplanned goal: %+v", got)
	}
	if engine.consumePlanMiss(back, g.GoalID) {
		t.Fatal("the blocked correction consumed the plan retry")
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
	pausedSnap := app.PauseSessionGoal(userID)
	pausedAt, _ := pausedSnap["stopped_at"].(int64)
	if pausedSnap["status"] != string(goal.StatusPaused) || pausedAt == 0 {
		t.Fatalf("pause should freeze the clock: %#v", pausedSnap)
	}
	store.UpdateStatus(userID, g.GoalID, goal.StatusFailed, "编译失败")
	failed := app.GetSessionGoalBar(userID)
	stopped, _ := failed["stopped_at"].(int64)
	created, _ := failed["created_at"].(int64)
	if failed["status"] != string(goal.StatusFailed) || stopped != pausedAt || stopped < created {
		t.Fatalf("failed clock created=%d paused=%d stopped=%#v snap=%#v", created, pausedAt, failed["stopped_at"], failed)
	}
	legacy := &goal.Goal{Status: goal.StatusPaused, UpdatedAt: time.Unix(pausedAt, 0)}
	if got := goalStoppedUnix(legacy); got != pausedAt {
		t.Fatalf("legacy pause freeze = %d, want %d", got, pausedAt)
	}
	if got := goalStoppedUnix(&goal.Goal{Status: goal.StatusActive, UpdatedAt: time.Unix(pausedAt, 0)}); got != 0 {
		t.Fatalf("active clock = %d", got)
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

func TestFinishCancelledGoalTurnPausesOnUserStopFence(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const userID = "desktop-user"
	g, err := store.Set(userID, "设计并实现一个愤怒的小鸟游戏")
	if err != nil {
		t.Fatal(err)
	}
	engine.scheduleDelayed(userID, g.GoalID)
	if !goalBarPending(engine, userID) {
		t.Fatal("the running goal should have a timer")
	}
	ctx := NewLoopContext("coding-abort", 4, nil)
	ctx.MarkUserCancel()
	ctx.Cancel()
	app.imHandler.markTaskCancelledByUser(userID)

	app.imHandler.finishCancelledGoalTurn(userID, &IMAgentResponse{Text: "Stopped before finishing"}, ctx)

	got := store.Get(userID)
	if got == nil || got.Status != goal.StatusPaused || got.IsTerminal() || !got.ComposerClockStopped() || got.StoppedAt.IsZero() {
		t.Fatalf("user stop should freeze a continuable goal: %+v", got)
	}
	if goalStoppedUnix(got) == 0 {
		t.Fatal("paused snapshot sent stopped_at 0")
	}
	if goalBarPending(engine, userID) {
		t.Fatal("user stop left the next turn armed")
	}
	if !strings.Contains(got.Summary, "停止") {
		t.Fatalf("summary = %q", got.Summary)
	}

	if app.ContinueSessionGoal(userID)["status"] != string(goal.StatusActive) || !goalBarPending(engine, userID) {
		t.Fatal("continue should re-arm the same fence")
	}
	app.imHandler.finishCancelledGoalTurn(userID, &IMAgentResponse{Text: "Stopped before finishing"}, ctx)
	kept := store.Get(userID)
	if kept == nil || kept.Status != goal.StatusActive || kept.ComposerClockStopped() || !goalBarPending(engine, userID) {
		t.Fatalf("dying abort undid continue: pending=%v goal=%+v", goalBarPending(engine, userID), kept)
	}
}

func TestFinishCancelledGoalTurnIgnoresParentCancel(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const userID = "desktop-user"
	g, err := store.Set(userID, "keep going")
	if err != nil {
		t.Fatal(err)
	}
	engine.scheduleDelayed(userID, g.GoalID)
	ctx := NewLoopContext("parent-cancel", 4, nil)
	ctx.Cancel()

	app.imHandler.finishCancelledGoalTurn(userID, &IMAgentResponse{Text: "context canceled"}, ctx)

	got := store.Get(userID)
	if got == nil || got.Status != goal.StatusActive || got.ComposerClockStopped() || !got.StoppedAt.IsZero() || !goalBarPending(engine, userID) {
		t.Fatalf("parent cancel changed the goal: pending=%v goal=%+v", goalBarPending(engine, userID), got)
	}
}

func TestFinishCancelledGoalTurnLeavesContinuedGoalArmed(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const userID = "desktop-user"
	if _, err := store.Set(userID, "keep going"); err != nil {
		t.Fatal(err)
	}
	ctx := NewLoopContext("goal-bar-pause", 4, nil)
	app.imHandler.sessionLoops.Store(userID, &sessionLoopState{loopCtx: ctx})
	if app.PauseSessionGoal(userID)["status"] != string(goal.StatusPaused) || !ctx.UserCancelled() {
		t.Fatal("pause should stop the live turn without a chat fence")
	}
	if app.imHandler.hasCancelledTaskBoundary(userID) {
		t.Fatal("goal-bar pause must not fence the next message")
	}
	if app.ContinueSessionGoal(userID)["status"] != string(goal.StatusActive) || !goalBarPending(engine, userID) {
		t.Fatal("continue should arm the same goal")
	}

	app.imHandler.finishCancelledGoalTurn(userID, &IMAgentResponse{Text: "Stopped before finishing"}, ctx)

	kept := store.Get(userID)
	if kept == nil || kept.Status != goal.StatusActive || kept.ComposerClockStopped() || !goalBarPending(engine, userID) {
		t.Fatalf("dying abort undid the goal-bar continue: pending=%v goal=%+v", goalBarPending(engine, userID), kept)
	}

	app.imHandler.markTaskCancelledByUser(userID)
	app.imHandler.maybeScheduleGoalContinuation(userID, &IMAgentResponse{Text: "stopped", userStopped: true}, "desktop")
	stopped := store.Get(userID)
	if stopped == nil || stopped.Status != goal.StatusPaused || stopped.IsTerminal() || stopped.StoppedAt.IsZero() || goalBarPending(engine, userID) {
		t.Fatalf("a later stop was swallowed: pending=%v goal=%+v", goalBarPending(engine, userID), stopped)
	}
}

func TestFinishCancelledGoalTurnKeepsAnArmedContinue(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const userID = "desktop-user"
	if _, err := store.Set(userID, "keep going"); err != nil {
		t.Fatal(err)
	}
	if app.PauseSessionGoal(userID)["status"] != string(goal.StatusPaused) {
		t.Fatal("pause")
	}
	if app.ContinueSessionGoal(userID)["status"] != string(goal.StatusActive) || !goalBarPending(engine, userID) {
		t.Fatal("continue should arm the timer before the turn is published")
	}
	ctx := NewLoopContext("unpublished-stop", 4, nil)
	ctx.MarkUserCancel()

	app.imHandler.finishCancelledGoalTurn(userID, &IMAgentResponse{Text: "Stopped before finishing"}, ctx)

	got := store.Get(userID)
	if got == nil || got.Status != goal.StatusActive || got.ComposerClockStopped() || !goalBarPending(engine, userID) {
		t.Fatalf("unpublished abort cancelled the continue: pending=%v goal=%+v", goalBarPending(engine, userID), got)
	}
}

func TestFinishCancelledGoalTurnPausesAQueuedTurn(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const userID = "desktop-user"
	g, err := store.Set(userID, "keep going")
	if err != nil {
		t.Fatal(err)
	}
	engine.scheduleDelayed(userID, g.GoalID)
	ctx := NewLoopContext("queued-stop", 4, nil)
	ctx.MarkUserCancel()

	app.imHandler.finishCancelledGoalTurn(userID, &IMAgentResponse{Text: "Stopped before finishing"}, ctx)

	got := store.Get(userID)
	if got == nil || got.Status != goal.StatusPaused || got.IsTerminal() || !got.ComposerClockStopped() || got.StoppedAt.IsZero() || goalBarPending(engine, userID) {
		t.Fatalf("a queued turn is not a continue: pending=%v goal=%+v", goalBarPending(engine, userID), got)
	}
}

func TestFinishCancelledGoalTurnHonorsContinueBeforeItsTimer(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const userID = "desktop-user"
	g, err := store.Set(userID, "keep going")
	if err != nil {
		t.Fatal(err)
	}
	if app.PauseSessionGoal(userID)["status"] != string(goal.StatusPaused) {
		t.Fatal("pause")
	}
	app.imHandler.noteGoalContinuePastUserStop(userID)
	if !store.ReleaseForContinue(userID, g.GoalID) {
		t.Fatal("release")
	}
	if goalBarPending(engine, userID) {
		t.Fatal("the timer is armed only after the goal is active")
	}
	ctx := NewLoopContext("continue-gap", 4, nil)
	ctx.MarkUserCancel()

	app.imHandler.finishCancelledGoalTurn(userID, &IMAgentResponse{Text: "Stopped before finishing"}, ctx)

	kept := store.Get(userID)
	if kept == nil || kept.Status != goal.StatusActive || kept.ComposerClockStopped() || !kept.StoppedAt.IsZero() {
		t.Fatalf("abort during the continue gap re-paused the goal: %+v", kept)
	}

	engine.scheduleDelayed(userID, g.GoalID)
	engine.CancelPending(userID)
	app.imHandler.finishCancelledGoalTurn(userID, &IMAgentResponse{Text: "Stopped before finishing"}, ctx)
	afterFire := store.Get(userID)
	if afterFire == nil || afterFire.Status != goal.StatusActive || afterFire.ComposerClockStopped() {
		t.Fatalf("abort after the continue timer fired re-paused the goal: %+v", afterFire)
	}

	app.imHandler.markTaskCancelledByUser(userID)
	app.imHandler.finishCancelledGoalTurn(userID, &IMAgentResponse{Text: "Stopped before finishing"}, ctx)
	stopped := store.Get(userID)
	if stopped == nil || stopped.Status != goal.StatusPaused || stopped.IsTerminal() || stopped.StoppedAt.IsZero() || goalBarPending(engine, userID) {
		t.Fatalf("a later user stop was swallowed: pending=%v goal=%+v", goalBarPending(engine, userID), stopped)
	}
}

func TestPauseSessionGoalStopsAnUnpublishedTurnAndReleasesTheQuestion(t *testing.T) {
	app, store, _ := newGoalBarApp(t)
	const userID = "desktop-user"
	if _, err := store.Set(userID, "keep going"); err != nil {
		t.Fatal(err)
	}
	app.imHandler.setPendingForegroundText(userID, "[系统续接][execute] 当前步骤")
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

func TestPauseSessionGoalLeavesADesktopCodingTurnRunning(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const userID = "desktop-user"
	g, err := store.Set(userID, "提升20%")
	if err != nil {
		t.Fatal(err)
	}
	engine.scheduleDelayed(userID, g.GoalID)
	ctx := NewLoopContext("coding", 4, nil)
	cleanup := app.imHandler.beginPureCodingRuntime(ctx, userID, "keep working")
	defer cleanup()
	app.imHandler.pendingInjection.Store(userID, "steer stays")
	app.imHandler.pendingAskUser.Store(userID, "which file?")

	if app.PauseSessionGoal(userID)["status"] != string(goal.StatusPaused) {
		t.Fatal("pause did not pause")
	}
	if ctx.IsCancelled() {
		t.Fatal("pausing the goal cancelled the desktop coding turn")
	}
	if goalBarPending(engine, userID) {
		t.Fatal("pause left the continuation timer")
	}
	if raw, _ := app.imHandler.pendingInjection.Load(userID); raw != "steer stays" {
		t.Fatalf("pause dropped the coding steer: %v", raw)
	}
	if _, ok := app.imHandler.pendingAskUser.Load(userID); !ok {
		t.Fatal("pause dropped the coding turn's question")
	}
}

func TestPauseSessionGoalLeavesAnInFlightDesktopTurnRunning(t *testing.T) {
	app, store, _ := newGoalBarApp(t)
	const userID = "desktop-user"
	if _, err := store.Set(userID, "提升20%"); err != nil {
		t.Fatal(err)
	}
	ctx := NewLoopContext("pre-publish", 4, nil)
	ctx.Platform = "desktop"
	app.imHandler.beginInFlightTurn(userID, "keep working", ctx)
	defer app.imHandler.endInFlightTurn(userID, ctx)
	if app.PauseSessionGoal(userID)["status"] != string(goal.StatusPaused) {
		t.Fatal("pause did not pause")
	}
	if ctx.IsCancelled() || app.imHandler.takeGoalBarStop(userID, ctx) {
		t.Fatal("pause marked the unpublished desktop turn stopped")
	}
}

func TestPauseSessionGoalLeavesAPendingUserMessageRunning(t *testing.T) {
	app, store, _ := newGoalBarApp(t)
	const userID = "desktop-user"
	if _, err := store.Set(userID, "提升20%"); err != nil {
		t.Fatal(err)
	}
	h := app.imHandler
	h.setPendingForegroundText(userID, "earlier turn")
	h.setPendingForegroundText(userID, "keep working")
	h.pendingAskUser.Store(userID, "which file?")
	h.goalBarSteer.Store(userID, "steer stays")
	h.pendingInjection.Store(userID, "injection stays")
	current := h.pendingForegroundGen(userID)

	if app.PauseSessionGoal(userID)["status"] != string(goal.StatusPaused) {
		t.Fatal("pause did not pause")
	}
	if h.takeGoalBarStop(userID, NewLoopContext("later", 4, nil)) {
		t.Fatal("pause marked the user's pending message stopped")
	}
	if _, ok := h.pendingAskUser.Load(userID); !ok {
		t.Fatal("pause dropped the user's question")
	}
	if raw, _ := h.goalBarSteer.Load(userID); raw != "steer stays" {
		t.Fatalf("pause dropped the user's steer: %v", raw)
	}
	if raw, _ := h.pendingInjection.Load(userID); raw != "injection stays" {
		t.Fatalf("pause dropped the user's injection: %v", raw)
	}
	if h.hasCancelledTaskBoundary(userID) {
		t.Fatal("pause must not fence the next chat message")
	}
	if current <= 1 || !h.goalBarQuestionBlocked(userID, current-1) {
		t.Fatal("pause let the previous generation publish a question")
	}
	if h.goalBarQuestionBlocked(userID, current) {
		t.Fatal("pause silenced the user's own message")
	}
}

func TestPauseSessionGoalStopsTheGoalContinuationTurn(t *testing.T) {
	app, store, _ := newGoalBarApp(t)
	const userID = "desktop-user"
	if _, err := store.Set(userID, "提升20%"); err != nil {
		t.Fatal(err)
	}
	ctx := NewLoopContext("goal", 4, nil)
	ctx.Platform = "goal-continuation"
	cleanup := app.imHandler.beginPureCodingRuntime(ctx, userID, "[系统续接][execute] 当前步骤")
	defer cleanup()
	if app.PauseSessionGoal(userID)["status"] != string(goal.StatusPaused) {
		t.Fatal("pause did not pause")
	}
	if !ctx.IsCancelled() {
		t.Fatal("pause should stop the goal continuation")
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
		h.setPendingForegroundText(userID, "[系统续接][execute] 当前步骤")
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
		h.setPendingForegroundText(userID, "[系统续接][execute] 当前步骤")
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
	app.imHandler.setPendingForegroundText(userID, "[系统续接][execute] 当前步骤")
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

func TestGoalCompleteRetiresCodingChecklist(t *testing.T) {
	app, store, _ := newGoalBarApp(t)
	const userID = "desktop-user"
	h := app.imHandler
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	g, err := store.Set(userID, "ship it")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlan(userID, g.GoalID, "freeze the baseline", []goal.Step{{Index: 1, Title: "freeze baseline"}}, []string{"baseline frozen"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateStep(userID, g.GoalID, 1, goal.StepDone, "baseline frozen"); err != nil {
		t.Fatal(err)
	}
	h.storeStickyCodingWorkbenchMemory(userID, stickyCodingWorkbenchMemory{
		Kind:                   "local",
		ExecutionPlan:          "### T1: freeze baseline",
		RequirementRestatement: "ship it",
		PlanRunStarted:         true,
		StepStatuses: []codingWorkbenchStepStatus{
			{Index: 1, Title: "freeze baseline", Status: codingStepPassed},
		},
	})

	if got := h.goalCompleteForUser(userID, map[string]interface{}{"summary": "baseline frozen"}); strings.Contains(got, "错误") {
		t.Fatalf("complete refused: %s", got)
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if len(mem.StepStatuses) != 0 || mem.PlanRunStarted || mem.ExecutionPlan != "" || mem.RequirementRestatement != "" {
		t.Fatalf("completed goal left a live checklist: %+v", mem)
	}
}

func TestGoalCompleteClearsTheCallingHandlerChecklist(t *testing.T) {
	app, store, _ := newGoalBarApp(t)
	const userID = "desktop-user"
	// Hub reconnect can leave the app pointing at another handler instance.
	// The tool call still owns the checklist on the handler that ran it.
	caller := &IMMessageHandler{app: app, goalStore: store}
	t.Cleanup(func() {
		caller.clearStickyCodingWorkbenchMemory(userID)
		app.imHandler.clearStickyCodingWorkbenchMemory(userID)
	})
	g, err := store.Set(userID, "ship it")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlan(userID, g.GoalID, "freeze the baseline", []goal.Step{{Index: 1, Title: "freeze baseline"}}, []string{"baseline frozen"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateStep(userID, g.GoalID, 1, goal.StepDone, "baseline frozen"); err != nil {
		t.Fatal(err)
	}
	caller.storeStickyCodingWorkbenchMemory(userID, stickyCodingWorkbenchMemory{
		Kind: "local",
		StepStatuses: []codingWorkbenchStepStatus{
			{Index: 1, Title: "freeze baseline", Status: codingStepPassed},
		},
	})

	if got := caller.goalCompleteForUser(userID, map[string]interface{}{"summary": "baseline frozen"}); strings.Contains(got, "错误") {
		t.Fatalf("complete refused: %s", got)
	}
	if mem := caller.getStickyCodingWorkbenchMemory(userID); len(mem.StepStatuses) != 0 {
		t.Fatalf("the handler that completed the goal kept its checklist: %+v", mem)
	}
}

func TestGoalStepEventKeepsCodingChecklist(t *testing.T) {
	app, store, engine := newGoalBarApp(t)
	const userID = "desktop-user"
	h := app.imHandler
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	g, err := store.Set(userID, "work in progress")
	if err != nil {
		t.Fatal(err)
	}
	h.storeStickyCodingWorkbenchMemory(userID, stickyCodingWorkbenchMemory{
		Kind: "local",
		StepStatuses: []codingWorkbenchStepStatus{
			{Index: 1, Title: "running", Status: codingStepRunning},
		},
	})

	engine.emitGoalStateChanged(userID, g)
	if mem := h.getStickyCodingWorkbenchMemory(userID); len(mem.StepStatuses) != 1 {
		t.Fatalf("an active goal event retired the live checklist: %+v", mem)
	}
}

func TestGoalCancelRetiresCodingChecklist(t *testing.T) {
	app, store, _ := newGoalBarApp(t)
	const userID = "desktop-user"
	h := app.imHandler
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	if _, err := store.Set(userID, "work in progress"); err != nil {
		t.Fatal(err)
	}
	h.storeStickyCodingWorkbenchMemory(userID, stickyCodingWorkbenchMemory{
		Kind: "local",
		StepStatuses: []codingWorkbenchStepStatus{
			{Index: 1, Title: "still running", Status: codingStepRunning},
		},
	})

	if got := h.goalClearForUser(userID); !strings.Contains(got, "目标已清除") {
		t.Fatalf("clear=%q", got)
	}
	if mem := h.getStickyCodingWorkbenchMemory(userID); len(mem.StepStatuses) != 0 {
		t.Fatalf("cancelled goal left its checklist: %+v", mem)
	}
}

func TestGoalClearKeepsLivePlanWhenRowAlreadyComplete(t *testing.T) {
	app, store, _ := newGoalBarApp(t)
	const userID = "desktop-user"
	h := app.imHandler
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	g, err := store.Set(userID, "done work")
	if err != nil {
		t.Fatal(err)
	}
	if !store.UpdateStatus(userID, g.GoalID, goal.StatusComplete, "done") {
		t.Fatal("complete the history row")
	}
	h.storeStickyCodingWorkbenchMemory(userID, stickyCodingWorkbenchMemory{
		Kind: "local",
		StepStatuses: []codingWorkbenchStepStatus{
			{Index: 1, Title: "new plan", Status: codingStepPending},
		},
	})

	if got := h.goalClearForUser(userID); !strings.Contains(got, "目标已清除") {
		t.Fatalf("clear=%q", got)
	}
	if mem := h.getStickyCodingWorkbenchMemory(userID); len(mem.StepStatuses) != 1 || mem.StepStatuses[0].Title != "new plan" {
		t.Fatalf("cleaning a completed row wiped a live plan: %+v", mem)
	}
}

func TestGoalClearKeepsLivePlanWhenRowPaused(t *testing.T) {
	app, store, _ := newGoalBarApp(t)
	const userID = "desktop-user"
	h := app.imHandler
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	g, err := store.Set(userID, "paused work")
	if err != nil {
		t.Fatal(err)
	}
	if !store.UpdateStatus(userID, g.GoalID, goal.StatusPaused, "用户已暂停") {
		t.Fatal("pause the history row")
	}
	h.storeStickyCodingWorkbenchMemory(userID, stickyCodingWorkbenchMemory{
		Kind: "local",
		StepStatuses: []codingWorkbenchStepStatus{
			{Index: 1, Title: "new plan", Status: codingStepPending},
		},
	})

	if got := h.goalClearForUser(userID); !strings.Contains(got, "目标已清除") {
		t.Fatalf("clear=%q", got)
	}
	if mem := h.getStickyCodingWorkbenchMemory(userID); len(mem.StepStatuses) != 1 || mem.StepStatuses[0].Title != "new plan" {
		t.Fatalf("cleaning a paused row wiped a live plan: %+v", mem)
	}
}

func TestCancelCompletedGoalKeepsLivePlan(t *testing.T) {
	app, store, _ := newGoalBarApp(t)
	const userID = "desktop-user"
	h := app.imHandler
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	g, err := store.Set(userID, "done work")
	if err != nil {
		t.Fatal(err)
	}
	if !store.UpdateStatus(userID, g.GoalID, goal.StatusComplete, "done") {
		t.Fatal("complete the history row")
	}
	h.storeStickyCodingWorkbenchMemory(userID, stickyCodingWorkbenchMemory{
		Kind: "local",
		StepStatuses: []codingWorkbenchStepStatus{
			{Index: 1, Title: "new plan", Status: codingStepPending},
		},
	})

	if app.CancelSessionGoal(userID)["visible"] != false {
		t.Fatal("cancel did not hide the bar")
	}
	if mem := h.getStickyCodingWorkbenchMemory(userID); len(mem.StepStatuses) != 1 || mem.StepStatuses[0].Title != "new plan" {
		t.Fatalf("deleting a completed row wiped a live plan: %+v", mem)
	}
}

func TestGoalClearRetiresChecklistWhenOtherStoreWasLive(t *testing.T) {
	app, engineStore, _ := newGoalBarApp(t)
	const userID = "desktop-user"
	h := app.imHandler
	other := goal.NewStore("")
	h.goalStore = other
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	done, err := other.Set(userID, "done in the handler store")
	if err != nil {
		t.Fatal(err)
	}
	if !other.UpdateStatus(userID, done.GoalID, goal.StatusComplete, "done") {
		t.Fatal("complete the history row")
	}
	if _, err := engineStore.Set(userID, "still running in the engine store"); err != nil {
		t.Fatal(err)
	}
	h.storeStickyCodingWorkbenchMemory(userID, stickyCodingWorkbenchMemory{
		Kind: "local",
		StepStatuses: []codingWorkbenchStepStatus{
			{Index: 1, Title: "live", Status: codingStepRunning},
		},
	})

	if got := h.goalClearForUser(userID); !strings.Contains(got, "目标已清除") {
		t.Fatalf("clear=%q", got)
	}
	if mem := h.getStickyCodingWorkbenchMemory(userID); len(mem.StepStatuses) != 0 {
		t.Fatalf("a live row in the engine store kept its checklist: %+v", mem)
	}
}

func TestCancelGoalRetiresChecklistWhenOtherStoreWasLive(t *testing.T) {
	app, engineStore, _ := newGoalBarApp(t)
	const userID = "desktop-user"
	h := app.imHandler
	other := goal.NewStore("")
	h.goalStore = other
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	done, err := engineStore.Set(userID, "done in the engine store")
	if err != nil {
		t.Fatal(err)
	}
	if !engineStore.UpdateStatus(userID, done.GoalID, goal.StatusComplete, "done") {
		t.Fatal("complete the history row")
	}
	if _, err := other.Set(userID, "still running in the handler store"); err != nil {
		t.Fatal(err)
	}
	h.storeStickyCodingWorkbenchMemory(userID, stickyCodingWorkbenchMemory{
		Kind: "local",
		StepStatuses: []codingWorkbenchStepStatus{
			{Index: 1, Title: "live", Status: codingStepRunning},
		},
	})

	if app.CancelSessionGoal(userID)["visible"] != false {
		t.Fatal("cancel did not hide the bar")
	}
	if mem := h.getStickyCodingWorkbenchMemory(userID); len(mem.StepStatuses) != 0 {
		t.Fatalf("a live row in the handler store kept its checklist: %+v", mem)
	}
}
