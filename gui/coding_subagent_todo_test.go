package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestCodingAgentTodoWriteReplaceAndMerge(t *testing.T) {
	var state codingAgentTodoState
	text, outcome := executeCodingAgentTodoWrite(&state, `{
		"todos": [
			{"id":"1","content":"探索代码","status":"in_progress"},
			{"id":"2","content":"实现改动","status":"pending"},
			{"id":"3","content":"验证","status":"pending"}
		],
		"merge": false
	}`, nil, nil)
	if outcome != codingToolOutcomeSuccess {
		t.Fatalf("outcome=%s text=%s", outcome, text)
	}
	if !strings.Contains(text, "… 1. 探索代码") || !strings.Contains(text, "☐ 2. 实现改动") {
		t.Fatalf("checklist=%q", text)
	}
	if !strings.Contains(text, "进度：0/3") {
		t.Fatalf("progress line missing: %q", text)
	}
	items := state.snapshot()
	if len(items) != 3 || items[0].Status != codingAgentTodoInProgress {
		t.Fatalf("items=%+v", items)
	}

	// Complete step 1, start step 2 (merge).
	text, outcome = executeCodingAgentTodoWrite(&state, `{
		"todos": [
			{"id":"1","content":"探索代码","status":"completed"},
			{"id":"2","content":"实现改动","status":"in_progress"}
		],
		"merge": true
	}`, nil, nil)
	if outcome != codingToolOutcomeSuccess {
		t.Fatalf("merge outcome=%s", outcome)
	}
	items = state.snapshot()
	if len(items) != 3 {
		t.Fatalf("merge should keep step 3, got %d", len(items))
	}
	if items[0].Status != codingAgentTodoCompleted || items[1].Status != codingAgentTodoInProgress || items[2].Status != codingAgentTodoPending {
		t.Fatalf("after merge: %+v", items)
	}
	if !strings.Contains(text, "☑ 1. 探索代码") || !strings.Contains(text, "… 2. 实现改动") {
		t.Fatalf("merged checklist=%q", text)
	}
	if !strings.Contains(text, "进度：1/3") {
		t.Fatalf("progress after one done: %q", text)
	}
}

func TestCodingAgentTodoWriteOnlyOneInProgress(t *testing.T) {
	var state codingAgentTodoState
	_, outcome := executeCodingAgentTodoWrite(&state, `{
		"todos": [
			{"id":"1","content":"A","status":"in_progress"},
			{"id":"2","content":"B","status":"in_progress"}
		]
	}`, nil, nil)
	if outcome != codingToolOutcomeSuccess {
		t.Fatal(outcome)
	}
	items := state.snapshot()
	if items[0].Status != codingAgentTodoPending || items[1].Status != codingAgentTodoInProgress {
		t.Fatalf("only last in_progress should remain: %+v", items)
	}
}

func TestCodingAgentTodoWriteNumericIDsAndAliases(t *testing.T) {
	var state codingAgentTodoState
	// Numeric ids + title alias + tasks key (common model variants).
	text, outcome := executeCodingAgentTodoWrite(&state, `{
		"tasks": [
			{"id": 1, "title": "Explore", "status": "running"},
			{"id": 2, "description": "Implement", "state": "pending"}
		]
	}`, nil, nil)
	if outcome != codingToolOutcomeSuccess {
		t.Fatalf("outcome=%s text=%s", outcome, text)
	}
	items := state.snapshot()
	if len(items) != 2 || items[0].ID != "1" || items[0].Content != "Explore" {
		t.Fatalf("items=%+v", items)
	}
	if items[0].Status != codingAgentTodoInProgress {
		t.Fatalf("running alias -> in_progress, got %s", items[0].Status)
	}

	// Status-only merge with numeric id (preserve content).
	text, outcome = executeCodingAgentTodoWrite(&state, `{
		"todos": [{"id": 1, "status": "done"}, {"id": 2, "status": "in_progress"}],
		"merge": true
	}`, nil, nil)
	if outcome != codingToolOutcomeSuccess {
		t.Fatalf("status merge: %s %s", outcome, text)
	}
	items = state.snapshot()
	if items[0].Content != "Explore" || items[0].Status != codingAgentTodoCompleted {
		t.Fatalf("content should be preserved: %+v", items)
	}
	if items[1].Status != codingAgentTodoInProgress || items[1].Content != "Implement" {
		t.Fatalf("step2=%+v", items[1])
	}
}

