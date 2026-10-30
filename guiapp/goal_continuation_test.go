package guiapp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/goal"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestGoalContinuationLifecycleToolStaysRequiredWhenRanked(t *testing.T) {
	provisions := tool.LegacyAdapterProvisions()
	defs := make([]map[string]interface{}, 0, len(provisions))
	ranked := make([]string, 0, len(provisions))
	now := time.Now().UTC()
	for _, provision := range provisions {
		if _, ok := tool.LegacyAdapterProvisionForTool(provision.ToolName, now); !ok {
			continue
		}
		defs = append(defs, map[string]interface{}{
			"type":     "function",
			"function": map[string]interface{}{"name": provision.ToolName, "parameters": map[string]interface{}{"type": "object"}},
		})
		ranked = append(ranked, provision.ToolName)
	}
	if len(defs) <= tool.MaxToolBudget {
		t.Fatalf("catalog %d does not exceed budget %d", len(defs), tool.MaxToolBudget)
	}
	for i, name := range ranked {
		if name != goalContinuationLifecycleTool {
			continue
		}
		ranked = append(ranked[:i], ranked[i+1:]...)
		ranked = append(ranked, name)
		break
	}
	pruned, _, err := renderReviewedLegacySurface("[系统续接]", defs, ranked)
	if err != nil {
		t.Fatal(err)
	}
	if containsAgentLoopToolNamed(pruned, goalContinuationLifecycleTool) {
		t.Fatal("a low-ranked goal is a retrieval candidate and the count guard drops it")
	}
	kept, _, err := renderReviewedLegacySurface("[系统续接]", defs, rankedNamesWithout(ranked, goalContinuationLifecycleTool))
	if err != nil {
		t.Fatal(err)
	}
	if !containsAgentLoopToolNamed(kept, goalContinuationLifecycleTool) {
		t.Fatal("goal present on the surface but absent from the rank list must stay host_policy_required")
	}
}

func TestSharedLoopGoalPlanPhaseDropsMutationTools(t *testing.T) {
	h := &IMMessageHandler{app: &App{}}
	h.toolDefGen = NewToolDefinitionGenerator(nil, []map[string]interface{}{
		toolDef("goal", "manage a goal", nil, nil),
		toolDef("read_file", "read", nil, nil),
		toolDef("list_directory", "list", nil, nil),
		toolDef("write_file", "write", nil, nil),
		toolDef("edit_file", "edit", nil, nil),
		toolDef("bash", "shell", nil, nil),
		toolDef("craft_tool", "craft", nil, nil),
	})
	planCtx := &LoopContext{Platform: "goal-continuation"}
	planCtx.Runtime.Execution = fullExecutionProfile("goal continuation")
	plan := h.prepareAgentLoopTools("desktop-user", "[系统续接][plan] 本轮只做规划。调用 goal(action=\"plan\")。", planCtx, agentLoopPhase{})
	if !containsAgentLoopToolNamed(plan.Tools, "goal") || !containsAgentLoopToolNamed(plan.Tools, "read_file") {
		t.Fatalf("plan surface = %v", agentLoopToolNamesForLog(plan.Tools))
	}
	for _, blocked := range []string{"bash", "write_file", "edit_file", "craft_tool"} {
		if containsAgentLoopToolNamed(plan.Tools, blocked) {
			t.Fatalf("plan surface kept %s: %v", blocked, agentLoopToolNamesForLog(plan.Tools))
		}
		if containsAgentLoopToolNamed(plan.BaseTools, blocked) {
			t.Fatalf("plan base surface kept %s", blocked)
		}
	}

	execCtx := &LoopContext{Platform: "goal-continuation"}
	execCtx.Runtime.Execution = fullExecutionProfile("goal continuation")
	exec := h.prepareAgentLoopTools("desktop-user", "[系统续接][execute] 做完后调用 goal(action=\"step\")。", execCtx, agentLoopPhase{})
	if !containsAgentLoopToolNamed(exec.Tools, "goal") || !containsAgentLoopToolNamed(exec.Tools, "write_file") {
		t.Fatalf("execute surface = %v", agentLoopToolNamesForLog(exec.Tools))
	}

	ordinaryCtx := &LoopContext{}
	ordinaryCtx.Runtime.Execution = fullExecutionProfile("user")
	ordinary := h.prepareAgentLoopTools("desktop-user", "请本轮只做规划，先别写代码", ordinaryCtx, agentLoopPhase{})
	if !containsAgentLoopToolNamed(ordinary.Tools, "write_file") {
		t.Fatalf("ordinary request lost write_file: %v", agentLoopToolNamesForLog(ordinary.Tools))
	}
}

func TestGoalContinuationTurnCarriesCodingIngressForWorkbench(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { app.closeCodingTaskRelationService() })
	handler := &IMMessageHandler{app: app, memory: agent.NewConversationMemory()}
	t.Cleanup(handler.memory.Stop)
	app.imHandler = handler
	owner := projectSessionOwnerID("C:/workspace/goal-cont")
	handler.storeStickyCodingWorkbenchMemory(owner, stickyCodingWorkbenchMemory{Kind: "local"})

	// A goal round is a host turn without a user send: the turn boundary is
	// what must mint its credential, so this exercises that boundary.
	boundary := handler.enterIMMessageSerializationBoundary(IMUserMessage{
		RequestID: "goal-round", UserID: owner, Platform: "goal-continuation", Text: "[系统续接] 继续执行",
	}, nil, nil, nil, explicitTaskSlotDecision{})
	// The sweep is part of the same boundary (owner lock held); always run it.
	defer boundary.Unlock()
	if boundary.CodingIngressToken == "" {
		t.Fatal("coding workbench continuation turn must carry a desktop ingress token")
	}
	_, _, handle, _, ok := app.nextDesktopCodingRootStepRelation(boundary.CodingIngressToken, owner)
	if !ok || !handle.complete() {
		t.Fatal("minted token did not bind a desktop relation")
	}

	plain := handler.enterIMMessageSerializationBoundary(IMUserMessage{
		RequestID: "chat-turn", UserID: "desktop-user", Platform: "goal-continuation", Text: "[系统续接] 继续执行",
	}, nil, nil, nil, explicitTaskSlotDecision{})
	defer plain.Unlock()
	if plain.CodingIngressToken != "" {
		t.Fatal("a chat session must not receive a coding ingress token")
	}
}

func TestGoalContinuationPinsLifecycleToolBeforeClosedPlan(t *testing.T) {
	goalDef := map[string]interface{}{
		"type":     "function",
		"function": map[string]interface{}{"name": "goal"},
	}
	first := ensureNamedToolsPresent(nil, []map[string]interface{}{goalDef}, []string{"goal"})
	if len(first) != 1 || extractToolName(first[0]) != "goal" {
		t.Fatalf("pin=%v", agentLoopToolNamesForLog(first))
	}
	again := ensureNamedToolsPresent(first, []map[string]interface{}{goalDef}, []string{"goal"})
	if len(again) != 1 {
		t.Fatalf("duplicated goal tool: %v", agentLoopToolNamesForLog(again))
	}
}

func TestGoalContinuationSkipsSemanticClassification(t *testing.T) {
	h := &IMMessageHandler{unifiedClassifier: semanticClassifierForLabel(t, intent.LabelGoalManage)}
	profile, semantic := h.classifyIMExecutionProfileAndSemantic(IMUserMessage{
		Platform: "goal-continuation",
		Text:     "[系统续接] 先理解并分解目标，不要实现。\n\n目标：做一个游戏",
	}, false, false)
	if semantic != nil {
		t.Fatalf("continuation must keep the lifecycle tool, not a fresh goal_manage grant: %+v", semantic)
	}
	if profile.Reason != "goal continuation" || profile.Layer != string(executionLayerFull) {
		t.Fatalf("profile=%+v", profile)
	}
}

func TestGoalContinuationWaitsForAskUserButNotAProseQuestion(t *testing.T) {
	userID := "goal-ask-wait"
	store := goal.NewStore("")
	g, err := store.Set(userID, "做游戏", goal.WithMaxTurns(5))
	if err != nil {
		t.Fatalf("Set goal: %v", err)
	}
	engine := NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending(userID) })
	handler := &IMMessageHandler{app: &App{goalContinuation: engine}}

	handler.pendingUserReply.Store(userID, &pendingUserReplyState{
		Question:  "半径怎么取？",
		History:   []agent.ConversationEntry{{Role: "assistant", Content: "半径怎么取？"}},
		Timestamp: time.Now(),
	})
	handler.maybeScheduleGoalContinuation(userID, &IMAgentResponse{Text: "半径怎么取？"}, "goal-continuation")
	if !goalTimerScheduled(t, engine, userID) {
		t.Fatal("a prose question must still schedule the next goal turn")
	}
	engine.CancelPending(userID)

	handler.pendingAskUser.Store(userID, &pendingAskUserState{
		Question:  "半径用 10 还是 20？",
		History:   []agent.ConversationEntry{{Role: "assistant", Content: "半径用 10 还是 20？"}},
		Timestamp: time.Now(),
	})
	handler.maybeScheduleGoalContinuation(userID, &IMAgentResponse{Text: "半径用 10 还是 20？", InputTokens: 10, OutputTokens: 5}, "goal-continuation")
	if goalTimerScheduled(t, engine, userID) {
		t.Fatal("ask_user must hold the next continuation")
	}
	held := store.Get(userID)
	if held == nil || held.GoalID != g.GoalID || held.Status != goal.StatusActive || held.TurnsUsed != 2 {
		t.Fatalf("ask_user wait changed the goal: %+v", held)
	}

	handler.pendingAskUser.Store(userID, &pendingAskUserState{
		Question:  "过期的问题",
		Timestamp: time.Now().Add(-pendingReplyTTL - time.Second),
	})
	handler.maybeScheduleGoalContinuation(userID, &IMAgentResponse{Text: "继续"}, "desktop")
	if !goalTimerScheduled(t, engine, userID) {
		t.Fatal("an expired ask_user must not keep the goal waiting")
	}
}

func TestAskUserTurnDoesNotScheduleAfterTheAnswerClearsIt(t *testing.T) {
	userID := "goal-ask-late"
	store := goal.NewStore("")
	g, err := store.Set(userID, "做游戏", goal.WithMaxTurns(5))
	if err != nil {
		t.Fatalf("Set goal: %v", err)
	}
	if err := store.SavePlan(userID, g.GoalID, "先做物理", []goal.Step{{Title: "物理"}}, []string{"能发射"}); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}
	engine := NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending(userID) })
	handler := &IMMessageHandler{app: &App{goalContinuation: engine}}

	handler.maybeScheduleGoalContinuation(userID, &IMAgentResponse{
		Text:           "半径用 10 还是 20？",
		ResponseSource: imResponseSourceAskUser.String(),
	}, "goal-continuation")
	if goalTimerScheduled(t, engine, userID) {
		t.Fatal("the ask_user turn must not schedule once the answer has already cleared the pending question")
	}

	engine.scheduleDelayed(userID, g.GoalID)
	handler.maybeScheduleGoalContinuation(userID, &IMAgentResponse{
		Text:           "半径用 10 还是 20？",
		ResponseSource: imResponseSourceAskUser.String(),
	}, "goal-continuation")
	if !goalTimerScheduled(t, engine, userID) {
		t.Fatal("a late ask_user post-loop must leave the continuation queued by the answer")
	}
	if got := store.Get(userID); got == nil || got.Status != goal.StatusActive {
		t.Fatalf("goal status = %+v", got)
	}
}

