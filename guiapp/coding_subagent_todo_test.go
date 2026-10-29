package guiapp

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
	if items[0].Status != codingAgentTodoInProgress || items[1].Status != codingAgentTodoPending {
		t.Fatalf("first unfinished stays in progress: %+v", items)
	}
}

func TestCodingAgentTodoWriteCannotSkipEarlierPending(t *testing.T) {
	var state codingAgentTodoState
	_, outcome := executeCodingAgentTodoWrite(&state, `{
		"todos": [
			{"id":"1","content":"probe","status":"completed"},
			{"id":"2","content":"build","status":"completed"},
			{"id":"3","content":"tests","status":"completed"},
			{"id":"4","content":"run probe","status":"pending"},
			{"id":"5","content":"git status","status":"pending"},
			{"id":"6","content":"fix renderer","status":"in_progress"}
		]
	}`, nil, nil)
	if outcome != codingToolOutcomeSuccess {
		t.Fatal(outcome)
	}
	items := state.snapshot()
	if items[3].Status != codingAgentTodoInProgress || items[3].Content != "run probe" {
		t.Fatalf("cursor should be the first unfinished item: %+v", items[3])
	}
	if items[5].Status != codingAgentTodoPending {
		t.Fatalf("later in_progress was not rewound: %+v", items[5])
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

func TestCodingAgentTodoWriteControlPlaneCASRejectsStaleReplacement(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{projectPath: t.TempDir()},
		task:     &TaskItem{Title: "implement feature", Description: "change code"},
	}
	firstDefinitions := cb.BuildToolsForModelRequest("implement feature", 0)
	assertCodingTodoDefinitionTokenForTest(t, firstDefinitions, 1, 0)
	first := cb.executeTodoWrite(`{
		"expected_revision":1,
		"expected_version":0,
		"merge":false,
		"todos":[{"id":"1","content":"inspect","status":"in_progress"}]
	}`)
	if first.Outcome != codingToolOutcomeSuccess || !strings.Contains(first.Text, "control_plane_revision=1 control_plane_version=1") {
		t.Fatalf("first todo result=%#v", first)
	}
	before := cb.todos.controlPlaneSnapshot()
	sameDefinitions := cb.BuildToolsForModelRequest("implement feature again", 1)
	afterSame := cb.todos.controlPlaneSnapshot()
	if afterSame.Revision != before.Revision || afterSame.Version != before.Version {
		t.Fatalf("identical surface re-render must keep CAS tokens, before=%+v after=%+v", before, afterSame)
	}
	assertCodingTodoDefinitionTokenForTest(t, sameDefinitions, before.Revision, before.Version)
	if rev := cb.setStaticCompatibilitySurface([]map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": codingAgentTodoToolName}},
	}); rev != 2 {
		t.Fatalf("name-set replacement revision=%d", rev)
	}
	afterReplacement := cb.todos.controlPlaneSnapshot()
	if afterReplacement.Revision != 2 || afterReplacement.Version != before.Version+1 {
		t.Fatalf("replacement must advance CAS generation, before=%+v after=%+v", before, afterReplacement)
	}
	stale := cb.executeTodoWrite(`{
		"expected_revision":1,
		"expected_version":1,
		"merge":false,
		"todos":[{"id":"1","content":"stale overwrite","status":"completed"}]
	}`)
	if stale.Outcome == codingToolOutcomeSuccess || !strings.Contains(stale.Text, "control_plane_stale") {
		t.Fatalf("stale replacement must fail closed, got %#v", stale)
	}
	afterStale := cb.todos.controlPlaneSnapshot()
	if afterStale.Revision != afterReplacement.Revision || afterStale.Version != afterReplacement.Version || len(afterStale.Items) != 1 || afterStale.Items[0].Content != "inspect" {
		t.Fatalf("stale mutation changed checklist: before=%+v after=%+v", afterReplacement, afterStale)
	}
	current := cb.executeTodoWrite(`{
		"expected_revision":2,
		"expected_version":2,
		"merge":true,
		"todos":[{"id":"1","status":"completed"}]
	}`)
	if current.Outcome != codingToolOutcomeSuccess || !strings.Contains(current.Text, "control_plane_revision=2 control_plane_version=3") {
		t.Fatalf("current CAS write=%#v", current)
	}
}

