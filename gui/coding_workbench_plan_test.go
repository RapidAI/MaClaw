package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestShouldEnableCodingTDD_SkipsOperationalRunRequests(t *testing.T) {
	// Pure ops in a sticky coding session must not enter TDD red/green.
	ops := []string{
		"运行下生成的游戏",
		"运行一下游戏",
		"跑一下",
		"再跑一次",
		"run the game",
		"run it",
		"build and run",
	}
	for _, text := range ops {
		if !looksLikeCodingOperationalRequest(text) {
			t.Fatalf("expected operational: %q", text)
		}
		if shouldEnableCodingTDD(text, false, 1) {
			t.Fatalf("TDD must stay off for operational request: %q", text)
		}
	}
	// Real implement/fix single tasks still get TDD.
	impl := []string{
		"修复蛇撞墙不结束的 bug",
		"实现暂停功能",
		"fix the collision detection",
		"add a score multiplier",
	}
	for _, text := range impl {
		if looksLikeCodingOperationalRequest(text) {
			t.Fatalf("implement request should not be operational: %q", text)
		}
		if !shouldEnableCodingTDD(text, false, 1) {
			t.Fatalf("TDD should stay on for implement request: %q", text)
		}
	}
	// Multi-step planned turns never use TDD (already explore→implement→verify).
	if shouldEnableCodingTDD("运行下生成的游戏", true, 1) {
		t.Fatal("planned turns must not enable TDD")
	}
	if shouldEnableCodingTDD("实现登录", false, 3) {
		t.Fatal("multi-task turns must not enable TDD")
	}
	// Implement + run still counts as implementation (needs code work).
	if looksLikeCodingOperationalRequest("实现暂停功能并运行验证") {
		t.Fatal("implement+run should not be treated as pure operational")
	}
	if !shouldEnableCodingTDD("实现暂停功能并运行验证", false, 1) {
		t.Fatal("implement+run should still enable TDD")
	}
}

func TestCodingTaskLooksOperational(t *testing.T) {
	if !codingTaskLooksOperational(&TaskItem{Title: "运行下生成的游戏", Description: "运行下生成的游戏"}) {
		t.Fatal("run-game task should be operational")
	}
	if !looksLikeCodingOperationalRequest("帮我运行一下") {
		t.Fatal("帮我运行一下 should be operational")
	}
	if codingTaskLooksOperational(&TaskItem{Title: "修复碰撞检测", Description: "蛇撞墙应 game over"}) {
		t.Fatal("fix task should not be operational")
	}
	// Must not treat implement phrasing as operational via bare English "start"/"run".
	if looksLikeCodingOperationalRequest("start coding") {
		t.Fatal("start coding must not be operational")
	}
	if looksLikeCodingOperationalRequest("实现暂停功能") {
		t.Fatal("implement request must not be operational")
	}
}

func TestSummarizeOperationalSubAgentQuality(t *testing.T) {
	// Empty no-tool operational run fails with a clear ops diagnostic (not implement no-change matrix).
	st, sum, n := summarizeOperationalSubAgentQuality(codingSubAgentAudit{}, agent.LoopResult{ToolCalls: 0})
	if st != codingSubAgentQualityFailed || n != 1 || !strings.Contains(sum, "ran no tools") {
		t.Fatalf("empty ops quality = %q %q %d", st, sum, n)
	}
	// Successful launch is enough — no file edits required.
	st, sum, n = summarizeOperationalSubAgentQuality(codingSubAgentAudit{
		AllCommandsRun: []CodingSubAgentCommandResult{{Command: ".\\snake.exe", Succeeded: true, Summary: "started"}},
	}, agent.LoopResult{ToolCalls: 1})
	if st != codingSubAgentQualityPassed || n != 0 || !strings.Contains(sum, "launch/build command evidence") {
		t.Fatalf("bash ops quality = %q %q %d", st, sum, n)
	}
	// dir/ls alone must NOT pass.
	st, sum, n = summarizeOperationalSubAgentQuality(codingSubAgentAudit{
		AllCommandsRun: []CodingSubAgentCommandResult{{Command: "dir", Succeeded: true, Summary: "files..."}},
	}, agent.LoopResult{ToolCalls: 1})
	if st != codingSubAgentQualityFailed || !strings.Contains(sum, "no launch/build command") {
		t.Fatalf("dir-only ops quality should fail, got %q %q %d", st, sum, n)
	}
	// mkdir alone must NOT pass (not launch/build evidence).
	st, sum, n = summarizeOperationalSubAgentQuality(codingSubAgentAudit{
		AllCommandsRun: []CodingSubAgentCommandResult{{Command: "mkdir tmpout", Succeeded: true, Summary: "ok"}},
	}, agent.LoopResult{ToolCalls: 1})
	if st != codingSubAgentQualityFailed || n != 1 {
		t.Fatalf("mkdir-only ops quality should fail, got %q %q %d", st, sum, n)
	}
	// Unknown non-launch shell (e.g. hostname) must NOT pass either.
	st, sum, n = summarizeOperationalSubAgentQuality(codingSubAgentAudit{
		AllCommandsRun: []CodingSubAgentCommandResult{{Command: "hostname", Succeeded: true, Summary: "pc"}},
	}, agent.LoopResult{ToolCalls: 1})
	if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none looked like launch/build") {
		t.Fatalf("hostname-only ops quality should fail as non-launch, got %q %q %d", st, sum, n)
	}
	// Get-ChildItem then real launch: launch evidence wins.
	st, sum, n = summarizeOperationalSubAgentQuality(codingSubAgentAudit{
		AllCommandsRun: []CodingSubAgentCommandResult{
			{Command: "Get-ChildItem", Succeeded: true, Summary: "list"},
			{Command: "cmd /c .\\build_and_run.bat", Succeeded: true, Summary: "ok"},
		},
	}, agent.LoopResult{ToolCalls: 2})
	if st != codingSubAgentQualityPassed || n != 0 {
		t.Fatalf("list+launch should pass, got %q %q %d", st, sum, n)
	}
	// Compound dir && launch in one shell line should count as launch.
	st, sum, n = summarizeOperationalSubAgentQuality(codingSubAgentAudit{
		AllCommandsRun: []CodingSubAgentCommandResult{
			{Command: "dir ; .\\snake.exe", Succeeded: true, Summary: "started"},
		},
	}, agent.LoopResult{ToolCalls: 1})
	if st != codingSubAgentQualityPassed || n != 0 {
		t.Fatalf("compound dir;launch should pass, got %q %q %d", st, sum, n)
	}
	// Read-only inspection without launch must NOT pass (would fake "已运行").
	st, sum, n = summarizeOperationalSubAgentQuality(codingSubAgentAudit{
		AllFilesRead: []string{"README.md"},
	}, agent.LoopResult{ToolCalls: 1})
	if st != codingSubAgentQualityFailed || n != 1 || !strings.Contains(sum, "no launch/build command") {
		t.Fatalf("inspection-only ops quality should fail, got %q %q %d", st, sum, n)
	}
	// Failed launch commands fail clearly.
	st, sum, n = summarizeOperationalSubAgentQuality(codingSubAgentAudit{
		AllCommandsRun: []CodingSubAgentCommandResult{{Command: ".\\snake.exe", Succeeded: false, Summary: "not found"}},
	}, agent.LoopResult{ToolCalls: 1})
	if st != codingSubAgentQualityFailed || !strings.Contains(sum, "failed") {
		t.Fatalf("failed launch ops quality = %q %q %d", st, sum, n)
	}
}

