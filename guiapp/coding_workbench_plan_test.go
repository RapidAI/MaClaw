package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestCodingRequestLooksExplicitWorkspaceClear(t *testing.T) {
	for _, text := range []string{
		"清空当前目录",
		"请把当前目录清空",
		"clear the current directory",
		"wipe the workspace",
		"delete all files in the folder",
	} {
		if !codingRequestLooksExplicitWorkspaceClear(text) {
			t.Fatalf("expected workspace-clear request: %q", text)
		}
	}
	for _, text := range []string{
		"怎么清空当前目录",
		"如何 clear the directory",
		"不要清空当前目录",
		"run the app",
		"fix the login bug",
	} {
		if codingRequestLooksExplicitWorkspaceClear(text) {
			t.Fatalf("did not expect workspace-clear request: %q", text)
		}
	}
}

func TestCodingRequestLooksModeratelyComplex(t *testing.T) {
	for _, text := range []string{
		"改为豪华版 hello world",
		"改为图形界面版",
		"实现登录并加测试",
		"add a login page with tests",
		"1. inspect auth\n2. implement jwt",
	} {
		if !codingRequestLooksModeratelyComplex(text) {
			t.Fatalf("expected moderately complex: %q", text)
		}
	}
	for _, text := range []string{
		"fix the button label",
		"fix a typo",
		"清空当前目录",
		"run the app",
	} {
		if codingRequestLooksModeratelyComplex(text) {
			t.Fatalf("did not expect moderately complex: %q", text)
		}
	}
}

func TestResolveCodingRequestDecisionPlansModerateRewrite(t *testing.T) {
	decision := (*IMMessageHandler)(nil).resolveCodingRequestDecision("改为豪华版 hello world")
	if decision.Kind != codingRequestImplementation || !decision.NeedsPlan {
		t.Fatalf("moderate rewrite must plan, got %#v", decision)
	}
}

func TestBareContinuationResumesImplementationAfterFileWrites(t *testing.T) {
	written := stickyCodingWorkbenchMemory{FilesModified: []string{"/home/prj8/src/ui.cpp"}, TurnCount: 4}
	for _, text := range []string{"继续", "继续。", "Continue", "keep going", "继续改进进程列表", "继续改", "继续，把顶栏改好", "帮我改顶栏", "能不能修一下顶栏？", "请修改顶栏折行", "fixed the header overflow", "优化进程 CPU 计算"} {
		got := applyCodingSessionContinuationFloor(codingRequestDecision{Kind: codingRequestInquiry}, text, written)
		if got.Kind != codingRequestImplementation || got.NeedsPlan {
			t.Fatalf("%q should resume read/write implementation without a new plan, got %#v", text, got)
		}
	}
	for _, text := range []string{"这段 CPU% 为什么乘了核心数？", "为什么没实现分页？", "怎么优化这段", "如何修改顶栏", "how to fix the header", "看看 drawHeader", "看看改动", "继续看看", "继续编译并运行", "what does the gap do", "谢谢", "编译并运行", "顶栏折行了", "列出源文件"} {
		got := applyCodingSessionContinuationFloor(codingRequestDecision{Kind: codingRequestInquiry}, text, written)
		if got.Kind != codingRequestInquiry {
			t.Fatalf("%q should stay a read-only question, got %#v", text, got)
		}
	}
	// No project writes yet: "继续" can still be "continue the explanation".
	blank := applyCodingSessionContinuationFloor(codingRequestDecision{Kind: codingRequestInquiry}, "继续", stickyCodingWorkbenchMemory{FilesModified: []string{"  "}})
	if blank.Kind != codingRequestInquiry {
		t.Fatalf("blank file records must not count as an implementation trajectory, got %#v", blank)
	}
	fresh := applyCodingSessionContinuationFloor(codingRequestDecision{Kind: codingRequestInquiry}, "继续", stickyCodingWorkbenchMemory{TurnCount: 1, SessionPlan: "开发一套系统信息查看软件"})
	if fresh.Kind != codingRequestInquiry {
		t.Fatalf("continuation without written files must stay inquiry, got %#v", fresh)
	}
	operational := applyCodingSessionContinuationFloor(codingRequestDecision{Kind: codingRequestOperational, Acceptance: codingOperationalAcceptanceLaunch}, "继续", written)
	if operational.Kind != codingRequestOperational {
		t.Fatalf("operational follow-up must not be rewritten, got %#v", operational)
	}
}

func TestApplyRemoteCodingTurnFileAuditKeepsEarlierStepWrites(t *testing.T) {
	result := &RemoteCodingSubAgentResult{FilesModified: []string{"/home/prj8/src/late.cpp"}}
	applyRemoteCodingTurnFileAudit(result,
		[]string{"/home/prj8/CMakeLists.txt", "  ", "/home/prj8/src/late.cpp"},
		[]string{"/home/prj8/src/ui.cpp", "/home/prj8/src/ui.cpp"},
	)
	if strings.Join(result.FilesModified, ",") != "/home/prj8/CMakeLists.txt,/home/prj8/src/late.cpp" {
		t.Fatalf("modified audit = %#v", result.FilesModified)
	}
	if strings.Join(result.FilesCreated, ",") != "/home/prj8/src/ui.cpp" {
		t.Fatalf("created audit = %#v", result.FilesCreated)
	}
	applyRemoteCodingTurnFileAudit(nil, []string{"a"}, []string{"b"})
}

func TestApplyCodingRequestPlanFloorPromotesModerateImplementation(t *testing.T) {
	got := applyCodingRequestPlanFloor(codingRequestDecision{Kind: codingRequestImplementation, NeedsPlan: false}, "改为豪华版 hello world")
	if !got.NeedsPlan {
		t.Fatal("moderate implementation must get a planning boundary")
	}
	got = applyCodingRequestPlanFloor(codingRequestDecision{Kind: codingRequestOperational, NeedsPlan: false}, "改为豪华版 hello world")
	if got.NeedsPlan {
		t.Fatal("operational requests must not gain a planning boundary")
	}
}

func TestResolveCodingRequestDecisionForcesWorkspaceClearToImplementation(t *testing.T) {
	decision := (*IMMessageHandler)(nil).resolveCodingRequestDecision("清空当前目录")
	if decision.Kind != codingRequestImplementation || decision.NeedsPlan {
		t.Fatalf("workspace clear must be a direct implementation turn, got %#v", decision)
	}
}

