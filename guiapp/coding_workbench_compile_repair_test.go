package guiapp

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestVerifyLogRecognizesCompilerDiagnostics(t *testing.T) {
	syntax := "" +
		"corelib\\agent\\commit_gate.go:1501:6: syntax error: unexpected name joinTypedPowerShellTarget, expected (\n" +
		"corelib\\agent\\commit_gate.go:1541:6: syntax error: unexpected name joinPowerShellAssignmentList, expected (\n" +
		"... (已截断)\n"
	if !verifyLogHasCompilerDiagnostic(syntax) {
		t.Fatal("syntax error in another file was not a compiler diagnostic")
	}
	files := compilerDiagnosticFiles("", syntax)
	if len(files) != 1 || files[0] != "corelib/agent/commit_gate.go" {
		t.Fatalf("diagnostic files = %#v", files)
	}
	vet := "corelib/opus/entcode/encoder.go:65:20: self-assignment of Syy to Syy\n"
	if verifyLogHasCompilerDiagnostic(vet) {
		t.Fatal("a file-local vet nit was treated as a compiler diagnostic")
	}
	if len(compilerDiagnosticFiles("", vet)) != 0 {
		t.Fatal("vet nit produced a repair file")
	}
	if verifyLogHasCompilerDiagnostic("go: cannot find main module\n") {
		t.Fatal("a toolchain line was treated as a compiler diagnostic")
	}
	prefixed := "vet.exe: corelib/agent/commit_gate.go:2:2: syntax error: unexpected newline\n"
	if !verifyLogHasCompilerDiagnostic(prefixed) {
		t.Fatal("a vet-prefixed syntax error was ignored")
	}
}

func TestCompileRepairTaskSkipsLocalization(t *testing.T) {
	output := "corelib/agent/commit_gate.go:1449:2: syntax error: unexpected newline, expected )\n"
	text := compileRepairTaskText("go build ./guiapp", output, []string{"corelib/agent/commit_gate.go"})
	if !strings.HasPrefix(text, hostCompileRepairMarker) {
		t.Fatal("repair task lost the host marker")
	}
	if !strings.Contains(text, "corelib/agent/commit_gate.go") || !strings.Contains(text, "syntax error") {
		t.Fatalf("repair task dropped the diagnostic:\n%s", text)
	}
	if !strings.Contains(text, "别人的文件") {
		t.Fatal("repair task does not forbid the convention refusal")
	}
	if codingTaskNeedsLocalization("让验证重新通过编译\n" + text) {
		t.Fatal("host compile repair entered bug localization")
	}
	if !codingTaskNeedsLocalization("修复登录失败") {
		t.Fatal("ordinary bug text no longer needs localization")
	}
	if n := utf8.RuneCountInString(text); n > codingSubAgentTaskDescriptionMaxRunes {
		t.Fatalf("repair task is %d runes, over the description budget", n)
	}
	long := compileRepairTaskText("go build ./guiapp", strings.Repeat("corelib/agent/commit_gate.go:1:1: syntax error: unexpected newline\n", 80), nil)
	if !strings.Contains(long, hostCompileRepairMarker) || !strings.Contains(long, "syntax error") {
		t.Fatal("a long diagnostic lost the marker or the compiler line")
	}
	if n := utf8.RuneCountInString(long); n > codingSubAgentTaskDescriptionMaxRunes {
		t.Fatalf("long repair task is %d runes", n)
	}
}

func TestStepVerifyCompileRepairPassesAfterEdit(t *testing.T) {
	calls := 0
	reverifies := 0
	out := runStepVerifyCompileRepair(context.Background(), "", "go build ./guiapp",
		"corelib/agent/commit_gate.go:1449:2: syntax error: unexpected newline, expected )\n",
		func(ctx context.Context, round int, taskText string, files []string) stepCompileRepairTurn {
			calls++
			if round != 1 || !strings.Contains(taskText, hostCompileRepairMarker) {
				t.Fatalf("round %d text missing marker", round)
			}
			if len(files) != 1 || files[0] != "corelib/agent/commit_gate.go" {
				t.Fatalf("files = %#v", files)
			}
			return stepCompileRepairTurn{Edited: true, Modified: []string{"corelib/agent/commit_gate.go"}}
		},
		func(ctx context.Context) (bool, string, string, bool) {
			reverifies++
			return true, "go build ./guiapp", "ok", false
		})
	if calls != 1 || reverifies != 1 || !out.OK || out.Note != stepCompileRepairFixedNote {
		t.Fatalf("outcome calls=%d reverify=%d %+v", calls, reverifies, out)
	}
	if len(out.Modified) != 1 || out.Modified[0] != "corelib/agent/commit_gate.go" {
		t.Fatalf("modified = %#v", out.Modified)
	}
}

