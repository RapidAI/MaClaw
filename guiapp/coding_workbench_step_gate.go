package guiapp

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
)

// hostCompileRepairMarker keeps a host-driven compile repair out of the
// bug-localization gate. The compiler log contains "error", and that word
// would otherwise demand a localization report before the edit.
const hostCompileRepairMarker = "[host-compile-repair]"

// textIsHostCompileRepair reports a turn whose verification belongs to the
// step gate. The compiler log is already in the prompt, so this turn edits
// and stops. The subagent post-loop and the self-verify quality gate must
// not start another compile.
func textIsHostCompileRepair(text string) bool {
	return strings.Contains(text, hostCompileRepairMarker)
}

func codingTaskIsHostCompileRepair(task *TaskItem) bool {
	if task == nil {
		return false
	}
	return textIsHostCompileRepair(task.Title + "\n" + task.Description)
}

const (
	stepCompileRepairFixedNote     = "已自动修复挡住验证的编译错误"
	stepCompileRepairStillFailNote = "已自动尝试修复编译错误，验证仍未通过。"
)

// stepNeedsVerifyGate returns true when a completed plan step should run an
// external project verification command (implement/test/verify style steps).
func stepNeedsVerifyGate(title, description string, stepIndex, stepTotal int) bool {
	// Always gate the last step of a multi-step plan.
	if stepTotal >= 2 && stepIndex == stepTotal {
		return true
	}
	blob := strings.ToLower(strings.TrimSpace(title + " " + description))
	if blob == "" {
		return false
	}
	keywords := []string{
		"verify", "test", "build", "lint", "typecheck", "regress", "check",
		"验证", "测试", "构建", "编译", "验收", "回归", "检查",
		"implement", "实现", "编码", "修复", "fix",
	}
	for _, kw := range keywords {
		if strings.Contains(blob, kw) {
			return true
		}
	}
	// Mid-plan explore-only steps skip gates.
	if strings.Contains(blob, "explor") || strings.Contains(blob, "探查") ||
		strings.Contains(blob, "定位") || strings.Contains(blob, "map ") ||
		strings.Contains(blob, "read") || strings.Contains(blob, "阅读") {
		return false
	}
	return false
}

// runCodingWorkbenchStepVerify runs the project verify command for a step gate.
// Returns ok=true when command exits 0, or when no verify command is detected
// (skip = not a hard fail). An explicit /test has no task file boundary and
// still verifies the whole project.
func runCodingWorkbenchStepVerify(ctx context.Context, projectPath string) (ok bool, cmd, output string, skipped bool) {
	return runCodingWorkbenchStepVerifyScoped(ctx, projectPath, nil, false)
}

// runCodingWorkbenchStepVerifyForTask verifies only packages this task
// changed. Findings in other packages are historical and do not fail the step.
func runCodingWorkbenchStepVerifyForTask(ctx context.Context, projectPath string, taskFiles []string) (ok bool, cmd, output string, skipped bool) {
	return runCodingWorkbenchStepVerifyScoped(ctx, projectPath, taskFiles, true)
}

func runCodingWorkbenchStepVerifyScoped(ctx context.Context, projectPath string, taskFiles []string, taskScoped bool) (ok bool, cmd, output string, skipped bool) {
	return runCodingWorkbenchStepVerifyScopedJudge(ctx, projectPath, taskFiles, taskFiles, taskScoped)
}

// runCodingWorkbenchStepVerifyForRepair re-derives the project command,
// scopes it to the original task files, and typechecks packages the repair
// opened. Judgment stays on the original files. A compiler diagnostic is
// still this step's failure. Vet analyzers are not run on a dependency the
// repair had to open, so that package's existing test failures and analyzer
// findings do not become a new step failure.
func runCodingWorkbenchStepVerifyForRepair(ctx context.Context, projectPath string, commandFiles, judgeFiles []string) (ok bool, cmd, output string, skipped bool) {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return true, "", "", true
	}
	cmd = ensureProjectVerifyCommand(ctx, projectPath)
	if strings.TrimSpace(cmd) == "" {
		reason := ""
		if ctx == nil || ctx.Err() == nil {
			reason = cmakeVerifySkippedReason(projectPath)
		}
		return true, "", reason, true
	}
	scoped, scopeSkip, scopeReason := repairGoVerifyCommand(cmd, projectPath, commandFiles, judgeFiles, localVerifyBuildOutDir())
	if scopeSkip {
		return true, "", scopeReason, true
	}
	return executeCodingWorkbenchStepVerify(ctx, projectPath, scoped, judgeFiles, true)
}

