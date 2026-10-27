package guiapp

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/config"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func newCodingWorkbenchContinuationHandler(t *testing.T) *IMMessageHandler {
	t.Helper()
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)
	h := &IMMessageHandler{app: &App{}, memory: memory}
	h.sessionRounds.cooldown = time.Hour
	return h
}

func TestSessionRoundCodingTaskUnwrapsTheHostWave(t *testing.T) {
	task := "做一个偷金子游戏"
	if got := sessionRoundCodingTask("普通请求"); got != "普通请求" {
		t.Fatalf("plain request changed: %q", got)
	}
	if got := sessionRoundCodingTask(sessionRoundContinuationPrompt(task)); got != task {
		t.Fatalf("wave task = %q, want %q", got, task)
	}
}

func TestCodingWorkbenchContinuationTaskResolvesBareContinue(t *testing.T) {
	h := newCodingWorkbenchContinuationHandler(t)
	userID := "desktop-user:coding-continuation-task"
	h.stickyCodingWorkbenchMemory.Store(userID, stickyCodingWorkbenchMemory{
		SessionPlan: "做一个偷金子游戏",
	})
	if got := h.codingWorkbenchContinuationTask(userID, "继续"); got != "做一个偷金子游戏" {
		t.Fatalf("bare continue task = %q, want the session plan", got)
	}
	if got := h.codingWorkbenchContinuationTask(userID, "把按钮改成红色"); got != "把按钮改成红色" {
		t.Fatalf("a new request was rewritten: %q", got)
	}
	if got := h.codingWorkbenchContinuationTask(userID, sessionRoundContinuationPrompt("继续")); got != "做一个偷金子游戏" {
		t.Fatalf("continued wave task = %q, want the session plan", got)
	}
}

func TestRemoteStepIterationExhaustionIsContinuable(t *testing.T) {
	result := remoteCodingStepToRunResult(
		&v2.TaskItem{Index: 1, Title: "实现玩法"},
		&RemoteCodingSubAgentResult{Status: "failed", Error: "max iterations reached"},
	)
	if !codingWorkbenchIterationExhausted([]v2.TaskRunResult{result}) {
		t.Fatalf("remote iteration exhaustion was not detected: %#v", result)
	}
}

func TestSessionRoundResumesIncompleteCodingPlan(t *testing.T) {
	mem := stickyCodingWorkbenchMemory{
		PlanRunStarted: true,
		StepStatuses: []codingWorkbenchStepStatus{
			{Index: 1, Title: "搭骨架", Status: codingStepPassed},
			{Index: 2, Title: "实现玩法", Status: codingStepFailed},
			{Index: 3, Title: "验证", Status: codingStepPending},
		},
	}
	tasks, ok := selectIncompleteCodingPlanTasks(sessionRoundContinuationPrompt("做一个偷金子游戏"), mem)
	if !ok || len(tasks) != 2 {
		t.Fatalf("resume tasks = %#v, ok=%v, want the two unfinished steps", tasks, ok)
	}
	if tasks[0].Index != 2 || tasks[1].Index != 3 {
		t.Fatalf("resume order = %d,%d, want 2,3", tasks[0].Index, tasks[1].Index)
	}
}

func TestCodingWorkbenchSessionRoundKeepsTheOriginalTask(t *testing.T) {
	h := newCodingWorkbenchContinuationHandler(t)
	projectPath := t.TempDir()
	userID := "desktop-user:" + projectPath
	wave := sessionRoundContinuationPrompt("做一个偷金子游戏")

	tasks, _, planned := h.resolveCodingWorkbenchTasksWithDecision(
		userID,
		wave,
		projectPath,
		stickyCodingWorkbenchMemory{PlanMode: codingPlanModeAuto},
		codingRequestDecision{Kind: codingRequestImplementation, NeedsPlan: true},
		nil,
		nil,
	)
	if planned || len(tasks) != 1 {
		t.Fatalf("single-task wave tasks=%#v planned=%v", tasks, planned)
	}
	if tasks[0].Title != "做一个偷金子游戏" || strings.Contains(tasks[0].Description, "[系统续轮]") {
		t.Fatalf("wave header leaked into the task: %#v", tasks[0])
	}

	mem := stickyCodingWorkbenchMemory{
		PlanRunStarted: true,
		StepStatuses: []codingWorkbenchStepStatus{
			{Index: 1, Title: "搭骨架", Status: codingStepPassed},
			{Index: 2, Title: "实现玩法", Status: codingStepFailed},
			{Index: 3, Title: "验证", Status: codingStepPending},
		},
	}
	tasks, _, planned = h.resolveCodingWorkbenchTasksWithDecision(
		userID,
		wave,
		projectPath,
		mem,
		codingRequestDecision{Kind: codingRequestImplementation, NeedsPlan: true},
		nil,
		nil,
	)
	if !planned || len(tasks) != 2 || tasks[0].Index != 2 || tasks[1].Index != 3 {
		t.Fatalf("planned wave tasks=%#v planned=%v, want T2 and T3 resumed", tasks, planned)
	}
}