func TestCodingAgentTodoWriteDedupeAndCap(t *testing.T) {
	var state codingAgentTodoState
	// Duplicate ids — last wins.
	_, outcome := executeCodingAgentTodoWrite(&state, `{
		"todos": [
			{"id":"1","content":"old","status":"pending"},
			{"id":"1","content":"new","status":"in_progress"}
		]
	}`, nil, nil)
	if outcome != codingToolOutcomeSuccess {
		t.Fatal(outcome)
	}
	items := state.snapshot()
	if len(items) != 1 || items[0].Content != "new" || items[0].Status != codingAgentTodoInProgress {
		t.Fatalf("dedupe: %+v", items)
	}

	// Cap at codingAgentTodoMaxItems.
	var many []string
	for i := 1; i <= codingAgentTodoMaxItems+5; i++ {
		many = append(many, fmt.Sprintf(`{"id":"%d","content":"s%d","status":"pending"}`, i, i))
	}
	payload := `{"todos":[` + strings.Join(many, ",") + `],"merge":false}`
	_, outcome = executeCodingAgentTodoWrite(&state, payload, nil, nil)
	if outcome != codingToolOutcomeSuccess {
		t.Fatal(outcome)
	}
	if got := len(state.snapshot()); got != codingAgentTodoMaxItems {
		t.Fatalf("cap=%d want %d", got, codingAgentTodoMaxItems)
	}
}

func TestCodingAgentTodoWriteEmptyGuard(t *testing.T) {
	var state codingAgentTodoState
	// Seed
	_, _ = executeCodingAgentTodoWrite(&state, `{"todos":[{"id":"1","content":"x","status":"pending"}]}`, nil, nil)

	// Empty without merge=false should fail (avoid wipe).
	text, outcome := executeCodingAgentTodoWrite(&state, `{"todos":[]}`, nil, nil)
	if outcome == codingToolOutcomeSuccess {
		t.Fatalf("empty should fail, got %s", text)
	}
	if len(state.snapshot()) != 1 {
		t.Fatal("state should be unchanged")
	}

	// Empty objects are not valid steps.
	text, outcome = executeCodingAgentTodoWrite(&state, `{"todos":[{},{"content":""}]}`, nil, nil)
	if outcome == codingToolOutcomeSuccess {
		t.Fatalf("empty objects should fail, got %s", text)
	}
	if len(state.snapshot()) != 1 {
		t.Fatal("state should stay after empty-object write")
	}

	// Explicit clear.
	text, outcome = executeCodingAgentTodoWrite(&state, `{"todos":[],"merge":false}`, nil, nil)
	if outcome != codingToolOutcomeSuccess {
		t.Fatalf("clear: %s %s", outcome, text)
	}
	if len(state.snapshot()) != 0 {
		t.Fatalf("cleared state=%+v", state.snapshot())
	}
}

func TestCodingAgentTodosToStepStatuses(t *testing.T) {
	steps := codingAgentTodosToStepStatuses([]codingAgentTodoItem{
		{ID: "1", Content: "a", Status: codingAgentTodoCompleted},
		{ID: "2", Content: "b", Status: codingAgentTodoInProgress},
		{ID: "3", Content: "c", Status: codingAgentTodoPending},
		{ID: "4", Content: "d", Status: codingAgentTodoCancelled},
	})
	if len(steps) != 4 {
		t.Fatalf("len=%d", len(steps))
	}
	if steps[0].Status != codingStepPassed || steps[1].Status != codingStepRunning ||
		steps[2].Status != codingStepPending || steps[3].Status != codingStepSkipped {
		t.Fatalf("%+v", steps)
	}
}

