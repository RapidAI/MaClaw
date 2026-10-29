package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/goal"
)

func TestHandleGoalCommandAcceptsFullwidthSlash(t *testing.T) {
	h := &IMMessageHandler{goalStore: goal.NewStore("")}
	userID := "desktop-user"
	text := "／goal\u3000做一个游戏"
	resp, handled := h.handleImmediateIMCommand(
		IMUserMessage{UserID: userID, Text: text, Lang: "zh-Hans"},
		text, nil, nil,
	)
	if !handled || resp == nil || resp.Error != "" {
		t.Fatalf("handled=%v resp=%+v", handled, resp)
	}
	g := h.getGoalStore().Get(userID)
	if g == nil || g.Objective != "做一个游戏" || g.Status != goal.StatusActive {
		t.Fatalf("goal = %+v", g)
	}
}

func TestClassifyImmediateIMCommandGoalCaseInsensitive(t *testing.T) {
	if got := classifyImmediateIMCommand("/Goal implement X"); got != imCommandGoal {
		t.Fatalf("got %v want imCommandGoal", got)
	}
	if got := classifyImmediateIMCommand("/GOAL"); got != imCommandGoal {
		t.Fatalf("bare /GOAL: got %v", got)
	}
	if got := classifyImmediateIMCommand("／goal\u3000做一个游戏"); got != imCommandGoal {
		t.Fatalf("fullwidth slash: got %v", got)
	}
}

func TestHandleGoalCreateRejectsEmptyUserID(t *testing.T) {
	h := &IMMessageHandler{goalStore: goal.NewStore("")}
	resp := h.handleGoalCreate(IMUserMessage{UserID: "", Text: "/goal x"}, "x")
	if resp == nil || resp.Error == "" {
		t.Fatalf("expected error for empty userID, got %+v", resp)
	}
}

func TestHandleGoalCreateRemoteUnarmedDefersContinuation(t *testing.T) {
	h := &IMMessageHandler{goalStore: goal.NewStore("")}
	userID := stickyTestUserID(t)
	// Sticky remote without live SSH / pending — re-arm must fail closed.
	h.storeStickyCodingWorkbenchMemory(userID, stickyCodingWorkbenchMemory{
		Kind:             "remote",
		RemoteSessionID:  "dead-session",
		RemoteWorkDir:    "/home/u/app",
		RemoteProjectDir: "/home/u/app",
	})
	resp := h.handleGoalCreate(IMUserMessage{UserID: userID, Text: "/goal ship feature"}, "ship feature")
	if resp == nil || resp.Error == "" {
		t.Fatalf("create resp=%+v", resp)
	}
	if !strings.Contains(resp.Error, "SSH") && !strings.Contains(resp.Error, "重连") {
		t.Fatalf("expected reconnect error: %q", resp.Error)
	}
	if h.hasPendingTemplateSubAgentExecution(userID) {
		t.Fatal("dead remote should not be armed")
	}
	g := h.getGoalStore().Get(userID)
	if g == nil || g.Objective != "ship feature" || g.Status != goal.StatusFailed {
		t.Fatalf("goal should be stored as failed: %+v", g)
	}
}

