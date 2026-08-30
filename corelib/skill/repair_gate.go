package skill

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// RepairGateConfig configures the sandbox verification gate.
type RepairGateConfig struct {
	// MaxReplayRuns is the maximum number of historical arg sets to replay.
	// Default: 3.
	MaxReplayRuns int

	// MinPassRate is the minimum fraction of successful replays required
	// for the gate to pass. Default: 0.67 (2/3).
	MinPassRate float64

	// SandboxTimeout is the per-execution timeout in the sandbox.
	// Default: 60s.
	SandboxTimeout time.Duration
}

func defaultRepairGateConfig(cfg RepairGateConfig) RepairGateConfig {
	if cfg.MaxReplayRuns <= 0 {
		cfg.MaxReplayRuns = 3
	}
	if cfg.MinPassRate <= 0 {
		cfg.MinPassRate = 0.67
	}
	if cfg.SandboxTimeout <= 0 {
		cfg.SandboxTimeout = 60 * time.Second
	}
	return cfg
}

// SandboxExecutor runs skill steps in an isolated temporary directory.
type SandboxExecutor interface {
	// RunInSandbox executes the given steps with args in a temporary workdir.
	// The implementation must:
	//   1. Create a temp directory as working dir
	//   2. Copy skill files (baseDir) into it if needed
	//   3. Execute steps with the provided args
	//   4. Clean up the temp directory on completion
	//   5. Respect context cancellation / timeout
	RunInSandbox(ctx context.Context, skill *corelib.NLSkillEntry, steps []corelib.NLSkillStep, args map[string]string, timeout time.Duration) (success bool, output string, err error)
}

// NonBashReplayResult is the explicit evidence contract for actions that the
// shell sandbox cannot execute (craft_tool, MCP, browser, poll/loop, etc.).
// An adapter must declare whether the returned evidence is real, mock, or
// unavailable; omitted/unknown modes are fail-closed as unverified.
type NonBashReplayResult struct {
	Success      bool   `json:"success"`
	Output       string `json:"output,omitempty"`
	Error        string `json:"error,omitempty"`
	EvidenceMode string `json:"evidence_mode"` // real | mock | none
}

// NonBashReplayAdapter is an opt-in isolated executor for non-shell actions.
// Implementations must enforce their own side-effect allowlist and must not
// mutate production files, Skill state, upload queues, or publish state.
// Without an adapter, RepairGate always returns unverified for such actions.
type NonBashReplayAdapter interface {
	ReplayNonBash(ctx context.Context, skill *corelib.NLSkillEntry, steps []corelib.NLSkillStep, args map[string]string, timeout time.Duration) (NonBashReplayResult, error)
}

// SandboxRunResult records the outcome of a single sandbox replay.
type SandboxRunResult struct {
	Args    map[string]string `json:"args"`
	Success bool              `json:"success"`
	Output  string            `json:"output,omitempty"`
	Error   string            `json:"error,omitempty"`
}

// GateResult is the outcome of a RepairGate verification.
type GateResult struct {
	Status string `json:"status"`
	Passed bool   `json:"passed"`
	// EvidenceMode distinguishes a real sandbox replay from missing evidence.
	// The default executor never reports a skipped non-bash action as real.
	EvidenceMode string             `json:"evidence_mode"` // real | mock | none
	RunResults   []SandboxRunResult `json:"run_results"`
	PassRate     float64            `json:"pass_rate"`
	Reason       string             `json:"reason"`
}

// IsRealPass is the single admission predicate for automatic mutations. A
// status-only pass from a mock, skipped, or legacy executor is not sufficient
// evidence to change a live Skill definition.
func (r *GateResult) IsRealPass() bool {
	return r != nil && r.Status == "passed" && r.Passed && r.EvidenceMode == "real"
}

