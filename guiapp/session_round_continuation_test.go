package guiapp

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/goal"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func newSessionRoundHandler(t *testing.T) *IMMessageHandler {
	t.Helper()
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)
	h := &IMMessageHandler{app: &App{}, memory: memory}
	h.sessionRounds.cooldown = time.Hour
	t.Cleanup(func() { h.cancelSessionRoundContinuation("desktop-user") })
	return h
}

func sessionRoundExitOptions(task string, toolCalls int) agentLoopMaxRoundsExitOptions {
	return agentLoopMaxRoundsExitOptions{
		UserID:   "desktop-user",
		UserText: task,
		History: []agent.ConversationEntry{
			{Role: "user", Content: task},
		},
		TotalToolCallsInLoop:   toolCalls,
		ConversationStartedAt:  time.Now(),
		AttachLLMTelemetry:     func(*IMAgentResponse) {},
		AttachVisibleArtifacts: func(*IMAgentResponse) {},
	}
}

func TestSessionRoundContinuationSchedulesTheNextWave(t *testing.T) {
	h := newSessionRoundHandler(t)
	ctx := NewLoopContext("chat", 30, nil)
	task := "把这批论文做成综述"
	resp := h.maxRoundsAgentLoopExit(ctx, sessionRoundExitOptions(task, 4))
	if resp == nil || !resp.sessionRoundContinue {
		t.Fatalf("response = %#v, want the same task to continue", resp)
	}
	if strings.Contains(resp.Text, "Send another message") || strings.Contains(resp.Text, "请继续发送") {
		t.Fatalf("response asked the user to continue: %q", resp.Text)
	}
	if !strings.Contains(resp.Text, "自动继续") {
		t.Fatalf("response = %q, want the automatic continuation note", resp.Text)
	}
	h.sessionRounds.mu.Lock()
	armed := h.sessionRounds.timers["desktop-user"] != nil
	waves := h.sessionRounds.waves["desktop-user"]
	h.sessionRounds.mu.Unlock()
	if !armed || waves != 1 {
		t.Fatalf("timer armed=%v waves=%d, want the next wave queued", armed, waves)
	}
	slot := h.memory.GetUnfinishedSlot("desktop-user")
	if slot == nil || slot.Status != agent.UnfinishedTaskSlotStatusMaxRoundsReached {
		t.Fatalf("slot = %#v, want a pending max-rounds slot until the wave fires", slot)
	}
	if !strings.Contains(sessionRoundContinuationPrompt(task), "继续完成同一任务") {
		t.Fatal("continuation prompt does not keep the same task")
	}
}

func TestSessionRoundContinuationStopsWithoutToolProgress(t *testing.T) {
	h := newSessionRoundHandler(t)
	ctx := NewLoopContext("chat", 30, nil)
	resp := h.maxRoundsAgentLoopExit(ctx, sessionRoundExitOptions("把这批论文做成综述", 0))
	if resp == nil || resp.sessionRoundContinue || resp.Text != sessionRoundManualContinueText {
		t.Fatalf("response = %#v, want the manual stop when the wave made no progress", resp)
	}
	h.sessionRounds.mu.Lock()
	armed := h.sessionRounds.timers["desktop-user"] != nil
	h.sessionRounds.mu.Unlock()
	if armed {
		t.Fatal("a wave with no tool progress queued another round")
	}
}

func TestSessionRoundContinuationSkipsLightAndGoalTurns(t *testing.T) {
	h := newSessionRoundHandler(t)
	light := NewLoopContext("chat-light", 30, nil)
	light.Runtime.Execution = ExecutionProfile{Layer: string(executionLayerLight), PromptProfile: "light"}
	if resp := h.maxRoundsAgentLoopExit(light, sessionRoundExitOptions("查询天气", 2)); resp == nil || resp.sessionRoundContinue {
		t.Fatalf("light lookup continued: %#v", resp)
	}
	goal := NewLoopContext("chat-goal", 30, nil)
	opts := sessionRoundExitOptions("[系统续接][execute] 按步骤执行目标", 4)
	if resp := h.maxRoundsAgentLoopExit(goal, opts); resp == nil || resp.sessionRoundContinue {
		t.Fatalf("goal phase started a session wave: %#v", resp)
	}
}

