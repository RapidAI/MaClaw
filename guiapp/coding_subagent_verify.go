package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// ---------------------------------------------------------------------------
// Built-in Verify Loop for CodingSubAgent
//
// Codex-inspired improvement: after the main coding loop completes, if the
// model didn't run verification itself, automatically run the project's
// verification command. If verification fails, inject the error back into
// the SAME conversation context (no new SubAgent, no lost context) and let
// the model fix it. Maximum 2 verify-fix rounds to prevent infinite loops.
//
// Design principles:
// - Only triggers when model skipped verification (hasRunVerification=false)
// - Only triggers when files were actually modified (pure read tasks skip)
// - Reuses existing conversation (model sees its own edits + error output)
// - Max 2 rounds: verify→fix→verify→fix→give up
// - Project verify command is auto-detected from build system files
// ---------------------------------------------------------------------------

const (
	subAgentMaxVerifyFixRounds    = 2
	subAgentVerifyOutputMaxTokens = 2000
)

// detectProjectVerifyCommand scans the project root for build system files
// and returns an appropriate verification command. Returns "" if none detected.
func detectProjectVerifyCommand(projectPath string) string {
	if projectPath == "" {
		return ""
	}

	isWindows := strings.EqualFold(normalizedRemotePlatform(), "windows")

	// Check in priority order (most specific to least specific)
	checks := []struct {
		file    string
		command string
	}{
		{"go.mod", goVerifyCommand()},
		{"Cargo.toml", "cargo check"},
		{"package.json", detectNodeVerifyCommand(projectPath)},
		{"pyproject.toml", pythonVerifyCommand(isWindows)},
		{"CMakeLists.txt", detectCMakeVerifyCommand(projectPath)},
		{"Makefile", makeVerifyCommand(isWindows)},
	}

	for _, c := range checks {
		if c.command == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(projectPath, c.file)); err == nil {
			return c.command
		}
	}
	return ""
}

// goVerifyCommand short-circuits. A semicolon would keep going after a failed
// build, and a later successful go vet would hide that failure. The Windows
// coding shell rewrites && for PowerShell 5.1.
func goVerifyCommand() string {
	return "go build ./... && go vet ./..."
}

func pythonVerifyCommand(isWindows bool) string {
	if isWindows {
		return "python -m py_compile *.py"
	}
	return "python -m pytest --co -q 2>/dev/null || python -m py_compile *.py 2>/dev/null || true"
}

func makeVerifyCommand(isWindows bool) string {
	if isWindows {
		return "" // Makefile on Windows is unreliable
	}
	return "make -n check 2>/dev/null || make -n test 2>/dev/null || make -n build 2>/dev/null || true"
}

func detectNodeVerifyCommand(projectPath string) string {
	pkgPath := filepath.Join(projectPath, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return ""
	}
	var pkg map[string]interface{}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return ""
	}
	scripts, ok := pkg["scripts"].(map[string]interface{})
	if !ok {
		return ""
	}

	// Priority: typecheck > build > test (test may be slow)
	if _, ok := scripts["typecheck"]; ok {
		return "npm run typecheck"
	}
	if _, ok := scripts["build"]; ok {
		return "npm run build"
	}
	if _, ok := scripts["lint"]; ok {
		return "npm run lint"
	}
	return ""
}

// hostBuildTools are absolute executables the host can already run.
// Empty fields are tools this machine does not have.
type hostBuildTools struct {
	cmake   string
	ninja   string
	msbuild string
	make    string
}

var lookupHostBuildTools = currentHostBuildTools

func detectCMakeVerifyCommand(projectPath string) string {
	return selectCMakeVerifyCommand(projectPath, lookupHostBuildTools())
}

// ensureProjectVerifyCommand is the command the host is about to execute.
// Detection stays free of downloads: commit text and prompts also call
// detectProjectVerifyCommand. Installation happens only here, and only when
// a CMake build directory exists and no local builder can drive it.
func ensureProjectVerifyCommand(ctx context.Context, projectPath string) string {
	if ctx != nil && ctx.Err() != nil {
		return ""
	}
	if cmd := detectProjectVerifyCommand(projectPath); cmd != "" {
		return cmd
	}
	if cmakeVerifySkippedReason(projectPath) == "" {
		return ""
	}
	exe := ensurePrivateCMakeInstalled(ctx)
	if exe == "" {
		return ""
	}
	return selectCMakeVerifyCommand(projectPath, hostBuildTools{cmake: exe})
}

