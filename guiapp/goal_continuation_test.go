package guiapp

import (
	"context"
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

func TestGoalContinuationMintsCodingIngressForWorkbench(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { app.closeCodingTaskRelationService() })
	handler := &IMMessageHandler{app: app}
	app.imHandler = handler
	owner := projectSessionOwnerID("C:/workspace/goal-cont")
	handler.storeStickyCodingWorkbenchMemory(owner, stickyCodingWorkbenchMemory{Kind: "local"})
	engine := NewGoalContinuationEngine(nil, app)

	msg := IMUserMessage{UserID: owner, Platform: "goal-continuation", Text: "[系统续接] 分解目标"}
	end := engine.attachCodingIngress(&msg)
	if end == nil || msg.CodingTaskIngressToken == "" {
		t.Fatal("coding workbench continuation must mint a desktop ingress token")
	}
	_, _, handle, _, ok := app.nextDesktopCodingRootStepRelation(msg.CodingTaskIngressToken, owner)
	if !ok || !handle.complete() {
		t.Fatal("minted token did not bind a desktop relation")
	}
	end()

	plain := IMUserMessage{UserID: "desktop-user", Platform: "goal-continuation", Text: "[系统续接] 分解目标"}
	if engine.attachCodingIngress(&plain) != nil || plain.CodingTaskIngressToken != "" {
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
	if !execNames["write_file"] || !execNames["goal"] {
		t.Fatalf("execute surface = %v", execNames)
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
	engine.noteContinuationExpect("user", store.Get("user"))
	engine.MaybeScheduleContinuation("user", g.GoalID, 10, 5, true)
	got := store.Get("user")
	if got == nil || got.Status != goal.StatusFailed || !strings.Contains(got.Summary, "未提交当前步骤") {
		t.Fatalf("unreported step should fail the goal: %+v", got)
	}

	userID := "goal-step-reported"
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
