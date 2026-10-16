package guiapp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestAgentsInstructionsCacheHit(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# rules\nuse gofmt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{}
	userID := "desktop-user:agents-cache"
	// Prime sticky + cache.
	c1 := h.ensureStickyProjectInstructions(userID, dir)
	if !strings.Contains(c1, "gofmt") {
		t.Fatalf("c1=%q", c1)
	}
	// Second call should hit mtime cache (still correct content).
	c2 := h.ensureStickyProjectInstructions(userID, dir)
	if c2 != c1 {
		t.Fatalf("cache mismatch %q vs %q", c1, c2)
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestLoadCodingWorkbenchProjectInstructions(t *testing.T) {
	dir := t.TempDir()
	if content, src := loadCodingWorkbenchProjectInstructions(dir); content != "" || len(src) != 0 {
		t.Fatalf("empty project should have no instructions")
	}
	agents := "# Project rules\n\n- Use tabs\n- Run tests\n"
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(agents), 0o644); err != nil {
		t.Fatal(err)
	}
	content, sources := loadCodingWorkbenchProjectInstructions(dir)
	if !strings.Contains(content, "Use tabs") {
		t.Fatalf("content=%q", content)
	}
	if len(sources) == 0 || sources[0] != "AGENTS.md" {
		t.Fatalf("sources=%v", sources)
	}
	// CLAUDE.md should also be picked when no AGENTS (priority group).
	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir2, "CLAUDE.md"), []byte("Claude rules here"), 0o644); err != nil {
		t.Fatal(err)
	}
	content2, sources2 := loadCodingWorkbenchProjectInstructions(dir2)
	if !strings.Contains(content2, "Claude rules") {
		t.Fatalf("content2=%q", content2)
	}
	if len(sources2) == 0 {
		t.Fatal("expected CLAUDE.md source")
	}
}

func TestNormalizeCodingPlanMode(t *testing.T) {
	if normalizeCodingPlanMode("") != codingPlanModeAuto {
		t.Fatal("default auto")
	}
	if normalizeCodingPlanMode("approve") != codingPlanModeApprove {
		t.Fatal("approve")
	}
	if normalizeCodingPlanMode("off") != codingPlanModeOff {
		t.Fatal("off")
	}
	if normalizeCodingPlanMode("confirm") != codingPlanModeApprove {
		t.Fatal("confirm alias")
	}
}