func TestHandleGoalCommandUsesMessageUserID(t *testing.T) {
	h := &IMMessageHandler{
		goalStore: goal.NewStore(""),
	}
	// Pollute lastUserID so a bug that keys on it would mis-route status.
	h.lastUserID = "desktop-user"
	userID := "desktop-user:D:/pure-coding-task"

	// Pure coding sticky so session plan sync engages.
	h.rearmStickyLocalCodingEnvironment(userID, "D:/repo/app")

	create, handled := h.handleImmediateIMCommand(
		IMUserMessage{UserID: userID, Text: "/goal implement auth module", Lang: "zh-Hans"},
		"/goal implement auth module", nil, nil,
	)
	if !handled || create == nil || create.Error != "" {
		t.Fatalf("create handled=%v resp=%+v", handled, create)
	}
	if create.Text != "目标已开始：implement auth module" {
		t.Fatalf("create text = %q", create.Text)
	}
	if strings.Contains(create.Text, "编程工作台") || strings.Contains(create.Text, "/goal pause") || strings.Contains(create.Text, "最大轮次") {
		t.Fatalf("create ack should not repeat the command card: %q", create.Text)
	}

	g := h.getGoalStore().Get(userID)
	if g == nil || g.Objective != "implement auth module" {
		t.Fatalf("goal not stored under project userID: %+v", g)
	}
	if wrong := h.getGoalStore().Get("desktop-user"); wrong != nil {
		t.Fatalf("goal must not land on lastUserID desktop-user: %+v", wrong)
	}

	// Session plan mirrors objective for pure coding banner.
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if mem.SessionPlan != "implement auth module" {
		t.Fatalf("session plan = %q", mem.SessionPlan)
	}

	status, handled := h.handleImmediateIMCommand(
		IMUserMessage{UserID: userID, Text: "/goal status", Lang: "zh-Hans"},
		"/goal status", nil, nil,
	)
	if !handled || status == nil {
		t.Fatal("status not handled")
	}
	if !strings.Contains(status.Text, "implement auth module") {
		t.Fatalf("status should read project goal: %q", status.Text)
	}

	// Coding tool path uses explicit userID (not lastUserID).
	toolText := h.toolGoalForUser(userID, map[string]interface{}{"action": "get"})
	if !strings.Contains(toolText, "implement auth module") {
		t.Fatalf("toolGoalForUser = %q", toolText)
	}
}

func TestCodingSubAgentActivityTraceCount(t *testing.T) {
	if got := codingSubAgentActivityTraceCount(7, 3); got != 7 {
		t.Fatalf("prefer toolCalls: %d", got)
	}
	if got := codingSubAgentActivityTraceCount(0, 5); got != 5 {
		t.Fatalf("fallback iterations: %d", got)
	}
	if got := codingSubAgentActivityTraceCount(0, 0); got != 0 {
		t.Fatalf("idle: %d", got)
	}
}

func TestFinalizeTraceResultPreservesPrePopulatedTraceEventCount(t *testing.T) {
	// Pure coding maps SubAgent tool calls into TraceEventCount before finalize.
	// When a RunID exists but the main IM trace has no events, finalize must keep
	// the pre-populated count so /goal continuation does not false-pause.
	svc := NewAITraceService()
	h := &IMMessageHandler{traceService: svc}
	_, run := svc.StartJobRun(TraceJobKindAIAssistant, "coding task", "desktop", "desktop-user:D:/x", "")
	ctx := &LoopContext{RunID: run.RunID, JobID: run.JobID}
	resp := h.finalizeTraceResult(ctx, &IMAgentResponse{
		Text:            "编码完成\n...",
		TraceEventCount: 12,
	}, "编码完成", "")
	if resp.TraceEventCount != 12 {
		t.Fatalf("TraceEventCount overwritten to %d, want 12", resp.TraceEventCount)
	}
}

func TestEnsurePureCodingArmedForGoalContinuation(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	// Sticky local kind without pending flags (simulates cold reopen).
	h.storeStickyCodingWorkbenchMemory(userID, stickyCodingWorkbenchMemory{
		Kind:        "local",
		ProjectPath: "D:/repo/app",
	})
	if h.hasPendingTemplateSubAgentExecution(userID) {
		t.Fatal("setup should not be pending")
	}
	h.ensurePureCodingArmedForGoalContinuation(userID)
	if !h.hasPendingTemplateSubAgentExecution(userID) {
		t.Fatal("local pure coding should re-arm pending for goal continuation")
	}
	raw, ok := h.pendingTemplateCodingProjectPath.Load(userID)
	if !ok || normalizeProjectSessionPath(raw.(string)) != normalizeProjectSessionPath("D:/repo/app") {
		t.Fatalf("pending path = %#v", raw)
	}
}

func TestExecuteGoalToolRequiresSessionOwner(t *testing.T) {
	h := &IMMessageHandler{goalStore: goal.NewStore("")}
	sa := &CodingSubAgent{handler: h, fullEnvironment: true, loopCtx: &LoopContext{}}
	cb := &codingSubAgentCallbacks{subagent: sa}
	res := cb.executeGoalTool(map[string]interface{}{"action": "get"})
	if res.Outcome != codingToolOutcomeFailed {
		t.Fatalf("empty UserID must fail, got %v %q", res.Outcome, res.Text)
	}
}