func cmakeVerifySkippedReason(projectPath string) string {
	if _, err := os.Stat(filepath.Join(projectPath, "CMakeLists.txt")); err != nil {
		return ""
	}
	buildDir := filepath.Join(projectPath, "build")
	info, err := os.Stat(buildDir)
	if err != nil || !info.IsDir() || !cmakeBuildDirConfigured(buildDir) {
		return ""
	}
	return "CMake build tree present, no cmake/ninja/msbuild available"
}

func cmakeBuildDirConfigured(buildDir string) bool {
	for _, name := range []string{"CMakeCache.txt", "build.ninja", "Makefile"} {
		info, err := os.Stat(filepath.Join(buildDir, name))
		if err == nil && !info.IsDir() {
			return true
		}
	}
	return firstBuildSolution(buildDir) != ""
}

func currentHostBuildTools() hostBuildTools {
	if runtime.GOOS != "windows" {
		return hostBuildTools{
			cmake: existingExecutable(lookPathTool("cmake")),
			ninja: existingExecutable(lookPathTool("ninja")),
			make:  existingExecutable(lookPathTool("make")),
		}
	}
	tools := hostBuildTools{msbuild: detectWindowsMSBuild()}
	if dir := detectWindowsCMakeBin(); dir != "" {
		tools.cmake = filepath.Join(dir, "cmake.exe")
	}
	if dir := detectWindowsNinjaBin(); dir != "" {
		tools.ninja = filepath.Join(dir, "ninja.exe")
	}
	if dir := detectWindowsMinGWBin(); dir != "" {
		for _, name := range []string{"mingw32-make.exe", "make.exe"} {
			candidate := filepath.Join(dir, name)
			if existingExecutable(candidate) != "" {
				tools.make = candidate
				break
			}
		}
	}
	return tools
}

func lookPathTool(name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return path
}

// selectCMakeVerifyCommand picks a build command that does not depend on the
// GUI process PATH. A bare "cmake" is never returned: on other machines that
// becomes a PowerShell CommandNotFound and fails a step the agent already built.
func selectCMakeVerifyCommand(projectPath string, tools hostBuildTools) string {
	buildDir := filepath.Join(projectPath, "build")
	info, err := os.Stat(buildDir)
	if err != nil || !info.IsDir() {
		return ""
	}
	cmakePath, makeProgram := readCMakeCacheTools(buildDir)
	if cmakePath != "" {
		return quoteVerifyExecutable(cmakePath) + " --build build"
	}
	if cmake := existingExecutable(tools.cmake); cmake != "" {
		return quoteVerifyExecutable(cmake) + " --build build"
	}
	if cmd := commandForCacheBuildTool(buildDir, makeProgram); cmd != "" {
		return cmd
	}
	if existingExecutable(filepath.Join(buildDir, "build.ninja")) != "" {
		if ninja := existingExecutable(tools.ninja); ninja != "" {
			return quoteVerifyExecutable(ninja) + " -C build"
		}
	}
	if sln := firstBuildSolution(buildDir); sln != "" {
		if msbuild := existingExecutable(tools.msbuild); msbuild != "" {
			return quoteVerifyExecutable(msbuild) + " " + shellQuoteVerifyToken(sln)
		}
	}
	if existingExecutable(filepath.Join(buildDir, "Makefile")) != "" {
		if makeExe := existingExecutable(tools.make); makeExe != "" {
			return quoteVerifyExecutable(makeExe) + " -C build"
		}
	}
	return ""
}