func runCodingWorkbenchStepVerifyScopedJudge(ctx context.Context, projectPath string, commandFiles, judgeFiles []string, taskScoped bool) (ok bool, cmd, output string, skipped bool) {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return true, "", "", true
	}
	cmd = ensureProjectVerifyCommand(ctx, projectPath)
	if strings.TrimSpace(cmd) == "" {
		reason := ""
		if ctx == nil || ctx.Err() == nil {
			reason = cmakeVerifySkippedReason(projectPath)
		}
		return true, "", reason, true
	}
	if taskScoped {
		scoped, scopeSkip, scopeReason := scopeGoVerifyToTaskAt(cmd, projectPath, commandFiles, localVerifyBuildOutDir())
		if scopeSkip {
			return true, "", scopeReason, true
		}
		cmd = scoped
	}
	return executeCodingWorkbenchStepVerify(ctx, projectPath, cmd, judgeFiles, taskScoped)
}

func executeCodingWorkbenchStepVerify(ctx context.Context, projectPath, cmd string, taskFiles []string, taskScoped bool) (ok bool, outCmd, output string, skipped bool) {
	// Same launcher as the coding agent's bash tool: absolute PowerShell,
	// UTF-8, and the MSVC/MinGW/CMake PATH repair. A bare "powershell" fails
	// when the GUI process inherits a stripped PATH, so cmake, cl, and g++
	// are resolved from the injected toolchain directories.
	res := executeCodingBashWithContext(ctx, map[string]interface{}{
		"command":      cmd,
		"working_dir":  projectPath,
		"timeout":      corelib.DefaultAgentTimeoutSec,
		"stderr_limit": verifyGateStderrBytes,
	}, nil)
	out := strings.TrimSpace(res.Text)
	// Judge the full log. Truncating first can drop the related diagnostic
	// and leave only a historical vet nit in the kept prefix.
	ok, cmd, out, skipped = acceptTaskVerifyResult(res.succeeded(), cmd, out, projectPath, taskFiles, taskScoped)
	if taskScoped && !ok {
		out = promoteRelatedGoDiagnostics(out, projectPath, taskFiles)
	}
	if utf8.RuneCountInString(out) > 2000 {
		out = truncateRunesForSubAgent(out, 2000)
	}
	return ok, cmd, out, skipped
}

// codingWorkbenchStepGateSummary formats verify result for step status / report.
func codingWorkbenchStepGateSummary(ok bool, cmd, output string, skipped bool) string {
	if skipped {
		if reason := strings.TrimSpace(output); reason != "" {
			return "step verify skipped (" + reason + ")"
		}
		return "step verify skipped (no project verify command detected)"
	}
	status := "PASS"
	if !ok {
		status = "FAIL"
	}
	return fmt.Sprintf("step verify %s: %s\n%s", status, cmd, truncateRunesForSubAgent(output, 600))
}

// verifyLogHasCompilerDiagnostic reports whether a failed verify log contains
// a Go compiler diagnostic. Analyzer nits and bare toolchain lines do not.
func verifyLogHasCompilerDiagnostic(output string) bool {
	for _, line := range strings.Split(normalizeGoVerifyOutput(output), "\n") {
		match := goVerifyDiagRE.FindStringSubmatch(stripGoVetPrefix(strings.TrimSpace(line)))
		if match != nil && isCompilerDiagnostic(match[3]) {
			return true
		}
	}
	return false
}

