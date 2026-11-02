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

func TestIterationLimitIsNotShownAsATaskError(t *testing.T) {
	prev, _ := agentViewCurrentLang.Load().(string)
	agentViewCurrentLang.Store("zh-Hans")
	t.Cleanup(func() { agentViewCurrentLang.Store(prev) })

	handoff := v2.TaskRunResult{
		TaskIndex: 1,
		Title:     "继续优化",
		Status:    v2.TaskFailed,
		Summary:   "任务运行错误 T1，300 轮迭代，322 次工具调用",
		Error:     "max iterations reached",
	}
	real := v2.TaskRunResult{
		TaskIndex: 2,
		Title:     "编译",
		Status:    v2.TaskFailed,
		Summary:   "任务运行错误 T2，12 轮迭代，4 次工具调用",
		Error:     "编译失败",
	}

	shown := formatCodingAgentResultParagraph(handoff)
	if strings.Contains(shown, "任务运行错误") || strings.Contains(shown, "max iterations reached") || strings.Contains(shown, "额度") || strings.Contains(shown, "异常") {
		t.Fatalf("iteration cap shown as a task error: %q", shown)
	}
	if !strings.Contains(shown, "迭代上限") {
		t.Fatalf("manual stop should say the round can continue: %q", shown)
	}

	continued := formatCodingWorkbenchUserAnswer(codingRequestImplementation, codingWorkbenchResultsForReport([]v2.TaskRunResult{handoff}, true), false)
	if strings.TrimSpace(continued) != "" {
		t.Fatalf("an automatic next round should not close the transcript: %q", continued)
	}
	withFailure := formatCodingWorkbenchUserAnswer(codingRequestImplementation, codingWorkbenchResultsForReport([]v2.TaskRunResult{handoff, real}, true), false)
	if strings.Contains(withFailure, "迭代上限") || strings.Contains(withFailure, "max iterations reached") || strings.Contains(withFailure, "额度") {
		t.Fatalf("queued continuation still showed the cap: %q", withFailure)
	}
	if !strings.Contains(withFailure, "编译失败") {
		t.Fatalf("a real failure was dropped from the continued report: %q", withFailure)
	}

	note := sessionRoundContinuationNote("继续优化")
	if note != "接着做。" || strings.Contains(note, "额度") || strings.Contains(note, "错误") {
		t.Fatalf("continuation note = %q", note)
	}

	withNote := formatCodingAgentResultParagraph(v2.TaskRunResult{
		Status:  v2.TaskFailed,
		Summary: "MSA 内核还差最后一处对齐。",
		Error:   "max iterations reached",
	})
	if !strings.Contains(withNote, "MSA 内核还差最后一处对齐") || !strings.Contains(withNote, "迭代上限") || strings.Contains(withNote, "任务运行错误") {
		t.Fatalf("manual stop dropped the progress note: %q", withNote)
	}
}