func TestLateAskUserPostLoopKeepsTheNextTurnsExpect(t *testing.T) {
	userID := "goal-ask-expect"
	store := goal.NewStore("")
	g, err := store.Set(userID, "做游戏", goal.WithMaxTurns(5))
	if err != nil {
		t.Fatalf("Set goal: %v", err)
	}
	if err := store.SavePlan(userID, g.GoalID, "先做物理", []goal.Step{{Title: "物理"}}, []string{"能发射"}); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}
	engine := NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending(userID) })
	handler := &IMMessageHandler{app: &App{goalContinuation: engine}}

	engine.noteContinuationExpect(userID, store.Get(userID))
	owned := engine.peekContinuationExpect(userID)
	handler.maybeScheduleGoalContinuationOwned(userID, &IMAgentResponse{
		ResponseSource: imResponseSourceAskUser.String(),
	}, "goal-continuation", owned, true)
	if got := engine.peekContinuationExpect(userID); got.goalID != "" {
		t.Fatalf("this turn's expect should be retired, got %+v", got)
	}

	engine.noteContinuationExpect(userID, store.Get(userID))
	owned = engine.peekContinuationExpect(userID)
	if err := store.UpdateStep(userID, g.GoalID, 1, goal.StepProgress, "人还没选半径"); err != nil {
		t.Fatalf("UpdateStep: %v", err)
	}
	engine.noteContinuationExpect(userID, store.Get(userID))
	next := engine.peekContinuationExpect(userID)
	handler.maybeScheduleGoalContinuationOwned(userID, &IMAgentResponse{
		ResponseSource: imResponseSourceAskUser.String(),
	}, "goal-continuation", owned, true)
	if got := engine.peekContinuationExpect(userID); got != next {
		t.Fatalf("late ask_user post-loop retired the next turn's expect: got %+v want %+v", got, next)
	}

	engine.noteContinuationExpect(userID, store.Get(userID))
	owned = engine.peekContinuationExpect(userID)
	engine.noteContinuationExpect(userID, store.Get(userID))
	next = engine.peekContinuationExpect(userID)
	if next.generation == 0 || next.generation == owned.generation {
		t.Fatalf("same step reused a generation: owned %+v next %+v", owned, next)
	}
	if next.stepIndex != owned.stepIndex || next.stepStatus != owned.stepStatus || next.stepResult != owned.stepResult {
		t.Fatalf("step identity changed: owned %+v next %+v", owned, next)
	}
	handler.maybeScheduleGoalContinuationOwned(userID, &IMAgentResponse{
		ResponseSource: imResponseSourceAskUser.String(),
	}, "goal-continuation", owned, true)
	if got := engine.peekContinuationExpect(userID); got != next {
		t.Fatalf("late ask_user post-loop retired the same-step note: got %+v want %+v", got, next)
	}
}

func TestOwnedContinuationStillSchedulesItsOwnTurn(t *testing.T) {
	userID := "goal-owned-schedule"
	store := goal.NewStore("")
	g, err := store.Set(userID, "做游戏", goal.WithMaxTurns(5))
	if err != nil {
		t.Fatalf("Set goal: %v", err)
	}
	if err := store.SavePlan(userID, g.GoalID, "先做物理", []goal.Step{{Title: "物理"}}, []string{"能发射"}); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}
	engine := NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending(userID) })
	handler := &IMMessageHandler{app: &App{goalContinuation: engine}}

	engine.noteContinuationExpect(userID, store.Get(userID))
	owned := engine.peekContinuationExpect(userID)
	handler.maybeScheduleGoalContinuationOwned(userID, &IMAgentResponse{TraceEventCount: 1}, "goal-continuation", owned, true)
	if got := engine.peekContinuationExpect(userID); got.goalID != "" {
		t.Fatalf("this turn's note should be retired, got %+v", got)
	}
	if !goalTimerScheduled(t, engine, userID) {
		t.Fatal("this turn should schedule the next continuation")
	}
	stored := store.Get(userID)
	if stored == nil || stored.Status != goal.StatusActive || stored.TurnsUsed != 1 {
		t.Fatalf("usage = %+v", stored)
	}
}

func TestLateContinuationPostLoopLeavesTheNextTurnsExpect(t *testing.T) {
	userID := "goal-late-same-step"
	store := goal.NewStore("")
	g, err := store.Set(userID, "做游戏", goal.WithMaxTurns(5))
	if err != nil {
		t.Fatalf("Set goal: %v", err)
	}
	if err := store.SavePlan(userID, g.GoalID, "先做物理", []goal.Step{{Title: "物理"}}, []string{"能发射"}); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}
	engine := NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending(userID) })
	handler := &IMMessageHandler{app: &App{goalContinuation: engine}}

	engine.noteContinuationExpect(userID, store.Get(userID))
	owned := engine.peekContinuationExpect(userID)
	engine.scheduleDelayed(userID, g.GoalID)
	engine.noteContinuationExpect(userID, store.Get(userID))
	next := engine.peekContinuationExpect(userID)
	if next.generation == owned.generation || next.stepStatus != owned.stepStatus || next.stepResult != owned.stepResult {
		t.Fatalf("successor note = %+v owned %+v", next, owned)
	}
	engine.mu.Lock()
	gen := engine.scheduleGen[userID]
	engine.mu.Unlock()

	handler.maybeScheduleGoalContinuationOwned(userID, &IMAgentResponse{Text: "还在做物理"}, "goal-continuation", owned, true)
	if got := engine.peekContinuationExpect(userID); got != next {
		t.Fatalf("late post-loop retired the next note: got %+v want %+v", got, next)
	}
	if !goalTimerScheduled(t, engine, userID) {
		t.Fatal("late post-loop dropped the queued continuation")
	}
	engine.mu.Lock()
	gotGen := engine.scheduleGen[userID]
	engine.mu.Unlock()
	if gotGen != gen {
		t.Fatalf("late post-loop replaced the timer: gen %d -> %d", gen, gotGen)
	}
	stored := store.Get(userID)
	if stored == nil || stored.Status != goal.StatusActive || stored.TurnsUsed != 1 {
		t.Fatalf("usage = %+v", stored)
	}
}

func TestLateTurnErrorKeepsAStoredPlanAndTheNextExpect(t *testing.T) {
	userID := "goal-late-error"
	store := goal.NewStore("")
	g, err := store.Set(userID, "做游戏", goal.WithMaxTurns(5))
	if err != nil {
		t.Fatalf("Set goal: %v", err)
	}
	engine := NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending(userID) })
	handler := &IMMessageHandler{app: &App{goalContinuation: engine}}

	engine.noteContinuationExpect(userID, store.Get(userID))
	owned := engine.peekContinuationExpect(userID)
	if err := store.SavePlan(userID, g.GoalID, "先做物理", []goal.Step{{Title: "物理"}}, []string{"能发射"}); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}
	engine.scheduleDelayed(userID, g.GoalID)
	engine.noteContinuationExpect(userID, store.Get(userID))
	next := engine.peekContinuationExpect(userID)
	engine.mu.Lock()
	gen := engine.scheduleGen[userID]
	engine.mu.Unlock()

	handler.maybeScheduleGoalContinuationOwned(userID, &IMAgentResponse{Error: "编译失败"}, "goal-continuation", owned, true)
	got := store.Get(userID)
	if got == nil || got.Status != goal.StatusActive || len(got.Steps) != 1 {
		t.Fatalf("stored plan must survive the late error: %+v", got)
	}
	if peeked := engine.peekContinuationExpect(userID); peeked != next {
		t.Fatalf("late error retired the next note: got %+v want %+v", peeked, next)
	}
	if !goalTimerScheduled(t, engine, userID) {
		t.Fatal("late error cancelled the queued continuation")
	}
	engine.mu.Lock()
	gotGen := engine.scheduleGen[userID]
	engine.mu.Unlock()
	if gotGen != gen {
		t.Fatalf("late error replaced the timer: gen %d -> %d", gen, gotGen)
	}

	userID = "goal-late-error-open"
	if _, err := store.Set(userID, "做游戏"); err != nil {
		t.Fatalf("Set goal: %v", err)
	}
	engine = NewGoalContinuationEngine(store, nil)
	t.Cleanup(func() { engine.CancelPending(userID) })
	handler = &IMMessageHandler{app: &App{goalContinuation: engine}}
	engine.noteContinuationExpect(userID, store.Get(userID))
	owned = engine.peekContinuationExpect(userID)
	engine.noteContinuationExpect(userID, store.Get(userID))
	handler.maybeScheduleGoalContinuationOwned(userID, &IMAgentResponse{Error: "编译失败"}, "goal-continuation", owned, true)
	got = store.Get(userID)
	if got == nil || got.Status != goal.StatusFailed || !strings.Contains(got.Summary, "编译失败") {
		t.Fatalf("open contract should still fail: %+v", got)
	}
}

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
	got := store.Get("desktop-user")
	if got == nil || got.GoalID != g.GoalID || got.Status != goal.StatusPaused {
		t.Fatalf("user stop should pause the same goal: %+v", got)
	}
	if !strings.Contains(got.Summary, "停止") || strings.Contains(got.Summary, "SSH") || strings.Contains(got.Summary, "远程") || strings.Contains(got.Summary, "工具") {
		t.Fatalf("user-stop summary = %q", got.Summary)
	}
	if !handler.hasCancelledTaskBoundary("desktop-user") {
		t.Fatal("user stop must keep the cancel fence")
	}
}

func TestMaybeScheduleGoalContinuationSkipsUnarmedRemoteEvenWithLocalPending(t *testing.T) {
	userID := stickyTestUserID(t)
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
	got := store.Get(userID)
	if scheduled || got == nil || got.Status != goal.StatusPaused || !strings.Contains(got.Summary, "SSH") {
		t.Fatalf("unarmed remote must stay paused without a desktop turn: scheduled=%v goal=%+v", scheduled, got)
	}
}

func TestMaybeScheduleGoalContinuationArmsTheStoredSlashKey(t *testing.T) {
	const asked = `desktop-user:F:/bird2`
	const stored = `desktop-user:F:\bird2`
	app, store, engine := newGoalBarApp(t)
	t.Cleanup(func() {
		engine.CancelPending(asked)
		engine.CancelPending(stored)
	})
	g, err := store.Set(stored, "图形版本愤怒的小鸟，c++语言")
	if err != nil {
		t.Fatal(err)
	}
	if !store.UpdateStatus(stored, g.GoalID, goal.StatusFailed, "Billing daily count exceeded") {
		t.Fatal("failed to mark the goal failed")
	}
	handler := app.imHandler
	// Keep the sticky lookup off disk. This id is only a slash spelling.
	handler.stickyCodingWorkbenchMemory.Store(asked, stickyCodingMemoryMiss{})
	// The user turn re-arms the stored row and must not queue a second turn.
	// Post-loop then calls maybeSchedule with the composer id.
	handler.releaseStoppedGoalForUserContinue(IMUserMessage{UserID: asked, Platform: "desktop", Text: "继续这个任务"}, stoppedGoalUserTurn{})
	if goalBarPending(engine, stored) || goalBarPending(engine, asked) {
		t.Fatal("the user turn must not schedule beside itself")
	}
	released := store.Get(stored)
	if released == nil || released.Status != goal.StatusActive || released.Summary != "" || released.ComposerClockStopped() {
		t.Fatalf("release left the stored goal stopped: %+v", released)
	}
	engine.mu.Lock()
	engine.scheduledTimers[asked] = time.AfterFunc(time.Hour, func() {})
	engine.mu.Unlock()

	handler.maybeScheduleGoalContinuation(asked, &IMAgentResponse{Text: "本轮做完了"}, "desktop")

	if !goalBarPending(engine, stored) {
		t.Fatal("next turn must be armed on the stored key")
	}
	if goalBarPending(engine, asked) {
		t.Fatal("asked spelling must not keep a second timer")
	}
	got := store.Get(stored)
	if got == nil || got.GoalID != g.GoalID || got.Status != goal.StatusActive {
		t.Fatalf("stored goal = %+v", got)
	}
}

func TestMaybeScheduleGoalContinuationPausesTheStoredSlashTwin(t *testing.T) {
	const asked = `desktop-user:F:/bird2`
	const stored = `desktop-user:F:\bird2`
	app, store, engine := newGoalBarApp(t)
	t.Cleanup(func() {
		engine.CancelPending(asked)
		engine.CancelPending(stored)
	})
	if _, err := store.Set(stored, "图形版本愤怒的小鸟，c++语言"); err != nil {
		t.Fatal(err)
	}
	engine.mu.Lock()
	engine.scheduledTimers[asked] = time.AfterFunc(time.Hour, func() {})
	engine.scheduledTimers[stored] = time.AfterFunc(time.Hour, func() {})
	engine.mu.Unlock()
	handler := app.imHandler
	// In memory only. storeStickyCodingWorkbenchMemory would write this
	// session's sticky file under the real data directory.
	handler.stickyCodingWorkbenchMemory.Store(asked, stickyCodingWorkbenchMemory{
		Kind:          "remote",
		RemoteHost:    "home.rapidaltech",
		RemoteWorkDir: "/home/ra/bird",
	})
	handler.pendingV2SubAgentExecution.Store(asked, true)
	handler.pendingTemplateCodingProjectPath.Store(asked, `E:\bird-test2`)

	handler.maybeScheduleGoalContinuation(asked, &IMAgentResponse{Text: "partial"}, "desktop")

	if goalBarPending(engine, asked) || goalBarPending(engine, stored) {
		t.Fatal("unarmed remote must not leave a timer on either spelling")
	}
	got := store.Get(stored)
	if got == nil || got.Status != goal.StatusPaused || !strings.Contains(got.Summary, "SSH") {
		t.Fatalf("stored slash twin was not paused: %+v", got)
	}
	if other := store.Get(asked); other != nil {
		t.Fatalf("asked spelling grew its own goal: %+v", other)
	}
}