// compilerDiagnosticFiles lists project files named by compiler diagnostics.
// The same file is returned once. Paths outside the project are left out;
// the raw log in the repair prompt still shows them.
func compilerDiagnosticFiles(projectPath, output string) []string {
	var files []string
	for _, line := range strings.Split(normalizeGoVerifyOutput(output), "\n") {
		match := goVerifyDiagRE.FindStringSubmatch(stripGoVetPrefix(strings.TrimSpace(line)))
		if match == nil || !isCompilerDiagnostic(match[3]) {
			continue
		}
		rel := taskRelativePath(projectPath, match[1])
		if rel == "" || strings.HasPrefix(rel, "../") || taskPathCovers(files, rel) {
			continue
		}
		files = append(files, rel)
		if len(files) >= 12 {
			break
		}
	}
	return files
}

// compileRepairTaskText is the user message for one automatic compile-repair
// turn. The marker is first so localization stays off. The diagnostic is
// kept inside the task-description budget.
func compileRepairTaskText(cmd, output string, files []string) string {
	var head strings.Builder
	head.WriteString(hostCompileRepairMarker)
	head.WriteString("\n验证没有通过，因为代码编译不过。请改诊断点名的文件，让这些诊断消失。点名的文件都在本次范围内，即使原先任务没有改它们。只做让它们重新能编译的最小修改。不要用 git checkout 或 git restore 回滚整文件，也不要以别人的文件或约定为由停下来。改完不要自己再跑验证命令，宿主会重跑。\n")
	if strings.TrimSpace(cmd) != "" {
		head.WriteString("\n验证命令：")
		head.WriteString(strings.TrimSpace(cmd))
		head.WriteString("\n")
	}
	if len(files) > 0 {
		head.WriteString("\n点名文件：\n")
		for _, file := range files {
			head.WriteString("- ")
			head.WriteString(file)
			head.WriteString("\n")
		}
	}
	head.WriteString("\n诊断：\n")
	prefix := head.String()
	room := codingSubAgentTaskDescriptionMaxRunes - utf8.RuneCountInString(prefix) - 20
	if room < 200 {
		room = 200
	}
	return prefix + truncateRunesV2(strings.TrimSpace(output), room)
}

// stepCompileRepairTurn is one model attempt. Edited means the workspace
// may have changed, so the host verifies again. Stop means do not start
// another model turn. Cancelled ends the step.
type stepCompileRepairTurn struct {
	Modified  []string
	Created   []string
	Edited    bool
	Stop      bool
	Cancelled bool
}

// stepCompileRepairOutcome is the gate's decision after the bounded repair.
type stepCompileRepairOutcome struct {
	OK        bool
	Attempted bool
	Cancelled bool
	Cmd       string
	Output    string
	Note      string
	Modified  []string
	Created   []string
}

type stepCompileRepairUsage struct {
	Iterations     int
	ToolCalls      int
	InputTokens    int
	OutputTokens   int
	LastPrompt     int
	LastCompletion int
	Cost           float64
}

func (u *stepCompileRepairUsage) addLocal(result *CodingSubAgentResult) {
	if u == nil || result == nil {
		return
	}
	u.Iterations += result.Iterations
	u.ToolCalls += result.ToolCalls
	u.InputTokens += result.InputTokens
	u.OutputTokens += result.OutputTokens
	u.Cost += result.EstCostRMB
	if result.LastPromptTokens > 0 {
		u.LastPrompt = result.LastPromptTokens
	}
	if result.LastCompletionTokens > 0 {
		u.LastCompletion = result.LastCompletionTokens
	}
}

func (u *stepCompileRepairUsage) addRemote(result *RemoteCodingSubAgentResult) {
	if u == nil || result == nil {
		return
	}
	u.Iterations += result.Iterations
	u.ToolCalls += result.ToolCalls
	u.InputTokens += result.InputTokens
	u.OutputTokens += result.OutputTokens
	u.Cost += result.EstCostRMB
	if result.LastPromptTokens > 0 {
		u.LastPrompt = result.LastPromptTokens
	}
	if result.LastCompletionTokens > 0 {
		u.LastCompletion = result.LastCompletionTokens
	}
}