func TestCodingLoopRefillsIterationsInTheSameConversation(t *testing.T) {
	root := &codingSubAgentCallbacks{subagent: &CodingSubAgent{}}
	if got := root.ExtendIterationBudget(80, 0); got != 0 {
		t.Fatalf("a turn with no tool calls was refilled by %d", got)
	}
	if got := root.ExtendIterationBudget(80, 12); got != codingSubAgentPerTaskMaxIterations {
		t.Fatalf("refill = %d, want another %d iterations of the same task", got, codingSubAgentPerTaskMaxIterations)
	}
	root.iterationExtensions = sessionRoundContinuationMaxWaves
	if got := root.ExtendIterationBudget(80, 12); got != codingSubAgentPerTaskMaxIterations {
		t.Fatalf("unfinished coding task stopped after %d waves: refill = %d", sessionRoundContinuationMaxWaves, got)
	}

	parent := NewLoopContext("coding-parent", 80, nil)
	held := &codingSubAgentCallbacks{subagent: &CodingSubAgent{loopCtx: parent}}
	if got := held.GetMaxIterations(); got != 80 {
		t.Fatalf("coding wave = %d, want 80", got)
	}
	parent.SetMaxIterations(30)
	if got := held.GetMaxIterations(); got != 80 {
		t.Fatalf("coding wave followed the parent reset: %d", got)
	}
	parent.SetMaxIterations(200)
	if got := held.GetMaxIterations(); got != 200 {
		t.Fatalf("coding wave = %d, want the raised parent budget 200", got)
	}
	parent.SetMaxIterations(30)
	if got := held.ExtendIterationBudget(200, 8); got != 200 {
		t.Fatalf("coding refill followed the parent reset: %d", got)
	}

	nested := &codingSubAgentCallbacks{subagent: &CodingSubAgent{nestDepth: 1}}
	if got := nested.ExtendIterationBudget(10, 4); got != 0 {
		t.Fatalf("a nested worker was refilled by %d", got)
	}

	remote := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{}}
	if got := remote.ExtendIterationBudget(300, 20); got != config.MaxAgentIterationsCap {
		t.Fatalf("remote refill = %d, want the root budget", got)
	}
	remote.iterationExtensions = sessionRoundContinuationMaxWaves
	if got := remote.ExtendIterationBudget(300, 20); got != config.MaxAgentIterationsCap {
		t.Fatalf("unfinished remote coding task stopped after %d waves: refill = %d", sessionRoundContinuationMaxWaves, got)
	}
	remote.agent.nestDepth = 1
	if got := remote.ExtendIterationBudget(300, 20); got != 0 {
		t.Fatalf("a nested remote worker was refilled by %d", got)
	}

	remoteParent := NewLoopContext("remote-parent", 80, nil)
	remoteHeld := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{loopCtx: remoteParent}}
	if got := remoteHeld.GetMaxIterations(); got != 80 {
		t.Fatalf("remote wave = %d, want 80", got)
	}
	remoteParent.SetMaxIterations(30)
	if got := remoteHeld.GetMaxIterations(); got != 80 {
		t.Fatalf("remote wave followed the parent reset: %d", got)
	}
	remoteParent.SetMaxIterations(200)
	if got := remoteHeld.GetMaxIterations(); got != 200 {
		t.Fatalf("remote wave = %d, want the raised parent budget 200", got)
	}
	remoteParent.SetMaxIterations(30)
	if got := remoteHeld.ExtendIterationBudget(200, 8); got != 200 {
		t.Fatalf("remote refill followed the parent reset: %d", got)
	}
}

func TestCodingWorkbenchIterationExhaustionDoesNotStartANewTask(t *testing.T) {
	h := newCodingWorkbenchContinuationHandler(t)
	userID := "desktop-user:coding-continuation"
	t.Cleanup(func() { h.cancelSessionRoundContinuation(userID) })
	ctx := NewLoopContext("coding-workbench", config.MaxAgentIterationsCap, nil)
	ctx.UserID = userID
	results := []v2.TaskRunResult{
		{TaskIndex: 1, Status: v2.TaskFailed, Error: "max iterations reached"},
	}

	note, ok := h.continueCodingWorkbenchAfterIterationBudget(ctx, userID, "做一个偷金子游戏", results, 320)
	if ok || strings.TrimSpace(note) != "" {
		t.Fatalf("continuation = %q, ok=%v, want the same loop to keep the task", note, ok)
	}
	h.sessionRounds.mu.Lock()
	armed := h.sessionRounds.timers[userID] != nil
	waves := h.sessionRounds.waves[userID]
	h.sessionRounds.mu.Unlock()
	if armed || waves != 0 {
		t.Fatalf("armed=%v waves=%d, want no new task", armed, waves)
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
	if _, ok := h.continueCodingWorkbenchAfterIterationBudget(ctx, userID, "做一个偷金子游戏", results, 80); ok {
		t.Fatal("a coding turn started a new task after the iteration cap")
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

func TestCodingWorkbenchContinuationSkipsACPSessions(t *testing.T) {
	h := newCodingWorkbenchContinuationHandler(t)
	userID := acpAssistantSessionOwnerID("vscode-session")
	t.Cleanup(func() { h.cancelSessionRoundContinuation(userID) })
	ctx := NewLoopContext("coding-workbench", config.MaxAgentIterationsCap, nil)
	ctx.UserID = userID
	results := []v2.TaskRunResult{
		{TaskIndex: 1, Status: v2.TaskFailed, Error: "max iterations reached"},
	}
	if _, ok := h.continueCodingWorkbenchAfterIterationBudget(ctx, userID, "做一个偷金子游戏", results, 80); ok {
		t.Fatal("an ACP session armed a desktop continuation")
	}
	h.sessionRounds.mu.Lock()
	armed := h.sessionRounds.timers[userID] != nil
	h.sessionRounds.mu.Unlock()
	if armed {
		t.Fatal("an ACP session left a wave queued")
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