func TestSessionRoundContinuationUserMessageCancelsTheWave(t *testing.T) {
	h := newSessionRoundHandler(t)
	if !h.armSessionRoundContinuation("desktop-user", "把这批论文做成综述") {
		t.Fatal("arm failed")
	}
	h.cancelSessionRoundContinuation("desktop-user")
	h.sessionRounds.mu.Lock()
	armed := h.sessionRounds.timers["desktop-user"] != nil
	waves := h.sessionRounds.waves["desktop-user"]
	h.sessionRounds.mu.Unlock()
	if armed || waves != 0 {
		t.Fatalf("after cancel armed=%v waves=%d", armed, waves)
	}
}

func TestSessionRoundContinuationSupersededFireDoesNotStart(t *testing.T) {
	h := newSessionRoundHandler(t)
	if !h.armSessionRoundContinuation("desktop-user", "把这批论文做成综述") {
		t.Fatal("arm failed")
	}
	h.sessionRounds.mu.Lock()
	gen := h.sessionRounds.gens["desktop-user"]
	epoch := h.sessionRounds.epochs["desktop-user"]
	h.sessionRounds.mu.Unlock()
	h.cancelSessionRoundContinuation("desktop-user")
	h.fireSessionRoundContinuation("desktop-user", "把这批论文做成综述", gen, epoch)
	h.sessionRounds.mu.Lock()
	armed := h.sessionRounds.timers["desktop-user"] != nil
	waves := h.sessionRounds.waves["desktop-user"]
	h.sessionRounds.mu.Unlock()
	if armed || waves != 0 {
		t.Fatalf("superseded fire armed=%v waves=%d", armed, waves)
	}
}

func TestSessionRoundContinuationStopsAtTheWaveCap(t *testing.T) {
	h := newSessionRoundHandler(t)
	h.sessionRounds.waves = map[string]int{"desktop-user": sessionRoundContinuationMaxWaves}
	ctx := NewLoopContext("chat", 30, nil)
	resp := h.maxRoundsAgentLoopExit(ctx, sessionRoundExitOptions("把这批论文做成综述", 6))
	if resp == nil || resp.sessionRoundContinue || resp.Text != sessionRoundManualContinueText {
		t.Fatalf("response = %#v, want the manual stop at the wave cap", resp)
	}
}

type sessionRoundNewTaskClassifier struct{}

func (sessionRoundNewTaskClassifier) Classify(string, string, int) (string, error) {
	return "new", nil
}

func TestSessionRoundContinuationDoesNotResetTheOpenTask(t *testing.T) {
	h := newSessionRoundHandler(t)
	h.taskContextManager = agent.NewTaskContextManager(agent.DefaultTaskContextConfig(), sessionRoundNewTaskClassifier{})
	userID := "desktop-user"
	history := []agent.ConversationEntry{
		{Role: "user", Content: "把这批论文做成综述"},
		{Role: "assistant", Content: "已读完三篇"},
	}
	h.memory.Save(userID, history)
	h.memory.UpsertUnfinishedSlot(userID, &agent.UnfinishedTaskSlot{
		SlotID:   "maxround-1",
		UserID:   userID,
		Status:   agent.UnfinishedTaskSlotStatusResumed,
		Source:   agent.UnfinishedTaskSlotSourceMaxRounds,
		LastTask: "把这批论文做成综述",
	})
	slot := h.memory.GetUnfinishedSlot(userID)
	prompt := sessionRoundContinuationPrompt("把这批论文做成综述")
	_, fresh, clear := h.applyUnifiedTaskContextDecision(
		IMUserMessage{UserID: userID, Platform: sessionRoundContinuationPlatform, Text: prompt},
		prompt,
		explicitTaskSlotDecision{},
		history,
		&slot,
		"",
		false,
		false,
		false,
	)
	if fresh || clear {
		t.Fatalf("host wave looked like a new task fresh=%v clear=%v", fresh, clear)
	}
	if got := h.memory.Load(userID); len(got) != len(history) {
		t.Fatalf("history len=%d, host wave cleared the task", len(got))
	}
}

