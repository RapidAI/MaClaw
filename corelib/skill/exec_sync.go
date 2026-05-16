package skill

// exec_sync.go provides a synchronous skill execution engine shared by
// GUI (gui/skill_runner.go) and MaClawSrv (corelib/agentservice/skill_integration.go).
//
// This is the Phase 2 extraction: the core step iteration logic that was
// previously duplicated between gui/skill_runner.go's executeAsync and
// agentservice/skill_integration.go's executeSteps.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// ExecDeps is the interface for platform-specific execution dependencies.
// GUI and MaClawSrv each provide their own implementation.
type ExecDeps interface {
	// ExecuteBash runs a shell command and returns combined stdout+stderr.
	// The implementation handles platform-specific shell selection (bash/cmd/powershell).
	ExecuteBash(ctx context.Context, command, workDir string, env map[string]string) (string, error)

	// OnStepProgress reports step execution progress.
	// stepIndex is 0-based, totalSteps is the total number of steps.
	OnStepProgress(stepIndex, totalSteps int, stepAction, status string)
}

// ExecConfig configures a synchronous skill execution.
type ExecConfig struct {
	// SkillDir is the skill's directory (used for {baseDir} substitution).
	SkillDir string

	// Timeout is the maximum execution time for the entire skill run.
	// Zero means no timeout (caller should set a context deadline instead).
	Timeout time.Duration

	// Params are the skill's parameter schema (for ResolveStep alias resolution).
	Params []corelib.NLSkillParam

	// SelectedSteps limits execution to steps with matching labels (api_workflow mode).
	// Empty means execute all steps.
	SelectedSteps []string

	// PreferredShell overrides automatic shell detection ("bash" or "cmd").
	PreferredShell string
}

// ExecResult is the result of a synchronous skill execution.
type ExecResult struct {
	// Output is the combined output from all executed steps.
	Output string

	// Vars contains all captured variables (from step.Capture directives).
	Vars map[string]string

	// StepsExecuted is the number of steps that were actually run.
	StepsExecuted int

	// StepsFailed is the number of steps that failed.
	StepsFailed int

	// LastStepOutput is the output of the last executed step.
	LastStepOutput string
}