func TestCodingWorkbenchIterationExhaustionArmsSessionRound(t *testing.T) {
	h := newCodingWorkbenchContinuationHandler(t)
	userID := "desktop-user:coding-continuation"
	t.Cleanup(func() { h.cancelSessionRoundContinuation(userID) })
	ctx := NewLoopContext("coding-workbench", config.MaxAgentIterationsCap, nil)
	ctx.UserID = userID
	results := []v2.TaskRunResult{
		{TaskIndex: 1, Status: v2.TaskFailed, Error: "max iterations reached"},
	}

	note, ok := h.continueCodingWorkbenchAfterIterationBudget(ctx, userID, "做一个偷金子游戏", results, 320)
	if !ok || strings.TrimSpace(note) == "" {
		t.Fatalf("continuation = %q, ok=%v, want a queued session round", note, ok)
	}
	h.sessionRounds.mu.Lock()
	armed := h.sessionRounds.timers[userID] != nil
	waves := h.sessionRounds.waves[userID]
	h.sessionRounds.mu.Unlock()
	if !armed || waves != 1 {
		t.Fatalf("armed=%v waves=%d, want the next round queued", armed, waves)
	}
}

func TestCodingWorkbenchContinuationUsesTheTurnEpoch(t *testing.T) {
	h := newCodingWorkbenchContinuationHandler(t)
	userID := "desktop-user:coding-epoch"
	t.Cleanup(func() { h.cancelSessionRoundContinuation(userID) })
	results := []v2.TaskRunResult{
		{TaskIndex: 1, Status: v2.TaskFailed, Error: "max iterations reached"},
	}

	// A prior user turn bumps the epoch; this turn starts at that value.
	h.cancelSessionRoundContinuation(userID)
	ctx := NewLoopContext("coding-workbench", config.MaxAgentIterationsCap, nil)
	ctx.UserID = userID
	ctx.sessionRoundEpochAtStart = h.sessionRoundEpoch(userID)
	if _, ok := h.continueCodingWorkbenchAfterIterationBudget(ctx, userID, "做一个偷金子游戏", results, 80); !ok {
		t.Fatal("matching turn epoch did not arm the session round")
	}

	// A newer user turn bumps it again while this one runs.
	h.cancelSessionRoundContinuation(userID)
	if _, ok := h.continueCodingWorkbenchAfterIterationBudget(ctx, userID, "做一个偷金子游戏", results, 80); ok {
		t.Fatal("a superseded turn armed a session round")
	}
	h.sessionRounds.mu.Lock()
	armed := h.sessionRounds.timers[userID] != nil
	h.sessionRounds.mu.Unlock()
	if armed {
		t.Fatal("a superseded turn left a wave queued")
	}
}

func TestPrepareIMLoopContextRefreshesEpochOnRetry(t *testing.T) {
	h := newCodingWorkbenchContinuationHandler(t)
	userID := "desktop-user:coding-retry"
	t.Cleanup(func() { h.cancelSessionRoundContinuation(userID) })
	msg := IMUserMessage{RequestID: "req-retry", UserID: userID, Platform: desktopPlatform, Text: "做一个偷金子游戏"}

	// The ingress cancels a queued wave before preparing the turn.
	h.cancelSessionRoundContinuation(userID)
	ctx := h.prepareIMLoopContext(nil, msg, nil, false, false)
	if ctx.sessionRoundEpochAtStart == 0 {
		t.Fatal("epoch was not captured")
	}

	// A transport retry of the same payload bumps the epoch at ingress.
	// Preparing the same loop again must capture the new value; otherwise a
	// retried long turn loses its continuation.
	h.cancelSessionRoundContinuation(userID)
	retried := h.prepareIMLoopContext(ctx, msg, nil, false, false)
	if retried.sessionRoundEpochAtStart != h.sessionRoundEpoch(userID) {
		t.Fatalf("retry epoch = %d, want %d", retried.sessionRoundEpochAtStart, h.sessionRoundEpoch(userID))
	}
}

func TestCodingWorkbenchContinuationLeavesOtherStopsAlone(t *testing.T) {
	h := newCodingWorkbenchContinuationHandler(t)
	userID := "desktop-user:coding-continuation-stop"
	t.Cleanup(func() { h.cancelSessionRoundContinuation(userID) })
	ctx := NewLoopContext("coding-workbench", config.MaxAgentIterationsCap, nil)
	ctx.UserID = userID

	if _, ok := h.continueCodingWorkbenchAfterIterationBudget(ctx, userID, "做一个偷金子游戏", []v2.TaskRunResult{
		{TaskIndex: 1, Status: v2.TaskFailed, Error: "编译失败"},
	}, 20); ok {
		t.Fatal("a real task failure was continued")
	}
	if _, ok := h.continueCodingWorkbenchAfterIterationBudget(ctx, userID, "[系统续接][execute] 按步骤执行目标", []v2.TaskRunResult{
		{TaskIndex: 1, Status: v2.TaskFailed, Error: "max iterations reached"},
	}, 80); ok {
		t.Fatal("a goal turn armed a session round beside its own continuation")
	}
	if _, ok := h.continueCodingWorkbenchAfterIterationBudget(ctx, userID, "做一个偷金子游戏", []v2.TaskRunResult{
		{TaskIndex: 1, Status: v2.TaskFailed, Error: "max iterations reached"},
	}, 0); ok {
		t.Fatal("a round with no tool progress was continued")
	}
	workflow := NewLoopContext("coding-workflow", config.MaxAgentIterationsCap, nil)
	workflow.UserID = userID
	workflow.WorkflowDocPhase = true
	if _, ok := h.continueCodingWorkbenchAfterIterationBudget(workflow, userID, "做一个偷金子游戏", []v2.TaskRunResult{
		{TaskIndex: 1, Status: v2.TaskFailed, Error: "max iterations reached"},
	}, 80); ok {
		t.Fatal("a workflow document phase armed a session round beside its own retry")
	}
	h.sessionRounds.mu.Lock()
	armed := h.sessionRounds.timers[userID] != nil
	h.sessionRounds.mu.Unlock()
	if armed {
		t.Fatal("a non-continuable stop armed a session round")
	}
}