func TestPublishCodingAgentTodosDoesNotOverwriteOrchestratedPlan(t *testing.T) {
	userID := "desktop-user:todo-plan-guard"
	h := &IMMessageHandler{}
	h.clearStickyCodingWorkbenchMemory(userID)
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	planSteps := []codingWorkbenchStepStatus{
		{Index: 1, Title: "检查远程环境", Status: codingStepRunning},
		{Index: 2, Title: "初始化项目", Status: codingStepPending},
		{Index: 3, Title: "实现功能", Status: codingStepPending},
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.ExecutionPlan = "### T1: 检查\n### T2: 初始化\n### T3: 实现"
		mem.StepStatuses = append([]codingWorkbenchStepStatus(nil), planSteps...)
	})
	if !stickyHasOrchestratedPlanSteps(h.getStickyCodingWorkbenchMemory(userID)) {
		t.Fatal("expected orchestrated plan detection")
	}

	// Agent re-lists whole plan and marks everything done (the rush-ahead bug).
	publishCodingAgentTodosToUI(h, userID, []codingAgentTodoItem{
		{ID: "1", Content: "检查远程环境", Status: codingAgentTodoCompleted},
		{ID: "2", Content: "初始化项目", Status: codingAgentTodoCompleted},
		{ID: "3", Content: "实现功能", Status: codingAgentTodoCompleted},
	})

	mem := h.getStickyCodingWorkbenchMemory(userID)
	if len(mem.StepStatuses) != 3 {
		t.Fatalf("plan steps replaced: %+v", mem.StepStatuses)
	}
	if mem.StepStatuses[0].Status != codingStepRunning || mem.StepStatuses[0].Title != "检查远程环境" {
		t.Fatalf("T1 should stay orchestrator-owned running: %+v", mem.StepStatuses[0])
	}
	if mem.StepStatuses[1].Status != codingStepPending || mem.StepStatuses[2].Status != codingStepPending {
		t.Fatalf("later plan steps must not be agent-passed: %+v", mem.StepStatuses)
	}
}

func TestPublishCodingAgentTodosStillMirrorsWithoutPlan(t *testing.T) {
	userID := "desktop-user:todo-no-plan"
	h := &IMMessageHandler{}
	h.clearStickyCodingWorkbenchMemory(userID)
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	publishCodingAgentTodosToUI(h, userID, []codingAgentTodoItem{
		{ID: "1", Content: "探查", Status: codingAgentTodoInProgress},
		{ID: "2", Content: "改码", Status: codingAgentTodoPending},
	})
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if len(mem.StepStatuses) != 2 || mem.StepStatuses[0].Status != codingStepRunning {
		t.Fatalf("without plan, todos should mirror to UI: %+v", mem.StepStatuses)
	}
}

func TestPublishCodingAgentTodosMirrorsAfterTerminalPlan(t *testing.T) {
	userID := "desktop-user:todo-after-plan"
	h := &IMMessageHandler{}
	h.clearStickyCodingWorkbenchMemory(userID)
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	// Plan finished: all steps terminal — free-form follow-up todos should own UI again.
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.ExecutionPlan = "### T1\n### T2\n### T3"
		mem.StepStatuses = []codingWorkbenchStepStatus{
			{Index: 1, Title: "检查", Status: codingStepPassed},
			{Index: 2, Title: "实现", Status: codingStepPassed},
			{Index: 3, Title: "验收", Status: codingStepPassed},
		}
	})
	if stickyHasOrchestratedPlanSteps(h.getStickyCodingWorkbenchMemory(userID)) {
		t.Fatal("terminal plan must not freeze todo UI")
	}
	publishCodingAgentTodosToUI(h, userID, []codingAgentTodoItem{
		{ID: "1", Content: "补一个 typo", Status: codingAgentTodoInProgress},
	})
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if len(mem.StepStatuses) != 1 || mem.StepStatuses[0].Title != "补一个 typo" {
		t.Fatalf("after terminal plan, todos should mirror: %+v", mem.StepStatuses)
	}
}