func TestQueuedGoalTurnHoldParksTheStoredSlashKey(t *testing.T) {
	const asked = `desktop-user:F:/bird2`
	const stored = `desktop-user:F:\bird2`
	app, store, engine := newGoalBarApp(t)
	t.Cleanup(func() {
		engine.CancelPending(asked)
		engine.CancelPending(stored)
	})
	g, err := store.Set(stored, "图形版本愤怒的小鸟，c++语言")
	if err != nil {
		t.Fatal(err)
	}
	engine.scheduleDelayed(stored, g.GoalID)
	if !engine.blocksRunWindowCompletion(asked) {
		t.Fatal("composer session treated the queued goal as finished")
	}
	engine.mu.Lock()
	engine.scheduledTimers[asked] = time.AfterFunc(time.Hour, func() {})
	engine.queuedTurn[asked] = queuedGoalTurn{goalID: "stale-twin"}
	engine.mu.Unlock()

	msg := IMUserMessage{UserID: asked, Platform: "desktop", Text: "继续这个任务"}
	end := app.imHandler.beginQueuedGoalTurnHold(msg)
	if goalBarPending(engine, stored) || goalBarPending(engine, asked) {
		t.Fatal("hold left a slash timer live")
	}
	end(false)
	if !goalBarPending(engine, stored) {
		t.Fatal("early return did not restore the stored timer")
	}
	if goalBarPending(engine, asked) {
		t.Fatal("stale asked timer came back")
	}
	end = app.imHandler.beginQueuedGoalTurnHold(msg)
	end(true)
	if goalBarPending(engine, stored) || goalBarPending(engine, asked) {
		t.Fatal("a turn that schedules the next goal restored the parked timer")
	}
}

func TestGoalStepProgressKeepsTheFollowingTurnOnTheSameStep(t *testing.T) {
	h := &IMMessageHandler{goalStore: goal.NewStore("")}
	userID := "desktop-user:D:/goal-progress"
	h.toolGoalForUser(userID, map[string]interface{}{"action": "create", "objective": "做游戏"})
	g := h.getGoalStore().Get(userID)
	if err := h.getGoalStore().SavePlan(userID, g.GoalID, "先骨架再物理", []goal.Step{
		{Title: "工程骨架"},
		{Title: "物理内核"},
	}, []string{"能编译"}); err != nil {
		t.Fatal(err)
	}
	skipped := h.toolGoalForUser(userID, map[string]interface{}{
		"action": "step", "index": 2, "status": "done", "result": "提前做了物理",
	})
	if !strings.Contains(skipped, "只能提交步骤 1") {
		t.Fatalf("skip = %q", skipped)
	}
	progress := h.toolGoalForUser(userID, map[string]interface{}{
		"action": "step", "index": 1, "status": "progress", "result": "骨架已写，还没编译",
	})
	if !strings.Contains(progress, "仍在进行") || !strings.Contains(progress, "不要开始下一步") {
		t.Fatalf("progress = %q", progress)
	}
	stored := h.getGoalStore().Get(userID)
	display := buildContinuationDisplayText(stored)
	if !strings.Contains(display, "目标清单") || !strings.Contains(display, "→ 1. 工程骨架") || !strings.Contains(display, "○ 2. 物理内核") || strings.Contains(display, "/goal") {
		t.Fatalf("display = %q", display)
	}
	prompt := buildGoalExecutePrompt(stored)
	if !strings.Contains(prompt, `status="progress"`) || !strings.Contains(prompt, "骨架已写，还没编译") {
		t.Fatalf("prompt = %s", prompt)
	}
	done := h.toolGoalForUser(userID, map[string]interface{}{
		"action": "step", "index": 1, "status": "done", "result": "窗口能打开",
	})
	if !strings.Contains(done, "步骤 1 已完成") || !strings.Contains(done, "不要开始它") {
		t.Fatalf("done = %q", done)
	}
	stored = h.getGoalStore().Get(userID)
	display = buildContinuationDisplayText(stored)
	if !strings.Contains(display, "✓ 1. 工程骨架") || !strings.Contains(display, "→ 2. 物理内核") || strings.Contains(display, "/goal") {
		t.Fatalf("next display = %q", display)
	}
	prompt = buildGoalExecutePrompt(stored)
	if strings.Contains(prompt, "骨架已写，还没编译") || strings.Contains(prompt, "窗口能打开") {
		t.Fatalf("closed step result was copied into the next step: %s", prompt)
	}
	if !strings.Contains(prompt, "本轮只能提交步骤 2") || strings.Contains(prompt, "理解：") {
		t.Fatalf("execute prompt still includes the plan essay: %s", prompt)
	}
	noted := h.toolGoalForUser(userID, map[string]interface{}{
		"action": "step", "index": "1", "status": "done", "result": "补记选型证据",
	})
	if strings.HasPrefix(noted, "错误") || !strings.Contains(noted, "补充说明已记下") || !strings.Contains(noted, "index=2") {
		t.Fatalf("done-step follow-up = %q", noted)
	}
	stored = h.getGoalStore().Get(userID)
	if stored.Steps[0].Status != goal.StepDone || !strings.Contains(stored.Steps[0].Result, "补记选型证据") || stored.Steps[1].Status != goal.StepPending {
		t.Fatalf("follow-up moved the current step: %+v", stored.Steps)
	}
	omitted := h.toolGoalForUser(userID, map[string]interface{}{
		"action": "step", "status": "progress", "result": "骨架目录已建",
	})
	if !strings.Contains(omitted, "步骤 2 仍在进行") {
		t.Fatalf("omitted index = %q", omitted)
	}
	noStatus := h.toolGoalForUser(userID, map[string]interface{}{
		"action": "step", "index": 2, "result": "还在搭骨架",
	})
	if !strings.Contains(noStatus, "步骤 2 仍在进行") {
		t.Fatalf("omitted status = %q", noStatus)
	}
}

func TestGoalExecutePromptKeepsCurrentStepAheadOfUnderstanding(t *testing.T) {
	store := goal.NewStore("")
	g, err := store.Set("user", "图形版游戏")
	if err != nil {
		t.Fatal(err)
	}
	understanding := strings.Repeat("这是步骤1的长篇选型，不要在本轮重做。", 80)
	detail := strings.Repeat("写目录和主循环。", 50) + "验收：窗口能开"
	if err := store.SavePlan("user", g.GoalID, understanding, []goal.Step{
		{Title: "锁定工具链", Detail: "只写文档"},
		{Title: "搭工程骨架", Detail: detail},
	}, []string{strings.Repeat("验收标准很长。", 40)}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateStep("user", g.GoalID, 1, goal.StepDone, "文档已写"); err != nil {
		t.Fatal(err)
	}
	prompt := buildGoalExecutePrompt(store.Get("user"))
	if strings.Contains(prompt, "这是步骤1的长篇选型") || !strings.Contains(prompt, "验收：窗口能开") {
		t.Fatalf("step detail was cut or the plan essay was copied in: %s", prompt)
	}
	msg := goalContinuationTaskUserMessage(&TaskItem{Description: prompt}, "")
	if !strings.Contains(msg, "index=2") || !strings.Contains(msg, "搭工程骨架") || !strings.Contains(msg, "验收：窗口能开") {
		t.Fatalf("user message lost the current step: %s", msg)
	}
}

func TestGoalContinuationCodingTaskIsThePhaseContract(t *testing.T) {
	store := goal.NewStore("")
	if _, err := store.Set("user", "实现一个图形界面版愤怒的小鸟"); err != nil {
		t.Fatal(err)
	}
	prompt := buildGoalPlanPrompt(store.Get("user"))
	task := &TaskItem{Title: truncateRunesV2(prompt, 80), Description: prompt}
	msg := goalContinuationTaskUserMessage(task, "Continue full coding workbench session")
	if strings.Contains(msg, "请执行以下编码任务") || strings.Contains(msg, "Before editing") {
		t.Fatalf("plan continuation was wrapped as a file edit: %s", msg)
	}
	if !strings.Contains(msg, `goal(action="plan"`) {
		t.Fatalf("plan contract missing from user message: %s", msg)
	}
	sys := buildCodingSubAgentSystemPrompt(task, `C:\game`, "", "", nil)
	if strings.Contains(sys, "最小改动") {
		t.Fatalf("plan phase kept the file-editing system prompt: %s", sys)
	}
	if !strings.Contains(sys, `goal(action="plan"`) {
		t.Fatalf("system prompt missing plan contract: %s", sys)
	}

	var long strings.Builder
	long.WriteString("[系统续接][execute] 按步骤执行目标：游戏\n")
	for i := 0; i < 80; i++ {
		long.WriteString("已完成 " + strings.Repeat("结果说明", 20) + "\n")
	}
	long.WriteString("做完后调用 goal(action=\"step\", index=81, status=\"done\" 或 \"failed\", result=\"...\").\n")
	kept := goalContinuationTaskUserMessage(&TaskItem{Description: long.String()}, "")
	if !strings.Contains(kept, `goal(action="step"`) {
		t.Fatal("truncation dropped the step call")
	}
	if isGoalSystemContinuation("请看这段：[系统续接] 不是宿主阶段") {
		t.Fatal("a quoted marker must not become a host continuation")
	}
	if goalContinuationPhase("请本轮只做规划，先别写代码") != "" {
		t.Fatal("a user sentence must not become a goal plan phase")
	}

	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{exposeGoalTool: true},
		task:     &TaskItem{Description: prompt},
	}
	names := map[string]bool{}
	for _, def := range cb.BuildTools("") {
		names[extractToolName(def)] = true
	}
	if !names["goal"] || !names["read_file"] {
		t.Fatalf("plan surface = %v", names)
	}
	for _, blocked := range []string{"write_file", "edit_file", "bash"} {
		if names[blocked] {
			t.Fatalf("plan surface exposed %s", blocked)
		}
	}
	exec := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{exposeGoalTool: true},
		task:     &TaskItem{Description: "[系统续接][execute] 按步骤执行目标：游戏\n做完后调用 goal(action=\"step\")."},
	}
	execNames := map[string]bool{}
	for _, def := range exec.BuildTools("") {
		execNames[extractToolName(def)] = true
	}
	if !execNames["write_file"] || !execNames["goal"] || execNames[codingAgentTodoToolName] {
		t.Fatalf("execute surface = %v", execNames)
	}
	execPrompt := buildCodingSubAgentSystemPrompt(exec.task, `C:\game`, "", "", nil)
	if strings.Contains(execPrompt, "必须先用 todo_write") {
		t.Fatalf("goal execute prompt required the local checklist: %s", execPrompt)
	}
}