func TestStepVerifyCompileRepairStopsWhenTheTurnDoesNotEdit(t *testing.T) {
	reverifies := 0
	out := runStepVerifyCompileRepair(context.Background(), "", "go build ./guiapp",
		"corelib/agent/commit_gate.go:1449:2: syntax error: unexpected newline, expected )\n",
		func(ctx context.Context, round int, taskText string, files []string) stepCompileRepairTurn {
			if round == 2 {
				t.Fatal("a turn that did not edit started another round")
			}
			return stepCompileRepairTurn{}
		},
		func(ctx context.Context) (bool, string, string, bool) {
			reverifies++
			return true, "go build ./guiapp", "ok", false
		})
	if reverifies != 0 || out.OK || out.Note != stepCompileRepairStillFailNote || !out.Attempted {
		t.Fatalf("reverify=%d outcome=%+v", reverifies, out)
	}
}

func TestStepVerifyCompileRepairStopsAfterADifferentFailure(t *testing.T) {
	calls := 0
	out := runStepVerifyCompileRepair(context.Background(), "", "go build ./guiapp",
		"corelib/agent/commit_gate.go:1449:2: syntax error: unexpected newline, expected )\n",
		func(ctx context.Context, round int, taskText string, files []string) stepCompileRepairTurn {
			calls++
			return stepCompileRepairTurn{Edited: true, Modified: []string{"corelib/agent/commit_gate.go"}}
		},
		func(ctx context.Context) (bool, string, string, bool) {
			return false, "go test ./guiapp", "--- FAIL: TestFoo\n", false
		})
	if calls != 1 || out.OK || out.Note != stepCompileRepairStillFailNote {
		t.Fatalf("calls=%d outcome=%+v", calls, out)
	}
	if !strings.Contains(out.Output, "FAIL: TestFoo") {
		t.Fatalf("kept output = %q", out.Output)
	}
}

func TestStepVerifyCompileRepairSkipsWhenTheCompilerNamesNoProjectFile(t *testing.T) {
	output := "../outside.go:1:1: syntax error: unexpected newline, expected )\n"
	if !verifyLogHasCompilerDiagnostic(output) {
		t.Fatal("an out-of-project syntax error was not a compiler diagnostic")
	}
	if files := compilerDiagnosticFiles(`D:\proj`, output); len(files) != 0 {
		t.Fatalf("out-of-project diagnostic files = %#v", files)
	}
	called := false
	out := runStepVerifyCompileRepair(context.Background(), `D:\proj`, "go build ./guiapp", output,
		func(ctx context.Context, round int, taskText string, files []string) stepCompileRepairTurn {
			called = true
			return stepCompileRepairTurn{Edited: true}
		},
		func(ctx context.Context) (bool, string, string, bool) {
			t.Fatal("a repair with no in-project file reverified")
			return false, "", "", false
		})
	if called || out.Attempted || out.OK || out.Note != "" || out.Cancelled {
		t.Fatalf("called=%v outcome=%+v", called, out)
	}
}

func TestStepVerifyCompileRepairCancelAfterEditKeepsFiles(t *testing.T) {
	out := runStepVerifyCompileRepair(context.Background(), "", "go build ./guiapp",
		"corelib/agent/commit_gate.go:1449:2: syntax error: unexpected newline, expected )\n",
		func(ctx context.Context, round int, taskText string, files []string) stepCompileRepairTurn {
			return stepCompileRepairTurn{Edited: true, Cancelled: true, Modified: []string{"corelib/agent/commit_gate.go"}}
		},
		func(ctx context.Context) (bool, string, string, bool) {
			t.Fatal("a cancelled repair reverified")
			return true, "go build ./guiapp", "ok", false
		})
	if !out.Cancelled || !out.Attempted || out.OK || len(out.Modified) != 1 || out.Modified[0] != "corelib/agent/commit_gate.go" {
		t.Fatalf("outcome=%+v", out)
	}
}