func TestCodingGoalToolDefinitionAndExecute(t *testing.T) {
	def := buildCodingGoalToolDefinition()
	fn, _ := def["function"].(map[string]interface{})
	if fn["name"] != "goal" {
		t.Fatalf("name = %v", fn["name"])
	}

	h := &IMMessageHandler{goalStore: goal.NewStore("")}
	userID := "desktop-user:D:/goal-tool"
	h.rearmStickyLocalCodingEnvironment(userID, "D:/repo")
	sa := &CodingSubAgent{
		handler:         h,
		projectPath:     "D:/repo",
		fullEnvironment: true,
		loopCtx:         &LoopContext{UserID: userID},
	}
	cb := &codingSubAgentCallbacks{subagent: sa}
	res := cb.executeGoalTool(map[string]interface{}{
		"action":    "create",
		"objective": "ship billing API",
	})
	if res.Outcome != codingToolOutcomeSuccess {
		t.Fatalf("outcome=%v text=%q", res.Outcome, res.Text)
	}
	if !strings.Contains(res.Text, "ship billing API") {
		t.Fatalf("text=%q", res.Text)
	}
	if g := h.getGoalStore().Get(userID); g == nil || g.Objective != "ship billing API" {
		t.Fatalf("store goal = %+v", g)
	}
	// Goal is host/orchestrator-owned while Coding S3 lacks durable goal CAS;
	// it must not re-enter the model-visible legacy compatibility surface.
	tools := cb.BuildTools("test")
	found := false
	for _, tool := range tools {
		fn, _ := tool["function"].(map[string]interface{})
		if fn["name"] == "goal" {
			found = true
			break
		}
	}
	if found {
		t.Fatal("full env BuildTools must not expose model-writable goal without durable CAS")
	}
}

func TestGoalPlanWithoutActionKeepsDetailOnlySteps(t *testing.T) {
	h := &IMMessageHandler{goalStore: goal.NewStore("")}
	userID := "desktop-user:D:/goal-infer"
	created := h.toolGoalForUser(userID, map[string]interface{}{
		"action":    "create",
		"objective": "做一个游戏",
	})
	if !strings.Contains(created, "目标已创建") {
		t.Fatalf("create = %q", created)
	}
	planned := h.toolGoalForUser(userID, map[string]interface{}{
		"understanding": "交付可玩的窗口游戏",
		"steps": []interface{}{
			map[string]interface{}{"detail": "确认仓库为空并锁定技术选型，只写决策文档。"},
		},
		"acceptance_criteria": []interface{}{"窗口能打开"},
		"project_path":        `E:\bird-test2`,
	})
	if !strings.Contains(planned, "计划已保存") || !strings.Contains(planned, "确认仓库为空并锁定技术选型，只写决策文档") {
		t.Fatalf("plan = %q", planned)
	}
}

