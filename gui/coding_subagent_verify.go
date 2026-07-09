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

// runPostLoopVerifyFixCycle runs automatic verification after the main coding
// loop completes. If verification fails, injects the error into the same
// conversation and lets the model fix it (up to maxRounds times).
//
// Returns the updated LoopResult (iterations/toolCalls accumulated).
func (s *CodingSubAgent) runPostLoopVerifyFixCycle(
	cb *codingSubAgentCallbacks,
	result *agent.LoopResult,
	verifyCmd string,
) {
	for round := 0; round < subAgentMaxVerifyFixRounds; round++ {
		if cb.ShouldStop() {
			return
		}

		log.Printf("[coding-subagent-verify] round %d/%d: running %q", round+1, subAgentMaxVerifyFixRounds, verifyCmd)
		if s.onProgress != nil {
			s.onProgress(fmt.Sprintf("🔍 验证中 (round %d/%d): %s", round+1, subAgentMaxVerifyFixRounds, verifyCmd))
		}

		// Run verification command
		verifyArgs := fmt.Sprintf(`{"command":%q,"working_dir":%q,"timeout":60}`, verifyCmd, s.projectPath)
		verifyResult := cb.ExecuteTool("bash", verifyArgs)

		if !isSubAgentVerificationFailure(verifyResult) {
			log.Printf("[coding-subagent-verify] round %d: verification PASSED", round+1)
			if s.onProgress != nil {
				s.onProgress("✅ 验证通过")
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

		// Merge results
		result.Iterations += fixResult.Iterations
		result.ToolCalls += fixResult.ToolCalls
		if fixResult.Text != "" {
			result.Text = fixResult.Text
		}
	}

	// Exhausted all fix rounds
	log.Printf("[coding-subagent-verify] exhausted %d verify-fix rounds", subAgentMaxVerifyFixRounds)
	if s.onProgress != nil {
		s.onProgress(fmt.Sprintf("⚠️ 验证未通过（已尝试 %d 次修复）", subAgentMaxVerifyFixRounds))
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

// hasSubAgentSelfVerified checks if the model already ran a verification
// command during the loop AND the last such command succeeded.
// Only skips post-loop verify when model both ran AND passed verification.
func hasSubAgentSelfVerified(cb *codingSubAgentCallbacks) bool {
	if cb == nil {
		return false
	}
	commands := cb.getCommandsRun()
	if len(commands) == 0 {
		return false
	}

	// Check if any command looks like a verification command
	verifyPatterns := []string{
		"go build", "go test", "go vet",
		"cargo test", "cargo check", "cargo build",
		"npm run build", "npm run test", "npm run lint", "npm run typecheck",
		"yarn build", "yarn test", "yarn lint",
		"pytest", "python -m pytest",
		"make test", "make check", "make build",
		"cmake --build",
		"tsc", "eslint", "prettier --check",
	}

	// Find the LAST verification command and check if it succeeded.
	// If the last verify failed, post-loop verify should still trigger
	// (model may have given up without fixing).
	lastVerifyIdx := -1
	for i := len(commands) - 1; i >= 0; i-- {
		cmdLower := strings.ToLower(commands[i].Command)
		for _, pattern := range verifyPatterns {
			if strings.Contains(cmdLower, pattern) {
				lastVerifyIdx = i
				break
			}
		}
		if lastVerifyIdx >= 0 {
			break
		}
	}

	if lastVerifyIdx < 0 {
		return false // never ran verification
	}
	return commands[lastVerifyIdx].Succeeded
}