func TestCodingResultCancelledMatchesLoopStop(t *testing.T) {
	if !codingResultCancelled(&CodingSubAgentResult{Status: TaskExecFailed, Error: "cancelled"}) {
		t.Fatal("loop stop cancelled was not a repair cancel")
	}
	if !codingResultCancelled(&CodingSubAgentResult{Status: TaskExecFailed, Error: "cancelled during LLM retry"}) {
		t.Fatal("loop stop during retry was not a repair cancel")
	}
	if !codingResultCancelled(&CodingSubAgentResult{Status: TaskExecInterrupted, Error: "task_cancelled"}) {
		t.Fatal("ledger task_cancelled was not a repair cancel")
	}
	if codingResultCancelled(&CodingSubAgentResult{Status: TaskExecFailed, Error: "post-loop verification failed after 2 rounds"}) {
		t.Fatal("a verify failure was treated as a user cancel")
	}
	if codingResultCancelled(&CodingSubAgentResult{Status: TaskExecPassed, Error: "cancelled"}) {
		t.Fatal("a passed result was treated as a user cancel")
	}
}

func TestHostCompileRepairChecklistLeavesVerificationToTheGate(t *testing.T) {
	var repair strings.Builder
	appendCodingSubAgentPreflightChecklist(&repair, true, true)
	text := repair.String()
	if !strings.Contains(text, "Do not run the verification command") || !strings.Contains(text, "Do not submit report_localization") {
		t.Fatalf("repair checklist still asks the model to verify or localize:\n%s", text)
	}
	if strings.Contains(text, "After the last edit, run matching verification") {
		t.Fatal("repair checklist still requires a self-verify")
	}
	var normal strings.Builder
	appendCodingSubAgentPreflightChecklist(&normal, true, false)
	if !strings.Contains(normal.String(), "After the last edit, run matching verification") || !strings.Contains(normal.String(), "report_localization") {
		t.Fatal("ordinary checklist dropped verification or localization")
	}
}

func TestRemotePromptLeavesHostCompileRepairVerification(t *testing.T) {
	prompt := buildRemoteCodingSystemPrompt("/proj", "/proj", "")
	if !strings.Contains(prompt, hostCompileRepairMarker) || !strings.Contains(prompt, "宿主会重跑") {
		t.Fatal("remote prompt still tells a host compile repair to verify itself")
	}
	if !strings.Contains(prompt, "其他步骤必须在本步骤验证") {
		t.Fatal("remote prompt dropped in-turn verification for ordinary tasks")
	}
}

func TestStepVerifyCompileRepairHonoursCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	out := runStepVerifyCompileRepair(ctx, "", "go build ./guiapp",
		"corelib/agent/commit_gate.go:1449:2: syntax error: unexpected newline, expected )\n",
		func(ctx context.Context, round int, taskText string, files []string) stepCompileRepairTurn {
			called = true
			return stepCompileRepairTurn{Edited: true}
		},
		func(ctx context.Context) (bool, string, string, bool) {
			t.Fatal("cancelled repair reverified")
			return false, "", "", false
		})
	if called || !out.Cancelled {
		t.Fatalf("called=%v outcome=%+v", called, out)
	}
}

func TestCompileRepairCancelStaysOpen(t *testing.T) {
	status := codingRunnerSkipStickyStatus("cancelled")
	if status != codingStepInterrupted {
		t.Fatalf("status=%s", status)
	}
	if codingChecklistStepClosed(status) {
		t.Fatal("a cancelled compile repair is a closed step, so resume would skip it")
	}
}

