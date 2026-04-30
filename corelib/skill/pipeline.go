package skill

// pipeline.go implements the multi-phase checkpoint pattern from the
// "5 Skill Architecture Patterns" article.
//
// A pipeline skill orchestrates multiple sub-skills in sequence, with
// optional checkpoints (Go/No-Go decision points) between phases.
// Each sub-skill's captured output flows into the next sub-skill's vars.
//
// This is distinct from TaskExecutionOrchestrator (which orchestrates
// coding tasks in the agent loop) and WorkflowEngine (which orchestrates
// multi-phase document workflows). PipelineRunner orchestrates skill-to-skill
// execution within a single manage_skill(action=run) call.
//
// Architecture:
//   pipeline.yaml declares: skill → params → checkpoint → checkpoint_message
//   PipelineRunner: for each step → resolve params → run sub-skill → checkpoint gate
//   Sub-skill output: captured vars flow into next step's params via {{skill.var}} templates

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// PipelineResult is the outcome of a pipeline execution.
type PipelineResult struct {
	Status      string            `json:"status"` // "completed", "failed", "stopped_at_checkpoint", "cancelled"
	FailedAt    int               `json:"failed_at,omitempty"`
	FailedSkill string            `json:"failed_skill,omitempty"`
	StoppedAt   int               `json:"stopped_at,omitempty"`
	Error       string            `json:"error,omitempty"`
	StepResults []PipelineStepResult `json:"step_results,omitempty"`
	Vars        map[string]string `json:"vars,omitempty"` // accumulated vars from all steps
}

// PipelineStepResult records the outcome of one pipeline step.
type PipelineStepResult struct {
	Skill        string            `json:"skill"`
	Status       string            `json:"status"` // "completed", "failed", "skipped"
	CapturedVars map[string]string `json:"captured_vars,omitempty"`
	Error        string            `json:"error,omitempty"`
	Duration     string            `json:"duration,omitempty"`
}

// SkillExecutor is the interface that PipelineRunner uses to execute
// sub-skills. This decouples the pipeline from the concrete Runner
// implementation (GUI SkillRunner or TUI toolRunSkill).
type SkillExecutor interface {
	// RunSubSkill executes a skill by name with the given parameters.
	// Returns captured variables and any error.
	RunSubSkill(ctx context.Context, skillName string, params map[string]string) (capturedVars map[string]string, output string, err error)
}

// AskUserFunc is the callback for checkpoint confirmation.
// Returns the 0-based index of the option the user chose, or error.
// The caller provides the options list; the callback presents them to the
// user and returns which one was selected. This is language-independent —
// the pipeline checks the index, not the option text.
type AskUserFunc func(question string, options []string) (chosenIndex int, err error)

// PipelineRunner executes a skill pipeline step by step.
type PipelineRunner struct {
	Executor SkillExecutor
	AskUser  AskUserFunc
}

// Run executes all steps in the pipeline sequentially.
// Each step's captured output is available to subsequent steps via
// {{skillName.varName}} template syntax in params.
//
// Checkpoints pause execution and ask the user for confirmation.
// The user can stop the pipeline at any checkpoint.
func (pr *PipelineRunner) Run(ctx context.Context, steps []corelib.SkillPipelineStep, initialVars map[string]string) (*PipelineResult, error) {
	if len(steps) == 0 {
		return &PipelineResult{Status: "completed"}, nil
	}

	vars := make(map[string]string)
	for k, v := range initialVars {
		vars[k] = v
	}

	result := &PipelineResult{
		StepResults: make([]PipelineStepResult, 0, len(steps)),
		Vars:        vars,
	}

	for i, step := range steps {
		// Check cancellation
		select {
		case <-ctx.Done():
			result.Status = "cancelled"
			return result, nil
		default:
		}

		// Resolve template variables in params
		resolvedParams := resolveTemplateParams(step.Params, vars)

		// Execute sub-skill
		start := time.Now()
		captured, output, err := pr.Executor.RunSubSkill(ctx, step.Skill, resolvedParams)
		duration := time.Since(start).Round(time.Millisecond).String()

		stepResult := PipelineStepResult{
			Skill:        step.Skill,
			CapturedVars: captured,
			Duration:     duration,
		}

		if err != nil {
			stepResult.Status = "failed"
			stepResult.Error = err.Error()
			result.StepResults = append(result.StepResults, stepResult)

			if !step.ContinueOnFail {
				result.Status = "failed"
				result.FailedAt = i
				result.FailedSkill = step.Skill
				result.Error = err.Error()
				return result, nil
			}
			// ContinueOnFail: record failure but proceed
		} else {
			stepResult.Status = "completed"
			result.StepResults = append(result.StepResults, stepResult)

			// Merge captured vars into pipeline vars with skill-name prefix
			for k, v := range captured {
				vars[step.Skill+"."+k] = v
			}
			// Also store raw output for simple reference
			if output != "" {
				vars[step.Skill+".output"] = truncateForVar(output, 500)
			}
		}

		// Checkpoint gate
		if step.Checkpoint && pr.AskUser != nil {
			msg := resolveTemplateString(step.CheckpointMessage, vars)
			if msg == "" {
				msg = fmt.Sprintf("Step %d (%s) completed. Continue?", i+1, step.Skill)
			}
			if step.TimeImpactOnReject != "" {
				msg += "\n(stopping will delay by " + step.TimeImpactOnReject + ")"
			}

			// Options: index 0 = continue, index 1 = stop.
			// The actual option text is provided by the caller (localized).
			// Pipeline only checks the index — language-independent.
			chosenIdx, askErr := pr.AskUser(msg, []string{"continue", "stop"})
			if askErr != nil || chosenIdx != 0 {
				result.Status = "stopped_at_checkpoint"
				result.StoppedAt = i
				return result, nil
			}
		}
	}

	result.Status = "completed"
	return result, nil
}

// FormatResult generates a human-readable summary of the pipeline execution.
func FormatPipelineResult(result *PipelineResult) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Pipeline 状态: %s\n", result.Status))

	for i, sr := range result.StepResults {
		icon := "✅"
		if sr.Status == "failed" {
			icon = "❌"
		} else if sr.Status == "skipped" {
			icon = "⏭️"
		}
		b.WriteString(fmt.Sprintf("  %d. %s %s (%s)", i+1, icon, sr.Skill, sr.Duration))
		if sr.Error != "" {
			b.WriteString(" — " + sr.Error)
		}
		b.WriteString("\n")
	}

	if result.Error != "" {
		b.WriteString("错误: " + result.Error + "\n")
	}
	return b.String()
}

// --- Template resolution ---

var pipelineVarPattern = regexp.MustCompile(`\{\{([a-zA-Z0-9_\-]+(?:\.[a-zA-Z0-9_\-]+)?)\}\}`)

func resolveTemplateParams(params map[string]string, vars map[string]string) map[string]string {
	if len(params) == 0 {
		return params
	}
	resolved := make(map[string]string, len(params))
	for k, v := range params {
		resolved[k] = resolveTemplateString(v, vars)
	}
	return resolved
}

func resolveTemplateString(s string, vars map[string]string) string {
	return pipelineVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		key := match[2 : len(match)-2] // strip {{ and }}
		if v, ok := vars[key]; ok {
			return v
		}
		return match // leave unresolved
	})
}

func truncateForVar(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