func TestSessionRoundContinuationDoesNotAnswerForTheUser(t *testing.T) {
	h := newSessionRoundHandler(t)
	userID := "desktop-user"
	prompt := sessionRoundContinuationPrompt("把这批论文做成综述")
	h.pendingAskUser.Store(userID, &pendingAskUserState{
		Question:  "半径用 10 还是 20？",
		Timestamp: time.Now(),
	})
	if _, _, consumed, _ := h.consumePendingAskUserAnswer(userID, prompt, nil); consumed {
		t.Fatal("host wave was consumed as the ask_user answer")
	}
	if _, ok := h.pendingAskUser.Load(userID); !ok {
		t.Fatal("host wave deleted the question waiting for the user")
	}

	h.pendingUserReply.Store(userID, &pendingUserReplyState{
		Question:  "用哪一版模型？",
		Timestamp: time.Now(),
	})
	entries := []agent.ConversationEntry{{Role: "assistant", Content: "用哪一版模型？"}}
	var slot *agent.UnfinishedTaskSlot
	if _, answered := h.bindPendingUserReplyAnswer(IMUserMessage{UserID: userID, Platform: sessionRoundContinuationPlatform, Text: prompt}, prompt, &entries, &slot); answered {
		t.Fatal("host wave was bound as the user's reply")
	}
	if _, ok := h.pendingUserReply.Load(userID); !ok {
		t.Fatal("host wave deleted the reply waiting for the user")
	}

	h.confirmationStore = newAIConfirmationStore("")
	h.confirmationStore.set(&pendingConfirmation{
		ID:           "confirm-1",
		UserID:       userID,
		OriginalText: "把这批论文做成综述",
		Summary:      "综述",
		WorkflowType: "doc",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	})
	trimmed := prompt
	result := h.handlePendingExecutionConfirmation(&IMUserMessage{UserID: userID, Platform: sessionRoundContinuationPlatform, Text: prompt}, &trimmed)
	if result.Handled || result.ConfirmedResume || result.ReprocessAsFreshTask {
		t.Fatalf("host wave changed the pending confirmation: %#v", result)
	}
	if h.confirmationStore.get(userID) == nil {
		t.Fatal("host wave cleared the confirmation still waiting for the user")
	}
}

func TestCodingBudgetHardStopContinuesTheSessionTask(t *testing.T) {
	h := newSessionRoundHandler(t)
	ctx := NewLoopContext("chat", config.MaxAgentIterationsCap, nil)
	history := []agent.ConversationEntry{{Role: "user", Content: "整理这批实验记录并写出结论"}}
	toolCalls := []llm.ToolCall{{Function: llm.ToolCallFunction{Name: "write_file"}}}
	result := h.enforceAgentLoopCodingBudget(
		ctx,
		"desktop-user",
		config.MaxAgentIterationsCap,
		config.MaxAgentIterationsCap-1,
		toolCalls,
		nil,
		history,
		nil,
		"",
		"",
		"",
		func(int, []interface{}) {},
		func(*IMAgentResponse) {},
		func(*IMAgentResponse) {},
	)
	if result.Response == nil || !result.Response.sessionRoundContinue {
		t.Fatalf("response = %#v, want the session task to continue", result.Response)
	}
	if strings.Contains(result.Response.Text, "Send 'continue'") {
		t.Fatalf("response asked the user to continue: %q", result.Response.Text)
	}
}