func assertCodingTodoDefinitionTokenForTest(t *testing.T, definitions []map[string]interface{}, revision, version uint64) {
	t.Helper()
	for _, definition := range definitions {
		fn, _ := definition["function"].(map[string]interface{})
		if fn == nil || fn["name"] != codingAgentTodoToolName {
			continue
		}
		params, _ := fn["parameters"].(map[string]interface{})
		properties, _ := params["properties"].(map[string]interface{})
		revisionDef, _ := properties["expected_revision"].(map[string]interface{})
		versionDef, _ := properties["expected_version"].(map[string]interface{})
		if !strings.Contains(fmt.Sprint(revisionDef["description"]), fmt.Sprint(revision)) || !strings.Contains(fmt.Sprint(versionDef["description"]), fmt.Sprint(version)) {
			t.Fatalf("todo definition has wrong control-plane tokens: revision=%#v version=%#v", revisionDef, versionDef)
		}
		return
	}
	t.Fatal("todo definition missing from rendered surface")
}

func TestRemoteCodingTodoWriteControlPlaneCASRejectsStaleReplacement(t *testing.T) {
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{}, task: "implement feature"}
	_ = cb.BuildToolsForModelRequest("implement feature", 0)
	if got := cb.executeRemoteTodoWrite(`{
		"expected_revision":1,"expected_version":0,"merge":false,
		"todos":[{"id":"1","content":"inspect","status":"in_progress"}]
	}`); !strings.Contains(got, "control_plane_revision=1 control_plane_version=1") {
		t.Fatalf("first remote todo=%q", got)
	}
	_ = cb.BuildToolsForModelRequest("implement feature again", 1)
	if cb.currentControlPlaneRevision() != 1 {
		t.Fatalf("identical remote surface must keep revision, got %d", cb.currentControlPlaneRevision())
	}
	if rev := cb.setStaticCompatibilitySurface([]map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": codingAgentTodoToolName}},
	}); rev != 2 {
		t.Fatalf("remote name-set replacement revision=%d", rev)
	}
	stale := cb.executeRemoteTodoWrite(`{
		"expected_revision":1,"expected_version":1,"merge":false,
		"todos":[{"id":"1","content":"stale overwrite","status":"completed"}]
	}`)
	if !strings.Contains(stale, "control_plane_stale") {
		t.Fatalf("stale remote replacement must fail closed, got %q", stale)
	}
	snapshot := cb.todos.controlPlaneSnapshot()
	if snapshot.Revision != 2 || snapshot.Version != 2 || len(snapshot.Items) != 1 || snapshot.Items[0].Content != "inspect" {
		t.Fatalf("remote stale mutation changed checklist: %+v", snapshot)
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

func TestPublishCodingAgentTodosKeepsTheStepError(t *testing.T) {
	userID := "desktop-user:todo-keep-note"
	h := &IMMessageHandler{}
	h.clearStickyCodingWorkbenchMemory(userID)
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	h.setStickyCodingStepStatuses(userID, []codingWorkbenchStepStatus{
		{Index: 1, Title: "运行 probe", Status: codingStepPending},
		{Index: 2, Title: "修复渲染器", Status: codingStepRunning, Summary: "error C3861: 'spawnFood': identifier not found"},
	})
	publishCodingAgentTodosToUI(h, userID, []codingAgentTodoItem{
		{ID: "1", Content: "运行 probe", Status: codingAgentTodoInProgress},
		{ID: "2", Content: "修复渲染器", Status: codingAgentTodoPending},
	})
	got := h.getStickyCodingWorkbenchMemory(userID).StepStatuses
	if len(got) != 2 || !strings.Contains(got[1].Summary, "C3861") {
		t.Fatalf("todo mirror dropped the stored error: %+v", got)
	}
	publishCodingAgentTodosToUI(h, userID, []codingAgentTodoItem{
		{ID: "1", Content: "运行 probe", Status: codingAgentTodoCompleted},
		{ID: "2", Content: "修复渲染器", Status: codingAgentTodoCompleted},
	})
	done := h.getStickyCodingWorkbenchMemory(userID).StepStatuses
	if len(done) != 2 || done[1].Status != codingStepPassed || done[1].Summary != "" {
		t.Fatalf("passed step should drop the old error: %+v", done)
	}
}

func TestPublishCodingAgentTodosKeepsErrorOnATruncatedTitle(t *testing.T) {
	userID := "desktop-user:todo-keep-long-note"
	h := &IMMessageHandler{}
	h.clearStickyCodingWorkbenchMemory(userID)
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	long := strings.Repeat("修", codingAgentTodoMaxContent+10)
	h.setStickyCodingStepStatuses(userID, []codingWorkbenchStepStatus{
		{Index: 1, Title: "运行 probe", Status: codingStepPending},
		{Index: 2, Title: long, Status: codingStepRunning, Summary: "error C3861: identifier not found"},
	})
	publishCodingAgentTodosToUI(h, userID, []codingAgentTodoItem{
		{ID: "1", Content: "运行 probe", Status: codingAgentTodoInProgress},
		{ID: "2", Content: truncateCodingAgentTodoContent(long), Status: codingAgentTodoPending},
	})
	got := h.getStickyCodingWorkbenchMemory(userID).StepStatuses
	if len(got) != 2 || !strings.Contains(got[1].Summary, "C3861") {
		t.Fatalf("truncated title dropped the stored error: %+v", got)
	}
}

func TestTodoWriteMergeAfterRestartKeepsTheChecklist(t *testing.T) {
	userID := "desktop-user:todo-merge-restart"
	h := &IMMessageHandler{}
	h.clearStickyCodingWorkbenchMemory(userID)
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	// Disk order is not list order. The cursor is the later running row.
	h.setStickyCodingStepStatuses(userID, []codingWorkbenchStepStatus{
		{Index: 6, Title: "修复渲染器", Status: codingStepRunning, Summary: "error C3861: 'spawnFood': identifier not found"},
		{Index: 1, Title: "确认现状", Status: codingStepPassed},
		{Index: 2, Title: "构建", Status: codingStepPassed},
		{Index: 3, Title: "测试", Status: codingStepPassed},
		{Index: 4, Title: "运行 probe", Status: codingStepPending, Summary: "probe exited 1"},
		{Index: 5, Title: "记录结果", Status: codingStepPending},
	})
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{
			handler:    h,
			loopCtx:    &LoopContext{UserID: userID},
			onProgress: func(string) {},
		},
	}
	res := cb.executeTodoWrite(`{"merge":true,"todos":[{"id":"T6","status":"in_progress"}]}`)
	if res.Outcome != codingToolOutcomeSuccess {
		t.Fatalf("%+v", res)
	}
	got := h.getStickyCodingWorkbenchMemory(userID).StepStatuses
	if len(got) != 6 {
		t.Fatalf("merge replaced the checklist: %+v", got)
	}
	if got[3].Index != 4 || got[3].Status != codingStepRunning || got[3].Title != "运行 probe" {
		t.Fatalf("cursor = %+v, want T4 running", got[3])
	}
	if !strings.Contains(got[3].Summary, "probe exited 1") {
		t.Fatalf("T4 note dropped: %+v", got[3])
	}
	if got[5].Index != 6 || got[5].Status != codingStepPending || !strings.Contains(got[5].Summary, "C3861") {
		t.Fatalf("T6 = %+v, want pending with the compiler note", got[5])
	}
	if got[0].Status != codingStepPassed || got[2].Status != codingStepPassed {
		t.Fatalf("passed rows changed: %+v", got[:3])
	}
}

