package skill

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// SelfRepairThreshold defines when a skill is eligible for self-repair.
const SelfRepairThreshold = 3 // minimum UsageCount before repair is considered

// SelfRepairMaxAttempts is the maximum consecutive repair attempts before
// the skill is marked as needs_review.
const SelfRepairMaxAttempts = 3

// LLMRepairer abstracts the LLM call needed for skill self-repair.
type LLMRepairer interface {
	ChatCall(messages []map[string]string) (string, error)
	IsConfigured() bool
}

// RepairContext provides detailed failure context for the LLM repairer.
// Inspired by Memento-Skills' failure attribution mechanism: the repairer
// needs to see the specific failure trace, not just the last error string.
type RepairContext struct {
	FailedStepIndex    int               `json:"failed_step_index"`
	StepOutput         string            `json:"step_output"`          // truncated to 2000 chars
	ErrorClass         string            `json:"error_class"`          // from classifySkillStepError
	RunArgs            map[string]string  `json:"run_args"`            // args used in this run
	PreviousRepairCount int              `json:"previous_repair_count"`
}

// nonRepairableErrorClasses lists error types caused by external/transient
// factors that cannot be fixed by modifying the skill's steps.
var nonRepairableErrorClasses = map[string]bool{
	"rate_limit":    true,
	"network_error": true,
}

// IsRepairableError returns true if the error class is worth attempting
// automated repair. External/transient errors return false.
func IsRepairableError(errorClass string) bool {
	return !nonRepairableErrorClasses[errorClass]
}

// ShouldAttemptRepair returns true if the skill has enough usage data and
// a low enough success rate to warrant an automated repair attempt.
func ShouldAttemptRepair(skill *corelib.NLSkillEntry) bool {
	if skill == nil {
		return false
	}
	if skill.UsageCount < SelfRepairThreshold {
		return false
	}
	if skill.LastError == "" {
		return false
	}
	if skill.RepairAttemptCount >= SelfRepairMaxAttempts {
		return false
	}
	successRate := float64(skill.SuccessCount) / float64(skill.UsageCount)
	return successRate < 0.5
}

// RepairResult holds the outcome of a self-repair attempt.
type RepairResult struct {
	Repaired    bool              `json:"repaired"`
	NewSteps    []SkillYAMLStep   `json:"new_steps,omitempty"`
	Explanation string            `json:"explanation"`
	ShouldDisable bool           `json:"should_disable"`
}

// AttemptRepair uses an LLM to analyze the skill's last error and propose
// modified steps. Returns a RepairResult with the proposed changes.
// The caller is responsible for writing the changes back to disk.
func AttemptRepair(llm LLMRepairer, skill *corelib.NLSkillEntry) (*RepairResult, error) {
	return AttemptRepairWithContext(llm, skill, nil)
}