func TestStepVerifyCompileRepairCancelDuringSuccessfulReverify(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	out := runStepVerifyCompileRepair(ctx, "", "go build ./guiapp",
		"corelib/agent/commit_gate.go:1449:2: syntax error: unexpected newline, expected )\n",
		func(ctx context.Context, round int, taskText string, files []string) stepCompileRepairTurn {
			return stepCompileRepairTurn{Edited: true, Modified: []string{"corelib/agent/commit_gate.go"}}
		},
		func(ctx context.Context) (bool, string, string, bool) {
			cancel()
			return true, "go build ./guiapp", "ok", false
		})
	if !out.Cancelled || out.OK || len(out.Modified) != 1 || out.Modified[0] != "corelib/agent/commit_gate.go" {
		t.Fatalf("a passing reverify after cancel was kept: %+v", out)
	}
}

func compileRepairRemoteEdit(h *IMMessageHandler, cfg corelib.MaclawLLMConfig, client *http.Client, remote remoteCodingTemplateContext, loop *LoopContext, task string, onProgress, onToken func(string)) *RemoteCodingSubAgentResult {
	return &RemoteCodingSubAgentResult{Status: "success", FilesModified: []string{"corelib/agent/commit_gate.go"}}
}

func TestLocalStepCompileRepairCancelDuringReverify(t *testing.T) {
	loop := NewLoopContext("compile-repair-local-cancel", 1, nil)
	t.Cleanup(loop.Cancel)
	original := runTaskWithSubAgent
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		return &CodingSubAgentResult{Status: TaskExecPassed, FilesModified: []string{"corelib/agent/commit_gate.go"}}
	}
	t.Cleanup(func() { runTaskWithSubAgent = original })
	prev := runLocalStepVerifyForRepair
	runLocalStepVerifyForRepair = func(ctx context.Context, projectPath string, commandFiles, judgeFiles []string) (bool, string, string, bool) {
		loop.Cancel()
		return true, "go build ./guiapp", "ok", false
	}
	t.Cleanup(func() { runLocalStepVerifyForRepair = prev })
	// The workflow context stays live. The build returned before that
	// context observed the stop, which is the race the policy misses.
	outcome, _ := runLocalStepCompileRepair(context.Background(), loop, corelib.MaclawLLMConfig{}, nil, nil, "", nil, 1,
		"go build ./guiapp",
		"corelib/agent/commit_gate.go:1449:2: syntax error: unexpected newline, expected )\n",
		nil, nil)
	if !outcome.Cancelled || outcome.OK || len(outcome.Modified) != 1 {
		t.Fatalf("outcome=%+v", outcome)
	}
	if outcome.Note == stepCompileRepairFixedNote {
		t.Fatal("a cancelled local reverify was reported as a fixed compile")
	}
}

func TestLocalStepCompileRepairKeepsAPassingReverify(t *testing.T) {
	loop := NewLoopContext("compile-repair-local-pass", 1, nil)
	t.Cleanup(loop.Cancel)
	original := runTaskWithSubAgent
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		return &CodingSubAgentResult{Status: TaskExecPassed, FilesModified: []string{"corelib/agent/commit_gate.go"}}
	}
	t.Cleanup(func() { runTaskWithSubAgent = original })
	prev := runLocalStepVerifyForRepair
	runLocalStepVerifyForRepair = func(ctx context.Context, projectPath string, commandFiles, judgeFiles []string) (bool, string, string, bool) {
		return true, "go build ./guiapp", "ok", false
	}
	t.Cleanup(func() { runLocalStepVerifyForRepair = prev })
	outcome, _ := runLocalStepCompileRepair(context.Background(), loop, corelib.MaclawLLMConfig{}, nil, nil, "", nil, 1,
		"go build ./guiapp",
		"corelib/agent/commit_gate.go:1449:2: syntax error: unexpected newline, expected )\n",
		nil, nil)
	if !outcome.OK || outcome.Cancelled || outcome.Note != stepCompileRepairFixedNote || len(outcome.Modified) != 1 {
		t.Fatalf("outcome=%+v", outcome)
	}
}