func TestGoalPlanSurvivesUncorrelatedCodingSurface(t *testing.T) {
	prompt := "[系统续接][plan] 先理解并分解目标，不要实现。\n本轮只做规划。\n调用 goal(action=\"plan\", understanding=\"...\", steps=[{\"title\":\"...\"}], acceptance_criteria=[\"...\"])."
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{exposeGoalTool: true},
		task:     &TaskItem{Description: prompt},
	}
	if !cb.usesUncorrelatedStaticCompatibilityModelSurface() {
		t.Fatal("test fixture must be the uncorrelated surface")
	}
	epoch := cb.BeginToolSurfaceEpoch(0)
	names := map[string]bool{}
	for _, def := range cb.BuildToolsForModelRequest(prompt, 0) {
		names[extractToolName(def)] = true
	}
	if !names["goal"] || !names["read_file"] {
		t.Fatalf("uncorrelated plan request dropped goal: %v", names)
	}
	if names["write_file"] || names["edit_file"] || names["bash"] {
		t.Fatalf("uncorrelated plan request exposed a write tool: %v", names)
	}
	sys := cb.BuildSystemPrompt(prompt, true)
	if !strings.Contains(sys, `goal(action="plan"`) || strings.Contains(sys, "Coding compatibility mode") {
		t.Fatalf("uncorrelated plan prompt = %s", sys)
	}
	admitted := cb.ExecuteToolCallWithContext("goal", `{"action":"get"}`, "call-goal", agent.ToolCallExecutionContext{SurfaceEpoch: epoch})
	if strings.Contains(admitted.Result, "static_response_correlation_missing") || strings.Contains(admitted.Result, "static_surface_unavailable") {
		t.Fatalf("goal dispatch was treated as a workspace write: %#v", admitted)
	}

	remote := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{exposeGoalTool: true},
		task:  prompt,
	}
	remoteNames := map[string]bool{}
	for _, def := range remote.BuildToolsForModelRequest(prompt, 0) {
		remoteNames[extractToolName(def)] = true
	}
	if !remoteNames["goal"] || remoteNames["ssh_write_file"] || remoteNames["ssh_bash"] {
		t.Fatalf("uncorrelated remote plan surface = %v", remoteNames)
	}
	remotePrompt := remote.BuildSystemPrompt(prompt, true)
	if !strings.Contains(remotePrompt, `goal(action="plan"`) || strings.Contains(remotePrompt, "Remote Coding compatibility mode") {
		t.Fatalf("uncorrelated remote plan prompt = %s", remotePrompt)
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

func TestSharedLoopGoalUsesTurnOwnerNotLastUserID(t *testing.T) {
	store := goal.NewStore("")
	if _, err := store.Set("desktop-user:C:/game", "做一个游戏"); err != nil {
		t.Fatal(err)
	}
	handler := &IMMessageHandler{goalStore: store, lastUserID: "desktop-user", registry: NewToolRegistry()}
	if err := handler.registry.Register(RegisteredTool{
		Name: "goal",
		Handler: func(map[string]interface{}) string {
			t.Fatal("registry handler must not choose the goal owner")
			return ""
		},
	}); err != nil {
		t.Fatal(err)
	}
	ctx := withMailboxOwner(context.Background(), "desktop-user:C:/game")
	result := handler.executeToolDetailedWithRuntimeContext(ctx, "other-policy", false, "", "goal", `{"action":"get"}`, "", nil)
	if !strings.Contains(result.Text, "做一个游戏") {
		t.Fatalf("goal ran against the wrong owner: %s", result.Text)
	}
	if got := store.Get("desktop-user"); got != nil {
		t.Fatal("goal was attached to the global lastUserID")
	}
}

func TestCodingGoalToolRejectionIsAFailedCall(t *testing.T) {
	handler := &IMMessageHandler{goalStore: goal.NewStore("")}
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{
		handler: handler,
		loopCtx: &LoopContext{UserID: "desktop-user"},
	}}
	rejected := cb.executeGoalTool(map[string]interface{}{
		"action":              "plan",
		"understanding":       "做一个游戏",
		"steps":               []interface{}{"搭窗口"},
		"acceptance_criteria": []interface{}{"能启动"},
	})
	if rejected.Outcome != codingToolOutcomeFailed || !strings.HasPrefix(rejected.Text, "错误:") {
		t.Fatalf("rejected plan was reported as success: %+v", rejected)
	}

	if _, err := handler.goalStore.Set("desktop-user", "做一个游戏"); err != nil {
		t.Fatal(err)
	}
	saved := cb.executeGoalTool(map[string]interface{}{
		"action":              "plan",
		"understanding":       "做一个游戏",
		"steps":               []interface{}{"搭窗口"},
		"acceptance_criteria": []interface{}{"能启动"},
	})
	if saved.Outcome != codingToolOutcomeSuccess || strings.HasPrefix(saved.Text, "错误:") {
		t.Fatalf("saved plan was reported as failure: %+v", saved)
	}
	if got := handler.goalStore.Get("desktop-user"); got == nil || len(got.Steps) != 1 {
		t.Fatalf("plan was not stored: %+v", got)
	}
}

func goalTimerScheduled(t *testing.T, engine *GoalContinuationEngine, userID string) bool {
	t.Helper()
	engine.mu.Lock()
	defer engine.mu.Unlock()
	_, ok := engine.scheduledTimers[userID]
	return ok
}

func TestGoalExecuteTurnMustRecordTheStep(t *testing.T) {
	store := goal.NewStore("")
	g, err := store.Set("user", "实现登录")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlan("user", g.GoalID, "交付登录", []goal.Step{{Title: "检查环境"}, {Title: "编写代码"}}, []string{"可以登录"}); err != nil {
		t.Fatal(err)
	}
	engine := NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending("user") })
	engine.noteContinuationExpect("user", store.Get("user"))
	engine.MaybeScheduleContinuation("user", g.GoalID, 10, 5, true)
	got := store.Get("user")
	if got == nil || got.Status != goal.StatusActive || got.Steps[0].Status == goal.StepDone {
		t.Fatalf("first missing step should stay on that step: %+v", got)
	}
	if !goalTimerScheduled(t, engine, "user") {
		t.Fatal("missing step should schedule the same step")
	}
	engine.mu.Lock()
	if engine.planCorrection["user"] != "" {
		t.Fatalf("missing step scheduled a plan correction: %q", engine.planCorrection["user"])
	}
	engine.mu.Unlock()
	nextPrompt := buildGoalExecutePrompt(got)
	if !strings.HasPrefix(nextPrompt, "[系统续接][execute]") || !strings.Contains(nextPrompt, "检查环境") || strings.Contains(nextPrompt, "编写代码") || strings.Contains(nextPrompt, "只补交记录") || strings.Contains(nextPrompt, "不要修改文件") {
		t.Fatalf("next prompt = %s", nextPrompt)
	}
	nextDisplay := buildContinuationDisplayText(got)
	if !strings.Contains(nextDisplay, "目标清单") || !strings.Contains(nextDisplay, "检查环境") || strings.Contains(nextDisplay, "正在补交") || strings.Contains(nextDisplay, "/goal") {
		t.Fatalf("next display = %q", nextDisplay)
	}
	engine.noteContinuationExpect("user", store.Get("user"))
	engine.MaybeScheduleContinuation("user", g.GoalID, 10, 5, true)
	got = store.Get("user")
	if got == nil || got.Status != goal.StatusActive || got.Steps[0].Status == goal.StepDone || strings.Contains(got.Summary, "未提交当前步骤") {
		t.Fatalf("second missing step should stay on that step: %+v", got)
	}
	if !goalTimerScheduled(t, engine, "user") {
		t.Fatal("second missing step should schedule the same step again")
	}

	userID := "goal-step-cancel"
	g, err = store.Set(userID, "实现登录")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlan(userID, g.GoalID, "交付登录", []goal.Step{{Title: "检查环境"}}, []string{"可以登录"}); err != nil {
		t.Fatal(err)
	}
	engine = NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending(userID) })
	engine.noteContinuationExpect(userID, store.Get(userID))
	engine.MaybeScheduleContinuation(userID, g.GoalID, 10, 5, true)
	engine.CancelPending(userID)
	if goalTimerScheduled(t, engine, userID) {
		t.Fatal("cancel should drop the pending step")
	}
	engine.noteContinuationExpect(userID, store.Get(userID))
	engine.MaybeScheduleContinuation(userID, g.GoalID, 10, 5, true)
	got = store.Get(userID)
	if got == nil || got.Status != goal.StatusActive || !goalTimerScheduled(t, engine, userID) {
		t.Fatalf("cancelled continuation must stay active and schedule again: %+v", got)
	}

	userID = "goal-step-empty"
	g, err = store.Set(userID, "实现登录")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlan(userID, g.GoalID, "交付登录", []goal.Step{{Title: "检查环境"}}, []string{"可以登录"}); err != nil {
		t.Fatal(err)
	}
	engine = NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending(userID) })
	engine.noteContinuationExpect(userID, store.Get(userID))
	engine.MaybeScheduleContinuation(userID, g.GoalID, 10, 5, false)
	engine.noteContinuationExpect(userID, store.Get(userID))
	engine.MaybeScheduleContinuation(userID, g.GoalID, 10, 5, false)
	got = store.Get(userID)
	if got == nil || got.Status != goal.StatusPaused || strings.Contains(got.Summary, "未提交当前步骤") {
		t.Fatalf("two empty turns should pause without failing the step: %+v", got)
	}

	userID = "goal-step-reported"
	g, err = store.Set(userID, "实现登录")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlan(userID, g.GoalID, "交付登录", []goal.Step{{Title: "检查环境"}}, []string{"可以登录"}); err != nil {
		t.Fatal(err)
	}
	engine = NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending(userID) })
	engine.noteContinuationExpect(userID, store.Get(userID))
	if err := store.UpdateStep(userID, g.GoalID, 1, goal.StepDone, "环境已检查"); err != nil {
		t.Fatal(err)
	}
	engine.MaybeScheduleContinuation(userID, g.GoalID, 10, 5, true)
	got = store.Get(userID)
	if got == nil || got.Status != goal.StatusActive || !got.ReadyToComplete() {
		t.Fatalf("reported step should stay active for completion: %+v", got)
	}

	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{},
		task:     &TaskItem{Description: "[系统续接][execute] 按步骤执行目标：登录"},
	}
	status, errMsg := applyUncorrelatedCompatibilitySurfaceOutcome(TaskExecPassed, "", false, cb, codingSubAgentAudit{})
	if status != TaskExecPassed || errMsg != "" {
		t.Fatalf("goal execute on the read-only surface was judged as a missing file change: %s %s", status, errMsg)
	}
}

func TestGoalContinuationRetriesTransientTurnError(t *testing.T) {
	userID := "goal-transient"
	store := goal.NewStore("")
	g, err := store.Set(userID, "持续巡检")
	if err != nil {
		t.Fatal(err)
	}
	engine := NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending(userID) })
	handler := &IMMessageHandler{app: &App{goalContinuation: engine}, goalStore: store}

	const retries = 12
	for i := 0; i < retries; i++ {
		handler.maybeScheduleGoalContinuation(userID, &IMAgentResponse{Error: "context deadline exceeded"}, "goal-continuation")
		got := store.Get(userID)
		if got == nil || got.Status != goal.StatusActive || got.GoalID != g.GoalID || got.TurnsUsed != 0 {
			t.Fatalf("retry %d closed the goal: %+v", i+1, got)
		}
	}
	engine.mu.Lock()
	scheduled := engine.scheduledTimers[userID] != nil
	misses := engine.transientTurnMiss[userID]
	engine.mu.Unlock()
	if !scheduled {
		t.Fatal("transient deadline did not schedule another turn")
	}
	if misses != retries {
		t.Fatalf("transport misses = %d, want %d", misses, retries)
	}

	if goalTurnErrorIsRetryable("编译失败") || goalTurnErrorIsRetryable("cancelled during LLM retry") || goalTurnErrorIsRetryable("unexpected EOF") {
		t.Fatal("task, cancel, and a copied unexpected EOF must not be retried")
	}
	if !goalTurnErrorIsRetryable("SSE stream idle timeout") || !goalTurnErrorIsRetryable("context deadline exceeded") || !goalTurnErrorIsRetryable("dial tcp 10.0.0.1:443: i/o timeout") {
		t.Fatal("idle timeout, a route deadline, and a dial timeout should be retried")
	}
	// The gateway reports this stall inside HTTP 200. The type is gone by the
	// time the goal sees the text, and failing the goal here is what left the
	// task list on 失败 after the stream produced nothing.
	if !goalTurnErrorIsRetryable("upstream stream idle timeout after 2m0s") || !goalTurnErrorIsRetryable("LLM call failed: upstream stream idle timeout after 2m0s") {
		t.Fatal("an upstream stream idle stall should be retried")
	}
	if goalTurnErrorIsRetryable("upstream model error: content policy") {
		t.Fatal("a named model rejection must not be retried")
	}
}

func TestGoalExecutePromptDoesNotShowAZeroTurnCap(t *testing.T) {
	store := goal.NewStore("")
	g, err := store.Set("user", "做游戏")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlan("user", g.GoalID, "交付游戏", []goal.Step{{Title: "骨架"}}, []string{"能玩"}); err != nil {
		t.Fatal(err)
	}
	prompt := buildGoalExecutePrompt(store.Get("user"))
	if strings.Contains(prompt, "/0") || !strings.Contains(prompt, "轮次不限") {
		t.Fatalf("unlimited prompt = %s", prompt)
	}
	capped, err := store.Set("user-capped", "做游戏", goal.WithMaxTurns(4))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlan("user-capped", capped.GoalID, "交付游戏", []goal.Step{{Title: "骨架"}}, []string{"能玩"}); err != nil {
		t.Fatal(err)
	}
	cappedPrompt := buildGoalExecutePrompt(store.Get("user-capped"))
	if !strings.Contains(cappedPrompt, "第 0/4 轮") {
		t.Fatalf("capped prompt = %s", cappedPrompt)
	}
}

