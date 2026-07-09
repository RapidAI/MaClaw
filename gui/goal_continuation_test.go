package main

import (
	"testing"

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