func TestClassifyOperationalShellCommand(t *testing.T) {
	if classifyOperationalShellCommand("dir") != operationalShellInspection {
		t.Fatal("dir should be inspection")
	}
	if classifyOperationalShellCommand(".\\snake.exe") != operationalShellLaunchBuild {
		t.Fatal("exe should be launch/build")
	}
	if classifyOperationalShellCommand("dir ; .\\snake.exe") != operationalShellLaunchBuild {
		t.Fatal("compound dir;exe should be launch/build")
	}
	if classifyOperationalShellCommand("cmd /c .\\build_and_run.bat") != operationalShellLaunchBuild {
		t.Fatal("cmd /c bat should be launch/build")
	}
	if classifyOperationalShellCommand("go run .") != operationalShellLaunchBuild {
		t.Fatal("go run should be launch/build")
	}
	if classifyOperationalShellCommand("mkdir x") == operationalShellLaunchBuild {
		t.Fatal("mkdir must not count as launch/build")
	}
	// Bare "." / ".." must not count as launch (path-token edge case).
	if classifyOperationalShellCommand(".") == operationalShellLaunchBuild ||
		classifyOperationalShellCommand("..") == operationalShellLaunchBuild {
		t.Fatal("bare . / .. must not count as launch/build")
	}
	// Bare python/node without a script is not launch evidence.
	if classifyOperationalShellCommand("python") == operationalShellLaunchBuild ||
		classifyOperationalShellCommand("node") == operationalShellLaunchBuild {
		t.Fatal("bare interpreter must not count as launch/build")
	}
	if classifyOperationalShellCommand("python main.py") != operationalShellLaunchBuild {
		t.Fatal("python script should count as launch/build")
	}
	if classifyOperationalShellCommand("hostname") != operationalShellUnknown {
		t.Fatalf("hostname should be unknown non-launch, got %v", classifyOperationalShellCommand("hostname"))
	}
}

func TestIsOperationalInspectionOnlyCommand(t *testing.T) {
	if !isOperationalInspectionOnlyCommand("dir") || !isOperationalInspectionOnlyCommand("Get-ChildItem -Force") {
		t.Fatal("listing commands should be inspection-only")
	}
	if isOperationalInspectionOnlyCommand(".\\snake.exe") || isOperationalInspectionOnlyCommand("cmd /c .\\build_and_run.bat") {
		t.Fatal("launch/build commands should not be inspection-only")
	}
	if isOperationalInspectionOnlyCommand("dir ; .\\snake.exe") {
		t.Fatal("compound with launch must not be pure inspection-only")
	}
}

func TestCodingTaskLooksOperationalPrefersDescription(t *testing.T) {
	// Description is the full user text; title may be truncated noise.
	if !codingTaskLooksOperational(&TaskItem{Title: "T1", Description: "运行下生成的游戏"}) {
		t.Fatal("description operational text should win")
	}
}

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