// RepairGate verifies repaired/optimized skill steps by replaying historical
// arguments in a sandbox. Only accepts the new version when it passes the
// minimum success threshold.
type RepairGate struct {
	Config         RepairGateConfig
	Executor       SandboxExecutor
	NonBashAdapter NonBashReplayAdapter
}

// NewRepairGate creates a RepairGate with the given config and executor.
func NewRepairGate(cfg RepairGateConfig, executor SandboxExecutor) *RepairGate {
	cfg = defaultRepairGateConfig(cfg)
	return &RepairGate{Config: cfg, Executor: executor}
}

// Verify replays the new steps against historical argument sets in a sandbox.
// Returns a GateResult indicating whether the new steps pass the threshold.
//
// Missing runtime evidence is reported as unverified and is never an
// automatic approval.
func (g *RepairGate) Verify(ctx context.Context, skill *corelib.NLSkillEntry, newSteps []corelib.NLSkillStep, historicalArgs []map[string]string) (*GateResult, error) {
	if g == nil || g.Executor == nil {
		return &GateResult{Status: "unverified", Passed: false, EvidenceMode: "none", Reason: "no sandbox executor configured; runtime verification unavailable"}, nil
	}
	if len(historicalArgs) == 0 {
		return &GateResult{Status: "unverified", Passed: false, EvidenceMode: "none", Reason: "no historical args to replay; no runtime evidence"}, nil
	}

	// Limit to MaxReplayRuns.
	args := historicalArgs
	if len(args) > g.Config.MaxReplayRuns {
		args = args[len(args)-g.Config.MaxReplayRuns:]
	}

	results := make([]SandboxRunResult, 0, len(args))
	var successes int
	adapterEvidence := "real"
	if stepsRequireNonBashReplay(newSteps) {
		if g.NonBashAdapter == nil {
			return &GateResult{Status: "unverified", Passed: false, EvidenceMode: "none", Reason: "non-bash actions require an explicit isolated replay adapter"}, nil
		}
		for i, argSet := range args {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			timeout := g.Config.SandboxTimeout
			replay, err := g.NonBashAdapter.ReplayNonBash(ctx, skill, newSteps, argSet, timeout)
			if err != nil {
				replay.Error = err.Error()
			}
			mode := strings.ToLower(strings.TrimSpace(replay.EvidenceMode))
			if mode != "real" && mode != "mock" {
				mode = "none"
			}
			if mode != "real" {
				adapterEvidence = mode
			}
			results = append(results, SandboxRunResult{Args: argSet, Success: replay.Success, Output: truncateGateOutput(replay.Output, 500), Error: replay.Error})
			if replay.Success {
				successes++
			}
			_ = i
		}
		if adapterEvidence != "real" {
			return &GateResult{Status: "unverified", Passed: false, EvidenceMode: adapterEvidence, RunResults: results, PassRate: float64(successes) / float64(len(results)), Reason: "non-bash replay returned non-real evidence"}, nil
		}
		passRate := float64(successes) / float64(len(results))
		passed := passRate+0.005 >= g.Config.MinPassRate
		status := "failed"
		if passed {
			status = "passed"
		}
		return &GateResult{Status: status, Passed: passed, EvidenceMode: "real", RunResults: results, PassRate: passRate, Reason: fmt.Sprintf("non-bash adapter: %d/%d runs passed", successes, len(results))}, nil
	}

	for i, argSet := range args {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		timeout := g.Config.SandboxTimeout
		runCtx, cancel := context.WithTimeout(ctx, timeout)

		log.Printf("[repair-gate] replaying run %d/%d for skill=%s args=%v",
			i+1, len(args), skill.Name, argSet)

		success, output, err := g.Executor.RunInSandbox(runCtx, skill, newSteps, argSet, timeout)
		cancel()

		result := SandboxRunResult{
			Args:    argSet,
			Success: success,
			Output:  truncateGateOutput(output, 500),
		}
		if err != nil {
			result.Error = err.Error()
		}
		results = append(results, result)

		if success {
			successes++
		}
	}
	// A custom executor may return success without identifying the evidence
	// source. Require an explicit opt-in marker for mock executors; otherwise a
	// non-empty replay result is considered real sandbox evidence.
	// A replay that only reports skipped non-bash actions is not runtime
	// evidence. Preserve the run details but downgrade the aggregate result.
	for _, r := range results {
		if strings.HasPrefix(r.Output, "sandbox_skipped_non_bash:") {
			return &GateResult{Status: "unverified", Passed: false, EvidenceMode: "none", RunResults: results,
				Reason: "replay skipped non-bash actions; runtime verification unavailable"}, nil
		}
	}

	passRate := float64(successes) / float64(len(results))
	passed := passRate+0.005 >= g.Config.MinPassRate
	status := "failed"
	if passed {
		status = "passed"
	}

	reason := fmt.Sprintf("%d/%d runs passed (%.0f%%), threshold %.0f%%",
		successes, len(results), passRate*100, g.Config.MinPassRate*100)
	if passed {
		reason = "PASSED: " + reason
	} else {
		reason = "FAILED: " + reason
	}

	log.Printf("[repair-gate] skill=%s %s", skill.Name, reason)

	return &GateResult{
		Status:       status,
		Passed:       passed,
		EvidenceMode: "real",
		RunResults:   results,
		PassRate:     passRate,
		Reason:       reason,
	}, nil
}