func TestLateRetryableTurnErrorDoesNotReplaceSuccessor(t *testing.T) {
	userID := "goal-late-retry"
	store := goal.NewStore("")
	if _, err := store.Set(userID, "做游戏"); err != nil {
		t.Fatal(err)
	}
	engine := NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending(userID) })
	handler := &IMMessageHandler{app: &App{goalContinuation: engine}}

	engine.noteContinuationExpect(userID, store.Get(userID))
	owned := engine.peekContinuationExpect(userID)
	engine.scheduleDelayed(userID, store.Get(userID).GoalID)
	engine.noteContinuationExpect(userID, store.Get(userID))
	next := engine.peekContinuationExpect(userID)
	if next.generation == owned.generation {
		t.Fatal("successor note did not advance")
	}
	engine.mu.Lock()
	gen := engine.scheduleGen[userID]
	engine.mu.Unlock()

	handler.maybeScheduleGoalContinuationOwned(userID, &IMAgentResponse{Error: "context deadline exceeded"}, "goal-continuation", owned, true)
	got := store.Get(userID)
	if got == nil || got.Status != goal.StatusActive || got.TurnsUsed != 0 {
		t.Fatalf("superseded transport error closed the goal: %+v", got)
	}
	if peeked := engine.peekContinuationExpect(userID); peeked != next {
		t.Fatalf("late retry retired the next note: got %+v want %+v", peeked, next)
	}
	engine.mu.Lock()
	gotGen := engine.scheduleGen[userID]
	misses := engine.transientTurnMiss[userID]
	engine.mu.Unlock()
	if gotGen != gen {
		t.Fatalf("late retry replaced the timer: gen %d -> %d", gen, gotGen)
	}
	if misses != 0 {
		t.Fatalf("superseded turn was counted: %d", misses)
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
	engine = NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending(userID) })
	handler = &IMMessageHandler{app: &App{goalContinuation: engine}, goalStore: store}
	handler.maybeScheduleGoalContinuation(userID, &IMAgentResponse{Text: "好的"}, "goal-continuation")
	got = store.Get(userID)
	if got == nil || got.Status != goal.StatusActive || len(got.Steps) != 0 {
		t.Fatalf("first missing plan should schedule a correction: %+v", got)
	}
	engine.mu.Lock()
	correction := engine.planCorrection[userID]
	_, scheduled := engine.scheduledTimers[userID]
	engine.mu.Unlock()
	if !scheduled || correction != got.GoalID {
		t.Fatalf("plan correction scheduled=%v goal=%q", scheduled, correction)
	}
	correctionPrompt := buildGoalPlanCorrectionPrompt(got)
	if !strings.HasPrefix(correctionPrompt, "[系统续接][plan]") || !strings.Contains(correctionPrompt, `goal(action="plan"`) {
		t.Fatalf("correction prompt = %s", correctionPrompt)
	}
	correctionDisplay := buildGoalPlanCorrectionDisplay(got)
	if !strings.Contains(correctionDisplay, "目标清单") || !strings.Contains(correctionDisplay, "正在补交分解：实现登录") || strings.Contains(correctionDisplay, "/goal") {
		t.Fatalf("correction display = %q", correctionDisplay)
	}
	notice := newGoalNoticeEvent(userID, "未能分解任务", true)
	if notice.Kind != "notice" || !notice.AsError || !strings.HasPrefix(notice.RequestID, "goal-notice-") {
		t.Fatalf("failure notice = %+v", notice)
	}

	handler.maybeScheduleGoalContinuation(userID, &IMAgentResponse{Text: "还是没有"}, "goal-continuation")
	got = store.Get(userID)
	if got == nil || got.Status != goal.StatusFailed || !strings.Contains(got.Summary, "未能分解任务") {
		t.Fatalf("second missing plan should fail the goal: %+v", got)
	}
}

func TestUnsentPlanCorrectionIsRefunded(t *testing.T) {
	userID := "goal-plan-cancel"
	store := goal.NewStore("")
	if _, err := store.Set(userID, "实现登录"); err != nil {
		t.Fatal(err)
	}
	engine := NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending(userID) })
	handler := &IMMessageHandler{app: &App{goalContinuation: engine}, goalStore: store}

	handler.maybeScheduleGoalContinuation(userID, &IMAgentResponse{Text: "好的"}, "goal-continuation")
	engine.CancelPending(userID)
	handler.maybeScheduleGoalContinuation(userID, &IMAgentResponse{Text: "仍未分解"}, "goal-continuation")
	got := store.Get(userID)
	if got == nil || got.Status != goal.StatusActive || len(got.Steps) != 0 {
		t.Fatalf("cancelled correction must stay active for one more try: %+v", got)
	}
	engine.mu.Lock()
	correction := engine.planCorrection[userID]
	engine.mu.Unlock()
	if correction != got.GoalID {
		t.Fatalf("correction not rescheduled: %q", correction)
	}

	userID = "goal-plan-paused"
	if _, err := store.Set(userID, "实现登录"); err != nil {
		t.Fatal(err)
	}
	engine = NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending(userID) })
	handler = &IMMessageHandler{app: &App{goalContinuation: engine}, goalStore: store}
	handler.maybeScheduleGoalContinuation(userID, &IMAgentResponse{Text: "好的"}, "goal-continuation")
	got = store.Get(userID)
	if got == nil {
		t.Fatal("missing goal")
	}
	goalID := got.GoalID
	if !store.Pause(userID, goalID) {
		t.Fatal("pause failed")
	}
	engine.mu.Lock()
	if timer, ok := engine.scheduledTimers[userID]; ok {
		timer.Stop()
		delete(engine.scheduledTimers, userID)
	}
	engine.mu.Unlock()
	engine.fireContinuation(userID, goalID)
	got = store.Get(userID)
	if got == nil || got.Status != goal.StatusPaused || strings.Contains(got.Summary, "未能分解任务") {
		t.Fatalf("paused goal must stay paused: %+v", got)
	}
	if !store.Resume(userID, goalID) {
		t.Fatal("resume failed")
	}
	handler.maybeScheduleGoalContinuation(userID, &IMAgentResponse{Text: "恢复后仍未分解"}, "goal-continuation")
	got = store.Get(userID)
	if got == nil || got.Status != goal.StatusActive || len(got.Steps) != 0 {
		t.Fatalf("resumed goal should receive the corrective turn: %+v", got)
	}
	engine.mu.Lock()
	correction = engine.planCorrection[userID]
	engine.mu.Unlock()
	if correction != goalID {
		t.Fatalf("resume correction = %q", correction)
	}

	userID = "goal-plan-reschedule"
	if _, err := store.Set(userID, "实现登录"); err != nil {
		t.Fatal(err)
	}
	engine = NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending(userID) })
	handler = &IMMessageHandler{app: &App{goalContinuation: engine}, goalStore: store}
	handler.maybeScheduleGoalContinuation(userID, &IMAgentResponse{Text: "好的"}, "goal-continuation")
	got = store.Get(userID)
	if got == nil {
		t.Fatal("missing goal")
	}
	goalID = got.GoalID
	engine.scheduleDelayed(userID, goalID)
	engine.mu.Lock()
	_, missLeft := engine.planMiss[userID]
	replaced := engine.planCorrection[userID]
	engine.mu.Unlock()
	if missLeft || replaced != "" {
		t.Fatalf("normal reschedule must refund the unsent correction miss=%v correction=%q", missLeft, replaced)
	}
	handler.maybeScheduleGoalContinuation(userID, &IMAgentResponse{Text: "重排后仍未分解"}, "goal-continuation")
	got = store.Get(userID)
	if got == nil || got.Status != goal.StatusActive || len(got.Steps) != 0 {
		t.Fatalf("rescheduled goal should receive the corrective turn: %+v", got)
	}
	engine.mu.Lock()
	correction = engine.planCorrection[userID]
	engine.mu.Unlock()
	if correction != goalID {
		t.Fatalf("reschedule correction = %q", correction)
	}
}

func TestActiveUnplannedGoalFailsWhenCorrectionCannotRun(t *testing.T) {
	userID := "goal-plan-stuck"
	goalID := "stuck-goal"
	dir := t.TempDir()
	body, err := json.Marshal(goal.Goal{
		GoalID:    goalID,
		UserID:    userID,
		Objective: "实现登录",
		Status:    goal.StatusActive,
		MaxTurns:  1,
		TurnsUsed: 1,
		Phase:     goal.PhasePlan,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, userID+".json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	store := goal.NewStore(dir)
	got := store.Get(userID)
	if got == nil || got.Status != goal.StatusActive || got.ShouldContinue() || len(got.Steps) != 0 {
		t.Fatalf("fixture = %+v", got)
	}
	engine := NewGoalContinuationEngine(store, nil)
	engine.planCorrection[userID] = goalID
	engine.planMiss[userID] = goalPlanMiss{goalID: goalID, retried: true}
	engine.fireContinuation(userID, goalID)
	got = store.Get(userID)
	if got == nil || got.Status != goal.StatusFailed || !strings.Contains(got.Summary, "未能分解任务") {
		t.Fatalf("stuck active goal = %+v", got)
	}
}

func TestGoalContinuationSharedLoopKeepsLifecycleGoal(t *testing.T) {
	h := &IMMessageHandler{
		app:               &App{},
		unifiedClassifier: semanticClassifierForLabel(t, intent.LabelGoalManage),
		toolDefGen: NewToolDefinitionGenerator(nil, []map[string]interface{}{
			toolDef("goal", "lifecycle goal", nil, nil),
			toolDef("read_file", "read", nil, nil),
			toolDef("write_file", "write", nil, nil),
		}),
	}
	text := "[系统续接][plan] 先理解并分解目标，不要实现。\n\n目标：做一个游戏\n\n调用 goal(action=\"plan\")."
	ctx := &LoopContext{Platform: "goal-continuation"}
	ctx.Runtime.Execution = fullExecutionProfile("goal continuation")
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndAttachments(ctx, "desktop-user", text, "goal-continuation", nil)
	if err != nil || handled || surface != nil || len(defs) != 0 {
		t.Fatalf("managed surface took the continuation: handled=%v err=%v defs=%d", handled, err, len(defs))
	}
	if ctx.Runtime.SemanticIntent != nil {
		t.Fatalf("continuation was classified: %+v", ctx.Runtime.SemanticIntent)
	}
	state := h.prepareAgentLoopStartState(agentLoopStartOptions{
		Context:      ctx,
		UserID:       "desktop-user",
		UserText:     text,
		SystemPrompt: "你是聊天助手",
		Platform:     "goal-continuation",
	})
	if state.Cleanup != nil {
		t.Cleanup(state.Cleanup)
	}
	if state.SemanticSurface != nil {
		t.Fatal("start state kept a semantic surface")
	}
	if ctx.Runtime.RoutingMissFallback || ctx.Runtime.SemanticIntent != nil {
		t.Fatalf("continuation was recorded as a routing miss: intent=%+v miss=%v", ctx.Runtime.SemanticIntent, ctx.Runtime.RoutingMissFallback)
	}
	if !containsAgentLoopToolNamed(state.Tools, "goal") {
		t.Fatalf("lifecycle goal missing: %v", agentLoopToolNamesForLog(state.Tools))
	}
	if containsAgentLoopToolNamed(state.Tools, semanticTrustedGoalAdapter) {
		t.Fatalf("closed goal adapter was published: %v", agentLoopToolNamesForLog(state.Tools))
	}
	for _, blocked := range []string{"write_file", "bash"} {
		if containsAgentLoopToolNamed(state.Tools, blocked) {
			t.Fatalf("plan surface kept %s: %v", blocked, agentLoopToolNamesForLog(state.Tools))
		}
	}
}

func TestGoalExecuteSkipsRemoteFileAudit(t *testing.T) {
	cb := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{},
		task:  "[系统续接][execute] 按步骤执行目标：登录",
	}
	result := cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status: "success", Summary: "步骤已提交", ToolCalls: 1,
	})
	if result == nil || result.Status != "success" || result.Error != "" || result.QualityStatus != codingSubAgentQualityNotNeeded {
		t.Fatalf("execute continuation was file-audited: %#v", result)
	}
}

func TestGoalContinuationCodingHostErrorClosesTheGoal(t *testing.T) {
	userText := "[系统续接][execute] 按步骤执行目标"
	hostErr := desktopCodingVerifiedSurfaceUnavailableDiagnostic
	got := goalContinuationCodingHostError(userText, false, []v2.TaskRunResult{{
		Status: v2.TaskFailed,
		Error:  hostErr,
	}})
	if got != hostErr {
		t.Fatalf("host error = %q", got)
	}
	if goalContinuationCodingHostError(userText, true, []v2.TaskRunResult{{Status: v2.TaskFailed, Error: hostErr}}) != "" {
		t.Fatal("a cancelled turn must not become a goal failure")
	}
	if goalContinuationCodingHostError("实现登录", false, []v2.TaskRunResult{{Status: v2.TaskFailed, Error: hostErr}}) != "" {
		t.Fatal("an ordinary coding failure is not a goal host error")
	}
	start := goalContinuationFailedStart(userText, &IMAgentResponse{Text: "远程编程无法启动：缺少 SSH 会话。"})
	if start == nil || start.Error == "" {
		t.Fatalf("start refusal = %+v", start)
	}

	userID := "goal-host-start"
	store := goal.NewStore("")
	if _, err := store.Set(userID, "实现登录"); err != nil {
		t.Fatal(err)
	}
	engine := NewGoalContinuationEngine(store, nil)
	engine.noteContinuationExpect(userID, store.Get(userID))
	handler := &IMMessageHandler{app: &App{goalContinuation: engine}, goalStore: store}
	handler.maybeScheduleGoalContinuation(userID, &IMAgentResponse{Text: "任务运行错误：" + hostErr, Error: hostErr}, "goal-continuation")
	stored := store.Get(userID)
	if stored == nil || stored.Status != goal.StatusFailed || !strings.Contains(stored.Summary, "桌面执行凭证") {
		t.Fatalf("goal = %+v", stored)
	}
}

