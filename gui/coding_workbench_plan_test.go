package main

import (
	"strings"
	"testing"

	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestLooksLikeComplexCodingTask(t *testing.T) {
	if looksLikeComplexCodingTask("fix typo") {
		t.Fatal("simple fix should not be complex")
	}
	if !looksLikeComplexCodingTask("实现用户登录模块，并补齐单元测试，然后做一次回归验证") {
		t.Fatal("multi-clause Chinese request should be complex")
	}
	if !looksLikeComplexCodingTask("1. explore auth\n2. implement JWT\n3. add tests\n4. document") {
		t.Fatal("numbered multi-step should be complex")
	}
	if looksLikeComplexCodingTask("[系统续接] 继续推进目标：ship billing") {
		t.Fatal("goal continuation should skip auto multi-plan")
	}
	// False positives to avoid.
	if looksLikeComplexCodingTask("合并这两个 helper 函数") {
		t.Fatal("合并 should not trigger bare-并 complexity")
	}
	if looksLikeComplexCodingTask("upgrade go 1.22 toolchain") {
		t.Fatal("version dots should not count as multi-sentence")
	}
	// Bare markdown bullets are not multi-step plans.
	if looksLikeComplexCodingTask("fix these:\n- typo in a\n- typo in b") {
		t.Fatal("bullet list alone should not be complex")
	}
	if extractUserProvidedCodingPlan("fix these:\n- typo in a\n- typo in b") != nil {
		t.Fatal("bullet list alone should not become user plan")
	}
	long := strings.Repeat("实现完整功能并验证。", 20)
	if !looksLikeComplexCodingTask(long) {
		t.Fatal("long request should be complex")
	}
}

func TestFinalizeCodingWorkbenchTasksChainsDeps(t *testing.T) {
	tasks := finalizeCodingWorkbenchTasks([]*v2.TaskItem{
		{Title: "explore", Description: "map"},
		{Title: "implement", Description: "code"},
		{Title: "verify", Description: "test"},
	}, "build auth end to end")
	if len(tasks) != 3 {
		t.Fatalf("len=%d", len(tasks))
	}
	if tasks[0].Index != 1 || tasks[1].Index != 2 || tasks[2].Index != 3 {
		t.Fatalf("indices=%d %d %d", tasks[0].Index, tasks[1].Index, tasks[2].Index)
	}
	if len(tasks[1].DependsOn) != 1 || tasks[1].DependsOn[0] != 1 {
		t.Fatalf("T2 deps=%v", tasks[1].DependsOn)
	}
	if len(tasks[2].DependsOn) != 1 || tasks[2].DependsOn[0] != 2 {
		t.Fatalf("T3 deps=%v", tasks[2].DependsOn)
	}
	if !strings.Contains(tasks[0].Description, "Overall request") {
		t.Fatalf("missing overall request footer: %q", tasks[0].Description)
	}
}

func TestStepsJSONToTasksSkipsEmptyWithoutIndexHoles(t *testing.T) {
	tasks := stepsJSONToTasks([]codingWorkbenchPlanStepJSON{
		{Title: "", Description: ""},
		{Title: "real", Description: "do it", DependsOn: []int{1}},
	})
	if len(tasks) != 1 || tasks[0].Index != 1 {
		t.Fatalf("tasks=%+v", tasks)
	}
}

func TestParseCodingWorkbenchPlanJSON(t *testing.T) {
	raw := `{"steps":[
		{"title":"探查代码结构","description":"定位登录相关入口"},
		{"title":"实现 JWT","description":"增加签发与校验","depends_on":[1]},
		{"title":"补测试","description":"覆盖登录成功/失败","depends_on":[2]}
	]}`
	tasks := parseCodingWorkbenchPlan(raw)
	if len(tasks) != 3 {
		t.Fatalf("tasks=%d", len(tasks))
	}
	if tasks[0].Title != "探查代码结构" {
		t.Fatalf("t0 title=%q", tasks[0].Title)
	}
	if len(tasks[1].DependsOn) != 1 || tasks[1].DependsOn[0] != 1 {
		t.Fatalf("depends=%v", tasks[1].DependsOn)
	}
}

func TestParseCodingWorkbenchPlanMarkdown(t *testing.T) {
	raw := `### T1: 探查
描述: 找入口
### T2: 实现
描述: 改代码
依赖: T1
### T3: 验证
描述: 跑测试
`
	tasks := parseCodingWorkbenchPlan(raw)
	if len(tasks) < 3 {
		t.Fatalf("tasks=%d want >=3", len(tasks))
	}
}

func TestParseCodingWorkbenchPlanNumbered(t *testing.T) {
	raw := "1. explore\n2. implement fix\n3. run tests"
	tasks := parseCodingWorkbenchPlan(raw)
	if len(tasks) != 3 {
		t.Fatalf("tasks=%d", len(tasks))
	}
}

func TestFormatCodingWorkbenchPlanMarkdown(t *testing.T) {
	md := formatCodingWorkbenchPlanMarkdown("ship auth", []*v2.TaskItem{
		{Index: 1, Title: "explore", Description: "map routes\n\n## Overall request\nship auth end to end with tests"},
		{Index: 2, Title: "implement", Description: "add jwt", DependsOn: []int{1}},
	})
	if !strings.Contains(md, "T1") || !strings.Contains(md, "explore") {
		t.Fatalf("md=%q", md)
	}
	if strings.Contains(md, "Overall request") {
		t.Fatalf("display markdown should strip overall request footer: %q", md)
	}
	if !strings.Contains(md, "map routes") {
		t.Fatalf("should keep real description: %q", md)
	}
}

func TestCodingWorkbenchRunHeader(t *testing.T) {
	if got := codingWorkbenchRunHeader(false, 1, []v2.TaskRunResult{{Status: v2.TaskPassed}}); got != "编码完成" {
		t.Fatalf("single pass: %q", got)
	}
	if got := codingWorkbenchRunHeader(true, 3, []v2.TaskRunResult{
		{Status: v2.TaskPassed}, {Status: v2.TaskPassed}, {Status: v2.TaskPassed},
	}); !strings.Contains(got, "3 步") {
		t.Fatalf("all pass: %q", got)
	}
	if got := codingWorkbenchRunHeader(true, 3, []v2.TaskRunResult{
		{Status: v2.TaskPassed}, {Status: v2.TaskFailed}, {Status: v2.TaskSkipped},
	}); !strings.Contains(got, "部分完成") {
		t.Fatalf("partial: %q", got)
	}
}

func TestFinalizeFillsMissingMidPlanDeps(t *testing.T) {
	tasks := finalizeCodingWorkbenchTasks([]*v2.TaskItem{
		{Title: "a", Description: "a", DependsOn: nil},
		{Title: "b", Description: "b", DependsOn: []int{1}},
		{Title: "c", Description: "c", DependsOn: nil}, // missing deps despite earlier having deps
	}, "overall")
	if len(tasks[2].DependsOn) != 1 || tasks[2].DependsOn[0] != 2 {
		t.Fatalf("T3 should chain to T2 when deps empty, got %v", tasks[2].DependsOn)
	}
}

func TestResolveCodingWorkbenchTasksSimpleSkipsPlanner(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	tasks, plan, planned := h.resolveCodingWorkbenchTasks(userID, "fix typo in README", "D:/repo", stickyCodingWorkbenchMemory{}, nil, nil)
	if planned || plan != "" {
		t.Fatalf("simple should not plan: planned=%v plan=%q", planned, plan)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks=%d", len(tasks))
	}
}

func TestExtractUserProvidedCodingPlan(t *testing.T) {
	text := "Please do:\n1. explore auth module\n2. implement JWT login\n3. add unit tests"
	tasks := extractUserProvidedCodingPlan(text)
	if len(tasks) != 3 {
		t.Fatalf("tasks=%d", len(tasks))
	}
	// Should not need LLM — resolve path with empty handler still plans.
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	got, md, planned := h.resolveCodingWorkbenchTasks(userID, text, "D:/repo", stickyCodingWorkbenchMemory{}, nil, nil)
	if !planned || len(got) != 3 {
		t.Fatalf("planned=%v steps=%d", planned, len(got))
	}
	if !strings.Contains(md, "T1") || !strings.Contains(md, "T3") {
		t.Fatalf("markdown=%q", md)
	}
	// Sequential deps after finalize.
	if len(got[1].DependsOn) != 1 || got[1].DependsOn[0] != 1 {
		t.Fatalf("deps=%v", got[1].DependsOn)
	}
}

func TestClearStickyCodingExecutionPlanOnSimpleTurn(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	h.setStickyCodingExecutionPlan(userID, "### T1: old\n### T2: plan")
	_, _, planned := h.resolveCodingWorkbenchTasks(userID, "fix typo", "D:/repo", stickyCodingWorkbenchMemory{TurnCount: 1, ExecutionPlan: "### T1: old"}, nil, nil)
	if planned {
		t.Fatal("simple should not plan")
	}
	if mem := h.getStickyCodingWorkbenchMemory(userID); mem.ExecutionPlan != "" {
		t.Fatalf("stale execution plan should clear: %q", mem.ExecutionPlan)
	}
}

func TestFinalizeRejectsForwardDepends(t *testing.T) {
	tasks := finalizeCodingWorkbenchTasks([]*v2.TaskItem{
		{Title: "a", Description: "a", DependsOn: []int{2}}, // forward dep invalid
		{Title: "b", Description: "b", DependsOn: []int{1}},
	}, "req")
	if len(tasks[0].DependsOn) != 0 {
		t.Fatalf("T1 should not depend on later step: %v", tasks[0].DependsOn)
	}
	if len(tasks[1].DependsOn) != 1 || tasks[1].DependsOn[0] != 1 {
		t.Fatalf("T2 deps=%v", tasks[1].DependsOn)
	}
}

func TestSetStickyCodingExecutionPlanVisibleInPrevOutputs(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	h.setStickyCodingExecutionPlan(userID, "### T1: a\n### T2: b")
	mem := h.getStickyCodingWorkbenchMemory(userID)
	joined := strings.Join(mem.prevOutputs(), "\n")
	if !strings.Contains(joined, "execution plan") && !strings.Contains(joined, "T1") {
		// prevOutputs only includes ExecutionPlan when non-empty
		if mem.ExecutionPlan == "" {
			t.Fatal("execution plan not stored")
		}
	}
	if !strings.Contains(strings.Join(mem.prevOutputs(), "\n"), "T1") {
		// Force TurnCount so prevOutputs is non-empty path... ExecutionPlan is always appended if set
		t.Fatalf("prevOutputs missing plan: %q mem=%+v", joined, mem)
	}
}