func TestRemoteStepCompileRepairCancelDuringReverify(t *testing.T) {
	loop := NewLoopContext("compile-repair-cancel", 1, nil)
	t.Cleanup(loop.Cancel)
	verified := false
	prev := runRemoteStepVerifyForRepair
	runRemoteStepVerifyForRepair = func(h *IMMessageHandler, sessionID, projectDir string, commandFiles, judgeFiles []string) (bool, string, string, bool) {
		verified = true
		loop.Cancel()
		return true, "go build ./guiapp", "ok", false
	}
	t.Cleanup(func() { runRemoteStepVerifyForRepair = prev })
	outcome, _ := runRemoteStepCompileRepair(nil, loop, corelib.MaclawLLMConfig{}, nil, nil, remoteCodingTemplateContext{},
		compileRepairRemoteEdit, "", nil, 1, "go build ./guiapp",
		"corelib/agent/commit_gate.go:1449:2: syntax error: unexpected newline, expected )\n", nil, nil)
	if !verified || !outcome.Cancelled || outcome.OK || len(outcome.Modified) != 1 {
		t.Fatalf("verified=%v outcome=%+v", verified, outcome)
	}
	if outcome.Note == stepCompileRepairFixedNote {
		t.Fatal("a cancelled remote reverify was reported as a fixed compile")
	}
}

func TestLocalStepCompileRepairKeepsOriginalAccountability(t *testing.T) {
	loop := NewLoopContext("compile-repair-local-scope", 1, nil)
	t.Cleanup(loop.Cancel)
	originalRun := runTaskWithSubAgent
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		return &CodingSubAgentResult{Status: TaskExecPassed, FilesModified: []string{"corelib/agent/commit_gate.go"}}
	}
	t.Cleanup(func() { runTaskWithSubAgent = originalRun })
	var gotCommand, gotJudge []string
	prev := runLocalStepVerifyForRepair
	runLocalStepVerifyForRepair = func(ctx context.Context, projectPath string, commandFiles, judgeFiles []string) (bool, string, string, bool) {
		gotCommand = append([]string{}, commandFiles...)
		gotJudge = append([]string{}, judgeFiles...)
		return true, "go vet ./MaClawSrv/cloudagent ./corelib/agent", "ok", false
	}
	t.Cleanup(func() { runLocalStepVerifyForRepair = prev })
	outcome, _ := runLocalStepCompileRepair(context.Background(), loop, corelib.MaclawLLMConfig{}, nil, nil, "", []string{"MaClawSrv/cloudagent/jail.go"}, 6,
		"echo __MACLAW_BUILT__ && go vet ./MaClawSrv/cloudagent && echo __MACLAW_VETTED__",
		"corelib\\agent\\commit_gate.go:1501:6: syntax error: unexpected name joinTypedPowerShellTarget, expected (\n",
		nil, nil)
	if !outcome.OK || !sameStrings(gotCommand, []string{"MaClawSrv/cloudagent/jail.go", "corelib/agent/commit_gate.go"}) || !sameStrings(gotJudge, []string{"MaClawSrv/cloudagent/jail.go"}) {
		t.Fatalf("command=%v judge=%v outcome=%+v", gotCommand, gotJudge, outcome)
	}
}

func TestRemoteStepCompileRepairKeepsOriginalAccountability(t *testing.T) {
	loop := NewLoopContext("compile-repair-remote-scope", 1, nil)
	t.Cleanup(loop.Cancel)
	var gotCommand, gotJudge []string
	prev := runRemoteStepVerifyForRepair
	runRemoteStepVerifyForRepair = func(h *IMMessageHandler, sessionID, projectDir string, commandFiles, judgeFiles []string) (bool, string, string, bool) {
		gotCommand = append([]string{}, commandFiles...)
		gotJudge = append([]string{}, judgeFiles...)
		return true, "go vet ./MaClawSrv/cloudagent ./corelib/agent", "ok", false
	}
	t.Cleanup(func() { runRemoteStepVerifyForRepair = prev })
	outcome, _ := runRemoteStepCompileRepair(nil, loop, corelib.MaclawLLMConfig{}, nil, nil, remoteCodingTemplateContext{},
		compileRepairRemoteEdit, "", []string{"MaClawSrv/cloudagent/jail.go"}, 6,
		"echo __MACLAW_BUILT__ && go vet ./MaClawSrv/cloudagent && echo __MACLAW_VETTED__",
		"corelib\\agent\\commit_gate.go:1501:6: syntax error: unexpected name joinTypedPowerShellTarget, expected (\n", nil, nil)
	if !outcome.OK || !sameStrings(gotCommand, []string{"MaClawSrv/cloudagent/jail.go", "corelib/agent/commit_gate.go"}) || !sameStrings(gotJudge, []string{"MaClawSrv/cloudagent/jail.go"}) {
		t.Fatalf("command=%v judge=%v outcome=%+v", gotCommand, gotJudge, outcome)
	}
}