func TestGoalContinuationKeepsWorkingWhenTheIterationBudgetIsSpent(t *testing.T) {
	userText := "[系统续接][execute] 按步骤执行目标"
	if got := goalContinuationCodingHostError(userText, false, []v2.TaskRunResult{{
		Status: v2.TaskFailed,
		Error:  "max iterations reached",
	}}); got != "" {
		t.Fatalf("iteration budget = %q, want the goal to stay open", got)
	}
	if got := goalContinuationCodingHostError(userText, false, []v2.TaskRunResult{{
		Status: v2.TaskFailed,
		Error:  "max iterations reached after the final check",
	}}); got != "" {
		t.Fatalf("late iteration budget = %q, want the goal to stay open", got)
	}

	userID := "goal-iteration-handoff"
	store := goal.NewStore("")
	g, err := store.Set(userID, "龙芯文本优化", goal.WithMaxTurns(8))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlan(userID, g.GoalID, "先测基线", []goal.Step{{Title: "测量基线"}}, []string{"有基线数字"}); err != nil {
		t.Fatal(err)
	}
	engine := NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending(userID) })
	engine.noteContinuationExpect(userID, store.Get(userID))
	owned := engine.peekContinuationExpect(userID)
	handler := &IMMessageHandler{app: &App{goalContinuation: engine}, goalStore: store}
	handler.maybeScheduleGoalContinuationOwned(userID, &IMAgentResponse{
		Text:            "第 1 步完成。",
		TraceEventCount: 4,
	}, "goal-continuation", owned, true)
	stored := store.Get(userID)
	if stored == nil || stored.Status != goal.StatusActive {
		t.Fatalf("goal = %+v", stored)
	}
	if !goalTimerScheduled(t, engine, userID) {
		t.Fatal("a spent iteration budget must schedule the same step again")
	}

	cb := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{},
		task:  userText,
	}
	remote := cb.applyRemoteVerificationOutcome(&RemoteCodingSubAgentResult{
		Status: "failed", Error: "max iterations reached", Summary: "第 1 步完成。", ToolCalls: 4,
	})
	if remote == nil || remote.Status != "success" || remote.Error != "" {
		t.Fatalf("remote goal turn = %#v", remote)
	}
}

func TestGoalContinuationKeepsRecordedPlanWhenTurnErrors(t *testing.T) {
	userID := "goal-plan-kept"
	store := goal.NewStore("")
	g, err := store.Set(userID, "实现登录")
	if err != nil {
		t.Fatal(err)
	}
	engine := NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending(userID) })
	engine.noteContinuationExpect(userID, g)
	if err := store.SavePlan(userID, g.GoalID, "交付登录", []goal.Step{{Title: "检查环境"}}, []string{"可以登录"}); err != nil {
		t.Fatal(err)
	}
	handler := &IMMessageHandler{app: &App{goalContinuation: engine}, goalStore: store}
	handler.maybeScheduleGoalContinuation(userID, &IMAgentResponse{Error: "编译失败"}, "goal-continuation")
	got := store.Get(userID)
	if got == nil || got.Status != goal.StatusActive || len(got.Steps) != 1 {
		t.Fatalf("recorded plan must survive a later turn error: %+v", got)
	}
}

func TestGoalPlanPhaseRejectsMutationAuthority(t *testing.T) {
	plan := &sharedAgentLoopCallbacks{handler: &IMMessageHandler{}, userText: "[系统续接][plan] 本轮只做规划"}
	if plan.IsToolAllowed("write_file") || plan.IsToolAllowed("bash") {
		t.Fatal("plan phase authorized a mutation tool")
	}
	if !plan.IsToolAllowed("read_file") || !plan.IsToolAllowed("goal") {
		t.Fatal("plan phase blocked a contract tool")
	}
	if ok, _ := plan.PetitionToolCall("write_file"); ok {
		t.Fatal("plan phase granted write_file")
	}
	execute := &sharedAgentLoopCallbacks{handler: &IMMessageHandler{}, userText: "[系统续接][execute] 当前步骤"}
	if !execute.IsToolAllowed("write_file") {
		t.Fatal("execute phase blocked write_file")
	}
}

func TestGoalPlanPhaseIsOnTheOpeningSystemPrompt(t *testing.T) {
	h := &IMMessageHandler{app: &App{}}
	ctx := &LoopContext{Platform: "goal-continuation"}
	ctx.Runtime.Execution = fullExecutionProfile("goal continuation")
	state := h.prepareAgentLoopStartState(agentLoopStartOptions{
		Context:      ctx,
		UserID:       "desktop-user",
		UserText:     "[系统续接][plan] 本轮只做规划",
		SystemPrompt: "你是聊天助手",
		Platform:     "goal-continuation",
	})
	if state.Cleanup != nil {
		t.Cleanup(state.Cleanup)
	}
	if !strings.HasPrefix(state.SystemPrompt, "## 宿主目标阶段") || !strings.Contains(state.SystemPrompt, `goal(action="plan"`) || !strings.Contains(state.SystemPrompt, "你是聊天助手") {
		t.Fatalf("opening prompt = %q", state.SystemPrompt)
	}
}

func TestSharedLoopGoalPhaseOutranksChatPrompt(t *testing.T) {
	plan := (&sharedAgentLoopCallbacks{userText: "[系统续接][plan] 本轮只做规划"}).systemPromptWithTaskAnchor("你是聊天助手，请直接回答。")
	if !strings.HasPrefix(plan, "## 宿主目标阶段") || !strings.Contains(plan, `goal(action="plan"`) || !strings.Contains(plan, "你是聊天助手") {
		t.Fatalf("plan prompt = %q", plan)
	}
	execute := (&sharedAgentLoopCallbacks{userText: "[系统续接][execute] 当前步骤"}).systemPromptWithTaskAnchor("chat")
	if !strings.Contains(execute, `goal(action="step"`) {
		t.Fatalf("execute prompt = %q", execute)
	}
	plain := (&sharedAgentLoopCallbacks{userText: "请本轮只做规划，先别写代码"}).systemPromptWithTaskAnchor("chat")
	if strings.Contains(plain, "宿主目标阶段") {
		t.Fatalf("user wording became a host phase: %q", plain)
	}
}

func TestStaleGoalContinuationTimerDoesNotReplaceTheSuccessor(t *testing.T) {
	store := goal.NewStore("")
	engine := NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour

	userID := "goal-stale-correction"
	g, err := store.Set(userID, "实现登录")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.CancelPending(userID) })
	engine.schedulePlanCorrection(userID, g.GoalID)
	engine.mu.Lock()
	stale := engine.scheduleGen[userID]
	engine.mu.Unlock()
	engine.schedulePlanCorrection(userID, g.GoalID)
	engine.runScheduledContinuation(userID, g.GoalID, stale)
	got := store.Get(userID)
	if got == nil || got.Status != goal.StatusActive {
		t.Fatalf("stale correction timer fired: %+v", got)
	}
	engine.mu.Lock()
	_, stillScheduled := engine.scheduledTimers[userID]
	correction := engine.planCorrection[userID]
	live := engine.scheduleGen[userID]
	engine.mu.Unlock()
	if !stillScheduled || correction != g.GoalID || live == stale {
		t.Fatalf("successor lost scheduled=%v correction=%q gen=%d stale=%d", stillScheduled, correction, live, stale)
	}

	userID = "goal-stale-reschedule"
	g, err = store.Set(userID, "实现登录")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.CancelPending(userID) })
	engine.schedulePlanCorrection(userID, g.GoalID)
	engine.mu.Lock()
	stale = engine.scheduleGen[userID]
	engine.mu.Unlock()
	engine.scheduleDelayed(userID, g.GoalID)
	engine.runScheduledContinuation(userID, g.GoalID, stale)
	got = store.Get(userID)
	if got == nil || got.Status != goal.StatusActive {
		t.Fatalf("stale reschedule timer fired: %+v", got)
	}
	engine.mu.Lock()
	_, stillScheduled = engine.scheduledTimers[userID]
	replaced := engine.planCorrection[userID]
	engine.mu.Unlock()
	if !stillScheduled || replaced != "" {
		t.Fatalf("normal successor lost scheduled=%v correction=%q", stillScheduled, replaced)
	}
}

func TestGoalSlashKeepsTheQueuedTurn(t *testing.T) {
	userID := "goal-slash-keeps-queue"
	store := goal.NewStore("")
	g, err := store.Set(userID, "以现有性能为基础，提升20%")
	if err != nil {
		t.Fatal(err)
	}
	engine := NewGoalContinuationEngine(store, &App{})
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending(userID) })
	handler := &IMMessageHandler{app: &App{goalContinuation: engine}, goalStore: store}
	engine.schedulePlanCorrection(userID, g.GoalID)

	msg := IMUserMessage{UserID: userID, Platform: "desktop"}
	handler.noteInboundUserMessageForGoal(msg, "/goal 优化indra模型")
	resp := handler.handleGoalCommand(msg, "/goal 优化indra模型")
	if resp == nil || !strings.Contains(resp.Error, "已有活跃目标") {
		t.Fatalf("create = %+v", resp)
	}
	handler.noteInboundUserMessageForGoal(IMUserMessage{UserID: userID, Platform: "goal-continuation"}, "下一轮")
	handler.noteInboundUserMessageForGoal(msg, "／goal status")
	resumed := handler.handleGoalCommand(msg, "/goal resume")
	if resumed == nil || !strings.Contains(resumed.Text, "目标不在暂停状态") {
		t.Fatalf("resume = %+v", resumed)
	}
	engine.mu.Lock()
	_, scheduled := engine.scheduledTimers[userID]
	correction := engine.planCorrection[userID]
	engine.mu.Unlock()
	if !scheduled || correction != g.GoalID {
		t.Fatalf("slash dropped the queued plan correction scheduled=%v correction=%q", scheduled, correction)
	}
	if stored := store.Get(userID); stored == nil || stored.Status != goal.StatusActive {
		t.Fatalf("status = %+v", stored)
	}

	handler.preemptQueuedGoalTurn(IMUserMessage{UserID: userID, Platform: "goal-continuation"})
	if resp, handled := handler.handleImmediateIMCommandWithLoop(msg, "/help", nil, nil, nil); !handled || resp == nil || strings.TrimSpace(resp.Text) == "" {
		t.Fatalf("help = %+v handled=%v", resp, handled)
	}
	if resp, handled := handler.handleImmediateIMCommandWithLoop(msg, "你好", nil, nil, nil); !handled || resp == nil {
		t.Fatalf("greeting = %+v handled=%v", resp, handled)
	}
	if resp, handled := handler.handleImmediateIMCommandWithLoop(msg, "谢谢", nil, nil, nil); !handled || resp == nil {
		t.Fatalf("thanks = %+v handled=%v", resp, handled)
	}
	engine.mu.Lock()
	_, scheduled = engine.scheduledTimers[userID]
	correction = engine.planCorrection[userID]
	engine.mu.Unlock()
	if !scheduled || correction != g.GoalID {
		t.Fatalf("immediate reply dropped the queued plan correction scheduled=%v correction=%q", scheduled, correction)
	}

	// A reply after the immediate gates, such as a cache hit or an LLM
	// configuration error, parks the turn and puts the same correction back.
	endHold := handler.beginQueuedGoalTurnHold(msg)
	endHold(false)
	handler.beginQueuedGoalTurnHold(IMUserMessage{UserID: userID, Platform: "goal-continuation"})(false)
	engine.mu.Lock()
	_, scheduled = engine.scheduledTimers[userID]
	correction = engine.planCorrection[userID]
	engine.mu.Unlock()
	if !scheduled || correction != g.GoalID {
		t.Fatalf("short session reply dropped the queued plan correction scheduled=%v correction=%q", scheduled, correction)
	}

	handler.noteInboundUserMessageForGoal(msg, "/goal pause")
	if text := handler.goalPauseForUser(userID); !strings.Contains(text, "目标已暂停") {
		t.Fatalf("pause = %q", text)
	}
	engine.mu.Lock()
	_, scheduled = engine.scheduledTimers[userID]
	correction = engine.planCorrection[userID]
	engine.mu.Unlock()
	if scheduled || correction != "" {
		t.Fatalf("pause left the queue scheduled=%v correction=%q", scheduled, correction)
	}

	chatID := "goal-slash-chat-preempts"
	g, err = store.Set(chatID, "另一个目标")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.CancelPending(chatID) })
	engine.scheduleDelayed(chatID, g.GoalID)
	chatMsg := IMUserMessage{UserID: chatID, Platform: "desktop"}
	handler.noteInboundUserMessageForGoal(chatMsg, "继续做")
	if _, handled := handler.handleImmediateIMCommandWithLoop(chatMsg, "继续做", nil, nil, nil); handled {
		t.Fatal("chat was consumed as an immediate command")
	}
	engine.mu.Lock()
	_, scheduled = engine.scheduledTimers[chatID]
	engine.mu.Unlock()
	if !scheduled {
		t.Fatal("ingress of a session message dropped the queued turn before it took the session")
	}
	endChat := handler.beginQueuedGoalTurnHold(chatMsg)
	endChat(true)
	endChat(false)
	engine.mu.Lock()
	_, scheduled = engine.scheduledTimers[chatID]
	engine.mu.Unlock()
	if scheduled {
		t.Fatal("chat message left the queued turn in place")
	}
}