func TestSessionRoundContinuationStopDropsTheQueuedWave(t *testing.T) {
	h := newSessionRoundHandler(t)
	task := "把这批论文做成综述"
	if !h.armSessionRoundContinuation("desktop-user", task) {
		t.Fatal("arm failed")
	}
	h.sessionRounds.mu.Lock()
	gen := h.sessionRounds.gens["desktop-user"]
	epoch := h.sessionRounds.epochs["desktop-user"]
	h.sessionRounds.mu.Unlock()

	if _, err := h.CancelSessionForUser("desktop-user"); err != nil {
		t.Fatalf("stop during the gap between waves: %v", err)
	}
	h.sessionRounds.mu.Lock()
	armed := h.sessionRounds.timers["desktop-user"] != nil
	waves := h.sessionRounds.waves["desktop-user"]
	h.sessionRounds.mu.Unlock()
	if armed || waves != 0 {
		t.Fatalf("stop left the next wave queued armed=%v waves=%d", armed, waves)
	}
	if h.hasCancelledTaskBoundary("desktop-user") {
		t.Fatal("stopping the queued wave fenced the conversation")
	}
	h.fireSessionRoundContinuation("desktop-user", task, gen, epoch)
	h.sessionRounds.mu.Lock()
	armed = h.sessionRounds.timers["desktop-user"] != nil
	h.sessionRounds.mu.Unlock()
	if armed {
		t.Fatal("a stopped wave fired again")
	}

	if !h.armSessionRoundContinuation("desktop-user", task) {
		t.Fatal("second arm failed")
	}
	if _, err := h.RequestCancelSessionForUser("desktop-user"); err != nil {
		t.Fatalf("interrupt stop during the gap: %v", err)
	}
	h.sessionRounds.mu.Lock()
	armed = h.sessionRounds.timers["desktop-user"] != nil
	waves = h.sessionRounds.waves["desktop-user"]
	h.sessionRounds.mu.Unlock()
	if armed || waves != 0 {
		t.Fatalf("interrupt stop left the next wave queued armed=%v waves=%d", armed, waves)
	}
}

func TestSessionRoundContinuationNoteIsNotAStopClaim(t *testing.T) {
	h := newSessionRoundHandler(t)
	called := false
	h.workStopClaimClassifier = func(string) (bool, error) {
		called = true
		return true, nil
	}
	loop := NewLoopContext("chat", 30, nil)
	loop.WorkLiveAtStart = true
	note := sessionRoundContinuationNote("把这批论文做成综述")
	msg := IMUserMessage{UserID: "desktop-user", Platform: desktopPlatform, Text: "把这批论文做成综述", Lang: "zh-Hans"}
	out := h.correctUnbackedStopClaim(msg, loop, &IMAgentResponse{
		Text:                 note,
		ResponseSource:       sessionRoundContinuationSource,
		sessionRoundContinue: true,
	})
	if called {
		t.Fatal("the continuation note was classified as a stop claim")
	}
	if out == nil || out.Text != note || !out.sessionRoundContinue || out.ResponseSource != sessionRoundContinuationSource {
		t.Fatalf("continuation response was rewritten: %#v", out)
	}
	if loop.WorkStopRan {
		t.Fatal("the continuation note cancelled live work")
	}
}

