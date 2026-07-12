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
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// PipelineResult is the outcome of a pipeline execution.
type PipelineResult struct {
	Status      PipelineStatus       `json:"status"`
	FailedAt    int                  `json:"failed_at,omitempty"`
	FailedSkill string               `json:"failed_skill,omitempty"`
	StoppedAt   int                  `json:"stopped_at,omitempty"`
	Error       string               `json:"error,omitempty"`
	StepResults []PipelineStepResult `json:"step_results,omitempty"`
	Vars        map[string]string    `json:"vars,omitempty"` // accumulated vars from all steps
}

// PipelineStepResult records the outcome of one pipeline step.
type PipelineStepResult struct {
	Skill        string             `json:"skill"`
	Status       PipelineStepStatus `json:"status"`
	CapturedVars map[string]string  `json:"captured_vars,omitempty"`
	Error        string             `json:"error,omitempty"`
	Duration     string             `json:"duration,omitempty"`
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
		return &PipelineResult{Status: PipelineStatusCompleted}, nil
	}
	if pr == nil || pr.Executor == nil {
		return &PipelineResult{Status: PipelineStatusFailed, Error: "pipeline executor is nil"}, fmt.Errorf("pipeline executor is nil")
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
			result.Status = PipelineStatusCancelled
			result.Error = ctx.Err().Error()
			return result, nil
		default:
		}

		skillName := strings.TrimSpace(step.Skill)
		if skillName == "" {
			errMsg := fmt.Sprintf("pipeline step %d is missing skill", i+1)
			result.StepResults = append(result.StepResults, PipelineStepResult{
				Status: PipelineStepStatusFailed,
				Error:  errMsg,
			})
			if !step.ContinueOnFail {
				result.Status = PipelineStatusFailed
				result.FailedAt = i
				result.Error = errMsg
				return result, nil
			}
			continue
		}

		// Resolve template variables in params
		resolvedParams := resolveTemplateParams(step.Params, vars)
		if errMsg := unresolvedPipelineParamMessage(resolvedParams); errMsg != "" {
			result.StepResults = append(result.StepResults, PipelineStepResult{
				Skill:  skillName,
				Status: PipelineStepStatusFailed,
				Error:  errMsg,
			})
			if !step.ContinueOnFail {
				result.Status = PipelineStatusFailed
				result.FailedAt = i
				result.FailedSkill = skillName
				result.Error = errMsg
				return result, nil
			}
			continue
		}

		// Execute sub-skill
		start := time.Now()
		captured, output, err := pr.Executor.RunSubSkill(ctx, skillName, resolvedParams)
		duration := time.Since(start).Round(time.Millisecond).String()
		if strings.TrimSpace(output) != "" {
			if captured == nil {
				captured = map[string]string{}
			}
			if _, hasOutput := captured["output"]; !hasOutput {
				captured["output"] = output
			}
		}

		stepResult := PipelineStepResult{
			Skill:        skillName,
			CapturedVars: captured,
			Duration:     duration,
		}

		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				stepResult.Status = PipelineStepStatusCancelled
				stepResult.Error = ctxErr.Error()
				result.StepResults = append(result.StepResults, stepResult)
				result.Status = PipelineStatusCancelled
				result.Error = ctxErr.Error()
				return result, nil
			}
			stepResult.Status = PipelineStepStatusFailed
			stepResult.Error = err.Error()
			result.StepResults = append(result.StepResults, stepResult)

			if !step.ContinueOnFail {
				result.Status = PipelineStatusFailed
				result.FailedAt = i
				result.FailedSkill = skillName
				result.Error = err.Error()
				return result, nil
			}
			mergePipelineStepVars(vars, skillName, captured, output)
			// ContinueOnFail: record failure but proceed
		} else {
			stepResult.Status = PipelineStepStatusCompleted
			result.StepResults = append(result.StepResults, stepResult)

			mergePipelineStepVars(vars, skillName, captured, output)
		}

		// Checkpoint gate
		if step.Checkpoint && pr.AskUser != nil {
			msg := resolveTemplateString(step.CheckpointMessage, vars)
			if msg == "" {
				msg = fmt.Sprintf("Step %d (%s) completed. Continue?", i+1, skillName)
			}
			if step.TimeImpactOnReject != "" {
				msg += "\n(stopping will delay by " + step.TimeImpactOnReject + ")"
			}

			// Options: index 0 = continue, index 1 = stop.
			// The actual option text is provided by the caller (localized).
			// Pipeline only checks the index — language-independent.
			chosenIdx, askErr := pr.AskUser(msg, []string{"continue", "stop"})
			if ctxErr := ctx.Err(); ctxErr != nil {
				result.Status = PipelineStatusCancelled
				result.Error = ctxErr.Error()
				return result, nil
			}
			if askErr != nil {
				result.Status = PipelineStatusFailed
				result.FailedAt = i
				result.FailedSkill = skillName
				result.Error = fmt.Sprintf("checkpoint prompt failed: %v", askErr)
				return result, nil
			}
			if chosenIdx != 0 {
				result.Status = PipelineStatusStoppedAtCheckpoint
				result.StoppedAt = i
				return result, nil
			}
		}
	}

	result.Status = PipelineStatusCompleted
	return result, nil
}

func mergePipelineStepVars(vars map[string]string, skillName string, captured map[string]string, output string) {
	for k, v := range captured {
		vars[skillName+"."+k] = v
	}
	if output != "" {
		vars[skillName+".output"] = truncateForVar(output, 500)
	}
}

// FormatResult generates a human-readable summary of the pipeline execution.
func FormatPipelineResult(result *PipelineResult) string {
	if result == nil {
		return "Pipeline status: unknown\n"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Pipeline 状态: %s\n", result.Status))

	for i, sr := range result.StepResults {
		icon := ""
		if sr.Status.IsFailed() {
			icon = ""
		} else if sr.Status.IsSkipped() {
			icon = ""
		}
		b.WriteString(fmt.Sprintf("  %d. %s %s (%s)", i+1, icon, sr.Skill, sr.Duration))
		if sr.Error != "" {
			b.WriteString(" — " + sr.Error)
		}
		b.WriteString("\n")
		if output := strings.TrimSpace(sr.CapturedVars["output"]); output != "" {
			displayOutput, truncated := truncatePipelineText(output, 1000)
			b.WriteString("```\n")
			b.WriteString(displayOutput)
			if truncated {
				b.WriteString("\n... (truncated)")
			}
			b.WriteString("\n```\n")
		}
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

func unresolvedPipelineParamMessage(params map[string]string) string {
	var missing []string
	for key, value := range params {
		matches := pipelineVarPattern.FindAllString(value, -1)
		for _, match := range matches {
			missing = append(missing, fmt.Sprintf("%s=%s", key, match))
		}
	}
	if len(missing) == 0 {
		return ""
	}
	sort.Strings(missing)
	return "pipeline params contain unresolved variable(s): " + strings.Join(missing, ", ")
}

func truncateForVar(s string, maxLen int) string {
	truncated, ok := truncatePipelineText(s, maxLen)
	if !ok {
		return s
	}
	return truncated + "..."
}

func truncatePipelineText(s string, maxRunes int) (string, bool) {
	if maxRunes <= 0 {
		return "", s != ""
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s, false
	}
	return string(runes[:maxRunes]), true
}