func TestQueuedGoalTurnHoldRestoresUntilTheLoopKeepsIt(t *testing.T) {
	userID := "goal-hold-restores"
	store := goal.NewStore("")
	g, err := store.Set(userID, "以现有性能为基础，提升20%")
	if err != nil {
		t.Fatal(err)
	}
	engine := NewGoalContinuationEngine(store, &App{})
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending(userID) })
	handler := &IMMessageHandler{app: &App{goalContinuation: engine}, goalStore: store}
	if engine.consumePlanMiss(userID, g.GoalID) {
		t.Fatal("first plan miss was already spent")
	}
	engine.schedulePlanCorrection(userID, g.GoalID)
	msg := IMUserMessage{UserID: userID, Platform: "desktop"}

	end := handler.beginQueuedGoalTurnHold(msg)
	engine.mu.Lock()
	_, parked := engine.scheduledTimers[userID]
	engine.mu.Unlock()
	if parked {
		t.Fatal("hold left the timer live")
	}
	end(false)
	engine.mu.Lock()
	_, scheduled := engine.scheduledTimers[userID]
	correction := engine.planCorrection[userID]
	miss := engine.planMiss[userID]
	engine.mu.Unlock()
	if !scheduled || correction != g.GoalID || !miss.retried || miss.goalID != g.GoalID {
		t.Fatalf("short reply lost the plan correction scheduled=%v correction=%q miss=%+v", scheduled, correction, miss)
	}

	end = handler.beginQueuedGoalTurnHold(msg)
	end(true)
	engine.mu.Lock()
	_, scheduled = engine.scheduledTimers[userID]
	correction = engine.planCorrection[userID]
	_, missSet := engine.planMiss[userID]
	engine.mu.Unlock()
	if scheduled || correction != "" || missSet {
		t.Fatalf("loop kept a parked turn scheduled=%v correction=%q miss=%v", scheduled, correction, missSet)
	}
}

func TestPostLoopKeepsTheQueuedGoalHold(t *testing.T) {
	keep := false
	loopCtx := &LoopContext{}
	loopCtx.queuedGoalTurnHold = &keep
	handler := &IMMessageHandler{}
	handler.schedulePostLoopSideEffects(IMUserMessage{UserID: "goal-post-loop-hold", Platform: "desktop"}, loopCtx, &IMAgentResponse{Text: "好的"}, false)
	if !keep {
		t.Fatal("post-loop schedule left the parked goal turn to be restored")
	}
}

func TestPreparedEntryReturnRestoresTheQueuedGoal(t *testing.T) {
	userID := "goal-entry-restores"
	store := goal.NewStore("")
	g, err := store.Set(userID, "以现有性能为基础，提升20%")
	if err != nil {
		t.Fatal(err)
	}
	engine := NewGoalContinuationEngine(store, &App{})
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending(userID) })
	handler := &IMMessageHandler{app: &App{goalContinuation: engine}, goalStore: store}
	if engine.consumePlanMiss(userID, g.GoalID) {
		t.Fatal("first plan miss was already spent")
	}
	engine.schedulePlanCorrection(userID, g.GoalID)
	msg := IMUserMessage{UserID: userID, Platform: "desktop", Text: "发到微信"}

	end := handler.beginQueuedGoalTurnHold(msg)
	keep := false
	resp := handler.executePreparedIMEntry(preparedIMEntryExecutionOptions{
		Message:            msg,
		Trimmed:            msg.Text,
		queuedGoalTurnHold: &keep,
	})
	if keep {
		t.Fatal("a return before post-loop kept the parked goal turn")
	}
	if resp == nil || resp.ResponseSource != "semantic_host_reject" {
		t.Fatalf("resp=%#v", resp)
	}
	end(keep)
	engine.mu.Lock()
	_, scheduled := engine.scheduledTimers[userID]
	correction := engine.planCorrection[userID]
	miss := engine.planMiss[userID]
	engine.mu.Unlock()
	got := store.Get(userID)
	if got == nil || got.Status != goal.StatusActive || !scheduled || correction != g.GoalID || !miss.retried || miss.goalID != g.GoalID {
		t.Fatalf("early return lost the plan correction status=%v scheduled=%v correction=%q miss=%+v", got, scheduled, correction, miss)
	}
}

func TestUnarmedRemoteReturnPausesTheQueuedGoal(t *testing.T) {
	userID := "goal-unarmed-hold"
	store := goal.NewStore("")
	g, err := store.Set(userID, "以现有性能为基础，提升20%")
	if err != nil {
		t.Fatal(err)
	}
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{Response: "DESKTOP_LOOP_RAN"})
	engine := NewGoalContinuationEngine(store, handler.app)
	engine.cooldown = time.Hour
	t.Cleanup(func() {
		engine.CancelPending(userID)
		handler.clearStickyCodingEnvironment(userID)
	})
	handler.app.goalContinuation = engine
	handler.goalStore = store
	handler.clearStickyCodingEnvironment(userID)
	handler.storeStickyCodingWorkbenchMemory(userID, stickyCodingWorkbenchMemory{
		Kind:          "remote",
		RemoteHost:    "home.rapidaltech",
		RemoteUser:    "ra",
		RemoteWorkDir: "/home/ra/clamav",
	})
	engine.schedulePlanCorrection(userID, g.GoalID)
	msg := IMUserMessage{UserID: userID, Platform: "desktop", Text: "继续优化"}

	end := handler.beginQueuedGoalTurnHold(msg)
	keep := false
	resp := handler.executePreparedIMEntry(preparedIMEntryExecutionOptions{
		Message:            msg,
		Trimmed:            msg.Text,
		FreshTask:          true,
		queuedGoalTurnHold: &keep,
	})
	if keep {
		t.Fatal("unarmed remote return kept the parked goal turn")
	}
	if resp == nil || resp.ResponseSource != "remote_coding_reconnect" || strings.Contains(resp.Text, "DESKTOP_LOOP_RAN") {
		t.Fatalf("resp=%#v", resp)
	}
	got := store.Get(userID)
	if got == nil || got.Status != goal.StatusPaused {
		t.Fatalf("unarmed return left status=%v", got)
	}
	end(keep)
	engine.mu.Lock()
	_, scheduled := engine.scheduledTimers[userID]
	correction := engine.planCorrection[userID]
	engine.mu.Unlock()
	if scheduled || correction != "" {
		t.Fatalf("paused remote goal was re-armed scheduled=%v correction=%q", scheduled, correction)
	}
}

func TestSideCommandRestoresTheQueuedGoalTurn(t *testing.T) {
	userID := "goal-side-restores-queue"
	store := goal.NewStore("")
	g, err := store.Set(userID, "以现有性能为基础，提升20%")
	if err != nil {
		t.Fatal(err)
	}
	engine := NewGoalContinuationEngine(store, &App{})
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending(userID) })
	handler := &IMMessageHandler{app: &App{goalContinuation: engine}, goalStore: store}
	if engine.consumePlanMiss(userID, g.GoalID) {
		t.Fatal("first plan miss was already spent")
	}
	engine.schedulePlanCorrection(userID, g.GoalID)

	resume := handler.suspendQueuedGoalTurn(userID)
	engine.mu.Lock()
	_, parked := engine.scheduledTimers[userID]
	parkedCorrection := engine.planCorrection[userID]
	_, parkedMiss := engine.planMiss[userID]
	engine.mu.Unlock()
	if parked || parkedCorrection != "" || parkedMiss {
		t.Fatalf("side command left the timer live scheduled=%v correction=%q miss=%v", parked, parkedCorrection, parkedMiss)
	}
	resume()
	engine.mu.Lock()
	_, scheduled := engine.scheduledTimers[userID]
	correction := engine.planCorrection[userID]
	miss := engine.planMiss[userID]
	engine.mu.Unlock()
	if !scheduled || correction != g.GoalID || !miss.retried || miss.goalID != g.GoalID {
		t.Fatalf("restore lost the plan correction scheduled=%v correction=%q miss=%+v", scheduled, correction, miss)
	}

	engine.mu.Lock()
	timer := engine.scheduledTimers[userID]
	gen := engine.scheduleGen[userID]
	engine.mu.Unlock()
	if timer != nil {
		timer.Stop()
	}
	kind, claimed := engine.claimScheduledContinuation(userID, g.GoalID, gen)
	if !claimed || kind != goalCorrectionPlan {
		t.Fatalf("claim = %v kind=%v", claimed, kind)
	}
	engine.mu.Lock()
	genAfterClaim := engine.scheduleGen[userID]
	engine.mu.Unlock()
	noop := handler.suspendQueuedGoalTurn(userID)
	noop()
	engine.mu.Lock()
	_, scheduled = engine.scheduledTimers[userID]
	genAfterEmpty := engine.scheduleGen[userID]
	engine.mu.Unlock()
	if scheduled || genAfterEmpty != genAfterClaim {
		t.Fatal("a claimed turn was parked again")
	}
	if !engine.consumePlanMiss(userID, g.GoalID) {
		t.Fatal("restored plan miss was no longer spent")
	}
	engine.schedulePlanCorrection(userID, g.GoalID)
	resume = handler.suspendQueuedGoalTurn(userID)
	if !store.Pause(userID, g.GoalID) {
		t.Fatal("pause")
	}
	resume()
	engine.mu.Lock()
	_, scheduled = engine.scheduledTimers[userID]
	correction = engine.planCorrection[userID]
	engine.mu.Unlock()
	got := store.Get(userID)
	if got == nil || got.Status != goal.StatusPaused || scheduled || correction != "" {
		t.Fatalf("paused goal was re-armed status=%v scheduled=%v correction=%q", got, scheduled, correction)
	}

	if !store.Resume(userID, g.GoalID) {
		t.Fatal("resume store")
	}
	engine.scheduleDelayed(userID, g.GoalID)
	resume = handler.suspendQueuedGoalTurn(userID)
	engine.scheduleDelayed(userID, g.GoalID)
	engine.mu.Lock()
	newerGen := engine.scheduleGen[userID]
	engine.mu.Unlock()
	resume()
	engine.mu.Lock()
	gotGen := engine.scheduleGen[userID]
	_, scheduled = engine.scheduledTimers[userID]
	correction = engine.planCorrection[userID]
	engine.mu.Unlock()
	if !scheduled || gotGen != newerGen || correction != "" {
		t.Fatalf("restore replaced the newer timer gen=%d/%d scheduled=%v correction=%q", newerGen, gotGen, scheduled, correction)
	}
}