func TestStickyPlanHasOpenSteps(t *testing.T) {
	if stickyPlanHasOpenSteps(nil) {
		t.Fatal("nil should be closed")
	}
	if !stickyPlanHasOpenSteps([]codingWorkbenchStepStatus{{Status: codingStepPending}}) {
		t.Fatal("pending is open")
	}
	if stickyPlanHasOpenSteps([]codingWorkbenchStepStatus{
		{Status: codingStepPassed},
		{Status: codingStepFailed},
		{Status: codingStepSkipped},
		{Status: codingStepVerifyFail},
	}) {
		t.Fatal("all terminal should be closed")
	}
}

func TestAnnotateTodoChecklistForOrchestratedPlan(t *testing.T) {
	userID := "desktop-user:todo-annotate"
	h := &IMMessageHandler{}
	// Sticky memory cold-loads from disk; isolate this test user fully.
	h.clearStickyCodingWorkbenchMemory(userID)
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	// No plan: unchanged.
	base := "执行步骤：\n☑ 1. a\n… 2. b"
	if got := annotateTodoChecklistForOrchestratedPlan(h, userID, base); got != base {
		t.Fatalf("no plan should leave checklist alone: %q", got)
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.ExecutionPlan = "T1\nT2"
		mem.StepStatuses = []codingWorkbenchStepStatus{
			{Index: 1, Title: "T1", Status: codingStepRunning},
			{Index: 2, Title: "T2", Status: codingStepPending},
		}
	})
	got := annotateTodoChecklistForOrchestratedPlan(h, userID, base)
	if !strings.Contains(got, base) || !strings.Contains(got, "外层多步计划进度由编排器维护") {
		t.Fatalf("expected plan annotation: %q", got)
	}
	// Idempotent.
	got2 := annotateTodoChecklistForOrchestratedPlan(h, userID, got)
	if got2 != got {
		t.Fatalf("annotate should be idempotent:\n%s\nvs\n%s", got, got2)
	}
}

func TestWrapTodoProgressForOrchestratedPlan(t *testing.T) {
	userID := "desktop-user:todo-progress-wrap"
	h := &IMMessageHandler{}
	h.clearStickyCodingWorkbenchMemory(userID)
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })

	var got string
	wrap := wrapTodoProgressForOrchestratedPlan(h, userID, func(s string) { got = s })
	wrap("完成 2/2 · 全部完成")
	if got != "完成 2/2 · 全部完成" {
		t.Fatalf("without plan, progress should be unchanged: %q", got)
	}

	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.ExecutionPlan = "T1\nT2"
		mem.StepStatuses = []codingWorkbenchStepStatus{
			{Index: 1, Status: codingStepRunning},
			{Index: 2, Status: codingStepPending},
		}
	})
	wrap("完成 2/2 · 全部完成")
	if !strings.Contains(got, "本步内部清单已勾完") || !strings.Contains(got, "外层计划另计") {
		t.Fatalf("with open plan, progress should be softened: %q", got)
	}
	if strings.Contains(got, " · 全部完成") {
		t.Fatalf("whole-plan completion wording should be rewritten: %q", got)
	}
}