func TestGoalPlanStepAndComplete(t *testing.T) {
	h := &IMMessageHandler{goalStore: goal.NewStore("")}
	userID := "desktop-user:D:/goal-loop"
	created := h.toolGoalForUser(userID, map[string]interface{}{
		"action":    "create",
		"objective": "实现登录",
	})
	if !strings.Contains(created, "目标已创建") {
		t.Fatalf("create = %q", created)
	}
	refused := h.toolGoalForUser(userID, map[string]interface{}{
		"action":  "complete",
		"summary": "go test 通过",
	})
	if !strings.Contains(refused, "尚未分解") {
		t.Fatalf("complete before plan = %q", refused)
	}
	if g := h.getGoalStore().Get(userID); g == nil || g.Status != goal.StatusActive {
		t.Fatalf("refused complete changed status: %+v", g)
	}

	planned := h.toolGoalForUser(userID, map[string]interface{}{
		"action":        "plan",
		"understanding": "交付可登录接口",
		"steps": []interface{}{
			map[string]interface{}{"title": "检查环境"},
			map[string]interface{}{"title": "编写代码"},
		},
		"acceptance_criteria": []interface{}{"go test 通过"},
	})
	if !strings.Contains(planned, "计划已保存") || !strings.Contains(planned, "检查环境") {
		t.Fatalf("plan = %q", planned)
	}

	done := h.toolGoalForUser(userID, map[string]interface{}{
		"action": "step",
		"index":  1,
		"status": "done",
		"result": "环境可用",
	})
	if !strings.Contains(done, "步骤 1 已完成") {
		t.Fatalf("step = %q", done)
	}
	early := h.toolGoalForUser(userID, map[string]interface{}{
		"action":  "complete",
		"summary": "go test 通过",
	})
	if !strings.Contains(early, "未完成") {
		t.Fatalf("complete with a pending step = %q", early)
	}

	if got := h.toolGoalForUser(userID, map[string]interface{}{
		"action": "step",
		"index":  2,
		"status": "done",
		"result": "代码已写",
	}); !strings.Contains(got, "步骤 2 已完成") {
		t.Fatalf("step 2 = %q", got)
	}
	uncovered := h.toolGoalForUser(userID, map[string]interface{}{
		"action":  "complete",
		"summary": "做完了",
	})
	if !strings.Contains(uncovered, "未覆盖验收标准") {
		t.Fatalf("uncovered summary = %q", uncovered)
	}

	finished := h.toolGoalForUser(userID, map[string]interface{}{
		"action":  "complete",
		"summary": "go test 通过",
	})
	if !strings.Contains(finished, "目标已完成") || !strings.Contains(finished, "go test 通过") || !strings.Contains(finished, "代码已写") {
		t.Fatalf("complete = %q", finished)
	}
	if g := h.getGoalStore().Get(userID); g == nil || g.Status != goal.StatusComplete {
		t.Fatalf("status = %+v", g)
	}
}