func TestSessionRoundContinuationKeepsTheFullSurface(t *testing.T) {
	h := &IMMessageHandler{
		app:               &App{},
		unifiedClassifier: semanticClassifierForLabel(t, intent.LabelGoalManage),
		toolDefGen: NewToolDefinitionGenerator(nil, []map[string]interface{}{
			toolDef("goal", "lifecycle goal", nil, nil),
			toolDef("read_file", "read", nil, nil),
			toolDef("write_file", "write", nil, nil),
		}),
	}
	text := sessionRoundContinuationPrompt("把这批论文做成综述")
	profile, semantic := h.classifyIMExecutionProfileAndSemantic(IMUserMessage{
		UserID:   "desktop-user",
		Platform: sessionRoundContinuationPlatform,
		Text:     text,
	}, false, false)
	if semantic != nil || profile.Reason != "session round continuation" || profile.IsLight() {
		t.Fatalf("profile=%+v semantic=%+v", profile, semantic)
	}
	workerProfile, workerSemantic := h.classifyIMExecutionProfileAndSemantic(IMUserMessage{
		UserID:   "desktop-user:task:ct",
		Platform: desktopPlatform,
		Text:     text,
	}, false, false)
	if workerSemantic != nil || workerProfile.Reason != "session round continuation" || workerProfile.IsLight() {
		t.Fatalf("worker profile=%+v semantic=%+v", workerProfile, workerSemantic)
	}

	ctx := &LoopContext{Platform: sessionRoundContinuationPlatform}
	ctx.Runtime.Execution = profile
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndAttachments(ctx, "desktop-user", text, sessionRoundContinuationPlatform, nil)
	if err != nil || handled || surface != nil || len(defs) != 0 {
		t.Fatalf("managed surface took the session wave: handled=%v err=%v defs=%d", handled, err, len(defs))
	}
	state := h.prepareAgentLoopStartState(agentLoopStartOptions{
		Context:      ctx,
		UserID:       "desktop-user",
		UserText:     text,
		SystemPrompt: "你是聊天助手",
		Platform:     sessionRoundContinuationPlatform,
	})
	if state.Cleanup != nil {
		t.Cleanup(state.Cleanup)
	}
	if state.SemanticSurface != nil || ctx.Runtime.RoutingMissFallback || ctx.Runtime.SemanticIntent != nil {
		t.Fatalf("session wave was reclassified: surface=%v miss=%v intent=%+v", state.SemanticSurface != nil, ctx.Runtime.RoutingMissFallback, ctx.Runtime.SemanticIntent)
	}
}

func TestSessionRoundContinuationDoesNotScheduleTheGoal(t *testing.T) {
	userID := "desktop-user"
	store := goal.NewStore("")
	if _, err := store.Set(userID, "做游戏", goal.WithMaxTurns(5)); err != nil {
		t.Fatalf("Set goal: %v", err)
	}
	engine := NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	t.Cleanup(func() { engine.CancelPending(userID) })
	h := &IMMessageHandler{app: &App{goalContinuation: engine}}

	h.maybeScheduleGoalContinuation(userID, &IMAgentResponse{
		Text:                 sessionRoundContinuationNote("做游戏"),
		ResponseSource:       sessionRoundContinuationSource,
		sessionRoundContinue: true,
	}, desktopPlatform)
	if goalTimerScheduled(t, engine, userID) {
		t.Fatal("a session wave started the goal while the chat was still running")
	}

	h.maybeScheduleGoalContinuation(userID, &IMAgentResponse{Error: "编译失败"}, sessionRoundContinuationPlatform)
	if got := store.Get(userID); got == nil || got.Status != goal.StatusActive {
		t.Fatalf("session failure closed the goal: %+v", got)
	}
	if goalTimerScheduled(t, engine, userID) {
		t.Fatal("a failed session wave started the goal")
	}

	h.maybeScheduleGoalContinuation(userID, &IMAgentResponse{Text: "综述已经写完。"}, sessionRoundContinuationPlatform)
	if !goalTimerScheduled(t, engine, userID) {
		t.Fatal("a finished session wave left the active goal idle")
	}
}

func TestSessionRoundContinuationNoteDoesNotClearPendingReply(t *testing.T) {
	h := newSessionRoundHandler(t)
	userID := "desktop-user"
	h.pendingUserReply.Store(userID, &pendingUserReplyState{
		Question:  "半径用 10 还是 20？",
		Timestamp: time.Now(),
	})
	classified := make(chan string, 1)
	h.pendingReplyPromptClassifier = func(text string) (bool, error) {
		classified <- text
		return false, nil
	}
	history := []agent.ConversationEntry{
		{Role: "user", Content: "把这批论文做成综述"},
		{Role: "assistant", Content: "半径用 10 还是 20？"},
	}
	h.updatePendingUserReplyFromHistory(userID, history, &IMAgentResponse{
		Text:                 sessionRoundContinuationNote("把这批论文做成综述"),
		ResponseSource:       sessionRoundContinuationSource,
		sessionRoundContinue: true,
	})
	select {
	case text := <-classified:
		t.Fatalf("continuation note was classified as a question: %q", text)
	case <-time.After(300 * time.Millisecond):
	}
	raw, ok := h.pendingUserReply.Load(userID)
	if !ok {
		t.Fatal("continuation note deleted the reply still waiting for the user")
	}
	pending, _ := raw.(*pendingUserReplyState)
	if pending == nil || pending.Question != "半径用 10 还是 20？" {
		t.Fatalf("pending question changed: %+v", raw)
	}
}