func commandForCacheBuildTool(buildDir, tool string) string {
	tool = existingExecutable(tool)
	if tool == "" {
		return ""
	}
	switch cmakeBuildToolKind(tool) {
	case "ninja":
		return quoteVerifyExecutable(tool) + " -C build"
	case "make":
		return quoteVerifyExecutable(tool) + " -C build"
	case "msbuild":
		if sln := firstBuildSolution(buildDir); sln != "" {
			return quoteVerifyExecutable(tool) + " " + shellQuoteVerifyToken(sln)
		}
	}
	return ""
}

func cmakeBuildToolKind(tool string) string {
	switch strings.ToLower(filepath.Base(tool)) {
	case "ninja", "ninja.exe":
		return "ninja"
	case "make", "make.exe", "gmake", "gmake.exe", "mingw32-make", "mingw32-make.exe":
		return "make"
	case "msbuild", "msbuild.exe":
		return "msbuild"
	default:
		return ""
	}
}

func firstBuildSolution(buildDir string) string {
	entries, err := os.ReadDir(buildDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".sln") {
			return filepath.Join(buildDir, entry.Name())
		}
	}
	return ""
}

func existingExecutable(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return ""
	}
	return path
}

func readCMakeCacheTools(buildDir string) (cmakePath, makeProgram string) {
	data, err := os.ReadFile(filepath.Join(buildDir, "CMakeCache.txt"))
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		name, _, _ := strings.Cut(key, ":")
		if name != "CMAKE_COMMAND" && name != "CMAKE_MAKE_PROGRAM" {
			continue
		}
		value = existingExecutable(filepath.Clean(filepath.FromSlash(strings.Trim(strings.TrimSpace(value), `"'`))))
		if value == "" {
			continue
		}
		switch name {
		case "CMAKE_COMMAND":
			if cmakePath == "" {
				cmakePath = value
			}
		case "CMAKE_MAKE_PROGRAM":
			if makeProgram == "" {
				makeProgram = value
			}
		}
		if cmakePath != "" && makeProgram != "" {
			break
		}
	}
	return cmakePath, makeProgram
}

func shellQuoteVerifyToken(token string) string {
	if !strings.ContainsAny(token, " \t\"") {
		return token
	}
	return `"` + strings.ReplaceAll(token, `"`, "") + `"`
}

// quoteVerifyExecutable makes a path runnable by Windows PowerShell 5.1.
// A quoted program path is an expression unless the call operator is present,
// so "C:\Program Files\CMake\bin\cmake.exe" --build would not start cmake.
func quoteVerifyExecutable(exe string) string {
	// Keep the command itself free of a leading &. The shell tokenizer treats
	// & as a command boundary, which would hide cmake from every detector.
	// windowsCodingBashInvocation adds the PowerShell call operator at launch.
	return shellQuoteVerifyToken(exe)
}

// runPostLoopVerification executes one host-selected verification command and
// records it in the same audit ledger used by normal bash calls. Recovery uses
// it to obtain direct exit-status evidence without entering a mutating fix loop.
func (s *CodingSubAgent) runPostLoopVerification(
	cb *codingSubAgentCallbacks,
	result *agent.LoopResult,
	verifyCmd string,
	workingDir string,
	traj *TrajectoryRecorder,
) bool {
	if cb == nil || result == nil || strings.TrimSpace(verifyCmd) == "" || cb.ShouldStop() {
		return false
	}
	if strings.TrimSpace(workingDir) == "" {
		workingDir = s.projectPath
	}
	log.Printf("[coding-subagent-verify] running recovered verifier %q", verifyCmd)
	if s.onProgress != nil {
		s.onProgress("验证中: " + verifyCmd)
	}
	verifyArgs := fmt.Sprintf(`{"command":%q,"working_dir":%q,"timeout":60}`, verifyCmd, workingDir)
	verifyResult := cb.ExecuteTool("bash", verifyArgs)
	ledgerKnown, ledgerFailed := commandLedgerVerificationOutcome(cb, verifyCmd)
	verifyFailed := postLoopVerificationFailed(verifyResult, ledgerKnown, ledgerFailed)
	outcome := "succeeded"
	if verifyFailed {
		outcome = "failed"
	}
	recordSubAgentPostLoopVerify(traj, 1, verifyCmd, verifyArgs, verifyResult, outcome)
	result.ToolCalls++
	if verifyFailed {
		log.Printf("[coding-subagent-verify] recovered verification FAILED")
		return false
	}
	if s.onProgress != nil {
		s.onProgress("验证通过")
	}
	return true
}