func TestStickyPendingPlanRoundTrip(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:test-plan-approve"
	tasks := []*v2.TaskItem{
		{Index: 1, Title: "explore", Description: "map code"},
		{Index: 2, Title: "implement", Description: "write code", DependsOn: []int{1}},
	}
	h.storeStickyPendingCodingPlan(userID, "build auth", "### T1: explore\n### T2: implement", tasks)
	pending, ok := h.loadStickyPendingCodingPlan(userID)
	if !ok || len(pending.Tasks) != 2 {
		t.Fatalf("pending=%+v ok=%v", pending, ok)
	}
	if pending.UserText != "build auth" {
		t.Fatalf("user text=%q", pending.UserText)
	}
	promoted, ok := h.promotePendingToApprovedCodingPlan(userID)
	if !ok || len(promoted.Tasks) != 2 {
		t.Fatalf("promoted=%+v", promoted)
	}
	if _, still := h.loadStickyPendingCodingPlan(userID); still {
		t.Fatal("pending should be cleared after promote")
	}
	taken, ok := h.takeStickyApprovedCodingPlan(userID)
	if !ok || len(taken.Tasks) != 2 {
		t.Fatalf("taken=%+v", taken)
	}
	if _, again := h.takeStickyApprovedCodingPlan(userID); again {
		t.Fatal("approved plan is one-shot")
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestRestoreApprovedCodingPlanAsPending(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:restore-approved-plan"
	tasks := []*v2.TaskItem{
		{Index: 1, Title: "explore", Description: "map code"},
		{Index: 2, Title: "implement", Description: "write code", DependsOn: []int{1}},
	}
	h.storeStickyPendingCodingPlan(userID, "build auth", "### T1: explore\n### T2: implement", tasks)
	if _, ok := h.promotePendingToApprovedCodingPlan(userID); !ok {
		t.Fatal("expected pending plan promotion")
	}
	restored, ok := h.restoreApprovedCodingPlanAsPending(userID)
	if !ok || len(restored.Tasks) != len(tasks) {
		t.Fatalf("restored=%+v ok=%v", restored, ok)
	}
	if pending, ok := h.loadStickyPendingCodingPlan(userID); !ok || pending.UserText != "build auth" || len(pending.Tasks) != len(tasks) {
		t.Fatalf("pending=%+v ok=%v", pending, ok)
	}
	if again, ok := h.restoreApprovedCodingPlanAsPending(userID); ok || len(again.Tasks) != 0 {
		t.Fatalf("restore must be one-shot, got %+v ok=%v", again, ok)
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if strings.TrimSpace(mem.ApprovedPlanJSON) != "" {
		t.Fatalf("approved plan must be cleared after restore: %+v", mem)
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestWindowsCodingBashFindsPowerShellWithoutPATH(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows shell resolution")
	}
	shell, args := windowsCodingBashInvocation("cmake --build build")
	if !filepath.IsAbs(shell) {
		t.Fatalf("shell %q must be absolute so a stripped PATH still starts powershell", shell)
	}
	base := strings.ToLower(filepath.Base(shell))
	if base != "powershell.exe" && base != "pwsh.exe" {
		t.Fatalf("shell %q", shell)
	}
	if len(args) == 0 || !strings.Contains(args[len(args)-1], "cmake --build build") {
		t.Fatalf("args %q", args)
	}
	if got := passthroughRuntimeProgram("powershell"); !filepath.IsAbs(got) {
		t.Fatalf("passthrough powershell %q", got)
	}
}

func TestWindowsPowerShellWrapperStopsAfterNativeFailure(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows shell")
	}
	shell, args := windowsCodingBashInvocation("cmd /c exit 3 && echo RAN_SECOND")
	cmd := exec.Command(shell, args...)
	hideCommandWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit, output: %s", out)
	}
	if strings.Contains(string(out), "RAN_SECOND") {
		t.Fatalf("command after && ran despite failure: %s", out)
	}
	okShell, okArgs := windowsCodingBashInvocation("cmd /c exit 0")
	okCmd := exec.Command(okShell, okArgs...)
	hideCommandWindow(okCmd)
	okOut, okErr := okCmd.CombinedOutput()
	if okErr != nil {
		t.Fatalf("successful command exited %v: %s", okErr, okOut)
	}
}

func TestGoVerifyCommandShortCircuits(t *testing.T) {
	got := goVerifyCommand()
	if strings.Contains(got, ";") || !strings.Contains(got, "&&") {
		t.Fatalf("verify command %q must stop after a failed build", got)
	}
	ps := convertUnquotedAndAndForPowerShell(got)
	if !strings.Contains(ps, "$LASTEXITCODE") || strings.Contains(ps, "&&") {
		t.Fatalf("powershell translation = %q", ps)
	}
}

func TestSelectCMakeVerifyCommandProbesThenFallsBack(t *testing.T) {
	root := t.TempDir()
	build := filepath.Join(root, "build")
	if err := os.MkdirAll(build, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := selectCMakeVerifyCommand(root, hostBuildTools{}); got != "" {
		t.Fatalf("unresolved cmake must not be emitted, got %q", got)
	}

	cmakeDir := filepath.Join(root, "Program Files", "CMake", "bin")
	if err := os.MkdirAll(cmakeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmake := filepath.Join(cmakeDir, "cmake.exe")
	if err := os.WriteFile(cmake, []byte("cmake"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := selectCMakeVerifyCommand(root, hostBuildTools{cmake: cmake}); got != quoteVerifyExecutable(cmake)+" --build build" {
		t.Fatalf("probed cmake = %q", got)
	}

	cache := "CMAKE_COMMAND:INTERNAL=" + filepath.ToSlash(cmake) + "\n"
	if err := os.WriteFile(filepath.Join(build, "CMakeCache.txt"), []byte(cache), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := selectCMakeVerifyCommand(root, hostBuildTools{}); got != quoteVerifyExecutable(cmake)+" --build build" {
		t.Fatalf("cache cmake = %q", got)
	}
	quoted := quoteVerifyExecutable(cmake) + " --build build"
	if strings.HasPrefix(strings.TrimSpace(quoted), "&") {
		t.Fatalf("verify command must not hide cmake behind a call operator: %q", quoted)
	}
	if fields := commandSegmentFields(shellCommandFields(quoted)); len(fields) == 0 || commandNameBase(fields[0]) != "cmake" {
		t.Fatalf("cmake was not visible to the command parser: %q", quoted)
	}
	if got := ensurePowerShellCallOperator(`"hello world"`); strings.HasPrefix(strings.TrimSpace(got), "&") {
		t.Fatalf("quoted text was treated as a program: %q", got)
	}
	if runtime.GOOS == "windows" {
		_, args := windowsCodingBashInvocation(quoted)
		if len(args) == 0 || !strings.Contains(args[len(args)-1], "& \"") {
			t.Fatalf("powershell launch = %#v", args)
		}
	}

	if err := os.Remove(filepath.Join(build, "CMakeCache.txt")); err != nil {
		t.Fatal(err)
	}
	ninja := filepath.Join(root, "ninja.exe")
	if err := os.WriteFile(ninja, []byte("ninja"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(build, "build.ninja"), []byte("build"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := selectCMakeVerifyCommand(root, hostBuildTools{ninja: ninja}); got != quoteVerifyExecutable(ninja)+" -C build" {
		t.Fatalf("ninja fallback = %q", got)
	}
	makeProgram := filepath.Join(root, "Program Files", "ninja.exe")
	if err := os.MkdirAll(filepath.Dir(makeProgram), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(makeProgram, []byte("ninja"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache = "CMAKE_COMMAND:INTERNAL=" + filepath.ToSlash(filepath.Join(root, "missing", "cmake.exe")) + "\nCMAKE_MAKE_PROGRAM:FILEPATH=" + filepath.ToSlash(makeProgram) + "\n"
	if err := os.WriteFile(filepath.Join(build, "CMakeCache.txt"), []byte(cache), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := selectCMakeVerifyCommand(root, hostBuildTools{}); got != quoteVerifyExecutable(makeProgram)+" -C build" {
		t.Fatalf("cache ninja = %q", got)
	}
	if err := os.Remove(filepath.Join(build, "CMakeCache.txt")); err != nil {
		t.Fatal(err)
	}
	nmake := filepath.Join(root, "nmake.exe")
	if err := os.WriteFile(nmake, []byte("nmake"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(build, "CMakeCache.txt"), []byte("CMAKE_MAKE_PROGRAM:FILEPATH="+filepath.ToSlash(nmake)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := selectCMakeVerifyCommand(root, hostBuildTools{}); got != "" {
		t.Fatalf("nmake must not be invoked as make, got %q", got)
	}
	if err := os.Remove(filepath.Join(build, "CMakeCache.txt")); err != nil {
		t.Fatal(err)
	}

	sln := filepath.Join(build, "app.sln")
	if err := os.WriteFile(sln, []byte("sln"), 0o644); err != nil {
		t.Fatal(err)
	}
	msbuild := filepath.Join(root, "MSBuild.exe")
	if err := os.WriteFile(msbuild, []byte("msbuild"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(build, "build.ninja")); err != nil {
		t.Fatal(err)
	}
	if got := selectCMakeVerifyCommand(root, hostBuildTools{msbuild: msbuild}); got != quoteVerifyExecutable(msbuild)+" "+shellQuoteVerifyToken(sln) {
		t.Fatalf("msbuild fallback = %q", got)
	}
}

func TestDetectCMakeVerifyInstallsWhenHostHasNoBuilder(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("private CMake install is Windows-only")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "CMakeLists.txt"), []byte("project(demo)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "cmake-bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	cmake := filepath.Join(bin, "cmake.exe")
	if err := os.WriteFile(cmake, []byte("cmake"), 0o644); err != nil {
		t.Fatal(err)
	}
	prevLookup := lookupHostBuildTools
	prevInstall := installPrivateCMakeFn
	prevPrivate := privateCMakeDir()
	t.Cleanup(func() {
		lookupHostBuildTools = prevLookup
		installPrivateCMakeFn = prevInstall
		setPrivateCMakeDir(prevPrivate)
	})
	lookupHostBuildTools = func() hostBuildTools { return hostBuildTools{} }
	installed := 0
	installPrivateCMakeFn = func(ctx context.Context) string {
		installed++
		return bin
	}
	if got := ensureProjectVerifyCommand(context.Background(), root); got != "" || installed != 0 {
		t.Fatalf("empty build dir command=%q install calls=%d", got, installed)
	}
	if err := os.WriteFile(filepath.Join(root, "build", "CMakeCache.txt"), []byte("CMAKE_COMMAND:INTERNAL="+filepath.ToSlash(filepath.Join(root, "missing", "cmake.exe"))+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if got := ensureProjectVerifyCommand(cancelled, root); got != "" || installed != 0 {
		t.Fatalf("cancelled command=%q calls=%d", got, installed)
	}
	got := ensureProjectVerifyCommand(context.Background(), root)
	if host := detectWindowsCMakeBin(); host != "" {
		if installed != 0 {
			t.Fatalf("cmake at %s was installed again (%d)", host, installed)
		}
		if !strings.Contains(strings.ToLower(got), "cmake") {
			t.Fatalf("existing cmake command = %q", got)
		}
		return
	}
	if installed != 1 {
		t.Fatalf("install calls=%d", installed)
	}
	if got != quoteVerifyExecutable(cmake)+" --build build" {
		t.Fatalf("installed cmake = %q", got)
	}
}

func TestStepVerifySkipReason(t *testing.T) {
	plain := codingWorkbenchStepGateSummary(true, "", "", true)
	if !strings.Contains(plain, "no project verify command") {
		t.Fatal(plain)
	}
	reason := codingWorkbenchStepGateSummary(true, "", "CMake build tree present, no cmake/ninja/msbuild available", true)
	if !strings.Contains(reason, "CMake build tree present") {
		t.Fatal(reason)
	}
}

func TestStepNeedsVerifyGate(t *testing.T) {
	if !stepNeedsVerifyGate("verify", "run tests", 3, 3) {
		t.Fatal("last step should gate")
	}
	if !stepNeedsVerifyGate("implement JWT", "write code", 2, 3) {
		t.Fatal("implement should gate")
	}
	if stepNeedsVerifyGate("explore codebase", "read files", 1, 3) {
		t.Fatal("explore should not gate")
	}
}

func TestIsCodingWorkbenchSlash(t *testing.T) {
	for _, cmd := range []string{"/plan", "/plan approve", "/review", "/test", "/commit msg", "/pr", "/map foo", "/checkpoint", "/agents", "/coding-help"} {
		if !isCodingWorkbenchSlash(cmd) {
			t.Fatalf("expected slash: %s", cmd)
		}
	}
	if isCodingWorkbenchSlash("/goal x") {
		t.Fatal("/goal is not coding workbench slash")
	}
	if classifyImmediateIMCommand("/plan approve") != imCommandCodingWorkbench {
		t.Fatal("classify plan")
	}
}

func TestCodingWorkbenchStepsFromTasks(t *testing.T) {
	steps := codingWorkbenchStepsFromTasks([]*v2.TaskItem{
		{Index: 1, Title: "a"},
		{Index: 2, Title: "b"},
	}, codingStepPending)
	if len(steps) != 2 || steps[0].Status != codingStepPending || steps[1].Title != "b" {
		t.Fatalf("%+v", steps)
	}
}

func TestCheckpointRoundTrip(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:test-checkpoint"
	h.setStickyCodingSessionPlan(userID, "Ship feature X")
	h.setStickyCodingExecutionPlan(userID, "### T1: do it")
	cp := h.saveStickyCodingCheckpoint(userID, "mid")
	if cp.Label != "mid" {
		t.Fatalf("label=%q", cp.Label)
	}
	h.setStickyCodingSessionPlan(userID, "changed")
	restored, ok := h.restoreStickyCodingCheckpoint(userID)
	if !ok || restored.Label != "mid" {
		t.Fatalf("restore=%+v", restored)
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if mem.SessionPlan != "Ship feature X" {
		t.Fatalf("session plan=%q", mem.SessionPlan)
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestResolvePlanModeOff(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:plan-off"
	text := "1. explore auth\n2. implement JWT\n3. add tests"
	tasks, _, planned := h.resolveCodingWorkbenchTasks(userID, text, t.TempDir(), stickyCodingWorkbenchMemory{
		PlanMode: codingPlanModeOff,
	}, nil, nil)
	if planned || len(tasks) != 1 {
		t.Fatalf("plan mode off should single-task: planned=%v len=%d", planned, len(tasks))
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestResolvePlanModeApproveStoresPending(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:plan-approve-mode"
	text := "1. explore auth module structure carefully\n2. implement JWT login endpoint\n3. add unit tests for auth"
	tasks, md, planned := h.resolveCodingWorkbenchTasks(userID, text, t.TempDir(), stickyCodingWorkbenchMemory{
		PlanMode: codingPlanModeApprove,
	}, nil, nil)
	if !planned || len(tasks) < 2 {
		t.Fatalf("planned=%v tasks=%d md=%q", planned, len(tasks), md)
	}
	pending, ok := h.loadStickyPendingCodingPlan(userID)
	if !ok || len(pending.Tasks) < 2 {
		t.Fatalf("expected pending plan, got ok=%v pending=%+v", ok, pending)
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestPrevOutputsIncludesProjectInstructions(t *testing.T) {
	mem := stickyCodingWorkbenchMemory{
		ProjectInstructions:       "Always use gofmt",
		ProjectInstructionSources: []string{"AGENTS.md"},
		SessionPlan:               "goal",
	}
	out := mem.prevOutputs()
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "Always use gofmt") {
		t.Fatalf("prevOutputs missing instructions: %q", joined)
	}
}

func TestMarkRemainingCodingStepsSkippedRewritesPrematurePassed(t *testing.T) {
	userID := "desktop-user:skip-premature"
	h := &IMMessageHandler{}
	h.clearStickyCodingWorkbenchMemory(userID)
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	h.setStickyCodingStepStatuses(userID, []codingWorkbenchStepStatus{
		{Index: 1, Title: "T1", Status: codingStepFailed},
		{Index: 2, Title: "T2", Status: codingStepPassed},  // premature agent mark
		{Index: 3, Title: "T3", Status: codingStepRunning}, // should also skip
		{Index: 4, Title: "T4", Status: codingStepPending},
		{Index: 5, Title: "T5", Status: codingStepSkipped}, // already skipped
	})
	h.markRemainingCodingStepsSkipped(userID, 1, "skipped: prior step failed")
	mem := h.getStickyCodingWorkbenchMemory(userID)
	byIdx := map[int]codingWorkbenchStepStatus{}
	for _, st := range mem.StepStatuses {
		byIdx[st.Index] = st
	}
	if byIdx[1].Status != codingStepFailed {
		t.Fatalf("T1 must stay failed: %+v", byIdx[1])
	}
	for _, idx := range []int{2, 3, 4} {
		if byIdx[idx].Status != codingStepSkipped {
			t.Fatalf("T%d want skipped, got %+v", idx, byIdx[idx])
		}
		if !strings.Contains(byIdx[idx].Summary, "prior step failed") {
			t.Fatalf("T%d summary=%q", idx, byIdx[idx].Summary)
		}
	}
	if byIdx[5].Status != codingStepSkipped {
		t.Fatalf("already-skipped T5 should remain skipped: %+v", byIdx[5])
	}
}
