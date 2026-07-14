package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"
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
	cmd = detectProjectVerifyCommand(projectPath)
	if strings.TrimSpace(cmd) == "" {
		return true, "", "", true
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
	} else {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
	}

	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", cmd)
	} else {
		c = exec.CommandContext(ctx, "bash", "-lc", cmd)
	}
	c.Dir = projectPath
	var buf bytes.Buffer
	c.Stdout = &buf
	c.Stderr = &buf
	err := c.Run()
	out := strings.TrimSpace(buf.String())
	if utf8.RuneCountInString(out) > 2000 {
		out = truncateRunesForSubAgent(out, 2000)
	}
	if err != nil {
		if out == "" {
			out = err.Error()
		} else {
			out = out + "\n" + err.Error()
		}
		return false, cmd, out, false
	}
	if out == "" {
		out = "(verify command completed with no output)"
	}
	return true, cmd, out, false
}

// codingWorkbenchStepGateSummary formats verify result for step status / report.
func codingWorkbenchStepGateSummary(ok bool, cmd, output string, skipped bool) string {
	if skipped {
		return "step verify skipped (no project verify command detected)"
	}
	status := "PASS"
	if !ok {
		status = "FAIL"
	}
	return fmt.Sprintf("step verify %s: %s\n%s", status, cmd, truncateRunesForSubAgent(output, 600))
}
