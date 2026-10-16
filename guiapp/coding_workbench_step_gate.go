package guiapp

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
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
// (skip = not a hard fail).
func runCodingWorkbenchStepVerify(ctx context.Context, projectPath string) (ok bool, cmd, output string, skipped bool) {
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
	// Same launcher as the coding agent's bash tool: absolute PowerShell,
	// UTF-8, and the MSVC/MinGW/CMake PATH repair. A bare "powershell" fails
	// when the GUI process inherits a stripped PATH, so cmake, cl, and g++
	// are resolved from the injected toolchain directories.
	res := executeCodingBashWithContext(ctx, map[string]interface{}{
		"command":     cmd,
		"working_dir": projectPath,
		"timeout":     corelib.DefaultAgentTimeoutSec,
	}, nil)
	out := strings.TrimSpace(res.Text)
	if utf8.RuneCountInString(out) > 2000 {
		out = truncateRunesForSubAgent(out, 2000)
	}
	return res.succeeded(), cmd, out, false
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