func TestTodoWriteWithoutMergeKeepsTheStickyChecklist(t *testing.T) {
	userID := "desktop-user:todo-merge-omitted"
	h := &IMMessageHandler{}
	h.clearStickyCodingWorkbenchMemory(userID)
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	h.setStickyCodingStepStatuses(userID, []codingWorkbenchStepStatus{
		{Index: 1, Title: "旧步骤", Status: codingStepRunning},
		{Index: 2, Title: "下一步", Status: codingStepPending},
	})
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{
			handler:    h,
			loopCtx:    &LoopContext{UserID: userID},
			onProgress: func(string) {},
		},
	}
	res := cb.executeTodoWrite(`{"todos":[{"id":"1","content":"新任务","status":"in_progress"}]}`)
	if res.Outcome != codingToolOutcomeSuccess {
		t.Fatalf("%+v", res)
	}
	got := h.getStickyCodingWorkbenchMemory(userID).StepStatuses
	if len(got) != 2 || got[0].Title != "新任务" || got[0].Status != codingStepRunning {
		t.Fatalf("omitted merge replaced the checklist: %+v", got)
	}
	if got[1].Index != 2 || got[1].Title != "下一步" || got[1].Status != codingStepPending {
		t.Fatalf("later row dropped: %+v", got)
	}
	if !strings.Contains(res.Text, "下一步") || !strings.Contains(res.Text, "进度：0/2") {
		t.Fatalf("tool result dropped the sticky row: %s", res.Text)
	}
}