func stepsRequireNonBashReplay(steps []corelib.NLSkillStep) bool {
	for _, step := range steps {
		action := NormalizeStepActionName(step.Action)
		if action != "bash" && action != "ssh_bash" {
			return true
		}
	}
	return false
}

// truncateGateOutput limits output length for storage.
func truncateGateOutput(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// --- Default Sandbox Executor (temp-dir based) ---

// NewDefaultSandboxExecutor returns a TempDirSandboxExecutor that runs bash-only
// skill steps via the shared ExecuteStepsSync engine. Non-bash steps are retained
// as explicit "skipped" evidence and the aggregate gate marks them unverified;
// they are never treated as a runtime pass.
func NewDefaultSandboxExecutor() *TempDirSandboxExecutor {
	return &TempDirSandboxExecutor{StepRunner: defaultBashSandboxStepRunner}
}

// defaultBashSandboxStepRunner executes only bash steps inside the sandbox.
// Skills with no bash steps are reported as unverified when every step is still
// a supported non-bash GUI action after normalization (craft_tool / poll / call_mcp_tool).
// Unsupported aliases (historically shell_tool) must fail the gate instead of
// silently passing with "no bash steps to verify".
func defaultBashSandboxStepRunner(ctx context.Context, sk *corelib.NLSkillEntry, steps []corelib.NLSkillStep, args map[string]string, workDir string) (bool, string, error) {
	skillDir := ""
	if sk != nil {
		skillDir = sk.SkillDir
	}
	bashSteps := make([]corelib.NLSkillStep, 0, len(steps))
	for _, s := range steps {
		normalized := NormalizeStepForRunnerCopy(s, skillDir)
		if err := EnsureStepActionSupported(RunnerBackendGUI, normalized.Action); err != nil {
			return false, "", err
		}
		action := NormalizeStepActionName(normalized.Action)
		if action == "bash" {
			bashSteps = append(bashSteps, normalized)
		}
	}
	if len(bashSteps) == 0 {
		return true, "sandbox_skipped_non_bash: no bash steps to verify", nil
	}
	entry := *sk
	entry.Steps = bashSteps
	result, err := ExecuteStepsSync(ctx, &entry, args, ExecConfig{
		SkillDir: entry.SkillDir,
		Timeout:  0, // caller already set context deadline
	}, &sandboxBashDeps{})
	if result == nil {
		if err != nil {
			return false, "", err
		}
		return false, "", fmt.Errorf("sandbox execution returned nil result")
	}
	out := result.Output
	if result.LastStepOutput != "" {
		out = result.LastStepOutput
	}
	if err != nil || result.StepsFailed > 0 {
		if err == nil {
			err = fmt.Errorf("sandbox: %d step(s) failed", result.StepsFailed)
		}
		return false, out, err
	}
	return true, out, nil
}

// sandboxBashDeps implements ExecDeps for sandbox verification only.
type sandboxBashDeps struct{}

func (d *sandboxBashDeps) ExecuteBash(ctx context.Context, command, workDir string, env map[string]string) (string, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		// cmd /c via Go argument escaping eats inner quotes and mangles CJK
		// paths; NewWindowsShellCommand passes the line verbatim.
		cmd = coretool.NewWindowsShellCommand(ctx, command, workDir)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
		if workDir != "" {
			cmd.Dir = workDir
		}
	}
	environ := append(os.Environ(), "PYTHONIOENCODING=utf-8", "PYTHONUTF8=1", "MACLAW_SKILL_SANDBOX=1")
	for k, v := range env {
		environ = append(environ, k+"="+v)
	}
	cmd.Env = environ
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	return output.String(), err
}

