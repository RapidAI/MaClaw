package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
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
		scoped, scopeSkip, scopeReason := scopeGoVerifyToTaskAt(cmd, projectPath, taskFiles, localVerifyBuildOutDir())
		if scopeSkip {
			return true, "", scopeReason, true
		}
		cmd = scoped
	}
	// The gate judges this task's bytes against committed dependencies. A shared
	// checkout still holds files another task left uncommitted; compiling there
	// fails this step on that other task's syntax error. Those paths are shown
	// to the compiler as HEAD. This task's files stay as written. A non-git
	// directory stays on the working tree: hiding that would skip the gate.
	verifyDir := projectPath
	var releaseVerify func()
	if taskScoped {
		if overlay, release, overlayErr := committedDepOverlay(projectPath, taskFiles); overlayErr == nil && overlay != "" {
			cmd = injectGoOverlayFlag(cmd, overlay)
			releaseVerify = release
		}
	}

	if releaseVerify != nil {
		defer releaseVerify()
	}
	// Same launcher as the coding agent's bash tool: absolute PowerShell,
	// UTF-8, and the MSVC/MinGW/CMake PATH repair. A bare "powershell" fails
	// when the GUI process inherits a stripped PATH, so cmake, cl, and g++
	// are resolved from the injected toolchain directories.
	res := executeCodingBashWithContext(ctx, map[string]interface{}{
		"command":      cmd,
		"working_dir":  verifyDir,
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
// committedDepOverlay points the Go compiler at HEAD for every dirty path this
// task did not change. The working tree is not rewritten. Another task's
// uncommitted syntax error therefore cannot fail this step, and this task's
// own files are still the ones the compiler typechecks.
func committedDepOverlay(projectPath string, taskFiles []string) (string, func(), error) {
	projectPath = strings.TrimSpace(projectPath)
	gitRoot, ok := remote.DetectGitWorkspaceRoot(projectPath)
	if !ok || !remote.GitRepoHasCommits(gitRoot) {
		return "", nil, nil
	}
	dirty, err := verifyDirtyPaths(gitRoot)
	if err != nil || len(dirty) == 0 {
		return "", nil, err
	}
	owned := taskOwnedGitPaths(gitRoot, projectPath, taskFiles)
	repl := map[string]string{}
	tmp, err := os.MkdirTemp("", "maclaw-verify-overlay-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	n := 0
	for _, item := range dirty {
		if gitPathOwned(item.rel, owned) {
			continue
		}
		disk, err := filepath.Abs(filepath.Join(gitRoot, filepath.FromSlash(item.rel)))
		if err != nil {
			cleanup()
			return "", nil, err
		}
		if item.inHead {
			body, err := gitShowHead(gitRoot, item.rel)
			if err != nil {
				continue
			}
			backing := filepath.Join(tmp, fmt.Sprintf("f%d%s", n, filepath.Ext(item.rel)))
			if err := os.WriteFile(backing, body, 0o644); err != nil {
				cleanup()
				return "", nil, err
			}
			repl[disk] = backing
		} else {
			repl[disk] = ""
		}
		n++
	}
	if len(repl) == 0 {
		cleanup()
		return "", nil, nil
	}
	payload, err := json.Marshal(map[string]any{"Replace": repl})
	if err != nil {
		cleanup()
		return "", nil, err
	}
	overlay := filepath.Join(tmp, "overlay.json")
	if err := os.WriteFile(overlay, payload, 0o644); err != nil {
		cleanup()
		return "", nil, err
	}
	return overlay, cleanup, nil
}

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

type verifyDirtyPath struct {
	rel    string
	inHead bool
}

func verifyDirtyPaths(gitRoot string) ([]verifyDirtyPath, error) {
	out, err := exec.Command("git", "-C", gitRoot, "status", "--porcelain", "-z", "--untracked-files=all").Output()
	if err != nil {
		return nil, err
	}
	var paths []verifyDirtyPath
	for _, rec := range strings.Split(string(out), "\x00") {
		if len(rec) < 4 {
			continue
		}
		rel := strings.TrimSpace(strings.ReplaceAll(rec[3:], `\`, `/`))
		if rel == "" {
			continue
		}
		paths = append(paths, verifyDirtyPath{rel: rel, inHead: rec[0] != '?' && rec[1] != '?'})
	}
	return paths, nil
}

func taskOwnedGitPaths(gitRoot, projectPath string, taskFiles []string) map[string]bool {
	owned := map[string]bool{}
	root := filepath.Clean(gitRoot)
	for _, file := range taskFiles {
		file = strings.TrimSpace(file)
		if file == "" {
			continue
		}
		abs := file
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(projectPath, file)
		}
		abs = filepath.Clean(abs)
		rel, err := filepath.Rel(root, abs)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			rel = filepath.ToSlash(strings.TrimPrefix(strings.ReplaceAll(file, `\`, `/`), "./"))
		} else {
			rel = filepath.ToSlash(rel)
		}
		if rel == "" || rel == "." || strings.HasPrefix(rel, "../") {
			continue
		}
		owned[strings.ToLower(rel)] = true
	}
	return owned
}

func gitPathOwned(rel string, owned map[string]bool) bool {
	rel = strings.ToLower(strings.ReplaceAll(rel, `\`, `/`))
	return owned[rel]
}


func gitShowHead(gitRoot, rel string) ([]byte, error) {
	return exec.Command("git", "-C", gitRoot, "show", "HEAD:"+filepath.ToSlash(rel)).Output()
}

func injectGoOverlayFlag(cmd, overlay string) string {
	flag := "-overlay=" + overlay
	if runtime.GOOS == "windows" {
		quoted := strings.ReplaceAll(flag, `"`, "")
		return "$env:GOFLAGS='" + quoted + "'; " + cmd
	}
	return "GOFLAGS=" + quoteGoArg(flag) + " " + cmd
}