func TestGoalStepFailureFailsTheGoal(t *testing.T) {
	h := &IMMessageHandler{goalStore: goal.NewStore("")}
	userID := "desktop-user:D:/goal-fail-step"
	h.toolGoalForUser(userID, map[string]interface{}{"action": "create", "objective": "实现登录"})
	g := h.getGoalStore().Get(userID)
	if err := h.getGoalStore().SavePlan(userID, g.GoalID, "写接口", []goal.Step{{Title: "检查环境"}}, []string{"go test 通过"}); err != nil {
		t.Fatal(err)
	}
	got := h.toolGoalForUser(userID, map[string]interface{}{
		"action": "step",
		"index":  1,
		"status": "failed",
		"result": "没有编译器",
	})
	if !strings.Contains(got, "步骤 1 失败") || !strings.Contains(got, "没有编译器") {
		t.Fatalf("step fail = %q", got)
	}
	stored := h.getGoalStore().Get(userID)
	if stored == nil || stored.Status != goal.StatusFailed || !strings.Contains(stored.Summary, "没有编译器") {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestGoalContinuationExposesGoalTool(t *testing.T) {
	h := &IMMessageHandler{goalStore: goal.NewStore("")}
	userID := "desktop-user:D:/goal-tool-exposed"
	sa := &CodingSubAgent{
		handler:         h,
		fullEnvironment: true,
		loopCtx:         &LoopContext{UserID: userID},
		exposeGoalTool:  true,
	}
	cb := &codingSubAgentCallbacks{subagent: sa}
	found := false
	for _, tool := range cb.BuildTools("test") {
		fn, _ := tool["function"].(map[string]interface{})
		if fn["name"] == "goal" {
			found = true
		}
	}
	if !found {
		t.Fatal("continuation turn should expose the goal tool")
	}
}

func TestGoalPlanSuccessStaysSuccessWhenTheBodyMentionsFailure(t *testing.T) {
	h := &IMMessageHandler{goalStore: goal.NewStore("")}
	userID := "desktop-user:D:/goal-plan-outcome"
	h.toolGoalForUser(userID, map[string]interface{}{
		"action":    "create",
		"objective": "以现有性能为基础，提升20%",
	})
	sa := &CodingSubAgent{
		handler:         h,
		fullEnvironment: true,
		loopCtx:         &LoopContext{UserID: userID},
		exposeGoalTool:  true,
	}
	cb := &codingSubAgentCallbacks{subagent: sa}
	res := cb.executeGoalTool(map[string]interface{}{
		"action":        "plan",
		"understanding": "目标本身没有失败。上一轮执行失败，日志里有 error: unavailable，只是被复核。",
		"steps": []interface{}{
			map[string]interface{}{"title": "定位热点"},
		},
		"acceptance_criteria": []interface{}{"性能提升20%"},
	})
	if res.Outcome != codingToolOutcomeSuccess {
		t.Fatalf("saved plan was reported as %s: %s", res.Outcome, res.Text)
	}
	if !strings.HasPrefix(res.Text, "计划已保存") {
		t.Fatalf("text = %q", res.Text)
	}
	if goalToolCallFailed(res.Text) {
		t.Fatalf("goal success classified as failure: %q", res.Text)
	}
	stored := h.getGoalStore().Get(userID)
	if stored == nil || stored.Status != goal.StatusActive || len(stored.Steps) != 1 {
		t.Fatalf("plan was not stored: %+v", stored)
	}

	refused := cb.executeGoalTool(map[string]interface{}{
		"action":  "complete",
		"summary": "做完了",
	})
	if refused.Outcome != codingToolOutcomeFailed || !strings.Contains(refused.Text, "不能完成目标") {
		t.Fatalf("refused complete = %s %q", refused.Outcome, refused.Text)
	}
	if got := h.getGoalStore().Get(userID); got == nil || got.Status != goal.StatusActive {
		t.Fatalf("refused complete changed status: %+v", got)
	}
	if remoteCodingExecutionOutcome("goal", res.Text) != "success" {
		t.Fatalf("remote plan outcome = failed for %q", res.Text)
	}
	if shared := classifySharedLoopToolResult("goal", res.Text); shared.Outcome != agent.ToolExecutionOutcomeOK {
		t.Fatalf("shared loop reclassified plan: %s", shared.Outcome)
	}
	if rejected := classifySharedLoopToolResult("goal", "[system rejected] tool not permitted"); rejected.Outcome != agent.ToolExecutionOutcomeError {
		t.Fatalf("shared loop hid a goal rejection: %s", rejected.Outcome)
	}
	quoted := res.Text + "\nAn approval panel has been opened"
	if quotedGoal := classifySharedLoopToolResult("goal", quoted); quotedGoal.Outcome != agent.ToolExecutionOutcomeOK {
		t.Fatalf("shared loop treated a quoted approval sentence as a failed plan: %s", quotedGoal.Outcome)
	}
	if quotedOther := classifySharedLoopToolResult("database_query", quoted); quotedOther.Outcome != agent.ToolExecutionOutcomeError {
		t.Fatalf("approval sentence = %s", quotedOther.Outcome)
	}
	if generic := classifySharedLoopToolResult("bash", res.Text); generic.Outcome == agent.ToolExecutionOutcomeOK {
		t.Fatal("generic classifier should still flag 执行失败 inside a plan body")
	}
	if remoteCodingExecutionOutcome("goal", "goal unavailable: remote coding session owner is missing") != "failed" {
		t.Fatal("missing goal session owner was reported as success")
	}

	resumeActive := cb.executeGoalTool(map[string]interface{}{"action": "resume"})
	if resumeActive.Outcome != codingToolOutcomeFailed || !strings.Contains(resumeActive.Text, "目标不在暂停状态") {
		t.Fatalf("resume of active = %s %q", resumeActive.Outcome, resumeActive.Text)
	}
	paused := cb.executeGoalTool(map[string]interface{}{"action": "pause"})
	if paused.Outcome != codingToolOutcomeSuccess || !strings.HasPrefix(paused.Text, "目标已暂停") {
		t.Fatalf("pause = %s %q", paused.Outcome, paused.Text)
	}
	pauseAgain := cb.executeGoalTool(map[string]interface{}{"action": "pause"})
	if pauseAgain.Outcome != codingToolOutcomeFailed || !strings.Contains(pauseAgain.Text, "目标不在活跃状态") {
		t.Fatalf("second pause = %s %q", pauseAgain.Outcome, pauseAgain.Text)
	}
}