func (d *sandboxBashDeps) OnStepProgress(stepIndex, totalSteps int, stepAction, status string) {
	// no-op in sandbox
}

// TempDirSandboxExecutor is the default SandboxExecutor that uses OS temp
// directories for isolation. It copies the skill directory into a temp dir,
// executes steps there, and cleans up after.
type TempDirSandboxExecutor struct {
	// StepRunner is the function that actually executes skill steps.
	// It receives the skill entry (with SkillDir pointing to the sandbox copy),
	// the steps to execute, args, and a working directory.
	// Returns (success, combined output, error).
	StepRunner func(ctx context.Context, skill *corelib.NLSkillEntry, steps []corelib.NLSkillStep, args map[string]string, workDir string) (bool, string, error)
}

// RunInSandbox implements SandboxExecutor.
func (e *TempDirSandboxExecutor) RunInSandbox(ctx context.Context, skill *corelib.NLSkillEntry, steps []corelib.NLSkillStep, args map[string]string, timeout time.Duration) (bool, string, error) {
	if e.StepRunner == nil {
		return false, "", fmt.Errorf("TempDirSandboxExecutor: StepRunner not configured")
	}

	// Create sandbox temp directory.
	sandboxDir, err := os.MkdirTemp("", "maclaw_skill_sandbox_*")
	if err != nil {
		return false, "", fmt.Errorf("create sandbox dir: %w", err)
	}
	defer os.RemoveAll(sandboxDir)

	// Copy skill directory into sandbox if it exists.
	sandboxSkillDir := sandboxDir
	if skill.SkillDir != "" {
		sandboxSkillDir = filepath.Join(sandboxDir, "skill")
		if err := copyDirForSandbox(skill.SkillDir, sandboxSkillDir); err != nil {
			return false, "", fmt.Errorf("copy skill to sandbox: %w", err)
		}
	}

	// Create a sandbox copy of the skill entry with the sandbox path.
	sandboxSkill := *skill
	sandboxSkill.SkillDir = sandboxSkillDir
	sandboxSkill.Steps = steps

	workDir := filepath.Join(sandboxDir, "workspace")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return false, "", fmt.Errorf("create workspace: %w", err)
	}

	// Inject sandbox marker into environment.
	if args == nil {
		args = make(map[string]string)
	}
	args["__maclaw_sandbox"] = "1"

	// Execute with timeout.
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	success, output, execErr := e.StepRunner(execCtx, &sandboxSkill, steps, args, workDir)
	return success, output, execErr
}

// copyDirForSandbox copies a directory tree. Only copies regular files and
// directories (skips symlinks, devices, etc.) to avoid sandbox escapes.
func copyDirForSandbox(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}

		// Only copy regular files.
		if !info.Mode().IsRegular() {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(targetPath, data, 0o644)
	})
}