// runPostLoopVerifyFixCycle runs automatic verification after the main coding
// loop completes. If verification fails, injects the error into the same
// conversation and lets the model fix it (up to maxRounds times).
//
// Returns the updated LoopResult (iterations/toolCalls accumulated).
func (s *CodingSubAgent) runPostLoopVerifyFixCycle(
	cb *codingSubAgentCallbacks,
	result *agent.LoopResult,
	verifyCmd string,
	traj *TrajectoryRecorder,
) {
	for round := 0; round < subAgentMaxVerifyFixRounds; round++ {
		if cb.ShouldStop() {
			if result != nil && strings.TrimSpace(result.Error) == "" {
				result.Error = "cancelled"
			}
			return
		}

		log.Printf("[coding-subagent-verify] round %d/%d: running %q", round+1, subAgentMaxVerifyFixRounds, verifyCmd)
		if s.onProgress != nil {
			s.onProgress(fmt.Sprintf("验证中 (round %d/%d): %s", round+1, subAgentMaxVerifyFixRounds, verifyCmd))
		}

		// Run verification command
		verifyArgs := fmt.Sprintf(`{"command":%q,"working_dir":%q,"timeout":60}`, verifyCmd, s.projectPath)
		verifyResult := cb.ExecuteTool("bash", verifyArgs)
		ledgerKnown, ledgerFailed := commandLedgerVerificationOutcome(cb, verifyCmd)
		verifyFailed := postLoopVerificationFailed(verifyResult, ledgerKnown, ledgerFailed)
		outcome := "succeeded"
		if verifyFailed {
			outcome = "failed"
		}
		recordSubAgentPostLoopVerify(traj, round+1, verifyCmd, verifyArgs, verifyResult, outcome)
		// Count automatic verify bash as a tool call for session accounting.
		result.ToolCalls++

		if !verifyFailed {
			log.Printf("[coding-subagent-verify] round %d: verification PASSED", round+1)
			if s.onProgress != nil {
				s.onProgress("验证通过")
			}
			return
		}

		log.Printf("[coding-subagent-verify] round %d: verification FAILED, starting fix loop", round+1)

		// Verification failed — run a new agent loop with the fix prompt.
		// We don't reuse the conversation directly (type mismatch). Instead,
		// start a fresh mini-loop with a focused fix prompt that includes
		// the verification error. The SubAgent's system prompt provides
		// enough project context for the fix.
		truncatedOutput := truncateForSubAgentVerify(verifyResult, subAgentVerifyOutputMaxTokens)
		fixPrompt := fmt.Sprintf(
			"验证命令 `%s` 执行失败。错误输出：\n```\n%s\n```\n\n请分析错误并修复代码使验证通过。只做最小必要修改。修复后不要重新运行验证命令（系统会自动运行）。",
			verifyCmd, truncatedOutput)

		// Run fix loop as a fresh RunLoop (shares system prompt + tools via cb)
		fixResult := agentruntime.RunAgentTurn(cb, fixPrompt, nil, s.httpClient)
		// Append fix-loop turns into the same trajectory session.
		appendSubAgentLoopResult(traj, fixResult, false)

		// Merge results
		result.Iterations += fixResult.Iterations
		result.ToolCalls += fixResult.ToolCalls
		result.Usage.Add(fixResult.Usage)
		if fixResult.Text != "" {
			result.Text = fixResult.Text
		}
		if fixResult.Error != "" {
			result.Error = fixResult.Error
		}
		if fixResult.HardExit {
			result.HardExit = true
		}
	}

	// Exhausted all fix rounds — surface failure so trajectory seals as error
	// and task status does not look like a clean success.
	log.Printf("[coding-subagent-verify] exhausted %d verify-fix rounds", subAgentMaxVerifyFixRounds)
	if s.onProgress != nil {
		s.onProgress(fmt.Sprintf("验证未通过（已尝试 %d 次修复）", subAgentMaxVerifyFixRounds))
	}
	if result != nil && strings.TrimSpace(result.Error) == "" {
		result.Error = fmt.Sprintf("post-loop verification failed after %d rounds (%s)", subAgentMaxVerifyFixRounds, verifyCmd)
	}
}

