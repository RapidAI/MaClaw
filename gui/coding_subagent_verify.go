package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
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
		{"go.mod", goVerifyCommand(isWindows)},
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

func goVerifyCommand(isWindows bool) string {
	if isWindows {
		return "go build ./...; go vet ./..."
	}
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

func detectCMakeVerifyCommand(projectPath string) string {
	// Check if build directory exists
	buildDir := filepath.Join(projectPath, "build")
	if info, err := os.Stat(buildDir); err == nil && info.IsDir() {
		return "cmake --build build"
	}
	return ""
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
		fixResult := agent.RunLoop(cb, fixPrompt, nil, s.httpClient)
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
	if output == "" || output == "(command completed with no output)" {
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
