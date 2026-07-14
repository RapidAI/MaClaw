package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/goal"
)

func TestClassifyImmediateIMCommandGoalCaseInsensitive(t *testing.T) {
	if got := classifyImmediateIMCommand("/Goal implement X"); got != imCommandGoal {
		t.Fatalf("got %v want imCommandGoal", got)
	}
	if got := classifyImmediateIMCommand("/GOAL"); got != imCommandGoal {
		t.Fatalf("bare /GOAL: got %v", got)
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
	if resp == nil || resp.Error != "" {
		t.Fatalf("create resp=%+v", resp)
	}
	if !strings.Contains(resp.Text, "SSH") && !strings.Contains(resp.Text, "重连") {
		t.Fatalf("expected reconnect guidance: %q", resp.Text)
	}
	if h.hasPendingTemplateSubAgentExecution(userID) {
		t.Fatal("dead remote should not be armed")
	}
	g := h.getGoalStore().Get(userID)
	if g == nil || g.Objective != "ship feature" {
		t.Fatalf("goal should still be stored: %+v", g)
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
	if !strings.Contains(create.Text, "目标已创建") {
		t.Fatalf("create text = %q", create.Text)
	}
	if !strings.Contains(create.Text, "编程工作台") {
		t.Fatalf("pure coding create should mention workbench binding: %q", create.Text)
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
	// Root full-env tools include goal.
	tools := cb.BuildTools("test")
	found := false
	for _, tool := range tools {
		fn, _ := tool["function"].(map[string]interface{})
		if fn["name"] == "goal" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("full env BuildTools should include goal")
	}
}