// isSubAgentVerificationFailure checks if bash output indicates a verification failure.
// Primary signal: exit code (SubAgent's bash tool includes exit status in output).
// Fallback: keyword detection for tools that don't report exit code.
func isSubAgentVerificationFailure(output string) bool {
	output = strings.TrimSpace(output)
	if output == "" || strings.HasPrefix(output, "(command completed with no output)") {
		return false // No output usually means success for build commands
	}

	// Primary: check for explicit exit code reporting from bash tool.
	// SubAgent's bash returns "exit status N" or "Exit Code: N" on failure.
	lower := strings.ToLower(output)
	if strings.Contains(lower, "exit status") || strings.Contains(lower, "exit code") {
		// "exit status 0" / "exit code: 0" = success
		if strings.Contains(lower, "exit status 0") || strings.Contains(lower, "exit code: 0") {
			return false
		}
		// Any non-zero exit code = failure
		return true
	}

	// Fallback: structural failure indicators (more specific than just "error")
	failureIndicators := []string{
		"fatal error",
		"compilation failed",
		"build failed",
		"test failed",
		"cannot find module",
		"cannot find package",
		"undefined:",
		"unresolved reference",
		"panic:",
		"segfault",
	}
	for _, indicator := range failureIndicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}
	return false
}

// postLoopVerificationFailed combines the tool's textual result with the
// command ledger. When the verifier appears in the ledger, its exit outcome
// is authoritative: successful tests may intentionally print phrases such as
// "exit status" or "build failed" as fixture data. Text remains a fallback
// only when no matching command result was recorded.
func postLoopVerificationFailed(output string, ledgerKnown, ledgerFailed bool) bool {
	if ledgerKnown {
		return ledgerFailed
	}
	return isSubAgentVerificationFailure(output)
}

// commandLedgerVerificationOutcome returns the final matching verifier result.
// Commands are searched newest-first so a successful retry supersedes an
// earlier failed attempt.
func commandLedgerVerificationOutcome(cb *codingSubAgentCallbacks, verifyCmd string) (known, failed bool) {
	if cb == nil {
		return false, false
	}
	want := normalizedSubAgentVerificationCommand(verifyCmd)
	if want == "" {
		return false, false
	}
	commands := cb.getCommandsRun()
	for i := len(commands) - 1; i >= 0; i-- {
		if normalizedSubAgentVerificationCommand(commands[i].Command) == want {
			return true, !commands[i].Succeeded
		}
	}
	return false, false
}

func normalizedSubAgentVerificationCommand(command string) string {
	return strings.ToLower(strings.Join(strings.Fields(command), " "))
}

// truncateForSubAgentVerify truncates verification output to fit in a fix prompt
// while preserving the most useful error information.
func truncateForSubAgentVerify(output string, maxTokens int) string {
	// Use the bash truncation logic which prioritizes error lines
	tokenEst := (len(output)*10 + 24) / 25
	if tokenEst <= maxTokens {
		return output
	}
	return truncateSubAgentBashResult(output)
}

// hasSubAgentSelfVerified checks whether the loop already produced a fresh,
// auditable verification result. It deliberately shares the quality gate's
// command classifier instead of keeping a smaller text-pattern list: otherwise
// the post-loop verifier can skip recovery for a command that the final audit
// will reject.
func hasSubAgentSelfVerified(cb *codingSubAgentCallbacks) bool {
	if cb == nil {
		return false
	}
	commands := cb.getCommandsRun()
	if len(commands) == 0 {
		return false
	}
	for i := len(commands) - 1; i >= 0; i-- {
		command := commands[i]
		if isUnsafeSubAgentVerificationCommand(command.Command) {
			return false
		}
		if isSubAgentVerificationCommand(command.Command) {
			return command.Succeeded
		}
	}
	return false
}