func stepRepairCancelled(ctx context.Context, loopCtx *LoopContext) bool {
	if ctx != nil && ctx.Err() != nil {
		return true
	}
	return loopCtx != nil && loopCtx.IsCancelled()
}

func codingResultCancelled(result *CodingSubAgentResult) bool {
	if result == nil {
		return false
	}
	errText := strings.TrimSpace(result.Error)
	lower := strings.ToLower(errText)
	switch result.Status {
	case TaskExecSkipped, TaskExecInterrupted:
		return strings.Contains(lower, "cancel") || strings.Contains(errText, "取消")
	case TaskExecFailed:
		// The agent loop records a user stop as a failed result whose error
		// is "cancelled" or "cancelled during LLM retry", not as an
		// interrupted status. A quality-gate sentence is not that stop.
		switch lower {
		case "cancelled", "canceled", "task_cancelled":
			return true
		}
		return strings.HasPrefix(lower, "cancelled ") || strings.HasPrefix(lower, "canceled ")
	default:
		return false
	}
}

func remoteResultCancelled(result *RemoteCodingSubAgentResult) bool {
	if result == nil {
		return false
	}
	return remoteCodingResultIsUserCancel(result.Status)
}

func activeSubAgentTaskFunc() subAgentTaskFunc {
	if runTaskWithSubAgent != nil {
		return runTaskWithSubAgent
	}
	return RunTaskWithSubAgent
}

// runStepVerifyCompileRepair edits the files a compiler diagnostic names,
// then runs the same verification again. It stops when the build passes,
// the turn does not edit, the failure is no longer a compile error, or the
// round budget is spent. Vet-only failures never reach this function.
func runStepVerifyCompileRepair(
	ctx context.Context,
	projectPath, verifyCmd, output string,
	turn func(ctx context.Context, round int, taskText string, files []string) stepCompileRepairTurn,
	reverify func(ctx context.Context) (ok bool, cmd, output string, skipped bool),
) stepCompileRepairOutcome {
	out := stepCompileRepairOutcome{Cmd: verifyCmd, Output: output}
	curCmd, curOut := verifyCmd, output
	for round := 1; round <= subAgentMaxVerifyFixRounds; round++ {
		if ctx != nil && ctx.Err() != nil {
			out.Cancelled = true
			out.Attempted = true
			return out
		}
		files := compilerDiagnosticFiles(projectPath, curOut)
		// An empty list means the compiler named nothing inside this project.
		// Starting a turn then would claim the whole project root.
		if len(files) == 0 {
			out.OK = false
			if out.Attempted {
				out.Note = stepCompileRepairStillFailNote
			}
			return out
		}
		got := turn(ctx, round, compileRepairTaskText(curCmd, curOut, files), files)
		out.Modified = append(out.Modified, got.Modified...)
		out.Created = append(out.Created, got.Created...)
		out.Attempted = true
		if got.Cancelled {
			out.Cancelled = true
			return out
		}
		if !got.Edited {
			out.Note = stepCompileRepairStillFailNote
			out.OK = false
			return out
		}
		if ctx != nil && ctx.Err() != nil {
			out.Cancelled = true
			return out
		}
		ok, cmd, next, skipped := reverify(ctx)
		if ctx != nil && ctx.Err() != nil {
			out.Cancelled = true
			return out
		}
		if strings.TrimSpace(cmd) != "" {
			curCmd = cmd
		}
		if strings.TrimSpace(next) != "" {
			curOut = next
		}
		out.Cmd = curCmd
		out.Output = curOut
		if ok && !skipped {
			out.OK = true
			out.Note = stepCompileRepairFixedNote
			return out
		}
		out.OK = false
		if skipped || got.Stop || !verifyLogHasCompilerDiagnostic(curOut) {
			out.Note = stepCompileRepairStillFailNote
			return out
		}
	}
	out.OK = false
	out.Note = stepCompileRepairStillFailNote
	return out
}