func TestTodoWriteMergeFalseStillReplacesTheChecklist(t *testing.T) {
	userID := "desktop-user:todo-merge-false"
	h := &IMMessageHandler{}
	h.clearStickyCodingWorkbenchMemory(userID)
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	h.setStickyCodingStepStatuses(userID, []codingWorkbenchStepStatus{
		{Index: 1, Title: "旧步骤", Status: codingStepRunning},
		{Index: 2, Title: "下一步", Status: codingStepPending},
	})
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{
			handler:    h,
			loopCtx:    &LoopContext{UserID: userID},
			onProgress: func(string) {},
		},
	}
	res := cb.executeTodoWrite(`{"merge":false,"todos":[{"id":"1","content":"新任务","status":"in_progress"}]}`)
	if res.Outcome != codingToolOutcomeSuccess {
		t.Fatalf("%+v", res)
	}
	got := h.getStickyCodingWorkbenchMemory(userID).StepStatuses
	if len(got) != 1 || got[0].Title != "新任务" || got[0].Status != codingStepRunning {
		t.Fatalf("explicit merge=false should replace: %+v", got)
	}
	if strings.Contains(res.Text, "下一步") {
		t.Fatalf("replace kept the old row: %s", res.Text)
	}
}

func TestTodoWriteMergeDoesNotAdoptAnOrchestratedPlan(t *testing.T) {
	userID := "desktop-user:todo-merge-plan"
	h := &IMMessageHandler{}
	h.clearStickyCodingWorkbenchMemory(userID)
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.ExecutionPlan = "### T1: 检查\n### T2: 实现"
		mem.StepStatuses = []codingWorkbenchStepStatus{
			{Index: 1, Title: "检查远程环境", Status: codingStepRunning},
			{Index: 2, Title: "实现功能", Status: codingStepPending},
		}
	})
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{
			handler:    h,
			loopCtx:    &LoopContext{UserID: userID},
			onProgress: func(string) {},
		},
	}
	res := cb.executeTodoWrite(`{"merge":true,"todos":[{"id":"1","content":"本步内部检查","status":"in_progress"}]}`)
	if res.Outcome != codingToolOutcomeSuccess {
		t.Fatalf("%+v", res)
	}
	if strings.Contains(res.Text, "检查远程环境") || strings.Contains(res.Text, "实现功能") || !strings.Contains(res.Text, "进度：0/1") {
		t.Fatalf("agent merge adopted the orchestrator plan: %s", res.Text)
	}
	got := h.getStickyCodingWorkbenchMemory(userID).StepStatuses
	if len(got) != 2 || got[0].Status != codingStepRunning || got[0].Title != "检查远程环境" {
		t.Fatalf("orchestrator plan changed: %+v", got)
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
	if !strings.Contains(prompt, "todo_write") || !strings.Contains(prompt, "宿主会拒绝纯文字收尾") {
		t.Fatalf("local prompt missing checklist stop rule")
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
	_, outcome = executeCodingAgentTodoWrite(&state, `{
		"todos": [{"id":"2","status":"失败"}],
		"merge": true
	}`, nil, nil)
	if outcome != codingToolOutcomeSuccess {
		t.Fatal(outcome)
	}
	items = state.snapshot()
	if items[1].Status != codingAgentTodoCancelled {
		t.Fatalf("failed alias stayed open: %+v", items)
	}
	if got := openCodingAgentChecklistNudge(&state); got != "" {
		t.Fatalf("settled after failed alias: %q", got)
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

func TestOpenCodingAgentChecklistNudge(t *testing.T) {
	if got := openCodingAgentChecklistNudge(nil); got != "" {
		t.Fatalf("nil state nudge=%q", got)
	}
	var state codingAgentTodoState
	if got := openCodingAgentChecklistNudge(&state); got != "" {
		t.Fatalf("empty checklist nudge=%q", got)
	}
	items := []codingAgentTodoItem{
		{ID: "1", Content: "Survey current modern-UI implementation", Status: codingAgentTodoCompleted},
		{ID: "2", Content: "Pick the follow-up change", Status: codingAgentTodoCompleted},
		{ID: "3", Content: "Implement the footer HUD split", Status: codingAgentTodoInProgress},
		{ID: "4", Content: "Extend the visual assertion", Status: codingAgentTodoPending},
		{ID: "5", Content: "Build and run selftest", Status: codingAgentTodoPending},
	}
	local := &codingSubAgentCallbacks{}
	local.todos.applyTodoWrite(items, false)
	got := local.OpenChecklistNudge()
	goalTurn := &codingSubAgentCallbacks{task: &TaskItem{Description: "[系统续接][execute] 按步骤执行目标：游戏"}}
	goalTurn.todos.applyTodoWrite(items, false)
	if nudge := goalTurn.OpenChecklistNudge(); nudge != "" {
		t.Fatalf("goal turn nudged the local checklist: %q", nudge)
	}
	if !strings.Contains(got, "Implement the footer HUD split") || !strings.Contains(got, "id=3") || !strings.Contains(got, "definition") || strings.Contains(got, "expected_revision=") {
		t.Fatalf("nudge=%q", got)
	}
	remoteCB := &remoteCodingCallbacks{}
	remoteCB.todos.applyTodoWrite(items, false)
	if remote := remoteCB.OpenChecklistNudge(); remote != got {
		t.Fatalf("remote nudge=%q local=%q", remote, got)
	}
	local.todos.applyTodoWrite([]codingAgentTodoItem{
		{ID: "3", Status: codingAgentTodoCompleted},
		{ID: "4", Status: codingAgentTodoCompleted},
		{ID: "5", Status: codingAgentTodoCancelled},
	}, true)
	if got := local.OpenChecklistNudge(); got != "" {
		t.Fatalf("settled checklist nudge=%q", got)
	}
}

func TestCodingSubAgentStopFailureLeavesOtherHardExits(t *testing.T) {
	items := []codingAgentTodoItem{{ID: "3", Content: "Implement the footer HUD split", Status: codingAgentTodoInProgress}}
	handled, status, msg := codingSubAgentStopFailure(agent.LoopResult{
		Error:    agent.ErrChecklistUnsettled,
		HardExit: true,
		Text:     "declared done",
	}, items)
	if !handled || status != TaskExecFailed || !strings.Contains(msg, checklistUnsettledUserMessage) || !strings.Contains(msg, "Implement the footer HUD split") {
		t.Fatalf("handled=%v status=%s msg=%q", handled, status, msg)
	}
	if handled, _, _ := codingSubAgentStopFailure(agent.LoopResult{HardExit: true}, items); handled {
		t.Fatal("plain hard exit should keep its existing message")
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
