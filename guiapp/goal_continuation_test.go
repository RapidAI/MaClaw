package guiapp

import (
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