func runLocalStepCompileRepair(
	ctx context.Context,
	loopCtx *LoopContext,
	cfg corelib.MaclawLLMConfig,
	httpClient *http.Client,
	handler *IMMessageHandler,
	projectPath string,
	taskFiles []string,
	stepIndex int,
	verifyCmd, output string,
	onToken func(string),
	onProgress func(string),
) (stepCompileRepairOutcome, stepCompileRepairUsage) {
	var usage stepCompileRepairUsage
	// commandFiles is the original task plus files the repair edited.
	// judgeFiles stays the original task. A dependency the repair opened is
	// typechecked, and its existing analyzer findings stay historical.
	commandFiles := append([]string{}, taskFiles...)
	judgeFiles := append([]string{}, taskFiles...)
	outcome := runStepVerifyCompileRepair(ctx, projectPath, verifyCmd, output, func(ctx context.Context, round int, taskText string, files []string) stepCompileRepairTurn {
		if stepRepairCancelled(ctx, loopCtx) {
			return stepCompileRepairTurn{Stop: true, Cancelled: true}
		}
		if onProgress != nil {
			onProgress(fmt.Sprintf("T%d 编译未通过，正在自动修复（%d/%d）", stepIndex, round, subAgentMaxVerifyFixRounds))
		}
		logStepCompileRepair(stepIndex, round, verifyCmd, files)
		task := &TaskItem{
			Index:       stepIndex,
			Title:       "让验证重新通过编译",
			Description: taskText,
			Files:       append([]string{}, files...),
			RequestKind: codingRequestImplementation,
		}
		result := activeSubAgentTaskFunc()(handler, cfg, httpClient, task, projectPath, "", "", nil, loopCtx, onToken, onProgress)
		if result == nil {
			return stepCompileRepairTurn{Stop: true}
		}
		usage.addLocal(result)
		commandFiles = append(commandFiles, result.FilesModified...)
		commandFiles = append(commandFiles, result.FilesCreated...)
		turn := stepCompileRepairTurn{
			Modified: result.FilesModified,
			Created:  result.FilesCreated,
			Edited:   len(result.FilesModified)+len(result.FilesCreated) > 0,
		}
		if strings.TrimSpace(result.AskQuestion) != "" {
			turn.Stop = true
		}
		if stepRepairCancelled(ctx, loopCtx) || codingResultCancelled(result) {
			turn.Stop = true
			turn.Cancelled = true
		}
		return turn
	}, func(ctx context.Context) (bool, string, string, bool) {
		return runLocalStepVerifyForRepair(ctx, projectPath, commandFiles, judgeFiles)
	})
	applyCompileRepairLoopCancel(loopCtx, &outcome)
	return outcome, usage
}

// runLocalStepVerifyForRepair is the local verify a compile repair reruns.
// commandFiles is the compile scope. judgeFiles is the step's accountability
// set. Tests replace it.
var runLocalStepVerifyForRepair = runCodingWorkbenchStepVerifyForRepair

// runRemoteStepVerifyForRepair is the SSH verify a remote compile repair
// reruns. The two file lists match the local repair verify. Tests replace it.
var runRemoteStepVerifyForRepair = runCodingWorkbenchRemoteStepVerifyForRepair

// applyCompileRepairLoopCancel records a user stop that landed after the
// policy already accepted a passing reverify. The verify command can return
// before the context the policy watches observes the closed loop.
func applyCompileRepairLoopCancel(loopCtx *LoopContext, outcome *stepCompileRepairOutcome) {
	if outcome == nil || loopCtx == nil || outcome.Cancelled || !loopCtx.IsCancelled() {
		return
	}
	outcome.Cancelled = true
	outcome.OK = false
	outcome.Attempted = true
	outcome.Note = ""
}