func TestLocalCodingSubAgentTodoWriteTool(t *testing.T) {
	userID := "desktop-user:todo-local-test"
	h := &IMMessageHandler{}
	h.clearStickyCodingWorkbenchMemory(userID)
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{
			handler:    h,
			loopCtx:    &LoopContext{UserID: userID},
			onProgress: func(string) {},
		},
	}
	res := cb.executeTodoWrite(`{"todos":[{"id":"1","content":"读代码","status":"in_progress"},{"id":"2","content":"改代码","status":"pending"}]}`)
	if res.Outcome != codingToolOutcomeSuccess {
		t.Fatalf("%+v", res)
	}
	if !strings.Contains(res.Text, "读代码") {
		t.Fatalf("text=%q", res.Text)
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if len(mem.StepStatuses) != 2 {
		t.Fatalf("UI steps=%+v", mem.StepStatuses)
	}
	if mem.StepStatuses[0].Status != codingStepRunning || mem.StepStatuses[1].Status != codingStepPending {
		t.Fatalf("UI statuses=%+v", mem.StepStatuses)
	}
	// Also via ExecuteToolStructured switch.
	out := cb.ExecuteToolStructured(codingAgentTodoToolName, `{"todos":[{"id":"1","status":"completed"},{"id":"2","status":"in_progress"}],"merge":true}`)
	if out.Outcome != agent.ToolExecutionOutcomeOK {
		t.Fatalf("structured outcome=%v result=%q", out.Outcome, out.Result)
	}
	if !strings.Contains(out.Result, "☑") {
		t.Fatalf("structured result=%q", out.Result)
	}
	// Content preserved after status-only merge.
	mem = h.getStickyCodingWorkbenchMemory(userID)
	if mem.StepStatuses[0].Title != "读代码" || mem.StepStatuses[0].Status != codingStepPassed {
		t.Fatalf("UI after merge=%+v", mem.StepStatuses)
	}
}