func TestSessionRoundContinuationDoesNotDriveTheWorkflow(t *testing.T) {
	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "desktop-user"
	if _, err := h.app.workflowV2.machine.Create(userID, "testing", t.TempDir(), "补完接口测试"); err != nil {
		t.Fatal(err)
	}
	var classified int
	h.app.workflowV2.machine.SetConfirmClassifier(func(_, text string) string {
		classified++
		if sessionRoundTurn(text, "") || strings.TrimSpace(text) == "确认" {
			return "confirm"
		}
		return ""
	})
	h.pendingExperimentNotification.Store(userID, "实验已经结束")
	prompt := sessionRoundContinuationPrompt("把接口测试补完")
	for _, platform := range []string{sessionRoundContinuationPlatform, desktopPlatform} {
		got := h.routeWithWorkflowV2(IMUserMessage{UserID: userID, Platform: platform, Text: prompt}, prompt)
		if got.Response != nil || got.WorkflowAgentLoop {
			t.Fatalf("platform %s consumed the session wave: %+v", platform, got)
		}
	}
	if _, ok := h.pendingExperimentNotification.Load(userID); !ok {
		t.Fatal("session wave consumed the pending experiment notification")
	}
	if classified != 0 {
		t.Fatalf("session wave was classified as workflow input: %d", classified)
	}
	h.pendingExperimentNotification.Delete(userID)
	open := h.app.workflowV2.machine.GetActive(userID)
	if open == nil || open.Status != "active" {
		t.Fatalf("session wave changed the open workflow: %+v", open)
	}
	form := h.routeWithWorkflowV2(IMUserMessage{UserID: userID, Platform: desktopPlatform, Text: "开始"}, "开始")
	if form.Response == nil || !strings.Contains(form.Response.Text, "填写") {
		t.Fatalf("an ordinary message no longer opens the phase form: %+v", form.Response)
	}

	if err := h.app.workflowV2.machine.RecordOutput(userID, "strategy"); err != nil {
		t.Fatal(err)
	}
	waiting := h.app.workflowV2.machine.GetActive(userID)
	if waiting == nil || !waiting.IsWaitingConfirm() {
		t.Fatalf("expected a review gate, got %+v", waiting)
	}
	classified = 0
	for _, platform := range []string{sessionRoundContinuationPlatform, desktopPlatform} {
		got := h.routeWithWorkflowV2(IMUserMessage{UserID: userID, Platform: platform, Text: prompt}, prompt)
		if got.Response != nil || got.WorkflowAgentLoop {
			t.Fatalf("platform %s answered the review gate: %+v", platform, got)
		}
	}
	if classified != 0 {
		t.Fatalf("session wave confirmed the review: %d", classified)
	}
	if fresh := h.app.workflowV2.machine.GetActive(userID); fresh == nil || !fresh.IsWaitingConfirm() {
		t.Fatalf("session wave moved the review gate: %+v", fresh)
	}
	confirmed := h.routeWithWorkflowV2(IMUserMessage{UserID: userID, Platform: desktopPlatform, Text: "确认"}, "确认")
	if confirmed.Response != nil && strings.Contains(confirmed.Response.Text, "等待") {
		t.Fatalf("a real confirmation stayed on the barrier: %+v", confirmed.Response)
	}
	if fresh := h.app.workflowV2.machine.GetActive(userID); fresh != nil && fresh.IsWaitingConfirm() {
		t.Fatalf("a real confirmation did not leave the review gate: %+v", fresh)
	}
}