func TestCompileRepairAcceptanceUsesOriginalTaskFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	original := []string{"MaClawSrv/cloudagent/jail.go"}
	repaired := []string{"MaClawSrv/cloudagent/jail.go", "corelib/agent/commit_gate.go"}
	narrowed, skip, _ := scopeGoVerifyToTaskAt(goVerifyCommand(), root, original, "")
	widened, skipWide, _ := repairGoVerifyCommand(goVerifyCommand(), root, repaired, original, "")
	if skip || skipWide || !strings.Contains(narrowed, "./MaClawSrv/cloudagent") || strings.Contains(narrowed, "corelib/agent") {
		t.Fatalf("narrowed=%q skip=%v", narrowed, skip)
	}
	if !strings.Contains(widened, "go build ./corelib/agent && ") || !strings.Contains(widened, "go vet ./MaClawSrv/cloudagent") || strings.Contains(widened, "vet ./corelib/agent") {
		t.Fatalf("widened=%q", widened)
	}
	nit := verifyBuildOKMarker + "\ncorelib/agent/commit_gate.go:10:2: loop variable x captured by func literal\n"
	syntax := verifyBuildOKMarker + "\ncorelib\\agent\\commit_gate.go:1501:6: syntax error: unexpected name joinTypedPowerShellTarget, expected (\n"
	if historicalGoVerifyOnly(nit, root, repaired) {
		t.Fatal("an analyzer finding was ignored after the repaired file joined the task")
	}
	if !historicalGoVerifyOnly(nit, root, original) {
		t.Fatal("an analyzer finding in the repaired dependency was charged to the original step")
	}
	if historicalGoVerifyOnly(syntax, root, original) {
		t.Fatal("a compiler error in the repaired dependency was ignored")
	}
	ok, _, note, skipped := acceptTaskVerifyResult(false, widened, nit, root, original, true)
	if !ok || skipped || !strings.Contains(note, "historical") {
		t.Fatalf("historical analyzer finding failed acceptance ok=%v skipped=%v note=%q", ok, skipped, note)
	}
	ok, _, note, skipped = acceptTaskVerifyResult(false, widened, syntax, root, original, true)
	if ok || skipped || strings.Contains(note, "historical") {
		t.Fatalf("compiler error was accepted ok=%v skipped=%v note=%q", ok, skipped, note)
	}
	// go vet of the repaired package would compile its tests and prefix a
	// failure with vet.exe. That line is not historical, and the original
	// importer command never compiled those tests.
	testFail := verifyBuildOKMarker + "\nvet.exe: corelib/agent/commit_gate_test.go:2:2: not enough return values\n"
	if historicalGoVerifyOnly(testFail, root, original) {
		t.Fatal("a dependency test compile error was ignored")
	}
}