func TestCodingRequestClassifierPromptTreatsWorkspaceClearAsImplementation(t *testing.T) {
	if !strings.Contains(codingRequestClassifierSystemPrompt, "clearing or emptying the current project directory is implementation") &&
		!strings.Contains(codingRequestClassifierSystemPrompt, "Clearing or emptying the current project directory is implementation") {
		t.Fatalf("classifier prompt must not treat workspace clear as operational: %s", codingRequestClassifierSystemPrompt)
	}
	if !strings.Contains(codingRequestClassifierSystemPrompt, `acceptance":"launch|command"`) {
		t.Fatalf("classifier prompt must own the operational evidence contract: %s", codingRequestClassifierSystemPrompt)
	}
	if strings.Contains(codingRequestClassifierSystemPrompt, "actually run something is operational with acceptance launch") {
		t.Fatal("classifier prompt must not treat every run request as a project launch")
	}
}

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
	decision, ok := parseCodingRequestDecision(`{"kind":"operational","needs_plan":false}`)
	if !ok || decision.Acceptance != codingOperationalAcceptanceLaunch {
		t.Fatalf("omitted operational acceptance must stay launch, got %#v %v", decision, ok)
	}
	decision, ok = parseCodingRequestDecision(`{"kind":"operational","needs_plan":true,"acceptance":"Command"}`)
	if !ok || decision.NeedsPlan || decision.Acceptance != codingOperationalAcceptanceCommand {
		t.Fatalf("command acceptance = %#v %v", decision, ok)
	}
	decision, ok = parseCodingRequestDecision(`{"kind":"operational","needs_plan":false,"acceptance":"yes"}`)
	if !ok || decision.Acceptance != codingOperationalAcceptanceLaunch {
		t.Fatalf("unknown acceptance must not loosen the gate, got %#v", decision)
	}
	decision, ok = parseCodingRequestDecision(`{"kind":"implementation","needs_plan":true,"acceptance":"command"}`)
	if !ok || decision.Acceptance != "" {
		t.Fatalf("implementation must drop acceptance, got %#v", decision)
	}
	decision, ok = parseCodingRequestDecision(`{"kind":"inquiry","needs_plan":false,"acceptance":"COMMAND"}`)
	if !ok || decision.Acceptance != "" {
		t.Fatalf("inquiry must drop acceptance, got %#v", decision)
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

func TestCodingTaskLooksOperationalRejectsWorkspaceClear(t *testing.T) {
	if codingTaskLooksOperational(&TaskItem{Title: "清空当前目录", RequestKind: codingRequestOperational}) {
		t.Fatal("workspace clear must not use operational launch/build scoring")
	}
	if codingTaskLooksOperational(&TaskItem{Description: "wipe the workspace", RequestKind: codingRequestOperational}) {
		t.Fatal("english workspace wipe must not use operational scoring")
	}
	if !codingTaskLooksOperational(&TaskItem{Title: "run the app", RequestKind: codingRequestOperational}) {
		t.Fatal("a real run/build request must stay operational")
	}
}

func TestForceWorkspaceClearCodingDecision(t *testing.T) {
	got := forceWorkspaceClearCodingDecision("清空当前目录", codingRequestDecision{Kind: codingRequestOperational, NeedsPlan: false})
	if got.Kind != codingRequestImplementation || got.NeedsPlan {
		t.Fatalf("leaked operational wipe must become implementation, got %#v", got)
	}
	got = forceWorkspaceClearCodingDecision("run the app", codingRequestDecision{Kind: codingRequestOperational, NeedsPlan: false})
	if got.Kind != codingRequestOperational {
		t.Fatalf("real run/build must stay operational, got %#v", got)
	}
	got = forceWorkspaceClearCodingDecision(codingPlanApproveExecuteMarker+" 清空当前目录", codingRequestDecision{Kind: codingRequestOperational, NeedsPlan: false})
	if got.Kind != codingRequestImplementation || got.NeedsPlan {
		t.Fatalf("approve-prefixed wipe must become implementation, got %#v", got)
	}
}

func TestNormalizeCodingWorkspaceClearTextStripsApproveMarker(t *testing.T) {
	if got := normalizeCodingWorkspaceClearText(codingPlanApproveExecuteMarker + " 清空当前目录"); got != "清空当前目录" {
		t.Fatalf("normalized approve-prefixed wipe = %q", got)
	}
	if !codingRequestIsPureWorkspaceClear(normalizeCodingWorkspaceClearText(codingPlanApproveExecuteMarker + " 清空当前目录")) {
		t.Fatal("approve-prefixed wipe must remain a pure host-clear")
	}
	if codingRequestIsPureWorkspaceClear(codingPlanApproveExecuteMarker + " 清空当前目录") {
		t.Fatal("raw approve marker must not look like a pure wipe without normalization")
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

func TestOperationalCommandAcceptanceFollowsClassifierDecision(t *testing.T) {
	commands := []CodingSubAgentCommandResult{
		{Command: `cat /etc/os-release | head -5; echo "---"; uname -a`, Succeeded: true},
		{Command: `which iostat 2>/dev/null && echo "iostat already installed" || echo "iostat NOT installed"`, Succeeded: true},
		{Command: `apt-get install -y sysstat 2>&1 | tail -15`, Succeeded: true},
		{Command: `iostat -V 2>&1 | head -3; echo "=== sample ==="; iostat 1 2 2>&1`, Succeeded: true},
		{Command: `echo "load"; uptime; free -h; df -h`, Succeeded: true},
	}
	st, sum, n := summarizeOperationalShellQuality(codingOperationalAcceptanceCommand, commands, len(commands), "")
	if st != codingSubAgentQualityPassed || n != 0 || !strings.Contains(sum, "host command evidence") {
		t.Fatalf("host command contract should pass on a non-probe command, got %q %q %d", st, sum, n)
	}
	st, sum, n = summarizeRemoteOperationalQuality(codingOperationalAcceptanceCommand, commands, len(commands))
	if st != codingSubAgentQualityPassed || n != 0 || !strings.Contains(sum, "host command evidence") {
		t.Fatalf("remote host command contract should pass, got %q %q %d", st, sum, n)
	}

	// Wording is not a second classifier. An install sentence under the launch
	// contract still fails, and a neutral title under the command contract passes.
	st, sum, n = summarizeOperationalSubAgentQualityForTask(&TaskItem{
		Title: "安装iostat", Description: "查看服务器状态", RequestKind: codingRequestOperational,
	}, codingSubAgentAudit{AllCommandsRun: commands}, agent.LoopResult{ToolCalls: len(commands)})
	if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none looked like launch/build") {
		t.Fatalf("task wording must not switch the launch contract, got %q %q %d", st, sum, n)
	}
	st, sum, n = summarizeOperationalSubAgentQualityForTask(&TaskItem{
		Title: "T1", RequestKind: codingRequestOperational, OperationalAcceptance: codingOperationalAcceptanceCommand,
	}, codingSubAgentAudit{AllCommandsRun: commands}, agent.LoopResult{ToolCalls: len(commands)})
	if st != codingSubAgentQualityPassed || n != 0 || !strings.Contains(sum, "host command evidence") {
		t.Fatalf("stored command contract should pass without reading the title, got %q %q %d", st, sum, n)
	}

	st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceLaunch, commands, len(commands), "")
	if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none looked like launch/build") {
		t.Fatalf("package install must not pass a project launch, got %q %q %d", st, sum, n)
	}

	st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceCommand, []CodingSubAgentCommandResult{
		{Command: `which iostat || echo missing`, Succeeded: true},
		{Command: "hostname", Succeeded: true},
		{Command: "sudo hostname", Succeeded: true},
		{Command: `cat /etc/os-release | head -5; uname -a`, Succeeded: true},
	}, 4, "")
	if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none performed the host action") {
		t.Fatalf("probes must not pass the command contract, got %q %q %d", st, sum, n)
	}

	st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceCommand, []CodingSubAgentCommandResult{
		{Command: "sudo apt-get install -y sysstat", Succeeded: false, Summary: "permission denied"},
		{Command: "true", Succeeded: true},
		{Command: "printf ok", Succeeded: true},
	}, 3, "")
	if st != codingSubAgentQualityFailed || !strings.Contains(sum, "host command failed") {
		t.Fatalf("a no-op must not hide a failed host command, got %q %q %d", st, sum, n)
	}
	st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptance(" Command "), []CodingSubAgentCommandResult{
		{Command: "command apt-get install -y sysstat", Succeeded: true},
	}, 1, "")
	if st != codingSubAgentQualityPassed || n != 0 {
		t.Fatalf("command builtin wrapping a host action should pass, got %q %q %d", st, sum, n)
	}
	st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceCommand, []CodingSubAgentCommandResult{
		{Command: "command -v iostat", Succeeded: true},
		{Command: "sudo command -v iostat", Succeeded: true},
		{Command: "test -x /usr/bin/iostat", Succeeded: true},
	}, 3, "")
	if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none performed the host action") {
		t.Fatalf("name lookup must not pass the command contract, got %q %q %d", st, sum, n)
	}

	st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceCommand, []CodingSubAgentCommandResult{
		{Command: "uptime; free -h; df -h", Succeeded: true},
	}, 1, "")
	if st != codingSubAgentQualityPassed || n != 0 {
		t.Fatalf("non-probe status command should pass the command contract, got %q %q %d", st, sum, n)
	}
	st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceCommand, []CodingSubAgentCommandResult{
		{Command: "df -h", Succeeded: true},
	}, 1, "")
	if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none performed the host action") {
		t.Fatalf("df alone must not pass the command contract, got %q %q %d", st, sum, n)
	}

	// A wrapper is not the action. The probe check has to see the program that
	// runs, including when sudo keeps bash -c in the same segment.
	for _, command := range []string{
		"bash -c hostname",
		"bash -lc hostname",
		"sh -c hostname",
		"sudo bash -c hostname",
		"sudo -u root hostname",
		"nice hostname",
		"nice -n 19 hostname",
		"nohup hostname",
		"stdbuf -oL hostname",
		"ionice -c 3 hostname",
		"setsid hostname",
		"sudo nice hostname",
		"timeout 5 hostname",
		"powershell -ExecutionPolicy Bypass -Command hostname",
	} {
		st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceCommand, []CodingSubAgentCommandResult{
			{Command: command, Succeeded: true},
		}, 1, "")
		if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none performed the host action") {
			t.Fatalf("wrapped probe %q must not pass the command contract, got %q %q %d", command, st, sum, n)
		}
		st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceLaunch, []CodingSubAgentCommandResult{
			{Command: command, Succeeded: true},
		}, 1, "")
		if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none looked like launch/build") {
			t.Fatalf("wrapped probe %q must not pass a project launch, got %q %q %d", command, st, sum, n)
		}
	}
	for _, command := range []string{
		`bash -c "apt-get install -y sysstat"`,
		`sudo bash -c "apt-get install -y sysstat"`,
		"nice apt-get install -y sysstat",
		"nohup iostat",
		`bash -lc "iostat 1 2"`,
	} {
		st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceCommand, []CodingSubAgentCommandResult{
			{Command: command, Succeeded: true},
		}, 1, "")
		if st != codingSubAgentQualityPassed || n != 0 || !strings.Contains(sum, "host command evidence") {
			t.Fatalf("wrapped host command %q should pass the command contract, got %q %q %d", command, st, sum, n)
		}
		st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceLaunch, []CodingSubAgentCommandResult{
			{Command: command, Succeeded: true},
		}, 1, "")
		if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none looked like launch/build") {
			t.Fatalf("wrapped host command %q must not pass a project launch, got %q %q %d", command, st, sum, n)
		}
	}
	st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceCommand, []CodingSubAgentCommandResult{
		{Command: `bash -c "apt-get install -y sysstat"`, Succeeded: false, Summary: "permission denied"},
		{Command: "true", Succeeded: true},
	}, 2, "")
	if st != codingSubAgentQualityFailed || !strings.Contains(sum, "host command failed") {
		t.Fatalf("a wrapped failure must stay failed, got %q %q %d", st, sum, n)
	}

	// Privilege and su -c hide the program from a classifier that only looks at
	// the first token. The launch contract has to see the program; a probe
	// behind the same prefix stays a probe.
	for _, command := range []string{
		`sudo .\snake.exe`,
		`sudo -u root .\snake.exe`,
		`sudo -g wheel .\snake.exe`,
		`sudo bash -c .\snake.exe`,
		`su -c .\snake.exe`,
		`su root -c .\snake.exe`,
	} {
		st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceLaunch, []CodingSubAgentCommandResult{
			{Command: command, Succeeded: true},
		}, 1, "")
		if st != codingSubAgentQualityPassed || n != 0 || !strings.Contains(sum, "launch/build command evidence") {
			t.Fatalf("wrapped launch %q should pass, got %q %q %d", command, st, sum, n)
		}
	}
	st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceLaunch, []CodingSubAgentCommandResult{
		{Command: `sudo .\snake.exe`, Succeeded: false, Summary: "not found"},
	}, 1, "")
	if st != codingSubAgentQualityFailed || !strings.Contains(sum, "failed") {
		t.Fatalf("failed wrapped launch should report the failure, got %q %q %d", st, sum, n)
	}
	st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceLaunch, []CodingSubAgentCommandResult{
		{Command: "sudo dir", Succeeded: true},
	}, 1, "")
	if st != codingSubAgentQualityFailed || !strings.Contains(sum, "no launch/build command") {
		t.Fatalf("sudo in front of a listing is still a listing, got %q %q %d", st, sum, n)
	}
	for _, command := range []string{
		"sudo -g wheel hostname",
		"sudo -p password hostname",
		"su -c hostname",
		"su root -c hostname",
		"pkexec hostname",
	} {
		st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceCommand, []CodingSubAgentCommandResult{
			{Command: command, Succeeded: true},
		}, 1, "")
		if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none performed the host action") {
			t.Fatalf("wrapped probe %q must not pass the command contract, got %q %q %d", command, st, sum, n)
		}
	}
	st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceCommand, []CodingSubAgentCommandResult{
		{Command: `su -c "apt-get install -y sysstat"`, Succeeded: true},
	}, 1, "")
	if st != codingSubAgentQualityPassed || n != 0 || !strings.Contains(sum, "host command evidence") {
		t.Fatalf("su -c of a host command should pass, got %q %q %d", st, sum, n)
	}
	st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceLaunch, []CodingSubAgentCommandResult{
		{Command: `su -c "apt-get install -y sysstat"`, Succeeded: true},
	}, 1, "")
	if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none looked like launch/build") {
		t.Fatalf("su -c of a package install must not pass a project launch, got %q %q %d", st, sum, n)
	}
	// The quoted payload is the command line, not one opaque program name.
	for _, command := range []string{
		`su -c "df -h"`,
		`eval "df -h"`,
		`sudo su -c "echo hi"`,
		`su -c -- hostname`,
		`eval "echo hi"`,
		`su -c "hostname; df -h"`,
	} {
		st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceCommand, []CodingSubAgentCommandResult{
			{Command: command, Succeeded: true},
		}, 1, "")
		if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none performed the host action") {
			t.Fatalf("quoted probe %q must not pass the command contract, got %q %q %d", command, st, sum, n)
		}
	}
	for _, command := range []string{
		`eval "apt-get install -y sysstat"`,
		`sudo su -c "apt-get install -y sysstat"`,
		`su -c "echo start; apt-get install -y sysstat"`,
	} {
		st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceCommand, []CodingSubAgentCommandResult{
			{Command: command, Succeeded: true},
		}, 1, "")
		if st != codingSubAgentQualityPassed || n != 0 || !strings.Contains(sum, "host command evidence") {
			t.Fatalf("quoted host command %q should pass the command contract, got %q %q %d", command, st, sum, n)
		}
		st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceLaunch, []CodingSubAgentCommandResult{
			{Command: command, Succeeded: true},
		}, 1, "")
		if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none looked like launch/build") {
			t.Fatalf("quoted host command %q must not pass a project launch, got %q %q %d", command, st, sum, n)
		}
	}
	for _, command := range []string{
		`su -c "go run ."`,
		`eval "python app.py"`,
		`su -c "C:\Program Files\snake.exe"`,
		`su -c "C:\Program Files\snake.exe --debug"`,
		`C:\Program Files\snake.exe`,
		`C:/Program Files/snake.exe`,
		`./snake src/test`,
		`./snake src/echo`,
		`.\snake src\test`,
	} {
		st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceLaunch, []CodingSubAgentCommandResult{
			{Command: command, Succeeded: true},
		}, 1, "")
		if st != codingSubAgentQualityPassed || n != 0 || !strings.Contains(sum, "launch/build command evidence") {
			t.Fatalf("quoted launch %q should pass, got %q %q %d", command, st, sum, n)
		}
	}
	// An absolute program plus an argument is not a path that contains a space.
	// A spaced path is still one program, so hostname.exe stays a probe.
	for _, command := range []string{
		`su -c "/bin/echo hi"`,
		`eval "/usr/bin/cat /etc/os-release"`,
		`su -c "/bin/echo files\x"`,
		`su -c "C:\Program Files\hostname.exe"`,
		`C:\Program Files\hostname.exe`,
		`C:/Program Files/hostname.exe`,
	} {
		st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceCommand, []CodingSubAgentCommandResult{
			{Command: command, Succeeded: true},
		}, 1, "")
		if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none performed the host action") {
			t.Fatalf("program path %q must not pass the command contract, got %q %q %d", command, st, sum, n)
		}
	}
	for _, command := range []string{
		`su -c "C:\Program Files\hostname.exe"`,
		`C:\Program Files\hostname.exe`,
		`C:/Program Files/hostname.exe`,
	} {
		st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceLaunch, []CodingSubAgentCommandResult{
			{Command: command, Succeeded: true},
		}, 1, "")
		if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none looked like launch/build") {
			t.Fatalf("spaced hostname %q must not pass a project launch, got %q %q %d", command, st, sum, n)
		}
	}
	st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceCommand, []CodingSubAgentCommandResult{
		{Command: `su -c "/usr/bin/apt-get install -y sysstat"`, Succeeded: true},
	}, 1, "")
	if st != codingSubAgentQualityPassed || n != 0 || !strings.Contains(sum, "host command evidence") {
		t.Fatalf("quoted absolute apt-get should pass the command contract, got %q %q %d", st, sum, n)
	}
	st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceLaunch, []CodingSubAgentCommandResult{
		{Command: `su -c "/usr/bin/apt-get install -y sysstat"`, Succeeded: true},
	}, 1, "")
	if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none looked like launch/build") {
		t.Fatalf("quoted absolute apt-get must not pass a project launch, got %q %q %d", st, sum, n)
	}

	// An option value is not the program. bash -o pipefail -c still has to
	// show the payload, and exec/eval are prefixes in the same way.
	for _, command := range []string{
		"bash -o pipefail -c hostname",
		"sudo bash -o pipefail -c hostname",
		"bash --rcfile /dev/null -c hostname",
		"exec hostname",
		"exec -a app hostname",
		"exec -c hostname",
		"eval hostname",
		"eval -- hostname",
		"sudo exec hostname",
	} {
		st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceCommand, []CodingSubAgentCommandResult{
			{Command: command, Succeeded: true},
		}, 1, "")
		if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none performed the host action") {
			t.Fatalf("hidden probe %q must not pass the command contract, got %q %q %d", command, st, sum, n)
		}
		st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceLaunch, []CodingSubAgentCommandResult{
			{Command: command, Succeeded: true},
		}, 1, "")
		if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none looked like launch/build") {
			t.Fatalf("hidden probe %q must not pass a project launch, got %q %q %d", command, st, sum, n)
		}
	}
	for _, command := range []string{
		`bash -o pipefail -c "apt-get install -y sysstat"`,
		"exec apt-get install -y sysstat",
		"eval apt-get install -y sysstat",
	} {
		st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceCommand, []CodingSubAgentCommandResult{
			{Command: command, Succeeded: true},
		}, 1, "")
		if st != codingSubAgentQualityPassed || n != 0 || !strings.Contains(sum, "host command evidence") {
			t.Fatalf("hidden host command %q should pass the command contract, got %q %q %d", command, st, sum, n)
		}
		st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceLaunch, []CodingSubAgentCommandResult{
			{Command: command, Succeeded: true},
		}, 1, "")
		if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none looked like launch/build") {
			t.Fatalf("hidden host command %q must not pass a project launch, got %q %q %d", command, st, sum, n)
		}
	}
	for _, command := range []string{
		"$(hostname)",
		"$(echo true)",
		"(hostname)",
		"{ hostname; }",
		"builtin hostname",
		"builtin -- hostname",
		"command hostname",
		`bash -c '$(hostname)'`,
	} {
		st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceCommand, []CodingSubAgentCommandResult{
			{Command: command, Succeeded: true},
		}, 1, "")
		if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none performed the host action") {
			t.Fatalf("syntax-wrapped probe %q must not pass the command contract, got %q %q %d", command, st, sum, n)
		}
	}
	for _, command := range []string{
		"/usr/bin/hostname",
		"/bin/echo hi",
		"/usr/bin/cat /etc/os-release",
		"/usr/bin/env hostname",
		"./hostname",
		`c:\windows\system32\hostname.exe`,
		"hostname.exe",
	} {
		st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceCommand, []CodingSubAgentCommandResult{
			{Command: command, Succeeded: true},
		}, 1, "")
		if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none performed the host action") {
			t.Fatalf("path probe %q must not pass the command contract, got %q %q %d", command, st, sum, n)
		}
		st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceLaunch, []CodingSubAgentCommandResult{
			{Command: command, Succeeded: true},
		}, 1, "")
		if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none looked like launch/build") && !strings.Contains(sum, "no launch/build command") {
			t.Fatalf("path probe %q must not pass a project launch, got %q %q %d", command, st, sum, n)
		}
	}
	// /bin/echo and /usr/bin/cat are listings. hostname is not, so the launch
	// failure stays the unknown-command message. Absolute apt-get is a host
	// command, not a project launch.
	for _, command := range []string{
		"python.exe",
		"py.exe",
		"./python",
		`c:\python\python.exe`,
		"/usr/bin/python3",
		"bash hostname.exe",
	} {
		st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceLaunch, []CodingSubAgentCommandResult{
			{Command: command, Succeeded: true},
		}, 1, "")
		if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none looked like launch/build") {
			t.Fatalf("bare interpreter %q must not pass a project launch, got %q %q %d", command, st, sum, n)
		}
	}
	st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceLaunch, []CodingSubAgentCommandResult{
		{Command: "/usr/bin/hostname", Succeeded: true},
	}, 1, "")
	if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none looked like launch/build") {
		t.Fatalf("absolute hostname must stay a non-launch, got %q %q %d", st, sum, n)
	}
	st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceCommand, []CodingSubAgentCommandResult{
		{Command: "/usr/bin/apt-get install -y sysstat", Succeeded: true},
	}, 1, "")
	if st != codingSubAgentQualityPassed || n != 0 || !strings.Contains(sum, "host command evidence") {
		t.Fatalf("absolute apt-get should pass the command contract, got %q %q %d", st, sum, n)
	}
	st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceLaunch, []CodingSubAgentCommandResult{
		{Command: "/usr/bin/apt-get install -y sysstat", Succeeded: true},
	}, 1, "")
	if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none looked like launch/build") {
		t.Fatalf("absolute apt-get must not pass a project launch, got %q %q %d", st, sum, n)
	}
	st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceCommand, []CodingSubAgentCommandResult{
		{Command: "/usr/bin/env apt-get install -y sysstat", Succeeded: true},
	}, 1, "")
	if st != codingSubAgentQualityPassed || n != 0 || !strings.Contains(sum, "host command evidence") {
		t.Fatalf("path-qualified env should reveal apt-get, got %q %q %d", st, sum, n)
	}
	// env -C is lowercased to -c before the option table runs. The directory
	// is not the program.
	for _, command := range []string{
		"env -C /tmp hostname",
		"sudo env -C /tmp hostname",
		"/usr/bin/env -C /var hostname",
		"env --chdir /tmp hostname",
	} {
		st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceCommand, []CodingSubAgentCommandResult{
			{Command: command, Succeeded: true},
		}, 1, "")
		if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none performed the host action") {
			t.Fatalf("env chdir probe %q must not pass the command contract, got %q %q %d", command, st, sum, n)
		}
		st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceLaunch, []CodingSubAgentCommandResult{
			{Command: command, Succeeded: true},
		}, 1, "")
		if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none looked like launch/build") {
			t.Fatalf("env chdir probe %q must not pass a project launch, got %q %q %d", command, st, sum, n)
		}
	}
	st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceCommand, []CodingSubAgentCommandResult{
		{Command: "env -C /tmp apt-get install -y sysstat", Succeeded: true},
	}, 1, "")
	if st != codingSubAgentQualityPassed || n != 0 || !strings.Contains(sum, "host command evidence") {
		t.Fatalf("env chdir should reveal apt-get, got %q %q %d", st, sum, n)
	}
	st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceLaunch, []CodingSubAgentCommandResult{
		{Command: "env -C /tmp apt-get install -y sysstat", Succeeded: true},
	}, 1, "")
	if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none looked like launch/build") {
		t.Fatalf("env chdir apt-get must not pass a project launch, got %q %q %d", st, sum, n)
	}
	st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceLaunch, []CodingSubAgentCommandResult{
		{Command: `env -C /tmp .\snake.exe`, Succeeded: true},
	}, 1, "")
	if st != codingSubAgentQualityPassed || n != 0 || !strings.Contains(sum, "launch/build command evidence") {
		t.Fatalf("env chdir should reveal a launch, got %q %q %d", st, sum, n)
	}
	// A relative directory is still the chdir value, not the program.
	for _, command := range []string{
		"env -C build hostname",
		"sudo env -C build hostname",
	} {
		st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceCommand, []CodingSubAgentCommandResult{
			{Command: command, Succeeded: true},
		}, 1, "")
		if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none performed the host action") {
			t.Fatalf("relative env chdir probe %q must not pass the command contract, got %q %q %d", command, st, sum, n)
		}
		st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceLaunch, []CodingSubAgentCommandResult{
			{Command: command, Succeeded: true},
		}, 1, "")
		if st != codingSubAgentQualityFailed || !strings.Contains(sum, "none looked like launch/build") {
			t.Fatalf("relative env chdir probe %q must not pass a project launch, got %q %q %d", command, st, sum, n)
		}
	}
	st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceLaunch, []CodingSubAgentCommandResult{
		{Command: "env -C build ./snake", Succeeded: true},
	}, 1, "")
	if st != codingSubAgentQualityPassed || n != 0 || !strings.Contains(sum, "launch/build command evidence") {
		t.Fatalf("relative env chdir should reveal a launch, got %q %q %d", st, sum, n)
	}
	st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceCommand, []CodingSubAgentCommandResult{
		{Command: "env -C build apt-get install -y sysstat", Succeeded: true},
	}, 1, "")
	if st != codingSubAgentQualityPassed || n != 0 || !strings.Contains(sum, "host command evidence") {
		t.Fatalf("relative env chdir should reveal apt-get, got %q %q %d", st, sum, n)
	}
	if msg := rejectCodingOperationalShellCommand("env -C build rm -rf /tmp/app"); msg == "" {
		t.Fatal("relative env chdir must not hide rm from the operational shell policy")
	}
	for _, command := range []string{
		"/home/test4/app",
		"/usr/bin/python3 app.py",
		"./snake",
		"snake.exe",
		"build_and_run.bat",
		"python.exe app.py",
		"bash snake.exe",
	} {
		st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceLaunch, []CodingSubAgentCommandResult{
			{Command: command, Succeeded: true},
		}, 1, "")
		if st != codingSubAgentQualityPassed || n != 0 || !strings.Contains(sum, "launch/build command evidence") {
			t.Fatalf("project launch %q should pass, got %q %q %d", command, st, sum, n)
		}
	}
	st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceCommand, []CodingSubAgentCommandResult{
		{Command: "$(apt-get install -y sysstat)", Succeeded: true},
	}, 1, "")
	if st != codingSubAgentQualityPassed || n != 0 || !strings.Contains(sum, "host command evidence") {
		t.Fatalf("substitution of a host command should pass, got %q %q %d", st, sum, n)
	}
	for _, command := range []string{
		`bash -o pipefail -c .\snake.exe`,
		`sudo bash -o pipefail -c .\snake.exe`,
	} {
		st, sum, n = summarizeOperationalShellQuality(codingOperationalAcceptanceLaunch, []CodingSubAgentCommandResult{
			{Command: command, Succeeded: true},
		}, 1, "")
		if st != codingSubAgentQualityPassed || n != 0 || !strings.Contains(sum, "launch/build command evidence") {
			t.Fatalf("shell option must not hide a launch %q, got %q %q %d", command, st, sum, n)
		}
	}
}