func TestRemoteCodingSubAgentTodoWriteTool(t *testing.T) {
	userID := "desktop-user:todo-remote-test"
	h := &IMMessageHandler{}
	h.clearStickyCodingWorkbenchMemory(userID)
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	cb := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{
			handler:    h,
			loopCtx:    &LoopContext{UserID: userID},
			onProgress: func(string) {},
		},
	}
	// Tool must be present for workers.
	tools := cb.BuildTools("implement feature")
	found := false
	for _, tool := range tools {
		fn, _ := tool["function"].(map[string]interface{})
		if name, _ := fn["name"].(string); name == codingAgentTodoToolName {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("todo_write missing from remote BuildTools")
	}
	// Prompt should mention todo_write.
	prompt := cb.BuildSystemPrompt("task", true)
	if !strings.Contains(prompt, "todo_write") {
		t.Fatalf("remote prompt missing todo section: %s", truncateRunesV2(prompt, 200))
	}

	result := cb.executeRemoteTodoWrite(`{"todos":[
		{"id":"1","content":"远程探查","status":"completed"},
		{"id":"2","content":"远程改码","status":"in_progress"},
		{"id":"3","content":"远程验证","status":"pending"}
	]}`)
	if !strings.Contains(result, "☑ 1. 远程探查") || !strings.Contains(result, "… 2. 远程改码") {
		t.Fatalf("result=%q", result)
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if len(mem.StepStatuses) != 3 || mem.StepStatuses[1].Status != codingStepRunning {
		t.Fatalf("remote UI steps=%+v", mem.StepStatuses)
	}
}

func TestLocalCodingSystemPromptIncludesTodoSection(t *testing.T) {
	prompt := buildCodingSubAgentSystemPrompt(&TaskItem{
		Index: 1, Title: "实现登录", Description: "实现用户登录并加测试",
	}, "/tmp/proj", "", "", nil)
	if !strings.Contains(prompt, "todo_write") {
		t.Fatalf("local prompt missing todo section")
	}
}

func TestCodingAgentTodoWriteProgressCallback(t *testing.T) {
	var state codingAgentTodoState
	var progressed string
	text, outcome := executeCodingAgentTodoWrite(&state, `{"todos":[{"id":"1","content":"A","status":"completed"}]}`, func(s string) {
		progressed = s
	}, nil)
	if outcome != codingToolOutcomeSuccess {
		t.Fatal(outcome)
	}
	// Tool result keeps full checklist; onProgress is a one-liner.
	if !strings.Contains(text, "☑ 1. A") {
		t.Fatalf("result checklist=%q", text)
	}
	if !strings.Contains(progressed, "完成 1/1") || !strings.Contains(progressed, "全部完成") {
		t.Fatalf("progress line=%q", progressed)
	}
}

func TestCodingAgentTodoChineseStatusAliases(t *testing.T) {
	var state codingAgentTodoState
	_, outcome := executeCodingAgentTodoWrite(&state, `{
		"todos": [
			{"id":"1","content":"A","status":"进行中"},
			{"id":"2","content":"B","status":"待办"}
		]
	}`, nil, nil)
	if outcome != codingToolOutcomeSuccess {
		t.Fatal(outcome)
	}
	items := state.snapshot()
	if items[0].Status != codingAgentTodoInProgress || items[1].Status != codingAgentTodoPending {
		t.Fatalf("%+v", items)
	}
	_, outcome = executeCodingAgentTodoWrite(&state, `{
		"todos": [{"id":"1","status":"完成"},{"id":"2","status":"进行中"}],
		"merge": true
	}`, nil, nil)
	if outcome != codingToolOutcomeSuccess {
		t.Fatal(outcome)
	}
	items = state.snapshot()
	if items[0].Status != codingAgentTodoCompleted || items[1].Status != codingAgentTodoInProgress {
		t.Fatalf("after CN status merge: %+v", items)
	}
}

func TestPublishCodingAgentTodosSkipsUnchangedEmit(t *testing.T) {
	userID := "desktop-user:todo-noop-emit"
	h := &IMMessageHandler{}
	items := []codingAgentTodoItem{{ID: "1", Content: "x", Status: codingAgentTodoInProgress}}
	publishCodingAgentTodosToUI(h, userID, items)
	// Second identical publish must not clear state and should be a no-op update.
	publishCodingAgentTodosToUI(h, userID, items)
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if len(mem.StepStatuses) != 1 || mem.StepStatuses[0].Title != "x" {
		t.Fatalf("%+v", mem.StepStatuses)
	}
}

func TestAppendCodingAgentTodoTurnNote(t *testing.T) {
	items := []codingAgentTodoItem{
		{ID: "1", Content: "A", Status: codingAgentTodoCompleted},
		{ID: "2", Content: "B", Status: codingAgentTodoPending},
	}
	got := appendCodingAgentTodoTurnNote("done work", items)
	if !strings.Contains(got, "done work") || !strings.Contains(got, "进度：1/2") || !strings.Contains(got, "未勾选步骤") {
		t.Fatalf("%q", got)
	}
	// No double-append.
	got2 := appendCodingAgentTodoTurnNote(got, items)
	if strings.Count(got2, "## 步骤清单") != 1 {
		t.Fatalf("double note: %q", got2)
	}
}

func TestLocalBuildToolsIncludesTodoWrite(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{fullEnvironment: true},
	}
	tools := cb.BuildTools("implement multi-file feature")
	found := false
	for _, tool := range tools {
		fn, _ := tool["function"].(map[string]interface{})
		if name, _ := fn["name"].(string); name == codingAgentTodoToolName {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("local BuildTools missing todo_write")
	}
}

func TestPublishCodingAgentTodosDebounced(t *testing.T) {
	userID := "desktop-user:todo-debounce"
	h := &IMMessageHandler{}
	publishCodingAgentTodosToUI(h, userID, []codingAgentTodoItem{
		{ID: "1", Content: "x", Status: codingAgentTodoInProgress},
	})
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if len(mem.StepStatuses) != 1 || mem.StepStatuses[0].Status != codingStepRunning {
		t.Fatalf("%+v", mem.StepStatuses)
	}
	publishCodingAgentTodosToUI(h, userID, nil)
	mem = h.getStickyCodingWorkbenchMemory(userID)
	if len(mem.StepStatuses) != 0 {
		t.Fatalf("clear: %+v", mem.StepStatuses)
	}
}
