package main

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