func TestSummarizeOperationalSubAgentQualityRejectsWorkspaceClear(t *testing.T) {
	st, sum, n := summarizeOperationalSubAgentQualityForTask(
		&TaskItem{Title: "清空当前目录", RequestKind: codingRequestOperational},
		codingSubAgentAudit{AllCommandsRun: []CodingSubAgentCommandResult{{Command: ".\\hello.exe", Succeeded: true, Summary: "ok"}}},
		agent.LoopResult{ToolCalls: 1},
	)
	if st != codingSubAgentQualityFailed || n != 1 || !strings.Contains(sum, "workspace clear") {
		t.Fatalf("leftover launch must not pass a wipe, got %q %q %d", st, sum, n)
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
	if !isCodingInquiryTool("read_file") || !isCodingInquiryTool("code_navigation") || !isCodingInquiryTool("knowledge_image_search") {
		t.Fatal("local inquiry must retain read/navigation tools")
	}
	if isCodingInquiryTool("write_file") || isCodingInquiryTool("todo_write") {
		t.Fatal("local inquiry must not expose mutation/planning tools")
	}
	if !isRemoteCodingInquiryTool("ssh_read_file") || !isRemoteCodingInquiryTool("ssh_list_dir") {
		t.Fatal("remote inquiry must retain SSH read tools")
	}
	if !isRemoteCodingInquiryTool("knowledge_image_search") {
		t.Fatal("remote inquiry must retain read-only image knowledge search")
	}
	if isRemoteCodingInquiryTool("ssh_write_file") || isRemoteCodingInquiryTool("ssh_edit_file") {
		t.Fatal("remote inquiry must not expose SSH write tools")
	}
}

// TestCodingInquiryFilterCompositionDropsRemoteToolsOnLocalHost pins the
// load-bearing composition order in codingSubAgentCallbacks.BuildTools: the
// local inquiry allow-list still carries historical ssh_* union entries, so
// the local-host compatibility filter that runs immediately after it is what
// keeps cross-mode names off a local inquiry surface. If the order flips or
// the host filter is dropped, this test fails instead of the isolation
// silently regressing (review P3-2).
func TestCodingInquiryFilterCompositionDropsRemoteToolsOnLocalHost(t *testing.T) {
	tools := testToolDefs("read_file", "bash", "ssh_read_file", "ssh_bash", "write_file")
	filtered := filterCodingStaticCompatibilitySurface(codingStaticCompatibilityHostLocal, filterCodingInquiryTools(tools))
	names := map[string]bool{}
	for _, def := range filtered {
		names[extractToolName(def)] = true
	}
	for _, want := range []string{"read_file", "bash"} {
		if !names[want] {
			t.Fatalf("local inquiry surface must keep %q, got %v", want, names)
		}
	}
	for _, ban := range []string{"ssh_read_file", "ssh_bash", "write_file"} {
		if names[ban] {
			t.Fatalf("local inquiry surface must drop %q, got %v", ban, names)
		}
	}
}

// TestCodingOperationalFilterIsSelfContained pins that the local operational
// allow-list carries no ssh_* union entries: unlike the inquiry list, its
// cross-mode isolation must not depend on the downstream host filter
// (review P3-2).
func TestCodingOperationalFilterIsSelfContained(t *testing.T) {
	tools := testToolDefs("bash", "read_file", "ssh_write_file", "ssh_bash")
	filtered := filterCodingOperationalTools(tools)
	for _, def := range filtered {
		if name := extractToolName(def); strings.HasPrefix(name, "ssh_") {
			t.Fatalf("local operational allow-list must not admit %q before the host filter", name)
		}
	}
}

func TestCodingOperationalToolFiltersAreNonMutating(t *testing.T) {
	for _, name := range []string{"bash", "read_file", "Glob", "ripgrep", "code_navigation", "knowledge_image_search"} {
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

func TestCodingInquiryShellAllowsHostStatusCommands(t *testing.T) {
	// These are the commands a server-status inquiry actually runs. df was
	// rejected beside du, so the agent could read /proc/diskstats and still
	// not report capacity.
	for _, command := range []string{
		"df -h",
		"df -h / /home",
		"df --si",
		"df --no-sync",
		"free -h",
		"free --si",
		"free -s 1 -c 2",
		"free --seconds=1 --count=2",
		"free --sec 1 --cou 2",
		"free -c 2",
		"uptime",
		"vmstat",
		"vmstat -s",
		"vmstat 1 3",
		"vmstat -p sda",
		"vmstat --unit m",
		"vmstat --un m",
		"vmstat --pa sda 1 3",
		"ps aux",
		"ps aux --sort=-%mem",
		"pstree -p",
		"pgrep -a sshd",
		"iostat -x",
		"iostat -x sda",
		"iostat sda 2 6",
		"iostat 0",
		"iostat -x 0",
		"iostat sda 0",
		"mpstat",
		"mpstat 0",
		"mpstat -P 1",
		"mpstat -P ALL 2 5",
		"ss -tulpn",
		"ss -s",
		"ss --extended",
		"ss --dccp",
		"netstat -tulpn",
		"lsblk -f",
		"lscpu",
		"findmnt",
		"findmnt -t ext4",
		"findmnt --pairs",
		"top -bn1",
		"top -bn=1",
		"top -b -n 1",
		"top -b -n=1",
		"top -p 1 -bn1",
		"top --batch-mode --iterations=1",
		"top --batch -n 1",
		"top -b --iter 1",
		"top -b -n 1 --delay 2",
		"top --pid 1 -bn1",
		"top -bn1 --threads",
		"top -bn1 | head",
		"uptime && free -h && df -h",
		"df -h | head",
	} {
		if msg := rejectCodingInquiryShellCommand(command); msg != "" {
			t.Fatalf("read-only host status command should pass: %q: %s", command, msg)
		}
	}
	for _, command := range []string{
		"ss -K",
		"ss --kill",
		"ss --k",
		"ss --ki",
		"ss -tulpnK",
		"ss -E",
		"ss --events",
		"ss --ev",
		"ss -e",
		"ss -D /tmp/ss.diag",
		"ss --diag /tmp/ss.diag",
		"ss --di=/tmp/ss.diag",
		"ss -d",
		"kill 1",
		"pkill sshd",
		"killall sshd",
		"top",
		"top -b",
		"top -n 1",
		"top -bn4",
		"top -bdn1",
		"top -b -d 5",
		"top --batch-mode",
		"top -b --delay 5",
		"top -b --iter 4",
		"top -b -n 1 --s 1",
		"free -s 1",
		"free -s 1.5",
		"free --sec 1",
		"free -c 40",
		"vmstat 1",
		"vmstat 0",
		"vmstat 1 500",
		"vmstat --unit m 1",
		"vmstat --un m 1",
		"vmstat --p 1 2",
		"iostat 1",
		"iostat -x 1",
		"iostat sda 1",
		"mpstat 2",
		"mpstat -P 1 2",
		"mpstat -N 0 2",
		"netstat -c",
		"netstat --continuous",
		"netstat --cont",
		"netstat -tulnc",
		"findmnt --poll",
		"findmnt --po",
		"findmnt --pol",
		"findmnt -p",
		"dmesg -c",
		"pidstat -e rm -rf /",
		"mount /dev/vdb /data",
		"df --sync",
		"df --s",
		"df --sy",
		"df -h --sync",
		"df -h > /tmp/usage.txt",
		"df -h 2>/dev/null",
	} {
		if msg := rejectCodingInquiryShellCommand(command); msg == "" {
			t.Fatalf("mutating or non-inspection command should stay rejected: %q", command)
		}
	}
}

func TestGuardedOperationalCommandAsksTheUserInsteadOfDeadEnding(t *testing.T) {
	// A run/build guardrail decides what runs without asking, so a command it
	// turns down has to reach the user. Dead-ending here is what made an agent
	// invent a network fault to explain a refusal it could not act on.
	const command = "git ls-remote --upload-pack=whoami origin"
	rejection := rejectCodingOperationalShellCommand(command)
	if rejection == "" {
		t.Fatal("the guardrail should have turned this command down")
	}

	var asked ScopeApprovalRequest
	callbacks := &codingSubAgentCallbacks{subagent: &CodingSubAgent{
		scopeApproval: newScopeApprovalState(func(req ScopeApprovalRequest) ScopeApprovalDecision {
			asked = req
			return ScopeApprovalAllowOnce
		}, false),
	}}
	if msg := callbacks.approveGuardedShellCommand(command, `D:\repo`, rejection); msg != "" {
		t.Fatalf("an approved command should run, got %q", msg)
	}
	if asked.ToolName != "bash" || asked.Kind != localHighRiskApprovalKind || asked.Path != command {
		t.Fatalf("approval request = %#v", asked)
	}
	if asked.Message != rejection {
		t.Fatalf("the user must see why it was guarded: %q", asked.Message)
	}

	denying := &codingSubAgentCallbacks{subagent: &CodingSubAgent{
		scopeApproval: newScopeApprovalState(func(ScopeApprovalRequest) ScopeApprovalDecision {
			return ScopeApprovalDeny
		}, false),
	}}
	if msg := denying.approveGuardedShellCommand(command, `D:\repo`, rejection); msg != rejection {
		t.Fatalf("a denied command must stay rejected, got %q", msg)
	}
	// Without an approval channel the guardrail is the whole answer.
	bare := &codingSubAgentCallbacks{subagent: &CodingSubAgent{}}
	if msg := bare.approveGuardedShellCommand(command, `D:\repo`, rejection); msg != rejection {
		t.Fatalf("a missing approval channel must keep the guardrail, got %q", msg)
	}
}

func TestGuardCodingShellCommandOrdersHardBlocksBeforeApproval(t *testing.T) {
	prompts := 0
	newGuard := func(kind codingRequestKind, decide ScopeApprovalDecision) *codingSubAgentCallbacks {
		prompts = 0
		return &codingSubAgentCallbacks{
			task: &TaskItem{RequestKind: kind},
			subagent: &CodingSubAgent{scopeApproval: newScopeApprovalState(func(ScopeApprovalRequest) ScopeApprovalDecision {
				prompts++
				return decide
			}, false)},
		}
	}

	// A repository inquiry never reaches the user: its report claims the turn
	// modified nothing, so there is nothing to approve away.
	guard := newGuard(codingRequestInquiry, ScopeApprovalAllowOnce)
	if msg := guard.guardCodingShellCommand("npm install left-pad", `D:\repo`); msg == "" {
		t.Fatal("an inquiry must not run a dependency install")
	}
	if prompts != 0 {
		t.Fatalf("an inquiry must not offer approval, prompts = %d", prompts)
	}
	if msg := guard.guardCodingShellCommand("git status --short", `D:\repo`); msg != "" {
		t.Fatalf("a read-only inspection should pass an inquiry: %s", msg)
	}

	// A run/build guardrail asks, and the user's answer decides.
	guard = newGuard(codingRequestOperational, ScopeApprovalAllowOnce)
	if msg := guard.guardCodingShellCommand("git fetch origin", `D:\repo`); msg != "" {
		t.Fatalf("an approved command should run: %s", msg)
	}
	if prompts != 1 {
		t.Fatalf("prompts = %d, want one approval", prompts)
	}
	guard = newGuard(codingRequestOperational, ScopeApprovalDeny)
	if msg := guard.guardCodingShellCommand("git fetch origin", `D:\repo`); msg == "" {
		t.Fatal("a denied command must not run")
	}

	// One command, one prompt: approving the mode guardrail must not queue up a
	// second prompt from the high-risk guardrail the same command also trips.
	guard = newGuard(codingRequestOperational, ScopeApprovalAllowOnce)
	if msg := guard.guardCodingShellCommand("git reset --hard HEAD", `D:\repo`); msg != "" {
		t.Fatalf("an approved command should run: %s", msg)
	}
	if prompts != 1 {
		t.Fatalf("prompts = %d, want exactly one for a single command", prompts)
	}

	// Outside a guarded mode the same command still reaches the high-risk gate.
	guard = newGuard(codingRequestImplementation, ScopeApprovalDeny)
	if msg := guard.guardCodingShellCommand("git reset --hard HEAD", `D:\repo`); msg == "" {
		t.Fatal("a denied high-risk command must not run")
	}
	if prompts != 1 {
		t.Fatalf("prompts = %d, want one high-risk approval", prompts)
	}

	// A silenced git self-check is a hard block: no prompt, and no answer opens
	// it, including one already given for the run/build guardrail it trips too.
	guard = newGuard(codingRequestImplementation, ScopeApprovalAllowOnce)
	if msg := guard.guardCodingShellCommand("git status 2>/dev/null", `D:\repo`); msg == "" {
		t.Fatal("a silenced git self-check must stay blocked")
	}
	if prompts != 0 {
		t.Fatalf("a hard block must not offer approval, prompts = %d", prompts)
	}
	guard = newGuard(codingRequestOperational, ScopeApprovalAllowOnce)
	if msg := guard.guardCodingShellCommand("git status 2>/dev/null", `D:\repo`); msg == "" {
		t.Fatal("approval must not unlock a hard block")
	}
}

func TestGuardRemoteShellCommandMirrorsTheLocalGuardOrdering(t *testing.T) {
	prompts := 0
	newGuard := func(inquiry, operational bool, decide ScopeApprovalDecision) *remoteCodingCallbacks {
		prompts = 0
		return &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{
			readOnlyInquiry:    inquiry,
			operationalRequest: operational,
			highRiskApproval: newRemoteHighRiskApprovalState(func(ScopeApprovalRequest) ScopeApprovalDecision {
				prompts++
				return decide
			}, false),
		}}
	}

	guard := newGuard(true, false, ScopeApprovalAllowOnce)
	msg, record := guard.guardRemoteShellCommand("npm install left-pad", "/srv/repo")
	if msg == "" {
		t.Fatal("an inquiry must not run a dependency install")
	}
	if !record {
		t.Fatal("a mode-guardrail rejection belongs in the run evidence")
	}
	if prompts != 0 {
		t.Fatalf("an inquiry must not offer approval, prompts = %d", prompts)
	}
	if msg, _ = guard.guardRemoteShellCommand("git status --short", "/srv/repo"); msg != "" {
		t.Fatalf("a read-only inspection should pass an inquiry: %s", msg)
	}

	guard = newGuard(false, true, ScopeApprovalAllowOnce)
	if msg, _ = guard.guardRemoteShellCommand("git fetch origin", "/srv/repo"); msg != "" {
		t.Fatalf("an approved command should run: %s", msg)
	}
	if prompts != 1 {
		t.Fatalf("prompts = %d, want one approval", prompts)
	}
	guard = newGuard(false, true, ScopeApprovalDeny)
	if msg, _ = guard.guardRemoteShellCommand("git fetch origin", "/srv/repo"); msg == "" {
		t.Fatal("a denied command must not run")
	}

	// One command, one prompt.
	guard = newGuard(false, true, ScopeApprovalAllowOnce)
	if msg, _ = guard.guardRemoteShellCommand("git reset --hard HEAD", "/srv/repo"); msg != "" {
		t.Fatalf("an approved command should run: %s", msg)
	}
	if prompts != 1 {
		t.Fatalf("prompts = %d, want exactly one for a single command", prompts)
	}

	// A silenced git self-check stays blocked, with or without an approval.
	guard = newGuard(false, false, ScopeApprovalAllowOnce)
	if msg, _ = guard.guardRemoteShellCommand("git status 2>/dev/null", "/srv/repo"); msg == "" {
		t.Fatal("a silenced git self-check must stay blocked")
	}
	if prompts != 0 {
		t.Fatalf("a hard block must not offer approval, prompts = %d", prompts)
	}
	guard = newGuard(false, true, ScopeApprovalAllowOnce)
	if msg, _ = guard.guardRemoteShellCommand("git status 2>/dev/null", "/srv/repo"); msg == "" {
		t.Fatal("approval must not unlock a hard block")
	}
}

func TestRemoteTaskModeGuardIsNotSatisfiedByAStickyHighRiskGrant(t *testing.T) {
	prompts := 0
	state := newRemoteHighRiskApprovalState(func(ScopeApprovalRequest) ScopeApprovalDecision {
		prompts++
		return ScopeApprovalDeny
	}, true)

	if msg := state.checkTaskModeGuard("git push origin main", "/srv/repo", "guarded"); msg == "" {
		t.Fatal("a sticky full-access grant must not silently satisfy a mode guardrail")
	}
	if prompts != 1 {
		t.Fatalf("prompts = %d, want the user to be asked despite full access", prompts)
	}
}

func TestRecursiveDeleteStillRequiresHighRiskApproval(t *testing.T) {
	if msg := rejectDisallowedCodingBashCommand(`Remove-Item -Recurse -Force *`); msg == "" {
		t.Fatal("recursive delete must still be classified as high-risk")
	}
	blocked := newScopeApprovalState(nil, false)
	if msg := blocked.checkHighRisk("bash", `Remove-Item -Recurse -Force *`, `D:\repo`, `D:\repo`, "high risk"); msg == "" {
		t.Fatal("without approval, recursive delete must stay blocked")
	}
	full := newScopeApprovalState(nil, true)
	if msg := full.checkHighRisk("bash", `Remove-Item -Recurse -Force *`, `D:\repo`, `D:\repo`, "high risk"); msg != "" {
		t.Fatalf("Full Control must auto-allow project-scoped high-risk delete, got %q", msg)
	}
}

func TestTaskModeGuardIsNotSatisfiedByAStickyHighRiskGrant(t *testing.T) {
	// A sticky "allow risky commands" answer is about danger, not about turning
	// a run/build turn into something wider, so it must not silently widen the
	// task mode without the user ever seeing a prompt.
	prompts := 0
	state := newScopeApprovalState(func(ScopeApprovalRequest) ScopeApprovalDecision {
		prompts++
		return ScopeApprovalFullAccess
	}, false)

	if msg := state.checkHighRisk("bash", "git reset --hard HEAD", `D:\repo`, `D:\repo`, "high risk"); msg != "" {
		t.Fatalf("full access should allow the high-risk command, got %q", msg)
	}
	// checkHighRisk has now stored the sticky grant and stops prompting.
	if msg := state.checkHighRisk("bash", "git clean -fd", `D:\repo`, `D:\repo`, "high risk"); msg != "" || prompts != 1 {
		t.Fatalf("sticky high-risk grant = %q after %d prompts", msg, prompts)
	}
	if msg := state.checkTaskModeGuard("bash", "npm install left-pad", `D:\repo`, `D:\repo`, "guarded"); msg != "" {
		t.Fatalf("the user allowed it at the prompt, got %q", msg)
	}
	if prompts != 2 {
		t.Fatalf("a task-mode guard must still prompt, prompts = %d", prompts)
	}

	// And a mode guard never installs a sticky grant of its own.
	silent := newScopeApprovalState(nil, false)
	if msg := silent.checkTaskModeGuard("bash", "npm install left-pad", `D:\repo`, `D:\repo`, "guarded"); msg != "guarded" {
		t.Fatalf("no approval channel must keep the guardrail, got %q", msg)
	}
}

func TestCodingGitInspectionSubcommandsStayAvailable(t *testing.T) {
	// Read-only git subcommands must survive both gates: an agent that only
	// wants to look at refs should never be told to ask for an implementation
	// change, and the rejection text must not call a read a mutation.
	for _, command := range []string{
		"git ls-remote --heads origin",
		"git ls-tree -r HEAD",
		"git rev-list --count HEAD",
		"git describe --tags",
		"git merge-base main HEAD",
		"git show-ref --tags",
		"git branch",
		"git branch -a -v",
		"git branch --list --sort=-committerdate",
		"git tag",
		"git tag -l v1.*",
		"git tag -n5",
		"git remote -v",
		"git remote show origin",
		"git remote get-url origin",
		"git remote get-url --push origin",
		"git branch --show-current",
		"git branch --merged main",
		"git branch --no-merged main",
		"git branch --contains HEAD",
		"git branch --list 'feature/*'",
		"git tag --contains HEAD",
		"git tag --no-merged main",
		"git tag --sort=-v:refname",
		// Clustered short flags are the usual spelling of `-a -v`.
		"git branch -av",
		"git branch -avv",
		"git tag -ln9",
	} {
		if msg := rejectCodingInquiryShellCommand(command); msg != "" {
			t.Fatalf("read-only git command should pass an inquiry: %q: %s", command, msg)
		}
		if msg := rejectCodingOperationalShellCommand(command); msg != "" {
			t.Fatalf("read-only git command should pass a run/build request: %q: %s", command, msg)
		}
	}
	for _, command := range []string{
		"git branch -D stale",
		"git branch --delete stale",
		"git branch feature/new",
		"git branch -m old new",
		"git branch --set-upstream-to=origin/main",
		// Before git 2.19 `-l` meant --create-reflog, so it must not license a
		// branch name the way --list does.
		"git branch -l newbranch",
		// The operand of a value-taking flag is consumed, so a name after it is
		// still a creation.
		"git branch --sort=committerdate newbranch",
		"git branch --sort committerdate newbranch",
		// A cluster is only as safe as its least safe letter, and it never
		// enters list mode, so it cannot license a branch name either.
		"git branch -vd stale",
		"git branch -av newbranch",
		"git tag -ld v1.0.0",
		"git tag v1.0.0",
		"git tag -d v1.0.0",
		"git tag -a v1.0.0 -m release",
		"git remote add upstream https://example.invalid/x.git",
		"git remote set-url origin https://example.invalid/y.git",
		"git remote rename origin old",
		"git push origin main",
		"git config user.name test",
	} {
		if msg := rejectCodingInquiryShellCommand(command); msg == "" {
			t.Fatalf("ref-mutating git command should be rejected by an inquiry: %q", command)
		}
		if msg := rejectCodingOperationalShellCommand(command); msg == "" {
			t.Fatalf("ref-mutating git command should be rejected by a run/build request: %q", command)
		}
	}
	// `--output=<path>` makes an otherwise read-only git command write a file
	// without ever using a shell redirect, so the redirect guard cannot see it.
	for _, command := range []string{
		"git diff --output=leak.txt",
		"git diff --output leak.txt",
		"git log --output=leak.txt",
		"git show --output=leak.txt HEAD",
	} {
		if msg := rejectCodingInquiryShellCommand(command); msg == "" {
			t.Fatalf("git file-writing option should be rejected by an inquiry: %q", command)
		}
		if msg := rejectCodingOperationalShellCommand(command); msg == "" {
			t.Fatalf("git file-writing option should be rejected by a run/build request: %q", command)
		}
	}
	// ls-remote is the one allowed subcommand that takes a URL, and git's ext/fd
	// transport helpers execute their payload as a command line.
	for _, command := range []string{
		"git ls-remote ext::sh -c whoami",
		"git ls-remote 'ext::sh -c whoami'",
		"git ls-remote fd::7",
		"git ls-remote --upload-pack=whoami origin",
		"git ls-remote --exec=whoami origin",
		"git remote show ext::sh -c whoami",
		"git grep -Owhoami pattern",
		"git grep --open-files-in-pager=whoami pattern",
		// --exec-path relocates the directory git loads git-remote-* and other
		// helper programs from, and it must not be mistaken for --exec.
		"git --exec-path=/tmp/evil ls-remote origin",
		"git --exec-path=/tmp/evil status",
	} {
		if msg := rejectCodingInquiryShellCommand(command); msg == "" {
			t.Fatalf("git transport execution vector should be rejected by an inquiry: %q", command)
		}
		if msg := rejectCodingOperationalShellCommand(command); msg == "" {
			t.Fatalf("git transport execution vector should be rejected by a run/build request: %q", command)
		}
	}
	// A search pattern has the same `a::b` shape as a transport URL and must not
	// be mistaken for one.
	if msg := rejectCodingInquiryShellCommand("git grep std::vector"); msg != "" {
		t.Fatalf("a pattern containing :: should still be searchable: %s", msg)
	}
	// Inline config before the subcommand can name a pager, an alias, or a
	// transport helper, so it must not ride along on a read-only subcommand.
	for _, command := range []string{
		"git -c core.pager=whoami log",
		"git -c protocol.ext.allow=always ls-remote origin",
		"git --config-env=core.pager=LEAK log",
	} {
		if msg := rejectCodingInquiryShellCommand(command); msg == "" {
			t.Fatalf("git config injection should be rejected by an inquiry: %q", command)
		}
		if msg := rejectCodingOperationalShellCommand(command); msg == "" {
			t.Fatalf("git config injection should be rejected by a run/build request: %q", command)
		}
	}
	// `-c` after the subcommand is git's combined-diff flag, not config.
	if msg := rejectCodingInquiryShellCommand("git log -c --oneline"); msg != "" {
		t.Fatalf("combined-diff flag should stay available: %s", msg)
	}
	// An inline shell script must not become a way to run the git commands the
	// gate just rejected.
	for _, command := range []string{
		"bash -c 'git push origin main'",
		"sh -c 'git push origin main'",
		"zsh -c 'git commit -am wip'",
	} {
		if msg := rejectCodingOperationalShellCommand(command); msg == "" {
			t.Fatalf("wrapped git mutation should be rejected by a run/build request: %q", command)
		}
	}
	// The old wording accused read-only commands of being mutations, which sent
	// agents chasing imaginary network faults instead of surfacing the real
	// allow-list miss.
	msg := rejectCodingOperationalShellCommand("git fetch origin")
	if msg == "" {
		t.Fatal("git fetch should stay unavailable for a run/build request")
	}
	if strings.Contains(strings.ToLower(msg), "mutating") {
		t.Fatalf("rejection must name the allow-list miss rather than claim a mutation: %s", msg)
	}
	if !strings.Contains(msg, "git fetch") {
		t.Fatalf("rejection should name the offending subcommand: %s", msg)
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

func TestParseCodingWorkbenchPlanJSONPreservesDeclaredFiles(t *testing.T) {
	tasks := parseCodingWorkbenchPlan(`{"steps":[{"title":"change auth","description":"implement auth","files":["internal/auth/service.go"]}]}`)
	if len(tasks) != 1 || len(tasks[0].Files) != 1 || tasks[0].Files[0] != "internal/auth/service.go" {
		t.Fatalf("tasks=%+v", tasks)
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

func TestFormatCodingWorkbenchPlanMarkdownIncludesDeclaredFiles(t *testing.T) {
	md := formatCodingWorkbenchPlanMarkdown("ship auth", []*v2.TaskItem{{Index: 1, Title: "change auth", Files: []string{"internal/auth/service.go"}}})
	if !strings.Contains(md, "Files:") || !strings.Contains(md, "internal/auth/service.go") {
		t.Fatalf("md=%q", md)
	}
}

func TestCodingWorkbenchRunHeader(t *testing.T) {
	if got := codingWorkbenchRunHeader(codingRequestImplementation, false, 1, []v2.TaskRunResult{{Status: v2.TaskPassed}}); got != "Coding complete" {
		t.Fatalf("single pass: %q", got)
	}
	if got := codingWorkbenchRunHeader(codingRequestImplementation, true, 3, []v2.TaskRunResult{
		{Status: v2.TaskPassed}, {Status: v2.TaskPassed}, {Status: v2.TaskPassed},
	}); !strings.Contains(got, "completed 3 planned steps") {
		t.Fatalf("all pass: %q", got)
	}
	if got := codingWorkbenchRunHeader(codingRequestImplementation, true, 3, []v2.TaskRunResult{
		{Status: v2.TaskPassed}, {Status: v2.TaskFailed}, {Status: v2.TaskSkipped},
	}); !strings.Contains(got, "partially complete") {
		t.Fatalf("partial: %q", got)
	}
	if got := codingWorkbenchRunHeader(codingRequestInquiry, false, 1, []v2.TaskRunResult{{Status: v2.TaskPassed}}); got != "Repository analysis complete" {
		t.Fatalf("inquiry pass: %q", got)
	}
	if got := codingWorkbenchRunHeader(codingRequestInquiry, false, 1, []v2.TaskRunResult{{Status: v2.TaskFailed}}); got != "Repository analysis incomplete" {
		t.Fatalf("inquiry failure: %q", got)
	}
	if got := codingWorkbenchRunHeader(codingRequestOperational, false, 1, []v2.TaskRunResult{{Status: v2.TaskPassed}}); got != "Task complete" {
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
	}); !strings.Contains(got, "Repository analysis partially complete") || strings.Contains(got, "Coding") {
		t.Fatalf("inquiry multi-step header must not claim coding: %q", got)
	}
}

func TestRepositoryInquiryHeaderAndReportStateNoFilesWereModified(t *testing.T) {
	body := formatCodingWorkbenchUserAnswer(codingRequestInquiry, []v2.TaskRunResult{{Status: v2.TaskPassed, Summary: "analysis"}}, false)
	if strings.Contains(body, "Coding complete") || strings.Contains(body, "## ") || !strings.Contains(body, "Read-only check: no files were modified.") {
		t.Fatalf("unexpected inquiry report: %q", body)
	}
}

func TestFormatCodingAgentUserFinishHidesScorecard(t *testing.T) {
	failed := formatCodingAgentUserFinish([]v2.TaskRunResult{{
		Title:        "hello world",
		Status:       v2.TaskFailed,
		Summary:      "Completed: created hello_world.cpp and successfully compiled.",
		Error:        "coding SubAgent quality audit failed: 1 command(s) failed: cl",
		FilesCreated: []string{"hello_world.cpp"},
	}}, false)
	if strings.Contains(failed, "## ") || strings.Contains(failed, "Execution report") || strings.Contains(failed, "successfully compiled") {
		t.Fatalf("failed finish should not keep the scorecard or success claim: %q", failed)
	}
	if strings.Contains(failed, "quality audit") || strings.Contains(failed, "did not finish") {
		t.Fatalf("failed finish should not use audit or generic incomplete phrasing: %q", failed)
	}
	if !strings.Contains(failed, "`cl` failed") || !strings.Contains(failed, "hello_world.cpp") {
		t.Fatalf("failed finish should name the command and file: %q", failed)
	}
	passed := formatCodingAgentUserFinish([]v2.TaskRunResult{{
		Title:   "hello world",
		Status:  v2.TaskPassed,
		Summary: "Created hello_world.cpp and ran it.",
	}}, false)
	if passed != "Created hello_world.cpp and ran it." {
		t.Fatalf("passed finish should be the model summary, got %q", passed)
	}
	zhFailed := formatCodingAgentUserFinish([]v2.TaskRunResult{{
		Title:        "hello world",
		Status:       v2.TaskFailed,
		Summary:      "已完成：创建了 hello_world.cpp 并成功编译运行。\n\n## 验证结果\ncl 通过\n\n## 涉及文件\nhello_world.cpp",
		Error:        "coding SubAgent quality audit failed: 1 command(s) failed: cl",
		FilesCreated: []string{"hello_world.cpp"},
	}}, false)
	if strings.Contains(zhFailed, "## ") || strings.Contains(zhFailed, "验证结果") || strings.Contains(zhFailed, "涉及文件") || strings.Contains(zhFailed, "成功编译") {
		t.Fatalf("chinese failed finish should drop audit sections and success claim: %q", zhFailed)
	}
	if strings.Contains(zhFailed, "quality audit") || strings.Contains(zhFailed, "did not finish") {
		t.Fatalf("chinese failed finish should not use audit or generic incomplete phrasing: %q", zhFailed)
	}
	if !strings.Contains(zhFailed, "`cl` failed") || !strings.Contains(zhFailed, "hello_world.cpp") {
		t.Fatalf("chinese failed finish should name the command and file: %q", zhFailed)
	}
	if got := formatCodingAgentVisibleError("coding SubAgent quality audit failed: 1 command(s) failed: go test ./pkg -> compile failed"); got != "`go test ./pkg` failed: compile failed" {
		t.Fatalf("visible error = %q", got)
	}
}

func TestFormatCodingAgentSkippedPrefersVisibleErrorOverLedgerSummary(t *testing.T) {
	paragraph := formatCodingAgentResultParagraph(v2.TaskRunResult{
		Title:   "scaffold project",
		Status:  v2.TaskSkipped,
		Summary: "Execution ledger attempt: 83e762b9-6a51-44e1-96f1-9fb96d366aff",
		Error:   "writer execution requires a successful read-only workspace baseline",
	})
	if !strings.HasPrefix(paragraph, "Skipped scaffold project: writer execution requires a successful read-only workspace baseline") {
		t.Fatalf("skipped paragraph should lead with the blocking reason: %q", paragraph)
	}
	if strings.Contains(paragraph, "Execution ledger attempt") {
		t.Fatalf("skipped paragraph should not surface the ledger noise summary: %q", paragraph)
	}
	if got := formatCodingAgentResultParagraph(v2.TaskRunResult{
		Title:   "review",
		Status:  v2.TaskSkipped,
		Summary: "Child results require an explicit review handoff.",
	}); got != "Child results require an explicit review handoff." {
		t.Fatalf("skipped paragraph without error should keep the summary: %q", got)
	}
	if got := formatCodingAgentResultParagraph(v2.TaskRunResult{
		Status: v2.TaskSkipped,
		Error:  "workspace probe failed",
	}); got != "Skipped Untitled task: workspace probe failed" {
		t.Fatalf("untitled skipped paragraph should still name the reason: %q", got)
	}
}

func TestIsCodingAgentUserProgressTextKeepsTrailOnly(t *testing.T) {
	if !isCodingAgentUserProgressText(`Coding Agent Event: {"version":1,"agent":"coding","event":"tool_started"}`) {
		t.Fatal("structured coding event should stay visible")
	}
	if !isCodingAgentUserProgressText("Coding Agent: running T1 - Write hello") {
		t.Fatal("legacy coding status should stay visible")
	}
	for _, banner := range []string{
		"全功能编程工作台：开始执行",
		"全功能远程编程：使用 SSH 会话 ssh_1 开始执行",
		"T1/2: write files",
		"执行步骤：\n☐ T1 write files",
		"① Local coding execution: 2 tasks",
	} {
		if isCodingAgentUserProgressText(banner) {
			t.Fatalf("board banner should stay off the chat trail: %q", banner)
		}
	}
}

func TestEmitCodingAgentUserProgressDropsBoardBanners(t *testing.T) {
	var got []string
	emitCodingAgentUserProgress(func(s string) { got = append(got, s) }, "全功能编程工作台：开始执行")
	emitCodingAgentUserProgress(func(s string) { got = append(got, s) }, "T1/2: write files")
	emitCodingAgentUserProgress(func(s string) { got = append(got, s) }, "Coding Agent: running T1 - write files")
	if len(got) != 1 || !strings.HasPrefix(got[0], "Coding Agent:") {
		t.Fatalf("only the coding trail line should be forwarded: %#v", got)
	}
}

func TestWrapCodingAgentReasoningTokenPrefixesThinking(t *testing.T) {
	var got []string
	wrapped := wrapCodingAgentReasoningToken(func(s string) { got = append(got, s) })
	wrapped("Created hello.cpp.")
	wrapped("\x01already thinking")
	wrapped("Browser: leaked")
	if wrapCodingAgentReasoningToken(nil) != nil {
		t.Fatal("nil callback should stay nil")
	}
	if len(got) != 3 {
		t.Fatalf("unexpected tokens: %#v", got)
	}
	if got[0] != "\x01Created hello.cpp." {
		t.Fatalf("live model prose should fold into thinking: %q", got[0])
	}
	if got[1] != "\x01already thinking" {
		t.Fatalf("already-prefixed thinking should pass through: %q", got[1])
	}
	if got[2] != "\x01leaked" {
		t.Fatalf("Browser prefix should be stripped into thinking: %q", got[2])
	}
}

func TestIsCodingAgentUserProgressTextKeepsPrefixedRemoteEvents(t *testing.T) {
	event := `Coding Agent Event: {"version":1,"agent":"coding","event":"tool_started"}`
	if !isCodingAgentUserProgressText(event) {
		t.Fatal("remote tool events must stay on the trail")
	}
	if isCodingAgentUserProgressText("   · T1 merged remote git worktree") {
		t.Fatal("prefixed board chrome must not look like a trail event")
	}
}

func TestStripCodingAgentAuditSectionsKeepsPlanApproval(t *testing.T) {
	card := formatPendingPlanApprovalText("**\u76ee\u6807**: write a hello binary\n\n### T1: write\n\u63cf\u8ff0: add hello\nFiles: hello.cpp\n\u4f9d\u8d56: T0\n### T2: build\n", 2)
	if strings.Contains(card, "\u63cf\u8ff0:") || strings.Contains(card, "Files:") || strings.Contains(card, "\u4f9d\u8d56:") || strings.Contains(card, "**\u76ee\u6807**") {
		t.Fatalf("user plan card should drop form labels: %q", card)
	}
	if strings.Contains(card, "## 需求理解") {
		t.Fatalf("plan card must not reuse the stream restatement heading: %q", card)
	}
	if !strings.Contains(card, "add hello") || !strings.Contains(card, "hello.cpp") {
		t.Fatalf("user plan card should keep step facts: %q", card)
	}
	got := stripCodingAgentAuditSections(card)
	if !strings.Contains(got, "### T1: write") || !strings.Contains(got, "### T2: build") || !strings.Contains(got, "/plan approve") {
		t.Fatalf("plan approval card must keep ### T steps: %q", got)
	}
	got = formatCodingSubAgentUserAnswer(&CodingSubAgentResult{
		Status:  TaskExecPassed,
		Summary: "Created hello.cpp.\n\n## \u9a8c\u8bc1\u7ed3\u679c\ncl passed",
	})
	if strings.Contains(got, "\u9a8c\u8bc1\u7ed3\u679c") || strings.Contains(got, "## ") {
		t.Fatalf("delegate/IM answer still has audit headings: %q", got)
	}
	if !strings.Contains(got, "Created hello.cpp.") {
		t.Fatalf("delegate/IM answer lost engineer prose: %q", got)
	}
}

func TestFormatCodingAgentUserFinishDropsPlanBoardChrome(t *testing.T) {
	got := formatCodingAgentUserFinish([]v2.TaskRunResult{{
		Status:  v2.TaskPassed,
		Summary: "Created hello.cpp.\n\n### T2: build\n状态: success\n\n### 计划执行结果\n远程编程完成\n\n执行步骤：\n☑ T1 write",
	}}, false)
	if strings.Contains(got, "计划执行结果") || strings.Contains(got, "执行步骤") || strings.Contains(got, "SSH") || strings.Contains(got, "### T") {
		t.Fatalf("plan board chrome leaked into finish: %q", got)
	}
	if !strings.Contains(got, "Created hello.cpp.") {
		t.Fatalf("engineer prose missing: %q", got)
	}
}

func TestRemoteCodingStepToRunResultAndSkippedRemainder(t *testing.T) {
	step := &v2.TaskItem{Index: 1, Title: "write hello"}
	got := remoteCodingStepToRunResult(step, &RemoteCodingSubAgentResult{
		Status:       "failed",
		Summary:      "Created hello.cpp.",
		Error:        "coding SubAgent quality audit failed: 1 command(s) failed: cl",
		FilesCreated: []string{"hello.cpp"},
	})
	if got.Status != v2.TaskFailed || got.Title != "write hello" || got.FilesCreated[0] != "hello.cpp" {
		t.Fatalf("unexpected run result: %#v", got)
	}
	results := appendCodingWorkbenchSkippedResults([]v2.TaskRunResult{got}, []*v2.TaskItem{
		step,
		{Index: 2, Title: "build"},
	}, []codingWorkbenchStepStatus{{Index: 2, Status: codingStepSkipped, Summary: "skipped: prior step failed"}})
	if len(results) != 2 || results[1].Status != v2.TaskSkipped || results[1].Title != "build" {
		t.Fatalf("skipped remainder: %#v", results)
	}
	finish := formatCodingWorkbenchUserAnswer(codingRequestImplementation, results, false)
	if strings.Contains(finish, "### T") || strings.Contains(finish, "SSH") || strings.Contains(finish, "执行报告") {
		t.Fatalf("remote finish must match local engineer prose: %q", finish)
	}
}

func TestEmitCodingAgentFinishTokenAlwaysStreamsVisibleAnswer(t *testing.T) {
	var got string
	emitCodingAgentFinishToken(func(s string) { got = s }, "Created foo.", []v2.TaskRunResult{{Status: v2.TaskPassed, Summary: "Created foo."}}, false)
	if !strings.Contains(got, "Created foo.") {
		t.Fatalf("visible finish must stream even when Summary already matches: %q", got)
	}
	emitCodingAgentFinishToken(func(s string) { got = s }, "Created foo.\n\n`cl` failed.", []v2.TaskRunResult{{Status: v2.TaskFailed, Summary: "Created foo.", Error: "cl failed"}}, false)
	if !strings.Contains(got, "Created foo.") {
		t.Fatalf("failed finish should stream: %q", got)
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