// AttemptRepairWithContext uses an LLM to analyze the skill's failure with
// detailed context (failed step output, error class, run args) and propose
// modified steps. When ctx is nil, falls back to basic repair using LastError.
//
// Inspired by Memento-Skills' Reflect phase: failures are training signals,
// not just reasons to retry. The LLM sees the specific failure trace and
// proposes targeted fixes rather than generic rewrites.
func AttemptRepairWithContext(llm LLMRepairer, skill *corelib.NLSkillEntry, ctx *RepairContext) (*RepairResult, error) {
	if llm == nil || !llm.IsConfigured() {
		return nil, fmt.Errorf("LLM not configured for skill repair")
	}
	if skill == nil {
		return nil, fmt.Errorf("skill is nil")
	}

	// Check if error class is repairable.
	if ctx != nil && !IsRepairableError(ctx.ErrorClass) {
		return &RepairResult{
			Repaired:    false,
			Explanation: fmt.Sprintf("error class %q is not repairable (external factor)", ctx.ErrorClass),
		}, nil
	}

	// Build the current steps as YAML-like text for the LLM.
	var stepsDesc strings.Builder
	for i, step := range skill.Steps {
		fmt.Fprintf(&stepsDesc, "  Step %d: action=%s", i, step.Action)
		if len(step.Params) > 0 {
			paramsJSON, _ := json.Marshal(step.Params)
			fmt.Fprintf(&stepsDesc, " params=%s", string(paramsJSON))
		}
		if step.OnError != "" {
			fmt.Fprintf(&stepsDesc, " on_error=%s", step.OnError)
		}
		stepsDesc.WriteString("\n")
	}

	systemPrompt := `You are a skill repair assistant. A skill has been failing repeatedly.
Analyze the error and the current steps, then propose fixed steps.

Rules:
- Keep the same action names (tool names) — only modify params or step order
- Do NOT change the skill's core functionality — only fix the specific failure
- If a step consistently fails, add on_error: "skip" or "continue"
- If the error is fundamental (wrong tool, impossible task), set should_disable: true
- Return ONLY a JSON object with fields: repaired (bool), new_steps (array), explanation (string), should_disable (bool)
- Each step in new_steps has: action (string), params (object), on_error (string, optional)`

	var contextSection string
	if ctx != nil {
		// Truncate step output to avoid blowing up the prompt.
		output := ctx.StepOutput
		if len([]rune(output)) > 2000 {
			output = string([]rune(output)[:2000]) + "\n... (truncated)"
		}
		argsJSON, _ := json.Marshal(ctx.RunArgs)
		contextSection = fmt.Sprintf(`
Failed step index: %d
Error classification: %s
Step output:
%s

Run arguments: %s
Previous repair attempts: %d`,
			ctx.FailedStepIndex,
			ctx.ErrorClass,
			output,
			string(argsJSON),
			ctx.PreviousRepairCount,
		)
	}

	userPrompt := fmt.Sprintf(`Skill: %s
Description: %s
Usage: %d times, %d successes (%.0f%% success rate)
Last error: %s
%s
Current steps:
%s

Propose a fix.`,
		skill.Name,
		skill.Description,
		skill.UsageCount,
		skill.SuccessCount,
		float64(skill.SuccessCount)/float64(skill.UsageCount)*100,
		skill.LastError,
		contextSection,
		stepsDesc.String(),
	)

	resp, err := llm.ChatCall([]map[string]string{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": userPrompt},
	})
	if err != nil {
		return nil, fmt.Errorf("LLM repair call failed: %w", err)
	}

	// Parse the response.
	body := strings.TrimSpace(resp)
	body = strings.TrimPrefix(body, "```json")
	body = strings.TrimPrefix(body, "```")
	body = strings.TrimSuffix(body, "```")
	body = strings.TrimSpace(body)

	var result RepairResult
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		return nil, fmt.Errorf("parse repair response: %w", err)
	}

	log.Printf("[skill-repair] skill=%s repaired=%v should_disable=%v explanation=%s",
		skill.Name, result.Repaired, result.ShouldDisable, result.Explanation)

	return &result, nil
}

// ApplyRepair writes the repaired steps back to the skill entry.
// Returns true if the skill was modified, false if it was disabled or unchanged.
func ApplyRepair(skill *corelib.NLSkillEntry, result *RepairResult) bool {
	if result.ShouldDisable {
		skill.Status = "needs_review"
		skill.LastError = fmt.Sprintf("auto-disabled: %s", result.Explanation)
		log.Printf("[skill-repair] marked skill %s as needs_review: %s", skill.Name, result.Explanation)
		return false
	}

	if !result.Repaired || len(result.NewSteps) == 0 {
		return false
	}

	// Convert RepairResult steps to NLSkillStep.
	newSteps := make([]corelib.NLSkillStep, len(result.NewSteps))
	for i, s := range result.NewSteps {
		newSteps[i] = corelib.NLSkillStep{
			Action:  s.Action,
			Params:  s.Params,
			OnError: s.OnError,
		}
	}

	skill.Steps = newSteps
	skill.LastError = fmt.Sprintf("auto-repaired: %s", result.Explanation)
	skill.RepairAttemptCount++
	skill.LastRepairAt = time.Now().Format(time.RFC3339)

	// Append to repair history (keep last 5).
	record := corelib.SkillRepairRecord{
		Timestamp:   skill.LastRepairAt,
		Explanation: result.Explanation,
		Success:     false, // caller sets to true after verify
	}
	skill.RepairHistory = append(skill.RepairHistory, record)
	if len(skill.RepairHistory) > 5 {
		skill.RepairHistory = skill.RepairHistory[len(skill.RepairHistory)-5:]
	}

	log.Printf("[skill-repair] repaired skill %s with %d new steps (attempt %d)",
		skill.Name, len(newSteps), skill.RepairAttemptCount)
	return true
}

// MarkRepairVerified marks the last repair history entry as verified (success).
// Call this after VerifyRepair succeeds.
func MarkRepairVerified(skill *corelib.NLSkillEntry) {
	if len(skill.RepairHistory) > 0 {
		skill.RepairHistory[len(skill.RepairHistory)-1].Success = true
	}
}

// ResetRepairCount resets the consecutive repair attempt counter.
// Call this when the skill succeeds after a repair, indicating the fix worked.
func ResetRepairCount(skill *corelib.NLSkillEntry) {
	skill.RepairAttemptCount = 0
}