// stepRepairContext is the context the compile-repair policy watches.
// A remote step has no workflow context. User stop closes the loop, and the
// SSH verify cannot be aborted, so this context must still end when the loop
// does. The returned cancel func stops the listen goroutine.
func stepRepairContext(parent context.Context, loopCtx *LoopContext) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	if loopCtx == nil || loopCtx.CancelC == nil {
		return ctx, cancel
	}
	if loopCtx.IsCancelled() {
		cancel()
		return ctx, cancel
	}
	cancelCh := loopCtx.CancelC
	go func() {
		select {
		case <-cancelCh:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func runRemoteStepCompileRepair(
	ctx context.Context,
	loopCtx *LoopContext,
	cfg corelib.MaclawLLMConfig,
	httpClient *http.Client,
	handler *IMMessageHandler,
	remoteCtx remoteCodingTemplateContext,
	runner remoteCodingTemplateRunnerFunc,
	projectPath string,
	taskFiles []string,
	stepIndex int,
	verifyCmd, output string,
	onToken func(string),
	onProgress func(string),
) (stepCompileRepairOutcome, stepCompileRepairUsage) {
	if runner == nil {
		runner = defaultRemoteCodingTemplateRunner
	}
	// Bind the loop before the policy runs. A nil parent is the remote call
	// site: stop still has to cancel this context, or a verify that finishes
	// after the stop is reported as a pass and the plan continues.
	repairListen, stopRepair := stepRepairContext(ctx, loopCtx)
	defer stopRepair()
	noteRepairStop := func() {
		if stepRepairCancelled(repairListen, loopCtx) {
			stopRepair()
		}
	}
	template := remoteCtx
	template.RequestKind = codingRequestImplementation
	template.OperationalAcceptance = ""
	var usage stepCompileRepairUsage
	sessionID := remoteCtx.SessionID
	commandFiles := append([]string{}, taskFiles...)
	judgeFiles := append([]string{}, taskFiles...)
	outcome := runStepVerifyCompileRepair(repairListen, projectPath, verifyCmd, output, func(ctx context.Context, round int, taskText string, files []string) stepCompileRepairTurn {
		noteRepairStop()
		if stepRepairCancelled(ctx, loopCtx) {
			return stepCompileRepairTurn{Stop: true, Cancelled: true}
		}
		if onProgress != nil {
			onProgress(fmt.Sprintf("T%d 编译未通过，正在自动修复（%d/%d）", stepIndex, round, subAgentMaxVerifyFixRounds))
		}
		logStepCompileRepair(stepIndex, round, verifyCmd, files)
		result := runner(handler, cfg, httpClient, template, loopCtx, taskText, onProgress, onToken)
		if result == nil {
			return stepCompileRepairTurn{Stop: true}
		}
		usage.addRemote(result)
		commandFiles = append(commandFiles, result.FilesModified...)
		commandFiles = append(commandFiles, result.FilesCreated...)
		turn := stepCompileRepairTurn{
			Modified: result.FilesModified,
			Created:  result.FilesCreated,
			Edited:   len(result.FilesModified)+len(result.FilesCreated) > 0,
		}
		noteRepairStop()
		if stepRepairCancelled(ctx, loopCtx) || remoteResultCancelled(result) {
			turn.Stop = true
			turn.Cancelled = true
		}
		return turn
	}, func(ctx context.Context) (bool, string, string, bool) {
		noteRepairStop()
		if ctx != nil && ctx.Err() != nil {
			return false, verifyCmd, output, false
		}
		ok, cmd, out, skipped := runRemoteStepVerifyForRepair(handler, sessionID, projectPath, commandFiles, judgeFiles)
		// The build may have passed after the user already stopped. Cancel
		// the watched context here, before the policy reads the pass.
		noteRepairStop()
		return ok, cmd, out, skipped
	})
	applyCompileRepairLoopCancel(loopCtx, &outcome)
	return outcome, usage
}

func logStepCompileRepair(stepIndex, round int, verifyCmd string, files []string) {
	log.Printf("[workflow-v2] T%d compile repair %d/%d files=%d: %s", stepIndex, round, subAgentMaxVerifyFixRounds, len(files), verifyCmd)
}
