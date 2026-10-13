package guiapp

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/goal"
)

func TestMaybeScheduleGoalContinuationSkipsAfterUserCancel(t *testing.T) {
	store := goal.NewStore("")
	g, err := store.Set("desktop-user", "keep working", goal.WithMaxTurns(3))
	if err != nil {
		t.Fatalf("Set goal: %v", err)
	}

	engine := NewGoalContinuationEngine(store, nil)
	engine.cooldown = 0
	handler := &IMMessageHandler{
		app: &App{
			goalContinuation: engine,
		},
	}
	handler.markTaskCancelledByUser("desktop-user")

	handler.maybeScheduleGoalContinuation("desktop-user", &IMAgentResponse{Text: "partial"}, "desktop")

	engine.mu.Lock()
	_, scheduled := engine.scheduledTimers["desktop-user"]
	engine.mu.Unlock()
	if scheduled {
		t.Fatal("cancelled task must not schedule another goal continuation")
	}
	if got := store.Get("desktop-user"); got == nil || got.GoalID != g.GoalID || got.Status != goal.StatusActive {
		t.Fatalf("goal state changed unexpectedly: %+v", got)
	}
}

func TestMaybeScheduleGoalContinuationSkipsUnarmedRemoteEvenWithLocalPending(t *testing.T) {
	userID := "remote-goal-local-pending"
	store := goal.NewStore("")
	if _, err := store.Set(userID, "开发 clamav 检测工具", goal.WithMaxTurns(3)); err != nil {
		t.Fatalf("Set goal: %v", err)
	}
	engine := NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending(userID) })
	handler := &IMMessageHandler{app: &App{goalContinuation: engine}}
	handler.storeStickyCodingWorkbenchMemory(userID, stickyCodingWorkbenchMemory{
		Kind:          "remote",
		RemoteHost:    "home.rapidaltech",
		RemoteWorkDir: "/home/ra/clamav",
	})
	handler.pendingV2SubAgentExecution.Store(userID, true)
	handler.pendingTemplateCodingProjectPath.Store(userID, `D:\workprj\clamav`)

	handler.maybeScheduleGoalContinuation(userID, &IMAgentResponse{Text: "partial"}, "desktop")

	engine.mu.Lock()
	_, scheduled := engine.scheduledTimers[userID]
	engine.mu.Unlock()
	if scheduled {
		t.Fatal("unarmed remote coding must not schedule a desktop goal continuation")
	}
}

func TestBuildContinuationPromptPlanThenCurrentStep(t *testing.T) {
	store := goal.NewStore("")
	g, err := store.Set("user", "实现登录")
	if err != nil {
		t.Fatal(err)
	}
	engine := NewGoalContinuationEngine(store, nil)
	planPrompt := engine.buildContinuationPrompt(store.Get("user"))
	if !strings.Contains(planPrompt, "分解") {
		t.Fatalf("plan prompt = %q", planPrompt)
	}
	if strings.Contains(planPrompt, "请继续实现") {
		t.Fatalf("plan prompt must not ask to implement: %q", planPrompt)
	}

	if err := store.SavePlan("user", g.GoalID, "交付可登录接口", []goal.Step{
		{Title: "检查环境"},
		{Title: "编写代码"},
	}, []string{"go test 通过"}); err != nil {
		t.Fatal(err)
	}
	execPrompt := engine.buildContinuationPrompt(store.Get("user"))
	if !strings.Contains(execPrompt, "检查环境") {
		t.Fatalf("execute prompt missing current step: %q", execPrompt)
	}
	if strings.Contains(execPrompt, "编写代码") {
		t.Fatalf("execute prompt included a later step: %q", execPrompt)
	}
}

func TestGoalContinuationTurnErrorAndMissingPlanFail(t *testing.T) {
	userID := "goal-turn-error"
	store := goal.NewStore("")
	if _, err := store.Set(userID, "实现登录"); err != nil {
		t.Fatal(err)
	}
	engine := NewGoalContinuationEngine(store, nil)
	handler := &IMMessageHandler{app: &App{goalContinuation: engine}, goalStore: store}

	handler.maybeScheduleGoalContinuation(userID, &IMAgentResponse{Error: "编译失败"}, "goal-continuation")
	got := store.Get(userID)
	if got == nil || got.Status != goal.StatusFailed || !strings.Contains(got.Summary, "编译失败") {
		t.Fatalf("turn error should fail the goal: %+v", got)
	}

	userID = "goal-no-plan"
	if _, err := store.Set(userID, "实现登录"); err != nil {
		t.Fatal(err)
	}
	handler.maybeScheduleGoalContinuation(userID, &IMAgentResponse{Text: "好的"}, "goal-continuation")
	got = store.Get(userID)
	if got == nil || got.Status != goal.StatusFailed || !strings.Contains(got.Summary, "未能分解任务") {
		t.Fatalf("missing plan should fail the goal: %+v", got)
	}
}

func TestFireContinuationWithoutHubFailsGoal(t *testing.T) {
	store := goal.NewStore("")
	g, err := store.Set("user", "实现登录")
	if err != nil {
		t.Fatal(err)
	}
	engine := NewGoalContinuationEngine(store, nil)
	engine.fireContinuation("user", g.GoalID)
	got := store.Get("user")
	if got == nil || got.Status != goal.StatusFailed || !strings.Contains(got.Summary, "未就绪") {
		t.Fatalf("missing hub should fail the goal: %+v", got)
	}
}

func TestNoToolPauseNoticeMentionsResume(t *testing.T) {
	userID := "goal-no-tool"
	store := goal.NewStore("")
	g, err := store.Set(userID, "实现登录")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlan(userID, g.GoalID, "先检查再写代码", []goal.Step{{Title: "检查环境"}}, []string{"go test 通过"}); err != nil {
		t.Fatal(err)
	}
	engine := NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending(userID) })

	engine.MaybeScheduleContinuation(userID, g.GoalID, 10, 5, false)
	engine.MaybeScheduleContinuation(userID, g.GoalID, 10, 5, false)
	got := store.Get(userID)
	if got == nil || got.Status != goal.StatusPaused {
		t.Fatalf("status = %+v", got)
	}
	text, asError := goalTerminalNotice(got)
	if asError || !strings.Contains(text, "/goal resume") {
		t.Fatalf("notice = %q asError=%v", text, asError)
	}
}