// ExecuteStepsSync runs a skill's steps synchronously, handling:
// - Variable substitution ({{key}}, ${key})
// - When conditions (conditional step execution)
// - Output variable capture (step.Capture)
// - Step label selection (api_workflow mode)
// - {baseDir} placeholder replacement
//
// It does NOT handle: craft_tool steps, poll loops, interactive mode,
// session binding, or pipeline skills. These require the full GUI SkillRunner.
//
// This function is the single source of truth for sequential bash step execution.
// Both GUI's executeAsync (for simple bash-only skills) and MaClawSrv's
// SkillToolBridge.RunSkill delegate to this function.
func ExecuteStepsSync(ctx context.Context, entry *corelib.NLSkillEntry, vars map[string]string, cfg ExecConfig, deps ExecDeps) (*ExecResult, error) {
	if entry == nil {
		return nil, fmt.Errorf("skill entry is nil")
	}
	if len(entry.Steps) == 0 {
		return nil, fmt.Errorf("skill has no executable steps")
	}
	if deps == nil {
		return nil, fmt.Errorf("execution dependencies are required")
	}

	// Apply timeout if configured.
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	// Initialize vars map.
	if vars == nil {
		vars = make(map[string]string)
	}

	result := &ExecResult{
		Vars: vars,
	}

	steps := entry.Steps

	// Filter by selected labels (api_workflow mode).
	if len(cfg.SelectedSteps) > 0 {
		steps = SelectedExecutableSteps(steps, cfg.SelectedSteps)
	}

	totalSteps := len(steps)

	for i, step := range steps {
		// Check context cancellation.
		if ctx.Err() != nil {
			return result, ctx.Err()
		}

		// Evaluate when condition.
		if step.When != "" {
			if !EvaluateStepWhen(step.When, vars) {
				continue
			}
		}

		// Evaluate legacy condition field.
		if step.Condition != "" {
			switch step.Condition {
			case "on_failure":
				if result.StepsFailed == 0 {
					continue
				}
			case "on_success":
				if result.StepsFailed > 0 {
					continue
				}
			}
		}

		// Only bash steps are supported in this synchronous engine.
		action := NormalizeStepActionName(step.Action)
		if action != "bash" && action != "" {
			// Skip unsupported step types gracefully.
			if deps != nil {
				deps.OnStepProgress(i, totalSteps, action, "skipped_unsupported")
			}
			continue
		}

		// Resolve step: substitute variables, apply aliases, inject CLI args.
		resolved, err := ResolveStep(step, vars, cfg.SkillDir, cfg.Params, nil)
		if err != nil {
			return result, fmt.Errorf("step %d resolve failed: %w", i+1, err)
		}
		resolvedStep := resolved.Step

		// Extract command from params.
		command := ""
		if resolvedStep.Params != nil {
			if cmd, ok := resolvedStep.Params["command"].(string); ok {
				command = cmd
			}
		}
		if command == "" {
			continue
		}

		// Replace {baseDir} placeholder with skill directory.
		if cfg.SkillDir != "" {
			command = strings.ReplaceAll(command, "{baseDir}", cfg.SkillDir)
			command = strings.ReplaceAll(command, "${baseDir}", cfg.SkillDir)
		}

		// Determine working directory.
		workDir := cfg.SkillDir
		if wd, ok := resolvedStep.Params["working_dir"].(string); ok && wd != "" {
			workDir = SubstituteVarsInString(wd, vars)
			if cfg.SkillDir != "" {
				workDir = strings.ReplaceAll(workDir, "{baseDir}", cfg.SkillDir)
				workDir = strings.ReplaceAll(workDir, "${baseDir}", cfg.SkillDir)
			}
		}

		// Report progress.
		if deps != nil {
			deps.OnStepProgress(i, totalSteps, "bash", "running")
		}

		// Build environment variables for this step.
		var stepEnv map[string]string
		if envRaw, ok := resolvedStep.Params["env"].(map[string]interface{}); ok {
			stepEnv = make(map[string]string, len(envRaw))
			for k, v := range envRaw {
				if s, ok := v.(string); ok {
					stepEnv[k] = s
				}
			}
		}

		// Execute the command.
		output, execErr := deps.ExecuteBash(ctx, command, workDir, stepEnv)
		result.StepsExecuted++
		result.LastStepOutput = output

		if execErr != nil {
			result.StepsFailed++

			// Check on_error policy.
			onError := strings.TrimSpace(step.OnError)
			if onError == "continue" {
				// Capture variables even on failure (some skills use partial output).
				if len(step.Capture) > 0 {
					captured := CaptureOutputVariables(output, step.Capture)
					for k, v := range captured {
						vars[k] = v
					}
				}
				if deps != nil {
					deps.OnStepProgress(i, totalSteps, "bash", "failed_continue")
				}
				continue
			}

			// Default: stop on error.
			return result, fmt.Errorf("step %d failed: %w\nOutput: %s", i+1, execErr, truncateOutput(output, 2000))
		}

		// Capture output variables.
		if len(step.Capture) > 0 {
			captured := CaptureOutputVariables(output, step.Capture)
			for k, v := range captured {
				vars[k] = v
			}
		}

		// Accumulate output.
		if output != "" {
			if result.Output != "" {
				result.Output += "\n"
			}
			result.Output += output
		}

		if deps != nil {
			deps.OnStepProgress(i, totalSteps, "bash", "success")
		}
	}

	if result.Output == "" && result.StepsExecuted > 0 {
		result.Output = "Skill executed successfully (no output)"
	}

	return result, nil
}

// truncateOutput truncates output to maxLen characters for error messages.
func truncateOutput(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "... (truncated)"
}