func TestRepairVerifyTypechecksEditedPackagesWithoutVet(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmdDir := filepath.Join(root, "cmd", "tool")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cmdDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	original := []string{"MaClawSrv/cloudagent/jail.go"}
	narrowed, skip, _ := scopeGoVerifyToTaskAt(goVerifyCommand(), root, original, "")
	if skip {
		t.Fatal(skip)
	}
	same, skip, _ := repairGoVerifyCommand(goVerifyCommand(), root, []string{
		"MaClawSrv/cloudagent/jail.go",
		"MaClawSrv/cloudagent/jail_windows.go",
	}, original, "")
	if skip || same != narrowed {
		t.Fatalf("same package changed the command:\n%s\n%s", same, narrowed)
	}
	cargo, skip, _ := repairGoVerifyCommand("cargo check", root, []string{
		"MaClawSrv/cloudagent/jail.go",
		"corelib/agent/commit_gate.go",
	}, original, "")
	if skip || cargo != "cargo check" {
		t.Fatalf("cargo=%q skip=%v", cargo, skip)
	}
	custom := "go build ./guiapp && echo " + verifyBuildOKMarker + " && go vet ./guiapp && echo " + verifyVetOKMarker
	got, skip, _ := repairGoVerifyCommand(custom, root, []string{
		"MaClawSrv/cloudagent/jail.go",
		"corelib/agent/commit_gate_test.go",
	}, original, "")
	if skip || got != "go build ./corelib/agent && "+custom {
		t.Fatalf("custom=%q skip=%v", got, skip)
	}
	if strings.Contains(got, "go vet ./corelib/agent") || strings.Contains(got, "go test") {
		t.Fatalf("dependency tests were vetted: %q", got)
	}
	out := filepath.ToSlash(filepath.Join(t.TempDir(), "bins"))
	sink := quoteGoArg(normalizeVerifyOutDir(out) + "repair.out")
	libCmd, skip, _ := repairGoVerifyCommand(goVerifyCommand(), root, []string{
		"MaClawSrv/cloudagent/jail.go",
		"corelib/agent/commit_gate.go",
	}, original, out)
	wantLib := "go build -o " + sink + " ./corelib/agent && " + narrowed
	if skip || libCmd != wantLib {
		t.Fatalf("library=%q\nwant %q", libCmd, wantLib)
	}
	mainCmd, skip, _ := repairGoVerifyCommand(goVerifyCommand(), root, []string{
		"MaClawSrv/cloudagent/jail.go",
		"cmd/tool/main.go",
	}, original, out)
	wantBuild := "go build -o " + sink + " ./cmd/tool && " + narrowed
	if skip || mainCmd != wantBuild {
		t.Fatalf("main=%q\nwant %q", mainCmd, wantBuild)
	}
	if strings.Contains(mainCmd, "go vet ./cmd/tool") || strings.Contains(mainCmd, normalizeVerifyOutDir(out)+" ./") {
		t.Fatalf("opened package was vetted or built into a directory: %q", mainCmd)
	}
	// The remote project is not on this machine, so package main cannot be
	// read. The check still has to discard the executable.
	missing := filepath.Join(t.TempDir(), "remote-only")
	remoteOut := "/tmp/maclaw-verify"
	judge := []string{"MaClawSrv/cloudagent/jail.go"}
	got, skip, _ = repairGoVerifyCommand(goVerifyCommand(), missing, []string{
		"MaClawSrv/cloudagent/jail.go",
		"cmd/tool/main.go",
	}, judge, remoteOut)
	base, baseSkip, _ := scopeGoVerifyToTaskAt(goVerifyCommand(), missing, judge, remoteOut)
	remoteSink := quoteGoArg(normalizeVerifyOutDir(remoteOut) + "repair.out")
	wantRemote := "go build -o " + remoteSink + " ./cmd/tool && " + base
	if skip || baseSkip || got != wantRemote {
		t.Fatalf("missing tree cmd=%q\nwant %q", got, wantRemote)
	}
}

func TestRemoteStepCompileRepairKeepsAPassingReverify(t *testing.T) {
	loop := NewLoopContext("compile-repair-pass", 1, nil)
	t.Cleanup(loop.Cancel)
	prev := runRemoteStepVerifyForRepair
	runRemoteStepVerifyForRepair = func(h *IMMessageHandler, sessionID, projectDir string, commandFiles, judgeFiles []string) (bool, string, string, bool) {
		return true, "go build ./guiapp", "ok", false
	}
	t.Cleanup(func() { runRemoteStepVerifyForRepair = prev })
	outcome, _ := runRemoteStepCompileRepair(nil, loop, corelib.MaclawLLMConfig{}, nil, nil, remoteCodingTemplateContext{},
		compileRepairRemoteEdit, "", nil, 1, "go build ./guiapp",
		"corelib/agent/commit_gate.go:1449:2: syntax error: unexpected newline, expected )\n", nil, nil)
	if !outcome.OK || outcome.Cancelled || outcome.Note != stepCompileRepairFixedNote || len(outcome.Modified) != 1 {
		t.Fatalf("outcome=%+v", outcome)
	}
}
