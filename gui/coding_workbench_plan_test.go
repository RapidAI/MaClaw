package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestParseCodingRequestDecision(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		kind codingRequestKind
		plan bool
	}{
		{`{"kind":"inquiry","needs_plan":false}`, codingRequestInquiry, false},
		{`{"kind":"operational","needs_plan":false}`, codingRequestOperational, false},
		{`{"kind":"operational","needs_plan":true}`, codingRequestOperational, false},
		{`{"kind":"inquiry","needs_plan":true}`, codingRequestInquiry, false},
		{`{"kind":"implementation","needs_plan":true}`, codingRequestImplementation, true},
	} {
		decision, ok := parseCodingRequestDecision(tc.raw)
		if !ok || decision.Kind != tc.kind || decision.NeedsPlan != tc.plan {
			t.Fatalf("parseCodingRequestDecision(%q) = %#v, %v", tc.raw, decision, ok)
		}
	}
	for _, raw := range []string{"", `{"kind":"unknown","needs_plan":false}`, "not json"} {
		if _, ok := parseCodingRequestDecision(raw); ok {
			t.Fatalf("invalid classifier response accepted: %q", raw)
		}
	}
}

func TestResolveCodingWorkbenchTasksWithDecisionKeepsSimpleImplementationDirectInApproveMode(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	tasks, plan, planned := h.resolveCodingWorkbenchTasksWithDecision(
		userID,
		"fix the button label",
		"D:/repo",
		stickyCodingWorkbenchMemory{PlanMode: codingPlanModeApprove},
		codingRequestDecision{Kind: codingRequestImplementation, NeedsPlan: false},
		nil,
		nil,
	)
	if planned || plan != "" || len(tasks) != 1 {
		t.Fatalf("simple implementation must stay direct in approve mode: planned=%v plan=%q tasks=%d", planned, plan, len(tasks))
	}
}
func TestCodingRequestNeedsPlanFallbackRequiresExplicitSteps(t *testing.T) {
	for _, text := range []string{
		strings.Repeat("broad implementation request ", 12),
		"first investigate the architecture\nthen implement the migration\nfinally verify the deployment",
	} {
		if codingRequestNeedsPlanFallback(text) {
			t.Fatalf("fallback must not create a planning boundary from wording alone: %q", text)
		}
	}
	if !codingRequestNeedsPlanFallback("1. inspect the module\n2. implement the change") {
		t.Fatal("explicit numbered steps must retain their planning boundary")
	}
}
func TestApprovedCodingPlanDecisionIsAlwaysImplementation(t *testing.T) {
	decision := approvedCodingPlanDecision()
	if decision.Kind != codingRequestImplementation || !decision.NeedsPlan {
		t.Fatalf("approved plan decision = %#v", decision)
	}
}
func TestCodingTaskRequestKindUsesPropagatedDecision(t *testing.T) {
	if !codingTaskLooksOperational(&TaskItem{RequestKind: codingRequestOperational}) {
		t.Fatal("operational task should use propagated decision")
	}
	if !codingTaskLooksInquiry(&TaskItem{RequestKind: codingRequestInquiry}) {
		t.Fatal("inquiry task should use propagated decision")
	}
	if codingTaskLooksOperational(&TaskItem{Title: "run the app"}) {
		t.Fatal("subagent must not reclassify task wording")
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

func TestCodingInquiryToolFiltersAreReadOnly(t *testing.T) {
	if !isCodingInquiryTool("read_file") || !isCodingInquiryTool("code_navigation") {
		t.Fatal("local inquiry must retain read/navigation tools")
	}
	if isCodingInquiryTool("write_file") || isCodingInquiryTool("todo_write") {
		t.Fatal("local inquiry must not expose mutation/planning tools")
	}
	if !isRemoteCodingInquiryTool("ssh_read_file") || !isRemoteCodingInquiryTool("ssh_list_dir") {
		t.Fatal("remote inquiry must retain SSH read tools")
	}
	if isRemoteCodingInquiryTool("ssh_write_file") || isRemoteCodingInquiryTool("ssh_edit_file") {
		t.Fatal("remote inquiry must not expose SSH write tools")
	}
}

func TestCodingOperationalToolFiltersAreNonMutating(t *testing.T) {
	for _, name := range []string{"bash", "read_file", "Glob", "ripgrep", "code_navigation"} {
		if !isCodingOperationalTool(name) {
			t.Fatalf("operational task should retain %q", name)
		}
	}
	for _, name := range []string{"write_file", "edit_file", "edit_lines", "todo_write", codingSubAgentSpawnToolName, "manage_skill"} {
		if isCodingOperationalTool(name) {
			t.Fatalf("operational task must not expose implementation/planning tool %q", name)
		}
	}
	if rejectCodingOperationalShellCommand("go test ./...") != "" {
		t.Fatal("normal verification command should remain available to an operational task")
	}
	if rejectCodingOperationalShellCommand("go generate ./...") == "" {
		t.Fatal("source generation must not be treated as an operational command")
	}
}

func TestCodingInquiryShellCommandsRejectWritesAndAllowInspection(t *testing.T) {
	for _, command := range []string{
		"git status --short && git diff --stat",
		"rg -n 'authentication' . | sort",
		"find . -maxdepth 2 -type f",
		"codegraph explore authentication",
		"codegraph.cmd node AuthenticationService",
	} {
		if msg := rejectCodingInquiryShellCommand(command); msg != "" {
			t.Fatalf("read-only inquiry command should pass: %q: %s", command, msg)
		}
	}
	for _, command := range []string{
		"go test ./...",
		"npm run build",
		"git branch -D stale",
		"git config user.name test",
		"sed 'w generated.txt' README.md",
		"find . -exec touch changed \\;",
		"ls $(touch changed)",
		"cat <(touch changed)",
		"rg todo > findings.txt",
	} {
		if msg := rejectCodingInquiryShellCommand(command); msg == "" {
			t.Fatalf("read-only inquiry command should be rejected: %q", command)
		}
	}
}

func TestResolveCodingWorkbenchTasksAutoPersistsComplexPlanForConfirmation(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	text := "Please do:\n1. inspect the auth module\n2. implement JWT login\n3. add unit tests"
	tasks, _, planned := h.resolveCodingWorkbenchTasks(userID, text, "D:/repo", stickyCodingWorkbenchMemory{PlanMode: codingPlanModeAuto}, nil, nil)
	if !planned || len(tasks) != 3 {
		t.Fatalf("complex auto request should plan: planned=%v steps=%d", planned, len(tasks))
	}
	if pending, ok := h.loadStickyPendingCodingPlan(userID); !ok || len(pending.Tasks) != 3 {
		t.Fatalf("complex auto request should await confirmation, pending=%+v ok=%v", pending, ok)
	}
}

func TestResolveCodingWorkbenchTasksNewDirectTaskClearsStalePendingPlan(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	h.storeStickyPendingCodingPlan(userID, "old multi-step request", "### T1: old\n### T2: stale", []*v2.TaskItem{
		{Index: 1, Title: "old", Description: "old"},
		{Index: 2, Title: "stale", Description: "stale"},
	})
	tasks, plan, planned := h.resolveCodingWorkbenchTasks(userID, "fix a typo", "D:/repo", stickyCodingWorkbenchMemory{}, nil, nil)
	if planned || plan != "" || len(tasks) != 1 {
		t.Fatalf("new direct task should stay single: planned=%v plan=%q tasks=%d", planned, plan, len(tasks))
	}
	if _, ok := h.loadStickyPendingCodingPlan(userID); ok {
		t.Fatal("new direct task must clear the stale pending plan")
	}
}

func TestCodingPlanApprovalActionsMatchConfirmationChoices(t *testing.T) {
	actions := codingPlanApproveActions()
	if len(actions) != 3 {
		t.Fatalf("confirmation should expose exactly start, direct execute, and reject; got %#v", actions)
	}
	for _, action := range actions {
		if action.Command == "/plan mode auto" {
			t.Fatal("confirmation must not offer a mode switch that leaves the current plan pending")
		}
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
	if got := codingWorkbenchRunHeader(codingRequestImplementation, false, 1, []v2.TaskRunResult{{Status: v2.TaskPassed}}); got != "编码完成" {
		t.Fatalf("single pass: %q", got)
	}
	if got := codingWorkbenchRunHeader(codingRequestImplementation, true, 3, []v2.TaskRunResult{
		{Status: v2.TaskPassed}, {Status: v2.TaskPassed}, {Status: v2.TaskPassed},
	}); !strings.Contains(got, "3 步") {
		t.Fatalf("all pass: %q", got)
	}
	if got := codingWorkbenchRunHeader(codingRequestImplementation, true, 3, []v2.TaskRunResult{
		{Status: v2.TaskPassed}, {Status: v2.TaskFailed}, {Status: v2.TaskSkipped},
	}); !strings.Contains(got, "部分完成") {
		t.Fatalf("partial: %q", got)
	}
	if got := codingWorkbenchRunHeader(codingRequestInquiry, false, 1, []v2.TaskRunResult{{Status: v2.TaskPassed}}); got != "仓库分析完成" {
		t.Fatalf("inquiry pass: %q", got)
	}
	if got := codingWorkbenchRunHeader(codingRequestInquiry, false, 1, []v2.TaskRunResult{{Status: v2.TaskFailed}}); got != "仓库分析未完成" {
		t.Fatalf("inquiry failure: %q", got)
	}
	if got := codingWorkbenchRunHeader(codingRequestOperational, false, 1, []v2.TaskRunResult{{Status: v2.TaskPassed}}); got != "任务完成" {
		t.Fatalf("operational pass: %q", got)
	}
}

func TestCodingWorkbenchRunLabelsStaySpecificToRequestKind(t *testing.T) {
	for _, kind := range []codingRequestKind{codingRequestInquiry, codingRequestOperational, codingRequestImplementation} {
		labels := codingWorkbenchLabelsForRequest(kind)
		if labels.complete == "" || labels.partial == "" || labels.incomplete == "" || labels.skipped == "" {
			t.Fatalf("kind %q has incomplete labels: %#v", kind, labels)
		}
	}
	if got := codingWorkbenchRunHeader(codingRequestInquiry, true, 2, []v2.TaskRunResult{
		{Status: v2.TaskPassed}, {Status: v2.TaskSkipped},
	}); !strings.Contains(got, "仓库分析部分完成") || strings.Contains(got, "编码") {
		t.Fatalf("inquiry multi-step header must not claim coding: %q", got)
	}
}

func TestRepositoryInquiryHeaderAndReportStateNoFilesWereModified(t *testing.T) {
	header := codingWorkbenchRunHeader(codingRequestInquiry, false, 1, []v2.TaskRunResult{{Status: v2.TaskPassed}})
	if header != "仓库分析完成" {
		t.Fatalf("header = %q", header)
	}
	body := fmt.Sprintf("%s\n项目路径：%s\n只读检查：未修改任何文件。\n\n%s", header, "D:/repo", "analysis")
	if strings.Contains(body, "编码完成") || !strings.Contains(body, "只读检查：未修改任何文件") {
		t.Fatalf("unexpected inquiry report: %q", body)
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