func TestGoalPauseStopsTheQueuedTurnAndResumeRestartsIt(t *testing.T) {
	userID := "goal-pause-queued"
	store := goal.NewStore("")
	if _, err := store.Set(userID, "实现登录"); err != nil {
		t.Fatal(err)
	}
	engine := NewGoalContinuationEngine(store, &App{})
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending(userID) })
	handler := &IMMessageHandler{app: &App{goalContinuation: engine}, goalStore: store}
	handler.maybeScheduleGoalContinuation(userID, &IMAgentResponse{Text: "好的"}, "goal-continuation")
	got := store.Get(userID)
	if got == nil {
		t.Fatal("missing goal")
	}
	engine.mu.Lock()
	stale := engine.scheduleGen[userID]
	engine.mu.Unlock()

	if !strings.Contains(handler.goalPauseForUser(userID), "目标已暂停") {
		t.Fatal("pause text")
	}
	got = store.Get(userID)
	engine.mu.Lock()
	_, scheduled := engine.scheduledTimers[userID]
	_, miss := engine.planMiss[userID]
	correction := engine.planCorrection[userID]
	engine.mu.Unlock()
	if got == nil || got.Status != goal.StatusPaused || scheduled || miss || correction != "" {
		t.Fatalf("pause left a queued turn: status=%v scheduled=%v miss=%v correction=%q", got, scheduled, miss, correction)
	}
	engine.runScheduledContinuation(userID, got.GoalID, stale)
	if again := store.Get(userID); again == nil || again.Status != goal.StatusPaused {
		t.Fatalf("paused timer fired: %+v", again)
	}

	resp := handler.handleGoalCommand(IMUserMessage{UserID: userID}, "/goal resume")
	if resp == nil || !strings.Contains(resp.Text, "目标已恢复") {
		t.Fatalf("resume = %+v", resp)
	}
	got = store.Get(userID)
	engine.mu.Lock()
	_, scheduled = engine.scheduledTimers[userID]
	engine.mu.Unlock()
	if got == nil || got.Status != goal.StatusActive || !scheduled {
		t.Fatalf("resume should arm one continuation: %+v scheduled=%v", got, scheduled)
	}

	activeID := "goal-resume-while-active"
	if _, err := store.Set(activeID, "实现登录"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.CancelPending(activeID) })
	handler.maybeScheduleGoalContinuation(activeID, &IMAgentResponse{Text: "好的"}, "goal-continuation")
	active := store.Get(activeID)
	engine.mu.Lock()
	gen := engine.scheduleGen[activeID]
	wantCorrection := engine.planCorrection[activeID]
	engine.mu.Unlock()
	resp = handler.handleGoalCommand(IMUserMessage{UserID: activeID}, "/goal resume")
	if resp == nil || !strings.Contains(resp.Text, "无法恢复") {
		t.Fatalf("active resume = %+v", resp)
	}
	engine.mu.Lock()
	_, stillScheduled := engine.scheduledTimers[activeID]
	gotGen := engine.scheduleGen[activeID]
	gotCorrection := engine.planCorrection[activeID]
	engine.mu.Unlock()
	if active == nil || !stillScheduled || gotGen != gen || gotCorrection != active.GoalID || gotCorrection != wantCorrection {
		t.Fatalf("resume on an active goal replaced the queued correction gen=%d/%d correction=%q", gen, gotGen, gotCorrection)
	}
}

func TestGoalResumeKeepsPausedWhenRemoteSSHIsDown(t *testing.T) {
	userID := "goal-resume-remote"
	store := goal.NewStore("")
	if _, err := store.Set(userID, "实现登录"); err != nil {
		t.Fatal(err)
	}
	if !store.Pause(userID, store.Get(userID).GoalID) {
		t.Fatal("pause failed")
	}
	engine := NewGoalContinuationEngine(store, &App{})
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending(userID) })
	handler := &IMMessageHandler{app: &App{goalContinuation: engine}, goalStore: store}
	handler.storeStickyCodingWorkbenchMemory(userID, stickyCodingWorkbenchMemory{
		Kind:          "remote",
		RemoteHost:    "home.example",
		RemoteWorkDir: "/tmp/work",
	})
	handler.pendingV2SubAgentExecution.Store(userID, true)
	handler.pendingTemplateCodingProjectPath.Store(userID, `D:\workprj\clamav`)
	t.Cleanup(func() {
		handler.pendingV2SubAgentExecution.Delete(userID)
		handler.pendingTemplateCodingProjectPath.Delete(userID)
		handler.clearStickyCodingWorkbenchMemory(userID)
	})

	resp := handler.handleGoalCommand(IMUserMessage{UserID: userID}, "/goal resume")
	got := store.Get(userID)
	engine.mu.Lock()
	_, scheduled := engine.scheduledTimers[userID]
	engine.mu.Unlock()
	if resp == nil || !strings.Contains(resp.Text, "目标仍处于暂停") || !strings.Contains(resp.Text, "SSH") || got == nil || got.Status != goal.StatusPaused || !strings.Contains(got.Summary, "SSH") || scheduled {
		t.Fatalf("remote resume = %+v goal=%+v scheduled=%v", resp, got, scheduled)
	}

	handler.clearStickyCodingWorkbenchMemory(userID)
	resp = handler.handleGoalCommand(IMUserMessage{UserID: userID}, "/goal resume")
	got = store.Get(userID)
	engine.mu.Lock()
	_, scheduled = engine.scheduledTimers[userID]
	engine.mu.Unlock()
	if resp == nil || !strings.Contains(resp.Text, "目标已恢复") || got == nil || got.Status != goal.StatusActive || !scheduled {
		t.Fatalf("resume after reconnect = %+v goal=%+v scheduled=%v", resp, got, scheduled)
	}
}

func TestRestartResumesPersistedActiveGoal(t *testing.T) {
	dir := t.TempDir()
	disk := goal.NewStore(filepath.Join(dir, "goals"))
	localID := stickyTestUserID(t)
	if _, err := disk.Set(localID, "继续优化向量指令"); err != nil {
		t.Fatal(err)
	}
	pausedID := "restart-goal-paused"
	paused, err := disk.Set(pausedID, "先停着")
	if err != nil || !disk.Pause(pausedID, paused.GoalID) {
		t.Fatalf("pause setup: %v", err)
	}
	doneID := "restart-goal-done"
	done, err := disk.Set(doneID, "已经做完")
	if err != nil || !disk.UpdateStatus(doneID, done.GoalID, goal.StatusComplete, "完成") {
		t.Fatalf("complete setup: %v", err)
	}

	reloaded := goal.NewStore(filepath.Join(dir, "goals"))
	app := &App{}
	engine := NewGoalContinuationEngine(reloaded, app)
	engine.cooldown = time.Hour
	app.goalContinuation = engine
	t.Cleanup(func() {
		engine.CancelPending(localID)
		engine.CancelPending(pausedID)
		engine.CancelPending(doneID)
	})

	if snap := app.GetSessionGoalBar(localID); snap["visible"] != true || snap["status"] != string(goal.StatusActive) || goalTimerScheduled(t, engine, localID) {
		t.Fatalf("bar before the assistant channel = %#v scheduled=%v", snap, goalTimerScheduled(t, engine, localID))
	}

	handler := &IMMessageHandler{app: app, goalStore: reloaded}
	app.imHandler = handler
	handler.storeStickyCodingWorkbenchMemory(localID, stickyCodingWorkbenchMemory{
		Kind:        "local",
		ProjectPath: "D:/repo/app",
	})
	t.Cleanup(func() { handler.clearStickyCodingWorkbenchMemory(localID) })

	if snap := app.GetSessionGoalBar(localID); snap["status"] != string(goal.StatusActive) {
		t.Fatalf("bar after restart = %#v", snap)
	}
	if !goalTimerScheduled(t, engine, localID) {
		t.Fatal("active goal was not resumed after restart")
	}
	if goalTimerScheduled(t, engine, pausedID) || goalTimerScheduled(t, engine, doneID) {
		t.Fatal("a paused or finished goal was resumed")
	}
	if handler.hasPendingTemplateSubAgentExecution(localID) {
		t.Fatal("the bar read must not arm a coding route; the continuation turn does that")
	}
	if mem := handler.getStickyCodingWorkbenchMemory(localID); normalizeProjectSessionPath(mem.ProjectPath) != normalizeProjectSessionPath("D:/repo/app") {
		t.Fatalf("restart rewrote sticky project path: %+v", mem)
	}
	got := reloaded.Get(localID)
	if got == nil || got.Status != goal.StatusActive || !got.ShouldContinue() {
		t.Fatalf("resumed goal = %+v", got)
	}

	engine.mu.Lock()
	gen := engine.scheduleGen[localID]
	engine.mu.Unlock()
	app.GetSessionGoalBar(localID)
	engine.mu.Lock()
	again := engine.scheduleGen[localID]
	engine.mu.Unlock()
	if again != gen {
		t.Fatalf("second restart resume replaced the armed turn gen=%d/%d", gen, again)
	}
}

func TestRestartDoesNotResumeAGoalCreatedAfterStartup(t *testing.T) {
	store := goal.NewStore("")
	app := &App{}
	engine := NewGoalContinuationEngine(store, app)
	engine.cooldown = time.Hour
	app.goalContinuation = engine
	app.imHandler = &IMMessageHandler{app: app, goalStore: store}
	const userID = "desktop-user"
	t.Cleanup(func() { engine.CancelPending(userID) })
	if _, err := store.Set(userID, "这次进程里新建的"); err != nil {
		t.Fatal(err)
	}
	if snap := app.GetSessionGoalBar(userID); snap["status"] != string(goal.StatusActive) || goalTimerScheduled(t, engine, userID) {
		t.Fatalf("a new goal must keep the timer its creator armed, snap=%#v scheduled=%v", snap, goalTimerScheduled(t, engine, userID))
	}
}

func TestRestartResumesGoalAskedWithTheOtherSlash(t *testing.T) {
	dir := t.TempDir()
	const stored = `desktop-user:F:\bird2`
	const asked = `desktop-user:F:/bird2`
	disk := goal.NewStore(filepath.Join(dir, "goals"))
	if _, err := disk.Set(stored, "做愤怒的小鸟"); err != nil {
		t.Fatal(err)
	}
	reloaded := goal.NewStore(filepath.Join(dir, "goals"))
	app := &App{}
	engine := NewGoalContinuationEngine(reloaded, app)
	engine.cooldown = time.Hour
	app.goalContinuation = engine
	app.imHandler = &IMMessageHandler{app: app, goalStore: reloaded}
	t.Cleanup(func() { engine.CancelPending(stored) })

	snap := app.GetSessionGoalBar(asked)
	if snap["visible"] != true || snap["user_id"] != stored || snap["status"] != string(goal.StatusActive) {
		t.Fatalf("slash variant snap = %#v", snap)
	}
	if !goalTimerScheduled(t, engine, stored) || goalTimerScheduled(t, engine, asked) {
		t.Fatal("resume should arm the stored session id only")
	}
}

func TestRestartPausesActiveGoalWhenRemoteSSHIsDown(t *testing.T) {
	userID := stickyTestUserID(t)
	store := goal.NewStore("")
	if _, err := store.Set(userID, "在远程机器上继续"); err != nil {
		t.Fatal(err)
	}
	app := &App{}
	engine := NewGoalContinuationEngine(store, app)
	engine.cooldown = time.Hour
	app.goalContinuation = engine
	t.Cleanup(func() { engine.CancelPending(userID) })
	handler := &IMMessageHandler{app: app, goalStore: store}
	app.imHandler = handler
	handler.storeStickyCodingWorkbenchMemory(userID, stickyCodingWorkbenchMemory{
		Kind:          "remote",
		RemoteHost:    "home.example",
		RemoteWorkDir: "/tmp/work",
	})
	handler.pendingV2SubAgentExecution.Store(userID, true)
	t.Cleanup(func() {
		handler.pendingV2SubAgentExecution.Delete(userID)
		handler.clearStickyCodingWorkbenchMemory(userID)
	})

	snap := app.GetSessionGoalBar(userID)
	got := store.Get(userID)
	if snap["status"] != string(goal.StatusPaused) || got == nil || got.Status != goal.StatusPaused || !strings.Contains(got.Summary, "SSH") || goalTimerScheduled(t, engine, userID) {
		t.Fatalf("remote restart resume = %#v goal=%+v scheduled=%v", snap, got, goalTimerScheduled(t, engine, userID))
	}
}

func TestGoalClearDropsQueuedContinuation(t *testing.T) {
	userID := "goal-clear-queued"
	store := goal.NewStore("")
	if _, err := store.Set(userID, "实现登录"); err != nil {
		t.Fatal(err)
	}
	engine := NewGoalContinuationEngine(store, &App{})
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending(userID) })
	handler := &IMMessageHandler{app: &App{goalContinuation: engine}, goalStore: store}
	handler.maybeScheduleGoalContinuation(userID, &IMAgentResponse{Text: "好的"}, "goal-continuation")
	engine.mu.Lock()
	stale := engine.scheduleGen[userID]
	goalID := ""
	if g := store.Get(userID); g != nil {
		goalID = g.GoalID
	}
	engine.mu.Unlock()

	if !strings.Contains(handler.goalClearForUser(userID), "目标已清除") {
		t.Fatal("clear text")
	}
	engine.mu.Lock()
	_, scheduled := engine.scheduledTimers[userID]
	engine.mu.Unlock()
	if store.Get(userID) != nil || scheduled {
		t.Fatal("clear left the goal or its timer")
	}
	next, err := store.Set(userID, "下一个目标")
	if err != nil {
		t.Fatal(err)
	}
	engine.runScheduledContinuation(userID, goalID, stale)
	if got := store.Get(userID); got == nil || got.GoalID != next.GoalID || got.Status != goal.StatusActive {
		t.Fatalf("stale clear timer touched the next goal: %+v", got)
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
